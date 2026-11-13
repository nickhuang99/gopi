package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The measured defect (2026-09-30, dumps/20260930-194502-1.dump.json):
// `gopi chat --once --tools` sent
//
//	{"model":"deepseek-flash","max_tokens":16384,"messages":null}
//
// and the endpoint answered HTTP 422 "Failed to deserialize the JSON body into
// the target type: messages: invalid type: null, expected a sequence". The
// array was emptied locally (Context.TrimToFit — guarded on its own side by
// internal/agent's trim_to_fit_empty_test.go), and the field carries no
// `omitempty`, so an empty slice reaches the wire as `null`.
//
// The choke point refuses it instead: an empty array is not a provider
// condition to be discovered by a round trip (the caller paid a wasted request
// plus a minimal-core degradation re-send), it is a broken request, and no
// variant of it can be valid.

// emptyMessagesTestClient points a client at a server that FAILS the test if it
// is ever contacted — every test here asserts a client-side refusal.
func emptyMessagesTestClient(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the request reached the network — the empty-messages guard did not fire before the send")
	}))
	t.Cleanup(server.Close)
	return &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0, InitialDelay: 1, MaxDelay: 1},
	}
}

// TestEmptyMessagesSerializeAsNull pins the wire fact the guard exists for.
// The production body was `null` (not `[]`) because BuildClaudeRequest builds
// req.Messages by APPENDING converted messages: an input with nothing to
// convert leaves the field at its zero value, and Go marshals a nil slice as
// `null` — `messages: null`, which the endpoint rejects at deserialization
// (before it can even tell us "at least one message is required"). Both halves
// are asserted here, and the guard catches both (len == 0).
func TestEmptyMessagesSerializeAsNull(t *testing.T) {
	// The real path: nothing to convert ⇒ nil ⇒ null.
	req := BuildClaudeRequest("deepseek-flash", nil, "kernel prompt", "", nil, 16384, nil)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if req.Messages != nil {
		t.Fatalf("premise changed: BuildClaudeRequest no longer leaves Messages nil for empty input (got %#v) — re-derive the guard before removing it", req.Messages)
	}
	if want := `"messages":null`; !contains(string(body), want) {
		t.Fatalf("expected %s on the wire, got %s", want, body)
	}

	// An explicitly empty array is the other shape a caller can hand us; the
	// guard refuses it for the same reason (the endpoint answers "messages: at
	// least one message is required").
	emptyBody, err := json.Marshal(&ClaudeRequest{Model: "deepseek-flash", MaxTokens: 16384, Messages: []ClaudeMessage{}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `"messages":[]`; !contains(string(emptyBody), want) {
		t.Fatalf("expected %s on the wire, got %s", want, emptyBody)
	}
}

// TestClaudeChatStreamRefusesEmptyMessages — the STREAMING outbound path (every
// chat round's first attempt) refuses an empty array without touching the
// network. This is the arm the 422 was observed on.
func TestClaudeChatStreamRefusesEmptyMessages(t *testing.T) {
	c := emptyMessagesTestClient(t)

	_, err := c.ClaudeChatStream(context.Background(), &ClaudeRequest{
		Model: "deepseek-flash", MaxTokens: 16384, Stream: true, Messages: nil,
	})
	if !errors.Is(err, ErrEmptyMessages) {
		t.Fatalf("want ErrEmptyMessages, got %v", err)
	}
}

// TestClaudeChatRefusesEmptyMessages — the NON-streaming path (the round's
// fallback, and the path the minimal-core degradation ladder re-sends through)
// refuses it too, so the request cannot slip through the arm the other one
// delegates to.
func TestClaudeChatRefusesEmptyMessages(t *testing.T) {
	c := emptyMessagesTestClient(t)

	_, err := c.ClaudeChat(context.Background(), &ClaudeRequest{
		Model: "deepseek-flash", MaxTokens: 16384, Messages: []ClaudeMessage{},
	})
	if !errors.Is(err, ErrEmptyMessages) {
		t.Fatalf("want ErrEmptyMessages, got %v", err)
	}
}

// TestNonEmptyMessagesStillReachTheWire — the guard is a floor, not a wall: a
// normal one-message request still goes out and is parsed.
func TestNonEmptyMessagesStillReachTheWire(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`)) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	}))
	t.Cleanup(server.Close)
	c := &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0, InitialDelay: 1, MaxDelay: 1},
	}

	resp, err := c.ClaudeChat(context.Background(), &ClaudeRequest{
		Model: "deepseek-flash", MaxTokens: 16384,
		Messages: []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("non-empty request was refused: %v", err)
	}
	if !called {
		t.Fatal("the request never reached the server")
	}
	if resp.Text() != "ok" {
		t.Errorf("response text = %q, want %q", resp.Text(), "ok")
	}
}
