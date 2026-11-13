package tools

// master_claim.go — "X is ALREADY ON MASTER" as a MEASUREMENT, not an opinion.
//
// THE MEASURED CASE (session 01a0f3de): auditing work against master, the run
// took the tip of an ARCHIVED branch (`feat/capture-self-window`, tip
// e1701ac8 — held only by the tag refs/tags/archive/feat/capture-self-window)
// and treated it as "how master implements this". It concluded "master already
// has it, no gap" and finished exit=0. In the SAME run it also observed,
// correctly, that the branch ref was gone. Two judgements about the same fact,
// pointing opposite ways, with nothing forcing them to agree: the tag kept the
// commit alive inside the object store, so every `git show`/`git grep` against
// that sha SUCCEEDED — and an operation that succeeds reads exactly like
// evidence.
//
// The mechanical criterion this file owns is deliberately small, because it is
// the whole gap: a commit is evidence of "master already has X" ONLY IF THE
// COMMIT IS AN ANCESTOR OF THE INTEGRATION BRANCH. Membership is decided by the
// object graph — `git merge-base --is-ancestor <c> <master>`, whose EXIT STATUS
// is the verdict (0 = ancestor, 1 = not) — never by text, never by the fact
// that some command about the sha happened to succeed.
//
// The verdict is returned with the OBSERVATION that produced it (the command
// line and the exit code), because a claim a reviewer cannot re-run is not a
// measurement (Rule 5's evidence section, Rule 31's `Ran:` line). Nothing here
// re-runs a report's gates or edits a sentence: the caller decides what to do
// with the finding (internal/tools/master_claim_report.go RECORDS it at seal
// time, the same discipline gateclaim.go and runcheck.go use).

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

const (
	// masterClaimAncestorExit / masterClaimNotAncestorExit are `git merge-base
	// --is-ancestor`'s verdict statuses: 0 = the first commit IS an ancestor of
	// the second, 1 = it is NOT. Anything else (128 for an unresolvable rev, a
	// wedged repo, a signal) is an ERROR, not a verdict — the distinction is the
	// whole point of reading the status instead of the text (see
	// reclaim_ground_checklist.go, which owns the same 1 for the sibling
	// question).
	masterClaimAncestorExit    = 0
	masterClaimNotAncestorExit = 1

	// masterClaimUnresolvedExit is the sentinel recorded when the git child
	// never produced a status at all (it could not start, timed out, or was
	// signalled): the verdict is UNKNOWN, never "not an ancestor" and never
	// "an ancestor". A test pins it below both real statuses so a caller that
	// conflated "unknown" with a verdict would be visible.
	masterClaimUnresolvedExit = -1

	// masterClaimPeelSuffix asks git for the COMMIT a revision resolves to.
	// An annotated tag is not itself a commit, and `--is-ancestor` accepts only
	// commits — peeling first is what keeps an annotated archive tag from being
	// read as an error.
	masterClaimPeelSuffix = "^{commit}"

	// masterClaimRefFormat is the single for-each-ref format the pointing-ref
	// scan reads: the full ref name, one per line.
	masterClaimRefFormat = "%(refname)"

	// MasterClaimDigestPrefix introduces the seal's record of an unsupported
	// implementation claim in the ledger's result digest — the same append-only
	// detail the report path, the gate-claim record and the ran-check record
	// ride, so a reviewer reaches the finding from the row.
	MasterClaimDigestPrefix = "master-claim-unverified: "

	// MasterClaimWarningLabel is the stderr label of the seal-time finding — the
	// `[task] `-prefixed family gateclaim.go and runcheck.go belong to.
	MasterClaimWarningLabel = "master-claim"

	// MaxRecordedMasterClaims caps how many unsupported claims the digest
	// records (the gate-claim cap's sibling): a report with a systemic problem
	// must not push the row's digest past what an operator reads, and the first
	// few name the pattern.
	MaxRecordedMasterClaims = 3
)

// MasterClaimEvidence is the mechanical verdict for ONE revision: can it stand
// as evidence that the integration branch implements something?
//
// Every field is an OBSERVATION — a command, its exit status or its output —
// so the value renders into a report's evidence section verbatim. A zero-ish
// Commit means the revision did not resolve at all, which is itself a verdict
// (`Err` says why) and never a pass.
type MasterClaimEvidence struct {
	// Rev is the revision exactly as the report or the caller spelled it.
	Rev string
	// Commit is the peeled commit sha ("" when the revision did not resolve).
	Commit string
	// ShortCommit is the abbreviated sha, for a line an operator reads.
	ShortCommit string
	// AncestorExit is the exit status of the ancestry probe
	// (masterClaimAncestorExit / masterClaimNotAncestorExit /
	// masterClaimUnresolvedExit).
	AncestorExit int
	// ClaimRef is the ref the ancestry probe asked — the ONLY value that
	// changed on 2026-10-12 (task 01a12d30). It is IntegrationClaimTargetRef's
	// answer: the peer lane's mirror when the peers' tracking refs are
	// demonstrably ahead of this checkout, else the local integration branch.
	// The verdict's ref must be READABLE off the evidence, because the two refs
	// answer differently and a reviewer cannot re-run a claim that does not say
	// which one decided it.
	ClaimRef string
	// MirrorLookahead is how many commits the delivery lane's mirror carried that
	// the local integration branch did not, at the moment of the measurement —
	// the fact that made ClaimRef the mirror (0 when it did not, including the
	// diverged case, where neither ref is a superset of the other). It is here so
	// the line states WHY the mirror was believed, not only that it was.
	MirrorLookahead int
	// ArchiveRefs are the refs in the archive namespace
	// (refs/tags/archive/…) that point at Commit — the namespace that keeps a
	// DECIDED loser's commit reachable, which is how "the command worked" and
	// "master has it" came apart in the motivating run.
	ArchiveRefs []string
	// PointingRefs are ALL refs that point at Commit (archive, heads, other
	// tags). Empty + a resolvable Commit is the motivating run's second
	// observation: the commit survives, but no ref names it.
	PointingRefs []string
	// Err is the reason the revision could not be resolved ("" when it could).
	Err string
}

// OK reports whether the revision is usable as "master already implements
// this" evidence: it resolved to a commit AND that commit is an ancestor of the
// integration branch. Anything else — unresolvable, not an ancestor, or a
// status git never produced — is NOT evidence.
func (e MasterClaimEvidence) OK() bool {
	return e.Commit != "" && e.AncestorExit == masterClaimAncestorExit
}

// NotOnIntegrationBranch reports the finding the seal records: the revision
// resolved to a real commit, the ancestry probe produced a definite negative
// verdict, and something is holding that commit alive (an archive tag is the
// measured shape). An unresolvable revision is NOT this finding — it is the
// caller's `Err`, a different sentence.
func (e MasterClaimEvidence) NotOnIntegrationBranch() bool {
	return e.Commit != "" && e.AncestorExit == masterClaimNotAncestorExit
}

// PeelCmd is the command that resolved the revision to a commit.
func (e MasterClaimEvidence) PeelCmd() string {
	return masterClaimGitLine("rev-parse", "--verify", "--quiet", e.Rev+masterClaimPeelSuffix)
}

// AncestorCmd is the command whose EXIT STATUS is the verdict. It names the
// REF that was asked (ClaimRef) — the answer IntegrationClaimTargetRef
// resolved — because on a checkout whose local integration branch is behind the
// peer, the same commit is an ancestor of one ref and not of the other.
func (e MasterClaimEvidence) AncestorCmd() string {
	rev := e.Rev
	if e.ShortCommit != "" {
		rev = e.ShortCommit
	}
	ref := e.ClaimRef
	if ref == "" {
		ref = integrationClaimLocalRef // pre-fix evidence keeps rendering its old line
	}
	return masterClaimGitLine("merge-base", "--is-ancestor", rev, ref)
}

// PointingCmd is the command that found the refs holding the commit alive.
func (e MasterClaimEvidence) PointingCmd() string {
	sha := e.Commit
	if sha == "" {
		sha = e.Rev
	}
	return masterClaimGitLine("for-each-ref", "--points-at="+sha, "--format="+masterClaimRefFormat)
}

// Observations renders the evidence as the lines a report's evidence section
// carries: the command, then what it produced. It never states a result without
// the instrument that produced it (the shape Rule 5 prescribes).
func (e MasterClaimEvidence) Observations() []string {
	if e.Err != "" {
		return []string{fmt.Sprintf("%s → %s", e.PeelCmd(), e.Err)}
	}
	out := []string{
		fmt.Sprintf("%s → %s", e.PeelCmd(), e.Commit),
		fmt.Sprintf("%s → exit=%d", e.AncestorCmd(), e.AncestorExit),
		fmt.Sprintf("%s → %s", e.PointingCmd(), strings.Join(e.PointingRefs, ", ")),
	}
	return out
}

// VerdictLine is the one-line verdict an operator reads (the CLI face and the
// seal's record both use it, so the two can never disagree). It names the REF
// the verdict is about: on a checkout whose local integration branch trails the
// peer, `master` and the delivery mirror answer differently for the same commit
// and an operator must be able to see which one was asked.
func (e MasterClaimEvidence) VerdictLine() string {
	if e.Err != "" {
		return fmt.Sprintf("%s: UNRESOLVED — %s", e.Rev, e.Err)
	}
	ref := e.ClaimRef
	if ref == "" {
		ref = integrationClaimLocalRef
	}
	why := e.mirrorWhy()
	if e.OK() {
		return fmt.Sprintf("%s (%s) IS an ancestor of %s — usable as evidence that %s implements it "+
			"(exit=%d%s)", e.ShortCommit, e.Rev, ref, ref, e.AncestorExit, why)
	}
	holding := "no ref names it"
	if len(e.ArchiveRefs) > 0 {
		holding = "held alive by " + strings.Join(e.ArchiveRefs, ", ")
	}
	return fmt.Sprintf("%s (%s) is NOT an ancestor of %s — NOT evidence that %s implements it "+
		"(exit=%d%s; %s)", e.ShortCommit, e.Rev, ref, ref,
		e.AncestorExit, why, holding)
}

// mirrorWhy renders the look-ahead beside a verdict decided on the peer lane's
// mirror — the measured reason the mirror, not the local branch, was asked.
// Empty when the claim ref IS the local branch: there, no peer comparison was
// made and saying so would invent one.
func (e MasterClaimEvidence) mirrorWhy() string {
	if e.ClaimRef != gitPushbackCheckRef {
		return ""
	}
	return fmt.Sprintf("; the delivery lane's mirror was %d commit(s) ahead of %s with nothing in return, so it decided",
		e.MirrorLookahead, integrationClaimLocalRef)
}

// masterClaimGitLine renders a git invocation the way the observations spell
// it. The `-C <repo>` prefix is added by the caller through the repo argument
// of the exec helpers; this renders the git-level command.
func masterClaimGitLine(args ...string) string {
	return "git " + strings.Join(args, " ")
}

// masterClaimMirrorLookahead reports the look-ahead the resolved claim ref won
// on: the count when the ref IS the mirror, 0 when it is the local branch (the
// pre-fix answer, where no peer comparison was made).
func masterClaimMirrorLookahead(repo, claimRef string) int {
	if claimRef != gitPushbackCheckRef {
		return 0
	}
	_, lookahead := integrationClaimTargetRefAndLookahead(repo)
	return lookahead
}

// MasterClaimEvidenceAt measures one revision inside repo. It never returns an
// error: an unusable repo or revision is a verdict with `Err` set, because the
// caller is a RECORDER that must still produce its line (Rule 15 — a silent
// hole would be worse than a stated one).
func MasterClaimEvidenceAt(repo, rev string) MasterClaimEvidence {
	e := MasterClaimEvidence{Rev: strings.TrimSpace(rev), AncestorExit: masterClaimUnresolvedExit}
	if e.Rev == "" {
		e.Err = "empty revision"
		return e
	}
	if strings.TrimSpace(repo) == "" {
		e.Err = "no repo to measure against"
		return e
	}
	e.ClaimRef = IntegrationClaimTargetRef(repo)
	sha, err := repoSweepGit(repo, "rev-parse", "--verify", "--quiet", e.Rev+masterClaimPeelSuffix)
	if err != nil || strings.TrimSpace(sha) == "" {
		e.Err = fmt.Sprintf("git rev-parse could not resolve %q", e.Rev)
		return e
	}
	e.Commit = strings.TrimSpace(sha)
	e.ShortCommit = masterClaimShortSha(e.Commit)
	// ONE resolution of the target ref per measurement: the probe and the
	// rendered command must agree (Rules 8/13 — measure once, read twice).
	e.MirrorLookahead = masterClaimMirrorLookahead(repo, e.ClaimRef)
	e.AncestorExit = masterClaimAncestorStatus(repo, e.Commit, e.ClaimRef)
	refs, refErr := repoSweepGit(repo, "for-each-ref", "--points-at="+e.Commit, "--format="+masterClaimRefFormat)
	if refErr == nil {
		e.PointingRefs = masterClaimRefList(refs)
	}
	for _, r := range e.PointingRefs {
		if strings.HasPrefix(r, archiveTagRefPrefix) {
			e.ArchiveRefs = append(e.ArchiveRefs, r)
		}
	}
	sort.Strings(e.ArchiveRefs)
	return e
}

// MasterClaimEvidenceCwd measures one revision in the working directory's repo
// (the CLI face's entry point). A directory that is not a git work tree yields
// a verdict with `Err` set, never a panic and never a pass.
func MasterClaimEvidenceCwd(rev string) MasterClaimEvidence {
	repo, err := RepoRootAt(".")
	if err != nil {
		return MasterClaimEvidence{Rev: strings.TrimSpace(rev), AncestorExit: masterClaimUnresolvedExit,
			Err: fmt.Sprintf("no git work tree at the working directory: %v", err)}
	}
	return MasterClaimEvidenceAt(repo, rev)
}

// masterClaimAncestorStatus runs the probe and returns its EXIT STATUS — the
// verdict, and the reason an error path is a third value rather than a
// convenient false. ref is the one the caller resolved (never re-derived here):
// the exit status is only a verdict ABOUT THAT REF.
func masterClaimAncestorStatus(repo, commit, ref string) int {
	ctx, cancel := context.WithTimeout(context.Background(), repoSweepGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "merge-base", "--is-ancestor", commit, ref)
	err := cmd.Run()
	if err == nil {
		return masterClaimAncestorExit
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return masterClaimUnresolvedExit
}

// masterClaimRefList splits for-each-ref output into ref names (blank lines
// dropped, order preserved).
func masterClaimRefList(out string) []string {
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(strings.TrimRight(line, "\r")); s != "" {
			refs = append(refs, s)
		}
	}
	return refs
}

// masterClaimShortShaWidth is the abbreviation the reports and the CLI print
// (the repo's report convention: 8 hex digits). The sibling abbreviation in
// task_sync_git_pushback.go owns its OWN width for its own line; this one is
// the report form, named here rather than typed at the call site (Rule 1).
const masterClaimShortShaWidth = 8

// masterClaimShortSha abbreviates a full sha to the report form.
func masterClaimShortSha(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= masterClaimShortShaWidth {
		return sha
	}
	return sha[:masterClaimShortShaWidth]
}
