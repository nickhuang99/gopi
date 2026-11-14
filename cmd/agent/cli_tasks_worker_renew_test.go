package main

// cli_tasks_worker_renew_test.go — the `gopi worker` START-UP trigger of the
// shot-unit reclamation (epic shot-reclaim-trigger), driven through the REAL
// binary.
//
// The defect this closes: the reclamation existed and was correct, but its only
// in-process carrier was the worker/merger TICK — reached only while a round is
// in flight, and a worker self-exits after a drained queue (~90s), so on the
// quiet host where the leak accumulates nothing ever reached the sweep
// (measured 2026-09-27: six ACTIVE gopi-shot-* units, oldest ~1h50m). The fix
// rides an event that necessarily happens: the worker's START.
//
// These are not mocks: the orphaned unit is a REAL transient systemd unit, its
// creator is a REAL killed process, and the trigger is a REAL `gopi worker`
// invocation (no --once: the start-up pass belongs to the LOOP, which is the
// face a host runs; --once is the unit/E2E seam and deliberately skips it, so
// the decoy row in TestWorkerOnceFaceKeepsTheStartupDecoy pins that boundary).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/shot"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// renewTestUnitSleepSeconds is the stand-in "chrome" of a fixture unit: the
	// unit's LIFETIME is what is under test, and an idle sleep costs the host
	// nothing (Rule 27: never generate load to satisfy a test).
	renewTestUnitSleepSeconds = "300"

	// renewTestWorkerIdleSec / renewTestWorkerMaxRounds bound the fixture worker
	// run to a couple of seconds: it must reach its start-up pass and then exit
	// without waiting out the production idle cadence.
	renewTestWorkerIdleSec   = "1"
	renewTestWorkerMaxRounds = "1"

	// renewTestMockURL is the mocked provider endpoint MockEnv needs (no row is
	// claimed in this fixture, so no request is ever made).
	renewTestMockURL = "http://127.0.0.1:1"
)

// renewChildDir is the working directory every fixture worker is launched from,
// and it is HALF of this file's repo-context pin (the other half is
// SandboxRepoContext, called by each spawning test).
//
// WHY THE CHILD'S CWD MATTERS (repo_context_sandbox_test.go): a real
// `gopi worker` resolves the repository its own-worktree-release pass acts on
// partly from ITS OWN working directory, so a child launched with the inherited
// cwd stands in the developer's live checkout, enumerates the worktrees
// registered there and can free every clean, unowned one past the age floor.
// Handing it a directory of its own closes that half; the directory is fresh
// per call, under the ambient temp root this suite measured off the `/`
// partition (Rule 22).
func renewChildDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// renewTestEnv builds the sandboxed environment for a real worker run: a private
// GOPI_HOME, ledger, settings and shot-unit REGISTRY, so nothing here can touch
// the operator's registrations or stop a real capture's chrome.
func renewTestEnv(t *testing.T) (registry string, env []string) {
	t.Helper()
	gopiHome := t.TempDir()
	registry = filepath.Join(t.TempDir(), scratch.ShotUnitRegistryDirName)
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"sync_peers":[],"repos":{}}`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	env = testutil.MockEnv(t, gopiHome, renewTestMockURL)
	env = append(env,
		memoryDBEnv+"="+filepath.Join(t.TempDir(), "memory.db"),
		"GOPI_SETTINGS="+settingsPath,
		scratch.HomeEnvName+"="+t.TempDir(),
		scratch.ShotUnitRegistryEnvName+"="+registry,
		workerMaxRoundsEnv+"="+renewTestWorkerMaxRounds,
		workerIdleIntervalEnv+"="+renewTestWorkerIdleSec,
	)
	// The systemd USER bus must be reachable for the child's `systemctl --user`
	// teardown. A systemd-scheduled run has these; the Go test harness does not,
	// and without them systemctl answers "Failed to connect to bus", which would
	// make the child report a denial that says nothing about the code under test.
	return registry, append(env, renewUserBusEnv()...)
}

// renewUserBusEnv passes through (or reconstructs) the two variables a
// user-bus client needs.
func renewUserBusEnv() []string {
	uid := strconv.Itoa(os.Getuid())
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/run/user/" + uid
	}
	bus := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if bus == "" {
		bus = "unix:path=" + runtimeDir + "/bus"
	}
	return []string{"XDG_RUNTIME_DIR=" + runtimeDir, "DBUS_SESSION_BUS_ADDRESS=" + bus}
}

// renewSystemdUserAvailable reports whether a user manager answers — the
// precondition of the unit path.
func renewSystemdUserAvailable() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exec.Command("systemctl", "--user", "list-units", "--no-legend", "--no-pager").Run() == nil
}

// renewUnitState reads a unit's ActiveState ("inactive" when it does not exist).
func renewUnitState(t *testing.T, unit string) string {
	t.Helper()
	out, err := exec.Command("systemctl", "--user", "is-active", unit).Output()
	state := strings.TrimSpace(string(out))
	if state == "" && err != nil {
		return "unknown"
	}
	return state
}

// renewStartUnit starts a REAL transient user unit standing in for a capture.
func renewStartUnit(t *testing.T, unit string) {
	t.Helper()
	if out, err := exec.Command("systemd-run", "--user", "--unit="+unit, "--collect", "--quiet",
		"--", "sleep", renewTestUnitSleepSeconds).CombinedOutput(); err != nil {
		t.Skipf("systemd-run --user unavailable: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() {
		_ = exec.Command("systemctl", "--user", "stop", unit).Run() //stylegate:ignore stops the fixture user unit; the state read afterwards is the assertion
	})
}

// renewLeak reproduces the measured leak in three steps, in the order its
// mechanism requires: a REAL transient unit is started, the creating process is
// registered while it is still ALIVE (the registrar reads the creator's
// incarnation at that moment — a dead pid has none), and only then is that
// creator killed, which is exactly what a SIGKILLed / timed-out capture leaves
// behind (its teardown defer never runs).
func renewLeak(t *testing.T, registry, unit string) {
	t.Helper()
	renewStartUnit(t, unit)
	victim := exec.Command("sleep", renewTestUnitSleepSeconds)
	// Pdeathsig + a neutral cwd: the victim's PID is the subject, its location is
	// not, and a killed binary runs no cleanup to reap it.
	victim.Dir = t.TempDir()
	victim.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := victim.Start(); err != nil {
		t.Skipf("cannot start a helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = victim.Process.Kill() //stylegate:ignore kills the fixture victim the test spawned
		_ = victim.Wait()         //stylegate:ignore reaps the fixture victim the test spawned
	})
	if _, err := scratch.RegisterShotUnit(registry, unit, victim.Process.Pid); err != nil {
		t.Fatalf("register %s: %v", unit, err)
	}
	if err := victim.Process.Kill(); err != nil {
		t.Fatalf("kill the creator: %v", err)
	}
	_ = victim.Wait() //stylegate:ignore reaps the fixture victim the test spawned
}

// TestWorkerStartupTriggerRenewsAnOrphanedUnit is the epic's second wired
// trigger, GREEN half: a unit leaked exactly the way the measured six were (a
// REAL unit whose registered creator was killed) is reclaimed by a REAL
// `gopi worker` run — with no manual call and no other trigger in play.
func TestWorkerStartupTriggerRenewsAnOrphanedUnit(t *testing.T) {
	if !renewSystemdUserAvailable() {
		t.Skip("no systemd --user manager on this host — the unit path does not exist here")
	}
	SandboxRepoContext(t)
	registry, env := renewTestEnv(t)
	childDir := renewChildDir(t)
	unit := scratch.ShotUnitPrefix + "test-worker-startup"
	renewLeak(t, registry, unit)
	if got := renewUnitState(t, unit); got != "active" {
		t.Fatalf("the leaked unit %s is %q before the trigger, want active (leak not reproduced)", unit, got)
	}

	stdout, stderr, code := runGopiMapped(t, []string{CliVerbWorker}, "", env, childDir)

	if !strings.Contains(stdout, "shot-unit-reclaim") || !strings.Contains(stdout, "role="+shot.RenewRoleWorkerStartup) {
		t.Fatalf("the `gopi worker` start-up trigger did not report (exit=%d)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stdout, "renewed-unit="+unit) {
		t.Fatalf("the worker start-up trigger did not name %s as renewed\n--- stdout ---\n%s\n--- stderr ---\n%s",
			unit, stdout, stderr)
	}
	if got := renewUnitState(t, unit); got == "active" {
		t.Fatalf("unit %s is STILL active after `gopi worker` ran\n--- stdout ---\n%s", unit, stdout)
	}
	if _, err := os.Stat(filepath.Join(registry, unit)); !os.IsNotExist(err) {
		t.Fatalf("the registration of %s survived the trigger (stat err = %v)", unit, err)
	}
}

// TestWorkerStartupTriggerKeepsALiveCreatorsUnit is the POSITIVE CONTROL: a unit
// whose creating process is STILL ALIVE must survive the worker's start-up pass.
// A sweep that killed live captures would be worse than the leak.
func TestWorkerStartupTriggerKeepsALiveCreatorsUnit(t *testing.T) {
	if !renewSystemdUserAvailable() {
		t.Skip("no systemd --user manager on this host — the unit path does not exist here")
	}
	SandboxRepoContext(t)
	registry, env := renewTestEnv(t)
	childDir := renewChildDir(t)
	unit := scratch.ShotUnitPrefix + "test-worker-live"
	renewStartUnit(t, unit)
	// A REAL live creator with a measurable incarnation: this test process.
	if _, err := scratch.RegisterShotUnit(registry, unit, os.Getpid()); err != nil {
		t.Fatalf("register %s: %v", unit, err)
	}

	stdout, stderr, code := runGopiMapped(t, []string{CliVerbWorker}, "", env, childDir)

	if got := renewUnitState(t, unit); got != "active" {
		t.Fatalf("the LIVE creator's unit %s is %q after the trigger, want active (exit=%d)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			unit, got, code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(registry, unit, scratch.ShotUnitRecordFileName)); err != nil {
		t.Fatalf("the live creator's registration was removed: %v", err)
	}
	if strings.Contains(stdout, "renewed-unit="+unit) {
		t.Fatalf("the report claims the live creator's unit was renewed:\n%s", stdout)
	}
}

// TestWorkerStartupTriggerNamesTheCaptureCountOnTheCLILine pins the epic's 4th
// acceptance clause at the face an operator actually runs: a real
// `gopi tool scratch-gc` reports the capture arm's count on its OWN line, separately
// from the scratch entries it removed — the defect being that six stopped units
// were once reported as `removed=4` of queue directories.
func TestWorkerStartupTriggerNamesTheCaptureCountOnTheCLILine(t *testing.T) {
	if !renewSystemdUserAvailable() {
		t.Skip("no systemd --user manager on this host — the unit path does not exist here")
	}
	registry, env := renewTestEnv(t)
	unit := scratch.ShotUnitPrefix + "test-cli-count"
	renewLeak(t, registry, unit)

	stdout, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", env, "")
	if code != 0 {
		t.Fatalf("gopi tool scratch-gc exited %d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "shot-units-renewed=1") {
		t.Fatalf("scratch-gc does not carry the capture arm's own count line (want shot-units-renewed=1):\n%s", stdout)
	}
	if got := renewUnitState(t, unit); got == "active" {
		t.Fatalf("unit %s is STILL active after gopi tool scratch-gc ran:\n%s", unit, stdout)
	}
}

// TestWorkerOnceFaceKeepsTheStartupBoundary pins WHERE the start-up trigger is
// wired, deliberately and visibly: `gopi worker --once` does NOT sweep, because
// --once is the hermetic unit/E2E seam (Rule 6 — a test worker must not touch
// real host state) and because the resident worker's existing idle tests drive
// the loop with --once, so sweeping there would silently kill their decoy
// fixture units.
//
// The LOOP is the face a host runs (`gopi worker`, foreground or --daemon), and
// TestWorkerStartupTriggerRenewsAnOrphanedUnit proves it sweeps. This test
// exists so an operator reading "the worker sweeps at start-up" knows which
// argv it means, and so the boundary cannot be moved by accident.
func TestWorkerOnceFaceKeepsTheStartupBoundary(t *testing.T) {
	if !renewSystemdUserAvailable() {
		t.Skip("no systemd --user manager on this host — the unit path does not exist here")
	}
	SandboxRepoContext(t)
	registry, env := renewTestEnv(t)
	childDir := renewChildDir(t)
	unit := scratch.ShotUnitPrefix + "test-worker-once"
	renewLeak(t, registry, unit)

	stdout, stderr, code := runGopiMapped(t, []string{CliVerbWorker, workerFlagOnce}, "", env, childDir)
	if code != 0 {
		t.Fatalf("gopi worker --once exited %d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if got := renewUnitState(t, unit); got != "active" {
		t.Fatalf("`gopi worker --once` swept the host (unit %s is %q) — the seam face must stay side-effect free", unit, got)
	}
	if strings.Contains(stdout, "shot-unit-reclaim") {
		t.Fatalf("the --once face printed a reclamation report:\n%s", stdout)
	}
}
