package testutil

// The SIGSTOP red proof for the bounded fixture subprocess (runfixture.go) — the
// arm the sibling red proofs in runfixture_test.go do NOT cover.
//
// runfixture_test.go proves the bound against a child that is merely SLOW (a
// plain `sleep`). This file proves it against the shape a live suite actually
// met on a loaded host: a child that is STOPPED — alive, making no progress, and
// never reachable by a signal it could act on. SIGSTOP is the kernel's own
// stand-in for the device stall that motivated the bound (a fresh-DB
// `gopi memory_store` whose fsync parked in jbd2_log_wait_commit, the whole
// fixture in D-state while its parent sat in os/exec.Waitid). The unbounded call
// could not end that wait; the bounded one must.
//
// What must be provable, and is, below:
//
//	① the child is REALLY stopped — the proof reads the state the KERNEL reports
//	   for that pid (/proc/<pid>/stat field 3 == "T"), never an inference from
//	   having sent the signal (Rule 24's habit: measure, do not assume);
//	② with the child stopped, the bounded call still RETURNS — inside budget +
//	   WaitDelay + slack — with an error that NAMES the child (its argv) and the
//	   budget it exceeded, carrying the output captured so far (Rule 15);
//	③ the stopped child is really GONE afterwards (its /proc entry is reaped),
//	   so the proof leaves no orphan holding a pipe or a worktree — the leak
//	   fixture_spawn_inventory_test.go exists to prevent.
//
// Rule 27 RESPECT: nothing here generates load. It reads /proc and sends
// SIGSTOP/SIGCONT/SIGKILL to exactly one child.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// fixtureStopProofBudget is the budget the stopped-child arm runs under. It
	// is larger than runfixture_test.go's red-proof budget ON PURPOSE: the probe
	// needs a moment to observe the child and stop it, and a bound that fired
	// before the child was stopped would prove nothing about a stopped child.
	fixtureStopProofBudget = 2 * time.Second
	// fixtureStopProofProbeTimeout bounds how long the probe waits for the child
	// to publish its own pid before the red proof is declared inconclusive.
	fixtureStopProofProbeTimeout = 10 * time.Second
	// fixtureStopProofProbeInterval is the pid-file poll interval.
	fixtureStopProofProbeInterval = 5 * time.Millisecond
	// fixtureStopProofStopTimeout bounds the wait for the kernel to report the
	// SIGSTOPped child as stopped (task state "T"). The task state is not
	// updated synchronously with kill(2), so the observation — not the
	// assertion — is what waits.
	fixtureStopProofStopTimeout = 2 * time.Second
	// fixtureStopProofChild is the child the probe stops. It publishes its own
	// pid, then REPLACES itself with the sleeper (`exec`), so it has NO
	// descendant: the stopped process IS the whole fixture, and killing it can
	// leak nothing. (A `sleep 300 &` grandchild — the shape proof ② uses — would
	// survive the kill and trip the suite's live-child sweep.)
	fixtureStopProofChild = "echo $$ > %s; exec sleep 300"
	// fixtureStopProofReapTimeout bounds the wait for the killed child's /proc
	// entry to disappear.
	fixtureStopProofReapTimeout = 5 * time.Second
	// fixtureStopProofStoppedState is the state letter Linux reports for a
	// SIGSTOPped process (task state "T" = stopped by job control signal).
	fixtureStopProofStoppedState = "T"
)

// TestFixtureCommandKillsAStoppedChildAndNamesIt is the SIGSTOP red proof. It
// starts a fixture child, stops it with SIGSTOP while the bounded call is in
// flight, and asserts the call comes back FAILING by name and budget instead of
// parking the suite.
func TestFixtureCommandKillsAStoppedChildAndNamesIt(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := fmt.Sprintf(fixtureStopProofChild, pidFile)

	// The probe runs on its own goroutine: the bounded call below BLOCKS until
	// its budget fires, so the stop has to be delivered from beside it. The
	// result travels back on a channel — a t.Errorf from a goroutine is not a
	// thing this test may do.
	type stopResult struct {
		pid   int
		state string
		err   error
	}
	stopped := make(chan stopResult, 1)
	go func() {
		pid := waitForPidFile(pidFile, fixtureStopProofProbeTimeout)
		if pid == 0 {
			stopped <- stopResult{err: fmt.Errorf(
				"the child never published its pid to %s within %s — the red proof did not run",
				pidFile, fixtureStopProofProbeTimeout)}
			return
		}
		if killErr := syscall.Kill(pid, syscall.SIGSTOP); killErr != nil {
			stopped <- stopResult{pid: pid, err: fmt.Errorf("SIGSTOP %d: %w", pid, killErr)}
			return
		}
		// Ground truth: the state the KERNEL reports. Having sent the signal is
		// not the same as the child being stopped — the task state is not
		// updated synchronously with kill(2), so a single read can still see
		// "R". Poll (bounded) until the kernel says "T"; the assertion below is
		// unchanged, only the observation waits for the transition it asks about.
		state := ""
		deadline := time.Now().Add(fixtureStopProofStopTimeout)
		for time.Now().Before(deadline) {
			state = procTaskState(pid)
			if state == fixtureStopProofStoppedState || state == "" {
				break
			}
			//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
			time.Sleep(fixtureStopProofProbeInterval)
		}
		stopped <- stopResult{pid: pid, state: state}
	}()

	started := time.Now()
	out, err := runFixtureCommand("/bin/sh", nil, []string{"-c", script}, fixtureStopProofBudget)
	elapsed := time.Since(started)

	res := <-stopped
	if res.err != nil {
		t.Fatalf("the probe could not stop the child, so this run proves nothing: %v", res.err)
	}
	// Whatever happens next, the probe's child must not be left behind.
	t.Cleanup(func() {
		_ = syscall.Kill(res.pid, syscall.SIGCONT) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error// a stopped process takes no SIGKILL until continued? it does — belt and braces
		_ = syscall.Kill(res.pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error
	})

	// ① the child was really stopped (measured, not assumed).
	if res.state != fixtureStopProofStoppedState {
		t.Errorf("/proc/%d/stat reports state %q, want %q — the fixture child was NOT stopped, so this run did not "+
			"exercise the stopped-child arm", res.pid, res.state, fixtureStopProofStoppedState)
	}

	// ② the bound held, and it reported itself.
	if err == nil {
		t.Fatalf("a SIGSTOPped child returned from the bounded call with NO error (out=%q) — an unbounded "+
			"fixture child in exactly this shape is what parks a suite in os/exec.Waitid forever", out)
	}
	if !strings.Contains(err.Error(), fixtureStopProofBudget.String()) {
		t.Errorf("the budget error must name the budget it exceeded (Rule 15: a killed child is reported, never "+
			"silent); got: %v", err)
	}
	if !strings.Contains(err.Error(), pidFile) {
		t.Errorf("the budget error must NAME THE CHILD — its argv, which carries this run's own pid file %s — so a "+
			"reader knows which fixture parked; got: %v", pidFile, err)
	}
	if elapsed > fixtureStopProofBudget+fixtureCommandWaitDelay+fixtureRedProofSlack {
		t.Errorf("the bounded call took %s under a %s budget — the bound did not hold for a STOPPED child "+
			"(budget + WaitDelay + slack)", elapsed, fixtureStopProofBudget)
	}

	// ③ the stopped child was reaped, not left holding a pipe or a worktree.
	reapDeadline := time.Now().Add(fixtureStopProofReapTimeout)
	for time.Now().Before(reapDeadline) && procTaskState(res.pid) != "" {
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(fixtureStopProofProbeInterval)
	}
	if state := procTaskState(res.pid); state != "" {
		t.Errorf("the killed child pid %d is still in /proc (state %q) %s after the kill — a leaked fixture child "+
			"holding the inherited pipe is the leak internal/testutil/fixture_spawn_inventory_test.go guards against",
			res.pid, state, fixtureStopProofReapTimeout)
	}
}

// waitForPidFile polls for the child's published pid, returning 0 on timeout.
func waitForPidFile(path string, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 0 {
				return pid
			}
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(fixtureStopProofProbeInterval)
	}
	return 0
}

// procTaskState returns the task state letter Linux reports for pid, or "" when
// the process is gone (or /proc cannot be read). The field is read AFTER the
// comm field's closing parenthesis, because comm may itself contain spaces and
// parentheses (`sh -c …` does not, but a reader must not depend on that).
func procTaskState(pid int) string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	text := string(raw)
	close := strings.LastIndex(text, ")")
	if close < 0 || close+2 >= len(text) {
		return ""
	}
	fields := strings.Fields(text[close+1:])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
