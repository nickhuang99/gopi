package agent

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// THE PIN: the ledger row a bare-prompt run auto-registers is NOT DISPATCHABLE.
//
// Epic 01a0deaa (claimability of the auto-registered row from a bare CLI
// prompt). A bare `gopi` run whose epic is unvalidated, non-briefing text —
// the repro the epic names is `gopi --gopi-prompt 1 version` — reaches
// maybeRegisterSessionTask (internal/agent/taskregister.go), which mints a
// real ledger row and then runs a full agent turn against the epic. The epic
// asked whether such a row is CLAIMABLE by the dispatch funnel.
//
// Measured verdict: NOT claimable, and by CONSTRUCTION rather than by any
// payload predicate. The row is born in_progress — TaskNewOID forces
// `opts.Status = taskStatusInProgress` (internal/tools/tasks.go) and the hook
// says so in as many words ("the row is inserted directly in_progress with NO
// claim") — while the claim funnel reads PENDING rows only: `SELECT ... FROM
// tasks WHERE status = ?` with taskStatusPending (TaskNextFiltered,
// internal/tools/tasks.go; taskNextClaimID uses the same WHERE, and so does
// TaskList's pending view). A row no funnel query ever SELECTS cannot be
// claimed, whatever its payload says.
//
// WHY THIS TEST EXISTS AT ALL, given the two gates above are independent. The
// auto row's payload is EMPTY by a deliberate, documented contract (the
// briefing lives in the session JSONL; a payload mirror would double-store
// every prompt), so it is ALSO excluded by the payload-less arm of
// taskRowClaimable. Two guards agreeing is exactly the situation in which a
// later change removes one of them and nobody notices: a row that became
// pending — a sync rollback walks claimed/in_progress → pending, see
// syncClaimRemoteRevert in internal/tools/memory_sync_claim.go — would be
// silently dispatchable, and a worker would be spawned onto a row whose entire
// briefing is the single character "1". This test pins BOTH ends, so "the auto
// row is undispatchable" is an asserted contract rather than an accident of
// the birth status.
//
// It is a control-pair test on purpose: a control row created THROUGH THE REAL
// SUBMIT FUNNEL (a genuine briefing, therefore a payload) IS claimed by the
// very same funnel call in the same process and ledger. That is what makes the
// negative assertion mean something — the funnel is live and looking at this
// ledger; it simply never sees the auto row.
//
// ORDER MATTERS: TaskNextFiltered CLAIMS (it stamps claimed_by and moves the
// row out of pending), so the marker assertions are all made BEFORE the
// control is claimed — otherwise the pending view is empty by construction and
// the [next] marker would be checked against a queue the test itself drained.
// ---------------------------------------------------------------------------

const (
	// testClaimHolder is the holder label this test stamps on its claim. A
	// label only: nothing else in this test's fresh ledger is claimed by
	// anyone, so there is no contender to route around.
	testClaimHolder = "claimability-pin"
	// controlRowID pins the control row's ledger id, because TaskSubmit
	// returns a human confirmation STRING ("Task queued: <id> …"), not the id
	// — parsing a display message back into an identity is the kind of
	// coupling a test should not depend on (Rule 1: an identity is passed,
	// never re-extracted from prose).
	controlRowID = "01a0decf-0000-7000-8000-0000000000c0"
	// controlRowPayload is the control row's briefing. Its only job is to be
	// NON-EMPTY: a payload is what the claim funnel's payload arm requires.
	controlRowPayload = "This row carries a payload, so the claim funnel must hand it back."
)

func TestAutoRegisteredRunRowIsNotClaimable(t *testing.T) {
	// The ledger pin is the HELPER's, not a second one here: newAutoTaskAgent
	// sets GOPI_HOME and GOPI_MEMORY_DB to a fresh per-test profile and DB
	// (Rule 32's writer scaffold, via the tools layer's own DSN), so the rows
	// this test mints can never reach the operator's append-only ledger. A
	// second pin in this body would be silently overridden by the helper's
	// and would read as a guarantee it is not (Rule 15).

	// --- Arm 1: the auto-registered row of a bare-prompt run. ---------------
	a := newAutoTaskAgent(t)
	// The epic's own repro payload — a non-briefing string that is nonetheless
	// non-empty, so the hook's own emptiness gate (a summary that is nothing
	// but AutoTaskSummaryPrefix) does NOT fire and a row really is minted.
	// This is the shape measured on the live surface: `summary: [auto] 1`.
	const nonBriefingEpic = "1"
	a.maybeRegisterSessionTask(nonBriefingEpic)
	if !a.autoTaskRegistered || a.autoTaskRowID == "" {
		t.Fatalf("the start hook registered no row for epic %q — the repro premise no longer holds", nonBriefingEpic)
	}
	autoID := a.autoTaskRowID

	// (a) the row exists and is born in_progress — the FIRST gate, and the
	// one that makes the funnel's WHERE clause miss it entirely.
	status, err := tools.TaskStatusOf(autoID)
	if err != nil {
		t.Fatalf("TaskStatusOf(%s): %v", autoID, err)
	}
	if status != tools.TaskStatusInProgress {
		t.Errorf("auto row status = %q, want %q (the birth status IS the claim gate: the funnel reads pending only)",
			status, tools.TaskStatusInProgress)
	}

	// (b) the funnel's OWN verdict: with the auto row as the only row in this
	// ledger it must report an EMPTY queue. A returned row here IS the defect
	// this test exists to catch.
	res, err := tools.TaskNextFiltered(testClaimHolder, "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered: %v", err)
	}
	if res.Task != nil && res.Task.TaskID == autoID {
		t.Fatalf("THE CLAIM FUNNEL TOOK THE AUTO-REGISTERED ROW %s — a bare `gopi --gopi-prompt 1 version` run is now dispatchable work", autoID)
	}
	if !res.Empty || res.Task != nil {
		t.Fatalf("claim funnel returned a row from a ledger holding only the auto row: empty=%v", res.Empty)
	}

	// (c) the LEDGER is the same door, and it is the one an operator reads.
	pending, err := tools.TaskList(tools.TaskStatusPending)
	if err != nil {
		t.Fatalf("TaskList(%s): %v", tools.TaskStatusPending, err)
	}
	if strings.Contains(pending, autoID) {
		t.Errorf("the auto row is PENDING — it is dispatchable-and-visible:\n%s", pending)
	}
	// (d) it is visible in the disk-state view, which is where it belongs: an
	// audit carrier, not work.
	all, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList(all): %v", err)
	}
	if !strings.Contains(all, autoID) {
		t.Errorf("the auto row vanished from the ledger view — the audit carrier was lost:\n%s", all)
	}
	if !strings.Contains(all, "[via "+runcmd.MethodAuto+"]") {
		t.Errorf("the auto row lost its [via %s] run provenance:\n%s", runcmd.MethodAuto, all)
	}

	// --- Arm 2: the CONTROL, submitted through the real submit funnel. ------
	// Still UNCLAIMED at this point, so the pending view must carry it.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		TaskID:  controlRowID,
		Summary: "control row: a real briefing, submitted through the real submit funnel",
		Payload: controlRowPayload,
		Model:   "test-model",
	}); err != nil {
		t.Fatalf("TaskSubmit (control): %v", err)
	}

	// (e) funnel and view AGREE, asserted while both are still observable: the
	// control row is pending and marked [next] — the marker is the funnel's
	// own verdict rendered, so a disagreement here is the Rule 33 defect — and
	// the auto row is absent from the pending view entirely. TaskQueueSkipMarker
	// is rendered only on a PENDING row, which is why "not pending" and "not
	// dispatchable" are one statement for the auto row.
	pending2, err := tools.TaskList(tools.TaskStatusPending)
	if err != nil {
		t.Fatalf("TaskList(%s) after control submit: %v", tools.TaskStatusPending, err)
	}
	if !strings.Contains(pending2, controlRowID) {
		t.Errorf("the control row is missing from the pending view:\n%s", pending2)
	}
	if !strings.Contains(pending2, tools.TaskQueueNextMarker) {
		t.Errorf("the pending view marks no row %q although the funnel will claim one:\n%s", tools.TaskQueueNextMarker, pending2)
	}
	if strings.Contains(pending2, autoID) {
		t.Errorf("the auto row appeared in the pending view once a claimable row existed:\n%s", pending2)
	}

	// --- Arm 3: the control row IS claimed by the very same funnel call. ----
	// If this fails, the funnel is dead in this process and arm 1's negative
	// result proves nothing about the auto row. LAST, because it mutates.
	res2, err := tools.TaskNextFiltered(testClaimHolder, "", "")
	if err != nil {
		t.Fatalf("TaskNextFiltered (control): %v", err)
	}
	if res2.Empty || res2.Task == nil {
		t.Fatal("the funnel claims NOTHING in this ledger — the control row is un-claimable, so arm 1 proves nothing about the auto row")
	}
	if res2.Task.TaskID != controlRowID {
		t.Errorf("claim funnel took %s, want the control row %s", res2.Task.TaskID, controlRowID)
	}
}
