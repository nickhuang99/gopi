package agent

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// countUserMessages returns how many user-role messages exactly match the
// given content in the context (via GetMessages(), which prepends the system
// prompt when set — the injected checkpoint is a plain user message).
func countUserMessages(a *Agent, content string) int {
	n := 0
	for _, m := range a.Ctx.GetMessages() {
		if m.Role == "user" && m.Content == content {
			n++
		}
	}
	return n
}

// TestCompletionCheckpoint_ForcesOneStore verifies the harness-side task-end
// memory_store enforcement (对称强制 with task-start memory_search): at the
// all-completed completion point, if the run has never stored, exactly one
// checkpoint message is injected and maybeEnforceCompletionStore returns true
// (so the caller continues instead of exiting). A second call is a no-op.
func TestCompletionCheckpoint_ForcesOneStore(t *testing.T) {
	a := &Agent{Ctx: &Context{}, Logger: nil}

	if a.memoryStored {
		t.Fatal("fresh agent must start with memoryStored=false")
	}

	// First call: no store yet → must inject and signal continue.
	if !a.maybeEnforceCompletionStore() {
		t.Fatal("expected maybeEnforceCompletionStore=true on first call (run not stored)")
	}
	if !a.completionStorePrompted {
		t.Fatal("completionStorePrompted must be latched after the first call")
	}
	if got := countUserMessages(a, completionCheckpointMessage); got != 1 {
		t.Fatalf("expected 1 injected checkpoint message, got %d", got)
	}

	// Second call: latch is set → must NOT inject again.
	if a.maybeEnforceCompletionStore() {
		t.Fatal("maybeEnforceCompletionStore must be a no-op once prompted")
	}
	if got := countUserMessages(a, completionCheckpointMessage); got != 1 {
		t.Fatalf("checkpoint must be injected exactly once, got %d", got)
	}
}

// TestCompletionCheckpoint_SkippedWhenStored verifies that once a real
// memory_store succeeded (memoryStored=true), the completion checkpoint never
// fires — the model already honored the task-end store.
func TestCompletionCheckpoint_SkippedWhenStored(t *testing.T) {
	a := &Agent{Ctx: &Context{}, Logger: nil, memoryStored: true}

	if a.maybeEnforceCompletionStore() {
		t.Fatal("must NOT force a store when the run already stored")
	}
	if a.completionStorePrompted {
		t.Fatal("completionStorePrompted must stay false when store already done")
	}
	if got := countUserMessages(a, completionCheckpointMessage); got != 0 {
		t.Fatalf("no checkpoint message should be injected when store already done, got %d", got)
	}
}

// TestMemoryStore_SuccessLatchesMemoryStored exercises the real dispatch:
// a genuine memory_store call against an isolated temp DB (GOPI_MEMORY_DB)
// must latch memoryStored=true, so the completion checkpoint will not force a
// redundant store. This is the harness half that closes the REPEAT-TEST gap:
// a store that actually lands (not a validation error) satisfies the
// task-end requirement.
func TestMemoryStore_SuccessLatchesMemoryStored(t *testing.T) {
	dbPath := t.TempDir() + "/test-memory.db"
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	a := &Agent{Ctx: &Context{}, Logger: nil}
	if a.memoryStored {
		t.Fatal("initial memoryStored should be false")
	}

	// A well-formed, distilled store via the real dispatch path.
	res := a.executeToolDefault(deepseek.ToolCall{
		ID:   "tc1",
		Name: toolnames.ToolMemory,
		Arguments: map[string]interface{}{
			"action":   toolnames.ToolMemoryActionStore,
			"topic":    "test task-end store latch",
			"question": "does a successful memory_store set memoryStored?",
			"answer":   "Yes: a distilled store lands and latches memoryStored so the completion checkpoint won't double-store.",
		},
	})
	if strings.Contains(res, "error") {
		t.Fatalf("unexpected store error: %s", res)
	}
	if !a.memoryStored {
		t.Fatal("successful memory_store must latch memoryStored=true")
	}
	// Completion checkpoint must now be a no-op.
	if a.maybeEnforceCompletionStore() {
		t.Fatal("must not force a store once one already succeeded")
	}
}

// TestMemoryStore_ErrorDoesNotLatch verifies a rejected store (garbage /
// missing content) leaves memoryStored=false, so the completion checkpoint
// can still fire and the run's task-end store requirement is not falsely
// satisfied.
func TestMemoryStore_ErrorDoesNotLatch(t *testing.T) {
	a := &Agent{Ctx: &Context{}, Logger: nil}
	res := a.executeToolDefault(deepseek.ToolCall{
		ID:        "tc1",
		Name:      toolnames.ToolMemory,
		Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionStore}, // empty → validation error
	})
	if !strings.Contains(res, "Missing required parameter") {
		t.Fatalf("expected a validation-error result, got: %s", res)
	}
	if a.memoryStored {
		t.Fatal("an errored memory_store must NOT latch memoryStored")
	}
}

// TestCompletionCheckpointMessage_RequiresDistilledStore guards the wording
// of the injected checkpoint so it steers the model toward distilled
// experience (not raw logs) and toward a single store before the final reply.
func TestCompletionCheckpointMessage_RequiresDistilledStore(t *testing.T) {
	for _, want := range []string{
		"call memory(action=store)",
		"exactly once",
		"knowledge gain",
		"skill gain",
		"lessons learned",
		"pitfalls avoided",
		"success stories",
		"setbacks",
		"not raw logs",
	} {
		if !strings.Contains(completionCheckpointMessage, want) {
			t.Errorf("completionCheckpointMessage missing %q", want)
		}
	}
}
