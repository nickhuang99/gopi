package tools

// Regression for the trail's THIRD render face: a recorded DELIVERY DEBT must
// be visible on `task show`, and a later verdict must not be able to take it
// off the row again.
//
// THE DEFECT THIS FILE PINS (measured 2026-11-01 on the live ledger, before the
// fix): `delivery-proof-owed` was written to task_events by the delivery-proof
// refusal and rendered by NOTHING.
//
//	occurrences of delivery-proof-owed        135
//	rendered on `task show`                     0
//	carried by an undelivered ground           65 of 82
//
// The tempting one-line "fix" is to add the name to verdictTrailEventNames.
// That is a different change with a different consequence: the verdict reader
// keeps the NEWEST judgment and nothing older, and 98 of those 135 records are
// followed by a later verdict (`reclaim-unreachable`, stamped days afterwards
// by the reclaim arm) — so the entry would show the debt on 37 records, hide it
// on 98, and move the `verdict:` line of every row that already rendered one.
//
// Hence a separate axis, and hence these arms: the FIRST proves the fact is
// visible at all, the SECOND proves it stays visible BESIDE a later verdict (a
// per-line test passes happily on a renderer where one line displaces the
// other), and the THIRD pins the design decision itself so a future "just add
// the name to the verdict list" cannot quietly reinstate the defect.

import (
	"context"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// dpOwedFixture seeds one open row and writes ONE trail record through the
// same writer the delivery-proof arm uses. It returns the ledger path.
func dpOwedFixture(t *testing.T, id, event, detail string) string {
	t.Helper()
	dbPath := testutil.NewTestDBPath(t)
	dpSeedRow(t, dbPath, id, "feat: work whose tip never reached its delivery point", taskStatusInProgress)
	dpOwedEvent(t, dbPath, id, event, detail)
	return dbPath
}

// dpOwedEvent appends one trail record and closes its own handle, so a fixture
// never leaves a second connection open on the ledger it is about to have
// rendered (the ledger pins a single connection — see the stranded-rows
// reader's note on per-row queries while a cursor is open).
func dpOwedEvent(t *testing.T, dbPath, id, event, detail string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	taskEventWrite(ctx, db, id, event, detail)
}

// dpOwedShow renders `task show` for a row in the given ledger, through the
// SAME function the CLI verb calls.
func dpOwedShow(t *testing.T, dbPath, id string) string {
	t.Helper()
	out, err := TaskShow(id)
	if err != nil {
		t.Fatalf("task show %s: %v", id, err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("task show rendered nothing — a non-vacuity floor, so a broken reader cannot pass")
	}
	return out
}

// TestTaskShowRendersTheDeliveryOwedDebt is the visibility arm: the record is
// on the trail, so the surface a reviewer reads must print it, on its OWN
// label, naming the event it came from.
//
// RED ON THE PRE-FIX TREE: before the delivery-owed axis existed, the show
// printed the status and the briefing and no line about the debt at all —
// `strings.Contains(out, TaskShowDeliveryOwedLabel)` was false and the whole
// render was quoted in the failure.
func TestTaskShowRendersTheDeliveryOwedDebt(t *testing.T) {
	const id = "01a20000-0000-7000-8000-000000000201"
	const marker = "the tip f2215b227ac3 is not at the destination it declared"
	dbPath := dpOwedFixture(t, id, deliveryProofOwedEvent, marker)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	out := dpOwedShow(t, dbPath, id)
	for _, want := range []string{TaskShowDeliveryOwedLabel, deliveryProofOwedEvent, marker} {
		if !strings.Contains(out, want) {
			t.Errorf("`task show` does not carry %q — the delivery debt is in task_events and invisible again:\n%s",
				want, out)
		}
	}
	// The record names its event, so a `where event = ...` query and the
	// rendered line agree (the same contract the verdict line keeps).
	wantName := TaskShowDeliveryOwedNameLabel + strings.Repeat(" ",
		taskShowLabelWidth-len(TaskShowDeliveryOwedNameLabel)) + deliveryProofOwedEvent
	if !strings.Contains(out, wantName) {
		t.Errorf("the delivery line must name its event:\n%s", out)
	}
	// It renders a RECORD, never a status change: the row is still open.
	if !strings.Contains(out, "status:         "+taskStatusInProgress) {
		t.Errorf("rendering the debt must not move the row:\n%s", out)
	}
}

// TestTaskShowRendersDeliveryOwedBesideALaterVerdict is THE MIRROR ARM, and it
// is the one that decides the design: a delivery debt written FIRST, a verdict
// written AFTER it — the shape of 98 of the 135 live records — must appear on
// ONE render, both lines, neither displacing the other.
//
// This is exactly the case a verdict-list entry cannot satisfy: the verdict
// selector keeps the newest judgment (`reclaim-unreachable` here), so folding
// the debt into that axis would render the verdict and drop the debt, and this
// arm would go red.
func TestTaskShowRendersDeliveryOwedBesideALaterVerdict(t *testing.T) {
	const id = "01a20000-0000-7000-8000-000000000202"
	const debt = "its deliverable is not at the destination it declared — peer advertises 2bba6075, row tip f2215b22"
	const laterVerdict = "path is a linked worktree of ANOTHER repo — refusing to remove a directory this row does not own"
	dbPath := dpOwedFixture(t, id, deliveryProofOwedEvent, debt)

	// The LATER verdict, written after the debt through the same writer the
	// reclaim arm uses (task_reclaim.go), and a member of the verdict axis.
	dpOwedEvent(t, dbPath, id, reclaimUnreachableEventName, laterVerdict)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	out := dpOwedShow(t, dbPath, id)

	// Face 1 — the newest judgment, unchanged on its own line.
	if !strings.Contains(out, TaskShowVerdictLabel) {
		t.Fatalf("the later verdict must still render on %s:\n%s", TaskShowVerdictLabel, out)
	}
	wantVerdict := TaskShowVerdictLabel + strings.Repeat(" ", taskShowLabelWidth-len(TaskShowVerdictLabel)) + laterVerdict
	if !strings.Contains(out, wantVerdict) {
		t.Errorf("the verdict line must carry the NEWEST judgment:\n%s", out)
	}
	// Face 2 — the delivery debt, which the verdict must not have taken away.
	if !strings.Contains(out, TaskShowDeliveryOwedLabel) || !strings.Contains(out, debt) {
		t.Errorf("a later verdict displaced the delivery debt — that is the defect this axis exists to close.\n"+
			"Both faces are real facts about the row and neither may hide the other. `task show`:\n%s", out)
	}
	// Face 3 — this change ADDS a label and moves no existing one: the run
	// digest still owns `result:` when the row has one. (A row whose newest
	// event is neither a digest nor a status still echoes on `result:` — that
	// is the pre-existing fallback, untouched here; see taskEventLatestDetail.)
	if !strings.Contains(out, "result:         ") {
		t.Errorf("the result line disappeared — this change must only ADD a label:\n%s", out)
	}
	if strings.Count(out, TaskShowDeliveryOwedLabel) != 1 {
		t.Errorf("the delivery debt must render exactly once per row, got %d:\n%s",
			strings.Count(out, TaskShowDeliveryOwedLabel), out)
	}
}

// TestDeliveryOwedIsItsOwnAxisNotAVerdictEntry pins the DECISION, not just the
// behaviour: the delivery debt is rendered by its own selector, and it is
// deliberately NOT a member of verdictTrailEventNames.
//
// Without this arm the cheap regression is invisible: someone adds the name to
// the verdict list, the first two arms stay GREEN (the newest record renders),
// and the 98 shadowed records go on being hidden — while every already-rendered
// row's verdict line moves to a different event.
func TestDeliveryOwedIsItsOwnAxisNotAVerdictEntry(t *testing.T) {
	if !taskShowDeliveryOwedEventName(deliveryProofOwedEvent) {
		t.Fatalf("%q must be a member of deliveryOwedTrailEventNames, or nothing renders the debt at all",
			deliveryProofOwedEvent)
	}
	if taskShowVerdictEventName(deliveryProofOwedEvent) {
		t.Errorf("%q must NOT be in verdictTrailEventNames: the verdict axis keeps only the NEWEST judgment, so "+
			"membership would hide the debt on every row a later verdict touched (98 of 135 measured) and move "+
			"the verdict line of rows that already rendered one", deliveryProofOwedEvent)
	}
	// The list is built from the constant (Rule 1b/13), so a rename cannot
	// leave a stale spelling behind.
	if len(deliveryOwedTrailEventNames) == 0 {
		t.Fatal("deliveryOwedTrailEventNames is empty — a name test over an empty list proves nothing")
	}
	for _, name := range deliveryOwedTrailEventNames {
		if strings.TrimSpace(name) == "" {
			t.Errorf("deliveryOwedTrailEventNames carries an empty entry")
		}
	}
}

// TestDeliveryOwedAxisRendersNothingOnACleanRow is the silence half: a row
// that owes nothing renders no delivery line — otherwise the label would be
// noise on every row in the ledger, and "the debt is gone" would be
// indistinguishable from "the label is always printed".
func TestDeliveryOwedAxisRendersNothingOnACleanRow(t *testing.T) {
	const id = "01a20000-0000-7000-8000-000000000203"
	dbPath := testutil.NewTestDBPath(t)
	dpSeedRow(t, dbPath, id, "feat: a row with nothing owed", taskStatusInProgress)
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	out := dpOwedShow(t, dbPath, id)
	if strings.Contains(out, TaskShowDeliveryOwedLabel) {
		t.Errorf("a row with no delivery debt must render no %s line:\n%s", TaskShowDeliveryOwedLabel, out)
	}
}
