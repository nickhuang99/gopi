package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/text2img"
)

// ---------------------------------------------------------------------------
// parseText2imgArgs — the pure front-end of the text2img subcommand.
// ---------------------------------------------------------------------------

func TestParseText2imgArgsBasic(t *testing.T) {
	opts, err := parseText2imgArgs([]string{"/tmp/notes.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.input != "/tmp/notes.txt" {
		t.Errorf("input = %q, want /tmp/notes.txt", opts.input)
	}
	if opts.output != "" {
		t.Errorf("output = %q, want empty (the default destination is resolved by the writer, not the parser)", opts.output)
	}
	// Defaults = the OCR-validated claude-version settings.
	if opts.size != 20 || opts.cols != 50 || opts.lh != 32 || opts.lines != 90 || opts.margin != 12 {
		t.Errorf("defaults wrong: size=%d cols=%d lh=%d lines=%d margin=%d",
			opts.size, opts.cols, opts.lh, opts.lines, opts.margin)
	}
}

func TestParseText2imgArgsFull(t *testing.T) {
	opts, err := parseText2imgArgs([]string{"report.txt", "out.png", "--size", "28", "--cols", "60", "--lh", "36", "--lines", "60", "--margin", "20", "--font", "/tmp/f.ttf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.input != "report.txt" || opts.output != "out.png" {
		t.Errorf("input/output = %q/%q, want report.txt/out.png", opts.input, opts.output)
	}
	if opts.size != 28 || opts.cols != 60 || opts.lh != 36 || opts.lines != 60 || opts.margin != 20 {
		t.Errorf("overrides wrong: size=%d cols=%d lh=%d lines=%d margin=%d",
			opts.size, opts.cols, opts.lh, opts.lines, opts.margin)
	}
	if opts.font != "/tmp/f.ttf" {
		t.Errorf("font = %q, want /tmp/f.ttf", opts.font)
	}
}

func TestParseText2imgArgsEqualForm(t *testing.T) {
	opts, err := parseText2imgArgs([]string{"--size=24", "--cols=40", "in.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.size != 24 || opts.cols != 40 {
		t.Errorf("size/cols = %d/%d, want 24/40", opts.size, opts.cols)
	}
	if opts.input != "in.txt" {
		t.Errorf("input = %q, want in.txt", opts.input)
	}
}

func TestParseText2imgArgsMissingInput(t *testing.T) {
	for _, args := range [][]string{nil, {"--size", "20"}, {""}} {
		_, err := parseText2imgArgs(args)
		if err == nil || !strings.Contains(err.Error(), "<input.txt>") {
			t.Fatalf("want missing <input.txt> error for %v, got %v", args, err)
		}
	}
}

func TestParseText2imgArgsExtraPositional(t *testing.T) {
	_, err := parseText2imgArgs([]string{"a.txt", "out.png", "extra.png"})
	if err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("want extra-argument error, got %v", err)
	}
}

func TestParseText2imgArgsUnknownFlag(t *testing.T) {
	_, err := parseText2imgArgs([]string{"a.txt", "--bogus", "1"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag --bogus") {
		t.Fatalf("want unknown-flag error, got %v", err)
	}
}

// TestParseText2imgArgsOutFlag is the minimal repro for the defect this file's
// sibling fix addresses: `gopi tool text2img <in> --out <out>` was refused with
// `unknown flag --out` (exit 1) even though `out` is the AGENT TOOL's own
// parameter name for the very same argument (internal/agent/loop.go's text2img
// schema). Red before the fix — parseText2imgArgs returned that refusal for
// every case below — green after it.
func TestParseText2imgArgsOutFlag(t *testing.T) {
	// (1) The flag alone, with the input positional. Pre-fix: `unknown flag --out`.
	opts, err := parseText2imgArgs([]string{"a.txt", "--out", "rendered.png"})
	if err != nil {
		t.Fatalf("`a.txt --out rendered.png`: unexpected error: %v", err)
	}
	if opts.output != "rendered.png" {
		t.Errorf("output = %q, want rendered.png", opts.output)
	}
	if opts.input != "a.txt" {
		t.Errorf("input = %q, want a.txt", opts.input)
	}

	// (2) The `--out=VALUE` spelling the shared parser also accepts.
	opts, err = parseText2imgArgs([]string{"a.txt", "--out=rendered.png"})
	if err != nil {
		t.Fatalf("`a.txt --out=rendered.png`: unexpected error: %v", err)
	}
	if opts.output != "rendered.png" {
		t.Errorf("--out=VALUE: output = %q, want rendered.png", opts.output)
	}

	// (3) Coexistence: the positional form still works, unchanged.
	opts, err = parseText2imgArgs([]string{"a.txt", "rendered.png"})
	if err != nil {
		t.Fatalf("positional form: unexpected error: %v", err)
	}
	if opts.output != "rendered.png" {
		t.Errorf("positional form: output = %q, want rendered.png", opts.output)
	}

	// (4) Both spellings naming the SAME path agree — not a conflict.
	opts, err = parseText2imgArgs([]string{"a.txt", "rendered.png", "--out", "rendered.png"})
	if err != nil {
		t.Fatalf("agreeing spellings: unexpected error: %v", err)
	}
	if opts.output != "rendered.png" {
		t.Errorf("agreeing spellings: output = %q, want rendered.png", opts.output)
	}

	// (5) The flag mixed with the numeric flags in one command.
	opts, err = parseText2imgArgs([]string{"a.txt", "--out", "rendered.png", "--size", "28"})
	if err != nil {
		t.Fatalf("mixed with --size: unexpected error: %v", err)
	}
	if opts.output != "rendered.png" || opts.size != 28 {
		t.Errorf("mixed with --size: output=%q size=%d, want rendered.png/28", opts.output, opts.size)
	}
}

// TestParseText2imgArgsOutFlagRefusals pins the two shapes the flag does NOT
// silently accept (Rule 15): a `--out` spelled with no operand, and two
// DIFFERENT output paths named in one command. Neither may be resolved for the
// user — the first because empty is a value that was spelled, the second
// because whichever path won, the user would be wrong about the other.
func TestParseText2imgArgsOutFlagRefusals(t *testing.T) {
	// Given-but-empty: a trailing `--out` and `--out=` are "given", not absent.
	for _, args := range [][]string{
		{"a.txt", "--out"},
		{"a.txt", "--out="},
	} {
		_, err := parseText2imgArgs(args)
		if err == nil || !strings.Contains(err.Error(), text2imgOutputEmptyMessage) {
			t.Errorf("%v: want the %q refusal, got %v", args, text2imgOutputEmptyMessage, err)
		}
	}

	// Two different outputs: refused, with BOTH named so the user can see the
	// disagreement rather than guess which one was used.
	_, err := parseText2imgArgs([]string{"a.txt", "one.png", "--out", "two.png"})
	if err == nil {
		t.Fatalf("`a.txt one.png --out two.png`: want a refusal, got nil (output silently chosen)")
	}
	for _, want := range []string{"one.png", "two.png", text2imgFlagOut} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("conflict refusal %q does not name %q", err, want)
		}
	}

	// An ABSENT --out is not the empty-refusal: no flag, no error (that is the
	// <input>.png default path).
	if _, err := parseText2imgArgs([]string{"a.txt"}); err != nil {
		t.Errorf("absent --out: unexpected error: %v", err)
	}
}

// TestParseText2imgArgsBadInt pins the parse-layer half of the positive-int
// contract: a value that is not a positive integer comes back as the sentinel,
// and ABSENT comes back as the flag's own default (the two paths must stay
// distinguishable — that difference is what lets dispatch refuse only what the
// user actually spelled).
func TestParseText2imgArgsBadInt(t *testing.T) {
	for _, v := range []string{"abc", "-5", "0"} {
		opts, err := parseText2imgArgs([]string{"a.txt", "--size", v})
		if err != nil {
			t.Fatalf("--size %s: unexpected error: %v", v, err)
		}
		if opts.size != text2imgInvalidInt {
			t.Errorf("--size %s: size = %d, want the sentinel %d", v, opts.size, text2imgInvalidInt)
		}
	}
	// A flag SPELLED with nothing after it is "given", not "absent": both the
	// shared parser's spellings (a trailing `--size`, and `--size=`) mean the
	// user asked for a size and supplied none — the sentinel, not the default.
	for _, args := range [][]string{
		{"a.txt", "--size"},
		{"a.txt", "--size="},
		{"a.txt", "--size=", "--cols", "40"},
	} {
		opts, err := parseText2imgArgs(args)
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
		if opts.size != text2imgInvalidInt {
			t.Errorf("%v: size = %d, want the sentinel %d", args, opts.size, text2imgInvalidInt)
		}
	}
	// Absent is NOT the sentinel: it is the documented default.
	opts, err := parseText2imgArgs([]string{"a.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.size != text2img.DefaultSize {
		t.Errorf("absent --size = %d, want the default %d", opts.size, text2img.DefaultSize)
	}
}
