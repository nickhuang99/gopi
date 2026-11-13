// thinking_raw_stream_test.go — the RAW reasoning mode (Agent.StreamThinkingDeltas).
//
// WHY THIS FILE EXISTS. A carrier that RENDERS the reasoning itself (`gopi ui`)
// must receive the fragments the model produced, not the bytes the terminal
// display wraps around them. The wrapping — an SGR dim, a `[thinking] ` prefix, a
// trailing reset and newline — is DISPLAY DECORATION: a renderer that counted
// cells would have to strip it back off, and every strip is a place to get the
// arithmetic wrong (the `gopi chat` band needs an SGR-aware cell walk for exactly
// this reason).
//
// The three properties pinned here are the whole contract, and each one has been
// a real defect class elsewhere in this tree:
//
//	1. the callback receives the model's bytes EXACTLY — no marker, no SGR;
//	2. the callback is the ONLY destination — nothing also lands on stderr
//	   (a duplicate would smear the screen the TUI owns; silenceEngineConsole
//	   exists for the same reason);
//	3. the DEFAULT (the flag off) is byte-identical to the historic stream —
//	   the prefix and markers still arrive, on stderr, for every existing
//	   carrier.
//
// (3) is the load-bearing one: an epic that changes what `gopi chat` prints while
// claiming to add a new surface has changed a working thing, and this test makes
// that a failure rather than a discovery.

package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// reasoningMockSSE is a Claude-shaped stream: one thinking block (three
// fragments, so the stream really is chunked), then the answer.
func reasoningMockSSE(t *testing.T, w http.ResponseWriter, fragments []string) {
	t.Helper()
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}")
	for _, f := range fragments {
		payload, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("marshal fragment: %v", err)
		}
		writeClaudeSSE(w, "content_block_delta",
			fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":%s}}", payload))
	}
	writeClaudeSSE(w, "content_block_delta", "{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig_1\"}}")
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}")
	writeClaudeSSE(w, "content_block_delta", "{\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\""+expectedAnswerText+"\"}}")
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":1}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":20}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// expectedAnswerText is the reply the mock streams after the thinking block.
const expectedAnswerText = "The answer is 42."

// reasoningAgent builds an Agent aimed at a mock that streams the fragments.
func reasoningAgent(t *testing.T, fragments []string) *Agent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		reasoningMockSSE(t, w, fragments)
	}))
	t.Cleanup(server.Close)
	return &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: server.Client(),
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		Thinking:      &deepseek.Thinking{Type: "enabled"},
	}
}

// captureReasoningStderr redirects the process's stderr for the length of fn and
// returns what was written to it (envint_test.go already owns a captureStderr).
func captureReasoningStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	fn()
	//stylegate:ignore test restores os.Stderr below; the close error cannot lose already-buffered output read on the next line
	_ = w.Close()
	os.Stderr = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	//stylegate:ignore the read side is drained; a close error here cannot lose a byte already returned
	_ = r.Close()
	return string(out)
}

// TestRawThinkingModeDeliversTheModelsBytesUntouched is the contract's first two
// properties: the seam gets the fragments EXACTLY as the model produced them, and
// nothing is copied to stderr beside the callback.
func TestRawThinkingModeDeliversTheModelsBytesUntouched(t *testing.T) {
	fragments := []string{"Let me ", "think about ", "it."}
	ag := reasoningAgent(t, fragments)
	ag.StreamThinkingDeltas = true
	var got []string
	ag.OnThinking = func(s string) { got = append(got, s) }

	var stderr string
	var runErr error
	stderr = captureReasoningStderr(t, func() { _, runErr = ag.Run("Hello") })
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}

	// The RUN may issue more than one model round (a completion claim is
	// confirmed by a second round — bug022), so the check is that the fragments
	// arrived UNCHANGED, not that they arrived exactly once: the property under
	// test is the SHAPE of what the seam receives, and a round count is another
	// test's business.
	if want := strings.Join(fragments, ""); !strings.Contains(strings.Join(got, ""), want) {
		t.Fatalf("the seam received %q, want the model's own fragments %q (chunked: %q)", strings.Join(got, ""), want, got)
	}
	// Chunked delivery, not one blob: the whole point of a live display is that
	// the pieces arrive as the model produces them.
	seen := 0
	for _, s := range got {
		if s == fragments[0] {
			seen++
		}
	}
	if seen == 0 {
		t.Fatalf("the first fragment %q never arrived as its own write — the seam got %q", fragments[0], got)
	}
	for _, s := range got {
		if strings.Contains(s, thinkingPrefix) {
			t.Errorf("RAW mode delivered the display marker: %q", s)
		}
		if strings.Contains(s, ansiDim) || strings.Contains(s, ansiReset) {
			t.Errorf("RAW mode delivered SGR decoration: %q", s)
		}
		if s != "" && strings.TrimSpace(s) == "" {
			// The engine emits "" at the block boundaries (the emptied markers);
			// a fragment that is only whitespace would be the marker's own
			// padding leaking through.
			continue
		}
	}
	// Property 2: the callback is the ONLY destination. A duplicate on stderr
	// would be written over the display the carrier owns.
	if strings.Contains(stderr, "Let me") || strings.Contains(stderr, "think about") {
		t.Errorf("RAW mode also wrote the reasoning to stderr — the carrier would be painted over:\n%q", stderr)
	}
	// The ANSWER is not the reasoning and must still reach stderr on this path
	// exactly as it always did (RAW mode moves the reasoning's destination, not
	// the reply's): a run that suppressed the answer too would be a display that
	// shows an empty screen.
	if !strings.Contains(stderr, expectedAnswerText) {
		t.Errorf("RAW mode dropped the answer from stderr:\n%q", stderr)
	}
}

// TestDefaultThinkingModeKeepsTheHistoricStream is the contract's third property:
// with the flag off, every existing carrier sees exactly what it saw before —
// the textual prefix on a non-TTY stderr, the same fragments, in order.
func TestDefaultThinkingModeKeepsTheHistoricStream(t *testing.T) {
	fragments := []string{"Let me ", "think about ", "it."}
	ag := reasoningAgent(t, fragments)

	var stderr string
	var runErr error
	stderr = captureReasoningStderr(t, func() { _, runErr = ag.Run("Hello") })
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}

	// The test process's stderr is a pipe, so the non-TTY shape is the one in
	// force: the textual prefix, once, before the fragments.
	if want := thinkingPrefix + strings.Join(fragments, ""); !strings.Contains(stderr, want) {
		t.Fatalf("the default (flag off) stream lost its historic shape:\n  want %q in\n  %q", want, stderr)
	}
	if strings.Contains(stderr, ansiDim) {
		t.Errorf("a non-TTY stderr carries SGR dim, which the TTY branch would be for:\n%q", stderr)
	}
}

// TestRawThinkingModeStillDropsHiddenReasoning pins that RAW mode does not
// reintroduce a suppression bypass: HideThinking still decides whether the
// reasoning reaches the seam at all. The flag says HOW the bytes are shaped, not
// WHETHER they are shown — the switch that shows them is `--hide-thinking`.
func TestRawThinkingModeStillDropsHiddenReasoning(t *testing.T) {
	ag := reasoningAgent(t, []string{"secret reasoning"})
	ag.StreamThinkingDeltas = true
	ag.HideThinking = true
	var got []string
	ag.OnThinking = func(s string) { got = append(got, s) }

	var runErr error
	_ = captureReasoningStderr(t, func() { _, runErr = ag.Run("Hello") })
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if len(got) != 0 {
		t.Fatalf("hidden reasoning reached the seam: %q", got)
	}
}
