// Package heavytest classifies heavy tests and keeps them OFF the default
// (smoke) run — the operator's convention of 2026-09-16: *"some of testsuite
// cannot be run every time, especially pressure/resource-heavy cases; normal
// merging verification or bugfixing usually runs smoke testing only."*
//
// WHY A LEAF PACKAGE (and not internal/testutil)
// ---------------------------------------------
// testutil is where test support normally lives, and it is unusable here:
// internal/testutil imports internal/scratch, so an in-package `scratch` test
// (or `shot`, `tools`, `agent`, …) that imported testutil would form the cycle
// pkg(test) → testutil → scratch. Every heavy test in those packages has to
// reach the opt-in, so the helper must be importable from in-package tests —
// hence a leaf package with ZERO project imports (PROJECT.md Rule 14: a small
// exported-constant package every consumer can import).
//
// WHAT IT DOES
// ------------
//   - a test declares itself heavy with Require(t, criterion) as its FIRST
//     statement (after fixture setup it would be too late to be cheap anyway);
//   - heaviness is OFF unless GOPI_HEAVY_TESTS is truthy — the ONLY reader of
//     that name (Rule 2: one reader per knob), so plain `go test ./...` and the
//     merge/verify path are smoke-only by construction;
//   - a skip is never silent: it logs one greppable line and appends it to the
//     run's ledger when GOPI_HEAVY_SKIP_LEDGER names a file, so what a smoke run
//     did NOT run stays enumerable (a skip nobody can enumerate is a deleted
//     test wearing a passing green);
//   - the checked-in inventory (testdata/heavy-tests.tsv) lists every heavy
//     call site, and internal/standards gates that list against the code, so
//     the classification cannot silently drift.
package heavytest

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nick/gopi/internal/boolvocab"
)

const (
	// EnvName is the ONE named opt-in for heavy tests. Read by Enabled only
	// (Rule 2/21); `make test-heavy` sets it.
	EnvName = "GOPI_HEAVY_TESTS"

	// SkipLedgerEnv optionally names a file that receives one line per skipped
	// heavy test (`Test<Name>\t<criterion>`). The Makefile's smoke target sets
	// it, so a smoke run leaves a reviewable artifact behind.
	SkipLedgerEnv = "GOPI_HEAVY_SKIP_LEDGER"

	// skipLinePrefix marks a heavy skip in test output. Deliberately distinct
	// from an ordinary t.Skip: this repo treats a reason-less skip as a defect,
	// and a classified heavy skip is a reviewed, enumerated decision.
	skipLinePrefix = "HEAVY TEST SKIPPED (set " + EnvName + "=1 to run): "

	// SkipMarkerPrefix is the greppable text a skipped heavy test logs, exported
	// so a consumer that reads a run's output (the branch-scoped smoke gate
	// parsing the `go test -json` stream) matches the SAME text the helper
	// writes instead of re-spelling it (Rule 14: one definition per shared value).
	SkipMarkerPrefix = skipLinePrefix

	// InventoryRel is the checked-in inventory, relative to the repo root.
	InventoryRel = "testdata/heavy-tests.tsv"

	// InventoryHeader heads the inventory file: a TSV with one row per heavy
	// call site, so the file is both human-reviewable and machine-checkable.
	InventoryHeader = "# test\tcriterion\tsource"
)

// Criterion constants (Rule 1b/14): the reason a test is heavy comes from here,
// never from a bare literal at the call site, so the inventory's per-entry
// criterion is one of a small reviewed set. Each mirrors the brief's criterion —
// "would a reasonable person not want this on a shared machine while other work
// proceeds, because it takes minutes of real device work, spawns wide
// parallelism, or saturates a resource".
const (
	// CriterionDiskFixture — minutes of real per-test disk cost: the ledger's
	// first-open schema commit, scratch-tree setup, per-test SQLite scratch DBs.
	CriterionDiskFixture = "disk-fixture: minutes of real device work (ledger/scratch fsync per fixture)"
	// CriterionWideParallel — spawns wide parallelism (N concurrent sweeps or
	// worker processes over one tree) and contends for the host.
	CriterionWideParallel = "wide-parallel: spawns concurrent processes sweeping the same tree"
	// CriterionDeviceExec — drives a real external process end to end (headless
	// chromium capture, daemon/worker spawn, git/build). Cost is process+jail
	// setup, not the assertion.
	CriterionDeviceExec = "device-exec: drives a real external process (browser/daemon/git) end to end"
	// CriterionHostCondition — the verdict depends on ambient host state (wall
	// clock budget, sudo availability, current load) rather than on the code.
	CriterionHostCondition = "host-condition: verdict depends on ambient host state (wall clock/sudo/load)"
	// CriterionE2ENetwork — an end-to-end contract whose cost is dominated by
	// host/network I/O, not by the assertion.
	CriterionE2ENetwork = "e2e-io: end-to-end contract cost dominated by host/network I/O"
)

// ledgerMu serialises the skip-ledger append: tests in one package may skip
// concurrently (t.Parallel) and interleaved appends would corrupt a line.
var ledgerMu sync.Mutex

// Enabled reports whether heavy tests may run; the single reader of EnvName.
//
// The truthy set comes from the ONE vocabulary definition
// (internal/boolvocab, PROJECT.md Rule 14) — read, never mirrored. This package
// IS the reason that definition is a LEAF: heavytest must not import
// internal/config (config → internal/scratch, and a scratch in-package test
// imports this package), so as long as the only definition lived in config,
// every leaf consumer had to re-spell the list. Only truthy values enable, so
// a canonical "off"/"false" written by a wrapper stays disabled — the
// regression config.EnvBool's doc records (a non-empty check treats "off" as
// enabled).
func Enabled() bool {
	return boolvocab.IsTrue(os.Getenv(EnvName))
}

// Require declares t heavy and skips it unless the heavy opt-in is set. Call it
// as the first statement of the test body.
//
// criterion is one of the Criterion* constants (never a bare literal): the
// static inventory gate (internal/standards) reads the call site's argument
// identifier to render the inventory's per-entry criterion, and a free-form
// string there is exactly the drift Rule 1b bans.
func Require(t *testing.T, criterion string) {
	t.Helper()
	if Enabled() {
		// BOUNDARY (boundary.go): the opt-in alone does not entitle this
		// process. A WORK context (an agent session above it, a task child)
		// that merely INHERITED the switch gets a LOUD refusal and the test
		// still skips — the default stays smoke-only.
		reason, _, declared := WorkRefusal()
		if reason != "" {
			refuseHeavyRun(t, criterion, reason)
			return
		}
		if declared {
			// The operator's own declaration was required to get here: say so,
			// so an override is never silent (Rule 15).
			t.Logf("%s", declarationLine)
		}
		return
	}
	line := t.Name() + "\t" + criterion
	recordSkip(t, line)
	t.Skip(skipLinePrefix + criterion)
}

// recordSkip prints the skip and appends it to the run's ledger. The ledger is
// best-effort — an unwritable path must never turn a smoke run red — but a
// failure is REPORTED rather than swallowed (Rule 15b).
func recordSkip(t *testing.T, line string) {
	t.Helper()
	t.Logf("%s%s", skipLinePrefix, line)
	path := strings.TrimSpace(os.Getenv(SkipLedgerEnv))
	if path == "" {
		return
	}
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("heavy skip ledger: cannot create %s: %v", filepath.Dir(path), err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Logf("heavy skip ledger: cannot open %s: %v", path, err)
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Logf("heavy skip ledger: close %s: %v", path, cerr)
		}
	}()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Logf("heavy skip ledger: write %s: %v", path, err)
	}
}
