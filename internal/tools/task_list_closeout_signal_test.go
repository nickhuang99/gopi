package tools

// task_list_closeout_signal_test.go — THE RED PROOF for the ledger view's
// close-out marker (epic 01a12af7, case ①).
//
// WHAT WAS MEASURED WRONG. A tracked dirty file in the MAIN checkout made the
// close-out chain refuse whole — no push, no `git branch -d` — while the row
// still read `completed`. Forty-two rows sat that way for nine hours, and the
// ledger view showed a wall of green. The refusal WAS durable (the chain has
// written `finish-cleanup-removal-refused` on the row's trail since
// devdoc/requirements/merge-and-branches.md B2) and it was invisible where the
// operator looks.
//
// THE FOUR CLAIMS THESE TESTS PIN, each of which fails on the pre-change tree:
//
//	① a row whose trail ENDS at a refusal wears the marker on the LIST line;
//	② a row that was refused once and DELIVERED LATER does NOT (a state, not a
//	  scar — the naive "has the event ever been written?" read gets this wrong);
//	③ a ledger with no refusals renders byte-identically to one that never
//	  asked (the zero contract — no stray blank line, no header);
//	④ the marker's own spelling is the constant, both in the header sentence
//	  and on the row, so a reader has ONE token to grep.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The HALF-CHAIN CLAIMS the header used to carry, and must never carry again.
//
// They were not merely verbose — they were FALSE, and they were falsified by the
// very rows printed under them (measured 2026-10-12, epic
// 01a12bad-a8ac-7b77-8964-227c9314ab75): `gopi task list --all` rendered 49
// marked rows and 49/49 carried the SAME reason, `removal skipped (delivery
// already attempted): the recorded worktree directory is not there` — the push
// half had already happened, so "the branch was NOT removed" described the
// opposite of the measured state.
//
// The reader behind the header (taskListCloseoutSignals) keys on ONE event name,
// written by BOTH the removal-refusal arm and the removal-not-reached arm, so it
// knows the row was refused and NOTHING about which half of the chain stopped. A
// header that names a half asserts what it cannot see.
var testCloseoutBannedHeaderClaims = []string{
	"stopped before the push",
	"before the push",
	"NOT removed",
}

// closeoutMarkedRows counts the rendered rows wearing the close-out tail — the
// figure the header's own count must agree with. A row is matched by the
// rendered TAIL shape (" [closeout-refused]" closing the line, or followed by
// the separator), never by the bare token: a row whose SUMMARY quotes the marker
// (a task about this marker, for instance) is not a refused row.
func closeoutMarkedRows(rendered string) int {
	n := 0
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, TaskListRowPrefix) {
			continue
		}
		if strings.HasSuffix(ln, " "+taskListCloseoutMarker) ||
			strings.Contains(ln, " "+taskListCloseoutMarker+taskListCloseoutTailSeparator) {
			n++
		}
	}
	return n
}

// TestTaskListCloseoutHeaderClaimsNoHalfOfTheChain pins the CORRECTION: the
// header states the population it counted and where the why is, and it never
// asserts which half of the delivery chain stopped.
func TestTaskListCloseoutHeaderClaimsNoHalfOfTheChain(t *testing.T) {
	for _, n := range []int{1, 2, 49} {
		header := taskListCloseoutNote(n, taskListCloseoutScopeLedger)
		for _, claim := range testCloseoutBannedHeaderClaims {
			if strings.Contains(header, claim) {
				t.Errorf("the header for %d row(s) still claims a half of the chain (%q):\n%s", n, claim, header)
			}
		}
		// The two facts it DOES own: how many, and where the reason is.
		if !strings.Contains(header, fmt.Sprintf("close-out refused on %d row(s)", n)) {
			t.Errorf("the header for %d row(s) lost its count: %q", n, header)
		}
		if !strings.Contains(header, taskListCloseoutMarker) {
			t.Errorf("the header for %d row(s) no longer names the marker: %q", n, header)
		}
	}
}

// The two tests that used to live here — a header count == marked rows on an
// UNFILTERED render, and a "ledger-wide, not view-scoped" contract — were
// superseded by epic 01a12bc0 and are replaced wholesale in
// task_list_closeout_scope_test.go: the count is now the number of markers the
// SAME render printed, and a filtered view counts its OWN population (the old
// contract pinned the measured defect as correct). What survives here is the
// wording guard below.

// closeoutRefusal is the detail the chain actually writes (measured on this
// host's ledger, row 01a120a7-…): the test quotes a real sentence rather than
// inventing a shape, so the 60-rune clip is exercised against the real thing.
// Composed from the marker constant so a wording change reaches this arm too
// (Rule 1b): the literal form of this sentence is what the fix retired.
const closeoutRefusal = finishCleanupGoneWorktreeSkipMarker +
	"git status: chdir /home/user/workspace/gopi-x: no such file or directory)"

// closeoutSeedRow writes one row plus its trail. `events` is rendered in order,
// so a caller states the row's history the way the ledger stores it.
func closeoutSeedRow(t *testing.T, dbPath, id string, events ...[2]string) {
	t.Helper()
	if _, err := TaskNew("row "+id, id, "", ""); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	for _, e := range events {
		taskEventWrite(ctx, db, id, e[0], e[1])
	}
}

// closeoutSetStatus moves the row's STATUS COLUMN alone, without appending an
// event — which is the point: a status transition through the normal door would
// write a trail row and the refusal would no longer be the row's newest event,
// so the fixture would silently stop being a refused-for-this-status row.
func closeoutSetStatus(t *testing.T, dbPath, id, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE task_id = ?`, status, id); err != nil {
		t.Fatalf("set status of %s: %v", id, err)
	}
}

func TestTaskListMarksARowWhoseCloseOutWasRefused(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	closeoutSeedRow(t, path, "t-refused", [2]string{finishCleanupRemovalRefusedEvent, closeoutRefusal})

	out, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(out, taskListCloseoutMarker) {
		t.Fatalf("the list does not mark a row whose close-out was refused:\n%s", out)
	}
	if !strings.Contains(out, "close-out refused on 1 row(s)") {
		t.Errorf("the header does not carry the COUNT — the number that was invisible for nine hours:\n%s", out)
	}
	if !strings.Contains(out, "t-refused") {
		t.Fatalf("the marked row is not the seeded one:\n%s", out)
	}
	// The reason rides the row, so the operator learns WHY without a second
	// invocation. The row is clipped, so the assertion is on a prefix that
	// survives the clip.
	if !strings.Contains(out, finishCleanupGoneWorktreeDirAbsentHead) {
		t.Errorf("the row's marker does not carry its reason:\n%s", out)
	}
}

func TestTaskListDoesNotMarkARowRefusedThenDelivered(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	// The measured shape this guards: a row can carry the event SOMEWHERE in
	// its history (296 such rows on this host's ledger) while only the rows
	// whose trail ENDS at it are still stranded. A marker that fired on the
	// event name alone would accuse every row a later pass repaired.
	closeoutSeedRow(t, path, "t-repaired",
		[2]string{finishCleanupRemovalRefusedEvent, closeoutRefusal},
		[2]string{taskStatusCompleted, "gopi run completed — delivered on the second pass"})

	out, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if strings.Contains(out, taskListCloseoutMarker) {
		t.Fatalf("a row whose LATER event superseded the refusal is still marked:\n%s", out)
	}
	if strings.Contains(out, "close-out refused on") {
		t.Errorf("the header counts a repaired row:\n%s", out)
	}
}

func TestTaskListCleanLedgerIsByteIdenticalToNoRefusals(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	closeoutSeedRow(t, path, "t-plain", [2]string{taskStatusCompleted, "gopi run completed"})

	out, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if strings.Contains(out, taskListCloseoutMarker) || strings.Contains(out, "close-out refused on") {
		t.Fatalf("a ledger with no refused close-out rendered a close-out signal:\n%s", out)
	}
	// The zero contract, asserted on the BYTES: no trailing blank line, no
	// stray separator — a render that never asked and one that asked and found
	// nothing must be indistinguishable.
	if strings.HasSuffix(out, "\n\n") {
		t.Errorf("the render gained a blank line with nothing to report:\n%q", out)
	}
}

func TestTaskListCloseoutSignalSuffixUsesTheOneMarker(t *testing.T) {
	// The spelling lives in ONE place (Rule 1b/13): the header and the row both
	// render this constant, so a reader greps one token and a rename cannot
	// leave half the surface behind.
	suffix := taskListCloseoutSignalSuffix(closeoutRefusal)
	if !strings.HasPrefix(suffix, " "+taskListCloseoutMarker) {
		t.Fatalf("the row tail does not open with the marker constant: %q", suffix)
	}
	if !strings.Contains(taskListCloseoutNote(1, taskListCloseoutScopeLedger), taskListCloseoutMarker) {
		t.Fatalf("the header does not name the marker constant: %q", taskListCloseoutNote(1, taskListCloseoutScopeLedger))
	}
	if taskListCloseoutNote(0, taskListCloseoutScopeLedger) != "" {
		t.Errorf("a zero count must render NOTHING, got %q", taskListCloseoutNote(0, taskListCloseoutScopeLedger))
	}
	// The counted DOMAIN rides the sentence (epic 01a12bc0): the same count
	// renders two different facts depending on what was walked, and a reader
	// must not have to infer which — so neither phrase may be optional and
	// neither may be the other's.
	for _, scope := range []string{taskListCloseoutScopeLedger, taskListCloseoutScopeView} {
		if !strings.Contains(taskListCloseoutNote(3, scope), scope) {
			t.Errorf("the header dropped its scope phrase %q: %q", scope, taskListCloseoutNote(3, scope))
		}
	}
	if taskListCloseoutScopeLedger == taskListCloseoutScopeView {
		t.Fatalf("the two scope phrases are the same string — the domain could not be read off the sentence")
	}
	// A nil reason (an event row with an empty detail) must still be visible:
	// the fact is the refusal, not the sentence.
	if got := taskListCloseoutSignalSuffix("  "); got != " "+taskListCloseoutMarker {
		t.Errorf("an empty reason lost the marker: %q", got)
	}
	// The clip is on a RUNE boundary, so a multi-byte reason cannot put invalid
	// UTF-8 on the terminal.
	long := strings.Repeat("错", taskListCloseoutReasonLimit+20)
	got := clipTaskListCloseoutReason(long)
	if !strings.HasSuffix(got, taskListCloseoutEllipsis) {
		t.Fatalf("a long reason was not clipped: %q", got)
	}
	if r := []rune(strings.TrimSuffix(got, taskListCloseoutEllipsis)); len(r) != taskListCloseoutReasonLimit {
		t.Errorf("clip kept %d runes, want %d", len(r), taskListCloseoutReasonLimit)
	}
}

func TestTaskListCloseoutSignalsToleratesAnUnmigratedLedger(t *testing.T) {
	// A ledger whose task_events table is absent answers "nothing to say" — the
	// same tolerance failedReissueNote takes. A list that FAILED because an
	// optional trail table is missing would trade a silent marker for a silent
	// list, which is the worse half of the same defect (Rule 15).
	path := testutil.NewTestDBPath(t)
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS task_events"); err != nil {
		t.Fatalf("drop task_events: %v", err)
	}
	// The UNFILTERED read ("" status) is the one the ledger-global contract
	// applies to; the scoped form is exercised by the scope tests.
	signals, err := taskListCloseoutSignals(ctx, db, "")
	if err != nil {
		t.Fatalf("an unmigrated ledger must not fail the read: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("an unmigrated ledger produced signals: %v", signals)
	}
}
