package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/text2img"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// ---------------------------------------------------------------------------
// `gopi tool text2img --size 0` (and the four sibling numeric flags) driven through
// the REAL binary with REAL argv.
//
// The bug this file is the anti-regression guard for (measured 2026-09-26,
// pre-fix binary): flagInt returned the parsed 0, the dispatch walk refused
// only `v < 0`, and internal/text2img's norm() rerouted 0 to the default — so
// `--size 0` exited 0 and rendered at 字号 20px while the refusal message said
// "requires a positive integer". A unit test on parseText2imgArgs cannot catch
// that class: the zero was parsed correctly and the silent reroute happened
// two layers away, in the renderer. Only the real argv → exit code → summary
// line chain shows it (Rule 31).
// ---------------------------------------------------------------------------

// text2imgZeroE2ETimeout bounds one probe. Rendering here takes milliseconds,
// so a probe that has not returned has fallen through to the agent loop (the
// same failure mode the help-surface probes guard against).
const text2imgZeroE2ETimeout = 30 * time.Second

// text2imgE2ESummaryPrefix opens the render summary line tools.Text2Img
// prints. Seeing it (and NOT on a refusal) is the observation that a render
// really happened.
const text2imgE2ESummaryPrefix = "✅"

// text2imgE2EInputBody is the fixture's content — short on purpose: the probe
// is about the flag contract, not about rendering.
const text2imgE2EInputBody = "Hello text2img zero-value contract probe.\n"

// text2imgE2EInvalidValues is the refusal set for the parse layer: a word that
// is not an integer, and integers below the stated minimum.
var text2imgE2EInvalidValues = []string{"abc", "0", "-5"}

// text2imgE2EFlagNames is the five numeric flags the subcommand validates.
// It is derived from the same constants the parser and the usage page use, so
// a sixth numeric flag cannot be added without this file following.
var text2imgE2EFlagNames = []string{
	text2imgFlagSize,
	text2imgFlagCols,
	text2imgFlagLh,
	text2imgFlagLines,
	text2imgFlagMargin,
}

// text2imgE2ESandbox prepares an isolated profile for one probe case: a
// private GOPI_HOME (so a probe can never touch the operator's profile) plus
// the input fixture. The directory is a t.TempDir, i.e. already off `/` under
// the suite's pinned scratch root (Rule 22).
func text2imgE2ESandbox(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("sandbox home: %v", err)
	}
	return home
}

// text2imgE2EInput writes the input fixture and returns its path.
func text2imgE2EInput(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(in, []byte(text2imgE2EInputBody), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return in
}

// text2imgE2EProbe runs the real binary with the real argv and returns
// (stdout, stderr, exit code). stdout and stderr are captured SEPARATELY — a
// run that succeeded and one that failed must not look alike — and a timeout
// is a failure of the probe, never a silent pass.
func text2imgE2EProbe(t *testing.T, bin, home string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	cmd.Env = append(os.Environ(), "GOPI_HOME="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start `gopi %s`: %v", strings.Join(args, " "), err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(text2imgZeroE2ETimeout):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("`gopi %s`: TIMEOUT after %s — argv fell through to the agent loop:\n%s",
			strings.Join(args, " "), text2imgZeroE2ETimeout, stdout.String()+stderr.String())
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("`gopi %s`: %v", strings.Join(args, " "), err)
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// TestText2imgZeroFlagIsRefusedE2E is the regression guard for the stated
// contract: GIVEN must be a positive integer. Every one of the five numeric
// flags is driven with 0 through the real binary and must be REFUSED (exit 1)
// with the flag named on stderr and the usage page attached — never a silent
// run at the default.
func TestText2imgZeroFlagIsRefusedE2E(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := text2imgE2ESandbox(t)
	in := text2imgE2EInput(t)

	for _, flag := range text2imgE2EFlagNames {
		flag := flag
		t.Run(flag, func(t *testing.T) {
			stdout, stderr, code := text2imgE2EProbe(t, bin, home,
				toolnames.ToolText2img, in, "--"+flag, "0")
			if code != 1 {
				t.Errorf("`text2img --%s 0` exit = %d, want 1 (stdout=%q stderr=%q)",
					flag, code, stdout, stderr)
			}
			want := "--" + flag + " " + text2imgPositiveIntMessage
			if !strings.Contains(stderr, want) {
				t.Errorf("`text2img --%s 0` stderr missing %q:\n%s", flag, want, stderr)
			}
			if !strings.Contains(stderr, text2imgUsage) {
				t.Errorf("`text2img --%s 0` stderr did not carry the usage page", flag)
			}
			if strings.Contains(stdout, text2imgE2ESummaryPrefix) {
				t.Errorf("`text2img --%s 0` refused but still rendered:\n%s", flag, stdout)
			}
		})
	}
}

// TestText2imgNonIntegerAndNegativeRefusedE2E keeps the pre-existing half of
// the contract pinned: "abc" and -5 were refused before the zero fix and must
// stay refused. The refusal names the flag and stays on stderr.
func TestText2imgNonIntegerAndNegativeRefusedE2E(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := text2imgE2ESandbox(t)
	in := text2imgE2EInput(t)

	for _, flag := range text2imgE2EFlagNames {
		for _, value := range text2imgE2EInvalidValues {
			stdout, stderr, code := text2imgE2EProbe(t, bin, home,
				toolnames.ToolText2img, in, "--"+flag, value)
			if code != 1 {
				t.Errorf("`text2img --%s %s` exit = %d, want 1 (stdout=%q stderr=%q)",
					flag, value, code, stdout, stderr)
			}
			want := "--" + flag + " " + text2imgPositiveIntMessage
			if !strings.Contains(stderr, want) {
				t.Errorf("`text2img --%s %s` stderr missing %q:\n%s", flag, value, want, stderr)
			}
		}
	}
}

// TestText2imgAbsentFlagStillRendersE2E pins the other face of the rule: a
// flag that is ABSENT is not a refusal — it takes the documented default, and
// the render summary says so. Without this half, "refuse everything" would
// pass the zero tests.
func TestText2imgAbsentFlagStillRendersE2E(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := text2imgE2ESandbox(t)
	in := text2imgE2EInput(t)

	// (a) no numeric flag at all → every default applies.
	stdout, stderr, code := text2imgE2EProbe(t, bin, home,
		toolnames.ToolText2img, in, filepath.Join(t.TempDir(), "absent.png"))
	if code != 0 {
		t.Fatalf("`text2img <in> <out>` exit = %d, want 0 (stderr=%q)", code, stderr)
	}
	for _, want := range []string{
		text2imgE2ESummaryPrefix,
		"字号 " + itoaForTest(text2img.DefaultSize) + "px",
		"折行 " + itoaForTest(text2img.DefaultCols),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("absent-flag render summary missing %q:\n%s", want, stdout)
		}
	}

	// (b) an explicit, LEGAL value is honoured — not rerouted to the default.
	stdout, stderr, code = text2imgE2EProbe(t, bin, home,
		toolnames.ToolText2img, in, filepath.Join(t.TempDir(), "explicit.png"), "--size", "28")
	if code != 0 {
		t.Fatalf("`text2img --size 28` exit = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "字号 28px") {
		t.Errorf("`--size 28` was not honoured — summary says:\n%s", stdout)
	}
}

// TestText2imgHelpContractHoldsForSpelledFormsE2E drives the two spellings of
// a flag GIVEN WITHOUT A VALUE through the real binary: the shared parser
// reports "" for both, and both must be refused. This is the case where the
// help sentence ("a flag that IS given must be a positive integer") and the
// help's own examples of the spelling disagree if the parser cannot tell
// absent from present — so the test drives exactly the argv the page teaches.
func TestText2imgHelpContractHoldsForSpelledFormsE2E(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := text2imgE2ESandbox(t)
	in := text2imgE2EInput(t)

	for _, args := range [][]string{
		{in, "--" + text2imgFlagSize},
		{in, "--" + text2imgFlagSize + "="},
		{in, "--" + text2imgFlagCols + "="},
	} {
		stdout, stderr, code := text2imgE2EProbe(t, bin, home, append([]string{toolnames.ToolText2img}, args...)...)
		if code != 1 {
			t.Errorf("`text2img %s`: exit = %d, want 1 (stdout=%q)", strings.Join(args, " "), code, stdout)
		}
		flag := text2imgFlagSize
		if strings.Contains(strings.Join(args, " "), "--"+text2imgFlagCols) {
			flag = text2imgFlagCols
		}
		if want := "--" + flag + " " + text2imgPositiveIntMessage; !strings.Contains(stderr, want) {
			t.Errorf("`text2img %s`: stderr missing %q:\n%s", strings.Join(args, " "), want, stderr)
		}
	}
}

// TestText2imgUsageStatesThePositiveContract pins that help and predicate are
// the SAME rule: the usage page the user reads must state the contract the
// dispatch walk enforces, and the REAL binary's own -h must print it (a
// constant nobody prints is not a help surface).
func TestText2imgUsageStatesThePositiveContract(t *testing.T) {
	if !strings.Contains(text2imgUsage, text2imgPositiveContractNote) {
		t.Errorf("text2imgUsage does not state the positive-integer contract (%q)", text2imgPositiveContractNote)
	}
	bin := gopiTestBinPath(t)
	home := text2imgE2ESandbox(t)
	stdout, stderr, code := text2imgE2EProbe(t, bin, home, toolnames.ToolText2img, HelpFlagLong)
	if code != 0 {
		t.Fatalf("`text2img -h` exit = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, text2imgPositiveContractNote) {
		t.Errorf("the binary's -h does not state the positive-integer contract:\n%s", stdout)
	}
}
