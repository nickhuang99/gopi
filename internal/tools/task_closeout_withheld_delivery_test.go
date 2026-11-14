package tools

// closeout-withheld-delivery — THE ROW'S TRAIL MUST SAY THE BRANCH NEVER LEFT.
//
// EPIC 01a12d5b-bb3a (measured 2026-10-12, host-b, client→peer1).
//
// The measured defect. A worker row's delivery branch is pushed by ONE arm —
// the client half of the close-out chain — and that arm is downstream of the
// MAIN-repo gate (`finishCleanupMainRepoGate`, task_finish_cleanup.go). When
// the checkout the run was LAUNCHED FROM (not the run's own worktree) carries a
// tracked modification, the gate stops the chain BEFORE the mode split, so the
// push and the worktree removal are both withheld — and, before this change, the
// gate also wrote NOTHING: `rep.Skipped` was set and the refusal went to stderr,
// which belongs to a run that has ended.
//
// The consequence on this host, measured: EIGHT of ten local `task/*` branches
// carry ZERO ref on the peer while their rows read `completed`, and the rendered
// surfaces cannot tell them from the rows whose branch DID cross (those carry a
// `[closeout-refused]` tail too — for the local-removal half). The instance row
// is 01a12d3f-be40-7604-97bd-3ab7394cd78a, whose fix 3771ebc93 exists under ONE
// ref on this host and none on the peer.
//
// WHAT THESE TESTS PIN, each failing on the pre-change tree:
//
//	① the chain's WITHHOLDING refusal is written to the ROW'S TRAIL (a durable
//	  record — the removed half was the one that stranded whole deliverables);
//	② the sentence it writes NAMES the state (the branch did NOT reach the peer)
//	  so the row is distinguishable from the removal-only refusal, whose branch
//	  already crossed;
//	③ `TaskList` renders that sentence INSIDE its 60-rune clip, so the state is
//	  readable at a glance and greppable — not clipped off the line;
//	④ the DELIVERY really was withheld in the fixture (no ref on the peer), so
//	  the test is about the measured state and not about a sentence.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// dirtyMainTracked writes an uncommitted TRACKED modification into the fixture's
// MAIN checkout — the exact input the gate's ① refuses on (the untracked-only
// shape is RECORDED rather than refusing; see
// TestFinishCleanupRecordsUntrackedMainRepoResidueButStillDelivers).
func dirtyMainTracked(t *testing.T, f *mainRepoFixture) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.clientDir, "a.txt"), []byte("uncommitted work\n"), 0o644); err != nil {
		t.Fatalf("dirty the main checkout: %v", err)
	}
	out := preMergeGit(t, f.clientDir, "status", "--porcelain")
	if !strings.Contains(out, "a.txt") {
		t.Fatalf("fixture: the main checkout is not dirty: %q", out)
	}
}

// TestCloseoutWithheldDeliveryIsRecordedOnTheRowTrail is claim ① + ②: the
// MAIN-repo refusal that withholds a whole delivery is durable, and it says
// which state it left the branch in.
func TestCloseoutWithheldDeliveryIsRecordedOnTheRowTrail(t *testing.T) {
	f := newMainRepoFixture(t)
	f.reset(t)
	dirtyMainTracked(t, f)

	// A real row on a real ledger (the trail write is the subject, so the row
	// must exist for the event to be readable back).
	const rowID = "t-withheld-delivery"
	path := testutil.NewTestDBPath(t)
	if _, err := TaskNew("withheld delivery", rowID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	closeoutSetStatus(t, path, rowID, taskStatusCompleted)

	rep, _ := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: rowID, Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, f.serverDir, false) //stylegate:ignore test helper: the report is the subject, the error is not asserted here

	// ④ THE DELIVERY REALLY WAS WITHHELD — the measured state of the eight
	// stranded branches, not a sentence about them.
	if !rep.Skipped {
		t.Fatalf("the main-repo gate must withhold this close-out, got %+v", rep)
	}
	if serverHasRefRuns(t, f.serverDir, f.featBranch) {
		t.Fatalf("fixture: the branch reached the peer, so the withheld case is not being exercised")
	}
	// ② THE SENTENCE NAMES THE STATE, beside the refusal it came from.
	for _, want := range []string{
		finishCleanupDeliveryWithheldMarker,
		finishCleanupMainRepoDirtyPrefix,
		finishCleanupDirtyTrackedPrefix,
		f.clientDir,
	} {
		if !strings.Contains(rep.SkipReason, want) {
			t.Errorf("the withheld refusal must name %q, got:\n%s", want, rep.SkipReason)
		}
	}
	// ① IT IS ON THE ROW'S TRAIL — the durable half. Before the change this
	// read returns zero rows: the arm that strands whole deliverables was the
	// one arm of the chain that left no record. Rule 15.
	rows := readTaskEventDetails(t, rowID, finishCleanupRemovalRefusedEvent)
	if len(rows) == 0 {
		t.Fatalf("the WITHHELD delivery left NO %q row on the trail — the state that cost eight deliverables is still silent",
			finishCleanupRemovalRefusedEvent)
	}
	last := rows[len(rows)-1]
	for _, want := range []string{
		closeoutWithheldStateSeparator,
		finishCleanupDeliveryWithheldMarker,
		finishCleanupMainRepoDirtyPrefix,
		finishCleanupMainRepoRepairNote,
		f.clientDir,
	} {
		if !strings.Contains(last, want) {
			t.Errorf("the trail record must name %q, got:\n%s", want, last)
		}
	}
}

// TestTaskListCarriesTheWithheldStateInsideItsClip is claim ③, on the rendered
// surface the operator actually scans. Two LEDGERS are compared, because the
// claim is not "a marker exists" (it did) but "the two refused states are
// DISTINGUISHABLE": the withheld one and the removal-only one both wear
// `[closeout-refused]`, and only one of them means the branch never crossed.
func TestTaskListCarriesTheWithheldStateInsideItsClip(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	// The row's OWN trail decides, so the two rows below carry exactly the two
	// sentences the chain writes — quoted from the chain's own vocabulary
	// rather than re-typed.
	withheld := finishCleanupMainRepoSkipMarker + finishCleanupMainRepoDirtyPrefix + "/home/user/work/GoPi: " +
		finishCleanupDirtyTrackedPrefix + " M CONFIG.md" + finishCleanupMainRepoRepairNote +
		finishCleanupRemovalNotReachedNote + finishCleanupDeliveryWithheldMarker
	removalOnly := finishCleanupGoneWorktreeSkipMarker + "nothing to remove)"
	closeoutSeedRow(t, path, "t-withheld", [2]string{finishCleanupRemovalRefusedEvent, withheld})
	closeoutSeedRow(t, path, "t-removal-only", [2]string{finishCleanupRemovalRefusedEvent, removalOnly})

	out, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	var withheldLine, removalLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "t-withheld") {
			withheldLine = ln
		}
		if strings.Contains(ln, "t-removal-only") {
			removalLine = ln
		}
	}
	if withheldLine == "" || removalLine == "" {
		t.Fatalf("both rows must render:\n%s", out)
	}
	if !strings.HasSuffix(withheldLine, " "+taskListCloseoutMarker) &&
		!strings.Contains(withheldLine, " "+taskListCloseoutMarker+taskListCloseoutTailSeparator) {
		t.Fatalf("the withheld row lost its close-out tail:\n%s", withheldLine)
	}
	// ③ THE STATE SURVIVES THE CLIP. The refusal is long enough that a naive
	// render truncates before the state — which is what makes this test about
	// the position and not only about the words.
	if !strings.Contains(withheldLine, finishCleanupDeliveryWithheldMarker) {
		t.Errorf("the list clipped the WITHHELD state off the row — an operator cannot tell this row from one whose branch already crossed:\n%s", withheldLine)
	}
	// And the removal-only row does NOT claim a withheld delivery: the marker
	// is a statement, so it must not be rendered on a row it is false for.
	if strings.Contains(removalLine, finishCleanupDeliveryWithheldMarker) {
		t.Errorf("a removal-only refusal must not be rendered as a withheld delivery:\n%s", removalLine)
	}
}

// TestCloseoutWithheldDeliverySharesTheLaneEventName pins the ONE spelling
// (Rules 13/14): the withheld-delivery refusal and the removal refusal reach
// the ledger under ONE event name — the LANE's name — so the view's reader
// needs no list of names and a historical row stays reachable.
func TestCloseoutWithheldDeliverySharesTheLaneEventName(t *testing.T) {
	if finishCleanupRemovalRefusedEvent != "finish-cleanup-removal-refused" {
		t.Fatalf("the lane event's spelling changed: %q", finishCleanupRemovalRefusedEvent)
	}
	// The list's reader and the writer must agree on ONE name — the property
	// that a second literal in either place would silently break.
	if !strings.Contains(closeoutSignalsNewestPerRow, "e.event = ?") {
		t.Fatalf("the close-out reader no longer binds the event name as a parameter")
	}
	rows := readTaskEventDetails(t, "t-absent", finishCleanupRemovalRefusedEvent)
	if len(rows) != 0 {
		t.Fatalf("fixture: an unseeded row reported %d events", len(rows))
	}
}

// TestCloseoutWithheldDeliveryPlanModeWritesNothing is the GUARD on the record:
// the write above is the fix, and a fix that also fired on `--dry-run` would be
// a small regression of its own — a plan would mutate the row it describes, and
// no later reader could tell a withheld delivery from a plan somebody ran.
func TestCloseoutWithheldDeliveryPlanModeWritesNothing(t *testing.T) {
	f := newMainRepoFixture(t)
	f.reset(t)
	dirtyMainTracked(t, f)

	const rowID = "t-withheld-plan"
	path := testutil.NewTestDBPath(t)
	if _, err := TaskNew("withheld delivery (plan)", rowID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	closeoutSetStatus(t, path, rowID, taskStatusCompleted)

	rep, _ := runFinishCleanupChainTarget(context.Background(),
		&FinishCleanupReport{TaskID: rowID, Repo: f.clientDir},
		taskFinishDeliverable{Repo: f.clientDir, Branch: f.featBranch, Worktree: f.wtDir}, f.serverDir, true) //stylegate:ignore test helper: the report is the subject, the error is not asserted here

	if !rep.Skipped || !strings.Contains(rep.SkipReason, finishCleanupDeliveryWithheldMarker) {
		t.Fatalf("a dry run must describe the withheld delivery, got %+v", rep)
	}
	if rows := readTaskEventDetails(t, rowID, finishCleanupRemovalRefusedEvent); len(rows) != 0 {
		t.Fatalf("a DRY RUN mutated the ledger: %d trail row(s) written:\n%s", len(rows), strings.Join(rows, "\n"))
	}
}
