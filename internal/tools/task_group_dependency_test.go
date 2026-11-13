package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// GROUP-COMPLETION DEPENDENCY (Rule 33's third edge): a row can declare
// "wait until EVERY row of this group is terminal" in its payload.
//
// THE MINIMAL REPRODUCTION, and the reason the arm exists at all: a
// DISPATCHER parent's work is to submit its children, so its `completed`
// status says "everything is dispatched" and RELEASES every sibling at once —
// including the CONSUMER that reads the batch. Measured 2026-11-05 (session
// 1918704d, episode competitor-analysis 阶段4): the parent closed at 23 rows
// dispatched, the consumer went to [next], and 0 of the 23 `gap-*.md` inputs
// existed. parent_task_id cannot express "wait for a SET that is not my
// ancestors", so the payload declares it:
//
//	task_group: <prefix>[,<prefix>...]
//
// The tests below drive the REAL surfaces — TaskNextFiltered (the funnel every
// dispatcher shares), TaskList (the operator's reading surface), and
// taskParentStatuses (the index the gate is actually handed) — because a
// predicate that is right in isolation and unreachable is exactly what Rule 31
// calls "not delivered".

// groupTestPrefix is the one prefix every fixture row in this file is minted
// under, so "the group" is unambiguous and a leftover row from another test
// could never join it by accident.
const groupTestPrefix = "t-groupdep-"

// groupTestRow plants a row by DIRECT ledger write — the path a synced or
// hand-planted row takes, and the only way to give a fixture a chosen task_id
// (the producer ids must be knowable before the consumer's payload is written).
func groupTestRow(t *testing.T, id, status, payload, tags string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID:  id,
		Summary: "group-dependency fixture " + id,
		Status:  status,
		Payload: payload,
		Tags:    tags,
		Source:  "auto",
	}); err != nil {
		t.Fatalf("insert %s (%s): %v", id, status, err)
	}
}

// groupTestSetStatus moves a row through the ledger's OWN transition gate
// (never a bare UPDATE), so a fixture cannot reach a state a real row could
// not.
func groupTestSetStatus(t *testing.T, id string, chain ...string) {
	t.Helper()
	for _, st := range chain {
		if _, err := TaskUpdate(id, st); err != nil {
			t.Fatalf("%s → %s: %v", id, st, err)
		}
	}
}

// groupTestIndex is the id → status map the gate is handed, built by the
// SAME function the funnel and the render use — a hand-typed map would mark
// the fixture's own homework.
func groupTestIndex(t *testing.T) map[string]string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	idx, err := taskParentStatuses(ctx, db)
	if err != nil {
		t.Fatalf("taskParentStatuses: %v", err)
	}
	return idx
}

// TestGroupDependencyMinimalRepro is the epic's red→green sample, at the
// smallest size that has the defect: P (the dispatcher) completes with A and B
// still open, and C — P's sibling, declaring the group of A and B — must NOT be
// claimable, must say how many are outstanding, and must BECOME claimable once
// A and B are terminal. Both halves are asserted: a fix that only ever refuses
// would starve the queue, which is the failure mode this arm must not create.
func TestGroupDependencyMinimalRepro(t *testing.T) {
	parentGateDB(t)

	// P is the dispatcher parent and it CLOSES FIRST — the whole premise.
	// It is filed under the group's own prefix, which is the ordinary case
	// (a dispatcher submits its children in one session-id family), and it
	// proves the criterion is TERMINAL rather than "not pending": P's own
	// terminal status must not count against the group.
	pID := groupTestPrefix + "p"
	groupTestRow(t, pID, taskStatusPending, "dispatcher parent briefing", parentGateParentTag)
	groupTestSetStatus(t, pID, taskStatusInProgress, taskStatusCompleted)

	aID := groupTestPrefix + "a"
	bID := groupTestPrefix + "b"
	groupTestRow(t, aID, taskStatusPending, "producer A briefing", parentGateParentTag)
	groupTestRow(t, bID, taskStatusPending, "producer B briefing", parentGateParentTag)

	cID := groupTestPrefix + "c"
	consumerPayload := "consumer briefing\n" + TaskGroupDependencyKey + " " + groupTestPrefix
	groupTestRow(t, cID, taskStatusPending, consumerPayload, "")

	// ① RED: the group is 0 of 2 done, so C is in no dispatcher's reach — and
	// the refusal NAMES the difference rather than looking starved.
	idx := groupTestIndex(t)
	declaration := "consumer briefing\n" + TaskGroupDependencyKey + " " + groupTestPrefix
	denied, reason := taskParentGateDenied(cID, "", declaration, nil, idx)
	if !denied {
		t.Fatalf("C must be parked while A and B are open: the parent gate released it on P's completion alone")
	}
	if !strings.Contains(reason, "waits for 2 of 3") {
		t.Errorf("the refusal must count the outstanding members (P is terminal, so 3 members of which 2 are open), got %q", reason)
	}
	if !strings.Contains(reason, aID) || !strings.Contains(reason, bID) {
		t.Errorf("the refusal must NAME the members still open (%s / %s), got %q", aID, bID, reason)
	}
	// The funnel, end to end: C is not handed out.
	res, err := TaskNextFiltered("groupdep-test", "", parentGateParentTag)
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if !res.Empty {
		t.Fatalf("the funnel claimed %q — a group-declaring row must not be offered while its group is open", res.Task.TaskID)
	}
	// The pending view marks it, with the SAME reason (Rule 14: one predicate).
	list, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(groupTestRowLine(list, cID), string(TaskQueueSkipMarker)) {
		t.Errorf("the pending view must mark C [queue-skip]; got:\n%s", list)
	}
	if !strings.Contains(groupTestRowLine(list, cID), "waits for 2 of 3") {
		t.Errorf("the pending view's marker must carry the group's reason; got:\n%s", list)
	}

	// ② GREEN: A and B reach terminal statuses — completed and CANCELLED,
	// deliberately different ones, because the criterion is TERMINAL and not
	// the release set (a cancelled producer still finished its attempt; that
	// is the difference between this edge and the parent edge).
	groupTestSetStatus(t, aID, taskStatusInProgress, taskStatusCompleted)
	groupTestSetStatus(t, bID, taskStatusCancelled)

	idx = groupTestIndex(t)
	if denied, reason := taskParentGateDenied(cID, "", declaration, nil, idx); denied {
		t.Fatalf("C must be released once every group member is terminal, still denied: %q", reason)
	}
	res, err = TaskNextFiltered("groupdep-test", "", parentGateParentTag)
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res.Empty || res.Task.TaskID != cID {
		t.Fatalf("the funnel must hand out C once its group is terminal; got %+v", res)
	}
}

// TestGroupDependencySelfIsExcluded pins the self-exclusion: a dispatcher that
// files its consumer under the SAME family prefix (the ordinary spelling —
// "this session's rows") must not deadlock on its own id. Without this arm the
// first correct usage would park forever, which is precisely the failure a
// careful author introduces when adding a check.
func TestGroupDependencySelfIsExcluded(t *testing.T) {
	parentGateDB(t)

	aID := groupTestPrefix + "solo-a"
	groupTestRow(t, aID, taskStatusPending, "the only producer", parentGateParentTag)
	cID := groupTestPrefix + "solo-c"
	groupTestRow(t, cID, taskStatusPending, "consumer\n"+TaskGroupDependencyKey+" "+groupTestPrefix, "")

	idx := groupTestIndex(t)
	declaration := "consumer briefing\n" + TaskGroupDependencyKey + " " + groupTestPrefix
	denied, reason := taskParentGateDenied(cID, "", declaration, nil, idx)
	if !denied || !strings.Contains(reason, "waits for 1 of 1") {
		t.Fatalf("C must wait for the one OTHER member of its own group (self excluded), got denied=%v reason=%q", denied, reason)
	}
	groupTestSetStatus(t, aID, taskStatusInProgress, taskStatusCompleted)
	if denied, reason := taskParentGateDenied(cID, "", declaration, nil, idx2(t)); denied {
		t.Fatalf("C must not wait on ITSELF: %q", reason)
	}
}

// groupTestRowLine returns the ONE rendered line for a row (the render carries
// no marker beside the id: the queue marker rides the end of its own line), so
// an assertion about a MARKER judges that row rather than the whole page.
func groupTestRowLine(rendered, id string) string {
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "#"+id) {
			return line
		}
	}
	return ""
}

// idx2 re-reads the index after a status move (a fixture-level convenience so
// each assertion judges the ledger as it stands, never a stale map).
func idx2(t *testing.T) map[string]string { return groupTestIndex(t) }

// TestGroupDependencyEmptyGroupIsNeverSatisfied pins the second state of the
// refusal, and it is the one that matters most: a row that DECLARES a
// dependency resolving to nothing must be parked, not released. The dangerous
// alternative is the silent one — treating "no members" as "no dependency",
// which would let a typo (or a dispatcher that never submitted) look
// satisfied.
func TestGroupDependencyEmptyGroupIsNeverSatisfied(t *testing.T) {
	parentGateDB(t)

	declaredButEmpty := groupTestPrefix + "empty-declared"
	groupTestRow(t, declaredButEmpty, taskStatusPending, "consumer\n"+TaskGroupDependencyKey, "")
	declaredNoMembers := groupTestPrefix + "empty-nomatch"
	groupTestRow(t, declaredNoMembers, taskStatusPending, "consumer\n"+TaskGroupDependencyKey+" t-nothing-matches-this-", "")

	idx := groupTestIndex(t)
	for _, tc := range []struct{ id, payload string }{
		{declaredButEmpty, "consumer\n" + TaskGroupDependencyKey},
		{declaredNoMembers, "consumer\n" + TaskGroupDependencyKey + " t-nothing-matches-this-"},
	} {
		id := tc.id
		denied, reason := taskParentGateDenied(id, "", tc.payload, nil, idx)
		if !denied {
			t.Fatalf("%s declares a group that resolves to nothing — an empty dependency is never satisfied", id)
		}
		if !strings.Contains(reason, "matches NO row") {
			t.Errorf("%s: the refusal must say the group is empty, got %q", id, reason)
		}
	}
	// A row that declares NOTHING is untouched: this arm must not make
	// ordinary rows unclaimable.
	plain := groupTestPrefix + "plain"
	groupTestRow(t, plain, taskStatusPending, "an ordinary row with no declaration", "")
	if denied, reason := taskParentGateDenied(plain, "", "", nil, idx); denied {
		t.Fatalf("a row that declares no group carries no group edge, got %q", reason)
	}
	if got := taskRowClaimable(plain, "", "an ordinary row with no declaration", nil, nil, nil); !got {
		t.Fatalf("a group-less row must stay claimable even with a nil index")
	}
}

// TestGroupDependencyNilIndexDenies pins Rule 15's arm: a row that declares a
// group must never be judged "fine" by a caller that could not read the
// ledger. It is denied, and the reason says the ledger was unreadable — the
// three distinguishable states the epic asks for are incomplete / empty /
// unreadable, and this is the third.
func TestGroupDependencyNilIndexDenies(t *testing.T) {
	declaring := "consumer\n" + TaskGroupDependencyKey + " t-anything-"
	denied, reason := taskParentGateDenied("t-self", "", declaring, nil, nil)
	if !denied {
		t.Fatalf("an unreadable ledger must deny a group-declaring row")
	}
	if reason != taskGroupReasonUnreadable {
		t.Fatalf("the refusal must say the ledger was unreadable, got %q", reason)
	}
	// And a row that declares nothing is STILL claimable with a nil index —
	// the nil arm belongs to the declaration, not to the row.
	if denied, reason := taskParentGateDenied("t-self", "", "no declaration", nil, nil); denied {
		t.Fatalf("a group-less row must not be denied by a nil index, got %q", reason)
	}
}

// TestGroupDependencyCoexistsWithParentGate pins that the two edges AND
// together and name BOTH causes: a row that is a child of an unreleased
// parent AND declares an open group reads both sentences, joined by the one
// separator the gate already uses. Whis is the compatibility requirement —
// adding the group arm must not let a parent-parked row look released, and
// must not swallow the parent's own reason.
func TestGroupDependencyCoexistsWithParentGate(t *testing.T) {
	parentGateDB(t)

	unreleasedParent := groupTestPrefix + "parent-open"
	groupTestRow(t, unreleasedParent, taskStatusPending, "a parent that has not been released", parentGateParentTag)
	producerID := groupTestPrefix + "prod-open"
	groupTestRow(t, producerID, taskStatusPending, "an open producer", parentGateParentTag)

	childID := groupTestPrefix + "child"
	childPayload := "child briefing\n" + TaskGroupDependencyKey + " " + groupTestPrefix
	groupTestRow(t, childID, taskStatusPending, childPayload, "")

	idx := groupTestIndex(t)
	denied, reason := taskParentGateDenied(childID, "", "child briefing\n"+TaskGroupDependencyKey+" "+groupTestPrefix+"\n",
		[]string{unreleasedParent}, idx)
	if !denied {
		t.Fatalf("a row parked by BOTH edges must be denied")
	}
	if !strings.Contains(reason, unreleasedParent) {
		t.Errorf("the parent's cause must survive beside the group's, got %q", reason)
	}
	if !strings.Contains(reason, "waits for") {
		t.Errorf("the group's cause must be reported too, got %q", reason)
	}
	// The parent sentence carries its OWN "; " inside "(status=…; releases
	// on …)", so counting separators is not how the join is checked: the
	// LAST segment must be the group's sentence and an earlier one must be
	// the parent's, which is what "joined by the one separator" means.
	parts := strings.Split(reason, taskParentsReasonSeparator)
	if len(parts) < 2 {
		t.Fatalf("the two causes must be joined by %q, got %q", taskParentsReasonSeparator, reason)
	}
	if !strings.Contains(parts[len(parts)-1], "waits for") {
		t.Errorf("the group's sentence must be its own segment, got %q", reason)
	}
	if !strings.Contains(parts[0], unreleasedParent) {
		t.Errorf("the parent's sentence must be its own segment, got %q", reason)
	}

	// The parent edge alone still decides exactly as it did before this arm
	// existed: release the parent, and the reason must become the GROUP's
	// sentence only (proving neither edge answers for the other).
	groupTestSetStatus(t, unreleasedParent, taskStatusInProgress, taskStatusCompleted)
	idx = groupTestIndex(t)
	denied, reason = taskParentGateDenied(childID, "", "child briefing\n"+TaskGroupDependencyKey+" "+groupTestPrefix+"\n",
		[]string{unreleasedParent}, idx)
	if !denied {
		t.Fatalf("the group edge must still park the row after the parent released")
	}
	if strings.Contains(reason, unreleasedParent) {
		t.Errorf("a released parent must vanish from the reason, got %q", reason)
	}
}

// TestGroupDependencyParserIsLineScoped pins the parser's boundary: the
// declaration is read LINE BY LINE, so a `task_group:` inside a pasted example
// does not silently activate a dependency, several lines accumulate, a
// repeated prefix is judged once, and a well-formed line is not silenced by a
// malformed neighbour.
func TestGroupDependencyParserIsLineScoped(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    []string
	}{
		{"absent", "a briefing with no declaration", nil},
		{"one", "briefing\n" + TaskGroupDependencyKey + " t-a-", []string{"t-a-"}},
		{"two lines", "briefing\n" + TaskGroupDependencyKey + " t-a-\n" + TaskGroupDependencyKey + " t-b-", []string{"t-a-", "t-b-"}},
		{"comma list", "briefing\n" + TaskGroupDependencyKey + " t-a-, t-b-", []string{"t-a-", "t-b-"}},
		{"dedupe", "briefing\n" + TaskGroupDependencyKey + " t-a-, t-a-", []string{"t-a-"}},
		{"empty declaration is one empty group", "briefing\n" + TaskGroupDependencyKey, []string{""}},
		{"indented still counts", "briefing\n   " + TaskGroupDependencyKey + " t-a-  ", []string{"t-a-"}},
		{"not a line prefix", "briefing\nnot " + TaskGroupDependencyKey + " t-a-", nil},
		{"malformed neighbour does not silence", "briefing\n" + TaskGroupDependencyKey + "\n" + TaskGroupDependencyKey + " t-b-", []string{"", "t-b-"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := taskGroupDependenciesOf(c.payload)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Fatalf("taskGroupDependenciesOf(%q) = %v, want %v", c.payload, got, c.want)
			}
		})
	}
}

// TestGroupDependencyIndexIsLoadedForGroupOnlyQueues pins the probe: a queue
// whose only dependency is a GROUP (no parent_task_id anywhere) must still load
// the id → status index, or the gate would be handed nil and park a satisfied
// row. This is the arm the parent-only probe got wrong, and it is asserted
// through the REAL funnel — the row is handed out, not merely "the predicate
// would allow it".
func TestGroupDependencyIndexIsLoadedForGroupOnlyQueues(t *testing.T) {
	parentGateDB(t)

	producer := groupTestPrefix + "probe-a"
	groupTestRow(t, producer, taskStatusPending, "producer", parentGateParentTag)
	consumer := groupTestPrefix + "probe-c"
	groupTestRow(t, consumer, taskStatusPending, "consumer\n"+TaskGroupDependencyKey+" "+groupTestPrefix, "")

	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	parented, err := taskPendingParentedCount(ctx, db)
	if err != nil {
		t.Fatalf("taskPendingParentedCount: %v", err)
	}
	if parented != 0 {
		t.Fatalf("fixture broken: no row here carries a parent_task_id, got %d", parented)
	}
	needed, err := taskDependencyIndexNeeded(ctx, db)
	if err != nil {
		t.Fatalf("taskDependencyIndexNeeded: %v", err)
	}
	if !needed {
		t.Fatalf("a queue carrying a group-declaring row must load the dependency index (the parent-only probe would have said no)")
	}

	// And end to end: satisfied group ⇒ the funnel hands the consumer out,
	// which is only possible if the index was actually loaded.
	groupTestSetStatus(t, producer, taskStatusInProgress, taskStatusCompleted)
	res, err := TaskNextFiltered("groupdep-test", "", parentGateParentTag)
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res.Empty || res.Task.TaskID != consumer {
		t.Fatalf("a satisfied group-only dependency must not park the row; got %+v", res)
	}
}
