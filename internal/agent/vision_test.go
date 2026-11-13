package agent

// vision_analyze tool tests: schema (image_path required, prompt optional,
// description teaches the use contract), validateToolArgs, and the dispatch
// path (base64 request to the vision endpoint, plain-text result back —
// images never enter the main context).

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// TestVisionAnalyzeToolSchema: image_path required, prompt optional; the
// description teaches when/how to use (图片理解/OCR/图表分析 — 截图文字识别直接调
// vision, 不需要 OCR 脚本) and the context-protection promise (图片不进主上下文).
func TestVisionAnalyzeToolSchema(t *testing.T) {
	td := toolDefByName(t, "vision_analyze")
	if td == nil {
		t.Fatal("vision_analyze missing from claudeToolDefs")
	}
	props := schemaProps(t, td)
	if _, ok := props["image_path"]; !ok {
		t.Fatal("vision_analyze schema missing required parameter image_path")
	}
	if _, ok := props["prompt"]; !ok {
		t.Fatal("vision_analyze schema missing optional parameter prompt")
	}
	if req := schemaRequired(t, td); len(req) != 1 || req[0] != "image_path" {
		t.Fatalf("vision_analyze required = %v, want [image_path] (prompt stays optional)", req)
	}
	for _, want := range []string{
		"图片理解", "OCR", "图表",
		"截图文字识别直接调 vision",
		"base64",
		"32 MiB",
		"user message", // images only valid in user messages
		"context protection",
	} {
		if !strings.Contains(td.Description, want) {
			t.Errorf("vision_analyze description missing %q", want)
		}
	}
}

// TestVisionAnalyzeValidateToolArgs: image_path enforced; prompt optional.
func TestVisionAnalyzeValidateToolArgs(t *testing.T) {
	if msg := validateToolArgs("vision_analyze", map[string]interface{}{}); msg != "Missing required parameter: image_path" {
		t.Fatalf("empty args msg = %q, want image_path error", msg)
	}
	if msg := validateToolArgs("vision_analyze", map[string]interface{}{"image_path": "  "}); msg != "Missing required parameter: image_path" {
		t.Fatalf("blank image_path msg = %q, want image_path error", msg)
	}
	if msg := validateToolArgs("vision_analyze", map[string]interface{}{"image_path": "/tmp/x.png"}); msg != "" {
		t.Fatalf("valid image_path msg = %q, want ''", msg)
	}
}

// TestVisionAnalyzeDispatch drives the tool dispatch end-to-end against an
// httptest server: it verifies the request carries the image block (base64 +
// media_type) in a USER message, and that the plain-text result is returned.
// The Agent's tool runner needs a real client, so we construct the agent the
// same way context_trim_test does and point the vision env at the test server.
func TestVisionAnalyzeDispatch(t *testing.T) {
	const fakeImage = "\xff\xd8\xff\xe0fake-jpeg-bytes"
	wantBase64 := base64.StdEncoding.EncodeToString([]byte(fakeImage))

	var gotReq deepseek.ClaudeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/messages" {
			t.Errorf("expected /anthropic/messages, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"m1","content":[{"type":"text","text":"终端截图：你好，世界"}],"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":20}}`))
	}))
	defer server.Close()

	// Point the vision endpoint at the test server (env override wins).
	t.Setenv("GOPI_VISION_BASE_URL", server.URL)
	t.Setenv("GOPI_VISION_MODEL", "test-vision-model")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-key")

	// Create the image file.
	imgPath := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(imgPath, []byte(fakeImage), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	// Run the dispatch path through executeToolDefault (the real per-tool
	// dispatch the loop uses), not VisionAnalyze directly — this tests the
	// loop wiring (validate → dispatch → result).
	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("epic")
	out := ag.executeToolDefault(deepseek.ToolCall{
		ID:   "call_vis1",
		Name: "vision_analyze",
		Arguments: map[string]interface{}{
			"image_path": imgPath,
			"prompt":     "识别文字",
		},
	})
	if strings.HasPrefix(out, "vision_analyze error:") {
		t.Fatalf("vision_analyze dispatch failed: %s", out)
	}
	if !strings.Contains(out, "你好，世界") {
		t.Errorf("result missing recognized text, got: %s", out)
	}
	if !strings.Contains(out, "test-vision-model") {
		t.Errorf("result missing model name, got: %s", out)
	}

	// Verify the wire request: one user message, image block with correct
	// base64 + media_type, text block with the prompt.
	if len(gotReq.Messages) != 1 || gotReq.Messages[0].Role != "user" {
		t.Fatalf("expected 1 user message, got %+v", gotReq.Messages)
	}
	blocks := gotReq.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("expected [image, text] blocks, got %+v", blocks)
	}
	if blocks[0].Type != "image" || blocks[0].Source == nil {
		t.Fatalf("expected image block with source, got %+v", blocks[0])
	}
	if blocks[0].Source.Type != "base64" || blocks[0].Source.Data != wantBase64 {
		t.Errorf("image source mismatch: type=%q data-prefix=%q want %q", blocks[0].Source.Type, blocks[0].Source.Data[:16], wantBase64[:16])
	}
	if blocks[0].Source.MediaType != "image/png" {
		t.Errorf("expected image/png media_type, got %q", blocks[0].Source.MediaType)
	}
	if blocks[1].Type != "text" || blocks[1].Text != "识别文字" {
		t.Errorf("expected prompt text block, got %+v", blocks[1])
	}
	if gotReq.Model != "test-vision-model" {
		t.Errorf("expected vision model, got %q", gotReq.Model)
	}
}

// TestVisionAnalyzeErrors: file-not-found / unsupported-format / oversized
// rejection — the validation contract, no network involved.
func TestVisionAnalyzeErrors(t *testing.T) {
	run := func(args map[string]interface{}) string {
		ag := &Agent{Ctx: &Context{}}
		ag.Ctx.AddUserMessage("epic")
		return ag.executeToolDefault(deepseek.ToolCall{ID: "call_vis1", Name: "vision_analyze", Arguments: args})
	}

	out := run(map[string]interface{}{
		"image_path": filepath.Join(t.TempDir(), "nope.png"),
	})
	if !strings.Contains(out, "file not found") {
		t.Errorf("missing-file error = %q, want 'file not found'", out)
	}

	out = run(map[string]interface{}{
		"image_path": filepath.Join(t.TempDir(), "doc.txt"),
	})
	if !strings.Contains(out, "unsupported image format") {
		t.Errorf("bad-format error = %q, want 'unsupported image format'", out)
	}

	// Oversized: write a file larger than the 32 MiB cap via truncate.
	bigPath := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(bigPath, nil, 0o644); err != nil {
		t.Fatalf("create big file: %v", err)
	}
	if err := os.Truncate(bigPath, 33<<20); err != nil {
		t.Fatalf("truncate big file: %v", err)
	}
	out = run(map[string]interface{}{
		"image_path": bigPath,
	})
	if !strings.Contains(out, "too large") {
		t.Errorf("oversize error = %q, want 'too large'", out)
	}
}
