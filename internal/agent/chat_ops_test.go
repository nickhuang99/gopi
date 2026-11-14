package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// chatOpsAgent is newTestAgent plus a SystemPrompt (the full tool-routing
// prompt ChatOps puts on the wire) and a GOPI_HOME isolated tmp dir so
// memory/task tools never touch the real ~/.gopi.
func chatOpsAgent(t *testing.T, server *httptest.Server) *Agent {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	ag := newTestAgent(t, server)
	ag.SystemPrompt = assembleSystemPrompt()
	ag.Ctx = &Context{}
	return ag
}

// systemPromptOf extracts the concatenated system text blocks from a request
// body for assertions.
func systemPromptOf(t *testing.T, body []byte) string {
	t.Helper()
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	var parts []string
	for _, b := range req.System {
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n")
}

// TestChatOps_ToolRoundThenAnswer verifies the tool-enabled chat turn runs
// real tool rounds: round 1 issues a read tool_use, the harness executes it
// (real file read from the working directory), the result rides back as a
// tool_result, and round 2's end_turn answer is returned. Also asserts the
// full tool defs are advertised and the full system prompt is on the wire.
func TestChatOps_ToolRoundThenAnswer(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("OPSCODE-7788"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			claudeToolUseStream(w, "read", map[string]interface{}{"path": secret})
			return
		}
		// Round 2 must carry the tool_result with the file's content.
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		var req deepseek.ClaudeRequest
		_ = json.Unmarshal(body, &req) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback
		if !strings.Contains(string(body), "OPSCODE-7788") {
			t.Errorf("round-2 request missing tool_result content (file read did not ride back): %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{fmt.Sprintf("The file says %s.", "OPSCODE-7788 — verified by reading the real file.")}, "end_turn")
	}))
	defer server.Close()

	// Real tool execution (executeToolDefault → tools.Read): point the
	// process at the tmp dir? Not needed — the read uses an absolute path.
	ag := chatOpsAgent(t, server)
	result, err := ag.ChatOps("what does the file say?", nil)
	if err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}
	if !strings.Contains(result, "OPSCODE-7788") {
		t.Errorf("result = %q, want it to relay the file content", result)
	}
	if n := atomic.LoadInt32(&callCount); n != 2 {
		t.Errorf("ChatOps() made %d API calls, want 2 (tool round + answer round)", n)
	}
}

// TestChatOps_FullToolsetAndPrompt verifies the ops-turn wire carries the
// FULL toolset and the full tool-routing system prompt (the model must know
// the tools exist), and never the EPIC framing, task-list request, or the
// tiny chatSystemPrompt.
func TestChatOps_FullToolsetAndPrompt(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"Answered fully, with substance enough to pass the length gate."}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	if _, err := ag.ChatOps("summarize the repo layout", nil); err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}

	var req deepseek.ClaudeRequest
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Tools) < 10 {
		t.Errorf("ops turn advertised %d tools, want the full toolset (>=10)", len(req.Tools))
	}
	sys := systemPromptOf(t, []byte(gotBody))
	if len(sys) < 1000 {
		t.Errorf("system prompt only %d chars — ops turns must send the FULL tool-routing prompt, not the tiny chat prompt", len(sys))
	}
	for _, banned := range []string{"[EPIC GOAL]", "establish an initial task list"} {
		if strings.Contains(gotBody, banned) {
			t.Errorf("ops turn body contains task scaffolding %q — chat must never frame turns as tasks", banned)
		}
	}
	if strings.Contains(sys, chatSystemPrompt) {
		t.Error("ops turn sent the minimal chat system prompt — want the full tool-routing prompt")
	}
}

// TestChatOps_DegenerateRoundGetsOneContinuation verifies the degenerate-round
// guard: a no-tool round that says NOTHING gets exactly ONE continuation
// message, and the second silently-empty round is accepted as-is (no infinite
// loop) — 2 API rounds total.
//
// The guard is scoped to the EMPTY round on purpose: a round that said
// something was already streamed to the carrier as this turn's answer, so
// re-asking the model would deliver that answer twice (the duplicated-record
// bug: "Reply with exactly: pty-ok." produced a second round whose text
// streamed and was logged again). Nothing is streamed for an empty round, so
// asking again here duplicates nothing — see TestChatOps_TerseAnswerAnsweredOnce
// for the short-answer half.
func TestChatOps_DegenerateRoundGetsOneContinuation(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		if n == 2 {
			// The continuation pair must appear exactly once (one-shot).
			if got := strings.Count(string(body), "Continue: you have tools available"); got != 1 {
				t.Errorf("round-2 request carries %d continuation messages, want exactly 1: %s", got, body)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			claudeTextStream(w, "", "end_turn") // silent round → degenerate
			return
		}
		claudeTextStreamFragmented(w, []string{"Nothing to add."}, "end_turn") // accepted as-is
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	ag.MaxIterations = 5
	ag.Logger = log.New(io.Discard, "", 0)
	result, err := ag.ChatOps("hello", nil)
	if err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}
	if result != "Nothing to add." {
		t.Errorf("result = %q, want the second degenerate round accepted", result)
	}
	if n := atomic.LoadInt32(&callCount); n != 2 {
		t.Errorf("made %d API calls, want 2 (degenerate, then accepted after one-shot continuation)", n)
	}
}

// TestChatOps_HistorySeededAndContinuity verifies the claude-code-style
// multi-turn requirement: turn 2 can reference what turn 1's tools found.
//
// HOW that requirement is met IS THE SUBSTITUTE FORM (task
// 01a1bafd-d5f6-7c1e-b88e-42373f79ae1c), and this arm is where it is pinned.
// Turn 1's raw assistant tool_use round and its tool_result LEAVE the seed; turn
// 2 references turn 1's tool work through the NOTE that replaced it (its `asked:`
// line, its anchors and its conclusion).
//
// THIS ARM WAS REWRITTEN TWICE, AND THAT HISTORY IS THE POINT. Commit 4c8c44df
// (threshold-triggered fold plus a raw window of 8) turned these assertions
// upside down: it asserted that the raw `tool_result` SURVIVES and that no note
// appears. That change was MEASURED inert on a real session (39 turns, 93k
// estimated tokens against a 150k trigger, 135 raw tool_result blocks on the wire
// and zero notes), so the assertions are restored to the substitute contract.
//
// So the assertions are, in order: the note replaced the turn; the ask and the
// path the tool acted on survive as anchors; and NO raw tool_result remains.
func TestChatOps_HistorySeededAndContinuity(t *testing.T) {
	var round2Body string
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		if n == 3 {                // turn 2's single round
			round2Body = string(b)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			claudeToolUseStream(w, "read", map[string]interface{}{"path": "/tmp/x"})
			return
		}
		claudeTextStreamFragmented(w, []string{"Turn one answer, long enough to satisfy the substantive answer gate."}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	if _, err := ag.ChatOps("look at /tmp/x", nil); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	history := append([]deepseek.Message{}, ag.Ctx.Messages...)
	if _, err := ag.ChatOps("and now?", history); err != nil {
		t.Fatalf("turn 2: %v", err)
	}

	for _, want := range []string{
		"look at /tmp/x", // turn 1's own ask, in the note's `asked:` line
		"and now?",       // turn 2's own input, still a raw user row
		"/tmp/x",         // the path turn 1's tool acted on, as an ANCHOR
		turnFoldNoteHead, // the note is what replaced the turn
	} {
		if !strings.Contains(round2Body, want) {
			t.Errorf("turn-2 seed missing %q", want)
		}
	}
	if strings.Contains(round2Body, "tool_result") {
		t.Errorf("turn-2 seed still carries turn 1's raw tool_result — the fold did not run on a turn that used a tool")
	}
}

// TestChatOps_TerseAnswerAnsweredOnce is the Rule 5 regression test for the
// duplicated assistant answer (found landing the Bubble Tea UI, task
// 01a0bedc): a chat turn whose answer is SHORTER than minFinalAnswerChars must
// be answered ONCE. Before the fix the degenerate-round guard re-issued the
// round, so the model's answer arrived twice — the carrier rendered it twice
// ("pty-okpty-ok") and the session JSONL carried two assistant records for one
// user message.
//
// Both halves of the defect are asserted here: the STREAM (what a display
// renders, via OnText) and the SESSION RECORD (what is persisted).
func TestChatOps_TerseAnswerAnsweredOnce(t *testing.T) {
	const terse = "pty-ok" // 6 chars < minFinalAnswerChars (40) — the repro

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, terse, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	ag.Logger = log.New(io.Discard, "", 0)

	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close() //stylegate:ignore test cleanup: SessionLogger.Close flushes the JSONL; the content is read by the test body
	ag.SessionLog = sl

	var streamed strings.Builder
	ag.OnText = func(d string) { streamed.WriteString(d) }

	result, err := ag.ChatOps("Reply with exactly: "+terse+".", nil)
	if err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}
	if result != terse {
		t.Errorf("ChatOps result = %q, want %q", result, terse)
	}
	// ONE round: a terse answer is accepted as the answer, not re-asked.
	if n := atomic.LoadInt32(&callCount); n != 1 {
		t.Errorf("made %d API calls, want 1 — a terse answer must not be re-asked (the second round duplicates the answer)", n)
	}
	// The stream a display renders carries the answer exactly once.
	if got := streamed.String(); got != terse {
		t.Errorf("streamed text = %q, want %q — the answer streamed more than once", got, terse)
	}

	// The persisted record: one user message ⇒ one assistant text record.
	if err := sl.Sync(); err != nil {
		t.Fatalf("session sync: %v", err)
	}
	recs := readSessionRecords(t, sl.Path())
	var assistantText []string
	users := 0
	for _, rec := range recs {
		switch rec["type"] {
		case "user":
			users++
		case "assistant":
			if m, ok := rec["message"].(map[string]interface{}); ok {
				if blocks, ok := m["content"].([]interface{}); ok {
					for _, b := range blocks {
						if blk, ok := b.(map[string]interface{}); ok && blk["type"] == "text" {
							if s, ok := blk["text"].(string); ok {
								assistantText = append(assistantText, s)
							}
						}
					}
				}
			}
		}
	}
	if users != 1 {
		t.Errorf("session has %d user records, want 1 (the turn)", users)
	}
	if len(assistantText) != 1 || assistantText[0] != terse {
		t.Errorf("session has %d assistant text records %v, want exactly one carrying %q", len(assistantText), assistantText, terse)
	}
}

// readSessionRecords parses a session JSONL into raw records, in file order.
func readSessionRecords(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session %s: %v", path, err)
	}
	var out []map[string]interface{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]interface{}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad session JSON line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}
