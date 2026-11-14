package tools

// Regression guards for the LEDGER-KEYED own-release arm's self-match (epic
// worktree-own-release, row 01a0d58e — the arm `cmd/agent`'s
// TestWorkerReleaseOwnWorktree_Acceptance drives through the worker's round-end
// pass).
//
// THE MEASURED DEFECT. `reclaimSelfRowsFrom` selects candidates by
// `claimed_by = MY claim stamp`, and the ledger's own writer stamps the
// EXECUTING process into the row's recorded runner triple (taskUpdateStampedFrom
// → proc_pid/proc_start_ticks/proc_cmd). At close-out time the process asking the
// liveness question IS the process the row records as its runner, and its pid is
// alive by construction — the round-end defer runs inside it. Guard ⓪
// (reclaimLiveSessionRefusal → worktreeSweepSessionLiveEvidence) asked both its
// bits with no exclusion at all, so it answered "live task X still has a run
// standing (its recorded runner (pid N) is still running)" about the asker
// itself, for every candidate, forever. Measured on the acceptance rig, verbatim:
//
//	[own-release] claimed by this worker: zpeer:771230      ← the asker's own stamp
//	own worktree release refused: feat/own-release (rig-own-release-own) — live
//	  task rig-own-release-own still has a run standing (its recorded runner
//	  (pid 771230) is still running)
//
// ONE pid in both lines: the asker answered its own question with itself, the
// finished run's ground was never freed, `git worktree list` kept naming it, and
// scripts/safe-merge.sh gate [c] kept refusing the branch — the withholding this
// arm exists to end.
//
// THE FIX (product): Guard ⓪ asks its evidence with the asker's own process tree
// REMOVED (finishCleanupSelfTree — the asker, its ancestors and its live
// descendants; ONE definition of "not a third party", shared with the
// finish-cleanup ladder). Self-exclusion is not self-permission: the asker's own
// position is withheld by the arm's own-ground rung (guestRowOwnCheckoutRefusal),
// which is untouched and still refuses the checkout the process stands in.
//
// WHAT MUST NOT REGRESS (the two controls below, one per direction):
//
//	the asker is not a run   → own ground, recorded runner = THIS process, freed.
//	a FOREIGN runner is       → its ground stays, refused BY NAME. The exclusion
//	  must never widen into "any pid I can attribute is free to go".
//	a FOREIGN cwd holder      → its ground stays: evidence ② is still asked, and a
//	  third party standing in the row's session directory still vetoes (the
//	  concurrent-session window the 2026-09-17 incident opened).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// ownReleaseSelfMatchLedger points the ledger + settings at sandboxed files
// under the suite's pinned scratch root, so the pass reads exactly the rows these
// tests wrote (never the operator's append-only ledger).
func ownReleaseSelfMatchLedger(t *testing.T) {
	t.Helper()
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")
}

// ownReleaseSelfMatchRow submits a row and drives it to the state a finished run
// leaves at close-out: claimed by THIS process (the same stamp the claim ladder
// writes), in_progress, its deliverable naming the fixture worktree, and its
// recorded RUNNER identity seeded explicitly.
//
// SEEDING THE IDENTITY IS THE FIXTURE'S WHOLE POINT, and it is not a
// contrivance: the ledger CLEARS those columns on a terminal transition, and the
// shape under test is a row whose recorded runner is a real process — the one the
// 2026-09-21/22 guards (zz_redproof_reclaim_live_runner_test.go) were built for.
// The values are captured from a real pid (taskCaptureProcIdentity), never forged.
func ownReleaseSelfMatchRow(t *testing.T, taskID, repo, branch, wt string, ident procIdentity) string {
	t.Helper()
	row := "t-own-release-" + taskID
	if _, err := TaskSubmit(TaskSubmitOptions{
		TaskID:  row,
		Summary: "own-release self-match fixture",
		Payload: "fixture row — never executed; only the claim ladder and the release pass are driven",
		Source:  "human",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("submit %s: %v", row, err)
	}
	stamp := reclaimSelfStamp()
	if stamp == "" {
		t.Skip("no hostname on this machine — the ledger-keyed self-claim shape is untestable here")
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	now := float64(time.Now().Unix())
	blob := `{"repo":"` + repo + `","branch":"` + branch + `","worktree":"` + wt + `"}`
	// The UPDATE names every column it writes, through the ledger's own column
	// constants (Rule 1b) — the pass reads these exact columns.
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, updated_at = ?, deliverable = ?,
		 `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = ?, `+tasksProcCmdColumnName+` = ? WHERE task_id = ?`,
		taskStatusInProgress, stamp, now, now, blob,
		ident.Pid, ident.StartTicks, ident.Cmd, row); err != nil {
		t.Fatalf("stamp row %s: %v", row, err)
	}
	return row
}

// ownReleaseSelfIdentity is the identity of THIS test process — the exact
// recorded-runner value the ledger writes for a row a run of this process owns
// (taskUpdateStampedFrom captures os.Getpid()'s + start ticks + cmdline).
func ownReleaseSelfIdentity(t *testing.T) procIdentity {
	t.Helper()
	pid, start, cmd := taskSelfIdentityValues()
	n, ok := pid.(int)
	if !ok || n <= 0 {
		t.Skip("this host cannot capture its own process identity — the self-match shape is unmeasurable here")
	}
	ticks, ok := start.(int64)
	if !ok || ticks <= 0 {
		t.Skip("this host cannot read its own start ticks — the self-match shape is unmeasurable here")
	}
	s, _ := cmd.(string)
	return procIdentity{Pid: n, StartTicks: ticks, Cmd: s}
}

// TestReclaimLiveSessionRefusalDoesNotTakeTheAskerForALiveRun is the MINIMAL
// red-then-green assertion, and it drives Guard ⓪ itself — the shared predicate
// whose answer becomes the refusal sentence.
//
// RED before the fix: the asker's own pid is in the row's recorded runner triple
// and is alive, so the rung returned "still has a run standing" for a row whose
// only run is the process asking the question.
func TestReclaimLiveSessionRefusalDoesNotTakeTheAskerForALiveRun(t *testing.T) {
	ownReleaseSelfMatchLedger(t)
	self := ownReleaseSelfIdentity(t)

	mine := reclaimDeadHolderRow{
		TaskID:    "t-own-release-unit",
		Status:    taskStatusInProgress,
		ClaimedBy: reclaimSelfStamp(),
		Repo:      "/nonexistent/repo",
		Worktree:  "/nonexistent/ground",
		Ident:     self,
	}
	if why := reclaimLiveSessionRefusal(mine); why != "" {
		t.Fatalf("the asker's OWN process identity is not evidence that its row still has a run standing, got %q", why)
	}
	// …while the same row with a genuinely FOREIGN runner is still refused, by
	// name and with the fact that decided it (the control that keeps the
	// exclusion from becoming a blanket wall).
	foreign := ownReleaseForeignRunner(t)
	other := mine
	other.TaskID = "t-own-release-unit-foreign"
	other.Ident = foreign
	why := reclaimLiveSessionRefusal(other)
	if why == "" {
		t.Fatal("a FOREIGN live runner must still refuse the release")
	}
	if !strings.Contains(why, strconv.Itoa(foreign.Pid)) {
		t.Errorf("the refusal must name the foreign pid %d, got %q", foreign.Pid, why)
	}
}

// TestReclaimLiveSessionRefusalKeepsALiveChildRunner is the trap this row fell
// into and the guard that keeps it closed: the exclusion is the asker's OWN PID,
// never its process TREE.
//
// MEASURED (2026-09-25, row 01a0d58e): the first version of the fix passed
// finishCleanupSelfTree() — the asker, its ancestors and its live descendants —
// as the excluded set, reasoning that the ladder already owns that notion of
// "not a third party". It is the right set for the LADDER (whose holder question
// runs as a descendant of the run it judges) and the WRONG set here: the
// red-proof rigs plant the live run as a CHILD of the process under test, so a
// tree-wide exclusion forgave exactly the liveness the guard exists to protect.
// Five veto tests went from refusing to FREEING a live run's ground —
// TestSelfReclaimRefusesALiveInProgressRowsGround, TestZZRedProofSelfReclaimKeepsALiveRunnersGround,
// TestReclaimRefusesALiveRunnersGroundWhenTheRowIsTerminal among them. The
// exclusion is therefore the asker alone, and this test states that as a
// property: a live runner that is this process's own DESCENDANT still refuses.
func TestReclaimLiveSessionRefusalKeepsALiveChildRunner(t *testing.T) {
	ownReleaseSelfMatchLedger(t)

	// A live process that IS this asker's descendant — the rigs' own shape. A
	// plain child (not reparented) so the parent relation is exactly the one under
	// test.
	child := exec.Command("sleep", "60")
	// Pdeathsig + a neutral cwd: the subject is the parent/child RELATION, and a
	// survivor from a killed binary would stand in the checkout for good.
	child.Dir = t.TempDir()
	child.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := child.Start(); err != nil {
		t.Skipf("no sleep to build the descendant stand-in: %v", err)
	}
	//stylegate:ignore killing the fixture child the test spawned to assert a liveness transition
	t.Cleanup(func() { _ = child.Process.Kill(); _, _ = child.Process.Wait() })
	parent, ok := finishCleanupProcParent(strconv.Itoa(child.Process.Pid))
	if !ok || parent != os.Getpid() {
		t.Skipf("the descendant stand-in pid %d does not read as this process's child (parent=%d ok=%v)", child.Process.Pid, parent, ok)
	}

	ident, ok := taskCaptureProcIdentity(child.Process.Pid)
	if !ok {
		t.Skipf("cannot capture the descendant's identity (pid %d)", child.Process.Pid)
	}
	rec := procIdentity{Pid: ident.Pid, StartTicks: ident.Start, Cmd: ident.Cmd}
	if !finishCleanupSelfTree()[rec.Pid] {
		t.Fatalf("fixture error: pid %d must be a DESCENDANT of this asker (a tree-wide exclusion was the defect)", rec.Pid)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !finishCleanupProcessHolderLive(rec) {
		if time.Now().After(deadline) {
			t.Skipf("the descendant stand-in pid %d never reads as live here", rec.Pid)
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(20 * time.Millisecond)
	}

	row := reclaimDeadHolderRow{
		TaskID:    "t-own-release-child",
		Status:    taskStatusInProgress,
		ClaimedBy: reclaimSelfStamp(),
		Repo:      "/nonexistent/repo",
		Worktree:  "/nonexistent/ground",
		Ident:     rec,
	}
	// Asked at the BIT, not at the whole arm: at arm level rung ②
	// (reclaimForeignRunnerRefusal) would also refuse this child, so a whole-arm
	// test would pass against the tree-wide variant for the wrong reason —
	// measured, that is exactly what the first draft of this test did (PASS, not
	// skip, on the defect). Bit ① is the unit the tree-wide exclusion broke.
	live := worktreeSweepSessionLiveEvidenceFor(row.Status, row.Ident, row.TaskID, os.Getpid())
	if live == "" {
		t.Fatal("a live recorded runner that is this asker's own DESCENDANT is a veto, not a self-answer — the exclusion must stop at the asker's pid, never its process tree")
	}
	if !strings.Contains(live, strconv.Itoa(rec.Pid)) {
		t.Errorf("the evidence must name the live child pid %d, got %q", rec.Pid, live)
	}
	// And the whole arm still refuses it, by name.
	why := reclaimLiveSessionRefusal(row)
	if !strings.Contains(why, strconv.Itoa(rec.Pid)) {
		t.Errorf("the arm must refuse the descendant runner by name (pid %d), got %q", rec.Pid, why)
	}
}

// ownReleaseForeignRunner returns the recorded identity of a live process that is
// NOT part of this asker's process tree — a launcher in its OWN session whose
// holder was reparented to init (reparentedToInitStandIn). It is the honest
// stand-in for another session's run of the same row: live in the process table,
// attributable to nobody here.
func ownReleaseForeignRunner(t *testing.T) procIdentity {
	t.Helper()
	pid := reparentedToInitStandIn(t)
	ident, ok := taskCaptureProcIdentity(pid)
	if !ok {
		t.Skipf("cannot capture the identity of the foreign stand-in pid %d", pid)
	}
	rec := procIdentity{Pid: ident.Pid, StartTicks: ident.Start, Cmd: ident.Cmd}
	if !finishCleanupProcessHolderLive(rec) {
		t.Skipf("the foreign stand-in pid %d does not read as live here", pid)
	}
	if finishCleanupSelfTree()[pid] {
		t.Fatalf("fixture error: pid %d must be a THIRD PARTY to this asker, but the self-tree excluded it", pid)
	}
	return rec
}

// TestReclaimOwnFinishedWorktreesFreesItsOwnGroundAndKeepsAForeigns is the arm's
// end-to-end pair, and both halves matter: the SAME pass, on the SAME ledger,
// must free the directory whose only run is this process and leave the one a
// foreign party is in.
func TestReclaimOwnFinishedWorktreesFreesItsOwnGroundAndKeepsAForeigns(t *testing.T) {
	ownReleaseSelfMatchLedger(t)
	repo, wt := worktreeRefusalFixture(t, t.TempDir())
	// The row's branch MUST match the branch checked out in the directory
	// (reclaimDisposition's rung ①), which is also what keeps the lineage rung
	// answerable: the fixture repo HAS the integration branch, so the deliverable's
	// branch shares an ancestor with it.
	const branch = "feat/x"
	self := ownReleaseSelfIdentity(t)
	ownRow := ownReleaseSelfMatchRow(t, "own-ground", repo, branch, wt, self)

	// The FOREIGN ground: a second linked worktree of the same repo, a row of
	// OURS (same claim stamp — the arm's candidate set) whose recorded runner is a
	// live third party, with that very process standing in the directory as a cwd
	// holder. That is the concurrent-session shape measured on 2026-09-17: the
	// ledger records ONE runner, and a second party keeps working in the ground.
	foreignWT := filepath.Join(filepath.Dir(repo), "wt-foreign")
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", "-q", foreignWT, "-b", "feat/foreign")
	foreignRunner := ownReleaseForeignRunner(t)
	foreignRow := ownReleaseSelfMatchRow(t, "foreign-ground", repo, "feat/foreign", foreignWT, foreignRunner)

	lines := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(lines, "\n")

	// ① the finished run's own ground is FREED — the directory, and with it the
	// registration gate [c] greps. This is the arm's whole job, and it happens
	// while the recorded runner (this process) is provably alive.
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("this process's own finished ground must be freed (stat err=%v); pass lines:\n%s", err, joined)
	}
	if !strings.Contains(joined, ReclaimSelfReleasedMarker) || !strings.Contains(joined, ownRow) {
		t.Errorf("the release must be NAMED and name the row it freed, got:\n%s", joined)
	}
	if pidAlive(self.Pid) != true {
		t.Fatal("fixture error: the recorded runner of the freed row must still be alive")
	}
	// ② the FOREIGN party's ground survives — the directory, and its registration.
	if _, err := os.Stat(foreignWT); err != nil {
		t.Fatalf("a ground a THIRD PARTY is standing in must survive this pass: %v\npass lines:\n%s", err, joined)
	}
	if !worktreeListed(t, repo, foreignWT) {
		t.Fatalf("the foreign ground's registration must survive:\n%s", worktreeList(t, repo))
	}
	if !strings.Contains(joined, reclaimSelfRefusedMarker) || !strings.Contains(joined, foreignRow) {
		t.Errorf("the foreign ground's refusal must be NAMED and name its row, got:\n%s", joined)
	}
	if !strings.Contains(joined, strconv.Itoa(foreignRunner.Pid)) {
		t.Errorf("the refusal must name the foreign runner pid %d it saw, got:\n%s", foreignRunner.Pid, joined)
	}
	// ③ the BRANCH of the freed ground is the deliverable: never deleted.
	if got := gitRevParse(t, repo, "--verify", "--quiet", branch); got == "" {
		t.Fatalf("the freed ground's branch must survive the release")
	}
}

// ownReleaseSessionDirStandIn starts a process whose CWD is dir and which is
// reparented to init in its own session — a THIRD PARTY to this asker by
// measurement, not by assumption (finishCleanupSelfTree is asserted against it by
// the caller). A plain child of the test would be a DESCENDANT of the asker and
// therefore excluded by the very closure under test, so the fixture has to leave
// this process tree to exercise evidence ②. Returns the pid.
func ownReleaseSessionDirStandIn(t *testing.T, dir string) int {
	t.Helper()
	script := "cd " + strconv.Quote(dir) + " || exit 1; sleep 60 >/dev/null 2>&1 </dev/null & echo $!"
	out, err := exec.Command("/bin/sh", "-c", script).Output()
	if err != nil {
		t.Skipf("cannot start the session-directory stand-in: %v", err)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(out)))
	if convErr != nil || pid <= 0 {
		t.Skipf("the stand-in reported no pid (%q)", strings.TrimSpace(string(out)))
	}
	//stylegate:ignore signals the fixture process the test spawned
	t.Cleanup(func() { _ = exec.Command("kill", strconv.Itoa(pid)).Run() })
	// The reparenting is the fixture's premise (a descendant would be excluded
	// by the asker's own tree); wait for the measured state, and FAIL when it
	// does not arrive — an unreparented stand-in is still the asker's own kind of
	// process, so a skip here would report success for a shape never built.
	waitOrphanReparentTarget(t, pid, orphanSettleWait, "session-directory stand-in")
	return pid
}

// TestReclaimOwnFinishedWorktreesKeepsAGroundAForeignCwdHolderStandsIn pins
// evidence ② on its own: the row's recorded runner is THIS process (so the
// recorded-runner bit is excluded and stays quiet) while a THIRD PARTY is
// standing in the row's own session directory. Before the fix the exclusion did
// not exist; a fix that excluded the whole session directory instead of only this
// asker's process tree would pass the acceptance test and quietly delete the
// ground a concurrent session is building in — this test is what refuses that.
func TestReclaimOwnFinishedWorktreesKeepsAGroundAForeignCwdHolderStandsIn(t *testing.T) {
	ownReleaseSelfMatchLedger(t)
	repo, wt := worktreeRefusalFixture(t, t.TempDir())
	self := ownReleaseSelfIdentity(t)
	row := ownReleaseSelfMatchRow(t, "session-peer", repo, "feat/x", wt, self)

	// A genuinely foreign process whose CWD is the row's own session container —
	// the measured shape of "a second session of this row is working" (the row's
	// session dir is the cwd of every process that adopts the row). The directory
	// is created exactly where the production path resolves it.
	sessionDir, err := taskSessionDir(row)
	if err != nil {
		t.Fatalf("resolve the row's session dir: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("create the row's session dir: %v", err)
	}
	holder := ownReleaseSessionDirStandIn(t, sessionDir)
	if finishCleanupSelfTree()[holder] {
		t.Fatalf("fixture error: the stand-in pid %d must be a third party", holder)
	}
	waitProcCwd(t, holder, sessionDir)

	lines := ReclaimOwnFinishedWorktrees()
	joined := strings.Join(lines, "\n")
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("a third party standing in the row's own session directory must keep the ground: %v\npass lines:\n%s", err, joined)
	}
	if !strings.Contains(joined, reclaimSelfRefusedMarker) {
		t.Errorf("the refusal must be NAMED, got:\n%s", joined)
	}
	if !strings.Contains(joined, sessionDir) {
		t.Errorf("the refusal must name the session directory it saw, got:\n%s", joined)
	}
}
