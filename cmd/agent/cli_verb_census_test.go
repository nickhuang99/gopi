// cli_verb_census_test.go — the G7 gate: the checked-in verb census
// (devdoc/gopi-verb-census.md) is re-derived here from the SAME sources its
// generator reads, and the table is compared in BOTH directions.
//
// WHY A GATE AND NOT A DOC. The census's whole value is that its number is
// true. A table written by hand describes the dispatch as it was on the day
// somebody read it — the moment a verb is added to cmd/agent's dispatch chain,
// a hand-written table is silently wrong while still reading green. So the
// gate re-derives:
//
//   - DISPATCHED — the isXxxSubcommand predicates the dispatch chain in
//     cmd/agent/main.go calls, each resolved (through its own body, and one
//     helper hop) to the verb constant it compares argv[0] against, and that
//     constant resolved to its word from its single definition site.
//   - ADVERTISED — the command index the built binary prints for `gopi -h`.
//     Availability is decided by BUILDING it: no binary → the direction is
//     skipped with a log line, never silently passed as if it had been checked.
//
// Then: every dispatched verb must have EXACTLY ONE table row whose dispatched
// column is 1 (a missing row = the doc is stale, duplicate = ambiguous), and
// every row claiming a verb must be corroborated (the verb is dispatched, or
// the binary advertises it). The class column must agree with the drives the
// generator would compute for the argv-literal and runner-line shapes, and the
// census must clear a NON-VACUITY FLOOR: a table that lost most of its rows —
// the failure mode where a gate over an empty list passes everything — fails
// here instead of passing quietly.
package main

import (
	"github.com/nick/gopi/internal/heavytest"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const (
	censusDocPath = "devdoc/gopi-verb-census.md"

	// The table's own header — the gate locates the census by THIS line, so the
	// doc may carry any amount of prose around it.
	censusHeaderLine = "| verb | dispatched | advertised | test_drive | runner_line | usage_log | doc_recipe | help_only | class |"

	// Non-vacuity floor, moved WITH the chain it guards. Measured 2026-09-25 on
	// master: 35 verbs dispatched; the verb-retirement epic (2026-10-05)
	// removed ~30 predicates from main.go and the census shrank to match, so a
	// floor of 25 would refuse a healthy tree. The floor's job is unchanged —
	// distinguish a table that collapsed (a parse that found nothing, a doc
	// truncated in a merge) from a table that is merely smaller — so it sits a
	// little under the chain's measured size. Measured 2026-10-05: 12 rows.
	censusMinRows = 8

	// censusMinChainPredicates is the pipe count's own floor, and it moved WITH
	// the chain the way censusMinRows moved with the table (union resolution,
	// rework 01a10e11…, 2026-10-06). The verb-retirement epic (2026-10-05) is
	// THIS branch's intent: it withdraws every flat/legacy spelling so each
	// command has one door, which deliberately takes main.go's pre-run chain
	// from master's 43 predicates down to 20 (18 group/verb predicates plus
	// isUISubcommand / isTermSubcommand, which the union keeps dispatchable —
	// master's later ruling, cc8289b0). Master's floor of 30 was written
	// against the un-retired surface, so it now refuses a HEALTHY tree — and a
	// floor that refuses a healthy tree is the defect, not the tree. The
	// floor's job is unchanged: tell a chain that collapsed (a parse that found
	// nothing, a main.go truncated in a merge) from a chain that is merely
	// smaller by design. It sits under the measured 20 with room for the epic
	// to retire a few more words.
	censusMinChainPredicates = 15

	// The class of a verb a test hands a real (non-help) argv: any other class
	// is a claim the gate can falsify from the same corpora the generator reads.
	censusClassTest = "a:test"
)

// censusRow is one parsed row of the checked-in table.
type censusRow struct {
	verb       string
	dispatched int
	advertised int
	testDrive  int
	runnerLine int
	helpOnly   int
	class      string
}

// NOTE — there is exactly ONE verb-word projection in this file, and it is
// censusVerbWordMap. A second, narrower one used to sit here: `Word = "<verb>"`
// over a hand-listed window (cmd/agent/cli_verb*.go, cli_act.go,
// internal/toolnames) whose comment asserted "the two definition sites". That
// window was the defect — a verb word whose single definition is a LEAF CONSTANT
// PACKAGE (Rule 14: `CliVerbMerge = cliverbs.Merge` in internal/cliverbs) is not
// in it, and a second projector that silently disagrees with the live one is how
// a wrong site list survives review. It was dead code (nothing called it), so it
// is deleted rather than taught: a stale claim kept green by a test cannot
// outlive the reader who trusted it.
//
// The live reader's rule, stated once: resolve the alias, never assume the word
// was typed again in cmd/agent (Rule 13 forbids the second literal, so a word
// whose definition is a leaf package MUST be reachable this way).

// censusVerbWord reports whether a constant NAME is one of the verb constants
// this census projects (CliVerbX / ToolX) — never a help string or a flag.
func censusVerbWord(name string) bool {
	if !strings.HasPrefix(name, "CliVerb") && !strings.HasPrefix(name, "Tool") {
		return false
	}
	switch name {
	case "CliVerbPrefix", "ToolNamePrefix":
		return false
	}
	return true
}

// censusSelectorDir maps a selector's package QUALIFIER (the `cliverbs` in
// `cliverbs.Merge`) to the directory it names, relative to the repo root — read
// from the importing file's OWN import block, so a new leaf constant package
// needs no list edited here. The import path's last segment is the convention Go
// itself uses for a package name; a file that imports the path under an explicit
// ALIAS is honoured first (an aliased import would otherwise be misread as the
// path's last segment). The module prefix comes off `module` in go.mod, so the
// result is a directory in THIS tree and not a reconstructed import path.
func censusSelectorDir(repoRoot, modulePath, src, qualifier string) (string, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "census_imports.go", src, parser.ImportsOnly)
	if err != nil {
		return "", false
	}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			name = path[i+1:]
		}
		matched := name == qualifier
		if imp.Name != nil {
			// `import cbs "…/internal/cliverbs"` — the alias IS the spelling the
			// body uses, so match on it instead of the path's last segment.
			matched = imp.Name.Name == qualifier
		}
		if !matched {
			continue
		}
		rel := strings.TrimPrefix(path, modulePath)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return repoRoot, true
		}
		return filepath.Join(repoRoot, rel), true
	}
	return "", false
}

// censusModulePath reads `module <path>` out of go.mod — the only thing that
// turns an import path (github.com/nick/gopi/internal/cliverbs) into a directory
// in this tree. Unreadable or absent → "", which makes censusSelectorDir try the
// import path as if it were repo-relative; an unresolvable alias then stays loud
// (censusDispatched fails by name) rather than resolving to the wrong directory.
func censusModulePath(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(censusRel(t, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// censusLeafConstWord reads `<name> = "<word>"` for the named constant out of a
// leaf constant package — the other end of an alias (`cliverbs.Merge`). It reads
// the NEXT string literal after the name inside the same file, which is the
// constant's value for every shape the leaf packages use (`Merge = "merge"`).
// Nothing is typed here: the word comes off the definition site, which is the
// whole point of resolving the alias rather than carrying a second literal.
func censusLeafConstWord(dir string, name string) (string, bool) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return "", false
	}
	nameRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	litRe := regexp.MustCompile(`"([a-z_][a-z0-9_-]*)"`)
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			loc := nameRe.FindStringIndex(line)
			if loc == nil {
				continue
			}
			if m := litRe.FindStringSubmatch(line[loc[1]:]); m != nil {
				return m[1], true
			}
		}
	}
	return "", false
}

// censusVerbWordMap builds constant-name → verb-word over the WHOLE tree, because
// the verbs' constants do not live in one file: cli_verbs.go (CliVerbXxx), the
// per-feature files (cli_act.go's CliVerbAct), internal/toolnames (ToolWebSearch,
// ToolText2img) and internal/tools (CliVerbMemoryGroup, the aliased `memory`
// group word). Reading one file and calling the rest "shape changed" would make
// this gate fail on a healthy tree; reading all of them is the same read the
// generator does.
//
// ALIASES INTO A LEAF PACKAGE ARE RESOLVED, NOT DEMANDED TWICE (Rule 14). A
// shared top-level word is written ONCE, in a leaf constant package cmd/agent
// imports — `CliVerbMerge = cliverbs.Merge` (internal/cliverbs). cmd/agent does
// NOT hold a second literal for it, and Rule 13 forbids adding one, so a reader
// that only knows the cmd/agent/toolnames/tools windows cannot resolve the merge
// verb word at all: it reported `constant CliVerbMerge (from isMergeSubcommand)
// holds no verb word anywhere in the tree's constant sites` — the census calling
// a Rule-14-correct tree a defect. The fix is the reader's, and it is generic:
// the right-hand side's qualifier is resolved through the IMPORTING FILE's own
// import block (censusSelectorDir) and the word is read from that package
// (censusLeafConstWord). No package list to keep in sync — a new leaf constant
// package needs no edit here. An alias this reader cannot follow is NOT silently
// dropped: censusDispatched fails by name, which is the loud failure the merge
// word hit.
func censusVerbWordMap(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{"cmd/agent", "internal/toolnames", "internal/tools"} {
		matches, err := filepath.Glob(censusRel(t, root+"/*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", root, err)
		}
		for _, path := range matches {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i >= len(vs.Values) || !censusVerbWord(name.Name) {
							continue
						}
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if w, err := strconv.Unquote(lit.Value); err == nil {
								out[name.Name] = w
							}
						}
					}
				}
			}
		}
	}
	// second pass: aliases — `CliVerbMemory = tools.CliVerbMemoryGroup` (a leaf
	// package in the window above) and `CliVerbMerge = cliverbs.Merge` (a leaf
	// package that is NOT, resolved through the importing file's own import
	// block). Run only after every literal is known: a single pass would depend
	// on file order.
	for _, root := range []string{"cmd/agent"} {
		matches, _ := filepath.Glob(censusRel(t, root+"/*.go")) //stylegate:ignore glob for optional cleanup; the pattern is a constant so ErrBadPattern is unreachable
		for _, path := range matches {
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			src := string(b)
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				continue
			}
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i >= len(vs.Values) || !censusVerbWord(name.Name) {
							continue
						}
						sel, ok := vs.Values[i].(*ast.SelectorExpr)
						if !ok {
							continue
						}
						if w, ok := out[sel.Sel.Name]; ok {
							out[name.Name] = w
							continue
						}
						// The word is defined in a leaf package that is not one of
						// this file's literal windows (Rule 14): follow the import.
						qual, ok := sel.X.(*ast.Ident)
						if !ok {
							continue
						}
						dir, ok := censusSelectorDir(repoRoot(t), censusModulePath(t), src, qual.Name)
						if !ok {
							continue
						}
						if w, ok := censusLeafConstWord(dir, sel.Sel.Name); ok {
							out[name.Name] = w
						}
					}
				}
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("resolved only %d verb constants across the tree — the constant definition sites changed shape", len(out))
	}
	return out
}

// censusChainNames reads the `is<Name>Subcommand(dispatchArgs)` tests out of
// main.go — the dispatch chain itself, with no verb name in this file.
func censusChainNames(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(censusRel(t, "cmd/agent/main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	chainRe := regexp.MustCompile(`is([A-Za-z0-9]+)Subcommand\(dispatchArgs\)`)
	names := map[string]bool{}
	for _, m := range chainRe.FindAllStringSubmatch(string(src), -1) {
		names[m[1]] = true
	}
	if len(names) < censusMinChainPredicates {
		t.Fatalf("main.go's dispatch chain mentions only %d predicates — the chain changed shape", len(names))
	}
	return names
}

// censusDispatched derives the dispatched verb set from main.go's own dispatch
// chain: the predicates it calls, each body walked for the constant it compares
// against (one helper hop when the body delegates), each constant resolved to its
// word. Nothing in this function knows a verb name.
func censusDispatched(t *testing.T) (map[string]bool, map[string]string) {
	t.Helper()
	wordOf := censusVerbWordMap(t)

	names := censusChainNames(t)
	if len(names) < censusMinChainPredicates {
		t.Fatalf("main.go's dispatch chain mentions only %d predicates — the chain changed shape", len(names))
	}

	dispatched := map[string]bool{}
	constOf := map[string]string{}
	for name := range names {
		if censusIsRefusingPredicate(name) {
			// A REFUSING predicate is not a verb — it dispatches nothing. The
			// chain calls it to stop `gopi code_search` (a real command's
			// spelling this CLI deliberately does not dispatch) from being
			// spent as a prompt: it prints a pointed refusal and exits 1, so
			// there is no verb word for the table to carry and no help page to
			// fingerprint. It is exempted HERE, by name, and the exemption is
			// dead the moment the predicate is — the body must still exist in
			// the file that declares the exemption. (Any OTHER predicate that
			// names no constant is still fatal, below: the census would
			// otherwise silently shrink its own dispatched set.)
			if !censusPredicateExists(t, name) {
				t.Fatalf("exempted predicate is%sSubcommand no longer exists — the census exempts a refusal that is gone; "+
					"remove the exemption from cmd/agent/cli_verb_census_test.go (the constant lives in cmd/agent/cli_undispatched_tool_words.go)", name)
			}
			continue
		}
		constName, ok := censusPredicateConstant(t, name)
		if !ok {
			t.Fatalf("is%sSubcommand names no resolvable verb constant — the predicate shape changed", name)
		}
		w, ok := wordOf[constName]
		if !ok {
			t.Fatalf("constant %s (from is%sSubcommand) holds no verb word anywhere in the tree's constant sites", constName, name)
		}
		dispatched[w] = true
		constOf[w] = constName
	}
	return dispatched, constOf
}

// censusIsRefusingPredicate reports whether the chain entry `name` (the text
// between `is` and `Subcommand`) is the chain's REFUSING predicate — the one
// that dispatches no verb. The comparison is on the predicate's own name
// (`is<name>Subcommand`), because the chain regex hands back the part before
// the `Subcommand` suffix, not the whole spelling.
func censusIsRefusingPredicate(name string) bool {
	return name+"Subcommand" == undispatchedToolWordsSubcommand
}

// censusPredicateExists reports whether `func is<name>Subcommand(args []string) bool`
// is declared anywhere in cmd/agent — the check that keeps the refusing-predicate
// exemption (censusDispatched) tied to a predicate that is really there.
func censusPredicateExists(t *testing.T, name string) bool {
	t.Helper()
	files, err := filepath.Glob(censusRel(t, "cmd/agent/*.go"))
	if err != nil {
		t.Fatalf("glob cmd/agent: %v", err)
	}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if _, ok := censusFuncBody(string(src), "is"+name+"Subcommand"); ok {
			return true
		}
	}
	return false
}

// censusPredicateBodies returns the source of is<Name>Subcommand plus (when the
// body delegates) the helper it calls — the two places a predicate names the
// constant it dispatches to and the run function it dispatches into.
func censusPredicateBodies(t *testing.T, name string) []string {
	t.Helper()
	files, err := filepath.Glob(censusRel(t, "cmd/agent/*.go"))
	if err != nil {
		t.Fatalf("glob cmd/agent: %v", err)
	}
	helperRe := regexp.MustCompile(`\b([a-z][A-Za-z0-9]*Subcommand)\(args\)`)
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		body, ok := censusFuncBody(string(src), "is"+name+"Subcommand")
		if !ok {
			continue
		}
		out := []string{body}
		if m := helperRe.FindStringSubmatch(body); m != nil {
			if hbody, ok := censusFuncBody(string(src), m[1]); ok {
				out = append(out, hbody)
			}
		}
		return out
	}
	return nil
}

// censusRunFunctionDrives re-derives the run-function shape of test_drive: a
// test file that CALLS one of the verb's own run functions (runTermOnce(…),
// runUIStateSubcommand(…)) drives the verb for real — the newline is the only
// difference from an argv slice. Counted per FILE, matching the generator.
func censusRunFunctionDrives(t *testing.T, dispatched map[string]bool, chainNames map[string]bool, wordOf map[string]string) map[string]int {
	t.Helper()
	runRe := regexp.MustCompile(`\brun[A-Za-z0-9]+`)
	spans := censusTestSources(t)
	out := map[string]int{}
	for name := range chainNames {
		fns := map[string]bool{}
		for _, body := range censusPredicateBodies(t, name) {
			for _, fn := range runRe.FindAllString(body, -1) {
				fns[fn] = true
			}
		}
		if len(fns) == 0 {
			continue
		}
		constName, ok := censusPredicateConstant(t, name)
		if !ok {
			continue
		}
		verb := wordOf[constName]
		if !dispatched[verb] {
			continue
		}
		for fn := range fns {
			callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(fn) + `\(`)
			for _, span := range spans {
				if callRe.MatchString(span.src) {
					out[verb]++
				}
			}
		}
	}
	return out
}

// censusPredicateConstant finds the file declaring is<Name>Subcommand and returns
// the verb constant its body names — directly, or through one helper hop
// (isBrewSubcommand → brewSubcommand). The constant may carry a package
// qualifier (`toolnames.ToolWebSearch`), which is stripped.
func censusPredicateConstant(t *testing.T, name string) (string, bool) {
	t.Helper()
	files, err := filepath.Glob(censusRel(t, "cmd/agent/*.go"))
	if err != nil {
		t.Fatalf("glob cmd/agent: %v", err)
	}
	direct := regexp.MustCompile(`\b(CliVerb[A-Za-z0-9]+|toolnames\.Tool[A-Za-z0-9]+)\b`)
	helperRe := regexp.MustCompile(`\b([a-z][A-Za-z0-9]*Subcommand)\(args\)`)
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// The chain names the predicate is<Name>Subcommand; the function's own
		// name keeps that suffix, so the lookup re-attaches it here (the helper
		// regex below captures the suffix as part of the helper's name).
		body, ok := censusFuncBody(string(src), "is"+name+"Subcommand")
		if !ok {
			continue
		}
		if m := direct.FindStringSubmatch(body); m != nil {
			return strings.TrimPrefix(m[1], "toolnames."), true
		}
		if m := helperRe.FindStringSubmatch(body); m != nil {
			if hbody, ok := censusFuncBody(string(src), m[1]); ok {
				if h := direct.FindStringSubmatch(hbody); h != nil {
					return strings.TrimPrefix(h[1], "toolnames."), true
				}
			}
		}
		return "", false
	}
	return "", false
}

// censusFuncBody returns the source of `func <name>(args []string) bool { … }`,
// found by brace depth from the declaration (a range over the file is brittle
// against the comment shapes these predicates carry).
func censusFuncBody(src, name string) (string, bool) {
	head := "func " + name + "(args []string) bool {"
	i := strings.Index(src, head)
	if i < 0 {
		return "", false
	}
	depth := 0
	for j := i + len(head) - 1; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i : j+1], true
			}
		}
	}
	return "", false
}

// censusAdvertised runs the BUILT binary and reads its own command index. The
// binary comes from the ONE resolver (testutil.GopiBinOptional) — this probe
// used to spell `bin/gopi` against its own cwd, which in a shared checkout is
// whichever tree's build it happened to find. A missing binary is reported and
// skipped: the direction is unverified, which the caller must be able to see
// (never a silent pass).
func censusAdvertised(t *testing.T) (map[string]bool, bool) {
	t.Helper()
	bin, ok, why := testutil.GopiBinOptional(t)
	if !ok {
		t.Logf("census: %s — the advertised direction is UNVERIFIED this run", why)
		return nil, false
	}
	{
		out, err := exec.Command(bin, "-h").CombinedOutput()
		if err != nil {
			t.Logf("census: `%s -h` failed (%v) — the advertised direction is UNVERIFIED this run", bin, err)
			return nil, false
		}
		adv := map[string]bool{}
		line := regexp.MustCompile(`(?m)^ {2}gopi ([a-z][a-z0-9_-]*)(?: |$)`)
		for _, m := range line.FindAllStringSubmatch(string(out), -1) {
			adv[m[1]] = true
		}
		if len(adv) < censusMinRows {
			t.Logf("census: the binary advertised only %d verbs — treating the direction as UNVERIFIED", len(adv))
			return nil, false
		}
		return adv, true
	}
}

// censusParseTable reads the checked-in rows. A doc with no header, or a row that
// is not made of integers where integers are required, fails — a malformed table
// must not be readable as an empty one.
// censusDocPathAbs resolves the doc against the repo root — the test's cwd is the
// package dir, so a bare "devdoc/..." never resolves (and would read as "no
// census", the one failure this gate must never fabricate).
func censusDocPathAbs(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), censusDocPath)
}

// censusRel anchors a repo-relative path on the repo root: the test's cwd is the
// package dir (cmd/agent), so a bare `cmd/agent/main.go` or `devdoc/...` would
// resolve nowhere and read as "the corpus is missing" — the one failure this gate
// must never fabricate.
func censusRel(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), rel)
}

func censusParseTable(t *testing.T, doc string) []censusRow {
	t.Helper()
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == censusHeaderLine {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s carries no census table: the header line %q is absent", censusDocPath, censusHeaderLine)
	}
	var rows []censusRow
	for _, l := range lines[start:] {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "|") {
			break
		}
		cells := censusSplitRow(l)
		if censusSeparatorRow(cells) {
			continue
		}
		if len(cells) != 9 {
			t.Fatalf("%s: row %q has %d cells, want 9", censusDocPath, l, len(cells))
		}
		r := censusRow{verb: cells[0], class: cells[8]}
		for i, dst := range []*int{&r.dispatched, &r.advertised, &r.testDrive, &r.runnerLine} {
			n, err := strconv.Atoi(cells[i+1])
			if err != nil {
				t.Fatalf("%s: row %q: column %d (%q) is not an integer: %v", censusDocPath, l, i+1, cells[i+1], err)
			}
			*dst = n
		}
		n, err := strconv.Atoi(cells[5])
		if err != nil {
			t.Fatalf("%s: row %q: usage_log (%q) is not an integer: %v", censusDocPath, l, cells[5], err)
		}
		m, err := strconv.Atoi(cells[6])
		if err != nil {
			t.Fatalf("%s: row %q: doc_recipe (%q) is not an integer: %v", censusDocPath, l, cells[6], err)
		}
		if n < 0 || m < 0 {
			t.Fatalf("%s: row %q: negative corpus count", censusDocPath, l)
		}
		if hl, err := strconv.Atoi(cells[7]); err != nil || (hl != 0 && hl != 1) {
			t.Fatalf("%s: row %q: help_only (%q) must be 0 or 1", censusDocPath, l, cells[7])
		}
		rows = append(rows, r)
	}
	if len(rows) < censusMinRows {
		t.Fatalf("%s: the census carries %d rows — below the non-vacuity floor of %d. "+
			"A census this small is a broken table, not a small surface (regenerate with scripts/g7-verb-census.sh).",
			censusDocPath, len(rows), censusMinRows)
	}
	return rows
}

// censusSeparatorRow reports the markdown table's `|---|---|` rule line, which is
// part of the table's shape and carries no row.
func censusSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

func censusSplitRow(l string) []string {
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")
	parts := strings.Split(l, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(strings.ReplaceAll(p, "`", "")))
	}
	return out
}

// censusArgvLiteralDrives re-derives the `test_drive` column: an argv literal
// with the verb FIRST — `[]string{"worker", …}`, a `[][]string{{CliVerbMerger, …}}`
// table row, `{[]string{CliVerbBrew, …}}`, `{toolnames.ToolScreenCapture, …}` —
// on a non-help line. This is the strong evidence: a caller hands this argv to a
// runner. The pattern is the whole-line match, not a scan from the first `{`:
// the argv's own `[]string` follows that brace, and a from-the-brace scan misses
// every literal of the ordinary shape.
func censusArgvLiteralDrives(t *testing.T, verbs map[string]bool, constOf map[string]string) map[string]int {
	t.Helper()
	drives := map[string]int{}
	spans := censusTestSources(t)
	for v := range verbs {
		alts := []string{regexp.QuoteMeta(`"` + v + `"`)}
		if c := constOf[v]; c != "" {
			alts = append(alts, regexp.QuoteMeta(c))
		}
		re := regexp.MustCompile(`\{\{?[[:space:]]*([a-z][A-Za-z0-9]*\.)?(` + strings.Join(alts, "|") + `)[[:space:]]*,`)
		for _, span := range spans {
			for _, line := range strings.Split(span.src, "\n") {
				if strings.Contains(line, `-h"`) || strings.Contains(line, "--help") {
					continue
				}
				if re.MatchString(line) {
					drives[v]++
				}
			}
		}
	}
	return drives
}

// censusRunnerLineDrives re-derives the `runner_line` column: the verb spelled on
// a line that calls one of the discovered variadic RUNNER helpers. Deliberately
// weak evidence — the argv reaches the binary, but the slice is only implied.
func censusRunnerLineDrives(t *testing.T, verbs map[string]bool) map[string]int {
	t.Helper()
	runnerRe := regexp.MustCompile(`func (?:\([a-z] \*[A-Za-z0-9]+\) )?([A-Za-z0-9]+)\(t \*testing\.T[^)]*\.\.\.string\)`)
	names := map[string]bool{}
	spans := censusTestSources(t)
	for _, span := range spans {
		for _, m := range runnerRe.FindAllStringSubmatch(span.src, -1) {
			if m[1] != "run" {
				names[m[1]] = true
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("no variadic test runner discovered — the runner shape changed and runner_line cannot be re-derived")
	}
	callRe := regexp.MustCompile(`\b(` + strings.Join(censusSortedKeys(names), "|") + `)\(`)
	out := map[string]int{}
	for _, span := range spans {
		for _, line := range strings.Split(span.src, "\n") {
			if !callRe.MatchString(line) {
				continue
			}
			for v := range verbs {
				if strings.Contains(line, `"`+v+`"`) {
					out[v]++
				}
			}
		}
	}
	return out
}

func censusSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type censusSource struct {
	path string
	src  string
}

// censusTestSources reads every test source the generator scans, so both read the
// same corpus. A read failure is fatal: a silently shorter corpus would make the
// gate agree with a stale table for the wrong reason.
func censusTestSources(t *testing.T) []censusSource {
	t.Helper()
	var out []censusSource
	for _, root := range []string{"cmd", "testsuite"} {
		err := filepath.Walk(censusRel(t, root), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			// This gate's own file is not evidence about the verbs: its lines are a
			// claim about usage, exactly like the census doc it reads.
			if filepath.Base(path) == "cli_verb_census_test.go" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out = append(out, censusSource{path: path, src: string(b)})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(out) < 50 {
		t.Fatalf("read only %d test sources — the corpus is not what the generator reads", len(out))
	}
	return out
}

// TestVerbCensusMatchesTheDispatch is the gate.
func TestVerbCensusMatchesTheDispatch(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	doc, err := os.ReadFile(censusDocPathAbs(t))
	if err != nil {
		t.Fatalf("read %s: %v — the census is the deliverable; without it the surface is unmeasured", censusDocPath, err)
	}
	rows := censusParseTable(t, string(doc))
	dispatched, constOf := censusDispatched(t)
	advertised, advertisedVerified := censusAdvertised(t)

	// direction 1: every dispatched verb has exactly one row claiming it.
	byVerb := map[string][]censusRow{}
	for _, r := range rows {
		byVerb[r.verb] = append(byVerb[r.verb], r)
	}
	for v := range dispatched {
		got, ok := byVerb[v]
		if !ok {
			t.Errorf("`gopi %s` is dispatched by cmd/agent/main.go but has NO row in %s — "+
				"the census is stale (regenerate: bash scripts/g7-verb-census.sh)", v, censusDocPath)
			continue
		}
		if len(got) > 1 {
			t.Errorf("`gopi %s` has %d rows in %s — a verb is a row, not a section", v, len(got), censusDocPath)
			continue
		}
		if got[0].dispatched != 1 {
			t.Errorf("`gopi %s` is dispatched, but its row says dispatched=%d", v, got[0].dispatched)
		}
	}

	// direction 2: every row is corroborated — a verb nobody dispatches and the
	// binary does not advertise is either a renamed verb or a doc that invented one.
	for _, r := range rows {
		if r.dispatched != 0 && r.dispatched != 1 {
			t.Errorf("row %q: dispatched=%d, must be 0 or 1", r.verb, r.dispatched)
		}
		if r.dispatched == 1 && !dispatched[r.verb] {
			t.Errorf("row %q claims dispatched=1, but cmd/agent/main.go's chain dispatches no such verb", r.verb)
		}
		if r.dispatched == 0 && r.advertised == 0 && advertisedVerified && !advertised[r.verb] {
			t.Errorf("row %q claims a verb that is neither dispatched nor advertised by the binary", r.verb)
		}
		if r.dispatched == 1 && advertisedVerified && r.advertised != 0 && !advertised[r.verb] {
			t.Errorf("row %q claims advertised=1, but the built binary's `gopi -h` does not list it", r.verb)
		}
		if r.dispatched == 1 && advertisedVerified && r.advertised == 0 && advertised[r.verb] {
			t.Errorf("row %q claims advertised=0, but the built binary's `gopi -h` DOES list it", r.verb)
		}
	}

	// direction 3: the exercised columns agree with the corpora, so a row cannot
	// buy a class it did not earn.
	literalDrives := censusArgvLiteralDrives(t, dispatched, constOf)
	runnerDrives := censusRunnerLineDrives(t, dispatched)
	runFnDrives := censusRunFunctionDrives(t, dispatched, censusChainNames(t), censusVerbWordMap(t))
	for _, r := range rows {
		if r.class == censusClassTest && r.testDrive == 0 {
			t.Errorf("row %q is classed %s with test_drive=0 — the class must be earned", r.verb, censusClassTest)
		}
		if r.testDrive > 0 && r.class == "c:neither" {
			t.Errorf("row %q has test_drive=%d yet is classed c:neither", r.verb, r.testDrive)
		}
		if r.runnerLine > 0 && r.class == "c:neither" {
			t.Errorf("row %q has runner_line=%d yet is classed c:neither", r.verb, r.runnerLine)
		}
		// a verb with live literal drives MUST be a:test — the strongest direction:
		// this is the check that catches "the census says nobody tests it" after
		// somebody added the test.
		if n := literalDrives[r.verb]; n > 0 && r.class != censusClassTest {
			t.Errorf("row %q is classed %q, but %d argv literal(s) in the test corpus hand it a real argv — "+
				"a row may not understate its own coverage", r.verb, r.class, n)
		}
		// and the reverse: an a:test claim must have SOME drive in the corpus the
		// gate can see (a literal or a runner line), never nothing at all.
		if r.class == censusClassTest && r.testDrive > 0 && literalDrives[r.verb] == 0 &&
			runnerDrives[r.verb] == 0 && runFnDrives[r.verb] == 0 {
			t.Errorf("row %q is classed a:test but neither an argv literal nor a runner line in the corpus drives it",
				r.verb)
		}
	}

	// non-vacuity: the corpora themselves must be non-empty, else the checks above
	// pass by having nothing to look at.
	if len(literalDrives) < 10 {
		t.Fatalf("the argv-literal corpus yielded only %d verbs — the corpus is broken, not the table", len(literalDrives))
	}
	if !advertisedVerified {
		// recorded, not hidden: this run could not check the advertised direction.
		t.Logf("census: %d rows verified against the dispatch chain; the ADVERTISED direction was UNVERIFIED this run", len(rows))
	}
	t.Logf("census: %d dispatched verbs, %d rows, %d with literal argv drives", len(dispatched), len(rows), len(literalDrives))
}
