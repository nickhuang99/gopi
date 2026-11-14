package tools

// deploy_drift_test.go — the deployment-drift measurement driven against a REAL
// repository with a REAL artifact.
//
// THREE LEGS, and the middle one is why this file exists:
//
//	① STALE — an artifact whose attested revision is BEHIND a ref is reported
//	   with the distance and the verdict, not merely "old".
//	② POSITIVE CONTROL — an artifact that follows the ref exactly is reported as
//	   following it, and Stale() is false. Without this leg, leg ① would pass on
//	   a function that reports every artifact stale.
//	③ THE UNMEASURED ARM — an artifact that attests NO revision is STATED with a
//	   reason (Rule 15), never read as a clean or as a stale answer. This is the
//	   arm the measured host actually hit: a build from a LINKED WORKTREE carries
//	   no toolchain VCS stamp, so the artifact answers with no revision at all.
//
// The artifact is a shell script standing in for bin/gopi: DeployDriftOf resolves
// <repo>/bin/gopi and probes it with `sh -c '"$0" --version'`, so a fixture needs
// only to answer the way a real report does.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// deployDriftFixtureArtifact writes a fake bin/gopi whose --version prints the
// given report, and returns the repo path.
func deployDriftFixtureArtifact(t *testing.T, report string) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, rebuildBinDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf '%s\\n' '" + report + "'\n"
	if err := os.WriteFile(filepath.Join(dir, rebuildBinName), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

// deployDriftFixtureRepo builds a real repo with one branch, so a ref exists to
// measure against, and returns the repo path and its tip.
func deployDriftFixtureRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	if code, out := deployDriftGit(t, repo, "init", "-q", "-b", IntegrationBranchName); code != 0 {
		t.Fatalf("git init: %s", out)
	}

	deployDriftGit(t, repo, "config", "user.email", "t@t")
	deployDriftGit(t, repo, "config", "user.name", "t")
	deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "seed")
	_, tipOut := deployDriftGit(t, repo, "rev-parse", "HEAD")
	return repo, strings.TrimSpace(tipOut)
}

// deployDriftGit runs one real git command in the fixture repo. It is local to
// this file on purpose: the package has another helper with a different
// signature, and reusing it here would mean either changing its contract or
// adapting it for one call shape.
func deployDriftGit(t *testing.T, repo string, args ...string) (int, string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return 0, string(out)
}

// TestDeployDrift_ArtifactBehindTheRefIsStale is leg ①.
func TestDeployDrift_ArtifactBehindTheRefIsStale(t *testing.T) {
	repo, tip := deployDriftFixtureRepo(t)
	// The artifact attests an EMPTY-TREE revision that is an ancestor of the
	// tip but not the tip: the measured shape of a stale artifact. A report
	// naming a revision that git can resolve is what versionMatches reads.
	_, oldOut := deployDriftGit(t, repo, "rev-parse", "HEAD")
	old := strings.TrimSpace(oldOut)
	deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "second")
	if err := os.MkdirAll(filepath.Join(repo, rebuildBinDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf 'v0.0.0\\nrevision: " + old + "\\n'\n"
	if err := os.WriteFile(filepath.Join(repo, rebuildBinDirName, rebuildBinName), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	d := DeployDriftOf(repo)
	if d.Revision != old {
		t.Fatalf("revision = %q, want the artifact's own attested %q", d.Revision, old)
	}
	if !d.Stale() {
		t.Fatalf("an artifact one commit behind %s must be STALE; report: %+v (tip was %s)", IntegrationBranchName, d, tip)
	}
	var found bool
	for _, r := range d.Refs {
		if r.Ref != "refs/heads/"+IntegrationBranchName {
			continue
		}
		found = true
		if r.Reason != "" {
			t.Errorf("the local branch must be measurable; got reason %q", r.Reason)
		}
		if r.Behind != 1 {
			t.Errorf("behind = %d, want 1 (the artifact predates one commit)", r.Behind)
		}
	}
	if !found {
		t.Errorf("the report must name refs/heads/%s; got %+v", IntegrationBranchName, d.Refs)
	}
}

// TestDeployDrift_ArtifactFollowingTheRefIsNotStale is leg ② — the positive
// control. Without it, leg ① would pass on a function that always says STALE.
func TestDeployDrift_ArtifactFollowingTheRefIsNotStale(t *testing.T) {
	repo, tip := deployDriftFixtureRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, rebuildBinDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf 'v0.0.0\\nrevision: " + tip + "\\n'\n"
	if err := os.WriteFile(filepath.Join(repo, rebuildBinDirName, rebuildBinName), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	d := DeployDriftOf(repo)
	if d.Stale() {
		t.Fatalf("an artifact built from %s must NOT be reported stale; report: %+v", tip, d)
	}
	for _, r := range d.Refs {
		if r.Ref == "refs/heads/"+IntegrationBranchName && !r.Follows {
			t.Errorf("refs/heads/%s: Follows=false for an artifact built from its tip; reason %q", IntegrationBranchName, r.Reason)
		}
	}
}

// TestDeployDrift_NoAttestedRevisionIsStated is leg ③ — the arm the measured
// host actually hit: a linked-worktree build attests no revision, and that must
// be UNMEASURED with a reason, never a clean answer and never a stale one.
func TestDeployDrift_NoAttestedRevisionIsStated(t *testing.T) {
	repo, _ := deployDriftFixtureRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, rebuildBinDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf 'v0.0.0\\nrevision: unavailable — this build carries no VCS stamp\\n'\n"
	if err := os.WriteFile(filepath.Join(repo, rebuildBinDirName, rebuildBinName), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	d := DeployDriftOf(repo)
	if d.Revision != "" {
		t.Fatalf("an artifact attesting no revision must report %q, got %q", "", d.Revision)
	}
	if d.Stale() {
		t.Errorf("an UNMEASURED artifact must not read as stale — the two silences differ (Rule 15); got %+v", d)
	}
	stated := 0
	for _, r := range d.Refs {
		if r.Reason != "" {
			stated++
		}
	}
	if stated != len(d.Refs) {
		t.Errorf("every ref must carry a stated reason when no revision is attested; got %+v", d.Refs)
	}
}

// TestDeployDrift_NoArtifactIsNotAnError pins the ordinary state of a host that
// never built here: a checkout with no bin/gopi reports no revision, states the
// distance as unmeasured, and never panics or errors.
func TestDeployDrift_NoArtifactIsNotAnError(t *testing.T) {
	repo, _ := deployDriftFixtureRepo(t)
	d := DeployDriftOf(repo)
	if d.Revision != "" || d.Stale() {
		t.Errorf("a checkout with no artifact must be a stated non-answer; got %+v", d)
	}
	if !strings.HasSuffix(d.Artifact, filepath.Join(rebuildBinDirName, rebuildBinName)) {
		t.Errorf("the report must name where the artifact WOULD be; got %q", d.Artifact)
	}
}

// TestDeployDrift_RefsAreNamedOnce pins the report's ref list to the ONE
// definition (Rules 1/13/14): a second spelling of "the integration branch" is
// how a checker and the thing it checks drift apart.
func TestDeployDrift_RefsAreNamedOnce(t *testing.T) {
	if len(DeployDriftRefs) == 0 {
		t.Fatal("DeployDriftRefs is empty — nothing would be measured")
	}
	for _, ref := range DeployDriftRefs {
		if !strings.Contains(ref, IntegrationBranchName) {
			t.Errorf("ref %q does not carry the canonical integration branch %q", ref, IntegrationBranchName)
		}
	}
}

// --- The DIVERGED / AHEAD arms, and the seam that measures the RIGHT artifact.
//
// These exist because a false-clean SHIPPED: the report printed "0 behind" for
// the deployed artifact and the number was true. Measured 2026-10-28 on host
// host-a, deployed revision 84beb30 against the peer mirror b7a1ff8:
//
//	git rev-list --count 84beb30..refs/remotes/gitPushcheck/master  → 0     ("0 behind")
//	git rev-list --count refs/remotes/gitPushcheck/master..84beb30  → 284   (AHEAD)
//	git merge-base --is-ancestor 84beb30 refs/remotes/gitPushcheck/master → rc=1
//
// `behind` alone cannot tell "follows" from "from another line", so the verdict
// needs BOTH distances. Every leg below drives the real function against a real
// repository with real commits — no mock strings (Rule 31).

// deployDriftFixtureArtifactAt writes a fake bin/gopi in repo printing a report
// that attests rev, and returns the artifact's path.
func deployDriftFixtureArtifactAt(t *testing.T, repo, rev string) string {
	t.Helper()
	dir := filepath.Join(repo, rebuildBinDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf 'v0.0.0\\nrevision: " + rev + "\\n'\n"
	path := filepath.Join(dir, rebuildBinName)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// driftEntry returns the measured entry for ref, failing when the report did not
// name it (a report that silently dropped a ref would pass a zero-value check).
func driftEntry(t *testing.T, d DeployDrift, ref string) DeployDriftRef {
	t.Helper()
	for _, r := range d.Refs {
		if r.Ref == ref {
			return r
		}
	}
	t.Fatalf("the report must name %q; got %+v", ref, d.Refs)
	return DeployDriftRef{}
}

// TestDeployDrift_BehindIsCountedExactlyN is the epic's first red proof: an
// artifact N commits behind the ref says N — not "old", not zero.
func TestDeployDrift_BehindIsCountedExactlyN(t *testing.T) {
	const n = 3
	repo, _ := deployDriftFixtureRepo(t)
	_, oldOut := deployDriftGit(t, repo, "rev-parse", "HEAD")
	old := strings.TrimSpace(oldOut)
	for i := 0; i < n; i++ {
		deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "later")
	}
	deployDriftFixtureArtifactAt(t, repo, old)
	r := driftEntry(t, DeployDriftOf(repo), "refs/heads/"+IntegrationBranchName)
	if r.Behind != n {
		t.Errorf("Behind = %d, want exactly %d", r.Behind, n)
	}
	if r.Ahead != 0 {
		t.Errorf("Ahead = %d, want 0 for an artifact that is a pure ancestor", r.Ahead)
	}
	if got := r.Verdict(); got != DeployDriftVerdictBehind {
		t.Errorf("Verdict = %q, want %q", got, DeployDriftVerdictBehind)
	}
}

// TestDeployDrift_AtTheTipSaysItFollows is the epic's second red proof: N=0 is
// the POSITIVE statement ("follows the ref"), never a zero that reads as silence.
func TestDeployDrift_AtTheTipSaysItFollows(t *testing.T) {
	repo, tip := deployDriftFixtureRepo(t)
	deployDriftFixtureArtifactAt(t, repo, tip)
	r := driftEntry(t, DeployDriftOf(repo), "refs/heads/"+IntegrationBranchName)
	if r.Behind != 0 || r.Ahead != 0 {
		t.Errorf("at the tip both distances are 0; got ahead=%d behind=%d", r.Ahead, r.Behind)
	}
	if !r.Follows {
		t.Fatalf("an artifact built from the ref's tip must FOLLOW it; got %+v", r)
	}
	if got := r.Verdict(); got != DeployDriftVerdictFollows {
		t.Errorf("Verdict = %q, want %q", got, DeployDriftVerdictFollows)
	}
}

// TestDeployDrift_AheadIsNotReportedAsZeroBehind is THE regression guard for the
// shipped false-clean: an artifact from a divergent line has Behind == 0, and a
// report that only carries `behind` would call it clean.
func TestDeployDrift_AheadIsNotReportedAsZeroBehind(t *testing.T) {
	repo, seed := deployDriftFixtureRepo(t)
	// Build the divergent line WITHOUT checking anything out: a second branch
	// that has one commit the integration branch does not. The artifact's
	// revision lives there; the measured ref stays at `seed`. They share a
	// merge base and then part — the shape the deployed artifact actually had
	// (0 behind, 284 ahead).
	deployDriftGit(t, repo, "checkout", "-q", "-b", "other-line", seed)
	deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "artifact-side")
	_, sideOut := deployDriftGit(t, repo, "rev-parse", "HEAD")
	side := strings.TrimSpace(sideOut)
	deployDriftGit(t, repo, "checkout", "-q", IntegrationBranchName)
	deployDriftFixtureArtifactAt(t, repo, side)

	r := driftEntry(t, DeployDriftOf(repo), "refs/heads/"+IntegrationBranchName)
	if r.Behind != 0 {
		t.Fatalf("Behind = %d, want 0 — this leg must reproduce the '0 behind' reading the defect printed", r.Behind)
	}
	if r.Ahead != 1 {
		t.Errorf("Ahead = %d, want 1 (the artifact has one commit the ref does not)", r.Ahead)
	}
	if r.Follows {
		t.Fatalf("a divergent artifact must NOT follow the ref; got %+v", r)
	}
	if got := r.Verdict(); got != DeployDriftVerdictAhead {
		t.Errorf("Verdict = %q, want %q — 'behind == 0' is NOT a clean verdict", got, DeployDriftVerdictAhead)
	}
	if r.Verdict() == DeployDriftVerdictFollows {
		t.Errorf("Verdict must never be %q here: behind == 0 is true and meaningless", DeployDriftVerdictFollows)
	}
}

// TestDeployDrift_BothDistancesNonZeroIsDiverged is the OTHER half of the same
// guard: when the artifact and the ref have each moved on, BOTH distances are
// non-zero and the verdict must say so rather than pick one number.
func TestDeployDrift_BothDistancesNonZeroIsDiverged(t *testing.T) {
	repo, seed := deployDriftFixtureRepo(t)
	deployDriftGit(t, repo, "checkout", "-q", "-b", "other-line", seed)
	deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "artifact-side")
	_, sideOut := deployDriftGit(t, repo, "rev-parse", "HEAD")
	side := strings.TrimSpace(sideOut)
	deployDriftGit(t, repo, "checkout", "-q", IntegrationBranchName)
	deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "ref-side")
	deployDriftFixtureArtifactAt(t, repo, side)

	r := driftEntry(t, DeployDriftOf(repo), "refs/heads/"+IntegrationBranchName)
	if r.Ahead != 1 || r.Behind != 1 {
		t.Fatalf("want ahead=1 behind=1; got ahead=%d behind=%d", r.Ahead, r.Behind)
	}
	if got := r.Verdict(); got != DeployDriftVerdictDiverged {
		t.Errorf("Verdict = %q, want %q", got, DeployDriftVerdictDiverged)
	}
}

// TestDeployDrift_OriginRefIsMeasured pins the ref list to the one the epic
// names: origin/master was absent from the report while it was the ref the
// operator's number came from (27 commits behind).
func TestDeployDrift_OriginRefIsMeasured(t *testing.T) {
	want := refsRemotesPrefix + deployDriftOriginRemote + "/" + IntegrationBranchName
	var found bool
	for _, ref := range DeployDriftRefs {
		if ref == want {
			found = true
		}
	}
	if !found {
		t.Errorf("DeployDriftRefs must measure %q; got %v", want, DeployDriftRefs)
	}
}

// TestDeployDriftRepoOf_ResolvesTheArtifactsOwnRepo pins the seam that makes the
// report about the RUNNING artifact: <repo>/bin/gopi ⇒ <repo>, and a path with
// any other shape is REFUSED (ok=false) so the caller states its fallback
// instead of silently measuring the wrong repo.
func TestDeployDriftRepoOf_ResolvesTheArtifactsOwnRepo(t *testing.T) {
	// A REAL checkout shape: the parent must carry a .git entry, or the answer
	// is "no repo resolved" (the /usr/local case). Built on disk, not spelled.
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, devworkspace.GroundGitEntryName), 0o755); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(repo, rebuildBinDirName, rebuildBinName)
	if err := os.MkdirAll(filepath.Dir(art), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := DeployDriftRepoOf(art)
	if !ok || got != repo {
		t.Fatalf("DeployDriftRepoOf(%q) = (%q, %v), want (%q, true)", art, got, ok, repo)
	}
	for _, bad := range []string{
		"",
		// A bin/ that is not <repo>/bin: /usr/local holds no repository, and this
		// is the exact path this machine's deployed artifact lives at.
		filepath.Join(string(filepath.Separator), "usr", "local", "bin", "gopi"),
		filepath.Join(string(filepath.Separator), "bin", "gopi"), // no parent
	} {
		if got, ok := DeployDriftRepoOf(bad); ok {
			t.Errorf("DeployDriftRepoOf(%q) must refuse (not a <repo>/bin/gopi layout under a checkout); got (%q, true)", bad, got)
		}
	}
}

// --- The SUMMARY verdict: AHEAD does not vote, BEHIND does, scratch never does.
//
// THE DEFECT THESE LEGS PIN (measured 2026-10-29, host host-a, deployed
// artifact d50f746d7):
//
//	refs/heads/master:                follows the ref — the artifact was built from it
//	refs/remotes/origin/master:       1 ahead commits … not stale behind it
//	refs/remotes/gitPushcheck/master: 426 ahead commits … not stale behind it
//	STALE: this machine runs an artifact that does not follow the ref(s) above.
//
// The summary contradicted all three lines above it AND the
// DeployDriftVerdictAhead doc. The criterion was `!Follows`, which folds AHEAD
// in, and the third ref is a push-back SCRATCH mirror that freezes on any host
// with no push-back lane (3 days / 405 commits, monotonically growing) so no
// operator action could ever clear it.
//
// Both facts are driven here against REAL repositories with REAL refs — a
// fixture that only set struct fields would prove the switch statement, not that
// the refs are measured the way the host measures them (Rule 31).

// deployDriftFixtureRef points ref at rev in the fixture repo.
func deployDriftFixtureRef(t *testing.T, repo, ref, rev string) {
	t.Helper()
	if code, out := deployDriftGit(t, repo, "update-ref", ref, rev); code != 0 {
		t.Fatalf("update-ref %s: %s", ref, out)
	}
}

// TestDeployDrift_AheadScratchMirrorDoesNotMakeTheArtifactStale is leg (a): the
// artifact is the tip of refs/heads/master AND of refs/remotes/origin/master,
// and only the push-back scratch mirror is older — which is exactly the measured
// host. The summary must NOT call that stale, and the scratch entry must be
// STATED as non-voting rather than silently dropped.
func TestDeployDrift_AheadScratchMirrorDoesNotMakeTheArtifactStale(t *testing.T) {
	repo, tip := deployDriftFixtureRepo(t)
	// The scratch mirror is an ANCESTOR of the artifact: the artifact carries
	// commits the mirror does not — the `426 ahead` reading, reproduced.
	deployDriftFixtureRef(t, repo, gitPushbackCheckRootRef+IntegrationBranchName, tip)
	deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "landed after the mirror froze")
	_, tipOut := deployDriftGit(t, repo, "rev-parse", "HEAD")
	newTip := strings.TrimSpace(tipOut)
	deployDriftFixtureRef(t, repo, "refs/heads/"+IntegrationBranchName, newTip)
	deployDriftFixtureRef(t, repo, refsRemotesPrefix+deployDriftOriginRemote+"/"+IntegrationBranchName, newTip)
	deployDriftFixtureArtifactAt(t, repo, newTip)

	d := DeployDriftOf(repo)
	if d.Stale() {
		t.Fatalf("an artifact that is the tip of refs/heads/%s AND origin/%s must NOT be stale, whatever an older scratch mirror says; refs: %+v",
			IntegrationBranchName, IntegrationBranchName, d.Refs)
	}
	scratch := driftEntry(t, d, gitPushbackCheckRootRef+IntegrationBranchName)
	if !scratch.Scratch {
		t.Errorf("the push-back mirror must be marked Scratch; got %+v", scratch)
	}
	if got := scratch.Verdict(); got != DeployDriftVerdictAhead {
		t.Errorf("the scratch mirror's verdict = %q, want %q (its distance is still MEASURED)", got, DeployDriftVerdictAhead)
	}
	if scratch.CountsAsStale() {
		t.Errorf("a scratch ref must never vote on staleness; got Behind=%d Ahead=%d Scratch=%v", scratch.Behind, scratch.Ahead, scratch.Scratch)
	}
	// The voting set must be exactly the two real refs — the exclusion is
	// stated, never inferred.
	voting := d.VotingRefs()
	if len(voting) != 2 {
		t.Fatalf("VotingRefs() = %d entries, want 2 (the local branch and origin); got %+v", len(voting), voting)
	}
	for _, r := range voting {
		if r.Scratch {
			t.Errorf("VotingRefs() must not carry a scratch ref: %+v", r)
		}
	}
}

// TestDeployDrift_BehindMasterByFiveIsStillStale is leg (b) — the REVERSE
// guarantee, and the one a careless "AHEAD is fine" fix would break: an artifact
// that is genuinely behind the integration branch by N commits must still read
// stale. The artifact here follows NO ref: master, origin and the scratch mirror
// are all ahead of it.
func TestDeployDrift_BehindMasterByFiveIsStillStale(t *testing.T) {
	const n = 5
	repo, base := deployDriftFixtureRepo(t)
	for i := 0; i < n; i++ {
		deployDriftGit(t, repo, "commit", "--allow-empty", "-q", "-m", "landed")
	}
	_, tipOut := deployDriftGit(t, repo, "rev-parse", "HEAD")
	tip := strings.TrimSpace(tipOut)
	deployDriftFixtureRef(t, repo, refsRemotesPrefix+deployDriftOriginRemote+"/"+IntegrationBranchName, tip)
	deployDriftFixtureRef(t, repo, gitPushbackCheckRootRef+IntegrationBranchName, base)
	deployDriftFixtureArtifactAt(t, repo, base)

	d := DeployDriftOf(repo)
	if !d.Stale() {
		t.Fatalf("an artifact %d commits behind refs/heads/%s must be STALE; refs: %+v", n, IntegrationBranchName, d.Refs)
	}
	local := driftEntry(t, d, "refs/heads/"+IntegrationBranchName)
	if local.Behind != n {
		t.Errorf("Behind = %d, want exactly %d", local.Behind, n)
	}
	if !local.CountsAsStale() {
		t.Errorf("a ref holding %d commits the artifact lacks must vote stale; got %+v", n, local)
	}
	// The scratch mirror is an ancestor of the artifact here, so it answers AHEAD
	// and must still not vote — the stale verdict must come from the real refs.
	scratch := driftEntry(t, d, gitPushbackCheckRootRef+IntegrationBranchName)
	if scratch.CountsAsStale() {
		t.Errorf("the scratch mirror must not vote even when it is behind the artifact; got %+v", scratch)
	}
}

// TestDeployDrift_AheadOfEveryRefIsNotStale is the small table the epic asks
// for, driven at the DECISION level: AHEAD anywhere does not vote, DIVERGED and
// BEHIND do. It is the one place both halves of the criterion are visible at
// once, so a mutation that folds AHEAD back in fails here by name.
func TestDeployDrift_AheadOfEveryRefIsNotStale(t *testing.T) {
	cases := []struct {
		name  string
		ref   DeployDriftRef
		stale bool
	}{
		{name: "follows", ref: DeployDriftRef{Follows: true}, stale: false},
		{name: "ahead only", ref: DeployDriftRef{Ahead: 426}, stale: false},
		{name: "behind only", ref: DeployDriftRef{Behind: 5}, stale: true},
		{name: "diverged", ref: DeployDriftRef{Ahead: 3, Behind: 5}, stale: true},
		{name: "unmeasured", ref: DeployDriftRef{Reason: deployDriftRefUnreadableReason}, stale: false},
		{name: "scratch behind", ref: DeployDriftRef{Behind: 405, Scratch: true}, stale: false},
		{name: "scratch ahead", ref: DeployDriftRef{Ahead: 405, Scratch: true}, stale: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ref.CountsAsStale(); got != tc.stale {
				t.Errorf("CountsAsStale() = %v, want %v for %+v", got, tc.stale, tc.ref)
			}
			d := DeployDrift{Refs: []DeployDriftRef{tc.ref}}
			if got := d.Stale(); got != tc.stale {
				t.Errorf("Stale() = %v, want %v — the summary must read the ONE predicate, not re-derive it", got, tc.stale)
			}
		})
	}
}

// TestDeployDrift_ScratchNamespaceIsThePushbackLane pins the scratch marker to
// the lane's ONE namespace constant (Rule 14): a second spelling of
// "refs/remotes/gitPushcheck/" would let a rename silently make the frozen
// mirror vote again.
func TestDeployDrift_ScratchNamespaceIsThePushbackLane(t *testing.T) {
	for _, ref := range []string{
		gitPushbackCheckRootRef + IntegrationBranchName,
		fmt.Sprintf(gitPushbackPeerRefMirrorFmt, "task/some-branch"),
	} {
		if !deployDriftRefIsScratch(ref) {
			t.Errorf("%q must be in the push-back scratch namespace (%q)", ref, gitPushbackCheckRootRef)
		}
	}
	for _, ref := range []string{
		"refs/heads/" + IntegrationBranchName,
		refsRemotesPrefix + deployDriftOriginRemote + "/" + IntegrationBranchName,
		"",
	} {
		if deployDriftRefIsScratch(ref) {
			t.Errorf("%q is NOT a scratch ref; it must be able to vote", ref)
		}
	}
}
