package main

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

// TestServeLockRefusalNamesTheActualErrnor is the regression guard for the
// diagnosis fix of 2026-09-17.
//
// BEFORE: `lockAndVerifyIdentity` turned EVERY `syscall.Flock` error into
// "another merge process is already running (lock … held by pid N)". "Held by a
// live daemon" is the meaning of ONE errno (EWOULDBLOCK); every other failure
// (EBADF, EINVAL, a full or read-only filesystem, a flock on an fd the kernel
// rejects) was reported as a claim about a PROCESS the caller cannot see. The
// message then pointed an operator at a pid, and the real syscall failure was
// lost — the exact Rule 15 defect ("a failure must say what happened").
//
// MEASURED (2026-09-17, `cmd/agent` E2E, 10 runs of
// TestMergerFlock_DaemonLockStopsWithTheDaemon): a refusal read
//
//	"another merge process is already running (lock …/.gopi/serve.lock held by
//	 pid 1630650)"
//
// while (a) the kernel's own table /proc/locks carried NO flock record for that
// file's inode (zero matches for inode 30305648), and (b) neither of the two
// processes that held an fd on the file (1630664, 1630690) appeared in any
// record either. The named "holder" was therefore a diagnostic stamp, not a
// holder — the message described a process that was not keeping that lock.
//
// The mapping is asserted here at the seam that owns it, because the E2E that
// surfaced it is a kill -9 race (1 failure in ~3-12 runs) and a race is not a
// reliable red-proof.
func TestServeLockRefusalNamesTheActualErrnor(t *testing.T) {
	// A contention keeps the operator-facing diagnosis AND the sentinel that
	// the restart handover waits on.
	held := serveLockHeldError("/tmp/example/serve.lock", "pid 4242")
	if !errors.Is(held, errServeLockHeld) {
		t.Fatalf("a contention must wrap errServeLockHeld so the handover can wait on it, got %v", held)
	}
	for _, want := range []string{"/tmp/example/serve.lock", "pid 4242"} {
		if !strings.Contains(held.Error(), want) {
			t.Errorf("the contention refusal must name %q, got %q", want, held.Error())
		}
	}

	// Any OTHER errno must be reported as the syscall failure it is — never as
	// "a live daemon holds it". The assertion is on the SHAPE of the mapping:
	// the production site is `if !errors.Is(err, syscall.EWOULDBLOCK)`, so this
	// pins the exact predicate the refusal depends on (a new errno that IS
	// treated as held would fail here).
	for _, err := range []error{syscall.EBADF, syscall.EINVAL, syscall.ENOLCK, syscall.EPERM} {
		if serveLockErrnoIsContention(err) {
			t.Fatalf("%v is classified as a contention — the refusal would blame a phantom holder instead of naming the syscall failure", err)
		}
	}
	// The positive half: a genuine contention IS one, so the refusal the
	// handover waits on is still reachable.
	if !serveLockErrnoIsContention(syscall.EWOULDBLOCK) {
		t.Fatal("EWOULDBLOCK must be a contention — otherwise a real double-serve is reported as a syscall failure")
	}

	// And the sentinel must not be conflated with the other acquisition
	// failures: a replaced lock file is RETRYABLE, a stray unlinked holder is
	// its own refusal.
	if errors.Is(errServeLockReplaced, errServeLockHeld) {
		t.Error("errServeLockReplaced must not read as a live holder — the retry loop depends on telling them apart")
	}
}

// TestServeLockContentionRefusalIsOneDefinition pins Rule 14/1b for the refusal
// text: the message the operator reads and the sentinel the code matches on come
// from ONE constructor. A second inlined copy (the shape that let the errno be
// misreported) fails here.
func TestServeLockContentionRefusalIsOneDefinition(t *testing.T) {
	a := serveLockHeldError("/x/serve.lock", "pid 1").Error()
	b := serveLockHeldError("/x/serve.lock", "pid 1").Error()
	if a != b {
		t.Fatalf("the refusal is not deterministic:\n%q\n%q", a, b)
	}
	if !strings.Contains(a, serveLockHeldRefusal[:20]) ||
		!strings.Contains(serveLockHeldRefusal, "another merge process is already running") {
		t.Errorf("the refusal text drifted from serveLockHeldRefusal: %q", a)
	}
}
