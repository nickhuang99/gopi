package devworkspace

// The channel's contract, proved on REAL git: a clone from a declared repo, the
// pull --rebase → build → push loop, a push REFUSED because somebody landed
// first (the loop must pull again and still push), the BRAKE (N refusals stop
// the loop and keep the workspace), and the reclaim.
//
// Nothing here fakes git: the "remote" is a local repository two directories
// away, so every classification the loop makes (refused vs hard error) is
// measured the way production measures it.

import (
	"context"
	"errors"
	"fmt"
	"github.com/nickhuang99/gopi/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// testTaskID is a task-id-shaped name: the workspace directory is named
	// after it, and the test asserts that name.
	testTaskID = "01a141a7-0000-7000-8000-00000000abcd"
	// testBuildCommand is a DECLARED build command that leaves a recognisable
	// mark, so "the build that ran was the declared one" is checkable.
	testBuildCommand = "echo BUILT-BY-DECLARATION > build-stamp.txt"
)

// gitIn runs one git command in dir with a pinned identity (so the test does
// not depend on the host's git config).
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// newOrigin builds an "origin" repository with one commit on master.
func newOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", DefaultBranch)
	// A push INTO a non-bare repository is refused unless the receiver allows
	// it ("branch is currently checked out"). The fixture is a stand-in for a
	// hosting server, so it allows it — otherwise every push would fail for a
	// reason that has nothing to do with the loop.
	gitIn(t, dir, "config", "receive.denyCurrentBranch", "ignore")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("origin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

// racingBuildCommand writes a DECLARED build command that lands a commit on
// origin while it "builds" — the race the loop's refusal path exists for. A
// push is only ever refused when origin moves BETWEEN the loop's own pull and
// its push, and the build step is exactly that window.
//
// times = 1 races once (the retry test); times < 0 races on every attempt (the
// brake test). The guard file lives OUTSIDE the workspace so `pull --rebase`
// cannot touch it and a reclaim cannot reset the count.
func racingBuildCommand(t *testing.T, origin string, times int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "build.sh")
	counter := filepath.Join(dir, "races")
	guard := `if [ "$n" -lt ` + strconv.Itoa(times) + ` ]; then`
	if times < 0 {
		guard = `if [ 1 = 1 ]; then`
	}
	body := strings.NewReplacer(
		"@GUARD@", guard,
		"@COUNTER@", counter,
		"@ORIGIN@", origin,
	).Replace(racingBuildTemplate)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// racingBuildTemplate is the build script's body; the three @…@ slots are what
// makes it a fixture rather than a literal (Rule 1b's spirit applied to a test).
const racingBuildTemplate = `#!/bin/sh
set -e
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t
export GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
echo BUILT-BY-DECLARATION > build-stamp.txt
n=0
[ -f @COUNTER@ ] && n=$(cat @COUNTER@)
@GUARD@
echo $((n+1)) > @COUNTER@
echo theirs > @ORIGIN@/theirs-$n.txt
git -C @ORIGIN@ add -A >/dev/null
git -C @ORIGIN@ commit -q -m "theirs: race $n" >/dev/null
fi
`

// ifRacing is the shell guard that decides whether THIS build races.
func ifRacing(times int) string {
	if times < 0 {
		return `if [ 1 = 1 ]; then`
	}
	return `if [ "$n" -lt ` + strconv.Itoa(times) + ` ]; then`
}

// cloneCfg is the channel's config pointed at a throwaway root.
func cloneCfg(t *testing.T, origin string) Config {
	t.Helper()
	return Config{
		TaskID:  testTaskID,
		DevRepo: origin,
		Root:    t.TempDir(),
		Build:   testBuildCommand,
		// The loop's waits are real but bounded by the brake: three attempts
		// cost 0.5s + 2s worst case, and the test only ever needs fewer.
		BuildTimeout: 60 * time.Second,
		Log:          testWriter{t},
	}
}

// testWriter forwards the channel's narration into the test log, so a failure
// shows the loop's own account of what it did.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// TestOpenClonesTheDeclaredRepoAndStampsItsGround proves the channel's entry
// step: the workspace is a CLONE of the declared repo at workspace_root/<id>,
// not a linked worktree of anything.
func TestOpenClonesTheDeclaredRepoAndStampsItsGround(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)

	res, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := filepath.Join(cfg.Root, testTaskID)
	if res.Dir != want {
		t.Fatalf("workspace = %q, want %q (workspace_root/<task-id>)", res.Dir, want)
	}
	if !isGitWorkTree(res.Dir) {
		t.Fatalf("%s is not a git checkout", res.Dir)
	}
	// A CLONE, proved by the shape a linked worktree never has: its own .git
	// DIRECTORY (a worktree has a .git FILE pointing elsewhere).
	if fi, statErr := os.Stat(filepath.Join(res.Dir, ".git")); statErr != nil || !fi.IsDir() {
		t.Fatalf("%s/.git must be a directory (a clone, not a linked worktree): %v", res.Dir, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(origin, ".git", "worktrees")); statErr == nil {
		t.Fatal("the origin registered a linked worktree — the clone channel must not use `git worktree add`")
	}
	if res.Branch != DefaultBranch {
		t.Fatalf("branch = %q, want %q", res.Branch, DefaultBranch)
	}
}

// TestLoopPushesAfterAPullRebase proves the success path end to end: the run
// commits in the workspace, the loop pulls, builds with the DECLARED command
// and pushes, and origin ends up carrying the commit.
func TestLoopPushesAfterAPullRebase(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	// The agent's own work, exactly as an executor would leave it.
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("a clean, buildable workspace must not be blocked, got %s: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed || res.Attempts != 1 || res.Exhausted {
		t.Fatalf("Loop result = %+v, want pushed on attempt 1", res)
	}
	// THE BUILD THAT RAN IS THE DECLARED ONE: its own output is in the tree.
	stamp, readErr := os.ReadFile(filepath.Join(dir, "build-stamp.txt"))
	if readErr != nil || !strings.Contains(string(stamp), "BUILT-BY-DECLARATION") {
		t.Fatalf("the declared build command must have run in the workspace (stamp=%q err=%v)", stamp, readErr)
	}
	// AND THE COMMIT REACHED ORIGIN.
	out := gitIn(t, origin, "log", "--oneline", DefaultBranch)
	if !strings.Contains(out, "deliverable work") {
		t.Fatalf("origin does not carry the pushed commit:\n%s", out)
	}
}

// TestLoopRetriesWhenThePushIsRefused is the channel's reason to exist: origin
// moved between the pull and the push, the push is REFUSED, and the loop goes
// back to pull --rebase and eventually lands.
func TestLoopRetriesWhenThePushIsRefused(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	// THE RACE: the first build lands a commit on origin, i.e. somebody else
	// moved the branch between our pull and our push.
	cfg.Build = racingBuildCommand(t, origin, 1)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("the retried attempt must land, got blocker %s: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("the loop must land after a refusal, got %+v", res)
	}
	if res.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one refusal, one landing)", res.Attempts)
	}
	// BOTH commits are on origin: ours rebased onto theirs, nothing lost.
	out := gitIn(t, origin, "log", "--oneline", DefaultBranch)
	for _, want := range []string{"deliverable work", "theirs: race 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("origin is missing %q:\n%s", want, out)
		}
	}
}

// TestBrakeStopsAfterMaxRefusalsAndKeepsTheWorkspace proves the brake: origin
// keeps moving first, so every attempt is refused; after MaxPushAttempts the
// loop STOPS, reports by name, keeps the workspace and leaves a record.
func TestBrakeStopsAfterMaxRefusalsAndKeepsTheWorkspace(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")

	// SLOW ORIGIN, EVERY ROUND: origin lands a commit during EVERY build, so
	// the attempt's own push-race retries all run out — the shape the inner
	// brake is for, now reported as a Push BLOCKER instead of a Result flag.
	cfg.Build = racingBuildCommand(t, origin, -1)
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("an exhausted inner brake is a BLOCKER, not an error: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindPush {
		t.Fatalf("the attempt must end in a push blocker, got %+v (%v)", blk, res)
	}
	if res.Attempts != MaxPushAttempts {
		t.Fatalf("attempts = %d, want the inner brake limit %d", res.Attempts, MaxPushAttempts)
	}
	if !strings.Contains(res.Detail, "REFUSED") {
		t.Fatalf("the report must name the refusals, got %q", res.Detail)
	}
	// THE WORKSPACE IS KEPT — nothing deleted, the agent's commit still there.
	if _, statErr := os.Stat(filepath.Join(dir, "work.txt")); statErr != nil {
		t.Fatalf("the workspace must be KEPT after the brake: %v", statErr)
	}
	record, readErr := os.ReadFile(filepath.Join(dir, BrakeRecordName))
	if readErr != nil {
		t.Fatalf("the brake must leave a re-readable record: %v", readErr)
	}
	if !strings.Contains(string(record), testTaskID) {
		t.Fatalf("the record must name the task:\n%s", record)
	}
}

// TestReclaimDeletesTheWorkspace proves the recycle: the directory and its
// contents are gone, and a second reclaim is a no-op rather than an error.
func TestReclaimDeletesTheWorkspace(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	removed, err := Reclaim(dir)
	if err != nil || !removed {
		t.Fatalf("Reclaim = (%v,%v), want (true,nil)", removed, err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("the workspace must be gone after reclaim: %v", statErr)
	}
	again, err := Reclaim(dir)
	if err != nil || again {
		t.Fatalf("a second reclaim must be a quiet no-op, got (%v,%v)", again, err)
	}
}

// TestLoopRefusesWithNoDeclaredBuild is the Rule 12 half: the command is never
// discovered, so a repo with no declaration CANNOT push. The loop refuses
// BEFORE running git at all.
func TestLoopRefusesWithNoDeclaredBuild(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.Build = ""
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, blk, err := AttemptDelivery(context.Background(), cfg, Dir(cfg.Root, cfg.TaskID))
	if blk != nil {
		t.Fatalf("a missing declaration is not a blocker (no agent round can conjure one), got %s", blk.Kind)
	}
	if err == nil || !strings.Contains(err.Error(), "no build command is declared") {
		t.Fatalf("want the no-declaration refusal, got %v", err)
	}
}

// TestBackoffGrowsAndJitters pins the brake's pacing: 0.5s → 2s → 8s, each
// within its jitter band.
func TestBackoffGrowsAndJitters(t *testing.T) {
	want := []time.Duration{500 * time.Millisecond, 2 * time.Second, 8 * time.Second}
	for i, w := range want {
		attempt := i + 1
		for trial := 0; trial < 20; trial++ {
			got := backoff(attempt)
			lo := time.Duration(float64(w) * (1 - BackoffJitterRatio))
			hi := time.Duration(float64(w) * (1 + BackoffJitterRatio))
			if got < lo || got > hi {
				t.Fatalf("backoff(%d) = %s, want within [%s,%s]", attempt, got, lo, hi)
			}
		}
	}
}

// commitOnOrigin lands one commit on the "remote", so a following pull
// genuinely has work to do — the shape in which git REFUSES a rebase pull over
// uncommitted changes (it must update a tree it may not touch).
func commitOnOrigin(t *testing.T, origin, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(origin, name), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "add", name)
	gitIn(t, origin, "commit", "-q", "-m", "theirs: "+name)
}

// TestLoopRefusesADirtyWorkspaceWithItsOwnDiagnosis is the regression test for
// the channel's one unforgivable failure mode: handing the operator git's raw
// "cannot pull with rebase: You have unstaged changes" / exit 128 as if it were
// the channel's reason. The gate must refuse the pull FIRST, in the channel's
// own words (the offending paths by name + the next step), and it must leave
// the workspace byte-for-byte as it found it — never stash, commit or clean.
func TestLoopRefusesADirtyWorkspaceWithItsOwnDiagnosis(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	// origin carries something we do not have yet: a pull here would move the
	// tree, which is what makes git refuse over an unclean worktree.
	commitOnOrigin(t, origin, "theirs.txt")
	// The agent's own uncommitted work: one TRACKED file, modified, unstaged.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := gitIn(t, dir, "status", "--porcelain")
	if strings.TrimSpace(before) == "" {
		t.Fatal("the fixture must be dirty")
	}
	headBefore := gitIn(t, dir, "rev-parse", "HEAD")
	t.Logf("git status --porcelain BEFORE the loop: %q", before)

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a dirty workspace is a BLOCKER, not an error: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindDirty {
		t.Fatalf("a workspace with uncommitted changes must come back as a dirty blocker: %+v", res)
	}
	msg := blk.Report()
	t.Logf("the blocker the channel hands the next round: %s", msg)
	if res.Attempts != 0 {
		t.Fatalf("the gate must refuse BEFORE the first pull, got %d attempt(s): %v", res.Attempts, msg)
	}
	// RULE 15, THE WHOLE POINT OF Words: what comes back is git's OWN listing,
	// byte for byte (here ` M README.md`), not a channel summary of it.
	if want := " M README.md"; blk.Words != want+"\n" {
		t.Fatalf("Blocker.Words must be `git status --porcelain` VERBATIM, got %q want %q", blk.Words, want+"\n")
	}
	if len(blk.Paths) != 1 || blk.Paths[0] != " M README.md" {
		t.Fatalf("Blocker.Paths must be parsed from those same words, got %q", blk.Paths)
	}
	if strings.Contains(msg, "exit status 128") {
		t.Fatalf("git's raw exit 128 leaked out as the channel's reason: %s", msg)
	}
	// THE REPORT SAYS WHAT IS WRONG AND PRESCRIBES NOTHING: no "git add",
	// no "git commit" — the decision belongs to the agent that reads the
	// blocker, and a step list here would be the pipeline this loop replaced.
	for _, want := range []string{"refusing to pull", "README.md", "never stashes, commits or cleans"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the channel's own diagnosis must say %q, got: %s", want, msg)
		}
	}
	for _, banned := range []string{"git add", "git commit", "next step"} {
		if strings.Contains(msg, banned) {
			t.Fatalf("the diagnosis must not prescribe steps (%q found): %s", banned, msg)
		}
	}
	// THE WORKSPACE IS KEPT VERBATIM: same status, same HEAD, same bytes, and
	// nothing hidden in a stash.
	after := gitIn(t, dir, "status", "--porcelain")
	t.Logf("git status --porcelain AFTER the loop:  %q", after)
	if after != before {
		t.Fatalf("the workspace changed: before=%q after=%q", strings.TrimSpace(before), strings.TrimSpace(after))
	}
	if headAfter := gitIn(t, dir, "rev-parse", "HEAD"); headAfter != headBefore {
		t.Fatalf("HEAD moved: %q → %q", headBefore, headAfter)
	}
	if stash := strings.TrimSpace(gitIn(t, dir, "stash", "list")); stash != "" {
		t.Fatalf("the channel must never stash the operator's work, found: %s", stash)
	}
	if body, readErr := os.ReadFile(filepath.Join(dir, "README.md")); readErr != nil || string(body) != "local edit\n" {
		t.Fatalf("the uncommitted edit must survive verbatim, got %q (%v)", body, readErr)
	}
}

// TestOpenRefusesADirtyReusedWorkspace is the same gate on the OTHER road into
// pull --rebase: Open reuses a previous attempt's workspace and pulls it up to
// date. A reused workspace carrying uncommitted work must be refused there too,
// with the same channel diagnosis.
func TestOpenRefusesADirtyReusedWorkspace(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	commitOnOrigin(t, origin, "theirs.txt")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := gitIn(t, dir, "status", "--porcelain")
	res, err := Open(context.Background(), cfg)
	if err == nil {
		t.Fatalf("re-Opening a dirty workspace must be refused, got %+v", res)
	}
	msg := err.Error()
	t.Logf("the refusal the channel hands the operator: %v", err)
	if strings.Contains(msg, "exit status 128") {
		t.Fatalf("git's raw exit 128 leaked out as the channel's reason: %v", msg)
	}
	for _, want := range []string{"refusing to pull", "README.md", "never stashes, commits or cleans"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the channel's own diagnosis must say %q, got: %v", want, msg)
		}
	}
	// Open relays the SAME Blocker through `error`: the structured facts must
	// survive that trip (a caller that needs them asserts, never parses).
	var asBlocked *Blocker
	if !errors.As(err, &asBlocked) || asBlocked.Kind != BlockerKindDirty {
		t.Fatalf("Open must relay the clean gate's *Blocker, got %T: %v", err, err)
	}
	if after := gitIn(t, dir, "status", "--porcelain"); after != before {
		t.Fatalf("the workspace changed: before=%q after=%q", strings.TrimSpace(before), strings.TrimSpace(after))
	}
}

// TestLoopPushesWithUntrackedFilesPresent pins the gate's other half: '??'
// entries do NOT block. git's rebase tolerates untracked files, and the loop's
// own build products are exactly that — a gate that refused them would stop the
// second attempt of every single run.
func TestLoopPushesWithUntrackedFilesPresent(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("untracked files must not block the delivery, got %s: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("Loop = %+v, want the push to land", res)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "stray.txt")); statErr != nil {
		t.Fatalf("the untracked file must be left alone: %v", statErr)
	}
}

// TestCleanGateNamesAtMostMaxReportedDirtyPaths pins Rule 1b's named N: a
// workspace with more dirty paths than the budget names N of them and COUNTS the
// rest, so a heavy edit does not paste a whole status listing into a ledger row
// and a light one is not silently truncated.
func TestCleanGateNamesAtMostMaxReportedDirtyPaths(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	total := MaxReportedDirtyPaths + 2
	for i := 0; i < total; i++ {
		name := filepath.Join(dir, "edit-"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(name, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", "-N", ".")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blk, err := cleanGate(context.Background(), dir)
	if err != nil {
		t.Fatalf("cleanGate: %v", err)
	}
	if blk == nil {
		t.Fatal("a dirty workspace must be refused")
	}
	msg := blk.Report()
	t.Logf("the gate's report over %d dirty paths: %v", total+1, msg)
	if !strings.Contains(msg, fmt.Sprintf("showing at most %d", MaxReportedDirtyPaths)) {
		t.Fatalf("the report must quote the named limit %d: %v", MaxReportedDirtyPaths, msg)
	}
	if !strings.Contains(msg, fmt.Sprintf("and %d more", total+1-MaxReportedDirtyPaths)) {
		t.Fatalf("the report must count what it did not name: %v", msg)
	}
	if n := strings.Count(msg, `"`); n != 2*MaxReportedDirtyPaths {
		t.Fatalf("the report named %d paths, want exactly %d: %v", n/2, MaxReportedDirtyPaths, msg)
	}
}

// TestAttemptDeliveryReportsAPullConflictAsABlocker covers the second blocker
// kind: git itself refuses the pull --rebase because the workspace's own commit
// and origin's new commit touch the same lines. What comes back is git's own
// words (a CONFLICT), the rebase is aborted so the workspace is not left
// mid-rebase, and nothing is committed on the agent's behalf.
func TestAttemptDeliveryReportsAPullConflictAsABlocker(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	// THE SAME LINE, BOTH SIDES: ours in the workspace, theirs on origin.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "ours")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "add", "README.md")
	gitIn(t, origin, "commit", "-q", "-m", "theirs")
	headBefore := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a content conflict is a BLOCKER, not an error: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindPull {
		t.Fatalf("want a pull blocker, got %+v (%+v)", blk, res)
	}
	// GIT'S OWN WORDS, MEASURED — and the FIRST run of this test is why the
	// assertion is now a criterion rather than a substring: it used to demand
	// the `rebase --abort` hint, i.e. it pinned the very defect epic 01a17a4f
	// removed (the abort's advice block crowded `CONFLICT … in <path>` out of a
	// six-line tail, leaving a reader a commit id and no file). What must be
	// there now is the CONFLICT ITSELF and the PATH it names; what must NOT be
	// there is the channel's own cleanup text.
	for _, want := range []string{"could not apply", "CONFLICT (content)", "README.md"} {
		if !strings.Contains(blk.Words, want) {
			t.Fatalf("Blocker.Words must carry git's own account of the conflict (%q missing), got %q", want, blk.Words)
		}
	}
	for _, unwanted := range []string{"To abort and get back to the state before", "You can instead skip this commit"} {
		if strings.Contains(blk.Words, unwanted) {
			t.Fatalf("the aborted rebase's hint %q must not be part of the words — it is the channel's cleanup step, not the refusal: %q", unwanted, blk.Words)
		}
	}
	// THE PULL KIND FILLS Paths TOO, and that is a deliberate widening: for a
	// pull refusal the paths ARE the conflicted files, captured before the abort
	// (after it git reports none — measured). The clean gate's paths are the
	// uncommitted ones, so the two kinds never share a meaning.
	if len(blk.Paths) != 1 || blk.Paths[0] != "README.md" {
		t.Fatalf("a pull blocker fills Paths with the conflicted path(s), got %q", blk.Paths)
	}
	// THE WORKSPACE IS NOT LEFT MID-REBASE (pullRebase aborts): a later attempt
	// of the same row would otherwise be poisoned by the half-done rebase.
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "rebase-merge")); statErr == nil {
		t.Fatal("the aborted rebase must not leave rebase-merge behind")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "rebase-apply")); statErr == nil {
		t.Fatal("the aborted rebase must not leave rebase-apply behind")
	}
	// AND NOTHING WAS COMMITTED OR STASHED FOR THE AGENT: HEAD is still ours.
	if after := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD")); after != headBefore {
		t.Fatalf("the channel must not commit on the agent's behalf: HEAD %s → %s", headBefore, after)
	}
	if stash := strings.TrimSpace(gitIn(t, dir, "stash", "list")); stash != "" {
		t.Fatalf("the channel must never stash, found: %s", stash)
	}
}

// TestAttemptDeliveryReportsABuildFailureAsABlocker covers the third kind: the
// DECLARED build command fails. Its own output is the blocker's words — the
// command that ran is the declaration, and the failure is quoted, never
// summarised (Rule 15).
func TestAttemptDeliveryReportsABuildFailureAsABlocker(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	cfg.Build = "echo DECLARED-BUILD-REFUSES; exit 7"
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "work.txt")
	gitIn(t, dir, "commit", "-q", "-m", "deliverable work")

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a failing build is a BLOCKER, not an error: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindBuild {
		t.Fatalf("want a build blocker, got %+v (%+v)", blk, res)
	}
	if !strings.Contains(blk.Words, "DECLARED-BUILD-REFUSES") {
		t.Fatalf("Blocker.Words must quote the build's own output, got %q", blk.Words)
	}
	if !strings.Contains(blk.Report(), "the build step refused") {
		t.Fatalf("the report must name the step that refused, got %q", blk.Report())
	}
	// NOTHING WAS PUSHED: a failed build must never reach origin.
	if out := gitIn(t, origin, "log", "--oneline", DefaultBranch); strings.Contains(out, "deliverable work") {
		t.Fatalf("a workspace that failed its build must not be pushed:\n%s", out)
	}
}
