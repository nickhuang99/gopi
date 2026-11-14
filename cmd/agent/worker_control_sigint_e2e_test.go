package main

// worker_control_sigint_e2e_test.go — Ctrl-C must end a control command's WAIT,
// in every disposition the command can inherit.
//
// THE DEFECT (measured 2026-10-16, task 01a14316-b7cb, operator report): on
// `gopi worker --stop --pid <n> --wait <bound>` the operator pressed Ctrl-C four
// times and NOTHING happened — no termination line, no exit; the tool built to
// stop other things could not be stopped. Root cause: the sender path
// (workerControlCommand) installed no signal handler of its own, so SIGINT was
// answered ENTIRELY by the disposition the process INHERITED:
//
//	SIG_DFL — an ordinary interactive shell. The kernel kills the command, so
//	          Ctrl-C "works" and silently bypasses every line of the stop flow
//	          (measured: exit -2 with no TERM_<pid> written at all — the
//	          operator's Ctrl-C also prevented the request from being delivered).
//	SIG_IGN — nohup / setsid / a non-interactive launcher / `&`. Ctrl-C is
//	          DISCARDED by the kernel and nothing in the process can re-enable
//	          it (measured: four Ctrl-C bytes on a pty, 8s and 45s ceilings, no
//	          exit). Registering a handler is what resets an inherited SIG_IGN,
//	          which is why "just check the mask" is not enough — the fix has to
//	          install one.
//
// This case pins the SIG_IGN half — the strictly worse one, and the only half a
// test can create deterministically: Go's os/exec has no preexec hook, but a
// child INHERITS its parent's ignored dispositions, so this test ignores SIGINT
// on ITSELF and every child it starts inherits SIG_IGN. That is the operator's
// condition, produced by the same kernel rule, with no pty and no shell.
//
// WHAT IS ASSERTED, and why each one is a criterion rather than a nicety:
//
//	1. the command ENDS, long before its bound (elapsed << bound);
//	2. it ends with wcAbortExit — NOT 0, because the requested end
//	   state was never observed, and a wrapper must be able to tell "the
//	   operator gave up" from "the stop succeeded";
//	3. the durable request is NOT withdrawn: TERM_<pid> is still standing, so
//	   the worker still leaves at its own round boundary;
//	4. the worker is still ALIVE — Ctrl-C on the sender must never reach the
//	   victim, and a repeat must not escalate to it either.
//
// RED PROOF: on the pre-fix binary arm 1 does not terminate at all (it runs to
// a bounded ceiling and is killed by this fixture), so the case FAILS there.

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// wcAbortExit is THIS FILE'S OWN spelling of the interrupted-wait exit code,
	// deliberately NOT the production constant wcAbortExit. The reason
	// is the red-proof: this file is also run against the pristine baseline, and
	// a file that referenced the production constant would fail to BUILD there —
	// which proves nothing about the command's behaviour. Spelled locally, the
	// baseline compiles and the case fails for the real reason (the command runs
	// to its ceiling). The two are kept in step by the case itself: the run must
	// return exactly this value, and a drift turns it red on the FIXED binary.
	wcAbortExit = 3
	// wcSigintBound is the bound handed to `--wait`. It is deliberately absurd
	// next to the assertion below (minutes vs ~a second): "the command obeyed
	// the operator" and "the command happened to finish before its bound"
	// must not be able to pass the same test.
	wcSigintBound = "25m"
	// wcSigintDeadline is how long the command is given, IN TOTAL, to end after
	// the signal. It is three orders of magnitude below wcSigintBound.
	wcSigintDeadline = 10 * time.Second
	// wcSigintSettle is the window between injections — enough for the abort to
	// land and print without racing it.
	wcSigintSettle = 400 * time.Millisecond
	// wcSigintPoll is the fixture's poll cadence (Rule 1).
	wcSigintPoll = 50 * time.Millisecond
)

// TestWorkerControlStop_CtrlCEndsTheWait is arm 1: ONE Ctrl-C ends the wait.
func TestWorkerControlStop_CtrlCEndsTheWait(t *testing.T) {
	f := newWorkerCtrlSigintFixture(t)
	t.Logf("fixture: SIGINT ignored in this process → every child inherits SIG_IGN (the operator's condition)")

	code, out, elapsed := f.runStopWithCtrlC(t, 1)
	if elapsed >= wcSigintDeadline {
		t.Fatalf("`%s %s` did NOT end within %s of Ctrl-C (its own bound was %s) — the tool cannot be stopped by its operator. Output:\n%s",
			CliVerbWorker, workerControlStop, wcSigintDeadline, wcSigintBound, out)
	}
	if code != wcAbortExit {
		t.Errorf("Ctrl-C must end the wait with exit %d (the requested end state was never observed — claiming 0 would be a false success), got %d after %s. Output:\n%s",
			wcAbortExit, code, elapsed.Round(time.Millisecond), out)
	}
	// The line the operator was missing. It must NAME the state, not just exit.
	if !strings.Contains(out, "SIGINT") || !strings.Contains(out, "no longer waiting") {
		t.Errorf("the abort must say which signal ended the wait and that it stopped waiting (stderr text the operator reads); got:\n%s", out)
	}
	f.assertRequestStillStanding(t)
	f.assertWorkerAlive(t)
}

// TestWorkerControlStop_RepeatedCtrlCDoesNotEscalate is arm 2: a SECOND and
// THIRD Ctrl-C re-affirm the same graceful request. Escalation in this system is
// the operator's explicit spelling (`--force` ⇒ FORCE_<pid> + SIGUSR1) — a
// repeat must not become a kill, and must not withdraw the request either.
func TestWorkerControlStop_RepeatedCtrlCDoesNotEscalate(t *testing.T) {
	f := newWorkerCtrlSigintFixture(t)

	code, out, elapsed := f.runStopWithCtrlC(t, 3)
	if elapsed >= wcSigintDeadline {
		t.Fatalf("`%s %s` did NOT end within %s of three Ctrl-C presses (bound %s). Output:\n%s",
			CliVerbWorker, workerControlStop, wcSigintDeadline, wcSigintBound, out)
	}
	if code != wcAbortExit {
		t.Errorf("repeated Ctrl-C must still end with the abort code %d, got %d. Output:\n%s", wcAbortExit, code, out)
	}
	f.assertRequestStillStanding(t)
	f.assertWorkerAlive(t)
}

// TestWorkerControlStop_NoWaitIsUnaffected is the regression arm: `--wait 0`
// returns immediately and must not be disturbed by the signal work (it never
// enters a wait, so it has nothing to interrupt).
func TestWorkerControlStop_NoWaitIsUnaffected(t *testing.T) {
	f := newWorkerCtrlSigintFixture(t)

	done := make(chan struct{})
	var code int
	var out string
	go func() {
		defer close(done)
		code, out = wdRun(t, f.bin, f.env, CliVerbWorker, workerControlStop, workerControlPIDFlag, f.pidArg(), workerControlWait, "0")
	}()
	select {
	case <-done:
	case <-time.After(wcSigintDeadline):
		t.Fatalf("`--wait 0` must return immediately; it did not within %s", wcSigintDeadline)
	}
	if code != workerControlExitOK && code != workerControlExitFailed {
		t.Errorf("`--wait 0` must return one of the two ordinary codes (%d/%d), got %d — the abort code belongs to an interrupted WAIT, and none happened here. Output:\n%s",
			workerControlExitOK, workerControlExitFailed, code, out)
	}
}

// workerCtrlSigintFixture is one sandbox profile whose stop TARGET is a plain
// live process with a seeded state record.
//
// THE TARGET IS DELIBERATELY NOT A REAL WORKER, and the reason is that this
// case must be able to tell "Ctrl-C did not withdraw the request" from "the
// worker consumed it on its way out". A real daemon consumes TERM_<pid> at its
// first round boundary and then EXITS — measured here: an idle daemon is gone
// ~1s after the request, so the request file is legitimately removed and the
// worker legitimately dies, and neither observation means what this case is
// asserting. The seeded record removes the race without weakening the
// assertion:
//
//   - the record is left WITHOUT a version, which is the "different binary"
//     shape (workerForeignDrainBinary) — so the command requests by the durable
//     FILE ONLY and never signals the target, which is exactly what lets this
//     case attribute the target's survival to the operator's Ctrl-C;
//   - the record claims the loop is open and a round in flight, so the command
//     enters the wait it is here to test instead of reporting a start-up
//     prologue;
//   - nothing consumes the request, so its survival is a fact about the
//     SENDER's behaviour — the thing under test.
//
// The same defect driven end-to-end against a REAL worker (real pty, real ^C
// bytes) is measured separately in the task's evidence; this file's job is the
// regression guard.
type workerCtrlSigintFixture struct {
	t      *testing.T
	bin    string
	env    []string
	home   string
	victim *exec.Cmd
	pid    int
}

func newWorkerCtrlSigintFixture(t *testing.T) *workerCtrlSigintFixture {
	t.Helper()
	bin := wdBin(t)
	// repo-context sandbox: the CHILD would otherwise inherit this process's cwd
	// (the live repository) and its own-worktree-release pass would enumerate the
	// developer's worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	// THE CONDITION, established on this process before any child exists: a
	// child inherits ignored signal dispositions across fork+exec, so SIGINT is
	// SIG_IGN for every command started below — the operator's shape, produced
	// by the kernel's own rule rather than by a shell.
	signalIgnoreSIGINT(t)

	// The victim is a plain sleeper, NOT a gopi spawn: the repo-context gate
	// judges only spawns of the real binary with a lifecycle verb, so this line
	// is outside its entry-point set and needs no inline escape reason at all.
	// A reason comment here is not harmless — it is an ORPHAN the gate COUNTS
	// and no recognized escape can match, which is what made the marker/escape
	// equality red (2026-10-17, this branch: 4 markers, 2 escaped sites). Note
	// the escape token is deliberately NOT spelled with its trailing colon
	// anywhere in this file: the gate counts the colon-terminated token as TEXT
	// (rcsEscapeMarker, repo_context_sandbox_guard_test.go), so prose that
	// writes it in full is itself counted as a marker. The fixture's own pin is
	// SandboxRepoContext(t) above and the env vector below is the REAL binary's
	// (f.env, built from that sandbox profile).
	victim := exec.Command("sleep", "600")
	// Pdeathsig, so the victim cannot outlive this test BINARY. It must NOT go
	// through testutil.StartMockProcessCmd: that helper also gives the child a
	// process group of its own and a cwd outside the checkout, and it reaps the
	// group at cleanup — here the fixture's subject IS its signal disposition
	// (SIGINT is SIG_IGN for children of this process, established above) and
	// the control command must find and stop exactly this pid. t.Cleanup below
	// kills and reaps it on every in-process exit; this flag covers the exit
	// where no Go code runs, so a SIGKILLed binary cannot leave a `sleep 600`
	// standing in the checkout, pinning the ground IN USE.
	victim.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := victim.Start(); err != nil {
		t.Fatalf("start the fixture's stop target: %v", err)
	}
	t.Cleanup(func() {
		_ = victim.Process.Kill()    //stylegate:ignore kills the fixture victim the test spawned
		_, _ = victim.Process.Wait() //stylegate:ignore reaps the fixture victim the test spawned
	})
	f := &workerCtrlSigintFixture{t: t, bin: bin, env: env, home: home, victim: victim, pid: victim.Process.Pid}
	f.seedStateRecord(t)
	return f
}

// seedStateRecord writes the worker state record the control command reads to
// resolve its target. It goes through the file's own encoder constants (Rule
// 1b/14) rather than a hand-typed name.
func (f *workerCtrlSigintFixture) seedStateRecord(t *testing.T) {
	t.Helper()
	dir := wdWorkerStateDir(f.home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the state dir %s: %v", dir, err)
	}
	now := time.Now().UTC()
	rec := fmt.Sprintf(
		`{"pid":%d,"started":%q,"rounds":1,"state":%q,"task_id":"fixture-round","last_round_start":%q,"claiming":true,"note":"fixture"}`,
		f.pid, now.Add(-time.Minute).Format(time.RFC3339), workerStatusStateBusy,
		now.Add(-time.Second).Format(time.RFC3339))
	path := filepath.Join(dir, workerStateFilePrefix+strconv.Itoa(f.pid))
	if err := os.WriteFile(path, []byte(rec), 0o644); err != nil {
		t.Fatalf("seed the state record %s: %v", path, err)
	}
}

// signalIgnoreSIGINT makes this test process ignore SIGINT for the duration of
// the case. Restoring SIG_DFL afterwards keeps the fixture from changing how the
// rest of the package's tests handle Ctrl-C (Rule 17's spirit: a case's mutation
// is scoped to the case).
func signalIgnoreSIGINT(t *testing.T) {
	t.Helper()
	signal.Ignore(syscall.SIGINT)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT) })
}

// pidArg is the fixture's own target: the live seeded victim.
func (f *workerCtrlSigintFixture) pidArg() string {
	return strconv.Itoa(f.pid)
}

// runStopWithCtrlC starts `gopi worker --stop --pid <daemon> --wait 25m`, sends
// SIGINT to it shots times, and reports the exit code, the captured output and
// the elapsed time measured from the FIRST shot to the exit.
//
// SIGINT is delivered with syscall.Kill, i.e. the same signal a terminal's ^C
// raises (the byte reaches the process as SIGINT through the tty discipline);
// the SIG_IGN inheritance above is what makes the delivery faithful to the
// operator's condition without a pty.
func (f *workerCtrlSigintFixture) runStopWithCtrlC(t *testing.T, shots int) (int, string, time.Duration) {
	t.Helper()
	// No inline escape reason here, and that is a statement about the GATE, not
	// a hole in this site: `worker --stop` is a READ-ONLY control face
	// (rcsReadOnlyWorkerControls — it answers a question / signals a pid and
	// resolves no repo set: the whole of cli_worker_control.go reaches
	// tools.ControlDir, tools.PidAlive and the request file, and never
	// GitSyncRepoRoot or a reclaim pass), so the gate does not judge this call.
	// An escape reason on a site the gate never counts cannot match anything and
	// its mere presence fails the marker/escape equality (measured 2026-10-17 on
	// this branch: 4 markers vs 2 escaped sites). The escape token is therefore
	// spelled WITHOUT its trailing colon everywhere in this file — the gate
	// counts the colon-terminated token as TEXT, so prose naming it in full is
	// counted as a marker. If the verb class ever changes (the gate would list
	// this function as an offender), the fix is the pin, not a reason string.
	// The child's environment is still the fixture's own sandbox vector (f.env:
	// GOPI_HOME/GOPI_SETTINGS/GOPI_MEMORY_DB pinned to t.TempDir()).
	cmd := exec.Command(f.bin, CliVerbWorker, workerControlStop, workerControlPIDFlag, f.pidArg(), workerControlWait, wcSigintBound)
	cmd.Env = f.env
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start `%s %s`: %v", CliVerbWorker, workerControlStop, err)
	}

	// The command must reach its wait before the signal means anything: it has to
	// resolve the target and deliver the request first. Waiting for the request
	// file is waiting for the fact, not for a timer.
	if !wcWaitRequestFile(t, f.home, f.pid, wcSigintDeadline) {
		_ = cmd.Process.Kill()    //stylegate:ignore kills the child process the test spawned and is tearing down
		_, _ = cmd.Process.Wait() //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
		t.Fatalf("the stop request file never appeared within %s — the command did not get as far as delivering it; output so far:\n%s",
			wcSigintDeadline, buf.String())
	}

	// The signal is delivered through the recorded IDENTITY, not the bare pid:
	// a pid is a number the kernel RECYCLES, and on 2026-10-17 a test binary's
	// pid-addressed SIGINT reached `systemd --user` and stopped the operator's
	// GNOME session (`journalctl -b | grep "Received SIGINT from PID"`).
	// testutil.KillIfIdentical re-reads pid+starttime and refuses a reused pid,
	// and it refuses a login-session-manager target outright (Rule 42).
	// The identity is captured HERE, after the request file proved the child
	// reached its wait — the child never execs, so this incarnation is the one
	// that receives every injection below.
	victim, err := testutil.ReadProcIdentity(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()    //stylegate:ignore the identity read already failed, so this Kill is a teardown of a child whose pid was never recorded; the Fatalf on the next line is the assertion and the error it names is the ReadProcIdentity one
		_, _ = cmd.Process.Wait() //stylegate:ignore reaping a just-killed child on the failure path: its exit status cannot be asserted because the assertion (ReadProcIdentity's error) already decided the test's outcome
		t.Fatalf("read the control command's identity (pid %d): %v", cmd.Process.Pid, err)
	}

	start := time.Now()
	for i := 0; i < shots; i++ {
		if err := testutil.KillIfIdentical(victim, syscall.SIGINT); err != nil {
			t.Fatalf("inject SIGINT #%d: %v", i+1, err)
		}
		time.Sleep(wcSigintSettle) //stylegate:allow-sleep the settle between two injected signals — a real child process receives them and the state must be observed
	}
	reapDone := make(chan error, 1)
	go func() { reapDone <- cmd.Wait() }()
	select {
	case err := <-reapDone:
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if !asExitError(err, &ee) {
				t.Fatalf("wait for `%s %s`: %v\n%s", CliVerbWorker, workerControlStop, err, buf.String())
			}
			code = ee.ExitCode()
		}
		return code, buf.String(), time.Since(start)
	case <-time.After(wcSigintDeadline):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-reapDone
		return -1, buf.String(), time.Since(start)
	}
}

// assertRequestStillStanding is criterion 3: Ctrl-C must not WITHDRAW the stop.
func (f *workerCtrlSigintFixture) assertRequestStillStanding(t *testing.T) {
	t.Helper()
	path := wcRequestPath(t, f.home, f.pid)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the durable stop request must SURVIVE the operator's Ctrl-C (a Ctrl-C ends the WAIT, never the request — the worker still has to leave at its own round boundary): %s is gone (%v)",
			path, err)
	}
}

// assertWorkerAlive is criterion 4: the signal went to the SENDER, and a repeat
// is a re-affirm — never an escalation onto the victim.
func (f *workerCtrlSigintFixture) assertWorkerAlive(t *testing.T) {
	t.Helper()
	if !wdProcAlive(f.pid) {
		t.Errorf("the target pid %d must still be alive: Ctrl-C on the control command ends the WAIT, it must never kill or escalate onto the worker", f.pid)
	}
}

// wcRequestPath is the durable request file for pid, resolved through the ONE
// encoder (tools.ControlRequestPath) rather than a second spelling of the name
// (Rule 1b/14).
func wcRequestPath(t *testing.T, home string, pid int) string {
	t.Helper()
	return tools.ControlRequestPath(filepath.Join(home, ".gopi", workerStateDirName), workerStopRequestPrefix, pid)
}

// wcWaitRequestFile polls for the request file's appearance.
func wcWaitRequestFile(t *testing.T, home string, pid int, budget time.Duration) bool {
	t.Helper()
	path := wcRequestPath(t, home, pid)
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(wcSigintPoll) //stylegate:allow-sleep polls for the request file the command under test writes
	}
	return false
}
