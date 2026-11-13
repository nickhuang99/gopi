package agent

// reframe_test.go — reframe+evaluate merge: ONE structured tool call
// (task action=reframe) replaces the free-text [REF RAMED EPIC] block and the
// retired task_evaluate tool. SPEC: briefings/SPEC-reframe-evaluate-merge.md.
//
// Covered: the directive wiring into the EPIC framing, the HARD validation
// gates (anti-bureaucracy: "too big" must cost a roadmap), the three-verdict
// dispatch (DOABLE continues / TOO_BIG auto-enqueues children + honorable exit
// / IMPOSSIBLE marks the ledger row failed), the briefing swap on a real Run,
// the nudge backstop, and the small-task zero-overhead path.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/taskverdict"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// reframeBigBriefing returns a raw briefing above reframeMinRawLength with a
// couple of realistic attachments/constraint lines (the shape an epic-shaped
// acceptance task carries).
func reframeBigBriefing() string {
	var b strings.Builder
	b.WriteString("[Task summary]\nShip the reframe flow\n")
	b.WriteString("[Briefing payload]\n")
	for len(b.String()) < reframeMinRawLength+800 {
		b.WriteString("Requirement: the reframe directive must force a task(action=reframe) call early; ")
		b.WriteString("the reframed goal replaces the raw briefing as the review anchor; attachments are read via file-read. ")
	}
	b.WriteString("\n[Attachments]\n[attachment1: /tmp/reframe/spec.md]\n")
	return b.String()
}

// taskSourceHumanTest is the ledger source value for test-seeded rows (the
// human/CLI origin — mirrors tools' validTaskSource vocabulary).
const taskSourceHumanTest = "human"

// reframeValidArgs builds a valid action=reframe payload for the given verdict.
func reframeValidArgs(verdict string) map[string]interface{} {
	args := map[string]interface{}{
		"action":       toolnames.ToolTaskReframe,
		"epic_summary": "Merge reframe and evaluate into one structured toolcall.",
		"constraints":  "16 tools, constants not literals, isolated worktree.",
		"deliverables": "Schema + validation + dispatch + prompt + telemetry.",
		"acceptance":   "Real-task E2E proves the LLM calls it; tests green.",
		"verdict":      verdict,
	}
	if verdict == taskverdict.TooBig {
		args["roadmap"] = []interface{}{
			map[string]interface{}{"goal": "Add schema + validation", "acceptance": "Tests green for the gates"},
			map[string]interface{}{"goal": "Wire dispatch + telemetry", "acceptance": "Children claimable in the ledger"},
		}
	}
	if verdict == taskverdict.Impossible {
		args["reason"] = "Provably unattainable in principle: the requested invariant contradicts the append-only ledger."
	}
	return args
}

// TestEpicFramingCarriesReframeDirective verifies the [EPIC GOAL] framing the
// model actually receives (request 1 of a Run) interpolates the reframe
// directive, the (formal) action name, and the three-verdict vocabulary.
func TestEpicFramingCarriesReframeDirective(t *testing.T) {
	var req1 *deepseek.ClaudeRequest
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var decoded deepseek.ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&decoded) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		if req1 == nil {
			req1 = &decoded
		}
		claudeTextStream(w, "On it.", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client:        newWfContractClient(server.URL),
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 2,
	}
	if _, err := ag.Run("Small task"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	epic := wfContractEpicMsg(t, req1)
	if !strings.Contains(epic, "[EPIC REFRAME]") {
		t.Fatalf("EPIC framing must carry the reframe directive, got: %q", epic)
	}
	if !strings.Contains(epic, "task(action="+toolnames.ToolTaskReframe+")") {
		t.Fatalf("directive must ask for the structured toolcall, got: %q", epic)
	}
	for _, v := range taskverdict.All() {
		if !strings.Contains(epic, v) {
			t.Fatalf("directive must name verdict %s (the whole vocabulary), got: %q", v, epic)
		}
	}
	if strings.Contains(epic, "NEEDS_RESEARCH") {
		t.Fatal("the retired NEEDS_RESEARCH verdict must appear nowhere (research is a roadmap item)")
	}
	if !strings.Contains(epic, "roadmap") || !strings.Contains(epic, "reason") {
		t.Fatalf("directive must name the conditional payloads, got: %q", epic)
	}
}

// TestNoReframedEpicBlockProtocol pins the retirement of the free-text flow:
// the void-slot marker and its parser are gone from the package (a re-import
// would be a regression to the protocol the merge replaced).
func TestNoReframedEpicBlockProtocol(t *testing.T) {
	if !strings.Contains(reframeDirective, toolnames.ToolTaskReframe) {
		t.Fatal("directive must route to the toolcall")
	}
	if strings.Contains(reframeDirective, "[REF RAMED EPIC]") {
		t.Fatal("the free-text [REF RAMED EPIC] block protocol is retired — the directive must not ask for prose")
	}
}

// TestValidateReframe_AllGates is the anti-bureaucracy core: the harness, not
// the prompt, enforces that every exit carries NEW work.
func TestValidateReframe_AllGates(t *testing.T) {
	// Valid payloads for all three verdicts.
	for _, v := range taskverdict.All() {
		if err := validateReframe(parseReframeInput(reframeValidArgs(v))); err != nil {
			t.Fatalf("valid verdict %s rejected: %v", v, err)
		}
	}
	cases := []struct {
		name    string
		mutate  func(map[string]interface{})
		wantSub string
	}{
		{"empty verdict", func(a map[string]interface{}) { a["verdict"] = "" }, "verdict"},
		{"whitespace verdict", func(a map[string]interface{}) { a["verdict"] = "   " }, "verdict"},
		{"unknown verdict", func(a map[string]interface{}) { a["verdict"] = "MAYBE" }, "invalid verdict"},
		{"retired NEEDS_RESEARCH", func(a map[string]interface{}) { a["verdict"] = "NEEDS_RESEARCH" }, "invalid verdict"},
		{"missing epic_summary", func(a map[string]interface{}) { delete(a, "epic_summary") }, "epic_summary"},
		{"missing constraints", func(a map[string]interface{}) { delete(a, "constraints") }, "constraints"},
		{"missing deliverables", func(a map[string]interface{}) { delete(a, "deliverables") }, "deliverables"},
		{"missing acceptance", func(a map[string]interface{}) { delete(a, "acceptance") }, "acceptance"},
	}
	for _, c := range cases {
		args := reframeValidArgs(taskverdict.Doable)
		c.mutate(args)
		err := validateReframe(parseReframeInput(args))
		if err == nil {
			t.Errorf("%s: expected rejection", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: error %q must name %q", c.name, err, c.wantSub)
		}
	}

	// TOO_BIG without a usable roadmap is REJECTED — the price of "too big".
	for _, bad := range []interface{}{
		nil,
		[]interface{}{},
		[]interface{}{map[string]interface{}{"goal": "do it"}},                     // no acceptance
		[]interface{}{map[string]interface{}{"acceptance": "green"}},               // no goal
		[]interface{}{map[string]interface{}{"goal": "  ", "acceptance": "green"}}, // blank goal
	} {
		args := reframeValidArgs(taskverdict.TooBig)
		if bad == nil {
			delete(args, "roadmap")
		} else {
			args["roadmap"] = bad
		}
		err := validateReframe(parseReframeInput(args))
		if err == nil {
			t.Errorf("TOO_BIG with roadmap %#v must be rejected", bad)
			continue
		}
		if !strings.Contains(err.Error(), "roadmap") {
			t.Errorf("TOO_BIG rejection must name the roadmap, got %q", err)
		}
	}

	// IMPOSSIBLE without a reason is REJECTED (the honest exit must explain).
	args := reframeValidArgs(taskverdict.Impossible)
	delete(args, "reason")
	if err := validateReframe(parseReframeInput(args)); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Errorf("IMPOSSIBLE without reason must be rejected naming reason, got %v", err)
	}
	// DOABLE needs no extra payload (and a stray roadmap is harmless).
	args = reframeValidArgs(taskverdict.Doable)
	args["roadmap"] = []interface{}{map[string]interface{}{"goal": "x", "acceptance": "y"}}
	if err := validateReframe(parseReframeInput(args)); err != nil {
		t.Errorf("DOABLE with a harmless extra roadmap rejected: %v", err)
	}
}

// TestValidateToolArgsReframeGates proves the pre-dispatch gate applies the
// same rules (an incomplete call never reaches dispatch).
func TestValidateToolArgsReframeGates(t *testing.T) {
	args := reframeValidArgs(taskverdict.TooBig)
	args["action"] = toolnames.ToolTaskReframe
	if msg := validateToolArgs(toolnames.ToolTask, args); msg != "" {
		t.Fatalf("valid reframe rejected by validateToolArgs: %s", msg)
	}
	delete(args, "roadmap")
	msg := validateToolArgs(toolnames.ToolTask, args)
	if !strings.Contains(msg, "roadmap") {
		t.Fatalf("TOO_BIG without roadmap must be rejected pre-dispatch, got %q", msg)
	}
}

// TestParseReframeInput_NormalizesVerdict: the verdict is upper-cased/trimmed
// so a lower-case wire value still validates; shape tolerance never becomes
// content tolerance (a malformed roadmap parses empty and is then rejected).
func TestParseReframeInput_NormalizesVerdict(t *testing.T) {
	args := reframeValidArgs(taskverdict.Doable)
	args["verdict"] = "  doable "
	in := parseReframeInput(args)
	if in.Verdict != taskverdict.Doable {
		t.Fatalf("verdict normalization: got %q", in.Verdict)
	}
	args = reframeValidArgs(taskverdict.TooBig)
	args["roadmap"] = "not an array" // shape-tolerance: parses empty…
	if err := validateReframe(parseReframeInput(args)); err == nil {
		t.Fatal("…and is then REJECTED — shape tolerance must not accept an empty roadmap")
	}
}

// TestHandleTaskReframeDoableContinues: DOABLE tells the model to proceed and
// records telemetry (no ledger row needed).
func TestHandleTaskReframeDoableContinues(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})

	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeValidArgs(taskverdict.Doable)})
	if !strings.Contains(out, "VERDICT "+taskverdict.Doable) || !strings.Contains(out, "proceed") {
		t.Fatalf("DOABLE must instruct the model to proceed, got: %s", out)
	}
	if ag.reframeVerdict != taskverdict.Doable {
		t.Fatalf("verdict must be latched, got %q", ag.reframeVerdict)
	}
	if ag.reframeHonestExit {
		t.Fatal("DOABLE must not latch the honest exit")
	}
}

// TestHandleTaskReframeTooBigEnqueuesChildren: the roadmap becomes REAL child
// tasks (claimable pending rows, parent-linked, tagged) and the run exits
// honorably — the anti-bureaucracy mechanism (the exit costs a plan).
func TestHandleTaskReframeTooBigEnqueuesChildren(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	// A real parent row: the child linkage must reference an EXISTING task.
	parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{Summary: "parent epic", Source: taskSourceHumanTest})
	if err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	parentID := taskIDFromSubmitOut(t, parentOut)

	ag := &Agent{Ctx: &Context{}, autoTaskRowID: parentID, Model: "deepseek-v4-flash"}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})

	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeValidArgs(taskverdict.TooBig)})
	if !strings.Contains(out, "VERDICT "+taskverdict.TooBig) {
		t.Fatalf("TOO_BIG verdict missing from output: %s", out)
	}
	if !strings.Contains(out, "2 child task(s)") {
		t.Fatalf("both roadmap items must be enqueued, got: %s", out)
	}
	// The roadmap must be a SERIAL CHAIN (epic 01a10a24, operator ruling
	// 2026-10-05: "LLM 提交任务的顺序 = 逻辑依赖顺序"): item 1 hangs off the epic
	// row, item i off item i-1. So the epic row has exactly ONE child — the
	// chain head — and the rest are reached one link at a time. The previous
	// shape (every item pointing at the epic row) made them all claimable the
	// instant the epic row reached terminal status: the dependency the model
	// stated, thrown away.
	first, err := tools.TaskChildren(parentID)
	if err != nil {
		t.Fatalf("TaskChildren: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("the epic row must have exactly ONE child (the chain head), got %d", len(first))
	}
	chain := []*tools.Task{first[0]}
	for {
		tail := chain[len(chain)-1]
		kids, err := tools.TaskChildren(tail.TaskID)
		if err != nil {
			t.Fatalf("TaskChildren(%s): %v", tail.TaskID, err)
		}
		if len(kids) == 0 {
			break
		}
		chain = append(chain, kids[0])
	}
	if len(chain) != 2 {
		t.Fatalf("expected 2 rows in the chain (one per roadmap item), got %d", len(chain))
	}
	for i, c := range chain {
		wantParent := parentID
		if i > 0 {
			wantParent = chain[i-1].TaskID
		}
		if c.ParentTaskID != wantParent {
			t.Errorf("chain[%d] %s parent = %q, want %q — item i's parent is item i-1", i, c.TaskID, c.ParentTaskID, wantParent)
		}
		if c.Status != tools.TaskStatusPending {
			t.Errorf("child %s status = %q, want pending (claimable)", c.TaskID, c.Status)
		}
		tags, err := tools.TaskTags(c.TaskID)
		if err != nil {
			t.Fatalf("TaskTags(%s): %v", c.TaskID, err)
		}
		if !strings.Contains(tags, tools.ReframeRoadmapTag) {
			t.Errorf("child %s tags = %q, want the %s origin tag", c.TaskID, tags, tools.ReframeRoadmapTag)
		}
		if strings.TrimSpace(c.Payload) == "" {
			t.Errorf("child %s carries no payload — each roadmap item must stand alone as a briefing", c.TaskID)
		}
	}
	// Honorable exit: the run must NOT start the children itself.
	if !strings.Contains(out, "CLOSED here") {
		t.Errorf("TOO_BIG must close this run honorably, got: %s", out)
	}
}

// TestHandleTaskReframeImpossibleMarksFailed: the honest exit lands in the
// LEDGER (row failed + reason), and the end hook must not re-close it.
func TestHandleTaskReframeImpossibleMarksFailed(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{Summary: "epic to judge", Source: taskSourceHumanTest})
	if err != nil {
		t.Fatalf("seed row: %v", err)
	}
	rowID := taskIDFromSubmitOut(t, parentOut)
	if _, err := tools.TaskUpdate(rowID, tools.TaskStatusInProgress); err != nil {
		t.Fatalf("mark in_progress: %v", err)
	}

	ag := &Agent{Ctx: &Context{}, autoTaskRowID: rowID}
	args := reframeValidArgs(taskverdict.Impossible)
	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: args})
	if !strings.Contains(out, "VERDICT "+taskverdict.Impossible) {
		t.Fatalf("IMPOSSIBLE verdict missing from output: %s", out)
	}
	if !ag.reframeHonestExit {
		t.Fatal("IMPOSSIBLE must latch the honest exit so the end hook keeps the failed status")
	}
	row, err := tools.TaskShow(rowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(row, tools.TaskStatusFailed) {
		t.Fatalf("the row must be marked failed, got:\n%s", row)
	}
	if !strings.Contains(row, args["reason"].(string)[:40]) {
		t.Fatalf("the reason must be recorded on the row, got:\n%s", row)
	}
}

// TestHandleTaskReframeInvalidHasNoSideEffects: a rejected call must not swap
// the briefing, latch a verdict, or enqueue anything.
func TestHandleTaskReframeInvalidHasNoSideEffects(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})

	args := reframeValidArgs(taskverdict.TooBig)
	delete(args, "roadmap")
	before, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList (before): %v", err)
	}
	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: args})
	if !strings.Contains(out, "roadmap") {
		t.Fatalf("expected the roadmap rejection, got: %s", out)
	}
	if ag.reframeVerdict != "" || ag.reframeApplied {
		t.Fatal("a rejected reframe must leave no state behind")
	}
	after, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList (after): %v", err)
	}
	if after != before {
		t.Fatalf("a rejected reframe must enqueue nothing:\nbefore: %s\nafter: %s", before, after)
	}
	if strings.Contains(after, tools.ReframeRoadmapTag) {
		t.Fatalf("a rejected reframe must not leave a roadmap child, got:\n%s", after)
	}
}

// TestHandleTaskReframeRepeatRecordedNotRedispatched: a second call is
// telemetry (the loop symptom) but never re-dispatched — the FIRST verdict
// stands (duplicate children would be a real defect).
func TestHandleTaskReframeRepeatRecordedNotRedispatched(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{Summary: "parent", Source: taskSourceHumanTest})
	if err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	parentID := taskIDFromSubmitOut(t, parentOut)

	ag := &Agent{Ctx: &Context{}, autoTaskRowID: parentID}
	first := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeValidArgs(taskverdict.TooBig)})
	if !strings.Contains(first, "2 child task(s)") {
		t.Fatalf("first TOO_BIG must enqueue, got: %s", first)
	}
	second := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeValidArgs(taskverdict.TooBig)})
	if !strings.Contains(second, "already reframed this run") {
		t.Fatalf("a repeat must be reported as recorded, not re-dispatched, got: %s", second)
	}
	// A repeat must not duplicate the chain either: the epic row still has
	// exactly one child (the chain head), and walking the chain still yields
	// one row per roadmap item.
	chainHead, err := tools.TaskChildren(parentID)
	if err != nil {
		t.Fatalf("TaskChildren: %v", err)
	}
	if len(chainHead) != 1 {
		t.Fatalf("a repeat must NOT duplicate children: epic row has %d children, want 1 (the chain head)", len(chainHead))
	}
	chainLen := 1
	for tail := chainHead[0]; ; {
		kids, err := tools.TaskChildren(tail.TaskID)
		if err != nil {
			t.Fatalf("TaskChildren(%s): %v", tail.TaskID, err)
		}
		if len(kids) == 0 {
			break
		}
		chainLen++
		tail = kids[0]
	}
	if chainLen != 2 {
		t.Fatalf("a repeat must NOT duplicate children: chain has %d rows, want 2", chainLen)
	}
}

// TestReframeSwap_BigBriefing runs a real Run whose first response calls
// task(action=reframe, DOABLE) plus a bash tool call (forcing a tool round, so
// the swap happens between round 1 and round 2's request). The second request
// must carry the COMPACT [EPIC GOAL] message.
func TestReframeSwap_BigBriefing(t *testing.T) {
	var reqs []*deepseek.ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded deepseek.ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&decoded) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		reqs = append(reqs, &decoded)
		switch len(reqs) {
		case 1:
			claudeTextPlusToolStream(w,
				"[TASK_LIST_BEGIN]\n[{\"task_id\":\"1\",\"summary\":\"build\",\"status\":\"progressing\"}]\n[TASK_LIST_END]\nReframing now.",
				toolnames.ToolTask, reframeValidArgs(taskverdict.Doable))
		case 2:
			claudeTextPlusToolStream(w, "Checking the tree.",
				"bash", map[string]interface{}{"command": "pwd"})
		default:
			claudeTextStream(w, "[TASK_LIST_BEGIN]\n[{\"task_id\":\"1\",\"summary\":\"build\",\"status\":\"completed\"}]\n[TASK_LIST_END]\nDone, verified.", "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client:        newWfContractClient(server.URL),
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}
	if _, err := ag.Run(reframeBigBriefing()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(reqs) < 3 {
		t.Fatalf("expected >=3 requests, got %d", len(reqs))
	}
	epic1 := wfContractEpicMsg(t, reqs[0])
	if !strings.Contains(epic1, "Requirement: the reframe directive") {
		t.Fatal("request 1 must carry the raw briefing")
	}
	if !ag.reframeApplied {
		t.Fatal("the swap must be applied after a valid reframe call")
	}
	epic2 := wfContractEpicMsg(t, reqs[1])
	if strings.Contains(epic2, "Requirement: the reframe directive") {
		t.Fatal("request 2 must NOT carry the raw giant briefing — reframe must replace it")
	}
	if !strings.Contains(epic2, "Merge reframe and evaluate") || !strings.Contains(epic2, "archived in the session log") {
		t.Fatalf("request 2 must carry the reframed goal + archive note, got: %.200q", epic2)
	}
	if !strings.Contains(epic2, "Verdict: "+taskverdict.Doable) {
		t.Fatalf("the reframed goal must carry the verdict, got: %.200q", epic2)
	}
	if len(epic2) > len(epic1)/4 {
		t.Fatalf("request 2 epic (%d chars) must be dramatically smaller than request 1's (%d chars)", len(epic2), len(epic1))
	}
}

// TestReframeSwap_SmallTask verifies zero reframe overhead: a small briefing is
// never swapped (the harness refuses below reframeMinRawLength), so a mistaken
// reframe cannot destroy a compact-but-complete raw goal.
func TestReframeSwap_SmallTask(t *testing.T) {
	var calls int32
	var reqs []*deepseek.ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded deepseek.ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&decoded) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		reqs = append(reqs, &decoded)
		if atomic.AddInt32(&calls, 1) == 1 {
			claudeTextPlusToolStream(w,
				"[TASK_LIST_BEGIN]\n[{\"task_id\":\"1\",\"summary\":\"do it\",\"status\":\"progressing\"}]\n[TASK_LIST_END]\nworking",
				toolnames.ToolTask, reframeValidArgs(taskverdict.Doable))
			return
		}
		claudeTextStream(w, "done", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client:        newWfContractClient(server.URL),
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 3,
	}
	small := "Do the small thing." // below reframeMinRawLength
	if _, err := ag.Run(small); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if ag.reframeApplied {
		t.Fatal("small task must NOT have its briefing swapped (zero-overhead path)")
	}
	if len(reqs) == 0 {
		t.Fatal("expected at least one request")
	}
	if epic := wfContractEpicMsg(t, reqs[0]); !strings.Contains(epic, "Do the small thing.") {
		t.Fatalf("raw small briefing must stay in context, got: %q", epic)
	}
}

// TestNudgeReframe_BigBriefingNoCall — the backstop: a big-briefing run that
// never calls task(action=reframe) gets ONE harness nudge naming the toolcall.
func TestNudgeReframe_BigBriefingNoCall(t *testing.T) {
	var reqs []*deepseek.ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded deepseek.ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&decoded) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		reqs = append(reqs, &decoded)
		switch len(reqs) {
		case 1:
			claudeTextPlusToolStream(w,
				"[TASK_LIST_BEGIN]\n[{\"task_id\":\"1\",\"summary\":\"investigate\",\"status\":\"progressing\"}]\n[TASK_LIST_END]\nExploring the repo.",
				"bash", map[string]interface{}{"command": "pwd"})
		case 2:
			claudeTextPlusToolStream(w, "continuing", "bash", map[string]interface{}{"command": "true"})
		default:
			claudeTextStream(w, "done", "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client:        newWfContractClient(server.URL),
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 4,
	}
	if _, err := ag.Run(reframeBigBriefing()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if ag.reframeApplied || ag.reframeVerdict != "" {
		t.Fatal("no reframe call → no swap, no verdict")
	}
	if !ag.reframeNudged {
		t.Fatal("a big briefing with no reframe call must be nudged exactly once")
	}
	if len(reqs) < 2 {
		t.Fatalf("expected >=2 requests, got %d", len(reqs))
	}
	req2Text := ""
	for _, m := range reqs[1].Messages {
		if m.Role != "user" {
			continue
		}
		for _, b := range m.Content {
			req2Text += b.Text
		}
	}
	if !strings.Contains(req2Text, "task(action="+toolnames.ToolTaskReframe+")") {
		t.Fatalf("request 2 must carry the nudge naming the toolcall, got: %.300q", req2Text)
	}
	if !strings.Contains(req2Text, taskverdict.Impossible) {
		t.Fatalf("the nudge must carry the verdict vocabulary, got: %.300q", req2Text)
	}
}

// TestNudgeReframe_SmallTaskNoNudge — the backstop must NOT fire for a small
// briefing: zero overhead regression stays true.
func TestNudgeReframe_SmallTaskNoNudge(t *testing.T) {
	ag := &Agent{Ctx: &Context{}, Logger: log.New(io.Discard, "", 0)}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\nDo the small thing."})
	if ag.maybeNudgeReframe(9, 100000) {
		t.Fatal("small task must never be reframe-nudged")
	}
}

// TestNudgeReframe_ReframedNoNudge — a run that already reframed is never nudged.
func TestNudgeReframe_ReframedNoNudge(t *testing.T) {
	ag := &Agent{Ctx: &Context{}, Logger: log.New(io.Discard, "", 0)}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
	ag.reframeVerdict = taskverdict.Doable
	if ag.maybeNudgeReframe(9, 100000) {
		t.Fatal("a reframed run must never be nudged")
	}
	ag.reframeVerdict = ""
	ag.reframeApplied = true
	if ag.maybeNudgeReframe(9, 100000) {
		t.Fatal("a swapped run must never be nudged")
	}
}

// TestNudgeReframe_ResumeNoDuplicate — after a resume (the injected nudge
// persists in Ctx but the latch resets), maybeNudgeReframe must NOT re-inject.
func TestNudgeReframe_ResumeNoDuplicate(t *testing.T) {
	ag := &Agent{Ctx: &Context{}, Logger: log.New(io.Discard, "", 0)}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: reframeNudgeMessage})
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "round-1 assistant text (no call)"})

	if ag.maybeNudgeReframe(2, 0) {
		t.Fatal("resume must not re-inject a nudge that is already in context")
	}
	if !ag.reframeNudged {
		t.Fatal("the existing nudge must latch the flag")
	}
}

// TestReframe_MessagesSwapKeepsPrefixOrder verifies the swap does not disturb
// the message order/role contract when the epic is NOT at index 0 (e.g. after
// compaction prepends a memory block): the swap scans by prefix, not index.
func TestReframe_MessagesSwapKeepsPrefixOrder(t *testing.T) {
	ag := &Agent{Ctx: &Context{}, Logger: log.New(io.Discard, "", 0)}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "compacted memory summary"})
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
	ag.applyReframe(parseReframeInput(reframeValidArgs(taskverdict.Doable)))
	if !ag.reframeApplied {
		t.Fatal("swap must apply when the epic message is found by prefix")
	}
	if !strings.HasPrefix(ag.Ctx.Messages[0].Content, "compacted memory summary") {
		t.Fatal("index 0 (memory block) must be untouched")
	}
	epic := ag.Ctx.Messages[1].Content
	if !strings.HasPrefix(epic, epicGoalHeader) || !strings.Contains(epic, "Merge reframe and evaluate") {
		t.Fatalf("epic message must be replaced with the reframed body, got: %q", epic)
	}
	if strings.Contains(epic, "Requirement: the reframe directive") {
		t.Fatal("raw briefing must be gone from the epic message")
	}
}

// TestReframe_ReviewGoalsAnchor checks that task(action=review) runs against a
// context whose index 0 is the (reframed) epic goal and that the goal stays
// the permanent review anchor.
func TestReframe_ReviewGoalsAnchor(t *testing.T) {
	ag := &Agent{Ctx: &Context{}, TaskState: newTodoState()}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: reframedEpicMessage("Reframed: compact goal.")})
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "step", "progressing"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	ag.TaskState.update(tasks)
	got := ag.handleTaskReview(map[string]interface{}{"action": toolnames.ToolTaskReview, "verdict": "A"})
	if ag.lastTaskReviewVerdict != "A" {
		t.Fatalf("review verdict A must be latched, got %q", ag.lastTaskReviewVerdict)
	}
	if !strings.Contains(got, "VERDICT A") {
		t.Fatalf("review must return the on-track verdict action, got %q", got)
	}
	if !strings.Contains(ag.Ctx.Messages[0].Content, "compact goal") {
		t.Fatalf("index 0 must keep the reframed epic goal as the review anchor, got %q", ag.Ctx.Messages[0].Content)
	}
}

// reframeArgsWithRoadmap builds a valid TOO_BIG payload whose roadmap is the
// given goals (each with a stand-alone acceptance line) — the parameterised
// form of reframeValidArgs, for the chain shapes (lone item / N items).
func reframeArgsWithRoadmap(goals ...string) map[string]interface{} {
	args := reframeValidArgs(taskverdict.TooBig)
	items := make([]interface{}, 0, len(goals))
	for _, g := range goals {
		items = append(items, map[string]interface{}{"goal": g, "acceptance": "verified on the sandbox ledger"})
	}
	args["roadmap"] = items
	return args
}

// taskChain walks the serial chain that hangs off head: each step asks "which
// row names this one as its parent", so chain[0] is the row dispatched FIRST,
// chain[1] the one after it … — the LEDGER's own answer to "what order will
// this run in", with no prose involved (epic 01a10a24). A row with more than
// one child is a failure, not a shape: a chain has one link out.
func taskChain(t *testing.T, head string) []*tools.Task {
	t.Helper()
	kids, err := tools.TaskChildren(head)
	if err != nil {
		t.Fatalf("TaskChildren(%s): %v", head, err)
	}
	if len(kids) == 0 {
		return nil
	}
	if len(kids) > 1 {
		t.Fatalf("row %s has %d children, want at most 1 — a chain has one link out", head, len(kids))
	}
	return append(kids, taskChain(t, kids[0].TaskID)...)
}

// taskPendingLinesFor returns the ONE pending-view line the ledger renders for
// each given row — the same text an operator reads, so an assertion about
// [next] / [queue-skip] reads markers, not internals.
func taskPendingLinesFor(t *testing.T, rows []*tools.Task) []string {
	t.Helper()
	list, err := tools.TaskList(tools.TaskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		found := ""
		for _, line := range strings.Split(list, "\n") {
			if strings.Contains(line, "#"+r.TaskID) {
				found = line
				break
			}
		}
		if found == "" {
			t.Fatalf("row %s is missing from the pending view:\n%s", r.TaskID, list)
		}
		out = append(out, found)
	}
	return out
}

// TestReframeRoadmapChainIsSerial: the operator's ruling made measurable —
// "LLM 提交任务的顺序 = 逻辑依赖顺序" (2026-10-05, epic 01a10a24). A 3-item
// roadmap must land as ONE chain (item i's parent = item i-1, item 1's parent =
// the epic row), and once the epic row reaches a RELEASING status exactly one
// row must be in a dispatcher's reach: the other two carry the parent gate's
// own [queue-skip] marker naming their predecessor. The pre-chain shape — every
// item pointing at the epic row — is the defect this pins: all three became
// claimable at the same instant, which is the dependency the model stated,
// discarded.
func TestReframeRoadmapChainIsSerial(t *testing.T) {
	testutil.NewTestDBPath(t)
	parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{Summary: "epic to split", Source: taskSourceHumanTest})
	if err != nil {
		t.Fatalf("seed epic row: %v", err)
	}
	rowID := taskIDFromSubmitOut(t, parentOut)

	ag := &Agent{Ctx: &Context{}, autoTaskRowID: rowID}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeArgsWithRoadmap("item one", "item two", "item three")})
	if !strings.Contains(out, "SERIAL CHAIN") {
		t.Fatalf("the result must TELL the model the roadmap became a chain, got: %s", out)
	}

	chain := taskChain(t, rowID)
	if len(chain) != 3 {
		t.Fatalf("expected 3 rows in the chain, got %d", len(chain))
	}
	wantParent := rowID
	for i, c := range chain {
		if c.ParentTaskID != wantParent {
			t.Errorf("chain[%d] %s parent = %q, want %q (item i hangs off item i-1)", i, c.TaskID, c.ParentTaskID, wantParent)
		}
		if !strings.Contains(c.Summary, reframeChainItemLabel(i+1, 3)) {
			t.Errorf("chain[%d] summary = %q, want the %s label — the ledger's own order must be readable",
				i, c.Summary, reframeChainItemLabel(i+1, 3))
		}
		wantParent = c.TaskID
	}

	// The dependency must hold at the DISPATCH seam, not only in the column:
	// release the epic row the way a finishing run does (the ledger's own
	// transition ladder — pending → claimed → in_progress → completed) and ask
	// what a dispatcher sees then.
	for _, st := range []string{tools.TaskStatusClaimed, tools.TaskStatusInProgress, tools.TaskStatusCompleted} {
		if _, err := tools.TaskUpdate(rowID, st); err != nil {
			t.Fatalf("release the epic row (%s): %v", st, err)
		}
	}
	lines := taskPendingLinesFor(t, chain)
	open, parked := 0, 0
	for i, line := range lines {
		if strings.Contains(line, tools.TaskQueueSkipMarker) {
			parked++
			// The marker names WHY — the gate's own text, naming the
			// predecessor, so the chain is readable off the view itself.
			if !strings.Contains(line, chain[i-1].TaskID) || !strings.Contains(line, "not released") {
				t.Errorf("parked row %s must name its predecessor %s and the gate's reason, got:\n%s",
					chain[i].TaskID, chain[i-1].TaskID, line)
			}
			continue
		}
		open++
	}
	if open != 1 || parked != 2 {
		t.Fatalf("exactly ONE row must be claimable (the chain head) and the other two parked, got open=%d parked=%d:\n%s",
			open, parked, strings.Join(lines, "\n"))
	}
}

// TestReframeRoadmapSingleItemStaysDirect: the lone item is the boundary where
// a chain has nothing to chain to — the single child hangs off the epic row
// exactly as it did before, and no position label decorates it (there is no
// order to state).
func TestReframeRoadmapSingleItemStaysDirect(t *testing.T) {
	testutil.NewTestDBPath(t)
	parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{Summary: "epic with one step", Source: taskSourceHumanTest})
	if err != nil {
		t.Fatalf("seed epic row: %v", err)
	}
	rowID := taskIDFromSubmitOut(t, parentOut)

	ag := &Agent{Ctx: &Context{}, autoTaskRowID: rowID}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeArgsWithRoadmap("the only step")})
	if !strings.Contains(out, "1 child task(s)") {
		t.Fatalf("a one-item roadmap still enqueues exactly one child, got: %s", out)
	}
	chain := taskChain(t, rowID)
	if len(chain) != 1 {
		t.Fatalf("expected exactly 1 child row, got %d", len(chain))
	}
	if chain[0].ParentTaskID != rowID {
		t.Fatalf("the lone item's parent = %q, want the epic row %q", chain[0].ParentTaskID, rowID)
	}
	if chain[0].Summary != "the only step" {
		t.Errorf("a lone item carries no [i/N] label, got summary %q", chain[0].Summary)
	}
}

// TestReframeRoadmapFailureKeepsChainLinkable: the injected failure — the seam
// exists for exactly this, since no real state produces a mid-roadmap submit
// error — must cost ONE item, not the chain. Item 2 fails; items 1 and 3 still
// land, and item 3 hangs off item 1 (the last row that really exists) rather
// than dangling off a row that was never created.
func TestReframeRoadmapFailureKeepsChainLinkable(t *testing.T) {
	testutil.NewTestDBPath(t)
	parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{Summary: "epic with a bad item", Source: taskSourceHumanTest})
	if err != nil {
		t.Fatalf("seed epic row: %v", err)
	}
	rowID := taskIDFromSubmitOut(t, parentOut)

	real := reframeSubmitFn
	t.Cleanup(func() { reframeSubmitFn = real })
	calls := 0
	reframeSubmitFn = func(opts tools.TaskSubmitOptions) (string, string, error) {
		calls++
		if calls == 2 {
			return "", "", fmt.Errorf("injected submit failure (the seam's only purpose)")
		}
		return real(opts)
	}

	ag := &Agent{Ctx: &Context{}, autoTaskRowID: rowID}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeArgsWithRoadmap("item one", "item two", "item three")})
	if !strings.Contains(out, "submit FAILED") {
		t.Fatalf("the failed item must be REPORTED, never silently dropped, got: %s", out)
	}

	chain := taskChain(t, rowID)
	if len(chain) != 2 {
		t.Fatalf("the failure costs exactly one item: expected 2 rows, got %d", len(chain))
	}
	if chain[0].ParentTaskID != rowID || chain[1].ParentTaskID != chain[0].TaskID {
		t.Fatalf("item 3 must link to item 1 (the last row that landed), got parents %q / %q",
			chain[0].ParentTaskID, chain[1].ParentTaskID)
	}
	if !strings.Contains(chain[1].Summary, reframeChainItemLabel(3, 3)) {
		t.Errorf("the surviving later item keeps its roadmap position, got summary %q", chain[1].Summary)
	}
}
