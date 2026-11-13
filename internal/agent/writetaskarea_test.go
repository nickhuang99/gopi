package agent

import (
	"github.com/nick/gopi/internal/toolnames"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/tools"
)

// tempAgentMemoryDB points the memory DB env at a throwaway file.
func tempAgentMemoryDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-gopi.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	return path
}

// commitViaAgent writes a real session conclusion through the tools layer.
func commitViaAgent(path, sessionID, taskID, conclusion string) error {
	_ = os.Getenv("GOPI_MEMORY_DB") // keep env write visible to the tools pkg
	_, err := tools.WriteTaskArea(tools.WriteTaskAreaOptions{
		SessionID:  sessionID,
		TaskID:     taskID,
		Status:     "completed",
		Conclusion: conclusion,
	})
	return err
}

// newTaskAreaTestAgent builds an Agent whose session id points at a temp
// memory DB (via env) so write_task_area persistence is real but isolated.
func newTaskAreaTestAgent(t *testing.T, sessionID string) *Agent {
	t.Helper()
	// newTestAgentEnv isolates GOPI_HOME/GOPI_SETTINGS/API-key/raw-log to a
	// temp dir — without it NewAgent("test-model") opens a real SessionLogger
	// under the developer's ~/.gopi and every one of these tests leaks a
	// session_start-only junk file into `gopi session log --last N`.
	newTestAgentEnv(t)
	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	ag.autoTaskSessionID = sessionID
	ag.Ctx = &Context{}
	return ag
}

// addAnalysisWorkloads simulates the working region before a commit round:
// the epic briefing, then an item's analysis round (assistant + tool result),
// then the commit round (assistant with a write_task_area call).
func addAnalysisWorkloads(ag *Agent) {
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL] solve five items"})
	// item 1 analysis: assistant calls code_search, tool returns, then text.
	ag.Ctx.AddAssistantMessage("analyzing item 1", []deepseek.ToolCall{
		{ID: "call-analysis-1", Name: "code_search", Arguments: map[string]interface{}{"symbol": "X"}},
	})
	ag.Ctx.AddToolResult("call-analysis-1", "some symbol found")
	// commit round
	ag.Ctx.AddAssistantMessage("committing item 1", []deepseek.ToolCall{
		{ID: "call-commit-1", Name: toolnames.ToolTask, Arguments: map[string]interface{}{"action": "commit", "task_id": "1", "status": "completed", "conclusion": "item1: resolved"}},
	})
	ag.Ctx.AddToolResult("call-commit-1", "committed subtask \"1\"")
}

func TestTrimCommittedTaskArea_DropsAnalysisKeepsEpicAndCommit(t *testing.T) {
	ag := newTaskAreaTestAgent(t, "sess-trim-test")
	addAnalysisWorkloads(ag)

	before := len(ag.Ctx.Messages)
	if before < 5 {
		t.Fatalf("test setup: want >=5 messages, got %d", before)
	}
	ag.trimCommittedTaskArea()

	msgs := ag.Ctx.Messages
	// Epic (index 0) must survive.
	if len(msgs) == 0 || !strings.Contains(msgs[0].Content, "EPIC GOAL") {
		t.Fatalf("epic briefing dropped: %+v", msgs)
	}
	// The committed item's analysis round must be gone...
	for _, m := range msgs {
		if strings.Contains(m.Content, "analyzing item 1") || strings.Contains(m.Content, "some symbol found") {
			t.Errorf("committed analysis region survived trim: %+v", msgs)
		}
	}
	// ...while the commit round (its own results) stays.
	foundCommit := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "committing item 1") || strings.Contains(m.Content, "committed subtask") {
			foundCommit = true
		}
	}
	if !foundCommit {
		t.Errorf("commit round dropped after trim: %+v", msgs)
	}
	if len(msgs) >= before {
		t.Errorf("trim did not reduce messages: %d -> %d", before, len(msgs))
	}
}

func TestTrimCommittedTaskArea_KeepsLaterLiveWork(t *testing.T) {
	ag := newTaskAreaTestAgent(t, "sess-trim-live")
	addAnalysisWorkloads(ag)
	// Live work on the NEXT item that must never be trimmed.
	ag.Ctx.AddAssistantMessage("analyzing item 2 now", []deepseek.ToolCall{
		{ID: "call-live", Name: "read", Arguments: map[string]interface{}{"path": "/x"}},
	})
	ag.Ctx.AddToolResult("call-live", "content of x")

	ag.trimCommittedTaskArea()

	var sawLive bool
	for _, m := range ag.Ctx.Messages {
		if strings.Contains(m.Content, "analyzing item 2 now") || strings.Contains(m.Content, "content of x") {
			sawLive = true
		}
	}
	if !sawLive {
		t.Errorf("live tail after the commit round was trimmed: %+v", ag.Ctx.Messages)
	}
	// Orphan check: head after trim must not be a lone tool result.
	if len(ag.Ctx.Messages) > 1 && ag.Ctx.Messages[1].Role == "tool" {
		t.Errorf("orphaned tool result at head after trim: %+v", ag.Ctx.Messages)
	}
}

func TestInjectTaskAreaConclusions_CarriesRecordedFacts(t *testing.T) {
	tempAgentMemoryDB(t)
	ag := newTaskAreaTestAgent(t, "sess-inject")
	// Commit two conclusions for real (DB-backed).
	if err := commitViaAgent("", "sess-inject", "1", "item one conclusion fact"); err != nil {
		t.Fatalf("commit 1: %v", err)
	}
	if err := commitViaAgent("", "sess-inject", "2", "item two conclusion fact"); err != nil {
		t.Fatalf("commit 2: %v", err)
	}

	ag.taskAreaDirty = true
	ag.injectTaskAreaConclusions()

	// The injected block is the newest user message carrying the marker and
	// both facts — the machine-trusted carry-forward.
	if len(ag.Ctx.Messages) == 0 {
		t.Fatal("no messages after inject")
	}
	last := ag.Ctx.Messages[len(ag.Ctx.Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, taskAreaConclusionsMessageMarker) {
		t.Fatalf("carry-forward block not injected as user message: %+v", last)
	}
	if !strings.Contains(last.Content, "item one conclusion fact") ||
		!strings.Contains(last.Content, "item two conclusion fact") {
		t.Errorf("carry-forward block missing recorded facts: %q", last.Content)
	}
}
