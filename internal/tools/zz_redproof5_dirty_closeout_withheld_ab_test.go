package tools

// zz_redproof5_dirty_closeout_withheld_ab_test.go — THE A/B HALF (Rule 5).
//
// EPIC 01a12d5b-bb3a. The behavior-visible pair is:
//
//	BEFORE  a close-out withheld by the MAIN-repo gate writes NOTHING to the
//	        row's trail and the ledger view renders no state marker for it;
//	AFTER   the same run writes the row's trail and the view renders
//	        "the DELIVERY was withheld (the branch did NOT reach the peer)".
//
// WHY THIS FILE SPELLS ITS LITERALS instead of referencing the constants the
// production change introduced: a red proof has to COMPILE against the
// pre-change tree, and a symbol that does not exist yet turns "the behavior is
// absent" into "the package does not build" — a compile error proves nothing
// about the behavior. This is the ONE place a literal is the instrument, and it
// is also why the file is named zz_redproof (the house marker for an A/B pin).
//
// The RED run: `git stash push -- internal/tools/task_finish_cleanup.go
// internal/tools/task_list_closeout_signal.go` (the fix, and nothing else) and
// run this file with the sibling file moved aside; the GREEN run is the tree as
// committed.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// The literals under test (A/B instrument — see the file comment).
const (
	redproofLaneEvent     = "finish-cleanup-removal-refused"
	redproofWithheldState = "the DELIVERY was withheld (the branch did NOT reach the peer)"
)

func TestRedProofWithheldDeliveryIsRecordedAndRendered(t *testing.T) {
	f := newMainRepoFixture(t)
	f.reset(t)
	if err := os.WriteFile(filepath.Join(f.clientDir, "a.txt"), []byte("uncommitted work\n"), 0o644); err != nil {
		t.Fatalf("dirty the main checkout: %v", err)
	}
	if out := preMergeGit(t, f.clientDir, "status", "--porcelain"); !strings.Contains(out, "a.txt") {
		t.Fatalf("fixture: the main checkout is not dirty: %q", out)
	}

	const rowID = "t-redproof-withheld"
	path := testutil.NewTestDBPath(t)
	if _, err := TaskNew("withheld delivery (red proof)", rowID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	closeoutSetStatus(t, path, rowID, taskStatusCompleted)

	rep, _ := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: rowID, Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, f.serverDir, false) //stylegate:ignore test helper: the report is the subject, the error is not asserted here

	if !rep.Skipped {
		t.Fatalf("the main-repo gate must withhold this close-out, got %+v", rep)
	}
	if serverHasRefRuns(t, f.serverDir, f.featBranch) {
		t.Fatalf("fixture: the branch reached the peer, so the withheld case is not exercised")
	}
	// ① THE DURABLE RECORD. Pre-change this list is EMPTY: the gate set
	// rep.Skipped and returned, and the only write site on the lane was the
	// removal arm it never reached.
	rows := readTaskEventDetails(t, rowID, redproofLaneEvent)
	if len(rows) == 0 {
		t.Fatalf("RED: the withheld delivery left no %q row on the trail", redproofLaneEvent)
	}
	if !strings.Contains(rows[len(rows)-1], redproofWithheldState) {
		t.Fatalf("RED: the trail row does not name the withheld state:\n%s", rows[len(rows)-1])
	}
	// ② THE RENDERED STATE, inside the list's own clip. Pre-change the row
	// carries no close-out tail at all.
	out, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	var line string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, rowID) {
			line = ln
		}
	}
	if line == "" {
		t.Fatalf("the row is not rendered:\n%s", out)
	}
	if !strings.Contains(line, redproofWithheldState) {
		t.Fatalf("RED: the list does not render the withheld state on the row (clipped or absent):\n%s", line)
	}
}
