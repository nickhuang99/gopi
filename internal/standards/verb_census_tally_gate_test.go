package standardgate

// THE DOCUMENT'S OWN ARITHMETIC — devdoc/gopi-verb-census.md carries TWO tables
// that must agree: the per-class tally ("## The answer, in one line per class")
// and the verb table itself ("## The table"). They drifted exactly once and the
// drift survived review: commit 6ccb7a49 dropped the `merge` ROW when the verb
// was removed, and left the prose reading "17 rows: 16 + 1" and the class table
// reading `a:test | 16` over a 14-row-and-two-b-row table. Nothing caught it —
// the existing arm, TestVerbCensusMatchesTheDispatch, re-derives the DISPATCHED
// set against the built binary and is heavy (heavytest.CriterionDeviceExec), so
// it never runs in a work context; and the census-evidence gate (Rule 38) asks
// only whether the document NAMES a generator and pins a commit, never whether
// its own numbers add up.
//
// SO THIS GATE ASKS ONE QUESTION, AND IT NEEDS NO BINARY: does the class table
// still count the rows the document itself prints? It reads the ONE file, in
// pure text, and it is cheap enough to run on every change:
//
//	go test ./internal/standards/ -run TestVerbCensusTallyMatchesItsOwnTable
//
// The predicate is both-directional, because the two failure shapes are as
// different as they are cheap to confuse:
//
//   - a class the ROWS carry but the class table counts differently (or omits)
//     → red: the tally is stale against the table it claims to summarise;
//   - a class the class table counts ABOVE ZERO with no row carrying it → red:
//     the table lost a row and the tally invented the population back.
//
// A class counted ZERO with no row is NOT red: `b:runner-line`, `b:help-only`
// and `c:neither` are the vocabulary's empty cells, and the empty class is the
// census's product (see the document's own row descriptions).
//
// Rule 1/13: every literal lives in a constant below — there is no bare number
// and no bare heading string in the body.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// verbCensusDocPath is the ONE document this gate reads, relative to the
	// module root the test walks up to.
	verbCensusDocPath = "devdoc/gopi-verb-census.md"

	// verbCensusClassHeading / verbCensusTableHeading are the two section
	// headings whose tables are compared. They are the document's own words —
	// renaming a heading here without renaming it there refuses the document
	// rather than passing a sweep that read nothing.
	verbCensusClassHeading = "## The answer, in one line per class"
	verbCensusTableHeading = "## The table"

	// verbCensusMarkdownCell is the table column separator, and
	// verbCensusCodeTick strips the backticks around a verb/class name.
	verbCensusMarkdownCell = "|"
	verbCensusCodeTick     = "`"

	// verbCensusMinRows / verbCensusMinClasses are the non-vacuity floors. A
	// table that lost most of its rows is a broken table, not a small surface
	// (the same reasoning censusMinRows carries in cmd/agent). The class floor
	// is 5 because the vocabulary has six cells and the document must keep
	// naming the empty ones.
	verbCensusMinRows    = 8
	verbCensusMinClasses = 5

	// verbCensusCellClassHeader is the header cell both tables name their class
	// column with — the row that must never be counted as a data row.
	verbCensusCellClassHeader = "class"

	// verbCensusClassCell / verbCensusCountCell are the column indexes each
	// table's row is read from.
	verbCensusClassCell = 0
	verbCensusCountCell = 1
)

// verbCensusSection returns the contiguous markdown table lines that follow the
// first line equal to heading (leading/trailing space ignored) in the document,
// stopping at the first non-table line. A section that is absent, or that holds
// no table line at all, is an error — "read nothing" is never a pass.
func verbCensusSection(doc, heading string) ([]string, error) {
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != heading {
			continue
		}
		var rows []string
		started := false
		for _, next := range lines[i+1:] {
			isRow := strings.HasPrefix(strings.TrimSpace(next), verbCensusMarkdownCell)
			if !isRow {
				if !started && strings.TrimSpace(next) == "" {
					continue // the blank line between a heading and its table
				}
				break
			}
			started = true
			rows = append(rows, next)
		}
		if len(rows) == 0 {
			return nil, errVerbCensus("section %q holds no table lines", heading)
		}
		return rows, nil
	}
	return nil, errVerbCensus("section %q is absent from %s", heading, verbCensusDocPath)
}

// verbCensusCells splits one markdown table line into trimmed cells, dropping
// the empty leading/trailing cell the outer pipes produce.
func verbCensusCells(row string) []string {
	parts := strings.Split(strings.TrimSpace(row), verbCensusMarkdownCell)
	var cells []string
	for _, part := range parts {
		cells = append(cells, strings.Trim(strings.TrimSpace(part), verbCensusCodeTick))
	}
	if len(cells) > 0 && cells[0] == "" {
		cells = cells[1:]
	}
	if n := len(cells); n > 0 && cells[n-1] == "" {
		cells = cells[:n-1]
	}
	return cells
}

// verbCensusIsSeparator reports whether a table line is the `|---|` rule rather
// than a data row.
func verbCensusIsSeparator(row string) bool {
	stripped := strings.NewReplacer(verbCensusMarkdownCell, "", "-", "", " ", "", ":", "").Replace(row)
	return stripped == ""
}

// verbCensusRowClasses reads the verb table and returns, per class, how many
// ROWS carry it, plus the row count. The class is the LAST cell of each row —
// the column order is the document's, and the last cell is the class by
// construction (it is the only free-form column and the table's header names it
// `class`).
func verbCensusRowClasses(doc string) (map[string]int, int, error) {
	rows, err := verbCensusSection(doc, verbCensusTableHeading)
	if err != nil {
		return nil, 0, err
	}
	counts := map[string]int{}
	rowsRead := 0
	for _, row := range rows {
		if verbCensusIsSeparator(row) {
			continue
		}
		cells := verbCensusCells(row)
		if len(cells) == 0 {
			continue
		}
		class := cells[len(cells)-1]
		if class == "" || strings.EqualFold(class, verbCensusCellClassHeader) {
			continue // the header row
		}
		counts[class]++
		rowsRead++
	}
	if rowsRead < verbCensusMinRows {
		return nil, 0, errVerbCensus("verb table holds %d rows (floor %d) — a table that lost most of its rows is not a small surface",
			rowsRead, verbCensusMinRows)
	}
	return counts, rowsRead, nil
}

// verbCensusClassCounts reads the per-class tally table: `| class | count | … |`.
func verbCensusClassCounts(doc string) (map[string]int, error) {
	rows, err := verbCensusSection(doc, verbCensusClassHeading)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, row := range rows {
		if verbCensusIsSeparator(row) {
			continue
		}
		cells := verbCensusCells(row)
		if len(cells) <= verbCensusCountCell {
			continue
		}
		name := cells[verbCensusClassCell]
		if name == "" || strings.EqualFold(name, verbCensusCellClassHeader) {
			continue // the header row
		}
		n, convErr := strconv.Atoi(cells[verbCensusCountCell])
		if convErr != nil {
			return nil, errVerbCensus("class table row %q carries a non-numeric count %q", name, cells[verbCensusCountCell])
		}
		counts[name] = n
	}
	if len(counts) < verbCensusMinClasses {
		return nil, errVerbCensus("class table names %d classes (floor %d) — the vocabulary must keep its empty cells",
			len(counts), verbCensusMinClasses)
	}
	return counts, nil
}

// verbCensusTallyDisagreement is the gate's predicate: it returns one sentence
// per disagreement between the two tables, empty when they agree.
func verbCensusTallyDisagreement(doc string) ([]string, int, error) {
	rowCounts, rows, err := verbCensusRowClasses(doc)
	if err != nil {
		return nil, 0, err
	}
	classCounts, err := verbCensusClassCounts(doc)
	if err != nil {
		return nil, 0, err
	}
	var complaints []string
	for class, want := range rowCounts {
		got, named := classCounts[class]
		if !named {
			complaints = append(complaints, "class "+class+" is carried by "+strconv.Itoa(want)+
				" row(s) but the class table does not name it at all")
			continue
		}
		if got != want {
			complaints = append(complaints, "class "+class+" reads "+strconv.Itoa(got)+
				" in the class table but "+strconv.Itoa(want)+" row(s) carry it")
		}
	}
	for class, got := range classCounts {
		if got == 0 {
			continue // an empty cell: the vocabulary's own zero, no row to find
		}
		if _, used := rowCounts[class]; !used {
			complaints = append(complaints, "class "+class+" reads "+strconv.Itoa(got)+
				" in the class table but NO row carries it — the table lost a row and the tally kept counting it")
		}
	}
	sum := 0
	for _, n := range classCounts {
		sum += n
	}
	if sum != rows {
		complaints = append(complaints, "the class table sums to "+strconv.Itoa(sum)+
			" but the verb table holds "+strconv.Itoa(rows)+" row(s)")
	}
	return complaints, rows, nil
}

// errVerbCensus builds a readable refusal. There is nothing to wrap here —
// these are this gate's own sentences about its own reading — so the format
// verb is %s and no %w is owed.
func errVerbCensus(format string, args ...any) error {
	return fmt.Errorf("verb-census tally: "+format, args...)
}

// TestVerbCensusTallyMatchesItsOwnTable is the real-tree arm: the checked-in
// document's class tally must count the rows that same document prints.
func TestVerbCensusTallyMatchesItsOwnTable(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, verbCensusDocPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — a gate that could not open the document must fail, not pass", path, err)
	}
	complaints, rows, err := verbCensusTallyDisagreement(string(raw))
	if err != nil {
		t.Fatalf("%s: %v", verbCensusDocPath, err)
	}
	classCounts, err := verbCensusClassCounts(string(raw))
	if err != nil {
		t.Fatalf("%s: %v", verbCensusDocPath, err)
	}
	t.Logf("%s: rows=%d classes=%d", verbCensusDocPath, rows, len(classCounts))
	for class, n := range classCounts {
		t.Logf("  %-14s %d", class, n)
	}
	if len(complaints) > 0 {
		for _, c := range complaints {
			t.Errorf("%s: %s", verbCensusDocPath, c)
		}
		t.Fatalf("%d disagreement(s) between the class table and the verb table — re-derive with `bash scripts/g7-verb-census.sh ./bin/gopi` (Rule 38: never hand-count)",
			len(complaints))
	}
}

// TestVerbCensusTallyGateRefusesAPlantedMismatch is the red proof: the SAME
// predicate, over documents this test writes and owns, must refuse each
// disagreement shape — and accept the healthy control, so a predicate that
// refuses everything cannot pass as strict.
func TestVerbCensusTallyGateRefusesAPlantedMismatch(t *testing.T) {
	cases := []struct {
		name        string
		doc         string
		wantFailure bool
		wantSubstr  string
	}{
		{
			name:        "healthy control: tally counts its own rows",
			doc:         verbCensusFixture(verbCensusFixtureHealthy),
			wantFailure: false,
		},
		{
			name:        "a class count that does not match the rows carrying it",
			doc:         verbCensusFixture(verbCensusFixtureCountDrift),
			wantFailure: true,
			wantSubstr:  "row(s) carry it",
		},
		{
			name:        "a count kept for a class whose rows are all gone",
			doc:         verbCensusFixture(verbCensusFixtureOrphanCount),
			wantFailure: true,
			wantSubstr:  "NO row carries it",
		},
		{
			name:        "a row whose class the tally never names",
			doc:         verbCensusFixture(verbCensusFixtureUnnamedClass),
			wantFailure: true,
			wantSubstr:  "does not name it at all",
		},
		{
			name:        "a document with the table section gone (the vacuum)",
			doc:         verbCensusFixture(verbCensusFixtureNoTable),
			wantFailure: true,
			wantSubstr:  "holds no table lines",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			complaints, _, err := verbCensusTallyDisagreement(tc.doc)
			failed := err != nil || len(complaints) > 0
			if !tc.wantFailure {
				if failed {
					t.Fatalf("the healthy control was refused: err=%v complaints=%v", err, complaints)
				}
				return
			}
			if !failed {
				t.Fatalf("the planted mismatch passed — the gate does not read what it claims to read")
			}
			reason := ""
			if err != nil {
				reason = err.Error()
			}
			joined := strings.Join(complaints, "\n") + "\n" + reason
			if !strings.Contains(joined, tc.wantSubstr) {
				t.Fatalf("refused for the wrong reason: want %q, got %q (err=%v)", tc.wantSubstr, joined, err)
			}
		})
	}
}

// The planted documents. Each is a whole census document (a class table, then a
// verb table) so the predicate reads them exactly as it reads the real one. The
// verb tables carry nine rows on purpose: the non-vacuity floor (verbCensusMinRows)
// is 8, and a fixture below it would be refused for being too small rather than
// for the disagreement it plants.
const (
	verbCensusFixtureHealthy = `# Census fixture

## The answer, in one line per class

| class | count | meaning |
|---|---|---|
| ` + "`a:test`" + ` | 6 | driven |
| ` + "`b:usage-log`" + ` | 2 | logged |
| ` + "`b:doc-recipe`" + ` | 1 | recipe |
| ` + "`b:runner-line`" + ` | 0 | weaker |
| ` + "`b:help-only`" + ` | 0 | help |
| ` + "`c:neither`" + ` | 0 | nothing |

## The table

| verb | dispatched | advertised | class |
|---|---|---|---|
| ` + "`alpha`" + ` | 1 | 1 | a:test |
| ` + "`beta`" + ` | 1 | 1 | a:test |
| ` + "`gamma`" + ` | 1 | 1 | a:test |
| ` + "`delta`" + ` | 1 | 1 | a:test |
| ` + "`epsilon`" + ` | 1 | 1 | a:test |
| ` + "`zeta`" + ` | 1 | 1 | a:test |
| ` + "`eta`" + ` | 1 | 0 | b:usage-log |
| ` + "`theta`" + ` | 1 | 0 | b:usage-log |
| ` + "`iota`" + ` | 1 | 0 | b:doc-recipe |
`

	// The 6ccb7a49 shape verbatim: rows were dropped, the tally kept counting them.
	verbCensusFixtureCountDrift = `# Census fixture

## The answer, in one line per class

| class | count | meaning |
|---|---|---|
| ` + "`a:test`" + ` | 8 | driven |
| ` + "`b:usage-log`" + ` | 2 | logged |
| ` + "`b:doc-recipe`" + ` | 1 | recipe |
| ` + "`b:runner-line`" + ` | 0 | weaker |
| ` + "`b:help-only`" + ` | 0 | help |
| ` + "`c:neither`" + ` | 0 | nothing |

## The table

| verb | dispatched | advertised | class |
|---|---|---|---|
| ` + "`alpha`" + ` | 1 | 1 | a:test |
| ` + "`beta`" + ` | 1 | 1 | a:test |
| ` + "`gamma`" + ` | 1 | 1 | a:test |
| ` + "`delta`" + ` | 1 | 1 | a:test |
| ` + "`epsilon`" + ` | 1 | 1 | a:test |
| ` + "`zeta`" + ` | 1 | 1 | a:test |
| ` + "`eta`" + ` | 1 | 0 | b:usage-log |
| ` + "`theta`" + ` | 1 | 0 | b:usage-log |
| ` + "`iota`" + ` | 1 | 0 | b:doc-recipe |
`

	verbCensusFixtureOrphanCount = `# Census fixture

## The answer, in one line per class

| class | count | meaning |
|---|---|---|
| ` + "`a:test`" + ` | 7 | driven |
| ` + "`b:usage-log`" + ` | 2 | logged |
| ` + "`b:doc-recipe`" + ` | 1 | recipe |
| ` + "`b:runner-line`" + ` | 0 | weaker |
| ` + "`b:help-only`" + ` | 0 | help |
| ` + "`c:neither`" + ` | 0 | nothing |

## The table

| verb | dispatched | advertised | class |
|---|---|---|---|
| ` + "`alpha`" + ` | 1 | 1 | a:test |
| ` + "`beta`" + ` | 1 | 1 | a:test |
| ` + "`gamma`" + ` | 1 | 1 | a:test |
| ` + "`delta`" + ` | 1 | 1 | a:test |
| ` + "`epsilon`" + ` | 1 | 1 | a:test |
| ` + "`zeta`" + ` | 1 | 1 | a:test |
| ` + "`eta`" + ` | 1 | 1 | a:test |
| ` + "`theta`" + ` | 1 | 0 | b:doc-recipe |
`

	verbCensusFixtureUnnamedClass = `# Census fixture

## The answer, in one line per class

| class | count | meaning |
|---|---|---|
| ` + "`a:test`" + ` | 7 | driven |
| ` + "`b:usage-log`" + ` | 1 | logged |
| ` + "`b:runner-line`" + ` | 0 | weaker |
| ` + "`b:help-only`" + ` | 0 | help |
| ` + "`c:neither`" + ` | 0 | nothing |

## The table

| verb | dispatched | advertised | class |
|---|---|---|---|
| ` + "`alpha`" + ` | 1 | 1 | a:test |
| ` + "`beta`" + ` | 1 | 1 | a:test |
| ` + "`gamma`" + ` | 1 | 1 | a:test |
| ` + "`delta`" + ` | 1 | 1 | a:test |
| ` + "`epsilon`" + ` | 1 | 1 | a:test |
| ` + "`zeta`" + ` | 1 | 1 | a:test |
| ` + "`eta`" + ` | 1 | 1 | a:test |
| ` + "`theta`" + ` | 1 | 0 | b:usage-log |
| ` + "`iota`" + ` | 1 | 0 | b:doc-recipe |
`

	verbCensusFixtureNoTable = `# Census fixture

## The answer, in one line per class

| class | count | meaning |
|---|---|---|
| ` + "`a:test`" + ` | 6 | driven |
| ` + "`b:usage-log`" + ` | 2 | logged |
| ` + "`b:doc-recipe`" + ` | 1 | recipe |
| ` + "`b:runner-line`" + ` | 0 | weaker |
| ` + "`b:help-only`" + ` | 0 | help |

## The table

no table here at all
`
)

func verbCensusFixture(body string) string { return body }
