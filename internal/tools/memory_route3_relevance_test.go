package tools

// Regression guard for the memory-search route-3 ordering defect
// (devdoc/memory-route3-relevance.md). The measured shape, reproduced here in
// miniature:
//
//   - the substring route (route 3) ended in `ORDER BY d.confidence DESC LIMIT
//     <quota>`, a key that says nothing about the QUERY. On the production
//     corpus 70 rows tie at confidence=1.0, so the quota (15) was filled by a
//     constant low-rowid set — the same 15 rows for every natural-language
//     question;
//   - the shared rescore could not repair it, because its coverage term was
//     structurally zero for CJK: meaningfulQueryTokens split on whitespace, so
//     a Chinese question was ONE token that no record contains.
//
// Both halves are asserted here. The corpus below is the minimal sample: a
// target that answers FOUR query words, stored LAST and at LOWER confidence,
// behind decoys that answer ONE query word each, stored first at confidence
// 1.0. There are more decoys than the route quota, so the pre-fix ordering
// does not merely rank the target low — it keeps it out of the candidate pool
// entirely.

import (
	"context"
	"fmt"
	"testing"
)

const (
	// route3RelevanceDecoys exceeds trigramRecallK (the route-3 quota) on
	// purpose: with fewer decoys than the quota, the target would still enter
	// the pool and the test would only measure ordering. Sized so the quota is
	// filled by decoys alone.
	route3RelevanceDecoys = trigramRecallK + 5

	// route3RelevanceQuery shares four content words with the target row and
	// one with every decoy.
	route3RelevanceQuery = "网络测试环境部署完成"

	route3RelevanceTopic = "网络测试环境部署完成的判定标准"
	route3RelevanceBody  = "网络测试环境部署完成的判定标准：先校验网段，再落盘配置，最后按链路分层复测一次。"

	route3RelevanceDecoyConf  = 1.0
	route3RelevanceTargetConf = 0.4
)

// seedRoute3RelevanceCorpus stores the decoys first (low rowids) and the
// target last, then pins the confidences so that the ONLY thing separating the
// target from the decoys is whether the ranking key knows what the query asked
// for: the decoys outrank it on confidence and on rowid, and lose on relevance.
func seedRoute3RelevanceCorpus(t *testing.T) (targetID int) {
	t.Helper()
	for i := 0; i < route3RelevanceDecoys; i++ {
		topic := fmt.Sprintf("网络巡检记录 %02d", i)
		question := fmt.Sprintf("第 %02d 次网络巡检记录：链路时延与丢包率的采样结果。", i)
		answer := fmt.Sprintf("第 %02d 次网络巡检的采样明细，用于绘制链路时延与丢包率的历史曲线。", i)
		if _, err := MemoryStore(topic, question, answer, "probe/route3-relevance",
			[]string{"probe", "route3", fmt.Sprintf("decoy%02d", i)}); err != nil {
			t.Fatalf("store decoy %d: %v", i, err)
		}
	}
	out, err := MemoryStore(route3RelevanceTopic, route3RelevanceTopic, route3RelevanceBody,
		"probe/route3-relevance", []string{"probe", "route3", "target"})
	if err != nil {
		t.Fatalf("store target: %v", err)
	}
	targetID = memoryStoredID(t, out)
	db := openRawDB(t, memoryDBPath())
	if _, err := db.ExecContext(context.Background(),
		`UPDATE distilled_qa SET confidence=? WHERE id <> ?`, route3RelevanceDecoyConf, targetID); err != nil {
		t.Fatalf("pin decoy confidence: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		`UPDATE distilled_qa SET confidence=? WHERE id = ?`, route3RelevanceTargetConf, targetID); err != nil {
		t.Fatalf("pin target confidence: %v", err)
	}
	return targetID
}

// TestMemorySearchRoute3RanksByRelevanceNotConfidence is the red/green guard.
//
// It drives route 3 ITSELF, not the merged entry point: the other recall
// routes can (and on this corpus do) reach the target through their own
// indexes, so a public-entry-point assertion would pass on the pre-fix code
// and measure nothing. Route 3's own quota is where the defect lives — before
// the fix the confidence-ordered LIMIT filled up with decoys and the target
// never entered the pool at all.
func TestMemorySearchRoute3RanksByRelevanceNotConfidence(t *testing.T) {
	_ = setMemoryDB(t)
	targetID := seedRoute3RelevanceCorpus(t)

	got := route3IDs(t, route3RelevanceQuery, trigramRecallK)
	if len(got) == 0 {
		t.Fatalf("route 3 returned nothing for %q", route3RelevanceQuery)
	}
	if got[0] != targetID {
		t.Fatalf("route 3 ranked %d first, want the row that answers the query (%d) — got %v",
			got[0], targetID, got)
	}
	if !containsInt(got, targetID) {
		t.Fatalf("route 3 did not return the target row %d at all — got %v", targetID, got)
	}

	// The coverage signal is the reason the ordering can see the difference:
	// a natural CJK question must yield terms, not one unsplittable token.
	hits, err := MemorySearchHits(route3RelevanceQuery, trigramRecallK, "")
	if err != nil {
		t.Fatalf("MemorySearchHits(%q): %v", route3RelevanceQuery, err)
	}
	var target *MemoryHit
	for i := range hits {
		if hits[i].ID == targetID {
			target = &hits[i]
		}
	}
	if target == nil {
		t.Fatalf("MemorySearchHits(%q) did not return the target row %d", route3RelevanceQuery, targetID)
	}
	if target.Tokens == 0 {
		t.Errorf("the target row reports tokens=0 — the coverage tokenizer produced nothing for %q",
			route3RelevanceQuery)
	}
}

// route3IDs runs route 3 exactly as MemorySearchHits sends it (same WHERE, same
// ORDER BY key, same LIMIT), built from the production helpers so a change to
// the key cannot drift away from what the test measures. It reads the ids in
// RETURNED ORDER — scanIDs sorts, which would throw the ranking away.
func route3IDs(t *testing.T, query string, limit int) []int {
	t.Helper()
	terms := cjkQuerySegments(query)
	if len(terms) == 0 {
		return nil
	}
	// The SQL and its args come from the PRODUCTION builder, so this reader
	// cannot measure a different query than the route sends.
	sqlText, args := likeCJKQuery(terms, "", limit)
	_ = limit
	db := openRawDB(t, memoryDBPath())
	rows, err := db.QueryContext(context.Background(), sqlText, args...)
	if err != nil {
		t.Fatalf("route 3 query %q: %v", query, err)
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		// The route's SELECT carries the base-prose columns plus the NULL rank
		// placeholder; only the id is of interest here.
		var id int
		var topic, question, answer string
		var rank interface{}
		if err := rows.Scan(&id, &topic, &question, &answer, &rank); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// TestMeaningfulQueryTokensSegmentsCJK locks the tokenizer contract the
// ordering depends on, at the granularity the corpus is written at: a
// whitespace-free Chinese question yields its WORDS, not itself.
func TestMeaningfulQueryTokensSegmentsCJK(t *testing.T) {
	terms := meaningfulQueryTokens(route3RelevanceQuery)
	if len(terms) < 2 {
		t.Fatalf("meaningfulQueryTokens(%q) = %v — a natural CJK question must segment into its words",
			route3RelevanceQuery, terms)
	}
	if len(terms) == 1 && terms[0] == route3RelevanceQuery {
		t.Fatalf("meaningfulQueryTokens(%q) returned the whole question as one token — whitespace splitting is back",
			route3RelevanceQuery)
	}
	for _, want := range []string{"网络", "测试环境", "部署"} {
		if !containsString(terms, want) {
			t.Errorf("meaningfulQueryTokens(%q) = %v, missing the segmented word %q",
				route3RelevanceQuery, terms, want)
		}
	}
}
