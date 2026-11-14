package tools

// Provenance of a MIRRORED TERMINAL row (epic: the write path's last bare
// provenance spelling).
//
// importTaskMirrorTx — the sync layer's mirror-history insert, the writer for a
// remote row that arrived TERMINAL and that this ledger has never seen — used
// to hardcode the literal `'sync'` as the row's `source`. `'sync'` is not a
// value the ledger's own vocabulary accepts: tasksource.Known("sync") = false
// and tasksource.Normalize("sync") = ("", false). So the mirror INSERT wrote a
// provenance its own validator (`validTaskSource`, which is tasksource.Known)
// refuses, and `task show` narrated the row through `taskSubmittedBy`'s orDash
// fallback, dumping the raw spelling.
//
// THE DECISION (recorded in internal/tasksource's package doc and on the
// constant): the sync layer writes provenance at two shapes and they are two
// values. A FRESH replicated row arrives as PENDING work and carries
// tasksource.Server (the row IS this node's enqueue). A MIRRORED terminal row
// is never enqueued here at all — it is copied history, the record of a run
// that happened on another node — so it carries tasksource.MirrorTerminal.
// Rendering it `server` would narrate an enqueue this node never made.
//
// RED before this change: the stored source is "sync"; Known("sync") is false;
// `task show` renders neither "mirrored terminal" nor the value — it renders
// the raw spelling off the orDash fallback. GREEN after: the stored source is
// the constant, it is accepted vocabulary, and the renderer narrates it.
//
// The test drives the REAL import path (SyncTaskImport, the function
// cmd/agent/cli_memsync.go's `memsync --import-tasks` calls) against a real
// mirror DB, and reads the row back from that DB.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/tasksource"
)

// TestMirrorTerminalRowCarriesLedgerVocabulary pins the stored provenance of a
// terminal row that arrives through the mirror insert, plus the two invariants
// the defect broke: the value is ACCEPTED vocabulary, and `task show` NARRATES
// it rather than falling through orDash.
func TestMirrorTerminalRowCarriesLedgerVocabulary(t *testing.T) {
	taskSyncTestDB(t)

	// A terminal row this ledger has never seen — the mirror-insert branch
	// (no local counterpart, terminal status).
	done := time.Now().Unix()
	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peerbox", ExportedAt: done,
		Rows: []SyncTaskRow{{
			TaskID: "t-mirror-9001", Summary: "work finished on the peer",
			Payload: "the peer's briefing", Tags: []string{"bug"},
			Source: tasksource.Agent, Status: taskStatusCompleted,
			CreatedAt: done - 60, UpdatedAt: done, EndedAt: done,
		}},
	}
	patchPath := filepath.Join(t.TempDir(), "mirror-patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatalf("SyncTaskImport: %v", err)
	}
	if stats.RowsImported != 1 {
		t.Fatalf("stats = %+v, want the terminal row imported through the mirror insert", stats)
	}

	row := taskRowForTest(t, "t-mirror-9001")
	if row.Status != taskStatusCompleted {
		t.Errorf("mirrored row status = %q, want %q (the mirror carries the terminal verdict verbatim)", row.Status, taskStatusCompleted)
	}

	// ① the STORED value is accepted ledger vocabulary — the invariant the
	// raw literal broke (the writer emitted a spelling its own validator
	// refuses).
	value, known := tasksource.Normalize(row.Source)
	if !known {
		t.Fatalf("mirrored terminal row source = %q — not ledger vocabulary (want %s): the writer stamped a spelling the ledger refuses",
			row.Source, tasksource.Policy())
	}
	if row.Source != value {
		t.Errorf("mirrored terminal row stores the non-canonical spelling %q (normalizes to %q) — a row written today never carries an alias", row.Source, value)
	}

	// ② the value answers THIS shape's question. `server` here would narrate an
	// enqueue this node never made; `forward` is the opposite direction (a row
	// THIS node filed to a peer).
	if value != tasksource.MirrorTerminal {
		t.Errorf("mirrored terminal row source = %q (normalized %q), want %q — a copied terminal row is neither this node's enqueue (%s) nor a forward (%s)",
			row.Source, value, tasksource.MirrorTerminal, tasksource.Server, tasksource.Forward)
	}
	if value == tasksource.Server {
		t.Errorf("the mirrored terminal row reads as %q — this node never enqueued or ran it", tasksource.Server)
	}

	// ③ `task show` NARRATES the value (Rule 31: the operator-facing surface is
	// run, not inferred). Before the fix this line was the orDash fallback with
	// the raw "sync" spelling in it.
	show, err := TaskShow("t-mirror-9001")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "mirrored terminal") || !strings.Contains(show, tasksource.MirrorTerminal) {
		t.Errorf("submitted_by must narrate %q, got:\n%s", tasksource.MirrorTerminal, show)
	}
	// The provenance LINES themselves (not the whole page: other columns —
	// `updated_at`'s "state/sync anchor" note, a run directory path — quote
	// the word legitimately).
	if line := showProvField(show, "submitted_by:"); strings.Contains(line, "sync") || strings.Contains(line, "—") {
		t.Errorf("submitted_by still carries the raw literal or fell through to the orDash placeholder: %q", line)
	}
	if line := showProvField(show, "source:"); strings.Contains(line, "sync") {
		t.Errorf("the source line still carries the raw literal: %q", line)
	}
	if strings.Contains(show, "resident node machinery") {
		t.Errorf("a copied terminal row is narrated as this node's own machinery:\n%s", show)
	}
}

// TestFreshReplicatedRowStaysServer is the OTHER half of the decision: the
// mirror insert's sibling (a PENDING row this node will actually run) keeps
// tasksource.Server. Pinning both shapes in one place is what stops a future
// edit from collapsing the decision — sharing one value across the two would
// make a copied terminal row read as an enqueue.
func TestFreshReplicatedRowStaysServer(t *testing.T) {
	taskSyncTestDB(t)

	src := SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peerbox", ExportedAt: time.Now().Unix(),
		Rows: []SyncTaskRow{{
			TaskID: "t-fresh-9002", Summary: "work to run here",
			Payload: "a live briefing", Tags: []string{"bug"},
			Status: taskStatusPending,
		}},
	}
	patchPath := filepath.Join(t.TempDir(), "fresh-patch.json")
	if err := WriteSyncTaskPatch(&src, patchPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncTaskImport(patchPath); err != nil {
		t.Fatalf("SyncTaskImport: %v", err)
	}
	if got := taskRowForTest(t, "t-fresh-9002").Source; got != tasksource.Server {
		t.Errorf("fresh replicated row source = %q, want %q (a pending row IS this node's own enqueue)", got, tasksource.Server)
	}
}

// taskShowField extracts one rendered `task show` field line (matched by its
// label prefix), so a provenance assertion is about the LINE and not about
// every other column on the page.
func showProvField(show, label string) string {
	for _, line := range strings.Split(show, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), label) {
			return line
		}
	}
	return ""
}
