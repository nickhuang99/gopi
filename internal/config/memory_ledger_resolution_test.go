package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The legend of this file: settings.json's `memory_db` was a DOCUMENTED channel
// of the ledger path (the catalog row for GOPI_MEMORY_DB, CONFIG.md §2c) that
// NOTHING read — an operator who declared it got the built-in default silently
// (Rule 15: "asked for something I could not honour" and "nobody asked" must not
// look alike). These tests pin the ranked resolution that closed it, on the
// RESOLVER the loader, `gopi config show` and the branch-attribution report all
// call — never on a re-implementation of the ranking.

// ledgerTestHome pins the profile the resolution hangs its default from, so the
// rank-4 assertion names a path this test owns instead of the operator's real
// ~/.gopi (Rule 34's spirit: a test never reads the operator's profile).
func ledgerTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	return home
}

// ledgerTestClearEnv masks BOTH env overrides for the duration of one test. It
// is explicit rather than implied because this package's suite entry point
// PINS GOPI_MEMORY_DB (testutil.PinSuiteLedgerEnv — a leaked fixture row in the
// operator's ledger is permanent), so "no env channel" is a state this test must
// ASK for, never one it inherits.
func ledgerTestClearEnv(t *testing.T) {
	t.Helper()
	t.Setenv(MemoryDBEnv, "")
	t.Setenv(MemoryDBEnvLegacy, "")
}

// ledgerTestSettings writes a settings file carrying `memory_db` and points the
// settings resolution at it — the channel under test.
func ledgerTestSettings(t *testing.T, memoryDB string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, defaultSettingsFileNameForTest)
	body, err := json.Marshal(map[string]string{LedgerSettingsKey: memoryDB})
	if err != nil {
		t.Fatalf("marshal the settings fixture: %v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write the settings fixture %s: %v", path, err)
	}
	t.Setenv(SettingsEnv, path)
	return path
}

// defaultSettingsFileNameForTest is the fixture's file name. It is deliberately
// NOT DefaultSettingsPath()'s name: the fixture lives in its own temp dir and
// the settings CHANNEL is selected by GOPI_SETTINGS, so the name is free — and
// using the production name here would suggest the fixture is the profile's own
// file, which it is not.
const defaultSettingsFileNameForTest = "settings.json"

// TestLedgerResolutionSettingsKeyIsHonoured is the core pin: with no flag and no
// env override, the path declared in settings.json is the path resolved — and
// the source says so, so a reporter can print the channel that decided.
func TestLedgerResolutionSettingsKeyIsHonoured(t *testing.T) {
	ledgerTestHome(t)
	ledgerTestClearEnv(t)
	declared := filepath.Join(t.TempDir(), "declared.db")
	ledgerTestSettings(t, declared)

	got, source := ResolveLedger("")
	if got != declared {
		t.Fatalf("ResolveLedger = %q, want the declared %q — the settings key %s is documented as a channel "+
			"and must decide when no higher-ranked channel speaks", got, declared, LedgerSettingsKey)
	}
	if source != LedgerSettingsKey {
		t.Errorf("source = %q, want %q (the report must name the key, not the value's luck)", source, LedgerSettingsKey)
	}
	if p := LedgerPath(); p != declared {
		t.Errorf("LedgerPath = %q, want %q — the convenience wrapper and the ranked resolution must agree", p, declared)
	}
}

// TestLedgerResolutionRankOrder is the precedence pin, in ONE place, over the
// REAL resolver: flag > env (canonical) > env (legacy alias) > settings >
// default. Each rung is added in turn and asserted on the same call, so a
// re-ranked chain fails here rather than in whichever consumer noticed.
func TestLedgerResolutionRankOrder(t *testing.T) {
	home := ledgerTestHome(t)
	ledgerTestClearEnv(t)
	declared := filepath.Join(t.TempDir(), "settings.db")
	ledgerTestSettings(t, declared)
	envPath := filepath.Join(t.TempDir(), "env.db")
	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	flagPath := filepath.Join(t.TempDir(), "flag.db")
	defaultPath := filepath.Join(home, ProfileDirName, MemoryDBFileName)

	// absentSettings is the fresh-host shape: no settings file at all, so rank 3
	// cannot speak and rank 4 must decide.
	absentSettings := filepath.Join(t.TempDir(), "absent.json")

	cases := []struct {
		name     string
		flag     string
		env      string
		legacy   string
		settings string
		want     string
		sources  string
	}{
		{"nothing set → the profile default", "", "", "", absentSettings, defaultPath, LedgerSourceDefault},
		{"settings key decides alone", "", "", "", "", declared, LedgerSettingsKey},
		{"env beats the settings key", "", envPath, "", "", envPath, MemoryDBEnv},
		{"the legacy alias ranks below the canonical env", "", "", legacyPath, "", legacyPath, MemoryDBEnvLegacy},
		{"the flag beats every env channel", flagPath, envPath, legacyPath, "", flagPath, LedgerSourceFlag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledgerTestClearEnv(t)
			t.Setenv(MemoryDBEnv, tc.env)
			t.Setenv(MemoryDBEnvLegacy, tc.legacy)
			if tc.settings != "" {
				t.Setenv(SettingsEnv, tc.settings)
			}
			got, source := ResolveLedger(tc.flag)
			if got != tc.want {
				t.Fatalf("ResolveLedger = %q, want %q (%s)", got, tc.want, tc.name)
			}
			if source != tc.sources {
				t.Errorf("source = %q, want %q", source, tc.sources)
			}
		})
	}
}

// TestLedgerResolutionEmptySettingsKeyFallsThrough is the zero-value case Rule
// 15a demands be tested: a settings file that carries the key EMPTY (or carries
// only other keys) must not resolve to "" — an empty declaration is no
// declaration, and the default decides.
func TestLedgerResolutionEmptySettingsKeyFallsThrough(t *testing.T) {
	home := ledgerTestHome(t)
	ledgerTestSettings(t, "")
	ledgerTestClearEnv(t)

	got, source := ResolveLedger("")
	want := filepath.Join(home, ProfileDirName, MemoryDBFileName)
	if got != want {
		t.Fatalf("an empty %s resolved to %q, want the default %q", LedgerSettingsKey, got, want)
	}
	if source != LedgerSourceDefault {
		t.Errorf("source = %q, want %q", source, LedgerSourceDefault)
	}
}

// TestLedgerResolutionAbsentSettingsFileIsNotAnError: no settings file at all is
// the ordinary fresh-host shape (FromSettingsFile documents defaults-on-missing),
// so the resolution must fall through to the default rather than report a fault.
func TestLedgerResolutionAbsentSettingsFileIsNotAnError(t *testing.T) {
	home := ledgerTestHome(t)
	t.Setenv(SettingsEnv, filepath.Join(t.TempDir(), "does-not-exist.json"))
	ledgerTestClearEnv(t)

	got, source := ResolveLedger("")
	if want := filepath.Join(home, ProfileDirName, MemoryDBFileName); got != want {
		t.Fatalf("ResolveLedger = %q, want the default %q when no settings file exists", got, want)
	}
	if source != LedgerSourceDefault {
		t.Errorf("source = %q, want %q", source, LedgerSourceDefault)
	}
}

// TestLedgerCatalogRowNamesTheConstantsItResolves pins the catalog row to the
// constants the resolution reads (Rule 13: the row, the docs and the code name
// one spelling). A row re-spelled by hand is how a knob stops being reachable —
// the EXACT defect this row was silently carrying in its settings-key column.
func TestLedgerCatalogRowNamesTheConstantsItResolves(t *testing.T) {
	var found int
	for _, e := range EnvVars {
		if e.Env != MemoryDBEnv {
			continue
		}
		found++
		if e.Key != LedgerSettingsKey {
			t.Errorf("catalog row %s carries settings key %q, want %q (the key the resolution reads)",
				MemoryDBEnv, e.Key, LedgerSettingsKey)
		}
		if e.CLIFlag != LedgerFlagName {
			t.Errorf("catalog row %s carries CLI flag %q, want %q", MemoryDBEnv, e.CLIFlag, LedgerFlagName)
		}
	}
	if found != 1 {
		t.Fatalf("found %d catalog rows for %s, want exactly 1 — a duplicated row is how two spellings appear", found, MemoryDBEnv)
	}
}

// TestHelpEnvLedgerRowShowsTheFlag is the table's own assertion (the epic's
// deliverable 3): the rendered `gopi --help-env` column for the ledger row is
// the flag, not the flagless em dash. It reads the RENDERED table — what the
// operator sees — and compares it against the same rendering the doc gate uses
// (EnvVarsFlagLabels), so the help, the doc and this assertion are one fact.
func TestHelpEnvLedgerRowShowsTheFlag(t *testing.T) {
	rendered := envFlagColumnOf(t, FormatEnvTable())
	got, ok := rendered[MemoryDBEnv]
	if !ok {
		t.Fatalf("the rendered env table carries no %s row — the table parser is measuring nothing", MemoryDBEnv)
	}
	want := flagPrefix + LedgerFlagName
	if got != want {
		t.Fatalf("%s renders flag column %q, want %q — the row must not hide a working flag behind the em dash",
			MemoryDBEnv, got, want)
	}
	// The label version the documentation gate compares against must agree with
	// the rendered cell (they are the same function today; this fails if a
	// second rendering is ever introduced).
	for _, l := range EnvVarsFlagLabels() {
		if l.Env == MemoryDBEnv && l.Flag != want {
			t.Fatalf("EnvVarsFlagLabels() reports %q for %s, the rendered table says %q", l.Flag, MemoryDBEnv, got)
		}
	}
	if !strings.Contains(FormatUsage(), want) {
		t.Errorf("`gopi -h` does not name %s anywhere, so the ledger flag is advertised only in the long table", want)
	}
}

// envFlagColumnOf maps env name → flag column for the rendered env table. It is
// a LOCAL copy of the parse the doc gate performs (that one is test-private to
// internal/standards and cannot be imported); the shapes it must accept are the
// rendered rows, and the ledger row is two-space indented with the flag last.
func envFlagColumnOf(t *testing.T, table string) map[string]string {
	t.Helper()
	col := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		rest := line[2:]
		if rest == "" || strings.HasPrefix(rest, " ") {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if name != strings.ToUpper(name) || !strings.Contains(name, "_") {
			continue
		}
		if _, dup := col[name]; dup {
			continue
		}
		col[name] = fields[len(fields)-1]
	}
	return col
}
