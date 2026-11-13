package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// claudeTextStreamFragmented writes a Claude SSE text response whose final
// text is split into the given fragments, each emitted as its own
// text_delta. This lets tests exercise the real-time streaming path where
// OnText receives per-delta fragments rather than one big blob.
func claudeTextStreamFragmented(w http.ResponseWriter, fragments []string, stopReason string) {
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}")
	for _, frag := range fragments {
		fragJSON, _ := json.Marshal(frag) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}", fragJSON))
	}
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "message_delta", fmt.Sprintf("{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q,\"stop_sequence\":null}}", stopReason))
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// newTestAgent builds a minimal Agent pointed at the given httptest server.
func newTestAgent(t *testing.T, server *httptest.Server) *Agent {
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
	}
	ag.Logger = log.New(io.Discard, "", 0)
	return ag
}

// TestOnText_StreamingFragments verifies the OnText callback receives each
// streamed text delta individually, in wire order, and that their
// concatenation equals the final result. This is the streaming hook gopi chat
// relies on to surface the model's answer on stdout in real time.
func TestOnText_StreamingFragments(t *testing.T) {
	fragments := []string{"Hello, ", "chat ", "mode", "!"}
	var callCount int32
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		// Only the first (real) round produces the fragmented answer; the
		// loop's confirmation rounds are filler so OnText records exactly
		// the fragments of the first answer.
		if atomic.LoadInt32(&callCount) == 1 {
			claudeTextStreamFragmented(w, fragments, "end_turn")
			return
		}
		claudeTextStreamFragmented(w, []string{""}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.OnText = func(delta string) {
		if atomic.LoadInt32(&callCount) == 1 {
			received = append(received, delta)
		}
	}

	result, err := ag.Resume("say hi", nil)
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	want := strings.Join(fragments, "")
	if len(received) != len(fragments) {
		t.Errorf("OnText received %d fragments, want %d (got %q)", len(received), len(fragments), received)
	}
	if got := strings.Join(received, ""); got != want {
		t.Errorf("OnText concatenated = %q, want %q", got, want)
	}
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
	if atomic.LoadInt32(&callCount) < 2 {
		t.Errorf("expected >= 2 API calls (loop confirmation), got %d", atomic.LoadInt32(&callCount))
	}
}

// TestOnText_FallbackPath verifies the non-streaming fallback (ClaudeChat,
// reached when ClaudeChatStream's HTTP request fails) also invokes OnText once
// with the full content, so chat streaming does not silently lose output when
// the SSE connection cannot be established.
func TestOnText_FallbackPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A streaming request carries Accept: text/event-stream → fail it
		// with 500 so ClaudeChatStream's HTTP layer errors out, forcing the
		// non-streaming fallback (which expects a JSON response).
		if r.Header.Get("Accept") == "text/event-stream" {
			http.Error(w, "stream unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"fallback answer"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	received := []string{}
	ag.OnText = func(delta string) { received = append(received, delta) }

	result, err := ag.Resume("hi", nil)
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	// The non-streaming fallback must reach the OnText callback at least once
	// (the loop may run a confirmation round and fire it again — only the
	// content matters here, not the count).
	found := false
	want := "fallback answer"
	for _, frag := range received {
		if frag == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("OnText (fallback) = %q, want it to contain %q", received, want)
	}
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
}
