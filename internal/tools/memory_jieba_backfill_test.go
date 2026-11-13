package tools

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Backfill of jieba_token for rows written BEFORE the segmenter swap.
//
// The rule this file exists to satisfy is Rule 35 (a persistent dependency
// needs a named lifecycle owner), and the answer is that NO new artifact is
// created: the backfill is an existing verb — `gopi memory backfill-tokens` —
// over the SAME database the engine already owns. The one-shot is invoked by a
// human (or the task that upgraded the binary); nothing self-schedules
// (devdoc/jieba-token-real-segmentation.md §Rule-35).
//
// What a re-derivation must do, and what it must NOT: rewrite ONLY the
// jieba_token column of rows whose stored value is not already what
// cjkSegmentation produces, leaving every other column — including the prose
// the row is FOR — byte-for-byte untouched. A backfill that silently
// re-answers, re-tiers or re-times a row would be a data mutation masquerading
// as an index rebuild.
// ---------------------------------------------------------------------------

// TestBackfillJiebaTokensRewritesOnlyTheTokenColumn drives the real backfill
// path over a legacy row: the old comma-joined 2-char gram list is replaced
// with real segmentation, the FTS index keeps finding the row under a query
// the segmenter produces, and every other column is unchanged.
func TestBackfillJiebaTokensRewritesOnlyTheTokenColumn(t *testing.T) {
	ctx := context.Background()
	path := newEmptyLedger(t)
	db := openRawDB(t, path)

	oldStyle := strings.Join(cjkGrams2("逐步缩小检索算法"), ", ")
	id := insertLegacyRow(t, db, legacyRow{
		topic:     "成本分析",
		question:  "逐步缩小检索算法在哪里实现",
		answer:    "the answer body long enough to pass the quality gate for this fixture",
		jiebaText: oldStyle,
	})

	// Precondition: the stored token is the OLD spelling and does not contain
	// the segmenter's own words as tokens.
	var before string
	if err := db.QueryRowContext(ctx, "SELECT jieba_token FROM distilled_qa WHERE id=?", id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != oldStyle {
		t.Fatalf("fixture precondition: stored token %q, want the legacy gram list %q", before, oldStyle)
	}

	// Rebuild the legacy FTS index shape the row would have arrived with.
	if _, err := db.ExecContext(ctx, `INSERT INTO `+ftsSyncFTSName+`(`+ftsSyncFTSName+`) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}

	scanned, changed, err := BackfillJiebaTokens(ctx, db)
	if err != nil {
		t.Fatalf("BackfillJiebaTokens: %v", err)
	}
	if scanned != 1 || changed != 1 {
		t.Errorf("backfill scanned=%d changed=%d, want 1/1", scanned, changed)
	}

	// The row's prose and metadata are untouched; only the token changed.
	var (
		topic, question, answer, after, tier string
		conf                                 float64
	)
	if err := db.QueryRowContext(ctx,
		"SELECT topic, question, answer, jieba_token, tier, confidence FROM distilled_qa WHERE id=?", id).
		Scan(&topic, &question, &answer, &after, &tier, &conf); err != nil {
		t.Fatal(err)
	}
	if topic != "成本分析" || question != "逐步缩小检索算法在哪里实现" || tier != "warm" || conf != 0.85 {
		t.Errorf("backfill changed columns it must not touch: topic=%q question=%q tier=%q conf=%v",
			topic, question, tier, conf)
	}
	if !strings.Contains(answer, "quality gate") {
		t.Errorf("backfill changed the answer body: %q", answer)
	}
	if after == oldStyle {
		t.Fatalf("backfill left the legacy token in place: %q", after)
	}
	if !strings.Contains(after, "逐步") || !strings.Contains(after, "缩小") {
		t.Errorf("backfill did not install real segmentation: %q", after)
	}
	if strings.Contains(after, ", ") {
		t.Errorf("backfilled token still looks like the comma-joined gram list: %q", after)
	}

	// The index agrees: the row is reachable at the segmenter's own
	// granularity after the rebuild.
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM `+ftsSyncFTSName+` WHERE `+ftsSyncFTSName+` MATCH ?`, `jieba_token:"逐步"`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("after the backfill the row is not addressable by the segmenter's own word 逐步")
	}

	// Idempotent: a second pass scans the same rows and changes none of them.
	scanned2, changed2, err := BackfillJiebaTokens(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 != 0 || scanned2 != 1 {
		t.Errorf("second backfill scanned=%d changed=%d, want scanned=1 changed=0 (idempotent)", scanned2, changed2)
	}
}

// TestBackfillJiebaTokensPreservesAnUnsegmentableRow guards the other side:
// a row whose text yields no words (pure ASCII punctuation, an empty topic)
// must keep its stored token rather than be blanked. An index rebuild that
// empties a row makes it permanently un-findable, which is worse than a stale
// token.
func TestBackfillJiebaTokensPreservesAnUnsegmentableRow(t *testing.T) {
	ctx := context.Background()
	path := newEmptyLedger(t)
	db := openRawDB(t, path)
	id := insertLegacyRow(t, db, legacyRow{
		topic:     "!!!",
		question:  "???",
		answer:    "///",
		jiebaText: "preserve me",
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO `+ftsSyncFTSName+`(`+ftsSyncFTSName+`) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	scanned, changed, err := BackfillJiebaTokens(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	// The row IS scanned (it looks like a legacy spelling) but its token must
	// not be blanked: either the derivation is unchanged, or the writer kept
	// the old value. Both are acceptable; an empty token is not.
	if scanned != 1 {
		t.Errorf("backfill scanned=%d, want 1", scanned)
	}
	var after string
	if err := db.QueryRowContext(ctx, "SELECT jieba_token FROM distilled_qa WHERE id=?", id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(after) == "" && changed == 1 {
		t.Errorf("backfill blanked a row's token column (changed=%d, token=%q)", changed, after)
	}
}

// TestBackfillJiebaTokensKeepsRowsCountStable is the blunt safety net: the
// backfill is an UPDATE-only path, so the row count and the ids are identical
// across it.
func TestBackfillJiebaTokensKeepsRowsCountStable(t *testing.T) {
	ctx := context.Background()
	path := newEmptyLedger(t)
	db := openRawDB(t, path)
	for i, q := range []string{"逐步缩小检索算法的实现", "网络测试环境的部署", "夜间批次调度的策略"} {
		insertLegacyRow(t, db, legacyRow{
			topic:     "t" + strconv.Itoa(i),
			question:  q,
			answer:    "answer body long enough to pass the quality gate for this fixture",
			jiebaText: strings.Join(cjkGrams2(q), ", "),
		})
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO `+ftsSyncFTSName+`(`+ftsSyncFTSName+`) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	before := readLedgerIDs(t, db)
	if _, _, err := BackfillJiebaTokens(ctx, db); err != nil {
		t.Fatal(err)
	}
	after := readLedgerIDs(t, db)
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Errorf("backfill changed the row set: before=%v after=%v", before, after)
	}
	if len(after) != 3 {
		t.Errorf("expected the 3 inserted rows to survive, got %v", after)
	}
}

// --- fixtures ---------------------------------------------------------------

type legacyRow struct {
	topic, question, answer, jiebaText string
}

// insertLegacyRow writes a row the way a PRE-swap gopi would have: the token
// column holds whatever the caller passes (the old comma-joined gram list),
// and the row's metadata carries the values the schema's defaults give.
func insertLegacyRow(t *testing.T, db *sql.DB, r legacyRow) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO distilled_qa(topic, question, answer, sources, search_kw, jieba_token, lesson_type,
		   confidence, tier, distilled_at, retrieval_count, useful_count, useless_count)
		 VALUES(?,?,?,'legacy','',?,'recipe',0.85,'warm',1.0,0,0,0)`,
		r.topic, r.question, r.answer, r.jiebaText)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	return id
}

// readLedgerIDs reads the ledger's row ids in order, as strings, so a
// before/after comparison is a plain equality on one value.
func readLedgerIDs(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT id FROM distilled_qa ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //stylegate:ignore read-only cursor closed on return
	var ids []string
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, strconv.Itoa(id))
	}
	return ids
}

// newEmptyLedger creates a fresh ledger path under the pinned scratch root
// (Rule 32/22) WITHOUT opening it through the engine, so a test can build a
// legacy-shaped database in it. It reuses the engine's own schema entry point
// (ensureSchema) so the tables are the production ones, not a second
// hand-rolled definition.
func newEmptyLedger(t *testing.T) string {
	t.Helper()
	path := setMemoryDB(t)
	// Open once through the production entry point so the schema is the real
	// one, then close: the test then writes its legacy rows by hand.
	if _, err := MemoryStore("bootstrap", "does this fixture row exist at all",
		"the bootstrap body long enough to pass the quality gate for this fixture", "fixture", nil); err != nil {
		t.Fatal(err)
	}
	db := openRawDB(t, path)
	if _, err := db.Exec("DELETE FROM distilled_qa"); err != nil {
		t.Fatal(err)
	}
	return path
}
