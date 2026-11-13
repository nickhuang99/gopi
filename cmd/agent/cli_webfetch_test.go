package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseWebFetchArgs — the pure front-end of the web_fetch subcommand.
// ---------------------------------------------------------------------------

func TestParseWebFetchArgsBasic(t *testing.T) {
	opts, err := parseWebFetchArgs([]string{"https://example.com/a", "--max-chars", "2000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.url != "https://example.com/a" {
		t.Errorf("url = %q, want https://example.com/a", opts.url)
	}
	if opts.maxChars != 2000 {
		t.Errorf("maxChars = %d, want 2000", opts.maxChars)
	}
}

func TestParseWebFetchArgsDefaults(t *testing.T) {
	opts, err := parseWebFetchArgs([]string{"https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.maxChars != webFetchDefaultMaxChars {
		t.Errorf("maxChars = %d, want default %d", opts.maxChars, webFetchDefaultMaxChars)
	}
}

func TestParseWebFetchArgsEqualForm(t *testing.T) {
	opts, err := parseWebFetchArgs([]string{"https://example.com", "--max-chars=25000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.maxChars != 25000 {
		t.Errorf("maxChars = %d, want 25000", opts.maxChars)
	}
}

func TestParseWebFetchArgsCapClamped(t *testing.T) {
	opts, err := parseWebFetchArgs([]string{"https://example.com", "--max-chars", "999999"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.maxChars != webFetchMaxMaxChars {
		t.Errorf("maxChars = %d, want capped %d", opts.maxChars, webFetchMaxMaxChars)
	}
}

func TestParseWebFetchMissingURL(t *testing.T) {
	for _, args := range [][]string{nil, {"--max-chars", "100"}} {
		_, err := parseWebFetchArgs(args)
		if err == nil || !strings.Contains(err.Error(), "<url>") {
			t.Fatalf("want missing-url error for %v, got %v", args, err)
		}
	}
}

func TestParseWebFetchBadMaxChars(t *testing.T) {
	for _, args := range [][]string{
		{"https://example.com", "--max-chars", "abc"},
		{"https://example.com", "--max-chars", "1.5"},
	} {
		_, err := parseWebFetchArgs(args)
		if err == nil || !strings.Contains(err.Error(), "--max-chars") {
			t.Fatalf("want --max-chars error for %v, got %v", args, err)
		}
	}
}

// TestParseWebFetchNonPositiveMaxCharsRestoresDefault pins the help's own
// promise — "Values <= 0 restore the default" — which the parser used to
// REFUSE with "--max-chars must be a positive integer". The clamp already
// lives in the tool layer (tools.WebFetch → clampWebFetchMaxChars), and this
// CLI is documented as the same function path, so the two must agree.
// Found by the G5 help-surface probe; a plain in-process test was not enough
// because the page was the thing that disagreed.
func TestParseWebFetchNonPositiveMaxCharsRestoresDefault(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		opts, err := parseWebFetchArgs([]string{"https://example.com", "--max-chars", v})
		if err != nil {
			t.Fatalf("--max-chars %s: the help says <= 0 restores the default, but the parser refused: %v", v, err)
		}
		if opts.maxChars != webFetchDefaultMaxChars {
			t.Errorf("--max-chars %s → maxChars %d, want the default %d", v, opts.maxChars, webFetchDefaultMaxChars)
		}
	}
}

func TestParseWebFetchUnknownFlagRejected(t *testing.T) {
	_, err := parseWebFetchArgs([]string{"https://example.com", "--method", "wiki"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag --method") {
		t.Fatalf("want unknown-flag error, got %v", err)
	}
}

func TestParseWebFetchURLWhitespaceTrimmed(t *testing.T) {
	opts, err := parseWebFetchArgs([]string{"  https://example.com/x  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.url != "https://example.com/x" {
		t.Errorf("url = %q, want trimmed", opts.url)
	}
}

// isWebFetchSubcommand dispatch guard: matches only exact "web_fetch" argv[0].
func TestIsWebFetchSubcommandDispatch(t *testing.T) {
	if isWebFetchSubcommand(nil) {
		t.Fatal("nil args must not match")
	}
	if isWebFetchSubcommand([]string{"web_search", "x"}) {
		t.Fatal("web_search must not match web_fetch dispatch")
	}
	// Exact match would run the subcommand (process exit) — only verify the
	// negative space here; the positive path is covered by the live smoke.
}

// webFetchUsage guard: the usage text stays greppable for both flags.
func TestWebFetchUsageMentionsFlags(t *testing.T) {
	for _, want := range []string{"--max-chars", "{status,url,chars,truncated,content"} {
		if !strings.Contains(webFetchUsage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
