package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// writeTestPNG writes a tiny valid PNG and returns its path.
func writeTestPNG(t *testing.T, dir, name string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.White)
		}
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	return path
}

// TestReadInput_ImageAutoDetect is the core Epic scenario: passing an image
// file as the sole positional must produce a native image input (no text
// prompt needed) so the vision model processes the pixels directly.
func TestReadInput_ImageAutoDetect(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestPNG(t, dir, "screenshot.png")

	in, err := readInput(&config.Config{Positionals: []string{imgPath}})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if len(in.images) != 1 {
		t.Fatalf("images = %d, want 1", len(in.images))
	}
	if in.images[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", in.images[0].MediaType)
	}
	if in.images[0].Base64 == "" {
		t.Error("Base64 empty — image not encoded")
	}
	if in.images[0].Bytes == 0 {
		t.Error("Bytes = 0 — image not read")
	}
}

// TestReadInput_ImageWithPrompt verifies --prompt + image positional: the
// prompt wins as the instruction and the image is attached natively.
func TestReadInput_ImageWithPrompt(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestPNG(t, dir, "chart.png")

	in, err := readInput(&config.Config{
		Prompt:      "describe the chart data",
		Positionals: []string{imgPath},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "describe the chart data" {
		t.Errorf("prompt = %q", in.prompt)
	}
	if len(in.images) != 1 {
		t.Fatalf("images = %d, want 1", len(in.images))
	}
}

// TestReadInput_LiteralPromptText — the regression test for the Epic's first
// failing command: prose that merely MENTIONS an image path (e.g. "your task
// is written in image ~/x.png") must be treated as prompt TEXT, never as an
// image — the file does not exist.
func TestReadInput_LiteralPromptText(t *testing.T) {
	in, err := readInput(&config.Config{
		Positionals: []string{"your task is written in image ~/x.png"},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "your task is written in image ~/x.png" {
		t.Errorf("prompt = %q", in.prompt)
	}
	if len(in.images) != 0 {
		t.Errorf("images = %d, want 0 (prose is not an image)", len(in.images))
	}
}

// TestReadInput_GOPIPromptEnv — P1-3 regression: GOPI_PROMPT is catalogued +
// injected by ApplyEnv but was never read by readInput (dead env channel).
// With no --prompt and no positionals, GOPI_PROMPT must become the task
// prompt (flag > env > settings > positional/stdin). An explicit --prompt
// must still win over the env var.
func TestReadInput_GOPIPromptEnv(t *testing.T) {
	t.Setenv("GOPI_PROMPT", "task from env")

	// No --prompt, no positionals → GOPI_PROMPT supplies the prompt.
	in, err := readInput(&config.Config{})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "task from env" {
		t.Errorf("GOPI_PROMPT prompt = %q, want %q (env channel must be live)", in.prompt, "task from env")
	}

	// --prompt wins over GOPI_PROMPT (flag > env).
	in, err = readInput(&config.Config{Prompt: "task from flag"})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "task from flag" {
		t.Errorf("prompt = %q, want %q (flag must beat env)", in.prompt, "task from flag")
	}
}

// TestReadInput_PromptPlusTextFile is the RP-3 regression: `--prompt TEXT`
// beside a non-image file positional must NOT drop the file. The file is
// read, its text is appended to the instruction, and the instruction stays
// the prompt — the rule `gopi -h` now states ("positionals still carry task
// input — images attach natively, a text file's contents are appended to the
// instruction").
//
// The defect this pins (end-user-audit slice 8, RP-3 F2): readInput built
// `texts` from the file and the prompt-precedence branch threw the slice
// away, so the file appeared nowhere on the wire while the run exited 0 —
// neither attached, nor read, nor refused, and nothing on stderr.
func TestReadInput_PromptPlusTextFile(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("RP3-FILE-TOKEN file body\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	in, err := readInput(&config.Config{
		Prompt:      "instruction",
		Positionals: []string{txtPath},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	want := "instruction" + promptTextJoiner + positionalTextBullet + "RP3-FILE-TOKEN file body"
	if in.prompt != want {
		t.Fatalf("prompt = %q, want %q (the file positional must not be dropped)", in.prompt, want)
	}
	if len(in.images) != 0 {
		t.Errorf("images = %d, want 0", len(in.images))
	}
}

// TestReadInput_PromptPlusMultipleTextFiles: every text positional beside
// --prompt contributes, in argv order, each on its own bulleted block.
func TestReadInput_PromptPlusMultipleTextFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(b, []byte("second\nthird"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}

	in, err := readInput(&config.Config{
		Prompt:      "compare",
		Positionals: []string{a, b},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	want := "compare" + promptTextJoiner +
		positionalTextBullet + "first" + textJoiner +
		positionalTextBullet + "second" + textJoiner + positionalTextBullet + "third"
	if in.prompt != want {
		t.Fatalf("prompt = %q, want %q (both files, in argv order)", in.prompt, want)
	}
}

// TestReadInput_PromptPlusImageOnlyKeepsPrompt: when the positionals beside
// --prompt are images only, there is no text to append — the prompt is
// unchanged (the RP-3 fix must not decorate the image case, whose half
// already held).
func TestReadInput_PromptPlusImageOnlyKeepsPrompt(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestPNG(t, dir, "only.png")

	in, err := readInput(&config.Config{
		Prompt:      "instruction",
		Positionals: []string{imgPath},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "instruction" {
		t.Errorf("prompt = %q, want %q (no text positional to append)", in.prompt, "instruction")
	}
	if len(in.images) != 1 {
		t.Fatalf("images = %d, want 1", len(in.images))
	}
}

// TestReadInput_PromptPlusEmptyTextFileKeepsPrompt: an empty file positional
// contributes nothing, so the prompt stays bare rather than gaining an empty
// bullet.
func TestReadInput_PromptPlusEmptyTextFileKeepsPrompt(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	in, err := readInput(&config.Config{
		Prompt:      "instruction",
		Positionals: []string{empty},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "instruction" {
		t.Errorf("prompt = %q, want %q (an empty file appends nothing)", in.prompt, "instruction")
	}
}

// TestReadInput_GOPIPromptEnvPayload — the GOPI_PROMPT @payload file-ref
// syntax must work like --prompt's (P1-3 wiring honours the same contract).
func TestReadInput_GOPIPromptEnvPayload(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(payload, []byte("payload contents\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("GOPI_PROMPT", "@"+payload)

	in, err := readInput(&config.Config{})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "payload contents" {
		t.Errorf("prompt = %q, want %q", in.prompt, "payload contents")
	}
}

// TestReadInput_TextFile verifies a plain text file positional becomes the
// prompt (the documented `gopi ./notes.txt` behavior).
func TestReadInput_TextFile(t *testing.T) {
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "task.txt")
	if err := os.WriteFile(txtPath, []byte("  write a README\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	in, err := readInput(&config.Config{Positionals: []string{txtPath}})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "write a README" {
		t.Errorf("prompt = %q, want %q", in.prompt, "write a README")
	}
	if len(in.images) != 0 {
		t.Errorf("images = %d, want 0", len(in.images))
	}
}

// TestReadInput_MultipleImages attaches every image positional natively.
func TestReadInput_MultipleImages(t *testing.T) {
	dir := t.TempDir()
	a := writeTestPNG(t, dir, "a.png")
	b := writeTestPNG(t, dir, "b.png")

	in, err := readInput(&config.Config{
		Prompt:      "compare these",
		Positionals: []string{a, b},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if len(in.images) != 2 {
		t.Fatalf("images = %d, want 2", len(in.images))
	}
	if in.prompt != "compare these" {
		t.Errorf("prompt = %q", in.prompt)
	}
}

// TestReadInput_MixedTextAndImage: an image positional + a prompt positional
// — the text contributes to the prompt and the image is attached.
func TestReadInput_MixedTextAndImage(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestPNG(t, dir, "mix.png")

	in, err := readInput(&config.Config{
		Positionals: []string{"explain this diagram", imgPath},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "explain this diagram" {
		t.Errorf("prompt = %q", in.prompt)
	}
	if len(in.images) != 1 {
		t.Fatalf("images = %d, want 1", len(in.images))
	}
}

// TestReadInput_ImageExtensionWithoutFile: an arg ending in .png that is NOT
// a readable file is prompt text (matches the image auto-detect rule: an
// existing file is an image; a non-file is prose).
func TestReadInput_ImageExtensionWithoutFile(t *testing.T) {
	in, err := readInput(&config.Config{
		Positionals: []string{"see /tmp/nonexistent.png for details"},
	})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "see /tmp/nonexistent.png for details" {
		t.Errorf("prompt = %q", in.prompt)
	}
	if len(in.images) != 0 {
		t.Errorf("images = %d, want 0", len(in.images))
	}
}

// TestImageSources converts loaded ImageFiles to wire ClaudeImageSource
// blocks with the correct media type and base64 payload.
func TestImageSources(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestPNG(t, dir, "src.png")
	in, err := readInput(&config.Config{Positionals: []string{imgPath}})
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	srcs := imageSources(in.images)
	if len(srcs) != 1 {
		t.Fatalf("sources = %d, want 1", len(srcs))
	}
	if srcs[0].Type != "base64" {
		t.Errorf("Type = %q, want base64", srcs[0].Type)
	}
	if srcs[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q", srcs[0].MediaType)
	}
	if srcs[0].Data != in.images[0].Base64 {
		t.Error("Data does not match loaded base64")
	}
}

// memoryDBEnv / memoryDBEnvLegacy / hippocampusEnvEnabled are the env names
// that OUTRANK GOPI_HOME: memoryDBPath() prefers the DB overrides over the
// data root, and the gate decides whether an agent builds its hippocampus
// host at all. They are re-stated here because the owning packages
// (internal/tools, internal/agent/hippocampus_config.go) keep them unexported
// — the same deliberate mirror as internal/agent/loop_test.go, where a
// behavioural rename guard (TestTestEnvIsolation_*) pins the values.
const (
	memoryDBEnv           = "GOPI_MEMORY_DB"
	memoryDBEnvLegacy     = "GOPI_HIPPO_DB"
	hippocampusEnvEnabled = "GOPI_HIPPOCAMPUS"
)

// testAgentEnv isolates newAgent from the real user profile: HOME and
// settings point into a temp dir, an API key is provided, and raw logging is
// off (the agent's SessionLogger still writes to ~/.gopi/sessions, which is
// redirected under the temp HOME).
//
// The setenv list is a DENYLIST-CLEARING contract, not a whitelist: any var
// that steers behaviour and is not named here stays visible to the agent
// under test, which is exactly how a leftover `GOPI_MEMORY_DB=<real db>`
// export leaked a real knowledge base into internal/agent's tests (env-leak
// incident, devdoc/test-env-isolation.md, memory #1618).
func testAgentEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOPI_HOME", home) // gopi data root isolation (sessions/raw/memory)
	// The DB overrides outrank GOPI_HOME, so pin the resolved path inside the
	// same temp home and make the legacy alias + the gate unreachable.
	t.Setenv(memoryDBEnv, filepath.Join(home, "memory.db"))
	t.Setenv(memoryDBEnvLegacy, "")
	t.Setenv(hippocampusEnvEnabled, "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(home, "no-settings.json"))
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("GOPI_RAW_LOG", "OFF")
	t.Setenv("DEEPSEEK_MODEL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("GOPI_VISION_MODEL", "")
}

// TestNewAgent_ImageInputRecordsVisionModel guards the Epic bug: with an
// image input and no explicit --model, the vision model must be resolved
// BEFORE the agent is constructed so the session_start event (the `Model:`
// banner `gopi session log --chat` prints) records the model that actually goes on
// the wire — not the pre-switch text model.
func TestNewAgent_ImageInputRecordsVisionModel(t *testing.T) {
	testAgentEnv(t)

	// Force a distinctive vision model so the test can tell which one was
	// recorded.
	t.Setenv("GOPI_VISION_MODEL", "my-vision-model")

	ag, err := newAgent("my-vision-model")
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if ag.Model != "my-vision-model" {
		t.Errorf("ag.Model = %q, want my-vision-model", ag.Model)
	}
	if ag.SessionLog == nil {
		t.Fatal("SessionLog is nil — cannot verify session_start")
	}
	start := readSessionStart(t, ag.SessionLog.Path())
	if start != "my-vision-model" {
		t.Errorf("session_start model = %q, want my-vision-model (the wire model)", start)
	}
}

// readSessionStart parses the first JSONL line of a session log and returns
// its model field.
func readSessionStart(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session log %s: %v", path, err)
	}
	var start struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(data, &start); err != nil {
		t.Fatalf("parse session_start: %v", err)
	}
	return start.Model
}
