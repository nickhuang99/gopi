package testutil

// The guard for the readiness site in lifetimeprobe.go.
//
// writeReadiness wraps a descriptor the CALLER named in the environment (this
// process never opened it: BORROWED) and then CLOSES it. That close is correct —
// os.File.Close clears the finalizer (os.(*file).close → runtime.SetFinalizer(file,
// nil), GOROOT/src/os/file_unix.go), so the descriptor is released exactly once,
// after the word is written — and this guard asserts that lifecycle from the
// outside, which is the property that keeps the site out of the drop-the-wrapper
// family.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/borrowedfd"
)

const (
	// readyProbeWord is the word written through the readiness descriptor.
	readyProbeWord = "R"
	// readyProbeWait bounds the read of that word.
	readyProbeWait = 5 * time.Second
)

// fdOpen reports whether n is an open descriptor, asked of the KERNEL (F_GETFD:
// 0 = open, EBADF = closed) rather than of any wrapper.
func fdOpen(n int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(n), uintptr(syscall.F_GETFD), 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// TestWriteReadinessClosesItsBorrowedDescriptor drives the production function
// through its real seam: the fd number is stated in lifetimeProbeReadyEnv (the
// same variable a caller uses), the word must arrive on the pipe, and the
// descriptor must then be gone — written once, released once.
func TestWriteReadinessClosesItsBorrowedDescriptor(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("readiness pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() }) //stylegate:ignore test fixture teardown
	dup, err := syscall.Dup(int(w.Fd()))
	if err != nil {
		t.Fatalf("dup the readiness write end: %v", err)
	}
	_ = w.Close() //stylegate:ignore the raw dup stands in for the caller's descriptor

	// OWNERSHIP: the dup is claimed by this fixture and then HANDED TO THE
	// PRODUCTION PATH, which releases it (writeReadiness closes its own wrapper).
	// The fixture therefore does NOT close it: a raw syscall.Close here would be a
	// SECOND close on a number the kernel has already recycled, so it would release
	// whichever descriptor took the number next. The identity is recorded at CLAIM
	// time so the late judgement can compare across the transfer.
	claim, err := borrowedfd.IdentityOf(dup)
	if err != nil {
		t.Fatalf("record the identity of the readiness fd %d: %v", dup, err)
	}
	t.Setenv(lifetimeProbeReadyEnv, strconv.Itoa(dup))

	writeReadiness(readyProbeWord)

	buf := make([]byte, len(readyProbeWord))
	if err := r.SetReadDeadline(time.Now().Add(readyProbeWait)); err != nil {
		t.Fatalf("set the readiness read deadline: %v", err)
	}
	n, err := r.Read(buf)
	if err != nil && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("read the readiness word: %v", err)
	}
	if got := string(buf[:n]); got != readyProbeWord {
		t.Fatalf("writeReadiness wrote %q, want %q — the readiness word is the whole point of the descriptor (Rule 15b: a swallowed handshake is a silent failure)", got, readyProbeWord)
	}
	if err := fdOpen(dup); err == nil {
		t.Fatalf("writeReadiness left readiness fd %d OPEN — this site must CLOSE its borrowed wrapper explicitly "+
			"(os.File.Close clears the finalizer); abandoning it turns a tidy one-shot write into a drop site whose "+
			"finalizer closes a descriptor the caller owns", dup)
	}

	// THE WINDOW, constructed rather than waited for (Rule 27): the number the
	// fixture used to hold is now free, so hand it to a STRANGER on purpose. This is
	// the exact state a late fixture close finds in a whole-package run — and the
	// guarded release must refuse it instead of releasing the stranger.
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
	// MEASURED: open() takes the LOWEST free number, so it can take the recycled
	// number by itself; dup'ing in that case would land on the NEXT free one.
	if strangerFd != dup {
		got, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(strangerFd), uintptr(syscall.F_DUPFD), uintptr(dup))
		if errno != 0 {
			t.Fatalf("hand the freed number %d to a stranger: %v", dup, errno)
		}
		// A raw number, closed here, on a window nothing else can open: no wrapper
		// exists yet, so no finalizer can close it a second time.
		if closeErr := syscall.Close(strangerFd); closeErr != nil {
			t.Fatalf("release the stranger's original raw handle after renumbering it: %v", closeErr)
		}
		if int(got) != dup {
			t.Fatalf("the stranger landed on fd %d, not the recycled %d — the window this guard needs is not open", got, dup)
		}
		strangerFd = int(got)
	}
	strangerClaim, err := borrowedfd.IdentityOf(strangerFd)
	if err != nil {
		t.Fatalf("record the stranger's identity on fd %d: %v", dup, err)
	}
	t.Cleanup(func() {
		// The stranger is the only holder of the number, so releasing it is safe.
		if relErr := strangerClaim.Release(); relErr != nil {
			t.Errorf("releasing the stranger's own descriptor %d was refused: %v", dup, relErr)
		}
	})

	// THE LATE CLOSE, defeated. Had the fixture kept its raw syscall.Close, this is
	// the call it would have made — and it would have reported success while
	// releasing the stranger's file.
	lateErr := claim.Release()
	if lateErr == nil {
		t.Fatalf("the fixture's late release of fd %d SUCCEEDED after the number was recycled to %q — the readiness "+
			"fixture closed a descriptor it did not own (writeReadiness already released it), and syscall.Close reports "+
			"success when it does", dup, strangerPath)
	}
	var moved *borrowedfd.MovedError
	if !errors.As(lateErr, &moved) {
		t.Fatalf("the refusal for fd %d is %T (%v), want *borrowedfd.MovedError — a caller must be able to tell "+
			"'refused to close a stranger' from 'the close failed'", dup, lateErr, lateErr)
	}
	if !strings.Contains(moved.Error(), strangerPath) {
		t.Errorf("the refusal does not name what the number denotes now (%q), so an operator cannot find the descriptor "+
			"that took it\nfull: %s", strangerPath, moved.Error())
	}
	if err := fdOpen(dup); err != nil {
		t.Fatalf("the stranger's descriptor %d was CLOSED by the refused late release (%v) — the defect, not a refusal", dup, err)
	}
}
