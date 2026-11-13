package memoryprefetch

// inject_floor_test.go — the INJECTION FLOOR: fewer than Config.MinInjectHits
// hits must inject NOTHING AT ALL (not the hits, not the header), and a
// retrieval at or above the floor must inject EXACTLY what it injected before
// the floor existed.
//
// The three cases below are the acceptance criterion, driven against the REAL
// InjectSection (Rule 31) rather than against a copy of its logic:
//
//	1 hit  -> out is returned untouched, byte for byte, and carries no header;
//	2 hits -> injected (the floor's boundary is inclusive, not exclusive);
//	3 hits -> byte-identical to the pre-floor rendering (out + separator +
//	          RenderSegment), so the floor cannot change what a passing
//	          retrieval looks like.

import (
	"strings"
	"testing"
)

// floorHits builds n renderable hits. Each carries a body long enough to survive
// MinHitRunes, so "nothing was injected" can never be an artefact of the trim
// having emptied the section (that is a DIFFERENT refusal, already pinned by
// TestInjectSectionJoinsOnlyARealSection).
func floorHits(n int) []Hit {
	hits := make([]Hit, 0, n)
	for i := 1; i <= n; i++ {
		hits = append(hits, Hit{
			ID:         i,
			Topic:      "prefetch",
			Question:   "does the floor drop this?",
			Answer:     fillAnswer(60),
			Tier:       tierWarm,
			Confidence: 0.9,
		})
	}
	return hits
}

// TestInjectSectionFloorDropsOneHitEntirely is the epic's primary case: ONE hit
// injects zero bytes. The assertion is TWO-SIDED on purpose — the returned
// message equals the input, AND the input does not appear with a header
// attached — because "returned the same string" would also be satisfied by a
// renderer that emitted the header into a discarded buffer, while the reader's
// cost is the header line, not the variable that held it.
func TestInjectSectionFloorDropsOneHitEntirely(t *testing.T) {
	const out = "what does the prefetch floor do?"
	joined, ok := InjectSection(out, floorHits(1), DefaultConfig())
	if ok {
		t.Errorf("a one-hit retrieval was injected: %q", joined)
	}
	if joined != out {
		t.Errorf("the message changed with nothing to inject:\n%q\nwant:\n%q", joined, out)
	}
	if strings.Contains(joined, sectionHeaderPrefix) {
		t.Errorf("the refused retrieval still wrote a section header: %q", joined)
	}
}

// TestInjectSectionFloorBoundaryIsInclusive pins the boundary FROM BOTH SIDES:
// the floor (2) injects, and the value below it (1) does not. A floor that was
// accidentally exclusive — "more than 2" for a floor of 2 — would pass the
// one-hit case above and silently drop every two-hit retrieval, which is the
// shape the operator's own corpus carries most of after the one-hit class.
func TestInjectSectionFloorBoundaryIsInclusive(t *testing.T) {
	const out = "the boundary question"
	cfg := DefaultConfig()
	if cfg.MinInjectHits != 2 {
		t.Fatalf("DefaultMinInjectHits = %d, want 2 — the floor's own default moved", cfg.MinInjectHits)
	}
	if _, ok := InjectSection(out, floorHits(cfg.MinInjectHits-1), cfg); ok {
		t.Errorf("a retrieval of %d hit(s) was injected with a floor of %d", cfg.MinInjectHits-1, cfg.MinInjectHits)
	}
	joined, ok := InjectSection(out, floorHits(cfg.MinInjectHits), cfg)
	if !ok {
		t.Fatalf("a retrieval exactly AT the floor (%d hits) was refused", cfg.MinInjectHits)
	}
	if !strings.HasPrefix(joined, out+sectionSeparator) {
		t.Errorf("the accepted section is not a separate block:\n%q", joined)
	}
	if !strings.Contains(joined, sectionHeaderPrefix) {
		t.Errorf("the accepted section carries no header:\n%q", joined)
	}
}

// TestInjectSectionAboveTheFloorIsUnchanged is the "no other behaviour moved"
// half, and it is asserted as BYTE IDENTITY against the renderer the floor does
// not touch: what a 3-hit retrieval injects today must equal what it injected
// before the floor existed, which is exactly out + separator + RenderSegment's
// own text. Comparing the two renderings (rather than re-typing the expected
// string) is what makes this a regression guard instead of a second definition.
func TestInjectSectionAboveTheFloorIsUnchanged(t *testing.T) {
	const out = "compare the two renderings"
	hits := floorHits(3)
	joined, ok := InjectSection(out, hits, DefaultConfig())
	if !ok {
		t.Fatal("a three-hit retrieval was refused by the floor")
	}
	want := out + sectionSeparator + RenderSegment(hits, nil, DefaultConfig()).Text
	if joined != want {
		t.Errorf("the injected form changed above the floor:\ngot:\n%q\nwant:\n%q", joined, want)
	}
	// The hits themselves are untouched: the floor decides whether to inject,
	// never WHAT to inject.
	seg := RenderSegment(hits, nil, DefaultConfig())
	for _, rh := range seg.Hits {
		if !strings.Contains(joined, rh.Line) {
			t.Errorf("hit %s is missing from the injected section", rh.Source.Record)
		}
	}
}

// TestInjectFloorCannotBeLowered is the POLICY half: the floor is
// framework-level, so (a) a caller that asks for less is REFUSED BY NAME
// (Rule 15 — never a silent correction), and (b) a caller that never asks for a
// refusal cannot get the weaker behaviour anyway, because the clamp at every
// entry point is the floor itself. Both halves are here because either alone
// leaves a door open: (a) without (b) is advice, (b) without (a) is silence.
func TestInjectFloorCannotBeLowered(t *testing.T) {
	if err := (Config{MinInjectHits: DefaultMinInjectHits - 1}).Validate(); err == nil {
		t.Error("a config below the framework floor validated — the floor is configurable downward in fact")
	} else if !strings.Contains(err.Error(), "MinInjectHits") {
		t.Errorf("the refusal does not name the field: %v", err)
	}
	// A config ABOVE the floor is legal and takes effect.
	if err := (Config{MinInjectHits: DefaultMinInjectHits + 3}).Validate(); err != nil {
		t.Errorf("raising the floor was refused: %v", err)
	}
	raised := Config{MinInjectHits: DefaultMinInjectHits + 3}
	if _, ok := InjectSection("x", floorHits(DefaultMinInjectHits), raised); ok {
		t.Errorf("a raised floor (%d) still injected %d hit(s)", raised.MinInjectHits, DefaultMinInjectHits)
	}

	// And the door is shut even for a caller that skips Validate: the entry
	// point clamps UP, so the weaker behaviour is unreachable by construction.
	defiant := Config{MinInjectHits: 1}
	joined, ok := InjectSection("the defiant caller", floorHits(1), defiant)
	if ok {
		t.Errorf("a hand-built config lowered the floor: %q", joined)
	}
	if joined != "the defiant caller" {
		t.Errorf("the message changed: %q", joined)
	}
}
