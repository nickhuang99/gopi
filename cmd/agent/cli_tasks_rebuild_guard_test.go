package main

// Regression guard for the SPLIT dirty predicate (epic 2026-09-12): the
// post-merge rebuild guard must judge the worktree by TRACKED modifications
// only, while the adoption chain keeps the strict untracked-inclusive
// contract. Before the split the rebuild guard used the untracked-inclusive
// tools.AdoptionGitStatus, so a repo that permanently holds untracked
// artifacts (a busy task field: briefings/, logs, other agents' new files)
// never looked clean and the Rule-12 close loop (merge → cleanup → REBUILD)
// was skipped on EVERY pass — bin/gopi stayed on a pre-merge master forever.
//
// These tests pin both halves of the split:
//
//	① untracked-only  → the rebuild RUNS        (fails on the pre-fix predicate)
//	② tracked dirt    → the rebuild still DEFERS (the guard is split, not deleted)
//	③ adoption's own strict guard still REFUSES an untracked-only tree
//	   (its semantics did not silently change when the rebuild moved)

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestServeRebuildUntrackedOnlyRunsBuild is the epic's acceptance ①: ≥1
// untracked file and 0 tracked changes → the guard must NOT defer; the
// declared build must run and report ok.
func TestServeRebuildUntrackedOnlyRunsBuild(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	writeReposEntry(t, repo)

	// A busy task field's typical state: untracked scratch, no tracked edit.
	for _, name := range []string{"briefings/brief-x.md", "scratch.py", "devdoc/note.md"} {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("untracked in-flight work\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if tracked := testGit(t, repo, "status", "--porcelain", "--untracked-files=no"); tracked != "" {
		t.Fatalf("fixture must have ZERO tracked changes, got %q", tracked)
	}

	out := captureServeRebuild(t, repo)
	if strings.Contains(out, "worktree dirty") {
		t.Fatalf("untracked-only tree must NOT defer the rebuild (pre-fix predicate):\n%s", out)
	}
	if !strings.Contains(out, "built in") && !strings.Contains(out, "already current") {
		t.Fatalf("expected the rebuild to run, got:\n%s", out)
	}
	// The guard must be a SPLIT, not a deletion: the bin is really there.
	if _, err := os.Stat(filepath.Join(repo, testutil.BinDirName, testutil.BinFileName)); err != nil {
		t.Fatalf("rebuild did not produce bin/gopi: %v", err)
	}
}

// TestServeRebuildTrackedDirtStillSkips is the epic's acceptance ②: a
// TRACKED modification still defers with the existing log line — the guard
// was split, never removed.
func TestServeRebuildTrackedDirtStillSkips(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	writeReposEntry(t, repo)

	// A tracked modification (f.txt is committed by the fixture).
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v2 — in-flight tracked edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureServeRebuild(t, repo)
	if !strings.Contains(out, "worktree dirty") {
		t.Fatalf("a tracked modification must still defer the rebuild, got:\n%s", out)
	}
	if strings.Contains(out, "built in") {
		t.Fatalf("tracked dirt must never reach the build:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(repo, testutil.BinDirName, testutil.BinFileName)); err == nil {
		t.Fatal("no build may run on a tracked-dirty tree")
	}
}

// TestServeRebuildTrackedAndUntrackedSkips pins the combined case: one
// tracked edit PLUS untracked scratch → defer (the tracked edit dominates).
func TestServeRebuildTrackedAndUntrackedSkips(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	writeReposEntry(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("tracked edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scratch.md"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := captureServeRebuild(t, repo); !strings.Contains(out, "worktree dirty") {
		t.Fatalf("tracked+untracked dirt must defer, got:\n%s", out)
	}
}

// TestAdoptionCleanTreeStaysStrict is the epic's acceptance ③: the adoption
// chain's guard is UNCHANGED — an untracked-only tree is still refused
// (probe merges / ③a commits must not entangle with untracked in-flight
// work). The rebuild's split must not have leaked into it.
func TestAdoptionCleanTreeStaysStrict(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	if err := os.WriteFile(filepath.Join(repo, "scratch.md"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err := adoptionCleanTree(repo)
	if err != nil {
		t.Fatalf("adoptionCleanTree: %v", err)
	}
	if clean {
		t.Fatal("adoption's strict guard must still refuse an untracked-only tree")
	}
	// And it agrees with the rebuild guard exactly when tracked dirt exists.
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	//stylegate:ignore adoptionCleanTree's error path is unreachable here: the fixture repo was just created and
	//stylegate:ignore committed by rebuildFixtureRepo, and the assertion only reads the boolean verdict.
	if clean, _ := adoptionCleanTree(repo); clean {
		t.Fatal("adoption guard must refuse a tracked-dirty tree")
	}
}
