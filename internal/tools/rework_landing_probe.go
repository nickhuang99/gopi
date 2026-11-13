package tools

// rework_landing_probe.go — "master may ALREADY carry this change", asked of the
// object store and put into the branch-rework briefing.
//
// THE DEFECT THIS FILE CLOSES (measured on zbook, 2026-10-13). A merge run filed
// a branch-rework row for tip 8fafbafee ("fix(code_search): accept a ctags index
// whose pseudo-tag block is not at byte 0"). Master already carried a commit
// with that EXACT subject (ffa1cf4f8), from the identical base — the branch's
// work was already landed by the sibling. Nothing in the rework briefing said
// so: the briefing could only say why the branch did not merge, so the executor
// re-derived "is this already on master?" by hand, with `git log -S`, at the end
// of a run instead of at the start.
//
// WHY THE OBVIOUS PROBE IS THE WRONG ONE — the measurement that shaped this
// file. Neither of the two probes one reaches for first fires on this case:
//
//	$ git show 8fafbafee --format= | git patch-id --stable
//	088b1f30fa26082cd0f00099771de720acd5119d   ← the branch's tip
//	$ git show ffa1cf4f8 --format= | git patch-id --stable
//	ec53b3cf3f7db46b99d212912705b5af4d6aa506   ← the landed commit
//	$ git cherry master 8fafbafee
//	+ 8fafbafee88258bb0c01b9af6033fee29c3cd627 ← "+" = "patch-unique", i.e. NOT landed
//
// The patch-ids DIFFER (the two fixes were written twice, not copied), so a
// patch-identity probe reports the branch as unique work — the exact inversion
// of the truth. The discriminator that DOES fire is the commit SUBJECT, which is
// byte-identical here. This file therefore compares subjects and says so in its
// own text, so the next reader does not "improve" it back into patch-ids.
//
// WHAT IT IS NOT. It is not a verdict and it takes no write: a subject match is
// a SIGNAL for the executor to read the landed commit, never a decision to skip
// the branch. Two commits can share a subject and differ in content (this very
// repo carries 456 "delivery-point sync: …" commits), and a mechanical bookkeeping
// subject says nothing about a branch's work — hence the skip list below. The
// probe also never touches the ledger and creates no persistent artifact
// (Rule 35): it is one in-process `git log` and one `git merge-base`.

import (
	"context"
	"fmt"
	"strings"
)

// reworkLandingProbeGitTimeout bounds the probe's two git invocations. It reuses
// the package's git-runner timeout (repoSweepGitTimeout) rather than minting a
// second clock for the same kind of read (Rule 13/14).
const reworkLandingProbeGitTimeout = repoSweepGitTimeout

const (
	// reworkLandingHitScanCap bounds how many MATCHING commits ONE probe reads
	// back. It bounds the probe's OUTPUT, never the search: the subject is
	// filtered in git over the WHOLE history (see the grep flags below), and
	// this cap only stops the walk after that many hits.
	//
	// A RECENCY WINDOW WAS THE FIRST IMPLEMENTATION AND IT WAS WRONG. The probe
	// originally read the newest 400 commits of the integration branch. Measured
	// on zbook for the very pair this file exists for: master's landed commit
	// ffa1cf4f8 sits **1313 commits below master's tip** (`git rev-list --count
	// ffa1cf4f8..master` = 1313), so a 400-commit window MISSES it and prints
	// "NOT ALREADY LANDED (probe ran)" — a false negative on the exact case the
	// probe was written for, stated with the same confidence as a true negative.
	// Therefore: no window; the object store is asked with a fixed-string subject
	// filter (0.19 s over master's 10 614 commits) and the cap below is on hits.
	reworkLandingHitScanCap = 50

	// reworkLandingSubjectMatchMinChars is the shortest subject that may carry a
	// signal. Below this length a subject is not identifying enough to claim an
	// equivalence (a one-word "wip" matches half a repository), and the probe
	// answers "no signal" rather than a guess (Rule 15: an unknown is reported,
	// never rounded to a pass).
	reworkLandingSubjectMatchMinChars = 12

	// reworkLandingBookkeepingSubjectPrefixes are subjects this repo's machinery
	// writes ON EVERY BRANCH — a sync, a pre-merge push check and a merge commit.
	// They repeat by construction (measured on master: 456 "delivery-point sync:
	// …", 312 "pre-merge: verify branch against peer master before pushing"), so a
	// match on one of them means two branches shared plumbing, NOT that a
	// branch's work is already landed. A subject beginning with one of these
	// yields no signal.
	reworkLandingSyncSubjectPrefix     = "delivery-point sync:"
	reworkLandingPreMergeSubjectPrefix = "pre-merge:"
	reworkLandingMergeSubjectPrefix    = "Merge branch "
	reworkLandingMergeIntoSubjectMid   = " into "
	reworkLandingMergeMasterSubjectMid = "Merge master into "
	reworkLandingRevertSubjectPrefix   = "Revert "
	reworkLandingLogFormat             = "--format=%H%x09%s"
	reworkLandingIntegrationRefFormat  = "refs/heads/%s"
	// reworkLandingGrepLongFlag / reworkLandingGrepFixedFlag make git filter on the
	// WHOLE history instead of a window: --fixed-strings, because a commit subject
	// is LITERAL text (a subject carrying `[`, `+`, `.` or `(` is a regex, and a
	// regex would match subjects that are not equal to it — the probe must compare
	// bytes, never patterns). NOTE what --grep matches: the whole commit MESSAGE,
	// not the subject, so it is a CANDIDATE filter and the exact `subj == subject`
	// comparison below is the actual gate (measured on real master, whose
	// top-scoring candidate is `Merge commit 8fafbafee (re-land): <subject>` — a
	// commit whose subject ENDS with the string and is correctly rejected).
	reworkLandingGrepLongFlag  = "--grep=%s"
	reworkLandingGrepFixedFlag = "--fixed-strings"
	// reworkLandingAncestryLineFmt is the ONE spelling of the ancestry evidence
	// line: the command a reader re-runs to see the match for themselves.
	reworkLandingAncestryLineFmt = "git merge-base --is-ancestor %s %s"
)

// The ANSWER HEADLINES — the first line of each of Section()'s three answers.
// They are EXPORTED because a second surface has to TEACH the child what an
// answer looks like before it runs the probe: the merge LLM child's briefing
// (cmd/agent/merge_prompt.md §3.6.3) quotes these three lines, and the gate that
// pins the prompt reads them from HERE (Rule 14: one spelling, never a second
// phrasing typed into the prose). A rename therefore renames the prompt's own
// text in the same commit, or the gate fails.
const (
	// ReworkLandingSignalHeadline opens the SIGNAL answer.
	ReworkLandingSignalHeadline = "MASTER MAY ALREADY CARRY THIS CHANGE"
	// ReworkLandingCleanHeadline opens the negative answer, stated explicitly so
	// an absent signal and an unrun probe do not read alike.
	ReworkLandingCleanHeadline = "NOT ALREADY LANDED (probe ran)"
	// ReworkLandingUnknownHeadline opens the "the probe could not answer" one,
	// which is never a statement that the branch is unlanded.
	ReworkLandingUnknownHeadline = "ALREADY-LANDED PROBE DID NOT RUN"
	// ReworkLandingRefutedHeadline opens the answer for a subject match whose
	// CONTENT is not at the landed commit — the case a subject-only probe reports
	// as a duplicate branch, and the reason this corroboration exists.
	ReworkLandingRefutedHeadline = "SUBJECT MATCHED BUT THE CHANGE DID NOT LAND"
)

// The section spellings. Rule 14: ONE spelling per answer, so the payload text
// the lane writes and the test that pins it cannot drift, and every refusal lane
// that carries the section states the same thing the same way.
const (
	// reworkLandingSectionFmt is the SIGNAL: the integration branch carries a
	// commit with this branch's tip subject, and it is a DIFFERENT commit. The
	// headline is the FIRST verb (ReworkLandingSignalHeadline), never typed here.
	reworkLandingSectionFmt = `%s — check this BEFORE you rework.

Commit-SUBJECT equality is the DISCRIMINATOR, not patch identity. This branch's tip
  %s  %s
has the SAME subject as a commit already on %s
  %s  %s
and that commit is a DIFFERENT commit from this tip:
  %s

CONTENT CORROBORATION (the delivery proof's own content arm, run against that
commit — the subject is a NAME, and two commits can share a name and differ in
work):
  %s

⚠ ` + "`git patch-id`" + ` and ` + "`git cherry`" + ` DO NOT answer this question: measured on this very
shape, the two commits' patch-ids differ (088b1f30… vs ec53b3cf…) and ` + "`git cherry`" + ` lists the
tip as patch-unique ` + "`+`" + ` — i.e. a patch-identity probe reports "not landed" for work that
IS landed. Subject equality is what fires; the content arm above is what says
whether the change itself arrived.

WHAT TO DO WITH IT: read the landed commit (` + "`git show %s`" + `) and decide directly.
If it already carries this branch's intent, this branch is a duplicate — say so in
your report and stop; do not re-apply the diff to no effect. If it only overlaps,
treat the landed commit as the baseline to build ON rather than a conflict to
re-fight. Either way the subject match is a SIGNAL, not a verdict: two commits can
share a subject and differ in content, so read the commit before you conclude.`

	// reworkLandingSectionCleanFmt is the answer when nothing matched: stated
	// explicitly, because an absent signal and an unrun probe must not read alike.
	// It names the SEARCH it ran (the whole history, literal subject) rather than
	// a window, because "the commit you are looking for is 1313 commits deep" is
	// exactly the case a window silently mis-answers. The headline is the FIRST
	// verb (ReworkLandingCleanHeadline), never typed here.
	reworkLandingSectionCleanFmt = `%s. No commit in %s's ENTIRE history carries the
exact subject of this branch's tip
  %s  %s
so this branch's change is not on %s under the same subject. Basis: commit-SUBJECT
equality over the whole history (` + "`git log --fixed-strings --grep=<subject>`" + `) —
` + "`git patch-id`" + ` / ` + "`git cherry`" + ` are NOT used here, because measured on this repo they
report an already-landed change as patch-unique.`

	// reworkLandingSectionUnknownFmt is the answer when the probe could not run.
	// It never reads as "not landed": that inversion is the defect this file exists
	// to remove (Rule 15).
	reworkLandingSectionUnknownFmt = `%s (%s) — this is NOT a statement that the branch is
unlanded. Re-derive it by hand before reworking: compare this branch's tip SUBJECT
(` + "`git log -1 --format=%%s <tip>`" + `) against %s (` + "`git log %s --format=%%s`" + `).`

	// reworkLandingSectionRefutedFmt is the answer when the subject matched and the
	// CONTENT did not. It names the match (so the executor can read it), the content
	// arm's own sentence (which quotes the missing lines), and what that means: the
	// landed commit is a different change under the same name — the branch is NOT a
	// duplicate, and the rework must be filed.
	reworkLandingSectionRefutedFmt = `%s — this branch is NOT a duplicate of that commit.

A commit on %s carries this branch's tip subject
  %s  %s
  %s  %s
but the CONTENT CORROBORATION says the change itself did not land:
  %s

So the shared subject is a NAME collision, not a delivery: the landed commit
(` + "`git show %s`" + `) does something else, and this branch's work is still unlanded.
File the rework.
`
)

// reworkLandingContentUnjudgedLine is what the SIGNAL answer says when the content
// arm could not answer. It is a STATEMENT OF THE GAP, never silence: a reader must
// be able to tell "corroborated by content" from "the name matched and nobody
// checked the bytes" (Rule 15).
const reworkLandingContentUnjudgedLine = "NOT MEASURED — the content arm could not answer here, " +
	"so this is a SUBJECT-ONLY match; read the landed commit before concluding"

// ReworkLandingEvidence is the probe's answer. It is a VALUE, not a verdict:
// Signal() is true only when the probe actually fired.
type ReworkLandingEvidence struct {
	// Tip is the resolved commit the branch points at, "" when unresolvable.
	Tip string
	// Subject is Tip's commit subject (one line).
	Subject string
	// Landed is the FULL sha of the integration-branch commit carrying the same
	// subject, "" when the integration branch carries none (or the probe could
	// not run — Err says which).
	Landed string
	// LandedSubject is that commit's own subject, carried so the section can show
	// both sides of the match rather than asserting equality.
	LandedSubject string
	// LandedDistinct is true when Landed is a different commit from Tip. A match
	// that IS the tip itself (the branch is an ancestor of the integration
	// branch — already merged outright) is NOT this signal: the merge run reports
	// that case as `already-merged` and files no rework row.
	LandedDistinct bool
	// Proof is the ancestry command and its observed exit status, i.e. the
	// evidence for LandedDistinct.
	Proof string
	// ContentLanded / ContentWhy / ContentJudged are the CONTENT CORROBORATION
	// of the subject match, taken from the delivery proof's OWN content arm
	// (deliveryProofContentLanded, task_delivery_proof_content.go) rather than a
	// fourth containment test implemented here (Rule 43: reuse first). The
	// subject is a NAME; two commits can carry the same name and different work,
	// and a probe that stops at the name reports a duplicate branch whose change
	// was never delivered.
	//
	//	ContentJudged=false              the arm could not answer (range
	//	                                 unreadable, oversized, or switched off
	//	                                 by the red proof) — NOT a refutation;
	//	ContentJudged=true, ContentLanded=true   the landed commit carries the
	//	                                 added lines of every commit this branch
	//	                                 produced, after the arm's rebase
	//	                                 normalisations;
	//	ContentJudged=true, ContentLanded=false  it does not, and ContentWhy
	//	                                 names WHICH lines are missing.
	ContentLanded bool
	// ContentWhy is the arm's own sentence: the closest destination commit, the
	// difference class per file and (on a miss) the missing lines quoted.
	ContentWhy string
	// ContentJudged reports whether the content arm could answer at all.
	ContentJudged bool
	// Err records why the probe could not answer. Non-empty means UNKNOWN, never
	// "clean".
	Err string
}

// Signal reports whether the integration branch already carries a DIFFERENT
// commit with this branch's tip subject — the one condition that puts the
// section's warning into the briefing.
//
// A REFUTED CONTENT MATCH IS NOT A SIGNAL. The subject is a NAME; the content
// arm (see ContentJudged) is what says whether the change itself arrived. When
// that arm answered and said the added lines are NOT at the landed commit, the
// match names a different change that happens to share a subject, and reporting
// it as "master may already carry this" is exactly the false positive this
// corroboration exists to remove. An arm that could NOT answer leaves the signal
// standing: the name match is real evidence, and an unanswered question is not a
// refutation (Rule 15).
func (e ReworkLandingEvidence) Signal() bool {
	if e.Err != "" || e.Landed == "" || !e.LandedDistinct {
		return false
	}
	return !(e.ContentJudged && !e.ContentLanded)
}

// Section renders the evidence as the briefing text: the ONE spelling of each of
// the three answers (Rule 14). It is always non-empty, so a caller appends it
// unconditionally and an unrun probe still tells the executor which fact it is
// missing.
func (e ReworkLandingEvidence) Section() string {
	switch {
	case e.Signal():
		return fmt.Sprintf(reworkLandingSectionFmt,
			ReworkLandingSignalHeadline,
			shortSha(e.Tip), e.Subject,
			IntegrationBranchName,
			shortSha(e.Landed), e.LandedSubject,
			e.Proof,
			e.contentCorroborationLine(),
			shortSha(e.Landed))
	case e.Err != "":
		return fmt.Sprintf(reworkLandingSectionUnknownFmt, ReworkLandingUnknownHeadline, e.Err, IntegrationBranchName, IntegrationBranchName)
	case e.ContentJudged && !e.ContentLanded:
		// The subject matched and the CONTENT did not: this is the refutation the
		// corroboration exists to state, and it must never read as either the
		// signal or the clean answer — "a commit with this name is on master" and
		// "your change is on master" are different facts.
		return fmt.Sprintf(reworkLandingSectionRefutedFmt,
			ReworkLandingRefutedHeadline,
			shortSha(e.Tip), e.Subject,
			IntegrationBranchName,
			shortSha(e.Landed), e.LandedSubject,
			e.ContentWhy,
			shortSha(e.Landed))
	default:
		return fmt.Sprintf(reworkLandingSectionCleanFmt,
			ReworkLandingCleanHeadline,
			IntegrationBranchName,
			shortSha(e.Tip), e.Subject,
			IntegrationBranchName)
	}
}

// contentCorroborationLine is the SIGNAL answer's corroboration sentence: the
// content arm's own evidence when it ran, and an explicit statement of what is
// missing when it did not (Rule 15 — an unanswered question is named, never
// rounded to a pass).
func (e ReworkLandingEvidence) contentCorroborationLine() string {
	if e.ContentJudged {
		return e.ContentWhy
	}
	return reworkLandingContentUnjudgedLine
}

// ProbeReworkLanding asks whether the integration branch already carries a
// commit with branch's tip subject, and reports the landed sha when it does.
//
// It NEVER returns an error and it NEVER writes: every failure is an Evidence
// whose Err is set, so a caller that appends Section() unconditionally gets a
// briefing that states either the fact or the fact that it does not know it.
func ProbeReworkLanding(repo, branch string) ReworkLandingEvidence {
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	if repo == "" {
		return ReworkLandingEvidence{Err: "no repo given"}
	}
	if branch == "" {
		return ReworkLandingEvidence{Err: "no branch given"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), reworkLandingProbeGitTimeout)
	defer cancel()

	// branchDeleteGit is this package's ONE git runner (`git -C <repo> …`); the
	// probe reuses it rather than minting a second one for the same read.
	tipOut, err := branchDeleteGit(ctx, repo, "rev-parse", "--verify", branch+"^{commit}")
	if err != nil {
		return ReworkLandingEvidence{Err: "branch " + branch + " does not resolve to a commit here"}
	}
	tip := strings.TrimSpace(tipOut)

	// %s is the subject line alone, so a multi-line message cannot smuggle a
	// second line into a one-line comparison.
	subjOut, err := branchDeleteGit(ctx, repo, "log", "-1", "--format=%s", tip)
	if err != nil {
		return ReworkLandingEvidence{Tip: tip, Err: "could not read the tip's subject"}
	}
	subject := strings.TrimSpace(subjOut)

	ev := ReworkLandingEvidence{Tip: tip, Subject: subject}
	if !reworkLandingSubjectCarriesSignal(subject) {
		// A bookkeeping subject (or one too short to identify anything) is
		// reported as "no signal" WITHOUT running the log scan: a match on a
		// subject every branch shares would be an equivalence claim the probe
		// cannot support.
		return ev
	}

	integRef := fmt.Sprintf(reworkLandingIntegrationRefFormat, IntegrationBranchName)
	// The subject is filtered in GIT over the whole history (fixed-string, so a
	// subject carrying regex metacharacters compares literally); the log is then
	// capped on HITS, never on the walk. Newest-first order is git's own, so the
	// match reported is the newest equivalent commit.
	logOut, err := branchDeleteGit(ctx, repo, "log",
		"-"+fmt.Sprint(reworkLandingHitScanCap),
		reworkLandingGrepFixedFlag,
		fmt.Sprintf(reworkLandingGrepLongFlag, subject),
		reworkLandingLogFormat, integRef)
	if err != nil {
		ev.Err = "could not read " + IntegrationBranchName + "'s history"
		return ev
	}
	for _, line := range strings.Split(logOut, "\n") {
		sha, subj, ok := reworkLandingSplitLogLine(line)
		if !ok || subj != subject {
			continue
		}
		ev.Landed = sha
		ev.LandedSubject = subj
		break
	}
	if ev.Landed == "" {
		return ev
	}
	if ev.Landed == tip {
		// The tip IS on the integration branch: already merged, a different case
		// from "an equivalent commit landed" — no signal (the merge run reports it
		// as already-merged and files no rework row).
		return ev
	}
	ev.LandedDistinct = true
	ev.Proof = reworkLandingAncestryProof(ctx, repo, ev.Landed, shortSha(tip))
	// THE CONTENT CORROBORATION. A subject match is a NAME match, and this file's
	// own doc says two commits can share a subject and differ in content — this is
	// where that is MEASURED instead of merely warned about. The arm is the
	// delivery proof's (`deliveryProofContentLanded`), reused verbatim so the two
	// readers of "did this land" cannot drift apart again (Rule 43): the same
	// normalisations, the same bounded history scan, the same missing-line
	// sentence. src=repo is the only directory that holds BOTH the branch's own
	// commits and the integration ref (the probe runs inside one repo), and the
	// destination is the SAME repo's integration ref at the SAME commit the
	// subject matched — so "the landed commit" and "the destination" are the same
	// thing here, and the arm answers about the right object.
	ev.ContentLanded, ev.ContentWhy, ev.ContentJudged = deliveryProofContentLanded(
		ctx, repo, repo, ev.Landed, ev.Landed, tip)
	if !ev.ContentJudged {
		// The arm declines when the tip is not readable HERE, when the range
		// exceeds its cap, or when its own red proof switched it off. A decline is
		// NOT a refutation: Signal() keeps the name match, and the section states
		// the gap by name.
		ev.ContentWhy = ""
	}
	return ev
}

// reworkLandingAncestryProof renders the evidence line for the match: the landed
// commit's ancestry verdict on the integration branch, and the tip's, so a
// reader can re-run both. A verdict the probe could not establish is REPORTED as
// unestablished (Rule 15) rather than printed as a success.
func reworkLandingAncestryProof(ctx context.Context, repo, landed, shortTip string) string {
	integRef := fmt.Sprintf(reworkLandingIntegrationRefFormat, IntegrationBranchName)
	landedLine := fmt.Sprintf(reworkLandingAncestryLineFmt, shortSha(landed), IntegrationBranchName)
	if _, err := branchDeleteGit(ctx, repo, "merge-base", "--is-ancestor", landed, integRef); err == nil {
		return landedLine + "; " + fmt.Sprintf(reworkLandingAncestryLineFmt, shortTip, IntegrationBranchName) + " → exit 1 (a different commit)"
	}
	return landedLine + " (verdict not established by this probe — check by hand)"
}

// reworkLandingSplitLogLine splits one `git log --format=%H%x09%s` line into sha
// and subject. A line without the separator is not a log line and is skipped.
func reworkLandingSplitLogLine(line string) (sha, subject string, ok bool) {
	sha, subject, ok = strings.Cut(strings.TrimRight(line, "\r"), "\t")
	if !ok {
		return "", "", false
	}
	sha = strings.TrimSpace(sha)
	subject = strings.TrimSpace(subject)
	if sha == "" || subject == "" {
		return "", "", false
	}
	return sha, subject, true
}

// reworkLandingSubjectCarriesSignal reports whether a subject is identifying
// enough to found an equivalence on: long enough, and not one of the subjects
// this repo's own machinery writes on every branch.
func reworkLandingSubjectCarriesSignal(subject string) bool {
	subject = strings.TrimSpace(subject)
	if len(subject) < reworkLandingSubjectMatchMinChars {
		return false
	}
	if strings.HasPrefix(subject, reworkLandingSyncSubjectPrefix) ||
		strings.HasPrefix(subject, reworkLandingPreMergeSubjectPrefix) ||
		strings.HasPrefix(subject, reworkLandingRevertSubjectPrefix) ||
		strings.HasPrefix(subject, reworkLandingMergeMasterSubjectMid) {
		return false
	}
	// A merge commit's subject names the branch it merged, so it is unique per
	// merge — but it says nothing about the WORK, and this lane files rows for
	// branches that could not merge at all. Treated as bookkeeping.
	if strings.HasPrefix(subject, reworkLandingMergeSubjectPrefix) &&
		strings.Contains(subject, reworkLandingMergeIntoSubjectMid) {
		return false
	}
	return true
}
