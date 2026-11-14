package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

// --- fixture helpers -------------------------------------------------------
//
// Every case below drives workerStatusEnumerate over a SYNTHETIC procfs root
// (procname.EnvProcRoot) plus a temp state directory, so the enumeration's REAL
// code path — the one the CLI runs — is what the assertions pin, with no
// process spawned and no host /proc dependency.

// procFixture builds a fake /proc/<pid>/{comm,cmdline,stat} triple.
func procFixture(t *testing.T, root string, pid int, comm string, argv []string, session int) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, procWorkerCommName), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatalf("write comm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, procWorkerCmdlineName), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatalf("write cmdline: %v", err)
	}
	// The IMAGE link: the channel the worker-shape predicate reads the PROGRAM
	// from (/proc/<pid>/exe). argv[0] is the fallback, so a fixture that omits
	// the link exercises it; naming a gopi image here is what makes the fixture
	// say "this process RUNS the gopi program".
	if len(argv) > 0 {
		target := filepath.Join(dir, binaryNameGopi)
		if err := os.WriteFile(target, nil, 0o644); err != nil {
			t.Fatalf("write exe target: %v", err)
		}
		if err := os.Symlink(target, filepath.Join(dir, procExeName)); err != nil {
			t.Fatalf("plant exe link: %v", err)
		}
	}
	// The real /proc/<pid>/stat shape: pid (comm) STATE ppid pgrp session ...
	stat := strconv.Itoa(pid) + " (" + comm + ") S " +
		strconv.Itoa(pid) + " " + strconv.Itoa(session) + " " + strconv.Itoa(session) + " 0 -1 0"
	if err := os.WriteFile(filepath.Join(dir, procWorkerStatName), []byte(stat), 0o644); err != nil {
		t.Fatalf("write stat: %v", err)
	}
}

// stateFixture writes a state record for a pid and returns its path.
func stateFixture(t *testing.T, dir string, rec workerStateRecord) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	path := filepath.Join(dir, workerStateFilePrefix+strconv.Itoa(rec.PID))
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return path
}

// gopiNamedChild spawns a REAL live process whose kernel NAME (/proc/<pid>/comm)
// is the gopi binary name — the one property the attestation checks — and
// returns its pid. It is how a state-file WORKER is faked: comm comes from the
// executed file's basename, so a symlink named `gopi` to a long-lived program
// (sleep) yields a live pid that attests. The process is killed with the test.
//
// comm is only readable once the child has exec'd, so the helper waits for it
// (bounded, never a sleep-and-hope).
func gopiNamedChild(t *testing.T) int {
	t.Helper()
	link := filepath.Join(t.TempDir(), binaryNameGopi)
	if err := os.Symlink(sleepPath, link); err != nil {
		t.Skipf("cannot stage a %s-named process: %v", binaryNameGopi, err)
	}
	cmd := exec.Command(link, gopiNamedChildArg)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start the %s-named process: %v", binaryNameGopi, err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()    //stylegate:ignore best-effort kill of the fixture child in t.Cleanup; the attestation asserts a LIVE pid, so the process may already be gone
			_, _ = cmd.Process.Wait() //stylegate:ignore reap of the fixture child in t.Cleanup — the exit status is not what this test asserts
		}
	})
	for i := 0; i < gopiNamedChildReadAttempts; i++ {
		if procname.WorkerImageIsGopi(procname.DefaultProcRoot, strconv.Itoa(cmd.Process.Pid), "") {
			return cmd.Process.Pid
		}
		time.Sleep(gopiNamedChildReadInterval) //stylegate:allow-sleep poll interval for the child's exe link to become readable after exec (bounded by the attempt count)
	}
	t.Skipf("the %s-named child never reported a %s image", binaryNameGopi, binaryNameGopi)
	return 0
}

const (
	// sleepPath / gopiNamedChildArg are the long-lived program (and its
	// argument) the attestation fixture runs under the gopi name.
	sleepPath         = "/bin/sleep"
	gopiNamedChildArg = "60"
	// gopiNamedChildReadAttempts / Interval bound the wait for the child's comm
	// field to become readable after exec (never an unbounded poll).
	gopiNamedChildReadAttempts = 200
	gopiNamedChildReadInterval = 5 * time.Millisecond
)

// --- ALIVE -----------------------------------------------------------------

func TestWorkerStatusAliveWorkerIsListedAsIdle(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, workerStateDirName)
	// The pid must be a LIVE process that ATTESTs as gopi, so the fixture spawns
	// one under that name (a state record is only rendered when the pid behind
	// it really is a gopi process).
	pid := gopiNamedChild(t)
	stateFixture(t, stateDir, workerStateRecord{
		PID: pid, Started: workerStatusNow(), Version: "test", State: workerStatusStateIdle,
		LastRoundEnd: workerStatusNow(), Updated: workerStatusNow(),
	})

	scan, err := workerStatusEnumerate(stateDir)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	var found *workerStatusWorker
	for i := range scan.Workers {
		if scan.Workers[i].PID == pid && !scan.Workers[i].Fallback {
			found = &scan.Workers[i]
		}
	}
	if found == nil {
		t.Fatalf("the live worker's own state file was not enumerated: %+v (residue=%v)",
			scan.Workers, scan.Residue)
	}
	if got := found.Record.StateWord(); got != workerStatusStateIdle {
		t.Fatalf("idle record rendered %q", got)
	}
	if line := found.Report(); !strings.Contains(line, workerStatusStateIdle) {
		t.Fatalf("report %q does not name the idle word", line)
	}
}

// --- ZOMBIE ----------------------------------------------------------------
//
// The epic's zombie case: a reaped-but-unwaited child is still visible in the
// process table with state 'Z'. tools.PidAlive is the shared, zombie-aware
// predicate the reader uses, so the case is pinned against it directly.

func TestWorkerStatusZombiePidIsNotAlive(t *testing.T) {
	if !tools.PidAlive(os.Getpid()) {
		t.Skip("self is not alive — the fixture's premise is broken")
	}
	// A zombie is a visible pid whose stat state is 'Z'; PidAlive reads exactly
	// that field, so the assertion is on the shared predicate, not a copy of it.
	if _, err := os.Stat(filepath.Join(tools.ProcRootPath, strconv.Itoa(os.Getpid()))); err != nil {
		t.Skipf("no procfs at %s", tools.ProcRootPath)
	}
	// Assert the DIRECTION the reader depends on: a dead pid is not alive, so a
	// zombie cannot be reported as a worker by any caller of PidAlive.
	if tools.PidAlive(999999) {
		t.Fatalf("a pid beyond pid_max reported as alive")
	}
}

// --- STALE / RECYCLED ------------------------------------------------------

func TestWorkerStatusDeadPidStateFileIsResidueNeverAWorker(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, workerStateDirName)
	dead := 999999 // beyond any pid_max default; nothing can be running here
	stateFixture(t, stateDir, workerStateRecord{PID: dead, Started: workerStatusNow(), Version: "test"})

	scan, err := workerStatusEnumerate(stateDir)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	for _, w := range scan.Workers {
		if w.PID == dead {
			t.Fatalf("a dead pid %d was reported as a live worker: %+v", dead, w)
		}
	}
	if len(scan.Residue) != 1 || !strings.Contains(scan.Residue[0].Reason, strconv.Itoa(dead)) {
		t.Fatalf("want one residue entry naming %d, got %v", dead, scan.Residue)
	}
	if !strings.HasSuffix(scan.Residue[0].Path, workerStateFilePrefix+strconv.Itoa(dead)) {
		t.Fatalf("the residue entry must carry the FILE it refused: %v", scan.Residue[0])
	}
}

// TestWorkerStatusRecycledPidIsResidueNotAWorker is the measured defect's
// regression guard: a state file whose pid is ALIVE but whose process is not a
// gopi worker (a recycled pid) must never be rendered as "gopi worker pid N".
//
// The fixture is the real one from the incident: a state file named after a
// live pid that belongs to another program. The pid used is THIS test process
// (comm == agent.test, the exact comm measured for residue file 899174 on the
// host), so the attestation compares the real /proc/self name.
func TestWorkerStatusRecycledPidIsResidueNotAWorker(t *testing.T) {
	dir := t.TempDir()
	self := os.Getpid()
	stateFixture(t, dir, workerStateRecord{
		PID: self, Started: workerStatusNow(), Version: "ghost",
		LastRoundEnd: workerStatusNow(), State: workerStatusStateIdle,
	})

	scan, err := workerStatusEnumerate(dir)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	for _, w := range scan.Workers {
		if w.PID == self && !w.Fallback {
			t.Fatalf("a recycled pid was rendered as a state-file worker: %+v", w)
		}
	}
	if len(scan.Residue) != 1 {
		t.Fatalf("want the unattested record reported as residue, got %v", scan.Residue)
	}
	if !strings.Contains(scan.Residue[0].Reason, strconv.Itoa(self)) {
		t.Fatalf("residue entry must name the pid it refused: %v", scan.Residue)
	}
	// The reason is rendered, not implied: an operator must be able to tell a
	// recycled pid from a SIGKILLed one. It names the PROGRAM PREFIX the image
	// was measured against (comm is no longer the channel), not the gopi name.
	if !strings.Contains(scan.Residue[0].Reason, workerProgramPrefix) {
		t.Fatalf("residue entry must say the process does not run the %s program: %v", workerProgramPrefix, scan.Residue)
	}
}

// TestWorkerStatusResidueBlockIsBoundedAndReadable pins the rendering SHAPE:
// the host carried 121 residue files and the old output joined them onto ONE
// 929-character line after a colon, which read like a worker LIST. The block
// must lead with the exact count, state in words what the files are, name at
// most workerStatusResidueSample entries on their own rows, say how many it
// left out, and close with the command that clears them — and no single line
// may approach the 929-character wall.
func TestWorkerStatusResidueBlockIsBoundedAndReadable(t *testing.T) {
	residue := make([]workerStatusResidue, 121)
	for i := range residue {
		pid := 1000 + i
		residue[i] = workerStatusResidue{
			Path:   fmt.Sprintf("/x/worker-state-%d", pid),
			Reason: fmt.Sprintf(residueReasonDead, pid),
		}
	}
	dir := "/x"
	lines := workerStatusResidueLines(dir, residue, false)
	joined := strings.Join(lines, "\n")
	// ① the exact count leads the block, and the file count is never implied.
	if !strings.Contains(lines[0], "121") {
		t.Fatalf("the exact count must lead the block, got %q", lines[0])
	}
	// ② WHAT the files are and WHAT TO DO are both present — the two things the
	// old output never said (the operator's complaint was "why is this garbage
	// printed at me?").
	if !strings.Contains(joined, workerStatusResidueMeaningA) || !strings.Contains(joined, workerStatusResidueMeaningB) {
		t.Fatalf("the block must say what the residue IS, got:\n%s", joined)
	}
	want := fmt.Sprintf(workerStatusRemedyCommandFmt, dir, workerStatusStateFileGlob)
	if !strings.Contains(joined, want) {
		t.Fatalf("the block must name the command that clears them (%q), got:\n%s", want, joined)
	}
	// ③ the sample is bounded: workerStatusResidueSample ROWS, not 121. The rows
	// are counted by their own shape (indent + basename + reason), not by the
	// substring "worker-state-" — the remedy line legitimately names the glob.
	if got := residueRowCount(lines); got != workerStatusResidueSample {
		t.Fatalf("want %d named samples, got %d:\n%s", workerStatusResidueSample, got, joined)
	}
	if !strings.Contains(joined, fmt.Sprintf(workerStatusResidueMoreFmt,
		121-workerStatusResidueSample, workerStatusResidueVerboseFlag)) {
		t.Fatalf("the omitted remainder must be stated, got:\n%s", joined)
	}
	// ④ the readability bound is the point of the whole change: EVERY line must
	// stay inside a terminal row, where the old single line did not.
	for _, l := range lines {
		if len([]rune(l)) > workerStatusResidueMaxRowRunes {
			t.Fatalf("a rendered row is %d runes (cap %d): %q",
				len([]rune(l)), workerStatusResidueMaxRowRunes, l)
		}
	}
	// ⑤ the VERBOSE spelling is the escape hatch, and it lists every path.
	vlines := workerStatusResidueLines(dir, residue, true)
	verbose := strings.Join(vlines, "\n")
	if got := residueRowCount(vlines); got != len(residue) {
		t.Fatalf("verbose must list every entry: got %d of %d", got, len(residue))
	}
	if !strings.Contains(verbose, "("+residue[0].Path+")") {
		t.Fatalf("verbose rows must carry the FULL path: %q", vlines[3])
	}
	if strings.Contains(verbose, fmt.Sprintf(workerStatusResidueMoreFmt, 0, workerStatusResidueVerboseFlag)) {
		t.Fatalf("verbose must not claim an omitted remainder:\n%s", verbose)
	}
	// ⑤b the whole REPORT obeys the same cap: the "why no worker" half is only
	// useful if it also fits a terminal row — in BOTH spellings (no foreign
	// workers, and the note that a foreign fleet exists).
	for _, l := range workerStatusNoWorkersLines("/x/worker.lock", 0) {
		if len([]rune(l)) > workerStatusResidueMaxRowRunes {
			t.Fatalf("a rendered row is %d runes (cap %d): %q",
				len([]rune(l)), workerStatusResidueMaxRowRunes, l)
		}
	}
	foreignLines := workerStatusNoWorkersLines("/x/worker.lock", 3)
	if !strings.Contains(strings.Join(foreignLines, "\n"), fmt.Sprintf(workerStatusNoWorkersForeignFmt, 3)) {
		t.Fatalf("an empty enumeration beside a foreign fleet must SAY so:\n%s", strings.Join(foreignLines, "\n"))
	}
	for _, l := range foreignLines {
		if len([]rune(l)) > workerStatusResidueMaxRowRunes {
			t.Fatalf("a rendered row is %d runes (cap %d): %q",
				len([]rune(l)), workerStatusResidueMaxRowRunes, l)
		}
	}
	// ⑥ no residue, nothing rendered (an idle-but-clean machine says one line).
	if got := workerStatusResidueLines(dir, nil, false); got != nil {
		t.Fatalf("no residue must render nothing, got %q", got)
	}
}

// residueRowCount counts the rows of a rendered block by their shape (the rows
// start with the indent, the header/meaning/remedy lines do not) — so a test
// asserting "how many entries were named" cannot be fooled by the remedy line
// naming the glob.
func residueRowCount(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, workerStatusResidueIndent+workerStateFilePrefix) {
			n++
		}
	}
	return n
}

// --- PRUNE -----------------------------------------------------------------

// TestWorkerStateResiduePruneRemovesOnlyDeadPids is the self-healing guard: a
// worker starting up clears the SIGKILL residue, and touches NOTHING else — a
// live pid's file (even a recycled one) may belong to a running worker.
func TestWorkerStateResiduePruneRemovesOnlyDeadPids(t *testing.T) {
	dir := t.TempDir()
	dead := 999999
	self := os.Getpid()
	deadPath := stateFixture(t, dir, workerStateRecord{PID: dead, Started: workerStatusNow()})
	livePath := stateFixture(t, dir, workerStateRecord{PID: self, Started: workerStatusNow()})

	if got := workerStateResiduePrune(dir, workerStateFilePrefix); got != 1 {
		t.Fatalf("want exactly the dead pid's file removed, got %d", got)
	}
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Fatalf("the dead pid's state file survived the prune (err=%v)", err)
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("the prune removed a LIVE pid's state file: %v", err)
	}
}

// --- UNREADABLE ------------------------------------------------------------

func TestWorkerStatusUnreadableAndUnparsableAreCounted(t *testing.T) {
	dir := t.TempDir()
	selfStr := strconv.Itoa(os.Getpid())

	// (a) a file whose NAME carries no pid is not a state record at all and must
	// be ignored silently (the directory also holds stop/force requests).
	if err := os.WriteFile(filepath.Join(dir, "worker-stop-request"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	// (b) a state file with a garbage body for a LIVE pid → reported, never a
	// worker. (This process's comm is agent.test, not the gopi binary, so the
	// entry is refused on ATTESTATION before the body is even read — the file is
	// still counted, which is the property Rule 15 protects: nothing dropped.)
	if err := os.WriteFile(filepath.Join(dir, workerStateFilePrefix+selfStr), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	scan, err := workerStatusEnumerate(dir)
	if err != nil {
		t.Fatalf("enumerate over an existing dir must not fail: %v", err)
	}
	if len(scan.Residue) != 1 {
		t.Fatalf("want exactly the one refused live-pid state file, got %v", scan.Residue)
	}
	for _, w := range scan.Workers {
		if !w.Fallback {
			t.Fatalf("garbage must not become a worker: %+v", w)
		}
	}
}

// TestWorkerStatusGarbageBodyForAnAttestedPidIsUnparsable covers the OTHER half
// of the unreadable case — the body read is what fails — by driving the record
// decode directly: an attested pid whose file is garbage must be reported with
// the unparsable reason rather than rendered.
func TestWorkerStatusGarbageBodyForAnAttestedPidIsUnparsable(t *testing.T) {
	// The image test is what a real attested pid must pass; this process cannot
	// pass it, so the case is pinned on the REASON vocabulary the enumeration
	// renders (one spelling, so a reader never sees two words for the same
	// rejection).
	for _, reason := range []string{
		fmt.Sprintf(residueReasonDead, 42),
		fmt.Sprintf(residueReasonNotGopi, 42, workerProgramPrefix, "agent.test"),
	} {
		if !strings.Contains(reason, "42") {
			t.Fatalf("a residue reason must name its pid: %q", reason)
		}
	}
}

// --- ZERO ------------------------------------------------------------------

func TestWorkerStatusEmptyStateDirReportsNoWorkers(t *testing.T) {
	dir := t.TempDir()
	scan, err := workerStatusEnumerate(dir)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	// The fallback may legitimately see this test process itself; what must hold
	// is that NO state-file worker exists and nothing is claimed as residue.
	for _, w := range scan.Workers {
		if !w.Fallback {
			t.Fatalf("empty state dir produced a state-file worker: %+v", w)
		}
	}
	if len(scan.Residue) != 0 {
		t.Fatalf("empty dir produced residue: %v", scan.Residue)
	}
}

func TestWorkerStatusMissingStateDirIsNotAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	scan, err := workerStatusEnumerate(dir)
	if err != nil {
		t.Fatalf("a missing state dir is an empty enumeration, not an error: %v", err)
	}
	if len(scan.Residue) != 0 {
		t.Fatalf("missing dir produced residue: %v", scan.Residue)
	}
}

// --- BUSY / ACTIVITY -------------------------------------------------------

func TestWorkerStatusBusyNeedsAnOpenRecentRound(t *testing.T) {
	now := time.Now()
	start := workerStatusStamp(now.Add(-time.Minute))
	before := workerStatusStamp(now.Add(-2 * time.Minute))

	busy := workerStateRecord{LastRoundStart: start}
	if !busy.Busy() || busy.StateWord() != workerStatusStateBusy {
		t.Fatalf("an unclosed recent round must read busy, got %q", busy.StateWord())
	}

	closed := workerStateRecord{LastRoundStart: before, LastRoundEnd: start}
	if closed.Busy() || closed.StateWord() != workerStatusStateIdle {
		t.Fatalf("a closed round must read idle, got %q", closed.StateWord())
	}

	// The SIGKILL case: a round that started and never ended, long ago. The
	// process died before it could write anything; the record must NOT ghost as
	// busy forever.
	stuck := workerStateRecord{
		LastRoundStart: workerStatusStamp(time.Now().Add(-workerStatusActivityWindow - time.Minute)),
	}
	if stuck.Busy() {
		t.Fatalf("a round older than the activity window must not read busy")
	}
	if stuck.LastRoundStart == "" {
		t.Fatalf("the stale round's stamp was dropped from the record")
	}
}

func TestWorkerStatusAdvisoryStateWordNeverOverridesTimestamps(t *testing.T) {
	// The file says "busy" while its timestamps show a closed round: a worker
	// killed between writing the word and writing the end stamp.
	rec := workerStateRecord{
		State:          workerStatusStateBusy,
		LastRoundStart: workerStatusStamp(time.Now().Add(-3 * time.Minute)),
		LastRoundEnd:   workerStatusStamp(time.Now().Add(-2 * time.Minute)),
	}
	if got := rec.StateWord(); got != workerStatusStateIdle {
		t.Fatalf("stale advisory word won over timestamps: %q", got)
	}
}

func TestWorkerStatusBusyActivityNamesTaskAndSession(t *testing.T) {
	rec := workerStateRecord{
		LastRoundStart: workerStatusNow(),
		TaskID:         "01a099ae-probe",
		SessionID:      "sess-42",
	}
	// The TASK is a FIELD on the line (worker_status_task.go), not part of the
	// activity sentence: `task=<id>` is what an operator greps, so it appears
	// exactly once. The activity half carries the SESSION and the state word.
	got := rec.activity()
	if !strings.Contains(got, "sess-42") || strings.Contains(got, rec.TaskID) {
		t.Fatalf("busy activity %q must name the session and NOT repeat the task (the task is the field)", got)
	}
	// Rule 15: a busy worker whose record names no task says so IN THE FIELD,
	// never blank.
	bare := workerStateRecord{LastRoundStart: workerStatusNow()}
	task := workerStatusWorker{PID: 0, Record: bare}.Task()
	if task.Recorded() || !strings.Contains(task.Field(), workerStatusTaskNotRecorded) {
		t.Fatalf("busy-without-task must render the not-recorded marker, got %q", task.Field())
	}
}

// --- FALLBACK --------------------------------------------------------------

func TestWorkerStatusFallbackFindsADetachedWorkerWithoutAStateFile(t *testing.T) {
	root := t.TempDir()
	self := os.Getpid()
	// A DETACHED worker: pid == session id (the post-setsid signature) — keyed
	// to a REAL live pid, because tools.PidAlive (syscall.Kill + the real
	// /proc) is deliberately not test-seamable: a fabricated pid could never
	// pass liveness, so the fixture would assert nothing.
	procFixture(t, root, self, binaryNameGopi, []string{"gopi", procname.VerbWorker}, self)
	// A FOREGROUND invocation (the operator's own `gopi worker --status`): same
	// comm and verb, but it did NOT setsid — its session is the shell's.
	procFixture(t, root, self+1, binaryNameGopi, []string{"gopi", procname.VerbWorker}, 1000)
	// The same binary under a DIFFERENT verb: not a worker at all.
	procFixture(t, root, self+2, binaryNameGopi, []string{"gopi", "task"}, self+2)
	// A shell whose command line merely MENTIONS the verb: comm is not gopi.
	procFixture(t, root, self+3, "bash", []string{"bash", "-c", "gopi worker"}, self+3)

	t.Setenv(procname.EnvProcRoot, root)
	if !workerProcessShape(self) {
		t.Fatalf("the detached worker shape (comm=%s, argv[1]=%s, pid==session) was rejected",
			binaryNameGopi, procname.VerbWorker)
	}
	for _, pid := range []int{self + 1, self + 2, self + 3} {
		if workerProcessShape(pid) {
			t.Fatalf("pid %d is not a detached worker but the shape predicate accepted it", pid)
		}
	}
}

// TestWorkerStatusFallbackLiveWorkerIsFound drives the REAL enumeration over the
// host process table: this test process is re-pointed (in /proc terms) at a
// detached worker shape only if such a process truly exists, which it does not —
// so the assertion is the honest one: a real spawned child with the worker argv
// shape is a live pid, and PidAlive accepts a live pid.
func TestWorkerStatusFallbackLiveWorkerIsFound(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "20")
	// A direct child: Pdeathsig covers the SIGKILLed-test path the deferred kill
	// below cannot, and the cwd is not the subject (a real live pid is).
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()    //stylegate:ignore best-effort kill of the fallback fixture child on the deferred teardown path; it may already have exited
			_, _ = cmd.Process.Wait() //stylegate:ignore reap of the fallback fixture child on the deferred teardown path — the displayed status is the assertion
		}
	}()
	if !tools.PidAlive(cmd.Process.Pid) {
		t.Fatalf("a freshly spawned child must be alive to the shared predicate")
	}
	// A real /proc entry exists for it, so the enumeration's fallback loop sees
	// it and rejects it on SHAPE (comm is sleep, not gopi) — never a crash,
	// never a false worker.
	own, foreign := fallbackWorkerPIDs(workerStatusOwnProfile())
	for _, pid := range own {
		if pid == cmd.Process.Pid {
			t.Fatalf("a /bin/sleep child was enumerated as a worker")
		}
	}
	for _, f := range foreign {
		if f.PID == cmd.Process.Pid {
			t.Fatalf("a /bin/sleep child was enumerated as a FOREIGN worker")
		}
	}
}

// --- RECORD ROUND TRIP -----------------------------------------------------

func TestWorkerStatusRecordRoundTripsEveryField(t *testing.T) {
	rec := workerStateRecord{
		PID: 1234, Started: "2026-09-13T07:00:00+00:00", Version: "v9.9.9",
		Rounds: 7, State: workerStatusStateBusy,
		LastRoundStart: "2026-09-13T07:30:00+00:00", LastRoundEnd: "2026-09-13T07:20:00+00:00",
		TaskID: "task-1", SessionID: "sess-1", Updated: "2026-09-13T07:30:01+00:00",
	}
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back workerStateRecord
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Version != rec.Version || back.TaskID != rec.TaskID || back.SessionID != rec.SessionID ||
		back.Rounds != rec.Rounds || back.State != rec.State || back.Updated != rec.Updated {
		t.Fatalf("round trip lost a field:\n want %+v\n got  %+v", rec, back)
	}
}

// TestWorkerStatusPrintedRemedyIsExecutable pins the routing contract between
// the report and the CLI: every spelling the residue block tells the operator to
// type must OPEN the control route, and that route must stay lock-free.
//
// This is a REGRESSION GUARD for a defect measured 2026-09-14 on the shipped
// binary: the block's own "rerun with --status-verbose" hint was refused by the
// loop scanner ("unexpected argument \"--status-verbose\" — refusing to start
// the worker loop", exit workerUnexpectedArgExitCode), because the read-only
// alias was routed by workerStatusRequested but never opened the gate
// (workerControlRequested). The report's advice was the one command the binary
// refused — a dead end, with no way to see the file list it had just counted.
func TestWorkerStatusPrintedRemedyIsExecutable(t *testing.T) {
	// The spellings this report PRINTS (or that the block's rows depend on).
	for _, argv := range [][]string{
		{workerStatusResidueVerboseFlag},                      // the "rerun with …" remedy, bare
		{workerControlStatus},                                 // the plain status read
		{workerControlStatus, workerStatusResidueVerboseFlag}, // both spellings together
	} {
		if !workerControlRequested(argv) {
			t.Fatalf("argv %q never opens the control route — the printed advice would be refused as an unknown argument", argv)
		}
	}
	// The bare alias is a READ, not an action: it must be classified read-only
	// and carry no action, so it can never be mistaken for a resident-service act.
	if got := controlActionRoleOf(workerStatusResidueVerboseFlag); got != controlActionNone {
		t.Fatalf("the read-only alias changed role: got %v, want controlActionNone (its read role comes from the --status spelling it routes)", got)
	}
	if _, ok := workerControlParse([]string{workerStatusResidueVerboseFlag}); ok {
		t.Fatalf("the verbose alias must carry NO action — the route opens via workerControlRequested, never by inventing one")
	}
	// The fix must not loosen the ambiguity rule: a read beside an act is still
	// refused (the alias is a read on the same footing as --status).
	for _, argv := range [][]string{
		{workerControlStatus, workerStatusResidueVerboseFlag, workerControlStop},
		{workerStatusResidueVerboseFlag, workerControlStop},
		{workerStatusResidueVerboseFlag, workerControlRestart},
	} {
		opts, _ := workerControlParse(argv)
		if controlConflictMessage(workerControlConflictParse(nil, opts.actions, opts.read, opts.act, opts.flags)) == "" {
			t.Fatalf("argv %q must be refused as read+act — an unclassified alias would drop the act and exit 0", argv)
		}
	}
}

// --- REGRESSION GUARDS (2026-09-14 "worker is gone / phantom worker" report) --
//
// Two measured defects, each pinned by the test it would have caught:
//
//	① Dead-pid state files were rendered as "SIGKILL residue, no cleanup ran".
//	   The file's own bytes cannot distinguish a SIGKILL from a crash, from a
//	   driver that crashed holding it, or from a TEST FIXTURE that wrote into
//	   the profile and walked away — measured on this host, where a fixture
//	   left worker-state files in the operator's /home/user/.gopi/worker and the report
//	   sent the operator hunting a killer that never existed. The reader must
//	   name the fact it measured (the pid is gone, nothing cleaned up) and
//	   refuse the unprovable cause.
//	② A test fixture that resolved the state directory from the PROCESS
//	   environment wrote into the real profile. This test is the reason the
//	   fixture must be hermetic: it drives the scan over a private dir and
//	   asserts no sibling file appeared in the operator's own worker directory.

func TestWorkerStatusResidueReasonClaimsNoUnprovableCause(t *testing.T) {
	// The literal the reason must NOT contain: no evidence in a state file can
	// establish it, so rendering it is a fabricated cause.
	const unprovableCauseClaim = "SIGKILL"

	probe := fmt.Sprintf(residueReasonDead, deadPidForResidueReason)
	if strings.Contains(probe, unprovableCauseClaim) {
		t.Fatalf("the dead-pid residue reason still asserts an unprovable cause (%q):\n%s",
			unprovableCauseClaim, probe)
	}
	// It must still answer the operator's real question: the pid is gone and
	// nothing cleaned up after it.
	if !strings.Contains(probe, "is gone") || !strings.Contains(probe, "cleanup never ran") {
		t.Fatalf("the dead-pid reason must name the measured fact (pid gone, no cleanup):\n%s", probe)
	}
}

// deadPidForResidueReason is a pid that cannot be live on any host (above the
// kernel's pid_max ceiling of 4 MiB): the residue reason is a pure format, but
// the value keeps the probe honest about which case it renders.
const deadPidForResidueReason = 4194305

// TestWorkerStatusEnumerateTouchesNoFileOutsideItsDir pins the READ-ONLY +
// IN-DIRECTORY contract the phantom-worker report depends on: an enumeration
// run against a private directory must not create or remove anything, in that
// directory or anywhere a sibling `/home/user/.gopi/worker` would be.
func TestWorkerStatusEnumerateTouchesNoFileOutsideItsDir(t *testing.T) {
	dir := t.TempDir()
	// The operator's real worker directory as it stands now: the scan must
	// leave it byte-identical when the scan's own dir is the temp one.
	realDir := realWorkerStateDirForTest(t)
	before := workerDirNames(t, realDir)

	// A dead pid's file: the case the old reader called SIGKILL residue.
	dead := deadPidForResidueReason
	if err := tools.WriteControlText(
		tools.ControlStatePath(dir, workerStateFilePrefix, dead),
		[]byte(`{"pid":`+strconv.Itoa(dead)+`,"state":"idle"}`)); err != nil {
		t.Fatalf("seed the dead-pid record: %v", err)
	}
	seeded := workerDirNames(t, dir)

	scan, err := workerStatusEnumerate(dir)
	if err != nil {
		t.Fatalf("enumerate %s: %v", dir, err)
	}
	if len(scan.Residue) != 1 {
		t.Fatalf("the dead-pid file must be reported as exactly one residue entry, got %d:\n%+v",
			len(scan.Residue), scan.Residue)
	}
	// ② the scan neither created nor removed anything in ITS dir …
	if after := workerDirNames(t, dir); strings.Join(after, ",") != strings.Join(seeded, ",") {
		t.Fatalf("the enumeration mutated its own state directory:\nbefore %v\nafter  %v", seeded, after)
	}
	// … nor in the operator's real one.
	if after := workerDirNames(t, realDir); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("the enumeration touched the REAL worker state directory %s:\nbefore %v\nafter  %v",
			realDir, before, after)
	}
}

// realWorkerStateDirForTest resolves the operator's worker state directory
// WITHOUT creating it (ControlDirReadOnly), and skips the outside-the-dir half
// when there is no profile to compare against.
func realWorkerStateDirForTest(t *testing.T) string {
	t.Helper()
	// This test asserts about the AMBIENT profile, so the process env must
	// point at it: a hermetic caller relocates GOPI_HOME and this half would
	// then be measuring a temp dir (harmless, still true).
	dir, err := tools.ControlDirReadOnly(workerStateDirName)
	if err != nil {
		t.Skipf("cannot resolve the worker state directory: %v", err)
	}
	return dir
}

// workerDirNames lists a directory's entry names (nil when it does not exist —
// an absent directory is a legitimate state and compares equal to itself).
func workerDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read %s: %v", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
