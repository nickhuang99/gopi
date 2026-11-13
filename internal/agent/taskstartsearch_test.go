package agent

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
)

// TestTaskStartCheckpoint_FiresBeforeFirstRound verifies the harness-side
// task-start memory_search enforcement (对称强制 with task-end completion
// store): a fresh run gets exactly one taskStartSearchMessage injected before
// the first round, and maybeInjectTaskStartSearch returns true so the caller
// knows the checkpoint landed. A second call is a no-op (one-shot latch).
func TestTaskStartCheckpoint_FiresBeforeFirstRound(t *testing.T) {
	a := &Agent{Ctx: &Context{}, Logger: nil}

	if a.memorySearched {
		t.Fatal("fresh agent must start with memorySearched=false")
	}

	// First call: no search yet → must inject and signal continue.
	if !a.maybeInjectTaskStartSearch() {
		t.Fatal("expected maybeInjectTaskStartSearch=true on first call (run not searched)")
	}
	if !a.taskStartSearchPrompted {
		t.Fatal("taskStartSearchPrompted must be latched after the first call")
	}
	if got := countUserMessages(a, taskStartSearchMessage); got != 1 {
		t.Fatalf("expected 1 injected task-start message, got %d", got)
	}

	// Second call: latch is set → must NOT inject again.
	if a.maybeInjectTaskStartSearch() {
		t.Fatal("maybeInjectTaskStartSearch must be a no-op once prompted")
	}
	if got := countUserMessages(a, taskStartSearchMessage); got != 1 {
		t.Fatalf("task-start checkpoint must be injected exactly once, got %d", got)
	}
}

// TestTaskStartCheckpoint_SkippedWhenSearched verifies that once a real
// memory_search succeeded (memorySearched=true), the task-start checkpoint
// never fires — the model already honored the task-start search.
func TestTaskStartCheckpoint_SkippedWhenSearched(t *testing.T) {
	a := &Agent{Ctx: &Context{}, Logger: nil, memorySearched: true}

	if a.maybeInjectTaskStartSearch() {
		t.Fatal("must NOT force a search when the run already searched")
	}
	if a.taskStartSearchPrompted {
		t.Fatal("taskStartSearchPrompted must stay false when search already done")
	}
	if got := countUserMessages(a, taskStartSearchMessage); got != 0 {
		t.Fatalf("no task-start message should be injected when search already done, got %d", got)
	}
}

// TestTaskStartCheckpoint_DisabledByNoEnforceMemory verifies the A/B switch:
// --no-enforce-memory (GOPI_NO_ENFORCE_MEMORY) disables BOTH harness memory
// checkpoints — the task-start search injection AND the task-end completion
// store — so a run depends purely on prompt persuasion (isolated A/B arm).
func TestTaskStartCheckpoint_DisabledByNoEnforceMemory(t *testing.T) {
	a := &Agent{Ctx: &Context{}, Logger: nil, NoEnforceMemory: true}

	if a.maybeInjectTaskStartSearch() {
		t.Fatal("must NOT inject the task-start checkpoint with enforcement disabled")
	}
	if a.taskStartSearchPrompted {
		t.Fatal("taskStartSearchPrompted must stay false with enforcement disabled")
	}
	if got := countUserMessages(a, taskStartSearchMessage); got != 0 {
		t.Fatalf("no task-start message expected with enforcement disabled, got %d", got)
	}
	if a.maybeEnforceCompletionStore() {
		t.Fatal("completion store checkpoint must also be disabled by NoEnforceMemory")
	}
	if got := countUserMessages(a, completionCheckpointMessage); got != 0 {
		t.Fatalf("no completion checkpoint expected with enforcement disabled, got %d", got)
	}
}

// TestMemorySearch_SuccessLatchesMemorySearched exercises the real dispatch:
// a genuine memory_search call against an isolated temp DB (GOPI_MEMORY_DB)
// must latch memorySearched=true, so the task-start checkpoint will not force
// a redundant search. This is the harness half that closes the loop ENTRY
// gap (对称 with TestMemoryStore_SuccessLatchesMemoryStored).
func TestMemorySearch_SuccessLatchesMemorySearched(t *testing.T) {
	dbPath := t.TempDir() + "/test-memory.db"
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	a := &Agent{Ctx: &Context{}, Logger: nil}
	if a.memorySearched {
		t.Fatal("initial memorySearched should be false")
	}

	res := a.executeToolDefault(deepseek.ToolCall{
		ID:   "tc1",
		Name: toolnames.ToolMemory,
		Arguments: map[string]interface{}{
			"action": toolnames.ToolMemoryActionSearch, "query": "test task-start search latch",
		},
	})
	if strings.Contains(res, "error") {
		t.Fatalf("unexpected search error: %s", res)
	}
	if !a.memorySearched {
		t.Fatal("successful memory_search must latch memorySearched=true")
	}
	// Task-start checkpoint must now be a no-op.
	if a.maybeInjectTaskStartSearch() {
		t.Fatal("must not force a search once one already succeeded")
	}
}

// TestMemorySearch_ErrorDoesNotLatch verifies a failed search (broken DB)
// leaves memorySearched=false, so the task-start checkpoint can still fire
// and the run's task-start search requirement is not falsely satisfied.
func TestMemorySearch_ErrorDoesNotLatch(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", "/nonexistent-dir-zzz/memory.db")

	a := &Agent{Ctx: &Context{}, Logger: nil}
	res := a.executeToolDefault(deepseek.ToolCall{
		ID:        "tc1",
		Name:      toolnames.ToolMemory,
		Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "x"},
	})
	if !strings.Contains(res, "memory error:") {
		t.Fatalf("expected a search-error result, got: %s", res)
	}
	if a.memorySearched {
		t.Fatal("an errored memory_search must NOT latch memorySearched")
	}
}

// TestTaskStartCheckpointMessage_RequiresCoreTopicSearch guards the wording
// of the injected checkpoint so it steers the model toward ONE search on the
// task's core topic before planning — and keeps the 取不到→memory_store contract
// with the task-end store symmetric to it.
func TestTaskStartCheckpointMessage_RequiresCoreTopicSearch(t *testing.T) {
	for _, want := range []string{
		"call memory(action=search) once",
		"core topic",
		"past experience",
		"取不到→memory(action=store)",
		"Before planning",
		"CONTRADICT",
		"memory(action=update)",
	} {
		if !strings.Contains(taskStartSearchMessage, want) {
			t.Errorf("taskStartSearchMessage missing %q", want)
		}
	}
}

// TestTaskStartCheckpointMessage_DeadEndTrimAnchor guards the context_trim
// priming sentence: the checkpoint must teach the model to call context_trim
// the moment a tool result is a dead-end, before it moves on. Placed in the
// harness-injected checkpoint (the strongest persuasion surface — model must
// read it), not just the tool description (which flash models ignore).
func TestTaskStartCheckpointMessage_DeadEndTrimAnchor(t *testing.T) {
	for _, want := range []string{
		"moment a tool result is a dead-end",
		"call context_trim",
		"one-line conclusion",
		"clean worktable",
	} {
		if !strings.Contains(taskStartSearchMessage, want) {
			t.Errorf("taskStartSearchMessage missing dead-end context_trim priming %q", want)
		}
	}
}
