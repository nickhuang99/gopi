package tools

// M4.8 hardening tests — the PA double-submit leak class (epic):
//   leak ① briefings embed a dispatch timestamp/path → the exact
//          sha256(summary+NUL+payload) differs → the old gate let a twin
//          through;
//   leak ② the summary changed by one word → any hash differs;
//   audit  dedup-scan must FIND planted twins and stay quiet on clean ledgers.
// Every test here doubles as the regression guard for the three-gate ladder
// (exact → canonical → similar).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// dedupTestDB opens a throwaway ledger with one live row and returns the
// path (import tests re-open through SetMemoryDBPath-free helpers — the
// sync probes take a path only via taskSyncDB()'s global, so tests write
// through TaskSubmit with the global redirected).
func dedupTestDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "dedup-test.db")
}

// withTasksDB redirects the global ledger path for the test's duration.
// The tasks layer resolves its DB through memoryDBPath() (GOPI_MEMORY_DB >
// legacy alias > default), so the test drives the env var and restores it.
func withTasksDB(t *testing.T, path string) {
	t.Helper()
	t.Setenv(memoryDBEnv, path)
}

func TestCanonicalStripsPathsTimestampsWhitespace(t *testing.T) {
	a := syncTaskCanonText("审查 泄漏。范围 /home/user/work/GoPi 派发 1735689600 \n\t报告  2026-08-31T13:00:00Z")
	b := syncTaskCanonText("审查 泄漏。范围 /home/user/work/GoPi 派发 1735776000 \n 报告   12:00:00")
	// Same path (same node dispatches from the same tree), different
	// timestamps and whitespace — the canonical forms must converge.
	if a == "" {
		t.Fatal("canonical form collapsed to empty")
	}
	for _, s := range []string{a, b} {
		if strings.ContainsAny(s, "0123456789") {
			t.Fatalf("timestamp survived canonicalization: %q", s)
		}
		if strings.Contains(s, "/") {
			t.Fatalf("path survived canonicalization: %q", s)
		}
	}
	if a != b {
		t.Fatalf("canonical forms diverge:\nA=%q\nB=%q", a, b)
	}
	// And a DIFFERENT path's basename survives — the residual noise the
	// similarity gate owns (asserted, not hidden).
	c := syncTaskCanonText("审查 泄漏。范围 /tmp/other 派发 1735689600 \n 报告   12:00:00")
	if !strings.Contains(c, "other") || strings.Contains(c, "gopi") {
		t.Fatalf("expected the surviving basename in %q", c)
	}
}

func TestCanonHashTwinSubmissionsMatch(t *testing.T) {
	// Leak ①: same brief, different dispatch timestamp + embedded path.
	a := "审查 M4.8 导入去重泄漏。审计范围 /home/user/work/GoPi，派发时间 1735689600，完成后写报告。"
	b := "审查 M4.8 导入去重泄漏。审计范围 /home/user/GoPi，派发时间 1735776000，完成后写报告。"
	if syncTaskWorkHash(a, "") == syncTaskWorkHash(b, "") {
		t.Fatal("exact hashes should differ (that is the leak)")
	}
	if syncTaskCanonHash(a, "") != syncTaskCanonHash(b, "") {
		t.Fatalf("canonical hashes should match for twin briefs:\nA=%q\nB=%q",
			syncTaskCanonText(a), syncTaskCanonText(b))
	}
}

func TestCanonHashBelowMinLenStaysExact(t *testing.T) {
	// Tiny strings are not canonicalized (too little signal to strip).
	if syncTaskCanonText("ab") != "ab" {
		t.Fatal("short strings must bypass canonicalization")
	}
}

func TestThreeGateLadderCatchesBothLeaks(t *testing.T) {
	twinA := liveDedupRow{taskID: "t-a",
		summary: "审查 M4.8 导入去重泄漏",
		payload: "审查 M4.8 导入去重泄漏。审计范围 /home/user/work/GoPi，派发时间 1735689600，完成后写报告。"}
	cases := []struct {
		name string
		in   liveDedupRow
		gate string
	}{
		{"leak1 timestamp+path", liveDedupRow{taskID: "t-b",
			summary: "审查 M4.8 导入去重泄漏",
			payload: "审查 M4.8 导入去重泄漏。审计范围 /home/user/GoPi，派发时间 1735776000，完成后写报告。"},
			"canonical"},
		{"leak2 one-word summary edit", liveDedupRow{taskID: "t-c",
			summary: "审查 M4.8 导入去重的泄漏点",
			payload: "审查 M4.8 导入去重的泄漏点。检查审计范围与派发时间，完成后写审查报告并归档。"},
			"similar"},
	}
	for _, tc := range cases {
		gate, twin, ok := syncTaskDedupDecide(tc.in, []liveDedupRow{twinA})
		if !ok {
			t.Fatalf("%s: ladder missed the twin", tc.name)
		}
		if gate != tc.gate {
			t.Fatalf("%s: gate = %q, want %q", tc.name, gate, tc.gate)
		}
		if twin != "t-a" {
			t.Fatalf("%s: twin = %q, want t-a", tc.name, twin)
		}
	}
}

func TestLadderKeepsUnrelatedWorkApart(t *testing.T) {
	a := liveDedupRow{taskID: "t-1", summary: "修复 bash 工具超时",
		payload: "修复 bash 工具 120s 超时误杀长任务的问题并补回归测试"}
	b := liveDedupRow{taskID: "t-2", summary: "渲染验证报告",
		payload: "用像素级 IoU 对比验证 text2img 渲染质量并输出报告"}
	if _, _, ok := syncTaskDedupDecide(a, []liveDedupRow{b}); ok {
		t.Fatal("unrelated work must not be judged twins")
	}
}

func TestImportDedupHardenedEndToEnd(t *testing.T) {
	// UPDATED for 01a0fea1 ③ (identity narrowing): the ladder still DETECTS
	// both leaks on one patch — that is the audit value, and it stays — but
	// it now REPORTS them (RowsContentWarn + Errors line) instead of SKIPPING
	// the rows. Two submissions of the same brief under different task_ids
	// are two legitimate work items; the payload-hash era silently dropped
	// the second. Master's contract was "queue exactly the first copy"
	// (RowsImported=1, RowsDuped=2); the epic's is "queue both, warn about
	// the twin".
	withTasksDB(t, dedupTestDB(t))
	patch := &SyncTaskPatch{
		Version: syncTaskPatchVersion,
		Node:    "peerbox",
		Rows: []SyncTaskRow{
			{TaskID: "t-a", Summary: "审查 M4.8 导入去重泄漏",
				Payload: "审查 M4.8 导入去重泄漏。审计范围 /home/user/work/GoPi，派发时间 1735689600，完成后写报告。",
				Source:  "auto"},
			{TaskID: "t-b", Summary: "审查 M4.8 导入去重泄漏",
				Payload: "审查 M4.8 导入去重泄漏。审计范围 /home/user/GoPi，派发时间 1735776000，完成后写报告。",
				Source:  "auto"},
			{TaskID: "t-c", Summary: "审查 M4.8 导入去重的泄漏点",
				Payload: "审查 M4.8 导入去重的泄漏点。检查审计范围与派发时间，完成后写审查报告并归档。",
				Source:  "auto"},
		},
	}
	path := filepath.Join(t.TempDir(), "twins.json")
	if err := WriteSyncTaskPatch(patch, path); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	stats, err := SyncTaskImport(path)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if stats.RowsImported != 3 {
		t.Fatalf("RowsImported = %d, want 3 (every row lands under its own task_id): %+v",
			stats.RowsImported, stats)
	}
	if stats.RowsContentWarn != 2 {
		t.Fatalf("RowsContentWarn = %d, want 2 (leaks ① and ② both detected and reported): %+v", stats.RowsContentWarn, stats)
	}
	for _, e := range stats.Errors {
		t.Logf("dedup audit line: %s", e)
	}
}

func TestImportReRunIsIdempotent(t *testing.T) {
	withTasksDB(t, dedupTestDB(t))
	patch := &SyncTaskPatch{
		Version: syncTaskPatchVersion,
		Node:    "peerbox",
		Rows: []SyncTaskRow{
			{TaskID: "t-a", Summary: "审查 M4.8 导入去重泄漏",
				Payload: "审查 M4.8 导入去重泄漏。审计范围 /home/user/work/GoPi，派发时间 1735689600，完成后写报告。",
				Source:  "auto"},
		},
	}
	path := filepath.Join(t.TempDir(), "one.json")
	if err := WriteSyncTaskPatch(patch, path); err != nil {
		t.Fatalf("write patch: %v", err)
	}
	if _, err := SyncTaskImport(path); err != nil {
		t.Fatalf("first import: %v", err)
	}
	stats, err := SyncTaskImport(path)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if stats.RowsImported != 0 {
		t.Fatalf("re-import queued %d rows, want 0 (idempotency contract): %+v", stats.RowsImported, stats)
	}
	// UPDATED for 01a0fea1 ③: the row is present under its own id, so the
	// honest answer is `already` — master answered `duped` because the
	// payload-hash gate ran first and skipped the row.
	if stats.RowsAlready != 1 {
		t.Fatalf("re-import already = %d, want 1: %+v", stats.RowsAlready, stats)
	}
}

func TestDedupScanFindsPlantedTwins(t *testing.T) {
	dbPath := dedupTestDB(t)
	withTasksDB(t, dbPath)
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	// Two live rows carrying the same work behind different embeds.
	for _, row := range [][2]string{
		{"t-a", "审查 M4.8 导入去重泄漏|审查 M4.8 导入去重泄漏。审计范围 /home/user/work/GoPi，派发时间 1735689600，完成后写报告。"},
		{"t-b", "审查 M4.8 导入去重泄漏|审查 M4.8 导入去重泄漏。审计范围 /home/user/GoPi，派发时间 1735776000，完成后写报告。"},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO tasks (task_id, summary, payload, status, tags, source, priority, created_at, updated_at)
			 VALUES (?, ?, ?, 'pending', '', 'auto', 5, 1, 1)`,
			row[0], row[1][:len("审查 M4.8 导入去重泄漏")], row[1][len("审查 M4.8 导入去重泄漏")+1:]); err != nil {
			t.Fatalf("seed %s: %v", row[0], err)
		}
	}
	stats, err := TaskDedupScan(dbPath, true)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stats.Scanned != 2 {
		t.Fatalf("Scanned = %d, want 2", stats.Scanned)
	}
	if len(stats.Pairs) != 1 {
		t.Fatalf("Pairs = %d, want 1: %+v", len(stats.Pairs), stats.Pairs)
	}
	p := stats.Pairs[0]
	if p.TaskIDA != "t-a" || p.TaskIDB != "t-b" {
		t.Fatalf("pair = %s/%s, want t-a/t-b (deterministic order)", p.TaskIDA, p.TaskIDB)
	}
	if p.Gate != "canonical" {
		t.Fatalf("gate = %q, want canonical", p.Gate)
	}
}

func TestDedupScanQuietOnCleanLedger(t *testing.T) {
	dbPath := dedupTestDB(t)
	withTasksDB(t, dbPath)
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, payload, status, tags, source, priority, created_at, updated_at)
		 VALUES ('t-1', '修复 bash 超时', '修复 bash 工具 120s 超时误杀长任务的问题并补回归测试', 'pending', '', 'auto', 5, 1, 1)`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	stats, err := TaskDedupScan(dbPath, true)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(stats.Pairs) != 0 {
		t.Fatalf("clean ledger reported %d pair(s): %+v", len(stats.Pairs), stats.Pairs)
	}
}
