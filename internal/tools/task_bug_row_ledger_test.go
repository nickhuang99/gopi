package tools

// task_bug_row_ledger_test.go — the regression guard for the "filed bug row"
// LOCUS (task_bug_row_ledger.go).
//
// WHAT IS BEING GUARDED. Three merger lanes end a successful filing with
// `filed bug row: <summary>`. A reader who checks `gopi task list` and gets 0
// hits had no way to tell "the write was lost" from "the write went to the
// ledger THIS process resolved" — and inside a test process that second world is
// the ordinary one (internal/testutil pins GOPI_MEMORY_DB to a per-test
// sandbox, Rule 32). The measured case (2026-10-16, task 01a142da): the row was
// `filed`, the operator's ledger held 0 hits, and only a probe test could say
// where it went.
//
// THE CONTRACTS PINNED HERE:
//
//	1. The locus is the PINNED ledger's exact path, from the ONE resolver the
//	   write itself uses (config.ResolveLedger), with the resolver's OWN channel
//	   name — never a second derivation (Rule 2).
//	2. All THREE filing sites carry it on the line they actually print — the
//	   tests drive the real report/filing functions, not the helper alone, so a
//	   site that forgot the clause is red.
//
// The pin is the test's own (testutil.NewTestDBPath), so the assertion can only
// pass by naming the file TaskSubmit opened: a hard-coded or re-derived path
// would differ from it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// brlPinLedger pins a fresh test ledger and returns its path. It ALSO proves the
// fixture is the right instrument: the pin must be the value the resolver
// reports, or every assertion below would be checking the wrong file.
func brlPinLedger(t *testing.T) string {
	t.Helper()
	ledger := testutil.NewTestDBPath(t)
	if got := config.LedgerPath(); got != ledger {
		t.Fatalf("the pinned ledger is %q but config.LedgerPath() answers %q — the locus would describe a different file than TaskSubmit opens",
			ledger, got)
	}
	// A notify sink of this test's own: TaskSubmit announces the new row, and an
	// unpinned sink resolves the operator's settings (the sibling suite's own
	// measured landmine).
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+filepath.Join(t.TempDir(), "events.jsonl"))
	return ledger
}

// TestBugRowLedgerLocus_NamesTheExactPinnedLedger is the unit arm: the helper
// names the pinned path AND the channel that decided it.
func TestBugRowLedgerLocus_NamesTheExactPinnedLedger(t *testing.T) {
	ledger := brlPinLedger(t)

	locus := bugRowLedgerLocus()

	if !strings.Contains(locus, ledger) {
		t.Fatalf("the locus must name the file TaskSubmit writes (%s), got %q", ledger, locus)
	}
	// The channel is the resolver's own source value, not a string this package
	// invented: that is what makes the line answer "which channel chose this?".
	if !strings.Contains(locus, config.MemoryDBEnv) {
		t.Fatalf("the locus must name the deciding channel (%s), got %q", config.MemoryDBEnv, locus)
	}
	if strings.Count(locus, ledger) != 1 {
		t.Fatalf("the locus must name the path exactly once, got %q", locus)
	}
}

// TestBugRowLedgerLocus_AllThreeFilingSitesCarryIt drives the three real filing
// paths and requires each printed line to carry the exact pinned ledger.
//
// This is the arm that catches the defect class the epic names: a helper that
// exists but is not called at a site is invisible, so the assertion is made
// against the CAPTURED STDERR of the lane's own log function — the same seam a
// merger operator reads.
func TestBugRowLedgerLocus_TheFilingSiteCarriesIt(t *testing.T) {
	ledger := brlPinLedger(t)

	wantClause := "(ledger " + ledger + " via " + config.MemoryDBEnv + ")"

	cases := []struct {
		site string
		run  func()
	}{
		// THE CONFIG-ANCHOR AND TARGET-DOC SITES USED TO BE CASES HERE. Both
		// producers were deleted with the document pipeline (removal slice 3/5):
		// mergeConfigAnchorReport measured the config index's own anchor table,
		// and mergeTargetDocGateFileBugRow filed the TARGET-DEVDOC lane's drift —
		// both read internal/docclass's corpus. The MERGESET site below is the
		// surviving member of the family, and it is the same seam (the ledger
		// locus clause on the delivered line), so the arm still measures what it
		// was built for.
		{"MERGESET", func() {
			mergesetGateFileBugRow(brlRepoRoot(t), "deadbeef", mergesetGateResult{
				Gate:   vetGateName,
				Status: mergeBuildGateStatusFail,
				Cmd:    "go vet ./...",
			})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.site, func(t *testing.T) {
			out := captureStderr(t, tc.run)
			if !strings.Contains(out, "filed bug row:") {
				t.Fatalf("the %s site filed no row — the arm cannot test a line that never printed:\n%s", tc.site, out)
			}
			// Log the delivered line so a -v run shows the shape the operator
			// reads (Rule 31: the report's Ran: step shows the real output).
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "filed bug row:") {
					t.Logf("delivered line: %s", line)
				}
			}
			if !strings.Contains(out, wantClause) {
				t.Fatalf("the %s site's `filed bug row` line must name the ledger it wrote (%s);\ncaptured stderr:\n%s",
					tc.site, wantClause, out)
			}
		})
	}
}

// brlRepoRoot walks up from the test's working directory to the module root —
// the repo path the filing-site arms hand to a bug-row producer. It replaced
// the config-anchor test file's helper when that file was deleted with the
// document pipeline (removal slice 3/5).
func brlRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
