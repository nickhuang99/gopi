package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// P2 task dispatcher LLM tool (task_submit): schema shape, argument
// validation, and dispatch against a temp memory.db — the
// memory_tools_test.go pattern (toolDefByName / executeTool seams).
//
// task_evaluate was RETIRED by the reframe+evaluate merge; its judgment
// function is now task(action=reframe) (see reframe_test.go).
// ---------------------------------------------------------------------------

func TestTaskToolDefsRegistered(t *testing.T) {
	for _, name := range []string{toolnames.ToolTaskSubmit} {
		td := toolDefByName(t, name)
		if td == nil {
			t.Fatalf("%s not found in claudeToolDefs", name)
		}
		// 描述即策略 (8/28 lesson): the description must carry the value
		// contrast and routing triggers, not bare feature text.
		lower := strings.ToLower(td.Description)
		for _, want := range []string{"bug", "split", "task_submit"} {
			if !strings.Contains(lower, want) {
				t.Errorf("%s description missing trigger word %q", name, want)
			}
		}
	}
}

func TestTaskToolsValidateArgs(t *testing.T) {
	if msg := validateToolArgs("task_submit", map[string]interface{}{}); !strings.Contains(msg, "summary") {
		t.Errorf("task_submit empty args: got %q, want missing summary", msg)
	}
	if msg := validateToolArgs("task_submit", map[string]interface{}{"summary": "x"}); msg != "" {
		t.Errorf("task_submit valid args rejected: %q", msg)
	}
}

func TestExecuteTaskSubmitEnqueuesPendingWithSessionStamp(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ag := &Agent{}
	ag.autoTaskSessionID = "sess-submitter-1" // test seam: the "current" session id

	out := ag.executeTool(deepseek.ToolCall{
		Name: "task_submit",
		Arguments: map[string]interface{}{
			"summary":        "fix nudge race in loop.go",
			"payload":        "Repro: two goroutines nudge simultaneously...",
			"tags":           []interface{}{"bug", "concurrency"},
			"parent_task_id": "",
		},
	})
	if strings.Contains(out, "task_submit error") {
		t.Fatalf("task_submit failed: %s", out)
	}
	if !strings.Contains(out, "status=pending") || !strings.Contains(out, "source=llm") {
		t.Fatalf("unexpected submit output: %s", out)
	}

	// Row truth: pending + llm + the submitting session stamped
	// (anti-self-reference key).
	tk := toolsTaskByID(t, taskIDFromSubmitOut(t, out))
	if tk.Status != "pending" || tk.Source != "llm" || tk.SessionID != "sess-submitter-1" {
		t.Errorf("row = %+v, want pending/llm/sess-submitter-1", tk)
	}

	// Anti-self-reference: next with the submitting session excludes it.
	res, err := toolsTaskNext("holder", "sess-submitter-1")
	if err != nil {
		t.Fatalf("TaskNext: %v", err)
	}
	if !res.Empty {
		t.Errorf("own-session task must be excluded from claims, got %+v", res.Task)
	}
	// Without the exclusion the same row is claimable.
	res2, err := toolsTaskNext("other-instance", "")
	if err != nil {
		t.Fatalf("TaskNext other: %v", err)
	}
	if res2.Empty || res2.Task.TaskID != tk.TaskID {
		t.Errorf("other instance should claim the task, got empty=%v", res2.Empty)
	}
}

// --- bridges into internal/tools (the agent package must not import the
// tools test helpers, so these wrap the exported/entry functions) ---

func taskIDFromSubmitOut(t *testing.T, out string) string {
	t.Helper()
	// "Task queued: t-<id> (status=pending, ...)"
	i := strings.Index(out, "Task queued: ")
	if i < 0 {
		t.Fatalf("no task id in submit output: %s", out)
	}
	rest := out[i+len("Task queued: "):]
	if j := strings.Index(rest, " "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func toolsTaskByID(t *testing.T, taskID string) *tools.Task {
	t.Helper()
	out, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	tk := &tools.Task{TaskID: taskID}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "status:"):
			tk.Status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		case strings.HasPrefix(line, "source:"):
			tk.Source = strings.TrimSpace(strings.TrimPrefix(line, "source:"))
		case strings.HasPrefix(line, "session_id:"):
			tk.SessionID = strings.TrimSpace(strings.TrimPrefix(line, "session_id:"))
		}
	}
	return tk
}

func toolsTaskNext(holder, excludeSession string) (*tools.TaskNextResult, error) {
	return tools.TaskNext(holder, excludeSession)
}
