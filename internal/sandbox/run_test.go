package sandbox

// run_test.go — the EXECUTION contract of run.go.
//
// Rule 31's spirit applied one level down: each test drives the REAL seam with a
// REAL child process, and asserts PHYSICAL truth — the child reads
// /proc/self/cwd, which is the kernel's answer about where the child actually
// ran, not the child's opinion of it (a shell's `pwd` is a builtin reading $PWD,
// and $PWD is inherited environment, not a measurement).
//
// The regression this file guards (the epic's whole point): before run.go, the
// only way to run something "inside" a sandbox was to launch it by absolute path
// from wherever the launcher stood — the child inherited the CALLER's directory
// (inside a task run: the run's git worktree), and an orphan left there made
// target worktree unreclaimable forever. So the two assertions that matter are
// (1) the child's cwd IS the sandbox and (2) the LAUNCHER's cwd did not move.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
)

// runFixture materialises a real sandbox under the suite's pinned scratch tree
// (Rule 22) and returns it plus a directory for the child's evidence files —
// OUTSIDE the sandbox, so the sandbox's shape stays exactly the standard one.
func runFixture(t *testing.T) (*Sandbox, string) {
	t.Helper()
	root := testutil.TestScratchDir(t, "sandbox-run")
	evidence := testutil.TestScratchDir(t, "sandbox-run-evidence")
	sb, err := Create(root, "", Options{
		Label:               "runcwd",
		TaskID:              "01a0demo-run0-7000-8000-00000000demo",
		ProfileSettingsPath: settingsFile(t, root),
		LedgerSeedPath:      testutil.SampleDBPath(t),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return sb, evidence
}

// childProbeArgv is the child that reports the three facts this contract is
// about: its PHYSICAL cwd (/proc/self/cwd), the shell's own view (pwd) and the
// profile it was handed. Written as one `sh -c` so the test needs no fixture
// file on disk; $1..$3 are the evidence paths.
const childProbeArgv = `readlink -f /proc/self/cwd > "$1"; pwd > "$2"; printf '%s' "$GOPI_HOME" > "$3"`

// readEvidence reads one evidence file the child wrote, failing loudly when the
// child did not write it (a missing file is the child's failure, never a pass).
func readEvidence(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the child did not write %s: %v", path, err)
	}
	return strings.TrimSpace(string(b))
}

// TestRunInChildRunsInTheSandboxAndTheLauncherDoesNotMove is the regression
// guard for the defect the epic names, A/B'd on the SAME seam:
//
//   - the child's /proc/self/cwd IS the sandbox directory (the fix);
//   - the child's GOPI_HOME is the sandbox (the private profile travels);
//   - the LAUNCHER's own cwd is byte-identical before and after the exec — the
//     rule is "children get cmd.Dir", never "gopi chdir's" (a chdir would move
//     every other thing the launcher does for the rest of its life).
func TestRunInChildRunsInTheSandboxAndTheLauncherDoesNotMove(t *testing.T) {
	sb, evidence := runFixture(t)
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd before: %v", err)
	}
	outCwd := filepath.Join(evidence, "cwd.txt")
	outPwd := filepath.Join(evidence, "pwd.txt")
	outHome := filepath.Join(evidence, "home.txt")

	err = RunIn(RunSpec{
		Dir: sb.Path,
		Argv: []string{"sh", "-c", childProbeArgv,
			"sh", outCwd, outPwd, outHome},
		Env: ProfileEnv(os.Environ(), sb.Path),
	})
	if err != nil {
		t.Fatalf("RunIn: %v", err)
	}

	// The sandbox path is canonical: Create resolved it through
	// scratch.CanonicalPath, and /proc/self/cwd reports the resolved directory.
	want := scratch.CanonicalPath(sb.Path)
	if got := readEvidence(t, outCwd); got != want {
		t.Errorf("the child's /proc/self/cwd is %q, want the sandbox %q — the exec seam did not give the child its own directory", got, want)
	}
	if got := readEvidence(t, outHome); got != want {
		t.Errorf("the child's GOPI_HOME is %q, want the sandbox %q — the private profile did not travel", got, want)
	}

	after, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after: %v", err)
	}
	if after != before {
		t.Errorf("the LAUNCHER's cwd moved: %q → %q — the child's directory must be per-exec (cmd.Dir), never a chdir of gopi itself", before, after)
	}
	if after == want {
		t.Errorf("the launcher's cwd is the sandbox %q — RunIn chdir'd the parent instead of setting the child's directory", want)
	}
}

// TestRunInPropagatesTheChildsExitStatus pins the propagation contract: a child
// that RAN and failed hands its own status back, so a script wrapping the
// sandbox sees the child's code (the only thing it can branch on).
func TestRunInPropagatesTheChildsExitStatus(t *testing.T) {
	sb, _ := runFixture(t)
	err := RunIn(RunSpec{Dir: sb.Path, Argv: []string{"sh", "-c", "exit 7"}})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("a child with exit status 7 must produce *ExitError, got %v", err)
	}
	if exitErr.ExitCode != 7 {
		t.Errorf("ExitError.ExitCode = %d, want the child's own 7", exitErr.ExitCode)
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("the error text must carry the status: %q", err.Error())
	}
}

// TestRunInReportsASignalDeathAsTheShellConvention pins the second exit shape: a
// child killed by a signal reports 128+N, the status a script would have seen
// from the shell — not a fabricated plain "1".
func TestRunInReportsASignalDeathAsTheShellConvention(t *testing.T) {
	sb, _ := runFixture(t)
	// The child kills ITSELF with SIGTERM (15) → 128+15. Asking the child to do
	// it keeps the test independent of signal delivery timing from outside.
	err := RunIn(RunSpec{Dir: sb.Path, Argv: []string{"sh", "-c", "kill -TERM $$; sleep 5"}})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("a signalled child must produce *ExitError, got %v", err)
	}
	if want := exitedWithSignal + int(termSignal); exitErr.ExitCode != want {
		t.Errorf("ExitError.ExitCode = %d, want %d (128+SIGTERM)", exitErr.ExitCode, want)
	}
}

// TestRunInRefusesWhatCannotRun is the malformed-argv half: nothing to run, no
// directory to run in, and a directory that is not one. Each is REFUSED with a
// message, and none of them starts a process — a refusal that silently ran
// something in the wrong directory is the defect this seam exists to prevent.
func TestRunInRefusesWhatCannotRun(t *testing.T) {
	sb, evidence := runFixture(t)
	notADir := filepath.Join(evidence, "regular-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write the regular file: %v", err)
	}
	for _, tc := range []struct {
		name string
		spec RunSpec
	}{
		{"empty argv", RunSpec{Dir: sb.Path}},
		{"no directory", RunSpec{Argv: []string{"true"}}},
		{"missing directory", RunSpec{Dir: filepath.Join(evidence, "does-not-exist"), Argv: []string{"true"}}},
		{"not a directory", RunSpec{Dir: notADir, Argv: []string{"true"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RunIn(tc.spec)
			if err == nil {
				t.Fatalf("RunIn(%s) must refuse", tc.name)
			}
			// A refusal is not a child failure: there is no status to report,
			// and inventing one would be a lie (Rule 15a).
			var exitErr *ExitError
			if errors.As(err, &exitErr) {
				t.Errorf("a refused exec must not report a child exit status, got %v", err)
			}
		})
	}
}

// TestProfileEnvOverridesAnInheritedProfile pins the environment contract: the
// sandbox's path WINS over whatever the launcher inherited (a child of a sandbox
// must not silently read the caller's profile), and nothing else is touched.
func TestProfileEnvOverridesAnInheritedProfile(t *testing.T) {
	sb, _ := runFixture(t)
	inherited := []string{
		"PATH=/usr/bin",
		config.GopiDirEnv + "=/home/someone/.gopi",
		config.SettingsEnv + "=/home/someone/.gopi/settings.json",
	}
	got := ProfileEnv(inherited, sb.Path)

	counts := map[string]int{}
	for _, kv := range got {
		key := strings.SplitN(kv, "=", 2)[0]
		counts[key]++
	}
	for _, key := range []string{config.GopiDirEnv, config.SettingsEnv} {
		if counts[key] != 1 {
			t.Errorf("%s appears %d times in the child env, want exactly 1: %v", key, counts[key], got)
		}
	}
	wantHome := config.GopiDirEnv + "=" + scratch.CanonicalPath(sb.Path)
	wantSettings := config.SettingsEnv + "=" + SettingsFileNameIn(sb.Path)
	if !contains(got, wantHome) {
		t.Errorf("the child env must carry %q, got %v", wantHome, got)
	}
	if !contains(got, wantSettings) {
		t.Errorf("the child env must carry %q, got %v", wantSettings, got)
	}
	if !contains(got, "PATH=/usr/bin") {
		t.Errorf("ProfileEnv must leave unrelated variables alone, got %v", got)
	}
	// The launcher's OWN environment is not modified — ProfileEnv is a pure
	// function, or the child's profile would leak back into gopi.
	if os.Getenv(config.GopiDirEnv) == sb.Path {
		t.Errorf("ProfileEnv must not mutate the launcher's environment")
	}
}

// termSignal is SIGTERM's number, named (Rule 1a) because the assertion above
// compares against it.
const termSignal = 15

// contains reports whether env carries exactly this KEY=VALUE entry.
func contains(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// TestRunInChildCwdIsSeenByAnOutsideReader closes the loop for the reclaim
// guard: the fact finish-cleanup reads is /proc/<pid>/cwd, so this test reads it
// the way the guard does — from OUTSIDE, on a LIVE pid, while a real long-lived
// child (whose launcher-shape is the same cmd.Dir the seam sets) stands in the
// sandbox.
func TestRunInChildCwdIsSeenByAnOutsideReader(t *testing.T) {
	sb, _ := runFixture(t)
	cmd := exec.Command("sh", "-c", "sleep 300")
	cmd.Dir = sb.Path
	mp := testutil.StartMockProcessCmd(t, cmd)
	pid := mp.Pid()
	want := scratch.CanonicalPath(sb.Path)

	// procfs liveness is not instantaneous after Start: poll briefly, the same
	// way the guard's own fixtures do.
	deadline := time.Now().Add(5 * time.Second)
	got := ""
	for time.Now().Before(deadline) {
		if link, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil {
			got = strings.TrimSuffix(link, " (deleted)")
			break
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(20 * time.Millisecond)
	}
	if got == "" {
		t.Fatalf("could not read /proc/%d/cwd — the outside-reader observation failed", pid)
	}
	if scratch.CanonicalPath(got) != want {
		t.Errorf("an outside reader sees the child's cwd as %q, want the sandbox %q", got, want)
	}
}
