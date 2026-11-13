package config

// cov_config_envbool_test.go — function-level coverage slice for
// config.EnvBool (branch feat/cov-config-hp). Tests only: no product code is
// touched by this slice. Helper prefix covCfg* is task-unique so a sibling
// coverage slice of the same package can never collide on a helper name.

import (
	"os"
	"strings"
	"testing"
)

// covCfgProbeEnv is a test-only env var name: EnvBool takes the NAME as an
// argument, so the matrix drives it through a probe rather than one of the
// product's own vars (whose values ApplyEnv writes).
const covCfgProbeEnv = "GOPI_COV_CFG_PROBE"

// covCfgAcceptedEnvBool / covCfgRejectedEnvBool are the EXPLICIT value matrix
// EnvBool's contract is pinned to: TRUTHY-ONLY parsing (exactly the five
// documented spellings, case/space insensitive) and everything else —
// including the canonical "off" ApplyEnv force-writes for a disabled
// switch — DISABLED. The rejected list deliberately carries the "false-ish"
// spellings that a naive `os.Getenv(...) != ""` reader would treat as ENABLED.
var (
	covCfgAcceptedEnvBool = []string{"1", "on", "yes", "true", "enabled", "TRUE", "On", "  yes  ", "Enabled", "1 ", "\ttrue"}
	covCfgRejectedEnvBool = []string{"", " ", "0", "off", "false", "no", "disabled", "OFF", "False", "NO", "banana", "2", "true1", "enable", "y", "t", "yes!", "on the way"}
)

// TestCovCfgEnvBool_AcceptedMatrix runs every accepted spelling through the
// exported entry point and asserts it reads TRUE.
func TestCovCfgEnvBool_AcceptedMatrix(t *testing.T) {
	for _, in := range covCfgAcceptedEnvBool {
		t.Run("accept_"+strings.ReplaceAll(in, " ", "_"), func(t *testing.T) {
			t.Setenv(covCfgProbeEnv, in)
			if !EnvBool(covCfgProbeEnv) {
				t.Errorf("EnvBool(%q=%q) = false, want true", covCfgProbeEnv, in)
			}
		})
	}
}

// TestCovCfgEnvBool_RejectedMatrix is the other half of the matrix: every
// non-truthy spelling (empty, the false-ish words, garbage, near-misses) must
// read FALSE — this is the regression guard for the "off means enabled"
// channel bug EnvBool's doc comment cites.
func TestCovCfgEnvBool_RejectedMatrix(t *testing.T) {
	for _, in := range covCfgRejectedEnvBool {
		t.Run("reject_"+strings.ReplaceAll(in, " ", "_"), func(t *testing.T) {
			t.Setenv(covCfgProbeEnv, in)
			if EnvBool(covCfgProbeEnv) {
				t.Errorf("EnvBool(%q=%q) = true, want false", covCfgProbeEnv, in)
			}
		})
	}
}

// TestCovCfgEnvBool_UnsetIsFalse pins the absence case: an env var that is
// not in the environment at all reads false (t.Setenv then os.Unsetenv —
// t.Setenv's own cleanup restores whatever the process had).
func TestCovCfgEnvBool_UnsetIsFalse(t *testing.T) {
	t.Setenv(covCfgProbeEnv, "1")
	if !EnvBool(covCfgProbeEnv) {
		t.Fatalf("precondition failed: %s=1 did not read true", covCfgProbeEnv)
	}
	if err := os.Unsetenv(covCfgProbeEnv); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}
	if EnvBool(covCfgProbeEnv) {
		t.Errorf("EnvBool(%q) with the var UNSET = true, want false", covCfgProbeEnv)
	}
	// The empty NAME is the degenerate lookup: os.Getenv("") is always "".
	if EnvBool("") {
		t.Error(`EnvBool("") = true, want false`)
	}
}

// TestCovCfgEnvBool_CaseAndSpaceInsensitive states the normalization
// explicitly: matching is ToLower(TrimSpace(value)), so surrounding spaces,
// tabs and any casing resolve to the same answer.
func TestCovCfgEnvBool_CaseAndSpaceInsensitive(t *testing.T) {
	for _, in := range []string{"true", "TRUE", "  true  ", "\ttrue\n", "TrUe"} {
		t.Setenv(covCfgProbeEnv, in)
		if !EnvBool(covCfgProbeEnv) {
			t.Errorf("EnvBool(%q=%q) = false, want true (case/space must not matter)", covCfgProbeEnv, in)
		}
	}
	for _, in := range []string{"off", " OFF ", "\tOff\n"} {
		t.Setenv(covCfgProbeEnv, in)
		if EnvBool(covCfgProbeEnv) {
			t.Errorf("EnvBool(%q=%q) = true, want false (case/space must not matter)", covCfgProbeEnv, in)
		}
	}
}

// TestCovCfgEnvBool_ReadsEnvPerCall proves there is no memoization: flipping
// the value between two calls flips the answer on the very next call. Cheap
// but load-bearing — a cached reader would freeze a test/sandbox flip.
func TestCovCfgEnvBool_ReadsEnvPerCall(t *testing.T) {
	t.Setenv(covCfgProbeEnv, "1")
	if !EnvBool(covCfgProbeEnv) {
		t.Fatal("first read of 1 = false")
	}
	t.Setenv(covCfgProbeEnv, "off")
	if EnvBool(covCfgProbeEnv) {
		t.Fatal("second read of off = true — the reader memoized")
	}
	t.Setenv(covCfgProbeEnv, "enabled")
	if !EnvBool(covCfgProbeEnv) {
		t.Fatal("third read of enabled = false — the reader memoized")
	}
}

// TestCovCfgEnvBool_CanonicalOffFromApplyEnv is the cross-function contract
// this slice cares about most: ApplyEnv emits the canonical "off" for a
// DISABLED switch, and EnvBool must read that literal as FALSE (the real
// 2026-08-29 debug-dump / 2026-08-30 retry-log regression the doc cites).
// ApplyEnv writes MANY knobs at once, so the process env is snapshotted and
// restored around it.
func TestCovCfgEnvBool_CanonicalOffFromApplyEnv(t *testing.T) {
	covCfgSnapshotEnv(t)

	ApplyEnv(&Config{}, true)
	if got := os.Getenv("GOPI_DEBUG"); got != "off" {
		t.Fatalf("ApplyEnv(disabled) wrote GOPI_DEBUG=%q, want the canonical \"off\"", got)
	}
	if EnvBool("GOPI_DEBUG") {
		t.Error(`EnvBool("GOPI_DEBUG") = true while ApplyEnv wrote "off" — the canonical-off channel regressed`)
	}

	ApplyEnv(&Config{Debug: true}, true)
	if got := os.Getenv("GOPI_DEBUG"); got != "1" {
		t.Fatalf("ApplyEnv(enabled) wrote GOPI_DEBUG=%q, want \"1\"", got)
	}
	if !EnvBool("GOPI_DEBUG") {
		t.Error(`EnvBool("GOPI_DEBUG") = false while ApplyEnv wrote "1"`)
	}
}

// covCfgSnapshotEnv snapshots the process environment and restores it at test
// end: ApplyEnv(fromFlags=true) force-writes a canonical value for every
// scalar knob (including "0" for unset ints), so without this the new values
// would leak into sibling tests of the package.
func covCfgSnapshotEnv(t *testing.T) {
	t.Helper()
	snap := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			snap[k] = v
		}
	}
	t.Cleanup(func() {
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if _, had := snap[k]; !had {
				_ = os.Unsetenv(k) //stylegate:ignore clears a fixture variable; the presence check afterwards is the assertion
			}
		}
		for k, v := range snap {
			_ = os.Setenv(k, v) //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset'
		}
	})
}
