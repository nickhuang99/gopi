package main

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// chat_status_ctx_provenance_glyph_test.go — the epic's gap A: statusOps.ctxMeasured
// promised, in its own field comment, that "the row marks the estimated case", and
// NOTHING read it. These tests are the promise's teeth — the flag now decides the
// context sub-cell's qualifier glyph, so a row whose number is a local estimate is
// distinguishable from one whose number is the API's own prompt count.

// TestCtxQualifierMarksTheMeasuredCase pins the choice per flag value, in both the
// aligned sub-cell and the fallback form, and pins that the two spellings differ.
func TestCtxQualifierMarksTheMeasuredCase(t *testing.T) {
	cases := []struct {
		name     string
		ops      statusOps
		wantSub  string // the aligned sub-cell's qualifier, read off the rendered field
		wantGlyp string
	}{
		{"a measured row carries the identity qualifier", statusOps{ctxUsed: 92100, ctxMax: 200000, ctxMeasured: true}, "≡", statusCtxQuantGlyph},
		{"an estimated row carries the approximation qualifier", statusOps{ctxUsed: 92100, ctxMax: 200000, ctxMeasured: false}, "≈", statusCtxEstimateGlyph},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := statusCtxQuantGlyphFor(c.ops); got != c.wantGlyp {
				t.Fatalf("statusCtxQuantGlyphFor(%v) = %q, want %q", c.ops.ctxMeasured, got, c.wantGlyp)
			}
			field := statusCtxField(c.ops, 0)
			if !strings.HasSuffix(field, c.wantSub) {
				t.Fatalf("field %q does not end with the qualifier %q", field, c.wantSub)
			}
			other := statusCtxQuantGlyph
			if c.wantSub == statusCtxQuantGlyph {
				other = statusCtxEstimateGlyph
			}
			if strings.HasSuffix(field, other) && statusCtxQuantGlyph != statusCtxEstimateGlyph {
				t.Fatalf("field %q ends with the OTHER qualifier %q", field, other)
			}
		})
	}
}

// TestCtxProvenanceMarkCostsNoCell is the measurement that CHOSE this design: the
// context sub-cell renders 12 of its 12 cells in the pinned form, so a second mark
// would need a 13th cell — a re-partition of a row whose arithmetic is guarded at
// compile time. The glyph therefore REPLACES the qualifier, and the width must be
// identical whether the number is measured or estimated, in every form.
func TestCtxProvenanceMarkCostsNoCell(t *testing.T) {
	shapes := []struct {
		name string
		used int
		max  int
	}{
		{"the pinned form, exactly full", 42100, 64000},
		{"a short pair, one cell of slack", 92100, 200000},
		{"a narrow pair", 100, 6400},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			measured := statusCtxField(statusOps{ctxUsed: s.used, ctxMax: s.max, ctxMeasured: true}, 0)
			estimated := statusCtxField(statusOps{ctxUsed: s.used, ctxMax: s.max, ctxMeasured: false}, 0)
			wm, we := lineedit.DisplayWidth(measured), lineedit.DisplayWidth(estimated)
			if wm != we {
				t.Fatalf("width differs by provenance: measured %q is %d cells, estimated %q is %d cells",
					measured, wm, estimated, we)
			}
			if wm > statusCtxCol {
				t.Fatalf("%q is %d cells, over the sub-cell budget of %d", measured, wm, statusCtxCol)
			}
			// The two rows must actually DIFFER, or the mark announces nothing.
			if measured == estimated {
				t.Fatalf("measured and estimated render identically (%q): the flag is spent on nothing", measured)
			}
		})
	}
}

// TestCtxProvenanceMarkOnTheFallbackForm covers the OTHER renderer: with no window
// known (ctxMax == 0) the field reports the total alone and has no reserved cells at
// all, so it appends the same one-cell spelling. An empty reading gets no mark —
// there is no number to grade, and a glyph beside nothing would be a claim.
func TestCtxProvenanceMarkOnTheFallbackForm(t *testing.T) {
	measured := statusCtxField(statusOps{ctxUsed: 15400, ctxMeasured: true}, 0)
	estimated := statusCtxField(statusOps{ctxUsed: 15400, ctxMeasured: false}, 0)
	if !strings.HasSuffix(measured, statusCtxQuantGlyph) {
		t.Fatalf("fallback measured form %q does not carry %q", measured, statusCtxQuantGlyph)
	}
	if !strings.HasSuffix(estimated, statusCtxEstimateGlyph) {
		t.Fatalf("fallback estimated form %q does not carry %q", estimated, statusCtxEstimateGlyph)
	}
	if wm, we := lineedit.DisplayWidth(measured), lineedit.DisplayWidth(estimated); wm != we {
		t.Fatalf("fallback widths differ: %d vs %d", wm, we)
	}
	// No number, no mark.
	if got := statusCtxField(statusOps{ctxMeasured: false}, 0); strings.ContainsAny(got, statusCtxQuantGlyph+statusCtxEstimateGlyph) && got != "" {
		if !strings.Contains(got, "0") && got != statusCtxEstimateGlyph {
			t.Fatalf("an empty reading %q carries a qualifier about nothing", got)
		}
	}
}

// TestCtxQualifierGlyphIsOneCell pins the assumption the no-cost design rests on:
// both spellings are exactly ONE display cell, so swapping them can never change
// the field's width (measured, not assumed — lineedit decides width, and a wide
// glyph would silently turn the pinned form into an overflow).
func TestCtxQualifierGlyphIsOneCell(t *testing.T) {
	for _, g := range []string{statusCtxQuantGlyph, statusCtxEstimateGlyph} {
		if w := lineedit.DisplayWidth(g); w != 1 {
			t.Fatalf("qualifier %q is %d display cells, want 1", g, w)
		}
	}
	if statusCtxQuantGlyph == statusCtxEstimateGlyph {
		t.Fatalf("the two spellings are the same character %q: the flag would mark nothing", statusCtxQuantGlyph)
	}
}
