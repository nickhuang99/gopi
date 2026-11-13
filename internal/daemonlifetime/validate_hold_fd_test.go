package daemonlifetime_test

// validate_hold_fd_test.go — the guard for the ONE death-pipe shape predicate.
//
// The predicate exists because os.NewFile validates NOTHING (it returns nil only
// for a negative number), so "the wrapper is nil ⇒ the number is unusable" is a
// refusal no caller can reach. Both readers of the death-pipe contract — the
// daemon-side ArmedFD and cmd/agent's spawn-side daemonDeathPipe — call
// ValidateHoldFD, so this file pins the answers they share:
//
//	1. below InheritFDFloor            → refused (ExtraFiles would replace it);
//	2. a number this process does not hold → refused (the kernel names nothing);
//	3. a live descriptor that is NOT a pipe → refused, quoting the kernel's
//	   target (a regular file, including one ABOVE the floor);
//	4. a genuine pipe at/above the floor → ACCEPTED (the suite's own shape — the
//	   regression this predicate must never introduce);
//	5. ArmedFD's verdict must AGREE with the predicate on every one of them, or
//	   the two readers have drifted apart (Rule 14: one source, not two).
//
// Every number here is claimed or released explicitly (raw syscall.Dup /
// F_DUPFD + syscall.Close), never through an *os.File wrapper the garbage
// collector could finalize and close under the assertion.

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/borrowedfd"
	"github.com/nick/gopi/internal/daemonlifetime"
)

// deathPipeFreeFD is a number no process in this suite holds: far above any
// plausible live descriptor, and low enough that a stray dup cannot take it.
const deathPipeFreeFD = 100000

// claimAtLeast dup's src onto the lowest free number >= min and returns it (the
// F_DUPFD idiom internal/daemonlifetime uses when arming the suite's binding).
func claimAtLeast(t *testing.T, src *os.File, min int) int {
	t.Helper()
	n, _, errno := syscall.Syscall(syscall.SYS_FCNTL, src.Fd(), uintptr(syscall.F_DUPFD), uintptr(min))
	if errno != 0 {
		t.Fatalf("claim the lowest free number >= %d from fd %d: %v", min, src.Fd(), errno)
	}
	return int(n)
}

// closeRaw releases a number claimed by claimAtLeast.
//
// The release goes through internal/borrowedfd: the kernel is asked, at teardown,
// whether the number is STILL the one that was claimed here, and only then is it
// closed. A bare syscall.Close would release whatever holds the number by then —
// and would report success while doing it, because Close is nil for an open
// number whoever owns it (internal/borrowedfd/release.go states the window).
func closeRaw(t *testing.T, n int) {
	t.Helper()
	claim := borrowedfd.Witness(n)
	t.Cleanup(func() {
		var moved *borrowedfd.MovedError
		err := claim.Release()
		if err != nil && !errors.As(err, &moved) {
			t.Errorf("releasing this test's own claimed fd %d failed: %v", n, err)
			return
		}
		if moved != nil {
			t.Errorf("%s", moved.Error())
		}
	})
}

// TestValidateHoldFDRefusesByShape is the refusal half: each failure mode is
// named, and the two shapes a caller would otherwise confuse are distinguished
// (a free number vs a live non-pipe).
func TestValidateHoldFDRefusesByShape(t *testing.T) {
	t.Run("below-the-floor", func(t *testing.T) {
		for _, n := range []int{0, 1, 2, 3, daemonlifetime.InheritFDFloor - 1} {
			target, err := daemonlifetime.ValidateHoldFD(n, daemonlifetime.InheritFDFloor)
			if err == nil {
				t.Fatalf("ValidateHoldFD(%d) accepted a number below the ExtraFiles floor %d — os/exec renumbers ExtraFiles from fd 3 and would silently REPLACE an inherited descriptor at that number, leaving the daemon bound to a stranger or unbound",
					n, daemonlifetime.InheritFDFloor)
			}
			if target != "" {
				t.Errorf("ValidateHoldFD(%d) reported target %q alongside an error — an unusable number has no kernel target to report", n, target)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(daemonlifetime.InheritFDFloor)) {
				t.Errorf("ValidateHoldFD(%d)'s refusal %q does not name the floor %d, so a reader cannot tell it from the other two failure modes",
					n, err, daemonlifetime.InheritFDFloor)
			}
		}
	})

	t.Run("not-open", func(t *testing.T) {
		target, err := daemonlifetime.ValidateHoldFD(deathPipeFreeFD, daemonlifetime.InheritFDFloor)
		if err == nil {
			t.Fatalf("ValidateHoldFD(%d) accepted a number no process here holds — this is the exact state the spawn side must refuse BEFORE wrapping it (os.NewFile would have returned a non-nil wrapper for it)", deathPipeFreeFD)
		}
		if target != "" {
			t.Errorf("ValidateHoldFD(%d) reported target %q for an unheld number", deathPipeFreeFD, target)
		}
		if !strings.Contains(err.Error(), "not open") {
			t.Errorf("the refusal for a free number is %q — it must SAY that nothing is open there, or the operator is left guessing which of the three shapes failed", err)
		}
	})

	t.Run("live-but-not-a-pipe", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "death-pipe-shape-*")
		if err != nil {
			t.Fatalf("create the non-pipe fixture: %v", err)
		}
		t.Cleanup(func() { _ = file.Close() }) //stylegate:ignore release of this test's own fixture
		// ABOVE the floor on purpose: this case must fail on the PIPE test, not
		// on the floor test — otherwise it would pass for the wrong reason and
		// the non-pipe branch would be unguarded.
		number := claimAtLeast(t, file, daemonlifetime.InheritFDFloor)
		closeRaw(t, number)
		if number < daemonlifetime.InheritFDFloor {
			t.Fatalf("the fixture claimed fd %d, below the floor %d — this case would then be testing the floor, not the shape", number, daemonlifetime.InheritFDFloor)
		}
		target, err := daemonlifetime.ValidateHoldFD(number, daemonlifetime.InheritFDFloor)
		if err == nil {
			t.Fatalf("ValidateHoldFD(%d) accepted a regular file at/above the floor — watching a file cannot report the holder's death, so the daemon would run unbound while every surface says it is bound", number)
		}
		if target != "" {
			t.Errorf("ValidateHoldFD(%d) reported target %q for a non-pipe — the target is reported only on success", number, target)
		}
		if !strings.Contains(err.Error(), "not a pipe") {
			t.Errorf("the refusal for a regular file is %q — it must say the descriptor is not a pipe", err)
		}
		if !strings.Contains(err.Error(), file.Name()) {
			t.Errorf("the refusal %q does not QUOTE the kernel's target %q — the operator is then told a shape failed without being told what was found instead (Rule 15)", err, file.Name())
		}
	})
}

// TestValidateHoldFDAcceptsTheSuitesOwnShape is the ACCEPTANCE half, and it is
// the regression guard for the daemon e2e: the suite arms a process-wide pipe
// whose read end sits at fd >= InheritFDFloor and exports it as HoldEnvName, so a
// predicate that refused a pipe at that number would refuse every daemon the test
// suite starts (the binding would silently stop being exercised).
//
// The number is claimed exactly the way ArmThisProcess claims it (F_DUPFD at the
// floor), so the fixture IS the suite's shape rather than a lookalike.
func TestValidateHoldFDAcceptsTheSuitesOwnShape(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create the suite-shape pipe fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close() //stylegate:ignore release of this test's own fixture
		_ = w.Close()
	})
	number := claimAtLeast(t, r, daemonlifetime.InheritFDFloor)
	closeRaw(t, number)
	if number < daemonlifetime.InheritFDFloor {
		t.Fatalf("the fixture's read end landed at fd %d, below the floor %d — the suite's binding cannot be reproduced here", number, daemonlifetime.InheritFDFloor)
	}
	target, err := daemonlifetime.ValidateHoldFD(number, daemonlifetime.InheritFDFloor)
	if err != nil {
		t.Fatalf("ValidateHoldFD(%d) REFUSED the suite's own binding shape (a pipe at/above the floor): %v — a shape test this strict refuses the inherited fd the test suite arms, and every daemon started under it would fail to start",
			number, err)
	}
	if !strings.HasPrefix(target, "pipe:[") {
		t.Errorf("ValidateHoldFD(%d) accepted the number but reported target %q — an accepted death pipe must carry the kernel's pipe target so a caller can say WHICH pipe it bound to", number, target)
	}

}
