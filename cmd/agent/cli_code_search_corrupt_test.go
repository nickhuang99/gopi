package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// TestCLICodeSearchSelfHealsCorruptIndex is the 2026-09-10 corrupt-index
// incident's CLI-level pin: a crash left `.gopi-tags` as megabytes of NUL
// bytes, and ctags refuses to overwrite an artifact that "doesn't look like a
// tag file" — so code_search stayed permanently dead (`No symbol ... found`,
// or a backend error) until a human deleted the file by hand. The CLI face is
// asserted here (real binary, real ctags invocation); the tools-level guard is
// TestCodeSearchSelfHealsCorruptIndex.
func TestCLICodeSearchSelfHealsCorruptIndex(t *testing.T) {
	if _, err := exec.LookPath("ctags"); err != nil {
		t.Skipf("ctags not installed: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package a\n\nfunc CsCliSelfHeal() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tagsPath := filepath.Join(dir, ".gopi-tags")
	if err := os.WriteFile(tagsPath, bytes.Repeat([]byte{0}, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	// A sandboxed GOPI_HOME so the run cannot touch the real profile.
	env := testutil.MockEnv(t, t.TempDir(), "http://127.0.0.1:1")
	stdout, stderr, code := runGopiMapped(t, []string{
		"code-search", "CsCliSelfHeal", "--root", dir,
	}, "", env, "")
	if code != 0 {
		t.Fatalf("code-search over a corrupt index: code=%d (a corrupt .gopi-tags must self-heal, not disable the tool)\nstdout=%s\nstderr=%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stdout, "CsCliSelfHeal") || !strings.Contains(stdout, "a.go") {
		t.Errorf("no definition hit for a symbol that exists:\n%s", stdout)
	}
	// The artifact must be REPAIRED on disk, not just ignored once.
	head := make([]byte, 16)
	f, err := os.Open(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatal(err)
	}
	if head[0] == 0 {
		t.Errorf("index still NUL-filled — the corrupt cache was not rebuilt in place (head=%q)", head)
	}
}
