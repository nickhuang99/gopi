package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// ── Context.RemoveToolPair unit tests (成对删除) ──────────────────────────

// ctxWithSearchPair builds a context whose history ends with a memory_search
// tool_use + tool_result pair, optionally followed by a feedback round.
func ctxWithSearchPair(extra func(*Context)) *Context {
	c := &Context{}
	c.AddUserMessage("epic task input")
	c.AddAssistantMessageWithThinking("", []deepseek.ToolCall{
		{ID: "call_s1", Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "past experience"}},
	}, "", "")
	c.AddToolResult("call_s1", "#1 useless hit noise that must vanish")
	if extra != nil {
		extra(c)
	}
	return c
}

func TestRemoveToolPair_NormalDelete(t *testing.T) {
	c := ctxWithSearchPair(func(c *Context) {
		c.AddAssistantMessageWithThinking("", []deepseek.ToolCall{
			{ID: "call_fb1", Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": "useless"}},
		}, "", "")
		c.AddToolResult("call_fb1", "feedback recorded")
	})
	before := len(c.Messages)

	if n, _ := c.RemoveToolPair("call_s1", ""); n != 2 {
		t.Fatalf("RemoveToolPair removed %d messages, want 2", n)
	}
	if len(c.Messages) != before-2 {
		t.Fatalf("message count %d, want %d", len(c.Messages), before-2)
	}
	// The search noise must be gone; the feedback pair must remain.
	joined := joinRoles(c.Messages)
	if strings.Contains(joined, "call_s1") || strings.Contains(joined, "useless hit noise") {
		t.Fatalf("search pair still present after removal: %s", joined)
	}
	if len(c.Messages) != 3 || c.Messages[2].ToolCallID != "call_fb1" {
		t.Fatalf("feedback pair must stay: %+v", c.Messages)
	}
	// Role sequence must stay legal: user → assistant → tool.
	want := "user,assistant,tool"
	if joined != want {
		t.Fatalf("role sequence after removal = %q, want %q", joined, want)
	}
}

func TestRemoveToolPair_NotFoundAndEmpty(t *testing.T) {
	c := ctxWithSearchPair(nil)
	orig := len(c.Messages)
	if n, _ := c.RemoveToolPair("call_unknown", ""); n != 0 {
		t.Fatalf("unknown id removed %d messages, want 0 (silent no-op)", n)
	}
	if n, _ := c.RemoveToolPair("", ""); n != 0 {
		t.Fatalf("empty id removed %d messages, want 0", n)
	}
	if len(c.Messages) != orig {
		t.Fatal("no-op must not mutate the context")
	}
}

func TestRemoveToolPair_AnyToolRemovable(t *testing.T) {
	// P1 generalization: the memory_search-only restriction is gone — ANY
	// tool's dead-end pair (read/bash/…) can now be trimmed via context_trim.
	c := &Context{}
	c.AddUserMessage("epic")
	c.AddAssistantMessageWithThinking("", []deepseek.ToolCall{
		{ID: "call_r1", Name: "read", Arguments: map[string]interface{}{"path": "/x"}},
	}, "", "")
	c.AddToolResult("call_r1", "file contents")
	if n, _ := c.RemoveToolPair("call_r1", ""); n != 2 {
		t.Fatalf("non-memory tool pair removed %d messages, want 2 (any tool is trimmable now)", n)
	}
	if len(c.Messages) != 1 {
		t.Fatal("read pair must be removed (generalization)")
	}
}

func TestRemoveToolPair_BatchAndTextKeepAssistant(t *testing.T) {
	// Search shares a batch with another tool and the assistant has text:
	// only the search call + its result are dropped; the assistant message,
	// sibling tool call and sibling result survive (no orphaned tool_result,
	// tool_use↔tool_result order preserved).
	c := &Context{}
	c.AddUserMessage("epic")
	c.AddAssistantMessageWithThinking("I am searching then reading.", []deepseek.ToolCall{
		{ID: "call_s1", Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "x"}},
		{ID: "call_r1", Name: "read", Arguments: map[string]interface{}{"path": "/x"}},
	}, "", "")
	c.AddToolResult("call_s1", "search noise")
	c.AddToolResult("call_r1", "read result keep me")
	if n, _ := c.RemoveToolPair("call_s1", ""); n != 1 {
		t.Fatalf("removed %d messages, want 1 (assistant kept, one tool result dropped)", n)
	}
	if len(c.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (user, assistant-with-read, read-result)", len(c.Messages))
	}
	last := c.Messages[len(c.Messages)-1]
	if last.Role != "tool" || last.ToolCallID != "call_r1" || last.Content != "read result keep me" {
		t.Fatalf("sibling read result must survive intact: %+v", last)
	}
	asst := c.Messages[1]
	if asst.Role != "assistant" || asst.Content != "I am searching then reading." || len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "call_r1" {
		t.Fatalf("assistant must keep text + read call only: %+v", asst)
	}
}

func TestRemoveToolPair_IncompletePairNoOrphan(t *testing.T) {
	// A tool_use whose result is missing must NOT be removed — deleting it
	// would orphan nothing here, but deleting a result whose tool_use is gone
	// would break the API contract; the safe rule is: incomplete pair → no-op.
	c := &Context{}
	c.AddUserMessage("epic")
	c.AddAssistantMessageWithThinking("", []deepseek.ToolCall{
		{ID: "call_s1", Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "x"}},
	}, "", "")
	if n, _ := c.RemoveToolPair("call_s1", ""); n != 0 {
		t.Fatalf("incomplete pair removed %d messages, want 0", n)
	}
	// And the reverse: a lone tool result (no owning tool_use in history).
	c2 := &Context{}
	c2.AddUserMessage("epic")
	c2.AddToolResult("call_s1", "orphan result")
	if n, _ := c2.RemoveToolPair("call_s1", ""); n != 0 {
		t.Fatalf("lone result removed %d messages, want 0", n)
	}
}

// TestRemoveToolPair_WireRoleAlternation builds the Claude request after a
// pair removal and asserts the wire contract holds: the removed content is
// gone, no orphaned tool_result survives, tool_use↔tool_result order is
// preserved, and roles alternate (the builder merges consecutive user text).
func TestRemoveToolPair_WireRoleAlternation(t *testing.T) {
	c := ctxWithSearchPair(func(c *Context) {
		c.AddAssistantMessageWithThinking("", []deepseek.ToolCall{
			{ID: "call_fb1", Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": "useless"}},
			{ID: "call_st1", Name: toolnames.ToolMemory, Arguments: map[string]interface{}{"action": toolnames.ToolMemoryActionStore, "topic": "t", "question": "q", "answer": "a"}},
		}, "", "")
		c.AddToolResult("call_fb1", "feedback recorded")
		c.AddToolResult("call_st1", "stored")
	})
	c.RemoveToolPair("call_s1", "")

	req := deepseek.BuildClaudeRequest("m", c.Messages, "sys", "", claudeToolDefs, 4096, nil)
	wire := req.Messages

	// Removed content absent.
	if claudeHasToolResult(req, "useless hit noise") {
		t.Fatal("removed search tool_result still on the wire")
	}
	// No orphaned tool_result: every tool_result block must have a preceding
	// tool_use with the same id in the assistant history.
	have := map[string]bool{}
	for _, m := range wire {
		if m.Role == "assistant" {
			for _, b := range m.Content {
				if b.Type == "tool_use" {
					have[b.ID] = true
				}
			}
		}
	}
	for _, m := range wire {
		for _, b := range m.Content {
			if b.Type == "tool_result" && !have[b.ToolUseID] {
				t.Fatalf("orphaned tool_result %s on the wire", b.ToolUseID)
			}
		}
	}
	// Role alternation on the wire (consecutive user messages are merged by
	// the builder, so a same-role pair here is a hard violation).
	for i := 1; i < len(wire); i++ {
		if wire[i].Role == wire[i-1].Role {
			t.Fatalf("role alternation violated on the wire: %s at %d and %d",
				wire[i].Role, i-1, i)
		}
	}
	// The feedback tool_use must still be present (only the search pair drops).
	lastAsst := claudeLastAssistant(req)
	if lastAsst == nil || !strings.Contains(fmt.Sprint(lastAsst.Content), toolnames.ToolMemoryActionFeedback) {
		t.Fatal("feedback tool_use must survive the removal")
	}
}

// ── Schema + description guards (描述即策略) ───────────────────────────────

// TestMemoryFeedbackToolSchemaIdRequired: `id` is the feedback schema's
// required record-id anchor, and the retired call-id parameter is gone — its
// promised mechanism (drop the originating search pair) was retired with
// whitelist slimming, which keys on RECORD-id useful votes.
//
// The sync gate that pins the whole contract is
// TestFeedbackSchemaTeachesWhatTheValidatorDemands.
func TestMemoryFeedbackToolSchemaIdRequired(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	props := schemaProps(t, mem)
	if _, ok := props["id"]; !ok {
		t.Fatal("memory schema missing required parameter id (feedback action)")
	}
	if _, ok := props["rating"]; !ok {
		t.Fatal("memory schema missing rating (feedback action)")
	}
	if _, ok := props[retiredFeedbackParam]; ok {
		t.Fatalf("memory schema still declares %q — unread on the feedback path", retiredFeedbackParam)
	}
	// The enum pins all four actions; conditional required params live in
	// validateToolArgs (the merged def itself requires only action).
	if req := schemaRequired(t, mem); len(req) != 1 || req[0] != "action" {
		t.Fatalf("memory required = %v, want [action] (id/rating are conditional per feedback)", req)
	}
	for _, want := range []string{
		"id=#N is REQUIRED", // the required record id, spelled where the model reads it
		"voted useful",      // the true whitelist survival rule
		"action=" + toolnames.ToolMemoryActionFeedback,
	} {
		if !strings.Contains(mem.Description, want) {
			t.Errorf("memory description missing feedback guidance %q", want)
		}
	}
}

// TestMemorySearchDescriptionImmediateFeedback: the memory(action=search)
// description must steer the LLM to report useless hits right after seeing
// results — by RECORD id, the only id the engine reads — and must teach the
// whitelist survival rule that replaced the retired call-id drop mechanism.
func TestMemorySearchDescriptionImmediateFeedback(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatal("memory missing from claudeToolDefs")
	}
	for _, want := range []string{
		"immediately assess each hit",
		"id=#N",
		"voted useful",
		"action=" + toolnames.ToolMemoryActionSearch,
	} {
		if !strings.Contains(mem.Description, want) {
			t.Errorf("memory description missing immediate-feedback guidance %q", want)
		}
	}
}

// ── Integration smoke: full loop, request-body verification ──────────────

// TestAgent_Run_SearchRemoveContextSlimming is the 真实冒烟 verification at
// the request-body level: the harness runs end-to-end against a mock API; the
// mock captures every request body. Round 1 calls memory_search (3 hits),
// round 2 votes one useless by record id, round 3 (the
// final request) must NO LONGER contain that search's tool_result — the pair
// was dropped from the message array, so it is not re-sent. The feedback
// pair itself stays (the vote remains visible) and wire role alternation
// holds throughout.
func TestAgent_Run_SearchRemoveContextSlimming(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// Seed 3 records sharing a searchable marker; the search must return them
	// so the tool_result carries noise that later rounds must not re-send.
	marker := "searchremove-uniquemarker"
	for i := 1; i <= 3; i++ {
		_, err := tools.MemoryStore(
			fmt.Sprintf("%s topic %d", marker, i),
			fmt.Sprintf("question %d about the marker", i),
			fmt.Sprintf("answer %d this is a long enough distilled answer body to pass the store gate with margin", i),
			"test", []string{"searchremove", "marker", fmt.Sprintf("topic%d", i)},
		)
		if err != nil {
			t.Fatalf("seed store %d: %v", i, err)
		}
	}

	type captured struct{ body string }
	var rounds []captured

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		rounds = append(rounds, captured{body: readBody(t, r)})

		switch callCount {
		case 1:
			// Round 1: memory_search with 3 hits.
			claudeToolUseStreamID(w, "call_s1", toolnames.ToolMemory,
				map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "searchremove uniquemarker"})
		case 2:
			// Round 2: vote the search useless (by record id) +
			// memory_store (so the completion checkpoint has nothing to force).
			claudeToolUseStreamTwo(w,
				"call_fb1", toolnames.ToolMemory, map[string]interface{}{
					"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": "useless", "reason": "contradicts",
				},
				"call_st1", toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionStore,
					"topic": "smoke topic", "question": "smoke question", "answer": "smoke answer that is long enough to pass the distillation gate comfortably",
				})
		default:
			// Round 3: all-completed end_turn → run exits.
			claudeTextStream(w, completedTaskListResponse, "end_turn")
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
		MaxIterations: 5,
	}

	result, err := ag.Run("Do the search-remove task")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected substantive completion, got %q", result)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 API calls, got %d", callCount)
	}
	if len(rounds) != 3 {
		t.Fatalf("captured %d request bodies, want 3", len(rounds))
	}

	// Round 2's request (sent AFTER the search executed) must contain the
	// search tool_result — the noise is in context at that point — and the
	// result must carry the stamped call id so the LLM can copy it verbatim.
	if !strings.Contains(rounds[1].body, marker) {
		t.Fatalf("round 2 request must still carry the search result (baseline): %s", truncate(rounds[1].body, 400))
	}
	if !strings.Contains(rounds[1].body, "[tool_use id: call_s1]") {
		t.Fatalf("round 2 request must carry the stamped search call id (verbatim-copy enabler)")
	}
	// Round 3's request (sent AFTER the useless feedback) must NOT contain it
	// — 上下文瘦身 verified at the request-body level.
	if strings.Contains(rounds[2].body, marker) {
		t.Fatalf("round 3 request still contains the dropped search tool_result (context not slimmed)")
	}
	// The feedback pair must remain visible (only the search pair is dropped).
	if !strings.Contains(rounds[2].body, "memory_feedback") || !strings.Contains(rounds[2].body, "call_fb1") {
		t.Fatalf("round 3 request lost the feedback pair (only search pairs are removable)")
	}
	// Wire role alternation: decode round 3 and check roles alternate.
	req3 := decodeClaudeRequest(t, rounds[2].body)
	for i := 1; i < len(req3.Messages); i++ {
		if req3.Messages[i].Role == req3.Messages[i-1].Role {
			t.Fatalf("role alternation violated in round 3 wire: %s at %d/%d", req3.Messages[i].Role, i-1, i)
		}
	}
}

// ── Whitelist context slimming (nick design) ──────────────────────────────
//
// Whitelist semantics: default = drop EVERY memory_search pair; only a pair
// whose returned records were explicitly voted useful survives. Test trace:
//   - seed two records with disjoint markers and search each with limit 1, so
//     search alpha returns only record #1 and search beta only record #2. The
//     limit is LOAD-BEARING, not decoration: both seed rows carry the shared
//     tag "whitelist", so without it each query matches BOTH rows and the two
//     rendered hit lists are identical except for rank order ({1,2} vs {2,1}).
//     Slim then applies its documented conservative rule ("a record shown in
//     several searches keeps them all"), sees #1 as a record of call_miss too,
//     and keeps that pair — so the assertion below would be testing the
//     fixture's overlap rather than the whitelist. With limit 1 each search
//     genuinely returns the single record this trace describes.
//   - round 1 does BOTH searches (call_hit + call_miss) in one message.
//   - round 2 votes useful on record #1 (a hit of call_hit) and memory_stores.
//   - round 3 is the final end_turn.
//
// After round 2's slim: call_hit returned #1 (useful) → its pair is KEPT;
// call_miss returned #2 (no useful vote — "no mention is also useless for
// this session") → its pair is DROPPED. So round 3's request must still carry
// call_hit's marker but NOT call_miss's. The kept/blocked distinction is
// asserted at the request-body level (the real wire the LLM sees).
func TestAgent_Run_WhitelistContextSlim(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// Record #1 (hit) and #2 (miss), disjoint markers.
	for i, seed := range []struct{ marker, topic string }{
		{"whitelist-alpha-marker", "whitelist alpha hit"},
		{"whitelist-beta-marker", "whitelist beta miss"},
	} {
		if _, err := tools.MemoryStore(
			seed.topic,
			fmt.Sprintf("question %d about the %s marker", i+1, seed.marker),
			fmt.Sprintf("answer %d this is a long enough distilled answer body to pass the store gate with margin", i+1),
			"test", []string{"whitelist", seed.marker},
		); err != nil {
			t.Fatalf("seed store %d: %v", i+1, err)
		}
	}

	type captured struct{ body string }
	var rounds []captured
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		rounds = append(rounds, captured{body: readBody(t, r)})
		switch callCount {
		case 1:
			// Two searches in one assistant message: call_hit (alpha) + call_miss (beta).
			claudeToolUseStreamTwo(w,
				"call_hit", toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "whitelist alpha-marker", "limit": float64(1)},
				"call_miss", toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionSearch, "query": "whitelist beta-marker", "limit": float64(1)})
		case 2:
			// Vote useful on record #1 (a hit of call_hit) — the ONLY search
			// deemed usable; store so the completion checkpoint has nothing to
			// force.
			claudeToolUseStreamTwo(w,
				"call_fb", toolnames.ToolMemory, map[string]interface{}{
					"action": toolnames.ToolMemoryActionFeedback, "id": float64(1), "rating": "useful", "reason": "hit covers the recipe",
				},
				"call_st", toolnames.ToolMemory, map[string]interface{}{"action": toolnames.ToolMemoryActionStore,
					"topic": "smoke topic", "question": "smoke question", "answer": "smoke answer that is long enough to pass the distillation gate comfortably",
				})
		default:
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{BaseURL: server.URL, APIKey: "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}}},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}
	result, err := ag.Run("Run the whitelist slimming task")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected substantive completion, got %q", result)
	}

	// Round 2 request (after round-1 searches executed) must carry BOTH
	// markers — both search results are in context at that point.
	if !strings.Contains(rounds[1].body, "whitelist-alpha-marker") {
		t.Fatalf("round 2 request lost call_hit's result (must be present before slimming): %s", truncate(rounds[1].body, 400))
	}
	if !strings.Contains(rounds[1].body, "whitelist-beta-marker") {
		t.Fatalf("round 2 request lost call_miss's result (must be present before slimming): %s", truncate(rounds[1].body, 400))
	}

	// Round 3 request (after round-2 useful vote + slim): call_hit survived
	// (record #1 was useful), call_miss was dropped (record #2 un-voted).
	if !strings.Contains(rounds[2].body, "whitelist-alpha-marker") {
		t.Fatalf("round 3 request dropped the useful search (call_hit must survive the whitelist): %s", truncate(rounds[2].body, 400))
	}
	if strings.Contains(rounds[2].body, "whitelist-beta-marker") {
		t.Fatalf("round 3 request still contains the unrewarded search (call_miss must be dropped): %s", truncate(rounds[2].body, 400))
	}

	// The useful feedback pair remains visible (only search pairs are removed).
	if !strings.Contains(rounds[2].body, "memory_feedback") || !strings.Contains(rounds[2].body, "call_fb") {
		t.Fatalf("round 3 request lost the useful feedback pair")
	}
	// Wire role alternation still holds after the partial removal.
	req3 := decodeClaudeRequest(t, rounds[2].body)
	for i := 1; i < len(req3.Messages); i++ {
		if req3.Messages[i].Role == req3.Messages[i-1].Role {
			t.Fatalf("role alternation violated in round 3 wire: %s at %d/%d", req3.Messages[i].Role, i-1, i)
		}
	}
}

func joinRoles(msgs []deepseek.Message) string {
	roles := make([]string, len(msgs))
	for i, m := range msgs {
		roles[i] = m.Role
	}
	return strings.Join(roles, ",")
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func decodeClaudeRequest(t *testing.T, body string) *deepseek.ClaudeRequest {
	t.Helper()
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return &req
}

// claudeToolUseStreamID writes a single tool_use block with a custom id.
func claudeToolUseStreamID(w http.ResponseWriter, id, name string, args map[string]interface{}) {
	argsJSON, _ := json.Marshal(args)                //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsJSONStr, _ := json.Marshal(string(argsJSON)) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf("{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q,\"input\":{}}}", id, name))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", argsJSONStr))
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// claudeToolUseStreamTwo writes two tool_use blocks in one assistant message.
func claudeToolUseStreamTwo(w http.ResponseWriter, id1, name1 string, args1 map[string]interface{}, id2, name2 string, args2 map[string]interface{}) {
	a1, _ := json.Marshal(args1)       //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	a1s, _ := json.Marshal(string(a1)) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	a2, _ := json.Marshal(args2)       //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	a2s, _ := json.Marshal(string(a2)) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf("{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q,\"input\":{}}}", id1, name1))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", a1s))
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf("{\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q,\"input\":{}}}", id2, name2))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", a2s))
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":1}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}
