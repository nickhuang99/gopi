package borrowedfd

// The helper's own smallest-sample guard (Rule 10.4): the retention idiom is
// asserted over ONE borrowed descriptor, and the instrument is CALIBRATED in the
// same file — a guard that cannot see the defect it guards against certifies
// nothing, so the dropped-wrapper arm asserts the CLOSED outcome and the
// detached arm asserts the open one, one line apart.
//
// Everything here asks the KERNEL (fcntl F_GETFD / readlink /proc/self/fd) and
// never the wrapper, because the unfixed code also "intended" to stop using the
// wrapper — its own state is exactly what cannot be trusted.

import (
	"errors"
	"os"
	"path/filepath"

	"strconv"
	"syscall"
	"testing"
)

const (
	// probeBorrowedName names the wrappers this file builds. It is not decoration:
	// os.NewFile's name string is what a future `lsof`/readlink-trail reader sees.
	probeBorrowedName = "borrowedfd-test"
	// procSelfFD (the procfs view of this process's descriptors) is NOT declared
	// here: production release.go defines it once (Rule 14), and this file reads
	// that definition rather than keeping a second copy.
)

// fdAlive asks the KERNEL whether n is an open descriptor: F_GETFD returns 0 for
// an open one and EBADF for a closed one.
func fdAlive(n int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(n), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// dupRaw places f on a FRESH descriptor and returns the raw NUMBER.
//
// It deliberately does NOT return an *os.File: a retained handle produced by
// os.NewFile would itself own the number and mask the very leak under test, and a
// t.Cleanup handle would do the same. The caller releases the number with a raw
// syscall.Close.
func dupRaw(t *testing.T, f *os.File) int {
	t.Helper()
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatalf("dup the fixture descriptor: %v", err)
	}
	return fd
}

// borrowFixture opens a real file and returns (owner, borrowed) where borrowed is
// a second descriptor onto the SAME open file description — i.e. the shape every
// production site has: the wrapper names a descriptor this process did not open.
func borrowFixture(t *testing.T) (*os.File, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "borrowed.fd")
	owner, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the fixture file: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() }) //stylegate:ignore test fixture teardown
	return owner, dupRaw(t, owner)
}

// claimIdentity takes the kernel's reading of a number this fixture has just
// claimed, while the number is still ours — the value a later release compares
// against (release.go).
//
// It is taken at CLAIM time, never at release time: a comparison whose two sides
// are both taken after the window has opened always agrees, and a guard built
// that way is green for the wrong reason.
func claimIdentity(t *testing.T, n int) Identity {
	t.Helper()
	id, err := IdentityOf(n)
	if err != nil {
		t.Fatalf("record the identity of fixture fd %d: %v", n, err)
	}
	return id
}

// releaseAtTeardown gives the number back the way every raw close in a fixture
// must: it closes it ONLY if the kernel still reports the object this fixture
// claimed, and otherwise refuses by name instead of releasing whatever holds the
// number now.
//
// alreadyClosed is the caller's statement that its own fixture PREMISE is that
// something else released the number (the dropped-wrapper arm measures exactly
// that). Then a refusal saying "it is gone" is the expected outcome, and any
// other refusal — the number alive under a STRANGER — is a failure.
func releaseAtTeardown(t *testing.T, id Identity, alreadyClosed bool) {
	t.Helper()
	err := id.Release()
	if err == nil {
		if alreadyClosed {
			t.Errorf("fd %d was still ours at teardown although this arm's premise is that it had been released — "+
				"the arm measured nothing", id.Number())
		}
		return
	}
	var moved *MovedError
	if !errors.As(err, &moved) {
		t.Errorf("releasing fixture fd %d failed for a reason that is not a refusal: %v", id.Number(), err)
		return
	}
	if !alreadyClosed {
		t.Errorf("%s", moved.Error())
		return
	}
	// The expected shape: the fixture's own premise released it. Say so, and prove
	// the refusal did NOT close a live stranger.
	if err := fdAlive(id.Number()); err == nil {
		t.Errorf("fixture fd %d is OPEN again at teardown and no longer ours — the late close would have released a "+
			"stranger's descriptor; the refusal correctly left it alone (%v)", id.Number(), moved)
	}
}

// TestDetachKeepsABorrowedDescriptorOpen is the guard for the fix: a wrapper the
// code DROPS is detached by Detach, so the collector never closes the number.
func TestDetachKeepsABorrowedDescriptorOpen(t *testing.T) {
	owner, borrowed := borrowFixture(t)
	// OWNERSHIP: this fixture's own number, claimed by dupRaw and never handed to a
	// production path — so it stays ours for the whole arm, and the guarded release
	// closes it. A raw syscall.Close here would be the same number with no question
	// asked before it.
	borrowedClaim := claimIdentity(t, borrowed)
	t.Cleanup(func() { releaseAtTeardown(t, borrowedClaim, false) })

	before := Retained()
	wrapper := os.NewFile(uintptr(borrowed), probeBorrowedName)
	if wrapper == nil {
		t.Fatalf("os.NewFile(%d) returned nil — the probe needs a wrapper", borrowed)
	}
	Detach(wrapper)
	if got := Retained(); got != before+1 {
		t.Fatalf("Detach retained %d wrappers, want %d: the wrapper must stay REACHABLE at package level, "+
			"because a finalizer runs only once its object is unreachable and a local going out of scope is exactly what makes it unreachable", got, before+1)
	}
	// Drop the only LOCAL reference: this is the production drop (the function
	// returns, the variable dies). Go's liveness analysis can end a local at its
	// last use, so the hazard's window opens before any return in real code.
	wrapper = nil
	SettleFinalizers()

	if err := fdAlive(borrowed); err != nil {
		t.Fatalf("the detached wrapper's descriptor %d is CLOSED (%v) — Detach cleared the finalizer but did not retain the wrapper, "+
			"so os.NewFile's (*file).close ran on a descriptor this process never owned", borrowed, err)
	}
	// A stronger fact than "some descriptor is open": the number still denotes the
	// OWNER's file, so a finalizer close would have handed it to a stranger.
	if target, err := os.Readlink(filepath.Join(procSelfFD, strconv.Itoa(borrowed))); err != nil || target != owner.Name() {
		t.Fatalf("descriptor %d no longer names its owner's file: readlink = %q, err = %v (want %q)", borrowed, target, err, owner.Name())
	}
}

// TestDroppedWrapperClosesABorrowedDescriptor CALIBRATES the instrument: the very
// same shape WITHOUT Detach must be observably closed. If this arm ever passes,
// the guard above measures nothing, and the whole file's green means nothing.
//
// It asserts the DEFECT on purpose, so it stays valid on the fixed tree — it is a
// property of os.NewFile and of this host's Go runtime, and it goes red only if
// the runtime stops installing the finalizer, which is information worth having.
func TestDroppedWrapperClosesABorrowedDescriptor(t *testing.T) {
	_, borrowed := borrowFixture(t)
	// THIS ARM'S PREMISE IS THAT THE NUMBER GETS RELEASED: the dropped wrapper's
	// finalizer closes it, which is the defect being calibrated. The fixture
	// therefore must NOT close it afterwards — a raw close here is a SECOND close on
	// a number the kernel may already have recycled, i.e. the very defect this file
	// guards, committed by the guard itself. The guarded release refuses by name and
	// reports which case it was.
	borrowedClaim := claimIdentity(t, borrowed)
	t.Cleanup(func() { releaseAtTeardown(t, borrowedClaim, true) })

	wrapper := os.NewFile(uintptr(borrowed), probeBorrowedName)
	if wrapper == nil {
		t.Fatalf("os.NewFile(%d) returned nil — the probe needs a wrapper", borrowed)
	}
	wrapper = nil // dropped, exactly as a refusal path drops it
	SettleFinalizers()

	if err := fdAlive(borrowed); err == nil {
		t.Fatalf("a DROPPED wrapper left descriptor %d open after %d GC rounds — the instrument cannot see the defect "+
			"(os.NewFile no longer installs a finalizer on this runtime, or the window is too short), so no guard in this package proves anything",
			borrowed, FinalizerSettleRounds())
	}
}

// TestDetachToleratesNil pins the documented nil tolerance: os.NewFile's answer
// for an unusable number is sometimes nil (the `uintptr(0)` sentinel shape), and
// there is then no finalizer to forget — Detach must not panic on the refusal
// paths that pass its result straight in.
func TestDetachToleratesNil(t *testing.T) {
	before := Retained()
	Detach(nil)
	if got := Retained(); got != before {
		t.Fatalf("Detach(nil) retained %d wrappers, want %d: a nil wrapper has no finalizer and must not enter the retention slice", got, before)
	}
}
