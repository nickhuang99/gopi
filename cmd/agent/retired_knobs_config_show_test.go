package main

// The RETIRED-KNOB REPORTS MOVED OFF THE START PATH (2026-11-06, task
// 01a1b003-0869). Two arms, and they are the two halves of one contract:
//
//	the NEGATIVE pin — `gopi task list` and `gopi config -h` print NOTHING about a
//	  retired knob, even when the settings file carries one and the environment
//	  exports one. Restoring the old global startup hook turns this arm RED.
//	the POSITIVE pin — `gopi config show` explains the key UNDER its own row, with
//	  the marker, what it did, why it went and what replaces it. That is the whole
//	  reason the noise was allowed to stop: the fact did not disappear, it moved to
//	  the command whose job is configuration.
//
// WHY THE NEGATIVE HALF IS MEASURED BY EXECUTING THE BINARY. The defect was never
// a function's return value; it was a CALL SITE in the one pre-chain moment every
// verb passes through (cmd/agent/tmpdir_inject.go's tmpDirBootstrapFromArgs). No
// in-process assertion can see a subprocess's stderr, and the shape of the
// regression — a new report function wired back into that hook — is exactly what
// an in-process test of the reporter would keep passing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// retiredKnobsMockURL is a placeholder endpoint for verbs that never talk to an
// LLM. Named (Rule 1).
const retiredKnobsMockURL = "http://127.0.0.1:1"

// retiredKnobsSettingsKey is the key the fixtures put in settings.json. It is
// THE retired settings key of the family whose report was the loudest (its
// paragraph opened `worker: the settings key `task_pull` is RETIRED`), read from
// the constant so a rename cannot make the fixture silently measure nothing.
const retiredKnobsSettingsKey = config.TaskPullRetiredSettingsKey

// retiredKnobsFixture writes a settings file carrying a RETIRED key beside a
// live one, and returns its path plus the child environment.
//
// The environment EXPORTS the retired env face too, because the old hook reported
// that face as well — a fixture that only carried the settings key would let a
// half-restored hook (env face only) pass.
func retiredKnobsFixture(t *testing.T) (env []string, settingsPath string) {
	t.Helper()
	home := t.TempDir()
	dir := t.TempDir()
	body := `{"` + retiredKnobsSettingsKey + `":true,"model":"m"}`
	settingsPath = filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write the settings fixture %s: %v", settingsPath, err)
	}
	env = append(testutil.MockEnv(t, home, retiredKnobsMockURL),
		config.SettingsEnv+"="+settingsPath,
		config.TaskPullRetiredEnvName+"=1",
	)
	return env, settingsPath
}

// retiredKnobsNoiseMarkers are the substrings the old start-path report printed.
// Each is a WORD an operator would have greped for, so "the noise is gone" is
// measured by the same tokens a reader uses — not by a byte count, which cannot
// tell a shortened message from a moved one.
var retiredKnobsNoiseMarkers = []string{
	configRetiredMarker,
	retiredKnobsSettingsKey,
	config.TaskPullRetiredEnvName,
	"settings key",
}

// TestStartPathPrintsNoRetiredKnobNotice is the NEGATIVE pin: the two verbs the
// operator actually runs print nothing about a retired knob, on a configuration
// that carries BOTH faces of one.
//
// RED PROOF (recorded in the task report): put the old report back on the hook —
// any `fmt.Fprintln(os.Stderr, …)` naming the key inside tmpDirBootstrapFromArgs
// reproduces the pre-move behaviour — and this arm fails on both subtests.
func TestStartPathPrintsNoRetiredKnobNotice(t *testing.T) {
	cases := []struct {
		name string
		argv []string
	}{
		{name: "task_list", argv: []string{CliVerbTask, "list", "--status", "in_progress"}},
		{name: "config_help", argv: []string{CliVerbConfig, HelpFlagLong}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := retiredKnobsFixture(t)
			stdout, stderr, code := testutil.RunGopi(t, tc.argv, "", env, t.TempDir())
			// The exit code is NOT asserted here: `task list` may refuse on a host
			// shape this fixture does not provide, and that refusal is not what
			// this arm measures — its subject is the stderr, and asserting a status
			// the fixture did not set up would make a red say the wrong thing.
			_ = code
			for _, marker := range retiredKnobsNoiseMarkers {
				if strings.Contains(stderr, marker) {
					t.Fatalf("`gopi %s` printed %q on stderr — a retired knob is reported by `%s %s` and by nothing else; the global start-path hook is back:\n--- stderr ---\n%s\n--- stdout ---\n%s",
						strings.Join(tc.argv, " "), marker, CliVerbConfig, configShowVerb, stderr, stdout)
				}
			}
		})
	}
}

// TestStartPathDoesNotReadTheRetiredRegistry is the SOURCE half of the same pin,
// and it is deliberately narrow: it does not forbid the registry (that is the
// fix), it forbids the START PATH from consulting it.
//
// A runtime arm alone would miss the regression that matters most — pointing a
// REINSTATED printer at the new registry instead of at a new constant — because
// such a report still only fires when a knob is set, and a later change to what
// gets printed could silence the arm without moving the call site.
func TestStartPathDoesNotReadTheRetiredRegistry(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("tmpdir_inject.go"))
	if err != nil {
		t.Fatalf("read the start-path hook: %v", err)
	}
	for _, forbidden := range []string{
		"RetiredSettingsKeyInfo",
		"RetiredKnobsWithEnvFace",
		"RetiredSettingsKeys(",
	} {
		if strings.Contains(string(src), forbidden) {
			t.Errorf("cmd/agent/tmpdir_inject.go calls %s — the start path is the one moment EVERY verb passes through, so reading the retired-knob registry here puts the explanation in front of `gopi task list` again; the registry belongs to `%s %s` alone (internal/config/retired_settings_keys.go)",
				forbidden, CliVerbConfig, configShowVerb)
		}
	}
}

// TestConfigShowExplainsTheRetiredKeyUnderItsRow is the POSITIVE pin: the fact
// moved rather than vanished. It asserts the row AND the four things the line
// below it must say, because a marker with no explanation is exactly the noise
// this task removed — only relocated.
func TestConfigShowExplainsTheRetiredKeyUnderItsRow(t *testing.T) {
	env, settingsPath := retiredKnobsFixture(t)
	stdout, stderr, code := testutil.RunGopi(t,
		[]string{CliVerbConfig, configShowVerb}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("`gopi config show` exited %d — it reports configuration and must succeed on every host shape:\n%s\n%s",
			code, stdout, stderr)
	}
	lines := strings.Split(stdout, "\n")
	row := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), retiredKnobsSettingsKey) {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatalf("`gopi config show` printed no row for %q, so its explanation has nowhere to hang:\n%s",
			retiredKnobsSettingsKey, stdout)
	}
	// The explanation must be UNDER the row, indented past the key column: "on the
	// line below the key" is the acceptance criterion, and a block printed at the
	// end of the table would satisfy a bare Contains while failing a reader.
	if row+1 >= len(lines) || !strings.HasPrefix(lines[row+1], "  ") {
		t.Fatalf("the line under the %q row is not an indented explanation:\n%s", retiredKnobsSettingsKey, stdout)
	}
	block := strings.Join(lines[row+1:row+5], "\n")
	for _, must := range []string{
		configRetiredMarker,
		retiredKnobsSettingsKey,
		config.TaskPullRetiredEnvName,
		configRetiredPurposeLabel,
		configRetiredHistoryLabel,
		configRetiredInsteadLabel,
		config.TaskPullRetiredReplacement,
	} {
		if !strings.Contains(block, must) {
			t.Errorf("the explanation under %q does not carry %q — an operator reading it must learn WHAT the knob did, WHY it went and WHAT to do instead:\n%s",
				retiredKnobsSettingsKey, must, block)
		}
	}
	// The settings path is echoed in the report, so a red can be re-run against the
	// same file; asserting it here also pins that the fixture was the one shown.
	if !strings.Contains(stdout, settingsPath) {
		t.Errorf("`gopi config show` did not name the settings file it reported (%s):\n%s", settingsPath, stdout)
	}
}

// TestConfigShowReportsARetiredEnvFace pins the OTHER FACE of the same move: a
// knob exported into the environment is not in settings.json at all, so it is
// reported in the environment section of the same verb. The arm is table-driven
// over the registry, which is what the deleted per-knob test files asserted one
// function at a time — a row added later is covered without a new test.
func TestConfigShowReportsARetiredEnvFace(t *testing.T) {
	cases := config.RetiredKnobsWithEnvFace()
	if len(cases) == 0 {
		t.Fatal("the registry carries no row with an env face — the environment half of the report would be vacuous")
	}
	home := t.TempDir()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"m"}`), 0o600); err != nil {
		t.Fatalf("write the settings fixture: %v", err)
	}
	// NOTHING is exported: the section must be ABSENT, because a report on every
	// ordinary run would be the start-path noise again in a new place.
	cleanEnv := append(testutil.MockEnv(t, home, retiredKnobsMockURL), config.SettingsEnv+"="+path)
	stdout, _, code := testutil.RunGopi(t, []string{CliVerbConfig, configShowVerb}, "", cleanEnv, t.TempDir())
	if code != 0 {
		t.Fatalf("`gopi config show` exited %d with nothing exported:\n%s", code, stdout)
	}
	if strings.Contains(stdout, retiredEnvFaceHeader) {
		t.Fatalf("the report printed the retired-env-face section with nothing exported — an unset variable is not a fact about the configuration:\n%s", stdout)
	}
	for _, retired := range cases {
		retired := retired
		t.Run(retired.EnvName, func(t *testing.T) {
			env := append(testutil.MockEnv(t, t.TempDir(), retiredKnobsMockURL),
				config.SettingsEnv+"="+path,
				retired.EnvName+"=1",
			)
			out, errOut, code := testutil.RunGopi(t, []string{CliVerbConfig, configShowVerb}, "", env, t.TempDir())
			if code != 0 {
				t.Fatalf("`gopi config show` exited %d with %s exported:\n%s\n%s", code, retired.EnvName, out, errOut)
			}
			if !strings.Contains(out, retiredEnvFaceHeader) {
				t.Fatalf("the report omitted its retired-env-face section with %s exported:\n%s", retired.EnvName, out)
			}
			section := out[strings.Index(out, retiredEnvFaceHeader):]
			for _, must := range []string{
				retired.EnvName,
				configRetiredMarker,
				configRetiredPurposeLabel,
				configRetiredHistoryLabel,
				configRetiredInsteadLabel,
			} {
				if !strings.Contains(section, must) {
					t.Errorf("the environment section does not carry %q for %s — a retired variable named with no explanation is the noise this task removed:\n%s",
						must, retired.EnvName, section)
				}
			}
		})
	}
}

// TestConfigShowNamesAnUnmodelledKey pins the neighbour this task had to say
// something about: a key no build models is IGNORED by the decoder, so the
// configuration verb must say so on the row rather than stay silent (Rule 15).
func TestConfigShowNamesAnUnmodelledKey(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"m","not_a_real_key":1}`), 0o600); err != nil {
		t.Fatalf("write the settings fixture: %v", err)
	}
	env := append(testutil.MockEnv(t, home, retiredKnobsMockURL), config.SettingsEnv+"="+path)
	stdout, stderr, code := testutil.RunGopi(t,
		[]string{CliVerbConfig, configShowVerb}, "", env, t.TempDir())
	if code != 0 {
		t.Fatalf("`gopi config show` exited %d:\n%s\n%s", code, stdout, stderr)
	}
	var row string
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "not_a_real_key") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("the report dropped an unmodelled key entirely, which renders "+
			"\"the configuration in force\" while hiding a line that does nothing:\n%s", stdout)
	}
	if !strings.Contains(row, configUnmodelledKeyNote) {
		t.Errorf("the unmodelled key's row carries no warning — the decoder IGNORES such a key, so silence here is the silent failure Rule 15 forbids:\n%s", row)
	}
}
