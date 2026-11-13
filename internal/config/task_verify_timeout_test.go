package config

// The verify-cap resolution (verify-ops defect, task 01a0ed37, 2026-09-29).
//
// WHAT IS PINNED HERE, AND WHY IT IS A TEST AND NOT A COMMENT. The defect was
// not a missing value: `gopi task verify`'s --timeout defaulted to 0 =
// UNLIMITED, so a scripted/sandboxed caller could be parked forever by a child
// that never returns — measured: the materials batch had to be truncated from
// the OUTSIDE (`timeout 40 gopi task verify …` → exit 124). A bounded DEFAULT
// is a claim about every caller that passes no flag, so it is asserted at the
// resolver, which is the ONE place the three channels meet (Rule 2).

import (
	"os"
	"path/filepath"
	"testing"
)

// clearVerifyTimeoutEnv blanks the knob's env and settings channels so a test
// reads what the tree says rather than what the developer's shell exports.
func clearVerifyTimeoutEnv(t *testing.T) {
	t.Helper()
	t.Setenv(TaskVerifyTimeoutEnv, "")
	t.Setenv("GOPI_SETTINGS", "")
}

// TestResolveTaskVerifyTimeout_DefaultIsBounded is THE regression guard: with
// no flag, no env and no settings key, the resolved cap must be the named
// bounded default — never 0 (the pre-fix value, which is what "unlimited"
// means here).
func TestResolveTaskVerifyTimeout_DefaultIsBounded(t *testing.T) {
	clearVerifyTimeoutEnv(t)
	got, err := ResolveTaskVerifyTimeout(0, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != DefaultTaskVerifyTimeoutSec {
		t.Fatalf("default verify cap = %d, want the bounded default %d (0 = unlimited is the DEFECT this knob exists to remove)",
			got, DefaultTaskVerifyTimeoutSec)
	}
	if DefaultTaskVerifyTimeoutSec <= 0 {
		t.Fatalf("DefaultTaskVerifyTimeoutSec = %d — the default must be finite", DefaultTaskVerifyTimeoutSec)
	}
}

// TestResolveTaskVerifyTimeout_ExplicitZeroIsUnlimited: the CLI may still ask
// for the unbounded behaviour, but only by SAYING so — cliSet is what separates
// a typed `--timeout 0` from a missing flag, and that separation is the whole
// point of the second argument.
func TestResolveTaskVerifyTimeout_ExplicitZeroIsUnlimited(t *testing.T) {
	clearVerifyTimeoutEnv(t)
	got, err := ResolveTaskVerifyTimeout(0, true)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != 0 {
		t.Fatalf("an explicit --timeout 0 must mean unlimited, got %d", got)
	}
}

// TestResolveTaskVerifyTimeout_Precedence: an explicit CLI value beats env and
// settings; env beats settings.
func TestResolveTaskVerifyTimeout_Precedence(t *testing.T) {
	clearVerifyTimeoutEnv(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath,
		[]byte(`{"task_verify_timeout_sec": 1111}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)

	got, err := ResolveTaskVerifyTimeout(0, false)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if got != 1111 {
		t.Errorf("settings-only = %d, want 1111", got)
	}

	t.Setenv(TaskVerifyTimeoutEnv, "2222")
	got, err = ResolveTaskVerifyTimeout(0, false)
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if got != 2222 {
		t.Errorf("env over settings = %d, want 2222", got)
	}

	got, err = ResolveTaskVerifyTimeout(3333, true)
	if err != nil {
		t.Fatalf("cli: %v", err)
	}
	if got != 3333 {
		t.Errorf("cli over env = %d, want 3333", got)
	}
}

// TestResolveTaskVerifyTimeout_EnvZeroDisablesTheBound: the env channel reaches
// the unlimited posture too — an operator whose host really does run hour-long
// verifies sets it in the environment rather than typing a flag on every call.
func TestResolveTaskVerifyTimeout_EnvZeroDisablesTheBound(t *testing.T) {
	clearVerifyTimeoutEnv(t)
	t.Setenv(TaskVerifyTimeoutEnv, "0")
	got, err := ResolveTaskVerifyTimeout(0, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != 0 {
		t.Fatalf("env 0 must mean unlimited, got %d", got)
	}
}

// TestResolveTaskVerifyTimeout_MalformedAndNegativeAreRefused: neither a
// mangled value nor a negative one falls back silently (Rule 15). The
// neighbour knobs (the sweep graces) report-and-keep a malformed value because
// a wrong grace only mis-times a report; a wrong CAP decides how long a process
// stayed parked, so guessing is the wrong repair.
func TestResolveTaskVerifyTimeout_MalformedAndNegativeAreRefused(t *testing.T) {
	clearVerifyTimeoutEnv(t)
	t.Setenv(TaskVerifyTimeoutEnv, "not-a-number")
	if _, err := ResolveTaskVerifyTimeout(0, false); err == nil {
		t.Errorf("a malformed %s must be refused, not silently defaulted", TaskVerifyTimeoutEnv)
	}

	clearVerifyTimeoutEnv(t)
	t.Setenv(TaskVerifyTimeoutEnv, "-5")
	if _, err := ResolveTaskVerifyTimeout(0, false); err == nil {
		t.Errorf("a negative %s must be refused", TaskVerifyTimeoutEnv)
	}

	clearVerifyTimeoutEnv(t)
	if _, err := ResolveTaskVerifyTimeout(-1, true); err == nil {
		t.Errorf("a negative CLI cap must be refused")
	}

	clearVerifyTimeoutEnv(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath,
		[]byte(`{"task_verify_timeout_sec": -7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	if _, err := ResolveTaskVerifyTimeout(0, false); err == nil {
		t.Errorf("a negative settings value must be refused")
	}
}
