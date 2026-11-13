package tools

// memory_sync_delta_cov_test.go — coverage tests for the exported delta-side
// faces: the worker-provenance tag family (M4.20 ①), the tags-settable
// terminal writeback, and the sync-state listing:
//
//	WorkerTaskTag          — this host's `worker:<hostname>` stamp; the tag
//	                         rides the row (never re-exported), so it must
//	                         be stable and prefix-namespaced.
//	TagWorkerClaim         — the idempotent union: no duplicate stamp, empty
//	                         input yields exactly the tag, existing tags are
//	                         never dropped.
//	TaskRunWritebackTagged — the worker loop's terminal write: the row ends
//	                         completed|failed AND carries the worker tag.
//	TaskRunWritebackTags   — the primitive: explicit tags land, empty tags
//	                         leave the row's tags untouched, an illegal
//	                         status is refused BEFORE any tag write (the
//	                         tags column must never move on a failed write).
//	SyncStateList          — every peer's anchors, oldest-synced first.
//
// Determinism: isolated temp ledgers only (GOPI_MEMORY_DB), no network, no
// sleeps. Rows are fixture-inserted with explicit timestamps, so ordering
// assertions never depend on the wall clock.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkerTaskTagHostPrefixAndStability — the provenance stamp: exactly
// one `worker:` prefix, this host's name, and identical across calls (the
// tag is compared by hasTag/TagWorkerClaim for idempotency, so drift would
// re-stamp every round).
func TestWorkerTaskTagHostPrefixAndStability(t *testing.T) {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		t.Skipf("no hostname in this environment")
	}
	tag := WorkerTaskTag()
	if !strings.HasPrefix(tag, WorkerTaskTagPrefix) {
		t.Fatalf("tag %q must start with the namespaced prefix %q", tag, WorkerTaskTagPrefix)
	}
	if tag != WorkerTaskTagPrefix+host {
		t.Fatalf("tag = %q, want %q (worker:<hostname>)", tag, WorkerTaskTagPrefix+host)
	}
	if strings.Count(tag, WorkerTaskTagPrefix) != 1 {
		t.Fatalf("tag %q carries the prefix more than once", tag)
	}
	if again := WorkerTaskTag(); again != tag {
		t.Fatalf("tag is not stable across calls: %q vs %q", tag, again)
	}
}

// TestTagWorkerClaimUnionIsIdempotent — the union's three shapes: empty
// input → exactly the tag (no leading comma); an unrelated tag → appended
// after the existing tags; re-tagging → BYTE-IDENTICAL (idempotent, so a
// retried writeback never grows the tag column).
func TestTagWorkerClaimUnionIsIdempotent(t *testing.T) {
	tag := WorkerTaskTag()

	if got := TagWorkerClaim(""); got != tag {
		t.Fatalf("empty tags → %q, want exactly %q", got, tag)
	}
	if got := TagWorkerClaim("   "); got != tag {
		t.Fatalf("whitespace-only tags → %q, want exactly %q", got, tag)
	}
	const existing = "module:sync,bug"
	if got := TagWorkerClaim(existing); got != existing+","+tag {
		t.Fatalf("existing tags must be preserved and the worker tag appended: got %q", got)
	}
	first := TagWorkerClaim(existing)
	if second := TagWorkerClaim(first); second != first {
		t.Fatalf("re-tagging must be a no-op: %q → %q", first, second)
	}
	// A tag list that ALREADY carries the worker tag in the middle stays
	// untouched (the union is a set membership test, not a prefix test).
	withMiddle := "module:x," + tag + ",module:y"
	if got := TagWorkerClaim(withMiddle); got != withMiddle {
		t.Fatalf("an embedded worker tag must be detected: got %q", got)
	}
}

// TestTaskRunWritebackTaggedStampsWorkerProvenance — happy path: the worker
// loop's terminal write ends the row completed AND stamps the worker tag,
// with the dispatcher's pre-existing tags preserved (the tag is ADDED, the
// row's tags are never replaced).
func TestTaskRunWritebackTaggedStampsWorkerProvenance(t *testing.T) {
	newDeltaTestDBClient(t)
	mustSubmitTest(t, "worker writeback", "briefing", "t-cov-wb-1")
	if _, err := TaskNext("runner-w", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart("t-cov-wb-1", "runner-w"); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Give the row a pre-existing tag set (the dispatcher's own provenance).
	seedTags(t, "t-cov-wb-1", "module:sync")

	out, err := TaskRunWritebackTagged("t-cov-wb-1", TaskStatusCompleted, "done — worker round")
	if err != nil {
		t.Fatalf("TaskRunWritebackTagged: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("writeback must report the terminal fact")
	}
	row := taskRowByID(t, "t-cov-wb-1")
	if row.Status != TaskStatusCompleted {
		t.Fatalf("row status = %q, want %q", row.Status, TaskStatusCompleted)
	}
	if !hasTag(row.Tags, WorkerTaskTag()) {
		t.Fatalf("worker provenance tag missing: tags=%q want tag %q", row.Tags, WorkerTaskTag())
	}
	if !hasTag(row.Tags, "module:sync") {
		t.Fatalf("pre-existing tags were dropped by the union: %q", row.Tags)
	}
	if len(row.Tags) > 0 && strings.HasPrefix(row.Tags, ",") {
		t.Fatalf("tag column must not start with a separator: %q", row.Tags)
	}
}

// TestTaskRunWritebackTaggedErrorPath — error face: a missing row makes the
// tag lookup fail, so the writeback must report the error and write
// NOTHING (no terminal write for a ghost id).
func TestTaskRunWritebackTaggedErrorPath(t *testing.T) {
	newDeltaTestDBClient(t)
	if _, err := TaskRunWritebackTagged("t-cov-wb-ghost", TaskStatusCompleted, "done"); err == nil {
		t.Fatalf("writeback of a nonexistent row must error")
	}
}

// TestTaskRunWritebackTagsExplicitAndEmpty — the primitive's two branches:
// an explicit tag string LANDS on the row after the terminal write; an
// empty string leaves the row's tags exactly as they were (the CLI's
// TaskRunWriteback contract — a plain run never re-tags).
func TestTaskRunWritebackTagsExplicitAndEmpty(t *testing.T) {
	newDeltaTestDBClient(t)
	mustSubmitTest(t, "tagged writeback", "briefing", "t-cov-wb-2")
	if _, err := TaskNext("runner-t", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart("t-cov-wb-2", "runner-t"); err != nil {
		t.Fatalf("start: %v", err)
	}
	seedTags(t, "t-cov-wb-2", "keep:me")

	const explicit = "keep:me,module:x"
	if _, err := TaskRunWritebackTags("t-cov-wb-2", TaskStatusCompleted, "done — tagged", explicit); err != nil {
		t.Fatalf("TaskRunWritebackTags: %v", err)
	}
	row := taskRowByID(t, "t-cov-wb-2")
	if row.Status != TaskStatusCompleted {
		t.Fatalf("row status = %q, want completed", row.Status)
	}
	if row.Tags != explicit {
		t.Fatalf("explicit tags = %q, want %q", row.Tags, explicit)
	}

	// Empty tags: the terminal write still happens, the tag column is
	// untouched (the exactly-empty branch returns before any UPDATE).
	newDeltaTestDBClient(t)
	mustSubmitTest(t, "empty-tag writeback", "briefing", "t-cov-wb-3")
	if _, err := TaskNext("runner-e", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart("t-cov-wb-3", "runner-e"); err != nil {
		t.Fatalf("start: %v", err)
	}
	seedTags(t, "t-cov-wb-3", "original:tag")
	if _, err := TaskRunWritebackTags("t-cov-wb-3", TaskStatusFailed, "boom", ""); err != nil {
		t.Fatalf("TaskRunWritebackTags(empty tags): %v", err)
	}
	row = taskRowByID(t, "t-cov-wb-3")
	if row.Status != TaskStatusFailed {
		t.Fatalf("row status = %q, want failed", row.Status)
	}
	if row.Tags != "original:tag" {
		t.Fatalf("empty tags must not touch the column: %q", row.Tags)
	}
}

// TestTaskRunWritebackTagsRefusesIllegalStatusBeforeTagWrite — error path
// + invariant: an illegal status (neither completed nor failed) is refused
// by the terminal write, so the tag UPDATE behind it must never run. A
// "tags written, status refused" row would be a silent half-write.
func TestTaskRunWritebackTagsRefusesIllegalStatusBeforeTagWrite(t *testing.T) {
	newDeltaTestDBClient(t)
	mustSubmitTest(t, "illegal status", "briefing", "t-cov-wb-4")
	seedTags(t, "t-cov-wb-4", "stay:put")

	if _, err := TaskRunWritebackTags("t-cov-wb-4", "in_progress", "nope", "clobber:tag"); err == nil {
		t.Fatalf("an illegal status must be refused")
	}
	row := taskRowByID(t, "t-cov-wb-4")
	if row.Status != taskStatusPending {
		t.Fatalf("refused writeback moved the status to %q", row.Status)
	}
	if row.Tags != "stay:put" {
		t.Fatalf("refused writeback wrote tags: %q (the tag UPDATE must ride a successful terminal write)", row.Tags)
	}
}

// TestSyncStateListOrderAndEmpty — SyncStateList returns every peer row and
// nothing else, oldest-synced first by updated_at. The empty-ledger boundary
// is an empty list, not an error. (The `full-counter:` namespace rows this arm
// used to assert are gone with the periodic-full mechanism, deleted
// 2026-11-05 — the filter they exercised is SyncStateList's own peer-column
// read, which these two anchors still cover.)
func TestSyncStateListOrderAndEmpty(t *testing.T) {
	newDeltaTestDBClient(t)

	rows, err := SyncStateList()
	if err != nil {
		t.Fatalf("SyncStateList on an empty ledger: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty ledger listed %d rows: %+v", len(rows), rows)
	}

	// Write peers through the real accessors. The full-counter row rides the
	// SAME table namespace but is not a peer — it must not be listed as one.
	ctx := context.Background()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	if err := syncStatePut(ctx, db, "peer-newer", 2000, 2000); err != nil {
		closeDB()
		t.Fatalf("put peer-newer: %v", err)
	}
	if err := syncStatePut(ctx, db, "peer-older", 1000, 1000); err != nil {
		closeDB()
		t.Fatalf("put peer-older: %v", err)
	}
	closeDB()

	rows, err = SyncStateList()
	if err != nil {
		t.Fatalf("SyncStateList: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("listed %d rows, want 2 (one anchor per peer): %+v", len(rows), rows)
	}
	if rows[0].Peer != "peer-older" || rows[1].Peer != "peer-newer" {
		t.Fatalf("rows must be oldest-synced first, got %q then %q", rows[0].Peer, rows[1].Peer)
	}
	if rows[0].Anchor != 1000 || rows[1].Anchor != 2000 {
		t.Fatalf("anchors not carried verbatim: %+v", rows[:2])
	}
}

// TestSyncStateListRefusesForeignLedger — error path: SyncStateList on a
// non-gopi file must report the error rather than silently listing nothing
// (a wrong GOPI_MEMORY_DB is an operator mistake, not an empty topology).
func TestSyncStateListRefusesForeignLedger(t *testing.T) {
	foreign := filepath.Join(t.TempDir(), "ledger.db")
	if err := os.WriteFile(foreign, []byte("this is not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(memoryDBEnv, foreign)
	rows, err := SyncStateList()
	if err == nil {
		t.Fatalf("a foreign ledger at %s must error, got rows=%+v", foreign, rows)
	}
	if rows != nil {
		t.Fatalf("error path must return nil rows, got %+v", rows)
	}
}

// seedTags writes a row's tags directly (fixture: TaskSubmit takes tags but
// the writeback tests need the tag column seeded mid-life).
func seedTags(t *testing.T, taskID, tags string) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET tags = ? WHERE task_id = ?`, tags, taskID); err != nil {
		t.Fatalf("seed tags %s: %v", taskID, err)
	}
}
