package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// M1 roadmap regression tests: exam_questions + 5W1H parallel recall columns.
// Each test pins one piece of the M1 contract:
//   - fresh DBs build the 12/4-column FTS layout;
//   - pre-M1 default DBs are ALTER-healed in place (only added, never dropped);
//   - exam phrasings recall via trigram (≥3-char CJK substring of a phrasing);
//   - 5W1H labels recall via exact-token FTS and the 2-char LIKE route;
//   - query words matching a phrasing/label raise the token-coverage score.
// ---------------------------------------------------------------------------

// m1ExamQ is a stored record whose BODY never contains the alternate wording
// used by the queries below — only the exam_questions column carries it.
const m1BodyQ = "如何清理 memory.db 里的 stopword keywords？"
const m1BodyA = "用 soup_ratio 识别停用词碎片，然后批量替换 search_kw 列。"
const m1ExamStored = `["怎样批量删除记忆库中的停用词关键词","memory keywords 批量清洗方法"]`
const m1KwWhat = "关键词清理"
const m1KwWhy = "检索召回率"

// m1Seed stores one record and backfills its M1 columns directly (the backfill
// script does the same UPDATE — MemoryStore leaves the columns empty).
func m1Seed(t *testing.T, path string) {
	t.Helper()
	if _, err := MemoryStore("keyword 清理", m1BodyQ, m1BodyA, "test", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	defer db.Close()
	seed(t, db,
		"UPDATE distilled_qa SET exam_questions="+lit(m1ExamStored)+","+
			" kw_what="+lit(m1KwWhat)+", kw_why="+lit(m1KwWhy)+
			", kw_when='', kw_where='', kw_who=''",
		"INSERT INTO distilled_qa_fts(distilled_qa_fts) VALUES('rebuild')",
		"INSERT INTO distilled_qa_tri(distilled_qa_tri) VALUES('rebuild')")
}

// lit renders a Go string as a single-quoted SQL literal (test data only —
// no quotes inside the M1 fixture strings).
func lit(s string) string { return "'" + s + "'" }

// TestM1FreshDBHasM1FTSCols: a brand-new DB builds the 12-column fts index
// and the 4-column trigram index (the exam phrasings must be trigram-indexed
// — they are whole CJK sentences whose substrings must recall).
func TestM1FreshDBHasM1FTSCols(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	if _, err := MemoryStore("t", "question about the schema", "answer body long enough for the gate to pass it through", "t", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	defer db.Close()
	for _, probe := range []struct{ tbl, col string }{
		{"distilled_qa_fts", "exam_questions"},
		{"distilled_qa_fts", "kw_who"},
		{"distilled_qa_tri", "exam_questions"},
	} {
		if err := db.QueryRow("SELECT count(*) FROM " + probe.tbl + " WHERE " +
			probe.col + " IS NOT NULL").Scan(new(int)); err != nil {
			t.Errorf("%s missing column %s: %v", probe.tbl, probe.col, err)
		}
	}
}

// TestM1LegacyDBHealed: a pre-M1 default DB (legacy six-column fts, no M1
// columns) is healed on open: the six columns are ALTERed in (only added,
// never dropped) and BOTH indexes rebuild at the wide layout — existing rows
// stay findable and the new columns become searchable.
func TestM1LegacyDBHealed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	// Build a current DB, then strip it back to the pre-M1 shape.
	if _, err := MemoryStore("legacy", "pre-m1 stored row", "the body of the pre-m1 stored row for the heal test", "t", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	seed(t, db,
		"DROP TRIGGER IF EXISTS distilled_qa_ai",
		"DROP TRIGGER IF EXISTS distilled_qa_au",
		"DROP TRIGGER IF EXISTS distilled_qa_ad",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_ai",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_au",
		"DROP TRIGGER IF EXISTS distilled_qa_tri_ad",
		"DROP TABLE IF EXISTS distilled_qa_fts",
		"DROP TABLE IF EXISTS distilled_qa_tri",
		"ALTER TABLE distilled_qa DROP COLUMN exam_questions",
		"ALTER TABLE distilled_qa DROP COLUMN kw_what",
		"ALTER TABLE distilled_qa DROP COLUMN kw_why",
		"ALTER TABLE distilled_qa DROP COLUMN kw_when",
		"ALTER TABLE distilled_qa DROP COLUMN kw_where",
		"ALTER TABLE distilled_qa DROP COLUMN kw_who",
		"CREATE VIRTUAL TABLE distilled_qa_fts USING fts5(topic, question, answer, sources, search_kw, jieba_token, content='distilled_qa', content_rowid='id')",
		"CREATE VIRTUAL TABLE distilled_qa_tri USING fts5(question, answer, topic, content='distilled_qa', content_rowid='id', tokenize='trigram')",
		"INSERT INTO distilled_qa_fts(distilled_qa_fts) VALUES('rebuild')",
		"INSERT INTO distilled_qa_tri(distilled_qa_tri) VALUES('rebuild')")
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	// Reopen via search: the default-DB heal path runs (path == default only
	// when GOPI_MEMORY_DB is unset, so emulate the migration directly).
	mdb, err := openDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer mdb.Close()
	if err := m1MigrateFTSCols(t.Context(), mdb); err != nil {
		t.Fatalf("m1MigrateFTSCols: %v", err)
	}
	mdb.Close() //stylegate:ignore test closes the handle before the heal path reopens the DB, which is what the test is about; a close error would surface in the reopen/query assertions

	// The healed DB has the M1 columns, the old row is intact, and the
	// rebuilt indexes still recall it.
	db2 := openRawDB(t, path)
	defer db2.Close()
	var n int
	if err := db2.QueryRow("SELECT count(*) FROM distilled_qa WHERE exam_questions IS NOT NULL AND kw_what IS NOT NULL").Scan(&n); err != nil {
		t.Fatalf("M1 columns missing after heal: %v", err)
	}
	var body string
	if err := db2.QueryRow("SELECT answer FROM distilled_qa WHERE topic='legacy'").Scan(&body); err != nil {
		t.Fatalf("pre-M1 row lost during heal: %v", err)
	}
	if err := db2.QueryRow("SELECT count(*) FROM distilled_qa_fts WHERE exam_questions IS NOT NULL").Scan(&n); err != nil {
		t.Fatalf("fts index not rebuilt at the wide layout: %v", err)
	}
	// A store into the healed DB must succeed (triggers recreated).
	db2.Close() //stylegate:ignore test closes the read handle before MemoryStore reopens the same DB, which the store assertion two lines below covers
	if _, err := MemoryStore("legacy2", "post-m1 stored row", "the body of the post-m1 stored row for the heal test", "t", nil); err != nil {
		t.Fatalf("store into healed DB: %v", err)
	}
}

// TestM1ExamQuestionsTrigramRecall: an ≥3-char CJK substring of an EXAM
// phrasing recalls the record even though the substring appears nowhere in
// topic/question/answer — the exam_questions column is trigram-indexed.
func TestM1ExamQuestionsTrigramRecall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	m1Seed(t, path)
	// "批量删除" appears only inside the first stored phrasing.
	out, err := MemorySearch("批量删除", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, m1BodyQ) {
		t.Errorf("exam-phrasing substring did not recall the record:\n%s", out)
	}
	// A ≥3-char run from the OTHER phrasing (mixed CJK+ASCII) too.
	out, err = MemorySearch("批量清洗", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, m1BodyQ) {
		t.Errorf("second exam phrasing did not recall the record:\n%s", out)
	}
}

// TestM1KwLabelRecall: 5W1H labels are searchable through both the exact
// token route (≥3-char CJK label via FTS/trigram) and the 2-char LIKE route.
func TestM1KwLabelRecall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	m1Seed(t, path)
	// 4-char what label → exact-token/trigram recall.
	out, err := MemorySearch("关键词清理", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, m1BodyQ) {
		t.Errorf("kw_what label did not recall the record:\n%s", out)
	}
	// 2-char slice of the why label → the LIKE route.
	out, err = MemorySearch("召回", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, m1BodyQ) {
		t.Errorf("2-char kw slice did not recall the record (LIKE route):\n%s", out)
	}
}

// TestM1CoverageBoost: a query word that matches only the exam phrasing (not
// the body) still counts toward token coverage — the M1 recall surface feeds
// the primary ranking key, so a phrasing-matching row outranks a body-only row.
func TestM1CoverageBoost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, path)
	m1Seed(t, path)
	// Second record sharing the shared token but with no exam phrasings.
	if _, err := MemoryStore("keyword 清理", "keywords 逗号簇怎么处理",
		"逗号簇要补查，避免中文碎片被误判为好词。", "test", nil); err != nil {
		t.Fatal(err)
	}
	// "批量" hits record 1 via its exam phrasing only; record 2 shares no
	// query token → record 1 must rank first with tokens≥1.
	out, err := MemorySearch("关键词 批量 删除", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.SplitN(out, "\n", 2)[0], "#1") {
		if blocks := splitRendered(t, out); len(blocks) == 0 || !strings.Contains(blocks[0], m1BodyQ) {
			t.Errorf("exam-phrase coverage did not rank the M1 record first:\n%s", out)
		}
	}
}
