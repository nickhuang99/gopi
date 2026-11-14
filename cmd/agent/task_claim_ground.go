package main

// task_claim_ground.go — the CLAIM-TIME ground stamp (epic branch↔task
// linkage, item [2/5], 2026-09-16).
//
// THE GAP THE SEAM CLOSES: before this, the pair (branch, worktree) reached a
// row through exactly two writers — the executor's CREATION-time detection
// seam (a bash round that created or moved into a linked worktree) and the
// COMPLETION-only backfill (taskBackfillDeliverableBranch, terminal status).
// A row claimed and executing in a worktree its own bash never created (the
// wrapper/verify/resume shapes) therefore named NO ground for the whole
// in-flight window, and a claimed row's deliverable stayed the submit-time
// repo-only placeholder.
//
// THE SEAM: the wrapper that has just claimed the row — and therefore knows it
// is about to execute it — records the ground the run will start in, right
// after it resolves the executor's cwd (cmd/agent/cli_tasks.go
// runTaskRunOnce, the ONE spawn funnel every path shares: worker round,
// manual `task run`, serve tick).
//
// The reference is the ROW'S OWN deliverable.repo, never the caller's cwd: the
// wrapper has already chdir'd into the task's session directory by then
// (taskWorkdirEnter), so a cwd-derived reference would resolve against the
// profile's enclosing tree. See tools.TaskWorktreeStampClaimerFor for the
// resolution and the merge rules.
//
// ADDITIVE BY CONTRACT (the briefing's hard constraint): the stamp shares the
// ONE blob writer/parser (taskDeliverableParse/taskDeliverableRebuild) and the
// ONE worktree key constant (DeliverableWorktreeKey) with every other
// deliverable path, refuses to re-point a row that already names a worktree,
// and CASes its UPDATE on the raw blob — so the completion-time backfill (which
// reads the branch at the CHILD's cwd, memory #2371) stays the authority for a
// terminal row and no second source of truth appears.
//
// Best-effort, like every other side-channel stamp: the caller ignores the
// result — a claim-time ground that cannot be resolved never blocks the run.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// taskClaimGroundStamp stamps the branch + linked worktree the executor child
// is about to start in onto the row that was just claimed. workdir is the
// directory the child will be spawned in (taskExecutorCwd's answer); the row's
// deliverable.repo is the ground it is resolved against.
//
// Returns the resolved (worktree, branch) and whether the row now carries
// them. A row with no deliverable.repo has nothing to pair and returns
// ("", "", false) silently — the same no-op contract every repo-less blob
// writer in internal/tools follows.
func taskClaimGroundStamp(taskID, workdir string) (worktree, branch string, written bool) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", "", false
	}
	repo := tools.TaskDeliverableRepoFor(taskID)
	if repo == "" {
		return "", "", false
	}
	return tools.TaskWorktreeStampClaimerFor(taskID, repo, workdir)
}
