package shot

// units_test.go — the REAL-EXECUTION half of the capture-unit reclamation.
//
// The unit-level contract (which verdict keeps a unit) is pinned in
// internal/scratch's units_registry_test.go. What can only be checked HERE is
// that the two faces a host actually runs — `gopi tool scratch-gc` and the
// reclamation called from the resident tick's tick — really stop the systemd
// units of creators that died, really leave a live creator's unit alone, and
// really run the same teardown argv the normal path uses.
//
// These are not mocks: the registry is written by the PRODUCTION registrar, the
// creators are real killed processes, and the teardown is a real
// `systemctl --user stop` (the unit names are capture-shaped but were never
// started, so systemctl reports the unit as gone — the documented
// systemctlExitNoSuchUnit "already gone" outcome the teardown treats as
// success). The CLI face is exec'd as the real binary, so the wiring
// cmd/agent -> internal/shot -> internal/scratch is exercised end to end.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
)

// unitReclaimEnv gives the test its own registry (inside a private scratch
// home), so nothing here can touch the developer's real registrations or stop a
// real capture's chrome. The env shape mirrors cmd/agent's scratch-gc E2E helper:
// a sandbox home, a sandbox scratch home, and no ambient GOPI_SETTINGS.
func unitReclaimEnv(t *testing.T) (registry string, env []string) {
	t.Helper()
	home := t.TempDir()
	scratchHome := t.TempDir()
	// A per-test registry: two tests in this package must never see each
	// other's registrations (a leftover record of a killed helper would make
	// the next test's counts wrong — measured: one test read another's two).
	registry = filepath.Join(t.TempDir(), scratch.ShotUnitRegistryDirName)
	t.Setenv(scratch.ShotUnitRegistryEnvName, registry)
	env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GOPI_HOME=" + home,
		"GOPI_SETTINGS=" + filepath.Join(home, "settings.json"),
		scratch.HomeEnvName + "=" + scratchHome,
		scratch.ShotUnitRegistryEnvName + "=" + registry,
	}
	// The user BUS must be reachable for the child's `systemctl --user` (the
	// teardown the reclamation runs). A systemd timer's environment has these;
	// the Go test harness's does not, and without them systemctl fails with
	// "Failed to connect to bus" — the child would then report a stop failure
	// that says nothing about the code under test.
	env = append(env, userBusEnv()...)
	return registry, env
}

// userBusEnv passes through (or reconstructs) the two variables a user-bus
// client needs, so a sandboxed child can reach the same manager this test runs
// under.
func userBusEnv() []string {
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

// liveHelper starts a real process whose pid has a measurable identity.
func liveHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	// The helper's subject is its PID's measurable identity, never its cwd, and
	// it must not outlive a SIGKILLed binary (no cleanup runs on that path).
	cmd.Dir = t.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a helper process: %v", err)
	}
	return cmd
}

// TestReclaimOrphanedUnitsStopsDeadCreatorKeepsLive is the surface the resident
// tick calls: a real process registers two units (one of which then dies), the
// sweep runs, and the dead creator's unit is stopped while the live one is not.
func TestReclaimOrphanedUnitsStopsDeadCreatorKeepsLive(t *testing.T) {
	registry, _ := unitReclaimEnv(t)

	live := liveHelper(t)
	defer func() {
		_ = live.Process.Kill() //stylegate:ignore kills the fixture process the test is tearing down
		_ = live.Wait()         //stylegate:ignore reaps the fixture process the test expects to be reaped
	}()
	doomed := liveHelper(t)

	liveUnit := scratch.ShotUnitPrefix + "test-live-keep"
	deadUnit := scratch.ShotUnitPrefix + "test-dead-stop"
	if _, err := scratch.RegisterShotUnit(registry, liveUnit, live.Process.Pid); err != nil {
		t.Fatalf("register live unit: %v", err)
	}
	if _, err := scratch.RegisterShotUnit(registry, deadUnit, doomed.Process.Pid); err != nil {
		t.Fatalf("register doomed unit: %v", err)
	}
	if err := doomed.Process.Kill(); err != nil {
		t.Fatalf("kill the doomed creator: %v", err)
	}
	_ = doomed.Wait() //stylegate:ignore reaps the fixture process the test expects to die

	out, err := ReclaimOrphanedUnits(procname.DefaultProcRoot)
	if err != nil {
		t.Fatalf("ReclaimOrphanedUnits: %v", err)
	}
	if out.Renewed != 1 || len(out.RenewedUnits) != 1 || out.RenewedUnits[0] != deadUnit {
		t.Fatalf("stopped = %v, want exactly [%s]", out.RenewedUnits, deadUnit)
	}
	if out.Kept != 1 || out.KeptRecords[0].Unit != liveUnit || out.KeptRecords[0].Verdict != "creator-live" {
		t.Fatalf("kept = %+v, want the live creator's %s kept as creator-live", out.KeptRecords, liveUnit)
	}
	if _, err := os.Stat(filepath.Join(registry, liveUnit, scratch.ShotUnitRecordFileName)); err != nil {
		t.Fatalf("the live creator's registration is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(registry, deadUnit)); !os.IsNotExist(err) {
		t.Fatalf("the dead creator's registration survived (stat err = %v)", err)
	}
}

// TestReclaimOrphanedUnitsStopsARealSystemdUnit is the true end-to-end unit
// case: a REAL transient capture unit is started, its registry record is
// rewritten to name a creator that is dead, and the sweep stops the unit — the
// unit stops being active and its chrome descendants are gone.
//
// It skips (never fails) where no user manager answers, because the leak this
// fixes only exists on a host that has one (launch.go announces the degradation
// where it does not).
func TestReclaimOrphanedUnitsStopsARealSystemdUnit(t *testing.T) {
	if !userManagerAnswers(t) {
		t.Skip("no systemd --user manager on this host — the unit path does not exist here")
	}
	registry, _ := unitReclaimEnv(t)

	unit := scratch.ShotUnitPrefix + "test-live-unit-" + strings.ReplaceAll(t.Name(), "/", "-")
	// The unit runs a sleep as its "chrome": the point is the UNIT's lifetime,
	// and a sleep costs the host nothing (Rule 27: never generate load).
	start := exec.Command(systemdRunBin, "--user", "--unit="+unit, "--collect", "--quiet", "--", "sleep", "300")
	if out, err := start.CombinedOutput(); err != nil {
		t.Skipf("%s --user unavailable: %v (%s)", systemdRunBin, err, strings.TrimSpace(string(out)))
	}
	//stylegate:ignore stops a fixture user unit; the state read afterwards is the assertion
	t.Cleanup(func() { _ = stopUserUnitErr(unit) })
	if got := unitActiveState(t, unit); got != "active" {
		t.Fatalf("unit %s state = %q right after start, want active", unit, got)
	}

	doomed := liveHelper(t)
	if _, err := scratch.RegisterShotUnit(registry, unit, doomed.Process.Pid); err != nil {
		t.Fatalf("register the real unit: %v", err)
	}
	if err := doomed.Process.Kill(); err != nil {
		t.Fatalf("kill the creator: %v", err)
	}
	_ = doomed.Wait() //stylegate:ignore reaps the fixture process the test expects to die

	out, err := ReclaimOrphanedUnits(procname.DefaultProcRoot)
	if err != nil {
		t.Fatalf("ReclaimOrphanedUnits: %v", err)
	}
	if out.Renewed != 1 {
		t.Fatalf("outcome = %+v, want the orphaned real unit stopped", out)
	}
	if got := unitActiveState(t, unit); got == "active" {
		t.Fatalf("unit %s is STILL active after the sweep", unit)
	}
}

// TestScratchGCCLIFaceReclaimsCaptureUnits is the harness check the epic asks
// for: the `gopi tool scratch-gc` verb — the face a host watchdog runs — is what
// performs the reclamation in production, not just the library call.
// cliScratchGCArgv is the FAMILY address of the one-shot sweep: `scratch-gc`
// alone is a RETIRED flat word the binary REFUSES (measured: exit 1 with the
// refusal on stderr), so a test driving it as argv[0] asserts against the
// refusal instead of the sweep. The words come from their owners (internal/cliverbs
// and the dispatch-side constant) rather than from bare literals (Rules 1/13/14).
func cliScratchGCArgv(extra ...string) []string {
	out := []string{cliverbs.Tool, cliverbs.ScratchGC}
	return append(out, extra...)
}

const cliScratchGCDryRunFlag = "--dry-run"

func TestScratchGCCLIFaceReclaimsCaptureUnits(t *testing.T) {
	registry, env := unitReclaimEnv(t)
	doomed := liveHelper(t)
	unit := scratch.ShotUnitPrefix + "test-cli-dead"
	if _, err := scratch.RegisterShotUnit(registry, unit, doomed.Process.Pid); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := doomed.Process.Kill(); err != nil {
		t.Fatalf("kill the creator: %v", err)
	}
	_ = doomed.Wait() //stylegate:ignore reaps the fixture process the test expects to die

	stdout, stderr, code := testutil.RunGopi(t, cliScratchGCArgv(), "", env, "")
	if code != 0 {
		t.Fatalf("gopi tool scratch-gc exited %d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "shot-unit-reclaim") || !strings.Contains(stdout, "renewed=1") {
		t.Fatalf("the CLI face did not report the reclamation:\n%s\n(stderr:\n%s)", stdout, stderr)
	}
	if !strings.Contains(stdout, "renewed-unit="+unit) {
		t.Fatalf("the CLI report does not name the reclaimed unit %s:\n%s", unit, stdout)
	}
	if _, err := os.Stat(filepath.Join(registry, unit)); !os.IsNotExist(err) {
		t.Fatalf("the registration survived the CLI sweep (stat err = %v)", err)
	}
}

// TestScratchGCDryRunReportsTheCapturePlan: the vetting face must show an
// operator what the sweep WOULD stop, and must stop nothing.
func TestScratchGCDryRunReportsTheCapturePlan(t *testing.T) {
	registry, env := unitReclaimEnv(t)
	doomed := liveHelper(t)
	unit := scratch.ShotUnitPrefix + "test-plan-dead"
	if _, err := scratch.RegisterShotUnit(registry, unit, doomed.Process.Pid); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := doomed.Process.Kill(); err != nil {
		t.Fatalf("kill the creator: %v", err)
	}
	_ = doomed.Wait() //stylegate:ignore reaps the fixture process the test expects to die

	stdout, stderr, code := testutil.RunGopi(t, cliScratchGCArgv(cliScratchGCDryRunFlag), "", env, "")
	if code != 0 {
		t.Fatalf("gopi tool scratch-gc --dry-run exited %d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "would-stop "+unit) {
		t.Fatalf("the dry-run report does not promise the stop of %s:\n%s", unit, stdout)
	}
	if _, err := os.Stat(filepath.Join(registry, unit, scratch.ShotUnitRecordFileName)); err != nil {
		t.Fatalf("the dry-run changed the registry: %v", err)
	}
}

// cliShotArgv is the FAMILY address of the capture the two trigger tests drive:
// `shot` ALONE is a RETIRED flat word the binary REFUSES (measured 2026-10-16:
// exit 1, "`shot` is RETIRED — use `gopi tool shot`"), so a test handing it to
// the binary as argv[0] asserts against the REFUSAL instead of against the
// start-up sweep — and reads the missing `shot-unit-reclaim` line as a missing
// feature. This is the same shape (and the same repair) as cliScratchGCArgv
// above, whose flat spelling was refused for the same reason.
//
// The words come from their owners — internal/cliverbs, which cmd/agent's
// CliVerbTool/CliVerbShot alias — rather than from bare literals (Rules 1/13/14):
// internal/shot cannot import cmd/agent (the direction of the dependency), so
// the leaf package's constants are the reachable single source.
func cliShotArgv(extra ...string) []string {
	out := []string{cliverbs.Tool, cliverbs.Shot}
	return append(out, extra...)
}

// deadCreatorE2E prepares one REAL orphaned transient unit — a unit actually
// started with systemd-run whose registered creator is then killed — and returns
// its setup and teardown. It is the fixture both trigger E2Es use, so the two
// cannot diverge in what "orphaned" means.
func deadCreatorE2E(t *testing.T, registry, label string) (unit string, cleanup func()) {
	t.Helper()
	unit = scratch.ShotUnitPrefix + "test-trigger-" + label
	// A `sleep` stands in for chrome: the point is the UNIT's lifetime, and a
	// sleep costs the host nothing (Rule 27: never generate load).
	if out, err := exec.Command(systemdRunBin, "--user", "--unit="+unit, "--collect", "--quiet", "--", "sleep", "300").CombinedOutput(); err != nil {
		t.Skipf("%s --user unavailable: %v (%s)", systemdRunBin, err, strings.TrimSpace(string(out)))
	}
	//stylegate:ignore stops a fixture user unit; the state read afterwards is the assertion
	cleanup = func() { _ = stopUserUnitErr(unit) }
	creator := liveHelper(t)
	if _, err := scratch.RegisterShotUnit(registry, unit, creator.Process.Pid); err != nil {
		t.Fatalf("register %s: %v", unit, err)
	}
	if err := creator.Process.Kill(); err != nil {
		t.Fatalf("kill the creator: %v", err)
	}
	_ = creator.Wait() //stylegate:ignore reaps the fixture process the test spawned
	return unit, cleanup
}

// TestShotStartupTriggerRenewsAnOrphanedUnit is the epic's SECOND trigger, GREEN
// half: a REAL orphaned unit (leaked by killing its creator, exactly the way the
// measured leak happened) is reclaimed by a REAL `gopi tool shot` invocation — the
// `--self` form whose render is expected to fail in the sandbox, so the unit's
// disappearance is attributable to the START-UP sweep and not to a successful
// capture.
//
// RED on the pre-change tree: `gopi tool shot` did not sweep at all, so the unit was
// still active after the same invocation (the acceptance's RED measurement).
func TestShotStartupTriggerRenewsAnOrphanedUnit(t *testing.T) {
	if !userManagerAnswers(t) {
		t.Skip("no systemd --user manager on this host — the unit path does not exist here")
	}
	registry, env := unitReclaimEnv(t)
	unit, cleanup := deadCreatorE2E(t, registry, "shot-startup")
	defer cleanup()
	if got := unitActiveState(t, unit); got != "active" {
		t.Fatalf("the leaked unit %s is %q before the trigger, want active (the leak was not reproduced)", unit, got)
	}

	start := time.Now()
	// --self with an unreachable address: the reachability guard answers before
	// the capture, so this exercises start-up WITHOUT starting a browser
	// (Rule 27's spirit — no render, no load) and still goes through the same
	// start-up path as a real capture.
	stdout, stderr, code := testutil.RunGopi(t, cliShotArgv(
		"--self", "--addr", "127.0.0.1:1", "--out", filepath.Join(t.TempDir(), "x.png"),
	), "", env, "")
	elapsed := time.Since(start)

	if !strings.Contains(stdout, "shot-unit-reclaim") ||
		!strings.Contains(stdout, "renewed-unit="+unit) {
		t.Fatalf("the `gopi tool shot` start-up trigger did not report renewing %s (exit=%d, elapsed=%s)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			unit, code, elapsed, stdout, stderr)
	}
	if got := unitActiveState(t, unit); got == "active" {
		t.Fatalf("unit %s is STILL active after `gopi tool shot` ran (elapsed=%s)", unit, elapsed)
	}
	if _, err := os.Stat(filepath.Join(registry, unit)); !os.IsNotExist(err) {
		t.Fatalf("the registration of %s survived the start-up trigger (stat err = %v)", unit, err)
	}
	t.Logf("shot start-up trigger: %s renewed, unit %s, elapsed %s (shot exit=%d)", unit, "inactive", elapsed, code)
}

// TestShotStartupTriggerKeepsALiveCreatorsUnit is the POSITIVE CONTROL for the
// shot trigger: a unit whose creating process is STILL ALIVE must survive the
// start-up sweep. A sweep that also kills live captures would be worse than the
// leak, so this is the direction that must never regress.
func TestShotStartupTriggerKeepsALiveCreatorsUnit(t *testing.T) {
	if !userManagerAnswers(t) {
		t.Skip("no systemd --user manager on this host — the unit path does not exist here")
	}
	registry, env := unitReclaimEnv(t)
	unit := scratch.ShotUnitPrefix + "test-trigger-live"
	if out, err := exec.Command(systemdRunBin, "--user", "--unit="+unit, "--collect", "--quiet", "--", "sleep", "300").CombinedOutput(); err != nil {
		t.Skipf("%s --user unavailable: %v (%s)", systemdRunBin, err, strings.TrimSpace(string(out)))
	}
	//stylegate:ignore stops a fixture user unit; the state read afterwards is the assertion
	defer func() { _ = stopUserUnitErr(unit) }()
	// A REAL live creator: this test process itself, so the identity triple is
	// measurable and LIVE (the sweep must refuse it on the verdict, not by luck).
	if _, err := scratch.RegisterShotUnit(registry, unit, os.Getpid()); err != nil {
		t.Fatalf("register %s: %v", unit, err)
	}

	stdout, stderr, code := testutil.RunGopi(t, cliShotArgv(
		"--self", "--addr", "127.0.0.1:1", "--out", filepath.Join(t.TempDir(), "x.png"),
	), "", env, "")

	if got := unitActiveState(t, unit); got != "active" {
		t.Fatalf("the LIVE creator's unit %s is %q after the start-up trigger, want active\n--- stdout ---\n%s\n--- stderr ---\n%s",
			unit, got, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(registry, unit, scratch.ShotUnitRecordFileName)); err != nil {
		t.Fatalf("the live creator's registration was removed: %v", err)
	}
	if strings.Contains(stdout, "renewed-unit="+unit) {
		t.Fatalf("the report claims the live creator's unit was renewed:\n%s", stdout)
	}
	_ = code
}

// userManagerAnswers reports whether a systemd --user manager is reachable —
// the precondition of the unit path (launch.go degrades loudly without one).
func userManagerAnswers(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath(systemctlBin); err != nil {
		return false
	}
	return exec.Command(systemctlBin, "--user", "is-system-running").Run() != nil ||
		exec.Command(systemctlBin, "--user", "list-units", "--no-legend", "--no-pager").Run() == nil
}

// unitActiveState reads one unit's ActiveState ("inactive" for a unit that does
// not exist — the state an orphan that was never started reports after a stop).
func unitActiveState(t *testing.T, unit string) string {
	t.Helper()
	out, err := exec.Command(systemctlBin, "--user", "is-active", unit).Output()
	state := strings.TrimSpace(string(out))
	if state == "" && err != nil {
		return "unknown"
	}
	return state
}
