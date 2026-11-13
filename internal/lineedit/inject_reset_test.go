package lineedit

// inject_reset_test.go — the RESET half of the key ceiling: who may open a fresh
// key budget, and what a reset is not allowed to do.
//
// WHY THIS IS A SEPARATE CONTRACT FROM THE CEILING ITSELF. The ceiling is a
// guarantee that a model cannot spend an unbounded number of keystrokes (the
// incident behind it: devdoc/runaway-loop-20260923.md). A guarantee whose
// counter the bounded party can move is decoration, so the counter has exactly
// one owner on the action side — ResetKeys — and exactly one caller, which sits
// on the CARRIER's line arm where the provenance of a line is still known (see
// agent.ChatSurfaceUserTurn). Nothing in the tool's vocabulary reaches it.
//
// The four claims below are the contract, each with its own failure mode:
//
//  1. A RESET OPENS A FRESH PERIOD — the next key is accepted again, and it
//     reports 1 of N, not "N+1 of N". Without it one exhausted period ends the
//     interaction capability for the whole session, however much supervision the
//     operator supplies.
//  2. A RESET NEVER RAISES THE CEILING — the constructor's value is the
//     session's DECLARATION, and both the reset and an injected caller-supplied
//     bound may only tighten it. A reset that restored a period's larger number
//     would be a way to grow the bound without an operator.
//  3. A CALLER'S BOUND CANNOT RAISE IT EITHER — InjectKeyBounded is the tool's
//     path (the model supplies `max_keys`, already clamped by the tool), so the
//     min-of-the-two rule is a second line of defence behind that clamp rather
//     than the only one.
//  4. NOTHING ELSE RESETS — injecting past the ceiling any number of times
//     leaves the counter exactly where the ceiling put it. This is the
//     in-package half of the tree-level negative case (the agent package drives
//     it through every action the wire advertises).

import (
	"strings"
	"testing"
)

// injectResetCeiling is the constructor ceiling these cases install: small
// enough that a period is spent in a few calls, large enough to distinguish "a
// fresh period" from "one key left".
const injectResetCeiling = 4

// TestInject_ResetOpensAFreshKeyPeriod is claim 1 at the injector's own
// boundary: the period that was spent accepts keys again, and the counter it
// reports is the NEW period's.
func TestInject_ResetOpensAFreshKeyPeriod(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectResetCeiling)

	for i := 1; i <= injectResetCeiling; i++ {
		if res := inj.InjectKey(KeyNameDown); !res.Accepted {
			t.Fatalf("key %d of %d refused: %s", i, injectResetCeiling, res.Why)
		}
	}
	spent := inj.InjectKey(KeyNameDown)
	if spent.Accepted {
		t.Fatalf("key past the ceiling accepted = true, want a refusal")
	}
	if !strings.Contains(spent.Why, "ceiling is reached") {
		t.Fatalf("the refusal must name the ceiling; got %q", spent.Why)
	}

	// THE RESET: a fresh period, and the ceiling that period runs against is the
	// session's own declaration again.
	inj.ResetKeys(0)
	if used := inj.Used(); used != 0 {
		t.Fatalf("used=%d after a reset, want 0 (a fresh period)", used)
	}
	if bound := inj.Bound(); bound != injectResetCeiling {
		t.Fatalf("bound=%d after a reset, want the constructor's %d", bound, injectResetCeiling)
	}
	fresh := inj.InjectKey(KeyNameUp)
	if !fresh.Accepted {
		t.Fatalf("the first key of the fresh period was refused: %s", fresh.Why)
	}
	if fresh.Used != 1 || fresh.Bound != injectResetCeiling {
		t.Fatalf("the fresh period reports %d/%d, want 1/%d — the count is the PERIOD's, not a session total",
			fresh.Used, fresh.Bound, injectResetCeiling)
	}
	if fresh.Last {
		t.Fatalf("the first key of a fresh period reported Last=true")
	}
}

// TestInject_ResetNeverRaisesTheCeiling is claim 2: a reset may re-establish the
// session's declaration or tighten it to a period bound the caller already asked
// for — never raise it.
func TestInject_ResetNeverRaisesTheCeiling(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectResetCeiling)

	// A reset asking for MORE than the declaration keeps the declaration.
	inj.ResetKeys(injectResetCeiling * 100)
	if bound := inj.Bound(); bound != injectResetCeiling {
		t.Fatalf("ResetKeys(%d) moved the ceiling to %d, want the declared %d — a reset must not raise a bound",
			injectResetCeiling*100, bound, injectResetCeiling)
	}

	// A reset asking for LESS tightens THIS period only, and the next period that
	// names no bound is back to the declaration (a period's experiment is not the
	// session's new normal).
	tighter := injectResetCeiling - 2
	inj.ResetKeys(tighter)
	if bound := inj.Bound(); bound != tighter {
		t.Fatalf("ResetKeys(%d) left the ceiling at %d, want %d", tighter, bound, tighter)
	}
	if res := inj.InjectKey(KeyNameDown); !res.Accepted || res.Bound != tighter {
		t.Fatalf("the tightened period's first key reports %+v, want an acceptance bounded by %d", res, tighter)
	}
	inj.ResetKeys(0)
	if bound := inj.Bound(); bound != injectResetCeiling {
		t.Fatalf("after a bare reset the ceiling is %d, want the declared %d again", bound, injectResetCeiling)
	}
}

// TestInject_CallersBoundCannotRaiseTheCeiling is claim 3: the per-call bound the
// tool passes (from `max_keys`, which the model chooses) can only tighten.
func TestInject_CallersBoundCannotRaiseTheCeiling(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectResetCeiling)

	// Above the declaration: clamped DOWN to it, and the refusal that eventually
	// comes quotes the declaration rather than the model's wish.
	first := inj.InjectKeyBounded(KeyNameDown, injectResetCeiling*10)
	if !first.Accepted {
		t.Fatalf("the first key was refused: %s", first.Why)
	}
	if first.Bound != injectResetCeiling {
		t.Fatalf("a caller bound of %d produced bound=%d, want the declared %d",
			injectResetCeiling*10, first.Bound, injectResetCeiling)
	}

	// Below it: that number is what the period is charged against, and the
	// refusal must quote it (the number in the sentence is the number enforced).
	inj.ResetKeys(0)
	small := injectResetCeiling - 1
	for i := 1; i <= small; i++ {
		if res := inj.InjectBytesBounded("x", small); !res.Accepted {
			t.Fatalf("key %d of %d refused: %s", i, small, res.Why)
		}
	}
	res := inj.InjectBytesBounded("x", small)
	if res.Accepted {
		t.Fatalf("the key past the caller's bound was accepted")
	}
	if !strings.Contains(res.Why, "ceiling is reached") || res.Bound != small {
		t.Fatalf("the refusal reports bound=%d and says %q, want the enforced %d", res.Bound, res.Why, small)
	}
}

// TestInject_NothingElseOpensAFreshPeriod is claim 4: the counter moves on ONE
// call and no other. It is the in-package half of the tree-level negative case —
// here it is "injecting again", there it is "every action the wire advertises".
func TestInject_NothingElseOpensAFreshPeriod(t *testing.T) {
	src := &scriptedSource{width: injectCols, height: injectRows, holdAtEnd: true}
	t.Cleanup(src.stopSource)
	inj := NewInjector(src, injectResetCeiling)

	for i := 0; i < injectResetCeiling; i++ {
		if res := inj.InjectKey(KeyNameDown); !res.Accepted {
			t.Fatalf("key %d refused early: %s", i+1, res.Why)
		}
	}
	// Every shape of injection a caller has: named keys, literal bytes, and the
	// multi-key helper. None of them is a reset.
	for i := 0; i < injectResetCeiling+2; i++ {
		if res := inj.InjectKey(KeyNameEnter); res.Accepted {
			t.Fatalf("call %d past the ceiling was accepted: %+v", i+1, res)
		}
		if res := inj.InjectBytes("/"); res.Accepted {
			t.Fatalf("literal bytes past the ceiling were accepted: %+v", res)
		}
		if used := inj.Used(); used != injectResetCeiling {
			t.Fatalf("used=%d after %d more attempts, want %d — nothing but a reset moves the counter",
				used, i+1, injectResetCeiling)
		}
	}
	for _, res := range inj.InjectKeys(KeyNameDown, KeyNameUp, KeyNameEnter) {
		if res.Accepted {
			t.Fatalf("InjectKeys accepted a key past the ceiling: %+v", res)
		}
	}
	if used := inj.Used(); used != injectResetCeiling {
		t.Fatalf("used=%d after InjectKeys, want %d", used, injectResetCeiling)
	}
}
