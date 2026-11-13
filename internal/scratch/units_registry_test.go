package scratch

// units_registry_test.go — the capture-unit registry's contract: a unit whose
// creating shot process is PROVABLY gone is stopped; a live creator's unit is
// untouched; an unmeasurable creator keeps its entry; a normal stop deregisters;
// and the registry itself is never collected by the scratch-home sweep.
//
// The rule under test is the tree's own conservative rule (gc.go's
// "cannot measure ⇒ keep"), applied to a NEW kind of entry. Every "kept" case is
// therefore as important as every "stopped" one: reading an unmeasurable creator
// as a death stops a LIVE capture's chrome.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/procname"
)

// writeShotUnitRecord writes a record the way the production registrar writes
// it, so a fixture cannot drift from the on-disk shape.
func writeShotUnitRecord(dir string, rec ShotUnitRecord) error {
	raw, err := json.MarshalIndent(rec, "", shotUnitRecordJSONIndent)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, rec.Unit, ShotUnitRecordFileName),
		append(raw, '\n'), shotUnitRecordPerm)
}

// unitFixtureDir gives one test its own registry directory, outside the real
// scratch tree (Rule 32's spirit: a test never resolves the operator's tree).
func unitFixtureDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// shotUnitHelperHoldSeconds is how long the helper process holds: long enough to
// outlive any assertion in these cases, so a case can never pass because the
// helper happened to exit first.
const shotUnitHelperHoldSeconds = "30"

// fakeUnitName mints a name in the capture namespace without a systemd call —
// the sweep's predicate is what these tests exercise, not systemctl.
func fakeUnitName(suffix string) string { return ShotUnitPrefix + "test-" + suffix }

// shotUnitHelperProcess is THE ONE spawn of the "helper process whose pid has a
// measurable incarnation" these tests need. Every site routes through it so the
// lifetime arm cannot be forgotten at one of them:
//
//   - Pdeathsig: the kernel takes the helper down when this binary dies, which
//     is the ONLY arm that covers `go test -timeout` (an os.Exit) and a
//     SIGKILLed binary — the two paths where no t.Cleanup runs at all;
//   - a NEUTRAL working directory: the helper's cwd is never the subject here
//     (its PID is), and a survivor standing in the checkout would make the
//     ground's live-holder veto read a finished run as IN USE.
//
// A spawn failure skips, never passes: an unavailable `sleep` must not look
// like a satisfied case (Rule 15a).
func shotUnitHelperProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", shotUnitHelperHoldSeconds)
	cmd.Dir = t.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a helper process: %v", err)
	}
	return cmd
}

// TestShotUnitSweepStopsAProvablyDeadCreatorsUnit is the GREEN core: a creator
// that is measurably gone authorises the stop, the stop is the ONE injected
// teardown, and the registration is removed afterwards.
func TestShotUnitSweepStopsAProvablyDeadCreatorsUnit(t *testing.T) {
	dir := unitFixtureDir(t)
	unit := fakeUnitName("dead")

	// Register while the process was still alive, then kill it: the record
	// states a pid + incarnation that no longer exists.
	live := shotUnitHelperProcess(t)
	if _, err := RegisterShotUnit(dir, unit, live.Process.Pid); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := live.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_ = live.Wait() //stylegate:ignore reaps the fixture process the test expects to be reaped

	verdict, reason := ShotUnitCreatorVerdict(
		ShotUnitRecord{Unit: unit, CreatorPid: live.Process.Pid, CreatorStartTicks: 1},
		procname.DefaultProcRoot)
	if verdict != shotUnitCreatorGone && verdict != shotUnitCreatorRecycled {
		t.Fatalf("verdict for a killed creator = %q (%s), want %s/%s",
			verdict, reason, shotUnitCreatorGone, shotUnitCreatorRecycled)
	}

	var stopped []string
	out, err := ReclaimShotUnits(procname.DefaultProcRoot, dir, func(u string) error {
		stopped = append(stopped, u)
		return nil
	})
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if len(stopped) != 1 || stopped[0] != unit {
		t.Fatalf("stopped = %v, want exactly [%s]", stopped, unit)
	}
	if out.Renewed != 1 || out.Kept != 0 {
		t.Fatalf("outcome = %+v, want 1 stopped / 0 kept", out)
	}
	if _, err := os.Stat(filepath.Join(dir, unit)); !os.IsNotExist(err) {
		t.Fatalf("registration survived the successful stop (stat err = %v)", err)
	}
}

// TestShotUnitSweepLeavesALiveCreatorsUnitAlone is the direction that protects a
// RUNNING capture: the creator is this very process, so its incarnation is
// measurable and LIVE. Nothing may be stopped.
func TestShotUnitSweepLeavesALiveCreatorsUnitAlone(t *testing.T) {
	dir := unitFixtureDir(t)
	unit := fakeUnitName("live")
	if _, err := RegisterShotUnit(dir, unit, os.Getpid()); err != nil {
		t.Fatalf("register: %v", err)
	}
	stopCalled := false
	out, err := ReclaimShotUnits(procname.DefaultProcRoot, dir, func(string) error {
		stopCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if stopCalled {
		t.Fatal("the sweep stopped the unit of a LIVE creator")
	}
	if out.Kept != 1 || out.Renewed != 0 {
		t.Fatalf("outcome = %+v, want 1 kept / 0 stopped", out)
	}
	if len(out.KeptRecords) != 1 || out.KeptRecords[0].Verdict != shotUnitCreatorLive {
		t.Fatalf("kept verdict = %+v, want %s", out.KeptRecords, shotUnitCreatorLive)
	}
	if _, err := os.Stat(filepath.Join(dir, unit)); err != nil {
		t.Fatalf("registration of a live creator was removed: %v", err)
	}
}

// TestShotUnitSweepKeepsAnUnmeasurableCreator is Rule 15a: a creator the kernel
// will not describe at all is a THIRD answer — kept, with a named reason, and
// never rendered as a death.
func TestShotUnitSweepKeepsAnUnmeasurableCreator(t *testing.T) {
	dir := unitFixtureDir(t)
	unit := fakeUnitName("unmeasurable")
	procRoot := t.TempDir()
	pid := "4242"
	pidDir := filepath.Join(procRoot, pid)
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatalf("fixture mkdir: %v", err)
	}
	statPath := filepath.Join(pidDir, procname.StatName)
	if err := os.WriteFile(statPath, []byte(pid+" (probe) R 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 0 0 0 900001 0\n"), 0o644); err != nil {
		t.Fatalf("fixture stat: %v", err)
	}
	if err := os.Chmod(statPath, 0o000); err != nil {
		t.Fatalf("fixture chmod: %v", err)
	}
	if _, err := os.ReadFile(statPath); err == nil {
		t.Skip("running as a user that can read a 0000 file (root?) — the unmeasurable fixture cannot be built")
	}
	rec := ShotUnitRecord{Unit: unit, CreatorPid: 4242, CreatorStartTicks: 900001}
	if err := os.MkdirAll(filepath.Join(dir, unit), 0o755); err != nil {
		t.Fatalf("fixture unit dir: %v", err)
	}
	if err := writeShotUnitRecord(dir, rec); err != nil {
		t.Fatalf("fixture record: %v", err)
	}

	verdict, reason := ShotUnitCreatorVerdict(rec, procRoot)
	if verdict != shotUnitCreatorUnmeasurable {
		t.Fatalf("verdict = %q (%s), want %s", verdict, reason, shotUnitCreatorUnmeasurable)
	}
	stopCalled := false
	out, err := ReclaimShotUnits(procRoot, dir, func(string) error {
		stopCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if stopCalled {
		t.Fatal("an UNMEASURABLE creator authorised a stop — that stops a live capture's chrome")
	}
	if out.Kept != 1 || out.Renewed != 0 {
		t.Fatalf("outcome = %+v, want 1 kept / 0 stopped", out)
	}
	if len(out.KeptRecords) != 1 || !strings.Contains(out.KeptRecords[0].Reason, "could not be measured") {
		t.Fatalf("kept reason = %+v, want the unmeasurable reading named", out.KeptRecords)
	}
}

// TestShotUnitSweepKeepsAnUnreadableRegistration: a record this code cannot vouch
// for is kept and NAMED — never silently skipped, never acted on.
func TestShotUnitSweepKeepsAnUnreadableRegistration(t *testing.T) {
	dir := unitFixtureDir(t)
	unit := fakeUnitName("garbage")
	if err := os.MkdirAll(filepath.Join(dir, unit), 0o755); err != nil {
		t.Fatalf("fixture unit dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, unit, ShotUnitRecordFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("fixture record: %v", err)
	}
	stopCalled := false
	out, err := ReclaimShotUnits(procname.DefaultProcRoot, dir, func(string) error {
		stopCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if stopCalled {
		t.Fatal("an unreadable registration authorised a stop")
	}
	if out.Scanned != 1 || out.Kept != 1 {
		t.Fatalf("outcome = %+v, want 1 scanned / 1 kept", out)
	}
	if out.KeptRecords[0].Verdict != ShotUnitRegistryUnreadable {
		t.Fatalf("verdict = %q, want %q", out.KeptRecords[0].Verdict, ShotUnitRegistryUnreadable)
	}
}

// TestShotUnitRecordRefusesAForeignUnitName: a record naming a unit outside the
// capture namespace must never reach a `systemctl stop`.
func TestShotUnitRecordRefusesAForeignUnitName(t *testing.T) {
	for _, bad := range []string{"", "other.service", "gopi-shot-/etc/passwd", "-gopi-shot-x", "gopi-shots"} {
		if err := ValidateShotUnitName(bad); err == nil {
			t.Errorf("ValidateShotUnitName(%q) = nil, want a refusal", bad)
		}
	}
	if err := ValidateShotUnitName(fakeUnitName("ok")); err != nil {
		t.Errorf("ValidateShotUnitName(a capture unit) = %v, want nil", err)
	}
}

// TestShotUnitStopFailureKeepsTheRegistration: a stop that FAILED must leave the
// record in place for the next sweep (Rule 15 — never a deregistration that hid
// a unit still running).
func TestShotUnitStopFailureKeepsTheRegistration(t *testing.T) {
	dir := unitFixtureDir(t)
	unit := fakeUnitName("stopfail")
	live := shotUnitHelperProcess(t)
	defer func() {
		_ = live.Process.Kill() //stylegate:ignore kills the fixture process the test is tearing down
		_ = live.Wait()         //stylegate:ignore reaps the fixture process the test expects to be reaped
	}()
	if _, err := RegisterShotUnit(dir, unit, live.Process.Pid); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := live.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_ = live.Wait() //stylegate:ignore reaps the fixture process the test expects to be reaped

	out, err := ReclaimShotUnits(procname.DefaultProcRoot, dir, func(string) error {
		return os.ErrPermission
	})
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if out.Renewed != 0 || out.Kept != 1 || out.Denied != 1 {
		t.Fatalf("outcome = %+v, want 0 renewed / 1 kept / 1 denied (a failed stop is not a neutral keep)", out)
	}
	if len(out.DeniedRecords) != 1 || out.DeniedRecords[0].Unit != unit {
		t.Fatalf("denied records = %+v, want the unit named", out.DeniedRecords)
	}
	if !strings.HasSuffix(out.DeniedRecords[0].Reason, ShotUnitStopFailedReason) {
		t.Fatalf("denied reason = %q, want it to carry the stop-failure marker %q",
			out.DeniedRecords[0].Reason, ShotUnitStopFailedReason)
	}
	if _, err := os.Stat(filepath.Join(dir, unit, ShotUnitRecordFileName)); err != nil {
		t.Fatalf("registration was removed after a FAILED stop: %v", err)
	}
}

// TestShotUnitNormalStopDeregisters is the normal-exit half: the unit's own
// teardown removes the registration, so a healthy capture leaves nothing behind.
func TestShotUnitNormalStopDeregisters(t *testing.T) {
	dir := unitFixtureDir(t)
	unit := fakeUnitName("normal")
	if _, err := RegisterShotUnit(dir, unit, os.Getpid()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, unit, ShotUnitRecordFileName)); err != nil {
		t.Fatalf("registration missing right after register: %v", err)
	}
	if err := DeregisterShotUnit(dir, unit); err != nil {
		t.Fatalf("deregister: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, unit)); !os.IsNotExist(err) {
		t.Fatalf("registration survived deregister (stat err = %v)", err)
	}
	// Idempotent: the second call is the normal case (chrome already exited).
	if err := DeregisterShotUnit(dir, unit); err != nil {
		t.Fatalf("deregister twice: %v", err)
	}
	out, err := ReclaimShotUnits(procname.DefaultProcRoot, dir, func(string) error { return nil })
	if err != nil {
		t.Fatalf("reclaim of an empty registry: %v", err)
	}
	if out.Scanned != 0 || out.Renewed != 0 || out.Kept != 0 {
		t.Fatalf("outcome of an empty registry = %+v, want all zeros", out)
	}
}

// TestShotUnitSweepOnAMissingRegistryIsAnEmptySweep: a host that never ran a
// capture has no registry directory, and that is "nothing to do", not an error
// (the same contract SweepHome has for a missing scratch home).
func TestShotUnitSweepOnAMissingRegistryIsAnEmptySweep(t *testing.T) {
	dir := filepath.Join(unitFixtureDir(t), "never-created")
	out, err := ReclaimShotUnits(procname.DefaultProcRoot, dir, func(string) error { return nil })
	if err != nil {
		t.Fatalf("reclaim on a missing registry: %v", err)
	}
	if out.Scanned != 0 {
		t.Fatalf("outcome = %+v, want a zero scan", out)
	}
	plans, err := PlanShotUnits(procname.DefaultProcRoot, dir)
	if err != nil || len(plans) != 0 {
		t.Fatalf("plan on a missing registry = %v, %v; want empty, nil", plans, err)
	}
}

// TestShotUnitRegistrySurvivesTheHomeSweep is the interaction guard: the
// registry lives INSIDE the scratch home, so the home sweep must never collect it
// (nor its records) — otherwise a stale sweep would delete the very evidence the
// reclamation depends on.
func TestShotUnitRegistrySurvivesTheHomeSweep(t *testing.T) {
	home := t.TempDir()
	registry := filepath.Join(home, ShotUnitRegistryDirName)
	unit := fakeUnitName("survives")
	if _, err := RegisterShotUnit(registry, unit, os.Getpid()); err != nil {
		t.Fatalf("register: %v", err)
	}
	// One genuinely collectable sibling, so the sweep actually does work and
	// this test cannot pass by the sweep being a no-op.
	stale := filepath.Join(home, SuiteRunMarker+"fixture-run-1")
	if err := os.MkdirAll(stale, DirPerm); err != nil {
		t.Fatalf("fixture stale root: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("backdate stale root: %v", err)
	}

	if Collectable(registry, ShotUnitRegistryDirName, time.Now(), time.Nanosecond, "-1") {
		t.Fatal("the registry directory is collectable by the home sweep — a sweep would delete live registrations")
	}
	plans, err := SweepHomePlan(home, time.Nanosecond)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, p := range plans {
		if p.Path == registry || strings.HasPrefix(p.Path, registry+string(filepath.Separator)) {
			t.Fatalf("the home sweep's plan names the registry: %s", p.Path)
		}
	}
	removed, err := SweepHome(home, time.Nanosecond)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed == 0 {
		t.Fatal("the sweep removed nothing — the fixture cannot distinguish 'kept' from 'not looked at'")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("the stale sibling survived, so this test measured nothing (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(registry, unit, ShotUnitRecordFileName)); err != nil {
		t.Fatalf("the registration did not survive the home sweep: %v", err)
	}
}

// TestShotUnitPlanAgreesWithTheSweep: the reports an operator vets with
// (`--dry-run`) must promise exactly what the real sweep does.
func TestShotUnitPlanAgreesWithTheSweep(t *testing.T) {
	dir := unitFixtureDir(t)
	live := shotUnitHelperProcess(t)
	defer func() {
		_ = live.Process.Kill() //stylegate:ignore kills the fixture process the test is tearing down
		_ = live.Wait()         //stylegate:ignore reaps the fixture process the test expects to be reaped
	}()
	doomed := shotUnitHelperProcess(t)
	liveUnit := fakeUnitName("plan-live")
	deadUnit := fakeUnitName("plan-dead")
	if _, err := RegisterShotUnit(dir, liveUnit, live.Process.Pid); err != nil {
		t.Fatalf("register live: %v", err)
	}
	if _, err := RegisterShotUnit(dir, deadUnit, doomed.Process.Pid); err != nil {
		t.Fatalf("register doomed: %v", err)
	}
	if err := doomed.Process.Kill(); err != nil {
		t.Fatalf("kill doomed helper: %v", err)
	}
	_ = doomed.Wait() //stylegate:ignore reaps the fixture process the test expects to die

	plans, err := PlanShotUnits(procname.DefaultProcRoot, dir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	byUnit := map[string]ShotUnitPlan{}
	for _, p := range plans {
		byUnit[p.Unit] = p
	}
	if got := byUnit[deadUnit]; got.Keep {
		t.Fatalf("plan keeps the dead creator's unit: %+v", got)
	}
	if got := byUnit[liveUnit]; !got.Keep {
		t.Fatalf("plan stops the live creator's unit: %+v", got)
	}
	out, err := ReclaimShotUnits(procname.DefaultProcRoot, dir, func(string) error { return nil })
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if out.Renewed != 1 || out.RenewedUnits[0] != deadUnit {
		t.Fatalf("sweep stopped %v, want exactly [%s]", out.RenewedUnits, deadUnit)
	}
	_ = byUnit
}

// TestShotUnitRegistryDenyReasonNamesAWholeRegistryTheProcessCannotActOn is the
// start-up probe's contract: a registry this user cannot WRITE is reported with
// the path and the reason (the trigger prints one sentence naming it, Rule 15),
// and the two non-cases stay silent so a fresh host never hears about a sweep it
// did not need.
func TestShotUnitRegistryDenyReasonNamesAWholeRegistryTheProcessCannotActOn(t *testing.T) {
	// A registry that does not exist: nothing has ever been registered here, so
	// there is nothing to deny — "no capture has run" must stay silent.
	if why := ShotUnitRegistryDenyReason(filepath.Join(t.TempDir(), "absent")); why != "" {
		t.Fatalf("a missing registry produced a denial: %q", why)
	}

	dir := unitFixtureDir(t)
	unit := fakeUnitName("deny-probe")
	if _, err := RegisterShotUnit(dir, unit, os.Getpid()); err != nil {
		t.Fatalf("register: %v", err)
	}
	// A writable registry with a registration is not a denial either: the probe
	// answers "is anything known against this process", not "is there work".
	if why := ShotUnitRegistryDenyReason(dir); why != "" {
		t.Fatalf("a writable registry produced a denial: %q", why)
	}

	// Remove the owner-write bit: now the sweep could stop a unit but could not
	// dereigster it, which is exactly the state the trigger must announce.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	//stylegate:ignore sets a fixture file's mode; the mode read back is the assertion
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	why := ShotUnitRegistryDenyReason(dir)
	if why == "" {
		t.Fatal("a registry this process cannot write produced no denial — the trigger would stay silent")
	}
	if !strings.Contains(why, dir) || !strings.Contains(why, "not writable") {
		t.Fatalf("denial %q does not name the path and the reason", why)
	}
}
