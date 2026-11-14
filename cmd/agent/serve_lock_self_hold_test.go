package main

// serve_lock_self_hold_test.go — the SELF-HOLD admission rule, red-proven.
//
// THE DEFECT (measured on this host, 2026-10-17): `gopi task finish-cleanup
// --apply` refused ITSELF — the refusal named pid 2196749 as "another merge
// process is already running" while `pgrep -a -x gopi` listed exactly one
// process during that run, the command itself — and the kernel's own table said
// the same pid held the flock:
//
//	lock-kernel-records: 1 record(s) on inode 25034787:
//	  6: FLOCK ADVISORY WRITE 2196749 103:05:25034787 0 EOF
//
// `--dry-run` (which never takes the lock) worked and `--apply` never did; that
// asymmetry is what located it. The mechanism is flock's own: the lock lives on
// the OPEN FILE DESCRIPTION, so a second open+flock in the SAME process — a face
// that asks for the single-instance lock while an outer step of this process
// already holds it — is refused by its own guard.
//
// Two instruments, both REAL (no synthetic /proc, no load — Rule 27): a flock
// held by THIS process, and a CHILD process holding one (the production shape,
// reusing the serve-lock fixture helper). Both acquisitions are the production
// acquireServeLock, so the assertion is on the verdict a face actually receives.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// selfHoldLockEnv pins the lock into a per-test gopi dir, so a contention here
// can never touch the operator's own serve.lock.
func selfHoldLockEnv(t *testing.T) (path, gopiDir string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(config.GopiDirEnv, dir)
	path, err := serveLockPathResolved()
	if err != nil {
		t.Fatalf("resolve the lock path: %v", err)
	}
	return path, filepath.Dir(path)
}

// TestServeLockSelfHeldIsAdmissionNotRefusal is the epic's primary repro.
//
// PRE-FIX (red): the second acquisition returned errServeLockHeld, and the
// sentence called this very process "another merge process".
// POST-FIX: it is ADMITTED, announced, and the outer flock is untouched.
func TestServeLockSelfHeldIsAdmissionNotRefusal(t *testing.T) {
	path, _ := selfHoldLockEnv(t)

	outer, err := acquireServeLock()
	if err != nil {
		t.Fatalf("the first (outer) acquisition must succeed on a free lock: %v", err)
	}
	defer outer.Close() //stylegate:ignore test release of the outer flock; the assertions have run and the OS reclaims the fd

	// The kernel must AGREE that this process is the holder, otherwise the
	// admission below would be judged against a state this host does not create.
	if self, probed := lockProvenSelfHeld(path); !probed || !self {
		t.Fatalf("precondition: the kernel must prove THIS process holds %s (probed=%v self=%v)", path, probed, self)
	}

	inner, err := acquireServeLock()
	if err != nil {
		if errors.Is(err, errServeLockHeld) {
			t.Fatalf("SELF-HELD LOCK REFUSED: a second acquisition in THIS process must be admitted, not refused — the refusal names this very process as another holder:\n%v", err)
		}
		t.Fatalf("the second acquisition in this process must be admitted, got: %v", err)
	}
	if inner != nil {
		// A NON-LOCKING handle (the ancestor admission's shape): closing it must
		// not drop the outer flock.
		if cerr := inner.Close(); cerr != nil {
			t.Fatalf("closing the admitted handle: %v", cerr)
		}
	}
	if err := syscall.Flock(int(outer.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("the outer flock must still be held after the admission: %v", err)
	}
}

// lockProvenSelfHeld asks the KERNEL (not the stamp) whether this process is the
// lock's holder, through the same evidence the admission rule reads.
func lockProvenSelfHeld(path string) (self bool, probed bool) {
	ev, probed := tools.LockEvidences(path)
	if !probed {
		return false, false
	}
	self = false
	for _, e := range ev {
		if e.PID != os.Getpid() {
			return false, true
		}
		self = true
	}
	return self, true
}

// TestServeLockThirdPartyHolderStillRefuses pins the half that must NOT change:
// merge mutual exclusion. A DIFFERENT process holding the flock is still
// refused, and the refusal names that process.
func TestServeLockThirdPartyHolderStillRefuses(t *testing.T) {
	path, gopiDir := selfHoldLockEnv(t)
	if err := os.MkdirAll(gopiDir, serveLockDirMode); err != nil {
		t.Fatalf("mkdir gopi dir: %v", err)
	}
	ready := filepath.Join(t.TempDir(), "holder.ready")
	holder := startHolderHandshake(t, holderHandshakeOptions{lockPath: path, readyPath: ready})
	t.Cleanup(func() { holder.killAndReap(t) })
	holder.waitReady(t, ready)
	// The STAMP is a diagnostic, not the lock: write the third party's pid so the
	// sentence has a holder to name (production stamps it at acquisition).
	if err := os.WriteFile(path, []byte(strconv.Itoa(holder.pid)), serveLockFileMode); err != nil {
		t.Fatalf("stamp the third-party holder: %v", err)
	}
	_, err := acquireServeLock()
	if err == nil {
		t.Fatalf("a THIRD-PARTY holder (pid %d) must still be refused — mutual exclusion is the lock's whole purpose", holder.pid)
	}
	if !errors.Is(err, errServeLockHeld) {
		t.Fatalf("a third-party contention must be the waitable errServeLockHeld, got %v", err)
	}
	if !strings.Contains(err.Error(), "pid "+strconv.Itoa(holder.pid)) {
		t.Errorf("the refusal must name the third-party holder %d, got:\n%v", holder.pid, err)
	}
	if strings.Contains(err.Error(), "this process") {
		t.Errorf("the refusal must not mention this process (pid %d) as a party — it holds nothing:\n%v", os.Getpid(), err)
	}
}

// TestServeLockRefusalNeverCallsThisProcessAnotherHolder covers the sentence
// half when the STAMP carries this process's pid while a third party keeps the
// flock: the holder text must name the THIRD PARTY, never the reader.
func TestServeLockRefusalNeverCallsThisProcessAnotherHolder(t *testing.T) {
	path, gopiDir := selfHoldLockEnv(t)
	if err := os.MkdirAll(gopiDir, serveLockDirMode); err != nil {
		t.Fatalf("mkdir gopi dir: %v", err)
	}
	// A diagnostic stamp is not the flock: stamp OUR pid while a THIRD PARTY
	// process holds the flock, so the contention is real and legal.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		t.Fatalf("open the lock file: %v", err)
	}
	defer f.Close() //stylegate:ignore test fixture descriptor; no flock is taken on it
	if _, werr := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); werr != nil {
		t.Fatalf("stamp this process's pid: %v", werr)
	}
	ready := filepath.Join(t.TempDir(), "holder.ready")
	holder := startHolderHandshake(t, holderHandshakeOptions{lockPath: path, readyPath: ready})
	t.Cleanup(func() { holder.killAndReap(t) })
	holder.waitReady(t, ready)

	refusal := serveLockContentionError(path, f)
	msg := refusal.Error()
	if !errors.Is(refusal, errServeLockHeld) {
		t.Fatalf("a real contention must still wrap errServeLockHeld, got %v", refusal)
	}
	if strings.Contains(msg, "held by pid "+strconv.Itoa(os.Getpid())) {
		t.Errorf("the sentence must not name THIS process (pid %d) as the holder — the flock belongs to pid %d:\n%s", os.Getpid(), holder.pid, msg)
	}
	if !strings.Contains(msg, strconv.Itoa(holder.pid)) {
		t.Errorf("the sentence must name the third-party holder %d:\n%s", holder.pid, msg)
	}
}
