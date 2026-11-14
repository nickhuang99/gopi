// baseline-rekey is the ONE generator that mints the gates' baseline entries.
//
// WHY. Every baseline in this package used to be a bare `file:line` key, which is
// a function of the LINE NUMBER: an edit above a baselined site rotted its entry,
// so one defect got reported twice by one gate (a NEW violation at the site's new
// line, a STALE entry at its old one). The key is now the site's CONTENT — path,
// normalized line text, and the ordinal among identical lines in that file — and
// this program is the single writer of the lists that carry those keys.
//
// It is a GENERATOR (GOPI.md Rule 38: a figure names its generator) and it is
// also the reason a hand-written key is never needed: it reads every baseline
// declaration in this package, converts each entry, and rewrites ONLY the quoted
// entry lines — the surrounding commentary (which carries each list's reason,
// its provenance and its measurement) is preserved byte for byte.
//
//	run:  go run internal/standards/testdata/baseline-rekey.go        (rewrite)
//	      go run internal/standards/testdata/baseline-rekey.go -check (report only, exit 1 on any delta)
//
// It is idempotent: a list already holding content keys is verified, not moved —
// an entry whose key still matches a line in its file is reported `ok`, one whose
// text moved (`Locate`) is re-keyed ONLY with the printout showing the reading,
// and one whose text is gone is a hard error, because that is a site that was
// repaired and whose permit must be DELETED by a human rather than silently
// dropped here.
//
// It walks up for go.mod, so it can be run from any directory in the module.
package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/sitekey"
)

const (
	// baselineVarSuffix identifies the declarations this program owns. A
	// []string var ending in this suffix IS a baseline.
	baselineVarSuffix = "Baseline"

	// declaration heads. Two shapes exist in this package: a literal
	// (`var xBaseline = []string{`), which this program rewrites, and an
	// explicitly empty declaration (`var xBaseline []string`), which carries no
	// entries and is left alone.
	declLiteralHead = " = []string{"
	declEmptyHead   = " []string"

	// entryIndent is the indentation of an entry inside a declaration, and
	// declEnd is the line that closes one.
	entryIndent = "\t"
	declEnd     = "}"

	// newline separates the lines of a source file.
	newline = "\n"

	// pkgDir is the package this program generates for, relative to the module
	// root, and scanSuffix is the file set it reads: the declarations live in
	// package source, tests included (a gate declared in a _test.go file owns
	// its own baseline list).
	pkgDir       = "internal/standards"
	scanSuffix   = ".go"
	moduleFile   = "go.mod"
	gofmtHint    = "gofmt -w internal/standards/"
	lineReadNote = "read from the working tree, so run this on the branch that will carry the keys"
)

var checkOnly = flag.Bool("check", false, "report the delta without writing (exit 1 on any change needed)")

func main() {
	flag.Parse()
	root, err := moduleRoot()
	if err != nil {
		fail(err)
	}
	carriers, err := carrierFiles(root)
	if err != nil {
		fail(err)
	}
	changed, errs := 0, 0
	for _, rel := range carriers {
		n, err := rekeyFile(root, rel)
		if err != nil {
			fmt.Printf("MISS %s: %v\n", rel, err)
			errs++
			continue
		}
		changed += n
	}
	fmt.Printf("\n%d file(s) carried baselines; %d entry line(s) %s.\n",
		len(carriers), changed, pick(*checkOnly, "WOULD CHANGE", "changed"))
	if *checkOnly && changed > 0 {
		os.Exit(1)
	}
	if errs > 0 {
		os.Exit(1)
	}
}

// pick returns a when cond, else b — spelled once so the two report shapes in
// main() cannot drift.
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// moduleRoot walks up from the working directory to the directory holding go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, moduleFile)); serr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s above %s — run this from inside the module", moduleFile, dir)
		}
		dir = parent
	}
}

// carrierFiles lists the package files that declare a baseline, sorted so the
// run is deterministic.
func carrierFiles(root string) ([]string, error) {
	dir := filepath.Join(root, filepath.FromSlash(pkgDir))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), scanSuffix) {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		if strings.Contains(string(b), baselineVarSuffix+declLiteralHead) {
			out = append(out, pkgDir+"/"+e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// entryLine is one quoted entry inside a baseline declaration, together with the
// trailing comment that is its reason. The two travel as ONE value because the
// list is SORTED: sorting keys while leaving comments in place re-attaches every
// reason to the wrong permit (measured in this program's first version — the
// keys are content, so their order differs from the old `file:line` order and
// every comment shifted one row). A reason on the wrong permit is a silent
// permission dressed as a reviewed one (GOPI.md Rule 15b).
type entryLine struct {
	index   int    // 0-based line index into the file's lines
	value   string // the unquoted entry
	comment string // the trailing ` // …` reason, "" when the entry carries none
}

// rekeyFile converts one file's baseline declarations. It returns how many entry
// lines it rewrote.
func rekeyFile(root, rel string) (int, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(raw), newline)
	rewritten := 0
	for _, decl := range declarations(lines) {
		fmt.Printf("\n== %s: %s (%d entry line(s)) ==\n", rel, decl.name, len(decl.entries))
		for _, u := range decl.unreadable {
			return rewritten, fmt.Errorf("%s: %s is inside the %s block but is neither a comment nor a "+
				"quoted entry — this program refuses to skip a line it did not read", rel, u, decl.name)
		}
		keyed := make([]entryLine, 0, len(decl.entries))
		for _, e := range decl.entries {
			key, how, kerr := keyFor(root, e.value)
			if kerr != nil {
				return rewritten, fmt.Errorf("%s line %d (%s): %w", rel, e.index+1, e.value, kerr)
			}
			fmt.Printf("  %-8s %s\n           %s\n", how, short(e.value), short(key))
			e.value = key
			keyed = append(keyed, e)
		}
		// Sort the (key, comment) PAIRS, so a permit's reason moves with the
		// permit it explains.
		sort.Slice(keyed, func(i, j int) bool { return keyed[i].value < keyed[j].value })
		for i, e := range keyed {
			// The line is rebuilt from the key rather than patched: the entry IS
			// the key (Rule 14 — one representation), and a patch would leave the
			// old value readable in the file if the lengths differed.
			//
			// A TRAILING COMMENT IS PRESERVED AND MOVES WITH ITS KEY, because in
			// several lists it is the permit's own reason ("— the budget window
			// OPENS here"), and Rule 15b keeps a permission's justification on
			// the same screen as the permission.
			target := decl.entries[i].index
			newLine := entryIndent + strconv.Quote(e.value) + "," + e.comment
			if lines[target] != newLine {
				lines[target] = newLine
				rewritten++
			}
		}
	}
	if rewritten == 0 {
		return 0, nil
	}
	// The rebuilt lines are emitted through go/format, so what this program
	// writes IS what gofmt would write. Without it the two disagree on the
	// alignment of a run of trailing comments, and `-check` then reports a
	// permanent delta over a tree that is already correct — the reading would be
	// unactionable, and an unactionable check is one a reader learns to ignore
	// (measured: 31 lines reported as "would change" on a gofmt-clean tree).
	formatted, ferr := format.Source([]byte(strings.Join(lines, newline)))
	if ferr != nil {
		return rewritten, fmt.Errorf("formatting %s: %w", rel, ferr)
	}
	out := string(formatted)
	if out == string(raw) {
		return 0, nil
	}
	// THE DELTA IS COUNTED FROM THE FORMATTED OUTPUT, not from the pre-format
	// lines: gofmt re-pads a run of trailing comments, so a per-line comparison
	// against the pre-format string reports a change on a line that is already
	// at its final form — and a `-check` that never settles to zero is not a
	// check (this exact defect was measured on this program's own output).
	changed := differingLineCount(string(raw), out)
	if *checkOnly {
		return changed, nil
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return changed, err
	}
	return changed, nil
}

// differingLineCount reports how many line POSITIONS differ between two texts —
// the honest size of a rewrite, for the report.
func differingLineCount(before, after string) int {
	a := strings.Split(before, newline)
	b := strings.Split(after, newline)
	n := 0
	for i := 0; i < len(a) || i < len(b); i++ {
		switch {
		case i >= len(a), i >= len(b):
			n++
		case a[i] != b[i]:
			n++
		}
	}
	return n
}

// keyFor converts one entry — old (`file:line`) or new (a content key) — into the
// content key of the line it names, reporting HOW it did it.
func keyFor(root, entry string) (key, how string, err error) {
	if sitekey.WellFormed(entry) {
		rel := sitekey.File(entry)
		src, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			return "", "", rerr
		}
		if line := sitekey.Locate(src, entry); line > 0 {
			k, kerr := sitekey.Key(rel, line, src)
			if kerr != nil {
				return "", "", kerr
			}
			if k == entry {
				return entry, "ok", nil
			}
			return k, "MOVED", nil
		}
		return "", "", fmt.Errorf("the text this entry names is GONE from %s — the site was repaired "+
			"or deleted, so its permit must be DELETED by a human (never dropped by this generator)", rel)
	}
	rel, line, perr := parseOldKey(entry)
	if perr != nil {
		return "", "", perr
	}
	src, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if rerr != nil {
		return "", "", rerr
	}
	// The site's own text is printed with the reading: converting a legacy entry
	// is the one step where the generator cannot tell "the site is still here"
	// from "the site moved and this line now holds something else", so the line
	// it keyed is put in front of the reader. Whether the resulting permit still
	// matches a live hit is the GATE's verdict (a STALE entry, and a NEW
	// violation at the site's new line), never this program's guess.
	if line-1 < len(strings.Split(string(src), newline)) {
		fmt.Printf("           line %d: %s\n", line, short(strings.Split(string(src), newline)[line-1]))
	}
	k, kerr := sitekey.Key(rel, line, src)
	if kerr != nil {
		return "", "", kerr
	}
	return k, "REKEYED", nil
}

// trailingComment returns the ` // …` tail that follows the entry's closing quote,
// formatted as it will be re-emitted (a single space, then the comment text) — the
// permit's own reason, which every rewrite keeps attached to its key.
//
// It searches AFTER the closing quote, never from the start of the line, and that
// distinction is load-bearing: a KEY's own text can contain `//` (a permitted
// site carrying `//stylegate:ignore …` inside its line), so taking the first `//`
// on the line captures the key's tail instead of the comment — and because the
// captured text is then re-emitted as the new comment, it duplicates on every run
// (measured: the reason appended three times after three passes, each pass looking
// idempotent in isolation). closingQuote is escape-aware, so the quote that ends
// the key literal is found correctly even with `\"` inside it.
func trailingComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, `"`) {
		return ""
	}
	end := closingQuote(trimmed)
	if end < 0 {
		return ""
	}
	rest := trimmed[end+1:]
	i := strings.Index(rest, "//")
	if i < 0 {
		return ""
	}
	return " " + strings.TrimSpace(rest[i:])
}

// parseOldKey splits a legacy `repo/relative/path.go:123` entry.
func parseOldKey(entry string) (string, int, error) {
	i := strings.LastIndex(entry, ":")
	if i <= 0 {
		return "", 0, fmt.Errorf("not a content key and not a file:line entry")
	}
	n, err := strconv.Atoi(entry[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("line %q is not a number: %w", entry[i+1:], err)
	}
	return entry[:i], n, nil
}

// short truncates a value for the report so one long line cannot flood it.
func short(s string) string {
	const width = 96
	if len(s) <= width {
		return s
	}
	return s[:width] + "…"
}

// declaration is one `var xBaseline = []string{…}` block found in a file.
type declaration struct {
	name string
	// entries are the quoted entries the program recognised.
	entries []entryLine
	// unreadable are the block's lines that are neither blank, nor a comment,
	// nor a recognised entry — the shape this program would otherwise SKIP in
	// silence, which is exactly the failure a generator must not have (GOPI.md
	// Rule 15: an item it cannot read is reported, never dropped). Measured: the
	// first version of this program skipped every entry carrying a trailing
	// comment (screen_assertion_gate_test.go's whole list) and reported a clean
	// run over a list it had not read.
	unreadable []string
}

// declarations finds every literal baseline declaration in a file's lines. A
// declaration with an explicitly empty shape (`var x []string`) carries no
// entries and is not returned: there is nothing to key.
func declarations(lines []string) []declaration {
	var out []declaration
	for i := 0; i < len(lines); i++ {
		name, ok := literalDeclName(lines[i])
		if !ok {
			continue
		}
		decl := declaration{name: name}
		for j := i + 1; j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == declEnd {
				break
			}
			if v, ok := quotedEntry(trimmed); ok {
				decl.entries = append(decl.entries, entryLine{
					index:   j,
					value:   v,
					comment: trailingComment(lines[j]),
				})
				continue
			}
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			decl.unreadable = append(decl.unreadable, fmt.Sprintf("line %d: %s", j+1, trimmed))
		}
		if len(decl.entries) > 0 || len(decl.unreadable) > 0 {
			out = append(out, decl)
		}
	}
	return out
}

// literalDeclName reports whether a line opens a literal baseline declaration and
// returns the variable name.
func literalDeclName(line string) (string, bool) {
	if !strings.HasPrefix(line, "var ") || !strings.HasSuffix(line, declLiteralHead) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(line, "var "), declLiteralHead)
	if !strings.HasSuffix(name, baselineVarSuffix) {
		return "", false
	}
	return name, true
}

// quotedEntry unquotes one `"…",` line, reporting whether it is one. A TRAILING
// COMMENT is allowed and ignored here (trailingComment keeps it in place on the
// rewrite): several lists annotate each permit with the reason it exists.
func quotedEntry(trimmed string) (string, bool) {
	if !strings.HasPrefix(trimmed, `"`) {
		return "", false
	}
	end := closingQuote(trimmed)
	if end < 0 {
		return "", false
	}
	unquoted, err := strconv.Unquote(trimmed[:end+1])
	if err != nil {
		return "", false
	}
	return unquoted, true
}

// closingQuote returns the index of the quote that closes the literal opening at
// s[0], or -1 — escape-aware, so a key containing a `\"` or a `\\` is read whole.
func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

// fail prints and exits non-zero.
func fail(err error) {
	fmt.Fprintf(os.Stderr, "baseline-rekey: %v\n%s\n", err, lineReadNote+"; "+gofmtHint)
	os.Exit(1)
}
