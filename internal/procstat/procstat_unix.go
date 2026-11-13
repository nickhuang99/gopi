package procstat

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// procfs layout, named once (Rule 1/13: no bare literals for a path or a state
// letter). Deliberately unexported: this package is the only reader, and its
// consumers ask Alive() rather than reassembling the path themselves.
const (
	// procRootName is the procfs mount point.
	procRootName = "/proc"
	// procStatName is the per-process stat file holding the state letter.
	procStatName = "stat"
	// procZombieState is "terminated but not yet reaped". The kernel keeps the
	// /proc entry (and signal 0 keeps succeeding) until the parent waits.
	procZombieState = "Z"
	// procDeadState is the transient state a pid passes through while its exit
	// is being reaped (measured: R → Z → X → /proc gone). It is terminal for
	// the same reason Z is — nothing can schedule it again — and omitting it
	// made a liveness predicate flip back to "alive" mid-exit (2026-10-03).
	procDeadState = "X"
	// procStateWidth is the byte width of the one-letter state field.
	procStateWidth = 1
	// procStateOffset is how far past the closing ')' of comm the state letter
	// sits: "<pid> (<comm>) <state> …" — one space, then the letter.
	procStateOffset = 2
)

// alive probes process existence with the classic POSIX signal-0 trick:
// kill(pid, 0) performs the permission and existence checks without sending
// a signal. ESRCH ("no such process") is the ONLY answer that proves the
// process is gone; EPERM ("exists, but not ours") proves it is still alive.
//
// Deliberately build-tag-free: syscall.Kill exists on every unix-like
// target GoPi is built for, and adding a build tag here would silently
// degrade a tagged build to "no probe at all" — an unknown verdict for
// every session, which is a correctness regression dressed as portability
// (a tagged build would need its own file returning VerdictUnknown).
func alive(pid int) Verdict {
	err := syscall.Kill(pid, 0)
	if err != nil {
		return verdictForKillErr(err)
	}
	// Signal 0 succeeds for a process that exists and for one that is
	// TERMINATED but not yet reaped — the kernel keeps the zombie's pid
	// reserved until its parent waits, and a SIGKILLed wrapper's unreaped
	// child is exactly that shape. A /proc state letter of Z (or the
	// transient X) proves nothing can schedule it again, so it downgrades
	// the signal-0 answer to gone. Unreadable /proc keeps VerdictAlive.
	if terminal(pid) {
		return VerdictGone
	}
	return VerdictAlive
}

// verdictForKillErr maps the FAILURE side of kill(pid, 0) onto a Verdict. It is
// a pure predicate over the error so the three branches can be pinned by a
// table test — the real probe cannot manufacture an EPERM on demand, and a
// fixture that faked one would be testing its own fake.
//
// THE BRANCH THAT WAS WRONG (epic 01a17e87): EPERM. kill(2) answers EPERM when
// the process EXISTS and the caller merely lacks permission to signal it, so
// EPERM is a measurement of EXISTENCE. Every implementation that folded it
// into "dead" turned "I may not look at it" into "it is not there" — measured
// on this host with uid 1000, where syscall.Kill(1, 0) returns EPERM and the old
// probes therefore reported init as dead.
//
//	ESRCH              → gone    the kernel knows no such process
//	EPERM              → alive   the process exists; we may not signal it
//	anything else      → unknown the probe could not answer (ENOSYS, an I/O
//	                            error, a bad pid shape): never a death proof
func verdictForKillErr(err error) Verdict {
	switch {
	case errors.Is(err, syscall.EPERM):
		return VerdictAlive
	case errors.Is(err, syscall.ESRCH):
		return VerdictGone
	default:
		return VerdictUnknown
	}
}

// terminal reports whether /proc/<pid>/stat names a process that can never run
// again (state Z, or the measured transient X). Best-effort by contract: a
// /proc that cannot answer leaves the signal-0 verdict standing, because the
// probe may only turn a live-LOOKING pid into gone, never the reverse.
func terminal(pid int) bool {
	state, ok := procStateLetter(pid)
	if !ok {
		return false
	}
	return stateIsTerminal(state)
}

// procStateLetter reads /proc/<pid>/stat and returns its state letter.
//
// The format is "<pid> (<comm>) <state> …", and comm may itself contain spaces
// and parentheses (a process named "foo (bar)"), so the state is the field
// after the LAST ')' — never a positional split on spaces.
//
// Best-effort by contract: ok=false means /proc did not answer (non-Linux host,
// ENOENT/ESRCH race with the parent's wait, malformed file), and the caller
// keeps the signal-0 verdict. The probe can therefore only turn a live-LOOKING
// pid into gone, never the reverse (Rule 15: no silent upgrade of evidence).
func procStateLetter(pid int) (string, bool) {
	b, err := os.ReadFile(filepath.Join(procRootName, strconv.Itoa(pid), procStatName))
	if err != nil {
		return "", false
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || i+procStateOffset+procStateWidth > len(b) {
		return "", false
	}
	return string(b[i+procStateOffset : i+procStateOffset+procStateWidth]), true
}

// stateIsTerminal reports whether a /proc state letter names a process that can
// never run again: Z (terminated, awaiting reap) or the transient X seen while
// an exit is reaped. Every other letter — and an empty/unknown letter — is NOT
// terminal, so the caller keeps the signal-0 answer.
func stateIsTerminal(state string) bool {
	return state == procZombieState || state == procDeadState
}
