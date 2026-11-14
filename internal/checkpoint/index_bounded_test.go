// index_bounded_test.go — the scan root's entry count is a BOUND, not an
// invitation to walk.
//
// THE DEFECT (measured on this host, peerbox, 2026-10-07). `gopi session
// checkpoint list` with no --dir scans <gopi dir>/sessions, whose first level
// held 83960 entries (83548 of them run directories). IndexTree listed the root
// and then called ReadDir on EVERY child directory — one listing per child, by
// design (a run directory may hold its own co-located raw log). Measured cost on
// this host: 8.7 ms per child listing, so the default walk projects to ~12
// MINUTES, and the command was killed at 30 s with rc=137 and ZERO output:
// real 4m40s / user 0.002s in the operator's report, real 30.0s / user 0.001s in
// this run's reproduction — zero CPU, so the block is I/O, not arithmetic.
//
// WHAT IS PINNED HERE:
//   - reading STOPS at the bound: a root one entry over it is refused, and the
//     read never walks the rest of the root (this is why the refusal is fast and
//     not merely correct),
//   - the boundary is exact — at the bound is NOT over it,
//   - the refusal is STRUCTURED (errors.As reaches *NamedRefusal) and carries the
//     numbers an operator needs (path, entries, limit) so the CLI can add the
//     next step without re-deriving anything,
//   - the scan does not descend AT ALL on the refusal path: no child directory is
//     listed, so the bound is structural and not a wall-clock coincidence,
//   - an ordinary root (well under the bound) still indexes its co-located logs,
//     the case the bound must never touch.
package checkpoint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// boundedRootFixture builds a directory holding n child directories — the shape
// of this host's sessions root (a listing per child, no file to read).
func boundedRootFixture(t *testing.T, parent string, n int) string {
	t.Helper()
	root := filepath.Join(parent, "sessions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("run-%06d", i)), 0o700); err != nil {
			t.Fatalf("mkdir child %d: %v", i, err)
		}
	}
	return root
}

// TestReadEntries_StopsAtTheBound pins the property the whole fix rests on: the
// read that decides the refusal STOPS, so the cost of refusing a huge root does
// not scale with the root's size. `over` is the observable; the returned slice is
// capped at Limit+1 (the caller only needs "over the limit, and by at least
// how much").
func TestReadEntries_StopsAtTheBound(t *testing.T) {
	over := boundedRootFixture(t, t.TempDir(), MaxScanRootEntries+1)
	entries, tooMany, err := readEntries(over)
	if err != nil {
		t.Fatalf("readEntries: %v", err)
	}
	if !tooMany {
		t.Fatalf("a root of %d entries (bound %d) was not reported as over the bound",
			MaxScanRootEntries+1, MaxScanRootEntries)
	}
	if len(entries) != MaxScanRootEntries+1 {
		t.Errorf("readEntries returned %d entries, want exactly limit+1 (%d) — the read must stop at "+
			"the bound, never materialise the whole root", len(entries), MaxScanRootEntries+1)
	}

	// AT the bound is not OVER it — a limit is a limit, and an off-by-one here
	// would refuse a root whose size is exactly the documented maximum.
	at := boundedRootFixture(t, t.TempDir(), MaxScanRootEntries)
	entries, tooMany, err = readEntries(at)
	if err != nil {
		t.Fatalf("readEntries at the bound: %v", err)
	}
	if tooMany {
		t.Errorf("a root of exactly %d entries was reported over the bound", MaxScanRootEntries)
	}
	if len(entries) != MaxScanRootEntries {
		t.Errorf("readEntries at the bound returned %d entries, want all %d",
			len(entries), MaxScanRootEntries)
	}
}

// TestIndexTree_RefusesOversizedRootByName is the defect's green: one entry over
// the bound is a REFUSAL, not a walk.
func TestIndexTree_RefusesOversizedRootByName(t *testing.T) {
	root := boundedRootFixture(t, t.TempDir(), MaxScanRootEntries+1)

	idxs, rep, err := IndexTree(root)
	if err == nil {
		t.Fatalf("IndexTree walked a %d-entry root without refusing — the unbounded scan the "+
			"epic was filed for is still reachable", MaxScanRootEntries+1)
	}
	var refusal *NamedRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("the refusal is not the NAMED, structured one: %v (%T)", err, err)
	}
	if refusal.Path != root {
		t.Errorf("refusal.Path = %q, want the refused root %q", refusal.Path, root)
	}
	// Entries is the bound+1 the read stopped at — a LOWER BOUND on the real
	// count (the fixture holds exactly that many, which is why they agree here).
	if refusal.Entries != MaxScanRootEntries+1 {
		t.Errorf("refusal.Entries = %d, want %d (the count the read stopped at)",
			refusal.Entries, MaxScanRootEntries+1)
	}
	if refusal.Limit != MaxScanRootEntries {
		t.Errorf("refusal.Limit = %d, want %d", refusal.Limit, MaxScanRootEntries)
	}
	if refusal.Code != RefusalScanRootTooLarge {
		t.Errorf("refusal.Code = %q, want %q", refusal.Code, RefusalScanRootTooLarge)
	}
	// Rule 15: the refusal NAMES the path, the numbers and its own reason — a
	// bare "too many entries" would leave the operator to guess the bound.
	msg := refusal.Error()
	for _, want := range []string{root, fmt.Sprintf("%d", MaxScanRootEntries), refusal.Code} {
		if !contains(msg, want) {
			t.Errorf("the refusal text does not carry %q:\n%s", want, msg)
		}
	}
	// NON-BLOCKING, STRUCTURALLY: the scan returns before it queues a single
	// child, so nothing below the root was listed. (A wall-clock assertion would
	// be the flaky half of this proof — the child-listing count is the cause, and
	// the report is its own observable.)
	if len(rep.Roots) != 0 {
		t.Errorf("the refusal path still listed %d director(ies): %v — the bound must be decided "+
			"from the root's own read", len(rep.Roots), reportPaths(rep))
	}
	if idxs != nil {
		t.Errorf("the refusal returned %d index entries; a refusal returns none", len(idxs))
	}
}

// TestIndexTree_OrdinaryRootStillScans is the other direction, at the scale the
// bound must never notice: a root with a handful of run directories is scanned
// exactly as before, co-located log included.
func TestIndexTree_OrdinaryRootStillScans(t *testing.T) {
	parent := t.TempDir()
	root := boundedRootFixture(t, parent, 4)
	const runID = "01a0ed99-0000-4000-8000-0000000000ff"
	want := runDirRawLog(t, filepath.Join(root, runID), runID+sessionRawLogFileSuffix)

	idxs, _, err := IndexTree(root)
	if err != nil {
		t.Fatalf("an ordinary root must scan: %v", err)
	}
	if len(idxs) != 1 || idxs[0].Path != want {
		t.Fatalf("ordinary root indexed %v, want exactly [%s]", pathsOf(idxs), want)
	}
}

// contains is a local substring helper: this file must not import strings for
// one call whose reader is one line above it.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
