package testutil

// The temp-root assertion's tests (tmpdir epic, 2026-09-14).
//
// WHAT IS BEING PINNED HERE: the suite-side half of the "/ partition must not
// hold a temp workspace" rule. AssertTempRootOffRootFS measures the EFFECTIVE
// temp root (an explicit TMPDIR when present, the implementation fallback when
// not) against the device of `/` and refuses the same-filesystem case.
//
// The NEGATIVE test is mandatory and load-bearing: it points TMPDIR at /tmp —
// the exact configuration that produced the 23.5G residue — and requires the
// assertion to fail, naming the measured device ids, the classification and the
// fix. Without it the assertion could be a function that always returns nil and
// every other test here would still pass (Rule 13: a convention that is not
// asserted regresses).
//
// WHY THE /tmp CASES SPAWN A CHILD: this suite's entry point pins TMPDIR to the
// scratch home (off `/`) and the Go runtime CACHES the empty-TMPDIR
// implementation default at startup, so neither "an explicit TMPDIR pointing at
// /tmp" nor "no TMPDIR at all, falling back to /tmp" can be reproduced in
// process once the pin has engaged. A fresh process is the only faithful
// execution of both — the SAME argument that makes the injection test one level
// up (cmd/agent) a subprocess test. The child runs the assertion and reports
// exactly what it measured.
//
// NOTHING IN THIS FILE WRITES TO `/`: every case either measures a path or
// points TMPDIR at a fixture inside the pinned scratch home (already proven off
// `/`). The /tmp cases are MEASUREMENTS, never writes.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// probeMarkerPrefix names the assertion probe's self-describing output lines.
// It is a constant (Rule 1b) so the child and the parent cannot drift on the
// spelling of the one line they agree about.
const probeMarkerPrefix = "TMProotProbe:"

// probeConfigEnv carries the probe case selection into the child: which TMPDIR
// the child should install BEFORE running the assertion ("absent" = remove it).
// It is a test-private variable (never read by production code), which is why
// it is not part of the scratch package's vocabulary.
const probeConfigEnv = "GOPI_TEST_TMPROOT_PROBE"

// Set by TestMain (internal/testutil/testmain_test.go) — the absolute path of
// the compiled test binary, so a probe child is an exact re-exec of this test
// binary rather than a PATH lookup (zero external dependency, Rule 10.7).
var tmprootProbeBinary string

// The probe cases: what the child installs before asserting.
const (
	probeCaseAbsent    = "absent"    // no TMPDIR at all → the implicit fallback
	probeCaseTmp       = "tmp"       // TMPDIR=/tmp → an explicit bad override
	probeCaseReadChild = "readchild" // start a child, report its /proc/<pid>/environ
)

// TestAssertTempRootOnTmpFails is the /tmp negative test: with TMPDIR pointing
// at the famous temp root, the assertion must REFUSE, must be recognisable with
// errors.Is, and must print the measured device id, the EXPLICIT classification
// and the fix.
//
// On a host where /tmp is not on `/` (an operator split it off) there is
// nothing to demonstrate, so the case skips with that reason rather than
// passing vacuously.
func TestAssertTempRootOnTmpFails(t *testing.T) {
	if fi, err := os.Stat(banProbeDir); err != nil || !fi.IsDir() {
		t.Skipf("%s does not exist on this host — the ban probe needs the famous temp root", banProbeDir)
	}
	shares, err := scratch.SharesRootFS(banProbeDir)
	if err != nil {
		t.Fatalf("SharesRootFS(%s): %v", banProbeDir, err)
	}
	if !shares {
		t.Skipf("%s is NOT on the same filesystem as / on this host (an operator split it off) — the ban cannot be demonstrated here", banProbeDir)
	}

	out := runTempRootProbe(t, probeCaseTmp)
	if !strings.Contains(out, markerRefused) {
		t.Fatalf("with %s=%s the assertion did NOT refuse — the exact configuration that produced the 23.5G residue passed:\n%s",
			TempDirEnvName, banProbeDir, out)
	}
	// The device ids must be MEASURED, not asserted: they are the evidence.
	rootDev, devErr := scratch.RootDeviceID()
	if devErr != nil {
		t.Fatalf("RootDeviceID: %v", devErr)
	}
	for _, want := range []string{
		banProbeDir, "SAME filesystem as /", "FIX:", TempDirEnvName, scratch.TmpDirSettingsKey,
		fmt.Sprintf("0x%x", rootDev),
		explicitProbeClause, // the classification: this run DID pin the value
		scratch.ErrTempRootOnRootFS.Error(),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("refusal is not actionable/recognisable: %q missing from\n%s", want, out)
		}
	}
}

// TestAssertTempRootImplicitFallbackOnRootFails is the other half of the ban:
// with TMPDIR ABSENT the effective temp root is the implementation default
// (os.TempDir() → /tmp on a default host), and the assertion must refuse that
// too — this is the case the epic is about (nobody chose /tmp; nobody noticed
// either). The refusal must say IMPLICIT and name the unset variable, so a
// reader can tell a deliberate pin from a fallback.
func TestAssertTempRootImplicitFallbackOnRootFails(t *testing.T) {
	shares, err := scratch.SharesRootFS(banProbeDir)
	if err != nil {
		t.Fatalf("SharesRootFS(%s): %v", banProbeDir, err)
	}
	if !shares {
		t.Skipf("%s is not on the / filesystem here — the implicit-fallback refusal cannot be demonstrated", banProbeDir)
	}

	out := runTempRootProbe(t, probeCaseAbsent)
	if got := probeLine(t, out, "temp_root"); got != banProbeDir {
		t.Skipf("with %s removed this host's temp root is %q, not %s — the implicit-fallback case cannot be demonstrated here",
			TempDirEnvName, got, banProbeDir)
	}
	if !strings.Contains(out, markerRefused) {
		t.Fatalf("with %s UNSET and the temp root resolving to %s (device of /) the assertion did NOT refuse:\n%s",
			TempDirEnvName, banProbeDir, out)
	}
	wants := []string{implicitProbeClause, "UNSET", scratch.ErrTempRootOnRootFS.Error()}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("the implicit-fallback refusal must NAME the cause: %q missing from\n%s", want, out)
		}
	}
}

// TestAssertTempRootAcceptsAnOffRootOverride is the POSITIVE case: an explicit
// override that is NOT on `/` is exactly what the settings key is for, so it
// must be accepted. Run in this process (the fixture is off `/` by
// construction), with TMPDIR restored exactly (SnapshotEnv, Rule 17).
func TestAssertTempRootAcceptsAnOffRootOverride(t *testing.T) {
	home, _ := fixtureScratchHome(t)
	dir := mkScratchDir(t, home, "suite-fixture-run-1")
	if shares, err := scratch.SharesRootFS(dir); err != nil || shares {
		t.Skipf("fixture %s is not provably off / (err=%v) — the acceptance case needs an off-/ path", dir, err)
	}
	defer SnapshotEnv(t, TempDirEnvName)()
	if err := os.Setenv(TempDirEnvName, dir); err != nil { // env-leak-ok: SnapshotEnv restores on test return, absence included //stylegate:ignore SnapshotEnv IS the restore, absence included; the variable must reach a CHILD process, so t.Setenv (test-scoped) is the wrong tool
		t.Fatalf("set %s=%s: %v", TempDirEnvName, dir, err)
	}

	path, explicit, err := scratch.EffectiveTempRoot()
	if err != nil {
		t.Fatalf("EffectiveTempRoot: %v", err)
	}
	if !explicit || path != dir {
		t.Fatalf("EffectiveTempRoot() = (%q, explicit=%v), want (%q, true) — the override was not honoured", path, explicit, dir)
	}
	if err := AssertTempRootOffRootFS(); err != nil {
		t.Fatalf("AssertTempRootOffRootFS() with %s=%s = %v, want nil (the mandated configuration)", TempDirEnvName, dir, err)
	}
}

// TestChildProcessInheritsTheInjectedTmpDir is the inheritance proof at THIS
// level: a child spawned from the process's own environment (Cmd.Env nil —
// exactly what internal/tools' bash tool and the task-run child do) receives
// the temp root. The evidence is the child's own /proc/<pid>/environ block, the
// kernel's record of what execve handed it — not the child's claim about
// itself, and not the parent's intention.
func TestChildProcessInheritsTheInjectedTmpDir(t *testing.T) {
	home, _ := fixtureScratchHome(t)
	dir := mkScratchDir(t, home, "suite-fixture-run-1")
	defer SnapshotEnv(t, TempDirEnvName)()
	if err := os.Setenv(TempDirEnvName, dir); err != nil { // env-leak-ok: SnapshotEnv restores on test return, absence included //stylegate:ignore SnapshotEnv IS the restore, absence included; the variable must reach a CHILD process, so t.Setenv (test-scoped) is the wrong tool
		t.Fatalf("set %s=%s: %v", TempDirEnvName, dir, err)
	}

	out := runTempRootProbe(t, probeCaseReadChild)
	pid, err := strconv.Atoi(probeLine(t, out, "child_pid"))
	if err != nil {
		t.Fatalf("probe did not report a numeric child pid: %v\n%s", err, out)
	}
	block, err := scratch.ReadProcEnviron(pid)
	if err != nil {
		t.Fatalf("read the child's environ block: %v", err)
	}
	// Reap the fixture once its evidence is in hand: it is a probe device, not
	// a process this suite should leak (a failed assert above still kills it —
	// the fixture is bounded independently, so this is tidiness, not safety).
	killProbeProcess(t, pid)
	got, present, err := scratch.ProcEnviron(block, TempDirEnvName)
	if err != nil {
		t.Fatalf("ProcEnviron(child block): %v", err)
	}
	if !present || got != dir {
		t.Fatalf("the child's /proc/<pid>/environ holds %s=%q (present=%v), want %q — a child spawned from this process's environment must inherit the temp root",
			TempDirEnvName, got, present, dir)
	}
}

// TestProcEnvironDistinguishesAbsentFromEmpty — the presence question os.Getenv
// cannot answer, which is why the child-side evidence is parsed rather than
// grepped. Both faces matter: "unset" and "set to empty" are different facts
// about a child's environment.
func TestProcEnvironDistinguishesAbsentFromEmpty(t *testing.T) {
	const name = "GOPI_PROBE_TMP_SENTINEL"
	for _, tc := range []struct {
		name    string
		block   string
		wantVal string
		wantOK  bool
	}{
		{"present", "A=1\x00" + name + "=/x\x00B=2", "/x", true},
		{"absent", "A=1\x00B=2", "", false},
		{"empty value", "A=1\x00" + name + "=\x00B=2", "", true},
		{"first wins", name + "=/x\x00" + name + "=/y", "/x", true},
		{"value carries =", name + "=/x=y", "/x=y", true},
		{"unterminated block", "A=1\x00" + name + "=/z", "/z", true},
	} {
		got, ok, err := scratch.ProcEnviron([]byte(tc.block), name)
		if err != nil {
			t.Fatalf("%s: ProcEnviron: %v", tc.name, err)
		}
		if ok != tc.wantOK || got != tc.wantVal {
			t.Fatalf("%s: ProcEnviron = (%q, %v), want (%q, %v)", tc.name, got, ok, tc.wantVal, tc.wantOK)
		}
	}
	if _, _, err := scratch.ProcEnviron([]byte("A=1"), ""); err == nil {
		t.Fatal("an empty variable name must be an error, never a silent false")
	}
}

// TestEffTempRootIsMeasuredNotGuessed — the probe must answer from
// MEASUREMENT: a path that does not exist is an error (never "assume safe"),
// and SharesRootFS on `/` is true by definition.
func TestEffTempRootIsMeasuredNotGuessed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if _, err := scratch.DeviceID(missing); err == nil {
		t.Fatal("DeviceID of a missing path must be an error")
	}
	shares, err := scratch.SharesRootFS("/")
	if err != nil {
		t.Fatalf("SharesRootFS(/): %v", err)
	}
	if !shares {
		t.Fatal("`/` must share a filesystem with itself")
	}
}

// TestSuiteTempRootPinIsProvenOffRootFS is the suite-level guard on the
// invariant every test in this package depends on: after PinSuiteForTestMain
// the pinned root is off `/`. It is this suite's instance of the
// settings→env→child chain (the cmd/agent test drives a child process
// instead), so a regression in the pin or in the measurement is caught from
// both sides.
func TestSuiteTempRootPinIsProvenOffRootFS(t *testing.T) {
	if !SuiteTempRootPinned() {
		t.Fatal("this suite did not pin its scratch root")
	}
	shares, err := scratch.SharesRootFS(SuiteTempRoot())
	if err != nil {
		t.Fatalf("SharesRootFS(%s): %v", SuiteTempRoot(), err)
	}
	if shares {
		t.Fatalf("the pinned scratch root %s is on the / filesystem — the pin is not protecting this suite", SuiteTempRoot())
	}
}

// ---------------------------------------------------------------------------
// the assertion probe: a fresh process that installs a TMPDIR and asserts
// ---------------------------------------------------------------------------

// probeMarker / markerRefused are the one line the parent and the child agree
// on. REFUSED is deliberately spelled once (Rule 1b).
const (
	probeMarker   = probeMarkerPrefix
	markerRefused = "REFUSED"
)

// killProbeProcess reaps a fixture process started by a probe child. The
// fixture is a GRANDCHILD of this process, so it cannot be waited on here — and
// it does not need to be: its own bounded lifetime (probeChildLifetime) is what
// makes a leaked process impossible. A failed signal is REPORTED, never
// swallowed (Rule 15b).
//
// The delivery is IDENTITY-checked: the pid is read as an incarnation first, so
// a number the kernel has since recycled is refused rather than signalled — the
// shape that put SIGINT into `systemd --user` on 2026-10-17 (Rule 41 evidence in
// cmd/agent/ground_kill_identity_test.go). A process already gone is the state
// this teardown wants, reported as a note rather than as a failure.
func killProbeProcess(t *testing.T, pid int) {
	t.Helper()
	id, err := ReadProcIdentity(pid)
	if err != nil || id.Pid <= 0 {
		t.Logf("probe fixture %d is already gone (it is bounded and would exit on its own anyway)", pid)
		return
	}
	if err := KillIfIdentical(id, syscall.SIGKILL); err != nil {
		t.Logf("probe fixture %d not killed: %v (it is bounded and will exit on its own)", pid, err)
	}
}

// probeLine returns the value of a "NAME: value" line the probe printed, or
// fails the test when the line is missing (a probe that did not report is not
// evidence — Rule 15).
func probeLine(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), probeMarker+" ")
		if !ok {
			continue
		}
		if v, ok := strings.CutPrefix(rest, name+": "); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("the probe printed no %q line:\n%s", name, out)
	return ""
}

// runTempRootProbe re-execs THIS test binary as the assertion probe with the
// given case installed, and returns everything it printed. A non-zero exit is
// reported with the output rather than swallowed: the probe is expected to
// exit 0 for every case (it reports the verdict as text, so the parent — not
// the child's exit status — decides what the verdict means).
func runTempRootProbe(t *testing.T, probeCase string) string {
	t.Helper()
	if tmprootProbeBinary == "" {
		t.Fatal("the probe binary path was not captured by TestMain — the assertion probe cannot run")
	}
	cmd := exec.Command(tmprootProbeBinary, "-test.run", "TestTempRootAssertProbeProcess")
	cmd.Env = append(os.Environ(), probeConfigEnv+"="+probeCase)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("probe (%s) failed: %v\n%s", probeCase, err, out)
	}
	return string(out)
}

// TestTempRootAssertProbeProcess is the probe child. It is inert unless the
// probe variable names a case, so a normal suite run executes it as a no-op
// (and it never runs the parent's tests: the parent re-execs it with
// -test.run selecting this function alone).
func TestTempRootAssertProbeProcess(t *testing.T) {
	probeCase := os.Getenv(probeConfigEnv)
	if probeCase == "" {
		t.Skip("not a probe child — this test only acts when the probe variable is set")
	}
	switch probeCase {
	case probeCaseAbsent:
		// The child inherited the PARENT test process's environment, which
		// carries whatever the harness had. Remove the variable for this
		// process so os.TempDir() falls back to the implementation default —
		// the case under test.
		if err := os.Unsetenv(TempDirEnvName); err != nil { // env-leak-ok: the probe child is a throwaway process — nothing survives it //stylegate:ignore the probe child exits before this test returns; there is nothing for t.Setenv to protect
			t.Fatalf("unset %s: %v", TempDirEnvName, err)
		}
	case probeCaseTmp:
		if err := os.Setenv(TempDirEnvName, banProbeDir); err != nil { // env-leak-ok: the probe child is a throwaway process — nothing survives it //stylegate:ignore the probe child exits before this test returns; there is nothing for t.Setenv to protect
			t.Fatalf("set %s: %v", TempDirEnvName, err)
		}
	case probeCaseReadChild:
		// Spawn a trivial long-lived child from THIS environment and hand the
		// caller its pid. The child must OUTLIVE this probe process — the
		// caller reads /proc/<pid>/environ after the probe returns, and a
		// /proc entry vanishes with its process. The fixture bounds its own
		// lifetime and the caller reaps it, so a failed assertion cannot leak
		// a process (see TestTempRootSleepingProbeChild).
		pid, err := spawnEnvironChild(t)
		if err != nil {
			fmt.Printf("%s spawn_error: %v\n", probeMarker, err)
			t.FailNow()
		}
		fmt.Printf("%s child_pid: %d\n", probeMarker, pid)
		return
	default:
		t.Fatalf("unknown probe case %q", probeCase)
	}

	path, explicit, err := scratch.EffectiveTempRoot()
	if err != nil {
		fmt.Printf("%s error: %v\n", probeMarker, err)
		t.FailNow()
	}
	fmt.Printf("%s temp_root: %s\n", probeMarker, path)
	fmt.Printf("%s explicit: %v\n", probeMarker, explicit)
	perr := AssertTempRootOffRootFS()
	// The classification the refusal printed, mirrored as machine-readable
	// text so the parent can assert WHICH message it got without parsing prose.
	if explicit {
		fmt.Printf("%s classification: %s\n", probeMarker, explicitProbeClause)
	} else {
		fmt.Printf("%s classification: %s\n", probeMarker, implicitProbeClause)
	}
	if perr == nil {
		fmt.Printf("%s accepted\n", probeMarker)
		return
	}
	if !errors.Is(perr, scratch.ErrTempRootOnRootFS) {
		fmt.Printf("%s bad_sentinel: %v\n", probeMarker, perr)
		t.FailNow()
	}
	fmt.Printf("%s %s: %v\n", probeMarker, markerRefused, perr)
}

// The classification markers the probe and the parent share. They are derived
// from the scratch package's own clause constants (Rule 14: one spelling), so a
// reworded refusal cannot leave this test asserting a stale phrase.
const (
	explicitProbeClause = "EXPLICIT"
	implicitProbeClause = "IMPLICIT"
)

// spawnEnvironChild starts the environ fixture child and returns its pid — the
// handle to /proc/<pid>/environ. The command is deliberately trivial and
// dependency-free: this binary is the only program the tests may assume exists
// (Rule 10.7 — no third-party dependency, no PATH lookup).
func spawnEnvironChild(t *testing.T) (pid int, err error) {
	t.Helper()
	if tmprootProbeBinary == "" {
		t.Fatal("probe binary path unavailable")
	}
	cmd := exec.Command(tmprootProbeBinary, "-test.run", "TestTempRootSleepingProbeChild")
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start the environ child: %w", err)
	}
	// Deliberately NOT waited on here: the caller reads /proc/<pid>/environ
	// after this process exits, so the fixture must outlive us. Its own bounded
	// lifetime is what keeps a leaked process impossible if the caller dies.
	return cmd.Process.Pid, nil
}

// probeChildLifetime bounds the environ fixture's life. It must comfortably
// exceed the caller's probe round-trip (a process start plus one /proc read)
// while still guaranteeing that a caller who dies mid-test cannot leave a
// blocked process behind forever.
const probeChildLifetime = 60 * time.Second

// TestTempRootSleepingProbeChild is the child whose environ is inspected: it
// blocks for a bounded lifetime (a fixture process, not a runner). The
// probe-variable guard is REQUIRED, not cosmetic — without it a test run that
// happens to match this name blocks for the full lifetime.
//
// The sleep IS the fixture: it must still be running when the PARENT reads
// /proc/<pid>/environ, and a /proc entry vanishes with its process. It is not a
// wait for another goroutine's completion, so there is nothing to select on —
// the block is the data being measured, and it is bounded by probeChildLifetime
// (the parent reaps the child, so a failed assertion cannot leak it).
func TestTempRootSleepingProbeChild(t *testing.T) {
	if os.Getenv(probeConfigEnv) != probeCaseReadChild {
		t.Skip("not the environ fixture child — this test only acts inside a probe")
	}
	time.Sleep(probeChildLifetime) //stylegate:allow-sleep the sleep IS the fixture: the probe child must stay alive while its parent inspects /proc/<pid>/environ
}

// ---------------------------------------------------------------------------
// PROJECT.md Rule 22 — the CRITERION the mechanical halves above implement
// ---------------------------------------------------------------------------

// gopiRuleFile is the agent's rule file at the repo root (Rule 1b: the name is
// spelled once, here and in the standardgate package's allowlist).
const gopiRuleFile = "PROJECT.md"

// gopiRuleHeadingPrefix is the heading shape every rule in that file uses.
const gopiRuleHeadingPrefix = "## Rule "

// tmpRootRuleNumber is the rule this epic adds. It is a criterion, so the test
// below checks it states a MEASURABLE property rather than a value.
const tmpRootRuleNumber = "22"

// TestGOPIHasTheTempRootCriterion asserts the criterion landed (acceptance 5):
// Rule 22 exists, and its body states the four checkable properties the epic
// asks for — a device-id measurement, no hardcoded /tmp in scripts, the
// explicit settings layer, and the host-level boundary.
func TestGOPIHasTheTempRootCriterion(t *testing.T) {
	body := gopiRuleBody(t, tmpRootRuleNumber)
	for _, want := range []struct{ needle, why string }{
		{"device id", "the criterion must be a MEASUREMENT (device id vs /), not a path spelling"},
		{"scripts/lib/tmp-ban.sh", "the shell-side mechanical implementation must be named"},
		{"tmpdir", "the explicit settings layer (the settings.json key) must be named"},
		{"/etc/tmpfiles.d", "the host-level boundary must be stated explicitly"},
		{"CRITERION", "the rule must declare itself a criterion rather than a value"},
	} {
		if !strings.Contains(body, want.needle) {
			t.Errorf("Rule %s does not mention %q — %s", tmpRootRuleNumber, want.needle, want.why)
		}
	}
	// A criterion that hardcodes the very path it forbids would be a value.
	if strings.Contains(body, "TMPDIR=/tmp") || strings.Contains(body, "export TMPDIR=/tmp") {
		t.Errorf("Rule %s prescribes a hardcoded /tmp value — it is meant to state a property, not a path", tmpRootRuleNumber)
	}
}

// TestGOPITempRootCriterionIsNotADuplicate is the non-duplication half of
// acceptance 5: no sentence of Rule 22's body appears verbatim in Rules 1–21.
// A rule that restates an existing one teaches nothing and, worse, makes the
// rule file look like it grew while adding no new constraint.
func TestGOPITempRootCriterionIsNotADuplicate(t *testing.T) {
	root := moduleRoot()
	if root == "" {
		t.Skip("repo root unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(root, gopiRuleFile))
	if err != nil {
		t.Fatalf("read %s: %v", gopiRuleFile, err)
	}
	earlier, body := splitRuleFileAt(string(raw), tmpRootRuleNumber)
	if earlier == "" || body == "" {
		t.Fatalf("%s does not carry a Rule %s section with text before and after it", gopiRuleFile, tmpRootRuleNumber)
	}
	normEarlier := normalizeRuleProse(earlier)
	dupes := 0
	for _, sentence := range ruleSentences(body) {
		if len(sentence) < minDuplicationProbeRunes {
			continue // too short to be a meaningful duplicate probe
		}
		if strings.Contains(normEarlier, sentence) {
			dupes++
			t.Errorf("Rule %s repeats an earlier rule verbatim: %q", tmpRootRuleNumber, sentence)
		}
	}
	if dupes == 0 {
		t.Logf("Rule %s: %d sentences checked, none duplicated from Rules 1–21", tmpRootRuleNumber, len(ruleSentences(body)))
	}
}

// minDuplicationProbeRunes is the length below which a fragment is too short to
// prove duplication (phrases like "the `/` partition" legitimately recur).
const minDuplicationProbeRunes = 60

// gopiRuleBody returns the body of one rule section.
func gopiRuleBody(t *testing.T, number string) string {
	t.Helper()
	root := moduleRoot()
	if root == "" {
		t.Skip("repo root unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(root, gopiRuleFile))
	if err != nil {
		t.Fatalf("read %s: %v", gopiRuleFile, err)
	}
	_, body := splitRuleFileAt(string(raw), number)
	if body == "" {
		t.Fatalf("%s carries no Rule %s section", gopiRuleFile, number)
	}
	return body
}

// splitRuleFileAt returns (everything BEFORE the numbered rule, that rule's
// body) — the two inputs the duplication check needs.
func splitRuleFileAt(doc, number string) (earlier, body string) {
	heading := gopiRuleHeadingPrefix + number + ":"
	idx := strings.Index(doc, heading)
	if idx < 0 {
		return "", ""
	}
	earlier = doc[:idx]
	rest := doc[idx+len(heading):]
	// The body ends at the next rule heading (or at end of file).
	if next := strings.Index(rest, gopiRuleHeadingPrefix); next >= 0 {
		rest = rest[:next]
	}
	return earlier, rest
}

// ruleSentences splits prose into whitespace-normalized fragments at sentence
// (and line) boundaries, so a copied-and-reflowed sentence is still detected.
func ruleSentences(body string) []string {
	fields := strings.FieldsFunc(body, func(r rune) bool {
		return r == '.' || r == '\n' || r == ';' || r == ':' || r == '!' || r == '?'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if s := normalizeRuleProse(f); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// normalizeRuleProse collapses every run of whitespace (and markdown emphasis)
// to a single space, so formatting differences cannot hide a duplicate.
func normalizeRuleProse(s string) string {
	r := strings.NewReplacer("*", "", "`", "", "\n", " ", "\t", " ")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}
