package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseVisionArgs — the pure front-end of the vision_analyze subcommand.
// ---------------------------------------------------------------------------

func TestParseVisionArgsBasic(t *testing.T) {
	opts, err := parseVisionArgs([]string{"/tmp/x.png", "--prompt", "识别图片中的所有中文文字"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.imagePath != "/tmp/x.png" {
		t.Errorf("imagePath = %q, want /tmp/x.png", opts.imagePath)
	}
	if opts.prompt != "识别图片中的所有中文文字" {
		t.Errorf("prompt = %q, want the Chinese prompt", opts.prompt)
	}
}

func TestParseVisionArgsDefaults(t *testing.T) {
	opts, err := parseVisionArgs([]string{"shot.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.imagePath != "shot.png" {
		t.Errorf("imagePath = %q, want shot.png", opts.imagePath)
	}
	if opts.prompt != "" {
		t.Errorf("prompt = %q, want empty (tool default applies)", opts.prompt)
	}
}

func TestParseVisionArgsPromptEqualForm(t *testing.T) {
	opts, err := parseVisionArgs([]string{"--prompt=describe the chart", "chart.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.imagePath != "chart.png" {
		t.Errorf("imagePath = %q, want chart.png", opts.imagePath)
	}
	if opts.prompt != "describe the chart" {
		t.Errorf("prompt = %q, want the given prompt", opts.prompt)
	}
}

func TestParseVisionArgsMissingImagePath(t *testing.T) {
	for _, args := range [][]string{nil, {"--prompt", "x"}, {""}} {
		_, err := parseVisionArgs(args)
		if err == nil || !strings.Contains(err.Error(), "<image_path>") {
			t.Fatalf("want missing <image_path> error for %v, got %v", args, err)
		}
	}
}

func TestParseVisionArgsExtraPositional(t *testing.T) {
	_, err := parseVisionArgs([]string{"a.png", "b.png"})
	if err == nil || !strings.Contains(err.Error(), "extra argument") {
		t.Fatalf("want extra-argument error, got %v", err)
	}
}

func TestParseVisionArgsUnknownFlag(t *testing.T) {
	_, err := parseVisionArgs([]string{"a.png", "--bogus", "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag --bogus") {
		t.Fatalf("want unknown-flag error, got %v", err)
	}
}

// TestParseVisionArgsRejectsSiblingFlags ensures subcommand flags that belong
// to other subcommands do not silently pass through (they must error instead
// of being treated as the image path / ignored).
func TestParseVisionArgsRejectsSiblingFlags(t *testing.T) {
	for _, args := range [][]string{
		{"a.png", "--method", "google"},
		{"a.png", "--count", "3"},
		{"a.png", "--last", "5"},
	} {
		if _, err := parseVisionArgs(args); err == nil {
			t.Errorf("parseVisionArgs(%v) should reject the sibling flag", args)
		}
	}
}

func TestIsVisionSubcommandNegative(t *testing.T) {
	if isVisionSubcommand(nil) {
		t.Error("isVisionSubcommand(nil) = true, want false")
	}
	// The word is a SIBLING verb this predicate must not claim (Rule 1: a word
	// is a decision value, so it is referenced from the constant that owns it,
	// never typed). `log` is the session family's reader — a different door on
	// the same binary, which is exactly what "vision must not shadow it" means.
	if isVisionSubcommand([]string{CliVerbLog}) {
		t.Errorf("isVisionSubcommand([%s]) = true, want false", CliVerbLog)
	}
	if isVisionSubcommand([]string{"web_search", "q"}) {
		t.Error("isVisionSubcommand([web_search]) = true, want false")
	}
}
