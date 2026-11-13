package procstat

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const (
	// procstatChildSleepEnv marks the RE-EXECUTED child image: when it is set
	// the helper below sleeps instead of running the suite again. Re-executing
	// this test binary (os.Args[0]) keeps the fixture free of an external
	// program (Rule 35: no new dependency for a test).
	procstatChildSleepEnv    = "GOPI_PROCSTAT_TEST_CHILD_SLEEP"
	procstatChildSleepMarker = "1"
	// procstatChildLifetime is how long the child sleeps if nothing kills it —
	// far longer than any path in this file waits, so a fixture that fails to
	// SIGKILL surfaces as an assertion, never as a hung test.
	procstatChildLifetime = 60 * time.Second
	// procstatStateDeadline bounds the "wait until the kernel reports state X"
	// polls. Rule 27: a bounded wait on the kernel's own bookkeeping is not
	// generated load.
	procstatStateDeadline = 10 * time.Second
	procstatStatePollGap  = time.Millisecond
	// procstatLiveChildState is the state a freshly exec'd, idle child shows.
	procstatLiveChildState = "S"
)

// TestProcstatZombieHelper is not a test of its own — it is the child image
// TestProcstatZombie re-executes. An ordinary suite run never sets the marker
// env var, so it returns immediately and costs nothing.
func TestProcstatZombieHelper(t *testing.T) {
	if os.Getenv(procstatChildSleepEnv) != procstatChildSleepMarker {
		return
	}
	time.Sleep(procstatChildLifetime) //stylegate:allow-sleep the sleeping child IS the fixture: TestProcstatZombie SIGKILLs it and polls the kernel's state letter, and nothing in this file waits on the sleep itself
}

// fixtureStateLetter reads /proc/<pid>/stat INDEPENDENTLY of the production
// parser (a fixture that asked the code under test to prove its own premise
// would be circular). The state is the field after the LAST ')' because comm
// may contain spaces and parentheses.
func fixtureStateLetter(t *testing.T, pid int) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(procRootName, strconv.Itoa(pid), procStatName))
	if err != nil {
		return "", false
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || i+2 >= len(b) {
		return "", false
	}
	return string(b[i+2 : i+3]), true
}

// waitForState polls until the kernel reports one of want (returning the
// observed state) or the deadline expires (returning the last observation, so
// the failure message can name what was actually measured).
func waitForState(t *testing.T, pid int, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(procstatStateDeadline)
	var last string
	for {
		state, ok := fixtureStateLetter(t, pid)
		switch {
		case ok:
			last = state
			for _, w := range want {
				if state == w {
					return state
				}
			}
		case last == "":
			last = "<unreadable>"
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d never reached state %v within %s (last observed %q) — "+
				"the fixture could not make its premise measurable, so no verdict about "+
				"the probe may be drawn from this run", pid, want, procstatStateDeadline, last)
		}
		time.Sleep(procstatStatePollGap) //stylegate:allow-sleep bounded poll gap: procstatStateDeadline owns the outcome and every iteration re-reads the kernel's state letter rather than assuming it
	}
}

// TestProcstatZombie is the regression guard for the missing state clause: a
// terminated-but-unreaped process (state Z) still answers signal 0, so a probe
// that stops at kill(pid, 0) reports VerdictAlive for a process that can never
// run again. Measured shape: the child is SIGKILLed and deliberately NOT
// waited, so the kernel keeps its /proc entry as a zombie for as long as this
// test needs it; the deferred Wait reaps it, leaving no zombie behind.
func TestProcstatZombie(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcstatZombieHelper$")
	cmd.Env = append(os.Environ(), procstatChildSleepEnv+"="+procstatChildSleepMarker)
	// The child re-runs a slice of this package's test binary; its chatter is
	// discarded so a fixture failure can never be mistaken for a suite failure.
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the child fixture: %v", err)
	}
	pid := cmd.Process.Pid
	// Reap whatever the outcome — a killed child left unreaped would keep its
	// zombie entry in this suite's process table (Rule 30's spirit: a run
	// leaves no litter). The error IS expected here: the child dies of SIGKILL.
	//stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	defer func() { _ = cmd.Wait() }()

	// PRECONDITION (measurable premise, not an assumption): the child is a
	// live, schedulable process before we kill it.
	if state := waitForState(t, pid, procstatLiveChildState); state != procstatLiveChildState {
		t.Fatalf("child %d measured state %q before the kill", pid, state)
	}
	if got := Alive(pid); got != VerdictAlive {
		t.Fatalf("Alive(%d) = %q while the child was still running, want %q — "+
			"a probe that reports a live process as gone is the worse failure",
			pid, got, VerdictAlive)
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL to child %d failed: %v", pid, err)
	}

	// THE PREMISE, proved rather than asserted: nothing has waited on the
	// child, so /proc/<pid>/stat MUST still exist and read state Z. This is
	// also the fact the old probe got wrong — signal 0 still succeeds here.
	if state := waitForState(t, pid, procZombieState); state != procZombieState {
		t.Fatalf("KILLED child %d measured state %q, want %q", pid, state, procZombieState)
	}
	if got := Alive(pid); got != VerdictGone {
		t.Fatalf("Alive(%d) = %q for a terminated-but-unreaped child (state %q), want %q — "+
			"a zombie cannot run, so reporting it alive makes every caller treat a dead "+
			"process as a live one", pid, got, procZombieState, VerdictGone)
	}
}

// TestProcstatZombieStateTable unit-tests the decision the probe applies to the
// state letter, so a table that loses a case fails here instead of only in a
// process-timing test.
func TestProcstatZombieStateTable(t *testing.T) {
	for _, tc := range []struct {
		state    string
		terminal bool
		why      string
	}{
		{procZombieState, true, "zombie: terminated, waiting to be reaped — not schedulable"},
		{procDeadState, true, "the measured transient death state a reaped pid passes through"},
		{"R", false, "running"},
		{"S", false, "sleeping"},
		{"D", false, "uninterruptible sleep — alive, merely blocked"},
		{"T", false, "stopped — alive, can still be resumed"},
		{"", false, "an unreadable/malformed stat keeps the signal-0 verdict"},
	} {
		if got := stateIsTerminal(tc.state); got != tc.terminal {
			t.Errorf("stateIsTerminal(%q) = %v, want %v (%s)", tc.state, got, tc.terminal, tc.why)
		}
	}
}

// TestProcstatKillErrTable pins the FAILURE side of the probe: the mapping from
// kill(pid, 0)'s error onto a Verdict. The real probe cannot manufacture an
// EPERM on demand, so this table is where all three branches are exercised —
// including a wrapped error, because every caller downstream distinguishes them
// with errors.Is.
func TestProcstatKillErrTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want Verdict
		why  string
	}{
		{"esrch", syscall.ESRCH, VerdictGone,
			"the kernel knows no such process — the ONLY measurement of death"},
		{"esrch-wrapped", fmt.Errorf("probe: %w", syscall.ESRCH), VerdictGone,
			"a wrapped ESRCH is still ESRCH: callers use errors.Is"},
		{"eperm", syscall.EPERM, VerdictAlive,
			"the process EXISTS and we may not signal it — a measurement of EXISTENCE"},
		{"eperm-wrapped", fmt.Errorf("probe: %w", syscall.EPERM), VerdictAlive,
			"same, through the wrap"},
		{"enosys", syscall.ENOSYS, VerdictUnknown,
			"the probe could not answer — never a death proof"},
		{"einval", syscall.EINVAL, VerdictUnknown,
			"a bad pid shape: not an existence statement either way"},
		{"other", errors.New("boom"), VerdictUnknown,
			"an unrecognized failure must not be upgraded into a deletion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verdictForKillErr(tc.err); got != tc.want {
				t.Errorf("verdictForKillErr(%v) = %q, want %q (%s)", tc.err, got, tc.want, tc.why)
			}
		})
	}
}

// TestProcstatEPERMIsAlive is the REAL EPERM probe — the epic's red→green
// criterion, and the one assertion a table test cannot make: pid 1 is init, it
// is unambiguously alive, and a non-root caller cannot signal it, so kill(1, 0)
// returns EPERM. The premise is MEASURED first (a root run would get nil
// instead, and this test then says so rather than passing for the wrong
// reason), and only then is the verdict asserted.
func TestProcstatEPERMIsAlive(t *testing.T) {
	const initPid = 1
	err := syscall.Kill(initPid, 0)
	if !errors.Is(err, syscall.EPERM) {
		t.Skipf("kill(%d, 0) = %v, not EPERM (running as root? then there is no EPERM "+
			"case to measure on this host) — the premise of this test is absent, so "+
			"its verdict would prove nothing", initPid, err)
	}
	// The premise, restated as a failing assertion on the OLD semantics: this
	// is exactly what the pre-fix code answered, and it is wrong.
	if got := Alive(initPid); got != VerdictAlive {
		t.Fatalf("Alive(%d) = %q while kill(%d,0) answered EPERM — EPERM means the "+
			"process EXISTS and is not ours, so this must be %q; reporting init as gone "+
			"is the defect epic 01a17e87 removes",
			initPid, got, initPid, VerdictAlive)
	}
}
