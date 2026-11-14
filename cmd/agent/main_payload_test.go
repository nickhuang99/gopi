package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return path
}

// TestReadPayloadFile_Positional covers `gopi @file` — the payload (with
// special characters, quotes, newlines, JSON) arrives verbatim, leading
// whitespace intact, trailing newline stripped.
func TestReadPayloadFile_Positional(t *testing.T) {
	dir := t.TempDir()
	payload := "  {\"key\": \"val & <weird> \\\"quoted\\\"\"}\nline2\ttab\n"
	path := writeTemp(t, dir, "payload.json", payload)

	text, err := readPayloadFile("@" + path)
	if err != nil {
		t.Fatalf("readPayloadFile: %v", err)
	}
	want := strings.TrimRight(payload, "\n")
	if text != want {
		t.Fatalf("payload mangled:\n got %q\nwant %q", text, want)
	}
}

// TestReadPayloadFile_MissingFileIsHardError: a caller explicitly asking for
// a payload file that does not exist must get an error, never silent
// prompt-text fallback (which would send a wrong task to the LLM).
func TestReadPayloadFile_MissingFileIsHardError(t *testing.T) {
	_, err := readPayloadFile("@/nonexistent/payload-xyz.txt")
	if err == nil {
		t.Fatal("expected hard error for missing @ payload file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/payload-xyz.txt") {
		t.Fatalf("error should name the path, got: %v", err)
	}
}

// TestReadPayloadFile_EscapedLiteral: "\@..." is literal prompt text with the
// backslash stripped — the escape hatch for talking ABOUT a file.
func TestReadPayloadFile_EscapedLiteral(t *testing.T) {
	text, err := readPayloadFile(`\@mention the file`)
	if err != nil {
		t.Fatalf("readPayloadFile: %v", err)
	}
	if text != "@mention the file" {
		t.Fatalf("got %q", text)
	}
}

// TestReadInput_AtPrefixPositional drives the full readInput classification:
// an @-positional becomes the prompt even when the file exists (it must NOT
// be double-processed as a text file path without the @ stripped).
func TestReadInput_AtPrefixPositional(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "task.md", "# Task\nFix the bug\n")
	overrides := &config.Config{Positionals: []string{"@" + path}}
	in, err := readInput(overrides)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "# Task\nFix the bug" {
		t.Fatalf("got %q", in.prompt)
	}
	if len(in.images) != 0 {
		t.Fatalf("unexpected images: %v", in.images)
	}
}

// TestReadPayloadFile_Stdin: "@-" reads the payload from piped stdin even
// though a positional was present (the normal no-positional stdin path would
// never run here).
func TestReadPayloadFile_Stdin(t *testing.T) {
	payload := "line1 & <special> \"quoted\"\nline2"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	if _, err := w.WriteString(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.Close() //stylegate:ignore test closes the os.Pipe writer so readPayloadFile sees EOF on stdin; a close error cannot alter the bytes already written

	text, err := readPayloadFile("@-")
	if err != nil {
		t.Fatalf("readPayloadFile(@-): %v", err)
	}
	if text != payload {
		t.Fatalf("got %q want %q", text, payload)
	}
}

// TestReadInput_PromptAtPrefix covers `gopi --prompt @file`.
func TestReadInput_PromptAtPrefix(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "p.txt", "line1\n<special> & \"chars\"\n")
	overrides := &config.Config{Prompt: "@" + path}
	in, err := readInput(overrides)
	if err != nil {
		t.Fatalf("readInput: %v", err)
	}
	if in.prompt != "line1\n<special> & \"chars\"" {
		t.Fatalf("got %q", in.prompt)
	}
}
