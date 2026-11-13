package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Task status constants — the only three valid states.
//
// Three, not two (review 2026-09-07 T2, matching Claude Code/Codex): the
// initial list is a PLAN, so planned-but-not-started work needs a state that
// does not assert it is already being worked. pending = on the list, not
// started; progressing = actively being worked (exactly ONE at a time);
// completed = verified done. IsCompleted/AllCompleted treat pending as
// not-done, so every existing downstream gate keeps working unchanged.
const (
	TaskStatusPending     = "pending"
	TaskStatusProgressing = "progressing"
	TaskStatusCompleted   = "completed"
)

// Marker strings that delimit the task list block in LLM responses.
const (
	TaskListBeginMarker = "[TASK_LIST_BEGIN]"
	TaskListEndMarker   = "[TASK_LIST_END]"
)

// Sentinel errors returned by ParseTaskList when the [TASK_LIST_BEGIN] marker
// is present but no valid [TASK_LIST_END] follows (the partial-parse recovery
// path). A sentinel error (not a separate return value) keeps the existing
// `tasks != nil` downstream contract intact: recovered lists carry non-nil
// tasks, so TaskState.update, R3 fast-exit and the exit protection all treat
// them exactly like a normal list.
var (
	// ErrTaskListRecovered: BEGIN-without-END but a balanced-prefix JSON parse
	// succeeded. tasks is non-nil and valid — treat as a real list + warn.
	ErrTaskListRecovered = errors.New("task list: BEGIN without END — recovered via balanced-prefix parse (LLM likely forgot [TASK_LIST_END])")
	// ErrTaskListUnparseable: BEGIN-without-END whose tail is not a parseable
	// JSON prefix (prose reference, mid-JSON truncation, bare XML). tasks is
	// nil — no garbage list is ever produced; warn that the drop happened.
	ErrTaskListUnparseable = errors.New("task list: BEGIN without END — no parseable JSON prefix (list dropped; log warning)")
)

// FlexibleString is a string field that accepts both JSON strings and numbers.
// The LLM sometimes outputs numeric IDs (e.g. "id": 1 instead of "id": "1"),
// which would cause json.Unmarshal to fail on a plain string field.
type FlexibleString string

func (f *FlexibleString) UnmarshalJSON(b []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = FlexibleString(s)
		return nil
	}
	// Fall back to number (e.g. 1 → "1").
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexibleString(n.String())
		return nil
	}
	return fmt.Errorf("flexibleString: cannot unmarshal %s", string(b))
}

// String returns the underlying string, satisfying fmt.Stringer so a
// FlexibleString renders inline under %s (gopilog's formatter relies on this).
func (f FlexibleString) String() string { return string(f) }

// TaskItem is a single task in the flat task list.
// ID fields use FlexibleString so that both "1" and 1 are accepted.
// Both canonical (task_id, summary, create_at) and alternate (id, content) field
// names are supported via the resolve() method.
type TaskItem struct {
	TaskID   FlexibleString `json:"task_id"`
	Summary  string         `json:"summary"`
	CreateAt FlexibleString `json:"create_at"` // Unix timestamp as string
	Status   string         `json:"status"`

	// Fallback fields for LLMs that use alternate naming (e.g. "id" instead
	// of "task_id", "content" instead of "summary").
	AltID       FlexibleString `json:"id"`
	AltSummary  string         `json:"content"`
	AltCreateAt FlexibleString `json:"created_at"`
}

// Resolve fills canonical fields from alternate names when the canonical
// field is empty. This provides backward compatibility with LLMs that use
// different JSON field names. Exported so gopilog (which aliases TaskItem for
// render-time parsing) can apply the same alt-name fallback.
func (t *TaskItem) Resolve() {
	if t.TaskID == "" && t.AltID != "" {
		t.TaskID = t.AltID
	}
	if t.Summary == "" && t.AltSummary != "" {
		t.Summary = t.AltSummary
	}
	if t.CreateAt == "" && t.AltCreateAt != "" {
		t.CreateAt = t.AltCreateAt
	}
}

func (t TaskItem) IsCompleted() bool { return t.Status == TaskStatusCompleted }

// IsProgressing returns true if the task is in progress.
func (t TaskItem) IsProgressing() bool { return t.Status == TaskStatusProgressing }

// IsPending returns true if the task is on the list but not yet started.
func (t TaskItem) IsPending() bool { return t.Status == TaskStatusPending }

// ParseTaskList extracts a task list from an LLM response text.
//
// It finds the LAST occurrence of [TASK_LIST_BEGIN]...[TASK_LIST_END] markers,
// extracts the JSON between them, and unmarshals into []TaskItem.
//
// Returns nil, nil if no markers are found (valid for non-task responses).
// Returns an error if markers exist but JSON is malformed.
//
// Recovery path (P0 — devdoc/tasklist-p0-evaluation.md): when [TASK_LIST_BEGIN]
// is present but [TASK_LIST_END] is absent or precedes it, the tail is parsed
// with balanced-prefix JSON detection: find the first '[' or '{' after BEGIN,
// scan string-aware (skipping brackets inside string literals) to its matching
// close, and json.Unmarshal that exact prefix.
//   - recovery succeeds → returns (tasks, ErrTaskListRecovered). tasks is
//     non-nil and flows through every normal downstream path (TaskState.update,
//     R3 fast-exit, exit protection) exactly like a closed-block list — it is
//     not second-class. The caller logs the warning (observability).
//   - no parseable prefix (prose reference / mid-JSON truncation / bare XML)
//     → returns (nil, ErrTaskListUnparseable). No garbage list is ever produced.
func ParseTaskList(response string) ([]TaskItem, error) {
	beginIdx := strings.LastIndex(response, TaskListBeginMarker)
	if beginIdx < 0 {
		return nil, nil
	}
	endIdx := strings.LastIndex(response, TaskListEndMarker)
	tail := response[beginIdx+len(TaskListBeginMarker):]
	if endIdx < 0 || endIdx <= beginIdx {
		// BEGIN exists but no valid END after it → balanced-prefix recovery.
		return ParseTaskListBalancedPrefix(tail)
	}

	// Extract text between markers.
	jsonStr := response[beginIdx+len(TaskListBeginMarker) : endIdx]
	jsonStr = strings.TrimSpace(jsonStr)

	if jsonStr == "" {
		return nil, nil
	}

	var tasks []TaskItem
	if err := json.Unmarshal([]byte(jsonStr), &tasks); err != nil {
		return nil, fmt.Errorf("task list JSON parse error: %w", err)
	}

	// Resolve alternate field names (id→task_id, content→summary, etc.)
	for i := range tasks {
		tasks[i].Resolve()
	}

	return tasks, nil
}

// ParseTaskListBalancedPrefix attempts to recover a task list from the text
// that follows a [TASK_LIST_BEGIN] marker when no [TASK_LIST_END] is present.
//
// Strategy (evaluated 23/23 = 100% recovery on real sessions, devdoc
// tasklist-p0-evaluation.md Q3): find the FIRST '[' or '{' in the tail (the
// start of the JSON array or object), then walk forward tracking bracket
// depth while skipping over string literals (so brackets inside summaries or
// quoted text do not unbalance the scan). When depth returns to zero, the
// prefix [0:closeIdx] is a candidate balanced JSON blob; json.Unmarshal it.
//
// This is deliberately NOT blind "BEGIN-to-EOF": trailing prose/XML pasted
// after a completed list (e.g. a `</parameter></invoke>` tool block followed
// by a list) must be stripped, which balanced-prefix does and to_EOF cannot.
// A parse of the empty/invalid prefix fails cleanly and returns the
// Unparseable sentinel — no garbage list is ever assembled.
//
// Exported because it is part of the shared task-list contract: gopilog's
// CLI display layer (internal/gopilog/formatter.go, `gopi session log --todo`) uses
// it to recover the same BEGIN-without-END blocks the agent-side ParseTaskList
// recovers, so the two layers can never diverge again (devdoc
// tasklist-cli-todo-fix.md). Success returns (tasks, ErrTaskListRecovered) —
// callers must treat ErrTaskListRecovered as a recovery, not a failure.
func ParseTaskListBalancedPrefix(tail string) ([]TaskItem, error) {
	// Skip leading whitespace/newlines after the BEGIN marker.
	i := 0
	for i < len(tail) && (tail[i] == ' ' || tail[i] == '\t' || tail[i] == '\n' || tail[i] == '\r') {
		i++
	}
	// The first non-space char must be an array '[' or object '{'.
	if i >= len(tail) || (tail[i] != '[' && tail[i] != '{') {
		return nil, ErrTaskListUnparseable
	}

	open, close := byte('['), byte(']')
	if tail[i] == '{' {
		open, close = '{', '}'
	}

	depth := 0
	inStr := false
	escaped := false
	for j := i; j < len(tail); j++ {
		c := tail[j]
		if inStr {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				prefix := tail[i : j+1]
				var tasks []TaskItem
				if err := json.Unmarshal([]byte(prefix), &tasks); err != nil {
					// Closed-balance but still not a valid task list (e.g. a
					// JSON string, number, object, or truncated-then-closed
					// array with no valid items). Reject, never fabricate.
					return nil, ErrTaskListUnparseable
				}
				for k := range tasks {
					tasks[k].Resolve()
				}
				return tasks, ErrTaskListRecovered
			}
		}
	}

	// Reached end of tail without closing the bracket — mid-JSON truncation.
	return nil, ErrTaskListUnparseable
}

// contentWithoutTaskList strips the [TASK_LIST_BEGIN]...[TASK_LIST_END]
// block (and anything after it) from a response, so "is the answer
// substantive" checks measure the answer, not the bookkeeping (R3 exit
// logic). Responses without markers are returned trimmed unchanged.
func contentWithoutTaskList(content string) string {
	if i := strings.Index(content, TaskListBeginMarker); i >= 0 {
		return strings.TrimSpace(content[:i])
	}
	return strings.TrimSpace(content)
}

// ValidateTaskList checks the new task list against the previous one.
//
// Rules enforced:
//   - Every task_id from previous MUST appear in current (no deletion)
//   - Every task MUST have status "pending", "progressing" or "completed"
//   - No duplicate task_ids
//   - No empty task_ids
//
// Returns nil if valid, or an error describing the first violation found.
func ValidateTaskList(previous, current []TaskItem) error {
	// Check current list for empty IDs, duplicates, and invalid statuses.
	seen := make(map[string]bool)
	for _, t := range current {
		if t.TaskID == "" {
			return fmt.Errorf("task list validation: empty task_id")
		}
		if !isValidTaskStatus(t.Status) {
			return fmt.Errorf("task list validation: invalid status %q for task %s", t.Status, string(t.TaskID))
		}
		if seen[string(t.TaskID)] {
			return fmt.Errorf("task list validation: duplicate task_id %q", string(t.TaskID))
		}
		seen[string(t.TaskID)] = true
	}

	// Check no task IDs were deleted from previous.
	for _, p := range previous {
		if !seen[string(p.TaskID)] {
			return fmt.Errorf("task list validation: task %q was deleted (all previous task_ids must be preserved)", string(p.TaskID))
		}
	}

	return nil
}

// AllCompleted returns true when every task has status "completed".
// Returns false for an empty list (no tasks = not done yet). A pending task
// is not completed, so a list still holding planned work never reads done.
func AllCompleted(tasks []TaskItem) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if t.Status != TaskStatusCompleted {
			return false
		}
	}
	return true
}

// isValidTaskStatus reports whether s is one of the three vocabulary states.
func isValidTaskStatus(s string) bool {
	return s == TaskStatusPending || s == TaskStatusProgressing || s == TaskStatusCompleted
}

// CountStatus returns the number of tasks with exactly the given status.
func CountStatus(tasks []TaskItem, status string) int {
	count := 0
	for _, t := range tasks {
		if t.Status == status {
			count++
		}
	}
	return count
}

// CountProgressing returns the number of tasks with status "progressing".
func CountProgressing(tasks []TaskItem) int { return CountStatus(tasks, TaskStatusProgressing) }

// CountPending returns the number of tasks with status "pending" — on the
// list but not yet started.
func CountPending(tasks []TaskItem) int { return CountStatus(tasks, TaskStatusPending) }

// FormatTaskList serializes a task list back to the JSON format for heartbeat
// reminders. The output includes the markers.
func FormatTaskList(tasks []TaskItem) string {
	b, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return TaskListBeginMarker + "\n[]\n" + TaskListEndMarker
	}
	return TaskListBeginMarker + "\n" + string(b) + "\n" + TaskListEndMarker
}

// EqualTaskLists returns true if two task lists have the same task IDs and
// statuses. Used by heartbeat to detect "no change" across rounds.
func EqualTaskLists(a, b []TaskItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].TaskID != b[i].TaskID || a[i].Status != b[i].Status {
			return false
		}
	}
	return true
}
