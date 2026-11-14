package tools

// peer_repo_identity.go — "is the tip we just mirrored the tip of the SAME
// repository we are standing in?" — asked BEFORE either half of the delivery
// lane believes the mirror (Rule 15: a measurement that reads as a verdict has
// to be a verdict about the thing it claims).
//
// THE DEFECT THIS ANSWERS (measured 2026-10-25, evidence in the filing run's
// session directory). The peer path the three delivery legs dialled was
// `~/work/GoPi` while this host's declared delivery point is
// `/home/user/work/gopi`, and on the peer those are TWO COMPLETELY UNRELATED
// REPOSITORIES (disjoint root commits; 12025 vs 359 commits). Two consequences,
// both measured, and only the second one is what this file fixes:
//
//   - every push was refused non-fast-forward (fixed by the resolver,
//     peer_repo_path.go);
//   - the mirror ref `refs/remotes/gitPushcheck/master` took the OTHER
//     repository's tip, so the staleness alarm reported
//     "… the two refs have DIVERGED … reconcile the tips with
//     `git merge refs/remotes/gitPushcheck/master`".
//
// THAT PRESCRIPTION IS NOT MERELY WRONG, IT IS THE WORST AVAILABLE ACTION: run
// on a pair with no common ancestor it grafts the whole unrelated history into
// master (git even refuses by default with `fatal: refusing to merge unrelated
// histories`, so the operator's next step is either a dead end or `--allow-unrelated-histories`,
// i.e. the graft). Project Rule 38 says a prescription must be executable and
// must not make things worse; the DIVERGED sentence's prescription was already
// narrowed once (devdoc/integration-lag-diverged-prescription.md) for the case
// "both tips are of the same repository" — it never asked whether they were.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const (
	// peerRepoIdentityMergeBaseVerb / peerRepoIdentityAllFlag name git's
	// ancestry survey: `merge-base --all <a> <b>` prints every merge base and
	// answers exit 1 with EMPTY output when the two commits share no ancestor
	// at all. The verb is referenced from the package's existing constant; only
	// the flag is spelled here (Rule 1b).
	peerRepoIdentityMergeBaseVerb = "merge-base"
	peerRepoIdentityAllFlag       = "--all"

	// peerRepoIdentityUnrelatedStmt is the ONE sentence both faces print for
	// "the two tips share no commit": the delivery lane's refusal (a named
	// error out of SyncPeerFetch) and the staleness classification (the
	// pass's `integration-ref-stale` step). It states the shape, the exact
	// measurement behind it, and why the merge remedy does not apply — the
	// three things a reader needs to stop looking for a merge to run.
	//
	// Arity: the peer clause (peerRepoIdentityPeerClause), the mirrored ref, the
	// local ref.
	peerRepoIdentityUnrelatedStmt = "%s is a DIFFERENT REPOSITORY: the peer tip it mirrors in %s shares NO commit with %s (`git %s %s` answered exit 1 with no output) — these are two unrelated histories, so nothing is pushable, fast-forwardable or mergeable between them and NO merge is a remedy; the peer path this leg dialled is the wrong repository"

	// peerRepoIdentityUnrelatedReasonFmt is the delivery lane's error, which
	// carries the statement above plus the ONE thing an operator can act on.
	// Arity: the statement, the resolver's provenance sentence.
	peerRepoIdentityUnrelatedReasonFmt = "%s (%s)"

	// peerRepoIdentityRemedyNote is the addendum both faces append: the
	// diagnosis is a mis-declared PATH, and the fix is a declaration, never a
	// git command — naming a git command here is what produced the dangerous
	// prescription in the first place.
	peerRepoIdentityRemedyNote = "fix the delivery point, not the history: check the `dev_repo` setting (and the repos table) against the peer's actual checkout path — the resolver reports which arm produced the path in use"
)

// peerRepoIdentityUnrelatedError renders the refusal for a mirror ref whose tip
// shares no commit with this repository's integration branch. It is ONE builder
// so the lane's error and the pass's step cannot drift into two spellings of
// one finding (Rule 14).
func peerRepoIdentityUnrelatedError(target, mirrorRef, localRef, provenance string) error {
	stmt := fmt.Sprintf(peerRepoIdentityUnrelatedStmt,
		target, mirrorRef, localRef, peerRepoIdentityMergeBaseVerb, peerRepoIdentityAllFlag)
	return fmt.Errorf(peerRepoIdentityUnrelatedReasonFmt,
		stmt, strings.TrimSpace(provenance)+"; "+peerRepoIdentityRemedyNote) // no-wrap: the identity verdict IS the error's whole content, and its caller decorates instead of unwrapping
}

// peerRepoIdentityUnrelatedDetail renders the same finding as the PASS's
// human sentence (the `integration-ref-stale` step's Detail), which is what an
// operator reading the pass sees. It carries no error wrapper and no remedy
// command: the pass has nothing to run, and saying so is the point.
func peerRepoIdentityUnrelatedDetail(target, mirrorRef, localRef string) string {
	return fmt.Sprintf(peerRepoIdentityUnrelatedStmt, peerRepoIdentityPeerClause(target), mirrorRef, localRef,
		peerRepoIdentityMergeBaseVerb, peerRepoIdentityAllFlag) + " — " + peerRepoIdentityRemedyNote
}

// peerRepoIdentityPeerClause renders the statement's opening: the dialled
// target when the caller knows it (the fetch does — it just dialled it), and a
// subject-free wording when it does not (the pass's staleness classification
// reads two LOCAL refs and never learned which target wrote the mirror). The
// two are ONE sentence otherwise: the measurement, the verdict and the reason the
// merge remedy does not apply do not depend on knowing the target.
func peerRepoIdentityPeerClause(target string) string {
	if strings.TrimSpace(target) == "" {
		return "the peer tip mirrored into the ref"
	}
	return "the peer at " + strings.TrimSpace(target)
}

// peerRepoIdentityShared reports whether the two refs share at least one
// commit, with an ERROR for the arm git could not answer.
//
// THREE-STATE, and the states are not interchangeable (Rule 15): exit 0 with a
// printed base is "shared", exit 1 with empty output is a MEASURED "unrelated"
// (git's documented answer, mergeVerifyGitExitNo), and anything else (128 for
// an unresolvable object, a timeout, a wedged repo) is UNMEASURED and must
// never be reported as either verdict. That is why the caller distinguishes
// `err != nil` from `false`.
func peerRepoIdentityShared(ctx context.Context, repoDir, refA, refB string) (bool, error) {
	out, err := gitPeerRun(ctx, repoDir, nil, peerRepoIdentityMergeBaseVerb, peerRepoIdentityAllFlag, refA, refB)
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != mergeVerifyGitExitNo {
			return false, fmt.Errorf("ask whether %s and %s share a commit: %w", refA, refB, err)
		}
	}
	return strings.TrimSpace(out) != "", nil
}

// peerRepoIdentityLocalRef names the ref this repository's own history is read
// from — the integration branch (master, else main), which is what every
// delivery leg measures the peer's tip against.
func peerRepoIdentityLocalRef(repoDir string) string {
	return refsHeadsPrefix + IntegrationBranchForRepo(repoDir)
}

// peerRepoIdentityCheck is the guard the delivery lane calls after a peer fetch:
// it answers (true, "") when the mirrored tip belongs to the same history, and
// (false, a named reason) when it does not — the reason being the statement
// above plus the resolver's provenance. An UNMEASURED answer is (true, ""): an
// error from git is not evidence of unrelatedness, and refusing a delivery on
// an unmeasured premise would be the same class of defect in the other
// direction (Rule 15 — never invent a finding).
//
// A missing LOCAL integration ref is unmeasured too: a repository that has no
// such ref yet has nothing to compare against, and the fetch that produced the
// mirror is the first step that would create it.
func peerRepoIdentityCheck(ctx context.Context, repoDir, target, provenance string) (bool, string) {
	localRef := peerRepoIdentityLocalRef(repoDir)
	if _, err := gitPeerRun(ctx, repoDir, nil, gitPeerRevParseVerb, gitPeerVerifyFlag, gitPeerQuietFlag,
		localRef+gitPeerCommitSuffix); err != nil {
		return true, "" // no local integration ref: nothing to compare, unmeasured
	}
	shared, err := peerRepoIdentityShared(ctx, repoDir, gitPushbackCheckRef, localRef)
	if err != nil {
		return true, "" // unmeasured, never read as "different repository"
	}
	if shared {
		return true, ""
	}
	return false, fmt.Sprintf(peerRepoIdentityUnrelatedReasonFmt,
		peerRepoIdentityUnrelatedDetail(target, gitPushbackCheckRef, localRef), strings.TrimSpace(provenance)) // no-wrap: the verdict is the whole message
}
