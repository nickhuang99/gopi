package tools

// task_run_ground_clone_anchor_test.go — the POPULATION GAP of a clone ground
// whose row names no repo (Rule 41 report: rows with `source=auto`, the
// auto-registration rows, carry no `deliverable.repo`).
//
// THE DEFECT, both halves, and they are two separate silences:
//
//	① THE PRODUCER. TaskRunGroundForClone returned EARLY when the row named no
//	   repo, and TaskGroundAnchorRecord — the ONLY writer of a run-ground anchor
//	   — sits after that early return. So a clone workspace opened for such a row
//	   (/home/nick/workspace/<task-id>, a full clone of its own) got NO anchor.
//	   The anchor is the sole input to the sweep's run-ground population
//	   (RunGroundCollectableGrounds ← RunGroundAnchors), so the directory was
//	   not "kept" — it was INVISIBLE: no reclaim arm could name it, and it stayed
//	   on disk forever (measured: /home/nick/workspace/002c43f2-d6dc-4c4b-82d9-
//	   f2b40163d53c, 233 MB, clean, on master, and absent from
//	   `gopi task sweep-worktrees --dry-run`'s output entirely).
//
//	② THE CONSUMER. Even WITH an anchor, RunGroundCollect's repo resolver
//	   skipped a ground whose anchor named no repo ("refusing to guess which
//	   repository the registration belongs to") — correct for a LINKED
//	   worktree, where the registration lives in a repository the anchor does
//	   not name, and WRONG for a CLONE ground, whose directory IS the
//	   repository. Those are the two cases pinned separately below, because the
//	   fix must move one and must not move the other.
//
// Every case drives the REAL surfaces: TaskRunGroundForClone writes, then
// RunGroundCollectableGrounds + RunGroundCollect read and act. Nothing is
// stubbed — the whole point is that the writer and the sweeper disagreed.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// cloneAnchorSeedRepoLessRow seeds a row that carries NO repo — the
// auto-registration shape (an empty deliverable blob, exactly what the harness
// writes for a bare-prompt run whose row IS the run).
func cloneAnchorSeedRepoLessRow(t *testing.T, taskID string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, 'auto', ?, ?)`,
		taskID, "a bare-prompt run whose row names no repo", "", nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed repo-less row %s: %v", taskID, err)
	}
}

// TestTaskRunGroundForCloneAnchorsARepoLessRow is half ① — the RED→GREEN core.
//
// Pre-fix this test failed on the AnchorRecord assertion: the early return on
// `repo == ""` ran BEFORE the only anchor writer, so the sink stayed empty and
// the directory the run had just taken was not in any reclaim population.
func TestTaskRunGroundForCloneAnchorsARepoLessRow(t *testing.T) {
	sink := anchorSink(t)
	const row = "01a17b5b-0000-7000-8000-0000000a0001"
	cloneAnchorSeedRepoLessRow(t, row)

	root := t.TempDir()
	t.Setenv(config.WorkspaceRootEnvName, root)
	main := groundTestRepo(t, "clone-anchor-main")
	if out, err := groundTestGit(root, "clone", "-q", main, filepath.Join(root, row)); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	ground := filepath.Join(root, row)
	// PRECONDITION: the directory is a CLONE ground (`.git` is a DIRECTORY) —
	// the shape that makes "the directory is its own repository" a measurement
	// rather than a guess, and the discriminator the sweep keys on.
	if !deliveryProofIsCloneGround(ground) {
		t.Fatalf("fixture: %s is not a clone ground (.git must be a DIRECTORY)", ground)
	}

	res, err := TaskRunGroundForClone(row, ground)
	if err != nil {
		t.Fatalf("TaskRunGroundForClone(%s): %v", row, err)
	}
	if !sameWorkingPath(res.Dir, ground) {
		t.Errorf("the resolution must name the workspace it was handed; got %q want %q", res.Dir, ground)
	}
	if res.Branch != masterBranchName {
		t.Errorf("the branch must be read from the GROUND itself (the row names none); got %q want %q",
			res.Branch, masterBranchName)
	}

	anchors := anchorsIn(t, sink)
	if len(anchors) != 1 {
		t.Fatalf("a repo-less clone ground must be anchored EXACTLY once (it is the only reclaim population this directory can enter); got %d anchors", len(anchors))
	}
	a := anchors[0]
	if !sameWorkingPath(a.Worktree, ground) {
		t.Errorf("the anchor names %q, want the ground %q", a.Worktree, ground)
	}
	if a.TaskID != row {
		t.Errorf("the anchor names row %q, want %q", a.TaskID, row)
	}
	if a.Branch != masterBranchName {
		t.Errorf("the anchor's branch = %q, want %q (read from the ground, not from the row)", a.Branch, masterBranchName)
	}
	// The anchor's repo is HONESTLY empty: the row names none, and the anchor
	// records what the row declared. The ground's own shape supplies the
	// repository at REMOVAL time (half ②), where the directory is measured — a
	// writer must not invent a repo the ledger never had.
	if a.Repo != "" {
		t.Errorf("the anchor must not invent a repo for a repo-less row; got %q", a.Repo)
	}
	// The identity is the TAKER's, read live — the field that makes the anchor
	// unmatchable by a recycled pid.
	if a.PID != strconv.Itoa(os.Getpid()) {
		t.Errorf("the anchor's pid = %q, want this process (%d)", a.PID, os.Getpid())
	}

	// THE STAMP IS STILL A NO-OP, and that is deliberate, not an omission: the
	// deliverable blob pairs a worktree WITH a repo (taskBackfillDeliverableWorktreeAt
	// returns early on an empty repo), so there is no pair to write. The row must
	// therefore still carry no worktree — a stamp nobody could honour would be
	// the false evidence Rule 15 refuses.
	blob := deliverableWTRowBlob(t, os.Getenv(config.MemoryDBEnv), row)
	if strings.Contains(blob, ground) {
		t.Errorf("the row's deliverable must keep NO worktree for a repo-less row (nothing to pair it with); got %q", blob)
	}
}

// anchoredRepoLess stages a FINISHED taker for a clone ground an anchor that
// names NO repo — the exact record a pre-fix run would have left, and the record
// the fix's producer now writes for a repo-less row.
func (f collectCloneFixture) anchoredRepoLess(t *testing.T) {
	t.Helper()
	dead := deadPidOnThisHost(t)
	writeAnchorRecordForTest(t, f.sink, "", f.ground, f.branch, "", strconv.Itoa(dead), "999999999", strconv.Itoa(dead))
	anchorAgeForTest(t, f.sink, f.ground, 48*time.Hour)
}

// TestRunGroundCollectFreesARepoLessCloneGround is half ② for the CLONE shape —
// and the case that says the audit record must name a repository truthfully.
//
// Pre-fix this ground was never removed: RunGroundCollect skipped it with
// "the anchor names no repo — refusing to guess which repository the
// registration belongs to", a sentence that is FALSE for this shape (the
// directory IS the repository, measured by deliveryProofIsCloneGround).
func TestRunGroundCollectFreesARepoLessCloneGround(t *testing.T) {
	const row = "01a17b5b-0000-7000-8000-0000000a0002"
	f := newCollectCloneFixture(t, row)
	f.anchoredRepoLess(t)

	_, removed, skipped := f.collect(t)
	if !containsPath(removed, f.ground) {
		t.Fatalf("a repo-less CLONE ground must be freed (its directory is its own repository); removed=%v skipped=%v",
			removed, skipped)
	}
	if _, err := os.Stat(f.ground); !os.IsNotExist(err) {
		t.Fatalf("the clone ground's directory must be GONE after the apply; stat err=%v", err)
	}
	recs := removalRecords(t, f.sink, f.ground)
	if len(recs) != 1 {
		t.Fatalf("the removal must be recorded exactly once; got %d records", len(recs))
	}
	// THE REPO FIELD IS EVIDENCE, not decoration: for a clone ground the
	// directory is the repository, so that is what the record must say. An audit
	// line naming "" would leave the next reader unable to answer "which
	// repository did this belong to?".
	if !sameWorkingPath(recs[0].Repo, f.ground) {
		t.Errorf("the removal record must name the ground's own directory as its repo; got %q want %q", recs[0].Repo, f.ground)
	}
}

// TestRunGroundCollectStillRefusesARepoLessLinkedWorktree is the case the fix
// must NOT move: a LINKED worktree (`.git` is a FILE) is a REGISTRATION of a
// repository the anchor does not name, so the directory cannot supply it — the
// registration's repository is somebody else's, and guessing it is exactly what
// the refusal exists to prevent.
//
// WHICH RUNG SPEAKS FIRST, measured while writing this case (and the reason the
// assertion below accepts two sentences): for a linked worktree the
// UNDELIVERED-COMMITS rung reads its refs from the repository, so with no repo it
// answers "the registration names no branch to measure (repo \"\", branch …)" —
// an unmeasurable question, which KEEPS the ground — before the apply leg ever
// reaches its own repo check. That ordering is pre-existing and untouched here;
// the clone leg reads its refs from the DIRECTORY (`deliveryProofIsCloneGround`
// → `readFrom = ground`), which is exactly why IT used to fall through to the
// repo check and be refused there. So this test pins the CONTRACT (a repo-less
// linked ground is never freed, and the sentence says which repository question
// could not be answered), not one particular rung's spelling.
func TestRunGroundCollectStillRefusesARepoLessLinkedWorktree(t *testing.T) {
	sink := anchorSink(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	root := t.TempDir()
	t.Setenv(config.WorkspaceRootEnvName, root)
	repo := groundTestRepo(t, "clone-anchor-linked-main")
	linked := filepath.Join(root, "gopi-repo-less-linked")
	if out, err := groundTestGit(repo, "worktree", "add", linked, "-b", "task/01a17b5b-repo-less"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	// PRECONDITION: this is the OTHER shape — `.git` is a FILE.
	if deliveryProofIsCloneGround(linked) {
		t.Fatalf("fixture: %s must be a LINKED worktree, not a clone ground", linked)
	}
	dead := deadPidOnThisHost(t)
	writeAnchorRecordForTest(t, sink, "", linked, "task/01a17b5b-repo-less", "", strconv.Itoa(dead), "999999999", strconv.Itoa(dead))
	anchorAgeForTest(t, sink, linked, 48*time.Hour)

	anchors := anchorsIn(t, sink)
	if len(anchors) != 1 {
		t.Fatalf("fixture must stage exactly one anchor; got %d", len(anchors))
	}
	removed, skipped, err := RunGroundCollect([]RunGroundLiveness{{Anchor: anchors[0]}})
	if err != nil {
		t.Fatalf("RunGroundCollect: %v", err)
	}
	if containsPath(removed, linked) {
		t.Fatalf("the leg FREED a repo-less LINKED worktree — its registration's repository is unknowable from the directory; removed=%v", removed)
	}
	var why string
	for _, s := range skipped {
		if strings.Contains(s, linked) {
			why = s
		}
	}
	if why == "" {
		t.Fatalf("the refusal must be REPORTED, not silent; skipped=%v", skipped)
	}
	// Either sentence is honest — but an EMPTY repository must be named in it, and
	// it must be one of the two settled spellings, never an improvised one.
	if !strings.Contains(why, runGroundAnchorNoRepoReason) && !strings.Contains(why, "repo \"\"") {
		t.Errorf("the skip must name the unknown repository (its own refusal or the unmeasurable probe's); got %q", why)
	}
	if _, err := os.Stat(linked); err != nil {
		t.Errorf("a refused ground's directory must survive: %v", err)
	}
	if recs := removalRecords(t, sink, linked); len(recs) != 0 {
		t.Errorf("a refusal must write NO removal record; got %d", len(recs))
	}
	// The BRANCH survives too — this arm never removes a branch.
	if out, err := groundTestGit(repo, "rev-parse", "--verify", "--quiet", "task/01a17b5b-repo-less"); err != nil {
		t.Errorf("the branch must survive the refusal: %v\n%s", err, out)
	}
}
