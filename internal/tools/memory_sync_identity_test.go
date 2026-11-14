package tools

// Epic 01a0fea1 item ③ — the sync layer's identity narrowing.
//
// Before: "the same work" was judged on (summary, payload). A row under an
// existing task_id whose payload had been re-worded — the SAME task, the
// SAME id, only the briefing's free-text restatement edited — was read as a
// DIFFERENT task and answered with a relabel (harvest) or a refusal
// (terminal sync-back), or swallowed by the content ladder (import).
//
// After: identity is the task_id. The SUMMARY still separates two genuinely
// different submissions that collided on one id, and the CONTENT ladder
// (exact → canonical → similar) still runs — as a WARNING: counted and
// reported, never a skip.
//
// These tests are the minimal repro of that change: each one was RED against
// master (payload edited ⇒ relabel / refused / row dropped) and is GREEN
// against the new identity rule.

import (
	"strings"
	"testing"
)

// identityImportPatch writes one patch row and imports it.
func identityImportPatch(t *testing.T, row SyncTaskRow) (*SyncTaskImportStats, error) {
	t.Helper()
	patchPath := taskSyncWritePatch(t, SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peerbox", Rows: []SyncTaskRow{row},
	})
	return SyncTaskImport(patchPath)
}

// TestImportSameIDEditedPayloadIsTheSameWork — the core case: an existing
// pending row and an incoming row share a task_id and a summary; only the
// payload differs. The incoming row is the SAME work: it must be absorbed
// (already / tags union) — never relabeled and never refused.
//
// RED on master: syncTaskWorkHash(summary, payload) differed, so the
// importTaskExisting collision gate relabeled the row to t-…-npeerbox and
// queued a second copy (RowsRelabeled=1, RowsAlready=0).
func TestTaskSyncImportSameIDEditedPayloadIsTheSameWork(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "shared task", Payload: "briefing v1", Tags: "local-tag",
		Source: "human", TaskID: "t-id-1",
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := identityImportPatch(t, SyncTaskRow{
		TaskID: "t-id-1", Summary: "shared task", Payload: "briefing v2 (re-worded)",
		Tags: []string{"local-tag"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsRelabeled != 0 {
		t.Fatalf("stats = %+v — an edited payload under one task_id must NOT fork a relabeled copy", stats)
	}
	if stats.RowsRefused != 0 {
		t.Fatalf("stats = %+v — an edited payload must NOT be refused", stats)
	}
	// The row is absorbed: already, or a tags union when the channel's
	// from-<node> tag is new (the acceptance's "already/tags-union").
	// The content ladder does NOT fire here: its candidates exclude the row's
	// OWN task_id (identity is the id, so a row is never its own twin), and
	// no OTHER live row carries this content.
	if stats.RowsContentWarn != 0 {
		t.Fatalf("stats = %+v, want no content warning (the only twin was the row's own id)", stats)
	}
	// The local row keeps its own payload (mechanic ⑤, no overwrite) and no
	// relabeled twin exists.
	if _, _, tags := taskSyncStatus(t, "t-id-1"); !strings.Contains(tags, "local-tag") {
		t.Fatalf("local row tags = %q, want the union path untouched", tags)
	}
	if row, err := syncTaskByID("t-id-1-npeerbox"); err != nil || row != nil {
		t.Fatalf("relabeled twin exists (row=%v, err=%v) — the payload edit forked the work", row, err)
	}
}

// TestImportSameIDEditedPayloadTagsStillUnion — the same case with the tags
// ALSO changed: the union path (tagged) must fire, not the relabel path.
func TestTaskSyncImportSameIDEditedPayloadTagsStillUnion(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "shared task", Payload: "briefing v1",
		Source: "human", TaskID: "t-id-2",
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := identityImportPatch(t, SyncTaskRow{
		TaskID: "t-id-2", Summary: "shared task", Payload: "briefing v2",
		Tags: []string{"review"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsTagged != 1 || stats.RowsRelabeled != 0 || stats.RowsRefused != 0 {
		t.Fatalf("stats = %+v, want tagged=1 relabeled=0 refused=0", stats)
	}
	if _, _, tags := taskSyncStatus(t, "t-id-2"); !strings.Contains(tags, "review") {
		t.Fatalf("tags = %q, want the union with the incoming tag", tags)
	}
}

// TestImportTerminalSyncBackEditedPayloadMigrates — a terminal sync-back
// under a pending local row whose PAYLOAD was re-worded is the same work
// finishing: the row migrates to completed.
//
// RED on master: the work hashes differed, so the import refused loudly
// ("terminal sync-back ... carries DIFFERENT work ... refusing to fabricate
// a completion") and the local row stayed pending forever.
func TestTaskSyncImportTerminalSyncBackEditedPayloadMigrates(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "finished elsewhere", Payload: "briefing v1",
		Source: "human", TaskID: "t-id-3",
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := identityImportPatch(t, SyncTaskRow{
		TaskID: "t-id-3", Summary: "finished elsewhere", Payload: "briefing v2",
		Status: taskStatusCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsMigrated != 1 || stats.RowsRefused != 0 {
		t.Fatalf("stats = %+v, want migrated=1 refused=0 (same work, re-worded payload)", stats)
	}
	if status, _, _ := taskSyncStatus(t, "t-id-3"); status != taskStatusCompleted {
		t.Fatalf("status = %q, want completed", status)
	}
}

// TestImportTerminalSyncBackDivergentSummaryStillRefused — the guard that
// SURVIVES the narrowing: a terminal sync-back whose SUMMARY differs is a
// genuinely different task under one id, and migrating it would fabricate a
// completion. Still refused.
func TestTaskSyncImportTerminalSyncBackDivergentSummaryStillRefused(t *testing.T) {
	taskSyncTestDB(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "local work", Payload: "briefing v1",
		Source: "human", TaskID: "t-id-4",
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := identityImportPatch(t, SyncTaskRow{
		TaskID: "t-id-4", Summary: "an entirely different task", Payload: "briefing v1",
		Status: taskStatusCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsRefused != 1 || stats.RowsMigrated != 0 {
		t.Fatalf("stats = %+v, want refused=1 migrated=0 (divergent summary = different task)", stats)
	}
	if status, _, _ := taskSyncStatus(t, "t-id-4"); status != taskStatusPending {
		t.Fatalf("status = %q, want pending (no fabricated completion)", status)
	}
}

// TestImportContentLadderWarnsButNeverSkips — content dedup degraded to a
// warning: two rows carrying the SAME summary+payload under DIFFERENT ids
// both land, and the twin is reported.
//
// RED on master: the second row reported duped and was SKIPPED — one of the
// two submitted tasks silently never reached the ledger.
func TestTaskSyncImportContentLadderWarnsButNeverSkips(t *testing.T) {
	taskSyncTestDB(t)
	patchPath := taskSyncWritePatch(t, SyncTaskPatch{
		Version: syncTaskPatchVersion, Node: "peerbox",
		Rows: []SyncTaskRow{
			{TaskID: "t-warn-1", Summary: "repeated brief", Payload: "identical body"},
			{TaskID: "t-warn-2", Summary: "repeated brief", Payload: "identical body"},
		},
	})
	stats, err := SyncTaskImport(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsImported != 2 {
		t.Fatalf("stats = %+v, want 2 imported (identity is the task_id — the row is never dropped)", stats)
	}
	if stats.RowsContentWarn != 1 || stats.RowsDuped != 1 {
		t.Fatalf("stats = %+v, want the content twin WARNED exactly once", stats)
	}
	// Rule 15: the warning is visible, not silent.
	warned := false
	for _, e := range stats.Errors {
		if strings.Contains(e, "t-warn-2") && strings.Contains(e, "task_id") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("content twin not reported in Errors: %v", stats.Errors)
	}
	for _, id := range []string{"t-warn-1", "t-warn-2"} {
		if status, _, _ := taskSyncStatus(t, id); status != taskStatusPending {
			t.Fatalf("%s status = %q, want pending (both rows land)", id, status)
		}
	}
}

// TestHarvestSameIDEditedPayloadIsDuped — the harvest half of the same case:
// local pending row and remote row share id + summary, same stamp, and only
// the payload differs. Same work ⇒ absorbed (duped).
//
// RED on master: harvestSameContent compared the payload, so the relabel
// branch fired — Relabeled=1, a duplicate row queued under t-h-1-nhp.
func TestHarvestSameIDEditedPayloadIsDuped(t *testing.T) {
	harvestTestDB(t)
	const id = "t-h-1"
	harvestSeedRow(t, id, "shared work", taskStatusPending, 1788000100, "")
	remote := harvestRemoteRow{
		TaskID: id, Summary: "shared work", Payload: "a re-worded briefing",
		CreatedAt: 1788000090, UpdatedAt: 1788000100,
	}
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{remote}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Duped != 1 || counts.Relabeled != 0 || counts.Conflict != 0 || counts.Skipped != 0 {
		t.Fatalf("counts = %+v, want duped=1 and no relabel/conflict/skip", counts)
	}
	// No relabeled twin exists under any name (the payload edit must not
	// fork the work into a second row).
	if n := taskSyncCountRows(t, id+"-n"); n != 0 {
		t.Fatalf("%d relabeled twin(s) exist — an edited payload forked the work", n)
	}
}

// TestHarvestSameIDDivergentSummaryRelabels — the collision guard that
// SURVIVES: same id, DIFFERENT summary in the live/pending lane still
// relabels (HP's new bug report is never absorbed as "already"). The payload
// axis plays no part.
func TestHarvestSameIDDivergentSummaryRelabels(t *testing.T) {
	harvestTestDB(t)
	t.Setenv("GOPI_SYNC_NODE", "peerbox")
	const id = "t-h-2"
	harvestSeedRow(t, id, "peerbox version", taskStatusPending, 1788000300, "")
	remote := harvestRemoteRow{
		TaskID: id, Summary: "hp version", Payload: "seed payload peerbox version",
		CreatedAt: 1788000290, UpdatedAt: 1788000300,
	}
	counts, err := TaskHarvest(HarvestOptions{Peer: harvestFixturePeer, Since: 0, Fetch: harvestRows([]harvestRemoteRow{remote}, nil)})
	if err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if counts.Relabeled != 1 {
		t.Fatalf("counts = %+v, want relabel=1 (a different SUMMARY is different work)", counts)
	}
}
