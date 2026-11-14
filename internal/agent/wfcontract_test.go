package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// wfContractEpicMsg returns the first user message whose content carries the
// [EPIC GOAL] framing (request 1 of any Run).
func wfContractEpicMsg(t *testing.T, req *deepseek.ClaudeRequest) string {
	t.Helper()
	for _, m := range req.Messages {
		if m.Role == "user" {
			for _, b := range m.Content {
				if b.Type == "text" && strings.HasPrefix(b.Text, "[EPIC GOAL]") {
					return b.Text
				}
			}
		}
	}
	t.Fatal("no [EPIC GOAL] user message in request")
	return ""
}

// TestEmissionContract_SingleSource verifies the P1 contract unification: the
// establishment deadline ("on or before your second tool round") is stated by
// ONE constant interpolated into BOTH the taskManagementRules block and the
// Epic framing, and the old ambiguous "first substantive round" phrasing that
// competed with rule 2's omit-cadence is gone from both.
func TestEmissionContract_SingleSource(t *testing.T) {
	if !strings.Contains(taskManagementRules(), emissionContract) {
		t.Fatal("taskManagementRules must interpolate emissionContract verbatim")
	}
	if strings.Contains(taskManagementRules(), "first substantive round") {
		t.Fatal("rule 1 must not resurrect the ambiguous 'first substantive round' deadline")
	}
	if !strings.Contains(taskManagementRules(), "PLAN (not receipt)") {
		// RE-ALIGNED by f8a35246: commit a4181498 ("feat(agent): self-knowledge
		// prompt section") reworded rule 1 to "The list is the PLAN: %s", which
		// dropped this phrase — and with it the plan-NOT-RECEIPT semantics a
		// completed list must never be read as — from every prompt the model
		// receives. f8a35246 restored the framing in internal/agent/loop.go's
		// taskManagementRules(), so this substring grades the very text
		// interpolated into the prompt. The assertion is UNCHANGED: a gated
		// phrase is restored at its source, never re-pointed at a weaker one.
		t.Fatal("rule 1 must keep the plan-not-receipt framing")
	}
}

// TestEmissionContract_InEpicFraming runs a real Run and checks the Epic
// framing message the model actually receives: same contract constant, no
// "first substantive round" ambiguity.
func TestEmissionContract_InEpicFraming(t *testing.T) {
	var req1 *deepseek.ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded deepseek.ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&decoded) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		if req1 == nil {
			req1 = &decoded
		}
		claudeTextStream(w, "Working on it.", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client:        newWfContractClient(server.URL),
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 2,
	}
	if _, err := ag.Run("Fix the bug"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	epic := wfContractEpicMsg(t, req1)
	if !strings.Contains(epic, emissionContract) {
		t.Fatalf("Epic framing must interpolate emissionContract verbatim, got: %q", epic)
	}
	if strings.Contains(epic, "first substantive round") {
		t.Fatalf("Epic framing must not contain the ambiguous deadline, got: %q", epic)
	}
}

// TestR3TrustedCompletion_FromTrackedState pins the P1.5 decoupling: a
// completed run whose FINAL answer omits the task list (contract-blessed —
// the list is harness state, not a receipt) still gets the R3 fast-exit,
// because trustedCompletion() reads the tracked all-completed state. The
// last assistant message must NOT carry a task-list block (the RC2
// plan-tailing incentive is gone).
func TestR3TrustedCompletion_FromTrackedState(t *testing.T) {
	callCount := 0
	var lastAssistant strings.Builder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var decoded deepseek.ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&decoded) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		if la := claudeLastAssistant(&decoded); la != nil {
			for _, b := range la.Content {
				lastAssistant.WriteString(b.Text)
			}
		}
		if callCount == 1 {
			claudeToolUseStream(w, "task", map[string]interface{}{"action": "view"})
			return
		}
		claudeTextStream(w, "All five tasks finished and verified; summary of the work follows in full detail.", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client:        newWfContractClient(server.URL),
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		// Skip the task-end store round: this test pins the R3 exit
		// decision, not the memory-store enforcement.
		NoEnforceMemory: true,
		// Harness state: list established earlier in the session and now
		// all completed (as tracked when it was emitted mid-run).
		TaskState: completedTodoState(t),
	}
	result, err := ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 API calls (P1.5 trusted completion from tracked state), got %d", callCount)
	}
	if !strings.Contains(result, "All five tasks finished") {
		t.Fatalf("expected the substantive final answer, got: %q", result)
	}
	if strings.Contains(lastAssistant.String(), TaskListBeginMarker) {
		t.Fatalf("final assistant message must NOT re-emit the list (receipt emission is the RC2 bug), got: %q", lastAssistant.String())
	}
}

// TestR3TrustedCompletion_NoListStillUntrusted guards the flip side: with no
// list ever established (hasList==false) the P1.5 shortcut must NOT fire —
// the bug022 confirmation round stays, so a list-less run cannot claim
// completion unchallenged.
func TestR3TrustedCompletion_NoListStillUntrusted(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			claudeToolUseStream(w, "task", map[string]interface{}{"action": "view"})
			return
		}
		if callCount == 2 {
			claudeTextStream(w, "Everything is done, verified end to end, nothing further remains at all.", "end_turn")
			return
		}
		claudeTextStream(w, "Confirmed.", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client:          newWfContractClient(server.URL),
		Ctx:             &Context{},
		Model:           "deepseek-v4-pro",
		MaxIterations:   5,
		NoEnforceMemory: true,
		TaskState:       newTodoState(),
	}
	if _, err := ag.Run("Fix the bug"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 API calls (no tracked list → bug022 confirmation round stays), got %d", callCount)
	}
}

// TestTrustedCompletion_Table unit-covers the decoupled signal: nil state,
// no list, mid-run list, and tracked all-completed.
func TestTrustedCompletion_Table(t *testing.T) {
	ag := &Agent{}
	if ag.trustedCompletion() {
		t.Fatal("nil TaskState must not be trusted")
	}
	ag.TaskState = newTodoState()
	if ag.trustedCompletion() {
		t.Fatal("hasList==false must not be trusted")
	}
	ag.TaskState.update([]TaskItem{
		{TaskID: "1", Summary: "a", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "b", Status: TaskStatusProgressing},
	})
	if ag.trustedCompletion() {
		t.Fatal("progressing tasks must not be trusted")
	}
	ag.TaskState.update([]TaskItem{
		{TaskID: "1", Summary: "a", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "b", Status: TaskStatusCompleted},
	})
	if !ag.trustedCompletion() {
		t.Fatal("tracked all-completed list must be trusted")
	}
}

// completedTodoState returns a todoState with an established, all-completed
// list (the harness's view after a mid-run emission).
func completedTodoState(t *testing.T) *todoState {
	t.Helper()
	ts := newTodoState()
	ts.update([]TaskItem{
		{TaskID: "1", Summary: "investigate", Status: TaskStatusCompleted},
		{TaskID: "2", Summary: "fix", Status: TaskStatusCompleted},
	})
	return ts
}

// newWfContractClient builds a deepseek client pointed at the httptest server
// (same shape as the other loop tests).
func newWfContractClient(baseURL string) *deepseek.Client {
	return &deepseek.Client{
		BaseURL:    baseURL,
		APIKey:     "test-key",
		HTTPClient: &http.Client{Transport: &http.Transport{}},
	}
}
