package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
)

// TestAgent_Run_TerseCompletedAnswer_StillEnforcesStore reproduces the
// task-end store bypass: a run that completes its task list but whose final
// answer is TERSER than minFinalAnswerChars (40) — e.g. a smoke test that
// replies "SMOKE-REMOVE-OK" — exits through the maxConsecutiveEndTurnRounds
// path (second exit), which historically NEVER called
// maybeEnforceCompletionStore(). The completion checkpoint must fire there
// too, so the run cannot end without a memory_store.
func TestAgent_Run_TerseCompletedAnswer_StillEnforcesStore(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		// Always reply with a completed task list + a terse answer
		// ("SMOKE-REMOVE-OK" is 14 chars < minFinalAnswerChars=40).
		terse := "SMOKE-REMOVE-OK\n" +
			"[TASK_LIST_BEGIN]\n" +
			`[{"task_id":"1","summary":"Call memory_search","status":"completed"},{"task_id":"2","summary":"Call memory_feedback","status":"completed"}]` + "\n" +
			"[TASK_LIST_END]"
		claudeTextStream(w, terse, "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL: server.URL,
			APIKey:  "test-key",
			HTTPClient: &http.Client{
				Transport: &http.Transport{},
			},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}

	result, err := ag.Run("Run the smoke sequence")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "SMOKE-REMOVE-OK") {
		t.Fatalf("expected the terse answer, got: %q", result)
	}
	// The terse completed answer must NOT bypass the task-end store: the
	// completion checkpoint must have been injected (forcing the model to
	// call memory_store before the final response).
	if !ag.completionStorePrompted {
		t.Fatalf("terse completed answer exited WITHOUT the completion checkpoint — "+
			"task-end memory_store was bypassed (callCount=%d)", callCount)
	}
	if n := countUserMessages(ag, completionCheckpointMessage); n != 1 {
		t.Fatalf("expected the completion checkpoint injected exactly once, got %d", n)
	}
}

// TestAgent_Run_TerseCompletedAnswer_ToolRoundThenExit mirrors the exact
// session pattern from the search-remove live smoke (f15da5e3): round 1 does
// memory_search + memory_feedback, round 2 answers tersely with a completed
// task list, round 3 confirms. The run must still force the store.
func TestAgent_Run_TerseCompletedAnswer_ToolRoundThenExit(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callCount {
		case 1:
			// Round 1: memory_search + memory_feedback tool calls.
			claudeToolUseStream(w, toolnames.ToolMemory, map[string]interface{}{
				"action": toolnames.ToolMemoryActionSearch, "query": "smoke test",
				"limit": 3,
			})
		case 2:
			claudeToolUseStream(w, toolnames.ToolMemory, map[string]interface{}{
				"action": toolnames.ToolMemoryActionFeedback, "id": 3,
				"rating": "useless",
				"reason": "smoke test",
			})
		case 3:
			// Round 3: terse completed answer.
			terse := "SMOKE-REMOVE-OK\n" +
				"[TASK_LIST_BEGIN]\n" +
				`[{"task_id":"1","summary":"Call memory_search","status":"completed"},{"task_id":"2","summary":"Pick least-useful hit","status":"completed"}]` + "\n" +
				"[TASK_LIST_END]"
			claudeTextStream(w, terse, "end_turn")
		default:
			// Subsequent rounds (checkpoint round + final) keep answering
			// the completed-terse response so the loop can terminate.
			terse := "SMOKE-REMOVE-OK\n" +
				"[TASK_LIST_BEGIN]\n" +
				`[{"task_id":"1","summary":"Call memory_search","status":"completed"},{"task_id":"2","summary":"Pick least-useful hit","status":"completed"}]` + "\n" +
				"[TASK_LIST_END]"
			claudeTextStream(w, terse, "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL: server.URL,
			APIKey:  "test-key",
			HTTPClient: &http.Client{
				Transport: &http.Transport{},
			},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 8,
	}

	result, err := ag.Run("Run the smoke sequence")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "SMOKE-REMOVE-OK") {
		t.Fatalf("expected the terse answer, got: %q", result)
	}
	if !ag.completionStorePrompted {
		t.Fatalf("completed-but-terse run exited without the completion checkpoint — task-end memory_store bypassed")
	}
}
