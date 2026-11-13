// cli_verb_classify_test.go — the gate over the census's CONCEPT-DIMENSION
// CLASSIFICATION (the `| verb | class | status | reason |` table in
// devdoc/gopi-verb-census.md).
//
// WHY THIS GATE EXISTS, and what it does NOT do. The classification decides
// which words stay top-level, and that is a JUDGMENT — a gate cannot argue with
// a reason, and this one does not try. What a gate CAN do, and what makes the
// judgment re-usable, is hold the SHAPE of the argument fixed:
//
//   - COVERAGE, both directions. Every verb the dispatch chain dispatches has
//     exactly one row here, and no row names a verb nothing dispatches. So a new
//     top-level verb cannot land without a decision about whether it IS a
//     dimension — the omission fails the build instead of being noticed in a
//     review six months later. The population is re-derived from main.go by the
//     same reader the G7 census gates use (censusDispatched), never hand-listed
//     here (Rule 13: a second list is a list that drifts).
//   - CONSISTENCY of the vocabulary. A class is `concept` or
//     `demote-to:<parent>`; a status is `n/a` for a concept and `landed`/`next`
//     for a demotion; a parent must itself be a dispatched verb and not the row
//     itself. Each is a rule the criterion implies, so a row that breaks one is
//     a row whose author was not using the criterion.
//   - A REASON per row. The reason is the deliverable (it is what a reader
//     argues with); an empty or stub reason is a row that decided nothing.
//   - NON-VACUITY, so the checks above cannot pass by looking at nothing: the
//     table must clear floors on total rows, on each class, and on distinct
//     parents. A parse that found nothing — the failure mode where a gate over
//     an empty list passes everything — fails HERE.
//
// The `landed` STATUS is NOT machine-checked, and that is a stated limit rather
// than an oversight: "the demotion is in tree" is a fact about a dispatch
// reachable under a parent, and pinning it generally would mean the gate
// evaluating each family's own sub-verb list (a Go var, per family). It is
// verified the way Rule 31 requires instead — by RUNNING the surface: the row's
// evidence is a real argv under the parent in the task report, and
// cmd/agent/cli_session_test.go drives the four session aliases.
package main

import (
	"os"
	"strings"
	"testing"
)

const (
	// The classification table's own header — the gate locates the table by
	// THIS line, so the doc may carry any amount of prose around it.
	classifyHeaderLine = "| verb | class | status | reason |"

	// The class vocabulary. A top-level verb either IS an independent concept
	// dimension or is demoted under the concept that owns its subject.
	classifyConcept = "concept"
	classifyDemote  = "demote-to:"

	// The status vocabulary, and the one pairing rule: a concept has nothing to
	// demote (`n/a`); a demotion is either in tree (`landed`) or agreed and not
	// yet done (`next` — the standing agenda).
	classifyStatusNA     = "n/a"
	classifyStatusLanded = "landed"
	classifyStatusNext   = "next"

	// Non-vacuity floors. Measured at master 156e2e69 (2026-10-05): 43 verbs
	// dispatched, 21 concept, 22 demote-to. The floors sit below that so ordinary
	// churn does not trip them, but a table that collapsed — a parse that found
	// nothing, a doc truncated by a merge — lands far under them.
	classifyMinRows        = 25
	classifyMinConcepts    = 8
	classifyMinDemotions   = 8
	classifyMinParents     = 4
	classifyMinReasonChars = 24
)

// classifyRow is one parsed row of the classification table.
type classifyRow struct {
	verb   string
	class  string
	parent string // non-empty only for a demotion
	status string
	reason string
}

// classifyParseTable reads the `| verb | class | status | reason |` table. The
// table ends at the first line that is not a `|` row, so prose may follow it
// (the census's own "what this does NOT claim" section does).
func classifyParseTable(t *testing.T, doc string) []classifyRow {
	t.Helper()
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == classifyHeaderLine {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s carries no classification table: the header line %q is absent — "+
			"the classification is the deliverable this gate guards", censusDocPath, classifyHeaderLine)
	}
	var rows []classifyRow
	for _, l := range lines[start:] {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "|") {
			break
		}
		cells := censusSplitRow(l)
		if censusSeparatorRow(cells) {
			continue
		}
		if len(cells) != 4 {
			t.Fatalf("%s: classification row %q has %d cells, want 4 (verb|class|status|reason)", censusDocPath, l, len(cells))
		}
		r := classifyRow{verb: cells[0], class: cells[1], status: cells[2], reason: cells[3]}
		if strings.HasPrefix(r.class, classifyDemote) {
			r.parent = strings.TrimPrefix(r.class, classifyDemote)
		}
		rows = append(rows, r)
	}
	return rows
}

// TestVerbClassificationCoversEveryDispatchedVerb is the classification's own
// gate: coverage in both directions, a consistent vocabulary, a real reason per
// row, and non-vacuity floors. See the file header for what it deliberately does
// not check.
func TestVerbClassificationCoversEveryDispatchedVerb(t *testing.T) {
	doc, err := os.ReadFile(censusDocPathAbs(t))
	if err != nil {
		t.Fatalf("read %s: %v — the classification is the deliverable; without it the surface is undecided", censusDocPath, err)
	}
	rows := classifyParseTable(t, string(doc))
	dispatched, _ := censusDispatched(t)

	byVerb := map[string]classifyRow{}
	for _, r := range rows {
		if r.verb == "" {
			t.Errorf("%s: a classification row names no verb: %q", censusDocPath, r.class+"|"+r.status)
			continue
		}
		if _, dup := byVerb[r.verb]; dup {
			t.Errorf("%s: `%s` has more than one classification row — a verb is a row, not a section", censusDocPath, r.verb)
		}
		byVerb[r.verb] = r
	}

	// direction 1: every dispatched verb is classified. This is the check that
	// makes a NEW top-level verb fail until somebody decides whether it IS a
	// dimension — the whole point of classifying ahead of the next demotion.
	for v := range dispatched {
		if _, ok := byVerb[v]; !ok {
			t.Errorf("`gopi %s` is dispatched by cmd/agent/main.go but has NO classification row in %s — "+
				"a new top-level verb must state whether it is an independent concept dimension or an operation on one "+
				"(the criterion is in the doc's classification section)", v, censusDocPath)
		}
	}

	// direction 2: no row invents a verb. A row is legitimate when the binary
	// still KNOWS the word — either it dispatches it, or it is a word that LEFT
	// the dispatch chain and is now REFUSED BY NAME with a pointer (the
	// `demote-to:<parent> … landed` rows: verb-retirement phase 1 moved
	// log/stats/raw/resume under `session`, and their rows are the record of
	// that decision). Both halves are derived, never hand-listed: the refusal
	// vocabulary is cli_verb_aliases.go's own table.
	for _, r := range rows {
		if dispatched[r.verb] {
			continue
		}
		if verbAliasCanonical(r.verb) != "" {
			continue
		}
		t.Errorf("classification row %q names a verb cmd/agent/main.go does not dispatch and the refusal vocabulary does not know — "+
			"either the verb was renamed (update the row) or the row was invented", r.verb)
	}

	// the vocabulary, and the rules the criterion implies.
	concepts, demotions := 0, 0
	parents := map[string]bool{}
	for v, r := range byVerb {
		switch {
		case r.class == classifyConcept:
			concepts++
			if r.status != classifyStatusNA {
				t.Errorf("`%s` is classed %s but its status is %q — a concept has nothing to demote, so its status is %q",
					v, classifyConcept, r.status, classifyStatusNA)
			}
		case strings.HasPrefix(r.class, classifyDemote):
			demotions++
			if r.parent == "" {
				t.Errorf("`%s` is classed %q with no parent — the class must name the verb that owns its subject", v, r.class)
			} else {
				if r.parent == v {
					t.Errorf("`%s` is demoted to itself — a demotion must name a DIFFERENT verb as the owner", v)
				}
				if !dispatched[r.parent] {
					t.Errorf("`%s` is demoted to parent %q, which cmd/agent/main.go does not dispatch — "+
						"a demotion's parent must be a real verb to land under", v, r.parent)
				}
				parents[r.parent] = true
			}
			if r.status != classifyStatusLanded && r.status != classifyStatusNext {
				t.Errorf("`%s` is a demotion with status %q — want %q (in tree) or %q (agreed, not yet done)",
					v, r.status, classifyStatusLanded, classifyStatusNext)
			}
		default:
			t.Errorf("`%s` has class %q — the vocabulary is %q or %q<parent>",
				v, r.class, classifyConcept, classifyDemote)
		}
		// the reason IS the deliverable: it is the sentence a reader argues with,
		// so a stub is a row that decided nothing.
		if n := len(strings.TrimSpace(r.reason)); n < classifyMinReasonChars {
			t.Errorf("`%s`: the reason is %d chars — a classification is a written argument, not a label "+
				"(state the verb's SUBJECT and why it is, or is not, its own dimension)", v, n)
		}
	}

	// non-vacuity: floors on the table's OWN size, each class, and the parents.
	// Without these, a parse that matched nothing satisfies every check above.
	if len(rows) < classifyMinRows {
		t.Fatalf("%s: the classification carries %d rows — below the non-vacuity floor of %d. "+
			"A classification this small is a broken table, not a small surface.", censusDocPath, len(rows), classifyMinRows)
	}
	if concepts < classifyMinConcepts {
		t.Fatalf("%s: only %d rows are classed %s — below the floor of %d; a table where nearly everything is demoted "+
			"is a table whose criterion was applied to nothing", censusDocPath, concepts, classifyConcept, classifyMinConcepts)
	}
	if demotions < classifyMinDemotions {
		t.Fatalf("%s: only %d rows are demotions — below the floor of %d", censusDocPath, demotions, classifyMinDemotions)
	}
	if len(parents) < classifyMinParents {
		t.Fatalf("%s: the demotions name only %d distinct parents — below the floor of %d; "+
			"a mere handful of parents usually means the parent column was mis-parsed", censusDocPath, len(parents), classifyMinParents)
	}

	t.Logf("classification: %d rows (%d concept, %d demote-to across %d parents), %d dispatched verbs covered",
		len(rows), concepts, demotions, len(parents), len(dispatched))
}
