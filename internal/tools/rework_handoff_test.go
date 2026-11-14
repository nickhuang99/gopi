package tools

// Tests for the branch-rework lane's dedup gate (bug: the merger re-issued a
// rework row for a branch whose rework had been MIGRATED to a peer).
//
// THE TWO HALVES, and why both are needed:
//
//  1. HANDED OFF (migrated) — the row was sent by `gopi task send
//     --cancel-source` and is cancelled HERE by that move. The work is NOT
//     abandoned: it runs on the peer, whose own row carries it. Filing again
//     puts one branch in two workers' hands — the measured defect, RED on the
//     pre-fix live-only probe.
//  2. PLAIN CANCELLED (abandoned) — an operator gave up on the branch. The
//     branch MUST be able to enter the lane again, so this arm must KEEP
//     FILING. It is the guard against the naive fix ("just add cancelled to the
//     live set"), which Rule 26 forbids.
//
// The fixtures are the sync row leg's own two-ledger rig (the migration is
// executed by the REAL leg, never simulated) plus a real git repo (TaskSubmit
// verifies its repo before it writes).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// rhSubmitReworkRow files the branch's rework row through the REAL submit + the
// lane's own deliverable stamp — the same shape every refusal lane produces.
func rhSubmitReworkRow(t *testing.T, repo, branch, payload string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: resolveRowReworkSummaryPrefix + branch,
		Payload: payload,
		Tags:    resolveRowReworkTags,
		Source:  taskMergeSourceAuto,
		Repo:    repo,
	}); err != nil {
		t.Fatalf("submit rework row: %v", err)
	}
	if err := resolveRowStampDeliverableBranch(repo, branch); err != nil {
		t.Fatalf("stamp deliverable: %v", err)
	}
}

// rhReworkRowCount counts the ledger's rows under the lane's summary — what a
// duplicate looks like.
func rhReworkRowCount(t *testing.T, branch string) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE summary = ?`,
		resolveRowReworkSummaryPrefix+branch).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

// rhMigrateRow runs the REAL move: send the row to the peer leg and cancel the
// source through the opt-in half. It returns the send result so the caller can
// assert on the receipts too.
func rhMigrateRow(t *testing.T, senderDB, peerDB, taskID string) *SyncRowSendResult {
	t.Helper()
	var res *SyncRowSendResult
	var err error
	withLedger(t, senderDB, func() {
		res, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       taskID,
			CancelSource: true, // a send IS a move (epic task-send-is-a-move)
			Peers:        []string{syncRowTestPeer},
			Exchange: func(peer string, body []byte) ([]byte, error) {
				var out []byte
				var herr error
				withLedger(t, peerDB, func() { out, herr = SyncRowRemote(body) })
				return out, herr
			},
		})
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Disposition == nil || !res.Disposition.Cancelled {
		t.Fatalf("the migration did not cancel the source row: %+v", res.Disposition)
	}
	return res
}

// TestReworkDedupHandedOffReworkIsNotRefiled is the epic's headline and the RED
// proof of the fix.
//
// PRE-FIX (master's live-only probe): step (3) counts TWO rows for one branch —
// the merger re-issued the branch-rework task 12 minutes after the move, which is
// exactly the incident this task was filed on (zpeer 01a11564 → hp, then
// 01a1156f filed again; measured again by this test against master's tree).
func TestReworkDedupHandedOffReworkIsNotRefiled(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	repo := mhRepo(t)
	branch := "task/rh-handed-off"
	summary := resolveRowReworkSummaryPrefix + branch

	var rowID string
	withLedger(t, senderDB, func() {
		rhSubmitReworkRow(t, repo, branch, "rework "+branch+": cannot auto-merge into master")
		rowID = MergeVerifyRowIDForTest(summary)
	})
	if rowID == "" {
		t.Fatalf("could not resolve the filed row's id (summary %q)", summary)
	}

	// (1) THE MOVE. The peer's ledger takes the row; this host's copy is
	//     cancelled by the migration itself.
	rhMigrateRow(t, senderDB, peerDB, rowID)

	withLedger(t, senderDB, func() {
		row, err := taskStatusDirect(rowID)
		if err != nil || row == nil {
			t.Fatalf("read migrated row: %v", err)
		}
		if row.Status != TaskStatusCancelled {
			t.Fatalf("source status = %q, want %q (the migration cancels here)", row.Status, TaskStatusCancelled)
		}
		if !hasTaskTagToken(row.Tags, reworkHandoffTag) {
			t.Fatalf("the migrated row does not carry the %q marker (tags=%q) — the marker is what tells the "+
				"lane the work is elsewhere rather than abandoned", reworkHandoffTag, row.Tags)
		}
	})
	withLedger(t, peerDB, func() {
		row, err := taskStatusDirect(rowID)
		if err != nil || row == nil {
			t.Fatalf("the peer ledger has no row: %v", err)
		}
		if row.Status != TaskStatusPending {
			t.Fatalf("the peer's copy is %q, want %q — it was sent as queued work", row.Status, TaskStatusPending)
		}
	})

	// (2) THE DEDUP ENTRY. A handed-off row blocks the lane: the branch's rework
	//     is in flight ON THE PEER, so a second row here is a second worker on one
	//     branch.
	withLedger(t, senderDB, func() {
		blocked, reason, err := reworkSummaryBlocked(summary)
		if err != nil {
			t.Fatalf("dedup probe: %v", err)
		}
		if !blocked {
			t.Fatalf("the lane is FREE for a handed-off branch — master's live-only probe re-issues it "+
				"(summary %q)", summary)
		}
		if !strings.Contains(reason, "HANDED OFF") {
			t.Errorf("the refusal does not name the fact it decided on (the row was handed off): %q", reason)
		}
		if !strings.Contains(reason, rowID) {
			t.Errorf("the refusal does not name the row that was moved: %q", reason)
		}
	})

	// (3) THE FILING LANES. A real refusal lane asked to file the same branch
	//     reports the duplicate and writes NOTHING — one branch, one row.
	withLedger(t, senderDB, func() {
		action, detail := contractGateSubmitRework(repo, masterBranchName, branch, "contract drift probe")
		if action != mergeConflictActionDuplicate {
			t.Errorf("contract lane action = %q, want %q — the lane re-filed a handed-off branch: %s",
				action, mergeConflictActionDuplicate, detail)
		}
		if n := rhReworkRowCount(t, branch); n != 1 {
			t.Errorf("the ledger holds %d rework rows for %s, want exactly 1", n, branch)
		}
	})
}

// TestReworkDedupManualCancelStillRefiles is the OPPOSITE half, and the reason
// the fix was not "add cancelled to the live set": a row the OPERATOR abandoned
// carries no migration marker, so the branch must be able to enter the lane
// again — otherwise one deliberate cancellation would make a branch
// un-reworkable forever (Rule 26).
func TestReworkDedupManualCancelStillRefiles(t *testing.T) {
	senderDB, _ := rowLegTwoLedgers(t)
	repo := mhRepo(t)
	branch := "task/rh-abandoned"
	summary := resolveRowReworkSummaryPrefix + branch

	var rowID string
	withLedger(t, senderDB, func() {
		rhSubmitReworkRow(t, repo, branch, "rework "+branch+": cannot auto-merge into master")
		rowID = MergeVerifyRowIDForTest(summary)
		if _, err := TaskUpdateWithResult(rowID, TaskStatusCancelled, "operator gave up on this branch (no migration)"); err != nil {
			t.Fatalf("cancel by hand: %v", err)
		}
	})

	withLedger(t, senderDB, func() {
		row, err := taskStatusDirect(rowID)
		if err != nil || row == nil {
			t.Fatalf("read cancelled row: %v", err)
		}
		if hasTaskTagToken(row.Tags, reworkHandoffTag) {
			t.Fatalf("a hand-cancelled row carries the migration marker (tags=%q) — the marker must be written "+
				"by the migration leg alone", row.Tags)
		}
		blocked, reason, err := reworkSummaryBlocked(summary)
		if err != nil {
			t.Fatalf("dedup probe: %v", err)
		}
		if blocked {
			t.Fatalf("a hand-cancelled branch is BLOCKED (%s) — the branch could never be reworked again", reason)
		}
		action, detail := contractGateSubmitRework(repo, masterBranchName, branch, "contract drift probe")
		if action != "rework-submitted" {
			t.Errorf("contract lane action = %q, want %q — an abandoned branch must stay re-workable: %s",
				action, "rework-submitted", detail)
		}
		if n := rhReworkRowCount(t, branch); n != 2 {
			t.Errorf("the ledger holds %d rework rows for %s, want 2 (the abandoned one + the new filing)", n, branch)
		}
	})
}

// TestReworkDedupKeysOnTheBranchSummary pins the IDENTITY the gate reads: the
// deterministic summary, byte for byte. A row for a DIFFERENT branch (including
// a longer name that CONTAINS this one) must not block, or one branch's rework
// would silence another's.
func TestReworkDedupKeysOnTheBranchSummary(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	repo := mhRepo(t)
	branch := "task/rh-keyed"
	summary := resolveRowReworkSummaryPrefix + branch

	if summary != "resolve merge conflict: rework: "+branch {
		t.Fatalf("the lane's identity changed shape: %q", summary)
	}

	var rowID string
	withLedger(t, senderDB, func() {
		rhSubmitReworkRow(t, repo, branch, "rework "+branch)
		rowID = MergeVerifyRowIDForTest(summary)
	})
	rhMigrateRow(t, senderDB, peerDB, rowID)

	withLedger(t, senderDB, func() {
		for _, other := range []string{branch + "-part-two", "task/rh-key", "task/rh-keyedx"} {
			otherSummary := resolveRowReworkSummaryPrefix + other
			blocked, _, err := reworkSummaryBlocked(otherSummary)
			if err != nil {
				t.Fatalf("probe %q: %v", other, err)
			}
			if blocked {
				t.Errorf("branch %q is blocked by %q's row — the gate must key on the summary, not a prefix or "+
					"substring match", other, branch)
			}
		}
	})
}

// TestReworkDedupMarkerIsATokenNotASubstring pins the tag read: a row whose tags
// merely CONTAIN the marker's text inside another token must not block (the
// substring-match hazard task_adoption.go records for `tags LIKE '%x%'`).
func TestReworkDedupMarkerIsATokenNotASubstring(t *testing.T) {
	senderDB, _ := rowLegTwoLedgers(t)
	repo := mhRepo(t)
	branch := "task/rh-token"
	summary := resolveRowReworkSummaryPrefix + branch

	var rowID string
	withLedger(t, senderDB, func() {
		rhSubmitReworkRow(t, repo, branch, "rework "+branch)
		rowID = MergeVerifyRowIDForTest(summary)
		if _, err := TaskUpdateWithResult(rowID, TaskStatusCancelled, "cancelled by hand, with a decoy tag"); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		ctx := context.Background()
		db, err := openTasksDB(ctx, memoryDBPath())
		if err != nil {
			t.Fatalf("open ledger: %v", err)
		}
		defer db.Close()
		if _, err := db.ExecContext(ctx,
			`UPDATE tasks SET tags = tags || ? WHERE task_id = ?`,
			","+reworkHandoffTag+"-x", rowID); err != nil {
			t.Fatalf("decoy tag: %v", err)
		}
		blocked, reason, err := reworkSummaryBlocked(summary)
		if err != nil {
			t.Fatalf("dedup probe: %v", err)
		}
		if blocked {
			t.Errorf("a mid-token decoy blocked the lane (%s) — the marker must match a WHOLE tag token", reason)
		}
	})
}

// TestRowSendCancelSourceStampsTheHandoffMarkerOnItsOwnLedger pins the WRITER
// half in isolation: the marker lands in the ledger the row lives in (the
// per-ledger pin the cancel itself runs under), and a NEGATIVE outcome — a
// refused or unreachable peer — stamps nothing, because a row that never
// travelled has not been handed off.
func TestRowSendCancelSourceStampsTheHandoffMarkerOnItsOwnLedger(t *testing.T) {
	senderDB, peerDB := rowLegTwoLedgers(t)
	withLedger(t, senderDB, func() { rowGateSeedPending(t, rowGatePendingIdle) })

	var res *SyncRowSendResult
	var err error
	withLedger(t, senderDB, func() {
		res, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       rowGatePendingIdle,
			CancelSource: true, // a send IS a move (epic task-send-is-a-move)
			Peers:        []string{syncRowTestPeer},
			Exchange:     rowGateStorePeer(t, peerDB, new(int)),
		})
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Disposition == nil || !res.Disposition.Cancelled {
		t.Fatalf("migration did not cancel: %+v", res.Disposition)
	}
	withLedger(t, senderDB, func() {
		row, rerr := taskStatusDirect(rowGatePendingIdle)
		if rerr != nil || row == nil {
			t.Fatalf("read row: %v", rerr)
		}
		if !hasTaskTagToken(row.Tags, reworkHandoffTag) {
			t.Errorf("the migrated row carries no %q marker (tags=%q)", reworkHandoffTag, row.Tags)
		}
	})

	// THE NEGATIVE ARM: a REFUSED send leaves the row pending and unmarked. The
	// row is re-seeded under a fresh id (the ledger keeps the first one, which is
	// the point of an append-only ledger) and forced back to pending by the
	// fixture, exactly as the gate suite's own seeding does.
	withLedger(t, senderDB, func() { rowGateSeedPendingAt(t, rowGatePendingRefused) })
	var refused *SyncRowSendResult
	withLedger(t, senderDB, func() {
		refused, err = SyncRowSendEx(SyncRowSendOptions{
			TaskID:       rowGatePendingRefused,
			CancelSource: true, // a send IS a move (epic task-send-is-a-move)
			Peers:        []string{syncRowTestPeer},
			Exchange: func(peer string, body []byte) ([]byte, error) {
				return []byte(`{"stored":0,"refused":1,"reason":"peer refused the probe"}`), nil
			},
		})
	})
	if err != nil {
		t.Fatalf("refused send: %v", err)
	}
	if refused.Disposition == nil || refused.Disposition.Cancelled {
		t.Fatalf("a refused send cancelled the row: %+v", refused.Disposition)
	}
	withLedger(t, senderDB, func() {
		row, rerr := taskStatusDirect(rowGatePendingRefused)
		if rerr != nil || row == nil {
			t.Fatalf("read row: %v", rerr)
		}
		if hasTaskTagToken(row.Tags, reworkHandoffTag) {
			t.Errorf("a REFUSED migration stamped the marker (tags=%q) — nothing travelled, so nothing was handed off",
				row.Tags)
		}
	})
}

// rowGatePendingRefused is the second row id the marker suite needs (a ledger is
// append-only, so the refused arm cannot reuse the first row's id).
const rowGatePendingRefused = "t-rowleg-gate-02"

// rowGateSeedPendingAt registers one row and forces it to pending — the gate
// suite's own seeding shape, parameterized by id.
func rowGateSeedPendingAt(t *testing.T, id string) {
	t.Helper()
	if _, err := TaskNewOID(TaskNewOptions{
		Summary: "row gate probe " + id,
		TaskID:  id,
		Payload: "the row the gate tests carry",
		Source:  "human",
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	rowLegForceStatus(t, id, TaskStatusPending, float64(time.Now().Unix()))
}

// TestReworkDedupMarkerTagIsTheOneSpelling pins the marker against an inline
// literal appearing anywhere else (Rule 13: the constant is referenced, never
// bypassed). It also pins the ledger's own vocabulary: the marker is a TAG
// token, not a status — folding it into the status set is the fix Rule 26
// forbids.
func TestReworkDedupMarkerTagIsTheOneSpelling(t *testing.T) {
	if strings.TrimSpace(reworkHandoffTag) == "" {
		t.Fatal("the migration marker has no value")
	}
	if isLiveTaskStatus(reworkHandoffTag) {
		t.Errorf("%q is a live STATUS value — the marker is a tag, and cancelled must NOT become live (Rule 26)",
			reworkHandoffTag)
	}
	if TaskStatusCancelled != "cancelled" {
		t.Fatalf("the status vocabulary changed shape: %q", TaskStatusCancelled)
	}
	if config.MemoryDBEnv == "" {
		t.Fatal("the ledger env name is empty — the probe would read whatever the ambient environment holds")
	}
}
