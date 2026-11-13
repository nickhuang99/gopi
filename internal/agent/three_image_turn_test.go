package agent

// The ONE-message regression: a turn carrying N images is ONE user message on
// the wire with N image blocks, and the wire's roles strictly alternate.
//
// The split this pins down: `Message.Image` (singular) made one image a
// structural part of one message, so every entry point — runEngine, Agent.Chat,
// Agent.ChatOps — emitted image[0] with the text and then one bare user message
// per extra image, and claude_client's user case refused to merge an
// image-carrying message. The endpoint never required that: one user message
// with 8/9/12 image blocks answers 200 (measured 2026-11-07). So the shape
// asserted here is what the API actually accepts, and the assertion is on the
// WIRE (what leaves the process), not on the in-memory slice.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// threeImageB64 are the three distinct payloads the turn below carries; each
// must appear exactly once, inside the same message.
var threeImageB64 = []string{"aW1nLTE=", "aW1nLTI=", "aW1nLTM="} // img-1 / img-2 / img-3

// wireMessage is the shape one request message deserializes into.
type wireMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type   string `json:"type"`
		Source *struct {
			Data string `json:"data"`
		} `json:"source"`
		Text string `json:"text"`
	} `json:"content"`
}

// wireRequest is the part of the Claude request body this file judges.
type wireRequest struct {
	Messages []wireMessage `json:"messages"`
}

// countImages returns how many image blocks a wire message carries.
func countImages(m wireMessage) int {
	n := 0
	for _, blk := range m.Content {
		if blk.Type == "image" {
			n++
		}
	}
	return n
}

// TestThreeImageTurn_OneUserMessageThreeBlocks is the epic's regression on the
// CHAT entry point: a turn handed three images produces exactly ONE user
// message on the wire, carrying three image blocks plus the turn's text.
func TestThreeImageTurn_OneUserMessageThreeBlocks(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"three pictures seen"}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Model = "vision-model-under-test"

	imgs := make([]*deepseek.ClaudeImageSource, 0, len(threeImageB64))
	for _, data := range threeImageB64 {
		imgs = append(imgs, &deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: data})
	}
	if _, err := ag.Chat("compare these three", nil, imgs...); err != nil {
		t.Fatalf("Chat(3 images) error: %v", err)
	}

	var wire wireRequest
	if err := json.Unmarshal([]byte(gotBody), &wire); err != nil {
		t.Fatalf("wire body is not Claude JSON: %v", err)
	}

	// A SPLIT turn shows up as three consecutive user messages; the fix makes
	// it one. Nothing else is in the request (the reply is the response).
	if len(wire.Messages) != 1 {
		t.Fatalf("wire carried %d messages, want 1 (the turn's single user message); roles = %v",
			len(wire.Messages), wireRoles(wire.Messages))
	}
	user := wire.Messages[0]
	if user.Role != "user" {
		t.Errorf("wire message role = %q, want user", user.Role)
	}
	if n := countImages(user); n != len(threeImageB64) {
		t.Errorf("the user message holds %d image blocks, want %d; roles = %v",
			n, len(threeImageB64), wireRoles(wire.Messages))
	}
	var textSeen bool
	for _, blk := range user.Content {
		if blk.Type == "text" && strings.Contains(blk.Text, "compare these three") {
			textSeen = true
		}
	}
	if !textSeen {
		t.Errorf("the turn's text is missing from its user message; block types = %v", blockTypes(user))
	}
	for _, data := range threeImageB64 {
		if n := strings.Count(gotBody, data); n != 1 {
			t.Errorf("payload %q appears %d time(s) on the wire, want exactly 1", data, n)
		}
	}
}

// TestThreeImageTurn_RolesStrictlyAlternate drives the WIRE BUILDER directly
// with a realistic multi-turn history: the three-image turn, the assistant's
// reply, a follow-up turn — the shape a real session sends on its second
// request. Roles must strictly alternate, and the image turn must still be ONE
// message carrying three blocks.
func TestThreeImageTurn_RolesStrictlyAlternate(t *testing.T) {
	imgs := make([]deepseek.ClaudeImageSource, 0, len(threeImageB64))
	for _, data := range threeImageB64 {
		imgs = append(imgs, deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: data})
	}
	msgs := []deepseek.Message{
		{Role: "user", Content: "compare these three", Images: imgs},
		{Role: "assistant", Content: "three pictures seen"},
		{Role: "user", Content: "and now?"},
	}
	req := deepseek.BuildClaudeRequest("m", msgs, "", "", nil, deepseek.DefaultClaudeMaxTokens, nil)

	if len(req.Messages) != 3 {
		t.Fatalf("wire carried %d messages, want 3 (user, assistant, user)", len(req.Messages))
	}
	for i, m := range req.Messages {
		want := "user"
		if i%2 == 1 {
			want = "assistant"
		}
		if m.Role != want {
			t.Errorf("wire message %d role = %q, want %q (roles must strictly alternate)", i, m.Role, want)
		}
	}
	if n := len(req.Messages[0].Content); n != len(threeImageB64)+1 {
		t.Errorf("image turn content blocks = %d, want %d (3 images + text)", n, len(threeImageB64)+1)
	}
	imageBlocks := 0
	for _, blk := range req.Messages[0].Content {
		if blk.Type == "image" {
			imageBlocks++
		}
	}
	if imageBlocks != len(threeImageB64) {
		t.Errorf("image turn carries %d image blocks, want %d", imageBlocks, len(threeImageB64))
	}
}

// TestConsecutiveUserMessagesMergeWithImages pins the WIRE leg on its own: fed
// CONSECUTIVE user messages that carry images (the shape the old entry-point
// split produced), the builder merges them into ONE message carrying all the
// blocks. This is the arm that used to be refused by name ("image-carrying
// user messages are NEVER merged"), so a regression there turns this red even
// if the entry points still happen to emit a single message.
func TestConsecutiveUserMessagesMergeWithImages(t *testing.T) {
	first := deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: threeImageB64[0]}
	second := deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: threeImageB64[1]}
	third := deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: threeImageB64[2]}
	msgs := []deepseek.Message{
		{Role: "user", Content: "compare these three", Images: []deepseek.ClaudeImageSource{first, second}},
		{Role: "user", Images: []deepseek.ClaudeImageSource{third}},
	}
	req := deepseek.BuildClaudeRequest("m", msgs, "", "", nil, deepseek.DefaultClaudeMaxTokens, nil)

	if len(req.Messages) != 1 {
		t.Fatalf("consecutive user messages did not merge: %d wire messages, want 1", len(req.Messages))
	}
	if req.Messages[0].Role != "user" {
		t.Errorf("merged message role = %q, want user", req.Messages[0].Role)
	}
	imageBlocks := 0
	for _, blk := range req.Messages[0].Content {
		if blk.Type == "image" {
			imageBlocks++
		}
	}
	if imageBlocks != len(threeImageB64) {
		t.Errorf("merged message carries %d image blocks, want %d", imageBlocks, len(threeImageB64))
	}
}

// wireRoles renders just the roles, so a failure never re-prints a request body
// (which carries the whole system prompt).
func wireRoles(msgs []wireMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Role)
	}
	return out
}

// blockTypes renders just the block types of one message, for the same reason.
func blockTypes(m wireMessage) []string {
	out := make([]string, 0, len(m.Content))
	for _, blk := range m.Content {
		out = append(out, blk.Type)
	}
	return out
}
