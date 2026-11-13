package replayplay

// loop.go — THE PLAYER: keys in, frames out, and a terminal state that is always
// given back.
//
// WHY THE KEY MAPPING IS A PURE FUNCTION AND THE LOOP IS SEPARATE. The bindings
// ARE the feature, and a binding can only be checked by pressing it on a real
// terminal — but the DECISION ("this byte sequence means step back") can be
// checked without one. So ParseKey is a pure function over bytes (unit-tested,
// and the same function the loop calls) and Player.Run is the small loop that
// reads, decides, paints and sleeps. Nothing about the key set lives in the loop.
//
// THE RESTORE IS STRUCTURAL, not a promise. Session.Open takes the terminal
// (raw mode through internal/lineedit, alternate screen, hidden cursor) and
// Session.Close gives it back in the reverse order; the runner defers Close, and
// installs a SIGINT/SIGTERM handler that restores and then re-raises, because a
// default signal death would otherwise run no defer at all and leave the
// operator's shell in raw mode inside the alternate screen.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sgr"
	"github.com/nick/gopi/internal/signalrec"
)

// Key is one playback decision, decoded from a byte sequence.
type Key int

// The bindings. The set is the contract the help page and the driver test both
// read, so a binding added here reaches both by construction (Rule 13).
const (
	KeyNone Key = iota
	KeyPause
	KeyStepForward
	KeyStepBack
	KeyHome
	KeyEnd
	// KeyJumpEnd is `G`: the same destination as KeyEnd (the last turn, its
	// BOTTOM row), spelled a second way.
	//
	// The two exist because they answer the same question from two habits — a
	// viewer who reaches for a pager presses End, a viewer who reaches for vi
	// presses G — and the destination was previously UNREACHABLE rather than
	// merely inconvenient: End landed at the TOP of the last turn, so the last
	// rows of a long log could not be reached by any key at all (measured
	// 2026-11-01). KeyEnd and KeyJumpEnd therefore share ONE destination, and
	// ApplyKey routes both through endOfMaterial so they cannot drift apart.
	KeyJumpEnd
	KeyFaster
	KeySlower
	KeyHelp
	KeyQuit
	// KeyScrollUp / KeyScrollDown move the view INSIDE the current round a ROW
	// at a time — they are what the ↑/↓ arrows decode to. KeyPageUp/KeyPageDown
	// move the same view a PAGE at a time (PgUp/PgDn). The two exist because ONE
	// KEY ONE RESPONSIBILITY cuts both ways: the arrows and the page keys move
	// the SAME thing (the Scroll offset, which is why they share the field) but
	// by DIFFERENT amounts, and an amount is a decision the key carries.
	//
	// They exist at all because a round is one step and a round can be taller
	// than the screen: without them, the content past the bottom row of a long
	// round would be unreachable — the step keys would skip over it.
	KeyScrollUp
	KeyScrollDown
	KeyPageUp
	KeyPageDown
)

// The escape sequences the arrows / Home / End arrive as. A terminal in
// APPLICATION cursor mode sends the SS3 forms (ESC O …) instead of CSI (ESC [
// …), so both are decoded — a replay that only knew one would silently ignore
// the arrow keys in half the terminals on this host.
//
// Neither introducer is spelled here: the CSI one is internal/sgr's (the
// project's single source of the escape vocabulary, and a gate fails the build
// on a second spelling), and the SS3 one is the same introducer with SS3's
// single-character selector after it, composed from sgr's own bytes.
const (
	seqCSI = sgr.CSI
	seqSS3 = sgr.ESC + string(sgr.SS3Selector)
	// finalUp / finalDown are the VERTICAL arrows — the SCROLL pair. A round is
	// one step and can be taller than the screen, so the vertical pair moves the
	// VIEW a row at a time inside the round that is already on screen.
	finalUp   = 'A'
	finalDown = 'B'
	// finalRight / finalLeft are the HORIZONTAL arrows — the STEP pair, making
	// the same decision `n` and `p` make (which round is on screen).
	finalRight  = 'C'
	finalLeft   = 'D'
	finalHome   = 'H'
	finalEnd    = 'F'
	seqTildeEnd = "~"
)

// The tilde-form spellings (Home/End/PgUp/PgDn/arrows on terminals without the
// CSI H/F forms).
const (
	tildeHome     = "1"
	tildeEnd      = "4"
	tildeHomeAlt  = "7"
	tildeEndAlt   = "8"
	tildePgUp     = "5"
	tildePgDn     = "6"
	tildeNumberCt = 2
)

// ParseKey decodes one key press. seq is what arrived after a read: one byte for
// an ordinary key, a whole escape sequence for a special one. An empty or
// unrecognised sequence is KeyNone — an unbound key is never a silent action.
func ParseKey(seq []byte) Key {
	if len(seq) == 0 {
		return KeyNone
	}
	if seq[0] == escByte {
		if len(seq) == 1 {
			// A BARE escape is the quit key. It is only reached when no further
			// bytes followed within the sequence window (see readKey), so an
			// escape SEQUENCE is never mistaken for it.
			return KeyQuit
		}
		return parseEscape(seq)
	}
	if len(seq) > 1 {
		// A paste or a burst of typing: act on the FIRST byte only, so a burst
		// cannot be read as one unrecognised key.
		seq = seq[:1]
	}
	switch seq[0] {
	case keySpace:
		return KeyPause
	case keyN:
		return KeyStepForward
	case keyP:
		return KeyStepBack
	case keyPlus:
		return KeyFaster
	case keyMinus:
		return KeySlower
	case keyQuestion:
		return KeyHelp
	case keyJumpEnd, keyJumpEndLower:
		return KeyJumpEnd
	case keyQ, keyCtlC, keyCtlD:
		return KeyQuit
	}
	return KeyNone
}

// parseEscape decodes the tail of an escape sequence.
func parseEscape(seq []byte) Key {
	body, ok := trimEscapePrefix(seq)
	if !ok || len(body) == 0 {
		return KeyNone
	}
	switch body[0] {
	case finalUp:
		// ↑ scrolls the view UP a row inside the round already on screen. (It
		// used to be an alias of step forward — "a replay's frames advance DOWN
		// the timeline, so Up is forward" — which made one key do two jobs: it
		// skipped over the rest of a long round instead of showing it. The
		// arrows are now one key one responsibility: vertical = the view,
		// horizontal = the round.)
		return KeyScrollUp
	case finalDown:
		return KeyScrollDown
	case finalRight:
		return KeyStepForward
	case finalLeft:
		return KeyStepBack
	case finalHome:
		return KeyHome
	case finalEnd:
		return KeyEnd
	}
	if len(body) >= tildeNumberCt && body[len(body)-1] == seqTildeEnd[0] {
		switch string(body[:len(body)-1]) {
		case tildeHome, tildeHomeAlt:
			return KeyHome
		case tildeEnd, tildeEndAlt:
			return KeyEnd
		case tildePgUp:
			return KeyPageUp
		case tildePgDn:
			return KeyPageDown
		}
	}
	return KeyNone
}

// trimEscapePrefix strips the CSI / SS3 introducer.
func trimEscapePrefix(seq []byte) ([]byte, bool) {
	s := string(seq)
	switch {
	case len(s) >= len(seqCSI) && s[:len(seqCSI)] == seqCSI:
		return seq[len(seqCSI):], true
	case len(s) >= len(seqSS3) && s[:len(seqSS3)] == seqSS3:
		return seq[len(seqSS3):], true
	}
	return nil, false
}

// The single-byte keys, named so the switch above reads as the key map rather
// than as a table of numbers (Rule 1b).
const (
	escByte     = sgr.Esc
	keySpace    = ' '
	keyN        = 'n'
	keyP        = 'p'
	keyPlus     = '+'
	keyMinus    = '-'
	keyQuestion = '?'
	keyQ        = 'q'
	keyJumpEnd  = 'G'
	// keyJumpEndLower is the same binding in lower case: `G` is the mnemonic and
	// a viewer who never presses SHIFT must still reach the end of the material
	// (vim's own `G`/`g` are different commands; here they are one destination,
	// because this player has no `gg` and a key that sometimes means the END and
	// sometimes does nothing would be the worse ambiguity).
	keyJumpEndLower = 'g'
	keyCtlC         = 0x03
	keyCtlD         = 0x04
)

// The playback's own timing and speed bounds. They are constants because the
// driver test asserts the pause freeze against them.
const (
	// baseFrameInterval is how long one frame is shown at speed 1.
	baseFrameInterval = 400 * time.Millisecond
	// DefaultSpeed is what a playback starts at. It is IN the ladder below.
	DefaultSpeed = 1.0
	// keySequenceWindowMS is how long readKey waits for the rest of an escape
	// sequence before deciding a bare ESC was pressed. It bounds the ONLY place
	// the player can be late for its own frame timer.
	keySequenceWindowMS = 40
	// readBufferBytes is the read buffer one key press is decoded from.
	readBufferBytes = 64
)

// Session is the TERMINAL the player owns: raw mode (through internal/lineedit,
// which also gives us the SIGWINCH channel), the alternate screen, and a hidden
// cursor. Close gives every one of them back.
type Session struct {
	term    *lineedit.Terminal
	out     io.Writer
	closed  bool
	sigCh   chan os.Signal
	sigStop chan struct{}
	sigDone chan struct{}
}

// OpenSession takes the terminal: raw mode, the alternate screen, cursor hidden.
func OpenSession(in *os.File, out io.Writer) (*Session, error) {
	term, err := lineedit.Open(in, out)
	if err != nil {
		return nil, err
	}
	s := &Session{term: term, out: out}
	// The alternate screen is entered AFTER raw mode is in force, so a failure
	// in Open has not already swapped the operator's screen.
	s.write(altScreenOn + cursorHide)
	return s, nil
}

// The two terminal-state sequences the player owns. They are composed by
// internal/sgr (the project's ONE spelling of the escape vocabulary — a gate
// fails the build on a second one) and named here, so entry and exit cannot
// disagree about what was turned on.
var (
	altScreenOn  = sgr.AltScreenSeq(true)
	altScreenOff = sgr.AltScreenSeq(false)
	cursorHide   = sgr.CursorHideSeq()
)

// write is best effort: a render that fails must never abort a playback whose
// terminal state still has to be restored.
func (s *Session) write(str string) {
	_, _ = io.WriteString(s.out, str) // deliberately ignored: best-effort render; the restore path is what matters
}

// ReadKey reads one key. It blocks up to timeoutMS milliseconds (-1 = no
// timeout), assembling an escape sequence when the first byte is ESC.
func (s *Session) ReadKey(timeoutMS int) (Key, error) {
	buf := make([]byte, readBufferBytes)
	ev, err := s.term.ReadEvent(buf, timeoutMS)
	if err != nil {
		return KeyNone, err
	}
	switch ev.Kind {
	case lineedit.EventInput:
		n := ev.Bytes
		if n == 0 {
			return KeyNone, nil
		}
		seq := append([]byte(nil), buf[:n]...)
		if seq[0] == escByte && len(seq) == 1 {
			// Wait a short, bounded window for the rest of the sequence: this is
			// the one place a bare ESC and an arrow key are told apart.
			if extra, _ := s.term.ReadEvent(buf, keySequenceWindowMS); extra.Kind == lineedit.EventInput && extra.Bytes > 0 { //stylegate:ignore the bounded ESC window read: a read failure means no sequence followed, which is the bare-ESC branch this code is told apart from
				seq = append(seq, buf[:extra.Bytes]...)
			}
		}
		return ParseKey(seq), nil
	case lineedit.EventWinch:
		return KeyNone, nil
	}
	return KeyNone, nil
}

// Size reads the terminal's current geometry.
func (s *Session) Size(cols, rows int) (int, int) {
	c, r := s.term.Width(cols), s.term.Height(rows)
	if c <= 0 {
		c = cols
	}
	if r <= 0 {
		r = rows
	}
	return c, r
}

// Out is the sink frames are painted into.
func (s *Session) Out() io.Writer { return s.out }

// Close restores the terminal: cursor shown, alternate screen left, termios
// restored (lineedit's Close restores the attribute set it snapshotted).
func (s *Session) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.write(sgr.Seq(sgr.ResetParams()) + sgr.CursorShowSeq() + altScreenOff)
	return s.term.Close()
}

// WatchSignals restores the terminal on the recorder's TERMINATING set and then
// dies the way the signal asked. It returns a stop function the caller defers, so
// a normal exit joins the watcher instead of leaving it parked.
//
// It exists because a defer does NOT run when the default signal action kills
// the process: without this, ^C on a playback leaves the operator's shell in raw
// mode inside the alternate screen — the state this whole file is about.
//
// THE SET IS THE RECORDER'S, NOT A LOCAL LIST (epic 01a15bca). It used to be
// `SIGINT, SIGTERM` and then SIGHUP — which signalrec's watchedSignals registers
// for EVERY verb, replacing its kernel default — reached nobody: the recorder
// records it and declines to re-raise for a handler-owning mode, so a hangup
// during a playback neither restored the terminal nor ended the process, leaving
// the operator's shell in raw mode in the alternate screen with nothing on
// stderr. Reading the set from signalrec is what keeps this face from drifting
// behind the recorders's obligations.
func (s *Session) WatchSignals() func() {
	s.sigCh = make(chan os.Signal, 1)
	s.sigStop = make(chan struct{})
	s.sigDone = make(chan struct{})
	signal.Notify(s.sigCh, signalrec.GracefulStopSignals()...)
	go func() {
		defer close(s.sigDone)
		select {
		case sig := <-s.sigCh:
			_ = s.Close() //stylegate:ignore the signal goroutine stops the replay session before exiting on the signal; the process is about to go away
			if n, ok := sig.(syscall.Signal); ok {
				os.Exit(signalExitBase + int(n))
			}
			os.Exit(1)
		case <-s.sigStop:
		}
	}()
	return func() {
		signal.Stop(s.sigCh)
		close(s.sigStop)
		<-s.sigDone
	}
}

// signalExitBase is the conventional exit status for a process killed by signal
// N (128 + N).
const signalExitBase = 128

// Player drives one playback.
type Player struct {
	// Mat is the material being replayed.
	Mat Material
	// Cols / Rows are the canvas (kept in step with the terminal's own size).
	Cols, Rows int
	// Speed is the current playback rate.
	Speed float64
	// Palette / Truecolor are the render options.
	Palette   Palette
	Truecolor bool
	// Session is the terminal the player owns.
	Session *Session
	// RoundIndex is the 1-based TRANSPORT POSITION currently shown — the index
	// into Material.Turns (the operator's ruling, 2026-10-22), with RoundTotal
	// how many units the material has, so the status line's "k/n" names one
	// unit. A dump-shaped material (no turns at all) carries Material.Turns
	// empty (Material.PlayableUnits then counts its dump frames) and this field
	// is the FRAME position instead.
	//
	// The name is the player's own transport index, kept because every call
	// site reads it as one; what it COUNTS is the TURN, and the status line says
	// which ("turn 3/8", not "3/8" under a round word).
	RoundIndex int
	// Paused is the transport state.
	Paused bool
	// Scroll is how many rows the view is scrolled down inside the current
	// round (the ↑/↓ arrows a row at a time, PgUp/PgDn a page at a time).
	Scroll int
	// Help is true while the `?` overlay is up.
	Help bool
	// Notice is the one-line status message (a hint or a refusal).
	Notice string
	// finished records that the last round was reached with playback running:
	// the player then pauses itself and says so (never silently stops).
	finished bool
	// lastAdvance is when the previous round was shown, for the speed clock.
	lastAdvance time.Time
}

// NewPlayer builds a player for one material on one session.
func NewPlayer(mat Material, sess *Session, cols, rows int, truecolor bool) *Player {
	return &Player{
		Mat:        mat,
		Cols:       cols,
		Rows:       rows,
		Speed:      DefaultSpeed,
		Palette:    DefaultPalette(),
		Truecolor:  truecolor,
		Session:    sess,
		RoundIndex: 1,
	}
}

// RoundCount is how many units the material has (at least 1, so the status line
// never shows "0/0"). The unit IS the TURN when the material recorded turns
// (Material.PlayableUnits), and the dump's screen frames otherwise.
func (p *Player) RoundCount() int {
	if n := p.Mat.PlayableUnits(); n > 0 {
		return n
	}
	return 1
}

// ApplyKey performs one binding's effect and reports whether to quit.
func (p *Player) ApplyKey(k Key) bool {
	switch k {
	case KeyQuit:
		return true
	case KeyPause:
		p.Help = false
		p.Paused = !p.Paused
		if !p.Paused {
			p.finished = false
			p.Notice = ""
			p.lastAdvance = time.Now()
		}
	case KeyStepForward:
		p.Help = false
		p.Paused = true
		p.step(+1)
	case KeyStepBack:
		p.Help = false
		p.Paused = true
		p.step(-1)
	case KeyHome:
		p.Help = false
		p.Paused = true
		p.RoundIndex = 1
		p.Scroll = 0
		p.Notice = ""
	case KeyEnd, KeyJumpEnd:
		// BOTH END KEYS LAND ON THE MATERIAL'S LAST ROW — the last turn's BOTTOM,
		// reached through ONE function so the two bindings cannot drift.
		p.Help = false
		p.Paused = true
		p.endOfMaterial()
	case KeyScrollUp:
		p.Help = false
		p.scroll(-scrollRowDelta)
	case KeyScrollDown:
		p.Help = false
		p.scroll(+scrollRowDelta)
	case KeyPageUp:
		p.Help = false
		p.scroll(-p.scrollPage())
	case KeyPageDown:
		p.Help = false
		p.scroll(+p.scrollPage())
	case KeyFaster:
		p.Help = false
		p.Speed = nextSpeed(p.Speed, +1)
	case KeySlower:
		p.Help = false
		p.Speed = nextSpeed(p.Speed, -1)
	case KeyHelp:
		p.Help = !p.Help
	default:
		// ANY key returns from the overlay — the help text says so ("any key
		// returns"), and an unbound key is exactly the key an operator presses
		// to get out of a screen they did not mean to open. This is the one
		// place KeyNone is not a no-op, and it is why the default arm exists
		// rather than the case being silent.
		p.Help = false
	}
	return false
}

// endOfMaterial puts the view at the very END of the material: the last turn,
// scrolled to that turn's BOTTOM row. It is the ONE implementation both end keys
// (KeyEnd, KeyJumpEnd) route through.
//
// THE DEFECT IT REPLACES (measured 2026-11-01): End set `RoundIndex =
// RoundCount()` and `Scroll = 0` — the last turn's TOP. Nothing else moved the
// view to the material's end either, so on a long log the last rows were
// UNREACHABLE by any key: an operator had to step to the last turn and then press
// PgDn until the scroll clamps. "The end key goes to the top of the last page" is
// wrong by exactly one page, and that is the page a reader wants.
//
// The scroll offset is the LAST turn's own MaxScroll, read through the same
// renderOptions the renderer and the scroll keys use — never a computed guess, so
// "the bottom" means the same number everywhere (`p.Scroll ==
// p.renderOptions().MaxScroll()` is then true by construction, and the assertion
// is a measurement rather than a restatement).
func (p *Player) endOfMaterial() {
	p.RoundIndex = p.RoundCount()
	p.Scroll = 0
	p.Notice = ""
	// The maximum is measured at scroll 0 for THIS turn: renderOptions reads the
	// round at RoundIndex and MaxScroll is its row overflow minus the body height.
	if max := p.renderOptions().MaxScroll(); max > 0 {
		p.Scroll = max
	}
}

// scrollPage is how many rows one PgUp/PgDn moves: the height of the body it
// scrolls, so a page key moves exactly the content that was on screen.
func (p *Player) scrollPage() int {
	if n := bodyRows(p.Rows); n > 0 {
		return n
	}
	return scrollMinPage
}

// scrollMinPage is the page size when the canvas has no body rows at all (a
// terminal one row tall): a key that moved zero rows would look broken.
const scrollMinPage = 1

// scrollRowDelta is how many rows one ↑/↓ moves: ONE row, because that is the
// only amount that makes the vertical arrows a different key from PgUp/PgDn. A
// named constant rather than a bare 1 so the arrows' amount and the page keys'
// amount read as the two decisions they are (Rule 1).
const scrollRowDelta = 1

// scroll moves the view by delta rows inside the current round, clamping at
// both ends and SAYING so, because a key that appears to do nothing is
// indistinguishable from a broken key.
func (p *Player) scroll(delta int) {
	opts := p.renderOptions()
	private := opts
	private.Scroll = 0
	max := private.MaxScroll()
	next := p.Scroll + delta
	switch {
	case next < 0:
		p.Notice = atRoundStartNotice
		p.Scroll = 0
		return
	case next > max:
		p.Notice = atRoundEndNotice
		p.Scroll = max
		return
	}
	p.Scroll = next
	p.Notice = ""
}

// step moves one round in dir, saturating at the ends and SAYING so, because a
// key that appears to do nothing is indistinguishable from a broken key. A step
// also resets the in-round scroll: the new round starts at its top.
func (p *Player) step(dir int) {
	next := p.RoundIndex + dir
	switch {
	case next < 1:
		p.Notice = atStartNotice
		return
	case next > p.RoundCount():
		p.Notice = atEndNotice
		return
	}
	p.RoundIndex = next
	p.Scroll = 0
	p.Notice = ""
}

// speedLadder is the set of playback rates the +/- keys step through, and it is
// the ONE definition of the range: SpeedMin/SpeedMax are its ends, so a rate can
// never be reached that the ladder cannot come back from.
//
// A DISCRETE LADDER, NOT A MULTIPLIER (measured): with a multiplicative step, `+`
// held to the clamp (8x) then `-` repeated walks 8 → 5.33 → 3.56 → 2.37 → 1.58 →
// 1.05 → 0.70 — it never lands back on 1x, so the operator cannot return to the
// rate they started at. A ladder makes every step exactly reversible.
var speedLadder = []float64{0.25, 0.5, 1, 2, 4, 8}

// SpeedMin and SpeedMax are the ladder's ends, so the bounds and the steps are
// one value.
var (
	SpeedMin = speedLadder[0]
	SpeedMax = speedLadder[len(speedLadder)-1]
)

// nextSpeed steps one rung up (dir > 0) or down (dir < 0), saturating at the
// ends — the same saturation-says-so discipline the frame index uses.
func nextSpeed(cur float64, dir int) float64 {
	at := 0
	best := -1.0
	for i, v := range speedLadder {
		d := v - cur
		if d < 0 {
			d = -d
		}
		if best < 0 || d < best {
			at, best = i, d
		}
	}
	next := at + dir
	if next < 0 {
		next = 0
	}
	if next >= len(speedLadder) {
		next = len(speedLadder) - 1
	}
	return speedLadder[next]
}

// The two end-of-material notices and the two in-round scroll notices, so the
// driver test and the player agree on the words.
const (
	atStartNotice      = "(first turn)"
	atEndNotice        = "(last turn)"
	atRoundStartNotice = "(top of this round)"
	atRoundEndNotice   = "(bottom of this round)"
)

// interval is how long the current frame is shown at the current speed.
func (p *Player) interval() time.Duration {
	d := time.Duration(float64(baseFrameInterval) / p.Speed)
	if d < time.Millisecond {
		return time.Millisecond
	}
	return d
}

// Paint writes the current state (round, or the dump frame, or the help
// overlay) to the terminal.
func (p *Player) Paint() {
	paint := p.renderOptions()
	if _, err := io.WriteString(p.Session.Out(), Render(paint)); err != nil {
		// A sink that fails mid-playback (the window went away) is reported, and
		// the playback stops: continuing would only produce a screen nobody can
		// see while the terminal state still had to be restored.
		p.Notice = "output closed: " + err.Error()
		p.Paused = true
	}
}

// renderOptions is the ONE place the playback state becomes a RenderOptions, so
// what is painted and what the tests assert on are the same value.
func (p *Player) renderOptions() RenderOptions {
	opts := RenderOptions{
		RoundIndex: p.RoundIndex,
		RoundTotal: p.RoundCount(),
		Cols:       p.Cols,
		Rows:       p.Rows,
		Scroll:     p.Scroll,
		Paused:     p.Paused,
		Speed:      p.Speed,
		Source:     p.Mat.Source,
		SessionID:  p.Mat.SessionID,
		Notice:     p.Notice,
		Palette:    p.Palette,
		Truecolor:  p.Truecolor,
	}
	// The step's material: a TURN when the session log had turns (the step
	// unit), else the dump's own screen frame. The two are never mixed in one
	// value — the status line names which kind is on screen (`FrameKind`).
	if turn, ok := p.Mat.TurnAt(p.RoundIndex); ok {
		opts.Round = turn
	} else if i := p.RoundIndex - 1; i >= 0 && i < len(p.Mat.DumpFrames) {
		opts.DumpFrame = p.Mat.DumpFrames[i]
		opts.ScreenFrame = p.Mat.DumpFrames[i].Label
	}
	if opts.Round.Label != "" && p.Notice == "" {
		opts.Notice = opts.Round.Label
	}
	if opts.ScreenFrame != "" && p.Notice == "" {
		opts.Notice = opts.ScreenFrame
	}
	if p.Help {
		opts.Help = HelpLines()
	}
	return opts
}

// Run plays until a quit key. It paints, reads a key, advances on the speed
// clock, and repeats. It returns nil on a quit key and the read error otherwise;
// the TERMINAL is restored by the caller's defer, never here.
func (p *Player) Run() error {
	p.lastAdvance = time.Now()
	for {
		p.Paint()
		wait := p.interval()
		if p.Paused || p.Help {
			wait = 0
		}
		// A zero timeout is a poll: a paused playback must still see a key
		// immediately, and a playing one must not sleep past its next frame.
		timeoutMS := -1
		if !p.Paused && !p.Help {
			timeoutMS = int(wait / time.Millisecond)
		}
		k, err := p.Session.ReadKey(timeoutMS)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read a key: %w", err)
		}
		if quit := p.ApplyKey(k); quit {
			return nil
		}
		if p.Help || p.Paused {
			continue
		}
		if time.Since(p.lastAdvance) < p.interval() {
			continue
		}
		p.advance()
	}
}

// advance moves the playback one round on the speed clock, pausing at the end
// with a notice rather than looping or stopping silently.
func (p *Player) advance() {
	p.lastAdvance = time.Now()
	if p.RoundIndex >= p.RoundCount() {
		p.finished = true
		p.Paused = true
		p.Notice = endOfMaterialNotice
		return
	}
	p.RoundIndex++
	p.Scroll = 0
}

// endOfMaterialNotice is what the status line says when the last round is
// reached: the recording ended, and the transport stopped on its own.
const endOfMaterialNotice = "(end of recording — space to replay, p to step back)"
