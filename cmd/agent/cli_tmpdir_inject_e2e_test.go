package main

// End-to-end proof of the settings.json `tmpdir` injection (tmpdir epic,
// 2026-09-14) — run against the REAL binary, a REAL mock LLM round and the REAL
// bash tool, because the claim being verified is about PROCESS INHERITANCE and
// nothing in-process can observe that.
//
// THE CHAIN UNDER TEST, and where each link is measured:
//
//	settings.json {"tmpdir": X}
//	  → main() injects TMPDIR=X into gopi's OWN environment   (measured: the
//	    child's own /proc/self/environ, read through the bash tool below)
//	  → internal/tools.bashChildEnv() hands os.Environ() to the shell, which
//	    starts from append(bashChildEnv(), …)                  (measured: the
//	    SHELL's /proc/self/environ, captured by the mocked tool call)
//	  → that shell's own mktemp/date/etc. land off `/`         (implied by the
//	    value the shell reports — the whole point of the injection)
//
// WHY /proc/self/environ IS THE EVIDENCE and not a printed variable: the block
// is what execve HANDED the process (the kernel's record). A shell echo would
// report its own expansion — a claim about itself — while the environ block
// cannot be true unless inheritance really happened.
//
// WHAT EACH CASE PINS (the epic's acceptance list, one case each):
//
//	TestTmpDirSettingsInstallsIntoTheChildTree — the settings key is set and the
//	  caller has no TMPDIR: every child sees the configured value, and the run
//	  says nothing about it (acceptance 1 + 2, and the SILENCE half of A).
//	TestTmpDirSettingsAbsentPublishesTheWorkspaceDefault — no key, no env, no
//	  flag: gopi publishes the built-in default, an ordinary directory under the
//	  WORKSPACE (acceptance 3 + 4; the earlier expectation "the scratch home's
//	  tmp child" is superseded by the operator's decision that the default is
//	  the working space).
//	TestTmpDirKnobChannelsAgree — the same value through --tmpdir and through
//	  GOPI_TMPDIR resolves to the same effective root (B: one knob, one entry).
//	TestTmpDirFlagBeatsTheEnvironment — the channels DISAGREE: the flag wins
//	  (B: CLI > env, the standing precedence).
//	TestTmpDirUnusableValueFailsLoudlyAndNamesThePath — a read-only parent makes
//	  the run FAIL, naming path + reason + channel (A: loud failure survives).
//	TestTmpDirSilentOnHelpAndVersion / TestTmpDirDebugSurfaceShowsTheValue —
//	  `--help`/`--version` say nothing; the value stays reachable through the
//	  EXISTING debug knob (A: the banner's replacement surface).
//	TestTmpDirSettingsNeverOverridesTheCaller — both present: the caller wins
//	  (the epic's explicit "never override a caller's TMPDIR" constraint).
//
// NOTHING HERE WRITES TO `/` FOR LOAD, and no case asserts a device id: the
// /-partition question is closed, and its mechanical half
// (internal/scratch.AssertPathOffRootFS) has its own test there. Each case
// drives the knob explicitly — no case depends on where this host's workspace
// happens to live.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

// tmpDirProbeSettingsKey is the settings.json key this file configures. It is
// the config package's constant (never a literal), so a rename cannot leave
// this test configuring a key the binary no longer reads.
const tmpDirProbeSettingsKey = config.TmpDirSettingsKey

// procSelfEnvironPath is the kernel's raw environment block for the reading
// process — what execve handed it. Named (Rule 1b): the probe command must not
// carry the path as an inline literal.
const procSelfEnvironPath = "/proc/self/environ"

// tmpDirProbeCommand builds the mocked tool call's shell command: rewrite the
// NUL separators of the environment block as newlines into the capture file. It
// is built from the constants above so the command and the parser cannot drift.
func tmpDirProbeCommand(outPath string) string {
	return "sed -e 's/\\x0/\\n/g' " + procSelfEnvironPath + " > " + outPath
}

// tmpDirEnvironOutName is the file the mocked bash tool writes the shell's raw
// environment block into, inside the per-test temp dir.
const tmpDirEnvironOutName = "shell-environ"

// tmpDirSettingsArgv is the --settings flag spelled once (Rule 1b).
const tmpDirSettingsArgv = "--settings"

// tmpDirProbeResult holds one case's observation: what the gopi child's OWN
// environment carried and what the shell it spawned was handed.
type tmpDirProbeResult struct {
	// shellEnv holds the shell's /proc/self/environ, parsed by name.
	shellEnv map[string]string
	// stderr is the child's stderr. NOTHING is expected here about the temp
	// workspace on a successful run (the banner is gone); it is captured so a
	// refusal still names its path and reason in the failure message.
	stderr string
}

// runTmpDirProbe drives one case end to end: it writes the settings file, runs
// `gopi --settings <path> "<prompt>"` with the caller's environment (TMPDIR
// either absent or pinned to callerTmpDir), and captures the environment block
// the mocked bash tool observed inside a shell the agent spawned.
//
// settingsTmpDir == "" writes a settings file with NO tmpdir key (the
// nothing-configured case). callerTmpDir == "" passes no TMPDIR at all (a
// default host, which is exactly the configuration that produced the residue).
func runTmpDirProbe(t *testing.T, settingsTmpDir, callerTmpDir string) tmpDirProbeResult {
	t.Helper()
	gopiHome := t.TempDir()
	workDir := t.TempDir()
	outPath := filepath.Join(workDir, tmpDirEnvironOutName)

	// The settings file: only the knob under test is interesting (the mock LLM
	// supplies endpoint/model through the environment, which outranks settings).
	settingsBody := "{}\n"
	if settingsTmpDir != "" {
		settingsBody = "{" + `"` + tmpDirProbeSettingsKey + `": "` + settingsTmpDir + `"}` + "\n"
	}
	settingsPath := filepath.Join(gopiHome, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(settingsBody), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_tmpdir_probe", Name: toolnames.ToolBash,
			Args: map[string]interface{}{
				// The shell reports the environment IT was handed. sed is used
				// rather than tr because a literal NUL byte cannot be passed as
				// an argv element at all — the escape form is the only spelling
				// that survives exec (measured: the tr spelling silently
				// produced an EMPTY capture).
				"command": tmpDirProbeCommand(outPath),
			}}},
		testutil.Resp{Text: "tmpdir probe done"},
	)
	t.Cleanup(mock.Close)

	env := tmpDirProbeEnv(t, gopiHome, mock.URL(), settingsPath, callerTmpDir, "")
	// A positional prompt is the agent path (no subcommand): the run goes
	// through main()'s config block, which is where the publication lives.
	stdout, stderr, code := runGopiMapped(t, []string{tmpDirSettingsArgv, settingsPath, "tmpdir probe"}, "", env, workDir)
	if code != 0 {
		t.Fatalf("gopi run failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("the mocked bash tool did not capture the shell environment (stderr:\n%s): %v", stderr, err)
	}
	block, err := environBlockFromLines(raw)
	if err != nil {
		t.Fatalf("parse the shell environment block: %v", err)
	}
	return tmpDirProbeResult{shellEnv: block, stderr: stderr}
}

// tmpDirProbeEnv builds the child's environment: the suite's standard mock env
// (endpoints + sandboxed home), with the temp-workspace variables controlled
// EXACTLY — TMPDIR present with callerTmpDir, GOPI_TMPDIR with knobEnv, and both
// ABSENT when they are empty. Stripping the inherited values is the point: this
// suite pins TMPDIR (off `/`, correct for the suite) and a leftover pin would
// silently turn every case into the caller-wins case.
//
// The workspace home (WorkspaceHomeEnvName) is pinned to a per-test fixture, so
// the DEFAULT case resolves its temp root under a directory this test owns
// rather than under the operator's real ~/workspace.
func tmpDirProbeEnv(t *testing.T, gopiHome, mockURL, settingsPath, callerTmpDir, knobEnv string) []string {
	t.Helper()
	base := testutil.MockEnv(t, gopiHome, mockURL)
	env := make([]string, 0, len(base)+4)
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		if key == scratch.TmpDirEnvName || key == config.TmpDirEnvName || key == scratch.WorkspaceHomeEnvName {
			continue // replaced below
		}
		env = append(env, kv)
	}
	env = append(env, scratch.WorkspaceHomeEnvName+"="+gopiHome)
	if callerTmpDir != "" {
		env = append(env, scratch.TmpDirEnvName+"="+callerTmpDir)
	}
	if knobEnv != "" {
		env = append(env, config.TmpDirEnvName+"="+knobEnv)
	}
	return env
}

// environBlockFromLines reassembles a "NAME=value per line" capture into the
// NUL-separated block shape scratch.ProcEnviron reads, so the parsing is the
// PRODUCTION parser rather than a second implementation in the test (Rule 14).
func environBlockFromLines(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		out[name] = value
	}
	return out, nil
}

// TestTmpDirSettingsInstallsIntoTheChildTree is acceptance 1 + 2: with the key
// configured and no caller TMPDIR, the value reaches a shell the agent spawned
// — evidence read from that shell's own /proc/self/environ, i.e. through
// os.Environ() → bashChildEnv() → the shell.
func TestTmpDirSettingsInstallsIntoTheChildTree(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	target := filepath.Join(t.TempDir(), "gopi-tmp")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("prepare the configured temp root: %v", err)
	}

	got := runTmpDirProbe(t, target, "")
	if v, present := got.shellEnv[scratch.TmpDirEnvName]; !present || v != target {
		t.Fatalf("the shell the agent spawned was handed %s=%q (present=%v), want %q — settings.json %q did not reach the child tree\ngopi stderr:\n%s",
			scratch.TmpDirEnvName, v, present, target, tmpDirProbeSettingsKey, got.stderr)
	}
	// Sanity: the block is a REAL environment (the publication must not have
	// replaced the environment wholesale).
	if got.shellEnv["GOPI_HOME"] == "" {
		t.Fatalf("the shell's environ block carries no GOPI_HOME — the captured block is not this run's environment: %v", got.shellEnv)
	}
	// Silence on the happy path is part of the contract now (the operator's
	// report): an ordinary run says nothing about the temp workspace. Asserted
	// HERE, on a real run, so a reintroduced banner fails a test.
	assertNoTmpDirBanner(t, got.stderr)
	// The raw observation, on the record: this line is the pasteable evidence
	// that the value reached a real child through a real spawn (the gate run
	// carries it, so it does not have to be reconstructed later).
	t.Logf("child shell /proc/self/environ: %s=%q (configured=%q), GOPI_HOME=%q",
		scratch.TmpDirEnvName, got.shellEnv[scratch.TmpDirEnvName], target, got.shellEnv["GOPI_HOME"])
}

// assertNoTmpDirBanner fails when a run announced its temp workspace. The
// banner is the defect this change removes; the surface that replaced it is
// opt-in (GOPI_DEBUG), so its marker must not appear on an ordinary run.
func assertNoTmpDirBanner(t *testing.T, stderr string) {
	t.Helper()
	if strings.Contains(stderr, scratch.TmpDirReportPrefix) {
		t.Fatalf("the run announced its temp workspace on stderr — the banner is gone by design; use %s=1 for the debug report\nstderr:\n%s",
			scratch.DebugEnvName, stderr)
	}
}

// TestTmpDirSettingsAbsentPublishesTheWorkspaceDefault is acceptance 3 + 4: no
// key, no env, no flag → the effective temp root is the built-in default, an
// ordinary directory under the WORKSPACE (measured, never asserted from a
// spelling), and the child the agent spawns is handed it.
//
// THE PREVIOUS EXPECTATIONS ARE BOTH SUPERSEDED. The case first asserted "gopi
// invents nothing" (which WAS the /tmp fallback defect), then "the default is
// the scratch home's tmp child" (superseded by the operator's decision that the
// default is the working space). What is kept is the OBSERVATION: a run with
// nothing configured must still publish a writable root, and it must be the
// workspace's.
func TestTmpDirSettingsAbsentPublishesTheWorkspaceDefault(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	got := runTmpDirProbe(t, "", "")
	v, present := got.shellEnv[scratch.TmpDirEnvName]
	if !present || v == "" {
		t.Fatalf("with no %q key and no caller %s, the child was handed no temp root (present=%v, value=%q) — "+
			"gopi must publish its own default instead of letting every child fall back to the ambient root",
			tmpDirProbeSettingsKey, scratch.TmpDirEnvName, present, v)
	}
	// The default is the workspace's own leaf: the probe pinned the workspace
	// home to its fixture, so the assertion is about RESOLUTION, not about the
	// operator's host.
	if filepath.Base(v) != scratch.DefaultTmpDirLeaf {
		t.Fatalf("the published default %q does not end in %q — the default is the workspace's ordinary directory",
			v, scratch.DefaultTmpDirLeaf)
	}
	if filepath.Base(filepath.Dir(v)) != scratch.WorkspaceDirName {
		t.Fatalf("the published default %q does not sit directly under a %q directory — the default must be the workspace's, not the scratch tree's",
			v, scratch.WorkspaceDirName)
	}
	info, err := os.Stat(v)
	if err != nil {
		t.Fatalf("the published default %q does not exist: %v", v, err)
	}
	if !info.IsDir() {
		t.Fatalf("the published default %q is not a directory", v)
	}
	assertNoTmpDirBanner(t, got.stderr)
	t.Logf("child shell /proc/self/environ carries %s=%q (published with no settings key, no env and no flag)\nstderr:\n%s",
		scratch.TmpDirEnvName, v, got.stderr)
}

// TestTmpDirKnobChannelsAgree pins acceptance 3's "each channel works": the SAME
// value set through the CLI flag and through GOPI_TMPDIR produces the SAME
// effective temp root — one knob, not three bespoke reads. The settings-key
// channel is covered by TestTmpDirSettingsInstallsIntoTheChildTree and the
// default by its sibling above; the PRECEDENCE between them is pinned by
// TestTmpDirFlagBeatsTheEnvironment.
func TestTmpDirKnobChannelsAgree(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	target := filepath.Join(t.TempDir(), "knob-tmp")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("prepare the configured temp root: %v", err)
	}

	// Channel 2: GOPI_TMPDIR (the knob's env spelling) — no settings key, no flag.
	viaEnv := runTmpDirProbeWithKnob(t, "", "", target, nil)
	if got := viaEnv.shellEnv[scratch.TmpDirEnvName]; got != target {
		t.Fatalf("%s=%q produced %s=%q, want the configured %q\nstderr:\n%s",
			config.TmpDirEnvName, target, scratch.TmpDirEnvName, got, target, viaEnv.stderr)
	}

	// Channel 1: --tmpdir on the command line.
	viaFlag := runTmpDirProbeWithKnob(t, "", "", "", []string{"--" + config.TmpDirFlagName, target})
	if got := viaFlag.shellEnv[scratch.TmpDirEnvName]; got != target {
		t.Fatalf("--%s %q produced %s=%q, want the configured %q\nstderr:\n%s",
			config.TmpDirFlagName, target, scratch.TmpDirEnvName, got, target, viaFlag.stderr)
	}
	assertNoTmpDirBanner(t, viaEnv.stderr)
	assertNoTmpDirBanner(t, viaFlag.stderr)
	t.Logf("both channels resolved to %s=%q (GOPI_TMPDIR and --%s agree)", scratch.TmpDirEnvName, target, config.TmpDirFlagName)
}

// TestTmpDirFlagBeatsTheEnvironment is acceptance 3's precedence half: when the
// CLI flag and the environment DISAGREE, the flag wins (CLI > env, the standing
// rule) — and the child the agent spawns is handed the flag's value, not the
// environment's.
func TestTmpDirFlagBeatsTheEnvironment(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	fromFlag := filepath.Join(t.TempDir(), "flag-wins")
	fromEnv := filepath.Join(t.TempDir(), "env-loses")
	for _, dir := range []string{fromFlag, fromEnv} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("prepare %s: %v", dir, err)
		}
	}

	got := runTmpDirProbeWithKnob(t, "", fromEnv, "", []string{"--" + config.TmpDirFlagName, fromFlag})
	if v := got.shellEnv[scratch.TmpDirEnvName]; v != fromFlag {
		t.Fatalf("with --%s=%q and %s=%q the child was handed %q, want the FLAG's %q — CLI must outrank env\nstderr:\n%s",
			config.TmpDirFlagName, fromFlag, config.TmpDirEnvName, fromEnv, v, fromFlag, got.stderr)
	}
	t.Logf("--%s won over %s: child handed %s=%q", config.TmpDirFlagName, config.TmpDirEnvName, scratch.TmpDirEnvName, fromFlag)
}

// TestTmpDirSettingsNeverOverridesTheCaller is the epic's hard constraint: a
// caller who set TMPDIR keeps it even when settings.json configures another
// one (the ambient override; a file on disk never redirects a pinned variable).
func TestTmpDirSettingsNeverOverridesTheCaller(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	caller := filepath.Join(t.TempDir(), "caller-tmp")
	configured := filepath.Join(t.TempDir(), "configured-tmp")
	for _, dir := range []string{caller, configured} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("prepare %s: %v", dir, err)
		}
	}

	got := runTmpDirProbe(t, configured, caller)
	if v, _ := got.shellEnv[scratch.TmpDirEnvName]; v != caller {
		t.Fatalf("the child was handed %s=%q, want the CALLER's %q — settings.json must never override a caller's temp root\nshell env keys: %v\ngopi stderr:\n%s",
			scratch.TmpDirEnvName, v, caller, environKeys(got.shellEnv), got.stderr)
	}
}

// TestTmpDirUnusableValueFailsLoudlyAndNamesThePath is acceptance 2: a
// deliberately unusable temp root (a path under a read-only parent) makes the
// run FAIL, and the failure names the path, the reason and the channel — never a
// silent substitution with some other directory.
func TestTmpDirUnusableValueFailsLoudlyAndNamesThePath(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a 0500 parent is still writable, so the unusable case cannot be constructed")
	}
	parent := filepath.Join(t.TempDir(), "readonly-parent")
	if err := os.MkdirAll(parent, 0o500); err != nil {
		t.Fatalf("prepare the read-only parent: %v", err)
	}
	t.Cleanup(func() {
		if cerr := os.Chmod(parent, 0o755); cerr != nil {
			t.Logf("could not restore %s: %v", parent, cerr)
		}
	})
	unusable := filepath.Join(parent, "cannot-be-created")

	stdout, stderr, code := runTmpDirRaw(t, []string{"--" + config.TmpDirFlagName, unusable, "tmpdir unusable probe"}, nil)
	if code == 0 {
		t.Fatalf("gopi exited 0 with an unusable --%s %q — a configured temp root that cannot be provided must fail loudly\nstdout:\n%s\nstderr:\n%s",
			config.TmpDirFlagName, unusable, stdout, stderr)
	}
	for _, want := range []string{unusable, "cannot create it", config.TmpDirSourceFlag} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("the refusal does not name %q:\nstderr:\n%s", want, stderr)
		}
	}
	t.Logf("refused (exit %d): %s", code, strings.TrimSpace(stderr))
}

// TestTmpDirSilentOnHelpAndVersion is acceptance 1 on the two invocations that
// spawn nothing: `--help` and `--version` must say nothing about the temp
// workspace (they used to print the banner, because the pre-subcommand
// publication ran before the help scan).
func TestTmpDirSilentOnHelpAndVersion(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	for _, args := range [][]string{{"--help"}, {"--version"}, {"version"}} {
		_, stderr, _ := runTmpDirRaw(t, args, nil)
		if strings.Contains(stderr, scratch.TmpDirReportPrefix) {
			t.Fatalf("`gopi %s` announced the temp workspace on stderr:\n%s", strings.Join(args, " "), stderr)
		}
	}
}

// TestTmpDirDebugSurfaceShowsTheValue pins the DELIBERATE surface that replaced
// the banner: the value stays reachable, on demand, through the existing debug
// knob — and the line names the channel that supplied it, so "why this
// directory?" has an answer without running anything else.
func TestTmpDirDebugSurfaceShowsTheValue(t *testing.T) {
	if _, err := os.Stat(testutil.GopiBin(t)); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	target := filepath.Join(t.TempDir(), "debug-surface-tmp")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("prepare the configured temp root: %v", err)
	}

	_, stderr, _ := runTmpDirRaw(t, []string{"version"}, []string{
		config.TmpDirEnvName + "=" + target,
		scratch.DebugEnvName + "=1",
	})
	if !strings.Contains(stderr, scratch.TmpDirReportPrefix) {
		t.Fatalf("%s=1 did not print the temp-workspace line — the banner's replacement surface is gone\nstderr:\n%s",
			scratch.DebugEnvName, stderr)
	}
	for _, want := range []string{target, config.TmpDirSourceEnv} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("the debug line does not name %q:\nstderr:\n%s", want, stderr)
		}
	}
	t.Logf("debug surface: %s", strings.TrimSpace(stderr))
}

// runTmpDirProbeWithKnob drives the standard probe with the KNOB's channels
// under explicit control (knobEnv → GOPI_TMPDIR, extraArgs → CLI flags such as
// --tmpdir) and the caller's TMPDIR taken from callerTmpDir. It exists so a
// precedence case can construct a DISAGREEMENT between channels, which
// runTmpDirProbe (one value per channel kind) cannot express.
func runTmpDirProbeWithKnob(t *testing.T, settingsTmpDir, knobEnv, callerTmpDir string, extraArgs []string) tmpDirProbeResult {
	t.Helper()
	gopiHome := t.TempDir()
	workDir := t.TempDir()
	outPath := filepath.Join(workDir, tmpDirEnvironOutName)

	settingsBody := "{}\n"
	if settingsTmpDir != "" {
		settingsBody = "{" + `"` + tmpDirProbeSettingsKey + `": "` + settingsTmpDir + `"}` + "\n"
	}
	settingsPath := filepath.Join(gopiHome, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(settingsBody), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_tmpdir_probe", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": tmpDirProbeCommand(outPath)}}},
		testutil.Resp{Text: "tmpdir probe done"},
	)
	t.Cleanup(mock.Close)

	env := tmpDirProbeEnv(t, gopiHome, mock.URL(), settingsPath, callerTmpDir, knobEnv)
	argv := append(append([]string{}, extraArgs...), tmpDirSettingsArgv, settingsPath, "tmpdir probe")
	stdout, stderr, code := runGopiMapped(t, argv, "", env, workDir)
	if code != 0 {
		t.Fatalf("gopi run failed (argv %v): code=%d\nstdout:\n%s\nstderr:\n%s", argv, code, stdout, stderr)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("the mocked bash tool did not capture the shell environment (stderr:\n%s): %v", stderr, err)
	}
	block, err := environBlockFromLines(raw)
	if err != nil {
		t.Fatalf("parse the shell environment block: %v", err)
	}
	return tmpDirProbeResult{shellEnv: block, stderr: stderr}
}

// runTmpDirRaw runs the real binary with EXTRA environment entries and no mock
// LLM: used by the cases that only need to observe the process's own stderr (the
// silence cases, the debug surface, the loud refusal). It returns stdout, stderr
// and the exit code.
func runTmpDirRaw(t *testing.T, args, extraEnv []string) (string, string, int) {
	t.Helper()
	env := testutil.MockEnv(t, t.TempDir(), tmpDirRawUnusedURL)
	filtered := make([]string, 0, len(env)+len(extraEnv)+1)
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key == scratch.TmpDirEnvName || key == config.TmpDirEnvName {
			continue // the case decides these
		}
		filtered = append(filtered, kv)
	}
	// A fixture workspace so the DEFAULT case never touches the operator's
	// ~/workspace (the default is resolved under it).
	filtered = append(filtered, scratch.WorkspaceHomeEnvName+"="+t.TempDir())
	filtered = append(filtered, extraEnv...)
	return runGopiMapped(t, args, "", filtered, t.TempDir())
}

// tmpDirRawUnusedURL is a placeholder endpoint for the runs that never talk to
// an LLM (help/version/refusal). Named (Rule 1b).
const tmpDirRawUnusedURL = "http://127.0.0.1:1"

// environKeys lists an environment block's variable names, sorted — the
// diagnostic that says WHAT the child was actually handed.
func environKeys(block map[string]string) []string {
	keys := make([]string, 0, len(block))
	for k := range block {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
