package tools

// task_briefing_premise.go — the DISPATCH-TIME re-measurement of a briefing's
// "not landed on the integration branch" premise, and the stamp it writes into
// the briefing the child actually receives.
//
// THE MEASURED CASE (row 01a12c7b, 2026-10-12): an epic briefing was rendered
// from a SNAPSHOT of the integration branch taken when the row was queued (base
// 305ae03d0, 03:37:55) and asserted that a completed row's fix had never reached
// master. The identical fix (9867ed461) became an ancestor of origin/master at
// 03:44:05, and the run was handed the briefing at 03:58 — so the run spent its
// budget re-implementing LANDED work and then had to deduplicate against a
// branch the briefing had not told it about. Nothing in the pipeline disagreed
// with the sentence, because a briefing's text was the only thing asserting it.
//
// THE SHAPE OF THE FIX, and why it is here: the sentence "X is not on master" is
// a MEASURABLE statement about the object graph, but it is measured at the wrong
// MOMENT (submit time) inside a process that has no way to notice it went stale.
// This file re-measures it at the ONE spawn seam every executor face funnels
// through (cmd/agent/cli_tasks.go, runTaskRunOnce), against THE RUN'S OWN GROUND,
// and appends a stamped clause to the briefing string that becomes the child's
// prompt. The judgement is NOT re-implemented here: it is
// internal/tools/master_claim.go's MasterClaimEvidenceAt — "is <rev> an ancestor
// of the integration branch", whose EXIT STATUS is the verdict (Rules 8/13:
// reuse, never a second git prober).
//
// WHAT IT IS NOT: not a refusal. A premise that can no longer be measured, or
// that resolves to a genuinely unlanded revision, leaves the briefing untouched
// and goes to the record as a STATED line (Rule 15) — the run proceeds. And it
// never stamps an unlanded revision: a guard that stamps everything is as
// useless as one that stamps nothing (the positive control in the test file
// exists for exactly that).

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/taskreport"
)

// TaskBriefingPremiseLabel is the stamped clause's leading marker — spelled
// once, exported, so the producer, the spawn seam's record and the regression
// tests (in this package and in cmd/agent) all read ONE string (Rules 1/13/14).
const TaskBriefingPremiseLabel = "[Briefing freshness — a premise was RE-MEASURED at dispatch]"

// TaskBriefingPremiseVerifyOnly is the instruction the stamp carries for an
// already-landed revision, spelled once for the same reason: a reader that
// greps for it, a test that asserts it and the text the child receives cannot
// drift apart.
const TaskBriefingPremiseVerifyOnly = "VERIFY ONLY"

// taskBriefingPremiseClaimFmt is the clause's per-claim block: the revision, the
// REF the measurement was taken against (master_claim.go's claim target, passed
// in — never re-derived), the measured command and exit status, the verdict, and
// what to do about it. Its substitutions are the abbreviated commit, that ref
// (three times: the verdict's subject, the command's target, the remedy), the
// exit status, and the quoted briefing sentence.
const taskBriefingPremiseClaimFmt = "- %s IS an ancestor of %s (`git merge-base --is-ancestor %s %s` → exit=%d). " +
	"The premise was true when this briefing was written and is FALSE now: the work is already on %s.\n" +
	"  %s: do NOT re-implement it, do not re-land it and do not treat it as an open gap. Verify the landed state and spend this run's budget on the part that is genuinely missing — a duplicate change has to be deduplicated against the branch this briefing did not know about.\n" +
	"  briefing sentence: %q\n"

// taskBriefingPremiseHeadFmt introduces the clause. The second substitution is
// the integration branch name (the CLAIM's subject, always), and the ref each
// individual claim was measured against is named per claim below — the two can
// differ, because the claim target falls back to the peer lane's mirror on a
// checkout whose local integration branch trails it (task 01a12d30).
const taskBriefingPremiseHeadFmt = "%s\n" +
	"This briefing asserts the integration branch (%s) does NOT have %d revision(s) it names. " +
	"Each was re-measured JUST NOW, against THIS RUN's own repo, before the briefing was handed to you — and each one IS already an ancestor of the ref named for it.\n"

// taskBriefingPremiseLandedRecordFmt / ...NotLandedRecordFmt / ...UnmeasuredRecordFmt
// are the record lines the seam prints for every claim it examined: the stamp's
// own evidence, readable where the run's facts are read (never in the child's
// prompt — a record is not an instruction). One spelling each (Rule 1).
// taskBriefingPremiseAlreadyStampedReason is the stated reason the seam writes
// when it measures a briefing that ALREADY carries a stamp: nothing was appended
// and the earlier measurement is left exactly as it stands (Rule 15 — and the
// reason this seam is idempotent).
const taskBriefingPremiseAlreadyStampedReason = "the briefing already carries a freshness stamp (an earlier pass appended it); no claim was re-measured and nothing was appended"

// THE BASE-REVISION CLAUSE — the SECOND measurement this seam takes, for the
// claim shape unlandedclaim.go cannot see.
//
// THE MEASURED CASE (row 01a136a7-dccd, 2026-10-14): a briefing declared its own
// base — "on master, `gopi tool doccheck` exits 1 (row #1212, base 2f697e5)" —
// and demanded a repair of the frozen permit table. The repair (34693afb3) is
// the DIRECT CHILD of that base: it landed 14 minutes after it and 20 minutes
// before the row was authored, so master's tip WAS the repair while the briefing
// still called the surface red. The base declaration is the only part of such a
// briefing that is a claim about the object graph rather than prose, and no
// assertion phrase exists in it for the landed/unlanded scanner to match — which
// is why this half is measured by commit DISTANCE instead.
//
// TaskBriefingPremiseBaseLabel heads the clause, spelled once and exported for
// the same reason as its sibling above (Rules 1/13/14): the producer, the spawn
// seam's record and the regression tests read ONE string.
const TaskBriefingPremiseBaseLabel = "[Briefing freshness — the BASE REVISION this briefing names was RE-MEASURED at dispatch]"

// TaskBriefingPremiseBaseVerifyOnly is the instruction the base clause carries.
// It names the ONLY honest consequence of a moved base: the measurement the
// briefing reports was taken at an older revision, so the SURFACE must be
// measured again — never that the work is done. A stamp that claimed completion
// on the strength of a moved base would be the defect this clause exists to
// prevent, one level up.
const TaskBriefingPremiseBaseVerifyOnly = "RE-MEASURE BEFORE REPAIRING"

// baseClaimRangeSep spells the git range operator once: it is read by the
// distance probe and rendered in the command the child is told to re-run, and
// the two must not drift (Rules 1/13).
const baseClaimRangeSep = ".."

// taskBriefingPremiseBaseHeadFmt introduces the base clause. Its substitutions
// are the label, the number of bases re-measured, and the integration branch
// name. Note what the head does NOT say: nothing here asserts the briefing's
// defect is gone.
const taskBriefingPremiseBaseHeadFmt = "%s\n" +
	"This briefing names %d revision(s) as the base its own measurements were taken at, and each was re-measured JUST NOW, against THIS RUN's own repo, before the briefing was handed to you — every one of them is BEHIND %s.\n"

// taskBriefingPremiseBaseClaimFmt is the clause's per-base block: the base, its
// distance behind the integration tip, the two measurements behind that reading
// (the ancestry probe's exit status and the commit count), the instruction, the
// explicit denial of the completion reading, and the quoted briefing line. Its
// substitutions are the abbreviated base (three times: the subject, the ancestry
// probe's target, the distance command's start), the distance, the ref (four
// times: the subject, the probe's ref, the distance command's end, and the
// remedy), the exit status, the distance again as the command's output, the
// instruction constant, and the quoted line.
const taskBriefingPremiseBaseClaimFmt = "- the base this briefing names, `%s`, is %d commit(s) BEHIND %s " +
	"(`git merge-base --is-ancestor %s %s` → exit=%d; `git rev-list --count %s%s%s` → %d).\n" +
	"  %s: the surface this briefing calls red was measured at the older revision — run THAT surface again HERE, quote its output, and repair it only if it still reproduces. If it is already green, report that instead of repairing it.\n" +
	"  This clause is NOT evidence that the work is done.\n" +
	"  briefing line: %q\n"

const (
	// taskBriefingPremiseBaseUnreadableRecordFmt is the record line for the
	// declaration the scanner REFUSED: a base word standing beside a
	// revision-LENGTH token the shape predicate does not accept. It states the
	// token, the reason and the line — a HOLE IN THE INSTRUMENT, not a fact
	// about the briefing (Rule 15). Nothing here is measured and nothing is
	// stamped: the line exists so this silence is distinguishable from the
	// silence of a briefing that declares no base at all.
	taskBriefingPremiseBaseUnreadableRecordFmt = "briefing base → UNREADABLE declaration outside the seam's predicate (token %q): %s — briefing line: %q\n"
	// taskBriefingPremiseBaseStaleRecordFmt / ...CurrentRecordFmt /
	// ...UnmeasRecordFmt are the record lines for every base examined: the
	// STAMPED one, the positive control (a base that IS the integration tip,
	// which by design stamps nothing), and the UNMEASURED arms — each stated
	// rather than read as a pass (Rule 15). One spelling each (Rule 1).
	taskBriefingPremiseBaseStaleRecordFmt   = "briefing base → %s is %d commit(s) BEHIND %s (exit=%d) — STAMPED into the child's briefing (%s)\n"
	taskBriefingPremiseBaseCurrentRecordFmt = "briefing base → %s IS an ancestor of %s and has nothing after it (exit=%d, 0 commit(s) behind) — the briefing's base is the tip, so the briefing stands and nothing is stamped\n"
	taskBriefingPremiseBaseUnmeasRecordFmt  = "briefing base → UNMEASURED (%s) — the base was NOT compared with %s, which is not a pass: %s\n"
)

// taskBriefingPremiseBaseReasons are the stated reasons a base reading is
// unavailable. Named, because a bare sentence at a fmt site is the shape Rule 1
// forbids, and because the seam's record is the only place a reader learns WHY a
// base went unmeasured.
const (
	taskBriefingPremiseBaseNoRepoReason      = "there is no repo to measure the base against"
	taskBriefingPremiseBaseNotAncestorReason = "the named base is not an ancestor of the integration branch, so the briefing is not describing an older tip"
	taskBriefingPremiseBaseDistanceErrReason = "the distance behind the integration tip could not be read"
	// taskBriefingPremiseBaseUnreadableReason is the stated reason a
	// declaration was REFUSED rather than measured: the base word was found, but
	// the token beside it is revision-LENGTH and not revision-SHAPED (a revision
	// carries both a hex letter and a digit), so the predicate cannot tell it
	// from a count or an English word and there is nothing to measure.
	taskBriefingPremiseBaseUnreadableReason = "the base word stands beside a revision-LENGTH token the shape predicate refuses (a revision carries both a hex letter and a digit), so there is nothing to measure"
)

// TaskBriefingPremiseBaseRefresh re-measures, at dispatch time, every revision a
// briefing declares as the base its own measurements were taken at, and returns
// the clause to APPEND to the briefing the child receives plus the record lines
// for every base examined.
//
// It is the SIBLING of TaskBriefingPremiseRefresh and lives in the same file for
// the same reason: this is the ONE spawn seam every executor face funnels
// through, the measurement is taken against the run's OWN ground, and the
// judgement is not re-implemented here (master_claim.go owns "is <rev> an
// ancestor of the integration branch"). The TWO differ in what they measure and
// in what they may conclude:
//
//   - its sibling fires on a sentence ASSERTING the branch lacks something, and
//     its stamp says VERIFY ONLY — the named work is already on the branch;
//   - this one fires on a base DECLARATION, which asserts nothing of the kind,
//     and its stamp says RE-MEASURE BEFORE REPAIRING — a base behind the tip
//     means the briefing's measurement is older than the branch, NOT that the
//     defect is gone. That is the whole distinction the epic's guard doctrine
//     requires, and it is why this half can never claim completion.
//
// Returns ("", nil) when the briefing declares no base revision — the ordinary
// case, and the reason this cannot degrade every briefing it touches. Every base
// examined leaves a record line, INCLUDING the positive control (a base that IS
// the current tip) and the arms that measured nothing (Rule 15). It never
// returns an error and never blocks a run.
func TaskBriefingPremiseBaseRefresh(repo, briefing string) (clause string, record []string) {
	// Same idempotence contract as its sibling, same reason: the clause QUOTES
	// the briefing line it contradicts, and that quote carries the very
	// "base <rev>" tokens the scanner looks for. A second pass over a stamped
	// briefing must append nothing and SAY so (a resume, a re-dispatch, an
	// operator paste).
	if i := strings.Index(briefing, TaskBriefingPremiseBaseLabel); i >= 0 {
		return "", []string{fmt.Sprintf(taskBriefingPremiseBaseUnmeasRecordFmt,
			taskBriefingPremiseAlreadyStampedReason, IntegrationBranchName, taskBriefingPremiseAlreadyStampedExcerpt)}
	}
	claims := taskreport.BaseClaimsIn(briefing)
	misses := taskreport.BaseClaimMissesIn(briefing)
	if len(claims) == 0 && len(misses) == 0 {
		return "", nil
	}
	r := strings.TrimSpace(repo)
	// The declarations this scanner could NOT read are STATED FIRST, and they are
	// never measured: a refused token names no revision to measure (Rule 15). It
	// is the half that closes the epic's silent ambiguity — without it, a refused
	// declaration and a briefing that declares no base at all produce the SAME
	// nothing (row 01a13715: a CJK base word glued to its revision and an
	// upper-case revision were both silent in the pre-fix predicate; the two
	// shapes are now READ, and this arm is what keeps the next unreadable
	// spelling audible instead of silent).
	for _, m := range misses {
		record = append(record, fmt.Sprintf(taskBriefingPremiseBaseUnreadableRecordFmt,
			m.Token, taskBriefingPremiseBaseUnreadableReason, taskreport.BaseClaimLineExcerpt(m.Line)))
	}
	if len(claims) == 0 {
		return "", record
	}
	// ONE measurement per base, kept beside its evidence: the stamp renders the
	// SAME measurement the record reports, so the child's instruction and the
	// run's evidence cannot disagree (Rules 8/13 — measure once, read twice).
	type behind struct {
		claim taskreport.BaseClaim
		ev    MasterClaimEvidence
		ahead int
	}
	var stamped []behind
	for _, c := range claims {
		excerpt := taskreport.BaseClaimLineExcerpt(c.Line)
		if r == "" {
			record = append(record, fmt.Sprintf(taskBriefingPremiseBaseUnmeasRecordFmt,
				taskBriefingPremiseBaseNoRepoReason, IntegrationBranchName, excerpt))
			continue
		}
		ev := MasterClaimEvidenceAt(r, c.Commit)
		if !ev.OK() {
			reason := ev.Err
			if reason == "" {
				reason = taskBriefingPremiseBaseNotAncestorReason
			}
			record = append(record, fmt.Sprintf(taskBriefingPremiseBaseUnmeasRecordFmt,
				reason, IntegrationBranchName, excerpt))
			continue
		}
		ahead, err := baseClaimCommitsAhead(r, ev.Commit, ev.ClaimRef)
		if err != nil {
			record = append(record, fmt.Sprintf(taskBriefingPremiseBaseUnmeasRecordFmt,
				fmt.Sprintf("%s: %v", taskBriefingPremiseBaseDistanceErrReason, err), IntegrationBranchName, excerpt))
			continue
		}
		if ahead == 0 {
			// THE POSITIVE CONTROL, in the wild: the base the briefing names IS
			// the integration tip, so the briefing's measurement is current and
			// nothing is stamped. A guard that stamped this arm would fire on
			// every freshly-cut briefing (the seam's own control exists for it).
			record = append(record, fmt.Sprintf(taskBriefingPremiseBaseCurrentRecordFmt,
				ev.ShortCommit, ev.ClaimRef, ev.AncestorExit))
			continue
		}
		record = append(record, fmt.Sprintf(taskBriefingPremiseBaseStaleRecordFmt,
			ev.ShortCommit, ahead, ev.ClaimRef, ev.AncestorExit, ev.ShortCommit))
		stamped = append(stamped, behind{claim: c, ev: ev, ahead: ahead})
	}
	if len(stamped) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, taskBriefingPremiseBaseHeadFmt, TaskBriefingPremiseBaseLabel, len(stamped), IntegrationBranchName)
	for _, s := range stamped {
		fmt.Fprintf(&b, taskBriefingPremiseBaseClaimFmt,
			s.ev.ShortCommit, s.ahead, s.ev.ClaimRef,
			s.ev.ShortCommit, s.ev.ClaimRef, s.ev.AncestorExit,
			s.ev.ShortCommit, baseClaimRangeSep, s.ev.ClaimRef, s.ahead,
			TaskBriefingPremiseBaseVerifyOnly, taskreport.BaseClaimLineExcerpt(s.claim.Line))
	}
	return strings.TrimRight(b.String(), "\n"), record
}

// baseClaimCommitsAhead reads how many commits the integration ref has that the
// base does not — the distance a base declaration is behind the tip, and the
// figure the stamp renders. It is read through the SAME git runner the ancestry
// probe uses (repo_sweep.go's repoSweepGit: one timeout, one environment), and
// an unreadable distance is an ERROR the caller STATES rather than a convenient
// zero — a zero here would read as "the base is current" and silence the clause
// on exactly the briefings it exists for (Rule 15).
func baseClaimCommitsAhead(repo, base, ref string) (int, error) {
	out, err := repoSweepGit(repo, "rev-list", "--count", base+baseClaimRangeSep+ref)
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return 0, fmt.Errorf("git rev-list --count %s%s%s printed %q: %w",
			base, baseClaimRangeSep, ref, strings.TrimSpace(out), convErr)
	}
	return n, nil
}

const (
	taskBriefingPremiseLandedRecordFmt = "briefing premise → %s IS an ancestor of %s (exit=%d) — STAMPED into the child's briefing (%s)\n"
	taskBriefingPremiseOpenRecordFmt   = "briefing premise → %s is NOT an ancestor of %s (exit=%d) — measured open; the briefing stands and nothing is stamped\n"
	taskBriefingPremiseUnmeasRecordFmt = "briefing premise → UNMEASURED (%s) — the premise was NOT checked, which is not a pass: %s\n"
)

// TaskBriefingPremiseRefresh re-measures, at dispatch time, every "X is not on
// the integration branch" claim the briefing body makes, and returns the clause
// to APPEND to the briefing the child receives plus the record lines for every
// claim examined.
//
// repo is the run's own ground (its linked worktree, or the row's repo). It is
// passed IN rather than resolved here, so the caller keeps ONE resolution (the
// row's deliverable, already resolved by the spawn seam) and so this function
// stays a pure text+measurement unit that a test can drive against a real git
// repository with no ledger at all.
//
// Returns ("", nil) when the briefing makes no such claim — the ordinary case,
// and the reason this cannot degrade every briefing it touches. An empty repo is
// NOT a silent no-op when claims exist: every claim is then reported UNMEASURED
// (Rule 15). The function never returns an error and never blocks a run.
func TaskBriefingPremiseRefresh(repo, briefing string) (clause string, record []string) {
	// MEASURE THE AUTHOR'S TEXT, NEVER OUR OWN STAMP. The clause QUOTES the
	// briefing sentence it contradicts — and that quote carries the very tokens
	// the detector looks for ("never landed on master") — so a plain re-read of a
	// STAMPED briefing re-fires on itself (measured while writing this: the
	// second pass produced a clause quoting the FIRST clause's own quote).
	// Stripping from the label onward makes a second pass append NOTHING and SAY
	// so, which is what "additive" has to mean for a briefing handed over twice
	// (a resume, a re-dispatch, an operator paste).
	if i := strings.Index(briefing, TaskBriefingPremiseLabel); i >= 0 {
		return "", []string{fmt.Sprintf(taskBriefingPremiseUnmeasRecordFmt,
			taskBriefingPremiseAlreadyStampedReason, taskBriefingPremiseAlreadyStampedExcerpt)}
	}
	claims := taskreport.UnlandedClaimsIn(briefing, IntegrationBranchName)
	if len(claims.Claims) == 0 {
		return "", nil
	}
	r := strings.TrimSpace(repo)
	// ONE measurement per claim, kept beside its evidence: the stamp renders the
	// SAME measurement the record reports, so the child's instruction and the
	// run's evidence cannot disagree (Rules 8/13 — measure once, read twice).
	type landed struct {
		claim taskreport.UnlandedClaim
		ev    MasterClaimEvidence
	}
	var stamped []landed
	for _, c := range claims.Claims {
		if strings.TrimSpace(c.Commit) == "" {
			// A premise naming no revision cannot be measured: it is reported,
			// never guessed at and never read as a pass.
			record = append(record, fmt.Sprintf(taskBriefingPremiseUnmeasRecordFmt,
				taskBriefingPremiseNoRevReason, taskreport.UnlandedClaimSentenceExcerpt(c.Sentence)))
			continue
		}
		ev := MasterClaimEvidenceAt(r, c.Commit)
		switch {
		case ev.OK():
			stamped = append(stamped, landed{claim: c, ev: ev})
			record = append(record, fmt.Sprintf(taskBriefingPremiseLandedRecordFmt,
				ev.ShortCommit, ev.ClaimRef, ev.AncestorExit, ev.ShortCommit))
		case ev.NotOnIntegrationBranch():
			record = append(record, fmt.Sprintf(taskBriefingPremiseOpenRecordFmt,
				ev.ShortCommit, ev.ClaimRef, ev.AncestorExit))
		default:
			record = append(record, fmt.Sprintf(taskBriefingPremiseUnmeasRecordFmt,
				ev.Err, taskreport.UnlandedClaimSentenceExcerpt(c.Sentence)))
		}
	}
	if len(stamped) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, taskBriefingPremiseHeadFmt, TaskBriefingPremiseLabel, IntegrationBranchName, len(stamped))
	for _, s := range stamped {
		fmt.Fprintf(&b, taskBriefingPremiseClaimFmt,
			s.ev.ShortCommit, s.ev.ClaimRef, s.ev.ShortCommit, s.ev.ClaimRef,
			s.ev.AncestorExit, s.ev.ClaimRef, TaskBriefingPremiseVerifyOnly,
			taskreport.UnlandedClaimSentenceExcerpt(s.claim.Sentence))
	}
	return strings.TrimRight(b.String(), "\n"), record
}

// taskBriefingPremiseAlreadyStampedExcerpt fills the already-stamped record's
// sentence field: there is no author sentence to quote — the stamp IS the
// subject.
const taskBriefingPremiseAlreadyStampedExcerpt = "(this briefing's own previously appended stamp)"

// taskBriefingPremiseNoRevReason is the stated reason for a claim that names no
// revision: a premise nobody can re-measure (Rule 1: named, not typed at the
// fmt site).
const taskBriefingPremiseNoRevReason = "the sentence names no commit-shaped revision, so nothing can be measured"
