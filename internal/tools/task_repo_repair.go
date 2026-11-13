package tools

// task_repo_repair.go — the row-repair surface the ground refusal points at
// (epic 01a0d55b).
//
// WHY IT EXISTS: a row whose deliverable.repo names a directory that is no
// longer a git work tree cannot be run at all — TaskRunGroundFor refuses,
// correctly, because it cannot tell the repo's main checkout from a task ground.
// Two shapes reach that state:
//
//   - a repo the operator renamed/moved/deleted;
//   - the defect this epic fixes: a TOO_BIG reframe's child rows born with the
//     PARENT RUN's ground as their "repo" (the linked worktree the worker
//     creates for that run and deletes when it finishes) — measured live on rows
//     01a0d528-*, all four refused with "the repo is not a git work tree".
//
// The generator is fixed (reframeSubmitOptions + DurableRepoForSubmit). The rows
// already in that state need a door, and hand-editing the ledger is not one: the
// repair goes through the SAME single blob writer every other deliverable stamp
// uses (taskDeliverableParse/Rebuild, CAS on the raw blob — Rules 13/14), so the
// attachments manifest and the append-only event trail are preserved.
//
// The new repo is resolved with DurableRepoForSubmit: an operator may hand it a
// LINKED WORKTREE (the natural thing to copy out of `git worktree list`) and the
// row lands on the repository's durable main checkout.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TaskRepairRepoVerb is the ONE spelling of the repair's flag surface, shared by
// the usage page and the refusal text (Rule 1b/13).
const TaskRepairRepoVerb = "--repo"

// TaskRepairRepointedFmt is the ONE shape of the repair's confirmation: the row,
// the repo it carried before, and the durable repo it now names.
const TaskRepairRepointedFmt = "task %s deliverable.repo: %s -> %s"

// TaskRepairSameRepoNote is returned when the row already names the resolved
// repo: the repair is a no-op, and saying so is the honest answer (Rule 15 —
// never a silent "ok" that hides a wrong argument).
const TaskRepairSameRepoNote = "task %s already names %s — nothing to repair"

// ── the GROUND arm's sentences (epic 01a131f5) ───────────────────────────────
//
// One constant per state of the ground question, because the refusal and its
// test have to agree about which state was reached (Rule 1b): "already records
// this worktree", "records a DIFFERENT one", "not a registered linked worktree
// of this repo", "another row already names it", and the success line.
const (
	// TaskRepairGroundStampedNote and TaskRepairGroundPresentPhrase are the two
	// STATE PHRASES of the ground arm — the words that tell a caller WHICH state
	// the call reached, split out from the sentences so a reader (a test, a
	// script) references the ONE spelling instead of re-typing it (Rule 1b/13:
	// the sentence and its reader must not be able to drift).
	TaskRepairGroundStampedNote   = "the row now records its ground"
	TaskRepairGroundPresentPhrase = "already names the worktree"
	// TaskRepairGroundStampedFmt is the SUCCESS line: the row now records the
	// ground, which is the outcome the submit refusal's door promises.
	TaskRepairGroundStampedFmt = "task %s deliverable.worktree: %s (branch %s) — " + TaskRepairGroundStampedNote
	// TaskRepairGroundPresentFmt — the row already records EXACTLY this
	// worktree: the request is satisfied, and re-stamping would be the
	// no-op the old message described (correctly, for THIS state).
	TaskRepairGroundPresentFmt = "task %s " + TaskRepairGroundPresentPhrase + " %s — nothing to repair"
	// TaskRepairGroundResetRefusalFmt — the row records a DIFFERENT worktree.
	// NEVER re-bound: the 2026-09-12 foreign-owner hijack was exactly this
	// write, and the ground pair is single-owner by construction.
	TaskRepairGroundResetRefusalFmt = "task repair: task %s already records the worktree %s — a recorded ground is never re-pointed " +
		"(the 2026-09-12 foreign-owner guard), so %s was NOT stamped; clear the recorded ground first if the row truly moved"
	// TaskRepairGroundNotLinkedFmt — the argument is not a LIVE REGISTERED
	// LINKED work tree of the row's own repo. This is the arm that keeps the
	// ground stamp an EVIDENCE claim rather than a copy of the operator's
	// argument (Rule 15: the reason, the path and the door).
	TaskRepairGroundNotLinkedFmt = "task repair: %s is not a LIVE REGISTERED LINKED work tree of %s " +
		"(%d linked worktree(s) registered there) — pass the row's own repo to re-point the repo axis, " +
		"or paste a linked worktree path that `git -C %s worktree list` actually lists"
	// TaskRepairGroundMainCheckoutFmt — the argument IS the row's repo (the
	// shared main checkout). Reporting it as a same-repo no-op is the honest
	// answer: the row already names it, and a main checkout is not a ground.
	TaskRepairGroundDetachedFmt = "task repair: %s is a DETACHED linked work tree of %s — a branch names the ground " +
		"(the merger's attribution and the sweep's ownership arm read it), and a detached HEAD names none, " +
		"so nothing was stamped; check the worktree out on its task branch first"
	TaskRepairGroundOwnedFmt = "task repair: the worktree %s is already named by task %s — refused (nothing was written): " +
		"naming one directory from two rows makes each block the other's recycle; %s is the row that owns it"
	TaskRepairGroundBranchOwnedFmt = "task repair: the branch %s is already named by task %s — refused (nothing was written): " +
		"one branch has one owning row, and two names for it would make the merger's attribution report two owners"
	TaskRepairGroundListErrFmt  = "task repair: cannot list the linked worktrees of %s: %w"
	TaskRepairGroundOwnerErrFmt = "task repair: cannot PROVE the worktree %s is unowned (%w) — refusing rather than " +
		"binding a directory another row may hold"
	TaskRepairGroundBranchErrFmt = "task repair: cannot PROVE the branch %s is unclaimed (%w) — refusing rather than " +
		"binding a branch another row may hold"
	TaskRepairGroundWriteErrFmt = "task repair: stamping the ground: %w"
	// TaskRepairGroundNotWrittenFmt — the ONE writer DECLINED or lost the CAS.
	// Its own reasons (the row's blob moved under us, or the worktree axis is
	// now bound) are the writer's to report; what the operator must not get is
	// silence, so the refusal names the path it could not record.
	TaskRepairGroundNotWrittenFmt = "task repair: the row's deliverable did not take the ground %s (the blob moved under the CAS, " +
		"or a concurrent writer bound the worktree axis first) — re-read the row and re-run"
)

// TaskRepointDeliverableRepo moves ONE row's deliverable.repo onto a live git
// work tree (the operator's remedy for a row whose repo died, made reachable as
// `gopi task update <id> --repo <path>`).
//
// The repo is resolved to its DURABLE main checkout (DurableRepoForSubmit), so a
// linked worktree argument — the shape `git worktree list` hands an operator —
// lands the row on the repository instead of on another ephemeral directory.
//
// CLEARING THE GROUND PAIR: when the repo actually changes, the row's recorded
// branch and worktree belong to the OLD repository and are dropped in the same
// write. Keeping them would re-create the very defect this repairs — a row
// naming a worktree that belongs to a repo it no longer points at (and the
// worktree axis is single-owner by construction: a stale one blocks its real
// owner's recycle). The attachments manifest survives the rewrite.
//
// Refusals are loud and precede the write: a missing row, an empty or
// unresolvable repo, or the same repo twice all return before any UPDATE.
//
// THE GROUND ARM (epic 01a131f5). Repairing the repo axis is not the only
// repair this verb owes: the submit refusal sends the operator here to give a
// repo-aware row a GROUND (`deliverable.worktree`), and before this arm existed
// the same-repo short-circuit above made that door a dead end — the operator
// pasted a linked worktree path out of `git worktree list`, DurableRepoForSubmit
// resolved it to the repo the row already stored, and the verb answered "nothing
// to repair" while the row kept its `{"repo":…}`-only blob. So when the
// argument IS a live registered linked worktree of the row's own repo, the same
// call now stamps the ground pair through the ONE existing blob writer
// (taskBackfillDeliverableWorktreeAt: parse/rebuild merge, CAS, attachments
// preserved), with the ONE-OWNER scans (worktree axis and branch axis) and the
// never-rebind guard intact.
//
// ORDER OF READS (all before any write): row → durable repo → same-repo
// short-circuit (reporting which of the two states it is) → linked-worktree
// registration → owner scans → the ONE writer.
func TaskRepointDeliverableRepo(taskID, repo string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task repair: task_id must not be empty")
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", fmt.Errorf("task repair: %s must name a git work tree", TaskRepairRepoVerb)
	}
	durable, err := DurableRepoForSubmit(repo)
	if err != nil {
		return "", fmt.Errorf("task repair: %w", err)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", fmt.Errorf("task repair: no task with task_id=%q", taskID)
	}
	raw := strings.TrimSpace(t.Deliverable)
	oldDel := taskDeliverableParse(raw)
	oldRepo, branch, worktree := oldDel.Repo, oldDel.Branch, oldDel.Worktree
	if SameDir(oldRepo, durable) {
		return taskRepairSameRepo(taskID, durable, repo, worktree)
	}
	// A repo-less blob is the other shape that reaches the refusal ("the row
	// names no repo at all"): the repair CREATES the repo stamp then, and there
	// is no ground pair to clear.
	merged := taskDeliverableRebuild(taskDeliverable{Repo: durable, Branch: branch, Worktree: worktree, Grounds: oldDel.Grounds, Attachments: oldDel.Attachments})
	if !SameDir(oldRepo, durable) && oldRepo != "" {
		// The repo CHANGED: the recorded branch/worktree belonged to the old
		// repository. Drop them — a ground is only meaningful inside the repo it
		// was created in (taskBackfillDeliverableWorktreeAt's own ownership
		// rule), and the next run mints a fresh one from the new repo.
		merged = taskDeliverableRebuild(taskDeliverable{Repo: durable, Grounds: oldDel.Grounds, Attachments: oldDel.Attachments})
	}
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ? AND deliverable = ?`,
		merged, float64(time.Now().Unix()), taskID, raw)
	if err != nil {
		return "", fmt.Errorf("task repair: %w", err)
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		// The CAS lost: a concurrent writer moved the blob between the read and
		// the write. Refuse rather than clobber (Rule 15 — report, never guess).
		return "", fmt.Errorf("task repair: the row's deliverable changed concurrently — re-read and re-run the repair")
	}
	if oldRepo == "" {
		return fmt.Sprintf(TaskRepairRepointedFmt, taskID, "(none)", durable), nil
	}
	return fmt.Sprintf(TaskRepairRepointedFmt, taskID, oldRepo, durable), nil
}

// taskRepairSameRepo is the SAME-REPO branch of the repair — the state the
// submit refusal's door walks into, and the reason the door was a dead end.
//
// The repo axis has nothing to repair here (the row already names it), which is
// TRUE and was also, before this epic, the whole answer: the operator pasted a
// linked worktree of that very repo and read "nothing to repair" while the row
// kept its ground-less blob. Two facts were being conflated — "the REPO is
// already right" and "there is nothing here to repair" — and only the first was
// true.
//
// So the same call now asks the GROUND question, in the one order that keeps the
// 2026-09-12 hijack guard intact:
//
//  1. the row RECORDS a worktree → this verb never re-points it (same argument
//     as the recorded one: satisfied; a different one: REFUSED by name);
//  2. the argument IS the row's repo (the shared main checkout, or DurableRepo's
//     own answer) → the repo is all that was asked for: the honest note;
//  3. otherwise the argument must be a LIVE REGISTERED LINKED worktree of that
//     repo — a directory git lists and `os.Stat` agrees exists. That is what
//     makes the stamp EVIDENCE (the operator's own `git worktree list` reading)
//     rather than a copy of his argv, and it is the same reader
//     (taskRepoLinkedWorktrees) the claim-time seam and `--resume` trust;
//  4. the ONE-OWNER scans run FAIL-CLOSED on both axes (worktree, branch);
//  5. the write is the ONE deliverable writer, so the CAS, the parse/rebuild
//     merge and the attachment manifest are the same ones every other stamp
//     uses — the row's repo axis is NOT rewritten here (it is already correct).
func taskRepairSameRepo(taskID, durable, repo, recordedWorktree string) (string, error) {
	// (2) The argument names the repo itself. Reached both by `--repo <main>`
	// and by DurableRepoForSubmit collapsing a path that is not a linked
	// worktree of it (a subdirectory, the .git dir, a symlink spelling).
	if SameDir(repo, durable) {
		return fmt.Sprintf(TaskRepairSameRepoNote, taskID, durable), nil
	}
	// (3) ONE listing, read once: its error is reported (never swallowed — an
	// unreadable registration list is not "there is no such worktree", Rule 15),
	// and both the "is it registered" and "which branch" questions are answered
	// from the same reading, so they cannot disagree.
	linked, lerr := taskRepoLinkedWorktrees(durable)
	if lerr != nil {
		return "", fmt.Errorf(TaskRepairGroundListErrFmt, durable, lerr)
	}
	branch, registered := taskRepairLinkedEntry(linked, repo)
	if recordedWorktree != "" {
		if registered && SameDir(repo, recordedWorktree) {
			// (1a) The row already records exactly this worktree.
			return fmt.Sprintf(TaskRepairGroundPresentFmt, taskID, recordedWorktree), nil
		}
		// (1b) A DIFFERENT worktree: never re-bound. Silence here would be the
		// hijack this guard exists for, and the refusal says so.
		return "", fmt.Errorf(TaskRepairGroundResetRefusalFmt, taskID, recordedWorktree, repo)
	}
	if !registered {
		return "", fmt.Errorf(TaskRepairGroundNotLinkedFmt, repo, durable, len(linked), durable)
	}
	if branch == "" {
		// A worktree this repo registers but `git worktree list` gives no branch
		// for: DETACHED. A ground is the worktree + the branch pair; stamping a
		// path whose branch cannot be named would leave the row half-bound.
		return "", fmt.Errorf(TaskRepairGroundDetachedFmt, repo, durable)
	}
	// (4) ONE WORKTREE / ONE BRANCH, ONE OWNING ROW — the same two scans the
	// claim-time seam runs, through the same seam for the worktree axis so the
	// fail-closed branch stays observable (and no second opinion about ownership
	// can exist). Both errors mean "no owner could be PROVEN" and both refuse.
	owner, oerr := taskWorktreeOwnerScan(taskID, repo)
	if oerr != nil {
		return "", fmt.Errorf(TaskRepairGroundOwnerErrFmt, repo, oerr)
	}
	if owner != "" {
		return "", fmt.Errorf(TaskRepairGroundOwnedFmt, repo, owner, owner)
	}
	bowner, berr := taskBranchRowOwner(taskID, durable, branch)
	if berr != nil {
		return "", fmt.Errorf(TaskRepairGroundBranchErrFmt, branch, berr)
	}
	if bowner != "" {
		return "", fmt.Errorf(TaskRepairGroundBranchOwnedFmt, branch, bowner)
	}
	// (5) THE ONE WRITER. groundRef is the row's OWN durable repo, so the
	// writer's own cross-repo guard (a claim-time stamp must never bind the row
	// to another project's worktree) is armed exactly as it is at the claim
	// seam; the never-rebind guard inside it is the second half of (1).
	written, werr := taskBackfillDeliverableWorktreeAt(taskID, repo, branch, &durable, taskWorktreeStampMomentRepair)
	if werr != nil {
		return "", fmt.Errorf(TaskRepairGroundWriteErrFmt, werr)
	}
	if !written {
		return "", fmt.Errorf(TaskRepairGroundNotWrittenFmt, repo)
	}
	return fmt.Sprintf(TaskRepairGroundStampedFmt, taskID, repo, branch), nil
}

// taskRepairLinkedEntry answers BOTH halves of the registration question for dir
// from ONE reading of the list: whether repo registers dir as a linked worktree
// at all, and the branch that registration is checked out on ("" = detached).
//
// Both halves are load-bearing and neither is inferable from the other: the
// REGISTRATION is what the sweep, the close-out chain and `--resume` act on (a
// directory git does not list would be stamped as a ground nothing would ever
// reclaim), and the DIRECTORY is what a run needs to start in
// (taskRepoLinkedWorktrees already drops registrations whose directory is gone).
// The listing is the ONE reader, and the comparison is by directory identity —
// the same rule taskResumeGroundLinkedEntry follows, so an operator's `pwd` and
// the registration's spelling of the same directory cannot disagree.
func taskRepairLinkedEntry(linked []taskLinkedWorktree, dir string) (branch string, registered bool) {
	for _, e := range linked {
		if SameDir(e.path, dir) {
			return e.branch, true
		}
	}
	return "", false
}
