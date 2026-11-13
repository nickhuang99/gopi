// cli_sandbox_show_surface_test.go — the `show` sub-verb's DOCUMENTED SURFACE,
// driven through the REAL BINARY (Rule 31), because both defects it pins are
// invisible to a unit test: they exist only in argv.
//
// THE TWO ASYMMETRIES, as measured on the pre-fix binary:
//
//	$ gopi tool sandbox show --sandbox-dir /tmp/root   → exit 2 "unexpected
//	  argument \"--sandbox-dir\"" — while `create` and `run` accept the very same
//	  flag, the verb's own usage page lists it, and CONFIG.md §1c promises it to
//	  the whole `gopi tool sandbox` verb;
//	$ gopi tool sandbox show -h                        → exit 0 and a STATUS LINE
//	  (root=… source=…), so the help flag was swallowed as a positional and the
//	  usage page was unreachable through the sub-verb.
//
// The repair is that the FLAG TAKES EFFECT — never that the help is narrowed to
// match the refusal (the ruling recorded with the text2img `--out` defect: a
// flag advertised-and-refused is fixed by making it work). So `show` now reads
// `--sandbox-dir` and routes it through the ONE resolver, `-h`/`--help` prints
// the usage page, and everything else is still a usage error naming the
// vocabulary it WILL accept.
//
// The resolver precedence is asserted here too, one probe per clause, because
// the flag is only correct if it ranks where CONFIG.md §1c says it does:
// flag > env > settings > default. A `show` that let the env channel win would
// pass a naive "the flag is accepted" test and still lie about the root.
//
// Fixtures are reused, never reinvented: gopiTestBinPath (gopibin_test.go)
// builds the binary ONCE per suite, cliContractEnv (cli_contracts_test.go)
// scrubs the environment so no probe can reach the operator's profile, and
// cliSandboxRunEnv (cli_sandbox_ledger_test.go) is the SAME real-binary driver
// the ledger tests use — this file adds no second way to spawn the CLI.
package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// sandboxShowStatusLinePrefix is the machine-readable shape of `show`'s stdout
// — the line a script parses. A constant rather than a literal, so the
// assertion and the human-facing format cannot drift apart unnoticed.
const sandboxShowStatusLinePrefix = "sandbox root="

// sandboxShowRootField extracts the `root=` value from a `show` status line and
// fails the probe when the line is not the documented shape at all (a missing
// status line is a different defect from a wrong root, and the two must not be
// reported as one).
func sandboxShowRootField(t *testing.T, out string) string {
	t.Helper()
	line := strings.TrimSpace(out)
	if !strings.HasPrefix(line, sandboxShowStatusLinePrefix) {
		t.Fatalf("`show` did not print its status line (want a line starting with %q):\n%s",
			sandboxShowStatusLinePrefix, out)
	}
	// Fields are read off the WHOLE line: the prefix itself carries the
	// `root=` field, so trimming it first would delete the value being parsed
	// (measured — the first draft did exactly that and reported "no root=").
	for _, field := range strings.Fields(line) {
		if value, ok := strings.CutPrefix(field, "source="); ok {
			if value == "" {
				t.Fatalf("`show` printed an empty source= — it must name the channel that decided the root:\n%s", out)
			}
			continue
		}
		if root, ok := strings.CutPrefix(field, "root="); ok {
			return root
		}
	}
	t.Fatalf("`show` printed no root= field:\n%s", out)
	return ""
}

// TestSandboxShowAcceptsTheRootFlagItsVerbAdvertises is the flag contract: the
// flag is accepted in BOTH its spellings (separate value and `=`), the value
// reaches the resolved root, and the deciding channel is reported as the flag's
// own — the last clause is what makes it the resolved root and not a
// coincidence.
func TestSandboxShowAcceptsTheRootFlagItsVerbAdvertises(t *testing.T) {
	home := t.TempDir()
	// A root that does NOT exist: `show` resolves configuration and materialises
	// nothing, so it must answer for a root nobody has created yet — that is the
	// question an operator asks BEFORE the first sandbox exists.
	flagRoot := filepath.Join(t.TempDir(), "not-yet-created")

	spellings := [][]string{
		{SandboxDirFlag, flagRoot},
		{SandboxDirFlag + "=" + flagRoot},
	}
	for _, spelling := range spellings {
		args := append([]string{CliVerbSandbox, SandboxShowVerb}, spelling...)
		out, code := cliSandboxRunEnv(t, gopiTestBinPath(t), cliContractEnv(home), args...)
		if code != 0 {
			t.Fatalf("`gopi tool %s` (spelling %q) exited %d — the flag its own verb advertises must be accepted:\n%s",
				strings.Join(args, " "), strings.Join(spelling, " "), code, out)
		}
		if got := sandboxShowRootField(t, out); got != flagRoot {
			t.Errorf("`%s` (spelling %q) resolved root=%s, want %s\n%s",
				SandboxDirFlag, strings.Join(spelling, " "), got, flagRoot, out)
		}
		if want := config.SandboxRootSourceFlag; !strings.Contains(out, "source="+want) {
			t.Errorf("`%s` did not report %q as the deciding channel — the flag must be the HIGHEST precedence:\n%s",
				SandboxDirFlag, want, out)
		}
	}
}

// TestSandboxShowPrecedenceAndNoRegression pins the OTHER three channels on the
// same sub-verb: the env channel, the built-in default (reported AS the
// default, never presented as a discovery), and the flag above the env. Without
// this, "the flag works" could be satisfied by a `show` that ignored env and
// settings entirely.
func TestSandboxShowPrecedenceAndNoRegression(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	envRoot := filepath.Join(t.TempDir(), "env-root")
	flagRoot := filepath.Join(t.TempDir(), "flag-root")

	// The env channel, alone.
	out, code := cliSandboxRunEnv(t, bin,
		append(cliContractEnv(home), config.SandboxEnvName+"="+envRoot),
		CliVerbSandbox, SandboxShowVerb)
	if code != 0 {
		t.Fatalf("`gopi tool sandbox show` with %s set exited %d:\n%s", config.SandboxEnvName, code, out)
	}
	if got := sandboxShowRootField(t, out); got != envRoot {
		t.Errorf("with %s set, root=%s, want %s\n%s", config.SandboxEnvName, got, envRoot, out)
	}

	// No channel configured: the built-in default.
	//
	// The default is resolved from the WORKSPACE HOME (scratch.WorkspaceUnderHome:
	// $GOPI_TEST_HOME, else the OS user's home) — and it is resolved by the CHILD
	// PROCESS, whose environment this probe owns. cliContractEnv SCRUBS every
	// GOPI_* variable (that scrub is what keeps a probe off the operator's real
	// profile), so a probe that read the suite's scratch home here would compute
	// its expectation from a home the binary under test never sees: measured
	// 2026-10-15, the child fell through to the operator's own
	// $HOME/workspace/gopi-sandbox while the parent expected the value under the
	// suite's scratch home, and the assertion compared two different homes.
	//
	// The expectation therefore reads the SAME source of truth the resolver does
	// (config.SandboxRootDefault, Rule 2 — one resolution, never a re-typed
	// literal) with the channel both sides must agree on pinned to ONE value:
	// the probe passes the workspace home to the child AND pins it in this
	// process, so parent and child resolve the default for the same home.
	t.Setenv(scratch.WorkspaceHomeEnvName, home)
	out, code = cliSandboxRunEnv(t, bin,
		append(cliContractEnv(home), scratch.WorkspaceHomeEnvName+"="+home),
		CliVerbSandbox, SandboxShowVerb)
	if code != 0 {
		t.Fatalf("`gopi tool sandbox show` with no channel configured exited %d:\n%s", code, out)
	}
	if got, want := sandboxShowRootField(t, out), config.SandboxRootDefault(); got != want {
		t.Errorf("with no channel configured, root=%s, want the built-in default %s\n%s", got, want, out)
	}
	if want := config.SandboxRootSourceDefault; !strings.Contains(out, "source="+want) {
		t.Errorf("the built-in default was not reported as %q:\n%s", want, out)
	}

	// The flag RANKS ABOVE the env channel — the clause that makes it the
	// highest-precedence channel rather than merely a second winner.
	out, code = cliSandboxRunEnv(t, bin,
		append(cliContractEnv(home), config.SandboxEnvName+"="+envRoot),
		CliVerbSandbox, SandboxShowVerb, SandboxDirFlag, flagRoot)
	if code != 0 {
		t.Fatalf("`gopi tool sandbox show %s` with %s also set exited %d:\n%s",
			SandboxDirFlag, config.SandboxEnvName, code, out)
	}
	if got := sandboxShowRootField(t, out); got != flagRoot {
		t.Errorf("flag over env: root=%s, want %s (the flag must win)\n%s", got, flagRoot, out)
	}
}

// TestSandboxShowDefaultFollowsTheWorkspaceHomeTheProbePins is the regression
// guard for the asymmetry that made TestSandboxShowPrecedenceAndNoRegression red
// (measured 2026-10-15, pre-existing since e3b4056cb): the expectation and the
// resolver disagreed about the HOME the built-in default hangs under, because
// cliContractEnv scrubs every GOPI_* variable — including the scratch home — so
// the child resolved the default from the operator's real $HOME while the parent
// computed it from the suite's scratch home.
//
// The property pinned here is not a path and not a spelling: the built-in
// default is the resolver's own value for the workspace home THE CHILD WAS GIVEN
// (config.SandboxRootDefault — Rule 2, one resolution, the same function the
// binary runs), and it is read from that home and no other. The ambient scratch
// home is deliberately set to a third directory, so a probe that let the
// runner's ambient value decide — the old shape — reports a root under the wrong
// home and this test goes red on its own, without depending on how the suite
// happens to be invoked.
func TestSandboxShowDefaultFollowsTheWorkspaceHomeTheProbePins(t *testing.T) {
	bin := gopiTestBinPath(t)
	// An ambient scratch home that must NOT decide the answer.
	t.Setenv(scratch.WorkspaceHomeEnvName, t.TempDir())

	for _, probedHome := range []string{t.TempDir(), t.TempDir()} {
		// The parent's own reading of the one channel, so the expectation and
		// the resolver cannot be comparing two different homes.
		t.Setenv(scratch.WorkspaceHomeEnvName, probedHome)
		want := config.SandboxRootDefault()
		if !strings.HasPrefix(want, probedHome) {
			t.Fatalf("the built-in default %s does not hang under the workspace home %s this probe pinned — the expectation would be measuring another channel", want, probedHome)
		}
		env := append(cliContractEnv(t.TempDir()), scratch.WorkspaceHomeEnvName+"="+probedHome)
		out, code := cliSandboxRunEnv(t, bin, env, CliVerbSandbox, SandboxShowVerb)
		if code != 0 {
			t.Fatalf("`gopi tool sandbox show` exited %d with the workspace home pinned to %s:\n%s", code, probedHome, out)
		}
		got := sandboxShowRootField(t, out)
		if got != want {
			t.Errorf("with the workspace home %s and no sandbox channel configured, root=%s, want %s (the same value %s resolves for that home)\n%s",
				probedHome, got, want, "config.SandboxRootDefault", out)
		}
		if !strings.HasPrefix(got, probedHome) {
			t.Errorf("root=%s does not live under the workspace home the probe pinned (%s) — the ambient home decided it instead\n%s",
				got, probedHome, out)
		}
	}
}

// TestSandboxShowAnswersHelpWithTheUsagePage is the help contract: `-h` and
// `--help` print the verb's usage page — the one whose fingerprint the CLI
// contract suite already asserts — and NOT a status line. Pre-fix this probe saw
// `sandbox root=… source=…` with exit 0: help was swallowed as a positional, so
// the page was unreachable through `show`.
func TestSandboxShowAnswersHelpWithTheUsagePage(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	for _, help := range []string{HelpFlag, HelpFlagLong} {
		out, code := cliSandboxRunEnv(t, bin, cliContractEnv(home), CliVerbSandbox, SandboxShowVerb, help)
		if code != 0 {
			t.Fatalf("`gopi tool sandbox show %s` exited %d, want 0:\n%s", help, code, out)
		}
		if !strings.HasPrefix(out, sandboxUsageFingerprint) {
			t.Errorf("`gopi tool sandbox show %s` did not print the usage page (want it to START with the fingerprint %q):\n%s",
				help, sandboxUsageFingerprint, out)
		}
		if strings.HasPrefix(out, sandboxShowStatusLinePrefix) {
			t.Errorf("`gopi tool sandbox show %s` printed a STATUS LINE — the help flag was swallowed as a positional:\n%s",
				help, out)
		}
		// The page must advertise the flag this sub-verb now honours: a help
		// page silent about the accepted flag is the same asymmetry, repaired.
		if !strings.Contains(out, SandboxDirFlag) {
			t.Errorf("the usage page does not name %s, so the flag `show` accepts is still undocumented:\n%s",
				SandboxDirFlag, out)
		}
	}
}

// TestSandboxShowRefusesUnknownArgumentsByName keeps the third leg honest: the
// repair widens what `show` ACCEPTS, and it must not widen it into "anything
// goes". An unknown argument — including the two flags `show` deliberately does
// not read, because it materialises nothing to label or to own — is a usage
// error, and the message names the vocabulary that WOULD be accepted.
func TestSandboxShowRefusesUnknownArgumentsByName(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()

	for _, bad := range []string{
		"--bogus",
		SandboxLabelFlag + " build",
		SandboxTaskIDFlag + " some-task",
		"stray-positional",
	} {
		args := append([]string{CliVerbSandbox, SandboxShowVerb}, strings.Fields(bad)...)
		out, code := cliSandboxRunEnv(t, bin, cliContractEnv(home), args...)
		if code != sandboxExitUsage {
			t.Errorf("`gopi tool %s` exited %d, want the usage status %d:\n%s",
				strings.Join(args, " "), code, sandboxExitUsage, out)
		}
		if !strings.Contains(out, "want ") || !strings.Contains(out, SandboxDirFlag) {
			t.Errorf("`gopi tool %s` did not name the accepted vocabulary (%s) in its refusal:\n%s",
				strings.Join(args, " "), SandboxDirFlag, out)
		}
		if strings.Contains(out, sandboxShowStatusLinePrefix) {
			t.Errorf("`gopi tool %s` printed a status line on the refusal path — a usage error must not answer the question it refused:\n%s",
				strings.Join(args, " "), out)
		}
	}
}
