package agent

// run_interrupt_test.go — the RUN-SCOPED interrupt: the difference between
// abandoning ONE turn and ending the process.
//
// WHY THIS FILE EXISTS (the measured defect it guards): Interrupt() cancels the
// process-wide interrupt root, which NewAgent builds ONCE and nothing re-arms.
// That is correct for a signal handler on a process that is about to end, and
// wrong for a long-lived server whose operator asks to stop a single turn: after
// one such call every later turn's context is born cancelled, and the surface
// would report an instant failure for the rest of its life. A `gopi ui` operator
// pressing Ctrl+X hit exactly this family of problem — the surface said
// "interrupted" while the engine kept the turn running and holding the lock the
// next message needed.
//
// So the properties asserted here are:
//  1. InterruptRun cancels THIS run, and reports truthfully whether there was
//     one to cancel (a false "interrupted" is the lie the UI must not tell).
//  2. It leaves the process-wide root — and therefore the NEXT run — intact.
//  3. The signal path (Interrupt) still reaches a run derived from the root.

import (
	"context"
	"testing"
	"time"
)

// waitClosed reports whether ctx is done within a short bound, so a broken
// cancel FAILS the test instead of hanging it.
func waitClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

// agentWithInterruptRoot is an agent carrying a REAL interrupt root, the shape
// NewAgent builds in production. The hand-built `&Agent{}` literal used elsewhere
// has no root (every run falls back to context.Background()), so it could not
// exercise the difference this file is about: whether a cancel reaches ONLY this
// run or the whole process.
func agentWithInterruptRoot() *Agent {
	base, cancel := context.WithCancel(context.Background())
	return &Agent{interruptBase: base, interruptCancel: cancel}
}

// TestInterruptRunCancelsOnlyTheRun is the core property: the current run dies,
// the next one is born alive.
func TestInterruptRunCancelsOnlyTheRun(t *testing.T) {
	a := agentWithInterruptRoot()
	run := a.runScopedRoot()
	if run.Err() != nil {
		t.Fatalf("a fresh run context is already cancelled: %v", run.Err())
	}
	if !a.InterruptRun() {
		t.Fatal("InterruptRun reported no turn to abandon while one was in flight")
	}
	if !waitClosed(run.Done()) {
		t.Fatal("InterruptRun did not cancel the run context")
	}
	// Idempotence and honesty: a second call has nothing to abandon and says so.
	if a.InterruptRun() {
		t.Error("InterruptRun claimed a second interruption after the run had already ended")
	}
	// THE POINT: the process-wide root was not touched, so the next turn of a
	// long-lived server still starts.
	if err := a.interruptRoot().Err(); err != nil {
		t.Fatalf("the process-wide interrupt root was cancelled: %v", err)
	}
	next := a.runScopedRoot()
	if next.Err() != nil {
		t.Fatalf("the run AFTER an interrupt is born cancelled (%v) — one Ctrl+X would poison every later turn", next.Err())
	}
	// And the next run is itself interruptible (the handle was republished).
	if !a.InterruptRun() {
		t.Fatal("the run after an interrupt could not be abandoned")
	}
	if !waitClosed(next.Done()) {
		t.Fatal("the second run was not cancelled")
	}
}

// TestInterruptRunWithoutARunIsFalse: no turn in flight is reported as no turn
// in flight — the surface must not print "interrupted" for a no-op.
func TestInterruptRunWithoutARunIsFalse(t *testing.T) {
	a := agentWithInterruptRoot()
	if a.InterruptRun() {
		t.Error("InterruptRun claimed an interruption with no run in flight")
	}
	var nilAgent *Agent
	if nilAgent.InterruptRun() {
		t.Error("a nil agent claimed an interruption")
	}
}

// TestInterruptStillReachesARunDerivedFromTheRoot: the signal path (what a
// SIGTERM handler calls) must keep working through the new layer — the run
// context derives from the root, so cancelling the root cancels the run.
func TestInterruptStillReachesARunDerivedFromTheRoot(t *testing.T) {
	a := agentWithInterruptRoot()
	run := a.runScopedRoot()
	a.Interrupt()
	if !waitClosed(run.Done()) {
		t.Fatal("Interrupt() no longer reaches a run derived from the interrupt root")
	}
	// The run-scoped handle is republished by the NEXT run, but that run derives
	// from the (now cancelled) root — the documented one-shot signal semantics.
	// Asserting it here keeps the behaviour visible rather than surprising: it is
	// exactly why an interactive "stop this turn" cannot go through Interrupt().
	if next := a.runScopedRoot(); !waitClosed(next.Done()) {
		t.Fatal("a run started after Interrupt() is not cancelled by the root — the signal contract changed")
	}
}

// TestTurnInterruptedIsScopedToTheRunThatWasAbandoned pins the LATCH's lifetime,
// which is the difference between a session that survives an interrupt and a
// session that lies about every turn after it.
//
// The defect it guards (found while building task 01a0e4390's per-turn Ctrl+C):
// one flag served both meanings — Interrupt()'s "leave, now" and InterruptRun()'s
// "abandon this turn" — and nothing cleared it. An interactive session that
// interrupted ONE turn then carried the flag into every later turn, so the next
// turn's own failure would be printed and RECORDED as an operator interrupt that
// never happened.
func TestTurnInterruptedIsScopedToTheRunThatWasAbandoned(t *testing.T) {
	a := agentWithInterruptRoot()
	run := a.runScopedRoot()
	if a.TurnInterrupted(unfinishedCtx(t)) {
		t.Fatal("a fresh run reports itself interrupted before any cancel")
	}
	if !a.InterruptRun() {
		t.Fatal("InterruptRun reported no turn to abandon while one was in flight")
	}
	if !waitClosed(run.Done()) {
		t.Fatal("InterruptRun did not cancel the run context")
	}
	// The run is dead, so its own ctx.Err() is set and the latch applies.
	if !a.TurnInterrupted(run) {
		t.Fatal("the abandoned run does not report itself interrupted — the carrier would print a turn failure instead of the interrupt marker")
	}
	// The NEXT run is a different run: it must be born un-interrupted. This is
	// the assertion the defect above fails.
	next := a.runScopedRoot()
	if a.TurnInterrupted(next) {
		t.Fatal("the run AFTER an interrupted one reports itself interrupted — one Ctrl+C would relabel every later turn")
	}
	// And it is itself interruptible, with the latch applying to IT.
	if !a.InterruptRun() {
		t.Fatal("the run after an interrupt could not be abandoned")
	}
	if !waitClosed(next.Done()) || !a.TurnInterrupted(next) {
		t.Fatal("the second run was not cancelled, or does not report itself interrupted")
	}
}

// TestInterruptIsNotTurnInterrupt: the SIGNAL path's meaning is untouched. A run
// cancelled by Interrupt() is a session ending, and TurnInterrupted must not
// claim it — the carrier's two obligations (close the session / keep reading the
// next prompt) are opposite, so confusing them is the whole risk of the feature.
func TestInterruptIsNotTurnInterrupt(t *testing.T) {
	a := agentWithInterruptRoot()
	run := a.runScopedRoot()
	a.Interrupt()
	if !waitClosed(run.Done()) {
		t.Fatal("Interrupt() did not cancel the run")
	}
	if a.TurnInterrupted(run) {
		t.Fatal("a signal-interrupted run was reported as an operator per-turn interrupt — the carrier would keep the session open after a SIGINT")
	}
}

// TestTurnInterruptedRequiresAnEndedContext: the latch is set BEFORE the cancel,
// so a run that is still alive must not be relabelled by a latch that has not
// been acted on yet.
func TestTurnInterruptedRequiresAnEndedContext(t *testing.T) {
	a := agentWithInterruptRoot()
	_ = a.runScopedRoot()
	a.runMu.Lock()
	a.runInterrupted = true // simulate "the claim happened, the cancel has not been observed yet"
	a.runMu.Unlock()
	if a.TurnInterrupted(unfinishedCtx(t)) {
		t.Fatal("an unfinished context was reported interrupted — a live turn would be relabelled by a not-yet-acted claim")
	}
}

// unfinishedCtx returns a live context with the same shape a run's ctx has, for
// the "the cancel has not landed" case above.
func unfinishedCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
