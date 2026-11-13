package scratch

// The stale-window knob's contract, unit-tested at the layer that owns it.
//
// The behaviour under test is the one the epic is about: the sweep's threshold
// must come from OUTSIDE the process (flag/env/settings), with the default as
// the fallback, and a value that cannot be used must be reported rather than
// silently reaped as "reap nothing".

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestResolveStaleAge_EnvFaces pins the environment face of the knob and the
// default it falls back to.
func TestResolveStaleAge_EnvFaces(t *testing.T) {
	t.Setenv(GCStaleAgeEnvName, "")
	if got := ResolveStaleAge(); got != DefaultStaleAge {
		t.Fatalf("unset: got %v, want the default %v", got, DefaultStaleAge)
	}
	if DefaultStaleAge != 6*time.Hour {
		t.Fatalf("the documented default moved: %v (the knob's default is the policy under discussion — change it deliberately)", DefaultStaleAge)
	}
	t.Setenv(GCStaleAgeEnvName, "90")
	if got := ResolveStaleAge(); got != 90*time.Minute {
		t.Fatalf("90: got %v, want 90m", got)
	}
	// A malformed or non-positive value is REPORTED and falls back to the
	// default — never interpreted as "collect nothing" (that is the off switch's
	// job, GCIntervalEnvName=0), because a threshold that can never be exceeded
	// is exactly the defect this knob exists to fix.
	for _, bad := range []string{"soon", "0", "-30", " ", "1.5h"} {
		t.Setenv(GCStaleAgeEnvName, bad)
		if got := ResolveStaleAge(); got != DefaultStaleAge {
			t.Fatalf("%q: got %v, want the default %v", bad, got, DefaultStaleAge)
		}
	}
}

// TestResolveStaleAge_MovesTheSweep is the behavioural half: the window the
// resolver returns is the window SweepHome actually applies, so an entry aged
// BETWEEN the override and the default is collected under the override and kept
// under the default. This is the minimal repro for the epic's finding that the
// sweep's policy — not its mechanism — was what left the tree full.
func TestResolveStaleAge_MovesTheSweep(t *testing.T) {
	home := filepath.Join(t.TempDir(), "scratch-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	// The fixture ages against the DEFAULT (the suite layer's alias), not the
	// override — so the entry sits between the two windows.
	entry := filepath.Join(home, SuiteRunMarker+"staleage-repro-run-1")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatalf("mkdir entry: %v", err)
	}
	aged := time.Now().Add(-90 * time.Minute)
	if err := os.Chtimes(entry, aged, aged); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// DEFAULT window (6h): the entry is young → the sweep must keep it.
	t.Setenv(GCStaleAgeEnvName, "")
	if removed, err := SweepHome(home, ResolveStaleAge()); err != nil || removed != 0 {
		t.Fatalf("default window: removed=%d err=%v, want 0/nil (90m is younger than %v)", removed, err, ResolveStaleAge())
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("the default window collected a 90m-old entry: %v", err)
	}

	// OVERRIDE (30m): the same entry is now stale → the sweep must collect it.
	t.Setenv(GCStaleAgeEnvName, "30")
	if got := ResolveStaleAge(); got != 30*time.Minute {
		t.Fatalf("override did not reach the resolver: %v", got)
	}
	removed, err := SweepHome(home, ResolveStaleAge())
	if err != nil {
		t.Fatalf("override window: %v", err)
	}
	if removed != 1 {
		t.Fatalf("override window: removed=%d, want 1 — the configured window did not reach the sweep", removed)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("the 90m-old entry survived the 30m window: %v", err)
	}
}

// TestResolveStaleAge_HeldEntryIsNeverCollected is the non-regression guard for
// the one rule the window must never override: an entry a LIVE process is using
// is kept however old it is and however small the window. The knob moves the
// threshold, never the safety arm.
//
// The holder is a FOREIGN live process sitting in the entry (a child with
// cmd.Dir = entry), not this test process: the sweep deliberately exempts ITSELF
// (a suite sweeping from inside its own root would otherwise be immortal), so a
// self-hold would assert the wrong arm.
func TestResolveStaleAge_HeldEntryIsNeverCollected(t *testing.T) {
	home := filepath.Join(t.TempDir(), "scratch-home")
	entry := filepath.Join(home, SuiteRunMarker+"held-repro-run-1")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatalf("mkdir entry: %v", err)
	}
	aged := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(entry, aged, aged); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	holder := exec.Command("sleep", "60")
	// Pdeathsig: the deferred kill covers a normal end only — a killed test
	// binary would leave this holder standing in the entry it is measuring.
	holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	holder.Dir = entry
	if err := holder.Start(); err != nil {
		t.Skipf("cannot start a holder process (%v) — the /proc arm is untestable here", err)
	}
	defer func() {
		_ = holder.Process.Kill() //stylegate:ignore kills the fixture holder the test spawned
		_ = holder.Wait()         //stylegate:ignore reaps the fixture holder the test spawned
	}()
	// Give the child a moment to be visible in /proc with the cwd set.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if pid, err := DirHolder(entry); err == nil && pid != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the holder process never became visible in %s", entry)
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(20 * time.Millisecond)
	}

	t.Setenv(GCStaleAgeEnvName, "1") // a one-minute window: everything is "stale"
	removed, err := SweepHome(home, ResolveStaleAge())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 {
		t.Fatalf("a HELD entry was collected (removed=%d) — the window must never override the holder veto", removed)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("the held entry is gone: %v", err)
	}
}
