package tools

// task_submit_durable_repo_test.go — the SUBMIT BOUNDARY that keeps a row
// runnable after its submitter's run is gone.
//
// THE DEFECT THIS PINS (measured 2026-10-07: 30 rows, 24 vanished paths). A row
// whose deliverable.repo names a LINKED WORKTREE — the run-scoped "ground" a
// worker creates for one run and deletes when it finishes — can never run.
// TaskRunGroundFor resolves the repo's MAIN checkout through git, and a directory
// that no longer exists cannot answer; the refusal is correct and the row is
// stranded forever:
//
//	cannot resolve the ground for task <id> in repo /home/user/workspace/gopi-<uuid>:
//	the repo is not a git work tree (or git cannot be read), so its main checkout
//	cannot be told from a task ground — refusing to start the run …
//
// The generator was a SUBMITTER that passed its own ground as an EXPLICIT repo.
// The DEFAULT arm has gone through DurableRepoForSubmit since epic 01a0d55b; the
// EXPLICIT arm stamped the caller's string verbatim, so `task_submit`'s own
// `repo` parameter and `gopi task submit --repo <my worktree>` minted rows born
// naming a directory their own parent was about to delete. Measured live on row
// 01a1103b: its submitter (01a11004) recorded ground
// /home/user/workspace/gopi-01a110043f0e… as the child's repo out of the session
// log, and finish-cleanup removed that ground 5 minutes later.
//
// WHAT EACH READING IS FOR, and why they are independent:
//
//	① the row's deliverable.repo is the repository's MAIN checkout — the path
//	   that outlives every run;
//	② it is NOT the linked worktree the caller passed (the defect's shape);
//	③ a main checkout passed explicitly is IDENTITY — a row that already names a
//	   durable path is not relocated (this is what keeps the existing explicit-repo
//	   suite meaningful, and it is asserted so a future "normalize harder" cannot
//	   quietly move those rows);
//	④ the row is RUNNABLE after the submitter's ground is deleted: the ground
//	   resolution CREATES a ground instead of refusing (the property the whole fix
//	   exists for).
//
// The fixture is a REAL repo (git init + commit) plus a REAL linked worktree of
// it; the ledger comes from the scaffold (Rule 32), never a hand-rolled path.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// durableRepoFixture builds a main checkout with one commit plus a LINKED
// worktree of it, and pins the ledger + worktree root. Returns both paths.
func durableRepoFixture(t *testing.T) (main, linked string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	t.Setenv(worktreeroots.WorktreeRootEnvName, t.TempDir())
	main = groundTestRepo(t, "durable-repo-main")
	linked = filepath.Join(t.TempDir(), "durable-linked")
	if out, err := groundTestGit(main, "worktree", "add", "-b", "feat/durable-linked", linked, "master"); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	// THE INSTRUMENT FIRST (Rule 10[4] — verify the smallest sample): the two
	// directories must be DIFFERENT, and the ONE resolution must already map the
	// linked worktree onto its main checkout. Without this the test could pass for
	// the wrong reason (e.g. both paths naming one directory).
	if SameDir(main, linked) {
		t.Fatalf("fixture degenerate: the linked worktree %s IS the main checkout", linked)
	}
	got, err := DurableRepoForSubmit(linked)
	if err != nil {
		t.Fatalf("DurableRepoForSubmit(%s): %v", linked, err)
	}
	if !SameDir(got, main) {
		t.Fatalf("the ONE durable resolution must map a linked worktree onto its main checkout: got %s, want %s", got, main)
	}
	return main, linked
}

// TestTaskSubmitLinkedWorktreeRepoIsNormalizedToTheMainCheckout is the regression
// guard for the ground-lost strand: an explicit repo that is a LINKED WORKTREE
// must land on the row as the DURABLE repository.
func TestTaskSubmitLinkedWorktreeRepoIsNormalizedToTheMainCheckout(t *testing.T) {
	main, linked := durableRepoFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "durable repo normalization", Payload: "do the work",
		Repo: linked, TaskID: "t-submit-durable-repo",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	got := taskRowDeliverableRepo("t-submit-durable-repo")
	if got == "" {
		t.Fatalf("the row carries no deliverable.repo at all — that stamp is the only handle the ground resolution has")
	}
	if !SameDir(got, main) {
		t.Errorf("the row's deliverable.repo must be the repository's MAIN checkout (%s), the path that outlives every run; got %s\n"+
			"a row born naming a linked worktree of its submitter is UNRUNNABLE the moment that worktree is reclaimed "+
			"(\"cannot resolve the ground\" — the measured 30-row strand)", main, got)
	}
	if SameDir(got, linked) {
		t.Errorf("the row's deliverable.repo is the linked worktree the caller passed (%s) — the defect verbatim", linked)
	}
}

// TestTaskSubmitMainCheckoutRepoIsUnchanged is the OTHER direction: a main
// checkout passed explicitly is IDENTITY, so the normalization cannot relocate a
// row that was already durable.
func TestTaskSubmitMainCheckoutRepoIsUnchanged(t *testing.T) {
	main, _ := durableRepoFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "main checkout identity", Payload: "do the work",
		Repo: main, TaskID: "t-submit-main-repo",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	got := taskRowDeliverableRepo("t-submit-main-repo")
	if !SameDir(got, main) {
		t.Errorf("a main checkout passed as the repo must be stamped UNCHANGED (%s); got %s", main, got)
	}
}

// TestTaskSubmitLinkedRepoRowIsStillRunnable is the PROPERTY the fix exists for,
// read through the surface that used to strand the row: after the submitter's
// ground is DELETED, the row still resolves a ground — created, under the
// worktree root, never the shared main checkout.
func TestTaskSubmitLinkedRepoRowIsStillRunnable(t *testing.T) {
	main, linked := durableRepoFixture(t)

	// A FULL uuid: this test asserts a ground is RESOLVED, and the ground name
	// is derived from the row's id — a non-uuid id is refused by design (see
	// task_ground_name_uuid_test.go), so the fixture must carry a real row id
	// shape or it would be testing the refusal instead of the resolution.
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "runnable after the submitter's ground is gone", Payload: "do the work",
		Repo: linked, TaskID: "01a11548-7cba-7170-91f5-430964f8e087",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	// The submitter's run ends and its ground is reclaimed — the event that
	// stranded 30 rows.
	if out, err := groundTestGit(main, "worktree", "remove", "--force", linked); err != nil {
		t.Fatalf("git worktree remove: %v\n%s", err, out)
	}
	if _, err := os.Stat(linked); err == nil {
		t.Fatalf("the linked worktree %s must be GONE for this test to mean anything", linked)
	}

	got, err := TaskRunGroundFor("01a11548-7cba-7170-91f5-430964f8e087")
	if err != nil {
		t.Fatalf("the row must still resolve a ground after its submitter's worktree was reclaimed; got the strand verbatim: %v", err)
	}
	if got.Dir == "" {
		t.Fatalf("a repo-aware row must resolve to a real ground directory; got %+v", got)
	}
	if SameDir(got.Dir, main) {
		t.Fatalf("the ground resolved to the MAIN checkout %s — a run must never work in the shared checkout", main)
	}
	if _, err := os.Stat(got.Dir); err != nil {
		t.Fatalf("the resolved ground %s must exist: %v", got.Dir, err)
	}
}
