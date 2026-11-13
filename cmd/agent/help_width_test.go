package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskverbs"
)

// helpTextMinConsts is the floor on how many help constants the AST sweep must
// discover. A scan that silently finds nothing passes every assertion below
// vacuously (the blind-green trap recorded in internal/standards' env-reader
// gate), so the sweep asserts it saw the real package.
const helpTextMinConsts = 30

// helpWidthEscape marks a help constant whose lines are deliberately allowed to
// exceed the wrap budget (e.g. a table whose columns are data, not prose). The
// marker is a REVIEWED exception and stays visible in the sweep, never a
// weakening of the check.
const helpWidthEscape = "help-width-ok:"

// TestEveryHelpConstantFitsTheWrapBudget is the exhaustive format gate for the
// SUBCOMMAND half of `gopi help` — the half the 2026-09-13 epic left
// unguarded. The top-level text renders from data through the config renderer
// and is bounded by TestTopLevelUsageIsAlignedAndTerse; every subcommand's
// usage is a hand-written raw-string constant in this package, and until this
// test nothing measured them: 14 lines ran past the terminal margin (the
// 152-column `gopi task sync` synopsis, the 103-column log row, two
// CJK-inflated rows whose width only shows when a glyph counts two columns).
//
// The sweep is DISCOVERY-based, not a hand-kept list: every raw-string
// constant named like help text in the package is parsed with go/parser and
// measured, so a NEW usage block is covered the moment it is written.
func TestEveryHelpConstantFitsTheWrapBudget(t *testing.T) {
	srcs := helpTextConsts(t)
	if len(srcs) < helpTextMinConsts {
		t.Fatalf("AST sweep found only %d help constants (want >= %d) — the sweep went blind, fix it before trusting a green run", len(srcs), helpTextMinConsts)
	}
	for _, c := range srcs {
		if strings.Contains(c.doc, helpWidthEscape) {
			continue
		}
		text := c.text
		if c.verbs > 0 {
			// A help constant can be a fmt TEMPLATE (values injected at render
			// time); render it with the SAME arguments its production var uses,
			// so the measured lines are the ones a user sees. The table below is
			// keyed by constant NAME and a template with no entry is a FATAL
			// failure, not a silent pass — measuring `%!s(int=2)` as text would
			// be a green run that measured nothing (Rule 15).
			args, ok := helpTemplateArgs[c.name]
			if !ok || len(args) == 0 {
				t.Fatalf("%s is a fmt template (%d verbs) with no entry in helpTemplateArgs — add its real arguments, or the lines measured here are not the lines a user sees",
					c.name, c.verbs)
			}
			text = fmt.Sprintf(text, args...)
		}
		maxWidth := 0
		for i, line := range strings.Split(text, "\n") {
			w := config.DisplayWidth(line)
			if w > maxWidth {
				maxWidth = w
			}
			if w > config.HelpWrapCol {
				t.Errorf("%s: %s line %d is %d display cols (> %d): %q",
					c.file, c.name, i+1, w, config.HelpWrapCol, line)
			}
		}
		t.Logf("%-22s lines=%3d maxWidth=%3d", c.name, strings.Count(text, "\n")+1, maxWidth)
	}
}

// TestHelpConstantsCarryNoCarelessText pins the CONTENT half of the epic: the
// fragments that made `gopi help` look unserious. Each entry is a defect that
// shipped once; the assertion is that its shape cannot come back.
func TestHelpConstantsCarryNoCarelessText(t *testing.T) {
	var all strings.Builder
	for _, c := range helpTextConsts(t) {
		all.WriteString(c.text)
		all.WriteString("\n")
	}
	text := all.String()
	for _, bad := range []struct {
		fragment string
		why      string
	}{
		{"run 'gopi task' or", "the attachment's dangling pointer standing in for content"},
		{"for the full family help", "the attachment's dangling pointer standing in for content"},
		{"Refused when >=3\n", "a sentence hand-broken mid-clause by an editor's line wrap"},
		{"--latent-crosscheck|--latent-calibrate", "a synopsis pipe-separated with no spaces (unreadable enum)"},
		{"/home/nick/", "a developer machine path leaking into user-facing help"},
	} {
		if strings.Contains(text, bad.fragment) {
			t.Errorf("help text still carries %q — %s", bad.fragment, bad.why)
		}
	}
	// Internal project jargon must not reach a user's terminal: M4.x milestone
	// numbers, bugNNN ids, P2.x phase ladders and epic ③ counters describe the
	// project's own roadmap, not the command's behaviour (the epic's "careless
	// text" complaint — a manual read as a commit log).
	for _, jarg := range []*regexp.Regexp{
		regexp.MustCompile(`M4\.\d+`),
		regexp.MustCompile(`bug\d{3}`),
		regexp.MustCompile(`P\d\.\d`),
		regexp.MustCompile(`epic [①-⑳]`),
	} {
		if m := jarg.FindString(text); m != "" {
			t.Errorf("help text carries internal roadmap jargon %q — describe the behaviour, not the milestone that shipped it", m)
		}
	}
	// Every MULTI-LINE help constant must be a raw string: a quoted literal
	// would hide its escapes from this sweep. Single-line constants (a usage
	// FINGERPRINT asserted by a contract test) legitimately use a plain quote.
	for _, c := range helpTextConsts(t) {
		if strings.Contains(c.text, "\n") && !c.raw {
			t.Errorf("%s: %s spans lines but is not a raw string — help text must be a backquoted literal so the format sweep measures what is printed", c.file, c.name)
		}
	}
}

// TestHelpWidthPredicateIsNotBlind is the red proof for the sweep above: the
// predicate must FLAG an over-budget line, or a green run proves nothing. It
// measures synthetic help text with the same function the sweep uses, plus the
// CJK case len() would miss (a wide glyph costs two columns).
func TestHelpWidthPredicateIsNotBlind(t *testing.T) {
	narrow := "  gopi x " + strings.Repeat("a", config.HelpWrapCol-9)
	if got := config.DisplayWidth(narrow); got != config.HelpWrapCol {
		t.Fatalf("predicate fixture: DisplayWidth = %d, want exactly the budget %d", got, config.HelpWrapCol)
	}
	cases := []struct {
		name string
		line string
		over bool
	}{
		{"exactly at budget", narrow, false},
		{"one byte over", narrow + "a", true},
		// 15 CJK glyphs = 30 display columns, but 45 bytes: a byte-counting
		// gate sees 45 and a rune-counting gate sees 15 — both under-measure
		// the terminal. The display measure sees the truth.
		{"CJK halves the visible budget", "  " + strings.Repeat("知", 44), true},
		{"CJK within budget", "  " + strings.Repeat("知", 43), false},
	}
	for _, tc := range cases {
		w := config.DisplayWidth(tc.line)
		if got := w > config.HelpWrapCol; got != tc.over {
			t.Errorf("%s: DisplayWidth = %d, over=%v, want over=%v", tc.name, w, got, tc.over)
		}
	}
}

// helpTextConst is one discovered help constant.
type helpTextConst struct {
	file  string
	name  string
	doc   string // the comment block directly above the declaration
	text  string
	raw   bool
	verbs int // count of fmt verbs in the literal (shotUsage is a template)
}

// helpTextConsts parses every non-test .go file of this package and returns the
// top-level string constants whose NAME says they are help text ("Usage"/"Help"
// — the naming convention every usage constant in this package already
// follows). Measuring the literals from source, not from a rendered binary,
// keeps the sweep dependency-free; the live binary is exercised by
// cli_contracts_test.go's -h fingerprints.
func helpTextConsts(t *testing.T) []helpTextConst {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package sources: %v", err)
	}
	fset := token.NewFileSet()
	var out []helpTextConst
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if !strings.Contains(name.Name, "Usage") && !strings.Contains(name.Name, "Help") {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					text, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: unquote %s: %v", f, name.Name, err)
					}
					doc := ""
					if vs.Doc != nil {
						doc = vs.Doc.Text()
					} else if gen.Doc != nil {
						doc = gen.Doc.Text()
					}
					out = append(out, helpTextConst{
						file:  f,
						name:  name.Name,
						doc:   doc,
						text:  text,
						raw:   strings.HasPrefix(lit.Value, "`"),
						verbs: countFormatVerbs(text),
					})
				}
			}
		}
	}
	return out
}

// countFormatVerbs counts the fmt conversion verbs in a help literal so the
// sweep can tell a static usage block from a template.
func countFormatVerbs(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 >= len(s) {
			continue
		}
		switch s[i+1] {
		case '%':
			i++
		case 's', 'd', 'v':
			n++
		}
	}
	return n
}

// helpTemplateArgs maps each templated help constant to the fmt arguments its
// PRODUCTION var passes — the same values, so the width measured here is the
// width printed. Keyed by constant name because a name is what the sweep has;
// a template with no entry fails the test (see the loop above).
var helpTemplateArgs = map[string][]any{
	"shotUsage": shotUsageTestArgs(),
	// The two search verbs' pages are rendered by codeSearchUsageText() /
	// keywordSearchUsageText(), which take their arguments from the SAME helper
	// the production renderer uses — so the width sweep measures the lines a
	// user's terminal receives, not a copy that could drift from them (the
	// sweep fails loudly on a template with no entry here).
	"codeSearchUsage":    codeSearchUsageArgs(),
	"keywordSearchUsage": keywordSearchUsageArgs(),
	// The merger atoms' pages are RENDERED by taskMergerAtomUsage (the exact
	// function the -h scan calls), so the arguments measured here are the ones
	// a user's terminal receives — not a copy that could drift from them.
	"taskMergeUsageHead":  mergerAtomUsageArgs(taskverbs.Merge),
	"mergerAtomUsageHead": mergerAtomUsageArgs(taskverbs.Adopt),
}

// mergerAtomUsageArgs returns the fmt arguments the production renderer passes
// for one merger atom. An empty list is returned when the name↔page mapping has
// gone blind; the sweep's own len check names that as a failure, so a broken
// render can never be measured as if it were help text (Rule 15).
func mergerAtomUsageArgs(verb string) []any {
	page, args := mergerAtomUsagePage(verb)
	if page == "" {
		return nil
	}
	return args
}

// shotUsageTestArgs are the fmt arguments shotUsage expects, kept next to the
// sweep so a new verb in that template fails loudly (a %!s(MISSING) line would
// otherwise be measured as text).
func shotUsageTestArgs() []any {
	return []any{1, 2, 3, 4}
}
