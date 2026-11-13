package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// porcelain parsing
// ---------------------------------------------------------------------------

func TestParseGitBlamePorcelain(t *testing.T) {
	// Mirrors REAL `git blame --porcelain` output: the first line of a run
	// carries the full header block, then git re-emits a shortened header
	// (hash orig final, no fields) before each subsequent line.
	hash1 := strings.Repeat("a", 40)
	hash2 := strings.Repeat("b", 40)
	out := hash1 + ` 1 1 2
author nick
author-mail <nick@example.com>
author-time 1750000000
author-tz +0800
committer nick
committer-mail <nick@example.com>
committer-time 1750000000
committer-tz +0800
summary fix: memory update avoid duplicate write
filename internal/memory/store.go
	func UpdateMemory(...) error {
` + hash1 + ` 2 2
		meta := Metadata{}
` + hash2 + ` 3 3
author nick
author-mail <nick@example.com>
author-time 1740000000
author-tz +0000
committer nick
committer-mail <nick@example.com>
committer-time 1740000000
committer-tz +0000
summary feat: add memory metadata tag
filename internal/memory/store.go
	return nil
`
	items, err := parseGitBlamePorcelain(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 line items, got %d", len(items))
	}
	want := []GitBlameLineItem{
		{LineNo: 1, CommitHash: hash1, Author: "nick",
			Timestamp: "2025-06-15T15:06:40Z", CommitMessage: "fix: memory update avoid duplicate write", CodeLine: "func UpdateMemory(...) error {"},
		// Incremental header for the same commit must NOT wipe the fields.
		// CodeLine keeps the file's own indentation (one porcelain marker
		// tab is stripped).
		{LineNo: 2, CommitHash: hash1, Author: "nick",
			Timestamp: "2025-06-15T15:06:40Z", CommitMessage: "fix: memory update avoid duplicate write", CodeLine: "	meta := Metadata{}"},
		{LineNo: 3, CommitHash: hash2, Author: "nick",
			Timestamp: "2025-02-19T21:20:00Z", CommitMessage: "feat: add memory metadata tag", CodeLine: "return nil"},
	}
	for i, w := range want {
		got := items[i]
		if got.LineNo != w.LineNo || got.CommitHash != w.CommitHash || got.Author != w.Author ||
			got.Timestamp != w.Timestamp || got.CommitMessage != w.CommitMessage || got.CodeLine != w.CodeLine {
			t.Errorf("item %d mismatch:\n got %+v\nwant %+v", i, got, w)
		}
	}
}

func TestParseGitBlamePorcelainBoundary(t *testing.T) {
	// A boundary commit (all-zero sha, "boundary" marker) must not break
	// parsing — the item is emitted with whatever fields exist.
	out := `0000000000000000000000000000000000000000 1 1 1
boundary
filename hello.go
	line one
`
	items, err := parseGitBlamePorcelain(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].LineNo != 1 || items[0].CodeLine != "line one" {
		t.Errorf("unexpected item: %+v", items[0])
	}
}

// ---------------------------------------------------------------------------
// path escape protection
// ---------------------------------------------------------------------------

func TestResolveInRepo(t *testing.T) {
	root := t.TempDir()

	// Normal repo-relative path.
	p, err := resolveInRepo(root, "internal/store.go")
	if err != nil {
		t.Fatalf("normal path rejected: %v", err)
	}
	if p != filepath.Join(root, "internal/store.go") {
		t.Errorf("unexpected path %q", p)
	}

	// ".." escape.
	if _, err := resolveInRepo(root, "../../etc/passwd"); err == nil {
		t.Error("expected escape rejected, got nil error")
	}
	// Absolute path.
	if _, err := resolveInRepo(root, "/etc/passwd"); err == nil {
		t.Error("expected absolute path rejected, got nil error")
	}
	// Bare / junk paths.
	if _, err := resolveInRepo(root, "."); err == nil {
		t.Error("expected '.' rejected, got nil error")
	}
	if _, err := resolveInRepo(root, ""); err == nil {
		t.Error("expected '' rejected, got nil error")
	}

	// Symlink out of the repo must be rejected.
	linkTarget := t.TempDir()
	secret := filepath.Join(linkTarget, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "leak")
	if err := os.Symlink(linkTarget, link); err == nil {
		if _, err := resolveInRepo(root, "leak/secret.txt"); err == nil {
			t.Error("expected symlink escape rejected, got nil error")
		}
	} else {
		t.Logf("symlink unsupported, skipping: %v", err)
	}
}

// ---------------------------------------------------------------------------
// full git integration
// ---------------------------------------------------------------------------

// TestGitBlameIntegration exercises the real tool path against a temp git
// repo: init → two commits → blame a range → structured JSON back. Skips
// when git is absent (the tool is optional by design).
func TestGitBlameIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — optional tool")
	}
	root := t.TempDir()
	// Isolate the memory linkage from the real knowledge base.
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	hello := filepath.Join(root, "hello.go")
	if err := os.WriteFile(hello, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "hello.go")
	runGit("commit", "-q", "-m", "feat: initial hello")
	if err := os.WriteFile(hello, []byte("line1\nline2\nline3\nline4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("commit", "-q", "-am", "fix: add line4")

	res := gitBlameAt(root, "hello.go", 1, 4)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(res), &parsed); err != nil {
		t.Fatalf("expected JSON result, got %q", res)
	}
	if e, ok := parsed["error"]; ok {
		t.Fatalf("unexpected error result: %v", e)
	}
	if parsed["file_path"] != "hello.go" {
		t.Errorf("file_path mismatch: %v", parsed["file_path"])
	}
	items, ok := parsed["line_items"].([]interface{})
	if !ok || len(items) != 4 {
		t.Fatalf("expected 4 line_items, got %v", parsed["line_items"])
	}
	// Lines 1-3 carry the first commit's message, line 4 the second's.
	first := items[0].(map[string]interface{})
	if first["commit_message"] != "feat: initial hello" {
		t.Errorf("line1 message mismatch: %v", first["commit_message"])
	}
	if first["line_no"] != float64(1) {
		t.Errorf("line1 line_no mismatch: %v", first["line_no"])
	}
	last := items[3].(map[string]interface{})
	if last["commit_message"] != "fix: add line4" {
		t.Errorf("line4 message mismatch: %v", last["commit_message"])
	}
	if ts, ok := last["timestamp"].(string); !ok || !strings.Contains(ts, "T") {
		t.Errorf("line4 timestamp not RFC3339: %v", last["timestamp"])
	}
}

// TestGitBlameStructuredErrors: the exception fallbacks return structured
// JSON errors, never raw git output.
func TestGitBlameStructuredErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — optional tool")
	}
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	hello := filepath.Join(root, "hello.go")
	if err := os.WriteFile(hello, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "hello.go")
	runGit("commit", "-q", "-m", "feat: initial hello")
	// A big tracked file for the too-wide-range case (need > gitBlameMaxSpan
	// lines so the clipped range still exceeds the cap).
	big := filepath.Join(root, "big.go")
	var bb strings.Builder
	for i := 1; i <= 250; i++ {
		fmt.Fprintf(&bb, "line %d\n", i)
	}
	if err := os.WriteFile(big, []byte(bb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "big.go")
	runGit("commit", "-q", "-m", "feat: big file")

	expectErr := func(res, wantSub string) {
		t.Helper()
		var e GitBlameError
		if err := json.Unmarshal([]byte(res), &e); err != nil || e.Error == "" {
			t.Fatalf("expected structured error containing %q, got %q", wantSub, res)
		}
		if !strings.Contains(e.Error, wantSub) {
			t.Errorf("error %q missing %q", e.Error, wantSub)
		}
	}

	// Missing file (outside a git context the wrapper's repo check is what
	// fires; gitBlameAt itself reports the first missing link).
	expectErr(gitBlameAt(t.TempDir(), "x.go", 0, 0), "not found")

	// File not found in the repo.
	expectErr(gitBlameAt(root, "nope.go", 0, 0), "not found")

	// Untracked file.
	untracked := filepath.Join(root, "new.go")
	if err := os.WriteFile(untracked, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectErr(gitBlameAt(root, "new.go", 0, 0), "not tracked")

	// Out-of-bounds range.
	expectErr(gitBlameAt(root, "hello.go", 10, 20), "out of bounds")

	// Too-wide range: clipped to the file's 250 lines, still > 200 cap.
	expectErr(gitBlameAt(root, "big.go", 1, 300), "too wide")

	// Path escape.
	expectErr(gitBlameAt(root, "../../etc/passwd", 0, 0), "escapes")

	// Valid call still works after all the failures (sanity): omitted range
	// on the 3-line file returns the whole file (default window clipped).
	res := gitBlameAt(root, "hello.go", 0, 0)
	var ok GitBlameResult
	if err := json.Unmarshal([]byte(res), &ok); err != nil || len(ok.LineItems) != 3 {
		t.Fatalf("expected valid result, got %q", res)
	}
}
