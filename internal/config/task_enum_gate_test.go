package config

// task_enum_gate_test.go — the RED-PROOF companion to
// TestTopLevelUsageShowsTaskLedger / TestTopLevelUsageTaskVerbEnumMatchesCompletion.
//
// Those two tests hold `gopi -h` and `gopi task <TAB>` equal because both
// derive from internal/taskverbs.FamilyVerbs. That equality alone does NOT
// stop a future agent from re-inlining the enumeration as a hand-typed list
// on the help side (a green guard you have not seen fail is a vacuous guard:
// reverting the help row to its old literal — the state that was red on
// master for `status` — must go RED here, not merely stay green).
//
// This gate closes that hole the Rule 13 way: it scans the production source
// and fails any file that carries the family enumeration as a LITERAL LIST
// instead of referencing the shared constant. A hand-typed list is exactly
// what drifted in the first place (d7474fb4 added `status` to the completion
// set and to tasksUsage, but the top-level row's literal list was maintained
// by hand and missed it).

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverbs"
)

// familyEnumerationFile is the ONE file allowed to spell the whole verb
// sequence: the leaf package that defines it.
const familyEnumerationFile = "../../internal/taskverbs/verbs.go"

// enumerationRunRe matches a literal list of family verbs: at least two
// consecutive verbs in the taskverbs order separated by a comma. The optional
// `"` around each separator is deliberate — a hand-typed re-inline in Go is a
// QUOTED string list (`"adopt", "archive", …`), while the definition site here
// is a bare-identifier list (`Adopt, Archive, …`); both must match, so a
// quoted list cannot slip past the gate (caught by the red-proof probe, which
// a quote-blind separator let through). Real separators seen in the tree:
// ", " (same line), ",\n\t" (wrapped literal row).
func enumerationRunRe(verbs []string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString(regexp.QuoteMeta(verbs[0]))
	for _, v := range verbs[1:] {
		b.WriteString(`"?\s*,\s*"?`)
		b.WriteString(regexp.QuoteMeta(v))
	}
	return regexp.MustCompile(b.String())
}

// TestNoProductionFileRestatesTheTaskVerbEnumeration is the anti-regression
// guard: the enumerated family surface has exactly ONE spelling site.
func TestNoProductionFileRestatesTheTaskVerbEnumeration(t *testing.T) {
	re := enumerationRunRe(taskverbs.FamilyVerbs)
	roots := []string{"..", "../../cmd"}
	checked := 0
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.Clean(path) == filepath.Clean(familyEnumerationFile) {
				return nil // the definition site — the ONE allowed holder
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			checked++
			if loc := re.FindIndex(src); loc != nil {
				t.Errorf("%s carries the task-family verb enumeration as a literal list (offset %d) — reference taskverbs.FamilyVerbs / strings.Join instead: a hand-typed list is what silently dropped %q from `gopi -h` (Rule 13/14)",
					path, loc[0], taskverbs.Status)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	// An exhaustive scan that silently checked nothing would pass vacuously.
	if checked < 50 {
		t.Fatalf("the gate scanned only %d production files — the tree walk is broken, so this guard proves nothing", checked)
	}
}

// TestTopLevelTaskRowDerivesTheEnumeration proves the DERIVATION survived: the
// task index row's summary must be the constant-built string, not a literal.
// (The rendered help WRAPS this row, so the assertion is against the row
// source, not FormatUsage()'s wrapped output; internal/taskverbs + the cmd/agent
// pair guard the rendered side.) A literal that happens to contain today's
// verbs cannot pass this AND TestNoProductionFileRestatesTheTaskVerbEnumeration.
//
// THE ENUMERATED SET IS taskverbs.IndexRowVerbs — FamilyVerbs ∪ SyncVerbs, the
// row's OWN set (task 01a11f70…, 2026-10-10). It used to be FamilyVerbs, and
// that is the defect: the row is the first page a reader opens, its head is
// `gopi task <subcommand>` (the whole family), and a cross-node leg missing from
// it (`send` — how a row reaches another machine) was published nowhere else a
// reader would look, except a negative-titled section at the bottom of
// `gopi task -h`. The completion surface (tab) is deliberately NOT widened: it
// stays FamilyVerbs, asserted by cmd/agent's
// TestTopLevelUsageTaskVerbEnumMatchesCompletion.
func TestTopLevelTaskRowDerivesTheEnumeration(t *testing.T) {
	// The row's SUMMARY is the derived enumeration and nothing more. It carried
	// a `; `send` moves a row to a `peer`` clause from 2026-10-10 until
	// 2026-11-05 — the clause existed to explain why a cross-node spelling was
	// in the list at all, and BOTH halves went with the family: SyncVerbs is
	// empty (IndexRowVerbs == FamilyVerbs) and `send` can only refuse now, so a
	// clause naming it would advertise the deletion target as a route.
	want := "The task-as-memory ledger: " + strings.Join(taskverbs.IndexRowVerbs, ", ") +
		" — append-only, never deleted."
	if got := taskRowOf(t); got != want {
		t.Errorf("the `gopi -h` task row is not the derived enumeration.\nhave: %s\nwant: %s", got, want)
	}
	// The rendering must reach the user: the row's static prefix appears in the
	// rendered help (the verb list itself is asserted by cmd/agent's pair).
	if !strings.Contains(FormatUsage(), "The task-as-memory ledger: ") {
		t.Error("FormatUsage does not render the task ledger row at all")
	}
	// THE UNIONS, asserted here so a hand-typed row cannot be approved by a
	// reviewer reading only the rendered page: the row's set is exactly
	// FamilyVerbs ∪ SyncVerbs, sorted, deduplicated — and it must actually have
	// GAINED the cross-node legs (a row that silently went back to the ledger
	// group would satisfy a "contains every FamilyVerbs entry" check).
	for _, v := range taskverbs.SyncVerbs {
		if !containsString(taskverbs.IndexRowVerbs, v) {
			t.Errorf("IndexRowVerbs omits the cross-node verb %q — the row enumerates FamilyVerbs ∪ SyncVerbs", v)
		}
		if !strings.Contains(taskRowOf(t), v) {
			t.Errorf("the `gopi -h` task row does not name %q — it is on the row's own enumeration", v)
		}
	}
	if len(taskverbs.IndexRowVerbs) != len(taskverbs.FamilyVerbs)+len(taskverbs.SyncVerbs) {
		t.Errorf("IndexRowVerbs has %d entries, FamilyVerbs (%d) + SyncVerbs (%d) have %d — the row's set is the UNION, "+
			"and the groups are disjoint (TestDispatchVerbsAreDerived)", len(taskverbs.IndexRowVerbs),
			len(taskverbs.FamilyVerbs), len(taskverbs.SyncVerbs), len(taskverbs.FamilyVerbs)+len(taskverbs.SyncVerbs))
	}
	if !sort.StringsAreSorted(taskverbs.IndexRowVerbs) {
		t.Errorf("IndexRowVerbs is not sorted — the row renders it in list order: %v", taskverbs.IndexRowVerbs)
	}
}

// taskRowOf returns the rendered task index row for diagnostics.
func taskRowOf(t *testing.T) string {
	t.Helper()
	for _, sec := range commandIndex {
		for _, row := range sec.rows {
			if row[0] == "gopi task <subcommand>" {
				return row[1]
			}
		}
	}
	t.Fatal("commandIndex has no `gopi task <subcommand>` row")
	return ""
}
