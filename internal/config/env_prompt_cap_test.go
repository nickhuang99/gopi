package config_test

// env_prompt_cap_test.go — the regression for the OVERSIZED-PROMPT environment
// defect (epic 01a1b2af, measured 2026-11-07).
//
// THE DEFECT, as it was measured: a ~1MB `--prompt @file` prompt was published
// into the process environment as GOPI_PROMPT, which is ONE string longer than
// the kernel's per-string execve limit (MAX_ARG_STRLEN). Every subsequent
// fork/exec in that process then failed E2BIG — `git rev-parse` (the clone
// channel's ground resolution and its anchor gate) and `sh -c` (notify.sh, and
// the task-row writeback) included:
//
//	[gopi] clone channel: cannot resolve the ground … git rev-parse --abbrev-ref HEAD:
//	       fork/exec /usr/local/bin/git: argument list too long
//	[task-notify] cmd "bash /home/user/.gopi/notify/notify.sh":
//	       fork/exec /usr/bin/sh: argument list too long
//
// So ONE oversized prompt turned "a large prompt" into "this runtime cannot
// spawn ANYTHING", and the run's own delivery leg and ledger writeback died
// while the model side was never even reached.
//
// WHAT THIS TEST MEASURES — a real child process, not a string comparison. The
// whole failure is in the kernel's exec boundary, so a test that only asserts
// `os.Getenv(GOPI_PROMPT) == ""` would be green on a machine where the limit is
// different, or green because nothing ever forked. Each arm therefore EXECS a
// child (git and sh, the two the incident actually lost) and requires it to
// start.
//
// This is an EXTERNAL test package (config_test) for the same reason
// testmain_scratchroot_test.go is: internal/testutil imports internal/config, so
// Rule 17's restore helper (testutil.SnapshotEnv) cannot be reached from the
// internal package without an import cycle.

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// oversizedPrompt builds a prompt ONE byte past the per-string execve limit —
// the smallest sample that reproduces the E2BIG (Rule 10's minimal sample: 1 byte
// over is the whole defect, and a 1MB sample costs the test 1MB of memory for the
// same verdict).
func oversizedPrompt() string {
	return strings.Repeat("x", config.ExecveMaxStringBytes+1)
}

// execOK runs argv with the CURRENT process environment inherited (the whole
// point: the child is what fails) and reports the failure verbatim.
func execOK(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("spawned %q %v FAILED: %v (output %q) — this is the defect: an "+
			"oversized GOPI_PROMPT in this process's environment makes every "+
			"fork/exec fail E2BIG", name, args, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

// TestOversizedPromptIsNotPublishedAndChildrenStillExec is the regression arm.
//
// RED before the fix (measured on this tree, 2026-11-07): both execs fail with
// `argument list too long`. GREEN after: the publication is skipped, the value
// travels in-process, and both children start.
func TestOversizedPromptIsNotPublishedAndChildrenStillExec(t *testing.T) {
	// t.Setenv pins + RESTORES the variable for this test (Rule 17): an
	// oversized value left behind by a red run would break every later exec in
	// this binary.
	t.Setenv(config.PromptEnvVar, "")

	big := oversizedPrompt()
	config.ApplyEnv(&config.Config{Prompt: big}, false)

	// 1. The environment must not carry it. Publishing it is the defect: this
	//    single string is what makes the kernel refuse every exec.
	if got := os.Getenv(config.PromptEnvVar); got != "" {
		t.Fatalf("%s was published into the environment (%d bytes) — above the "+
			"%d-byte per-string execve limit this makes every child fail E2BIG",
			config.PromptEnvVar, len(got), config.ExecveMaxStringBytes)
	}

	// 2. …but the RUN must still see its prompt: the value travels in-process,
	//    so skipping the publication is not a silent drop (Rule 15).
	if got := config.ProcessOnlyPrompt(); got != big {
		t.Fatalf("ProcessOnlyPrompt() = %d bytes, want the %d-byte prompt — the "+
			"prompt must not vanish with the unpublished env var",
			len(got), len(big))
	}

	// 3. THE POINT: real children start. git is the clone channel's ground
	//    resolution; sh is notify.sh and the task-row writeback. Both are the
	//    commands the incident lost.
	execOK(t, "git", "rev-parse", "--git-dir")
	execOK(t, "sh", "-c", "exit 0")

	// 4. And the child sees an environment WITHOUT the prompt, which is the
	//    property that lets it start at all.
	out := execOK(t, "sh", "-c", "printf %s \"$"+config.PromptEnvVar+"\"")
	if out != "" {
		t.Fatalf("child inherited %d bytes of %s, want 0", len(out), config.PromptEnvVar)
	}
}

// TestNormalPromptStillReachesTheEnvironment is the non-regression arm: the
// documented GOPI_PROMPT channel (cmd/agent/main_test.go's P1-3 arms read it)
// must be untouched for a prompt the environment can carry. Without this, "fix
// the oversized case" could be satisfied by never publishing anything — and the
// settings-file `prompt` key reaches readInput ONLY through this publication.
func TestNormalPromptStillReachesTheEnvironment(t *testing.T) {
	t.Setenv(config.PromptEnvVar, "")

	const small = "task from the settings file"
	config.ApplyEnv(&config.Config{Prompt: small}, false)

	if got := os.Getenv(config.PromptEnvVar); got != small {
		t.Fatalf("%s = %q, want %q — the normal-size env channel must stay live",
			config.PromptEnvVar, got, small)
	}
	if got := config.ProcessOnlyPrompt(); got != "" {
		t.Fatalf("ProcessOnlyPrompt() = %q, want empty when the environment "+
			"carried the prompt", got)
	}
}

// TestExecutorConsumesTheInProcessPrompt closes the loop at the CONSUMER: the
// carrier is only worth anything if the run path reads it, so this arm drives
// the real publication and then reads the value the way the CLI does. It is
// deliberately here (not in cmd/agent) because the CONDITION it protects is the
// publication point's: "published ⇒ the reader finds it, unpublished ⇒ the
// reader still finds it".
func TestExecutorConsumesTheInProcessPrompt(t *testing.T) {
	t.Setenv(config.PromptEnvVar, "")

	big := oversizedPrompt()
	config.ApplyEnv(&config.Config{Prompt: big}, false)

	// The reader's own precedence, in one line: the environment first (it is
	// the operator's channel), the in-process carrier when the environment
	// could not carry the value.
	got := os.Getenv(config.PromptEnvVar)
	if got == "" {
		got = config.ProcessOnlyPrompt()
	}
	if got != big {
		t.Fatalf("the run path would read %d bytes, want %d — an oversized "+
			"prompt must still reach the run", len(got), len(big))
	}
}
