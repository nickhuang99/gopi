package main

// THE RESUME GATE ON A HANDED-ON ROW (epic: task status cwd-row evidence,
// 2026-11-01).
//
// THE BUG. `gopi task status` decided `verdict: running` from pid liveness plus
// the recorded incarnation (start ticks + cmdline) — a triple that still AGREES
// after the worker has moved on to another row. Measured on this host: row
// 01a19075-80b5 recorded pid 1604177, and /proc/1604177/cwd was row
// 01a1954e-17a6's session container. Because the verdict came back `running`,
// `resumable:` read `no` and `gopi session resume` refused with
// `resumeGateRefuseRunning` — the row's recovery path was blocked by a process
// that had not touched it again.
//
// WHAT THIS FILE PINS. The gate is DOWNSTREAM of the ladder: it reads
// tools.TaskProcessStatusFor and refuses on `running`, so the fix cannot live in
// cli_resume.go — it lives one layer down, in the ladder's cwd-row rung, and
// this test is the evidence that the gate INHERITS it. A test asserting the
// ladder alone would prove the predicate and say nothing about the refusal the
// operator actually meets (Rule 31: exercise the surface).
//
// EVERY FACT HERE IS THE KERNEL'S, NOT A FIXTURE. The probe processes are
// really spawned with `cmd.Dir` set to the directory under test, so
// /proc/<pid>/cwd is measured by the same reader production uses — this package
// needs no seam into internal/tools' identity root, which is deliberately NOT
// environment-steered (an env knob there would let a caller make every live
// runner read "gone": a fail-OPEN hazard on the destructive reclaim path).
//
// THE THREE ARMS, and what each is FOR:
//
//	(a) the pid stands in its OWN row's container  → running, gate REFUSES
//	    (the control: admitting this would run a live row twice)
//	(b) the SAME ledger row is stamped with a pid standing in ANOTHER row's
//	    container → not running, resumable, gate PROCEEDS
//	    (the measured defect: identity triple agrees, cwd names row B)
//	(c) back to a pid in its OWN container → running again
//	    (proves arm (b) followed the cwd and not a sticky state)

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/tools"
)

const (
	// cwdGateRowID is the row whose recovery path the bug blocked, and
	// cwdGateOtherRowID is the row its pid had moved on to. Both are the REAL
	// ids from the measured incident, and both must be canonical uuids: the rung
	// recognises a row by its MINTED container-name predicate, so a made-up id
	// would make every arm pass for the wrong reason.
	cwdGateRowID      = "01a19075-80b5-7a85-b0df-02a55f1c8b16"
	cwdGateOtherRowID = "01a1954e-17a6-7954-ad7a-99cad43f41cb"
	// cwdGateSleepArg is the probe child's argv[1] — ONE definition read by the
	// spawned command AND the recorded column, so the identity triple cannot
	// drift and turn an arm into a cmdline mismatch wearing a cwd finding's
	// clothes.
	cwdGateSleepArg = "60"
)

// cwdGateFixture is the ledger + the two session containers + the db path.
type cwdGateFixture struct {
	dbPath   string
	ownDir   string // <sessions>/<cwdGateRowID> — this row's container
	otherDir string // <sessions>/<cwdGateOtherRowID> — the row the pid serves now
}

// cwdGateSeed builds it: one in_progress row and the two container directories.
func cwdGateSeed(t *testing.T) *cwdGateFixture {
	t.Helper()
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(config.GopiDirEnv, gopiHome)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	sessions := filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName)
	f := &cwdGateFixture{
		dbPath:   dbPath,
		ownDir:   filepath.Join(sessions, cwdGateRowID),
		otherDir: filepath.Join(sessions, cwdGateOtherRowID),
	}
	for _, dir := range []string{f.ownDir, f.otherDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "resume gate: handed-on holder fixture",
		Payload: "cwd-row resume gate fixture",
		TaskID:  cwdGateRowID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	return f
}

// cwdGateProbe starts a REAL child standing in dir and returns its pid and the
// kernel's start ticks for it.
func cwdGateProbe(t *testing.T, dir string) (pid int, start int64) {
	t.Helper()
	cmd := exec.Command("sleep", cwdGateSleepArg)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a probe process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore kills the child this test spawned and is tearing down
		_ = cmd.Wait()         //stylegate:ignore reaps the child this test spawned; its status is not the assertion
	})
	pid = cmd.Process.Pid
	rawStart, ok := procname.StartTicks(procname.DefaultProcRoot, strconv.Itoa(pid))
	if !ok {
		t.Skipf("no readable start ticks for pid %d", pid)
	}
	start, err := strconv.ParseInt(rawStart, 10, 64)
	if err != nil {
		t.Skipf("unparsable start ticks %q for pid %d: %v", rawStart, pid, err)
	}
	return pid, start
}

// stampIdentity writes the row's holder identity — the ledger half of a handoff.
//
// IT BYPASSES the claim guard ON PURPOSE, and the reason is the incident this
// test pins: the guard refuses to claim a row whose recorded holder is LIVE
// (correctly — it cannot tell a handoff from a concurrent run), while the
// MEASURED ledger state was a row stamped with a process that had already moved
// on to another row. That state is what `gopi task status` has to judge, so the
// fixture writes it directly instead of re-enacting a claim the guard is right
// to refuse.
func (f *cwdGateFixture) stampIdentity(t *testing.T, pid int, start int64) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(f.dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET status = 'in_progress', claimed_by = ?, proc_pid = ?, proc_start_ticks = ?, proc_cmd = ? WHERE task_id = ?`,
		host+":"+strconv.Itoa(pid), pid, start, "sleep "+cwdGateSleepArg, cwdGateRowID); err != nil {
		t.Fatalf("stamp identity of pid %d: %v", pid, err)
	}
}

// TestResumeGateInheritsTheCwdRowEvidence is the epic's acceptance arm at the
// surface the operator actually meets.
func TestResumeGateInheritsTheCwdRowEvidence(t *testing.T) {
	f := cwdGateSeed(t)

	// (a) CONTROL — the holder stands in its OWN row's container.
	pidA, startA := cwdGateProbe(t, f.ownDir)
	f.stampIdentity(t, pidA, startA)
	st, err := tools.TaskProcessStatusFor(cwdGateRowID)
	if err != nil {
		t.Fatalf("status (control): %v", err)
	}
	if st.Verdict != tools.TaskProcessStateRunning {
		t.Fatalf("control: verdict = %q (note %q), want %q — a process standing in its own row's directory is running",
			st.Verdict, st.Note, tools.TaskProcessStateRunning)
	}
	// The ledger says the holder is LIVE; the run lock — NOT taken here — is
	// what decides whether that claim is actionable, so with nothing arbitrating
	// it the refusal stands (criterion ②: admitting a live row would run it
	// twice).
	if got := decideResumeByTaskID(cwdGateRowID); got != resumeGateHolderLive {
		t.Fatalf("control: gate = %d, want resumeGateHolderLive — the ledger's claim is the run lock's to arbitrate", got)
	}
	if got := resumeDecideWithLock(cwdGateRowID, false); got != resumeGateRefuseRunning {
		t.Fatalf("control: gate = %d, want resumeGateRefuseRunning with no quorum from the run lock", got)
	}

	// (b) THE HANDOFF — the row is stamped with a process standing in ANOTHER
	// row's container. The identity triple still agrees (pid + start + cmdline
	// are all this process's); only WHICH ROW the cwd names differs.
	pidB, startB := cwdGateProbe(t, f.otherDir)
	f.stampIdentity(t, pidB, startB)
	st, err = tools.TaskProcessStatusFor(cwdGateRowID)
	if err != nil {
		t.Fatalf("status (handoff): %v", err)
	}
	if st.Verdict == tools.TaskProcessStateRunning {
		t.Fatalf("handoff: verdict = %q (note %q) — a pid whose cwd names ANOTHER row must not read running",
			st.Verdict, st.Note)
	}
	if !st.Resumable {
		t.Fatalf("handoff: resumable = false — the measured bug is exactly that the recovery path stayed closed")
	}
	if got := decideResumeByTaskID(cwdGateRowID); got != resumeGateProceed {
		t.Fatalf("handoff: gate = %d, want resumeGateProceed (the recorded pid stands in row %s)",
			got, cwdGateOtherRowID)
	}

	// (c) BACK AGAIN — a holder standing in THIS row's container reads running,
	// so arm (b) tracked the cwd rather than a state that stuck.
	pidC, startC := cwdGateProbe(t, f.ownDir)
	f.stampIdentity(t, pidC, startC)
	st, err = tools.TaskProcessStatusFor(cwdGateRowID)
	if err != nil {
		t.Fatalf("status (return): %v", err)
	}
	if st.Verdict != tools.TaskProcessStateRunning {
		t.Fatalf("return: verdict = %q (note %q), want %q", st.Verdict, st.Note, tools.TaskProcessStateRunning)
	}
	if got := resumeDecideWithLock(cwdGateRowID, false); got != resumeGateRefuseRunning {
		t.Fatalf("return: gate = %d, want resumeGateRefuseRunning", got)
	}
}
