// task_deliverable_grounds.go — the SECOND-GROUND axis of a row's deliverable.
//
// THE GAP (measured 2026-09-26, row 01a0ddc4-c410, session 3592a5ef). A run's
// deliverable ground is single-valued on purpose: the primary worktree/branch
// are the row's own ground and NO detection seam may rebind them (the
// 2026-09-12 foreign-owner hijack — a probe of another task's directory
// re-pointed a row and made it a foreign owner). But a run legitimately creates
// MORE than one ground, and the incident is the shape: the row already named
// .../gopi-01a0ddc4… on task/01a0ddc4…, and the run then minted a SECOND
// worktree for its evidence
// (`git worktree add /home/nick/workspace/gopi-evi-pushback -b fix/pushback-rule31-evidence`).
// That directory ended up in NO record at all:
//
//	the row          — the primary axes were taken and the writer refuses to
//	                   overwrite them (correctly: never rebind);
//	the creation seam — its candidate loop RETURNED at the FIRST candidate that
//	                   resolved (the run's own ground, which is candidate #1 by
//	                   construction), so the created directory was never probed;
//	the run anchor    — TaskGroundAnchorRecord is called from a seam that had
//	                   already returned, so no ground anchor was written either
//	                   (measured: the sink held 154 anchors and NOTHING for
//	                   /home/nick/workspace/gopi-evi-pushback).
//
// The consequence was an owner-less live ground: the branch→row lookup
// fix/pushback-rule31-evidence` answered "no owner" while the branch sat in a
// registered linked worktree (tip 42fc17d6), and `gopi worker --resume
// --branch B` had no row to resume (the wording half of this fix is 64b6952d;
// this file is the producer-side cause).
//
// THE REPAIR is a PLACE for the fact, not a new store: the axis rides the ONE
// deliverable blob the ledger already keeps (Rule 35 — no new file, key store or
// daemon), written by the SAME blob writer family (one parser, one rebuild, one
// CAS — Rules 13/14), and read by the SAME deliverable→row scan `for-branch`
// already uses. The primary axes stay authoritative and are never rebound: the
// writer below adds an ENTRY, it never rewrites the row's own ground.
package tools

import (
	"context"
	"strings"
	"time"
)

// groundListed reports whether raw's deliverable already carries the ground
// (branch/worktree) as a SECONDARY entry. One reader for the writer's dedup and
// for the lookup's matcher, so the two cannot disagree about what "listed" means
// (Rule 14).
func taskDeliverableGroundListed(d taskDeliverable, worktree, branch string) bool {
	for _, g := range d.Grounds {
		if strings.TrimSpace(branch) != "" && strings.TrimSpace(g.Branch) == strings.TrimSpace(branch) {
			return true
		}
		if strings.TrimSpace(worktree) != "" && sameWorkingPath(g.Worktree, worktree) {
			return true
		}
	}
	return false
}

// taskBackfillDeliverableGrounds records a ground a run CREATED as a SECONDARY
// entry on the row — the axis a row had nowhere to put until now.
//
// It is deliberately NOT the claim/creation stamp: it never writes the primary
// axes, never rebinds them, and every refusal below is a silent no-op by
// contract (the caller is a detection seam that must never fail a run). What it
// refuses, and why each refusal is correct:
//
//	same as the primary ground  the row already names this directory/branch;
//	                            a second entry would be a duplicate of the
//	                            authoritative axis (the for-branch scan reads
//	                            the primary anyway).
//	already listed              idempotent re-stamp (the seam fires per round).
//	repo-less blob / no row     nothing to attach to (the same rule the other
//	                            stamps use).
//
// The write is the family's ONE shape: parse the blob, mutate ONE axis, rebuild
// through the ONE rebuild (which preserves the other axes — attachments and any
// ground already recorded), and CAS the UPDATE on the raw blob read back, so a
// concurrent writer cannot be clobbered.
func taskBackfillDeliverableGrounds(taskID, worktree, branch string) error {
	taskID = taskIDNormalize(taskID)
	worktree = strings.TrimSpace(worktree)
	branch = strings.TrimSpace(branch)
	if taskID == "" || (worktree == "" && branch == "") {
		return nil
	}
	repo := TaskDeliverableRepoFor(taskID)
	if repo == "" {
		return nil // repo-less row — no ground reference to pair against
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return err
	}
	if t == nil {
		return nil // row gone — nothing to attach the ground to
	}
	raw := strings.TrimSpace(t.Deliverable)
	if raw == "" {
		return nil // no deliverable blob — nothing to extend
	}
	d := taskDeliverableParse(raw)
	if d.Repo == "" {
		return nil
	}
	// THE PRIMARY AXIS WINS, ALWAYS: a "second" ground that IS the row's own
	// ground is not a second ground. Compared on both axes with the family's own
	// path comparator (a differently-spelled path is still the same directory).
	if worktree != "" && d.Worktree != "" && sameWorkingPath(d.Worktree, worktree) {
		return nil
	}
	if branch != "" && d.Branch != "" && d.Branch == branch {
		return nil
	}
	if taskDeliverableGroundListed(d, worktree, branch) {
		return nil // already recorded — one entry per ground
	}
	d.Grounds = append(d.Grounds, taskDeliverableGround{Repo: repo, Branch: branch, Worktree: worktree})
	merged := taskDeliverableRebuild(d)
	if merged == raw {
		return nil
	}
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ? AND deliverable = ?`,
		merged, float64(time.Now().Unix()), taskID, raw)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		taskWorktreeStampLog(taskDeliverableGroundRecordedFmt, taskID, worktree, branch)
	}
	return nil
}

// taskDeliverableGroundRecordedFmt is the ONE spelling of the line an operator
// reads when a second ground lands on a row. It names the row, the directory and
// the branch, because that triple is what the operator re-reads with
// `gopi task show <id>` (Rule 15 — a record nobody can re-derive is not an
// explanation).
const taskDeliverableGroundRecordedFmt = "task %s: deliverable.grounds recorded a SECOND ground the run created: %s (branch %s)"

// taskDeliverableGroundsOf is the parse-side reader the deliverable→row scans
// use: the secondary grounds of a raw blob (nil for a blob that carries none, a
// non-blob, or a repo-less row — every one of which is "no second ground", not
// an error).
func taskDeliverableGroundsOf(raw string) []taskDeliverableGround {
	return taskDeliverableParse(raw).Grounds
}

// taskDeliverableGroundProvenance renders the sentence a lookup attaches to an
// owner row it matched through a SECONDARY ground: the row does not name the
// asked branch/worktree on its primary axes, and a reader who checks with
// `gopi task show <id>` would otherwise see a worktree/branch that does not
// match the question. It names both facts — the ground matched, and the row's
// own ground — so the answer is re-derivable rather than surprising (Rule 15).
//
// It takes the row's two primary values plus the ASKED value, so the sentence is
// built from what the caller already read rather than from a second parse of the
// blob at a second site (Rules 13/14).
func taskDeliverableGroundProvenance(rowWorktree, rowBranch, asked string) string {
	own := strings.TrimSpace(rowWorktree)
	if own == "" {
		own = "(none recorded)"
	}
	if b := strings.TrimSpace(rowBranch); b != "" {
		own += " on " + b
	}
	return "secondary ground: this run CREATED " + strings.TrimSpace(asked) + "; the row's own ground is " + own
}

// taskDeliverableGroundLine renders ONE secondary ground for `task show`, in the
// same shape the primary axes print (worktree first, then " on " + branch; the
// repo only when it differs from the row's own, since a second ground of another
// repo is the interesting case and the row's repo is already on its own line).
// It is the renderer's ONE spelling of a ground line (Rule 13/14) — a caller that
// greps the output reads it from TaskShowDeliverableGroundsLabel, never a copy.
func taskDeliverableGroundLine(g taskDeliverableGround) string {
	wt := strings.TrimSpace(g.Worktree)
	if wt == "" {
		wt = "(no directory recorded)"
	}
	if b := strings.TrimSpace(g.Branch); b != "" {
		wt += " on " + b
	}
	if r := strings.TrimSpace(g.Repo); r != "" {
		wt += "  [repo " + r + "]"
	}
	return wt
}
