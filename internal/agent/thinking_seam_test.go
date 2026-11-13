package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// thinking_seam_test.go — Agent.OnThinking is a DESTINATION seam, not a
// formatting one.
//
// The seam exists so a carrier that owns the screen (gopi chat's reserved
// layout) can take the live reasoning stream instead of having its rows written
// over by it (task 01a0c0a7). The carrier's safety depends on one property:
// the bytes handed to the seam are the SAME bytes the stderr path writes —
// markers (including the ANSI dim/open and the closing newline) and deltas,
// unchanged and in the same order. A seam that reworded, reordered or dropped
// anything would silently change what every flow reader sees, which is the one
// thing this family of work may not do.

// thinkingStreamServer answers one thinking-then-text stream: a reasoning block
// (two deltas + a signature), then the answer, then end_turn.
func thinkingStreamServer(t *testing.T, requests *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		w.Header().Set("Content-Type", "text/event-stream")
		writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
		writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`)
		writeClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think"}}`)
		writeClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" about it."}}`)
		writeClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_1"}}`)
		writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
		writeClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer is 42."}}`)
		writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
		writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":20}}`)
		writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	}))
}

// runThinkingTurn runs one thinking round against the given server, either
// capturing stderr (seam absent) or collecting the seam's chunks (seam
// installed). It returns the full byte stream of the reasoning output.
//
// Both runs go through Agent.Chat, whose answer reaches the caller via OnText
// rather than the flow stream — so the stderr capture holds the reasoning alone
// and the comparison is byte-exact.
func runThinkingTurn(t *testing.T, server *httptest.Server, viaSeam bool) string {
	t.Helper()
	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		Thinking:      &deepseek.Thinking{Type: "enabled"},
	}
	var seam strings.Builder
	if viaSeam {
		ag.OnThinking = func(s string) { seam.WriteString(s) }
	}
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	// The CHAT path (Agent.Chat): its answer surfaces through OnText, not on
	// stderr, so the stderr stream carries the reasoning and nothing else — which
	// makes the byte comparison below exact instead of a subsequence match.
	_, runErr := ag.Chat("Hello", nil)
	w.Close() //stylegate:ignore the test restores os.Stderr immediately and drains the pipe read below; a close error cannot lose already-buffered output
	os.Stderr = oldStderr
	captured, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
	r.Close()
	if runErr != nil {
		t.Fatalf("Run() error: %v", runErr)
	}
	if viaSeam {
		return seam.String()
	}
	return string(captured)
}

// TestThinkingSeam_ReceivesTheExactStderrBytes is the seam's contract: the same
// turn run twice — once with the reasoning captured from stderr and once with it
// handed to OnThinking — must produce the SAME bytes. It is the guarantee the
// carrier relies on when it renders the stream in its own rows, and it is also
// the guarantee that a flow reader who installs no seam loses nothing.
func TestThinkingSeam_ReceivesTheExactStderrBytes(t *testing.T) {
	var stderrRequests, seamRequests int
	stderrServer := thinkingStreamServer(t, &stderrRequests)
	defer stderrServer.Close()
	seamServer := thinkingStreamServer(t, &seamRequests)
	defer seamServer.Close()

	viaStderr := runThinkingTurn(t, stderrServer, false)
	viaSeam := runThinkingTurn(t, seamServer, true)

	// Both runs must really have streamed the reasoning — otherwise two empty
	// streams would compare equal and prove nothing.
	if !strings.Contains(viaStderr, "[thinking]") || !strings.Contains(viaStderr, "Let me think about it.") {
		t.Fatalf("the stderr run carried no reasoning output: %q", viaStderr)
	}
	if viaSeam != viaStderr {
		t.Fatalf("the seam received %q, want the stderr path's exact bytes %q", viaSeam, viaStderr)
	}
	// The seam's stream is the reasoning only: the marker, both deltas, and the
	// closing newline the answer path relies on for separation.
	if !strings.HasPrefix(viaSeam, "[thinking] ") {
		t.Fatalf("the seam's stream does not start with the reasoning marker: %q", viaSeam)
	}
	if !strings.Contains(viaSeam, "Let me think about it.") {
		t.Fatalf("the seam's stream lost a reasoning delta: %q", viaSeam)
	}
	if !strings.HasSuffix(viaSeam, "\n") {
		t.Fatalf("the seam's stream lost the closing newline (the answer would be spliced onto the reasoning row): %q", viaSeam)
	}
}

// TestThinkingSeam_AbsentKeepsTheHistoricStderrPath pins the other half: with no
// seam the reasoning goes to stderr exactly as it always did, which is what
// cooked/flow, a piped run and the testsuite's greps depend on.
func TestThinkingSeam_AbsentKeepsTheHistoricStderrPath(t *testing.T) {
	var requests int
	server := thinkingStreamServer(t, &requests)
	defer server.Close()

	got := runThinkingTurn(t, server, false)
	if !strings.Contains(got, "[thinking] Let me think about it.") {
		t.Fatalf("the historic path lost its reasoning rendering: %q", got)
	}
}
