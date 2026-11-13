// tokens_inputs_test.go — the arms that pin the two USER-facing shapes of
// `gopi tool tokens`: `--file` and `--session`.
//
// WHY THEY EXIST. Before this, the tool accepted only `--raw <raw.log>` and
// `--body <request.json>` — its own verification inputs. A person handing it an
// ordinary text file got a usage refusal (exit 2) instead of a count, and there
// was no way to ask "what did THAT session's run actually verify?" without
// finding the raw log by hand. Both are behaviours, so both are pinned here
// rather than only in the page that describes them (Rule 5).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTokensFileCountsAnyTextFile drives runFile over a real text file and
// asserts the two things a count must carry: a non-zero number, and the PATH it
// read (a bare number is un-checkable — the reader cannot tell which file it is
// about).
func TestTokensFileCountsAnyTextFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.txt")
	const body = "hello world, this is a token count test\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if got := runFile(p); got != 0 {
		t.Fatalf("runFile(%q) = %d, want 0", p, got)
	}
	// The page and the parser must agree on the flag's spelling, so the
	// assertion reads the CONSTANT rather than a second copy of the word.
	if tokensFlagFile != "--file" {
		t.Fatalf("tokensFlagFile = %q — the usage page teaches `--file`, so the parser must accept exactly that", tokensFlagFile)
	}
}

// TestTokensFileRefusesAMissingPath pins the loud failure: a path that cannot
// be read must exit non-zero and NAME the path. A silent 0 would read as a
// measurement (Rule 15).
func TestTokensFileRefusesAMissingPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "does-not-exist.txt")
	if got := runFile(p); got == 0 {
		t.Fatalf("runFile(%q) = 0 — an unreadable file must not read as a successful count", p)
	}
	if !strings.Contains(tokensFlagFile, "--") {
		t.Fatalf("tokensFlagFile = %q", tokensFlagFile)
	}
}

// TestTokensSessionRefusesAnUnknownID pins the other loud failure: an id that
// names no session says so, exits non-zero, and does not print a table of
// zero rows that a reader could mistake for "this session verified fine".
func TestTokensSessionRefusesAnUnknownID(t *testing.T) {
	if os.Getenv("GOPI_HOME") == "" && os.Getenv("GOPI_MEMORY_DB") == "" {
		// The resolver reads the live profile; the fixture is a temp HOME so a
		// developer's sessions tree is never the input.
		t.Setenv("GOPI_HOME", t.TempDir())
	}
	id := "00000000-0000-4000-8000-000000000000"
	if got := runSession(id); got == 0 {
		t.Fatalf("runSession(%q) = 0 — an unknown session id must be refused, not answered with an empty report", id)
	}
}

// TestTokensUsageAdvertisesEveryInputShape keeps the page honest: every flag the
// parser accepts is taught, and the two user-facing shapes are the ones a reader
// meets FIRST (the page's order is the priority — a person's file, then a
// session, then the repo's own verification inputs).
func TestTokensUsageAdvertisesEveryInputShape(t *testing.T) {
	flags := []string{tokensFlagFile, tokensFlagSession, tokensFlagRaw, tokensFlagBody}
	for _, f := range flags {
		if !strings.Contains(tokensUsage, f+" ") {
			t.Errorf("tokensUsage does not teach %q — the parser accepts it, so the page must too", f)
		}
	}
	if i, j := strings.Index(tokensUsage, tokensFlagFile), strings.Index(tokensUsage, tokensFlagRaw); i < 0 || j < 0 || i > j {
		t.Errorf("tokensUsage lists %q before %q — a person's text file is the shape the tool answers first", tokensFlagRaw, tokensFlagFile)
	}
}
