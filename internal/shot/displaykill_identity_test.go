package shot

// displaykill_identity_test.go — the regression guard for the PRODUCTION half of
// the recycled-pid fix: `FilmDisplay.Close` used to signal `d.PID` through
// `os.FindProcess`, and a `FilmDisplay` outlives its spawn (a caller closes a
// display it allocated minutes earlier), so the pid could have been recycled in
// between.
//
// The measured incident (2026-10-17): five SIGINTs from `agent.test` reached
// `systemd --user`, each one stopping the operator's whole user session
// (`journalctl -b | grep 'Received SIG'`). The test-side guard lives in
// cmd/agent/ground_kill_identity_test.go; this file guards the production path,
// which is the one that can kill a stranger OUTSIDE a test process.
//
// Each arm is aimed at a fixture THIS test spawned, so no arm can reach a
// stranger — and both directions are measured: the refusal must leave the
// process ALIVE (read from /proc), and the accepted delivery must leave it GONE.

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

const displayKillExitPollStep = 20 * time.Millisecond

const displayKillExitPollBound = 5 * time.Second

// startDisplayFixture starts a process that will not exit on its own, and
// returns its pid with the incarnation read the same way the allocator reads it.
func startDisplayFixture(t *testing.T) (int, string) {
	t.Helper()
	cmd := exec.Command("/bin/sleep", "300")
	// The fixture must not outlive its test binary (Rule 30's leak shape, and the
	// gate's criterion): Pdeathsig is the only cover a SIGKILLed `go test` cannot
	// skip, and the cwd is NOT this test's subject (the incarnation in /proc is),
	// so the child is parked outside every checkout — a survivor standing in one
	// makes that ground read IN USE forever.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fixture: %v", err)
	}
	pid := cmd.Process.Pid
	ticks := displayStartTicks(pid)
	if ticks == "" {
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		t.Skipf("the host does not expose /proc/%d/stat starttime — the incarnation check has nothing to compare", pid)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error
		_ = cmd.Wait()                         //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	})
	return pid, ticks
}

// TestDisplayKillRefusesARecycledPid is the red path: a record whose starttime
// does not match the live process must be REFUSED, and the live process must
// measurably survive the attempt. The survival reading is the claim — an error
// return alone would not distinguish "refused" from "sent and failed".
func TestDisplayKillRefusesARecycledPid(t *testing.T) {
	pid, ticks := startDisplayFixture(t)

	// A different incarnation of the same number: exactly the state at close
	// time when the kernel has handed the number to somebody else.
	stale, err := strconv.Atoi(ticks)
	if err != nil {
		t.Fatalf("starttime %q is not a number: %v", ticks, err)
	}
	staleTicks := strconv.Itoa(stale + 1)

	err = displayKillIncarnation(pid, staleTicks, syscall.SIGTERM)
	if err == nil {
		t.Fatalf("displayKillIncarnation delivered to pid %d whose incarnation changed (live starttime %s, record says %s) — a recycled pid must be refused", pid, ticks, staleTicks)
	}
	if !strings.Contains(err.Error(), "incarnation changed") {
		t.Errorf("the refusal must name the reason; got: %v", err)
	}
	if _, ok := os.Stat("/proc/" + strconv.Itoa(pid)); ok != nil {
		t.Fatalf("the refused signal reached the fixture anyway (pid %d is gone: %v) — the guard is not guarding", pid, ok)
	}
}

// TestDisplayKillRefusesAnEmptyIncarnation pins the fail-closed direction: a
// record with no starttime is NOT usable identity, so it is refused rather than
// treated as "no check needed". This is the arm that keeps a racing read at
// spawn (where the identity may be unreadable) from degrading into the old
// pid-only behaviour.
func TestDisplayKillRefusesAnEmptyIncarnation(t *testing.T) {
	pid, _ := startDisplayFixture(t)
	for _, empty := range []string{"", "   "} {
		err := displayKillIncarnation(pid, empty, syscall.SIGTERM)
		if err == nil {
			t.Fatalf("displayKillIncarnation accepted the %q incarnation for pid %d — an empty identity must never be treated as a match", empty, pid)
		}
	}
	if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
		t.Fatalf("the fixture died despite a refused delivery: %v", err)
	}
}

// TestDisplayKillDeliversToTheRecordedIncarnation is the second half of the same
// measurement: the guard must still STOP a display THIS run allocated, or the
// fix would trade a stranger-killing bug for a leaked-server bug. The reading is
// the process table, not the return value.
func TestDisplayKillDeliversToTheRecordedIncarnation(t *testing.T) {
	pid, ticks := startDisplayFixture(t)
	display := &FilmDisplay{Number: 0, PID: pid, StartTicks: ticks}
	if err := display.Close(); err != nil {
		t.Fatalf("Close on the recorded incarnation: %v", err)
	}
	deadline := time.Now().Add(displayKillExitPollBound)
	for {
		// GONE means the entry vanished OR the process is a ZOMBIE. The zombie
		// arm is not a shortcut: this fixture's parent (the test) has not reaped
		// it yet, and a zombie "does no work" while still answering kill(pid,0)
		// — the same distinction procname.Verdict documents (a zombie is
		// VerdictRecycled/Gone, never Alive). Reading only "the entry is missing"
		// would report a successfully stopped server as a leaked one.
		state, ok := procname.State(procname.DefaultProcRoot, strconv.Itoa(pid))
		if !ok || state == procname.ZombieState {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("Close returned without stopping pid %d (state %q) — a guard that refuses everything leaks the display server", pid, state)
		}
		time.Sleep(displayKillExitPollStep) //stylegate:allow-sleep bounded wait for the just-signalled fixture to leave /proc
	}
}

// TestDisplayCloseOnAGoneProcessIsNotAnError: the caller closes after a failure
// path may already have killed the server, so ESRCH is the goal state, not a
// reportable failure (Rule 15a: a required non-zero value's zero case gets its
// own test).
func TestDisplayCloseOnAGoneProcessIsNotAnError(t *testing.T) {
	done := exec.Command("/bin/true")
	if err := done.Start(); err != nil {
		t.Skipf("cannot start /bin/true: %v", err)
	}
	pid := done.Process.Pid
	ticks := displayStartTicks(pid)
	_ = done.Wait() //stylegate:ignore the child is /bin/true and its exit status is not the assertion; the Close verdict below is

	if err := (&FilmDisplay{PID: pid, StartTicks: ticks}).Close(); err != nil {
		t.Errorf("closing an already-exited display server reported %v — that is the state Close wants", err)
	}
}
