package tools

// task_claim_local.go: the LOCAL claim face -- everything a SINGLE-NODE ledger
// still needs from the claim family, moved out of
// internal/tools/memory_sync_claim.go in the same commit that deleted the
// cross-node half of it (Rule 43: a survivor moves BEFORE the mechanism it
// lived beside is removed).
//
// WHAT WENT WITH THAT MECHANISM: the two-phase DISTRIBUTED lock -- Phase-2
// remote CAS on every declared peer, Phase-3 double-lock, the ALOHA collision
// retry ladder (claimLockMaxRounds / backoff), the round classifier, the
// runner's driver (RunClaimLockProtocol) and the ssh exchange that carried
// them. All of it could only decide anything when a peer was declared; on a
// one-host ledger every round short-circuited to "proceed" after three config
// gates and two [claim-lock] prints.
//
// WHAT SURVIVES HERE, each for a reason that is about THIS host:
//
//	rollbackLocalClaimTagged (+ the two exported faces)
//	    the revert leg: claimed/in_progress -> pending with claimed_by KEPT
//	    and a reason tag. This is the anti-deadlock the LOCAL CAS needs: a
//	    runner that claims a row and then cannot start it must give it back,
//	    or the row is owned by nobody and runnable by nobody.
//	TaskClaimOwner
//	    the holder stamp's ONE owner (the host:pid shape); the runner, the
//	    worker loop and the worktree-release path all read it.
//	claimHostIsSelf / filterSelfOwnedClaims / countLiveRowsOwnedByOthers /
//	countLiveRowsByHolder / liveForeignClaimsSQL / LiveRowsOwnedByOthers
//	    "is another holder live HERE?" -- the worker's drained-exit predicate.
//	    The ownership rule is Go, not SQL, because a LIKE prefix cannot
//	    express a component boundary (host vs host2). Nothing about it is
//	    cross-node: it reads this ledger.
//	OpenWorkerProbeDB
//	    the test/introspection seam onto that same ledger.
//	claimBroadcastNodeName
//	    this node's sync identity (GOPI_SYNC_NODE override, else hostname).
//	    It was DEFINED beside the claim broadcast, but its consumers are the
//	    row-send / submit-forward / transfer / asset legs that survive -- a
//	    symbol with consumers outside the deleted mechanism moves with the
//	    survivors, never with the mechanism.
//
// WHAT WAS NEVER HERE, and did not change: the pending->claimed local CAS is
// TaskNextFiltered (tasks.go), and claimed->in_progress is written ONLY by
// TaskStartChildAdmitted, after cmd.Start() succeeds.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

// claimBroadcastNodeName mirrors the delta exporter's node identity
// (GOPI_SYNC_NODE override for same-host two-node sims).
func claimBroadcastNodeName() string {
	if n := os.Getenv("GOPI_SYNC_NODE"); n != "" {
		return n
	}
	host, err := os.Hostname()
	if err != nil {
		return syncTaskUnknownNode
	}
	return host
}

// RollbackLocalClaim is the LOCAL half of the transaction rollback (M4.21
// revert semantics — the nick veto of cancel+sync-yielded): claimed →
// pending (and, in the runTaskRunOnce yield shape where TaskStart already
// advanced the row to in_progress before the remote lock leg failed —
// the anchor-guard incident, 2026-09-02 — in_progress → pending too),
// with claimed_by KEPT (the audit trail of who rolled back), a fresh
// updated_at (the revert's anchor travels through the merge), and a
// sync-claim-revert tag so a peer seeing the row knows the state is the
// rollback's truth, not a stale echo. Terminal rows never roll back. An
// in_progress row is only ever reverted here by the run that started it
// (the child has not yet been spawned at the yield point), so the revert
// cannot race a genuinely executing task.
//
// Error-prefix contract (bugfix claim-rollback-prefix, 2026-09-02): the
// returned errors carry NO "claim rollback:" prefix — callers compose their
// own face (RunClaimLockProtocol adds "claim rollback:", the CLI adds its
// [claim-lock] print). The old version pre-prefixed here AND re-prefixed in
// the driver → the doubled "claim rollback: claim rollback:" line in every
// yield log (live incident t-1788329099).
func RollbackLocalClaim(taskID string) error {
	taskID = taskIDNormalize(taskID)
	return rollbackLocalClaimTagged(taskID, claimRevertTag)
}

// RevertClaimUnattributed reverts a claim the RUNNER itself refuses to
// execute — the worker-log-session-attribution case: the task's session dir
// is unavailable, so the worker would run with its output in no attributable
// place. The row goes back to pending (another holder / a later round may
// retry once the profile is writable) and carries an explicit reason tag so
// the ledger says WHY it bounced, distinct from the sync-collision revert.
func RevertClaimUnattributed(taskID string) error {
	return rollbackLocalClaimTagged(taskID, claimUnattributedTag)
}

// claimUnattributedTag records the attribution refusal (see
// RevertClaimUnattributed) — its own tag, so a bounce for "the profile could
// not hold this run's output" is never mistaken for a sync-collision revert.
const claimUnattributedTag = "worker-output-unattributed"

// rollbackLocalClaimTagged is the one revert-to-pending implementation; the
// tag parameter is the audit reason (Rule 1: the reason strings are
// constants, never literals at the call sites).
func rollbackLocalClaimTagged(taskID, reasonTag string) error {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return err
	}
	defer closeDB()
	ctx := context.Background()
	now := float64(time.Now().Unix())
	// Pre-write row read (notify bus): the CAS below is status-either, so
	// the ledger itself must supply the authoritative previous_status.
	prev, err := taskByID(ctx, db, taskID)
	if err != nil {
		return fmt.Errorf("revert read: %w", err)
	}
	res, err := db.Exec(`UPDATE tasks SET status = ?, updated_at = ? WHERE task_id = ? AND status IN (?, ?)`,
		taskStatusPending, now, taskID, taskStatusClaimed, taskStatusInProgress)
	if err != nil {
		return fmt.Errorf("revert write: %w", err)
	}
	//stylegate:ignore n==0 is the meaningful branch; a driver error yields the same 0 and same path
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %s is no longer %s (raced a writeback — left as-is)", taskID, taskStatusClaimed)
	}
	// Transition hook (task-notify-terminal-gap): the revert is a real
	// status move — claimed/in_progress → pending — that the operator's
	// stream must see. Fired AFTER the CAS won (the write is committed;
	// the caller's own DB handle is not a transaction here, but the
	// pending-verb contract is identical: never notify a write that lost
	// its CAS). prev is the pre-write read above.
	if prev != nil {
		taskNotifyFlushStatus(prev, prev.Status, taskStatusPending)
	}
	// Best-effort audit tag: the marker tells the merge story; a tag
	// failure never undoes the revert itself.
	if _, err := taskAddTag(ctx, db, taskID, reasonTag); err != nil {
		return fmt.Errorf("revert tag: %w", err)
	}
	return nil
}

// claimRevertTag records WHY a row went back to pending (the M4.21
// rollback audit face): the lock protocol reverted the claim.
const claimRevertTag = "sync-claim-revert"

// TaskClaimOwner renders the claim holder name for the protocol's audit
// stamp (mirrors taskDefaultClaimHolder's hostname:pid shape).
func TaskClaimOwner(asOverride string) string {
	if s := strings.TrimSpace(asOverride); s != "" {
		return s
	}
	return taskDefaultClaimHolder()
}

// claimHostIsSelf reports whether a claim-holder stamp belongs to THIS node:
// the stamp's HOST component equals self (the stamp is either the bare host
// — the worker loop's `host` shape — or the `host:pid` shape written by
// taskDefaultClaimHolder). The host component is delimited EXACTLY: the stamp
// must be `self` or `self + claimHolderHostPidSep + <pid>`.
//
// The previous matcher was a bare prefix LIKE (`claimed_by NOT LIKE self%`):
// a FOREIGN host whose name merely EXTENDS ours ("host" vs foreign "host2" →
// stamp "host2:1234") matched the prefix and was classified as self, so
// LiveRowsOwnedByOthers under-reported live foreign work (a node could
// believe it was alone while a same-prefix peer was mid-task). A prefix LIKE
// cannot express "component boundary" — the separator itself is the whole
// difference between `host:pid` and `host2:pid`.
func claimHostIsSelf(claimedBy, self string) bool {
	claimedBy = strings.TrimSpace(claimedBy)
	self = strings.TrimSpace(self)
	if claimedBy == "" || self == "" {
		return false // no stamp / no identity → not provably self
	}
	if claimedBy == self {
		return true // the bare-host shape
	}
	// The host:pid shape — the suffix after the separator must be a pid, and
	// the host component must be EXACTLY self (no extra characters).
	host, pid, ok := strings.Cut(claimedBy, claimHolderHostPidSep)
	if !ok || host != self || pid == "" {
		return false
	}
	return true
}

// filterSelfOwnedClaims returns exactly the FOREIGN holder stamps of claims:
// blank stamps are dropped (an unclaimed row has no owner to report — the SQL
// row selection excludes them too), self-owned stamps are dropped by the one
// ownership rule (claimHostIsSelf), everything else is foreign. Nil-safe.
func filterSelfOwnedClaims(claims []string, self string) []string {
	out := make([]string, 0, len(claims))
	for _, c := range claims {
		if strings.TrimSpace(c) == "" {
			continue // unclaimed: nobody owns it, not a foreign holder
		}
		if !claimHostIsSelf(c, self) {
			out = append(out, c)
		}
	}
	return out
}

// LiveRowsOwnedByOthers counts in_progress rows whose claimed_by belongs to
// another holder (M4.20-D worker drained-exit line: "other holder active").
// A row is "owned by others" when claimed_by is set and does NOT match this
// node's worker identity. The ownership rule lives in ONE place —
// claimHostIsSelf — and the ledger read deliberately does NOT re-express it
// as a SQL LIKE: a bare prefix pattern cannot say "the host component must
// match exactly", which is precisely the `host` vs `host2:pid` defect this
// function used to carry (the SQL keeps only the row selection; the
// classification is Go, so the two can never drift — Rule 2). Read-only:
// pure introspection.
func LiveRowsOwnedByOthers() int {
	db, closeDB, err := taskSyncDB()
	if err != nil {
		return 0 // introspection must never break the exit path
	}
	defer closeDB()
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		self = syncUnknownHost
	}
	return countLiveRowsOwnedByOthers(context.Background(), db, self)
}

// liveForeignClaimsSQL selects the claimed_by stamps of live rows. The
// self-classification is deliberately NOT in SQL (a LIKE prefix cannot
// express the component boundary — the bug this fixes); the caller filters
// with claimHostIsSelf, the one place the ownership rule lives.
const liveForeignClaimsSQL = `SELECT DISTINCT claimed_by FROM tasks
	WHERE status = ? AND TRIM(claimed_by) != ''`

// countLiveRowsOwnedByOthers counts live rows owned by holders other than
// self: the ledger supplies the stamps (row selection), filterSelfOwnedClaims
// applies the ONE ownership rule, and each foreign holder's live rows are
// counted. The classification never happens in SQL, so the predicate and
// claimHostIsSelf cannot drift apart.
func countLiveRowsOwnedByOthers(ctx context.Context, db *sql.DB, self string) int {
	rows, err := db.QueryContext(ctx, liveForeignClaimsSQL, taskStatusInProgress)
	if err != nil {
		return 0
	}
	defer rows.Close()
	stamps := []string{}
	for rows.Next() {
		var claimedBy string
		if err := rows.Scan(&claimedBy); err != nil {
			return 0
		}
		stamps = append(stamps, claimedBy)
	}
	if err := rows.Err(); err != nil {
		return 0
	}
	// COUNT of ROWS, not of distinct holders: several live rows claimed by
	// the same foreign holder all count (the probe reports live WORK); the
	// distinct-holder walk keeps the per-holder COUNT query bounded.
	seen := map[string]bool{}
	var n int
	for _, holder := range filterSelfOwnedClaims(stamps, self) {
		holder = strings.TrimSpace(holder)
		if seen[holder] {
			continue
		}
		seen[holder] = true
		c, err := countLiveRowsByHolder(ctx, db, holder)
		if err != nil {
			return 0
		}
		n += c
	}
	return n
}

// countLiveRowsByHolder counts the live rows carrying exactly one holder
// stamp (the in_progress + claimed_by pair is the ledger's only selector).
func countLiveRowsByHolder(ctx context.Context, db *sql.DB, holder string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status = ? AND TRIM(claimed_by) = ?`,
		taskStatusInProgress, holder).Scan(&n)
	return n, err
}

// OpenWorkerProbeDB opens the env-resolved ledger for tests/introspection
// (the worker-drained probe uses it to stamp a foreign claim). Test-facing
// seam: same openTasksDB contract the tools layer uses everywhere.
func OpenWorkerProbeDB() (*sql.DB, func(), error) {
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		return nil, nil, err
	}
	//stylegate:ignore close of a handle whose caller already finished; the OS reclaims the descriptor
	return db, func() { _ = db.Close() }, nil
}
