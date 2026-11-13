// textcapture.go — the TEXT surface of a capture: a UI that draws with terminal
// escapes has no browser and no X11 window, so `gopi tool shot` reaches it the way a
// terminal does. A pseudo-terminal (the same /dev/ptmx pair the chat PTY suite
// uses) receives the child's byte stream, internal/lineedit's cell grid — a real
// VT interpreter (CSI/SGR, wide runes, scroll regions, scrollback) — turns that
// stream back into the rows a person would see, and internal/text2img paints
// those rows to a PNG.
//
// Why the grid and not the raw bytes: a TUI redraws in place. The byte stream
// contains every intermediate frame, cursor jumps and erase runs, so it is NOT
// what the screen shows. Only the interpreted grid is the screen, and a capture
// that skips the interpreter captures a log, not a screen.
//
// Zero dependencies (Rule 10.7): /dev/ptmx, the TIOCGPTN/TIOCSPTLCK/TIOCSWINSZ
// ioctls (internal/lineedit and golang.org/x/sys/unix, both already imported by
// the module) and os/exec.
package shot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"golang.org/x/sys/unix"
)

// Named bounds (Rule 1) for the text-surface path.
const (
	// TextSurfaceDefaults are the geometry a capture uses when the caller does
	// not size it: a terminal wide enough for the chat UI's editor rows and
	// tall enough for its status row plus a few turns.
	TextDefaultCols = 120
	TextDefaultRows = 40

	// textReadBuf is one pty drain's buffer size.
	textReadBuf = 4096

	// TextSettleDefault is the quiet window that ends a capture: with no new
	// byte for this long the UI has finished drawing. It is a SETTLE, not a
	// render delay — the wait returns the moment the screen is ready and the
	// child falls silent.
	TextSettleDefault = 700 * time.Millisecond

	// TextWaitDefault bounds the whole capture (ready marker + settle). A TUI
	// that never draws anything must fail loudly rather than hang.
	TextWaitDefault = 20 * time.Second

	// textKillGrace is how long the child gets to exit after the capture ends
	// before it is killed, and textWaitGrace bounds the final Wait.
	textKillGrace = 250 * time.Millisecond
	textWaitGrace = 3 * time.Second

	// textExitKeysGrace bounds the wait for a UI to honour the exit keys the
	// caller sent (a UI that does not end itself is killed, loudly).
	textExitKeysGrace = 3 * time.Second

	// maxExitSettle bounds the window that samples the grid after the exit keys
	// were typed. It must be long enough for a UI to repaint its answer to the
	// submitting Enter, and far too short for a teardown's screen wipe — which
	// arrives with the goodbye line, tens of milliseconds later — to land inside
	// it. That is what keeps an exit-key capture photographing the screen the
	// keys were typed on rather than the empty grid the ending leaves behind.
	maxExitSettle = 200 * time.Millisecond

	// TextReadyDefault is the TUI's own readiness marker: the chat prompt as it
	// appears on the INTERPRETED screen — the renderer trims a row's trailing
	// cells, so the marker is "you>", not the padded "you> " the prompt constant
	// carries. The CLI passes the real marker; this is only what a caller that
	// passes none gets.
	TextReadyDefault = "you>"

	// TextReadyNone is the opt-out a caller spells out when its UI prints ONE frame
	// and then waits forever: the capture then stops on the child's own exit or the
	// overall bound instead of on a quiet window after the marker.
	TextReadyNone = ""

	// textChunkQueue is how many pty reads may be buffered before the reader
	// blocks — a small queue so a slow UI cannot make the capture allocate
	// without bound.
	textChunkQueue = 64

	// textPollInterval is how often the capture loop re-checks the child state
	// while waiting for bytes.
	textPollInterval = 25 * time.Millisecond

	// ptyMasterPath is the pty multiplexer device (the single source for the
	// path — Rule 1b: never spelled inline again).
	ptyMasterPath = "/dev/ptmx"
	// ptySlaveDir is the directory pty slaves appear under; the slave path is
	// ptySlaveDir/<number from TIOCGPTN>.
	ptySlaveDir = "/dev/pts"
)

// textKeyGap separates the exit keys' characters from each other, and
// textEnterGap separates the terminating Enter from the last of them. Both are
// derived by REFERENCE from internal/lineedit's paste-burst policy rather than
// copied: the burst is armed by ONE READ that carries PasteBurstMinChars keys,
// so keys written one at a time with a gap above PasteBurstCharInterval are
// delivered as separate reads (typed characters) rather than as one pasted
// block, and an Enter held back by more than PasteEnterSuppressWindow cannot be
// suppressed by anything the block armed — exactly the trap a single "keys\r"
// write falls into.
const (
	textKeyGap   = lineedit.PasteBurstCharInterval * 4
	textEnterGap = lineedit.PasteEnterSuppressWindow * 2

	// textCarriageReturn is the submit key the capture sends after a UI's exit
	// verb. It is '\r' (not '\n'): a terminal's Enter is CR, and the editor's
	// submit path reads it that way.
	textCarriageReturn = "\r"
)

// TextCaptureOptions is one text-surface capture request.
type TextCaptureOptions struct {
	Argv       []string      // child command; empty → DefaultTUIArgv()
	Cols, Rows int           // winsize; 0 → the TextDefault geometry
	Settle     time.Duration // quiet window; 0 → TextSettleDefault
	Wait       time.Duration // overall bound; 0 → TextWaitDefault
	Ready      string        // substring the RENDERED screen must show first ("" → none)
	ExitKeys   string        // keys written once the screen is ready ("" → none)
	ExitWait   time.Duration // how long to wait for the child's own exit after ExitKeys
	Scrollback bool          // also collect the rows pushed off the top
}

// TextCapture is the captured screen.
type TextCapture struct {
	Lines      []string // screen rows, trailing blanks trimmed
	Scrollback []string // rows that scrolled off, oldest first (optional)
	ScreenText string   // Lines joined by "\n"
	Cols, Rows int      // the geometry the grid was interpreted at
	Bytes      int      // bytes received from the pty (diagnostics)
	Exited     bool     // the child exited on its own (vs was killed)
	ExitCode   int
	// FrameFromHistory reports that the engine fell back to a frame the UI drew
	// EARLIER, because the frame in hand at the end no longer showed the ready
	// marker — which is what a UI's own teardown leaves behind when it wipes the
	// display on exit (`\033[r\033[H\033[2J`, measured). It is a diagnostic, not a
	// substitute for a grid: Lines always holds a real frame.
	FrameFromHistory bool

	// frames is the exit keys' own record: the grid as it stood when the exit
	// keys were typed, appended at most once per capture.
	frames [][]string
}

// DefaultTUIArgv is the argv a bare `gopi tool shot --self` text capture runs: gopi's
// own chat UI, which is the "cli text UI" this surface exists for. The caller
// passes the binary path (the CLI knows how it was invoked).
func DefaultTUIArgv(binPath string) []string {
	return []string{binPath, cliVerbChat}
}

// CaptureTextSurface runs argv on a fresh pty, interprets the byte stream with
// internal/lineedit's cell grid, and returns the rows the screen ended up
// showing.
//
// Synchronisation is marker/settle driven: the caller names a string the
// rendered screen must contain (Ready) and the capture ends on the first quiet
// window afterwards — never after a fixed delay that hopes the UI finished.
func CaptureTextSurface(ctx context.Context, opts TextCaptureOptions) (*TextCapture, error) {
	cols, rows := opts.Cols, opts.Rows
	if cols <= 0 {
		cols = TextDefaultCols
	}
	if rows <= 0 {
		rows = TextDefaultRows
	}
	settle := opts.Settle
	if settle <= 0 {
		settle = TextSettleDefault
	}
	wait := opts.Wait
	if wait <= 0 {
		wait = TextWaitDefault
	}

	master, slavePath, err := openPtyPair()
	if err != nil {
		return nil, err
	}
	defer master.Close() //stylegate:ignore the pty master is torn down when the capture returns

	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("shot: text surface: open pty slave %s: %w", slavePath, err)
	}

	// The geometry is installed BEFORE the child starts, so the UI's very first
	// draw already uses the capture's size (installing it later would capture a
	// redraw at the default 80x24 and misreport what the screen looks like).
	if err := setPtyWinsize(int(master.Fd()), cols, rows); err != nil {
		slave.Close() //stylegate:ignore teardown on the error path below
		return nil, err
	}

	argv := opts.Argv
	if len(argv) == 0 {
		slave.Close() //stylegate:ignore teardown on the error path below
		return nil, errors.New("shot: text surface: no argv — pass the UI command to capture")
	}
	//stylegate:ignore the capture's own context cancellation is the parent's business
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Setsid + Setctty: the child owns the pty as its controlling terminal, so
	// the TUI's raw-mode/termios path runs exactly as it does for a user.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		slave.Close() //stylegate:ignore teardown on the error path below
		return nil, fmt.Errorf("shot: text surface: start %s: %w", argv[0], err)
	}
	// The parent's copy of the slave must go: while it is open the master never
	// reports EOF, so a finished child would look like a silent one.
	_ = slave.Close() //stylegate:ignore the child holds its own descriptor for the slave

	cap := &TextCapture{Cols: cols, Rows: rows}
	screen := lineedit.NewScreen(cols, rows)
	w := startChildWait(cmd)

	// settledFrame is the grid as of the last quiet window: the screen the UI had
	// FINISHED drawing. It is kept because an ending can erase everything after
	// the last good frame (see the fallback below).
	var settledFrame []string

	// How long past the opening of the exit keys' write the grid is sampled: a
	// UI that repaints the prompt on the submit key (the common case) gets a
	// chance to be seen, while a teardown's screen wipe — which arrives with the
	// goodbye line, tens of milliseconds later — is kept out.
	exitSettle := exitSettleFor(settle)

	if err := pumpPty(master, screen, cap, w, opts, settle, wait, exitSettle, &settledFrame); err != nil {
		killSession(w, false)
		return nil, err
	}

	cap.Lines = gridLines(screen)
	// The UI's own teardown wiped the display (measured: `\033[r\033[H\033[2J`
	// right before the goodbye line), so the grid in hand is the goodbye and a
	// screen whose content is GONE. When that happens the capture falls back to
	// the screen the UI had FINISHED drawing — the grid as of the last quiet
	// window, which is the engine's own definition of a complete frame. The
	// post-exit erase is a side effect of the ending, never the thing being
	// captured; a UI that ANSWERS the exit keys instead (its rows are all still
	// there) is captured answering them.
	settled := trimGridRows(settledFrame)
	if len(settled) > 0 && gridLostRows(settled, cap.Lines) {
		cap.Lines = settled
		cap.FrameFromHistory = true
	}
	cap.ScreenText = strings.Join(cap.Lines, "\n")
	if opts.Scrollback {
		cap.Scrollback = screen.Scrollback()
	}
	return cap, nil
}

// gridLostRows reports whether the rows the settled screen showed are GONE from
// the grid in hand: content the ending DESTROYED, as opposed to a UI that
// answered the exit keys and still shows everything it showed before.
//
// Row containment (not equality) is the test on purpose: a UI answering the keys
// rewrites a row in place — the prompt "you>" becomes "you> seen:[quit]" — and
// that row is still the screen it was. A wipe leaves nothing to contain it.
// Blank rows carry no content and are skipped.
func gridLostRows(settled, current []string) bool {
	for _, row := range settled {
		if strings.TrimSpace(row) == "" {
			continue
		}
		found := false
		for _, cur := range current {
			if strings.Contains(cur, row) {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// pumpPty reads the pty until the screen is ready and quiet, or the child exits,
// or the overall bound expires.
func pumpPty(master *os.File, screen *lineedit.Screen, cap *TextCapture, w *childWait,
	opts TextCaptureOptions, settle, wait, exitSettle time.Duration, settledFrame *[]string) error {
	chunks := make(chan []byte, textChunkQueue)
	readErr := make(chan error, 1)
	go readPty(master, chunks, readErr)

	deadline := time.Now().Add(wait)
	// lastByte is when the screen last changed; the quiet window is measured
	// from it, so a busy UI is never cut off mid-draw.
	var lastByte time.Time
	sawReady := opts.Ready == ""
	if sawReady {
		lastByte = time.Now()
	}
	exited := false
	for {
		if time.Now().After(deadline) {
			if !sawReady {
				return fmt.Errorf("shot: text surface: screen never showed %q within %s (%d bytes received; the UI may need a different --wait)",
					opts.Ready, wait, cap.Bytes)
			}
			break
		}

		if exited {
			break
		}

		select {
		case chunk, ok := <-chunks:
			if ok {
				screen.Feed(chunk)
				cap.Bytes += len(chunk)
				lastByte = time.Now()
				if !sawReady && strings.Contains(screen.Contents(), opts.Ready) {
					// The first frame that carries the marker starts the quiet
					// window: a UI that draws once and then waits must not be
					// read mid-frame.
					sawReady = true
					lastByte = time.Now()
				}
				continue
			}
			// Channel closed: the master reported EOF (the session is over).
			exited = true
		case err := <-readErr:
			if err != nil && !isPtyEOF(err) {
				return fmt.Errorf("shot: text surface: read pty: %w", err)
			}
			exited = true
		case <-time.After(textPollInterval):
		}

		if !exited && w.poll() {
			// The UI ended on its own. One more drain keeps its final frame.
			exited = true
		}
		if !exited && sawReady && !lastByte.IsZero() && time.Since(lastByte) >= settle {
			// The quiet window elapsed: the UI has finished drawing, so this is
			// the screen — recorded before anything can erase it.
			*settledFrame = gridRowStrings(screen)
			break
		}
	}

	if exited {
		drainUntilClosed(chunks, screen, cap, &lastByte, textWaitGrace)
		cap.Exited, cap.ExitCode = true, w.exitCode()
		return nil
	}

	if opts.ExitKeys != "" {
		if !sawReady {
			return fmt.Errorf("shot: text surface: refusing to send the exit keys before the screen showed %q — the UI was not ready", opts.Ready)
		}
		if _, err := writeTypedKeys(master, opts.ExitKeys); err != nil {
			return fmt.Errorf("shot: text surface: send exit keys: %w", err)
		}
		// The UI is asked to end itself; the grace it gets is the ExitWait knob
		// (default textExitKeysGrace), the same bound the settle path uses to let a
		// process unwind, and the screen is sampled for the exitSettle window
		// first so a UI that ANSWERS the keys is captured answering them.
		if exitSettle > 0 {
			drainFor(chunks, screen, cap, &lastByte, exitSettle)
		}
		exitWait := opts.ExitWait
		if exitWait <= 0 {
			exitWait = textExitKeysGrace
		}
		if w.block(exitWait) {
			drainUntilClosed(chunks, screen, cap, &lastByte, textWaitGrace)
			cap.Exited, cap.ExitCode = true, w.exitCode()
			return nil
		}
		killSession(w, true)
		return fmt.Errorf("shot: text surface: the UI did not exit within %s of the exit keys — killed", exitWait)
	}

	// A settle-ended capture stops the UI deliberately (nothing else should be
	// left running), and the capture reports the child as killed, not exited.
	killSession(w, false)
	return nil
}

// writeTypedKeys delivers the exit keys the way a person types them: each
// character separated by textKeyGap, with the final carriage return held back by
// textEnterGap. A single back-to-back write would be read as a PASTE, and a
// pasted Enter is deliberately suppressed by the editor (a newline inside
// pasted content is content, not a submit) — the UI would never end. Both gaps
// come from internal/lineedit's own burst policy, so this cannot drift from the
// rule it is working around.
func writeTypedKeys(master *os.File, keys string) (int, error) {
	written := 0
	runes := []rune(keys)
	for i, r := range runes {
		if i > 0 && r == '\r' {
			time.Sleep(textEnterGap) //stylegate:allow-sleep clears the editor's paste-burst window so the submit Enter is not suppressed
		} else if i > 0 {
			time.Sleep(textKeyGap) //stylegate:allow-sleep keeps the keys typed, not pasted
		}
		n, err := master.WriteString(string(r))
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// exitSettleFor derives the post-exit-keys sampling window from the capture's
// own quiet window: half of it, capped at maxExitSettle (see that constant for
// why the cap is what makes an exit-key capture photograph the right screen).
func exitSettleFor(settle time.Duration) time.Duration {
	out := settle / 2
	if out > maxExitSettle {
		out = maxExitSettle
	}
	return out
}

// drainFor consumes the pty for at most grace, feeding the screen and recording
// the last frame the UI drew. It is the answer-sampling drain: it exists so a UI
// that REPAINTS when the exit keys reach it is captured mid-answer, and it stops
// well before a teardown's wipe (see maxExitSettle).
func drainFor(chunks chan []byte, screen *lineedit.Screen, cap *TextCapture,
	lastByte *time.Time, grace time.Duration) {
	deadline := time.After(grace)
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return
			}
			screen.Feed(chunk)
			cap.Bytes += len(chunk)
			*lastByte = time.Now()
		case <-deadline:
			return
		}
	}
}

// drainUntilClosed keeps consuming the pty until the reader finishes (the master
// reports the session's end) or the bound expires.
//
// It is the AWAITED drain: once the UI has ended, its last frame — the goodbye
// line, the final status row — is still in flight, and a single non-blocking
// drain returns before the reader goroutine has delivered it, silently dropping
// the very rows a caller captures the screen for. Waiting for the reader to close
// is bounded because the pty ends when the last slave handle is gone.
func drainUntilClosed(chunks chan []byte, screen *lineedit.Screen, cap *TextCapture, lastByte *time.Time, grace time.Duration) {
	deadline := time.After(grace)
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return
			}
			screen.Feed(chunk)
			cap.Bytes += len(chunk)
			*lastByte = time.Now()
		case <-deadline:
			return
		}
	}
}

// childWait tracks a started child's termination without stealing its exit
// status: Wait runs once in a goroutine and the loop polls the resulting
// channel, so the capture learns BOTH that the UI ended and how it ended (a
// Wait4-based probe would reap the child and lose the code).
type childWait struct {
	done chan error
	err  error
	got  bool
	pid  int
}

// startChildWait begins the one Wait for cmd. It must be called exactly once per
// child.
func startChildWait(cmd *exec.Cmd) *childWait {
	w := &childWait{done: make(chan error, 1)}
	if cmd.Process != nil {
		w.pid = cmd.Process.Pid
	}
	go func() { w.done <- cmd.Wait() }()
	return w
}

// poll reports whether the child has exited, recording its status once.
func (w *childWait) poll() bool {
	if w.got {
		return true
	}
	select {
	case err := <-w.done:
		w.err, w.got = err, true
		return true
	default:
		return false
	}
}

// block waits up to d for the child to exit and reports whether it did.
func (w *childWait) block(d time.Duration) bool {
	if w.poll() {
		return true
	}
	select {
	case err := <-w.done:
		w.err, w.got = err, true
		return true
	case <-time.After(d):
		return false
	}
}

// exitCode is the child's exit status (0 when it ended without a code).
func (w *childWait) exitCode() int {
	if w.err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(w.err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// readPty copies the master into chunks until it fails, then closes chunks.
func readPty(master *os.File, chunks chan<- []byte, readErr chan<- error) {
	defer close(chunks)
	buf := make([]byte, textReadBuf)
	for {
		n, err := master.Read(buf)
		if n > 0 {
			out := make([]byte, n)
			copy(out, buf[:n])
			chunks <- out
		}
		if err != nil {
			readErr <- err
			return
		}
	}
}

// isPtyEOF reports whether a pty read error is the ordinary end of a session
// (EIO once the last slave handle is gone) rather than a real failure.
func isPtyEOF(err error) bool {
	return errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO) || errors.Is(err, unix.EIO)
}

// killSession ends the child and its process group (Setsid made it a session
// leader, so the group kill also reaches a TUI's spawned children).
// waitForExit says which ending is being forced: after exit keys were sent, the
// wait is for the UI to honour them; on the settle path there is nothing to wait
// for. Either way the kill is what ends the session.
func killSession(w *childWait, waitForExit bool) {
	if waitForExit && w.block(textKillGrace) {
		return
	}
	pid := w.pid
	if pid <= 0 {
		return
	}
	//stylegate:ignore the group kill is best effort; the direct kill below is authoritative
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(textKillGrace) //stylegate:allow-sleep gives the UI a moment to unwind before SIGKILL
	//stylegate:ignore as above — a process already gone is not an error here
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	//stylegate:ignore reaping a child that already exited reports ECHILD, which is fine
	_ = syscall.Kill(pid, syscall.SIGKILL)
	w.block(textWaitGrace)
}

// gridLines renders the screen's rows, trailing blanks trimmed — the rows as a
// person reads them (the grid is the capture's truth, not the byte stream).
func gridLines(screen *lineedit.Screen) []string {
	out := make([]string, screen.Rows)
	for i := range out {
		out[i] = screen.Line(i)
	}
	return trimGridRows(out)
}

// trimGridRows drops the grid's trailing blank rows.
//
// Blank BLOCKS inside the image are kept: the rows between a banner and a prompt
// are part of the layout, and folding them would renumber the screen. Blank
// INTERIOR rows are indistinguishable from unpainted ones in a cell grid (the
// parser's own documented blind spot), which is why the rule can only be stated
// about the tail.
func trimGridRows(rows []string) []string {
	// Trailing blank rows are not part of what the UI drew; dropping them keeps
	// the PNG from ending in a page of nothing.
	last := len(rows)
	for last > 0 && strings.TrimSpace(rows[last-1]) == "" {
		last--
	}
	return rows[:last]
}

// gridRowStrings snapshots a screen as plain rows — the form the settled frame
// is kept in, so it outlives the screen it came from.
func gridRowStrings(screen *lineedit.Screen) []string {
	out := make([]string, screen.Rows)
	for i := range out {
		out[i] = screen.Line(i)
	}
	return out
}

// openPtyPair opens a pty master and resolves its slave path. Both failures name
// the missing facility (Rule 15: a capture that cannot run must say why).
func openPtyPair() (*os.File, string, error) {
	master, err := os.OpenFile(ptyMasterPath, os.O_RDWR, 0)
	if err != nil {
		return nil, "", fmt.Errorf("shot: text surface: no pty available (%s): %w", ptyMasterPath, err)
	}
	fd := int(master.Fd())
	n, err := lineedit.PtyNumber(fd)
	if err != nil {
		master.Close() //stylegate:ignore teardown on the error path below
		return nil, "", fmt.Errorf("shot: text surface: TIOCGPTN unavailable on %s: %w", ptyMasterPath, err)
	}
	if err := lineedit.PtyUnlock(fd); err != nil {
		master.Close() //stylegate:ignore teardown on the error path below
		return nil, "", fmt.Errorf("shot: text surface: TIOCSPTLCK unavailable on %s: %w", ptyMasterPath, err)
	}
	return master, fmt.Sprintf("%s/%d", ptySlaveDir, n), nil
}

// setPtyWinsize installs the capture geometry on the pty, which is also how the
// child learns its terminal size (the kernel reports it on TIOCGWINSZ).
func setPtyWinsize(fd, cols, rows int) error {
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)}); err != nil {
		return fmt.Errorf("shot: text surface: set pty winsize %dx%d: %w", cols, rows, err)
	}
	return nil
}

// cliVerbChat is the chat subcommand's verb, read here for the default TUI argv.
//
// It is NOT a second definition of the CLI verb: cmd/agent owns the chat verb
// and this package cannot import package main, so the value is declared once in
// this package and the CLI passes its own argv explicitly for anything but the
// A test in cmd/agent asserts the two spellings agree (TestShotTextDefaultIsTheChatVerb).
const cliVerbChat = "chat" // surface-literal-ok: this is the CLI VERB "chat" (gopi chat), not the shot surface name
