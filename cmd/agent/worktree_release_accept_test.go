package main

// Shell-level acceptance for the PRODUCER-side (own) worktree release — the
// arm `gopi worker` runs at the end of a round that freed its own directory
// (internal/tools/task_reclaim_self.go), driven here through the CLI process's
// OWN round-end function.
//
// WHY THIS FILE EXISTS (and how it differs from the dead-holder rig next to
// it, cli_tasks_dead_holder_accept_test.go): that rig's holder is a DEAD pid,
// which only ever exercises the sweep's dead-holder arm. The gap this file
// pins is the LIVE one — the claim stamp a worker round writes is the
// long-lived worker PID, so at close-out time the holder is still ALIVE and
// every sweep keyed on "the holder is dead" is blind to the row. Nothing but
// the producer's own pass can free that directory, and the arm is only
// reachable while the claim stamp IS this process's:
//
//	TestWorkerReleaseOwnWorktree_Acceptance — the finished run's own directory
//	is gone, its branch survives, and a LIVE SIBLING's directory AND branch are
//	untouched (the ownership axis: both rows name their own worktree and carry
//	the live host:pid fingerprint, so a pass that keyed on "the row looks
//	finished" instead of "the row is MINE" would take the sibling's ground).
//
//	TestOwnRelease_RefusesLiveSibling — the unconditional red-proof: the
//	sibling's directory must SURVIVE even when this process's row is released.
//
// THE CLAIM STAMP IS THE LIVE `host:pid`, ON PURPOSE. There is no worker
// claim segment to fetch and none is faked: the ledger's own renderer
// (tools.TaskClaimOwner) stamps this test process's pid — the same shape a
// worker round writes — and the pass reads it back from the ledger. The
// directory is therefore released while the holder pid is provably alive
// (/proc/<pid>/status is asserted to exist BEFORE and AFTER the release), which
// is the whole point: a dead-pid fixture would pass vacuously because the
// dead-holder sweep would have freed it too.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// fixture: a real repo, two linked worktrees, two own-row deliverables
// ---------------------------------------------------------------------------

// ownReleaseStamp renders the claim stamp the LEDGER writes for this process.
// It is the production renderer (tools.TaskClaimOwner → hostname:pid), never a
// hand-built string, so the test cannot drift from the shape the release pass
// matches on.
func ownReleaseStamp(t *testing.T) string {
	t.Helper()
	stamp := strings.TrimSpace(tools.TaskClaimOwner(""))
	if stamp == "" || strings.HasPrefix(stamp, "unknown-host") {
		t.Skip("no hostname on this machine — the host:pid claim shape is untestable here")
	}
	return stamp
}

// assertLiveClaimWitness states the fixture's central fact: the pid named by
// the claim stamp is alive RIGHT NOW. The producer arm is only meaningful
// while that holds, so it is asserted at both ends of the release — a fixture
// that quietly became a dead-holder row would test the wrong arm.
func assertLiveClaimWitness(t *testing.T, stamp, phase string) {
	t.Helper()
	_, pidStr, ok := strings.Cut(stamp, ":")
	if !ok {
		t.Fatalf("claim stamp %q is not host:pid — the live-holder precondition cannot be stated", stamp)
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		t.Fatalf("claim stamp %q: pid is not a number: %v", stamp, err)
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "status")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("no procfs entry for pid %d — the live-claim precondition is unprovable on this host", pid)
		}
		t.Fatalf("%s: stat /proc/%d/status: %v", phase, pid, err)
	}
}

// ownReleaseRepo is a fixture repo with the linked worktree of the row under
// test (wt1 / branch1) and a SIBLING's worktree (wt2 / branch2) on its own
// branch — the live peer's ground the release must never touch.
type ownReleaseRepo struct {
	repo     string
	wt1, wt2 string
	b1, b2   string
	b1Tip    string // branch1's tip before the release: the deliverable's fingerprint
}

// releaseGit runs one git command in dir with a hermetic config (the host's
// global/system config, and any inherited GIT_DIR / GIT_WORK_TREE /
// GIT_INDEX_FILE — an inherited GIT_DIR makes even `git init` fail with
// "fatal: The empty string is not a valid path") and returns its trimmed
// stdout.
func releaseGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = releaseGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// releaseGitEnv is the environment every fixture git call runs with: the
// ambient GIT_* routing vars are DROPPED (never set to "" — an empty-but-set
// GIT_DIR is itself the failure), the host's global/system config is pinned
// off, and the committer identity is fixed so commits are reproducible.
func releaseGitEnv() []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_CONFIG_GLOBAL": true, "GIT_CONFIG_SYSTEM": true,
		"GIT_AUTHOR_NAME": true, "GIT_AUTHOR_EMAIL": true,
		"GIT_COMMITTER_NAME": true, "GIT_COMMITTER_EMAIL": true,
	}
	env := make([]string, 0, len(os.Environ())+6)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop[kv[:i]] {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_AUTHOR_NAME=rig", "GIT_AUTHOR_EMAIL=rig@rig",
		"GIT_COMMITTER_NAME=rig", "GIT_COMMITTER_EMAIL=rig@rig",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0")
}

// ownReleaseFixture builds the repo + both worktrees. Every directory is a
// t.TempDir() (the suite's pinned scratch root) — nothing is written to the
// rig's own tree or to a shared /tmp path.
func ownReleaseFixture(t *testing.T) ownReleaseRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	f := ownReleaseRepo{
		repo: filepath.Join(root, "repo"),
		wt1:  filepath.Join(root, "wt-own"),
		wt2:  filepath.Join(root, "wt-sibling"),
		b1:   "feat/own-release",
		b2:   "feat/sibling-release",
	}
	releaseGit(t, root, "init", "-b", "master", f.repo)
	releaseGit(t, f.repo, "config", "user.email", "rig@rig")
	releaseGit(t, f.repo, "config", "user.name", "rig")
	if err := os.WriteFile(filepath.Join(f.repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	releaseGit(t, f.repo, "add", "base.txt")
	releaseGit(t, f.repo, "commit", "-m", "base")
	// The deliverable: one commit per branch, each on its own branch.
	for _, wt := range []struct{ dir, branch, file string }{
		{f.wt1, f.b1, "own-deliverable.txt"},
		{f.wt2, f.b2, "sibling-deliverable.txt"},
	} {
		releaseGit(t, f.repo, "worktree", "add", "-b", wt.branch, wt.dir, "master")
		if err := os.WriteFile(filepath.Join(wt.dir, wt.file), []byte("the delivery\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		releaseGit(t, wt.dir, "add", wt.file)
		releaseGit(t, wt.dir, "commit", "-m", "the deliverable")
	}
	f.b1Tip = releaseGit(t, f.repo, "rev-parse", f.b1)
	return f
}

// releaseLedger points the ledger + settings at sandboxed files under the
// suite's scratch root, so the pass reads exactly the rows this test wrote.
func releaseLedger(t *testing.T) {
	t.Helper()
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")
}

// releaseOwnRow submits a real row for the fixture repo and drives it up the
// ledger's own claim ladder to the state a worker round leaves at close-out:
// claimed_by = THIS process (host:pid), status = in_progress. Returns the row
// id.
//
// THE WORKTREE IS STAMPED THROUGH THE PRODUCTION SEAM, not by hand: the row is
// repo-only after TaskSubmit, and the deliverable.worktree backfill
// (tools.TaskDeliverableWorktreeBash — the executor bash-round hook that fires
// on a `git worktree add` round) is what puts the worktree + branch into the
// row. That is the same information-closure chain a live run builds, so the
// row the release pass reads is the row a live run leaves behind.
func releaseOwnRow(t *testing.T, taskID, repo, branch, wt string) string {
	t.Helper()
	row := "rig-own-release-" + taskID
	// TaskSubmit RETURNS A MESSAGE ("Task queued: <id> …"), not the id — the
	// fixture supplies the id explicitly so the row it drives is the row it
	// reads back (same shape every internal/tools fixture uses).
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		TaskID:  row,
		Summary: "own worktree release acceptance",
		Payload: "fixture row — never executed, only the claim ladder is driven",
		Source:  "human",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("submit own row: %v", err)
	}
	// The executor's own backfill seam, driven with the wrapper env the child
	// gopi process carries (no env is written back — the row field is).
	func() {
		defer testutil.SnapshotEnv(t, tools.TaskWrapperEnv)()
		// A deliberate per-call flip of a process-global var: the seam the test
		// drives (TaskDeliverableWorktreeBash) reads it via os.Getenv, and the
		// child gopi process this call spawns reads the wrapper env at exec
		// time — so the flip must undo when THIS CALL returns. The deferred
		// SnapshotEnv above does exactly that, absence included; t.Setenv is
		// the wrong tool twice over (it can only SET, restoring "" instead of
		// absence, and its undo is TEST-scoped, leaving the last value
		// installed for the rest of the test).
		// env-leak-ok: reviewed SnapshotEnv pair — per-call flip, undone on return.
		if err := os.Setenv(tools.TaskWrapperEnv, row); err != nil { //stylegate:ignore the flip must undo when THIS CALL returns, not at test cleanup; SnapshotEnv restores it (absence included) // env-leak-ok: SnapshotEnv restores on return, absence included
			t.Fatalf("set %s: %v", tools.TaskWrapperEnv, err)
		}
		tools.TaskDeliverableWorktreeBash(wt, "git worktree add "+wt+" -b "+branch+" master")
	}()
	if gotRepo, gotBranch, gotWT := tools.TaskDeliverableRepoFor(row), taskDeliverableBranch(t, row), taskDeliverableWorktreePath(t, row); gotRepo != repo || gotBranch != branch || gotWT != wt {
		t.Fatalf("the backfill seam must stamp the row's worktree: repo=%q branch=%q worktree=%q (want %q/%q/%q)",
			gotRepo, gotBranch, gotWT, repo, branch, wt)
	}
	// The claim a worker round's row carries is a live `host:pid` — here that
	// is THIS process, the arm's whole precondition (also asserted by the
	// caller against /proc).
	if _, err := tools.TaskStart(row, ""); err != nil {
		t.Fatalf("start own row %s: %v", row, err)
	}
	return row
}

// taskDeliverableBranch reads the branch a row's deliverable blob names.
func taskDeliverableBranch(t *testing.T, row string) string {
	t.Helper()
	return deliverableField(t, row, "branch")
}

// taskDeliverableWorktreePath reads the worktree a row's deliverable blob
// names.
func taskDeliverableWorktreePath(t *testing.T, row string) string {
	t.Helper()
	return deliverableField(t, row, "worktree")
}

// deliverableField decodes one string field of a row's deliverable blob.
func deliverableField(t *testing.T, row, field string) string {
	t.Helper()
	ctx := context.Background()
	db, err := tools.OpenTasksDBForSweep(ctx)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var blob string
	if err := db.QueryRowContext(ctx, `SELECT deliverable FROM tasks WHERE task_id = ?`, row).Scan(&blob); err != nil {
		t.Fatalf("read deliverable of %s: %v", row, err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		t.Fatalf("decode deliverable %q: %v", blob, err)
	}
	s, _ := d[field].(string)
	return s
}

// worktreeListOf returns the branch names `git worktree list` currently names
// for the repo — the source gate [c] of scripts/safe-merge.sh greps.
func worktreeListOf(t *testing.T, repo string) string {
	t.Helper()
	return releaseGit(t, repo, "worktree", "list", "--porcelain")
}

// branchListedInWorktrees reports whether git still holds a worktree
// registration for branch ("branch refs/heads/<b>" in porcelain output).
func branchListedInWorktrees(t *testing.T, repo, branch string) bool {
	t.Helper()
	for _, line := range strings.Split(worktreeListOf(t, repo), "\n") {
		if strings.TrimSpace(line) == "branch refs/heads/"+branch {
			return true
		}
	}
	return false
}

// releaseSafeMerge runs the REAL scripts/safe-merge.sh (gate [c] is the
// worktree-occupancy gate the release exists to unblock) and returns the exit
// code with the combined output.
func releaseSafeMerge(t *testing.T, repo, branch string) (int, string) {
	t.Helper()
	script := filepath.Join(repoRootForRig(t), "scripts", "safe-merge.sh")
	cmd := exec.Command("bash", script, branch)
	cmd.Dir = repo
	cmd.Env = releaseGitEnv()
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("safe-merge %s: %v: %s", branch, err, out)
		}
		code = ee.ExitCode()
	}
	return code, string(out)
}

// repoRootForRig walks up from the test's cwd to the module root (the test
// binary's cwd is cmd/agent under `go test`).
func repoRootForRig(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Skip("go.mod not found above the test dir")
		}
		root = parent
	}
}

// siblingOwnedRow submits the LIVE SIBLING's row: claimed by a pid that is
// genuinely alive and whose deliverable names the sibling's own branch +
// directory.
//
// This is not decoration: it is the shape that makes the ownership axis
// falsifiable. Both rows name a worktree, the peer's claim names a live pid,
// and its directory carries no occupant at all — so a pass that selected
// candidates by "the row is finished / in_progress" instead of "the row is
// MINE" would take the sibling's ground, and the assertions below would see
// it. The ONLY thing standing between the pass and the sibling's directory is
// the ownership stamp.
func siblingOwnedRow(t *testing.T, taskID, repo, branch, wt string, holderPID int) string {
	t.Helper()
	row := "rig-sibling-" + taskID
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		TaskID:  row,
		Summary: "sibling worker row (live peer) — must never be released by this process",
		Payload: "fixture row — the live peer's ground",
		Source:  "human",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("submit sibling row: %v", err)
	}
	stamp := ownReleaseStamp(t)
	host := stamp[:strings.LastIndex(stamp, ":")]
	ctx := context.Background()
	db, err := tools.OpenTasksDBForSweep(ctx)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	// The sibling's deliverable names ITS OWN directory and branch — the point
	// of the test is that the ownership stamp, not the deliverable's
	// plausibility, is what keeps the pass off it.
	deliverable := `{"repo":"` + repo + `","branch":"` + branch + `","worktree":"` + wt + `"}`
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, in_progress_at = ?, updated_at = ?, deliverable = ? WHERE task_id = ?`,
		"in_progress", host+":"+strconv.Itoa(holderPID), float64(time.Now().Unix()),
		float64(time.Now().Unix()), float64(time.Now().Unix()), deliverable, row); err != nil {
		t.Fatalf("stamp sibling row: %v", err)
	}
	return row
}

// liveIdlePeer starts a genuinely alive process whose cwd is NOT the
// worktree under test: the "live peer worker" shape reduced to its ownership
// axis — the claim stamp names a live pid, while the directory itself carries
// no occupant, so nothing but the OWNERSHIP gate keeps the pass off it.
// Returns the pid.
func liveIdlePeer(t *testing.T) int {
	t.Helper()
	peer := exec.Command("sleep", "60")
	// Dir stays (a cwd OUTSIDE the worktree is the ownership axis under test);
	// Pdeathsig keeps the peer from outliving a SIGKILLed test binary.
	peer.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	peer.Dir = t.TempDir()
	if err := peer.Start(); err != nil {
		t.Fatalf("start idle peer: %v", err)
	}
	t.Cleanup(func() {
		_ = peer.Process.Kill() //stylegate:ignore teardown of the idle peer fixture; the release verdict is what the test asserts
		_ = peer.Wait()         //stylegate:ignore reap of the killed peer; its exit is observed through the worktree state
	})
	pid := peer.Process.Pid
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "status")); err != nil {
		t.Skipf("no procfs entry for the idle peer pid %d: %v", pid, err)
	}
	return pid
}

// ---------------------------------------------------------------------------
// the acceptance pair
// ---------------------------------------------------------------------------

// TestWorkerReleaseOwnWorktree_Acceptance is the positive end-to-end: the
// worker's OWN round-end pass frees the finished run's directory, the branch
// (the deliverable) survives byte-identical, and the LIVE SIBLING's directory
// and branch are untouched.
func TestWorkerReleaseOwnWorktree_Acceptance(t *testing.T) {
	releaseLedger(t)
	f := ownReleaseFixture(t)
	stamp := ownReleaseStamp(t)

	own := releaseOwnRow(t, "own", f.repo, f.b1, f.wt1)
	assertLiveClaimWitness(t, stamp, "before the release")

	// The live sibling: a real process named by the sibling's claim stamp, and
	// its own in_progress row naming its own directory. The peer's cwd is
	// deliberately NOT the worktree, so nothing but the OWNERSHIP gate (the
	// stamp is a live peer, not this process) can keep the pass off it — the
	// sibling's directory is exactly as releasable as the owner's.
	holderPID := liveIdlePeer(t)
	sibling := siblingOwnedRow(t, "live-peer", f.repo, f.b2, f.wt2, holderPID)
	if sibling == own {
		t.Fatalf("fixture error: the sibling row must be a different row (%s)", own)
	}

	// PRECONDITION (stated as the defect this arm closes): gate [c] refuses
	// the owner's branch exactly because its directory is still registered.
	if code, out := releaseSafeMerge(t, f.repo, f.b1); code == 0 {
		t.Fatalf("fixture precondition: gate [c] must refuse the occupied branch before the release:\n%s", out)
	} else if !strings.Contains(out, "[c]") {
		t.Fatalf("fixture precondition: the WORKTREE-OCCUPANCY gate must be the refusal:\n%s", out)
	}

	// THE ARM, driven at its real entry point: the CLI's own round-end pass.
	// Output is captured because workerRoundOwnWorktrees prints the pass's
	// lines (the attribution contract); the lines are also asserted below.
	lines := captureStdout(t, workerRoundOwnWorktrees)

	// ① the finished run's own directory is GONE …
	if _, err := os.Stat(f.wt1); !os.IsNotExist(err) {
		t.Fatalf("the own directory must be released (stat err=%v) — this is the arm's whole job; pass lines:\n%s", err, lines)
	}
	// … and its registration with it: gate [c] is what the release unblocks.
	if branchListedInWorktrees(t, f.repo, f.b1) {
		t.Fatalf("the released branch must no longer be occupied in `git worktree list`:\n%s", worktreeListOf(t, f.repo))
	}
	// ② the BRANCH is the deliverable: never deleted, tip byte-identical.
	if now := releaseGit(t, f.repo, "rev-parse", f.b1); now != f.b1Tip {
		t.Fatalf("the branch tip must be untouched by the release: %s → %s", f.b1Tip, now)
	}
	if err := exec.Command("git", "-C", f.repo, "cat-file", "-e", f.b1+":own-deliverable.txt").Run(); err != nil {
		t.Fatalf("the deliverable must still be on the branch: %v", err)
	}
	// ③ the LIVE SIBLING's directory, registration and branch are untouched.
	if _, err := os.Stat(f.wt2); err != nil {
		t.Fatalf("a LIVE peer's worktree must survive this process's close-out: %v", err)
	}
	if !branchListedInWorktrees(t, f.repo, f.b2) {
		t.Fatalf("the live peer's worktree registration must survive:\n%s", worktreeListOf(t, f.repo))
	}
	if now := releaseGit(t, f.repo, "rev-parse", f.b2); now == "" {
		t.Fatalf("the live peer's branch must be untouched")
	}
	// The pass reports WHAT it freed, and it reports it as the own-release arm
	// (one grepable phrase for the log AND the tests).
	if !strings.Contains(lines, "own worktree released") || !strings.Contains(lines, own) {
		t.Fatalf("the release must report the row it freed, got:\n%s", lines)
	}

	// The claim witness is STILL alive after the release — the row was freed
	// while its holder was live, which is exactly what the dead-holder sweep
	// cannot do.
	assertLiveClaimWitness(t, stamp, "after the release")

	// AFTER: gate [c] passes for the owner's branch — the finished deliverable
	// is mergeable again.
	if code, out := releaseSafeMerge(t, f.repo, f.b1); code != 0 {
		t.Fatalf("safe-merge must exit 0 after the release:\n%s", out)
	}
	// …and it must still REFUSE the sibling's branch: the peer's ground is not
	// ours to hand to the merger either.
	if code, out := releaseSafeMerge(t, f.repo, f.b2); code == 0 {
		t.Fatalf("gate [c] must still refuse a LIVE peer's occupied branch:\n%s", out)
	}
}

// TestOwnRelease_RefusesLiveSibling is the red-proof half of the pair, and it
// is deliberately phrased so it can only pass for the right reason: it asserts
// the SIBLING's directory and branch survive, while the sibling's row is
// terminal-eligible and its deliverable names that very directory.
//
// The failure mode it pins: a release keyed on "the row looks finished" rather
// than "the row is MINE (host:pid of this process)" frees the live peer's
// ground. Nothing here depends on the owner's own row, so the two tests cannot
// mask each other.
func TestOwnRelease_RefusesLiveSibling(t *testing.T) {
	releaseLedger(t)
	f := ownReleaseFixture(t)
	stamp := ownReleaseStamp(t)
	assertLiveClaimWitness(t, stamp, "before the sibling assertion")

	holderPID := liveIdlePeer(t)
	sibling := siblingOwnedRow(t, "live-peer", f.repo, f.b2, f.wt2, holderPID)
	// Make the sibling's OWN release as plausible as it can be: its row is
	// already finished and its deliverable names its directory. Only the claim
	// stamp (a live peer, not this process) keeps it out of the pass's
	// candidate set.
	ctx := context.Background()
	db, err := tools.OpenTasksDBForSweep(ctx)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE task_id = ?`, "completed", sibling); err != nil {
		db.Close() //stylegate:ignore close on an already-failing t.Fatalf path; the reported error is the UPDATE, not the close
		t.Fatalf("terminalise sibling row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}

	siblingBranchTip := releaseGit(t, f.repo, "rev-parse", f.b2)
	lines := captureStdout(t, workerRoundOwnWorktrees)

	if _, err := os.Stat(f.wt2); err != nil {
		t.Fatalf("the LIVE SIBLING's worktree is gone — the release took a peer's ground: %v\npass lines:\n%s", err, lines)
	}
	if !branchListedInWorktrees(t, f.repo, f.b2) {
		t.Fatalf("the live sibling's worktree registration was dropped:\n%s", worktreeListOf(t, f.repo))
	}
	if now := releaseGit(t, f.repo, "rev-parse", f.b2); now != siblingBranchTip {
		t.Fatalf("the live sibling's branch moved: %s → %s", siblingBranchTip, now)
	}
	// The row must not even be CLAIMED as a candidate — the pass is silent
	// about rows that are not this process's (the named-refusal contract
	// applies to rows it owns; a foreign row is not its business at all).
	if strings.Contains(lines, sibling) {
		t.Fatalf("a foreign row must not appear in this process's release report, got:\n%s", lines)
	}
	// The holder is still live: the fixture never degenerated into the
	// dead-holder case this test exists to be distinct from.
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(holderPID), "status")); err != nil {
		t.Fatalf("the sibling holder pid %d died mid-test — the live-sibling shape was not held: %v", holderPID, err)
	}
}
