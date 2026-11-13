package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests lock the codeSearchWithTimeout / keywordSearchWithTimeout
// wrappers — the two goroutine+channel+context timeout wrappers that the
// Phase-2 withTimeout[T] generic must preserve. The FIFO-block read test
// (TestReadTimeout_FIFOBlocks) already proves the timeout machinery fires;
// these pin the normal-path result delivery and error passthrough so the
// generic extraction cannot drop the return values or mishandle dropped
// counts / validation errors.

// TestCodeSearchWithTimeout_NormalPath locks that a fast code_search on a
// scratch directory delivers its structured hits through the channel (not
// the timeout branch) when the operation completes within the window.
func TestCodeSearchWithTimeout_NormalPath(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	dir := chdirTemp(t)
	src := "package demo\n\n// Foo is exported.\nfunc Foo() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, dropped, _, err := codeSearchWithTimeout(SearchScope{RootPath: "."}, "Foo", 30*time.Second)
	if err != nil {
		t.Fatalf("normal-path code_search error: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected at least one hit for Foo, got none")
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (no cap)", dropped)
	}
}

// TestCodeSearchWithTimeout_IgnoresTimeBoundary locks that a nonzero timeout
// (even a short one on a fast machine) yields the result, never a spurious
// timeout error.
func TestCodeSearchWithTimeout_ShortTimeoutNormal(t *testing.T) {
	requireRipgrep(t)
	dir := chdirTemp(t)
	// keyword_search (ripgrep) is used here — no ctags required. A fast scan
	// of a tiny tree must complete even under a short deadline.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello marker-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, _, _, err := keywordSearchWithTimeout(SearchScope{RootPath: "."}, "marker", 5*time.Second)
	if err != nil {
		t.Fatalf("normal-path keyword_search error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if !strings.HasSuffix(hits[0].FilePath, "a.txt") {
		t.Errorf("hit path = %q, want suffix a.txt", hits[0].FilePath)
	}
}

// TestKeywordSearchWithTimeout_ValidationErrorPassthrough locks that the
// wrapper forwards the impl's validation error (empty query / empty symbol)
// rather than replacing or swallowing it — the caller must see the same
// "must not be empty" message whether or not the timeout wrapper is used.
func TestKeywordSearchWithTimeout_ValidationErrorPassthrough(t *testing.T) {
	_, _, _, err := keywordSearchWithTimeout(SearchScope{}, "   ", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("expected 'must not be empty' in error, got: %v", err)
	}
}

func TestCodeSearchWithTimeout_ValidationErrorPassthrough(t *testing.T) {
	_, _, _, err := codeSearchWithTimeout(SearchScope{}, "", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for empty symbol, got nil")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("expected 'must not be empty' in error, got: %v", err)
	}
}

// TestSearchWithTimeout_NoMatchReturnsEmptyNotError locks that a clean
// no-match does NOT surface as a timeout or error — the wrappers hand back an
// empty hit list with a nil error (rg exit 1 = "no matches", not a failure),
// and the timeout branch must not falsely fire on the fast empty scan.
func TestSearchWithTimeout_NoMatchReturnsEmptyNotError(t *testing.T) {
	requireRipgrep(t)
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing relevant\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, dropped, _, err := keywordSearchWithTimeout(SearchScope{RootPath: "."}, "zzz-nope", 30*time.Second)
	if err != nil {
		t.Fatalf("no-match must not error, got: %v", err)
	}
	if len(hits) != 0 || dropped != 0 {
		t.Errorf("no-match: got %d hits, %d dropped; want 0, 0", len(hits), dropped)
	}
}

// TestCodeSearchWithTimeout_UnknownSymbolStillHit is a regression guard for
// the code_search "No symbol found" contract through the timeout wrapper: an
// unknown symbol returns an empty hit list with nil error (the fallback to
// keyword_search runs inside the wrapper and must complete fast on a tiny
// tree).
func TestCodeSearchWithTimeout_UnknownSymbolStillHit(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	dir := chdirTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "c.go"), []byte("package demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := codeSearchWithTimeout(SearchScope{RootPath: "."}, "DefinitelyNotARealSymbolXYZ", 30*time.Second)
	if err != nil {
		t.Fatalf("unknown-symbol lookup must not error, got: %v", err)
	}
}
