package daemonlifetime

// The SUITE-level binding: one pipe per test binary, so EVERY daemon that binary
// starts is bound to its lifetime — with no per-call handshake and no fd number
// to get wrong at a call site.
//
// WHY A PROCESS-WIDE PIPE. The per-spawn contract (HoldEnvName naming a fd in
// the spawning process) is correct but needs every caller to create a pipe, keep
// its write end alive and state the right number — and "the right number" is not
// the caller's own fd: exec renumbers the descriptor. A first attempt got that
// wrong and the failure was SILENT (the launcher refused the fd, the watchdog
// stayed unarmed, the daemon outlived its test) — measured 2026-09-14. The
// process-wide pipe removes the class: the number is fixed once, at arming time,
// and every descendant inherits it unchanged.
//
// THE SHAPE (a listen-fd):
//
//	ArmThisProcess() creates ONE pipe, clears FD_CLOEXEC on the READ end only,
//	parks the WRITE end in this process, and exports HoldEnvName naming the read
//	end. Every descendant inherits the read end at the SAME fd number (fd numbers
//	survive exec unless renumbered), so a short-lived launcher passes the binding
//	on to the daemon it fork/execs. The write end keeps os.Pipe's O_CLOEXEC, so NO
//	descendant can hold it: the kernel closes the last writer exactly when THIS
//	process dies — SIGKILL included — and the daemon's watchdog kills it.
//
// PRODUCTION IS UNTOUCHED: nothing in a production path calls ArmThisProcess, so
// an operator's `gopi worker --daemon` inherits no binding and keeps its detached
// lifetime exactly as before.
//
// A caller must NOT arm this from a process that is not the intended holder: the
// holder is whoever dies first, and binding a daemon to a launcher that exits
// immediately would kill it at once.

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	// suiteHold parks the ONE write end for the life of this process. A write end
	// the garbage collector finalized would close the pipe and kill every bound
	// daemon early — the same class of bug as an *os.File holding a flock going
	// unreachable.
	suiteHold *os.File
	// suiteArmedOnce makes arming idempotent (a second pipe would leave the first
	// unheld, and the daemons bound to it would be unbound).
	suiteArmedOnce sync.Once
	// suiteArmedErr records a failure to arm, so a caller can report it instead
	// of running unprotected while believing otherwise (Rule 15).
	suiteArmedErr error
)

// ArmThisProcess binds every daemon this process — or any descendant it starts —
// to this process's lifetime.
//
// It is the test suite's arming call; production paths do not make it. An error
// means the binding is NOT in force and the caller must say so, never shrug: a
// suite that believes it is protected while it is not is the original leak with
// nobody watching.
func ArmThisProcess() error {
	suiteArmedOnce.Do(func() { suiteArmedErr = armSuite() })
	return suiteArmedErr
}

// AdoptInheritedHoldFD converts the HOLD-side variable into the watch-side one
// for a process that inherited the death pipe at the SAME fd number.
//
// This is the listen-fd case: the holder armed a pipe and exported HoldEnvName,
// every descendant inherited the read end unchanged, and a process that IS the
// daemon (a child started with the environment as-is) only needs to adopt the
// number its parent already named. It returns (false, nil) when no binding was
// inherited — a normal production start — and an error when a binding WAS
// inherited but is unusable, so a caller can never silently run unbound
// (Rule 15). When EnvName is already set explicitly, that wins: the caller made
// a deliberate statement about which fd to watch.
func AdoptInheritedHoldFD() (bool, error) {
	if os.Getenv(EnvName) != "" {
		return true, nil
	}
	raw := os.Getenv(HoldEnvName)
	if raw == "" {
		return false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return false, fmt.Errorf("%s=%q is not an fd number — refusing to run with an unusable lifetime binding", HoldEnvName, raw)
	}
	if err := os.Setenv(EnvName, strconv.Itoa(n)); err != nil { // env-leak-ok: this process is the daemon; it never returns to a caller's test
		return false, fmt.Errorf("adopt the inherited lifetime fd %d: %w", n, err)
	}
	return true, nil
}

// ArmedForThisProcess reports whether the binding is in force — the guard a case
// that DEPENDS on it uses, so it fails rather than passing for the wrong reason.
func ArmedForThisProcess() bool { return suiteHold != nil }

// InheritFDFloor is the lowest fd number the inherited read end may occupy.
//
// WHY NOT fd 3: os/exec renumbers every ExtraFiles entry from fd 3 upwards
// (dup2 into 3..3+n-1), so a descriptor inherited at fd 3 is SILENTLY REPLACED by
// whatever the launcher passes as ExtraFiles[0]. MEASURED 2026-09-14: the
// cmd/agent launcher passes a report pipe as ExtraFiles[0], the inherited fd 3
// was overwritten by that pipe's WRITE end, the daemon's watchdog read the
// wrong descriptor, got n=0 with EBADF, read it as "the holder is gone" and
// self-terminated at once — a bound daemon dying immediately, and the negative
// case caught it while the positive case would have stayed green for the wrong
// reason. An fd above any plausible ExtraFiles count cannot be clobbered.
const InheritFDFloor = 200

// armSuite creates the listen pipe and exports it.
func armSuite() error {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create the daemon lifetime pipe: %w", err)
	}
	// Move the read end ABOVE the ExtraFiles renumbering range and clear
	// FD_CLOEXEC there in one step (F_DUPFD returns a fresh descriptor with
	// close-on-exec cleared). Clearing it on the READ end ONLY is what makes the
	// write end unforgeable: no descendant can hold the write end, so only this
	// process's death closes the pipe.
	inheritFD, err := unix.FcntlInt(readEnd.Fd(), unix.F_DUPFD, InheritFDFloor)
	if err != nil {
		_ = readEnd.Close() //stylegate:ignore best-effort close of a pipe this function is abandoning
		_ = writeEnd.Close()
		return fmt.Errorf("move the daemon lifetime read end above fd %d: %w", InheritFDFloor, err)
	}
	if err := readEnd.Close(); err != nil {
		return fmt.Errorf("close the original daemon lifetime read end: %w", err)
	}
	suiteHold = writeEnd
	if err := os.Setenv(HoldEnvName, strconv.Itoa(inheritFD)); err != nil {
		return fmt.Errorf("export %s: %w", HoldEnvName, err)
	}
	fmt.Fprintf(os.Stderr, "daemon lifetime: armed — read end fd %d is inherited by every gopi process this one starts; "+
		"the write end is held by pid %d alone, so any daemon started under it dies with this process (SIGKILL included)\n",
		inheritFD, os.Getpid())
	return nil
}
