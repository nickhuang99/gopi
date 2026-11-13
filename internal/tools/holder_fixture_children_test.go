package tools

// The holder CHILD BODIES for this package's fixture selectors (reproduction of
// the defect measured 2026-10-31 on clean master, row 01a18f1e).
//
// THE DEFECT: `TestReclaimSelfOwnKeepsLiveChildHeldRegistration` asserts that a
// live child of the asker standing in a registration KEEPS it, and measured ""
// (the registration was released). The child was never alive: `daemonCwdHolder`
// re-executes this test binary with `-test.run=TestHelperDaemonWorktreeHolder`,
// and that function had been deleted with the finish-cleanup retirement
// (58b5618b, slice 3/4) while the SELECTOR spelling survived in the restore of
// its sibling helpers (0e9fdbca). A selector that names nothing does not fail —
// the Go test binary prints "testing: warning: no tests to run", exits 0
// instantly, and the fixture then measures a DEAD child:
//
//	$ ./tools.test -test.run=TestHelperDaemonWorktreeHolder -test.v
//	PASS
//	RC=0
//	[stderr] testing: warning: no tests to run
//
// So the positive control could not hold, and the pass it is supposed to
// constrain ("an orphaned cwd-inheritor is not a holder") was free to widen
// without bound. Measured in the same probe: the child's /proc/<pid> entry was
// gone on the second reading, one second after the fixture returned.
//
// WHY THESE LIVE HERE (Rule 43 — the reference distribution is the criterion):
// the SELECTOR spellings in finish_cleanup_moved_helpers_test.go are still named
// by live tests, so what has to be restored is what those selectors POINT AT.
// The bodies are carried verbatim from 58b5618b^, where each was a real test
// body whose only job is to hold a CWD (or, for the probe, to exit at once).
//
// The four selectors and their child bodies, one line each so a reader can check
// the map without opening anything else:
//
//	-test.run=TestHelperWorktreeCwdHolder   holderEnvMarker
//	-test.run=TestHelperDaemonWorktreeHolder daemonHolderEnvMarker
//	-test.run=TestHelperOrphanCwdHolder     orphanHolderEnvMarker
//	-test.run=TestHelperNeverRuns           orphanHolderEnvMarker (exits at once)
//
// The regression guard that keeps this class of defect from returning is
// TestHolderFixtureSelectorsNameADefinedTest below: it reads THIS package's test
// sources and fails when a selector names a function no file defines. A gate
// over the selector's SPELLING cannot see this defect (the spelling was fine);
// what was wrong is that the name resolved to nothing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHelperWorktreeCwdHolder is NOT a test of its own: re-exec'd by
// liveCwdHolder with holderEnvMarker set, it blocks so the process keeps the
// worktree CWD (the live-session shape the guard protects). Without the marker
// it skips instantly — a normal `go test` run is unaffected.
func TestHelperWorktreeCwdHolder(t *testing.T) {
	if os.Getenv(holderEnvMarker) == "" {
		t.Skip("holder helper — only runs as the re-exec'd child a liveness test starts")
	}
	time.Sleep(procHolderHoldTime) //stylegate:allow-sleep input to the liveness observation itself — the holder must stay alive and the log stay quiet
}

// TestHelperDaemonWorktreeHolder is the DAEMON-shaped holder (epic
// daemon-cwd-escape): same body as the session holder, but the child is started
// with an overridden argv[0] so it is recognised as a DETACHED GOPI DAEMON —
// the immortal holder the reclaim report must name instead of treating it as a
// task session that will eventually leave.
func TestHelperDaemonWorktreeHolder(t *testing.T) {
	if os.Getenv(daemonHolderEnvMarker) == "" {
		t.Skip("daemon holder helper — only runs as the re-exec'd child of the recovery-report test")
	}
	time.Sleep(procHolderHoldTime) //stylegate:allow-sleep input to the liveness observation itself — the holder must stay alive and the log stay quiet
}

// TestHelperOrphanCwdHolder is the RE-EXEC'D CHILD body: it runs only when the
// marker is set, and it holds the cwd its launcher chose.
//
// It does NOT use t.TempDir() (there is no *testing.T lifecycle here — the
// helper outlives its own test function) and it does not read stdin: `go test`
// closes that descriptor, so a stdin-held holder would exit immediately. It
// sleeps instead, bounded by the same named budget the suite's other cwd holder
// uses, and the tests kill it as soon as they are done — so a passing run
// leaves no fixture behind (the defect this file is about would otherwise be
// committed by its own test).
func TestHelperOrphanCwdHolder(t *testing.T) {
	if os.Getenv(orphanHolderEnvMarker) == "" {
		t.Skip("orphan cwd-holder helper — only runs as the re-exec'd child of the orphan fixture")
	}
	time.Sleep(procHolderHoldTime) //stylegate:allow-sleep input to the liveness observation itself — the holder must stay alive while the guard is read
}

// TestHelperNeverRuns is the fixture's dead-pid probe: it is never selected, so
// the probe process exits immediately and its pid is then a MEASURED dead pid
// (started, waited, confirmed dead) rather than an assumed one.
func TestHelperNeverRuns(t *testing.T) {
	if os.Getenv(orphanHolderEnvMarker) == "" {
		t.Skip("dead-pid probe — selected only to be started and reaped")
	}
}

const (
	// testRunSelectorPrefix is the flag whose value decides which test a
	// re-executed test binary runs (Rule 1/13: named once, referenced below).
	// It is this package's own definition — internal/testutil's is unexported,
	// so importing it would mean exporting a fixture detail to production code.
	testRunSelectorPrefix = "-test.run="

	// holderSelectorSourceDir is where this package's test bodies live; the
	// guard below reads the files beside itself, never a build artifact.
	holderSelectorSourceDir = "."

	// holderSelectorTestFileSuffix narrows that read to the files that can
	// DEFINE a test body — a non-test file cannot, so scanning one would be a
	// scan that can never find what it is looking for.
	holderSelectorTestFileSuffix = "_test.go"

	// holderSelectorFuncPrefix is how a Go test body is declared. The guard
	// looks for this prefix followed by the selector's own value and "(", so a
	// comment or a string naming the test cannot satisfy it.
	holderSelectorFuncPrefix = "func "
)

// reclaimSelfOwnHolderSelectors is the map the guard walks: every selector this
// package hands a re-executed child, with the marker that dispatches its body.
// It is a TABLE OF DATA on purpose — a new fixture selector that is added here
// and points at nothing turns the guard red, which is the only way a deleted
// child body gets noticed before a control silently stops controlling.
var reclaimSelfOwnHolderSelectors = map[string]string{
	holderChildArgRun:       holderEnvMarker,
	daemonHolderChildArgRun: daemonHolderEnvMarker,
	orphanHolderRunArg:      orphanHolderEnvMarker,
	orphanHolderProbeRunArg: orphanHolderEnvMarker,
}

// TestHolderFixtureSelectorsNameADefinedTest is the regression guard for the
// measured defect: a selector naming a test that no longer exists makes the
// child exit at once ("testing: warning: no tests to run", exit 0), so every
// fixture built on it measures a DEAD process while its assertions still read
// as real measurements.
//
// A spelling gate cannot catch this — the spelling was correct throughout. What
// is checked is RESOLUTION: the selector's value must appear as a declared test
// body in this package's own sources.
func TestHolderFixtureSelectorsNameADefinedTest(t *testing.T) {
	sources, files := holderSelectorTestSources(t)
	for selector, marker := range reclaimSelfOwnHolderSelectors {
		name := strings.TrimPrefix(selector, testRunSelectorPrefix)
		if name == selector || name == "" {
			t.Errorf("selector %q does not carry the %q prefix — the guard cannot resolve it, and an unresolved selector is never a pass", selector, testRunSelectorPrefix)
			continue
		}
		if !strings.Contains(sources, holderSelectorFuncPrefix+name+"(") {
			t.Errorf("selector %q (marker %s) names a test body this package does not define — the re-executed child prints \"no tests to run\", exits 0 at once, and the fixture then measures a DEAD process while its assertions still read as real measurements (measured 2026-10-31: a selector naming %q, resolved against the %d test source(s) beside this one)",
				selector, marker, name, len(files))
		}
	}
	if len(reclaimSelfOwnHolderSelectors) == 0 {
		t.Fatal("the selector table is empty — a guard over nothing is not a green, it is a scan that never ran")
	}
	t.Logf("holder selector guard: %d selector(s) resolved against %d test source(s)", len(reclaimSelfOwnHolderSelectors), len(files))
}

// holderSelectorTestSources concatenates this package's own test sources and
// returns the file names it read. A directory that cannot be read is a FAILURE,
// never a skip: the guard's subject is exactly the thing it could not look at
// (Rule 15 — a scan that read nothing must never read as clean).
func holderSelectorTestSources(t *testing.T) (string, []string) {
	t.Helper()
	entries, err := os.ReadDir(holderSelectorSourceDir)
	if err != nil {
		t.Fatalf("cannot read this package's source directory (%v) — the selector guard would be a green over nothing", err)
	}
	var b strings.Builder
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, holderSelectorTestFileSuffix) {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(holderSelectorSourceDir, name))
		if rerr != nil {
			t.Fatalf("cannot read %s (%v) — an unread file is an unmeasured selector", name, rerr)
		}
		b.Write(raw)
		b.WriteString("\n")
		files = append(files, name)
	}
	if len(files) == 0 {
		t.Fatalf("no %s file found beside this test — the guard read nothing", holderSelectorTestFileSuffix)
	}
	return b.String(), files
}
