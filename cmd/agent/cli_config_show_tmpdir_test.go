package main

// config show's TEMP-WORKSPACE ATTRIBUTION, measured by executing the built
// binary (`gopi config show`) as a subprocess — the only instrument that can
// see this defect at all.
//
// WHY A SUBPROCESS AND NOT A UNIT TEST (the reason this file exists in this
// shape): the wrong label was never a resolver bug. config.ResolveTmpDirConfigured
// ranks its four channels correctly and always did; the DEFECT was CALLER-SIDE —
// the report re-asked a question the process had already answered, and by then
// main()'s tmpDirBootstrapFromArgs had published the resolved directory into
// gopi's OWN TMPDIR for the children to inherit. The resolver tests its ambient
// arm (TMPDIR) before its GOPI_TMPDIR arm, so the re-read matched gopi's own
// publication: every channel — `--tmpdir X` included — printed "from the
// caller's TMPDIR". Nothing in-process reproduces that, because the publication
// IS the mechanism.
//
// THE OBSERVATION IS THE PRINTED LINE, and it carries both halves: the PATH
// (which directory is in force) and the PARENTHETICAL (which channel decided
// it). Asserting only the path would have passed on the broken binary for the
// flag/env/settings cases — the value moved correctly the whole time; it was
// the provenance that lied. The labels are also asserted to be DISTINCT, which
// is the defect's signature: a re-derivation collapses them to one.
//
// RED PROOF (recorded in the report): the suite's ONE build is overridable with
// GOPI_TEST_BIN (cmd/agent/gopibin_test.go), so this same test run against a
// binary built BEFORE the fix fails there, with the four identical labels:
//
//	GOPI_TEST_BIN=<pre-fix binary> go test ./cmd/agent/ -run TestConfigShowTmpDirAttribution
//	go test ./cmd/agent/ -run TestConfigShowTmpDirAttribution   # the fixed tree

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// configShowTempWorkspaceLabel is the report's row label for the temp
// workspace — the first word of the line cli_config.go renders. Both the
// parser and the failure messages read it from here, so a rename breaks the
// test loudly instead of silently matching nothing.
const configShowTempWorkspaceLabel = "temp workspace"

// configShowMockURL is a placeholder endpoint for a verb that never talks to an
// LLM. Named (Rule 1b).
const configShowMockURL = "http://127.0.0.1:1"

// configShowCase is one channel configuration: what to set, and what the
// printed line must say. Every leaf below is resolved under the test's own
// fixture root, never the operator's profile.
type configShowCase struct {
	name string
	// flag is the --tmpdir VALUE, "" when the case passes no flag.
	flag string
	// gopiTmpDir is the GOPI_TMPDIR value, "" when unset.
	gopiTmpDir string
	// ambientTmpDir is a caller-exported TMPDIR leaf, "" when the case exports
	// none.
	ambientTmpDir string
	// settingsTmpDir is the settings `tmpdir` key's leaf, "" when the file
	// carries no such key.
	settingsTmpDir string
	// wantLabel is the EXACT parenthetical the report must print.
	wantLabel string
	// repeatsLabel marks a case whose label is deliberately the SAME as another
	// case's: the all-channels case must report the flag's label, because the
	// flag is what wins there. Only the single-channel cases and the default
	// carry the distinctness obligation, and they are what proves the report
	// tells the four channels apart.
	repeatsLabel bool
}

// TestConfigShowTmpDirAttribution drives `gopi config show` across the four
// channels (each alone), the all-at-once precedence case and the default, and
// asserts the path AND the parenthetical of every case.
func TestConfigShowTmpDirAttribution(t *testing.T) {

	bin := gopiTestBinPath(t)

	cases := []configShowCase{
		{name: "flag", flag: "flag-tmp", wantLabel: config.TmpDirSourceFlag},
		{name: "gopi_tmpdir_env", gopiTmpDir: "env-tmp", wantLabel: config.TmpDirSourceEnv},
		{name: "settings_only", settingsTmpDir: "settings-tmp", wantLabel: config.TmpDirSourceSettings},
		{name: "caller_tmpdir_ambient", ambientTmpDir: "ambient-tmp", wantLabel: config.TmpDirSourceAmbient},
		{
			name: "all_channels_flag_wins", flag: "flag-tmp", gopiTmpDir: "env-tmp",
			ambientTmpDir: "ambient-tmp", settingsTmpDir: "settings-tmp",
			wantLabel: config.TmpDirSourceFlag, repeatsLabel: true,
		},
		{name: "default", wantLabel: scratch.TmpDirSourceBuiltInDefault},
	}

	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			env, argv, wantDir := configShowCaseRun(t, root, tc)
			path, label := configShowTmpDir(t, bin, argv, env)
			if label != tc.wantLabel {
				t.Fatalf("`gopi %v` attributed the temp workspace to %q, want %q\n"+
					"the path it printed was %q (want %q) — the VALUE moved correctly and only the "+
					"provenance is wrong, which is the defect this test exists for",
					strings.Join(argv, " "), label, tc.wantLabel, path, wantDir)
			}
			if path != wantDir {
				t.Fatalf("`gopi %v` printed temp workspace %q, want %q",
					strings.Join(argv, " "), path, wantDir)
			}
			if other, ok := seen[label]; ok && !tc.repeatsLabel {
				t.Fatalf("%s and %s both print the parenthetical %q — the report is collapsing "+
					"distinct channels onto one label, which is the defect", other, tc.name, label)
			}
			seen[label] = tc.name
			t.Logf("temp workspace: %s  (%s)", path, label)
		})
	}
}

// configShowCaseRun materialises one case: it creates the directories the case
// names, writes the settings fixture, builds the child's environment and argv,
// and returns the directory the report must print. The asserted path is derived
// here from the SAME case value that configured the child, so a case cannot
// assert a directory it did not set up.
func configShowCaseRun(t *testing.T, root string, tc configShowCase) (env, argv []string, wantDir string) {
	t.Helper()
	leaf := func(name string) string {
		if name == "" {
			return ""
		}
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatalf("prepare the case's temp root %s: %v", p, err)
		}
		return p
	}
	flagDir := leaf(tc.flag)
	envDir := leaf(tc.gopiTmpDir)
	ambientDir := leaf(tc.ambientTmpDir)
	settingsDir := leaf(tc.settingsTmpDir)

	var settingsArg string
	if settingsDir != "" {
		path := filepath.Join(root, "settings.json")
		body := "{\"" + config.TmpDirSettingsKey + "\": " + configShowJSONString(settingsDir) + "}\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write the settings fixture %s: %v", path, err)
		}
		settingsArg = path
	}

	env = configShowEnv(t, root, tc, envDir, ambientDir, settingsArg)
	if flagDir != "" {
		argv = append(argv, "--"+config.TmpDirFlagName+"="+flagDir)
	}
	argv = append(argv, CliVerbConfig, configShowVerb)

	// The winner per the standing precedence (config.ResolveTmpDirConfigured):
	// flag > caller TMPDIR > GOPI_TMPDIR > settings > default.
	for _, cand := range []string{flagDir, ambientDir, envDir, settingsDir} {
		if cand != "" {
			return env, argv, scratch.CanonicalPath(cand)
		}
	}
	return env, argv, scratch.CanonicalPath(
		filepath.Join(leaf(configShowDefaultWorkspaceLeaf), scratch.DefaultTmpDirLeaf))
}

// configShowDefaultWorkspaceLeaf is the fixture workspace the default case
// resolves under; named (Rule 1b) because both the environment and the asserted
// path are built from it.
const configShowDefaultWorkspaceLeaf = scratch.WorkspaceDirName

// configShowEnv is the base environment every case starts from: the suite's
// mocked-run env (temp GOPI_HOME, no provider secrets) with every channel of
// the knob under test REMOVED — so "this case exports no TMPDIR" is a fact
// about the child rather than a property of whatever the test process
// inherited — and a per-test workspace so the built-in default never resolves
// under the operator's real home.
func configShowEnv(t *testing.T, root string, tc configShowCase, envDir, ambientDir, settingsArg string) []string {
	t.Helper()
	base := testutil.MockEnv(t, t.TempDir(), configShowMockURL)
	out := make([]string, 0, len(base)+4)
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case scratch.TmpDirEnvName, config.TmpDirEnvName, scratch.TmpDirSourceEnvName,
			config.SettingsEnv, scratch.WorkspaceHomeEnvName:
			continue // the case decides these
		}
		out = append(out, kv)
	}
	if envDir != "" {
		out = append(out, config.TmpDirEnvName+"="+envDir)
	}
	if ambientDir != "" {
		out = append(out, scratch.TmpDirEnvName+"="+ambientDir)
	}
	if settingsArg != "" {
		out = append(out, config.SettingsEnv+"="+settingsArg)
	}
	out = append(out, scratch.WorkspaceHomeEnvName+"="+root)
	return out
}

// configShowTmpDir runs `gopi config show` as a subprocess and returns the
// temp-workspace path and parenthetical it PRINTED.
//
// It executes the binary the SUITE resolved (cmd/agent/gopibin_test.go's ONE
// cached build, overridable with GOPI_TEST_BIN) rather than this package's
// default: that override is what lets the same test be pointed at a PRE-FIX
// binary to record the red half of the proof.
func configShowTmpDir(t *testing.T, bin string, argv, env []string) (path, label string) {
	t.Helper()
	out, code := runGopiIn(t, bin, env, "", argv...)
	if code != 0 {
		t.Fatalf("`gopi %v` exited %d\noutput:\n%s",
			strings.Join(argv, " "), code, out)
	}
	prefix := configShowTempWorkspaceLabel
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), prefix, 2)
		if len(fields) != 2 || fields[0] != "" {
			continue
		}
		return parseConfigShowTempWorkspace(t, strings.TrimSpace(fields[1]))
	}
	t.Fatalf("`gopi config show` printed no %q row:\n%s", configShowTempWorkspaceLabel, out)
	return "", ""
}

// parseConfigShowTempWorkspace splits the report's row into its two halves —
// "<path>  (<label>)". Both are returned verbatim; a row carrying no
// parenthetical is a failure, not an empty label (a value whose channel nobody
// named is the defect).
func parseConfigShowTempWorkspace(t *testing.T, rest string) (path, label string) {
	t.Helper()
	open := strings.LastIndex(rest, "  (")
	if open < 0 || !strings.HasSuffix(rest, ")") {
		t.Fatalf("the temp-workspace row %q carries no (channel) parenthetical — the report must name "+
			"whichever channel decided the value", rest)
	}
	return strings.TrimSpace(rest[:open]), rest[open+3 : len(rest)-1]
}

// configShowJSONString renders the settings fixture's value as a JSON string
// literal. It is the package's ONE escaping rule (strconv.Quote) so the fixture
// cannot disagree with the parser reading it.
func configShowJSONString(s string) string { return strconv.Quote(s) }
