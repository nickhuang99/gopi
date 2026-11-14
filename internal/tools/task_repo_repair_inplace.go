package tools

// task_repo_repair_inplace.go — THE ROW-SIDE HALF OF THE DEAD-WORKSPACE REPAIR
// (epic 01a17980; the VALUE half is task_submit_durable_repo_repair.go).
//
// THE MEASURED DEFECT. Nineteen failed rows were killed at DISPATCH, before any
// run: their deliverable.repo named a clone-channel workspace
// (<workspace_root>/<some-other-task-id>) that had already been reclaimed. The
// clone channel refused — correctly, and now naming the REAL reason
// (01a15629) — but the refusal arrived at the LAST possible moment, so the row
// entered `failed` still carrying the dead path, and every later attempt met the
// same refusal again. The retired reopen verb made it worse: its deliverable
// copy INHERITED the judged row's repo verbatim, so the successor was born with
// the same dead path and could never run either — which is why `gopi task
// clone` reaches the SAME resolver this file owns (taskCloneDeliverable).
//
// THE TWO SEAMS THAT ACT ON A ROW'S REPO are therefore both repaired by ONE
// resolver here:
//
//	① the CLAIM/LAUNCH seam (runTaskRunOnce) — the row is repaired BEFORE the
//	   channel that would refuse it is asked, so it never reaches `failed`
//	   carrying a dead path;
//	② the SUCCESSOR seam (taskCloneDeliverable) — the inherited repo is repaired
//	   BEFORE it is stamped, so a successor can never re-stamp the dead path.
//
// NOTHING IS RE-DERIVED HERE (Rules 1/13/14). The recovery is the SAME ledger
// fact RepairDurableRepoForSubmit reads — the row that recorded that workspace as
// its deliverable.worktree (taskWorkspaceRowRepo) — and the ledger write is
// TaskRepointDeliverableRepo, the ONE deliverable-blob writer that already
// carries the CAS, the event trail and the ground-drop rule. The workspace root
// comes from the ONE reader (config.WorkspaceRootConfigured) and the containment
// test from the ONE predicate (worktreeroots.Within).
//
// WHY THIS RESOLVER IS TRANSITIVE, MEASURED. The first real reissue (row
// 01a15a17, 2026-10-27) came back repaired to
// /home/user/workspace/01a1597d-… — a workspace that was STILL ALIVE, because it
// was some other run's current clone. RepairDurableRepoForSubmit answers such a
// path IDENTITY (it IS a git work tree, and for a SUBMIT that is correct: the
// workspace's own origin is the durable answer, which cloneGroundSourceRepo
// reads). A ROW may not carry it: the clone channel reclaims it when that run's
// push lands, which is exactly how the nineteen rows were born. So the row-side
// question is asked differently and repeatedly: WHILE the value lies under the
// workspace root, follow the recorded repository, up to a bounded number of hops.
//
// Rule 15: the repair is RETURNED as one readable line per hop (the MISS case is
// deliberately silent HERE — a path that could not be recovered is the clone
// channel's refusal to make loud, and it already does, naming the real reason).

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/worktreeroots"
)

// taskDeadRepoRepairMaxHops bounds the transitive walk. A chain of reclaims is
// finite in practice (row A's workspace → row B's repo, B's workspace → row C's
// repo, …); the cap is what makes a CYCLE impossible rather than merely
// unlikely — a row whose recorded repo loops back is left as it stands and the
// clone channel refuses it by name.
const taskDeadRepoRepairMaxHops = 4

// TaskRepairDeadRepoInPlaceFmt is the ONE shape of "this row's repo was repaired
// in place": the row, the repair's own account, and the ledger write it
// produced.
const TaskRepairDeadRepoInPlaceFmt = "task %s: %s — %s"

// TaskRepairDeadRepoInPlaceErrFmt is the ONE shape of "the recovery was found
// but the ledger would not take it" — never swallowed into the silent arm: a
// repaired value the row did not receive is a fact the operator must see.
const TaskRepairDeadRepoInPlaceErrFmt = "task_repo_repair: %s: recovered the repository but could not record it on the row: %w"

// durableRowRepo resolves repo to a repository a LEDGER ROW may carry, and
// answers the ONE line describing each substitution it made ("" = the value
// stands as given).
//
// The rule it enforces in one sentence: a path under the workspace root is a
// clone the channel made and will reclaim, so it is never a row's repo — follow
// it, repeatedly, until the answer lies outside the root (or nothing more can be
// recovered, in which case the caller keeps the value it was given and the clone
// channel refuses it naming the real reason).
//
// THE TWO EXISTING RECOVERIES ARE BOTH USED, and neither is re-derived here
// (Rule 13/14): a workspace that is STILL ALIVE answers with the repository it
// was cloned from (DurableRepoForSubmitWithNote → cloneGroundSourceRepo, which
// asks git for the origin and the ground model for whether the question even
// applies), and a RECLAIMED one answers with the repository its own ledger row
// recorded (RepairDurableRepoForSubmit → taskWorkspaceRowRepo, because a gone
// directory has no origin to read). Which arm applies is decided by whether the
// directory is still a git work tree — a fact, not a guess.
//
// It is read-only: nothing is written here, which is what lets the reissue seam
// use it before a successor row exists.
func durableRowRepo(repo string) (string, string) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return repo, ""
	}
	root, _, err := config.WorkspaceRootConfigured()
	if err != nil || root == "" {
		// An unreadable workspace root is NOT a licence to guess: the repair is
		// confined to the channel's own checkouts, and without the root there is
		// no way to tell one from an operator's repository.
		return repo, ""
	}
	cur := repo
	var notes []string
	for hop := 0; hop < taskDeadRepoRepairMaxHops; hop++ {
		if !worktreeroots.Within(cur, root) {
			break // outside the channel's checkouts: this value is durable
		}
		next, note := taskWorkspaceHopRepo(cur)
		if note == "" || strings.TrimSpace(next) == "" || SameDir(next, cur) {
			// Nothing more can be recovered. The path is handed back untouched;
			// the clone channel names the real reason when it refuses.
			break
		}
		notes = append(notes, note)
		cur = strings.TrimSpace(next)
	}
	return cur, strings.Join(notes, "; ")
}

// taskWorkspaceHopRepo takes ONE hop from a workspace path to a repository a row
// may carry, using whichever of the two existing recoveries the path's MEASURED
// state calls for. "" means "this hop has nothing to say".
func taskWorkspaceHopRepo(cur string) (string, string) {
	if _, why := devworkspace.RepoPathReadable(cur); why != "" {
		// A GONE (or non-git) workspace: the only surviving record is the ledger
		// row that stamped it as its ground.
		next, note := RepairDurableRepoForSubmit(cur)
		if note == "" {
			return "", ""
		}
		return next, note
	}
	// A LIVE workspace: its own origin is the durable answer, and the ONE
	// resolver that already asks for it decides whether the question applies
	// (a linked worktree under the same root keeps its own answer).
	next, note, derr := DurableRepoForSubmitWithNote(cur)
	if derr != nil || note == "" {
		return "", ""
	}
	return next, note
}

// TaskRepairDeadRepoInPlace repairs a row's deliverable.repo IN PLACE when it
// names a clone-channel workspace, and answers the ONE line describing it ("" =
// nothing to repair: the row names no repo, or the repo it names is outside the
// workspace root, which is every normal row).
//
// It is IDEMPOTENT by construction: the repaired value lies outside the workspace
// root, so a second call sees nothing to follow and returns silence with no git
// process spawned. That is what lets the claim seam call it on every dispatch.
//
// It returns an error ONLY for "recovered but could not record": the recovery is
// a value the caller may still use, and the ledger write is a separate fact.
func TaskRepairDeadRepoInPlace(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", nil
	}
	repo := taskRowDeliverableRepo(taskID)
	if repo == "" {
		return "", nil
	}
	repaired, note := durableRowRepo(repo)
	if note == "" || SameDir(repaired, repo) {
		return "", nil
	}
	repointed, err := TaskRepointDeliverableRepo(taskID, repaired)
	if err != nil {
		return "", fmt.Errorf(TaskRepairDeadRepoInPlaceErrFmt, taskID, err)
	}
	return fmt.Sprintf(TaskRepairDeadRepoInPlaceFmt, taskID, note, repointed), nil
}
