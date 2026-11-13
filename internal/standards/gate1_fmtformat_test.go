// Rule 16's constant format strings — red proofs, false-positive probes, and
// the undecidable channel (see gate1_fmtformat.go for the mechanism).
//
// WHY THESE TESTS EXIST. The predicate used to accept only a string literal, so
// a `fmt.Errorf(someConst, err)` site was skipped in silence: not reported, not
// counted, not listed anywhere. A gate that stops seeing a shape it once saw is
// indistinguishable from a clean tree — the probes below are what keep the
// widened predicate from rotting back into that state, and the undecidable arm
// is what keeps a shape it CANNOT see from disappearing again (PROJECT.md
// Rule 15 inside the gate).
package standardgate

import (
	"go/ast"
	"go/parser"
	"strings"
	"testing"
)

// gate1 returns Rule 16's gate, by identity rather than by a hard-coded index.
func gate1() gate {
	for _, g := range gates() {
		if g.id == gate1ID {
			return g
		}
	}
	return gates()[0]
}

// gate1ID names Rule 16's gate so the probes select it by identity.
const gate1ID = "Rule 16 (%w error wrapping)"

// probeHitsForGate1 runs Rule 16's predicate over an in-memory source.
func probeHitsForGate1(t *testing.T, src string) []hit {
	t.Helper()
	return probeHits(t, gate1(), src)
}

// TestGate1ConstantFormatRedProof is the RED PROOF for the widened predicate:
// a named constant carrying %v must be reported exactly as a literal one is.
// Before the widening this test could not fail — the predicate returned early on
// anything that was not an *ast.BasicLit.
func TestGate1ConstantFormatRedProof(t *testing.T) {
	const src = `package p

import "fmt"

const loadConfigFormat = "load config: %v"

func f(err error) error { return fmt.Errorf(loadConfigFormat, err) }
`
	hits := probeHitsForGate1(t, src)
	if len(hits) == 0 {
		t.Fatal("RED PROOF FAILED: a NAMED-CONSTANT percent-v wrap was not reported — " +
			"the predicate is blind to constant format strings again (it used to " +
			"silently return on anything that is not a *ast.BasicLit)")
	}
	if hits[0].line != probeConstFormatLine {
		t.Fatalf("RED PROOF: reported line %d, want %d", hits[0].line, probeConstFormatLine)
	}
}

// probeConstFormatLine is the line the constant-format call sits on in
// TestGate1ConstantFormatRedProof's source.
const probeConstFormatLine = 7

// TestGate1ConstantFormatConcatRedProof pins the other spelling of the same
// defect: a format built by `+` (how a long refusal message is usually written).
func TestGate1ConstantFormatConcatRedProof(t *testing.T) {
	const src = `package p

import "fmt"

func f(err error) error {
	return fmt.Errorf("load config: "+
		"cannot parse it: %v", err)
}
`
	if hits := probeHitsForGate1(t, src); len(hits) == 0 {
		t.Fatal("RED PROOF FAILED: a percent-v wrap in a CONCATENATED format string was not " +
			"reported — resolution must follow the `+` operator")
	}
}

// TestGate1ConstantFormatNoFalsePositive is the other half of the red proof: a
// constant that already wraps, or that renders a non-error value, must stay
// silent. A widened predicate that reports these would push the tree toward
// blanket escapes instead of fixed sites.
func TestGate1ConstantFormatNoFalsePositive(t *testing.T) {
	const wraps = `package p

import "fmt"

const loadConfigFormat = "load config: %w"

func f(err error) error { return fmt.Errorf(loadConfigFormat, err) }
`
	if hits := probeHitsForGate1(t, wraps); len(hits) != 0 {
		t.Fatalf("FALSE POSITIVE on a constant %%w wrap: %+v", hits)
	}

	const nonError = `package p

import "fmt"

const badCountFormat = "bad count: %v"

func f(n int) error { return fmt.Errorf(badCountFormat, n) }
`
	if hits := probeHitsForGate1(t, nonError); len(hits) != 0 {
		t.Fatalf("FALSE POSITIVE on a constant %%v that renders a NON-error: %+v", hits)
	}

	// A constant resolving to a string with no verb at all is formatting with
	// no wrapped value — nothing to report, and nothing to record either.
	const noVerb = `package p

import "fmt"

const plainFormat = "load config failed"

func f(err error) error { return fmt.Errorf(plainFormat, err) }
`
	if hits := probeHitsForGate1(t, noVerb); len(hits) != 0 {
		t.Fatalf("FALSE POSITIVE on a constant format with no verb: %+v", hits)
	}
}

// TestGate1VariableFormatIsUndecidable is the Rule 15 arm: a format string the
// analyser CANNOT resolve (a parameter, a struct field, a function call) must be
// RECORDED, not dropped. Before this channel existed such a site vanished from
// every number the gate printed — the silent failure this epic closes.
func TestGate1VariableFormatIsUndecidable(t *testing.T) {
	ForgetUndecidableFormats()
	defer ForgetUndecidableFormats()

	const src = `package p

import "fmt"

func f(format string, err error) error { return fmt.Errorf(format, err) }

func g(c config, err error) error { return fmt.Errorf(c.format, err) }

func h(err error) error { return fmt.Errorf(loadFormat(), err) }
`
	hits := probeHitsForGate1(t, src)
	if len(hits) != 0 {
		t.Fatalf("a format string the analyser cannot resolve must not be reported as a "+
			"violation (the verbs are not knowable), got %+v", hits)
	}
	got := UndecidableFormats()
	if len(got) != probeUndecidableSites {
		t.Fatalf("UNDECIDABLE CHANNEL: recorded %d site(s), want %d — an unresolvable "+
			"format string must be COUNTED, never silently skipped (Rule 15)\n  got: %v",
			len(got), probeUndecidableSites, got)
	}
	for _, want := range []string{"probe.go:5  format", "probe.go:7  c.format", "probe.go:9  loadFormat()"} {
		if !hasPrefix(got, want) {
			t.Errorf("UNDECIDABLE CHANNEL: %q is not reported as `%s`\n  got: %v", want, want, got)
		}
	}

	// The channel is DRAINED by a read: a second consumer must not see a stale
	// copy of the same sites (the gate runs once per test, and a growing
	// accumulate-forever list is the other silent failure).
	if again := UndecidableFormats(); len(again) != 0 {
		t.Fatalf("UNDECIDABLE CHANNEL: the second read returned %d site(s) — the list must "+
			"drain on read, got %v", len(again), again)
	}
}

// probeUndecidableSites is how many unresolvable calls the probe source above
// holds: a parameter, a field selector through a value, and a call.
const probeUndecidableSites = 3

// hasPrefix reports whether any entry starts with want (entries carry the
// rendered expression after the `rel:line` prefix).
func hasPrefix(entries []string, want string) bool {
	for _, e := range entries {
		if strings.HasPrefix(e, want) {
			return true
		}
	}
	return false
}

// TestFormatTextResolution pins the resolver itself: every shape it claims to
// follow, and the ones it must refuse instead of guessing.
func TestFormatTextResolution(t *testing.T) {
	const src = `package p

const (
	direct  = "load config: %v"
	alias   = direct
	nested  = alias
	concat  = "load " + "config: %v"
	raw     = ` + "`line1 %v`" + `
	notStr  = 42
)

func f() string { return direct }
`
	fset, f := parseSource(t, "probe.go", src)
	p := &parsed{rel: "probe.go", fset: fset, file: f, src: []byte(src)}
	cases := []struct {
		name string
		expr string
		want string
		ok   bool
	}{
		{"literal", `"load config: %v"`, "load config: %v", true},
		{"direct", "direct", "load config: %v", true},
		{"alias chain", "nested", "load config: %v", true},
		{"concatenation", "concat", "load config: %v", true},
		{"raw string", "raw", "line1 %v", true},
		{"non-string const", "notStr", "", false},
		{"unknown identifier", "nope", "", false},
		{"call", "f()", "", false},
		{"field", "c.format", "", false},
	}
	for _, c := range cases {
		got, ok := formatText(p, exprFromSource(t, p, c.expr), 0)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: formatText(%s) = (%q, %v), want (%q, %v)", c.name, c.expr, got, ok, c.want, c.ok)
		}
	}
}

// TestFormatTextDepthBound proves a cyclic const chain terminates as UNDECIDABLE
// rather than hanging the gate (a self-referential const does not compile, but a
// long legal chain must still stop at the named bound).
func TestFormatTextDepthBound(t *testing.T) {
	const src = `package p

const (
	a = b
	b = c
	c = d
	d = e
	e = "load: %v"
)
`
	fset, f := parseSource(t, "probe.go", src)
	p := &parsed{rel: "probe.go", fset: fset, file: f, src: []byte(src)}
	if got, ok := formatText(p, exprFromSource(t, p, "a"), 0); ok {
		t.Fatalf("a const chain deeper than maxFormatResolveDepth (%d) resolved to %q — the "+
			"bound must make it undecidable, not silently resolvable", maxFormatResolveDepth, got)
	}
}

// TestImportDirOfResolvesWithinTheModule proves the cross-package arm's ONE
// resolution step: an import path inside this module maps to its directory, an
// import from outside it maps to nothing (the site is then undecidable, never
// guessed).
func TestImportDirOfResolvesWithinTheModule(t *testing.T) {
	const src = `package p

import (
	"github.com/nick/gopi/internal/sitekey"
	"strings"
)

var _ = strings.TrimSpace
var _ = sitekey.MaxLineLen
`
	fset, f := parseSource(t, "internal/standards/probe.go", src)
	p := &parsed{rel: "internal/standards/probe.go", fset: fset, file: f, src: []byte(src)}
	got := importDirOf(p, "sitekey")
	if !strings.HasSuffix(got, "/internal/sitekey") {
		t.Fatalf("importDirOf(sitekey) = %q, want a path ending in /internal/sitekey", got)
	}
	if got := importDirOf(p, "strings"); got != "" {
		t.Fatalf("importDirOf(strings) = %q — an import OUTSIDE this module must resolve to "+
			"nothing (the site is then recorded as undecidable)", got)
	}
}

// exprFromSource parses one expression's source text on its own, so a table
// test can name a shape without hand-building an AST (which would test the
// builder, not the resolver).
func exprFromSource(t *testing.T, p *parsed, text string) ast.Expr {
	t.Helper()
	e, err := parser.ParseExprFrom(p.fset, "expr", text, 0)
	if err != nil {
		t.Fatalf("parse expression %q: %v", text, err)
	}
	return e
}

// TestGate1EveryFmtErrorfIsJudgedOrCounted is the CENSUS arm: over the whole
// production tree, every `fmt.Errorf` call that carries an argument must be
// RESOLVED to a format string, and the tree must actually CONTAIN the shape the
// predicate was widened for. Two claims, both falsifiable:
//
//  1. `constFmt > 0` — there really are fmt.Errorf sites whose first argument is
//     not a string literal on this tree. Without this floor the widening could
//     be dead code and every assertion below would still pass (a scan that found
//     nothing must never read as a clean scan — PROJECT.md Rule 38's lesson).
//  2. `undecidable == 0` — every one of them resolves through the AST alone.
//     This is the number stylegate cannot give: its go/types pass counts 599
//     undecidable sites tree-wide, and 11 of the real %v wraps were hidden
//     inside that mass. A regression in package-directory or import resolution
//     shows up here as a non-zero undecidable count, NAMED by the data report.
func TestGate1EveryFmtErrorfIsJudgedOrCounted(t *testing.T) {
	root := repoRoot(t)
	ForgetUndecidableFormats()
	defer ForgetUndecidableFormats()

	total, constFmt, resolved := 0, 0, 0
	for _, rel := range goFilesUnder(t, root, prodRoots, false) {
		p := parseRepoFile(t, root, rel)
		ast.Inspect(p.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isFmtErrorf(call) || len(call.Args) < 2 {
				return true
			}
			total++
			if _, isLit := call.Args[0].(*ast.BasicLit); !isLit {
				constFmt++
			}
			if _, ok := formatText(p, call.Args[0], 0); ok {
				resolved++
			}
			return true
		})
	}
	undecidable := CountUndecidableFormats()
	if constFmt == probeCensusConstFloor {
		t.Fatalf("CENSUS VACUOUS: not one fmt.Errorf on this tree has a non-literal format "+
			"string — the widening has no live subject, so nothing here proves it works")
	}
	if want := total - undecidable; resolved != want {
		t.Fatalf("CENSUS: %d fmt.Errorf call(s) with arguments — %d resolved by the census, "+
			"%d judged by the predicate (%d recorded undecidable) — the two disagree by %d",
			total, resolved, want, undecidable, resolved-want)
	}
	if undecidable != 0 {
		t.Fatalf("CENSUS: %d fmt.Errorf call(s) the analyser cannot resolve; every site on this "+
			"tree is expected to resolve (see the data report for their names). Each one is a site "+
			"Rule 16 cannot check — the shape that hid 11 real %%v wraps.", undecidable)
	}
	t.Logf("CENSUS: %d fmt.Errorf call(s) with arguments — %d resolved (of which %d are "+
		"CONSTANT format strings, the shape the old predicate skipped), 0 undecidable",
		total, resolved, constFmt)
}

// probeCensusConstFloor is the number of non-literal format strings below which
// the census is declared vacuous. It is a FLOOR, not an exact count: the tree
// grows, and the claim being made is "the widened shape is populated", not
// "the tree has exactly N of them".
const probeCensusConstFloor = 0
