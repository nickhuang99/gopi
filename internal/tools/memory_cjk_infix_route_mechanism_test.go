package tools

import (
	"context"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// The CJK infix-recall epic (01a0e4b4) measured WHICH route owns which query
// shape on the shipped index. The measurement is the deliverable — its
// verdict lines are quoted in devdoc/memsearch-cjk-infix-recall.md — but the
// TOKENIZER and the SEGMENTER it measured have since changed:
//
//   - distilled_qa_fts moved from unicode61 to porter (ASCII stems fold);
//   - jieba_token moved from a 2-char gram window to real segmentation
//     (internal/cjkseg, gse), and route 1 gained a segmented MATCH at that
//     granularity while route 3 became a segmented LIKE scan.
//
// A route measurement that hard-codes the old mechanism does not survive that
// (it fails on a change that is not a regression — measured when the swap
// landed: `测试` is no longer a stored token, because the segmenter emits the
// WORD 测试环境 rather than every overlapping 2-char window). What is TRUE
// across both tokenizers is the MECHANISM: the token index cannot carry a
// substring that reaches across a word boundary, and the substring route can.
// So the tests below assert the invariants and EXTRACT the measured table,
// instead of pinning one tokenizer's token list.
//
// The two subtests kept here are the parts of the epic's evidence that are
// mechanism-independent: a query shape that must stay reachable (with the
// public entry point agreeing with the union of the routes), and the control
// pair that proves the substring route — not the token index — is what
// carries a cross-boundary infix.
// ---------------------------------------------------------------------------

// TestMemorySearchInfixCJKRecallAndUnion fixes the epic's regression bar: the
// shipped corpus stays reachable by every query shape a human would call a
// hit, and MemorySearchHits never disagrees with the union of its routes.
func TestMemorySearchInfixCJKRecallAndUnion(t *testing.T) {
	_ = setMemoryDB(t)
	ids := seedCJKInfixCorpus(t)

	recoverable := []struct {
		name   string
		query  string
		expect []string // topics a human would call hits
	}{
		{"full stored CJK run", "网络测试环境部署完成", []string{"网络测试环境部署完成"}},
		{"word inside a stored phrase", "测试环境", []string{"网络测试环境部署完成"}},
		{"mixed CJK+ASCII", "VPN 网络测试环境", []string{"VPN 网络测试环境"}},
		{"CJK split at ASCII boundary", "网络 延迟", []string{"网络延迟排查"}},
	}
	for _, tc := range recoverable {
		want := make([]int, 0, len(tc.expect))
		for _, topic := range tc.expect {
			id, ok := ids[topic]
			if !ok {
				t.Fatalf("%s: corpus row %q missing", tc.name, topic)
			}
			want = append(want, id)
		}
		sort.Ints(want)

		got := publicHitIDs(t, tc.query)
		for _, id := range want {
			if !containsInt(got, id) {
				t.Errorf("%s: MemorySearchHits(%q) missed row %d — got %v, want ⊇ %v",
					tc.name, tc.query, id, got, want)
			}
		}

		// The union of the routes must equal what the public entry point
		// returns: a route reachable from one and not the other is a silent
		// recall/display drift.
		union := map[int]bool{}
		for _, id := range segRouteIDs(t, tc.query) {
			union[id] = true
		}
		for _, id := range ftsRouteIDs(t, tc.query, memoryTriIndexTable) {
			union[id] = true
		}
		for _, id := range likeRouteIDs(t, tc.query) {
			union[id] = true
		}
		unionIDs := make([]int, 0, len(union))
		for id := range union {
			unionIDs = append(unionIDs, id)
		}
		sort.Ints(unionIDs)
		if !sameIDSet(unionIDs, got) {
			t.Errorf("%s: MemorySearchHits(%q) returned %v but the route union is %v — the public entry point and the routes disagree",
				tc.name, tc.query, got, unionIDs)
		}
		t.Logf("%-38s %-22q public=%d segRoute1b=%d", tc.name, tc.query, len(got), len(segRouteIDs(t, tc.query)))
	}
}

// TestMemorySearchSubstringRouteOwnsCrossWordInfix is the causal control pair
// (epic 01a0e4b4's measurement, re-derived under the new tokenizer). A
// substring that does NOT fall on a word boundary the segmenter produced —
// "络测" spans 网络|测试环境 — is carried by the substring route, while the
// token index answers a query only when the query IS a stored token.
func TestMemorySearchSubstringRouteOwnsCrossWordInfix(t *testing.T) {
	ctx := context.Background()
	_ = setMemoryDB(t)
	seedCJKInfixCorpus(t)

	// (1) The causal control: the same corpus in a plain unicode61 index (no
	// jieba_token column at all) answers a cross-boundary infix ZERO times.
	// The delta against the production routes IS the mechanism.
	ctl := openRawDB(t, memoryDBPath())
	if _, err := ctl.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS cjk_probe_ctl
		USING fts5(topic, question, answer, tokenize='unicode61')`); err != nil {
		t.Fatalf("create control table: %v", err)
	}
	for _, r := range cjkInfixCorpus {
		if _, err := ctl.ExecContext(ctx,
			`INSERT INTO cjk_probe_ctl(topic, question, answer) VALUES (?,?,?)`,
			r.topic, r.question, r.answer); err != nil {
			t.Fatal(err)
		}
	}
	ctlHits := 0
	for _, q := range []string{"络测", "试环境"} {
		var n int
		if err := ctl.QueryRowContext(ctx,
			`SELECT count(*) FROM cjk_probe_ctl WHERE cjk_probe_ctl MATCH ?`, `"`+q+`"`).Scan(&n); err != nil {
			t.Fatalf("control MATCH %q: %v", q, err)
		}
		ctlHits += n
	}
	if ctlHits != 0 {
		t.Errorf("unicode61-only control matched a cross-boundary infix %d times, want 0 — the premise this devdoc measures is not reproducible", ctlHits)
	}

	// (2) On the production index the same shapes are reachable, and the
	// substring route is what carries them (the token index does not have the
	// substring as a token — that is exactly why the route exists).
	for _, q := range []string{"络测", "试环境"} {
		got := publicHitIDs(t, q)
		if len(got) == 0 {
			t.Errorf("cross-boundary infix %q recalled nothing: unicode61-only control=%d rows, production=%v", q, ctlHits, got)
		}
	}
	// (3) The token index answers only a stored token: the segmenter's own
	// word for the corpus phrase must be a stored token of the index.
	if !storedFTSTerm(t, "测试环境") {
		t.Error(`token |测试环境| is not in the index — the segmenter's own word for the corpus phrase is not addressable by route 1`)
	}
}

// segRouteIDs runs the segmented route (route 1b) exactly as MemorySearchHits
// does: the query's segmented words AND-ed against the jieba_token column.
func segRouteIDs(t *testing.T, query string) []int {
	t.Helper()
	terms := cjkQuerySegments(query)
	match := ftsAndTerms(terms)
	if match == "" {
		return nil
	}
	return scanIDs(t, query, `SELECT d.id FROM `+ftsSyncFTSName+` x JOIN distilled_qa d ON d.id = x.rowid
		WHERE `+ftsSyncFTSName+` MATCH ? ORDER BY d.id`, match)
}

// storedFTSTerm reports whether term is an actual token of the production FTS
// index — asked of the index itself (fts5vocab), never inferred from the
// segmenter's source (Rule 38: the figure names its generator).
func storedFTSTerm(t *testing.T, term string) bool {
	t.Helper()
	db := openRawDB(t, memoryDBPath())
	if _, err := db.ExecContext(context.Background(), `CREATE VIRTUAL TABLE IF NOT EXISTS cjk_probe_vocab
		USING fts5vocab('`+ftsSyncFTSName+`','row')`); err != nil {
		t.Fatalf("fts5vocab over %s: %v", ftsSyncFTSName, err)
	}
	var one int
	err := db.QueryRow(
		`SELECT 1 FROM cjk_probe_vocab WHERE term=?`, term).Scan(&one)
	return err == nil
}
