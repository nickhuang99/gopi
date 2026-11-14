package tools

// memory_sync_delta_test.go — M4.13 timestamp-delta protocol tests
// (devdoc/memsync-timestamp-delta.md). Coverage mirrors the epic's four
// 实测 items plus the merge ladder's guards:
//
//	① two-DB sim: A creates → sync → B sees; B completes + adds → sync →
//	   A sees the terminal state and the new row.
//	② idempotent re-import (the kill-mid-sync rerun contract).
//	③ equal-updated_at conflict → both rows kept, 'conflict' tagged.
//	④ clock-skew margin: a row backdated within the margin re-travels.
//	plus: terminal-history insert (no re-run), live-mirror guard, reflow
//	echo no-op, forward-only anchors, non-forwardable status never regresses.

import (
	"context"
	"encoding/json"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// newDeltaTestDB creates an isolated ledger (the same memory.db shape the
// tasks engine expects) and points memoryDBPath at it for the test's life.
//
// THE LEDGER IS ITS OWN PROFILE'S (changed 2026-11-04). This suite simulates two
// nodes by SWAPPING GOPI_MEMORY_DB between files, and the local half of that
// swap used to be a bare temp file with the profile left ambient: ledger in one
// place, notify target in another. That is the foreign-ledger shape the delivery
// gate (taskNotifyDelivers) withholds, and the sync tests that assert an alarm
// reached the notify pipe failed for exactly that reason — correctly, but for a
// reason that had nothing to do with the sync logic they name. The correct
// configuration for "this process's own ledger" is a profile that owns it, so
// GOPI_HOME moves with the ledger here, once, for every test in the suite.
func newDeltaTestDB(t *testing.T) string {
	t.Helper()
	path := taskNotifyProfileLedger(t)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("fresh db path already exists: %s", path)
	}
	// Force schema creation (openTasksDB inits on first open).
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close test db: %v", err)
	}
	return path
}

// newDeltaTestDBClient pins the CLIENT face for the delta suite (M4.20-B: the
// default sandbox must NOT rule — a server-role host (peerbox's settings or
// hostname fallback) turns every equal-rank residue into a ruling, which
// breaks the echo-absorption semantics the client tests assert). Tests
// that exercise the ruling path set the role themselves.
//
// The pin is UNDECLARED, not the literal "client": IsServerNode() is false for
// an undeclared role too (memory_sync_delta.go's N1 fail-safe), so the
// no-ruling face is kept while a caller may still write LOCALLY. A declared
// client with no sync_to peer makes TaskSubmit refuse (task_submit_forward.go
// — the ledger-address gate), which breaks the writeback tests that submit a
// real row here; the refusal itself is asserted in remote_submit_repro_test.go.
func newDeltaTestDBClient(t *testing.T) string {
	t.Helper()
	t.Setenv(config.RoleEnv, "")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	return newDeltaTestDB(t)
}

// deltaInsertRaw writes a row directly (test fixture — TaskNew/TaskSubmit
// create in_progress rows and stamp updated_at themselves; the delta tests
// need controlled anchors).
func deltaInsertRaw(t *testing.T, taskID, summary, status string, createdAt, updatedAt float64, tags string) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	endedAt := any(nil)
	if terminalTaskStatus(status) {
		endedAt = updatedAt
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO tasks (task_id, summary, status, session_id, model, parent_task_id,
		    tags, payload, source, claimed_by, claimed_at, verify_evidence, deliverable,
		    created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, '', '', '', ?, '', 'auto', '', NULL, '', '', ?, ?, ?)`,
		taskID, summary, status, tags, createdAt, updatedAt, endedAt); err != nil {
		t.Fatalf("fixture insert %s: %v", taskID, err)
	}
}

// deltaPatch v2 patch builder (rows sorted by updated_at ASC, id ASC — the
// export's order; the merge never requires it but tests keep it honest).
func deltaPatch(node string, rows ...SyncTaskRow) *SyncTaskPatch {
	if rows == nil {
		rows = []SyncTaskRow{}
	}
	return &SyncTaskPatch{Version: syncPatchVersionDelta, Node: node, ExportedAt: 0, Rows: rows}
}

// syncTestClockBackstep backdates a fixture's timestamps so assertions on
// "the code wrote time.Now()" are strictly greater even within the same
// wall-clock second.
const syncTestClockBackstep = 3600

// syncTestSleepToNextSecond parks the test until the wall clock's Unix-second
// tick turns — engine writes stamp time.Now().Unix() at second resolution, so
// a second write must land in a NEW second to order strictly after the first.
func syncTestSleepToNextSecond() {
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(2 * time.Second))) //stylegate:allow-sleep second-boundary alignment: the sync delta window is second-granular
}

// TestSyncDeltaTwoNodeHandoff — epic 实测 ①: A creates 3 rows → sync → B
// sees 3; B completes one + adds one → sync → A sees terminal + new row.
// Two REAL dbs (path-swapped via GOPI_MEMORY_DB) — the E2E sim's shape.
func TestSyncDeltaTwoNodeHandoff(t *testing.T) {
	pathA := filepath.Join(t.TempDir(), "a-memory.db")
	pathB := filepath.Join(t.TempDir(), "b-memory.db")
	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-1", "alpha", taskStatusPending, base, base, "")
	deltaInsertRaw(t, "t-2", "bravo", taskStatusPending, base+1, base+1, "")
	deltaInsertRaw(t, "t-3", "charlie", taskStatusPending, base+2, base+2, "")

	patchA, stats, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export A: %v", err)
	}
	if stats == nil || len(patchA.Rows) != 3 {
		t.Fatalf("want 3 rows from A, got %d", len(patchA.Rows))
	}
	anchorA := syncEnvAnchor(patchA.Rows)
	if anchorA != base+2 {
		t.Fatalf("anchor = max(updated_at): want %.0f got %.0f", base+2, anchorA)
	}
	// M4.20-B: pin CLIENT role for BOTH nodes — the merge residues below
	// (echo absorption) are client semantics; server-role ruling on echo
	// rows is TestSyncDeltaServerResolvesConflict's subject.
	t.Setenv(config.RoleEnv, "client")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	// --- node B: merge A's delta. GOPI_SYNC_NODE names this simulated node
	// "b": the export's anti-reflow gate excludes from-<self> rows, and B's
	// imported A-rows carry from-a (not from-b) — the same-host sim needs a
	// node identity distinct from A's, or B's own push would self-filter.
	t.Setenv("GOPI_SYNC_NODE", "b")
	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	pullStats, err := SyncTaskImportDelta(patchA)
	if err != nil {
		t.Fatalf("import into B: %v", err)
	}
	if pullStats.RowsImported != 3 {
		t.Fatalf("B imported %d, want 3 (%v)", pullStats.RowsImported, pullStats.Errors)
	}
	// B completes one (its anchor advances beyond A's copy) and adds its own.
	now := base + 10
	deltaUpdateStatus(t, "t-2", taskStatusCompleted, now)
	deltaInsertRaw(t, "t-9", "delta-from-b", taskStatusPending, now, now, "")

	// --- back to A: pull B's delta. t-2's anchor now > A's base+1 → newer
	// wins (forward migration); t-9 unknown → insert with B's from-tag; the
	// t-1/t-3 echoes stay no-ops. NOTE (M4.20-B): on a SERVER-role node the
	// t-1/t-3 echoes rule as sync-resolved (RowsResolved) instead of
	// RowsAlready — the server ruling IS the idempotent no-op's new face
	// for equal-anchor rows.
	patchB, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export B: %v", err)
	}
	fromB := SyncTaskTagPrefix + patchB.Node
	t.Setenv("GOPI_MEMORY_DB", pathA)
	backStats, err := SyncTaskImportDelta(patchB)
	if err != nil {
		t.Fatalf("merge B's delta into A: %v", err)
	}
	if backStats.RowsMigrated != 1 {
		t.Fatalf("A: want 1 migrated (t-2 → completed), got %+v", backStats)
	}
	if backStats.RowsImported != 1 {
		t.Fatalf("A: want 1 imported (t-9), got %+v", backStats)
	}
	if backStats.RowsAlready != 2 {
		t.Fatalf("A: want 2 echo no-ops (t-1,t-3, client-role), got %+v", backStats)
	}
	got := syncDeltaRow(t, "t-2")
	if got.Status != taskStatusCompleted {
		t.Fatalf("A: t-2 status = %s, want completed (forward migration)", got.Status)
	}
	if got.UpdatedAt != now {
		t.Fatalf("A: t-2 updated_at = %.0f, want %.0f (remote truth)", got.UpdatedAt, now)
	}
	if got.EndedAt != now {
		t.Fatalf("A: t-2 ended_at = %.0f, want %.0f", got.EndedAt, now)
	}
	got9 := syncDeltaRow(t, "t-9")
	if got9 == nil || got9.Status != taskStatusPending {
		t.Fatalf("A: t-9 missing or wrong status: %+v", got9)
	}
	if !hasTag(got9.Tags, fromB) {
		t.Fatalf("A: t-9 tags %q missing %s provenance", got9.Tags, fromB)
	}
}

// TestSyncDeltaIdempotentRerun — epic 实测 ②: the same delta applied twice
// no-ops on the second pass (the kill-mid-sync → rerun contract: nothing
// double-applies, nothing duplicates).
func TestSyncDeltaIdempotentRerun(t *testing.T) {
	newDeltaTestDBClient(t)
	// M4.20-B: pin CLIENT — echo absorption, not server ruling.
	t.Setenv(config.RoleEnv, "client")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-1", "alpha", taskStatusPending, base, base, "")

	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	first, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import 1: %v", err)
	}
	if first.RowsAlready != 1 {
		t.Fatalf("first import of an owned row: want already=1, got %+v", first)
	}
	second, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import 2: %v", err)
	}
	if second.RowsImported != 0 || second.RowsAlready != 1 || second.RowsConflict != 0 {
		t.Fatalf("rerun must fully no-op, got %+v", second)
	}
	// DB row count unchanged.
	if n := deltaRowCount(t); n != 1 {
		t.Fatalf("rerun changed row count: %d", n)
	}
}

// TestSyncDeltaConflictEqualAnchor — the CLIENT side of epic 实测 ③: both
// sides advance the same task to different statuses at the SAME updated_at.
// A CLIENT keeps the local row's status, unions tags, stamps 'conflict' —
// never rewrites (the server's verdict reaches it via the next pull as
// forward migration). This test FORCES the client path: the hostname is
// planted in sync_peers, so syncIsServerNode() is false.
// TestSyncDeltaContentDivergenceEqualAnchorClient — the CLIENT side of a
// same-anchor CONTENT divergence (both nodes restate the same task_id
// differently): the local row's status and content are never rewritten, the
// peer's OWN tag unions in, and the divergence is recorded as the WARNING
// counter RowsDiverged — NOT as 'conflict' (the must-arbitrate marker a
// STATUS divergence carries). This test FORCES the client path: the hostname
// is planted in sync_peers, so syncIsServerNode() is false.
func TestSyncDeltaContentDivergenceEqualAnchorClient(t *testing.T) {
	newDeltaTestDBClient(t)
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		self = "gopi-test-client"
	}
	t.Setenv(config.SyncPeersEnv, self)
	// Explicit CLIENT role: the N1 fix made role explicit (config/env)
	// primary — without this, ambient settings (role=server on peerbox)
	// overrides the peer-planting below and flips the test to the server
	// path. GOPI_NODE_ROLE=client + identity reset keeps the test honest.
	t.Setenv("GOPI_NODE_ROLE", "client")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	base := float64(time.Now().Unix())
	// M4.20-B: equal-ANCHOR terminal peers no longer conflict — the later
	// timestamp covers (后到覆盖). The residue the rank ladder cannot order
	// is an EQUAL-RANK NON-TERMINAL pair with DIFFERING statuses (rank 0:
	// pending vs reopened), so the fixture pins that shape.
	deltaInsertRaw(t, "t-c", "contested", taskStatusPending, base, base, "team-a")

	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-c", Summary: "contested", Status: taskStatusReopened,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"team-b"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsConflict != 1 || stats.RowsDiverged != 0 {
		t.Fatalf("want 1 status conflict (and no content divergence), got %+v", stats)
	}
	row := syncDeltaRow(t, "t-c")
	if row.Status != taskStatusPending {
		t.Fatalf("local row's status was rewritten: %s", row.Status)
	}
	if !hasTag(row.Tags, syncConflictTag) {
		t.Fatalf("row tags %q missing 'conflict'", row.Tags)
	}
	if !hasTag(row.Tags, "team-b") {
		t.Fatalf("row tags %q missing tag union (the peer's own tag team-b)", row.Tags)
	}
	if hasTag(row.Tags, SyncTaskTagPrefix+"other") {
		t.Fatalf("row tags %q must NOT carry channel metadata from-other (rule ⑦)", row.Tags)
	}
	// The same echo again (a re-sync) stays a no-op (already-tagged).
	again, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if again.RowsConflict != 0 || again.RowsTagged != 0 || again.RowsAlready != 1 {
		t.Fatalf("conflict echo must no-op, got %+v", again)
	}
}

// TestSyncDeltaServerResolvesConflict — 冲突语义定稿 (nick): the SERVER
// side of an equal-updated_at both-sides-wrote conflict rules immediately —
// status=completed, ended_at=now, tags += sync-resolved, updated_at=now —
// token-free pure Go. Real E2E shape (both GOPI_MEMORY_DBs swapped): the
// server's verdict reaches the client through the next pull as FORWARD
// MIGRATION (client never rules), and a re-run is idempotent (③锚点推进,
// ③重跑幂等).
func TestSyncDeltaServerResolvesConflict(t *testing.T) {
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		self = "gopi-test-server"
	}
	// ① config role must NOT flip this test: the ambient settings.json may
	// declare role=client (a real client host runs this suite) — pin the
	// role env ABOVE the legacy peers pin so the legacy hostname fallback
	// (②) is what's under test here (236fb03 made config the primary
	// source; this pin keeps the legacy path deterministic either way).
	config.ResetNodeIdentityForTest() // memoised role=client would survive the env pins
	t.Setenv(config.RoleEnv, "")
	t.Setenv(config.SyncToEnv, "")
	// --- server side: the import must run as the SERVER.
	// 236fb03 made the config role the PRIMARY source and demoted the
	// hostname heuristic to a fail-safe-CLIENT fallback (N1) — a foreign
	// peers pin alone can no longer produce a server. Declare the role
	// through its env override (GOPI_NODE_ROLE=server) so the ruling path
	// is deterministic on every host; the peers pin below stays as
	// belt-and-braces for the legacy fallback branch.
	t.Setenv(config.RoleEnv, "server")
	t.Setenv(config.SyncPeersEnv, "client-node-under-test")
	newDeltaTestDB(t)
	// base is one hour back: the ruling stamps updated_at=now, and the
	// anchor-advance assertion needs now STRICTLY greater than the fixture.
	base := float64(time.Now().Unix() - syncTestClockBackstep)
	deltaInsertRaw(t, "t-s", "contested", taskStatusInProgress, base, base, "team-a")
	patch := deltaPatch("client-node",
		SyncTaskRow{TaskID: "t-s", Summary: "contested-elsewhere", Status: taskStatusInProgress,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"team-b"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("server import: %v", err)
	}
	if stats.RowsResolved != 1 || stats.RowsConflict != 0 {
		t.Fatalf("want 1 server-resolved, got %+v", stats)
	}
	row := syncDeltaRow(t, "t-s")
	if row.Status != taskStatusCompleted {
		t.Fatalf("server must rule completed, got %s", row.Status)
	}
	if row.EndedAt <= 0 {
		t.Fatalf("ended_at must be stamped, got %v", row.EndedAt)
	}
	if row.UpdatedAt <= base {
		t.Fatalf("anchor must ADVANCE (updated_at=now > base %v), got %v", base, row.UpdatedAt)
	}
	if !hasTag(row.Tags, syncTagSyncResolved) || !hasTag(row.Tags, "team-b") {
		t.Fatalf("tags %q missing sync-resolved / the peer's own team-b", row.Tags)
	}
	// Idempotent re-run: the row is now terminal-completed; the peer echo
	// (same old status) walks the remote<local branch → already, no churn.
	again, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("server re-import: %v", err)
	}
	if again.RowsResolved != 0 || again.RowsAlready != 1 {
		t.Fatalf("server rerun must no-op, got %+v", again)
	}
	rerow := syncDeltaRow(t, "t-s")
	if rerow.Status != taskStatusCompleted || !hasTag(rerow.Tags, syncTagSyncResolved) {
		t.Fatalf("rerun mutated the ruling: %s %q", rerow.Status, rerow.Tags)
	}

	// --- client side: the verdict arrives via the next pull. The client's
	// row (same id, its own failed-at-base state) receives the server's
	// NEWER completed row → syncDeltaApplyNewer forward migration.
	clientPath := filepath.Join(t.TempDir(), "client.db")
	t.Setenv("GOPI_MEMORY_DB", clientPath)
	if _, err := os.Stat(clientPath); err == nil {
		t.Fatalf("fresh client db path already exists: %s", clientPath)
	}
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("open client db: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO tasks (task_id, summary, status, session_id, model, parent_task_id,
		    tags, payload, source, claimed_by, claimed_at, verify_evidence, deliverable,
		    created_at, updated_at, ended_at)
		 VALUES ('t-s', 'contested', 'failed', '', '', '', 'team-b', '', 'auto', '', NULL, '', '', ?, ?, NULL)`,
		base, base); err != nil {
		closeDB()
		t.Fatalf("client fixture: %v", err)
	}
	closeDB()
	verdict := deltaPatch("server-node",
		SyncTaskRow{TaskID: "t-s", Summary: "contested", Status: taskStatusCompleted,
			UpdatedAt: int64(row.UpdatedAt), EndedAt: int64(row.EndedAt), CreatedAt: int64(base),
			Source: "auto", Tags: []string{"team-b", syncTagSyncResolved}})
	pull, err := SyncTaskImportDelta(verdict)
	if err != nil {
		t.Fatalf("client pull: %v", err)
	}
	if pull.RowsMigrated != 1 {
		t.Fatalf("client must forward-migrate the verdict, got %+v", pull)
	}
	crow := syncDeltaRow(t, "t-s")
	if crow.Status != taskStatusCompleted || !hasTag(crow.Tags, syncTagSyncResolved) {
		t.Fatalf("client row not migrated to the verdict: %s %q", crow.Status, crow.Tags)
	}
	// And the SAME pull again is a pure no-op (③重跑幂等).
	pull2, err := SyncTaskImportDelta(verdict)
	if err != nil {
		t.Fatalf("client re-pull: %v", err)
	}
	if pull2.RowsMigrated != 0 || pull2.RowsAlready != 1 {
		t.Fatalf("client re-pull must no-op, got %+v", pull2)
	}
}

// TestSyncDeltaEqualAnchorEcho — rule ⑦ 防回流: a peer re-sends rows it
// received from us (same id, same anchor, same status) → pure no-op, no
// conflict tag, no tag churn.
func TestSyncDeltaEqualAnchorEcho(t *testing.T) {
	newDeltaTestDBClient(t)
	// M4.20-B: pin CLIENT — echo absorption, not server ruling.
	t.Setenv(config.RoleEnv, "client")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-e", "echo", taskStatusPending, base, base, "from-a,mine")
	patch := deltaPatch("b",
		SyncTaskRow{TaskID: "t-e", Summary: "echo", Status: taskStatusPending,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"from-a", "mine"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsAlready != 1 || stats.RowsConflict != 0 || stats.RowsTagged != 0 {
		t.Fatalf("echo must no-op cleanly, got %+v", stats)
	}
}

// TestSyncDeltaTerminalHistoryInsert — a terminal row for an UNKNOWN id is
// history: inserted AS TERMINAL (never re-queued pending — importing it
// pending would re-run finished work; that was M4.8's refusal rule, the
// v2 merge mirrors the row instead).
func TestSyncDeltaTerminalHistoryInsert(t *testing.T) {
	newDeltaTestDBClient(t)
	base := float64(time.Now().Unix())
	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-h", Summary: "done elsewhere", Status: taskStatusCompleted,
			UpdatedAt: int64(base), CreatedAt: int64(base), EndedAt: int64(base), Source: "llm"})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsImported != 1 {
		t.Fatalf("want 1 imported, got %+v", stats)
	}
	row := syncDeltaRow(t, "t-h")
	if row.Status != taskStatusCompleted {
		t.Fatalf("history row inserted as %s, want completed", row.Status)
	}
	if row.EndedAt != base {
		t.Fatalf("history ended_at = %.0f, want %.0f", row.EndedAt, base)
	}
}

// TestSyncDeltaLiveExecutionGuard — a locally CLAIMED row (claimed_by set,
// live execution) is never clobbered by the remote's terminal report (the
// M4.8 hard rule carried into v2).
// TestSyncDeltaLiveExecutionGuard — M4.20-B NOTE: the old "locally claimed
// row is never clobbered" guard is SUBSUMED by the rank model. in_progress
// (rank 2) → completed (rank 3) is a FORWARD move, and start-point
// atomicity (claim-broadcast M4.18 + 起点 CAS) guarantees the terminal
// verdict is the single-runner truth: a local live row whose peer already
// wrote back terminal is a CRASHED runner's residue, and converging it to
// the terminal verdict is exactly the epic's 双侧收敛. The rank guard still
// refuses REGRESSIONS (terminal → live mirror), which the M4.18 refusal
// semantics + YieldClaimedTask handle at claim time.
func TestSyncDeltaLiveExecutionGuard(t *testing.T) {
	newDeltaTestDBClient(t)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-live", "running here", taskStatusInProgress, base, base, "")
	deltaSetClaim(t, "t-live", "local-runner")

	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-live", Summary: "running here", Status: taskStatusCompleted,
			UpdatedAt: int64(base + 5), CreatedAt: int64(base), Source: "auto"})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// rank 2 → 3 is forward: the row converges to the terminal truth.
	if stats.RowsMigrated != 1 {
		t.Fatalf("forward converge must migrate, got %+v", stats)
	}
	if row := syncDeltaRow(t, "t-live"); row.Status != taskStatusCompleted {
		t.Fatalf("row must converge to the terminal verdict, got %s", row.Status)
	}
}

// TestSyncDeltaMirrorClaim — a remote's claim of a pending row mirrors as a
// claim-LESS claimed status (the local dispatcher only claims pending rows,
// so the mirror keeps it off the row — anti double-execution).
func TestSyncDeltaMirrorClaim(t *testing.T) {
	newDeltaTestDBClient(t)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-m", "claim me", taskStatusPending, base, base, "")

	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-m", Summary: "claim me", Status: taskStatusClaimed,
			UpdatedAt: int64(base + 1), CreatedAt: int64(base), Source: "auto"})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsMirrored != 1 {
		t.Fatalf("want 1 mirrored, got %+v", stats)
	}
	row := syncDeltaRow(t, "t-m")
	if row.Status != taskStatusClaimed {
		t.Fatalf("mirror status = %s, want claimed", row.Status)
	}
	if row.HasClaim {
		t.Fatalf("mirrored row must stay claim-less locally (claimed_at=%.0f)", row.ClaimedAt)
	}
}

// TestSyncDeltaTerminalNeverRegresses — a completed row is never pulled
// back to claimed/in_progress by a remote row with a newer anchor (终态不回退).
func TestSyncDeltaTerminalNeverRegresses(t *testing.T) {
	newDeltaTestDBClient(t)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-done", "finished", taskStatusCompleted, base, base, "")

	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-done", Summary: "finished", Status: taskStatusInProgress,
			UpdatedAt: int64(base + 5), CreatedAt: int64(base), Source: "auto"})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsAlready != 1 || stats.RowsMirrored != 0 {
		t.Fatalf("terminal must never regress, got %+v", stats)
	}
	if row := syncDeltaRow(t, "t-done"); row.Status != taskStatusCompleted {
		t.Fatalf("terminal regressed to %s", row.Status)
	}
}

// TestSyncDeltaSkewMargin — epic ④: a row whose updated_at falls within
// syncAnchorMarginBackdate BELOW the stored anchor still travels (the
// export subtracts the margin from the cutoff).
func TestSyncDeltaSkewMargin(t *testing.T) {
	newDeltaTestDBClient(t)
	now := float64(time.Now().Unix())
	inside := now - syncAnchorMarginBackdate.Seconds() + 2 // inside the margin
	outside := now - syncAnchorMarginBackdate.Seconds() - 60
	deltaInsertRaw(t, "t-in", "skewed writer", taskStatusPending, inside, inside, "")
	deltaInsertRaw(t, "t-out", "old news", taskStatusPending, outside, outside, "")

	// Anchor at `now` — a session that already saw everything up to now.
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{AfterSource: now})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range patch.Rows {
		ids[r.TaskID] = true
	}
	if !ids["t-in"] {
		t.Fatalf("margin row must travel (cutoff=%.0f, row=%.0f, margin=%v)",
			now, inside, syncAnchorMarginBackdate)
	}
	if ids["t-out"] {
		t.Fatalf("row outside the margin must not travel")
	}
}

// TestSyncStateForwardOnlyAnchors — a stale session (crashed retry with an
// old envelope) can never roll an anchor backwards.
func TestSyncStateForwardOnlyAnchors(t *testing.T) {
	newDeltaTestDBClient(t)
	if err := syncStateCommit("peer-a", 1000); err != nil {
		t.Fatalf("commit 1000: %v", err)
	}
	if err := syncStateCommit("peer-a", 500); err != nil {
		t.Fatalf("commit 500: %v", err)
	}
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	_, source, err := syncStateGet(context.Background(), db, "peer-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if source != 1000 {
		t.Fatalf("anchor rolled back: %.0f, want 1000", source)
	}
}

// TestSyncStateSchemaRoundTrip — the sync_state table survives reopen
// (ensureTasksSchema creates it idempotently; 断线/重启恢复继续).
func TestSyncStateSchemaRoundTrip(t *testing.T) {
	path := newDeltaTestDBClient(t)
	if err := syncStateCommit("peer-x", 42); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// Reopen (a new process).
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_, source, err := syncStateGet(context.Background(), db, "peer-x")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if source != 42 {
		t.Fatalf("anchor lost across reopen: %.0f", source)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestSyncRemoteServeRoundTrip — the peer side: stdin patch in → merged →
// its own envelope out, receive-anchor committed (the bidirectional session
// without the ssh leg — the E2E sim exercises the ssh leg end to end).
func TestSyncRemoteServeRoundTrip(t *testing.T) {
	peerDB := newDeltaTestDBClient(t)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-p1", "peer row", taskStatusPending, base, base, "")
	_ = peerDB

	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	patch.Node = "initiator"
	body, err := marshalJSON(patch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := SyncRemoteServe(body)
	if err != nil {
		t.Fatalf("serve-remote: %v", err)
	}
	gotPatch, anchor, err := syncSessionFromEnvelopeTest(out)
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if gotPatch.Version != syncPatchVersionDelta {
		t.Fatalf("envelope version = %d", gotPatch.Version)
	}
	if anchor != base {
		t.Fatalf("envelope anchor = %.0f, want %.0f (max updated_at of what the initiator sent)",
			anchor, base)
	}
	// The receive anchor for the initiator was committed.
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	_, initiatorAnchor, err := syncStateGet(context.Background(), db, "initiator")
	if err != nil {
		t.Fatalf("sync_state get: %v", err)
	}
	if initiatorAnchor != base {
		t.Fatalf("receive anchor = %.0f, want %.0f", initiatorAnchor, base)
	}
}

// TestSyncTaskExportDeltaEmptyPatch — an empty delta stays a VALID v2 patch
// ([] not null) and its anchor is 0 (the caller keeps the old anchor).
func TestSyncTaskExportDeltaEmptyPatch(t *testing.T) {
	newDeltaTestDBClient(t)
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{AfterSource: float64(time.Now().Unix() + 100)})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if patch.Version != syncPatchVersionDelta || patch.Rows == nil || len(patch.Rows) != 0 {
		t.Fatalf("empty delta malformed: version=%d rows=%v", patch.Version, patch.Rows)
	}
	if a := syncEnvAnchor(patch.Rows); a != 0 {
		t.Fatalf("empty anchor = %.0f, want 0", a)
	}
}

// --- test helpers ---

// openDeltaDB initializes a ledger at path (the two-node sim's per-node
// schema bootstrap).
func openDeltaDB(t *testing.T, path string) {
	t.Helper()
	db, err := openTasksDB(context.Background(), path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// deltaUpdateStatus advances a row's status + anchor directly (fixture).
func deltaUpdateStatus(t *testing.T, taskID, status string, updatedAt float64) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	endedAt := any(nil)
	if terminalTaskStatus(status) {
		endedAt = updatedAt
	}
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET status = ?, updated_at = ?, ended_at = ? WHERE task_id = ?`,
		status, updatedAt, endedAt, taskID); err != nil {
		t.Fatalf("update %s: %v", taskID, err)
	}
}

// deltaSetClaim stamps a local claim (fixture for the live-execution guard).
func deltaSetClaim(t *testing.T, taskID, claimedBy string) {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET claimed_by = ?, claimed_at = ? WHERE task_id = ?`,
		claimedBy, float64(time.Now().Unix()), taskID); err != nil {
		t.Fatalf("claim %s: %v", taskID, err)
	}
}

// TestSyncDeltaWidensParentSet — THE REGRESSION for the dropped parent set.
//
// Scenario (the defect measured on two real nodes): the origin holds one child
// row carrying parents {P1,P2,P3} while the peer's ledger already holds the
// same child with P1 ONLY (it synced before P2/P3 were attached). The delta
// merge's adopt arm fires only on a row with NO parents at all, so the wider
// remote set was silently discarded: the child stayed claimable as soon as P1
// released, and P2/P3 were never known on the peer — no error, no counter, no
// re-send.
//
// The assertions are deliberately made against the LEDGER (the two raw
// columns, and the pending view's [queue-skip] marker through the claim gate),
// not against the code's own option struct: "the set reached the row" is the
// deliverable, and only a re-read can show it.
func TestSyncDeltaWidensParentSet(t *testing.T) {
	newDeltaTestDBClient(t)
	base := float64(time.Now().Unix()) - syncTestClockBackstep

	// Three parents, all LIVE here (pending parks the child: Rule 33's release
	// set is exactly {completed, verified, superseded}).
	for _, p := range []string{"t-pw-p1", "t-pw-p2", "t-pw-p3"} {
		deltaInsertRaw(t, p, "parent "+p, taskStatusPending, base, base, "")
	}
	// The peer's copy is the OLDER shape: the child with ONE parent — that is
	// the whole point of the regression, so the local row must carry an edge
	// BEFORE the wider patch arrives.
	deltaInsertRaw(t, "t-pw-child", "child of three", taskStatusPending, base, base, "")
	dbFix, closeFix, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	if _, err := dbFix.ExecContext(context.Background(),
		`UPDATE tasks SET parent_task_id = 't-pw-p1' WHERE task_id = 't-pw-child'`); err != nil {
		t.Fatalf("seed the local single parent: %v", err)
	}
	closeFix()

	// The origin's copy carries the child with the FULL set, one second later
	// so the rank ladder takes the forward path (the merge, not the echo).
	full := []string{"t-pw-p1", "t-pw-p2", "t-pw-p3"}
	stats, err := SyncTaskImportDelta(deltaPatch("peer",
		SyncTaskRow{TaskID: "t-pw-child", Summary: "child of three", Status: taskStatusPending,
			ParentTaskID: full[0], ParentTaskIDs: TaskParentIDsJoin(full[1:]),
			UpdatedAt: int64(base) + 1, CreatedAt: int64(base), Source: "auto"}))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsParentsWidened != 1 {
		t.Fatalf("the wider remote set must be landed and COUNTED (RowsParentsWidened=1) — got %+v", stats)
	}
	if got := multiParentOf(t, "t-pw-child"); len(got) != 3 ||
		got[0] != "t-pw-p1" || got[1] != "t-pw-p2" || got[2] != "t-pw-p3" {
		t.Fatalf("the child must carry ALL THREE parents after the widen (local order kept, new edges appended), got %v", got)
	}
	if row := syncDeltaRow(t, "t-pw-child"); row == nil || row.ParentTaskID != "t-pw-p1" {
		t.Fatalf("parent 1 must not move (an older reader keeps the dependency), row=%+v", row)
	}

	// The marker the acceptance criterion names: an ALL-LIVE parent set parks
	// the row, so the pending view calls it unclaimable.
	view, err := TaskList(taskStatusPending)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	childLine := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, "#t-pw-child ") {
			childLine = line
		}
	}
	if !strings.Contains(childLine, "[queue-skip]") {
		t.Fatalf("the widened child must render as [queue-skip] (three live parents park it), line = %q:\n%s", childLine, view)
	}

	// IDEMPOTENCY: the same patch again writes nothing and counts nothing — a
	// re-sync of an already-widened row is the ordinary echo.
	again, err := SyncTaskImportDelta(deltaPatch("peer",
		SyncTaskRow{TaskID: "t-pw-child", Summary: "child of three", Status: taskStatusPending,
			ParentTaskID: full[0], ParentTaskIDs: TaskParentIDsJoin(full[1:]),
			UpdatedAt: int64(base) + 1, CreatedAt: int64(base), Source: "auto"}))
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if again.RowsParentsWidened != 0 {
		t.Fatalf("a re-sync of an already-widened row must not count a second widen — got %+v", again)
	}
	if got := multiParentOf(t, "t-pw-child"); len(got) != 3 {
		t.Fatalf("re-sync must not change the set, got %v", got)
	}
}

// TestSyncDeltaParentSetRelation — the decision surface, as a table. The three
// answers are the whole contract of the widen arm: only a STRICT SUPERSET is
// merged; a smaller set can never TAKE an edge away, and two nodes each holding
// an edge the other lacks are a disagreement this leg reports instead of
// resolving.
func TestSyncDeltaParentSetRelation(t *testing.T) {
	cases := []struct {
		name   string
		local  []string
		remote []string
		want   syncParentSetRelation
	}{
		{"equal", []string{"a", "b"}, []string{"a", "b"}, syncParentSetNoNewEdge},
		{"remote-smaller", []string{"a", "b"}, []string{"a"}, syncParentSetNoNewEdge},
		{"both-empty", nil, nil, syncParentSetNoNewEdge},
		{"local-empty-remote-one", nil, []string{"a"}, syncParentSetSuperset},
		{"added-at-origin", []string{"a"}, []string{"a", "b"}, syncParentSetSuperset},
		{"added-at-origin-unsorted", []string{"a", "b"}, []string{"c", "b", "a"}, syncParentSetSuperset},
		{"disjoint", []string{"a"}, []string{"b"}, syncParentSetDisjoint},
		{"partial-overlap", []string{"a", "x"}, []string{"a", "y"}, syncParentSetDisjoint},
	}
	for _, c := range cases {
		if got := syncDeltaParentSetRelation(c.local, c.remote); got != c.want {
			t.Fatalf("%s: relation(%v,%v) = %d, want %d", c.name, c.local, c.remote, got, c.want)
		}
	}
}

// TestSyncDeltaParentSetWidenRefusesDangling — a wider set is not a licence to
// write an edge this ledger cannot honour. Rule 33 parks a row whose parent
// names no row FOREVER, so the added edge is refused BY NAME and counted in
// RowsRefused — and the row keeps the parents it already had (the refusal must
// not half-write the set).
func TestSyncDeltaParentSetWidenRefusesDangling(t *testing.T) {
	newDeltaTestDBClient(t)
	base := float64(time.Now().Unix()) - syncTestClockBackstep
	deltaInsertRaw(t, "t-pw-known", "known parent", taskStatusPending, base, base, "")
	deltaInsertRaw(t, "t-pw-kid", "child", taskStatusPending, base, base, "")
	dbFix, closeFix, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	if _, err := dbFix.ExecContext(context.Background(),
		`UPDATE tasks SET parent_task_id = 't-pw-known' WHERE task_id = 't-pw-kid'`); err != nil {
		t.Fatalf("seed local parent: %v", err)
	}
	closeFix()

	stats, err := SyncTaskImportDelta(deltaPatch("peer",
		SyncTaskRow{TaskID: "t-pw-kid", Summary: "child", Status: taskStatusPending,
			ParentTaskID: "t-pw-known", ParentTaskIDs: "t-pw-ghost",
			UpdatedAt: int64(base) + 1, CreatedAt: int64(base), Source: "auto"}))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsRefused != 1 || len(stats.Errors) != 1 ||
		!strings.Contains(stats.Errors[0], "t-pw-ghost") {
		t.Fatalf("the dangling ADDED edge must be refused BY NAME and counted, got %+v", stats)
	}
	if stats.RowsParentsWidened != 0 {
		t.Fatalf("a refused widen must not count as a widen, got %+v", stats)
	}
	if got := multiParentOf(t, "t-pw-kid"); len(got) != 1 || got[0] != "t-pw-known" {
		t.Fatalf("the refusal must not half-write the set — row keeps exactly its known parent, got %v", got)
	}
}

// syncDeltaRow reads one row back (nil = absent).
func syncDeltaRow(t *testing.T, taskID string) *Task {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	row, err := taskByID(context.Background(), db, taskID)
	if err != nil {
		t.Fatalf("taskByID %s: %v", taskID, err)
	}
	return row
}

// deltaRowCount counts ledger rows (dup detection).
func deltaRowCount(t *testing.T) int {
	t.Helper()
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	defer closeDB()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// marshalJSON + syncSessionFromEnvelopeTest mirror the cmd-layer envelope
// decode so the test doesn't import package main.
func marshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", syncTaskJSONIndent)
}

func syncSessionFromEnvelopeTest(data []byte) (*SyncTaskPatch, float64, error) {
	var env struct {
		SyncTaskPatch
		Anchor float64 `json:"anchor"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, 0, err
	}
	return &env.SyncTaskPatch, env.Anchor, nil
}

// TestSyncDeltaContentDivergenceIsAWarning — 内容冲突降级 (nick): same id,
// same anchor, SAME status, DIFFERING summary/payload is NOT "both sides
// edited a shared immutable field = a bug to arbitrate". The row's identity
// is its task_id (UUID) and summary/payload are free-text restatements that
// differ across nodes by nature — so the merge NEVER rewrites either side's
// content (nick rule), unions the peer's OWN tags, and records the
// divergence as a WARNING: RowsDiverged on the report line + a warn log.
// It must NOT stamp the 'conflict' tag (the must-arbitrate-by-a-human
// marker is reserved for a STATUS divergence) and must NOT raise the
// legacy-conflict notify event. A re-echo is a no-op that still reports the
// divergence (the row stays diverged until a person edits its content).
func TestSyncDeltaContentDivergenceIsAWarning(t *testing.T) {
	newDeltaTestDBClient(t)
	// M4.20-B: pin CLIENT — echo absorption, not server ruling.
	t.Setenv(config.RoleEnv, "client")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-x", "local summary", taskStatusPending, base, base, "module:pipeline")
	localBefore := syncDeltaRow(t, "t-x")

	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-x", Summary: "HACKED summary", Payload: "hacked payload",
			Status: taskStatusPending, UpdatedAt: int64(base), CreatedAt: int64(base),
			Source: "auto", Tags: []string{"module:other"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsDiverged != 1 || stats.RowsConflict != 0 {
		t.Fatalf("content divergence must be REPORTED as a warning (RowsDiverged=1, RowsConflict=0), got %+v", stats)
	}
	after := syncDeltaRow(t, "t-x")
	if after.Summary != localBefore.Summary || after.Payload != localBefore.Payload {
		t.Fatalf("never-overwrite violated: local summary/payload rewritten\nbefore: %q / %q\nafter:  %q / %q",
			localBefore.Summary, localBefore.Payload, after.Summary, after.Payload)
	}
	if after.Status != localBefore.Status {
		t.Fatalf("equal-anchor divergence moved status: %s → %s", localBefore.Status, after.Status)
	}
	if hasTag(after.Tags, syncConflictTag) {
		t.Fatalf("tags %q must NOT carry 'conflict' (that tag is the must-arbitrate marker, reserved for a STATUS divergence)", after.Tags)
	}
	if !hasTag(after.Tags, "module:other") {
		t.Fatalf("tags %q missing the peer's own tag (union only adds)", after.Tags)
	}
	if !hasTag(after.Tags, "module:pipeline") {
		t.Fatalf("tags %q lost the local tag (union only adds)", after.Tags)
	}
	if stats.RowsTagged != 1 {
		t.Fatalf("a diverged row whose tag set changed is a tags union (RowsTagged=1), got %+v", stats)
	}
	// Re-echo of the same diverged patch: no write (the union is already
	// there → RowsAlready), but the divergence is still REPORTED — the row
	// stays diverged until a person edits its content, so a fresh report
	// line must keep surfacing it.
	again, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if again.RowsDiverged != 1 || again.RowsConflict != 0 || again.RowsTagged != 0 || again.RowsAlready != 1 {
		t.Fatalf("divergence re-echo must re-report (diverged=1) without writing (already=1), got %+v", again)
	}
	if againRow := syncDeltaRow(t, "t-x"); againRow.Summary != localBefore.Summary || againRow.Payload != localBefore.Payload {
		t.Fatal("re-echo rewrote content")
	}
}

// TestSyncDeltaContentDivergenceFiresNoConflictNotify — the downgrade's
// other half: a content divergence must NOT reach the legacy-conflict
// notify pipe (that alarm means "the normal path has zero conflicts, this
// firing is a bug signal" — a differing free-text payload is not that).
// The status-divergence path still fires it (TestSyncDeltaLegacyConflictFiresNotify).
func TestSyncDeltaContentDivergenceFiresNoConflictNotify(t *testing.T) {
	newDeltaTestDBClient(t)
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		self = "gopi-test-client"
	}
	t.Setenv(config.SyncPeersEnv, self)
	t.Setenv("GOPI_NODE_ROLE", "client")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	capture := filepath.Join(t.TempDir(), "notify-capture.json")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+capture+" && echo >> "+capture)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")

	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-div-notify", "local summary", taskStatusPending, base, base, "module:pipeline")
	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-div-notify", Summary: "peer restatement", Payload: "peer payload",
			Status: taskStatusPending, UpdatedAt: int64(base), CreatedAt: int64(base),
			Source: "auto", Tags: []string{"module:other"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsDiverged != 1 {
		t.Fatalf("want 1 divergence, got %+v", stats)
	}
	if b, err := os.ReadFile(capture); err == nil {
		var p taskNotifyPayload
		if json.Unmarshal(b, &p) == nil && p.Event == taskNotifyEventLegacyConflict {
			t.Fatalf("a content divergence must not fire the legacy-conflict alarm:\n%s", b)
		}
	}
	row := syncDeltaRow(t, "t-div-notify")
	if hasTag(row.Tags, syncConflictTag) {
		t.Fatalf("tags %q must not carry 'conflict'", row.Tags)
	}
}

// TestSyncDeltaTagsPartitionExport — epic ③ 模块分区选段导出: --tags
// restricts the delta to rows carrying that tag (comma-bounded LIKE), and
// composes with the anchor window. Non-matching rows never travel, so the
// two nodes' sync writes stay partitioned (lower conflict surface).
func TestSyncDeltaTagsPartitionExport(t *testing.T) {
	newDeltaTestDBClient(t)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-in", "in partition", taskStatusCompleted, base, base, "module:x,from-hp")
	deltaInsertRaw(t, "t-out", "outside", taskStatusCompleted, base+1, base+1, "module:y")
	deltaInsertRaw(t, "t-sub", "substring trap", taskStatusCompleted, base+2, base+2, "module:xx")

	patch, stats, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Tags: "module:x"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if stats.Scanned != 1 { // Scanned = rows RETURNED post-filter (delta semantics)
		t.Fatalf("scanned = %d, want 1", stats.Scanned)
	}
	if len(patch.Rows) != 1 || patch.Rows[0].TaskID != "t-in" {
		t.Fatalf("partition export must carry ONLY the matching row, got %+v", patch.Rows)
	}
	// A substring tag (module:x vs module:xx) must not leak through the
	// comma-bounded gate.
	patch2, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Tags: "module:xx"})
	if err != nil {
		t.Fatalf("export 2: %v", err)
	}
	if len(patch2.Rows) != 1 || patch2.Rows[0].TaskID != "t-sub" {
		t.Fatalf("exact-tag gate failed: got %+v", patch2.Rows)
	}
	// Empty filter = the whole window (session default unchanged).
	patch3, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export 3: %v", err)
	}
	if len(patch3.Rows) != 3 {
		t.Fatalf("no-filter export = %d rows, want 3", len(patch3.Rows))
	}
}

// TestSyncDeltaTagsPartitionSymmetricSession — epic ③ end to end: the
// TagsFilter rides in the patch and the PEER's serve-remote applies it to
// its own push. A's --tags module:x session pulls only x-rows from B AND
// receives only x-rows from B's push — symmetric partitioning.
func TestSyncDeltaTagsPartitionSymmetricSession(t *testing.T) {
	newDeltaTestDBClient(t) // node B (the serve-remote side)
	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-bx", "B x-row", taskStatusCompleted, base, base, "module:x")
	deltaInsertRaw(t, "t-by", "B y-row", taskStatusCompleted, base+1, base+1, "module:y")

	// Node A's patch: filtered, with TagsFilter stamped.
	aPatch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Tags: "module:x"})
	if err != nil {
		t.Fatalf("A export: %v", err)
	}
	aPatch.Node = "node-a"
	aPatch.TagsFilter = "module:x"
	body, err := json.Marshal(aPatch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// B serves: merges A's patch, exports ITS partitioned delta back.
	env, err := SyncRemoteServe(body)
	if err != nil {
		t.Fatalf("serve-remote: %v", err)
	}
	got, _, err := syncSessionFromEnvelopeTest(env)
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0].TaskID != "t-bx" {
		t.Fatalf("peer's push must be partitioned to module:x only, got %+v", got.Rows)
	}
	// A's row merged into B with provenance + original tags intact — the
	// session-merge side of the partition is the standard merge (imported
	// rows carry from-node); verified by the content-conflict test above.
}

// ---- M4.19 ③ conflict semantic re-label (legacy-conflict) ----

// TestSyncDeltaLegacyConflictFiresNotify — both conflict paths (client
// conflict-tag + server ruling) must raise the legacy-conflict synthetic
// notify event: since claim-broadcast (M4.18) the normal path has zero
// conflicts, a hit is a bug signal. Ruling itself stays unchanged (M4.16
// verdict downgraded to legacy backstop). The CLIENT conflict-tag path it
// exercises is the STATUS divergence at an equal anchor (pending vs
// reopened) — a content divergence is a WARNING since 内容冲突降级 and fires
// no alarm (TestSyncDeltaContentDivergenceFiresNoConflictNotify).
func TestSyncDeltaLegacyConflictFiresNotify(t *testing.T) {
	newDeltaTestDBClient(t)
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		self = "gopi-test-client"
	}
	t.Setenv(config.SyncPeersEnv, self)
	t.Setenv("GOPI_NODE_ROLE", "client")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	capture := filepath.Join(t.TempDir(), "notify-capture.json")
	// append, not truncate: the shared notify funnel delivers the ruling
	// alarm AND the row's own completed transition (task-notify-terminal-gap)
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+capture+" && echo >> "+capture)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")

	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-lc", "contested", taskStatusPending, base, base, "team-a")
	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-lc", Summary: "contested", Status: taskStatusReopened,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"team-b"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsConflict != 1 || stats.RowsDiverged != 0 {
		t.Fatalf("want 1 status conflict (and no content divergence), got %+v", stats)
	}
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("legacy-conflict event never reached the notify pipe: %v", err)
	}
	var p taskNotifyPayload
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	if p.Event != taskNotifyEventLegacyConflict {
		t.Fatalf("event = %q, want %q", p.Event, taskNotifyEventLegacyConflict)
	}
	if p.TaskID != "t-lc" {
		t.Fatalf("payload task_id = %q, want t-lc", p.TaskID)
	}
	// The re-echo (already tagged) is a no-op: no second event.
	os.Remove(capture)
	if _, err := SyncTaskImportDelta(patch); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if _, err := os.Stat(capture); err == nil {
		t.Fatalf("known-conflict re-echo must not re-fire the event")
	}
}

// TestSyncDeltaServerRulingFiresLegacyConflictNotify — the SERVER ruling
// path (syncDeltaServerResolve) also reports the anomaly: ruling preserved,
// alarm added.
func TestSyncDeltaServerRulingFiresLegacyConflictNotify(t *testing.T) {
	newDeltaTestDB(t)
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		self = "gopi-test-server"
	}
	t.Setenv(config.SyncPeersEnv, "some-other-node")
	t.Setenv("GOPI_NODE_ROLE", "server")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	capture := filepath.Join(t.TempDir(), "notify-capture.json")
	// append, not truncate: the shared notify funnel delivers the ruling
	// alarm AND the row's own completed transition (task-notify-terminal-gap)
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+capture+" && echo >> "+capture)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")

	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-lc-srv", "contested", taskStatusInProgress, base, base, "team-a")
	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-lc-srv", Summary: "contested-elsewhere", Status: taskStatusInProgress,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"team-b"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsResolved != 1 {
		t.Fatalf("server must rule (RowsResolved=1), got %+v", stats)
	}
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("server ruling must also fire legacy-conflict notify: %v", err)
	}
	if !notifyLogHasEvent(t, b, taskNotifyEventLegacyConflict) {
		t.Fatalf("legacy-conflict alarm missing from the delivered events:\n%s", b)
	}
}

// TestSyncDeltaServerRulingBlocksHumanTicket — bug053 M4.16 semantic-fix
// regression: a REAL human ticket (source=human, non-empty payload) sitting
// pending on the server (peerbox) whose peer mirror row — same task_id, SAME
// status (pending), SAME anchor, differing peer tags — collides with it
// (the exact bug050/051/052 signature: equal non-terminal status + equal
// anchor → the server blind-ruling fired) must NEVER be blind-flattened to
// completed. The ruling only exists to absorb 登记噪音 (source=auto mirror
// rows); a real ticket keeps its row (conflict tag union only) and raises
// the ledger-conflict-human-task alarm for a human/PA to adjudicate —
// bug050/051/052 were fake-closed exactly this way (sync-resolved on
// tickets whose implementation never existed).
func TestSyncDeltaServerRulingBlocksHumanTicket(t *testing.T) {
	newDeltaTestDB(t) // server role (default: no SyncPeersEnv → IsServerNode)
	t.Setenv("GOPI_NODE_ROLE", "server")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	capture := filepath.Join(t.TempDir(), "notify-capture.json")
	// append, not truncate: the shared notify funnel delivers the ruling
	// alarm AND the row's own completed transition (task-notify-terminal-gap)
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+capture+" && echo >> "+capture)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")

	base := float64(time.Now().Unix())
	// peerbox's REAL pending human ticket — the exact bug053 row class
	// (source=human, payload = the briefing).
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO tasks (task_id, summary, status, session_id, model, parent_task_id,
		    tags, payload, source, claimed_by, claimed_at, verify_evidence, deliverable,
		    created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, '', '', '', 'bug', ?, 'human', '', NULL, '', '', ?, ?, NULL)`,
		"t-bug053-human", "bug050 toolStreak 修复", "pending", "fix toolStreak 盲区: ...",
		base, base); err != nil {
		t.Fatalf("fixture insert: %v", err)
	}
	closeDB()

	// HP's mirror row of the SAME ticket — the bug050/051/052 signature:
	// SAME status (pending), SAME anchor, DIFFERING peer tags (the mirror
	// advanced its tag set → the equal-anchor row collides → server ruling).
	patch := deltaPatch("hp",
		SyncTaskRow{TaskID: "t-bug053-human", Summary: "bug050 toolStreak 修复", Status: taskStatusPending,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"sync-mirror"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsResolved != 0 {
		t.Fatalf("human ticket must NOT be blind-resolved (RowsResolved=0), got %+v", stats)
	}
	if stats.RowsConflict != 1 {
		t.Fatalf("human ticket must surface as a conflict (RowsConflict=1), got %+v", stats)
	}

	// The row is UNTOUCHED: still pending, no ended_at, only the conflict
	// tag unioned — never flattened to completed.
	db2, closeDB2, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB re-open: %v", err)
	}
	defer closeDB2()
	row, err := taskByID(context.Background(), db2, "t-bug053-human")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if row == nil {
		t.Fatalf("row vanished")
	}
	if row.Status != taskStatusPending {
		t.Fatalf("human ticket flattened: status=%s, want pending (M4.16 must not fake-close real work)", row.Status)
	}
	if !hasTag(row.Tags, syncConflictTag) {
		t.Fatalf("conflict tag must mark the blocked ruling, tags=%q", row.Tags)
	}
	if row.EndedAt != 0 {
		t.Fatalf("ended_at must stay unset on a blocked ruling, got %v", row.EndedAt)
	}

	// The alarm reaches the notify pipe.
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("ledger-conflict-human-task event never reached the notify pipe: %v", err)
	}
	var p taskNotifyPayload
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("payload parse: %v", err)
	}
	if p.Event != taskNotifyEventLedgerConflictHumanTask {
		t.Fatalf("event = %q, want %q", p.Event, taskNotifyEventLedgerConflictHumanTask)
	}
	if p.TaskID != "t-bug053-human" {
		t.Fatalf("payload task_id = %q, want t-bug053-human", p.TaskID)
	}

	// Re-echo of the same conflict = known no-op, no second event.
	os.Remove(capture)
	if _, err := SyncTaskImportDelta(patch); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if _, err := os.Stat(capture); err == nil {
		t.Fatalf("known-conflict re-echo must not re-fire the human-task alarm")
	}
}

// TestSyncDeltaServerRulingStillFlattensAutoMirror — the OTHER side of the
// bug053 guard: a source=auto mirror row (登记噪音) MAY still be blind-
// flattened by the server ruling — that is the ruling's only legitimate
// use. The guard must not break the legacy noise-absorption path.
func TestSyncDeltaServerRulingStillFlattensAutoMirror(t *testing.T) {
	newDeltaTestDB(t)
	t.Setenv("GOPI_NODE_ROLE", "server")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	capture := filepath.Join(t.TempDir(), "notify-capture.json")
	// append, not truncate: the shared notify funnel delivers the ruling
	// alarm AND the row's own completed transition (task-notify-terminal-gap)
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+capture+" && echo >> "+capture)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "all")

	base := float64(time.Now().Unix())
	deltaInsertRaw(t, "t-mirror-auto", "auto-registered mirror row", taskStatusInProgress, base, base, "team-a")
	patch := deltaPatch("other",
		SyncTaskRow{TaskID: "t-mirror-auto", Summary: "auto-registered mirror row", Status: taskStatusInProgress,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"team-b"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsResolved != 1 {
		t.Fatalf("auto mirror must still be blind-resolved (RowsResolved=1), got %+v", stats)
	}
	// The ruling fires the legacy-conflict alarm (unchanged), NOT the
	// human-task alarm; the shared funnel also delivers the row's own
	// completed transition, so scan all delivered events rather than
	// parsing the last one.
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("server ruling must fire legacy-conflict notify: %v", err)
	}
	if !notifyLogHasEvent(t, b, taskNotifyEventLegacyConflict) {
		t.Fatalf("legacy-conflict alarm missing from the delivered events:\n%s", b)
	}
	if notifyLogHasEvent(t, b, taskNotifyEventLedgerConflictHumanTask) {
		t.Fatalf("auto mirror must NOT fire the human-task alarm:\n%s", b)
	}
}

// TestSyncDeltaPendingHumanTicketVsCancelledMirror — bug053 requirement C,
// AMENDED 2026-10-29 by measurement. The original assertion was "the rank
// ladder forward-migrates pending(0) → cancelled(3) — a terminal, REOPENABLE
// verdict — but it must NEVER arrive at the fake-closed completed+sync-resolved
// the M4.16 blind ruling granted". The protective half of that (never a fake
// `completed`) is still exactly right and is still asserted below. The other
// half rested on a premise this build refutes:
//
//	$ gopi task update t-bug053-cancel --status pending
//	gopi task update error: task: illegal status transition cancelled → pending
//	  — cancelled is TERMINAL: no status write leaves it (a rejected/unfinished
//	  row is repaired by REISSUE: submit a NEW task and leave this row as history)
//
// `taskAllowedTransitions[cancelled]` is EMPTY, so "reopenable" is false: a
// sync-borne cancel of a pending HUMAN ticket is not a survivable verdict, it
// is the loss of the ticket (reissue mints a new id and abandons this row).
// Measured on the real ledgers the same day, on eight rows whose cancel reason
// literally said "keep THIS side": the kept side was cancelled by the ladder
// within one second and could not be brought back.
//
// So the delta channel now does what the v1 arm in the same file already did
// (terminalConflictRulingTx): the cancel travels as `cancel-was-intent`, the
// pending row STAYS pending, and cancelling local work remains the local
// dispatcher's decision. RowsCancelKept counts it.
func TestSyncDeltaPendingHumanTicketVsCancelledMirror(t *testing.T) {
	newDeltaTestDB(t)
	t.Setenv("GOPI_NODE_ROLE", "server")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	base := float64(time.Now().Unix())
	db, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO tasks (task_id, summary, status, session_id, model, parent_task_id,
		    tags, payload, source, claimed_by, claimed_at, verify_evidence, deliverable,
		    created_at, updated_at, ended_at)
		 VALUES (?, ?, ?, '', '', '', 'bug', ?, 'human', '', NULL, '', '', ?, ?, NULL)`,
		"t-bug053-cancel", "bug052 dispatch db-sync-first", "pending", "dispatch 前缺 db-sync ...",
		base, base); err != nil {
		t.Fatalf("fixture insert: %v", err)
	}
	closeDB()

	// HP mirror of the SAME ticket, cancelled.
	patch := deltaPatch("hp",
		SyncTaskRow{TaskID: "t-bug053-cancel", Summary: "bug052 dispatch db-sync-first", Status: taskStatusCancelled,
			UpdatedAt: int64(base), CreatedAt: int64(base), Source: "auto", Tags: []string{"sync-mirror"}})
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsResolved != 0 {
		t.Fatalf("cancelled-mirror must NOT trigger the completed ruling (RowsResolved=0), got %+v", stats)
	}

	db2, closeDB2, err := taskSyncDB()
	if err != nil {
		t.Fatalf("taskSyncDB re-open: %v", err)
	}
	defer closeDB2()
	row, err := taskByID(context.Background(), db2, "t-bug053-cancel")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if row.Status == taskStatusCompleted {
		t.Fatalf("human ticket FAKE-CLOSED to completed by the cancelled mirror — bug053 must prevent this")
	}
	if row.Status != taskStatusPending {
		t.Fatalf("a peer's cancelled MIRROR must not cancel a pending human ticket on this host (%s) — "+
			"cancelled has no legal exit here, so the ladder would have LOST the ticket; the intent must "+
			"land as the %q tag instead (v1 terminalConflictRulingTx has said so all along)", row.Status, syncTaskCancelIntentTag)
	}
	if stats.RowsCancelKept != 1 {
		t.Fatalf("the kept cancel must be COUNTED, not absorbed silently — got %+v", stats)
	}
	row2, err := taskByID(context.Background(), db2, "t-bug053-cancel")
	if err != nil {
		t.Fatalf("re-read for tags: %v", err)
	}
	if !taskHasTagToken(row2.Tags, syncTaskCancelIntentTag) {
		t.Fatalf("the cancel INTENT must reach this ledger as %q — tags = %q", syncTaskCancelIntentTag, row2.Tags)
	}
}

// TestSyncDeltaRunProvenanceTravels: the run_method/run_cmd columns ride
// the v2 delta wire (devdoc/run-method-provenance.md) — a peer's import
// lands the provenance so its list/show answers "how was this run" like
// the origin node. Fresh-row insert carries both; a forward status move
// adopts them last-non-empty (never clears a filled local value with an
// empty remote).
func TestSyncDeltaRunProvenanceTravels(t *testing.T) {
	pathA := filepath.Join(t.TempDir(), "a-memory.db")
	pathB := filepath.Join(t.TempDir(), "b-memory.db")
	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	base := float64(time.Now().Unix()) - syncTestClockBackstep
	// A stamps a manual run then completes the row (terminal + provenance).
	deltaInsertRaw(t, "t-prov", "provenance run", taskStatusPending, base, base, "")
	if err := TaskStampRun("t-prov", "manual", "gopi task run t-prov"); err != nil {
		t.Fatalf("TaskStampRun: %v", err)
	}
	deltaUpdateStatus(t, "t-prov", taskStatusCompleted, base+5)

	// --- export A → import into B: the fresh terminal row must land with
	// run_method/run_cmd intact.
	patchA, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export A: %v", err)
	}
	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	if _, err := SyncTaskImportDelta(patchA); err != nil {
		t.Fatalf("import into B: %v", err)
	}
	got := syncDeltaRow(t, "t-prov")
	if got == nil {
		t.Fatalf("t-prov missing on B")
	}
	if got.RunMethod != "manual" || got.RunCmd != "gopi task run t-prov" {
		t.Fatalf("B row provenance = %q / %q, want manual / gopi task run t-prov (fresh insert must carry it)", got.RunMethod, got.RunCmd)
	}

	// --- A re-runs the row as worker (newer anchor) and pushes again; B
	// must adopt the NEWER provenance over its filled value (last-non-
	// empty-wins), not keep the stale manual stamp.
	t.Setenv("GOPI_MEMORY_DB", pathA)
	// Re-open in_progress (a fresh execution of the same row).
	deltaUpdateStatus(t, "t-prov", taskStatusInProgress, base+10)
	if err := TaskStampRun("t-prov", "worker", "gopi worker (claim→run round, target t-prov)"); err != nil {
		t.Fatalf("TaskStampRun worker: %v", err)
	}
	deltaUpdateStatus(t, "t-prov", taskStatusCompleted, base+15)
	patchA2, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export A2: %v", err)
	}
	t.Setenv("GOPI_MEMORY_DB", pathB)
	stats2, err := SyncTaskImportDelta(patchA2)
	if err != nil {
		t.Fatalf("import A2 into B: %v", err)
	}
	if len(patchA2.Rows) > 0 {
		t.Logf("A2 stats: %+v (remote row: status=%s updatedAt=%d reason=%q)", stats2, patchA2.Rows[0].Status, patchA2.Rows[0].UpdatedAt, patchA2.Rows[0].Reason)
	} else {
		t.Logf("A2 stats: %+v (empty patch)", stats2)
	}
	if stats2.RowsMigrated == 0 && stats2.RowsAlready == 0 {
		t.Fatalf("A2 import did not merge: %+v", stats2)
	}
	got2 := syncDeltaRow(t, "t-prov")
	if got2.RunMethod != "worker" || got2.RunCmd != "gopi worker (claim→run round, target t-prov)" {
		t.Fatalf("B provenance after re-run = %q / %q, want worker stamp (last-non-empty-wins)", got2.RunMethod, got2.RunCmd)
	}
}

// TestSyncDeltaReasonTravels: the reason column rides the v2 delta wire
// (task-update-reason epic) — a peer's import lands the WHY so its show
// answers "why is this task in this state" like the origin node. Fresh-row
// insert carries it; a forward status move adopts it last-non-empty (never
// clears a filled local value with an empty remote).
func TestSyncDeltaReasonTravels(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	pathA := filepath.Join(t.TempDir(), "a-memory.db")
	pathB := filepath.Join(t.TempDir(), "b-memory.db")
	t.Setenv("GOPI_MEMORY_DB", pathA)
	openDeltaDB(t, pathA)
	base := float64(time.Now().Unix()) - syncTestClockBackstep
	// A completes the row WITH a custom reason (the operator explained why).
	// The fixture stages pending → in_progress via direct SQL (bypassing the
	// gate like deltaUpdateStatus fixtures do), then the real engine path
	// (TaskUpdateReason) performs the legal in_progress → completed move.
	deltaInsertRaw(t, "t-reason", "reason run", taskStatusPending, base, base, "")
	deltaUpdateStatus(t, "t-reason", taskStatusInProgress, base+2)
	if _, err := TaskUpdateReason("t-reason", taskStatusCompleted, "root cause fixed with repro", ""); err != nil {
		t.Fatalf("TaskUpdateReason: %v", err)
	}

	// --- export A → import into B: the fresh terminal row lands with the
	// reason intact.
	patchA, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export A: %v", err)
	}
	t.Setenv("GOPI_MEMORY_DB", pathB)
	openDeltaDB(t, pathB)
	if _, err := SyncTaskImportDelta(patchA); err != nil {
		t.Fatalf("import into B: %v", err)
	}
	got := syncDeltaRow(t, "t-reason")
	if got == nil {
		t.Fatalf("t-reason missing on B")
	}
	if got.Reason != "root cause fixed with repro" {
		t.Fatalf("B reason = %q, want 'root cause fixed with repro' (fresh insert must carry it)", got.Reason)
	}

	// --- A re-runs the row (a bare transition stamps the status DEFAULT,
	// then a custom reason on the close) and pushes again; B must adopt the
	// newer custom reason (last-non-empty-wins).
	// The A1 close above used the real engine clock (time.Now().Unix(),
	// second resolution). This re-close must stamp STRICTLY later — the
	// terminal-tier 后到覆盖 tie-break orders equal-rank peers by anchor,
	// and a same-Unix-second tie is an un-orderable echo (970b7cb lesson).
	// Sleep to the next second boundary, then reopen (the state machine
	// requires completed → in_progress → completed).
	t.Setenv("GOPI_MEMORY_DB", pathA)
	syncTestSleepToNextSecond()
	deltaUpdateStatus(t, "t-reason", taskStatusInProgress, base+10)
	// The rank-guard adopts an equal-tier patch only when its updated_at is
	// strictly later. A1's TaskUpdateReason stamped real now (second
	// granularity) and B imported that stamp — a fast re-run within the same
	// second would tie and the guard would block the newer reason. Sleep one
	// clear second so A2's close stamp is strictly later than B's row.
	time.Sleep(2 * time.Second) //stylegate:allow-sleep second-boundary alignment: the sync delta window is second-granular
	if _, err := TaskUpdateReason("t-reason", taskStatusCompleted, "re-verified green after infra flake", ""); err != nil {
		t.Fatalf("TaskUpdateReason 2: %v", err)
	}
	patchA2, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{})
	if err != nil {
		t.Fatalf("export A2: %v", err)
	}
	t.Setenv("GOPI_MEMORY_DB", pathB)
	stats2, err := SyncTaskImportDelta(patchA2)
	if err != nil {
		t.Fatalf("import A2 into B: %v", err)
	}
	if stats2.RowsMigrated == 0 && stats2.RowsAlready == 0 {
		t.Fatalf("A2 import did not merge: %+v", stats2)
	}
	got2 := syncDeltaRow(t, "t-reason")
	if got2.Reason != "re-verified green after infra flake" {
		t.Fatalf("B reason after re-run = %q, want 're-verified green after infra flake' (last-non-empty-wins)", got2.Reason)
	}
}

// notifyLogHasEvent reports whether ANY delivered JSON payload in the raw
// capture carries the given event name. The capture may hold several
// deliveries (the shared notify funnel emits one per observed move), so a
// test asserting an alarm must scan rather than parse the last line.
func notifyLogHasEvent(t *testing.T, raw []byte, event string) bool {
	t.Helper()
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p taskNotifyPayload
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatalf("payload parse %q: %v", line, err)
		}
		if p.Event == event {
			return true
		}
	}
	return false
}
