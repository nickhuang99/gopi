package lineedit

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Editor drives one line of input end to end: read → decode → mutate → redraw,
// with the paste-burst classification in the middle. It is the seam the chat
// REPL calls ("give me one line").
//
// The terminal is reached only through terminalSource, so the policy — including
// the paste-burst decision that a newline inside a paste never submits — is
// exercised in tests with a scripted byte stream and a fake clock, throwing away
// nothing but the ioctl layer itself.
// (declared in raw.go beside *Terminal, which satisfies it.)

// Editor bounds. editReadBuf is one read(2)'s capacity; editColsFallback is the
// assumed width when the kernel reports none; editRowsFallback is the assumed
// height (a terminal that reports no geometry still gets the A5 layout).
const (
	editReadBuf      = readBufBytes
	editColsFallback = defaultEditorCols
	editRowsFallback = 24
	// escWaitMS bounds the wait for the rest of an escape sequence. It is the
	// only way to tell a LONE Escape (A7: close the completion menu) from the
	// prefix of an arrow key — the same ambiguity vim resolves with its
	// ttimeout. Short enough to feel immediate, long enough that a real
	// terminal's sequence (delivered in one read in practice) never loses its
	// first byte.
	escWaitMS = 30
	// completionPollMS bounds the read wait right after a completion request.
	// The completer runs on its own goroutine (A7), so its result arrives with
	// no keystroke to trigger a repaint; the bounded wait is how the editor
	// wakes up to draw the candidates. Without it the menu would only appear
	// after the NEXT key — i.e. typing "/" alone would show nothing. Short
	// enough that the menu feels immediate.
	completionPollMS = 15
	// completionFastPollWakes is how many fast checks one request gets before
	// the editor switches to the slow wait. A completer that has not answered
	// within that many polls (a few tens of ms) is not a fast one, so paying a
	// fast poll for it buys nothing but CPU.
	completionFastPollWakes = 20
	// completionBlockedMS is the read wait once the request has outlived the
	// fast checks — the shape of a completer that is BLOCKED (a stub holding
	// its answer, a cold filesystem, a busy host).
	//
	// It is deliberately NOT a give-up deadline. The editor keeps waiting while
	// the menu is undrawn, however long that takes, because a slow completer
	// must still get its menu drawn without the user typing again; an earlier
	// version spent a fixed budget and gave up, which left typing "/" against a
	// slow completer showing nothing at all until the next keystroke. A bounded
	// wait that is always RE-ARMED cannot spin — the read parks between wakes —
	// so waiting longer costs nothing, which is why this is a cadence and not a
	// budget.
	completionBlockedMS = 150
	// drawLevelPollMS bounds the read wait while a DRAW LEVEL is pending — a band
	// frame, a band note or an answer op the loop has not written yet — and no
	// other cadence applies.
	//
	// IT IS THE INVARIANT, not a tuning knob: the read loop's wait is normally
	// unbounded (`pollTimeoutBlock`), and that is right only while NOTHING is
	// owed. Every pending level is drawn by whoever re-derives it — the read
	// loop — so a level that is pending while the loop blocks for ever can only
	// be cleared by a wake, and a wake is an EDGE: `Terminal.ReadEvent` DRAINS
	// the notification pipe when it reports EventWake, so one that lands in the
	// park window (after the loop decided to block, before the read began) is
	// spent on a decision it could not inform. Nothing else would ever wake the
	// loop, and the only key that can reach a console in that state is the
	// operator's Ctrl+C — which then waits for ever instead of exiting
	// (measured: `chat-exit.marker` says `reason:"interrupt"`, `exit_code:0`,
	// `signal:""`, i.e. the 0x03 DID arrive, arbitrarily late).
	//
	// So: any pending level implies a BOUNDED wait. This constant is that bound.
	// It is a CADENCE and not a budget (the same reasoning as
	// completionBlockedMS): it is re-armed on every iteration while the level is
	// still pending, so a draw that takes longer is not given up on — the loop
	// comes back, re-derives the level, redraws. It must stay well under the
	// band wait budget so a FOREIGN caller's wait (bandWaitSteps, 4000ms) is
	// served many times over inside its own bound.
	drawLevelPollMS = 50
	// noWakePollMS bounds the read wait when a level has NO ROAD TO A PARKED
	// READ — i.e. `NotifyEnabled()` is false, the state a source with no wake
	// pipe leaves (BandNotifyMisses counts it, and a run announces it).
	//
	// THE TWO HALVES ARE DIFFERENT DEFECTS AND BOTH MUST BE CLOSED:
	//
	//   - drawLevelPollMS closes "a level was pending when the wait was decided".
	//     The loop can see that level and refuses to block on it.
	//   - THIS closes "a level is raised AFTER the wait was decided, by a
	//     producer whose notification cannot reach a parked read". The loop
	//     cannot see that future level, and there is no edge that could tell it,
	//     so the only honest answer is a cadence — this value, re-armed for
	//     ever, never a give-up budget. Then the loop comes back around on its own and
	//     the level is re-derived by the same code that draws it.
	//
	// The cost is bounded and mechanical — one read returning EventNone per
	// `noWakePollMS` while a run has no wake seam — and it is the price of a
	// degraded transport. What it buys is the invariant: NO LEVEL CAN BE
	// STARVED BY THE TRANSPORT IT WAS PUBLISHED THROUGH, so the operator's
	// Ctrl+C is always read within this bound instead of waiting for a wake that
	// the source cannot deliver.
	noWakePollMS = 100
)

// drawLevelPending is THE level the unbounded read is conditional on: every
// queue whose drainer is the read loop itself, and every producer-side flag a
// repaint consumes. It is ONE predicate because the answer decides ONE thing
// ("may the loop block here?"), and two spellings of it would let a level that
// one arm draws be invisible to the arm that decides whether to wait — which is
// exactly the stall this predicate closes.
//
// The list is deliberately the SAME set the EventWake arm repaints on
// (statusPending, menuNeedsDraw, bandPending, answerPending, lineInsertPending,
// draftPending): the question "is there something to draw?" has one answer in
// this package, and the read wait asks it rather than a shorter version of it.
//
// Completion is NOT in the list on purpose: it has its own cadence
// (completionWait) and its own long bound (completionBlockedMS), because a
// completer that is still computing owes the loop nothing yet.
func (e *Editor) drawLevelPending() bool {
	return e.statusPending() || e.menuNeedsDraw() || e.bandPending() || e.answerPending() ||
		e.lineInsertPending() || e.draftPending()
}

// The EOF-kind vocabulary (Result.EOFKind): WHICH event ended the input.
//
// The distinction exists because the two are different facts about the run and
// the record used to have neither (task 01a163f8). A key is the operator asking
// to stop; a closed source is the operator's PEER going away (a pipe's writer
// exiting, a terminal being torn down) and the run simply finding that out. Both
// reach the caller as EOF, and "the input ended" is not the same sentence as
// "the human pressed Ctrl+D" — the first is evidence about the world, the second
// about intent.
const (
	// EOFKindNone is the zero value: no end of input was reported.
	EOFKindNone = ""
	// EOFKindKey is Ctrl+D on an empty line — a human at the keyboard.
	EOFKindKey = "key"
	// EOFKindSource is the SOURCE ending under the reader: a closed stdin, a
	// pipe whose writer exited, a terminal that went away.
	EOFKindSource = "source"
)

// Result is one finished line of input.
type Result struct {
	// Line is the submitted text (may contain newlines: multiline entry and
	// pasted blocks are first-class).
	Line string
	// EOF reports Ctrl+D on an empty line (or a closed stdin).
	EOF bool
	// EOFKind says WHICH of those two it was (EOFKindKey / EOFKindSource), so
	// the caller can record the operator's intent instead of collapsing both
	// into "the input ended". Empty when EOF is false.
	EOFKind string
	// Interrupted reports Ctrl+C at the prompt.
	Interrupted bool
}

// Editor holds the per-run editing state.
type Editor struct {
	src Source
	// backlog is the source's optional input-readiness seam (inputBacklog).
	// Nil means the source does not report a backlog, and a publish is drawn
	// without asking whether input is waiting — the historic order, kept for a
	// source that cannot answer.
	backlog inputBacklog
	out     io.Writer
	prompt  string
	hist    *History
	dec     *Decoder
	val     *Value
	render  *Renderer
	now     func() time.Time

	// minTurnActive and interruptTurn are the caller's side of the turn-interrupt
	// seam: the predicate says whether a turn is in flight (either key then means
	// "abandon it"), the action says how and is told WHICH key claimed it, and it
	// reports whether it really claimed the key. Both nil (the zero value) keeps
	// every historic meaning — see SetMinTurnActive / SetInterruptTurn.
	minTurnActive func() bool
	interruptTurn func(KeyOrigin) bool

	// keyGate is the OWNER's seat at this editor's key seam, nil by default —
	// and nil means the seat does not exist rather than a stub that gets called
	// and answers "no" (keysource.go carries the contract, including the
	// disjointness that makes an owner's own count assertable). Installed by
	// SetKeyGate; consulted once per decoded key, in arrival order.
	keyGate KeyGate

	// inputLocked is the HOLD a surface that owns the editing line takes over it
	// (inputlock.go): while armed, a key a HUMAN typed that would change the
	// line's text is DROPPED rather than applied. It is the state behind
	// `gopi chat --replay`'s read-only performance line — the recording is typed
	// into this same buffer, so a watcher's keystroke in the middle of it edits
	// the artifact the next recorded key is appended to, irreversibly.
	//
	// WHY IT LIVES HERE BESIDE THE VALUE and not only in the key gate a surface
	// installs: a gate is consulted on the key path alone, is absent for every
	// other caller, and left the editor's second route to handleKey (a flushed
	// lone Escape) unguarded — inputlock.go's header carries the measured reason.
	// inputLockDrops counts what it refused, so "the line was protected" is a
	// measurement rather than a claim (Rule 15).
	inputLocked    bool
	inputLockDrops int

	// crPending marks that the previous key was a suppressed CR (a carriage
	// return that arrived with a paste). A pasted CRLF is ONE line break, not
	// two, so the LF that follows such a CR is dropped. The flag survives a
	// chunk boundary, which is where a paste can be split.
	crPending bool

	// pasteWarned is the throttle for the paste-burst warning: TRUE means the
	// burst currently in force has already been reported to the operator, so the
	// suppression is announced ONCE per burst and never once per swallowed
	// newline (a pasted 50-line block would otherwise print 50 lines of
	// diagnostic over the drawing). Cleared by the same event that ends the
	// burst — a read that is not a paste, or an Enter that submits — so the next
	// burst is announced again.
	pasteWarned bool

	// pasteSuppressed counts the Enters this surface has read as pasted content
	// since it was built. The warning quotes it, so "how often does this happen
	// to me?" is a measurement rather than a memory (Rule 15).
	pasteSuppressed int

	// inChunk marks that the keys now being handled came from ONE decoded read,
	// so a RUN of plain text among them is applied in one splice
	// (Value.InsertRunes) and the redraw it asks for is deferred to the end of
	// the chunk. It is the ONE place the editor tells its buffer "this is a bulk
	// arrival": the alternative — inspecting the decoder's own bytes per key — is
	// a second answer to a question the read loop has already answered.
	//
	// WHY IT MATTERS (measured): a terminal delivers a paste as FULL read chunks
	// (readBufBytes), and at 4 KiB per chunk the per-rune splice cost 61ms of
	// frozen input, 23.0s at 64 KiB. It is scoped to the key loop, so a keystroke
	// — one read, one key — is unaffected and a caller that drives handleKey by
	// hand (every existing unit test) keeps the eager redraw it asserts.
	inChunk bool

	// escOrigin is the ORIGIN of the read that left a lone ESC pending in the
	// decoder, and it is what makes an Escape survivable when the next bytes
	// come from ANOTHER producer (see resolveBareEscape). The zero value is
	// "typed", which is the right answer for every read that has no provenance
	// of its own.
	escOrigin ReadOrigin

	// region is the reserved screen layout (A5). Nil means the historic
	// full-screen behaviour: the editor owns the cursor and nothing is
	// reserved for the answer band, the menu or the status line.
	region *region
	// menu holds the completion state (A7). Always non-nil; a nil Completer
	// makes it inert.
	menu *completionState
	// completionWakes counts the bounded wakes spent on the CURRENT completion
	// request. It selects the wait cadence (fast, then slow) and carries the
	// spin guard; it is never a give-up deadline. The next request re-arms it.
	completionWakes int
	// menuRev is the completion state's PUBLISH generation, as seen by the last
	// repaint. The menu must be drawn when the generation moves, independent of
	// whether a request is still in flight: the worker can publish between the
	// repaint's snapshot and the pending check, and pending then reads false
	// with the new menu undrawn — the editor would park in an unbounded read
	// and the menu would not appear until the NEXT keystroke. Comparing the
	// generation closes that window (it cannot be missed, only seen).
	menuRev uint64
	// menuDirty marks that the completion menu changed WITHOUT the buffer
	// changing (selection moved, menu closed). The read loop's repaint gate is
	// "the line changed", so without this the menu would keep showing the old
	// selection after Up/Down and would stay painted after Escape — the keys
	// would work internally and be invisible, which is the defect this flag
	// closes.
	menuDirty bool
	// diagSink, when installed, receives every DEGRADATION this editor reports —
	// the same sentence it writes to errOut — so the record of a stall outlives
	// the terminal that showed it.
	//
	// WHY IT IS NOT errOut ALONE (measured, task 01a18990): cmd/agent binds the
	// editor's errOut to os.Stderr, while a chat run's console.log only receives
	// the AGENT's logger output, so `grep 'read loop is stalled' console.log`
	// returned 0 after a real stall — not because it did not happen but because
	// the editor never had a road to that file. A diagnostic whose only reader
	// is the operator's screen cannot be used to diagnose the screen.
	//
	// It is called with the finished sentence, on whatever goroutine detected the
	// degradation, and it must never block or paint: the caller installs a
	// RECORD (cmd/agent appends to the session's console.log), never the flow
	// seam — a report that queued a band row would re-enter the very wait it is
	// reporting on.
	diagSink func(string)
	// repaints counts how many times the editor decided to redraw (writeRepaint
	// entry point calls). It is the honest unit for a "no repaint storm" budget:
	// the number of write(2) CHUNKS a redraw emits depends on the transport
	// (whether a bufio flush coincided with a region move), so a chunk count
	// varies by load for an identical screen; the decision count does not. Only
	// tests read it — production never branches on it.
	//
	// IT IS ATOMIC BECAUSE ITS READER IS ANOTHER GOROUTINE. The count has ONE
	// writer — repaint(), on the read loop — and the invariant this field states
	// is exactly that; what it does not have is a happens-before edge with the
	// test that polls it while the loop is parked in a read. An atomic counter is
	// the shape for "one writer, readers anywhere": it publishes the value without
	// admitting a second writer, which a mutex here would have implied.
	repaints atomic.Int64
	// geomW, geomH and geomKnown remember the source's size at the last redraw,
	// so an idle bounded wait can tell a coalesced resize from no news at all
	// (a redraw with nothing to draw is a repaint per poll).
	geomW, geomH int
	geomKnown    bool

	// canvasCols is the caller's canvas width: the width the reserved layout is
	// installed at, whether or not the terminal is wider. Zero means "no canvas —
	// use the terminal's own width" (SetCanvasCols, which carries the reasoning).
	canvasCols int
	// answer is the renderer the turn's deltas are fed to, through this
	// editor's read loop (see answerqueue.go): the queue below owns WHEN a
	// turn-side call is applied, and the renderer's own state is touched by the
	// loop alone while it runs.
	answer *AnswerRenderer
	// answers is the pending turn-side calls plus the level that says they are
	// waiting. It is a QUEUE guard, never a lock over renderer state (Rule 15:
	// a lock that hides the race is a defect — this one exists because two
	// goroutines legitimately share the queue, while the renderer's fields have
	// exactly one owner at any instant).
	answers answerQueue
	// notify wakes a blocked read after a publish. A nil/no-op sink means the
	// status reaches the screen on the next wake — reported by NotifyEnabled so
	// the caller can announce it (Rule 15), never left to discover.
	notify Notifier
	// pal is the emphasis table for this editor's surfaces. Zero value =
	// monochrome, so an editor nobody configured emits the historic bytes.
	pal Palette
	// errOut receives anything the user must be told. A failed terminal probe
	// or a lost history entry is never swallowed (Rule 15) — and it cannot go
	// to the drawing writer without corrupting the layout.
	errOut io.Writer
	// geomObs, when set, is told the reserved layout's geometry every time it is
	// (re)installed — including with ok=false when the layout could not be
	// installed or was taken down. It exists so an OBSERVER of this editor's
	// paint stream (the screen dump) can build its grid at the geometry the
	// layout actually drew into, instead of probing a descriptor of its own and
	// re-interpreting the stream at a size the layout never used.
	geomObs func(cols, rows int, ok bool)
	// pastes, when set, spills a long paste to a file and leaves a REFERENCE on
	// the input line instead of typing the bytes into the buffer (see
	// pastestore.go). Nil means no spill — every caller that has no session
	// directory keeps the historic behaviour, byte for byte.
	pastes *PasteStore
	// pasteOpen is the file the paste now arriving is being written to, and
	// pasteUntil is when that paste's reads stop belonging to it. A paste is not
	// delivered as whole read buffers (measured: 64 KiB arrives as 16 reads of
	// 4095 bytes plus a 16-byte tail), so the reads after the first one are
	// CONTINUATIONS of the same paste and go to the SAME file — one paste, one
	// file, one reference.
	pasteOpen  string
	pasteBytes int
	pasteRunes int
	// pasteSource is the provenance this paste's reference carries: the store's
	// own (internal/inputsource — the operator's terminal is the HUMAN face), and
	// it is read off the store so the rewrite below and the first render state
	// one source.
	pasteSource string
	pasteUntil  time.Time
	// pasteBuf collects the bytes of a bracketed-paste block whose opening marker
	// has arrived and whose size is not yet known: the decision (spill, or hand to
	// the decoder) waits for the closing marker, because a paste that fits in one
	// read is the SHORT paste the historic path has always typed (see
	// pasteDecision). pasteOpenWhy records how the block was recognised, so the
	// rule that opened it is the rule its size is judged against.
	pasteBuf bytes.Buffer
	// pasteHead is how much of the start marker stands in the assembled block (the
	// whole sequence when the opening read carried it, its leading part when the
	// delivery split it and the rest was the decoder's fragment — pasteHeld, which
	// is prepended to the block). pasteGrew is true once a second read has
	// contributed, which is how a block still arriving is told from one that
	// fitted in its opening read.
	pasteHead int
	pasteGrew bool
	// pasteHeld is the decoder's fragment that the opening read's marker was SPLIT
	// at: the assembler takes it (Decoder.Take) so the block it belongs to is
	// collected whole, and it is put back in front of the bytes when the block
	// turns out to be a short paste the decoder must still see.
	pasteHeld []byte
	// runeRun is the accumulated plain-text run of the read being handled (see
	// flushChunkRunes). It is reused between reads, so a keystroke-heavy session
	// allocates nothing.
	runeRun []rune
	// frameMu guards the foreign-goroutine seam — the ONE seam a goroutine that
	// is NOT the read loop may reach (WriteLines / WriteBandRow). It protects
	// the QUEUE of pending band writes and never the editing model: the model
	// (Value, the renderer, the region) is read and written by the read loop
	// ALONE, which is what makes a lock here a queue and not a cure for the
	// race it removes (Rule 15 — a lock that hides a race is a defect).
	//
	// band, bandActive and flushing are the guarded state: the queue itself, the
	// level that says rows are waiting (what the loop's drain predicate and the
	// waiter both read), and the flag that marks the loop as being INSIDE the
	// drain (see WriteLines' fallback).
	frameMu sync.Mutex
	// band is the pending band layout: the rows a foreign producer handed over,
	// each still to be written into the region by the read loop.
	band []bandFrame
	// bandActive is the LEVEL a pending band write raises: at least one queued
	// row, or a drain in progress. It is level-triggered, not a one-shot
	// notification, for the reason every other publish in this package is:
	// measured (2026-09-28) the read loop can be parked in an UNBOUNDED read
	// when a publish lands, and only a flag the loop re-derives can carry the
	// draw past that park, and what the read loop re-derives in EVERY read
	// outcome (see Run's arms).
	bandActive bool
	// flushing is true while the read loop is inside drainBand: rows queued
	// during a drain are the drain's own (nested) production, so the caller that
	// queued them must not wait for a later drain to write them (see WriteLines).
	flushing bool
	// loopGoid is the goroutine id of the read loop while it is inside Run (0
	// when no loop is running). It is how the seam tells a FOREIGN band writer,
	// which must be able to read the screen back when it returns, from the loop's
	// OWN call (the submit path's flush), which must not wait for a drain only
	// it can perform (see bandwait.go).
	loopGoid atomic.Int64
	// notifyMisses counts the band frames queued while no wake seam was
	// installed. It is the FALLBACK's own bookkeeping, and it exists because the
	// fallback is otherwise SILENT: a frame queued with no seam is still a level
	// the read loop re-derives (bandPending, in every read-outcome arm), so it is
	// written on the next wake — a bounded delay, never a lost row. A count makes
	// the degradation OBSERVABLE (the caller can say "this row waited for the
	// loop's next wake" instead of leaving the operator to infer it), which is
	// what Rule 15 asks of a degraded path. It is guarded by frameMu: it is
	// written on the producer's goroutine.
	notifyMisses int

	// lines is the SECOND foreign-producer seam, guarded by its own mutex: the
	// texts a goroutine that is not the read loop wants INSERTED into the editing
	// line (InsertIntoLine, lineinsert.go). It carries its own lock rather than
	// sharing frameMu because the two queues are drained at different points of
	// repaint (the insert at the TOP, before the plan is resolved; the band
	// frames at the END, after the chrome is placed) and a shared lock would
	// couple two producers that have nothing to do with each other.
	lines lineInsertQueue
	// drafts is the pending DRAFT SEGMENT (draft.go): text a streaming producer
	// is still refining, drawn in the line but deliberately NOT in the buffer.
	// The read loop is the only goroutine that applies it, exactly as for the
	// line inserts above.
	drafts lineDraft
}

// BandNotifyMisses reports how many band frames were queued with no wake seam
// installed, i.e. how many rows reached the screen only on the read loop's next
// own wake. Zero means every band write had a real channel to interrupt the
// loop's blocked read with; production installs one (the terminal's wake pipe),
// so a non-zero value here means the fallback cadence is in force and a caller
// may announce it (Rule 15 — a degradation is reported, never inferred).
func (e *Editor) BandNotifyMisses() int {
	e.frameMu.Lock()
	defer e.frameMu.Unlock()
	return e.notifyMisses
}

// bandFrame is ONE band layout handed over by a foreign goroutine and written by
// the read loop (see Editor.WriteLines).
//
// It carries the text and the KIND of row layout the caller asked for — never a
// terminal byte and never a read of the editing model: the frame is data, and
// every read of the editor's own state (the region, the band width, the chrome
// rows) happens on the read loop, inside drainBand.
type bandFrame struct {
	// text is the caller's own text, verbatim.
	text string
	// oneRow is WriteBandRow's layout: the text is ONE already-split row of the
	// agent's flow output, so it is written as it stands and its trailing
	// newlines are dropped. FALSE is WriteLines' layout: trailing breaks are
	// dropped and the panel's own breaks START new band rows (region.WriteLines'
	// two policies, which is why the frame states which one was asked for).
	oneRow bool
}

// queueBand queues one band layout and raises the level. It is called by the
// FOREIGN goroutine (WriteLines) and it touches nothing but the queue: no
// terminal byte, no Value, no region — the read loop reads the whole editing
// model, and this is what keeps that true from a second goroutine.
//
// It reports whether a WAKE CHANNEL delivered the notification. False means the
// frame is queued with no channel to interrupt the loop's blocked read: it is
// still a level the loop re-derives on its own next wake (late, never lost), and
// the seam then returns without waiting for a draw it cannot bound — the miss is
// counted (see BandNotifyMisses) so the degraded cadence is announced rather
// than assumed (Rule 15).
func (e *Editor) queueBand(f bandFrame) bool {
	e.frameMu.Lock()
	e.band = append(e.band, f)
	e.bandActive = true
	e.frameMu.Unlock()
	// The level is raised BEFORE the wake: a notification that is missed can
	// only cost the row a bounded delay, while a row that is queued and never
	// flagged would be lost until the next keystroke (Rule 15).
	e.frameMu.Lock()
	if e.notify == nil || !e.notify.NotifyEnabled() {
		// No channel to interrupt a blocked read with: the frame is still a
		// LEVEL the loop re-derives on every read outcome, and the wait itself
		// is bounded for that reason (wakeReachesParkedRead → noWakePollMS), so
		// it is drawn within that bound rather than immediately — never "on the
		// next keystroke", which is the shape that made a lost wake into a
		// permanent stall. COUNTED, so the caller can announce the degradation
		// rather than leave it silent (see BandNotifyMisses).
		e.notifyMisses++
		e.frameMu.Unlock()
		return false
	}
	e.frameMu.Unlock()
	e.notify.Notify()
	return true
}

// bandPending reports whether a foreign producer has handed over a band layout
// the read loop has not written, or is writing one right now. It is a PEEK: the
// drain clears the level when the queue is empty, so asking in two places cannot
// drop a row.
func (e *Editor) bandPending() bool {
	e.frameMu.Lock()
	defer e.frameMu.Unlock()
	return e.bandActive
}

// drainBand writes every queued frame into the region, IN ORDER, on the caller's
// goroutine — which is the read loop. It reports whether it wrote anything.
//
// It is the whole of the fix: the region write and the chrome repaint below it
// happen here, so the band's writer and the editing model's reader are the same
// goroutine that also runs Run — there is no second reader to race.
//
// The drain is re-entrant by construction (a region write ends in a flush, and a
// flush callback that publishes again must not deadlock): the queue is taken
// under the lock and the write happens OUTSIDE it, so a row queued during the
// write is written by the loop's next pass.
func (e *Editor) drainBand() bool {
	if e.region == nil || !e.region.Active() {
		// The layout was taken down between the queue and the drain: the frames
		// cannot be drawn. They are dropped with their caller TOLD — the same
		// false the caller would have got had it asked at the seam (Rule 15),
		// and never a write on a screen the layout does not own.
		e.frameMu.Lock()
		e.band = nil
		e.bandActive = false
		e.frameMu.Unlock()
		return false
	}
	e.frameMu.Lock()
	if e.flushing {
		// A nested call (a flush callback reached from the write below): the
		// outer drain owns the queue, so this pass writes nothing rather than
		// re-entering the region's writer.
		e.frameMu.Unlock()
		return false
	}
	e.flushing = true
	e.frameMu.Unlock()
	wrote := false
	for {
		e.frameMu.Lock()
		if len(e.band) == 0 {
			// The queue is empty and no write is in flight: lower the level. It
			// is lowered HERE, with the queue empty and under the same lock, so
			// a producer that queues after this line raises it again — the level
			// can never be lowered over a frame that is still waiting.
			e.bandActive = false
			e.frameMu.Unlock()
			break
		}
		f := e.band[0]
		e.band = e.band[1:]
		e.frameMu.Unlock()
		if f.oneRow {
			e.region.WriteLines(strings.Trim(f.text, escNewline))
		} else {
			e.region.WriteLines(f.text)
		}
		wrote = true
	}
	e.frameMu.Lock()
	e.flushing = false
	e.bandActive = len(e.band) > 0
	e.frameMu.Unlock()
	return wrote
}

// drainBandFrames writes the queued band frames and then commits whatever the
// band's OTHER producers handed over while they were in flight (the rows
// Editor.Note queues), so one repaint's two halves cannot leave a row the queue
// already carried undrawn. Both halves run on the read loop.
//
// The ANSWER renderer's own queued calls are drained EARLIER in the repaint (see
// repaint): they commit answer rows, and the chrome below the band is placed
// from the layout the plan resolves, so a band that grows after the chrome was
// drawn would leave the editing line and the menu on rows the plan no longer
// gives them (measured: cmd/agent's menu surface read the paint stream at rows
// the layout no longer named).
func (e *Editor) drainBandFrames() {
	e.drainBand()
	e.drainNotes()
}

// pendRune accumulates one rune of the read's text run.
func (e *Editor) pendRune(r rune) { e.runeRun = append(e.runeRun, r) }

// flushChunkRunes applies the accumulated run of text to the buffer in ONE
// splice and refuses (with a bell) whatever the buffer bound would not take.
//
// The order of a refused rune is preserved: the run is inserted head-first, so
// the rune that did not fit is the LAST one, exactly as the per-rune form
// behaved. One bell covers the whole refused tail rather than one per rune —
// a paste whose tail overruns the bound is one refusal, and 4 KiB of bells is
// not a message (the same reason the per-rune form's caller belled once per
// keystroke, not once per byte).
func (e *Editor) flushChunkRunes() {
	if len(e.runeRun) == 0 {
		return
	}
	refused := e.val.InsertRunes(e.runeRun)
	e.runeRun = e.runeRun[:0]
	if refused > 0 {
		e.bell()
	}
}

// NewEditor builds an editor reading from src and drawing to out. hist may be
// nil (no recall). The prompt is drawn before the buffer on every redraw.
func NewEditor(src Source, out io.Writer, prompt string, hist *History) *Editor {
	e := &Editor{
		src:    src,
		out:    out,
		prompt: prompt,
		hist:   hist,
		dec:    NewDecoder(),
		val:    NewValue(0, 0),
		render: NewRenderer(out),
		now:    time.Now,
	}
	e.installInputBacklog()
	e.render.Prompt(prompt)
	e.render.SetRegion(e.region)
	return e
}

// installInputBacklog installs the source's optional input-readiness seam. It is
// asked for HERE, in the same spirit as the notification seam (EnableRegion
// asks for the Notifier): the editor never guesses what a source can say — a
// source that does not implement inputBacklog leaves the field nil and the read
// loop keeps the unread drawing order.
func (e *Editor) installInputBacklog() {
	if b, ok := e.src.(inputBacklog); ok {
		e.backlog = b
	}
}

// inputPending reports whether input is already waiting to be read. It is the
// read loop's ordering gate: a publish is drawn only when no input is queued, so
// a draw never overtakes a keystroke the source has already accepted. A source
// without the seam reports false (nothing is known to be waiting).
func (e *Editor) inputPending() bool {
	return e.backlog != nil && e.backlog.InputPending()
}

// NewChatEditor builds an editor WITH the A5/A7 layout: a reserved answer band
// (the live row of the streamed answer), a completion menu drawn below the
// editing line and a one-line status row, all inside a terminal scroll region.
// errOut receives user-facing warnings (it is written only when something is
// wrong, so it cannot disturb the layout in the normal case).
//
// The layout is enabled only if the terminal reports a geometry it can host;
// EnableRegion reports that, and the caller announces the degradation when it
// is false (never a silent difference in behaviour).
func NewChatEditor(src Source, out io.Writer, prompt string, hist *History, errOut io.Writer, completer Completer) *Editor {
	e := &Editor{
		src:    src,
		out:    out,
		prompt: prompt,
		hist:   hist,
		dec:    NewDecoder(),
		val:    NewValue(0, 0),
		render: NewRenderer(out),
		now:    time.Now,
		errOut: errOut,
		menu:   newCompletionState(completer),
		notify: noNotify{},
	}
	e.installInputBacklog()
	e.render.Prompt(prompt)
	e.region = newRegion(out)
	e.render.SetRegion(e.region)
	e.answer = NewAnswerRenderer(e.region, nil)
	e.answer.editor = e
	e.answers.editor = e
	return e
}

// SourceNotifier resolves the wake seam a Source exposes, or nil when it exposes
// none. It is the ONE resolution: the editor (EnableRegion) and the chat wiring
// both ask it, so the two cannot disagree about which source has a seam.
//
// Two spellings are honoured because the terminal's own notifier is deliberately
// NOT named Notify (raw.go's WakeNotifier: a *Terminal carrying both a Notify
// method and a WakeNotifier field would collide, "the kind of thing that silently
// makes the type assertion ... miss"). Spelling the assertion as a bare
// `src.(Notifier)` is what MADE it miss: production's *Terminal satisfied
// neither, so no seam was installed, the read loop parked in an UNBOUNDED read,
// and a status publish could only reach the screen when a keystroke happened to
// wake the loop — the operator's defect, whose trigger is this line
// (chat_stream.go's fix publishes more; this is what makes a publish ARRIVE).
func SourceNotifier(src Source) Notifier {
	if src == nil {
		return nil
	}
	if n, ok := src.(Notifier); ok {
		return n
	}
	if w, ok := src.(wakeNotifierSource); ok {
		return w.WakeNotifier()
	}
	return nil
}

// wakeNotifierSource is a source whose notifier carries its own name (see
// SourceNotifier).
type wakeNotifierSource interface{ WakeNotifier() Notifier }

// SetCanvasCols bounds the drawing width the reserved layout is installed at.
// Zero (the default) means "the terminal's own width" — the historic behaviour,
// byte-for-byte.
//
// WHY A CANVAS AND NOT A WIDER TERMINAL. The chat surface's chrome (the startup
// box with its tips panel, the reserved editing line and status row, the info
// bar) is designed for one geometry, and a terminal WIDER than that geometry must
// still draw the screen the design describes rather than stretching it: a box
// drawn at 200 columns is not the box the tips were measured for, and the info
// bar's columns are pinned to a fixed cell count so extra room cannot be spent on
// them anyway. So the layout is installed at min(terminal width, canvas) and the
// remaining cells to the right are left alone.
//
// IT IS DELIBERATELY A WIDTH AND NOT A HEIGHT. The status row is anchored to the
// screen's LAST row (region.go), so a canvas that claimed fewer rows than the
// terminal has would park the status line in the middle of the screen — the
// "composer is not at the bottom" defect the layout's own comments record. Rows
// therefore always come from the terminal, and a terminal SHORTER than the design
// is a degradation the caller announces (Rule 15).
func (e *Editor) SetCanvasCols(cols int) {
	if cols < 0 {
		cols = 0
	}
	e.canvasCols = cols
}

// canvasWidth is the width the reserved layout is installed at: the terminal's
// own width, capped by the caller's canvas. It is the ONE place the cap is
// applied, so the install, every resize and the answer renderer's column count
// cannot disagree about the width the screen is drawn at (Rule 14).
func (e *Editor) canvasWidth() int {
	w := e.width()
	if e.canvasCols > 0 && e.canvasCols < w {
		return e.canvasCols
	}
	return w
}

// EnableRegion installs the reserved layout and reports whether the terminal is
// big enough for it. False leaves the editor on the historic full-screen path
// (single Repaint, no reserved rows) — the caller should say why.
func (e *Editor) EnableRegion() bool {
	if e.region == nil || !e.region.Enable(e.height(), e.canvasWidth()) {
		return false
	}
	e.region.SetEditorRows(1)
	e.answer.SetCols(e.region.Cols())
	// The status row is drawn by the read loop, so a publish needs a way to wake
	// it. A source that can supply one is asked for it here, through the ONE
	// resolver that also answers the chat wiring's question (SourceNotifier); a
	// caller may also install one explicitly (SetNotification). An editor built
	// without one keeps the no-op sink, and NotifyEnabled REPORTS that instead of
	// letting a caller assume the publish reached the screen (Rule 15).
	if n := SourceNotifier(e.src); n != nil {
		e.SetNotification(n)
	}
	// The layout geometry EXISTS from here on: whoever observes this editor's
	// paint stream is told the geometry it was installed with, in the same call
	// that installs it (one source of truth — never a second probe).
	e.announceGeometry()
	return true
}

// DisableRegion returns the terminal to the full-screen layout (called on the
// exit path, before the terminal's attributes are restored).
func (e *Editor) DisableRegion() {
	if e.region != nil {
		e.region.Disable()
	}
}

// CloseOut hands the screen back: it stops the completion worker, then erases the
// reserved layout's rows and parks the cursor at the screen's first cell. It
// reports true when the layout was ACTIVE and therefore owned the screen — the
// caller then knows the screen is now clean and blank rather than merely not
// repainted — and false when the editor never had a reserved layout (the
// historic full-screen path, where the terminal is left exactly as it printed).
//
// It exists as its own step because the chat REPL closes out in TWO places: the
// editor's own Close() (which the input layer's deferred cleanup runs) and the
// REPL's final goodbye, which writes its own line on the way out. Erasing the
// screen and then printing the goodbye is the only order that leaves neither a
// stale band row nor a goodbye the terminal then throws away.
func (e *Editor) CloseOut() bool {
	if e.menu != nil {
		e.menu.stop()
	}
	if e.region == nil || !e.region.Active() {
		return false
	}
	e.region.Reset()
	return true
}

// RegionActive reports whether the reserved layout is installed.
func (e *Editor) RegionActive() bool { return e.region != nil && e.region.Active() }

// Layout reports the reserved layout's geometry (the rows each part owns). The
// band is above the editing line, so a caller that must place something on
// screen — or a test that must know WHICH row is the editing line — asks here
// instead of assuming the editing line is the screen's first row. ok is false
// when no layout is installed (the full-screen path owns the whole screen).
func (e *Editor) Layout() (Layout, bool) {
	if !e.RegionActive() {
		return Layout{}, false
	}
	p := e.region.Plan()
	return Layout{
		EditorTop:  p.EditorTop,
		EditorRows: p.EditorRows,
		MenuTop:    p.MenuTop,
		MenuRows:   p.MenuRows,
		BandTop:    p.BandTop,
		BandBottom: p.BandBottom,
		StatusRow:  p.StatusRow,
	}, true
}

// BandActive reports whether the reserved layout is installed AND live, i.e.
// whether a band write can be claimed (WriteBandRow) — the SINGLE answer to
// "does the layout own the screen" that every caller of the flow seam branches
// on (Rule 14), so the layout path and its fallback cannot disagree.
//
// It is the same predicate Answer() uses to decide whether there is a band at
// all, and it is deliberately asked per call rather than cached: the region can
// be taken down between turns (a terminal resized below the layout's minimum).
func (e *Editor) BandActive() bool {
	return e.region != nil && e.region.Active()
}

// Answer returns the streamed-answer renderer (nil unless the layout is
// installed): the caller feeds it the model's deltas and finishes it at the end
// of the turn.
//
// It is a LIVE answer: the region it draws through can be taken down between
// turns (a terminal resized below the layout's minimum), so a caller must not
// hold the pointer across a turn boundary. The REPL asks per turn, at the moment
// it builds the turn's renderer.
func (e *Editor) Answer() *AnswerRenderer {
	if e.region == nil || !e.region.Active() {
		return nil
	}
	return e.answer
}

// SetPasteStore installs the session's paste store: a read that fills the read
// buffer is then spilled to a file and replaced on the input line by its
// reference, instead of being typed into the buffer (see pastestore.go). A nil
// store (the zero value, and every caller with no session directory) disables
// spilling — the editor then behaves exactly as before.
func (e *Editor) SetPasteStore(s *PasteStore) { e.pastes = s }

// SetPalette installs the emphasis table for every surface the editor owns: the
// editing line's prompt, the streamed answer body, and the status row. One call
// is what makes the capability decision single (colour.go): the REPL resolves it
// once and hands the same table to the whole surface.
func (e *Editor) SetPalette(p Palette) {
	e.pal = p
	e.render.SetPalette(p)
	if e.answer != nil {
		e.answer.SetPalette(p)
	}
}

// SetStatus installs the one-line status source (model / iteration / tokens).
func (e *Editor) SetStatus(fn func() Status) {
	if e.answer != nil {
		e.answer.SetStatus(fn)
	}
}

// WriteLines streams the caller's own text (the REPL's /help panel, a slash
// command's table) into the answer band. It reports false when no reserved
// layout is installed, and the caller then keeps the historic flow path.
//
// The REPL MUST NOT print such text itself: with the layout installed the
// terminal is inside the scrolling region with the cursor parked on the editing
// line, so a flowed write lands on the editing row and every LF scrolls the band
// onto the caller's previous line — the panel paints itself over its own text.
// This is the same defect class as printing over the answer band, and the fix is
// the same one the answer renderer uses (region.WriteLines: address the live row,
// promote it with ONE LF inside the region).
//
// The editing line, the menu and the status row are redrawn afterwards: the
// panel's rows shift the band, and the editor owns every other row on screen.
func (e *Editor) WriteLines(text string) bool {
	if e.region == nil || !e.region.Active() {
		return false
	}
	// THE ROUTING (the defect this closes). This method is reached from a
	// FOREIGN goroutine — the turn goroutine's flow seam, the REPL's own
	// notices — and it used to write the band AND call e.repaint() here, which
	// reads the mutable editing model (clampedMenu → Value.RowsFor → Value.Text)
	// while the read loop's Run was doing e.val.Reset() on the same Value. Two
	// goroutines, one non-synchronised Value: measured 2026-09-28, 26
	// `WARNING: DATA RACE` blocks in ONE run of cmd/agent's
	// `go test -race -run TestChatSurfaceKey_`, all of them this seam against
	// Run's Reset.
	//
	// So the row is not written here. It is queued as a FRAME (data only — this
	// method reads no editing model and writes no terminal byte), the level is
	// raised and the read loop is woken; drainBand writes it inside the loop's
	// own repaint, where the region, the band width and the editing line are all
	// read by the ONE goroutine allowed to read them. The package's invariant —
	// "the read loop is the layout's only writer and the model's only reader" —
	// is thereby restored rather than worked around: no lock protects Value in
	// this design, because no second goroutine reaches for it.
	//
	// A FOREIGN caller keeps its historic contract: true means the row IS ON THE
	// SCREEN when this returns, because callers read their own row back
	// (cmd/agent's chat surface ARMS itself by writing a notice and asserting the
	// notice is on the grid). The wait is on the LEVEL, never on a one-shot
	// notification, for the reason every other publish in this package waits on a
	// level: the read loop can be parked when the notification lands, and only a
	// level it re-derives carries the draw past that park.
	//
	// The loop's OWN call is excluded (bandwait.go): the submit path flushes its
	// row from inside ReadLine, so waiting would be a deadlock — the only drainer
	// would be blocked waiting for itself.
	//
	// A false return keeps its historic meaning — no reserved layout (or a layout
	// taken down before the drain), so the caller keeps the historic flow bytes.
	// A frame that never reaches the screen is REPORTED, never assumed: the wait
	// returns false and names itself on the editor's error channel, and a queued
	// frame with no wake seam is counted (BandNotifyMisses) so the degraded
	// cadence is observable (Rule 15).
	if !e.queueBand(bandFrame{text: text}) {
		// No wake channel: the frame waits for the loop's own next wake, so this
		// reports the historic true (the layout owns the screen and will draw the
		// row) rather than a false that would make the caller ALSO write the
		// historic flow bytes — the row would then appear twice. The degraded
		// cadence is what BandNotifyMisses counts.
		return true
	}
	if e.onLoopGoroutine() {
		// The loop's OWN call (the submit path, a nested producer): it is the
		// only drainer, so waiting here would be a deadlock, and its caller does
		// not read the screen during the call — the drain follows in this same
		// read outcome.
		return true
	}
	if e.loopGoid.Load() == 0 {
		// NO READ LOOP IS RUNNING — between two lines, at EOF, at teardown.
		// Nothing can drain the queue until the next Run, so waiting here would
		// spend the whole bound and then report a failure that is not one. The
		// frame is the same LEVEL the no-wake-channel case relies on: the next
		// repaint drains it (late, never lost), which is also why the historic
		// true is right — the layout still owns the screen. Measured: without
		// this arm the cmd/agent pty case spent the bound on a shutdown-path
		// write and then fell back to the flow bytes mid-session.
		return true
	}
	return e.waitBandWrite()
}

// LayoutGeometry reports the geometry the reserved layout was INSTALLED with —
// the rows × cols the band, the editing line and the status row are drawn into.
// It is the ONE source of truth about "what geometry is on screen", and it is
// deliberately not a second probe of a descriptor: a probe answers "how big is
// this fd NOW", which is a different question from "what geometry did the
// layout measure", and answering the second with the first is how an observer
// ends up re-interpreting a repaint at a width the layout never used.
//
// ok is false when no reserved layout is installed: there is then no layout
// geometry to state, and a caller must say so rather than invent one.
func (e *Editor) LayoutGeometry() (cols, rows int, ok bool) {
	if e.region == nil {
		return 0, 0, false
	}
	active, rws, cls, _ := e.region.state()
	if !active || cls <= 0 || rws <= 0 {
		return 0, 0, false
	}
	return cls, rws, true
}

// SetGeometryObserver installs the sink that wants this editor's layout
// geometry (see Editor.geomObs). A nil observer disables the notification.
func (e *Editor) SetGeometryObserver(f func(cols, rows int, ok bool)) { e.geomObs = f }

// announceGeometry tells an installed observer what the layout now is. It reads
// the region's PUBLISHED state (not a fresh probe of the source), so what the
// observer is told is exactly what the layout drew into; with no reserved
// layout it reports ok=false, which is the fact — never a default dressed up as
// a measurement.
func (e *Editor) announceGeometry() {
	if e.geomObs == nil {
		return
	}
	cols, rows, ok := e.LayoutGeometry()
	e.geomObs(cols, rows, ok)
}

// BandCols reports the band's drawing width in cells, or 0 when no reserved
// layout is installed. It is the width a PRODUCER of band rows must wrap its own
// text to (see Editor.WriteBandRow and cmd/agent's flow seam): the split column
// belongs to whoever wrote the text, not to the band's row loop.
func (e *Editor) BandCols() int {
	if !e.BandActive() {
		return 0
	}
	return e.region.Cols()
}

// WriteBandRow draws ONE row of the agent's own flow output — a whole line of
// the live reasoning stream, a `[gopi]` diagnostic notice, a blank row that
// separates two turns — on the answer band, so a carrier that owns the screen
// shows the text instead of having its layout written over.
//
// WHY it is not WriteLines: the two are handed different text. WriteLines takes
// a FINISHED panel (many lines, trailing breaks dropped); this takes ONE row,
// already split by the caller, and the caller is the one that knows where its
// stream's line breaks are (see cmd/agent/chat_flow_seam.go: the reasoning
// stream arrives as fragments of one line, and the fragment boundary is NOT a
// row boundary). So a row is drawn as it comes — including an empty one, which
// is a real row of output — and no wrapper re-interprets the text here.
//
// A row WIDER THAN THE BAND is wrapped by the CALLER, at the band's own width
// (Editor.BandCols) and with the one wrap policy (lineedit.WrapToCols), before
// it arrives; this method does not re-cut it. The band's own row loop keeps its
// safety split for the callers that do not wrap (Rule 15: a line must never be
// dropped), but a producer that wrapped its own text owns the split column —
// the layout deciding it again is the residual this contract closes (measured:
// the layout's re-cut landed mid-escape and printed the sequence's parameters as
// text, and measured the introducer's bytes as cells).
//
// It reports false when no reserved layout is installed, and the caller keeps
// the historic flow bytes (Rule 15: a row must not be dropped on a layout that
// was taken down mid-turn).
func (e *Editor) WriteBandRow(row string) bool {
	if !e.BandActive() {
		return false
	}
	// No decoration is added to the row. The band is the layout's own answer
	// surface and its rows carry the text as written (a committed answer line
	// looks the same); the sources stay distinguishable because each one labels
	// itself — a notice starts with the agent's log prefix, the reasoning block
	// with the agent's own thinking marker.
	//
	// ROUTED, exactly as WriteLines is (see it for the measured race). This
	// method is called from the FLOW SEAM's own reader goroutine — the agent's
	// reasoning stream and the `[gopi]` log lines arrive there — and it used to
	// perform the band write and the repaint itself, i.e. it was the second
	// goroutine reading the editing model. The row is queued as a ONE-ROW frame
	// and written by drainBand on the read loop, which also draws the chrome the
	// promoted band row displaced.
	//
	// The row is QUEUED, and the read loop writes it — the same road WriteLines
	// takes, and for the same measured reason. A foreign caller returns once the
	// row is written (the seam flushes its pending text at a turn boundary and a
	// caller reports what it has committed); the loop's own call returns at once
	// (see bandwait.go).
	f := bandFrame{text: strings.Trim(row, escNewline), oneRow: true}
	if !e.queueBand(f) {
		// No wake channel: the frame waits for the loop's own next wake and is
		// still a LEVEL the loop re-derives, so the historic true is right (the
		// layout owns the screen and will draw the row). The degraded cadence is
		// what BandNotifyMisses counts.
		return true
	}
	if e.onLoopGoroutine() {
		// The loop's OWN call (the submit path, a nested producer): it is the only
		// drainer, so waiting here would be a deadlock.
		return true
	}
	if e.loopGoid.Load() == 0 {
		// A READ LOOP MAY EXIST BUT IS NOT RUNNING THIS INSTANT — before its first
		// Run, and between two lines of a live ReadLine loop (Run returns per
		// line). Nobody is draining now, so waiting would spend the whole bound and
		// then report a failure that is not one: the frame stays the LEVEL the
		// loop's next read outcome re-derives (the same arm WriteLines keeps for
		// this state, 363aa34e). It is late, never lost.
		//
		// WHY THIS IS THE ANSWER FOR *THIS* GOROUTINE, when the LAYOUT'S DIRECT
		// DRIVERS need the row DRAWN: the two are indistinguishable HERE. A fixture
		// that never runs a loop and a REPL whose loop has not been scheduled yet
		// are the same editor state — EnableRegion returned, nothing has read yet —
		// and the difference only becomes visible after the fact. Drawing in this
		// window is therefore not safe either: measured on cmd/agent's harnesses
		// (which start ReadLine in a goroutine and write band rows from the test
		// goroutine), the row drawn here lands on a grid the loop's FIRST repaint
		// then paints over — TestChatStream_RepaintsStayLinearInTheAnswer loses a
		// line (committed 39 of 40) and TestChatStatus_StaysOneScreenRowWithALong
		// ToolSummary takes the editing row. So the wait is not what those drivers
		// need removed; what they need is a DRAIN (Editor.SettleBand), which is the
		// only thing that can tell their state apart from the loop's boot window.
		return true
	}
	return e.waitBandWrite()
}

// SettleBand writes whatever band frames are QUEUED, on the caller's goroutine,
// and reports whether the layout still owns the screen.
//
// It exists for the LAYOUT'S DIRECT DRIVERS — a caller that installs the layout
// (EnableRegion) and writes band rows WITHOUT a read loop ever running: the pty
// harnesses in cmd/agent (cli_uistate_audit_test.go's auditFixture,
// chat_dump_geometry_pty_test.go, this package's own band tests). Their historic
// contract is WriteBandRow's: the row is on the screen when the call returns,
// because they read the grid back on the same goroutine. A queued frame is
// carried only by whoever re-derives it, and these callers have no next wake at
// all — for them "late" is "never", and the grid they then audit is empty (the
// measured defect: 10 cmd/agent cases, each spending the full 4s bandWaitBudget
// and then reading a false as "no reserved layout is active").
//
// WHY IT IS A SEPARATE CALL AND NOT A BRANCH INSIDE WriteBandRow — the honest
// part, and the reason it is a seam rather than a guess: the "no loop will come"
// state and the "the loop has not been scheduled yet" state are THE SAME EDITOR
// STATE at the moment of a write (EnableRegion done, nothing read yet), and no
// per-call fact can tell them apart. A branch on `loopGoid == 0` therefore has to
// pick one answer for both, and the measured cost of picking "draw" is a row the
// loop's first repaint paints over (39 of 40 committed lines) while the cost of
// picking "queue" is an empty grid for the fixtures. A caller that KNOWS it owns
// the screen — because it is the one that never starts a loop — says so HERE, and
// every other caller keeps the behaviour the read-loop routing gave them.
//
// It writes no editing model (drainBand writes frames; repaint(), which reads
// Value, stays the loop's own) and adds no writer: a direct driver is by
// definition the only goroutine holding this editor.
func (e *Editor) SettleBand() bool {
	wrote := e.drainBand()
	if wrote {
		// The drain parked the cursor on the band's live row; hand it back to the
		// caret, exactly as the repaint path does. Without this the frame a
		// direct driver settled left the hardware block off the input — the
		// operator's startup sighting, on the one road that has no repaint after
		// the drain (render.go's PlaceCaret carries the measurement).
		e.render.PlaceCaret()
	}
	return e.BandActive()
}

// Note queues one row of the CALLER'S OWN text for the answer band — an
// activity row ("⚙ read internal/lineedit/answer.go"), a notice, a failure —
// so the row is part of the transcript and survives the turn that produced it.
//
// WHY IT IS NOT WriteBandRow. That method WRITES and repaints, and it is called
// from the goroutine that owns the text (the flow seam's own reader). Note is
// called from the TURN's goroutine — the one concurrently streaming the answer
// into the same band — so a write here would interleave its cursor placement
// with the read loop's repaint. It therefore takes the publish road: the row is
// queued on the answer renderer and committed by the read loop (see
// AnswerRenderer.Note), which is the only writer.
//
// It reports false when no reserved layout owns the screen; the caller then
// writes the historic flow bytes, so a note is never dropped on a layout that
// was taken down mid-turn (Rule 15).
func (e *Editor) Note(text string, st Style) bool {
	if !e.BandActive() {
		return false
	}
	e.answer.Note(text, st)
	e.awaitForeignNote()
	return true
}

// NoteRuns queues ONE caller-composed row whose parts carry different emphasis
// (see AnswerRenderer.NoteRuns). It is Note's sibling, not a second road: the
// row is published the same way, drained by the same read loop, and reports
// false for the same reason (no reserved layout — the caller keeps its own
// bytes).
func (e *Editor) NoteRuns(runs []NoteRun) bool {
	if !e.BandActive() {
		return false
	}
	e.answer.NoteRuns(runs)
	e.awaitForeignNote()
	return true
}

// NoteRunsCopied is NoteRuns for a row the operator will SELECT AND PASTE (the
// `/info` block, whose whole reason to exist is that a line can be copied into a
// shell): same cells, same emphasis, no continuation marker on a wrapped row.
func (e *Editor) NoteRunsCopied(runs []NoteRun) bool {
	if !e.BandActive() {
		return false
	}
	e.answer.NoteRunsCopied(runs)
	e.awaitForeignNote()
	return true
}

// SetNotification installs the wake mechanism the publish path uses. Production
// passes the terminal's Notify() (see Open); a source that cannot provide one
// leaves it unset, and a publish then reaches the screen on the next wake —
// NotifyEnabled reports which, so the caller announces the degradation.
func (e *Editor) SetNotification(n Notifier) {
	if n == nil {
		n = noNotify{}
	}
	e.notify = n
	if e.answer != nil {
		e.answer.SetNotification(n)
	}
	if e.menu != nil {
		// The completion worker publishes candidates it does not draw; it wakes
		// the read loop through the same seam (see completionState.worker). One
		// seam for every publish keeps "every write has one owner" true for both
		// producers.
		e.menu.setNotification(n)
	}
}

// Source returns the editor's input source. It exists so the wiring can ask the
// source for the wake seam it may expose (lineedit.Terminal does) and install it
// — the editor never guesses.
func (e *Editor) Source() Source { return e.src }

// NotifyEnabled reports whether a publish actually reaches a blocked read. False
// means the status row waits for the next keystroke.
func (e *Editor) NotifyEnabled() bool {
	return e.notify != nil && e.notify.NotifyEnabled()
}

// Notifier returns the wake seam installed on this editor (the zero-value
// no-op sink when none is). It exists so the chat wiring can inspect — and
// announce — the degradation instead of assuming a publish is seen.
func (e *Editor) Notifier() Notifier { return e.notify }

// PublishStatus announces that the status source's values changed. It performs
// NO terminal I/O and returns immediately: it raises the dirty flag and wakes
// the read loop, which is the only goroutine that writes to the terminal
// (AnswerRenderer.drawStatus, through the read loop's repaint). A caller on its
// own goroutine therefore never interleaves a cursor placement with the loop's
// repaint.
//
// The earlier design drew straight from the caller (StatusTick) while the read
// loop was mid-repaint: two byte streams of absolute cursor motion on one fd.
// Both callers — the REPL's turn loop and the completion worker — now publish,
// and every write has one owner.
func (e *Editor) PublishStatus() {
	if e.answer == nil {
		return
	}
	e.answer.PublishStatus()
}

// statusPending reports whether a status publish is waiting to be drawn. It
// PEEKS (the flag stays set) — the read loop consults it to decide whether a
// repaint is owed, and repaint consumes the flag when it draws. Consuming in
// both places would drop the draw.
func (e *Editor) statusPending() bool {
	return e.answer != nil && e.answer.StatusPublishPending()
}

// drainNotes commits any caller rows the band is holding (Editor.Note) and
// reports whether it committed one. It is called by the READ LOOP only, before
// the chrome is repainted, for the reason Note exists: the band's rows are
// committed by its one writer, and a note's own producer is the turn's
// goroutine — which is concurrently streaming the answer into the same band.
//
// It is a PEEK's sibling rather than a flag: the queue itself is the level, so
// asking costs one length check and a drain leaves the queue empty (a second
// ask in the same iteration writes nothing).
func (e *Editor) drainNotes() bool {
	if e.answer == nil || !e.answer.Noting() {
		return false
	}
	return e.answer.drainNotes()
}

// menuUnread reports whether the completion worker published a candidate list
// the read loop has not drawn. It is a PEEK, like statusPending: repaint clears
// it, so consulting it in both places cannot drop a draw.
func (e *Editor) menuUnread() bool {
	return e.menu != nil && e.menu.menuUnread()
}

// SetStatusLayout installs the reserved layout's status source AND installs the
// notification seam in one call, mirroring NewChatEditor's wiring. It exists so
// a caller that builds an editor over an arbitrary Source (cmd/agent's tests)
// gets the same single-writer road as production.
func (e *Editor) SetStatusLayout(fn func() Status, n Notifier) {
	e.SetStatus(fn)
	e.SetNotification(n)
}

// Close releases the editor's background state (the completion worker) and
// returns the terminal layout. It is idempotent.
func (e *Editor) Close() {
	e.DisableRegion()
	if e.menu != nil {
		e.menu.stop()
	}
}

// Menu returns the currently published completion menu.
func (e *Editor) MenuState() Menu {
	if e.menu == nil {
		return Menu{}
	}
	return e.menu.snapshot()
}

// SetCompletionNotify installs a raw wake function for the completion worker,
// for a caller that knows only a func (the tests' scripted source). Production
// installs the terminal's Notifier once, through SetNotification, and that seam
// already covers the completion publish — a Notifier set there is ADAPTED here
// rather than overwritten, so the two entry points cannot install different
// wake mechanisms behind the same editor.
func (e *Editor) SetCompletionNotify(f func()) {
	if e.menu == nil {
		return
	}
	if _, isNoop := e.notify.(noNotify); !isNoop && e.notify != nil {
		e.menu.setNotification(e.notify)
		return
	}
	e.menu.setNotifier(f)
}

// SetCompleter installs the completion source. It exists as a seam for callers
// that build the editor before they know the source (cmd/agent's chat input
// wires production's completer through it, and its tests inject a stub), and it
// makes "no completer" reachable on an editor that was built with one.
func (e *Editor) SetCompleter(c Completer) {
	if e.menu == nil {
		return
	}
	e.menu.setCompleter(c)
}

// CompleterRequest asks the worker for candidates for the current buffer. It is
// the ONLY completion entry point, and it never blocks: the redraw path calls it
// and reads the previous snapshot.
func (e *Editor) CompleterRequest() {
	if e.menu == nil || !e.RegionActive() {
		return
	}
	// A fresh request gets a fresh wake budget: the bound is per request, not
	// per line, so every keystroke is allowed its own chance to see the answer.
	e.completionWakes = 0
	e.menu.request(e.val.Text())
}

// completionPending reports whether the completion menu still needs a wake.
//
// Two conditions, and BOTH are needed:
//
//   - the published menu is a generation the last repaint did not draw. The
//     worker publishes with no keystroke behind it and can publish in the window
//     between the repaint and this check, when "a request is in flight" is
//     already false — relying on that test alone parked the editor in an
//     unbounded read with the candidates published but never drawn until the
//     NEXT keystroke (typing "/" and seeing nothing);
//   - a request is still being computed (the wait cadence's fast phase).
//
// A generation comparison cannot be missed, only seen, which is why it is a
// counter and not a flag the publisher would have to re-arm.
func (e *Editor) completionPending() bool {
	if e.menu == nil || !e.RegionActive() {
		return false
	}
	return e.menuUndrawn() || e.menu.requestPending()
}

// menuUndrawn reports whether the completion state published a menu the last
// repaint did not draw. It is the editor's own view of "there is something on
// the screen that is not there yet".
func (e *Editor) menuUndrawn() bool {
	if e.menu == nil {
		// The historic editor has no completion state at all, and every caller
		// asks this on the repaint path (which the non-chat editor also walks):
		// an unguarded receiver here panicked Run for an editor built without a
		// menu (measured — cursor_screen_test.go's typedRowsScreen). The sibling
		// predicate menuUnread has always been nil-safe; this one now matches.
		return false
	}
	rev, _ := e.menu.publishedRev()
	return rev != e.menuRev
}

// menuNeedsDraw is the completion menu's LEVEL: there is a published candidate
// list the screen has not seen. It is the OR of the three ways a publish can be
// outstanding — the worker's unread flag, the read loop's own menu change, and
// the publish generation the last repaint did not draw — and it is what every
// read outcome must ask, because the wake is a NOTIFICATION and a notification
// that lands while the loop is parked in a blocking read is consumed by nobody:
// measured under -race (menu_test.go EscapeClosesTheMenu, ~1 in 30-60) the menu
// was published and open with the worker's request finished, yet drawn=1,
// wakes=0 and every read timeout -1 — the loop was parked through the alert.
// Asking the LEVEL is idempotent, so a branch that consults it twice can never
// miss a draw and can never cost one it does not owe.
func (e *Editor) menuNeedsDraw() bool {
	return e.menuUnread() || e.menuDirty || e.menuUndrawn()
}

// completionWait is the read wait for the current completion state: a fast poll
// while the fast checks last, then the slow cadence. It returns pollTimeoutBlock
// when nothing completion-related is outstanding.
func (e *Editor) completionWait() (timeoutMS int, polling bool) {
	if !e.completionPending() {
		return pollTimeoutBlock, false
	}
	e.completionWakes++
	if e.completionWakes <= completionFastPollWakes {
		return completionPollMS, true
	}
	return completionBlockedMS, true
}

// Repaints is how many times the editor has redrawn since it was built. It
// exists for the repaint-budget assertions (Rule 10.2): the honest unit of a
// "no repaint storm" claim is the number of redraw DECISIONS, not the number of
// write(2) chunks those decisions were split into.
func (e *Editor) Repaints() int { return int(e.repaints.Load()) }

// MenuSelect moves the completion selection (Up/Down while a menu is open).
//
// The menu is re-PAINTED here rather than flagged for the next repaint: the
// painted rows and the rows the layout reserves move together, and a window
// that could be skipped (a repaint the read loop decides it does not owe) is a
// window in which the two disagree — the editing line drawn one row too high
// with nothing left to correct it.
func (e *Editor) MenuSelect(delta int) {
	m := e.MenuState()
	if !m.Open {
		return
	}
	e.menu.publish(m.MoveSelection(delta))
	// The line did not change, so the read loop's repaint gate ("the buffer
	// changed") would not fire: the moved selection must ask for its own
	// repaint, or Up/Down would be invisible.
	e.menuDirty = true
	e.paintMenu()
}

// MenuClose closes the menu (Escape).
func (e *Editor) MenuClose() {
	if e.menu == nil {
		return
	}
	if !e.MenuState().Open {
		// Already closed: Escape is inert, and an inert key must not repaint
		// (the read loop would otherwise wake the terminal on every stray ESC).
		return
	}
	e.menu.cancel()
	e.menuDirty = true
	// The erase and the layout's release happen together (see MenuSelect): a
	// closed menu that still reserved rows would leave the editing line drawn
	// above a band that is no longer there.
	e.paintMenu()
}

// MenuAccept applies the selected candidate to the buffer. It is the OPEN
// MENU'S ACCEPT ACTION, reached from TWO keys: Enter (the choice key, which
// consumes the keystroke — the menu is a list and Enter takes the item) and Tab
// (the completion key, which the caller then leaves alone). It reports false
// when there is nothing to accept.
func (e *Editor) MenuAccept() bool {
	m := e.MenuState()
	if !m.Open {
		return false
	}
	if !m.Complete(e.val) {
		return false
	}
	// The line changed under the menu, so the candidate list is recomputed for
	// it (off the read path) and the menu re-sizes.
	e.CompleterRequest()
	return true
}

// SetClock replaces the time source (tests drive the paste-burst windows with a
// fake clock instead of sleeping — the windows are milliseconds and a sleeping
// test would be both slow and flaky).
func (e *Editor) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	e.now = now
	if e.answer != nil {
		// One clock for the whole editor: the answer's flush cadence and the
		// paste-burst windows are the same kind of policy, so a test that drives
		// one controls both (and production's real clock is installed in both
		// places by the same call).
		e.answer.SetClock(now)
	}
}

// Value exposes the buffer (tests assert the editing model through it).
func (e *Editor) Value() *Value { return e.val }

// Run reads one line. It returns as soon as a submit, Ctrl+D or Ctrl+C is
// classified; the caller decides what the line means.
// noteAudience records WHO runs the read loop, for the whole of a Run: the band
// seam and the answer-renderer seam decide with it whether a call from another
// goroutine must be queued (and waited for) or performed here, and the EDITING
// MODEL records it too (noteValueLoop) so a reader outside the loop reads the
// published snapshot rather than sampling the buffer this goroutine edits. It is
// set BEFORE the first repaint, so a producer that reaches the editor during the
// boot paint already sees the loop, and cleared when Run returns.
func (e *Editor) noteAudience() {
	e.noteLoopGoroutine()
	e.val.noteValueLoop(e.loopGoid.Load())
}

// wakeReachesParkedRead reports whether a producer's notification can INTERRUPT
// a read the loop has already blocked in. False is the degraded transport (a
// source with no wake pipe, a notification sink that is a no-op), and it is the
// ONE fact that decides whether the unbounded wait is safe at all: an unbounded
// wait is a promise that SOMETHING ELSE will wake the loop, and this is that
// something.
//
// It asks the sink rather than the source, because the sink is what the loop's
// own read is watching — `Terminal` exposes its wake pipe through NotifyEnabled,
// and a source that has none leaves the editor's no-op sink in place
// (Editor.SetNotification, and NewChatEditor's zero value).
func (e *Editor) wakeReachesParkedRead() bool {
	return e.notify != nil && e.notify.NotifyEnabled()
}

// readWait decides how long this iteration's read may block, and reports what
// SORT of wait it is: polledCompletion (a completion cadence, so an expiry is a
// draw of the candidates) and levelPolled (the wait was armed BECAUSE a draw
// level was pending, so an expiry is the prompt that level is owed).
//
// THE INVARIANT IT ENFORCES — the whole reason it is a function instead of
// eleven lines inline: THE READ WAIT IS UNBOUNDED ONLY WHILE NOTHING IS OWED.
// Every bound below is a LEVEL being re-derived at the moment the wait is
// decided, and one of them (drawLevelPending) covers every queue whose drainer
// is this loop. That is what makes the wait independent of a wake that may
// already have been spent: a notification is an EDGE, and an edge that lands
// between this decision and poll(2) is consumed by the same read that reports
// it — so a level that trusts edges can wait for ever, holding the operator's
// keystrokes (Ctrl+C among them) in the tty buffer. Measured on a real session:
// `chat-exit.marker` recorded `reason:"interrupt"`, `exit_code:0`,
// `signal:""` — the 0x03 DID arrive, arbitrarily late.
//
// It is a pure decision over the editor's own LEVELS (plus the test-only park
// hooks, nil in production), so a case can assert it directly — including the
// state that cannot be forced through a real scheduler: "a level is pending and
// no wake is coming".
func (e *Editor) readWait() (timeout int, polledCompletion, levelPolled bool) {
	if e.dec.BareEscape() {
		// A lone ESC in the decoder is the completion menu's close key;
		// it is also the prefix of every arrow key, so the wait for the
		// rest of the sequence is BOUNDED. Without the bound Escape would
		// only take effect when the next key arrived (and would then be
		// reported late).
		timeout = escWaitMS
	} else {
		// Candidates are being computed off-path, or published and not yet
		// drawn: wake up to DRAW them. The editor's read wait is otherwise
		// unbounded, so without this bounded wake a candidate list computed
		// for "/" would not appear until the next keystroke.
		timeout, polledCompletion = e.completionWait()
	}
	// The PARK WINDOW: the wait has been decided (here: unbounded, because
	// nothing was outstanding) and the read has not begun. A publish landing
	// in this span is the flake's own interleaving — the loop is committed
	// to a blocking read and the alert arrives after the decision it was
	// meant to inform. What must carry the draw is therefore the LEVEL
	// (menuNeedsDraw, re-derived below), never the notification. The hook is
	// nil in production: this is a nil check, not a behavioural branch.
	// (The classification of this read is in spillableChunk, which asks the
	// bufers this point and the decoder's own state — the delivery's division
	// is the terminal's to make, so it is read, never predicted.)
	//
	// THE INJECTED-KEY LEVEL IS RE-DERIVED HERE TOO, and for the same reason
	// one layer down: a queued key's wake is a ONE-BYTE pipe (`Terminal`
	// drains it when it reports EventWake, `Injector.wake` writes it), so an
	// edge that lands in this window is a signal spent on a loop committed to
	// blocking (measured: the key NAME never reached the turn seam inside the
	// case's 3s bound). The queue itself is the level — a key may be queued
	// and still being paced, which the injector delivers on the read the loop
	// is about to make. The hook is nil in production (Rule 15: a seam that
	// is live announces itself, see setInjectParkHook).
	if retryMS, bounded := runInjectParkHook(); !bounded && timeout == pollTimeoutBlock && e.inputPending() {
		// A read unit is queued while the loop was about to block for ever:
		// bound this wait so the read collects the key instead of waiting
		// for a notification that has already been spent.
		timeout = injectParkRetryMS
	} else if bounded && timeout == pollTimeoutBlock {
		timeout = retryMS
	}
	// A PENDING DRAW LEVEL BOUNDS THE WAIT — the invariant this file states
	// for every level (see drawLevelPollMS). It comes AFTER the completion
	// and injected-key arms because those two are faster cadences for the
	// same wait, and BEFORE the read, because a wait whose bound is decided
	// after it is taken is not a bound.
	//
	// WHY THIS IS NOT REDUNDANT WITH THE EventWake ARM. That arm draws a
	// level whose WAKE ARRIVED. This one covers the level whose wake did
	// NOT: the notification pipe is drained by the read that reports
	// EventWake, so an edge landing between the decision to block and the
	// poll(2) is spent — and the loop would then sit in pollTimeoutBlock for
	// ever with a row, a note or an answer op queued behind it, holding the
	// operator's next keystroke (Ctrl+C included) in the tty buffer. The
	// level is re-derived here on every pass, so this arm needs no edge at
	// all: the loop wakes itself, draws, and its next pass finds nothing
	// pending and blocks for real.
	if timeout == pollTimeoutBlock && e.drawLevelPending() {
		timeout = drawLevelPollMS
		levelPolled = true
	}
	// AND A LEVEL RAISED AFTER THIS DECISION CANNOT BE STARVED EITHER: when the
	// source cannot wake a PARKED read, no edge exists that could tell the loop
	// about a frame queued one microsecond from now, so the wait must come back
	// around on its own (see noWakePollMS).
	if timeout == pollTimeoutBlock && !e.wakeReachesParkedRead() {
		timeout = noWakePollMS
	}
	return timeout, polledCompletion, levelPolled
}

// reportAnswerWaitFailure announces that a published answer call was not applied
// within the bound: the caller is about to read a screen the read loop has not
// drawn, so it is told on the editor's own error channel rather than left to
// infer it (Rule 15). It never blocks and never retries.
func (e *Editor) reportAnswerWaitFailure() {
	if e.errOut == nil && e.diagSink == nil {
		return
	}
	e.degradation("[gopi] editor: a published answer call was not applied within " + answerWaitBudget.String() + " (the read loop is stalled?) — the caller announced a screen it does not own\n")
}

func (e *Editor) Run() (Result, error) {
	// The read loop's identity, for the whole of this call: it is what makes a
	// band write FROM here return without waiting (see bandwait.go) while one
	// from the REPL's own goroutine waits for the draw it then reads back — and
	// the same fact decides, for the answer renderer, whether a turn-side call is
	// applied here or queued for this goroutine (see answerqueue.go).
	// e.loopGoid is cleared on the return — it is the PER-RUN fact the audience
	// decisions ask fresh (bandwait.go) — while the buffer's own record of its read
	// loop is NOT: Run returns between lines (ReadLine calls it per line), and
	// ownership of the model is a lifetime fact (valueowner.go).
	e.noteAudience()
	defer e.loopGoid.Store(0)
	e.val.Reset()
	// A NEW LINE starts with the window following the caret: a pin left from the
	// previous line's scroll would show the middle of a message that no longer
	// exists (the buffer was just reset).
	e.render.unpinWindow()
	e.closePaste()
	if e.hist != nil {
		e.hist.Reset()
	}
	e.startLine()
	e.repaint()

	buf := make([]byte, editReadBuf)
	for {
		// The read wait is normally unbounded, but two situations BOUND it:
		// a pending lone ESC (resolved as the Escape key when the wait
		// expires), and a completion request still being computed off-path
		// (woken up so the candidates reach the screen without another
		// keystroke). Which one applies is decided here, once per iteration.
		timeout, polledCompletion, levelPolled := e.readWait()
		runParkGateHook()
		ev, err := e.src.ReadEvent(buf, timeout)
		if err != nil {
			return Result{}, err
		}
		switch ev.Kind {
		case EventWinch:
			// Resize: re-install the layout, then repaint (the buffer and the
			// cursor are preserved — nothing else changes). The answer
			// renderer's width follows the new geometry: a stale width would
			// wrap (and truncate) the answer at the OLD terminal size.
			if e.region != nil && e.region.Active() {
				if !e.region.Resize(e.height(), e.canvasWidth()) {
					// The layout could not follow the terminal down: it is taken
					// DOWN, and the degradation is announced with the height the
					// composer needs (Rule 15 — never a silent layout change).
					e.warn(fmt.Sprintf("terminal is now too short for the chat layout (needs %d rows) — full screen output", RowsMinUsable()))
				}
				e.answer.SetCols(e.region.Cols())
				// The layout moved (or went away): an observer of this paint
				// stream must re-size its grid to the SAME geometry, or it keeps
				// re-interpreting the repaint at the old width.
				e.announceGeometry()
			}
			e.repaint()
			continue
		case EventWake:
			// An in-process notification: a caller PUBLISHED a new status, or
			// queued a row of its own text for the band (Editor.Note), or the
			// completion worker published candidates. The read loop draws it —
			// this branch is the entire fix for the two-writer defect, and it is
			// what makes the status row update DURING a turn, when no keystroke
			// is coming to wake the loop.
			//
			// Repaint only when there is really something new to draw — a wake
			// whose publish was already painted must not cost a redraw, or
			// three keystrokes become a repaint storm. Every flag below is
			// CONSUMED by the repaint that shows it (the status flag by
			// AnswerRenderer.Tick, the unread flag by noteDrawn), so a wake
			// that carried no news costs nothing. The completion half keeps
			// BOTH of master's arms — menuDirty is the read loop's OWN menu
			// change (a selection moved, a menu closed: no publish behind it)
			// and menuUndrawn the publish generation the last repaint did not
			// draw — beside the branch's publish-seam unread flag. Either pair
			// can be argued about on its own; neither can miss a draw.
			//
			// A QUEUED CALLER ROW IS DRAWN FIRST: it is committed INTO the band
			// (it scrolls), while the chrome below the band is placed
			// absolutely, so the drain must complete before the repaint that
			// re-places the editing line and the status row.
			e.drainNotes()
			// THE DRAFT IS DRAWN HERE BECAUSE THE WAKE IS THE ONLY THING THAT
			// CAN CARRY IT. A streaming producer states its partial from
			// another goroutine (draft.go) and raises this same seam, so a wake
			// whose only news is a draft belongs in this predicate: without it
			// the read loop answered "nothing to draw", parked again, and the
			// operator's text reached the line only when a KEYSTROKE woke the
			// loop (measured: `gopi chat /voice` showed nothing until Escape).
			// The level is CONSUMED by the repaint that shows it (repaint →
			// drainDraft), so a wake carrying no news still costs no redraw.
			// THE INPUT YIELD BELOW COVERS THE DRAFT TOO, which is the same
			// rule the other flags get: a keystroke already on the wire is read
			// first and the draft's own wake follows.
			if e.statusPending() || e.menuNeedsDraw() || e.bandPending() || e.answerPending() || e.lineInsertPending() || e.draftPending() {
				if e.inputPending() {
					// Input is already waiting: read it FIRST and leave the
					// publish raised. The two are independent events that this
					// single wake reported as one, and drawing the publish here
					// would make the draw overtake a keystroke the source has
					// already delivered — a caller waiting on the draw would see
					// an editor that has not read input already on the wire
					// (measured: cmd/agent's small-height chat test asserted the
					// editing row while the 5th keystroke was still unread, so
					// the row read "you> hel"). The status flag is sticky and
					// the publish's own wake follows, so the draw is LATE by one
					// iteration, never lost.
					continue
				}
				e.repaint()
			}
			continue
		case EventNone:
			// A bounded read expired with no input. If that bounded wait was
			// the lone-ESC wait, the ESC resolves as the Escape key here.
			//
			// THE FLUSHED KEY TAKES THE SAME ROAD AS A DECODED ONE, and that is
			// not a detail — it was a DEFECT. A LONE Escape never travels through
			// applyRead: an ESC is the prefix of every arrow key, so the decoder
			// holds it and the TIMEOUT resolves it (FlushEscape), which is the arm
			// this branch owns. Calling handleKey directly from here therefore
			// handed the key straight to the editor, and a key-gate owner that
			// claims Escape never saw it. MEASURED 2026-10-07, on a real pty: the
			// /voice mode installed a gate that claims a typed Escape, the gate
			// passed its own unit case, and the mode did NOT leave when Escape was
			// pressed. The unit case missed it because InjectKey decodes a COMPLETE
			// sequence and so reaches applyRead — the ordinary path — while the
			// operator's Escape is exactly the one that does not.
			//
			// The origin is stamped here for the same reason it is stamped there:
			// provenance is a fact about the READ this key came from, and this key
			// came from the same read.
			if res, done, err := e.flushEscapedKey(); err != nil {
				return Result{}, err
			} else if done { // else-ok: the error arm returns above; the submit arm and the repaint arm are the same statement
				return res, nil
			}
			// A completion wake with nothing on the wire still repaints: the
			// worker may have published candidates in the meantime, and the
			// menu is the only thing that changes (the editing line is
			// redrawn from the same buffer).
			if polledCompletion {
				// The bounded wait was a completion wake. Draw only what is
				// really new, exactly as the notification path does: this
				// branch is the DEGRADED cadence (a source with no wake
				// notification), and a poll that found the previous publish
				// already drawn must not cost a redraw — otherwise the
				// fallback turns keystrokes into a repaint storm.
				if e.statusPending() || e.menuNeedsDraw() || e.bandPending() || e.answerPending() || e.lineInsertPending() || e.draftPending() {
					e.repaint()
				}
				continue
			}
			// Otherwise: nothing to classify — the paste window is a property
			// of the keys that already arrived, so there is no pending state to
			// resolve. Two things a bounded wait can still discover: a resize
			// whose notification was coalesced away, and a PUBLISH that raced
			// the read loop into the park (the caller raised the flag at the
			// moment the loop decided to block). The publish is drawn whenever
			// one is pending; the resize only when the geometry really moved —
			// an unconditional repaint here would be a repaint per poll, and on
			// a source without a wake pipe these bounded waits are the fallback
			// cadence, i.e. exactly the storm this editor must not produce.
			// The completion LEVEL belongs here as much as in the arms above:
			// an idle wait is the read outcome a publish raised in the park
			// window meets, and a branch that asks only about the status row
			// leaves a published candidate list undrawn until the next
			// keystroke. It is not a repaint per poll: the level is raised by a
			// publish, so it is false on every idle wait that has no news.
			//
			// THE DRAFT BELONGS HERE FOR THAT SAME REASON, and this is the arm
			// the operator's defect landed in: a bounded wait is the read the
			// loop PARKED through when the producer stated its draft, so the
			// seam's wake was spent on the decision to block and the text — like
			// a publish raised in that window — must be found by the LEVEL on
			// the way back around. It is not a repaint per poll for the identical
			// reason: drainDraft lowers it under the data's own lock.
			e.drainNotes()
			if levelPolled {
				// THE WAIT WAS ARMED *BECAUSE* A LEVEL WAS PENDING
				// (drawLevelPollMS), so its expiry is not an idle tick — it is
				// the prompt that level is owed. Drawing unconditionally here
				// (rather than re-asking the level) is what makes the bound
				// meaningful: the level was pending when the wait was taken, and
				// this package has ONE drainer, so nothing but this pass can
				// have lowered it.
				e.repaint()
				continue
			}
			if e.statusPending() || e.menuNeedsDraw() || e.geometryChanged() || e.bandPending() || e.answerPending() || e.lineInsertPending() || e.draftPending() {
				e.repaint()
			}
			continue
		}
		if ev.Bytes == 0 {
			if ev.EOF {
				// A paste block still held when the input ends is decoded, not
				// dropped: below the spill threshold it is the short paste the
				// historic path has always typed (Rule 15 — the source ending is
				// not a reason to lose the operator's bytes).
				res, done, err := e.flushPasteBlock()
				if err != nil {
					return Result{}, err
				}
				if done {
					return res, nil
				}
				e.render.Finish()
				return Result{EOF: true, EOFKind: EOFKindSource}, nil
			}
			continue
		}
		// A PASTE IS SPILLED BEFORE IT IS DECODED, and the spill IS the whole
		// edit for its reads: a read no human could have typed (PasteThreshold)
		// cannot be typing, so what it carries goes to a file and the input line
		// gets the reference. Nothing is decoded on this path — that is what makes
		// it byte-exact (a pasted ESC, a pasted control byte, a pasted invalid
		// UTF-8 sequence and a pasted CRLF all reach the file as they arrived,
		// instead of being re-encoded through the key decoder's own meaning) —
		// and it is also what keeps the buffer small.
		//
		// The condition is on the READ, not on the burst clock: a paste arrives as
		// large reads by construction, while a user's keystrokes arrive one per
		// read. A short paste (below PasteThreshold in one read, the ordinary
		// case) therefore takes the historic path unchanged.
		// THE TERMINAL'S OWN BRACKETED-PASTE MARKER IS AN OPENING, NOT YET A
		// SPILL. The marker is the authoritative statement that a paste begins
		// here (see spillableChunk), and it is what a size rule cannot say: a
		// paste the link divides into short reads carries the marker without ever
		// tripping PasteThreshold. But the marker only OPENS the block — and a
		// paste that fits in one read is exactly the short paste the historic path
		// has always typed (a CRLF inside it must collapse, and the line must hold
		// the text). So an opening read is COLLECTED here, with nothing decoded and
		// nothing decided, and the decision waits for the closing marker: the block
		// is spilled if it is too large to be typing, and handed to the decoder
		// otherwise (pasteDecision).
		if e.pasteOpening(buf[:ev.Bytes]) {
			e.pasteBuf.Write(buf[:ev.Bytes])
		} else if e.pasteBuf.Len() > 0 {
			// A paste block is open: every read that arrives before its closing
			// marker is its body, whatever the read's size (a short read is a
			// short DIVISION of the block, never a keystroke — the block cannot be
			// typed into).
			e.pasteGrew = true
			e.pasteBuf.Write(buf[:ev.Bytes])
		} else {
			// No paste block is open: the read is judged on its own, by the two
			// rules the editor has always had (see spillableChunk and
			// pasteContinuation), and typed if neither applies.
			if e.spillableChunk(ev.Bytes) && e.spillPaste(buf[:ev.Bytes]) {
				// The reference is now on the line, and this redraw is what paints
				// it: the read carried no keystroke, so there is nothing to decode
				// and nothing else to do for it.
				//
				// The spill failed and was reported (`warn`) in the other branch:
				// the bytes then take the historic path rather than being dropped
				// (Rule 15 — nothing is swallowed, and the user's paste is never
				// silently lost).
				if e.val.Changed() {
					e.repaint()
				}
				continue
			}
			if e.pasteContinuation(ev.Bytes) && e.spillAppend(buf[:ev.Bytes]) {
				// This read belongs to the paste already on the line: it is
				// APPENDED to the same file and NOTHING is added to the line (its
				// reference already stands for the whole paste). Same reasons as
				// above: nothing decoded, nothing dropped, and only the reference's
				// size changes — which the append itself rewrites.
				if e.val.Changed() {
					e.repaint()
				}
				continue
			}
			// A block still HELD (its reads arrived below the threshold, in more
			// than one read) ends here: this read is the keystroke that says the
			// paste is over, and the block is resolved to the historic path before
			// the keystroke itself is handled — the order the user typed is the
			// order the buffer must hold.
			if res, done, err := e.flushPasteBlock(); err != nil {
				return Result{}, err
			} else if done { // else-ok: the error arm returns above; one statement keeps the held block's own submit distinct from the keystroke's
				return res, nil
			}
			// This read is ordinary input (a keystroke, a short paste, a command
			// sequence): the paste that was open is OVER, so its window is closed
			// here — the next large arrival is read on its own terms instead of
			// being appended to a finished paste.
			e.closePaste()
			res, done, err := e.applyRead(buf[:ev.Bytes])
			if err != nil {
				return Result{}, err
			}
			if done {
				return res, nil
			}
			continue
		}
		// A paste block just took this read (it opened, or it continued). The
		// block's own window is re-armed for the read that may close it, and the
		// decision is taken HERE, once per contributing read — a holding decision
		// returns without touching the buffer, so nothing of the block is decoded
		// while its size is still unknown.
		e.pasteUntil = e.now().Add(pasteContinuationGap)
		res, done, err := e.resolvePaste(false)
		if err != nil {
			return Result{}, err
		}
		if done {
			return res, nil
		}
	}
}

// applyRead decodes one read's bytes into keys and applies them as ordinary input
// — the historic path, with the buffer's splice batched into one edit per run of
// runes (see flushChunkRunes). It is the ONE place a read's keys are applied, so
// the paste envelope's own deferred arm (a bracketed paste too short to spill)
// goes through exactly the same handling as a keystroke would.
//
// THE READ'S ORIGIN IS STAMPED HERE, on every key of the read, because this is
// the one place that knows which read produced them (e.sourceOrigin). Three
// arms reach this function — a keystroke, the short-paste envelope, and
// applyData — and all three pass through this single stamp, which is why the
// origin cannot be attached on one path and forgotten on another.
func (e *Editor) applyRead(data []byte) (Result, bool, error) {
	origin := e.sourceOrigin()
	// A READ FROM ANOTHER PRODUCER CANNOT CONTINUE A HELD ESCAPE (see
	// resolveBareEscape). This is asked BEFORE the decoder is fed, because the
	// fusion happens inside Feed: once the two bytes are joined there is no
	// record that they came from different producers, and the key that results
	// belongs to whichever read completed it.
	if e.resolveBareEscape(origin) {
		res, done, err := e.flushEscapedKey()
		if err != nil {
			return Result{}, false, err
		}
		if done {
			return res, true, nil
		}
	}
	keys := e.dec.Feed(data)
	// A LONE ESC STILL HELD AFTER THIS READ IS THIS READ'S KEY, and the origin
	// it was typed at is recorded with it (resolveBareEscape reads the pair).
	if e.dec.BareEscape() {
		e.escOrigin = origin
	}
	// THE TERMINAL'S OWN FRAMING IS EVIDENCE THE SIZE RULE CANNOT HAVE, and it
	// is recorded HERE because this is the one place that sees the decoded keys
	// of every read (the read loop, the short-paste envelope and applyData all
	// arrive here). A read that carries a bracketed-paste marker under the
	// decoder — either end of it: a short paste's start and its end commonly
	// land in different reads — is a block the terminal SAID was a paste, so the
	// burst is filed as framed and keeps the full window (Value.burstWindow).
	// A read with no marker keeps whatever the size rule decided (NoteKeys).
	keys, framed := withoutPasteMarkers(keys)
	if framed {
		e.val.NoteBurst(len(keys), origin, e.now())
	}
	if len(keys) > 0 {
		if !framed {
			e.val.NoteKeys(len(keys), origin, e.now())
		}
		// ONE READ may END a burst as well as start one, and the throttle is
		// released with it: the paste is over, so the next one is news again.
		if !e.val.BurstActive() {
			e.pasteWarned = false
		}
	}
	stampKeyOrigins(keys, origin)
	// Every key of this read is applied as ONE bulk edit: the buffer's splice
	// happens once for the chunk (Value.InsertRunes) instead of once per rune,
	// which is the difference between a paste that costs one memmove and one
	// that costs O(N²) — measured 23.0s of frozen input at 64 KiB before the
	// fix. The keys are decoded and handled exactly as before, one by one;
	// only the buffer's own copy is batched.
	chunked := false
	for _, k := range keys {
		// THE OWNER'S SEAT, for every key of the read — including the runs of
		// plain text that the `continue` below would otherwise skip. An owner
		// that consumes the key is obeyed here and nowhere else: this loop is
		// the read's ONLY decoder of intent, so "the run does not get the key"
		// is expressible exactly once (keysource.go's KeyGate contract).
		if e.keyDropped(k) {
			continue
		}
		// NOT CONSUMED AND NOT IGNORED → the editor is about to act on it, and
		// the owner is told so here, BEFORE the branch that acts: this is the
		// run's own movement, and it carries the key (hence its Origin) that
		// caused it. Announcing it in one place — above BOTH the rune fast path
		// and the handler switch — is what keeps the triple disjoint: every key
		// of every read is reported exactly once (keysource.go).
		e.keyGateRelease(k)
		if k.Code == KeyRune && !k.Alt {
			// A RUN of plain text is one bulk edit: the rune is accumulated
			// and spliced once, when the run ends (see flushChunkRunes).
			// Anything else — a motion, a kill, an Enter, a newline (whose
			// CRLF adjacency rule lives in handleKey), a bare Alt — flushes
			// first, so every handler still runs against a buffer with
			// everything before it applied, in order.
			e.pendRune(k.Rune)
			chunked = true
			continue
		}
		e.flushChunkRunes()
		res, done, err := e.handleKey(k)
		if err != nil {
			return Result{}, false, err
		}
		if done {
			return res, true, nil
		}
	}
	e.flushChunkRunes()
	// Changed() CLEARS the flag, so it is read exactly once here and the
	// result is what both the gate and the re-request decision use.
	lineChanged := e.val.Changed()
	if lineChanged && chunked {
		// EVERY KEY OF THIS READ IS ALREADY APPLIED TO THE BUFFER and only
		// one picture of it is owed: the redraw happens HERE, once, instead
		// of once per key. The buffer is mutated synchronously at each key,
		// so this is a scheduling decision and not a state one — the
		// handlers above still saw the buffer they expect (a menu Enter
		// compares the text before and after its own accept; a CRLF pair is
		// judged against crPending).
		//
		// A chunk that ENDS the line skips this (the submit path drew the
		// uncapped line itself, and a repaint after it would paint over a
		// line that has already been reported).
		e.repaint()
	}
	// THE DRAFT'S LATE WAKE for the ordinary key path: a producer that stated
	// its partial while this read was being DECODED raises the same equality
	// this line evaluates, so an unchanged buffer must not hide it — the draft
	// is not in the buffer (draft.go), which is exactly why a keystroke that
	// changed nothing still owes it a frame.
	if lineChanged || e.menuDirty || e.statusPending() || e.bandPending() || e.answerPending() || e.lineInsertPending() || e.draftPending() {
		// The line, the menu or the published status changed. A BUFFER
		// change asks the completion worker for candidates (a non-blocking
		// hand-off — a filesystem-backed completer must never stall typing)
		// and repaints from the PREVIOUS snapshot; new candidates for a
		// narrowed line then arrive on a completion wake. A menu-only change
		// (selection moved, menu closed) must NOT re-ask: the worker would
		// publish the closed menu's candidates straight back.
		//
		// menuDirty is NOT cleared here: repaint owns it (a change is
		// consumed by the redraw that shows it, and by nothing else). The
		// status flag follows the same rule — statusPending PEEKS, and the
		// repaint calls Tick, which is what consumes the publish. Clearing
		// either one here would drop the draw it asked for.
		if lineChanged {
			e.CompleterRequest()
		}
		e.repaint()
	}
	return Result{}, false, nil
}

// spillableChunk reports whether the read's SIZE makes it the START of a paste —
// a read so large a human cannot have typed it (see PasteThreshold). It is one of
// the two ways a paste is recognised, and the rule itself is unchanged: a store is
// needed (no session directory = the historic behaviour), the paste window must be
// shut, and the read must be a unit — Decoder.Pending() == 0, because a read that
// ARRIVES while the decoder holds a fragment continues that fragment, so its bytes
// are not the whole of what is being delivered and filing them would write half a
// sequence into the user's file.
//
// THE OTHER WAY IS THE TERMINAL'S OWN STATEMENT, and it is what a size rule cannot
// make: pasteEnvelope recognises the bracketed-paste start marker at the head of a
// read, and that recognition does not depend on how the link chose to divide the
// delivery — which is the property the size rule cannot have, because the division
// is the TERMINAL's to make (measured 2026-09-28: 64 KiB written to a pty arrives
// as 16 reads of 4095 bytes — the case the spill was built for — while a paste
// written in smaller writes, or one the terminal delivers in short reads, arrives
// as reads of a few hundred bytes or fewer, every one of them below PasteThreshold,
// so the whole paste was decoded into the buffer and the 64 KiB bound cut the
// operator off tens of characters later).
//
// The bytes of a read that is neither are typed: still one splice and one redraw,
// so nothing is slow, and nothing is misfiled.
func (e *Editor) spillableChunk(n int) bool {
	return e.pastes != nil && e.pasteOpen == "" && n >= PasteThreshold && e.dec.Pending() == 0
}

// pasteOpening reports whether a read OPENS a bracketed-paste block.
//
// THE MARKER IS THE AUTHORITATIVE STATEMENT, and it is what a size rule cannot
// make: PasteThreshold INFERS "this is a paste" from a read being too large to
// type, so a paste the link divides into short reads is never recognised by it.
// The terminal, meanwhile, SAYS it — in the same block of bytes — and that
// sentence does not depend on how the delivery was divided. So the marker opens
// the block whatever the read's size is.
//
// THE SAME STATEMENT REACHES THE BURST. The block this function opens is handed
// to applyRead when it is solved, and the read that carries the marker marks the
// burst FRAMED (withoutPasteMarkers + Value.NoteBurst) — so the Enter suppression
// window follows the terminal's own claim on every delivery shape, including the
// one that never spills.
//
// THE MARKER IS READ ACROSS THE BOUNDARY IT MAY BE SPLIT AT. The decoder holds a
// fragment of the previous read (Decoder.Pending), and the marker's head can be
// that fragment (a six-byte sequence the link divided between two reads). The
// fragment is TAKEN here — with Take, not a copy — because the assembler now owns
// those bytes: the block they belong to is being collected, and a decoder left
// holding them would decode the same bytes twice. pasteHead records how much of
// the marker stands in the BLOCK (all of it when nothing was held, the rest
// otherwise), which is what the spill arm strips.
//
// An open is NOT yet a spill: the block is collected and its size decides (see
// pasteDecision), because a paste that fits in one read is the SHORT paste the
// historic path has always typed, CRLF collapse included.
func (e *Editor) pasteOpening(data []byte) bool {
	if e.pastes == nil || e.pasteOpen != "" || e.pasteBuf.Len() > 0 || len(data) == 0 {
		return false
	}
	held := e.dec.Pending()
	if held > len(pasteStartMarker) {
		return false
	}
	want := pasteStartMarker[held:]
	if len(data) < len(want) || !bytes.Equal(data[:len(want)], want) {
		return false
	}
	if held > 0 {
		e.pasteHeld = e.dec.Take()
	}
	e.pasteHead = len(want)
	return true
}

// pasteDecision resolves the paste block whose bytes are now collected: it either
// SPILLS the block or hands it to the decoder as ordinary input. It is called
// after every read that contributes to the block, and it reports a submit exactly
// as applyRead does (a block whose bytes type an Enter ends the line).
//
// THE SIZE DECIDES, and both arms are the rule the editor already had — applied to
// the bytes the terminal bracketed, whatever shape its delivery took:
//
//   - a block at or past PasteThreshold is spilled: one read that large cannot be
//     typing, and a block that reaches it over several reads is a paste the
//     terminal announced (the case this change repairs: the operator's paste no
//     longer depends on the link dividing it into full read buffers);
//   - a block a spanning delivery has CLOSED (its end marker arrived) is a paste
//     the terminal bracketed and framed, so it is filed as one even when a short
//     division kept it below the byte threshold;
//   - a block that is still arriving below the threshold is HELD, bounded by
//     PasteThreshold bytes (a paste of any size costs one buffer, not one per
//     byte), so the decision is never made on a prefix of the block;
//   - and a block that fits in ONE read, below the threshold, is the short paste
//     the historic path has always typed: the bytes go to the decoder as the
//     terminal sent them (its own markers dropped, CRLF collapsed), no file is
//     written.
//
// NOTHING IS EVER LOST (Rule 15): the bytes not spilled are the bytes decoded, and
// the bytes spilled are written to the file whose reference stands on the line —
// the two arms partition the block, and no arm drops a byte.
func (e *Editor) pasteDecision() (Result, bool, error) { return e.resolvePaste(false) }

// flushPasteBlock resolves a block still held when the input ends or when the
// paste's window has expired — the block is OVER, so a block below the threshold
// is the short paste the historic path types rather than one still expected to
// grow (Rule 15: the source ending, or the terminal going quiet, is not a reason to
// lose the operator's bytes).
func (e *Editor) flushPasteBlock() (Result, bool, error) { return e.resolvePaste(true) }

// resolvePaste is the ONE resolution of an assembled paste block: every caller
// above reaches the same decision, and the only difference between them is whether
// the block may still grow (final == false) or is over (final == true).
//
// THE DECISION, in order:
//
//   - a block at or past PasteThreshold is SPILLED: one read that large cannot be
//     typing, and a block that reaches it over several reads is a paste the
//     terminal announced (the case this change repairs: the operator's paste no
//     longer depends on the link dividing it into full read buffers);
//   - a block the delivery CLOSED (the end marker arrived) after more than its
//     opening read is SPILLED too: the terminal bracketed it and framed it, and
//     one paste is one file however short its division was;
//   - a block still arriving below the threshold is HELD (bounded by
//     PasteThreshold bytes — a paste of any size costs one buffer, not one per
//     byte), so the decision is never made on a prefix of the block. A block that
//     FITTED IN ITS OPENING READ is never held: its size is already known;
//   - and a block below the threshold that is over is the SHORT paste the historic
//     path has always typed: the bytes go to the decoder as the terminal sent them
//     (its own markers dropped, CRLF collapsed), no file is written.
//
// NOTHING IS EVER LOST (Rule 15): the bytes not spilled are the bytes decoded, and
// the bytes spilled are written to the file whose reference stands on the line —
// the two arms partition the block, and no arm drops a byte.
func (e *Editor) resolvePaste(final bool) (Result, bool, error) {
	block := e.pasteBuf.Bytes()
	if len(block) == 0 {
		e.resetPasteBuf()
		return Result{}, false, nil
	}
	framed := bytes.HasSuffix(block, pasteEndMarker)
	// A block that CLOSED in its opening read is whole: its size is known now.
	// One that did not is arriving (pasteGrew, or the opening read alone), and a
	// block under the threshold must be held rather than decided on a prefix.
	closedInOpeningRead := !e.pasteGrew && framed
	spill := len(block) >= PasteThreshold || (framed && e.pasteGrew)
	if !spill && !final && !closedInOpeningRead {
		// Still arriving, not yet large enough to judge: the next read continues
		// it (see resolvePaste's own account of the bounds).
		return Result{}, false, nil
	}
	if !spill {
		// The short paste: the historic path, on the bytes as the terminal sent
		// them. The fragment the marker was split at goes back in FRONT of the
		// block, because for this arm the decoder is still the stream's owner and
		// must see whole the sequence it was holding.
		data := append(append([]byte(nil), e.pasteHeld...), block...)
		e.resetPasteBuf()
		return e.applyRead(data)
	}
	// A paste by size or one the terminal's own framing closed: the framing is not
	// content, so it is stripped before the bytes are filed (see stripPasteBlock),
	// and the reference takes its place on the line.
	body := stripPasteBlock(block, e.pasteHead)
	e.resetPasteBuf()
	if len(body) == 0 {
		// Nothing but framing: there is no paste to file, and the read belongs to
		// the decoder after all (it drops the markers exactly as this arm just
		// did).
		return Result{}, false, nil
	}
	if e.spillPaste(body) {
		if e.val.Changed() {
			e.repaint()
		}
	}
	return Result{}, false, nil
}

// SetKeyGate installs the OWNER's seat at this editor's key seam: every decoded
// key of every read is offered to g before the editor acts on it (keysource.go
// carries the contract, the three methods and the disjointness they promise).
//
// A NIL GATE IS THE DEFAULT AND MEANS "NO SEAT": the key loop tests the
// interface once per read's key list and calls nothing, so a surface with no
// owner behaves — byte for byte — as it did before this seam existed.
func (e *Editor) SetKeyGate(g KeyGate) { e.keyGate = g }

// SetWindowKeyRow forwards to the renderer (Renderer.SetWindowKeyRow): the
// editing line's marker rows do (default) or do NOT name the scroll keys.
//
// IT IS THE OWNING SURFACE'S SWITCH, and it exists because a surface can claim
// every key before this editor sees it — `gopi chat --replay` installs a key gate
// that claims all typed keys, so the scroll keys never reach the window and the
// marker row's key clause describes a binding the surface refuses. The counts are
// untouched; see the renderer's own comment for what may not be turned off.
func (e *Editor) SetWindowKeyRow(on bool) { e.render.SetWindowKeyRow(on) }

// WindowKeyRow is the READER beside SetWindowKeyRow (Renderer.WindowKeyRow): does
// the editing line's marker rows state the scroll keys?
func (e *Editor) WindowKeyRow() bool { return e.render.WindowKeyRow() }

// sourceOrigin reports where THIS read's bytes came from: the source's own
// answer when it can give one (an injector can), and "typed" otherwise. It is
// read ONCE per read, immediately after the read returned, which is the only
// moment the answer is about the bytes just received (ReadOriginSource states
// that contract; Injector.ReadOrigin implements it).
func (e *Editor) sourceOrigin() ReadOrigin {
	if s, ok := e.src.(ReadOriginSource); ok {
		return s.ReadOrigin()
	}
	return ReadOriginTyped
}

// stampKeyOrigins writes one read's origin onto every key it produced. It is a
// package function rather than a method so the stamp cannot depend on editor
// state: provenance is a fact about the read, and it is applied to the whole
// decoded batch in one place.
func stampKeyOrigins(keys []Key, origin ReadOrigin) {
	for i := range keys {
		keys[i].Origin = origin
	}
}

// withoutPasteMarkers reports the keys a read contributed with the terminal's
// bracketed-paste markers removed, plus whether the read carried one.
//
// THE MARKER IS NOT CONTENT (handleKey drops it for the same reason), but it IS
// evidence: it is the terminal stating that the block it frames is a paste. The
// caller records that statement against the burst (Value.NoteBurst) instead of
// letting the read be judged by its size alone — a short paste below
// PasteBurstMinChars has no other evidence at all, and a paste's closing marker
// arriving in its own read would otherwise look like a one-key read.
//
// It never reclassifies by itself: a read carrying ONLY a marker yields no keys
// and no burst, exactly as before this file listed it here.
func withoutPasteMarkers(keys []Key) ([]Key, bool) {
	framed := false
	kept := keys[:0]
	for _, k := range keys {
		if k.Code == KeyPasteStart || k.Code == KeyPasteEnd {
			framed = true
			continue
		}
		kept = append(kept, k)
	}
	return kept, framed
}

// dispatchKeyGate offers one key to the installed gate and reports whether the
// owner CONSUMED it. A nil gate costs one interface comparison and returns
// immediately — the no-op default is a property of the code, not a promise.
//
// The order is the contract's: ask first (the owner may want any key), then tell
// it about a key the editor would ignore anyway (KeyUnclaimed) — and if the
// owner did not consume that one either, nothing further is reported, because
// the editor will not act and there is no movement to announce.
func (e *Editor) dispatchKeyGate(k Key) bool {
	if e.keyGate == nil {
		return false
	}
	if e.keyGate.GateKey(k) {
		return true
	}
	if editorIgnoresKey(k) {
		e.keyGate.KeyUnclaimed(k)
	}
	return false
}

// keyGateRelease announces that the editor is about to ACT on this key, so the
// owner's record of "the run moved" is complete: every key is either consumed at
// GateKey, unclaimed at KeyUnclaimed, or released here — exactly one of the
// three, once (keysource.go).
func (e *Editor) keyGateRelease(k Key) {
	if e.keyGate != nil {
		e.keyGate.KeyRelease(k)
	}
}

// stripPasteBlock removes the terminal's bracketed-paste framing from an
// ASSEMBLED block: the start marker (the head-count part of it that stands in the
// block, which is short of the whole sequence when the marker was split across the
// read boundary) and the end marker when it arrived. An assembled block is where a
// read alone could not be: the head count is known (pasteHead), so a marker the
// delivery divided between two reads is stripped instead of leaving its tail as
// the file's first bytes.
func stripPasteBlock(block []byte, head int) []byte {
	if head > len(block) {
		head = len(block)
	}
	block = block[head:]
	if bytes.HasSuffix(block, pasteEndMarker) {
		block = block[:len(block)-len(pasteEndMarker)]
	}
	return block
}

// resetPasteBuf discards an assembled block and the opening state that framed it.
func (e *Editor) resetPasteBuf() {
	e.pasteBuf.Reset()
	e.pasteHeld = nil
	e.pasteHead = 0
	e.pasteGrew = false
}

// pasteContinuation reports whether one read continues the paste already being
// spilled: the paste's window is open, the read arrived inside it, and it is too
// large to be a keystroke.
//
// A CONTINUATION CAN ITSELF BE A FULL READ — that is the normal case, not an edge
// one: a 64 KiB paste arrives as sixteen full reads plus a short tail, and every
// one of them belongs to the SAME paste (one paste, one file, one reference —
// measured 2026-09-28: filing each full read separately produced sixteen
// references on the line). The window is what separates "the paste is still
// arriving" from "a new paste"; a keystroke between two pastes closes it
// (closePaste), so two pastes the user made at different moments stay two.
func (e *Editor) pasteContinuation(n int) bool {
	return e.pastes != nil && e.pasteOpen != "" && n >= pasteContinuationMinBytes &&
		e.dec.Pending() == 0 && !e.now().After(e.pasteUntil)
}

// spillChunkBytes is the read size a paste arrives in, and the ONLY producer of
// the fixture size the tests paste with: the pty reports one byte short of the
// read buffer (readChunkShortfall — measured, see PasteThreshold), so a test that
// pasted exactly editReadBuf would exercise a shape no terminal produces.
const spillChunkBytes = PasteThreshold

// spillPaste writes one read's bytes to the session's paste store and leaves the
// REFERENCE on the input line. It reports whether the paste was spilled: false
// means the store refused, the reason was reported, and the caller must fall
// back to typing the bytes (never dropping them).
//
// The reference is inserted as ordinary text, so it moves and deletes like any
// other input: the user may send it as it stands, or delete it (the file stays —
// nothing here reclaims a spill, because a deleted reference is not proof that
// the content was unwanted).
func (e *Editor) spillPaste(data []byte) bool {
	// The terminal's bracketed-paste framing is NOT content: the decoder drops
	// those two markers, and this path bypasses the decoder, so it drops them
	// here (stripPasteMarkers) rather than filing the terminal's envelope into a
	// file the user is handed.
	data = stripPasteMarkers(data)
	if len(data) == 0 {
		// Nothing but framing: there is no paste to file, and the read belongs to
		// the decoder after all (it drops the markers exactly as this path just
		// did). Unreachable from a real terminal — a read this large cannot be
		// six bytes of framing — but a silent empty spill file would be a lie.
		return false
	}
	ref, err := e.pastes.Spill(data)
	if err != nil {
		e.warn(fmt.Sprintf("the %d-byte paste could not be spilled to a file (%v) — it was inserted into the message as text instead", len(data), err))
		return false
	}
	line := PasteRefFor(ref)
	if refused := e.val.InsertString(line); refused > 0 {
		// The reference does not fit the (already nearly full) buffer. The paste
		// is still not lost — it is on disk, and the path is announced where the
		// user can read it — but the line cannot carry it.
		e.warn(fmt.Sprintf("the %d-byte paste was saved to %s but the input line has no room for its reference (%d runes refused)", len(data), ref.Path, refused))
		e.bell()
		return true
	}
	// The paste's window is OPEN: the reads that follow inside it are this paste's
	// tail (see pasteContinuation), and they go to this same file instead of being
	// typed. `ref.Bytes` is what the reference on the line claims, so the count is
	// kept for the reader: spillAppend rewrites the reference's size as the paste
	// grows (the file, not the line, is the paste's truth — but a reader must not
	// be told the paste was 4095 bytes when it was 65536).
	e.pasteOpen = ref.Path
	e.pasteBytes = ref.Bytes
	e.pasteRunes = ref.Runes
	e.pasteSource = e.pastes.Source()
	e.pasteUntil = e.now().Add(pasteContinuationGap)
	return true
}

// closePaste ends the open paste: nothing that arrives later is appended to it.
// It is called when a read turns out to be ordinary input, and whenever the
// editor returns a line (Run's own resets), so a finished paste can never absorb
// the next one.
func (e *Editor) closePaste() {
	e.pasteOpen = ""
	e.pasteBytes = 0
	e.pasteRunes = 0
	e.pasteSource = ""
	e.resetPasteBuf()
}

// spillAppend adds one continuation read to the open paste. It reports whether
// the bytes were filed: false means they must take the ordinary path (the reason
// was reported) — the paste is never silently truncated.
//
// The reference already on the line is REWRITTEN with the new size, so the size a
// reader sees is the size of the file (one place states it: the renderer, with
// the value the store's own count produced).
func (e *Editor) spillAppend(data []byte) bool {
	path := e.pasteOpen
	// Same framing rule as the first read (see spillPaste): this append path is
	// where the envelope's SECOND half usually lands — measured: a 42 KiB
	// bracketed paste filed its end marker 12 bytes into a later read, because a
	// 4095-byte read is smaller than the paste and the marker arrives with the
	// bytes that follow it.
	data = stripPasteMarkers(data)
	if len(data) == 0 {
		// A read that was ONLY the closing marker: the paste is complete, and
		// there is nothing to add to it. Reported as filed, so the marker is not
		// typed onto the line.
		e.pasteUntil = e.now().Add(pasteContinuationGap)
		return true
	}
	if err := e.pastes.appendBytes(path, data); err != nil {
		e.warn(fmt.Sprintf("the %d-byte tail of the paste could not be appended to %s (%v) — it was inserted into the message as text instead", len(data), path, err))
		e.pasteOpen = ""
		return false
	}
	old := PasteRefFor(PasteRef{Path: path, Bytes: e.pasteBytes, Runes: e.pasteRunes, Source: e.pasteSource})
	e.pasteBytes += len(data)
	e.pasteRunes += utf8.RuneCount(data)
	fresh := PasteRefFor(PasteRef{Path: path, Bytes: e.pasteBytes, Runes: e.pasteRunes, Source: e.pasteSource})
	// The replacement is a plain text substitution of the reference the editor
	// itself inserted. Value.ReplaceRange works in RUNE indices while
	// strings.Index reports a BYTE offset, so the offset is converted — a line
	// carrying a CJK reference would otherwise be spliced at the wrong place.
	// Nothing else on the line can carry that exact spelling: the reference holds
	// the absolute path the store just minted (the store refuses to hand out a
	// name twice).
	if at := strings.Index(e.val.Text(), old); at >= 0 {
		from := utf8.RuneCountInString(e.val.Text()[:at])
		e.val.ReplaceRange(from, from+utf8.RuneCountInString(old), fresh)
	}
	e.pasteUntil = e.now().Add(pasteContinuationGap)
	return true
}

// warn tells the user something they must know, without writing into the
// drawing stream (which would corrupt the layout). It is the Rule 15 landing
// spot for the editor's own degradations.
//
// IT GOES THROUGH degradation(), so a warning reaches BOTH destinations. The
// prefix is composed HERE and handed over as the finished sentence, so the
// screen's bytes are the ones this function has always written — the change is
// the second destination, not the first.
//
// WHY (measured, task 01a1a0ed, 2026-11-03): the diagSink was installed for
// `degradation()` only, and every one of the editor's SIX user-facing warnings
// calls warn() — so the sentence the operator was told on screen
// ("read an Enter as pasted content, not as a submit: …") reached NO record.
// `grep 'read an Enter as pasted' <session dir>` over the whole session
// returned nothing, and the sink was the ONE road that exists so a degradation
// outlives the terminal that showed it (the same defect task 01a18990 fixed
// for `degradation()`; it left warn() behind).
func (e *Editor) warn(msg string) {
	e.degradation("[gopi] editor: " + msg + "\n")
	e.repaintAfterDegradation()
}

// repaintAfterDegradation puts the layout's own rows back after a diagnostic has
// been written over them.
//
// WHY IT IS NEEDED AT ALL. errOut is not the layout's writer (it cannot be: the
// layout's bytes are a cursor-addressed stream and a diagnostic is a sentence),
// so a degradation lands on whatever rows the cursor was standing on and every
// LF inside the band's scrolling region scrolls the frame. The editor used to
// recover incidentally — the next keystroke repainted — which is exactly the
// "until something else happens" shape this package refuses elsewhere: the
// operator is left reading a status row with a sentence printed through it.
//
// WHAT IS REDRAWN IS THE ROWS A DIAGNOSTIC CAN DAMAGE, not the whole screen. The
// band's committed transcript is the answer renderer's and is scrolled by this
// write as a unit (the rows move, they are not mixed with the sentence's cells a
// row at a time), and re-drawing it is not something the band can do from here —
// but the CHROME can be put back exactly, because every one of its rows is
// placed ABSOLUTELY by the layout: the editing line is redrawn from the buffer
// (Renderer.Redraw erases and rewrites its own span), the menu from its
// published list, and the status row is invalidated so its next draw repaints a
// row that the sentence may have covered.
//
// IT RUNS ON THE READ LOOP ONLY. A repaint reads the editing model, and the
// package's invariant is that one goroutine does that — so a degradation from a
// foreign producer (a band-wait timeout in the REPL's goroutine) leaves the
// repaint to the loop's own next frame rather than racing it. The check is the
// goroutine identity, never a flag.
func (e *Editor) repaintAfterDegradation() {
	if e.region == nil || !e.region.Active() || !e.onLoopGoroutine() {
		return
	}
	if e.answer != nil {
		// The row may have been written over, so the renderer's "this is what the
		// row shows" record is no longer a fact. Clearing it is what makes the
		// next draw a WRITE instead of the optimisation that would skip it, and
		// the draw is taken HERE because the repaint below only reaches the
		// status row when a publish is pending — a diagnostic does not publish.
		e.answer.forgetDrawnStatus()
		e.answer.Tick()
	}
	e.repaint()
}

// SetDiagSink installs the RECORD a degradation is also written to (see the
// diagSink field). Nil — the zero value, and every caller with no record to
// write — keeps the historic stderr-only behaviour byte for byte.
func (e *Editor) SetDiagSink(f func(string)) { e.diagSink = f }

// emitWarnRows writes ONE diagnostic sentence to errOut WRAPPED TO THE CURRENT
// WIDTH — the destination the operator actually sees.
//
// WHY THIS IS NOT A PLAIN WriteString (measured 2026-11-10). errOut is stderr,
// written OUTSIDE the reserved layout: the terminal is inside the band's
// scrolling region with the cursor parked on the editing line, so a diagnostic
// goes down from wherever the cursor is, and every LF it carries scrolls. A
// ~300-cell sentence at 101 columns was therefore THREE ROWS of a fixed-height
// screen, and they covered the status row (the layout's last row) until the next
// keystroke repainted it. The row budget is the layout's, not the sentence's, so
// the fix is the layout's number: wrap with the ONE wrap policy this package
// already exports for exactly this ("a caller that streams to a terminal WITHOUT
// a band wraps to the same policy"), and, while the layout is installed, cap the
// rows at the one row the layout can spare — the status row is the only row
// outside the band and the editing line, so a diagnostic that took more than one
// row was taking the status row's.
//
// The record is unchanged: degradation() hands the sink the sentence VERBATIM
// (Rule 15 — a diagnostic must not be truncated in the record that outlives the
// screen), and only the SCREEN's bytes are folded.
//
// CRLF, not LF: raw mode cleared OPOST, so a bare "\n" moves down without
// returning the column and every continuation row would start where the previous
// one ended. Every row after the first is placed at column 0 explicitly.
func (e *Editor) emitWarnRows(sentence string) {
	text := strings.TrimRight(sentence, "\n")
	// The layout's own numbers decide the fold: its drawing width and the ONE
	// row it can spare (statusRows — the only row outside the band and the
	// editing line, and therefore the row a taller diagnostic covers).
	width, rows, active := 0, 0, false
	if e.region != nil && e.region.Active() {
		active = true
		width, rows = e.region.Cols(), statusRows
	}
	lines := WrapToCols(text, width)
	if rows > 0 && len(lines) > rows {
		lines = lines[:rows]
	}
	// CRLF only where the layout is installed (raw mode cleared OPOST, so a bare
	// LF would start every continuation row where the previous one ended). The
	// full-screen path keeps the historic bytes exactly — its own renderer
	// places the cursor absolutely before drawing.
	brk := escNewline
	if active {
		brk = escCarriage + escNewline
	}
	for i, line := range lines {
		if i > 0 {
			_, _ = io.WriteString(e.errOut, brk) //stylegate:ignore the diagnostic stream is the last reporting path
		}
		_, _ = io.WriteString(e.errOut, line) //stylegate:ignore the diagnostic stream is the last reporting path
	}
	_, _ = io.WriteString(e.errOut, brk) //stylegate:ignore the diagnostic stream is the last reporting path
}

// degradation reports ONE degradation twice: to the operator on errOut (the
// sentence that has always been written there) and, when a sink is installed, to
// the run's RECORD — so "the read loop is stalled?" survives the screen that
// showed it. The text is composed once, by the caller, so the two destinations
// cannot disagree about what happened.
func (e *Editor) degradation(sentence string) {
	if e.errOut != nil {
		e.emitWarnRows(sentence)
	}
	if e.diagSink != nil {
		// THE RECORD GETS THE SENTENCE VERBATIM, and that is not an oversight
		// (Rule 15): the screen's rows are folded to the width, the record must
		// stay greppable, and a record that inherited the fold would turn one
		// sentence into N lines with a carriage return in front of each.
		e.diagSink(sentence)
	}
}

// width is the current drawing width.
func (e *Editor) width() int { return e.src.Width(editColsFallback) }

// height is the current terminal height (the layout needs it).
func (e *Editor) height() int { return e.src.Height(editRowsFallback) }

// geometryChanged reports whether the source's size moved since the last redraw
// — the ONE discovery a bounded wait of the read loop can still make (a resize
// whose SIGWINCH was coalesced away), now that a publish announces itself
// through the wake seam. It also records what it saw, so a caller that acts on
// a resize is not asked again on the next idle wait. A source that cannot
// report a size keeps the last value (never a spurious redraw).
//
// The geometry record is updated HERE, the one place an idle wait asks the
// question, so it cannot drift from what the repaint drew.
func (e *Editor) geometryChanged() bool {
	w, h := e.width(), e.height()
	changed := e.geomKnown && (w != e.geomW || h != e.geomH)
	e.geomW, e.geomH, e.geomKnown = w, h, true
	return changed
}

// repaint redraws the editing line and everything below it. It is the single
// redraw entry point: the region's layout is refreshed first (the editing line's
// row count moves the band), then the line is drawn, then the menu. Nothing here
// touches the answer band or the status row — those belong to the answer
// renderer, which is why a keystroke can never repaint the model's answer.
func (e *Editor) repaint() {
	// Count the repaint itself, not the chunks it happens to emit: how a
	// redraw is split across write(2) calls is a property of the transport
	// (one bufio flush per line, or one per region move) and varies with
	// scheduling, while the thing a budget must bound is how many times the
	// editor decided to redraw. It is incremented here — the single redraw
	// entry point — so no caller can bypass it.
	e.repaints.Add(1)
	// THE TURN'S PUBLISHED CALLS ARE APPLIED FIRST — before the plan is resolved
	// and before any of it is drawn. An answer call COMMITS rows into the band
	// (writeRow → commitScroll), and the band's row count is half of what the plan
	// decides: the editing line's row and the menu's row come from it. Applying
	// them after the chrome was placed would leave the editing line and the menu
	// on rows the layout no longer gives them — the plan and the screen would
	// disagree for the rest of the frame (measured: cmd/agent's menu surface read
	// the paint stream at rows the published layout no longer named).
	//
	// It is also the ORDER the turn produced: a delta that arrived before this
	// repaint belongs above the chrome, not below it. drainBand (the flow seam's
	// frames) stays at the END, where it has always been: a foreign producer's
	// panel is written after the chrome so its own body cannot erase the rows
	// just drawn.
	// THE QUEUED LINE INSERTS GO IN FIRST — before the plan is resolved and
	// before anything is read back off the model. An insert MUTATES the buffer,
	// and the rows this frame will occupy are derived from the buffer a few lines
	// below (e.val.RowsFor), so applying it anywhere later would draw the line at
	// the geometry of the text BEFORE it and leave the plan and the screen
	// disagreeing for the rest of the frame. It is the same reasoning that puts
	// drainAnswerOps above the plan, for the same class of defect.
	e.drainLineInserts()
	// The DRAFT SEGMENT is drained here, beside the line inserts and for the same
	// reason: a producer wrote it from another goroutine, so it is applied at the
	// top of a frame by the OWNER of the model — never by the producer itself
	// (draft.go, and the invariant lineinsert.go established).
	e.drainDraft()
	e.drainAnswerOps()
	if e.region != nil && e.region.Active() {
		// The rows the PREVIOUS layout's menu occupies are erased BEFORE the
		// layout moves. A menu is placed absolutely, so the moment the layout
		// shifts (the band contracts, the editing line moves down) those rows
		// belong to the editing line — and erasing them afterwards wipes the
		// line that was just drawn onto them (measured: the typed text vanished
		// as soon as the menu drew).
		e.region.eraseMenuPaint()
		// The layout is resolved BEFORE anything is drawn: the band gives rows
		// to the editing line and to the menu, and the editing line's own row
		// MOVES when the menu opens. Drawing first and shrinking after puts the
		// line on the row the new layout gave to the band (measured: the typed
		// text ended up under the menu).
		//
		// THE EDITING LINE'S ROW BUDGET IS DERIVED FROM THE TERMINAL HEIGHT, not
		// from a fixed cap: the band keeps its floor (regionRowsMin) and every
		// row above it is the user's to type into, so the editor grows as the
		// text wraps and the user always reads their own input. The budget is
		// this region's own number (EditorRowsCap), so what the editor asks for
		// and what computePlan reserves cannot disagree — and the renderer
		// clips to the resolved plan (EditingRows) regardless.
		//
		// The wish is the rows the buffer REALLY occupies, bounded by the
		// region's derived budget — never more than the layout can host, and
		// never fewer than the truth: asking for less than the truth would leave
		// the layout believing a shorter line while the renderer draws the real
		// one, which is a disagreement between plan and screen.
		rows := e.val.RowsForDraft(e.render.prompt, e.Draft(), e.width())
		if budget := e.region.EditorRowsCap(); rows > budget {
			rows = budget
		}
		e.region.SetEditorRows(rows)
		e.publishMenuHeight()
	}
	// The renderer is told the draft it must lay out on the frame it is about to
	// draw, from the same source the rows wish above was computed from — so the
	// plan, the layout and the emphasis cannot describe three different lines.
	// THE DRAFT IS READ THROUGH ITS OWN ACCESSOR: SetDraft is called from any
	// goroutine (a voice producer's, a test's), so the field is guarded by
	// e.drafts.mu — a bare `e.drafts.text` here is a read with no happens-before
	// against that write, and -race reported exactly this pair (measured
	// 2026-10-27, task 01a1756e): Write by Editor.SetDraft (draft.go:91, via
	// chatInput.VoiceDraft) vs Previous read by Editor.repaint (this line).
	e.render.SetDraft(e.Draft())
	e.render.Redraw(e.val, e.width())
	e.paintMenu()
	// The foreign-producer rows go in LAST — after the chrome below the band has
	// been placed absolutely, so a band row cannot be drawn over the editing line
	// or the status row, and after the frame that was just drawn, so its own body
	// does not erase the rows this drain commits.
	//
	// It is THE WRITE the flow seam's producer waits on (WriteLines): whatever
	// that goroutine handed over is on the screen — and every read of the
	// editing model this drain performs (the region's band width, its live row,
	// the editing line's own row) happens on THIS goroutine, which is the same
	// one that runs Run and Reset the buffer.
	e.drainBandFrames()
	// THE CARET GOES BACK LAST. The band write above ends by parking the cursor
	// on the band's LIVE row (region.writeBandLine), and Redraw placed the caret
	// before it — so a repaint that carried a row left the hardware block one row
	// ABOVE the input, where the operator saw it and where it stayed until the
	// next keystroke. Re-placing the caret after every other writer of the frame
	// is what makes "the cursor is on the input" true of the FRAME, not only of
	// the part of it the renderer owns (render.go's PlaceCaret carries the
	// measurement).
	e.render.PlaceCaret()
	// The repaint is the ONLY thing that makes a menu-state change visible, so
	// it is also what consumes the request for one. Clearing it HERE (rather
	// than at each call site) is what keeps a flag from outliving the redraw it
	// asked for: a stale "the menu changed" is a permission for a future
	// redraw that has nothing left to draw, i.e. exactly the repaint-per-poll
	// this editor must not do.
	//
	// UNLESS the publish this repaint acknowledged was not drawn after all — a
	// DRAFT on the request that was just consumed. clampedMenu acknowledges the
	// generation by READING it, and it is reached from publishMenuHeight in the
	// middle of this repaint, so a worker that publishes between that read and
	// paintMenu's draw (menu.go's worker stores the unread flag AFTER installing
	// the menu, so the flag can be set after noteDrawn already cleared it)
	// leaves the page looking clean while the candidates are published and
	// undrawn. The level is therefore re-derived from the generations, not
	// assumed: a repaint that did not draw the current publish keeps the request
	// alive, and the next read outcome draws it.
	if e.menuUndrawn() {
		e.menuDirty = true
	}
}

// publishMenuHeight tells the layout how many rows the published menu wants, so
// the editing line's row and the menu's row are resolved from the geometry that
// will really be on screen. It is a no-op without a menu.
func (e *Editor) publishMenuHeight() {
	if e.menu == nil {
		return
	}
	// The published menu is the ONE place the menu's visible height is decided
	// (clampedMenu), so the rows the layout reserves and the rows that are
	// painted cannot disagree: a menu taller than the room left would land on
	// the status row. The candidates are not dropped — the visible window
	// shrinks and follows the selection — and the generation the menu was read
	// at is remembered, so the repaint can tell "a publish I have not drawn".
	e.region.SetMenuRows(e.clampedMenu().Rows())
}

// clampedMenu is the published menu reduced to the rows the layout can host
// below the editing line, together with the generation it was published under.
// It is the ONE place the menu's visible height is decided, so the rows the
// layout reserves and the rows that are painted cannot disagree (a menu taller
// than the room left would land on the status row; the candidates are not
// dropped, the visible window shrinks and follows the selection).
func (e *Editor) clampedMenu() Menu {
	rev, m := e.menu.publishedRev()
	if rev != e.menuRev {
		// The publish generation moved: a candidate list was published that no
		// repaint has drawn. READING it is not drawing it, so the generation is
		// acknowledged only together with the LEVEL the draw leaves behind —
		// menuDirty. Acknowledging the generation alone (e.menuRev = rev) is
		// how this window was lost: the read loop can be parked in an UNBOUNDED
		// read when the publish lands (wakes=0, lastTimeouts=[-1 -1 -1] —
		// measured under -race on menu_test.go's EscapeClosesTheMenu, ~1 in
		// 30-60), and an unbounded read has no expiry to re-check anything. The
		// menu then stayed published-but-undrawn until the NEXT keystroke.
		//
		// menuDirty is the READ LOOP's own flag, so it is testable at the
		// boundary that actually matters, and it is cleared by the repaint that
		// DRAWS the menu — never by the acknowledgement above (see repaint's
		// tail: the menu's own paint clears it, the acknowledge-only path does
		// not). That is what makes the signal LEVEL-triggered rather than a
		// one-shot notification: a draw that is not reached leaves the level
		// raised, so the next iteration repaints, whatever woke it.
		e.menuDirty = true
	}
	e.menuRev = rev
	if cap := e.region.MaxMenuRows(e.val.RowsFor(e.render.prompt, e.width())); m.Rows() > cap {
		m = m.WithPage(cap)
	}
	return m
}

// paintMenu draws the completion menu for the current layout. It is the ONE
// place the menu is painted: the repaint path and a selection move (which does
// not touch the buffer, so it never reaches the repaint gate) both go through
// it, so neither can leave the layout believing a menu height that is not on
// screen.
func (e *Editor) paintMenu() {
	if e.menu == nil || e.region == nil || !e.region.Active() {
		return
	}
	m := e.clampedMenu()
	if e.region.SetMenuRows(m.Rows()) {
		// The layout moved while the menu was being sized (the editing line's
		// row count changed with the candidates): the line was drawn for the
		// previous placement, so put it back on the row the layout now gives
		// it before painting the menu.
		e.render.Redraw(e.val, e.width())
	}
	e.menuDirty = false
	m.draw(e.region, e.render, e.width())
	// The drawn menu is now on screen: clear the worker's unread flag so its
	// wake does not schedule a second repaint for a list already drawn.
	e.menu.noteDrawn()
	// A published status is drawn HERE, on the read loop's own repaint — the
	// caller that published never writes (see PublishStatus). Drawing it last
	// is deliberate: the status row is below the menu, and the region's
	// placement is absolute, so nothing above it is disturbed.
	if e.statusPending() && e.answer != nil {
		e.answer.Tick()
	}
}

// startLine arms a fresh line: the editing row count resets, the menu closes and
// the answer layout is refreshed. A new line never inherits the previous line's
// completion state.
func (e *Editor) startLine() {
	if e.menu != nil {
		// cancel (not publish): a request left in flight by the PREVIOUS line
		// must not reopen a menu for it on this line. The cancel is itself a
		// publish (rev moves), so the repaint that follows this call both shows
		// the closed menu and consumes the change — menuDirty is not touched
		// here (it belongs to repaint alone).
		e.menu.cancel()
	}
	if e.region != nil && e.region.Active() {
		e.region.SetEditorRows(1)
		e.region.SetMenuRows(0)
	}
}

// scrollWindow applies a PAGED scroll key to the editing line's window (PageUp/
// PageDown) and repaints only when the window actually moved (a key that changes
// nothing must not cost a repaint). up is the direction: true shows EARLIER text.
func (e *Editor) scrollWindow(up bool) {
	if !e.scrollEditingLine(up, true) {
		return
	}
	e.repaint()
}

// scrollWindowByOneRow is the SINGLE-ROW scroll (Ctrl+P/Ctrl+N, Alt+Arrow): the
// step is one buffer row, and the key is inert when the window is already at that
// end (the caller then falls through to the key's other meaning).
func (e *Editor) scrollWindowByOneRow(up bool) {
	if !e.scrollEditingLine(up, false) {
		return
	}
	e.repaint()
}

// scrollWindowIfClipped is the ARROW's arm: it scrolls the window by ONE row
// when the editing line has rows the window does not show. It reports whether a
// REDRAW is owed — false when the window is already at that end — and is never
// asked whether the arrow owns the key (that question is windowHidesRows, and the
// caller must ask it FIRST, before it considers the history: the two questions are
// different, and reading this return value as the first one is how a multi-line
// draft got replaced by a recalled line).
func (e *Editor) scrollWindowIfClipped(up bool) bool {
	if !e.windowHidesRows() {
		return false
	}
	if !e.scrollEditingLine(up, false) {
		// The window is already at that end: nothing moved, so there is nothing
		// to repaint. The key stays the WINDOW's key — an inert press at the edge
		// of the user's own text, never a hidden second meaning.
		return false
	}
	e.repaint()
	return true
}

// handleKey applies one key. done means the line is finished.
func (e *Editor) handleKey(k Key) (Result, bool, error) {
	if k.Code != KeyEnter && k.Code != KeyNewline {
		// Any other key breaks a CR LF pair's adjacency.
		e.crPending = false
	}
	switch k.Code {
	case KeyRune:
		if k.Alt {
			return Result{}, false, nil // bare Alt+<char>: not a binding we own
		}
		// A refused insertion is reported to the user (a bell), never silently
		// dropped — see Value.InsertRune.
		if !e.val.InsertRune(k.Rune) {
			e.bell()
		}
	case KeyNewline:
		// Alt+Enter / Shift+Enter / a newline that arrived inside a bracketed
		// paste: content, never a submit.
		if e.crPending && !k.Alt {
			// A CRLF paste: the LF of a CR LF pair is the same line break as
			// the CR that preceded it (the CR was already inserted), so this
			// one is dropped rather than producing a blank line.
			e.crPending = false
			return Result{}, false, nil
		}
		e.crPending = false
		e.val.InsertRune('\n')
	case KeyEnter:
		// THE OPEN MENU OWNS ENTER. The menu is a list of choices; a user who
		// has walked it with Up/Down and presses Enter means "take this one",
		// exactly as the web carrier's composer does (cli_web.go: "Enter never
		// sends while the menu is open — the only way to send is to accept
		// first"). Without this, Enter submitted the RAW typed prefix and the
		// selection was discarded, which made the menu a photograph rather than
		// a control.
		//
		// It is decided BEFORE the paste-burst classification: accepting the
		// candidate REPLACES the line with a command name (no trailing break),
		// so the buffer the user sees is the buffer the next Enter submits, and
		// a line that is no longer a paste has nothing for Value.Enter to
		// suppress.
		//
		// A selection that does NOT change the line is not a choice: the
		// completer re-publishes candidates for the completed line (so "/task"
		// leaves a menu offering exactly "/task"), and an Enter that accepted
		// that would leave the user unable to send a command whose own name is
		// in the list. The buffer comparison is the honest test of "did this
		// Enter take a choice", so the menu cannot swallow the submit key.
		if e.MenuState().Open {
			e.crPending = false
			before := e.val.Text()
			e.MenuAccept()
			if e.val.Text() != before {
				break
			}
		}
		if !e.val.Enter(k.Origin, e.now()) {
			e.crPending = false
			// The burst is over — this Enter ENDED it — so the throttle is
			// released here too, before the submit path has a chance to reject
			// the line (a rejected submit is still the end of a paste).
			e.pasteWarned = false
			return e.submit()
		}
		e.crPending = true
		// Suppressed: the Enter was pasted content, not a submit — the newline
		// is already in the buffer (and marked the editor dirty, so the redraw
		// below renders it).
		//
		// AND IT IS ANNOUNCED — ONCE. Reading an Enter as content when the
		// operator pressed a key called Enter is exactly the silent failure
		// Rule 15 forbids: without a line, "my Enter did nothing" and "the
		// program is wedged" look identical, and the operator's only recourse is
		// to guess. The throttle is what makes it usable — a pasted 50-line
		// block suppresses 50 newlines, and 50 identical lines of diagnostic on
		// a drawing surface would be noise, not information. So the FIRST
		// suppression of a burst speaks for all of them.
		e.pasteSuppressed++
		if !e.pasteWarned {
			e.pasteWarned = true
			e.warnPasteSuppressed()
		}
	case KeyInterrupt:
		// WHAT Ctrl+C MEANS DEPENDS ON WHETHER A TURN IS IN FLIGHT (measured,
		// task 01a0e4390): with an answer streaming, the key is "abandon THIS
		// turn" — the caller's interrupt target cancels it and the session stays
		// open, so Run keeps reading and the operator types the next prompt
		// normally. At the prompt, with nothing in flight, the key keeps its
		// historic meaning (Result.Interrupted → the REPL's clean exit), and a
		// caller that installed no target gets that meaning unconditionally.
		if e.InterruptTurn(OriginCtrlC) {
			e.val.Reset()
			e.repaint()
			break
		}
		e.render.Finish()
		return Result{Interrupted: true}, true, nil
	case KeyEOF:
		if e.val.Len() == 0 {
			// MID-TURN THE KEY STILL HAS TO CLAIM THE TURN (task 01a1650f).
			// The marker below is the operator's "end the input", and the REPL
			// answers it by leaving — but it can only answer at a turn boundary,
			// and the loop is blocked INSIDE the turn until it returns on its
			// own. Measured against a peer that never answers, the loop never
			// got there and the reader goroutine then retired on the EOF, so the
			// console stopped reading the terminal entirely: the Ctrl+C the
			// operator pressed 30s later did nothing (pty probe 2026-10-23).
			// Claiming the turn first means the queued marker is read at the
			// boundary the cancel creates, instead of at a boundary that may
			// never come.
			e.InterruptTurn(OriginEOF)
			e.render.Finish()
			return Result{EOF: true, EOFKind: EOFKindKey}, true, nil
		}
		e.val.DeleteForward()
	case KeyBackspace:
		if !e.val.Backspace() && e.hist != nil {
			e.val.SetText("") // at column 0: nothing to delete
		}
	case KeyDelete:
		e.val.DeleteForward()
	case KeyLeft:
		e.val.MoveLeft()
	case KeyRight:
		e.val.MoveRight()
	case KeyUp:
		if e.MenuState().Open {
			// While the completion menu is open, Up/Down move the selection
			// (a menu is a list; history recall resumes when it closes).
			e.MenuSelect(-1)
			break
		}
		// WHETHER ANYTHING IS HIDDEN DECIDES WHOSE KEY THIS IS — never whether the
		// window moved. With hidden rows the arrow belongs to the WINDOW: it scrolls
		// while there is somewhere to go, and is INERT at the end. It must not touch
		// the history there, because recall REPLACES the buffer (Value.SetText) and
		// the operator's multi-line draft has no second copy — reading the scroll's
		// "did not move" as "not my key" lost that text on a single press.
		if e.windowHidesRows() {
			e.scrollWindowIfClipped(true)
			break
		}
		e.recall(func() (string, bool) { return e.hist.Prev(e.val.Text()) })
	case KeyDown:
		if e.MenuState().Open {
			e.MenuSelect(1)
			break
		}
		if e.windowHidesRows() {
			e.scrollWindowIfClipped(false)
			break
		}
		e.recall(func() (string, bool) { return e.hist.Next() })
	case KeyPageUp:
		e.scrollWindow(true)
	case KeyPageDown:
		e.scrollWindow(false)
	case KeyScrollUp:
		e.scrollWindowByOneRow(true)
	case KeyScrollDown:
		e.scrollWindowByOneRow(false)
	case KeyHome:
		e.val.Home()
	case KeyEnd:
		e.val.End()
	case KeyWordLeft:
		e.val.MoveWordLeft()
	case KeyWordRight:
		e.val.MoveWordRight()
	case KeyKillToStart:
		e.val.KillToStart()
	case KeyKillToEnd:
		e.val.KillToEnd()
	case KeyKillWordBack:
		e.val.KillWordBack()
	case KeyKillWordForward:
		for ok := true; ok; {
			ok = e.val.DeleteForward()
		}
	case KeyYank:
		e.val.Yank()
	case KeyClearScreen:
		e.render.Control(escClearScreen)
		e.render.Reset()
		e.render.Redraw(e.val, e.width())
	case KeyTab:
		// Tab accepts the completion menu's selection. With no menu it is
		// deliberately inert: a chat message is not indented source code, and
		// inserting a tab would silently change the text the model receives.
		e.MenuAccept()
	case KeyEscape:
		// A lone Escape (the decoder only reports it after its bounded wait) is
		// the PURE INTERRUPT while a turn is in flight, and the completion
		// menu's close key otherwise.
		//
		// THE TURN ARM COMES FIRST, and the ORDER is the whole difference
		// between the two: a person who presses Escape while an answer is
		// streaming means "stop this" — they are not closing a candidate list
		// they cannot be reading. Claiming the key here leaves the EDITING
		// BUFFER ALONE (the abandoned line is what they were typing next, not
		// part of the turn that was cancelled) and keeps the read loop going, so
		// the session survives with the prompt exactly where it was.
		//
		// A claim that comes back FALSE (the turn ended in the same instant the
		// byte arrived) falls through to the historic meaning rather than
		// vanishing (Rule 15) — and the historic meaning is this same MenuClose,
		// so the arm has one effect whether or not the turn was claimed: only
		// the CANCEL is conditional.
		e.InterruptTurn(OriginEscape)
		e.MenuClose()
	case KeyPasteStart:
		// Bracketed paste marker, and — when it reaches the DECODER, i.e. on a
		// source that does not spill (no session directory) — that is all it is
		// HERE: the marker is not content. The EVIDENCE it carries was already
		// taken by applyRead (withoutPasteMarkers), which marked the burst as
		// FRAMED — the terminal stated "this block is a paste", so the burst it
		// opens keeps PasteEnterSuppressWindow however few keys the block carried
		// (Value.NoteBurst). On the spill path the same marker opens the envelope
		// first, and the read that then reaches this arm carries it too.
	case KeyPasteEnd:
		// The bracketed-paste region is over. The burst window remains armed for
		// the window its EVIDENCE carries (Value.burstWindow: the full
		// PasteEnterSuppressWindow when the terminal framed it, PasteBurstCharInterval
		// when only the read's size did), which is exactly the state a following
		// Enter must be judged against — so there is nothing to flush here.
	case KeyNone:
		// Unhandled control byte or an unknown sequence — dropped, never
		// inserted as text.
	}
	return Result{}, false, nil
}

// submit finishes the line: it leaves the editing row, records the text in the
// recall ring, and reports it. A history write failure is reported to the
// caller — a silently lost recall entry is the kind of failure that looks like a
// user complaint with no evidence (Rule 15).
func (e *Editor) submit() (Result, bool, error) {
	line := e.val.Text()
	// The whole message must be ON SCREEN as the user's own text before the
	// live row takes over: the display cap above clips long input, so the
	// submitted line is redrawn uncapped here (and the band gives way), so a
	// multi-line message is visible in full rather than half-drawn.
	if e.region != nil && e.region.Active() {
		if rows, _, _ := layoutText(e.render.prompt, line, len([]rune(line)), e.width()); len(rows) > 0 {
			// The submitted message is drawn WHOLE (the band gives way): so the
			// window is deliberately unpinned AND unclipped here — what leaves the
			// editing line as the request row must be the entire text.
			e.render.unpinWindow()
			e.region.SetEditorRows(len(rows))
			e.render.Redraw(e.val, e.width())
		}
	}
	e.MenuClose()
	e.render.Finish()
	if e.hist != nil && strings.TrimSpace(line) != "" {
		if err := e.hist.Add(line); err != nil {
			return Result{Line: line}, true, err
		}
	}
	return Result{Line: line}, true, nil
}

// recall applies a history motion; an empty history (or no history at all) is a
// no-op.
func (e *Editor) recall(next func() (string, bool)) {
	if e.hist == nil {
		return
	}
	if text, ok := next(); ok {
		e.val.SetText(text)
		// A recalled line is a NEW line to edit: the window follows its caret, so
		// the recalled text the user is about to work on is what they see (a pin
		// from the previous line would show its middle).
		e.render.unpinWindow()
	}
}

// bell signals a refused input (the buffer bound). Best effort: it is feedback,
// not state.
func (e *Editor) bell() {
	if e.out != nil {
		_, _ = io.WriteString(e.out, "\a") // deliberately ignored: feedback only
	}
}

// SetMinTurnActive installs the "a turn is in flight" predicate, and
// SetInterruptTurn installs the action to take when a turn-abandoning key
// arrives DURING one. Both are one seam with two halves, because the key's
// meaning needs both: the predicate says whether the key means "abandon the
// turn", and the action says how.
//
// WHY THE EDITOR CANNOT DECIDE ALONE (measured, task 01a0e4390). A raw-mode
// terminal clears ISIG, so Ctrl+C arrives as the 0x03 byte and is classified
// here — but only the caller knows whether an answer is streaming at that
// instant. The editor states what it observed; the REPL states what it means.
//
// TWO KEYS REACH THIS SEAM, and they mean the same thing (task 01a0e84b): a
// LONE ESCAPE while a turn is in flight is "abandon this turn" — the pure
// interrupt, which cancels the turn, keeps whatever was typed, and carries no
// new input of its own. With no turn in flight Escape keeps its historic
// meaning exactly: it closes the completion menu, and with no menu open it is
// inert (never a session exit, which is Ctrl+C's and Ctrl+D's to do).
//
// A nil action (the zero value, and every caller with no turns at all) keeps
// every historic meaning unconditionally: Escape is the menu's close key, and
// Ctrl+C is Result.Interrupted, which the chat REPL treats as a clean exit. That
// is what protects the at-the-prompt semantics this feature must not change
// (Rule: at-the-prompt Ctrl+C still exits).
func (e *Editor) SetMinTurnActive(active func() bool) { e.minTurnActive = active }

// SetInterruptTurn installs the action a turn-abandoning key (Ctrl+C, or a lone
// Escape) triggers while a turn is in flight. It is told WHICH input carried the
// key (KeyOrigin) and reports whether it CLAIMED it: true means a turn was
// really cancelled (the editor then keeps reading), false means there was
// nothing to abandon after all — and the key falls back to its historic meaning,
// so a race between "the turn ended" and "the key arrived" can never silently
// swallow the operator's keystroke (Rule 15).
func (e *Editor) SetInterruptTurn(interrupt func(KeyOrigin) bool) { e.interruptTurn = interrupt }

// MinTurnActive reports whether a turn is in flight, per the predicate installed
// by SetMinTurnActive. False without one: the caller has no turns, so every key
// keeps its historic meaning.
func (e *Editor) MinTurnActive() bool {
	return e.minTurnActive != nil && e.minTurnActive()
}

// InterruptTurn claims a turn-abandoning key for the turn in flight: it is the
// ONE place the two halves of the seam are combined, so no key loop can read the
// predicate and skip the action (or the reverse). origin says which input
// carried the key, so the caller's marker and its session record name the key
// the operator actually pressed.
func (e *Editor) InterruptTurn(origin KeyOrigin) bool {
	return e.MinTurnActive() && e.interruptTurn != nil && e.interruptTurn(origin)
}
