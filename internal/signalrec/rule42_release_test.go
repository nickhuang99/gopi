package signalrec

// rule42_release_test.go — the RELEASE half of the re-raise, as a unit.
//
// CONTRACT UNDER TEST (PROJECT.md Rule 42). `reRaise` no longer calls
// `signal.Reset`; it releases this package's own process-lifetime registration
// instead. That is only correct if the release is REAL — a function that reports
// success while the registration stays live would restore the exact shape Rule 42
// forbids (a raise swallowed by an invisible subscriber), which is unkillable by
// the operator's own signal.
//
// WHAT A UNIT CAN AND CANNOT SEE, stated so the test is not read as more than it
// is: the os/signal PEER SET is unreadable from outside the package (measured —
// //go:linkname runtime.sig and os/signal.handlers are both refused on go1.24,
// and /proc/self/status SigCgt/SigIgn reads identically with and without a second
// subscriber). So this file pins the OBSERVABLE this package owns — the slot the
// release clears — and the DEATH itself is pinned where it is observable: a real
// child process in TestSignalRecForwardingPreservesDefaultDeath (heavy; run with
// GOPI_HEAVY_TESTS=1).

import "testing"

// TestReleaseSignalHandlersClearsTheSlot pins the state transition the whole
// no-Reset re-raise depends on: after Install the recorder reports a live
// registration, and after ReleaseSignalHandlers it does not — and a SECOND
// release is a no-op that does not re-arm anything.
func TestReleaseSignalHandlersClearsTheSlot(t *testing.T) {
	srHome(t)
	installStop.Store(nil) // the process may have installed once already
	t.Cleanup(func() { installStop.Store(nil) })

	if RecorderInstallIsLive() {
		t.Fatalf("the recorder reports a live registration before Install — the slot is not " +
			"per-process state but leftover, so every assertion below would be about the wrong thing")
	}
	stop := Install("")
	if !RecorderInstallIsLive() {
		t.Fatalf("Install returned but the recorder does not report its own registration as live — " +
			"release would then be a no-op while the re-raise relies on it")
	}
	if !ReleaseSignalHandlers() {
		t.Fatalf("ReleaseSignalHandlers reported failure on a process that DID hold a registration")
	}
	if RecorderInstallIsLive() {
		t.Fatalf("the registration is still reported LIVE after release — a re-raise would be " +
			"swallowed by this very channel, which is the unkillable shape Rule 42 exists to prevent")
	}
	// Idempotent: a second release must be safe and must not resurrect the slot.
	if !ReleaseSignalHandlers() {
		t.Fatalf("a second ReleaseSignalHandlers reported failure")
	}
	if RecorderInstallIsLive() {
		t.Fatalf("the second release re-armed the slot")
	}
	// And the uninstall the caller was handed is the SAME registration, so
	// calling it after the release is the ordinary double-stop every caller does.
	stop()
	if RecorderInstallIsLive() {
		t.Fatalf("the caller's own stop re-armed the slot")
	}
}

// TestReleaseSignalHandlersWithNoInstallIsAnHonestNoOp pins the other half: a
// process that never installed the recorder (a library, a `go test` binary) must
// get a truthful answer rather than a silent "I released something".
func TestReleaseSignalHandlersWithNoInstallIsAnHonestNoOp(t *testing.T) {
	installStop.Store(nil)
	if !ReleaseSignalHandlers() {
		t.Fatalf("release reported failure with nothing to release — there is no registration of this " +
			"package's to leave behind, so the answer must be true")
	}
}
