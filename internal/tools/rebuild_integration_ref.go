package tools

// rebuild_integration_ref.go — WHICH ref the close loop's "already current?"
// question is asked of (task 01a1385d, filed by row 01a1385d…/bug).
//
// THE MEASURED DEFECT. `RebuildAfterMerge` (rebuild.go) decided whether a
// rebuild was needed with `gitTip(repo)` = `git -C repo rev-parse --short HEAD`
// — the checkout's LOCAL HEAD. On a host whose local master trails the
// integration mirror the task grounds are cut from, an artifact whose attested
// revision EQUALS local HEAD read as "already current" and the rebuild SKIPPED
// while the machine ran code ~97 commits stale (measured 2026-10-14 on
// host-b: artifact attested c3a3f4eae74c = refs/heads/master, while
// refs/remotes/gitPushcheck/master read 7d0f7848b9e8 and was 97 commits ahead).
// The mechanism that already ticks could never repair that class: by its own
// definition the artifact WAS current.
//
// THE FIX IS A BETTER QUESTION, NOT A NEW MECHANISM. The ref that must answer
// "what should this artifact carry?" is the ref the grounds are cut from — the
// integration ref `IntegrationClaimTargetRef` already resolves for the
// ancestry question, the one the DELIVERY lane writes and the ground baseline
// reads (gitPushbackCheckRef, integration_claim_target.go). This file resolves
// the same ref for the version question: one answer to "which ref is the
// integration branch here", asked once, read by the close loop.
//
// NOTHING BEYOND THE REPOSITORY IS READ, and nothing is refreshed: the mirror
// ref either exists in this object store (then it is the fresher answer) or it
// does not (then the local facts answer, and the caller is TOLD that is what
// happened). No fetch, no timer, no state file, no new ref, no hook (Rule 35).
//
// THE FALLBACK IS THE SHIPPED BEHAVIOUR, BY CONSTRUCTION: a repo with an absent
// or diverged mirror falls back to its local integration branch, and a repo
// with neither (a peerless single-host deployment, a test fixture) falls back
// to HEAD — which is exactly the pre-fix measurement, so those deployments
// behave as before. What is new is only that the fallback is NAMED (the
// measurement's Note), never silently substituted: a reader of the close loop
// can tell "measured against the mirror it follows" from "measured against
// this checkout's own HEAD".

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const (
	// rebuildIntegrationRefGitUnresolvedExit is the sentinel recorded when the
	// ref probe produced no status at all (git could not start, timed out, or
	// was signalled): the measurement is UNKNOWN, and the caller must hear that
	// rather than a silent fallback to a stale ref.
	rebuildIntegrationRefUnresolved = -1

	// The two spellings of "the local integration branch", taken from the ONE
	// definitions (Rule 1/13): the local branch ref and the remote-tracking
	// mirror the delivery lane writes.
	rebuildIntegrationLocalRef = refsHeadsPrefix + IntegrationBranchName

	// rebuildIntegrationRefNotePrefix introduces the reason a LESS preferred ref
	// answered. The close loop prints it, so an operator reading a rebuild line
	// can never mistake a fallback for a measurement of the ref it follows.
	rebuildIntegrationRefNotePrefix = "note: "
)

// rebuildIntegrationRefNoteNoIntegrationRef is the Note for the LAST fallback
// arm: this checkout carries no integration ref at all (reason says which
// candidate was missing, ref names it), so the measurement is HEAD — the
// pre-fix question, kept for peerless single-host and test deployments.
func rebuildIntegrationRefNoteNoIntegrationRef(ref, reason string) string {
	return rebuildIntegrationRefNotePrefix + "this checkout carries no integration ref (" + ref + ": " +
		reason + ") — measured against " + taskGitHeadToken + ", the pre-fix measurement (peerless or test deployment)"
}

// rebuildTipMeasurement is the ONE answer to "what revision must this repo's
// artifact carry, and which ref said so?" — the fact the close loop's
// already-current shortcut and its post-build assertion must BOTH read, so a
// skip and a build cannot answer "current" about two different refs.
type rebuildTipMeasurement struct {
	// Revision is the short hash the artifact must attest. "" only with Err.
	Revision string
	// Ref is the ref the revision was read from — printed by the close loop,
	// never inferred by the caller.
	Ref string
	// Note is "" when the PREFERRED ref answered, and otherwise says which ref
	// answered instead and why. Always non-empty when Ref is the fallback, so a
	// fallback is never silent (Rule 15).
	Note string
	// Err is set only when NO ref answered — the measurement is unknown.
	Err error
}

// rebuildTipForRepo resolves the tip of the ref the grounds are cut from — the
// revision this repo's artifact must carry for the machine to run the code the
// task field is grounded on.
//
// IT ASKS THE EXISTING RESOLVER, and asks nothing else (Rule 14: one answer to
// "which ref is the integration branch here"). integrationClaimTargetMeasured
// already decides, from the object graph alone, whether this checkout's mirror
// is the fresher answer to a question about the integration branch — the same
// decision the ancestry question reads — and it already explains itself when
// the mirror does not win. This function adds only what the version question
// needs on top of that: the TIP of the resolved ref, abbreviated the way the
// pre-fix measurement was (`--short`), and a fallback when the checkout carries
// no integration ref at all (a peerless single-host deployment, a test fixture)
// to HEAD — the pre-fix measurement, i.e. the shipped behaviour, DELIBERATELY
// and with a Note saying so.
func rebuildTipForRepo(repo string) rebuildTipMeasurement {
	if strings.TrimSpace(repo) == "" {
		return rebuildTipMeasurement{Err: fmt.Errorf("no repo to measure")}
	}
	ref, _, note := integrationClaimTargetMeasured(repo)
	if rev, _, err := rebuildIntegrationRefTip(repo, ref); err == nil {
		return rebuildTipMeasurement{Revision: rev, Ref: ref, Note: note}
	}
	// No integration ref answers here. Falling back to HEAD keeps single-host
	// and test deployments behaving exactly as before the fix — and says so.
	if rev, _, err := rebuildIntegrationRefTip(repo, taskGitHeadToken); err == nil {
		return rebuildTipMeasurement{Revision: rev, Ref: taskGitHeadToken,
			Note: rebuildIntegrationRefNoteNoIntegrationRef(ref, note)}
	}
	return rebuildTipMeasurement{Err: fmt.Errorf("no ref in %s answers a tip (tried %s and %s)",
		repo, ref, taskGitHeadToken)}
}

// rebuildIntegrationRefTip reads ONE candidate ref's short tip. The second
// return is the probe's exit status, and it is a VERDICT, not an error:
// masterClaimNotAncestorExit (git's documented exit 1) is "no such ref here" —
// the ordinary, expected answer for a checkout that has no mirror — while every
// other failure (a wedged repository, an unresolvable revision, a git that
// never started: rebuildIntegrationRefUnresolved) is an ERROR the caller must
// be able to report (same discipline as masterClaimAncestorStatus and
// gitPushbackIsAncestorOf: one definition of "git said no" in this package).
//
// The peel suffix asks for the COMMIT a ref resolves to, so an annotated tag
// sitting at one of these names is never read as an error (masterClaimPeelSuffix).
func rebuildIntegrationRefTip(repo, ref string) (string, int, error) {
	if _, err := repoSweepGit(repo, integrationClaimRevParseVerb, integrationClaimRefVerifyFlag,
		integrationClaimRefQuietFlag, ref+masterClaimPeelSuffix); err != nil {
		return "", rebuildIntegrationRefExitCode(err), err
	}
	// --short: the same abbreviation shape the pre-fix measurement produced
	// (gitTip used `rev-parse --short HEAD`), so every existing reader of
	// ExpectedVersion/versionMatches keeps the length it was written against —
	// and versionMatches' prefix comparison absorbs the difference anyway.
	short, err := repoSweepGit(repo, integrationClaimRevParseVerb, taskGitShortFlag, ref+masterClaimPeelSuffix)
	if err != nil {
		return "", rebuildIntegrationRefExitCode(err), err
	}
	if strings.TrimSpace(short) == "" {
		return "", rebuildIntegrationRefUnresolved, fmt.Errorf("git rev-parse --short %s answered nothing", ref)
	}
	return strings.TrimSpace(short), 0, nil
}

// rebuildIntegrationRefExitCode recovers git's own exit status from the runner's
// wrapped error (repoSweepGit wraps with %w, so errors.As reaches the child's
// ExitError), and answers the UNKNOWN sentinel when there was no status at all
// (a git that never started, a timeout, a signal). The distinction is the whole
// point: masterClaimNotAncestorExit is git SAYING NO, and everything else is
// git being unable to answer (Rule 15 — a measurement that could not be taken
// is not evidence).
func rebuildIntegrationRefExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return rebuildIntegrationRefUnresolved
}
