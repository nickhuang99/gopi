package gopilog

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// These tests lock the gopilog side of the cross-package task-item
// duplication (agent/tasklist.go ↔ gopilog/formatter.go). The Phase-2 merge
// into a shared type must preserve BOTH copies' behavior — this file pins the
// gopilog copy's flexibleString coercion + todoItem.Resolve() semantics so a
// refactor cannot silently drop one package's contract.

// ---- flexibleString:String() ----

// TestFlexibleString_String locks the one method the agent copy lacks: the
// Stringer. Formatting code relies on fmt %s / String() to render the value.
func TestFlexibleString_String(t *testing.T) {
	f := flexibleString("abc")
	if f.String() != "abc" {
		t.Errorf("String() = %q, want abc", f.String())
	}
	// %s on a flexibleString uses String().
	if got := fmt.Sprintf("%s", f); got != "abc" {
		t.Errorf("%%s = %q, want abc", got)
	}
	// Numeric-coerced value also renders its stored string.
	f2 := gopiUnmarshalNumber(t, "42")
	if f2.String() != "42" {
		t.Errorf("String() = %q after number coercion, want 42", f2.String())
	}
}

// TestFlexibleString_TodoItemFieldString locks that a todoItem's ID field is
// a Stringer (the phase-2 merged type must keep this or formatter output
// changes).
func TestFlexibleString_TodoItemFieldString(t *testing.T) {
	raw := `{"task_id": 5, "summary":"s", "create_at": 1700000000, "status":"completed"}`
	var it todoItem
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := it.TaskID.String(); got != "5" {
		t.Errorf("TaskID.String() = %q, want 5", got)
	}
	if got := it.CreateAt.String(); got != "1700000000" {
		t.Errorf("CreateAt.String() = %q, want 1700000000", got)
	}
}

// ---- flexibleString:UnmarshalJSON string/number compat ----

func TestFlexibleString_Unmarshal_String(t *testing.T) {
	var f flexibleString
	if err := json.Unmarshal([]byte(`"t001"`), &f); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if f != "t001" {
		t.Errorf("got %q, want t001", string(f))
	}
}

func TestFlexibleString_Unmarshal_Number(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"1", "1"},
		{"0", "0"},
		{"123456", "123456"},
		{"3.14", "3.14"},
	} {
		var f flexibleString
		if err := json.Unmarshal([]byte(tc.in), &f); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.in, err)
		}
		if string(f) != tc.want {
			t.Errorf("unmarshal %s = %q, want %q", tc.in, string(f), tc.want)
		}
	}
}

func TestFlexibleString_Unmarshal_Invalid(t *testing.T) {
	var f flexibleString
	err := json.Unmarshal([]byte(`true`), &f)
	if err == nil {
		t.Fatal("expected error for bool, got nil")
	}
	if !strings.Contains(err.Error(), "flexibleString") {
		t.Errorf("expected 'flexibleString' in error, got %v", err)
	}
}

// ---- todoItem.Resolve(): alternate-name fallback ----

func TestTodoItem_Resolve_FallsBackToAltFields(t *testing.T) {
	it := todoItem{
		AltID:       "alt-9",
		AltSummary:  "alternate",
		AltCreateAt: "1780000000",
	}
	it.Resolve()
	if it.TaskID != "alt-9" {
		t.Errorf("TaskID = %q, want alt-9", string(it.TaskID))
	}
	if it.Summary != "alternate" {
		t.Errorf("Summary = %q, want alternate", it.Summary)
	}
	if it.CreateAt != "1780000000" {
		t.Errorf("CreateAt = %q, want 1780000000", string(it.CreateAt))
	}
}

func TestTodoItem_Resolve_CanonicalWins(t *testing.T) {
	it := todoItem{
		TaskID:      "canon",
		Summary:     "canonical",
		CreateAt:    "999",
		AltID:       "alt",
		AltSummary:  "alt-sum",
		AltCreateAt: "888",
	}
	it.Resolve()
	if it.TaskID != "canon" {
		t.Errorf("TaskID = %q, want canonical wins", string(it.TaskID))
	}
	if it.Summary != "canonical" {
		t.Errorf("Summary = %q, want canonical wins", it.Summary)
	}
	if it.CreateAt != "999" {
		t.Errorf("CreateAt = %q, want canonical wins", string(it.CreateAt))
	}
}

func TestTodoItem_Resolve_Partial(t *testing.T) {
	it := todoItem{TaskID: "keep", AltSummary: "only-alt", AltCreateAt: "777"}
	it.Resolve()
	if it.TaskID != "keep" || it.Summary != "only-alt" || it.CreateAt != "777" {
		t.Errorf("partial resolve = %+v", it)
	}
}

func TestTodoItem_Resolve_EmptyBoth(t *testing.T) {
	it := todoItem{}
	it.Resolve()
	if it.TaskID != "" || it.Summary != "" || it.CreateAt != "" {
		t.Errorf("empty item must stay empty, got %+v", it)
	}
}

// ---- helper for unmarshaling a flexibleString number in a canonical field ----

// gopiUnmarshalNumber unmarshals a numeric JSON value into a flexibleString
// and returns it, so callers can assert the Stringer on a coerced value.
func gopiUnmarshalNumber(t *testing.T, num string) flexibleString {
	t.Helper()
	var f flexibleString
	if err := json.Unmarshal([]byte(num), &f); err != nil {
		t.Fatalf("unmarshal %s: %v", num, err)
	}
	return f
}
