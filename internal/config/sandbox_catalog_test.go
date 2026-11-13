package config

import (
	"strings"
	"testing"
)

// sandbox_catalog_test.go — the regression guard for the two sandbox knobs
// having LEFT the uncatalogued set (2026-09-27).
//
// WHY IT EXISTS: both knobs were live in code for days while `internal/config`'s
// catalog carried NO row for either — so `gopi --help-env` never listed them,
// the flag⇒env map could not reach them, and documentation gate §2c had to
// record them as rows it could not score. The repair was a CATALOG change; this
// test is what fails if the rows are ever removed again, i.e. the state that
// produced the finding rather than the finding itself.

// TestSandboxKnobsAreCatalogued pins one EnvVars row per knob, with the flag
// face the renderer produces: the root carries the agent-level `--sandbox-dir`
// (CLIFlag stored bare/dashless, `--` added by envFlagLabel), the seed carries
// no flag at all — the em dash the flag gate's flagless arm scores.
func TestSandboxKnobsAreCatalogued(t *testing.T) {
	byEnv := envByName()

	root, ok := byEnv[SandboxEnvName]
	if !ok {
		t.Fatalf("%s has no catalog row — it is a live knob, and an uncatalogued knob "+
			"has no `gopi --help-env` face and no env-named flag reach", SandboxEnvName)
	}
	if root.CLIFlag != SandboxFlagName || root.Key != SandboxSettingsKey {
		t.Errorf("%s row carries key=%q flag=%q, want key=%q flag=%q",
			SandboxEnvName, root.Key, root.CLIFlag, SandboxSettingsKey, SandboxFlagName)
	}
	if got := envFlagLabel(root); got != "--"+SandboxFlagName {
		t.Errorf("%s renders flag column %q, want %q — the column `gopi --help-env` and "+
			"CONFIG.md §2c are checked against", SandboxEnvName, got, "--"+SandboxFlagName)
	}

	seed, ok := byEnv[SandboxLedgerSeedEnvName]
	if !ok {
		t.Fatalf("%s has no catalog row — it is a live knob read through "+
			"SandboxLedgerSeedValue", SandboxLedgerSeedEnvName)
	}
	if seed.CLIFlag != "" || seed.Key != "" {
		t.Errorf("%s is environment-only, but its row carries key=%q flag=%q",
			SandboxLedgerSeedEnvName, seed.Key, seed.CLIFlag)
	}
	if got := envFlagLabel(seed); got != EnvVarsNoFlagMarker {
		t.Errorf("%s renders flag column %q, want the no-flag marker %q",
			SandboxLedgerSeedEnvName, got, EnvVarsNoFlagMarker)
	}
}

// TestSandboxLedgerSeedValueIsTheReader is the other half of the same fact: the
// catalog row is a promise about a knob the process actually honours, so the
// env channel must have a reader IN THIS PACKAGE (gate 6/Rule 21 — the first cut
// declared the name here and read it in internal/sandbox, which is exactly the
// unread-hook defect the gate reported).
func TestSandboxLedgerSeedValueIsTheReader(t *testing.T) {
	t.Setenv(SandboxLedgerSeedEnvName, "  /tmp/seed.db  ")
	if got := SandboxLedgerSeedValue(); got != "/tmp/seed.db" {
		t.Errorf("SandboxLedgerSeedValue() = %q, want the trimmed env value %q",
			got, "/tmp/seed.db")
	}
	t.Setenv(SandboxLedgerSeedEnvName, "   ")
	if got := SandboxLedgerSeedValue(); got != "" {
		t.Errorf("a whitespace-only seed resolved to %q, want empty (no seed)", got)
	}
}

// TestSandboxKnobsRenderInTheSectionedTable: the rows must land in a named
// section, not the trailing "Other" catch-all — an operator looking for the
// sandbox root looks under a heading that names it.
func TestSandboxKnobsRenderInTheSectionedTable(t *testing.T) {
	table := FormatEnvTable()
	heading := -1
	rootRow, seedRow := -1, -1
	for i, line := range strings.Split(table, "\n") {
		switch {
		case strings.HasPrefix(line, "Temp workspace"):
			heading = i
		case strings.HasPrefix(line, "  "+SandboxEnvName):
			rootRow = i
		case strings.HasPrefix(line, "  "+SandboxLedgerSeedEnvName):
			seedRow = i
		}
	}
	if heading < 0 || rootRow < heading || seedRow < heading {
		t.Fatalf("heading=%d root=%d seed=%d: both sandbox rows must appear under a named "+
			"section heading in `gopi --help-env`", heading, rootRow, seedRow)
	}
	if strings.Contains(table, "\nOther\n") &&
		strings.Index(table, "\nOther\n") < rootRow {
		t.Error("the sandbox rows landed in the trailing \"Other\" catch-all")
	}
}
