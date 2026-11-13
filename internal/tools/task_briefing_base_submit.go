package tools

// task_briefing_base_submit.go — the SUBMIT-TIME re-measurement of a briefing's
// declared BASE revision, and the warning it appends to the submitter's own
// confirmation.
//
// THE MEASURED CASE (row 01a136a7-dccd, 2026-10-13): a briefing declared
// `base 2f697e5` as the revision its evidence was measured at — 14 minutes
// AFTER the repair it demanded had become the integration branch's tip. The
// sentence was honest when it was first written and was copied into a second row
// verbatim, without re-measuring; the dispatch seam
// (task_briefing_premise.go, commit b0890a420) re-measures the ASSERTION half at
// dispatch, but a base DECLARATION asserts nothing, so nothing disagreed with it
// at the one moment the AUTHOR could still have fixed it — the submit.
//
// THE SHAPE OF THE FIX, and why it is a warning and never a refusal. A base
// revision strictly behind the integration tip is a fact about the briefing's
// AGE, not about the work: a payload may legitimately cite an old base (a
// diagnosis taken there, a row whose premise is exactly "the base was X"), and a
// submit that blocked on it would let a stale-able measurement stop a dispatch.
// So the row queues — always — and the submitter learns BEFORE the row leaves,
// where the fix costs one edit.
//
// THE JUDGEMENT IS NOT RE-IMPLEMENTED HERE (Rules 8/13 — reuse, never a second
// git prober). Two facts are read, each from its OWN existing owner:
//
//	detection   internal/taskreport.BaseClaimsIn — the LEAF scanner the dispatch
//	            seam uses; it yields only a base WORD standing beside a
//	            COMMIT-SHAPED token on one line, so an ordinary payload that
//	            merely says "the base case" yields nothing.
//	measurement MasterClaimEvidenceAt — "is <rev> an ancestor of the integration
//	            branch", whose EXIT STATUS is the verdict, and which resolves the
//	            ref to ask (the delivery lane's mirror when it is demonstrably
//	            ahead, else the local branch). The commits-ahead count is the
//	            existing integrationClaimCountBetween (`git rev-list --count`).
//
// THE POSITIVE CONTROL, and why it is structural rather than an extra check: the
// warning fires on `ancestor AND strictly behind`. A payload whose base IS the
// tip is an ancestor with ZERO commits ahead, so it is silent by construction —
// the ordinary, honest submit is untouched, and a guard that stamped everything
// would be as useless as one that stamped nothing (the same discipline the
// dispatch seam's test carries). Nothing here is a gate: it renders a string.
//
// WHAT IT DELIBERATELY DOES NOT DO: it does not resolve a revision that is not
// an ancestor (a base on a side branch is not "stale", it is a different
// question), it does not fetch, it does not write and it does not refuse.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/taskreport"
)

// TaskSubmitBaseStaleLabel is the warning's leading marker — spelled once,
// exported, so the producer and every assertion read ONE string (Rules 1/13/14).
const TaskSubmitBaseStaleLabel = "[Base revision — a declared base was re-measured at submit]"

// TaskSubmitBaseStaleInstruction is the instruction the warning carries: the
// measure is a fact about the briefing's age, so the reader is told what to DO
// about it rather than handed a verdict about the work. One spelling, like the
// dispatch seam's TaskBriefingPremiseVerifyOnly.
const TaskSubmitBaseStaleInstruction = "RE-MEASURE BEFORE REPAIRING"

// taskSubmitBaseStaleHeadFmt introduces the warning. Substitutions: the label,
// the number of stale declarations found, and the fact that the row still
// queues (a warning is not a refusal — the property this seam must never lose).
const taskSubmitBaseStaleHeadFmt = "%s\n" +
	"warning: %d declared base revision(s) in this payload are already behind the integration branch — " +
	"this row still queues (a warning is never a refusal), but the briefing's measurement point is STALE.\n" +
	"  %s: the state described below is not the integration branch's state as of this submit.\n"

// taskSubmitBaseStaleLineFmt is the per-declaration block: the base (the
// canonical ABBREVIATION of the commit it resolved to — the subject is rendered
// from the RESOLUTION rather than from the author's spelling, so the warning
// names one revision in one spelling however the brief spelled it; the quoted
// payload line below carries the author's text verbatim), the commits-ahead
// count, the tip that count was measured against, the command whose output IS
// that number (so a reviewer can re-run it), and the payload line the
// declaration was read from.
// Its substitutions are the abbreviated base, the count, the tip, the
// reachability-safe base commit, the ref that was measured, the count (again —
// the command's output), and the quoted line.
const taskSubmitBaseStaleLineFmt = "- base %s is %d commit(s) behind %s " +
	"(`git rev-list --count %s..%s` → %d); payload line: %q\n"

// taskSubmitBaseStaleMinAhead is the smallest lead that makes a declared base
// STALE. One commit ahead is already a state in which the briefed measurement
// and the integration branch can disagree — which is the whole failure — and
// zero (base IS the tip) is the positive control that must stay silent. Named
// rather than typed (Rule 1), and reused rather than re-derived: it is
// integrationClaimMinLookahead's criterion, seen from the submit seam.
const taskSubmitBaseStaleMinAhead = integrationClaimMinLookahead

// TaskSubmitBaseStalenessWarning measures every base revision a payload declares
// against repo's integration branch and returns the warning to APPEND to the
// submit confirmation — or "" when there is nothing to say.
//
// repo is the DURABLE repository the row is being filed against (resolved by
// TaskSubmitReturningID before the INSERT, exactly the string stamped into
// deliverable.repo) and payload is the author's prose AFTER the inline spill
// decision, so a payload that became an attachment is judged by the text the
// author wrote — the same columns every other submit-seam warning reads.
//
// It returns "" for: no declaration at all (the ordinary payload — the reason
// this cannot degrade every submit it touches), a declaration that does not
// resolve, one that resolves to a commit which is NOT an ancestor of the
// integration branch (a different question), and the positive control (the
// declared base IS the tip: ancestor with nothing ahead of it). Every arm is
// silence, never an error and never a refusal — the caller appends the string it
// gets and the row queues either way (Rule 15: a measurement that could not be
// taken is not evidence, and is not a warning either).
func TaskSubmitBaseStalenessWarning(repo, payload string) string {
	if strings.TrimSpace(payload) == "" {
		return ""
	}
	claims := taskreport.BaseClaimsIn(payload)
	if len(claims) == 0 {
		return ""
	}
	r := strings.TrimSpace(repo)
	if r == "" {
		// No repository to measure against: nothing can be said about the
		// declaration, and guessing is not an option. Silent by design — a
		// submit on a non-repo cwd is a legitimate path (TaskSubmit stamps such
		// a row without a deliverable), and it is not this warning's business.
		return ""
	}
	type stale struct {
		claim taskreport.BaseClaim
		ev    MasterClaimEvidence
		tip   string
		ahead int
	}
	var found []stale
	for _, c := range claims {
		ev := MasterClaimEvidenceAt(r, c.Commit)
		if !ev.OK() {
			// Not an ancestor (a base on a side branch, an unlanded revision) or
			// not resolvable: neither is the staleness this warning states.
			continue
		}
		ahead, err := integrationClaimCountBetween(r, ev.Commit, ev.ClaimRef)
		if err != nil || ahead < taskSubmitBaseStaleMinAhead {
			// Unmeasurable, or the positive control: the declared base IS the
			// tip (ahead == 0). Both are silence.
			continue
		}
		found = append(found, stale{claim: c, ev: ev, tip: taskSubmitBaseTipShort(r, ev.ClaimRef), ahead: ahead})
	}
	if len(found) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, taskSubmitBaseStaleHeadFmt, TaskSubmitBaseStaleLabel, len(found), TaskSubmitBaseStaleInstruction)
	for _, s := range found {
		fmt.Fprintf(&b, taskSubmitBaseStaleLineFmt,
			s.ev.ShortCommit, s.ahead, taskSubmitBaseOrDash(s.tip),
			s.ev.Commit, s.ev.ClaimRef, s.ahead,
			taskreport.BaseClaimLineExcerpt(s.claim.Line))
	}
	return strings.TrimRight(b.String(), "\n")
}

// taskSubmitBaseTipShort resolves the tip of the ref the measurement was taken
// against, in the report's abbreviation, so the warning can NAME what the base
// is behind. Reuses the existing rev-parse spellings (Rule 13); an unreadable
// ref yields "" and the caller renders a dash rather than inventing a revision.
func taskSubmitBaseTipShort(repo, ref string) string {
	out, err := repoSweepGit(repo, integrationClaimRevParseVerb, integrationClaimRefVerifyFlag,
		integrationClaimRefQuietFlag, ref+integrationClaimPeelSuffix)
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return ""
	}
	return masterClaimShortSha(sha)
}

// taskSubmitBaseOrDash renders an absent measurement the way every other
// confirmation line in this package does — a dash, never an empty gap a reader
// would read as "no tip".
func taskSubmitBaseOrDash(s string) string { return orDash(s) }
