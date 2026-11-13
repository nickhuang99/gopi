package agent

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// task_submit's SERIAL CHAIN — the LLM tool path (epic 01a12e34).
//
// THE DEFECT THESE TESTS PIN (measured 2026-10-12, task 01a12e06's triage run):
// three INDEPENDENT bug rows were submitted from one session and all three went
// out [queue-skip] — the head parked on the submitter's OWN in_progress row
// (autoTaskRowID), the next on the head, the third on the second — so no
// dispatcher could take any of them, and the only repair was a hand-run
// `gopi task update <id> --clear-parent` per row.
//
// The first test below is RED against that code (see the report's red-then-green
// evidence): with the old fallback the first submit's parent_task_id is the
// run's own row and its pending line carries [queue-skip].
// ---------------------------------------------------------------------------

// chainTestAgent builds an agent whose ledger is a FRESH per-test database
// (testutil.NewTestDBPath — Rule 32) and whose run row exists and is
// in_progress, so "the submitter's own row" is a REAL ledger row a fallback
// could actually park against.
//
// Every submit below carries a PAYLOAD (the tool path does not mirror the
// summary into it) — a payload-less pending row is not dispatchable work, so
// taskRowClaimable would mark it [queue-skip] for THAT reason and the test
// would be measuring the wrong gate (measured: the first version of this test
// passed no payload and read [queue-skip] with no reason suffix).
//
// It deliberately passes NO repo to the submits: the engine's cwd default
// (which resolves the durable repo this test process stands in) keeps the
// fixture free of a throwaway git work tree, and the assertions below read the
// LEDGER (parent_task_id + the pending view), never the deliverable stamp.
func chainTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	testutil.NewTestDBPath(t) // pins GOPI_MEMORY_DB (+ masks the legacy spelling)

	const ownRow = "t-own-run-row"
	if _, err := tools.TaskNew("the run's own row (the submitter)", ownRow, "sess-submitter", "test-model"); err != nil {
		t.Fatalf("seed the run's own row: %v", err)
	}
	if _, err := tools.TaskUpdate(ownRow, tools.TaskStatusInProgress); err != nil {
		t.Fatalf("the run's own row must be in_progress (that is what makes it a parking risk): %v", err)
	}
	ag := &Agent{autoTaskRowID: ownRow, autoTaskSessionID: "sess-submitter", Model: "test-model"}
	return ag, ownRow
}

// submitViaTool drives the REAL tool handler (the surface the model calls).
func submitViaTool(t *testing.T, ag *Agent, args map[string]interface{}) string {
	t.Helper()
	return ag.executeTool(deepseek.ToolCall{Name: "task_submit", Arguments: args})
}

// queueLineFor returns the pending-view line of one row, or "" when the row is
// not in the pending view at all.
func queueLineFor(t *testing.T, rowID string) string {
	t.Helper()
	out, err := tools.TaskList(tools.TaskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, rowID) {
			return line
		}
	}
	return ""
}

// TestTaskSubmitChain_FirstSubmitOfARunIsNotParkedBehindTheRunItself is the
// repair's primary regression: the run's FIRST auto-linked submit goes out
// PARENTLESS and claimable, and the SECOND one chains to the FIRST — never to
// the submitter's own in_progress row.
func TestTaskSubmitChain_FirstSubmitOfARunIsNotParkedBehindTheRunItself(t *testing.T) {
	ag, ownRow := chainTestAgent(t)

	first := submitViaTool(t, ag, map[string]interface{}{
		"summary": "bug one (independent of the other two)",
		"payload": "repro steps for bug one",
	})
	if strings.Contains(first, "task_submit error") {
		t.Fatalf("first submit failed: %s", first)
	}
	firstID := taskIDFromSubmitOut(t, first)
	if got := parentOf(t, firstID); got != "" {
		t.Errorf("first submit's parent_task_id = %q, want NO parent — a row parked behind its own submitter waits on work that is still running; the pre-fix fallback stamped the run's own row (%s)", got, ownRow)
	}
	if line := queueLineFor(t, firstID); strings.Contains(line, "[queue-skip]") {
		t.Errorf("first submit must be claimable the moment it lands; the pending view says:\n%s", line)
	}
	if strings.Contains(first, "chained") {
		t.Errorf("the first submit is NOT chained — the receipt must not claim a link that was not made:\n%s", first)
	}
	if !strings.Contains(first, "claimable IMMEDIATELY") {
		t.Errorf("the receipt must report the row as claimable (Rule 15 — a link the model cannot see is one it will not rely on):\n%s", first)
	}

	second := submitViaTool(t, ag, map[string]interface{}{
		"summary": "bug two (runs after bug one)",
		"payload": "repro steps for bug two",
	})
	secondID := taskIDFromSubmitOut(t, second)
	if got := parentOf(t, secondID); got != firstID {
		t.Errorf("second submit's parent_task_id = %q, want the FIRST submission %q (item i hangs off item i-1) — and never %q", got, firstID, ownRow)
	}
	if line := queueLineFor(t, secondID); !strings.Contains(line, "[queue-skip]") {
		t.Errorf("the second row IS chained and must be parked until the first releases; pending view:\n%s", line)
	}
	if !strings.Contains(second, "PARKED until") {
		t.Errorf("a chained submit's receipt must say it is parked:\n%s", second)
	}
	if !strings.Contains(second, firstID) {
		t.Errorf("a chained submit's receipt must NAME the parent it was linked to (here %s):\n%s", firstID, second)
	}
}

// TestTaskSubmitChain_IndependentSpellingLeavesTheRowParentless pins the
// opt-out: the spelling suppresses the chain for THAT submit, and the submits
// after it stay chained (the semantics the receipt states).
func TestTaskSubmitChain_IndependentSpellingLeavesTheRowParentless(t *testing.T) {
	ag, ownRow := chainTestAgent(t)

	// A first submit to establish a chain head, so "independent" is provably
	// DIFFERENT from "the chain had nothing to link to yet".
	head := submitViaTool(t, ag, map[string]interface{}{"summary": "chain head", "payload": "the head of the chain"})
	headID := taskIDFromSubmitOut(t, head)

	out := submitViaTool(t, ag, map[string]interface{}{
		"summary":        "an independent bug row",
		"payload":        "repro steps for the independent bug",
		"parent_task_id": taskSubmitIndependentSentinel,
	})
	if strings.Contains(out, "task_submit error") {
		t.Fatalf("independent submit failed: %s", out)
	}
	id := taskIDFromSubmitOut(t, out)
	if got := parentOf(t, id); got != "" {
		t.Errorf("parent_task_id=%q must yield a PARENTLESS row, got parent %q (the chain head was %q, the run's own row %q)", taskSubmitIndependentSentinel, got, headID, ownRow)
	}
	if line := queueLineFor(t, id); strings.Contains(line, "[queue-skip]") {
		t.Errorf("an independent row must be claimable immediately; pending view:\n%s", line)
	}
	if !strings.Contains(out, taskSubmitIndependentSentinel) || !strings.Contains(out, "NOT linked") {
		t.Errorf("the receipt must report that the sentinel suppressed the chain:\n%s", out)
	}

	// The spelling is per-submit: the following submit is chained again.
	after := submitViaTool(t, ag, map[string]interface{}{"summary": "chained again", "payload": "back on the chain"})
	if got := parentOf(t, taskIDFromSubmitOut(t, after)); got != id {
		t.Errorf("after an independent submit the chain continues from it: parent = %q, want %q", got, id)
	}
}

// TestTaskSubmitChain_ExplicitParentIsHonouredVerbatimAndStillGated pins the
// two arms the repair must NOT have touched: an explicit id is passed through
// exactly, and Rule 33's gate still refuses a parent that names no row.
func TestTaskSubmitChain_ExplicitParentIsHonouredVerbatimAndStillGated(t *testing.T) {
	ag, ownRow := chainTestAgent(t)

	out := submitViaTool(t, ag, map[string]interface{}{
		"summary":        "linked to the run's own row ON PURPOSE",
		"parent_task_id": ownRow,
	})
	id := taskIDFromSubmitOut(t, out)
	if got := parentOf(t, id); got != ownRow {
		t.Errorf("an explicit parent_task_id must be honoured verbatim: got %q, want %q", got, ownRow)
	}
	if !strings.Contains(queueLineFor(t, id), "[queue-skip]") {
		t.Errorf("a row explicitly parented to an in_progress row IS parked — Rule 33 is untouched; pending view:\n%s", queueLineFor(t, id))
	}

	dangling := submitViaTool(t, ag, map[string]interface{}{
		"summary":        "names a parent that does not exist",
		"parent_task_id": "t-no-such-row",
	})
	if !strings.Contains(dangling, "does not exist") {
		t.Errorf("a dangling parent must be refused at submit (Rule 33: an unresolvable dependency is never assumed satisfied), got:\n%s", dangling)
	}
}

// TestTaskSubmitChain_ReceiptsNameTheGatesOwnReleaseSet is the prose-vs-predicate
// check (Rule 14): every sentence the model reads renders the release set from
// the SAME reader the gate does, so the note cannot describe a set the gate
// does not enforce — the old wording said "completed/verified" while the gate
// also releases on superseded.
func TestTaskSubmitChain_ReceiptsNameTheGatesOwnReleaseSet(t *testing.T) {
	ag, _ := chainTestAgent(t)
	label := tools.TaskParentReleaseSetLabel()
	if label == "" {
		t.Fatal("the release-set label is empty — the shared reader is broken (the gate renders its reasons from it)")
	}

	submitViaTool(t, ag, map[string]interface{}{"summary": "chain head", "payload": "the head of the chain"})
	second := submitViaTool(t, ag, map[string]interface{}{"summary": "chained", "payload": "runs after the head"})
	if !strings.Contains(second, label) {
		t.Errorf("the chain receipt must name the release set %q the gate enforces, got:\n%s", label, second)
	}
	if strings.Contains(second, "completed/verified") {
		t.Errorf("the receipt still spells the release set by hand (it omits the other released statuses):\n%s", second)
	}

	// The TOOL DESCRIPTION is the other model-facing surface: it must carry the
	// same derived set and name the sentinel from its constant.
	def := taskSubmitToolDef(t)
	if !strings.Contains(def.Description, taskSubmitIndependentSentinel) {
		t.Errorf("the tool description must teach the independent spelling %q:\n%s", taskSubmitIndependentSentinel, def.Description)
	}
	schema, ok := def.InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("task_submit InputSchema is %T, want a map", def.InputSchema)
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("task_submit InputSchema has no properties map: %v", schema)
	}
	parentProp, ok := props["parent_task_id"].(map[string]interface{})
	if !ok {
		t.Fatalf("task_submit has no parent_task_id property: %v", props)
	}
	desc, _ := parentProp["description"].(string)
	if !strings.Contains(desc, label) {
		t.Errorf("parent_task_id's description must render the gate's release set %q, got:\n%s", label, desc)
	}
	if !strings.Contains(desc, taskSubmitIndependentSentinel) {
		t.Errorf("parent_task_id's description must name the independent spelling, got:\n%s", desc)
	}
}

// --- small bridges ---------------------------------------------------------

func parentOf(t *testing.T, taskID string) string {
	t.Helper()
	out, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow(%s): %v", taskID, err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "parent_task_id:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "parent_task_id:"))
			if v == "—" {
				return ""
			}
			return v
		}
	}
	t.Fatalf("no parent_task_id line in TaskShow(%s):\n%s", taskID, out)
	return ""
}

func taskSubmitToolDef(t *testing.T) deepseek.ClaudeToolDef {
	t.Helper()
	for _, d := range claudeToolDefs {
		if d.Name == "task_submit" {
			return d
		}
	}
	t.Fatal("task_submit tool def not found in claudeToolDefs")
	return deepseek.ClaudeToolDef{}
}
