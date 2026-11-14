package config

// delivery_repo_path_test.go — the regression guard for the PATH half of a
// declared delivery point (delivery_repo_path.go).
//
// THE DEFECT (measured 2026-10-25): three delivery legs each hardcoded the peer
// checkout path (`~/work/GoPi`) while the host declared a different one under
// a different spelling. These cases pin the reading half — that the declared
// value is what comes back, WITH the channel that declared it (Rule 15), and
// that the repos table is readable without a second parse of the settings
// document.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// deliveryRepoTestSettings writes a settings document into a fresh GOPI_HOME and
// points the config layer at it (Rule 32: the environment is pinned, never the
// operator's own file).
func deliveryRepoTestSettings(t *testing.T, doc map[string]any) string {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SettingsPathEnv, path)
	return path
}

// TestDeliveryRepoDeclaredPrefersTheEnvChannel pins Rule 2's precedence at this
// reader: the env channel wins over the settings document, and the CHANNEL NAME
// travels back with the value so a caller can say which declaration is in
// force rather than presenting a value with no provenance.
func TestDeliveryRepoDeclaredPrefersTheEnvChannel(t *testing.T) {
	deliveryRepoTestSettings(t, map[string]any{DevRepoSettingsKey: "peer1:/home/user/work/gopi"})
	t.Setenv(DevRepoEnvName, "peer:/srv/from-env")

	got, source, err := DeliveryRepoDeclared()
	if err != nil {
		t.Fatalf("DeliveryRepoDeclared: %v", err)
	}
	if got != "peer:/srv/from-env" {
		t.Fatalf("DeliveryRepoDeclared = %q, want the env value (Rule 2 precedence)", got)
	}
	if source != DevRepoEnvName {
		t.Errorf("channel = %q, want %q — the declaration's provenance is part of the answer (Rule 15)", source, DevRepoEnvName)
	}
}

// TestDeliveryRepoDeclaredFallsBackToTheSettingsDocument is the other half of
// the same precedence: with no env value the document decides, and the channel
// names the KEY, not the file.
func TestDeliveryRepoDeclaredFallsBackToTheSettingsDocument(t *testing.T) {
	deliveryRepoTestSettings(t, map[string]any{DevRepoSettingsKey: "peer1:/home/user/work/gopi"})
	t.Setenv(DevRepoEnvName, "")

	got, source, err := DeliveryRepoDeclared()
	if err != nil {
		t.Fatalf("DeliveryRepoDeclared: %v", err)
	}
	if got != "peer1:/home/user/work/gopi" {
		t.Fatalf("DeliveryRepoDeclared = %q, want the settings value", got)
	}
	if source != "settings "+DevRepoSettingsKey {
		t.Errorf("channel = %q, want %q", source, "settings "+DevRepoSettingsKey)
	}
}

// TestDeliveryRepoDeclaredEmptyIsNotAnError pins the single-node shape: nothing
// declared is a CONDITION ("no delivery point"), never a failure — the callers
// treat it as their fail-soft arm, and an error here would make a valid topology
// look broken.
func TestDeliveryRepoDeclaredEmptyIsNotAnError(t *testing.T) {
	deliveryRepoTestSettings(t, map[string]any{})
	t.Setenv(DevRepoEnvName, "")

	got, source, err := DeliveryRepoDeclared()
	if err != nil {
		t.Fatalf("no declared delivery point must not be an error: %v", err)
	}
	if got != "" || source != "" {
		t.Fatalf("DeliveryRepoDeclared = (%q, %q), want empty/empty", got, source)
	}
}

// TestDeliveryReposTableReadsTheKeysTheMergerSweeps proves the second arm's
// reading half: the table's KEYS are the paths (the same table
// ResolveMergerRepos sweeps), so a host that declares its system repo there is
// answering the resolver's question.
func TestDeliveryReposTableReadsTheKeysTheMergerSweeps(t *testing.T) {
	deliveryRepoTestSettings(t, map[string]any{
		"repos": map[string]any{
			"/home/user/work/gopi": map[string]any{"build": "make build"},
			"/srv/other":           map[string]any{},
		},
	})

	table, err := DeliveryReposTable()
	if err != nil {
		t.Fatalf("DeliveryReposTable: %v", err)
	}
	if len(table) != 2 {
		t.Fatalf("table = %v, want the two declared keys", table)
	}
	if _, ok := table["/home/user/work/gopi"]; !ok {
		t.Errorf("table keys = %v, want /home/user/work/gopi among them", table)
	}

	swept, err := ResolveMergerRepos()
	if err != nil {
		t.Fatalf("ResolveMergerRepos: %v", err)
	}
	if len(swept) != len(table) {
		t.Errorf("the sweep list (%v) and the delivery reading (%v) must be the SAME table — one parse, two questions", swept, table)
	}
}

// TestDeliveryReposTableReportsABrokenDocument pins Rule 15 on this arm: an
// unparseable settings file is an ERROR, never an empty table. An empty table
// would let the resolver fall through to its last-resort constant on a broken
// configuration, which is exactly the "silently wrong destination" the whole
// resolution exists to prevent.
func TestDeliveryReposTableReportsABrokenDocument(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SettingsPathEnv, path)

	if _, err := DeliveryReposTable(); err == nil {
		t.Fatal("a malformed settings document must be an error, not an empty table")
	}
}
