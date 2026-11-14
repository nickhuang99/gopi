package agent

// sessionbudgetgate_test.go — the commit-beat budget GATE (option A) and the
// reframe TIME DIMENSION (option B).
//
// Both features are deliberately NARROW (the operator's constraint: 强制不好).
// What the tests must pin is therefore not only that the gate fires, but that
// everything it must NOT touch is untouched: budget 0 (unlimited), a nil clock
// and a below-the-mark session keep the commit return byte-identical, and a
// commit that CLOSES the last open item is always allowed (that is the
// documented "land the work" exit — a gate that refuses it would deadlock the
// very escape it names).

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/taskverdict"
)

// budgetTestAgent builds an agent whose ONLY variable is the clock, so a test
// comparison never accidentally varies two things at once.
func budgetTestAgent(t *testing.T, ago time.Duration) *Agent {
	t.Helper()
	a := newAutoTaskAgent(t)
	a.Clock = &SessionClock{t0: time.Now().Add(-ago)}
	a.TaskState = newTodoState()
	return a
}

// budgetCommitArgs is the minimal arity-valid task(action=commit) payload.
func budgetCommitArgs(id string) map[string]interface{} {
	return map[string]interface{}{
		"task_id":    id,
		"status":     "completed",
		"conclusion": "gate fixture",
	}
}

// TestSessionBudgetGateBands is the table-driven pin on the gate's three bands,
// exercised at the REAL dispatch entry point (commitTaskItem — what
// task(action=commit) calls), not on the predicate alone: the refusal must
// actually short-circuit the write, and the two allowed bands must actually
// record.
func TestSessionBudgetGateBands(t *testing.T) {
	openList := []TaskItem{
		{TaskID: "1", Summary: "recon", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "implement", Status: TaskStatusProgressing},
	}
	closedList := []TaskItem{
		{TaskID: "1", Summary: "recon", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "implement", Status: TaskStatusCompleted},
	}

	cases := []struct {
		name      string
		minutes   string // GOPI_SESSION_BUDGET ("" = 60m default)
		ago       time.Duration
		list      []TaskItem
		wantRefus bool
	}{
		{
			name: "below the mark — unchanged return",
			ago:  10 * time.Minute, // 16% of 60m
			list: openList,
		},
		{
			name:      "past the mark with unfinished items — refused",
			ago:       50 * time.Minute, // 83%
			list:      openList,
			wantRefus: true,
		},
		{
			name: "past the mark with every item completed — allowed (land the work)",
			ago:  50 * time.Minute,
			list: closedList,
		},
		{
			name:    "past the mark with a short budget override — refused",
			minutes: "2",
			ago:     100 * time.Second, // 83% of 2m
			list:    openList,

			wantRefus: true,
		},
		{
			name:    "past the mark but no refusable subtask (no tracked list)",
			minutes: "2",
			ago:     100 * time.Second,
		},
		{
			name:    "unlimited budget ignores a past-any-mark clock",
			minutes: "0",
			ago:     99 * time.Hour,
			list:    openList,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(sessionBudgetEnv, tc.minutes)
			a := budgetTestAgent(t, tc.ago)
			if tc.list != nil {
				a.TaskState.update(tc.list)
			}

			got := a.commitTaskItem(budgetCommitArgs("2"))
			refused := strings.HasPrefix(got, "REFUSED")
			if refused != tc.wantRefus {
				t.Fatalf("refused = %v, want %v (return %q)", refused, tc.wantRefus, got)
			}

			if !tc.wantRefus {
				// The allowed bands keep the ORDINARY commit return: the
				// suite's own success word, never the refusal banner.
				if !strings.Contains(got, "committed") && !strings.Contains(got, "deduped") {
					t.Fatalf("allowed commit must return the ordinary record result, got %q", got)
				}
				if !a.taskAreaActive {
					t.Fatalf("an allowed commit must latch the working area, got taskAreaActive=false (%q)", got)
				}
				return
			}

			// The refused band: the write must NOT have happened (nothing
			// half-committed), and the text must carry the numbers AND both
			// concrete exits — the whole point of returning instead of failing.
			if a.taskAreaActive {
				t.Fatalf("a refused commit must not latch the working area (%q)", got)
			}
			for _, want := range []string{
				"elapsed",
				"remaining",
				"used",
				"LAND THE WORK",
				toolTaskReframeSpelling,
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("refusal must state %q, got %q", want, got)
				}
			}
			for _, verdict := range []string{taskverdict.TooBig} {
				if !strings.Contains(got, verdict) {
					t.Fatalf("refusal must name verdict=%s from taskverdict (Rule 1b), got %q", verdict, got)
				}
			}
		})
	}
}

// toolTaskReframeSpelling is the reframe call as the refusal renders it. The
// TOOL name comes from toolnames in the production code; the test asserts the
// rendered shape, so it spells the call rather than re-deriving the formatter.
const toolTaskReframeSpelling = "task(action=reframe, verdict=TOO_BIG)"

// TestSessionBudgetGateUnlimitedAndNilClockByteIdentical pins the invariant the
// epic calls out by name: with budget 0 (unlimited) or without a clock, the
// commit return carries NO time dependence at all. The comparison is between
// returns from three identically-configured agents whose clocks differ wildly —
// if any of the gate's machinery leaked, the strings would differ.
func TestSessionBudgetGateUnlimitedAndNilClockByteIdentical(t *testing.T) {
	build := func(t *testing.T, minutes string, clock bool, ago time.Duration) string {
		t.Helper()
		t.Setenv(sessionBudgetEnv, minutes)
		a := budgetTestAgent(t, ago)
		if !clock {
			a.Clock = nil // the no-clock shape: an Agent literal built by a test
		}
		a.TaskState.update([]TaskItem{
			{TaskID: "1", Summary: "recon", Status: TaskStatusCompleted},
			{TaskID: "2", Summary: "implement", Status: TaskStatusProgressing},
		})
		return a.commitTaskItem(budgetCommitArgs("2"))
	}

	unlimitedAncient := build(t, "0", true, 99*time.Hour) // unlimited, far past any mark
	unlimitedFresh := build(t, "0", true, time.Second)    // unlimited, just started
	nilClock := build(t, "", false, 0)                    // default budget, no clock

	if unlimitedAncient != unlimitedFresh {
		t.Fatalf("unlimited budget must not depend on elapsed time:\n ancient=%q\n fresh=%q", unlimitedAncient, unlimitedFresh)
	}
	if unlimitedAncient != nilClock {
		t.Fatalf("a nil clock must behave like an unlimited budget (no claim):\n unlimited=%q\n nilclock=%q", unlimitedAncient, nilClock)
	}
	for _, s := range []string{unlimitedAncient, nilClock} {
		if strings.Contains(s, "session budget:") || strings.Contains(s, "REFUSED") {
			t.Fatalf("no-claim mode must leave the return undecorated by the gate, got %q", s)
		}
	}
}

// TestSessionBudgetGateKillSwitch pins the single reader of the kill-switch env
// (Rule 2/21): an explicit off-spelling disarms the gate, and — critically — a
// TYPO does not (a mistyped variable must never silently disable the behaviour
// under measurement).
func TestSessionBudgetGateKillSwitch(t *testing.T) {
	cases := []struct {
		val       string
		wantArmed bool
	}{
		{"", true},
		{"1", true},
		{"on", true},
		{"maybe", true}, // a typo is not an off-spelling
		{"0", false},
		{"false", false},
		{"off", false},
		{"OFF", false},
		{" no ", false},
	}
	for _, tc := range cases {
		t.Run("switch="+tc.val, func(t *testing.T) {
			t.Setenv(sessionBudgetRefusalEnv, tc.val)
			if got := sessionBudgetCommitRefusal(); got != tc.wantArmed {
				t.Fatalf("sessionBudgetCommitRefusal(%q) = %v, want %v", tc.val, got, tc.wantArmed)
			}
		})
	}

	// The switch reaches the real beat, not just the predicate.
	t.Setenv(sessionBudgetEnv, "2")
	t.Setenv(sessionBudgetRefusalEnv, "0")
	a := budgetTestAgent(t, 100*time.Second)
	a.TaskState.update([]TaskItem{{TaskID: "1", Summary: "implement", Status: TaskStatusProgressing}})
	if got := a.commitTaskItem(budgetCommitArgs("1")); strings.HasPrefix(got, "REFUSED") {
		t.Fatalf("kill switch off must let the commit through, got %q", got)
	}
}

// TestSessionBudgetGateStandsAsideAfterReframe pins the anti-deadlock clause: a
// run that has already filed its verdict is EXECUTING the split, so its commits
// must pass (otherwise the gate would refuse the very exit it names).
func TestSessionBudgetGateStandsAsideAfterReframe(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "2")
	a := budgetTestAgent(t, 100*time.Second)
	a.TaskState.update([]TaskItem{{TaskID: "1", Summary: "implement", Status: TaskStatusProgressing}})
	a.reframeVerdict = taskverdict.TooBig

	if _, refused := a.sessionBudgetRefusal(); refused {
		t.Fatal("a run that already reframed must not be refused further commits")
	}
	if got := a.commitTaskItem(budgetCommitArgs("1")); strings.HasPrefix(got, "REFUSED") {
		t.Fatalf("post-reframe commit must be recorded, got %q", got)
	}
}

// TestReframeTimeDimension is the option-B pin at the reframe dispatch entry
// point: past the mark, verdict=DOABLE for an epic that is NOT already finished
// is refused and NOTHING is latched (the run may re-issue with the honest
// verdict); the same call below the mark, on a finished list, or on the
// unlimited budget is accepted.
func TestReframeTimeDimension(t *testing.T) {
	doableArgs := func() map[string]interface{} {
		return map[string]interface{}{
			"verdict":      taskverdict.Doable,
			"epic_summary": "wire the budget into the commit beat",
			"constraints":  "no new tools",
			"deliverables": "gate + tests",
			"acceptance":   "go test ./internal/agent/",
		}
	}
	tooBigArgs := func() map[string]interface{} {
		a := doableArgs()
		a["verdict"] = taskverdict.TooBig
		a["roadmap"] = []interface{}{
			map[string]interface{}{"goal": "gate", "acceptance": "unit test green"},
		}
		return a
	}
	openList := []TaskItem{{TaskID: "1", Summary: "implement", Status: TaskStatusProgressing}}
	closedList := []TaskItem{{TaskID: "1", Summary: "implement", Status: TaskStatusCompleted}}

	cases := []struct {
		name      string
		minutes   string
		ago       time.Duration
		clock     bool
		list      []TaskItem
		args      map[string]interface{}
		wantBlock bool
	}{
		{
			name: "past the mark, DOABLE, work unfinished — refused",
			ago:  50 * time.Minute, clock: true, list: openList,
			args: doableArgs(), wantBlock: true,
		},
		{
			name: "past the mark, DOABLE, epic already finished — allowed",
			ago:  50 * time.Minute, clock: true, list: closedList,
			args: doableArgs(),
		},
		{
			name: "below the mark, DOABLE — allowed",
			ago:  5 * time.Minute, clock: true, list: openList,
			args: doableArgs(),
		},
		{
			name:    "unlimited budget, DOABLE — allowed",
			minutes: "0", ago: 99 * time.Hour, clock: true, list: openList,
			args: doableArgs(),
		},
		{
			name:  "no clock, DOABLE — allowed",
			clock: false, list: openList,
			args: doableArgs(),
		},
		{
			name: "past the mark, TOO_BIG — allowed (it already carries work)",
			ago:  50 * time.Minute, clock: true, list: openList,
			args: tooBigArgs(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(sessionBudgetEnv, tc.minutes)
			a := budgetTestAgent(t, tc.ago)
			if !tc.clock {
				a.Clock = nil
			}
			if tc.list != nil {
				a.TaskState.update(tc.list)
			}

			blocked := a.reframeTimeRefusal(parseReframeInput(tc.args)) != ""
			if blocked != tc.wantBlock {
				t.Fatalf("blocked = %v, want %v (%q)", blocked, tc.wantBlock, a.reframeTimeRefusal(parseReframeInput(tc.args)))
			}
			if !tc.wantBlock {
				return
			}
			text := a.reframeTimeRefusal(parseReframeInput(tc.args))
			for _, want := range []string{
				taskverdict.Doable, "REFUSED", "elapsed", "remaining",
				taskverdict.TooBig, taskverdict.Impossible,
			} {
				if !strings.Contains(text, want) {
					t.Fatalf("time-dimension refusal must state %q, got %q", want, text)
				}
			}

			// No latch: the refusal must leave the run free to re-issue.
			got := a.handleTaskReframe(tc.args)
			if !strings.Contains(got, "REFUSED") {
				t.Fatalf("dispatch must surface the time-dimension refusal, got %q", got)
			}
			if a.reframeVerdict != "" || a.reframeApplied {
				t.Fatalf("a refused reframe must latch nothing: verdict=%q applied=%v", a.reframeVerdict, a.reframeApplied)
			}
		})
	}
}

// TestReframeTimeDimensionTeachesTheCeiling is the prompt-side pin: the time
// dimension must reach the model through the surfaces that carry it — the
// roadmap schema clause (a roadmap implying more than one budget is TOO_BIG by
// definition) and the reframe directive. The clause renders from the ONE budget
// source, so the env override must change the number the model reads.
func TestReframeTimeDimensionTeachesTheCeiling(t *testing.T) {
	t.Setenv(sessionBudgetEnv, "")
	clause := sessionBudgetCeilingClause()
	if !strings.Contains(clause, "60") {
		t.Fatalf("ceiling clause must state the resolved budget, got %q", clause)
	}
	t.Setenv(sessionBudgetEnv, "7")
	if clause = sessionBudgetCeilingClause(); !strings.Contains(clause, "7") {
		t.Fatalf("ceiling clause must follow the budget override (one source, Rule 2), got %q", clause)
	}
	t.Setenv(sessionBudgetEnv, "0")
	if clause = sessionBudgetCeilingClause(); !strings.Contains(clause, "unlimited") {
		t.Fatalf("unlimited budget must say so rather than invent a number, got %q", clause)
	}

	t.Setenv(sessionBudgetEnv, "")
	// The roadmap item schema is where the ceiling must be legible at the
	// moment the model WRITES an item (not only in the prose above it).
	if !roadmapItemHasProperty(t, "goal") {
		t.Fatal("roadmap item schema reader is broken — gate fixture")
	}
	desc := taskToolDescription(t) + " " + taskToolPropertyDescriptions(t)
	for _, want := range []string{"ONE session budget", "MORE THAN ONE session"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("task tool schema/description must teach the time dimension (want %q)", want)
		}
	}
	for _, d := range []string{reframeDirective, reframeNudgeMessage} {
		if !strings.Contains(d, "ONE session budget") && !strings.Contains(d, "one session") {
			t.Fatalf("reframe guidance must carry the time dimension, got %q", d)
		}
	}
}

// budgetClauseProbeEnv marks the RE-EXEC child of the gate below: set in the
// child's OWN environment by the parent, never by a running suite. Its value
// names the budget shape the parent handed over, so a child's output says which
// shape it measured.
const budgetClauseProbeEnv = "GOPI_TEST_BUDGET_CLAUSE_PROBE"

// budgetClauseProbeUnset is the shape spelling for "the parent removed
// GOPI_SESSION_BUDGET from the child's environment" — distinct from the value
// "0", which is a real budget spelling (0 = unlimited) that `gopi chat`
// publishes at arm time. One spelling cannot mean both.
const budgetClauseProbeUnset = "unset"

// budgetClauseShapes are the shapes the gate measures: the removed variable (the
// default 60-minute budget), the unlimited spelling, and an explicit ceiling.
var budgetClauseShapes = []string{budgetClauseProbeUnset, "0", "45"}

// TestReframeTimeDimensionIsReadableAtEveryAmbientBudget closes the hole the pin
// above cannot reach. claudeToolDefs is a PACKAGE-LEVEL var, so the task tool's
// description is rendered ONCE at package init from the environment the test
// process started with; t.Setenv runs far too late to change it, and the pin's
// schema read therefore always observes the AMBIENT shape.
//
// That is why the gate was stably red under the ambient GOPI_SESSION_BUDGET=0 a
// chat publishes at arm time (cmd/agent/chat_session_budget.go) and green under
// `env -u GOPI_SESSION_BUDGET`: two different rendered surfaces, one assertion.
// The criterion is pinned the only way an init-time surface can be pinned — by
// RE-EXECING the test binary with each budget shape in the child's environment
// and reading what THAT process rendered. Both phrases are required in every
// shape: the limited budget must keep teaching the ceiling, and the unlimited
// one must teach the same criterion instead of dropping it.
func TestReframeTimeDimensionIsReadableAtEveryAmbientBudget(t *testing.T) {
	if os.Getenv(budgetClauseProbeEnv) != "" {
		t.Skip("probe child: the parent drives this test")
	}
	for _, shape := range budgetClauseShapes {
		t.Run(shape, func(t *testing.T) {
			out := runBudgetClauseProbe(t, shape)
			if t.Failed() {
				return
			}
			if !strings.Contains(out, "probe-ok") {
				t.Fatalf("probe child (budget=%s) did not report a verdict — the arm did not run:\n%s", shape, out)
			}
			if strings.Contains(out, "no tests to run") {
				t.Fatalf("probe child (budget=%s) matched no test — the arm would be a vacuum:\n%s", shape, out)
			}
		})
	}
}

// runBudgetClauseProbe re-execs this test binary as the probe child with
// GOPI_SESSION_BUDGET removed and then re-set to shape, and returns the child's
// combined output. A child that could not start is reported, never silently read
// as a pass (Rule 15b).
func runBudgetClauseProbe(t *testing.T, shape string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestZZBudgetClauseProbe$", "-test.v")
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, sessionBudgetEnv+"=") {
			continue // the parent decides this shape, not the ambient environment
		}
		env = append(env, kv)
	}
	env = append(env, budgetClauseProbeEnv+"="+shape)
	if shape != budgetClauseProbeUnset {
		env = append(env, sessionBudgetEnv+"="+shape)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		t.Fatalf("probe child (budget=%s) could not run: %v", shape, err)
	}
	// Report the child's own lines: the gate's evidence is what THAT process
	// rendered at init, which no assertion of the parent can quote by itself.
	t.Logf("probe child (budget=%s) said:\n%s", shape, strings.TrimSpace(string(out)))
	return string(out)
}

// TestZZBudgetClauseProbe is the re-exec child body — the name starts with Z so
// it sorts last in a verbose run, and it is INERT unless a parent handed it a
// shape. It asserts the criterion against the surface THIS process rendered at
// init, i.e. the real one the model reads, and prints both the resolved budget
// and the clause so a report can quote what was taught.
func TestZZBudgetClauseProbe(t *testing.T) {
	shape := os.Getenv(budgetClauseProbeEnv)
	if shape == "" {
		t.Skip("not a probe child")
	}
	desc := taskToolDescription(t) + " " + taskToolPropertyDescriptions(t)
	t.Logf("budget shape=%s resolved_minutes=%d", shape, sessionBudgetMinutesResolved())
	t.Logf("time dimension clause: %s", sessionBudgetTimeDimension())
	for _, want := range []string{"ONE session budget", "MORE THAN ONE session"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("budget=%s: task tool schema/description must teach %q, got %q", shape, want, desc)
		}
	}
	t.Logf("probe-ok budget=%s carries both phrases", shape)
}
