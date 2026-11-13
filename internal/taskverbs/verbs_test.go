// Package taskverbs tests: the family verb spellings are the CLI's advertised
// contract (documented in `gopi -h`, offered by `gopi task <TAB>`, dispatched
// by runTasksSubcommand), so a silent edit to one is a user-visible surface
// change (Rule 13/14).
package taskverbs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"testing"
)

// TestVerbValuesAreTheDispatchedSpellings pins the on-the-wire spellings.
// The expected strings are literals ON PURPOSE: this is the one place the
// spelling is asserted, so a rename has to be a deliberate, reviewed change
// that also moves the dispatch case, the completion surface and the family
// help — never an accident.
func TestVerbValuesAreTheDispatchedSpellings(t *testing.T) {
	cases := []struct{ what, got, want string }{
		{"Status", Status, "status"},
		{"Show", Show, "show"},
		{"List", List, "list"},
		{"MergeExecute", MergeExecute, "merge-execute"},
		{"MergedCleanup", MergedCleanup, "merged-cleanup"},
		{"SweepZombies", SweepZombies, "sweep-zombies"},
		{"AuditMerges", AuditMerges, "audit-merges"},
		{"DedupScan", DedupScan, "dedup-scan"},
		{"ForBranch", ForBranch, "for-branch"},
		{"BranchRefs", BranchRefs, "branch-refs"},
		{"Orphans", Orphans, "orphans"},
		{"Stranded", Stranded, "stranded"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q — this spelling is the dispatched verb and the advertised help surface; changing one strands the other", c.what, c.got, c.want)
		}
	}
}

// TestFamilyVerbsMatchConstants is the anti-omission guard: FamilyVerbs is the
// derived advertised surface, so a constant defined but not listed (or listed
// twice) would silently produce an inconsistent enumeration.
//
// THE EXPECTATION IS DERIVED, IT IS NO LONGER A HAND-TYPED LIST (2026-10-30,
// task 01a18f9c3fe4). This test used to carry a literal `want` list, and that
// list IS the defect this change removes: commit 58b5618b retired
// `finish-cleanup` (moved FinishCleanup into RemovedVerbs) and the production
// side moved correctly, while the test's copy kept the word — so the package's
// baseline read RED ("FamilyVerbs has 11 entries, the constants number 12") over
// a difference only the stale copy could see. Read the git history of that list
// and the tell is plain: every earlier repair appended another paragraph
// narrating which verb had moved and why (`Run` left, `branch-refs` arrived,
// `orphans`/`stranded` retired, slice 2/4 retired three more). A manual second
// copy of a derived surface ANNOUNCES its repairs and is silent about the next
// move; the paragraphs are the cost of the construction, not a mitigation.
//
// So the expectation is the two derived groups, by definition:
//
//	FamilyVerbs == DispatchVerbs − (MergerVerbs ∪ SyncVerbs ∪ ServiceAliases ∪ RemovedVerbs)
//
// A verb moving between groups now needs only its group edited, and this test
// follows by construction.
//
// WHAT A SUBTRACTION CANNOT SEE, and therefore what this test ALSO checks: a
// verb that reached NO group at all. Such a spelling is absent from both sides
// of the equation above, so the equation balances on a silent omission — the
// very class of defect the hand-typed list was trying to catch. That half is
// closed from the other end: familyConstantsFromSource reads every value the
// package DECLARES out of verbs.go and requires each one to be in exactly one
// group, so "defined but enumerated nowhere" is named where the construction
// left the hole. The enumeration is read from the declaration site, never from
// a third copy of it (Rule 13).
func TestFamilyVerbsMatchConstants(t *testing.T) {
	want := ledgerFamilyVerbs()
	if len(FamilyVerbs) != len(want) {
		t.Fatalf("FamilyVerbs has %d entries, the derived ledger surface numbers %d — the advertised group and the groups it is carved out of disagree:\ngot  %v\nwant %v",
			len(FamilyVerbs), len(want), FamilyVerbs, want)
	}
	for i := range want {
		if FamilyVerbs[i] != want[i] {
			t.Errorf("FamilyVerbs[%d] = %q, want %q (the advertised surface is DispatchVerbs minus the merger/sync/service/removed groups, so the two enumerations must agree)",
				i, FamilyVerbs[i], want[i])
		}
	}

	// A spelling that is in no group dispatches nowhere and is advertised
	// nowhere — the omission a subtraction cannot report. Two constants carry
	// that state DELIBERATELY, and they are the only two:
	//
	//   - New  — the M4.5 removal; the dispatcher answers the word with its own
	//     removal sentence, and the constant exists so no caller retypes "new".
	//   - Next — the M4.5 claiming spelling, same treatment.
	//
	// Anything OUTSIDE this set must be in exactly one group, so a constant
	// added to the block and enumerated nowhere fails HERE, by name.
	groups := []struct {
		name  string
		verbs []string
	}{
		{"FamilyVerbs", FamilyVerbs},
		{"SyncVerbs", SyncVerbs},
		{"MergerVerbs", MergerVerbs},
		{"ServiceAliases", ServiceAliases},
		{"RemovedVerbs", RemovedVerbs},
	}
	declared := familyConstantsFromSource(t)
	t.Logf("census: %d declared verb constants; groups: FamilyVerbs=%d SyncVerbs=%d MergerVerbs=%d ServiceAliases=%d RemovedVerbs=%d",
		len(declared), len(FamilyVerbs), len(SyncVerbs), len(MergerVerbs), len(ServiceAliases), len(RemovedVerbs))
	for _, value := range declared {
		if verbInNoGroupByDesign[value] {
			for _, g := range groups {
				if containsStringStr(g.verbs, value) {
					t.Errorf("constant %q is declared as carrying no group, but it now appears in %s — the two facts cannot both be true (a spelling that dispatches belongs in a group, and the retired-with-no-group pair is New/Next)",
						value, g.name)
				}
			}
			continue
		}
		placed := 0
		var where []string
		for _, g := range groups {
			if containsStringStr(g.verbs, value) {
				placed++
				where = append(where, g.name)
			}
		}
		if placed == 0 {
			t.Errorf("constant %q is declared in verbs.go but enumerated in NO group — it dispatches nothing, is advertised nowhere, and nothing covers it: add it to the group that owns its behaviour (FamilyVerbs for a ledger verb, or RemovedVerbs when the spelling retires)",
				value)
			continue
		}
		if placed > 1 {
			t.Errorf("constant %q is in %d groups (%v) — the dispatch union would carry the spelling %d times and the group split would answer two homes for one verb",
				value, placed, where, placed)
		}
	}
}

// verbInNoGroupByDesign names the constants that are deliberately in NO group
// (see TestFamilyVerbsMatchConstants for why each one is). It is a NAMED set,
// not a silent skip: a value added here is a decision a reader can audit, and
// the loop above refuses a member that has since joined a group.
var verbInNoGroupByDesign = map[string]bool{
	New:  true,
	Next: true,
}

// ledgerFamilyVerbs derives the advertised LEDGER surface from the groups the
// package declares (Rule 13: the derivation, never a second hand-typed copy).
//
// It is DispatchVerbs minus every group that is advertised somewhere else. The
// subtraction is the definition, so it CANNOT report a verb that moved from
// FamilyVerbs into one of those groups — that is the point (the assertion is
// the derivation, not a snapshot). The omission a subtraction is blind to is
// covered by the declaration census in TestFamilyVerbsMatchConstants.
func ledgerFamilyVerbs() []string {
	advertisedElsewhere := make(map[string]bool,
		len(MergerVerbs)+len(SyncVerbs)+len(ServiceAliases)+len(RemovedVerbs))
	for _, group := range [][]string{MergerVerbs, SyncVerbs, ServiceAliases, RemovedVerbs} {
		for _, v := range group {
			advertisedElsewhere[v] = true
		}
	}
	out := make([]string, 0, len(FamilyVerbs))
	for _, v := range DispatchVerbs {
		if advertisedElsewhere[v] {
			continue
		}
		out = append(out, v)
	}
	return out
}

// familyConstantsFromSource reads the VERB CONSTANTS out of verbs.go — the
// declaration site — and returns their values in declaration order.
//
// WHY THE SOURCE AND NOT A LIST: the defect this test repairs was a manual copy
// drifting from a derived surface. A census written as a third list would have
// the same failure mode one level up, so the population is taken from the one
// place that cannot be stale — the const block itself. Reading it needs the
// standard library's parser, which the package's PRODUCTION code never touches
// (the leaf stays dependency-free; Rule 14's discipline is about what ships).
//
// It fails LOUDLY on the two shapes it cannot census rather than scanning
// nothing and passing: a missing/unparseable source file, and an exported
// constant whose value is not a plain literal (a computed value would escape
// the census silently). A non-string literal is skipped by KIND — the block
// also carries MaxTaskVerbs, an int ceiling, which is not a verb.
func familyConstantsFromSource(t *testing.T) []string {
	t.Helper()
	const (
		sourcePath = "verbs.go"
		// censusFloor is deliberately below the declared population (38 on
		// 2026-10-30) and far above zero: a census that read a fraction of the
		// file, or nothing at all, must fail here instead of reporting green.
		censusFloor = 30
	)
	src, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read %s: %v — the census reads the DECLARATION site, and a file it cannot read would census nothing while reporting green", sourcePath, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), sourcePath, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", sourcePath, err)
	}
	values := make([]string, 0, len(DispatchVerbs))
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				if !name.IsExported() || i >= len(valueSpec.Values) {
					continue
				}
				lit, isLiteral := valueSpec.Values[i].(*ast.BasicLit)
				if !isLiteral {
					t.Fatalf("%s: constant %s is declared with a computed value, which this census cannot read — make it a plain string literal or extend familyConstantsFromSource",
						sourcePath, name.Name)
				}
				if lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: constant %s carries an unquotable literal %s: %v", sourcePath, name.Name, lit.Value, err)
				}
				values = append(values, value)
			}
		}
	}
	if len(values) < censusFloor {
		t.Fatalf("the census found %d verb constants in %s, floor is %d — a population this small means the scan read a fragment of the file, and a fragment cannot vouch for what it did not see",
			len(values), sourcePath, censusFloor)
	}
	return values
}

// TestSyncVerbsIsEmpty pins the shape the cross-node surface has after
// 2026-11-05: EMPTY, with all eight former members in RemovedVerbs.
//
// WHY AN EMPTY-LIST TEST IS NOT A NO-OP. Three properties could silently break
// and each would change what the CLI accepts:
//
//  1. a spelling re-added to SyncVerbs would be ADVERTISED again (IndexRowVerbs
//     is `FamilyVerbs ∪ SyncVerbs`), and the family help page no longer
//     documents any of them — so the verb would be dispatchable and advertised
//     with no page, which TestTaskVerbCeiling would then fail for a reason that
//     looks unrelated to this change;
//  2. a spelling dropped from RemovedVerbs would fall back to the generic
//     "unknown subcommand" dump, losing its removal sentence (Rule 15);
//  3. a spelling in BOTH lists would be advertised AND retired — a page that
//     documents a verb the family refuses.
func TestSyncVerbsIsEmpty(t *testing.T) {
	if len(SyncVerbs) != 0 {
		t.Fatalf("SyncVerbs has %d entries (%v) — the cross-node surface is DELETED and must stay empty; "+
			"its spellings live in RemovedVerbs so they keep a dispatch case with a removal sentence",
			len(SyncVerbs), SyncVerbs)
	}
	retired := map[string]bool{}
	for _, v := range RemovedVerbs {
		retired[v] = true
	}
	for _, v := range []string{Diff, Harvest, Pull, Push, Receive, Send, Sync, SyncPass} {
		if !retired[v] {
			t.Errorf("%q is not in RemovedVerbs — its dispatch case would answer with the generic "+
				"usage dump instead of a removal sentence", v)
		}
		if containsVerb(FamilyVerbs, v) {
			t.Errorf("%q is in FamilyVerbs — a deleted spelling must not be advertised", v)
		}
	}
}

// containsVerb reports whether list carries verb.
func containsVerb(list []string, verb string) bool {
	for _, v := range list {
		if v == verb {
			return true
		}
	}
	return false
}

// TestMergerVerbsMatchConstants pins the daemon's own page contents: the
// git-infrastructure atoms (its pass, once, on demand) plus the `merger`
// spelling itself — `gopi merger -h` is the ONE home for all of them.
func TestMergerVerbsMatchConstants(t *testing.T) {
	want := []string{Adopt, AuditMerges, Merge, MergeExecute, MergedCleanup, Merger}
	if len(MergerVerbs) != len(want) {
		t.Fatalf("MergerVerbs has %d entries, want %d: got %v want %v", len(MergerVerbs), len(want), MergerVerbs, want)
	}
	for i := range want {
		if MergerVerbs[i] != want[i] {
			t.Errorf("MergerVerbs[%d] = %q, want %q", i, MergerVerbs[i], want[i])
		}
	}
}

// TestServiceAliasesMatchConstants pins the service-alias spelling set (the
// spellings with no other group to belong to — `merger` sits in MergerVerbs
// beside the git atoms it daemonizes).
func TestServiceAliasesMatchConstants(t *testing.T) {
	want := []string{Serve}
	if len(ServiceAliases) != len(want) {
		t.Fatalf("ServiceAliases has %d entries, want %d: got %v", len(ServiceAliases), len(want), ServiceAliases)
	}
	for i := range want {
		if ServiceAliases[i] != want[i] {
			t.Errorf("ServiceAliases[%d] = %q, want %q", i, ServiceAliases[i], want[i])
		}
	}
}

// TestFamilyGroupingsAreDisjoint is the structural invariant of the
// restructure: the four lists partition the family vocabulary — a verb in two
// groups would be advertised twice (two homes for one function), which is
// exactly what the epic removes.
func TestFamilyGroupingsAreDisjoint(t *testing.T) {
	groups := []struct {
		name  string
		verbs []string
	}{
		{"FamilyVerbs", FamilyVerbs},
		{"SyncVerbs", SyncVerbs},
		{"MergerVerbs", MergerVerbs},
		{"ServiceAliases", ServiceAliases},
		{"RemovedVerbs", RemovedVerbs},
	}
	seen := map[string]string{}
	for _, g := range groups {
		for _, v := range g.verbs {
			if prev, dup := seen[v]; dup {
				t.Errorf("%q is in both %s and %s — a verb has exactly ONE home (the restructure's whole point)", v, prev, g.name)
				continue
			}
			seen[v] = g.name
		}
	}
	// Except for the deliberate single overlap assertion: the git atoms are
	// NOT ledger verbs, so they must be absent from FamilyVerbs.
	for _, v := range MergerVerbs {
		for _, fam := range FamilyVerbs {
			if fam == v {
				t.Errorf("%q is a git-infrastructure atom AND advertised in the ledger group — the merger's pass must be documented in `gopi merger -h` only", v)
			}
		}
	}
	for _, v := range SyncVerbs {
		for _, fam := range FamilyVerbs {
			if fam == v {
				t.Errorf("%q is a cross-node sync verb AND advertised in the ledger group — sync verbs have their own -h", v)
			}
		}
	}
}

// TestIndexRowVerbsIsTheFamilyUnion pins the row's OWN enumeration (task
// 01a11f70…, 2026-10-10): the `gopi -h` task row lists FamilyVerbs ∪ SyncVerbs,
// sorted and distinct.
//
// WHY IT IS NOT FamilyVerbs, and why the distinction has to be asserted rather
// than described: the row is the FIRST page a reader opens and its head is
// `gopi task <subcommand>` — the whole family — so a row that enumerated only
// the ledger group left the cross-node legs (`send` — how a row reaches another
// machine) reachable nowhere a reader who does not already suspect they exist
// would look. The completion surface is deliberately NOT the row's set: tab
// offers FamilyVerbs alone, because a sync verb's documented home is
// `gopi task sync -h` (cmd/agent's TestTopLevelUsageTaskVerbEnumMatchesCompletion
// asserts both halves).
//
// A DERIVATION test, not a pinned list (Rule 13): the union is computed here
// from the groups, so a verb moving between FamilyVerbs and SyncVerbs — or a new
// verb joining either — cannot leave this list stale.
func TestIndexRowVerbsIsTheFamilyUnion(t *testing.T) {
	want := append(append([]string{}, FamilyVerbs...), SyncVerbs...)
	sort.Strings(want)
	if len(IndexRowVerbs) != len(want) {
		t.Fatalf("IndexRowVerbs has %d entries, want %d (FamilyVerbs ∪ SyncVerbs):\ngot  %v\nwant %v",
			len(IndexRowVerbs), len(want), IndexRowVerbs, want)
	}
	for i := range want {
		if IndexRowVerbs[i] != want[i] {
			t.Errorf("IndexRowVerbs[%d] = %q, want %q — the row's enumeration is the sorted union of the two advertised groups",
				i, IndexRowVerbs[i], want[i])
		}
	}
	// THE DEFECT ITSELF: the cross-node legs must be ON the row. A silent
	// revert to FamilyVerbs satisfies "is a derived list" and would still pass a
	// shape-only check, so the regression is named where the list is defined.
	for _, v := range SyncVerbs {
		if !containsStringStr(IndexRowVerbs, v) {
			t.Errorf("IndexRowVerbs omits the cross-node verb %q — the whole point of the row's set is that `gopi task %s` is reachable from `gopi -h`", v, v)
		}
		if containsStringStr(FamilyVerbs, v) {
			t.Errorf("FamilyVerbs now carries the sync verb %q — the group split is the 2026-09-15 restructure (tab completion reads FamilyVerbs, and a sync verb there advertises a group whose home is `gopi task sync -h`)", v)
		}
	}
	// And the groups stay disjoint, so the union cannot silently double a name.
	for _, v := range SyncVerbs {
		if containsStringStr(FamilyVerbs, v) {
			t.Errorf("%q is in BOTH groups — the union would render it twice", v)
		}
	}
}

// containsStringStr is a local membership reader (the package's tests are
// dependency-free by design — Rule 14's leaf discipline).
func containsStringStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// TestFamilyVerbsAreSortedAndDistinct: the advertised enumeration renders in
// list order, and a duplicate would produce a doubled help entry plus a
// duplicated completion candidate.
func TestFamilyVerbsAreSortedAndDistinct(t *testing.T) {
	if !sort.StringsAreSorted(FamilyVerbs) {
		t.Errorf("FamilyVerbs is not sorted — the help enumeration renders in list order: %v", FamilyVerbs)
	}
	for i, v := range FamilyVerbs {
		if v == "" {
			t.Errorf("FamilyVerbs[%d] is empty (an empty verb matches nothing and renders as a stray separator)", i)
		}
		if i > 0 && FamilyVerbs[i-1] == v {
			t.Errorf("FamilyVerbs[%d] = %q duplicates the previous entry", i, v)
		}
	}
}

// TestDispatchVerbsAreDerived proves the dispatch surface is DERIVED from the
// disjoint groups — FamilyVerbs ∪ SyncVerbs ∪ Merger ∪ Serve ∪ the RETIRED
// spellings — not a second hand-typed list (Rule 13: a mirrored list is drift
// waiting to happen).
//
// RemovedVerbs joined the union in epic worker-run-entry (2026-09-16): `run`
// keeps a DISPATCH case (its pointed removal message) while being advertised
// nowhere, so the union must cover it or the completion gate would report a
// dispatchable-but-undeclared spelling. The union stays the single place a
// group is declared — adding a group means adding it here, which is exactly
// what this test failing looks like.
func TestDispatchVerbsAreDerived(t *testing.T) {
	want := append(append(append(append(append([]string{}, FamilyVerbs...), SyncVerbs...), MergerVerbs...), ServiceAliases...), RemovedVerbs...)
	if len(DispatchVerbs) != len(want) {
		t.Fatalf("DispatchVerbs has %d entries, want %d (FamilyVerbs ∪ SyncVerbs ∪ MergerVerbs ∪ ServiceAliases ∪ RemovedVerbs):\ngot  %v\nwant %v",
			len(DispatchVerbs), len(want), DispatchVerbs, want)
	}
	for i := range want {
		if DispatchVerbs[i] != want[i] {
			t.Errorf("DispatchVerbs[%d] = %q, want %q — the dispatch surface must be the union of the advertised groups plus the service aliases",
				i, DispatchVerbs[i], want[i])
		}
	}
	// The union must still cover EVERY constant that dispatches: a family
	// spelling reachable by neither a group nor the aliases would be
	// dispatchable-but-undeclared (the drift the union exists to prevent).
	// Run and SweepZombies are in that list as the RETIRED spellings: each
	// dispatches (its pointed removal message) and must stay declared exactly
	// once — in RemovedVerbs.
	for _, v := range []string{Adopt, Merge, MergeExecute, MergedCleanup, AuditMerges, Diff, Harvest, Push, Sync, Merger, Serve, Run, SweepZombies, BranchRefs, ForBranch, Orphans, Stranded} {
		found := false
		for _, d := range DispatchVerbs {
			if d == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("constant %q is missing from DispatchVerbs — every dispatching family spelling must be in exactly one of the four groups", v)
		}
	}
}

// TestRemovedVerbsAreNotAdvertised is the anti-resurrection guard of the
// worker-run-entry retirement: `run` keeps a dispatch case (its pointed
// removal message) but must be advertised NOWHERE — not in the ledger help,
// not in the top-level index, not in `gopi task <TAB>`. Re-adding it to
// FamilyVerbs (or to any advertised group) would restore the second runner
// entry this epic removed, silently.
func TestRemovedVerbsAreNotAdvertised(t *testing.T) {
	if len(RemovedVerbs) == 0 {
		t.Fatalf("RemovedVerbs is empty — the removed surface would have no dispatch case and a retired spelling would fall back to the generic usage dump")
	}
	advertised := [][]string{FamilyVerbs, SyncVerbs, MergerVerbs, ServiceAliases}
	for _, v := range RemovedVerbs {
		for _, group := range advertised {
			for _, a := range group {
				if a == v {
					t.Errorf("%q is a REMOVED spelling and still advertised — a retired spelling keeps its dispatch case (its one removal sentence) and appears in no advertised group", v)
				}
			}
		}
	}
	// The derivation must still cover them (they dispatch), and the completion
	// surface must not offer them (the completion test reads FamilyVerbs).
	for _, v := range RemovedVerbs {
		found := false
		for _, d := range DispatchVerbs {
			if d == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is a removed spelling with a dispatch case but is missing from DispatchVerbs — the derivation would no longer describe the dispatcher", v)
		}
	}
}

// TestServeAliasIsNotFamilySurface locks the deliberate exclusion of the
// deprecated `serve` alias: it keeps dispatching for scripts, but must never
// be advertised by the help or offered by tab completion (the assertion in
// cmd/agent/usage_test.go depends on this).
func TestServeAliasIsNotFamilySurface(t *testing.T) {
	for _, v := range FamilyVerbs {
		if v == "serve" {
			t.Fatal("FamilyVerbs advertises the deprecated `serve` alias — `merger` is canonical and the advertised surface must not carry it")
		}
	}
}
