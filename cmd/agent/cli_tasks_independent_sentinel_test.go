package main

// cli_tasks_independent_sentinel_test.go — the CLI face of the task_submit
// tool's INDEPENDENT sentinel.
//
// THE MEASURED DEFECT this file pins (task 01a1b448, filed from a real session
// on this host): the SAME parameter name carries two meanings on two faces, and
// the CLI face neither understood the value nor said it did not.
//
//	$ gopi task submit "probe" --parent-task-id independent
//	gopi task submit error: task_submit: parent_task_id "independent" does not exist
//
// `independent` is the task_submit TOOL's sentinel (now
// toolnames.TaskSubmitIndependentSentinel): "do not chain me to my previous
// submission". The CLI never chains, so the value had no chain to suppress
// there — but passing it to the engine verbatim made it look like a mistyped
// task id, and the refusal sent its reader hunting a typo that never existed.
//
// WHAT IS ASSERTED, and why each half is a different failure:
//
//   - the PARSER resolves the sentinel to a PARENTLESS row with the sentinel
//     recorded (so runTaskSubmit can say what it did), and refuses to mix it
//     with a real id — a row either waits for something or it does not;
//   - the FLAG keeps its ordinary meaning for every other value (a real id is
//     still honoured verbatim: the parser must not eat a parent whose id
//     happens to be typed);
//   - the BINARY, end to end, queues the row with an empty parent column and
//     exits 0 — the parser half alone would pass even if the dispatch arm
//     dropped the row;
//   - the USAGE PAGE names the spelling. The help is the promise; a value the
//     parser accepts and the page never mentions is the documentation half of
//     the same defect.
//
// RED BEFORE THE FIX: the e2e arm exits 1 with "does not exist", and the parser
// arms see the sentinel arrive as a parent id (and no `independentParent` field
// exists at all, so the file does not even compile against the pre-fix tree —
// which is why the pre-fix red is recorded by running the binary, not this
// file; see the report's Ran: lines).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// independentSentinelProbeID is a real uuid: `task submit` refuses an id that
// cannot be grounded, and a slug fixture would be refused for the wrong reason.
const independentSentinelProbeID = "6f0c9f9e-6d1a-4a1e-9f8e-01a1b4480001"

// TestSubmitParserResolvesTheIndependentSentinel: the sentinel is a WORD, not an
// id — it must reach `opts.independentParent` and leave the parent list EMPTY,
// so the engine is never asked to look up a row that cannot exist.
func TestSubmitParserResolvesTheIndependentSentinel(t *testing.T) {
	for _, spelling := range []string{
		toolnames.TaskSubmitIndependentSentinel,
		"Independent", // the tool's own resolver matches case-insensitively
	} {
		opts, err := parseTasksArgs(taskverbs.Submit, []string{
			"a row that depends on nothing",
			"--" + taskParentTaskIDFlagName, spelling,
		})
		if err != nil {
			t.Fatalf("submit parse with %q: %v", spelling, err)
		}
		if !opts.independentParent {
			t.Errorf("%q must set independentParent — otherwise the value reaches the engine as a task id and the submit dies on `does not exist`", spelling)
		}
		if len(opts.parentTaskIDs) != 0 || opts.parentTaskID != "" {
			t.Errorf("%q must leave NO parent (list=%v first=%q)", spelling, opts.parentTaskIDs, opts.parentTaskID)
		}
	}
}

// TestSubmitParserRefusesTheSentinelMixedWithARealParent: "no parent" and
// "these parents" are contradictory instructions about one column, and Rule 15
// says the pair is named rather than guessed at.
func TestSubmitParserRefusesTheSentinelMixedWithARealParent(t *testing.T) {
	_, err := parseTasksArgs(taskverbs.Submit, []string{
		"a row with a contradictory dependency",
		"--" + taskParentTaskIDFlagName, toolnames.TaskSubmitIndependentSentinel,
		"--" + taskParentTaskIDFlagName, mpParentA,
	})
	if err == nil {
		t.Fatalf("mixing %q with a real parent id must be REFUSED, not resolved silently", toolnames.TaskSubmitIndependentSentinel)
	}
	if !strings.Contains(err.Error(), toolnames.TaskSubmitIndependentSentinel) {
		t.Errorf("the refusal must quote the spelling that caused it, got %q", err.Error())
	}
}

// TestSubmitParserKeepsARealParentBesideTheSentinelArm: the ordinary arm is
// untouched — a real id is still honoured VERBATIM and does not set the
// sentinel flag (the parser must not widen its own reading of the value).
func TestSubmitParserKeepsARealParentBesideTheSentinelArm(t *testing.T) {
	opts, err := parseTasksArgs(taskverbs.Submit, []string{"a child", "--" + taskParentTaskIDFlagName, mpParentA})
	if err != nil {
		t.Fatalf("submit parse: %v", err)
	}
	if opts.independentParent {
		t.Error("a real parent id must NOT set independentParent")
	}
	if opts.parentTaskID != mpParentA || len(opts.parentTaskIDs) != 1 {
		t.Errorf("a real parent must survive verbatim, got first=%q list=%v", opts.parentTaskID, opts.parentTaskIDs)
	}
}

// TestSubmitIndependentSentinelQueuesAParentlessRow is the END-TO-END arm: the
// real binary, the real parser, the real engine, a real ledger. The parser arms
// above would pass even if the dispatch arm discarded the flag's meaning.
func TestSubmitIndependentSentinelQueuesAParentlessRow(t *testing.T) {
	dbPath := testutil.NewTestDBPath(t)
	env := testutil.MockEnv(t, t.TempDir(), "http://127.0.0.1:1") // no LLM traffic
	env = append(env, "GOPI_MEMORY_DB="+dbPath)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	stdout, stderr, code := runGopiMapped(t, []string{
		"task", "submit", "the sentinel must not be read as a task id",
		"--task-id", independentSentinelProbeID,
		"--parent-task-id", toolnames.TaskSubmitIndependentSentinel,
	}, "", env, "")
	if code != 0 {
		t.Fatalf("code=%d — the sentinel must be UNDERSTOOD on this face, not refused as a missing id\nstdout=%s\nstderr=%s",
			code, stdout, stderr)
	}
	if strings.Contains(stderr, "does not exist") {
		t.Errorf("the pre-fix sentence is back — the sentinel reached the engine as a task id:\n%s", stderr)
	}
	if !strings.Contains(stderr, toolnames.TaskSubmitIndependentSentinel) {
		t.Errorf("the submit must say what it did with the spelling (a silent accept is the other half of the defect):\n%s", stderr)
	}

	db, err := openRawTasksDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var parent, parents, payload string
	if err := db.QueryRow(`SELECT parent_task_id, parent_task_ids, payload FROM tasks WHERE task_id = ?`,
		independentSentinelProbeID).Scan(&parent, &parents, &payload); err != nil {
		t.Fatalf("read submitted row: %v", err)
	}
	if strings.TrimSpace(parent) != "" {
		t.Errorf("parent_task_id = %q, want EMPTY — that is what the sentinel MEANS", parent)
	}
	if strings.TrimSpace(parents) != "" {
		t.Errorf("parent_task_ids = %q, want EMPTY", parents)
	}
	if strings.TrimSpace(payload) == "" {
		t.Error("the row carries no payload — a payload-less row is never claimed, so it would prove nothing about a parentless row")
	}
}

// TestTaskUsagePageNamesTheIndependentSentinel is the DOCUMENTATION half of the
// same defect: the value was accepted by one face and named by neither page, so
// a caller who read `gopi task submit -h` could not learn that the spelling
// exists at all. The help is the promise the render must keep — a page that
// mentions neither the value nor the "no parent" outcome is the pre-fix state.
func TestTaskUsagePageNamesTheIndependentSentinel(t *testing.T) {
	if !strings.Contains(tasksUsage, `"`+toolnames.TaskSubmitIndependentSentinel+`"`) {
		t.Fatalf("`gopi task -h` must name the %q spelling in quotes — the page is where a CLI caller learns the value exists (toolnames.TaskSubmitIndependentSentinel is the ONE definition it must be rendered from)",
			toolnames.TaskSubmitIndependentSentinel)
	}
	if !strings.Contains(tasksUsage, "NO parent") {
		t.Error("the page must state the OUTCOME (a parentless row), not only the spelling — a reader who learns the word but not what it does is exactly where this defect started")
	}
}
