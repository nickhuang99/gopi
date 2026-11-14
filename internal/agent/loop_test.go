package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// writeSSELine writes a single SSE data line followed by a blank line.
func writeSSELine(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// writeClaudeSSE writes a single Claude SSE event (event: + data: lines).
func writeClaudeSSE(w http.ResponseWriter, eventType, payloadJSON string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payloadJSON)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// claudeTextStream writes a minimal Claude SSE text response ending with the
// given stop_reason.
func claudeTextStream(w http.ResponseWriter, text, stopReason string) {
	textJSON, _ := json.Marshal(text) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}")
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}", textJSON))
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "message_delta", fmt.Sprintf("{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q,\"stop_sequence\":null}}", stopReason))
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// claudeRequestFrom decodes the mock request body as a ClaudeRequest.
func claudeRequestFrom(t *testing.T, r *http.Request) *deepseek.ClaudeRequest {
	t.Helper()
	var req deepseek.ClaudeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("failed to decode request: %v", err)
	}
	return &req
}

// claudeHasToolResult reports whether the request contains a tool_result
// block whose content contains the given substring.
func claudeHasToolResult(req *deepseek.ClaudeRequest, substr string) bool {
	for _, msg := range req.Messages {
		for _, b := range msg.Content {
			if b.Type == "tool_result" && strings.Contains(fmt.Sprint(b.Content), substr) {
				return true
			}
		}
	}
	return false
}

// claudeToolUseStream writes a minimal Claude SSE stream containing a single
// tool_use block with the given name and arguments, ending with tool_use.
func claudeToolUseStream(w http.ResponseWriter, name string, args map[string]interface{}) {
	argsJSON, _ := json.Marshal(args)                //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsJSONStr, _ := json.Marshal(string(argsJSON)) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf("{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":%q,\"input\":{}}}", name))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", argsJSONStr))
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// claudeLastAssistant returns the last assistant message in the request, or
// nil when there is none.
func claudeLastAssistant(req *deepseek.ClaudeRequest) *deepseek.ClaudeMessage {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "assistant" {
			return &req.Messages[i]
		}
	}
	return nil
}

func TestAgent_Run_WithToolCall(t *testing.T) {
	// Create a temp file for the agent to read.
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	testContent := "hello from gopi test file\n"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Build the escaped arguments JSON string for the input_json_delta payload.
	argsBytes, _ := json.Marshal(map[string]interface{}{"path": testFile}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsJSONStr, _ := json.Marshal(string(argsBytes))                      //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)// JSON-encoded string

	// Track how many times the mock is called.
	callCount := 0

	// Mock DeepSeek Claude API server — returns Claude SSE streams.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// Verify request method and path.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/anthropic/messages" {
			t.Errorf("expected /anthropic/messages, got %s", r.URL.Path)
		}

		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")

		switch callCount {
		case 1:
			// First call: return a streaming tool_use block to read the test file.
			writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"read\",\"input\":{}}}")
			writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", argsJSONStr))
			writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
			writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
			writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")

		case 2:
			// Second call: verify the conversation includes the tool result,
			// then return the final text response (end_turn #1).
			if !claudeHasToolResult(req, testContent) {
				t.Errorf("expected tool_result message containing file content, but not found in messages")
			}

			finalText := "I read the file. It says: " + testContent
			claudeTextStream(w, finalText, "end_turn")

		case 3:
			// Third call: confirmation round — the continuation message must
			// be present, then end_turn again → the agent exits.
			if n := len(req.Messages); n == 0 || req.Messages[n-1].Role != "user" {
				t.Errorf("expected last message to be the user continuation, got %d messages", n)
			} else {
				foundContinuation := false
				for _, b := range req.Messages[n-1].Content {
					if b.Type == "text" && strings.Contains(b.Text, "ended your turn") {
						foundContinuation = true
					}
				}
				if !foundContinuation {
					t.Errorf("expected continuation message in last user message")
				}
			}
			claudeTextStream(w, "All tasks completed.", "end_turn")

		default:
			t.Errorf("unexpected call count: %d", callCount)
		}
	}))
	defer server.Close()

	// Build an agent pointing at the mock server.
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

	result, err := ag.Run("What is in the test file?")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if !strings.Contains(result, testContent) {
		t.Fatalf("expected result to contain %q, got: %q", testContent, result)
	}

	if callCount != 3 {
		t.Fatalf("expected 3 API calls (tool round + end_turn + confirmation), got %d", callCount)
	}
}

func TestAgent_Run_SimpleResponse(t *testing.T) {
	// Mock server returning end_turn text twice: the first end_turn is not
	// trusted as an exit signal (bug022), the second consecutive one is.
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, "Hello! I am GoPi, your coding agent.", "end_turn")
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

	result, err := ag.Run("Hello")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	expected := "Hello! I am GoPi, your coding agent."
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 API calls (end_turn + confirmation), got %d", callCount)
	}
}

func TestContext_AddMessage(t *testing.T) {
	ctx := &Context{}
	ctx.AddMessage(deepseek.Message{Role: "user", Content: "hello"})

	msgs := ctx.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}
}

func TestContext_GetMessages_WithSystemPrompt(t *testing.T) {
	ctx := &Context{SystemPrompt: "You are a coding agent."}
	ctx.AddUserMessage("hi")

	msgs := ctx.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected system message first, got role=%s", msgs[0].Role)
	}
	if msgs[0].Content != "You are a coding agent." {
		t.Fatalf("unexpected system content: %s", msgs[0].Content)
	}
	if msgs[1].Role != "user" {
		t.Fatalf("expected user message second, got role=%s", msgs[1].Role)
	}
}

func TestExecuteTool_UnknownTool(t *testing.T) {
	ag := &Agent{}
	result := ag.executeTool(deepseek.ToolCall{
		Name:      "nonexistent",
		Arguments: map[string]interface{}{"x": 1},
	})
	if !strings.Contains(result, "Unknown tool") {
		t.Fatalf("expected 'Unknown tool' in result, got: %s", result)
	}
}

func TestExecuteTool_MissingRequiredArgs(t *testing.T) {
	ag := &Agent{}

	cases := []struct {
		name     string
		call     deepseek.ToolCall
		expected string
	}{
		// read/write/edit require a non-empty path.
		{"read: missing path", deepseek.ToolCall{Name: "read", Arguments: map[string]interface{}{}}, "Missing required parameter: path"},
		{"read: empty path", deepseek.ToolCall{Name: "read", Arguments: map[string]interface{}{"path": ""}}, "Missing required parameter: path"},
		{"read: blank path", deepseek.ToolCall{Name: "read", Arguments: map[string]interface{}{"path": "   "}}, "Missing required parameter: path"},
		{"read: nil args", deepseek.ToolCall{Name: "read", Arguments: nil}, "Missing required parameter: path"},
		{"write: missing path", deepseek.ToolCall{Name: "write", Arguments: map[string]interface{}{"content": "x"}}, "Missing required parameter: path"},
		{"edit: missing path", deepseek.ToolCall{Name: "edit", Arguments: map[string]interface{}{"oldStr": "a", "newStr": "b"}}, "Missing required parameter: path"},
		// bash requires a non-empty command.
		{"bash: missing command", deepseek.ToolCall{Name: "bash", Arguments: map[string]interface{}{}}, "Missing required parameter: command"},
		{"bash: empty command", deepseek.ToolCall{Name: "bash", Arguments: map[string]interface{}{"command": ""}}, "Missing required parameter: command"},
		{"bash: blank command", deepseek.ToolCall{Name: "bash", Arguments: map[string]interface{}{"command": "  "}}, "Missing required parameter: command"},
		// code_search requires a symbol name; keyword_search a query.
		{"code_search: missing symbol", deepseek.ToolCall{Name: "code_search", Arguments: map[string]interface{}{}}, "Missing required parameter: symbol"},
		{"code_search: blank symbol", deepseek.ToolCall{Name: "code_search", Arguments: map[string]interface{}{"symbol": "   "}}, "Missing required parameter: symbol"},
		{"keyword_search: missing query", deepseek.ToolCall{Name: "keyword_search", Arguments: map[string]interface{}{}}, "Missing required parameter: query"},
		{"keyword_search: blank query", deepseek.ToolCall{Name: "keyword_search", Arguments: map[string]interface{}{"query": "  "}}, "Missing required parameter: query"},
		// job tool: action is required; action=start needs command,
		// action=output/kill need job_id.
		{"job: missing action", deepseek.ToolCall{Name: "job", Arguments: map[string]interface{}{}}, "Missing required parameter: action (start|output|kill), got \"\""},
		{"job: bad action", deepseek.ToolCall{Name: "job", Arguments: map[string]interface{}{"action": "frobnicate"}}, "Missing required parameter: action (start|output|kill), got \"frobnicate\""},
		{"job: start missing command", deepseek.ToolCall{Name: "job", Arguments: map[string]interface{}{"action": "start"}}, "Missing required parameter: command"},
		{"job: start blank command", deepseek.ToolCall{Name: "job", Arguments: map[string]interface{}{"action": "start", "command": "  "}}, "Missing required parameter: command"},
		{"job: output missing job_id", deepseek.ToolCall{Name: "job", Arguments: map[string]interface{}{"action": "output"}}, "Missing required parameter: job_id"},
		{"job: kill missing job_id", deepseek.ToolCall{Name: "job", Arguments: map[string]interface{}{"action": "kill"}}, "Missing required parameter: job_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ag.executeTool(tc.call); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

// TestExecuteTool_ReadValidPath verifies that a well-formed call still
// executes normally — the validation must not block valid invocations.
func TestExecuteTool_ReadValidPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hello validation\n"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ag := &Agent{}
	result := ag.executeTool(deepseek.ToolCall{
		Name:      "read",
		Arguments: map[string]interface{}{"path": path},
	})
	if !strings.Contains(result, "hello validation") {
		t.Fatalf("expected file content in result, got: %s", result)
	}
}

// TestExecuteTool_BashWorkdir verifies the workdir arg is parsed from tool
// args and wired into the bash call (pwd reflects the directory), and that a
// nonexistent workdir surfaces the tool error.
func TestExecuteTool_BashWorkdir(t *testing.T) {
	dir := t.TempDir()

	ag := &Agent{}
	result := ag.executeTool(deepseek.ToolCall{
		Name:      "bash",
		Arguments: map[string]interface{}{"command": "pwd", "workdir": dir},
	})
	if !strings.Contains(result, dir) {
		t.Fatalf("expected pwd to show workdir %q, got: %s", dir, result)
	}

	missing := filepath.Join(dir, "nope")
	result = ag.executeTool(deepseek.ToolCall{
		Name:      "bash",
		Arguments: map[string]interface{}{"command": "pwd", "workdir": missing},
	})
	if !strings.Contains(result, "does not exist") {
		t.Fatalf("expected workdir-not-found error, got: %s", result)
	}
}

// TestExecuteTool_EditReplaceAll verifies the replace_all arg is parsed from
// tool args and wired into tools.Edit: true replaces every occurrence, and
// the strict-unique default still refuses a multi-match file.
func TestExecuteTool_EditReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("foo foo\n"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ag := &Agent{}
	// replace_all=true (JSON boolean → bool) replaces both occurrences.
	result := ag.executeTool(deepseek.ToolCall{
		Name:      "edit",
		Arguments: map[string]interface{}{"path": path, "oldStr": "foo", "newStr": "bar", "replace_all": true},
	})
	if !strings.Contains(result, "Successfully edited") {
		t.Fatalf("replace_all edit failed: %s", result)
	}
	got, _ := os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if string(got) != "bar bar\n" {
		t.Fatalf("replace_all result content = %q, want %q", got, "bar bar\n")
	}

	// Strict-unique default: multi-match is refused, file untouched.
	if err := os.WriteFile(path, []byte("foo foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result = ag.executeTool(deepseek.ToolCall{
		Name:      "edit",
		Arguments: map[string]interface{}{"path": path, "oldStr": "foo", "newStr": "bar"},
	})
	if !strings.Contains(result, "must be unique") {
		t.Fatalf("expected strict-unique refusal, got: %s", result)
	}
	got, _ = os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if string(got) != "foo foo\n" {
		t.Fatalf("file changed after refused edit: %q", got)
	}
}

// TestExecuteTool_BashValidCommand verifies that a valid bash command still
// executes normally.
func TestExecuteTool_BashValidCommand(t *testing.T) {
	ag := &Agent{}
	result := ag.executeTool(deepseek.ToolCall{
		Name:      "bash",
		Arguments: map[string]interface{}{"command": "echo gopi-validation-ok"},
	})
	if !strings.Contains(result, "gopi-validation-ok") {
		t.Fatalf("expected command output in result, got: %s", result)
	}
}

// TestExecuteTool_JobDispatch verifies the merged job tool (tool-family
// merge, job family) is wired into the per-tool dispatcher: the action
// parameter routes to start → output → kill, end to end.
func TestExecuteTool_JobDispatch(t *testing.T) {
	ag := &Agent{}

	// action=start returns a job id and runs the command detached.
	start := ag.executeTool(deepseek.ToolCall{
		Name:      "job",
		Arguments: map[string]interface{}{"action": "start", "command": "echo job-dispatch-ok"},
	})
	if !strings.Contains(start, "job started: job-") {
		t.Fatalf("unexpected job start result: %q", start)
	}
	jobID := strings.TrimPrefix(strings.SplitN(start, "\n", 2)[0], "job started: ")

	// action=output: bounded wait for the short job to exit, then verify content.
	deadline := time.Now().Add(5 * time.Second)
	for {
		out := ag.executeTool(deepseek.ToolCall{
			Name:      "job",
			Arguments: map[string]interface{}{"action": "output", "job_id": jobID},
		})
		if !strings.Contains(out, "status=running") {
			if !strings.Contains(out, "status=exited code=0") || !strings.Contains(out, "job-dispatch-ok") {
				t.Fatalf("unexpected job output result: %q", out)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never exited: %q", out)
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep tool-call concurrency window: the slow call must still be in flight
	}

	// Unknown job ids surface a clear, standardized error on both actions.
	for _, action := range []string{"output", "kill"} {
		got := ag.executeTool(deepseek.ToolCall{
			Name:      "job",
			Arguments: map[string]interface{}{"action": action, "job_id": "job-nope"},
		})
		if !strings.Contains(got, "unknown job id") {
			t.Fatalf("action=%s: expected unknown-job error, got: %q", action, got)
		}
	}
}

// TestExecuteTool_Todo tracks the harness's task-list view (A3): the todo
// tool must report "no list yet" before any [TASK_LIST] block, and the
// parsed state (counts + markers) afterwards. The update path mirrors the
// Run loop's ParseTaskList → TaskState.update wiring.
func TestExecuteTool_TaskView(t *testing.T) {
	viewCall := deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: map[string]interface{}{"action": toolnames.ToolTaskView}}

	// Before any task list: the tool says so, it does not guess.
	ag := &Agent{TaskState: newTodoState()}
	if got := ag.executeTool(viewCall); !strings.Contains(got, "No task list yet") {
		t.Fatalf("expected 'no task list yet', got: %q", got)
	}

	// A response carrying a TASK_LIST block feeds the tracked state.
	response := "Working on it.\n[TASK_LIST_BEGIN]\n" +
		`[{"task_id":"1","summary":"Read source","status":"completed","create_at":"1"},` +
		`{"task_id":"2","summary":"Fix bug","status":"progressing","create_at":"2"}]` +
		"\n[TASK_LIST_END]"
	tasks, err := ParseTaskList(response)
	if err != nil || tasks == nil {
		t.Fatalf("ParseTaskList: %v", err)
	}
	ag.TaskState.update(tasks)

	got := ag.executeTool(viewCall)
	for _, want := range []string{"1 progressing, 1 completed", "TASK_LIST_BEGIN", `"task_id": "2"`} {
		if !strings.Contains(got, want) {
			t.Errorf("todo result missing %q, got: %q", want, got)
		}
	}

	// A nil Agent (tests building literals without TaskState) degrades
	// gracefully to the same "no list" answer.
	agNil := &Agent{}
	if got := agNil.executeTool(viewCall); !strings.Contains(got, "No task list yet") {
		t.Fatalf("nil TaskState: expected 'no task list yet', got: %q", got)
	}
}

// TestExecuteTool_KeywordSearchValid verifies that a well-formed keyword_search
// call executes and returns formatted hits (rg required).
func TestExecuteTool_KeywordSearchValid(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n// gopi-kw-marker-42\n"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(oldwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	ag := &Agent{}
	result := ag.executeTool(deepseek.ToolCall{
		Name:      "keyword_search",
		Arguments: map[string]interface{}{"query": "gopi-kw-marker-42"},
	})
	if !strings.Contains(result, "a.go") || !strings.Contains(result, "gopi-kw-marker-42") {
		t.Fatalf("expected formatted hit in result, got: %s", result)
	}
}

// TestAgent_Run_StreamFallback verifies that Run() falls back to the
// non-streaming Chat() path when ChatStream() fails to establish a stream.

// TestExecuteTool_BashTimeoutSec verifies the LLM-driven timeout_sec parameter
// is parsed from tool args and enforced: positive → absolute kill, 0 →
// unlimited (runs to completion), omitted → default (runs to completion).
// Args arrive as float64 because tool JSON is decoded with plain
// json.Unmarshal (no UseNumber).
func TestExecuteTool_BashTimeoutSec(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	ag := &Agent{}

	// timeout_sec=1 on a 2s silent sleep → killed by the absolute timeout.
	result := ag.executeTool(deepseek.ToolCall{
		Name:      "bash",
		Arguments: map[string]interface{}{"command": "sleep 2", "timeout_sec": float64(1)},
	})
	if !strings.Contains(result, "timed out") {
		t.Fatalf("expected timeout error for timeout_sec=1, got: %s", result)
	}

	// timeout_sec=0 → unlimited absolute time → sleep 2 completes.
	result = ag.executeTool(deepseek.ToolCall{
		Name:      "bash",
		Arguments: map[string]interface{}{"command": "sleep 2", "timeout_sec": float64(0)},
	})
	if !strings.HasPrefix(result, "exit=0\n") {
		t.Fatalf("expected exit=0 for timeout_sec=0, got: %s", result)
	}

	// Omitted timeout_sec → default (120s) → sleep 2 completes.
	result = ag.executeTool(deepseek.ToolCall{
		Name:      "bash",
		Arguments: map[string]interface{}{"command": "sleep 2"},
	})
	if !strings.HasPrefix(result, "exit=0\n") {
		t.Fatalf("expected exit=0 for omitted timeout_sec, got: %s", result)
	}
}

func TestAgent_Run_StreamFallback(t *testing.T) {
	// Track how many times the mock is called.
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		req := claudeRequestFrom(t, r)

		switch callCount {
		case 1:
			// First call is the streaming attempt — reject it so
			// ClaudeChatStream() fails and Run() falls back to ClaudeChat().
			if !req.Stream {
				t.Errorf("expected stream=true on first call, got stream=%v", req.Stream)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "streaming not supported"}) //stylegate:ignore encodes a stub response; a failure surfaces as a client-side decode error in the same test

		case 2:
			// Second call is the non-streaming fallback (end_turn #1).
			if req.Stream {
				t.Errorf("expected stream=false on fallback call, got stream=%v", req.Stream)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":          "resp-2",
				"type":        "message",
				"role":        "assistant",
				"model":       "deepseek-v4-flash",
				"content":     []map[string]interface{}{{"type": "text", "text": "Fallback response: streaming was unavailable."}},
				"stop_reason": "end_turn",
				"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			}) //stylegate:ignore encodes a stub response; a failure surfaces as a client-side decode error in the same test

		case 3:
			// Third call: confirmation round streams normally.
			w.Header().Set("Content-Type", "text/event-stream")
			claudeTextStream(w, "Confirmed.", "end_turn")

		default:
			t.Errorf("unexpected call count: %d", callCount)
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

	result, err := ag.Run("hello")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	expected := "Fallback response: streaming was unavailable."
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 API calls (stream attempt + fallback + confirmation), got %d", callCount)
	}
}

func TestAgent_Run_StreamsReasoningToStderr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// The request must carry thinking enabled (the new default).
		req := claudeRequestFrom(t, r)
		if req.Thinking == nil || req.Thinking.Type != "enabled" {
			t.Errorf("expected thinking enabled in request, got %+v", req.Thinking)
		}

		// Stream: thinking block (2 deltas + signature) → text answer → end_turn.
		writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
		writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}")
		writeClaudeSSE(w, "content_block_delta", "{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Let me think\"}}")
		writeClaudeSSE(w, "content_block_delta", "{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\" about it.\"}}")
		writeClaudeSSE(w, "content_block_delta", "{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig_1\"}}")
		writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
		writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}")
		writeClaudeSSE(w, "content_block_delta", "{\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"The answer is 42.\"}}")
		writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":1}")
		writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":20}}")
		writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
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
		Thinking:      &deepseek.Thinking{Type: "enabled"},
	}

	// Capture stderr to verify thinking is displayed live before the answer.
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	result, runErr := ag.Run("Hello")
	w.Close() //stylegate:ignore test restores os.Stderr after capturing through an os.Pipe; the close error cannot lose already-buffered output read two lines below
	os.Stderr = oldStderr
	captured, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
	r.Close()

	if runErr != nil {
		t.Fatalf("Run() error: %v", runErr)
	}
	if !strings.Contains(result, "The answer is 42.") {
		t.Fatalf("expected answer in result, got %q", result)
	}

	stderr := string(captured)
	if !strings.Contains(stderr, "[thinking] Let me think about it.") {
		t.Fatalf("expected thinking text on stderr with prefix, got:\n%s", stderr)
	}
	ti := strings.Index(stderr, "[thinking]")
	ai := strings.Index(stderr, "The answer is 42.")
	if ti == -1 || ai == -1 || ti > ai {
		t.Fatalf("expected thinking before answer on stderr, got:\n%s", stderr)
	}
}

// ---------------------------------------------------------------------------
// Thinking display + thinking-default tests (bug008)
// ---------------------------------------------------------------------------

// TestAgent_Run_ThinkingBudgetDowngrade covers the harness-enforced thinking
// budget (devdoc/thinking-budget.md): DeepSeek's Anthropic-compatible
// endpoint accepts budget_tokens but does NOT enforce it (verified live), so
// when a round's streamed thinking exceeds the budget the harness must abort
// the stream and re-issue the round with thinking disabled. The budget is a
// hard cap, not a hint.
func TestAgent_Run_ThinkingBudgetDowngrade(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		req := claudeRequestFrom(t, r)

		switch callCount {
		case 1:
			// First attempt: thinking enabled with a tiny budget, and the
			// mock streams far more thinking than the budget allows. The
			// harness must abort the stream and re-issue the round.
			if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 16 {
				t.Errorf("expected thinking enabled with 16-token budget on first attempt, got %+v", req.Thinking)
			}
			writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}")
			// 16-token budget = 64 chars; three 50-char deltas blow through it.
			for i := 0; i < 3; i++ {
				writeClaudeSSE(w, "content_block_delta",
					fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%q}}",
						strings.Repeat("x", 50)))
			}
			// The client aborts mid-stream; whatever follows is discarded.
			claudeTextStream(w, "I was thinking too much.", "end_turn")

		case 2:
			// Re-issue: thinking must now be disabled — the budget held. The
			// re-issued round produces a tool_use (generated WITHOUT thinking);
			// the next round's request must carry the injected empty thinking
			// block or the API rejects the tool round (verified live).
			if req.Thinking == nil || req.Thinking.Type != "disabled" {
				t.Errorf("expected thinking disabled on re-issue, got %+v", req.Thinking)
			}
			claudeToolUseStream(w, "read", map[string]interface{}{"path": "/tmp/any"})

		case 3:
			// Next round, thinking enabled again: the assistant message with
			// the re-issued round's tool_use must carry the harness-injected
			// empty thinking block.
			if req.Thinking == nil || req.Thinking.Type != "enabled" {
				t.Errorf("expected thinking enabled on round after re-issue, got %+v", req.Thinking)
			}
			asst := claudeLastAssistant(req)
			if asst == nil {
				t.Fatalf("expected an assistant message in the request")
			}
			foundPlaceholder := false
			for _, b := range asst.Content {
				if b.Type == "thinking" && b.Thinking == " " && b.Signature == deepseek.EmptyThinkingSignature {
					foundPlaceholder = true
				}
			}
			if !foundPlaceholder {
				t.Errorf("expected injected empty thinking block before the re-issued round's tool_use, got %+v", asst.Content)
			}
			claudeTextStream(w, "Done with the task.", "end_turn")

		case 4:
			// Confirmation round (bug022): second consecutive end_turn exits.
			claudeTextStream(w, "All tasks completed.", "end_turn")

		default:
			t.Errorf("unexpected call count: %d", callCount)
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
		Thinking:      &deepseek.Thinking{Type: "enabled", BudgetTokens: 16},
	}

	result, err := ag.Run("Complete the task")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "Done with the task.") {
		t.Fatalf("expected the re-issued round's answer in result, got: %q", result)
	}
	if callCount != 4 {
		t.Fatalf("expected 4 API calls (over-budget attempt + disabled re-issue + tool round + confirmation), got %d", callCount)
	}
}

// memoryDBEnv / memoryDBEnvLegacy are the knowledge-base path overrides
// (internal/tools/memoryDBEnv + memoryDBEnvLegacy). They are re-stated here
// because tools keeps them unexported, and this package MUST clear them:
// memoryDBPath() prefers them over GOPI_HOME, so an ambient export (an e2e
// run's `GOPI_MEMORY_DB=<real db>`) would otherwise be read straight by
// MemoryHotResident() inside the agent under test.
// TestTestEnvIsolation_* pins the pair against a rename (Rule 13).
const (
	memoryDBEnv       = "GOPI_MEMORY_DB"
	memoryDBEnvLegacy = "GOPI_HIPPO_DB"
)

// isolateMemoryEnv removes the knowledge-base and hippocampus overrides from
// the agent-under-test's view: both DB names point at dbPath (a path that
// does not exist, so the read resolves to the EMPTY isolated base — the
// hot-resident digest stays ""), and the gate starts off.
func isolateMemoryEnv(t *testing.T, dbPath string) {
	t.Helper()
	t.Setenv(memoryDBEnv, dbPath)
	t.Setenv(memoryDBEnvLegacy, "")
	t.Setenv(hippocampusEnvEnabled, "")
}

// newTestAgentEnv isolates NewAgent from the real user profile and env:
// HOME + settings point into a temp dir, an API key is provided, raw logging
// is off, and the thinking-related env vars start cleared.
//
// The list is a DENYLIST-CLEARING contract, not a whitelist: an env var the
// helper does not name stays visible to the agent, so anything that steers
// behaviour (data roots, gates, overrides) must appear here. The memory DB
// names did not, which let a leftover e2e export leak a real DB into the
// task-start hot-resident digest (env-leak incident, memory #1618).
func newTestAgentEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOPI_HOME", home) // gopi data root isolation (sessions/raw/memory)
	// The DB overrides outrank GOPI_HOME, so pin them explicitly (and keep
	// the default path inside the same temp home).
	isolateMemoryEnv(t, filepath.Join(home, "memory.db"))
	t.Setenv("GOPI_SETTINGS", filepath.Join(home, "no-settings.json"))
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("GOPI_RAW_LOG", "OFF")
	t.Setenv("GOPI_THINKING", "")
	t.Setenv("GOPI_THINKING_BUDGET", "")
	t.Setenv("GOPI_HIDE_THINKING", "")
	t.Setenv("GOPI_REASONING_EFFORT", "")
	t.Setenv("GOPI_MAX_TOKENS", "")
}

// TestSystemPrompt_IncludesBashEfficiencyRules guards the measured
// efficiency levers (race 2026-08-06): dense single commands, git --all
// awareness, cheap verification — the prompt must keep them.
func TestSystemPrompt_IncludesBashEfficiencyRules(t *testing.T) {
	newTestAgentEnv(t)
	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	// "Never cat a large file whole" was reworded 2026-09-07 (review A1): the
	// read-vs-sed distinction is now stated ONCE, on the read routing line, so
	// the two sections cannot prescribe opposite tools for the same job.
	for _, want := range []string{"BASH EFFICIENCY RULES", "git log --all", "not shell cat/head/sed"} {
		if !strings.Contains(ag.SystemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

// TestSystemPrompt_CounterExampleRoutingRules guards the A1 rework
// (competition #4, 2026-08-21): the tool routing rules must use the dense
// counter-example style measured in dsh's 4.1K-character system prompt —
// one line per tool, "Use X — not shell Y", plus when-to-parallelize — so the
// model stops narrating tool choices in thinking and just calls the tool.
func TestSystemPrompt_CounterExampleRoutingRules(t *testing.T) {
	newTestAgentEnv(t)
	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	p := ag.SystemPrompt
	for _, want := range []string{
		// Per-tool one-liners with counter-examples.
		"Use read — not shell cat/head/sed",
		"not write",
		"not shell grep",
		"not for reading files or searching code",
		// When to parallelize (moved into Efficiency rule 1 — the old
		// Discipline 4 duplicated it and was dropped in the persuasion draft
		// §3.1 deletion list).
		"all independent tool calls in ONE response",
		// Thinking target (claude median 150-280 chars).
		"under ~250 characters",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing counter-example rule %q", want)
		}
	}
	// The routing block must still teach the code_search-first discipline.
	if !strings.Contains(p, `Call code_search FIRST with the bare identifier`) {
		t.Error("system prompt missing the code_search-first discipline")
	}
}

func TestNewAgent_ThinkingDefaultDisabled(t *testing.T) {
	newTestAgentEnv(t)

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.Thinking == nil {
		t.Fatal("expected non-nil Thinking (explicit disabled by default)")
	}
	if ag.Thinking.Type != "disabled" {
		t.Errorf("expected Thinking.Type=disabled, got %q", ag.Thinking.Type)
	}
	// ReasoningEffort keeps the resolved default (minimal) internally, but
	// stays OFF the wire while thinking is disabled — it is only sent when
	// thinking is enabled (PA ruling 2026-08-27; the wire check itself is
	// TestNewAgent_DefaultMinimalWired).
	if ag.ReasoningEffort != "minimal" {
		t.Errorf("expected ReasoningEffort=minimal internally, got %q", ag.ReasoningEffort)
	}
	if ag.HideThinking {
		t.Error("expected HideThinking=false by default")
	}
}

func TestNewAgent_ThinkingDisabledOverride(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_THINKING", "disabled")

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "disabled" {
		t.Errorf("expected Thinking.Type=disabled, got %+v", ag.Thinking)
	}
}

func TestNewAgent_ThinkingEnabledEscapeHatch(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_THINKING", "enabled")

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "enabled" {
		t.Errorf("expected Thinking.Type=enabled (explicit opt-in), got %+v", ag.Thinking)
	}
}

func TestNewAgent_ThinkingInvalidFallsBackToDisabled(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_THINKING", "banana")

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "disabled" {
		t.Errorf("expected Thinking.Type=disabled fallback, got %+v", ag.Thinking)
	}
}

// TestNewAgent_ThinkingBudgetOverride pins the GOPI_THINKING_BUDGET env var
// (documented but never parsed before the thinking-budget fix — the budget
// was always the package default). The harness enforces this budget against
// streamed thinking; invalid values warn and fall back to the default.
func TestNewAgent_ThinkingBudgetOverride(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_THINKING_BUDGET", "512")

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.BudgetTokens != 512 {
		t.Errorf("expected BudgetTokens=512 from GOPI_THINKING_BUDGET, got %+v", ag.Thinking)
	}

	// Invalid values warn and leave the budget unset (0 = the package default,
	// applied at request build time by deepseek.EffectiveThinkingBudget); thinking stays at the
	// default (disabled).
	t.Setenv("GOPI_THINKING_BUDGET", "banana")
	ag, err = NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.Thinking == nil || ag.Thinking.Type != "disabled" || ag.Thinking.BudgetTokens != 0 {
		t.Errorf("expected disabled thinking with unset budget after invalid GOPI_THINKING_BUDGET, got %+v", ag.Thinking)
	}
}

func TestNewAgent_HideThinkingEnv(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_HIDE_THINKING", "1")

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if !ag.HideThinking {
		t.Error("expected HideThinking=true with GOPI_HIDE_THINKING=1")
	}
}

// TestNewAgent_HideThinkingInvertedValues — P1-4 regression: hideThinking
// used `os.Getenv != ""`, so GOPI_HIDE_THINKING=0/off/false ENABLED hiding
// (inverted vs the EnvBool truthy-only convention, and vs the "Set to 1"
// catalog doc). Truthy-only parsing: every inverted form must NOT hide.
func TestNewAgent_HideThinkingInvertedValues(t *testing.T) {
	for _, v := range []string{"0", "off", "false", "no", "disabled", ""} {
		newTestAgentEnv(t)
		t.Setenv("GOPI_HIDE_THINKING", v)
		ag, err := NewAgent("deepseek-v4-pro")
		if err != nil {
			t.Fatalf("NewAgent error: %v", err)
		}
		if ag.HideThinking {
			t.Errorf("GOPI_HIDE_THINKING=%q must NOT hide thinking (truthy-only; inverted forms are off)", v)
		}
	}
	// Truthy forms still hide.
	for _, v := range []string{"1", "true", "on", "yes", "enabled"} {
		newTestAgentEnv(t)
		t.Setenv("GOPI_HIDE_THINKING", v)
		ag, err := NewAgent("deepseek-v4-pro")
		if err != nil {
			t.Fatalf("NewAgent error: %v", err)
		}
		if !ag.HideThinking {
			t.Errorf("GOPI_HIDE_THINKING=%q must hide thinking", v)
		}
	}
}

// TestNewAgent_KeepThinkingInvertedValues — P1-4 regression, same class:
// GOPI_KEEP_THINKING=0/off must NOT replay reasoning (only truthy values do).
func TestNewAgent_KeepThinkingInvertedValues(t *testing.T) {
	for _, v := range []string{"0", "off", "false", "no", "disabled", ""} {
		newTestAgentEnv(t)
		t.Setenv("GOPI_KEEP_THINKING", v)
		ag, err := NewAgent("deepseek-v4-pro")
		if err != nil {
			t.Fatalf("NewAgent error: %v", err)
		}
		if ag.KeepThinking {
			t.Errorf("GOPI_KEEP_THINKING=%q must NOT keep thinking (truthy-only)", v)
		}
	}
	for _, v := range []string{"1", "true", "on", "yes", "enabled"} {
		newTestAgentEnv(t)
		t.Setenv("GOPI_KEEP_THINKING", v)
		ag, err := NewAgent("deepseek-v4-pro")
		if err != nil {
			t.Fatalf("NewAgent error: %v", err)
		}
		if !ag.KeepThinking {
			t.Errorf("GOPI_KEEP_THINKING=%q must keep thinking", v)
		}
	}
}

func TestNewAgent_MaxTokensDefault(t *testing.T) {
	newTestAgentEnv(t)

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.MaxTokens != deepseek.DefaultClaudeMaxTokens {
		t.Errorf("MaxTokens = %d, want default %d", ag.MaxTokens, deepseek.DefaultClaudeMaxTokens)
	}
}

func TestNewAgent_MaxTokensOverride(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_MAX_TOKENS", "65536")

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.MaxTokens != 65536 {
		t.Errorf("MaxTokens = %d, want 65536", ag.MaxTokens)
	}
}

func TestNewAgent_MaxTokensInvalidFallsBackToDefault(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_MAX_TOKENS", "banana")

	ag, err := NewAgent("deepseek-v4-pro")
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	if ag.MaxTokens != deepseek.DefaultClaudeMaxTokens {
		t.Errorf("MaxTokens = %d, want default %d", ag.MaxTokens, deepseek.DefaultClaudeMaxTokens)
	}
}

func TestThinkingMarkers(t *testing.T) {
	tty := newThinkingMarkers(true)
	if tty.open != ansiDim+thinkingPrefix {
		t.Errorf("TTY open = %q, want %q", tty.open, ansiDim+thinkingPrefix)
	}
	if tty.close != ansiReset+"\n" {
		t.Errorf("TTY close = %q, want %q", tty.close, ansiReset+"\n")
	}

	plain := newThinkingMarkers(false)
	if plain.open != thinkingPrefix {
		t.Errorf("non-TTY open = %q, want %q", plain.open, thinkingPrefix)
	}
	if plain.close != "\n" {
		t.Errorf("non-TTY close = %q, want %q", plain.close, "\n")
	}
}

// TestAgent_Run_StreamsReasoningToStderr verifies Task 1 end-to-end via the
// mock API: reasoning deltas are printed to stderr with the "[thinking] "
// prefix BEFORE the final answer, the request carries thinking enabled, and
// the final answer is still returned as the result.
// ---------------------------------------------------------------------------
// R3 — trust a completed end_turn (perf-vs-claude-fix-plan.md)
// ---------------------------------------------------------------------------

// completedTaskListResponse is an end_turn response that declares the task
// list all-completed with a substantive answer.
const completedTaskListResponse = "All tasks are complete. The bug is fixed, " +
	"all tests pass, and the changes are committed.\n" +
	"[TASK_LIST_BEGIN]\n" +
	`[{"task_id":"1","summary":"Fix bug","status":"completed"}]` + "\n" +
	"[TASK_LIST_END]"

// TestAgent_Run_ExitsOnFirstCompletedEndTurn verifies R3: a first end_turn
// whose task list is all-completed with a substantive answer exits on the
// FIRST round — no confirmation round, exactly 1 API call (previously 2).
func TestAgent_Run_ExitsOnFirstCompletedEndTurn(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, completedTaskListResponse, "end_turn")
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

	result, err := ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected the substantive answer, got: %q", result)
	}
	// Symmetric task-end memory_store enforcement: a completed run that never
	// called memory_store gets ONE injected completion-checkpoint round to
	// persist its experience before the final response (R3's confirmation-round
	// elimination is preserved — the extra call here is the mandatory store
	// round, not a bug022 confirmation). The stub always returns the completed
	// response, so the second call still carries the substantive answer and
	// the loop then exits (completionStorePrompted latches after the first).
	if callCount != 2 {
		t.Fatalf("expected 2 API calls (trusted completed end_turn R3 + one task-end store round), got %d", callCount)
	}
	if !ag.completionStorePrompted {
		t.Fatalf("completion checkpoint must have been injected for this completed-but-never-stored run")
	}
	if n := countUserMessages(ag, completionCheckpointMessage); n != 1 {
		t.Fatalf("expected the completion checkpoint injected exactly once, got %d", n)
	}
}

// TestAgent_Run_KeepsConfirmationForProgressingList verifies the bug022 guard
// stays for mid-task stops: a first end_turn whose task list still has
// progressing tasks keeps the confirmation round (2 API calls) and the
// injected continuation message.
func TestAgent_Run_KeepsConfirmationForProgressingList(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		if callCount == 1 {
			progressing := "Working on the fix.\n[TASK_LIST_BEGIN]\n" +
				`[{"task_id":"1","summary":"Fix bug","status":"progressing"}]` + "\n" +
				"[TASK_LIST_END]"
			claudeTextStream(w, progressing, "end_turn")
			return
		}
		claudeTextStream(w, "Confirmed.", "end_turn")
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
		// The exit-path warning ("run exiting with N progressing task(s)")
		// logs on a real exit whose task list still has progressing entries
		// — the test agent needs a Logger for it.
		Logger: log.New(io.Discard, "", 0),
	}

	result, err := ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "Working on the fix") {
		t.Fatalf("expected first end_turn's answer as result, got: %q", result)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 API calls (confirmation kept for progressing tasks), got %d", callCount)
	}
}

// TestAgent_Run_KeepsConfirmationForTerseCompletedList verifies the R3
// substantive-answer guard: an all-completed list with a terse (or empty)
// answer is NOT trusted on the first round — the confirmation round stays,
// so the run never exits with a meaningless result.
func TestAgent_Run_KeepsConfirmationForTerseCompletedList(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		if callCount == 1 {
			terse := "Done.\n[TASK_LIST_BEGIN]\n" +
				`[{"task_id":"1","summary":"Fix bug","status":"completed"}]` + "\n" +
				"[TASK_LIST_END]"
			claudeTextStream(w, terse, "end_turn")
			return
		}
		claudeTextStream(w, "Confirmed.", "end_turn")
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

	if _, err := ag.Run("Fix the bug"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 API calls (terse completed answer not trusted), got %d", callCount)
	}
	// The terse completed answer must STILL trigger the task-end store
	// enforcement: the completion checkpoint replaces the bug022 nudge (the
	// task is declared complete), so the model is forced to memory_store
	// before the final response — the terse-answer exit must not bypass it.
	if !ag.completionStorePrompted {
		t.Fatalf("terse completed answer must inject the completion checkpoint (task-end store enforcement)")
	}
}

// TestContentWithoutTaskList verifies the R3 stripping helper: everything
// before the markers survives (including the answer text that precedes the
// block), the block itself and anything after it is dropped.
func TestContentWithoutTaskList(t *testing.T) {
	// completedTaskListResponse carries its answer BEFORE the markers — the
	// helper must keep the answer and drop the JSON block.
	if got := contentWithoutTaskList("Answer text.\n" + completedTaskListResponse); got != "Answer text.\nAll tasks are complete. The bug is fixed, all tests pass, and the changes are committed." {
		t.Fatalf("expected answer text only, got %q", got)
	}
	// Only a task list, no answer → empty.
	if got := contentWithoutTaskList(completedTaskListResponse); got != "All tasks are complete. The bug is fixed, all tests pass, and the changes are committed." {
		t.Fatalf("expected the pre-marker answer to survive, got %q", got)
	}
	if got := contentWithoutTaskList("no markers here"); got != "no markers here" {
		t.Fatalf("expected unchanged content, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Parallel tool-call execution (feat/parallel-claude)
// ---------------------------------------------------------------------------

// concurrentTracker is a small test helper that measures the maximum number
// of tool executions overlapping at any instant (via an atomic counter), so
// tests can assert concurrency vs serial execution deterministically.
type concurrentTracker struct {
	cur int32
	max int32
}

func (ct *concurrentTracker) enter() {
	c := atomic.AddInt32(&ct.cur, 1)
	for {
		m := atomic.LoadInt32(&ct.max)
		if c <= m || atomic.CompareAndSwapInt32(&ct.max, m, c) {
			break
		}
	}
}

func (ct *concurrentTracker) leave() {
	atomic.AddInt32(&ct.cur, -1)
}

func (ct *concurrentTracker) maxConcurrent() int {
	return int(atomic.LoadInt32(&ct.max))
}

// TestExecuteToolBatch_SingleCallSerial verifies the regression guard: a
// single tool call takes the serial path and still returns its result.
func TestExecuteToolBatch_SingleCallSerial(t *testing.T) {
	ag := &Agent{executeToolFn: func(tc deepseek.ToolCall) string {
		return "serial-result-" + tc.ID
	}}

	results := ag.executeToolBatch([]deepseek.ToolCall{{ID: "call_0", Name: "read"}})
	if len(results) != 1 || results[0] != "serial-result-call_0" {
		t.Fatalf("expected single serial result, got %v", results)
	}
}

// TestExecuteToolBatch_ParallelOrdered verifies that an all-whitelisted round
// runs its calls CONCURRENTLY and still returns results in tool-call order.
// call_0 sleeps longest, so its completion order is the REVERSE of call
// order — an implementation that appends in completion order would put a
// fast call's result at index 0.
func TestExecuteToolBatch_ParallelOrdered(t *testing.T) {
	var ct concurrentTracker

	ag := &Agent{executeToolFn: func(tc deepseek.ToolCall) string {
		ct.enter()
		defer ct.leave()
		if tc.ID == "call_0" {
			time.Sleep(300 * time.Millisecond) //stylegate:allow-sleep tool-call concurrency window: the slow call must still be in flight// slowest — finishes LAST
		} else {
			time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep tool-call concurrency window: the slow call must still be in flight
		}
		return "result-" + tc.ID
	}}

	calls := []deepseek.ToolCall{
		{ID: "call_0", Name: "read"},
		{ID: "call_1", Name: "code_search"},
		{ID: "call_2", Name: "keyword_search"},
	}
	results := ag.executeToolBatch(calls)

	if max := ct.maxConcurrent(); max < 2 {
		t.Fatalf("expected concurrent execution (max concurrent >= 2), got %d", max)
	}
	want := []string{"result-call_0", "result-call_1", "result-call_2"}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("results out of order: got %v, want %v", results, want)
		}
	}
}

// TestExecuteToolBatch_MixedParallelizesReadSubset verifies R4: in a round
// mixing read-only tools with a side-effect tool (write), the read-only
// subset runs CONCURRENTLY while the side-effect tool stays serial — all
// calls sleep the same duration, so max concurrency is exactly the
// read-subset size (2), never 3 — and results still come back in call order
// with the write's result at its original index.
func TestExecuteToolBatch_MixedParallelizesReadSubset(t *testing.T) {
	var ct concurrentTracker

	ag := &Agent{executeToolFn: func(tc deepseek.ToolCall) string {
		ct.enter()
		defer ct.leave()
		time.Sleep(200 * time.Millisecond) //stylegate:allow-sleep tool-call concurrency window: the slow call must still be in flight
		return "result-" + tc.ID
	}}

	calls := []deepseek.ToolCall{
		{ID: "call_read", Name: "read"},
		{ID: "call_write", Name: "write"},
		{ID: "call_kw", Name: "keyword_search"},
	}
	results := ag.executeToolBatch(calls)

	// The two reads overlap (max reaches 2); the write never overlaps them
	// (max would be 3 if it ran concurrently with a read).
	if max := ct.maxConcurrent(); max != 2 {
		t.Fatalf("expected read subset concurrent, write serial (max == 2), got %d", max)
	}
	want := []string{"result-call_read", "result-call_write", "result-call_kw"}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("results out of order: got %v, want %v", results, want)
		}
	}
}

// TestExecuteToolBatch_MixedSingleReadSerial verifies a mixed round with only
// ONE read-only call stays fully serial (nothing to overlap with) and keeps
// call order.
func TestExecuteToolBatch_MixedSingleReadSerial(t *testing.T) {
	var ct concurrentTracker

	ag := &Agent{executeToolFn: func(tc deepseek.ToolCall) string {
		ct.enter()
		defer ct.leave()
		time.Sleep(10 * time.Millisecond) //stylegate:allow-sleep tool-call concurrency window: the slow call must still be in flight
		return "result-" + tc.ID
	}}

	calls := []deepseek.ToolCall{
		{ID: "call_read", Name: "read"},
		{ID: "call_bash", Name: "bash"},
	}
	results := ag.executeToolBatch(calls)

	if max := ct.maxConcurrent(); max != 1 {
		t.Fatalf("expected serial execution for single-read round (max == 1), got %d", max)
	}
	want := []string{"result-call_read", "result-call_bash"}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("results out of order: got %v, want %v", results, want)
		}
	}
}

// TestExecuteToolBatchParallel_TimeoutOthersStillReturn verifies the batch
// deadline: a hung call yields a timeout error result while the other calls
// in the same round still return normally, and the batch returns well within
// the (shortened) deadline.
func TestExecuteToolBatchParallel_TimeoutOthersStillReturn(t *testing.T) {
	blocked := make(chan struct{}) // never closed — simulates a hung tool
	ag := &Agent{
		parallelTimeout: 150 * time.Millisecond,
		executeToolFn: func(tc deepseek.ToolCall) string {
			if tc.ID == "call_hang" {
				<-blocked // hang forever
			}
			return "fast-result-" + tc.ID
		},
	}

	calls := []deepseek.ToolCall{
		{ID: "call_hang", Name: "read"},
		{ID: "call_ok", Name: "keyword_search"},
	}
	start := time.Now()
	results := ag.executeToolBatch(calls)
	elapsed := time.Since(start)

	if !strings.Contains(results[0], "timed out") {
		t.Fatalf("expected timeout error for hung call, got %q", results[0])
	}
	if results[1] != "fast-result-call_ok" {
		t.Fatalf("expected fast call to return normally, got %q", results[1])
	}
	if elapsed > 2*time.Second {
		t.Fatalf("batch took %s — expected ~150ms (timeout bound)", elapsed)
	}
}

// TestAgent_Run_ParallelReadsEndToEnd drives the full Run() loop with a round
// of TWO concurrent read calls and verifies the tool results arrive back at
// the model as separate messages, in call order.
func TestAgent_Run_ParallelReadsEndToEnd(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")
	contentA := "content-of-file-a\n"
	contentB := "content-of-file-b\n"
	if err := os.WriteFile(fileA, []byte(contentA), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := os.WriteFile(fileB, []byte(contentB), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	argsA, _ := json.Marshal(map[string]interface{}{"path": fileA}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsB, _ := json.Marshal(map[string]interface{}{"path": fileB}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsAJSON, _ := json.Marshal(string(argsA))                     //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)// JSON-encoded string for the wire
	argsBJSON, _ := json.Marshal(string(argsB))                     //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")

		req := claudeRequestFrom(t, r)

		switch callCount {
		case 1:
			// Round 1: two read tool calls in a single round (Claude SSE).
			writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_a\",\"name\":\"read\",\"input\":{}}}")
			writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", argsAJSON))
			writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_b\",\"name\":\"read\",\"input\":{}}}")
			writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", argsBJSON))
			writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":1}")
			writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
			writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")

		case 2:
			// Round 2: both tool results must be present as separate tool_result
			// blocks, in call order (A before B), then final answer.
			var toolResults []string
			for _, msg := range req.Messages {
				for _, b := range msg.Content {
					if b.Type == "tool_result" {
						toolResults = append(toolResults, fmt.Sprint(b.Content))
					}
				}
			}
			if len(toolResults) != 2 {
				t.Errorf("expected 2 tool result messages, got %d", len(toolResults))
			}
			if !strings.Contains(toolResults[0], contentA) || !strings.Contains(toolResults[1], contentB) {
				t.Errorf("expected tool results in call order (A, B), got: %v", toolResults)
			}

			finalText := "Read both files. A: " + contentA + " B: " + contentB
			claudeTextStream(w, finalText, "end_turn")

		case 3:
			// Third call: second consecutive end_turn (bug022 fix) — exits.
			claudeTextStream(w, "Done.", "end_turn")

		default:
			t.Errorf("unexpected call count: %d", callCount)
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

	result, err := ag.Run("Read both files")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, contentA) || !strings.Contains(result, contentB) {
		t.Fatalf("expected both file contents in result, got: %q", result)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 API calls, got %d", callCount)
	}
}

// ---------------------------------------------------------------------------

// TestCodeSearchScopeCoversIndexedLanguages: the code_search scope must glob
// the same languages ctags indexes — Go, Python, shell, C++, Rust, JavaScript
// and TypeScript at minimum — so the ripgrep fallback never searches a
// narrower set than the ctags index.
func TestCodeSearchScopeCoversIndexedLanguages(t *testing.T) {
	scope := codeSearchScope()
	joined := strings.Join(scope.IncludeGlob, "\n")
	for _, want := range []string{"**/*.go", "**/*.py", "**/*.sh", "**/*.bash",
		"**/*.js", "**/*.jsx", "**/*.mjs", "**/*.cjs", "**/*.ts", "**/*.tsx"} {
		if !strings.Contains(joined, want) {
			t.Errorf("codeSearchScope include globs %v missing %q", scope.IncludeGlob, want)
		}
	}
}

// TestAgent_Run_ThinkingDowngradeLatch covers the maxThinkingAbortStreak
// latch (gopi-race R5-R6 post-mortem): two consecutive rounds whose thinking
// blows the budget downgrade the WHOLE SESSION to thinking-disabled requests
// — the remaining rounds pay one prefill instead of the abort round's two.
func TestAgent_Run_ThinkingDowngradeLatch(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		req := claudeRequestFrom(t, r)

		switch callCount {
		case 1:
			// Round 1, attempt 1: thinking enabled, mock streams past the
			// 16-token budget (64 chars) → harness aborts.
			if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 16 {
				t.Errorf("call 1: expected thinking enabled with 16-token budget, got %+v", req.Thinking)
			}
			writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}")
			for i := 0; i < 3; i++ {
				writeClaudeSSE(w, "content_block_delta",
					fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%q}}",
						strings.Repeat("x", 50)))
			}
			claudeTextStream(w, "Thinking too much.", "end_turn")

		case 2:
			// Round 1, attempt 2 (re-issue): thinking disabled → tool round.
			if req.Thinking == nil || req.Thinking.Type != "disabled" {
				t.Errorf("call 2: expected thinking disabled on re-issue, got %+v", req.Thinking)
			}
			claudeToolUseStream(w, "read", map[string]interface{}{"path": "/tmp/a"})

		case 3:
			// Round 2, attempt 1: thinking enabled again (streak 1 < latch) —
			// blows the budget again → abort.
			if req.Thinking == nil || req.Thinking.Type != "enabled" {
				t.Errorf("call 3: expected thinking enabled before the second abort, got %+v", req.Thinking)
			}
			writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
			writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}")
			for i := 0; i < 3; i++ {
				writeClaudeSSE(w, "content_block_delta",
					fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%q}}",
						strings.Repeat("y", 50)))
			}
			claudeTextStream(w, "Still thinking too much.", "end_turn")

		case 4:
			// Round 2, attempt 2: disabled re-issue → tool round. This abort
			// latches the session-level downgrade (streak reached 2).
			if req.Thinking == nil || req.Thinking.Type != "disabled" {
				t.Errorf("call 4: expected thinking disabled on re-issue, got %+v", req.Thinking)
			}
			claudeToolUseStream(w, "read", map[string]interface{}{"path": "/tmp/b"})

		case 5:
			// Round 3: the latch must keep thinking disabled — no more
			// abort attempts, one prefill per round.
			if req.Thinking == nil || req.Thinking.Type != "disabled" {
				t.Errorf("call 5: expected session-level thinking disabled after 2 aborts, got %+v", req.Thinking)
			}
			claudeTextStream(w, "Done with the task.", "end_turn")

		case 6:
			// Confirmation round (bug022): still disabled.
			if req.Thinking == nil || req.Thinking.Type != "disabled" {
				t.Errorf("call 6: expected thinking disabled on confirmation round, got %+v", req.Thinking)
			}
			claudeTextStream(w, "All tasks completed.", "end_turn")

		default:
			t.Errorf("unexpected call count: %d", callCount)
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
		MaxIterations: 6,
		Thinking:      &deepseek.Thinking{Type: "enabled", BudgetTokens: 16},
	}

	result, err := ag.Run("Complete the task")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "Done with the task.") {
		t.Fatalf("expected the answer in result, got: %q", result)
	}
	if callCount != 6 {
		t.Fatalf("expected 6 API calls (2x abort+re-issue, 2 latched rounds), got %d", callCount)
	}
}

func TestBoolEnvTrue(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"on", true},
		{"yes", true},
		{"TRUE", true},
		{"Enabled", true},
		{"0", false},
		{"off", false},
		{"false", false},
		{"no", false},
		{"", false},
		{"garbage", false},
	}
	for _, c := range cases {
		t.Setenv("GOPI_DEBUG_TEST_ENV", c.val)
		if got := boolEnvTrue("GOPI_DEBUG_TEST_ENV"); got != c.want {
			t.Errorf("boolEnvTrue(%q) = %v, want %v", c.val, got, c.want)
		}
	}
}

// TestAgent_Run_AccountLevelSkipsSameProviderFallback is the same-provider
// fallback guard for account-level failures (epic
// fix/llm-account-error-classification).
//
// The pre-fix behaviour (23:00 HP worker death log, devdoc/night-report-20260910.md):
// a GLM account out of balance answered the streaming attempt with HTTP 429
// {"code":"1113"}, the client burned 3 retries, and the agent then "fell back"
// to a non-streaming call against the SAME dead credential — 4+ wasted round
// trips on an account that could not possibly answer.
//
// Contract pinned here: ONE HTTP send, no fallback send, and the surfaced
// error is the actionable operator message naming the condition and remedy.
func TestAgent_Run_AccountLevelSkipsSameProviderFallback(t *testing.T) {
	const glmBalanceBody = `{"type":"error","error":{"type":"rate_limit_error","code":"1113","message":"[1113][余额不足或无可用资源包,请充值。]"}}`

	var sends int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		// Account-level signal, in the status the provider actually used.
		w.Header().Set("Retry-After", "60s")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(glmBalanceBody)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	defer server.Close()

	var logs strings.Builder
	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:     server.URL,
			APIKey:      "dead-key",
			HTTPClient:  server.Client(),
			RetryConfig: deepseek.RetryConfig{MaxRetries: 3, InitialDelay: time.Nanosecond, MaxDelay: time.Nanosecond},
		},
		Ctx:           &Context{},
		Model:         "glm-5.3-flash",
		MaxIterations: 5,
		Logger:        log.New(&logs, "", 0),
	}

	_, err := ag.Run("hello")
	if err == nil {
		t.Fatal("an account-level failure must abort the run")
	}

	// (1) No retry beyond the single scored attempt and (2) no same-provider
	// fallback: the streaming attempt is the ONLY HTTP send.
	if sends != 1 {
		t.Fatalf("account-level failure must not be retried or fall back to the same provider: %d HTTP sends, want 1", sends)
	}
	if strings.Contains(err.Error(), "fallback:") {
		t.Errorf("error must not be the stream→non-stream fallback wrap: %v", err)
	}

	// (3) The operator message names the condition AND the remedy.
	msg := err.Error()
	for _, want := range []string{
		deepseek.AccountLevelMessagePrefix,
		"account out of balance",
		"code 1113",
		"top up the provider account",
		"switch to another provider",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("operator message missing %q:\n%s", want, msg)
		}
	}
	// (4) The same message is logged for a operator watching the run.
	if !strings.Contains(logs.String(), "account out of balance") {
		t.Errorf("account-level condition must be logged: %s", logs.String())
	}
}

// TestAgent_Run_GenuineRateLimitStillFallsBack is the regression guard for
// the fallback path itself: a GENUINE rate limit (429 with no account code)
// keeps the pre-existing stream→non-stream recovery.
func TestAgent_Run_GenuineRateLimitStillFallsBack(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		switch callCount {
		case 1:
			if !req.Stream {
				t.Errorf("expected the streaming attempt first, got stream=%v", req.Stream)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
		default:
			// The non-streaming fallback answers normally — the recovery must
			// still work for a transient limit.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"id":          "resp-fallback",
				"type":        "message",
				"role":        "assistant",
				"model":       "glm-5.3-flash",
				"content":     []map[string]interface{}{{"type": "text", "text": "Recovered after rate limit."}},
				"stop_reason": "end_turn",
				"usage":       map[string]interface{}{"input_tokens": 5, "output_tokens": 3},
			}) //stylegate:ignore encodes a stub response; a failure surfaces as a client-side decode error in the same test
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:     server.URL,
			APIKey:      "test-key",
			HTTPClient:  server.Client(),
			RetryConfig: deepseek.RetryConfig{MaxRetries: 0, InitialDelay: time.Nanosecond, MaxDelay: time.Nanosecond},
		},
		Ctx:           &Context{},
		Model:         "glm-5.3-flash",
		MaxIterations: 2,
	}

	result, err := ag.Run("hello")
	if err != nil {
		t.Fatalf("genuine rate limit must still recover via the fallback: %v", err)
	}
	if result != "Recovered after rate limit." {
		t.Fatalf("fallback response not used: %q", result)
	}
	if callCount < 2 {
		t.Fatalf("expected the non-streaming fallback send, got %d calls", callCount)
	}
}
