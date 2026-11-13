package tools

// The DETERMINISTIC CHECKLIST every arm runs before it removes a run's
// worktree (epic: the reclamation end of "the worker owns the child's ground").
//
// WHY A SHARED PREDICATE AND NOT A PARALLEL CHAIN (Rules 13/14): the guards that
// already exist answer the DIRECTORY's questions and are NOT re-implemented
// here — `finishCleanupWorktreeInUse` / `reclaimDisposition`'s Guard ③ own
// in-use, `finishCleanupProbeClean` + `finishCleanupWorktreeDirtyReason` +
// `reclaimDirtyReason` own dirt (Rule 30), `mergeHasCommonAncestor` owns the
// merge lane's lineage verdict, and the liveness question is answered by
// `taskSelfIdentityValues` / `finishCleanupProcessHolderLive` /
// `worktreeSweepSessionLiveEvidence`. This file adds the TWO rungs the guards
// leave open and routes every removal arm through them:
//
//	① THE GROUND IS NOT MINE. A process must never free the checkout it is
//	  standing in — `git worktree remove` deletes the directory under the running
//	  process, and the loss stays invisible until some later step asks for its own
//	  cwd. Measured 2026-09-21 (`~/workspace/gopi-jieba-searchkw`, row 01a0c574,
//	  holder pid 1191072 alive): the freed directory was the run's own ground.
//	② THE BRANCH SHARES HISTORY WITH THE INTEGRATION BRANCH. A branch with NO
//	  common ancestor is the state `cmd/agent/merge_run.go` names as
//	  unrelated-history and refuses to treat as an ordinary branch; the same
//	  question is asked here (`git merge-base`, the direct measurement —
//	  `--is-ancestor` is its one-way projection and would additionally refuse a
//	  healthy branch that is merely BEHIND), so the two lanes cannot disagree
//	  about one branch's lineage.
//
// A FAILED CHECK NEVER REMOVES ANYTHING: removal is never the response to a
// failed check. The verdict is a sentence (the epic's "refuses naming the failed
// check"), and each arm renders it into its own output line and its own
// removalaudit evidence string.
//
// CONFLICTS ARE OUT OF SCOPE, deliberately and verifiably: nothing here
// measures, classifies, transmits or reports a conflict. A branch that merges
// with conflicts shares history and passes rung ② untouched; what to do with it
// is `gopi merge`'s business, and this file never asks.

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// reclaimGroundChecklistVerdictForCloseout is rung ① asked by a run's OWN
// close-out (the finish-cleanup chain), where the asker's own cwd is NOT a veto.
//
// WHY THERE ARE TWO ENTRY POINTS AND NOT ONE (measured 2026-09-25, epic
// master-reds-internal-tools). Rung ①'s harm model is "a process is standing in
// the ground and will keep working after it is deleted". That model does NOT
// hold for the run's own close-out, and two independent surfaces say so:
//
//	cmd/agent cli_tasks_recycle_e2e_test.go:TestManualRunRecyclesItsWorktree —
//	  the REAL `gopi task run` binary, whose close-out runs with its own CWD
//	  inside the worktree it must recycle, came back RED the moment this rung
//	  was wired here ("manual run left its worktree behind"); it is the measured
//	  regression the in-use guard's self-tree exclusion exists for.
//	TestFinishCleanupWorktreeHeldByCallerIsRecycled — the same contract one
//	  level down, on the arm itself: "the caller's own CWD is not a competing
//	  holder".
//
// The two questions rung ① was standing in for are asked by the chain ITSELF,
// on this path, and asked better (Rule 13/14 — no second implementation):
//
//	"is the ROW still alive?"          → rung ⓪ (reclaimLiveSessionRefusal) and
//	                                     finishCleanupWorkCompletionReason —
//	                                     a live or unproven row's ground stays.
//	"is a THIRD PARTY standing in it?" → finishCleanupWorktreeInUse, whose
//	                                     self-tree exclusion is the ONE reading
//	                                     of "my own process is not a competing
//	                                     holder".
//
// The arms that free SOMEBODY ELSE'S ground (the sweep, the ledger-keyed
// reclaim arm) keep the unmodified rung: there the asker's own position is not
// its own close-out, and the 2026-09-21 breach (`~/workspace/gopi-jieba-searchkw`,
// row 01a0c574, holder alive, branch 14 ahead) came through exactly that route.
func reclaimGroundChecklistVerdictForCloseout(repo, worktree, branch string) string {
	return reclaimGroundChecklistVerdictExceptOwnGround(repo, worktree, branch, true)
}

// reclaimGroundChecklistVerdict runs the checklist for one candidate removal and
// returns "" when the arm may proceed, or a NAMED refusal sentence (naming the
// failed check, the branch, the worktree and — where git spoke — git's own
// diagnostic, so the reader can reproduce the question).
//
// It reads no /proc, compares no pid and calls no liveness ladder: the liveness
// and in-use questions are NOT this function's (see the file doc) and asking
// them here would be a second implementation of a guard that already exists.
func reclaimGroundChecklistVerdict(repo, worktree, branch string) string {
	return reclaimGroundChecklistVerdictExceptOwnGround(repo, worktree, branch, false)
}

// reclaimGroundChecklistVerdictExceptOwnGround is the ONE body (Rules 13/14):
// skipOwnGround selects whether rung ① asks the asker's own position (see
// reclaimGroundChecklistVerdictForCloseout for which caller passes true and why).
func reclaimGroundChecklistVerdictExceptOwnGround(repo, worktree, branch string, skipOwnGround bool) string {
	worktree = strings.TrimSpace(worktree)
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	// ① the ground is not mine. SKIPPED in two cases, and they are the same
	// fact reached two ways: `skipOwnGround` is the close-out ENTRY POINT
	// (reclaimGroundChecklistVerdictForCloseout — see its doc for why the run's
	// own close-out does not hold rung ①'s harm model), and
	// SelfGroundReleasePendingFor(worktree) is the same fact published for the
	// arm that reaches this checklist through the OWNER-INDEPENDENT pass
	// (reclaim_own_ground.go): the directory is the ground of one of THIS
	// process's own finished rows, so it is the pass's to release, not a
	// stranger's the rung exists to protect.
	if !skipOwnGround && !SelfGroundReleasePendingFor(worktree) {
		if why := guestRowOwnCheckoutRefusal(worktree); why != "" {
			return why
		}
	}
	// ② the branch shares history with the integration branch. A registration
	// without a branch has no lineage to judge (the sweeper's ordinary shape) —
	// rung ② says nothing about it, and INVENTING a refusal there would be a
	// stopped release pass, not a guard.
	if repo == "" || branch == "" || worktree == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	_, err := finishCleanupGitRaw(ctx, repo, reclaimMergeBaseVerb, IntegrationBranchName, branch)
	if err == nil {
		return ""
	}
	// ⚠️ THE UNANSWERED QUESTION IS NOT A FAILED CHECK. `git merge-base` exits
	// 1 EXACTLY when the two tips have no common ancestor — that exit IS rung
	// ②'s answer, and the precedent is cmd/agent/merge_run.go:2190
	// (`mergeHasCommonAncestor` reads `err == nil`, so a MISSING base is the
	// answer, not a failure to report). Anything else is git being UNABLE to ask
	// the question at all — the integration branch does not exist yet, the repo
	// is not one, the object is missing — and a refusal there invents a lineage
	// verdict out of an unread fact. Measured 2026-09-22: git exits 128 for a
	// missing ref (`git merge-base master <b>` in a `git init` fixture that has
	// no `master`), 1 for a genuinely unrelated one; the first version of this
	// rung refused BOTH, which vetoed the worktree removal on
	// cmd/agent/cli_tasks_rancheck_seam_test.go's fixture (git init, no commit,
	// no integration branch) and reddened two tests unrelated to any merga.
	//
	// The discriminator is git's OWN exit code, read through an
	// `*exec.ExitError` — never the rendered English sentence (merge_run.go:2190
	// rejects message matching for the same reason: that spelling is git's, not
	// ours).
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != mergeBaseNoCommonAncestorExit {
		return ""
	}
	// git's own diagnostic carries the question it asked (its error text
	// names the verb and both revisions), so the sentence quotes git once
	// rather than re-printing the argv — measured on the real verb, where
	// the doubled prefix read as two different git invocations.
	return reclaimGroundLineageRefusalFmt + ": " + err.Error()
}

// mergeBaseNoCommonAncestorExit is git's exit status for "the two tips share NO
// ancestor" — rung ②'s POSITIVE finding, and the only exit that blocks a
// removal. Every other status (128 for a missing ref or a non-repo, or a
// timeout) means the question could not be asked. Rule 1b: a number with a
// meaning has a name.
const mergeBaseNoCommonAncestorExit = 1

// reclaimMergeBaseVerb is git's `merge-base` subcommand — the ONE spelling the
// checklist's lineage rung uses (Rule 1b: a verb referenced by tests and greps
// has a name, not a literal buried in an arg list).
const reclaimMergeBaseVerb = "merge-base"

// reclaimGoalChecklistSummary is the checklist's identity as it appears in an
// arm's removalaudit evidence string — ONE spelling (Rule 13), so a reader can
// grep every record an arm wrote and see whether the checklist was consulted
// before that removal.
const reclaimGoalChecklistSummary = "own-ground + integration-lineage (reclaim_ground_checklist.go)"

// reclaimGroundLineageRefusalFmt is the checklist's rung-② sentence (Rule 13:
// one spelling, referenced by the checklist, the arms' tests and the report).
const reclaimGroundLineageRefusalFmt = "refusing to free the ground of a branch sharing no ancestor with " +
	IntegrationBranchName + " — an unrelated-history branch is not an ordinary deliverable"
