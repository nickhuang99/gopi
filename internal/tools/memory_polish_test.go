package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Self-evolution polish features (MEMORY-GO-POLISH): CJK recall, garbage
// gate, similarity dedup, trigger-less FTS sync.
// ---------------------------------------------------------------------------

// TestMemorySearchCJKHit: storing a Chinese entry then querying with a
// Chinese fragment that was previously an empty hit now matches. This covers
// feature 1 (CJK retrieval via trigram index + LIKE fallback).
func TestMemorySearchCJKHit(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("网络测试", "如何排查网络测试环境连接问题？",
		"使用抓包工具检查网络测试环境的连接，重点排查防火墙和代理设置。", "smoke", nil); err != nil {
		t.Fatalf("seed CJK store: %v", err)
	}
	// "网络测试" (4-char CJK) was an empty hit before the trigram route.
	for _, q := range []string{"网络测试", "网络测试环境", "防火墙"} {
		out, err := MemorySearch(q, 3, "")
		if err != nil {
			t.Fatalf("CJK search %q: %v", q, err)
		}
		if !strings.Contains(out, "网络测试") || !strings.Contains(out, "防火墙") {
			t.Errorf("CJK query %q did not hit the Chinese entry:\n%s", q, out)
		}
	}
}

// TestMemorySearchCJKTwoCharSubstring: a 2-char CJK fragment (below the
// trigram tokenizer's 3-char lower bound) is recalled via the LIKE fallback.
func TestMemorySearchCJKTwoCharSubstring(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("部署", "如何自动化应用部署流程？",
		"用 CI 流水线自动构建并部署应用到生产服务器的流程", "ci", nil); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	// "部署" appears in both question and answer; 2-char gram → LIKE route.
	out, err := MemorySearch("部署", 3, "")
	if err != nil {
		t.Fatalf("2-char CJK search: %v", err)
	}
	if !strings.Contains(out, "部署") {
		t.Errorf("2-char CJK query did not hit:\n%s", out)
	}
}

// TestMemoryStoreGarbageRejected: the _is_garbage gate (feature 3) refuses
// non-knowledge candidates — a meta-ingestion copy, a garbage prefix, and a
// no-meaningful-content fragment — before any write.
func TestMemoryStoreGarbageRejected(t *testing.T) {
	setMemoryDB(t)
	cases := []struct{ q, a string }{
		// rule 1: garbage prefix (hermes async-delegation artifact)
		{"[Background process] started the build", "this is an async delegation leftover with no knowledge"},
		// rule 2: meta-ingestion — memory_search result copied as knowledge
		{"use the memory_search tool to get VPN lessons — 检索结果", "this is a copy of search results not distilled knowledge bad"},
		// rule 3: no meaningful (CJK/ASCII alnum) content
		{"。。。", "。。。"},
	}
	for _, c := range cases {
		if out, err := MemoryStore("garbage", c.q, c.a, "", nil); err == nil || !strings.Contains(err.Error(), "garbage") {
			t.Errorf("garbage candidate (q=%q) should be rejected, got err=%v out=%q", c.q, err, out)
		}
	}
	// A legitimate distilled lesson is still accepted after the gate.
	if _, err := MemoryStore("vpn", "what mtu avoids vpn drops under load?", "raise the mtu and disable tso offload for stable vpn", "net", nil); err != nil {
		t.Errorf("legitimate lesson should pass the gate: %v", err)
	}
}

// TestMemoryStoreSimilarityDedup: storing the SAME lesson again — re-worded on
// both the question and the answer — returns the existing id with deduped=true.
//
// The fixture used to pass by a different mechanism: the OLD screen compared the
// incoming question against the existing ANSWER[:300] as one string, and this
// fixture's answers were deliberately echo-shaped, so it scored 0.553 across
// fields (measured). Real rows are not echo-shaped — the epic's own twins scored
// 0.181/0.219 on that comparison and were never merged — so the screen now
// compares like with like (topic+question+answer body, both sides), with the
// 5W1H breadcrumbs as the second facet (see findSameLessonDuplicate). The
// fixture follows the real shape: both rows carry breadcrumbs, which is the
// second facet's precondition.
func TestMemoryStoreSimilarityDedup(t *testing.T) {
	setMemoryDB(t)
	out, err := MemoryStore("docker", "how to rebuild docker images",
		"to rebuild a container image you run the docker build command in the directory that holds the Dockerfile, and the layer cache is what makes the second build fast: keep the dependency installation step above the source copy so a change to the source does not invalidate the cached dependency layer, and pass the cache-from flag when the builder runs on a fresh machine.", "src-a",
		[]string{"docker build", "rebuild docker images", "image cache"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "deduped") {
		t.Fatalf("first store should not be deduped: %s", out)
	}
	// The same lesson, re-worded, filed under the same breadcrumbs → dedup id=1.
	out2, err := MemoryStore("docker", "how do i go about rebuilding docker images",
		"the same build instruction written differently: run the docker build command from the directory that holds the Dockerfile to rebuild the image, and keep the dependency install step above the copy of the sources so that editing a source file does not throw away the cached dependency layer and force the dependency installation to run again from the start.", "src-b",
		[]string{"docker build", "rebuild docker images", "docker image cache"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "deduped") || !strings.Contains(out2, "id=1") {
		t.Errorf("similar store should dedup into id=1, got %q", out2)
	}
	// A truly distinct entry is inserted fresh (deduped=false).
	out3, err := MemoryStore("docker", "what is docker networking mode",
		"docker supports bridge host and overlay networking for container isolation", "net", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out3, "deduped") {
		t.Errorf("distinct store should not be deduped: %s", out3)
	}
}

// TestMemoryStoreFTSyncNoTrigger: on a DB whose FTS keep-in-sync triggers
// were removed (simulating a hermes production copy), a store still leaves
// the entry findable by search — the explicit FTS sync fallback (feature 5).
func TestMemoryStoreFTSyncNoTrigger(t *testing.T) {
	path := setMemoryDB(t)
	// Seed a row, then drop the FTS triggers to simulate a trigger-less host DB.
	if _, err := MemoryStore("backup", "how to take a db backup", "run sqlite3 .backup to take a consistent copy of the database", "", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	seed(t, db, "DROP TRIGGER IF EXISTS distilled_qa_ai",
		"DROP TRIGGER IF EXISTS distilled_qa_au",
		"DROP TRIGGER IF EXISTS distilled_qa_ad",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_ai",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_au",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_ad")
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// Store another entry with no triggers present → manual FTS sync kicks in.
	if _, err := MemoryStore("backup", "how to restore a db backup", "use sqlite3 .restore to bring back a previously taken database copy", "ops", nil); err != nil {
		t.Fatalf("store on trigger-less DB: %v", err)
	}
	out, err := MemorySearch("restore backup", 5, "")
	if err != nil {
		t.Fatalf("search after trigger-less store: %v", err)
	}
	if !strings.Contains(out, "restore a db backup") {
		t.Errorf("search did not find the trigger-less-stored row (FTS sync fallback missing):\n%s", out)
	}
}

// TestMigrateDefaultTriggers: a DB created before the trigram index polish has
// no distilled_qa_tri table. migrateDefaultTriggers adds it, installs the
// keep-in-sync triggers, and backfills the index from existing rows so CJK
// substring recall works on pre-existing content.
func TestMigrateDefaultTriggers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	// Build a legacy schema: base table + fts virtual table, NO trigram index.
	db := openRawDB(t, path)
	seed(t, db,
		`CREATE TABLE distilled_qa (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic TEXT NOT NULL, question TEXT NOT NULL, answer TEXT NOT NULL,
			sources TEXT DEFAULT '', confidence REAL DEFAULT 0.5,
			tier TEXT DEFAULT 'warm', search_kw TEXT DEFAULT '', jieba_token TEXT DEFAULT '',
			lesson_type TEXT DEFAULT '', when_at TEXT DEFAULT '', distilled_at REAL, last_retrieved REAL
		)`,
		`CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(
			topic, question, answer, sources, search_kw, jieba_token
		)`,
	)
	// Seed an existing Chinese row (not yet indexed by trigram).
	seed(t, db, `INSERT INTO distilled_qa(topic, question, answer, confidence, tier)
		VALUES('旧库','旧库的网络测试环境', '用网关处理旧库的网络测试环境', 0.85, 'warm')`)
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	ctx := context.Background()
	if err := migrateDefaultTriggers(ctx, path); err != nil {
		t.Fatalf("migrateDefaultTriggers: %v", err)
	}

	// Verify the trigram index exists on a FRESH connection (the original raw
	// handle was closed above).
	check := openRawDB(t, path)
	defer check.Close()
	ok, _ := tableExists(ctx, check, "distilled_qa_tri") //stylegate:ignore existence probe; an error is read as 'absent' and the caller heals the table
	if !ok {
		t.Fatal("migrateDefaultTriggers did not create distilled_qa_tri")
	}
	// t.Setenv — NOT the os.Setenv/os.Unsetenv pair this used to be. The
	// deferred os.Unsetenv threw the SUITE-WIDE ledger pin away for every test
	// that ran after this one in the same binary, which is exactly the hole a
	// fixture row escaped through into the operator's real ledger
	// (devdoc/reclaim-fossil-20260915.md). t.Setenv restores the pin, not "".
	t.Setenv(memoryDBEnv, path)
	out, err := MemorySearch("网络测试", 3, "")
	if err != nil {
		t.Fatalf("search after migration: %v", err)
	}
	if !strings.Contains(out, "旧库") {
		t.Errorf("migration did not make pre-existing CJK content recallable:\n%s", out)
	}
}

// TestMemorySearchCJKOnLegacyLikeDB: a DB that never gets the trigram index
// (custom path, no migration) still recalls CJK fragments via the LIKE route,
// so pre-polish custom DBs are not left empty on Chinese queries.
func TestMemorySearchCJKOnLegacyLikeDB(t *testing.T) {
	path := setMemoryDB(t)
	// Prime the schema by storing once, then drop the trigram index + triggers
	// to simulate a custom legacy DB with no trigram route.
	if _, err := MemoryStore("部署", "自动化应用部署流程", "用 CI 流水线自动构建并部署应用到服务器", "", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	seed(t, db, "DROP TABLE IF EXISTS distilled_qa_tri",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_ai",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_au",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_ad")
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	out, err := MemorySearch("部署", 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "部署") {
		t.Errorf("legacy (no trigram) DB should still recall '部署' via LIKE:\n%s", out)
	}
}
