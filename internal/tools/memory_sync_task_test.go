package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// taskSyncTestDB prepares an isolated ledger (GOPI_MEMORY_DB + GOPI_HOME)
// and returns its path (the memory_sync_test.go syncTestDB pattern).
func taskSyncTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_HOME", dir)
	return dbPath
}

// taskSyncSeed queues n pending tasks with distinct summaries.
func taskSyncSeed(t *testing.T, n int, tag string) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out, err := TaskSubmit(TaskSubmitOptions{
			Summary: "sync task number " + string(rune('a'+i)),
			Payload: "payload for sync task " + string(rune('a'+i)),
			Tags:    tag,
			Source:  "human",
		})
		if err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
		ids = append(ids, taskSyncIDFromOut(t, out))
	}
	return ids
}

// taskSyncIDFromOut extracts the task id from a TaskSubmit confirmation
// (uuid v7 for auto-submits, t-* or custom for explicit-id fixtures).
func taskSyncIDFromOut(t *testing.T, out string) string {
	t.Helper()
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "t-") || len(f) == 36 {
			return strings.TrimSuffix(f, ",")
		}
	}
	t.Fatalf("no task id in %q", out)
	return ""
}

// taskSyncStatus loads a row's current status (nil row = "").
func taskSyncStatus(t *testing.T, taskID string) (string, string, string) {
	t.Helper()
	row, err := syncTaskByID(taskID)
	if err != nil || row == nil {
		return "", "", ""
	}
	return row.Status, row.Summary, row.Tags
}

// TestTaskSyncExportPending — the peer1 side: --status pending exports live
// rows status-less (execution state never travels), --limit caps the count,
// --tags filters.
func TestTaskSyncExportPending(t *testing.T) {
	dbPath := taskSyncTestDB(t)
	taskSyncSeed(t, 3, "sometag")

	opts := SyncTaskExportOptions{DBPath: dbPath, Status: taskStatusPending, Limit: 2}
	patch, stats, err := SyncTaskExport(opts)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Scanned != 3 || len(patch.Rows) != 2 {
		t.Fatalf("export: scanned=%d rows=%d, want scanned=3 rows=2 (--limit)", stats.Scanned, len(patch.Rows))
	}
	if patch.Node == "" || patch.Version != syncTaskPatchVersion {
		t.Fatalf("patch header missing node/version: %+v", patch)
	}
	for _, r := range patch.Rows {
		if r.Status != "" {
			t.Errorf("live row %s exported with status %q — execution state must not travel", r.TaskID, r.Status)
		}
		if r.TaskID == "" || r.Summary == "" {
			t.Errorf("row missing task_id/summary: %+v", r)
		}
		if !strings.Contains(strings.Join(r.Tags, ","), "sometag") {
			t.Errorf("row tags lost: %v", r.Tags)
		}
		if r.Payload == "" {
			t.Errorf("payload not exported: %+v", r)
		}
	}
	// Oldest-first ordering (the dispatcher's claim order).
	if patch.Rows[0].CreatedAt > patch.Rows[1].CreatedAt {
		t.Errorf("export not oldest-first: %d > %d", patch.Rows[0].CreatedAt, patch.Rows[1].CreatedAt)
	}
}

// TestTaskSyncExportTerminalCarriesStatus — a completed row exported with
// --status completed carries its status (the reverse sync-back payload).
func TestTaskSyncExportTerminalCarriesStatus(t *testing.T) {
	dbPath := taskSyncTestDB(t)
	ids := taskSyncSeed(t, 1, "")
	// Walk the legal local lifecycle (pending→claimed→in_progress→completed)
	// so the row is genuinely terminal before export.
	for _, s := range []string{taskStatusClaimed, taskStatusInProgress, taskStatusCompleted} {
		if _, err := TaskUpdate(ids[0], s); err != nil {
			t.Fatal(err)
		}
	}
	patch, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: dbPath, Status: taskStatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.Rows) != 1 || patch.Rows[0].Status != taskStatusCompleted {
		t.Fatalf("terminal row must export with status=completed: %+v", patch.Rows)
	}
	if patch.Rows[0].TaskID != ids[0] {
		t.Errorf("task_id not preserved: %s != %s", patch.Rows[0].TaskID, ids[0])
	}
}

// TestTaskSyncImportFreshQueue — HP side: importing a live patch queues new
// pending rows through TaskSubmit (status=pending, from-node tag forced).
func TestTaskSyncImportFreshQueue(t *testing.T) {
	taskSyncTestDB(t)

	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1", ExportedAt: 1234,
		Rows: []SyncTaskRow{
			{TaskID: "t-1001", Summary: "do the thing", Payload: "payload body", Tags: []string{"bug", "ui"}, Source: "llm"},
			{TaskID: "t-1002", Summary: "second thing"},
		},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}

	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsImported != 2 || stats.RowsDuped != 0 || stats.RowsRefused != 0 {
		t.Fatalf("stats = %+v, want 2 imported 0 duped 0 refused", stats)
	}
	for _, want := range []struct{ id, summary string }{{"t-1001", "do the thing"}, {"t-1002", "second thing"}} {
		status, summary, tags := taskSyncStatus(t, want.id)
		if status != taskStatusPending {
			t.Errorf("%s status = %q, want pending", want.id, status)
		}
		if summary != want.summary {
			t.Errorf("%s summary = %q, want %q", want.id, summary, want.summary)
		}
		if !strings.Contains(tags, "from-peer1") {
			t.Errorf("%s tags = %q — from-node tag must be forced at import", want.id, tags)
		}
	}
	if _, _, tags := taskSyncStatus(t, "t-1001"); !strings.Contains(tags, "bug") || !strings.Contains(tags, "ui") {
		t.Errorf("row tags lost in import: %q", tags)
	}
}

// TestTaskSyncImportIdempotent — re-importing the same patch: both routes
// now answer "this row is already here" — the two rows sharing summary+
// payload report already (the shared id IS the work, and the content ladder
// WARNED about the twin), and the third row likewise. ZERO new rows (the
// epic's 幂等 requirement).
//
// UPDATED for 01a0fea1 ③: the old contract answered `duped` for every row of
// the second import because the payload-hash gate skipped them; identity is
// the task_id now, so the honest answer is `already` — the row is present,
// nothing to apply — and the content ladder is a warning counter
// (RowsDuped/RowsContentWarn) rather than a skip.
func TestTaskSyncImportIdempotent(t *testing.T) {
	taskSyncTestDB(t)

	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1",
		Rows: []SyncTaskRow{
			{TaskID: "t-2001", Summary: "same work", Payload: "same body"},
			{TaskID: "t-2002", Summary: "same work", Payload: "same body"}, // content twin of t-2001
			{TaskID: "t-2003", Summary: "distinct work", Payload: "distinct body"},
		},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}

	first, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	// All three rows land (identity is the task_id); the twin is WARNED.
	if first.RowsImported != 3 || first.RowsContentWarn != 1 {
		t.Fatalf("first import: %+v, want 3 imported 1 content-warned", first)
	}

	second, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if second.RowsImported != 0 {
		t.Fatalf("re-import: %+v, want 0 imported (idempotency contract)", second)
	}
	if second.RowsAlready != 3 {
		t.Fatalf("re-import: %+v, want 3 already (every row is present under its own id)", second)
	}
	// The ledger holds exactly three rows.
	var n int
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("ledger rows = %d (%v), want 3 — re-import must not duplicate", n, err)
	}
}

// TestTaskSyncImportHashDedupLive — a patch row whose (summary,payload)
// matches a LIVE local row queued under a DIFFERENT task_id: the content
// ladder WARNS, and the row still lands under its own id.
//
// UPDATED for 01a0fea1 ③: master SKIPPED this row (deduped=1, imported=0) —
// a legitimate second submission of the same brief was silently dropped.
// Identity is the task_id, so the row is imported; the twin is reported.
func TestTaskSyncImportHashDedupLive(t *testing.T) {
	taskSyncTestDB(t)
	// Locally queued work — same summary+payload as the patch will carry.
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "identical work", Payload: "identical body", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1",
		Rows: []SyncTaskRow{{TaskID: "t-9999", Summary: "identical work", Payload: "identical body"}},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsImported != 1 || stats.RowsContentWarn != 1 {
		t.Fatalf("stats = %+v, want 1 imported + 1 content warning (the row lands; the twin is reported)", stats)
	}
	if status, _, _ := taskSyncStatus(t, "t-9999"); status != taskStatusPending {
		t.Fatalf("t-9999 status = %q, want pending (the row must land)", status)
	}
}

// TestTaskSyncImportNoOverwrite — mechanic ⑤: an existing pending row's
// summary/payload are NEVER rewritten; only tags gain the from-node union.
func TestTaskSyncImportNoOverwrite(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "original summary", Payload: "original payload", Tags: "local-tag", Source: "human", TaskID: "t-3001"}); err != nil {
		t.Fatal(err)
	}
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1",
		Rows: []SyncTaskRow{{TaskID: "t-3001", Summary: "HACKED summary", Payload: "HACKED payload", Tags: []string{"injected"}}},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	// Post-collision-fix semantics: HACKED summary/payload is DIFFERENT work
	// under the same id → relabeled to t-3001-npeer1 (never absorbed into the
	// local row). Mechanic ⑤ intact: the local row's data is untouched.
	if stats.RowsRelabeled != 1 || stats.RowsImported != 1 {
		t.Fatalf("stats = %+v, want 1 relabeled 1 imported", stats)
	}
	status, summary, tags := taskSyncStatus(t, "t-3001")
	if status != taskStatusPending {
		t.Errorf("status = %q, want pending", status)
	}
	if summary != "original summary" {
		t.Errorf("summary OVERWRITTEN to %q — mechanic ⑤ violated", summary)
	}
	if !strings.Contains(tags, "local-tag") {
		t.Errorf("local tags lost: %q", tags)
	}
	// Payload untouched (the hash identity check).
	row, _ := syncTaskByID("t-3001") //stylegate:ignore reads a row in a fixture; the compared row is the assertion
	if row == nil || row.Payload != "original payload" {
		t.Errorf("payload OVERWRITTEN — mechanic ⑤ violated: %+v", row)
	}
	// The incoming (different-work) row queued under the relabeled id.
	hacked, _ := syncTaskByID("t-3001-npeer1") //stylegate:ignore reads a row in a fixture; the compared row is the assertion
	if hacked == nil || hacked.Summary != "HACKED summary" || hacked.Status != taskStatusPending {
		t.Errorf("relabeled incoming row missing/wrong: %+v", hacked)
	}
}

// TestTaskSyncForwardMigration — the reverse sync-back: HP finished the
// work, exports completed rows, peer1 imports → its pending row migrates
// forward to completed (taskAllowedTransitions: pending→completed legal).
// A second import is idempotent (same status → already).
func TestTaskSyncForwardMigration(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "forward me", TaskID: "t-4001"}); err != nil {
		t.Fatal(err)
	}
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "hp",
		Rows: []SyncTaskRow{{TaskID: "t-4001", Summary: "forward me", Status: taskStatusCompleted}},
	}
	patchPath := filepath.Join(t.TempDir(), "back.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsMigrated != 1 {
		t.Fatalf("stats = %+v, want 1 migrated", stats)
	}
	if status, _, _ := taskSyncStatus(t, "t-4001"); status != taskStatusCompleted {
		t.Fatalf("status = %q, want completed (forward migration)", status)
	}
	again, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if again.RowsAlready != 1 || again.RowsMigrated != 0 {
		t.Errorf("re-import = %+v, want idempotent already", again)
	}
}

// TestTaskSyncForwardMigrationGates — cancelled is NOT remote-forwardable
// (a local judgment stays local), and a terminal patch row against a
// non-pending local row (in_progress) is refused by the state machine.
func TestTaskSyncForwardMigrationGates(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "cancel judgement", TaskID: "t-5001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "busy local", TaskID: "t-5002"}); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskUpdate("t-5002", taskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "hp",
		Rows: []SyncTaskRow{
			{TaskID: "t-5001", Summary: "cancel judgement", Status: taskStatusCancelled},
			{TaskID: "t-5002", Summary: "busy local", Status: taskStatusCompleted},
		},
	}
	patchPath := filepath.Join(t.TempDir(), "back.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	// M4.20-B rank model: forward is FORWARD — in_progress→completed is a
	// legal rank advance (2<3), and the cancelled remote intent rides the
	// same-tier ruling. The old state-machine-refusal semantics (nick veto:
	// "status cannot go backward, it must go forward") is retired.
	if stats.RowsAlready != 1 || stats.RowsMigrated != 1 {
		t.Errorf("rank model: want 1 already (pending vs pending echo) + 1 migrated (in_progress→completed): %+v", stats)
	}
	if status, _, _ := taskSyncStatus(t, "t-5002"); status != taskStatusCompleted {
		t.Errorf("t-5002 status = %q, want completed (rank forward 2→3)", status)
	}
}

// TestTaskSyncImportIdCollisionDistinctWork — mechanic ②: a colliding
// task_id with DISTINCT work (no hash hit) is never rewritten — the pending
// row is kept and only the from-node tags union in (⑤); terminal-against-
// unknown-id is refused (import never re-runs finished work).
func TestTaskSyncImportIdCollisionDistinctWork(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "local original", Payload: "local body", Tags: "local-tag", Source: "human", TaskID: "t-8001"}); err != nil {
		t.Fatal(err)
	}
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1",
		Rows: []SyncTaskRow{
			{TaskID: "t-8001", Summary: "different work entirely", Payload: "different body"},
			{TaskID: "t-8002", Summary: "ghost terminal", Status: taskStatusCompleted},
		},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	// Post-collision semantics + M4.20-B rank: the colliding id is RELABELED
	// (mechanic ② kept), and the terminal ghost row (t-8002) is now IMPORTED
	// under the rank model — a completed row on a fresh local id is a plain
	// new-row insert (rank 3 beats nonexistent), not a refusal (the old
	// "never re-run finished work" gate assumed the local row existed).
	if stats.RowsRelabeled != 1 || stats.RowsRefused != 0 || stats.RowsImported != 2 {
		t.Fatalf("stats = %+v, want 1 relabeled 0 refused 2 imported (rank model)", stats)
	}
	if status, summary, _ := taskSyncStatus(t, "t-8001"); status != taskStatusPending || summary != "local original" {
		t.Errorf("collision row mutated: status=%s summary=%q — mechanic ②/⑤ violated", status, summary)
	}
	if status, _, _ := taskSyncStatus(t, "t-8001-npeer1"); status != taskStatusPending {
		t.Errorf("relabeled row t-8001-npeer1 missing (status=%q)", status)
	}
	if status, _, _ := taskSyncStatus(t, "t-8002"); status != taskStatusCompleted {
		t.Errorf("terminal ghost row: status=%q, want completed (rank model inserts fresh)", status)
	}
}

// TestTaskSyncBrewChannelUnaffected — zero-regression: the brew channel's
// export/import behave exactly as before the task extension (brew rows
// travel, task tables untouched by the brew path).
func TestTaskSyncBrewChannelUnaffected(t *testing.T) {
	dbPath := taskSyncTestDB(t)
	taskSyncSeed(t, 1, "unrelated")

	if _, err := MemoryStore("brew: keep", "brew regression question", "brew regression answer", BrewSourceTag, nil); err != nil {
		t.Fatal(err)
	}
	patch, _, err := SyncExport(SyncExportOptions{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(patch.Rows) != 1 {
		t.Fatalf("brew export changed: %d rows (want 1)", len(patch.Rows))
	}
	// Import the brew patch into the SAME db — dedup path (human row for a
	// different question must not interfere).
	patchPath := filepath.Join(t.TempDir(), "brew.json")
	if err := WriteSyncPatch(patch, patchPath); err != nil {
		t.Fatal(err)
	}
	imported, err := SyncImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if imported.RowsImported != 0 || imported.RowsDeduped != 1 {
		t.Fatalf("brew import changed: %+v (same-db re-import = 1 deduped)", imported)
	}
	// Task table untouched by the brew round-trip.
	var n int
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("tasks rows = %d (%v), want 1 — brew channel must not touch tasks", n, err)
	}
}

// TestTaskSyncImportValidation — garbage gate: empty summary refused, bad
// source refused, missing file errors.
func TestTaskSyncImportValidation(t *testing.T) {
	taskSyncTestDB(t)
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1",
		Rows: []SyncTaskRow{
			{TaskID: "t-6001", Summary: "   "},
			{TaskID: "t-6002", Summary: "bad source", Source: "aliens"},
		},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsRefused != 2 || len(stats.Errors) != 2 {
		t.Fatalf("stats = %+v, want 2 refused with reasons", stats)
	}
	if _, err := SyncTaskImport(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("missing patch file must error")
	}
}

// TestTaskSyncImportNoNodeTag — a patch without Node still imports, tagged
// from-unknown-node (never an empty provenance tag).
func TestTaskSyncImportNoNodeTag(t *testing.T) {
	taskSyncTestDB(t)
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion,
		Rows:    []SyncTaskRow{{TaskID: "t-7001", Summary: "no node"}},
	}
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncTaskImport(patchPath); err != nil {
		t.Fatal(err)
	}
	if _, _, tags := taskSyncStatus(t, "t-7001"); !strings.Contains(tags, "from-unknown-node") {
		t.Errorf("tags = %q, want from-unknown-node", tags)
	}
}

// TestTaskSyncExportInvalidStatus — the export rejects a typo'd --status
// (no silent empty results).
func TestTaskSyncExportInvalidStatus(t *testing.T) {
	dbPath := taskSyncTestDB(t)
	if _, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: dbPath, Status: "done"}); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("err = %v, want invalid status", err)
	}
}

// TestTaskSyncImportIdCollisionRelabelsDistinctWork — the Epic's silent-loss
// bug: two nodes generate t-<unix> in the SAME SECOND; the importer must not
// absorb HP's new bug report as "already exists" (tags-union on a colliding
// id). Different work under the same id → relabel <id>-n<node> + explicit
// report; same work → the old idempotent semantics; terminal-different-work →
// refused (no ghost completion). Re-import of the relabeled patch = duped.
func TestTaskSyncImportIdCollisionRelabelsDistinctWork(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "local original", Payload: "local body", Tags: "local-tag", Source: "human", TaskID: "t-9001"}); err != nil {
		t.Fatal(err)
	}
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "hp",
		Rows: []SyncTaskRow{
			{TaskID: "t-9001", Summary: "hp bug report", Payload: "hp body"},         // collision, distinct work → relabel
			{TaskID: "t-9002", Summary: "same work", Payload: "same body"},           // no local twin → plain import
			{TaskID: "t-9003", Summary: "ghost finish", Status: taskStatusCompleted}, // terminal ghost → refused
		},
	}
	patchPath := filepath.Join(t.TempDir(), "collide.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsRelabeled != 1 {
		t.Fatalf("stats = %+v, want 1 relabeled", stats)
	}
	// M4.20-B rank: ghost terminal rows insert as fresh completed rows
	// (rank 3 on nonexistent local) — the old refusal gate assumed the
	// row existed. 3 = relabel + fresh + ghost terminal.
	if stats.RowsImported != 3 || stats.RowsRefused != 0 {
		t.Fatalf("stats = %+v, want 3 imported (relabel+fresh+ghost) 0 refused (rank model)", stats)
	}
	// The original row is untouched; the incoming work landed under -nhp.
	if status, summary, _ := taskSyncStatus(t, "t-9001"); status != taskStatusPending || summary != "local original" {
		t.Errorf("collision row mutated: status=%s summary=%q", status, summary)
	}
	if status, _, _ := taskSyncStatus(t, "t-9001-nhp"); status != taskStatusPending {
		t.Errorf("relabeled row t-9001-nhp missing (status=%q)", status)
	}
	relabelReport := false
	for _, e := range stats.Errors {
		if strings.Contains(e, "relabeled t-9001 → t-9001-nhp") {
			relabelReport = true
		}
	}
	if !relabelReport {
		t.Errorf("no explicit relabel report in errors: %v", stats.Errors)
	}
	// Idempotent re-import: every row is already present under its own id,
	// and the collision row is NOT relabeled a second time (01a0fea1 ③ —
	// the relabel is one-shot, judged on the summary).
	again, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if again.RowsRelabeled != 0 {
		t.Errorf("re-import relabeled again: %+v — relabel must be one-shot", again)
	}
	if again.RowsAlready != 3 || again.RowsImported != 0 {
		// UPDATED for 01a0fea1 ③: master answered `duped` for the two live
		// rows (the payload-hash gate skipped them before the id branch).
		// Identity is the task_id, so the second pass finds the row present
		// and answers `already` — same count, honest gate.
		t.Errorf("re-import = %+v, want 3 already 0 imported (every row present under its own id)", again)
	}
}

// TestTaskSyncImportCollisionTerminalDifferentWorkRefused — a terminal
// sync-back under a colliding id with DIFFERENT work must never fabricate a
// completion of the local row.
func TestTaskSyncImportCollisionTerminalDifferentWorkRefused(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "pending here", Payload: "p1", TaskID: "t-9101"}); err != nil {
		t.Fatal(err)
	}
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "hp",
		Rows: []SyncTaskRow{{TaskID: "t-9101", Summary: "different task", Payload: "p2", Status: taskStatusCompleted}},
	}
	patchPath := filepath.Join(t.TempDir(), "ghost.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsMigrated != 0 || stats.RowsRelabeled != 0 || stats.RowsRefused != 1 {
		t.Fatalf("stats = %+v, want refused, no ghost completion", stats)
	}
	if status, _, _ := taskSyncStatus(t, "t-9101"); status != taskStatusPending {
		t.Errorf("local row fabricated to %q — must stay pending", status)
	}
}

// TestHarvestCollisionRelabelsDistinctWork — harvest twin of the import fix:
// a peer row arriving under a same-second colliding id with different work is
// relabeled <id>-n<peer> and queued, not conflict-stranded.
func TestHarvestCollisionRelabelsDistinctWork(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "local first", Payload: "lp", TaskID: "t-9201"}); err != nil {
		t.Fatal(err)
	}
	fetch := func(peer string, anchor float64) ([]harvestRemoteRow, error) {
		return []harvestRemoteRow{{
			TaskID: "t-9201", Summary: "peer bug", Payload: "pp",
			Status: "pending", CreatedAt: 1000, UpdatedAt: 1000,
		}}, nil
	}
	harvestDeclarePeer(t, harvestFixturePeer)
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Fetch: fetch})
	if err != nil {
		t.Fatal(err)
	}
	if counts.Relabeled != 1 || counts.Conflict != 0 {
		t.Fatalf("counts = %+v, want 1 relabeled 0 conflict", counts)
	}
	if status, _, _ := taskSyncStatus(t, "t-9201"); status != taskStatusPending {
		t.Errorf("local row disturbed: %q", status)
	}
	if status, _, _ := taskSyncStatus(t, "t-9201-nhp"); status != taskStatusPending {
		t.Errorf("relabeled harvest row t-9201-nhp missing (status=%q)", status)
	}
}

// taskSyncSeedTerminal walks a row through the legal local lifecycle to a
// terminal status (pending→claimed→in_progress→<terminal>).
func taskSyncSeedTerminal(t *testing.T, taskID, terminal string) {
	t.Helper()
	for _, s := range []string{taskStatusClaimed, taskStatusInProgress, terminal} {
		if _, err := TaskUpdate(taskID, s); err != nil {
			t.Fatalf("seed terminal %s: %v", terminal, err)
		}
	}
}

// taskSyncRowID builds a one-row patch for import tests.
func taskSyncRowID(id, status string) SyncTaskPatch {
	return SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1", ExportedAt: 1234,
		Rows: []SyncTaskRow{{TaskID: id, Summary: "sync task number a", Payload: "payload for sync task a", Status: status}},
	}
}

// taskSyncImportRow writes the patch to a temp file and imports it.
func taskSyncImportRow(t *testing.T, patch SyncTaskPatch) *SyncTaskImportStats {
	t.Helper()
	data, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "patch.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(path)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

// taskSyncImportHasTag reports whether a row's tags carry the tag.
func taskSyncImportHasTag(t *testing.T, taskID, want string) bool {
	t.Helper()
	_, _, tags := taskSyncStatus(t, taskID)
	return strings.Contains(tags, want)
}

// TestTaskSyncCompletedBeatsCancelled — THE epic ruling: a completed patch
// (work finished on the other node) meeting a locally cancelled row must
// WIN — the row migrates cancelled→completed and the cancel intent stays
// on the row as the cancel-was-intent tag (work is never dropped, intent
// is never lost).
func TestTaskSyncCompletedBeatsCancelled(t *testing.T) {
	taskSyncTestDB(t)
	ids := taskSyncSeed(t, 1, "")
	taskSyncSeedTerminal(t, ids[0], taskStatusCancelled) // local: cancelled (PA's intent to abandon)

	stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusCompleted)) // remote: completed (HP finished the work)

	if stats.RowsMigrated != 1 || stats.RowsRefused != 0 {
		t.Fatalf("stats = %+v, want migrated=1 refused=0 (completed wins)", stats)
	}
	status, _, _ := taskSyncStatus(t, ids[0])
	if status != taskStatusCompleted {
		t.Fatalf("status = %s, want completed (work outcome kept)", status)
	}
	if !taskSyncImportHasTag(t, ids[0], "cancel-was-intent") {
		t.Fatalf("cancel-was-intent tag missing — intent must survive the ruling")
	}
}

// TestTaskSyncCancelledLosesToTerminalWork — the mirror half of the ruling:
// a cancelled patch meeting locally finished work (completed) does NOT
// demote the row — the work stays, the intent lands as a tag.
func TestTaskSyncCancelledLosesToTerminalWork(t *testing.T) {
	taskSyncTestDB(t)
	ids := taskSyncSeed(t, 1, "")
	taskSyncSeedTerminal(t, ids[0], taskStatusCompleted) // local: completed (work done)

	stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusCancelled)) // remote: cancelled (intent to abandon)

	if stats.RowsAlready != 1 || stats.RowsRefused != 0 || stats.RowsMigrated != 0 {
		t.Fatalf("stats = %+v, want already=1 refused=0 migrated=0 (work never demoted)", stats)
	}
	status, _, _ := taskSyncStatus(t, ids[0])
	if status != taskStatusCompleted {
		t.Fatalf("status = %s, want completed (terminal work never demoted)", status)
	}
	if !taskSyncImportHasTag(t, ids[0], "cancel-was-intent") {
		t.Fatalf("cancel-was-intent tag missing — intent must be recorded")
	}
}

// TestTaskSyncCancelConflictMatrix — the remaining terminal-vs-terminal
// combinations: failed/verified patches also beat a cancelled row (any
// finished-work outcome outranks intent), double-cancel is idempotent,
// and the original gates (live rows, same-status re-imports) hold.
func TestTaskSyncCancelConflictMatrix(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	t.Run("failed-patch-beats-cancelled", func(t *testing.T) {
		taskSyncTestDB(t)
		ids := taskSyncSeed(t, 1, "")
		taskSyncSeedTerminal(t, ids[0], taskStatusCancelled)
		stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusFailed))
		if stats.RowsMigrated != 1 || stats.RowsRefused != 0 {
			t.Fatalf("stats = %+v, want migrated=1", stats)
		}
		status, _, _ := taskSyncStatus(t, ids[0])
		if status != taskStatusFailed {
			t.Fatalf("status = %s, want failed", status)
		}
		if !taskSyncImportHasTag(t, ids[0], "cancel-was-intent") {
			t.Fatal("cancel-was-intent tag missing")
		}
	})
	t.Run("verified-patch-beats-cancelled", func(t *testing.T) {
		taskSyncTestDB(t)
		ids := taskSyncSeed(t, 1, "")
		taskSyncSeedTerminal(t, ids[0], taskStatusCancelled)
		stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusVerified))
		if stats.RowsMigrated != 1 || stats.RowsRefused != 0 {
			t.Fatalf("stats = %+v, want migrated=1", stats)
		}
		status, _, _ := taskSyncStatus(t, ids[0])
		if status != taskStatusVerified {
			t.Fatalf("status = %s, want verified", status)
		}
	})
	t.Run("double-cancel-idempotent", func(t *testing.T) {
		taskSyncTestDB(t)
		ids := taskSyncSeed(t, 1, "")
		taskSyncSeedTerminal(t, ids[0], taskStatusCancelled)
		for i := 0; i < 2; i++ {
			stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusCancelled))
			if stats.RowsAlready != 1 || stats.RowsRefused != 0 {
				t.Fatalf("run %d: stats = %+v, want already=1 refused=0", i+1, stats)
			}
		}
		status, _, _ := taskSyncStatus(t, ids[0])
		if status != taskStatusCancelled {
			t.Fatalf("status = %s, want cancelled (both sides agree)", status)
		}
	})
	t.Run("cancelled-patch-pending-row-tagged-not-cancelled", func(t *testing.T) {
		taskSyncTestDB(t)
		ids := taskSyncSeed(t, 1, "") // local pending
		stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusCancelled))
		if stats.RowsAlready != 1 || stats.RowsRefused != 0 {
			t.Fatalf("stats = %+v, want already=1 (cancel intent recorded, row untouched)", stats)
		}
		status, _, _ := taskSyncStatus(t, ids[0])
		if status != taskStatusPending {
			t.Fatalf("status = %s, want pending (local dispatcher owns the cancel)", status)
		}
		if !taskSyncImportHasTag(t, ids[0], "cancel-was-intent") {
			t.Fatal("cancel-was-intent tag missing on pending row")
		}
	})
	t.Run("live-local-row-rank-advances-to-terminal", func(t *testing.T) {
		// M4.20-B rank model: a live local row (claimed) meeting a remote
		// terminal verdict is a FORWARD move (rank 1→3) — the start-point
		// atomicity makes the terminal verdict the single-runner truth; a
		// locally live row whose peer already delivered terminal is a
		// crashed runner's residue. The old "live execution protection"
		// refusal is retired with the rank ladder.
		taskSyncTestDB(t)
		ids := taskSyncSeed(t, 1, "")
		if _, err := TaskUpdate(ids[0], taskStatusClaimed); err != nil {
			t.Fatal(err)
		}
		stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusCompleted))
		if stats.RowsMigrated != 1 {
			t.Fatalf("stats = %+v, want migrated=1 (rank advance claimed→completed)", stats)
		}
	})
	t.Run("same-status-reimport-already", func(t *testing.T) {
		taskSyncTestDB(t)
		ids := taskSyncSeed(t, 1, "")
		taskSyncSeedTerminal(t, ids[0], taskStatusCompleted)
		stats := taskSyncImportRow(t, taskSyncRowID(ids[0], taskStatusCompleted))
		if stats.RowsAlready != 1 || stats.RowsRefused != 0 {
			t.Fatalf("stats = %+v, want already=1 (idempotent sync-back re-import)", stats)
		}
	})
}

// TestUnknownNode_MirrorLiteralGate — '"unknown-node"' must live ONLY at the
// syncTaskUnknownNode definition (memory_sync_task.go). The three sibling
// importers (memory_sync.go / memory_sync_procedural.go / memory_sync_assets.go)
// fell back to a bare literal when added later; the gate closes that hole:
// any future re-inline or cross-file copy fails here (Rule 1 SSOT).
func TestUnknownNode_MirrorLiteralGate(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	const holder = "memory_sync_task.go"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		// The ONE allowed holder is the const definition file; every other
		// production file must reference syncTaskUnknownNode.
		if e.Name() == holder {
			continue
		}
		b, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		if bytes.Contains(b, []byte(`"unknown-node"`)) {
			t.Errorf("%s carries the literal \"unknown-node\" — reference syncTaskUnknownNode (defined in %s)", e.Name(), holder)
		}
	}
}

// ---------------------------------------------------------------------------
// memsync import atomicity (epic memsync-import-atomic)
//
// These tests are the regression guard for the WHOLE-PATCH transaction in
// SyncTaskImport (importTaskBeginTx). The defect they pin: the import used to
// apply rows through per-helper connections with per-statement auto-commit, so
// a mid-patch SQLITE_BUSY (or a crash) left a PARTIAL patch in the ledger,
// indistinguishable from real work. The contract now is all-or-nothing — a
// storage failure rolls the whole batch back (zero rows) and reports it; a
// policy refusal records the row and continues, because it writes nothing.
// ---------------------------------------------------------------------------

const (
	// The probe task_id prefixes. Exact-prefix counts keep a test's own
	// fixture rows (the probe's lock holder) out of the assertions.
	memsyncAtomicPrefix   = "t-atomic-probe-"
	memsyncAtomicHolderID = "t-atomic-holder-row"
	memsyncRetryPrefix    = "t-retry-probe-"
	memsyncRacePrefix     = "t-race-probe-"
	// memsyncRetryRows / memsyncRaceRows are the patch sizes of the
	// idempotency and concurrency tests.
	memsyncRetryRows = 4
	memsyncRaceRows  = 8
)

// memsyncMidPatchHolder is the SQLite write-lock probe the atomicity test
// drives (see testdata/memsync-midpatch-hold.sh). Named (Rule 1b): the file
// name is load-bearing and must not be re-typed at the call site.
const memsyncMidPatchHolder = "memsync-midpatch-hold.sh"

// taskSyncWritePatch renders a patch file for the import tests.
func taskSyncWritePatch(t *testing.T, src SyncTaskPatch) string {
	t.Helper()
	patchPath := filepath.Join(t.TempDir(), "patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	return patchPath
}

// taskSyncCountRows counts ledger rows (optionally by task_id prefix).
func taskSyncCountRows(t *testing.T, like string) int {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	var n int
	q := `SELECT COUNT(*) FROM tasks`
	var args []any
	if like != "" {
		q += ` WHERE task_id LIKE ?`
		args = append(args, like+"%")
	}
	if err := db.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// taskSyncHoldWriteLock runs the probe script and waits until it has the write
// lock (the file marker is written the moment BEGIN IMMEDIATE succeeded).
// Returns a release func. A probe that cannot take the lock is an inconclusive
// run, reported as a skip — never as a pass.
func taskSyncHoldWriteLock(t *testing.T, dbPath, rowID string, hold time.Duration) func() {
	t.Helper()
	script := filepath.Join("testdata", memsyncMidPatchHolder)
	sig := filepath.Join(t.TempDir(), "lock-held")
	cmd := exec.Command(script, dbPath, sig, fmt.Sprintf("%.0f", hold.Seconds()), rowID) //nolint:gosec // fixed probe script, test-owned args
	if err := cmd.Start(); err != nil {
		t.Skipf("lock probe unavailable: %v", err)
	}
	deadline := time.Now().Add(memsyncProbeReadyTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sig); err == nil {
			// The probe is KILLED on release: it may still be sleeping out
			// its hold, and waiting for that would only lengthen the test —
			// its lock is released by the process dying.
			return func() {
				_ = cmd.Process.Kill() //stylegate:ignore the probe's only job (owning the lock) is over; killing it is how the test ends the window
				_ = cmd.Wait()         //stylegate:ignore reaping the killed probe; the exit code carries nothing the assertions need
			}
		}
		time.Sleep(memsyncProbePollInterval) //stylegate:allow-sleep poll interval while the probe child takes the memsync lock; the lock state is only observable by sampling
	}
	_ = cmd.Process.Kill() //stylegate:ignore probe never took the lock; killing it is the only way to unblock Wait
	_ = cmd.Wait()         //stylegate:ignore reaping the killed probe
	t.Skipf("lock probe did not take the write lock within %s — inconclusive, not a pass", memsyncProbeReadyTimeout)
	return nil
}

const (
	// memsyncProbeReadyTimeout bounds the wait for the probe's lock marker.
	memsyncProbeReadyTimeout = 10 * time.Second
	// memsyncProbePollInterval is the marker poll period.
	memsyncProbePollInterval = 10 * time.Millisecond
	// memsyncProbeContendHold is how long the contention probe owns the write
	// lock: long enough that the import cannot finish before the probe's first
	// write window, short enough to keep the test cheap. The probe is killed
	// on release, so the hold is an upper bound, not a sleep.
	memsyncProbeContendHold = 5 * time.Second
	// memsyncProbeRows is the patch size.
	memsyncProbeRows = 6
)

// memsyncProbeTexts are the probe rows' word sets. The import's M4.8 dedup
// ladder (exact → canonical → trigram-similar) reports near-identical work as
// twins, and a patch emptied by the ladder would never reach its write phase —
// so each row uses a DIFFERENT sentence, sharing no phrase with its siblings.
var memsyncProbeTexts = [][2]string{
	{"roll back the ledger patch when the write lock is taken", "the patch applies as one unit so a failure leaves nothing behind"},
	{"measure how long a row costs under indexed write triggers", "timing tells us whether a timeout has to grow"},
	{"audit the transaction boundary of the import path", "a boundary exists only if every writer shares one handle"},
	{"compare two nodes exchanging task rows at the same moment", "the loser of the race must write nothing at all"},
	{"document the contract at the point where callers read it", "the comment names both failure modes and what a caller sees"},
	{"count rows after an interrupted conversation with the database", "counting is the only evidence that survives a crash"},
	{"verify a retry applies the whole batch a second time", "a retry is safe when the first attempt left no residue"},
	{"inspect the refusal path that records a row and continues", "a refusal writes nothing so continuing cannot split a patch"},
}

// memsyncProbeRow builds the i-th probe row — text distinct by construction so
// the dedup ladder cannot swallow the patch before its write phase.
func memsyncProbeRow(prefix string, i int) SyncTaskRow {
	text := memsyncProbeTexts[i%len(memsyncProbeTexts)]
	return SyncTaskRow{
		TaskID:  fmt.Sprintf("%s%d", prefix, i),
		Summary: text[0],
		Payload: text[1],
	}
}

// TestTaskSyncImportMidPatchStorageFailureLeavesNoRows is THE red-proof for
// the whole-patch transaction.
//
// The failure is INJECTED, so the test is deterministic and costs nothing: a
// BEFORE INSERT trigger on tasks aborts the insert of one row in the middle of
// the patch, exactly the shape of a real storage failure (SQLITE_BUSY past the
// busy window, disk I/O, a constraint — the point is that it is a STORAGE
// failure, not a policy refusal, so the loop's triage must abort the batch).
//
// With the whole patch inside one transaction the abort rolls everything back:
// ZERO rows and an explicit error. Apply the patch row-by-row instead — the
// pre-fix structure, where every helper opened its own connection and each
// statement auto-committed — and rows 0..2 of this patch are already committed
// when the abort hits: the test then fails with "PARTIAL IMPORT: 3 of 6 rows
// landed". That is the regression this test exists to catch.
//
// The retry half is asserted too: once the injected failure is gone the same
// patch imports completely, because the failed attempt left nothing behind.
func TestTaskSyncImportMidPatchStorageFailureLeavesNoRows(t *testing.T) {
	taskSyncTestDB(t)
	rows := make([]SyncTaskRow, 0, memsyncProbeRows)
	for i := 0; i < memsyncProbeRows; i++ {
		rows = append(rows, memsyncProbeRow(memsyncAtomicPrefix, i))
	}
	failing := rows[memsyncProbeFailIndex].TaskID
	patchPath := taskSyncWritePatch(t, SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1", Rows: rows,
	})

	taskSyncInjectStorageFailure(t, failing)
	stats, importErr := SyncTaskImport(patchPath)

	if n := taskSyncCountRows(t, memsyncAtomicPrefix); n != 0 {
		t.Fatalf("PARTIAL IMPORT: %d of %d rows landed (error reported: %v) — the patch must be all-or-nothing",
			n, memsyncProbeRows, importErr)
	}
	if importErr == nil {
		t.Fatalf("import reported success (%+v) but wrote no rows — the abort must be explicit", stats)
	}
	if !strings.Contains(importErr.Error(), memsyncProbeAbort) {
		t.Fatalf("import failed with %v, want the injected storage failure surfaced verbatim", importErr)
	}
	// The refusal path must NOT have absorbed this: a storage failure cannot
	// be recorded per-row and continued past.
	if stats != nil && stats.RowsRefused > 0 {
		t.Fatalf("storage failure was misread as a policy refusal: %+v", stats)
	}

	// Retry contract: nothing to clean up, so the same patch applies whole.
	taskSyncClearStorageFailure(t)
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatalf("retry import: %v", err)
	}
	if stats.RowsImported != memsyncProbeRows {
		t.Fatalf("retry = %+v, want %d imported", stats, memsyncProbeRows)
	}
	if n := taskSyncCountRows(t, memsyncAtomicPrefix); n != memsyncProbeRows {
		t.Fatalf("ledger holds %d rows after the retry, want %d", n, memsyncProbeRows)
	}
}

// memsyncProbeFailIndex is the patch position whose INSERT the probe trigger
// aborts — mid-patch on purpose: a prefix exists by then, which is what makes
// a partial import observable.
const memsyncProbeFailIndex = 3

// memsyncProbeAbort is the message the injected trigger raises, so the test
// can prove the error it asserts on is the injected one.
const memsyncProbeAbort = "memsync probe: injected storage failure"

// taskSyncInjectStorageFailure installs a persistent (not TEMP) BEFORE INSERT
// trigger that aborts one specific row's insert. It must be persistent: the
// import runs on its OWN connection, which a TEMP trigger of this connection
// would not see.
func taskSyncInjectStorageFailure(t *testing.T, taskID string) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	// The condition is inlined as a quoted literal: this driver rejects a
	// bound parameter inside a trigger body ("trigger cannot use variables").
	// The task_id is a test-owned constant here, and %q quotes it as a SQL
	// string literal.
	ddl := fmt.Sprintf(`CREATE TRIGGER memsync_import_fail_probe
		BEFORE INSERT ON tasks
		WHEN NEW.task_id = %q
		BEGIN SELECT RAISE(ABORT, '%s'); END`, taskID, memsyncProbeAbort)
	_, err = db.ExecContext(context.Background(), ddl)
	if err != nil {
		t.Fatalf("inject storage failure: %v", err)
	}
}

// taskSyncClearStorageFailure removes the injected trigger.
func taskSyncClearStorageFailure(t *testing.T) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	if _, err := db.ExecContext(context.Background(),
		`DROP TRIGGER IF EXISTS memsync_import_fail_probe`); err != nil {
		t.Fatalf("clear storage failure: %v", err)
	}
}

// TestTaskSyncImportUnderWriteLockIsNeverPartial pins the CONTENTION half of
// the same invariant: while a foreign writer owns the ledger's write lock
// (BEGIN IMMEDIATE, the same mode the import's own transaction needs), an
// import may wait or fail — but it may never leave a partial patch, and what
// it reports must match what the ledger holds.
//
// Measured on this driver, the BEGIN queues past the busy pragma rather than
// failing (see devdoc/memsync-import-atomic.md), which is why the probe holds
// the lock for a short window and the assertions accept either outcome. The
// probe is killed on release, so the lock is freed the moment the import
// returns.
func TestTaskSyncImportUnderWriteLockIsNeverPartial(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	dbPath := taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "memsync atomicity probe: the row the foreign writer locks",
		Payload: "the probe takes the ledger write lock by updating this row",
		Source:  "human",
		TaskID:  memsyncAtomicHolderID,
	}); err != nil {
		t.Fatalf("seed lock holder: %v", err)
	}
	rows := make([]SyncTaskRow, 0, memsyncProbeRows)
	for i := 0; i < memsyncProbeRows; i++ {
		rows = append(rows, memsyncProbeRow(memsyncAtomicPrefix, i))
	}
	patchPath := taskSyncWritePatch(t, SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1", Rows: rows,
	})

	release := taskSyncHoldWriteLock(t, dbPath, memsyncAtomicHolderID, memsyncProbeContendHold)
	stats, importErr := SyncTaskImport(patchPath)
	release()

	landed := taskSyncCountRows(t, memsyncAtomicPrefix)
	switch {
	case importErr != nil && landed != 0:
		t.Fatalf("PARTIAL IMPORT under lock contention: %d rows landed alongside error %v", landed, importErr)
	case importErr == nil && landed != memsyncProbeRows:
		t.Fatalf("import reported success (%+v) but %d of %d rows landed", stats, landed, memsyncProbeRows)
	}
}

// TestTaskSyncImportThenRetryIsIdempotent pins the CONTRACT the atomic failure
// rests on: a failed whole-batch import leaves nothing to clean up, so the
// operator's re-run applies the patch completely and a third run is a
// no-op (exactly-once, the payload-hash gate reports duped).
func TestTaskSyncImportThenRetryIsIdempotent(t *testing.T) {
	taskSyncTestDB(t)
	rows := make([]SyncTaskRow, 0, memsyncRetryRows)
	for i := 0; i < memsyncRetryRows; i++ {
		rows = append(rows, memsyncProbeRow(memsyncRetryPrefix, i))
	}
	patchPath := taskSyncWritePatch(t, SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1", Rows: rows,
	})
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatalf("clean import: %v", err)
	}
	if stats.RowsImported != memsyncRetryRows {
		t.Fatalf("stats = %+v, want %d imported", stats, memsyncRetryRows)
	}
	again, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	// UPDATED for 01a0fea1 ③: the second pass answers `already` for every row
	// (they are present under their own ids) instead of the payload-hash
	// gate's `duped` skip. The exactly-once contract is unchanged: nothing is
	// re-inserted.
	if again.RowsAlready != memsyncRetryRows || again.RowsImported != 0 {
		t.Fatalf("re-import = %+v, want %d already 0 imported", again, memsyncRetryRows)
	}
	if n := taskSyncCountRows(t, memsyncRetryPrefix); n != memsyncRetryRows {
		t.Fatalf("ledger holds %d retry rows, want exactly %d", n, memsyncRetryRows)
	}
}

// TestTaskSyncImportConcurrentSamePatch — two importers racing the SAME patch
// against ONE ledger (the db-sync normal case): the ledger must hold exactly
// one row per patch task and at least one importer must fully succeed. The
// UNIQUE(task_id) constraint plus the single tx are the arbiters. The loser
// either (a) queues behind the winner on the write lock and reports
// already/duped, or (b) fails BUSY whole-batch after the timeout — having
// written NOTHING. Both outcomes satisfy exactly-once; the contract is one
// committed copy and an explicit, retry-safe failure — never a partial merge.
func TestTaskSyncImportConcurrentSamePatch(t *testing.T) {
	taskSyncTestDB(t)
	rows := make([]SyncTaskRow, 0, memsyncRaceRows)
	for i := 0; i < memsyncRaceRows; i++ {
		rows = append(rows, memsyncProbeRow(memsyncRacePrefix, i))
	}
	patchPath := taskSyncWritePatch(t, SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peer1", Rows: rows,
	})

	type result struct {
		stats *SyncTaskImportStats
		err   error
	}
	resCh := make(chan result, 2)
	for w := 0; w < 2; w++ {
		go func() {
			// Each importer re-derives the env-dependent path itself;
			// GOPI_MEMORY_DB is inherited from the test.
			stats, err := SyncTaskImport(patchPath)
			resCh <- result{stats, err}
		}()
	}
	okTotal := 0
	for w := 0; w < 2; w++ {
		r := <-resCh
		if r.err != nil {
			// A whole-batch lock failure is a legal loser outcome ONLY as an
			// explicit BUSY — and the row-count assertion below proves it
			// wrote nothing.
			if msg := r.err.Error(); !strings.Contains(msg, "locked") && !strings.Contains(msg, "SQLITE_BUSY") {
				t.Fatalf("concurrent import error: %v", r.err)
			}
			continue
		}
		okTotal++
		if r.stats.RowsRefused != 0 {
			t.Fatalf("concurrent imports must not refuse rows: %+v", r.stats)
		}
	}
	if okTotal == 0 {
		t.Fatal("both importers failed — at least one must succeed")
	}
	if n := taskSyncCountRows(t, memsyncRacePrefix); n != memsyncRaceRows {
		t.Fatalf("ledger holds %d race rows, want exactly %d (no double-enqueue)", n, memsyncRaceRows)
	}
}
