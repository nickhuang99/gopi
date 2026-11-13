package tools

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Writer-side half of the memory-search mismatch: jieba_token must absorb the
// retrieval-intent columns (search_kw, exam_questions, the 5W1H labels).
//
// The defect these tests pin (index-side vs query-side tokenization):
//   - the INDEX side indexes search_kw through FTS5 unicode61, whose split on
//     CJK is whitespace/punctuation only — a contiguous CJK run of ANY length
//     becomes ONE token ("挪到夜间" is one token; "夜间" is not in the index);
//   - the QUERY side speaks jieba/CJK-grams (2/3-char fragments) and never
//     reconstructs that arbitrary long token.
// So a row whose question is a declarative title and whose natural phrasing
// lives ONLY in search_kw is unreachable by the natural phrasing. Absorbing
// search_kw into jieba_token makes the same 2-char grams the query side
// produces exist as standalone index tokens.
//
// The tests below are RED on the pre-change code (jieba_token is built from
// question+answer only) and GREEN once the write path absorbs the
// retrieval-intent columns. The contract, the measured symptom and the
// reusable verification recipe: devdoc/jieba-token-absorbs-search-kw.md.
// ---------------------------------------------------------------------------

// searchKwPrefixProbe is the record under test: a DECLARATIVE title question
// whose body shares no wording with the natural phrasing. Every CJK 2-char
// gram of the phrasing lives only in search_kw.
const (
	searchKwTitleQ = "DeepSeek 账单与错峰策略的实测结论"
	searchKwBodyA  = "The measured conclusion is recorded above; the natural wording is not in this body."
)

// searchKwPhrasing is the natural phrasing a user would search with — it never
// appears verbatim in the question, the token, or the body.
const searchKwPhrasing = "挪到夜间"

func TestSearchKwAbsorbedIntoJiebaToken(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("成本分析", searchKwTitleQ, searchKwBodyA, "test",
		[]string{"避峰 挪到夜间 错峰", "deepseek billing"}); err != nil {
		t.Fatal(err)
	}

	db := openRawDB(t, path)
	defer db.Close()
	var jieba, searchKw string
	if err := db.QueryRow(
		"SELECT jieba_token, search_kw FROM distilled_qa WHERE id=1").Scan(&jieba, &searchKw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searchKw, "挪到夜间") {
		t.Fatalf("fixture precondition: search_kw lacks the phrasing: %q", searchKw)
	}
	// The phrasing must survive segmentation at the granularity the QUERY side
	// speaks: cjkQuerySegments is what a search for this phrasing produces, so
	// every fragment it emits must exist inside the stored token — that is the
	// index-side/query-side contract, asserted as a set relation rather than
	// against one tokenizer's idea of a word.
	term := strings.Join(cjkQuerySegments(searchKwPhrasing), cjkSegmentSep)
	if term == "" {
		t.Fatalf("cjkQuerySegments(%q) produced nothing", searchKwPhrasing)
	}
	if !strings.Contains(jieba, term) {
		t.Errorf("jieba_token does not carry the query's own segmentation %q: %q", term, jieba)
	}
	// The declarative title's own grams stay present (no regression).
	if !strings.Contains(jieba, "账单") {
		t.Errorf("jieba_token dropped the question's own grams: %q", jieba)
	}
}

// TestSearchKwOnlyPhrasingIsRetrievable is the end-to-end repro: the natural
// phrasing exists ONLY in search_kw, and a search for it must return the row.
// RED before the change (the row is not recallable at all), GREEN after.
func TestSearchKwOnlyPhrasingIsRetrievable(t *testing.T) {
	setMemoryDB(t)
	if _, err := MemoryStore("成本分析", searchKwTitleQ, searchKwBodyA, "test",
		[]string{"避峰 挪到夜间 错峰"}); err != nil {
		t.Fatal(err)
	}
	// A decoy row that shares NO fragment with the phrasing — it keeps the
	// assertion honest (an empty index returning nothing is not a pass).
	if _, err := MemoryStore("unrelated", "how do split tunnels drop packets",
		"raise the mtu and enable dns over the tunnel for the wg interface", "test", nil); err != nil {
		t.Fatal(err)
	}

	out, err := MemorySearch(searchKwPhrasing, 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, searchKwTitleQ) {
		t.Errorf("search %q did not recall the search_kw-only row:\n%s", searchKwPhrasing, out)
	}
}

// TestSearchKwEmptyBoundaryUnchanged: an empty/short keyword list must not
// change the stored jieba_token — the boundary the fix must not move (old
// behaviour: question+answer only).
func TestSearchKwEmptyBoundaryUnchanged(t *testing.T) {
	path := setMemoryDB(t)
	const topic = "t"
	if _, err := MemoryStore(topic, "what does the empty-keyword boundary store",
		"an answer body long enough to pass the garbage gate", "test", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	defer db.Close()
	var jieba, question, answer string
	if err := db.QueryRow(
		"SELECT jieba_token, question, answer FROM distilled_qa WHERE id=1").Scan(&jieba, &question, &answer); err != nil {
		t.Fatal(err)
	}
	if want := cjkSegmentation(topic + " " + question + " " + answer); jieba != want {
		t.Errorf("empty search_kw changed jieba_token:\n got %q\nwant %q", jieba, want)
	}
}

// TestUpdateRewritesJiebaTokenWithKeywords: memory_update --keywords replaces
// search_kw, so it must re-derive jieba_token from the NEW breadcrumbs — a
// stale jieba_token would keep the pre-edit phrasing inscribed in the index.
func TestUpdateRewritesJiebaTokenWithKeywords(t *testing.T) {
	path := setMemoryDB(t)
	if _, err := MemoryStore("成本分析", searchKwTitleQ, searchKwBodyA, "test",
		[]string{"旧词 换个说法"}); err != nil {
		t.Fatal(err)
	}
	if _, err := MemoryUpdate(1, MemoryUpdateFields{Keywords: []string{"避峰 挪到夜间"}}); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	defer db.Close()
	var jieba, searchKw string
	if err := db.QueryRow(
		"SELECT jieba_token, search_kw FROM distilled_qa WHERE id=1").Scan(&jieba, &searchKw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searchKw, "挪到夜间") {
		t.Fatalf("update did not replace search_kw: %q", searchKw)
	}
	if !strings.Contains(jieba, "夜间") {
		t.Errorf("update left jieba_token stale (missing %q): %q", "夜间", jieba)
	}
	if strings.Contains(jieba, "换个") {
		t.Errorf("update left the OLD keyword's grams in jieba_token: %q", jieba)
	}
	// The new phrasing is retrievable through the rewritten index.
	out, err := MemorySearch(searchKwPhrasing, 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, searchKwTitleQ) {
		t.Errorf("post-update search %q did not hit:\n%s", searchKwPhrasing, out)
	}
}
