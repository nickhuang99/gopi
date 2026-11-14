package main

// borrowed_fd_sites_test.go — the kernel-level guards for the cmd/agent
// borrowed-fd sites of the os.NewFile-finalizer defect (task 01a0b6fa,
// devdoc/fd-finalizer-leak.md).
//
// THE DEFECT, once, because every guard below is one of its shapes:
// os.NewFile(fd, name) wraps an already-open descriptor AND installs
// runtime.SetFinalizer(f.file, (*file).close) (GOROOT/src/os/file_unix.go). A
// wrapper a production path merely DROPS is therefore closed by the runtime's
// finalizer goroutine (runtime.runfinq) — under whoever really owns that number.
// Two damages follow: the owner's next readdirent/close returns EBADF, and the
// kernel hands the freed number to whatever is opened next, so the owner's handle
// silently denotes a stranger's file. Measured shape: `TempDir RemoveAll cleanup:
// readdirent 001: bad file descriptor`, fd 7, stack from runtime.runfinq.
//
// HOW THESE GUARDS ARE SHAPED. Every claim is asked of the KERNEL — fcntl
// F_GETFD (0 = open, EBADF = closed) and readlink /proc/self/fd/N (which file the
// number denotes) — never of the wrapper's own state, because the unfixed code
// also "intended" to stop using the wrapper. A forced-GC window
// (borrowedfd.SettleFinalizers: 8 rounds, named constants) opens the hazard's
// moment, which only exists once the wrapper is unreachable.
//
// Every descriptor these guards wrap is taken with a RAW syscall.Dup, never
// through os.NewFile: a retained *os.File or a t.Cleanup handle would own the
// number and mask the very leak under test.
//
// RED-BEFORE is part of the deliverable: each guard was run on a PRISTINE worktree
// detached at the base commit and failed there. The outputs are quoted in
// devdoc/fd-finalizer-borrowed-sites.md.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/borrowedfd"
	"github.com/nickhuang99/gopi/internal/daemonlifetime"
)

const (
	// bfdProbeName is what a probe wrapper calls itself — the string a future
	// reader sees in an fd trail, and the one `os.NewFile` records.
	bfdProbeName = "borrowed-fd-site-probe"
	// bfdReadyDeadlineWait bounds the read of the readiness byte in the readiness-site
	// guard: the write has already happened, so this only covers scheduling.
	bfdReadyDeadlineWait = 3 * time.Second
	// bfdProgName is the program label these guards pass to the production
	// handshake helpers (they take it for their messages).
	bfdProgName = "gopi-fd-site-probe"
	// bfdProcSelfFD is the procfs directory of THIS process's descriptors (Rule 1b).
	bfdProcSelfFD = "/proc/self/fd"
	// bfdReadyMsg is the byte written through a readiness descriptor.
	bfdReadyMsg = "R"
	// bfdFlockHolderScript holds LOCK_EX on the file named in argv[1] until killed.
	// It is a SEPARATE PROCESS because flock lives on the open file description: a
	// dup of THIS process's own description re-takes the lock successfully, so the
	// contention the production refusal waits for cannot be produced in-process.
	bfdFlockHolderScript = "import fcntl,sys,time\n" +
		"f=open(sys.argv[1],'w')\n" +
		"fcntl.flock(f,fcntl.LOCK_EX)\n" +
		"sys.stdout.write('locked\\n')\n" +
		"sys.stdout.flush()\n" +
		"time.sleep(120)\n"
	// bfdHolderReadyWait bounds the wait for that holder's readiness line.
	bfdHolderReadyWait = 20 * time.Second
)

// bfdFDAlive asks the KERNEL whether n is an open descriptor.
func bfdFDAlive(n int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(n), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// bfdTarget reports what this process's descriptor n points at, and whether the
// kernel could resolve it at all ("gone" is the observation a finalizer close
// produces).
func bfdTarget(n int) (string, bool) {
	target, err := os.Readlink(filepath.Join(bfdProcSelfFD, strconv.Itoa(n)))
	if err != nil {
		return "", false
	}
	return target, true
}

// bfdRawDup places f on a fresh descriptor and returns the raw NUMBER (no
// *os.File, no cleanup handle — see the file header).
func bfdRawDup(t *testing.T, f *os.File) int {
	t.Helper()
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatalf("dup the fixture descriptor: %v", err)
	}
	return fd
}

// bfdStartFlockHolder starts a separate process holding LOCK_EX on path, and
// returns it once the lock is provably taken. The caller must stop it.
func bfdStartFlockHolder(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	holder := exec.Command("python3", "-c", bfdFlockHolderScript, path)
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatalf("holder stdout pipe: %v", err)
	}
	if err := holder.Start(); err != nil {
		t.Skipf("cannot start the external flock holder (python3 unavailable): %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill() //stylegate:ignore best-effort kill of the fixture holder
		_, _ = holder.Process.Wait()
	})
	ready := make(chan string, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr != nil && line == "" {
			// The holder died before it could report: the caller's emptiness check
			// turns this into a failure, so the reason is recorded here (Rule 15b).
			fmt.Fprintf(os.Stderr, "borrowed-fd guard: the flock holder's readiness line: %v\n", readErr)
		}
		ready <- line
	}()
	select {
	case line := <-ready:
		if line == "" {
			t.Fatalf("the external flock holder died before taking the lock")
		}
	case <-time.After(bfdHolderReadyWait):
		t.Fatalf("the external flock holder did not take the lock within %s", bfdHolderReadyWait)
	}
	return holder
}

// TestBorrowedFDObservationIsSensitive CALIBRATES the instrument these guards
// rest on. A recipe that cannot go red makes every green guard in this file
// worthless, so the calibration asserts the DEFECT itself: a wrapper dropped with
// no detach must close its borrowed number, observably, through the same
// F_GETFD / readlink calls the guards use.
func TestBorrowedFDObservationIsSensitive(t *testing.T) {
	owner, doomed := bfdRawFixture(t)
	// THIS ARM'S PREMISE IS THAT THE NUMBER DIES: the dropped wrapper below is what
	// closes it. The fixture must therefore NOT close it afterwards — a raw close
	// here is a second close on a number the kernel may already have recycled.
	doomedClaim := bfdClaimIdentity(t, doomed)
	t.Cleanup(func() { bfdExpectLateCloseDefeated(t, doomedClaim) })

	wrapper := os.NewFile(uintptr(doomed), bfdProbeName)
	if wrapper == nil {
		t.Fatalf("os.NewFile(%d) returned nil — the calibration needs a wrapper", doomed)
	}
	wrapper = nil // dropped, with no detach: the defect's exact shape
	borrowedfd.SettleFinalizers()

	if err := bfdFDAlive(doomed); err == nil {
		t.Fatalf("the observation cannot see the defect: descriptor %d still open after %d GC rounds "+
			"(os.NewFile no longer installs a finalizer on this runtime, or the window is too short) — "+
			"every other guard in this file would be green for the wrong reason", doomed, borrowedfd.FinalizerSettleRounds())
	}
	if _, ok := bfdTarget(doomed); ok {
		t.Fatalf("readlink still resolves descriptor %d although F_GETFD reported it closed — the two observations disagree", doomed)
	}
	// The owner's description is untouched: only the NUMBER died, which is why the
	// damage is a recycled number rather than a lost file.
	if err := bfdFDAlive(int(owner.Fd())); err != nil {
		t.Fatalf("the OWNER's descriptor %d died too (%v) — the calibration needs the borrowed number to die alone", owner.Fd(), err)
	}
}

// bfdClaimIdentity records what the kernel says number n denotes, taken while the
// number is still this fixture's.
//
// It is taken at CLAIM time and never at release time: a comparison with both
// sides read after the window opened always agrees, and a guard built that way is
// green for the wrong reason (internal/borrowedfd/release.go says the same).
func bfdClaimIdentity(t *testing.T, n int) borrowedfd.Identity {
	t.Helper()
	id, err := borrowedfd.IdentityOf(n)
	if err != nil {
		t.Fatalf("record the identity of fixture fd %d: %v", n, err)
	}
	return id
}

// bfdReleaseOwn gives back a number NOBODY ELSE has released: the guarded release
// closes it, and a refusal is a failure because that would mean the fixture no
// longer agrees with the kernel about what it holds.
func bfdReleaseOwn(t *testing.T, id borrowedfd.Identity) {
	t.Helper()
	if err := id.Release(); err != nil {
		t.Errorf("releasing the fixture's own fd %d was refused or failed: %v", id.Number(), err)
	}
}

// bfdReleaseViaHolder releases a number whose OWNERSHIP WAS HANDED OVER, through
// the holder the production path returned (*os.File) — never by raw number.
//
// Why this and not syscall.Close: the holder is the object that owns the open file
// description (the flock lives on it), and its Close clears the finalizer
// os.NewFile installed. The fixture's raw close would be a SECOND, late close on a
// number the kernel may already have recycled — the defect itself, committed by
// the guard. The identity check runs first so a recycled number is NOT closed even
// through the holder.
func bfdReleaseViaHolder(t *testing.T, holder *os.File, id borrowedfd.Identity) {
	t.Helper()
	if holder == nil {
		t.Errorf("fd %d was handed over but the holder is nil — nothing can release it", id.Number())
		return
	}
	if err := id.Check(); err != nil {
		t.Errorf("fd %d is no longer the descriptor this fixture claimed, so it is NOT closed through its holder: %v", id.Number(), err)
		return
	}
	if err := holder.Close(); err != nil {
		t.Errorf("the holder released fd %d and failed: %v", id.Number(), err)
	}
}

// bfdExpectLateCloseDefeated constructs the DEFECT's window on the number the
// fixture used to hold and proves the late close cannot reach a stranger:
//
//	the number is already released (by a production path or by the finalizer)
//	→  a STRANGER is placed on it with F_DUPFD  →  the fixture's late release.
//
// The late release must refuse and name the stranger, and the stranger must
// survive. An ordinary open() cannot be used to place the stranger: it takes the
// LOWEST free number (measured: fd 7), which is why the window is built with an
// explicit floor instead of hoped for.
func bfdExpectLateCloseDefeated(t *testing.T, id borrowedfd.Identity) {
	t.Helper()
	number := id.Number()
	strangerPath := filepath.Join(t.TempDir(), "stranger")
	// THE STRANGER IS OPENED AS A RAW NUMBER, never through os.OpenFile: a wrapper
	// minted before the number is placed would be ABANDONED when the descriptor is
	// renumbered below, and its finalizer would later close that same number a
	// second time — after the kernel had recycled it to somebody else. Measured:
	// that stray close landed on another test's pipe (fd 8) in the same package run
	// and failed it with "transport-lost". A wrapper is only ever minted HERE, on a
	// number that will not move again.
	strangerFd, err := syscall.Open(strangerPath, syscall.O_CREAT|syscall.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the stranger file: %v", err)
	}
	// MEASURED: an ordinary open() takes the LOWEST free number, so the stranger's
	// own open often lands on the recycled number by itself (fd 7). The first
	// version of this helper then dup'ed it to the same number and found the next
	// free one (fd 8), failing for a reason that had nothing to do with the defect.
	if strangerFd != number {
		got, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(strangerFd), uintptr(syscall.F_DUPFD), uintptr(number))
		if errno != 0 {
			t.Fatalf("hand the released number %d to a stranger: %v", number, errno)
		}
		if closeErr := syscall.Close(strangerFd); closeErr != nil {
			t.Fatalf("release the stranger's original raw handle after renumbering it: %v", closeErr)
		}
		if int(got) != number {
			t.Fatalf("the stranger landed on fd %d, not the released %d — the window this guard needs is not open", got, number)
		}
		strangerFd = int(got)
	}
	strangerClaim, err := borrowedfd.IdentityOf(strangerFd)
	if err != nil {
		t.Fatalf("record the stranger's identity on fd %d: %v", number, err)
	}
	t.Cleanup(func() {
		// The stranger is the ONLY holder of the number, so releasing it is safe.
		if relErr := strangerClaim.Release(); relErr != nil {
			t.Errorf("releasing the stranger's own descriptor %d was refused: %v", number, relErr)
		}
	})

	// THE LATE CLOSE — the one the fixture used to make with a bare syscall.Close.
	lateErr := id.Release()
	if lateErr == nil {
		t.Fatalf("the fixture's late release of fd %d SUCCEEDED after the number was recycled to %q — the fixture "+
			"closed a descriptor it no longer owned, and syscall.Close reports success while doing it", number, strangerPath)
	}
	var moved *borrowedfd.MovedError
	if !errors.As(lateErr, &moved) {
		t.Fatalf("the refusal for fd %d is %T (%v), want *borrowedfd.MovedError — 'refused to close a stranger' must be "+
			"distinguishable from 'the close failed'", number, lateErr, lateErr)
	}
	if !strings.Contains(moved.Error(), strangerPath) {
		t.Errorf("the refusal does not name what the number denotes now (%q), so an operator cannot find the descriptor "+
			"that took it\nfull: %s", strangerPath, moved.Error())
	}
	if err := bfdFDAlive(number); err != nil {
		t.Fatalf("the stranger's descriptor %d was CLOSED by the refused late release (%v) — that is the defect, not a refusal", number, err)
	}
}

// bfdRawFixture opens a real file and returns (owner, borrowed) where borrowed is
// a second descriptor onto the same file — the shape every production site here
// has: the wrapper names a number this process did not open.
func bfdRawFixture(t *testing.T) (*os.File, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "borrowed.fd")
	owner, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the fixture file: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown
	return owner, bfdRawDup(t, owner)
}

// bfdRawPipeFixture returns (owner, borrowed) where borrowed is a SECOND
// descriptor onto a pipe, placed at or above daemonlifetime.InheritFDFloor —
// the production shape of a death pipe: the holder keeps the write end, and the
// daemon is handed the read end's NUMBER through the environment.
func bfdRawPipeFixture(t *testing.T) (*os.File, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("death-pipe fixture: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore test fixture teardown
	if err := r.Close(); err != nil {
		t.Fatalf("release the fixture's own read end: %v", err)
	}
	read, _, errno := syscall.Syscall(syscall.SYS_FCNTL, w.Fd(), uintptr(syscall.F_DUPFD), uintptr(daemonlifetime.InheritFDFloor))
	if errno != 0 {
		t.Fatalf("place the fixture's read end at or above fd %d: %v", daemonlifetime.InheritFDFloor, errno)
	}
	return w, int(read)
}

// TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive guards the
// cli_daemon_spawn.go death-pipe site (BORROWED).
//
// The production shape: daemonDeathPipe wraps the number the HOLDER stated in
// daemonlifetime.HoldEnvName and returns the wiring slot. The deferring caller
// hands that slot to os/exec, and this process then has nothing left pointing at
// the wrapper — so the wrapper is the only thing between the holder's descriptor
// and the collector. Its finalizer closing the number is invisible here and fatal
// in the child: the daemon starts with a closed/unbound death pipe, i.e. it
// silently outlives the harness the binding exists to bind it to.
//
// The fixture is a PIPE read end at or above daemonlifetime.InheritFDFloor, which
// is the site's own admission contract: daemonDeathPipe validates the number's
// shape before wrapping it, so a lower or non-pipe number is refused on purpose
// and would never reach the wrap this guard measures.
//
// RED on the base commit (no detach): the descriptor is closed within the
// finalizer window. GREEN with borrowedfd.Detach.
func TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive(t *testing.T) {
	_, borrowed := bfdRawPipeFixture(t)
	// OWNERSHIP: daemonDeathPipe DETACHES the wrapper (borrowedfd.Detach) instead of
	// closing it, so the number stays open and this fixture remains its only
	// releaser — released through the instrument, which asks the kernel first
	// whether it is still the number that was claimed.
	borrowedClaim := bfdClaimIdentity(t, borrowed)
	t.Cleanup(func() { bfdReleaseOwn(t, borrowedClaim) })

	t.Setenv(daemonlifetime.HoldEnvName, strconv.Itoa(borrowed))

	death, err := daemonDeathPipe()
	if err != nil {
		t.Fatalf("daemonDeathPipe refused a live descriptor: %v", err)
	}
	if death == nil {
		t.Fatalf("%s=%d was set but daemonDeathPipe returned no wrapper — this guard needs the wiring slot",
			daemonlifetime.HoldEnvName, borrowed)
	}
	if int(death.Fd()) != borrowed {
		t.Fatalf("daemonDeathPipe wrapped fd %d, want %d", death.Fd(), borrowed)
	}
	// THE PRODUCTION DROP: the caller's only reference goes away.
	death = nil
	borrowedfd.SettleFinalizers()

	if err := bfdFDAlive(borrowed); err != nil {
		t.Fatalf("death-pipe descriptor %d was CLOSED under its holder (%v) after the wiring slot was dropped — "+
			"os.NewFile's finalizer closed a descriptor this process never opened, and the started daemon would then watch a dead "+
			"number (or a recycled one) instead of its spawner's pipe. daemonDeathPipe must hand the wrapper to borrowedfd.Detach",
			borrowed, err)
	}
}

// TestWorkerDaemonInheritLockSuccessKeepsTheBorrowedNumberLive is the OTHER half
// of the same classification: the SUCCESS exit deliberately does NOT detach,
// because it hands the wrapper to the daemon, which keeps it as its own handle for
// the process's whole life (workerDaemonLockRef) — the flock IS the
// single-instance guard, and closing it would release the guard silently.
//
// It also pins that the successful adoption is not weakened by the refusal-path
// treatment: the number is still open and still denotes the parent's lock file.
func TestWorkerDaemonInheritLockSuccessKeepsTheBorrowedNumberLive(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), workerLockFilename)
	owner, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the lock fixture: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown
	borrowed := bfdRawDup(t, owner)
	// OWNERSHIP: this number is HANDED TO THE PRODUCTION HOLDER below
	// (workerDaemonLockRef), which keeps the wrapper for the daemon's whole life —
	// so this fixture must not close it by number. It is released through that
	// holder at teardown, with the identity checked first: a raw syscall.Close here
	// is a late close on a number nothing guarantees is still ours.
	borrowedClaim := bfdClaimIdentity(t, borrowed)

	lock, err := workerDaemonInheritLockAt(borrowed)
	if err != nil {
		t.Fatalf("an uncontended inherited lock was refused: %v", err)
	}
	if lock == nil {
		t.Fatalf("an uncontended inherited lock returned no handle")
	}
	if int(lock.Fd()) != borrowed {
		t.Fatalf("the adopted handle is on fd %d, want the inherited %d", lock.Fd(), borrowed)
	}
	// THE PRODUCTION RETENTION, not a local: workerDaemonRun hands the handle to
	// runTaskWorkerWithDaemon, which parks it in workerDaemonLockRef for the
	// daemon's whole life. Dropping that reference is a REAL regression — the flock
	// lives on the open file description, so a finalized wrapper would release the
	// single-instance guard silently while the daemon kept sweeping.
	prev := workerDaemonLockRef
	workerDaemonLockRef = lock
	t.Cleanup(func() { workerDaemonLockRef = prev })
	// THE HANDLE IS CAPTURED BY VALUE: the test drops the LOCAL further down (that
	// drop is what makes the package-level ref the only thing holding it), so a
	// closure reading `lock` would find nil at teardown and release nothing —
	// measured: "fd 7 was handed over but the holder is nil".
	adopted := lock
	// Registered after the ref restore so it runs FIRST (LIFO): the holder is
	// released while workerDaemonLockRef still points at it.
	t.Cleanup(func() { bfdReleaseViaHolder(t, adopted, borrowedClaim) })
	lock = nil // the local dies; the package-level reference is what must hold it
	borrowedfd.SettleFinalizers()

	if err := bfdFDAlive(borrowed); err != nil {
		t.Fatalf("the ADOPTED worker lock fd %d was closed although workerDaemonLockRef holds its handle (%v) — "+
			"the flock lives on the open file description, so closing the last copy silently releases the single-instance guard "+
			"and a second daemon could start while the first keeps sweeping", borrowed, err)
	}
}

// workerDaemonSourcePath resolves cli_worker_daemon.go next to this test file.
// The runtime.Caller(0) shape is the established one in this package for locating
// a sibling source file from a test.
func workerDaemonSourcePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller could not resolve this test file's path — the source pin cannot find cli_worker_daemon.go")
	}
	return filepath.Join(filepath.Dir(thisFile), "cli_worker_daemon.go")
}

// TestWorkerDaemonLockSiteSourcePin pins the classification the two guards above
// cannot reach: the `^uintptr(0)` and not-a-usable-file refusals are UNREACHABLE
// with a descriptor that has a live owner (os.NewFile performs no validity check —
// it wraps a free number happily and the failure surfaces later as EBADF from the
// syscall). Their treatment is therefore pinned against the PRODUCTION SOURCE, so
// a later edit that drops the detach from those exits fails here instead of
// passing for lack of a fixture.
func TestWorkerDaemonLockSiteSourcePin(t *testing.T) {
	src, err := os.ReadFile(workerDaemonSourcePath(t))
	if err != nil {
		t.Fatalf("read the worker daemon source: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		"borrowedfd.Detach(inherited)",                           // the refusal exits
		"OWNERSHIP: BORROWED",                                    // the classification, in code
		"return workerDaemonInheritLockAt(daemonControlChildFD)", // the production number stays wired
	} {
		if !strings.Contains(text, want) {
			t.Errorf("cli_worker_daemon.go no longer carries %q — the worker-lock site's classification/treatment was changed without this guard being updated", want)
		}
	}
}

// TestDaemonReadyFDSitesCloseTheirBorrowedWrapper pins the classification of the
// two READINESS sites (cmd/agent/cli_worker_daemon.go workerDaemonNoteReady and
// cli_daemon_spawn.go daemonNote). They are BORROWED but NOT defect sites: each one
// CLOSES the wrapper on every exit, and os.File.Close clears the finalizer
// (os.(*file).close → runtime.SetFinalizer(file, nil)), so the close is explicit
// and lands exactly once on a number the call has finished writing.
//
// The guard asserts that explicit lifecycle from the outside: the readiness byte
// arrives, and the number is then provably gone — i.e. the wrapper did its work
// and released the descriptor instead of being abandoned to the collector.
func TestDaemonReadyFDSitesCloseTheirBorrowedWrapper(t *testing.T) {
	for _, tc := range []struct {
		name string
		note func(fd int)
	}{
		{"daemonNote", func(fd int) { daemonNote(bfdProgName, fd, bfdReadyMsg) }},
		{"workerDaemonNoteReady", func(fd int) { workerDaemonNoteReady() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("readiness pipe: %v", err)
			}
			defer r.Close() //stylegate:ignore test fixture teardown
			borrowed := bfdRawDup(t, w)
			_ = w.Close() //stylegate:ignore the raw dup stands in for the launcher's descriptor

			// OWNERSHIP: the readiness helper takes this number and CLOSES it itself
			// (daemonNote / workerDaemonNoteReady defer f.Close()). The fixture must
			// not close it again — the second close is the late one, and it lands on
			// whatever took the recycled number. The guard proves that below.
			borrowedClaim := bfdClaimIdentity(t, borrowed)

			t.Setenv(daemonReadyEnv, strconv.Itoa(borrowed))

			tc.note(borrowed)

			// The byte must arrive BEFORE the close is asserted: a closed-but-
			// silent handshake would be a start failure the launcher reads as EOF.
			buf := make([]byte, 1)
			if dlErr := r.SetReadDeadline(time.Now().Add(bfdReadyDeadlineWait)); dlErr != nil {
				// Not fatal: the write already happened, so a blocking read still
				// returns it — but say so rather than discarding the error (Rule 15b).
				t.Logf("%s: readiness pipe does not support read deadlines (%v) — reading without one", tc.name, dlErr)
			}
			n, err := r.Read(buf)
			if err != nil || n == 0 {
				t.Fatalf("%s wrote nothing to the readiness descriptor (n=%d, err=%v) — the handshake must be reported, not swallowed (Rule 15b)", tc.name, n, err)
			}
			if err := bfdFDAlive(borrowed); err == nil {
				t.Fatalf("%s left readiness fd %d OPEN — this site must CLOSE its borrowed wrapper explicitly (os.File.Close clears the finalizer), "+
					"otherwise it becomes a drop site whose finalizer closes a descriptor owned by the launcher", tc.name, borrowed)
			}
			// THE WINDOW, constructed: the number is free, so hand it to a stranger and
			// show the fixture's late close cannot reach it.
			bfdExpectLateCloseDefeated(t, borrowedClaim)
		})
	}
}
