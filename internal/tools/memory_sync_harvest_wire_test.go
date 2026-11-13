package tools

// memory_sync_harvest_wire_test.go — the WINDOW leg's transport shape.
//
// THE DEFECT THIS PINS (same root cause as the pull leg, a second transport;
// measured 2026-10-07 against a real `sqlite3` CLI): harvestRemoteSelect wrote
// `COALESCE(deliverable,'')`, `COALESCE(run_method,'')`, `COALESCE(run_cmd,'')`,
// `COALESCE(reason,'')` and `COALESCE(parent_task_id,'')` with no alias, and
// `sqlite3 -json` names a result column after SQLite's own RESULT name: a BARE
// column keeps its name, an EXPRESSION is named by its WHOLE SOURCE TEXT. So
// the JSON keys came back as "COALESCE(reason,'')", and encoding/json — whose
// match is case-insensitive but does NOT skip punctuation — bound none of them
// to harvestRemoteRow. Every row this window ever landed carried an EMPTY
// deliverable, run_method, run_cmd, reason and parent_task_id: the dependency
// edge (Rule 33), the branch provenance the adopt gate pairs on, and the run
// provenance, all dropped in transport — no error, no counter, no log (Rule 15).
//
// WHY THIS GATE IS POSITIONAL AND NOT A SUBSTRING CHECK: the shipped, BROKEN
// query still *contains* the string `deliverable` (inside its COALESCE), so a
// strings.Contains gate stays green on the exact defect. This one parses the
// SELECT's column list, derives each column's RESULT NAME (`AS <x>` ⇒ `x`,
// else the bare column name), and compares the list, INDEX BY INDEX, with the
// json tags encoding/json actually binds (read by reflection). Dropping a
// column, leaving one unaliased, or reordering one all fail — and the red
// proof below feeds the same parser the two mutilated queries, so a future
// edit that weakens the parser cannot pass green.
//
// The wire-level instrument lives in memory_sync_remote_projection_test.go
// (TestRemoteProjectionDecodesThroughTheRealCLI), which runs the real binary on
// a real fixture ledger and decodes into the very struct this leg decodes into;
// it covers both SELECTs through the one shared projection.

import (
	"reflect"
	"strings"
	"testing"
)

// TestHarvestRemoteSelectNamesEveryColumnForTheWireStruct is the static gate
// for the window leg's transport shape.
func TestHarvestRemoteSelectNamesEveryColumnForTheWireStruct(t *testing.T) {
	// A sanity check first: the gate is only meaningful if the query is the one
	// the leg sends (and if it still carries its anchor predicate).
	if !strings.Contains(harvestRemoteSelect, harvestAnchorPredicate) {
		t.Fatalf("harvestRemoteSelect is not the window SELECT this gate reads:\n%s", harvestRemoteSelect)
	}
	fields := wireFieldNames(t, reflect.TypeOf(harvestRemoteRow{}))
	got := harvestSelectColumnKeys(t, harvestRemoteSelect)

	t.Run("positional/real-select", func(t *testing.T) {
		if len(got) != len(fields) {
			t.Fatalf("the window SELECT names %d columns, the wire struct has %d fields — the column list and the struct have drifted apart:\n  columns: %v\n  fields:  %v\n%s",
				len(got), len(fields), got, fields, harvestRemoteSelect)
		}
		for i, want := range fields {
			if got[i] != want {
				t.Fatalf("column %d of the window SELECT arrives as %q, but the wire struct binds %q there — sqlite3 -json keys a column by its RESULT name, so an unaliased expression lands as its own source text and binds to nothing:\n%s",
					i+1, got[i], want, harvestRemoteSelect)
			}
		}

		// The two facts the loop above cannot state on its own, because they
		// are about the ONE shared value rather than about one column.
		if !strings.Contains(harvestRemoteSelect, syncParentColumnNamed) {
			t.Fatalf("the window SELECT does not carry the family's shared %q — the dependency edge must be spelled once (Rule 14), aliased to the wire field (Rule 1b):\n%s",
				syncParentColumnNamed, harvestRemoteSelect)
		}
		if syncParentColumnNamed != syncParentColumnExpr+syncColumnAliasKeyword+syncParentColumnAlias {
			t.Fatalf("syncParentColumnNamed (%q) does not pair the shared expression (%q) with the wire field (%q)",
				syncParentColumnNamed, syncParentColumnExpr, syncParentColumnAlias)
		}
	})

	// THE RED PROOF, in the two shapes the defect actually takes: a column
	// DELETED, and a column left UNALIASED (the shipped bug). Both are fed to
	// the SAME parser the green arm uses, and both must be named.
	t.Run("red/dropped-column", func(t *testing.T) {
		mutilated := strings.Replace(harvestRemoteSelect, wireDroppedColumnFixture, "", 1)
		if mutilated == harvestRemoteSelect {
			t.Fatalf("fixture out of date: the window SELECT no longer contains %q:\n%s", wireDroppedColumnFixture, harvestRemoteSelect)
		}
		redKeys := harvestSelectColumnKeys(t, mutilated)
		if len(redKeys) == len(fields) {
			t.Fatalf("dropping a column left the column count unchanged — the parser is reading something other than the SELECT's column list:\n%s", mutilated)
		}
		if missing := firstDivergentField(t, fields, redKeys); missing != wireDroppedColumnField {
			t.Fatalf("the gate blamed %q for a dropped %q column — the parser does not align columns to struct fields positionally:\n%s",
				missing, wireDroppedColumnField, mutilated)
		}
	})

	t.Run("red/unaliased-expression", func(t *testing.T) {
		// The SHIPPED shape, exactly: the expression loses its ` AS <field>`.
		mutilated := strings.Replace(harvestRemoteSelect, wireAliasedColumnFixture, wireUnaliasedColumnFixture, 1)
		if mutilated == harvestRemoteSelect {
			t.Fatalf("fixture out of date: the window SELECT no longer contains %q:\n%s", wireAliasedColumnFixture, harvestRemoteSelect)
		}
		redKeys := harvestSelectColumnKeys(t, mutilated)
		if len(redKeys) != len(fields) {
			t.Fatalf("unaliasing a column changed the column COUNT (%d vs %d) — this arm must fail on the NAME, not on a shift:\n%s",
				len(redKeys), len(fields), mutilated)
		}
		if missing := firstDivergentField(t, fields, redKeys); missing != wireDroppedColumnField {
			t.Fatalf("the gate blamed %q for the unaliased %q expression — the parser is not reading the column's RESULT name:\n%s",
				missing, wireDroppedColumnField, mutilated)
		}
	})
}

// The red proof's fixtures, named (Rule 1: a meaning-carrying literal in one
// place). The column under test is `deliverable`: it is the FIRST coalesced
// column of the shared tail, so a shift/binding error is unambiguous.
const (
	wireDroppedColumnField     = "deliverable"
	wireDroppedColumnFixture   = "COALESCE(deliverable,'') AS deliverable,\n"
	wireAliasedColumnFixture   = "COALESCE(deliverable,'') AS deliverable"
	wireUnaliasedColumnFixture = "COALESCE(deliverable,'')"
)

// firstDivergentField returns the wire field name at the first index where the
// parsed columns disagree with the struct's tags ("" when they all agree).
func firstDivergentField(t *testing.T, fields, keys []string) string {
	t.Helper()
	for i, want := range fields {
		if i >= len(keys) || keys[i] != want {
			return want
		}
	}
	return ""
}

// wireFieldNames returns a struct type's json tag names in field order — the
// keys encoding/json will bind. A field with no tag would bind to its Go name,
// which no SQL column spells, so it is refused here rather than silently
// compared.
func wireFieldNames(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	out := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.TrimSpace(typ.Field(i).Tag.Get("json"))
		name := strings.Split(tag, ",")[0]
		if name == "" {
			t.Fatalf("wire struct field %s carries no json tag — the wire key is what this gate compares, so a field without one cannot be checked", typ.Field(i).Name)
		}
		out = append(out, name)
	}
	return out
}

// harvestSelectColumnKeys reads the RESULT NAME of each column in the SELECT's
// column list: the token after `AS` when the column is an expression (trailing
// whitespace trimmed), otherwise the bare column name. It mirrors what
// `sqlite3 -json` does — key a result column by SQLite's own result name.
func harvestSelectColumnKeys(t *testing.T, query string) []string {
	t.Helper()
	start := strings.Index(query, harvestSelectHead)
	end := strings.Index(query, harvestAnchorPredicate)
	if start < 0 || end < 0 || end < start {
		t.Fatalf("cannot read a column list out of this query:\n%s", query)
	}
	list := query[start+len(harvestSelectHead) : end]
	keys := make([]string, 0, 16)
	for _, col := range splitSQLList(list) {
		col = strings.Join(strings.Fields(col), " ")
		if col == "" {
			continue
		}
		if at := strings.LastIndex(col, syncColumnAliasKeyword); at >= 0 {
			col = strings.TrimSpace(col[at+len(syncColumnAliasKeyword):])
		}
		keys = append(keys, col)
	}
	return keys
}

// harvestSelectHead / harvestAnchorPredicate delimit the column list this gate
// reads. The predicate is spelled once here as the gate's own boundary marker
// AND asserted to occur in the query (the two uses are the same fact: the tail
// of the column list IS what precedes the anchor predicate).
const (
	harvestSelectHead      = "SELECT "
	harvestAnchorPredicate = "FROM tasks WHERE updated_at >"
)
