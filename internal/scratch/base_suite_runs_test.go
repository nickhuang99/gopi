package scratch

// Tests for the EXPLICIT suite-run arm (base_suite_runs.go): the family the
// placement creates directly in the default memory base, which the AUTOMATIC
// arm refuses by construction. The two halves of the contract tested here are
// (i) the automatic arm leaves the family alone, so a `go test` run can never
// delete the running host's suite roots, and (ii) the explicit arm collects
// exactly the stale, unheld ones — and REPORTS the live ones it keeps.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureStderrLocal runs fn with os.Stderr redirected to a pipe and returns what
// was written (the kept-root report is the deliverable of one case below, so it
// must be READ, not just produced).
func captureStderrLocal(t *testing.T, fn func()) string {
	t.Helper()
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = wr
	defer func() { os.Stderr = saved }()
	fn()
	if err := wr.Close(); err != nil {
		t.Fatalf("close write end: %v", err) // Rule 15b
	}
	buf, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err) // Rule 15b
	}
	if err := rd.Close(); err != nil {
		t.Fatalf("close read end: %v", err) // Rule 15b
	}
	return string(buf)
}

// suiteRunFixtureBase builds a base holding one stale, unheld suite root and
// returns (base, root).
func suiteRunFixtureBase(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "suite-cmd-agent-run-3113361481")
	if err := os.Mkdir(root, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	reAge(t, root, baseResidueStaleAge)
	return base, root
}

// TestAutomaticBaseArmLeavesSuiteRunsAlone is the STAND-DOWN: the automatic arm
// (the predicate the sweep applies when nobody configured a base) must refuse a
// per-run suite root even when it is stale and unheld. Without this, every
// `go test` run and every `gopi task worker` tick over the shared /dev/shm is a
// delete of the host's live suite roots.
func TestAutomaticBaseArmLeavesSuiteRunsAlone(t *testing.T) {
	base, root := suiteRunFixtureBase(t)
	prev := SetAutoBaseResidue(true)
	defer SetAutoBaseResidue(prev)

	// PART 1 — the suite case: the arm stands DOWN inside a suite
	// (internal/testutil's TestMain pin calls SetAutoBaseResidue(false)), and in
	// that state the family is refused by the predicate, the plan and the sweep
	// alike. That is what makes a `go test` run safe on the host whose /dev/shm
	// holds the running suites' roots.
	SetAutoBaseResidue(false)
	if IsBaseResidueName(filepath.Base(root)) {
		t.Fatalf("the standby arm claims %s — a `go test` run would delete the roots of the suites running on "+
			"this host", filepath.Base(root))
	}
	plan, err := BaseResiduePlan(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 0 {
		t.Errorf("the standby plan offers %d suite root(s): %+v — a --dry-run must never promise a delete the "+
			"sweep would refuse", len(plan), plan)
	}
	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 || !exists(t, root) {
		t.Errorf("removed = %d, exists = %v — the standby arm must leave the family alone", removed, exists(t, root))
	}

	// PART 2 — the host case: the worker/merger tick (the arm ON, the operator's
	// live host) DOES treat the family as its namespace. The liveness guard, not
	// the namespace, is what protects the suites a tick must not break — the
	// same guard the other families have always relied on.
	SetAutoBaseResidue(true)
	if !IsBaseResidueName(filepath.Base(root)) {
		t.Fatalf("the armed arm does not claim %s — the resident tick would leave the family unreclaimed", filepath.Base(root))
	}
	if _, err := SweepBase(base, DefaultStaleAge); err != nil {
		t.Fatalf("armed sweep: %v", err)
	}
	if exists(t, root) {
		t.Errorf("%s survived the armed sweep — a stale, unheld suite root must be collected", root)
	}

	// PART 3 — the explicit arm, on its own: same base, same age, same holder
	// rule, and it reports the two counts the family needs.
	again := filepath.Join(base, "suite-cmd-agent-run-3113361482")
	if err := os.Mkdir(again, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", again, err)
	}
	reAge(t, again, baseResidueStaleAge)
	SetAutoBaseResidue(false)
	swept, kept, err := SweepBaseSuiteRuns(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("explicit sweep: %v", err)
	}
	if swept != 1 || kept != 0 {
		t.Fatalf("explicit sweep removed=%d kept=%d, want 1/0", swept, kept)
	}
	if exists(t, again) {
		t.Errorf("%s survived the explicit sweep, want removed", again)
	}
}

// TestExplicitBaseArmKeepsLiveAndFreshSuiteRuns is the hard rule for the new
// family, on the arm that IS allowed to delete: a root a live process holds and
// a root that is still fresh are both KEPT, and the live one is NAMED in the
// report (an operator must be able to tell "in use" from "the sweep is broken").
func TestExplicitBaseArmKeepsLiveAndFreshSuiteRuns(t *testing.T) {
	defer armAutoBaseForFixture(t)()
	base := t.TempDir()
	fresh := filepath.Join(base, "suite-agent-run-1234567890")
	held := filepath.Join(base, "suite-tools-run-9876543210")
	for _, dir := range []string{fresh, held} {
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	reAge(t, held, baseResidueStaleAge)
	handle, err := os.Open(held)
	if err != nil {
		t.Fatalf("open %s: %v", held, err)
	}
	defer func() { _ = handle.Close() }() //stylegate:ignore test fixture handle; the verdict is taken while it is open

	report := captureStderrLocal(t, func() {
		removed, kept, serr := SweepBaseSuiteRuns(base, DefaultStaleAge)
		if serr != nil {
			t.Fatalf("explicit sweep: %v", serr)
		}
		if removed != 0 {
			t.Errorf("removed = %d, want 0 (one fresh, one held)", removed)
		}
		if kept != 1 {
			t.Errorf("kept = %d, want 1 (the held root; the fresh one is not a holder question)", kept)
		}
	})
	for _, dir := range []string{fresh, held} {
		if !exists(t, dir) {
			t.Errorf("%s was removed — age and the holder rule must apply unchanged", dir)
		}
	}
	if !strings.Contains(report, held) {
		t.Errorf("the kept root is not named in the report:\n%s", report)
	}
}

// TestExplicitBaseArmRefusesABaseThatIsNotTheDefault: the explicit arm is
// reached from SweepBase only for the DEFAULT base (a caller-configured base
// keeps the automatic arm). Its own safety gates still apply: a base on the `/`
// filesystem is refused by measured device id, never swept.
func TestExplicitBaseArmRefusesABaseThatIsNotTheDefault(t *testing.T) {
	defer armAutoBaseForFixture(t)()
	base, root := suiteRunFixtureBase(t)
	deviceBase := t.TempDir() // on the /home device here; the SHARES check is per host
	if err := os.MkdirAll(filepath.Join(deviceBase, filepath.Base(root)), DirPerm); err != nil {
		t.Fatalf("seed fixture base: %v", err)
	}
	reAge(t, filepath.Join(deviceBase, filepath.Base(root)), baseResidueStaleAge)

	shares, err := SharesRootFS(deviceBase)
	if err != nil {
		t.Fatalf("measure %s: %v", deviceBase, err)
	}
	removed, _, err := SweepBaseSuiteRuns(deviceBase, DefaultStaleAge)
	if shares {
		if err == nil {
			t.Fatalf("a base on the / filesystem must be refused (removed=%d)", removed)
		}
		if !strings.Contains(err.Error(), "same filesystem") {
			t.Errorf("the refusal does not name the reason: %v", err)
		}
	} else if err != nil {
		t.Errorf("a legal base must be sweepable: %v", err)
	}
	// A missing base is the expected non-event, not an error.
	removed, kept, err := SweepBaseSuiteRuns(filepath.Join(base, "does-not-exist"), DefaultStaleAge)
	if err != nil || removed != 0 || kept != 0 {
		t.Errorf("missing base: removed=%d kept=%d err=%v, want 0/0/nil", removed, kept, err)
	}
	_ = time.Now()
}

// TestBaseSuiteRunReportAgreesWithTheExplicitArm is the AGREEMENT half of the
// report-only entry point: the counts the resident tick is handed must be
// produced by the SAME predicate the explicit sweep applies, or the report
// becomes a second policy that can drift from the deletion it describes.
//
// The fixture holds one root of every verdict the family can have — stale and
// unheld (what the explicit arm would remove), held by a live process (kept),
// fresh (not yet a candidate), a symlink in the same namespace (refused as a
// KIND, never as a count), and a foreign name (not this system's at all) — and
// the test drives BOTH faces on it.
func TestBaseSuiteRunReportAgreesWithTheExplicitArm(t *testing.T) {
	defer armAutoBaseForFixture(t)()
	base := t.TempDir()

	auto := filepath.Join(base, "suite-cmd-agent-run-3113361481")
	held := filepath.Join(base, "suite-tools-run-9876543210")
	fresh := filepath.Join(base, "suite-internal-tools-run-1234567890")
	for _, dir := range []string{auto, held, fresh} {
		if err := os.Mkdir(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	reAge(t, auto, baseResidueStaleAge)
	reAge(t, held, baseResidueStaleAge)
	// A live holder: this process's own open descriptor inside the root.
	handle, err := os.Open(held)
	if err != nil {
		t.Fatalf("open %s: %v", held, err)
	}
	defer func() { _ = handle.Close() }() //stylegate:ignore test fixture handle; the verdict is taken while it is open
	// A SYMLINK in the namespace, and a foreign name beside it.
	link := filepath.Join(base, "suite-cmd-agent-run-4444444444")
	if err := os.Symlink(auto, link); err != nil {
		t.Fatalf("symlink %s: %v", link, err)
	}
	foreign := filepath.Join(base, "somebody-elses-tree-3113361481")
	if err := os.Mkdir(foreign, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", foreign, err)
	}
	reAge(t, foreign, baseResidueStaleAge)

	// THE REPORT: nothing is removed, and every count is taken.
	gotAuto, gotKept, gotFresh, err := BaseSuiteRunReport(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if gotAuto != 1 || gotKept != 1 || gotFresh != 1 {
		t.Fatalf("report = (auto=%d kept=%d fresh=%d), want (1,1,1)", gotAuto, gotKept, gotFresh)
	}
	for _, dir := range []string{auto, held, fresh, foreign} {
		if !exists(t, dir) {
			t.Errorf("%s was removed by the REPORT face — it must delete nothing at all", dir)
		}
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the report removed the symlink %s: %v", link, err)
	}

	// THE AGREEMENT: the explicit arm removes exactly the root the report
	// counted as collectable — no more, no fewer.
	removed, kept, err := SweepBaseSuiteRuns(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("explicit sweep: %v", err)
	}
	if removed != gotAuto || kept != gotKept {
		t.Fatalf("explicit sweep = (removed=%d kept=%d), the report said (auto=%d kept=%d) — the report and the "+
			"deletion must be the same predicate", removed, kept, gotAuto, gotKept)
	}
	if exists(t, auto) {
		t.Errorf("%s survived the explicit sweep, want removed", auto)
	}
	for _, dir := range []string{held, fresh, foreign} {
		if !exists(t, dir) {
			t.Errorf("%s was removed — age, holder and namespace rules must apply unchanged", dir)
		}
	}

	// A MISSING base is the expected non-event for the report too (the same
	// contract the explicit arm carries), and never an error.
	missingAuto, missingKept, missingFresh, err := BaseSuiteRunReport(filepath.Join(base, "does-not-exist"), DefaultStaleAge)
	if err != nil || missingAuto != 0 || missingKept != 0 || missingFresh != 0 {
		t.Errorf("missing base: (%d,%d,%d) err=%v, want (0,0,0)/nil", missingAuto, missingKept, missingFresh, err)
	}
}
