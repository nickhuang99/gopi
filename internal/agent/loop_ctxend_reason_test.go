package agent

// loop_ctxend_reason_test.go — the regression guard for the MISATTRIBUTED exit
// (defect found 2026-10-16, 81 merge hand-over failures sharing one sentence).
//
// WHAT WAS WRONG: the round-boundary ctx.Done() fallback in runEngine answered
// every context end with `agent timed out after %d iterations`. In production
// a.MaxIterations is 0 (unlimited — loop.go's NewAgent sets it, and no
// non-test code assigns anything else), so there is no iteration budget to
// time out against, and the %d was the loop counter — which is why the same
// defect reported 43, 48, 49, 52 … . A reader of the row (or of the crash
// dump) was pointed at a budget that does not exist instead of at the caller
// that cancelled the run.
//
// WHAT IS ASSERTED HERE, and why it is the MINIMAL reproduction: the loop is
// driven with MaxIterations == 0 and a real mock LLM, and the run's context is
// cancelled from inside the first tool round (the shape a parent run / a
// supervising surface has). Then:
//  1. the returned error names the CONTEXT, never an iteration budget — this
//     is the half that fails on master (the string "iterations" is present
//     there regardless of a.MaxIterations);
//  2. the run's end reason is the distinguishable runend value and it reaches
//     the ledger row's reason column / task_events trail — so the row is
//     `failed` with a WHY, not a false `completed` and not a mystery.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/runend"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ctxEndToolCalls is the number of tool rounds the mock is asked for before
// the test's cancel is supposed to have ended the run: the loop must stop at
// the round BOUNDARY, so more than one round means the cancel was ignored.
const ctxEndToolCalls = 2

// startCtxEndServer serves varying tool_use rounds (varying, so the toolStreak
// net cannot fire and end the run for the wrong reason) for a run whose
// cancellation is driven by the test itself.
func startCtxEndServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		stuckToolUseStreamArgs(t, w, "task", fmt.Sprintf(`{"action":"view","query":"tick %d"}`, calls))
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// TestAgent_MaxIterationsZero_CancelledRunClaimsNoIterationBudget is the
// minimal repro: MaxIterations = 0 (production), ctx cancelled by the caller →
// the error must not mention iterations at all.
func TestAgent_MaxIterationsZero_CancelledRunClaimsNoIterationBudget(t *testing.T) {
	server, calls := startCtxEndServer(t)

	a := newTruncationTestAgent(t, server.URL, 0)
	if a.MaxIterations != 0 {
		t.Fatalf("this test is only meaningful with MaxIterations=0 (production), got %d", a.MaxIterations)
	}
	// The caller's stop, delivered exactly where a parent run / a supervising
	// surface delivers it: the run-scoped cancel, from outside the loop.
	a.executeToolFn = func(tc deepseek.ToolCall) string {
		a.InterruptRun()
		return "mock result for " + tc.Name
	}

	_, err := a.Run("long-running task the caller will cancel")
	if err == nil {
		t.Fatal("a cancelled run must report an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "iterations") {
		t.Fatalf("MaxIterations=0 means there is NO iteration budget — the message must not name one:\n%s", msg)
	}
	if !strings.Contains(msg, context.Canceled.Error()) {
		t.Fatalf("the message must name the real cause (%q):\n%s", context.Canceled.Error(), msg)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the error must stay in the context chain (errors.Is) so a carrier can classify it:\n%s", msg)
	}
	if *calls > ctxEndToolCalls {
		t.Fatalf("the run continued past its cancellation: %d tool rounds served", *calls)
	}

	// The row: the reason column and the trail carry the DISTINGUISHABLE end
	// reason, and the row is not a completion.
	if got := runEndReasonRecorded(a.autoTaskRowID); got != runend.RunContextCancelled {
		t.Fatalf("row %s records end reason %q, want %q", a.autoTaskRowID, got, runend.RunContextCancelled)
	}
	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(row, runend.RunContextCancelled.Token()) {
		t.Fatalf("the row must record WHY it stopped (%q):\n%s", runend.RunContextCancelled.Token(), row)
	}
	if !strings.Contains(row, runend.TruncationMarker) {
		t.Fatalf("an externally cancelled run is engine-forced, not a completion:\n%s", row)
	}
	assertTerminalRowWriteWitness(t, a, a.autoTaskRowID, tools.TaskStatusFailed,
		runend.RunContextCancelled.Token())
}

// TestAgent_RunCtxEnded_DistinguishesDeadlineFromCancel pins the second half of
// the vocabulary: an EXPIRED deadline is its own reason (a budget fact) and the
// message says so — the two cancels demand different responses, so merging them
// would leave a reader where the single misleading sentence left them.
//
// The deadline is armed at a nanosecond so it expires at the FIRST round
// boundary — no waiting, no load (Rule 27), and the exit site is the same one.
func TestAgent_RunCtxEnded_DistinguishesDeadlineFromCancel(t *testing.T) {
	server, calls := startCtxEndServer(t)

	a := newTruncationTestAgent(t, server.URL, 0)
	a.Timeout = ctxEndDeadline // the run's own deadline (GOPI_TIMEOUT_SEC's field)

	_, err := a.Run("long-running task against an expired deadline")
	if err == nil {
		t.Fatal("an expired run deadline must report an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "iterations") {
		t.Fatalf("an expired deadline is not an iteration budget:\n%s", msg)
	}
	if !strings.Contains(msg, context.DeadlineExceeded.Error()) {
		t.Fatalf("the message must name the deadline (%q):\n%s", context.DeadlineExceeded.Error(), msg)
	}
	if *calls != 0 {
		t.Fatalf("the deadline had already expired, yet %d LLM rounds were issued", *calls)
	}
	if got := runEndReasonRecorded(a.autoTaskRowID); got != runend.DeadlineExceeded {
		t.Fatalf("row %s records end reason %q, want %q — an expired limit and a caller's cancel must stay distinguishable",
			a.autoTaskRowID, got, runend.DeadlineExceeded)
	}
}

// ctxEndDeadline is the run deadline the deadline arm arms: one nanosecond, so
// the run context is already expired when the loop's first boundary check runs.
// Named rather than inline (Rule 1); it is deliberately not a knob, because the
// value carries no meaning beyond "expired before the first round".
const ctxEndDeadline = time.Nanosecond
