package taskverdict

// Tests for the reframe verdict vocabulary. Anti-drift (Rules 1b/13): the
// verdict values are NEVER typed as literals here — every expectation is built
// from the package's own constants, so a rename propagates instead of leaving
// a passing-but-wrong test behind. The one gate test that must reason about the
// literal strings derives them from All() for the same reason.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test-local named constants (Rule 1: no bare string keywords / magic values).
// Each one is a value the test asserts on or injects; naming them keeps the
// intent visible and the injection points greppable.
const (
	// enumSeparator is the join character internal/agent uses to render All()
	// as the task tool's verdict enum description (toolnamesVerdictEnum).
	enumSeparator = "|"

	// gateDefinitionFile is the ONE production file allowed to carry the
	// verdict literals (their single definition site).
	gateDefinitionFile = "verdict.go"

	// retiredResearchVerdict is the dropped fourth verdict (SPEC §3: research
	// is a roadmap ITEM, not an exit) — the gate must still reject it.
	retiredResearchVerdict = "NEEDS_RESEARCH"

	// telemetryUnknownBucket mirrors the consumer's malformed-detail bucket
	// (reframeVerdictUnknown, internal/tools/reframe.go — unexported there, so
	// it cannot be imported). It is a RENDER-ONLY bucket, never a wire value:
	// if Valid() accepted it, an LLM sending it would bypass the vocab check and
	// fall into the dispatch switch's default branch — i.e. the gate would mint
	// a fabricated exit verdict out of a telemetry token. Rejecting it is what
	// keeps the bucket telemetry-only. (Named without the verdict literal on
	// purpose: Rule 1b — this file carries no verdict string literals at all.)
	telemetryUnknownBucket = "UNKNOWN"

	// callerMutation / callerAppendedValue are the injected mutations of the
	// TestAll_ReturnsAFreshSlice boundary test: they never collide with a real
	// vocabulary entry (which is why the assertions below stay meaningful).
	callerMutation      = "MUTATED-BY-CALLER"
	callerAppendedValue = "EXTRA"

	// wantVocabularyLen is the closed-vocabulary size (SPEC §3: three
	// verdicts, no fourth).
	wantVocabularyLen = 3
)

// TestAll_ReturnsFullVocabularyInSchemaOrder pins the vocabulary contents AND
// its order: the order is part of the wire contract (the tool schema's enum
// description and the distribution report both render All() in slice order), so
// a reordering is a behaviour change, not a cosmetic one.
func TestAll_ReturnsFullVocabularyInSchemaOrder(t *testing.T) {
	got := All()
	want := []string{Doable, TooBig, Impossible}

	if len(got) != len(want) {
		t.Fatalf("All() returned %d verdicts, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("All()[%d] = %q, want %q (schema/enum order is the wire contract)", i, got[i], want[i])
		}
	}
}

// TestAll_VocabularyIsClosedAndClean guards the "THREE verdicts, no fourth"
// invariant (SPEC §3) plus the hygiene properties the harness gate relies on:
// no duplicate, no empty and no whitespace-padded entry. A padded entry would
// make Valid() refuse the very string All() advertises, which is the
// self-contradiction this test exists to catch.
func TestAll_VocabularyIsClosedAndClean(t *testing.T) {
	all := All()

	if len(all) != wantVocabularyLen {
		t.Errorf("the reframe vocabulary must have exactly %d verdicts (SPEC §3: no fourth), got %d: %v", wantVocabularyLen, len(all), all)
	}

	seen := make(map[string]bool, len(all))
	for _, v := range all {
		if v == "" {
			t.Error("All() contains an empty verdict — the harness gate rejects empty, so the schema must never advertise it")
		}
		if v != strings.TrimSpace(v) {
			t.Errorf("All() entry %q is whitespace-padded — Valid() would reject the advertised value", v)
		}
		if strings.ContainsAny(v, "|") {
			t.Errorf("All() entry %q contains the enum separator %q — it would split into two schema enum options", v, "|")
		}
		if seen[v] {
			t.Errorf("All() lists %q twice", v)
		}
		seen[v] = true
	}
}

// TestAll_ReturnsAFreshSlice is a boundary test on the returned value's
// aliasing: All() must hand out a new slice per call, so a caller that sorts or
// mutates it (the distribution report sorts by popularity — reframe.go) cannot
// corrupt the vocabulary for the next caller in the same process.
func TestAll_ReturnsAFreshSlice(t *testing.T) {
	first := All()
	if len(first) == 0 {
		t.Fatal("All() returned nothing")
	}
	original := first[0]
	first[0] = callerMutation
	first = append(first, callerAppendedValue)

	second := All()
	if second[0] != original {
		t.Errorf("All() shares its backing array: caller mutation changed the next call to %q, want %q", second[0], original)
	}
	if len(second) != wantVocabularyLen {
		t.Errorf("All() returned %d verdicts after a caller appended, want %d — the slice must be fresh per call", len(second), wantVocabularyLen)
	}
}

// TestValid_AcceptsEveryVocabularyEntry is the happy path: the harness validation
// gate must accept every value the tool schema advertises. Driven by All() so a
// new constant is automatically covered.
func TestValid_AcceptsEveryVocabularyEntry(t *testing.T) {
	for _, v := range All() {
		if !Valid(v) {
			t.Errorf("Valid(%q) = false, but All() advertises it — schema and gate disagree", v)
		}
	}
}

// TestValid_RejectsNonVocabulary is the error/invalid-input path and the heart
// of the gate: an empty or unknown verdict is REJECTED, never silently
// defaulted (Rule 15 — a dropped decision is a silent failure). The cases below
// are the realistic ways an LLM (or a hand-written config) gets it wrong:
// empty, missing, lowercase, TitleCase, padded, separator-mangled, dropped
// legacy value, prefix, and a compound value.
func TestValid_RejectsNonVocabulary(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace only", " "},
		{"tab and newline", "\t\n"},
		{"lowercase", strings.ToLower(Doable)},
		{"lowercase too-big", strings.ToLower(TooBig)},
		{"title case", strings.ToUpper(Doable[:1]) + strings.ToLower(Doable[1:])},
		{"trailing space", Doable + " "},
		{"leading space", " " + Doable},
		{"surrounding spaces", " " + TooBig + " "},
		{"trailing newline", Impossible + "\n"},
		{"separator-mangled", Doable + "|" + TooBig},
		{"space-mangled too-big", strings.ReplaceAll(TooBig, "_", " ")},
		{"underscore-stripped too-big", strings.ReplaceAll(TooBig, "_", "")},
		{"prefix only", Doable[:2]},
		{"prefix of impossible", Impossible[:len(Impossible)-1]},
		{"superset", Doable + Doable},
		{"dropped legacy research verdict", retiredResearchVerdict},
		{"telemetry unknown bucket", telemetryUnknownBucket},
		{"unknown word", "MAYBE"},
		{"review code", "A"},
		{"numeric string", "3"},
		{"null-ish", "null"},
		{"unicode lookalike", strings.ToUpper("ｄｏａｂｌｅ")},
		{"zero byte", Doable + "\x00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if Valid(tc.in) {
				t.Errorf("Valid(%q) = true, want false — the harness gate must reject anything outside All() %v", tc.in, All())
			}
		})
	}
}

// TestValid_RejectionIsExhaustiveOverTrimmedAll re-states the gate as a
// property: no whitespace-trimmed form of a valid verdict is accepted, i.e.
// Valid() is an exact match and never trims. Combined with the accept test this
// makes the gate's contract two-sided.
func TestValid_RejectionIsExhaustiveOverTrimmedAll(t *testing.T) {
	for _, v := range All() {
		padded := " " + v + "\t"
		if Valid(padded) {
			t.Errorf("Valid(%q) = true — the gate trims, so %q would be accepted too; validation must be exact", padded, padded)
		}
	}
}

// TestWireContract_EnumDescriptionOrder is the coupling test with the consumer:
// internal/agent renders the task tool's verdict enum description by joining
// All() with "|" (toolnamesVerdictEnum, internal/agent/loop.go) and
// internal/agent/reframe.go renders the human-facing list with " | ". Both
// depend on THIS order. The expectation is assembled from the constants, so it
// pins order without a literal.
func TestWireContract_EnumDescriptionOrder(t *testing.T) {
	want := strings.Join([]string{Doable, TooBig, Impossible}, enumSeparator)

	got := strings.Join(All(), enumSeparator)
	if got != want {
		t.Errorf("schema enum description = %q, want %q — the LLM is taught this exact string", got, want)
	}
	if Valid(want) {
		t.Errorf("Valid(%q) = true — the joined enum must never be accepted as a single verdict", want)
	}
}

// TestGate_PackageSourcesCarryNoVerdictLiterals is the Rule 13 gate for this
// package: the verdict literals may live ONLY at their single definition site
// (verdict.go, the constants). Any other production file in the package that
// re-types a value would drift the moment the constant is renamed. The literals
// are derived from All(), so this gate itself carries none.
func TestGate_PackageSourcesCarryNoVerdictLiterals(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == gateDefinitionFile {
			continue
		}
		checked++
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, literal := range All() {
			if strings.Contains(string(data), literal) {
				t.Errorf("%s re-types the verdict literal %q — reference the constant in %s instead", name, literal, gateDefinitionFile)
			}
		}
	}
	if checked == 0 {
		t.Logf("no production file besides %s in this package — gate is vacuously green", gateDefinitionFile)
	}
}

// TestGate_VocabularyStaysMinimal pins the second half of the closed-vocabulary
// invariant against API drift: the package exports exactly the two accessor
// functions All and Valid (plus the three constants). Adding an exported func
// here widens the harness's vocabulary surface, which is a SPEC-level decision
// — this test makes it a deliberate, visible edit rather than an accident.
func TestGate_VocabularyStaysMinimal(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, gateDefinitionFile, nil, 0)
	if err != nil {
		t.Fatalf("parse verdict.go: %v", err)
	}

	const wantAll, wantValid = "All", "Valid"
	funcs := make(map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.IsExported() {
			funcs[fn.Name.Name] = true
		}
		return true
	})

	if !funcs[wantAll] || !funcs[wantValid] {
		t.Errorf("%s must define both %s and %s, found %v", gateDefinitionFile, wantAll, wantValid, funcs)
	}
	if len(funcs) != 2 {
		t.Errorf("internal/taskverdict exports %d functions (%v), want exactly %s and %s — a wider surface means the verdict vocabulary leaked beyond All/Valid", len(funcs), funcs, wantAll, wantValid)
	}
}
