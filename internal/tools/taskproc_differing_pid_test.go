package tools

// "THE TWO PIDS DIFFER" IS NOT "CANNOT JUDGE" (bug epic 01a1b5bb).
//
// THE BUG THESE TESTS PIN, measured on this host 2026-11-07 on 4/4 live worker
// rows: `gopi task status <id>` printed
//
//	verdict:     unknown
//	resumable:   yes          ← the operator's next step is `gopi worker --resume`
//	holder:      host-a:3718508 (claim stamp is fresh for this run)
//	recorded:    pid=3721192 …
//	live:        pid 3718508 alive=yes cmd="gopi worker"
//
// i.e. one report saying, in three lines, that the holder is ALIVE, that the
// evidence does not settle anything, and that the row may be started again. The
// shape is the normal one, not a corner: the claim stamp names the `gopi worker`
// PARENT and the identity triple names the CHILD it spawned, so
// `ProcPid != claimStampPid` fired on every healthy run and the `unknown` fell
// through taskVerdictResumable's default to `yes`. An operator acting on that
// yes starts a SECOND executor on the same files.
//
// THE FIXTURE IS A REAL PARENT/CHILD PAIR, and that is the point of this file: a
// same-pid fixture would have passed on the broken code. A spawned `bash` claims
// the row (the stamp's pid) and its `sleep` child is what the row RECORDS as the
// executor, so the two numbers genuinely differ while BOTH are alive — the exact
// shape the fix must judge rather than abstain on.
//
// BOTH DIRECTIONS ARE PINNED (changing the default instead of the branch would
// block the rows that really are resumable):
//
//	TestDifferingPidsWithALiveRecordedExecutorReadRunning   the fix
//	TestDifferingPidsWithADeadRecordedExecutorStaysUnknown  the conservative half
//	TestADifferingPidOnAForeignStampStaysUnmeasurable       the host arm
//
// NO POLLING AND NO DISCARDED ERRORS: the child's pid is READ off the parent's
// stdout (a blocking read, so no waiting loop), and a "provably gone" pid comes
// from a process the test STARTED AND REAPED itself rather than from a kill
// followed by a deadline. Both were rewritten to meet the stylegate's
// test-scope ceilings (`test-sleep` and `ignored-error`, both 0) — the earlier
// polling/kill shape measured 4 sleeps and 5 discarded errors, which is drift in
// this tree's own terms, not a cosmetic complaint.

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/runlock"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// differingPidRowID is the canonical-uuid row the fixtures use: the cwd-row
	// rung recognises a row by internal/scratch's MINTED container-name
	// predicate, so a made-up name would make every arm here pass for the wrong
	// reason ("the directory names no row").
	differingPidRowID = "01a1b5bb-1cb0-753a-a712-61eb39423410"
	// differingPidChildArgv is the child's argv[1] — the ONE definition the
	// spawned command and the recorded command line both read.
	differingPidChildArgv = "120"
	// differingPidChildCmd is what the child's cmdline reads, derived from the
	// argument above so the two cannot drift.
	differingPidChildCmd = "sleep " + differingPidChildArgv
	// differingPidParentHoldArgv is how long the PARENT stays alive after
	// printing its child's pid. It must outlive the arm that measures the pair:
	// a parent that reaped its child and exited would make the two pids stop
	// differing, and that arm would then be measuring nothing (measured on this
	// file's first draft, where `wait` did exactly that).
	differingPidParentHoldArgv = "300"
)

// differingPidRunDir builds an isolated profile whose sessions/<row> container
// exists, and answers that container's path — the directory the children are
// started IN, so the cwd-row rung has a real row to compare against instead of
// "the row records no row identity".
func differingPidRunDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir := filepath.Join(config.ProfileDirIn(home), config.SessionsDirName, differingPidRowID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("fixture: create the row's session container: %v", err)
	}
	return dir
}

// differingPidProc is one spawned PARENT (the claim stamp's pid) with its real
// CHILD (the row's recorded executor), both alive.
type differingPidProc struct {
	parent    *exec.Cmd // the `bash` that claims the row
	parentPID int
	childPID  int // the `sleep` the row records
}

// startDifferingPidPair spawns `bash -c 'sleep N & echo $! ; exec sleep M'` in
// dir and reports the pair. The child's pid comes from bash itself (`$!`), read
// off the parent's stdout — the kernel's own fork, read through a pipe, never a
// guess and never a poll.
func startDifferingPidPair(t *testing.T, dir string) *differingPidProc {
	t.Helper()
	cmd := exec.Command("bash", "-c", differingPidChildCmd+" & echo $! ; exec sleep "+differingPidParentHoldArgv)
	cmd.Dir = dir
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		t.Fatalf("fixture: take the parent's stdout pipe: %v", pipeErr)
	}
	// StartMockProcessCmd is the ONE sanctioned spawn (internal/testutil): it
	// arms Pdeathsig (so a SIGKILLed or -timeout'd test binary takes the pair
	// with it instead of leaving a survivor whose cwd is a checkout) and owns a
	// fresh process GROUP — which is what actually reaps the `sleep` bash
	// forked into the background, since PR_SET_PDEATHSIG is NOT inherited
	// across fork(2) and a plain Pdeathsig on the parent would leave that child
	// standing.
	mp := testutil.StartMockProcessCmd(t, cmd)
	p := &differingPidProc{parent: cmd, parentPID: mp.Pid()}
	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	if readErr != nil {
		t.Fatalf("fixture: the parent never printed its child's pid (%v)", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(line))
	if convErr != nil || pid <= 0 {
		t.Fatalf("fixture: the parent's first line %q is not a pid: %v", line, convErr)
	}
	p.childPID = pid
	// The pair shares one process group (bash forked without `set -m`), so the
	// helper's group kill reaches this child; naming it here keeps the fixture
	// self-evident next to the pid it just read off the pipe.
	t.Cleanup(func() { differingPidSignal(t, p.childPID) })
	if !pidAlive(p.childPID) {
		t.Fatalf("fixture: the child pid %d is not alive — a fixture whose subject is dead proves nothing", p.childPID)
	}
	if p.childPID == p.parentPID {
		t.Fatalf("fixture: parent and child share pid %d — this fixture would pass on the BROKEN code and cannot test the fix", p.parentPID)
	}
	return p
}

// startDifferingPidHolder spawns ONE long-lived process that stands in for the
// claim stamp's pid — the `gopi worker` PARENT on a real row — and answers its
// pid. It is a single process so a caller that needs a live stamp WITHOUT a live
// child (the "recorded executor is gone" arm) does not have to explain why it
// spawned a pair it does not measure.
func startDifferingPidHolder(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("sleep", differingPidParentHoldArgv)
	cmd.Dir = dir
	// The sanctioned spawn for the same two reasons as the pair above: Pdeathsig
	// survives a SIGKILLed test binary, and the owned process group is what the
	// teardown addresses (Rule 42: it signals only what it created).
	mp := testutil.StartMockProcessCmd(t, cmd)
	if !pidAlive(mp.Pid()) {
		t.Fatalf("fixture: the stamp holder %d is not alive", mp.Pid())
	}
	return mp.Pid()
}

// differingPidGone answers a pid this process STARTED AND REAPED, so /proc no
// longer has it. `Wait` is the reap: the pid is released by the kernel before it
// returns, which makes "gone" a MEASUREMENT the fixture performed rather than a
// deadline it hoped for.
func differingPidGone(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("bash", "-c", "exit 0")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatalf("fixture: spawn the throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("fixture: reap the throwaway process %d: %v", pid, err)
	}
	if pidAlive(pid) {
		t.Fatalf("CONTROL FAILED: pid %d is still alive after its own Wait() — this arm cannot test the 'gone' shape", pid)
	}
	return pid
}

// differingPidSignal kills one pid this TEST started, and RECORDS a failure to
// do so (an ignored error here would be Rule 15b's defect: a cleanup that did
// not happen and did not say so).
func differingPidSignal(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	proc, findErr := os.FindProcess(pid)
	if findErr != nil {
		t.Logf("cleanup: pid %d no longer findable: %v", pid, findErr)
		return
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Logf("cleanup: kill pid %d: %v", pid, err)
	}
}

// differingPidKillAndReap tears down ONE process this test started, through the
// handle that created it (Rule 42: signal only what you own) — and the parent's
// own reaping is what keeps its `sleep` child from being orphaned.
func differingPidKillAndReap(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	differingPidSignal(t, cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		// Expected: the process was signalled. Logged rather than discarded so a
		// genuinely surprising wait error is still visible.
		t.Logf("cleanup: reap pid %d: %v", cmd.Process.Pid, err)
	}
}

// differingPidRow builds the ledger row image: the claim stamp names stampPID,
// the identity triple names execPID. It is the measured worker shape verbatim (a
// fresh, local stamp; a recorded executor that differs from it).
func differingPidRow(t *testing.T, stampPID, execPID int) *Task {
	t.Helper()
	// The incarnation is read only when the executor is ALIVE. The "provably
	// gone" arm records a pid whose /proc entry this kernel no longer has, so
	// demanding ticks there would make that fixture impossible — and the row
	// shape it must test is exactly "identity recorded, executor gone", where
	// the recorded command line is the only surviving factor.
	ticks, ok := procStartTicks(execPID)
	if execPID > 0 && pidAlive(execPID) && (!ok || ticks <= 0) {
		t.Fatalf("fixture: the LIVE executor's incarnation was not readable (ok=%v ticks=%d)", ok, ticks)
	}
	now := float64(time.Now().Unix())
	return &Task{
		TaskID:          differingPidRowID,
		Status:          taskStatusInProgress,
		ClaimedBy:       hostPid(t, stampPID),
		HasClaim:        true,
		ClaimedAt:       now,
		HasInProgressAt: true, InProgressAt: now,
		HasProcIdentity: true,
		ProcPid:         execPID,
		ProcStartTicks:  ticks,
		ProcCmd:         differingPidChildCmd,
	}
}

// TestDifferingPidsWithALiveRecordedExecutorReadRunning is THE fix: the two pids
// differ, the recorded executor is measurably alive with a matching identity, so
// the row is RUNNING and NOT resumable — and the report names which pid was
// judged and why the differing pair is not "cannot judge".
func TestDifferingPidsWithALiveRecordedExecutorReadRunning(t *testing.T) {
	dir := differingPidRunDir(t)
	p := startDifferingPidPair(t, dir)
	task := differingPidRow(t, p.parentPID, p.childPID)

	st := taskProcessStatus(task)

	if st.Verdict != TaskProcessStateRunning {
		t.Fatalf("verdict = %q (note: %s), want %q — the recorded executor (pid %d) is alive and its "+
			"identity matches while the claim stamp names a DIFFERENT live pid (%d); abstaining here is "+
			"what authorised a second executor on 4/4 live rows",
			st.Verdict, st.Note, TaskProcessStateRunning, p.childPID, p.parentPID)
	}
	if st.Resumable {
		t.Fatalf("resumable = yes on a row whose executor is ALIVE (pid %d) — this is the double-run the epic exists to stop", p.childPID)
	}
	// The JUDGED pid is the recorded executor, not the stamp's — a reader must
	// not have to guess which of the two numbers the verdict is about.
	if st.JudgedPid != p.childPID || st.JudgedPidRole != taskJudgedPidRoleRecordedExecutor {
		t.Fatalf("judged pid = %d/%q, want %d/%q", st.JudgedPid, st.JudgedPidRole,
			p.childPID, taskJudgedPidRoleRecordedExecutor)
	}
	if !strings.Contains(st.JudgedPidWhy, strconv.Itoa(p.parentPID)) || !strings.Contains(st.JudgedPidWhy, strconv.Itoa(p.childPID)) {
		t.Fatalf("judged_pid_why does not name BOTH pids (%d and %d): %q", p.parentPID, p.childPID, st.JudgedPidWhy)
	}
	if !st.ExecutorAlive || st.ExecutorStartTick <= 0 {
		t.Fatalf("the executor's own reading is missing (alive=%v ticks=%d) — the verdict rests on it",
			st.ExecutorAlive, st.ExecutorStartTick)
	}

	// THE OPERATOR-FACING TEXT, asserted on the rendered report (a struct that
	// carries the facts but prints one of them still misleads).
	report := TaskProcessStatusReport(st)
	if line := findReportLine(report, "verdict:     "); !strings.Contains(line, TaskProcessStateRunning) {
		t.Fatalf("the report's verdict line is %q, want %q — report:\n%s", line, TaskProcessStateRunning, report)
	}
	if line := findReportLine(report, "resumable:   "); !strings.Contains(line, "no") {
		t.Fatalf("the report's resumable line is %q, want no — report:\n%s", line, report)
	}
	judged := findReportLine(report, "judged:      ")
	if !strings.Contains(judged, strconv.Itoa(p.childPID)) || !strings.Contains(judged, taskJudgedPidRoleRecordedExecutor) {
		t.Fatalf("the report has no `judged:` line naming pid %d as the %s — it printed:\n%s",
			p.childPID, taskJudgedPidRoleRecordedExecutor, report)
	}
	if !strings.Contains(report, "executor:    ") {
		t.Fatalf("the report prints no `executor:` line beside its `live:` line, so the two pids are "+
			"indistinguishable to a reader:\n%s", report)
	}
	// The JSON face carries the same subject (a machine consumer branches on it).
	blob, err := TaskProcessStatusJSON(st)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, want := range []string{`"judged_pid":`, `"judged_pid_role": "recorded-executor"`, `"judged_pid_why":`} {
		if !strings.Contains(blob, want) {
			t.Fatalf("the JSON face does not carry %s — a machine consumer cannot tell which pid was judged:\n%s", want, blob)
		}
	}
}

// TestDifferingPidsWithADeadRecordedExecutorStaysUnknown is the CONSERVATIVE
// half, and it is the one that keeps the fix from moving toward danger: the row
// records an executor that is PROVABLY gone (this test started and reaped it)
// while the claim stamp names a DIFFERENT live pid, so this host can neither
// confirm nor rule out that somebody is executing the row. The answer stays
// `unknown` / `resumable: yes` — the crash-recovery path — exactly as before the
// fix.
func TestDifferingPidsWithADeadRecordedExecutorStaysUnknown(t *testing.T) {
	dir := differingPidRunDir(t)
	stampPID := startDifferingPidHolder(t, dir)
	gonePID := differingPidGone(t, dir)
	if stampPID == gonePID {
		t.Fatalf("CONTROL FAILED: the stamp and the recorded executor are the same pid %d — the two pids "+
			"must differ for this arm to test anything", stampPID)
	}
	task := differingPidRow(t, stampPID, gonePID)

	st := taskProcessStatus(task)
	if st.Verdict != TaskProcessStateUnknown {
		t.Fatalf("verdict = %q (note: %s), want %q — a GONE recorded executor must not be promoted to "+
			"running, and it also must not be called dead while the stamp names another live pid",
			st.Verdict, st.Note, TaskProcessStateUnknown)
	}
	if !st.Resumable {
		t.Fatalf("resumable = no on a row whose recorded executor is provably gone — that is the "+
			"crash-recovery path this fix must not block (verdict %q)", st.Verdict)
	}
	if st.JudgedPidRole == taskJudgedPidRoleRecordedExecutor {
		t.Fatalf("the report claims it judged the recorded executor (%d) although the fix did not adopt it", gonePID)
	}
}

// TestADifferingPidOnAForeignStampStaysUnmeasurable is the HOST arm (Rule 15a):
// a stamp minted on another machine names a pid in ANOTHER kernel's namespace, so
// this host measured nothing — about either number — and the answer is the same
// `unknown` it was before, with `resumable` refused by the host flag.
func TestADifferingPidOnAForeignStampStaysUnmeasurable(t *testing.T) {
	dir := differingPidRunDir(t)
	p := startDifferingPidPair(t, dir)
	task := differingPidRow(t, p.parentPID, p.childPID)
	task.ClaimedBy = "host-b:" + strconv.Itoa(p.parentPID)

	st := taskProcessStatus(task)
	if st.Verdict != TaskProcessStateUnknown || !st.Foreign {
		t.Fatalf("verdict = %q foreign=%v, want %q/true — a foreign stamp must stay unmeasurable here",
			st.Verdict, st.Foreign, TaskProcessStateUnknown)
	}
	if st.Resumable {
		t.Fatalf("resumable = yes on a FOREIGN-stamped row: the run may be alive over there")
	}
	if st.JudgedPidRole == taskJudgedPidRoleRecordedExecutor {
		t.Fatalf("the fix judged a recorded executor while the stamp was foreign — the host arm must come first")
	}
}

// TestRunningVerdictBesideAFreeRunLockReconcilesItsSubject is THE arm for bug
// epic 01a1b5f2: `verdict: running` printed directly above `run_lock: free`,
// with nothing in the report saying why the two may coexist.
//
// MEASURED, BOTH DIRECTIONS (2026-11-07, this host), which is why the fix is
// wording and attribution rather than a change to either criterion:
//
//  1. the flock is PER RUN DIRECTORY, not per process — one `gopi worker` pid
//     held row 01a1b5f2's lock while being row 01a1b5e2's recorded executor
//     (`/proc/locks` named it for one row's inode while the other row's lock
//     read `free`);
//  2. that flock is taken PER ROUND and released at every round end
//     (cmd/agent/cli_tasks_worker.go: `defer relRunLock()`), so a live executor
//     between two rounds legitimately holds nothing.
//
// Neither line is false. What was false was the sentence under them — `the
// kernel flock above is the verdict` — since no verdict computation in this tree
// reads RunLockState (`grep -n RunLockState internal/tools/*.go` finds one
// ASSIGNMENT and the render site, nothing else). So this arm pins three things:
// the pair is reachable, the reconciliation names the SUBJECTS, and the old
// false attribution is GONE.
//
// It reuses THIS file's fixture on purpose (a second fixture would be a second
// answer to "what does a stamp-with-no-lock look like"): the same real
// parent/child pair, with the stamp written by the test and the flock left to
// nobody — the exact shape an operator photographed on the real row.
func TestRunningVerdictBesideAFreeRunLockReconcilesItsSubject(t *testing.T) {
	dir := differingPidRunDir(t)
	p := startDifferingPidPair(t, dir)

	// The stamp is written (so `lock_stamp:` has a pid to print) and NOBODY
	// takes the flock — the measured shape, not a synthesised one.
	stamp := "pid=" + strconv.Itoa(p.parentPID) + "\nstartticks=1\nacquired_at=2026-11-07T12:17:44Z\nhost=" + hostnameOrEmpty() + "\n"
	if err := os.WriteFile(runlock.PathIn(dir), []byte(stamp), 0o600); err != nil {
		t.Fatalf("fixture: write the lock stamp: %v", err)
	}

	task := differingPidRow(t, p.parentPID, p.childPID)
	st := taskProcessStatusWithGroundFacts(task)

	// THE PAIR MUST BE REACHABLE, or this arm measures nothing: a `running`
	// verdict with a LIVE flock would make the reconciliation branch untested
	// (and a `free` lock with no live executor would print nothing to explain).
	if st.Verdict != TaskProcessStateRunning {
		t.Fatalf("verdict = %q (%s), want %q — this arm needs the row to be RUNNING for the pair to exist",
			st.Verdict, st.Note, TaskProcessStateRunning)
	}
	if st.RunLockState != runlock.LockStateFree {
		t.Fatalf("run_lock = %q (%s), want %q — the fixture holds no flock, so a non-free reading means "+
			"something else took it and this arm is measuring the wrong thing",
			st.RunLockState, st.RunLockWhy, runlock.LockStateFree)
	}

	report := TaskProcessStatusReport(st)

	// 1. THE OLD SENTENCE IS GONE. It asserted a data flow the tree does not
	//    have; leaving it in place is the defect, so its absence is pinned.
	const falseAttribution = "the kernel flock above is the verdict"
	if strings.Contains(report, falseAttribution) {
		t.Fatalf("the report still claims %q — no verdict computation reads RunLockState "+
			"(one assignment at taskproc.go:905, then the render site), so that sentence is false about the "+
			"`verdict:` line printed above it. Report:\n%s", falseAttribution, report)
	}

	// 2. A RECONCILIATION IS PRINTED, and it is on its own labelled line so a
	//    reader (or a script) can find it without parsing prose.
	note := findReportLine(report, taskStatusLockNoteLabel)
	if note == "" {
		t.Fatalf("`verdict: running` and `run_lock: free` are printed with NO reconciliation line — "+
			"that is exactly the shape this epic was filed on, and the reader has one decision to make "+
			"(\"may I touch this row now?\"). Report:\n%s", report)
	}

	// 3. IT NAMES BOTH SUBJECTS: the process the verdict measured, and the fact
	//    that the lock's subject is the DIRECTORY. Naming only one half leaves
	//    the other reading as a denial of it.
	if !strings.Contains(note, strconv.Itoa(st.JudgedPid)) {
		t.Fatalf("the reconciliation does not name the pid the verdict was measured against (%d): %q", st.JudgedPid, note)
	}
	for _, want := range []string{"PROCESS", "RUN DIRECTORY", "PER ROUND"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the reconciliation does not say %q — without it `free` still reads as "+
				"\"nobody is executing this row\": %q", want, note)
		}
	}

	// 4. IT ANSWERS THE READER'S DECISION in favour of the verdict — a
	//    reconciliation that explains the pair and then leaves the choice open
	//    has moved the ambiguity, not removed it.
	if !strings.Contains(note, "DECIDE ON `verdict:`") {
		t.Fatalf("the reconciliation explains the pair but does not say which line decides: %q", note)
	}

	// 5. THE STAMP STILL PRINTS, and still says it is not a verdict. Rule 1 of
	//    this epic's constraints: `run_lock`/`lock_stamp` are the only
	//    instruments for "stamp alive, executor long dead", so a fix that
	//    silenced them would be deletion, not repair.
	if line := findReportLine(report, taskStatusLockStampLabel); !strings.Contains(line, strconv.Itoa(p.parentPID)) {
		t.Fatalf("the stamp's pid is no longer printed (%q) — it is the instrument that detects a stamp "+
			"outliving its writer:\n%s", line, report)
	}
	if !strings.Contains(report, taskLockStampDiagnosis) || !strings.Contains(taskLockStampDiagnosis, "NOT a verdict") {
		t.Fatalf("the stamp no longer carries its not-a-verdict diagnosis:\n%s", report)
	}

	// THE RENDERED REPORT, logged verbatim: this arm is the ONE place the
	// `running` + `free` pair is produced on demand (a live row reaches it only
	// between two rounds), so the evidence a reader needs is this text.
	t.Logf("report for verdict=%s run_lock=%s:\n%s", st.Verdict, st.RunLockState, report)
}
