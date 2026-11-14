// chat_replay_beats.go — the BEAT PROTOCOL for a live `gopi chat --replay` run:
// the same ledger + per-beat dump + acknowledgement handshake
// scripts/replay/replay.sh already speaks to `gopi replay`.
//
// WHY THIS EXISTS RATHER THAN A SECOND DRIVER. scripts/replay/replay.sh films a
// session by launching a launcher, then reading <scratch>/beats.tsv, grabbing the
// window once per beat and answering <scratch>/ack; it ends on <scratch>/done.txt.
// That protocol was defined by internal/replay (a RENDERER that re-draws the
// recording). The whole point of this verb is that the drawing must be the REAL
// chat surface — so the film has to keep the protocol and lose the second
// renderer, and this file is the adapter: it emits beats at the seams of the
// PRODUCTION render path (a typed chunk, an answer delta, a tool-activity row,
// the end of a turn) instead of at a re-drawer's steps.
//
// IT DOES NOT DRIVE THE RUN. The turns are still typed by chatReplay.run through
// the editor's own injector, and the answers still arrive from the stub through
// ag.OnText. This file only WATCHES those seams, settles the dump, writes the row,
// and waits for the driver. A beat therefore cannot be emitted for something the
// surface did not draw: it is emitted where the draw happened.
//
// THE REDACTION PASS RUNS HERE, upstream of the surface (chat_replay_redact.go):
// the text this driver hands to the ledger is the text it handed to the surface,
// because the mask is applied BEFORE either — at the seam, to the bytes the
// renderer is about to draw. internal/replay owns the shapes, the mask, the log
// format and the log's name (Rule 14), so the film's producer and the renderer's
// producer withhold the same runs.
//
// THE COLUMN FORMAT IS NOT RE-DECLARED. The ledger's header, its six columns, the
// preview cap, the ellipsis and the per-beat dump file name all come from
// internal/replay's exported constants, and the ledger itself is written by that
// package's own Writer — one definition of the format (Rule 14), so a change there
// cannot leave this emitter writing a file the driver no longer parses.

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/replay"
)

// The flag vocabulary of the beat protocol on the chat surface. The SCRATCH is
// the only required word: the rest have defaults that match `gopi replay`'s, so a
// launcher that passes nothing but --scratch still films.
const (
	// chatFlagScratch is the switch naming the directory holding beats.tsv, ack,
	// done.txt and dumps/. It is the SAME directory name `gopi replay --scratch`
	// takes, because the driver passes the same value to whichever of the two it
	// launched.
	chatFlagScratch = "--scratch"
	// chatFlagCols / chatFlagRows state the geometry the layout must be installed
	// at. They are the SAME two numbers the launcher passes to `gopi replay`, and
	// the run REFUSES a terminal that disagrees with them: a film drawn at a
	// different width is a different picture, and the driver's legibility floor
	// was computed for the width it asked for.
	chatFlagCols = "--cols"
	chatFlagRows = "--rows"
	// chatFlagMaxBeats bounds the run's beat count, so a long recording cannot
	// render for ever. The film then holds at most this many frames.
	chatFlagMaxBeats = "--max-beats"
	// chatFlagAckTimeoutMS bounds the wait for one acknowledgement.
	chatFlagAckTimeoutMS = "--ack-timeout-ms"

	// chatBeatDefaultCols / chatBeatDefaultRows were a SECOND copy of the film's
	// designed geometry (117x26) beside the replay driver's own pair. They are
	// gone: the value is the chat surface's ideal geometry now (one knob,
	// config.ChatIdealGeometry), read by chatBeatIdealCols/Rows below — so the
	// beat driver and the film it feeds cannot hold two ideas of the canvas.
	// `cmd/agent/chat_film.go` reads the SAME accessors for the geometry it forces
	// onto the producer, so the driver and the surface agree by construction.
	chatBeatDefaultMaxBeats = 200
	// chatBeatDefaultAckTimeoutMS bounds one beat's wait for the driver.
	chatBeatDefaultAckTimeoutMS = 30000

	// chatBeatTurnEndText is the chrome beat that CLOSES a turn, and it is the
	// same word internal/replay writes: tools/replay/verify.py groups the ledger's
	// rows into turns by it, so a different spelling would make every beat belong
	// to one turn.
	chatBeatTurnEndText = "turn end"
	// chatBeatKindRequest / Answer / ToolCall / Chrome are the ledger's kind
	// values, spelled as internal/replay spells them (the checker switches on
	// them).
	chatBeatKindRequest  = "request"
	chatBeatKindAnswer   = "answer"
	chatBeatKindToolCall = "tool_call"
	chatBeatKindChrome   = "chrome"

	// chatBeatTurnEndWaitMS bounds the wait for a turn's own answer stream to go
	// quiet before the driver types the next turn. It is the ONE place this
	// emitter waits on a CONDITION it cannot observe directly, and the bound is
	// what keeps that from being a hang: the run then reports the shortfall and
	// finishes (see chatReplayBeatDriver.settleTurn).
	chatBeatTurnEndWaitMS = 4000
	// chatBeatQuietMS is how long the seam must be silent for the turn to read as
	// over. Both writers — the answer deltas and the injected keys — touch the
	// seam, so "nothing has happened for this long" is the honest end condition
	// for a turn whose final beat is this emitter's own.
	chatBeatQuietMS = 250
	// chatBeatPollMS is the poll interval of both waits.
	chatBeatPollMS = 10
)

// chatBeatFlag is one parsed word of the beat protocol's vocabulary.
type chatBeatFlag struct {
	scratch string
	cols    int
	rows    int
	// colsSet / rowsSet record that the CALLER stated that dimension, rather
	// than the field holding the default the parser filled in. `0` cannot say
	// it (Rule 15a): every value here is a positive cell count, so a stated
	// dimension and a defaulted one would be indistinguishable — and the film
	// driver must know the difference, because a stated size wins over the
	// recording's own terminal size while a defaulted one does not.
	colsSet      bool
	rowsSet      bool
	maxBeats     int
	ackTimeoutMS int
}

// chatBeatOwnedWords are the words this vocabulary CONSUMES, so the REPL's argv
// walk must not then refuse them as unknown. It is derived from the same
// constants the parser matches (Rule 14: one list, two readers).
func chatBeatOwnedWords() []string {
	return []string{chatFlagScratch, chatFlagCols, chatFlagRows, chatFlagMaxBeats, chatFlagAckTimeoutMS}
}

// chatBeatFlagWord reports whether a word belongs to this vocabulary (in either
// spelling: `--scratch X` and `--scratch=X`).
func chatBeatFlagWord(a string) bool {
	for _, w := range chatBeatOwnedWords() {
		if a == w || strings.HasPrefix(a, w+"=") {
			return true
		}
	}
	return false
}

// parseChatBeatFlags consumes the beat protocol's words from an argv slice and
// returns the rest. It is deliberately SEPARATE from the REPL's own walk and runs
// FIRST: the REPL's vocabulary is closed (an unknown `-`-word is a flag error),
// so a word this verb has to accept would otherwise be refused before it is read.
func parseChatBeatFlags(args []string) (chatBeatFlag, []string, error) {
	f := chatBeatFlag{
		cols:         chatBeatIdealCols(),
		rows:         chatBeatIdealRows(),
		maxBeats:     chatBeatDefaultMaxBeats,
		ackTimeoutMS: chatBeatDefaultAckTimeoutMS,
	}
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		key, val := a, ""
		if k := strings.Index(a, "="); k > 0 {
			key, val = a[:k], a[k+1:]
		}
		if !chatBeatFlagWord(a) {
			rest = append(rest, a)
			continue
		}
		if val == "" {
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("%s needs a value", key)
			}
			if strings.HasPrefix(args[i+1], "-") {
				// A USAGE QUERY IN THIS WORD'S VALUE SLOT IS NOT A MISSING VALUE.
				// `gopi chat --scratch -h` is the shape the REPL's own walk answers
				// (chatUsageQueryWord): the operator asked to read the page and
				// named the flag they were reading about — "needs a value" would
				// refuse the question, which is the recorded C5 defect one word
				// over. The query word is left where it is, so the argv walk reads
				// it and prints the page, and THIS word is ignored exactly as every
				// other switch beside a `-h` is (`--tools -h` has always answered
				// the page and run nothing): no value is invented for it and it is
				// not remembered as stated, because the usage query means nothing
				// below it runs.
				if chatUsageQueryWord(args[i+1]) {
					continue
				}
				return f, nil, fmt.Errorf("%s needs a value", key)
			}
			i++
			val = args[i]
		}
		switch key {
		case chatFlagScratch:
			f.scratch = val
		case chatFlagCols:
			n, err := chatBeatPositiveInt(key, val)
			if err != nil {
				return f, nil, err
			}
			f.cols = n
			f.colsSet = true
		case chatFlagRows:
			n, err := chatBeatPositiveInt(key, val)
			if err != nil {
				return f, nil, err
			}
			f.rows = n
			f.rowsSet = true
		case chatFlagMaxBeats:
			n, err := chatBeatPositiveInt(key, val)
			if err != nil {
				return f, nil, err
			}
			f.maxBeats = n
		case chatFlagAckTimeoutMS:
			n, err := chatBeatPositiveInt(key, val)
			if err != nil {
				return f, nil, err
			}
			f.ackTimeoutMS = n
		}
	}
	return f, rest, nil
}

// chatBeatPositiveInt parses a strictly positive integer flag value. Zero and
// negative values are REFUSED rather than clamped: each of these numbers is a
// bound or a geometry, and a silent substitution would make the run's own record
// of what it was asked for a lie (Rule 15).
func chatBeatPositiveInt(name, val string) (int, error) {
	n := 0
	if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
		return 0, fmt.Errorf("%s wants an integer, got %q", name, val)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s wants a positive value, got %d", name, n)
	}
	return n, nil
}

// chatReplayBeatDriver emits the beat protocol for a live chat run. It is nil for
// a run that asked for no scratch, and every method is nil-safe, so the seams it
// hangs on can call it unconditionally.
type chatReplayBeatDriver struct {
	ledger *replay.LedgerWriter
	dir    string

	// red is the run's OWN redaction pass (chat_replay_redact.go): the ONE
	// definition internal/replay exports, applied to the text the SURFACE is
	// about to be handed — so the ledger's row is the film's text, not a second
	// account of it. It is written to under d.mu (every emitter holds it), so the
	// log's rows are ordered against the beats they belong to.
	red *replay.Redactor
	// pending is the answer stream's un-emitted tail: the runes at the end of
	// what has arrived that could still grow into an identifying shape. See
	// maskAnswerStream (chat_replay_redact.go) — emitting them now would paint
	// the head of an identifier that the next fragment completes.
	pending string

	// input is the run's own line layer: the sink each beat settles and copies.
	// Held here for the whole run, because the emitters are the REPLAY's own
	// seams (they already hold it) and this field would otherwise be threaded
	// through four closures for one reader.
	input *chatInput

	cols         int
	rows         int
	maxBeats     int
	ackTimeout   time.Duration
	ackTimeoutMS int
	ackPolls     int

	beats int
	// turnOrdinal is the count of turns whose end beat has been written.
	turnOrdinal int
	// shortfall counts turns whose end beat was emitted on the timeout arm
	// instead of on quiescence; it is REPORTED at the end, never swallowed.
	shortfall int
	// lastActivity is the seam's own clock: every emit and every typed key moves
	// it, and a turn is over when it has not moved for chatBeatQuietMS.
	lastActivity time.Time
	// mu serialises the two producers (see emit) around the beat NUMBER, the
	// ledger row and the acknowledgement. It is held across the ack wait on
	// purpose: the protocol admits exactly one unacknowledged beat.
	mu sync.Mutex
	// first is the run's clock origin, used only by the closing report.
	first time.Time
	// stopped is set when the beat bound is reached, so the seams become no-ops
	// instead of refusing (a run at its bound still finishes; it just stops
	// drawing into the film).
	stopped bool
}

// startChatReplayBeats resolves the protocol's files and opens the ledger. A
// scratch that cannot be written is a REFUSAL, not a degraded run: a film whose
// driver is polling a directory nobody writes would end in a timeout that names
// nothing.
//
// input is the run's line layer, and it is REQUIRED with a scratch: every beat's
// grid comes from that layer's sink, so a protocol run with no layer has nothing
// to photograph.
func startChatReplayBeats(f chatBeatFlag, input *chatInput) (*chatReplayBeatDriver, error) {
	if f.scratch == "" {
		return nil, nil
	}
	info, err := os.Stat(f.scratch)
	if err != nil {
		return nil, fmt.Errorf("beat scratch %s: %w", f.scratch, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("beat scratch %s is not a directory", f.scratch)
	}
	if input == nil || !input.hasDumpSink() {
		return nil, errChatBeatNoSink
	}
	// The requested geometry is CHECKED against the layout's own — the number the
	// surface actually drew at, read from the installed band rather than from the
	// caller's word. A disagreement is a REFUSAL, not a warning: the driver's
	// legibility floor was computed for the width it asked for, and a film drawn at
	// another width is a different picture that would still be reported as this one
	// (measured: `gopi replay` asserts the same thing, for the same reason).
	if haveCols, haveRows := input.bandGeometry(); haveCols > 0 && (haveCols != f.cols || haveRows != f.rows) {
		return nil, fmt.Errorf("this terminal is %dx%d but the beat protocol was asked for %dx%d — start `gopi chat %s` at the geometry the driver films (the driver's legibility floor belongs to that width)",
			haveCols, haveRows, f.cols, f.rows, chatFlagReplay)
	}
	if err := os.MkdirAll(filepath.Join(f.scratch, replay.DumpDirName), 0o755); err != nil {
		return nil, fmt.Errorf("create the per-beat dump directory under %s: %w", f.scratch, err)
	}
	ledger, err := replay.NewLedgerWriter(filepath.Join(f.scratch, replay.LedgerName))
	if err != nil {
		return nil, err
	}
	// THE REDACTION LOG IS OPENED BEFORE THE FIRST BEAT, and a log that cannot be
	// opened is a REFUSAL (Rule 15): the whole point of the pass is that what it
	// withheld is recorded, and a run that could not record it must not silently
	// draw the identifier instead — which is exactly what the chat route did
	// before this pass existed (tools/replay/verify.py's fourth self-check caught
	// it: "no redaction log at <scratch>/redactions.tsv").
	red, redErr := replay.NewRedactor(filepath.Join(f.scratch, replay.RedactionLogName))
	if redErr != nil {
		return nil, redErr
	}
	d := &chatReplayBeatDriver{
		ledger:       ledger,
		red:          red,
		input:        input,
		dir:          f.scratch,
		cols:         f.cols,
		rows:         f.rows,
		maxBeats:     f.maxBeats,
		ackTimeout:   time.Duration(f.ackTimeoutMS) * time.Millisecond,
		ackTimeoutMS: f.ackTimeoutMS,
		ackPolls:     0,
		lastActivity: time.Now(),
		first:        time.Now(),
	}
	// THE LINE LAYER'S OWN ROWS GO THROUGH THE SAME PASS. Not every identifying
	// run is recorded content: the `chat_surface` arming notice and the /help
	// panel are composed AT DRAW TIME and can carry this run's own session
	// directory, and they land in the dumped grids the film's checker greps. The
	// layer takes a FUNCTION rather than the driver, so it stays ignorant of what
	// a beat protocol is; this is the ONE place it is armed, and it is armed
	// after the driver exists because the function must take the driver's own
	// lock (the log's rows are ordered against the beats they belong to).
	if input != nil {
		input.redact = d.maskRow
	}
	return d, nil
}

// geometry reports the layout geometry the protocol requires, and whether one was
// stated at all (a run without a scratch states none and keeps the terminal's own).
func (d *chatReplayBeatDriver) geometry() (cols, rows int, wanted bool) {
	if d == nil {
		return 0, 0, false
	}
	return d.cols, d.rows, true
}

// turnsClosed reports how many turns this run has closed. It is the REPLAY
// DRIVER's turn-over witness: the REPL closes a turn once that turn's own screen
// is final, which is the only honest moment for the next turn's keys to be typed.
func (d *chatReplayBeatDriver) turnsClosed() int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.turnOrdinal
}

// touch records seam activity. It is called by the emitters AND by the key
// injector, so the quiet test sees both roads into the surface.
//
// It is an alias of the locked form rather than a lock-then-call, because emit
// already holds the mutex: the plain spelling was a SELF-DEADLOCK (measured
// 2026-10-03, the run hung on its first beat with the ledger holding one row
// nobody acknowledged).
func (d *chatReplayBeatDriver) touch() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.touchLocked()
}

// touchLocked moves the seam's clock. The caller holds d.mu.
func (d *chatReplayBeatDriver) touchLocked() { d.lastActivity = time.Now() }

// emit draws one beat: settle the dump, copy it beside the ledger, append the row,
// flush it, then BLOCK until the driver acknowledges this beat number.
//
// IT IS SERIALISED BY A MUTEX, and the mutex is load-bearing rather than
// defensive (measured 2026-10-03, the first end-to-end run of this bridge): a
// replay has TWO producers on two goroutines — the run's turn goroutine emits the
// typed chunks, the answers and the turn's own end, while the replay driver's
// goroutine emits the next turn's keys as soon as the turn boundary is visible.
// Without the lock both read the same `beats` value and TWO ROWS WERE NUMBERED
// 11: one beat number was lost, the driver's frame count came up one short of the
// ledger's, and the whole film was refused by replay.sh's frame/beat equality
// check — a half-written protocol that looked like a renderer defect.
func (d *chatReplayBeatDriver) emit(kind, text string, line int) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.emitLocked(kind, text, line)
}

// emitLocked is emit with the mutex already held, so the redaction pass and the
// row it produced cannot be interleaved with another producer's.
func (d *chatReplayBeatDriver) emitLocked(kind, text string, line int) error {
	if d.stopped {
		return nil
	}
	d.touchLocked()
	if d.beats >= d.maxBeats {
		d.stopped = true
		return nil
	}
	d.beats++
	snap := fmt.Sprintf(replay.DumpBeatFormat, d.beats)
	if err := d.settleAndCopy(snap); err != nil {
		return err
	}
	if err := d.ledger.Write(d.beats, kind, line, int(time.Since(d.first).Milliseconds()), text); err != nil {
		return err
	}
	return d.waitAck(d.beats)
}

// settleAndCopy installs the CURRENT painted frame as the dump's state and copies
// it to <scratch>/dumps/beat-NNNN.dump, so the driver photographs the same screen
// this beat describes. The order is the protocol's: the copy happens BEFORE the
// acknowledgement, because the driver grabs the window the moment it reads the row.
//
// The sink is this run's OWN chatInput, held by the driver because the emitters
// live in the seams (a field would have to be threaded through the REPL's
// closures for no other reader). A run with no sink REFUSES the protocol at start
// time rather than skipping silently here: a ledger whose beats carry no grid is a
// film of nothing, and the driver would hang waiting for frames that were never
// written (Rule 15).
func (d *chatReplayBeatDriver) settleAndCopy(snap string) error {
	if d.input == nil {
		return fmt.Errorf("the beat protocol needs the run's screen dump sink — start `gopi chat %s <session>` with the sink armed", chatFlagReplay)
	}
	d.input.SettleDump()
	dst := filepath.Join(d.dir, replay.DumpDirName, snap)
	if err := d.input.CopyDumpTo(dst); err != nil {
		return err
	}
	return nil
}

// waitAck blocks until the driver acknowledges beat n. A timeout is an ERROR and
// stops the run: a beat that was never acknowledged has no frame behind it, so a
// film built from it would have frames and a ledger that disagree.
func (d *chatReplayBeatDriver) waitAck(n int) error {
	path := filepath.Join(d.dir, replay.AckName)
	deadline := time.Now().Add(d.ackTimeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if v, cerr := parseAckBeat(raw); cerr == nil && v >= n {
				return nil
			}
		}
		time.Sleep(chatBeatPollMS * time.Millisecond) //stylegate:allow-sleep polling the driver's acknowledgement of this beat
	}
	return fmt.Errorf("beat %d was never acknowledged within %dms (the driver stopped; see %s)", n, d.ackTimeoutMS, path)
}

// parseAckBeat reads the acknowledgement file's integer.
func parseAckBeat(raw []byte) (int, error) {
	n := 0
	_, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &n)
	return n, err
}

// finishTurn closes the current turn: it waits (bounded) for the seam to go quiet
// and writes the chrome beat that ENDS the turn. tools/replay/verify.py groups the
// ledger into turns by exactly this beat, and the driver's own frame loop needs the
// turn boundary to know the surface is between turns.
//
// A turn whose end is reached on the timeout arm is COUNTED and reported: the
// bound exists so a stalled stream cannot hang a terminal session, and a silent
// substitution of "we gave up" for "the turn ended" is the failure this counter
// removes.
func (d *chatReplayBeatDriver) finishTurn() error {
	if d == nil || d.stopped {
		return nil
	}
	quietDeadline := time.Now().Add(chatBeatTurnEndWaitMS * time.Millisecond)
	for time.Now().Before(quietDeadline) {
		d.mu.Lock()
		idle := time.Since(d.lastActivity)
		d.mu.Unlock()
		if idle >= chatBeatQuietMS*time.Millisecond {
			break
		}
		time.Sleep(chatBeatPollMS * time.Millisecond) //stylegate:allow-sleep waiting for the answer stream to go quiet before the turn is closed
	}
	quiet := time.Since(d.lastActivity) >= chatBeatQuietMS*time.Millisecond
	if !quiet {
		d.mu.Lock()
		d.shortfall++
		d.mu.Unlock()
	}
	if err := d.emit(chatBeatKindChrome, chatBeatTurnEndText, 0); err != nil {
		return err
	}
	// THE TURN COUNTER MOVES ONLY AFTER ITS BEAT IS ON DISK, and that ordering is
	// the whole turn-over contract: the replay driver types the next turn when
	// turnsClosed reports this turn — counted here, the counter would announce a
	// boundary one beat before the boundary existed, and the two producers would
	// draw the next turn's keys into the frame that still belongs to this one
	// (the race measured on 2026-10-03: two rows numbered 11, one a turn's end and
	// one the next turn's first keys).
	d.mu.Lock()
	d.turnOrdinal++
	d.mu.Unlock()
	return nil
}

// close writes the completion marker holding the run's own beat count, so the
// driver's frame loop ends on a fact the producer states rather than on the window
// closing (a terminal outlives the process inside it).
func (d *chatReplayBeatDriver) close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ledger != nil {
		if err := d.ledger.Close(); err != nil {
			return err
		}
	}
	// THE SUBSTITUTION LOG IS PERSISTED WITH THE LEDGER, and its own sentence is
	// printed: the file is what tools/replay/verify.py's positive control reads,
	// and a run that replaced nothing says so (see redactionSummary).
	logPath := filepath.Join(d.dir, replay.RedactionLogName)
	if err := d.closeRedaction(); err != nil {
		return err
	}
	if line := redactionSummary(d, logPath); line != "" {
		chatNoticef("%s", line)
	}
	path := filepath.Join(d.dir, replay.DoneName)
	if err := replay.WriteDone(path, d.beats); err != nil {
		return err
	}
	chatNoticef(chatReplayNotice+"done: %d beats over %s; the driver's ledger is %s\n",
		d.beats, time.Since(d.first).Round(time.Millisecond), filepath.Join(d.dir, replay.LedgerName))
	if d.shortfall > 0 {
		chatNoticef(chatReplayNotice+"%d turn(s) were closed on the %dms quiet timeout rather than on a quiet seam — their last beats may hold a partial answer\n",
			d.shortfall, chatBeatTurnEndWaitMS)
	}
	return nil
}

// errChatBeatNoSink is the refusal a run gets when the beat protocol is asked for
// on a surface that cannot be photographed: a ledger whose beats have no grid is a
// film of nothing, and the driver would hang waiting for frames nobody writes.
var errChatBeatNoSink = errors.New("the beat protocol needs the screen dump sink (--dump-screen) to photograph")
