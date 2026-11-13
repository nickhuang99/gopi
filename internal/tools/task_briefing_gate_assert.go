package tools

// task_briefing_gate_assert.go — the DISPATCH-TIME re-anchoring of a briefing's
// "SURFACE is RED on <integration branch>" assertion, a claim shape whose
// MEASUREMENT MOMENT is not in its text at all.
//
// THE MEASURED CASE (row 01a136be-e3b2-71b9-9219-0b4a2258feff, dispatched
// 2026-10-16 08:52:47). Its briefing opened with
//
//	`go test -run TestDocClassBannerMatchesManifest ./internal/standards/` is RED
//	on master, in a file this branch does not touch:
//
// That assertion was TRUE when the row was AUTHORED (2026-10-14 03:30:42; the
// integration branch stood at 82605ae57 and the named file's DOC-INDEX row said
// "historical" while the file did not open with its banner) and it was repaired
// 22 minutes later (f468c3101, 04:49:50, on the branch by 05:35:57). The row was
// dispatched TWO DAYS later with nothing on it saying so: the run spent its first
// moves re-measuring a premise that had been dead since the day it was written,
// and it could only report that AFTER paying for it.
//
// WHY THE TWO SHIPPED HALVES OF THIS SEAM ARE STRUCTURALLY BLIND TO THAT SHAPE
// (measured on this row's OWN briefing text: claims=0 / misses=0 for both, while
// the same seam stamps the sibling spelling `base 2f697e5` — the gap is the
// SPELLING, not the seam):
//
//   - the base half (TaskBriefingPremiseBaseRefresh) fires on a base WORD beside
//     a COMMIT-SHAPED token on one line. This sentence declares no base and names
//     no revision — its subjects are a SURFACE and a MOMENT.
//   - the landed/unlanded half (TaskBriefingPremiseRefresh) fires on a sentence
//     ASSERTING the branch LACKS something. This one asserts the branch HAS a red
//     surface — the opposite direction.
//
// WHAT IS DELIVERED, and what it may NOT conclude. The MOMENT the assertion was
// made is recovered from the ROW's own authoring time (`created_at`, passed in by
// the spawn seam — this function reads no ledger), resolved to a REVISION on the
// integration ref (`git rev-list --first-parent -1 --before=<ts>`), and measured
// for DISTANCE against the ref every other clause here is measured against
// (IntegrationClaimTargetRef, one resolver). A non-zero distance is a fact about
// the briefing's AGE: the clause therefore says RE-MEASURE BEFORE REPAIRING,
// names the surface rather than any verdict, and DENIES the completion reading in
// its own words. A stamp that claimed the defect was gone would be the very
// defect this epic is about, one level up.
//
// THE POSITIVE CONTROL IS STRUCTURAL, not an extra check: an assertion whose
// authoring revision IS the integration tip has distance ZERO and stamps
// nothing — so a briefing written moments ago is untouched, and the arm is
// RECORDED (Rule 15) rather than silently skipped.
//
// NO NEW MECHANISM (Rule 35): no tool, no script, no resident anything. One
// clause on the seam that already exists, one ledger accessor beside the
// existing ones, reading an integration ref, a first-parent walk and a distance
// count through the package's OWN git runner (repoSweepGit) and its OWN ancestry
// prober (MasterClaimEvidenceAt) — Rules 8/13: reuse the reading, never mint a
// second one.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/taskreport"
)

// TaskBriefingGateAssertLabel is the stamped clause's leading marker — spelled
// once, exported, so the producer, the spawn seam's record and the regression
// tests (in this package and in cmd/agent) all read ONE string (Rules 1/13/14).
const TaskBriefingGateAssertLabel = "[Briefing freshness — a GATE ASSERTION this briefing makes was anchored to the ROW's authoring time at dispatch]"

// TaskBriefingGateAssertReMeasure is the instruction the clause carries, and it
// is deliberately a THIRD spelling rather than a reuse of its siblings: the base
// clause's RE-MEASURE BEFORE REPAIRING is about a revision the author NAMED,
// while this one is about a fact (the row's age) the author never mentioned at
// all — so the child is told which anchor produced the reading.
const TaskBriefingGateAssertReMeasure = "RE-MEASURE THE SURFACE BEFORE REPAIRING"

// gateAssertRevListVerb / gateAssertFirstParentFlag / gateAssertSingleFlag /
// gateAssertBeforeFlag render the ONE command that recovers the revision the
// integration branch stood at when the row was authored. Each is named here
// because it is both RUN (through repoSweepGit) and PRINTED into the clause as
// the child's re-runnable evidence — one spelling, so the two cannot drift
// (Rules 1/13).
const (
	gateAssertRevListVerb     = integrationClaimRevListVerb
	gateAssertFirstParentFlag = "--first-parent"
	gateAssertSingleFlag      = "--max-count=1"
	gateAssertBeforeFlag      = "--before="
)

// gateAssertMinAhead is the smallest distance that makes an assertion STALE — the
// same criterion its sibling halves use (integrationClaimMinLookahead, seen from
// this anchor): one commit past the authoring revision is already a state in
// which the briefed measurement and the branch can disagree, and zero is the
// positive control that must stay silent.
const gateAssertMinAhead = integrationClaimMinLookahead

// taskBriefingGateAssertHeadFmt introduces the clause. Substitutions: the label,
// the number of assertions re-anchored, the integration branch name (twice — the
// subject of the assertions, and the branch that moved).
const taskBriefingGateAssertHeadFmt = "%s\n" +
	"This briefing asserts that %d surface(s) are RED — on %s, or on an unmodified TREE STATE the assertion itself names — and names NO revision its measurements were taken at, " +
	"so the moment each assertion was made was anchored by the ROW's own authoring time instead — and each was measured JUST NOW, " +
	"against THIS RUN's own repo, before the briefing was handed to you. Every one of them was written at a revision that %s has since moved past.\n"

// gateAssertBranchlessAnchorFmt renders the anchor of a BRANCH-LESS assertion —
// one whose verdict is bound to a tree state ("pristine") instead of to the
// integration branch's name. It is the honest spelling of what the author
// asserted: the surface is red on an UNMODIFIED CHECKOUT OF the integration
// branch, which is a different, narrower statement than "red on master" (nothing
// has landed there), and the child is told which one it is being asked to
// re-measure. Substitutions: the author's own anchor word, the integration
// branch name.
const gateAssertionBranchlessAnchorMarker = "TREE STATE"

const gateAssertBranchlessAnchorFmt = "the " + gateAssertionBranchlessAnchorMarker +
	" %q the assertion names (an unmodified checkout of %s)"

// gateAssertAnchorPhrase renders the anchor a claim was bound to: the integration
// branch's own name for a branch-anchored assertion, the tree-state phrase for a
// branch-less one. ONE place decides this, so the clause's prose and its quoted
// line can never disagree about what was asserted (Rules 13/14).
func gateAssertAnchorPhrase(claimBranch string) string {
	if claimBranch == "" || claimBranch == IntegrationBranchName {
		return IntegrationBranchName
	}
	return fmt.Sprintf(gateAssertBranchlessAnchorFmt, claimBranch, IntegrationBranchName)
}

// taskBriefingGateAssertClaimFmt is the clause's per-assertion block: the
// surface, the authoring moment (rendered AND as the epoch the command takes),
// the revision that moment resolves to, the distance the branch has moved, the
// three measurements behind that reading, the instruction, the explicit denial
// of the completion reading, and the quoted briefing line.
//
// Its substitutions, in order: the surface; the integration branch name; the
// authoring time rendered; the authoring epoch (command operand); the ref the
// authoring revision was resolved on; the abbreviated authoring revision (twice:
// the subject and the ancestry operand); the integration branch name; the
// distance; the abbreviated authoring revision (the distance command's start);
// the ref (its end); the distance again as that command's own output; the
// ancestry exit status; the instruction constant; and the quoted line.
const taskBriefingGateAssertClaimFmt = "- %q is asserted RED on %s as of the row's authoring time (%s — epoch %d). " +
	"That moment resolves to `%s` on %s, and %s has moved %d commit(s) PAST it " +
	"(`git %s %s %s %s%d %s` → %s; `git merge-base --is-ancestor %s %s` → exit=%d; `git %s %s %s%s%s` → %d).\n" +
	"  %s: the assertion was written at an older revision, so THIS RUN must measure the surface again HERE — run the command it names, quote its output, and repair the surface only if it still reproduces. If it is already green, report that instead of repairing it.\n" +
	"  This clause is NOT evidence that the work is done: a moved branch is a fact about the briefing's AGE, never about the defect.\n" +
	"  briefing line: %q\n"

const (
	// taskBriefingGateAssertUnreadableRecordFmt is the record line for an
	// assertion the scanner SAW but could not attribute to a surface: a verdict
	// word on the integration branch with no surface-shaped token beside it. It
	// states the line and the reason — a HOLE IN THE INSTRUMENT, not a fact
	// about the briefing (Rule 15). Nothing is measured and nothing is stamped:
	// the line exists so this silence is distinguishable from the silence of a
	// briefing that asserts no such thing (measured shape: "names its measurement
	// point as a branch/worktree and asserts \"<surface> is RED on master\"." —
	// real prose, no surface to re-measure).
	taskBriefingGateAssertUnreadableRecordFmt = "briefing gate assertion → UNREADABLE assertion (%s): %s — briefing line: %q\n"
	// taskBriefingGateAssertStaleRecordFmt / ...CurrentRecordFmt /
	// ...UnmeasRecordFmt are the record lines for every assertion examined: the
	// STAMPED one, the POSITIVE CONTROL (the row was authored AT the integration
	// tip, which by design stamps nothing), and the UNMEASURED arms — each stated
	// rather than read as a pass (Rule 15). One spelling each (Rule 1).
	taskBriefingGateAssertStaleRecordFmt   = "briefing gate assertion → %q as of %s (%s) is %d commit(s) BEHIND %s (exit=%d) — STAMPED into the child's briefing (%s)\n"
	taskBriefingGateAssertCurrentRecordFmt = "briefing gate assertion → %q is anchored at %s, which IS %s (exit=%d, 0 commit(s) behind) — the row was authored at the branch's own tip, so the assertion stands as written and nothing is stamped\n"
	taskBriefingGateAssertUnmeasRecordFmt  = "briefing gate assertion → UNMEASURED (%s) — %q was NOT compared with %s, which is not a pass: %s\n"
)

// taskBriefingGateAssertReasons are the stated reasons an anchoring is
// unavailable. Named, because a bare sentence at a fmt site is the shape Rule 1
// forbids, and because the seam's record is the only place a reader learns WHY an
// assertion went unmeasured.
const (
	gateAssertNoRepoReason       = "there is no repo to resolve the authoring moment against"
	gateAssertNoAuthoredReason   = "the row's authoring time (created_at) was not read, so the moment the assertion was made resolves to no revision"
	gateAssertAuthoredTipReason  = "the integration branch had no commit at or before the row's authoring time, so the authoring revision was not read"
	gateAssertNotAncestorReason  = "the revision the row was authored at is not an ancestor of the integration branch, so the two are not comparable by distance"
	gateAssertDistanceErrReason  = "the distance from the authoring revision to the integration tip could not be read"
	gateAssertUnreadableReason   = "the line states a RED verdict on the integration branch but names no surface-shaped token (a fenced run, a test-function name, a repo path or a revision), so there is nothing to send the child back to"
	gateAssertAlreadyStampedRsn  = "the briefing already carries a gate-assertion anchor (an earlier pass appended it); no assertion was re-anchored and nothing was appended"
	gateAssertAlreadyStampedText = "(this briefing's own previously appended gate-assertion clause)"
)

// TaskBriefingGateAssertRefresh re-anchors, at dispatch time, every assertion a
// briefing makes that a SURFACE is RED on the integration branch, and returns the
// clause to APPEND to the briefing the child receives plus the record lines for
// every assertion examined.
//
// repo is the run's own ground (its linked worktree) and authoredAt is the ROW's
// created_at in epoch seconds — the moment the author wrote the text, and the
// only anchor such a sentence carries. Both are passed IN rather than resolved
// here: the spawn seam already holds the row and the ground, and this function
// stays a pure text+measurement unit a test can drive against a real git
// repository with no ledger at all.
//
// Returns ("", nil) when the briefing asserts no such thing — the ordinary case,
// and the reason this cannot degrade every briefing it touches. Every assertion
// examined leaves a record line, INCLUDING the positive control (an authoring
// moment at the branch's tip) and the arms that measured nothing (Rule 15). It
// never returns an error and never blocks a run.
func TaskBriefingGateAssertRefresh(repo string, authoredAt float64, briefing string) (clause string, record []string) {
	// Same idempotence contract as its two siblings, same reason: the clause
	// QUOTES the briefing line it contradicts, and that quote carries the very
	// verdict word and branch name the scanner looks for. A second pass over a
	// stamped briefing must append nothing and SAY so (a resume, a re-dispatch,
	// an operator paste).
	if strings.Index(briefing, TaskBriefingGateAssertLabel) >= 0 {
		return "", []string{fmt.Sprintf(taskBriefingGateAssertUnmeasRecordFmt,
			gateAssertAlreadyStampedRsn, gateAssertAlreadyStampedText, IntegrationBranchName, taskBriefingPremiseAlreadyStampedExcerpt)}
	}
	claims := taskreport.GateAssertionsIn(briefing, IntegrationBranchName)
	misses := taskreport.GateAssertionMissesIn(briefing, IntegrationBranchName)
	if len(claims) == 0 && len(misses) == 0 {
		return "", nil
	}
	// The assertions this scanner could NOT read are STATED FIRST, and they are
	// never measured: a line with no surface names nothing to send the child back
	// to (Rule 15).
	for _, m := range misses {
		record = append(record, fmt.Sprintf(taskBriefingGateAssertUnreadableRecordFmt,
			gateAssertUnreadableReason, m.Branch, taskreport.GateAssertionLineExcerpt(m.Line)))
	}
	if len(claims) == 0 {
		return "", record
	}
	r := strings.TrimSpace(repo)
	type anchored struct {
		claim   taskreport.GateAssertion
		ev      MasterClaimEvidence
		ref     string
		ahead   int
		moment  string
		epochIn int64
	}
	var stamped []anchored
	for _, c := range claims {
		excerpt := taskreport.GateAssertionLineExcerpt(c.Line)
		if r == "" {
			record = append(record, fmt.Sprintf(taskBriefingGateAssertUnmeasRecordFmt,
				gateAssertNoRepoReason, excerpt, IntegrationBranchName, excerpt))
			continue
		}
		if authoredAt <= 0 {
			// A row whose created_at was not read has NO anchor: guessing one
			// (now, the claim time, the cut point) would produce a reading about
			// a moment the author never wrote at, and a stamp built on it would
			// be a false positive on an honest briefing (Rule 15).
			record = append(record, fmt.Sprintf(taskBriefingGateAssertUnmeasRecordFmt,
				gateAssertNoAuthoredReason, excerpt, IntegrationBranchName, excerpt))
			continue
		}
		epoch := int64(authoredAt)
		ref := IntegrationClaimTargetRef(r)
		tip, err := gateAssertRevisionAt(r, ref, epoch)
		if err != nil || tip == "" {
			reason := gateAssertAuthoredTipReason
			if err != nil {
				reason = fmt.Sprintf("%s: %v", gateAssertAuthoredTipReason, err)
			}
			record = append(record, fmt.Sprintf(taskBriefingGateAssertUnmeasRecordFmt,
				reason, excerpt, IntegrationBranchName, excerpt))
			continue
		}
		ev := MasterClaimEvidenceAt(r, tip)
		if !ev.OK() {
			reason := gateAssertNotAncestorReason
			if ev.Err != "" {
				reason = ev.Err
			}
			record = append(record, fmt.Sprintf(taskBriefingGateAssertUnmeasRecordFmt,
				reason, excerpt, IntegrationBranchName, excerpt))
			continue
		}
		ahead, err := integrationClaimCountBetween(r, ev.Commit, ev.ClaimRef)
		if err != nil {
			record = append(record, fmt.Sprintf(taskBriefingGateAssertUnmeasRecordFmt,
				fmt.Sprintf("%s: %v", gateAssertDistanceErrReason, err), excerpt, IntegrationBranchName, excerpt))
			continue
		}
		if ahead < gateAssertMinAhead {
			// THE POSITIVE CONTROL, in the wild: the row was authored AT the
			// integration tip, so the assertion describes the branch as it is and
			// nothing is stamped. A guard that stamped this arm would fire on
			// every freshly-authored row.
			record = append(record, fmt.Sprintf(taskBriefingGateAssertCurrentRecordFmt,
				excerpt, formatTaskTime(float64(epoch)), ev.ClaimRef, ev.AncestorExit))
			continue
		}
		record = append(record, fmt.Sprintf(taskBriefingGateAssertStaleRecordFmt,
			excerpt, formatTaskTime(float64(epoch)), ev.ShortCommit, ahead, ev.ClaimRef, ev.AncestorExit, ev.ShortCommit))
		stamped = append(stamped, anchored{
			claim: c, ev: ev, ref: ev.ClaimRef, ahead: ahead,
			moment: formatTaskTime(float64(epoch)), epochIn: epoch,
		})
	}
	if len(stamped) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, taskBriefingGateAssertHeadFmt, TaskBriefingGateAssertLabel, len(stamped), IntegrationBranchName, IntegrationBranchName)
	for _, s := range stamped {
		fmt.Fprintf(&b, taskBriefingGateAssertClaimFmt,
			s.claim.Surface, gateAssertAnchorPhrase(s.claim.Branch),
			s.moment, s.epochIn,
			s.ev.Commit, s.ref,
			IntegrationBranchName, s.ahead,
			gateAssertRevListVerb, gateAssertFirstParentFlag, gateAssertSingleFlag, gateAssertBeforeFlag, s.epochIn, s.ref, s.ev.ShortCommit,
			s.ev.ShortCommit, s.ref, s.ev.AncestorExit,
			gateAssertRevListVerb, integrationClaimCountFlag, s.ev.ShortCommit, baseClaimRangeSep, s.ref, s.ahead,
			TaskBriefingGateAssertReMeasure,
			taskreport.GateAssertionLineExcerpt(s.claim.Line))
	}
	return strings.TrimRight(b.String(), "\n"), record
}

// gateAssertRevisionAt resolves the revision the integration ref stood at when
// the row was AUTHORED — `git rev-list --first-parent -1 --before=<epoch> <ref>`,
// the first-parent walk that makes the reading follow the branch's own line
// rather than whichever merge happened to land in the same second. The walk is
// read through the package's ONE git runner (repoSweepGit) and the revision is
// returned WHOLE; "" with a nil error is the stated "the ref had no commit at or
// before that moment" case, which the caller reports rather than guesses around.
func gateAssertRevisionAt(repo, ref string, epoch int64) (string, error) {
	out, err := repoSweepGit(repo, gateAssertRevListVerb, gateAssertFirstParentFlag,
		gateAssertSingleFlag, gateAssertBeforeFlag+strconv.FormatInt(epoch, 10), ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
