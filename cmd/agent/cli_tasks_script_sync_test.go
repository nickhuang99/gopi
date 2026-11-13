package main

// The merge-round-end WIRING of the installed-script refresh (rework of
// fix/launcher-install-refresh, 2026-09-12) — the cmd/agent face.
//
// internal/tools/script_install_sync_test.go pins the ENGINE (drift detection,
// byte-identity re-assertion, scope boundaries). These tests pin the two
// things only the caller can get wrong:
//
//  1. the refresh actually RUNS at the end of a merge round (the hole the
//     reproduced failure came through: the audit seam existed, nothing drove
//     the install), and
//  2. it does NOT run when this round's landing was AUTO-REVERTED — a revert
//     restores the pre-sweep tip, so refreshing from it would faithfully
//     re-install the OLD bytes and invert the drift.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// scriptSyncSeamBody is the fixture script's install seam: copy myself to the
// stable dir under HOME (the shape every operator script in this repo has).
const scriptSyncSeamBody = "  mkdir -p \"$HOME/" + scriptSyncDirRelFixture + "\"\n" +
	"  install -m 0755 \"${BASH_SOURCE[0]}\" \"$HOME/" + scriptSyncDirRelFixture + "/$SELF\"\n" +
	"  echo \"installed\"\n"

// scriptSyncDirRelFixture mirrors tools.ScriptSyncDirRel ("~/.local/bin"), the
// directory every `--install` seam writes. Spelled here rather than imported so
// the test pins the VALUE the production constant must keep.
const scriptSyncDirRelFixture = ".local/bin"

const scriptSyncScriptName = "gopi-worker-up.sh"

// seedScriptSyncRepo writes the fixture script into repo/scripts and returns
// its path.
func seedScriptSyncRepo(t *testing.T, repo string) string {
	t.Helper()
	dir := filepath.Join(repo, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, scriptSyncScriptName)
	script := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"SELF=\"${0##*/}\"\n" +
		"if [[ \"${1:-}\" == \"--install\" ]]; then\n" +
		scriptSyncSeamBody +
		"exit 0\nfi\nexit 9\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// sandboxInstalledHome points HOME at a temp dir carrying a DRIFTED installed
// copy of the fixture script (the incident's shape: same name, pre-merge
// bytes) and returns that path.
func sandboxInstalledHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home) // the fixture's own profile, not the suite pin's sandbox
	if got, err := os.UserHomeDir(); err != nil || got != home {
		t.Fatalf("sandbox HOME did not take effect: got %q err=%v want %q", got, err, home)
	}
	installed := filepath.Join(home, filepath.FromSlash(scriptSyncDirRelFixture), scriptSyncScriptName)
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("#!/usr/bin/env bash\n# the PRE-fix build\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return installed
}

// TestServeRebuildRefreshesDriftedInstalledScript is THE wiring test for the
// reproduced failure: a merge round ends, the repo's launcher bytes differ from
// the installed copy → the close loop re-installs it and says so.
//
// Before this fix the exact same fixture ended the round with the installed
// copy untouched — which is what the host did (installed 16457 B / 20:22:56 vs
// merged 19138 B / 20:31:52) while every gate stayed green.
func TestServeRebuildRefreshesDriftedInstalledScript(t *testing.T) {
	repo := rebuildFixtureRepo(t, tipBinBuild("__unused__"))
	writeReposEntry(t, repo)
	src := seedScriptSyncRepo(t, repo)
	testGit(t, repo, "add", "scripts")
	testGit(t, repo, "commit", "-q", "-m", "the merged launcher")
	installed := sandboxInstalledHome(t)

	out := captureServeRebuild(t, repo)

	if !strings.Contains(out, "refreshed") || !strings.Contains(out, scriptSyncScriptName) {
		t.Fatalf("the merge round must refresh the drifted installed copy and log it, got:\n%s", out)
	}
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("the installed copy must carry the repo's bytes after the merge round:\n got %q\nwant %q", got, want)
	}
}

// TestServeRebuildSkipsScriptSyncOnAutoRevert is the guard on the ONE ordering
// that would invert the fix: when the round's landing is auto-reverted (the
// build failed on the merged state), master is restored to the PRE-sweep tip —
// so a refresh there would re-install the OLD script bytes and re-create the
// very drift this closes.
func TestServeRebuildSkipsScriptSyncOnAutoRevert(t *testing.T) {
	repo := rebuildFixtureRepo(t, "exit 9") // declared build ALWAYS fails
	seedScriptSyncRepo(t, repo)
	testGit(t, repo, "add", "scripts")
	testGit(t, repo, "commit", "-q", "-m", "land the launcher")
	installed := sandboxInstalledHome(t)
	preRefresh, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}

	// The sweep round: capture, land a branch, prune it.
	testGit(t, repo, "checkout", "-q", "-b", "feat/bad")
	if err := os.WriteFile(filepath.Join(repo, "broken.go"), []byte("package broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "add", "broken.go")
	testGit(t, repo, "commit", "-q", "-m", "bad")
	testGit(t, repo, "checkout", "-q", "master")
	capture := tools.CaptureFailedMergeRevert(repo)
	rvt := &tools.FailedMergeRevert{Repo: repo, PreTip: capture.PreTip, BranchTips: capture.BranchTips}
	testGit(t, repo, "merge", "-q", "feat/bad")
	testGit(t, repo, "branch", "-q", "-d", "feat/bad")

	out := captureServeRebuildWithRevert(t, repo, rvt)

	if !strings.Contains(out, "REVERTED") {
		t.Fatalf("precondition: this round must auto-revert, got:\n%s", out)
	}
	if strings.Contains(out, "refreshed") {
		t.Fatalf("a reverted round must NOT refresh the installed copy (it would re-install the OLD bytes):\n%s", out)
	}
	after, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(preRefresh) {
		t.Fatalf("the installed copy must be untouched by a reverted round:\n got %q\nwant %q", after, preRefresh)
	}
}
