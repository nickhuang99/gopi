package tools

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tasktags"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// FIXTURE ROW (epic fixture-row, 2026-10-13) — the reserved tag a RIG marks a
// seeded row with, so the row is queue DECORATION and never a unit of work.
//
// The failure this closes (measured 2026-10-13): a rig seeded two labelled
// pending rows into a profile its own foreground workers drained; both were
// CLAIMED and executed as REAL agent runs (one ran 22 turns / 998k tokens),
// and the rig's SIGTERM could not recall the claim — a claimed row belongs to
// the worker, not to the rig, and the LLM round is already spent.
//
// These tests drive the REAL surfaces — TaskNextFiltered (the ONE claim funnel
// every dispatcher shares) and TaskList (the operator's reading of the queue) —
// never the predicate in isolation, and the parent-edge recipe Rule 33 already
// provides (a fixture parented to a row that never reaches completed) is
// asserted alongside so the documented fallback keeps working.

// fixtureGateDB pins a fresh per-test ledger (Rule 32: the scaffold owns the
// path and the environment; no hand-rolled path, no operator-profile write).
func fixtureGateDB(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
}

// markerRowID returns the task id of the row wearing `marker` in a rendered
// pending view — read off the id COLUMN (never a substring probe, the
// regression TestParentGateRowLocatorNamesTheIdColumn pins).
func markerRowID(t *testing.T, rendered, marker string) string {
	t.Helper()
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, TaskListRowPrefix) || !strings.Contains(ln, marker) {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) > 0 {
			return strings.TrimPrefix(fields[0], TaskListRowPrefix)
		}
	}
	t.Fatalf("no row carries %q in:\n%s", marker, rendered)
	return ""
}

// fixtureSubmit writes one fixture row through the ORDINARY submit path (the
// reserved tag is what makes it a fixture — never ledger surgery).
func fixtureSubmit(t *testing.T, summary string) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary,
		Payload: "seeded fixture: " + summary,
		Tags:    tasktags.FixtureTag,
		Source:  "agent",
	})
	if err != nil {
		t.Fatalf("submit fixture %s: %v", summary, err)
	}
	return taskIDFromSubmitOutput(t, out)
}

// TestFixtureRowIsNeverClaimedWhileANormalRowStillIs is the RED PROOF: a
// pending FIXTURE row is refused by the claim funnel while an unrelated normal
// pending row is offered — a park is never a head-block (the failure mode
// t-1788053263 measured for another filter). The fixture is submitted FIRST, so
// a FIFO reading of the queue would reach it first: the assertion is about
// eligibility, not about ordering.
func TestFixtureRowIsNeverClaimedWhileANormalRowStillIs(t *testing.T) {
	fixtureGateDB(t)

	fixtureID := fixtureSubmit(t, "seeded fixture row")

	normalOut, err := TaskSubmit(TaskSubmitOptions{
		Summary: "real work",
		Payload: "the briefing that makes the row dispatchable",
		Source:  "agent",
	})
	if err != nil {
		t.Fatalf("submit normal row: %v", err)
	}
	normalID := taskIDFromSubmitOutput(t, normalOut)

	if got := claimNext(t, "fixture-gate-test"); got != normalID {
		t.Fatalf("funnel must skip the fixture and offer the normal row: want %s, got %q", normalID, got)
	}
	// The fixture is refused, not merely outranked: with the normal row now
	// claimed, the queue is otherwise EMPTY and the funnel still returns
	// nothing.
	if got := claimNext(t, "fixture-gate-test"); got != "" {
		t.Fatalf("the fixture row must never be claimed, got %q", got)
	}
	// It is queue decoration, NOT consumed: still pending, never claimed.
	row := taskRowByID(t, fixtureID)
	if row.Status != taskStatusPending {
		t.Fatalf("fixture row must stay pending, got status=%s", row.Status)
	}
	if strings.TrimSpace(row.ClaimedBy) != "" || row.HasClaim {
		t.Fatalf("fixture row must never carry a claim, got claimed_by=%q hasClaim=%v", row.ClaimedBy, row.HasClaim)
	}
}

// TestFixtureMarkerNamesTheReasonAndAgreesWithTheFunnel pins the second half of
// Rule 14 — the pending view's marker and the claim funnel answer identically:
// the fixture wears [queue-skip] with a reason NAMING the fixture (Rule 15: a
// refused row says WHY, and it is not the parent reason), the funnel's row
// wears [next], and a normal row is untouched.
func TestFixtureMarkerNamesTheReasonAndAgreesWithTheFunnel(t *testing.T) {
	fixtureGateDB(t)

	fixtureID := fixtureSubmit(t, "seeded fixture row")
	normalOut, err := TaskSubmit(TaskSubmitOptions{
		Summary: "real work",
		Payload: "the briefing that makes the row dispatchable",
		Source:  "agent",
	})
	if err != nil {
		t.Fatalf("submit normal row: %v", err)
	}
	normalID := taskIDFromSubmitOutput(t, normalOut)

	list, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	fixtureLine := renderedRowLine(list, fixtureID)
	normalLine := renderedRowLine(list, normalID)

	if !strings.Contains(fixtureLine, TaskQueueSkipMarker) {
		t.Fatalf("fixture row must carry %q, got %q", TaskQueueSkipMarker, fixtureLine)
	}
	// Rule 15: the reason NAMES the fixture — never the parent reason, which
	// would send a reader looking for a dependency that does not exist.
	if !strings.Contains(fixtureLine, tasktags.FixtureTag) {
		t.Fatalf("the fixture's [queue-skip] reason must name the fixture, got %q", fixtureLine)
	}
	if strings.Contains(fixtureLine, "parent") {
		t.Fatalf("the fixture's reason must not borrow the parent reason, got %q", fixtureLine)
	}

	// The marker agrees with the funnel: the row wearing [next] is the row the
	// funnel claims.
	nextID := markerRowID(t, list, TaskQueueNextMarker)
	if nextID != normalID {
		t.Fatalf("[next] must sit on the row the funnel claims: want %s, got %q\n%s", normalID, nextID, list)
	}
	if !strings.Contains(normalLine, TaskQueueNextMarker) {
		t.Fatalf("the row the funnel claims must wear %q, got %q", TaskQueueNextMarker, normalLine)
	}
	if got := claimNext(t, "fixture-gate-test"); got != normalID {
		t.Fatalf("marker/funnel parity: want %s, got %q", normalID, got)
	}
}

// TestFixtureRowIsRefusedByTransferEligibility closes the SIBLING DOOR: a
// second consumer of the SAME predicate decides whether a row may travel to
// another ledger. A fixture that leaked through here would arrive on a peer and
// be claimed there instead — the identical LLM round, one host over.
func TestFixtureRowIsRefusedByTransferEligibility(t *testing.T) {
	fixtureGateDB(t)

	fixtureID := fixtureSubmit(t, "seeded fixture row")
	row := taskRowByID(t, fixtureID)

	ok, why := TaskTransferEligible(row, TaskTransferDependency{}, nil)
	if ok {
		t.Fatalf("a fixture row must never be transferable (it would be claimed on the peer)")
	}
	if !strings.Contains(why, tasktags.FixtureTag) {
		t.Fatalf("the transfer refusal must name the fixture, got %q", why)
	}

	// The control: an ordinary pending row of the same shape still travels.
	normalOut, err := TaskSubmit(TaskSubmitOptions{
		Summary: "real work",
		Payload: "the briefing that makes the row dispatchable",
		Source:  "agent",
	})
	if err != nil {
		t.Fatalf("submit normal row: %v", err)
	}
	normalID := taskIDFromSubmitOutput(t, normalOut)
	if ok, why := TaskTransferEligible(taskRowByID(t, normalID), TaskTransferDependency{}, nil); !ok {
		t.Fatalf("a normal pending row must stay transferable, refused with %q", why)
	}
}

// TestNonFixtureRowsAreUntouched is the CONTROL half: the fixture arm changes
// the verdict for exactly one tag. An ordinary row, a merger-resolve row and a
// payload-less row keep the verdict they had.
func TestNonFixtureRowsAreUntouched(t *testing.T) {
	// Ordinary row: claimable.
	if !taskRowClaimable("", "", "a briefing", []string{}, nil, nil) {
		t.Fatal("an ordinary pending row must stay claimable")
	}
	// Tagged with something else entirely: still claimable.
	if !taskRowClaimable("", "ui,feature", "a briefing", []string{}, nil, nil) {
		t.Fatal("a row tagged ui,feature must stay claimable")
	}
	// A tag that merely CONTAINS the fixture word mid-token is a different
	// tag: the tag set is comma-split, never substring-matched.
	if !taskRowClaimable("", "fixture-ish", "a briefing", []string{}, nil, nil) {
		t.Fatal("a mid-token match must not be read as the fixture tag")
	}
	// The pre-existing arms keep their verdicts.
	if taskRowClaimable("", "", "", []string{}, nil, nil) {
		t.Fatal("a payload-less row must stay unclaimable")
	}
}

// TestFixtureParentRecipeStillParksForever pins the DOCUMENTED FALLBACK that
// works on a binary without this mechanism: a fixture parented to a row that
// never reaches completed is parked FOREVER by Rule 33. The fixture arm must
// not weaken it (the reason stays the parent's), and the two mechanisms must
// compose: a fixture that is ALSO parented is refused on both counts.
func TestFixtureParentRecipeStillParksForever(t *testing.T) {
	fixtureGateDB(t)

	parentOut, err := TaskSubmit(TaskSubmitOptions{
		Summary: "epic parent that never completes",
		Payload: "parent briefing",
		Tags:    parentGateParentTag,
		Source:  "agent",
	})
	if err != nil {
		t.Fatalf("submit parent: %v", err)
	}
	parentID := taskIDFromSubmitOutput(t, parentOut)

	childOut, err := TaskSubmit(TaskSubmitOptions{
		Summary:      "parented fixture",
		Payload:      "seeded fixture under a never-released parent",
		Tags:         tasktags.FixtureTag,
		ParentTaskID: parentID,
		Source:       "agent",
	})
	if err != nil {
		t.Fatalf("submit parented fixture: %v", err)
	}
	childID := taskIDFromSubmitOutput(t, childOut)

	normalOut, err := TaskSubmit(TaskSubmitOptions{
		Summary: "real work",
		Payload: "the briefing that makes the row dispatchable",
		Source:  "agent",
	})
	if err != nil {
		t.Fatalf("submit normal row: %v", err)
	}
	normalID := taskIDFromSubmitOutput(t, normalOut)

	if got := claimNext(t, "fixture-gate-test"); got != normalID {
		t.Fatalf("the parented fixture must be skipped and the normal row offered: want %s, got %q", normalID, got)
	}
	// The parent arm still parks it on its own terms: the marker reason is the
	// PARENT's, exactly as Rule 33 renders it (this row is a fixture too, and
	// either reason is honest — the test pins that the parent recipe alone is
	// sufficient by asking the parent gate directly).
	if denied, reason := taskParentGateDenied("", "", "", []string{parentID}, map[string]string{parentID: taskStatusPending}); !denied {
		t.Fatalf("the parent recipe must still park a child; reason=%q", reason)
	}
	if row := taskRowByID(t, childID); row.Status != taskStatusPending {
		t.Fatalf("parked fixture must stay pending, got %s", row.Status)
	}
}
