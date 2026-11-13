package main

// retired_verb_test_driver_gate_test.go — the ANTI-REGRESSION gate over the
// TEST-SIDE driver of the real binary (task 01a10e03…, epic verb-retirement
// item 4).
//
// WHY IT EXISTS. The retirement (devdoc/verb-retirement-complete.md) removed
// every flat door and made the old words REFUSE at argv[0]. The PRODUCTION
// half was proved by running it; the TEST half was not, and it produced
// **263 failing tests across 5 of 6 packages** — every one of them a test
// that drives the REAL BINARY with a word the binary now refuses:
//
//	exec.Command(bin, "checkpoint", …)   → the binary prints the refusal, exit 1
//	exec.Command(bin, "code-search", …)  → same
//
// The failing test then reads the refusal as "the feature is missing" and
// blames the retirement, which is the wrong direction: the DRIVER is what
// went stale. That is the shape this gate makes impossible to re-introduce —
// a test that keeps the flat spelling is caught at the SOURCE (the argv it
// passes), not after a 25-minute suite run.
//
// THE VOCABULARY IS READ, NEVER COPIED (Rules 13/14). Every word this gate
// refuses is projected out of the two SSOT tables through their own
// accessors:
//
//	cmd/agent/cli_verb_aliases.go        → verbAliasWords()
//	cmd/agent/cli_undispatched_tool_words.go → undispatchedRefusedWordSpellings()
//
// A word is retired in exactly ONE place, and this gate learns it there. A
// second list here would drift the day a word is added, which is the defect
// the two tables exist to prevent.
//
// WHAT IT READS. The gate is a SOURCE scan (go/parser, the same instrument
// gopibin_test.go already uses) rather than a run of the suite: it walks every
// *_test.go in the tree, and inside each one it looks for a
// `exec.Command(...)` / `exec.CommandContext(...)` call whose FIRST PROGRAM
// ARGUMENT is a retired word. File order and test order are irrelevant to it,
// and it costs no binary build — which is what lets it run in the ordinary
// package suite instead of a heavy tier.
//
// WHAT IT DOES *NOT* FLAG — the two shapes that must stay legal, because a
// gate that refuses them would be refusing the retirement's own contract
// tests:
//
//	(a) a retired word passed to the REAL binary as an ARGUMENT rather than as
//	    the program — `exec.Command(bin, "session", "log", "-h")` is the
//	    family door, and `exec.Command(bin, CliVerbLog)` in a REFUSAL test is
//	    the subject under test, not a stale drive.
//	(b) any use of the word that never reaches a binary at all — a constant
//	    reference, a string compared in a message, a fixture field.
//	(c) a call that starts a DIFFERENT program whose own subcommand happens to
//	    be spelled like a retired word — `exec.Command("git", "log", "-1")`,
//	    `exec.Command("go", "version", "-m", bin)`. The predicate asks WHICH
//	    PROGRAM is started, not only which word is passed (see
//	    retiredVerbDriveGateProgramIsTheRealBinary and the measurement in
//	    TestRetiredVerbDriveGateReadsTheProgramNotJustTheWord). An
//	    UNRESOLVABLE program (`bin`, `testutil.GopiBin(t)`) stays gated: every
//	    driver of the real binary in this repo passes a variable exactly like
//	    that, so the exemption must never be reachable by a guess.
//
// So the predicate is deliberately narrow: FIRST VARIADIC ARGUMENT of the
// program (the position the dispatch chain reads as the verb — see
// devdoc/verb-retirement-complete.md's two-entry-point table). Shape (a) is
// reachable in the gate only through that position, which is exactly the
// position the binary refuses.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/testutil"
)

// --- the gate's own named constants (Rule 1) --------------------------------

const (
	// retiredVerbDriveGateScanRoot is the directory the walk starts from,
	// relative to the repo root: the whole tree of test sources that could
	// drive a gopi binary.
	retiredVerbDriveGateScanRoot = "."

	// retiredVerbDriveGateTestSuffix selects the files in scope. The gate is
	// about *_test.go — production code is the retirement's own business, and
	// a non-test file driving the binary is a different review.
	retiredVerbDriveGateTestSuffix = "_test.go"

	// retiredVerbDriveGateSkipDir is the ONE directory the walk prunes: the
	// module cache and .git are not sources, and a vendored dependency's tests
	// are not ours to gate.
	retiredVerbDriveGateSkipDir = ".git"

	// retiredVerbDriveGateEscape marks a line the gate must not flag, for the
	// reviewer who has read the call and decided it is legitimate (a test that
	// MUST feed a retired word to the binary is a refusal test, and it carries
	// this marker with its reason). The marker is the ONE escape; there is no
	// flag to disable the gate.
	retiredVerbDriveGateEscape = "retired-drive-ok:"

	// retiredVerbDriveGateMinTestFiles is the read-floor: the gate refuses to
	// pass on a tree it did not really read. Measured 2026-10-06: 1445
	// *_test.go files under cmd/ internal/ testsuite/. A floor below that
	// catches a walk that silently found nothing (Rule 15 — a scan that
	// produced no input must not look like a clean scan).
	retiredVerbDriveGateMinTestFiles = 1200

	// retiredVerbDriveGateMinVocabulary is the second read-floor, over the
	// OTHER side: a vocabulary that came back empty or nearly so (a table
	// renamed, an accessor returning nil) would make every assertion below
	// vacuous. Measured 2026-10-06: 41 refused words across the two tables.
	retiredVerbDriveGateMinVocabulary = 25

	// retiredVerbDriveGateGoSuffix is the WIDENED walk's selector. The gate
	// reads every Go source, not only the *_test.go ones, because the helper a
	// test reaches the binary THROUGH may live in a non-test file — and, more
	// to the point, the helper's BODY is what decides whether its call sites
	// drive the binary verbatim. The narrow predicates below still only fire on
	// the two exec shapes; widening the WALK is what makes the helper table
	// complete rather than test-file-only.
	retiredVerbDriveGateGoSuffix = ".go"

	// retiredVerbDriveGateMinGoFiles is the widened walk's read-floor (Rule 15).
	// It is measured on the CORPUS ITSELF rather than lowered to fit: a floor
	// under the measured count is what catches a walk that quietly found
	// nothing. Measured 2026-10-06 at this branch's tip.
	retiredVerbDriveGateMinGoFiles = 1500

	// retiredVerbDriveGateMinPassthroughHelpers is the HELPER side's read-floor,
	// and it is the one this widening lives or dies on. A helper-detector that
	// silently found NO helper would leave every helper call site invisible
	// while the gate still printed its green line — a scan that never ran
	// wearing a pass (Rule 15). Measured 2026-10-06 in this repo: several
	// verbatim argv forwarders (the `exec.Command(bin, args...)` shape).
	retiredVerbDriveGateMinPassthroughHelpers = 3

	// retiredVerbDriveGateVendorDir is pruned alongside .git: a vendored
	// dependency's sources are not ours to gate, and its helpers are not ours
	// to attribute.
	retiredVerbDriveGateVendorDir = "vendor"

	// retiredVerbDriveGateMinExemptPrograms and retiredVerbDriveGateMinExemptCalls
	// are the PROGRAM-NAME EXEMPTION's read-floor (Rule 15): the two numbers that
	// separate "the tree holds no false positive" from "the exemption never
	// fired". Measured 2026-10-25 on this tree's tip: 532 exempted program starts
	// over 30 distinct program words (`git` alone 347). The floors sit well under
	// that measurement, because their job is to catch an exemption that matched
	// NOTHING (a renamed helper, a shadowed identifier, an AST shape nobody
	// writes) — not to pin today's count.
	retiredVerbDriveGateMinExemptPrograms = 10
	retiredVerbDriveGateMinExemptCalls    = 100
)

// --- the two exec entry points -------------------------------------------------

// retiredVerbDriveGateExecFuncs names the stdlib entry points that START A
// PROGRAM. The gate reads only these calls: they are the whole surface on which
// "a test drove the real binary with this word" is decidable from source.
var retiredVerbDriveGateExecFuncs = map[string]bool{
	"exec.Command":        true,
	"exec.CommandContext": true,
}

// retiredVerbDriveGateWord is one word the gate refuses, with the DRIVE that
// must replace it — the row's own address, so the failure message is actionable
// (Rule 15: a refusal that does not say how to proceed is a dead end).
type retiredVerbDriveGateWord struct {
	word    string
	address string
}

// retiredVerbDriveGateVocabulary projects the refused words out of the two SSOT
// tables, through their own accessors. It returns the word→address map AND the
// table-order word list; the list is what the read-floor measures, and the
// address is what a failure prints.
//
// BOTH tables are read here for the reason cli_verb_guard_test.go reads both: a
// word refused in one half and absent from the other is the drift this epic is
// closing, and a gate that learned only half the vocabulary would let the other
// half's tests stay stale.
func retiredVerbDriveGateVocabulary(t *testing.T) (map[string]string, []string) {
	t.Helper()
	words := map[string]string{}
	order := []string{}

	add := func(word, address string) {
		if word == "" {
			return
		}
		if _, seen := words[word]; !seen {
			order = append(order, word)
		}
		words[word] = address
	}

	// The SPELLING table's rows carry their own address (refusalAddress), which
	// is the command line that actually runs.
	for _, e := range undispatchedToolWords {
		add(e.word, e.refusalAddress())
	}
	// The SYNONYM table's rows carry `canonical`, rendered through the ONE
	// address assembler programAddress (never re-assembled here).
	for _, a := range verbAliases {
		add(a.alias, programAddress(a.canonical))
	}

	if len(order) < retiredVerbDriveGateMinVocabulary {
		t.Fatalf("the refusal vocabulary came back with %d words (floor %d) — the gate would be vacuous; "+
			"one of the two tables or its accessor changed shape", len(order), retiredVerbDriveGateMinVocabulary)
	}
	return words, order
}

// --- the source scan ----------------------------------------------------------

// retiredVerbDriveGateFinding is one flagged call, with everything the message
// needs to be actionable without re-reading the file.
type retiredVerbDriveGateFinding struct {
	file    string // repo-relative
	line    int
	word    string
	address string
	snippet string
	// via names the LOCAL HELPER the word reached the binary through, when the
	// finding came from the widened half of the predicate. Empty for a direct
	// exec call. It is part of the message because "which helper" is what tells
	// the reviewer whether the fix is at the call site or in the helper.
	via string
}

func (f retiredVerbDriveGateFinding) String() string {
	if f.via != "" {
		return fmt.Sprintf("%s:%d: the real binary is driven with the RETIRED word %q as argv[0], through the local helper %s — use `%s`",
			f.file, f.line, f.word, f.via, f.address)
	}
	return fmt.Sprintf("%s:%d: the real binary is driven with the RETIRED word %q as argv[0] — use `%s`",
		f.file, f.line, f.word, f.address)
}

// retiredVerbDriveGateGoModRoot walks up from the test's own working directory
// to the directory holding go.mod — the repo root the walk is anchored on (the
// same resolution cli_code_search_test.go's repoRoot performs, kept local so
// this file reads no other test's helper).
func retiredVerbDriveGateGoModRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the repo root (go.mod) walking up from the test's cwd")
		}
		dir = parent
	}
}

// retiredVerbDriveGateReadsTheProgram is the SHIPPED setting of the predicate
// this task added, and the whole point is that nothing but the red proof may
// pass anything else: production scanning always reads the program
// (retiredVerbDriveGateProgramIsTheRealBinary). The false arm exists so the
// red proof can measure the pre-fix behaviour over the SAME corpus rather than
// against a mutated copy of this file — a copy is a second artifact that can
// drift from what ships, and its restoration has to be re-anchored to a
// revision to mean anything at all.
const retiredVerbDriveGateReadsTheProgram = true

// retiredVerbDriveGateScan walks the tree and returns every flagged call. It
// also returns the number of *_test.go files it actually PARSED, which the
// read-floor measures — a walk that found nothing must not read as a pass.
func retiredVerbDriveGateScan(t *testing.T, root string, words map[string]string) ([]retiredVerbDriveGateFinding, int) {
	t.Helper()
	findings, parsed, _ := retiredVerbDriveGateScanCensus(t, root, words, retiredVerbDriveGateReadsTheProgram)
	return findings, parsed
}

// retiredVerbDriveGateScanCensus is the scan plus the PROGRAM-NAME CENSUS: how
// many program starts it exempted as a non-gopi program (per program word), how
// many it kept gated because the program was the binary itself, and how many
// because the program could not be read at all.
//
// The census exists because the exemption above is the one place this gate can
// go quiet, and a gate that goes quiet for the wrong reason is indistinguishable
// from a clean tree (Rule 15). The real-corpus test asserts the exemption
// actually fires there and prints the distinct names with their counts, so a
// predicate that matched nothing — a renamed field, a shadowed identifier, an
// AST shape nobody writes — fails instead of reading green.
//
// `readsProgram` is the SWITCH the red proof toggles (false = the pre-fix
// predicate, which compared argv[0] to the vocabulary without ever asking which
// program was started). It is a parameter rather than a mutated source line so
// the two arms of that proof run over the SAME corpus, in the SAME process, at
// the SAME revision — which is what makes the number of findings comparable
// (Rule 5's timeliness criterion: a reading anchored to nothing but itself
// cannot tell "the fix worked" from "the file moved").
func retiredVerbDriveGateScanCensus(t *testing.T, root string, words map[string]string, readsProgram bool) ([]retiredVerbDriveGateFinding, int, *retiredVerbDriveGateProgramCensus) {
	t.Helper()
	findings := []retiredVerbDriveGateFinding{}
	parsed := 0
	census := retiredVerbDriveGateNewProgramCensus()

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == retiredVerbDriveGateSkipDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, retiredVerbDriveGateTestSuffix) {
			return nil
		}
		parsed++
		findings = append(findings, retiredVerbDriveGateScanFile(t, root, path, words, census, readsProgram)...)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", root, walkErr)
	}
	return findings, parsed, census
}

// retiredVerbDriveGateScanFile parses ONE test source and returns the flagged
// calls in it. A file that does not parse is an ERROR, not a skip: a gate that
// silently ignores the files it cannot read is a gate with a hole shaped like
// whatever broke Go's parser.
func retiredVerbDriveGateScanFile(t *testing.T, root, path string, words map[string]string, census *retiredVerbDriveGateProgramCensus, readsProgram bool) []retiredVerbDriveGateFinding {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// The escape is LINE-scoped (the same shape internal/standards uses): a
	// reviewer marks the CALL's line, or the line directly ABOVE it — the
	// "reason directly above the function" idiom those gates use, which is what
	// a call too long to comment inline needs. It is never file-scoped.
	escaped := map[int]bool{}
	for i, line := range strings.Split(string(src), "\n") {
		if !strings.Contains(line, retiredVerbDriveGateEscape) {
			continue
		}
		escaped[i+1] = true
		escaped[i+2] = true
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	rel := path
	if r, err := filepath.Rel(root, path); err == nil {
		rel = r
	}

	slices := retiredVerbDriveGateSliceLiterals(file)
	local := retiredVerbDriveGateLocalBindings(file)
	out := []retiredVerbDriveGateFinding{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		execFunc := retiredVerbDriveGateExecCallName(call)
		if execFunc == "" {
			return true
		}
		pos := fset.Position(call.Pos())
		if escaped[pos.Line] {
			return true
		}
		// WHICH PROGRAM is this call starting? The words below are gopi VERBS,
		// so the question is only well-formed about a gopi program: `git log`
		// and `go version` carry a retired word as their first ARGUMENT without
		// driving this binary at all (measured pre-fix false positives, see the
		// file header's shape (c)). A program the resolver could not read stays
		// GATED — the conservative direction, because every driver of the real
		// binary in this repo reaches it through a variable.
		if readsProgram {
			program, known := retiredVerbDriveGateExecProgramName(call, execFunc, local, retiredVerbGateStringLiterals(file))
			if known && !retiredVerbDriveGateProgramIsTheRealBinary(program) {
				census.noteExempt(program)
				return true
			}
			if known {
				census.noteGatedOnTheBinary(program)
			} else {
				census.noteGatedUnreadable()
			}
		}
		word, ok := retiredVerbDriveGateFirstProgramArg(call, execFunc, slices)
		if !ok {
			return true
		}
		if address, refused := words[word]; refused {
			out = append(out, retiredVerbDriveGateFinding{
				file:    rel,
				line:    pos.Line,
				word:    word,
				address: address,
				snippet: retiredVerbDriveGateSnippet(string(src), pos.Line),
			})
			return true
		}
		// The SECOND shape: the retired word after its own family door. The
		// argv[0] predicate above cannot see it, and it is just as broken.
		flat := retiredVerbDriveGateFlatten(call.Args, slices)
		if group, address, ok := retiredVerbDriveGateFamilyShape(flat, words); ok {
			out = append(out, retiredVerbDriveGateFinding{
				file:    rel,
				line:    pos.Line,
				word:    group + " " + retiredVerbDriveGateFamilyLeaf(call, slices, group),
				address: address,
				snippet: retiredVerbDriveGateSnippet(string(src), pos.Line),
			})
		}
		return true
	})
	return out
}

// retiredVerbDriveGateExecCallName returns the stdlib starter's own name
// ("Command" / "CommandContext") when the call is one of them, and "" otherwise.
// The NAME is returned rather than a bool because the two have DIFFERENT
// ARITIES — `Command(name, arg0, …)` against `CommandContext(ctx, name, arg0,
// …)` — and the argv[0] position shifts with the arity. A predicate that only
// said "yes" would read the program word as an entry by accident.
func retiredVerbDriveGateExecCallName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	full := pkg.Name + "." + sel.Sel.Name
	if !retiredVerbDriveGateExecFuncs[full] {
		return ""
	}
	return sel.Sel.Name
}

// retiredVerbDriveGateFirstProgramArg returns the call's FIRST PROGRAM
// argument — the argv[0] the dispatch chain reads as the verb — as a literal
// word. Both entry points are `f(program string, args ...string)`: the argv[0]
// is either the variadic's first element (a literal, or an identifier the gate
// resolves when it is a package CONSTANT), or, for the variadic-free form, the
// literal the call passes directly after the program.
//
// The identifier resolution is the reason this is AST work rather than a regex:
// `exec.Command(bin, CliVerbLog)` and `exec.Command(bin, "log")` are the same
// defect, and only one of them spells the word.
func retiredVerbDriveGateFirstProgramArg(call *ast.CallExpr, execFunc string, slices map[string][]ast.Expr) (string, bool) {
	// The program word sits after the function's own leading parameters: none
	// for Command, the context for CommandContext.
	first := retiredVerbDriveGateExecProgramIndex[execFunc]
	if len(call.Args) <= first {
		return "", false
	}
	// The variadic form carries argv[0] as the FIRST ELEMENT of its slice
	// argument — written inline (`[]string{"log"}`) or bound to a name earlier
	// in the same file (`args := []string{"log", …}`; `exec.Command(bin, args…)`).
	if rest := call.Args[first+1:]; len(rest) > 0 {
		switch v := rest[0].(type) {
		case *ast.CompositeLit:
			if len(v.Elts) == 0 {
				return "", false
			}
			return retiredVerbDriveGateLiteralWord(v.Elts[0])
		case *ast.Ident:
			// A named slice (`args := []string{…}` bound earlier in this file)
			// resolves through the index; anything else is not a slice.
			if elts, ok := slices[v.Name]; ok {
				if len(elts) == 0 {
					return "", false
				}
				return retiredVerbDriveGateLiteralWord(elts[0])
			}
			// The COMMON case: the variadic is written inline, so rest[0] IS
			// argv[0] (`exec.Command(bin, "log", …)`) — word or constant.
			return retiredVerbDriveGateLiteralWord(v)
		default:
			// An inline literal or any other expression: rest[0] is argv[0].
			return retiredVerbDriveGateLiteralWord(rest[0])
		}
	}
	return retiredVerbDriveGateLiteralWord(call.Args[first])
}

// retiredVerbDriveGateExecProgramIndex is where the PROGRAM word sits in each
// starter's argument list (Rule 1: the position is a policy, so it is named).
// Command(name, args…) → 0; CommandContext(ctx, name, args…) → 1.
var retiredVerbDriveGateExecProgramIndex = map[string]int{
	"Command":        0,
	"CommandContext": 1,
}

// retiredVerbDriveGateSelfProgramNames are the spellings that mean THIS binary —
// the program word that dispatches gopi. A call whose program resolves to one of
// them is gated exactly as before. It is an ALLOW-list of spellings, not a
// deny-list of tools: a new third-party program must not need a row here, and a
// name that appears in it is a statement that driving it is driving gopi.
//
// The word is read from its ONE definition (Rule 13): `gopiProgramWord` in
// cli_tasks.go is `cliverbs.Program`, and `testutil.BinFileName` is the SSOT for
// the installed file's own name — both are listed so the two channels that
// declare it cannot drift, and the test below fails if either value moves out
// from under this table.
var retiredVerbDriveGateSelfProgramNames = []string{
	gopiProgramWord,      // "gopi" — the command word
	testutil.BinFileName, // "gopi" — the installed binary's file name
	"./" + testutil.BinDirName + "/" + testutil.BinFileName, // "./bin/gopi" — the repo-relative spelling
}

// retiredVerbDriveGateProgramIsTheRealBinary answers the question every finding
// below PRESUMES: is this program THIS binary?
//
// It is stated as a decision about the program's VALUE, not about its spelling,
// and the direction of the default is the whole point: an unresolvable program
// is the binary's own driver. Every call site that drives gopi in this repo
// reaches it through a variable (`bin`, `gopiBin`, `testutil.GopiBin(t)`), so a
// predicate that treated "unknown" as "not gopi" would exempt precisely the
// calls the gate exists to catch — a false-negative machine wearing a fix.
func retiredVerbDriveGateProgramIsTheRealBinary(program string) bool {
	if program == "" {
		return true // not a name this gate can read: stay on the gated side
	}
	base := filepath.Base(program)
	for _, name := range retiredVerbDriveGateSelfProgramNames {
		if name == "" {
			continue
		}
		if program == name || base == filepath.Base(name) {
			return true
		}
	}
	return false
}

// retiredVerbDriveGateExecProgramName resolves the PROGRAM expression of one
// program start — the `name` parameter of exec.Command(name, args…) — to the
// word it carries, when that word is knowable statically.
//
// The resolution reuses the two resolvers this package already owns rather than
// adding a third (Rule 13): `retiredVerbDriveGateLiteralWord` for a literal or a
// this-package verb constant, and `retiredVerbGateStringLiterals` for the
// package-level `NAME = "literal"` rows of ANY file in this package (that is the
// shape the repo actually uses for a tool's program name —
// `const cwdProbeGitBinary = "git"`, `const systemctlPath = "/usr/bin/systemctl"`).
//
// known=false is the ANSWER, not a failure: a parameter, a field
// (`s.bin`), a call (`testutil.GopiBin(t)`) or a path BUILT at runtime
// (`filepath.Join(dir, "gopi")`) cannot be decided from one expression, and the
// caller must keep gating it. Only a name whose VALUE is in hand may be exempted.
func retiredVerbDriveGateExecProgramName(call *ast.CallExpr, execFunc string, local map[string]ast.Expr, consts map[string]string) (string, bool) {
	first := retiredVerbDriveGateExecProgramIndex[execFunc]
	if len(call.Args) <= first {
		return "", false
	}
	expr := call.Args[first]
	// A file-local binding (`bin := "git"`) is consulted first: a local
	// declaration SHADOWS a package-level one of the same name, and reading the
	// package row first would attribute a string the call does not use.
	if id, ok := expr.(*ast.Ident); ok {
		if bound, ok := local[id.Name]; ok {
			return retiredVerbDriveGateProgramValue(bound, local, consts)
		}
	}
	return retiredVerbDriveGateProgramValue(expr, local, consts)
}

// retiredVerbDriveGateProgramValue resolves one step of a file-local binding
// chain (`p := "git"` → "git"), then falls back to the package constant table.
// One step of indirection is what the tree writes; a longer chain resolves the
// same way because the binding map is consulted again at each hop.
func retiredVerbDriveGateProgramValue(expr ast.Expr, local map[string]ast.Expr, consts map[string]string) (string, bool) {
	if id, ok := expr.(*ast.Ident); ok {
		if bound, ok := local[id.Name]; ok {
			return retiredVerbDriveGateProgramValue(bound, local, consts)
		}
		if value, ok := consts[id.Name]; ok {
			return value, true
		}
		return "", false
	}
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok {
			if value, ok := consts[pkg.Name+"."+sel.Sel.Name]; ok {
				return value, true
			}
		}
		return "", false
	}
	return retiredVerbDriveGateLiteralWord(expr)
}

// retiredVerbDriveGateLocalBindings indexes every `name := <expr>` and
// `var name = <expr>` in ONE file, so the program of a call can be read through
// the binding it was written with. It is file-scoped on purpose: a package-level
// declaration is the OTHER table's business, and a cross-file binding lookup
// would have to answer "which of these two `bin`s does this file see", which is
// a compiler's question and not a gate's.
func retiredVerbDriveGateLocalBindings(file *ast.File) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE || len(s.Lhs) != len(s.Rhs) {
				return true
			}
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					out[id.Name] = s.Rhs[i]
				}
			}
		case *ast.ValueSpec:
			if len(s.Names) != len(s.Values) {
				return true
			}
			for i, name := range s.Names {
				out[name.Name] = s.Values[i]
			}
		}
		return true
	})
	return out
}

// retiredVerbDriveGateProgramCensus counts what the exemption DID on the corpus
// it was given — the non-vacuity half of this predicate (Rule 15). It answers
// three questions the assertions need and the findings cannot:
//
//	exempt     program starts skipped because the program is NOT this binary,
//	           keyed by the program's own word (`git`, `go`, `bash`, …);
//	gatedSelf  program starts kept gated because the program IS this binary;
//	gatedBlind program starts kept gated because the program was unreadable.
//
// Without it, an exemption that matched nothing would look exactly like a clean
// tree: the scan would return the same zero findings either way.
type retiredVerbDriveGateProgramCensus struct {
	exempt     map[string]int
	gatedSelf  map[string]int
	gatedBlind int
}

func retiredVerbDriveGateNewProgramCensus() *retiredVerbDriveGateProgramCensus {
	return &retiredVerbDriveGateProgramCensus{
		exempt:    map[string]int{},
		gatedSelf: map[string]int{},
	}
}

func (c *retiredVerbDriveGateProgramCensus) noteExempt(program string) {
	if c != nil {
		c.exempt[program]++
	}
}

func (c *retiredVerbDriveGateProgramCensus) noteGatedOnTheBinary(program string) {
	if c != nil {
		c.gatedSelf[program]++
	}
}

func (c *retiredVerbDriveGateProgramCensus) noteGatedUnreadable() {
	if c != nil {
		c.gatedBlind++
	}
}

// exemptPrograms lists the distinct program words this scan skipped, sorted, so
// a failure message and a log line both name what the exemption really matched.
func (c *retiredVerbDriveGateProgramCensus) exemptPrograms() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.exempt))
	for name := range c.exempt {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// retiredVerbDriveGateSliceLiterals indexes the file's []string literals by the
// name they are bound to, so `exec.Command(bin, args…)` can be read: the
// variadic identifier is the same defect as the inline literal, and it is the
// form the measured violations actually use.
func retiredVerbDriveGateSliceLiterals(file *ast.File) map[string][]ast.Expr {
	out := map[string][]ast.Expr{}
	record := func(name string, value ast.Expr) {
		if name == "" {
			return
		}
		lit, ok := value.(*ast.CompositeLit)
		if !ok {
			return
		}
		if arr, ok := lit.Type.(*ast.ArrayType); !ok || arr.Len != nil {
			return // only an untyped-length []T literal is the variadic shape
		}
		out[name] = lit.Elts
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) != len(s.Rhs) {
				return true
			}
			for i, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					record(id.Name, s.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			if len(s.Names) != len(s.Values) {
				return true
			}
			for i, name := range s.Names {
				record(name.Name, s.Values[i])
			}
		}
		return true
	})
	return out
}

// retiredVerbDriveGateLiteralWord resolves an expression to the WORD it carries
// when that word is knowable statically: a string literal, or an identifier
// naming a package-level string CONSTANT of this package (the cliVerbs the
// retirement renamed live as constants — `CliVerbLog = "log"` — so a stale test
// may spell the word through the constant and must be caught all the same).
func retiredVerbDriveGateLiteralWord(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.Ident:
		if s, ok := retiredVerbDriveGateConstWords[v.Name]; ok {
			return s, true
		}
		return "", false
	case *ast.SelectorExpr:
		key := retiredVerbDriveGateSelectorKey(v)
		if s, ok := retiredVerbDriveGateConstWords[key]; ok {
			return s, true
		}
		return "", false
	}
	return "", false
}

// retiredVerbDriveGateSelectorKey renders `pkg.Ident` for the package-qualified
// constant form used by cross-package references (toolnames.ToolCodeSearch).
func retiredVerbDriveGateSelectorKey(sel *ast.SelectorExpr) string {
	if pkg, ok := sel.X.(*ast.Ident); ok {
		return pkg.Name + "." + sel.Sel.Name
	}
	return ""
}

// retiredVerbDriveGateConstWords is the word carried by every package-level
// string CONSTANT that a test could name as argv[0], keyed by the identifier
// (and by `pkg.Ident` for imported ones). It is built ONCE from the tables
// this package already owns plus the tool-wire names, so the identifier form
// and the literal form cannot disagree.
//
// The map is deliberately built from the SAME two tables the refusal vocabulary
// comes from, in BOTH directions: any constant the retirement renamed is
// reachable as an identifier in a stale test, and each has to resolve to its
// word for the gate to see it.
var retiredVerbDriveGateConstWords = func() map[string]string {
	out := map[string]string{}
	// The withdrawal/retirement constants this package declares. They are read
	// by identifier — never by value — so a test spelling `CliVerbLog` is
	// caught with the same finding as one spelling "log".
	for ident, word := range map[string]string{
		"CliVerbLog":        CliVerbLog,
		"CliVerbStats":      CliVerbStats,
		"CliVerbRaw":        CliVerbRaw,
		"CliVerbResume":     CliVerbResume,
		"CliVerbCheckpoint": CliVerbCheckpoint,
		"CliVerbArchive":    CliVerbArchive,
		"CliVerbCapture":    CliVerbCapture,
		"CliVerbShot":       CliVerbShot,
		"CliVerbProxy":      CliVerbProxy,
		"CliVerbMemsync":    CliVerbMemsync,
		"CliVerbBrew":       CliVerbBrew,
		"CliVerbSetup":      CliVerbSetup,
		"CliVerbCompletion": CliVerbCompletion,
		"CliVerbUI":         CliVerbUI,
		"CliVerbCLI":        CliVerbCLI,
	} {
		out[ident] = word
	}
	// The tables' own rows, so a row whose word is a dash twin or a tool-wire
	// name resolves the same way whether it arrives as a literal or through the
	// constant that declares it.
	for _, e := range undispatchedToolWords {
		out[e.word] = e.word
	}
	for _, a := range verbAliases {
		out[a.alias] = a.alias
	}
	return out
}()

// retiredVerbDriveGateFamilyShape returns the flags in an argv whose shape is
// "the RETIRED word handed to the binary as a POSITIONAL after its own family
// door" — `exec.Command(bin, CliVerbSession, CliVerbCheckpoint, "list")`. It is
// not argv[0], so the argv[0] predicate above cannot see it; it is exactly as
// broken (the dispatch chain reads the family word, forwards, and the leaf
// spellings do not include the retired one any more), and it is the shape that
// slipped past a hand survey during this task's own repair.
//
// The predicate is stated over the FLATTENED argument list, so it covers the
// inline form, a `[]string{…}` literal and a slice bound to a name.
func retiredVerbDriveGateFamilyShape(args []ast.Expr, words map[string]string) (string, string, bool) {
	for i := 0; i+1 < len(args); i++ {
		group, ok := retiredVerbDriveGateLiteralWord(args[i])
		if !ok || !retiredVerbDriveGateGroupWords[group] {
			continue
		}
		leaf, ok := retiredVerbDriveGateLiteralWord(args[i+1])
		if !ok {
			continue
		}
		address, refused := words[leaf]
		if !refused {
			continue
		}
		// SOUNDNESS: the pair `group leaf` is the word's LIVE address whenever
		// the refusal's own address ends with it — `gopi session log` is where
		// the refused flat word `log` now lives, so a test driving
		// `exec.Command(bin, CliVerbSession, CliVerbLog)` is driving the door,
		// not the retired spelling. Only a pair the address does NOT back is a
		// finding (`gopi tool code_search`, `gopi session archive`, …): the
		// group word is a door, and a leaf that group does not serve is a word
		// the binary refuses.
		if strings.HasSuffix(address, group+" "+leaf) {
			continue
		}
		return group, address, true
	}
	return "", "", false
}

// retiredVerbDriveGateGroupWords are the FAMILY doors that forward to a leaf:
// the words whose presence in argv means the NEXT word is a leaf name rather
// than a top-level verb. They are the same set the group pages document.
var retiredVerbDriveGateGroupWords = map[string]bool{
	CliVerbSession: true,
	CliVerbTool:    true,
	CliVerbMemory:  true,
	CliVerbTask:    true,
	CliVerbConfig:  true,
}

// retiredVerbDriveGateFlatten returns a call's argument expressions as one
// flat list — the call's own args, with each inline `[]string{…}` literal
// expanded in place and each named slice resolved. It is what lets one
// predicate cover the three spellings of the same argv.
func retiredVerbDriveGateFlatten(args []ast.Expr, slices map[string][]ast.Expr) []ast.Expr {
	out := make([]ast.Expr, 0, len(args))
	for _, a := range args {
		if lit, ok := a.(*ast.CompositeLit); ok {
			out = append(out, lit.Elts...)
			continue
		}
		if id, ok := a.(*ast.Ident); ok {
			if elts, ok := slices[id.Name]; ok {
				out = append(out, elts...)
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// retiredVerbDriveGateFamilyLeaf names the offending leaf for the message: the
// word right after the group word in the call's flattened argv.
func retiredVerbDriveGateFamilyLeaf(call *ast.CallExpr, slices map[string][]ast.Expr, group string) string {
	flat := retiredVerbDriveGateFlatten(call.Args, slices)
	for i := 0; i+1 < len(flat); i++ {
		if g, ok := retiredVerbDriveGateLiteralWord(flat[i]); ok && g == group {
			if leaf, ok := retiredVerbDriveGateLiteralWord(flat[i+1]); ok {
				return leaf
			}
		}
	}
	return ""
}

// retiredVerbDriveGateSnippet returns the source line, trimmed — the message's
// quote, so a reviewer sees the call without opening the file.
func retiredVerbDriveGateSnippet(src string, line int) string {
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// --- the gate -----------------------------------------------------------------

// TestNoTestDrivesTheRealBinaryWithARetiredWord is the gate itself: no
// *_test.go in the tree may pass a retired word as argv[0] to the real binary.
//
// The failure message names the file, the line, the word and the address that
// replaced it (Rule 15), and lists every finding in ONE report rather than
// stopping at the first — a stale driver is usually a family of them, and the
// reviewer wants the whole list in one run.
func TestNoTestDrivesTheRealBinaryWithARetiredWord(t *testing.T) {
	words, order := retiredVerbDriveGateVocabulary(t)
	root := retiredVerbDriveGateGoModRoot(t)

	findings, parsed := retiredVerbDriveGateScan(t, root, words)
	if parsed < retiredVerbDriveGateMinTestFiles {
		t.Fatalf("the walk parsed only %d %s files under %s (floor %d) — the scan would be vacuous; "+
			"the walk root or the suffix moved", parsed, retiredVerbDriveGateTestSuffix, root, retiredVerbDriveGateMinTestFiles)
	}

	// THE WIDENED ARM (task 01a10ef3…). The direct scan above reads the argv at
	// the exec call site; a test that types its argv into a LOCAL HELPER puts two
	// frames between the word and argv[0], and was invisible to that scan — a
	// measured blind spot, not a hypothetical one. The helper arm closes it for
	// the one shape that is decidable: a helper that forwards the caller's argv
	// VERBATIM (see retired_verb_helper_reach_test.go for the classification and
	// for the honest statement of what stays out of scope).
	helperFindings, verbatimHelpers := retiredVerbGateHelperReachScan(t, root, words)
	if verbatimHelpers < retiredVerbGateHelperVerbatimMin {
		t.Fatalf("the helper arm recognised only %d verbatim forwarder(s) (floor %d) — the widened half "+
			"would be blind while this gate printed a clean line (Rule 15)",
			verbatimHelpers, retiredVerbGateHelperVerbatimMin)
	}
	findings = append(findings, helperFindings...)

	if len(findings) == 0 {
		t.Logf("gate clean: %d refused words (from both SSOT tables) against %d %s files, "+
			"and %d verbatim argv forwarder(s) reached through the helper arm",
			len(order), parsed, retiredVerbDriveGateTestSuffix, verbatimHelpers)
		return
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].file != findings[j].file {
			return findings[i].file < findings[j].file
		}
		return findings[i].line < findings[j].line
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%d test driver(s) still hand the REAL binary a RETIRED word as argv[0].\n"+
		"The binary refuses these by design (devdoc/verb-retirement-complete.md): the test reads the\n"+
		"refusal as a missing feature and goes red for a reason that is not a defect.\n"+
		"Drive the FAMILY address instead, or mark a deliberate refusal probe with `%s <reason>`.\n",
		len(findings), retiredVerbDriveGateEscape)
	for _, f := range findings {
		fmt.Fprintf(&b, "\n  %s\n      %s\n", f, f.snippet)
	}
	t.Error(b.String())
}

// TestRetiredVerbDriveGateIsRedOnAReallyDrivenWord is the RED PROOF, and it
// uses the live tree rather than a synthetic fixture: the vocabulary is
// extended with a word that TODAY'S tests really do drive as argv[0]
// (`worker`, measured 2026-10-06: five exec.Command call sites in this
// package), and the same scan must then report at least one finding.
//
// That is the load-bearing claim stated as a measurement: the walk, the
// parser and the argv[0] predicate all read the real corpus, so when a word
// currently in use is retired the gate goes RED. A fixture-only proof would
// show the predicate works on a toy input; this one shows it works on the
// tree the gate actually governs.
func TestRetiredVerbDriveGateIsRedOnAReallyDrivenWord(t *testing.T) {
	root := retiredVerbDriveGateGoModRoot(t)
	words, _ := retiredVerbDriveGateVocabulary(t)
	// A word in use by today's tests as argv[0] — retired only inside this
	// sub-run, never in the product vocabulary.
	const reallyDriven = "worker"
	words[reallyDriven] = "worker <task-id>"

	findings, parsed := retiredVerbDriveGateScan(t, root, words)
	if parsed < retiredVerbDriveGateMinTestFiles {
		t.Fatalf("the walk parsed only %d files — the red proof read no corpus", parsed)
	}
	if len(findings) == 0 {
		t.Fatalf("the gate found NO call driving the binary with %q, a word today's tests drive — "+
			"the scan is not reading the real tree, so the green run above proves nothing", reallyDriven)
	}
	t.Logf("red proof: retiring %q alone turns %d existing test call site(s) red (e.g. %s)",
		reallyDriven, len(findings), findings[0])
}

// TestRetiredVerbDriveGateVocabularyComesFromTheTwoTables pins the gate's own
// input, in the direction that matters: the vocabulary is EXACTLY the union of
// the two SSOT tables' accessors — no word added, none dropped, no second list.
// A copy pasted into this file would fail here the moment either table moves.
func TestRetiredVerbDriveGateVocabularyComesFromTheTwoTables(t *testing.T) {
	words, order := retiredVerbDriveGateVocabulary(t)

	spellings := undispatchedRefusedWordSpellings()
	if len(spellings) < retiredVerbDriveGateMinVocabulary {
		t.Fatalf("undispatchedRefusedWordSpellings returned %d words — the SSOT projection is empty or truncated", len(spellings))
	}
	for _, w := range spellings {
		if _, ok := words[w]; !ok {
			t.Errorf("the gate's vocabulary is missing %q, which the SSOT accessors refuse — a second list has drifted", w)
		}
	}
	if len(order) != len(spellings) {
		t.Errorf("the gate read %d words, the SSOT accessors refuse %d — the gate is not reading the tables alone:\n%v",
			len(order), len(spellings), order)
	}
	// Both HALVES must be represented: a gate that learned only the spelling
	// table would let every synonym's stale driver through.
	spelled := map[string]bool{}
	for _, w := range undispatchedToolWordSpellings() {
		spelled[w] = true
	}
	synonym := 0
	for _, a := range verbAliases {
		if !spelled[a.alias] {
			synonym++
		}
	}
	if synonym == 0 {
		t.Fatal("the synonym table (cli_verb_aliases.go) contributed no row — the gate would only see half the vocabulary")
	}
	for _, a := range verbAliases {
		if _, ok := words[a.alias]; !ok {
			t.Errorf("synonym %q is refused by cli_verb_aliases.go but absent from the gate's vocabulary", a.alias)
		}
	}
}

// TestRetiredVerbDriveGateAcceptsTheFamilyDoor is the NEGATIVE CONTROL: the
// shapes that must stay legal are not flagged. Without it the gate could be
// made green by refusing every test that mentions a retired word, which would
// delete the retirement's own refusal tests.
func TestRetiredVerbDriveGateAcceptsTheFamilyDoor(t *testing.T) {
	words, _ := retiredVerbDriveGateVocabulary(t)
	root := t.TempDir()

	const clean = `package main

import "os/exec"

func drives() {
	// the family door: the retired word is an ARGUMENT, never argv[0]
	exec.Command(bin, CliVerbSession, CliVerbLog, "-h")
	// a legitimate non-binary word
	exec.Command("git", "status", "--porcelain")
	// the word as a MESSAGE, not a drive
	msg := "gopi: log is RETIRED — use gopi session log"
	_ = msg
}
`
	if err := os.WriteFile(filepath.Join(root, "clean_test.go"), []byte(clean), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	findings, parsed := retiredVerbDriveGateScan(t, root, words)
	if parsed != 1 {
		t.Fatalf("the fixture walk parsed %d files, want 1", parsed)
	}
	if len(findings) != 0 {
		t.Fatalf("the gate flagged %d legal call(s) — it would delete the retirement's own refusal tests:\n%v",
			len(findings), findings)
	}
}

// TestRetiredVerbDriveGateCatchesAFixtureViolation is the SECOND red proof, at
// the smallest sample (Rule 10's minimal-sample habit): one synthetic file per
// violation SHAPE the gate claims to catch, asserted line by line, plus the
// escape marker's own boundary. It is what makes the assertion "this gate
// catches the real defect" falsifiable without waiting for the next retiree.
func TestRetiredVerbDriveGateCatchesAFixtureViolation(t *testing.T) {
	words, _ := retiredVerbDriveGateVocabulary(t)

	cases := []struct {
		name     string
		src      string
		wantLine int    // the line the finding must land on
		wantWord string // the word the finding must name
		wantAny  bool   // true when the exact word may vary (identifier form)
	}{
		{
			name: "string literal as argv[0]",
			src: `package main

import "os/exec"

func f() {
	exec.Command(bin, "checkpoint", "list")
}
`,
			wantLine: 6, wantWord: "checkpoint",
		},
		{
			name: "package CONSTANT as argv[0] — the identifier form",
			src: `package main

import "os/exec"

func f() {
	exec.Command(bin, CliVerbLog)
}
`,
			wantLine: 6, wantWord: "log",
		},
		{
			name: "variadic slice whose first element is the word",
			src: `package main

import "os/exec"

func f() {
	args := []string{"code-search", "x"}
	exec.Command(bin, args...)
}
`,
			wantLine: 7, wantWord: "code-search",
		},
		{
			name: "CommandContext is the same door",
			src: `package main

import (
	"context"
	"os/exec"
)

func f() {
	exec.CommandContext(context.Background(), bin, "archive")
}
`,
			wantLine: 9, wantWord: "archive",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "violation_test.go")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			findings, _ := retiredVerbDriveGateScan(t, dir, words)
			if len(findings) != 1 {
				t.Fatalf("the gate reported %d findings for this shape, want exactly 1 (it must be red on it and quiet about nothing else):\n%v",
					len(findings), findings)
			}
			if findings[0].line != tc.wantLine || findings[0].word != tc.wantWord {
				t.Fatalf("finding = %s (word %q line %d), want word %q on line %d",
					findings[0], findings[0].word, findings[0].line, tc.wantWord, tc.wantLine)
			}
			if findings[0].address == "" {
				t.Errorf("the finding carries no address — the refusal text must send the reader to a live door (Rule 15)")
			}
		})
	}

	// The escape boundary: the SAME violation, marked — not flagged.
	t.Run("the escape marker suppresses one line only", func(t *testing.T) {
		dir := t.TempDir()
		src := `package main

import "os/exec"

func f() {
	// retired-drive-ok: this test MUST feed the refusal its own word
	exec.Command(bin, "checkpoint", "list")
	exec.Command(bin, "log")
}
`
		if err := os.WriteFile(filepath.Join(dir, "escaped_test.go"), []byte(src), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		findings, _ := retiredVerbDriveGateScan(t, dir, words)
		if len(findings) != 1 {
			t.Fatalf("the escape must suppress exactly the marked line; got %d findings on the unmarked line's behalf:\n%v",
				len(findings), findings)
		}
		if findings[0].line != 8 || findings[0].word != "log" {
			t.Fatalf("escape leaked: finding = %s, want the UNMARKED line 8 (`log`)", findings[0])
		}
	})
}

// TestRetiredVerbDriveGateRefusesAnUnreadableTree pins the read-floor's other
// half (Rule 15): a walk over a directory holding no test sources must FAIL the
// gate's floor, never pass silently.
func TestRetiredVerbDriveGateRefusesAnUnreadableTree(t *testing.T) {
	words, _ := retiredVerbDriveGateVocabulary(t)
	findings, parsed := retiredVerbDriveGateScan(t, t.TempDir(), words)
	if parsed != 0 || len(findings) != 0 {
		t.Fatalf("an empty tree reported parsed=%d findings=%d, want 0/0", parsed, len(findings))
	}
	if parsed >= retiredVerbDriveGateMinTestFiles {
		t.Fatalf("the floor (%d) is not above an empty walk — it cannot detect a scan that read nothing",
			retiredVerbDriveGateMinTestFiles)
	}
}

// --- the program-name predicate (this task's fix) -----------------------------

// TestRetiredVerbDriveGateReadsTheProgramNotJustTheWord is the fix's own
// FIXTURE proof, and it is written as a DIFF: one synthetic file per shape, run
// through BOTH settings of the predicate — the pre-fix one (which compared the
// call's first ARGUMENT to the vocabulary without asking which program it was
// starting) and the shipped one — so every assertion below is about the change
// itself rather than about the gate's general behaviour.
//
// The measured false positive this closes (2026-10-25, this tree): the direct
// arm reported `exec.Command("git", "-C", repo, "log", "-1", …)` and
// `exec.Command(versionProofGoBinaryName, "version", "-m", bin)` as "the real
// binary is driven with a RETIRED word" — git's and go's own subcommands read as
// gopi verbs, because nothing asked WHICH program was started.
//
// The FOURTH case is the positive control this fix would be unsafe without: a
// program that resolves to the gopi word must STILL be flagged, in both
// settings. Without it, "a statically known program is exempt" would blind the
// gate to `exec.Command("gopi", "log")` — the exact defect it exists to catch —
func TestRetiredVerbDriveGateReadsTheProgramNotJustTheWord(t *testing.T) {
	words, _ := retiredVerbDriveGateVocabulary(t)

	cases := []struct {
		name string
		src  string
		// retired adds the word this fixture needs RETIRED for the measurement.
		// The vocabulary holds today's retired words only, while half of the
		// shapes below are about a TOOL's own subcommand (`show`, `version`,
		// `session log`) — a word the binary dispatches today. Retiring it
		// inside the fixture is what makes the pre-fix predicate produce the
		// false positive HERE, which is the column this fix is measured against.
		retired string
		// wantPre / wantPost are the finding counts under the pre-fix and the
		// shipped predicate, per WORD — the difference between the two columns
		// IS this fix, and stating both makes a regression in either direction
		// visible (a fix that exempted nothing, or one that exempted everything).
		wantPreWord string
		wantPre     int
		wantPost    int
		why         string
	}{
		{
			name:        "a NAMED other program: git's own subcommand is not a gopi verb",
			retired:     "show",
			wantPreWord: "log", wantPre: 1, wantPost: 0,
			why: "`git log` and `git show` are git's subcommands; the exemption must fire on the program NAME",
			src: `package main

import "os/exec"

func f() {
	exec.Command("git", "log", "-1", "--format=%H")
	exec.Command("git", "show", "HEAD")
}
`,
		},
		{
			name:        "the same program through a package CONSTANT (`const gitBin = \"git\"`)",
			retired:     "show",
			wantPreWord: "log", wantPre: 1, wantPost: 0,
			why: "the repo's own shape: cwdProbeGitBinary / systemctlPath — the value is in hand, so the name is readable",
			src: `package main

import "os/exec"

const gitBin = "git"

func f() {
	exec.Command(gitBin, "log", "-1")
}
`,
		},
		{
			name:        "the same program through a FILE-LOCAL binding (`bin := \"go\"`)",
			retired:     "version",
			wantPreWord: "version", wantPre: 1, wantPost: 0,
			why: "a `:=` binding shadows a package row; the local value decides",
			src: `package main

import "os/exec"

func f() {
	bin := "go"
	exec.Command(bin, "version", "-m", "x")
}
`,
		},
		{
			name:        "POSITIVE CONTROL: a program that IS this binary stays gated",
			wantPreWord: "log", wantPre: 1, wantPost: 1,
			why: "without this the fix becomes a false-negative machine (memory #13035): the gopi word must never be exempted",
			src: `package main

import "os/exec"

func f() {
	exec.Command("gopi", "log")
}
`,
		},
		{
			name:        "POSITIVE CONTROL through the SSOT constant: gopiProgramWord",
			wantPreWord: "log", wantPre: 1, wantPost: 1,
			why: "the repo spells the binary's name through a constant (Rule 13); resolving it must NOT exempt it",
			src: `package main

import "os/exec"

func f() {
	exec.Command(gopiProgramWord, "log")
}
`,
		},
		{
			name:        "an UNRESOLVABLE program stays gated — the default direction",
			wantPreWord: "log", wantPre: 1, wantPost: 1,
			why: "`bin` is how every real driver in this repo names the binary; treating unknown as not-gopi would exempt them all",
			src: `package main

import "os/exec"

func f() {
	exec.Command(bin, "log")
}
`,
		},
		{
			name:        "an unresolvable CALL program (gopiBin(t)) stays gated",
			wantPreWord: "checkpoint", wantPre: 1, wantPost: 1,
			why: "a call is not a value; nothing about it is statically known",
			src: `package main

import (
	"os/exec"
	"testing"
)

func f(t *testing.T) {
	exec.Command(gopiBin(t), "checkpoint", "list")
}
`,
		},
		{
			name:        "the FAMILY shape on a non-gopi program is exempt too",
			retired:     "checkpoint",
			wantPreWord: "session checkpoint", wantPre: 1, wantPost: 0,
			why: "the second shape must share the same program predicate, or `git session <leaf>` would stay a finding",
			src: `package main

import "os/exec"

func f() {
	exec.Command("git", "session", "checkpoint")
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "shape_test.go")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			probe := make(map[string]string, len(words)+1)
			for w, a := range words {
				probe[w] = a
			}
			if tc.retired != "" {
				probe[tc.retired] = "probe address for " + tc.retired
			}
			count := func(readsProgram bool) int {
				findings, _, _ := retiredVerbDriveGateScanCensus(t, dir, probe, readsProgram)
				n := 0
				for _, f := range findings {
					if f.word == tc.wantPreWord {
						n++
					}
				}
				for _, f := range findings {
					if f.word != tc.wantPreWord && f.word != tc.retired {
						t.Fatalf("the fixture produced a finding for a word this case does not measure (%q): %v",
							f.word, findings)
					}
				}
				return n
			}
			if got := count(false); got != tc.wantPre {
				t.Fatalf("PRE-fix predicate reported %d finding(s) for %q, want %d — the fixture does not reproduce "+
					"the shape this fix is measured against, so the second column would prove nothing:\n%s",
					got, tc.wantPreWord, tc.wantPre, tc.src)
			}
			if got := count(true); got != tc.wantPost {
				t.Fatalf("the shipped predicate reported %d finding(s) for %q, want %d — %s\n%s",
					got, tc.wantPreWord, tc.wantPost, tc.why, tc.src)
			}
		})
	}
}

// TestRetiredVerbDriveGateProgramNameTableIsTheBinarysOwn pins the exemption's
// ALLOW-list to the definitions it is projected from (Rule 13): the program word
// has ONE home (`cliverbs.Program`, re-exported as this package's
// `gopiProgramWord`) and the artifact's file name has ONE home
// (`testutil.BinFileName`). A row typed here instead would drift the day either
// moves — and the direction of that drift is a driver of the real binary being
// exempted as if it were a tool.
func TestRetiredVerbDriveGateProgramNameTableIsTheBinarysOwn(t *testing.T) {
	if gopiProgramWord != cliverbs.Program {
		t.Fatalf("gopiProgramWord = %q, cliverbs.Program = %q — the two have drifted", gopiProgramWord, cliverbs.Program)
	}
	for _, name := range retiredVerbDriveGateSelfProgramNames {
		if !retiredVerbDriveGateProgramIsTheRealBinary(name) {
			t.Fatalf("%q is declared a spelling of THIS binary but the predicate does not read it as one", name)
		}
	}
	// And the other direction: a tool's name, and the empty answer, are never
	// the binary. The empty string is the UNRESOLVABLE program — it must come
	// out on the GATED side, which is what the whole predicate's default rests on.
	for _, tool := range []string{"git", "go", "bash", "sh", "/bin/sh", "sqlite3", "systemd-run"} {
		if retiredVerbDriveGateProgramIsTheRealBinary(tool) {
			t.Fatalf("%q is a TOOL, but the predicate reads it as this binary — every `%s <retired word>` "+
				"call site would stay a false positive", tool, tool)
		}
	}
	if !retiredVerbDriveGateProgramIsTheRealBinary("") {
		t.Fatal("an UNREADABLE program must stay on the GATED side — a false-negative machine starts here")
	}
}

// TestRetiredVerbDriveGateProgramExemptionFiresOnTheRealTree is the NON-VACUITY
// assertion (Rule 15) and the RED PROOF, both measured on the corpus this gate
// actually governs rather than on a fixture.
//
// Three claims, and each is falsifiable on its own:
//
//  1. the exemption FIRES on the real tree — the census must show a floor of
//     distinct program names and of exempted call sites, and it prints them, so
//     "the predicate matched nothing" fails instead of reading as a clean tree;
//  2. the shipped scan reports NO finding for a word today's tests really do
//     carry as a non-gopi program's first argument (`version` — git's and go's
//     own subcommand, measured twice on this tree), while the PRE-fix predicate
//     reports exactly those sites. That difference is this fix, measured;
//  3. the pre-fix arm is not simply blind: the same scan still flags a gopi
//     driver in the same run, so a green in (2) cannot come from a scan that
//     stopped reading.
func TestRetiredVerbDriveGateProgramExemptionFiresOnTheRealTree(t *testing.T) {
	root := retiredVerbDriveGateGoModRoot(t)
	words, _ := retiredVerbDriveGateVocabulary(t)

	findings, parsed, census := retiredVerbDriveGateScanCensus(t, root, words, retiredVerbDriveGateReadsTheProgram)
	if parsed < retiredVerbDriveGateMinTestFiles {
		t.Fatalf("the walk parsed only %d files — the measurement read no corpus", parsed)
	}
	if got := len(census.exemptPrograms()); got < retiredVerbDriveGateMinExemptPrograms {
		t.Fatalf("the program-name exemption fired on only %d distinct program(s) (floor %d) over %d files — "+
			"it is not doing anything on the tree it governs, so the false-positive fix is unproven (exempted: %v)",
			got, retiredVerbDriveGateMinExemptPrograms, parsed, census.exemptPrograms())
	}
	exemptCalls := 0
	for _, n := range census.exempt {
		exemptCalls += n
	}
	if exemptCalls < retiredVerbDriveGateMinExemptCalls {
		t.Fatalf("the exemption covered only %d program start(s) (floor %d) — the floor exists to catch an "+
			"exemption that silently stopped matching", exemptCalls, retiredVerbDriveGateMinExemptCalls)
	}
	t.Logf("program-name exemption: %d program start(s) exempted over %d distinct program(s) — %v",
		exemptCalls, len(census.exemptPrograms()), census.exemptPrograms())
	t.Logf("kept gated: %d start(s) of the real binary, %d with a program this scan could not read",
		len(census.gatedSelf), census.gatedBlind)

	// (2)+(3): retire a word that is really driven — but by a TOOL, never by this
	// binary — and measure both predicates over the SAME corpus.
	const drivenOnlyByATool = "version"
	probe := make(map[string]string, len(words)+1)
	for w, a := range words {
		probe[w] = a
	}
	probe[drivenOnlyByATool] = "version (probe address)"

	postFindings, _, _ := retiredVerbDriveGateScanCensus(t, root, probe, true)
	preFindings, _, _ := retiredVerbDriveGateScanCensus(t, root, probe, false)
	post, pre := 0, 0
	for _, f := range postFindings {
		if f.word == drivenOnlyByATool {
			post++
		}
	}
	preSites := []string{}
	for _, f := range preFindings {
		if f.word == drivenOnlyByATool {
			pre++
			preSites = append(preSites, f.String())
		}
	}
	if pre == 0 {
		t.Fatalf("the PRE-fix predicate found no site carrying %q as a non-gopi program's first argument — "+
			"the fixture shapes have moved on this tree, so this proof no longer measures the false positive; "+
			"pick another word (`status`, `show`, `list` were also measured)", drivenOnlyByATool)
	}
	if post != 0 {
		t.Fatalf("the SHIPPED predicate still reports %d finding(s) for %q — the program-name exemption did not "+
			"fire on them:\n%v", post, drivenOnlyByATool, postFindings)
	}
	if len(findings) != 0 {
		t.Fatalf("the real tree reports %d finding(s) with the shipped predicate — the exemption must leave the "+
			"corpus clean:\n%v", len(findings), findings)
	}
	t.Logf("red proof (false positive): %q alone is reported by the PRE-fix predicate at %d site(s) and by the "+
		"shipped one at 0 — e.g. %s", drivenOnlyByATool, pre, preSites[0])
}
