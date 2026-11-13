package gopilog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jsonMsg converts a JSON literal to json.RawMessage for helper calls.
func jsonMsg(s string) json.RawMessage {
	return json.RawMessage(s)
}

// writeFixture writes JSONL lines to a temp file and returns its path.
func writeFixture(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// anthropicSession is a realistic post-migration (Anthropic content-block) session:
// session_start → user → thinking → tool_use → request → tool_result (after the
// request flushed the iteration) → text → request → session_end.
var anthropicSession = []string{
	`{"model":"deepseek-v4-flash","sessionId":"test-session-1","timestamp":"2026-08-06T14:58:12.635051212+08:00","type":"session_start","workdir":"/home/nick/work/demo"}`,
	`{"message":{"content":"[EPIC GOAL]\nread go.mod\n\nBased on this Epic, generate an initial task list.","role":"user"},"promptId":"p1","timestamp":"2026-08-06T14:58:12.636510863+08:00","type":"user"}`,
	`{"message":{"content":[{"thinking":"Let me read go.mod.","type":"thinking"}],"role":"assistant"},"timestamp":"2026-08-06T14:58:13.908451918+08:00","type":"assistant"}`,
	`{"message":{"content":[{"id":"call_00_abc","input":{"path":"go.mod"},"name":"read","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-06T14:58:13.909299996+08:00","type":"assistant"}`,
	`{"cache_hit":1920,"cache_miss":138,"completion_tokens":81,"iteration":0,"prompt_tokens":2058,"timestamp":"2026-08-06T14:58:13.91019884+08:00","type":"request"}`,
	`{"message":{"content":[{"content":"module github.com/nick/demo\n\ngo 1.22\n","tool_use_id":"call_00_abc","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-06T14:58:13.911110238+08:00","type":"tool_result"}`,
	`{"message":{"content":[{"text":"The module is **github.com/nick/demo**.\n\n[TASK_LIST_BEGIN]\n[{\"task_id\":\"1\",\"summary\":\"Read go.mod\",\"status\":\"completed\",\"create_at\":\"1712345678\"}]\n[TASK_LIST_END]","type":"text"}],"role":"assistant"},"timestamp":"2026-08-06T14:58:14.980063789+08:00","type":"assistant"}`,
	`{"cache_hit":2048,"cache_miss":117,"completion_tokens":94,"iteration":1,"prompt_tokens":2165,"timestamp":"2026-08-06T14:58:14.98078569+08:00","type":"request"}`,
	`{"elapsed_sec":3.5,"exit_code":0,"total_iterations":2,"total_tokens":6796,"timestamp":"2026-08-06T14:58:16.164767078+08:00","type":"session_end"}`,
}

// TestParseAnthropicSession verifies the full pipeline on a realistic
// post-migration (Anthropic content-block) session log.
func TestParseAnthropicSession(t *testing.T) {
	s, err := ParseFile(writeFixture(t, anthropicSession))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// session_start fields
	if s.ID != "test-session-1" {
		t.Errorf("ID = %q, want test-session-1", s.ID)
	}
	if s.Model != "deepseek-v4-flash" {
		t.Errorf("Model = %q, want deepseek-v4-flash", s.Model)
	}
	if s.Workdir != "/home/nick/work/demo" {
		t.Errorf("Workdir = %q", s.Workdir)
	}
	if s.Task == "" || !strings.HasPrefix(s.Task, "[EPIC GOAL]") {
		t.Errorf("Task not captured: %q", s.Task)
	}

	// session_end fields
	if s.ElapsedSec != 3.5 {
		t.Errorf("ElapsedSec = %v, want 3.5", s.ElapsedSec)
	}
	if s.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", s.ExitCode)
	}
	if s.TotalIterations != 2 {
		t.Errorf("TotalIterations = %d, want 2", s.TotalIterations)
	}
	if s.TotalTokens != 6796 {
		t.Errorf("TotalTokens = %d, want 6796", s.TotalTokens)
	}

	// token accounting sums from request events
	if s.TotalPrompt != 2058+2165 {
		t.Errorf("TotalPrompt = %d, want 4223", s.TotalPrompt)
	}
	if s.TotalCompletion != 81+94 {
		t.Errorf("TotalCompletion = %d, want 175", s.TotalCompletion)
	}

	// two iterations flushed by the two request events
	if len(s.Iterations) != 2 {
		t.Fatalf("len(Iterations) = %d, want 2", len(s.Iterations))
	}

	iter0 := s.Iterations[0]
	if iter0.Number != 0 {
		t.Errorf("iter0.Number = %d, want 0", iter0.Number)
	}
	if iter0.PromptTokens != 2058 || iter0.CacheHit != 1920 || iter0.CacheMiss != 138 || iter0.CompletionTokens != 81 {
		t.Errorf("iter0 token stats wrong: %+v", iter0)
	}
	if iter0.UserPrompt != s.Task {
		t.Errorf("iter0.UserPrompt = %q, want task", iter0.UserPrompt)
	}
	if len(iter0.ThinkingBlocks) != 1 || iter0.ThinkingBlocks[0] != "Let me read go.mod." {
		t.Errorf("ThinkingBlocks = %q", iter0.ThinkingBlocks)
	}
	if len(iter0.ToolCalls) != 1 {
		t.Fatalf("iter0.ToolCalls = %d, want 1", len(iter0.ToolCalls))
	}
	tc := iter0.ToolCalls[0]
	if tc.ID != "call_00_abc" || tc.Name != "read" {
		t.Errorf("ToolCall = %+v", tc)
	}
	if tc.Args["path"] != "go.mod" {
		t.Errorf("ToolCall.Args = %v", tc.Args)
	}
	// tool_result arrived AFTER the request flushed iter0 → cross-iteration match
	if tc.Result != "module github.com/nick/demo\n\ngo 1.22\n" {
		t.Errorf("ToolCall.Result = %q (cross-iteration match failed)", tc.Result)
	}

	iter1 := s.Iterations[1]
	if iter1.Number != 1 {
		t.Errorf("iter1.Number = %d, want 1", iter1.Number)
	}
	if len(iter1.TextBlocks) != 1 {
		t.Fatalf("iter1.TextBlocks = %d, want 1", len(iter1.TextBlocks))
	}
	if len(iter1.ToolCalls) != 0 {
		t.Errorf("iter1.ToolCalls = %d, want 0", len(iter1.ToolCalls))
	}
}

// TestParseBashExitCode verifies bash tool results yield exit codes.
func TestParseBashExitCode(t *testing.T) {
	lines := []string{
		`{"model":"m","sessionId":"s","timestamp":"2026-08-06T14:58:12Z","type":"session_start"}`,
		`{"message":{"content":[{"id":"call_00_b1","input":{"command":"ls"},"name":"bash","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-06T14:58:13Z","type":"assistant"}`,
		`{"cache_hit":0,"cache_miss":0,"completion_tokens":1,"iteration":0,"prompt_tokens":1,"timestamp":"2026-08-06T14:58:14Z","type":"request"}`,
		`{"message":{"content":[{"content":"exit=0\nfile1\nfile2\n","tool_use_id":"call_00_b1","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-06T14:58:15Z","type":"tool_result"}`,
	}
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(s.Iterations) != 1 || len(s.Iterations[0].ToolCalls) != 1 {
		t.Fatalf("iterations = %+v", s.Iterations)
	}
	tc := s.Iterations[0].ToolCalls[0]
	if tc.ExitCode == nil || *tc.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", tc.ExitCode)
	}
	if tc.Result != "exit=0\nfile1\nfile2\n" {
		t.Errorf("Result = %q", tc.Result)
	}
}

// TestParseNoSessionEnd verifies a live/incomplete session (no session_end)
// still parses and retains iterations.
func TestParseNoSessionEnd(t *testing.T) {
	lines := []string{
		`{"model":"m","sessionId":"s2","timestamp":"2026-08-06T14:58:12Z","type":"session_start"}`,
		`{"message":{"content":"hello","role":"user"},"timestamp":"2026-08-06T14:58:12Z","type":"user"}`,
		`{"message":{"content":[{"text":"hi there","type":"text"}],"role":"assistant"},"timestamp":"2026-08-06T14:58:13Z","type":"assistant"}`,
		`{"cache_hit":1,"cache_miss":2,"completion_tokens":3,"iteration":0,"prompt_tokens":4,"timestamp":"2026-08-06T14:58:14Z","type":"request"}`,
	}
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(s.Iterations) != 1 {
		t.Fatalf("iterations = %d, want 1", len(s.Iterations))
	}
	if s.Iterations[0].TextBlocks[0] != "hi there" {
		t.Errorf("TextBlocks = %q", s.Iterations[0].TextBlocks)
	}
	// No session_end → the TOKEN sums are derived from the request events
	// (live-session fallback), because the request rows carry the same token
	// numbers the end hook sums. THE PASS COUNT IS NOT DERIVED, and that is the
	// point of this assertion (task 01a17492, measured 2026-10-26): a pass is
	// counted once per REPL turn the loop ran, while a `request` row is one HTTP
	// exchange — a turn routinely takes many. Filling TotalIterations with the
	// request count printed the ROUNDS under the PASS word ("Iterations: 468 /
	// Passes: 468" on session 41e5dade), so the field stays 0 and EndUnrecorded
	// says why.
	if s.TotalIterations != 0 {
		t.Errorf("TotalIterations = %d, want 0 (no session_end recorded a pass count; the request count is the ROUNDS, not passes)", s.TotalIterations)
	}
	if !s.EndUnrecorded {
		t.Error("EndUnrecorded = false, want true — a session with no session_end must say so rather than implying a recorded count")
	}
	if s.TotalPrompt != 4 {
		t.Errorf("TotalPrompt = %d, want 4", s.TotalPrompt)
	}
	if s.TotalCompletion != 3 {
		t.Errorf("TotalCompletion = %d, want 3", s.TotalCompletion)
	}
	if s.TotalTokens != 7 {
		t.Errorf("TotalTokens = %d, want 7", s.TotalTokens)
	}
	if s.ElapsedSec <= 0 {
		t.Errorf("ElapsedSec = %v, want > 0 (session_start→request span)", s.ElapsedSec)
	}
	if s.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (still running)", s.ExitCode)
	}
	// ExitCode 0 here means "nothing recorded an exit", NOT "the run
	// succeeded" — the two are distinguishable only through this flag, and a
	// formatter that reads ExitCode alone calls a SIGKILLed session a success
	// (measured 2026-09-29). The reader is where the distinction is made.
	if !s.EndUnrecorded {
		t.Error("EndUnrecorded = false, want true (no session_end in the log)")
	}
}

// TestParseRecordsEndWhenSessionEndPresent is the other half of the pair: a
// log that DOES end must not be marked unrecorded, or every healthy session
// would be reported as "no end recorded".
func TestParseRecordsEndWhenSessionEndPresent(t *testing.T) {
	lines := []string{
		`{"model":"m","sessionId":"s3","timestamp":"2026-08-06T14:58:12Z","type":"session_start"}`,
		`{"message":{"content":"hello","role":"user"},"timestamp":"2026-08-06T14:58:12Z","type":"user"}`,
		`{"cache_hit":1,"cache_miss":2,"completion_tokens":3,"iteration":0,"prompt_tokens":4,"timestamp":"2026-08-06T14:58:14Z","type":"request"}`,
	}
	lines = append(lines, `{"elapsed_sec":1.0,"exit_code":0,"total_iterations":1,"total_tokens":7,"timestamp":"2026-08-06T14:58:16Z","type":"session_end"}`)
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.EndUnrecorded {
		t.Error("EndUnrecorded = true for a log that carries session_end")
	}
	if s.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", s.ExitCode)
	}
}

// TestParseInvalidJSON verifies a malformed line produces an error.
func TestParseInvalidJSON(t *testing.T) {
	lines := []string{`{"type":"session_start",`}
	_, err := ParseFile(writeFixture(t, lines))
	if err == nil {
		t.Fatal("expected error for malformed JSONL, got nil")
	}
}

// TestExtractContent handles both string and array content forms.
func TestExtractContent(t *testing.T) {
	str := jsonMsg(`"plain string"`)
	if got := extractContent(str); got != "plain string" {
		t.Errorf("string content = %q", got)
	}

	arr := jsonMsg(`[{"type":"text","text":"first block"},{"type":"thinking","thinking":"hidden"}]`)
	if got := extractContent(arr); got != "first block" {
		t.Errorf("array content = %q, want first text block", got)
	}

	empty := jsonMsg(``)
	if got := extractContent(empty); got != "" {
		t.Errorf("empty content = %q", got)
	}
}

// TestParseContentBlocks parses Anthropic content blocks.
func TestParseContentBlocks(t *testing.T) {
	raw := jsonMsg(`[
		{"type":"thinking","thinking":"reason here"},
		{"type":"tool_use","id":"call_1","name":"bash","input":{"command":"pwd"}},
		{"type":"text","text":"final answer"}
	]`)
	blocks := parseContentBlocks(raw)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}
	if blocks[0].Type != "thinking" || blocks[0].Thinking != "reason here" {
		t.Errorf("thinking block = %+v", blocks[0])
	}
	if blocks[1].Type != "tool_use" || blocks[1].ID != "call_1" || blocks[1].Name != "bash" {
		t.Errorf("tool_use block = %+v", blocks[1])
	}
	if blocks[2].Type != "text" || blocks[2].Text != "final answer" {
		t.Errorf("text block = %+v", blocks[2])
	}
}

// TestExtractExitCode covers valid, missing, and malformed exit prefixes.
func TestExtractExitCode(t *testing.T) {
	if got := extractExitCode("exit=0\noutput\n"); got == nil || *got != 0 {
		t.Errorf("exit=0 → %v, want 0", got)
	}
	if got := extractExitCode("exit=127\n"); got == nil || *got != 127 {
		t.Errorf("exit=127 → %v, want 127", got)
	}
	if got := extractExitCode("plain output"); got != nil {
		t.Errorf("plain output → %v, want nil", got)
	}
	if got := extractExitCode(""); got != nil {
		t.Errorf("empty → %v, want nil", got)
	}
}

// TestTimestampParsing verifies RFC3339 with sub-second precision and zone offset.
func TestTimestampParsing(t *testing.T) {
	lines := []string{
		`{"model":"m","sessionId":"s3","timestamp":"2026-08-06T14:58:12.635051212+08:00","type":"session_start"}`,
	}
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.Started.IsZero() {
		t.Fatal("Started timestamp not parsed")
	}
	// 14:58:12 +08:00 == 06:58:12 UTC
	if s.Started.UTC().Hour() != 6 || s.Started.UTC().Minute() != 58 {
		t.Errorf("Started = %v, want 06:58 UTC", s.Started)
	}
}

// memorySession is a minimal Anthropic-content-block session with one
// tool_use/tool_result pair per memory tool (memory_search / memory_store /
// memory_feedback / memory_update), for the --memory/--filter feature.
var memorySession = []string{
	`{"model":"deepseek-v4-flash","sessionId":"mem-sess-1","timestamp":"2026-08-10T09:00:00.000+08:00","type":"session_start","workdir":"/tmp/w"}`,
	`{"message":{"content":"[EPIC GOAL]\ndebug memory","role":"user"},"promptId":"p1","timestamp":"2026-08-10T09:00:01.0+08:00","type":"user"}`,
	`{"message":{"content":[{"id":"call_s1","input":{"limit":5,"query":"go sqlite fts5"},"name":"memory_search","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-10T09:00:02.5+08:00","type":"assistant"}`,
	`{"cache_hit":1,"cache_miss":2,"completion_tokens":3,"iteration":0,"prompt_tokens":4,"timestamp":"2026-08-10T09:00:02.6+08:00","type":"request"}`,
	`{"message":{"content":[{"content":"#1 memory hit here","tool_use_id":"call_s1","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-10T09:00:03.0+08:00","type":"tool_result"}`,
	`{"message":{"content":[{"id":"call_fb1","input":{"id":1,"rating":"useful","reason":"hit matched"},"name":"memory_feedback","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-10T09:00:03.5+08:00","type":"assistant"}`,
	`{"message":{"content":[{"content":"Feedback recorded (id=1).","tool_use_id":"call_fb1","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-10T09:00:03.9+08:00","type":"tool_result"}`,
	`{"message":{"content":[{"id":"call_st1","input":{"answer":"the answer text","question":"the question?","topic":"t","source":"s"},"name":"memory_store","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-10T09:00:04.0+08:00","type":"assistant"}`,
	`{"cache_hit":0,"cache_miss":0,"completion_tokens":1,"iteration":1,"prompt_tokens":2,"timestamp":"2026-08-10T09:00:04.1+08:00","type":"request"}`,
	`{"message":{"content":[{"content":"Memory stored (id=99).","tool_use_id":"call_st1","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-10T09:00:05.0+08:00","type":"tool_result"}`,
	`{"message":{"content":[{"id":"call_up1","input":{"id":99,"keywords":["kw1"]},"name":"memory_update","type":"tool_use"}],"role":"assistant"},"timestamp":"2026-08-10T09:00:05.5+08:00","type":"assistant"}`,
	`{"message":{"content":[{"content":"Memory updated (id=99).","tool_use_id":"call_up1","type":"tool_result"}],"role":"user"},"timestamp":"2026-08-10T09:00:06.0+08:00","type":"tool_result"}`,
	`{"elapsed_sec":1.0,"exit_code":0,"total_iterations":2,"total_tokens":10,"timestamp":"2026-08-10T09:00:07.0+08:00","type":"session_end"}`,
}

// checkpointSession reproduces a task-run session where the framework injects
// a second user row ([system] TASK-START CHECKPOINT) between the [EPIC GOAL]
// briefing and the first assistant turn. Regression: the briefing must survive
// to Iteration 0's UserPrompt — last-wins previously overwrote it with the
// checkpoint, silently dropping the original task request from --chat.
var checkpointSession = []string{
	`{"model":"deepseek-v4-flash","sessionId":"ckpt-sess-1","timestamp":"2026-08-10T10:00:00.000+08:00","type":"session_start","workdir":"/tmp/w"}`,
	`{"message":{"content":"[EPIC GOAL]\n[Task summary]\nDo the thing\n[Briefing payload]\nDo it in detail.","role":"user"},"promptId":"p0","timestamp":"2026-08-10T10:00:01.0+08:00","type":"user"}`,
	`{"message":{"content":"[system] TASK-START CHECKPOINT: this task has just begun.","role":"user"},"promptId":"p0b","timestamp":"2026-08-10T10:00:01.1+08:00","type":"user"}`,
	`{"message":{"content":[{"text":"Let me start.","type":"text"}],"role":"assistant"},"timestamp":"2026-08-10T10:00:02.0+08:00","type":"assistant"}`,
	`{"cache_hit":1,"cache_miss":2,"completion_tokens":3,"iteration":0,"prompt_tokens":4,"timestamp":"2026-08-10T10:00:02.1+08:00","type":"request"}`,
	`{"elapsed_sec":1.0,"exit_code":0,"total_iterations":1,"total_tokens":10,"timestamp":"2026-08-10T10:00:03.0+08:00","type":"session_end"}`,
}

// TestParseCheckpointKeepsEpicBriefing verifies the first user row (the
// [EPIC GOAL] briefing) is not overwritten by the framework checkpoint row
// that follows it before the first assistant turn.
func TestParseCheckpointKeepsEpicBriefing(t *testing.T) {
	s, err := ParseFile(writeFixture(t, checkpointSession))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(s.Iterations) != 1 {
		t.Fatalf("iterations = %d, want 1", len(s.Iterations))
	}
	want := "[EPIC GOAL]\n[Task summary]\nDo the thing\n[Briefing payload]\nDo it in detail."
	if s.Iterations[0].UserPrompt != want {
		t.Errorf("Iteration 0 UserPrompt = %q, want the Epic briefing %q (checkpoint must not overwrite it)", s.Iterations[0].UserPrompt, want)
	}
	if s.Task != want {
		t.Errorf("s.Task = %q, want %q", s.Task, want)
	}
}

// TestParseMemorySession verifies the parser captures timestamp + full input
// (InputRaw/Args) + matched tool_result for every memory tool call
// (search/store/feedback/update) — the raw data the --memory/--filter view
// renders.
func TestParseMemorySession(t *testing.T) {
	s, err := ParseFile(writeFixture(t, memorySession))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var search, store, feedback, update *ToolCall
	for i := range s.Iterations {
		for j := range s.Iterations[i].ToolCalls {
			tc := &s.Iterations[i].ToolCalls[j]
			switch tc.Name {
			case "memory_search":
				search = tc
			case "memory_store":
				store = tc
			case "memory_feedback":
				feedback = tc
			case "memory_update":
				update = tc
			}
		}
	}
	if search == nil || store == nil || feedback == nil || update == nil {
		t.Fatalf("memory tool calls not captured (search=%v store=%v feedback=%v update=%v)", search != nil, store != nil, feedback != nil, update != nil)
	}

	// timestamp captured from tool_use event
	if search.Timestamp.IsZero() {
		t.Error("search Timestamp not captured")
	} else if h, m, s2 := search.Timestamp.Clock(); h != 9 || m != 0 || s2 != 2 {
		t.Errorf("search Timestamp = %v, want 09:00:02", search.Timestamp)
	}

	// full input preserved as raw + parsed args
	if len(search.InputRaw) == 0 {
		t.Error("search InputRaw not preserved")
	}
	if search.Args["query"] != "go sqlite fts5" {
		t.Errorf("search Args query = %v", search.Args["query"])
	}

	// matched tool_result full content
	if search.Result != "#1 memory hit here" {
		t.Errorf("search Result = %q", search.Result)
	}

	// store full input: answer/question/topic present
	if store.Args["answer"] != "the answer text" || store.Args["question"] != "the question?" || store.Args["topic"] != "t" {
		t.Errorf("store Args = %v", store.Args)
	}
	if store.Result != "Memory stored (id=99)." {
		t.Errorf("store Result = %q", store.Result)
	}

	// feedback: id/rating/reason input + matched result
	if feedback.Args["id"] != float64(1) || feedback.Args["rating"] != "useful" || feedback.Args["reason"] != "hit matched" {
		t.Errorf("feedback Args = %v", feedback.Args)
	}
	if feedback.Result != "Feedback recorded (id=1)." {
		t.Errorf("feedback Result = %q", feedback.Result)
	}

	// update: id/keywords input + matched result
	if update.Args["id"] != float64(99) {
		t.Errorf("update Args id = %v", update.Args["id"])
	}
	if update.Result != "Memory updated (id=99)." {
		t.Errorf("update Result = %q", update.Result)
	}
}

// ---------------------------------------------------------------------------
// BUG-2 regression: FindSession prefix-match ambiguity.
// A short ID that matches several files must return an error naming the
// candidates — never silently pick an arbitrary one.
// ---------------------------------------------------------------------------

// writeSessions writes N session files into a temp dir (each with its own
// session_start) and returns the dir path.
func writeSessions(t *testing.T, ids ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i, id := range ids {
		lines := []string{
			fmt.Sprintf(`{"type":"session_start","sessionId":%q,"timestamp":"2026-08-0%dT10:00:00Z"}`, id, 1+i%9),
		}
		path := filepath.Join(dir, id+".jsonl")
		data := ""
		for _, l := range lines {
			data += l + "\n"
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatalf("write session %s: %v", id, err)
		}
	}
	return dir
}

func TestFindSessionExactMatch(t *testing.T) {
	dir := writeSessions(t, "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb")
	got, err := FindSession(dir, "aaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("FindSession exact: unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, "aaaaaaaaaaaaaaaa.jsonl") {
		t.Errorf("FindSession exact = %q, want .../aaaaaaaaaaaaaaaa.jsonl", got)
	}
}

// TestFindSessionRefusesUniquePrefix is the epic's core inversion
// (full-id-no-prefix, 2026-09-23): a prefix is NOT resolved even when exactly
// one session matches it. The refusal names the full id, so the operator's
// next command is a copy-paste rather than a guess. (The pre-fix behaviour —
// resolve the unique prefix, error only on ambiguity — is what let a listing's
// 8-character chunk stand in for an identifier.)
func TestFindSessionRefusesUniquePrefix(t *testing.T) {
	dir := writeSessions(t, "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb")
	got, err := FindSession(dir, "bbbb")
	if err == nil {
		t.Fatalf("FindSession unique prefix: expected a refusal, got path %q", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, "not a full id") || !strings.Contains(msg, "bbbbbbbbbbbbbbbb") {
		t.Errorf("unique-prefix refusal must say it is not a full id AND name the full id, got: %s", msg)
	}
	if strings.Contains(msg, "aaaaaaaaaaaaaaaa") {
		t.Errorf("the refusal must name only the ids the prefix actually matches, got: %s", msg)
	}
	// The operand that IS the full id resolves as always.
	full, err := FindSession(dir, "bbbbbbbbbbbbbbbb")
	if err != nil || full == "" {
		t.Errorf("FindSession(full id) = (%q, %v), want the session path", full, err)
	}
}

// TestFindSessionIgnoresCrashDumps: <session-id>.dump.json lives next to the
// session file. Two things must hold: a FULL id resolves to the session, and a
// truncated id's refusal names only real sessions — its own crash dump is not
// an identity and must never be offered as a candidate.
func TestFindSessionIgnoresCrashDumps(t *testing.T) {
	const full = "7168c24d1bad41598baf"
	dir := writeSessions(t, full)
	dump := filepath.Join(dir, full+".dump.json")
	if err := os.WriteFile(dump, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write dump: %v", err)
	}
	got, err := FindSession(dir, full)
	if err != nil {
		t.Fatalf("FindSession full id with a crash dump present: %v", err)
	}
	if !strings.HasSuffix(got, full+".jsonl") {
		t.Errorf("FindSession = %q, want the .jsonl session", got)
	}
	_, err = FindSession(dir, "7168c24d")
	if err == nil {
		t.Fatal("a truncated id must be refused, not resolved")
	}
	if strings.Contains(err.Error(), "dump") {
		t.Errorf("the refusal must not name a crash dump as an identity, got: %s", err.Error())
	}
}

// TestFindSessionAmbiguousPrefix — several sessions sharing a prefix: the
// refusal must name EVERY one of them, so the reader can see the ambiguity the
// old silent pick hid.
func TestFindSessionAmbiguousPrefix(t *testing.T) {
	dir := writeSessions(t, "aaaaaaaaaaaaaaaa", "aaaabbbbbbbbbbbb", "aaaacccccccccccc")
	got, err := FindSession(dir, "aaaa")
	if err == nil {
		t.Fatalf("FindSession ambiguous prefix: expected error, got path %q", got)
	}
	msg := err.Error()
	for _, cand := range []string{"aaaaaaaaaaaaaaaa", "aaaabbbbbbbbbbbb", "aaaacccccccccccc"} {
		if !strings.Contains(msg, cand) {
			t.Errorf("refusal must name candidate %q, got: %s", cand, msg)
		}
	}
	if !strings.Contains(msg, "ambiguous") {
		t.Errorf("a multi-candidate refusal must say so, got: %s", msg)
	}
}

// TestFindSessionNonUUIDOperandKeepsNotFound — the refusal is for ID-SHAPED
// prefixes. An operand that is not a cut-short uuid (a stale id, a typo, a
// legacy name) keeps the ordinary "no match" answer, so no caller has to
// handle a new error for an operand it never resolved either.
func TestFindSessionNonUUIDOperandKeepsNotFound(t *testing.T) {
	dir := writeSessions(t, "aaaaaaaaaaaaaaaa")
	got, err := FindSession(dir, "zzzz")
	if err != nil {
		t.Fatalf("FindSession no-match: unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("FindSession no-match = %q, want empty", got)
	}
}

// TestFindSessionDualReadLayouts — session-dir-unify S1: FindSession must
// resolve a session id in ANY of the three layouts: legacy flat
// <root>/<uuid>.jsonl, task-run <root>/<task_id>/<uuid>.jsonl, and bare
// <root>/<uuid>/session.jsonl. The display id comes from the session_start
// event (sessionId) for every layout — a bare dir file named session.jsonl
// has no usable basename.
func TestFindSessionDualReadLayouts(t *testing.T) {
	dir := t.TempDir()
	writeSessionFile := func(p, id string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		line := fmt.Sprintf(`{"type":"session_start","sessionId":%q,"timestamp":"2026-08-06T10:00:00Z"}`+"\n", id)
		if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	legacyID := "legacy-11111111111111"
	taskID := "t-s1-dual-read"
	bareID := "bare-22222222222222"
	writeSessionFile(filepath.Join(dir, legacyID+".jsonl"), legacyID)                              // legacy flat
	writeSessionFile(filepath.Join(dir, taskID, "run-33333333333333.jsonl"), "run-33333333333333") // task-run run file
	writeSessionFile(filepath.Join(dir, bareID, "session.jsonl"), bareID)                          // bare session dir

	cases := []struct {
		id, wantSuffix string
	}{
		{legacyID, legacyID + ".jsonl"},
		{"run-33333333333333", filepath.Join(taskID, "run-33333333333333.jsonl")},
		{bareID, filepath.Join(bareID, "session.jsonl")},
	}
	for _, c := range cases {
		got, err := FindSession(dir, c.id)
		if err != nil {
			t.Fatalf("FindSession(%q): %v", c.id, err)
		}
		if got == "" || !strings.HasSuffix(got, filepath.FromSlash(c.wantSuffix)) {
			t.Errorf("FindSession(%q) = %q, want suffix .../%s", c.id, got, c.wantSuffix)
		}
	}
}

// TestFileToIDEventAuthoritative — a bare dir session file (session.jsonl)
// has no usable basename; FileToID must return the sessionId recorded in
// session_start. Legacy flat files match by basename alone.
func TestFileToIDEventAuthoritative(t *testing.T) {
	dir := t.TempDir()
	runID := "uuid-run-44444444"
	bareID := "uuid-bare-55555555"
	if err := os.MkdirAll(filepath.Join(dir, bareID), 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"session_start","sessionId":%q,"timestamp":"2026-08-06T10:00:00Z"}`+"\n", bareID)
	if err := os.WriteFile(filepath.Join(dir, bareID, "session.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	line2 := fmt.Sprintf(`{"type":"session_start","sessionId":%q,"timestamp":"2026-08-06T10:00:00Z"}`+"\n", runID)
	if err := os.WriteFile(filepath.Join(dir, runID+".jsonl"), []byte(line2), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FileToID(filepath.Join(dir, bareID, "session.jsonl")); got != bareID {
		t.Errorf("FileToID(bare session.jsonl) = %q, want %q (session_start authoritative)", got, bareID)
	}
	if got := FileToID(filepath.Join(dir, runID+".jsonl")); got != runID {
		t.Errorf("FileToID(flat <uuid>.jsonl) = %q, want %q", got, runID)
	}
}

// TestFileToIDBareDirFallback — a bare session file with an UNREADABLE /
// headless body (no session_start to read) must still resolve to its
// session's uuid: the FALLBACK for session.jsonl is the parent directory's
// name, never the basename stem ("session").
func TestFileToIDBareDirFallback(t *testing.T) {
	dir := t.TempDir()
	bareID := "uuid-bare-nohead-66666666"
	if err := os.MkdirAll(filepath.Join(dir, bareID), 0o755); err != nil {
		t.Fatal(err)
	}
	// Empty file — no session_start event, sessionIDFromStart returns "".
	if err := os.WriteFile(filepath.Join(dir, bareID, "session.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FileToID(filepath.Join(dir, bareID, "session.jsonl")); got != bareID {
		t.Errorf("FileToID(headless bare session.jsonl) = %q, want %q (parent dir fallback)", got, bareID)
	}
	// Task-run file without a head event: basename stem is the run uuid.
	if got := FileToID(filepath.Join(dir, "some-task", "run-77777777.jsonl")); got != "run-77777777" {
		t.Errorf("FileToID(headless task-run file) = %q, want %q (basename stem)", got, "run-77777777")
	}
}

// ---------------------------------------------------------------------------
// BUG-4 regression: ListSessionFiles sorts by session_start timestamp, not
// file mtime. A stale file with a newer mtime must NOT jump ahead of a
// newer session — and the order must be stable across re-syncs.
// ---------------------------------------------------------------------------

func TestListSessionFilesSortsBySessionStart(t *testing.T) {
	// Point the gopi config root at a temp dir via GOPI_HOME so
	// ListSessionFiles reads the fixture sessions.
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	dir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Session A starts 09:00 (older), file mtime 11:00 (newer — a re-sync).
	// Session B starts 10:00 (newer), file mtime 09:30.
	writeFile := func(id, startTS string, mtime time.Time) {
		t.Helper()
		p := filepath.Join(dir, id+".jsonl")
		content := fmt.Sprintf("{\"type\":\"session_start\",\"sessionId\":%q,\"timestamp\":%q}\n", id, startTS)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", id, err)
		}
	}
	writeFile("sess-A", "2026-08-06T09:00:00+08:00", time.Date(2026, 8, 6, 11, 0, 0, 0, time.Local))
	writeFile("sess-B", "2026-08-06T10:00:00+08:00", time.Date(2026, 8, 6, 9, 30, 0, 0, time.Local))

	files, err := ListSessionFiles()
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2: %v", len(files), files)
	}
	// B (10:00 start) must come first even though A has the newer mtime.
	if !strings.Contains(files[0], "sess-B") || !strings.Contains(files[1], "sess-A") {
		t.Errorf("ListSessionFiles order = %v, want [sess-B sess-A] (session_start order, not mtime)", files)
	}
}

// TestSessionHasActivity — a bare session_start-only file (the unit-test /
// died-before-first-turn fingerprint) must read as inactive, while any real
// activity event makes it active. This is the discriminator `--last N` and
// LoadAllSummaries use to keep empty "0 iter 0 tok" rows out of listings.
func TestSessionHasActivity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	dir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	start := "2026-08-06T09:00:00+08:00"
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	startOnly := write("stub.jsonl",
		fmt.Sprintf("{\"type\":\"session_start\",\"sessionId\":%q,\"timestamp\":%q}\n", "stub", start))
	if SessionHasActivity(startOnly) {
		t.Error("session_start-only file must NOT have activity")
	}

	// Each activity event type alone must flip it to active.
	for _, evType := range []string{"user", "request", "assistant", "session_end"} {
		var line string
		switch evType {
		case "user":
			line = fmt.Sprintf("{\"type\":\"user\",\"message\":{\"content\":\"task\",\"role\":\"user\"},\"timestamp\":%q}\n", start)
		case "request":
			line = fmt.Sprintf("{\"type\":\"request\",\"iteration\":1,\"timestamp\":%q}\n", start)
		case "assistant":
			line = fmt.Sprintf("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"hi\"}],\"role\":\"assistant\"},\"timestamp\":%q}\n", start)
		case "session_end":
			line = fmt.Sprintf("{\"type\":\"session_end\",\"elapsed_sec\":1,\"exit_code\":0,\"timestamp\":%q}\n", start)
		}
		content := fmt.Sprintf("{\"type\":\"session_start\",\"sessionId\":%q,\"timestamp\":%q}\n", evType, start) + line
		p := write(evType+".jsonl", content)
		if !SessionHasActivity(p) {
			t.Errorf("session with a %s event must have activity", evType)
		}
	}
}

// TestLoadAllSummariesSkipsDegenerateSessions — LoadAllSummaries (used by
// `gopi session log --all --summary` and the /log interactive views) must not
// include bare session_start-only files as empty summary rows.
func TestLoadAllSummariesSkipsDegenerateSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	dir := filepath.Join(home, ".gopi", "sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	start := "2026-08-06T09:00:00+08:00"
	// One real session (start + user + session_end) and one degenerate stub.
	real := fmt.Sprintf("{\"type\":\"session_start\",\"sessionId\":%q,\"timestamp\":%q}\n", "real-1", start) +
		fmt.Sprintf("{\"type\":\"user\",\"message\":{\"content\":\"real task\",\"role\":\"user\"},\"timestamp\":%q}\n", start) +
		fmt.Sprintf("{\"type\":\"session_end\",\"elapsed_sec\":2,\"exit_code\":0,\"total_iterations\":1,\"total_tokens\":5,\"timestamp\":%q}\n", start)
	stub := fmt.Sprintf("{\"type\":\"session_start\",\"sessionId\":%q,\"timestamp\":%q}\n", "stub-1", start)
	for name, content := range map[string]string{"real-1.jsonl": real, "stub-1.jsonl": stub} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	summaries, err := LoadAllSummaries(0)
	if err != nil {
		t.Fatalf("LoadAllSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1 (real session only): %+v", len(summaries), summaries)
	}
	if summaries[0].SessionID != "real-1" {
		t.Errorf("summary session = %q, want real-1 (degenerate stub must be skipped)", summaries[0].SessionID)
	}
}
