package tools

// THE STATUS VIEW'S TWO LOCK LINES (bug epic 01a1ac0f).
//
// The defect these arms pin is a RENDERING defect as much as a measuring one:
// the report used to have no way to show that the pid inside `run.lock` is a
// dead writer's stamp while nobody at all holds the directory. So the assertion
// is made against the rendered TEXT the operator reads, not against the struct:
// a struct that carries both facts but prints one of them still misleads.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/runlock"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// TestStatusReportKeepsTheStampAndTheFlockApart drives the real path —
// taskProcessStatusWithGroundFacts + TaskProcessStatusReport — over a run
// directory whose lock file carries a stamp left by a process that is NOT
// holding it (the measured 2026-11-05 shape: `pid=2662735` in the file, no
// flock in the kernel).
func TestStatusReportKeepsTheStampAndTheFlockApart(t *testing.T) {
	const taskID = "01a1ac0f-0000-0000-0000-0000000f10c1"
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir := filepath.Join(config.ProfileDirIn(home), config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the run directory: %v", err)
	}
	// The stamp names a pid that no longer writes anything; the transcript is
	// 5h18m old — the exact pair the epic was filed on.
	stamp := "pid=518745\nstartticks=179267413\nacquired_at=2026-11-05T14:14:47Z\nhost=host-a\n"
	if err := os.WriteFile(runlock.PathIn(dir), []byte(stamp), 0o600); err != nil {
		t.Fatalf("seed the lock stamp: %v", err)
	}
	record := filepath.Join(dir, sessionlayout.RecordName)
	if err := os.WriteFile(record, []byte("{\"type\":\"user\"}\n"), 0o644); err != nil {
		t.Fatalf("seed the transcript: %v", err)
	}
	old := time.Now().Add(-5*time.Hour - 18*time.Minute)
	if err := os.Chtimes(record, old, old); err != nil {
		t.Fatalf("age the transcript: %v", err)
	}

	// The row carries a LOCAL holder stamp naming THIS process, so the progress
	// axis has a subject: a pid that is measurably ALIVE while the row it holds
	// has not advanced for 5h18m. That pair is the epic.
	holder := hostnameOrEmpty() + claimHolderHostPidSep + strconv.Itoa(os.Getpid())
	task := &Task{TaskID: taskID, Status: taskStatusInProgress, SessionID: taskID, ClaimedBy: holder}
	st := taskProcessStatusWithGroundFacts(task)
	out := TaskProcessStatusReport(st)

	// 1. The FLOCK is the verdict, and it is measured: nobody holds the file.
	if st.RunLockState != runlock.LockStateFree {
		t.Fatalf("RunLockState = %q (%s), want %q — a stamped file nobody holds is FREE", st.RunLockState, st.RunLockWhy, runlock.LockStateFree)
	}
	line := findReportLine(out, taskStatusRunLockLabel)
	if line == "" || !strings.Contains(line, runlock.LockStateFree) {
		t.Fatalf("the report has no %q line saying %q — it printed:\n%s", taskStatusRunLockLabel, runlock.LockStateFree, out)
	}

	// 2. The STAMP is printed — as DIAGNOSIS, beside it.
	stampLine := findReportLine(out, taskStatusLockStampLabel)
	if stampLine == "" || !strings.Contains(stampLine, "518745") {
		t.Fatalf("the report does not print the stamp's pid — it printed:\n%s", out)
	}
	if !strings.Contains(out, taskLockStampDiagnosis) {
		t.Fatalf("the report prints a stamp pid with NO sentence saying it is not the verdict — this is the exact misreading the epic exists to stop:\n%s", out)
	}

	// 3. The PROGRESS axis is rendered, and it says idle — the row is occupied
	//    by nobody and nothing has advanced in 5h18m.
	progressLine := findReportLine(out, taskStatusProgressLabel)
	if progressLine == "" || !strings.Contains(progressLine, TaskProgressIdle) {
		t.Fatalf("the report has no %q line saying %q — it printed:\n%s", taskStatusProgressLabel, TaskProgressIdle, out)
	}
	if !strings.Contains(progressLine, "--takeover") {
		t.Fatalf("the idle line does not carry the recovery command: %q", progressLine)
	}
}

// TestStatusReportOmitsAxesItNeverMeasured is the Rule 15b half: a row with no
// run directory grows no lock/progress lines at all. A blank-but-present line
// would read as a measurement of nothing.
func TestStatusReportOmitsAxesItNeverMeasured(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	st := &TaskProcessStatus{TaskID: "01a1ac0f-0000-0000-0000-0000000dead0", Status: taskStatusPending}
	out := TaskProcessStatusReport(st)
	for _, label := range []string{taskStatusProgressLabel, taskStatusRunLockLabel, taskStatusLockStampLabel} {
		if findReportLine(out, label) != "" {
			t.Fatalf("an unmeasured axis printed a %q line:\n%s", label, out)
		}
	}
}

// mustAtoi64 parses the tick string procname reports, failing the arm rather
// than silently recording a zero identity (which would read as unjudgeable).
func mustAtoi64(t *testing.T, raw string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		t.Fatalf("parse start ticks %q: %v", raw, err)
	}
	return n
}

// findReportLine returns the report line beginning with label ("" when absent).
func findReportLine(report, label string) string {
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, label) {
			return line
		}
	}
	return ""
}

// TestRunRefusalCarriesTheProgressAxis pins the SECOND refusal surface: the
// pre-spawn guard's own text. A live-but-idle holder must be refused WITH the
// axis and the recovery command, because the old text ("wait for the holder to
// finish") sent an operator to wait on a process that would never finish —
// measured 2026-11-05 on holder 518745 (alive 5h18m, 24s CPU, silent since
// 16:57:18).
//
// The arm is driven through taskRunGuardRuling (the row-image half the door and
// the claim paths share), so it asserts the SAME text a runner really gets,
// without starting a second run to find out.
func TestRunRefusalCarriesTheProgressAxis(t *testing.T) {
	const taskID = "01a1ac0f-0000-0000-0000-0000000a7711"
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	dir := filepath.Join(config.ProfileDirIn(home), config.SessionsDirName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the run directory: %v", err)
	}
	record := filepath.Join(dir, sessionlayout.RecordName)
	if err := os.WriteFile(record, []byte("{\"type\":\"user\"}\n"), 0o644); err != nil {
		t.Fatalf("seed the transcript: %v", err)
	}
	old := time.Now().Add(-5*time.Hour - 18*time.Minute)
	if err := os.Chtimes(record, old, old); err != nil {
		t.Fatalf("age the transcript: %v", err)
	}
	// A DIFFERENT live process must hold the row: this test process's own pid
	// would be judged "self" (proceeding), and the arm needs the refusal.
	pid, _ := livenessSpawnLiveChild(t)
	ticks, ok := procname.StartTicks(procname.ProcRootDefault, strconv.Itoa(pid))
	if !ok {
		t.Skipf("cannot read the probe child's start ticks")
	}
	task := &Task{
		TaskID:          taskID,
		Status:          taskStatusInProgress,
		SessionID:       taskID,
		ClaimedBy:       hostnameOrEmpty() + claimHolderHostPidSep + strconv.Itoa(pid),
		HasProcIdentity: true,
		ProcPid:         pid,
		ProcStartTicks:  mustAtoi64(t, ticks),
	}
	verdict, refusal, err := taskRunGuardRuling(task)
	if err != nil {
		t.Fatalf("taskRunGuardRuling returned an error: %v", err)
	}
	if verdict != taskRunHolderLive {
		t.Fatalf("verdict = %q, want %q — the arm needs a REFUSED row", verdict, taskRunHolderLive)
	}
	if !strings.Contains(refusal, "PROGRESS: "+TaskProgressIdle) {
		t.Fatalf("the refusal does not carry the idle axis:\n%s", refusal)
	}
	if !strings.Contains(refusal, "--takeover") {
		t.Fatalf("the refusal does not name the recovery command:\n%s", refusal)
	}
}
