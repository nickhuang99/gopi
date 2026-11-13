// The EXPLICIT half of the memory-base residue sweep: the families that live in
// the DEFAULT memory base and are refused by the AUTOMATIC arm.
//
// WHY THE TWO ARMS EXIST (the 2026-09-28 measurement this file answers): a
// suite's per-run root is created DIRECTLY in the memory base it was placed on
// — internal/testutil/scratch_placement.go prefers a memory filesystem, and the
// pin then calls os.MkdirTemp(<base>, "suite-<suite>-run-"), producing
// "suite-<pkg>-run-<N>" as a direct child of /dev/shm. Measured on this host:
// 146 such roots, 5.9G of a 7.8G tmpfs (98% used), every sweep in the tree
// blind to them —
//
//   - sweep.go's SweepHome walks the scratch HOME only;
//   - internal/testutil's legacy arm matches "gopi-suite-tmp-" only;
//   - SweepBase matched the gopi-/gocache-/probe- families only.
//
// The family therefore had no collector, and the container filled until a
// suite's first larger write failed as `… is not a git work tree` about a path
// the suite had itself just created (7-8 cmd/agent E2E tests read as a code
// regression).
//
// WHY THE COLLECTION IS EXPLICIT AND NOT RIDDEN BY THE RESIDENT TICK: the
// automatic arm stands DOWN inside a suite (SetAutoBaseResidue, called by
// internal/testutil's TestMain pin) so that a `go test` run can never delete
// the developer's own live base. The suite-root family needs the same
// protection for a stronger reason: the default base is the SHARED /dev/shm and
// it is exactly where running suites put their roots, so an automatic pass over
// it would delete the roots of the suites executing on the same host. The
// operator's own sweep is the mechanism the epic names — `gopi tool scratch-gc base`
// (the existing CLI verb; corrected 2026-10-30, task 01a18ef7: this used to
// add "and the arm the resident worker/merger tick already carries for the
// configured case", and no resident component calls the sweep any more) — and
// it is a one-shot, so it adds no
// resident process, no timer and no script (Rule 35: the reclaim rides the
// machinery that already exists).
//
// THE LIFETIME GUARD: a suite in flight is protected by BOTH channels the sweep
// family already uses — the live /proc pass (the run's shell and test binary
// stand in the root) and the .holder registration internal/testutil writes at
// pin time (pid + kernel start time, readable even where /proc/<pid>/cwd is
// not). The stand-down is therefore bounded, not silent: every root the
// explicit pass SKIPS because a live process holds it is REPORTED by name, so an
// operator re-running the sweep after their suite exits sees the residue go to
// zero rather than wondering whether the sweep works.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// isSuiteRunRoot is the PATH half of the explicit arm's namespace test: the
// candidate is a real directory OR a regular file, and never a symlink.
//
// It deliberately does NOT call IsBaseResiduePath: that helper composes the
// AUTOMATIC namespace (IsBaseResidueName), which stands DOWN inside a suite —
// so a suite-run sweep written through it would find nothing to collect while
// the base filled up, which is the defect this arm exists to fix. The namespace
// here is nameIsDefaultBaseSuiteRun, and only the KIND test is shared.
//
// WHY FILES ARE IN (the same relaxation base_residue.go carries, and for the
// same measured reason): the kind gate is about what this system can leave
// DIRECTLY in the base, and a suite-run name is a name our writer produces — a
// regular file carrying it is residue in the same namespace, and a dirs-only
// gate would put it in the namespace with no collector, which is exactly the
// defect this file's sibling arm was fixed for. A symlink stays refused: its
// target is never this sweep's to remove.
func isSuiteRunRoot(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false // vanished or unreadable: never a delete authorisation (Rule 15)
	}
	return isBaseResidueContainer(fi)
}

// nameIsDefaultBaseSuiteRun reports whether name is a per-run suite root of the
// kind the placement creates directly in the DEFAULT memory base — the family
// the automatic arm refuses to reap (see IsBaseResidueName).
func nameIsDefaultBaseSuiteRun(name string) bool { return suiteRunTokens(name) != nil }

// sweepDefaultBaseSuiteRuns is the explicit arm's deleting face as SweepBase
// calls it: the same predicate, resolved through the ONE base reader, with the
// outcome reported on stdout so an operator sees the two counts the family
// needs ("removed" and "kept").
//
// WHAT IT IS NOT, and this is the 2026-10-01 correction: it is not a leg a
// RESIDENT pass may reach. cmd/agent/scratch_gc_tick.go called this through
// SweepBase on every tick and deleted the SHARED base's suite roots — the family
// base_suite_runs.go reserves for the operator's one-shot — while the comment
// beside the call claimed the resident tick does not delete it. The tick now
// calls BaseSuiteRunReport (nothing removed, counts reported), and SweepBase's
// fold-in below is reserved for callers that may delete.
//
// A failure of this arm is REPORTED and does not stop the sibling arm (Rule
// 15b): the two families are disjoint, and one unreadable base must not turn a
// sweep into a no-op for the family that is still collectable.
func sweepDefaultBaseSuiteRuns(olderThan time.Duration) int {
	removed, kept, err := SweepBaseSuiteRuns(ResolveBase(), olderThan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scratch: base suite-run sweep skipped: %v\n", err)
		return 0
	}
	if removed > 0 || kept > 0 {
		fmt.Printf("scratch-gc role=base-suite-runs removed=%d kept-live=%d stale=%s\n", removed, kept, olderThan)
	}
	return removed
}

// BaseSuiteRunPlan reports the per-run suite roots in base that the EXPLICIT
// sweep would collect, deleting nothing. It shares the ONE collectability
// predicate (BaseResidueCollectable: namespace + age + no live holder) with the
// sweep, so its promise is the sweep's behaviour.
//
// It exists because the automatic plan face refuses this family by construction
// (autoBaseResidueEnabled) — a `--dry-run` that reported nothing while the real
// explicit sweep collected 146 roots would be a lie about the machine.
func BaseSuiteRunPlan(base string, olderThan time.Duration) ([]ScratchPlanEntry, error) {
	if base == "" {
		base = ResolveBase()
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory base %s: %w", base, err)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	holders := ScanHolders()
	plan := make([]ScratchPlanEntry, 0)
	for i, entry := range entries {
		if i >= ScanLimit {
			return plan, fmt.Errorf("memory base %s has more than %d entries — plan stopped early", base, ScanLimit)
		}
		name := entry.Name()
		if !nameIsDefaultBaseSuiteRun(name) {
			continue
		}
		path := filepath.Join(base, name)
		if !staleUnheldDirectory(path, now, olderThan, holders, self) {
			continue
		}
		fi, lerr := os.Lstat(path)
		if lerr != nil {
			continue // vanished between the predicate and the measurement
		}
		plan = append(plan, ScratchPlanEntry{Path: path, Age: now.Sub(fi.ModTime())})
	}
	return plan, nil
}

// BaseSuiteRunReport counts the per-run suite roots a caller must NOT delete
// but must still be told about, deleting nothing at all.
//
// WHY THIS ENTRY POINT EXISTS (measured 2026-10-01, task 01a0f4b7): the
// scratch tick (cmd/agent/scratch_gc_tick.go — then fired by the resident
// worker/merger loops, today by `gopi tool scratch-gc --tick`) called
// SweepBaseSuiteRuns — the EXPLICIT arm — on the DEFAULT, SHARED memory base,
// under a comment claiming the opposite ("The resident tick does NOT delete this
// family"). It deleted it: measured on this host 2026-10-01, 36 `suite-*` roots
// were already older than 30 minutes (29 older than two hours) out of 54, so a
// 60-minute tick with a 6h stale window met a non-empty victim set every time it
// fired. The roots are the ones a host running `go test` is using, and the
// pre-2026-09-28 leak (146 roots / 5.9G of a 7.8G tmpfs) is exactly the state
// that re-forms behind the deletion, hidden. A resident pass has no way to tell
// ANOTHER host-user's live suite from an abandoned one — its own liveness guard
// only protects its own. So the automatic arm gets a REPORT and the deletion
// stays where the design put it: the operator's one-shot `gopi tool scratch-gc base`
// and the placement's reclaim-before-decline (both of which call
// SweepBaseSuiteRuns directly and are unchanged).
//
// The counts come from the SAME deletion predicate SweepBaseSuiteRuns applies —
// isSuiteRunRoot (the symlink refusal), staleUnheldDirectory (age + the holder
// rule) — so the report can never disagree with what the explicit arm would do
// (Rule 14: one predicate, no second policy). The differences are exactly two,
// and both are the point: nothing is removed, and the counts include the FRESH
// roots, because a reader asking "how much of the shared base is this family
// holding" needs the whole family, not only the part past its age. `auto` is
// returned for the caller to render as its own field rather than infer.
//
// Refusals are the explicit arm's, unchanged and never silent (Rule 15): a base
// that cannot be read is an error, a missing base is the expected non-event, and
// a base on the `/` filesystem is refused by measured device id (Rule 22).
func BaseSuiteRunReport(base string, olderThan time.Duration) (auto, kept, fresh int, err error) {
	if base == "" {
		base = ResolveBase()
	}
	entries, rerr := os.ReadDir(base)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return 0, 0, 0, nil
		}
		return 0, 0, 0, fmt.Errorf("read memory base %s: %w", base, rerr)
	}
	sharesRoot, mErr := SharesRootFS(base)
	if mErr != nil {
		return 0, 0, 0, fmt.Errorf("scratch: cannot measure the filesystem of memory base %s — refusing to report on it: %w", base, mErr)
	}
	if sharesRoot {
		return 0, 0, 0, fmt.Errorf("scratch: memory base %s is on the same filesystem as %s — refusing to report on it", base, rootDeviceProbePath)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	for i, entry := range entries {
		if i >= ScanLimit {
			return auto, kept, fresh, fmt.Errorf("memory base %s has more than %d entries — report stopped early", base, ScanLimit)
		}
		name := entry.Name()
		if !nameIsDefaultBaseSuiteRun(name) {
			continue
		}
		path := filepath.Join(base, name)
		if !isSuiteRunRoot(path) {
			continue // not a real directory (a file, or a symlink): never touched
		}
		if !holdersLoaded {
			holders = ScanHolders()
			holdersLoaded = true
		}
		if staleUnheldDirectory(path, now, olderThan, holders, self) {
			auto++
			continue
		}
		// Kept, and the two reasons are reported apart ON PURPOSE: "a suite is
		// running inside it right now" is a different fact from "nobody holds it
		// yet, it just has not aged out", and only the first one explains why the
		// explicit verb would decline too.
		if live, _ := baseSuiteRunHolder(path, holders); live {
			kept++
			continue
		}
		fresh++
	}
	return auto, kept, fresh, nil
}

// SweepBaseSuiteRuns removes the per-run suite roots in base that are older
// than olderThan and held by no live process, returning how many it removed and
// how many it KEPT because a live process holds them.
//
// The CLI verb that calls it MUST honour the returned error (Rule 15): an empty
// error means the family was swept, and a caller that prints success regardless
// turns a refusal into a silent "removed 0".
//
// The kept count is the operator's feedback channel: the family is created by
// suites that may be running right now, so "removed=0, kept=3" is the expected,
// successful answer during a test run — and re-running after the suites exit
// must show them gone. Without the count, that state is indistinguishable from a
// sweep that does not work.
//
// Refusals are reported, never silent (Rule 15), in the same order as SweepBase:
// a base that cannot be read is an error; a missing base is the expected
// non-event; a base on the `/` filesystem is refused by measured device id
// (Rule 22).
func SweepBaseSuiteRuns(base string, olderThan time.Duration) (removed, kept int, err error) {
	if base == "" {
		base = ResolveBase()
	}
	entries, rerr := os.ReadDir(base)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("read memory base %s: %w", base, rerr)
	}
	sharesRoot, mErr := SharesRootFS(base)
	if mErr != nil {
		return 0, 0, fmt.Errorf("scratch: cannot measure the filesystem of memory base %s — refusing to sweep it: %w", base, mErr)
	}
	if sharesRoot {
		return 0, 0, fmt.Errorf("scratch: memory base %s is on the same filesystem as %s — refusing to sweep it", base, rootDeviceProbePath)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	for i, entry := range entries {
		if i >= ScanLimit {
			return removed, kept, fmt.Errorf("memory base %s has more than %d entries — sweep stopped early", base, ScanLimit)
		}
		name := entry.Name()
		if !nameIsDefaultBaseSuiteRun(name) {
			continue
		}
		path := filepath.Join(base, name)
		if !isSuiteRunRoot(path) {
			continue // not a real directory (a file, or a symlink): never touched
		}
		if !holdersLoaded {
			holders = ScanHolders()
			holdersLoaded = true
		}
		if !staleUnheldDirectory(path, now, olderThan, holders, self) {
			if live, why := baseSuiteRunHolder(path, holders); live {
				// Reported, never silent (Rule 15): an operator must be able to
				// tell "still in use" from "the sweep does not work".
				fmt.Fprintf(os.Stderr, "scratch: base suite root %s kept — %s\n", path, why)
				kept++
			}
			continue
		}
		if err := removeCollectable(path); err != nil {
			fmt.Fprintf(os.Stderr, "scratch: base sweep could not remove %s: %v\n", path, err)
			continue
		}
		removed++
	}
	return removed, kept, nil
}

// baseSuiteRunHolder answers "is this suite root still live, and on what
// evidence" — the reporting half of the guard, so the kept line names a reason
// rather than a shrug.
func baseSuiteRunHolder(path string, holders HolderScan) (bool, string) {
	stale, err := ScratchEntryStale(path, time.Now(), 0)
	if err != nil {
		return false, ""
	}
	if !stale {
		return false, "" // fresh: not a holder question at all
	}
	if unheldBatch(path, holders) {
		if alive, holder := HolderAlive(path); alive {
			return true, fmt.Sprintf("the .holder registration names live pid %s", holder)
		}
		return false, ""
	}
	return true, "a live process holds it (/proc cwd/fd)"
}

// staleUnheldDirectory is the collectability predicate WITHOUT the namespace
// test — the namespace is the caller's (each arm has its own), while the age
// and holder halves are shared with BaseResidueCollectable, so the two arms
// cannot drift into different safety rules (Rule 14).
func staleUnheldDirectory(path string, now time.Time, olderThan time.Duration, holders HolderScan, self string) bool {
	stale, err := ScratchEntryStale(path, now, olderThan)
	if err != nil {
		return false // unmeasurable: keep it (Rule 15 — never guess)
	}
	if !stale {
		return false
	}
	return unheldBatch(path, holders) || ownSelfHolder(path, self)
}
