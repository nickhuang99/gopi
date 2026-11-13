// serve_lock_replacement_test.go — the MECHANISM behind the 2026-09-17 refusal
// that had no holder: "another merge daemon is already running (lock
// ~/.gopi/serve.lock held by pid N)" while the kernel's own table
// (/proc/locks) carried NO record for the lock file's inode, and the two
// processes holding a descriptor on that inode were not the named pid.
//
// Two hypotheses were left open by fix/cmd-agent-suite-red (c50e7f2a):
//
//	(a) the lock FILE was REPLACED (inode swapped) under the acquirer, so the
//	    refusal was about a race with the replacement, not a live holder;
//	(b) a NON-EWOULDBLOCK errno was mis-reported as contention.
//
// (b) is fixed by serveLockErrnoIsContention and is proven here as a positive
// control: the flock that a REPLACEMENT leaves behind reports the one errno
// that means contention (EWOULDBLOCK), so an errno mis-read has nothing to
// mis-report in that scenario. (a) is measured directly, in this file, with an
// injected replacement — the deterministic unit-level repro the epic asked for.
//
// What the measurements establish (each asserted below, with the kernel's own
// table quoted on failure):
//
//  1. A flock is NOT invalidated by an unlink/replace of the path: the kernel
//     keeps the lock (and its /proc/locks record) on the OLD inode. Two
//     writers can therefore coexist — the holder keeps merging, the successor
//     wins the FRESH inode's free lock.
//  2. The holder's DESCRIPTOR no longer names the file the path resolves to.
//     That state has its own name in this codebase — REPLACEMENT — and
//     sameFileAsPath is what tells it apart from a genuine holder (both the
//     true and the false direction are asserted here, so the check is pinned
//     with its control).
//  3. A flocked descriptor DUPLICATED into another process (os/exec
//     ExtraFiles — the exact mechanism the merger daemon face uses) keeps the
//     flock alive after its OWNER DIES, and the successor's refusal then
//     reports EWOULDBLOCK while /proc/locks names a pid that no longer exists.
//     A refusal message built from that table therefore points at a dead pid:
//     a diagnostic artifact, never evidence of a live holder.
//
// Zero load, no thresholds, no timing races (Rule 27): every process here is
// either a child of this test or is killed by its explicit pid.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// lockRaceChildFd is the descriptor number a helper receives through
	// os/exec's ExtraFiles (ExtraFiles is numbered from 3).
	lockRaceChildFd = 3
	// lockRaceReadyTimeout / lockRaceReadyTick bound the readiness handshake
	// with a helper process (never a sleep long enough to be a race).
	lockRaceReadyTimeout = 15 * time.Second
	lockRaceReadyTick    = 25 * time.Millisecond
	// lockRaceFreeTimeout bounds the wait for the flock to become acquirable
	// after the last holder dies: a kernel-side release, not work.
	lockRaceFreeTimeout = 5 * time.Second
	// lockRaceChildHoldSec is how long the duplicated-descriptor holder lives
	// (bounded; the test kills it by pid before it expires).
	lockRaceChildHoldSec = 60
)

// The helper roles, driven entirely through the environment (the helper body
// under a normal `go test` run sees no mode and skips).
const (
	lockRaceModeEnv   = "GOPI_TEST_LOCK_RACE_MODE"
	lockRacePathEnv   = "GOPI_TEST_LOCK_RACE_PATH"
	lockRaceMarkerEnv = "GOPI_TEST_LOCK_RACE_MARKER"
	lockRaceModeOwner = "owner"
	lockRaceModeHold  = "hold"

	// procLocksKernelTable is the kernel's own lock table — the file the
	// c50e7f2a diagnosis was read from, and the instrument this file uses to
	// separate "a live holder" from "a replacement".
	procLocksKernelTable = "/proc/locks"
)

// lockRaceGopiDir pins a private profile and returns the lock path inside it —
// the same shape every serve-lock test in this package uses.
func lockRaceGopiDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)            // acquireServeLock resolves ~/.gopi/serve.lock
	t.Setenv(config.GopiDirEnv, dir) // …and GOPI_HOME outranks HOME, so pin it too
	gopiDir := filepath.Join(dir, ".gopi")
	if err := os.MkdirAll(gopiDir, serveLockDirMode); err != nil {
		t.Fatalf("create %s: %v", gopiDir, err)
	}
	return gopiDir
}

// lockRaceInode reports the inode the path resolves to right now.
func lockRaceInode(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat %s: no stat_t", path)
	}
	return st.Ino
}

// lockRaceKernelRecords returns the kernel's FLOCK records naming the given
// inode — the evidence "this flock is still held by THIS pid", straight from
// the table the refusal was diagnosed against.
func lockRaceKernelRecords(t *testing.T, inode uint64) []string {
	t.Helper()
	b, err := os.ReadFile(procLocksKernelTable)
	if err != nil {
		t.Fatalf("read %s: %v (the mechanism cannot be named without the kernel's own table)", procLocksKernelTable, err)
	}
	want := strconv.FormatUint(inode, 10)
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		// <id>: FLOCK ADVISORY WRITE <pid> <maj>:<min>:<ino> <start> <end>
		if len(fields) < 8 {
			continue
		}
		parts := strings.Split(fields[5], ":")
		if len(parts) == 3 && parts[2] == want {
			out = append(out, strings.Join(fields, " "))
		}
	}
	return out
}

// TestServeLockReplacementMechanism measures hypothesis (a) end to end: the
// lock file is REPLACED under a process that holds the flock. Every step is an
// assertion, so a host where the kernel behaved differently would report the
// mechanism as unproven instead of passing silently.
func TestServeLockReplacementMechanism(t *testing.T) {
	path := filepath.Join(lockRaceGopiDir(t), serveLockFilename)

	// The holder: open + flock the lock the way EVERY acquisition path does.
	own, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	t.Cleanup(func() { _ = own.Close() }) //stylegate:ignore test teardown of the held lock; the assertions have run
	if err := syscall.Flock(int(own.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock: %v", err)
	}
	before := lockRaceInode(t, path)

	// POSITIVE CONTROL for the identity check, taken BEFORE any replacement:
	// the descriptor and the name are the same file.
	if ok, err := sameFileAsPath(own, path); err != nil || !ok {
		t.Fatalf("sameFileAsPath before the replacement = %v (err=%v), want true — the check's own control must hold", ok, err)
	}
	// INSTRUMENT VALIDATION: a HELD flock really is recorded (with its inode) in
	// the kernel's table on this host, so an EMPTY record for a lock we hold
	// would not be a missing-feature artifact.
	held := lockRaceKernelRecords(t, before)
	if len(held) == 0 {
		t.Fatalf("the kernel records no FLOCK for the inode %d we hold — %s cannot witness a held lock on this host", before, procLocksKernelTable)
	}
	if !strings.Contains(strings.Join(held, " "), " "+strconv.Itoa(os.Getpid())+" ") {
		t.Fatalf("the kernel's record for the held lock does not name our pid %d: %v", os.Getpid(), held)
	}

	// THE REPLACEMENT. It needs no privilege and no cooperation from the
	// holder: unlink is a directory operation, and no flock syscall is
	// invalidated by it.
	if err := os.Remove(path); err != nil {
		t.Fatalf("unlink the held lock: %v", err)
	}
	if err := os.WriteFile(path, []byte("0000000"), serveLockFileMode); err != nil {
		t.Fatalf("recreate the lock path: %v", err)
	}
	after := lockRaceInode(t, path)
	if after == before {
		t.Fatalf("the recreate reused inode %d — this host cannot demonstrate a replacement here", before)
	}

	// ① The flock we hold SURVIVED the replacement: the kernel still records it
	//    on the OLD inode. This is why "no record for the path's inode" does not
	//    mean "nobody holds the lock".
	if rec := lockRaceKernelRecords(t, before); len(rec) == 0 {
		t.Errorf("the held flock vanished from %s after the unlink — the replacement would then be a live-holder situation: %v", procLocksKernelTable, rec)
	}

	// ② The descriptor no longer names what the path resolves to. This state is
	//    named REPLACEMENT (not "held"), and sameFileAsPath is the one check
	//    that reports it — pinned here in the negative direction, with ①'s
	//    positive control above.
	if ok, err := sameFileAsPath(own, path); err != nil || ok {
		t.Fatalf("sameFileAsPath after the replacement = %v (err=%v), want false — the replacement must be distinguishable from a holder", ok, err)
	}

	// ③ The kernel grants the successor the fresh inode's flock: no contention
	//    exists for it, which is the "two writers coexist" hazard the identity
	//    checks exist to catch.
	succ, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("successor open: %v", err)
	}
	ferr := syscall.Flock(int(succ.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	_ = succ.Close() //stylegate:ignore the successor's probe descriptor; its flock result is already captured
	if ferr != nil {
		t.Fatalf("the successor's flock on the replaced path failed with %v — on this host a replacement DID refuse the successor, which would make the refusal a contention after all", ferr)
	}

	// ④ The production acquisition, in this exact state. It must NOT report
	//    contention (that refusal belongs to EWOULDBLOCK alone); it must report
	//    the REPLACEMENT/unlinked-holder state — the refusal that carries no
	//    live holder and no kernel record for the path's inode, i.e. the
	//    observable the 2026-09-17 run was chasing.
	_, aerr := acquireServeLock()
	if aerr == nil {
		t.Fatalf("acquireServeLock succeeded while a live process (this one, pid %d) still holds the unlinked lock — the double-merge guard did not fire", os.Getpid())
	}
	if errors.Is(aerr, errServeLockHeld) {
		t.Errorf("acquireServeLock reported a LIVE HOLDER, but the lock's live holder is gone from every name and no kernel record names this process for the path's inode; the state is a replacement: %v", aerr)
	}
	if !strings.Contains(aerr.Error(), "UNLINKED copy") {
		t.Errorf("acquireServeLock must refuse through the stray-holder/replacement arm, got: %v", aerr)
	}
	// ⑤ The refusal must CARRY THE DIAGNOSIS, not only the sentence (bug row
	//    01a14252, recommendation ②): the contention arm has always appended the
	//    three labelled lines, while this arm — the one an operator hits after
	//    an unlink, where the sentence's own advice ("restores the lock on its
	//    next pass, or kill pid N") is exactly what the run measured as
	//    NON-ACTIONABLE — printed the sentence alone. Same refusal state, same
	//    three questions, so the same three lines: who the stamp names (and
	//    whether it is alive), who holds an unlinked copy, and what the kernel
	//    records for the inode the acquirer was refused on.
	for _, want := range []string{serveLockHolderPrefix, serveLockStrayHolderPrefix, serveLockKernelRecordsPrefix} {
		if !strings.Contains(aerr.Error(), want) {
			t.Errorf("the stray-holder refusal is missing the %q line — the operator gets a sentence with no diagnosis:\n%s", want, aerr)
		}
	}
	// The stray-holder line must name THIS process: it is the live holder of the
	// unlinked copy, so a line that named someone else would be a second wrong
	// pointer in the same refusal.
	if !strings.Contains(aerr.Error(), strconv.Itoa(os.Getpid())) {
		t.Errorf("the refusal's diagnosis does not name the live unlinked holder (pid %d):\n%s", os.Getpid(), aerr)
	}
}

// TestServeLockFlockOutlivesItsInheritedCopy pins the flock semantics that make
// the 2026-09-17 signature possible: a flocked descriptor HANDED TO ANOTHER
// PROCESS (os/exec ExtraFiles — what the merger daemon face does) keeps the
// flock alive after its owner dies, and the successor's refusal then reports
// EWOULDBLOCK while the kernel's own table names a pid that no longer exists.
//
// That is the whole reason /proc/locks may be quoted only with the pid's
// liveness beside it, and the reason hypothesis (b) alone could not explain the
// observation.
func TestServeLockFlockOutlivesItsInheritedCopy(t *testing.T) {
	dir := lockRaceGopiDir(t)
	path := filepath.Join(dir, serveLockFilename)
	marker := filepath.Join(dir, "lock-race-holder.marker")

	owner := exec.Command(os.Args[0], "-test.run=TestServeLockRaceHelperProcess")
	owner.Env = append(os.Environ(),
		lockRaceModeEnv+"="+lockRaceModeOwner,
		lockRacePathEnv+"="+path,
		lockRaceMarkerEnv+"="+marker,
	)
	if err := owner.Start(); err != nil {
		t.Fatalf("start the owner helper: %v", err)
	}
	ownerPid, holderPid := lockRaceWaitMarker(t, marker)
	t.Cleanup(func() { lockRaceKill(t, holderPid) })
	if err := owner.Wait(); err != nil {
		t.Fatalf("the owner helper must exit cleanly (it leaves the flock to its duplicated copy): %v", err)
	}
	if mdProcAlive(ownerPid) {
		t.Fatalf("the owner pid %d is still alive — the fixture needs it DEAD for the stale-record measurement", ownerPid)
	}
	// RIG CHECK: the holder really does carry the duplicated descriptor on the
	// lock. Without it the flock would have died with the owner and this test
	// would prove the opposite of what it claims.
	if !lockRaceHolderCarriesFd(t, path, holderPid) {
		t.Fatalf("the holder pid %d carries no descriptor on %s — the rig is not armed", holderPid, path)
	}

	// ① The flock outlives its owner: the successor is REFUSED, with the ONE
	//    errno that means contention — measured, never assumed.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("successor open: %v", err)
	}
	ferr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	_ = f.Close() //stylegate:ignore the refused probe descriptor; the errno is the measurement
	if !errors.Is(ferr, syscall.EWOULDBLOCK) {
		t.Fatalf("the successor's flock = %v, want EWOULDBLOCK — an inherited copy must keep the lock", ferr)
	}

	// ② …and the kernel's table names the DEAD owner. A refusal message built
	//    from that table therefore points at a process that no longer exists:
	//    the stale-pid artifact c50e7f2a measured, and the reason a pid from
	//    this table is not by itself a live holder.
	rec := lockRaceKernelRecords(t, lockRaceInode(t, path))
	if len(rec) == 0 {
		t.Fatalf("the kernel records nothing for the inode while the successor is refused EWOULDBLOCK — the two instruments disagree")
	}
	if !strings.Contains(strings.Join(rec, " "), " "+strconv.Itoa(ownerPid)+" ") {
		t.Errorf("the kernel's record %v does not name the (dead) owner pid %d — the stale-record artifact is not reproduced", rec, ownerPid)
	}

	// ③ Release: the duplicated copy dies, the flock goes with it, and the
	//    kernel's records disappear. This closes the loop — the refusal in ①
	//    was the holder's flock and nothing else.
	lockRaceKill(t, holderPid)
	if !lockRaceWaitAcquirable(t, path) {
		t.Fatalf("the path never became flockable after the duplicated holder was killed — the flock outlived every holder")
	}
	if rec := lockRaceKernelRecords(t, lockRaceInode(t, path)); len(rec) != 0 {
		t.Errorf("the kernel still records %v after the last holder died", rec)
	}
}

// TestServeLockRaceHelperProcess is not a test: it is the helper body the two
// roles above re-exec (it skips unless the mode env is set).
//
//	owner — takes the flock, duplicating its descriptor into a child through
//	        os/exec ExtraFiles, records both pids, and EXITS. The child inherits
//	        the flocked open file description.
//	hold  — owns nothing; it exists to keep the inherited descriptor (fd 3)
//	        open, which is the only thing keeping the flock alive.
func TestServeLockRaceHelperProcess(t *testing.T) {
	mode := os.Getenv(lockRaceModeEnv)
	if mode == "" {
		t.Skip("helper process body — driven by the serve-lock replacement tests")
	}
	path := os.Getenv(lockRacePathEnv)
	switch mode {
	case lockRaceModeOwner:
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
		if err != nil {
			t.Fatalf("helper owner open: %v", err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatalf("helper owner flock: %v", err)
		}
		self := exec.Command(os.Args[0], "-test.run=TestServeLockRaceHelperProcess")
		self.Env = append(os.Environ(), lockRaceModeEnv+"="+lockRaceModeHold)
		self.ExtraFiles = []*os.File{f} // the duplicated descriptor (fd 3 in the child)
		if err := self.Start(); err != nil {
			t.Fatalf("helper owner spawn holder: %v", err)
		}
		marker := fmt.Sprintf("owner=%d holder=%d", os.Getpid(), self.Process.Pid)
		if err := os.WriteFile(os.Getenv(lockRaceMarkerEnv), []byte(marker), 0o644); err != nil {
			t.Fatalf("helper owner marker: %v", err)
		}
		// EXIT WITHOUT CLOSING: this is the point of the fixture — the flock
		// must survive in the copy the child holds.
		os.Exit(0)
	case lockRaceModeHold:
		time.Sleep(lockRaceChildHoldSec * time.Second) //stylegate:allow-sleep the holder's whole purpose is to keep the inherited descriptor open while the probe runs
	}
}

// lockRaceWaitMarker waits for the owner's marker file and returns the pids it
// recorded (bounded; a missing marker is a failed rig, never a skipped check).
func lockRaceWaitMarker(t *testing.T, marker string) (ownerPid, holderPid int) {
	t.Helper()
	deadline := time.Now().Add(lockRaceReadyTimeout)
	for {
		b, err := os.ReadFile(marker)
		if err == nil {
			var o, h int
			if _, serr := fmt.Sscanf(string(b), "owner=%d holder=%d", &o, &h); serr == nil && o > 0 && h > 0 {
				return o, h
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the owner helper never recorded its pids in %s within %s", marker, lockRaceReadyTimeout)
		}
		time.Sleep(lockRaceReadyTick) //stylegate:allow-sleep poll tick of the helper's readiness handshake
	}
}

// lockRaceHolderCarriesFd reports whether pid has a descriptor whose target is
// the lock path — read from /proc, the only authoritative answer.
func lockRaceHolderCarriesFd(t *testing.T, path string, pid int) bool {
	t.Helper()
	links, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		t.Fatalf("read the holder's fd table: %v", err)
	}
	for _, l := range links {
		target, rerr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", l.Name()))
		if rerr != nil {
			continue
		}
		if target == path {
			return true
		}
	}
	return false
}

// lockRaceKill SIGKILLs an explicit pid (never a pattern).
//
// THE PID COMES OUT OF A MARKER FILE the helper wrote (`lockRaceWaitMarker`), so
// it is a number read from a filesystem, not a handle this test holds — exactly
// the shape Rule 42 refuses to deliver to blind. The incarnation is therefore
// read at delivery time and the guard decides; a pid that has already been
// recycled is left alone, which is the correct outcome for a teardown.
func lockRaceKill(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	id, err := testutil.ReadProcIdentity(pid)
	if err != nil {
		return // already gone: there is no incarnation left to address
	}
	_ = testutil.KillIfIdentical(id, syscall.SIGKILL) //stylegate:ignore a best-effort teardown kill; the assertions read the kernel, never this return
}

// lockRaceWaitAcquirable waits, bounded, until the path is flockable — the
// successor's own operation, so the wait observes the fact the check is about.
func lockRaceWaitAcquirable(t *testing.T, path string) bool {
	t.Helper()
	deadline := time.Now().Add(lockRaceFreeTimeout)
	for {
		f, err := os.OpenFile(path, os.O_RDWR, serveLockFileMode)
		if err == nil {
			ferr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if ferr == nil {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //stylegate:ignore flock on a fixture file; the lock state read afterwards is the assertion
			}
			_ = f.Close() //stylegate:ignore the probe descriptor, released immediately above
			if ferr == nil {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(lockRaceReadyTick) //stylegate:allow-sleep bounded poll for a kernel-side flock release
	}
}
