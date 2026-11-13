package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Test budgets for the attribution rule. Named so the two shapes the rule turns
// on — CLAMPED (idle == abs) and UNCAPPED (idle < abs) — are readable at the
// call site (Rule 1).
const (
	attributionShortBudget = 1 * time.Second  // timeout_sec=1: the clamp shape
	attributionShortIdle   = 1 * time.Second  // == abs: bashBudget clamped the net
	attributionLongIdle    = 30 * time.Second // > abs: the net is its own cause
	attributionLongBudget  = 30 * time.Second // > the net: nothing is clamped
)

// TestBashKillReasonAttribution_IsDecidedByBudgetShape pins the rule that
// removed the flaky wording in TestBashWithTimeout_PositiveKills: a kill that
// came due on the idle net is attributed from the BUDGET SHAPE, never from
// which watcher (step 6b vs step 8) got to the kill slot first.
//
// The clamp in bashBudget (idle = min(idle, abs)) makes the net and the budget
// come due in the same second whenever the budget is the smaller of the two, so
// the ARRIVAL ORDER is scheduler-dependent — and an attribution read off
// order/`cmdCtx.Err()` flips the message run to run. Each row below states the
// budget pair and what the caller must be told; the RED-proof is the first row:
// before the fix the kill site asked cmdCtx.Err() and — on the runs where the
// idle watcher won — answered KillReasonIdleTimeout, i.e. "no output for 1s" for
// a call whose budget had run out.
func TestBashKillReasonAttribution_IsDecidedByBudgetShape(t *testing.T) {
	// A context whose deadline has ALREADY passed, i.e. cmdCtx.Err() ==
	// DeadlineExceeded — the state the idle watcher may or may not observe,
	// depending on arrival order.
	expired := func() context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		t.Cleanup(cancel)
		<-ctx.Done() // the deadline has genuinely fired
		return ctx
	}
	// A context with no deadline at all — the unlimited (timeout_sec=0) shape.
	unlimited := func() context.Context { return context.Background() }

	cases := []struct {
		name     string
		ctx      context.Context
		abs      time.Duration
		idle     time.Duration
		expected string
	}{
		{
			// THE BUG: the clamped pair, seen from the idle watcher BEFORE the
			// deadline is observable through cmdCtx. Budget shape decides.
			name:     "clamped idle at the budget stays the budget",
			ctx:      unlimited(),
			abs:      attributionShortBudget,
			idle:     attributionShortIdle,
			expected: KillReasonAbsoluteTimeout,
		},
		{
			// The same clamped pair, this time with the deadline already fired
			// (the other arrival order) — the same answer. This is what makes
			// the message deterministic regardless of the race.
			name:     "clamped idle with the deadline already fired",
			ctx:      expired(),
			abs:      attributionShortBudget,
			idle:     attributionShortIdle,
			expected: KillReasonAbsoluteTimeout,
		},
		{
			// The idle net's OWN case: it fired while the budget is still far
			// away (the configured net, 120s, against a 30s budget — nothing
			// was clamped). Must keep saying "no output for".
			name:     "uncapped idle before the budget is the idle net",
			ctx:      unlimited(),
			abs:      attributionLongBudget,
			idle:     attributionShortIdle,
			expected: KillReasonIdleTimeout,
		},
		{
			// An uncapped net that fires as the budget also expires is still a
			// budget death.
			name:     "uncapped idle as its own deadline fires",
			ctx:      expired(),
			abs:      attributionLongBudget,
			idle:     attributionLongIdle,
			expected: KillReasonAbsoluteTimeout,
		},
		{
			// timeout_sec=0 (unlimited): there is no budget to expire, so the
			// net can only ever be an idle death.
			name:     "unlimited budget has no budget death",
			ctx:      unlimited(),
			abs:      0,
			idle:     attributionLongIdle,
			expected: KillReasonIdleTimeout,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := killReasonIdleOrBudget(tc.ctx, tc.abs, tc.idle)
			if got != tc.expected {
				t.Errorf("killReasonIdleOrBudget(abs=%v, idle=%v) = %q, want %q",
					tc.abs, tc.idle, got, tc.expected)
			}
		})
	}
}

// TestBashBudget_ClampIsWhatMakesTheShapeDecidable guards the PREMISE the rule
// rests on: bashBudget is the one place that clamps the net down to the budget,
// so "idle == abs" is evidence of a clamp rather than a coincidence. If a future
// change stops clamping, the shape test above would start lying.
func TestBashBudget_ClampIsWhatMakesTheShapeDecidable(t *testing.T) {
	// A budget far below the configured net must clamp.
	abs, idle := bashBudget(int(attributionShortBudget / time.Second))
	if abs != attributionShortBudget {
		t.Fatalf("bashBudget(1) abs = %v, want %v", abs, attributionShortBudget)
	}
	if idle != abs {
		t.Errorf("bashBudget(1) idle = %v, want it clamped to abs %v", idle, abs)
	}

	// A budget far above the configured net must not clamp (the net keeps its
	// own value, which is what makes the uncapped branch reachable).
	abs, idle = bashBudget(0)
	if abs != 0 {
		t.Errorf("bashBudget(0) abs = %v, want 0 (unlimited)", abs)
	}
	if idle != BashIdleTimeout() {
		t.Errorf("bashBudget(0) idle = %v, want the configured net %v", idle, BashIdleTimeout())
	}
}

// TestBashWithTimeout_PositiveKills_MessageIsStable is the regression guard on
// the REAL path: it drives BashWithTimeout end to end and asserts the exact
// wording, so the flaky attribution cannot come back silently. The repetition
// count here is small on purpose — the measurement is `-count=N` on this test
// (Rule 27: no synthesised load), and the guard's job is the MESSAGE, not the
// timing.
//
// RED-proof: on the pre-fix tree this fails within a few runs with
// `command killed: no output for 1s`.
func TestBashWithTimeout_PositiveKills_MessageIsStable(t *testing.T) {
	const (
		iterations = 3
		// The wording each death deserves, taken from the constants that
		// produce them rather than re-typed.
		wantSubstring = "timed out"
		unwanted      = "no output for"
	)

	for i := 0; i < iterations; i++ {
		output, err := BashWithTimeout("sleep 2", int(attributionShortBudget/time.Second))
		if err == nil {
			t.Fatalf("iteration %d: expected a timeout error, got nil", i)
		}
		if !strings.Contains(err.Error(), wantSubstring) {
			t.Fatalf("iteration %d: error = %q, want %q in it (the clamped idle net was attributed as idle)",
				i, err.Error(), wantSubstring)
		}
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("iteration %d: error = %q must not carry the idle wording %q",
				i, err.Error(), unwanted)
		}
		if !strings.Contains(output, wantSubstring) {
			t.Errorf("iteration %d: output = %q, want %q in it", i, output, wantSubstring)
		}
	}
}
