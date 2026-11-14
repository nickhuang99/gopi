// Resume-gate tests (epic task-process-identity, 2026-09-13).
//
// The gate's contract: `gopi session resume <task-id>` decides from the ROW'S PROCESS
// IDENTITY, never from the status string. These tests drive the decision
// function directly — the surrounding runResume needs an agent and a raw log,
// neither of which is part of the gate under test — and pin all six verdict
// outcomes: a live holder refuses, a dead/recycled/no-holder row is
// reclaimed, a foreign/unverifiable one refuses, and a nonexistent row does
// not block a plain session resume.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

const ()

// resumeGateSeedRow submits a row and moves it to in_progress, then returns
// its id. holder "" leaves no claim stamp (the auto-register shape).
func resumeGateSeedRow(t *testing.T, taskID, holder string) {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "resume gate fixture " + taskID,
		Payload: "resume gate fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	if holder == "" {
		return
	}
	if err := tools.TaskClaimExplicit(taskID, holder); err != nil {
		t.Fatalf("claim %s: %v", taskID, err)
	}
	if _, err := tools.TaskStart(taskID, holder); err != nil {
		t.Fatalf("start %s: %v", taskID, err)
	}
}

// TestResumeGateRefusesLiveHolder is the double-run guard: a row whose
// recorded identity matches a LIVE process must block the resume.
func TestResumeGateRefusesLiveHolder(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	resumeGateSeedRow(t, "t-gate-live", host+":"+strconv.Itoa(os.Getpid()))

	// THE LEDGER'S ANSWER IS A CLAIM (epic resume-gate-is-the-lock): it names a
	// live identity, and says nothing about whether that identity is EXECUTING
	// the run directory.
	if got := decideResumeByTaskID("t-gate-live"); got != resumeGateHolderLive {
		t.Fatalf("gate = %d, want resumeGateHolderLive for a verified-live holder (the claim the run lock arbitrates)", got)
	}
	// CRITERION ②: with NOTHING arbitrating the directory (no lock was taken),
	// the claim stands as a refusal — the override needs a measurement, and the
	// absence of one is not it.
	if got := resumeDecideWithLock("t-gate-live", false); got != resumeGateRefuseRunning {
		t.Fatalf("gate = %d, want resumeGateRefuseRunning when no run lock arbitrated the directory — admitting there would run a live row twice", got)
	}
	// …and WITH the kernel's verdict (this process holds the flock, so nothing
	// else is executing the directory) the claim is overridden.
	if got := resumeDecideWithLock("t-gate-live", true); got != resumeGateHolderLive {
		t.Fatalf("gate = %d, want resumeGateHolderLive when the run lock arbitrated the directory (pid alive but not running it)", got)
	}
}

// resumeGateDeadPid is a pid no process can own (above the kernel's pid_max
// on every Linux configuration gopi ships on), used as the "certainly dead"
// fixture — deterministic where spawning a child would race the OS's reaping.
const resumeGateDeadPid = 1 << 22

// TestResumeGateReclaimsDeadHolder is the epic's headline acceptance at the
// gate: the worker was killed, the row still says in_progress, and the resume
// PROCEEDS (reclaiming the zombie) because the machine says the holder is gone.
func TestResumeGateReclaimsDeadHolder(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	// A pid that has certainly died: use one above the kernel's pid_max — no
	// process can own it (a spawned-then-reaped child would race the OS's
	// pid recycling, which is the very thing this epic is about).
	deadPid := resumeGateDeadPid
	resumeGateSeedRow(t, "t-gate-dead", host+":"+strconv.Itoa(deadPid))

	if got := decideResumeByTaskID("t-gate-dead"); got != resumeGateProceed {
		t.Fatalf("gate = %d, want resumeGateProceed for a dead holder (the zombie-recovery path)", got)
	}
}

// TestResumeGateRefusesForeignHolder: another host owns the stamp, so this
// host cannot prove the run is over — the gate must refuse rather than risk a
// double run.
func TestResumeGateRefusesForeignHolder(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	resumeGateSeedRow(t, "t-gate-foreign", "another-host.example:4242")

	if got := decideResumeByTaskID("t-gate-foreign"); got != resumeGateRefuseUnknown {
		t.Fatalf("gate = %d, want resumeGateRefuseUnknown for a foreign holder", got)
	}
}

// TestResumeGateProceedsWithoutTaskID: a plain session resume (no --task, or
// a task id that names no row) has no holder to verify — the gate must not
// block it.
func TestResumeGateProceedsWithoutTaskID(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if got := decideResumeByTaskID(""); got != resumeGateProceed {
		t.Fatalf("gate = %d, want resumeGateProceed for an empty task id", got)
	}
	if got := decideResumeByTaskID("t-never-submitted"); got != resumeGateProceed {
		t.Fatalf("gate = %d, want resumeGateProceed for an unknown row (a session resume cannot double-run it)", got)
	}
}

// TestStatusExitCodesSplitRunningFromOccupiedGround pins the machine-readable
// contract `gopi task status` advertises: the two "do NOT resume" answers are
// different EXIT CODES, because they send an operator to different places (the
// row's holder vs the row's directory). Both are distinct from 0-resumable and
// from 3-unverifiable.
func TestStatusExitCodesSplitRunningFromOccupiedGround(t *testing.T) {
	if taskStatusExitRunning == taskStatusExitGroundInUse {
		t.Fatalf("running and ground-in-use share exit code %d — a caller cannot tell which question was answered", taskStatusExitRunning)
	}
	if taskStatusExitGroundInUse == taskStatusExitUnknown {
		t.Fatalf("ground-in-use and unverifiable share exit code %d — 'the directory is occupied' must not read as 'I could not tell'", taskStatusExitUnknown)
	}
	if taskStatusExitGroundInUse == 0 {
		t.Fatalf("ground-in-use must not be 0: 0 means a resume may proceed")
	}
}

// TestResumeTargetAsTaskIDResolvesTheSessionLog: the deliverable's "no
// session-id hunting" half — `gopi session resume <task-id>` resolves the row's own
// session checkpoint. A row whose session has no recorded raw log resolves to
// an empty path WITH ok=true (the row was found and its absence is definite),
// which is what lets runResume report the recovery recipe instead of falling
// through to a confusing "not a path or session id" error.
func TestResumeTargetAsTaskIDResolvesTheSessionLog(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	resumeGateSeedRow(t, "t-gate-target", "")

	raw, row, ok := resumeTaskRawLog("t-gate-target")
	if !ok {
		t.Fatalf("a submitted task id must be recognized as a resume target")
	}
	if row == nil || row.TaskID != "t-gate-target" {
		t.Fatalf("the resolved row must carry the task id, got %+v", row)
	}
	// This fixture row has no session, so there is no checkpoint: the
	// resolution reports that absence (empty) rather than inventing a path.
	if raw != "" {
		t.Fatalf("raw log = %q, want empty for a row with no session", raw)
	}
	// And an argument that names no task row is NOT a task target.
	if _, _, ok := resumeTaskRawLog("not-a-task-id-at-all"); ok {
		t.Fatalf("an unknown argument must not be treated as a task target")
	}
}

// resumeGateMoveTo walks a freshly-submitted row to the status under test
// THROUGH the state machine (pending reaches a terminal status only via
// in_progress — taskAllowedTransitions), so a fixture can never stand for a
// ledger state the product could not produce.
func resumeGateMoveTo(t *testing.T, taskID, status string) {
	t.Helper()
	if status == tools.TaskStatusPending {
		return
	}
	if _, err := tools.TaskUpdateReason(taskID, tools.TaskStatusInProgress, "gate fixture", ""); err != nil {
		t.Fatalf("move %s to in_progress: %v", taskID, err)
	}
	if status == tools.TaskStatusInProgress {
		return
	}
	if _, err := tools.TaskUpdateReason(taskID, status, "gate fixture", ""); err != nil {
		t.Fatalf("move %s to %s: %v", taskID, status, err)
	}
}

// TestResumeGateActsOnTheStatusAxis is the SECOND fact the gate must read, and
// the arm it was missing while its usage text already claimed it: a free holder
// says nobody is running the row, NOT that the work is unfinished.
//
// THREE CASES, and the middle one is the ruling (2026-10-29):
//   - completed → REFUSE with the status code (finished work; the remedy is a
//     COPY, `gopi task clone`, and that is what the refusal names);
//   - failed    → PROCEED (a failure judges one ATTEMPT — level 1 is to resume
//     it; refusing here was the pre-ruling posture and it stranded every
//     recoverable failure);
//   - cancelled → PROCEED (stopped externally; the caveat is printed by
//     `task status`, not enforced as a refusal here).
//
// The rows all carry NO holder, so the process axis is constant and the status
// is the only variable — a change in any outcome below is a change to THIS arm.
func TestResumeGateActsOnTheStatusAxis(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   int
	}{{
		status: tools.TaskStatusCompleted,
		want:   resumeGateRefuseStatus,
	}, {
		status: tools.TaskStatusFailed,
		want:   resumeGateProceed,
	}, {
		status: tools.TaskStatusCancelled,
		want:   resumeGateProceed,
	}} {
		t.Run(tc.status, func(t *testing.T) {
			t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
			taskID := "t-gate-status-" + tc.status
			resumeGateSeedRow(t, taskID, "")
			resumeGateMoveTo(t, taskID, tc.status)
			if got := decideResumeByTaskID(taskID); got != tc.want {
				t.Fatalf("gate(%s) = %d, want %d", tc.status, got, tc.want)
			}
		})
	}
}

// TestResumeGateStatusRefusalNamesTheCopy pins the SENTENCE, not just the code:
// a refusal that does not name the remedy is half a refusal (Rule 15), and the
// remedy for finished work is a named command the operator can paste.
func TestResumeGateStatusRefusalNamesTheCopy(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "t-gate-status-word"
	resumeGateSeedRow(t, taskID, "")
	resumeGateMoveTo(t, taskID, tools.TaskStatusCompleted)
	if got := resumeStatusWord(taskID); got != tools.TaskStatusCompleted {
		t.Fatalf("resumeStatusWord = %q, want %q — the refusal sentence must name the REAL status", got, tools.TaskStatusCompleted)
	}
	// An unreadable row must not print an empty word into the sentence.
	if got := resumeStatusWord("t-never-submitted"); got == "" {
		t.Fatalf("resumeStatusWord returned an empty word — the refusal would read \"is FINISHED ()\"")
	}
}
