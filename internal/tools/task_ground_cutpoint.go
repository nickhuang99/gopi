package tools

// task_ground_cutpoint.go — THE GROUND'S CUT POINT, ANNOUNCED TO THE CHILD, so a
// criterion phrased against the INTEGRATION BRANCH stops being read as a
// measurement of THIS RUN's work (epic 01a12d66).
//
// THE MEASURED CASE (row 01a12d24-7857-7731-b81f-501c470ab883, 2026-10-12). That
// row's briefing carried the acceptance criterion «`internal/` 零改动
// （`git diff --stat master...HEAD -- internal/` 为空）» and the run had changed
// exactly ONE markdown file — yet the command printed
//
//	11 files changed, 1654 insertions(+), 99 deletions(-)
//
// while the run's OWN diff (`git diff --stat -- internal/`) was empty. The same
// shape was measured again on THIS epic's own fresh ground (2026-10-12, row
// 01a12d66), before its first commit: `git diff --stat master...HEAD --
// internal/` → `19 files changed, 3952 insertions(+), 121 deletions(-)`, while
// `git diff --stat <cut>..HEAD -- internal/` printed nothing.
//
// WHY, and it is a property of how a ground is provisioned rather than of any
// one run: a ground is cut from the PEER's integration-branch tip
// (taskGroundPeerBaseline, epic worktree-base-align), because the local
// integration branch lags the peer's by however long the host has been busy
// (measured: `git rev-list --count master..HEAD` = 55 on that row, 83 here —
// the local `refs/heads/master` sits far behind the commit the ground was cut
// from). `git diff --stat master...HEAD` is a THREE-DOT diff against that stale
// local ref, so it reports everything the ground INHERITED from the peer —
// other rows' merged work — as though this run had authored it. The criterion
// is not hard to satisfy; it measures the wrong thing on every freshly claimed
// ground on a client host.
//
// WHAT IS DELIVERED HERE, and what is deliberately NOT. A row's briefing TEXT is
// the submitter's own prose (cmd/agent/cli_tasks.go's taskRunBriefing
// concatenates the row's summary + payload + attachment index and generates no
// acceptance wording), so there is no generator to mend; what gopi DOES own is
// the ONE seam that appends clauses to a briefing before the child's first model
// round. This file adds the clause that NAMES the baseline: the commit the ground
// was actually cut at, read back from the branch's own reflog — the same
// git-native origin record internal/tools/task_merged_cleanup.go already reads to
// tell "this branch committed" from "this branch never moved" (Rules 8/13: reuse
// the reading, do not invent a second one) — and NO new ref, file, env var or
// knob (Rule 35: the ground's creation already recorded it).
//
// It does NOT rewrite the payload's prose. The clause is ADDITIVE and states the
// baseline; a run that reads a `master...HEAD` criterion is told, in the same
// message, what that criterion is measuring and which spelling answers the
// question it means to ask.

import (
	"fmt"
	"strings"
)

// GroundCutPointRead is the measurement behind the clause: the commit the branch
// was cut at, and the evidence that answered it.
//
// Measured=false is NOT a pass and NOT an empty cut point (Rule 15): it is a
// branch whose origin could not be read, and the clause that renders it says so
// in the same breath rather than letting the integration branch be read as the
// fallback — the exact substitution that produced this epic.
type GroundCutPointRead struct {
	SHA      string // the cut commit, "" when unmeasured
	Evidence string // the ONE git invocation and its exit status
	Note     string // the stated reason, when unmeasured
}

// Measured reports whether the cut point was read off a real git answer.
func (r GroundCutPointRead) Measured() bool { return strings.TrimSpace(r.SHA) != "" }

const (
	// GroundCutPointLabel is the clause's leading marker — spelled once and
	// EXPORTED, so the producer, the seam's record and the regression tests (in
	// this package and in cmd/agent) address ONE string (Rules 1/13/14). It is
	// how a reader greps a briefing for "you told me my baseline".
	GroundCutPointLabel = "[This branch's cut point — the baseline for what THIS RUN changed]"

	// groundCutPointReflogFormat is the reflog field list the read asks for: the
	// commit each recorded position pointed at, and nothing else a human line
	// would have to be parsed out of. Declared rather than typed at the call
	// site, like every other git argument in this package.
	groundCutPointReflogFormat = "--format=%H"

	// groundCutPointMismatchFmt is the SHAPE of the false criterion this clause
	// exists to defuse — the three-dot form against the integration branch as a
	// run meets it in a briefing. Rendered from ONE definition (Rule 1b), so the
	// sentence and any later reader cannot drift apart.
	groundCutPointMismatchFmt = "`git diff --stat %s...HEAD`"

	// groundCutPointCutRangeFmt is the corrected spelling's base half: the
	// `<cut>..HEAD` range every runnable line of the clause is built from.
	groundCutPointCutRangeFmt = "%s..HEAD"
)

// taskGroundCutPointClauseFmt is the clause's ONLY spelling (Rules 1/13/14). Its
// substitutions are the label, the abbreviated cut commit, the integration
// branch, and the ground branch — the integration branch is never typed here
// (IntegrationBranchName is passed in).
//
// WHY IT NAMES THREE SPELLINGS rather than one: the three questions a run asks
// are different — "what did I change (incl. uncommitted)", "what did I commit",
// "what did my one commit change" — and each has a different base. Naming the
// single right answer while the wrong one stays in the briefing is what produced
// this epic; naming all three makes the correction executable without a second
// document.
const taskGroundCutPointClauseFmt = "%s\n" +
	"The integration branch in this tree is %s — and it is NOT your baseline. This ground was cut from the PEER's %s tip, so %s counts every commit the ground INHERITED (other rows' merged work) as though this run had written it: on a freshly claimed ground it is non-empty even for a run that changed nothing (measured 2026-10-12: 19 files / 3952 insertions on this epic's own ground before its first commit, whose own diff was empty).\n" +
	"Your branch was cut at %s. Measure THIS RUN against it:\n" +
	"  git diff --stat %s -- <path>      # this run's COMMITTED changes (empty output = this run changed nothing there)\n" +
	"  git diff --stat -- <path>                # this run's UNCOMMITTED changes (same reading, pre-commit)\n" +
	"  git show --stat <one-commit-sha> -- <path>   # one commit, when this run makes exactly one\n" +
	"A criterion a briefing states against the integration branch — %s — must be re-based on %s before it is read: on this ground it is non-empty by construction and says nothing about this run. The branch is %s.\n"

// taskGroundCutPointUnmeasuredFmt is the clause when the origin could not be
// read: the baseline is NAMED as unmeasured, with the reason, and the run is
// told what to do instead — the shape Rule 15 requires of a record that cannot
// name its subject, and the reason the integration branch is never substituted
// here.
const taskGroundCutPointUnmeasuredFmt = "%s\n" +
	"The integration branch in this tree is %s — and it is NOT your baseline: this ground was cut from the PEER's %s tip, so %s counts the commits the ground INHERITED as though this run had written them.\n" +
	"The commit this branch was cut at could NOT be read (%s) — so no baseline is named here, and you must NOT substitute %s for one. Measure THIS RUN by its own commits instead: `git show --stat <sha>` for each commit this run makes, and `git diff --stat -- <path>` for the uncommitted work. The branch is %s.\n"

// GroundCutPoint reads the commit the ground's branch was cut at, off the
// branch's OWN reflog — the git-native origin record, written when
// `git worktree add -b` created the ref (taskGroundCreateFrom), and already the
// instrument internal/tools/task_merged_cleanup.go uses to tell "this branch
// committed" from "this branch never moved".
//
// The OLDEST reflog entry is the answer, and it is the entry rather than its
// message that answers: the reflog records the commit each position pointed at,
// so the creation entry's COMMIT is the cut point whatever its subject says (a
// `Created from HEAD` subject names a ref, not a commit — the field does not). A
// branch with no reflog, or one this git cannot read, returns unmeasured with the
// evidence that says so; a branch whose reflog was mutated after creation (a
// force-write or a rename — both banned by PROJECT.md Rule 6a) reports the oldest
// RECORDED position, which is the honest reading of a ref nobody promised was
// never reset.
//
// repo is the tree git runs in (the ground's own worktree, so the read follows
// the row rather than the caller's cwd). It reuses the package's ONE bounded git
// runner (mergeVerifyStaleGit) rather than opening a second one (Rules 8/13).
func GroundCutPoint(repo, branch string) GroundCutPointRead {
	r := strings.TrimSpace(repo)
	b := strings.TrimSpace(branch)
	if r == "" || b == "" {
		return GroundCutPointRead{
			Note:     "the ground repo or the branch is not recorded on this row",
			Evidence: "not attempted: repo or branch is empty",
		}
	}
	out, exit, detail := mergeVerifyStaleGit(r, "reflog", "show", groundCutPointReflogFormat, b)
	ev := fmt.Sprintf("git -C %s reflog show %s %s ⇒ exit=%d %s", r, groundCutPointReflogFormat, b,
		exit, mergeBuildGateTail(detail))
	if exit != 0 {
		return GroundCutPointRead{
			Note: fmt.Sprintf("`git reflog show %s` exited %d (%s): this branch's origin was not read, and an unread origin is not an empty cut point",
				b, exit, mergeBuildGateTail(detail)),
			Evidence: ev,
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if sha := strings.TrimSpace(lines[i]); sha != "" {
			return GroundCutPointRead{SHA: sha, Evidence: ev}
		}
	}
	return GroundCutPointRead{
		Note:     fmt.Sprintf("`git reflog show %s` printed no entry: the branch has no reflog here (git may be configured without one), so its cut point cannot be read", b),
		Evidence: ev,
	}
}

// TaskGroundCutPointClause renders the clause for one ground: the baseline as a
// runnable command when it was measured, and the STATED absence when it was not.
// The second return value is the seam's record line — the ONE measurement, never
// a pass.
//
// It returns ("", "") only when there is no ground to speak about (an empty
// branch — a row whose ground names none): a clause naming no subject is worse
// than no clause (Rule 15), and the caller then binds nothing, exactly as the
// announcement and the authority clause do.
func TaskGroundCutPointClause(repo, branch string) (clause, record string) {
	b := strings.TrimSpace(branch)
	if b == "" {
		return "", ""
	}
	read := GroundCutPoint(repo, b)
	rec := fmt.Sprintf("briefing cut point → branch %s: %s (evidence: %s)\n", b, groundCutPointRecordVerdict(read), read.Evidence)
	mismatch := fmt.Sprintf(groundCutPointMismatchFmt, IntegrationBranchName)
	if !read.Measured() {
		return fmt.Sprintf(taskGroundCutPointUnmeasuredFmt,
			GroundCutPointLabel, IntegrationBranchName, IntegrationBranchName, mismatch,
			read.Note, IntegrationBranchName, b), rec
	}
	return fmt.Sprintf(taskGroundCutPointClauseFmt,
		GroundCutPointLabel, IntegrationBranchName, IntegrationBranchName, mismatch,
		shortSha(read.SHA), fmt.Sprintf(groundCutPointCutRangeFmt, shortSha(read.SHA)),
		mismatch, shortSha(read.SHA), b), rec
}

// groundCutPointRecordVerdict is the record's one-word verdict (the run's
// evidence reader greps it), and it never lets "unread" print as "empty".
func groundCutPointRecordVerdict(r GroundCutPointRead) string {
	if r.Measured() {
		return "cut at " + shortSha(r.SHA)
	}
	return "UNMEASURED — " + r.Note
}
