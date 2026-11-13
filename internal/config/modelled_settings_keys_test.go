package config

// modelled_settings_keys_test.go — the two arms of the 2026-11-13 report bug.
//
// THE DEFECT. `gopi config show` marks a key `(unrecognized key — … so it is
// IGNORED)` by asking ModelledSettingsKeys(), which answered from the Config
// STRUCT's json tags. dev_repo and workspace_root are deliberately NOT struct
// fields (devrepo.go: their only reader is the document's own top level), so the
// verb called two LIVE knobs IGNORED — while the same run's worker banner, three
// lines earlier in its own log, read both of them:
//
//	task <id>: clone channel ON (dev_repo=zbook:/home/nick/work/gopi via settings
//	              dev_repo; workspace_root=/home/nick/workspace via settings workspace_root)
//
// TWO ARMS, and they fail differently:
//
//	① the ANSWER arm — the keys a production read path consumes are reported as
//	   modelled. A registry that lost a row makes this red.
//	② the READ-PATH arm — the registry IS the read path, so a reader added
//	   outside it (a direct rawTopLevelString call in new code) is invisible to
//	   the answer and would re-create the bug. This arm scans the tree.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModelledSettingsKeysCoverBothReadPaths is arm ①: both shapes of production
// reader are modelled, and the settings REPORT is honest about them.
func TestModelledSettingsKeysCoverBothReadPaths(t *testing.T) {
	keys := ModelledSettingsKeys()
	for _, want := range ModelledTopLevelOnlySettingsKeys() {
		if !containsString(keys, want) {
			t.Errorf("ModelledSettingsKeys() does not carry %q — a key with a live reader would be reported IGNORED by `gopi config show`, which is the contradiction this list exists to prevent", want)
		}
	}
	// The two measured casualties, named so a registry that came back EMPTY
	// cannot make the loop above vacuous.
	for _, want := range []string{DevRepoSettingsKey, WorkspaceRootSettingsKey} {
		if !containsString(keys, want) {
			t.Errorf("ModelledSettingsKeys() does not carry %q", want)
		}
	}
	// A key with NO reader at all stays unmodelled: the answer must still be able
	// to say "this build does not model it" (a typo, a key of another build).
	if containsString(keys, "not_a_real_key") {
		t.Error("ModelledSettingsKeys() reports a key nothing reads as modelled — the note would then never appear")
	}
}

// TestTopLevelReadersAreRegisteredAndComplete is arm ②: the registry is the read
// path, and it is COMPLETE.
//
// The scan is over the package's own production files. A direct
// `rawTopLevelString(` call outside the registry would be a reader
// ModelledSettingsKeys() cannot see — exactly the bug — so it is refused by
// name, with the one correct place to add instead.
func TestTopLevelReadersAreRegisteredAndComplete(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the package's files: %v", err)
	}
	registered := make(map[string]bool, len(topLevelOnlySettingsKeys))
	for _, row := range topLevelOnlySettingsKeys {
		registered[row.Key] = true
		if row.Key == "" || row.read == nil {
			t.Errorf("registry row %+v is incomplete: a row without a key or without a reader is a list entry, not a modelled key", row)
		}
	}
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		text := string(body)
		scanned++
		for i, line := range strings.Split(text, "\n") {
			call := "rawTopLevelString("
			idx := strings.Index(line, call)
			if idx < 0 {
				continue
			}
			// The declaration itself and the registry are the two allowed sites;
			// a comment mentioning the name is not a call.
			if strings.Contains(line, "func (c *Config) rawTopLevelString(") ||
				strings.HasPrefix(strings.TrimSpace(line), "//") ||
				strings.Contains(line, "return c.rawTopLevelString(") {
				continue
			}
			t.Errorf("%s:%d calls rawTopLevelString outside the registry: %s\n"+
				"such a reader is invisible to ModelledSettingsKeys(), so `gopi config show` would report the key IGNORED — add a row to topLevelOnlySettingsKeys instead", name, i+1, strings.TrimSpace(line))
		}
	}
	// NON-VACUITY: a scan that read nothing must not pass as a clean package.
	const minScannedFiles = 20
	if scanned < minScannedFiles {
		t.Fatalf("scanned only %d production file(s), want at least %d — the read-path arm would be vacuous", scanned, minScannedFiles)
	}
	if len(registered) < 2 {
		t.Fatalf("the top-level-only registry carried %d row(s), want at least 2 (dev_repo, workspace_root) — an empty registry would make the loop above vacuous", len(registered))
	}
}

// TestWorkspaceRootSettingsKeyIsReportedAsModelled is the END-TO-END statement of
// the operator's own question, from the loader's side: a settings document that
// declares workspace_root is READ, and the value reaches the resolver.
func TestWorkspaceRootSettingsKeyIsReportedAsModelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := `{"` + WorkspaceRootSettingsKey + `":"` + dir + `"}`
	if werr := os.WriteFile(path, []byte(body), 0o600); werr != nil {
		t.Fatalf("write the settings fixture: %v", werr)
	}
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	got := cfg.topLevelOnlyString(WorkspaceRootSettingsKey)
	if got != dir {
		t.Fatalf("the registered reader answered %q for %q, want %q — a declared workspace root that a reader cannot see is the silent-ignore this file exists to catch",
			got, WorkspaceRootSettingsKey, dir)
	}
	if unregistered := cfg.topLevelOnlyString(DevRepoSettingsKey); unregistered != "" {
		t.Errorf("an absent key answered %q, want \"\" (absence must not be confused with a value)", unregistered)
	}
}
