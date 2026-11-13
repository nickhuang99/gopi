package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTaskReparentEdgeDoesNotTravelThroughTheDeltaTransport used to pin the
// OPPOSITE boundary: the v1 file channel carried no parent column, so an edge
// moved by `gopi task update --parent-task-id` was local to its ledger.
//
// THAT BOUNDARY IS RETIRED (2026-11-04). The transport now carries both parent
// columns, because "the edge is local" was not a design decision — it was a
// defect: a child arrived on the receiving ledger looking parentless, was
// claimable at once while its parent sat pending beside it, and the whole
// exchange reported exit=0 / refused=0 with an empty stderr. What replaces the
// old assertion is the positive one below: the edge CROSSES, and the receiving
// ledger's own claimability predicate reads it.
//
// The test keeps its original job in a new form — it fails the moment the edge
// stops travelling, which is the regression that shipped once already.
func TestTaskReparentEdgeTravelsThroughTheFileTransport(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "node-a-memory.db")
	pathB := filepath.Join(dir, "node-b-memory.db")

	// Node A: a producer and a consumer, then the edge is MOVED to a third row
	// — a move the local ledger reads back, so the sending node's state is
	// unambiguous before anything travels.
	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	reparentRow(t, "x-a", "producer a", "")
	reparentRow(t, "x-b", "producer b", "")
	reparentRow(t, "x-c", "consumer c", "x-a")
	if _, err := TaskReparent("x-c", "x-b", "the producer changed"); err != nil {
		t.Fatalf("reparent: %v", err)
	}
	if parent, _, _, _ := reparentState(t, "x-c"); parent != "x-b" {
		t.Fatalf("precondition: node A's edge must read x-b, got %q", parent)
	}

	// The export, through the real entry point.
	patch, _, err := SyncTaskExport(SyncTaskExportOptions{DBPath: pathA})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// NOTE: stats.RowsExported is NOT set by this exporter (only Scanned is),
	// so the precondition reads the PATCH — the thing that actually travels.
	if len(patch.Rows) != 3 {
		t.Fatalf("precondition: 3 rows must travel, got %d", len(patch.Rows))
	}
	// Node B: a FRESH ledger. The rows are re-sent in the ledger's own claim
	// order (producers first), because a child whose parent this ledger does
	// not hold is refused by name (Rule 33) — the channel's contract, not a
	// concession for the test.
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(patchPath, raw, 0o600); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	if _, err := SyncTaskImport(patchPath); err != nil {
		t.Fatalf("import: %v", err)
	}

	// The ROW itself did arrive, so the assertion below is about the edge and
	// not about a row that never crossed (a scan that found nothing must be
	// distinguishable from a scan that never ran).
	var n int
	db, err := openTasksDB(context.Background(), pathB)
	if err != nil {
		t.Fatalf("open node B: %v", err)
	}
	defer db.Close()
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE task_id = ?`, "x-c").Scan(&n); err != nil {
		t.Fatalf("count on node B: %v", err)
	}
	if n != 1 {
		t.Fatalf("the row must have crossed the transport, found %d copies", n)
	}
	// The EDGE crossed with it — the reparent moved x-c from x-a to x-b, and
	// the receiving ledger must read the MOVED edge, not the original one.
	parent, status, _, _ := reparentState(t, "x-c")
	if status == "" {
		t.Fatalf("precondition: the row must have arrived")
	}
	if parent != "x-b" {
		t.Errorf("the file transport lost the dependency edge: node B read parent %q, want %q — a child whose edge does not travel arrives claimable (the defect this transport carried until 2026-11-04)", parent, "x-b")
	}
}
