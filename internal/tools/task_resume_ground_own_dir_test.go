// Tests for TaskResumeGroundForOwnDir — "which ledger row names the directory
// THIS FACE was standing in?" (task_resume_ground.go).
//
// THE DEFECT THESE PIN (measured 2026-10-15, real binary, the operator's own
// capture): the worker-resume close-out re-parents its own CWD to the
// filesystem root on purpose (it must not hold a ground its next step may
// remove), and it THEN asked the generic resolver about its "current
// directory" — which by then was "/". git was asked about "/", answered "not a
// git repository", and every SUCCESSFUL run ended with a false pair of lines:
//
//	gopi worker --resume: close-out for /: resume: --worktree / is not a work
//	tree: git rev-parse --show-toplevel in /: exit status 128
//
// So the property under test is not "does it find the row" — it is "does it
// answer from the directory it was GIVEN, without asking the process where it
// is". The first case below makes that mechanical: it stands the test process
// at the root (the post-release state) and asserts the answer still names the
// directory passed in, while the pre-fix SHAPE — the same resolver asked about
// the root — is asserted to be exactly the failure it was.
package tools

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreerelease"
)

// TestResumeGroundForOwnDirAnswersFromTheArgumentNotTheProcessCwd is the
// regression for the false close-out line: the directory is an INPUT, and a
// process standing somewhere else (including at the filesystem root, where the
// real caller stands when it asks) cannot change the answer.
func TestResumeGroundForOwnDirAnswersFromTheArgumentNotTheProcessCwd(t *testing.T) {
	branch := "feat/resume-own-dir"
	repo, wt, taskID := resumeGroundFixture(t, branch)

	// The real caller's state: the CWD release has already run.
	t.Chdir(worktreereleaseRootForTest)

	got, err := TaskResumeGroundForOwnDir(wt)
	if err != nil {
		t.Fatalf("TaskResumeGroundForOwnDir(%s) with the process at %s: %v", wt, worktreereleaseRootForTest, err)
	}
	if got.TaskID != taskID {
		t.Errorf("TaskID = %q, want the row that names the directory %q", got.TaskID, taskID)
	}
	if !SameDir(got.Dir, wt) {
		t.Errorf("Dir = %q, want %q", got.Dir, wt)
	}
	if got.Branch != branch {
		t.Errorf("Branch = %q, want %q (the registration's own branch)", got.Branch, branch)
	}
	if !SameDir(got.Repo, repo) {
		t.Errorf("Repo = %q, want %q", got.Repo, repo)
	}

	// THE PRE-FIX SHAPE, asserted so the defect is not merely described: the
	// same resolver asked about the released root is the failure the caller
	// printed twice per run. Reading it here keeps the mechanism pinned — if
	// that refusal ever changed shape, this arm would say so instead of leaving
	// the regression's premise to drift.
	_, rootErr := TaskResumeGroundFor("", worktreereleaseRootForTest)
	if rootErr == nil || !strings.Contains(rootErr.Error(), resumeGroundNotAWorkTreeMarker) {
		t.Fatalf("the released root must be refused as a non-work-tree (the defect's premise), got: %v", rootErr)
	}
}

// resumeGroundNotAWorkTreeMarker is the ONE spelling of the refusal the
// pre-fix shape produced, shared by the arm above and the cmd/agent e2e
// regression that counts it in the merged capture (Rule 1b).
const resumeGroundNotAWorkTreeMarker = "is not a work tree"

// worktreereleaseRootForTest is the directory the close-out releases its CWD to.
// It is a REFERENCE, not a spelling (Rules 13/14): internal/worktreerelease owns
// the value and is a leaf package with zero project imports, so referencing it
// here costs nothing and makes this regression follow the release if it ever
// moves.
const worktreereleaseRootForTest = worktreerelease.ProcessRootPath

// TestResumeGroundForOwnDirReportsADirectoryNoRowNames covers the answer that
// must NOT be an error: a directory the ledger does not name at all (the main
// checkout, a foreign checkout, a ground whose row was deleted) is reported as
// a fact about the directory.
func TestResumeGroundForOwnDirReportsADirectoryNoRowNames(t *testing.T) {
	branch := "feat/resume-own-dir-unowned"
	repo, _, _ := resumeGroundFixture(t, branch)

	got, err := TaskResumeGroundForOwnDir(repo)
	if err != nil {
		t.Fatalf("a directory no row names is an ANSWER, not a failure: %v", err)
	}
	if got.TaskID != "" {
		t.Errorf("TaskID = %q, want empty — no row names %s", got.TaskID, repo)
	}
	if got.Detail == "" {
		t.Error("the answer must carry a re-derivable detail line, got an empty one")
	}
	if !strings.Contains(got.Detail, repo) {
		t.Errorf("the detail must name the directory it is about, got: %s", got.Detail)
	}
}

// TestResumeGroundForOwnDirRefusesAnUnnamedDirectory pins the empty input: a
// caller that could not name its directory must be REFUSED rather than answered
// about a directory nobody named (Rule 15 — guessing the main checkout here
// would report on ground this face never occupied).
func TestResumeGroundForOwnDirRefusesAnUnnamedDirectory(t *testing.T) {
	// The scaffold's writer, never a hand-rolled path (Rule 32): this arm reads
	// nothing, but the resolver opens the ledger on the way, and an unpinned
	// environment would point that read at the operator's own profile.
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	if _, err := TaskResumeGroundForOwnDir("   "); err == nil {
		t.Fatal("an unnamed directory must be refused, got a nil error")
	}
}
