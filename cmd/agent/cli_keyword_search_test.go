package main

// cli_keyword_search_test.go — the contract for `gopi tool keyword_search`, the
// CLI face of the keyword_search TOOL (naming rule 1: a tool-derived command
// keeps the tool's own underscore spelling).
//
// THE DEFECT THIS PINS (task 01a0dde8…, EUAUDIT slice 4 TOOLS-1 F2): the word
// was in NO dispatch chain, so `gopi tool keyword_search -h` printed the TOP-LEVEL
// help page BYTE-FOR-BYTE (same md5, and the string `keyword_search` occurred
// zero times in it) and `gopi tool keyword_search <query>` fell through to the
// bare-prompt path — a real agent run (session dir + `[auto]` ledger row +
// tokens) for a word the operator typed as a COMMAND.
//
// Three layers, all necessary:
//  1. unit — the parser (pure, no process side effects),
//  2. the two-sided wiring contract — the word reaches the dispatch chain AND
//     the completion set AND the guard vocabulary (each of which can silently
//     rot on its own),
//  3. live binary — the smallest repro from the acceptance: the -h page must
//     DIFFER from `gopi --help` and must NAME the word.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// TestParseKeywordSearchArgs pins the pure parser: one query (words join),
// an optional --root, and a pointed error for the shapes that cannot run.
func TestParseKeywordSearchArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		query string
		root  string
	}{
		{name: "bare query", args: []string{"pointed hint"}, query: "pointed hint", root: keywordSearchDefaultRoot},
		{name: "word per positional joins in order", args: []string{"two", "words"}, query: "two words", root: keywordSearchDefaultRoot},
		{name: "explicit root", args: []string{"needle", "--root", "internal/config"}, query: "needle", root: "internal/config"},
		{name: "flag before positional", args: []string{"--root", "internal", "needle"}, query: "needle", root: "internal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseKeywordSearchArgs(tc.args)
			if err != nil {
				t.Fatalf("parseKeywordSearchArgs(%q): %v", tc.args, err)
			}
			if opts.query != tc.query {
				t.Errorf("query = %q, want %q", opts.query, tc.query)
			}
			if opts.root != tc.root {
				t.Errorf("root = %q, want %q", opts.root, tc.root)
			}
		})
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "no query", args: nil, want: "missing <query>"},
		{name: "blank query", args: []string{"   "}, want: "missing <query>"},
		{name: "unknown flag", args: []string{"needle", "--pattern", "x"}, want: "unknown flag --pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseKeywordSearchArgs(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseKeywordSearchArgs(%q) error = %v, want it to contain %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestIsKeywordSearchSubcommandIgnoresSiblings — the predicate answers for
// its OWN word only; a sibling verb must not be captured by it (the dispatch
// chain is a sequence of such predicates, and a too-greedy one would shadow
// everything after it).
func TestIsKeywordSearchSubcommandIgnoresSiblings(t *testing.T) {
	for _, sibling := range []string{CliVerbCodeSearch, CliVerbLog, CliVerbTask, CliVerbVersion, ""} {
		if sibling == "" {
			if isKeywordSearchSubcommand(nil) {
				t.Error("isKeywordSearchSubcommand(nil) = true, want false")
			}
			continue
		}
		// Only the argv[0] SHAPE is classified here; the sibling words must
		// not reach the runner (a true return would run and os.Exit).
		if sibling == toolnames.ToolKeywordSearch {
			continue
		}
		if isKeywordSearchSubcommand([]string{sibling, "-h"}) {
			t.Errorf("isKeywordSearchSubcommand([%s]) = true, want false", sibling)
		}
	}
}

// TestKeywordSearchVerbIsWiredEverywhere is the two-sided wiring gate: the
// word must be dispatchable (main.go), completable (topLevelCommandNames),
// advertised (the top-level index) and guarded (config.PositionalVerbs) — a
// word missing from ANY of these re-opens part of the hole the fix closes.
func TestKeywordSearchVerbIsWiredEverywhere(t *testing.T) {
	word := toolnames.ToolKeywordSearch

	// The word is a GROUP leaf since the retirement (2026-10-05): absent from
	// the top-level set (which would teach a refused spelling) and present in
	// the group's own.
	if containsString(topLevelCommandNames, word) {
		t.Errorf("%q is still in topLevelCommandNames — the bare word is refused now; completion would teach it", word)
	}
	if !containsString(toolCompletionNames, word) {
		t.Errorf("%q is missing from toolCompletionNames — `gopi tool <TAB>` would not offer it", word)
	}
	if !containsString(config.PositionalVerbs, word) {
		t.Errorf("%q is dispatched but missing from config.PositionalVerbs — `gopi --timeout 3 %s` would still run as a prompt", word, word)
	}
	// The index advertises the DOOR, not the leaf (verb-retirement, 2026-10-05):
	// `gopi tool <command>` is one line of the top-level page, and the leaf
	// words are enumerated by `gopi tool -h`. Demanding the leaf's own line here
	// would re-introduce the alias this epic removed.
	if !strings.Contains(usage, "gopi "+CliVerbTool+" ") {
		t.Errorf("the top-level command index does not advertise `gopi %s …` — the group is the only door and must be discoverable", CliVerbTool)
	}
	if !strings.Contains(toolUsage, word) {
		t.Errorf("`gopi tool -h` does not list %q — the leaf is undiscoverable on its own page", word)
	}
	if _, ok := dispatcherOfToken[word]; !ok {
		t.Errorf("%q has no dispatcherOfToken entry — the contract suite does not cover its routing", word)
	}
}

// TestKeywordSearchHelpPageDiffersFromTopLevel is the acceptance's smallest
// repro, on the real binary: the page must differ from the top-level page (the
// original defect was byte-identical: measured md5 6546d3ee for both) and must
// contain the word, which the top-level page did zero times.
//
// THE PAGE IS REACHED THROUGH THE WORD'S DOOR (verb-retirement follow-up,
// 2026-10-07). The bare word stopped dispatching when the local tools got one
// door each (epic 2026-10-05; devdoc/gopi-verb-census.md records the demotion as
// LANDED, with `gopi tool keyword_search` as the only address), so the pre-fix
// form of this test drove a word the binary now refuses — and the property it
// asserts was never about the flat spelling anyway: the page for this command
// must be its OWN page, distinct from `gopi -h`. Driving the door asks exactly
// that, and the door is spelled from the constants that own its two words
// (Rules 1/13/14), which the census's generator resolves as readily as a literal
// (it follows `pkg.Const` into the leaf package), so the drive stays visible to
// the classification gate.
func TestKeywordSearchHelpPageDiffersFromTopLevel(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()

	// The door's two words come from the constants that own them (Rules 1/13/14),
	// never typed again here: `CliVerbTool` names the group, and the leaf word is
	// the tool's own name.
	page, err := runCLIContractProbeRaw(t, bin, home, CliVerbTool, toolnames.ToolKeywordSearch, "-h")
	if err != nil {
		t.Fatalf("gopi %s %s -h: exit error: %v\n--- output ---\n%s", CliVerbTool, toolnames.ToolKeywordSearch, err, page)
	}
	if !strings.Contains(page, toolnames.ToolKeywordSearch) {
		t.Fatalf("gopi %s %s -h does not name the command — the page is not this command's help\n--- output ---\n%s", CliVerbTool, toolnames.ToolKeywordSearch, page)
	}
	top, err := runCLIContractProbeEnv(t, bin, home, "-h")
	if err != nil {
		t.Fatalf("gopi -h: exit error: %v\n--- output ---\n%s", err, top)
	}
	if page == top {
		t.Fatal("gopi tool keyword_search -h is BYTE-IDENTICAL to gopi -h — the word fell through to the top-level usage (the measured defect)")
	}
	assertNoCLIContractSession(t, toolnames.ToolKeywordSearch+" -h", home)
}

// TestKeywordSearchRunPrintsHitsAndNoMatches drives the RUN path on the real
// binary (Rule 31): a query that exists in a fixture tree prints
// `file:line: snippet` hits and exits 0; a query that cannot match prints the
// engine's own "No matches." and exits 1. Neither shape may mint a session —
// the whole point is that this word is a COMMAND, never a prompt.
//
// THE RUN GOES THROUGH THE DOOR (verb-retirement follow-up, 2026-10-07): the
// bare word is refused now (its only address is `gopi tool keyword_search`), and
// running the door is what actually exercises the engine — the pre-fix form
// asserted the shape of a refusal instead, which is how a run-path test can stay
// green while testing nothing. The shape being asserted is unchanged: hits, the
// engine's own miss verdict, and zero sessions.
func TestKeywordSearchRunPrintsHitsAndNoMatches(t *testing.T) {
	requireRipgrep(t)
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	root := t.TempDir()

	const needle = "EUAUDIT_NEEDLE_ZZZ"
	fixture := filepath.Join(root, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("first line\n"+needle+" lives here\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Shape the argv as a literal with the DOOR's verb FIRST — the family
	// address `gopi tool keyword_search <query>`, the only spelling this binary
	// dispatches. The census's argv-literal shape is what classes a verb a:test
	// (the runner-line shape is weaker evidence and only a floor); its generator
	// reads the constants as readily as literals, so the drive stays visible.
	argv := []string{CliVerbTool, toolnames.ToolKeywordSearch, needle, "--root", root}
	out, err := runCLIContractProbeRaw(t, bin, home, argv...)
	if err != nil {
		t.Fatalf("gopi %s %s: exit error: %v\n--- output ---\n%s", strings.Join(argv[:2], " "), needle, err, out)
	}
	if !strings.Contains(out, "fixture.txt:2:") || !strings.Contains(out, needle) {
		t.Fatalf("hit output is not the tool's `file:line: snippet` shape\n--- output ---\n%s", out)
	}
	assertNoCLIContractSession(t, toolnames.ToolKeywordSearch+" "+needle, home)

	missOut, missErr := runCLIContractProbeEnv(t, bin, home, CliVerbTool, toolnames.ToolKeywordSearch, "EUAUDIT_NOTHING_HERE_ZZZ", "--root", root)
	if missErr == nil {
		t.Fatalf("a no-match query exited 0 — a script cannot branch on the result\n--- output ---\n%s", missOut)
	}
	if !strings.Contains(missOut, "No matches.") {
		t.Fatalf("a no-match query did not print the engine's own verdict\n--- output ---\n%s", missOut)
	}
	assertNoCLIContractSession(t, toolnames.ToolKeywordSearch+" (miss)", home)
}

// TestKeywordSearchFamilyDrivesNoRetiredSpelling is the FAMILY's regression
// guard, and it exists because the generic retired-verb gates provably do NOT
// cover this family's argv shape — MEASURED, not assumed: the pre-fix file
// (flat `runCLIContractProbeRaw(t, bin, home, CliVerbTool, "keyword_search", …)`)
// was restored and BOTH `TestNoTestDrivesTheRealBinaryWithARetiredWord` and its
// widened helper arm ran GREEN on it. The reason is structural and worth
// stating, because it is what makes a family-level guard necessary rather than
// redundant:
//
//   - the DIRECT arm reads argv[0] only at an `exec.Command` call site, and
//   - the HELPER arm reads call sites of a helper that FORWARDS its argv
//     VERBATIM, while `runCLIContractProbeRaw` is classified TRANSFORMED (its
//     body hands the argv to `cliContractProbeWithTimeout` first).
//
// So a retired word reaching the binary as the FIRST VARIADIC ARGUMENT of a
// probe helper sits in neither arm's field of view: not the call site (the
// word is not beside an exec), not the helper call site (the helper is not
// verbatim). That is the hole the two red tests fell into, and this test is
// the smallest thing that closes it for this family.
//
// WHAT IT ASSERTS, precisely. Read at the PROBE-CALL level, over the DRIVE
// HEAD of every raw-probe call in this file — the leading word(s) that address
// this binary, as opposed to the payload that follows them:
//
//	(a) a call whose first word is the family door must take the leaf word from
//	    the CONSTANT that owns it — a bare `"keyword_search"` in that position
//	    IS the retired flat spelling, and every such call is refused by the
//	    binary, so the test reads a refusal as a missing feature; and a call
//	    whose first word is anything else must not START with a retired word
//	    (the fully flat drive); and
//	(b) this file still DRIVES the family — at least one probe call whose head
//	    is the group constant followed by the leaf constant. Without (b) a file
//	    that stopped calling the binary entirely would pass (a) trivially,
//	    which is the vacuous-green shape (Rule 15).
//
// WHY THE HEAD AND NOT argv[0] ALONE — MEASURED, not assumed. The first red
// proof re-introduced the stale drive in the form `CliVerbTool,
// "keyword_search"` while the guard read argv[0] alone, and the guard PASSED:
// the group word was still the constant, so the first word looked correct and
// the retired leaf slipped through in second position. The drive head is where
// the spelling can be re-typed; the words AFTER it are payload (the query
// needle, `--root`, a path), where a word that happens to equal a retired
// spelling is a query, not a drive — flagging those would be a false positive of
// the same cost as the false negative this guard closes.
//
// The vocabulary is READ, never copied: the retired words come from
// verbAliasWords() (the table the refusal itself renders from — the flat leaf
// spelling is a row in it, which is why it needs no second list here), and the
// door from the constants that own its two words (CliVerbTool /
// toolnames.ToolKeywordSearch — Rules 1/13/14).
func TestKeywordSearchFamilyDrivesNoRetiredSpelling(t *testing.T) {
	rawProbes := map[string]bool{
		"runCLIContractProbeRaw":        true,
		"cliContractProbeWithTimeout":   true,
		"runCLIContractProbeEnvTimeout": true,
	}
	retired := map[string]bool{}
	for _, w := range verbAliasWords() {
		retired[w] = true
	}
	// The constants that would make this guard vacuous if they were empty.
	if len(retired) == 0 {
		t.Fatal("verbAliasWords() came back empty — the guard's vocabulary is missing and it would pass on anything")
	}
	if CliVerbTool == "" || toolnames.ToolKeywordSearch == "" {
		t.Fatal("the door's own words resolved empty — the guard cannot state what a correct drive looks like")
	}

	src, err := os.ReadFile("cli_keyword_search_test.go")
	if err != nil {
		t.Fatalf("read this family's test file: %v", err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "cli_keyword_search_test.go", src, 0)
	if err != nil {
		t.Fatalf("parse this family's test file: %v", err)
	}

	doorDrives := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !rawProbes[probeCallName(call)] {
			return true
		}
		// EVERY word this probe hands the binary — a slice bound to a name
		// earlier in the file is followed, and the position of each word is
		// kept, because the defect can sit in whichever argument was re-typed.
		refs := probeArgvWords(call, file, fset, src)
		if len(refs) == 0 {
			return true
		}
		leaf := toolnames.ToolKeywordSearch
		if refs[0].word == CliVerbTool {
			// The family door: the leaf must come from its CONSTANT. A bare
			// literal here is the retired flat spelling — the exact re-typing
			// this guard exists to catch.
			if len(refs) > 1 && refs[1].word == leaf {
				if refs[1].literal {
					t.Errorf("cli_keyword_search_test.go:%d drives the binary with the door spelled %s — the leaf word is re-typed as a bare literal, "+
						"which is the RETIRED flat spelling and is refused by name (`gopi %s` → exit 1). "+
						"Take it from the constant that owns it: `%s %s`",
						refs[1].line, refs[1].src, leaf, CliVerbTool, "toolnames.ToolKeywordSearch")
					return true
				}
				doorDrives++
			}
			return true
		}
		// Not the group door: the head must not be a retired word, whatever
		// position it sits in (the fully flat drive).
		if retired[refs[0].word] {
			spelling := refs[0].src
			if refs[0].literal {
				spelling += " (a bare string literal, where the constant that owns it resolves to the same word)"
			}
			t.Errorf("cli_keyword_search_test.go:%d drives the real binary with the RETIRED spelling %q as %s — "+
				"the binary refuses it by name (`gopi %s %s` → exit 1) and the test then reads the refusal as a missing feature. "+
				"Drive the door instead: `%s %s`",
				refs[0].line, refs[0].word, spelling, CliVerbTool, refs[0].word, CliVerbTool, leaf)
		}
		return true
	})

	if doorDrives == 0 {
		t.Errorf("no probe call in this file hands the binary the group door %q followed by the leaf CONSTANT %s — "+
			"the family drives nothing, so the assertions above are vacuous (this file must reach `%s %s`)",
			CliVerbTool, "toolnames.ToolKeywordSearch", CliVerbTool, toolnames.ToolKeywordSearch)
	}
}

// probeArgvConstWords is the constant-name → word table this family's guard
// resolves an argv expression through: the RETIRED words' own constants (read
// from the gate file's table, so a word the binary refuses is recognised
// whether it is spelled as a literal or through the constant that names it)
// plus the DOOR's two constants, whose values come from the constants
// themselves and are never typed here (Rules 1/13/14).
func probeArgvConstWords() map[string]string {
	out := make(map[string]string, len(retiredVerbDriveGateConstWords)+3)
	for k, v := range retiredVerbDriveGateConstWords {
		out[k] = v
	}
	out["CliVerbTool"] = CliVerbTool
	out["ToolKeywordSearch"] = toolnames.ToolKeywordSearch
	out["toolnames.ToolKeywordSearch"] = toolnames.ToolKeywordSearch
	return out
}

// probeCallName names the function a call expression invokes, by its last
// identifier — the form the raw-probe set is keyed on.
func probeCallName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// argvWordRef is ONE word a probe hands the binary, with the spelling it was
// written in — kept because the guard's verdicts differ by spelling (a retired
// word is refused either way; the door's leaf word may only come from its
// constant).
type argvWordRef struct {
	word    string
	literal bool
	line    int
	src     string
}

// probeArgvWords resolves EVERY word a probe call hands the binary, in argv
// order. An argument that resolves to nothing — `t`, `bin`, `home`, a path
// expression — is skipped, because it is not a word at all; a slice literal
// bound to a name earlier in the file is EXPANDED, so the `argv := []string{…}`;
// `probe(…, argv...)` shape is read word by word exactly like the inline form.
func probeArgvWords(call *ast.CallExpr, file *ast.File, fset *token.FileSet, src []byte) []argvWordRef {
	slices := retiredVerbDriveGateSliceLiterals(file)
	words := probeArgvConstWords()
	var out []argvWordRef
	add := func(e ast.Expr) {
		w, literal, ok := probeArgvLiteralWord(e, words)
		if !ok {
			return
		}
		out = append(out, argvWordRef{
			word:    w,
			literal: literal,
			line:    fset.Position(e.Pos()).Line,
			src:     exprSource(fset, src, e),
		})
	}
	for _, arg := range call.Args {
		if id, ok := arg.(*ast.Ident); ok {
			if elts, bound := slices[id.Name]; bound {
				for _, e := range elts {
					add(e)
				}
				continue
			}
		}
		add(arg)
	}
	return out
}

// probeArgvLiteralWord resolves ONE expression to the word it carries: a string
// literal (reported as `literal`, so a re-typed word is distinguishable from one
// taken from its constant), or an identifier/selector naming a constant in the
// resolved table.
func probeArgvLiteralWord(e ast.Expr, words map[string]string) (word string, literal bool, ok bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false, false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false, false
		}
		return s, true, true
	case *ast.Ident:
		w, ok := words[v.Name]
		return w, false, ok
	case *ast.SelectorExpr:
		pkg, ok := v.X.(*ast.Ident)
		if !ok {
			return "", false, false
		}
		w, ok := words[pkg.Name+"."+v.Sel.Name]
		return w, false, ok
	}
	return "", false, false
}

// exprSource renders an expression back to its own source text, for the failure
// message — a guard that names the spelling it found is a guard a reader can act
// on without opening the file.
func exprSource(fset *token.FileSet, src []byte, e ast.Expr) string {
	start, end := fset.Position(e.Pos()), fset.Position(e.End())
	if !start.IsValid() || !end.IsValid() || end.Offset > len(src) || start.Offset > end.Offset {
		return ""
	}
	return string(src[start.Offset:end.Offset])
}

// TestKeywordSearchUsageAdvertisesRootFlag — the flag the runner accepts must
// be the flag the help page documents (an accepted-but-undocumented flag, or a
// documented-but-unaccepted one, is the "help promises, code refuses" class
// this epic exists for).
func TestKeywordSearchUsageAdvertisesRootFlag(t *testing.T) {
	if !strings.Contains(keywordSearchUsage, "--"+keywordSearchRootFlag) {
		t.Errorf("keyword_search usage does not document --%s, which its parser accepts", keywordSearchRootFlag)
	}
}
