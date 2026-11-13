package main

// cli_scratch_gc_test.go — the one-shot sweep verb's contract: the watchdog
// face must actually sweep (acceptance b: a 7h-old orphan is gone with no test
// running), must report it in the exact logfmt shape the acceptance greps for,
// must honour the operator's off switch, and must never delete a directory a
// live process holds (the epic's hard rule).
//
// The E2E tests exec the REAL built binary through testutil.RunGopi against a
// SANDBOX scratch home (GOPI_TEST_HOME + GOPI_TEST_SCRATCH_HOME both inside a
// t.TempDir), so nothing here can touch the developer's real scratch tree.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// scratchGCEnv is the child environment for the E2E cases: a sandbox home, a
// sandbox scratch home, no ambient GOPI_SETTINGS (the real ~/.gopi settings
// would otherwise decide the sweep's on/off switch for us).
func scratchGCEnv(home, scratchHome string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GOPI_HOME=" + home,
		"GOPI_SETTINGS=" + filepath.Join(home, "settings.json"),
		scratch.HomeEnvName + "=" + scratchHome,
	}
}

// seedScratchRoot creates one suite run root in the scratch home, backdated by
// age (0 = leave it fresh).
func seedScratchRoot(t *testing.T, scratchHome, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(scratchHome, name)
	if err := os.MkdirAll(path, scratch.DirPerm); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	if age > 0 {
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("backdate %s: %v", path, err)
		}
	}
	return path
}

// TestScratchGCCLI_RemovesStaleOrphanE2E is acceptance (b) as a test: a 7h-old
// abandoned run root disappears, a fresh one survives, and the log line names
// the scratch home and the removed count — all with NO test process pinning
// the sweep (the verb itself is the only sweeper in this sandbox).
func TestScratchGCCLI_RemovesStaleOrphanE2E(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	orphan := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-777777", 7*time.Hour)
	fresh := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-777778", 0)
	foreign := filepath.Join(scratchHome, "my-own-data")
	if err := os.MkdirAll(foreign, scratch.DirPerm); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-7 * time.Hour)
	if err := os.Chtimes(foreign, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Errorf("7h-old orphan %s survived the sweep (err=%v)", orphan, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh root %s was collected — the age rule must protect it: %v", fresh, err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("foreign entry %s was collected — the sweep is namespace-limited: %v", foreign, err)
	}
	// The log line is the acceptance evidence: home + removed count, logfmt.
	if !strings.Contains(out, "role=cli") || !strings.Contains(out, "home="+scratchHome) {
		t.Errorf("log line must name the role and the scratch home, got: %q", out)
	}
	if !strings.Contains(out, "removed=1") {
		t.Errorf("log line must report removed=1, got: %q", out)
	}
}

// TestScratchGCCLI_NeverRemovesHeldRoot is the hard rule: a stale root whose
// .holder registration points at a LIVE process must survive, and the sweep
// must still collect its unheld neighbour in the same run.
func TestScratchGCCLI_NeverRemovesHeldRoot(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	held := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-666001", 7*time.Hour)
	loose := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-666002", 7*time.Hour)

	// A live child process is the holder: RegisterHolder records OUR pid +
	// kernel start time, and this test process is alive for the whole run.
	scratch.RegisterHolder(held)
	t.Cleanup(func() { _ = os.Remove(filepath.Join(held, scratch.HolderFileName)) }) //stylegate:ignore t.Cleanup removal of the marker this test registered; a failed removal cannot change the assertion already made

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(held); err != nil {
		t.Errorf("HELD root %s was removed — the hard rule is violated: %v", held, err)
	}
	if _, err := os.Lstat(loose); !os.IsNotExist(err) {
		t.Errorf("unheld neighbour %s survived — the sweep must still work (err=%v)", loose, err)
	}
	if !strings.Contains(out, "removed=1") {
		t.Errorf("expected removed=1 (the unheld root only), got: %q", out)
	}
}

// TestScratchGCCLI_DryRunDeletesNothing pins the vetting face: the plan must
// name the candidate and the real sweep must be the only thing that deletes.
func TestScratchGCCLI_DryRunDeletesNothing(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	orphan := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-555001", 7*time.Hour)

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCDryRunFlag}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(out, "dry-run=true") || !strings.Contains(out, "removed=1") {
		t.Errorf("dry-run summary must report the plan size, got: %q", out)
	}
	if !strings.Contains(out, "would-remove "+orphan) {
		t.Errorf("dry-run must name the candidate %s, got: %q", orphan, out)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("dry-run DELETED %s — a report must have no side effects: %v", orphan, err)
	}
}

// TestScratchGCCLI_OffSwitchHonoured: the operator's knob wins and the verb
// reports itself disabled instead of sweeping (a timer must be able to be
// switched off without an alarm).
func TestScratchGCCLI_OffSwitchHonoured(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	orphan := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-444001", 7*time.Hour)

	env := append(scratchGCEnv(home, scratchHome), scratch.GCIntervalEnvName+"=0")
	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", env, "")
	if code != 0 {
		t.Fatalf("a disabled sweep must exit 0, got exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(out, "disabled=true") {
		t.Errorf("the log must say the sweep was disabled, got: %q", out)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("a disabled sweep deleted %s: %v", orphan, err)
	}
}

// TestScratchGCCLI_UsageIsSideEffectFree: -h must print help and exit 0
// WITHOUT sweeping (the P0-1 lesson: a help flag had a live side effect).
func TestScratchGCCLI_UsageIsSideEffectFree(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	orphan := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-333001", 7*time.Hour)

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, "-h"}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(out, scratchGCUsageFingerprint) {
		t.Errorf("-h must print the usage head, got: %q", out)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("-h swept the tree (%s gone) — a usage query must be zero-side-effect: %v", orphan, err)
	}
}

// scratchGCDeadPid finds a pid the kernel itself says is gone: it asks the SAME
// predicate the production lock decision uses (scratch.GCLockOwnerLivenessFor),
// so the fixture can never be red because it picked a number that happens to be
// live on this host.
func scratchGCDeadPid(t *testing.T) int {
	t.Helper()
	ceiling := 1 << 22
	raw, err := os.ReadFile("/proc/sys/kernel/pid_max")
	if err == nil {
		if n, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && n > 0 {
			ceiling = n
		}
	}
	for pid := ceiling; pid > ceiling-10000 && pid > 1; pid-- {
		if liveness, _ := scratch.GCLockOwnerLivenessFor(pid); liveness == scratch.GCLockOwnerGone {
			return pid
		}
	}
	t.Skip("no free pid above this host's process table — cannot build a dead-holder fixture")
	return 0
}

// TestScratchGCCLI_BrokenAbandonedLockIsReportedE2E is the epic's user-visible
// acceptance, read off the REAL binary: a `.gc.lock` whose recorded holder is
// DEAD must (a) not park the sweep, and (b) be named on the output, so an
// operator stops reading `removed=0` as "nothing to collect" when what actually
// happened is that the sweep never ran.
func TestScratchGCCLI_BrokenAbandonedLockIsReportedE2E(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	orphan := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-888001", 7*time.Hour)
	deadPid := scratchGCDeadPid(t)
	lockPath := filepath.Join(scratchHome, scratch.GCLockFileName)
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(deadPid)+"\n"), scratch.FilePerm); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Errorf("the dead holder's lock parked the sweep: %s survived (err=%v)", orphan, err)
	}
	if !strings.Contains(out, "removed=1") {
		t.Errorf("the sweep must have run and reported removed=1, got: %q", out)
	}
	if !strings.Contains(out, "lock="+string(scratch.GCLockOutcomeBrokeAbandoned)) {
		t.Errorf("the output must state the abandoned lock was broken, got: %q", out)
	}
	if !strings.Contains(out, "owner=pid:"+strconv.Itoa(deadPid)) {
		t.Errorf("the output must name the dead holder pid %d, got: %q", deadPid, out)
	}
	if !strings.Contains(out, "reason=") {
		t.Errorf("the decision must carry its sentence, got: %q", out)
	}
}

// TestScratchGCCLI_DeferredLiveLockNamesTheHolderE2E is the other half, and the
// reason the count line grew `lock=`/`owner=` fields: a pass deferred to a LIVE
// holder deletes nothing — and must SAY whose lock it deferred to, because that
// is the only thing separating "deferred" from "nothing to collect".
func TestScratchGCCLI_DeferredLiveLockNamesTheHolderE2E(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	orphan := seedScratchRoot(t, scratchHome, scratch.SuiteRunMarker+"cmd-agent-run-888002", 7*time.Hour)
	// This test process is alive for the whole run: its pid is a live holder.
	livePid := os.Getpid()
	if err := os.WriteFile(filepath.Join(scratchHome, scratch.GCLockFileName),
		[]byte(strconv.Itoa(livePid)+"\n"), scratch.FilePerm); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC}, "", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("a live holder must keep the sweep out of the tree: %s gone (%v)", orphan, err)
	}
	if !strings.Contains(out, "removed=0") {
		t.Errorf("the deferred pass removes nothing, got: %q", out)
	}
	if !strings.Contains(out, "lock="+string(scratch.GCLockOutcomeDeferredLive)) {
		t.Errorf("the output must state the deferral, got: %q", out)
	}
	if !strings.Contains(out, "owner=pid:"+strconv.Itoa(livePid)) {
		t.Errorf("the output must name the live holder pid %d, got: %q", livePid, out)
	}
}
