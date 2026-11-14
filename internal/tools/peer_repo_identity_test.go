package tools

// peer_repo_identity_test.go — the regression guard for "the two tips are the
// same REPOSITORY", and for the prescription that must never be printed when
// they are not.
//
// THE DEFECT (measured 2026-10-25, evidence and raw output in the filing run's
// session directory): the delivery legs dialled `~/work/GoPi` while the host
// declared `peer1:/home/user/work/gopi`, and on the peer those are two
// repositories with disjoint root commits (12025 vs 359 commits, `rev-list
// --max-parents=0` answering different roots). The mirror ref therefore held a
// stranger's tip, and the staleness alarm answered it with
//
//	"… the two refs have DIVERGED … reconcile the tips … with
//	 `git merge refs/remotes/gitPushcheck/master`, after which this pass reports
//	 the ref aligned"
//
// — a prescription that, run, grafts the other repository's entire history into
// master (or dies on `fatal: refusing to merge unrelated histories`, which then
// invites `--allow-unrelated-histories`, i.e. the graft). Project Rule 38: a
// prescription must be executable and must not make things worse.
//
// These cases drive REAL git in REAL repositories, because the verdict is a
// property of commit graphs — a text fixture would prove nothing. They are
// PAIRED with the same-repository diverged case (task_sync_git_lag_test.go),
// which asserts the merge prescription is still printed there: removing the
// prescription everywhere would be a different bug.

import (
	"context"
	"strings"
	"testing"
)

const (
	peerIdentityTestStrangerMsg = "stranger-repo-root"
	peerIdentityTestOwnMsg      = "own-repo-root"
)

// peerIdentityTestBuildFixture returns (client, strangerPeer): the client is a
// real checkout on the integration branch, and the peer is a bare repository
// whose master has a DIFFERENT ROOT COMMIT — no common ancestor, which is
// exactly the measured shape of `~/work/GoPi` vs `/home/user/work/gopi` on the
// peer.
func peerIdentityTestBuildFixture(t *testing.T) (client, stranger string) {
	t.Helper()
	root := t.TempDir()

	// The client's own history.
	client = root + "/client"
	integrationClaimTestGit(t, root, "init", "-q", "-b", IntegrationBranchName, client)
	integrationClaimTestGit(t, client, "commit", "-q", "--allow-empty", "-m", peerIdentityTestOwnMsg)

	// The stranger: its own root commit, never shared with the client.
	own := root + "/stranger-work"
	integrationClaimTestGit(t, root, "init", "-q", "-b", IntegrationBranchName, own)
	integrationClaimTestGit(t, own, "commit", "-q", "--allow-empty", "-m", peerIdentityTestStrangerMsg)

	stranger = root + "/stranger.git"
	integrationClaimTestGit(t, root, "init", "-q", "--bare", "-b", IntegrationBranchName, stranger)
	integrationClaimTestGit(t, own, "push", "-q", stranger, "refs/heads/"+IntegrationBranchName)

	// The premise ("the two tips share no commit") is measured by the CASES, in
	// the same terms production measures it — after the mirror ref exists. A
	// premise probe here would ask about a ref nobody has written yet and read
	// git's exit 128 ("not a valid object name") as if it were the answer.
	return client, stranger
}

// TestPeerIdentityUnrelatedPeerIsNotReportedAsDiverged is the PRIMARY
// regression: on a mirror taken from an unrelated repository, the pass must
// report the DIFFERENT-REPOSITORY finding and must NOT emit the merge
// prescription.
func TestPeerIdentityUnrelatedPeerIsNotReportedAsDiverged(t *testing.T) {
	client, stranger := peerIdentityTestBuildFixture(t)

	// The mirror leg, exactly as the lane writes it.
	integrationClaimTestGit(t, client, "fetch", "-q", "--force", "--no-tags", stranger,
		"+refs/heads/"+IntegrationBranchName+":"+gitSyncLagRef)

	// MEASURE THE PREMISE, in git's own terms and on the refs production reads:
	// the fixture is only the defect's shape if git itself says the two tips
	// share nothing (exit 1 + empty output — not 128, which is "no such ref").
	if code := integrationClaimTestGitExit(t, client, peerRepoIdentityMergeBaseVerb,
		peerRepoIdentityAllFlag, gitSyncLagRef, refsHeadsPrefix+IntegrationBranchName); code != mergeVerifyGitExitNo {
		t.Fatalf("fixture premise broken: `git merge-base --all` answered exit %d, want %d (no common ancestor)", code, mergeVerifyGitExitNo)
	}

	// ① the classification itself.
	lag, ahead, detail := gitSyncIntegrationLag(client)
	if lag == gitSyncLagUnknown || lag == "" {
		t.Fatalf("the classification answered %q for a mirror that resolves — an unrelated peer is a MEASURED condition, not an unmeasurable one (detail %q)", lag, detail)
	}
	if !strings.Contains(detail, "DIFFERENT REPOSITORY") {
		t.Fatalf("the classification did not name the different-repository condition: %q", detail)
	}
	if strings.Contains(detail, gitSyncLagManualMergeCommand) {
		t.Fatalf("the classification printed the MERGE PRESCRIPTION (%q) for an unrelated peer — running it grafts another repository's history: %q",
			gitSyncLagManualMergeCommand, detail)
	}
	if strings.Contains(detail, "DIVERGED") {
		t.Fatalf("an unrelated peer is not a divergent pair — the two are different findings with different remedies: %q", detail)
	}
	_ = ahead

	// ② the whole pass: no step detail and no alarm may carry the prescription.
	rep := GitSyncClientPass(client, stranger)
	all := passStepSummary(rep)
	if strings.Contains(all, gitSyncLagManualMergeCommand) {
		t.Fatalf("a pass step printed the merge prescription for an unrelated peer: %s", all)
	}
	if line, warn := rep.GitSyncStaleAlarm(gitSyncLagTestRole); warn {
		if strings.Contains(line, gitSyncLagManualMergeCommand) {
			t.Fatalf("the stale alarm printed the merge prescription for an unrelated peer: %q", line)
		}
		if !strings.Contains(line, "DIFFERENT REPOSITORY") {
			t.Errorf("the alarm must name the different-repository condition, got %q", line)
		}
	}

	// ③ the delivery lane refuses, by name, before it measures anything against
	// a tip that belongs to another repository.
	_, err := SyncPeerFetch(context.Background(), client, stranger)
	if err == nil {
		t.Fatalf("SyncPeerFetch accepted a tip from an unrelated repository — every downstream gate would then measure against a stranger's history")
	}
	if !strings.Contains(err.Error(), "DIFFERENT REPOSITORY") {
		t.Errorf("the fetch refusal must name the different-repository finding, got %v", err)
	}
}

// TestPeerIdentitySameRepoIsStillClassifiedNormally is the CONTROL, and it is
// what keeps the fix honest: when the mirror really IS the peer's tip of THIS
// repository, the ordinary findings come back unchanged — including the merge
// prescription on a genuinely diverged pair, which task_sync_git_lag_test.go
// asserts in full. This case pins the narrow half needed here: the identity
// guard does not fire on a shared history.
func TestPeerIdentitySameRepoIsStillClassifiedNormally(t *testing.T) {
	client, stranger := peerIdentityTestBuildFixture(t)

	// Mirror the CLIENT's own tip: same repository by construction.
	integrationClaimTestGit(t, client, "fetch", "-q", "--force", "--no-tags", client,
		"+refs/heads/"+IntegrationBranchName+":"+gitSyncLagRef)
	shared, err := peerRepoIdentityShared(context.Background(), client, gitPushbackCheckRef,
		refsHeadsPrefix+IntegrationBranchName)
	if err != nil {
		t.Fatalf("the identity probe failed on a shared history: %v", err)
	}
	if !shared {
		t.Fatal("the identity probe called the client's OWN tip a different repository — the guard would refuse every ordinary delivery")
	}
	// And the stranger really is a stranger, so the case above is not vacuous.
	if shared, err := peerRepoIdentityShared(context.Background(), client, gitPushbackCheckRef, gitPushbackCheckRef); err != nil || !shared {
		t.Fatalf("control: a ref compared with ITSELF must be shared (shared=%v err=%v)", shared, err)
	}
	_ = stranger
}
