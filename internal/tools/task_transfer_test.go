package tools

// task_transfer_test.go — the two-way predicate and the transfer's writes.
//
// THE TESTS ARE ORGANISED AS THE DELIVERABLE'S ASSERTION GROUPS, so a reader
// can map a failure straight back to the claim it breaks:
//
//	[5a] every filter arm of the predicate, one subtest each
//	[5b] the REVERSE edge (a live child) — the arm that is not derivable from
//	     the row's own fields
//	[5c] the handover write: CAS, refusal-by-name, and the predicate gate that a
//	     NAMED handover cannot smuggle past
//	[6]  the admission: the row the handover produced is claimed; every other
//	     shape is refused and SAID SO

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// transferTestRow is the one row these fixtures move (minimal sample).
	transferTestRow = "t-transfer-01"
	// transferTestChild is the row that may still depend on it.
	transferTestChild = "t-transfer-child"
)

// transferTestDB seeds an isolated ledger with one QUEUED row and switches the
// declared topology on (nothing here dials ssh — the peer side is called
// in-process).
func transferTestDB(t *testing.T) {
	t.Helper()
	testutil.NewTestDBPath(t)
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	if _, err := TaskNewOID(TaskNewOptions{
		Summary: "transfer probe",
		TaskID:  transferTestRow,
		Payload: "the row a transfer moves",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	rowLegForceStatus(t, transferTestRow, TaskStatusPending, float64(time.Now().Unix()))
}

// transferTestAnchors backdates the row and its child so the claim order is
// deterministic (the child is NEWER, which is what the "youngest live child"
// assertion reads).
func transferTestAnchors(t *testing.T, rowAt, childAt float64) {
	t.Helper()
	rowLegForceStatus(t, transferTestRow, TaskStatusPending, rowAt)
}

// transferTestAddChild seeds a child row pointing at the parent through the
// REAL column (parent_task_id), never a fixture-only path: the predicate reads
// the ledger, so the fixture must write what the ledger would.
func transferTestAddChild(t *testing.T, id, status string, createdAt float64) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{
		Summary:      "dependent row",
		TaskID:       id,
		Payload:      "a row that waits for its parent",
		Source:       "human",
		ParentTaskID: transferTestRow,
	}); err != nil {
		t.Fatalf("seed dependent row: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, created_at = ? WHERE task_id = ?`,
		status, createdAt, id); err != nil {
		t.Fatalf("force child state: %v", err)
	}
}

// transferSelect runs the peer-side selector against THIS ledger.
func transferSelect(t *testing.T, skip map[string]bool) *SyncTransferEligibleDoc {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	doc, err := TaskTransferSelectOne(context.Background(), db, skip, "")
	if err != nil {
		t.Fatalf("TaskTransferSelectOne: %v", err)
	}
	return doc
}

// --- [5a] the filter arms --------------------------------------------------

// TestTransferEligibilityFilterArms drives EVERY arm of the predicate, one
// subtest each, against the real ledger where an arm needs one and against a
// literal row where it does not.
//
// The table is the deliverable's own list, so an arm removed from the predicate
// fails HERE (the positive half) rather than being discovered later as a row
// that travelled when it should not have.
func TestTransferEligibilityFilterArms(t *testing.T) {
	skip := map[string]bool{"pa-queue": true}
	cases := []struct {
		name string
		row  *Task
		dep  TaskTransferDependency
		skip map[string]bool
		want bool
		// wantWhy asserts on the refusal's own words: an arm that refuses for
		// the WRONG reason is a different defect from one that admits.
		wantWhy string
	}{
		{
			name: "plain queued row travels",
			row:  &Task{TaskID: "t-a", Status: taskStatusPending, Payload: "p"},
			want: true,
		},
		{
			name:    "nil row",
			row:     nil,
			wantWhy: "no row was handed in",
		},
		{
			name:    "claimed row does not travel",
			row:     &Task{TaskID: "t-b", Status: taskStatusClaimed, Payload: "p"},
			wantWhy: taskStatusClaimed + ", not " + taskStatusPending,
		},
		{
			name:    "in_progress row does not travel",
			row:     &Task{TaskID: "t-b2", Status: taskStatusInProgress, Payload: "p"},
			wantWhy: taskStatusInProgress + ", not " + taskStatusPending,
		},
		{
			name:    "parented row does not travel",
			row:     &Task{TaskID: "t-c", Status: taskStatusPending, Payload: "p", ParentTaskID: "t-parent"},
			wantWhy: "parent_task_id t-parent",
		},
		{
			name:    "skip-tagged row does not travel",
			row:     &Task{TaskID: "t-d", Status: taskStatusPending, Payload: "p", Tags: "pa-queue"},
			skip:    skip,
			wantWhy: "which the receiving dispatcher SKIPS",
		},
		{
			name:    "payload-less row does not travel",
			row:     &Task{TaskID: "t-e", Status: taskStatusPending},
			wantWhy: "carries no payload",
		},
		{
			name:    "merger-resolve row does not travel",
			row:     &Task{TaskID: "t-f", Status: taskStatusPending, Payload: "p", Tags: taskTagMergeConflict + ",auto-spawn"},
			wantWhy: "MERGER-RESOLVE",
		},
		{
			name: "live child blocks it",
			row:  &Task{TaskID: "t-g", Status: taskStatusPending, Payload: "p"},
			dep:  TaskTransferDependency{Live: true, ChildID: transferTestChild, ChildStatus: taskStatusPending},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := TaskTransferEligible(tc.row, tc.dep, tc.skip)
			if tc.want && !ok {
				t.Fatalf("refused, want eligible: %s", why)
			}
			if !tc.want && ok {
				t.Fatalf("eligible, want refused")
			}
			if tc.wantWhy != "" && !strings.Contains(why, tc.wantWhy) {
				t.Fatalf("refusal %q does not carry %q — a refusal must name its own cause", why, tc.wantWhy)
			}
		})
	}
}

// --- [5b] the reverse edge --------------------------------------------------

// TestTransferLiveChildIsTheReverseHalf is the assertion group the epic calls
// out by name: the predicate is TWO-WAY, so the reverse edge must be read from
// the ledger rather than inferred from the row.
//
// A/B/C/D walk the four states a dependent can be in, and each one is a
// DIFFERENT verdict:
//
//	A  pending child      → blocks (moving the parent parks it forever)
//	B  in_progress child  → blocks (same edge, one status later)
//	C  completed child    → does not block (the edge is resolved)
//	D  cancelled child    → does not block (a cancelled child waits for nothing)
func TestTransferLiveChildIsTheReverseHalf(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		blocked bool
	}{
		{"A pending child blocks", TaskStatusPending, true},
		{"B in_progress child blocks", TaskStatusInProgress, true},
		{"C completed child does not block", TaskStatusCompleted, false},
		{"D cancelled child does not block", TaskStatusCancelled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transferTestDB(t)
			transferTestAddChild(t, transferTestChild, tc.status, float64(time.Now().Unix()))

			doc := transferSelect(t, nil)
			if tc.blocked {
				if doc.Eligible {
					t.Fatalf("the row travelled while %s child %s still depends on it — moving it would park that child forever (Rule 33)",
						tc.status, transferTestChild)
				}
				if !strings.Contains(doc.Reason, transferTestChild) {
					t.Fatalf("the refusal does not name the child to deal with: %q", doc.Reason)
				}
				return
			}
			if !doc.Eligible {
				t.Fatalf("the row was refused although its only dependent is %s: %s", tc.status, doc.Reason)
			}
			if doc.Row == nil || doc.Row.TaskID != transferTestRow {
				t.Fatalf("eligible but the offered row is %+v", doc.Row)
			}
		})
	}
}

// TestTransferLiveChildNamesTheFirstByClaimOrder pins WHICH dependent the
// refusal names when several exist: the FIRST by the ledger's own claim order
// (taskClaimOrderBy — priority, then created_at, then id), i.e. the row a
// dispatcher would reach next. The fixture makes the two orders DISAGREE on
// purpose (the older child is inserted first and is also the one claim order
// reaches first), so a query that silently ordered by rowid or by status would
// answer the other one and fail here.
func TestTransferLiveChildNamesTheYoungestDependent(t *testing.T) {
	transferTestDB(t)
	older := float64(time.Now().Unix()) - 600
	newer := float64(time.Now().Unix())
	transferTestAddChild(t, "t-transfer-child-old", TaskStatusPending, older)
	transferTestAddChild(t, transferTestChild, TaskStatusInProgress, newer)

	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	dep, err := TaskTransferLiveChild(context.Background(), db, transferTestRow)
	if err != nil {
		t.Fatalf("TaskTransferLiveChild: %v", err)
	}
	const firstByClaimOrder = "t-transfer-child-old"
	if !dep.Live || dep.ChildID != firstByClaimOrder {
		t.Fatalf("live child = %+v, want %s (the first in claim order)", dep, firstByClaimOrder)
	}
}

// TestTransferLiveChildIgnoresAFinishedDependent is the OTHER direction of the
// same query, asked directly (not through the sentence): a terminal child is
// not "live", so it must not even be found.
func TestTransferLiveChildIgnoresAFinishedDependent(t *testing.T) {
	transferTestDB(t)
	transferTestAddChild(t, transferTestChild, TaskStatusSuperseded, float64(time.Now().Unix()))
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	dep, err := TaskTransferLiveChild(context.Background(), db, transferTestRow)
	if err != nil {
		t.Fatalf("TaskTransferLiveChild: %v", err)
	}
	if dep.Live {
		t.Fatalf("a superseded dependent counted as live: %+v", dep)
	}
}

// --- [5c] the handover ------------------------------------------------------

// TestTransferHandoverSignsTheRowOver is the happy path, measured on the
// ledger: the row becomes cancelled, the CAS reports success, and the reason
// carries the handover's own line (which is ALSO the admission's key — the
// receiving side recognises its copy by it).
func TestTransferHandoverSignsTheRowOver(t *testing.T) {
	transferTestDB(t)
	if err := TaskTransferHandover(transferTestRow, syncRowTestPeer, "", ""); err != nil {
		t.Fatalf("TaskTransferHandover: %v", err)
	}
	if got := rowLegStatus(t, transferTestRow); got != TaskStatusCancelled {
		t.Fatalf("status = %q, want %q", got, TaskStatusCancelled)
	}
	row, err := taskStatusDirect(transferTestRow)
	if err != nil || row == nil {
		t.Fatalf("taskStatusDirect: %v (%v)", err, row)
	}
	if !strings.HasPrefix(row.Reason, taskTransferHandoverReasonPrefix) {
		t.Fatalf("the row's reason does not carry the handover's line: %q", row.Reason)
	}
	if !strings.Contains(row.Reason, syncRowTestPeer) {
		t.Fatalf("the row's reason does not name the peer it went to: %q", row.Reason)
	}
}

// TestTransferHandoverLosesTheCASByName is the race, MEASURED: a second writer
// moves the row first, and the handover must refuse BY NAME (the status it
// actually found), never silently retry or over-write.
func TestTransferHandoverLosesTheCASByName(t *testing.T) {
	transferTestDB(t)
	rowLegForceStatus(t, transferTestRow, TaskStatusClaimed, float64(time.Now().Unix()))

	err := TaskTransferHandover(transferTestRow, syncRowTestPeer, "", "")
	if err == nil {
		t.Fatalf("the handover signed over a row that is no longer pending")
	}
	var refusal *SyncTransferCASRefusal
	if !asCASRefusal(err, &refusal) {
		t.Fatalf("the refusal is not a CAS refusal: %T %v", err, err)
	}
	if refusal.Status != TaskStatusClaimed {
		t.Fatalf("the refusal says the row is %q, want the status that actually beat it", refusal.Status)
	}
	if got := rowLegStatus(t, transferTestRow); got != TaskStatusClaimed {
		t.Fatalf("the losing handover still changed the row: %s", got)
	}
}

// TestTransferHandoverEnforcesThePredicate is the gate a NAMED handover needs:
// the id travels on the command line without ever passing the selector, so the
// predicate must run at the WRITE point. Without this, a caller could name a
// row with a live dependent and park that child forever.
func TestTransferHandoverEnforcesThePredicate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shape  func(t *testing.T)
		expect string
	}{
		{
			name: "a live dependent refuses the named handover",
			shape: func(t *testing.T) {
				transferTestAddChild(t, transferTestChild, TaskStatusPending, float64(time.Now().Unix()))
			},
			expect: transferTestChild,
		},
		{
			name: "a parented row refuses the named handover",
			shape: func(t *testing.T) {
				db, err := openTasksDB(context.Background(), memoryDBPath())
				if err != nil {
					t.Fatalf("open ledger: %v", err)
				}
				defer db.Close()
				if _, err := db.Exec(`UPDATE tasks SET parent_task_id = ? WHERE task_id = ?`,
					"t-somewhere-else", transferTestRow); err != nil {
					t.Fatalf("set parent: %v", err)
				}
			},
			expect: "parent_task_id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transferTestDB(t)
			tc.shape(t)
			err := TaskTransferHandover(transferTestRow, syncRowTestPeer, "", "")
			if err == nil {
				t.Fatalf("the handover signed over a row the predicate refuses")
			}
			if !strings.Contains(err.Error(), tc.expect) {
				t.Fatalf("refusal %q does not carry %q", err.Error(), tc.expect)
			}
			if got := rowLegStatus(t, transferTestRow); got != TaskStatusPending {
				t.Fatalf("a refused handover changed the row to %s — it must leave it alone", got)
			}
		})
	}
}

// TestTransferHandoverRefusesAnUnknownRow pins the missing-row arm: the refusal
// says the row is ABSENT rather than inventing a status for it.
func TestTransferHandoverRefusesAnUnknownRow(t *testing.T) {
	transferTestDB(t)
	err := TaskTransferHandover("t-nowhere", syncRowTestPeer, "", "")
	if err == nil {
		t.Fatalf("the handover accepted an unknown row")
	}
	if !strings.Contains(err.Error(), taskTransferMissingStatus) {
		t.Fatalf("refusal %q does not say the row is absent", err.Error())
	}
}

// TestTransferPeerFacesAnswerDocuments pins the PEER-SIDE wire contract in one
// process: both faces answer a JSON document (never a bare error string), the
// selector is read-only, and the handover's refusal carries what beat it.
func TestTransferPeerFacesAnswerDocuments(t *testing.T) {
	transferTestDB(t)

	out, err := SyncTransferPeerFace(SyncTransferPeerFaceOptions{SelectorBare: true})
	if err != nil {
		t.Fatalf("selector face: %v", err)
	}
	doc := &SyncTransferEligibleDoc{}
	if err := json.Unmarshal(out, doc); err != nil {
		t.Fatalf("the selector's answer is not a document: %v (%s)", err, out)
	}
	if !doc.Eligible || doc.Row == nil || doc.Row.TaskID != transferTestRow {
		t.Fatalf("selector answer = %s", out)
	}
	// READ-ONLY: the selector must not have moved the row.
	if got := rowLegStatus(t, transferTestRow); got != TaskStatusPending {
		t.Fatalf("the selector changed the row to %s — it is a READ", got)
	}

	out, err = SyncTransferPeerFace(SyncTransferPeerFaceOptions{
		HandoverID:  transferTestRow,
		CallerAlias: syncRowTestPeer,
	})
	if err != nil {
		t.Fatalf("handover face: %v", err)
	}
	hdoc := &SyncTransferHandoverDoc{}
	if err := json.Unmarshal(out, hdoc); err != nil {
		t.Fatalf("the handover's answer is not a document: %v (%s)", err, out)
	}
	if !hdoc.Signed {
		t.Fatalf("handover answer = %s", out)
	}
	if got := rowLegStatus(t, transferTestRow); got != TaskStatusCancelled {
		t.Fatalf("the handover did not sign the row over: %s", got)
	}

	// The SECOND handover of the same row is a losing race — and must still be
	// a DOCUMENT, not an error (the initiator walks on from a named refusal).
	out, err = SyncTransferPeerFace(SyncTransferPeerFaceOptions{
		HandoverID:  transferTestRow,
		CallerAlias: syncRowTestPeer,
	})
	if err != nil {
		t.Fatalf("second handover: %v", err)
	}
	hdoc2 := &SyncTransferHandoverDoc{}
	if err := json.Unmarshal(out, hdoc2); err != nil {
		t.Fatalf("second handover's answer is not a document: %v", err)
	}
	if hdoc2.Signed {
		t.Fatalf("the second handover claims it signed the row again: %s", out)
	}
	if !strings.Contains(hdoc2.Reason, TaskStatusCancelled) {
		t.Fatalf("the losing race does not name the status that beat it: %q", hdoc2.Reason)
	}
}

// TestTransferSelectorAnswersNoRow pins the empty answer: an empty queue is an
// ANSWER (exit-0 shape, machine-readable reason), never an error and never a
// silently different document.
func TestTransferSelectorAnswersNoRow(t *testing.T) {
	testutil.NewTestDBPath(t) // an empty ledger
	t.Setenv(config.SyncPeersEnv, syncRowTestPeer)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	doc := transferSelect(t, nil)
	if doc.Eligible || doc.Row != nil {
		t.Fatalf("an empty ledger offered a row: %+v", doc)
	}
	if !strings.Contains(doc.Reason, TaskTransferNoRow) {
		t.Fatalf("the empty answer is %q — it must carry the family's own sentence %q", doc.Reason, TaskTransferNoRow)
	}
}

// TestTransferSelectorNamesOneRow pins the narrowed question: `--next-eligible
// ID` answers about THAT row (eligible or not), so an operator (and the
// handover that follows) gets the same judgement the walk would have given.
func TestTransferSelectorNamesOneRow(t *testing.T) {
	transferTestDB(t)
	transferTestAddChild(t, transferTestChild, TaskStatusPending, float64(time.Now().Unix()))

	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	doc, err := TaskTransferSelectOne(context.Background(), db, nil, transferTestRow)
	if err != nil {
		t.Fatalf("TaskTransferSelectOne(named): %v", err)
	}
	if doc.Eligible {
		t.Fatalf("the named question admitted a row with a live dependent")
	}
	if !strings.Contains(doc.Reason, transferTestChild) {
		t.Fatalf("the named refusal does not name the child: %q", doc.Reason)
	}
	missing, err := TaskTransferSelectOne(context.Background(), db, nil, "t-nowhere")
	if err != nil {
		t.Fatalf("TaskTransferSelectOne(missing): %v", err)
	}
	if missing.Eligible || !strings.Contains(missing.Reason, "not in this ledger") {
		t.Fatalf("a missing row's answer = %+v", missing)
	}
}

// asCASRefusal is a tiny errors.As shim kept local so the assertions above read
// as prose (the type is the transfer's own).
func asCASRefusal(err error, target **SyncTransferCASRefusal) bool {
	for err != nil {
		if r, ok := err.(*SyncTransferCASRefusal); ok { //stylegate:ignore the refusal is returned unwrapped by TaskTransferHandover; a wrapped one still matches through Unwrap below
			*target = r
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// --- [6] the admission ------------------------------------------------------

// TestTransferAdmissionTakesTheRowsTheHandoverProduced is the receiving half's
// whole safety argument, asserted on shapes rather than argued in prose: the
// row is claimed when it is the handover's own copy, and REFUSED (with a
// sentence) under every other shape this ledger can hold for the same id.
//
// The four shapes are the four ways the copy can fail to be ours:
//
//	A  cancelled + the handover's reason + at least the offered stamp → claimed
//	B  denied: cancelled by SOMETHING ELSE (a local operator cancel)
//	C  denied: cancelled BEFORE the offer (a stale local row of the same id)
//	D  denied: not cancelled at all (the import did not apply the row)
func TestTransferAdmissionTakesTheRowsTheHandoverProduced(t *testing.T) {
	const offeredAt = 1_700_000_000.0
	cases := []struct {
		name    string
		shape   func(t *testing.T)
		claimed bool
		expect  string
	}{
		{
			name: "A the handover's own copy is claimed",
			shape: func(t *testing.T) {
				transferTestDB(t)
				rowLegForceStatus(t, transferTestRow, TaskStatusCancelled, float64(offeredAt)+5)
				setTransferReason(t, transferTestRow, taskTransferHandoverReason(syncRowTestPeer, transferTestRow))
			},
			claimed: true,
		},
		{
			name: "B a cancelled row that is NOT a handover's is refused",
			shape: func(t *testing.T) {
				transferTestDB(t)
				rowLegForceStatus(t, transferTestRow, TaskStatusCancelled, float64(offeredAt)+5)
				setTransferReason(t, transferTestRow, "cancelled by an operator")
			},
			expect: "was NOT cancelled by a handover",
		},
		{
			name: "C a cancellation older than the offer is refused",
			shape: func(t *testing.T) {
				transferTestDB(t)
				rowLegForceStatus(t, transferTestRow, TaskStatusCancelled, float64(offeredAt)-60)
				setTransferReason(t, transferTestRow, taskTransferHandoverReason(syncRowTestPeer, transferTestRow))
			},
			expect: "PREDATES the shipped copy",
		},
		{
			name: "D a row that is not cancelled is refused",
			shape: func(t *testing.T) {
				transferTestDB(t)
			},
			expect: "landed as " + TaskStatusPending,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.shape(t)
			shipped := SyncTaskRow{TaskID: transferTestRow, UpdatedAt: int64(offeredAt)}
			msg, err := TaskTransferAdmit(shipped, offeredAt)
			if err != nil {
				t.Fatalf("TaskTransferAdmit: %v", err)
			}
			got := rowLegStatus(t, transferTestRow)
			if tc.claimed {
				// The handover's own copy lands CLAIMED, not in_progress (epic
				// 01a1a87b): the adopting host exec's nothing, and in_progress now
				// means "a child was exec'd HERE". The lock half of the adoption
				// is recorded on lock_phase — asserted below, so this arm proves
				// the handover's acceptance is still complete rather than merely
				// weaker.
				if got != TaskStatusClaimed {
					t.Fatalf("the handover's own copy was not claimed: status=%s (%s)", got, msg)
				}
				if phase := rowLegLockPhase(t, transferTestRow); phase != TaskLockPhaseBoth {
					t.Fatalf("the handover's own copy was claimed with lock_phase=%q, want %q (the adoption must still settle the lock)", phase, TaskLockPhaseBoth)
				}
				return
			}
			if tc.expect != "" && !strings.Contains(msg, tc.expect) {
				t.Fatalf("refusal %q does not carry %q — a row that is not claimed must say so in words", msg, tc.expect)
			}
			if got == TaskStatusInProgress || got == TaskStatusClaimed {
				t.Fatalf("a row this transfer did NOT produce was claimed anyway (status=%s)", got)
			}
		})
	}
}

// rowLegLockPhase reads a row's lock_phase through the production projection
// (Task row → LockPhase), so the assertion rides the same reader an operator's
// `task show` uses rather than a private SELECT.
func rowLegLockPhase(t *testing.T, id string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row, err := taskByID(ctx, db, id)
	if err != nil || row == nil {
		t.Fatalf("read row %s: %v", id, err)
	}
	return row.LockPhase
}

// setTransferReason writes a row's reason straight into the ledger: the
// admission READS that column, so the fixture has to be able to produce exactly
// the text under test without going through the status machine.
func setTransferReason(t *testing.T, id, reason string) {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET reason = ? WHERE task_id = ?`, reason, id); err != nil {
		t.Fatalf("set reason: %v", err)
	}
}

// --- [6b] the reported state ------------------------------------------------

// TestRowReceiveReportsTheStateTheMessageLeft pins the ORDER of the leg's last
// two steps, which the cross-host probe measured the hard way (2026-10-07): the
// returned status was read BEFORE the post-import message, so a `--takeover`
// that had just claimed its row still reported the handed-over `cancelled`.
// The value is not cosmetic — the worker's trigger compares it to decide
// whether it has a row to run, and a wrong one drops the row silently.
//
// The sample is the smallest one that can express it: a read-only import
// followed by a message that MOVES the row, exactly as the admission does.
func TestRowReceiveReportsTheStateTheMessageLeft(t *testing.T) {
	rowLegTestDB(t)
	rows := []SyncTaskRow{{
		TaskID: transferTestRow, Summary: "wire probe", Payload: "P",
		Status: taskStatusCancelled, UpdatedAt: 1, Tags: []string{},
	}}
	_, status, err := SyncRowReceive(SyncRowReceiveOptions{
		TaskID: transferTestRow, Peer: syncRowTestPeer,
		Fetch: func(peer, id string) ([]SyncTaskRow, error) { return rows, nil },
		Message: func(id string) (string, error) {
			rowLegForceStatus(t, id, TaskStatusInProgress, 2)
			return "claimed here", nil
		},
	})
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if status != TaskStatusInProgress {
		t.Fatalf("receive reported %q after its message moved the row to %q — a caller reading this value would conclude it has no row to run",
			status, TaskStatusInProgress)
	}
	if got := rowLegStatus(t, transferTestRow); got != TaskStatusInProgress {
		t.Fatalf("the fixture's own move did not land: %s", got)
	}
}
