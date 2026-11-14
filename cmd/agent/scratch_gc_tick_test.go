package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The resident sweep's two controls, unit-tested — a knob whose off switch does
// not switch anything is the defect class this whole task exists for.

func TestScratchGCInterval_EnvFaces(t *testing.T) {
	t.Setenv(scratch.GCIntervalEnvName, "")
	t.Setenv("GOPI_SETTINGS", filepath.Join(t.TempDir(), "absent.json"))
	// Unset → the default cadence.
	if got, ok := scratchGCInterval(); !ok || got != scratchGCDefaultIntervalMin*time.Minute {
		t.Fatalf("unset: got %v ok=%v, want %v/true", got, ok, scratchGCDefaultIntervalMin*time.Minute)
	}
	// Explicit value → minutes.
	t.Setenv(scratch.GCIntervalEnvName, "45")
	if got, ok := scratchGCInterval(); !ok || got != 45*time.Minute {
		t.Fatalf("45: got %v ok=%v, want 45m/true", got, ok)
	}
	// 0 and negatives disable.
	for _, off := range []string{"0", "-5"} {
		t.Setenv(scratch.GCIntervalEnvName, off)
		if got, ok := scratchGCInterval(); ok || got != 0 {
			t.Fatalf("%s must disable the sweep, got %v ok=%v", off, got, ok)
		}
	}
	// Garbage falls back to the default (reported, never silent).
	t.Setenv(scratch.GCIntervalEnvName, "soon")
	if got, ok := scratchGCInterval(); !ok || got != scratchGCDefaultIntervalMin*time.Minute {
		t.Fatalf("garbage: got %v ok=%v, want the default/true", got, ok)
	}
}

func TestScratchGCSettingsSwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	t.Setenv(scratch.GCIntervalEnvName, "") // env unset: the settings face decides
	t.Setenv("GOPI_SETTINGS", path)

	// No file at all → enabled (the sweep is opt-OUT: a deployment that
	// upgrades into it gets a bounded tree without editing anything).
	if got, ok := scratchGCInterval(); !ok || got != scratchGCDefaultIntervalMin*time.Minute {
		t.Fatalf("no settings file: got %v ok=%v, want the default/true", got, ok)
	}
	// "scratch_gc": false → off.
	if err := os.WriteFile(path, []byte(`{"scratch_gc": false}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if got, ok := scratchGCInterval(); ok || got != 0 {
		t.Fatalf(`"scratch_gc": false must disable the sweep, got %v ok=%v`, got, ok)
	}
	// "scratch_gc": true → the default cadence.
	if err := os.WriteFile(path, []byte(`{"scratch_gc": true}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if got, ok := scratchGCInterval(); !ok || got != scratchGCDefaultIntervalMin*time.Minute {
		t.Fatalf(`"scratch_gc": true must enable the default cadence, got %v ok=%v`, got, ok)
	}
	// A malformed file must NOT disable disk hygiene (fail-open, Rule 15): the
	// sweep stays on rather than silently stopping.
	if err := os.WriteFile(path, []byte(`{"scratch_gc": `), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if got, ok := scratchGCInterval(); !ok || got != scratchGCDefaultIntervalMin*time.Minute {
		t.Fatalf("malformed settings: got %v ok=%v, want the default/true", got, ok)
	}
	// The env face always wins over the settings face.
	if err := os.WriteFile(path, []byte(`{"scratch_gc": false}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(scratch.GCIntervalEnvName, "15")
	if got, ok := scratchGCInterval(); !ok || got != 15*time.Minute {
		t.Fatalf("env must win over settings: got %v ok=%v, want 15m/true", got, ok)
	}
}

// TestScratchGCTick_ThrottleAndFailOpen pins the tick's two contracts: it runs
// at most once per interval, and a sweep problem is never propagated to the
// caller (the host task's round must be unaffected).
func TestScratchGCTick_ThrottleAndFailOpen(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	home := t.TempDir()
	t.Setenv(scratch.HomeEnvName, home)
	t.Setenv(scratch.GCIntervalEnvName, "10")
	// The tick's return value is the SUM of its arms, so the two arms the
	// fixtures below do not build must be pointed at sandboxes of their own —
	// otherwise a residue the OPERATOR's host happens to hold (an abandoned
	// `sandbox-*` past its 72h age, say) is counted into this assertion and the
	// test reports a defect in a tick that behaved exactly as specified.
	// Measured 2026-10-01: with the real sandbox root in force, `first tick
	// removed 2, want 1`, the extra one being
	// `removed 1 abandoned sandbox(es) … from /home/user/workspace/gopi-sandbox`.
	// Both are pinned EMPTY, which is the same "this arm finds nothing" state the
	// fixtures rely on for the base arm.
	t.Setenv(scratch.ResolveBaseEnvName, t.TempDir())
	t.Setenv(config.SandboxEnvName, t.TempDir())
	scratchGCLastTick = time.Time{} // isolate the process-wide clock

	old := time.Now().Add(-2 * scratch.DefaultStaleAge)
	root := filepath.Join(home, "suite-tick-run-1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chtimes(root, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	now := time.Now()
	if got := scratchGCTick(workerRoleLabel, now); got != 1 {
		t.Fatalf("first tick removed %d, want 1", got)
	}
	// A second tick inside the interval does nothing (and must not sweep the
	// fresh fixture a suite would be holding).
	fresh := filepath.Join(home, "suite-tick-run-2")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chtimes(fresh, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if got := scratchGCTick(workerRoleLabel, now.Add(time.Minute)); got != 0 {
		t.Fatalf("throttled tick removed %d, want 0", got)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("the throttled tick swept anyway: %v", err)
	}
	// Past the interval it runs again.
	if got := scratchGCTick(workerRoleLabel, now.Add(11*time.Minute)); got != 1 {
		t.Fatalf("tick past the interval removed %d, want 1", got)
	}

	// Fail-open: a home path that is a FILE makes the sweep error, and the tick
	// must return without panicking (never failing the host task).
	scratchGCLastTick = time.Time{}
	t.Setenv(scratch.HomeEnvName, filepath.Join(home, "suite-tick-run-9"))
	if err := os.WriteFile(filepath.Join(home, "suite-tick-run-9"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := scratchGCTick(workerRoleLabel, time.Now()); got != 0 {
		t.Fatalf("a failing sweep must remove nothing, got %d", got)
	}
	// Disabled → no sweep at all, not even a stat.
	scratchGCLastTick = time.Time{}
	t.Setenv(scratch.GCIntervalEnvName, "0")
	stale := filepath.Join(home, "suite-tick-run-3")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv(scratch.HomeEnvName, home)
	if got := scratchGCTick(workerRoleLabel, time.Now()); got != 0 {
		t.Fatalf("a disabled sweep must do nothing, got %d", got)
	}
}
