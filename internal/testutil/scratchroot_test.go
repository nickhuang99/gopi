package testutil

// Scratch-root contract tests: the /tmp ban, the bounded (age + /proc-holder)
// GC, and the suite entry point this package uses itself.
//
// Every test that exercises the GC or the assertion runs against a FIXTURE
// scratch home (t.Setenv(WorkspaceHomeEnvName, t.TempDir())) so the real
// ~/workspace/gopi-test-scratch is never touched by the tests that verify
// how it is swept.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/scratch"
)

// TestMain is this package's OWN entry point: it pins the scratch root the
// same way every other suite does, and refuses to run on `/` — the pin and
// the assertion are exercised by the very tests below, so a regression in
// either is visible from inside this package.
// internalTestutilSuite is this package's suite name for the shared scratch
// conventions (the sweep scope and the pin both use it).
const internalTestutilSuite = "internal-testutil"

func TestMain(m *testing.M) {
	// The temp-root assertion probe (tmproot_assert_test.go) re-execs THIS test
	// binary as a fresh process, because neither an explicit TMPDIR=/tmp nor an
	// absent TMPDIR can be reproduced in a process whose pin has already
	// engaged (and the Go runtime caches the empty-TMPDIR default at startup).
	// The path is captured here, once — os.Args[0] is the compiled binary.
	tmprootProbeBinary = os.Args[0]
	// The fixture-lifetime children (fixturelifetime_test.go) are fixtures, not
	// suites: one is about to be SIGKILLed and the other exists to be killed by
	// the kernel's descriptor teardown, so neither may run a suite. Checked
	// before the daemon-lifetime holder for the same reason.
	if runFixtureHolderChild() {
		return
	}
	if runFixtureVictimChild() {
		return
	}
	if runLifetimeHolderAndProbeChild() {
		// A daemon-lifetime fixture child (daemonlifetime_test.go / lifetimeprobe.go)
		// arms the production watchdog on an inherited death pipe and blocks. It
		// must return BEFORE the pin and before m.Run: it is a fixture, not a
		// suite, and it is about to be killed on purpose.
		return
	}
	if os.Getenv(probeConfigEnv) != "" {
		// The temp-root assertion probe (tmproot_assert_test.go) is a FIXTURE
		// process, not a suite: the case under test IS the TMPDIR it inherited,
		// so it must run before any pin could overwrite it.
		os.Exit(m.Run())
	}
	if os.Getenv(reexecFallbackEnvName) != "" {
		// The IMPLICIT-fallback child of TestSuitePinFallbackToBootDeviceFails:
		// run the real suite entry point with a scratch home that CANNOT be
		// created, so the pin does not engage and the fallback guard must
		// refuse. Every other child (and every normal run) takes the healthy
		// path below.
		PinSuiteForTestMain("fallback-reexec")
		os.Exit(0) // only reachable if the guard failed to refuse
	}
	if os.Getenv(suiteEnvRawEnvName) != "" {
		// The RAW child of TestSuiteNotifyEnvRawSuiteFails: reproduce the
		// PRE-FIX entry point verbatim — scratch root only, notify environment
		// left ambient — so the red proof measures the real defect shape
		// rather than a re-creation of it. It must NOT reach the sandbox below.
		PinSuiteTempRoot("notify-raw-reexec")
		os.Exit(m.Run())
	}
	if os.Getenv(suiteAutoRegisterRawEnvName) != "" {
		// The RAW child of TestSuiteAutoRegisterRawSuiteFails: the PRE-FIX
		// shape for the AUTO-REGISTER switch — scratch root, notify and ledger
		// pinned as before, the switch deliberately left exactly as the ambient
		// environment hands it over (a clone-channel round exports =0). The
		// anchor test must fail here; it must NOT reach the pins below.
		PinSuiteTempRoot("autoregister-raw-reexec")
		PinSuiteNotifyEnv("autoregister-raw-reexec")
		PinSuiteLedgerEnv("autoregister-raw-reexec")
		os.Exit(m.Run())
	}
	if os.Getenv(suiteLedgerRawEnvName) != "" {
		// The RAW child of TestSuiteLedgerPinRawSuiteFails: the PRE-FIX ledger
		// shape — scratch root pinned as before, LEDGER left ambient (what
		// every suite did before ledgerpin.go). The anchor test must fail here;
		// it must NOT reach the pins below.
		PinSuiteTempRoot("ledger-raw-reexec")
		PinSuiteNotifyEnv("ledger-raw-reexec")
		os.Exit(m.Run())
	}
	PinSuiteTempRoot("internal-testutil")
	FatalAssertSuiteScratchRootSafe("internal-testutil")
	// Bind this suite's own fixture daemons to this process's lifetime (the
	// binding cases re-exec children and kill their holders by contract, so the
	// fixture must be armed exactly as cmd/agent's is), and sweep the orphans of
	// earlier runs of THIS suite.
	ArmAndSweepForSuite(internalTestutilSuite)
	// This suite asserts its own scratch root explicitly instead of calling
	// PinSuiteForTestMain, so it installs the notify + ledger sandboxes on its
	// own — the same calls that entry point makes, and the ones the anchor
	// tests measure.
	PinSuiteNotifyEnv("internal-testutil")
	PinSuiteLedgerEnv("internal-testutil")
	// The auto-register switch rides the same list: this suite's own TestMain
	// spells the pins instead of calling PinSuiteForTestMain, so it must not
	// forget one (2026-10-29, autoregisterenv.go).
	PinSuiteAutoRegisterEnv("internal-testutil")
	code := m.Run()
	// The creator removes what it created: this suite pins a run root of its
	// own, so it removes that root itself instead of leaving it to the 6h age
	// sweep. It cannot use the one-line RunSuiteMain (it asserts the root's
	// safety explicitly, which is the point of this package's own suite), so
	// the finish half is called here — the two-call form the gate documents
	// (scripts_gate_test.go). Without it every run of this package left an
	// empty suite-internal-testutil-run-* entry behind.
	FinishSuiteForTestMain()
	os.Exit(code)
}

// fixtureScratchHome points the scratch home at a fresh fixture and returns
// (home, queue) — both created. Restores the real home on cleanup.
func fixtureScratchHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(WorkspaceHomeEnvName, home)
	t.Setenv(SuiteTempRootEnvName, "")
	homePath := ScratchHomeDir()
	if err := os.MkdirAll(homePath, dirPerm); err != nil {
		t.Fatalf("fixture scratch home: %v", err)
	}
	queue := ScratchRootQueue()
	if err := os.MkdirAll(queue, dirPerm); err != nil {
		t.Fatalf("fixture scratch queue: %v", err)
	}
	return homePath, queue
}

// ageDir backdates a directory's mtime by d (fixture ageing: a 7h-old orphan
// without waiting 7 hours).
func ageDir(t *testing.T, dir string, d time.Duration) {
	t.Helper()
	when := time.Now().Add(-d)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatalf("age %s: %v", dir, err)
	}
}

// mkScratchDir creates one candidate directory inside the fixture home.
func mkScratchDir(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), dirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

// ---------------------------------------------------------------------------
// (A) location: the /tmp ban is asserted, not documented.
// ---------------------------------------------------------------------------

// TestAssertScratchRootOnTmpFails is the MECHANICAL /tmp ban: a scratch root
// on the same filesystem as `/` (which is what /tmp is on this host) must be
// a hard failure naming the reason AND the fix.
func TestAssertScratchRootOnTmpFails(t *testing.T) {
	if fi, err := os.Stat(banProbeDir); err != nil || !fi.IsDir() {
		t.Skipf("%s does not exist on this host — the ban probe needs the famous temp root", banProbeDir)
	}
	tmp, err := os.MkdirTemp(banProbeDir, "gopi-tmpban-probe-")
	if err != nil {
		t.Fatalf("probe dir under %s: %v", banProbeDir, err)
	}
	//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })

	shares, err := ScratchRootSharesRootFS(tmp)
	if err != nil {
		t.Fatalf("ScratchRootSharesRootFS(%s): %v", tmp, err)
	}
	if !shares {
		t.Skipf("%s is NOT on the same filesystem as / on this host (an operator split /tmp off) — the ban cannot be demonstrated here", tmp)
	}

	err = assertScratchPathSafe(tmp)
	if err == nil {
		t.Fatalf("assertScratchPathSafe(%s) = nil, want a refusal: a scratch root on the / filesystem is exactly the 2026-09-11 exhaustion", tmp)
	}
	if !errors.Is(err, ErrScratchRootOnRootFS) {
		t.Fatalf("error %v does not wrap ErrScratchRootOnRootFS — a caller cannot recognise the refusal", err)
	}
	msg := err.Error()
	for _, want := range []string{tmp, "SAME filesystem as /", "FIX:", SuiteTempRootEnvName, ScratchHomeDir()} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal message is not actionable: %q missing from\n%s", want, msg)
		}
	}
	// The device ids must be MEASURED, not asserted: they are the evidence.
	rootDev, err := RootDeviceID()
	if err != nil {
		t.Fatalf("RootDeviceID: %v", err)
	}
	if !strings.Contains(msg, fmt.Sprintf("0x%x", rootDev)) {
		t.Fatalf("refusal message does not name the measured root device 0x%x:\n%s", rootDev, msg)
	}
}

// TestAssertScratchRootSafeOnWorkspaceRoot — the POSITIVE case: a scratch
// root under the workspace is accepted (whatever it is backed by — the
// backing assertion is a separate, separately-worded guard).
func TestAssertScratchRootSafeOnWorkspaceRoot(t *testing.T) {
	home, _ := fixtureScratchHome(t)
	dir := mkScratchDir(t, home, "suite-fixture-run-1")
	if err := assertScratchPathSafe(dir); err != nil {
		t.Fatalf("assertScratchPathSafe(%s) = %v, want nil (the workspace is the mandated home)", dir, err)
	}
}

// TestScratchRootSharesRootFSAnswersFromStatfs — the classifier behind the
// ban must answer from measurement: `/` shares with itself, and an
// unreadable path is an ERROR, never a "safe" verdict (Rule 15).
func TestScratchRootSharesRootFSAnswersFromStatfs(t *testing.T) {
	shares, err := ScratchRootSharesRootFS("/")
	if err != nil {
		t.Fatalf("ScratchRootSharesRootFS(/): %v", err)
	}
	if !shares {
		t.Fatal("`/` must share a filesystem with itself")
	}
	if _, err := ScratchRootSharesRootFS(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("an unmeasurable path must be an error, never a silent 'safe'")
	}
	if _, err := ScratchDeviceID(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("ScratchDeviceID of a missing path must be an error")
	}
}

// TestScratchHomeIsNeverTheAmbientTempRoot — the location contract itself:
// with no override, the scratch home is under <home>/workspace, NOT
// os.TempDir() and not /tmp.
func TestScratchHomeIsNeverTheAmbientTempRoot(t *testing.T) {
	fixtureScratchHome(t)
	rawHome := os.Getenv(WorkspaceHomeEnvName)
	want := filepath.Join(rawHome, WorkspaceDirName, ScratchRootDirName)
	if got := ScratchHomeDir(); got != want {
		t.Fatalf("ScratchHomeDir() = %q, want %q", got, want)
	}
	if queue := ScratchRootQueue(); queue != filepath.Join(want, ScratchQueueDirName) {
		t.Fatalf("ScratchRootQueue() = %q, want %q", queue, filepath.Join(want, ScratchQueueDirName))
	}
	// The ambient temp root is NEVER a candidate: with TMPDIR cleared (its
	// system default is what os.TempDir() reports) the scratch home must not
	// move to it.
	t.Setenv(TempDirEnvName, "")
	if got := ScratchHomeDir(); got == os.TempDir() || got != want {
		t.Fatalf("with an unset %s the scratch home is %q (ambient temp root %q) — want %q",
			TempDirEnvName, got, os.TempDir(), want)
	}
	if got := SuiteTempRootDir(); got != want {
		t.Fatalf("SuiteTempRootDir() = %q, want the workspace scratch home %q (never %s)", got, want, os.TempDir())
	}
}

// TestSuiteScratchRootIsPinnedUnderTheScratchHome — the suite's own pin
// (this package's TestMain ran it): TMPDIR and t.TempDir() are inside the
// run's scratch root, the pin names the suite, and the root is off `/`.
//
// The "inside" half is PLACEMENT-AWARE, and that is a correction rather than a
// relaxation (2026-09-26, measured): scratchPlacementFor prefers a
// memory-backed base (/dev/shm) over the device the scratch home sits on —
// "inside the scratch home" was the OLD wording, and it is FALSE by
// construction on any host where the placement takes that base (this run's own
// line reads `placement memory-base, 2.7 GiB free`, root
// /dev/shm/suite-internal-testutil-run-<N>, device 28 against the home's 2052).
// The rule the pin owes is the one below: the root is inside the DIRECTORY THE
// PLACEMENT CHOSE, off `/`, and named for its suite. Both arms are covered —
// the device arm (a GOPI_TEST_TMPDIR run, where the caller's directory IS the
// home) keeps the scratch-home clause and gains the off-`/` one.
func TestSuiteScratchRootIsPinnedUnderTheScratchHome(t *testing.T) {
	if !SuiteTempRootPinned() {
		t.Fatal("TestMain did not pin the scratch root — every t.TempDir() here is back on the ambient temp root")
	}
	root := SuiteTempRoot()
	home := ScratchHomeDir()
	// Which arm this run took is the PLACEMENT's own answer, never re-derived
	// from the path: pairing the boolean with the label is what stops a run
	// from silently asserting the wrong arm (Rule 15).
	label := ScratchPlacementLabel()
	if mem := ScratchPlacementMemoryBacked(); mem != (label != placementDeviceRoot) {
		t.Fatalf("placement {memory=%v label=%q} disagrees with itself — the memory flag and the label are "+
			"two halves of one decision", mem, label)
	}
	switch {
	case ScratchPlacementMemoryBacked():
		// THE MEMORY-BASE ARM: the root hangs under the memory directory the
		// placement named, and it is memory-backed BY MEASUREMENT. The
		// scratch-home clause deliberately does NOT apply here (the memory
		// base is not a subtree of the home); the property that replaces it is
		// the one that made the placement choose it.
		dir := ScratchPlacementDir()
		if dir == "" {
			t.Fatalf("placement %q names no directory, so the pinned root %q is nested under nothing", label, root)
		}
		if !strings.HasPrefix(root, dir+string(filepath.Separator)) {
			t.Fatalf("placement %q: pinned root %q is not inside the directory the placement chose (%q)", label, root, dir)
		}
		if mem, description := ScratchRootFilesystem(root); !mem {
			t.Fatalf("placement %q put the pinned root %q on %s — the memory arm must be memory-backed, "+
				"which is the entire reason it was chosen over the device the scratch home sits on", label, root, description)
		}
	case label == placementDeviceRoot:
		// THE DEVICE ARM: the root IS the scratch home the caller pinned (the
		// placement takes a caller's directory as the home itself), so the
		// scratch-home clause still applies — and the run is an honest
		// degradation, which the placement report has already said out loud.
		if !strings.HasPrefix(root, home+string(filepath.Separator)) {
			t.Fatalf("placement %q: pinned root %q is not inside the scratch home %q", label, root, home)
		}
	default:
		// A future placement arm reaches here instead of being asserted as one
		// of the two above. This is the anti-vacuity guard: an arm nobody wrote
		// a clause for is a FAILURE, never a silent pass (Rule 15).
		t.Fatalf("pinned root %q carries placement label %q, for which this test has no arm — extend the "+
			"assertion rather than letting a new placement go unverified", root, label)
	}
	if pattern := scratchRunDirName(internalTestutilSuite); !strings.HasPrefix(filepath.Base(root), pattern) {
		// The marker is the PIN'S OWN pattern function, so the name this test
		// requires and the name PinSuiteTempRoot writes cannot drift apart
		// (Rule 1b: a keyword is taken from its definition site, never retyped).
		t.Fatalf("pinned root %q does not name its suite (want the prefix %q that scratchRunDirName(%q) builds)",
			filepath.Base(root), pattern, internalTestutilSuite)
	}
	if got := os.Getenv(TempDirEnvName); got != root {
		t.Fatalf("%s = %q, want the pinned root %q", TempDirEnvName, got, root)
	}
	dir := t.TempDir()
	if !strings.HasPrefix(dir, root) {
		t.Fatalf("t.TempDir() = %q, want a path under the pinned root %q", dir, root)
	}
	if shares, err := ScratchRootSharesRootFS(root); err != nil {
		t.Fatalf("ScratchRootSharesRootFS(%s): %v", root, err)
	} else if shares {
		t.Fatalf("the pinned scratch root %s is on the / filesystem", root)
	}
	// The published home lets a grandchild process find the same tree.
	if got := os.Getenv(ScratchHomeEnvName); got != home {
		t.Fatalf("%s = %q, want %q", ScratchHomeEnvName, got, home)
	}
}

// TestTestScratchDirLandsInTheQueueAndIsSelfCleaning — large throwaway files
// get a home under the GC'd scratch tree, removed by t.Cleanup (a subtest so
// the cleanup actually runs before the assertion).
func TestTestScratchDirLandsInTheQueueAndIsSelfCleaning(t *testing.T) {
	var captured string
	t.Run("inner", func(t *testing.T) {
		captured = TestScratchDir(t, "load")
		if !strings.HasPrefix(captured, ScratchRootQueue()) {
			t.Fatalf("TestScratchDir = %q, want a path under %q", captured, ScratchRootQueue())
		}
		if fi, err := os.Stat(captured); err != nil || !fi.IsDir() {
			t.Fatalf("TestScratchDir did not create %s: %v", captured, err)
		}
	})
	if _, err := os.Stat(captured); !os.IsNotExist(err) {
		t.Fatalf("%s survived t.Cleanup (stat err = %v) — the self-clean contract is broken", captured, err)
	}
}

// ---------------------------------------------------------------------------
// (B) bounded: age-based GC that never deletes a held directory.
// ---------------------------------------------------------------------------

// TestGCStaleScratchRemovesOldOrphanAndKeepsYoung — the acceptance case, plus
// its boundary: an UNHELD directory older than the threshold is collected, an
// unheld directory younger than it is not.
func TestGCStaleScratchRemovesOldOrphanAndKeepsYoung(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	home, _ := fixtureScratchHome(t)
	orphan := mkScratchDir(t, home, "suite-dead-old")
	young := mkScratchDir(t, home, "suite-live-young")
	ageDir(t, orphan, ScratchStaleAge+time.Hour) // 7h old
	ageDir(t, young, ScratchStaleAge-time.Hour)  // 5h old

	removed, err := GCStaleScratchRoot()
	if err != nil {
		t.Fatalf("GCStaleScratchRoot: %v", err)
	}
	if removed != 1 {
		t.Fatalf("GC removed %d entries, want exactly 1 (the 7h-old orphan)", removed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("7h-old orphan %s was NOT collected (stat err = %v)", orphan, err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Fatalf("5h-old %s was collected — the age boundary is off by more than an hour: %v", young, err)
	}
}

// TestGCNeverTouchesAnotherSuitesLiveDirectory is the NAMESPACE/LIVENESS
// boundary of the sweep, and it is deliberately two-sided:
//
//   - another suite's AGED root whose owner is still alive must survive. The
//     sweep is shared across suites (a suite killed mid-run leaves a root no
//     one else would ever collect — see TestGCCollectsAbandonedRunRoot), so
//     the ONLY thing standing between two concurrent suites is the liveness
//     veto: cwd/fd holder scan plus the .holder registration. Sweeping a
//     peer's live tree is the race this test exists to prevent.
//   - a foreign directory outside the scratch namespace is never a candidate
//     at all, however old it is (Rule 15: a check we cannot attribute never
//     authorises a delete).
//
// The aged, unheld orphan in THIS suite's namespace is the control: it must be
// the one and only entry removed, so a sweep that did nothing cannot pass.
func TestGCNeverTouchesAnotherSuitesLiveDirectory(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionWideParallel)
	home, _ := fixtureScratchHome(t)

	// A peer suite's run root, held live by this test's own child process
	// (spawned through the pgid-scoped reaper, so it is cleaned up too).
	peerLive := mkScratchDir(t, home, scratchRunDirName(peerSuiteName))
	holder := StartMockProcessDir(t, peerLive, sleepBinary, sleepArg)
	if err := waitForHolder(peerLive, strconv.Itoa(holder.Pid()), procHolderWait); err != nil {
		t.Fatalf("%v", err)
	}
	ageDir(t, peerLive, ScratchStaleAge+time.Hour)

	// A foreign, non-scratch directory in the same home (an operator's own
	// data): outside the namespace entirely.
	foreign := mkScratchDir(t, home, foreignDirName)
	ageDir(t, foreign, ScratchStaleAge+time.Hour)

	// Control: an aged, unheld orphan in this suite's namespace.
	orphan := mkScratchDir(t, home, scratchRunDirName(ownSuiteName))
	ageDir(t, orphan, ScratchStaleAge+time.Hour)

	removed, err := GCStaleScratchRoot()
	if err != nil {
		t.Fatalf("GCStaleScratchRoot: %v", err)
	}
	if removed != 1 {
		t.Fatalf("GC removed %d entries, want exactly 1 (the unheld orphan)", removed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the aged unheld orphan %s was NOT collected (stat err = %v) — the sweep did not run", orphan, err)
	}
	if _, err := os.Stat(peerLive); err != nil {
		t.Fatalf("another suite's LIVE directory %s was collected — a running peer lost its scratch: %v", peerLive, err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("a foreign directory %s (outside the scratch namespace) was collected: %v", foreign, err)
	}
}

// peerSuiteName / ownSuiteName / foreignDirName name the three participants
// above: a peer suite's namespace, this suite's namespace, and a tree that is
// not a scratch entry at all.
const (
	peerSuiteName  = "internal-other-suite"
	ownSuiteName   = "internal-testutil"
	foreignDirName = "operator-data"
)

// TestGCKeepsScratchHeldByALiveProcess is the HARD RULE: a directory older
// than the threshold whose owner is alive (cwd inside it) must survive.
func TestGCKeepsScratchHeldByALiveProcess(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionWideParallel)
	home, _ := fixtureScratchHome(t)
	held := mkScratchDir(t, home, "suite-running-held")

	// The holder is spawned through the shared pgid-scoped reaper: it is the
	// same "real live process" the production fixtures use, and its cleanup is
	// owned by the test that created it.
	holder := StartMockProcessDir(t, held, sleepBinary, sleepArg)
	pid := strconv.Itoa(holder.Pid())
	if err := waitForHolder(held, pid, procHolderWait); err != nil {
		t.Fatalf("%v", err)
	}

	ageDir(t, held, ScratchStaleAge+time.Hour)               // old enough to collect
	ageDir(t, filepath.Dir(held), ScratchStaleAge+time.Hour) // its parent moves when a child is created
	ageDir(t, home, ScratchStaleAge+time.Hour)               // the home itself must never be swept

	// The holder check is asserted directly (the GC decision is derived from
	// it) — a GC that ran with a broken check would delete a live tree.
	if holder, err := ScratchDirHolder(held); err != nil {
		t.Fatalf("ScratchDirHolder(%s): %v", held, err)
	} else if holder != pid {
		t.Fatalf("ScratchDirHolder(%s) = %q, want the live holder %s", held, holder, pid)
	}

	removed, err := GCStaleScratchRoot()
	if err != nil {
		t.Fatalf("GCStaleScratchRoot: %v", err)
	}
	if removed != 0 {
		t.Fatalf("GC removed %d entries while a live process held %s — the /proc holder check failed", removed, held)
	}
	if _, err := os.Stat(held); err != nil {
		t.Fatalf("the held directory %s was deleted out from under a live process: %v", held, err)
	}
	// And once the holder is gone the same entry IS collectable — proving the
	// survivor above was kept by the holder check, not by an inert sweep.
	holder.Reap()
	if err := waitForNoHolder(held, procHolderWait); err != nil {
		t.Fatalf("%v", err)
	}
	if removed, err := GCStaleScratchRoot(); err != nil {
		t.Fatalf("GC after the holder exited: %v", err)
	} else if removed != 1 {
		t.Fatalf("GC removed %d entries after the holder exited, want 1 (the aged root)", removed)
	}
	if _, err := os.Stat(held); !os.IsNotExist(err) {
		t.Fatalf("%s survived a GC with no holder (stat err = %v)", held, err)
	}
}

// TestGCKeepsScratchHeldByOpenFileDescriptor covers the second half of the
// holder check: a process whose CWD is elsewhere but which holds a scratch
// FILE open is still an owner.
func TestGCKeepsScratchHeldByOpenFileDescriptor(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	home, _ := fixtureScratchHome(t)
	held := mkScratchDir(t, home, "suite-fd")
	blob := filepath.Join(held, "load.bin")
	if err := os.WriteFile(blob, []byte("x"), filePerm); err != nil {
		t.Fatalf("write %s: %v", blob, err)
	}
	f, err := os.Open(blob)
	if err != nil {
		t.Fatalf("open %s: %v", blob, err)
	}
	defer f.Close()

	holder, err := ScratchDirHolder(held)
	if err != nil {
		t.Fatalf("ScratchDirHolder(%s): %v", held, err)
	}
	if holder != "" && holder != strconv.Itoa(os.Getpid()) {
		t.Fatalf("ScratchDirHolder(%s) = %q, want either nobody or this process", held, holder)
	}
	// The observable holder check can legitimately see nobody (a shared host
	// restricts /proc), so the SUITE's protection is the registration: assert
	// the liveness question the sweep asks about a live owner.
	registerScratchHolder(held)
	if alive, why := ScratchHolderAlive(held); !alive {
		t.Fatalf("the live process that owns %s was not recognised as a holder (%s) — the sweep would delete a running suite's scratch", held, why)
	}
	ageDir(t, held, ScratchStaleAge+time.Hour)
	ageDir(t, filepath.Dir(held), ScratchStaleAge+time.Hour)
	ageDir(t, home, ScratchStaleAge+time.Hour)
	if removed, err := GCStaleScratchRoot(); err != nil {
		t.Fatalf("GC: %v", err)
	} else if removed != 0 {
		t.Fatalf("GC removed %d entries while a REGISTERED live process held %s", removed, held)
	}
	if _, err := os.Stat(held); err != nil {
		t.Fatalf("the registered live holder %s was deleted: %v", held, err)
	}
	// ...and a dead registration is collectable: a stale pid (killed process,
	// or a reused one) must not protect garbage forever.
	if err := os.WriteFile(filepath.Join(held, holderFileName),
		[]byte("pid=999999999\nstart=1\n"), filePerm); err != nil {
		t.Fatalf("write stale holder: %v", err)
	}
	ageDir(t, held, ScratchStaleAge+time.Hour)
	if removed, err := GCStaleScratchRoot(); err != nil {
		t.Fatalf("GC after the holder died: %v", err)
	} else if removed != 1 {
		t.Fatalf("GC removed %d entries for a DEAD registration, want 1", removed)
	}
}

// TestGCUnreadableScratchEntryDoesNotPanic — an entry the sweep cannot
// measure is KEPT and never crashes: unreadable directory, dangling symlink,
// vanished entry.
func TestGCUnreadableScratchEntryDoesNotPanic(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	home, _ := fixtureScratchHome(t)
	locked := mkScratchDir(t, home, "suite-locked")
	ageDir(t, locked, ScratchStaleAge+time.Hour)
	if err := os.Chmod(locked, 0); err != nil {
		t.Skipf("cannot drop permissions on %s: %v", locked, err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, dirPerm) }) //stylegate:ignore restore of the mode this test dropped to 0; the test already holds the directory

	dangling := filepath.Join(home, "suite-dangling")
	if err := os.Symlink(filepath.Join(home, "does-not-exist"), dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	ageDir(t, home, ScratchStaleAge+2*time.Hour) // age the whole home (mtime moves with chmod/symlink)

	if _, err := GCStaleScratchRoot(); err != nil {
		t.Fatalf("GC over an unreadable/dangling entry: %v", err)
	}
	// A holder check that cannot be completed must not authorise a delete.
	if _, err := os.Stat(locked); err != nil {
		t.Fatalf("the unreadable entry %s was deleted instead of kept: %v", locked, err)
	}
	if _, err := os.Lstat(dangling); err != nil {
		t.Fatalf("the dangling symlink %s was deleted instead of kept: %v", dangling, err)
	}
	// The unreadable directory must also not be reported as safe-to-delete by
	// mistake: the sweep simply skips what it cannot measure (Rule 15).
	if err := os.Chmod(locked, dirPerm); err != nil {
		t.Fatalf("restore %s: %v", locked, err)
	}
}

// TestGCConcurrentSweepsAreSafe — two suites starting at the same moment must
// not sweep the same home into each other's faces: concurrent sweeps never
// panic, never remove a held directory, and the advisory lock lets at most
// one of them do the removal work.
func TestGCConcurrentSweepsAreSafe(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionWideParallel)
	home, _ := fixtureScratchHome(t)
	const orphans = 8
	for i := 0; i < orphans; i++ {
		ageDir(t, mkScratchDir(t, home, fmt.Sprintf("suite-orphan-%d", i)), ScratchStaleAge+time.Hour)
	}
	held := mkScratchDir(t, home, "suite-held")
	holder := StartMockProcessDir(t, held, sleepBinary, sleepArg)
	if err := waitForHolder(held, strconv.Itoa(holder.Pid()), procHolderWait); err != nil {
		t.Fatalf("%v", err)
	}
	ageDir(t, held, ScratchStaleAge+time.Hour)
	ageDir(t, filepath.Dir(held), ScratchStaleAge+time.Hour)
	ageDir(t, home, ScratchStaleAge+time.Hour)

	const sweeps = 6
	var wg sync.WaitGroup
	results := make([]int, sweeps)
	errs := make([]error, sweeps)
	for i := 0; i < sweeps; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = GCStaleScratchRoot()
		}(i)
	}
	wg.Wait()

	total := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent sweep %d: %v", i, err)
		}
		total += results[i]
	}
	if total != orphans {
		t.Fatalf("concurrent sweeps removed %d entries in total, want exactly %d orphans (each removed once)", total, orphans)
	}
	if _, err := os.Stat(held); err != nil {
		t.Fatalf("the held directory %s was removed by a concurrent sweep: %v", held, err)
	}
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("the scratch home itself was removed by the sweep: %v", err)
	}
}

// TestGCMissingHomeIsANoOp — GC on a home that does not exist is "nothing to
// do", and an override pointing outside the workspace is still swept (the
// operator pin is a scratch home too).
func TestGCMissingHomeIsANoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv(WorkspaceHomeEnvName, home)
	t.Setenv(SuiteTempRootEnvName, "")
	removed, err := GCStaleScratchRoot()
	if err != nil {
		t.Fatalf("GC with no scratch home: %v", err)
	}
	if removed != 0 {
		t.Fatalf("GC removed %d entries from a nonexistent home", removed)
	}
}

// TestGCCollectsAbandonedRunRoot is the regression guard for an UNBOUNDED
// growth mode found by measurement, not by reading: a per-run root
// ("suite-<name>-run-<rand>") was excluded from the sweep by name, so a suite
// KILLED mid-run left its root (and every test directory inside it) forever —
// 39 roots / 53M in this host's scratch home. The root's own mtime does not
// move while tests create and abandon directories inside it, so the age rule
// is the only bound it can have; a LIVE suite is protected by the holder
// channels, which this test also pins down.
func TestGCCollectsAbandonedRunRoot(t *testing.T) {
	home, _ := fixtureScratchHome(t)
	// An abandoned run root, 7h old, with the nested test garbage a killed
	// suite leaves behind.
	deadRoot := mkScratchDir(t, home, "suite-tools"+runSuffixPattern+"12345")
	if err := os.MkdirAll(filepath.Join(deadRoot, "TestFoo123", "001"), dirPerm); err != nil {
		t.Fatalf("nested test dir: %v", err)
	}
	ageDir(t, deadRoot, ScratchStaleAge+time.Hour)

	// A LIVE run root (this process holds it and has registered itself): the
	// same shape, aged the same way — it must survive.
	liveRoot := mkScratchDir(t, home, "suite-tools"+runSuffixPattern+"67890")
	registerScratchHolder(liveRoot)
	ageDir(t, liveRoot, ScratchStaleAge+time.Hour)

	removed, err := GCStaleScratchRoot()
	if err != nil {
		t.Fatalf("GCStaleScratchRoot: %v", err)
	}
	if removed != 1 {
		t.Fatalf("GC removed %d entries, want exactly 1 (the abandoned run root; the live one is held)", removed)
	}
	if _, err := os.Stat(deadRoot); !os.IsNotExist(err) {
		t.Fatalf("an abandoned run root %s was NOT collected — killed suites leak their whole tree (stat err = %v)", deadRoot, err)
	}
	if _, err := os.Stat(liveRoot); err != nil {
		t.Fatalf("a LIVE, holder-registered run root %s was collected — a running suite lost its scratch: %v", liveRoot, err)
	}
}

// TestGCStaleSweepBoundedAgeBoundary pins the exact threshold semantics
// (just under = kept, just over = collected) so a future edit that shifts the
// age by an hour is caught here rather than in production.
func TestGCStaleSweepBoundedAgeBoundary(t *testing.T) {
	home, _ := fixtureScratchHome(t)
	older := mkScratchDir(t, home, "suite-older")
	newer := mkScratchDir(t, home, "suite-newer")
	ageDir(t, older, ScratchStaleAge+time.Minute)
	ageDir(t, newer, ScratchStaleAge-time.Minute)

	removed, err := GCStaleScratchRootOlderThan(ScratchStaleAge)
	if err != nil {
		t.Fatalf("GCStaleScratchRootOlderThan: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d, want 1 (only the entry past the threshold)", removed)
	}
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Fatalf("entry past the threshold survived: %v", err)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Fatalf("entry inside the threshold was removed: %v", err)
	}
}

// TestPathWithinDoesNotMatchSiblingPrefixes — a prefix STRING match would call
// /scratch-other a child of /scratch (and authorise deleting a hold on it).
func TestPathWithinDoesNotMatchSiblingPrefixes(t *testing.T) {
	cases := []struct {
		path, target string
		want         bool
	}{
		{"/a/scratch", "/a/scratch", true},
		{"/a/scratch/sub/file", "/a/scratch", true},
		{"/a/scratch-other", "/a/scratch", false},
		{"/a/scratch-other/file", "/a/scratch", false},
		{"/b/scratch", "/a/scratch", false},
		{"/", "/", false},  // never claim the whole filesystem
		{"", "/a", false},  // an empty target
		{"/a", "", false},  // an empty link target resolves to "" or "."
		{"/a", ".", false}, //
	}
	for _, c := range cases {
		if got := pathWithin(c.path, c.target); got != c.want {
			t.Errorf("pathWithin(%q, %q) = %v, want %v", c.path, c.target, got, c.want)
		}
	}
}

// TestScratchHomeMountHintNamesTheFix — the hint is the remediation contract,
// so it must name the ONE command that fixes a missing scratch home. It is a
// mkdir, NOT a tmpfs mount: the requirement is a scratch home off the `/`
// device, and the memory-backed route was rejected (operator ruling,
// 2026-09-18). A regression that reintroduces the mount fails here.
func TestScratchHomeMountHintNamesTheFix(t *testing.T) {
	hint := ScratchHomeMountHint()
	for _, want := range []string{"mkdir -p", ScratchHomeDir()} {
		if !strings.Contains(hint, want) {
			t.Fatalf("mount hint %q does not contain %q", hint, want)
		}
	}
	if strings.Contains(hint, "tmpfs") {
		t.Fatalf("mount hint %q offers a tmpfs mount — the memory-backed route is retired "+
			"(the scratch home is a real directory off the / device)", hint)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

const (
	// banProbeDir is the temp root the ban is aimed at, spelled out: the
	// probe must land there even when this suite's own TMPDIR is pinned to a
	// memory filesystem (which is the normal, healthy state).
	banProbeDir = "/tmp"
	// sleepBinary / sleepArg are the holder fixture: a real live process with
	// a working directory we control.
	sleepBinary = "sleep"
	sleepArg    = "120"
	// procHolderWait bounds waiting for the kernel to publish the holder's
	// cwd/fd (exec plus /proc visibility is asynchronous).
	procHolderWait = 5 * time.Second
	// holderPollInterval is the polling step for that wait.
	holderPollInterval = 20 * time.Millisecond
)

// waitForHolder blocks until ScratchDirHolder reports pid (or the budget
// expires).
func waitForHolder(dir, pid string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		holder, err := ScratchDirHolder(dir)
		if err == nil && holder == pid {
			return nil
		}
		time.Sleep(holderPollInterval) //stylegate:allow-sleep poll interval while the holder process publishes its cwd
	}
	return fmt.Errorf("pid %s never showed up as the holder of %s within %v", pid, dir, budget)
}

// waitForNoHolder blocks until no live process holds dir (or the budget
// expires).
func waitForNoHolder(dir string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		holder, err := ScratchDirHolder(dir)
		if err == nil && holder == "" {
			return nil
		}
		time.Sleep(holderPollInterval) //stylegate:allow-sleep poll interval while the holder process publishes its cwd
	}
	return fmt.Errorf("a holder of %s never went away within %v", dir, budget)
}

// envForSetupRoutes builds the child environment for a setup-script route
// test: a clean copy of the ambient environment with every variable that can
// steer the script's scratch target REMOVED, then re-set to the fixture. The
// removal is the safety property (see the caller): a duplicate key would let
// the ambient value win and aim the script at the host's real scratch tree.
func envForSetupRoutes(base, home string) []string {
	drop := map[string]bool{
		scratchMemoryBaseEnvName: true,
		ScratchHomeEnvName:       true,
		SuiteTempRootEnvName:     true,
		WorkspaceHomeEnvName:     true,
	}
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop[kv[:i]] {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		scratchMemoryBaseEnvName+"="+base,
		ScratchHomeEnvName+"="+home,
	)
}

// TestSetupScratchHomeRefusesToLinkADirectoryToItsOwnAncestor is the minimal
// repro for a defect found by RUNNING the host setup script: its symlink route
// had no guard for a scratch home that resolves at or INSIDE the memory-backed
// tree, so re-running it with GOPI_TEST_SCRATCH_HOME set to the tree itself (or
// a subdirectory of it) replaced that path with a link to its own ancestor —
// observed on this host, 2026-09-11 06:30, as
// /dev/shm/gopi-test-scratch/gopi-test-scratch -> /dev/shm/gopi-test-scratch.
// Such a link is a loop every directory walk chases, and the migration route
// below it would `cp -a` the tree onto itself and then delete it.
//
// The test runs the script hermetically (a private base under the memory
// TestSetupScratchHomeIsRetiredAndChangesNothing pins the two contracts the
// retired setup script still owes (operator ruling, 2026-09-18: the scratch
// home stays on the block device, never a tmpfs):
//
//  1. It CHANGES NOTHING — no mount, no symlink, no migration, on any input.
//     This is the contract that matters: the script used to install the
//     memory backing, so a future edit that "restores" it must fail here.
//  2. It never creates the self-referential link (a scratch home inside the
//     memory tree linked to that tree) — the stray observed on this host on
//     2026-09-11, a loop no GC can drain.
//
// The script is run hermetically: the child environment is rebuilt with every
// scratch-related variable removed and then re-set to the fixture
// (envForSetupRoutes), so it can never aim at the host's real scratch tree.
func TestSetupScratchHomeIsRetiredAndChangesNothing(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	script := filepath.Join(root, scriptsDirName, scratchHomeSetupScript)
	if _, err := os.Stat(script); err != nil {
		t.Skipf("%s is absent on this checkout", script)
	}
	// The fixture base is the memory tree the script must NOT touch. A host
	// without a writable memory filesystem is skipped, not failed.
	base := filepath.Join(scratchMemoryBaseDefault, scratchSetupTestBasePrefix+strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(base, dirPerm); err != nil {
		t.Skipf("memory-backed base %s is not usable (%v) — the retired script cannot be exercised on this host",
			scratchMemoryBaseDefault, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove the setup-route fixture %s: %v", base, err)
		}
	})
	tree := filepath.Join(base, scratchSetupTreeName)

	run := func(t *testing.T, home string) (string, error) {
		t.Helper()
		cmd := exec.Command("bash", script)
		cmd.Env = envForSetupRoutes(base, home)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// The three shapes a caller could present, including the two that used to
	// mutate the host. NONE may change anything now.
	shapes := []struct {
		name string
		home string
	}{
		{"inside-the-tree", filepath.Join(tree, scratchSetupTreeName)},
		{"the-tree-itself", tree},
		{"a-real-directory-outside-the-tree", filepath.Join(base, scratchSetupHomeName)},
	}
	for _, s := range shapes {
		out, err := run(t, s.home)
		if err != nil {
			t.Errorf("%s: the retired script must exit 0, got %v\n%s", s.name, err, out)
		}
		if !strings.Contains(out, "RETIRED") {
			t.Errorf("%s: the script did not report itself retired; output:\n%s", s.name, out)
		}
		if !strings.Contains(out, "nothing was changed") {
			t.Errorf("%s: the script did not state that nothing was changed; output:\n%s", s.name, out)
		}
		// Contract 1, the one that matters: nothing appeared where the home
		// was asked to be. A symlink here would be the deleted behaviour.
		if _, statErr := os.Lstat(s.home); statErr == nil {
			t.Errorf("%s: the retired script created %s — it must change NOTHING "+
				"(no mount, no symlink, no migration). Script output:\n%s", s.name, s.home, out)
		}
	}

	// Contract 2: the self-referential stray must still not exist after every
	// shape was exercised. This is the 2026-09-11 incident the guard existed
	// for, and it outlives the guard.
	nested := filepath.Join(tree, scratchSetupTreeName)
	if _, statErr := os.Lstat(nested); statErr == nil {
		t.Fatalf("%s exists — a scratch home pointed at its own ancestor is a loop no GC can drain", nested)
	}
}

// TestSuiteRootRegistersItsWriter — the writer-side obligation, asserted on the
// REAL suite root the pin created (not a fixture): a per-run scratch root must
// REGISTER its owner at creation time, with all THREE factors (pid, kernel start
// ticks, command line), so a sweep can later prove whether that owner is still the
// process at that pid. Without the registration the root's only bound is the age
// clock — which is exactly the wait this registration removes.
func TestSuiteRootRegistersItsWriter(t *testing.T) {
	root := SuiteTempRoot()
	if root == "" {
		t.Fatal("no suite scratch root is pinned")
	}
	raw, err := os.ReadFile(filepath.Join(root, scratch.HolderFileName))
	if err != nil {
		t.Fatalf("the pinned suite root %s carries no owner record: %v", root, err)
	}
	rec, err := scratch.ParseOwnerRecord(string(raw))
	if err != nil {
		t.Fatalf("owner record unreadable: %v", err)
	}
	if rec.Pid != os.Getpid() {
		t.Fatalf("owner pid = %d, want this process (%d)", rec.Pid, os.Getpid())
	}
	want, err := scratch.ProcStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("read own start ticks: %v", err)
	}
	if rec.Start != want {
		t.Fatalf("owner start ticks = %d, want %d (the incarnation is what makes pid reuse detectable)", rec.Start, want)
	}
	if rec.Cmd == "" {
		t.Fatal("owner record carries no command line — the third identity factor is missing")
	}
	if rec.Version != scratch.OwnerRecordVersion {
		t.Fatalf("owner record version = %d, want %d", rec.Version, scratch.OwnerRecordVersion)
	}
	// And the verdict the sweep will reach: this writer is LIVE, so the root is
	// protected at any age.
	v := scratch.OwnerVerdictFor(root)
	if v.Outcome != procname.OutcomeLive || v.Collectable {
		t.Fatalf("the suite root's own verdict = %s (collectable=%v, %s), want live/keep",
			v.Outcome, v.Collectable, v.Reason)
	}
}
