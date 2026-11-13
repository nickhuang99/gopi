package agent

// chat_surface_renewal_test.go — the RENEWABLE self-driven bound, driven through
// the REAL surfaces (the tool dispatcher and the carrier seam), not through the
// counter.
//
// WHAT IS BEING PINNED, and why each half is a separate claim:
//
//  1. THE PERIOD ENDS AT A USER LINE. After a full period is spent (the tool
//     refuses), a line the OPERATOR submitted renews the budget and the very
//     next `type` is accepted again. Without this, one session can never run a
//     long autonomous verification — it dies after chatSurfaceLoopBound turns.
//  2. THE MODEL CANNOT RENEW IT. The renewal is not reachable from any tool
//     action: a full period stays refused however many times the tool is called.
//     This is the operator's ruling (task 01a0e44a) and the reason the reset
//     lives in one function the carrier calls — a "renew myself" action would
//     make the bound decorative.
//  3. THE BOUND STILL TERMINATES. A period nobody re-enters ends at the bound,
//     once, with Closing on the last turn: renewal must not turn the bound into
//     a loop that never stops.
//  4. A `stop` IS NOT LIFTED BY A USER LINE. Renewal re-opens a BUDGET; it does
//     not restart a loop the operator deliberately ended (the tool's own
//     refusal on a stopped loop says "it does not restart itself").
//  5. A MID-PERIOD OPERATOR LINE IS DEFERRED, NOT APPLIED. When a self-driven
//     line is still in hand, the operator's line is not the boundary that owns
//     the reset — and the refusal is a NAMED outcome so the carrier can say so
//     instead of leaving a renewal to be inferred (an inferred reset is
//     self-renewal wearing the operator's clothes).

import (
	"strings"
	"testing"
)

// chatSurfaceRenewalAgent builds the smallest Agent the renewal needs: a
// surface installed (the renewing seam is the tool's own state) and nothing
// else.
func chatSurfaceRenewalAgent() *Agent {
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{})
	return a
}

// spendPeriod drives one FULL period through the real dispatcher + seam, and
// fails if the period did not reach its bound.
func spendPeriod(t *testing.T, a *Agent, tag string) {
	t.Helper()
	for i := 1; i <= chatSurfaceLoopBound; i++ {
		got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "turn"})
		if !strings.Contains(got, "typed=true") {
			t.Fatalf("%s: type #%d returned %q, want an acceptance", tag, i, got)
		}
		if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamTyped {
			t.Fatalf("%s: consumption #%d = %+v, want a typed turn", tag, i, turn)
		}
	}
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "past the bound"})
	if !strings.Contains(got, "refused") {
		t.Fatalf("%s: type past the bound returned %q, want a refusal", tag, got)
	}
	if used := a.ChatSurfaceLoopUsed(); used != chatSurfaceLoopBound {
		t.Fatalf("%s: used=%d after a full period, want %d", tag, used, chatSurfaceLoopBound)
	}
}

// TestChatSurfaceRenewal_UserLineOpensAFreshPeriod is claim 1: the operator's own
// line is what makes a long run possible, and the next `type` is accepted.
func TestChatSurfaceRenewal_UserLineOpensAFreshPeriod(t *testing.T) {
	a := chatSurfaceRenewalAgent()
	spendPeriod(t, a, "period 1")

	// Before the renewal the tool refuses, so the reset is the ONLY thing that
	// can change the answer below — this is not a counter that was never spent.
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "x"}); !strings.Contains(got, "refused") {
		t.Fatalf("the spent period did not refuse: %q", got)
	}

	renew := a.ChatSurfaceUserTurn()
	if !renew.Renewed {
		t.Fatalf("a user line at a clean boundary reported %+v, want Renewed", renew)
	}
	if renew.Bound != chatSurfaceLoopBound {
		t.Fatalf("the renewal reported bound %d, want %d", renew.Bound, chatSurfaceLoopBound)
	}
	if used := a.ChatSurfaceLoopUsed(); used != 0 {
		t.Fatalf("after the renewal used=%d, want 0 (a fresh period)", used)
	}

	// THE POINT OF THE WHOLE CHANGE: a second period runs in the SAME session.
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "period 2"})
	if !strings.Contains(got, "typed=true") {
		t.Fatalf("type after the renewal returned %q, want an acceptance — this is the long-run capability", got)
	}
	turn := a.ChatSurfaceTakePending()
	if turn.Kind != ChatSurfaceSeamTyped || turn.Line != "period 2" {
		t.Fatalf("the first turn of period 2 = %+v, want the typed line", turn)
	}
	// And the counter is reporting the NEW period, not a session total: 1 of 8.
	if turn.Used != 1 || turn.Bound != chatSurfaceLoopBound {
		t.Fatalf("period 2's first turn reports %d/%d, want 1/%d", turn.Used, turn.Bound, chatSurfaceLoopBound)
	}
	if turn.Closing {
		t.Fatalf("the first turn of a fresh period reported Closing=true")
	}
}

// TestChatSurfaceRenewal_TheModelCannotRenewIt is claim 2 — the operator's
// ruling, driven at the tool's own surface: no action the model can reach moves
// the counter back, so a full period stays full however hard the model tries.
func TestChatSurfaceRenewal_TheModelCannotRenewIt(t *testing.T) {
	a := chatSurfaceRenewalAgent()
	spendPeriod(t, a, "period")

	// EVERY action the wire advertises. A renewal reachable from any one of them
	// would be a bound the bounded party controls.
	for _, action := range ChatSurfaceActions() {
		for i := 0; i < chatSurfaceLoopBound+1; i++ {
			a.runChatSurfaceTool(map[string]interface{}{"action": action, "text": "turn"})
			if used := a.ChatSurfaceLoopUsed(); used != chatSurfaceLoopBound {
				t.Fatalf("action=%s moved the counter to %d (from %d): the model can renew its own budget",
					action, used, chatSurfaceLoopBound)
			}
		}
	}

	// `stop` also must not reset it forward: a stop ends the loop, it does not
	// hand the model a new budget.
	a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionStop})
	if used := a.ChatSurfaceLoopUsed(); used != chatSurfaceLoopBound {
		t.Fatalf("stop moved the counter to %d, want %d", used, chatSurfaceLoopBound)
	}

	// The narrowly-scoped claim, stated as the tool's contract: the refusal the
	// model gets names the OPERATOR as the renewer, so a model that reads it
	// knows the one action that opens a period is not its own.
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "again"})
	if !strings.Contains(got, "only a line the OPERATOR submits") {
		t.Fatalf("the bound refusal does not name the operator as the renewer: %q", got)
	}
}

// TestChatSurfaceRenewal_BoundStillTerminates is claim 3: with no user line, a
// period ends at the bound, exactly once, and the last turn carries its own
// ending.
func TestChatSurfaceRenewal_BoundStillTerminates(t *testing.T) {
	a := chatSurfaceRenewalAgent()
	closing := 0
	for i := 1; i <= chatSurfaceLoopBound; i++ {
		if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "turn"}); !strings.Contains(got, "typed=true") {
			t.Fatalf("type #%d returned %q", i, got)
		}
		turn := a.ChatSurfaceTakePending()
		if turn.Closing {
			closing++
			if i != chatSurfaceLoopBound {
				t.Fatalf("Closing was set on turn %d, want only the last (%d)", i, chatSurfaceLoopBound)
			}
		}
	}
	if closing != 1 {
		t.Fatalf("the ending was announced %d time(s) in one period, want exactly 1", closing)
	}
	// And it stays terminated: nothing but a user line re-opens it.
	for i := 0; i < 3; i++ {
		if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "more"}); !strings.Contains(got, "refused") {
			t.Fatalf("the terminated period accepted another turn: %q", got)
		}
		if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamNone {
			t.Fatalf("a terminated period handed out a turn: %+v", turn)
		}
	}
}

// TestChatSurfaceRenewal_StopIsNotLiftedByAUserLine is claim 4: renewal re-opens
// a budget, never a loop the operator ended.
func TestChatSurfaceRenewal_StopIsNotLiftedByAUserLine(t *testing.T) {
	a := chatSurfaceRenewalAgent()
	// Start a period (so bound > 0 and a reset is otherwise available), then stop
	// the loop with a line still in hand — the stop that actually cancels.
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "step"}); !strings.Contains(got, "typed=true") {
		t.Fatalf("type returned %q", got)
	}
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionStop}); !strings.Contains(got, "pending_dropped=true") {
		t.Fatalf("stop returned %q, want the drop named", got)
	}

	ren := a.ChatSurfaceUserTurn()
	if ren.Renewed || ren.Deferred {
		t.Fatalf("a user line lifted a latched stop: %+v", ren)
	}
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "after stop"}); !strings.Contains(got, "refused") || !strings.Contains(got, "does not restart itself") {
		t.Fatalf("type after a stop returned %q, want the stop refusal", got)
	}
}

// TestChatSurfaceRenewal_MidPeriodUserLineIsDeferredNotApplied is claim 5: when a
// self-driven line is still in hand the operator's line is NOT the boundary, and
// the refusal is a named outcome rather than a silent no-op.
func TestChatSurfaceRenewal_MidPeriodUserLineIsDeferredNotApplied(t *testing.T) {
	a := chatSurfaceRenewalAgent()
	// A period is open, and the tool has typed a line that has NOT been consumed:
	// the model's turn, not the operator's, owns the next boundary.
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "model's line"}); !strings.Contains(got, "typed=true") {
		t.Fatalf("type returned %q", got)
	}
	ren := a.ChatSurfaceUserTurn()
	if !ren.Deferred {
		t.Fatalf("a user line with a self-driven line in hand reported %+v, want Deferred", ren)
	}
	if ren.Renewed {
		t.Fatalf("the deferred boundary also reported Renewed")
	}
	// NOTHING MOVED: the counter is where it was, and the model's line is still
	// the one that will run (a deferral must not consume it either).
	if used := a.ChatSurfaceLoopUsed(); used != 0 {
		t.Fatalf("used=%d after a deferral, want 0 (the reset was refused, not applied elsewhere)", used)
	}
	turn := a.ChatSurfaceTakePending()
	if turn.Kind != ChatSurfaceSeamTyped || turn.Line != "model's line" {
		t.Fatalf("the deferred renewal changed whose line runs: %+v", turn)
	}

	// The operator re-opens the period by submitting another line, which is
	// exactly what the refusal tells them (the carrier prints the notice).
	if used := a.ChatSurfaceLoopUsed(); used != 1 {
		t.Fatalf("used=%d after the period's first turn, want 1", used)
	}
	if ren := a.ChatSurfaceUserTurn(); !ren.Renewed || ren.Deferred {
		t.Fatalf("a clean boundary reported %+v, want Renewed", ren)
	}
	if used := a.ChatSurfaceLoopUsed(); used != 0 {
		t.Fatalf("the reset left used=%d, want 0", used)
	}
}

// TestChatSurfaceRenewal_NoSurfaceNoBudget pins the one refusal that is not about
// the operator: a run with no surface installed (a worker, a one-shot) has no
// self-driven loop to renew, so a line must not claim to have opened a period —
// there is nothing that could ever spend it.
func TestChatSurfaceRenewal_NoSurfaceNoBudget(t *testing.T) {
	a := &Agent{}
	ren := a.ChatSurfaceUserTurn()
	if ren.Renewed || ren.Deferred {
		t.Fatalf("a run with no chat surface reported a renewal: %+v", ren)
	}
	if ren.Bound != chatSurfaceLoopBound {
		t.Fatalf("bound=%d with no surface, want the declared default %d", ren.Bound, chatSurfaceLoopBound)
	}
}
