// screencapture_default_out_test.go — the REGRESSION GUARD for the capture
// family's default `--out` (measured 2026-11-07, task 01a1b3b1).
//
// THE DEFECT IT PINS: a capture taken with no `--out` wrote a BARE RELATIVE
// `screen-<unix>.png`, which `os.WriteFile` resolved against the process's
// working directory. A task run's cwd IS its own clone ground, so every capture
// left an untracked PNG inside a git work tree — PROJECT.md Rule 30's exact
// defect (`completed-unclean` at close-out, and no merge path collects the file).
//
// THE ASSERTION IS THE ACCEPTANCE CRITERION ITSELF: run the writer with NO output
// path inside a real git repository, then require (a) `git status --porcelain`
// prints nothing and (b) the path it reports is an ABSOLUTE path inside the
// capture class directory of gopi's scratch tree, and the file is there. On the
// pre-fix writer both halves are red: the path was relative and the repository
// gained an untracked file.
//
// X11 IS NOT PART OF THIS QUESTION, and that is why the test drives the WRITER
// (screenCaptureWrite) rather than a capture: WHERE a default capture lands is
// decided by the writer, and a display-dependent test would go green or red for
// a reason that has nothing to do with the defect.
package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/shot"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// fixtureGitRepo creates a minimal git repository under a test temp dir and
// returns its path. The temp dir is the suite's own (internal/testutil's
// off-`/` root, Rule 22), so the fixture never touches a shared tree.
func captureFixtureGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		// testutil.GitEnv strips every repository-routing name (GIT_DIR,
		// GIT_WORK_TREE, GIT_INDEX_FILE, GIT_OBJECT_DIRECTORY, GIT_COMMON_DIR)
		// from the INHERITED environment before the fixture's own pins are
		// appended. It is required, not decoration: this fixture builds its own
		// repository, and `cmd.Dir` cannot override an inherited GIT_DIR — a
		// caller that exported one would route `git init`/`git add`/`git commit`
		// at that repository instead of the temp dir (the 2026-10-13 incident:
		// 12 fixture commits landed on real master).
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "fixture")
	return repo
}

// gitStatusPorcelain returns the fixture repository's dirty entries.
func captureFixturePorcelain(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status --porcelain: %v", err)
	}
	return string(out)
}

// TestDefaultCaptureOutLeavesTheGroundClean is the regression guard: the writer's
// default destination is OFF every git tree, and the repository it was invoked
// from stays clean.
func TestDefaultCaptureOutLeavesTheGroundClean(t *testing.T) {
	// The scratch home is pinned so the assertions below are about a directory
	// this test owns (Rule 17: t.Setenv, never os.Setenv).
	home := t.TempDir()
	t.Setenv(scratch.HomeDirEnvName, home)

	repo := captureFixtureGitRepo(t)
	if dirty := captureFixturePorcelain(t, repo); dirty != "" {
		t.Fatalf("fixture repository is dirty before the capture: %q", dirty)
	}
	// The defect was cwd-dependent, so the test must stand IN the repository —
	// this is the task-run shape (cwd == clone ground).
	t.Chdir(repo)

	// A PNG-shaped payload is enough: the writer's contract is the PATH and the
	// BYTES ON DISK, not what the pixels depict.
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	out, err := screenCaptureWrite(png, shot.Geometry{Mode: ScreenCaptureModeScreen}, "")
	if err != nil {
		t.Fatalf("screenCaptureWrite with no --out: %v", err)
	}

	// (b) the reported path is ABSOLUTE and inside the capture class directory.
	path := resultPath(t, out)
	if !filepath.IsAbs(path) {
		t.Fatalf("default capture path %q is not absolute — a relative path is resolved against the caller's cwd, which is the defect", path)
	}
	root, rerr := scratch.CaptureDumpRoot()
	if rerr != nil {
		t.Fatalf("scratch.CaptureDumpRoot: %v", rerr)
	}
	if !strings.HasPrefix(path, root+string(filepath.Separator)) {
		t.Fatalf("default capture landed at %q, outside the capture class directory %q", path, root)
	}
	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("the reported path %q does not exist: %v", path, serr)
	}
	// The name must be one the reclaim predicate accepts, or the file would sit
	// there forever (Rule 35: a destination with no collector is not a fix).
	if !scratch.IsCaptureDumpName(filepath.Base(path)) {
		t.Fatalf("default capture name %q is not a shape the capture tree collects — it would never be reclaimed", filepath.Base(path))
	}

	// (a) the ground is clean: this is the acceptance criterion.
	if dirty := captureFixturePorcelain(t, repo); dirty != "" {
		t.Fatalf("the capture left the ground dirty: %q — Rule 30 (the default destination must never be the caller's cwd)", dirty)
	}
}

// resultPath pulls the `path` field out of screenCaptureWrite's JSON payload
// without pulling encoding/json into the assertion (the payload is the tool's
// wire contract and is asserted in its own test).
func resultPath(t *testing.T, payload string) string {
	t.Helper()
	var res screenCaptureResult
	if err := json.Unmarshal([]byte(payload), &res); err != nil {
		t.Fatalf("default capture payload is not JSON: %v (%s)", err, payload)
	}
	if res.Path == "" {
		t.Fatalf("default capture payload carries no path: %s", payload)
	}
	return res.Path
}
