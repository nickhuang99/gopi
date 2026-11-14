package lineedit

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nickhuang99/gopi/internal/sgr"
	"github.com/nickhuang99/gopi/internal/signalrec"
)

// Raw-mode flag sets. Each value is named (Rule 1) because "which flags raw
// mode clears" is exactly the knowledge a future reader needs in one place —
// the bit-mask restore this file exists to replace got it wrong precisely
// because the set was implicit.
const (
	// lineeditSurviveWait is how long the fatal path waits before reporting that
	// the re-raise did not land. Same measurement and same reasoning as
	// internal/signalrec's reraiseSurviveWait (on this host the delivery follows
	// syscall.Kill by 130µs … 307µs), and it costs nothing on the path that
	// dies, which never reaches the far side of it.
	lineeditSurviveWait = 500 * time.Millisecond
	// rawInputFlags are cleared on input: IXON (Ctrl+S/Q flow control), ICRNL
	// (CR→NL translation — cleared so Enter arrives as CR and can be told
	// apart from a pasted NL), BRKINT (break → SIGINT), plus the parity and
	// framing handling (INPCK/ISTRIP/PARMRK/IGNBRK).
	rawInputFlags = unix.IXON | unix.ICRNL | unix.BRKINT | unix.INPCK | unix.ISTRIP | unix.PARMRK | unix.IGNBRK
	// rawOutputFlags are cleared on output: OPOST (all output
	// post-processing, so our LF means LF and our CR means "column 0").
	rawOutputFlags = unix.OPOST
	// rawLocalFlags are cleared locally: ECHO/ECHONL/ICANON (we own the line
	// buffer and draw it ourselves), ISIG (Ctrl+C arrives as a byte — the chat
	// REPL finalizes its session on it itself), IEXTEN.
	rawLocalFlags = unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	// rawControlFlags are reset and then set to rawControlValue: 8-bit
	// characters, no parity.
	rawControlFlags = unix.CSIZE | unix.PARENB
	// rawControlValue is the control-flag value raw mode installs: CS8.
	rawControlValue = unix.CS8
	// rawVMIN makes read(2) return as soon as one byte is available; rawVTIME
	// leaves no inter-byte timer (we do our own paste-burst timing).
	rawVMIN  = 1
	rawVTIME = 0
)

// Escape sequences the editor writes. Named (Rule 1b) because the same
// sequences are compared in tests and written on every redraw — a typo in one
// spelling yields a terminal that ignores us, silently.
//
// The SEQUENCES come from internal/sgr — the one escape vocabulary this project
// emits — and the names here are the editor's own vocabulary for what it does
// with them (Rule 13/14). The pre-merge branch declared these spellings again
// as literals in this package; that second family is retired, so a change to
// the wire bytes cannot leave one caller composing the old shape.
// These are VARS, not consts, because a sequence is now composed by a call
// (sgr's builders) rather than written as a literal — the go compiler requires
// a constant expression for const. They are assigned once at init and never
// written afterwards.
var (
	escCSI          = sgr.CSI
	escBracketedOn  = sgr.BracketedPasteSeq(true)
	escBracketedOff = sgr.BracketedPasteSeq(false)
	escCursorShow   = sgr.CursorShowSeq()
	escEraseLine    = sgr.EraseLineSeq()
	escEraseToEnd   = sgr.EraseToEndLineSeq()
	escClearScreen  = sgr.EraseDisplaySeq()
	escCarriage     = "\r"
	escNewline      = "\n"
)

// Polling shape. pollTimeoutBlock is the "no timeout" value for poll(2);
// readBufBytes bounds one read(2); writeBufBytes bounds one redraw write.
const (
	pollTimeoutBlock = -1
	readBufBytes     = 4096
	writeBufBytes    = 8192
	winchPipeBytes   = 1
)

// fdMinValid is the lowest descriptor number os.File.Fd can return for a real
// stream; anything below means the file is not usable for termios ioctls.
const fdMinValid = 0

// inputReadyTimeoutMS is the readiness probe's timeout. Zero makes the probe a
// pure poll(2) that never waits: the question is "is there input NOW", and a
// probe that blocked would be a second reader competing with the read loop.
const inputReadyTimeoutMS = 0

// ErrNotTerminal reports that the file is not a terminal, so raw mode cannot
// be entered (the caller keeps the cooked path, unchanged).
var ErrNotTerminal = errors.New("lineedit: not a terminal")

// IsTerminal reports whether f is attached to a terminal. It asks the kernel
// (the termios ioctl succeeds) instead of trusting ModeCharDevice: /dev/null is
// a character device with no termios, and answering "yes" there would put a
// pipe-like path into raw mode.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fd := int(f.Fd())
	if fd < fdMinValid {
		return false
	}
	_, err := readTermios(fd)
	return err == nil
}

// EventKind says what woke a read.
type EventKind int

// The wake reasons: input bytes, a terminal resize, or a notification from
// inside the process (a status publish or a completion candidate list — see
// notify.go).
const (
	EventNone EventKind = iota
	EventInput
	EventWinch
	EventWake
)

// Event is one wake-up of Terminal.ReadEvent.
type Event struct {
	Kind  EventKind
	Bytes int // bytes placed in the caller's buffer (EventInput only)
	EOF   bool
}

// Notifier is the seam a publisher uses to wake a blocked read. The editor
// passes its own implementation (wakeSink) and calls it after a publish.
type Notifier interface {
	// Notify wakes the reader. It never blocks.
	Notify()
	// NotifyEnabled reports whether a wake-up will actually reach a blocked
	// read. False means the notification is a no-op and the caller should say
	// so rather than silently degrade (Rule 15).
	NotifyEnabled() bool
}

// Source is the interface the editor consumes: the two things a line
// editor needs from a terminal (event reads with an optional timeout, and the
// current width). It is satisfied by *Terminal and by the tests' scripted
// source, so the editing POLICY (including the paste-burst classification of a
// newline) is asserted without a pty — only the ioctl layer sits behind it.
type Source interface {
	ReadEvent(buf []byte, timeoutMS int) (Event, error)
	Width(fallback int) int
	// Height reports the terminal's row count (fallback when unknown). The
	// editor needs it to reserve the lower rows for the answer band, the
	// completion menu and the status line (A5/A7).
	Height(fallback int) int
}

// inputBacklog is the OPTIONAL source seam for reporting that input is already
// waiting. It is not part of Source: a source that cannot answer (no stdin
// backlog, or none it will admit to) simply does not implement it, and the read
// loop takes the unread arm — the same drawing order it has always had.
//
// Why the editor asks AT ALL: the read loop's ordering guarantee — a keystroke
// already queued is read before a publish is drawn — is only as strong as the
// event the source reports. *Terminal reports input AHEAD of the wake inside one
// poll (see ReadEvent), but a scripted source can hold a step while reporting
// the publish's wake, and a single EventWake then leaves the loop drawing input
// it has not read.
type inputBacklog interface {
	// InputPending reports whether input is waiting to be read. A FALSE answer
	// is a hint the editor acts on, never a safety property: a source whose
	// input only becomes ready later costs at most the old unread arm (the next
	// wake sees it).
	InputPending() bool
}

// Compile-time proof that the real terminal satisfies the editor's source.
var _ Source = (*Terminal)(nil)

// Terminal owns terminal state for one interactive session: it saves the FULL
// termios attribute set on entry and restores it on every exit path (Close,
// signal-driven teardown, or a panic unwinding through a deferred Close). It
// also turns SIGWINCH into a readable event so a redraw happens on resize even
// though Go installs its signal handlers with SA_RESTART (which would silently
// restart a bare read/poll and lose the resize).
//
// Invariant: after Close returns, the descriptor's attributes are byte-equal to
// the snapshot taken by Open — the same struct, not "close enough".
type Terminal struct {
	in       *os.File
	out      io.Writer
	snapshot termiosState
	closed   bool

	winchPipeR *os.File
	winchPipeW *os.File
	// winchPipeReadFD / winchPipeWriteFD are the raw descriptors, captured ONCE
	// at creation. Every later poll/read/write uses these, never f.Fd(): Go's
	// (*os.File).Fd() forces the descriptor back into BLOCKING mode, which would
	// undo the non-blocking flag the drain depends on (measured — see
	// setNotificationPipe).
	winchPipeReadFD  int
	winchPipeWriteFD int

	// The wake pipe is the editor's own notification channel, and the queue for
	// a publish that comes from another goroutine: whoever publishes (the REPL's
	// status refresh, the completion worker) signals it, and its read end is
	// polled alongside stdin, so a publish INTERRUPTS a blocked read instead of
	// waiting for the next keystroke. It is optional — a terminal opened without
	// one keeps a no-op sink that SAYS so (NotifyEnabled), so the caller can
	// announce the degradation rather than assume the publish was seen.
	wake     *wakePipe
	wakeSink Notifier
	// inputFD is stdin's raw descriptor, captured ONCE at Open — the same
	// reason the pipe descriptors below are (a later (*os.File).Fd() would force
	// the descriptor back into BLOCKING mode). It exists for the non-blocking
	// readiness probe the editor's ordering gate asks before it draws a publish
	// (InputPending).
	inputFD  int
	winchSig chan os.Signal
	stopSig  chan struct{}
	sigOnce  sync.Once
	// winchDone is closed by the SIGWINCH watcher as it returns, so Close can
	// JOIN it instead of assuming a closed stop channel has already been
	// observed. The distinction is measured, not theoretical: a stop that only
	// closes the channel leaves the watcher runnable for a scheduling quantum,
	// which is long enough for a close-out sweep sampling this process to report
	// it as parked work the run never ended (Rule 15 — an exit that is merely
	// ASKED for is not an exit).
	winchDone chan struct{}
	// stopped is the terminal's STOP PATH, raised by Close before anything is
	// torn down. ReadEvent consults it at entry and after every poll, so a read
	// parked in poll(2) — and a read that has not begun — both end at the next
	// observation. Closing a descriptor does NOT stop a reader: the poll returns
	// POLLNVAL and a loop that only inspects Revents spins there for ever
	// (measured: after Close, ReadEvent returned POLLNVAL=32 on the closed pipe
	// descriptors in every round, and the editor's read loop never left Run).
	stopped atomic.Bool

	// The keyboard-protocol negotiation (keyprotocol.go). keyboard is the
	// verdict the caller reports, keyboardFlags the value the terminal answered
	// with, and keyboardPushed whether OUR `CSI > 1 u` is still on the terminal's
	// stack (so the pop in Close is symmetric with the push and never fires
	// twice).
	//
	// The prefetch queue they fill is NOT declared here: master already owns ONE
	// replay buffer (`prefetch` + `probeMu`, the colour probe's — see the field
	// block below), and the keyboard probe appends to that same queue through
	// addPrefetch. One queue means the two probes cannot disagree about which
	// byte arrived first, which is the whole ordering claim ReadEvent makes.
	keyboard       KeyboardProtocol
	keyboardFlags  int
	keyboardPushed bool

	// A TOUR of the terminal's signal handling, because three handlers now share
	// one descriptor and each has a different job — the budget is spent on NOT
	// writing a fourth one:
	//
	//   · SIGWINCH (winchSig/stopSig, startWinch) — a resize becomes a readable
	//     event on the winch self-pipe.
	//   · SIGINT (sigCh/sigStop/sigHandler, CaptureSignals) — the FATAL path: the
	//     attributes are restored and the signal is re-raised, for a process about
	//     to die. A registered handler that is INSTALLED but has no target (see
	//     SetInterruptHandler) is what turns this into a per-turn interrupt.
	//   · nothing else may signal.Notify this descriptor: Go delivers to every
	//     registered channel, so each new registration would wake paths that have
	//     no use for the signal.
	sigTarget atomic.Pointer[func() bool]

	// THE CAPABILITY PROBE (probe.go). An answering terminal is a terminal that
	// writes bytes INTO this descriptor, so a probe is an act on the input path
	// and its leftovers are input (see the file header for the whole argument).
	//
	// probeOnce makes the probe a ONE-SHOT per terminal: a second burst of
	// queries would be answered by a terminal that ignored the first no
	// differently, and a run that probed per turn would interrupt the operator
	// once per turn.
	probeOnce sync.Once
	// measured is what the probe read back — the frozen answer, so a caller (the
	// capability decision, a dump record) asks the terminal exactly once.
	measured CapabilityMeasurement
	// prefetch holds the bytes the probe read but did not claim as answers: the
	// operator's own keystrokes, arriving while the terminal replied. It is
	// REPLAYED in order by ReadEvent before the descriptor is touched, so a key
	// pressed during the probe reaches the decoder exactly as typed — never
	// reordered and never dropped (probe.go's rule 2).
	prefetch []byte
	// probeMu guards prefetch, which is written by the probe and read by the
	// read loop. In production both run on the same goroutine (the probe runs at
	// Open, before the editor's read loop exists), so the lock is what makes that
	// a property of the code rather than of the wiring.
	probeMu sync.Mutex
}

// Open puts in into raw mode (saving the snapshot first) and starts watching
// SIGWINCH. It fails with ErrNotTerminal when in is not a terminal.
func Open(in *os.File, out io.Writer) (*Terminal, error) {
	if in == nil {
		return nil, ErrNotTerminal
	}
	fd := int(in.Fd())
	if fd < fdMinValid {
		return nil, ErrNotTerminal
	}
	snap, err := readTermios(fd)
	if err != nil {
		return nil, fmt.Errorf("lineedit: read termios on fd %d: %w", fd, errors.Join(ErrNotTerminal, err))
	}
	t := &Terminal{in: in, out: out, snapshot: snap, inputFD: fd}
	if err := writeTermios(fd, rawState(snap)); err != nil {
		return nil, fmt.Errorf("lineedit: enter raw mode on fd %d: %w", fd, err)
	}
	if err := t.startWinch(); err != nil {
		// The terminal is already raw: undo it before reporting, otherwise a
		// failed open leaves the user's shell in raw mode.
		restoreErr := writeTermios(fd, t.snapshot)
		if restoreErr != nil {
			// Both errors matter (the watcher failure AND the failed restore);
			// errors.Join keeps BOTH in the chain so errors.Is/As can match
			// either one.
			return nil, fmt.Errorf("lineedit: start resize watcher: %w", errors.Join(err, restoreErr))
		}
		return nil, fmt.Errorf("lineedit: start resize watcher: %w", err)
	}
	t.write(escBracketedOn)
	// The keyboard-protocol negotiation, BEFORE the caller can read a byte: it
	// is what makes Shift+Enter arrive as `CSI 13;2u` instead of the same 0x0d a
	// bare Enter sends. It is bounded and it never eats a keystroke — see
	// keyprotocol.go. A terminal that does not answer costs one window here and
	// is REPORTED through KeyboardProtocol() rather than silently skipped.
	t.keyboardProtocolProbe()
	// The wake pipe is part of opening a terminal, not an optional extra: the
	// editor needs it to draw a status row refreshed from another goroutine
	// (the M2 wiring). A failure is not fatal — the terminal still works — but
	// it is ANNOUNCED and it is not silent: the sink reports NotifyEnabled()
	// false and the chat layer says the row updates on the next keystroke.
	p, err := newWakePipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] editor: %v — the status row will refresh on the next keystroke\n", err)
		t.wakeSink = noNotify{}
		return t, nil
	}
	t.wake = p
	t.wakeSink = wakeNotify{t: t}
	return t, nil
}

// WakeNotifier returns the seam a publisher uses to wake a blocked read. It is
// never nil: without a wake pipe it returns a sink whose NotifyEnabled() is
// false, so the caller can announce the degradation instead of assuming the
// publish reached the screen.
//
// The name is deliberately not "Notify": the Notifier interface has a Notify
// METHOD, and a *Terminal carrying both would be a compile-time collision — the
// kind of thing that silently makes the type assertion in the editor miss.
func (t *Terminal) WakeNotifier() Notifier { return t.wakeSink }

// Compile-time proof that a terminal can be the editor's wake source.
var _ Notifier = (*wakeSink)(nil)

// rawState derives the raw-mode attribute set from a snapshot: every field is
// carried over and only the flags/control characters raw mode owns are changed,
// so nothing the terminal had configured is invented.
func rawState(snap termiosState) termiosState {
	next := snap
	next.raw.Iflag &^= rawInputFlags
	next.raw.Oflag &^= rawOutputFlags
	next.raw.Lflag &^= rawLocalFlags
	next.raw.Cflag &^= rawControlFlags
	next.raw.Cflag |= rawControlValue
	next.raw.Cc[unix.VMIN] = rawVMIN
	next.raw.Cc[unix.VTIME] = rawVTIME
	return next
}

// startWinch builds the resize self-pipe and the SIGWINCH watcher: the handler
// goroutine writes one byte into the pipe, and ReadEvent polls the pipe's read
// end alongside stdin. A pipe is used rather than relying on EINTR because a
// Go-installed signal handler carries SA_RESTART, so the kernel would restart
// the blocking poll and the resize would be lost until the next keystroke.
func (t *Terminal) startWinch() error {
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create resize pipe: %w", err)
	}
	// The self-pipe must be non-blocking on BOTH ends: the reader drains it
	// until empty (drainWinchPipe), which a blocking read can never observe,
	// and the SIGWINCH goroutine below must never park on a full pipe. The
	// descriptors are captured once here, BEFORE any Fd() call can reset them.
	readFD, writeFD, err := setNotificationPipe(r, w)
	if err != nil {
		_ = r.Close() // deliberately ignored: reporting the setup failure is what matters
		_ = w.Close() // deliberately ignored: as above
		return err
	}
	t.winchPipeR, t.winchPipeW = r, w
	t.winchPipeReadFD, t.winchPipeWriteFD = readFD, writeFD
	t.winchSig = make(chan os.Signal, winchPipeBytes)
	t.stopSig = make(chan struct{})
	t.winchDone = make(chan struct{})
	signal.Notify(t.winchSig, syscall.SIGWINCH)
	go func() {
		defer close(t.winchDone)
		for {
			select {
			case <-t.stopSig:
				return
			case <-t.winchSig:
				// deliberately ignored: a full pipe already holds an unread
				// resize notification, which is all the reader needs.
				_, _ = unix.Write(t.winchPipeWriteFD, []byte{0})
			}
		}
	}()
	return nil
}

// Write sends bytes to the terminal output.
func (t *Terminal) Write(p []byte) (int, error) { return t.out.Write(p) }

// write is the internal write helper: terminal escape sequences are best
// effort, and a failed render must never abort an editing session (the user's
// input is unaffected either way).
func (t *Terminal) write(s string) {
	_, _ = io.WriteString(t.out, s) // deliberately ignored: best-effort render, input is unaffected
}

// flush pushes the terminal's output buffer, if the writer has one. The probe
// NEEDS it: its queries are the one write whose answer is a TIMEOUT-bounded
// wait, so a query still sitting in a buffered writer at the moment the probe
// starts reading is a query the terminal never saw. One type assertion, so the
// probe works with a buffered sink (production: the paint stream) and with an
// unbuffered one (a test's buffer, a pipe) without a flush method on every
// writer.
func (t *Terminal) flush() {
	if f, ok := t.out.(interface{ Flush() error }); ok {
		_ = f.Flush() //stylegate:ignore the render path's own discipline — a failed flush surfaces at the next write, and the probe must not fail on it
	}
}

// Height returns the current terminal row count, falling back to fallback when
// the kernel reports none.
func (t *Terminal) Height(fallback int) int {
	if ws := t.Size(); ws.Rows > 0 {
		return int(ws.Rows)
	}
	return fallback
}

// Width returns the current terminal width in columns, falling back to fallback
// when the kernel cannot report a size.
func (t *Terminal) Width(fallback int) int {
	w := int(t.Size().Cols)
	if w <= 0 {
		return fallback
	}
	return w
}

// Size reads the terminal geometry; a zero value means "unknown".
func (t *Terminal) Size() winSize {
	ws, err := unix.IoctlGetWinsize(int(t.in.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return winSize{}
	}
	return winSize{Rows: ws.Row, Cols: ws.Col, X: ws.Xpixel, Y: ws.Ypixel}
}

// ReadEvent waits up to timeoutMS milliseconds (-1 blocks) for input or a
// resize. A timeout returns Event{Kind: EventNone} with no error — the editor
// uses it to resolve a suppressed paste Enter, which is a real state change and
// not an error.
//
// When input and a publish wake are ready in the SAME round, INPUT is reported:
// the two are independent events, and this poll is the one place that can see
// both. Reporting the wake first let the read loop draw (and consume) a publish
// before reading a key that was already waiting, so a caller waiting on that
// draw observed an editor that had not consumed input already on the wire. The
// wake pipe is level-triggered and not drained on that path, so the publish is
// still reported as EventWake on the next round — never lost.
func (t *Terminal) InputPending() bool {
	if t.inputFD < fdMinValid {
		return false
	}
	// Bytes the probe read and did not consume are input the read loop has not
	// taken yet — the same answer the poll below gives for bytes still on the
	// wire, and the ordering gate that asks this question must see them (see
	// keyprotocol.go).
	if len(t.prefetch) > 0 {
		return true
	}
	fds := []unix.PollFd{{Fd: int32(t.inputFD), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, inputReadyTimeoutMS)
	if err != nil {
		// A failed probe is "no input to report", not a read error: the read
		// loop's own read still surfaces any real failure (Rule 15 — the only
		// thing a false positive here could cost is one extra read attempt).
		return false
	}
	return n > 0 && fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0
}

func (t *Terminal) ReadEvent(buf []byte, timeoutMS int) (Event, error) {
	if len(buf) == 0 {
		return Event{}, nil
	}
	// THE PREFETCH COMES FIRST. It holds bytes that arrived BEFORE anything
	// still on the wire — the operator's own keystrokes, read and not consumed
	// by a capability probe during Open (the palette probe on master, the
	// keyboard-protocol probe added by keyprotocol.go) — so replaying it after a
	// fresh read would REORDER the operator's own keys. It is drained without
	// polling, because its bytes are already in hand, and it is drained through
	// the ONE helper (takePrefetch) so a probe that adds to the queue and the
	// reader that empties it cannot disagree about arrival order.
	if n := t.takePrefetch(buf); n > 0 {
		return Event{Kind: EventInput, Bytes: n}, nil
	}
	// THE STOP IS OBSERVED AT ENTRY, before anything is polled: a read that
	// begins after Close would otherwise park on descriptors that are being
	// torn down and report POLLNVAL for ever (see Close).
	if t.stopped.Load() {
		return t.closeEvent(), nil
	}
	fds := []unix.PollFd{
		{Fd: int32(t.inputFD), Events: unix.POLLIN},
		{Fd: int32(t.winchPipeReadFD), Events: unix.POLLIN},
	}
	// The wake pipe joins the poll set only when it exists: its read end is
	// where a publish from another goroutine (the REPL's status refresh, the
	// completion worker) lands, and polling it is what makes the publish
	// INTERRUPT a blocked read instead of waiting for the next keystroke.
	wakeIdx := -1
	if t.wake != nil {
		wakeIdx = len(fds)
		fds = append(fds, unix.PollFd{Fd: int32(t.wake.readFD), Events: unix.POLLIN})
	}
	for {
		// A STOP RAISED WHILE THIS READ WAS PARKED IS OBSERVED HERE, before any
		// event is classified. It is checked after the poll (rather than only at
		// entry) because the park is unbounded in production: the stop has to end
		// a read that is ALREADY waiting, not only one that starts later.
		if t.stopped.Load() {
			return t.closeEvent(), nil
		}
		n, err := unix.Poll(fds, timeoutMS)
		if err != nil {
			if err == unix.EINTR {
				return Event{Kind: EventNone}, nil
			}
			return Event{}, fmt.Errorf("lineedit: poll stdin: %w", err)
		}
		if t.stopped.Load() {
			return t.closeEvent(), nil
		}
		if n == 0 {
			return Event{Kind: EventNone}, nil
		}
		// A DESCRIPTOR THAT IS NO LONGER OPEN ENDS THE READ. poll(2) reports
		// POLLNVAL for a closed fd and does NOT set any of POLLIN/POLLHUP, so a
		// loop that inspects only those two re-polls for ever on a dead
		// descriptor — the spin measured after Close, before this arm existed.
		// It is reported as the same terminal-is-gone event as the stop itself,
		// so a reader that somehow misses the flag still ends.
		if closedPollFd(fds) {
			return t.closeEvent(), nil
		}
		if fds[1].Revents&unix.POLLIN != 0 {
			t.drainWinchPipe()
			return Event{Kind: EventWinch}, nil
		}
		// INPUT IS REPORTED BEFORE THE WAKE. A publish and a keystroke are
		// INDEPENDENT events and this poll can see both in one round; reporting
		// the wake first made the read loop draw (and consume) the publish
		// WITHOUT reading the key that was already waiting — so a caller that
		// waits for the draw (a test driving the editor, the status row drawn
		// one round early) observed an editor that had not yet consumed input
		// that was on the wire. Reading stdin first orders the two inside one
		// wake: the wake pipe is level-triggered and only drained below, so the
		// publish is never lost — its byte keeps POLLIN set and is reported as
		// an EventWake on the next round.
		if fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
			got, err := t.in.Read(buf)
			if err != nil {
				if err == io.EOF {
					return Event{Kind: EventInput, Bytes: got, EOF: true}, nil
				}
				if err == unix.EINTR {
					return Event{Kind: EventNone}, nil
				}
				return Event{}, fmt.Errorf("lineedit: read stdin: %w", err)
			}
			return Event{Kind: EventInput, Bytes: got}, nil
		}
		if wakeIdx >= 0 && fds[wakeIdx].Revents&unix.POLLIN != 0 {
			// A notification from inside the process (a status publish or
			// completion candidates). It carries no information beyond "look
			// again", so the pipe is simply drained — the read loop then asks
			// the editor what is pending.
			t.wake.drain()
			return Event{Kind: EventWake}, nil
		}
	}
}

// drainWinchPipe empties the resize pipe so the next poll blocks again.
func (t *Terminal) drainWinchPipe() { drainPipe(t.winchPipeReadFD) }

// closedPollFd reports whether any descriptor of a poll set is no longer open.
// poll(2) answers POLLNVAL for a closed fd, and POLLNVAL is NOT POLLIN/POLLHUP:
// a loop that inspects only the latter two re-polls a dead descriptor for ever
// (measured after Close, before ReadEvent consulted the stop).
func closedPollFd(fds []unix.PollFd) bool {
	for i := range fds {
		if fds[i].Revents&unix.POLLNVAL != 0 {
			return true
		}
	}
	return false
}

// takePrefetch moves up to len(buf) prefetched bytes into buf, in arrival
// order, and reports how many it moved. It is the ONE reader of t.prefetch, and
// ReadEvent calls it before it polls — see the ordering argument there.
func (t *Terminal) takePrefetch(buf []byte) int {
	t.probeMu.Lock()
	defer t.probeMu.Unlock()
	if len(t.prefetch) == 0 {
		return 0
	}
	n := copy(buf, t.prefetch)
	t.prefetch = t.prefetch[n:]
	if len(t.prefetch) == 0 {
		t.prefetch = nil
	}
	return n
}

// Prefetched reports how many bytes the capability probe read that were not its
// own answers and are still waiting to be replayed as input. It exists so the
// probe's price is MEASURABLE from outside (a test asserting no keystroke is
// lost reads it) rather than being an implementation detail a reader has to
// trust.
func (t *Terminal) Prefetched() int {
	t.probeMu.Lock()
	defer t.probeMu.Unlock()
	return len(t.prefetch)
}

// addPrefetch appends bytes to the replay buffer. It is the probe's writer (the
// probe runs before the read loop, so the lock is about the property, not about
// a race that exists today — see Terminal.probeMu).
func (t *Terminal) addPrefetch(p []byte) {
	if len(p) == 0 {
		return
	}
	t.probeMu.Lock()
	defer t.probeMu.Unlock()
	t.prefetch = append(t.prefetch, p...)
}

// drainPipe empties a notification pipe so the next poll blocks again.
//
// It loops until the pipe reports "nothing left", which requires the read end
// to be NON-BLOCKING (setNotificationPipe). The emptiness of a pipe cannot be
// observed on a blocking descriptor: the read that would prove it empty parks
// instead, so the loop never returns and the reader is stuck forever. That is
// not hypothetical — it is how the real-terminal path froze on its first
// keystroke while every scripted-source test stayed green (the scripted tests
// never touch the real notification pipe). With O_NONBLOCK a read with no data
// left returns EAGAIN (n < 0, err != nil), which is this loop's exit condition.
//
// The fd is taken as an int, never as *os.File: Go's (*os.File).Fd() forces the
// descriptor back into BLOCKING mode, which would silently undo the flag this
// loop depends on (measured — F_GETFL came back 0 right after Fd()).
func drainPipe(fd int) {
	if fd < 0 {
		return
	}
	var scratch [winchPipeBytes]byte
	for {
		n, err := unix.Read(fd, scratch[:])
		if n <= 0 || err != nil {
			// EAGAIN (now empty) and any real error both end the drain: the
			// notification's only content is "look again", and the next poll
			// re-reports readiness if a byte really is pending.
			return
		}
	}
}

// Close restores the saved attributes and stops the resize watcher. It is
// idempotent, so a deferred Close plus an explicit one on a signal path is
// safe. Bracketed-paste mode is turned off FIRST, so the bytes that follow are
// read by a cooked terminal.
//
// CLOSING THE DESCRIPTORS IS NOT A STOP PATH, and that is the whole reason
// `stopped` and the wake below exist. A reader parked in poll(2) on a
// descriptor that is closed underneath it does not fail: poll reports POLLNVAL
// and returns, the loop inspects only POLLIN/POLLHUP, finds nothing, and polls
// again — for ever. Measured before the fix (2026-10-02): after Close,
// ReadEvent answered `n=1 winch=32` (POLLNVAL on the closed winch pipe) in
// round after round while the editor's read loop stayed in Run, so the run
// never ended. A stop is therefore stated BEFORE the teardown, and the parked
// read is WOKEN by the notification seam it is already polling, so it observes
// the statement at once instead of at the next keystroke.
func (t *Terminal) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	// The keyboard-protocol stack is popped FIRST among the escape writes, and
	// BEFORE the termios snapshot is restored: the pop is what tells the terminal
	// to stop disambiguating key events, and a terminal that kept it would report
	// the operator's next program's keys in a shape that program may not know.
	// It reuses THIS one restore path (Close) rather than a second teardown, so
	// every exit — a normal return, a deferred Close, a panic, and the fatal
	// signal path in CaptureSignals (which calls Close) — restores it.
	t.releaseKeyboardProtocol()
	// THE STOP IS RAISED FIRST, and the parked read is woken through the seam
	// it watches (the wake pipe; the resize pipe when there is no wake pipe) —
	// both are still open here, which is why this happens before the teardown
	// below closes them.
	t.stopped.Store(true)
	t.wake.signal()
	if t.winchPipeW != nil {
		// deliberately ignored: a full resize pipe already holds an unread
		// notification, and either one is enough to end the park.
		_, _ = unix.Write(t.winchPipeWriteFD, []byte{0})
	}
	t.write(escBracketedOff)
	t.write(escCursorShow)
	t.stopWinchWatch()
	if err := writeTermios(int(t.in.Fd()), t.snapshot); err != nil {
		return fmt.Errorf("lineedit: restore termios on fd %d: %w", int(t.in.Fd()), err)
	}
	return nil
}

// stopWinchWatch tears the signal watcher and the pipe down exactly once, and
// JOINS the watcher goroutine before returning.
//
// The join is the half a closed stop channel cannot provide: the watcher is
// only ASKED to return, and a caller that samples the process in the window
// between the close and the return sees work it never ended. That window is not
// hypothetical — this package's own close-out sweep reported the watcher as
// parked on a run whose test had already returned.
func (t *Terminal) stopWinchWatch() {
	t.sigOnce.Do(func() {
		signal.Stop(t.winchSig)
		close(t.stopSig)
		if t.winchDone != nil {
			<-t.winchDone
		}
		t.wake.close()
		if t.winchPipeR != nil {
			// deliberately ignored: the reader is being torn down; a close
			// error on a pipe we own cannot make the terminal raw.
			_ = t.winchPipeR.Close()
		}
		if t.winchPipeW != nil {
			// deliberately ignored: as above.
			_ = t.winchPipeW.Close()
		}
	})
}

// Stopped reports whether Close has raised the terminal's stop path. It exists
// so a caller (a test, the read loop's own source) can ASK rather than assume,
// and so the stop is observable from outside the read.
func (t *Terminal) Stopped() bool { return t.stopped.Load() }

// closeEvent is the read outcome of a stopped terminal: input ended because the
// terminal was closed. It is reported as EOF rather than as an error, because
// that is what it is — the same thing the caller sees when the pty hangs up —
// and a read error would be indistinguishable from a broken descriptor.
func (t *Terminal) closeEvent() Event { return Event{Kind: EventInput, EOF: true} }

// CaptureSignals restores the terminal before the process dies on a fatal
// signal: the handler restores the snapshot and then re-raises the signal with
// the default disposition, so the exit status still reflects the cause. Without
// this a SIGTERM (or a SIGINT arriving while the editor owns the terminal)
// leaves a raw, echo-less terminal behind. The returned function unsubscribes.
func (t *Terminal) CaptureSignals(sigs ...os.Signal) func() {
	ch := make(chan os.Signal, winchPipeBytes)
	signal.Notify(ch, sigs...)
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case sig := <-ch:
			// A PER-TURN HANDLER WINS HERE (see SetInterruptHandler). When the
			// owner has told this terminal what a SIGINT MEANS while a turn is
			// in flight, the signal is the operator's "abandon this turn" — not
			// a reason to die — and the handler decides which it is: it answers
			// true when it has CLAIMED the signal, i.e. when it cancelled a turn
			// that was really running. Only an unclaimed signal falls through to
			// the fatal path below.
			if fn := t.sigTarget.Load(); fn != nil && (*fn)() {
				return
			}
			// deliberately ignored: this is the fatal path. The terminal is
			// being restored as a last act; a failure here cannot be reported
			// to anyone (the process is about to die by design).
			_ = t.Close()
			// THIS channel is stopped FIRST and forever: with it still
			// registered, the re-raise below would be delivered straight back
			// into this (still running) goroutine and the process would never
			// die — the exact "unkillable" shape the re-raise exists to avoid.
			// This goroutine finishes the act immediately after, so there is
			// nothing left that wants the signal.
			signal.Stop(ch)
			lineeditFatalSignal(sig)
			if s, ok := sig.(syscall.Signal); ok {
				// deliberately ignored: re-raising the fatal signal is the
				// point; the process is expected to die here.
				_ = syscall.Kill(syscall.Getpid(), s)
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// lineeditFatalSignal is the last act of the fatal path, after the terminal has
// been restored: it re-delivers the signal and replaces the
// `signal.Reset(sigs...)` + self-kill this path used to run (PROJECT.md Rule 42).
//
// WHY NO `signal.Reset`. Reset is `cancel(sig, disableSignal)` over EVERY
// registered channel, so one component restoring "the" disposition DISMANTLES
// registrations it never installed — measured in this repository: a subprocess
// relying on os/signal lost its handler when another component reset, and
// internal/testutil/suiterootsignal.go records the run that hung 90s because of
// it. Two of the subscribers alive in a chat process at this moment are NOT this
// package's to remove:
//
//	cmd/agent/cli_chat.go's watcher  — signal.Notify over chatGracefulSignals,
//	                                   registered for the WHOLE run, and (per
//	                                   lineedit_wire.go's own note) BEFORE the
//	                                   input layer is built;
//	internal/signalrec               — the recorder main.go installs at startup.
//
// This package stops its OWN registration in the caller (`signal.Stop(ch)` in
// CaptureSignals' unsubscribe) and states the division here: the recorder
// releases its own through its one entry point, and the chat watcher is released
// by whoever installed it.
//
// SO THE RE-RAISE MAY STILL BE RECEIVED BY A LIVE SUBSCRIBER, and that is stated
// rather than hidden, because this package CANNOT enumerate the peers: the
// os/signal peer set is unreadable from outside the package (measured — //go:
// linkname runtime.sig and os/signal.handlers are both refused on go1.24, and
// /proc/self/status SigCgt/SigIgn and signal.Ignored read identically with and
// without a second subscriber; see suiterootsignal.go's header). Two outcomes
// remain, and both END the run:
//
//	nothing left subscribed  → the kernel's default action, i.e. the status the
//	                           caller of this terminal asked for (exit -N);
//	a live subscriber        → that subscriber receives the raise. In chat that
//	                           is the REPL watcher, which closes interruptCh and
//	                           unwinds through its normal close-out (session_end,
//	                           exit 1) — the graceful ending
//	                           testsuite/chat_pty_sigint_prompt_test.go measures.
//	                           Strictly better than the crash the old Reset path
//	                           produced (exit -2, ZERO session_end).
//
// What is NOT acceptable is silence, so a raise that leaves this process running
// says so by name (Rule 15b), naming the mechanism that swallowed it.
func lineeditFatalSignal(sig os.Signal) {
	_ = signalrec.ReleaseSignalHandlers()
	s, ok := sig.(syscall.Signal)
	if !ok {
		return
	}
	// deliberately ignored: re-raising the fatal signal is the point; the
	// process is expected to die here.
	_ = syscall.Kill(syscall.Getpid(), s)
	lineeditReportSurvivedFatal(sig)
}

// lineeditReportSurvivedFatal is reached only when the re-raise did not end this
// process within lineeditSurviveWait — i.e. a subscription this package cannot
// remove received it. It is a wait plus an honest sentence: the operator must
// learn which mechanism is ending the run, not be left with a process that
// "should" have died and a terminal already out of raw mode.
func lineeditReportSurvivedFatal(sig os.Signal) {
	time.Sleep(lineeditSurviveWait)
	fmt.Fprintf(os.Stderr, "lineedit: re-raised %v and this process is still running %s later — a signal "+
		"subscriber this package cannot remove received it (the REPL's watcher or internal/signalrec), so "+
		"the run ends through that subscriber rather than by the default action\n", sig, lineeditSurviveWait)
}

// SetInterruptHandler installs what a terminating signal MEANS while this
// terminal is up: fn is called on a captured signal and answers whether it
// CLAIMED it — true means "handled, the process lives", false means "not mine,
// fall through to the fatal re-raise".
//
// WHY THIS EXISTS AT ALL (measured, task 01a0e4390). A raw-mode terminal clears
// ISIG, so Ctrl+C arrives as the 0x03 BYTE and never becomes a signal: the
// editor's key decoder sees it, classifies KeyInterrupt, and — outside a turn —
// Run returns Interrupted, which the chat REPL treats as "end the session". The
// same hardware key therefore has to mean two things depending on whether a turn
// is in flight, and the only party that knows is the REPL. It states that
// meaning HERE, in one call, at the moment it knows:
//
//   - the key path (the 0x03 byte) is decided by the editor on the read loop,
//     which already knows whether its caller is mid-turn (MinTurnActive, below);
//   - a real SIGINT (a process-group deliverable signal, and the ONLY path when
//     the terminal was left cooked because raw mode was unavailable) lands in
//     CaptureSignals, which asks fn before it dies.
//
// The signal registration itself is NEVER removed: it is what gives the chat
// REPL ownership of SIGINT, so signalrec (which re-raises for any mode that owns
// no handler) leaves the process alone and the per-turn interrupt can work at
// all (see signalrec.modesThatOwnTheirSignalHandlers, "chat").
func (t *Terminal) SetInterruptHandler(fn func() bool) {
	if fn == nil {
		t.sigTarget.Store(nil)
		return
	}
	t.sigTarget.Store(&fn)
}
