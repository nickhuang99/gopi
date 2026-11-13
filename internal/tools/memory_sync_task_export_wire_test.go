package tools

// memory_sync_task_export_wire_test.go — the v1 FILE channel's dependency edge.
//
// THE DEFECT THIS PINS (measured 2026-11-04, EPIC multi-parent cross-node
// verification): `gopi memory memsync --export-tasks/--import-tasks` lost the
// row's whole dependency edge, in TWO halves that each looked complete on its
// own —
//
//	[export] the SELECT named no parent column, and the scan had no destination
//	         for one, so a patch row carried NO parent key at all (not even an
//	         empty one — the json tags are omitempty);
//	[import] importTaskInsertTx was called without the parent fields, though
//	         its own INSERT already names both columns.
//
// The observable result: a child imported onto a receiving ledger with both
// parent columns EMPTY, i.e. a row that looks parentless and is claimable at
// once, while the parent sits pending on the same ledger — with exit=0,
// refused=0 and an empty stderr. No counter, no note, no log (Rule 15).
//
// THREE INSTRUMENTS, because each sees something the others cannot:
//
//	[shape] the SQL text: the export projection names both parent expressions,
//	        and it spells them from the SHARED constants the delta and harvest
//	        transports read (a second spelling is how the siblings drifted).
//	[wire]  the real transport on two real ledgers: the encoded patch carries
//	        the parent keys, and the receiving ledger reads the same two
//	        columns back.
//	[gate]  the RECEIVING ledger's OWN claimability predicate — the edge has to
//	        be visible to taskRowClaimable, because "the column has a value" and
//	        "the row is parked" are different facts and only the second is the
//	        dependency.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileChannelProjectionCarriesTheDependencyEdge is the hermetic half: it
// reads the projection CONSTANT (never a copy of it) and refuses an export that
// cannot carry the edge.
func TestFileChannelProjectionCarriesTheDependencyEdge(t *testing.T) {
	for name, expr := range map[string]string{
		"parent_task_id":  syncParentColumnExpr,
		"parent_task_ids": syncParentIDsColumnExpr,
	} {
		if !strings.Contains(syncTaskExportColumns, expr) {
			t.Fatalf("the export projection carries no %s column (%q) — a patch row would travel with no dependency edge and the child would arrive claimable (Rule 33)", name, expr)
		}
	}
	// ONE spelling, three transports (Rule 14): the delta export and the
	// peer-side projection must already name the same expressions, so the file
	// channel cannot disagree with them about a row's parent.
	if !strings.Contains(syncDeltaSelectColumns, syncParentColumnExpr) ||
		!strings.Contains(syncDeltaSelectColumns, syncParentIDsColumnExpr) {
		t.Fatalf("the delta projection moved off the shared parent expressions — the file channel above would now be the odd one out: %q", syncDeltaSelectColumns)
	}
	if !strings.Contains(syncRemoteCoalescedProjection, syncParentColumnNamed) ||
		!strings.Contains(syncRemoteCoalescedProjection, syncParentIDsColumnNamed) {
		t.Fatalf("the peer-side projection moved off the shared parent expressions: %q", syncRemoteCoalescedProjection)
	}
}

// TestFileChannelCarriesTheDependencyEdge drives the REAL transport across two
// fresh ledgers and asserts BOTH halves, plus the receiver's own gate.
func TestFileChannelCarriesTheDependencyEdge(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "node-a-memory.db")
	pathB := filepath.Join(dir, "node-b-memory.db")

	// Node A: two producers and one consumer depending on BOTH (EPIC
	// multi-parent — parent 1 rides parent_task_id, parent 2 parent_task_ids).
	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	reparentRow(t, "edge-p1", "producer one", "")
	reparentRow(t, "edge-p2", "producer two", "")
	if _, err := TaskSubmit(TaskSubmitOptions{
		TaskID:        "edge-child",
		Summary:       "consumer of two producers",
		Payload:       "briefing for the consumer",
		ParentTaskID:  "edge-p1",
		ParentTaskIDs: TaskParentIDsJoin([]string{"edge-p2"}),
		Source:        "auto",
	}); err != nil {
		t.Fatalf("submit the consumer: %v", err)
	}

	// [wire] the export, through the real entry point.
	patch, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: pathA})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(patch.Rows) != 3 {
		t.Fatalf("precondition: 3 rows must travel, got %d", len(patch.Rows))
	}
	var wire *SyncTaskRow
	for i := range patch.Rows {
		if patch.Rows[i].TaskID == "edge-child" {
			wire = &patch.Rows[i]
		}
	}
	if wire == nil {
		t.Fatalf("precondition: the consumer row must be in the patch")
	}
	if wire.ParentTaskID != "edge-p1" || wire.ParentTaskIDs != "edge-p2" {
		t.Fatalf("the exported row carries parents (%q, %q), want (%q, %q) — the patch is the thing that travels, so this is the defect",
			wire.ParentTaskID, wire.ParentTaskIDs, "edge-p1", "edge-p2")
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	// Asserted on the ENCODED bytes, so the claim is about the FORMAT and not
	// about a Go field a future refactor could stop serialising.
	for _, want := range []string{`"parent_task_id":"edge-p1"`, `"parent_task_ids":"edge-p2"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("the patch JSON carries no %s — a receiver binds the edge from this key, and its absence is the silent loss this test exists for:\n%s", want, raw)
		}
	}

	// Node B: a FRESH ledger. The rows are read straight from the receiving
	// ledger — the facts its own gate reads.
	patchPath := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(patchPath, raw, 0o600); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	// The PARENTS travel first, as they must on any real channel (a child
	// whose parent this ledger does not hold is refused by name — Rule 33).
	parentsOnly := SyncTaskPatch{Version: patch.Version, Node: patch.Node, ExportedAt: patch.ExportedAt, Generator: patch.Generator}
	for _, r := range patch.Rows {
		if r.TaskID != "edge-child" {
			parentsOnly.Rows = append(parentsOnly.Rows, r)
		}
	}
	parentsPath := filepath.Join(dir, "parents.json")
	rawParents, err := json.Marshal(parentsOnly)
	if err != nil {
		t.Fatalf("marshal parents patch: %v", err)
	}
	if err := os.WriteFile(parentsPath, rawParents, 0o600); err != nil {
		t.Fatalf("write parents patch: %v", err)
	}
	if _, err := SyncTaskImport(parentsPath); err != nil {
		t.Fatalf("import parents: %v", err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsRefused != 0 {
		t.Fatalf("the rows with a resolvable edge must not be refused: %v", stats.Errors)
	}

	parentFirst, parentRest := fileChannelParents(t, pathB, "edge-child")
	if parentFirst != "edge-p1" || parentRest != "edge-p2" {
		t.Fatalf("the receiving ledger read back parents (%q, %q), want (%q, %q) — the import call must hand the edge to importTaskInsertTx",
			parentFirst, parentRest, "edge-p1", "edge-p2")
	}

	// [gate] the RECEIVING ledger's own claimability predicate: the edge must
	// actually PARK the row. A value in a column is not a dependency; this is.
	tags, payload := fileChannelRowText(t, pathB, "edge-child")
	for _, parentID := range []string{"edge-p1", "edge-p2"} {
		status := fileChannelStatus(t, pathB, parentID)
		if taskRowClaimable("", tags, payload, []string{"edge-p1", "edge-p2"}, map[string]string{parentID: status}, nil) {
			t.Fatalf("the imported child is CLAIMABLE while %s is %s — the edge arrived but the gate cannot see it, which is the same loss in a different column", parentID, status)
		}
	}

	// Idempotent replay: the second import must neither duplicate the row nor
	// claim it adopted anything (the edge is already there).
	replay, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatalf("replay import: %v", err)
	}
	if replay.RowsParented != 0 {
		t.Fatalf("the replay adopted %d edge(s) onto rows that already carry them — the adopt arm must be guarded on an EMPTY local edge", replay.RowsParented)
	}
}

// TestFileChannelAdoptsTheEdgeOntoAnExistingRow is the REPAIR half: a row that
// reached the receiver while the channel carried no parent column must gain its
// edge when the row is re-sent. Without this arm the fix would only help rows
// the receiver had never seen — precisely not the ones the defect corrupted.
func TestFileChannelAdoptsTheEdgeOntoAnExistingRow(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "node-a-memory.db")
	pathB := filepath.Join(dir, "node-b-memory.db")

	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	reparentRow(t, "adopt-p", "producer", "")
	if _, err := TaskSubmit(TaskSubmitOptions{
		TaskID:       "adopt-child",
		Summary:      "consumer",
		Payload:      "briefing for the consumer",
		ParentTaskID: "adopt-p",
		Source:       "auto",
	}); err != nil {
		t.Fatalf("submit the consumer: %v", err)
	}
	patch, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: pathA})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// The receiver already holds BOTH rows, unparented — the shape every
	// pre-fix transfer left behind.
	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	reparentRow(t, "adopt-p", "producer", "")
	reparentRow(t, "adopt-child", "consumer", "")
	if first, _ := fileChannelParents(t, pathB, "adopt-child"); first != "" {
		t.Fatalf("precondition: the local row must start with NO edge, got %q", first)
	}

	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(patchPath, raw, 0o600); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsParented != 1 {
		t.Fatalf("the re-send adopted %d edge(s), want 1 — the repair arm is what makes a re-send able to heal the rows this defect left unparented (notes: %v)",
			stats.RowsParented, stats.Errors)
	}
	if first, _ := fileChannelParents(t, pathB, "adopt-child"); first != "adopt-p" {
		t.Fatalf("the adopted edge reads %q, want %q", first, "adopt-p")
	}
	// The adoption is announced, not silent (Rule 15): an operator reading the
	// import report must be able to tell a repaired ledger from a quiet one.
	var noted bool
	for _, e := range stats.Errors {
		if strings.Contains(e, "adopt-child") && strings.Contains(e, "adopted") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("the adoption carries no note — a repair nobody can see is a silent one: %v", stats.Errors)
	}
}

// TestFileChannelRefusesAnUnhonourableEdge pins the refusal: a row whose parent
// this ledger does not hold must NOT be written with its edge dropped. Rule 33
// parks such a row forever, so the two honest outcomes are "the edge lands" and
// "the row is refused by name" — never a parentless row that reads as ready.
func TestFileChannelRefusesAnUnhonourableEdge(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "node-a-memory.db")
	pathB := filepath.Join(dir, "node-b-memory.db")

	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	reparentRow(t, "ghost-p", "producer that never travels", "")
	if _, err := TaskSubmit(TaskSubmitOptions{
		TaskID:       "ghost-child",
		Summary:      "consumer of an absent parent",
		Payload:      "briefing for the consumer",
		ParentTaskID: "ghost-p",
		Source:       "auto",
	}); err != nil {
		t.Fatalf("submit the consumer: %v", err)
	}
	patch, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: pathA})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// Only the CHILD travels: its parent exists on the sender and not here.
	only := SyncTaskPatch{Version: patch.Version, Node: patch.Node, ExportedAt: patch.ExportedAt, Generator: patch.Generator}
	for _, r := range patch.Rows {
		if r.TaskID == "ghost-child" {
			only.Rows = append(only.Rows, r)
		}
	}
	raw, err := json.Marshal(only)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(patchPath, raw, 0o600); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsRefused != 1 {
		t.Fatalf("a row whose parent this ledger cannot honour must be REFUSED by name, got refused=%d imported=%d (notes: %v)",
			stats.RowsRefused, stats.RowsImported, stats.Errors)
	}
	var refused bool
	for _, e := range stats.Errors {
		if strings.Contains(e, "ghost-p") {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("the refusal must NAME the parent it cannot resolve (Rule 15): %v", stats.Errors)
	}
	// A refusal writes nothing — a half-written row is the failure mode the
	// sentinel classification exists to prevent. "Nothing" is asserted as the
	// ABSENCE of the row (not as an empty parent column): a row that is not
	// there cannot become claimable later either.
	if n := fileChannelRowCount(t, pathB, "ghost-child"); n != 0 {
		t.Fatalf("a refused row must not be written at all, found %d copy/copies on the receiving ledger", n)
	}
}

// fileChannelParents reads the two parent columns straight from a ledger.
func fileChannelParents(t *testing.T, path, taskID string) (first, rest string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(parent_task_id,''), COALESCE(`+taskParentIDsColumnName+`,'') FROM tasks WHERE task_id = ?`,
		taskID).Scan(&first, &rest); err != nil {
		t.Fatalf("read parents of %s: %v", taskID, err)
	}
	return first, rest
}

// fileChannelRowText reads the two columns the claimability predicate reads.
func fileChannelRowText(t *testing.T, path, taskID string) (tags, payload string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(tags,''), COALESCE(payload,'') FROM tasks WHERE task_id = ?`,
		taskID).Scan(&tags, &payload); err != nil {
		t.Fatalf("read row %s: %v", taskID, err)
	}
	return tags, payload
}

// fileChannelRowCount reports how many copies of a task_id a ledger holds.
func fileChannelRowCount(t *testing.T, path, taskID string) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE task_id = ?`, taskID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", taskID, err)
	}
	return n
}

// fileChannelStatus reads one row's status from a ledger.
func fileChannelStatus(t *testing.T, path, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM tasks WHERE task_id = ?`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status of %s: %v", taskID, err)
	}
	return status
}

// TestFileChannelWidensTheParentSetOnAnExistingRow pins the arm the 2026-11-04
// fix left half-done: the file channel adopted a peer's edge only when the
// local row carried NONE, so a child that already had P1 and whose peer copy
// carried {P1, P2} kept P1 alone — it released the moment P1 finished and
// stayed CLAIMABLE while P2 was live, silently (no error, no counter).
//
// The delta channel had the same hole and closed it in de9b7f2d with
// syncDeltaParentSetRelation/syncParentSetUnion. This test asserts the FILE
// channel uses THAT classifier rather than a second one with a different idea
// of "new edge" (Rule 14) — which is why it drives the real transport across
// two ledgers instead of unit-testing a helper.
func TestFileChannelWidensTheParentSetOnAnExistingRow(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "wide-a.db")
	pathB := filepath.Join(dir, "wide-b.db")

	// Node A: two producers and a child depending on the FIRST only.
	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	reparentRow(t, "wide-p1", "producer one", "")
	reparentRow(t, "wide-p2", "producer two", "")
	reparentRow(t, "wide-child", "child under test", "wide-p1")

	patch1, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: pathA})
	if err != nil {
		t.Fatalf("first export: %v", err)
	}

	// Node B receives it, parents first (Rule 33: a child whose parent this
	// ledger cannot resolve is refused by name).
	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	parentsOnly := SyncTaskPatch{Version: patch1.Version, Node: patch1.Node, ExportedAt: patch1.ExportedAt, Generator: patch1.Generator}
	for _, r := range patch1.Rows {
		if r.TaskID != "wide-child" {
			parentsOnly.Rows = append(parentsOnly.Rows, r)
		}
	}
	if _, err := importPatchObject(t, dir, "wide-parents.json", parentsOnly); err != nil {
		t.Fatalf("import parents: %v", err)
	}
	firstStats, err := importPatchObject(t, dir, "wide-1.json", *patch1)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if firstStats.RowsRefused != 0 {
		t.Fatalf("the first import must land cleanly: %v", firstStats.Errors)
	}
	if p1, _ := fileChannelParents(t, pathB, "wide-child"); p1 != "wide-p1" {
		t.Fatalf("precondition: node B reads the child's first parent as %q, want \"wide-p1\"", p1)
	}

	// Node A: the child gains a SECOND parent (the set GROWS at the origin).
	t.Setenv("GOPI_MEMORY_DB", pathA)
	// The REAL reparent entry point, not a hand-rolled UPDATE: the set the
	// export then carries is whatever that write left on the row.
	if _, err := TaskReparentParents("wide-child", []string{"wide-p1", "wide-p2"}, "the child gains a second dependency"); err != nil {
		t.Fatalf("widen the child's parent set on A: %v", err)
	}
	patch2, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: pathA})
	if err != nil {
		t.Fatalf("re-export: %v", err)
	}

	// Node B: the SAME row arrives again, now with a wider set.
	t.Setenv("GOPI_MEMORY_DB", pathB)
	stats, err := importPatchObject(t, dir, "wide-2.json", *patch2)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if stats.RowsParentsWidened != 1 {
		t.Fatalf("the wider remote set was not landed: widened=%d conflict=%d refused=%d — the child keeps one parent and becomes claimable while the other is live (notes: %v)",
			stats.RowsParentsWidened, stats.RowsParentsConflict, stats.RowsRefused, stats.Errors)
	}
	p1, p2 := fileChannelParents(t, pathB, "wide-child")
	if p1 != "wide-p1" || p2 != "wide-p2" {
		t.Fatalf("node B reads parents (%q, %q), want (\"wide-p1\", \"wide-p2\") — the local order is KEPT and the new edge APPENDED, so an older reader's single-parent column keeps meaning what it meant", p1, p2)
	}

	// The point of the fix: BOTH parents park the row, read through the
	// receiving ledger's own claimability predicate.
	tags, payload := fileChannelRowText(t, pathB, "wide-child")
	for _, parentID := range []string{"wide-p1", "wide-p2"} {
		status := fileChannelStatus(t, pathB, parentID)
		if taskRowClaimable("", tags, payload, []string{"wide-p1", "wide-p2"}, map[string]string{parentID: status}, nil) {
			t.Fatalf("the child is CLAIMABLE while %s is %s — the widened edge is on the row but the gate cannot see it", parentID, status)
		}
	}

	// A STALE peer (one still carrying {P1} only) must never TAKE the edge away.
	stale := SyncTaskPatch{Version: patch1.Version, Node: patch1.Node, ExportedAt: patch1.ExportedAt, Generator: patch1.Generator}
	for _, r := range patch1.Rows {
		if r.TaskID == "wide-child" {
			stale.Rows = append(stale.Rows, r)
		}
	}
	staleStats, err := importPatchObject(t, dir, "wide-3.json", stale)
	if err != nil {
		t.Fatalf("stale import: %v", err)
	}
	if staleStats.RowsParentsWidened != 0 {
		t.Fatalf("a subset remote set must write NOTHING, widened=%d", staleStats.RowsParentsWidened)
	}
	if p1, p2 := fileChannelParents(t, pathB, "wide-child"); p1 != "wide-p1" || p2 != "wide-p2" {
		t.Fatalf("a stale peer TAUGHT the edge away: (%q, %q) — a subset must never shrink the local set", p1, p2)
	}
}

// importPatchObject writes a patch to disk and imports it through the real
// entry point (the wire is the subject, not a Go call).
func importPatchObject(t *testing.T, dir, name string, patch SyncTaskPatch) (*SyncTaskImportStats, error) {
	t.Helper()
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return SyncTaskImport(path)
}
