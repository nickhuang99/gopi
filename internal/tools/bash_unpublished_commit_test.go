package tools

// bash_unpublished_commit_test.go — THE RED PROOF for bash_unpublished_commit.go.
//
// The mechanism's whole claim is a sentence about a REAL repository state, so the
// fixture builds that state with real git: a bare "origin", a clone whose
// `refs/remotes/origin/master` exists, and then a commit made in the clone that
// no remote-tracking ref reaches — the exact shape a chat's refused `git push`
// leaves behind (measured 2026-10-24, revision d50f746d7, which existed on no
// server while being installed into /usr/local/bin/gopi).
//
// RED PROOF (run and recorded in the task report): delete the call to
// unpublishedCommitNotice at the end of bashInDirCtx and
// TestUnpublishedCommitNoticeNamesTheCommit fails on its FIRST assertion — the
// sha is absent from the result — while the two SILENCE arms keep passing, which
// is what makes them silence arms rather than a second copy of the positive one.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// unpublishedFixtureGit runs one git command in dir and returns its stdout,
// failing the test on error. It is a local helper (not gitIn, which returns the
// error) because every step of a fixture setup is "this MUST have worked".
func unpublishedFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// unpublishedFixtureClone builds origin + clone and returns the clone's path,
// standing on a branch whose tip no remote-tracking ref reaches.
//
// The branch is named explicitly (`symbolic-ref HEAD refs/heads/master`) rather
// than left to git's default-branch configuration: this fixture asserts on the
// branch name it prints, so the name cannot be whatever init.defaultBranch says
// on the host running the suite.
func unpublishedFixtureClone(t *testing.T) string {
	t.Helper()
	root := testutil.TestScratchDir(t, "unpublished")
	origin := filepath.Join(root, "origin.git")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatalf("mkdir origin: %v", err)
	}
	unpublishedFixtureGit(t, origin, "init", "--bare", "-q", ".")

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	unpublishedFixtureGit(t, seed, "init", "-q", ".")
	unpublishedFixtureGit(t, seed, "symbolic-ref", "HEAD", "refs/heads/master")
	unpublishedFixtureGit(t, seed, "config", "user.email", "unpublished@test.invalid")
	unpublishedFixtureGit(t, seed, "config", "user.name", "unpublished test")
	if err := os.WriteFile(filepath.Join(seed, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatalf("seed base.txt: %v", err)
	}
	unpublishedFixtureGit(t, seed, "add", "base.txt")
	unpublishedFixtureGit(t, seed, "commit", "-q", "-m", "base")
	unpublishedFixtureGit(t, seed, "remote", "add", "origin", origin)
	unpublishedFixtureGit(t, seed, "push", "-q", "origin", "master")

	clone := filepath.Join(root, "clone")
	unpublishedFixtureGit(t, root, "clone", "-q", origin, clone)
	unpublishedFixtureGit(t, clone, "config", "user.email", "unpublished@test.invalid")
	unpublishedFixtureGit(t, clone, "config", "user.name", "unpublished test")
	// The clone must really carry the tracking ref the probe counts against,
	// or "unreachable from every remote" would be true for the wrong reason.
	if out := unpublishedFixtureGit(t, clone, "rev-parse", "--verify", "refs/remotes/origin/master"); strings.TrimSpace(out) == "" {
		t.Fatalf("the fixture clone carries no refs/remotes/origin/master — the negative measurement would be vacuous")
	}
	return clone
}

// unpublishedFixtureCommit writes a file and commits it, returning the new HEAD.
func unpublishedFixtureCommit(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	unpublishedFixtureGit(t, dir, "add", name)
	unpublishedFixtureGit(t, dir, "commit", "-q", "-m", "feat: "+name)
	return strings.TrimSpace(unpublishedFixtureGit(t, dir, "rev-parse", "HEAD"))
}

// TestUnpublishedCommitNoticeNamesTheCommit is the ACCEPTANCE arm: after a real
// commit in a real clone, the tool's RESULT carries the commit's full sha, its
// branch, the count and the two routes out of the state.
func TestUnpublishedCommitNoticeNamesTheCommit(t *testing.T) {
	dir := unpublishedFixtureClone(t)
	t.Setenv(config.ChatSurfaceIdentityEnv, config.FormatChatSurfaceIdentity(os.Getpid(), dir))

	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}
	out, err := BashInDir(`git add . && git commit -m "feat: the work nobody published"`, dir)
	if err != nil {
		t.Fatalf("bash commit failed: %v\n%s", err, out)
	}
	sha := strings.TrimSpace(unpublishedFixtureGit(t, dir, "rev-parse", "HEAD"))

	// The state the notice is about really exists: prove it before asserting on
	// the sentence, so a green cannot come from a fixture that never got ahead.
	ahead := strings.TrimSpace(unpublishedFixtureGit(t, dir, "rev-list", "--count", "HEAD", "--not", "--remotes"))
	if ahead != "1" {
		t.Fatalf("fixture is not in the measured state: `rev-list --count HEAD --not --remotes` = %q, want 1", ahead)
	}

	if !strings.Contains(out, sha) {
		t.Fatalf("the result never named the commit %s — a notice that cannot be copied cannot be acted on.\n%s", sha, out)
	}
	for _, want := range []string{
		unpublishedProbeCommand(),
		"master",
		"reachable from NO remote-tracking ref",
		"no publication path",
		"gopi task submit",
		unpublishedNothingPushedClaim,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the notice is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "answered 1 in "+dir) {
		t.Fatalf("the notice does not quote the measurement that produced it (expected `answered 1 in %s`):\n%s", dir, out)
	}
}

// TestUnpublishedCommitNoticeSilentForAWorkerGround is the GATE arm: the same
// repository state, the same command, and NO chat identity — a worker's ground is
// published by the clone channel's delivery loop (Rule 6c), so the claim "there
// is no publication path" would be FALSE there and must not be printed.
func TestUnpublishedCommitNoticeSilentForAWorkerGround(t *testing.T) {
	dir := unpublishedFixtureClone(t)
	t.Setenv(config.ChatSurfaceIdentityEnv, "")

	if err := os.WriteFile(filepath.Join(dir, "worker.txt"), []byte("worker\n"), 0o600); err != nil {
		t.Fatalf("write worker.txt: %v", err)
	}
	out, err := BashInDir(`git add . && git commit -m "feat: a worker ground"`, dir)
	if err != nil {
		t.Fatalf("bash commit failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "no publication path") {
		t.Fatalf("a worker ground was told it has no publication path — its delivery loop is exactly that path:\n%s", out)
	}
}

// TestUnpublishedCommitNoticeSilentWhenReachable is the MEASUREMENT arm: the
// commit IS reachable from a remote-tracking ref, so there is no lost work and
// nothing to say. Without this arm the notice could be firing on every commit and
// the positive test would still pass.
func TestUnpublishedCommitNoticeSilentWhenReachable(t *testing.T) {
	dir := unpublishedFixtureClone(t)
	t.Setenv(config.ChatSurfaceIdentityEnv, config.FormatChatSurfaceIdentity(os.Getpid(), dir))

	// THE STATE IS BUILT INSIDE THE ONE COMMAND the notice is about, because the
	// commit does not exist before it runs: the command creates the commit AND
	// gives it a remote-tracking home, so the measurement at notice time answers
	// 0. The ref is a remote-tracking one (never refs/heads/, which the shared
	// integration-ref guard owns) and the assertion is about REACHABILITY, not
	// about which remote or branch name carries it.
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("visible\n"), 0o600); err != nil {
		t.Fatalf("write visible.txt: %v", err)
	}
	out, err := BashInDir(`git add . && git commit -m "feat: visible" && `+
		`git update-ref refs/remotes/origin/published HEAD`, dir)
	if err != nil {
		t.Fatalf("bash commit failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(unpublishedFixtureGit(t, dir, "rev-list", "--count", "HEAD", "--not", "--remotes")); got != "0" {
		t.Fatalf("fixture setup failed: %q commit(s) are still unreachable, want 0", got)
	}
	if strings.Contains(out, "no publication path") {
		t.Fatalf("a commit reachable from refs/remotes/origin/published was reported as unpublished:\n%s", out)
	}
}

// TestUnpublishedCommitNoticeSkipsNonCommittingCommands pins the third
// precondition: a chat child running read-only git (or any command at all) in the
// same ahead-of-every-remote state gets no notice, because no commit was created
// by the command being reported on.
func TestUnpublishedCommitNoticeSkipsNonCommittingCommands(t *testing.T) {
	dir := unpublishedFixtureClone(t)
	t.Setenv(config.ChatSurfaceIdentityEnv, config.FormatChatSurfaceIdentity(os.Getpid(), dir))
	sha := unpublishedFixtureCommit(t, dir, "ahead.txt")

	out, err := BashInDir("git status --porcelain", dir)
	if err != nil {
		t.Fatalf("bash status failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "no publication path") || strings.Contains(out, sha) {
		t.Fatalf("a read-only command reported the commit as unpublished — the notice is tied to a command that "+
			"MADE a commit, not to the repository state alone:\n%s", out)
	}

	// And the same state DOES report when the command is the commit that made it.
	if err := os.WriteFile(filepath.Join(dir, "ahead2.txt"), []byte("ahead2\n"), 0o600); err != nil {
		t.Fatalf("write ahead2.txt: %v", err)
	}
	out2, err := BashInDir(`git add . && git commit -m "feat: ahead2"`, dir)
	if err != nil {
		t.Fatalf("bash commit failed: %v\n%s", err, out2)
	}
	again := strings.TrimSpace(unpublishedFixtureGit(t, dir, "rev-parse", "HEAD"))
	if !strings.Contains(out2, again) {
		t.Fatalf("the notice did not name %s after the commit that created it:\n%s", again, out2)
	}
}
