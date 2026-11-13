package lineedit

import (
	"fmt"
	"sync"
	"time"
)

// lineinsert.go — TEXT INTO THE EDITING LINE, from a goroutine that is NOT the
// read loop.
//
// WHY THIS IS A QUEUE AND NOT A WRITE. The editing model has ONE writer while a
// read loop runs: the goroutine inside Editor.Run, whose drains and repaints are
// its own work (valueowner.go carries the invariant and the two measured windows
// that produced it). A producer that called Value.InsertString from another
// goroutine would be a SECOND writer of the buffer — and it would also have to
// draw, which is the second reader of the model the whole package forbids. So
// the road is the one queueBand established for the band's rows: the FOREIGN
// caller queues DATA (a string, never a terminal byte and never a read of the
// model), raises a LEVEL, and wakes the loop; the loop APPLIES it on its own
// goroutine, inside repaint, so the very frame that follows the mutation draws
// the text that mutation produced.
//
// WHAT IT IS FOR. A producer outside the key path that has text the operator
// should be able to EDIT before submitting it: a transcriber appending a
// recogniser's output to whatever the operator has already typed. The upload
// counter-example is stated because it is the reason this is not a second
// "type the next turn" seam: Editor.submit returns a WHOLE LINE as the next
// turn, and the self-driven seam (internal/agent ChatSurfaceTakePending) exists
// for a model that types a turn. Neither lets a human keep editing the text —
// an appended transcript the operator cannot backspace is not input, it is an
// announcement.
//
// CURSOR PLACEMENT, and why it is the cursor's position rather than the end of
// the buffer: the insert goes in AT THE CURSOR through Value.InsertString, the
// same call the completion menu's own buffer edit uses, and the cursor lands
// after the inserted text (Value.spliceRunes advances it by the inserted
// length). An operator who has moved the caret into the middle of a sentence
// therefore gets the transcript where they put it, not appended past the end of
// a draft they were still working in. The consequence is stated rather than
// hidden: appending to the END of the line is the caller's to arrange — park the
// caret with Value.End (the End key) before the insert, and the two compose to
// the append shape without a second method or a mode flag here.

// lineInsertQueueCap is how many inserts may wait for the read loop at once. It
// is a bound on the PRODUCER's backlog, so a caller that outruns the loop is
// refused loudly (InsertIntoLine reports false and counts it, see
// LineInsertRefusals) rather than growing a queue the loop can never drain —
// the same discipline the injected-key queue keeps, and for the same reason
// (Rule 15: a refused insert is reported, never silently dropped).
const lineInsertQueueCap = 64

// lineInsertWaitSteps / lineInsertWaitStep bound a foreign caller's wait for the
// read loop to APPLY an insert (SettleLineInserts). It is a COUNT of steps, so
// the waiter needs no clock, and it is a SYNCHRONIZATION bound rather than a
// measured duration: reaching it means the read loop is gone (a torn-down
// editor, an exited loop), never that it is slow (Rule 27 — no load is
// generated and no assertion rides a duration).
const (
	lineInsertWaitSteps = 4000
	lineInsertWaitStep  = 1 * time.Millisecond
)

// LineInsertPlacement says WHERE an inserted text lands in the editing line. It
// is part of the queue's DATA (not a mode flag on the editor) so two producers
// with different intentions can never be served by whichever one queued last.
type LineInsertPlacement uint8

const (
	// LineInsertAtCursor inserts at the caret. It is the ZERO value on purpose:
	// a caller with no opinion means the ordinary "type it here" behaviour, and
	// an unset placement cannot be mistaken for a missing one (Rule 15a).
	LineInsertAtCursor LineInsertPlacement = iota
	// LineInsertAtEnd parks the caret at the END of the line and then inserts, i.e.
	// APPENDS. It exists because the producer cannot do it itself: the caret lives
	// in the editing model, which only the read loop may write (valueowner.go), so
	// a transcriber that wants its text after what the operator has typed must say
	// so HERE — reaching for Value.End from its own goroutine would be the second
	// writer this whole seam exists to avoid.
	LineInsertAtEnd
)

// lineInsert is ONE queued text with the placement its producer asked for, and
// its PROVENANCE.
type lineInsert struct {
	text  string
	place LineInsertPlacement
	// dictated marks the text as a recogniser's output, so the runes it inserts
	// keep dictation provenance (valuevoice.go). It travels with the text for the
	// same reason `place` does: the two are ONE producer's decision, and a flag
	// stored on the editor instead would be read by whichever insert happened to
	// be applied next.
	//
	// THE ZERO VALUE IS "NOT DICTATED", which is the honest default for a
	// producer that says nothing: a completion menu's text and a caller's own
	// line are the operator's, and a missing field must never make one of them
	// claim to be a transcript.
	dictated bool
}

// lineInsertQueue is the pending inserts plus the level that says they are
// waiting. The mutex guards the QUEUE and the LEVEL only — data two goroutines
// legitimately share — and never the editing model, which only the read loop
// touches.
type lineInsertQueue struct {
	mu sync.Mutex
	// pending holds the texts waiting to be applied, in the producer's order.
	pending []lineInsert
	// active is the LEVEL: at least one text is queued, or the loop is inside
	// the drain. Level-triggered and sticky, for the reason every publish in
	// this package is — the loop can be parked in an unbounded read when a
	// caller queues, so what carries the insert past that park is a flag the
	// loop RE-DERIVES on every read outcome, never a one-shot notification.
	active bool
	// flushing marks the read loop as being inside the drain, so a nested
	// producer does not wait for a later drain of a queue only it can empty.
	flushing bool
	// refused counts the inserts this editor has REFUSED (an over-full backlog).
	// The refusal is a fact the caller learns from InsertIntoLine's return, and
	// the count is what makes it observable from the outside too (Rule 15).
	refused int
}

// InsertIntoLine queues text to be inserted into the editing line AT THE CURSOR
// (see the file header for the placement and for the append shape). It is safe
// to call from any goroutine, including one that is not the read loop.
//
// It reports whether the text was ACCEPTED — queued for the read loop when one
// is running, applied directly when none is (the same road the band seam takes
// for a caller no loop owns). False means the text was NOT inserted: an empty
// string, a nil editor, or a backlog already at lineInsertQueueCap. A false is
// never a silent no-op: the caller reports it, and LineInsertRefusals counts it.
//
// A text the BUFFER refuses (its own rune/byte bound) is not this seam's false:
// the write happened, the tail did not fit, and the loop says so through the
// editor's own warning channel (e.warn) as it applies it.
func (e *Editor) InsertIntoLine(text string) bool {
	return e.InsertLineText(text, LineInsertAtCursor)
}

// InsertIntoLineAtEnd queues text to be APPENDED to the editing line (see
// LineInsertAtEnd for why the placement must be decided here rather than by the
// caller moving the caret). Same contract as InsertIntoLine.
func (e *Editor) InsertIntoLineAtEnd(text string) bool {
	return e.InsertLineText(text, LineInsertAtEnd)
}

// InsertDictationAtEnd appends a RECOGNISER'S text to the editing line and
// records which runes it produced.
//
// IT IS A SEPARATE ENTRY POINT rather than a flag on the two above, because the
// question the call answers is a different one: not "where does this text go"
// but "whose characters are these". A caller that wanted only the placement
// would otherwise have to pass `false` for a fact it never measured, and the
// one producer that DOES measure it (the voice mode) is the one that must not
// be able to forget.
//
// The provenance it records is what lets the submitted line say "this was
// dictated" without ever asking "was voice mode on" — an operator who deletes
// the transcript and types their own leaves no dictated rune behind, and the
// count goes to zero by the ordinary buffer mutations rather than by a rule of
// its own (valuevoice.go).
func (e *Editor) InsertDictationAtEnd(text string) bool {
	return e.insertLineText(text, LineInsertAtEnd, true)
}

// InsertLineText is the ONE insertion entry point: it decides whether the caller
// owns the editor (and so performs the edit itself) or must hand it to the read
// loop, and it carries the PLACEMENT to whichever of them applies. Its text is
// the CALLER'S OWN — every rune is stamped plain (valuevoice.go).
func (e *Editor) InsertLineText(text string, place LineInsertPlacement) bool {
	return e.insertLineText(text, place, false)
}

// insertLineText is InsertLineText with the PROVENANCE decision made: dictated
// text keeps its dictation bits, everything else is stamped plain.
func (e *Editor) insertLineText(text string, place LineInsertPlacement, dictated bool) bool {
	if e == nil || text == "" {
		return false
	}
	ins := lineInsert{text: text, place: place, dictated: dictated}
	if e.onLoopGoroutine() || e.loopGoid.Load() == 0 {
		// No read loop owns this editor (a direct-drive caller, or the loop
		// itself): the caller owns the renderer, so it performs the edit and the
		// draw — the same choice AnswerRenderer.routed makes for a published
		// answer call, and the reason a unit test can assert an insert without
		// building a goroutine around it.
		e.applyLineInsertText(ins)
		e.repaint()
		return true
	}
	if !e.queueLineInsert(ins) {
		return false
	}
	e.waitLineInsert()
	return true
}

// queueLineInsert appends the text to the backlog and raises the level, then
// wakes the parked read when a wake seam is installed. False = the backlog is
// full (counted; nothing was queued).
func (e *Editor) queueLineInsert(ins lineInsert) bool {
	e.lines.mu.Lock()
	if len(e.lines.pending) >= lineInsertQueueCap {
		e.lines.refused++
		e.lines.mu.Unlock()
		return false
	}
	e.lines.pending = append(e.lines.pending, ins)
	// The level is raised BEFORE the wake: a notification that is missed can
	// only cost the text a bounded delay, while a text queued and never flagged
	// would wait for the next keystroke (Rule 15).
	e.lines.active = true
	e.lines.mu.Unlock()
	// The wake seam is resolved through the package's ONE accessor (the editor
	// holds whatever its source offered — see SetNotification), and a source
	// without one is silent here on purpose: the level still carries the insert,
	// so the worst case is the delayed cadence the level exists to bound.
	if e.notify != nil && e.notify.NotifyEnabled() {
		e.notify.Notify()
	}
	return true
}

// lineInsertPending reports whether an insert is waiting to be applied, or is
// being applied right now. It is a PEEK: the drain lowers the level, so asking
// in two places cannot drop an insert.
func (e *Editor) lineInsertPending() bool {
	e.lines.mu.Lock()
	defer e.lines.mu.Unlock()
	return e.lines.active
}

// LineInsertPending reports whether a queued insert has not been applied yet.
// A foreign caller that must know "is my text in the buffer?" asks this (or
// SettleLineInserts, which waits for it).
func (e *Editor) LineInsertPending() bool {
	if e == nil {
		return false
	}
	return e.lineInsertPending()
}

// LineInsertRefusals reports how many inserts this editor has refused (an
// over-full backlog). Zero means every accepted text was queued; the caller that
// saw a false can quote this rather than infer why.
func (e *Editor) LineInsertRefusals() int {
	if e == nil {
		return 0
	}
	e.lines.mu.Lock()
	defer e.lines.mu.Unlock()
	return e.lines.refused
}

// drainLineInserts applies every queued text, IN ORDER, on the caller's
// goroutine — which is the read loop. It reports whether it applied one.
//
// IT RUNS AT THE TOP OF repaint, before the plan is resolved and before the
// buffer is drawn. That position is the whole reason this seam needs no second
// draw: the insert mutates the model, and the repaint that called the drain then
// reads that model for its rows and its text, so the frame the terminal receives
// IS the frame after the mutation. (Its sibling drains sit elsewhere on purpose:
// drainAnswerOps commits rows the plan depends on, and drainBandFrames writes
// after the chrome has been placed.)
//
// The drain is re-entrant by construction, exactly as drainBand and
// drainAnswerOps are: the queue is taken under the lock and the application
// happens OUTSIDE it (an application can reach e.warn, which writes), so a text
// queued during the drain is applied by the loop's next pass.
func (e *Editor) drainLineInserts() bool {
	e.lines.mu.Lock()
	if e.lines.flushing {
		e.lines.mu.Unlock()
		return false
	}
	e.lines.flushing = true
	e.lines.mu.Unlock()
	applied := false
	for {
		e.lines.mu.Lock()
		if len(e.lines.pending) == 0 {
			// The queue is empty and no application is in flight: lower the
			// level HERE, under the same lock that saw it empty, so a producer
			// queueing after this line raises it again — the level can never be
			// lowered over a text that is still waiting.
			e.lines.active = false
			e.lines.mu.Unlock()
			break
		}
		ins := e.lines.pending[0]
		e.lines.pending = e.lines.pending[1:]
		e.lines.mu.Unlock()
		e.applyLineInsertText(ins)
		applied = true
	}
	e.lines.mu.Lock()
	e.lines.flushing = false
	e.lines.active = len(e.lines.pending) > 0
	e.lines.mu.Unlock()
	return applied
}

// applyLineInsertText performs ONE insert on the owner's goroutine: the model
// mutation and nothing else (the caller draws).
//
// A buffer that refuses part of the text is REPORTED through the editor's own
// warning channel rather than swallowed: the bound is the buffer's (Value's rune
// and byte limits), the text that did not fit is gone, and an operator whose
// transcript was cut short must learn it from the surface they are looking at
// (Rule 15b).
func (e *Editor) applyLineInsertText(ins lineInsert) {
	if e.val == nil {
		return
	}
	if ins.place == LineInsertAtEnd {
		// The caret is parked by the OWNER, one line before the insert: the
		// producer could not do it, and doing it as a separate step (a queue op
		// of its own) would let another producer's text land between the two.
		e.val.End()
	}
	refused := 0
	if ins.dictated {
		// THE PROVENANCE IS STAMPED AT THE MOMENT THE RUNES LAND, by the same
		// call that puts them in the buffer: doing it as a second step would give
		// the read loop a window in which dictated runes are on the line and the
		// bits do not say so.
		refused = e.val.insertRunesStamped([]rune(ins.text), voiceOriginated)
	} else {
		refused = e.val.InsertString(ins.text)
	}
	if refused > 0 {
		e.warn(fmt.Sprintf("the input line has no room for the last %d rune(s) of inserted text — the buffer bound is %d runes / %d bytes", refused, e.val.maxRune, e.val.maxByte))
	}
	// THE PASTE-BURST STATE IS DELIBERATELY NOT TOUCHED. A burst is a property of
	// the KEYSTROKES that arrived (Value.NoteKeys is told by the read loop's own
	// decode), and an insert is not a keystroke: marking one here would arm the
	// Enter suppression against the operator's next Enter, which is precisely the
	// "the transcript I appended cannot be submitted" defect this seam must not
	// introduce.
}

// waitLineInsert blocks until the read loop has APPLIED the queued inserts.
// It is the terminator for a foreign caller (and for a test) that reads the
// screen back after an insert, the same shape WaitAnswerDrawn and SettleBand
// have for their own seams. True means the level is clear.
func (e *Editor) waitLineInsert() bool {
	for i := 0; i < lineInsertWaitSteps; i++ {
		if !e.lineInsertPending() {
			return true
		}
		time.Sleep(lineInsertWaitStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's drain (see lineInsertWaitSteps)
	}
	return false
}

// SettleLineInserts waits for the queued inserts to be applied and reports
// whether the queue really emptied. False means the read loop did not answer
// within the bound — a torn-down editor, or a loop that has exited — and the
// caller must not read the screen back as if its text were there (Rule 15: the
// degradation is returned, never assumed away).
func (e *Editor) SettleLineInserts() bool {
	if e == nil {
		return true
	}
	if e.onLoopGoroutine() || e.loopGoid.Load() == 0 {
		return true
	}
	return e.waitLineInsert()
}

// KeyGate returns the owner installed at this editor's key seam, or nil when
// there is none. It is the READER beside SetKeyGate, and it exists so a second
// owner can CHAIN rather than replace: a layer that must consume one key while
// leaving every other key to whatever owner was there before asks for the
// current gate, keeps it, and forwards what it does not claim — which is the
// difference between a cooperative seat and a second surface that silently
// switches the first one off (measured on the replay controls: the "two surfaces
// at once" the operator photographed).
//
// It is read from the producer's goroutine, and that is safe by CONSTRUCTION
// rather than by a lock: production installs every gate from the REPL's own
// goroutine before the next line is read, and the read loop only ever READS the
// field (see dispatchKeyGate). Nothing here mutates a gate's own state.
func (e *Editor) KeyGate() KeyGate {
	if e == nil {
		return nil
	}
	return e.keyGate
}

// Forwarding is the shared half of a CHAINED gate: a helper an owner embeds so
// the unclaimed/released half of the seam's contract reaches the next owner in
// the chain. It is a named type rather than copied methods because the
// disjointness promise (keysource.go: every key is offered exactly one of
// {consumed, unclaimed, released}) is only kept if the chain FORWARDS rather
// than swallowing — a chained owner that implemented these as no-ops would make
// every key it declined invisible to the owner behind it.
type Forwarding struct {
	// Next is the gate behind this one, or nil when this owner is the last.
	Next KeyGate
}

// KeyUnclaimed forwards the editor's "I would ignore this key anyway" report.
func (f Forwarding) KeyUnclaimed(k Key) {
	if f.Next != nil {
		f.Next.KeyUnclaimed(k)
	}
}

// KeyRelease forwards the editor's "I am about to act on this key" report.
func (f Forwarding) KeyRelease(k Key) {
	if f.Next != nil {
		f.Next.KeyRelease(k)
	}
}
