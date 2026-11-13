package lineedit

import (
	"sync"
	"time"
)

// answerqueue.go — WHO MUTATES THE ANSWER RENDERER, and why it is one goroutine.
//
// The renderer's state (the in-flight line, the live row and column, the marker
// and reasoning ledger, the queued caller rows, the composer's fence) is a
// PROGRAM COUNTER: every field is written on the way from one delta to the next,
// and none of it is meaningful if two goroutines interleave their passes. It was
// therefore mutated from two — the turn's goroutine (Delta/Finish/BeginTurn/
// BeginThinking/Note) and the read loop (Tick/drainNotes/repaint) — and the
// measured result was 11 `WARNING: DATA RACE` blocks in ONE run of cmd/agent's
// `go test -race -run TestChatSurfaceKey_`, every one of them a field of this
// renderer (drawStatus↔reanchorLiveRow, Note↔Noting, drainNotes↔writeRow,
// commitPrefix↔writeRow, LineComposer.inFence↔EndStream, writeRow↔writeRow).
//
// THE ROUTING, and it is the same one the band seam already made (queueBand):
// a turn-side call QUEUES AN OP and wakes the read loop; the loop APPLIES the
// ops, in order, on its own goroutine, through the renderer's own methods. Every
// field of the renderer is then touched by exactly one goroutine at a time — the
// loop while it is running, the caller while it is not (every unit test, the pty
// cases, and a caller that reaches a renderer no editor owns). There is NO LOCK
// over renderer state: the mutex below protects the QUEUE and the level, which
// are data two goroutines legitimately share, and nothing else. A lock over the
// fields would be a lock that hides the defect (Rule 15) — it would leave the
// interleaving possible and merely undetectable.
//
// WHY THE OP AND NOT A PARTIAL WRITE. An op carries the CALL, not a piece of
// it: `deltaNow` runs the whole delta (the buffer, the cadence, the commit), so
// the loop applies exactly the sequence the caller asked for, in the caller's
// order, on one goroutine. Splitting a call's internals between the goroutines
// would put the fields back in two hands.
type answerKind uint8

// The ops. Each one is a public method of AnswerRenderer that mutates renderer
// state when a read loop owns the renderer (see routed).
const (
	answerOpBegin answerKind = iota
	answerOpDelta
	answerOpFinish
	answerOpBeginTurn
	answerOpEndTurn
	answerOpBeginThinking
	answerOpEndThinking
	answerOpBoundary
	answerOpNote
)

// answerOp is ONE turn-side call, replayed by the read loop.
type answerOp struct {
	kind answerKind
	// text is the call's text: a delta's fragment, a note's row, a turn
	// header's prompt.
	text string
	// st is a note's style.
	st Style
	// runs is a caller-composed note's per-run emphasis (NoteRuns). When it is
	// set, text is ignored and the row is rebuilt from the runs — they carry
	// their own text, so a queued op stays self-contained.
	runs []NoteRun
	// copied says the note's rows are a PASTE SOURCE: they are committed with no
	// continuation marker (noteRow.copied). It rides the op because the read loop
	// is the applier — a property of the row's purpose that did not travel would
	// be silently dropped on the queued path, which is the path production takes.
	copied bool
	// info is a turn header's identity (index and prompt).
	info TurnInfo
}

// answerQueue is the pending turn-side calls plus the level that says they are
// waiting. The mutex guards the queue and the level ONLY: every field of the
// renderer is applied on the read loop (or, with no loop running, on the
// caller's own goroutine — see routed), which is what makes this a queue rather
// than a cure for the race it removes.
type answerQueue struct {
	// editor is the owner this queue belongs to: the goroutine identity of its
	// read loop, and the wake seam a publish raises, both live there.
	editor *Editor
	mu     sync.Mutex
	// ops are the calls waiting to be applied, in the producer's order.
	ops []answerOp
	// active is the LEVEL: at least one op is queued, or the read loop is inside
	// the drain. Level-triggered and sticky until the drain empties it, for the
	// reason every other publish in this package is: the loop can be parked in
	// an unbounded read when an op lands, so what carries the draw past the park
	// is a flag the loop RE-DERIVES, never a one-shot notification.
	active bool
	// flushing marks the read loop as being inside the drain, so a nested
	// producer (a region flush callback) does not wait for a later drain of a
	// queue only it can empty.
	flushing bool
	// notifyMisses counts the ops queued while no wake seam was installed. It is
	// the fallback's own bookkeeping: with no channel to interrupt the loop's
	// blocked read, the op is still a level the loop re-derives on its next own
	// wake (late, never lost), and the count makes the degraded cadence
	// OBSERVABLE instead of leaving the caller to infer it (Rule 15).
	notifyMisses int
}

// queueAnswerOp queues one turn-side call and raises the level. It is called
// from the TURN's goroutine and touches nothing but the queue: no terminal byte,
// no renderer field, no region.
//
// It reports whether a WAKE CHANNEL delivered the notification; false means the
// op waits for the loop's own next wake (bounded, never lost) and is COUNTED
// (see AnswerNotifyMisses).
func (e *Editor) queueAnswerOp(op answerOp) bool {
	e.answers.mu.Lock()
	e.answers.ops = append(e.answers.ops, op)
	e.answers.active = true
	notify := e.notify != nil && e.notify.NotifyEnabled()
	if !notify {
		e.answers.notifyMisses++
	}
	e.answers.mu.Unlock()
	if notify {
		e.notify.Notify()
	}
	return notify
}

// answerPending reports whether a turn-side call is waiting to be applied, or is
// being applied right now. It is a PEEK: the drain lowers the level, so asking in
// two places cannot drop an op.
//
// It is FALSE with no read loop running: those calls are not queued at all (the
// caller performs them — see AnswerRenderer.routed), and a level left raised by
// an op queued before the loop exited must not ask a repaint of a loop that is
// not there.
func (e *Editor) answerPending() bool {
	e.answers.mu.Lock()
	defer e.answers.mu.Unlock()
	return e.answers.active
}

// drainAnswerOps applies every queued turn-side call, IN ORDER, on the caller's
// goroutine — which is the read loop. It reports whether it applied one.
//
// The drain is re-entrant by construction, exactly as drainBand is: the queue is
// taken under the lock and the call is applied OUTSIDE it (an op can reach a
// region write, and a flush callback that publishes again must not deadlock), so
// an op queued during the drain is applied by the loop's next pass.
func (e *Editor) drainAnswerOps() bool {
	if e.answer == nil {
		// No renderer (no reserved layout, or one built without an editor): the
		// ops cannot be applied. They are dropped with the level lowered — the
		// same explicit "nothing owns this" the band drain reports, never an
		// op that sits in a queue forever (Rule 15).
		e.answers.mu.Lock()
		e.answers.ops = nil
		e.answers.active = false
		e.answers.mu.Unlock()
		return false
	}
	e.answers.mu.Lock()
	if e.answers.flushing {
		e.answers.mu.Unlock()
		return false
	}
	e.answers.flushing = true
	e.answers.mu.Unlock()
	applied := false
	for {
		e.answers.mu.Lock()
		if len(e.answers.ops) == 0 {
			// Empty AND no application in flight: lower the level HERE, under the
			// same lock that sees the queue empty, so a producer that queues after
			// this line raises it again and the level can never be lowered over a
			// call that is still waiting.
			e.answers.active = false
			e.answers.mu.Unlock()
			break
		}
		op := e.answers.ops[0]
		e.answers.ops = e.answers.ops[1:]
		e.answers.mu.Unlock()
		e.applyAnswerOp(op)
		applied = true
	}
	e.answers.mu.Lock()
	e.answers.flushing = false
	e.answers.active = len(e.answers.ops) > 0
	e.answers.mu.Unlock()
	return applied
}

// applyAnswerOp performs ONE queued call through the renderer's own method. It
// runs on the read loop, which is the whole point: the method's body — its reads
// of the region's live row, its writes of the live text, its commit and scroll —
// is the code that used to run on the turn's goroutine.
func (e *Editor) applyAnswerOp(op answerOp) {
	r := e.answer
	if r == nil {
		return
	}
	switch op.kind {
	case answerOpBegin:
		r.beginNow()
	case answerOpDelta:
		r.deltaNow(op.text)
	case answerOpFinish:
		r.finishNow()
	case answerOpBeginTurn:
		r.beginTurnNow(op.info)
	case answerOpEndTurn:
		r.endTurnNow()
	case answerOpBeginThinking:
		r.beginThinkingNow()
	case answerOpEndThinking:
		r.endThinkingNow()
	case answerOpBoundary:
		r.answerBoundaryNow()
	case answerOpNote:
		if len(op.runs) > 0 {
			r.noteNowRunsCopied(op.runs, op.copied)
			return
		}
		r.noteNow(op.text, op.st)
	}
}

// answerWaitSteps and answerWaitStep bound a caller's wait for the read loop to
// APPLY the calls it published. The wait is a COUNT of steps, so the waiter needs
// no clock of its own, and it is a SYNCHRONIZATION bound rather than a measured
// duration: reaching it means the read loop is gone (a torn-down region, an
// exited loop), never that it is slow. An op is applied in microseconds, and a
// host under load only makes the loop take longer to answer (Rule 27: no load is
// generated for this, and no assertion rides a duration).
const (
	answerWaitSteps = 4000
	answerWaitStep  = 1 * time.Millisecond
	// answerWaitBudget is the bound itself, named once.
	answerWaitBudget = answerWaitSteps * answerWaitStep
)

// WaitAnswerDrawn blocks until the queued turn-side calls have been applied by
// the read loop, and reports whether the queue really emptied.
//
// It is the terminator for a FOREIGN caller that reads the screen back — the
// shape cmd/agent's turn boundary has (Finish → EndTurn → the flow seam → the
// screen dump's Settle), and the same shape the band seam's callers have (see
// waitBandWrite). Without it the write-back the caller asserts on is painted by
// the loop at some later moment, and the caller reports a screen state that was
// never drawn.
//
// The two exclusions are the ones the band terminator states, for the same
// reasons: the LOOP's own call must never wait (it is the only applier, so
// waiting is a deadlock), and a call with NO LOOP RUNNING is performed by the
// caller itself (see routed), so there is nothing to wait for.
func (e *Editor) WaitAnswerDrawn() bool {
	if e == nil || e.answer == nil {
		return true
	}
	if e.onLoopGoroutine() || e.loopGoid.Load() == 0 {
		return true
	}
	for i := 0; i < answerWaitSteps; i++ {
		if !e.answerPending() {
			return true
		}
		time.Sleep(answerWaitStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's drain (see answerWaitSteps)
	}
	e.reportAnswerWaitFailure()
	return false
}

// AnswerNotifyMisses reports how many turn-side calls were queued while no wake
// seam was installed, i.e. how many reached the screen only on the read loop's
// own next wake. Zero means every published call had a real channel to interrupt
// the loop's blocked read with; production installs one (the terminal's wake
// pipe), so a non-zero value means the fallback cadence is in force and a caller
// may announce it (Rule 15 — a degradation is reported, never inferred).
func (e *Editor) AnswerNotifyMisses() int {
	e.answers.mu.Lock()
	defer e.answers.mu.Unlock()
	return e.answers.notifyMisses
}
