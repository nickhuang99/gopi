package tools

// THE TEST FENCE AT THE PUSH-BACK BOUNDARY — red proof, re-exec proof and the
// positive control, in one file.
//
// The defect this closes: a `t <t@t>` fixture branch reached the operator's
// real repository by PUSH (the reflog of the residue says `push`). The
// close-out chain has been fenced since 2026-09-22 (finishCleanupTargetLeavesTestSpace),
// but the fence lived at ONE boundary — the close-out chain's target injection
// point — while this lane (`gitPushbackPlan` -> `gitPushbackPushOne`) renders
// its own target from the declared peers and had no such question. This file
// drives THAT boundary.
//
// Three observations, and each exists because the other two cannot replace it:
//
//	① IN-PROCESS (`go test`): the target is a real node's repository but the
//	   RECEIVER is the test's own sentinel (the ssh shim), so a RED run — on a
//	   checkout without the fence — observes the leak attempt without any
//	   possibility of writing the real repo (Rule 30: a test never writes it).
//	   The observation is the SENTINEL's refs/heads: empty before AND after
//	   when the fence holds, gaining the fixture branch when it does not.
//	② RE-EXEC'D CHILD: `testing.Testing()` is FALSE in a subprocess, so an
//	   in-process-only fence would be green here and open in the one arm where
//	   a fixture shelling out to `go test`/a helper binary pushes for real.
//	   The child re-enters this package's test binary with the marker env
//	   unset and reports the same two readings through `go test -v` output.
//	   RED on a marker-less fence: the child's sentinel gains the ref.
//	③ POSITIVE CONTROL: a fixture pushing to its OWN temp-rooted bare repo
//	   still delivers. Without it, a fence that refuses everything (or a
//	   predicate that no longer sees test space) would read green.
//
// The fence itself is REUSED, never re-implemented: the predicate
// (finishCleanupTargetLeavesTestSpace) and its refusal line are the ones the
// close-out chain has shipped since 2026-09-22 — this file pins the fact that
// the second boundary asks the SAME question.
//
// Run:
//
//	go test ./internal/tools/ -run 'TestPushbackTestFence' -count=1 -v
//	# the RED direction (fence opt-out set, the pre-fence behaviour):
//	GOPI_FINISH_CLEANUP_TEST_PUSH=1 go test ./internal/tools/ -run 'TestPushbackTestFence' -count=1

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	// pbFenceFixtureBranch is the deliverable the fence must keep out of a real
	// repository. Named after the residue family (a fixture branch, an orphan
	// root, a placeholder author) so a reader of a failure knows what almost
	// landed.
	pbFenceFixtureBranch = "feat/zz-testfence-orphan"

	// pbFenceFixtureFile / pbFenceFixtureBlob are the residue's exact content:
	// the measured blob in the real repo's orphan root. Byte-for-byte, so a RED
	// reading is comparable with the object that actually appeared there.
	pbFenceFixtureFile = "orphan.txt"
	pbFenceFixtureBlob = "orphan\n"

	// pbFenceShimScript is the checked-in ssh sentinel (shared with the
	// close-out fence's red proof — one shim, not two).
	pbFenceShimScript = "testdata/ssh-sentinel-shim.sh"

	// pbFenceRealNodeAlias is an ssh-looking peer name the shim answers for. It
	// is NOT resolved by a real ssh, so a RED run never opens a connection.
	pbFenceRealNodeAlias = "gopi-pushback-fence-shim"

	// pbFenceReExecMarker makes the child re-entry opt-in: the parent sets it,
	// and the child test skips without it, so a plain package run pays the
	// child's compile-and-run cost exactly once, from its own test.
	pbFenceReExecMarker = "GOPI_PUSHBACK_FENCE_REEXEC"

	// pbFenceChildTestName is the test the child runs by name.
	pbFenceChildTestName = "TestPushbackTestFenceReExecChildBody"

	// pbFenceNoRefs is the reading of a repository that owns no branch — what
	// the sentinel must still print after a fenced run.
	pbFenceNoRefs = ""
)

// pbFenceReading is one run's evidence: what the sentinel ended up owning, what
// the lane reported, and the shim's own log of the ssh it was handed.
type pbFenceReading struct {
	sentinelBefore string
	sentinelAfter  string
	failures       string
	sshLog         []string
}

// leaked reports whether a push landed in the sentinel.
func (r pbFenceReading) leaked() bool {
	return r.sentinelAfter != r.sentinelBefore
}

// String renders the reading as the child prints it and the parent parses it:
// one field per line, prefixed, so a truncated `go test -v` window is still
// unambiguous.
func (r pbFenceReading) String() string {
	return strings.Join([]string{
		pbFenceSentinelBeforePrefix + r.sentinelBefore,
		pbFenceSentinelAfterPrefix + r.sentinelAfter,
		pbFenceFailuresPrefix + r.failures,
		pbFenceSSHLogPrefix + strings.Join(r.sshLog, pbFenceSSHLogJoin),
	}, "\n")
}

const (
	pbFenceSentinelBeforePrefix = "SENTINEL-BEFORE="
	pbFenceSentinelAfterPrefix  = "SENTINEL-AFTER="
	pbFenceFailuresPrefix       = "FAILURES="
	pbFenceSSHLogPrefix         = "SSH-LOG="
	pbFenceSSHLogJoin           = " | "
)

// pbFenceRun builds a client whose only non-integration branch is an ORPHAN
// ROOT fixture deliverable (the residue's own shape), installs the sentinel ssh
// so the target's receiver is the test's own bare repo, and drives the
// production entry `gitPushbackPlan` with a real node's target.
//
// The target's LOCAL side is the process's own `~/work/GoPi` — on the loopback
// host the alias resolves to this very machine, which is the whole mechanism
// (memory #4709/#4710). The client is a temp clone with NO remote configured,
// so git's own fallback cannot be the answer here: the target is what names the
// recipient.
func pbFenceRun(t *testing.T) pbFenceReading {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not installed")
	}
	if _, err := os.Stat(pbFenceShimScript); err != nil {
		// The sentinel is what makes a RED run safe (it can never reach the
		// path the target names). Without it the honest thing is to SKIP, not
		// to run the leak for real.
		t.Skipf("sentinel shim %s missing: %v", pbFenceShimScript, err)
	}
	shim := installLeakSSHShim(t)
	t.Setenv("GOPI_TMPDIR", t.TempDir()) // the suite's temp root, pinned (Rule 22)

	w := newPBWorldUnfenced(t)
	w.git(w.client, "config", "user.email", "t@t")
	w.git(w.client, "config", "user.name", "t")

	// The RECEIVER is the fixture's own bare repo (w.server) and the fixture's
	// client is a clone OF it, so the deliverable below is a real, ff-able
	// branch of the peer's trunk — the shape this lane exists to deliver.
	// Pointing the shim at it is what makes a RED run observable: the pushed
	// bytes land in a repo the test owns, NEVER at the path the target names
	// (Rule 30). On a GREEN run nothing arrives at all.
	t.Setenv(leakShimRepoEnv, w.server)

	// The fixture deliverable: an ordinary branch of the peer's trunk. (An
	// ORPHAN root would be refused by this lane's own ancestry gate —
	// gitPushbackSharesHistoryWithMaster — before any push is attempted, which
	// is a reading this fence does NOT own and the report records separately.)
	w.git(w.client, "checkout", "-q", "-b", pbFenceFixtureBranch)
	pbWrite(t, w.client, pbFenceFixtureFile, pbFenceFixtureBlob)
	w.git(w.client, "add", pbFenceFixtureFile)
	w.git(w.client, "commit", "-q", "-m", "deliverable work")
	w.git(w.client, "checkout", "-q", masterBranchName)

	before := pbHeadRefs(t, w.server)
	flush := gitPushbackPlan(context.Background(), w.client, pbFenceTarget())
	return pbFenceReading{
		sentinelBefore: before,
		sentinelAfter:  pbHeadRefs(t, w.server),
		failures:       strings.Join(flush.Failed, " ; "),
		sshLog:         shim.invocations(t),
	}
}

// pbFenceTarget renders the target a real node's checkout resolves to: the ssh
// alias plus the peer repo path the production constants carry (Rules 13/14 —
// the path is referenced, never re-typed).
func pbFenceTarget() string {
	return pbFenceRealNodeAlias + ":" + peerRepoPath()
}

// pbHeadRefs snapshots a repository's refs/heads namespace (object name +
// refname per line): the ground truth for "did a push land here", taken from
// git itself rather than from the lane's report.
func pbHeadRefs(t *testing.T, repo string) string {
	t.Helper()
	out, code := pbGitMaybe(t, repo, "for-each-ref", "--format=%(objectname) %(refname:short)", "refs/heads")
	if code != 0 {
		t.Fatalf("for-each-ref in %s failed (%d): %s", repo, code, out)
	}
	return strings.TrimSpace(out)
}

// TestPushbackTestFenceRefusesARealNodeTarget is observation ①: the in-process
// arm. GREEN means the lane refused AND the sentinel — the stand-in for
// whatever the target names — owns no branch; RED (fence absent or opted out)
// means the sentinel gained the fixture's branch.
func TestPushbackTestFenceRefusesARealNodeTarget(t *testing.T) {
	got := pbFenceRun(t)

	if got.leaked() {
		t.Fatalf("LEAK: the push-back executed a push to a real node's target and the receiver gained a ref: %q → %q (ssh argv: %v)",
			got.sentinelBefore, got.sentinelAfter, got.sshLog)
	}
	if !strings.Contains(got.failures, finishCleanupTestFenceMarker) {
		t.Fatalf("the lane must REFUSE by name (%q), got %q", finishCleanupTestFenceMarker, got.failures)
	}
	// The refusal must name the blocked target — an operator reading it has to
	// see WHERE the push was headed (Rule 15).
	if !strings.Contains(got.failures, pbFenceRealNodeAlias) {
		t.Errorf("the refusal must name the blocked target, got %q", got.failures)
	}
	// And it must name the way out, so a fixture that legitimately needs the
	// target can say so in one place.
	if !strings.Contains(got.failures, finishCleanupInProcessTestMarker) {
		t.Errorf("the refusal must name the opt-out %s, got %q", finishCleanupInProcessTestMarker, got.failures)
	}
}

// TestPushbackTestFenceReExecChildBody is observation ②'s child half. It is
// SKIPPED unless the parent set the marker, so a plain package run does not pay
// for it and the child's output is only produced when it is being read.
//
// The child reports through its own stdout: the three readings, printed. The
// parent parses them. Nothing in the child re-asserts the fence — that is the
// point: this is the same lane, driven by a real subprocess, where
// testing.Testing() is false.
func TestPushbackTestFenceReExecChildBody(t *testing.T) {
	if strings.TrimSpace(os.Getenv(pbFenceReExecMarker)) == "" {
		t.Skipf("child re-entry; %s not set", pbFenceReExecMarker)
	}
	got := pbFenceRun(t)
	t.Logf("\n%s", got)
}

// TestPushbackTestFenceRefusesAReExecChild is observation ②'s parent half: a
// REAL subprocess runs the same lane with the same target, and the child's own
// reading is what the assertion is made on.
//
// WHY THIS IS NOT REDUNDANT WITH ①: a re-exec'd child reports
// testing.Testing()==false, so a fence that asked only that question would let
// this arm push (that is the gap the marker env exists to close). This test is
// therefore the one that fails if the marker half is dropped.
func TestPushbackTestFenceRefusesAReExecChild(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not installed")
	}
	if strings.TrimSpace(os.Getenv(pbFenceReExecMarker)) != "" {
		t.Skip("already inside the child")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cmd := exec.Command("go", "test", ".", "-run", "^"+pbFenceChildTestName+"$", "-count=1", "-v")
	cmd.Dir = cwd
	// os.Environ copies the opt-out if the operator set it — which is exactly
	// how the RED control is run. The marker is what makes the child DO the
	// work, never what relaxes the fence.
	cmd.Env = append(os.Environ(), pbFenceReExecMarker+"=1")
	out, runErr := cmd.CombinedOutput()
	if runErr != nil && !pbChildRanAndSkipped(string(out)) {
		t.Fatalf("child run failed: %v\n%s", runErr, out)
	}
	child := pbParseChildReading(t, string(out))

	if child.leaked() {
		t.Fatalf("LEAK: a RE-EXEC'D child pushed to a real node's target and the receiver gained a ref: %q → %q (ssh argv: %v)",
			child.sentinelBefore, child.sentinelAfter, child.sshLog)
	}
	if !strings.Contains(child.failures, finishCleanupTestFenceMarker) {
		t.Fatalf("the child's lane must REFUSE by name (%q), got %q", finishCleanupTestFenceMarker, child.failures)
	}
}

// pbParseChildReading pulls the three prefixed lines out of the child's output.
// A missing line is a failure, not a zero value: a child that printed nothing
// must never read as "no leak".
func pbParseChildReading(t *testing.T, out string) pbFenceReading {
	t.Helper()
	var r pbFenceReading
	found := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, pbFenceSentinelBeforePrefix):
			r.sentinelBefore = strings.TrimPrefix(line, pbFenceSentinelBeforePrefix)
			found++
		case strings.HasPrefix(line, pbFenceSentinelAfterPrefix):
			r.sentinelAfter = strings.TrimPrefix(line, pbFenceSentinelAfterPrefix)
			found++
		case strings.HasPrefix(line, pbFenceFailuresPrefix):
			r.failures = strings.TrimPrefix(line, pbFenceFailuresPrefix)
			found++
		case strings.HasPrefix(line, pbFenceSSHLogPrefix):
			body := strings.TrimPrefix(line, pbFenceSSHLogPrefix)
			if body != "" {
				r.sshLog = strings.Split(body, pbFenceSSHLogJoin)
			}
			found++
		}
	}
	if found < pbFenceReadingFields {
		t.Fatalf("the child printed %d of %d readings — the assertion would be vacuous; output:\n%s",
			found, pbFenceReadingFields, out)
	}
	return r
}

// pbFenceReadingFields is how many prefixed lines pbFenceReading.String prints.
const pbFenceReadingFields = 4

// pbChildRanAndSkipped reports whether the child's output shows it ran the
// body (rather than failing to build, or skipping for a missing tool). A skip
// is legitimate for an absent `sh`/sentinel, and the parent must not turn that
// into a pass or a failure it did not measure.
func pbChildRanAndSkipped(out string) bool {
	return strings.Contains(out, pbFenceSSHLogPrefix)
}

// TestPushbackTestFenceAllowsAFixturesOwnTempRepo is observation ③, the
// positive control: the SAME lane, the SAME fixture, a target that is a bare
// repo inside the test's own temp root. It must deliver.
//
// This is also the guard against the quiet failure mode this fence could
// introduce: a predicate that stops recognising test space would turn every
// fixture push into a refusal, and only this direction would notice.
func TestPushbackTestFenceAllowsAFixturesOwnTempRepo(t *testing.T) {
	w := newPBWorld(t)
	t.Setenv("GOPI_TMPDIR", w.root)
	w.git(w.client, "checkout", "-q", "-b", pbFenceFixtureBranch)
	pbWrite(t, w.client, "deliverable.txt", "deliverable\n")
	w.git(w.client, "add", "deliverable.txt")
	w.git(w.client, "commit", "-q", "-m", "deliverable work")

	flush := gitPushbackPlan(context.Background(), w.client, w.server)

	if len(flush.Failed) != 0 {
		t.Fatalf("a fixture pushing to its OWN temp-rooted repo must be allowed, got failures: %v", flush.Failed)
	}
	if !pushbackContains(flush.Pushed, pbFenceFixtureBranch) {
		t.Fatalf("the fixture's own repo must RECEIVE the branch, pushed=%v", flush.Pushed)
	}
	if !w.serverRefExists(pbFenceFixtureBranch) {
		t.Fatalf("the fixture's own bare repo holds no refs/heads/%s after the push", pbFenceFixtureBranch)
	}
}
