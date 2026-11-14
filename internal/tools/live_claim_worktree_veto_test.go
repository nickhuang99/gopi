package tools

// Regression tests for the 2026-09-14 incident (task 01a09d67): an ACTIVE
// session's linked worktree (~/workspace/gopi-nopriv-daemon) was deleted twice
// mid-run by an external sweep while its owner was running `go test` inside it.
// ~30 phantom test failures (getwd/open on relative paths) followed and were
// initially misread as code regressions.
//
// TWO independent defects, one test each:
//
//	RC-A (window truncation) — worktreeSweepTaskRows loaded the OLDEST
//	  reclaimScanMax rows with `ORDER BY updated_at ASC LIMIT 200`, while the
//	  incident ledger held 286 worktree rows. The newest 86 — precisely the
//	  LIVE ones — were never loaded, so the ownership veto (worktreeSweepJudge
//	  rung ③) never saw the live owner (row 01a09d07, position 275) and the
//	  sweep saw an unowned-looking, clean, aged registration.
//	TestWorktreeSweepSeesTheNewestRowsOnALargeLedger is the red proof.
//
//	RC-B (dead pid != dead holder) — the dead-holder reclaim arm
//	  (reclaimDeadHolderCandidate) had NO live-row veto at all, and the holder
//	  verdicts read "the claim pid is dead" as "the holder is dead". The only
//	  liveness channel that can see a foreign session's process is
//	  /proc/<pid>/cwd, which is EACCES across sessions under
//	  kernel.yama.ptrace_scope=1 (measured on this host: 185 of ~360 entries),
//	  so the probe failed OPEN for exactly the processes a sweep must not
//	  disturb. The fix anchors liveness on the row's OWN recorded runner
//	  identity (proc_pid + proc_start_ticks + proc_cmd — readable
//	  cross-session), so an unresolvable claim pid/host can no longer turn
//	  "alive" into "dead".
//	TestLiveClaimRunnerSurvivesDeadHolderReclaim / ...UnreadableClaimStamp are
//	the red proofs; TestDeadHolderReclaimStillFreesAProvenDeadRunner is the
//	other direction (the guard is not a blanket refusal).

import (
	"context"
	"fmt"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// --- command-line measurability (the fork→exec window) ---------------------
//
// MEASURED 2026-10-12 (task 01a12b6a, this host): immediately after
// exec.Command(...).Start() returns, /proc/<pid>/cmdline is READABLE AND ZERO
// BYTES until the child reaches execve. The file exists (the directory does),
// but the kernel has no argv yet, so procCmdline() answers "" — indistinguishable
// from "this process has no command line".
//
// Two probes (repeat runs, Rule 27 — no load generator was used):
//
//	probe_cmdline_window  400 spawns, immediate read: empty=183 (46%)  rawlen=0  state=R
//	probe_window_duration 300 spawns, first read:    empty=39  (13%)  widest window 1.58ms
//
// The ratio tracks host load (46% vs 13% for the same code), which is why this
// presented as a load-related flake: the fixture read the file ONCE, inside the
// window, and got "" for BOTH readings (procCmdline() and the raw spelling), so
// the precondition — "the raw spelling differs from the canonical rendering" —
// compared "" to "" and reported a FIXTURE failure as an assertion failure.
// The production classifier was never wrong: an empty cmdline is genuinely no
// evidence (see procCmdline's doc).
//
// The remedy is a BOUNDED RETRY over the same read (never a sleep-and-pray, and
// never a relaxed assertion): the window is closed microseconds after execve, so
// the second or third read answers. 64 × 2ms = 128ms is ~80× the widest window
// measured above. When the read still answers nothing, the failure NAMES the
// fixture (Rule 15: no silent failure, and no reporting of read jitter as a
// verdict about the code under test).
const (
	// cmdlineAwaitAttempts is the bounded retry count for one command-line read.
	cmdlineAwaitAttempts = procname.CmdlineAwaitAttempts
	// cmdlineAwaitStep is the pause between retries; attempts × step is the
	// total window allowed for the child to reach execve.
	cmdlineAwaitStep = procname.CmdlineAwaitStep
)

// cmdlineReading is ONE attempt at reading a process command line: Value is the
// reading ("" = the kernel has not answered yet — the fork window, or a process
// that has no argv), Err is a read failure of the file itself.
type cmdlineReading struct {
	Value string
	Err   error
}

// awaitCmdline retries a command-line read until it answers something, up to
// attempts reads spaced by step. It returns the value and an empty reason on
// success, or ("", reason) — the reason naming the fixture label, the retry
// count and what the reads actually did — when the line stayed unmeasurable.
//
// It is deliberately SEPARATE from the t.Fatalf wrapper below: a retry that can
// only be exercised through a Fatal is untestable, and an untestable retry is
// how the next reader ends up with a sleep-and-pray window instead.
func awaitCmdline(read func() cmdlineReading, label string, attempts int, step time.Duration) (string, string) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && step > 0 {
			time.Sleep(step) //stylegate:allow-sleep paces the /proc re-reads of the veto fixture: the cmdline of a spawned holder is not visible in the first polls
		}
		r := read()
		if r.Value != "" {
			return r.Value, ""
		}
		if r.Err != nil {
			lastErr = r.Err
		}
	}
	why := fmt.Sprintf(
		"%s: no command line after %d reads (%v apart) — the kernel never answered /proc/<pid>/cmdline",
		label, attempts, step)
	if lastErr != nil {
		why += "; last read error: " + lastErr.Error()
	} else {
		why += "; every read returned an EMPTY file (the pre-exec window, or a pid the kernel gives no argv)"
	}
	return "", why
}

// requireProcCmdline is the fixture precondition for a spawned helper: the
// command line the ledger would record, read only once the kernel answers it.
// Unmeasurable after the bounded retry is a LOUD failure naming the fixture —
// never a silent "" that a later string comparison turns into a false verdict.
func requireProcCmdline(t *testing.T, pid int, what string) string {
	t.Helper()
	v, why := awaitCmdline(func() cmdlineReading {
		return cmdlineReading{Value: procCmdlineAt(cmdlineFixtureRoot, pid)}
	}, what, cmdlineAwaitAttempts, cmdlineAwaitStep)
	if v == "" {
		t.Fatalf("fixture (pid %d, cmdline path %s): %s",
			pid, filepath.Join(cmdlineFixtureRoot, strconv.Itoa(pid), procCmdlineName), why)
	}
	return v
}

// requireRawProcCmdlineSpelling is requireProcCmdline's raw-transcription
// sibling: the same bounded retry over the VERBATIM file, NUL separators
// rendered as spaces (the trailing-separator spelling a fixture writes). Both
// readings sit inside the same fork window, so both need the retry — the
// measured flake needed BOTH of them to come back "" for the premise to fail.
func requireRawProcCmdlineSpelling(t *testing.T, pid int, what string) string {
	t.Helper()
	path := filepath.Join(cmdlineFixtureRoot, strconv.Itoa(pid), procCmdlineName)
	v, why := awaitCmdline(func() cmdlineReading {
		b, err := os.ReadFile(path)
		if err != nil {
			return cmdlineReading{Err: err}
		}
		return cmdlineReading{Value: strings.ReplaceAll(string(b), "\x00", " ")}
	}, what+" ("+path+")", cmdlineAwaitAttempts, cmdlineAwaitStep)
	if v == "" {
		t.Fatalf("fixture %s", why)
	}
	return v
}

// liveRunner is a real, live process standing in for a task row's RUNNER (the
// `gopi worker` / session process), recorded the way the ledger records it:
// pid + start ticks + command line.
type liveRunner struct {
	identity procIdentity
}

// startLiveRunner spawns a long-lived child and returns its recorded identity.
// A real process is used, never a forged pid: the point of these tests is that
// a LIVE process survives the sweep, and a fabricated identity would only
// prove the predicate can be fooled.
func startLiveRunner(t *testing.T) liveRunner {
	t.Helper()
	// StartMockProcess owns the spawn: it sets a process group the reaper can
	// address, arms Pdeathsig so the child dies even when this test BINARY is
	// SIGKILLed (a t.Cleanup never runs then — measured 2026-10-27, task
	// 01a179d7: the hand-rolled spawn below leaked one `sleep 300` per killed
	// run, cwd inside the checkout, which is what makes a ground read IN USE),
	// and keeps the child's cwd out of the repository under test.
	mp := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	pid := mp.Pid()
	start, ok := procStartTicks(pid)
	if !ok {
		t.Skipf("cannot read start ticks for pid %d", pid)
	}
	if !pidAlive(pid) {
		t.Skipf("helper pid %d is not alive", pid)
	}
	return liveRunner{identity: procIdentity{
		Pid:        pid,
		StartTicks: start,
		Cmd:        requireProcCmdline(t, pid, "pre-exec window: the helper's procCmdline() is empty"),
	}}
}

// seedRunnerIdentity writes the row's recorded runner identity — the values
// the runner itself stamps at run start (taskProcIdentity).
func seedRunnerIdentity(t *testing.T, taskID string, rec procIdentity) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET proc_pid = ?, proc_start_ticks = ?, proc_cmd = ? WHERE task_id = ?`,
		rec.Pid, rec.StartTicks, rec.Cmd, taskID); err != nil {
		t.Fatalf("seed runner identity %s: %v", taskID, err)
	}
}

// seedLiveStatusRow writes a LIVE-status row (in_progress) naming a worktree,
// whose CLAIM stamp names a provably DEAD pid — the incident's ledger shape.
func seedLiveStatusRow(t *testing.T, taskID, repo, branch, worktree, claimedBy string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "live " + taskID, Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	seedDeliverable(t, taskID, repo, branch, worktree)
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	now := float64(time.Now().Unix())
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, `+tasksInProgressAtColumnName+` = ?, updated_at = ? WHERE task_id = ?`,
		taskStatusInProgress, claimedBy, now, now, taskID); err != nil {
		t.Fatalf("forge live row %s: %v", taskID, err)
	}
}

// reclaimCandidatesFor runs the reclaim scan and returns the candidates naming
// worktree — the exact set the removal arm iterates.
func reclaimCandidatesFor(t *testing.T, worktree string) []reclaimDeadHolderRow {
	t.Helper()
	rows, err := reclaimDeadHolderRows()
	if err != nil {
		t.Fatalf("reclaim scan: %v", err)
	}
	var out []reclaimDeadHolderRow
	for _, r := range rows {
		if finishCleanupSameWorktreePath(r.Worktree, worktree) {
			out = append(out, r)
		}
	}
	return out
}

// TestLiveClaimRunnerSurvivesDeadHolderReclaim is the incident's exact shape: a
// LIVE-status row owns the directory, its claim stamp names a dead pid, and its
// recorded RUNNER is a live process. The reclaim scan must produce NO candidate
// for that directory — before the fix it produced one and the removal arm freed
// the tree out from under the running session.
//
// This is the "claimed pid/host does not resolve locally" direction in its
// resolvable-claim-pid form; the unresolvable form is the next test.
func TestLiveClaimRunnerSurvivesDeadHolderReclaim(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	runner := startLiveRunner(t)

	seedLiveStatusRow(t, "t-live-runner", repo, branch, wt, hostPid(t, deadPID(t)))
	seedRunnerIdentity(t, "t-live-runner", runner.identity)

	if got := reclaimCandidatesFor(t, wt); len(got) != 0 {
		t.Fatalf("a live runner's worktree must never be a reclaim candidate, got %d: %+v", len(got), got)
	}
	// The other half of the fact: the row is still a HOLDER for the cleanup
	// chain's own verdict, so the finish-cleanup gate refuses it too.
	if !finishCleanupOwnerRowLive(taskStatusInProgress, hostPid(t, deadPID(t)), runner.identity, wt) {
		t.Error("finishCleanupOwnerRowLive must keep the veto while the recorded runner is alive")
	}
}

// TestLiveClaimRunnerSurvivesWhenClaimStampIsUnresolvable is the briefing's
// explicit case: "the claimed pid/host does not resolve locally". The claim
// stamp names ANOTHER HOST (unknowable here, so it can never be called dead)
// while the row's recorded runner is alive on this host — the directory must
// stay vetoed through every arm.
func TestLiveClaimRunnerSurvivesWhenClaimStampIsUnresolvable(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	runner := startLiveRunner(t)

	const foreignHost = "some-other-host:4242"
	seedLiveStatusRow(t, "t-foreign-stamp", repo, branch, wt, foreignHost)
	seedRunnerIdentity(t, "t-foreign-stamp", runner.identity)

	if got := reclaimCandidatesFor(t, wt); len(got) != 0 {
		t.Fatalf("an unresolvable claim stamp plus a live runner must not be a candidate, got %d: %+v", len(got), got)
	}
	if !finishCleanupOwnerRowLive(taskStatusInProgress, foreignHost, runner.identity, wt) {
		t.Error("a foreign claim stamp is unknowable here — never called dead")
	}
	// And the dead-local-holder disposition must NOT fire while a runner lives,
	// whatever the claim stamp says.
	if finishCleanupDeadLocalHolder(taskStatusInProgress, hostPid(t, deadPID(t)), runner.identity) {
		t.Error("a live recorded runner must not be reported as a dead local holder")
	}
}

// sweepRootsFor permits one directory tree in the sweep's root policy, the way
// a declared `repos`/root entry does — so the judge reaches its LEDGER rung
// instead of stopping at the out-of-policy guard.
func sweepRootsFor(path string) []worktreeroots.Root {
	return []worktreeroots.Root{{
		Path:   path,
		Role:   worktreeroots.RoleWorktree,
		Source: worktreeroots.SourceSettingsKey,
	}}
}

// rawProcCmdlineSpelling transcribes /proc/<pid>/cmdline the way a fixture (and
// any writer that read the file verbatim) does: NUL separators become spaces,
// so the string carries a TRAILING separator. It is the spelling difference —
// not the runner's identity — that was this host's false death proof on
// 2026-09-17 (see the scratch rig scripts/repro-live-holder-worktree-veto.sh).
func rawProcCmdlineSpelling(t *testing.T, pid int) string {
	t.Helper()
	return requireRawProcCmdlineSpelling(t, pid, "used by rawProcCmdlineSpelling")
}

// TestProcessHolderLiveIgnoresTheRecordedCmdlineSpelling pins the classifier's
// ladder directly and cheaply: pid + start ticks MATCH, the recorded cmdline is
// spelled differently from procCmdline()'s rendering, and the runner must still
// count as LIVE. Before the fix the cmdline was compared as a separate identity
// axis, so any spelling difference (raw /proc transcription, in-place exec,
// truncation at procIdentityCmdlineMax) turned a live runner into a death proof
// and handed its directory to the sweep. The counterpart check below keeps the
// refusal direction honest: a recycled pid (start ticks differ) is still gone.
func TestProcessHolderLiveIgnoresTheRecordedCmdlineSpelling(t *testing.T) {
	runner := startLiveRunner(t)
	const otherSpelling = "sleep 300 "
	if runner.identity.Cmd == "" {
		t.Skip("cannot read the helper's cmdline")
	}
	if runner.identity.Cmd == otherSpelling {
		t.Fatalf("precondition: procCmdline() already renders %q — the spelling case is untestable here", otherSpelling)
	}
	if !finishCleanupProcessHolderLive(procIdentity{
		Pid:        runner.identity.Pid,
		StartTicks: runner.identity.StartTicks,
		Cmd:        otherSpelling,
	}) {
		t.Fatalf("a live runner (pid %d, matching start ticks) was called NOT live because its recorded cmdline — %q — differs from procCmdline()'s %q",
			runner.identity.Pid, otherSpelling, runner.identity.Cmd)
	}
	if finishCleanupProcessHolderLive(procIdentity{
		Pid:        runner.identity.Pid,
		StartTicks: runner.identity.StartTicks + 1, // the number was recycled
		Cmd:        runner.identity.Cmd,
	}) {
		t.Error("a recycled pid (start ticks differ) must not count as this row's runner")
	}
}

// TestSweepKeepsTheWorktreeOfALiveRecordedRunner is the SWEEP-level red proof
// for the incident shape (2026-09-14, task 01a09d67): the reclaim arm was not
// the only consumer of the dead-local-holder verdict. Rung ③ of the sweep's
// judge decides ownership from the SAME classifier
// (worktreeSweepRowHoldsGround → HolderProvenDead), so a row whose claim stamp
// names a dead pid while its own recorded RUNNER is a live process was
// classified "holder proven dead", rung ③ skipped it, and the sweep fell
// through to `unclaimed-reclaim` — deleting the directory and dropping the git
// registration of a session that was still running in it.
//
// The recorded runner identity is seeded in the RAW /proc spelling (the one the
// scratch rig's fixture writes), which is what made the pre-fix comparison a
// false death proof. Together with TestProcessHolderLiveIgnoresTheRecordedCmdlineSpelling
// this pins both the load-bearing clause and the arm it feeds.
//
// This test drives the REAL loader and the REAL judge on a REAL git
// registration, so it also pins the loader's column plumbing: a verdict
// derived from the wrong columns shows up here as a reclaimed worktree.
func TestSweepKeepsTheWorktreeOfALiveRecordedRunner(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	runner := startLiveRunner(t)
	raw := runner.identity
	raw.Cmd = rawProcCmdlineSpelling(t, raw.Pid)
	if raw.Cmd == runner.identity.Cmd {
		t.Fatalf("precondition: the raw /proc spelling equals procCmdline()'s (%q) — this test would pin nothing", raw.Cmd)
	}
	seedLiveStatusRow(t, "t-sweep-live-runner", repo, branch, wt, hostPid(t, deadPID(t)))
	seedRunnerIdentity(t, "t-sweep-live-runner", raw)

	// (a) the classifier half: the loaded row must NOT carry a death proof.
	rows, err := worktreeSweepTaskRows(context.Background())
	if err != nil {
		t.Fatalf("sweep row loader: %v", err)
	}
	var row tasksRowForSweep
	found := false
	for _, r := range rows {
		if r.TaskID == "t-sweep-live-runner" {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatal("the live row is missing from the sweep's evidence")
	}
	if row.HolderProvenDead {
		t.Fatal("a LIVE recorded runner was classified HolderProvenDead — rung ③ will skip the row and reclaim its worktree")
	}
	if !worktreeSweepRowHoldsGround(row, sweepOptionsNow(true)) {
		t.Fatal("the row does not hold ground although its recorded runner is alive")
	}

	// (b) the judge half: the registration that row names is KEPT as owned.
	opts := sweepOptionsNow(true)
	reg := worktreeRegistration{Repo: repo, Path: wt, Branch: branch}
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}
	f := worktreeSweepJudge(reg, rows, scratch.HolderScan{}, sweepRootsFor(filepath.Dir(wt)), opts, ho)
	if f.Disposition != string(worktreeKeep) {
		t.Fatalf("the sweep would %s a live runner's worktree (%s): %s", f.Disposition, f.Rule, f.Reason)
	}
	if f.Rule != string(worktreeRuleLedgerOwnership) {
		t.Fatalf("kept for the wrong reason: rule=%s (%s), want %s", f.Rule, f.Reason, worktreeRuleLedgerOwnership)
	}
	if !strings.Contains(f.Reason, "t-sweep-live-runner") {
		t.Errorf("the keep reason must name the owning row, got %q", f.Reason)
	}
}

// TestDeadHolderReclaimStillFreesAProvenDeadRunner is the direction that keeps
// the guard honest: the fix must not become a blanket refusal. A live-status
// row whose claim pid is dead AND whose recorded runner is dead is still
// reclaimed — the orphan the earlier epic deliberately un-stranded. Without
// this test a "return true always" fix would pass the two above.
func TestDeadHolderReclaimStillFreesAProvenDeadRunner(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	// A runner that really ran and really exited: spawn, record, reap.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	deadRunnerPID := cmd.Process.Pid
	start, ok := procStartTicks(deadRunnerPID)
	if !ok {
		t.Skip("cannot read start ticks")
	}
	rec := procIdentity{Pid: deadRunnerPID, StartTicks: start, Cmd: procCmdline(deadRunnerPID)}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if pidAlive(deadRunnerPID) {
		t.Fatalf("precondition: pid %d must be reaped and dead", deadRunnerPID)
	}

	seedLiveStatusRow(t, "t-dead-runner", repo, branch, wt, hostPid(t, deadPID(t)))
	seedRunnerIdentity(t, "t-dead-runner", rec)

	got := reclaimCandidatesFor(t, wt)
	if len(got) != 1 {
		t.Fatalf("a provably dead runner's directory must still be reclaimable, got %d candidates", len(got))
	}
	if !got[0].LiveRow {
		t.Error("the orphan shape (live status, provably dead holder) must be reported as a live row")
	}
	if finishCleanupOwnerRowLive(taskStatusInProgress, hostPid(t, deadPID(t)), rec, wt) {
		t.Error("a provably dead runner must not keep the holder veto")
	}
}

// TestWorktreeSweepSeesTheNewestRowsOnALargeLedger is the RC-A red proof: the
// sweep's ledger evidence used to be truncated to the OLDEST 200 worktree rows,
// which on the incident host (286 rows) dropped the newest 86 — the live ones.
// The live owner was therefore invisible to the ownership veto and its
// directory was reclaimed.
//
// The test seeds more worktree rows than the window and asserts the NEWEST live
// row is present in the loaded evidence AND that the sweep's judge keeps its
// registration. Pre-fix the loader returned 200 rows without the newest one.
func TestWorktreeSweepSeesTheNewestRowsOnALargeLedger(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	repo, wt, branch := deadHolderReclaimFixture(t)

	const filler = reclaimScanMax + 50 // comfortably past the old window
	base := time.Now().Add(-72 * time.Hour)
	for i := 0; i < filler; i++ {
		id := fmt.Sprintf("t-filler-%03d", i)
		if _, err := TaskSubmit(TaskSubmitOptions{Summary: "filler", Payload: "p", TaskID: id, Source: "human"}); err != nil {
			t.Fatalf("submit filler %d: %v", i, err)
		}
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	// Every filler row names a worktree (so it is inside the LIKE filter) and is
	// OLD, so all of them sort before the live row below.
	for i := 0; i < filler; i++ {
		id := fmt.Sprintf("t-filler-%03d", i)
		blob := `{"repo":"` + repo + `","branch":"feat/filler-` + fmt.Sprintf("%03d", i) + `","worktree":"` + filepath.Join(filepath.Dir(repo), fmt.Sprintf("filler-%03d", i)) + `"}`
		if _, err := db.ExecContext(ctx,
			`UPDATE tasks SET deliverable = ?, status = ?, claimed_by = ?, updated_at = ? WHERE task_id = ?`,
			blob, taskStatusCompleted, hostPid(t, deadPID(t)), float64(base.Unix())+float64(i), id); err != nil {
			t.Fatalf("forge filler %d: %v", i, err)
		}
	}
	db.Close() //stylegate:ignore reopened by the sweep loader below

	// The LIVE row is the NEWEST row in the ledger — the one an age-ASC LIMIT
	// drops first.
	seedLiveStatusRow(t, "t-newest-live", repo, branch, wt, hostPid(t, os.Getpid()))

	rows, err := worktreeSweepTaskRows(ctx)
	if err != nil {
		t.Fatalf("sweep row loader: %v", err)
	}
	if len(rows) < filler+1 {
		t.Fatalf("the sweep's evidence must not be truncated: loaded %d rows, seeded %d — a capped window drops the NEWEST (live) rows first", len(rows), filler+1)
	}
	found := false
	for _, r := range rows {
		if r.TaskID == "t-newest-live" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the newest (live) row is missing from the sweep's evidence — the ownership veto can never see its worktree")
	}
}

// --- RC-C: the pre-exec window must never be reported as a verdict ----------

const (
	// cmdlineFatalChildMarker brands the re-exec'd child of
	// TestEmptyCmdlineProcTreeFailsLoudlyNamingTheFixture: the Fatalf arm can
	// only be proven by READING a child's failure, never by surviving it.
	cmdlineFatalChildMarker = "GOPI_CMDLINE_FATAL_CHILD"
	// cmdlineFatalRootEnv carries the synthetic procfs root the child must read,
	// so the parent can prove the message names the FIXTURE and not the host.
	cmdlineFatalRootEnv = "GOPI_CMDLINE_FATAL_ROOT"
	// cmdlineFatalArmEnv selects which precondition arm the child drives.
	cmdlineFatalArmEnv = "GOPI_CMDLINE_FATAL_ARM"
	// cmdlineFatalArmIdentity drives requireProcCmdline (the seam
	// identityProcRoot, the one procCmdline() reads).
	cmdlineFatalArmIdentity = "identity"
	// cmdlineFatalArmRaw drives requireRawProcCmdlineSpelling (the verbatim
	// transcription, the seam rawCmdlineRoot). BOTH readings sit inside the same
	// fork window — the measured false premise needed BOTH of them to come back
	// "" — so both arms carry the retry and both are pinned here.
	cmdlineFatalArmRaw = "raw"
)

// cmdlineFixtureRoot is the procfs root the TWO cmdline preconditions read —
// both requireProcCmdline (the canonical rendering, procCmdlineAt) and
// requireRawProcCmdlineSpelling (the verbatim transcription). Its default is the
// production alias procRootName, REFERENCED and never re-typed (Rule 13); a test
// overrides it so both arms can be driven over a synthetic tree.
//
// It is the CMDLINE-read sibling of identityProcRoot, which covers the
// comparison side (procname.RunnerLive/RunnerIdentity) but NOT this read: the
// measured flake's reading path is procCmdline → procCmdlineAt(procRootName, …),
// so pointing identityProcRoot at a fixture would leave the read on the host.
// In production the two roots hold the same value
// (procname.DefaultProcRoot), which is why the distinction is invisible until a
// fixture replaces one of them.
//
// It lives in the test file on purpose: the brief for this fix scopes the change
// to internal/tools' tests, so the seam is stated here rather than widening a
// production var.
var cmdlineFixtureRoot = procRootName

// withCmdlineFixtureRoot points the cmdline preconditions at a synthetic procfs
// root for one test and restores the production value.
func withCmdlineFixtureRoot(t *testing.T, root string) {
	t.Helper()
	prev := cmdlineFixtureRoot
	cmdlineFixtureRoot = root
	t.Cleanup(func() { cmdlineFixtureRoot = prev })
}

// writeEmptyCmdlineProcTree builds a synthetic procfs tree holding ONE pid slot
// whose cmdline file EXISTS and is ZERO BYTES — the kernel's exact shape inside
// the fork→exec window (measured on this host 2026-10-12: 183/400 spawns read
// 0 bytes with state=R, window up to 1.58ms). A fixture that merely OMITTED the
// file would exercise ENOENT instead — a different defect with a different
// message — so the file is written empty, and its byte count is asserted.
func writeEmptyCmdlineProcTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(identityTestPid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, procCmdlineName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("fixture: write empty cmdline %s: %v", path, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("fixture: stat %s: %v", path, err)
	}
	if fi.Size() != 0 {
		t.Fatalf("fixture: %s is %d bytes, want the EMPTY file the fork window produces", path, fi.Size())
	}
	return root
}

// TestAwaitCmdlineRetriesUntilTheKernelAnswers is the GREEN half of the
// red/green pair for the retry: a read that answers nothing at first and then
// answers must be waited out, and the retry must stop at the FIRST answer
// (bounded, never a spinner dressed as patience).
func TestAwaitCmdlineRetriesUntilTheKernelAnswers(t *testing.T) {
	root := writeEmptyCmdlineProcTree(t)
	withCmdlineFixtureRoot(t, root)

	const (
		waitingReads = 3 // the fork window, as the probe sized it (max 1.58ms)
		attempts     = 8
	)
	reads := 0
	got, why := awaitCmdline(func() cmdlineReading {
		reads++
		if reads <= waitingReads {
			return cmdlineReading{Value: ""} // the kernel has not answered yet
		}
		return cmdlineReading{Value: "sleep 300"}
	}, "fixture: cmdline-await retry proof", attempts, time.Millisecond)
	if why != "" {
		t.Fatalf("the retry gave up on a line that answered on read %d: %s", waitingReads+1, why)
	}
	if got != "sleep 300" {
		t.Fatalf("the retry returned %q, want the line the later read answered", got)
	}
	if reads != waitingReads+1 {
		t.Fatalf("the retry made %d reads, want exactly %d — it must stop at the FIRST answer", reads, waitingReads+1)
	}
}

// TestAwaitCmdlineGivesUpWithAReasonNamingTheFixture is the LOUD half: an
// unmeasurable read must produce a reason that names the fixture, the retry
// count and the fact that the reads were EMPTY — and it must do so after the
// BOUNDED number of reads, not by spinning.
//
// It reads the REAL reader over the REAL empty file (the production spelling
// through the seam), because a stubbed "" would not prove that path resolution
// and the empty-file shape meet.
func TestAwaitCmdlineGivesUpWithAReasonNamingTheFixture(t *testing.T) {
	root := writeEmptyCmdlineProcTree(t)
	withCmdlineFixtureRoot(t, root)

	const (
		label    = "fixture: empty-cmdline proc tree"
		attempts = 5
	)
	reads := 0
	v, why := awaitCmdline(func() cmdlineReading {
		reads++
		return cmdlineReading{Value: procCmdlineAt(cmdlineFixtureRoot, identityTestPid)}
	}, label, attempts, 0)
	if v != "" {
		t.Fatalf("a zero-byte cmdline fixture produced %q — the fixture is not the pre-exec shape", v)
	}
	if reads != attempts {
		t.Fatalf("the retry made %d reads, want exactly %d (bounded)", reads, attempts)
	}
	if why == "" {
		t.Fatal("giving up produced NO reason — that is exactly the silent failure of Rule 15")
	}
	if !strings.Contains(why, label) {
		t.Errorf("the reason must NAME the fixture label %q, got %q", label, why)
	}
	if !strings.Contains(why, "EMPTY") {
		t.Errorf("the reason must say the reads came back EMPTY (the pre-exec window), got %q", why)
	}
}

// TestEmptyCmdlineProcTreeFailsLoudlyNamingTheFixture drives BOTH production-font
// preconditions — requireProcCmdline (the canonical rendering) and
// requireRawProcCmdlineSpelling (the verbatim transcription), both reading
// through the cmdlineFixtureRoot seam — over the same empty-cmdline tree, and
// reads the result from a re-exec'd child so the Fatalf is examined rather than
// survived.
//
// The assertions are the anti-regression: the child MUST fail (an empty cmdline
// can never be handed up to a string comparison as if it were a reading), the
// message MUST name the fixture and the synthetic root, and it MUST NOT be the
// pre-fix message — "the raw /proc spelling equals procCmdline()'s" — which is
// the false verdict this fix removes. That sentence is the whole defect: the
// fixture's unmeasurable reading was reported as an assertion about the pair.
func TestEmptyCmdlineProcTreeFailsLoudlyNamingTheFixture(t *testing.T) {
	if os.Getenv(cmdlineFatalChildMarker) == "" {
		for _, arm := range []string{cmdlineFatalArmIdentity, cmdlineFatalArmRaw} {
			root := writeEmptyCmdlineProcTree(t)
			cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
			cmd.Env = append(os.Environ(),
				cmdlineFatalChildMarker+"=1",
				cmdlineFatalArmEnv+"="+arm,
				cmdlineFatalRootEnv+"="+root,
			)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("[%s] the child PASSED although its cmdline fixture is a zero-byte file — the precondition is not loud:\n%s", arm, out)
			}
			text := string(out)
			if !strings.Contains(text, "fixture") {
				t.Errorf("[%s] the failure must name the fixture, got:\n%s", arm, text)
			}
			if !strings.Contains(text, root) {
				t.Errorf("[%s] the failure must name the synthetic procfs root %s — otherwise the fixture was not the thing under test:\n%s", arm, root, text)
			}
			if !strings.Contains(text, strconv.Itoa(identityTestPid)) {
				t.Errorf("[%s] the failure must name the pid slot %d, got:\n%s", arm, identityTestPid, text)
			}
			if !strings.Contains(text, "EMPTY") {
				t.Errorf("[%s] the failure must say the reads were EMPTY, got:\n%s", arm, text)
			}
			if strings.Contains(text, "this test would pin nothing") {
				t.Errorf("[%s] the child reported the PRE-FIX verdict (an unmeasurable reading rendered as an assertion about the pair):\n%s", arm, text)
			}
		}
		return
	}

	// The re-exec'd child: read the root the parent built, point the matching
	// seam at it, and drive that arm's precondition.
	root := os.Getenv(cmdlineFatalRootEnv)
	if root == "" {
		t.Fatalf("child: %s is unset", cmdlineFatalRootEnv)
	}
	switch os.Getenv(cmdlineFatalArmEnv) {
	case cmdlineFatalArmIdentity:
		withCmdlineFixtureRoot(t, root)
		_ = requireProcCmdline(t, identityTestPid, "empty-cmdline fixture probe")
	case cmdlineFatalArmRaw:
		withCmdlineFixtureRoot(t, root)
		_ = requireRawProcCmdlineSpelling(t, identityTestPid, "empty-cmdline fixture probe")
	default:
		t.Fatalf("child: unknown arm %q", os.Getenv(cmdlineFatalArmEnv))
	}
	t.Fatal("precondition: the fixture is a zero-byte cmdline, so this line must be unreachable")
}
