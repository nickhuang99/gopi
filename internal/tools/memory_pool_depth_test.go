package tools

// Regression guard for the memory-search CANDIDATE POOL defect
// (devdoc/memory-recall-pool.md): a route's LIMIT is a recall ceiling, and the
// shared rescore can only re-order the rows a route RETURNED — it never sees
// the ones the LIMIT discarded before it ran.
//
// The measured shape, reproduced here in miniature: route 3's WHERE is a broad
// OR of LIKE predicates, so its candidate set on the real corpus runs into the
// thousands (median 4,412 rows for the natural-question sample). With the scan
// bounded at trigramRecallK (15) the route returned the first 15 rows the
// planner visited, and every row beyond that cut was unreachable no matter how
// well it answered the query. The upstream route-3 fix gave the route a
// relevance key that can ORDER the whole match set — but an ordering the LIMIT
// truncates inside a TIE still hands the rescore a set that does not contain
// the answer (measured: 39/120 natural queries had their target outside the
// pool entirely).
//
// The corpus below is the minimal sample of exactly that: the decoys TIE the
// target on route 3's own relevance key (one rankable term, weight 2, in two
// columns each) and win the key's confidence tiebreak, so the pre-fix depth
// fills the route with decoys; the target is strictly BETTER for the shared
// rescore (it covers more of the query), so it wins the moment it enters the
// pool. There are more decoys than trigramRecallK on purpose.

import (
	"context"
	"fmt"
	"testing"
)

const (
	// poolDepthDecoys exceeds trigramRecallK (route 3's pre-fix depth) so the
	// route's old LIMIT is filled by decoys alone — the target is not merely
	// ranked low, it is never recalled.
	poolDepthDecoys = trigramRecallK + 5

	// poolDepthQuery is one the OTHER routes cannot answer: it is a single CJK
	// run, so route 1 asks FTS5 for the whole run (0 rows), route 1b demands
	// the segmenter's words 盘配/测链 together (the row carries neither word —
	// only the infix 盘配 inside 网盘配置), route 2 asks the trigram index for
	// the verbatim run (0 rows), and the ASCII-OR fallback has no ASCII token
	// to fall back on. Route 3 — the CJK substring LIKE scan — is therefore the
	// ONLY route that can reach either the decoys or the target, which is what
	// makes the route's scan depth the single variable under test.
	poolDepthQuery = "盘配与测链"

	poolDepthDecoyConf  = 1.0
	poolDepthTargetConf = 0.3

	// poolDepthQuota is the result quota the acceptance metric scores (the
	// arena searches every query with --limit 10).
	poolDepthQuota = 10
)

// seedPoolDepthCorpus stores the decoys first (low rowids, high confidence) and
// the target last (high rowid, low confidence): every tiebreak that is not a
// query signal — rowid, confidence — points AWAY from the target, so only a
// route that returns deep enough for the query signal to act can recall it.
func seedPoolDepthCorpus(t *testing.T) (targetID int) {
	t.Helper()
	// Decoys: each DISTINCT (the store dedups identical topic+question pairs, so
	// identical decoys would collapse into one row and the corpus would measure
	// nothing). Each carries the query's infix 盘配 — inside the word 网盘配置,
	// which is a different word than the query's 盘配, so no token route sees it
	// — in question and topic, i.e. the same route-3 relevance weight as the
	// target (one rankable term, weight 2, in two columns), and each sits at
	// confidence 1.0 against the target's 0.3 so the key's confidence tiebreak
	// puts every decoy ahead of it.
	for i := 0; i < poolDepthDecoys; i++ {
		topic := fmt.Sprintf("网盘配置巡检口径 %02d", i)
		question := fmt.Sprintf("第 %02d 次网盘配置巡检：口径与采样频率。", i)
		answer := fmt.Sprintf("第 %02d 次巡检的采样明细，只约束网盘配置的口径与频率，"+
			"用于绘制历史曲线，不涉及其他判定依据。", i)
		if _, err := MemoryStore(topic, question, answer, "probe/pool-depth",
			[]string{"probe", "pooldepth", fmt.Sprintf("decoy%02d", i)}); err != nil {
			t.Fatalf("store decoy %d: %v", i, err)
		}
	}
	// The target: the SAME route-3 relevance weight as every decoy, and strictly
	// better for the shared rescore (it covers both of the query's content
	// words).
	// The target holds the query's 盘配 only as an INFIX of a longer word
	// (落盘配置) — never as a word of its own — so no token route can reach it:
	// route 1 asks for the whole query run, route 1b demands the segmenter's
	// words 盘配 AND 测链 as jieba_token tokens (the row has neither as a token),
	// route 2 asks the trigram index for the verbatim run, and the ASCII-OR
	// fallback has no ASCII token. Route 3's substring LIKE is the only route
	// that can match it, which is what makes that route's scan depth the single
	// variable under test. Its route-3 relevance weight (one rankable term in
	// one column) is BELOW a decoy's, so the confidence tiebreak is not even
	// needed to keep it out of a shallow scan's prefix.
	topic := "验收口径与分层复测"
	question := "盘配的验收口径是什么？"
	answer := "先校验网段可达性，再按测链分层复测一次，三项全绿才算完成。" +
		"这里给的是判定依据，不是操作步骤。"
	out, err := MemoryStore(topic, question, answer, "probe/pool-depth",
		[]string{"probe", "pooldepth", "target"})
	if err != nil {
		t.Fatalf("store target: %v", err)
	}
	targetID = memoryStoredID(t, out)
	db := openRawDB(t, memoryDBPath())
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE distilled_qa SET confidence=? WHERE id <> ?`,
		poolDepthDecoyConf, targetID); err != nil {
		t.Fatalf("pin decoy confidence: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE distilled_qa SET confidence=? WHERE id = ?`,
		poolDepthTargetConf, targetID); err != nil {
		t.Fatalf("pin target confidence: %v", err)
	}
	return targetID
}

// TestMemorySearchRouteDepthRecallsTheAnswer is the red/green guard.
//
// It drives route 3 through the PRODUCTION SQL builder (likeCJKQuery — the very
// function the route calls, with the shipped depth constant), so the reader
// cannot measure a different query than the route sends and the assertion fails
// exactly when the route's scan depth does.
//
// Why route 3's own ordering and not the merged entry point: the OTHER recall
// routes are allowed to rescue a row (that is what a union is for), so a
// public-entry assertion can be green on pre-fix code for reasons that have
// nothing to do with this route's depth. The defect under test is "the route's
// candidate set is cut before the relevance key can act", and the route's own
// returned set is its exact measurement. The merged entry point is asserted too,
// as a GREEN-only check that the row a route recalls is not lost downstream.
func TestMemorySearchRouteDepthRecallsTheAnswer(t *testing.T) {
	_ = setMemoryDB(t)
	targetID := seedPoolDepthCorpus(t)

	// The SQL and its args come from the production builder; the depth is the
	// SHIPPED constant, so this test tracks the route's real reach.
	sqlText, args := likeCJKQuery(cjkQuerySegments(poolDepthQuery), "", likeCJKRecallK)
	db := openRawDB(t, memoryDBPath())
	rows, err := db.QueryContext(context.Background(), sqlText, args...)
	if err != nil {
		t.Fatalf("route 3 query %q: %v", poolDepthQuery, err)
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
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
	found := false
	for _, id := range ids {
		if id == targetID {
			found = true
		}
	}
	if !found {
		t.Fatalf("route 3 (depth %d) did not recall the row that answers %q (id=%d): the scan depth cut "+
			"the candidate set before the relevance key could act, so the shared rescore never saw it. "+
			"returned %d ids: %v", likeCJKRecallK, poolDepthQuery, targetID, len(ids), ids)
	}

	// GREEN-only downstream check: the row the route recalls is not lost by the
	// union, the rescore or the quota.
	hits, err := MemorySearchHits(poolDepthQuery, poolDepthQuota, "")
	if err != nil {
		t.Fatalf("MemorySearchHits(%q): %v", poolDepthQuery, err)
	}
	rank := 0
	for i, h := range hits {
		if h.ID == targetID {
			rank = i + 1
		}
	}
	if rank == 0 {
		returned := make([]int, 0, len(hits))
		for _, h := range hits {
			returned = append(returned, h.ID)
		}
		t.Errorf("MemorySearchHits(%q) did not return the target row %d at limit %d (returned %v)",
			poolDepthQuery, targetID, poolDepthQuota, returned)
	}
}
