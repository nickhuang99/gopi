package deepseek

// Vision endpoint tests: model/base-URL resolution (env > settings >
// default — the "future provider swap is zero-code" contract) and the
// VisionAnalyze request shape (image block in a USER message, correct
// media_type, plain-text result + usage surfaced).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveVisionModel: GOPI_VISION_MODEL env > settings vision_model >
// DefaultVisionModel.
func TestResolveVisionModel(t *testing.T) {
	t.Setenv("GOPI_VISION_MODEL", "")
	if got := ResolveVisionModel(nil); got != DefaultVisionModel {
		t.Errorf("default model = %q, want %q", got, DefaultVisionModel)
	}
	cfg := &Config{VisionModel: "my-vision-model"}
	if got := ResolveVisionModel(cfg); got != "my-vision-model" {
		t.Errorf("settings model = %q, want my-vision-model", got)
	}
	t.Setenv("GOPI_VISION_MODEL", "env-vision-model")
	if got := ResolveVisionModel(cfg); got != "env-vision-model" {
		t.Errorf("env model = %q, want env-vision-model (env beats settings)", got)
	}
}

// TestResolveVisionBaseURL: GOPI_VISION_BASE_URL env > settings
// vision_base_url > DefaultVisionBaseURL.
func TestResolveVisionBaseURL(t *testing.T) {
	t.Setenv("GOPI_VISION_BASE_URL", "")
	if got := ResolveVisionBaseURL(nil); got != DefaultVisionBaseURL {
		t.Errorf("default base = %q, want %q", got, DefaultVisionBaseURL)
	}
	cfg := &Config{VisionBaseURL: "https://other.example.com/anthropic"}
	if got := ResolveVisionBaseURL(cfg); got != "https://other.example.com/anthropic" {
		t.Errorf("settings base = %q, want configured value", got)
	}
	t.Setenv("GOPI_VISION_BASE_URL", "https://env.example.com/anthropic")
	if got := ResolveVisionBaseURL(cfg); got != "https://env.example.com/anthropic" {
		t.Errorf("env base = %q, want env value (env beats settings)", got)
	}
}

// TestNewVisionClient_ReusesAPIKey: the vision client resolves the SAME key
// path as the main Claude client (ANTHROPIC_AUTH_TOKEN > DEEPSEEK_API_KEY >
// settings api_key) — no separate vision key needed for DeepSeek.
//
// The test stubs the settings file (GOPI_SETTINGS → empty temp file) so the
// resolution chain is exercised deterministically: without this, a HOST
// settings.json carrying vision_base_url (e.g. open.bigmodel.cn) silently
// wins over DefaultVisionBaseURL and fails the test on machines configured
// for GLM — a test-of-the-host, not of the code.
func TestNewVisionClient_ReusesAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "vision-key")
	t.Setenv("GOPI_VISION_BASE_URL", "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "empty-settings.json"))
	client, err := NewVisionClient()
	if err != nil {
		t.Fatalf("NewVisionClient() error: %v", err)
	}
	if client.APIKey != "vision-key" {
		t.Errorf("APIKey = %q, want vision-key (same DeepSeek key)", client.APIKey)
	}
	if client.BaseURL != DefaultVisionBaseURL {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, DefaultVisionBaseURL)
	}
}

// TestVisionAnalyze_WireShape drives VisionAnalyze against an httptest server
// and verifies: POST to /anthropic/messages, one USER message carrying an
// image block (base64 + media_type) + prompt text block, and the returned
// text + usage.
func TestVisionAnalyze_WireShape(t *testing.T) {
	const fakeImage = "PNG-FAKE-BYTES"
	var gotReq ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/messages" {
			t.Errorf("path = %s, want /anthropic/messages", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"v1","content":[{"type":"text","text":"识别结果：你好世界"}],"stop_reason":"end_turn","usage":{"input_tokens":120,"output_tokens":30}}`))
	}))
	defer server.Close()

	t.Setenv("GOPI_VISION_BASE_URL", server.URL)
	t.Setenv("GOPI_VISION_MODEL", "test-vision-model")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-key")

	client, err := NewVisionClient()
	if err != nil {
		t.Fatalf("NewVisionClient() error: %v", err)
	}

	text, usage, err := client.VisionAnalyze(context.Background(), fakeImage, "image/png", "识别文字")
	if err != nil {
		t.Fatalf("VisionAnalyze() error: %v", err)
	}
	if !strings.Contains(text, "你好世界") {
		t.Errorf("text = %q, want recognized text", text)
	}
	if usage.InputTokens != 120 || usage.OutputTokens != 30 {
		t.Errorf("usage = %+v, want input 120 output 30", usage)
	}

	if len(gotReq.Messages) != 1 || gotReq.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v, want exactly one user message (images only valid in user messages)", gotReq.Messages)
	}
	blocks := gotReq.Messages[0].Content
	if len(blocks) != 2 || blocks[0].Type != "image" || blocks[1].Type != "text" {
		t.Fatalf("content blocks = %+v, want [image, text]", blocks)
	}
	if blocks[0].Source.Type != "base64" || blocks[0].Source.Data != fakeImage || blocks[0].Source.MediaType != "image/png" {
		t.Errorf("image source = %+v, want base64 png payload", blocks[0].Source)
	}
	if blocks[1].Text != "识别文字" {
		t.Errorf("prompt block text = %q, want 识别文字", blocks[1].Text)
	}
	if gotReq.Model != "test-vision-model" {
		t.Errorf("model = %q, want test-vision-model", gotReq.Model)
	}
}

// TestVisionAnalyze_DefaultPrompt: an empty prompt gets the default
// description/OCR instruction.
func TestVisionAnalyze_DefaultPrompt(t *testing.T) {
	var gotReq ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq) //stylegate:ignore decodes the request the test's own client sent; the decoded value is the assertion
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"v1","content":[{"type":"text","text":"ok"}],"usage":{}}`))
	}))
	defer server.Close()

	t.Setenv("GOPI_VISION_BASE_URL", server.URL)
	t.Setenv("GOPI_VISION_MODEL", "test-vision-model")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-key")

	client, err := NewVisionClient()
	if err != nil {
		t.Fatalf("NewVisionClient() error: %v", err)
	}
	if _, _, err := client.VisionAnalyze(context.Background(), "data", "image/jpeg", ""); err != nil {
		t.Fatalf("VisionAnalyze() error: %v", err)
	}
	blocks := gotReq.Messages[0].Content
	if len(blocks) != 2 || !strings.Contains(blocks[1].Text, "描述这张图片") {
		t.Errorf("default prompt = %q, want default description instruction", blocks[1].Text)
	}
}
