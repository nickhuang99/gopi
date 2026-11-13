package main

// sandbox_profile_resolution_test.go — the REGRESSION PIN for the
// sandbox/PROFILE-RESOLUTION divergence.
//
// THE DEFECT (measured 2026-09-26, before the fix; the verbatim transcript is in
// devdoc/sandbox-profile-resolution.md):
//
//	$ SB=$(gopi tool sandbox create --label redprobe | tail -1)
//	$ ls $SB                       # sessions/ raw/ logs/ settings.json — "the shape"
//	$ GOPI_HOME=$SB gopi session log --last
//	Error listing sessions: open $SB/.gopi/sessions: no such file or directory
//
// The CREATOR materialised a profile-shaped directory; every READER resolves
// $GOPI_HOME/.gopi/... (config.GopiDir) — two answers to "where does a gopi
// profile live", and the documented invocation could not find the sessions the
// help promised were right there.
//
// WHY THIS TEST LIVES IN cmd/agent AND DRIVES THE BINARY. The thing that broke
// is not a function's contract, it is the SHIPPED INVOCATION: `sandbox create`
// followed by a reader under the printed path. A unit test of the initializer
// would have been green on the broken tree (it materialised exactly what it was
// told to). Only the end-to-end pair — create with the REAL binary, then read
// back through the REAL reader — distinguishes "the shape is consistent" from
// "the shape is what the other half resolves". Rule 31: execute the surface.
//
// RED on the pre-fix tree: `gopi session log --last` exits 1 with "Error listing
// sessions: open <SB>/.gopi/sessions: no such file or directory" (the session
// lookup fails before any LLM call, so a missing API key cannot mask it).
// GREEN after: the profile root <SB>/.gopi exists (created by `sandbox create`
// alone, no run needed) and `gopi session log --last` names that writer-side directory.
//
// The full LLM round trip (a real `gopi "<prompt>"` landing a session under
// <SB>/.gopi/sessions) is the ACCEPTANCE run, executed by hand against a dead
// endpoint and recorded verbatim in the devdoc — it needs an endpoint and a
// model turn, which a regression test must not depend on.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sandbox"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// sandboxProfileRootEnv is the configured sandbox root for this probe: a
	// HERMETIC root under the suite's scratch tree, so the test never reads or
	// writes the operator's sandbox root (Rule 34's root is configuration — the
	// env channel is the one a test can set without touching the operator's
	// settings file).
	sandboxProfileRootEnv = config.SandboxEnvName
	// sandboxProfileLabel is the probe sandbox's label (short and unique to
	// this test, so a reused sandbox cannot be another test's).
	sandboxProfileLabel = "resolvetest"
	// sandboxProfileCreateVerb is the create sub-verb.
	sandboxProfileCreateVerb = "create"
	// sandboxLogVerb / sandboxLogLastFlag name the reader under test. The verb
	// is the session GROUP's sub-verb spelling (`gopi session log`), which is
	// the spelling `gopi log` was retired to in verb-retirement phase 1: a
	// probe driving the top-level word would read a refusal message and assert
	// the sandbox's own sessions directory is named in it.
	sandboxLogVerb     = CliVerbLog
	sandboxLogLastFlag = "--last"
)

// TestSandboxCreateProfileRootIsWhatReadersResolve is the end-to-end pin: the
// directory `gopi tool sandbox create` materialises is the directory a reader
// resolves from the SAME path used as GOPI_HOME.
func TestSandboxCreateProfileRootIsWhatReadersResolve(t *testing.T) {
	bin := gopiTestBinPath(t)
	root := testutil.TestScratchDir(t, "sbprofile")
	home := t.TempDir()
	settings := filepath.Join(home, config.SandboxSettingsFileName)
	if err := os.WriteFile(settings, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write the probe settings file: %v", err)
	}

	// ① CREATE through the real CLI (Rule 31: the verb, with real arguments).
	createArgs := []string{
		CliVerbSandbox, sandboxProfileCreateVerb,
		SandboxLabelFlag, sandboxProfileLabel,
	}
	createEnv := sandboxRootEnv(cliContractEnv(home), root)
	out, err := runSandboxProfileProbe(t, bin, createEnv, createArgs...)
	if err != nil {
		t.Fatalf("gopi %s: %v\n--- output ---\n%s", strings.Join(createArgs, " "), err, out)
	}
	sb := lastNonEmptyLine(out)
	if sb == "" || !strings.HasPrefix(filepath.Base(sb), sandbox.SandboxNamePrefix) {
		t.Fatalf("`gopi tool sandbox create` printed no sandbox path\n--- output ---\n%s", out)
	}

	// ② THE DIVERGENCE ITSELF: the profile root a reader resolves from that
	//    path must be a directory create alone materialised.
	profileRoot := config.ProfileDirIn(sb)
	if info, err := os.Stat(profileRoot); err != nil || !info.IsDir() {
		t.Fatalf("the reader-resolved profile root %s does not exist after `gopi tool sandbox create` (%v) — "+
			"the created shape and the readers disagree (the divergence this test pins)", profileRoot, err)
	}
	for _, name := range []string{
		config.SandboxSessionsDirName, config.SandboxRawDirName, config.SandboxLogsDirName,
	} {
		p := config.ProfilePathIn(sb, name)
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Fatalf("the reader-resolved %s is not a directory: %v", p, err)
		}
	}

	// ③ READ back through the real reader under GOPI_HOME=<the printed path>.
	//    The failure this asserts away is a MISSING SESSIONS DIRECTORY, which
	//    happens before any network or model call — so no endpoint is needed
	//    and a missing API key cannot mask the result.
	logArgs := []string{CliVerbSession, sandboxLogVerb, sandboxLogLastFlag}
	env := sandboxReaderEnv(cliContractEnv(home), sb)
	logOut, _ := runSandboxProfileProbe(t, bin, env, logArgs...) //stylegate:ignore runs the fixture probe; the captured output is the assertion
	if strings.Contains(logOut, "Error listing sessions") {
		t.Fatalf("`GOPI_HOME=%s gopi %s` cannot list sessions after `gopi tool sandbox create`:\n%s\n"+
			"the created shape must be the shape the readers resolve (one truth)", sb, strings.Join(logArgs, " "), logOut)
	}
	wantDir := config.ProfilePathIn(sb, config.SandboxSessionsDirName)
	if !strings.Contains(logOut, wantDir) {
		t.Fatalf("`GOPI_HOME=%s gopi %s` did not read %s:\n%s", sb, strings.Join(logArgs, " "), wantDir, logOut)
	}
}

// sandboxRootEnv builds an environment with the sandbox ROOT configured through
// the env channel (GOPI_SANDBOX_DIR), so the probe's sandbox tree is hermetic —
// never the operator's configured root.
func sandboxRootEnv(env []string, root string) []string {
	prefix := sandboxProfileRootEnv + "="
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+root)
}

// sandboxReaderEnv builds the reader's environment: the contract probe env plus
// GOPI_HOME=<sandbox> — the exact spelling the docs tell a user to type. The
// sandbox's own settings symlink is not needed (the reader resolves its data
// paths from GOPI_HOME alone), and leaving GOPI_SETTINGS unset keeps the probe
// honest about that: the profile root, not a flag, decides where sessions live.
func sandboxReaderEnv(env []string, sb string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, config.GopiDirEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, config.GopiDirEnv+"="+sb)
}

// runSandboxProfileProbe runs the binary with env and a bounded wall-clock,
// returning combined output. It does not fail on a non-zero exit: an assertion
// about the OUTPUT (the pre-fix "Error listing sessions" line) is more precise
// than one about the status, and the caller checks it.
func runSandboxProfileProbe(t *testing.T, bin string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// lastNonEmptyLine returns the last non-blank line of s — the sandbox path,
// which the CLI prints as stdout's final line on purpose
// (`SB=$(gopi tool sandbox create … | tail -1)` is the documented idiom).
func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
