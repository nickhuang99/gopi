package lineedit

// capability_measure_test.go — the epic's rule at the level where it is DECIDED:
// a terminal's own answer outranks the environment's declaration, and a probe
// that read nothing leaves the declaration standing.
//
// WHY IN THIS PACKAGE. The precedence lives in Capability.WithMeasurement /
// evidenceDepth, whose fields are unexported (the whole point: no surface may
// decide for itself which evidence to trust). A test in another package could
// only assert the exported verdict, never that the DECLARATION survived being
// overruled — and "the claim that lost is still readable" is exactly what a
// capability record needs.

import (
	"strings"
	"testing"
)

// testCapability resolves the capability an environment describes, with colour
// forced ON (the field-level entry point: these tests are about the DEPTH
// decision, and routing them through a real terminal would test the sink
// instead).
func testCapability(term, colorTerm string) Capability {
	c := Capability{colour: ColourOn}
	if strings.EqualFold(colorTerm, ColorTermTruecolor) {
		c.declaredTruecolor = true
	}
	if strings.Contains(strings.ToLower(term), Term256Color) {
		c.declaredPalette256 = true
	}
	c.declaredDepth = declaredTermDepth(c)
	return c
}

// TestMeasurementOutranksDeclarationBothWays is the epic, asserted in the two
// directions that matter: a truecolor DECLARATION must fall to a terminal that
// answers 16 colours, and a 256-colour DECLARATION must rise to a terminal that
// answers `RGB=1`. Both directions, because a rule implemented as "measurement
// wins" by taking the MINIMUM would pass one and fail the other.
func TestMeasurementOutranksDeclarationBothWays(t *testing.T) {
	// Direction 1: declared truecolor, measured 256 (no RGB capability).
	declaredTrue := testCapability("xterm-256color", ColorTermTruecolor)
	measured256 := declaredTrue.WithMeasurement(CapabilityMeasurement{
		Colors: colorEntries256, CursorPositionReport: true, Bytes: 24,
	})
	if got := measured256.Depth(); got != Depth256 {
		t.Errorf("a truecolor declaration against a terminal answering colors=256 decided %s, want 256", DepthName(got))
	}
	if measured256.Truecolor() {
		t.Error("a terminal that answered colors=256 (and refused nothing) is still reported as truecolor")
	}
	if got := measured256.DepthSource(); got != DepthSourceMeasured {
		t.Errorf("DepthSource() = %q, want %q", got, DepthSourceMeasured)
	}
	// The loser is still quoted: a record that dropped it could not answer
	// "which of the two did the run use?".
	if !measured256.DeclaredTruecolor() {
		t.Error("the truecolor declaration was forgotten once it was overruled")
	}

	// Direction 2: declared 16 colours, measured 24-bit.
	declaredBasic := testCapability("xterm", "")
	measuredTrue := declaredBasic.WithMeasurement(CapabilityMeasurement{
		RGB: true, RGBKnown: true, CursorPositionReport: true, Bytes: 20,
	})
	if got := measuredTrue.Depth(); got != DepthTruecolor {
		t.Errorf("a terminal answering RGB=1 under a 16-colour declaration decided %s, want truecolor", DepthName(got))
	}
	if declaredBasic.Depth() != DepthBasic {
		t.Errorf("the unmeasured capability changed: %s", DepthName(declaredBasic.Depth()))
	}
}

// TestASilentProbeLeavesTheDeclarationStanding is the fallback the whole design
// promises: a measurement of NOTHING must not become a measurement of "basic".
func TestASilentProbeLeavesTheDeclarationStanding(t *testing.T) {
	declared := testCapability("xterm-256color", ColorTermTruecolor)
	silent := declared.WithMeasurement(CapabilityMeasurement{})
	if got := silent.Depth(); got != DepthTruecolor {
		t.Errorf("a probe that read nothing decided %s, want the declared truecolor", DepthName(got))
	}
	if got := silent.DepthSource(); got != DepthSourceDeclared {
		t.Errorf("DepthSource() = %q, want %q — nothing was measured, and the record must say so", got, DepthSourceDeclared)
	}
	if silent.Measurement().Measured() {
		t.Error("an empty measurement reports itself as Measured()")
	}
}

// TestLivenessAloneIsNotADepthMeasurement pins the boundary that keeps a
// measurement from displacing a declaration on no evidence: a terminal that
// answered only the cursor-position query has told us it EXISTS, not how deep it
// is — so the declared depth must still decide.
func TestLivenessAloneIsNotADepthMeasurement(t *testing.T) {
	declared := testCapability("xterm-256color", "")
	answered := declared.WithMeasurement(CapabilityMeasurement{
		CursorPositionReport: true, Answered: true, Bytes: 8,
	})
	if got := answered.DepthSource(); got != DepthSourceDeclared {
		t.Errorf("DepthSource() = %q after a liveness answer alone, want %q", got, DepthSourceDeclared)
	}
	if got := answered.Depth(); got != Depth256 {
		t.Errorf("a liveness answer alone decided %s, want the declared 256", DepthName(got))
	}
}

// TestCapabilityNoticeNamesBothSidesOnADisagreement pins the operator-facing
// sentence: it fires on a disagreement and names BOTH tiers, so the reader can
// see which one their terminal actually is.
func TestCapabilityNoticeNamesBothSidesOnADisagreement(t *testing.T) {
	declared := testCapability("xterm-256color", ColorTermTruecolor)
	measured := declared.WithMeasurement(CapabilityMeasurement{
		Colors: 16, CursorPositionReport: true, Bytes: 16,
	})
	notice := CapabilityNotice(measured)
	if notice == "" {
		t.Fatal("a terminal measured at 16 colours under a truecolor declaration produced no notice")
	}
	for _, want := range []string{DepthNameBasic, DepthNameTruecolor} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not name %q: %q", want, notice)
		}
	}
	if got := CapabilityNotice(declared); got != "" {
		t.Errorf("an unmeasured capability produced a notice: %q", got)
	}
	// Agreement is not announced: the screen matches what the environment said.
	agree := testCapability("xterm-256color", "")
	agreed := agree.WithMeasurement(CapabilityMeasurement{
		Colors: colorEntries256, CursorPositionReport: true, Bytes: 20,
	})
	if got := CapabilityNotice(agreed); got != "" {
		t.Errorf("agreement produced a notice: %q", got)
	}
}

// TestCapabilityReportQuotesBothEvidences pins the record's decision line: the
// source, the decided tier and the CLAIM that lost, in one line, with the probe
// summary beside it when one was made.
func TestCapabilityReportQuotesBothEvidences(t *testing.T) {
	declared := testCapability("xterm-256color", ColorTermTruecolor)
	measured := declared.WithMeasurement(CapabilityMeasurement{
		Colors: 16, CursorPositionReport: true, Bytes: 16,
	})
	lines := CapabilityReport(measured)
	if len(lines) != 2 {
		t.Fatalf("CapabilityReport returned %d line(s), want a decision line and a probe line: %q", len(lines), lines)
	}
	decision := lines[0]
	for _, want := range []string{
		CapabilityDecisionField + "=" + DepthSourceMeasured,
		"depth=" + DepthNameBasic,
		"declared_truecolor=on",
	} {
		if !strings.Contains(decision, want) {
			t.Errorf("the decision line does not carry %q: %q", want, decision)
		}
	}
	if !strings.Contains(lines[1], "colors=16") {
		t.Errorf("the probe line does not carry the measuring evidence: %q", lines[1])
	}
	// A capability with no measurement reports ONE line: a probe line that said
	// "nothing" would be indistinguishable from a probe that ran and was
	// ignored.
	if got := CapabilityReport(declared); len(got) != 1 {
		t.Errorf("an unmeasured capability reported %d line(s), want only the decision: %q", len(got), got)
	}
}
