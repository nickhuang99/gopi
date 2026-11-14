package tools

// task_reclaim_clone_residue.go — the CLONE ground's ATTRIBUTION predicates for
// Rule 30's close-out residue gate.
//
// THE GAP THIS CLOSES (measured 2026-10-27, epic 01a17b9d). The close-out
// residue gate (ReclaimRunResidueCheck → reclaimRunCodeWorktree) admitted a
// ground ONLY when `git worktree list` of the row's repo named it — i.e. only a
// LINKED registration. The clone channel (`dev_repo`, this host's default) works
// in a standalone `git clone` whose `.git` is a DIRECTORY: it is registered
// NOWHERE, so `mergedCleanupWorktrees` answered an empty linked map (measured:
// linked=0 on /home/user/work/gopi) and SIX clone grounds holding 104
// uncommitted entries were measured by nobody:
//
//	ReclaimRunResidueCheck(row) → {Attributed:false Porcelain:"" Note:""}
//	                             for every one of them — no measurement, no
//	                             event, and GOPI_TASK_RUN_RESIDUE_ENFORCE=1 had
//	                             nothing to refuse.
//
// WHY THE ANSWER IS A NEW PREDICATE AND NOT A WIDER OLD ONE. Widening
// reclaimRunCodeWorktree to "any live directory" would attribute the SHARED
// MAIN CHECKOUT to whichever row closed out next — the misattribution that arm's
// own comment forbids and the 2026-09-16 leak. Attribution must stay EXCLUSIVE:
// it may name only a directory the ledger and the filesystem TOGETHER prove is
// this row's own clone ground. So the probe here is a conjunction of facts, each
// of which alone would be a guess (see reclaimRunCloneGroundIdentity).
//
// WHAT IT DOES AND DOES NOT DO. These functions only ADMIT a directory to be
// MEASURED: the measurement, the classification, the ledger event, the
// operator's switch and the tracked-work refusal are the machinery that already
// exists and are not touched here. Nothing is deleted, moved, committed or
// cleaned by this file — and the sweep's own dirt guards
// (reclaimSkipDirtyReason, finishCleanupWorktreeDirtyReason) are neither
// referenced nor loosened: their question is "may this directory be REMOVED",
// and this predicate never asks it.

import (
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
)

// reclaimRunCloneGroundIdentity answers "is this directory the clone channel's
// ground for THIS row?" — the four facts that make a path the row's own without
// consulting the checked-out branch:
//
//	① SHAPE     `.git` is a DIRECTORY (devworkspace.GroundModelOf — the ONE
//	             discriminator, Rules 13/14);
//	② NOT-REPO  it is not the row's own repo (the shared checkout is never this
//	             arm's to blame);
//	③ ORIGIN    it is inside the configured workspace root and exactly one level
//	             deep (cloneWorkspaceDirShape — the ONE test of the channel's
//	             shape, reused rather than re-spelled: a nested scratch home under
//	             the root was measured too wide a test, task 01a16218);
//	④ IDENTITY  it is the path the channel derives from the row's id
//	             (devworkspace.Dir — the ONE producer).
//
// It returns the workspace root and whether all four hold. An unreadable answer
// on ANY of them is false — never a guess (the same contract
// reclaimRunCodeWorktree documents for the linked shape).
func reclaimRunCloneGroundIdentity(taskID, repo, worktree string) (string, bool) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" || strings.TrimSpace(repo) == "" || strings.TrimSpace(worktree) == "" {
		return "", false
	}
	if liveDir(worktree) == "" {
		return "", false // recycled / never created: nothing to measure
	}
	// ① SHAPE — the ONE discriminator (Rules 13/14).
	if devworkspace.GroundModelOf(worktree) != devworkspace.GroundModelClone {
		return "", false
	}
	// ② NOT-REPO — the shared checkout is never this arm's to measure.
	if SameDir(worktree, repo) {
		return "", false
	}
	// ③ ORIGIN — the channel's own tree; an unreadable root is false (never a
	// guess about somebody's directory).
	root, _, rerr := config.WorkspaceRootConfigured()
	if rerr != nil || strings.TrimSpace(root) == "" {
		return "", false
	}
	if !cloneWorkspaceDirShape(worktree, root) {
		return "", false
	}
	// ④ IDENTITY — the channel names the ground by the row's own id.
	if !SameDir(worktree, devworkspace.Dir(root, taskID)) {
		return "", false
	}
	return root, true
}

// reclaimRunCloneGroundWorktree returns the row's own recorded worktree when the
// directory is measurably a CLONE ground of THIS row (identity above, PLUS the
// branch it stands on), and "" otherwise.
func reclaimRunCloneGroundWorktree(taskID, repo, branch, worktree string) string {
	if strings.TrimSpace(branch) == "" {
		return ""
	}
	if _, ok := reclaimRunCloneGroundIdentity(taskID, repo, worktree); !ok {
		return ""
	}
	// ⑤ BRANCH — read from the directory itself (a detached HEAD answers "",
	// which is not the row's branch and is therefore refused).
	if b, err := currentGitBranch(worktree); err != nil || b != branch {
		return ""
	}
	return worktree
}

// reclaimRunCloneGroundUnattributable is this shape's answer to the OTHER leg of
// the close-out check (reclaimRunUnattributableGround): the directory IS the
// row's clone ground by identity, but git will not confirm it stands on the
// row's recorded branch, so it cannot be ATTRIBUTED. It is still worth
// MEASURING, exactly as a registered-but-unattributable linked worktree is, and
// the outcome keeps today's asymmetry: untracked-only residue is BLOCKED
// (recorded as a fact about the ground), a TRACKED change is NOT (that is
// somebody's uncommitted work — the epic's prohibition stands: never weaken and
// never add traffic to the uncommitted-work guards).
func reclaimRunCloneGroundUnattributable(taskID, repo, worktree string) string {
	if _, ok := reclaimRunCloneGroundIdentity(taskID, repo, worktree); !ok {
		return ""
	}
	return worktree
}
