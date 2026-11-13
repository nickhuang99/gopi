// Package daemonlifetime binds a detached gopi daemon's lifetime to a process
// its spawner chose — the KERNEL-enforced half of the e2e process-leak fix.
//
// THE PROBLEM. A gopi daemon (`gopi worker --daemon`, `gopi merger --daemon`)
// is self-daemonized: fork/exec-self + setsid, no controlling terminal. It is
// therefore independent of every shell and of the process that started it —
// which is the POINT in production (an operator's daemon must outlive the
// terminal) and a leak in a test harness, where the daemon belongs to a sandbox
// that dies with the test process. t.Cleanup covers the managed exits (normal
// end, t.Fatal, panic) but cannot cover the test PROCESS being killed: `go test
// -timeout` sends SIGQUIT then SIGKILL, an operator sends kill -9, the OOM killer
// fires. Every daemon started by such a run survived as PPID=1 for hours and kept
// its sandbox on the tmpfs open (the 2026-09-14 /dev/shm exhaustion).
//
// THE MECHANISM (no signal, no polling, no cooperation needed at death time).
// The spawner hands the daemon one extra descriptor: the READ end of a pipe
// whose WRITE end the spawner holds. The daemon watches that descriptor with
// poll(2); when the spawner dies — SIGKILL included, no handler, no cleanup —
// the kernel closes its descriptors and the pipe reports POLLHUP. The daemon
// then takes itself down with SIGKILL.
//
// Two properties make this the right primitive:
//
//   - It is the KERNEL that reports the death, not the spawner's code, so the
//     SIGKILL path (where no user code runs) is covered by construction.
//   - The daemon kills ITSELF. The spawner never has to find, enumerate or
//     signal a pid, so the pid-reuse and "never kill by pattern" hazards do not
//     arise at all.
//
// The write end is deliberately held by the process that must outlive the test
// binary (see internal/testutil's lifetime spawner), so killing the test binary
// is enough: a group leader that outlives it would defeat the whole point.
//
// ARMING IS OPT-IN: with DaemonDeathSignalEnv unset (an operator's own start,
// every production path today) the watchdog is not installed and the daemon
// behaves exactly as before. The mechanism is inert unless a harness asks for it.
package daemonlifetime

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/nick/gopi/internal/borrowedfd"
)

const (
	// EnvName carries the CHILD-side fd number of the death-pipe read end, in
	// the same child-fd convention the daemon handshake uses (os/exec
	// renumbers ExtraFiles from firstExtraFD). A value that cannot be parsed,
	// or is absent, means "not armed".
	//
	// The spawner sets this variable in the CHILD's environment; a process that
	// finds it in its own environment is a daemon that must watch it.
	EnvName = "GOPI_DAEMON_DEATHSIG_FD"
	// HoldEnvName carries the SPAWN-SIDE fd number of the same pipe's read end,
	// set by a harness that asks the spawn seam to hand the descriptor to the
	// daemon. It is a SEPARATE name from EnvName on purpose: two different
	// processes read them, and one number meaning "my fd" in both is exactly the
	// renumbering bug the daemon handshake was bitten by (the parent's fd number
	// is never the child's).
	HoldEnvName = "GOPI_DAEMON_DEATHSIG_HOLD_FD"
	// firstExtraFD is the fd number os/exec gives ExtraFiles[0] in the child. It
	// is the same constant the daemon handshake relies on, restated here because
	// both call sites must agree on ONE number (Rules 1b/14): a mismatch would
	// put the watchdog on a descriptor the child does not have.
	firstExtraFD = 3
)

// FdForExtraEntry maps a 1-based ExtraFiles position (n = the entry counting
// from 1, in the order they were appended) to the fd number the child sees.
// Callers on the spawn side use it to write EnvName, so the number is computed
// once instead of hand-counted at each site.
func FdForExtraEntry(n int) int {
	if n < 1 {
		return firstExtraFD
	}
	return firstExtraFD + n - 1
}

// ExtraFilesOffset is FdForExtraEntry for a 0-based ExtraFiles index — the shape
// a caller has in hand when it appends the death pipe to an existing slice.
func ExtraFilesOffset(index int) int {
	return FdForExtraEntry(index + 1)
}

// ArmedFD returns the death-pipe fd this process was handed, and whether the
// watchdog should run. It is the ONE reader of EnvName (Rule 2).
//
// The fd is VERIFIED to be a pipe before it is used (readlink /proc/self/fd/N
// names the target "pipe:[<inode>]"). An armed variable pointing at a
// descriptor that is not a pipe — a mis-added ExtraFiles entry, a renumbered
// handshake — would otherwise make the watchdog read an unrelated descriptor and
// DISARM itself right where the caller believes the daemon is bound; that is a
// silent failure of exactly the kind this epic is about, so it is announced.
func ArmedFD() (int, bool) {
	v := os.Getenv(EnvName)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "gopi: %s=%q is not an fd number — the daemon lifetime watchdog is NOT armed\n", EnvName, v)
		return 0, false
	}
	if _, err := ValidateHoldFD(n, firstExtraFD); err != nil {
		fmt.Fprintf(os.Stderr, "gopi: %s=%d is not a usable death pipe — the daemon lifetime watchdog is NOT armed: %v\n",
			EnvName, n, err)
		return 0, false
	}
	return n, true
}

// ValidateHoldFD reports whether n is a NUMBER the death-pipe contract can
// actually bind to, and returns what the kernel says that number points at (the
// empty string when the number cannot be resolved at all).
//
// It is the SINGLE SOURCE of the death-pipe shape test (Rule 14): both readers of
// the contract — ArmedFD in this package (the daemon side, watching the number it
// inherited) and cmd/agent's daemonDeathPipe (the spawn side, handing the number
// to os/exec) — call it, so the two can never drift into disagreeing about what a
// usable binding looks like. A second spelling of the pipe test would be a second
// answer to "is this fd the death pipe", which is precisely the class of bug the
// renumbering comments above exist for.
//
// floor IS NOT A SECOND SPELLING — IT IS THE CALLER'S. The two readers stand on
// opposite sides of one renumbering (os/exec numbers ExtraFiles from fd 3), so the
// LOWEST number that can be the pipe differs between them, and only the caller
// knows which side it is on:
//
//	the SPAWN side (daemonDeathPipe) passes InheritFDFloor: it holds the number the
//	  HOLDER named in ITS OWN numbering, where the suite's read end sits at fd 200
//	  precisely so a launcher's ExtraFiles cannot replace it;
//	the DAEMON side (ArmedFD) passes firstExtraFD: the number it sees is the one
//	  os/exec ASSIGNED the entry, and it was handed the entry for exactly that
//	  reason — refusing it for being "below 200" would disarm every bound daemon.
//	  MEASURED 2026-09-19: with both sides on InheritFDFloor the two daemon e2e
//	  cases went red (the daemon outlived its SIGKILLed spawner — a real
//	  regression, first run of this fix).
//
// So the rule each side states is "this number could be the death pipe in MY
// numbering", and the PIPE SHAPE — the part that is genuinely common — has exactly
// one implementation, here.
//
// WHY A SHAPE TEST AT ALL — os.NewFile validates NOTHING. os.NewFile(n, name)
// returns nil only for n < 0; for any other number it hands back a wrapper whose
// Fd() is n, whether or not the process holds such a descriptor (MEASURED
// 2026-09-19: os.NewFile(9999) is non-nil with Fd()==9999, and Stat() is what
// fails). So "the wrapper is nil ⇒ the number is unusable" is a refusal no caller
// can ever reach, and a spawn path that relied on it would accept a free number,
// append it to ExtraFiles, and let os/exec's own "invalid argument" be the only
// report — the precise refusal replaced by a generic failure one layer down
// (Rule 15: the code claims a validation it does not perform).
//
// FAILURE MODES ARE NAMED, NOT LUMPED (each is a different repair):
//
//	n < floor — below the ExtraFiles renumbering range, so the number is silently
//	  REPLACED by whatever the launcher passes as ExtraFiles[0]. MEASURED
//	  2026-09-14: the cmd/agent launcher passes a report pipe as ExtraFiles[0], the
//	  inherited fd 3 was overwritten by that pipe's WRITE end, and the daemon's
//	  watchdog read the wrong descriptor, took EBADF for "the holder is gone" and
//	  self-terminated at once. Under `go test` the standard streams ARE pipes too,
//	  so a value of "1" would arm the watchdog on stdout.
//	unresolvable — no descriptor with that number is open in this process.
//	not a pipe — the number is live but points at something else (a file, a
//	  socket, a device); watching it cannot report the holder's death.
//
// The pipe shape is tested by the kernel's own answer (the /proc/self/fd link
// starts with pipeTargetPrefix), never by a Go-side assumption: os.Pipe on Linux
// is always anonymous, so the evidence is exact.
func ValidateHoldFD(n, floor int) (string, error) {
	if n < floor {
		return "", fmt.Errorf("fd %d is below fd %d, where os/exec's ExtraFiles entries start — a descriptor inherited lower is silently replaced by whatever the launcher passes there",
			n, floor)
	}
	target, ok := fdTarget(n)
	if !ok {
		return "", fmt.Errorf("fd %d is not open in this process (the kernel names no target for it)", n)
	}
	if !strings.HasPrefix(target, pipeTargetPrefix) {
		return "", fmt.Errorf("fd %d is not a pipe: the kernel reports it as %q", n, target)
	}
	return target, nil
}

// pipeTargetPrefix is what the kernel reports for a pipe's /proc/self/fd link
// (`pipe:[12345]`). It is the shape test that distinguishes the death pipe from
// any other descriptor a start may have handed over.
const pipeTargetPrefix = "pipe:["

// fdTarget reads what this process's descriptor n points at. It reports false
// when the descriptor cannot be resolved (closed, or no procfs).
func fdTarget(n int) (string, bool) {
	target, err := os.Readlink(filepath.Join(procSelfFD, strconv.Itoa(n)))
	if err != nil {
		return "", false
	}
	return target, true
}

// procSelfFD is the procfs path for THIS process's descriptors (Rule 1b).
const procSelfFD = "/proc/self/fd"

// WatchFromEnv arms the watchdog when this process was handed a death pipe, and
// reports whether it did. It returns immediately: the watching happens on a
// goroutine, so a daemon's startup path is never delayed by it.
//
// A daemon calls this ONCE, as early as possible in its child entry, so that a
// spawner dying during the daemon's own startup is still caught.
func WatchFromEnv() bool {
	fd, ok := ArmedFD()
	if !ok {
		return false
	}
	// ARM ONCE PER DESCRIPTOR, NOT ONCE PER PROCESS. Two faces of the same verb
	// can reach this call (runTaskWorker arms it for every face, and
	// workerDaemonRun still arms it for the daemon face), and Watch() starts a
	// goroutine plus its own os.NewFile on the SAME borrowed descriptor each
	// time — two readers racing one pipe, and two owners for a descriptor
	// neither of them opened. The second arm on the same fd is therefore
	// skipped.
	//
	// PER FD and not a sync.Once for the whole process: a test binary arms the
	// watchdog repeatedly, each time with a FRESH pipe (fixturelifetime_test,
	// signalrec_fixturebinding_test), and a once-per-process guard would make
	// every arming after the first silently do nothing — turning those tests
	// green while the child they think they armed watches nothing.
	watchArmedMu.Lock()
	defer watchArmedMu.Unlock()
	if watchArmedFDs[fd] {
		return true
	}
	watchArmedFDs[fd] = true
	Watch(fd)
	return true
}

// watchArmedFDs records the descriptors this process has already armed, and
// watchArmedMu guards it (a daemon face can reach the call from a different
// goroutine than the verb entry).
var (
	watchArmedMu  sync.Mutex
	watchArmedFDs = map[int]bool{}
)

// Watch blocks on fd in a goroutine and terminates this process when the pipe's
// last writer goes away (POLLHUP) — i.e. when the process that held it died.
//
// The deadline is -1 (infinite): a daemon is meant to run forever, and a
// timeout here would produce a spurious self-kill on a healthy long-lived
// daemon. The read is used rather than a bare poll so the very same code path
// also ends the daemon if the spawner ever writes a byte (an explicit release).
func Watch(fd int) {
	go func() {
		// OWNERSHIP: this descriptor is BORROWED. Its number arrives through
		// EnvName — named by the process that spawned this daemon — so nothing
		// in this package opened it, and a wrapper dropped on the disarmed route
		// (a read error other than EOF prints and returns WITHOUT terminating)
		// would have its finalizer close a descriptor the HOLDER still owns; the
		// kernel would then recycle that number onto an unrelated live handle.
		// borrowedfd.Detach is the ONE definition of the retention idiom
		// (Rule 14) — imported rather than re-implemented here.
		f := os.NewFile(uintptr(fd), EnvName)
		if f == nil {
			return
		}
		borrowedfd.Detach(f)
		buf := make([]byte, 1)
		// A read on the read end of a pipe returns:
		//   n=0, err=EOF  when every writer is gone (the spawner died), or
		//   n=1           when a byte was written (an explicit release).
		// Both mean "your spawner is gone — stop". Any other error is
		// reported and the watchdog stays off rather than killing a healthy
		// daemon on a transient condition (Rule 15b: reported, not silent).
		n, err := f.Read(buf)
		switch {
		case n == 0 && err != nil:
			selfTerminate("the process that owned this daemon is gone (death pipe closed)")
		case n > 0:
			selfTerminate("the process that owned this daemon released it (death pipe written)")
		default:
			fmt.Fprintf(os.Stderr, "gopi: daemon lifetime watchdog read fd %d: %v — watchdog disarmed\n", fd, err)
		}
	}()
}

// selfTerminate ends this process immediately, with SIGNAL semantics: the
// daemon is killed by SIGKILL rather than exiting cleanly, because the process
// that would have collected the exit status is gone by definition and nothing
// can run its shutdown path anyway.
//
// It is the one deliberate self-kill in this package and it is reported first,
// so the daemon's log records WHY the daemon died (Rule 15b) — an unexplained
// daemon death is exactly the kind of leak/tombstone confusion this epic is
// about.
func selfTerminate(reason string) {
	fmt.Fprintf(os.Stderr, "gopi: daemon lifetime: %s — terminating this daemon (pid %d)\n", reason, os.Getpid())
	// SIGKILL to self: exitCode is irrelevant (the process dies on delivery),
	// and the error is reported rather than swallowed if the signal is refused.
	if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
		fmt.Fprintf(os.Stderr, "gopi: daemon lifetime: self-terminate failed: %v — exiting instead\n", err)
		os.Exit(selfTerminateExitCode)
	}
}

// selfTerminateExitCode is the fallback exit code when SIGKILL to self cannot be
// delivered (a restricted sandbox): the conventional 128+SIGKILL value, so a
// supervisor sees the same number either way.
const selfTerminateExitCode = 137
