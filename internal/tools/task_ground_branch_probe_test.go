package tools

// task_ground_branch_probe_test.go — the branch axis of the advisory ground
// line, asked BY THE GROUND'S SHAPE (Rule 30's sibling: the reading must be
// taken where the value lives).
//
// THE DEFECT (measured 2026-10-28, live row 01a17df2-1d64-7624-b08e-f6ba187631b2):
// the line asked the row's RECORDED REPO for the branch, which on a
// clone-channel run is the shared main checkout — and a clone's refs exist
// nowhere else. The live commands, run against that row's real directories:
//
//	$ git -C /home/user/work/gopi rev-parse --verify --quiet \
//	      refs/heads/task/01a17df21d647624b08ef6ba187631b2 ; echo $?
//	1
//	$ git -C /home/user/workspace/01a17df2-1d64-7624-b08e-f6ba187631b2 \
//	      rev-parse refs/heads/task/01a17df21d647624b08ef6ba187631b2
//	3af81a93271e80356c99b95263a125a808a27e67
//	$ ls -ld /home/user/workspace/01a17df2-…/.git
//	drwxrwxr-x 8 nick nick 4096 Oct 28 01:47 …/.git     # a DIRECTORY ⇒ CLONE
//
// and the ledger said NOT FOUND. So a delivered-but-stuck run read exactly like
// a run that did nothing.
//
// WHAT THESE TESTS PIN:
//
//	(a) a branch that exists ONLY in the clone ground renders `(exists)` and the
//	    why NAMES the clone — the fixed behaviour;
//	(b) a branch that exists in NEITHER place still renders NOT FOUND, and the
//	    why names BOTH directories asked (so a reader can tell "asked two trees"
//	    from the pre-fix "asked the wrong one");
//	(c) a LINKED WORKTREE ground still reads through the shared repo (the other
//	    shape must not regress), and a branch deleted there still reads NOT
//	    FOUND.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// clonedGroundFixture is a main checkout on master plus a real CLONE of it — the
// clone-channel ground shape (`.git` is a DIRECTORY). The two directories are
// separate repositories with separate ref stores, which is the whole point: a
// branch created in one is invisible in the other.
type clonedGroundFixture struct {
	t      *testing.T
	repo   string // the shared main checkout (what deliverable.repo records)
	clone  string // the run's own ground (what deliverable.worktree records)
	branch string
	// session is the row's run session id (the fixture writes a session that
	// launches in the clone and records no ground-creating command — the row
	// itself records its ground, which is the branch this file's defect is
	// about).
	session string
}

// newClonedGroundFixture builds the two trees and asserts the SHAPE it built —
// a fixture that silently produced a linked worktree would make every arm below
// measure the wrong model.
func newClonedGroundFixture(t *testing.T, branch, sessionID string) *clonedGroundFixture {
	t.Helper()
	root := t.TempDir()
	f := &clonedGroundFixture{
		t:       t,
		repo:    filepath.Join(root, "gopi"),
		clone:   filepath.Join(root, "workspace", "01a17df2-1d64-7624-b08e-f6ba187631b2"),
		branch:  branch,
		session: sessionID,
	}
	deliverableWTGit(t, root, "init", f.repo)
	deliverableWTGit(t, f.repo, "config", "user.email", "t@t")
	deliverableWTGit(t, f.repo, "config", "user.name", "t")
	deliverableWTGit(t, f.repo, "checkout", "-b", masterBranchName)
	if err := os.WriteFile(filepath.Join(f.repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, f.repo, "add", "a.txt")
	deliverableWTGit(t, f.repo, "commit", "-m", "base")
	if err := os.MkdirAll(filepath.Dir(f.clone), 0o755); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, root, "clone", f.repo, f.clone)
	deliverableWTGit(t, f.clone, "config", "user.email", "t@t")
	deliverableWTGit(t, f.clone, "config", "user.name", "t")
	if got := devworkspace.GroundModelOf(f.clone); got != devworkspace.GroundModelClone {
		t.Fatalf("fixture precondition: %s must be a CLONE ground, GroundModelOf said %q", f.clone, got)
	}
	return f
}

// branchInCloneOnly creates the branch inside the clone's OWN ref store (a
// commit on it, so it is a branch with work — the delivered shape).
func (f *clonedGroundFixture) branchInCloneOnly(t *testing.T) {
	t.Helper()
	deliverableWTGit(t, f.clone, "checkout", "-b", f.branch)
	if err := os.WriteFile(filepath.Join(f.clone, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, f.clone, "add", "work.txt")
	deliverableWTGit(t, f.clone, "commit", "-m", "the run's work")
}

// deliverableBlob is what the row records: the shared repo, the run's branch and
// its clone ground.
func (f *clonedGroundFixture) deliverableBlob() string {
	return `{"repo":"` + f.repo + `","branch":"` + f.branch + `","worktree":"` + f.clone + `"}`
}

// TestTaskShowGroundBranchResolvesInTheCloneGround is case (a) — THE regression
// this file exists for. The branch is in the clone and in NO other place; the
// line must say it EXISTS and must name the clone as where it found it. Before
// the fix the same row rendered `(NOT FOUND now — a stale reading)`, because the
// probe asked only the recorded repo.
func TestTaskShowGroundBranchResolvesInTheCloneGround(t *testing.T) {
	testutil.NewTestDBPath(t)
	f := newClonedGroundFixture(t, "task/01a17df2clone", "sess-clone-ground")
	f.branchInCloneOnly(t)

	// The measured asymmetry, asserted directly: the clone has the ref, the
	// recorded repo does not. If this ever stops holding the fixture — not the
	// fix — has drifted.
	if !taskBranchExists(f.clone, f.branch) {
		t.Fatalf("fixture precondition: the clone ground must hold refs/heads/%s", f.branch)
	}
	if taskBranchExists(f.repo, f.branch) {
		t.Fatalf("fixture precondition: the recorded repo must NOT hold refs/heads/%s (that asymmetry IS the defect)", f.branch)
	}

	advisorySeedRow(t, "t-clone-ground", f.deliverableBlob(), f.session)
	out, err := TaskShow("t-clone-ground")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := advisoryGroundLine(t, out)
	if strings.Contains(line, TaskShowGroundStaleMarker) {
		t.Fatalf("a branch that exists in the clone ground must NOT render the stale marker: %q", line)
	}
	if !strings.Contains(line, TaskShowGroundExistsMarker) {
		t.Fatalf("a branch that exists in the clone ground must render the exists marker: %q", line)
	}
	// WHERE it was found is part of the answer — the two trees are different
	// repositories and only one of them is the delivery point.
	if !strings.Contains(line, f.clone) {
		t.Errorf("the line must name the clone ground it found the branch in: %q", line)
	}
	// AND SO IS WHERE IT WAS NOT — the positive clause. Naming the clone alone
	// leaves a reader unable to tell a shape-decisive answer ("the clone has it,
	// the repo does not — two ref stores, one delivery point") from a probe that
	// happened to answer YES first. The clause is the ONE the two negative
	// answers in (b) complete, so it is pinned from the renderer's own constant
	// rather than a hand-typed sentence (Rules 13/14).
	want := taskGroundBranchRefIn(f.repo, f.clone, f.branch).whereNote()
	if want == "" {
		t.Fatalf("fixture precondition: a clone-shaped answer must carry a where-note, the probe said none for %s/%s", f.clone, f.repo)
	}
	if !strings.Contains(line, want) {
		t.Errorf("the line must carry the probe's own where-note %q (found here, NOT there): %q", want, line)
	}
	// The note's SECOND half is the recorded repo — the tree that answered NO
	// and is therefore the one a "no longer exists in the repo" defect would
	// have been reading.
	if !strings.Contains(want, f.repo) {
		t.Errorf("the where-note must name the recorded repo it did NOT find the branch in, got %q", want)
	}
	if strings.Contains(line, "no longer exists in the repo") {
		t.Errorf("the pre-fix sentence (a repo-only verdict) must never render beside a live clone branch: %q", line)
	}
}

// TestTaskShowGroundBranchNotFoundNamesEveryPlaceAsked is case (b): with the
// branch in NEITHER tree the verdict is still NOT FOUND — and the why names both
// directories asked, so the honest negative is distinguishable from the pre-fix
// "asked the wrong tree and called it stale".
func TestTaskShowGroundBranchNotFoundNamesEveryPlaceAsked(t *testing.T) {
	testutil.NewTestDBPath(t)
	f := newClonedGroundFixture(t, "task/01a17df2absent", "sess-clone-absent")
	// No branch created anywhere.

	advisorySeedRow(t, "t-clone-absent", f.deliverableBlob(), f.session)
	out, err := TaskShow("t-clone-absent")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := advisoryGroundLine(t, out)
	if !strings.Contains(line, TaskShowGroundStaleMarker) {
		t.Fatalf("a branch that exists nowhere must render the stale marker: %q", line)
	}
	if !strings.Contains(line, f.clone) {
		t.Errorf("the NOT FOUND why must name the CLONE it asked: %q", line)
	}
	if !strings.Contains(line, f.repo) {
		t.Errorf("the NOT FOUND why must name the RECORDED REPO it asked: %q", line)
	}
	// The evidence clause is the probe's own rendering, not this test's.
	want := taskGroundBranchRefSiteEvidence(f)
	if !strings.Contains(line, want) {
		t.Errorf("the NOT FOUND why must carry the asked-places evidence %q: %q", want, line)
	}
}

// taskGroundBranchRefSiteEvidence is the asked-places clause the probe renders
// for this fixture — built from the SAME function the renderer uses, so the test
// cannot drift into pinning a hand-typed sentence (Rules 13/14).
func taskGroundBranchRefSiteEvidence(f *clonedGroundFixture) string {
	return taskGroundBranchRefIn(f.repo, f.clone, f.branch).siteText()
}

// TestTaskShowGroundBranchLinkedWorktreeStaysInTheSharedRepo is case (c): the
// OTHER shape must not regress. A linked worktree shares the shared repo's ref
// store, so the branch is found through the repo and the line reads exactly as
// it did before this fix — no invented "asked the clone" clause, because there
// is no clone.
func TestTaskShowGroundBranchLinkedWorktreeStaysInTheSharedRepo(t *testing.T) {
	testutil.NewTestDBPath(t)
	f := newAdvisoryShowFixture(t, "feat/advisory-linked-shape", "sess-advisory-linked-shape")
	deliverable := `{"repo":"` + f.repo + `","branch":"` + f.branch + `","worktree":"` + f.wt + `"}`
	advisorySeedRow(t, "t-advisory-linked-shape", deliverable, f.session)

	if got := devworkspace.GroundModelOf(f.wt); got != devworkspace.GroundModelLinkedWorktree {
		t.Fatalf("fixture precondition: %s must be a LINKED WORKTREE, GroundModelOf said %q", f.wt, got)
	}
	out, err := TaskShow("t-advisory-linked-shape")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := advisoryGroundLine(t, out)
	if !strings.Contains(line, TaskShowGroundExistsMarker) || strings.Contains(line, TaskShowGroundStaleMarker) {
		t.Fatalf("a live linked worktree branch must render exists: %q", line)
	}
	// The shape decides the ORDER: for a linked worktree the shared repo is
	// asked first, so the branch resolves THERE and the "found in the clone"
	// clause (taskGroundWhyBranchWhereFmt) must not appear at all.
	ref := taskGroundBranchRefIn(f.repo, f.wt, f.branch)
	if !ref.exists || SamePath(ref.where) != SamePath(f.repo) {
		t.Fatalf("a linked worktree's branch must resolve in the shared repo, got where=%q", ref.where)
	}
	if note := ref.whereNote(); note != "" {
		t.Fatalf("a linked worktree reading needs no \"found elsewhere\" clause, got %q", note)
	}
}
