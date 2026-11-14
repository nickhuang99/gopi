package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// REGRESSION GATE for the settings.json `memory_db` VALUE, driven through the
// REAL binary.
//
// WHAT THIS PINS. The catalog row for GOPI_MEMORY_DB documented `memory_db` as
// the settings channel of the ledger path, and nothing read it: the declared
// file was not even created, and every surface fell through to the built-in
// default (Rule 15 — a documented channel that silently does nothing). The
// resolution now lives in ONE place (config.ResolveLedger) and reaches
// internal/tools' ledger opener, but the property under test is a SPAWNED
// PROCESS's choice of file, which no in-process unit test can observe: only the
// binary can say which ledger the run actually opened.
//
// The probe is deliberately the CHEAPEST real verb that opens the ledger
// (`gopi memory search`, which creates the knowledge base on first use) — no
// model, no task row, no daemon. The binary built is this package's own
// production build (requireE2EGopiBin, which FAILS rather than skips when
// the build cannot be produced: a skipped regression guard reads as a green
// one), and the observation is the FILESYSTEM afterwards, not the process's
// self-report.
//
// The settings file, the declared ledger and the profile all live under the
// test's own temp dirs; the operator's profile is never read or written
// (Rule 34's spirit). The child env is testutil.MockEnv with GOPI_SETTINGS
// pointed at the fixture, so the sandbox is explicit and complete.

const (
	// ledgerSettingsFileName is the fixture settings file's name.
	ledgerSettingsFileName = "settings.json"
	// ledgerDeclaredSubdir is the directory under the probe root that holds the
	// DECLARED ledger: it does not exist until gopi creates it, so "the file is
	// there afterwards" cannot be satisfied by something else having made it.
	ledgerDeclaredSubdir = "declared"
	// ledgerDeclaredFileName / ledgerDefaultFileName name the two files the
	// probe distinguishes — the declared ledger, and the profile default the
	// pre-fix behaviour would have created instead. The default's file name comes
	// from the config package (Rule 14), never a bare literal; the declared one
	// is this fixture's own and is named here (Rule 1b).
	ledgerDeclaredFileName = "declared-ledger.db"
	ledgerDefaultFileName  = config.MemoryDBFileName
	// ledgerQuery is the search term — never distilled, so it cannot depend on
	// the ledger's contents.
	ledgerQuery = "slice7-memory-db-settings-probe"
)

// ledgerProbeEnv is the child's environment: the suite's mocked sandbox with the
// fixture settings file selected. GOPI_SETTINGS is the ONLY configuration the
// probe passes — no GOPI_MEMORY_DB, no --db — so the settings key is the only
// channel that can decide.
func ledgerProbeEnv(t *testing.T, gopiHome, settingsPath string) []string {
	t.Helper()
	base := testutil.MockEnv(t, gopiHome, ledgerProbeMockURL)
	return append(base, config.SettingsEnv+"="+settingsPath)
}

// ledgerProbeMockURL is a closed loopback port: the probe makes no model call,
// and a run that somehow tried would fail fast and visibly rather than reaching
// a real provider.
const ledgerProbeMockURL = "http://127.0.0.1:1/"

// ledgerProbeSettings writes the fixture settings file. body is the raw JSON, so
// the "no key" case and the "key set" case differ by exactly the text an operator
// would write.
func ledgerProbeSettings(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, ledgerSettingsFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the settings fixture %s: %v", path, err)
	}
	return path
}

// ledgerProbeSearch runs `gopi memory search <term>` under env and returns the
// captured output plus the exit code. The binary is this tree's build (stamped
// into GOPI_BIN by the resolver), never whichever bin/gopi a shared host
// happened to have.
func ledgerProbeSearch(t *testing.T, env []string, dir string) (string, string, int) {
	t.Helper()
	requireE2EGopiBin(t)
	return runGopiMapped(t, []string{CliVerbMemory, memorySearchVerb, ledgerQuery}, "", env, dir)
}

// memorySearchVerb is the `gopi memory` sub-verb the probe drives.
const memorySearchVerb = "search"

// TestMemoryDBSettingsKeyOpensTheDeclaredLedger is the epic's acceptance on the
// real surface: a settings.json declaring `memory_db` makes the run CREATE and
// open that file, and it does NOT create the profile default.
func TestMemoryDBSettingsKeyOpensTheDeclaredLedger(t *testing.T) {
	root := t.TempDir()
	declaredDir := filepath.Join(root, ledgerDeclaredSubdir)
	if err := os.MkdirAll(declaredDir, 0o700); err != nil {
		t.Fatalf("create the declared ledger's directory %s: %v", declaredDir, err)
	}
	declared := filepath.Join(declaredDir, ledgerDeclaredFileName)
	settingsPath := ledgerProbeSettings(t, root, fmt.Sprintf("{%q: %q}\n", config.LedgerSettingsKey, declared))

	gopiHome := t.TempDir()
	stdout, stderr, code := ledgerProbeSearch(t, ledgerProbeEnv(t, gopiHome, settingsPath), root)
	if code != 0 {
		t.Fatalf("gopi memory search exited %d — the probe could not run, so it proves nothing\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}

	if _, err := os.Stat(declared); err != nil {
		t.Fatalf("the declared ledger %s was NOT created: %v\n"+
			"the settings key %s is a documented channel of the ledger path; a run under it must open THAT file\n"+
			"stdout:\n%s\nstderr:\n%s", declared, err, config.LedgerSettingsKey, stdout, stderr)
	}
	if defaultPath := filepath.Join(gopiHome, config.ProfileDirName, ledgerDefaultFileName); fileExistsAt(defaultPath) {
		t.Errorf("the run ALSO created the profile default %s — the settings channel and the default cannot both decide",
			defaultPath)
	}
	t.Logf("settings %s=%q → the run created and opened exactly that file; no profile default was created",
		config.LedgerSettingsKey, declared)
}

// TestMemoryDBSettingsKeyAbsentFallsBackToTheDefault is the probe's own CONTROL,
// and the reason the case above is capable of failing: with the SAME probe and a
// settings file that does not carry the key, the profile default is what gets
// created and the declared path stays untouched. Without this case the green
// assertion above could be satisfied by a probe that creates both files, or by a
// resolution that ignores the key and always uses the default (which is exactly
// the pre-fix behaviour).
func TestMemoryDBSettingsKeyAbsentFallsBackToTheDefault(t *testing.T) {
	root := t.TempDir()
	declaredDir := filepath.Join(root, ledgerDeclaredSubdir)
	declared := filepath.Join(declaredDir, ledgerDeclaredFileName)
	settingsPath := ledgerProbeSettings(t, root, "{}\n")

	gopiHome := t.TempDir()
	stdout, stderr, code := ledgerProbeSearch(t, ledgerProbeEnv(t, gopiHome, settingsPath), root)
	if code != 0 {
		t.Fatalf("gopi memory search exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	defaultPath := filepath.Join(gopiHome, config.ProfileDirName, ledgerDefaultFileName)
	if _, err := os.Stat(defaultPath); err != nil {
		t.Fatalf("with no %s key the run must fall back to the profile default %s, which does not exist: %v\n"+
			"stdout:\n%s\nstderr:\n%s", config.LedgerSettingsKey, defaultPath, err, stdout, stderr)
	}
	if _, err := os.Stat(declared); err == nil {
		t.Errorf("the declared path %s was created although the settings file carries no %s key",
			declared, config.LedgerSettingsKey)
	}
	t.Logf("no %s key → the run created the profile default %s (the control that makes the declared-ledger case falsifiable)",
		config.LedgerSettingsKey, defaultPath)
}

// TestConfigShowNamesOneLedgerInBothSections is deliverable 4's guard: ONE
// `gopi config show` cannot show the settings.json section declaring one ledger
// and the identity section naming another. Before the fix the identity line
// announced "built-in default" while the section below it printed the declared
// path — one report, two ledgers, and the wrong one named as in force.
func TestConfigShowNamesOneLedgerInBothSections(t *testing.T) {
	root := t.TempDir()
	declared := filepath.Join(root, ledgerDeclaredSubdir, ledgerDeclaredFileName)
	settingsPath := ledgerProbeSettings(t, root, fmt.Sprintf("{%q: %q}\n", config.LedgerSettingsKey, declared))
	gopiHome := t.TempDir()

	requireE2EGopiBin(t)
	stdout, stderr, code := runGopiMapped(t,
		[]string{CliVerbConfig, configShowVerb, "--settings", settingsPath},
		"", ledgerProbeEnv(t, gopiHome, ""), root)
	if code != 0 {
		t.Fatalf("gopi config show exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	identity := configShowLedgerLine(t, stdout)
	section := configShowSettingsValue(t, stdout, config.LedgerSettingsKey)
	if !strings.Contains(identity, declared) {
		t.Fatalf("the identity section names %q, which does not carry the declared ledger %q\nreport:\n%s",
			identity, declared, stdout)
	}
	if !strings.Contains(section, declared) {
		t.Fatalf("the settings.json section shows %q, which does not carry the declared ledger %q\nreport:\n%s",
			section, declared, stdout)
	}
	if !strings.Contains(identity, config.LedgerSettingsKey) {
		t.Errorf("the identity line %q does not name the channel (%s) that decided it — a report must say WHY",
			identity, config.LedgerSettingsKey)
	}
	t.Logf("config show names ONE ledger in both sections: identity %q; settings %q", identity, section)
}

// configShowLedgerLine returns the identity section's ledger line, verbatim.
func configShowLedgerLine(t *testing.T, report string) string {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, configShowLedgerLabel) {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("the report carries no %q line:\n%s", configShowLedgerLabel, report)
	return ""
}

// configShowLedgerLabel is the identity section's label for the ledger path — the
// same spelling `gopi config show` prints (Rule 1b: the test and the renderer
// must not disagree about the label they mean).
const configShowLedgerLabel = "memory db / ledger"

// configShowSettingsValue returns the settings.json section's line for key,
// verbatim.
func configShowSettingsValue(t *testing.T, report, key string) string {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == key {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("the report's settings.json section carries no %q key:\n%s", key, report)
	return ""
}
