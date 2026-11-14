package daemonlifetime

// validate_hold_fd_internal_test.go — the DAEMON-side half of the shape
// predicate, tested from inside the package because ArmedFD's number is the one
// os/exec ASSIGNED an ExtraFiles entry (firstExtraFD), not the holder's number.
//
// Two things are pinned here, and the second is a regression this fix itself
// produced and then removed:
//
//  1. ArmedFD must accept a pipe at ITS floor (firstExtraFD) — that is the shape a
//     real `gopi worker --daemon` child sees, because the spawner appends the
//     death pipe to child.ExtraFiles after the handshake pipe.
//  2. It must NOT be held to the SPAWN side's floor. InheritFDFloor (200) exists
//     so a launcher's ExtraFiles cannot replace the HOLDER's inherited number; the
//     daemon's number IS an ExtraFiles number, so refusing it for being below 200
//     disarms every bound daemon. MEASURED 2026-09-19, first run of the shape fix
//     with both readers on InheritFDFloor:
//
//     --- FAIL: TestDaemonDiesWhenItsSpawnerIsSIGKILLed
//         daemon pid … SURVIVED its spawner's SIGKILL — this is exactly the leak the epic is about
//     --- FAIL: TestBoundDaemonStaysAliveWhileItsSpawnerLives
//         daemon 3624470 survived its spawner even in the control case
//
//     … the watchdog was never armed, so the daemon outlived the holder.
//
// The case below asks the predicate BOTH ways on the same number, so the
// distinction is asserted rather than described in a comment.

import (
	"errors"
	"os"
	"strconv"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/borrowedfd"
)

// claimPipeAt dup's a fresh pipe's read end onto the lowest free number >= min and
// returns it with a release registered. Raw F_DUPFD for the CLAIM (never an
// *os.File wrapper the garbage collector could finalize over the assertion), and
// internal/borrowedfd for the RELEASE — so the teardown asks the kernel whether the
// number is still the one claimed here before closing it.
func claimPipeAt(t *testing.T, min int) int {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create the pipe fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close() //stylegate:ignore release of this test's own fixture
		_ = w.Close()
	})
	n, _, errno := syscall.Syscall(syscall.SYS_FCNTL, r.Fd(), uintptr(syscall.F_DUPFD), uintptr(min))
	if errno != 0 {
		t.Fatalf("claim the lowest free number >= %d from fd %d: %v", min, r.Fd(), errno)
	}
	claim := borrowedfd.Witness(int(n))
	t.Cleanup(func() {
		var moved *borrowedfd.MovedError
		if relErr := claim.Release(); relErr != nil && !errors.As(relErr, &moved) {
			t.Errorf("releasing this test's own claimed fd %d failed: %v", n, relErr)
		}
	})
	return int(n)
}

// TestArmedFDAcceptsThePipeAtItsOwnFloor is the daemon-side acceptance case: the
// number a real daemon is handed, and the same number judged by the spawn side's
// floor — which must refuse it, because that is the difference between the two
// sides and the reason the floor is the caller's argument.
func TestArmedFDAcceptsThePipeAtItsOwnFloor(t *testing.T) {
	number := claimPipeAt(t, firstExtraFD)
	t.Setenv(EnvName, strconv.Itoa(number))

	got, ok := ArmedFD()
	if !ok || got != number {
		t.Fatalf("ArmedFD refused fd %d, the number os/exec assigns the FIRST ExtraFiles entry — a real daemon is handed the death pipe exactly there (the spawner appends it after the handshake pipe), so this refusal disarms every bound daemon (measured: the two cmd/agent daemon e2e cases then went red)",
			number)
	}
	if _, err := ValidateHoldFD(number, firstExtraFD); err != nil {
		t.Errorf("ValidateHoldFD(%d, firstExtraFD=%d) refused the daemon side's own shape: %v", number, firstExtraFD, err)
	}
	if _, err := ValidateHoldFD(number, InheritFDFloor); err == nil {
		t.Errorf("ValidateHoldFD(%d, InheritFDFloor=%d) ACCEPTED a number below the spawn side's floor — the two floors exist precisely because the spawn side's number is the holder's and the daemon's is an ExtraFiles number; if one floor satisfies both, the distinction has been silently dropped",
			number, InheritFDFloor)
	}
}

// TestArmedFDRefusesABelowFloorNumber pins the original renumbering guard: under
// `go test` the standard streams ARE pipes, so a mis-added ExtraFiles entry (or a
// hand-taken number) can point the watchdog at stdout or stderr — MEASURED
// 2026-09-14: that exact mistake killed a test binary. A pipe at a number the
// daemon side can never be handed must be refused even though it IS a pipe.
func TestArmedFDRefusesABelowFloorNumber(t *testing.T) {
	for _, n := range []int{0, 1, 2, firstExtraFD - 1} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			if _, err := ValidateHoldFD(n, firstExtraFD); err == nil {
				t.Errorf("ValidateHoldFD(%d, %d) accepted a number below the daemon side's floor — fd 0/1/2 are the standard streams, which under `go test` are pipes, so the watchdog would be armed on one of them", n, firstExtraFD)
			}
			t.Setenv(EnvName, strconv.Itoa(n))
			if got, ok := ArmedFD(); ok || got != 0 {
				t.Errorf("ArmedFD accepted fd %d (got %d, ok=%v) — a number below the ExtraFiles range can never be the binding the spawner handed over", n, got, ok)
			}
		})
	}
}

// TestArmedFDAndValidateHoldFDShareOneVerdict is the drift guard: the daemon-side
// reader's verdict must be the predicate's, on the same floor, for every shape.
// That is what makes the predicate the SINGLE SOURCE rather than a second opinion
// added beside the old inline test.
func TestArmedFDAndValidateHoldFDShareOneVerdict(t *testing.T) {
	pipeFD := claimPipeAt(t, firstExtraFD)
	file, err := os.CreateTemp(t.TempDir(), "death-pipe-internal-*")
	if err != nil {
		t.Fatalf("create the non-pipe fixture: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() }) //stylegate:ignore release of this test's own fixture
	raw, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), uintptr(syscall.F_DUPFD), uintptr(firstExtraFD))
	if errno != 0 {
		t.Fatalf("claim the non-pipe fixture's number: %v", errno)
	}
	fileNumber := int(raw)
	fileClaim := borrowedfd.Witness(fileNumber)
	t.Cleanup(func() {
		var moved *borrowedfd.MovedError
		if relErr := fileClaim.Release(); relErr != nil && !errors.As(relErr, &moved) {
			t.Errorf("releasing this test's own claimed fd %d failed: %v", fileNumber, relErr)
		}
	})

	for _, n := range []int{0, 1, 2, firstExtraFD - 1, pipeFD, fileNumber, 100000} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			_, verr := ValidateHoldFD(n, firstExtraFD)
			t.Setenv(EnvName, strconv.Itoa(n))
			got, ok := ArmedFD()
			if ok != (verr == nil) {
				t.Fatalf("the two readers of the death-pipe contract disagree about fd %d: ValidateHoldFD err=%v, ArmedFD ok=%v — the shape test must have exactly ONE source (Rule 14)", n, verr, ok)
			}
			if ok && got != n {
				t.Errorf("ArmedFD accepted fd %d but returned %d", n, got)
			}
		})
	}
}
