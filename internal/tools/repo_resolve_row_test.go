package tools

// merger-owned row markers — the ledger half of the merger redesign that
// SURVIVES it (epic merger-ff-park-file, 2026-09-16).
//
// The resolve TRACKING row (created + closed per resolve by the merger's own
// resolver child) is gone with the resolver. What stays is the two-part ledger
// contract those rows still impose on the DISPATCH side:
//
//	E2 — a pending merger-owned row (merge-conflict + auto-spawn, however it
//	    got there) is NEVER dispatchable by TaskNextFiltered: the
//	    single-ownership gate. Rows written by binaries predating this change
//	    still sit in real ledgers, so the gate keeps matching them.
//	I1 — the merge-conflict tag VALUE has ONE identity across the three faces
//	    that spell it (adoption dispatch, the sweep's dispatch tag, and the
//	    ownership gate). A rename touching one side would desync the gate from
//	    the row creators, and merger-owned rows would reach workers.
//
// Rows a predating merger minted for a non-ff branch (the park lane's own
// hand-over rows) are ORDINARY rows on purpose — they carry the merge-conflict
// tier tag but NOT auto-spawn — so the gate above must not swallow them: they
// are the exact shape E2 has to distinguish from a resolver tracking row. The
// lane that WROTE them is gone (epic merger-remove-park, 2026-09-17), the rows
// it already wrote are still in real ledgers, and this gate is what keeps them
// dispatchable.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskNextFiltered_NeverDispatchesMergerOwnedResolveRow(t *testing.T) {
	db := filepath.Join(t.TempDir(), "mem.db")
	t.Setenv("GOPI_MEMORY_DB", db)

	// Worst case: a payload-carrying merge-conflict,auto-spawn row that some
	// future path mis-created as PENDING. Even pending the single-ownership
	// gate must refuse to dispatch it to any worker.
	ownedID := newRow(t, "e2owned", "resolve merge conflict: x into master",
		taskStatusPending, resolveRowTags, "payload-brief", "auto",
		repoDeliv("x"), "")
	// A normal eligible pending row — a refused owned row must not head-block
	// the queue.
	normalID := newRow(t, "e2normal", "a normal task", taskStatusPending,
		"", "normal payload", "auto", "", "")

	got, err := TaskNextFiltered("", "", "pa-queue")
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if got.Empty {
		t.Fatalf("next returned Empty — a normal eligible pending row exists")
	}
	if got.Task.TaskID == ownedID {
		t.Fatalf("TaskNextFiltered dispatched the merger-owned row %s — gate broken", ownedID)
	}
	if got.Task.TaskID != normalID {
		t.Fatalf("next picked %s, want the normal row %s (owned row %s must be skipped)",
			got.Task.TaskID, normalID, ownedID)
	}
	if r := rowView(t, db, ownedID); r.status != taskStatusPending {
		t.Errorf("owned resolve row after next = %q, want still pending (never claimed)", r.status)
	}
}

func TestMergeConflictTag_SingleIdentityAcrossFaces(t *testing.T) {
	// The three same-package consts that reference the merge-conflict tag
	// VALUE must be one identifier: adoption dispatch writes it, the resolve
	// row creator writes it, and resolveRowIsMergerOwned (the worker
	// exclusion gate) keys on it. If a rename touched only one side, the
	// gate would desync from the row creators and merger-owned resolve rows
	// could be mis-dispatched to workers (the row-per-resolve single-owner
	// invariant). EqualFold = semantic identity, tolerant of any
	// future case-normalization of tags.
	if !strings.EqualFold(taskTagMergeConflict, resolveRowTagMergeConflict) {
		t.Errorf("adoption dispatch tag %q ≠ resolve row tag %q — one rename desyncs the resolve gate",
			taskTagMergeConflict, resolveRowTagMergeConflict)
	}
	if !strings.EqualFold(taskTagMergeConflict, taskMergeTagMergeConflict) {
		t.Errorf("taskMerge dispatch tag %q ≠ adoption tag %q — same semantic tag, two spellings",
			taskMergeTagMergeConflict, taskTagMergeConflict)
	}
	// Gate-level check: the discriminator the exclusion gate actually runs on
	// must accept the tags the adoption dispatch row is created with.
	if !resolveRowIsMergerOwned(resolveRowTags) {
		t.Errorf("resolveRowTags %q rejected by resolveRowIsMergerOwned — gate can never match its own rows",
			resolveRowTags)
	}
	if resolveRowIsMergerOwned(taskTagMergeConflict + ",auto-dispatch") {
		t.Errorf("legacy dispatch row (%s,auto-dispatch) matched the merger-owned gate — must stay dispatchable",
			taskTagMergeConflict)
	}
}

// --- helpers ---

// newRow inserts a task row with full column control and returns its id.
func newRow(t *testing.T, idTag, summary, status, tags, payload, source, deliverable, reason string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	id := idForTest(idTag)
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks
		(task_id, summary, status, session_id, model, parent_task_id, tags, payload,
		 source, deliverable, priority, reason, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, summary, status, "", "", "", tags, payload, source, deliverable, 0, reason, 1.0, 1.0); err != nil {
		t.Fatalf("insert row %s: %v", id, err)
	}
	return id
}

// idForTest mints a unique task_id (the row's identity is never the point of
// these tests; its TAGS are).
func idForTest(tag string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b) //stylegate:ignore fills a fixture buffer; the value is not asserted
	return "t-" + tag + "-" + hex.EncodeToString(b)
}

// rowView reads the operator-relevant columns of one row.
type rowViewT struct {
	status, reason, tags, deliverable string
}

func rowView(t *testing.T, db, taskID string) rowViewT {
	t.Helper()
	ctx := context.Background()
	led, err := openTasksDB(ctx, db)
	if err != nil {
		t.Fatalf("open ledger for %s: %v", taskID, err)
	}
	defer led.Close()
	var r rowViewT
	if err := led.QueryRowContext(ctx,
		`SELECT status, reason, tags, deliverable FROM tasks WHERE task_id = ?`, taskID).
		Scan(&r.status, &r.reason, &r.tags, &r.deliverable); err != nil {
		t.Fatalf("read row %s: %v", taskID, err)
	}
	return r
}

// repoDeliv renders the repo+branch deliverable the stamping lanes write.
func repoDeliv(branch string) string {
	return `{"repo":"/tmp/t","branch":"` + branch + `"}`
}
