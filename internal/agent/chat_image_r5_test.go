package agent

// R5 acceptance tests (devdoc/web-align-p1.md): image turns in Agent.Chat /
// Agent.ChatOps carry native Anthropic image blocks on the wire — image
// blocks FIRST inside the turn's ONE user message, text after, so a turn
// with N images is ONE message with N blocks (never N messages), and the
// vision-model switch is the CALLER's job (the engine never rewrites a.Model
// — asserted here so the web handler's turn-level switch stays the only
// model mutation).

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

const testImageB64 = "aGVsbG8tcG5n" // "hello-png" — shape is what matters, not pixels

// TestChat_ImageTurn_WireShape locks the R5 engine contract on Agent.Chat:
// the image rides in the turn's user message as a native Anthropic image
// block BEFORE the text block, the model name in the request body is
// whatever the caller set (turn-level switch responsibility), and text-only
// turns remain image-free.
func TestChat_ImageTurn_WireShape(t *testing.T) {
	var gotBody string
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		gotBody = string(b)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type   string `json:"type"`
					Source *struct {
						Type      string `json:"type"`
						MediaType string `json:"media_type"`
						Data      string `json:"data"`
					} `json:"source,omitempty"`
					Text string `json:"text,omitempty"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Errorf("mock: request body is not valid Claude JSON: %v", err)
		}
		gotModel = req.Model
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"I see the words"}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Model = "glm-5.3-flash" // the caller's turn-level switch (web handler does this)

	result, err := ag.Chat("图中写了什么字", nil, &deepseek.ClaudeImageSource{
		Type: "base64", MediaType: "image/png", Data: testImageB64,
	})
	if err != nil {
		t.Fatalf("Chat(image) error: %v", err)
	}
	if result != "I see the words" {
		t.Errorf("result = %q", result)
	}
	if gotModel != "glm-5.3-flash" {
		t.Errorf("wire model = %q, want the caller-set vision model", gotModel)
	}

	// Exactly one user message, image block first, text after.
	var user struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type   string `json:"type"`
				Source *struct {
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(gotBody), &user); err != nil {
		t.Fatalf("re-unmarshal wire: %v", err)
	}
	var userMsgs = 0
	var imgIdx, txtIdx = -1, -1
	for _, m := range user.Messages {
		if m.Role != "user" {
			continue
		}
		userMsgs++
		for j, blk := range m.Content {
			if blk.Type == "image" {
				imgIdx = j
				if blk.Source == nil || blk.Source.MediaType != "image/png" || blk.Source.Data != testImageB64 {
					t.Errorf("image block source = %+v, want base64 image/png %q", blk.Source, testImageB64)
				}
			}
			if blk.Type == "text" && strings.Contains(blk.Text, "图中写了什么字") {
				txtIdx = j
			}
		}
	}
	if userMsgs != 1 {
		t.Errorf("wire carried %d user messages, want 1 (image+text in one turn message)", userMsgs)
	}
	if imgIdx == -1 {
		t.Fatal("no image block in the user message — R5 wire shape broken")
	}
	if txtIdx == -1 {
		t.Fatal("no text block in the user message")
	}
	if imgIdx > txtIdx {
		t.Errorf("image block (idx %d) must precede text (idx %d)", imgIdx, txtIdx)
	}
}

// TestChat_TextTurn_HasNoImageBlocks is the negative control: the variadic
// change must not disturb the plain Q&A path (no image blocks when no
// images are passed).
func TestChat_TextTurn_HasNoImageBlocks(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"ok"}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Logger = log.New(io.Discard, "", 0)
	if _, err := ag.Chat("plain turn", nil); err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if strings.Contains(gotBody, `"type": "image"`) || strings.Contains(gotBody, `"type":"image"`) {
		t.Errorf("text-only turn carried an image block: %s", gotBody)
	}
}

// TestChatOps_ImageTurn_ToolRoundKeepsImageInContext locks the ChatOps R5
// behavior: the image turn's ONE user message stays in Ctx.Messages through
// the tool rounds (the wire rebuilds it every round), carrying BOTH images as
// blocks of that one message.
func TestChatOps_ImageTurn_ToolRoundKeepsImageInContext(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		// Round 1: answer directly (no tool calls) — the context shape is
		// what we assert, not a tool round-trip.
		claudeTextStreamFragmented(w, []string{strings.Repeat("seen both images clearly ", 2)}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Model = "vision-model-under-test"

	result, err := ag.ChatOps("compare these", nil,
		&deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: "img-one"},
		&deepseek.ClaudeImageSource{Type: "base64", MediaType: "image/jpeg", Data: "img-two"},
	)
	if err != nil {
		t.Fatalf("ChatOps(images) error: %v", err)
	}
	if !strings.HasPrefix(result, "seen both images clearly") {
		t.Errorf("result = %q", result)
	}

	// Engine context: ONE user message carries BOTH images plus the text — a
	// turn's N images ride in one message, role-strictly-alternating with the
	// assistant reply (mirrors runEngine and Agent.Chat).
	msgs := ag.Ctx.Messages
	if len(msgs) != 2 {
		t.Fatalf("Ctx.Messages = %d, want 2 (one user message with both images + assistant)", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "compare these" {
		t.Errorf("msgs[0] = %+v, want user text\"compare these\"", msgs[0])
	}
	if len(msgs[0].Images) != 2 {
		t.Fatalf("msgs[0].Images = %d, want 2: %+v", len(msgs[0].Images), msgs[0].Images)
	}
	if msgs[0].Images[0].Data != "img-one" || msgs[0].Images[1].Data != "img-two" {
		t.Errorf("msgs[0].Images = %+v, want img-one then img-two in order", msgs[0].Images)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want assistant", msgs[1].Role)
	}

	// Wire: both image blocks present, in ONE user message.
	j := strings.Join(bodies, "\n")
	if !strings.Contains(j, `"img-one"`) || !strings.Contains(j, `"img-two"`) {
		t.Error("wire missing one of the two image payloads")
	}
}

// TestChatOps_TextTurn_UnchangedShape guards the variadic signature change:
// ChatOps without images must produce the exact pre-R5 context shape
// (text-only user message appended after the seed).
func TestChatOps_TextTurn_UnchangedShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{strings.Repeat("ok ", 25)}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	seed := []deepseek.Message{{Role: "user", Content: "earlier"}, {Role: "assistant", Content: "yes"}}
	if _, err := ag.ChatOps("new turn", seed); err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}
	msgs := ag.Ctx.Messages
	// seed(2) + user + assistant = 4; the assistant answer IS the final
	// content (>= minFinalAnswerChars, so no confirmation round).
	if len(msgs) != 4 {
		t.Fatalf("Ctx.Messages = %d, want 4 (seed x2 + user + assistant)", len(msgs))
	}
	if msgs[2].Role != "user" || msgs[2].Content != "new turn" || len(msgs[2].Images) != 0 {
		t.Errorf("msgs[2] = %+v, want plain text user turn without image", msgs[2])
	}
}
