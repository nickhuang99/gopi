package tools

// task_main_checkout_deposit_test.go — the MAIN-CHECKOUT arm's regression guard
// (epic 01a0db25, measured on live row 01a0dabe-3fa1-7cc8-b601-66f1456b8ffd).
//
// THE DEFECT THESE TESTS PIN. That row's payload carried the repo's MAIN
// checkout as its only absolute path —
//
//	"Repo: /home/user/work/GoPi (branch master at 4c802614). Re-verify every
//	 line number before relying on it."
//
// — and the run wrote the two things a run must write into the SHARED checkout:
// the pixel-evidence directory `devdoc/evidence/tui-two-defects/` and the test
// file `internal/uiclient/live_emphasis_attr_test.go`. No arm noticed: the
// close-out residue arm measures only the row's OWN recorded worktree, and
// reclaimRunUnattributableGround excludes the main checkout BY CONSTRUCTION (it
// is not a linked registration). The shared tree is shared, so a record must
// earn its attribution — and this arm's precondition is the one fact that
// separates the shapes: the row's OWN briefing named that checkout.
//
// The fixture reproduces the measured SHAPE, not the machine: a repo with
// TRACKED parent directories (so git reports the deposit itself, `?? path`, and
// not a collapsed parent), a linked worktree as the row's ground, and a payload
// that names the shared checkout and the deposit RELATIVELY — exactly why the
// path proof cannot fire on this row and the window proof carries it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// mainCheckoutDepositFixture builds the measured shape: the standard dead-holder
// fixture (a repo = MAIN checkout on master, plus a linked worktree = the row's
// ground) whose two deposit parents exist and are TRACKED, so a deposit into
// them is reported by git as the deposit itself.
func mainCheckoutDepositFixture(t *testing.T) (repo, wt, branch string) {
	t.Helper()
	repo, wt, branch = deadHolderReclaimFixture(t)
	for _, d := range []string{depositParentEvidence, depositParentUIClient} {
		dir := filepath.Join(repo, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "add", filepath.Join(depositParentEvidence, ".keep"), filepath.Join(depositParentUIClient, ".keep"))
	gitRun(t, repo, "commit", "-m", "tracked deposit parents")
	return repo, wt, branch
}

// The measured deposits, spelled once (Rule 1b): the row's evidence directory
// and its new test file. Both are SPENT by the fixture below (the payload, the
// deposit, and the assertions all go through them), which is what the naming
// gate reads — a constant nobody references would be a spelling, not a citation.
const (
	depositParentEvidence = "devdoc/evidence"
	depositParentUIClient = "internal/uiclient"
	depositEvidenceEntry  = "devdoc/evidence/tui-two-defects/"
	depositTestEntry      = "internal/uiclient/live_emphasis_attr_test.go"
)

// The measured row and the epic that produced this arm, spelled once for the
// same reason: the payload quotes the row (so the reproduction is traceable
// from the ledger alone), and depositNamingGate below reads the constants'
// USE SITES rather than their spellings.
const (
	depositMeasuredRow = "01a0dabe-3fa1-7cc8-b601-66f1456b8ffd"
	depositEpicID      = "01a0db25"
)

// mainCheckoutDepositPayload renders the measured briefing: the main checkout as
// the only absolute path, the deposits named RELATIVELY, and the row/evidence
// named in full so the reproduction is traceable from the ledger alone.
func mainCheckoutDepositPayload(repo string) string {
	return fmt.Sprintf("Repo: %s (branch master at 4c802614). Re-verify every line number before relying on it.\n\n"+
		"GOAL\nFix two measured defects in the `gopi chat` terminal UI.\n\n"+
		"Write the pixel evidence into `%s` and the regression test at `%s`.\n"+
		"Reproduction of the deposit recorded on row %s (epic %s).",
		repo, depositEvidenceEntry, depositTestEntry, depositMeasuredRow, depositEpicID)
}

// depositIntoMainCheckout performs the measured writes — the evidence directory
// and the new test file — inside the SHARED checkout.
func depositIntoMainCheckout(t *testing.T, repo string) {
	t.Helper()
	evidence := filepath.Join(repo, filepath.FromSlash(depositEvidenceEntry))
	if err := os.MkdirAll(evidence, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "capture.txt"), []byte("pixel evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(repo, filepath.FromSlash(depositTestEntry))
	if err := os.MkdirAll(filepath.Dir(testFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testFile, []byte("package uiclient\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mainCheckoutDepositEvents returns every event detail on the row's trail that
// NAMES the shared checkout — the record this arm is supposed to leave, found
// the way an operator would find it (by the checkout's path in the sentence).
func mainCheckoutDepositEvents(t *testing.T, taskID, repo string) []string {
	t.Helper()
	ctx, cancel := contextWithReclaimGitTimeout()
	defer cancel()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, repo) {
			out = append(out, detail)
		}
	}
	return out
}

// TestMainCheckoutDepositIsRecordedForTheRowThatNamedIt is the PRIMARY
// regression guard: the measured row shape closes out and MUST leave one
// attributed record naming the shared checkout AND the deposits — with every
// byte of that checkout left exactly as the run left it.
func TestMainCheckoutDepositIsRecordedForTheRowThatNamedIt(t *testing.T) {
	repo, wt, branch := mainCheckoutDepositFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "fix(chat-ui): status line + bold", Payload: mainCheckoutDepositPayload(repo),
		TaskID: "t-maindep", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-maindep", repo, branch, wt)
	if _, err := TaskStart("t-maindep", "worker-1"); err != nil {
		t.Fatal(err)
	}
	depositIntoMainCheckout(t, repo)
	before := mainCheckoutPorcelain(t, repo)
	if !strings.Contains(before, depositTestEntry) {
		t.Fatalf("the fixture must reproduce the measured deposit (git must REPORT it): %q", before)
	}

	if _, err := TaskRunWriteback("t-maindep", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}

	events := mainCheckoutDepositEvents(t, "t-maindep", repo)
	if len(events) == 0 {
		t.Fatalf("UNATTRIBUTED DEPOSIT: the run closed out `%s` after writing %s and %s into the repo's "+
			"MAIN checkout %s (its own briefing named that checkout), and NOTHING on the row's trail says so — "+
			"the shared tree is shared and no arm measures it (reproduction: row 01a0dabe-3fa1-7cc8-b601-66f1456b8ffd, "+
			"deposits %s + %s)",
			TaskStatusCompleted, depositEvidenceEntry, depositTestEntry, repo, depositEvidenceEntry, depositTestEntry)
	}
	joined := strings.Join(events, "\n")
	for _, want := range []string{repo, depositTestEntry, reclaimMainCheckoutDepositBasisBriefing} {
		if !strings.Contains(joined, want) {
			t.Errorf("the record must name %q — an unattributed deposit is not a record:\n%s", want, joined)
		}
	}

	// NEVER A BYTE TOUCHED (the epic's hard constraint): the deposits survive,
	// the checkout's porcelain is IDENTICAL to what the run left, and no arm
	// wrote, moved or deleted anything.
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(depositTestEntry))); err != nil {
		t.Fatalf("the deposit must survive the close-out (the arm never cleans): %v", err)
	}
	if after := mainCheckoutPorcelain(t, repo); after != before {
		t.Fatalf("the arm must not touch the shared checkout:\nbefore=%q\nafter =%q", before, after)
	}
}

// TestMainCheckoutDepositIsSilentForTheRowThatNeverNamedIt is the SELECTIVITY
// proof — the epic's own boundary. A row whose briefing names only its own
// worktree must leave NOTHING about the shared checkout: the tree is the
// operator's and every sibling run's, and a record naming it would be a blame
// instrument, not evidence.
func TestMainCheckoutDepositIsSilentForTheRowThatNeverNamedIt(t *testing.T) {
	repo, wt, branch := mainCheckoutDepositFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "s", Payload: "work only inside " + wt + " — this run's ground",
		TaskID: "t-maindep-silent", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-maindep-silent", repo, branch, wt)
	if _, err := TaskStart("t-maindep-silent", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// The shared checkout is dirty with SOMEBODY ELSE's untracked file, written
	// inside this row's window — the strongest form of the trap: a time-only
	// attribution would blame this row for it.
	if err := os.WriteFile(filepath.Join(repo, "operator-note.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-maindep-silent", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if events := mainCheckoutDepositEvents(t, "t-maindep-silent", repo); len(events) != 0 {
		t.Fatalf("the arm must NOT measure a shared checkout this row never named — got %d record(s):\n%s",
			len(events), strings.Join(events, "\n"))
	}
}

// TestMainCheckoutDepositRecordsThePathTheBriefingNames covers the OTHER
// attribution proof: when the briefing names the deposit's absolute path, the
// record rests on that path — not on a time window — and the tracked-file case
// stays out of it (a tracked change in the shared tree is somebody's
// uncommitted work; the arm never speaks for that shape).
func TestMainCheckoutDepositRecordsThePathTheBriefingNames(t *testing.T) {
	repo, wt, branch := mainCheckoutDepositFixture(t)
	absDeposit := filepath.Join(repo, filepath.FromSlash(depositTestEntry))
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "s",
		Payload: fmt.Sprintf("Repo: %s\nLand the regression test at %s and nothing else.", repo, absDeposit),
		TaskID:  "t-maindep-path", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-maindep-path", repo, branch, wt)
	if _, err := TaskStart("t-maindep-path", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// The NAMED path (untracked) and an unnamed untracked sibling, plus a TRACKED
	// edit that must never be attributed.
	if err := os.WriteFile(absDeposit, []byte("package uiclient\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "unnamed-sibling.txt"), []byte("not named\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("tracked edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-maindep-path", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	events := mainCheckoutDepositEvents(t, "t-maindep-path", repo)
	if len(events) == 0 {
		t.Fatalf("the deposit named by the briefing's ABSOLUTE path must be recorded — got nothing")
	}
	joined := strings.Join(events, "\n")
	if !strings.Contains(joined, depositTestEntry) {
		t.Errorf("the record must quote the named deposit %q:\n%s", depositTestEntry, joined)
	}
	if strings.Contains(joined, "a.txt") {
		t.Errorf("a TRACKED change in the shared checkout is somebody's uncommitted work and must never be attributed:\n%s", joined)
	}
}

// TestMainCheckoutDepositAndOwnWorktreeResidueAreTwoDistinctHandles pins the
// record FAMILY's shape for a run that did both: a deposit into the shared
// checkout AND residue in its own worktree produce TWO events, not one merged
// sentence — because they call for different remedies (the own-worktree residue
// is the run's scratch to clean; the shared checkout is the operator's tree to
// inspect) and an operator greps for the one they need.
func TestMainCheckoutDepositAndOwnWorktreeResidueAreTwoDistinctHandles(t *testing.T) {
	repo, wt, branch := mainCheckoutDepositFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "s", Payload: mainCheckoutDepositPayload(repo),
		TaskID: "t-maindep-cross", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-maindep-cross", repo, branch, wt)
	if _, err := TaskStart("t-maindep-cross", "worker-1"); err != nil {
		t.Fatal(err)
	}
	depositIntoMainCheckout(t, repo)
	if err := os.WriteFile(filepath.Join(wt, "build.out"), []byte("residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-maindep-cross", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	ctx, cancel := contextWithReclaimGitTimeout()
	defer cancel()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	for _, event := range []string{reclaimRunResidueEvent, reclaimMainCheckoutDepositEvent} {
		var n int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`,
			"t-maindep-cross", event).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Errorf("a run that left residue in BOTH its own worktree (%s) and the shared checkout (%s) "+
				"must carry the %q record", wt, repo, event)
		}
	}
}

// TestMainCheckoutDepositTestNamesTheMeasuredReproduction is the epic's own
// requirement that the regression NAMES its evidence: the file that reproduces
// this defect must cite the measured row, the epic and the two deposits by their
// NAMED CONSTANTS, so a reader six months from now can reach the ledger row
// without re-deriving anything.
//
// THE GATE READS USES, NOT SPELLINGS (repaired 2026-10-12, task
// 01a13025). The first version of this guard asked
// `strings.Contains(string(src), want)` about THIS VERY FILE — and every needle
// already occurs in this file's own comments (lines 4, 13, 14), its constants
// and its failure messages, so the guard could not tell "the reproduction cites
// the evidence" from "these characters appear somewhere in the file". It was
// SELF-MATCHING BY CONSTRUCTION and could never fail. That is the same defect
// class as cmd/agent/merge_every_test.go's one-definition pin (repaired in
// 4ebb9113f), and a comment scan would not fix it either: a mention inside a
// comment or a string is exactly what still matches.
//
// So the verdict is a COUNT OF IDENTIFIER USES in the parsed file: each of the
// four citations must appear as a NAME the code spends, not as a spelling
// anywhere. The producer half keeps it honest — the constants must still be
// DEFINED here with the measured values, or a use site would point at a name
// that carries nothing — and a non-vacuity floor refuses a parse that yielded
// no tree at all (an empty tree would make every count zero and the gate would
// pass on any file).
func TestMainCheckoutDepositTestNamesTheMeasuredReproduction(t *testing.T) {
	const pen = mainCheckoutDepositTestFile
	src, err := os.ReadFile(pen)
	if err != nil {
		t.Fatalf("read the regression file: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, pen, src, 0)
	if err != nil {
		t.Fatalf("parse the regression file: %v", err)
	}

	// The PRODUCER half: each citation must be defined here with the measured
	// value. A use site of a name nobody defines is not a citation.
	defined := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil {
						defined[name.Name] = v
					}
				}
			}
		}
	}
	for _, c := range depositCitationList() {
		got, ok := defined[c.constName]
		if !ok {
			t.Errorf("%s must be DEFINED in %s — a citation the file only mentions is a spelling, not evidence "+
				"(epic %s)", c.constName, pen, depositEpicID)
			continue
		}
		if got != c.value {
			t.Errorf("%s = %q, want %q — the reproduction must name the MEASURED evidence (epic %s)",
				c.constName, got, c.value, depositEpicID)
		}
	}

	// The CONSUMER half: each citation must be SPENT by the code — an identifier
	// read somewhere other than its own definition. A comment or a string
	// literal carrying the same characters is not a use, which is precisely the
	// distinction the substring version could not make.
	//
	// Two things must NOT count as uses, or the gate would satisfy itself again:
	// the const declarations' own names (a definition is not a citation), and
	// THIS gate's own body (its failure messages spend depositEpicID, so counting
	// them would let the gate carry its own subject). Definitions are excluded by
	// POSITION — `id.Obj.Kind` is the declaration's kind on a USE too, so it
	// cannot tell the two apart — and the gate's body by the range of its own
	// FuncDecl, whose presence is asserted so a rename cannot silently disable
	// the exclusion.
	defPos := map[token.Pos]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				defPos[name.Pos()] = true
			}
		}
	}
	var gate *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == mainCheckoutDepositNamingGate {
			gate = fn
		}
	}
	if gate == nil {
		t.Fatalf("%s no longer declares %s — the exclusion below would silently become a no-op and the gate "+
			"would count its own failure messages as the reproduction's citations",
			pen, mainCheckoutDepositNamingGate)
	}
	uses := map[string]int{}
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || defPos[id.Pos()] {
			return true
		}
		if id.Pos() >= gate.Pos() && id.Pos() <= gate.End() {
			return true
		}
		uses[id.Name]++
		return true
	})
	total := 0
	for _, c := range depositCitationList() {
		total += uses[c.constName]
		if uses[c.constName] < depositCitationMinUses {
			t.Errorf("the reproduction spends %s %d times, want at least %d — the evidence must be CITED by the "+
				"code that reproduces it, not merely spelled in a comment: a regression whose evidence is "+
				"only in a report is one nobody can re-open (epic %s)",
				c.constName, uses[c.constName], depositCitationMinUses, depositEpicID)
		}
	}
	if total < depositCitationTotalUses {
		t.Fatalf("this gate's scan saw %d citation ident(s) across the %d subjects, want at least %d — a "+
			"truncated or wrong parse cannot be distinguished from a reproduction that cites nothing, and "+
			"zero-on-zero is how a vacuous guard reads green", total, len(depositCitationList()), depositCitationTotalUses)
	}

	// NON-VACUITY: a parser that silently returned an empty tree would make
	// every count above zero and this gate would pass on any file at all.
	if len(file.Decls) < depositMinDecls {
		t.Fatalf("the parse of %s yielded %d declaration(s), want at least %d — this gate read an empty or "+
			"wrong tree, so its counts mean nothing", pen, len(file.Decls), depositMinDecls)
	}
	if len(defined) < depositMinConsts {
		t.Fatalf("the parse of %s yielded %d string constant(s), want at least %d — the producer half read a "+
			"wrong tree", pen, len(defined), depositMinConsts)
	}
}

// depositCitation is one piece of evidence the reproduction must cite: the NAME
// of the constant that carries it and the measured value that constant must
// hold. The name is a string because Go cannot reflect a constant's own name —
// and it is the IDENTIFIER, never the value, that the consumer half counts: a
// string literal in this list is not an identifier use, so spelling it here
// cannot satisfy the gate it configures.
type depositCitation struct {
	constName string
	value     string
}

// depositCitationList is the gate's subject list, spelled once (Rule 1b): the
// measured row, the epic that produced the arm, and the two deposits the
// incident left in the shared checkout.
func depositCitationList() []depositCitation {
	return []depositCitation{
		{depositMeasuredRowName, "01a0dabe-3fa1-7cc8-b601-66f1456b8ffd"},
		{depositEpicIDName, "01a0db25"},
		{depositEvidenceEntryName, "devdoc/evidence/tui-two-defects/"},
		{depositTestEntryName, "internal/uiclient/live_emphasis_attr_test.go"},
	}
}

// The four citations' NAMES, spelled once each as strings: they are the gate's
// vocabulary (which identifier it counts) and cannot be obtained any other way.
const (
	depositMeasuredRowName   = "depositMeasuredRow"
	depositEpicIDName        = "depositEpicID"
	depositEvidenceEntryName = "depositEvidenceEntry"
	depositTestEntryName     = "depositTestEntry"
)

// depositCitationMinUses is the floor one citation's identifier-use count must
// clear. It is ONE, and that is the measured minimum rather than a taste: the
// row and the epic are spent exactly once each (by the payload), the two
// deposits more often. What makes the floor meaningful is not its size but what
// it counts — an Ident node in the parsed tree, which no comment and no string
// literal can produce.
const depositCitationMinUses = 1

// depositCitationTotalUses is the scan's own non-vacuity floor, set well under
// the measured total (the two deposits are spent ~8 times each): a parse that
// yielded a truncated tree cannot clear it.
const depositCitationTotalUses = 10

// depositMinDecls and depositMinConsts are the parse non-vacuity floors for the
// two halves of the gate: this file declares far more than these, so a parse
// that yielded an empty or truncated tree cannot pass.
const (
	depositMinDecls  = 10
	depositMinConsts = 6
)

// mainCheckoutDepositNamingGate is the gate's OWN function name, spelled once
// (Rule 1b) because the gate's consumer half excludes its own body from the use
// count: the failure messages inside it spend depositEpicID, and counting them
// would let the gate carry its own subject.
const mainCheckoutDepositNamingGate = "TestMainCheckoutDepositTestNamesTheMeasuredReproduction"

// mainCheckoutDepositTestFile is THIS file's name — the reproduction's own
// subject, spelled once (Rule 1b) so the naming gate above cannot drift from
// the file it audits. Tests run in the package directory, so it resolves.
const mainCheckoutDepositTestFile = "task_main_checkout_deposit_test.go"

// TestBriefingNamesDirIsTokenBoundExceptForPaths pins the attribution predicate
// itself: a SIBLING checkout whose name shares the shared checkout's spelling
// must not be read as the row pointing at the shared tree. This is the
// difference between a record and a blame instrument.
func TestBriefingNamesDirIsTokenBoundExceptForPaths(t *testing.T) {
	const main = "/home/user/work/GoPi"
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"named in prose", "Repo: " + main + " (branch master at 4c802614).", true},
		{"named with a path inside it", "write into " + main + "/devdoc/evidence/", true},
		{"named with a trailing separator", "write into " + main + "/", true},
		{"named in a sentence, no space", "repo(" + main + ")", true},
		{"sentence-final full stop — deliberately NOT read as a name", "See " + main + ". Then start.", false},
		{"a SIBLING sharing the prefix", "Repo: " + main + "-old (stale clone)", false},
		{"a BACKUP directory", "Repo: " + main + ".bak", false},
		{"a SUFFIXED parent name", "Repo: /home/user/work/my-GoPi", false},
		{"not named at all", "Repo: /home/user/workspace/gopi-other", false},
		{"empty text", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TaskBriefingNamesDir(tc.text, main); got != tc.want {
				t.Fatalf("TaskBriefingNamesDir(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// TestBriefingAuthorityClauseNamesTheGroundAndDisarmsThePayloadPath pins the
// BRIEFING half: the clause that makes the linked worktree the only tree the run
// writes, and tells the model what an absolute path in its payload IS.
func TestBriefingAuthorityClauseNamesTheGroundAndDisarmsThePayloadPath(t *testing.T) {
	const (
		ground = "/home/user/workspace/gopi-01a0db25"
		main   = "/home/user/work/GoPi"
	)
	clause := TaskBriefingAuthorityClause(ground, main)
	for _, want := range []string{ground, main, "ONLY tree this run writes", "REFERENCE for reading", "MAIN checkout"} {
		if !strings.Contains(clause, want) {
			t.Errorf("the authority clause must carry %q:\n%s", want, clause)
		}
	}
	// A row with no ground says nothing (a clause naming no directory is worse
	// than no clause), and an unresolvable shared checkout is not invented.
	if got := TaskBriefingAuthorityClause("", main); got != "" {
		t.Errorf("no ground ⇒ no clause, got %q", got)
	}
	if got := TaskBriefingAuthorityClause(ground, ""); !strings.Contains(got, ground) || strings.Contains(got, "MAIN checkout") {
		t.Errorf("an unknown shared checkout must be stated as a bare authority, got %q", got)
	}
}

// mainCheckoutDepositSeedSession makes the row's run REAL: a session record with
// the given launch directory is written into the pinned gopi root's sessions
// tree, and the row's session_id is pointed at it. This is the fixture half of
// the deliverable door's LINEAGE proof (the run's own session_start.workdir) —
// without it, a row whose deliverable.repo is the shared checkout has a claim
// and no witness, and the arm correctly attributes nothing.
func mainCheckoutDepositSeedSession(t *testing.T, taskID, workdir string) {
	t.Helper()
	sessHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sessHome, ".gopi", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The gopi root must be named explicitly: a suite-pinned GOPI_HOME wins over
	// HOME for config.GopiDir, so HOME alone would point the lookup elsewhere.
	t.Setenv(config.GopiDirEnv, sessHome)
	sessID := "sess-" + taskID
	line := fmt.Sprintf(`{"type":"session_start","sessionId":%q,"model":"m","timestamp":"x","workdir":%q}`, sessID, workdir)
	if err := os.WriteFile(filepath.Join(sessHome, ".gopi", "sessions", sessID+".jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := contextWithReclaimGitTimeout()
	defer cancel()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET session_id = ? WHERE task_id = ?`, sessID, taskID); err != nil {
		t.Fatal(err)
	}
}

// TestMainCheckoutDepositRecordsTheRowWhoseDeliverableIsTheSharedCheckout is the
// regression for the shape that ACTUALLY leaked (row
// 01a0dbe4-ac93-74f6-8afe-e740669c9532, HP main checkout): the row's
// deliverable.repo IS the shared checkout, its briefing is SILENT about it — so
// the arm's briefing precondition is false BY CONSTRUCTION — and its own session
// records the shared checkout as the launch directory. The run deposits untracked
// residue and closes out.
//
// RED before this change: the briefing door never opened, the arm returned "no
// measurement", and the record did not exist.
func TestMainCheckoutDepositRecordsTheRowWhoseDeliverableIsTheSharedCheckout(t *testing.T) {
	repo, wt, branch := mainCheckoutDepositFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "evidence-heavy work in the shared checkout",
		// SILENT about the checkout: no absolute path names it, so
		// TaskBriefingNamesDir is false and the briefing door cannot open.
		Payload: "Collect the pixel evidence and the dumps for this round; land nothing else.",
		TaskID:  "t-maindep-repo", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	// deliverable.repo == the shared checkout, and the ground is the row's own
	// linked worktree (so the same-ground exclusion does not apply).
	seedDeliverable(t, "t-maindep-repo", repo, branch, wt)
	if _, err := TaskStart("t-maindep-repo", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// The run's own witness: its session launched IN the shared checkout.
	mainCheckoutDepositSeedSession(t, "t-maindep-repo", repo)
	scratchPath := filepath.Join(repo, filepath.FromSlash(mainCheckoutDepositScratchEntry))
	if err := os.MkdirAll(filepath.Dir(scratchPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratchPath, []byte("dump\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := mainCheckoutPorcelain(t, repo)
	// git reports an untracked DIRECTORY once, collapsed (measured: the incident's
	// own porcelain printed `?? scratch/<label>-<task-id>/` per subtree), which is
	// also why the arm walks a directory entry to find the write that proves the
	// hand. Asserting on the collapsed spelling measures what git actually says.
	if !strings.Contains(before, mainCheckoutDepositScratchName+"/") {
		t.Fatalf("the fixture must reproduce the deposit (git must REPORT it): %q", before)
	}

	if _, err := TaskRunWriteback("t-maindep-repo", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}

	events := mainCheckoutDepositEvents(t, "t-maindep-repo", repo)
	if len(events) == 0 {
		t.Fatalf("UNATTRIBUTED DEPOSIT: the run closed out `%s` after writing %s into the repo's MAIN checkout %s — "+
			"the row's own deliverable.repo IS that checkout and its own session recorded it as the launch directory, "+
			"while its BRIEFING never names it, so the briefing door cannot open and NOTHING on the row's trail says so "+
			"(reproduction: row 01a0dbe4-ac93-74f6-8afe-e740669c9532, which invented <repo>/scratch/ — 161 files)",
			TaskStatusCompleted, mainCheckoutDepositScratchEntry, repo)
	}
	joined := strings.Join(events, "\n")
	for _, want := range []string{repo, mainCheckoutDepositScratchName + "/", reclaimMainCheckoutDepositBasisRepo, reclaimMainCheckoutScratchName} {
		if !strings.Contains(joined, want) {
			t.Errorf("the record must name %q — the basis is what makes it falsifiable:\n%s", want, joined)
		}
	}
	if _, err := os.Stat(scratchPath); err != nil {
		t.Fatalf("the deposit must survive the close-out (the arm never cleans): %v", err)
	}
	if after := mainCheckoutPorcelain(t, repo); after != before {
		t.Fatalf("the arm must not touch the shared checkout:\nbefore=%q\nafter =%q", before, after)
	}
}

// TestMainCheckoutDepositIsSilentForTheForeignRowThatDepositedNothing is the
// door's NEGATIVE CONTROL, and it is the assertion the epic's "a row that
// deposited nothing records NOTHING" clause turns into a machine check. The
// operator's own untracked file appears in the shared checkout DURING this row's
// window — the strongest form of the trap — and the row's session did NOT launch
// there, so the lineage proof fails and the arm must stay silent.
//
// A time-only arm files exactly this file against this row; that is the
// misattribution this test exists to refuse.
func TestMainCheckoutDepositIsSilentForTheForeignRowThatDepositedNothing(t *testing.T) {
	repo, wt, branch := mainCheckoutDepositFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "unrelated work", Payload: "work only inside " + wt,
		TaskID: "t-maindep-foreign", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-maindep-foreign", repo, branch, wt)
	if _, err := TaskStart("t-maindep-foreign", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// The row's run stood in its OWN worktree — the lineage proof fails.
	mainCheckoutDepositSeedSession(t, "t-maindep-foreign", wt)
	// The operator's file, written inside this row's window.
	if err := os.WriteFile(filepath.Join(repo, "operator-note.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-maindep-foreign", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if events := mainCheckoutDepositEvents(t, "t-maindep-foreign", repo); len(events) != 0 {
		t.Fatalf("the arm must NOT attribute untracked dirt to a run that did not launch in the shared checkout — "+
			"the file is somebody else's and a timestamp is not lineage; got %d record(s):\n%s",
			len(events), strings.Join(events, "\n"))
	}
}

// mainCheckoutDepositScratchEntry is the deposit the incident actually left —
// the invented <repo>/scratch/ directory, spelled once (Rule 1b) so the
// regression, its failure message and the record's own vocabulary cannot drift.
const (
	mainCheckoutDepositScratchName  = reclaimMainCheckoutScratchName
	mainCheckoutDepositScratchEntry = mainCheckoutDepositScratchName + "/evidence-01a0dbe4.txt"
)

// mainCheckoutPorcelain reads the shared checkout's porcelain body the way the
// arm and an operator both do.
func mainCheckoutPorcelain(t *testing.T, repo string) string {
	t.Helper()
	out, err := reclaimGitStatus(repo)
	if err != nil {
		t.Fatalf("git status in %s: %v", repo, err)
	}
	return out
}

// TestMainCheckoutDepositArmReportsWhyNothingWasAttributed is the DIAGNOSTIC
// face of the arm's hardest silence, and it is a REGRESSION GUARD for the
// measured failure of this file's PRIMARY test.
//
// THE MEASURED SHAPE. During a full `./internal/tools` run under a foreign
// locale (evidence: session 01a18d50-eeed-7ed0-89c0-8b8ffd675226,
// `evidence/suite-zh.txt`), this family's primary test failed while its three
// siblings passed. Reading that run's stderr, the failing row was the only
// member of the family with a deposit, NO record and NO diagnostic at all:
//
//	grep -c 'run t-maindep deposited'  suite-zh.txt   -> 0
//	grep -c 'run t-maindep-path deposited' suite-zh.txt -> 1
//	grep -c 'main checkout deposit'   suite-zh.txt    -> 0
//
// So the arm had reached its "dirt this row cannot be shown to own" return and
// left NOTHING behind. A reader could not tell "these entries belong to somebody
// else" from "the attribution proof could not be made" — opposite conclusions
// with opposite repairs, and no way to choose between them AFTER THE FACT. That
// is what made a 0.07s failure undiagnosable from the run's own output.
//
// WHAT IS PINNED. The arm still records NOTHING on the trail (the record is
// evidence, never a suspicion) and still touches nothing in the shared
// checkout — but when it SAW untracked entries under an open door and
// attributed none, it reports the measurement's own inputs on stderr. Both
// halves are asserted, because either alone is a weaker test: a record would be
// a false accusation, and silence is the defect being closed.
func TestMainCheckoutDepositArmReportsWhyNothingWasAttributed(t *testing.T) {
	repo, wt, branch := mainCheckoutDepositFixture(t)
	const taskID = "t-maindep-unattributed"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "s", Payload: mainCheckoutDepositPayload(repo),
		TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, branch, wt)
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
	depositIntoMainCheckout(t, repo)
	// Dated OUTSIDE this row's window: the entries exist and the door is open,
	// yet the time proof cannot reach them. This is the one input the arm can
	// be denied without touching the ledger, and it is exactly the shape a
	// reader has to be able to distinguish from "not this row's work".
	old := time.Now().Add(-2 * time.Hour)
	backdateTree(t, repo, depositEvidenceEntry, old)
	backdateTree(t, repo, depositTestEntry, old)

	before := mainCheckoutPorcelain(t, repo)
	if !strings.Contains(before, depositTestEntry) {
		t.Fatalf("the fixture must reproduce the deposit (git must REPORT it): %q", before)
	}
	stderr := captureStderrDuring(t, func() {
		if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — elapsed=1s"); err != nil {
			t.Fatalf("writeback: %v", err)
		}
	})

	if events := mainCheckoutDepositEvents(t, taskID, repo); len(events) != 0 {
		t.Fatalf("dirt this row cannot be shown to own must NOT be recorded as its deposit — the record is evidence, never a suspicion:\n%s",
			strings.Join(events, "\n"))
	}
	if !strings.Contains(stderr, "main checkout arm "+taskID) {
		t.Fatalf("the arm saw untracked entries under an open door and attributed none, so it MUST say so — "+
			"without this line 'the entries are not this row's' is indistinguishable from 'the attribution proof could not be made' "+
			"(the measured failure of the primary test left no trace for exactly this reason). stderr was:\n%s", stderr)
	}
	for _, want := range []string{repo, "launchedIn=", "windowStart="} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the diagnostic must carry the measurement's own input %q so the reader can falsify it:\n%s", want, stderr)
		}
	}
	if after := mainCheckoutPorcelain(t, repo); after != before {
		t.Fatalf("reporting must not touch the shared checkout:\nbefore=%q\nafter =%q", before, after)
	}
}

// backdateTree sets the mtime of one deposit entry (and, for a directory, of
// everything under it) to a KNOWN moment — the one way to deny the arm its time
// proof without editing the ledger.
func backdateTree(t *testing.T, repo, rel string, at time.Time) {
	t.Helper()
	root := filepath.Join(repo, filepath.FromSlash(strings.TrimSuffix(rel, "/")))
	err := filepath.WalkDir(root, func(p string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Chtimes(p, at, at)
	})
	if err != nil {
		t.Fatalf("backdate %s: %v", root, err)
	}
}
