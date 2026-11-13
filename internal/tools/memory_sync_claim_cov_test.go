package tools

// memory_sync_claim_cov_test.go — coverage for the SURVIVING claim faces
// after the cross-node claim lock was deleted (batch 4):
//
//	NotifyTerminalPushFailed — the exported alarm wrapper (events filter +
//	                         async delivery shapes).
//	TaskClaimOwner          — override wins / blank falls through to the
//	                         hostname:pid audit stamp.
//	LiveRowsOwnedByOthers   — only FOREIGN holders count; self rows and the
//	                         host:pid shape of self are excluded.
//	OpenWorkerProbeDB       — the test-facing ledger seam (same DB contract
//	                         as taskSyncDB, plus the error path a foreign
//	                         file must produce).
//
// The WaitClaimLockVerdict cases that used to open this file are GONE with
// the lock they waited for: a single node has no round to wait on, so the
// bounded wait and its retry shape had nothing left to assert.
//
// Determinism: no network, no sleeps.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNotifyTerminalPushFailedExportedWrapper — the exported face delivers
// the same alarm payload as the unexported helper: TaskID + the push error
// text + the reserved event name (the ops signal that a terminal state
// never reached the peers).
func TestNotifyTerminalPushFailedExportedWrapper(t *testing.T) {
	claimTestDB(t)
	out := filepath.Join(t.TempDir(), "notify-capture.json")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat > "+out)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")
	t.Cleanup(func() {
		os.Unsetenv("GOPI_TASK_NOTIFY_CMD")
		os.Unsetenv("GOPI_TASK_NOTIFY_EVENTS")
	})

	NotifyTerminalPushFailed("t-cov-m419", fmt.Errorf("terminal push: 3 attempts exhausted"))

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("alarm never reached the notify cmd: %v", err)
	}
	var p taskNotifyPayload
	if err := claimUnmarshal(b, &p); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	if p.Event != taskNotifyEventTerminalPushFailed {
		t.Fatalf("event = %q, want %q", p.Event, taskNotifyEventTerminalPushFailed)
	}
	if p.TaskID != "t-cov-m419" {
		t.Fatalf("payload task id = %q", p.TaskID)
	}
	if !strings.Contains(p.Summary, "3 attempts exhausted") {
		t.Fatalf("payload must quote the push failure, got %q", p.Summary)
	}
	if p.Status != taskStatusCompleted {
		t.Fatalf("alarm payload status = %q (the row IS terminal)", p.Status)
	}
}

// TestNotifyTerminalPushFailedAsyncIsSilentWithoutSubscriber — error/
// boundary face: ASYNC delivery returns before the payload is written (the
// documented goroutine shape), and with no event subscription nothing is
// delivered at all. Asserted WITHOUT a sleep: the cmd target must not exist
// (sync-firing would create it).
func TestNotifyTerminalPushFailedAsyncIsSilentWithoutSubscriber(t *testing.T) {
	claimTestDB(t)
	out := filepath.Join(t.TempDir(), "notify-capture.json")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat > "+out)
	t.Setenv("GOPI_TASK_NOTIFY_ASYNC", "1") // default event set: alarm not included
	t.Cleanup(func() {
		os.Unsetenv("GOPI_TASK_NOTIFY_CMD")
		os.Unsetenv("GOPI_TASK_NOTIFY_ASYNC")
	})

	NotifyTerminalPushFailed("t-cov-async", fmt.Errorf("boom"))

	if _, err := os.Stat(out); err == nil {
		t.Fatalf("unsubscribed synthetic event must not deliver (async or not)")
	}
}

// TestTaskClaimOwnerOverrideAndDefault — the audit stamp: an explicit --as
// (trimmed) wins; blank whitespace falls through to this host's
// hostname:pid default. The stamp is what the collision protocol compares,
// so a blank override must never produce an empty owner.
func TestTaskClaimOwnerOverrideAndDefault(t *testing.T) {
	if got := TaskClaimOwner("  runner-7  "); got != "runner-7" {
		t.Fatalf("override must win (trimmed), got %q", got)
	}
	for _, blank := range []string{"", "   ", "\t\n", "   \t"} {
		host, _ := os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
		got := TaskClaimOwner(blank)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("blank override %q produced an empty owner — the lock protocol would collide with itself", blank)
		}
		if !strings.HasPrefix(got, host+":") {
			t.Fatalf("default owner %q must be the hostname:pid shape of %q", got, host)
		}
	}
}

// TestLiveRowsOwnedByOthersCountsForeignHoldersOnly — the worker drained-
// exit predicate. Boundary rows: in_progress held by a FOREIGN host counts;
// in_progress held by THIS host (both the bare-host and host:pid shapes)
// does NOT; pending/completed rows never count; an empty claimed_by never
// counts.
func TestLiveRowsOwnedByOthersCountsForeignHoldersOnly(t *testing.T) {
	newDeltaTestDB(t) // schema-initialized isolated ledger
	base := float64(time.Now().Unix())
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		t.Skipf("no hostname in this environment")
	}

	if got := LiveRowsOwnedByOthers(); got != 0 {
		t.Fatalf("empty ledger reported %d foreign live rows, want 0", got)
	}

	deltaInsertRaw(t, "t-cov-foreign", "peer-held row", taskStatusInProgress, base, base, "")
	deltaSetClaim(t, "t-cov-foreign", "some-other-host:4242")

	if got := LiveRowsOwnedByOthers(); got != 1 {
		t.Fatalf("foreign in_progress row not counted: got %d, want 1", got)
	}

	// Same host, two shapes the worker loop stamps (bare host / host:pid).
	deltaInsertRaw(t, "t-cov-self-bare", "self held", taskStatusInProgress, base, base, "")
	deltaSetClaim(t, "t-cov-self-bare", self)
	deltaInsertRaw(t, "t-cov-self-pid", "self held with pid", taskStatusInProgress, base, base, "")
	deltaSetClaim(t, "t-cov-self-pid", fmt.Sprintf("%s:%d", self, os.Getpid()))

	if got := LiveRowsOwnedByOthers(); got != 1 {
		t.Fatalf("self-held live rows must not count: got %d, want 1 (foreign only)", got)
	}

	// Non-live rows and an unheld row are outside the predicate.
	deltaInsertRaw(t, "t-cov-pending", "queued", taskStatusPending, base, base, "")
	deltaInsertRaw(t, "t-cov-done", "finished", taskStatusCompleted, base, base, "")
	deltaInsertRaw(t, "t-cov-unclaimed", "no holder", taskStatusInProgress, base, base, "")
	if got := LiveRowsOwnedByOthers(); got != 1 {
		t.Fatalf("non-live/unclaimed rows leaked into the count: got %d, want 1", got)
	}
}

// TestOpenWorkerProbeDBResolvesLedgerAndFailsSoft — the seam's three
// shapes: (a) it opens the SAME env-resolved ledger taskSyncDB does, so a
// probe stamp is visible to the tools layer; (b) the closer really closes
// (a query after close errors); (c) error path: a NON-gopi file at the env
// path must be refused (openTasksDB's foreign-DB guard) with db/closer nil
// — never a half-open handle the caller would leak.
func TestOpenWorkerProbeDBResolvesLedgerAndFailsSoft(t *testing.T) {
	path := newDeltaTestDB(t)
	deltaInsertRaw(t, "t-cov-probe", "row for the probe", taskStatusPending, 1000, 1000, "")

	db, closeDB, err := OpenWorkerProbeDB()
	if err != nil {
		t.Fatalf("OpenWorkerProbeDB: %v", err)
	}
	if closeDB == nil {
		t.Fatalf("OpenWorkerProbeDB returned a nil closer")
	}
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE task_id = 't-cov-probe'`).Scan(&n); err != nil {
		t.Fatalf("probe query: %v", err)
	}
	if n != 1 {
		t.Fatalf("probe DB is not the env-resolved ledger: found %d rows", n)
	}
	// The probe writes through the same file taskSyncDB reads.
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET claimed_by = 'probe-host:1' WHERE task_id = 't-cov-probe'`); err != nil {
		t.Fatalf("probe write: %v", err)
	}
	closeDB()
	if err := db.QueryRowContext(context.Background(), `SELECT 1`).Scan(&n); err == nil {
		t.Fatalf("closer did not close the handle")
	}
	row := taskRowByID(t, "t-cov-probe")
	if row.ClaimedBy != "probe-host:1" {
		t.Fatalf("probe write not visible to the tools layer: claimed_by=%q", row.ClaimedBy)
	}

	// Error path: a foreign sqlite file (not a gopi knowledge base).
	foreignDir := t.TempDir()
	foreign := filepath.Join(foreignDir, "foreign.db")
	if err := os.WriteFile(foreign, []byte("not a database at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memoryDBEnv, foreign)
	db2, close2, err := OpenWorkerProbeDB()
	if err == nil {
		if close2 != nil {
			close2()
		}
		t.Fatalf("a foreign file at %s must be refused (decided: %v)", foreign, memoryDBPath())
	}
	if db2 != nil || close2 != nil {
		t.Fatalf("error path must return nil db/closer, got db=%v closer_nil=%v", db2, close2 == nil)
	}
	_ = path
}
