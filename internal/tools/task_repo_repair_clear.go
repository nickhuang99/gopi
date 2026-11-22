package tools

// task_repo_repair_clear.go — the FIRST half of the deliberate TWO-step move
// that re-points a row's recorded ground. It exists because the refusal it
// answers (TaskRepairGroundResetRefusalFmt, the 2026-09-12 foreign-owner guard)
// used to send its reader to "clear the recorded ground first" while NO verb,
// flag or code path could clear one — the eleven `gopi task` verbs carry no
// `clear`, and `update` had no `--clear-ground` (epic 01a1fc52).
//
// WHY TWO STEPS AND NOT ONE (this is the whole design):
//
//   - The guard that refuses a one-step re-point is CORRECT. A row's recorded
//     ground is single-owner by construction: the merger's attribution, the
//     sweep's ownership arm and the finish-cleanup recycle all read it, and a
//     rebind performed by whatever directory the executor last touched is
//     exactly the 2026-09-12 hijack (row 01a09250's deliverable flipped from
//     gopi-wtrecycle2 to gopi-cov-agent mid-run, wedging the ownership gate for
//     both rows). Nothing here weakens it.
//   - So the move is split into an EDIT that REMOVES the claim and an EDIT that
//     MAKES a new one, each with its own evidence. Between them the row carries
//     no ground at all, which is a state every reader already handles ("this row
//     has no recorded ground" — the shape a freshly submitted row is born in).
//   - A clear is never silent: `--reason` is REQUIRED (refused by name when
//     absent) and the append-only trail records the reason together with both
//     values, old → new. Clearing a ground changes WHERE a row's work lives, so
//     an unexplained clear is an audit hole of the same class the ledger's
//     append-only rule exists to prevent.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// GroundClearedEventName is the event under which a ground clear is recorded in
// the append-only task_events trail. One spelling, referenced by the writer and
// by the tests (Rules 1b/13).
//
// It is deliberately NOT a member of verdictTrailEventNames (a clear is not a
// judgment on the row) and IS effectively in the contentRevision family's
// spirit — its detail is the row's comparative history (old → new) — but it is
// kept out of contentRevisionEventNames for the reason those members are IN it:
// that list suppresses the `result:` render, and a clear carries one short line
// whose presence on the row's render is useful. The detail is small by
// construction (two paths and a branch name), so it does not bury a run digest.
const GroundClearedEventName = "ground_cleared"

// TaskGroundClearedLabel / TaskGroundClearedNoneLabel are the two state words of
// the clear's confirmation — the state phrase a reader (a test, a script)
// references instead of re-typing it (Rule 1b/13).
const (
	// TaskGroundClearedLabel is the state phrase of a performed clear.
	TaskGroundClearedLabel = "the recorded ground is CLEARED — the row now records none"
	// TaskGroundClearedNoneLabel is the no-op state: there was nothing to clear,
	// reported rather than answered with a silent success (Rule 15).
	TaskGroundClearedNoneLabel = "the row records no ground — nothing to clear"
	// TaskGroundClearedReasonFirst is the refusal shape used when `--reason` is
	// missing: it names the flag, the reason it is required and the exact
	// invocation that satisfies it.
	TaskGroundClearedReasonFirst = "task repair: %s: clearing a recorded ground must carry %s — the trail records WHY the row moved, " +
		"and an unexplained clear of the axis the merger and the sweep read is what the ledger's append-only rule exists to prevent; " +
		"re-run as `" + cliverbs.Program + " " + cliverbs.Task + " update %s " + cliverbs.TaskClearGroundFlag + " " + cliverbs.TaskReasonFlag + " \"<why the ground moved>\"`"
	// TaskGroundClearedMovedFmt renders the confirmation: the row, what the
	// ground WAS, and what it is now (old → new, never a bare "ok").
	TaskGroundClearedMovedFmt = "task %s deliverable.worktree/branch: %s / %s -> (none) — " + TaskGroundClearedLabel + "; " +
		"stamp the new one with `" + cliverbs.Program + " " + cliverbs.Task + " update %s " + TaskRepairRepoVerb + " <path>`"
	// TaskGroundClearedWriteErrFmt — the ONE writer DECLINED or lost the CAS.
	TaskGroundClearedWriteErrFmt = "task repair: the row's deliverable did not take the clear (the blob moved under the CAS) — " +
		"re-read the row and re-run; the trail entry is rolled back with it"
)

// TaskClearDeliverableGround removes a row's recorded ground pair
// (deliverable.worktree + deliverable.branch) through the ONE deliverable blob
// writer, recording WHY on the append-only trail together with both old values.
//
// It is the first step of the two-step move the ground refusal names: after it
// the row records no ground, so the SECOND step (`--repo <path>` naming a live
// registered linked worktree) stamps a fresh one through the never-rebind guard
// — which stays exactly as strict as it was, because there is nothing left for
// it to re-bind.
//
// The repo axis is untouched: this clears WHERE the work lives, never WHERE the
// project is. The secondary `grounds` list and the attachments manifest survive
// the rewrite (the same parse/rebuild contract every deliverable writer obeys).
func TaskClearDeliverableGround(taskID, reason string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: task_id must not be empty")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf(TaskGroundClearedReasonFirst, taskID, cliverbs.TaskReasonFlag, taskID)
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
	parsed := taskDeliverableParse(raw)
	if parsed.Worktree == "" && parsed.Branch == "" {
		// Nothing to clear. Reported as its own state rather than a silent
		// success or a refusal: the request is ALREADY satisfied, and an
		// operator who typed the flag deserves to know the row was born that way
		// (Rule 15 — a no-op that cannot be told apart from a performed write is
		// a lie about the ledger).
		return fmt.Sprintf("task %s: %s — %s", taskID, TaskGroundClearedNoneLabel, TaskGroundClearedMovedHint(taskID)), nil
	}
	// The clear drops the ground PAIR only; repo, secondary grounds and the
	// attachment manifest ride the same rebuild (taskDeliverableRebuild carries
	// every field it was given, Rule 13/14 — there is no second JSON writer).
	merged := taskDeliverableRebuild(taskDeliverable{
		Repo:        parsed.Repo,
		Grounds:     parsed.Grounds,
		Attachments: parsed.Attachments,
	})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("task repair: clear: begin: %w", err)
	}
	// The rollback is the defer's whole job; after Commit it is a documented
	// no-op, and a teardown failure would replace the real cause.
	defer func() { _ = tx.Rollback() }() //stylegate:ignore the commit below owns the verdict; a rollback failure would mask the error being returned
	// THE TRAIL FIRST: the values being removed are captured from the row that
	// still holds them, and one transaction means a crash cannot separate the
	// record from the write it describes.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		taskID, GroundClearedEventName, groundClearedDetail(parsed.Worktree, parsed.Branch, reason), float64(time.Now().Unix())); err != nil {
		return "", fmt.Errorf("task repair: clear: audit trail: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ? AND deliverable = ?`,
		merged, float64(time.Now().Unix()), taskID, raw)
	if err != nil {
		return "", fmt.Errorf("task repair: clear: %w", err)
	}
	if n, rerr := res.RowsAffected(); rerr != nil || n == 0 {
		// The CAS lost (or the row vanished): refuse rather than clobber, and the
		// trail entry rolls back with it (Rule 15 — never claim a clear that did
		// not land).
		return "", fmt.Errorf(TaskGroundClearedWriteErrFmt)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("task repair: clear: commit: %w", err)
	}
	return fmt.Sprintf(TaskGroundClearedMovedFmt, taskID, parsed.Worktree, parsed.Branch, taskID), nil
}

// TaskGroundClearedMovedHint is the ONE half-sentence that names the SECOND step
// of the move — shared by the no-op note above and by the refusal text, so an
// operator who is told "there is nothing to clear" still learns how to stamp a
// ground (Rule 15: every exit names the next real door).
func TaskGroundClearedMovedHint(taskID string) string {
	return "stamp one with `" + cliverbs.Program + " " + cliverbs.Task + " update " + taskID + " " + TaskRepairRepoVerb + " <path>`"
}

// groundClearedDetail renders the append-only trail entry for one clear: the
// reason, then the values being removed, each labelled so a later reader can
// recover what the row recorded before the move without re-deriving it. The
// values are included VERBATIM and an absent one renders as the same "(none)"
// every other comparative record uses (Rule 1b: `""` and `-` must not silently
// mean the same thing on two different lines).
func groundClearedDetail(worktree, branch, reason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "reason: %s\n", reason)
	fmt.Fprintf(&b, "old worktree: %s\n", reparentSetLabel(nonEmpty(worktree)))
	fmt.Fprintf(&b, "old branch: %s\n", reparentSetLabel(nonEmpty(branch)))
	fmt.Fprintf(&b, "new worktree: %s\n", reparentNoneLabel)
	fmt.Fprintf(&b, "new branch: %s", reparentNoneLabel)
	return b.String()
}

// nonEmpty turns one value into the zero-or-one-element slice reparentSetLabel
// takes, so the absent case renders through the ONE "(none)" spelling instead of
// a second literal (Rule 13).
func nonEmpty(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return []string{v}
}
