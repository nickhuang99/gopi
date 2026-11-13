package tools

// task_repo_repair_inplace_test.go — THE ROW-SIDE HALF OF THE DEAD-WORKSPACE
// REPAIR (epic 01a17980).
//
// The measured defect: nineteen failed rows were killed at DISPATCH — before any
// run — because their deliverable.repo named a clone-channel workspace that had
// been reclaimed. The channel's refusal is correct, but it arrives at the LAST
// moment, so the row enters `failed` still carrying the dead path, and
// The retired reopen verb then INHERITED that path into the successor, so the
// rework could not run either.
//
// The contract, at the two seams that act on a row's repo:
//
//	① CLAIM/LAUNCH — TaskRepairDeadRepoInPlace repairs the row's deliverable.repo
//	   in place, through the ONE blob writer, and returns ONE line saying what
//	   happened (Rule 15); a live repo is identity, silent, and spawns no git
//	   process (so the seam can run on every dispatch);
//	② SUCCESSOR — the inherited repo is repaired BEFORE it is stamped, so no
//	   `gopi task clone` can be born naming a dead directory.
//
// The fixtures are the durable-repair suite's own (repairFixture / repairSeedRow:
// a real `git init` source tree, a workspace root pointed at through the ONE
// reader, and a row seeded through the PRODUCTION submit). Nothing here invents
// a second way to build a ledger or a repository (Rule 32/34).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/worktreeroots"
)

// successorSeedRow submits a row, drives it to a terminal verdict, and writes the
// given deliverable blob onto it (the shape a real failed/completed row has:
// repo always, worktree+branch only for a row that ran). clearDeliverable writes
// an EMPTY blob — the shape an auto-registered row born outside a work tree has
// (and the only way to build it: the submit engine stamps the cwd's repo by
// default).
func successorSeedRow(t *testing.T, id, deliverable string, clearDeliverable bool) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "seed row for the successor repo axis", TaskID: id,
		Payload: "acceptance: the successor row runs", Tags: "bug", Source: "llm"}); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
	if _, err := TaskUpdate(id, TaskStatusInProgress); err != nil {
		t.Fatalf("progress %s: %v", id, err)
	}
	if _, err := TaskUpdateWithResult(id, TaskStatusFailed, "the run died on the ground"); err != nil {
		t.Fatalf("fail %s: %v", id, err)
	}
	if deliverable == "" && !clearDeliverable {
		return
	}
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer closeDB()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, deliverable, id); err != nil {
		t.Fatalf("stamp deliverable on %s: %v", id, err)
	}
}

// successorRepoFixture creates a real (non-bare) git work tree and returns its
// path — the assertion is about the DERIVATION (a git tree answers its own top
// level) and not about which directory the test binary happened to run in.
func successorRepoFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(testutil.TestDeviceScratchDir(t, "successorrepo"), name)
	if err := exec.Command("git", "init", "-q", dir).Run(); err != nil {
		t.Fatalf("git init %s: %v", dir, err)
	}
	return dir
}

// inPlaceDeadWorkspaceRow seeds a row whose deliverable.repo points at a
// RECLAIMED workspace of ANOTHER task, exactly the shape the nineteen carry: the
// parent row recorded that workspace as its own ground, and the child inherited
// the path as its repo. Returns the dead path and the live source repository.
func inPlaceDeadWorkspaceRow(t *testing.T, taskID, parentID string) (dead, source string) {
	t.Helper()
	source, root := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	// The PARENT is the row that recorded the workspace as its ground — the one
	// entry the recovery reads.
	dead = devworkspace.Dir(root, parentID)
	repairSeedRow(t, parentID, source, dead)
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatalf("fixture: the workspace must be gone (%v)", err)
	}
	// The CHILD names that dead workspace as its REPO (the defect).
	successorSeedRow(t, taskID, taskDeliverableRepoJSON(dead), false)
	return dead, source
}

// TestRepairDeadRepoInPlaceFixesTheRowAndSaysSo is ①: the row's blob is
// rewritten to the live repository and the ONE line names the row, why the old
// path could not stand, and the ledger write that replaced it.
func TestRepairDeadRepoInPlaceFixesTheRowAndSaysSo(t *testing.T) {
	const child, parent = "01a17980-0000-7000-8000-00000000child", "01a17980-0000-7000-8000-0000000parent"
	dead, source := inPlaceDeadWorkspaceRow(t, child, parent)

	// PRECONDITION: the row really carries the dead path (otherwise the
	// assertion below would pass for the wrong reason).
	if got := taskRowDeliverableRepo(child); !SameDir(got, dead) {
		t.Fatalf("fixture: the row must carry the dead workspace %q, carries %q", dead, got)
	}

	line, err := TaskRepairDeadRepoInPlace(child)
	if err != nil {
		t.Fatalf("repair in place: %v", err)
	}
	if line == "" {
		t.Fatalf("a repair that happened must be REPORTED (Rule 15); it returned nothing")
	}
	// THE LEDGER, read back — the repair is a WRITE, and a value the row did not
	// receive is not a repair.
	if got := taskRowDeliverableRepo(child); !SameDir(got, source) {
		t.Fatalf("after the repair the row must name the live repository %q, names %q", source, got)
	}
	for _, want := range []string{child, dead, source, parent, TaskRepairDeadWorkspaceSourceLabel} {
		if !strings.Contains(line, want) {
			t.Fatalf("the line must carry %q — the row, the dead path, the recovered repo and the row that recorded it; got: %q", want, line)
		}
	}

	// IDEMPOTENT: a second call sees a live repo and says nothing.
	if again, err := TaskRepairDeadRepoInPlace(child); err != nil || again != "" {
		t.Fatalf("a second call must be identity and silent (the seam runs on every dispatch); got (%q, %v)", again, err)
	}
}

// TestRepairDeadRepoInPlaceFollowsALiveWorkspaceTransitively is the MEASURED
// case (real reissue of row 01a15a17, 2026-10-27): the recovery answered with a
// workspace that was STILL ALIVE — another run's current clone — which
// RepairDurableRepoForSubmit correctly calls identity for a SUBMIT (its origin is
// the durable answer) and which a ROW may never carry, because the channel
// reclaims it the moment that run's push lands. THAT is how the nineteen rows
// were born, so the row-side resolver must keep following until the answer lies
// outside the workspace root.
func TestRepairDeadRepoInPlaceFollowsALiveWorkspaceTransitively(t *testing.T) {
	const child, parent = "01a17980-0000-7000-8000-0000000trans", "01a17980-0000-7000-8000-00000tparent"
	source, root := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	// The LIVE workspace: another run's current clone, with a readable origin.
	live := devworkspace.Dir(root, "01a17980-0000-7000-8000-000000000live")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	repairGit(t, root, "clone", "-q", source, live)
	// The parent row recorded a RECLAIMED workspace as its ground, and its own
	// repo is the LIVE one — so the child's dead path resolves to a path that is
	// itself under the root, which must be followed again.
	reclaimed := devworkspace.Dir(root, parent)
	repairSeedRow(t, parent, live, reclaimed)
	successorSeedRow(t, child, taskDeliverableRepoJSON(reclaimed), false)

	line, err := TaskRepairDeadRepoInPlace(child)
	if err != nil {
		t.Fatalf("repair in place: %v", err)
	}
	got := taskRowDeliverableRepo(child)
	if worktreeroots.Within(got, root) {
		t.Fatalf("the row must never carry a path under the workspace root %q (the channel reclaims it) — it names %q", root, got)
	}
	if !SameDir(got, source) {
		t.Fatalf("after following both hops the row must name the real repository %q, names %q", source, got)
	}
	for _, want := range []string{reclaimed, live, source} {
		if !strings.Contains(line, want) {
			t.Fatalf("the account must name every hop %q (a repair nobody can read is a silent one); got: %q", want, line)
		}
	}
	// Idempotent: the answer is outside the root, so a second call is silent.
	if again, err := TaskRepairDeadRepoInPlace(child); err != nil || again != "" {
		t.Fatalf("a second call must be silent; got (%q, %v)", again, err)
	}
}

// TestRepairDeadRepoInPlaceLeavesALiveRepoAlone: every normal row names a live
// work tree, and the seam must cost it nothing — no note, and NO GIT PROCESS
// (measured at the process boundary, so it cannot be satisfied by reading the
// source).
func TestRepairDeadRepoInPlaceLeavesALiveRepoAlone(t *testing.T) {
	source, _ := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	successorSeedRow(t, "01a17980-0000-7000-8000-0000000000live", taskDeliverableRepoJSON(source), false)
	calls := repairInstallGitCallRecorder(t)

	line, err := TaskRepairDeadRepoInPlace("01a17980-0000-7000-8000-0000000000live")
	if err != nil || line != "" {
		t.Fatalf("a live repo must be identity and silent; got (%q, %v)", line, err)
	}
	if n := calls(); n != 0 {
		t.Fatalf("a live repo must spawn no git process for the repair; it ran git %d time(s)", n)
	}
	if got := taskRowDeliverableRepo("01a17980-0000-7000-8000-0000000000live"); !SameDir(got, source) {
		t.Fatalf("the row must still name its own repository %q, names %q", source, got)
	}
}

// TestRepairDeadRepoInPlaceIsSilentWhenNothingCanBeRecovered is the honest MISS:
// a dead path NO row recorded comes back untouched and this seam says nothing —
// the path the row was given stays, and the clone channel refuses it naming the
// REAL reason. A seam that invented a note here would be reporting a repair it
// did not make.
func TestRepairDeadRepoInPlaceIsSilentWhenNothingCanBeRecovered(t *testing.T) {
	_, root := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	orphan := devworkspace.Dir(root, "01a17980-0000-7000-8000-0000000orphan")
	successorSeedRow(t, "01a17980-0000-7000-8000-000000000miss", taskDeliverableRepoJSON(orphan), false)

	line, err := TaskRepairDeadRepoInPlace("01a17980-0000-7000-8000-000000000miss")
	if err != nil || line != "" {
		t.Fatalf("an unrecoverable path must be left alone and this seam silent; got (%q, %v)", line, err)
	}
	if got := taskRowDeliverableRepo("01a17980-0000-7000-8000-000000000miss"); !SameDir(got, orphan) {
		t.Fatalf("the row keeps the path it was given; it now names %q", got)
	}
}

// TestCloneRepairsADeadInheritedRepo is ②: a row whose repo is a RECLAIMED
// workspace yields a successor naming the LIVE repository — the very shape that
// made every successor of the nineteen re-stamp the dead path.
func TestCloneRepairsADeadInheritedRepo(t *testing.T) {
	const child, parent = "01a17980-0000-7000-8000-0000000reopen", "01a17980-0000-7000-8000-0000000rparent"
	dead, source := inPlaceDeadWorkspaceRow(t, child, parent)

	out, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: child, Reason: "the row was killed at dispatch by a dead deliverable.repo"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	got := taskDeliverableParse(taskRowByID(t, cloneMintedSuccessorOf(t, child)).Deliverable).Repo
	if SameDir(got, dead) {
		t.Fatalf("the successor must NOT be born naming the dead workspace %q — that is the defect this seam closes", dead)
	}
	if !SameDir(got, source) {
		t.Fatalf("successor deliverable.repo = %q, want the live repository %q the judged row's own parent recorded", got, source)
	}
	// The repair RIDES the reissue line: an operator reading it learns that the
	// value they inherited was replaced, and why (Rule 15).
	for _, want := range []string{TaskSuccessorRepoCarriedMarker, source, TaskRepairDeadWorkspaceSourceLabel, parent} {
		if !strings.Contains(out, want) {
			t.Fatalf("the mint's line must state the repair with %q; got:\n%s", want, out)
		}
	}
	// The source row is untouched: a successor mint never rewrites the verdict
	// it copies from. Only the SUCCESSOR is repaired (the source row keeps its
	// failure text, which is the audit record of what happened).
	if old := taskRowDeliverableRepo(child); !SameDir(old, dead) {
		t.Fatalf("the source row's own repo must not be rewritten by a clone; it now names %q", old)
	}
}

// TestCloneLeavesALiveInheritedRepoAlone is ②'s other half: an ordinary row
// (a live repo) yields a successor carrying the same value and no repair
// sentence. The seam must not annotate a value it did not change.
func TestCloneLeavesALiveInheritedRepoAlone(t *testing.T) {
	newVerifyTestDB(t)
	repo := successorRepoFixture(t, "live-inherit")
	successorSeedRow(t, "t-live", taskDeliverableRepoJSON(repo), false)

	out, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: "t-live", Reason: "the run never started"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if got := taskDeliverableParse(taskRowByID(t, cloneMintedSuccessorOf(t, "t-live")).Deliverable).Repo; got != repo {
		t.Fatalf("successor deliverable.repo = %q, want the inherited live repo %q", got, repo)
	}
	if strings.Contains(out, TaskRepairDeadWorkspaceSourceLabel) {
		t.Fatalf("a live inherited repo must not be reported as repaired; got:\n%s", out)
	}
}

// TestRepairInPlaceUsesTheOneWorkspaceRootReader pins the boundary the repair
// inherits (Rule 1/13/14): the workspace root comes from the ONE reader, so a
// path under it is repaired and a path that is not under it never is — even when
// a ledger row records that path as a worktree.
func TestRepairInPlaceUsesTheOneWorkspaceRootReader(t *testing.T) {
	source, root := repairFixture(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	// A row recording a directory that is GONE and is NOT under the root: the
	// operator's own removed checkout. The repair must not touch it — its
	// boundary is the channel's own checkouts.
	outside := t.TempDir() + "/gone-checkout"
	repairSeedRow(t, "01a17980-0000-7000-8000-00000outside", source, outside)
	successorSeedRow(t, "01a17980-0000-7000-8000-000000000out", taskDeliverableRepoJSON(outside), false)

	if gotRoot, _, err := config.WorkspaceRootConfigured(); err != nil || !SameDir(gotRoot, root) {
		t.Fatalf("fixture: the ONE workspace-root reader must answer %q (got %q, %v)", root, gotRoot, err)
	}
	line, err := TaskRepairDeadRepoInPlace("01a17980-0000-7000-8000-000000000out")
	if err != nil || line != "" {
		t.Fatalf("a dead path OUTSIDE the workspace root is not this repair's business; got (%q, %v)", line, err)
	}
	if got := taskRowDeliverableRepo("01a17980-0000-7000-8000-000000000out"); !SameDir(got, outside) {
		t.Fatalf("the row outside the root keeps its path; it now names %q", got)
	}
}
