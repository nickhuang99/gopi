package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// procTestDB isolates the DB (GOPI_MEMORY_DB + GOPI_HOME) — syncTestDB pattern.
func procTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_HOME", dir)
	return dbPath
}

// TestProceduralStoreAndSearch — the epic acceptance ②: a stored workflow is
// retrievable by name, trigger keyword (CJK), and via the tool-merge path.
func TestProceduralStoreAndSearch(t *testing.T) {
	procTestDB(t)

	res, err := ProceduralStore(&ProceduralWorkflow{
		Name:          "读错误日志→定位→修复→验证",
		Trigger:       "遇到编译或测试报错需要修复时",
		Preconditions: "仓库可写",
		Steps: []ProceduralStep{
			{Action: "run", Detail: "go build ./... 2>&1 | head -20"},
			{Action: "edit", Detail: "定位首个错误并修复"},
			{Action: "verify", Detail: "go test ./... -run TestX"},
		},
		ToolsUsed: []string{"bash", "edit"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "stored procedural workflow") {
		t.Fatalf("unexpected store result: %s", res)
	}

	// Name keyword (ASCII-ish unique substring) via exact-token route.
	for _, q := range []string{"读错误日志 定位 修复 验证", "错误日志 修复", "go build 错误"} {
		out, err := ProceduralSearch(q, 5)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if !strings.Contains(out, "读错误日志→定位→修复→验证") {
			t.Fatalf("search %q missed the workflow:\n%s", q, out)
		}
	}
}

// TestProceduralDedup — epic acceptance ③: same name merges (version bump,
// no new row); a similar name+trigger merges too; a different workflow inserts.
func TestProceduralDedup(t *testing.T) {
	procTestDB(t)
	mk := func(name string) *ProceduralWorkflow {
		return &ProceduralWorkflow{
			Name:    name,
			Trigger: "网络服务异常需要重启时",
			Steps:   []ProceduralStep{{Action: "run", Detail: "systemctl restart nginx"}},
		}
	}
	if _, err := ProceduralStore(mk("重启网络服务"), ""); err != nil {
		t.Fatal(err)
	}
	// Same name → deduped, version bumped.
	res, err := ProceduralStore(mk("重启网络服务"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "deduped") || !strings.Contains(res, "version bumped to 2") {
		t.Fatalf("same-name store should dedup+version-bump, got: %s", res)
	}
	// Similar name (same workflow, reordered words: 重启网络服务 vs
	// 网络服务重启 — trigram Jaccard of the two names is 1/3, so the
	// CJK-flow threshold for name-only comparison is 0.3, below the QA
	// channel's 0.5 because a workflow's name is short and reordering
	// halves the shared grams; a genuinely different flow ("清理构建缓存")
	// shares almost no grams and stays far below it.
	res, err = ProceduralStore(mk("网络服务重启"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "deduped") {
		t.Fatalf("similar name should merge, got: %s", res)
	}
	// Count rows: still exactly 1.
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM procedural`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("dedup failed: %d rows (want 1)", n)
	}
	// Distinct workflow inserts.
	if _, err := ProceduralStore(mk("清理构建缓存"), ""); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM procedural`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("distinct workflow should insert: %d rows (want 2)", n)
	}
}

// TestProceduralSourceTagging — every store tags sources with the
// procedural tag (+ caller tags) so the export gate can scope it.
func TestProceduralSourceTagging(t *testing.T) {
	procTestDB(t)
	if _, err := ProceduralStore(&ProceduralWorkflow{
		Name: "tagged flow", Trigger: "when build fails",
		Steps: []ProceduralStep{{Action: "run", Detail: "go build ./..."}},
	}, BrewSourceTag); err != nil {
		t.Fatal(err)
	}
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sources string
	if err := db.QueryRow(`SELECT COALESCE(sources,'') FROM procedural WHERE name='tagged flow'`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"procedural", BrewSourceTag} {
		if !strings.Contains(","+sources+",", ","+want+",") {
			t.Fatalf("sources %q missing tag %q", sources, want)
		}
	}
}

// TestProceduralSearchEmpty — clean no-hit message, not an error.
func TestProceduralSearchEmpty(t *testing.T) {
	procTestDB(t)
	out, err := ProceduralSearch("完全无关的查询词组", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No procedural workflow found") {
		t.Fatalf("expected clean empty result, got: %s", out)
	}
	// NOTE (2026-09-15, devdoc/reclaim-fossil-20260915.md): this test used to
	// end with `_ = os.Unsetenv("GOPI_MEMORY_DB")` — the ledger pin's exact
	// opposite. procTestDB above already uses t.Setenv, so the ledger is
	// restored for every LATER test in this binary by the testing package; the
	// extra Unsetenv instead threw the suite-wide pin away and left every
	// subsequent test resolving the operator's REAL ledger (the append-only
	// file t-stale-sibling leaked a row into). Unsetting a safety pin is never
	// cleanup — it is the leak.
}
