package agent

// reframe_dropped_clause_labels_test.go — the MACHINE half of "a reframe keeps
// the clauses that say WHICH TREE this run writes".
//
// ── THE GAP THIS CLOSES ────────────────────────────────────────────────────
//
// reframePreservedClauseLabels is a hand-maintained list, and that is exactly
// the defect it was created to repair, one level down: a briefing clause added
// TOMORROW is preserved only if somebody remembers to add its label. Forgetting
// is SILENT — the clause is simply dropped at the first reframe, which is the
// shape of the 01a1957a incident (the run read the authority clause in req#1
// and #2, lost it at req#3, and 12 s later created a branch in the shared
// checkout). A list nothing checks is a hope.
//
// So this test enumerates the population the list is a decision about, and
// requires every member to be classified:
//
//	CENSUS            every package-level const in internal/tools that is a
//	                  briefing-clause LABEL — decided by the value, not by a
//	                  hand-kept index (see reframeClauseLabelCensus)
//	PRESERVED         reframePreservedClauseLabels (re-attached verbatim)
//	DROPPED           reframeDroppedClauseLabels (a STATED reason each)
//
// A census member in neither list fails the build, with the label's own text in
// the message. That is the whole mechanism: a new clause cannot join the dropped
// set quietly either, because membership is a decision somebody wrote down.
//
// ── WHY THE POPULATION IS DEFINED THIS WAY ─────────────────────────────────
//
// "Ends in `Label`" alone is not enough (TaskShowDeliverableRepoLabel is a
// `task show` FIELD, TaskGroundChannelLabel is a format string), and "starts
// with `[`" alone would sweep in markers like MergeVerifyStaleNoOpMarker that is
// never appended to a briefing. The conjunction of the two is what makes the
// census equal the real population — measured on this rev: 15 members, and the
// two hand-maintained lists hold exactly those 15 and nothing else. A future
// clause that is a bracket-headed `…Label` constant is caught by construction;
// anything else is not, and that limit is stated rather than papered over.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

const (
	// reframeClauseLabelPackageDir is the package that WRITES the clauses — the
	// ONE definition site this census reads (Rules 13/14: the labels are not
	// re-spelled here).
	reframeClauseLabelPackageDir = "tools"
	// reframeClauseLabelSuffix is the naming convention a clause label follows.
	reframeClauseLabelSuffix = "Label"
	// reframeClauseLabelHead is the character a briefing clause's marker line
	// begins with — the second half of the census predicate.
	reframeClauseLabelHead = "["
	// reframeClauseLabelReadFloor refuses a scan that read nothing. It is a
	// FLOOR, not the expected size: a census that suddenly finds 3 labels has
	// stopped parsing (a rename of the package, a build tag, a moved file), and
	// a gate that reads an empty population passes everything trivially.
	// Rule 15: a scan that found nothing must be distinguishable from a clean
	// corpus.
	reframeClauseLabelReadFloor = 12
)

// reframeClauseLabel is one census member: the CONSTANT's name (what a reader
// greps and what a refusal names) and the LABEL TEXT it resolves to (what the
// two lists hold, because a list element is the compiled VALUE of the constant,
// not its spelling).
type reframeClauseLabel struct {
	name string
	text string
}

// reframeConstDef is one package-level const NAME → the expression that gives
// it its value, kept so a label built out of other constants resolves.
type reframeConstDef struct {
	name string
	expr ast.Expr
}

// reframeClauseLabelCensus returns every exported package-level const in
// internal/tools whose name ends in `Label` AND whose resolved value begins with
// `[`, sorted. It resolves constants against each other (a label spelled as
// `A + "\n" + …` or as a plain reference to another constant), because the
// clause's real first line is what a briefing reader matches on.
//
// Values it cannot resolve to a string literal are SKIPPED rather than guessed —
// and the read floor is what keeps that from hiding a broken scan.
func reframeClauseLabelCensus(t *testing.T) []reframeClauseLabel {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file to find internal/tools")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", reframeClauseLabelPackageDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	var defs []reframeConstDef
	parsed := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range value.Names {
					if i < len(value.Values) {
						defs = append(defs, reframeConstDef{name: ident.Name, expr: value.Values[i]})
					}
				}
			}
		}
	}
	if parsed == 0 {
		t.Fatalf("no non-test Go file parsed under %s — the census would be vacuous", dir)
	}

	// Fixpoint resolution: a constant that names another constant is resolved as
	// soon as its referent is known, which is what makes a chained label
	// (`X = Y + "\n…"`) usable instead of silently unresolvable.
	resolved := map[string]string{}
	for pass := 0; pass < len(defs); pass++ {
		changed := false
		for _, d := range defs {
			if _, ok := resolved[d.name]; ok {
				continue
			}
			if v := reframeResolveConstValue(d.expr, d.name, resolved); v != "" {
				resolved[d.name] = v
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	var census []reframeClauseLabel
	for _, d := range defs {
		if !reframeIsClauseLabelName(d.name) {
			continue
		}
		head := reframeLabelHead(resolved[d.name])
		if !strings.HasPrefix(head, reframeClauseLabelHead) {
			continue
		}
		census = append(census, reframeClauseLabel{name: d.name, text: head})
	}
	sort.Slice(census, func(i, j int) bool { return census[i].name < census[j].name })
	return census
}

// reframeIsClauseLabelName is the naming half of the census predicate: an
// EXPORTED constant whose name ends in `Label` (the convention the clause
// writers follow, so a reader can find the family by grep).
func reframeIsClauseLabelName(name string) bool {
	if name == "" || !unicode.IsUpper(rune(name[0])) {
		return false
	}
	return strings.HasSuffix(name, reframeClauseLabelSuffix)
}

// reframeResolveConstValue resolves a const's expression to a string, "" when it
// cannot (a call result, an arithmetic expression, an unknown identifier). The
// `self` argument stops a definition from resolving through itself.
func reframeResolveConstValue(expr ast.Expr, self string, resolved map[string]string) string {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.ParenExpr:
		return reframeResolveConstValue(v.X, self, resolved)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return ""
		}
		left := reframeResolveConstValue(v.X, self, resolved)
		right := reframeResolveConstValue(v.Y, self, resolved)
		if left == "" && right == "" {
			return ""
		}
		return left + right
	case *ast.Ident:
		if v.Name == self {
			return ""
		}
		return resolved[v.Name]
	}
	return ""
}

// reframeLabelHead is the part of a resolved clause value that a briefing line
// is matched on: everything up to the first newline or format verb. A clause
// written with fmt.Sprintf carries its marker before the first verb, and the
// marker is what identifies it.
func reframeLabelHead(value string) string {
	if i := strings.IndexAny(value, "\n%"); i >= 0 {
		return value[:i]
	}
	return value
}

// reframeUnclassifiedClauseLabels returns the census members that appear in
// NEITHER list — the "nobody decided" set this gate exists to empty. It is a
// separate function so the planted case below can drive the SAME predicate
// against an invented label, which is what proves a future clause reddens this
// test rather than merely being described as if it would.
func reframeUnclassifiedClauseLabels(census []reframeClauseLabel) []string {
	preserved := make(map[string]bool, len(reframePreservedClauseLabels))
	for _, label := range reframePreservedClauseLabels {
		preserved[label] = true
	}
	var missing []string
	for _, member := range census {
		if preserved[member.text] {
			continue
		}
		if _, ok := reframeDroppedClauseLabels[member.text]; ok {
			continue
		}
		missing = append(missing, member.name)
	}
	return missing
}

// TestReframe_EveryClauseLabelIsClassified is the gate: every briefing-clause
// label the tools package defines is either preserved or dropped-with-a-reason.
func TestReframe_EveryClauseLabelIsClassified(t *testing.T) {
	census := reframeClauseLabelCensus(t)
	if len(census) < reframeClauseLabelReadFloor {
		t.Fatalf("census found %d clause labels, below the floor %d — the scan is not reading the package it thinks it is:\n%v",
			len(census), reframeClauseLabelReadFloor, reframeClauseLabelNames(census))
	}

	missing := reframeUnclassifiedClauseLabels(census)
	if len(missing) > 0 {
		t.Fatalf("%d briefing-clause label(s) are in NEITHER reframePreservedClauseLabels nor "+
			"reframeDroppedClauseLabels, so a reframe silently drops them (the 01a1957a defect shape):\n  %s\n"+
			"Decide for each: preserve it in internal/agent/reframe_ground_clauses.go, or add it to the "+
			"dropped map WITH the reason it is about the epic rather than about the run.",
			len(missing), strings.Join(missing, "\n  "))
	}

	// The other direction: a list entry naming a constant that no longer exists
	// is a decision about something gone. It would also let the lists drift into
	// naming spellings the census can no longer see (Rules 13/14), so it is an
	// error rather than a harmless leftover.
	inCensus := make(map[string]bool, len(census))
	for _, member := range census {
		inCensus[member.text] = true
	}
	for _, label := range reframePreservedClauseLabels {
		if !inCensus[label] {
			t.Fatalf("reframePreservedClauseLabels holds %q, which no clause label in internal/tools resolves to "+
				"any more (renamed, deleted, or no longer bracket-headed) — update or remove the entry", label)
		}
	}
	for label := range reframeDroppedClauseLabels {
		if !inCensus[label] {
			t.Fatalf("reframeDroppedClauseLabels holds %q, which no clause label in internal/tools resolves to "+
				"any more — the reason it carries is a decision about something that no longer exists", label)
		}
	}

	t.Logf("clause-label census: %d labels, %d preserved, %d dropped-with-reason, 0 unclassified",
		len(census), len(reframePreservedClauseLabels), len(reframeDroppedClauseLabels))
}

// TestReframe_AnUnclassifiedNewClauseTurnsTheGateRed is the PLANTED half: it
// proves the predicate above is not vacuous by feeding it a label nobody
// classified — the exact shape of "a clause added tomorrow, its author forgot
// the list". Without this, the gate's green could be the green of a comparison
// that always agrees.
func TestReframe_AnUnclassifiedNewClauseTurnsTheGateRed(t *testing.T) {
	census := reframeClauseLabelCensus(t)
	if len(reframeUnclassifiedClauseLabels(census)) != 0 {
		t.Fatal("precondition: the real census must be fully classified")
	}

	const invented = "TaskBriefingFutureLabel"
	planted := append(append([]reframeClauseLabel{}, census...),
		reframeClauseLabel{name: invented, text: "[A clause added tomorrow — nobody classified it]"})
	missing := reframeUnclassifiedClauseLabels(planted)
	if len(missing) != 1 || missing[0] != invented {
		t.Fatalf("an unclassified clause label must be reported by name, got %v", missing)
	}
	t.Logf("planted label reported as unclassified: %s", missing[0])
}

// TestReframe_DroppedClauseLabelsCarryReasons pins the deny-list's own contract:
// an entry with no reason is a silent decision, which is the same failure as an
// entry that does not exist.
func TestReframe_DroppedClauseLabelsCarryReasons(t *testing.T) {
	if len(reframeDroppedClauseLabels) == 0 {
		t.Fatal("the dropped list is empty — every dropped clause must carry a stated reason")
	}
	for label, reason := range reframeDroppedClauseLabels {
		if strings.TrimSpace(reason) == "" {
			t.Fatalf("%q is dropped with NO reason — a decision nobody can review is not a decision", label)
		}
	}
}

// reframeClauseLabelNames renders a census as the constant names, for a refusal
// message a reader can act on.
func reframeClauseLabelNames(census []reframeClauseLabel) []string {
	names := make([]string, 0, len(census))
	for _, member := range census {
		names = append(names, member.name)
	}
	return names
}
