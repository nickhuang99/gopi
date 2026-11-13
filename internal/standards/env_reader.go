// gate 6 — every env-name constant must have a REAL reader (Rule 21).
//
// WHY THIS GATE EXISTS (the defect it makes unrepresentable): the scratch GC
// shipped a documented knob, GOPI_TEST_SCRATCH_GC_MIN ("makes the age GC run
// every N minutes"), whose env-name constant was declared and NEVER READ — not
// in production, not in a test. A hook with no consumer is worse than a missing
// feature: it reads as a working control surface (help text, devdoc and the
// constant all claim it works), so an operator who sets it concludes the sweep
// is running while nothing consumes the value at all.
//
// The predicate is structural and deterministic (the bar Rules 16-20 set — no
// judgement call, stdlib only):
//
//  1. A const DECLARES an env name when its right-hand side is a single string
//     literal shaped like one — an all-caps identifier in a known env namespace
//     (GOPI_/ANTHROPIC_/DEEPSEEK_) or the standard TMPDIR. An inline
//     os.Getenv("GOPI_X") is a bare literal (Rule 1b) and is not this gate's
//     business.
//  2. It has a READER when its identifier occurs INSIDE the argument list of an
//     environment call — a call whose callee name carries "env"
//     (os.Getenv / os.LookupEnv / os.Setenv / t.Setenv / config.EnvBool /
//     boolEnvTrue / envInt / envStr / envDur / ...). The paren depth is tracked,
//     so a multi-line call is exact and a FOLLOWING statement's identifiers are
//     never claimed (`os.Getenv(X)` counts, `set(X, "0")` structurally cannot —
//     an outbound push is not a reader).
//
// An env constant whose reader is intentionally elsewhere carries
// envReaderEscape in its doc comment — the reviewed, reasoned exemption list,
// which stays visible in TestStandardGateDataReport instead of disappearing
// into a baseline. The baseline is EMPTY by construction, so a new unread hook
// fails on the commit that introduces it.
package standardgate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// scratchAliasFile / scratchAliasConst / scratchAliasTarget are the alias case
// the exhaustive test pins: internal/testutil spells the resident sweep's
// cadence knob through ONE definition (scratch.GCIntervalEnvName, Rule 14) —
// the alias's own name is read by nothing, and that is correct. They are
// constants here (Rule 1b) so the test and the gate's docs name one spelling.
const (
	scratchAliasFile   = "internal/testutil/scratchroot.go"
	scratchAliasConst  = "ScratchGCIntervalEnvName"
	scratchAliasTarget = "GCIntervalEnvName"
)

// envNamePattern is the shape of a string that names an environment variable:
// all-caps words joined by underscores inside a known env namespace (or the
// standard TMPDIR). Anchored, so a message or a path can never match.
var envNamePattern = regexp.MustCompile(`^(GOPI|ANTHROPIC|DEEPSEEK)_[A-Z0-9_]+$|^TMPDIR$`)

// envReaderEscape marks a const whose reader is deliberately NOT in the
// production tree (the reviewed exemption list).
const envReaderEscape = "// env-reader-exempt:"

// envCalleeMarker is the substring that makes a CALL an environment call: the
// knob funnels all carry it (os.Getenv / os.LookupEnv / os.Setenv / t.Setenv /
// config.EnvBool / boolEnvTrue / envInt / envStr / envDur / envBoolValue ...).
// Matching the callee by name — rather than against an exhaustive helper list —
// is what keeps a NEW helper from silently escaping the gate.
const envCalleeMarker = "env"

var (
	blockCommentOpen = "/*"
	blockCommentEnd  = "*/"
	lineCommentStart = "//"
)

// envConst is one declared env-name constant.
type envConst struct {
	name   string
	value  string
	target string
	line   int
}

// constSpec is one package-level const with a single name and value, reduced to
// what env-name classification needs.
type constSpec struct {
	name   string
	value  string // RHS string literal ("" when the RHS is not a literal)
	refers string // RHS identifier or selector's Sel name ("" when not a name)
	line   int
}

// constSpecs lists the single-name/single-value consts of a parsed file. A spec
// with several names or several values is skipped: nothing there is one name
// mapped to one value.
func constSpecs(p *parsed) []constSpec {
	var out []constSpec
	for _, decl := range p.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "const" {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			cs := constSpec{name: vs.Names[0].Name, line: p.fset.Position(vs.Names[0].Pos()).Line}
			switch v := vs.Values[0].(type) {
			case *ast.BasicLit:
				if v.Kind.String() != "STRING" {
					continue
				}
				cs.value = strings.Trim(v.Value, `"`)
			case *ast.SelectorExpr:
				cs.refers = v.Sel.Name
			case *ast.Ident:
				cs.refers = v.Name
			default:
				continue
			}
			out = append(out, cs)
		}
	}
	return out
}

// envNameConsts lists, for one file, the consts that DECLARE or alias an env
// name:
//
//   - a RHS string literal shaped like an env name (the declaration site), and
//   - a RHS that names another const which IS an env name anywhere in the
//     production tree (`X = scratch.GCIntervalEnvName`, `X = Y`) — Rule 14's
//     aliasing pattern, where one knob is spelled in two packages and read in
//     neither's own file, so the gate must follow the alias instead of
//     demanding a duplicate read.
//
// The "is an env name" test consults envNameSet(), which resolves the alias
// chain to a fixpoint across the tree — a qualified alias of a non-env const
// (`X = time.RFC3339`, `X = scratch.DirPerm`) is an ordinary constant and stays
// out of the gate's scope.
func envNameConsts(p *parsed) []envConst {
	envNames := envNameSet()
	var out []envConst
	for _, cs := range constSpecs(p) {
		switch {
		case cs.value != "":
			if !envNamePattern.MatchString(cs.value) {
				continue // a const holding a non-env string is not a hook
			}
			out = append(out, envConst{name: cs.name, value: cs.value, line: cs.line})
		case cs.refers != "" && envNames[cs.refers]:
			out = append(out, envConst{name: cs.name, target: cs.refers, line: cs.line})
		}
	}
	return out
}

// aliasesOf maps each alias constant declared in p to its target identifier.
func aliasesOf(p *parsed) map[string]string {
	out := map[string]string{}
	for _, c := range envNameConsts(p) {
		if c.target != "" {
			out[c.name] = c.target
		}
	}
	return out
}

// envNameSetOnce / envNameSetVal hold the tree-wide env-name vocabulary.
var (
	envNameSetOnce sync.Once
	envNameSetVal  map[string]bool
)

// envNameSet returns every identifier that IS an env name in the production
// tree: the consts whose RHS is an env-shaped string literal, plus the consts
// that alias one (transitively), resolved to a fixpoint so a two-hop alias
// (`a = pkg.B`, `b = pkg2.a`) is recognised as well as a direct one.
func envNameSet() map[string]bool {
	envNameSetOnce.Do(func() {
		envNameSetVal = map[string]bool{}
		root := repoRootForScan()
		if root == "" {
			return
		}
		var all []constSpec
		for _, rel := range goFilesUnderDir(root, prodRoots, prodOnly) {
			fset, f, err := parseFileForScan(root, rel)
			if err != nil {
				continue
			}
			all = append(all, constSpecs(&parsed{rel: rel, fset: fset, file: f})...)
		}
		for _, cs := range all {
			if cs.value != "" && envNamePattern.MatchString(cs.value) {
				envNameSetVal[cs.name] = true
			}
		}
		// Fixpoint over alias hops; bounded so a cyclic alias chain terminates.
		for hop := 0; hop < maxAliasDepth; hop++ {
			added := false
			for _, cs := range all {
				if cs.refers == "" || envNameSetVal[cs.name] {
					continue
				}
				if envNameSetVal[cs.refers] {
					envNameSetVal[cs.name] = true
					added = true
				}
			}
			if !added {
				break
			}
		}
	})
	return envNameSetVal
}

// parseFileForScan parses one repo-relative production file into an AST (the
// vocabulary needs the AST, not the text form).
func parseFileForScan(root, rel string) (*token.FileSet, *ast.File, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, rel, b, parser.ParseComments)
	if perr != nil {
		return nil, nil, perr
	}
	return fset, f, nil
}

// resolvedByReader reports whether a constant is read, directly or through a
// chain of aliases (bounded: an alias cycle is a code bug, not a reason to
// hang the gate).
func resolvedByReader(name string, index *readerIndex, local map[string]bool, aliases map[string]string, depth int) bool {
	if depth > maxAliasDepth {
		return false
	}
	if len(index.sites(name)) > 0 || local[name] {
		return true
	}
	target, ok := aliases[name]
	if !ok {
		return false
	}
	return resolvedByReader(target, index, local, aliases, depth+1)
}

// maxAliasDepth bounds alias resolution (an aliasing chain is a handful of
// hops; a cycle must not become a hang).
const maxAliasDepth = 8

// envNameFind reports every env-name constant in p that no environment call
// consumes. Two sources are consulted: the tree-wide reader index (built once
// per test binary) and the scanned file itself — the red-proof tests scan a
// SYNTHETIC file that is not on disk, and a reader in the scanned text is a
// reader wherever the scan was pointed.
func envNameFind(g gate, p *parsed) []hit {
	consts := envNameConsts(p)
	if len(consts) == 0 {
		return nil
	}
	index := envReaders()
	local := collectReaders(commentStrippedLines(string(p.src)))
	aliases := aliasesOf(p)
	var hits []hit
	for _, c := range consts {
		if index.consumedValue(c.value) ||
			resolvedByReader(c.name, index, local, aliases, 1) {
			continue
		}
		what := c.name + " (" + c.value + ")"
		if c.target != "" {
			what = c.name + " → " + c.target + " (alias of an env name)"
		}
		hits = append(hits, hit{
			rel:  p.rel,
			line: c.line,
			desc: "env constant " + what + " has no environment reader — " +
				"a hook with no consumer reads as a working control surface; wire it or delete it",
		})
	}
	return hits
}

// readerIndex is the tree-wide consumption index:
//
//   - byID: constant identifier → files that hand it to a call (the direct
//     reader), and
//   - valueRead: env NAME literal → whether SOME constant carrying that literal
//     is handed to a call. A knob has two faces when a parent PUSHES it and a
//     child reads it (the merger's outbound pair vs. autoTaskRegisterEnv's
//     os.Getenv): the same env name under two spellings is ONE consumed knob,
//     and reporting the writer's face as a hook with no consumer would be
//     exactly the false positive the gate must not produce.
type readerIndex struct {
	once      sync.Once
	byID      map[string][]string
	valueRead map[string]bool
	ready     bool
}

// envReaderOnce guards the one tree-wide scan per test binary.
var envReaderOnce readerIndex

// sites returns the reader files recorded for an identifier.
func (r *readerIndex) sites(name string) []string {
	if !r.ready {
		return nil
	}
	return r.byID[name]
}

// consumedValue reports whether any constant carrying this env-name literal is
// handed to a call (see readerIndex.valueRead).
func (r *readerIndex) consumedValue(value string) bool {
	if !r.ready || value == "" {
		return false
	}
	return r.valueRead[value]
}

// envReaders builds (once) the identifier→reader-sites index over every
// production Go file in the tree. Site discovery is textual ON PURPOSE: the
// question is "does this value reach an environment call anywhere", and the
// call may live in the package that spells the constant qualified
// (os.Getenv(tools.BrewIntervalEnv)) as well as in the constant's own file.
func envReaders() *readerIndex {
	envReaderOnce.once.Do(func() {
		envReaderOnce.byID = map[string][]string{}
		envReaderOnce.valueRead = map[string]bool{}
		envReaderOnce.ready = true
		root := repoRootForScan()
		if root == "" {
			return
		}
		// Readers are looked for in PRODUCTION AND TEST files: a test is a real
		// consumer (mrsResolverEnvOutEnv is a TEST-ONLY spawn contract, read by
		// t.Setenv in the E2E test and by the stub script it drives), and a
		// hook read only by a test is still a hook that does something. The
		// defect this gate exists for — a constant referenced NOWHERE but its
		// own declaration and a help table — is caught either way.
		for _, rel := range goFilesUnderDir(root, prodRoots, includeTests) {
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			read := collectReaders(commentStrippedLines(string(src)))
			for name := range read {
				envReaderOnce.byID[name] = append(envReaderOnce.byID[name], rel)
			}
			// Map each declared env name to its literal, so the two faces of
			// one knob share the verdict.
			fset, f, perr := parseFileForScan(root, rel)
			if perr != nil {
				continue
			}
			for _, cs := range constSpecs(&parsed{rel: rel, fset: fset, file: f}) {
				if cs.value != "" && read[cs.name] {
					envReaderOnce.valueRead[cs.value] = true
				}
			}
		}
	})
	return &envReaderOnce
}

// collectReaders returns every identifier that occurs INSIDE a call's argument
// list — the structural definition of "this constant is handed to code".
//
// WHY "ANY call" and not only env-named calls: the tree's knob funnels take the
// env NAME as a PARAMETER (config.resolveGraceInt("stall", …, TaskStallSecEnv)
// does os.Getenv(envName) inside), so a name-only rule reports those knobs as
// unread — a false positive, and false positives are what get a gate disabled.
// The price is that an identifier passed to an unrelated helper counts as read;
// the target defect (a hook referenced NOWHERE — no call, no argument, only its
// own declaration and a help table) is still caught, which is the case that
// motivated the gate and the case that cannot be argued away.
//
// The scan tracks PAREN DEPTH (not a fixed line lookback), so a multi-line call
// is handled exactly and a following statement's identifiers are never claimed:
//
//	os.Getenv(\n    scratch.GCIntervalEnvName)   ← one arg, one reader
//	t.Setenv(name, "1")                          ← one reader
//	other(foo)                                   ← foo is NOT a reader
//
// Each open paren pushes whether that call is env-shaped (its callee name
// carries the env marker) — kept in the scan so a DIRECT env call can be told
// from a mere hand-off — and an identifier is a reader when any frame encloses
// it at all.
func collectReaders(lines []string) map[string]bool {
	readers := map[string]bool{}
	frames := []bool{} // one entry per open paren; true = inside an env call
	enclosingEnv := func() bool {
		for _, f := range frames {
			if f {
				return true
			}
		}
		return false
	}
	for _, line := range lines {
		for i := 0; i < len(line); {
			c := line[i]
			switch {
			case isIdentStart(c):
				j := i
				for j < len(line) && isIdentChar(line[j]) {
					j++
				}
				tok := line[i:j]
				k := j
				for k < len(line) && line[k] == ' ' {
					k++
				}
				if k < len(line) && line[k] == '(' {
					// A call: env-shaped iff its callee carries the marker.
					frames = append(frames, enclosingEnv() ||
						strings.Contains(strings.ToLower(tok), envCalleeMarker))
					i = k + 1
					continue
				}
				if len(frames) > 0 {
					readers[tok] = true
				}
				i = j
			case c == '(':
				frames = append(frames, enclosingEnv())
				i++
			case c == ')':
				if len(frames) > 0 {
					frames = frames[:len(frames)-1]
				}
				i++
			default:
				i++
			}
		}
	}
	return readers
}

// isIdentStart / isIdentChar describe a Go identifier for the reader scanner.
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// repoRootForScan locates the module root from the test binary's working
// directory (a package test's cwd is its own directory), without failing: the
// index is built lazily, so a missing module root degrades to "no readers
// known" — which the exhaustive test below turns into a failure rather than a
// silently green gate.
func repoRootForScan() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, moduleFileName)); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// goFilesUnderDir lists production Go files below roots (repo-relative slash
// paths) — the same production set the gate framework scans.
// testScope selects whether the walk includes _test.go files.
type testScope bool

const (
	prodOnly     testScope = false
	includeTests testScope = true
)

func goFilesUnderDir(root string, roots []string, tests testScope) []string {
	var out []string
	for _, r := range roots {
		base := filepath.Join(root, filepath.FromSlash(r))
		if _, err := os.Stat(base); err != nil {
			continue
		}
		_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error { //stylegate:ignore best-effort walk: a per-entry error is re-checked inside the callback, which skips it deliberately
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") && !bool(tests) {
				return nil // prodOnly: test files are not part of the production set
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
	}
	return out
}

// commentStrippedLines blanks comments while KEEPING line numbers 1:1 with the
// file (the hit keys are file:line, so a shift would misattribute every hit).
// String literals stay in place: a reader may legitimately pass a constant
// qualified by a package, and the literal that DEFINES an env name is
// identified by the AST, not by this text scan.
//
// STRING AWARENESS IS LOAD-BEARING, and so is RAW-STRING STATE ACROSS LINES —
// both were bugs this gate found in itself:
//
//   - a "/*" inside a STRING is not a comment; treating it as one blanked the
//     rest of the file and the gate went blind while still reporting green;
//   - a multi-line RAW string (a usage/help block — cmd/agent's usage texts
//     contain "~/.gopi/sessions/*.jsonl") puts a "/*" on a line whose opening
//     backquote was lines above, so the state MUST persist across lines.
func commentStrippedLines(src string) []string {
	lines := strings.Split(src, "\n")
	inBlock := false
	inRaw := false
	for i, line := range lines {
		var b strings.Builder
		for j := 0; j < len(line); {
			switch {
			case inRaw:
				end := strings.IndexByte(line[j:], '`')
				if end < 0 {
					b.WriteString(line[j:])
					j = len(line)
					continue
				}
				b.WriteString(line[j : j+end+1])
				j += end + 1
				inRaw = false
			case inBlock:
				end := strings.Index(line[j:], blockCommentEnd)
				if end < 0 {
					j = len(line)
					continue
				}
				inBlock = false
				j += end + len(blockCommentEnd)
			case strings.HasPrefix(line[j:], lineCommentStart):
				j = len(line)
			case strings.HasPrefix(line[j:], blockCommentOpen):
				inBlock = true
				j += len(blockCommentOpen)
			case line[j] == '`':
				inRaw = true
				b.WriteByte(line[j])
				j++
			case line[j] == '"' || line[j] == '\'':
				end := stringLiteralEnd(line, j)
				b.WriteString(line[j:end])
				j = end
			default:
				b.WriteByte(line[j])
				j++
			}
		}
		lines[i] = b.String()
	}
	return lines
}

// stringLiteralEnd returns the index just past the string/rune literal that
// starts at i (the opening quote), honouring backslash escapes. A literal that
// runs off the end of the line (a raw string spanning lines) returns len(line):
// the scan is line-oriented, and a multi-line literal carries no env call.
func stringLiteralEnd(line string, i int) int {
	quote := line[i]
	for j := i + 1; j < len(line); j++ {
		if quote != '`' && line[j] == '\\' {
			j++ // escaped character: skip it (covers \" and \\)
			continue
		}
		if line[j] == quote {
			return j + 1
		}
	}
	return len(line)
}
