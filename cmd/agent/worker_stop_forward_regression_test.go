// worker_stop_forward_regression_test.go — the regression guard for the
// mid-round stop hang (devdoc/worker-sigterm-futex-hang.md, measured
// 2026-09-17).
//
// THE DEFECT: the graceful-stop contract says a bare signal is forwarded to the
// in-flight run child (cli_tasks_worker_stop.go, contract item ②), but the only
// forward lived in awaitWorkerStopChild — which the loop reaches AFTER the round
// returns. A round does not return by itself: runTaskRunOnce blocks in
// cmd.Wait() on that very child. So a SIGTERM mid-round set the flag, printed
// "terminated received — draining", and parked in futex_wait_queue_me forever:
// the child was never told, the round never ended, and the operator's only exit
// was SIGKILL (which takes the terminal writeback and the own-worktree release
// with it — the exact loss the graceful stop exists to prevent).
//
// MEASURED ON THE PRE-FIX BINARY (real worker, real run child, /proc evidence):
//
//	worker=1668184 wrapper=1669125 agent=1671426 (sleep 600)
//	### A. SIGTERM the WORKER only
//	RED: worker STILL ALIVE 30s after SIGTERM — wchan=futex_wait_queue_me state=S
//	### B. now manually deliver the stop DOWN the chain
//	WORKER EXITED 6851 ms after the CHILD was signalled (vs never, before)
//
// These tests pin the two halves of the fix: a BARE signal reaches the child
// while the round is in flight, and a DRAIN still does not.

package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
)

// workerStopForwardHoldSeconds is how long the synthetic in-flight child stays
// alive unless it is signalled. It must be comfortably longer than the bound the
// assertion waits, so "the wait returned" can only mean the signal arrived —
// never that the child happened to exit on its own.
const workerStopForwardHoldSeconds = 60

// workerStopForwardBound is the assertion's deadline: the forwarder settles
// workerStopDrainSettle (2s) before forwarding, so this clears that by a wide
// margin while staying far inside the child's own hold.
const workerStopForwardBound = 20 * time.Second

// startSyntheticRunChild starts a real child process in its OWN group (the
// shape runTaskRunOnce registers: Group == PID, so a group-directed signal
// takes the child's grandchildren along) and registers it as the in-flight run
// child. Returns the command and the deregistration.
func startSyntheticRunChild(t *testing.T) (*exec.Cmd, func()) {
	t.Helper()
	cmd := exec.Command("sleep", strconv.Itoa(workerStopForwardHoldSeconds))
	// Setpgid is the registered shape (Group == PID); Pdeathsig is the lifetime arm
	// so the synthetic run child cannot outlive a killed test binary.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start synthetic run child: %v", err)
	}
	deregister := registerPassChild(passChild{
		Kind:  passChildKindRun,
		Label: "synthetic run child",
		PID:   cmd.Process.Pid,
		Group: cmd.Process.Pid,
	})
	t.Cleanup(func() {
		deregister()
		_ = cmd.Process.Kill()    //stylegate:ignore test teardown of the probe's own synthetic child (the assertions own the liveness checks)
		_, _ = cmd.Process.Wait() //stylegate:ignore the exit status is not the subject; the assertions read the child's liveness
	})
	return cmd, deregister
}

// TestWorkerBareSignalReachesChildDuringRound is the RED-proof: on the pre-fix
// shape the wait below never returns (the child was never signalled), which is
// exactly the field hang.
func TestWorkerBareSignalReachesChildDuringRound(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	// The FOREGROUND face: an inert control context, i.e. no durable request
	// file anywhere — the shape of a bare `timeout N gopi worker`.
	workerCtx = workerDaemonCtx{}
	cmd, _ := startSyntheticRunChild(t)

	uninstall := installWorkerStopSignals()
	defer uninstall()
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()

	// The round "starts": the signal arrives while this process is blocked on
	// the child, exactly like runTaskRunOnce's cmd.Wait().
	go func() {
		time.Sleep(200 * time.Millisecond) //stylegate:allow-sleep start-up handshake: the signal must land while the round is blocked on the child, or the assertion proves nothing about the forward path
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Errorf("signal self: %v", err)
		}
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// The child exited because it was signalled — the forwarder ran. The
		// wait returning at all is the assertion; it cannot return on its own
		// inside the bound (the child holds for workerStopForwardHoldSeconds).
	case <-time.After(workerStopForwardBound):
		t.Fatalf("the in-flight run child was never signalled: the round is still "+
			"waiting %s after a bare SIGTERM (pre-fix: parked in futex_wait_queue_me "+
			"until SIGKILL). The stop never reached the child.", workerStopForwardBound)
	}
	if !workerStopRequested() {
		t.Error("the signal recorded no stop request")
	}
	if workerStopEscalated() {
		t.Error("a bare SIGTERM escalated the stop — escalation is the force spelling only")
	}
}

// TestWorkerDrainStillLetsTheRoundFinish is the guarantee half: a DURABLE drain
// (the operator's --stop/--restart, which wrote the request file first) must NOT
// forward anything — that request means "let the in-flight round finish", and
// signalling the child would end it as a failure (measured 2026-09-13).
func TestWorkerDrainStillLetsTheRoundFinish(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	workerCtx = drainTestCtx(t, dir)
	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	cmd, _ := startSyntheticRunChild(t)

	uninstall := installWorkerStopSignals()
	defer uninstall()
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()

	// The nudge a bare signal would send — but the request file is standing, so
	// the forwarder must read it as a DRAIN and forward nothing.
	notifyWorkerStopNudge()
	done := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(done) }() //stylegate:ignore the wait error IS the signal under test — the done channel is what the assertions read
	select {
	case <-done:
		t.Fatal("a DURABLE drain killed the in-flight round — the drain guarantee is broken")
	case <-time.After(workerStopForwardBound):
		// Still running: correct. The drain waits for the round.
	}
	if !workerStopRequested() {
		t.Error("the durable request was not consumed into the stop flag")
	}
	if !workerDrainRequestedByFile() {
		t.Error("the durable request was not recognised as a DRAIN — a later bare-signal " +
			"forwarder could therefore signal a round the operator asked to finish")
	}
	if workerStopEscalated() {
		t.Error("a durable drain escalated the stop")
	}
	// The child is still alive, i.e. untouched by the drain.
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("the in-flight child was signalled by a drain: %v", err)
	}
}

// TestWorkerStopReachesChildWhoseGroupWasAssumed is the second half's RED-proof
// (measured 2026-09-17): the registry records Group = the child's pid as an
// ASSUMPTION ("== its pid whenever the caller set Setpgid"), but the task-run
// wrapper does NOT set Setpgid — the child's real process group is the WORKER's.
// The old forward built kill(-Group), addressed a group that does not exist, and
// swallowed ESRCH as benign: the stop reported success while delivering NOTHING.
//
// This test builds exactly that geometry (a child spawned WITHOUT Setpgid,
// registered with Group = its pid) and fails if the stop does not reach it.
func TestWorkerStopReachesChildWhoseGroupWasAssumed(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	// No Setpgid — the production shape. The child inherits OUR process group,
	// so its pgid is NOT its pid, and kill(-childPID) addresses nothing.
	cmd := exec.Command("sleep", strconv.Itoa(workerStopForwardHoldSeconds))
	// NO Setpgid here — inheriting OUR group is the geometry under test — but
	// Pdeathsig still binds it to this binary's lifetime.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	deregister := registerPassChild(passChild{
		Kind:  passChildKindRun,
		Label: "assumed-group run child",
		PID:   cmd.Process.Pid,
		Group: cmd.Process.Pid, // the assumption the registry documents
	})
	defer func() {
		deregister()
		_ = cmd.Process.Kill()    //stylegate:ignore test teardown of the probe's own synthetic child (the assertions own the liveness checks)
		_, _ = cmd.Process.Wait() //stylegate:ignore the exit status is not the subject; the assertions read the child's liveness
	}()

	done := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(done) }() //stylegate:ignore the wait error IS the signal under test — the done channel is what the assertions read
	forwardWorkerStopToChild(false)
	select {
	case <-done:
	case <-time.After(workerStopForwardBound):
		t.Fatalf("the run child (pid %d) never received the stop: its group was "+
			"ASSUMED to be its pid, so kill(-pid) addressed no process and the ESRCH "+
			"was swallowed. The child survived a stop that reported success.",
			cmd.Process.Pid)
	}
}

// TestWorkerStopReachesGrandchildren pins the other half of the same fix: a run
// child's OWN descendants (the agent grandchild, the bash tool it spawned) must
// be signalled too, or the child's close-out still waits on them.
func TestWorkerStopReachesGrandchildren(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	// The child is a shell that spawns its own long-lived grandchild (the bash
	// tool's shape). No Setpgid anywhere: the production geometry.
	//fixture-spawn-ok: the test asserts the stop REACHES A GRANDCHILD through the unmodified production geometry (no Setpgid anywhere), so the shell must fork and its group must stay the launcher's; a reaping helper would change exactly the geometry under test
	cmd := exec.Command("bash", "-c", "sleep "+strconv.Itoa(workerStopForwardHoldSeconds)+" & wait")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	deregister := registerPassChild(passChild{
		Kind:  passChildKindRun,
		Label: "run child with a grandchild",
		PID:   cmd.Process.Pid,
		Group: cmd.Process.Pid,
	})
	defer func() {
		deregister()
		_ = cmd.Process.Kill()    //stylegate:ignore test teardown of the probe's own synthetic child (the assertions own the liveness checks)
		_, _ = cmd.Process.Wait() //stylegate:ignore the exit status is not the subject; the assertions read the child's liveness
	}()

	// Wait for the grandchild to exist.
	var grand int
	for i := 0; i < 100; i++ {
		if kids := descendantPIDs(cmd.Process.Pid); len(kids) > 0 {
			grand = kids[0]
			break
		}
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep bounded poll of the real process table, capped by workerStopForwardBound
	}
	if grand == 0 {
		t.Skip("no grandchild appeared to observe — the shell did not fork in time")
	}
	forwardWorkerStopToChild(false)
	deadline := time.Now().Add(workerStopForwardBound)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(grand, syscall.Signal(0)); err != nil {
			return // the grandchild is gone: the stop reached it
		}
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep bounded poll of the real process table, capped by workerStopForwardBound
	}
	t.Errorf("the run child's grandchild (pid %d) was never signalled — a stop that "+
		"reaches only the direct child leaves its close-out waiting on the grandchild", grand)
}
