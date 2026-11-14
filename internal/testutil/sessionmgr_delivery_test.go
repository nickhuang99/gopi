package testutil

// The DELIVERY-POINT seam: KillIfIdentical must refuse the login session's
// manager, not merely a recycled pid.
//
// SAFETY: this file never delivers a terminating signal. Every arm uses signal 0
// — a permission probe that consults the same credential checks and terminates
// nothing, so the "guard absent" side of the red proof is harmless even though
// the target is the operator's own `systemd --user` (whose SIGINT stops the
// whole desktop session: journalctl -b "Received SIGINT from PID … (agent.test)"").

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// sessionMgrDeliverySig is the PROBE signal: it consults the kernel's
	// permission and existence checks and delivers nothing, so an arm that
	// reaches the syscall cannot end a session.
	sessionMgrDeliverySig = syscall.Signal(0)
)

// liveSessionManagerIdentity returns the ProcIdentity of this pid namespace's
// systemd user instance, or ok=false when the host has none.
func liveSessionManagerIdentity() (ProcIdentity, bool) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return ProcIdentity{}, false
	}
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		pid, convErr := strconv.Atoi(name)
		if convErr != nil || pid <= 0 {
			continue
		}
		if !procname.IsLoginSessionManagerPID(pid) {
			continue
		}
		id, readErr := ReadProcIdentity(pid)
		if readErr != nil || id.StartTime == 0 {
			continue
		}
		return id, true
	}
	return ProcIdentity{}, false
}

// TestKillIfIdenticalRefusesTheLoginSessionManager is the repro for the seam:
// the incarnation identity ALONE would permit this delivery (starttime matches,
// the process is alive and not a zombie), and only the manager predicate stops
// it.
func TestKillIfIdenticalRefusesTheLoginSessionManager(t *testing.T) {
	id, ok := liveSessionManagerIdentity()
	if !ok {
		t.Skipf("no systemd user instance in this pid namespace (%s) — the seam cannot be measured here", procDir)
	}

	// (1) THE INCARNATION ARM AGREES — the pre-existing guard is not what stops
	// this delivery. Asserted directly so the case below cannot pass for the
	// wrong reason.
	alive, why := id.Alive()
	if !alive {
		t.Fatalf("the session manager's captured identity does not match its live process (%s) — the two arms below would then be measuring the wrong refusal", why)
	}

	// (2) THE SEAM REFUSES, by name.
	err := KillIfIdentical(id, sessionMgrDeliverySig)
	if !errors.Is(err, procname.ErrSessionManagerTarget) {
		t.Fatalf("KillIfIdentical(pid %d = systemd user instance, signal 0) = %v, want an error wrapping procname.ErrSessionManagerTarget — the incarnation arm alone PERMITS this delivery, so without this refusal the shipped seam would signal the operator's session manager", id.Pid, err)
	}
	if !strings.Contains(err.Error(), "desktop session") {
		t.Errorf("the refusal must name the consequence, got: %v", err)
	}

	// (3) NEGATIVE CONTROL, same seam: an ordinary child must still be signalled
	// (otherwise a seam that refused everything would pass (2)).
	mock := StartMockProcess(t, MockDaemonBinary, MockDaemonHoldSeconds)
	child, err := AwaitProcIdentity(mock.Pid())
	if err != nil {
		t.Fatalf("read the identity of the control child %d: %v", mock.Pid(), err)
	}
	if err := KillIfIdentical(child, syscall.SIGTERM); err != nil {
		t.Fatalf("the seam refused an ordinary child (%v) — it must stay transparent to every non-manager target", err)
	}

	// (4) AND THE SESSION SURVIVED: the manager is still the same incarnation.
	after, readErr := ReadProcIdentity(id.Pid)
	if readErr != nil || after.StartTime != id.StartTime {
		t.Fatalf("the session manager's incarnation changed across this test (was %s, now %+v, error %v) — an arm above delivered a real signal", id, after, readErr)
	}
}
