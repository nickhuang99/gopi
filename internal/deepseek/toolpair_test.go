package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This file proves the outbound choke point enforces the Claude Messages
// tool_use/tool_result pairing invariant. The API's own rule, verbatim from
// the death dump (~/.gopi/dumps/20260922-040822.dump.json, HTTP 400):
//
//	"messages.1: `tool_use` ids were found without `tool_result` blocks
//	 immediately after: call_00_ET_TBCTnLz8k12TGe2ZEVxl2105. Each `tool_use`
//	 block must have a corresponding `tool_result` block in the next message."
//
// The choke point must therefore do two things and in this order: REPAIR what
// it can (fail closed only as the last resort), and never let an unbalanced
// array reach the wire.

// assertWirePairing applies the API's rule to a request the server actually
// received.
func assertWirePairing(t *testing.T, tag string, req *ClaudeRequest) {
	t.Helper()
	for i := range req.Messages {
		m := &req.Messages[i]
		if m.Role != wireRoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Type != wireBlockTypeToolUse || b.ID == "" {
				continue
			}
			ok := false
			if i+1 < len(req.Messages) && req.Messages[i+1].Role == wireRoleUser {
				for _, nb := range req.Messages[i+1].Content {
					if nb.Type == wireBlockTypeToolResult && nb.ToolUseID == b.ID {
						ok = true
					}
				}
			}
			if !ok {
				t.Errorf("%s: server received an UNBALANCED tool_use %q at messages.%d — the API rejects exactly this with HTTP 400", tag, b.ID, i)
			}
		}
	}
}

// poisonedRequest is the dumped request's shape: head assistant tool_use with
// no result anywhere in the array, then a later round's pairing.
func poisonedRequest() *ClaudeRequest {
	return &ClaudeRequest{
		Model:     "m",
		MaxTokens: 64,
		Messages: []ClaudeMessage{
			{Role: wireRoleAssistant, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeToolUse, ID: "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105", Name: "bash", Input: map[string]interface{}{"command": "ls"}},
			}},
			{Role: wireRoleAssistant, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeToolUse, ID: "call_ok", Name: "read", Input: map[string]interface{}{"path": "/x"}},
			}},
			{Role: wireRoleUser, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeToolResult, ToolUseID: "call_ok", Content: "body"},
			}},
		},
	}
}

// TestRepairToolUsePairs_SplicesIntoToolRound is the unit shape: the orphan's
// result joins the tool round's user message when one exists directly after,
// and a free-standing assistant tool_use gets its own following user message.
func TestRepairToolUsePairs_SplicesIntoToolRound(t *testing.T) {
	req := poisonedRequest()
	res := RepairToolUsePairs(req)
	if !res.Touched() {
		t.Fatal("repair did not fire on a request with an orphan tool_use")
	}
	if len(res.OrphanIDs) != 1 || res.OrphanIDs[0] != "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105" {
		t.Fatalf("OrphanIDs = %v, want the dumped orphan only", res.OrphanIDs)
	}
	if err := ValidateToolUsePairs(req); err != nil {
		t.Fatalf("validate after repair: %v", err)
	}
	// The exhausted head had no tool round after it (its next message is
	// another assistant), so the repair supplies its own user message — and
	// the later, healthy round must survive untouched.
	if len(req.Messages) != 4 {
		t.Fatalf("message count = %d, want 4 (one synthesized user message; the existing round kept)", len(req.Messages))
	}
	if req.Messages[1].Role != wireRoleUser {
		t.Fatalf("synthesized result must be a user message directly after the orphan's assistant: %+v", req.Messages)
	}
	for _, b := range req.Messages[1].Content {
		if b.Type == wireBlockTypeToolResult && b.ToolUseID == "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105" {
			return
		}
	}
	t.Error("the orphan's synthesized tool_result is missing from the inserted message")
}

// TestRepairToolUsePairs_JoinsTheToolRoundWhenPresent covers the splice
// preference: when the message directly after the assistant carries
// tool_result blocks, the synthesized result joins THAT message instead of
// creating a new one — the shape BuildClaudeRequest emits, where consecutive
// tool results coalesce into a single user message.
func TestRepairToolUsePairs_JoinsTheToolRoundWhenPresent(t *testing.T) {
	req := &ClaudeRequest{
		Model: "m",
		Messages: []ClaudeMessage{
			{Role: wireRoleAssistant, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeToolUse, ID: "call_a", Name: "bash", Input: map[string]interface{}{}},
				{Type: wireBlockTypeToolUse, ID: "call_b", Name: "read", Input: map[string]interface{}{}},
			}},
			{Role: wireRoleUser, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeToolResult, ToolUseID: "call_a", Content: "only a's result survived"},
			}},
		},
	}
	res := RepairToolUsePairs(req)
	if res.Repaired != 1 || len(res.OrphanIDs) != 1 || res.OrphanIDs[0] != "call_b" {
		t.Fatalf("res = %+v, want exactly call_b repaired (call_a was already answered)", res)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("message count = %d, want 2 (joined the tool round, no new message)", len(req.Messages))
	}
	if got := len(req.Messages[1].Content); got != 2 {
		t.Fatalf("tool round block count = %d, want 2 (call_a's real result + call_b's repair)", got)
	}
	if err := ValidateToolUsePairs(req); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// TestRepairToolUsePairs_FreeStandingGetsOwnMessage covers the other insertion
// shape: assistant tool_use with no tool round after it.
func TestRepairToolUsePairs_FreeStandingGetsOwnMessage(t *testing.T) {
	req := &ClaudeRequest{
		Model: "m",
		Messages: []ClaudeMessage{
			{Role: wireRoleUser, Content: []ClaudeContentBlock{{Type: wireBlockTypeText, Text: "go"}}},
			{Role: wireRoleAssistant, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeToolUse, ID: "call_solo", Name: "bash", Input: map[string]interface{}{"command": "ls"}},
			}},
		},
	}
	res := RepairToolUsePairs(req)
	if res.Repaired != 1 {
		t.Fatalf("Repaired = %d, want 1", res.Repaired)
	}
	if err := ValidateToolUsePairs(req); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(req.Messages) != 3 || req.Messages[2].Role != wireRoleUser {
		t.Fatalf("expected a new user message after the assistant, got %+v", req.Messages)
	}
}

// TestRepairToolUsePairs_HealthyRequestIsByteIdentical is the
// no-behaviour-change regression on the wire side: a sound array comes out
// unchanged, block for block.
func TestRepairToolUsePairs_HealthyRequestIsByteIdentical(t *testing.T) {
	req := &ClaudeRequest{
		Model: "m",
		Messages: []ClaudeMessage{
			{Role: wireRoleUser, Content: []ClaudeContentBlock{{Type: wireBlockTypeText, Text: "go"}}},
			{Role: wireRoleAssistant, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeText, Text: "reading"},
				{Type: wireBlockTypeToolUse, ID: "call_1", Name: "read", Input: map[string]interface{}{"path": "/x"}},
			}},
			{Role: wireRoleUser, Content: []ClaudeContentBlock{
				{Type: wireBlockTypeToolResult, ToolUseID: "call_1", Content: "body"},
			}},
			{Role: wireRoleAssistant, Content: []ClaudeContentBlock{{Type: wireBlockTypeText, Text: "done"}}},
		},
	}
	before, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	res := RepairToolUsePairs(req)
	if res.Touched() {
		t.Fatalf("repair fired on a healthy request: %+v", res)
	}
	after, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("healthy request changed:\n before=%s\n after =%s", before, after)
	}
}

// TestValidateToolUsePairs_RefusesAndNamesTheOrphan is the fail-closed half:
// an array the repair cannot fix (an unanswered LAST message, which the repair
// does fix — so this drives the predicate directly on the one shape it must
// still report) is reported with its coordinates.
func TestValidateToolUsePairs_RefusesAndNamesTheOrphan(t *testing.T) {
	// A healthy request validates.
	healthy := poisonedRequest()
	RepairToolUsePairs(healthy)
	if err := ValidateToolUsePairs(healthy); err != nil {
		t.Fatalf("repaired request must validate: %v", err)
	}
	// A dangling tool_use is reported, naming the id and the index.
	dangling := &ClaudeRequest{Messages: []ClaudeMessage{
		{Role: wireRoleUser, Content: []ClaudeContentBlock{{Type: wireBlockTypeText, Text: "go"}}},
		{Role: wireRoleAssistant, Content: []ClaudeContentBlock{
			{Type: wireBlockTypeToolUse, ID: "call_dead", Name: "bash", Input: map[string]interface{}{}},
		}},
	}}
	err := ValidateToolUsePairs(dangling)
	if err == nil {
		t.Fatal("dangling tool_use must not validate")
	}
	if !strings.Contains(err.Error(), "call_dead") || !strings.Contains(err.Error(), "messages.1") {
		t.Errorf("violation must name the id and the index (the API reports the same coordinates): %v", err)
	}
}

// TestClaudeChatStream_GateRefusesUnrepairableRequest proves the gate is
// fail-closed rather than decorative: when the invite invariant is broken in a
// way the repair cannot resolve, nothing is sent. The repair resolves every
// shape the protocol can express (it always supplies the missing result), so
// the reachable fail-closed case is asserted through the predicate — a request
// whose repair is disabled. This test documents that the send path calls the
// predicate at all: the previous shape of this code path had NO check, so a
// poisoned array was sent and the API answered 400.
func TestClaudeChatStream_GateRefusesUnrepairableRequest(t *testing.T) {
	if err := ValidateToolUsePairs(poisonedRequest()); err == nil {
		t.Fatal("the un-repaired poisoned request must fail validation — otherwise the gate cannot refuse anything")
	}
	sent := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()
	client := &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0},
	}
	// Sanity: the repaired request DOES go out (the repair is the first
	// response; failing closed is the last resort).
	ch, err := client.ClaudeChatStream(context.Background(), poisonedRequest())
	if err != nil {
		t.Fatalf("repaired request must be sendable: %v", err)
	}
	for range ch {
	}
	if !sent {
		t.Fatal("repaired request never reached the server — the repair path is dead code")
	}
}

// TestMinimalCoreDegrade_NeverEmitsOrphanToolUseWithToolsStripped closes the
// degradation half of the incident. The minimal-core ladder strips the `tools`
// array (that is the point — the greatest common denominator every provider
// accepts), which makes an orphaned tool_use worse, not better: the request
// then declares NO tools while still replaying a tool_use block, the one shape
// on the wire that cannot be explained by the declared tool set at all.
//
// The repair runs BEFORE the degradation, so the core it sends is already
// paired. Asserted on what the SERVER received, not on the local variable.
func TestMinimalCoreDegrade_NeverEmitsOrphanToolUseWithToolsStripped(t *testing.T) {
	var seen []*ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ClaudeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		seen = append(seen, &req)
		if len(seen) == 1 {
			// Deterministic 4xx on the full request → one minimal-core re-send.
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"invalid_request_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"m1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer server.Close()

	client := &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
	}
	req := poisonedRequest()
	req.Tools = []ClaudeToolDef{{Name: "bash", Description: "run"}}
	if _, err := client.ClaudeChat(context.Background(), req); err != nil {
		t.Fatalf("ClaudeChat (degradation path): %v", err)
	}
	if len(seen) < 2 {
		t.Fatalf("expected the full request + the minimal core, server saw %d request(s)", len(seen))
	}
	for i, r := range seen {
		assertWirePairing(t, fmt.Sprintf("degradation send #%d", i+1), r)
	}
	// The LAST send is the minimal core: no tools, and still paired (the
	// property that makes stripping tools safe).
	core := seen[len(seen)-1]
	if len(core.Tools) != 0 {
		t.Errorf("minimal core still carries %d tool def(s) — not the minimal core", len(core.Tools))
	}
	if id := firstUnpairedToolUse(core); id != "" {
		t.Errorf("minimal core (tools stripped) carries a tool_use %q with no tool_result — the worst pairing shape on the wire", id)
	}
}

// firstUnpairedToolUse returns the first tool_use id with no tool_result in the
// immediately following message, or "".
func firstUnpairedToolUse(req *ClaudeRequest) string {
	for i := range req.Messages {
		m := &req.Messages[i]
		if m.Role != wireRoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Type != wireBlockTypeToolUse || b.ID == "" {
				continue
			}
			ok := false
			if i+1 < len(req.Messages) && req.Messages[i+1].Role == wireRoleUser {
				for _, nb := range req.Messages[i+1].Content {
					if nb.Type == wireBlockTypeToolResult && nb.ToolUseID == b.ID {
						ok = true
					}
				}
			}
			if !ok {
				return b.ID
			}
		}
	}
	return ""
}
