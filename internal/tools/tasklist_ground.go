package tools

// tasklist_ground.go — the GROUND TAIL of a rendered ledger row (2026-10-07):
// the branch and the worktree directory the row's run actually works in, read
// off the row's OWN deliverable blob and printed on the row itself.
//
// WHY IT EXISTS. The naming rule this task lands (a run's ground branch IS the
// row's uuid) is what makes task ↔ branch ↔ worktree cross-comparable — but a
// DERIVED name only helps a reader who can SEE it. Before this tail, answering
// "which branch is that row running on?" took `gopi task show <id>` per row
// (plus a `git worktree list` to turn the directory into a ref), which is the
// per-row round trip the operator reported as his cost. The row is already
// loaded when TaskList renders it (deliverable rides taskSelectColumns), so the
// answer costs one parse and zero extra queries — the list stays a pure read
// (Rule 15's read-path contract: no sweep, no write, no notify).
//
// WHICH ROWS CARRY IT: the LIVE ones (in_progress). A ground is the thing a run
// is standing in RIGHT NOW, and the running suffix beside it already marks that
// set; a pending row has no ground yet and a terminal row's has been recycled,
// so printing theirs would be history the verb does not claim to carry (that
// answer is `task show`, which renders the recorded blob unfiltered).
//
// WHAT IS PRINTED IS WHAT IS RECORDED, never a re-derivation: the branch and
// the directory come from the row's deliverable blob through the SAME readers
// every other consumer uses (TaskDeliverableBranch, DeliverableWorktreePath), so
// the list cannot disagree with the merger, the sweeper or `task show` about
// where a row's ground is (Rules 13/14). A row that records neither renders
// nothing — an empty qualifier is worse than no qualifier (Rule 15).

import "strings"

// The ground tail's spelling, each part named once (Rule 1b/13: the renderer
// and the regression test address the same constants instead of re-typing the
// punctuation beside each other).
const (
	// TaskListGroundLabel is the tail's leading word — the ONE spelling a
	// reader (or a script's grep) matches to find the clause on a rendered row.
	TaskListGroundLabel = "ground"
	// taskListGroundOpen / taskListGroundClose delimit the tail on the row.
	taskListGroundOpen  = " ["
	taskListGroundClose = "]"
	// taskListGroundAt joins the branch to the directory: git's own preposition
	// for "this ref, here" (`git worktree list` prints "<path> <sha> [<ref>]"),
	// spelled as a separator a human reads as "at this directory".
	taskListGroundAt = " @ "
)

// taskListGroundSuffix renders the ground tail for one row: "" for every row
// that is not live, and for a live row that records no ground (nothing to say —
// the run is in the launcher's directory by design, or the row predates the
// stamp).
//
// The two axes are INDEPENDENT, and each renders only when recorded: a run can
// carry a branch and no directory (the claim-moment stamp) or a directory with
// no branch (an orphaned ground whose ref was lost), and inventing the missing
// half from the other would be the reconstruction Rule 15 forbids — the blob is
// the recorded truth.
func taskListGroundSuffix(t *Task) string {
	if t == nil || t.Status != taskStatusInProgress {
		return ""
	}
	branch := TaskDeliverableBranch(t.Deliverable)
	dir := DeliverableWorktreePath(t.Deliverable)
	if branch == "" && dir == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(taskListGroundOpen)
	b.WriteString(TaskListGroundLabel)
	if branch != "" {
		b.WriteString(" " + branch)
	}
	if dir != "" {
		b.WriteString(taskListGroundAt + dir)
	}
	b.WriteString(taskListGroundClose)
	return b.String()
}
