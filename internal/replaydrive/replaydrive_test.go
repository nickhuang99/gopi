package replaydrive

// replaydrive_test.go — the parser's and the stub's contract, driven through
// their own surfaces. The recording fixtures are WRITTEN as session JSONL (the
// shape a live run produces) rather than read from a committed file: the JSONL
// format is the input, and a fixture that was not this format would test
// nothing.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/testutil"
)

// writeJSONL writes a session JSONL fixture OUTSIDE the source tree (Rule 32's
// device-backed-fixture rule: a scratch directory inside the tree is committed by
// a concurrent `git add`).
func writeJSONL(t *testing.T, name string, rows []string) string {
	t.Helper()
	dir := testutil.TestDeviceScratchDir(t, "replaydrive")
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil { //stylegate:ignore a scratch fixture, not a deliverable
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

// userRow, assistantRow, requestRow and resultRow build the four row shapes a
// live session log writes.
func userRow(text string) string {
	b, _ := json.Marshal(text) //stylegate:ignore marshalling a string cannot fail
	return fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%s}}`, b)
}

func assistantRow(blocks string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[%s]}}`, blocks)
}

func jsonTextBlock(s string) string {
	b, _ := json.Marshal(s) //stylegate:ignore marshalling a string cannot fail
	return fmt.Sprintf(`{"type":"text","text":%s}`, b)
}

func toolUseBlock(id, name, args string) string {
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":%s}`, id, name, args)
}

func resultRow(id, content string) string {
	b, _ := json.Marshal(content) //stylegate:ignore marshalling a string cannot fail
	return fmt.Sprintf(`{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%s}]}}`, id, b)
}

// requestRow is the RESPONSE BOUNDARY. Its message is absent, and that absence is
// the whole point of the parse (see LoadRecording).
func requestRow() string {
	return `{"type":"request","cache_hit":1,"cache_miss":2,"prompt_tokens":3}`
}

// TestLoadRecordingSplitsResponsesAtRequestRows is the regression guard for the
// measured defect: with the boundary handled after the message-nil skip, EVERY
// request row was dropped, all assistant blocks fused into ONE response, and the
// replay re-served that single tool-bearing response until the loop guard fired
// after 40 rounds (evidence replay4).
func TestLoadRecordingSplitsResponsesAtRequestRows(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		userRow("turn one"),
		assistantRow(jsonTextBlock("hello")),
		assistantRow(toolUseBlock("call_1", "bash", `{"command":"echo hi"}`)),
		requestRow(),
		resultRow("call_1", "exit=0\nhi\n"),
		assistantRow(jsonTextBlock("done")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	if got, want := rec.ResponseCount(), 2; got != want {
		t.Fatalf("responses = %d, want %d (the request row is the boundary)", got, want)
	}
	if got := len(rec.Responses[0]); got != 2 {
		t.Fatalf("response 0 blocks = %d, want 2 (a text block then a tool_use)", got)
	}
	if rec.Responses[0][1].Tool == nil || rec.Responses[0][1].Tool.Name != "bash" {
		t.Fatalf("response 0's second block is not the bash call: %+v", rec.Responses[0][1])
	}
	if got := rec.Responses[1][0].Text; got != "done" {
		t.Fatalf("response 1 text = %q, want %q", got, "done")
	}
	if got, ok := rec.Results["call_1"]; !ok || got != "exit=0\nhi\n" {
		t.Fatalf("recorded result = %q (ok=%v), want the row's content verbatim", got, ok)
	}
	if len(rec.Turns) != 1 {
		t.Fatalf("turns = %d, want 1 (one operator turn carries both responses)", len(rec.Turns))
	}
	if got := rec.RequestsThroughTurn(0); got != 2 {
		t.Fatalf("RequestsThroughTurn(0) = %d, want 2", got)
	}
}

// TestLoadRecordingDropsThinkingBlocks pins the deliberate drop: a thinking block
// never reached the screen, so re-serving it would make the replay show something
// the recording never showed.
func TestLoadRecordingDropsThinkingBlocks(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		userRow("q"),
		assistantRow(`{"type":"thinking","thinking":"private reasoning"},` + jsonTextBlock("the answer")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	if got := len(rec.Responses[0]); got != 1 {
		t.Fatalf("blocks = %d, want 1 (the thinking block is dropped)", got)
	}
	if got := rec.Responses[0][0].Text; got != "the answer" {
		t.Fatalf("block text = %q, want %q", got, "the answer")
	}
}

// TestLoadRecordingRefusesAScriptWithNoResponse is the Rule 15 arm: a recording
// with nothing to serve would start a real chat surface and report success having
// replayed nothing.
func TestLoadRecordingRefusesAScriptWithNoResponse(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{userRow("q")})
	if _, err := LoadRecording(p); err == nil {
		t.Fatal("LoadRecording accepted a recording with no model response")
	}
}

// TestStubServesTheScriptInRequestOrder drives the stub's OWN HTTP surface (a
// real request, a real SSE body) and reads back the frames the client parses.
func TestStubServesTheScriptInRequestOrder(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		userRow("q"),
		assistantRow(jsonTextBlock("first")),
		requestRow(),
		assistantRow(toolUseBlock("call_9", "read", `{"path":"a.txt"}`)),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	stub := NewStub(rec)
	defer stub.Close()

	body := func() string {
		resp, err := http.Post(stub.URL()+claudeMessagesPath, "application/json", strings.NewReader(`{"stream":true}`))
		if err != nil {
			t.Fatalf("POST the stub: %v", err)
		}
		defer resp.Body.Close()
		var buf strings.Builder
		if _, err := io.Copy(&buf, resp.Body); err != nil {
			t.Fatalf("read the stub's stream: %v", err)
		}
		return buf.String()
	}
	first := body()
	// The text arrives as FRAGMENTS (the surface's streaming renderer is what a
	// replay exists to film), so the assertion is on the concatenation, not on
	// one frame.
	if got := deltaText(t, first); got != "first" {
		t.Fatalf("request 1 served text %q, want %q:\n%s", got, "first", first)
	}
	if strings.Contains(first, `"type":"tool_use"`) {
		t.Fatalf("request 1 carried a tool block — it served response 1, not 0:\n%s", first)
	}
	second := body()
	if !strings.Contains(second, `"type":"tool_use"`) || !strings.Contains(second, "call_9") {
		t.Fatalf("request 2 did not serve the recorded tool call:\n%s", second)
	}
	// The arguments must arrive as an input_json_delta STRING: a bare object
	// reads as no input at all in the loop's accumulator.
	if !strings.Contains(second, `"type":"input_json_delta"`) {
		t.Fatalf("the tool arguments were not streamed as input_json_delta:\n%s", second)
	}
	// Beyond the script: the tail is the last recorded answer's TEXT, with no
	// tool call — re-serving a tool-bearing response is what looped for 40
	// rounds in the measured run.
	third := body()
	if strings.Contains(third, `"type":"tool_use"`) {
		t.Fatalf("the overlong request re-served a tool call:\n%s", third)
	}
	if got := stub.OverlongRequests(); got != 1 {
		t.Fatalf("OverlongRequests = %d, want 1", got)
	}
	if got := stub.RequestCount(); got != 3 {
		t.Fatalf("RequestCount = %d, want 3", got)
	}
}

// deltaText concatenates the text_delta payloads of one served stream — the
// answer as the surface reassembles it.
func deltaText(t *testing.T, stream string) string {
	t.Helper()
	var out strings.Builder
	for _, line := range strings.Split(stream, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame struct {
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			continue
		}
		if frame.Delta.Type == "text_delta" {
			out.WriteString(frame.Delta.Text)
		}
	}
	return out.String()
}

// TestStubExecutorServesRecordedBytesVerbatim is the replay's central claim, as a
// test: the string handed to the agent is the recording's, byte for byte, and the
// real dispatch is never reached.
func TestStubExecutorServesRecordedBytesVerbatim(t *testing.T) {
	recorded := "[t+22s] [tool_use id: call_7]\nexit=0\nDONE_SLEEP\n"
	p := writeJSONL(t, "session.jsonl", []string{
		userRow("run it"),
		assistantRow(toolUseBlock("call_7", "bash", `{"command":"sleep 12 && echo DONE_SLEEP"}`)),
		requestRow(),
		resultRow("call_7", recorded),
		assistantRow(jsonTextBlock("ran it")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	stub := NewStub(rec)
	defer stub.Close()

	got := stub.Executor()(deepseek.ToolCall{ID: "call_7", Name: "bash"})
	if got != recorded {
		t.Fatalf("executor returned %q, want the recorded bytes %q", got, recorded)
	}
	served := stub.ServedResults()
	if len(served) != 1 || !served[0].Recorded {
		t.Fatalf("served log = %+v, want one RECORDED result", served)
	}
	// An id the recording never answered is ANNOUNCED, never silently empty.
	missing := stub.Executor()(deepseek.ToolCall{ID: "call_ghost", Name: "bash"})
	if !strings.Contains(missing, "call_ghost") {
		t.Fatalf("an unrecorded call was not named in its refusal: %q", missing)
	}
	if ids := stub.MissingResults(); len(ids) != 1 || ids[0] != "call_ghost" {
		t.Fatalf("MissingResults = %v, want [call_ghost]", ids)
	}
}

// TestStubClosesItsListener is the zero-residue arm: after Close, nothing answers.
func TestStubClosesItsListener(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		userRow("q"),
		assistantRow(jsonTextBlock("a")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	stub := NewStub(rec)
	url := stub.URL()
	stub.Close()
	if _, err := http.Post(url+claudeMessagesPath, "application/json", strings.NewReader("{}")); err == nil {
		t.Fatal("the stub still answers after Close — the listener is not released")
	}
}

// sourcedUserRow builds a `user` row carrying the FACE it arrived under
// (internal/inputsource), which every row written since 2026-10-25 records. It
// sits beside the three unsourced helpers above so a test can say WHICH face it
// is exercising rather than relying on the loader's default.
func sourcedUserRow(text, source string) string {
	b, _ := json.Marshal(text) //stylegate:ignore marshalling a string cannot fail
	return fmt.Sprintf(`{"type":"user","source":%q,"message":{"role":"user","content":%s}}`, source, b)
}
