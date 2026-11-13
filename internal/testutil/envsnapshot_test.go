package testutil

import (
	"os"
	"testing"
)

// TestSnapshotEnvRestoresAbsence is the behavioural half of the guard for the
// defect class SnapshotEnv exists to close: os.Getenv renders "unset" and "set
// to the empty string" identically, so a hand-rolled `prev := os.Getenv(k)` +
// `os.Setenv(k, prev)` restore leaves a variable that was ABSENT set to "" after
// the flip. os.LookupEnv can tell the two apart, and that is the whole reason
// SnapshotEnv captures with it.
//
// The probe starts UNSET: t.Setenv cannot unset (it only sets, which makes
// LookupEnv report the variable as present), so the raw os.Unsetenv here is the
// SUBJECT of the test rather than a leaked environment write — which is why the
// two sites carry an explicit reason instead of being converted.
func TestSnapshotEnvRestoresAbsence(t *testing.T) {
	const probe = "GOPI_TESTUTIL_ENV_SNAPSHOT_PROBE"
	os.Unsetenv(probe)       // env-leak-ok: the ABSENT state is this test's subject; t.Setenv cannot unset
	defer os.Unsetenv(probe) // env-leak-ok: leaves the probe absent, which is exactly the state it arrived in

	if _, present := os.LookupEnv(probe); present {
		t.Fatalf("precondition: %s is present; this test needs it absent", probe)
	}

	restore := SnapshotEnv(t, probe)
	t.Setenv(probe, "live") // the per-call flip the hook would make
	if got, present := os.LookupEnv(probe); !present || got != "live" {
		t.Fatalf("after the flip: %s = %q present=%v, want %q", probe, got, present, "live")
	}

	restore()
	if got, present := os.LookupEnv(probe); present {
		t.Errorf("SnapshotEnv did not restore absence: %s = %q stayed PRESENT — "+
			"os.Getenv would have made this exact mistake (got %q)", probe, got, "")
	}
}
