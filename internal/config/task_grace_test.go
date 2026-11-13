package config

// Grace-period resolution tests: the ONE surviving grace — the heartbeat-stall
// grace — resolves CLI > env > settings > default, and the pre-rename
// GOPI_TASK_STALE_SEC spelling keeps working.
//
// WHY ONLY ONE GRACE (2026-10-26): this file used to test a TRIPLE, because the
// zombie sweeper consumed three (stall, max-run, auto-dispose). The sweeper is
// deleted whole (devdoc/zombie-sweep-removed.md) and the surviving leg only
// REPORTS, so two of the three lost their only reader and were removed with it
// — a knob nothing reads reads as a working control surface (Rule 21), which
// is the failure mode this file must not restore by testing them back into
// existence.

import (
	"os"
	"path/filepath"
	"testing"
)

// clearGraceEnv blanks every grace env var so tests are hermetic.
func clearGraceEnv(t *testing.T) {
	t.Helper()
	t.Setenv(TaskStallSecEnv, "")
	t.Setenv(taskStallSecEnvLegacy, "")
	t.Setenv("GOPI_SETTINGS", "")
}

// TestResolveStallGrace_Default: no CLI, no env, no settings → 1800s (30min).
func TestResolveStallGrace_Default(t *testing.T) {
	clearGraceEnv(t)
	sec, err := ResolveTaskStallGrace(0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sec != defaultTaskStallGraceSec {
		t.Errorf("stall = %d, want %d", sec, defaultTaskStallGraceSec)
	}
}

// TestResolveStallGrace_CLIOverEnvOverSettings: precedence flag > env >
// settings, for the one knob.
func TestResolveStallGrace_CLIOverEnvOverSettings(t *testing.T) {
	clearGraceEnv(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"task_stall_sec": 100}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)

	// Settings only.
	sec, err := ResolveTaskStallGrace(0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sec != 100 {
		t.Errorf("settings: got %d, want 100", sec)
	}

	// Env overrides settings.
	t.Setenv(TaskStallSecEnv, "1500")
	sec, err = ResolveTaskStallGrace(0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sec != 1500 {
		t.Errorf("env: got %d, want 1500", sec)
	}

	// CLI overrides env.
	sec, err = ResolveTaskStallGrace(900)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sec != 900 {
		t.Errorf("cli: got %d, want 900", sec)
	}

	// CLI zero = absent (does NOT disable) — env still wins below CLI.
	sec, err = ResolveTaskStallGrace(0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sec != 1500 {
		t.Errorf("cli-absent: got %d, want the env value 1500", sec)
	}
}

// TestResolveStallGrace_LegacyStaleEnv: the pre-parameterization
// GOPI_TASK_STALE_SEC spelling still drives the knob when the new name is
// unset.
func TestResolveStallGrace_LegacyStaleEnv(t *testing.T) {
	clearGraceEnv(t)
	t.Setenv(taskStallSecEnvLegacy, "2700")
	sec, err := ResolveTaskStallGrace(0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sec != 2700 {
		t.Errorf("legacy env must drive stall, got %d", sec)
	}
}

// TestResolveStallGrace_NegativeRejected: a negative grace is an error, never
// a silent disable.
func TestResolveStallGrace_NegativeRejected(t *testing.T) {
	clearGraceEnv(t)
	if _, err := ResolveTaskStallGrace(-1); err == nil {
		t.Errorf("negative CLI stall must error")
	}
	t.Setenv(TaskStallSecEnv, "-5")
	if _, err := ResolveTaskStallGrace(0); err == nil {
		t.Errorf("negative env stall must error")
	}
}

// TestResolveStallGrace_ExplicitZeroDisables: an explicit env 0 disables the
// knob (operator opt-out), overriding a settings value.
func TestResolveStallGrace_ExplicitZeroDisables(t *testing.T) {
	clearGraceEnv(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"task_stall_sec": 100}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv(TaskStallSecEnv, "0")
	sec, err := ResolveTaskStallGrace(0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sec != 0 {
		t.Errorf("explicit env 0 must disable stall, got %d", sec)
	}
}

// TestResolveStallGrace_DisposalKnobsAreGONE pins the REMOVAL (Rule 21/35): the
// auto-disposal grace and the max-run backstop existed only to serve the
// zombie sweeper, and a knob with no reader is worse than an absent one — it
// reads as a working control surface while nothing consumes it. The check is
// on behaviour, not on a missing identifier: an operator who still exports the
// retired spellings must get NO effect rather than a surprise, and the
// surviving grace must resolve independently of them.
func TestResolveStallGrace_DisposalKnobsAreGONE(t *testing.T) {
	clearGraceEnv(t)
	// The retired env spellings and settings keys, spelled as literals ON
	// PURPOSE: this test is about what a deployment on disk still carries, so
	// it must keep naming those strings after the constants are deleted.
	t.Setenv("GOPI_TASK_DISPOSE_SEC", "5000")
	t.Setenv("GOPI_TASK_MAX_RUN_SEC", "7200")
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"task_stall_sec": 100, "task_dispose_sec": 7200, "task_max_run_sec": 3600}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)

	sec, err := ResolveTaskStallGrace(0)
	if err != nil {
		t.Fatalf("a retired grace key must not break the surviving resolution: %v", err)
	}
	if sec != 100 {
		t.Errorf("stall = %d, want 100 — a retired knob must have no effect on the surviving one", sec)
	}
}
