// cli_checkpoint_bounded_test.go — the CLI face of the bounded scan: a sessions
// root too large to walk is refused BY NAME, with the next step spelled out,
// and the command RETURNS.
//
// THE DEFECT (measured on this host, 2026-10-07): `gopi session checkpoint list`
// against the operator's own ~/.gopi/sessions (83956 first-level entries) was
// killed by `timeout -s KILL 30` with rc=137 and NOT ONE BYTE of output —
// real 30.0s / user 0.001s, i.e. blocked on I/O, not computing. Rule 15's worst
// case is not the hang itself but the SILENCE: there was nothing to read, before
// or after.
//
// WHAT THIS PINS, through argv (Rule 31 — the binary, not the function):
//   - a root one entry over the bound exits NON-ZERO, fast, and says why,
//   - the refusal names the numbers (entries, limit) and the two channels that
//     narrow the scan (--dir, GOPI_RAW_LOG_DIR) — an operator who cannot act on
//     the message has been told nothing,
//   - stderr carries it (stdout stays a clean, empty listing; a refusal is not a
//     result),
//   - the probe is TIME-GUARDED by the shared helper, so this test fails loud if
//     the walk ever comes back.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/checkpoint"
	"github.com/nickhuang99/gopi/internal/config"
)

// cpbOversizedProfile builds a private profile whose sessions root holds one
// more entry than the indexer is willing to descend into.
func cpbOversizedProfile(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	sessions := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	for i := 0; i <= checkpoint.MaxScanRootEntries; i++ {
		if err := os.Mkdir(filepath.Join(sessions, fmt.Sprintf("run-%06d", i)), 0o700); err != nil {
			t.Fatalf("mkdir child %d: %v", i, err)
		}
	}
	return home
}

// TestCheckpointListOversizedRootIsRefusedByName is the defect's green at the
// surface an operator drives.
func TestCheckpointListOversizedRootIsRefusedByName(t *testing.T) {
	home := cpbOversizedProfile(t)
	stdout, stderr, code := cprRunCheckpointList(t, gopiTestBinPath(t), cliContractEnv(home), "", cprListSubcommand)
	if code == 0 {
		t.Fatalf("`gopi session checkpoint %s` exited 0 over a root it cannot walk:\n--- stdout ---\n%s\n--- stderr ---\n%s",
			cprListSubcommand, stdout, stderr)
	}
	if !strings.Contains(stderr, fmt.Sprintf("%d", checkpoint.MaxScanRootEntries)) {
		t.Errorf("the refusal does not name the limit (%d):\n%s", checkpoint.MaxScanRootEntries, stderr)
	}
	if !strings.Contains(stderr, checkpoint.RefusalScanRootTooLarge) {
		t.Errorf("the refusal does not name itself (%q) — Rule 15: a refusal is identified, not just "+
			"declined:\n%s", checkpoint.RefusalScanRootTooLarge, stderr)
	}
	// THE NEXT STEP, both channels, spelled so an operator can copy it. The
	// spelling is DERIVED from the constants the dispatcher switches on, so a
	// refusal that advises a flag nobody answers fails here (measured: a first
	// draft printed "--dirdir DIR").
	for _, want := range []string{checkpointFlagPrefix + checkpointDirFlag + " DIR", rawLogDirEnv + "=DIR"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal does not offer %q as the way to narrow the scan — a refusal "+
				"without a next step is the silence this epic was filed for:\n%s", want, stderr)
		}
	}
	// The page's own name for the channel, so the two faces of one channel are
	// not spelled differently.
	if !strings.Contains(stderr, cpRawLogDirSettingsKey) {
		t.Errorf("the refusal does not name the %q knob — the reader cannot find its row in "+
			"CONFIG.md:\n%s", cpRawLogDirSettingsKey, stderr)
	}
	if strings.Contains(stdout, cprListColumn) {
		t.Errorf("a refusal must not print a listing table:\n%s", stdout)
	}
}
