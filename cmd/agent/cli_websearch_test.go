package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseWebSearchArgs — the pure front-end of the web_search subcommand.
// ---------------------------------------------------------------------------

func TestParseWebSearchArgsBasic(t *testing.T) {
	opts, err := parseWebSearchArgs([]string{`VPN python`, "--method", "google", "--count", "3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.query != "VPN python" {
		t.Errorf("query = %q, want %q", opts.query, "VPN python")
	}
	if opts.method != "google" {
		t.Errorf("method = %q, want google", opts.method)
	}
	if opts.count != 3 {
		t.Errorf("count = %d, want 3", opts.count)
	}
}

func TestParseWebSearchArgsDefaults(t *testing.T) {
	opts, err := parseWebSearchArgs([]string{"test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.method != "" {
		t.Errorf("method = %q, want empty (merged)", opts.method)
	}
	if opts.count != webSearchDefaultCount {
		t.Errorf("count = %d, want default %d", opts.count, webSearchDefaultCount)
	}
}

func TestParseWebSearchArgsMethodEqualForm(t *testing.T) {
	opts, err := parseWebSearchArgs([]string{"x", "--method=arxiv", "--count=9"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.method != "arxiv" {
		t.Errorf("method = %q, want arxiv", opts.method)
	}
	if opts.count != 9 {
		t.Errorf("count = %d, want 9", opts.count)
	}
}

func TestParseWebSearchMissingQuery(t *testing.T) {
	for _, args := range [][]string{nil, {"--method", "github"}} {
		_, err := parseWebSearchArgs(args)
		if err == nil || !strings.Contains(err.Error(), "<query>") {
			t.Fatalf("want missing-query error for %v, got %v", args, err)
		}
	}
}

func TestParseWebSearchEmptyQuery(t *testing.T) {
	_, err := parseWebSearchArgs([]string{"  "})
	if err == nil || !strings.Contains(err.Error(), "<query>") {
		t.Fatalf("want missing-query error, got %v", err)
	}
}

// TestParseWebSearchMethodWithoutValueRefused: `--method` with no value is a
// usage error. The flag is spelled `--method X` in the help; omitting the flag
// is how "merged" is asked for, so a bare `--method` must not silently mean
// merged. Found by the G5 help-surface probe — the sibling `--count` already
// refused a missing value.
func TestParseWebSearchMethodWithoutValueRefused(t *testing.T) {
	_, err := parseWebSearchArgs([]string{"q", "--method"})
	if err == nil {
		t.Fatal("a bare --method was accepted — it must refuse (empty is the flag OMITTED, not passed)")
	}
	if !strings.Contains(err.Error(), "--method") {
		t.Errorf("error should name --method, got: %v", err)
	}
}

// TestParseWebSearchInvalidMethod pins the enum refusal.
func TestParseWebSearchInvalidMethod(t *testing.T) {
	_, err := parseWebSearchArgs([]string{"x", "--method", "yahoo"})
	if err == nil {
		t.Fatal("want invalid-method error, got nil")
	}
	if !strings.Contains(err.Error(), "bing|google|arxiv|deepseek|github|wiki") {
		t.Errorf("error should hint the enum, got: %v", err)
	}
}

func TestParseWebSearchCountCappedAtMax(t *testing.T) {
	opts, err := parseWebSearchArgs([]string{"x", "--count", "9999"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.count != webSearchMaxCount {
		t.Errorf("count = %d, want capped %d", opts.count, webSearchMaxCount)
	}
}

func TestParseWebSearchBadCount(t *testing.T) {
	_, err := parseWebSearchArgs([]string{"x", "--count", "abc"})
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("want bad-count error, got %v", err)
	}
	_, err = parseWebSearchArgs([]string{"x", "--count", "0"})
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("want bad-count error for 0, got %v", err)
	}
}

func TestParseWebSearchUnknownFlag(t *testing.T) {
	_, err := parseWebSearchArgs([]string{"x", "--bogus", "y"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown-flag error, got %v", err)
	}
}

// parseWebSearchArgs must not mutate the process: verify it rejects flags
// belonging to sibling subcommands (mirrors the strict tool-arg validation).
func TestParseWebSearchRejectsSiblingFlags(t *testing.T) {
	_, err := parseWebSearchArgs([]string{"x", "--db", "/tmp/x.db"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown-flag error for --db, got %v", err)
	}
	_, err = parseWebSearchArgs([]string{"x", "--limit", "3"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown-flag error for --limit, got %v", err)
	}
}
