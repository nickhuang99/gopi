// The bound on the /proc holder scan, and the ONE thing that matters about a
// truncated scan: it is not evidence of absence.
//
// WHY THESE TESTS DRIVE AN EXPIRED DEADLINE INSTEAD OF LOAD (Rule 27):
// truncation is a property of the CODE PATH, not of the host. Manufacturing load
// to reach it would make the assertion depend on the machine, and the machine
// would be the thing under measurement — so the truncating case is driven by
// ScanHoldersWithin with a deadline that has already expired, which is the same
// branch a loaded host takes, reached deterministically.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHolderScanDeadlineZeroIsUnboundedAndComplete pins the NEGATIVE direction:
// budget == 0 means NO bound, and the scan must then look at the whole process
// table — the property that keeps the bound from being widened into a silently
// softened check (a bounded scan that quietly stops early on a healthy host
// would be a deletion authorisation nobody measured).
func TestHolderScanDeadlineZeroIsUnboundedAndComplete(t *testing.T) {
	unbounded := ScanHoldersWithin(0)
	if unbounded.Truncated {
		t.Fatalf("budget 0 (the deliberate unbounded form) truncated the scan — the unbounded form must look at every process")
	}
	if len(unbounded.Paths) == 0 {
		t.Fatalf("the unbounded scan returned no holder paths at all — it did not read /proc, so this test pinned nothing")
	}
	if unbounded.Elapsed <= 0 {
		t.Fatalf("the unbounded scan reported no elapsed time")
	}

	// The DEFAULT bound is the production shape of the same call, and it must
	// also complete here: a default that truncates a healthy host would turn
	// every sweep on this machine into a keep-everything pass with no symptom
	// other than unbounded disk usage. Direct measurement of the named call,
	// never generated load (Rule 27).
	bounded := ScanHoldersWithin(HolderScanDefaultBudget)
	if bounded.Truncated {
		t.Fatalf("the default bound (%s) truncated a scan that took %s — the default must sit above any healthy host",
			HolderScanDefaultBudget, bounded.Elapsed)
	}
	if len(bounded.Paths) == 0 {
		t.Fatalf("the bounded scan returned no holder paths")
	}
}

// TestHolderScanExpiredDeadlineTruncates: the bound is REAL — a deadline that
// has already expired stops the pass BEFORE the process table is walked, and it
// says so. It also proves the truncation is disclosed rather than inferred:
// Elapsed is measured, Truncated is set, and the paths collected so far are
// still returned (a partial scan is partial, never empty-by-convention).
func TestHolderScanExpiredDeadlineTruncates(t *testing.T) {
	// A budget this small cannot be met by any real scan: the check runs before
	// the first PID is opened, so the pass stops with nothing collected.
	past := ScanHoldersWithin(time.Nanosecond)
	if !past.Truncated {
		t.Fatalf("a scan bounded by 1ns reported itself complete — the bound is not being applied")
	}
	if len(past.Paths) != 0 {
		t.Fatalf("a scan that expired before opening the first process collected %d paths", len(past.Paths))
	}
	if past.Complete() {
		t.Fatalf("Complete() disagrees with Truncated")
	}
	if _, ok := past.HeldByProcesses("/anywhere"); ok {
		t.Fatalf("a truncated scan answered a holder question — an unmeasured fact must not read as a measured negative")
	}
	if why := past.HeldByProcesses; why == nil {
		t.Fatalf("sanity: the method value must exist")
	}
	if !strings.Contains(past.Disclosure(), "TRUNCATED") {
		t.Fatalf("the disclosure does not say the scan was truncated: %s", past.Disclosure())
	}
	if !strings.Contains(ScanHoldersWithin(0).Disclosure(), "complete") {
		t.Fatalf("a completed scan does not disclose that it completed: %s", ScanHoldersWithin(0).Disclosure())
	}
}

// TestHolderScanDeadlineReadsTheOneEnvReader pins the knob's contract: the
// default is the named constant, "0" is the deliberate unbounded form, a
// positive value is seconds, and an UNPARSABLE value falls back to the default
// rather than silently becoming "no bound" (Rule 2: exactly one reader).
func TestHolderScanDeadlineReadsTheOneEnvReader(t *testing.T) {
	t.Setenv(HolderScanDeadlineEnvName, "")
	if got := HolderScanDeadline(); got != HolderScanDefaultBudget {
		t.Fatalf("unset %s: got %s, want the default %s", HolderScanDeadlineEnvName, got, HolderScanDefaultBudget)
	}
	t.Setenv(HolderScanDeadlineEnvName, "0")
	if got := HolderScanDeadline(); got != 0 {
		t.Fatalf("`0` must mean UNBOUNDED (0), got %s", got)
	}
	t.Setenv(HolderScanDeadlineEnvName, "3")
	if got := HolderScanDeadline(); got != 3*time.Second {
		t.Fatalf("`3` must mean 3s, got %s", got)
	}
	t.Setenv(HolderScanDeadlineEnvName, "not-a-number")
	if got := HolderScanDeadline(); got != HolderScanDefaultBudget {
		t.Fatalf("an unparsable value must fall back to the default %s (a silent unbounded fallback is the defect the knob closes), got %s",
			HolderScanDefaultBudget, got)
	}
}

// TestTruncatedHolderScanNeverAuthorisesADelete is the epic's second acceptance
// clause, on the REAL delete path: the SAME fixture is judged twice, and the
// only difference is whether the scan finished.
//
//	complete scan  → stale + unheld → COLLECTABLE (the fixture is provably a
//	                 delete candidate, so the second half cannot pass vacuously)
//	truncated scan → KEPT, with a reason that discloses the truncation and does
//	                 NOT claim a process was seen holding it
//
// The reason text is asserted, not just the verdict: a caller that renders
// "a live process holds it" for an unscanned directory sends an operator
// hunting a process that was never observed (Rule 15).
func TestTruncatedHolderScanNeverAuthorisesADelete(t *testing.T) {
	base := t.TempDir()
	name := "gopi-red2"
	now := time.Now()

	dir := filepath.Join(base, name)
	if err := os.Mkdir(dir, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	reAge(t, dir, baseResidueStaleAge)

	// ① A COMPLETE scan that found nobody: collectable. This is the control that
	// makes the truncated half meaningful.
	complete := HolderScan{}
	v := BaseResidueVerdictFor(dir, name, now, DefaultStaleAge, complete, "")
	if !v.Collectable {
		t.Fatalf("the control failed: a stale unheld directory is not collectable (%s) — this test would pass vacuously", v.Reason)
	}

	// ② The SAME directory, judged with a scan that STOPPED EARLY: kept.
	truncated := HolderScan{Truncated: true}
	kept := BaseResidueVerdictFor(dir, name, now, DefaultStaleAge, truncated, "")
	if kept.Collectable {
		t.Fatalf("a TRUNCATED holder scan authorised a delete — 'not scanned' was read as 'not held', which is the fail-open direction this epic closes")
	}
	if !strings.Contains(kept.Reason, HolderScanWithheldReason()) {
		t.Fatalf("the keep reason does not disclose the truncation: %q", kept.Reason)
	}
	if strings.Contains(kept.Reason, EntryKeepLiveHolder) {
		t.Fatalf("the keep reason CLAIMS a live holder for a directory nothing was observed holding: %q", kept.Reason)
	}

	// ③ The shared delete predicate, directly: same two answers.
	if !unheldBatch(dir, complete) {
		t.Fatalf("the control predicate says the stale unheld directory is held")
	}
	if unheldBatch(dir, truncated) {
		t.Fatalf("unheldBatch returned TRUE for a truncated scan — the shared delete predicate is fail-open")
	}

	// ④ The worktree-stub arm, which is a delete path and has its own sentence.
	held, why := worktreeStubHeld(dir, truncated)
	if !held {
		t.Fatalf("a truncated scan did not veto the stub arm — a stub whose holder was never looked for would be collected")
	}
	if !strings.Contains(why, HolderScanWithheldReason()) {
		t.Fatalf("the stub arm's veto does not disclose the truncation: %q", why)
	}

	// ⑤ The exported veto that the internal/tools arms call: a measured holder
	//    and an unfinished scan must NOT render the same sentence.
	if got := HolderVeto(dir, HolderScan{Paths: []string{filepath.Join(dir, "sub")}}); got != HolderVetoReason {
		t.Fatalf("a measured holder rendered %q, want %q", got, HolderVetoReason)
	}
	if got := HolderVeto(dir, truncated); got != HolderScanWithheldReason() {
		t.Fatalf("an unfinished scan rendered %q, want the truncation disclosure", got)
	}
	if HolderVeto(dir, complete) != "" {
		t.Fatalf("a complete scan that found nobody must not veto")
	}
}

// TestEntryVerdictDoesNotDressTruncationAsAHolder pins the shared namespace
// verdict's sentence: the truncation disclosure must NOT be rendered behind the
// EntryKeepLiveHolder prefix ("a live process holds it (…)"), because that
// prefix asserts a sighting the scan never made (Rule 15). The two facts must
// read differently, and this is the one place a caller could still merge them.
func TestEntryVerdictDoesNotDressTruncationAsAHolder(t *testing.T) {
	dir := t.TempDir()
	v := entryVerdictInNamespace(dir, time.Now(), DefaultStaleAge, true, HolderScanWithheldReason(), nil)
	if !strings.Contains(v.Reason, HolderScanWithheldReason()) {
		t.Fatalf("the truncation veto did not reach the verdict's reason: %q", v.Reason)
	}
	if strings.Contains(v.Reason, EntryKeepLiveHolder) {
		t.Fatalf("the verdict renders a truncated scan as %q — a holder nobody observed", EntryKeepLiveHolder)
	}
	// The measured-holder arm keeps its prefix + channel, unchanged.
	seen := entryVerdictInNamespace(dir, time.Now(), DefaultStaleAge, true, HolderVetoReason, nil)
	if !strings.Contains(seen.Reason, EntryKeepLiveHolder) {
		t.Fatalf("a MEASURED holder lost its sentence: %q", seen.Reason)
	}
}

// TestHolderScanTruncationKeepsTheProfileArm: the profile-root residue arm is a
// delete path with its own report line, and it must disclose the truncation for
// the same reason — its sentence is what an operator reads when a child "should"
// have gone.
func TestHolderScanTruncationKeepsTheProfileArm(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	name := "evid-01a0e562" // the measured residue family (ProfileResidueName's own table)
	if !ProfileResidueName(name) {
		t.Fatalf("fixture name %q is not in the residue namespace — this test would pin nothing", name)
	}
	child := filepath.Join(root, name)
	if err := os.Mkdir(child, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", child, err)
	}
	reAge(t, child, DefaultStaleAge+time.Hour)

	if profileResidueCollectable(now, child, name, DefaultStaleAge, HolderScan{}) != true {
		t.Fatalf("the control failed: a stale unheld residue child is not collectable — vacuous test")
	}
	if profileResidueCollectable(now, child, name, DefaultStaleAge, HolderScan{Truncated: true}) {
		t.Fatalf("a truncated scan made a profile residue child collectable")
	}
	reason := profileResidueReason(child, now, DefaultStaleAge, HolderScan{Truncated: true}, false)
	if !strings.Contains(reason, HolderScanWithheldReason()) {
		t.Fatalf("the profile arm's keep reason does not disclose the truncation: %q", reason)
	}
}
