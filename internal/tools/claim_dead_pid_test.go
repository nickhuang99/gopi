package tools

// Dead-claim fixtures (previously part of task_run_closeout_test.go, which the
// child-side run close-out's tests filled; task 01a16f7c deleted that surface
// and kept these two helpers because OTHER tests build their fixtures on them).
//
// Both derive their value from the system rather than from a literal: the pid
// ceiling comes from /proc/sys/kernel/pid_max and the host from os.Hostname,
// the same rule the claim parser itself obeys (Rule 13).

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// pidMaxPath is the kernel's pid ceiling (Linux), read so the dead-claim
// fixture is derived from the system rather than from a magic number.
const pidMaxPath = "/proc/sys/kernel/pid_max"

// deadClaimPid returns a pid no process can ever hold — `pidAlive` is false by
// construction, which is exactly the zombie chain's layer-b verdict ("the
// claimed runner is gone"). A freshly-reaped child (the obvious fixture) is NOT
// good enough here: the go test binary itself is forking processes constantly,
// and a recycled pid made this fixture flap.
func deadClaimPid(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(pidMaxPath)
	if err != nil {
		t.Skipf("cannot read %s: %v", pidMaxPath, err)
	}
	pidMax, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pidMax <= 0 {
		t.Skipf("unparsable %s: %q", pidMaxPath, string(raw))
	}
	pid := pidMax + 1 // never allocatable: above the kernel's pid ceiling
	if pidAlive(pid) {
		t.Skipf("pid %d (pid_max+1) reports alive — cannot build a dead-claim fixture", pid)
	}
	return pid
}

// hostClaimStamp builds a claimed_by stamp (the local host + a pid — the shape
// the run funnel records) the zombie claim parser accepts: the REAL local
// hostname (claimHolder ignores a pid on a foreign host) + a pid. Rule 13: the
// values come from their own sources, never a literal host.
func hostClaimStamp(t *testing.T, pid int) string {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	if _, _, ok := claimHolder(host + ":" + itoa(pid)); !ok {
		t.Skipf("hostname %q is not the claim parser's local host", host)
	}
	return host + ":" + itoa(pid)
}
