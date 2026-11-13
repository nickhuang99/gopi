package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---- FlexibleString: JSON string/number compatibility ----

// TestFlexibleString_Unmarshal_String locks the primary path: a JSON string
// unmarshals identically to its quoted form. The gopilog sibling package
// keeps a second copy of this type (todoItem); this test protects the agent
// copy so the Phase-2 merge must preserve both behaviors.
func TestFlexibleString_Unmarshal_String(t *testing.T) {
	var f FlexibleString
	if err := json.Unmarshal([]byte(`"t001"`), &f); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if f != "t001" {
		t.Fatalf("got %q, want t001", string(f))
	}
}

// TestFlexibleString_Unmarshal_Number locks the number fallback: numeric IDs
// the LLM emits ("id": 1) must coerce to their decimal string form ("1").
func TestFlexibleString_Unmarshal_Number(t *testing.T) {
	var f FlexibleString
	if err := json.Unmarshal([]byte(`1`), &f); err != nil {
		t.Fatalf("unmarshal integer: %v", err)
	}
	if f != "1" {
		t.Fatalf("got %q, want 1", string(f))
	}

	var g FlexibleString
	if err := json.Unmarshal([]byte(`123456`), &g); err != nil {
		t.Fatalf("unmarshal integer 123456: %v", err)
	}
	if g != "123456" {
		t.Fatalf("got %q, want 123456", string(g))
	}
}

// TestFlexibleString_Unmarshal_Fraction and float forms: json.Number.String()
// preserves the raw literal, so 3.14 → "3.14".
func TestFlexibleString_Unmarshal_Float(t *testing.T) {
	var f FlexibleString
	if err := json.Unmarshal([]byte(`3.14`), &f); err != nil {
		t.Fatalf("unmarshal float: %v", err)
	}
	if f != "3.14" {
		t.Fatalf("got %q, want 3.14", string(f))
	}
}

// TestFlexibleString_Unmarshal_Invalid locks the rejection path: non-string,
// non-number JSON cannot unmarshal into FlexibleString.
func TestFlexibleString_Unmarshal_Invalid(t *testing.T) {
	var f FlexibleString
	err := json.Unmarshal([]byte(`true`), &f)
	if err == nil {
		t.Fatal("expected error unmarshaling a bool into FlexibleString, got nil")
	}
	if !strings.Contains(err.Error(), "flexibleString") {
		t.Fatalf("expected 'flexibleString' in error, got: %v", err)
	}

	var g FlexibleString
	err = json.Unmarshal([]byte(`{"nested":"obj"}`), &g)
	if err == nil {
		t.Fatal("expected error unmarshaling an object, got nil")
	}
}

// TestFlexibleString_EmptyNumber keeps the coercion honest for 0.
func TestFlexibleString_Unmarshal_Zero(t *testing.T) {
	var f FlexibleString
	if err := json.Unmarshal([]byte(`0`), &f); err != nil {
		t.Fatalf("unmarshal 0: %v", err)
	}
	if f != "0" {
		t.Fatalf("got %q, want 0", string(f))
	}
}

// ---- TaskItem.Resolve(): alternate-names fallback ----

// TestTaskItem_Resolve_FallsBackToAltFields locks the backward-compat logic:
// when the canonical field is empty and its alternate is set, resolve() fills
// the canonical from the alternate (id→task_id, content→summary,
// created_at→create_at).
func TestTaskItem_Resolve_FallsBackToAltFields(t *testing.T) {
	ti := TaskItem{
		AltID:       "alt-42",
		AltSummary:  "alt summary text",
		AltCreateAt: "1780000000",
	}
	ti.Resolve()
	if ti.TaskID != "alt-42" {
		t.Errorf("TaskID = %q, want alt-42", string(ti.TaskID))
	}
	if ti.Summary != "alt summary text" {
		t.Errorf("Summary = %q, want alt summary text", ti.Summary)
	}
	if ti.CreateAt != "1780000000" {
		t.Errorf("CreateAt = %q, want 1780000000", string(ti.CreateAt))
	}
}

// TestTaskItem_Resolve_CanonicalWins locks precedence: a non-empty canonical
// field is never overwritten by the alternate, even when both are present.
func TestTaskItem_Resolve_CanonicalWins(t *testing.T) {
	ti := TaskItem{
		TaskID:      "canon-1",
		Summary:     "canonical summary",
		CreateAt:    "999",
		AltID:       "alt-1",
		AltSummary:  "alt summary",
		AltCreateAt: "888",
	}
	ti.Resolve()
	if ti.TaskID != "canon-1" {
		t.Errorf("TaskID = %q, want canonical canonical-1", string(ti.TaskID))
	}
	if ti.Summary != "canonical summary" {
		t.Errorf("Summary = %q, want canonical summary", ti.Summary)
	}
	if ti.CreateAt != "999" {
		t.Errorf("CreateAt = %q, want 999", string(ti.CreateAt))
	}
}

// TestTaskItem_Resolve_PartialFallback locks mixed inputs: resolution is
// per-field, so a mix of canonical and alternate names each resolve.
func TestTaskItem_Resolve_PartialFallback(t *testing.T) {
	ti := TaskItem{
		TaskID:      "keep-me",
		AltSummary:  "alt only",
		AltCreateAt: "777",
	}
	ti.Resolve()
	if ti.TaskID != "keep-me" {
		t.Errorf("TaskID = %q, want keep-me", string(ti.TaskID))
	}
	if ti.Summary != "alt only" {
		t.Errorf("Summary = %q, want alt only", ti.Summary)
	}
	if ti.CreateAt != "777" {
		t.Errorf("CreateAt = %q, want 777", string(ti.CreateAt))
	}
}

// TestTaskItem_Resolve_EmptyBoth locks the no-op: empty canonical AND empty
// alternate leaves the field empty (no spurious fill).
func TestTaskItem_Resolve_EmptyBoth(t *testing.T) {
	ti := TaskItem{}
	ti.Resolve()
	if ti.TaskID != "" || ti.Summary != "" || ti.CreateAt != "" {
		t.Errorf("empty item must stay empty after resolve, got %+v", ti)
	}
}

// ---- ParseTaskList resolves alt fields end-to-end ----

// TestParseTaskList_ResolvesAltFields locks the end-to-end contract: a task
// block using alternate field names (id/content/created_at) and a numeric id
// must come back canonicalized after ParseTaskList.
func TestParseTaskList_ResolvesAltFields(t *testing.T) {
	response := `[TASK_LIST_BEGIN]
[
  {"id": 7, "content": "write parser", "created_at": 1780000000, "status": "progressing"}
]
[TASK_LIST_END]`

	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].TaskID != "7" {
		t.Errorf("TaskID = %q, want 7 (numeric id coerced)", string(tasks[0].TaskID))
	}
	if tasks[0].Summary != "write parser" {
		t.Errorf("Summary = %q, want 'write parser'", tasks[0].Summary)
	}
	if tasks[0].CreateAt != "1780000000" {
		t.Errorf("CreateAt = %q, want 1780000000", string(tasks[0].CreateAt))
	}
	if tasks[0].Status != TaskStatusProgressing {
		t.Errorf("Status = %q, want progressing", tasks[0].Status)
	}
}

// TestParseTaskList_NoAltFields locks that canonical fields pass through
// untouched (resolve is a no-op when nothing to fall back). Guards against an
// over-eager refactor that drops the resolve call entirely.
func TestParseTaskList_NoAltFields(t *testing.T) {
	response := `[TASK_LIST_BEGIN]
[{"task_id":"t1","summary":"s","create_at":"5","status":"completed"}]
[TASK_LIST_END]`
	tasks, err := ParseTaskList(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].TaskID != "t1" || tasks[0].Summary != "s" || tasks[0].CreateAt != "5" {
		t.Errorf("canonical fields mangled by resolve: %+v", tasks)
	}
}

// ---- contentWithoutTaskList: answer-substantiveness stripping ----

func TestContentWithoutTaskList_StripsBlock(t *testing.T) {
	in := "  This is the answer text.\n[TASK_LIST_BEGIN]\n[{}]\n[TASK_LIST_END]\n"
	got := contentWithoutTaskList(in)
	if got != "This is the answer text." {
		t.Errorf("got %q, want answer without block", got)
	}
}

func TestContentWithoutTaskList_NoMarker_Passthrough(t *testing.T) {
	in := "  just content, no markers  "
	got := contentWithoutTaskList(in)
	if got != "just content, no markers" {
		t.Errorf("got %q, want trimmed passthrough", got)
	}
}

func TestContentWithoutTaskList_Empty(t *testing.T) {
	if got := contentWithoutTaskList(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
