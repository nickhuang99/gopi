package tools

// db-sync deliverable round trip (Epic 2026-08-31 ④ acceptance): the
// deliverable blob (repo/branch/worktree) must travel the memsync
// transports — verbatim insert, forward migration, harvest pull — so the
// server's adoption gate (① discovery) can pair a pushed-back branch with
// its completed ledger task WITHOUT the client and server sharing a DB.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// deliverableE2ESimulateRun stamps a task completed with a deliverable
// (the executor-backfill shape), as a finishing run would.
func deliverableE2ESimulateRun(t *testing.T, id, repo, branch string) {
	t.Helper()
	if _, err := TaskUpdate(id, "in_progress"); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if _, err := TaskUpdate(id, "completed"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`UPDATE tasks SET deliverable=? WHERE task_id=?`,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+repo+`"}`, id)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
}

// deliverableE2EAdoptable asserts the adoption gate accepts exactly the
// branch the synced deliverable names.
func deliverableE2EAdoptable(t *testing.T, branch string) bool {
	t.Helper()
	ok, err := adoptionTaskCompleted(context.Background(), branch)
	if err != nil {
		t.Fatalf("adopt gate: %v", err)
	}
	return ok
}

// TestSyncTaskDeliverableRoundTrip: ④ export → import → re-export —
// the blob enters the row at completion, survives a delta import into a
// second node's ledger, and re-exports (no re-transmit loop: the second
// export is identical content-wise).
func TestSyncTaskDeliverableRoundTrip(t *testing.T) {
	serverDB := setMemoryDB(t)
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "branch work", Payload: "executed on client", Tags: "e2e"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	id := adoptionTaskID(t, out)
	deliverableE2ESimulateRun(t, id, "/tmp/client-repo", "feat/roundtrip")

	// ① export the delta (server side would PUSH; here we capture).
	patch1, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var row *SyncTaskRow
	for i := range patch1.Rows {
		if patch1.Rows[i].TaskID == id {
			row = &patch1.Rows[i]
		}
	}
	if row == nil || row.Deliverable == "" {
		t.Fatalf("export lost the deliverable blob: %+v", row)
	}
	if !strings.Contains(row.Deliverable, `"branch":"feat/roundtrip"`) {
		t.Fatalf("blob malformed: %s", row.Deliverable)
	}
	if row.DeliverableVersion != deliverableVersionKnown {
		t.Fatalf("version %d != known %d", row.DeliverableVersion, deliverableVersionKnown)
	}

	// ② client-side ledger: apply the delta verbatim (new row there).
	clientDB := filepath.Join(t.TempDir(), "client.db")
	t.Setenv(memoryDBEnv, clientDB)
	t.Setenv("GOPI_SYNC_NODE", "client")
	patch := &SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer1", Rows: patch1.Rows}
	if _, err := SyncTaskImportDelta(patch); err != nil {
		t.Fatalf("import: %v", err)
	}
	// The imported row carries the blob → the client's OWN adopt gate
	// (a second server sim) would accept the branch.
	if !deliverableE2EAdoptable(t, "feat/roundtrip") {
		t.Fatalf("imported row's deliverable not visible to the adopt gate")
	}

	// ③ re-export from the client: same row, blob still present (forward
	// flow is stable — no loss on the return hop).
	patch2, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("re-export: %v", err)
	}
	found := false
	for _, r := range patch2.Rows {
		if r.TaskID == id && strings.Contains(r.Deliverable, `"branch":"feat/roundtrip"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("re-export lost the blob: %+v", patch2.Rows)
	}
	_ = serverDB
}

// TestSyncDeltaDeliverableForwardMigration: a row ALREADY on the server
// (imported pre-completion as pending) gains the blob through the
// forward-migration UPDATE when the completed status arrives.
func TestSyncDeltaDeliverableForwardMigration(t *testing.T) {
	serverDB := setMemoryDB(t)
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "pending work", Payload: "in flight", Tags: "e2e"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	id := adoptionTaskID(t, out)

	// Client side: the same row completed there with a branch stamped.
	clientDB := filepath.Join(t.TempDir(), "client.db")
	t.Setenv(memoryDBEnv, clientDB)
	// (The pending row was created on the server ledger; for the client
	// simulation just build the delta row directly.)
	completed := SyncTaskRow{
		TaskID: id, Summary: "pending work", Payload: "in flight",
		Tags: []string{"e2e"}, Source: "auto", Status: "completed",
		UpdatedAt: time.Now().Unix() + 10, EndedAt: time.Now().Unix() + 10,
		Deliverable:        `{"repo":"/tmp/client-repo","branch":"feat/migrate"}`,
		DeliverableVersion: deliverableVersionKnown,
	}

	// Back on the server ledger: apply the completed delta → forward
	// migration must backfill the deliverable next to the status.
	t.Setenv(memoryDBEnv, serverDB)
	t.Setenv("GOPI_SYNC_NODE", "server")
	patch := &SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer1", Rows: []SyncTaskRow{completed}}
	if _, err := SyncTaskImportDelta(patch); err != nil {
		t.Fatalf("import delta: %v", err)
	}
	if !deliverableE2EAdoptable(t, "feat/migrate") {
		t.Fatalf("forward migration did not backfill the deliverable")
	}
}

// TestSyncDeltaDeliverableUnknownVersionDropped: a blob from the FUTURE
// (deliverable_version > deliverableVersionKnown) is dropped, not
// misparsed — the row keeps syncing, the gate just stays blind to it.
func TestSyncDeltaDeliverableUnknownVersionDropped(t *testing.T) {
	setMemoryDB(t)
	t.Setenv("GOPI_SYNC_NODE", "server")
	rows := []SyncTaskRow{{
		TaskID: "t-future", Summary: "future blob", Payload: "p",
		Tags: []string{"e2e"}, Source: "auto", Status: "completed",
		UpdatedAt:          time.Now().Unix() + 10,
		Deliverable:        `{"repo":"x","branch":"feat/future","newfield":1}`,
		DeliverableVersion: deliverableVersionKnown + 1,
	}}
	if _, err := SyncTaskImportDelta(&SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer1", Rows: rows}); err != nil {
		t.Fatalf("import: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var deliverable string
	if err := db.QueryRow(`SELECT COALESCE(deliverable,'') FROM tasks WHERE task_id='t-future'`).Scan(&deliverable); err != nil {
		t.Fatalf("query: %v", err)
	}
	if deliverable != "" {
		t.Fatalf("future-version blob must be dropped, got %q", deliverable)
	}
}

// TestSyncDeltaDeliverableDoesNotOverwrite: first valid truth wins — a
// second, different blob must NOT rewrite an existing deliverable.
func TestSyncDeltaDeliverableDoesNotOverwrite(t *testing.T) {
	setMemoryDB(t)
	first := SyncTaskRow{
		TaskID: "t-first", Summary: "s", Payload: "p", Tags: []string{"e2e"},
		Status: "completed", UpdatedAt: time.Now().Unix() + 10,
		Deliverable:        `{"repo":"/a","branch":"feat/first"}`,
		DeliverableVersion: deliverableVersionKnown,
	}
	if _, err := SyncTaskImportDelta(&SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer1", Rows: []SyncTaskRow{first}}); err != nil {
		t.Fatalf("import 1: %v", err)
	}
	second := first
	second.Deliverable = `{"repo":"/b","branch":"feat/second"}`
	second.UpdatedAt = time.Now().Unix() + 20
	if _, err := SyncTaskImportDelta(&SyncTaskPatch{Version: syncPatchVersionDelta, Node: "peer1", Rows: []SyncTaskRow{second}}); err != nil {
		t.Fatalf("import 2: %v", err)
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var deliverable string
	if err := db.QueryRow(`SELECT COALESCE(deliverable,'') FROM tasks WHERE task_id='t-first'`).Scan(&deliverable); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !strings.Contains(deliverable, "feat/first") {
		t.Fatalf("first truth must win, got %q", deliverable)
	}
}

// TestHarvestCarriesDeliverable: the v1 transport (harvestRemoteRow)
// backfills the blob on UPDATE — the server learns the branch when the
// completed status lands.
func TestHarvestCarriesDeliverable(t *testing.T) {
	setMemoryDB(t)
	// Seed the row pre-completion (pending) locally.
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "hp work", Payload: "p", Tags: "e2e"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	id := adoptionTaskID(t, out)

	remote := harvestRemoteRow{
		TaskID: id, Summary: "hp work", Payload: "p", Tags: "e2e",
		Source: "auto", Status: "completed",
		CreatedAt: float64(time.Now().Unix() - 5), UpdatedAt: float64(time.Now().Unix() + 10), EndedAt: float64(time.Now().Unix() + 10),
		Deliverable: `{"repo":"/tmp/hp","branch":"feat/hp-branch","worktree":"/tmp/hp-wt"}`,
	}
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("tx: %v", err)
	}
	if _, err := harvestApplyTx(context.Background(), tx, &taskNotifyBus{}, []harvestRemoteRow{remote}, "hp", "from-hp"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !deliverableE2EAdoptable(t, "feat/hp-branch") {
		t.Fatalf("harvest UPDATE did not backfill the deliverable")
	}
}

// TestAdoptionGateBlindWithoutSync: negative control — the gate must NOT
// accept a branch whose completed task exists ONLY on the client (no
// synced blob): the pairing is what makes the chain safe.
func TestAdoptionGateBlindWithoutSync(t *testing.T) {
	setMemoryDB(t)
	ok, err := adoptionTaskCompleted(context.Background(), "feat/never-synced")
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if ok {
		t.Fatalf("gate accepted a branch with no completed+deliverable row")
	}
}
