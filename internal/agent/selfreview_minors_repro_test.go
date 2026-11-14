package agent

// Repro tests for the two minor agent-loop bugs filed by the 2026-08-30
// self-review (devdoc/self-review-20260830.md):
//
//   B1  t-1788053264-1 — the "exit with N progressing tasks" warning sits on
//       the every-round path (before the finish_reason switch), so any round
//       whose task list still has progressing entries logs a misleading
//       "exit" line. It must fire ONLY on a real exit round, once.
//   B2  t-1788053264-2 — isNonZeroExit hard-codes {1,2,128,127,126,255,11,137}
//       as substrings; codes 3/42/134/143 (git exit 3, curl 42, SIGABRT 134,
//       SIGTERM 143) slip past the dead-end nudge. It must parse the numeric
//       exit code instead of enumerating.

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
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// claudeTextPlusToolStream writes a Claude SSE stream carrying BOTH a text
// block (a task list, in our case) and one tool_use block, ending with
// stop_reason=tool_use — the shape of a normal working round.
func claudeTextPlusToolStream(w http.ResponseWriter, text, name string, args map[string]interface{}) {
	textJSON, _ := json.Marshal(text) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
	writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, textJSON))
	writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	argsJSON, _ := json.Marshal(args)                //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsJSONStr, _ := json.Marshal(string(argsJSON)) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_repro","name":%q,"input":{}}}`, name))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":%s}}`, argsJSONStr))
	writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
	writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":5}}`)
	writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// TestReproExitCheckWarnsOnlyOnExit (t-1788053264-1): a normal multi-round
// run whose task list stays progressing until the end must log the
// "exit with N progressing tasks" warning EXACTLY ONCE — on the round that
// actually returns. Before the fix the warning also fired on the mid-run
// rounds that merely re-emitted the progressing list (stderr spam, and an
// "exit" claim on rounds that were not exiting).
func TestReproExitCheckWarnsOnlyOnExit(t *testing.T) {
	progressing := "Working on the fix.\n[TASK_LIST_BEGIN]\n" +
		`[{"task_id":"1","summary":"Fix bug","status":"progressing"}]` + "\n" +
		"[TASK_LIST_END]"

	dir := t.TempDir()
	toolFile := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(toolFile, []byte("work log\n"), 0o644); err != nil {
		t.Fatalf("temp file: %v", err)
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callCount {
		case 1: // mid-run: progressing list + real tool call
			claudeTextPlusToolStream(w, progressing, "read", map[string]interface{}{"path": toolFile})
		case 2: // mid-run: tool call only (no list)
			claudeToolUseStream(w, "read", map[string]interface{}{"path": toolFile})
		case 3: // first end_turn (bug022 keeps a confirmation round)
			claudeTextStream(w, progressing, "end_turn")
		default: // confirmation round — the real exit
			claudeTextStream(w, progressing, "end_turn")
		}
	}))
	defer server.Close()

	var buf strings.Builder
	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 8,
		Logger:        log.New(&buf, "", 0),
	}

	result, err := ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "Working on the fix") {
		t.Fatalf("unexpected result: %q", result)
	}
	if callCount != 4 {
		t.Fatalf("expected 4 API calls (2 tool rounds + end_turn + confirmation), got %d", callCount)
	}

	warns := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		// Anchor on the stable "run exiting with" prefix. The counts after it
		// split progressing/pending since the three-state vocabulary (review
		// 2026-09-07 T2) — a pending task is not progressing.
		if strings.Contains(line, "run exiting with") {
			warns++
		}
	}
	if warns != 1 {
		t.Errorf("exit-check warning fired %d times; want exactly 1 (on the real exit round). Log:\n%s",
			warns, buf.String())
	}
}

// TestReproIsNonZeroExitParsesCode (t-1788053264-2): every non-zero exit
// code is a dead end; only exit=0 (and a non-numeric/absent exit line) is
// exempt. Regression list includes the previously missed 3/42/134/143.
func TestReproIsNonZeroExitParsesCode(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   bool
	}{
		{"success zero", "exit=0\ncompiled ok", false},
		{"bare zero", "exit=0", false},
		{"one", "exit=1\n[stderr] no such file", true},
		{"git bad object (was missed)", "exit=3\n[stderr] fatal: bad object", true},
		{"curl-ish 42 (was missed)", "exit=42\n[stderr] not found", true},
		{"perm 126", "exit=126\npermission denied", true},
		{"not found 127", "exit=127\ncmd not found", true},
		{"SIGABRT 134 (was missed)", "exit=134\n[stderr] abort", true},
		{"OOM kill 137", "exit=137\nkilled", true},
		{"SIGTERM 143 (was missed)", "exit=143\n[stderr] terminated", true},
		{"ceiling 255", "exit=255\nerr", true},
		{"arbitrary 97", "exit=97\nweird code", true},
		{"non-numeric exit", "exit=foo\n??? ", false},
		{"no exit line", "plain output", false},
		{"empty", "", false},
		{"embedded word not exit line", "the pexit=1 prefix is not an exit line", false},
	}
	for _, c := range cases {
		tc := []deepseek.ToolCall{{Name: "bash"}}
		if got := resultsHaveDeadEnd(tc, []string{c.result}); got != c.want {
			t.Errorf("%s: resultsHaveDeadEnd(bash, %q) = %v, want %v", c.name, c.result, got, c.want)
		}
	}
}

// compile-time guard: the helpers above must stay in sync with the
// writeClaudeSSE test helper used by the rest of the suite.
var _ = io.Discard
