package tools

// COST CONTRACT for the sessions-tree scan (epic: re-land the address-based
// session-log resolution, 2026-09-15).
//
// The defect this guards: the per-row session probe
// (taskProbeRowState → taskFindSessionJSONLForRow) resolved a row's session
// log by WALKING the whole sessions tree (70,438 session dirs on the operator's
// host; strace census 601,165 getdents64 / 299,848 openat for ONE
// `task list --status in_progress`), while the row already carried the task id
// and session id that NAME the file.
//
// What is asserted here is the COST property, at the seam: resolving a row's
// session log from the row's own columns must walk the sessions tree ZERO
// times. The seam (taskSessionTreeScan) is the only way the resolution path
// reaches a whole-tree walk, so a counter on it is the property itself — not a
// timing of a machine. The red proof lives in the task report: demoting the
// address probe below the scan turns these tests red.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/sessionlayout"
)

const (
	// costTaskID/costSessionID are the row identity the resolution is given.
	costTaskID    = "01a09fd1-3f80-702d-98a3-d220d7f87814"
	costSessionID = "819fc1ad-e248-4fc4-8b20-5c7193668c4d"
	// costOtherDirName is a session DIRECTORY that is not the row's own task
	// id — the "moved/foreign layout" shape only a scan can resolve.
	costOtherDirName = "0-foreign-layout"
)

// countSessionTreeScans rebinds the tree-WALK seam to a counter for one test
// and returns a func reporting the walks so far. The wrapped walk is the
// PRODUCTION one, so a test that expects the fallback still resolves for real
// while the count stays the seam's own. It clears the memo and restores both
// the walk and the cache, so no sibling test inherits either.
func countSessionTreeScans(t *testing.T) func() int {
	t.Helper()
	calls := 0
	prev := taskSessionTreeWalk
	taskSessionTreeWalk = func(dir string) []sessionlayout.Record {
		calls++
		return prev(dir)
	}
	t.Cleanup(func() {
		taskSessionTreeWalk = prev
		clearSessionTreeCache()
	})
	clearSessionTreeCache()
	return func() int { return calls }
}

// costSessionRoot lays down a sessions tree with a session RECORD at recPath
// (its dirs created) and returns the root.
func costSessionRoot(t *testing.T, recPath string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(recPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(recPath), err)
	}
	if err := os.WriteFile(recPath, []byte(sessionStartLine(costSessionID)), 0o644); err != nil {
		t.Fatalf("write %s: %v", recPath, err)
	}
	return root
}

// TestRowAddressResolutionWalksNoSessionTree is the acceptance property: for
// EVERY layout a ledger row's session log has been observed at, the resolution
// from the row's own columns walks the sessions tree zero times.
func TestRowAddressResolutionWalksNoSessionTree(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		rel  string // the record's path relative to the sessions root
	}{
		{"task-run", filepath.Join(costTaskID, costSessionID+taskSessionJSONLExt)},
		{"bare", filepath.Join(costSessionID, taskBareSessionRecordName)},
		{"flat", costSessionID + taskSessionJSONLExt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := filepath.Join(root, c.rel)
			if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(rec, []byte(sessionStartLine(costSessionID)), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			defer os.RemoveAll(rec)

			scans := countSessionTreeScans(t)
			if got := taskFindSessionJSONLForRow(root, costTaskID, costSessionID); got != rec {
				t.Fatalf("resolved %q, want the row's own record %q", got, rec)
			}
			if n := scans(); n != 0 {
				t.Fatalf("resolving the %s layout walked the sessions tree %d time(s); a row-addressed resolution must walk it ZERO times", c.name, n)
			}
		})
	}
}

// TestRowAddressResolutionMatchesTheScan pins BEHAVIOURAL identity with the
// tree scan it replaces: when a row's session exists in several of its dirs,
// the addressed resolution must return the SAME file the scan returns — the
// scan's precedence is readdir order over the sessions root, i.e. the
// byte-smaller DIR NAME wins (the bare <session-id> dir vs the task-run
// <task-id> dir). A cost fix that changed which file resolves would be a
// behaviour change wearing a perf commit's clothes.
func TestRowAddressResolutionMatchesTheScan(t *testing.T) {
	// A session id that sorts BEFORE its task id, and one that sorts AFTER —
	// the two orderings of the same pair of dirs.
	cases := []struct {
		name    string
		taskID  string
		sessID  string
		wantDir string // "" = the task-run dir, else the dir name that wins
	}{
		{"session dir sorts first", "t-1789381146", "08ed4b5e-8340-4cd8-8a60-affc92ccdd8f", "08ed4b5e-8340-4cd8-8a60-affc92ccdd8f"},
		{"task dir sorts first", "0aaa-task-row", "f8ed4b5e-8340-4cd8-8a60-affc92ccdd8f", "0aaa-task-row"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			bare := filepath.Join(root, c.sessID, taskBareSessionRecordName)
			run := filepath.Join(root, c.taskID, c.sessID+taskSessionJSONLExt)
			for _, p := range []string{bare, run} {
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
				}
				if err := os.WriteFile(p, []byte(sessionStartLine(c.sessID)), 0o644); err != nil {
					t.Fatalf("write %s: %v", p, err)
				}
			}
			want := filepath.Join(root, c.wantDir, filepath.Base(run))
			if c.wantDir == c.sessID {
				want = bare
			}

			// The SCAN is the oracle: whatever it resolves, the addressed
			// resolution must resolve identically.
			//
			// The oracle is the WALK itself (taskFindSessionJSONLDeepOnly), not
			// the taskFindSessionJSONL entry point: since the addressed-first
			// epic that entry point asks the id's own BARE dir before walking
			// (measured 2026-10-07), so it can no longer stand in for the scan
			// this contract is about — its answer for the bare-vs-task-run pair
			// is the id's own dir by construction. The walk IS what the
			// row-addressed lookup falls back to (taskFindSessionJSONLForRow's
			// last line), so comparing against it is the precise statement of
			// "the lookup may only replace a scan it agrees with".
			oracle := taskFindSessionJSONLDeepOnly(root, c.sessID)
			if oracle != want {
				t.Fatalf("fixture/oracle mismatch: scan resolved %q, expected %q", oracle, want)
			}
			if got := taskFindSessionJSONLForRow(root, c.taskID, c.sessID); got != oracle {
				t.Fatalf("addressed resolution returned %q but the scan returns %q — the cost fix must not change WHICH file resolves", got, oracle)
			}
		})
	}
}

// TestSessionTreeScanStaysTheFallbackAndIsMemoised pins the two halves of the
// fallback contract: a record the row's columns cannot address is STILL found
// (no resolution narrowed), and the walk is memoised per process — two
// fallback resolutions of the same root pay ONE walk, which is what keeps a
// render with several unaddressable rows from being O(rows × tree).
func TestSessionTreeScanStaysTheFallbackAndIsMemoised(t *testing.T) {
	root := t.TempDir()
	rec := filepath.Join(root, costOtherDirName, costSessionID+taskSessionJSONLExt)
	if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(rec, []byte(sessionStartLine(costSessionID)), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	scans := countSessionTreeScans(t)
	for i := 1; i <= 2; i++ {
		if got := taskFindSessionJSONLForRow(root, costTaskID, costSessionID); got != rec {
			t.Fatalf("call %d: fallback lost the record: got %q, want %q", i, got, rec)
		}
		if n := scans(); n != 1 {
			t.Fatalf("call %d walked the sessions tree %d time(s); the fallback walk must be memoised per process (exactly 1)", i, n)
		}
	}
}
