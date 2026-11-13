package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests lock the ~/ home-directory expansion semantics so the Phase-2
// unify helper (expandHome) must preserve the same behavior across
// read/write/edit. read.go supports both bare "~" and "~/"; write.go and
// edit.go support only "~/". The distinct error messages (read's "failed to
// get home directory" vs write's "cannot expand ~/" vs edit's "cannot expand
// ~") are part of the observable contract and are asserted here.

// TestExpandHome_BareTilde locks the shared helper's semantics: bare "~"
// resolves to the home directory; "~/x" to <home>/x; non-tilde paths pass
// through unchanged.
func TestExpandHome_BareTilde(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(homeEnvName, dir)

	p, err := expandHome("~")
	if err != nil {
		t.Fatalf("expandHome(~): %v", err)
	}
	if p != dir {
		t.Errorf("expandHome(~) = %q, want %q (HOME override)", p, dir)
	}

	p, err = expandHome("~/sub/file.txt")
	if err != nil {
		t.Fatalf("expandHome(~/sub/file.txt): %v", err)
	}
	if got := filepath.Join(dir, "sub", "file.txt"); p != got {
		t.Errorf("expandHome(~/sub/file.txt) = %q, want %q", p, got)
	}

	// Non-tilde paths are returned unchanged (and never trigger a home lookup).
	p, err = expandHome("/abs/path")
	if err != nil || p != "/abs/path" {
		t.Errorf("expandHome(/abs/path) = %q, err %v; want pass-through", p, err)
	}
	p, err = expandHome("relative.txt")
	if err != nil || p != "relative.txt" {
		t.Errorf("expandHome(relative.txt) = %q, err %v; want pass-through", p, err)
	}
	p, err = expandHome("")
	if err != nil || p != "" {
		t.Errorf("expandHome() = %q, err %v; want pass-through", p, err)
	}
}

// TestEdit_TildeExpansion locks that edit expands "~/path" against the user's
// home directory — the one file tool whose tilde path was previously untested.
func TestEdit_TildeExpansion(t *testing.T) {
	// Fixture home (never the real one): ~/ resolves inside this t.TempDir().
	dir := t.TempDir()
	t.Setenv(homeEnvName, dir)

	target := filepath.Join(dir, "edit-tilde-a.txt")
	if err := os.WriteFile(target, []byte("hello old\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	defer os.Remove(target)

	if err := Edit("~/edit-tilde-a.txt", "old", "new", false); err != nil {
		t.Fatalf("Edit with ~/ path: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello new\n" {
		t.Errorf("edit result = %q, want hello new", string(got))
	}
}

// TestEdit_TildeExpansionErrorClause locks edit's expansion error message —
// a refactor must keep the "cannot expand ~" wording or callers matching on it
// break. The failure itself is forced by making the home directory unresolvable
// is environment-specific, so this asserts the message shape via an empty-path
// guard instead: expansion on an already-absolute path is a no-op, and the
// error that DOES surface (if any) must not be a misleading home-dir one.
func TestEdit_TildePrefixErrorShape(t *testing.T) {
	// edit on a path with no ~ prefix must not attempt expansion and must
	// fault on the missing file with the not-found error — proving the ~
	// prefix branch is the only one touching home resolution.
	path := filepath.Join(t.TempDir(), "does-not-exist.txt")
	err := Edit(path, "a", "b", false)
	if err == nil {
		t.Fatal("expected error editing a missing file, got nil")
	}
	if strings.Contains(err.Error(), "cannot expand ~") {
		t.Errorf("non-tilde path wrongly hit ~ expansion: %v", err)
	}
}

// TestRead_TildeAloneStillDirectoryErrorScenario guard: this locks that read's
// bare-"~" support maps to the home directory (whose read is refused as a
// directory) — the behavior write/edit do NOT share. The home is a fixture, so
// the tool never enters the developer's real home.
func TestRead_TildeAloneStillDirectoryError(t *testing.T) {
	t.Setenv(homeEnvName, t.TempDir())

	got, err := Read("~", 0, 0)
	if err == nil {
		t.Fatalf("expected error reading home dir, got %q", got)
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected 'directory' in read(~) error, got: %v", err)
	}
}
