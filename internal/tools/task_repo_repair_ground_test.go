package tools

// task_repo_repair_ground_test.go — THE ADVERTISED DOOR MUST BE PERFORMABLE
// (epic 01a131f5).
//
// THE DEFECT (measured 2026-10-13, quoted in the report this test guards):
// `gopi task submit` refuses a non-uuid row id and names two remedies, the
// second being `gopi task update <id> --repo <path-to-live-work-tree>`. That
// remedy resolved its argument to the DURABLE repo (DurableRepoForSubmit) and
// short-circuited on the REPO axis — which a linked worktree OF THE ROW'S OWN
// REPO always satisfies — so the answer was "already names <repo> — nothing to
// repair" and `deliverable.worktree` stayed unset. The row therefore never
// became a candidate of the reclaim arm, which is exactly the outcome the
// refusal promised the operator could reach. The door was advertised and
// un-performable.
//
// WHAT THIS FILE PINS, and why each assertion is the smallest sample that
// separates the two shapes:
//
//	① ACCEPTANCE (b): `--repo <live linked worktree of the row's own repo>`
//	   STAMPS the ground pair. The ledger is READ BACK (the blob, then the
//	   typed reader), never the returned string.
//	② ACCEPTANCE (c): the row is then runnable in the sense the refusal
//	   promises — TaskRunGroundFor no longer refuses and resolves to THAT
//	   directory. The uuid gate is bypassed because a live recorded worktree
//	   exists (task_run_ground.go's own condition), which is the whole point of
//	   the remedy.
//	③ ACCEPTANCE (d) first half: a SECOND run naming a DIFFERENT live worktree
//	   of the same repo REFUSES by name and writes nothing — the 2026-09-12
//	   foreign-owner hijack guard, intact.
//	④ ACCEPTANCE (d) second half: a worktree ANOTHER ROW already names refuses,
//	   naming that row, and writes nothing.
//	⑤ The argument must be EVIDENCE, not a copy of argv: a directory that is
//	   not a registered linked worktree, and a DETACHED one, are both refused.
//	⑥ The pure repo repair is unchanged: `--repo <a different repo>` still
//	   re-points the repo axis and clears the stale ground pair.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// repairGroundFixture pins a private ledger, builds a real repo with a real
// linked worktree, and submits the row in the DEFECT SHAPE: deliverable.repo
// set, no worktree key. It returns the row id, the repo's main checkout and the
// linked worktree path.
func repairGroundFixture(t *testing.T, taskID, repoName, branch string) (id, repo, wt string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	repo = groundTestRepo(t, repoName)
	wt = filepath.Join(filepath.Dir(repo), repoName+"-wt")
	if out, err := groundTestGit(repo, "worktree", "add", "-q", "-b", branch, wt); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "repair ground " + taskID, Payload: "do the work", Repo: repo, TaskID: taskID,
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	// The row must be in the defect shape BEFORE anything is asked of the verb:
	// a pre-condition that fails would make every assertion below vacuous.
	if got := deliverableWorktreeStored(t, taskID); got != "" {
		t.Fatalf("fixture: the row must be born with no worktree key, got %q", got)
	}
	return taskID, repo, wt
}

// deliverableWorktreeStored reads the worktree axis STRAIGHT OUT OF THE LEDGER —
// the raw blob through the ONE parser, never a rendered view of it. A test that
// read the verb's own stdout would pass on a message that lied about the write.
func deliverableWorktreeStored(t *testing.T, id string) string {
	t.Helper()
	raw := taskDeliverableRawStored(t, id)
	return taskDeliverableParse(raw).Worktree
}

// deliverableBranchStored is the branch axis of the same read.
func deliverableBranchStored(t *testing.T, id string) string {
	t.Helper()
	return taskDeliverableParse(taskDeliverableRawStored(t, id)).Branch
}

// taskDeliverableRawStored reads a row's raw deliverable blob from the ledger
// through the ONE row reader the package already uses.
func taskDeliverableRawStored(t *testing.T, id string) string {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row, err := taskByID(context.Background(), db, id)
	if err != nil {
		t.Fatalf("read row %s: %v", id, err)
	}
	if row == nil {
		t.Fatalf("row %s is not in the ledger", id)
	}
	return strings.TrimSpace(row.Deliverable)
}

// TestRepairRepoStampsTheGroundPromisedByTheSubmitRefusal is ACCEPTANCE (b) and
// (c): the advertised door performs the repair it names.
func TestRepairRepoStampsTheGroundPromisedByTheSubmitRefusal(t *testing.T) {
	const branch = "task/01a131f5-repair-ground"
	id, repo, wt := repairGroundFixture(t, "01a131f5-0000-7000-8000-00000000c101", "repair-repo", branch)

	line, err := TaskRepointDeliverableRepo(id, wt)
	if err != nil {
		t.Fatalf("naming a live registered linked worktree of the row's own repo must PERFORM the repair, not refuse: %v", err)
	}
	// ① THE LEDGER READ BACK — the assertion the whole epic turns on.
	if got := deliverableWorktreeStored(t, id); !SameDir(got, wt) {
		t.Fatalf("deliverable.worktree = %q, want the worktree that was named %q (verb said %q; raw blob %s)",
			got, wt, line, taskDeliverableRawStored(t, id))
	}
	if got := deliverableBranchStored(t, id); got != branch {
		t.Fatalf("deliverable.branch = %q, want the registration's branch %q", got, branch)
	}
	if !SameDir(taskDeliverableParse(taskDeliverableRawStored(t, id)).Repo, repo) {
		t.Fatalf("the ground stamp must not move the repo axis; blob is %s", taskDeliverableRawStored(t, id))
	}
	// ② RUNNABLE IN THE SENSE THE REFUSAL PROMISES.
	got, err := TaskRunGroundFor(id)
	if err != nil {
		t.Fatalf("after the repair the row must resolve a ground (this is what the submit refusal promises): %v", err)
	}
	if !SameDir(got.Dir, wt) {
		t.Fatalf("the runnable ground resolved to %q, want the stamped %q", got.Dir, wt)
	}

	// Re-naming the SAME worktree is the satisfied state, reported (never a
	// silent ok, and never a refusal — the request already holds).
	again, err := TaskRepointDeliverableRepo(id, wt)
	if err != nil {
		t.Fatalf("re-naming the recorded worktree must be a satisfied no-op, got %v", err)
	}
	if !strings.Contains(again, TaskRepairGroundPresentPhrase) {
		t.Errorf("re-naming the SAME worktree must report the satisfied state (it is NOT the never-rebind refusal); got %q", again)
	}
	if got := deliverableWorktreeStored(t, id); !SameDir(got, wt) {
		t.Errorf("the no-op must not move the recorded ground; blob is %s", taskDeliverableRawStored(t, id))
	}
}

// TestRepairRepoNeverRebindsARecordedGround is ACCEPTANCE (d) first half: a
// second run naming a DIFFERENT live worktree of the same repo REFUSES by name
// and writes nothing. This is the 2026-09-12 foreign-owner hijack guard, and it
// is the half that must survive the new ground arm.
func TestRepairRepoNeverRebindsARecordedGround(t *testing.T) {
	id, repo, wt := repairGroundFixture(t, "01a131f5-0000-7000-8000-00000000c102", "rebind-repo", "task/01a131f5-rebind-a")
	wt2 := filepath.Join(filepath.Dir(repo), "rebind-repo-wt2")
	if out, err := groundTestGit(repo, "worktree", "add", "-q", "-b", "task/01a131f5-rebind-b", wt2); err != nil {
		t.Fatalf("git worktree add wt2: %v\n%s", err, out)
	}
	if _, err := TaskRepointDeliverableRepo(id, wt); err != nil {
		t.Fatalf("first stamp (wt): %v", err)
	}
	before := taskDeliverableRawStored(t, id)

	line, err := TaskRepointDeliverableRepo(id, wt2)
	if err == nil {
		t.Fatalf("naming a DIFFERENT worktree for a row that already records one must REFUSE (never re-bind); got %q", line)
	}
	if !strings.Contains(err.Error(), "never re-pointed") {
		t.Errorf("the refusal must name the never-re-point rule; got %v", err)
	}
	if !strings.Contains(err.Error(), wt2) {
		t.Errorf("the refusal must name the path it refused; got %v", err)
	}
	if after := taskDeliverableRawStored(t, id); after != before {
		t.Fatalf("a refused repair must write NOTHING; blob was %s, now %s", before, after)
	}
}

// TestRepairRepoRefusesAWorktreeAnotherRowNames is ACCEPTANCE (d) second half:
// ONE WORKTREE, ONE OWNING ROW. The rival row owns the directory through its
// own deliverable (the shape the reclaim arm reads), and the repair must refuse
// naming it — and must not have written the ground.
func TestRepairRepoRefusesAWorktreeAnotherRowNames(t *testing.T) {
	id, repo, wt := repairGroundFixture(t, "01a131f5-0000-7000-8000-00000000c103", "owned-repo", "task/01a131f5-owned")
	rival := "01a131f5-0000-7000-8000-00000000c104"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "rival owner", Payload: "owns the directory", Repo: repo, TaskID: rival,
	}); err != nil {
		t.Fatalf("TaskSubmit rival: %v", err)
	}
	// The rival's claim is written by the ONE writer — the same door production
	// uses (the ownership scan reads exactly this field).
	if written, err := taskBackfillDeliverableWorktreeAt(rival, wt, "task/01a131f5-owned", &repo, taskWorktreeStampMomentClaim); err != nil || !written {
		t.Fatalf("fixture: the rival row must own %s (written=%v err=%v)", wt, written, err)
	}
	before := taskDeliverableRawStored(t, id)

	line, err := TaskRepointDeliverableRepo(id, wt)
	if err == nil {
		t.Fatalf("a worktree another row already names must be REFUSED; got %q", line)
	}
	if !strings.Contains(err.Error(), rival) {
		t.Errorf("the refusal must name the owning row %s; got %v", rival, err)
	}
	if after := taskDeliverableRawStored(t, id); after != before {
		t.Fatalf("a refused repair must write NOTHING; blob was %s, now %s", before, after)
	}
}

// TestRepairRepoGroundRequiresALiveRegisteredLinkedWorktree pins the EVIDENCE
// half: only a directory git actually lists as a linked worktree of the row's
// own repo may become the ground. A bare directory inside the repo, a path that
// does not exist, and a DETACHED worktree are each refused by name.
func TestRepairRepoGroundRequiresALiveRegisteredLinkedWorktree(t *testing.T) {
	id, repo, wt := repairGroundFixture(t, "01a131f5-0000-7000-8000-00000000c105", "evidence-repo", "task/01a131f5-evidence")

	// A plain directory that is not a worktree at all.
	plain := filepath.Join(t.TempDir(), "not-a-worktree")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRepointDeliverableRepo(id, plain); err == nil {
		t.Errorf("a plain directory %s must not become the row's ground", plain)
	}
	// A subdirectory of the row's repo: inside a work tree, but not a ground.
	if _, err := TaskRepointDeliverableRepo(id, filepath.Join(repo, ".git")); err == nil {
		t.Errorf("a path inside the repo that git does not register as a linked worktree must not become the ground")
	}
	// A DETACHED linked worktree: a real registration with no branch, so half a
	// ground pair — refused, naming the branch the ground needs.
	det := filepath.Join(filepath.Dir(repo), "evidence-repo-det")
	if out, err := groundTestGit(repo, "worktree", "add", "-q", "--detach", det, "master"); err != nil {
		t.Fatalf("git worktree add --detach: %v\n%s", err, out)
	}
	line, err := TaskRepointDeliverableRepo(id, det)
	if err == nil {
		t.Fatalf("a detached worktree names no branch, so it cannot be a ground; got %q", line)
	}
	if !strings.Contains(err.Error(), "DETACHED") {
		t.Errorf("the refusal must name the detached shape; got %v", err)
	}
	if got := deliverableWorktreeStored(t, id); got != "" {
		t.Fatalf("no refusal above may write the ground; blob is %s", taskDeliverableRawStored(t, id))
	}
	// And the live, registered, attached worktree is still accepted afterwards:
	// the refusals above reject the ARGUMENT, never the row.
	if _, err := TaskRepointDeliverableRepo(id, wt); err != nil {
		t.Fatalf("the row must still accept a genuine ground after refused arguments: %v", err)
	}
}

// TestRepairRepoStillRepointsADifferentRepo pins that the ORIGINAL repair is
// untouched: naming another repository moves the repo axis and clears the ground
// pair that belonged to the old one (the epic 01a0d55b behaviour).
func TestRepairRepoStillRepointsADifferentRepo(t *testing.T) {
	id, _, wt := repairGroundFixture(t, "01a131f5-0000-7000-8000-00000000c106", "old-repo", "task/01a131f5-old")
	if _, err := TaskRepointDeliverableRepo(id, wt); err != nil {
		t.Fatalf("stamp the ground first: %v", err)
	}
	other := groundTestRepo(t, "new-repo")
	line, err := TaskRepointDeliverableRepo(id, other)
	if err != nil {
		t.Fatalf("re-pointing onto another repo: %v", err)
	}
	if !strings.Contains(line, "deliverable.repo:") {
		t.Errorf("the repo re-point must keep its confirmation shape; got %q", line)
	}
	blob := taskDeliverableRawStored(t, id)
	got := taskDeliverableParse(blob)
	if !SameDir(got.Repo, other) {
		t.Fatalf("deliverable.repo = %q, want %q", got.Repo, other)
	}
	if got.Worktree != "" || got.Branch != "" {
		t.Fatalf("the ground pair belonged to the OLD repo and must be cleared; blob is %s", blob)
	}
}

// TestRepairRepoMakesANonUUIDRowRunnable is ACCEPTANCE (c), stated in the
// refusal's OWN scenario: a slug id that CANNOT name a ground, refused by
// TaskRunGroundFor exactly as the submit refusal predicts, becomes RUNNABLE
// after the advertised remedy — because the row now records a live worktree and
// the uuid gate sits at the DERIVATION, not before the reuse.
//
// This is the row the refusal talks about; a uuid fixture would prove the stamp
// and miss the point, since the gate would never have fired for it.
func TestRepairRepoMakesANonUUIDRowRunnable(t *testing.T) {
	const slug = "01a131f5slugrow-fixture"
	const branch = "task/01a131f5-slug"
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	repo := groundTestRepo(t, "slug-repo")
	wt := filepath.Join(filepath.Dir(repo), "slug-repo-wt")
	if out, err := groundTestGit(repo, "worktree", "add", "-q", "-b", branch, wt); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	// The library face accepts the slug id (only the CLI submit face carries the
	// early refusal, by design — see task_submit_ground_name.go), which is what
	// lets this test construct the state the refusal describes.
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "slug row needing a ground", Payload: "do the work", Repo: repo, TaskID: slug,
	}); err != nil {
		t.Fatalf("TaskSubmit slug row: %v", err)
	}
	// BEFORE: the refusal the operator was sent here from.
	_, err := TaskRunGroundFor(slug)
	if err == nil {
		t.Fatalf("fixture: a slug-id repo-aware row with no ground must be refused (that is the defect's premise)")
	}
	if !strings.Contains(err.Error(), "not a full uuid") {
		t.Fatalf("the pre-repair refusal must be the uuid one; got %v", err)
	}
	// The door.
	if _, err := TaskRepointDeliverableRepo(slug, wt); err != nil {
		t.Fatalf("the advertised remedy must perform the repair for the very row shape it names: %v", err)
	}
	// AFTER: runnable, on the stamped directory, with the registration's branch.
	got, err := TaskRunGroundFor(slug)
	if err != nil {
		t.Fatalf("after the repair the slug row must resolve a ground — this is the promise the refusal makes: %v", err)
	}
	if !SameDir(got.Dir, wt) {
		t.Fatalf("resolved ground = %q, want the stamped %q", got.Dir, wt)
	}
	if got.Branch != branch {
		t.Errorf("resolved branch = %q, want the registration's %q", got.Branch, branch)
	}
}
