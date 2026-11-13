// doc-class: normative
package tools

import (
	"context"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// kwTailAnswer builds the exact answer shape memoryStoreRow produced before
// commit 7139b28: the body, then one "\n\nkeywords: …" block per keyword update,
// each appended WITHOUT stripping the previous one. It is built from the
// writer's own separator/marker spelling so the fixture cannot drift from what
// the store actually wrote (Rules 1b/13).
func kwTailAnswer(body string, blocks ...string) string {
	out := body
	for _, b := range blocks {
		// keywordsTailSeparator() already carries the marker ("\n\n" + marker),
		// so the block text is appended bare — adding the marker again here would
		// build "keywords: keywords: …", a fixture shape the store never writes.
		out += keywordsTailSeparator() + b
	}
	return out
}

// TestKeywordsTailConvergeDoubleTailKeepsLastAndBody is the migration's minimal
// repro: a row carrying TWO different tail blocks becomes a row carrying ONE,
// the body is byte-identical, and the block kept is the LAST one.
//
// The last-block assertion is the whole point of the rule: the two blocks
// deliberately DIFFER, so an implementation that keeps the first (the stale
// breadcrumbs the keyword update existed to replace) fails here even though the
// block COUNT would still be 1.
func TestKeywordsTailConvergeDoubleTailKeepsLastAndBody(t *testing.T) {
	ctx := context.Background()
	db, err := OpenMemoryLedger(testutil.NewTestDBPath(t), false)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore test ledger handle closed at cleanup

	const body = "症状：这个正文里含 keywords: 这个词但不是尾块，必须逐字节保留。"
	stale, current := "old-a, old-b", "new-a, new-b"
	before := kwTailAnswer(body, stale, current)
	if _, err := db.ExecContext(ctx,
		"INSERT INTO distilled_qa (topic, question, answer, search_kw) VALUES (?,?,?,?)",
		"t", "q", before, current); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rep, err := KeywordsTailConverge(ctx, db, false)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if rep.Affected != 1 || rep.Changed != 1 {
		t.Fatalf("affected=%d changed=%d, want 1/1 (report %+v)", rep.Affected, rep.Changed, rep)
	}
	if rep.FirstDiffers != 1 {
		t.Fatalf("FirstDiffers=%d, want 1 — the blocks differ, so the choice of block must be reported", rep.FirstDiffers)
	}

	var got, gotKW string
	if err := db.QueryRowContext(ctx, "SELECT answer, search_kw FROM distilled_qa").Scan(&got, &gotKW); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if n := keywordsTailBlockCount(got); n != 1 {
		t.Fatalf("tail blocks after converge = %d, want 1 (answer %q)", n, got)
	}
	if gotBody, _ := KeywordsTailBody(got); gotBody != body {
		t.Fatalf("body moved:\n got %q\nwant %q", gotBody, body)
	}
	if want := body + keywordsTailSeparator() + current; got != want {
		t.Fatalf("kept block is not the LAST one:\n got %q\nwant %q", got, want)
	}
	// The kept block is the one that matches the row's own search_kw column —
	// the measured reason the LAST block is the current one.
	if rep.LastMatchesSearchKW != 1 || gotKW != current {
		t.Fatalf("kept block does not match search_kw (report %+v)", rep)
	}

	// Idempotent: a second run changes nothing.
	rep2, err := KeywordsTailConverge(ctx, db, false)
	if err != nil {
		t.Fatalf("second converge: %v", err)
	}
	if rep2.Changed != 0 || rep2.Affected != 0 {
		t.Fatalf("second run changed=%d affected=%d, want 0/0", rep2.Changed, rep2.Affected)
	}
}

// TestKeywordsTailConvergeLeavesNonTailKeywordsTextAlone pins the shape the
// briefing calls out: an answer that MENTIONS "keywords: " in its prose, or whose
// prose is followed by a single tail, must not be cut.
//
// This is the false-positive side of the greedy keywordsTailRe: `(?s)\n\nkeywords:
// .*$` matches to the END of the string, so the only safe convergence is one that
// strips every block and re-attaches one — never one that removes "the extra"
// block by scanning for a second occurrence, which would find a marker inside the
// prose and eat the rest of the answer.
func TestKeywordsTailConvergeLeavesNonTailKeywordsTextAlone(t *testing.T) {
	ctx := context.Background()
	db, err := OpenMemoryLedger(testutil.NewTestDBPath(t), false)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore test ledger handle closed at cleanup

	cases := []struct{ name, answer string }{
		// No tail at all; "keywords: " appears mid-prose with real text after it.
		{"prose-mentions-marker", "keep keywords: this and everything after it, because it is prose."},
		// A single tail: already converged, must not be touched.
		{"single-tail", kwTailAnswer("body text", "a, b")},
		// Mid-prose marker followed by a blank line and MORE prose, then a tail:
		// the greedy strip would swallow the prose, so the row is refused.
		{"marker-blank-line-then-prose", "intro\n\nkeywords: looks like a tail\n\nbut this is still body, and it must survive."},
	}
	for _, c := range cases {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO distilled_qa (topic, question, answer) VALUES (?,?,?)",
			c.name, c.name, c.answer); err != nil {
			t.Fatalf("seed %s: %v", c.name, err)
		}
	}

	rep, err := KeywordsTailConverge(ctx, db, false)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	// Affected may be 1: the third case carries two separators, so it LOOKS like
	// a duplicated tail. What must hold is that NOTHING was rewritten — the
	// prose after the apparent block is real body, and collapsing it would
	// delete it, so the row is refused and only counted.
	if rep.Changed != 0 || rep.Preserved != 1 {
		t.Fatalf("changed=%d preserved=%d, want 0/1 — a shape whose \"tail\" has prose after it must be refused, not cut (report %+v)",
			rep.Changed, rep.Preserved, rep)
	}
	for _, c := range cases {
		var got string
		if err := db.QueryRowContext(ctx,
			"SELECT answer FROM distilled_qa WHERE topic = ?", c.name).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", c.name, err)
		}
		if got != c.answer {
			t.Fatalf("%s was rewritten:\n got %q\nwant %q", c.name, got, c.answer)
		}
	}
}

// TestKeywordsTailConvergeTripleTailKeepsLast covers the 127 three-block rows:
// the strip is greedy, so "collapse to one" must hold for 3 as well as 2.
func TestKeywordsTailConvergeTripleTailKeepsLast(t *testing.T) {
	ctx := context.Background()
	db, err := OpenMemoryLedger(testutil.NewTestDBPath(t), false)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore test ledger handle closed at cleanup

	before := kwTailAnswer("the body", "first", "second", "third")
	if _, err := db.ExecContext(ctx,
		"INSERT INTO distilled_qa (topic, question, answer) VALUES (?,?,?)", "t", "q", before); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rep, err := KeywordsTailConverge(ctx, db, false)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if rep.Before[3] != 1 {
		t.Fatalf("before histogram %v, want a 3-block row", rep.Before)
	}
	if rep.After[2]+rep.After[3] != 0 || rep.After[1] != 1 {
		t.Fatalf("after histogram %v, want {1:1} — no duplicated block may survive", rep.After)
	}
	var got string
	if err := db.QueryRowContext(ctx, "SELECT answer FROM distilled_qa").Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if want := kwTailAnswer("the body", "third"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestKeywordsTailConvergeDryRunWritesNothing pins the --dry-run contract the
// CLI's own -h advertises: the same numbers, no write.
func TestKeywordsTailConvergeDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	db, err := OpenMemoryLedger(testutil.NewTestDBPath(t), false)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore test ledger handle closed at cleanup

	before := kwTailAnswer("body", "a", "b")
	if _, err := db.ExecContext(ctx,
		"INSERT INTO distilled_qa (topic, question, answer) VALUES (?,?,?)", "t", "q", before); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rep, err := KeywordsTailConverge(ctx, db, true)
	if err != nil {
		t.Fatalf("dry converge: %v", err)
	}
	if rep.Changed != 1 || rep.After != nil {
		t.Fatalf("dry run changed=%d after=%v, want 1/nil", rep.Changed, rep.After)
	}
	var got string
	if err := db.QueryRowContext(ctx, "SELECT answer FROM distilled_qa").Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != before {
		t.Fatalf("dry run wrote: got %q", got)
	}
}
