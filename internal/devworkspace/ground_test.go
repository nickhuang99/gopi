package devworkspace

// ground_test.go — the FOUR measured shapes of ResolveGroundState, one test
// each, on REAL git (no fake): the clone model, the linked-worktree model, a
// directory with no ground at all, and a linked worktree whose registration is
// gone (`.git` still a file, its gitdir removed).
//
// THE REGRESSION THIS FILE PINS (measured 2026-10-19, row 01a150b3): the clone
// channel's workspace — a standalone `git clone`, `.git` a DIRECTORY, registered
// in no worktree list — was reported by `gopi worker --resume` as "NOT a live
// linked worktree … the ground is gone" while the ledger's advisory line called
// the same directory "verified live". Case (a) here is the fact that settles it:
// a clone is ALIVE without being registered anywhere.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// groundTestRepo builds a repository with one commit on DefaultBranch.
func groundTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", DefaultBranch)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

// TestResolveGroundStateCloneIsAliveWithoutRegistration is case (a): `.git` is a
// DIRECTORY, so the directory holds its own repository. It is alive, and NOTHING
// registers it — the model that the linked-only reader reported as "gone".
func TestResolveGroundStateCloneIsAliveWithoutRegistration(t *testing.T) {
	repo := groundTestRepo(t)
	// The clone model's own shape: a standalone clone of that repo, the way the
	// clone channel's Open builds a workspace.
	ws := filepath.Join(t.TempDir(), "workspace-clone")
	gitIn(t, filepath.Dir(ws), "clone", "-q", repo, ws)

	info, err := os.Stat(filepath.Join(ws, ".git"))
	if err != nil || !info.IsDir() {
		t.Fatalf("the fixture must be the CLONE shape (.git a directory): err=%v", err)
	}
	// And it is registered NOWHERE: the main repository does not list it.
	if out := gitIn(t, repo, "worktree", "list", "--porcelain"); strings.Contains(out, ws) {
		t.Fatalf("the fixture must be UNREGISTERED (a clone is not a worktree):\n%s", out)
	}

	st := ResolveGroundState(ws)
	if !st.Alive {
		t.Fatalf("a CLONE workspace must be alive — a clone is registered nowhere and needs no registration; got %+v", st)
	}
	if st.Model != GroundModelClone {
		t.Errorf("model = %q, want %q", st.Model, GroundModelClone)
	}
	if st.Repo != ws {
		t.Errorf("repo = %q, want the clone's own directory %q", st.Repo, ws)
	}
	if st.Branch != DefaultBranch {
		t.Errorf("branch = %q, want %q", st.Branch, DefaultBranch)
	}
	for _, want := range []string{".git is a DIRECTORY", "CLONE model", "registered nowhere"} {
		if !strings.Contains(st.Detail, want) {
			t.Errorf("the Detail must state what was checked (%q missing): %s", want, st.Detail)
		}
	}
}

// TestResolveGroundStateLinkedWorktreeIsAliveWhileRegistered is case (b): `.git`
// is a FILE naming a gitdir under the main checkout, and the main checkout's own
// `git worktree list` names the directory.
func TestResolveGroundStateLinkedWorktreeIsAliveWhileRegistered(t *testing.T) {
	main := groundTestRepo(t)
	wt := filepath.Join(filepath.Dir(main), "wt-linked")
	gitIn(t, main, "worktree", "add", "-q", wt, "-b", "feat/ground-linked")

	info, err := os.Stat(filepath.Join(wt, ".git"))
	if err != nil || info.IsDir() {
		t.Fatalf("the fixture must be the LINKED shape (.git a file): err=%v isDir=%v", err, err == nil && info.IsDir())
	}
	st := ResolveGroundState(wt)
	if !st.Alive {
		t.Fatalf("a registered linked worktree must be alive; got %+v", st)
	}
	if st.Model != GroundModelLinkedWorktree {
		t.Errorf("model = %q, want %q", st.Model, GroundModelLinkedWorktree)
	}
	if resolveSame(t, st.Repo, main) == false {
		t.Errorf("repo = %q, want the main checkout %q", st.Repo, main)
	}
	if st.Branch != "feat/ground-linked" {
		t.Errorf("branch = %q, want feat/ground-linked", st.Branch)
	}
	for _, want := range []string{".git is a FILE", "worktree list` registers", "LINKED-worktree model"} {
		if !strings.Contains(st.Detail, want) {
			t.Errorf("the Detail must state what was checked (%q missing): %s", want, st.Detail)
		}
	}
}

// TestResolveGroundStateMissingWhenNoGitEntry is case (c): the directory carries
// no `.git` entry at all, so no model applies and nothing is claimed about git.
func TestResolveGroundStateMissingWhenNoGitEntry(t *testing.T) {
	dir := t.TempDir()
	st := ResolveGroundState(dir)
	if st.Alive {
		t.Fatalf("a directory with no .git entry is not a ground; got %+v", st)
	}
	if st.Model != GroundModelMissing {
		t.Errorf("model = %q, want %q", st.Model, GroundModelMissing)
	}
	if st.Repo != "" || st.Branch != "" {
		t.Errorf("a missing ground names neither repo nor branch: %+v", st)
	}
	for _, want := range []string{"no .git entry", "CLONE has a .git DIRECTORY", "LINKED worktree has a .git FILE"} {
		if !strings.Contains(st.Detail, want) {
			t.Errorf("the Detail must name the entry it looked for and both models (%q missing): %s", want, st.Detail)
		}
	}
	// "" is a question nobody asked: an explicit answer, never a crash.
	if empty := ResolveGroundState("  "); empty.Alive || empty.Model != GroundModelMissing {
		t.Errorf("an empty directory name must answer missing, got %+v", empty)
	}
}

// TestResolveGroundStateBrokenLinkWhenRegistrationRemoved is case (d): the
// directory is a linked worktree whose gitdir was deleted out from under it —
// `.git` is still a FILE, but the repository no longer knows the directory. This
// is the shape a swept registration leaves, and it must NOT read as alive while
// a clone must (the pair that the old readers got backwards).
func TestResolveGroundStateBrokenLinkWhenRegistrationRemoved(t *testing.T) {
	main := groundTestRepo(t)
	wt := filepath.Join(filepath.Dir(main), "wt-broken")
	gitIn(t, main, "worktree", "add", "-q", wt, "-b", "feat/ground-broken")

	// Remove the registration's OWN gitdir, leaving the directory and its `.git`
	// file behind — exactly what a hand-deleted `<main>/.git/worktrees/<name>`
	// leaves, and the state in which a linked-only reader says "gone" for the
	// right reason while a path-existence reader still says "verified live".
	body, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdir := groundGitdirTarget(string(body), wt)
	if gitdir == "" {
		t.Fatalf("the fixture's .git file names no gitdir: %s", body)
	}
	if err := os.RemoveAll(gitdir); err != nil {
		t.Fatalf("remove the worktree gitdir: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the DIRECTORY must survive the registration (that is the shape): %v", err)
	}

	st := ResolveGroundState(wt)
	if st.Alive {
		t.Fatalf("a linked worktree whose gitdir is gone is not alive; got %+v", st)
	}
	if st.Model != GroundModelBrokenLink {
		t.Errorf("model = %q, want %q", st.Model, GroundModelBrokenLink)
	}
	for _, want := range []string{".git is a FILE", "which does not exist", "registration was removed"} {
		if !strings.Contains(st.Detail, want) {
			t.Errorf("the Detail must say the gitdir is gone and what that means (%q missing): %s", want, st.Detail)
		}
	}
}

// TestResolveGroundStateLinkedWorktreeDroppedFromListing is the OTHER broken
// shape: the gitdir survives but the repository no longer lists the directory.
// It is a separate sentence from "the gitdir is gone" because the repair is
// different (a plain `git worktree prune`, not a re-clone).
func TestResolveGroundStateLinkedWorktreeDroppedFromListing(t *testing.T) {
	main := groundTestRepo(t)
	wt := filepath.Join(filepath.Dir(main), "wt-unlisted")
	gitIn(t, main, "worktree", "add", "-q", wt, "-b", "feat/ground-unlisted")
	// Take the directory OUT of the listing without touching its gitdir: the
	// repository's own registration file is what `worktree list` reads.
	reg := filepath.Join(main, ".git", "worktrees")
	entries, err := os.ReadDir(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(reg, e.Name(), "gitdir")); err != nil {
			t.Fatalf("drop the registration of %s: %v", e.Name(), err)
		}
	}
	st := ResolveGroundState(wt)
	if st.Alive {
		t.Fatalf("a directory the repository no longer lists is not alive; got %+v", st)
	}
	if st.Model != GroundModelBrokenLink {
		t.Errorf("model = %q, want %q", st.Model, GroundModelBrokenLink)
	}
	if !strings.Contains(st.Detail, "does not name") {
		t.Errorf("the Detail must say the listing was read and did not name the directory: %s", st.Detail)
	}
}

// resolveSame reports whether two paths name the same directory, through the
// resolver's own comparison so the test cannot pass on a spelling the code
// would reject.
func resolveSame(t *testing.T, a, b string) bool {
	t.Helper()
	return sameDirGround(a, b)
}
