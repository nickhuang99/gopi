package agent

// sessionbudget_test.go — the session time budget the PROMPT teaches (P1-P3).
//
// Three faces, one source: the tick-clock line, the task-management rule and
// the commit return's elapsed/budget line all render from sessionBudget() /
// the three budget constants. These tests pin the source's resolution rules
// (default / env / 0=unlimited / invalid), the commit return at both sides of
// the escalation mark, and that the assembled prompt actually carries the rule.

import (
	"strings"
	"testing"
	"time"
)

// TestSessionBudgetResolution pins the ONE reader's contract (Rule 2): unset
// renders the default, a positive integer is that many MINUTES, "0" means
// UNLIMITED and keeps meaning it (0, not a re-substituted default), and an
// invalid value warns and falls back rather than silently disabling the
// budget.
func TestSessionBudgetResolution(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want time.Duration
	}{
		{"unset renders the default", "", false, sessionBudgetMinutes * time.Minute},
		{"positive overrides in minutes", "30", true, 30 * time.Minute},
		{"0 is unlimited, not a fallback", "0", true, 0},
		{"invalid falls back to the default", "abc", true, sessionBudgetMinutes * time.Minute},
		{"negative falls back to the default", "-5", true, sessionBudgetMinutes * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(sessionBudgetEnv, tc.env)
			} else {
				// An exported-empty env is indistinguishable from unset to
				// sessionBudget(), which is the behaviour under test; an
				// inherited value from the parent process would otherwise
				// leak in.
				t.Setenv(sessionBudgetEnv, "")
			}
			if got := sessionBudget(); got != tc.want {
				t.Fatalf("sessionBudget() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSessionBudgetIsNotTheWallclockCap is the Rule-28/Rule-2 guard on the
// FINDING this epic raised: the prompt budget and the engine's 8h cap are two
// different numbers with two different jobs. Folding them together (or letting
// a future edit read the cap's env from the budget resolver) is the thing to
// catch — so the test pins that they are unequal and separately sourced.
func TestSessionBudgetIsNotTheWallclockCap(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "")
	if sessionBudget() == defaultSessionWallclock {
		t.Fatalf("session budget (%v) equals the engine cap (%v) — they are separate numbers: "+
			"one is taught, the other is enforced", sessionBudget(), defaultSessionWallclock)
	}
	if sessionBudgetEnv == sessionWallclockEnv {
		t.Fatalf("budget env (%s) is the cap env (%s) — one knob would collapse two jobs", sessionBudgetEnv, sessionWallclockEnv)
	}
}

// TestSessionBudgetLine pins the commit-return line on BOTH sides of the
// escalation mark: below it a plain report, at/past it the report PLUS the one
// concrete move the model has left.
func TestSessionBudgetLine(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "") // 60m default
	a := &Agent{Clock: &SessionClock{t0: time.Now().Add(-10 * time.Minute)}}
	line := a.sessionBudgetLine()
	if !strings.Contains(line, "session budget:") || !strings.Contains(line, "elapsed of") {
		t.Fatalf("below the mark the line must report elapsed/budget, got %q", line)
	}
	if !strings.Contains(line, "16%") {
		t.Fatalf("line must carry the used percentage (10m of 60m = 16%%), got %q", line)
	}
	if strings.Contains(line, "TOO_BIG") {
		t.Fatalf("below the mark the line must NOT escalate, got %q", line)
	}

	// 50m/60m = 83% — past the 75% mark.
	a.Clock = &SessionClock{t0: time.Now().Add(-50 * time.Minute)}
	line = a.sessionBudgetLine()
	if !strings.Contains(line, "83%") {
		t.Fatalf("past-the-mark line must report the real percentage, got %q", line)
	}
	for _, want := range []string{"TOO_BIG", "task(action=reframe, verdict=TOO_BIG)", "75%"} {
		if !strings.Contains(line, want) {
			t.Fatalf("past-the-mark line must carry %q (the concrete available action), got %q", want, line)
		}
	}
}

// TestSessionBudgetLineUnlimitedAndNoClock: 0 = unlimited means the line is
// OMITTED — not rendered against a substituted denominator — and an Agent
// literal with no clock has no elapsed to report. Both leave the commit return
// byte-identical to what it was before this feature.
func TestSessionBudgetLineUnlimitedAndNoClock(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "0")
	a := &Agent{Clock: &SessionClock{t0: time.Now().Add(-99 * time.Hour)}}
	if line := a.sessionBudgetLine(); line != "" {
		t.Fatalf("unlimited budget must render no line, got %q", line)
	}

	t.Setenv(sessionBudgetEnv, "")
	if line := (&Agent{}).sessionBudgetLine(); line != "" {
		t.Fatalf("a nil clock must render no line, got %q", line)
	}
}

// TestCommitTaskItemReturnCarriesBudgetLine is the P3 acceptance at the real
// entry point: the string task(action=commit) hands back carries the
// elapsed/budget line, and the unlimited mode leaves it untouched.
func TestCommitTaskItemReturnCarriesBudgetLine(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "")
	a := newAutoTaskAgent(t)
	a.Clock = &SessionClock{t0: time.Now().Add(-46 * time.Minute)} // 76% → escalated

	args := map[string]interface{}{
		"task_id":    "1",
		"status":     "completed",
		"conclusion": "budget line check",
	}
	got := a.commitTaskItem(args)
	if !strings.Contains(got, "session budget:") {
		t.Fatalf("commit return must carry the elapsed/budget line, got %q", got)
	}
	if !strings.Contains(got, "TOO_BIG") || !strings.Contains(got, "task(action=reframe, verdict=TOO_BIG)") {
		t.Fatalf("past 75%% the commit return must name the available action, got %q", got)
	}

	t.Setenv(sessionBudgetEnv, "0")
	got = a.commitTaskItem(args)
	if strings.Contains(got, "session budget:") {
		t.Fatalf("unlimited budget must not decorate the commit return, got %q", got)
	}
}

// TestSystemPromptRendersTheBudget is the P1/P2 acceptance on the rendered
// prompt: both faces carry the rule, and the rendered NUMBER comes from the
// budget source rather than a typed-in literal (Rule 1b/13) — the env override
// must change what the prompt says.
func TestSystemPromptRendersTheBudget(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "")
	p := assembleSystemPrompt()
	for _, want := range []string{
		"Session budget 60min",
		"session budget 60min",
		"TOO BIG by definition",
		"running out is a planning failure, not a graceful ending",
		"past 75%",
		"task(action=reframe, verdict=TOO_BIG)",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("rendered system prompt missing budget text %q", want)
		}
	}
	// The number is the source's, not a literal: re-render through the env.
	t.Setenv(sessionBudgetEnv, "15")
	p = assembleSystemPrompt()
	if !strings.Contains(p, "Session budget 15min") || !strings.Contains(p, "session budget 15min") {
		t.Fatalf("prompt did not pick up %s=15 — the rendered number is not sourced from sessionBudget()", sessionBudgetEnv)
	}
	if strings.Contains(p, "60min") {
		t.Fatalf("the default 60min literal survived an override — a number is typed in, not rendered")
	}
}

// TestClockRuleCarriesTheBudgetNotJustTheTick guards the P1 fold: the budget
// lives ON the tick line (the model already reads the tick every round), and
// the tick line's decision attachment (tickDecisionLabel) survives alongside
// it. Both clock spellings come from clock.go, so this test tracks the clock
// instead of pinning a format that can drift out from under it (Rule 1/13).
func TestClockRuleCarriesTheBudgetNotJustTheTick(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "")
	rule := clockRule()
	for _, want := range []string{"ticking clock runs from session start", tickShapeLabel, tickDecisionLabel, "Session budget 60min", "TOO BIG by definition"} {
		if !strings.Contains(rule, want) {
			t.Errorf("clockRule() missing %q: %q", want, rule)
		}
	}
}

// TestUnlimitedBudgetRendersNoCeiling: 0 = unlimited must not surface a
// "0min" budget in either prompt face — that would teach a ceiling nobody
// has, and the point of unlimited is that the budget claim is ABSENT while the
// clock and its t+300s decision stay.
func TestUnlimitedBudgetRendersNoCeiling(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "0")
	if rule := clockRule(); strings.Contains(rule, "0min") || strings.Contains(rule, "Session budget") {
		t.Fatalf("unlimited clock rule must claim no ceiling, got %q", rule)
	} else if !strings.Contains(rule, "ticking clock runs from session start") || !strings.Contains(rule, tickDecisionLabel) {
		t.Fatalf("unlimited clock rule must keep the tick and its decision, got %q", rule)
	}
	if r5 := sessionBudgetRule(); !strings.Contains(r5, "unlimited") || strings.Contains(r5, "0min") {
		t.Fatalf("unlimited rule 5 must say so plainly, got %q", r5)
	}
	p := assembleSystemPrompt()
	if strings.Contains(p, "0min") {
		t.Fatal("the rendered prompt claims a 0min budget under GOPI_SESSION_BUDGET=0")
	}
	if !strings.Contains(p, "BUDGET: unlimited") {
		t.Fatal("unlimited mode must still render a budget rule (the tick line still needs its ceiling statement)")
	}
	// The same KERNEL budget the density gate enforces, through the same
	// helper: the unlimited prompt must also fit, and (as there) the operator's
	// reply-language directive is not the kernel's to spend.
	if n := len(ruleKernel()); n >= systemPromptDensityBudget {
		t.Fatalf("unlimited prompt is %d chars >= %d", n, systemPromptDensityBudget)
	}
}
