// search_fallback.go — the ZERO-BINARY leg of code_search / keyword_search.
//
// THE DEFECT (measured 2026-10-11/12 on host nick-sager; the raw artifacts live
// in that session's own directory). Both tools shell out — code_search to ctags,
// keyword_search to ripgrep — and neither had a fallback that is not itself an
// external binary. On a host with neither package installed:
//
//	$ gopi tool code-search NewAgent --root internal
//	code_search: ctags: exec: "ctags": executable file not found in $PATH:
//	$ gopi tool keyword_search NewAgent
//	keyword_search: rg: exec: "rg": executable file not found in $PATH:
//
// Worse, because codeSearchImpl's clean-miss hop called keywordSearchImpl
// UNCONDITIONALLY, installing ONLY ctags changed the same call's error into the
// OTHER tool's name — `code_search: keyword_search: rg: exec: "rg": ...` — a
// caller who asked for code_search was told about keyword_search, and "install
// ctags" could not fix code_search.
//
// WHAT THIS FILE IS. One in-process scanner over scope.RootPath, standard
// library only (Rule 35 — no doublestar, no new dependency), reachable whenever
// the external backend for the CALLING surface is not startable. It serves both
// legs:
//
//   - scanTextHits is keyword_search's fallback leg: every line matching the
//     query (a regex; an invalid expression is searched LITERALLY rather than
//     aborting the search) becomes a TextHit, with the same snippet shaping and
//     the same result cap the ripgrep leg applies.
//   - scanSymbolHits is code_search's LAST-RESORT leg: definition-shaped lines
//     for the symbol come back as definitions; when there are none, the symbol's
//     occurrences come back with kind codeSearchReferenceKind — the two-answer
//     contract the ctags-miss hop has always had, now with no binary behind it.
//
// PARITY WITH THE BACKENDS, stated so it is a decision and not an accident.
// ripgrep's own defaults are matched: hidden entries (a name beginning with ".")
// are skipped, a version-control directory is never descended, symlinks are not
// followed, binary content stops a file's scan, and the scope's include/exclude
// globs decide what else is read. Paths are reported as filepath.Join(root, rel)
// — a path the caller can open. Two deliberate divergences: ignore-file rules
// (.gitignore/.ignore) are NOT honored here (the scanner is the last resort on a
// host with no search backend at all, and those semantics are a dependency's
// worth of work — Rule 35), and an unreadable file is a LOUD error naming the
// path (rg exits 2 with "Permission denied" on the same input; Rule 15 — never a
// silent gap in coverage).
//
// WHY IT LIVES IN internal/tools AND NOT IN A SCRIPT: it is the search tools'
// own behavioural contract, so it belongs to the tools (Rule 35 — a script beside
// gopi is not the answer). Nothing here is registered as a new tool (Rule 3): it
// is a leg of the two that already exist.
package tools

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// searchBackendCtags / searchBackendRipgrep name the two external backends
	// ONCE, so the availability probe and every message that explains which leg
	// was taken spell them the same way (Rule 13).
	searchBackendCtags   = "ctags"
	searchBackendRipgrep = "rg"

	// searchFallbackVCSDirName is never descended by the scanner: ripgrep
	// hardcodes the same skip, and code_search's own index artifact lives beside
	// it. Compared by exact basename, never by a suffix pattern.
	searchFallbackVCSDirName = ".git"

	// searchFallbackMaxFileBytes bounds how much of ONE file the scanner reads. A
	// file this large is data, not source; reading it whole would let one artifact
	// dominate the tool's wall time.
	searchFallbackMaxFileBytes = 8 << 20

	// searchFallbackMaxLineBytes is the longest line the scanner will hold; it
	// matches the ripgrep leg's scanner buffer, so the two legs degrade the same
	// way on a machine-generated single-line file.
	searchFallbackMaxLineBytes = 1 << 20

	// searchFallbackGlobNegate prefixes an EXCLUDE glob in the ripgrep argument
	// list ("!.git/**"); the marker is stripped, the glob is what matters.
	searchFallbackGlobNegate = "!"

	// searchFallbackGlobAnyDirs is the "zero or more directories" prefix a scope
	// glob is written with ("**/*.go"). It must match a file at the root too,
	// which is what makes "**/*.go" and "*.go" agree for a top-level file.
	searchFallbackGlobAnyDirs = "**/"

	// searchFallbackGlobStar / searchFallbackGlobQuestion are the two wildcards a
	// scope glob may use; neither crosses a path separator, and a file's BASE NAME
	// is tested as well (ripgrep's own glob semantics: `*.go` matches a .go file
	// in any subdirectory).
	searchFallbackGlobStar     = '*'
	searchFallbackGlobQuestion = '?'

	// searchFallbackGlobDirSuffix is how a scope glob names a DIRECTORY's
	// contents ("vendor/**"). Its presence gives the walker a prune form: the same
	// glob without the suffix names the directory itself.
	searchFallbackGlobDirSuffix = "/**"

	// searchFallbackMaxDecorators bounds how many leading decorators are skipped
	// before a line is classified (see definitionKind) — bounded, never a loop on
	// hostile input.
	searchFallbackMaxDecorators = 4
)

// codeSearchReferenceKind is the SymbolKind code_search stamps on an occurrence
// that is not a definition. One definition, used by both hops (Rule 14).
const codeSearchReferenceKind = "reference"

// searchBackendAvailable reports whether an external search backend can be
// started at all. It is a LookPath probe, not a run: the point is to choose the
// leg BEFORE an exec error has to be interpreted, so a host without the package
// never spawns a doomed child (and cannot produce a half-parsed result from one).
func searchBackendAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// globPattern is one include/exclude glob of a SearchScope, compiled ONCE per
// search. dirRE is non-nil only for a "…/**" glob and is the form that matches
// the DIRECTORY the glob names, so the walker can prune it without descending.
type globPattern struct {
	re    *regexp.Regexp
	dirRE *regexp.Regexp
}

// compileGlob translates a scope glob into the matching forms. A glob that does
// not compile is reported, never silently dropped (Rule 15): a scope whose filter
// cannot be honored would return hits the caller asked to exclude.
func compileGlob(glob string) (globPattern, error) {
	glob = strings.TrimPrefix(glob, searchFallbackGlobNegate)
	p := globPattern{}
	re, err := regexp.Compile(globToRegexp(glob))
	if err != nil {
		return globPattern{}, fmt.Errorf("search scope: glob %q: %w", glob, err)
	}
	p.re = re
	if base, ok := strings.CutSuffix(glob, searchFallbackGlobDirSuffix); ok {
		dirRE, dirErr := regexp.Compile(globToRegexp(base))
		if dirErr != nil {
			return globPattern{}, fmt.Errorf("search scope: glob %q: %w", glob, dirErr)
		}
		p.dirRE = dirRE
	}
	return p, nil
}

// globToRegexp renders one glob as an anchored regular expression. `**/` means
// "zero or more directories" (so "**/*.go" matches a root-level .go file), `**`
// means "any characters", `*` and `?` never cross a separator, and every other
// character is quoted — a glob carrying regexp punctuation matches literally.
func globToRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], searchFallbackGlobAnyDirs):
			b.WriteString(`(?:[^/]*/)*`)
			i += len(searchFallbackGlobAnyDirs) - 1
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(`.*`)
			i++
		case glob[i] == searchFallbackGlobStar:
			b.WriteString(`[^/]*`)
		case glob[i] == searchFallbackGlobQuestion:
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	b.WriteString("$")
	return b.String()
}

// matches reports whether a path (slash-separated, relative to the search root)
// passes this glob. A FILE is tested against its full relative path AND its base
// name — the union ripgrep implements, so the two legs agree about `*.go`. A
// DIRECTORY matches only a "…/**" glob through its prune form.
func (p globPattern) matches(rel string, isDir bool) bool {
	if p.re != nil && p.re.MatchString(rel) {
		return true
	}
	if isDir {
		return p.dirRE != nil && p.dirRE.MatchString(rel)
	}
	return p.re != nil && p.re.MatchString(path.Base(rel))
}

// scopeMatcher is a scope's globs compiled for the walk: the exclude set (a file
// or directory the caller ruled out) and the include set (empty = every file).
type scopeMatcher struct {
	include []globPattern
	exclude []globPattern
}

// compileScopeMatcher compiles both glob lists, naming the failing glob.
func compileScopeMatcher(scope SearchScope) (scopeMatcher, error) {
	m := scopeMatcher{}
	for _, g := range scope.IncludeGlob {
		p, err := compileGlob(g)
		if err != nil {
			return scopeMatcher{}, err
		}
		m.include = append(m.include, p)
	}
	for _, g := range scope.ExcludeGlob {
		p, err := compileGlob(g)
		if err != nil {
			return scopeMatcher{}, err
		}
		m.exclude = append(m.exclude, p)
	}
	return m, nil
}

// excluded reports whether the scope excludes this path. An EXCLUDED directory is
// pruned whole, which is the one decision the walker may make about a directory on
// its own.
func (m scopeMatcher) excluded(rel string, isDir bool) bool {
	for _, p := range m.exclude {
		if p.matches(rel, isDir) {
			return true
		}
	}
	return false
}

// allowsFile reports whether a FILE may be read: not excluded, and matching the
// include set when one is present. Include globs constrain FILES only — a
// directory is never pruned for failing them, because nothing about its name says
// whether the files under it match.
func (m scopeMatcher) allowsFile(rel string) bool {
	if m.excluded(rel, false) {
		return false
	}
	if len(m.include) == 0 {
		return true
	}
	for _, p := range m.include {
		if p.matches(rel, false) {
			return true
		}
	}
	return false
}

// searchWalkEntry is one file the walk will read.
type searchWalkEntry struct {
	rel string // slash-separated, relative to the search root
	abs string
}

// walkSearchTree walks root in a deterministic (lexical) order, applying the
// scope's globs, and calls visit for every regular file that passes. visit
// returns stop=true to end the walk early (the result cap was reached).
//
// Only REGULAR files are read and only real directories are descended: symlinks
// are never followed, which is ripgrep's own default and the one rule that
// removes every link cycle and every dangling-link error from this code path.
func walkSearchTree(root string, matcher scopeMatcher, visit func(searchWalkEntry) (stop bool)) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("scanner: %s: %w", p, err)
		}
		name := d.Name()
		if d.IsDir() {
			if p == root {
				return nil
			}
			// Hidden and VCS directories are never descended (ripgrep's default),
			// and the scope may exclude more.
			if strings.HasPrefix(name, ".") || name == searchFallbackVCSDirName {
				return fs.SkipDir
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return fmt.Errorf("scanner: %s: %w", p, relErr)
			}
			if matcher.excluded(filepath.ToSlash(rel), true) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlink / device / fifo — not followed (rg's default)
		}
		if strings.HasPrefix(name, ".") {
			return nil // hidden file (rg's default; also keeps .gopi-tags out)
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return fmt.Errorf("scanner: %s: %w", p, relErr)
		}
		rel = filepath.ToSlash(rel)
		if !matcher.allowsFile(rel) {
			return nil
		}
		if visit(searchWalkEntry{rel: rel, abs: p}) {
			return fs.SkipAll
		}
		return nil
	})
}

// scanLines reads one file, applies fn to every text line, and stops at binary
// content. A NUL byte marks the content binary — the scan of THAT file stops
// without failing the search (rg behaves the same way). A line longer than
// searchFallbackMaxLineBytes, or an unreadable file, is a named error: this leg is
// the last resort, and a silently half-scanned tree is exactly the silent failure
// this file exists to remove (Rule 15).
func scanLines(abs string, fn func(line string, lineNo int) (stop bool)) error {
	f, err := os.Open(abs)
	if err != nil {
		return fmt.Errorf("scanner: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, searchFallbackMaxFileBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), searchFallbackMaxLineBytes)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if strings.IndexByte(raw, 0) >= 0 {
			return nil // binary content — stop this file, not the search
		}
		if fn(raw, lineNo) {
			return nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		if errors.Is(scanErr, bufio.ErrTooLong) {
			return fmt.Errorf("scanner: %s: line longer than %d bytes", abs, searchFallbackMaxLineBytes)
		}
		return fmt.Errorf("scanner: %s: %w", abs, scanErr)
	}
	return nil
}

// scanTextHits is keyword_search's zero-binary leg: every line matching the query
// yields a TextHit shaped exactly like the ripgrep leg's (same snippet builder, so
// the two render identically). The collection stops at the scope's cap while
// COUNTING continues, so "N more matches" stays truthful without holding an
// unbounded result set in memory.
func scanTextHits(root string, scope SearchScope, query string) ([]TextHit, int, error) {
	re, err := compileQueryRegexp(query)
	if err != nil {
		return nil, 0, err
	}
	matcher, err := compileScopeMatcher(scope)
	if err != nil {
		return nil, 0, err
	}
	limit := scope.MaxResult
	var (
		hits  []TextHit
		total int
		fail  error
	)
	visit := func(e searchWalkEntry) bool {
		fileErr := scanLines(e.abs, func(line string, lineNo int) bool {
			loc := re.FindStringIndex(line)
			if loc == nil {
				return false
			}
			total++
			if limit <= 0 || len(hits) < limit {
				hits = append(hits, TextHit{
					FilePath: filepath.Join(root, filepath.FromSlash(e.rel)),
					LineNo:   lineNo,
					Snippet:  buildSnippet(line, loc[0]+1),
				})
			}
			return false
		})
		if fileErr != nil {
			fail = fileErr
			return true
		}
		return false
	}
	if walkErr := walkSearchTree(root, matcher, visit); walkErr != nil {
		return nil, 0, walkErr
	}
	if fail != nil {
		return nil, 0, fail
	}
	dropped := 0
	if limit > 0 && total > limit {
		dropped = total - limit
	}
	return hits, dropped, nil
}

// compileQueryRegexp compiles a keyword_search query. An INVALID expression is
// searched as a LITERAL string rather than failing the tool: the query reaches
// this leg from a caller who asked for a text search, and "your regex is broken"
// is a worse answer than the literal hits it can still produce. Every valid
// expression keeps ripgrep's own semantics.
func compileQueryRegexp(query string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(query)
	if err == nil {
		return re, nil
	}
	literal, literalErr := regexp.Compile(regexp.QuoteMeta(query))
	if literalErr != nil {
		return nil, fmt.Errorf("keyword_search: query %q: %w", query, err)
	}
	return literal, nil
}

// searchFallbackDefinitionKeywords maps the leading keyword of a definition line
// to the SymbolKind code_search reports. It is the scanner's whole notion of "this
// line defines something" — no parser, so the test is cheap and the classification
// is best-effort by construction. The keyword alone is never enough: the name it
// DECLARES has to be the symbol being looked up (see definitionSignature), which is
// what keeps `func caller() { _ = GadgetTarget }` from being reported as a
// definition of GadgetTarget.
var searchFallbackDefinitionKeywords = map[string]string{
	"func":      "function",
	"fn":        "function",
	"def":       "function",
	"function":  "function",
	"impl":      "function",
	"type":      "type",
	"class":     "type",
	"struct":    "type",
	"interface": "type",
	"trait":     "type",
	"enum":      "type",
	"var":       "variable",
	"const":     "variable",
	"let":       "variable",
}

// searchFallbackDefinitionModifiers are language keywords that may precede the
// definition keyword ("pub fn", "export class", "static func"). They are skipped,
// in order and up to searchFallbackMaxModifiers of them, before a token is
// classified.
var searchFallbackDefinitionModifiers = map[string]bool{
	"pub": true, "export": true, "async": true, "static": true, "unsafe": true,
	"inline": true, "extern": true, "public": true, "private": true,
	"protected": true, "final": true, "abstract": true, "default": true,
	"declare": true,
}

// searchFallbackMaxModifiers bounds how many leading tokens may be skipped while
// looking for the definition keyword — bounded, so no input can make this scan run
// away.
const searchFallbackMaxModifiers = 4

// definitionSignature reports the SymbolKind and the DECLARED NAME of a definition
// line ("function"/"Foo" for `func Foo() {`), and ""/"" when the line declares
// nothing. Names are compared as IDENTIFIERS, not as substrings: that is the whole
// difference between "this line defines the symbol" and "this line mentions it".
func definitionSignature(line string) (kind string, name string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || lineOpensComment(trimmed) {
		return "", ""
	}
	rest := skipDecorators(trimmed)
	for i := 0; i < searchFallbackMaxModifiers && rest != ""; i++ {
		head := leadingWord(rest)
		if head == "" {
			// Punctuation leads the line (a pointer or generic marker): step
			// over it and look at the next token.
			rest = strings.TrimLeft(rest, "*&[]?<>'")
			continue
		}
		rest = strings.TrimLeft(rest[len(head):], " \t")
		if k := searchFallbackDefinitionKeywords[head]; k != "" {
			after := rest
			// A Go method declares its receiver in a leading parenthesised
			// group, and the declared name follows that group.
			if strings.HasPrefix(after, "(") {
				if i := strings.Index(after, ")"); i >= 0 {
					after = strings.TrimLeft(after[i+1:], " \t")
				}
			}
			return k, leadingIdentifier(after)
		}
		if !searchFallbackDefinitionModifiers[head] {
			return "", ""
		}
	}
	return "", ""
}

// skipDecorators removes a bounded run of leading decorators/annotations, so
// `#[derive(Debug)] struct S` and `@Override public void f()` are still read as
// declarations, and returns "" when the line is prose about a symbol rather than a
// declaration of it.
func skipDecorators(line string) string {
	rest := strings.TrimSpace(line)
	for i := 0; i < searchFallbackMaxDecorators && rest != ""; i++ {
		if rest[0] != '#' && rest[0] != '@' {
			break
		}
		next := strings.IndexAny(rest, " \t")
		if next < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[next:])
	}
	if rest == "" || lineOpensComment(rest) {
		return ""
	}
	return rest
}

// leadingWord returns the identifier-ish run at the start of s, "" when s does not
// start with one.
func leadingWord(s string) string {
	end := 0
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if !isIdentRune(r) {
			break
		}
		end += size
	}
	return s[:end]
}

// leadingIdentifier returns the first identifier in s, stepping over the
// punctuation a declared name may be preceded by ("*Foo", "[3]Foo", "?Foo").
func leadingIdentifier(s string) string {
	for from := 0; from < len(s); {
		r, size := utf8.DecodeRuneInString(s[from:])
		if isIdentRune(r) {
			return leadingWord(s[from:])
		}
		from += size
	}
	return ""
}

// lineOpensComment reports whether a line is PROSE about a symbol rather than a
// declaration of it. Without it a commented-out `// func Foo()` would be handed to
// the caller as a definition. The test is deliberately shallow and stated: the
// scanner has no parser, and a cheap honest filter beats an expensive guess.
func lineOpensComment(line string) bool {
	switch {
	case strings.HasPrefix(line, "//"), strings.HasPrefix(line, "/*"),
		strings.HasPrefix(line, "* "), strings.HasPrefix(line, "*/"),
		strings.HasPrefix(line, "--"), strings.HasPrefix(line, "%"),
		strings.HasPrefix(line, ";;"), strings.HasPrefix(line, "REM "):
		return true
	case strings.HasPrefix(line, "#"):
		// A Rust/Python attribute opens with "#["; every other "#" line is a
		// comment in the languages this scanner reads.
		return !strings.HasPrefix(line, "#[")
	}
	return false
}

// isIdentRune reports whether r may appear inside an identifier: an ASCII
// identifier rune, or a LETTER/DIGIT of any other script (a symbol in a non-Latin
// script is still a symbol). Unicode PUNCTUATION is deliberately not an identifier
// rune — "\u300cNewClient\u300d" is a symbol between brackets, not a longer name.
func isIdentRune(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
		return true
	}
	return r > 0x7f && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// wholeSymbolIndex returns the byte offset of the first occurrence of symbol in
// line that is bounded by non-identifier runes on BOTH sides, and whether such an
// occurrence exists. A plain substring search would match NewClientPool for the
// symbol NewClient — a wrong occurrence handed to a caller with no way to see it
// was wrong — and it would match inside a quoted string or a comment exactly as it
// matches in a call, which is the same hit at the same line for a text search.
func wholeSymbolIndex(line, symbol string) (int, bool) {
	for from := 0; from+len(symbol) <= len(line); {
		i := strings.Index(line[from:], symbol)
		if i < 0 {
			return 0, false
		}
		at := from + i
		if !startsAtIdentBoundary(line, at, len(symbol)) {
			from = at + len(symbol)
			continue
		}
		return at, true
	}
	return 0, false
}

// startsAtIdentBoundary reports whether line[at:at+n] is a whole identifier: the
// rune BEFORE at (when there is one) and the rune AT at+n (when there is one) are
// both non-identifier. Both neighbours are decoded as runes, so a multi-byte rune is
// never mistaken for a boundary.
func startsAtIdentBoundary(line string, at, n int) bool {
	if at > 0 {
		if r, _ := utf8.DecodeLastRuneInString(line[:at]); isIdentRune(r) {
			return false
		}
	}
	if at+n < len(line) {
		if r, _ := utf8.DecodeRuneInString(line[at+n:]); isIdentRune(r) {
			return false
		}
	}
	return true
}

// scanSymbolHits is code_search's LAST-RESORT leg: the symbol's definition lines
// when the scanner finds any, otherwise its occurrences marked
// codeSearchReferenceKind — the same two-answer contract queryDefinition has. The
// walk runs to completion (definitions may sort anywhere in the tree) while both
// result sets stay capped, so memory is bounded by the scope's cap, not by the
// size of the tree.
func scanSymbolHits(root string, scope SearchScope, symbol string) ([]CodeSymbolHit, int, error) {
	matcher, err := compileScopeMatcher(scope)
	if err != nil {
		return nil, 0, err
	}
	limit := scope.MaxResult
	keep := func(n int) bool { return limit <= 0 || n < limit }
	var (
		defs, refs []CodeSymbolHit
		defTotal   int
		refTotal   int
		fail       error
	)
	visit := func(e searchWalkEntry) bool {
		fileErr := scanLines(e.abs, func(line string, lineNo int) bool {
			at, ok := wholeSymbolIndex(line, symbol)
			if !ok {
				return false
			}
			shown := filepath.Join(root, filepath.FromSlash(e.rel))
			trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
			if kind, declared := definitionSignature(trimmed); kind != "" && declared == symbol {
				defTotal++
				if keep(len(defs)) {
					defs = append(defs, CodeSymbolHit{
						SymbolName: symbol, SymbolKind: kind, FilePath: shown,
						LineNo: lineNo, Signature: buildSnippet(trimmed, at+1),
					})
				}
				return false
			}
			refTotal++
			if keep(len(refs)) {
				refs = append(refs, CodeSymbolHit{
					SymbolName: symbol, SymbolKind: codeSearchReferenceKind, FilePath: shown,
					LineNo: lineNo, Signature: buildSnippet(line, at+1),
				})
			}
			return false
		})
		if fileErr != nil {
			fail = fileErr
			return true
		}
		return false
	}
	if walkErr := walkSearchTree(root, matcher, visit); walkErr != nil {
		return nil, 0, walkErr
	}
	if fail != nil {
		return nil, 0, fail
	}
	if len(defs) > 0 {
		return defs, overflow(defTotal, limit), nil
	}
	return refs, overflow(refTotal, limit), nil
}

// overflow reports how many hits the cap hid, given the total the walk counted.
func overflow(total, limit int) int {
	if limit > 0 && total > limit {
		return total - limit
	}
	return 0
}
