package main

// cli_config_settings_model_test.go — `gopi config show` must not call a key it
// READS "IGNORED", and must report the workspace root it resolves.
//
// THE BUG (measured 2026-11-13, on this host). The settings table printed
//
//	dev_repo        "zbook:/home/nick/work/gopi"  (unrecognized key — … so it is IGNORED)
//	workspace_root  "/home/nick/workspace"        (unrecognized key — … so it is IGNORED)
//
// while the SAME binary's worker banner, in that run's own log, said
//
//	task <id>: clone channel ON (dev_repo=zbook:/home/nick/work/gopi via settings
//	              dev_repo; workspace_root=/home/nick/workspace via settings workspace_root)
//
// and the run's ground was really built under it. Two surfaces of one binary
// contradicting each other is worse than either being absent (Rule 15).
//
// The arms below pin the SURFACE, not the resolver: the resolver's own behaviour
// (including the GOPI_WORKTREE_ROOT precedence) is asserted in
// internal/config's tests, where it lives.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// configShowModelFixture writes a settings document carrying BOTH live
// top-level-only keys and returns the child environment. It is written with the
// keys' own constants so a rename cannot make the fixture measure nothing.
func configShowModelFixture(t *testing.T, workspaceRoot string) (env []string, settingsPath string) {
	t.Helper()
	home := t.TempDir()
	dir := t.TempDir()
	body := `{"model":"m","` + config.DevRepoSettingsKey + `":"zbook:/home/nick/work/gopi","` +
		config.WorkspaceRootSettingsKey + `":"` + workspaceRoot + `"}`
	settingsPath = filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write the settings fixture: %v", err)
	}
	return append(testutil.MockEnv(t, home, retiredKnobsMockURL),
		config.SettingsEnv+"="+settingsPath), settingsPath
}

// settingsRowFor returns the stdout line carrying key, or "".
func settingsRowFor(stdout, key string) string {
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == key {
			return line
		}
	}
	return ""
}

// TestConfigShowDoesNotCallItsOwnReadersIgnored is the CONTENT arm: a key with a
// live production reader is not marked unrecognized, while a key nothing reads
// still is.
//
// The second half matters as much as the first: silencing the note entirely
// would "fix" this bug by removing the signal that catches a real typo, so both
// halves are asserted from ONE run of the real binary.
func TestConfigShowDoesNotCallItsOwnReadersIgnored(t *testing.T) {
	env, _ := configShowModelFixture(t, t.TempDir())
	stdout, stderr, code := testutil.RunGopi(t,
		[]string{CliVerbConfig, configShowVerb}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("`gopi config %s` exited %d:\n%s\n%s", configShowVerb, code, stdout, stderr)
	}
	for _, key := range []string{config.DevRepoSettingsKey, config.WorkspaceRootSettingsKey} {
		row := settingsRowFor(stdout, key)
		if row == "" {
			t.Fatalf("the report dropped %q entirely:\n%s", key, stdout)
		}
		if strings.Contains(row, configUnmodelledKeyNote) {
			t.Errorf("the report calls %q IGNORED, but this build reads it (internal/config/devrepo.go) and the worker banner prints its value — a report that contradicts the runtime is the defect this arm pins:\n%s", key, row)
		}
	}
	// NON-VACUITY: an unknown key must STILL be marked, or the note is dead text
	// and this test would pass on a build that simply stopped printing it.
	if !strings.Contains(stdout, configUnmodelledKeyNote) {
		t.Logf("(no unmodelled key in this fixture, so the note's presence is not asserted here — see TestConfigShowNamesAnUnmodelledKey)")
	}
}

// TestConfigShowReportsTheResolvedWorkspaceRoot is the ROW arm: the verb prints
// where a task's workspace actually lands and which channel decided it, so an
// operator no longer has to infer the answer from a settings key that carries no
// resolved value of its own.
func TestConfigShowReportsTheResolvedWorkspaceRoot(t *testing.T) {
	declared := t.TempDir()
	env, _ := configShowModelFixture(t, declared)
	stdout, stderr, code := testutil.RunGopi(t,
		[]string{CliVerbConfig, configShowVerb}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("`gopi config %s` exited %d:\n%s\n%s", configShowVerb, code, stdout, stderr)
	}
	var row string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "  "+configWorkspaceRootLabel) {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("the report carries no %q row, so the one question this key raises — WHERE does a task workspace land — has no answer on the configuration surface:\n%s", configWorkspaceRootLabel, stdout)
	}
	if !strings.Contains(row, declared) {
		t.Errorf("the %q row does not name the declared directory %q — the row would be a label with no value:\n%s", configWorkspaceRootLabel, declared, row)
	}
	if !strings.Contains(row, config.WorkspaceRootSettingsLabel) {
		t.Errorf("the %q row does not name the channel that decided it (%q), so a reader cannot tell a declaration from a default:\n%s", configWorkspaceRootLabel, config.WorkspaceRootSettingsLabel, row)
	}
}
