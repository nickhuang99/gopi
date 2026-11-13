package tools

// integration_claim_target_test.go — the RED-THEN-GREEN proof for task
// 01a12d30's defect: the same commit is contained by the delivery lane's mirror
// and NOT by the local integration branch, and MasterClaimEvidenceAt used to
// answer only from the local one.
//
// The fixture is the MEASURED shape, not a caricature of it: a bare peer whose
// master carries a commit the client's checkout does not, a client whose
// `refs/remotes/gitPushcheck/master` holds that commit (the ref
// gitPeerMirrorRefspec writes and gitPushbackIsMerged decides on), and objects
// transported WITHOUT writing any ref (FETCH_HEAD only) wherever the mirror ref
// must be absent. Everything runs against REAL git in REAL repositories — the
// master_claim_test.go discipline: the verdict is an exit status, so a text
// fixture would prove nothing.

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	integrationClaimTestGitTimeout = 60 * time.Second
	integrationClaimTestSeedMsg    = "seed-base"
	integrationClaimTestGateMsg    = "landed-gate-commit"
	integrationClaimTestPeerMsg    = "peer-head-commit"
)

// integrationClaimTestRepo is the built client checkout plus the commits the
// assertions name.
type integrationClaimTestRepo struct {
	// Client is the checkout under test (local master at the seed).
	Client string
	// Gate is the commit the peer's master carries and the client's master does
	// not — the commit a reviewer asks about.
	Gate string
	// PeerHead is the peer's master tip (one commit past Gate).
	PeerHead string
}

// integrationClaimTestGit runs one git command in dir and fails the test on
// error — the fixture is built with real commands, so a broken fixture is a
// broken test rather than a silently different repository.
func integrationClaimTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationClaimTestGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@test",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// integrationClaimTestGitExit returns a git command's EXIT STATUS — the
// instrument every assertion here reads (never git's text).
func integrationClaimTestGitExit(t *testing.T, dir string, args ...string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationClaimTestGitTimeout)
	defer cancel()
	err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return masterClaimUnresolvedExit
}

// integrationClaimBuildRepo builds the fixture: peer master = seed → gate →
// peer-head; client = a clone stopped at the seed.
//
// Objects are always transported into the client WITHOUT writing a ref
// (`git fetch <peer> refs/heads/master` updates FETCH_HEAD only), so each arm
// below chooses exactly which refs exist:
//
//	mirror == ""     no mirror ref at all (the pre-fix repo shape)
//	mirror == a rev  the delivery lane's mirror points at that rev
func integrationClaimBuildRepo(t *testing.T, mirror string) integrationClaimTestRepo {
	t.Helper()
	peer := t.TempDir()
	integrationClaimTestGit(t, peer, "init", "-q", "-b", IntegrationBranchName)
	integrationClaimTestGit(t, peer, "commit", "-q", "--allow-empty", "-m", integrationClaimTestSeedMsg)
	seed := integrationClaimTestGit(t, peer, "rev-parse", "HEAD")

	clientParent := t.TempDir()
	client := clientParent + "/client"
	integrationClaimTestGit(t, clientParent, "clone", "-q", peer, client)
	if got := integrationClaimTestGit(t, client, "rev-parse", "HEAD"); got != seed {
		t.Fatalf("fixture: client HEAD = %s, want the seed %s", got, seed)
	}

	integrationClaimTestGit(t, peer, "commit", "-q", "--allow-empty", "-m", integrationClaimTestGateMsg)
	gate := integrationClaimTestGit(t, peer, "rev-parse", "HEAD")
	integrationClaimTestGit(t, peer, "commit", "-q", "--allow-empty", "-m", integrationClaimTestPeerMsg)
	peerHead := integrationClaimTestGit(t, peer, "rev-parse", "HEAD")

	// Objects in, refs untouched (FETCH_HEAD is not a ref for-each-ref lists).
	integrationClaimTestGit(t, client, "fetch", "-q", "--force", "--no-tags", peer,
		"refs/heads/"+IntegrationBranchName)
	if mirror != "" {
		integrationClaimTestGit(t, client, "update-ref", gitPushbackCheckRef, mirror)
	}
	return integrationClaimTestRepo{Client: client, Gate: gate, PeerHead: peerHead}
}

// TestIntegrationClaimTargetRefReadsTheMirrorWhenThePeerIsAhead is the GREEN
// leg: with the mirror strictly ahead, the verdict is taken on the mirror, and
// the SAME commit the local branch denies is reported as landed.
func TestIntegrationClaimTargetRefReadsTheMirrorWhenThePeerIsAhead(t *testing.T) {
	fx := integrationClaimBuildRepo(t, "" /*set below*/)
	// The mirror is the peer's head — exactly what gitPeerMirrorRefspec leaves.
	integrationClaimTestGit(t, fx.Client, "update-ref", gitPushbackCheckRef, fx.PeerHead)

	// The fixture must really BE the defect's shape, else the assertions below
	// prove nothing: the local branch must DENY what the mirror CONTAINS.
	if got := integrationClaimTestGitExit(t, fx.Client, "merge-base", "--is-ancestor", fx.Gate, integrationClaimLocalRef); got != masterClaimNotAncestorExit {
		t.Fatalf("fixture: %s vs %s → exit=%d, want %d (the stale-local-master shape)",
			fx.Gate, integrationClaimLocalRef, got, masterClaimNotAncestorExit)
	}
	if got := integrationClaimTestGitExit(t, fx.Client, "merge-base", "--is-ancestor", fx.Gate, gitPushbackCheckRef); got != masterClaimAncestorExit {
		t.Fatalf("fixture: %s vs %s → exit=%d, want %d (the delivery lane's answer)",
			fx.Gate, gitPushbackCheckRef, got, masterClaimAncestorExit)
	}

	ref, lead := integrationClaimTargetRefAndLookahead(fx.Client)
	if ref != gitPushbackCheckRef {
		t.Fatalf("target ref = %q, want the delivery lane's mirror %q (lead=%d)", ref, gitPushbackCheckRef, lead)
	}
	if lead < integrationClaimMinLookahead {
		t.Errorf("lead = %d, want >= %d (the measured premise that chose the mirror)", lead, integrationClaimMinLookahead)
	}

	ev := MasterClaimEvidenceAt(fx.Client, fx.Gate)
	if !ev.OK() {
		t.Fatalf("evidence: %s is denied — verdict line %q (this IS the defect's false negative)", fx.Gate, ev.VerdictLine())
	}
	if ev.AncestorExit != masterClaimAncestorExit {
		t.Errorf("AncestorExit = %d, want %d", ev.AncestorExit, masterClaimAncestorExit)
	}
	if ev.ClaimRef != gitPushbackCheckRef {
		t.Errorf("ClaimRef = %q, want %q", ev.ClaimRef, gitPushbackCheckRef)
	}
	if ev.MirrorLookahead < integrationClaimMinLookahead {
		t.Errorf("MirrorLookahead = %d, want >= %d", ev.MirrorLookahead, integrationClaimMinLookahead)
	}
	// The rendered verdict must name the ref that decided it, and the command
	// must be the one actually run — a reviewer re-runs the line.
	if !strings.Contains(ev.VerdictLine(), gitPushbackCheckRef) {
		t.Errorf("verdict line %q must name the ref it was measured against", ev.VerdictLine())
	}
	want := "merge-base --is-ancestor " + ev.ShortCommit + " " + gitPushbackCheckRef
	if !strings.Contains(ev.AncestorCmd(), want) {
		t.Errorf("AncestorCmd = %q, want it to contain %q", ev.AncestorCmd(), want)
	}
}

// TestIntegrationClaimTargetRefFallsBackWithoutAMirror is the CONTROL arm: the
// same repository with NO mirror ref must answer exactly as it did before this
// change — the local integration branch, the same NOT-an-ancestor verdict, and
// no invented peer comparison. Without this leg the fix could be satisfied by
// always answering from the mirror, which would be a different bug (a mirror
// left over from another topology would become the answer).
func TestIntegrationClaimTargetRefFallsBackWithoutAMirror(t *testing.T) {
	fx := integrationClaimBuildRepo(t, "" /* no mirror ref */)

	if ref, lead := integrationClaimTargetRefAndLookahead(fx.Client); ref != integrationClaimLocalRef || lead != 0 {
		t.Fatalf("target = (%q, %d), want (%q, 0) when no mirror exists", ref, lead, integrationClaimLocalRef)
	}

	ev := MasterClaimEvidenceAt(fx.Client, fx.Gate)
	if ev.OK() {
		t.Fatalf("evidence: %s must NOT be read as landed without a mirror — verdict line %q", fx.Gate, ev.VerdictLine())
	}
	if ev.ClaimRef != integrationClaimLocalRef {
		t.Errorf("ClaimRef = %q, want %q", ev.ClaimRef, integrationClaimLocalRef)
	}
	if ev.MirrorLookahead != 0 {
		t.Errorf("MirrorLookahead = %d, want 0 (no peer comparison was made)", ev.MirrorLookahead)
	}
	if !strings.Contains(ev.AncestorCmd(), integrationClaimLocalRef) {
		t.Errorf("AncestorCmd = %q, want the local ref %q", ev.AncestorCmd(), integrationClaimLocalRef)
	}
	if !strings.Contains(ev.VerdictLine(), integrationClaimLocalRef) {
		t.Errorf("verdict line %q must name the local ref it was measured against", ev.VerdictLine())
	}
}

// TestIntegrationClaimTargetRefIgnoresADivergedMirror pins the ACTIVATOR as
// "strictly ahead", not merely "a mirror exists": a mirror the local branch has
// commits missing (the two refs diverged — the peer's master was rewound) is
// NOT a superset of this checkout, so it is not the better answer and the
// fallback must hold.
func TestIntegrationClaimTargetRefIgnoresADivergedMirror(t *testing.T) {
	fx := integrationClaimBuildRepo(t, "")
	// Mirror = the peer's head, then give the LOCAL branch a commit of its own:
	// each ref now holds something the other does not.
	integrationClaimTestGit(t, fx.Client, "update-ref", gitPushbackCheckRef, fx.PeerHead)
	integrationClaimTestGit(t, fx.Client, "commit", "-q", "--allow-empty", "-m", "local-only-commit")

	lead, _ := integrationClaimCountBetween(fx.Client, integrationClaimLocalRef, gitPushbackCheckRef)   //stylegate:ignore a fixture count whose divergence assertion below fails on zero, so a git failure cannot pass silently
	behind, _ := integrationClaimCountBetween(fx.Client, gitPushbackCheckRef, integrationClaimLocalRef) //stylegate:ignore same fixture count in the opposite direction; the same assertion below carries the verdict
	if lead < integrationClaimMinLookahead || behind < integrationClaimMinLookahead {
		t.Fatalf("fixture: lead=%d behind=%d — want both >= %d (a DIVERGED pair)", lead, behind, integrationClaimMinLookahead)
	}
	if ref, _ := integrationClaimTargetRefAndLookahead(fx.Client); ref != integrationClaimLocalRef {
		t.Fatalf("target = %q, want the local branch %q for a diverged mirror", ref, integrationClaimLocalRef)
	}
}

// TestIntegrationClaimTargetRefOnAnUnreadableRepoAbstainsToTheLocalRef pins the
// conservative direction: a directory that is not a repository is NOT evidence
// for the mirror (Rule 15 — a measurement that could not be taken never moves a
// verdict).
func TestIntegrationClaimTargetRefOnAnUnreadableRepoAbstainsToTheLocalRef(t *testing.T) {
	if got := IntegrationClaimTargetRef(t.TempDir()); got != integrationClaimLocalRef {
		t.Fatalf("IntegrationClaimTargetRef(not-a-repo) = %q, want %q", got, integrationClaimLocalRef)
	}
	if got := IntegrationClaimTargetRef(""); got != integrationClaimLocalRef {
		t.Fatalf("IntegrationClaimTargetRef(\"\") = %q, want %q", got, integrationClaimLocalRef)
	}
}
