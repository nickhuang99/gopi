package tools

// Rebuild-after-merge engine tests (epic 2026-09-02): real git repos in
// t.TempDir + real build commands — the smallest sample that exercises the
// contract (no build entry → silent skip; declared build → runs; failure →
// notify event; version verify vs master tip). Hermetic: GOPI_SETTINGS
// points each test at its own settings.json, so the machine profile's
// repos table can never leak in.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rebuildTestGit runs git in dir (temp-repo fixture helper).
func rebuildTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// rebuildTestRepo makes a temp git repo with one commit on master and the
// master tip returned.
func rebuildTestRepo(t *testing.T) (repo, tip string) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "f.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "c1")
	rebuildTestGit(t, repo, "config", "user.email", "t@t")
	rebuildTestGit(t, repo, "config", "user.name", "t")
	return repo, rebuildTestGit(t, repo, "rev-parse", "--short", "HEAD")
}

// rebuildTestSettings writes a settings.json with the given repos table
// into the test's GOPI_HOME and returns the settings path.
func rebuildTestSettings(t *testing.T, repos map[string]map[string]any) string {
	t.Helper()
	home := t.TempDir()
	settings := filepath.Join(home, "settings.json")
	body := map[string]any{"repos": repos}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	return settings
}

// TestRebuildNoContractSkipsSilently: a repo absent from the settings
// repos table → Skipped, no build attempted, no error.
func TestRebuildNoContractSkipsSilently(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	rebuildTestSettings(t, nil) // empty repos table
	res := RebuildAfterMerge(repo)
	if !res.Skipped {
		t.Fatalf("expected Skipped for repo with no build contract, got %+v", res)
	}
	if res.Err != "" {
		t.Fatalf("no-contract skip must not error, got %q", res.Err)
	}
}

// TestRebuildDeclaredBuildRunsAndVerifies: a repo declaring a build that
// writes bin/gopi — the build command creates a script whose --version
// prints the current git tip (mirrors Go's vcs stamping: a rebuilt binary
// identifies the source it came from). The engine must run the build and
// verify the version matches the master tip.
func TestRebuildDeclaredBuildRunsAndVerifies(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	binDir := filepath.Join(repo, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The build command writes bin/gopi as a script that prints the repo's
	// current tip when invoked with --version (the GoPi convention the
	// engine verifies against).
	buildCmd := "printf '#!/bin/sh\\nif [ \"$1\" = \"--version\" ]; then git -C " + repo +
		" rev-parse --short HEAD; fi\\n' > " + filepath.Join(binDir, "gopi") +
		" && chmod +x " + filepath.Join(binDir, "gopi")
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": buildCmd, "build_timeout_sec": 60},
	})
	res := RebuildAfterMerge(repo)
	if !res.OK {
		t.Fatalf("expected OK rebuild, got %+v (err=%q)", res, res.Err)
	}
	if res.ActualVersion == "" {
		t.Fatal("expected the verified binary version to be recorded")
	}
	// Idempotency: a second call with the binary already current must skip
	// the rebuild and stay OK (no re-run of the build).
	res2 := RebuildAfterMerge(repo)
	if !res2.OK || res2.Elapsed != 0 {
		t.Fatalf("expected idempotent already-current skip, got %+v", res2)
	}
}

// TestRebuildVersionMismatchNotifies: a build that succeeds but whose
// binary reports a DIFFERENT version than the master tip → OK=false +
// build-failed notify (the operator must know the bin didn't follow).
func TestRebuildVersionMismatchNotifies(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	binDir := filepath.Join(repo, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+eventsFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "build-failed")
	// The build writes a bin/gopi whose --version prints a STALE tip
	// (hardcoded deadbeef — not the repo's master tip).
	buildCmd := "printf '#!/bin/sh\\nif [ \"$1\" = \"--version\" ]; then echo deadbeef; fi\\n' > " +
		filepath.Join(binDir, "gopi") + " && chmod +x " + filepath.Join(binDir, "gopi")
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": buildCmd, "build_timeout_sec": 60},
	})
	res := RebuildAfterMerge(repo)
	if res.OK {
		t.Fatal("expected version-mismatch failure (build ok, binary stale)")
	}
	if !strings.Contains(res.Err, "version mismatch") {
		t.Fatalf("expected version-mismatch error, got %q", res.Err)
	}
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("build-failed event not delivered: %v", err)
	}
	if !strings.Contains(string(data), "version mismatch") {
		t.Fatalf("event payload missing mismatch detail: %s", data)
	}
}

// TestRebuildBuildFailureNotifies: a failing build command → OK=false,
// Err set, and the build-failed event fires through the notify pipeline.
func TestRebuildBuildFailureNotifies(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	// Notify target: append payloads to a file.
	eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+eventsFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "build-failed")
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": "exit 3", "build_timeout_sec": 60},
	})
	res := RebuildAfterMerge(repo)
	if res.OK {
		t.Fatal("expected build failure")
	}
	if res.Err == "" {
		t.Fatal("expected Err on build failure")
	}
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("build-failed event not delivered: %v", err)
	}
	if !strings.Contains(string(data), `"event":"build-failed"`) {
		t.Fatalf("event payload missing build-failed marker: %s", data)
	}
}

// TestRebuildE2EMergeRoundBinFollowsMaster is the acceptance E2E: a
// feature branch merges into master (simulating the merger round's git
// result), then RebuildAfterMerge must produce a bin/gopi whose version
// equals the NEW master tip — the deployed binary follows master. The
// repo's "build" writes a bin/gopi that reports the CURRENT git tip at
// build time (mirrors Go's vcs stamping; a real vcs-stamped Go build is
// impossible in a test worktree, so the script is the honest stand-in for
// the stamp the canonical build produces).
func TestRebuildE2EMergeRoundBinFollowsMaster(t *testing.T) {
	repo, _ := rebuildTestRepo(t)
	// Feature branch with a new commit.
	rebuildTestGit(t, repo, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", "f.txt")
	rebuildTestGit(t, repo, "commit", "-q", "-m", "feat: v2")
	// The merge round lands the branch into master.
	rebuildTestGit(t, repo, "checkout", "-q", "master")
	rebuildTestGit(t, repo, "merge", "-q", "--no-ff", "feat/x", "-m", "Merge branch 'feat/x'")
	newTip := rebuildTestGit(t, repo, "rev-parse", "--short", "HEAD")

	// The build contract: writes a bin/gopi reporting the CURRENT tip.
	binDir := filepath.Join(repo, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildCmd := "printf '#!/bin/sh\\nif [ \"$1\" = \"--version\" ]; then git -C " + repo +
		" rev-parse --short HEAD; fi\\n' > " + filepath.Join(binDir, "gopi") +
		" && chmod +x " + filepath.Join(binDir, "gopi")
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": buildCmd, "build_timeout_sec": 60},
	})

	res := RebuildAfterMerge(repo)
	if !res.OK {
		t.Fatalf("expected OK rebuild after merge round, got %+v (err=%q)", res, res.Err)
	}
	if res.ActualVersion != newTip {
		t.Fatalf("binary version %q does not follow new master tip %q", res.ActualVersion, newTip)
	}
}
