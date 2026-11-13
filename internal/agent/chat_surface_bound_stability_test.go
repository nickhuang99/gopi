package agent

// chat_surface_bound_stability_test.go — the bound's own stability, which is a
// SEPARATE claim from the renewal's reachability (chat_surface_renewal_test.go).
//
// The renewal suite pins WHO may open a period. This file pins the other half of
// the same ruling: once a period is running, the number the period is bounded by
// belongs to the SESSION, not to the model — a `type` that arrives between two
// of the model's own turns must not move it.
//
// WHY THIS IS NOT A STYLE POINT (measured against the shipped code, 2026-09-28).
// The set-site was keyed on the SEAM BEING IDLE (`if a.chatSurface.pending == ""`)
// rather than on the bound being UNSET (`if a.chatSurface.bound == 0`), and the
// seam is idle on EVERY turn boundary — the REPL consumes the pending line before
// the next model request is made. So the set ran on every `type`, and
// `chatSurfaceLoopBoundArgs` happily accepted a LOWER value: a model that asked
// for `max_turns: 2` on its second turn in the same period moved the period's
// bound from the declared 8 down to the floor of 2, cutting the period short at a
// number the operator never chose and could not see. The ceiling itself never
// rose (the clamp is downward-only), so this was never an escape past the bound —
// it was the bound MOVING UNDER THE MODEL, which is the same class of defect the
// operator's ruling forbids: the model may not decide how long its own leash is.
//
// The sibling action=key had it right all along (`if a.chatSurface.keys == 0`,
// "first key of the session"); this file holds `type` to the same shape.

import (
	"strings"
	"testing"
)

// TestChatSurfaceRenewal_ModelCannotMoveThePeriodBoundDown is the anti-regression
// guard for the set-site: within ONE period, a later `type` may not change the
// bound the period is running under — in either direction.
func TestChatSurfaceRenewal_ModelCannotMoveThePeriodBoundDown(t *testing.T) {
	a := chatSurfaceRenewalAgent()

	// The period's first type, asking for nothing: the declared bound comes into
	// force, as the tool's own documentation promises.
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "turn 1"})
	if !strings.Contains(got, "typed=true") {
		t.Fatalf("turn 1 returned %q, want an acceptance", got)
	}
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamTyped {
		t.Fatalf("consumption of turn 1 = %+v, want a typed turn", turn)
	}
	if bound := a.ChatSurfaceLoopBound(); bound != chatSurfaceLoopBound {
		t.Fatalf("bound=%d after the period's first type, want the declared %d", bound, chatSurfaceLoopBound)
	}

	// Turn 2 of the SAME period asks for the smallest legal bound. This must be
	// refused BY NAME — silently applying it is the defect this test exists for.
	got = a.runChatSurfaceTool(map[string]interface{}{
		"action": ChatSurfaceActionType, "text": "turn 2", "max_turns": float64(chatSurfaceRestartFloor),
	})
	if !strings.Contains(got, "typed=true") {
		t.Fatalf("turn 2 returned %q, want the line to still be accepted (the line is legal; only its max_turns is stale)", got)
	}
	if bound := a.ChatSurfaceLoopBound(); bound != chatSurfaceLoopBound {
		t.Fatalf("the model moved the period's bound from %d to %d with a mid-period max_turns — the bound is the session's, set at the first type of the session, and it must not move under the model",
			chatSurfaceLoopBound, bound)
	}
	if !strings.Contains(got, "max_turns=2 was IGNORED") {
		t.Fatalf("turn 2's answer did not say the request could not move the bound: %q", got)
	}
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamTyped {
		t.Fatalf("consumption of turn 2 = %+v, want a typed turn", turn)
	}
	if used := a.ChatSurfaceLoopUsed(); used != 2 {
		t.Fatalf("used=%d after two types, want 2", used)
	}

	// The period keeps the budget it started with: the remaining turns of the
	// declared bound are STILL SPENDABLE. Under the defect the period had already
	// been cut to the floor, so turn 3 was refused.
	spendRestOfPeriod(t, a, 3, "the period's declared bound must survive a mid-period max_turns")
}

// TestChatSurfaceRenewal_PeriodBoundIsStableUntilRenewed pins the other
// direction: an operator line opens a fresh period, and the new period is bounded
// by the SESSION's number — a renewal reinstates the budget, it does not hand the
// bound back to the model.
func TestChatSurfaceRenewal_PeriodBoundIsStableUntilRenewed(t *testing.T) {
	a := chatSurfaceRenewalAgent()
	spendPeriod(t, a, "period 1")

	if ren := a.ChatSurfaceUserTurn(); !ren.Renewed {
		t.Fatalf("the operator's line reported %+v, want Renewed", ren)
	}
	if bound := a.ChatSurfaceLoopBound(); bound != chatSurfaceLoopBound {
		t.Fatalf("bound=%d after a renewal, want the declared %d", bound, chatSurfaceLoopBound)
	}

	// The fresh period spends the SAME bound, not a number the model picked.
	spendPeriod(t, a, "period 2")
}

// TestChatSurfaceRenewal_FirstTypeStillChoosesTheSessionBound pins what the fix
// must NOT change: the FIRST type of the session still sets the bound from the
// caller's max_turns (the feature the set-site exists for), it is only the
// SECOND and later types that are ignored.
func TestChatSurfaceRenewal_FirstTypeStillChoosesTheSessionBound(t *testing.T) {
	const chosen = 3
	a := chatSurfaceRenewalAgent()
	got := a.runChatSurfaceTool(map[string]interface{}{
		"action": ChatSurfaceActionType, "text": "turn 1", "max_turns": float64(chosen),
	})
	if !strings.Contains(got, "typed=true") || !strings.Contains(got, "bound_in_force=3") {
		t.Fatalf("the first type did not put the caller's bound in force: %q", got)
	}
	if strings.Contains(got, "IGNORED") {
		t.Fatalf("the FIRST type reported its own max_turns as ignored: %q", got)
	}
	if bound := a.ChatSurfaceLoopBound(); bound != chosen {
		t.Fatalf("bound=%d after the first type asked for %d", bound, chosen)
	}
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamTyped {
		t.Fatalf("consumption of turn 1 = %+v, want a typed turn", turn)
	}
	spendRestOfPeriod(t, a, 2, "a caller-chosen bound of 3 must be spendable to 3")
}

// spendRestOfPeriod spends turns first..chatSurfaceLoopBound of the period in
// force, failing on the first refusal — i.e. it asserts the period still HAD a
// budget left to spend, which is exactly what a moved bound destroys.
func spendRestOfPeriod(t *testing.T, a *Agent, first int, why string) {
	t.Helper()
	bound := a.ChatSurfaceLoopBound()
	for i := first; i <= bound; i++ {
		got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "turn"})
		if !strings.Contains(got, "typed=true") {
			t.Fatalf("%s: type #%d of %d was refused (%q)", why, i, bound, got)
		}
		if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamTyped {
			t.Fatalf("%s: consumption #%d = %+v, want a typed turn", why, i, turn)
		}
	}
	if used := a.ChatSurfaceLoopUsed(); used != bound {
		t.Fatalf("%s: used=%d at the end of the period, want %d", why, used, bound)
	}
}
