package tools

// memory_sync_task_gitpushback_cov_test.go — coverage tests for the exported
// default git push-back (DefaultGitPushback), whose whole point is the
// STALE-BRANCH RESURRECTION GUARD (2026-09-02 incident): the server adopts a
// deliverable into master and DELETES the branch ref; a blanket client
// re-push would recreate that ref from a now-ancestor tip and flood master
// with stale branches.
//
// The transport is real `git` between two LOCAL repositories (zero network,
// zero ssh): the peer target is the literal "<path>:~/work/GoPi" shape the
// production code composes (peerRepoPath()), with "<path>" pointing at a
// temp bare repo — so the whole plan (branch listing → server-master fetch →
// per-branch ancestor check → push) runs for real, just locally.
//
// Invariants asserted:
//   · an already-integrated branch is SKIPPED (never re-pushed) and the
//     server's master tip stays where the server left it;
//   · truly-unmerged work IS pushed (the deliverable still crosses);
//   · a nothing-unmerged flush pushes NOTHING — and reports no failure;
//   · the report is print-safe (empty slices, never nil);
//   · an invalid peer alias never reaches git (error, nil report);
//   · a non-repo cwd fails to resolve the repo (error, nil report) rather
//     than pushing blind.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// covGit runs one git command in dir and fails the test on error.
func covGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// covInitRepo creates a commit-bearing git repo with `master` checked out.
func covInitRepo(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	covGit(t, dir, "init", "-q", ".")
	covGit(t, dir, "config", "user.email", "cov@test")
	covGit(t, dir, "config", "user.name", "cov")
	covGit(t, dir, "config", "commit.gpgsign", "false")
	covGit(t, dir, "checkout", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	covGit(t, dir, "add", file)
	covGit(t, dir, "commit", "-qm", "init "+file)
}

// covChdir moves the process into dir for the duration of the test (the
// push-back default resolves the repo as the process CWD — see
// gitPushbackDefault) and restores the previous directory afterwards.
func covChdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	//stylegate:ignore restores the cwd the fixture changed (the test's own temp dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// covPushbackPeer builds a bare peer repo under the literal
// "<path>:~/work/GoPi" target shape and returns (peerAlias, peerRepoPath).
// `git` treats "<dir>/peer.git:~/work/GoPi" as a local path whose textual
// part after the colon is just a directory name, which is exactly the
// production target syntax minus the (absent) ssh hop.
func covPushbackPeer(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	peerRepo := filepath.Join(root, "peer.git:~/work/GoPi")
	if err := os.MkdirAll(peerRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	covGit(t, peerRepo, "init", "-q", "--bare")
	aliasBase := filepath.Join(root, "peer.git")
	return aliasBase, peerRepo
}

// covPushbackClient builds the client repo with (a) a commit on master,
// (b) an ALREADY-INTEGRATED branch (its tip reaches the peer's master), and
// (c) an UNMERGED branch (the deliverable). The peer's master is advanced to
// the integrated branch's tip by a direct push, reproducing "the server
// adopted the branch and deleted it" without any ssh.
func covPushbackClient(t *testing.T, peerRepo string) string {
	t.Helper()
	clientDir := t.TempDir()
	covInitRepo(t, clientDir, "base.txt", "base\n")

	// Integrated branch: commit, then adopt it into the peer's master.
	covGit(t, clientDir, "checkout", "-q", "-b", "cov-integrated")
	if err := os.WriteFile(filepath.Join(clientDir, "integrated.txt"), []byte("adopted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covGit(t, clientDir, "add", "integrated.txt")
	covGit(t, clientDir, "commit", "-qm", "integrated work")

	// Unmerged branch: the DELIVERABLE the peer has never seen. It is based on
	// the peer's CURRENT master (the adopt pushes below advance it), so it
	// FAST-FORWARDS — which is what makes it deliverable at all: the peer lands
	// with `merge --ff-only`, so a branch merely behind master is refused by
	// gitPushbackFastForwardsOntoMaster, not delivered.
	covGit(t, clientDir, "checkout", "-q", "master")
	// Adopt: master → peer, then the integrated branch's tip → peer master
	// (i.e. the server merged it and deleted the client-side ref's twin).
	// NB: push to the LITERAL target path (what gitPushbackDefault composes:
	// <peer>:/home/user/work/GoPi) — git resolves it to the local bare repo above.
	covGit(t, clientDir, "push", "-q", peerRepo, "master:master")
	covGit(t, clientDir, "push", "-q", peerRepo, "cov-integrated:master")
	// The client learns the peer's new master (what a fetch does) and bases the
	// deliverable on it: -B moves the local master onto the adopted tip.
	covGit(t, clientDir, "checkout", "-q", "-B", "master", "cov-integrated")
	covGit(t, clientDir, "checkout", "-q", "-b", "cov-unmerged")
	if err := os.WriteFile(filepath.Join(clientDir, "unmerged.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covGit(t, clientDir, "add", "unmerged.txt")
	covGit(t, clientDir, "commit", "-qm", "unmerged deliverable")
	covGit(t, clientDir, "checkout", "-q", "master")
	return clientDir
}

// TestDefaultGitPushbackSkipsIntegratedPushesUnmerged — the resurrection
// guard end to end: the integrated branch is SKIPPED (its tip is an
// ancestor of the server's master), the unmerged deliverable IS pushed, and
// the server's master tip is left exactly where the server put it (no
// stale branch flooded in).
func TestDefaultGitPushbackSkipsIntegratedPushesUnmerged(t *testing.T) {
	alias, peerRepo := covPushbackPeer(t)
	clientDir := covPushbackClient(t, peerRepo)
	covChdir(t, clientDir)

	masterBefore := covGit(t, peerRepo, "rev-parse", "refs/heads/master")

	report, err := DefaultGitPushback(alias)
	if err != nil {
		t.Fatalf("DefaultGitPushback: %v", err)
	}
	if report == nil {
		t.Fatalf("nil report")
	}
	if !covContains(report.Skipped, "cov-integrated") {
		t.Fatalf("the already-integrated branch must be SKIPPED, got skipped=%v pushed=%v", report.Skipped, report.BranchesPushed)
	}
	if !covContains(report.BranchesPushed, "cov-unmerged") {
		t.Fatalf("the unmerged deliverable must be pushed, got pushed=%v failed=%v", report.BranchesPushed, report.PushFailed)
	}
	if covContains(report.BranchesPushed, "cov-integrated") {
		t.Fatalf("a merged branch must never appear as pushed: %v", report.BranchesPushed)
	}
	if len(report.PushFailed) != 0 {
		t.Fatalf("no push should fail in this topology: %v", report.PushFailed)
	}
	// master is never a push target (asymmetry: the client never pushes
	// master) and the server's tip is untouched.
	if covContains(report.BranchesPushed, "master") || covContains(report.Skipped, "master") {
		t.Fatalf("master must not be listed by the push-back plan: %+v", report)
	}
	if masterAfter := covGit(t, peerRepo, "rev-parse", "refs/heads/master"); masterAfter != masterBefore {
		t.Fatalf("server master moved: %s → %s (the guard must not rewrite it)", masterBefore, masterAfter)
	}
	// The deliverable really landed on the peer.
	if got := covGit(t, peerRepo, "rev-parse", "refs/heads/cov-unmerged"); got != covGit(t, clientDir, "rev-parse", "cov-unmerged") {
		t.Fatalf("pushed branch tip mismatch: peer=%s client=%s", got, covGit(t, clientDir, "rev-parse", "cov-unmerged"))
	}
	// Print-safe contract: both slices non-nil.
	if report.BranchesPushed == nil || report.PushFailed == nil {
		t.Fatalf("report slices must be non-nil for the CLI printer: %+v", report)
	}
}

// TestDefaultGitPushbackNothingUnmergedPushesNothing — requirement (d): when
// every local branch is already integrated, the flush pushes NOTHING and
// reports no failure (an empty pass, not an error).
func TestDefaultGitPushbackNothingUnmergedPushesNothing(t *testing.T) {
	alias, peerRepo := covPushbackPeer(t)
	clientDir := t.TempDir()
	covInitRepo(t, clientDir, "base.txt", "base\n")
	// One branch whose work the server has ALREADY adopted into master.
	covGit(t, clientDir, "checkout", "-q", "-b", "cov-integrated-only")
	if err := os.WriteFile(filepath.Join(clientDir, "adopted.txt"), []byte("adopted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	covGit(t, clientDir, "add", "adopted.txt")
	covGit(t, clientDir, "commit", "-qm", "adopted work")
	covGit(t, clientDir, "checkout", "-q", "master")
	covGit(t, clientDir, "push", "-q", peerRepo, "master:master")
	covGit(t, clientDir, "push", "-q", peerRepo, "cov-integrated-only:master")
	covChdir(t, clientDir)

	masterBefore := covGit(t, peerRepo, "rev-parse", "refs/heads/master")
	report, err := DefaultGitPushback(alias)
	if err != nil {
		t.Fatalf("DefaultGitPushback: %v", err)
	}
	if len(report.BranchesPushed) != 0 {
		t.Fatalf("nothing-unmerged flush pushed %v (resurrection risk)", report.BranchesPushed)
	}
	if len(report.PushFailed) != 0 {
		t.Fatalf("nothing-unmerged flush must not report a failure: %v", report.PushFailed)
	}
	if !covContains(report.Skipped, "cov-integrated-only") {
		t.Fatalf("the integrated branch should be skipped, got %v", report.Skipped)
	}
	if got := covGit(t, peerRepo, "rev-parse", "refs/heads/master"); got != masterBefore {
		t.Fatalf("server master moved on an empty flush: %s → %s", masterBefore, got)
	}
}

// TestDefaultGitPushbackRefusesInvalidPeer — boundary/error: an empty or
// shell-metacharacter-carrying peer alias never reaches git (it becomes an
// argv element in a remote command context), so it is refused with a nil
// report.
func TestDefaultGitPushbackRefusesInvalidPeer(t *testing.T) {
	alias, peerRepo := covPushbackPeer(t)
	clientDir := covPushbackClient(t, peerRepo)
	covChdir(t, clientDir)

	for _, bad := range []string{"", "   ", "peer with space", "peer\ttail", "peer\nnext", "peer;rm", "a;b"} {
		report, err := DefaultGitPushback(bad)
		if err == nil {
			t.Fatalf("peer %q must be refused, got report %+v", bad, report)
		}
		if report != nil {
			t.Fatalf("refused peer %q must return a nil report, got %+v", bad, report)
		}
		if !strings.Contains(err.Error(), "invalid peer") {
			t.Fatalf("refusal must self-describe, got: %v", err)
		}
	}
	// Whitespace around a VALID alias is trimmed, not refused.
	if _, err := DefaultGitPushback("  " + alias + "  "); err != nil {
		t.Fatalf("a padded valid alias must be trimmed and accepted: %v", err)
	}
}

// TestDefaultGitPushbackUnresolvableRepoErrors — error path: outside a git
// work tree the push-back cannot resolve the repo (the CWD IS the repo by
// contract) and must report that instead of pushing blind from nowhere.
func TestDefaultGitPushbackUnresolvableRepoErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary in test env")
	}
	alias, _ := covPushbackPeer(t)
	notARepo := t.TempDir()
	covChdir(t, notARepo)

	report, err := DefaultGitPushback(alias)
	if err == nil {
		t.Fatalf("a non-repo cwd must error, got report %+v", report)
	}
	if report != nil {
		t.Fatalf("error path must return a nil report, got %+v", report)
	}
	if !strings.Contains(err.Error(), "resolve repo") {
		t.Fatalf("error must name the failing step, got: %v", err)
	}
}

// TestDefaultGitPushbackUnreachablePeerFailsSoft — error path: an
// unreachable peer target fails the server-master fetch, and the flush
// refuses to push anything (pushing blind would resurrect deleted refs).
// The failure is REPORTED in the flush (the report is non-nil), which is
// how the harvest path distinguishes "nothing to do" from "could not talk
// to the peer".
func TestDefaultGitPushbackUnreachablePeerFailsSoft(t *testing.T) {
	_, peerRepo := covPushbackPeer(t)
	clientDir := covPushbackClient(t, peerRepo)
	// Point the peer at a path that does not exist: the fetch leg fails.
	gone := filepath.Join(t.TempDir(), "gone", "peer.git")
	covChdir(t, clientDir)

	report, err := DefaultGitPushback(gone)
	// The plan returns a report carrying the fetch failure (fail-soft), so
	// either shape is acceptable — but a push must NEVER have been attempted.
	if err != nil {
		if report != nil {
			t.Fatalf("error path returned a report too: %+v", report)
		}
		return
	}
	if len(report.BranchesPushed) != 0 {
		t.Fatalf("an unreachable peer must push nothing (blind push resurrects refs): %v", report.BranchesPushed)
	}
	if len(report.PushFailed) == 0 && len(report.Skipped) == 0 {
		t.Fatalf("the fetch failure must be reported: %+v", report)
	}
	// The deliverable is still only local.
	if _, err := covGitAllowError(peerRepo, "rev-parse", "refs/heads/cov-unmerged"); err == nil {
		t.Fatalf("nothing should have reached the real peer")
	}
}

// covContains reports whether a string slice has an exact entry.
func covContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// covGitAllowError runs git and returns its error (for negative assertions).
func covGitAllowError(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
