package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// THE GROUND-IN-USE CRITERION — the ONE reading of "somebody is standing in a
// row's recorded ground RIGHT NOW" (Rules 13/14: one implementation, no second
// probe).
//
// WHY IT EXISTS (measured 2026-10-07, row 01a11561; the defect's whole shape).
// A row's ledger entry and the directory its run works in are written by
// DIFFERENT parties: the row's holder columns by the claim/start path, the
// deliverable.worktree by the executor's own ground-detection seam.
//
// THE HOLDER HALF WAS FIXED, THE GROUND HALF WAS NOT (2026-10-29). The resume
// stamp used to be pinned `WHERE status='in_progress'`, so on a `failed` row the
// ownership handoff matched ZERO rows and the row kept reading `holder: (none)`
// while a run executed it. Since the two-level resume policy
// (task_run_retry.go — a failure judges one ATTEMPT, and level 1 is to resume
// it) `failed` is admitted and the stamp MOVES the row back to in_progress in
// the same write, so that silent no-op is gone.
//
// What remains — and it is the reason THIS file exists — is a fact the ledger
// columns cannot express even now: a live gopi process standing in the row's
// RECORDED GROUND and writing files in it. The status axis cannot see it (the
// row may legitimately be failed and resumable), and the holder axis cannot see
// it either (the ground's occupant is not always the row's stamped holder). A
// directory with a live process in it is not free, so the ground axis answers
// that question by itself — which is exactly what the surfaces below read.
//
// WHAT THE READER MUST NOT DO. The answer is NOT "resume, because the ledger
// says nothing holds the row": the row's own record names the directory, and a
// directory with a live process in it is not free. The three surfaces that act
// on this question — `gopi task status`/`gopi session resume`, the retry
// gate's resumability verdict, and (already, independently) every removal arm —
// must therefore agree.
//
// WHAT WAS ALREADY SAFE, MEASURED (so nobody re-fixes it). The REMOVAL arms
// never had this hole: the worktree sweep's rung ② asks
// `scratch.HeldByProcesses(reg.Path, holders)` over LiveHolders (cwd/fd/exe/maps
// of every process on the host) and the ledger-keyed reclaim arm asks
// `finishCleanupWorktreeInUse` (guard ③), so a live process standing in a
// registration's directory keeps it on both paths. Measured on this host
// 2026-10-07: `gopi task sweep-worktrees --dry-run` printed `keep
// [self-checkout]` / `keep [run-ground-anchor]` / `keep … held by a live
// process` for all three live grounds and `collectable` only for grounds whose
// run was provably gone. The gap was in the STATUS/RESUME reading alone, and
// this file closes it on the SAME evidence the removal arms use rather than
// with a second, drifting probe (memory #2632: a second liveness probe is how a
// live worktree gets deleted).
//
// THE CRITERION, and its two deliberate boundaries:
//
//	· a live process whose working directory lies in the ground, judged by the
//	  shared cwd scan (finishCleanupWorktreeInUse) — never a new /proc walk;
//	· minus the ORPHANED cwd-inheritor reading (finishCleanupOrphanCwdInheritorReason),
//	  the SAME input every removal arm pairs with that scan, so a directory whose
//	  entire occupancy is the wreckage of an already-dead run stays free — the
//	  reported shape of TaskGroundInUse is therefore exactly runGroundOccupied's,
//	  and the two can never disagree.
//
// UNMEASURABLE IS NOT FREE (Rule 15a): an unreadable /proc answers "free" here
// only in the same sense it answers every other arm, and the row's verdict is
// what carries the refusal — this predicate never manufactures a "nobody is
// there" from a scan that could not run.

const (
	// taskGroundInUseNoHolder is the explicit absence marker: the ground holds
	// no live genuine holder, so the caller renders nothing (never a blank).
	taskGroundInUseNoHolder = ""
)

// TaskGroundInUse reports the live holder standing in one GROUND directory, as
// (holder, reason) — both "" when the directory holds no genuine holder, and
// both "" when there is nothing to judge (an empty path, or a directory that no
// longer exists: a vanished ground has no occupation to report).
//
// The two returned strings are the SAME pair the removal arms carry into their
// refusal lines (finishCleanupHolderWho's "pid N (comm) argv=…" and the
// "worktree in use by live process …: cwd …" sentence), so an operator reading
// `gopi task status` sees the identical evidence a refused sweep would print —
// one reading, one sentence (Rule 14).
func TaskGroundInUse(dir string) (holder, reason string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return taskGroundInUseNoHolder, taskGroundInUseNoHolder
	}
	if !taskPathExists(dir) {
		return taskGroundInUseNoHolder, taskGroundInUseNoHolder
	}
	holder, reason = finishCleanupWorktreeInUse(dir)
	if reason == "" {
		return taskGroundInUseNoHolder, taskGroundInUseNoHolder
	}
	// The orphan reading is consulted ONLY on a refusal — the same order (and
	// the same all-or-nothing acceptance) every other arm uses: one
	// unclassifiable leftover is still a holder, so this rung can only ever
	// RELAX a refusal, never invent one.
	if _, orphanWhy := finishCleanupOrphanCwdInheritorReason(dir); orphanWhy != "" {
		return taskGroundInUseNoHolder, taskGroundInUseNoHolder
	}
	return holder, reason
}

// taskGroundInUseForRow answers the same question for ONE ledger row: it reads
// the row's RECORDED ground (deliverable.worktree) and asks TaskGroundInUse
// about it. A row that records no worktree, or whose recorded directory is
// gone, has no ground to occupy — both are explicit absences, never a guess
// (Rule 15).
func taskGroundInUseForRow(deliverable string) (holder, reason string) {
	return TaskGroundInUse(taskDeliverableWorktree(deliverable))
}

// TaskStampDeliverableGroundForTest writes ONE row's deliverable.worktree — the
// seam a caller needs to build the terminal-row shape this criterion exists for,
// because the deliverable is written in production by the executor's own backfill
// (taskBackfillDeliverableWorktree, unexported) and a test that constructs a row
// without running an executor has no other way to name its ground. It writes the
// SAME shape the production writer writes ({"repo","worktree"}, merged into
// whatever the row already carries) and is keyed on the row's id; a row that does
// not exist is an error, never a silent no-op (Rule 15).
//
// Precedent for the seam: MergeVerifySetDeliverableBranchForTest
// (verify_gate.go) and runFinishCleanupChainForTest — an unexported production
// body exposed to the layer that must exercise it.
func TaskStampDeliverableGroundForTest(taskID, worktree string) error {
	taskID = taskIDNormalize(taskID)
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("task: stamp ground: empty task id")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Errorf("task: stamp ground %s: %w", taskID, err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&raw); err != nil {
		return fmt.Errorf("task: stamp ground %s: read the row: %w", taskID, err)
	}
	spec := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		// A blob this process wrote itself; an undecodable one means the row is
		// not the shape the caller thinks it is, and overwriting it silently
		// would destroy another writer's field (Rule 15: refuse, name it).
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			return fmt.Errorf("task: stamp ground %s: existing deliverable is not decodable (%w)", taskID, err)
		}
	}
	spec["worktree"] = worktree
	blob, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("task: stamp ground %s: encode: %w", taskID, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ?`,
		string(blob), float64(time.Now().Unix()), taskID); err != nil {
		return fmt.Errorf("task: stamp ground %s: %w", taskID, err)
	}
	return nil
}
