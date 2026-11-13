package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrewWorkflowRoutingE2E — epic acceptance ①: a session containing a
// workflow (kind=workflow judge verdict) brews into a procedural row; the
// declarative lesson still lands in distilled_qa. Full pipeline with a
// stub LLM: judge reply #1 = one workflow lesson, #2 = one QA lesson;
// distill replies follow in call order.
func mn(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestBrewWorkflowRoutingE2E(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_HOME", dir)

	stub := &stubBrewClient{replies: []string{
		// judge reply: one workflow lesson + one plain lesson (one chunk)
		`{"lessons":[{"kind":"workflow","topic":"测试失败修复流程","evidence":"先 go build 看错误，再修代码，再跑 test 验证"},{"kind":"lesson","topic":"FTS 重建","evidence":"external-content FTS needs rebuild after trigger change"}]}`,
		// distill workflow
		`{"name":"测试失败修复流程","trigger":"when a build or test failure appears","preconditions":"repo writable","steps":["run go build ./...","fix the first error","run go test to verify"],"tools_used":["bash","edit"],"keywords":["测试修复,test fix,workflow","错误定位,error triage"]}`,
		// distill lesson
		`{"question":"FTS external-content 表什么时候需要重建? When to rebuild the FTS index?","answer":"触发器变更后必须 rebuild external-content FTS 表，否则索引与基础表脱节。用 INSERT INTO fts(fts) VALUES('rebuild')。","keywords":["fts rebuild","索引重建","trigger 变更","external content","schema repair","FTS5 external"]}`,
	}}

	path := writeSessionJSONL(t, dir, "s1.jsonl",
		map[string]interface{}{"type": "session_start", "sessionId": "e2e", "model": "stub"},
		map[string]interface{}{"type": "user", "message": map[string]interface{}{"role": "user", "content": "workflow practice: fix test failures then document"}},
		map[string]interface{}{"type": "assistant", "message": map[string]interface{}{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": "I ran go build, fixed the error, ran go test — repeatable loop for test failures."},
			},
		}},
	)

	opts := RunBrewOptions{SessionPaths: []string{path}, SessionsDir: dir}
	stats, err := RunBrew(context.Background(), stub, opts)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Stored != 2 {
		t.Fatalf("brew stored=%d (want 2: 1 procedural + 1 QA), errors=%v", stats.Stored, stats.Errors)
	}

	// ① the workflow landed in the procedural table.
	db, err := openStoreDB(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name, sources string
	if err := db.QueryRow(`SELECT name, COALESCE(sources,'') FROM procedural`).Scan(&name, &sources); err != nil {
		t.Fatalf("procedural row missing: %v", err)
	}
	if name != "测试失败修复流程" || !strings.Contains(sources, BrewSourceTag) || !strings.Contains(sources, proceduralSourceTag) {
		t.Fatalf("procedural row name=%q sources=%q", name, sources)
	}
	// ② the lesson landed in distilled_qa.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM distilled_qa`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("distilled_qa rows=%d err=%v (want 1)", n, err)
	}

	// ③ memory_search merges the procedural hit into the QA result — also
	// when the QA channel is empty (the workflow is the only knowledge).
	out, err := MemorySearch("测试失败修复流程 test failure workflow", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[procedural workflows]") || !strings.Contains(out, "测试失败修复流程") {
		t.Fatalf("memory_search output lacks the procedural section:\n%s", out[:mn(len(out), 400)])
	}
}
