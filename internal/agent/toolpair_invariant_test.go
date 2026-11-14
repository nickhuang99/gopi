package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// This file proves the Claude Messages tool_use/tool_result pairing invariant
// — the constraint that killed a live p1 run at its hand-off on 2026-09-22 and
// then killed every resume of it, because the damage was persisted into the
// raw-log checkpoint (epic: trimCommittedTaskArea + the outbound choke point).
//
// The API's own words, from the death dump
// (~/.gopi/dumps/20260922-040822.dump.json, HTTP 400):
//
//	"messages.1: `tool_use` ids were found without `tool_result` blocks
//	 immediately after: call_00_ET_TBCTnLz8k12TGe2ZEVxl2105. Each `tool_use`
//	 block must have a corresponding `tool_result` block in the next message."
//
// Every assertion below is therefore written against THAT rule, not against a
// GoPI-internal convention: an assistant message carrying a tool_use must be
// followed by a user message carrying its tool_result.

// assertClaudeWirePairing is the API's rule as a predicate over a wire
// request: every assistant tool_use block must be answered by a tool_result
// block in the immediately following message. It fails the test with the
// offending message index and id — the same coordinates the API reports.
func assertClaudeWirePairing(t *testing.T, tag string, req *deepseek.ClaudeRequest) {
	t.Helper()
	for i := range req.Messages {
		m := &req.Messages[i]
		if m.Role != "assistant" {
			continue
		}
		for _, b := range m.Content {
			if b.Type != "tool_use" || b.ID == "" {
				continue
			}
			ok := false
			if i+1 < len(req.Messages) && req.Messages[i+1].Role == "user" {
				for _, nb := range req.Messages[i+1].Content {
					if nb.Type == "tool_result" && nb.ToolUseID == b.ID {
						ok = true
						break
					}
				}
			}
			if !ok {
				t.Errorf("%s: UNBALANCED tool_use %q at messages.%d — the API rejects this with HTTP 400 (no tool_result in the next message)", tag, b.ID, i)
			}
		}
	}
}

// poisonedHeadWindowDump reproduces the EXACT window from the death dump
// (20260922-040822.dump.json): the first message is an assistant carrying a
// tool_use whose result is not in the array at all, followed by the commit
// round and its result. On master the trim kept index 0 unconditionally, so
// the kept head was that orphan tool_use — the 400 in the dump, verbatim.
//
// The window is built at the SOURCE layer ([]deepseek.Message) because that is
// where the trim operates.
func poisonedHeadWindowDump() []deepseek.Message {
	return []deepseek.Message{
		// messages[0]: assistant tool_use — its result lived at an index
		// inside the dropped window and was cut. This is the orphan the API
		// named.
		{Role: "assistant", ToolCalls: []deepseek.ToolCall{
			{ID: "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105", Name: "bash", Arguments: map[string]interface{}{"command": "ls"}},
		}},
		{Role: "tool", Content: "unrelated earlier result", ToolCallID: "call_other"},
		{Role: "assistant", Content: "working on item 1", ToolCalls: []deepseek.ToolCall{
			{ID: "call-analysis-1", Name: "code_search", Arguments: map[string]interface{}{"symbol": "X"}},
		}},
		{Role: "tool", Content: "some symbol found", ToolCallID: "call-analysis-1"},
		{Role: "assistant", Content: "committing item 1", ToolCalls: []deepseek.ToolCall{
			{ID: "call-commit-1", Name: toolnames.ToolTask, Arguments: map[string]interface{}{
				"action": "commit", "task_id": "1", "status": "completed", "conclusion": "item1: resolved",
			}},
		}},
		{Role: "tool", Content: "committed subtask \"1\"", ToolCallID: "call-commit-1"},
	}
}

// TestTrimCommittedTaskArea_PoisonedHeadMustNotKeepOrphan is the regression
// test for the measured bug: the committed-region trim may not leave the kept
// head an unanswered assistant tool_use.
//
// RED on master (the head kept `call_00_ET_TBCTnLz8k12TGe2ZEVxl2105` while its
// result was cut), GREEN with trimCommittedTaskArea head derivation.
func TestTrimCommittedTaskArea_PoisonedHeadMustNotKeepOrphan(t *testing.T) {
	ag := newTaskAreaTestAgent(t, "sess-poisoned-head")
	for _, m := range poisonedHeadWindowDump() {
		ag.Ctx.AddMessage(m)
	}

	ag.trimCommittedTaskArea()

	// The commit round must still be there (the trim must have happened).
	found := false
	for _, m := range ag.Ctx.Messages {
		if strings.Contains(m.Content, "committing item 1") || strings.Contains(m.Content, "committed subtask") {
			found = true
		}
	}
	if !found {
		t.Fatalf("commit round dropped by the trim: %+v", ag.Ctx.Messages)
	}
	// ...and the surviving array must satisfy the API's pairing rule.
	req := deepseek.BuildClaudeRequest("m", ag.Ctx.Messages, "sys", "", claudeToolDefs, 4096, nil)
	assertClaudeWirePairing(t, "post-trim wire", req)
	if _, orphan := findUnpairedSourceToolUse(ag.Ctx.Messages); orphan {
		t.Errorf("source array still holds an unanswered assistant tool_use after trim: %+v", ag.Ctx.Messages)
	}
}

// TestTrimCommittedTaskArea_HealthyShapeUnchanged is the no-behaviour-change
// regression: for the healthy window (epic briefing at index 0 with NO
// tool_use, analysis + commit round after it) the trim is exactly what it was
// — the epic is kept, the analysis region goes.
func TestTrimCommittedTaskArea_HealthyShapeUnchanged(t *testing.T) {
	ag := newTaskAreaTestAgent(t, "sess-healthy-shape")
	addAnalysisWorkloads(ag)
	before := len(ag.Ctx.Messages)

	ag.trimCommittedTaskArea()

	msgs := ag.Ctx.Messages
	if len(msgs) == 0 || !strings.Contains(msgs[0].Content, "EPIC GOAL") {
		t.Fatalf("healthy trim changed behaviour: epic briefing no longer at index 0: %+v", msgs)
	}
	if want := 4; len(msgs) != want { // epic + commit assistant + commit result + ...
		// The count is asserted loosely (>=2 and reduced) — the point is that
		// the epic survived and the region shrank, not a byte count.
		if len(msgs) < 2 || len(msgs) >= before {
			t.Errorf("healthy trim did not keep the epic + commit round and shrink: before=%d after=%d %+v", before, len(msgs), msgs)
		}
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "analyzing item 1") || strings.Contains(m.Content, "some symbol found") {
			t.Errorf("committed analysis region survived the healthy trim: %+v", msgs)
		}
	}
	req := deepseek.BuildClaudeRequest("m", msgs, "sys", "", claudeToolDefs, 4096, nil)
	assertClaudeWirePairing(t, "healthy post-trim wire", req)
}

// findUnpairedSourceToolUse reports the first source-layer assistant tool_use
// with no tool message answering it directly after.
func findUnpairedSourceToolUse(msgs []deepseek.Message) (string, bool) {
	for i := range msgs {
		if msgs[i].Role != "assistant" {
			continue
		}
		for _, tc := range msgs[i].ToolCalls {
			answered := false
			for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
				if msgs[j].ToolCallID == tc.ID {
					answered = true
					break
				}
			}
			if !answered {
				return tc.ID, true
			}
		}
	}
	return "", false
}

// TestResume_PoisonedCheckpointRepairsInsteadOf400 is the resume-path
// regression. The checkpoint recovered from a raw log is the one input the
// harness did not build itself, and a damaged one is exactly how the 400-death
// became permanent. RED on master: the recovered history is sent verbatim with
// an unbalanced head. GREEN: the resume repairs it, logs it, and the request
// that actually reaches the wire satisfies the API constraint.
func TestResume_PoisonedCheckpointRepairsInsteadOf400(t *testing.T) {
	// The recovered history, as requestToMessages would reconstruct it from
	// the dumped checkpoint: an assistant tool_use whose result is absent.
	history := []deepseek.Message{
		{Role: "assistant", ToolCalls: []deepseek.ToolCall{
			{ID: "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105", Name: "bash", Arguments: map[string]interface{}{"command": "ls"}},
		}},
		{Role: "assistant", Content: "previous answer"},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		// THE API's OWN CONSTRAINT, asserted on every message of every
		// request the resumed run sends — this is the assertion whose absence
		// let the 400 through.
		assertClaudeWirePairing(t, "resumed wire", req)
		// The repaired result must be present as a tool_result block, not
		// silently dropped.
		seen := false
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_result" && b.ToolUseID == "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105" {
					seen = true
				}
			}
		}
		if !seen {
			t.Errorf("resumed request dropped the orphan instead of repairing it: %+v", req.Messages)
		}
		claudeTextStream(w, "ok", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}
	res, err := ag.Resume("", history)
	if err != nil {
		t.Fatalf("Resume() on a poisoned checkpoint must repair and continue, got error: %v", err)
	}
	if callCount == 0 {
		t.Fatal("no request reached the server — the run never sent anything")
	}
	if strings.TrimSpace(res) == "" {
		t.Logf("resume returned no text (fine for this assertion): %q", res)
	}
}

// TestResume_HealthyHistoryUnchanged is the no-behaviour-change regression on
// the resume path: a sound checkpoint is sent as-is (no synthesized blocks,
// no extra messages).
func TestResume_HealthyHistoryUnchanged(t *testing.T) {
	history := []deepseek.Message{
		{Role: "user", Content: "[EPIC GOAL]\ndo a thing"},
		{Role: "assistant", ToolCalls: []deepseek.ToolCall{
			{ID: "call_9", Name: "bash", Arguments: map[string]interface{}{"command": "echo hi"}},
		}},
		{Role: "tool", Content: "hi", ToolCallID: "call_9"},
		{Role: "assistant", Content: "done that"},
	}

	var captured *deepseek.ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = claudeRequestFrom(t, r)
		claudeTextStream(w, "ok", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
	}
	if _, err := ag.Resume("", history); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if captured == nil {
		t.Fatal("no request captured")
	}
	assertClaudeWirePairing(t, "healthy resume wire", captured)
	for _, m := range captured.Messages {
		for _, b := range m.Content {
			if b.Type == "tool_result" && strings.Contains(stringifyBlockContent(b.Content), "tool result unavailable") {
				t.Errorf("healthy history must not carry a synthesized repair block: %+v", captured.Messages)
			}
		}
	}
}

// stringifyBlockContent renders a tool_result content value for matching.
func stringifyBlockContent(v interface{}) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	default:
		b, _ := json.Marshal(s) //stylegate:ignore marshalling an in-process value for a log/matching line
		return string(b)
	}
}

// TestLoadCheckpoint_PoisonedRawLogRecoversARepairedHistory is the persistence
// half of the incident: LoadCheckpoint is the ONLY door a persisted checkpoint
// enters the process through (the resume CLI and the chat turn loop both read
// it), and the damaged array was what the raw log recorded. RED on master —
// the recovered history keeps the unanswered head tool_use, so the request
// built from it is the one the API rejects.
func TestLoadCheckpoint_PoisonedRawLogRecoversARepairedHistory(t *testing.T) {
	// The checkpoint exactly as the raw logger records it: a single JSON line
	// after the "→ REQUEST" marker. Built from the dumped request's shape.
	poisoned := deepseek.ClaudeRequest{
		Model:     "deepseek-v4-pro",
		MaxTokens: 16384,
		Messages: []deepseek.ClaudeMessage{
			{Role: "assistant", Content: []deepseek.ClaudeContentBlock{
				{Type: "tool_use", ID: "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105", Name: "bash", Input: map[string]interface{}{"command": "ls"}},
			}},
			{Role: "assistant", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "previous answer"}}},
		},
	}
	body, err := json.Marshal(poisoned)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	rawLog := filepath.Join(t.TempDir(), "gopi-raw-test.log")
	if err := os.WriteFile(rawLog, append(append([]byte("→ REQUEST\n"), body...), '\n'), 0o600); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	_, history, _, err := LoadCheckpoint(rawLog)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	req := deepseek.BuildClaudeRequest("deepseek-v4-pro", history, "sys", "", claudeToolDefs, 4096, nil)
	assertClaudeWirePairing(t, "wire built from the recovered checkpoint", req)
	if _, orphan := findUnpairedSourceToolUse(history); orphan {
		t.Errorf("recovered history carries an unanswered tool_use: %+v", history)
	}
}
