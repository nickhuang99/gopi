package procname

// The login-session MANAGER as a MEASURED process fact — the one process whose
// death takes the operator's whole desktop session with it.
//
// WHY THIS LIVES HERE, NEXT TO THE OTHER PROCESS FACTS (Rules 13/14): the
// identification is two reads of a live /proc entry (comm == "systemd" AND the
// "--user" argv word), which is the same positive identification
// UserManagerComm/UserManagerArg already document and OrphanReparentTargets
// already performs. A second copy of "what a user manager looks like" in another
// package would be a second model to keep true.
//
// WHY AN INCARNATION GUARD IS NOT ENOUGH (measured 2026-10-17, five times in one
// boot on this host). testutil.KillIfIdentical / internal/shot's
// displayKillIncarnation answer "is the pid still the process I recorded?" — they
// refuse a RECYCLED pid. They cannot answer "would delivering to this pid be
// catastrophic?" for a pid that is not recycled at all: a delivery point that
// formed its pid some other way (a pattern match, a group id, a sibling's pid, a
// ledger row written by another run) hands the incarnation guard a coherent
// identity and the guard correctly agrees. SIGINT to a systemd USER instance
// stops the WHOLE user session (journalctl -b: "Received SIGINT from PID …
// (agent.test)" → "Stopped target Current graphical user session").

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrSessionManagerTarget is the sentinel a refused delivery wraps, so a caller
// can report the refusal as its own verdict rather than a generic failure.
var ErrSessionManagerTarget = fmt.Errorf("refusing to deliver a signal to the login session's manager")

// IsLoginSessionManagerPID reports whether pid is, RIGHT NOW, a systemd USER
// instance — the manager systemd names `user@<uid>.service`.
//
// A DEAD pid is not a manager (the /proc entry is gone, so the number names
// nothing and a delivery to it reaches nobody — the harmless case the caller's
// own incarnation record already covers). An UNREADABLE one is not a manager
// either: this predicate's contract is a POSITIVE identification, and it must
// never claim a manager it did not measure.
func IsLoginSessionManagerPID(pid int) bool {
	return isLoginSessionManagerIn(ProcRootDefault, pid)
}

// isLoginSessionManagerIn is the same predicate against an arbitrary procfs root
// — the seam this package's own tests drive with a fixture tree, so the
// identification is exercised against a KNOWN systemd-shaped entry without ever
// starting (or signalling) a real user manager.
func isLoginSessionManagerIn(procRoot string, pid int) bool {
	if pid <= 0 {
		return false
	}
	dir := procRoot + "/" + strconv.Itoa(pid)
	comm, err := os.ReadFile(dir + "/" + CommName)
	if err != nil || strings.TrimSpace(string(comm)) != UserManagerComm {
		return false
	}
	argv, ok := Cmdline(procRoot, strconv.Itoa(pid))
	if !ok {
		return false
	}
	for _, word := range argv {
		if word == UserManagerArg {
			return true
		}
	}
	return false
}

// RefuseLoginSessionManagerTarget is the DELIVERY-POINT second question: does
// pid currently name the login session's manager, and must a signal therefore be
// refused?
//
// A REFUSAL IS AN ERROR, never a silent skip: the caller's bookkeeping then
// points at the operator's own session manager, which is a fact worth surfacing
// (Rule 15 — silence is the defect, not the inconvenience). A nil return means
// only "not a manager"; the caller's own liveness/incarnation handling stays
// exactly as it was.
func RefuseLoginSessionManagerTarget(pid int) error {
	if !IsLoginSessionManagerPID(pid) {
		return nil
	}
	return fmt.Errorf("%w: pid %d is a systemd user instance (%s / %s) — a signal there ends the operator's whole desktop session, not one process",
		ErrSessionManagerTarget, pid, UserManagerComm, UserManagerArg)
}
