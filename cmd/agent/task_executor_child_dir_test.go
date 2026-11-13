package main

// task_executor_child_dir_test.go — the spawn-side cwd decision's own guard.
//
// WHY A UNIT TEST SITS UNDER AN EPIC WHOSE ACCEPTANCE IS AN E2E WALK: the bug
// that motivated this file was a silent, one-line contract violation — the seam
// assigned EscapeFromWorktree's FIRST return unconditionally, so every
// non-escaping child got an EMPTY cwd and the wrapper fell back to the session
// dir. Five cmd/agent e2e walks caught it (each takes seconds and needs the real
// binary); this test catches the same class of mistake in milliseconds, on the
// shape that matters: the resolved directory is the DEFAULT.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/daemoncwd"
	"github.com/nick/gopi/internal/scratch"
)

// TestExecutorChildDirKeepsTheResolvedDirectoryWhenNothingEscapes is the
// regression guard: a directory that is NOT a linked worktree is neutral, so the
// decision must hand it back UNCHANGED — never "" (the shape that silently
// re-pointed every executor child at the wrapper's session dir).
func TestExecutorChildDirKeepsTheResolvedDirectoryWhenNothingEscapes(t *testing.T) {
	neutral := t.TempDir()
	if got := executorChildDir(neutral, "t-no-such-row", neutral); got != neutral {
		t.Fatalf("a neutral directory must be kept as the child's cwd; got %q want %q", got, neutral)
	}
	// An unresolvable cwd stays unresolvable: the caller's `cmd.Dir != ""`
	// guard (inherit) must still see "".
	if got := executorChildDir("", "t-no-such-row", neutral); got != "" {
		t.Fatalf("an empty resolved cwd must stay empty (the caller inherits); got %q", got)
	}
}

// TestExecutorChildDirEscapesAForeignWorktreeAndReportsIt: a directory inside a
// FOREIGN linked worktree escapes to the measured process root, and the escape
// is REPORTED (Rule 15) — the child never stands in a worktree it does not own.
//
// The measurement here is the one the epic accepts on: the returned root is
// outside the worktree AND off the `/` partition (Rule 22 — a device-id
// comparison, not a spelling).
func TestExecutorChildDirEscapesAForeignWorktreeAndReportsIt(t *testing.T) {
	base := t.TempDir()
	wt := filepath.Join(base, "wt")
	nested := filepath.Join(wt, "internal", "tools")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The linked-worktree marker: .git is a FILE (a .git DIRECTORY is a main
	// checkout, deliberately not escaped).
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	got := executorChildDir(nested, "t-no-such-row", nested)
	if got == "" {
		t.Fatalf("a foreign worktree holder must escape to a MEASURED root, not to the empty/inherited cwd")
	}
	if daemoncwd.WithinWorktree(got, wt) {
		t.Fatalf("the child must not stand in the foreign worktree; got %q (worktree %q)", got, wt)
	}
	if shares, err := scratch.SharesRootFS(got); err != nil || shares {
		t.Fatalf("the escape root %q must be MEASURABLY off the `/` partition (shares=/ err=%v) — Rule 22 requires the measurement, not a different spelling", got, err)
	}
}
