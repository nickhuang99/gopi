package sigsource

// capture_state_test.go — the guard for the ONE decision function.
//
// Describe is the only place that decides which of the closed-vocabulary states
// a delivery gets, and it states the PRECEDENCE once (unsupported > arm-failed >
// captured > not-captured). Two arms matter and they fail in opposite
// directions:
//
//	① an arm REFUSAL must be visible as "arm-failed" WITH its verbatim reason —
//	   the refusals used to reach stderr once, at install time, where no reader of
//	   a provenance record could ever find them (Rule 15);
//	② a SUCCESSFUL re-arm must CLEAR a previous refusal, otherwise a process that
//	   once failed to arm is labelled "arm-failed" for the rest of its life and
//	   the label outlives the fact it describes.
//
// The ordering is exercised on this build only, so the "unsupported-host" arm is
// pinned where it is reachable and the rest of the file skips there (see
// unsupportedGuard below): on linux/amd64 UnsupportedReason() is "" and asserting
// anything about the unsupported state would be asserting about a branch that
// cannot run here.

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

// unsupportedGuard reports whether this build has NO capture implementation, so
// the capture-specific arms below must not run (they would assert a state this
// target can never reach and fail for the wrong reason).
func unsupportedGuard(t *testing.T) bool {
	t.Helper()
	if why := UnsupportedReason(); why != "" {
		if state, reason := Describe(syscall.SIGTERM); state != CaptureUnsupported {
			t.Fatalf("Describe = (%q, %q) on a target whose UnsupportedReason is %q, want %q",
				state, reason, why, CaptureUnsupported)
		}
		return true
	}
	return false
}

// TestDescribeReportsAnArmRefusalAndClearsIt is arms ①+② on the arm state
// itself. noteArmFailure is unexported and only Arm calls it, so a test in this
// package is the only caller that can drive the states without forcing the
// kernel to refuse an rt_sigaction (which would need a signal number outside the
// table — refused before syscall, so it never reaches noteArmFailure).
func TestDescribeReportsAnArmRefusalAndClearsIt(t *testing.T) {
	if unsupportedGuard(t) {
		t.Skip("no capture implementation on this target: the arm-failure state is unreachable")
	}
	t.Cleanup(func() { noteArmFailure("") })

	const refusal = "rt_sigaction(15): operation not permitted (fixture)"
	noteArmFailure(refusal)

	// ARM ① — the refusal is the STATE, and its reason is the refusal VERBATIM,
	// not a summary that would drop the errno a reader needs.
	state, reason := Describe(syscall.SIGTERM)
	if state != CaptureArmFailed {
		t.Fatalf("Describe = (%q, %q) while an arming refusal is on record, want (%q, %q)",
			state, reason, CaptureArmFailed, refusal)
	}
	if !strings.Contains(reason, "operation not permitted") {
		t.Fatalf("reason = %q, want the kernel's own words quoted: %q", reason, refusal)
	}

	// ARM ② — a successful re-arm clears it. "" is what Arm writes on entry, and
	// describing the very same signal must stop claiming a failure that no longer
	// describes anything.
	noteArmFailure("")
	if state, _ := Describe(syscall.SIGTERM); state == CaptureArmFailed {
		t.Fatalf("Describe still reports %q after the refusal was cleared — the label outlived the fact",
			CaptureArmFailed)
	}
	if got := armFailure(); got != "" {
		t.Fatalf("armFailure = %q after a successful arm, want the empty string", got)
	}
}

// TestDescribeNeverReturnsAnEmptyState pins the contract internal/signalrec
// depends on: two strings, and the FIRST one is never empty on ANY target. An
// empty state would put a "" into a JSON field the record declares as required,
// which is exactly the silence the field exists to remove (Rule 15a).
func TestDescribeNeverReturnsAnEmptyState(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		state, reason := Describe(sig)
		if strings.TrimSpace(state) == "" {
			t.Fatalf("Describe(%v) returned an empty state — the record's capture field would be the empty string", sig)
		}
		if state == CaptureCaptured && reason != "" {
			t.Fatalf("Describe(%v) = (%q, %q): a captured delivery gives no reason", sig, state, reason)
		}
		if state != CaptureCaptured && strings.TrimSpace(reason) == "" {
			t.Fatalf("Describe(%v) = (%q, \"\"): a state that is NOT a capture must say why", sig, state)
		}
	}
}
