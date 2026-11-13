package agent

import (
	"bufio"
	"encoding/json"
	"github.com/nick/gopi/internal/sessionevent"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
)

// tempHome sets HOME to a temporary directory and returns the sessions path.
//
// GopiDirEnv is pinned at the SAME directory, and that is not belt-and-braces:
// config.GopiDir() prefers $GOPI_HOME over $HOME, and the suite pins GopiDirEnv
// in TestMain (testutil.PinSuiteGopiProfile, task 01a13233) so that audit
// lookups stop walking the developer's real sessions tree. A HOME-only pin
// therefore named a profile the logger no longer resolved: every test below
// failed with "session file not found: <the temp HOME>/.gopi/sessions/…" while
// the logger wrote into the suite sandbox. Pinning BOTH keeps the isolation and
// the assertion pointing at one tree, which is the shape the suite entry point
// documents ("a test that wants a session file … names its OWN GOPI_HOME").
func tempHome(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(config.GopiDirEnv, dir)
	sessionsDir := filepath.Join(dir, ".gopi", "sessions")
	return dir, sessionsDir
}

// bareSessionPath resolves a bare (non-task) session's JSONL in the
// session-dir-unify layout: sessions/<session-id>/session.jsonl.
func bareSessionPath(sessionsDir, sessionID string) string {
	return filepath.Join(sessionsDir, sessionID, "session.jsonl")
}

// readJSONLLines reads all JSON objects from a JSONL file.
func readJSONLLines(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("cannot open %s: %v", path, err)
	}
	defer f.Close()

	var lines []map[string]interface{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("invalid JSON line: %s — %v", line, err)
		}
		lines = append(lines, obj)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	return lines
}

// TestSessionLogger_NewSessionLogger verifies that NewSessionLogger creates the
// sessions directory, opens a JSONL file named <session-id>.jsonl, and writes a
// session_start event as the first line.
func TestSessionLogger_NewSessionLogger(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	if sl.SessionID() == "" {
		t.Fatal("expected non-empty session ID")
	}

	// Verify the JSONL file exists.
	path := bareSessionPath(sessionsDir, sl.SessionID())
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("session file not found: %s", path)
	}

	// Read and verify the first event is session_start.
	lines := readJSONLLines(t, path)
	if len(lines) < 1 {
		t.Fatal("expected at least 1 line (session_start)")
	}
	first := lines[0]
	if first["type"] != "session_start" {
		t.Errorf("expected type=session_start, got %q", first["type"])
	}
	if first["sessionId"] != sl.SessionID() {
		t.Errorf("expected sessionId=%q, got %q", sl.SessionID(), first["sessionId"])
	}
	if first["model"] != "test-model" {
		t.Errorf("expected model=test-model, got %q", first["model"])
	}
	if _, ok := first["timestamp"]; !ok {
		t.Error("session_start missing timestamp")
	}
	if _, ok := first["workdir"]; !ok {
		t.Error("session_start missing workdir")
	}
}

// TestSessionLogger_RecordsRawLogPath verifies the raw log path passed to
// NewSessionLogger lands in the session_start event (the authoritative
// session-id → raw log link used by gopi session resume).
func TestSessionLogger_RecordsRawLogPath(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "/tmp/custom/gopi-raw-x.log")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)
	first := lines[0]
	if first["rawLogPath"] != "/tmp/custom/gopi-raw-x.log" {
		t.Errorf("rawLogPath = %q, want /tmp/custom/gopi-raw-x.log", first["rawLogPath"])
	}
}

// TestSessionLogger_LogUser verifies the user event format.
func TestSessionLogger_LogUser(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	if err := sl.LogUser("prompt-123", "Hello, world!"); err != nil {
		t.Fatalf("LogUser failed: %v", err)
	}

	sl.Sync() // R5: buffered writes — flush before reading back

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	userEvent := lines[1]
	if userEvent["type"] != "user" {
		t.Errorf("expected type=user, got %q", userEvent["type"])
	}
	if userEvent["promptId"] != "prompt-123" {
		t.Errorf("expected promptId=prompt-123, got %q", userEvent["promptId"])
	}
	msg, ok := userEvent["message"].(map[string]interface{})
	if !ok {
		t.Fatal("message field missing or wrong type")
	}
	if msg["role"] != "user" {
		t.Errorf("expected role=user, got %q", msg["role"])
	}
	if msg["content"] != "Hello, world!" {
		t.Errorf("expected content='Hello, world!', got %q", msg["content"])
	}
}

// TestSessionLogger_LogAssistantText verifies the assistant text event format.
func TestSessionLogger_LogAssistantText(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	if err := sl.LogAssistantText("Here is the answer."); err != nil {
		t.Fatalf("LogAssistantText failed: %v", err)
	}

	sl.Sync() // R5: buffered writes — flush before reading back

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	event := lines[1]
	if event["type"] != "assistant" {
		t.Errorf("expected type=assistant, got %q", event["type"])
	}
	msg, ok := event["message"].(map[string]interface{})
	if !ok {
		t.Fatal("message field missing or wrong type")
	}
	if msg["role"] != "assistant" {
		t.Errorf("expected role=assistant, got %q", msg["role"])
	}
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected content array with 1 element, got %v", msg["content"])
	}
	block, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatal("content[0] not a map")
	}
	if block["type"] != "text" {
		t.Errorf("expected block type=text, got %q", block["type"])
	}
	if block["text"] != "Here is the answer." {
		t.Errorf("expected text='Here is the answer.', got %q", block["text"])
	}
}

// TestSessionLogger_LogAssistantThinking verifies the thinking content block format.
func TestSessionLogger_LogAssistantThinking(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	if err := sl.LogAssistantThinking("Let me analyze this step by step..."); err != nil {
		t.Fatalf("LogAssistantThinking failed: %v", err)
	}

	sl.Sync() // R5: buffered writes — flush before reading back

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	event := lines[1]
	if event["type"] != "assistant" {
		t.Errorf("expected type=assistant, got %q", event["type"])
	}
	msg, ok := event["message"].(map[string]interface{})
	if !ok {
		t.Fatal("message field missing or wrong type")
	}
	if msg["role"] != "assistant" {
		t.Errorf("expected role=assistant, got %q", msg["role"])
	}
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected content array with 1 element, got %v", msg["content"])
	}
	block, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatal("content[0] not a map")
	}
	if block["type"] != "thinking" {
		t.Errorf("expected block type=thinking, got %q", block["type"])
	}
	if block["thinking"] != "Let me analyze this step by step..." {
		t.Errorf("expected thinking='Let me analyze this step by step...', got %q", block["thinking"])
	}
}

// TestSessionLogger_LogAssistantToolUse verifies the tool_use content block format.
func TestSessionLogger_LogAssistantToolUse(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	tc := deepseek.ToolCall{
		ID:   "call_00_abc123",
		Name: "bash",
		Arguments: map[string]interface{}{
			"command": "ls -la",
		},
	}

	if err := sl.LogAssistantToolUse(tc); err != nil {
		t.Fatalf("LogAssistantToolUse failed: %v", err)
	}

	sl.Sync() // R5: buffered writes — flush before reading back

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	event := lines[1]
	if event["type"] != "assistant" {
		t.Errorf("expected type=assistant, got %q", event["type"])
	}
	msg, ok := event["message"].(map[string]interface{})
	if !ok {
		t.Fatal("message field missing")
	}
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected content array with 1 element")
	}
	block, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatal("content[0] not a map")
	}
	if block["type"] != "tool_use" {
		t.Errorf("expected type=tool_use, got %q", block["type"])
	}
	if block["id"] != "call_00_abc123" {
		t.Errorf("expected id=call_00_abc123, got %q", block["id"])
	}
	if block["name"] != "bash" {
		t.Errorf("expected name=bash, got %q", block["name"])
	}
	input, ok := block["input"].(map[string]interface{})
	if !ok {
		t.Fatal("input field missing or wrong type")
	}
	if input["command"] != "ls -la" {
		t.Errorf("expected command='ls -la', got %q", input["command"])
	}
}

// TestSessionLogger_LogRequest verifies the request metadata event format.
func TestSessionLogger_LogRequest(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	if err := sl.LogRequest(3, 1500, 800, 700, 200); err != nil {
		t.Fatalf("LogRequest failed: %v", err)
	}

	sl.Sync() // R5: buffered writes — flush before reading back

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	event := lines[1]
	if event["type"] != "request" {
		t.Errorf("expected type=request, got %q", event["type"])
	}
	if v, _ := event["iteration"].(float64); int(v) != 3 {
		t.Errorf("expected iteration=3, got %v", event["iteration"])
	}
	if v, _ := event["prompt_tokens"].(float64); int(v) != 1500 {
		t.Errorf("expected prompt_tokens=1500, got %v", event["prompt_tokens"])
	}
	if v, _ := event["cache_hit"].(float64); int(v) != 800 {
		t.Errorf("expected cache_hit=800, got %v", event["cache_hit"])
	}
	if v, _ := event["cache_miss"].(float64); int(v) != 700 {
		t.Errorf("expected cache_miss=700, got %v", event["cache_miss"])
	}
	if v, _ := event["completion_tokens"].(float64); int(v) != 200 {
		t.Errorf("expected completion_tokens=200, got %v", event["completion_tokens"])
	}
}

// TestSessionLogger_LogSessionEnd verifies the session_end event format.
func TestSessionLogger_LogSessionEnd(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	if err := sl.LogSessionEnd(0, 5, 5000, 12.5, nil, EndCause{Reason: sessionevent.SessionEndReasonComplete}); err != nil {
		t.Fatalf("LogSessionEnd failed: %v", err)
	}

	sl.Sync() // R5: buffered writes — flush before reading back

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	event := lines[1]
	if event["type"] != "session_end" {
		t.Errorf("expected type=session_end, got %q", event["type"])
	}
	if v, _ := event["exit_code"].(float64); int(v) != 0 {
		t.Errorf("expected exit_code=0, got %v", event["exit_code"])
	}
	if v, _ := event["total_iterations"].(float64); int(v) != 5 {
		t.Errorf("expected total_iterations=5, got %v", event["total_iterations"])
	}
	if v, _ := event["total_tokens"].(float64); int(v) != 5000 {
		t.Errorf("expected total_tokens=5000, got %v", event["total_tokens"])
	}
	if v, _ := event["elapsed_sec"].(float64); v != 12.5 {
		t.Errorf("expected elapsed_sec=12.5, got %v", event["elapsed_sec"])
	}
}

// TestSessionLogger_FullSession verifies a complete session flow: session_start,
// user, assistant text + tool_use, tool_result, request, session_end — all
// written as valid JSONL lines.
func TestSessionLogger_FullSession(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	// Simulate a full agent run.
	sl.LogUser(newUUIDv4(), "What files are in the current directory?")
	sl.LogRequest(0, 100, 0, 100, 50)

	tc := deepseek.ToolCall{
		ID:   "call_00_xyz",
		Name: "bash",
		Arguments: map[string]interface{}{
			"command": "ls",
		},
	}
	sl.LogAssistantToolUse(tc)
	sl.LogToolResult("call_00_xyz", "file1.txt\nfile2.txt\n")
	sl.LogRequest(1, 200, 100, 100, 30)

	sl.LogAssistantText("The directory contains file1.txt and file2.txt.")
	sl.LogSessionEnd(0, 2, 480, 3.5, nil, EndCause{Reason: sessionevent.SessionEndReasonComplete})
	sl.Sync() // R5: buffered writes — flush before reading back

	// Verify all lines.
	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)

	if len(lines) != 8 {
		t.Fatalf("expected 8 events, got %d", len(lines))
	}

	expectedTypes := []string{
		"session_start",
		"user",
		"request",
		"assistant",
		"tool_result",
		"request",
		"assistant",
		"session_end",
	}

	for i, exp := range expectedTypes {
		got, _ := lines[i]["type"].(string)
		if got != exp {
			t.Errorf("line %d: expected type=%q, got %q", i, exp, got)
		}
	}

	// Verify every line has a timestamp.
	for i, line := range lines {
		if _, ok := line["timestamp"]; !ok {
			t.Errorf("line %d (%q): missing timestamp", i, line["type"])
		}
	}
}

// TestSessionLogger_ConcurrentWrites verifies that concurrent writes to the
// session logger don't race or produce corrupted JSON lines.
func TestSessionLogger_ConcurrentWrites(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	const n = 50
	done := make(chan bool, n)

	for i := 0; i < n; i++ {
		go func(iteration int) {
			sl.LogRequest(iteration, 100, 0, 100, 20)
			done <- true
		}(i)
	}

	for i := 0; i < n; i++ {
		<-done
	}
	sl.Sync() // R5: buffered writes — flush before reading back

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)

	// n request events + 1 session_start = n + 1 lines.
	expected := n + 1
	if len(lines) != expected {
		t.Errorf("expected %d lines, got %d (some writes may have been lost)", expected, len(lines))
	}

	// All non-session_start lines should be valid request events.
	for i, line := range lines {
		if _, ok := line["type"]; !ok {
			t.Errorf("line %d: missing type field (corrupted JSON?)", i)
		}
		if _, ok := line["timestamp"]; !ok {
			t.Errorf("line %d: missing timestamp field", i)
		}
	}
}

// TestNewUUIDv4 verifies UUID v4 formatting and uniqueness.
func TestNewUUIDv4(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := newUUIDv4()
		if seen[id] {
			t.Errorf("duplicate UUID generated: %s", id)
		}
		seen[id] = true

		// Check format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
		if len(id) != 36 {
			t.Errorf("expected 36-char UUID, got %d: %s", len(id), id)
		}
		parts := strings.Split(id, "-")
		if len(parts) != 5 {
			t.Errorf("expected 5 dash-separated parts, got %d: %s", len(parts), id)
		}
		// Version nibble should be '4'.
		if len(parts[2]) >= 1 && parts[2][0] != '4' {
			t.Errorf("expected version 4 UUID, got version %c: %s", parts[2][0], id)
		}
	}
}

// TestSessionLogger_BufferedFlush verifies the R5 buffering: before Sync() the
// file on disk holds only session_start (events are in the memory buffer);
// after Sync() all events are present and in order.
func TestSessionLogger_BufferedFlush(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	sl.LogUser("prompt-1", "first")
	sl.LogAssistantText("second")
	sl.LogRequest(0, 100, 0, 100, 10)
	sl.LogAssistantText("third")

	path := bareSessionPath(sessionsDir, sl.SessionID())

	// Before Sync: only session_start is on disk.
	before := readJSONLLines(t, path)
	if len(before) != 1 {
		t.Fatalf("expected only session_start on disk before Sync, got %d lines", len(before))
	}
	if before[0]["type"] != "session_start" {
		t.Fatalf("expected first line type=session_start, got %q", before[0]["type"])
	}

	sl.Sync()

	// After Sync: all 4 events flushed, in order.
	after := readJSONLLines(t, path)
	want := []string{"session_start", "user", "assistant", "request", "assistant"}
	if len(after) != len(want) {
		t.Fatalf("expected %d lines after Sync, got %d", len(want), len(after))
	}
	for i, exp := range want {
		got, _ := after[i]["type"].(string)
		if got != exp {
			t.Errorf("line %d: expected type=%q, got %q", i, exp, got)
		}
	}
}

// TestSessionLogger_ToolResultSyncedBeforeReturn verifies the crash-safety
// exception: LogToolResult must be readable from disk immediately after it
// returns, with no explicit Sync.
func TestSessionLogger_ToolResultSyncedBeforeReturn(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	tc := deepseek.ToolCall{
		ID:   "call_00_tool",
		Name: "bash",
		Arguments: map[string]interface{}{
			"command": "ls",
		},
	}
	sl.LogAssistantToolUse(tc)
	if err := sl.LogToolResult("call_00_tool", "file1\n"); err != nil {
		t.Fatalf("LogToolResult failed: %v", err)
	}

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)

	// No Sync call — the tool result must already be on disk.
	found := false
	for _, line := range lines {
		if line["type"] == "tool_result" {
			found = true
			msg, _ := line["message"].(map[string]interface{})
			content, _ := msg["content"].([]interface{})
			if len(content) != 1 {
				t.Fatalf("expected 1 content block in tool_result")
			}
			block, _ := content[0].(map[string]interface{})
			if block["tool_use_id"] != "call_00_tool" {
				t.Errorf("expected tool_use_id=call_00_tool, got %v", block["tool_use_id"])
			}
		}
	}
	if !found {
		t.Fatal("tool_result event not on disk right after LogToolResult returned")
	}
}

// TestSessionLogger_ThresholdAutoSync verifies the buffer auto-syncs when the
// accumulated bytes reach sessionLogSyncThreshold (64KB) — a large event is
// readable without an explicit Sync.
func TestSessionLogger_ThresholdAutoSync(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	// 3 x 40KB texts: the second write crosses the 64KB threshold, so the
	// auto-sync fires without an explicit Sync (events 1-2 land on disk).
	big := strings.Repeat("x", 40*1024)
	sl.LogAssistantText(big)
	sl.LogAssistantText(big)
	sl.LogAssistantText(big)

	path := bareSessionPath(sessionsDir, sl.SessionID())
	lines := readJSONLLines(t, path)

	// No explicit Sync: the 64KB threshold must have auto-flushed.
	if len(lines) < 2 {
		t.Fatalf("expected threshold auto-sync to flush >= 2 lines, got %d", len(lines))
	}
	for i, line := range lines {
		if _, ok := line["type"]; !ok {
			t.Errorf("line %d: missing type field", i)
		}
	}
}

// TestSessionLoggerForRun_TaskLayout — session-dir-unify S1 acceptance: a
// task-run child (taskID set) logs into sessions/<task_id>/<run-uuid>.jsonl
// with a co-located <run-uuid>.raw.log and a base metadata.json; the session
// start event carries taskId. Re-running the task mints a fresh run-uuid, so
// several run files coexist in one task dir (acceptance: resume = multiple
// files coexist).
func TestSessionLoggerForRun_TaskLayout(t *testing.T) {
	_, sessionsDir := tempHome(t)

	// Pre-mint the shared run uuid (NewAgent's seam: raw logger first, then
	// the session logger, same uuid).
	runUUID := newUUIDv4()
	taskID := "t-s1-layout-test"

	rawLog, err := NewRawLoggerForRun(taskID, runUUID)
	if err != nil {
		t.Fatalf("NewRawLoggerForRun failed: %v", err)
	}
	defer rawLog.Close()
	rawPath := rawLog.Path()

	sl, err := NewSessionLoggerForRun("test-model", rawPath, runUUID, []string{taskID})
	if err != nil {
		t.Fatalf("NewSessionLoggerForRun failed: %v", err)
	}
	defer sl.Close()

	// 1. Run files co-locate in the task dir.
	taskDir := filepath.Join(sessionsDir, taskID)
	if filepath.Dir(sl.Path()) != taskDir {
		t.Fatalf("session dir = %q, want task dir %q", filepath.Dir(sl.Path()), taskDir)
	}
	wantJSONL := filepath.Join(taskDir, runUUID+".jsonl")
	wantRaw := filepath.Join(taskDir, runUUID+".raw.log")
	if sl.Path() != wantJSONL {
		t.Errorf("session file = %q, want %q", sl.Path(), wantJSONL)
	}
	if rawPath != wantRaw {
		t.Errorf("raw file = %q, want %q", rawPath, wantRaw)
	}

	// 2. metadata.json base carries task_id/session_id/model/created.
	metaRaw, err := os.ReadFile(filepath.Join(taskDir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	if meta["task_id"] != taskID {
		t.Errorf("metadata task_id = %v, want %s", meta["task_id"], taskID)
	}
	if meta["session_id"] != runUUID {
		t.Errorf("metadata session_id = %v, want %s", meta["session_id"], runUUID)
	}
	if meta["model"] != "test-model" {
		t.Errorf("metadata model = %v, want test-model", meta["model"])
	}
	if _, ok := meta["created"]; !ok {
		t.Error("metadata created missing")
	}

	// 3. session_start carries taskId.
	lines := readJSONLLines(t, sl.Path())
	if len(lines) == 0 || lines[0]["type"] != "session_start" {
		t.Fatalf("first event not session_start: %v", lines)
	}
	if lines[0]["taskId"] != taskID {
		t.Errorf("session_start taskId = %v, want %s", lines[0]["taskId"], taskID)
	}
	if lines[0]["rawLogPath"] != wantRaw {
		t.Errorf("session_start rawLogPath = %v, want %s", lines[0]["rawLogPath"], wantRaw)
	}

	// 4. A second run mints a fresh uuid — two run files coexist.
	runUUID2 := newUUIDv4()
	rawLog2, err := NewRawLoggerForRun(taskID, runUUID2)
	if err != nil {
		t.Fatalf("second NewRawLoggerForRun failed: %v", err)
	}
	defer rawLog2.Close()
	sl2, err := NewSessionLoggerForRun("test-model", rawLog2.Path(), runUUID2, []string{taskID})
	if err != nil {
		t.Fatalf("second NewSessionLoggerForRun failed: %v", err)
	}
	defer sl2.Close()
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	var runFiles int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			runFiles++
		}
	}
	if runFiles < 2 {
		t.Errorf("expected >= 2 run files coexisting in task dir, got %d", runFiles)
	}
}

// TestSessionLogger_MetadataMergePreservesSubmitFields — uuid-taskid phase
// regression: a submit-time metadata.json skeleton (summary/payload/status/
// source/manifest, written by taskWriteSubmitMetadata into
// sessions/<uuid>/) must SURVIVE the first task run, whose writeMetadataJSON
// is a read-modify-write merge — never a blind overwrite (or the submit
// fields would vanish the moment the task runs).
func TestSessionLogger_MetadataMergePreservesSubmitFields(t *testing.T) {
	_, sessionsDir := tempHome(t)

	// Simulate the submit-time skeleton the task queue wrote on enqueue.
	taskID := "019c2f1e-4a1b-7f2c-9d3e-8f4a5b6c7d8e"
	taskDir := filepath.Join(sessionsDir, taskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skeleton := map[string]interface{}{
		"task_id":      taskID,
		"status":       "pending",
		"summary":      "submit skeleton survives run",
		"source":       "human",
		"submitted_at": "2026-09-08T22:57:57Z",
		"payload":      "the payload body",
		"priority":     5,
	}
	b, err := json.MarshalIndent(skeleton, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "metadata.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	// A task-run child logs into sessions/<task_id>/ — writeMetadataJSON
	// runs during construction.
	runUUID := newUUIDv4()
	rawLog, err := NewRawLoggerForRun(taskID, runUUID)
	if err != nil {
		t.Fatalf("NewRawLoggerForRun failed: %v", err)
	}
	defer rawLog.Close()
	sl, err := NewSessionLoggerForRun("test-model", rawLog.Path(), runUUID, []string{taskID})
	if err != nil {
		t.Fatalf("NewSessionLoggerForRun failed: %v", err)
	}
	defer sl.Close()

	metaRaw, err := os.ReadFile(filepath.Join(taskDir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	// Submit skeleton fields survive…
	for k, want := range map[string]interface{}{
		"summary": "submit skeleton survives run",
		"source":  "human",
		"payload": "the payload body",
	} {
		if meta[k] != want {
			t.Errorf("metadata[%q] = %v, want %v (submit skeleton lost by run merge)", k, meta[k], want)
		}
	}
	if meta["status"] != "pending" {
		t.Errorf("metadata[status] = %v, want pending", meta["status"])
	}
	// …and the run-time layer is present alongside.
	if meta["task_id"] != taskID || meta["session_id"] != runUUID || meta["model"] != "test-model" {
		t.Errorf("run-time fields missing after merge: %v", meta)
	}
	if _, ok := meta["created"]; !ok {
		t.Error("run-time created field missing after merge")
	}
}

// TestSessionLogger_TurnsSoFarCountsUnderTheOnePredicate pins the WRITER half
// of the session's turn numbering: TurnsSoFar must advance on exactly the rows
// sessionevent.IsTurnOpeningRow accepts, because the other half of the number
// (the JSONL reader, internal/gopilog's SessionTurns) uses that same predicate.
//
// THE READER HALF IS ASSERTED IN cmd/agent (cli_web_turn_test.go), not here:
// internal/gopilog imports internal/agent (its formatter renders agent types),
// so a test in this package cannot import the reader without an import cycle.
// The equality is still measured across both halves — one test on each side of
// the boundary the two packages sit on.
//
// The three rows exercise every leg: an operator line (a turn), a framework
// carrier (not a turn), and a second operator line (a turn again).
func TestSessionLogger_TurnsSoFarCountsUnderTheOnePredicate(t *testing.T) {
	tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger failed: %v", err)
	}
	defer sl.Close()

	if got := sl.TurnsSoFar(); got != 0 {
		t.Fatalf("a fresh logger reports %d turns, want 0", got)
	}

	if err := sl.LogUser("p1", "an operator line"); err != nil {
		t.Fatalf("LogUser: %v", err)
	}
	if got := sl.TurnsSoFar(); got != 1 {
		t.Fatalf("after one operator row TurnsSoFar = %d, want 1", got)
	}
	// A framework carrier opens no turn — the same predicate, at the writer.
	if err := sl.LogUser("p2", sessionevent.HarnessMessagePrefix+"not a turn"); err != nil {
		t.Fatalf("LogUser (carrier): %v", err)
	}
	if got := sl.TurnsSoFar(); got != 1 {
		t.Fatalf("after a carrier row TurnsSoFar = %d, want 1 (a carrier opens no turn)", got)
	}
	if err := sl.LogUser("p3", "a second operator line"); err != nil {
		t.Fatalf("LogUser: %v", err)
	}
	if got := sl.TurnsSoFar(); got != 2 {
		t.Fatalf("after two operator rows TurnsSoFar = %d, want 2", got)
	}
	// A nil logger answers 0 rather than panicking: the callers on the web path
	// reach it through an agent that may carry no record at all.
	var nilLogger *SessionLogger
	if got := nilLogger.TurnsSoFar(); got != 0 {
		t.Errorf("(*SessionLogger)(nil).TurnsSoFar() = %d, want 0", got)
	}
}
