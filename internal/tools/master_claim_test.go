package tools

// master_claim_test.go — the mechanical criterion's RED-THEN-GREEN proof
// (master_claim.go): the verdict is the EXIT STATUS of
// `git merge-base --is-ancestor <c> master`, so the test drives it through REAL
// git in a REAL repository, never through a text match.
//
// The shape is the motivating case: a commit kept alive ONLY by an archive tag
// (refs/tags/archive/<branch>) answers every `git show`, so a run reading it
// concluded "master already implements it" while master did not contain it. The
// RED leg builds exactly that repo; the GREEN leg lands the same commit on
// master and the SAME revision flips to OK — one command, two verdicts, no
// change in the evidence's spelling.

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// masterClaimGitTimeout bounds every git child this file spawns (Rule 27: a test
// that can park the package's budget must have its own ceiling).
const masterClaimGitTimeout = 60 * time.Second

// masterClaimFixture is the built repo: master at a seed commit and a SIDE
// branch carrying one further commit, tagged into the archive namespace with no
// refs/heads left behind (exactly the archived state).
type masterClaimFixture struct {
	Repo string
	// ArchiveRev is the archive TAG's name — how the motivating run named it.
	ArchiveRev string
	// ArchiveRef is the full tag ref (refs/tags/archive/…).
	ArchiveRef string
	// SideTip is the side branch's commit (the tag's target).
	SideTip string
	// MasterTip is master's commit (the seed).
	MasterTip string
}

const (
	masterClaimFixtureSeedMsg    = "seed"
	masterClaimFixtureFeatureMsg = "feature"
	masterClaimFixtureTagName    = archiveBranchPrefix + "feat/master-claim-fixture"
	masterClaimFixtureSideBranch = "feat/master-claim-fixture"
)

// masterClaimBuildRepo builds the fixture with REAL git commands.
func masterClaimBuildRepo(t *testing.T) masterClaimFixture {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), masterClaimGitTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", IntegrationBranchName)
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("commit", "--allow-empty", "-q", "-m", masterClaimFixtureSeedMsg)
	masterTip := git("rev-parse", "HEAD")
	git("checkout", "-q", "-b", masterClaimFixtureSideBranch)
	git("commit", "--allow-empty", "-q", "-m", masterClaimFixtureFeatureMsg)
	sideTip := git("rev-parse", "HEAD")
	git("tag", masterClaimFixtureTagName, masterClaimFixtureSideBranch)
	// The archived state: the ref is GONE, the tag is what keeps the tip alive.
	// Without this the fixture still holds a branch master could reach — and the
	// motivating case is precisely that no branch names it any more.
	git("update-ref", "-d", refsHeadsPrefix+masterClaimFixtureSideBranch)
	git("checkout", "-q", IntegrationBranchName)
	return masterClaimFixture{
		Repo: repo, ArchiveRev: masterClaimFixtureTagName,
		// The tag NAME carries the archive/ branch prefix; the REF it lives at is
		// the archive namespace plus the BRANCH name — derived, never re-typed
		// (Rules 1b/13).
		ArchiveRef: archiveTagRefPrefix + strings.TrimPrefix(masterClaimFixtureTagName, archiveBranchPrefix),
		SideTip:    sideTip, MasterTip: masterTip,
	}
}

// TestMasterClaim_ArchiveTagCommitIsNotMasterEvidence is the RED leg: the
// revision resolves (a command about it SUCCEEDS) and the verdict is still NOT
// evidence, with every observation the record needs.
func TestMasterClaim_ArchiveTagCommitIsNotMasterEvidence(t *testing.T) {
	f := masterClaimBuildRepo(t)
	ev := MasterClaimEvidenceAt(f.Repo, f.ArchiveRev)
	if ev.Err != "" {
		t.Fatalf("the archive tag must resolve to a commit, got Err=%q", ev.Err)
	}
	if ev.Commit != f.SideTip {
		t.Fatalf("peeled commit = %s, want the side tip %s", ev.Commit, f.SideTip)
	}
	if ev.OK() {
		t.Fatalf("an archive-tagged commit master does not contain was ruled OK evidence — "+
			"the motivating defect; observations: %v", ev.Observations())
	}
	if !ev.NotOnIntegrationBranch() {
		t.Fatalf("AncestorExit = %d, want the definite negative verdict", ev.AncestorExit)
	}
	if ev.AncestorExit != masterClaimNotAncestorExit {
		t.Fatalf("AncestorExit = %d, want %d", ev.AncestorExit, masterClaimNotAncestorExit)
	}
	if len(ev.ArchiveRefs) != 1 || ev.ArchiveRefs[0] != f.ArchiveRef {
		t.Fatalf("ArchiveRefs = %v, want [%s] — the namespace that keeps the commit alive",
			ev.ArchiveRefs, f.ArchiveRef)
	}
	if len(ev.PointingRefs) != 1 {
		t.Fatalf("PointingRefs = %v, want exactly the archive tag (the branch ref is gone)", ev.PointingRefs)
	}
	line := ev.VerdictLine()
	for _, want := range []string{"NOT an ancestor", IntegrationBranchName, f.ArchiveRef} {
		if !strings.Contains(line, want) {
			t.Errorf("verdict line %q must name %q", line, want)
		}
	}
	obs := strings.Join(ev.Observations(), "\n")
	if !strings.Contains(obs, "merge-base --is-ancestor") || !strings.Contains(obs, "exit=1") {
		t.Errorf("observations must carry the command and its exit code, got:\n%s", obs)
	}
}

// TestMasterClaim_LandedCommitIsMasterEvidence is the GREEN leg: the SAME
// revision, after its commit reaches master, flips to OK. Nothing about the
// revision's spelling changed — only the object graph did, which is what makes
// the criterion mechanical.
func TestMasterClaim_LandedCommitIsMasterEvidence(t *testing.T) {
	f := masterClaimBuildRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), masterClaimGitTimeout)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "git", "-C", f.Repo,
		"merge", "--ff-only", f.SideTip).CombinedOutput(); err != nil {
		t.Fatalf("land the side tip on master: %v: %s", err, out)
	}
	ev := MasterClaimEvidenceAt(f.Repo, f.ArchiveRev)
	if !ev.OK() {
		t.Fatalf("a commit that IS an ancestor of %s must be usable evidence; "+
			"exit=%d observations=%v", IntegrationBranchName, ev.AncestorExit, ev.Observations())
	}
	if ev.AncestorExit != masterClaimAncestorExit {
		t.Fatalf("AncestorExit = %d, want %d", ev.AncestorExit, masterClaimAncestorExit)
	}
	if ev.NotOnIntegrationBranch() {
		t.Fatalf("the landed commit is still reported as not-on-master")
	}
	if !strings.Contains(ev.VerdictLine(), "IS an ancestor") {
		t.Errorf("verdict line = %q, want the ancestor wording", ev.VerdictLine())
	}
}

// TestMasterClaim_ShortShaAndMasterItself: the revision forms a report actually
// uses — a short sha, and the integration branch itself.
func TestMasterClaim_ShortShaAndMasterItself(t *testing.T) {
	f := masterClaimBuildRepo(t)
	short := masterClaimShortSha(f.SideTip)
	if len(short) != masterClaimShortShaWidth {
		t.Fatalf("fixture short sha %q is not the %d-digit report form", short, masterClaimShortShaWidth)
	}
	ev := MasterClaimEvidenceAt(f.Repo, short)
	if ev.Commit != f.SideTip || ev.OK() {
		t.Fatalf("short sha %s: commit=%s ok=%v — want the side tip, not OK", short, ev.Commit, ev.OK())
	}
	self := MasterClaimEvidenceAt(f.Repo, IntegrationBranchName)
	if !self.OK() {
		t.Fatalf("%s must be an ancestor of itself; exit=%d err=%q",
			IntegrationBranchName, self.AncestorExit, self.Err)
	}
	landed := MasterClaimEvidenceAt(f.Repo, f.MasterTip)
	if !landed.OK() {
		t.Fatalf("master's own tip must be evidence; exit=%d err=%q", landed.AncestorExit, landed.Err)
	}
}

// TestMasterClaim_UnresolvedIsNotAPass: a revision that does not resolve is a
// THIRD verdict — never a silent "false" that a caller could read as a
// measurement, and never a pass.
func TestMasterClaim_UnresolvedIsNotAPass(t *testing.T) {
	f := masterClaimBuildRepo(t)
	for _, rev := range []string{"", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "no/such-ref"} {
		ev := MasterClaimEvidenceAt(f.Repo, rev)
		if ev.OK() || ev.NotOnIntegrationBranch() {
			t.Fatalf("rev %q: unresolved revisions must be neither OK nor a negative verdict (got %+v)", rev, ev)
		}
		if ev.AncestorExit != masterClaimUnresolvedExit {
			t.Fatalf("rev %q: AncestorExit = %d, want the unresolved sentinel %d",
				rev, ev.AncestorExit, masterClaimUnresolvedExit)
		}
		if ev.Err == "" {
			t.Fatalf("rev %q: an unresolved revision must state why", rev)
		}
		if !strings.Contains(ev.VerdictLine(), "UNRESOLVED") {
			t.Errorf("rev %q: verdict line %q must say UNRESOLVED", rev, ev.VerdictLine())
		}
	}
	if ev := MasterClaimEvidenceAt("", "master"); ev.Err == "" || ev.OK() {
		t.Fatalf("an empty repo must be an error verdict, got %+v", ev)
	}
}
