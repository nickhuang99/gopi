package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestParseTaskList_Valid(t *testing.T) {
	response := `Some text before.
[TASK_LIST_BEGIN]
[
  {"task_id":"t001","summary":"读取配置文件","create_at":"1780000000","status":"completed"},
  {"task_id":"t002","summary":"修改数据库schema","create_at":"1780000000","status":"progressing"}
]
[TASK_LIST_END]
Some text after.`

	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].TaskID != "t001" {
		t.Fatalf("expected t001, got %s", tasks[0].TaskID)
	}
	if tasks[0].Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", tasks[0].Status)
	}
	if tasks[1].TaskID != "t002" {
		t.Fatalf("expected t002, got %s", tasks[1].TaskID)
	}
	if tasks[1].Status != TaskStatusProgressing {
		t.Fatalf("expected progressing, got %s", tasks[1].Status)
	}
}

func TestParseTaskList_NoMarkers(t *testing.T) {
	response := "Just some normal text without any task list markers."
	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tasks != nil {
		t.Fatalf("expected nil tasks when no markers, got %v", tasks)
	}
}

func TestParseTaskList_MalformedJSON(t *testing.T) {
	response := `[TASK_LIST_BEGIN]
{this is not valid json}
[TASK_LIST_END]`

	_, err := ParseTaskList(response)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "JSON parse error") {
		t.Fatalf("expected JSON parse error, got: %v", err)
	}
}

func TestParseTaskList_EmptyList(t *testing.T) {
	response := `[TASK_LIST_BEGIN]
[]
[TASK_LIST_END]`

	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected empty list, got %d tasks", len(tasks))
	}
}

func TestParseTaskList_MultipleBlocks(t *testing.T) {
	// When multiple blocks exist, the LAST one should be used.
	response := `[TASK_LIST_BEGIN]
[{"task_id":"old","summary":"old task","create_at":"1","status":"progressing"}]
[TASK_LIST_END]
Some text in between.
[TASK_LIST_BEGIN]
[{"task_id":"new","summary":"new task","create_at":"2","status":"completed"}]
[TASK_LIST_END]`

	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task from last block, got %d", len(tasks))
	}
	if tasks[0].TaskID != "new" {
		t.Fatalf("expected task_id 'new' from last block, got %q", tasks[0].TaskID)
	}
}

func TestParseTaskList_WhitespaceAroundMarkers(t *testing.T) {
	response := `  [TASK_LIST_BEGIN]  
[{"task_id":"t1","summary":"test","create_at":"1","status":"progressing"}]
  [TASK_LIST_END]  `

	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestParseTaskList_MissingEndMarker_PartialRecovery(t *testing.T) {
	// A-class fixture (mirrors the audit's recoverable class): a complete
	// JSON list with no [TASK_LIST_END]. balanced-prefix recovery must return
	// the tasks AND the ErrTaskListRecovered sentinel (not nil).
	response := `[TASK_LIST_BEGIN]
[{"task_id":"t1","summary":"test","create_at":"1","status":"progressing"}]`

	tasks, err := ParseTaskList(response)
	if !errors.Is(err, ErrTaskListRecovered) {
		t.Fatalf("expected ErrTaskListRecovered, got err=%v", err)
	}
	if tasks == nil {
		t.Fatalf("expected recovered tasks, got nil")
	}
	if len(tasks) != 1 || tasks[0].TaskID != "t1" || tasks[0].Status != TaskStatusProgressing {
		t.Fatalf("unexpected recovered tasks: %+v", tasks)
	}
}

func TestParseTaskList_OnlyBeginMarker(t *testing.T) {
	// Nothing parseable after BEGIN → nil + ErrTaskListUnparseable, no garbage.
	response := "[TASK_LIST_BEGIN]"
	tasks, err := ParseTaskList(response)
	if !errors.Is(err, ErrTaskListUnparseable) {
		t.Fatalf("expected ErrTaskListUnparseable, got err=%v", err)
	}
	if tasks != nil {
		t.Fatalf("expected nil tasks, got %v", tasks)
	}
}

func TestParseTaskList_EmptyBetweenMarkers(t *testing.T) {
	response := "[TASK_LIST_BEGIN]\n\n[TASK_LIST_END]"
	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tasks != nil {
		t.Fatalf("expected nil tasks, got %v", tasks)
	}
}

// classAFixture68 is the REAL 7-task list from session
// 68827372-1b53-4e1f-b21a-92c97bf5f177.jsonl line 281 — complete balanced JSON,
// no [TASK_LIST_END] (the LLM forgot the closing marker). This is the exact
// text the eval's A-class recovery targets.
const classAFixture68 = `All deliverables complete. Let me finalize the task list.
[TASK_LIST_BEGIN]
[{"task_id":"1","summary":"研究 loop.go prompt 常量 + gitblame.go 实现 + claude 命令锚点参考","status":"completed"},{"task_id":"2","summary":"修改 git_blame Description 首句为 ALWAYS/NEVER 命令锚点（零密度成本）","status":"completed"},{"task_id":"3","summary":"重建二进制 bin/gopi","status":"completed"},{"task_id":"4","summary":"设计并跑 2 个天然触发 git_blame 的真实任务场景","status":"completed"},{"task_id":"5","summary":"从会话日志验证 git_blame tool_use ≥2 次独立场景","status":"completed"},{"task_id":"6","summary":"go build + go test 全绿（确认唯一失败为 pre-existing flake）","status":"completed"},{"task_id":"7","summary":"逐文件 commit + devdoc 报告","status":"completed"}]`

// classAFixtureXML is REAL text from session
// 6613fe97-5524-49df-b417-4a31a614b98a.jsonl line 591 — a completed task list
// followed by a trailing `</parameter></invoke>` XML pastiche (the model's next
// tool_use leaked into the text after the list). balanced-prefix recovery must
// strips the trailing XML and recover exactly the JSON array (a blind
// BEGIN-to-EOF parse would fail on the non-JSON tail).
const classAFixtureXML = `While the full suite runs, let me update my task list and memory with lessons learned.
[TASK_LIST_BEGIN]
[{"task_id":"1","summary":"Read all task files + source","status":"completed"},
{"task_id":"2","summary":"P0: API/env/struct","status":"completed"},
{"task_id":"3","summary":"P0: TakeOldest + serialize + budget","status":"completed"},
{"task_id":"4","summary":"P0: compactContext + reset + assertion log","status":"completed"},
{"task_id":"5","summary":"P0: Tests + commit 6d4d862","status":"completed"},
{"task_id":"6","summary":"P1: REFERENCE ONLY + history prompt","status":"completed"},
{"task_id":"7","summary":"P1: Anchors + store protection","status":"completed"},
{"task_id":"8","summary":"P1: Cooldown + debounce","status":"completed"},
{"task_id":"9","summary":"P1: Tests + commit e1fe019","status":"completed"},
{"task_id":"10","summary":"Defaults test + Way-A simulator + memory smoke + report","status":"completed"},
{"task_id":"11","summary":"Atomic commits (4 total) + final test green","status":"completed"}]</parameter>
</invoke>
</tool_calls>`

// classBFixture is a BEGIN-with-END-missing AND the JSON array is truncated
// (never closed — a real finish_reason=length cut). Recovery cannot produce a
// valid list → nil + ErrTaskListUnparseable, and MUST NOT panic.
const classBFixture = `[TASK_LIST_BEGIN]
[{"task_id":"1","summary":"first task","status":"progressing"},
{"task_id":"2","summary":"truncated task, no closing bracket`

// classCFixtureProse is REAL prose from session 0a2a237c line 103 — it quotes
// the "[TASK_LIST_BEGIN] JSON block" marker in discussion but has NO task-list
// JSON after it. balanced-prefix recovery must reject it → nil + warning, zero
// garbage list.
const classCFixtureProse = `The relevant comparison framework for gopi is Claude Code itself — which the repo's own devdoc already covers (the perf-vs-claude-fix-plan references "the mandatory [TASK_LIST_BEGIN] JSON block").`

func TestParseTaskList_ClassA_Recover_RealFixture68(t *testing.T) {
	tasks, err := ParseTaskList(classAFixture68)
	if !errors.Is(err, ErrTaskListRecovered) {
		t.Fatalf("expected ErrTaskListRecovered, got err=%v", err)
	}
	if tasks == nil || len(tasks) != 7 {
		t.Fatalf("expected 7 recovered tasks, got %d: %+v", len(tasks), tasks)
	}
	// Task 7 must be completed and its summary intact (real fixture check).
	if tasks[6].TaskID != "7" || tasks[6].Status != TaskStatusCompleted {
		t.Fatalf("unexpected recovered task[6]: %+v", tasks[6])
	}
}

func TestParseTaskList_ClassA_Recover_StripsTrailingXML(t *testing.T) {
	tasks, err := ParseTaskList(classAFixtureXML)
	if !errors.Is(err, ErrTaskListRecovered) {
		t.Fatalf("expected ErrTaskListRecovered, got err=%v", err)
	}
	if tasks == nil || len(tasks) != 11 {
		t.Fatalf("expected 11 recovered tasks, got %d", len(tasks))
	}
	// The trailing `</parameter>` (and everything after the JSON array) must be
	// stripped — task 11 is a valid completed task, not XML garbage.
	if tasks[10].TaskID != "11" || tasks[10].Status != TaskStatusCompleted {
		t.Fatalf("unexpected task[10] (XML tail not stripped?): %+v", tasks[10])
	}
}

func TestParseTaskList_ClassB_TruncatedJSON_NoPanic(t *testing.T) {
	// Must not panic and must not produce a truncated/garbage list.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ParseTaskList panicked on truncated JSON: %v", r)
		}
	}()
	tasks, err := ParseTaskList(classBFixture)
	if !errors.Is(err, ErrTaskListUnparseable) {
		t.Fatalf("expected ErrTaskListUnparseable, got err=%v", err)
	}
	if tasks != nil {
		t.Fatalf("expected nil tasks for truncated JSON, got %+v", tasks)
	}
}

func TestParseTaskList_ClassC_ProseReference_ZeroGarbage(t *testing.T) {
	tasks, err := ParseTaskList(classCFixtureProse)
	if !errors.Is(err, ErrTaskListUnparseable) {
		t.Fatalf("expected ErrTaskListUnparseable (prose ref), got err=%v", err)
	}
	if tasks != nil {
		t.Fatalf("expected nil tasks for prose reference (no garbage), got %+v", tasks)
	}
}

// The recovery list must be a CITIZEN of the normal downstream pipeline: after
// ParseTaskList recovers it, TaskState.update must set hasList=true exactly as
// a closed-block list would, so the exit protection and R3 fire. This tests
// the end-to-end todoState wiring with a recovered list.
func TestParseTaskList_Recovered_FeedsTaskState_ActivatesExitProtection(t *testing.T) {
	ts := newTodoState()
	tasks, err := ParseTaskList(classAFixture68)
	if !errors.Is(err, ErrTaskListRecovered) {
		t.Fatalf("expected recovery, got err=%v", err)
	}
	ts.update(tasks)
	if !ts.hasList {
		t.Fatal("recovered list must set hasList=true (activates exit protection)")
	}
	if len(ts.tasks) != 7 {
		t.Fatalf("expected 7 tasks in state, got %d", len(ts.tasks))
	}
	// All 7 completed → the exit-protection/AllCompleted path sees a finished
	// plan, so the loop can trust a final exit.
	if !AllCompleted(ts.tasks) {
		t.Fatalf("recovered all-completed list must be AllCompleted")
	}
}

// Boundaries: END appears BEFORE BEGIN — existing paired semantics hold (the
// list between BEGIN and a LATER END, or nothing, is returned per LastIndex).
func TestParseTaskList_Boundary_EndBeforeBegin(t *testing.T) {
	// No END after BEGIN → recovery path. The tail after BEGIN here has no JSON
	// prefix → unparseable (not a valid list).
	response := "some [TASK_LIST_END] text [TASK_LIST_BEGIN]\njust prose"
	tasks, err := ParseTaskList(response)
	if !errors.Is(err, ErrTaskListUnparseable) {
		t.Fatalf("expected ErrTaskListUnparseable, got err=%v", err)
	}
	if tasks != nil {
		t.Fatalf("expected nil tasks, got %v", tasks)
	}
}

// Boundaries: a normal closed BEGIN+END pair still parses via the strict path
// (delta from recovery sentinel — no warning).
func TestParseTaskList_Boundary_NormalClosedPair(t *testing.T) {
	response := "[TASK_LIST_BEGIN]\n[{\"task_id\":\"a\",\"summary\":\"x\",\"status\":\"progressing\"}]\n[TASK_LIST_END]"
	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tasks == nil || len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %v", tasks)
	}
}

func TestValidateTaskList_NoChanges(t *testing.T) {
	prev := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
		{TaskID: "t002", Status: TaskStatusProgressing},
	}
	curr := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
		{TaskID: "t002", Status: TaskStatusProgressing},
	}
	if err := ValidateTaskList(prev, curr); err != nil {
		t.Fatalf("expected no error for identical lists, got: %v", err)
	}
}

func TestValidateTaskList_StatusChange(t *testing.T) {
	prev := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	curr := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
	}
	if err := ValidateTaskList(prev, curr); err != nil {
		t.Fatalf("expected no error for status change, got: %v", err)
	}
}

func TestValidateTaskList_RejectDeletion(t *testing.T) {
	prev := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusProgressing},
	}
	curr := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		// t002 deleted!
	}
	err := ValidateTaskList(prev, curr)
	if err == nil {
		t.Fatal("expected error for deleted task, got nil")
	}
	if !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("expected 'deleted' in error, got: %v", err)
	}
}

func TestValidateTaskList_InvalidStatus(t *testing.T) {
	prev := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	curr := []TaskItem{
		{TaskID: "t001", Status: "done"}, // invalid
	}
	err := ValidateTaskList(prev, curr)
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected 'invalid status' in error, got: %v", err)
	}
}

func TestValidateTaskList_DuplicateID(t *testing.T) {
	prev := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	curr := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
		{TaskID: "t001", Status: TaskStatusCompleted}, // duplicate
	}
	err := ValidateTaskList(prev, curr)
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected 'duplicate' in error, got: %v", err)
	}
}

func TestValidateTaskList_ParentDecomposed(t *testing.T) {
	// Decomposition = new child ids added alongside the parent id. The JSON is
	// flat (no nesting), so this structural validator cannot see WHICH id is
	// the parent — it only guards the vocabulary/deletion/duplicate invariants.
	// The "parent stays progressing until every child is completed" rule is a
	// prompt contract, backstopped by the all-completed exit gate (see
	// devdoc/system-prompt-fix-2026-09-07.md, T3).
	prev := []TaskItem{
		{TaskID: "t001", Summary: "implement feature", Status: TaskStatusProgressing},
	}
	// Parent id preserved (still progressing), two children added.
	curr := []TaskItem{
		{TaskID: "t001", Summary: "implement feature", Status: TaskStatusProgressing},
		{TaskID: "t002", Summary: "write parser", Status: TaskStatusPending},
		{TaskID: "t003", Summary: "write tests", Status: TaskStatusPending},
	}
	if err := ValidateTaskList(prev, curr); err != nil {
		t.Fatalf("expected decomposition (new child ids) to be valid, got: %v", err)
	}
}

// TestValidateTaskList_PendingAccepted guards the three-state vocabulary
// (review 2026-09-07 T2): "pending" is a legal status — planned-but-not-started
// work must not have to assert it is already progressing.
func TestValidateTaskList_PendingAccepted(t *testing.T) {
	curr := []TaskItem{
		{TaskID: "t001", Status: TaskStatusPending},
		{TaskID: "t002", Status: TaskStatusProgressing},
		{TaskID: "t003", Status: TaskStatusCompleted},
	}
	if err := ValidateTaskList(nil, curr); err != nil {
		t.Fatalf("expected all three statuses to validate, got: %v", err)
	}
	// A pending task can transition to progressing and on to completed.
	prev := []TaskItem{{TaskID: "t001", Status: TaskStatusPending}}
	if err := ValidateTaskList(prev, []TaskItem{{TaskID: "t001", Status: TaskStatusProgressing}}); err != nil {
		t.Fatalf("expected pending→progressing to validate, got: %v", err)
	}
}

// TestAllCompleted_PendingNotDone guards the DONE gate against the new state:
// a list holding pending work never reads all-completed, so the R3 fast-exit
// and the exit warning cannot fire on a plan-only list.
func TestAllCompleted_PendingNotDone(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusPending},
	}
	if AllCompleted(tasks) {
		t.Fatal("pending task must block AllCompleted")
	}
}

// TestCountPending guards the pending counter used by todoState.format and the
// exit warning.
func TestCountPending(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Status: TaskStatusPending},
		{TaskID: "t002", Status: TaskStatusPending},
		{TaskID: "t003", Status: TaskStatusProgressing},
		{TaskID: "t004", Status: TaskStatusCompleted},
	}
	if got := CountPending(tasks); got != 2 {
		t.Fatalf("CountPending = %d, want 2", got)
	}
	if got := CountPending(nil); got != 0 {
		t.Fatalf("CountPending(nil) = %d, want 0", got)
	}
}

// TestTaskItem_IsPending guards the pending predicate.
func TestTaskItem_IsPending(t *testing.T) {
	if !(TaskItem{Status: TaskStatusPending}).IsPending() {
		t.Fatal("pending item must report IsPending")
	}
	for _, s := range []string{TaskStatusProgressing, TaskStatusCompleted, ""} {
		if (TaskItem{Status: s}).IsPending() {
			t.Fatalf("status %q must not report IsPending", s)
		}
	}
}

func TestValidateTaskList_EmptyTaskID(t *testing.T) {
	prev := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	curr := []TaskItem{
		{TaskID: "", Status: TaskStatusProgressing},
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	err := ValidateTaskList(prev, curr)
	if err == nil {
		t.Fatal("expected error for empty task_id, got nil")
	}
	if !strings.Contains(err.Error(), "empty task_id") {
		t.Fatalf("expected 'empty task_id' in error, got: %v", err)
	}
}

func TestValidateTaskList_EmptyPrevious(t *testing.T) {
	// First-round: no previous tasks, any current list is valid.
	curr := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	if err := ValidateTaskList(nil, curr); err != nil {
		t.Fatalf("expected no error for first round, got: %v", err)
	}
}

func TestAllCompleted_AllDone(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusCompleted},
	}
	if !AllCompleted(tasks) {
		t.Fatal("expected true when all tasks completed")
	}
}

func TestAllCompleted_SomeProgressing(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusProgressing},
	}
	if AllCompleted(tasks) {
		t.Fatal("expected false when some tasks progressing")
	}
}

func TestAllCompleted_Empty(t *testing.T) {
	tasks := []TaskItem{}
	if AllCompleted(tasks) {
		t.Fatal("expected false for empty list (no tasks = not done yet)")
	}
}

func TestAllCompleted_SingleProgressing(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	if AllCompleted(tasks) {
		t.Fatal("expected false for single progressing task")
	}
}

func TestCountProgressing(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusProgressing},
		{TaskID: "t003", Status: TaskStatusProgressing},
	}
	count := CountProgressing(tasks)
	if count != 2 {
		t.Fatalf("expected 2 progressing, got %d", count)
	}
}

func TestCountProgressing_None(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
	}
	count := CountProgressing(tasks)
	if count != 0 {
		t.Fatalf("expected 0 progressing, got %d", count)
	}
}

func TestCountProgressing_Empty(t *testing.T) {
	count := CountProgressing(nil)
	if count != 0 {
		t.Fatalf("expected 0 for nil list, got %d", count)
	}
}

func TestEqualTaskLists_Same(t *testing.T) {
	a := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusProgressing},
	}
	b := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusProgressing},
	}
	if !EqualTaskLists(a, b) {
		t.Fatal("expected equal for identical lists")
	}
}

func TestEqualTaskLists_DifferentStatus(t *testing.T) {
	a := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
	}
	b := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	if EqualTaskLists(a, b) {
		t.Fatal("expected not equal for different statuses")
	}
}

func TestEqualTaskLists_DifferentIDs(t *testing.T) {
	a := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
	}
	b := []TaskItem{
		{TaskID: "t002", Status: TaskStatusCompleted},
	}
	if EqualTaskLists(a, b) {
		t.Fatal("expected not equal for different IDs")
	}
}

func TestEqualTaskLists_DifferentLength(t *testing.T) {
	a := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
	}
	b := []TaskItem{
		{TaskID: "t001", Status: TaskStatusCompleted},
		{TaskID: "t002", Status: TaskStatusProgressing},
	}
	if EqualTaskLists(a, b) {
		t.Fatal("expected not equal for different lengths")
	}
}

func TestEqualTaskLists_BothEmpty(t *testing.T) {
	if !EqualTaskLists(nil, nil) {
		t.Fatal("expected equal for both nil")
	}
	if !EqualTaskLists([]TaskItem{}, []TaskItem{}) {
		t.Fatal("expected equal for both empty")
	}
}

func TestFormatTaskList(t *testing.T) {
	tasks := []TaskItem{
		{TaskID: "t001", Summary: "read config", CreateAt: "1780000000", Status: TaskStatusCompleted},
	}
	result := FormatTaskList(tasks)
	if !strings.Contains(result, TaskListBeginMarker) {
		t.Fatal("expected begin marker in output")
	}
	if !strings.Contains(result, TaskListEndMarker) {
		t.Fatal("expected end marker in output")
	}
	if !strings.Contains(result, "t001") {
		t.Fatal("expected task ID in output")
	}
	if !strings.Contains(result, TaskStatusCompleted) {
		t.Fatal("expected status in output")
	}
}

func TestFormatTaskList_Empty(t *testing.T) {
	result := FormatTaskList(nil)
	if !strings.Contains(result, TaskListBeginMarker) {
		t.Fatal("expected begin marker in output")
	}
	if !strings.Contains(result, TaskListEndMarker) {
		t.Fatal("expected end marker in output")
	}
}

func TestTaskItem_IsCompleted(t *testing.T) {
	item := TaskItem{Status: TaskStatusCompleted}
	if !item.IsCompleted() {
		t.Fatal("expected IsCompleted true")
	}
}

func TestTaskItem_IsCompleted_False(t *testing.T) {
	item := TaskItem{Status: TaskStatusProgressing}
	if item.IsCompleted() {
		t.Fatal("expected IsCompleted false for progressing")
	}
}

func TestTaskItem_IsProgressing(t *testing.T) {
	item := TaskItem{Status: TaskStatusProgressing}
	if !item.IsProgressing() {
		t.Fatal("expected IsProgressing true")
	}
}

func TestTaskItem_IsProgressing_False(t *testing.T) {
	item := TaskItem{Status: TaskStatusCompleted}
	if item.IsProgressing() {
		t.Fatal("expected IsProgressing false for completed")
	}
}

// Report A 6.1 (T2): "pending" is a third valid status — planned but not
// started. It must pass validation and read as not-completed / not-progressing
// downstream (IsCompleted/IsProgressing/AllCompleted/CountProgressing).
func TestValidateTaskList_AcceptsPending(t *testing.T) {
	prev := []TaskItem{
		{TaskID: "t001", Status: TaskStatusProgressing},
	}
	curr := []TaskItem{
		{TaskID: "t001", Status: TaskStatusPending}, // pending transition is legal
		{TaskID: "t002", Status: TaskStatusPending}, // planned-but-not-started
	}
	if err := ValidateTaskList(prev, curr); err != nil {
		t.Fatalf("expected no error for pending status, got: %v", err)
	}
	pending := TaskItem{TaskID: "t002", Status: TaskStatusPending}
	if pending.IsProgressing() {
		t.Fatal("pending must not read as progressing")
	}
	if pending.IsCompleted() {
		t.Fatal("pending must not read as completed")
	}
	if AllCompleted(curr) {
		t.Fatal("all-pending list must not read as all-completed")
	}
	if n := CountProgressing(curr); n != 0 {
		t.Fatalf("pending must not count as progressing, got %d", n)
	}
}
