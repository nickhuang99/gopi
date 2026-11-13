package main

// The stale-window knob, end to end through the REAL binary — the half that
// unit tests cannot show: that the flag survives main()'s argument handling,
// that the verb reports the window it used, and that the window decides what
// the sweep actually collects.
//
// It works on a scratch home of its own (GOPI_TEST_SCRATCH_HOME) with an aged
// fixture, so nothing here touches the operator's tree, and it never creates
// load: the "age" of an entry is set with os.Chtimes, not waited for.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// staleWindowFixtureMinutes is how old the fixture entry is made: 90 minutes,
// i.e. BETWEEN the 30-minute window the tests configure and the 6h default —
// the interval that makes "was the knob honoured?" observable at all.
const staleWindowFixtureMinutes = 90

// TestScratchGCStaleWindow_FlagReachesTheSweep runs `gopi tool scratch-gc
// --scratch-gc-stale-min 30` against an aged fixture and asserts BOTH halves:
// the verb's own report names the 30m window (the flag survived main's argv
// handling), and the 90-minute-old entry is gone (the flag reached the sweep).
//
// Without the first assertion a "removed=1" could come from any window; without
// the second, a report is just a string.
func TestScratchGCStaleWindow_FlagReachesTheSweep(t *testing.T) {
	home, entry := staleWindowFixture(t)
	out := runScratchGCVerb(t, home, "--"+config.ScratchGCStaleMinFlagName, "30")
	if !strings.Contains(out, "stale=30m0s") {
		t.Fatalf("the verb did not report the configured window:\n%s", out)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("the 90m-old entry survived the 30m window (stat err=%v)\noutput:\n%s", err, out)
	}
}

// TestScratchGCStaleWindow_DefaultKeepsTheFixture is the control: with nothing
// configured, the SAME fixture (90 minutes old) is younger than the 6h default
// and must survive. It is what makes the test above evidence about the knob
// rather than about the fixture being collectable anyway.
func TestScratchGCStaleWindow_DefaultKeepsTheFixture(t *testing.T) {
	home, entry := staleWindowFixture(t)
	out := runScratchGCVerb(t, home)
	if !strings.Contains(out, "stale="+scratch.DefaultStaleAge.String()) {
		t.Fatalf("the default window is not what the verb reports:\n%s", out)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("the default window collected a 90m-old entry: %v\noutput:\n%s", err, out)
	}
}

// TestScratchGCStaleWindow_EnvFaceReachesTheSweep pins the second channel: the
// same window set through the knob's environment variable, with no flag at all.
func TestScratchGCStaleWindow_EnvFaceReachesTheSweep(t *testing.T) {
	home, entry := staleWindowFixture(t)
	out := runScratchGCVerb(t, home, "env:"+scratch.GCStaleAgeEnvName+"=45")
	if !strings.Contains(out, "stale=45m0s") {
		t.Fatalf("the env channel did not reach the verb:\n%s", out)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("the 90m-old entry survived the 45m window (stat err=%v)\noutput:\n%s", err, out)
	}
}

// TestScratchGCStaleWindow_BadValueIsRefused pins Rule 15's non-negotiable half
// for a watchdog verb: a window that cannot be honoured must FAIL LOUDLY rather
// than silently sweep with the default — the opposite would let a mistyped
// watchdog reclaim nothing forever while its log reads healthy.
func TestScratchGCStaleWindow_BadValueIsRefused(t *testing.T) {
	home, entry := staleWindowFixture(t)
	out := runScratchGCVerbExpectFailure(t, home, "--"+config.ScratchGCStaleMinFlagName, "soon")
	if !strings.Contains(out, "is not a positive number of minutes") {
		t.Fatalf("a bad window was not refused with a reason:\n%s", out)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("a refused invocation must not touch the tree: %v", err)
	}
}

// staleWindowFixture builds an isolated scratch home holding ONE in-namespace
// entry aged staleWindowFixtureMinutes, and returns both paths.
func staleWindowFixture(t *testing.T) (home, entry string) {
	t.Helper()
	home = t.TempDir()
	entry = filepath.Join(home, scratch.SuiteRunMarker+"stale-window-e2e-run-1")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	aged := time.Now().Add(-staleWindowFixtureMinutes * time.Minute)
	if err := os.Chtimes(entry, aged, aged); err != nil {
		t.Fatalf("age the fixture: %v", err)
	}
	return home, entry
}

// runScratchGCVerb runs the real `gopi tool scratch-gc` binary against home with a
// scrubbed environment (no ambient GOPI_*), plus any extra args. An argument of
// the form "env:NAME=VALUE" is applied to the CHILD's environment instead of
// being passed as argv (the env channel needs no flag).
func runScratchGCVerb(t *testing.T, home string, args ...string) string {
	t.Helper()
	out, err := runScratchGCVerbAllowFailure(t, home, args...)
	if err != nil {
		t.Fatalf("gopi tool scratch-gc %v: %v\n--- output ---\n%s", args, err, out)
	}
	return out
}

func runScratchGCVerbExpectFailure(t *testing.T, home string, args ...string) string {
	t.Helper()
	out, err := runScratchGCVerbAllowFailure(t, home, args...)
	if err == nil {
		t.Fatalf("gopi tool scratch-gc %v: expected a non-zero exit\n--- output ---\n%s", args, out)
	}
	return out
}

// runScratchGCVerbAllowFailure is the shared runner: a scrubbed environment plus
// GOPI_TEST_SCRATCH_HOME + GOPI_HOME pointed into the test's own temp tree, so
// the verb cannot read or write anything of the operator's.
func runScratchGCVerbAllowFailure(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	argv := make([]string, 0, len(args))
	extraEnv := []string{}
	for _, a := range args {
		if strings.HasPrefix(a, "env:") {
			extraEnv = append(extraEnv, strings.TrimPrefix(a, "env:"))
			continue
		}
		argv = append(argv, a)
	}
	cmd := exec.Command(gopiTestBinPath(t), append(familyAddressArgs([]string{CliVerbScratchGC}), argv...)...)
	cmd.Env = append(scrubbedGopiEnv(t, home), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// scrubbedGopiEnv is cliContractEnv's shape (drop every ambient GOPI_*/
// ANTHROPIC_*/DEEPSEEK_* variable, then point the data root and the scratch home
// at the test's own tree) — reused deliberately, because a task-run child
// exports GOPI_SETTINGS / GOPI_TASK_WRAPPER_ID and those would redirect the very
// resolution this test is about.
func scrubbedGopiEnv(t *testing.T, scratchHome string) []string {
	t.Helper()
	env := cliContractEnv(t.TempDir())
	kept := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, scratch.HomeEnvName+"=") {
			continue // replaced below
		}
		kept = append(kept, kv)
	}
	return append(kept, scratch.HomeEnvName+"="+scratchHome)
}
