package standardgate

// The gate on EXPORTED SYMBOLS declared in `_test.go` files (PROJECT.md Rule 43).
//
// WHY IT EXISTS — the measured incident. On 2026-10-31 (task 01a18f76) four
// production symbols in internal/tools were found to have ZERO production
// definitions and ZERO production call sites, while a `_test.go` file declared
// all four:
//
//	FinishCleanupReportLine / FinishCleanupReportRemovedWorktree /
//	FinishCleanupReportSettled / TaskFinishCleanupSessionID
//
// Their doc comments still said "exported so the CLI sweep shares the agent
// hook's renderer shape" — a caller that had ceased to exist when the
// finish-cleanup mechanism was retired (58b5618b, slice 3/4). The mechanism's
// disappearance had been made INVISIBLE one commit earlier: 0e9fdbca "restore
// the fixtures" gave 43 undefined symbols a same-named definition in a `_test.go`
// file, so `go build ./...` — the instrument that points at every surviving
// reference (Rule 43's own check) — had nothing left to point at. An exported
// spelling in a test file is the shape that swallows that signal: an EXPORT is a
// CLAIM THAT SOMETHING OUTSIDE THIS PACKAGE CONSUMES IT, and when the claimant is
// a test file, the claim is not checked by anything.
//
// THE PREDICATE. A top-level declaration in a `*_test.go` file whose name is
// EXPORTED (and is not a test entry point — Test/Benchmark/Example/Fuzz) must be
// REFERENCED BY NAME FROM ANOTHER FILE of the same directory and the same
// package clause, COUNTING CODE ONLY (comments are stripped by scanning tokens
// with comment reporting OFF). Two shapes it refuses:
//
//	· a `_test.go`-defined exported symbol NO OTHER FILE names — the vacuous
//	  export, and the exact shape a retirement leaves behind when a moved body is
//	  restored as a same-named test fixture;
//	· a `_test.go`-defined exported symbol whose only `name` occurrences
//	  elsewhere are prose — a COMMENT is not a reader (Rule 43's homophone trap:
//	  `git grep -c` counts comment lines, and a comment claiming a caller is
//	  precisely what this gate is built to stop trusting).
//
// WHAT IT DOES NOT JUDGE, said plainly rather than over-claimed (Rule 32's
// honesty clause). It does not decide whether a symbol that DOES have a
// same-directory reader is exported for a good reason: `C` in
// internal/lineedit/palette256_test.go is a one-letter constructor used by 29
// files, and `SandboxRepoContext` is a fixture helper 42 files share. Those are
// legal today and this gate keeps them legal. What it refuses is the export whose
// reader is absent or is a sentence. It also does not read a symbol reached
// through reflection or a string-keyed lookup — no site in this tree does that
// for a test-only helper, and pretending to detect it would be a wider predicate
// with a false-positive count that gets the gate switched off (gate.go's own
// recorded argument).
//
// THE SAME PACKAGE CLAUSE, not merely the same directory: two test files in one
// directory can be `package tools` and `package tools_test`, and a name in the
// second is not reachable from the first. Comparing the clause is what keeps the
// reader test from passing on a neighbour that could never call the symbol.
//
// THE ESCAPE and the BASELINE, both ratcheting DOWN (the house shape — see
// repo_hygiene_gate.go). A declaration that must be exported from a test file
// for a reason this predicate cannot see carries
// `// exported-test-symbol-ok: <reason>` on the line directly above it; a
// violation that predates the gate lives in
// testdata/exported-test-symbol-baseline.tsv as `<path>\t<symbol>\t<reason>`.
// A baseline row that no longer fires is an ERROR, so the allow-list cannot
// ratchet up by neglect, and an escape whose declaration is gone is reported for
// the same reason.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// exportedTestSymbolSuffix is the file kind the gate speaks about (Rule 1b:
	// the value is a constant, compared by name at every site).
	exportedTestSymbolSuffix = "_test.go"
	// exportedTestSymbolGoSuffix is the file kind whose identifiers count as a
	// READER. A production file in the same directory counts too — a test-only
	// exported symbol read by production code is the strongest reader there is.
	exportedTestSymbolGoSuffix = ".go"
	// exportedTestSymbolEscape is the documented inline escape. It rides the line
	// DIRECTLY ABOVE the declaration, so the reason cannot drift onto a different
	// symbol after an edit.
	exportedTestSymbolEscape = "exported-test-symbol-ok:"
	// exportedTestSymbolTestPrefixes are the test entry points: exported by the
	// `go test` contract itself, so they are not exports at all.
	exportedTestSymbolTestPrefixTest      = "Test"
	exportedTestSymbolTestPrefixBenchmark = "Benchmark"
	exportedTestSymbolTestPrefixExample   = "Example"
	exportedTestSymbolTestPrefixFuzz      = "Fuzz"
	// exportedTestSymbolFixtureDir is skipped: fixtures are parsed by gates, not
	// compiled, so a symbol there has no reader in anything this gate can see.
	exportedTestSymbolFixtureDir = "testdata"
	// exportedTestSymbolScanFloor is the non-vacuity floor over the tree: a scan
	// that reached fewer test files than this proves nothing (Rule 36's lesson —
	// a named check that judged nothing must not read green).
	exportedTestSymbolScanFloor = 800
)

// exportedTestSymbolFinding is one refused declaration.
type exportedTestSymbolFinding struct {
	File   string // repo-relative, slash-separated
	Symbol string
	Kind   string // "func" or "type"
	Line   int
	// ProseOnly names the files that MENTION the symbol in comments and nowhere
	// else. Empty when nothing outside the declaring file mentions it at all —
	// the two shapes are different evidence and are reported differently.
	ProseOnly []string
}

// exportedTestSymbolScan is the predicate over a TREE. It takes its root as an
// argument (never a hard-coded repo path) so the red proof can run the SAME
// predicate over a synthetic tree it builds, which is the only way a gate's
// green and its red are known to be the same predicate.
//
// It returns the findings plus the two counts that make a green non-vacuous: the
// number of test files walked and the number of exported declarations judged.
func exportedTestSymbolScan(root string) (walked, judged int, findings []exportedTestSymbolFinding, err error) {
	// Directory (package-directory) -> "pkg\x00ident" set, and per file the set of
	// identifiers it uses IN CODE (comments stripped).
	type pkgIdent struct {
		file string
		pkg  string
		ids  map[string]bool
	}
	var byDir = map[string][]*pkgIdent{}
	var testFiles []string

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", exportedTestSymbolFixtureDir:
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), exportedTestSymbolGoSuffix) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		pkg, ids, perr := exportedTestSymbolFileIdents(p)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", rel, perr)
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		byDir[dir] = append(byDir[dir], &pkgIdent{file: rel, pkg: pkg, ids: ids})
		if strings.HasSuffix(d.Name(), exportedTestSymbolSuffix) {
			testFiles = append(testFiles, rel)
		}
		return nil
	})
	if walkErr != nil {
		return 0, 0, nil, walkErr
	}

	sort.Strings(testFiles)
	walked = len(testFiles)
	for _, rel := range testFiles {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		decls, derr := exportedTestSymbolDecls(abs, rel)
		if derr != nil {
			return walked, judged, nil, fmt.Errorf("parse %s: %w", rel, derr)
		}
		judged += len(decls)
		dir := filepath.ToSlash(filepath.Dir(rel))
		for _, d := range decls {
			var proseOnly []string
			hasReader := false
			for _, other := range byDir[dir] {
				if other.file == d.File || other.pkg != d.Pkg {
					continue
				}
				if other.ids[d.Symbol] {
					hasReader = true
					break
				}
			}
			if hasReader {
				continue
			}
			// No code reader in the same package: say WHICH files mention it in
			// prose only, because "nothing names it" and "only a comment names
			// it" are different findings (Rule 43's homophone trap).
			for _, other := range byDir[dir] {
				if other.file == d.File || other.pkg != d.Pkg {
					continue
				}
				if exportedTestSymbolMentionsInProse(filepath.Join(root, filepath.FromSlash(other.file)), d.Symbol) {
					proseOnly = append(proseOnly, other.file)
				}
			}
			sort.Strings(proseOnly)
			findings = append(findings, exportedTestSymbolFinding{
				File: d.File, Symbol: d.Symbol, Kind: d.Kind, Line: d.Line, ProseOnly: proseOnly,
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Symbol < findings[j].Symbol
	})
	return walked, judged, findings, nil
}

// exportedTestSymbolDecl is one exported top-level declaration of a test file.
type exportedTestSymbolDecl struct {
	File   string
	Pkg    string
	Symbol string
	Kind   string
	Line   int
}

// exportedTestSymbolDecls parses one file and returns its exported top-level
// declarations, minus the test entry points and minus anything carrying the
// inline escape.
func exportedTestSymbolDecls(absPath, relPath string) ([]exportedTestSymbolDecl, error) {
	fset := token.NewFileSet()
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	f, err := parser.ParseFile(fset, absPath, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	pkg := f.Name.Name
	var out []exportedTestSymbolDecl
	add := func(name, kind string, pos token.Pos, doc *ast.CommentGroup) {
		if !ast.IsExported(name) {
			return
		}
		for _, p := range []string{
			exportedTestSymbolTestPrefixTest,
			exportedTestSymbolTestPrefixBenchmark,
			exportedTestSymbolTestPrefixExample,
			exportedTestSymbolTestPrefixFuzz,
		} {
			if strings.HasPrefix(name, p) {
				return
			}
		}
		if exportedTestSymbolEscaped(fset, pos, doc) {
			return
		}
		out = append(out, exportedTestSymbolDecl{
			File: relPath, Pkg: pkg, Symbol: name, Kind: kind, Line: fset.Position(pos).Line,
		})
	}
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv != nil { // a method is reached through its type
				continue
			}
			add(x.Name.Name, "func", x.Pos(), x.Doc)
		case *ast.GenDecl:
			if x.Tok != token.TYPE { // const/var blocks are values, not exports
				continue
			}
			for _, spec := range x.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				add(ts.Name.Name, "type", ts.Pos(), x.Doc)
			}
		}
	}
	return out, nil
}

// exportedTestSymbolEscaped reports whether the declaration at pos carries the
// escape on the line directly above it.
func exportedTestSymbolEscaped(fset *token.FileSet, pos token.Pos, doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	declLine := fset.Position(pos).Line
	for _, c := range doc.List {
		if !strings.Contains(c.Text, exportedTestSymbolEscape) {
			continue
		}
		if fset.Position(c.End()).Line == declLine-1 {
			return true
		}
	}
	return false
}

// exportedTestSymbolFileIdents returns the file's package clause and the set of
// identifiers it uses IN CODE. Comments are dropped by scanning with comment
// reporting OFF — this is what makes a prose mention stop counting as a reader.
func exportedTestSymbolFileIdents(absPath string) (string, map[string]bool, error) {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return "", nil, err
	}
	// The package clause: parsed, because a token scan cannot tell which of
	// several identifiers after `package` is the name.
	fsetPkg := token.NewFileSet()
	f, err := parser.ParseFile(fsetPkg, absPath, src, parser.PackageClauseOnly)
	if err != nil {
		return "", nil, err
	}
	fset := token.NewFileSet()
	file := fset.AddFile(absPath, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, 0)
	ids := map[string]bool{}
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.IDENT {
			ids[lit] = true
		}
	}
	return f.Name.Name, ids, nil
}

// exportedTestSymbolMentionsInProse reports whether the symbol occurs as a WHOLE
// WORD in a COMMENT of the file (used only to explain a finding, never to clear
// one). The boundary matters: without it a one-letter helper like `C` "matches"
// every sentence containing the letter, and a finding's explanation must be a
// reading, not a coincidence.
func exportedTestSymbolMentionsInProse(absPath, symbol string) bool {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(symbol) + `\b`)
	fset := token.NewFileSet()
	file := fset.AddFile(absPath, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return false
		}
		if tok == token.COMMENT && re.MatchString(lit) {
			return true
		}
	}
}

// exportedTestSymbolBaselineRow is one frozen violation.
type exportedTestSymbolBaselineRow struct {
	File   string
	Symbol string
	Reason string
}

// exportedTestSymbolBaseline parses the frozen table. A malformed row is an
// error, never a silent skip (Rule 15 — a permit the parser dropped would look
// exactly like a permit nobody needed).
func exportedTestSymbolBaseline(path string) ([]exportedTestSymbolBaselineRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []exportedTestSymbolBaselineRow
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 || strings.TrimSpace(cols[0]) == "" || strings.TrimSpace(cols[1]) == "" || strings.TrimSpace(cols[2]) == "" {
			return nil, fmt.Errorf("%s:%d: a baseline row is `path<TAB>symbol<TAB>reason` with all three non-empty: %q", path, i+1, line)
		}
		out = append(out, exportedTestSymbolBaselineRow{
			File:   filepath.ToSlash(strings.TrimSpace(cols[0])),
			Symbol: strings.TrimSpace(cols[1]),
			Reason: strings.TrimSpace(cols[2]),
		})
	}
	return out, nil
}

// exportedTestSymbolJudge applies the baseline to a finding set and returns the
// errors a caller should report: an unexplained finding, and a baseline row that
// no longer fires (the ratchet).
func exportedTestSymbolJudge(findings []exportedTestSymbolFinding, rows []exportedTestSymbolBaselineRow) (unexplained []exportedTestSymbolFinding, stale []exportedTestSymbolBaselineRow) {
	permit := map[string]string{}
	for _, r := range rows {
		permit[r.File+"\x00"+r.Symbol] = r.Reason
	}
	seen := map[string]bool{}
	for _, f := range findings {
		key := f.File + "\x00" + f.Symbol
		if _, ok := permit[key]; ok {
			seen[key] = true
			continue
		}
		unexplained = append(unexplained, f)
	}
	for _, r := range rows {
		if !seen[r.File+"\x00"+r.Symbol] {
			stale = append(stale, r)
		}
	}
	return unexplained, stale
}
