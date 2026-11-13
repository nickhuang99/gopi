package tools

// task_briefing_census_revision.go — the REVISION every dispatch-seam census
// was read at, stated BESIDE the clauses that quote it.
//
// THE GAP THIS CLOSES (epic 01a1abcd, 2026-11-05). The seam's acceptance clauses
// quote MEASUREMENTS — "this ground counts 3114 top-level test function(s) in
// ./internal/tools/", "7 of these path(s) do not resolve inside this ground" —
// and those measurements were taken by READING THIS GROUND'S TREE. A tree is not
// a constant: the child commits into it, a sibling landing moves it, and a
// re-dispatch reads a different one. So a count written into a child's briefing
// with no revision beside it is an observation wearing the clothes of a standing
// fact — exactly the shape the seam's base / gate-assertion clauses were built to
// correct one level up, and the shape the filer's own report measured: a number
// that was TRUE when it was read and that nothing re-tests afterwards.
//
// WHAT CHANGES, AND WHAT DOES NOT. Nothing about WHICH names reach a test and
// which do not — that judgement is the parcels' own (the sixth and seventh
// clauses) and is untouched. What is added is the anchor: the ground's HEAD
// commit, read ONCE per dispatch, so a reader can ask "3107 vs 3114 — at which
// revision?" and answer it. When the revision cannot be read, the clause
// SAYS SO with the command's own exit and stderr — an unread revision is stated,
// never omitted (Rule 15: a measurement without a date must not read like one
// with a date).
//
// COST (Rule 35's discipline). ONE `git rev-parse --short HEAD` per DISPATCH —
// not one per clause — through the package's ONE bounded git runner
// (mergeVerifyStaleGit, task_staleness_guard.go; Rules 8/13: no second runner,
// no second timeout). Measured on this host: three invocations in 4 ms. The
// ordinary briefing stamps no clause and therefore pays nothing at all: the call
// site sits behind the "did anything stamp?" guard, not in front of it.
//
// EXPORTED SPELLING (Rules 1/13/14). The one sentence a reader must recognise —
// the anchor's own opening words — is a constant HERE, so the producer, its
// regression arm and any future clause that quotes a census read ONE string.

import (
	"fmt"
	"strings"
)

// TaskBriefingCensusRevisionAnchor is the opening every stamped census carries.
// Spelled once: an arm that greps for "MEASURED AT" is reading this constant,
// not a second literal that can drift from it.
const TaskBriefingCensusRevisionAnchor = "MEASURED AT"

// taskBriefingCensusRevisionFmt is the measured arm: the revision, the ground it
// was read in, and what a reader must do when the two have parted.
const taskBriefingCensusRevisionFmt = TaskBriefingCensusRevisionAnchor + " %s — every count quoted by the stamped clauses ABOVE was read from the tree in %s at that revision. A count is a MEASUREMENT, not a standing fact: if this ground has moved since (the run's own commits, a sibling landing, a re-dispatch), re-read the census HERE before you treat a number above as today's answer — and re-derive the REPLACEMENT command with it, because a selector that was right at %s may be wrong at the current HEAD.\n"

// taskBriefingCensusRevisionUnreadFmt is the STATED arm (Rule 15): the census was
// taken, but the revision it was taken at could not be read, so the counts stand
// as measurements without a date rather than as facts with one.
const taskBriefingCensusRevisionUnreadFmt = TaskBriefingCensusRevisionAnchor + " AN UNREAD REVISION (%s) — the counts this clause quotes were read from this ground's tree, but WHICH commit that tree stood on could not be resolved, so they are measurements without a date and must not be quoted as today's.\n"

// taskBriefingCensusRevisionRecordFmt is the seam record line — the ONE reading,
// never a pass.
const taskBriefingCensusRevisionRecordFmt = "briefing census revision → %s\n"

const (
	// briefingCensusRevisionNoGroundReason is the reason a revision is unread:
	// the clause was stamped with no ground to read it in.
	briefingCensusRevisionNoGroundReason = "there is no ground to read the census revision from"
	// briefingCensusRevisionUnreadReasonFmt carries the git command's own exit
	// and stderr into the reason, so the reader can reproduce it.
	briefingCensusRevisionUnreadReasonFmt = "`%s` did not resolve a commit"
	// briefingCensusRevisionEmptyReason is the reason a git that exited 0 while
	// printing nothing leaves: an empty revision is not a revision.
	briefingCensusRevisionEmptyReason = "`%s` exited 0 but printed no revision"
)

// TaskBriefingCensusRevisionClause renders the anchor a stamped census carries.
// It is a SEPARATE append (not a prefix baked into each clause) on purpose: one
// reading of the ground's HEAD serves every census this dispatch stamped, so the
// cost is one `git rev-parse` per DISPATCH rather than one per clause, and the
// anchor states the fact once instead of repeating it three times in a prompt.
//
// The caller appends it only when at least one census was stamped — a briefing
// that carried no measurement has nothing to date, and an anchor over nothing is
// noise a reader learns to skip.
func TaskBriefingCensusRevisionClause(repo string) (clause, record string) {
	return briefingCensusRevisionLine(repo)
}

// TaskBriefingCensusRevisionLabel introduces the anchor in the dispatch log, so a
// round can be read back without re-deriving which revision its census quoted.
const TaskBriefingCensusRevisionLabel = "briefing census revision"

// TaskBriefingCensusRevisionLineFmt is the seam's own marker for the anchor. The
// flag rides the ONE append decision (tools.BriefingWithClause), so the marker
// and the appended bytes cannot disagree.
const TaskBriefingCensusRevisionLineFmt = "briefing census revision stamped → %s\n"

// briefCensusRevisionOneLine flattens git's multi-line stderr into one line: the
// reason sentence lives inside a paragraph a child reads, and an embedded newline
// splits one finding into two that look like separate findings.
func briefCensusRevisionOneLine(s string) string {
	return strings.Join(strings.Fields(mergeBuildGateTail(s)), " ")
}

// briefingCensusRevisionLine renders the anchor for one stamped clause: the
// clause's own first line, plus the seam's record line. Both come from ONE call
// so the text the child reads and the line the round log carries cannot disagree
// about which revision was measured (the contract BriefingWithClause keeps for
// the appended bytes).
func briefingCensusRevisionLine(repo string) (anchor string, record string) {
	r := strings.TrimSpace(repo)
	if r == "" {
		return fmt.Sprintf(taskBriefingCensusRevisionUnreadFmt, briefingCensusRevisionNoGroundReason),
			fmt.Sprintf(taskBriefingCensusRevisionRecordFmt, briefingCensusRevisionNoGroundReason)
	}
	out, exit, detail := mergeVerifyStaleGit(r, briefingCensusRevisionGitVerb, briefingCensusRevisionGitFlag, briefingCensusRevisionGitRef)
	// ONE LINE, always: this sentence lands in a child's prompt, and an embedded
	// newline from git's stderr splits one finding into two paragraphs that read
	// like two findings.
	ev := fmt.Sprintf("git -C %s %s %s %s ⇒ exit=%d %s", r,
		briefingCensusRevisionGitVerb, briefingCensusRevisionGitFlag, briefingCensusRevisionGitRef,
		exit, briefCensusRevisionOneLine(detail))
	rev := strings.TrimSpace(out)
	if exit != 0 {
		reason := fmt.Sprintf(briefingCensusRevisionUnreadReasonFmt, ev)
		return fmt.Sprintf(taskBriefingCensusRevisionUnreadFmt, reason),
			fmt.Sprintf(taskBriefingCensusRevisionRecordFmt, reason)
	}
	if rev == "" {
		reason := fmt.Sprintf(briefingCensusRevisionEmptyReason, ev)
		return fmt.Sprintf(taskBriefingCensusRevisionUnreadFmt, reason),
			fmt.Sprintf(taskBriefingCensusRevisionRecordFmt, reason)
	}
	return fmt.Sprintf(taskBriefingCensusRevisionFmt, rev, r, rev),
		fmt.Sprintf(taskBriefingCensusRevisionRecordFmt, fmt.Sprintf("commit %s of %s (evidence: %s)", rev, r, ev))
}

// The git invocation this anchor reads, spelled once (Rule 1): the ground's own
// HEAD, never a branch name — a ground may stand on a `task/<id>` ref, a detached
// HEAD or the integration branch, and the census was read from whatever was
// checked out.
const (
	briefingCensusRevisionGitVerb = "rev-parse"
	briefingCensusRevisionGitFlag = "--short"
	briefingCensusRevisionGitRef  = "HEAD"
)
