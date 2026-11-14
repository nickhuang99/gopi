package main

// daemon_spawn_profile_guard_test.go — the regression test for the daemon
// spawn's pre-fork profile guard (epic daemon-spawn-profile-guard, 2026-10-04).
//
// THE SHAPE THIS PINS: `gopi worker --daemon` with NO explicit GOPI_HOME. Before
// the guard, that spawn resolved config.GopiDir() from HOME, took the flock on
// <HOME>/.gopi/worker.lock, and detached — the measured 639670 shape, a resident
// daemon with ppid=1 holding the operator's real lock.
//
// TWO PROOFS, by design:
//
//	RED   TestDaemonSpawnRefusalRedProofOnThePreFixShape — the SAME
//	      predicate the production spawn calls, with the guard's own env switch
//	      ON (the pre-fix shape: nobody named the profile). It must report a
//	      REFUSAL. The switch is the test-only bypass the acceptance criterion
//	      names, so the red proof needs no edit to production code.
//	GREEN TestDaemonSpawnRefusalRealBinaryRefuses — the REAL binary, no
//	      GOPI_HOME, must refuse with the guard's exit code and leave NO
//	      worker.lock in the real profile and no daemon process behind.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestDaemonSpawnRefusalRedProofOnThePreFixShape drives the guard's own
// decision function with the PRE-FIX environment shape and asserts it REFUSES.
//
// "Pre-fix shape" here is precise: it is the exact input set the production
// reader passes, read from an environment that carries NO GOPI_HOME — i.e. the
// environment a spawner with os.Environ() and no explicit profile produces.
// The decision must say Refused, and it must name the real profile it would
// have used. Turning the guard OFF (its env switch) flips the verdict to
// allowed — that flip IS the red/green transition this case pins.
func TestDaemonSpawnRefusalRedProofOnThePreFixShape(t *testing.T) {
	userHome := "/home/somebody"

	// The pre-fix shape: no GOPI_HOME, no sandbox, guard ON (the default).
	refused := daemonProfileGuard(
		"", // guard switch absent ⇒ the guard runs
		"", // GOPI_HOME absent ⇒ the 639670 shape
		"", // no sandbox declared either
		userHome,
	)
	if !refused.Refused {
		t.Fatalf("the pre-fix shape (no GOPI_HOME) must be REFUSED, got allowed: %+v", refused)
	}
	wantDir := filepath.Join(userHome, config.ProfileDirName)
	if refused.ResolvedDir != wantDir {
		t.Errorf("the refusal must name the real profile it would have used: got %q, want %q", refused.ResolvedDir, wantDir)
	}

	// The guard's own switch is the red proof's bypass: the SAME inputs with
	// the switch on are ALLOWED (this is what makes the case a red proof rather
	// than a constant assertion — the predicate discriminates on exactly the
	// fact under test).
	bypassed := daemonProfileGuard("1", "", "", userHome)
	if bypassed.Refused {
		t.Errorf("with the guard switch on the pre-fix shape must be allowed (that is the pre-fix behaviour), got refused: %+v", bypassed)
	}

	// And a NAMED profile is allowed with the guard running — the other half of
	// "the guard does not refuse a legitimate spawn".
	named := daemonProfileGuard("", filepath.Join(userHome, "sandbox-abc"), "", userHome)
	if named.Refused {
		t.Errorf("a spawn that NAMES its profile must be allowed, got refused: %+v", named)
	}
}

// TestDaemonSpawnRefusalRealBinaryRefuses runs the REAL binary with the
// 639670 environment shape (no GOPI_HOME) and asserts:
//
//  1. it exits with the guard's refusal code (not 0, not 1);
//  2. it printed the refusal label and named GOPI_HOME as the correction;
//  3. it created NO worker.lock under the HOME it was given — the refusal
//     happened BEFORE the flock, which is the whole point of "pre-fork";
//  4. it left NO detached process behind.
//
// The HOME is redirected to an empty temp dir, so the "real profile" the guard
// compares against is that temp dir's — the case is hermetic and can never touch
// the operator's own profile even if it went wrong.
func TestDaemonSpawnRefusalRealBinaryRefuses(t *testing.T) {
	bin := wdBin(t)
	home := t.TempDir()

	// The child environment: the daemon face's flag, an agent-signature-free
	// env vector, NO GOPI_HOME and NO GOPI_SETTINGS — the shape the incident's
	// spawner produced. Everything else ambient is dropped so a variable this
	// case does not intend cannot supply the profile.
	env := stripAgentSignature([]string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
	})
	cmd := exec.Command(bin, CliVerbWorker, daemonFlagLong)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("run the real binary: %v\n%s", err, out)
		}
		code = ee.ExitCode()
	}
	if code == 0 {
		t.Fatalf("a daemon spawn with no GOPI_HOME must be refused, got exit 0:\n%s", out)
	}
	if code != daemonProfileRefusalExitCode {
		t.Errorf("the refusal must use its OWN exit code %d so a caller can tell it from a broken start; got %d:\n%s",
			daemonProfileRefusalExitCode, code, out)
	}
	for _, want := range []string{daemonProfileRefusalLabel, config.GopiDirEnv} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the refusal must name %q, got:\n%s", want, out)
		}
	}

	// 3. NO lock under the HOME it was given — the refusal is pre-fork and
	//    pre-flock.
	lock := filepath.Join(home, config.ProfileDirName, workerLockFilename)
	if _, err := os.Stat(lock); err == nil {
		t.Errorf("the refusal created %s — the guard must run BEFORE the control fd is acquired", lock)
	}
	// 4. No process marks this HOME as a daemon's.
	if pids := wdSandboxPIDs(home); len(pids) > 0 {
		t.Errorf("the refused spawn left %d daemon process(es) behind: %v", len(pids), pids)
	}
}

// TestDaemonSpawnRefusalAllowsANamedProfile is the compatibility half:
// a spawn that NAMES its profile must behave exactly as the running production
// binary does — the guard must not have narrowed the daemon face's normal
// operation. It is the smallest observable proof (the daemon reports its own
// start), and it reaps what it starts.
func TestDaemonSpawnRefusalAllowsANamedProfile(t *testing.T) {
	bin := wdBin(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wsDaemonEnv(t, home, settings)

	pid, _ := wsStartDaemon(t, bin, env)
	if pid <= 0 {
		t.Fatalf("a daemon with a NAMED profile must still start; got pid %d", pid)
	}
	// Prove the lock the daemon took is THIS profile's, not the operator's.
	lock := filepath.Join(home, config.ProfileDirName, workerLockFilename)
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the daemon must take ITS OWN profile's lock %s: %v", lock, err)
	}
	// wsStartDaemon registers its own reap (t.Cleanup → wdStopPID); the lock's
	// presence is the assertion — the daemon took THIS profile's lock.
	wdStopPID(t, pid)
}
