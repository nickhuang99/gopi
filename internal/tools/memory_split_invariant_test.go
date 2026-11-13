package tools

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// The separability invariant behind the fix: our ASCII boundary split must
// agree with the index's own tokenizer. It is deliberately in its OWN file,
// because it calls the internal helper memsearchMatchTerms — a test that
// references the fix cannot be compiled against the pre-fix source, and a
// compile error is not the RED observation the regression owes (an assertion
// failure on the real symptom is).
// ---------------------------------------------------------------------------
// The separability check: our ASCII boundary split must agree with the index's
// own tokenizer.
// ---------------------------------------------------------------------------

// ftsBoundaryProbes are the fields whose boundary behaviour the split must
// reproduce, including a multi-byte token (the tokenizer is unicode61, so a
// "word" is not ASCII-only).
var ftsBoundaryProbes = []string{
	"token-free", "no-", "-free", "x--y", "a_b", "a.b", "a:b", "a/b", "a+b",
	"a^b", "a*b", "a(b)", "a'b", `a"b`, "a\\b", "123-abc", "Ünïcode-wörd",
	"naïve-café", "a-b-c-d", "9-9",
}

// TestFTSBoundarySplitMatchesUnicode61 is the separability check behind the
// fix's central assumption: quoting every ASCII PIECE (rather than wrapping the
// whole field in one phrase) is only correct if the pieces are exactly the
// tokens the unicode61 index stores for that field. A piece the tokenizer does
// not produce would make the route's all-AND expression match NOTHING — a
// silent recall loss that looks like "no distilled memory found".
//
// The instrument is not a raw-vs-quoted MATCH comparison: an FTS5 bareword is
// not a plain AND (a raw field the grammar refuses, or one it reads as a
// column/NEAR construct, has no comparable row set at all). The invariant
// checked is the one the route needs, per probe: the split's all-AND expression
// must MATCH the row holding that field VERBATIM, and so must each individual
// piece. A real fts5 unicode61 table on this build decides, not a rule restated
// in Go.
func TestFTSBoundarySplitMatchesUnicode61(t *testing.T) {
	// db-scaffold-ok: :memory: opens no file and resolves no path, so the
	// operator's append-only ledger is unreachable from this test — there is
	// nothing for the scaffold to pin. The scaffold's writer fixture
	// (testutil.NewTestDBPath) applies the LEDGER's schema, which is the wrong
	// instrument for asking a bare fts5 tokenizer a question; and this package
	// cannot import internal/testutil for it (testutil's own comment records
	// the cycle: it must not own internal/tools' schema).
	// The DSN comes from the package's ONE in-memory definition, not a literal:
	// the ledger DSN contract gate (cmd/agent/ledger_dsn_contract_test.go) scans
	// this tree and flags a bare sqlite DSN argument, and a re-typed ":memory:"
	// was the one hit that kept the gate red on master. The in-memory class is
	// the contract's documented exemption (LedgerMemoryDSN's doc comment carries
	// the measurement), so the fix is to NAME the DSN, never to widen the gate to
	// a value it recognises by content.
	db, err := sql.Open("sqlite", LedgerMemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`CREATE VIRTUAL TABLE probe USING fts5(x, tokenize='unicode61')`); err != nil {
		t.Fatalf("fts5 unavailable on this build: %v", err)
	}
	for i, p := range ftsBoundaryProbes {
		if _, err := db.ExecContext(ctx, `INSERT INTO probe(rowid, x) VALUES (?, ?)`, i+1, p); err != nil {
			t.Fatalf("insert probe %q: %v", p, err)
		}
	}
	// matchesRow reports whether match selects the probe's own rowid.
	matchesRow := func(match string, rowid int) bool {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT rowid FROM probe WHERE probe MATCH ?`, match)
		if err != nil {
			// A MATCH expression the engine refuses is itself a finding: the
			// split's spelling must be one the grammar accepts.
			t.Errorf("the split's spelling %q is not a legal MATCH expression: %v", match, err)
			return false
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			if id == rowid {
				return true
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return false
	}
	for i, probe := range ftsBoundaryProbes {
		terms := memsearchMatchTerms(probe)
		if len(terms) == 0 {
			t.Errorf("probe %q produced no terms", probe)
			continue
		}
		if !matchesRow(strings.Join(terms, " AND "), i+1) {
			t.Errorf("probe %q: the split's all-AND expression %q does not match the row holding %q verbatim "+
				"— the pieces are not the tokens the index stores, so the route can never recall that row",
				probe, strings.Join(terms, " AND "), probe)
			continue
		}
		for _, term := range terms {
			if !matchesRow(term, i+1) {
				t.Errorf("probe %q: piece %s alone does not match the row holding %q verbatim", probe, term, probe)
			}
		}
	}
}
