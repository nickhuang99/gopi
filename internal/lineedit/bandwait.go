package lineedit

import (
	"runtime"
	"strconv"
	"strings"
	"time"
)

// bandwait.go — the framing terminator: WHO waits for a band row to be drawn.
//
// A band frame is written by the read loop (see drainBand), and the seam that
// queues it is reached from two kinds of caller:
//
//   - the REPL / the turn goroutine / the /help panel's writer — a FOREIGN
//     goroutine. Those callers read the screen back: cmd/agent's chat surface
//     ARMS itself by writing a notice and then asserting the notice is on the
//     grid, and cmd/agent's panel tests assert the whole panel's rows after the
//     call. Their historic contract is "on screen when this returns", and the
//     wait below is what keeps it.
//
//   - the READ LOOP'S OWN goroutine — the submit path (Editor.submit flushes the
//     line's tail through the same seam) and any nested producer reached from a
//     region write. The loop is the ONLY drainer, so a wait here would deadlock:
//     the queue could never be drained by the goroutine that is blocked waiting
//     for it. These callers do not read the screen during the call (the REPL is
//     inside ReadLine, blocked on the next line), so they return immediately and
//     the drain happens in that same read outcome.
//
// The two cases are told apart by GOROUTINE IDENTITY, never by a flag the caller
// sets: `loopGoid` is captured by Run (the one place the read loop is), and the
// comparison below is exact — a goroutine whose id differs is not the read loop,
// whatever it calls itself. A flag would be a claim; the id is a fact.
//
// The cost is two runtime calls per band write: the seam is a per-row call on a
// transcript path, not a per-keystroke one.

// bandWaitSteps and bandWaitStep bound a foreign caller's wait for the read loop
// to DRAW its row. The wait is a COUNT of steps, so the waiter needs no clock of
// its own, and it is a SYNCHRONIZATION bound rather than a measured duration: it
// exists so a layout that stopped drawing (a torn-down region, a loop that has
// exited) cannot park its caller forever. Reaching it means the read loop is
// gone, not slow — a frame is written in microseconds, and a host under load
// only makes the loop take longer to answer, never the row wrong (Rule 27: no
// load is generated for this, and no assertion rides a duration).
const (
	bandWaitSteps = 4000
	bandWaitStep  = 1 * time.Millisecond
	// bandWaitBudget is the bound itself, named once: what the degradation below
	// reports, and what a reader compares against a frame's own cost (one region
	// write, microseconds).
	bandWaitBudget = bandWaitSteps * bandWaitStep
)

// loopGoid is the goroutine id of the read loop while it is inside Run, and 0
// when no read loop is running. It is what tells a foreign band writer from the
// loop's own (see the file comment).
func (e *Editor) noteLoopGoroutine() { e.loopGoid.Store(goid()) }

// onLoopGoroutine reports whether the CALLER is the goroutine currently running
// the read loop.
func (e *Editor) onLoopGoroutine() bool {
	g := goid()
	return g != 0 && g == e.loopGoid.Load()
}

// CurrentGoroutineID reports the CALLING goroutine's id, or 0 when it cannot be
// read (see goid).
//
// It is exported for the ONE kind of caller that must state an IDENTITY FACT
// rather than set a flag about itself: cmd/agent's voice mode records which
// goroutine runs its capture loop, so a teardown can tell the loop's own call
// from a foreign one (chat_voice.go) — the same distinction, and the same fact,
// the band seam above decides with e.loopGoid. A flag a caller sets about itself
// is a claim; the id is a measurement (bandwait.go's file comment).
func CurrentGoroutineID() int64 { return goid() }

// waitBandWrite blocks until the queued band frames have been written by the
// read loop, and reports whether they reached the screen.
//
// false means the row was NOT drawn: the layout was taken down between the queue
// and the drain (drainBand lowers the level and drops the frames), or the read
// loop did not answer within bandWaitSteps. Both are REPORTED — the second on
// the editor's own error channel as well as by the return — so a caller that
// falls back to the historic flow bytes, or that asserts a screen, is never
// doing so on the strength of a silent failure (Rule 15).
//
// THE LOOP THAT OWED THIS ROW IS THE ONE THAT MUST DRAIN IT, and its identity is
// read ONCE, on entry: `loopGoid` is the PER-RUN fact Run clears on its return
// (editor.go), and a queue handed over while a loop ran belongs to that loop.
// The wait therefore ends the moment that loop is GONE — a caller whose loop
// exited between the queue and the wait has a frame waiting for a drainer that
// will not come until the next Run, and spending 4000 steps to then report "the
// read loop is stalled" would name the wrong defect and park the caller for
// four seconds (measured: `-cpu=1 -count=40` on cmd/agent's voice PTY cases, two
// goroutines parked here — a teardown closed the terminal under the capture loop
// that was still inside its own notice, the loop exited, and the notice was left
// waiting for it). A loop that is merely busy keeps the same id and keeps the
// full bound, which is what the bound is for.
func (e *Editor) waitBandWrite() bool {
	owner := e.loopGoid.Load()
	for i := 0; i < bandWaitSteps; i++ {
		if owner == 0 && !e.answerPending() && !e.answer.Noting() {
			return e.BandActive()
		}
		if owner == 0 {
			owner = e.loopGoid.Load()
			if owner == 0 {
				time.Sleep(bandWaitStep) //stylegate:allow-sleep bounded wait for the read loop to publish its identity
				continue
			}
		}
		if !e.bandPending() {
			// The drain lowered the level: the frames are written (or were
			// dropped with the layout). BandActive() distinguishes the two.
			return e.BandActive()
		}
		if e.loopGoid.Load() != owner {
			// THE DRAINER IS GONE (see the doc comment): the frame stays the LEVEL
			// the next read outcome re-derives — late, never lost — which is the
			// same answer the seam gives for the no-loop state, re-read at the
			// moment that decides it. NOT a failure, so it is not reported as one.
			return e.BandActive()
		}
		time.Sleep(bandWaitStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's drain (see bandWaitSteps)
	}
	// degradation writes to BOTH destinations and no-ops when neither is
	// installed, so no guard is needed here (and a guard naming only errOut is
	// how the record went missing in the first place).
	e.degradation("[gopi] editor: a band write was not drawn within " + bandWaitBudget.String() + " (the read loop is stalled?) — the caller announced a screen it does not own\n")
	return false
}

// awaitForeignNote is the NOTE seam's terminator, and it exists for the ORDER
// between the two band queues rather than for the row itself.
//
// drainBandFrames drains the band FRAMES first and the caller rows (notes)
// after, and that fixed order is right for a drain — it is not an ordering the
// producers can rely on: a note queued at T is committed after a frame queued at
// T+Δ, because the two queues are separate and the drain order is not the enqueue
// order. Measured on a real 100×30 PTY (2026-10-22): `/info` queued its block as
// notes, the REPL's own next-pass `WriteLines("\n")` queued a frame a moment
// later, and the blank row was committed ABOVE the block — the block started one
// grid row lower than master's (12 instead of 11) with every following row
// shifted, while the text was identical. The same block through WriteLines (one
// queue) came out at row 11.
//
// The fix is the contract the frame seam already keeps: a FOREIGN caller's rows
// are on the screen when the call returns (bandwait.go's file comment). A caller
// that then queues another row — the REPL's inter-turn blank, a panel — is
// queuing it AFTER rows that are already drawn, so the band's order is the
// caller's order. The read loop's own calls return immediately (it is the only
// drainer: waiting would deadlock), and a run with no live loop returns too (the
// queue is then a level the next Run re-derives).
func (e *Editor) awaitForeignNote() {
	if e.answer == nil || e.onLoopGoroutine() {
		return
	}
	// A ZERO IDENTITY IS "NOT PUBLISHED YET", NOT "NO LOOP": the loop stores its
	// id at Run's start and clears it on the way out, and this caller can arrive
	// in between — measured in this package's own harness, where the FIRST
	// foreign call after Run was launched saw loopGoid=0, skipped the wait, and
	// put the blank row back above the block. So a zero identity waits too,
	// bounded, and gives up the same way a stalled loop does. It never waits when
	// no level shows anything pending (waitNoteWrite's first check), so a region
	// with no loop at all still costs nothing.
	e.waitNoteWrite()
}

// waitNoteWrite is waitBandWrite's sibling on the note queue: the same bound, the
// same goroutine-identity exit, the same reporting — because the question is the
// same one ("are this caller's rows drawn?") and only the LEVEL differs
// (AnswerRenderer.Noting for notes, bandPending for frames).
func (e *Editor) waitNoteWrite() bool {
	owner := e.loopGoid.Load()
	for i := 0; i < bandWaitSteps; i++ {
		if owner == 0 && !e.answerPending() && !e.answer.Noting() {
			return e.BandActive()
		}
		if owner == 0 {
			owner = e.loopGoid.Load()
			if owner == 0 {
				time.Sleep(bandWaitStep) //stylegate:allow-sleep bounded wait for the read loop to publish its identity
				continue
			}
		}
		// BOTH LEVELS: a queued note is first an ANSWER OP (routed, applied by
		// drainAnswerOps) and only then a caller row in the note queue that
		// drainNotes commits, so "my rows are drawn" is neither level alone —
		// waiting on Noting() returns while the op is still queued, which is
		// exactly the race that put the blank row back above the block.
		if !e.answerPending() && !e.answer.Noting() {
			return e.BandActive()
		}
		if e.loopGoid.Load() != owner {
			return e.BandActive()
		}
		time.Sleep(bandWaitStep) //stylegate:allow-sleep bounded synchronization wait on the read loop's drain (see bandWaitSteps)
	}
	e.degradation("[gopi] editor: a band note was not drawn within " + bandWaitBudget.String() + " (the read loop is stalled?) — the caller's next row would be committed above it\n")
	return false
}

// goid returns the calling goroutine's id, or 0 when it cannot be read. The
// runtime exposes no accessor, so it is parsed off the panic header of the
// stack it was called from — a supported-by-observation, read-only use: the
// value is compared for EQUALITY only (never ordered, never stored across
// sessions, never printed in production).
func goid() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// The header is "goroutine 123 [running]:\n".
	s := string(buf[:n])
	const prefix = "goroutine "
	if !strings.HasPrefix(s, prefix) {
		return 0
	}
	s = s[len(prefix):]
	end := strings.IndexByte(s, ' ')
	if end < 0 {
		return 0
	}
	id, err := strconv.ParseInt(s[:end], 10, 64)
	if err != nil {
		return 0
	}
	return id
}
