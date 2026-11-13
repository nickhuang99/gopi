package main

// Rebuild-after-merge serve-pass wiring tests (epic 2026-09-02): the
// cmd/agent faces (serveRebuildAfterMerge — no-contract silent skip,
// dirty-tree guard, ok path) against a sandbox git repo fixture. The
// ENGINE (real build subprocess, version verify, notify) lives in
// internal/tools/rebuild_test.go; these tests pin the LOG/guard surface of
// the merge-round-end hook.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// rebuildFixtureRepo makes a sandbox git repo (one commit on master) whose
// settings.json repos table declares buildCmd for it. Returns the repo
// path. The repo's bin/gopi is NOT pre-created — a real build contract
// creates it; tests that need a matching binary pass a build that writes
// one (see tipBinBuild below).
func rebuildFixtureRepo(t *testing.T, buildCmd string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "master")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "f.txt")
	git("commit", "-q", "-m", "c1")
	// settings.json with the repos entry.
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	body, err := json.Marshal(map[string]any{
		"repos": map[string]any{repo: map[string]any{"build": buildCmd, "build_timeout_sec": 60}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	return repo
}

// tipBinBuild returns a build command that writes repo/bin/gopi as a
// script printing the repo's CURRENT git tip on --version (the GoPi
// convention the engine verifies against).
func tipBinBuild(repo string) string {
	bin := filepath.Join(repo, testutil.BinDirName, testutil.BinFileName)
	return "mkdir -p " + filepath.Dir(bin) +
		" && printf '#!/bin/sh\\nif [ \"$1\" = \"--version\" ]; then git -C " + repo +
		" rev-parse --short HEAD; fi\\n' > " + bin + " && chmod +x " + bin
}

// TestServeRebuildNoContractSilentSkip: a repo without a settings entry →
// serveRebuildAfterMerge logs the skip and does NOT error (the deliberate
// docs-only/repo-without-declared-build silence).
func TestServeRebuildNoContractSilentSkip(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// settings with an EMPTY repos table (the repo absent from it).
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"repos":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv("GOPI_HOME", t.TempDir())
	out := captureServeRebuild(t, repo)
	if !strings.Contains(out, "no build contract") {
		t.Fatalf("expected no-contract skip log, got %q", out)
	}
}

// TestServeRebuildDirtyTreeSkips: a TRACKED-dirty worktree → the rebuild is
// deferred with a log line (never a build attempt / false build-failed).
// The dirt must be a modification of a TRACKED file: untracked-only scratch
// is the case that must RUN (epic 2026-09-12 — see
// TestServeRebuildUntrackedOnlyRunsBuild in cli_tasks_rebuild_guard_test.go;
// this fixture's untracked dirty.txt is now explicitly the RUN case).
func TestServeRebuildDirtyTreeSkips(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__")) // contract declared
	// dirty the tree with a TRACKED modification (f.txt is committed).
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("in-flight tracked edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureServeRebuild(t, repo)
	if !strings.Contains(out, "worktree dirty") {
		t.Fatalf("expected dirty-tree skip log, got %q", out)
	}
}

// TestServeRebuildOKLogs: a clean repo with a declared build that writes a
// matching bin/gopi → the rebuild runs and the ok log line appears.
func TestServeRebuildOKLogs(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	// The fixture's buildCmd references a fixed path placeholder — rewrite
	// settings with the real repo path before running.
	writeReposEntry(t, repo)
	out := captureServeRebuild(t, repo)
	if !strings.Contains(out, "built in") && !strings.Contains(out, "already current") {
		t.Fatalf("expected rebuild ok log, got %q", out)
	}
}

// writeReposEntry (re)writes settings so repo declares the real tipBinBuild.
func writeReposEntry(t *testing.T, repo string) {
	t.Helper()
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	body, err := json.Marshal(map[string]any{
		"repos": map[string]any{repo: map[string]any{"build": tipBinBuild(repo), "build_timeout_sec": 60}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
}

// captureServeRebuild runs serveRebuildAfterMerge capturing stdout+stderr.
func captureServeRebuild(t *testing.T, repo string) string {
	t.Helper()
	return captureServeRebuildWithRevert(t, repo, nil)
}

// captureServeRebuildWithRevert runs serveRebuildAfterMerge with an optional
// Mechanism-2 revert snapshot, capturing stdout+stderr.
func captureServeRebuildWithRevert(t *testing.T, repo string, revert *tools.FailedMergeRevert) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	serveRebuildAfterMerge(repo, revert)
	_ = wOut.Close() //stylegate:ignore write-end of the capture pipe; the buffered bytes are read from the other end below
	_ = wErr.Close() //stylegate:ignore write-end of the capture pipe; the buffered bytes are read from the other end below
	os.Stdout, os.Stderr = oldOut, oldErr
	outB, _ := readAll(rOut) //stylegate:ignore read end of the capture pipe; a short read is what the caller's assertion reports
	errB, _ := readAll(rErr) //stylegate:ignore read end of the capture pipe; a short read is what the caller's assertion reports
	return string(outB) + string(errB)
}

func readAll(r *os.File) ([]byte, error) {
	var b []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil {
			return b, nil
		}
	}
}

// testGit runs git in the temp repo dir and returns trimmed output.
func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestServeRebuildAutoRevertOnFailure is mechanism-2's acceptance (the
// cmd face): a build contract that FAILS after a landable merge. The
// server, handed a FailedMergeRevert snapshot (merger sweep captures
// pre-sweep state), must auto-revert master to the pre-merge buildable tip
// and log the REVERTED line — master is restored buildable, not left
// unbuildable on the failed merge.
func TestServeRebuildAutoRevertOnFailure(t *testing.T) {
	repo := rebuildFixtureRepo(t, "exit 9") // declared build ALWAYS fails

	// Land a feature branch into master (as the sweep round does).
	testGit(t, repo, "checkout", "-q", "-b", "feat/bad")
	if err := os.WriteFile(filepath.Join(repo, "broken.go"), []byte("package broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", "broken.go")
	testGit(t, repo, "commit", "-q", "-m", "bad: broken")
	badTip := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "checkout", "-q", "master")
	preTip := testGit(t, repo, "rev-parse", "HEAD")

	// Mechanism-2 snapshot (the merger captures this BEFORE the sweep).
	capture := tools.CaptureFailedMergeRevert(repo)
	rvt := &tools.FailedMergeRevert{Repo: repo, PreTip: capture.PreTip, BranchTips: capture.BranchTips}
	if capture.PreTip != preTip {
		t.Fatalf("capture preTip %q != current master pre-merge tip %q", capture.PreTip, preTip)
	}

	// Land the branch (sweep round's git result) + prune the ref (branch -d).
	testGit(t, repo, "merge", "-q", "feat/bad")
	testGit(t, repo, "branch", "-q", "-d", "feat/bad")
	mergedTip := testGit(t, repo, "rev-parse", "HEAD")
	if mergedTip == preTip {
		t.Fatal("setup: landing did not advance master")
	}
	// Branch tip preserved for the restore's recreate assertion.
	_ = badTip

	out := captureServeRebuildWithRevert(t, repo, rvt)

	// Mechanism 2 fired: master restored to the pre-merge buildable tip.
	if !strings.Contains(out, "REVERTED") {
		t.Fatalf("expected auto-revert REVERTED log, got:\n%s", out)
	}
	if strings.Contains(out, "no auto-revert") {
		t.Fatalf("unexpected no-auto-revert path: %s", out)
	}
	if got := testGit(t, repo, "rev-parse", "HEAD"); got != preTip {
		t.Fatalf("master after auto-revert = %q, want pre-merge tip %q", got, preTip)
	}
	if _, err := os.Stat(filepath.Join(repo, "broken.go")); err == nil {
		t.Fatal("broken.go must be gone from master after auto-revert (landing unwound)")
	}
}

// TestServeRebuildNoRevertWithoutSnapshot guards the opposite: serveRebuild
// with a NIL revert snapshot (the manual `gopi task merge` face / a round
// that captured nothing) must NOT reset master when the build fails — it
// logs no-auto-revert and leaves the merge landed. Never reset a merge the
// caller didn't initiate through the sweep snapshot.
func TestServeRebuildNoRevertWithoutSnapshot(t *testing.T) {
	repo := rebuildFixtureRepo(t, "exit 9") // declared build fails
	testGit(t, repo, "checkout", "-q", "-b", "feat/bad")
	if err := os.WriteFile(filepath.Join(repo, "broken.go"), []byte("package broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", "broken.go")
	testGit(t, repo, "commit", "-q", "-m", "bad: broken")
	testGit(t, repo, "checkout", "-q", "master")
	preTip := testGit(t, repo, "rev-parse", "HEAD")
	testGit(t, repo, "merge", "-q", "feat/bad")
	mergedTip := testGit(t, repo, "rev-parse", "HEAD")
	_ = mergedTip

	out := captureServeRebuild(t, repo) // nil snapshot
	if !strings.Contains(out, "no auto-revert") {
		t.Fatalf("expected no-auto-revert guard log, got:\n%s", out)
	}
	// Merge left as-is (NOT reset) — the no-snapshot guard.
	if got := testGit(t, repo, "rev-parse", "HEAD"); got == preTip {
		t.Fatal("no-snapshot serveRebuild must NOT reset master (manual/unsnapshotted merge)")
	}
}
