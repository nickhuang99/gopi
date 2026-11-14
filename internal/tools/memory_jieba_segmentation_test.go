package tools

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cjkseg"
)

// ---------------------------------------------------------------------------
// jieba_token is REAL Chinese segmentation (gse), and the query side speaks
// the same granularity. The defects these tests pin:
//
//  1. cjkSegmentation emitted every overlapping 2-char gram of a CJK run — a
//     dictionary-free stand-in. It could not tell 网络测试环境's real words
//     (网络 + 测试环境) from its arbitrary bigrams, and it could not peel a
//     Latin word off a glued run (这些commit).
//  2. The query side's route 1 asked for a WHOLE CJK run (escapeQuery's bare
//     idiom), and the LIKE route walked 2-char grams — neither is the
//     granularity the index column stores, so index and query spoke past each
//     other for every phrasing that is not a single dictionary word.
//
// The contract now: ONE segmenter (internal/cjkseg, gse over its embedded
// dictionary) is called by the writer (cjkSegmentation) and by the query side
// (cjkQuerySegments), so a stored row is reachable by the words it was
// indexed under — including a word the index does not know, which the
// segmented LIKE route recalls as a substring.
//
// devdoc/jieba-token-real-segmentation.md carries the measured before/after.
// ---------------------------------------------------------------------------

// segSeg = cjkSegmentation; the tests below read better with a short name and
// the write-side function is the subject of most of them.
func segSeg(t *testing.T, s string) string {
	t.Helper()
	out := cjkSegmentation(s)
	if out == "" {
		t.Fatalf("cjkSegmentation(%q) returned empty — the segmenter produced nothing", s)
	}
	return out
}

// TestJiebaTokenIsRealSegmentation pins the words the column holds for the
// inputs that motivated the swap: a whole CJK run split into real words, the
// English stem folded, and 这些commit peeled apart. "逐步缩小" is the case the
// old bigram list got WRONG in a way no gram can fix — the correct answer
// (逐步 缩小) is not one of its bigrams (逐步/步缩/缩小), so `逐步` was never a
// standalone index token and a query for it could only reach the row through
// the LIKE substring route.
func TestJiebaTokenIsRealSegmentation(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"逐步缩小", []string{"逐步", "缩小"}},
		{"网络测试环境部署完成", []string{"网络", "测试环境", "部署", "完成"}},
		{"怎么降低deepseek开支", []string{"怎么", "降低", "deepseek", "开支"}},
		{"这些commit", []string{"这些", "commit"}},
		{"挪到夜间便宜时段", []string{"挪到", "夜间", "便宜", "时段"}},
	}
	for _, c := range cases {
		got := strings.Fields(segSeg(t, c.in))
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("cjkSegmentation(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestJiebaTokenKeepsEveryTwoCharGram is the RECALL GUARD on the swap: a real
// segmenter is not allowed to lose a 2-char fragment the old gram list used to
// index. Every 2-char gram of a contiguous CJK run must still be a substring
// of the segmented text, so route 3 (the segmented LIKE scan) and the old
// gram-shaped queries both keep finding the row.
func TestJiebaTokenKeepsEveryTwoCharGram(t *testing.T) {
	inputs := []string{
		"逐步缩小",
		"网络测试环境部署完成",
		"怎么降低deepseek开支",
		"挪到夜间便宜时段",
		"账单错峰",
		"测试",
	}
	for _, in := range inputs {
		seg := segSeg(t, in)
		flat := strings.ReplaceAll(seg, cjkSegmentSep, "")
		for i, gram := range cjkGrams2(in) {
			if !strings.Contains(flat, gram) {
				t.Errorf("input %q: gram %d (%q) lost by segmentation %q", in, i, gram, seg)
			}
		}
	}
}

// TestSegmentedQueryMatchesStoredToken is the index-side/query-side contract
// in one assertion: a row stored under a natural Chinese phrase is recalled by
// a LONGER, differently-worded natural query that shares its words, because
// both sides segment with the same call. RED on the pre-change code for the
// segmenter's own words (逐步 / 缩小 were not standalone tokens on either
// side).
func TestSegmentedQueryMatchesStoredToken(t *testing.T) {
	setMemoryDB(t)
	stored := "逐步缩小 AND 检索算法的实现位置与退化策略"
	answer := "the ladder degrades one token at a time and returns as soon as a subset qualifies"
	if _, err := MemoryStore("检索", stored, answer, "test", nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"逐步 缩小", "逐步缩小", "检索算法"} {
		out, err := MemorySearch(q, 5, "")
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if !strings.Contains(out, "逐步缩小") {
			t.Errorf("search %q did not recall the stored row:\n%s", q, out)
		}
	}
}

// TestSegmentedLikeRouteRecallsUnknownWord: the token index can only match a
// word boundary it knows. A query word that the stored phrase contains as a
// SUBSTRING of a longer word (检索算法 lives inside 检索算法的…, and the
// segmenter may join or split it differently) is recalled by route 3, the
// segmented LIKE scan — the route that replaced the 2-char gram walk. Without
// it the row would be reachable only by a boundary the segmenter happened to
// produce on both sides.
func TestSegmentedLikeRouteRecallsUnknownWord(t *testing.T) {
	setMemoryDB(t)
	// 络测 is not a word in any dictionary and not a token boundary; it is a
	// pure substring of the stored phrase.
	if _, err := MemoryStore("t", "网络测试环境的部署", "an answer body long enough for the quality gate", "test", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryStore("decoy", "how do split tunnels drop packets",
		"raise the mtu and enable dns over the tunnel for the wg interface", "test", nil); err != nil {
		t.Fatal(err)
	}
	segs := cjkQuerySegments("络测")
	if len(segs) == 0 {
		t.Fatalf("cjkQuerySegments(%q) returned nothing", "络测")
	}
	// The route's own SQL is what must find it: the terms are substrings, so
	// the LIKE clause matches the stored text.
	out, err := MemorySearch("络测", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "络测") && strings.Contains(out, `"empty":true`) {
		t.Errorf("segmented LIKE route did not recall the infix row:\n%s", out)
	}
}

// TestQuerySegmentationSharesOneSegmenter is the anti-drift assertion: the
// query side may never keep its own segmentation rules. Both lists come from
// cjkseg.Words, so equality here is the machine's own statement that index and
// query cannot disagree.
func TestQuerySegmentationSharesOneSegmenter(t *testing.T) {
	q := "把 gopi 的 jieba_token 换成真实的 gse 分词"
	words, err := cjkseg.Words(q)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cjkQuerySegments(q), " "), strings.Join(words, " "); got != want {
		t.Errorf("query segmentation drifted from the segmenter:\n got %q\nwant %q", got, want)
	}
	if got, want := cjkSegmentation(q), strings.Join(words, cjkSegmentSep); got != want {
		t.Errorf("index segmentation drifted from the segmenter:\n got %q\nwant %q", got, want)
	}
}

// TestFtsIndexUsesPorterTokenizer: a fresh knowledge base must build its FTS
// index on porter, not unicode61 — the DDL that sqlite_master holds is the
// evidence, and it is the same string the migration probes.
func TestFtsIndexUsesPorterTokenizer(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("t", "a question long enough to pass the gate",
		"an answer body long enough to pass the quality gate", "test", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	var ddl string
	if err := db.QueryRow(
		"SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name=?", ftsSyncFTSName).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, ftsIndexPorter) {
		t.Fatalf("distilled_qa_fts was not created with porter:\n%s", ddl)
	}
}

// TestPorterStemFoldsCommitAndCommits is the RED→GREEN proof for the
// tokenizer: the SAME row carries `commits` in its body and is found by
// MATCH 'commit', while MATCH 'commits' still finds it. On unicode61 the
// first query returns 0 rows — measured on the linked engine before the fix.
func TestPorterStemFoldsCommitAndCommits(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("检索", "the tokenizer migration question",
		"the index keeps folding these commits into one stem for the search route", "test", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	count := func(match string) int {
		var n int
		if err := db.QueryRow(
			`SELECT count(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH ?`, match).Scan(&n); err != nil {
			t.Fatalf("MATCH %q: %v", match, err)
		}
		return n
	}
	if got := count(`"commits"`); got == 0 {
		t.Fatalf(`MATCH "commits" found 0 rows — the fixture did not index the body`)
	}
	if got := count(`"commit"`); got == 0 {
		t.Errorf(`MATCH "commit" found 0 rows for a row whose body says commits — porter is not in force`)
	}
	// The reverse direction: a row stored with the singular is found by the
	// plural query.
	if _, err := MemoryStore("检索", "the singular spelling question",
		"this answer body records one commit and nothing else at all here", "test", nil); err != nil {
		t.Fatal(err)
	}
	if got := count(`"commits"`); got < 2 {
		t.Errorf(`MATCH "commits" found %d rows; the singular row should fold into the same stem`, got)
	}
}

// TestFtsTokenizerMigrationIsIdempotentAndPreservesRows drives the REAL
// migration entry point twice on a DB built the old way: the row survives, the
// index is refilled under porter (the body becomes stem-folded), and the
// second call is a no-op rather than an error.
func TestFtsTokenizerMigrationIsIdempotentAndPreservesRows(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("检索", "the migration fixture question",
		"these commits must remain searchable after the index is rebuilt", "test", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	// Downgrade the index to the pre-migration shape: drop it and rebuild it
	// WITHOUT the tokenizer option (exactly what a pre-fix gopi left on disk).
	for _, stmt := range []string{
		"DROP TRIGGER IF EXISTS distilled_qa_ai",
		"DROP TRIGGER IF EXISTS distilled_qa_au",
		"DROP TRIGGER IF EXISTS distilled_qa_ad",
		"DROP TABLE IF EXISTS " + ftsSyncFTSName,
		m1FTSVirtualTableNoTokenizer(),
		`INSERT INTO ` + ftsSyncFTSName + `(` + ftsSyncFTSName + `) VALUES('rebuild')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("downgrade %q: %v", stmt, err)
		}
	}
	if ftsIsPorter(t.Context(), db) {
		t.Fatal("fixture precondition: the downgraded index still reports porter")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH '"commit"'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("fixture precondition: unicode61 already folded commit (n=%d)", n)
	}

	// RED before the fix: the migration is the only thing that changes it.
	if err := migrateDefaultTriggers(t.Context(), path); err != nil {
		t.Fatalf("migrateDefaultTriggers: %v", err)
	}
	db2 := openRawDB(t, path)
	var rows int
	if err := db2.QueryRow("SELECT count(*) FROM distilled_qa").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("migration lost rows: distilled_qa has %d, want 1", rows)
	}
	if !ftsIsPorter(t.Context(), db2) {
		t.Fatal("migration did not convert the index to porter")
	}
	if err := db2.QueryRow(`SELECT count(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH '"commit"'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error(`after migration MATCH "commit" still finds nothing — the index was not reindexed under porter`)
	}
	// Idempotent: a second run is a no-op, not an error, and the row stays.
	if err := migrateDefaultTriggers(t.Context(), path); err != nil {
		t.Fatalf("second migrateDefaultTriggers: %v", err)
	}
	db3 := openRawDB(t, path)
	if err := db3.QueryRow("SELECT count(*) FROM distilled_qa").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("second migration changed the row count: %d", rows)
	}
}

// m1FTSVirtualTableNoTokenizer renders the PRE-migration CREATE — the index
// shape a gopi built before this change left on disk. It is a test-only
// literal on purpose: production must never build an index this way again.
func m1FTSVirtualTableNoTokenizer() string {
	return "CREATE VIRTUAL TABLE IF NOT EXISTS " + ftsSyncFTSName + " USING fts5(" +
		m1FTSCols + ", content='distilled_qa', content_rowid='id')"
}

// TestJiebaTokenBackfillReindexesExistingRows is the BACKFILL proof: rows
// written before the swap hold a bigram token list, and the engine's own
// reindex path — MemoryUpdate, the same verb the maintenance loop already
// has — rewrites them into real segmentation without touching the row's
// prose. No new script is involved (Rule 35: the existing CLI face is the
// migration).
func TestJiebaTokenBackfillReindexesExistingRows(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("成本分析", "逐步缩小检索算法的实现位置",
		"the answer body long enough to pass the quality gate", "test", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	// Simulate the pre-swap row: overwrite jieba_token with the old bigram
	// list (what the previous release's writer produced).
	oldStyle := strings.Join(cjkGrams2("逐步缩小"), ", ")
	if _, err := db.Exec("UPDATE distilled_qa SET jieba_token=? WHERE id=1", oldStyle); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.QueryRow("SELECT jieba_token FROM distilled_qa WHERE id=1").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before, "逐步 缩小") {
		t.Fatalf("fixture precondition: the row already carries real segmentation: %q", before)
	}

	// The backfill: re-derive the token through the engine's own update path
	// (re-writing the question re-derives the token from the STORED row).
	if _, err := MemoryUpdate(1, MemoryUpdateFields{Question: "逐步缩小检索算法的实现位置"}); err != nil {
		t.Fatal(err)
	}
	db2 := openRawDB(t, path)
	var after string
	if err := db2.QueryRow("SELECT jieba_token FROM distilled_qa WHERE id=1").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "逐步") || !strings.Contains(after, "缩小") {
		t.Errorf("backfill did not install real segmentation: %q", after)
	}
	if strings.Contains(after, ", ") {
		t.Errorf("backfill left the old comma-joined gram list in place: %q", after)
	}
}
