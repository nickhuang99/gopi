// cli_shot_default_out_test.go — the `gopi tool shot` face's half of the capture
// default-destination regression (task 01a1b3b1).
//
// The defect was the same on all three faces: with no `--out`, the writer built a
// BARE RELATIVE name and `os.WriteFile` resolved it against the process's cwd —
// which for a task run IS its clone ground, so each shot left an untracked PNG in
// a git work tree (PROJECT.md Rule 30). The shared resolver
// (scratch.CaptureDumpFile) is exercised end-to-end here through writeShotPNG,
// i.e. the function the CLI and the navigation-failure path both call.
package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
)

// shotFixturePorcelain returns a fixture repository's dirty entries.
func shotFixturePorcelain(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status --porcelain: %v", err)
	}
	return string(out)
}

// TestWriteShotPNGDefaultLeavesTheGroundClean is the shot face's regression
// guard: no `--out`, cwd inside a real git repository, and the repository must
// stay clean while the path the shot reports is absolute and inside the capture
// class directory.
func TestWriteShotPNGDefaultLeavesTheGroundClean(t *testing.T) {
	home := t.TempDir()
	t.Setenv(scratch.HomeDirEnvName, home)

	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"commit", "-q", "--allow-empty", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		// testutil.GitEnv strips every repository-routing name (GIT_DIR,
		// GIT_WORK_TREE, GIT_INDEX_FILE, GIT_OBJECT_DIRECTORY, GIT_COMMON_DIR)
		// from the INHERITED environment before the fixture's own pins are
		// appended. It is required, not decoration: this fixture builds its own
		// repository, and `cmd.Dir` cannot override an inherited GIT_DIR — a
		// caller that exported one would route `git init`/`git commit` at that
		// repository instead of the temp dir (the 2026-10-13 incident: 12
		// fixture commits landed on real master).
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, out)
		}
	}
	t.Chdir(repo)

	path, err := writeShotPNG("", []byte("\x89PNG\r\n\x1a\nfixture"))
	if err != nil {
		t.Fatalf("writeShotPNG with no --out: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("default shot path %q is relative — it would be resolved against the caller's cwd", path)
	}
	root, rerr := scratch.CaptureDumpRoot()
	if rerr != nil {
		t.Fatalf("scratch.CaptureDumpRoot: %v", rerr)
	}
	if !strings.HasPrefix(path, root+string(filepath.Separator)) {
		t.Fatalf("default shot landed at %q, outside the capture class directory %q", path, root)
	}
	if !scratch.IsCaptureDumpName(filepath.Base(path)) {
		t.Fatalf("default shot name %q is not collected by the capture tree — it would never be reclaimed", filepath.Base(path))
	}
	if dirty := shotFixturePorcelain(t, repo); dirty != "" {
		t.Fatalf("the shot left the ground dirty: %q (Rule 30)", dirty)
	}

	// The explicit --out path must keep behaving exactly as before: a caller that
	// names a file still gets THAT file (the fix changes the default only).
	named := filepath.Join(home, "explicit.png")
	got, err := writeShotPNG(named, []byte("png"))
	if err != nil {
		t.Fatalf("writeShotPNG with --out: %v", err)
	}
	if got != named {
		t.Fatalf("writeShotPNG with --out answered %q, want %q — an explicit destination must be honoured verbatim", got, named)
	}
}
