package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestSetupProvisionsFreshProfile pins the epic's acceptance clause: ONE
// invocation of `gopi config setup` against an empty profile makes a host run-ready,
// reports what it did, and is idempotent on a second run.
//
// The env is pinned to a temp GopiDir (Rule 17 t.Setenv, Rule 32's rule that a
// test never touches the operator's profile) and the ledger override chain is
// cleared, so the ledger path resolves to the profile's own memory.db.
func TestSetupProvisionsFreshProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")

	report, err := runSetup()
	if err != nil {
		t.Fatalf("runSetup on a fresh profile: %v", err)
	}
	gd := filepath.Join(home, config.ProfileDirName)
	want := []string{
		gd,
		filepath.Join(gd, config.SessionsDirName),
		config.DefaultSettingsPath(),
		filepath.Join(gd, config.MemoryDBFileName),
	}
	for _, p := range want {
		if !fileExistsAt(p) {
			t.Errorf("fresh run did not provision %s\nreport:\n%s", p, report)
		}
	}
	if !strings.Contains(report, setupStateInitialized) {
		t.Errorf("ledger not reported as initialized:\n%s", report)
	}
	if strings.Contains(report, "nothing to do") {
		t.Errorf("fresh run reported nothing to do:\n%s", report)
	}

	// Second run: everything is already there, so it must be a visible no-op.
	report2, err := runSetup()
	if err != nil {
		t.Fatalf("second runSetup: %v", err)
	}
	if !strings.Contains(report2, "nothing to do") {
		t.Errorf("second run was not a no-op:\n%s", report2)
	}
	if strings.Contains(report2, setupStateCreated) {
		t.Errorf("second run claimed to create something:\n%s", report2)
	}
}

// TestSetupLeavesExistingSettingsAlone: an operator's settings.json is never
// rewritten by provisioning.
func TestSetupLeavesExistingSettingsAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(config.MemoryDBEnv, "")
	t.Setenv(config.MemoryDBEnvLegacy, "")

	settings := config.DefaultSettingsPath()
	if _, err := ensureSetupDir(filepath.Dir(settings), setupProfileDirMode); err != nil {
		t.Fatal(err)
	}
	const mine = `{"api_key":"operator-owned"}` + "\n"
	if _, err := ensureSetupFile(settings, []byte(mine), setupSettingsMode); err != nil {
		t.Fatal(err)
	}

	if _, err := runSetup(); err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	got, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mine {
		t.Errorf("setup rewrote an existing settings.json:\ngot  %q\nwant %q", got, mine)
	}
}
