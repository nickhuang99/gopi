package terminalgeom

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/nickhuang99/gopi/internal/borrowedfd"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestStandardReportsNoTerminalUnderTestRun is the reason the unknown state
// exists, asserted where it actually holds: a `go test` process has PIPES for
// stdout/stderr/stdin, so there is no terminal to measure and the answer must be
// UNKNOWN-with-a-reason. A zero, or a silent "measured 0x0", is the shape this
// assertion forbids.
//
// The premise (this test binary really has no tty) is checked rather than
// assumed: run under a terminal-attached harness the assertion would be about
// the harness, so the case names what it found and skips nothing silently.
func TestStandardReportsNoTerminalUnderTestRun(t *testing.T) {
	// os.Stdout under `go test` is a pipe (or /dev/null with -v redirected);
	// TerminalCols says 0 for anything the kernel will not answer TIOCGWINSZ
	// for, which is the premise this case needs.
	if cols, rows := terminalSize(os.Stdout); cols > 0 && rows > 0 {
		t.Skipf("this test binary has a real terminal on stdout (%dx%d) — the no-terminal assertion is not about this run", cols, rows)
	}
	got := Standard()
	switch {
	case got.Known():
		t.Fatalf("Standard() claims a measurement (%+v) although no standard descriptor is a terminal", got)
	case got.State != sessionevent.GeometryStateUnknown:
		t.Fatalf("Standard() state = %q, want %q", got.State, sessionevent.GeometryStateUnknown)
	case got.Reason != sessionevent.GeometryReasonNoTerminal:
		t.Fatalf("Standard() reason = %q, want %q", got.Reason, sessionevent.GeometryReasonNoTerminal)
	case got.Cols != 0 || got.Rows != 0:
		t.Fatalf("Standard() carries numbers (%+v) it did not measure", got)
	}
}

// TestMeasureReadsARealTerminal drives the SAME code the production probe runs
// against a descriptor the test owns: a pty whose size the test set. This is the
// measurement half — without it, "unknown" would be the only behaviour ever
// exercised, and a probe that never measures anything would pass the case above.
func TestMeasureReadsARealTerminal(t *testing.T) {
	master, slave := openTestPty(t)
	setTestPtySize(t, slave, 93, 21)

	if got := measure([]*os.File{master, slave}); !got.Known() || got.Cols != 93 || got.Rows != 21 {
		t.Fatalf("measure([master slave]) = %+v, want 93x21 measured", got)
	}
	// A descriptor that answers nothing (a regular file) must not become a
	// measurement: the probe walks on and, with no terminal left, says unknown.
	f, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatalf("create the non-terminal descriptor: %v", err)
	}
	defer f.Close()
	if got := measure([]*os.File{f}); got.Known() {
		t.Fatalf("a regular file was measured as a terminal: %+v", got)
	}
}

// openTestPty returns a master/slave pty PAIR this test owns, so the probe can
// be driven against a descriptor whose geometry the test itself dictates. It is
// the plain POSIX /dev/ptmx route (open, unlock, ask the number, open the slave)
// through the same x/sys/unix this package's own reader already uses — no new
// dependency, and nothing under the repo's scratch rules because the pair is a
// kernel object this process closes at exit (no path is written).
func openTestPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	mfd, err := unix.Open(ptmxPath, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("this host will not open %s (%v) — the measurement half cannot be driven here", ptmxPath, err)
	}
	// CLAIMED HERE, before the first arm that can fail: every teardown below
	// carries this, so it can never mistake a recycled number for this fixture's.
	masterClaim := claimPtyMaster(t, mfd)
	if err := unix.IoctlSetPointerInt(mfd, unix.TIOCSPTLCK, 0); err != nil {
		if relErr := masterClaim.releaseOnFatal(); relErr != nil {
			t.Errorf("releasing the pty master fd %d: %v", mfd, relErr)
		}
		t.Fatalf("unlock the pty master: %v", err)
	}
	n, err := unix.IoctlGetInt(mfd, unix.TIOCGPTN)
	if err != nil {
		if relErr := masterClaim.releaseOnFatal(); relErr != nil {
			t.Errorf("releasing the pty master fd %d: %v", mfd, relErr)
		}
		t.Fatalf("ask the pty master for its slave number: %v", err)
	}
	sfd, err := unix.Open(fmt.Sprintf(ptmxSlaveFormat, n), unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		if relErr := masterClaim.releaseOnFatal(); relErr != nil {
			t.Errorf("releasing the pty master fd %d: %v", mfd, relErr)
		}
		t.Fatalf("open the pty slave: %v", err)
	}
	master, slave = os.NewFile(uintptr(mfd), "ptmx"), os.NewFile(uintptr(sfd), "pts")
	t.Cleanup(func() {
		master.Close() //stylegate:ignore closing the pty pair this fixture opened; the kernel releases it at process exit
		slave.Close()  //stylegate:ignore closing the pty pair this fixture opened; the kernel releases it at process exit
	})
	return master, slave
}

// ptyMasterClaim is the pty master number TOGETHER with what the kernel said it
// denoted at the moment this fixture opened it.
//
// The identity must be captured AT CLAIM TIME and carried, never re-read at
// release time. Measured, and this is why the type exists: a teardown helper that
// called IdentityOf(n) itself reported the number as its own even after the kernel
// had recycled it to a stranger — claiming and closing that stranger, silently. A
// comparison whose two sides are both read after the window opened always agrees.
type ptyMasterClaim struct {
	id borrowedfd.Identity
}

// claimPtyMaster records what number mfd denotes while it is still this fixture's.
func claimPtyMaster(t *testing.T, mfd int) ptyMasterClaim {
	t.Helper()
	id, err := borrowedfd.IdentityOf(mfd)
	if err != nil {
		t.Fatalf("record the identity of the pty master fd %d: %v", mfd, err)
	}
	return ptyMasterClaim{id: id}
}

// releaseOnFatal gives the raw master back on an arm that is about to t.Fatalf,
// before the number was ever wrapped into an *os.File.
//
// It asks the kernel whether the number is STILL the one this fixture opened, and
// closes it only then — the rule every raw descriptor close in this tree follows
// (devdoc/raw-fd-close-identity-guard.md). A refusal is REPORTED, never swallowed:
// the caller's t.Fatalf is about to report the real cause and both facts must
// survive to the reader.
// It RETURNS the refusal instead of reporting it, so the caller's fatal arm decides
// how loud to be — and so a guard can assert the refusal without needing a failing
// *testing.T. A helper that reported through t.Errorf itself could not be tested
// from the outside: the only way to observe it would be to fail the test.
func (c ptyMasterClaim) releaseOnFatal() error {
	var moved *borrowedfd.MovedError
	relErr := c.id.Release()
	if relErr == nil {
		return nil
	}
	if errors.As(relErr, &moved) {
		return moved
	}
	return relErr
}

// TestReleasePtyMasterOnFatalReleasesAndRefuses drives the fatal-arm helper
// DIRECTLY, because a green suite never reaches the arm it lives on (the three
// calls sit under ioctl/open failures). Without this, the helper would be a claim
// nothing exercises — Rule 31's standing.
//
// Two facts, both about the same number:
//  1. a number nothing took away IS released (the ordinary teardown);
//  2. a number the kernel has recycled to a STRANGER is NOT closed, and the
//     refusal names it — the defect the helper exists to prevent.
func TestReleasePtyMasterOnFatalReleasesAndRefuses(t *testing.T) {
	// (1) the ordinary shape: our own pty master, released once.
	mfd, err := unix.Open(ptmxPath, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("this host will not open %s (%v) — the release half cannot be driven here", ptmxPath, err)
	}
	if relErr := claimPtyMaster(t, mfd).releaseOnFatal(); relErr != nil {
		t.Fatalf("releasing the fixture's own pty master %d was refused: %v", mfd, relErr)
	}
	if _, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(mfd), uintptr(unix.F_GETFD), 0); errno == 0 {
		t.Fatalf("fd %d is still open after releasePtyMasterOnFatal — the teardown released nothing", mfd)
	}

	// (2) the defect's shape: claim, release, hand the number to a stranger, then
	// call the helper again as if it were still ours.
	claimed, err := unix.Open(ptmxPath, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open the second pty master: %v", err)
	}
	// THE CLAIM IS TAKEN HERE, WHILE THE NUMBER IS STILL OURS — carried across the
	// window below. This is the difference the first version of this helper got
	// wrong: re-reading the number at release time made the stranger look like the
	// claim, and the helper closed it.
	own := claimPtyMaster(t, claimed)
	if err := own.id.Release(); err != nil {
		t.Fatalf("release the claimed master %d to open the window: %v", claimed, err)
	}
	strangerPath := filepath.Join(t.TempDir(), "stranger")
	// THE STRANGER IS OPENED AS A RAW NUMBER, never through os.OpenFile: a wrapper
	// minted before the number is placed would be ABANDONED when the descriptor is
	// renumbered below, and its finalizer would later close that same number a
	// second time — after the kernel had recycled it to somebody else. Measured:
	// that stray close landed on another test's pipe (fd 8) in the same package run
	// and failed it with "transport-lost". A wrapper is only ever minted HERE, on a
	// number that will not move again.
	strangerFd, err := unix.Open(strangerPath, unix.O_CREAT|unix.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the stranger file: %v", err)
	}
	if strangerFd != claimed {
		got, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(strangerFd), uintptr(unix.F_DUPFD), uintptr(claimed))
		if errno != 0 {
			t.Fatalf("hand the freed number %d to a stranger: %v", claimed, errno)
		}
		if closeErr := unix.Close(strangerFd); closeErr != nil {
			t.Fatalf("release the stranger's original raw handle: %v", closeErr)
		}
		if int(got) != claimed {
			t.Fatalf("the stranger landed on fd %d, not the released %d — the window this guard needs is not open", got, claimed)
		}
		strangerFd = int(got)
	}
	stranger := os.NewFile(uintptr(strangerFd), strangerPath)
	strangerClaim := borrowedfd.Witness(strangerFd)
	t.Cleanup(func() {
		if relErr := strangerClaim.Release(); relErr != nil {
			t.Errorf("releasing the stranger's own descriptor: %v", relErr)
		}
	})

	// THE HELPER ITSELF must refuse. This is the arm the first version of this
	// fixture failed: it re-read the number at release time, so the stranger looked
	// like the claim and the helper CLOSED it. Asserted through the helper, not
	// around it — a guard that reached past the helper would stay green while the
	// helper destroyed someone's descriptor.
	var moved *borrowedfd.MovedError
	if relErr := own.releaseOnFatal(); !errors.As(relErr, &moved) {
		t.Fatalf("the helper answered %v for a number that is no longer ours, want a *borrowedfd.MovedError — "+
			"anything else means it closed (or tried to close) the stranger on fd %d", relErr, claimed)
	}
	if _, _, errno := unix.Syscall(unix.SYS_FCNTL, stranger.Fd(), uintptr(unix.F_GETFD), 0); errno != 0 {
		t.Fatalf("the stranger's descriptor %d was CLOSED (%v) — the helper released a number it no longer owned", claimed, errno)
	}
}

// setTestPtySize sets the pair's geometry the way a terminal emulator would,
// so the probe reads a size THIS test chose rather than the host's.
func setTestPtySize(t *testing.T, f *os.File, cols, rows int) {
	t.Helper()
	if err := unix.IoctlSetWinsize(int(f.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)}); err != nil {
		t.Fatalf("set the pty size to %dx%d: %v", cols, rows, err)
	}
}

// terminalSize reports what the kernel says about a descriptor, for the premise
// check above — the same ioctl the probe uses, asked without the package's own
// "0 means no terminal" folding.
func terminalSize(f *os.File) (cols, rows int) {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil {
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}

const (
	// ptmxPath is the pty multiplexer this test asks for a pair; ptmxSlaveFormat
	// is the slave path the master hands out. Both are named because the path is
	// a kernel interface, not a scratch location (Rule 22 is about temp DATA; a
	// pty pair writes no file).
	ptmxPath        = "/dev/ptmx"
	ptmxSlaveFormat = "/dev/pts/%d"
)
