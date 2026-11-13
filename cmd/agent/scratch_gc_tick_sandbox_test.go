package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// The RESIDENT arm of the sandbox reclaimer must run from the EXISTING tick —
// no new daemon, no new schedule. This test drives scratchGCTick (the function
// the worker and the merger already call) and asserts that a sandbox whose owner
// is gone and that has aged out is collected by it, while a live-held one and
// the configured ROOT survive.
//
// It is the tick-side half of the epic's proof; the CLI-side half is the raw
// demonstration in devdoc/agent-sandbox-unify.md (`gopi tool scratch-gc sandboxes`),
// and the predicate itself is pinned in internal/scratch/sandbox_gc_test.go.
func TestScratchGCTickSweepsAbandonedSandboxes(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "sandboxes")
	t.Setenv(scratch.HomeEnvName, home)       // the suite scratch home (empty: this test is about the sandbox arm)
	t.Setenv(scratch.GCIntervalEnvName, "10") // cadence enabled
	t.Setenv(config.SandboxEnvName, root)     // the sandbox root, from configuration
	scratchGCLastTick = time.Time{}           // isolate the process-wide clock

	// A sandbox with a marker naming a pid that cannot exist.
	dead := filepath.Join(root, scratch.SandboxMarkerPrefix+"tickdemo")
	if err := os.MkdirAll(filepath.Join(dead, config.SandboxSessionsDirName), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dead, err)
	}
	marker := `{"task_id":"t","pid":999999,"start_ticks":1,"created_at":"2026-01-01T00:00:00Z","version":1}`
	if err := os.WriteFile(filepath.Join(dead, config.SandboxOwnerFileName), []byte(marker), 0o644); err != nil {
		t.Fatalf("write the marker: %v", err)
	}
	old := time.Now().Add(-2 * scratch.SandboxStaleAge)
	if err := os.Chtimes(dead, old, old); err != nil {
		t.Fatalf("age %s: %v", dead, err)
	}

	// A sandbox that is FRESH: the same dead owner, but inside its window.
	fresh := filepath.Join(root, scratch.SandboxMarkerPrefix+"freshdemo")
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", fresh, err)
	}
	if err := os.WriteFile(filepath.Join(fresh, scratch.SandboxOwnerFileName), []byte(marker), 0o644); err != nil {
		t.Fatalf("write the marker: %v", err)
	}

	if got := scratchGCTick(workerRoleLabel, time.Now()); got != 1 {
		t.Fatalf("the tick removed %d entries, want 1 (the aged, unowned sandbox)", got)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("the tick left the aged, unowned sandbox %s in place", dead)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the tick removed a sandbox inside its window: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the tick removed the configured sandbox root itself: %v", err)
	}

	// A second tick inside the throttle interval does nothing (the resident arm
	// must not sweep on every call).
	if got := scratchGCTick(workerRoleLabel, time.Now().Add(time.Minute)); got != 0 {
		t.Errorf("the throttled tick removed %d, want 0", got)
	}
}

// TestScratchGCTickSandboxArmFailsOpenAndRespectsTheOffSwitch: the resident arm
// must never be able to fail the host task, and the operator's cadence off switch
// must silence it too (ONE switch for every arm, Rule 2).
func TestScratchGCTickSandboxArmFailsOpenAndRespectsTheOffSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv(scratch.HomeEnvName, home)
	t.Setenv(scratch.GCIntervalEnvName, "10")
	// A root that is the FILESYSTEM ROOT: refused by name, and the tick must
	// report it and continue (fail-open) rather than sweep anything.
	t.Setenv(config.SandboxEnvName, "/")
	scratchGCLastTick = time.Time{}
	if got := scratchGCTick(workerRoleLabel, time.Now()); got != 0 {
		t.Errorf("a refused sandbox root produced %d removals, want 0", got)
	}

	// The off switch silences every arm, sandbox included.
	root := filepath.Join(home, "sandboxes")
	t.Setenv(config.SandboxEnvName, root)
	abandoned := filepath.Join(root, scratch.SandboxMarkerPrefix+"offswitch")
	if err := os.MkdirAll(abandoned, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	marker := `{"pid":999999,"start_ticks":1,"created_at":"2026-01-01T00:00:00Z","version":1}`
	if err := os.WriteFile(filepath.Join(abandoned, scratch.SandboxOwnerFileName), []byte(marker), 0o644); err != nil {
		t.Fatalf("write the marker: %v", err)
	}
	old := time.Now().Add(-2 * scratch.SandboxStaleAge)
	if err := os.Chtimes(abandoned, old, old); err != nil {
		t.Fatalf("age: %v", err)
	}
	t.Setenv(scratch.GCIntervalEnvName, "0")
	scratchGCLastTick = time.Time{}
	if got := scratchGCTick(workerRoleLabel, time.Now()); got != 0 {
		t.Errorf("a disabled sweep produced %d removals, want 0", got)
	}
	if _, err := os.Stat(abandoned); err != nil {
		t.Errorf("a disabled sweep removed the sandbox anyway: %v", err)
	}
}
