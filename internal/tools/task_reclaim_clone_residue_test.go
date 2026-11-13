package tools

// THE CLONE GROUND'S CLOSE-OUT RESIDUE — the regression pin for epic 01a17b9d.
//
// MEASURED BEFORE THE FIX (this host, 2026-10-27): six clone-channel grounds
// holding 104 uncommitted entries, every one of them answering
//
//	ReclaimRunResidueCheck(row) → Attributed=false Porcelain="" Note=""
//
// because the only attribution predicate admitted a LINKED worktree
// (`git worktree list` of the row's repo, measured linked=0 here), and a clone
// channel ground (`.git` is a DIRECTORY) is registered nowhere. The result: no
// measurement, no ledger event, and GOPI_TASK_RUN_RESIDUE_ENFORCE=1 with nothing
// to refuse — while the row wrote back `completed`.
//
// WHAT THESE TESTS PIN, one per acceptance arm:
//
//	① a DIRTY clone ground IS attributed, measured, and its tracked shape is
//	   REFUSED at the writeback (the row stays open, the event names the file);
//	② a CLEAN clone ground is attributed, measures empty, and files NO event;
//	③ the LINKED shape is unchanged — the behaviour the arm had before this
//	   change still holds (dirty linked worktree measured + refused);
//	④ attribution stays EXCLUSIVE: a clone-named directory OUTSIDE the
//	   configured workspace root, and a clone ground standing on a DIFFERENT
//	   branch, are both "" — never attributed, never refused.
//
// Nothing here weakens the removal guards (reclaimSkipDirtyReason,
// finishCleanupWorktreeDirtyReason): this file never asks the REMOVAL question,
// only the MEASUREMENT one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// reclaimCloneResidueFixture builds the incident's EXACT shape: a row whose
// recorded repo is `repo` and whose recorded worktree is a REAL `git clone` of
// it under the configured workspace root, named by the row's own id and standing
// on the row's recorded branch. It returns repo, the clone ground and the row id.
//
// The workspace root is pinned through `config.WorkspaceRootEnvName` — the ONE
// reader's own channel (Rule 2), never a second spelling.
func reclaimCloneResidueFixture(t *testing.T, taskID string) (repo, ground, branch string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv(config.WorkspaceRootEnvName, base)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))

	repo = filepath.Join(base, "repo")
	branch = "master"
	gitRun(t, base, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}
	gitRun(t, repo, "add", "work.txt")
	gitRun(t, repo, "commit", "-m", "init")

	ground = filepath.Join(base, taskID) // the clone channel's own naming
	gitRun(t, base, "clone", repo, ground)
	return repo, ground, branch
}

// seedCloneResidueRow files the row and stamps its deliverable to the fixture.
func seedCloneResidueRow(t *testing.T, taskID, repo, branch, ground string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "clone residue " + taskID, Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, branch, ground)
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
}

// TestReclaimRunResidueCheckMeasuresADirtyCloneGround is arm ①: the measured
// defect itself. A clone ground with ONE tracked edit must come back attributed,
// with a non-empty measurement and a note — and its writeback must be REFUSED.
func TestReclaimRunResidueCheckMeasuresADirtyCloneGround(t *testing.T) {
	const id = "01a17b9d-0000-7000-8000-0000clone0001"
	repo, ground, branch := reclaimCloneResidueFixture(t, id)
	seedCloneResidueRow(t, id, repo, branch, ground)

	if err := os.WriteFile(filepath.Join(ground, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := ReclaimRunResidueCheck(id)
	if err != nil {
		t.Fatalf("ReclaimRunResidueCheck: %v", err)
	}
	if !out.Attributed {
		t.Fatalf("a CLONE ground holding %q of uncommitted work must be ATTRIBUTED to the row — "+
			"measured Attributed=false (the zero outcome that hid six grounds holding 104 entries)",
			gitStatusPorcelainFixture(t, ground))
	}
	if strings.TrimSpace(out.Porcelain) == "" {
		t.Fatalf("the clone ground's dirt must be MEASURED (Porcelain is empty)")
	}
	if out.Note == "" {
		t.Fatalf("an attributed dirty clone ground must carry its sentence")
	}
	if !strings.Contains(out.Note, "work.txt") {
		t.Errorf("the note must quote the offending entry, got %q", out.Note)
	}

	// The BINDING gate rides the same measurement: the claim is REFUSED and the
	// row stays OPEN.
	if _, err := TaskRunWriteback(id, TaskStatusCompleted, "done"); err == nil {
		t.Fatalf("a clone ground holding UNCOMMITTED TRACKED WORK must refuse the %s writeback", TaskStatusCompleted)
	}
	if got := taskRowStatus(t, id); got == TaskStatusCompleted {
		t.Fatalf("the row must stay OPEN after the refusal, measured status %q", got)
	}
	if detail := taskEventDetailFor(t, id, reclaimRunTrackedDirtRefusalEvent); detail == "" {
		t.Fatalf("the REFUSAL must be a ledger fact (event %q) on the row's trail", reclaimRunTrackedDirtRefusalEvent)
	}
	if detail := taskEventDetailFor(t, id, reclaimRunResidueDirtyEvent); detail == "" {
		t.Fatalf("the SHAPE must be RECORDED (event %q) as well as refused", reclaimRunResidueDirtyEvent)
	}
}

// TestReclaimRunResidueCheckIsSilentForACleanCloneGround is arm ②: a clean clone
// ground is attributed (it IS the row's ground) but measures NOTHING and files
// NO event — the arm must not manufacture a record for a correct close-out.
func TestReclaimRunResidueCheckIsSilentForACleanCloneGround(t *testing.T) {
	const id = "01a17b9d-0000-7000-8000-0000clone0002"
	repo, ground, branch := reclaimCloneResidueFixture(t, id)
	seedCloneResidueRow(t, id, repo, branch, ground)

	out, err := ReclaimRunResidueCheck(id)
	if err != nil {
		t.Fatalf("ReclaimRunResidueCheck: %v", err)
	}
	if strings.TrimSpace(out.Porcelain) != "" {
		t.Fatalf("a CLEAN clone ground must measure empty, got %q", out.Porcelain)
	}
	if out.Note != "" {
		t.Fatalf("a clean close-out must say nothing, got %q", out.Note)
	}
	if _, err := TaskRunWriteback(id, TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("a clean clone ground must be allowed to write back: %v", err)
	}
	for _, ev := range []string{reclaimRunResidueDirtyEvent, reclaimRunResidueEvent, reclaimRunTrackedDirtRefusalEvent} {
		if detail := taskEventDetailFor(t, id, ev); detail != "" {
			t.Fatalf("a clean clone ground must file NO %q event, got %q", ev, detail)
		}
	}
}

// TestReclaimRunResidueCheckKeepsTheLinkedShape is arm ③: the LINKED worktree
// behaviour the arm had before this change is untouched — the same fixture the
// pre-existing tracked-work test uses still measures and refuses.
func TestReclaimRunResidueCheckKeepsTheLinkedShape(t *testing.T) {
	_, wt, branch := dirtyCloseoutFixture(t, "t-linked-unchanged")
	_ = branch
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ReclaimRunResidueCheck("t-linked-unchanged")
	if err != nil {
		t.Fatalf("ReclaimRunResidueCheck: %v", err)
	}
	if !out.Attributed || strings.TrimSpace(out.Porcelain) == "" {
		t.Fatalf("a LINKED worktree holding uncommitted work must stay attributed and measured "+
			"(Attributed=%v Porcelain=%q)", out.Attributed, out.Porcelain)
	}
	if _, err := TaskRunWriteback("t-linked-unchanged", TaskStatusCompleted, "done"); err == nil {
		t.Fatalf("the linked shape's tracked-work refusal must be unchanged")
	}
}

// TestReclaimRunCloneGroundAttributionStaysExclusive is arm ④ — the epic's hard
// constraint, at its smallest sample: each rung of the five-fact conjunction,
// removed one at a time, must send the answer to "".
func TestReclaimRunCloneGroundAttributionStaysExclusive(t *testing.T) {
	const id = "01a17b9d-0000-7000-8000-0000clone0003"
	repo, ground, branch := reclaimCloneResidueFixture(t, id)

	if got := reclaimRunCloneGroundWorktree(id, repo, branch, ground); got == "" {
		t.Fatalf("precondition: the fixture must be admitted (%s)", ground)
	}
	// ⑤ NOT-THE-REPO: the shared checkout is never attributable, even when a row
	// names it as its worktree.
	if got := reclaimRunCloneGroundWorktree(id, repo, branch, repo); got != "" {
		t.Fatalf("the row's own REPO must never be attributed as a run ground, got %q", got)
	}
	// ② ORIGIN: the same clone-shaped directory, this time the workspace root is
	// somewhere else — nothing outside the root may be attributed.
	t.Setenv(config.WorkspaceRootEnvName, filepath.Join(t.TempDir(), "elsewhere"))
	if got := reclaimRunCloneGroundWorktree(id, repo, branch, ground); got != "" {
		t.Fatalf("a directory outside the configured workspace root must not be attributed, got %q", got)
	}
	// restore the root, then break ④ BRANCH (a ground standing elsewhere is
	// somebody's other checkout).
	t.Setenv(config.WorkspaceRootEnvName, filepath.Dir(ground))
	if got := reclaimRunCloneGroundWorktree(id, repo, "feat/somebody-else", ground); got != "" {
		t.Fatalf("a clone ground on ANOTHER branch must not be attributed, got %q", got)
	}
	// ③ IDENTITY: a clone of the same repo named by a DIFFERENT row.
	if got := reclaimRunCloneGroundWorktree("01a17b9d-0000-7000-8000-0000other0004", repo, branch, ground); got != "" {
		t.Fatalf("a ground named for another row's id must not be attributed, got %q", got)
	}
	// ① SHAPE / ② live: a directory that is not a work tree at all.
	plain := filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := reclaimRunCloneGroundWorktree(id, repo, branch, plain); got != "" {
		t.Fatalf("a plain directory must not be attributed, got %q", got)
	}
}

// TestReclaimRunCloneGroundOnAnotherBranchIsMeasuredNotAttributed pins the SECOND
// leg (reclaimRunCloneGroundUnattributable): the row's own clone ground standing
// on some other branch cannot be ATTRIBUTED, but it is still this run's directory
// and must be MEASURED — with today's asymmetry preserved (untracked-only blocks,
// a tracked change never does).
func TestReclaimRunCloneGroundOnAnotherBranchIsMeasuredNotAttributed(t *testing.T) {
	const id = "01a17b9d-0000-7000-8000-0000clone0005"
	repo, ground, branch := reclaimCloneResidueFixture(t, id)
	seedCloneResidueRow(t, id, repo, branch, ground)
	gitRun(t, ground, "checkout", "-b", "feat/moved-on")

	// Untracked-only residue in a ground that is not measurable-as-the-row's.
	if err := os.WriteFile(filepath.Join(ground, "scratch.txt"), []byte("litter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := ReclaimRunResidueCheck(id)
	if err != nil {
		t.Fatalf("ReclaimRunResidueCheck: %v", err)
	}
	if out.Attributed {
		t.Fatalf("a ground standing on ANOTHER branch must not be ATTRIBUTED")
	}
	if strings.TrimSpace(out.Porcelain) == "" {
		t.Fatalf("the un-attributable clone ground must still be MEASURED (Porcelain empty)")
	}
	if !out.Blocked || out.Note == "" {
		t.Fatalf("untracked-only residue in it must be BLOCKED and named (Blocked=%v Note=%q)", out.Blocked, out.Note)
	}
	// The BLOCKED shape is RECORDED, never refused: the operator's switch refuses
	// only the row's OWN (attributed) untracked residue — unchanged by this epic.
	if _, err := TaskRunWriteback(id, TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("a blocked-but-unattributed ground must not refuse the claim: %v", err)
	}
	if detail := taskEventDetailFor(t, id, reclaimRunResidueBlockedEvent); detail == "" {
		t.Fatalf("the blocked shape must be a ledger fact (event %q)", reclaimRunResidueBlockedEvent)
	}

	// The TRACKED shape in the same ground: measured, NEVER blocked (the epic's
	// uncommitted-work prohibition).
	const id2 = "01a17b9d-0000-7000-8000-0000clone0006"
	repo2, ground2, branch2 := reclaimCloneResidueFixture(t, id2)
	seedCloneResidueRow(t, id2, repo2, branch2, ground2)
	gitRun(t, ground2, "checkout", "-b", "feat/moved-on-too")
	if err := os.WriteFile(filepath.Join(ground2, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out2, err := ReclaimRunResidueCheck(id2)
	if err != nil {
		t.Fatalf("ReclaimRunResidueCheck: %v", err)
	}
	if out2.Attributed || out2.Blocked {
		t.Fatalf("a TRACKED change in an un-attributable ground is measured and never blocked "+
			"(Attributed=%v Blocked=%v)", out2.Attributed, out2.Blocked)
	}
	if strings.TrimSpace(out2.Porcelain) == "" {
		t.Fatalf("it must still be measured, got Porcelain=%q", out2.Porcelain)
	}
}
