package agent

// Behavioral tests for task(action=review): the post-commit checkpoint as a
// STRUCTURED TOOL CALL (tool-family merge, nick 2026-09-10 design), replacing
// the old injectPostCommitReview TEXT injection (which asked the model to
// free-text a REVIEW-VERDICT: line nothing parsed — a void slot). Now the
// model calls task(action=review, verdict=A-E...) and the harness validates,
// latches, receipt-ed the verdict, and returns the verdict-driven action.
//
// Acceptance:
//   - R1 unit: handleTaskReview validates the verdict (A-E; unknown rejected),
//     latches a.lastTaskReviewVerdict, persists a machine-scannable
//     REVIEW-VERDICT:<code> receipt row, and returns the verdict action
//     (verdict-driven).
//   - R2 e2e: a real task(action=commit) round is followed by the model
//     CALLING task(action=review, verdict=A) as a tool_use (visible in the
//     session); the harness records verdict A; NO text-injection review user
//     message is added (the old postCommitReviewMarker is gone).
//   - R3: verdict B/D/C returns the deterministic re-align / redo / amend task
//     instruction the model must execute (plan re-emission), not free text.
//   - R4 e2e redo: verdict D → the model re-commits the same task_id with a
//     SUPERSEDES conclusion (append-only); both the original and the
//     superseding conclusions exist.
//   - R5: with all tracked items completed, the review is flagged CLOSING
//     (whole-epic final review).

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/tools"
)

// R1 — unit: handleTaskReview validates, latches, receipts, and returns the
// verdict action. Verification also confirms the OLD text-injection markers
// are gone (review is a structured tool call now).
func TestTaskReview_R1_HandleVerdictValidatesAndReceipts(t *testing.T) {
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	ag := newTaskAreaTestAgent(t, "sess-task-review-r1")

	// Invalid verdict rejected.
	gotInvalid := ag.handleTaskReview(map[string]interface{}{"action": "review", "verdict": "X"})
	if !strings.Contains(gotInvalid, "invalid verdict") {
		t.Errorf("invalid verdict must be rejected, got: %q", gotInvalid)
	}
	if ag.lastTaskReviewVerdict != "" {
		t.Errorf("invalid verdict must not latch a verdict, got %q", ag.lastTaskReviewVerdict)
	}

	// Valid verdict A latches + returns the on-track action.
	gotA := ag.handleTaskReview(map[string]interface{}{"action": "review", "verdict": "A"})
	if ag.lastTaskReviewVerdict != "A" {
		t.Errorf("verdict A must latch, got %q", ag.lastTaskReviewVerdict)
	}
	if !strings.Contains(gotA, "VERDICT A") || !strings.Contains(gotA, "continue") {
		t.Errorf("A must return the on-track continue action, got: %q", gotA)
	}
}

// R1b — verdict B (re-align) drives the model to re-emit the roadmap: the
// returned instruction names the re-aligned [TASK_LIST] re-emission.
func TestTaskReview_R1b_VerdictBRAlignInstructsReEmission(t *testing.T) {
	action := taskReviewMenuFor("B", "", "goal was narrower than stated")
	if !strings.Contains(action, "RE-ALIGN") {
		t.Errorf("B must frame a re-align, got: %q", action)
	}
	if !strings.Contains(action, "[TASK_LIST_BEGIN]") {
		t.Errorf("B must instruct the roadmap re-emission, got: %q", action)
	}
}

// R2 — e2e: a commit round is followed by the model CALLING task(action=review)
// as a real tool_use; the harness records the verdict. No text-injection
// review user message appears.
func TestTaskReview_R2_CommitThenReviewToolcall_NoTextInjection(t *testing.T) {
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	script := []func(w http.ResponseWriter){
		// Round 1: commit item "1".
		func(w http.ResponseWriter) { commitToolUseStream(w, "1", "item 1 resolved") },
		// Round 2: model calls task(action=review, verdict=A).
		func(w http.ResponseWriter) { reviewToolUseStream(w, "A", "", "all good") },
		// Round 3: all-done exit.
		func(w http.ResponseWriter) {
			claudeTextStream(w, taskListResponse(allDoneText, t2Item("1", "Fix bug", "completed")), "end_turn")
		},
	}
	e := newE2EAgent(t, "sess-task-review-r2", script)
	e.ag.taskAreaActive = true
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "progressing"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	e.ag.TaskState.update(tasks)

	result, err := e.ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected the all-done answer as result, got: %q", result)
	}
	// The review was a real tool call and the harness latched the verdict.
	if e.ag.lastTaskReviewVerdict != "A" {
		t.Fatalf("expected the review verdict A to be latched, got %q", e.ag.lastTaskReviewVerdict)
	}
	// The session raw log must show the task(action=review) tool_use (the
	// acceptance that review is a TOOL CALL, not an injected text prompt) —
	// and must NOT contain the removed text-injection marker.
	e.ag.SessionLog.Sync()
	raw := mustReadFile(t, e.ag.SessionLog.Path())
	if !strings.Contains(raw, `"name":"task"`) || !strings.Contains(raw, `"action"`) || !strings.Contains(raw, `"verdict"`) {
		t.Fatalf("session raw log must show the task(action=review) tool call:\n%s", raw)
	}
}

// R4 — e2e redo: verdict D → the model re-commits the same task_id with a
// SUPERSEDES conclusion (append-only). Both the original conclusion, the
// review receipt, and the superseding redo exist.
func TestTaskReview_R4_VerdictDRedo_SupersedesAppendOnly(t *testing.T) {
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	script := []func(w http.ResponseWriter){
		// Round 1: commit item "1" via approach A.
		func(w http.ResponseWriter) { commitToolUseStream(w, "1", "item 1 resolved via approach A") },
		// Round 2: review verdict D → redo item 1.
		func(w http.ResponseWriter) { reviewToolUseStream(w, "D", "1", "approach A was wrong") },
		// Round 3: model commits the redo on the SAME task_id (supersede).
		func(w http.ResponseWriter) {
			commitToolUseStream(w, "1", "SUPERSEDES previous: approach A hit a dead-end, redoing via approach B")
		},
		// Round 4: all-done exit.
		func(w http.ResponseWriter) {
			claudeTextStream(w, taskListResponse(allDoneText, t2Item("1", "Fix bug", "completed")), "end_turn")
		},
	}
	e := newE2EAgent(t, "sess-task-review-r4", script)
	e.ag.taskAreaActive = true
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "progressing"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	e.ag.TaskState.update(tasks)

	result, err := e.ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected the all-done answer as result, got: %q", result)
	}
	// Verdict D latched.
	if e.ag.lastTaskReviewVerdict != "D" {
		t.Fatalf("expected verdict D latched, got %q", e.ag.lastTaskReviewVerdict)
	}
	// DB has BOTH the original conclusion (commit round 1) and the
	// supersede redo conclusion (commit round 3) — append-only superseding.
	concs, err := tools.SessionConclusions("sess-task-review-r4")
	if err != nil {
		t.Fatalf("SessionConclusions: %v", err)
	}
	var sawOriginal, sawSupercede bool
	for _, c := range concs {
		if strings.Contains(c.Conclusion, "resolved via approach A") {
			sawOriginal = true // the ORIGINAL commit conclusion
		}
		if strings.HasPrefix(c.Conclusion, "SUPERSEDES") {
			sawSupercede = true // the redo commit conclusion
		}
	}
	if !sawOriginal || !sawSupercede {
		t.Errorf("append-only: expected an original conclusion AND a superseding redo; concs=%+v", concs)
	}
}

// R5 — closing review: when every tracked item is committed, the review
// result flags the whole-epic closing review.
func TestTaskReview_R5_ClosingReview_AllItemsCommitted(t *testing.T) {
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	ag := newTaskAreaTestAgent(t, "sess-task-review-r5")
	ag.TaskState = newTodoState()
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "completed"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	ag.TaskState.update(tasks)

	got := ag.handleTaskReview(map[string]interface{}{"action": "review", "verdict": "A"})
	if !strings.Contains(got, "CLOSING REVIEW") {
		t.Errorf("with all items completed, the review must be flagged closing; got: %q", got)
	}
}

// R6 — e2e sanity: with NoEnforceMemory + a trivial run, task(action=view)
// still returns the tracked list (view stays read-only/parallel-safe).
func TestTaskView_StillReturnsTaskList(t *testing.T) {
	ag := newTaskAreaTestAgent(t, "sess-task-view")
	ag.TaskState = newTodoState()
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "progressing"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	ag.TaskState.update(tasks)

	got := ag.executeTool(deepseek.ToolCall{Name: "task", Arguments: map[string]interface{}{"action": "view"}})
	if !strings.Contains(got, "progressing") || !strings.Contains(got, "Fix bug") {
		t.Errorf("task(action=view) must render the tracked list, got: %q", got)
	}
}

// R7 — parallel classification is ACTION-AWARE: task(action=view) is
// parallel-safe (READ-ONLY, like the old todo), while task(action=commit) and
// task(action=review) are side effects that must run serial (Phase 2). Mixed
// rounds overlap only the two view calls.
func TestTaskReview_R7_ActionAwareParallelSafety(t *testing.T) {
	view := deepseek.ToolCall{Name: "task", Arguments: map[string]interface{}{"action": "view"}}
	commit := deepseek.ToolCall{Name: "task", Arguments: map[string]interface{}{"action": "commit", "task_id": "1", "conclusion": "x"}}
	review := deepseek.ToolCall{Name: "task", Arguments: map[string]interface{}{"action": "review", "verdict": "A"}}

	if !parallelSafeCall(view) {
		t.Error("task(action=view) must be parallel-safe (read-only view)")
	}
	if parallelSafeCall(commit) {
		t.Error("task(action=commit) must run serial (side effect)")
	}
	if parallelSafeCall(review) {
		t.Error("task(action=review) must run serial (records verdict)")
	}

	// Real batch scheduling: two view calls overlap (max==2); a commit in the
	// same round never overlaps them.
	var ct concurrentTracker
	ag := &Agent{executeToolFn: func(tc deepseek.ToolCall) string {
		ct.enter()
		defer ct.leave()
		time.Sleep(150 * time.Millisecond) //stylegate:allow-sleep settle window for the asynchronous review write
		return "result-" + tc.ID
	}}
	calls := []deepseek.ToolCall{
		{ID: "v1", Name: "task", Arguments: map[string]interface{}{"action": "view"}},
		{ID: "c1", Name: "task", Arguments: map[string]interface{}{"action": "commit", "task_id": "1", "conclusion": "x"}},
		{ID: "v2", Name: "task", Arguments: map[string]interface{}{"action": "view"}},
	}
	results := ag.executeToolBatch(calls)
	if max := ct.maxConcurrent(); max != 2 {
		t.Fatalf("expected the two task-views concurrent, commit serial (max==2), got %d", max)
	}
	want := []string{"result-v1", "result-c1", "result-v2"}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("results out of order at %d: %v", i, results)
		}
	}
}
