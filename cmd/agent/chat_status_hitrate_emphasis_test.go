package main

import (
	"math"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestStatusHitRateEmphasis_TierBoundaries pins the producer's thresholds at
// their EDGES: a boundary that is off by one is invisible on a real screen (the
// rate moves a tenth of a percent at a time), so the tiers are asserted on the
// exact reading each floor names, plus one PRINTED STEP on either side of it —
// the step is the form's own resolution now that the row carries a fraction
// (statusHitRateDecimals), not the whole percent it used to print.
func TestStatusHitRateEmphasis_TierBoundaries(t *testing.T) {
	// A hit rate of `tenths` tenths-of-a-percent is composed from counters whose
	// SUM is a round 1000, so the division lands exactly on the reading the case
	// names (no rounding surprise in the fixture).
	countsFor := func(tenths int) statusOps {
		return statusOps{cacheRead: tenths * 10, cacheWrite: (1000 - tenths) * 10}
	}
	// The floors are whole percents (they are the producer's judgement), so the
	// printed step below a floor is one tenth under it.
	step := 1
	good, fair := statusHitRateGoodFloorPct*10, statusHitRateFairFloorPct*10
	cases := []struct {
		name string
		ops  statusOps
		want lineedit.Style
	}{
		{"nothing served", countsFor(0), lineedit.BasicPalette.StatusBad},
		{"one step under the fair floor", countsFor(fair - step), lineedit.BasicPalette.StatusBad},
		{"the fair floor itself", countsFor(fair), lineedit.BasicPalette.StatusFair},
		{"one step under the good floor", countsFor(good - step), lineedit.BasicPalette.StatusFair},
		{"the good floor itself", countsFor(good), lineedit.BasicPalette.StatusGood},
		{"everything served", countsFor(1000), lineedit.BasicPalette.StatusGood},
		// The session the epic was filed from: 111104 of 112473 prompt tokens.
		{"the operator's own session", statusOps{cacheRead: 111104, cacheWrite: 1369},
			lineedit.BasicPalette.StatusGood},
	}
	for _, tc := range cases {
		span, style := statusHitRateEmphasis(tc.ops, lineedit.BasicPalette)
		if style != tc.want {
			t.Errorf("%s: tier = %q, want the tier of %q (floors %d/%d)",
				tc.name, style.Open(), tc.want.Open(),
				statusHitRateFairFloorPct, statusHitRateGoodFloorPct)
		}
		// THE SPAN IS THE READING ITSELF, and it must be a substring of the
		// metrics column's own text — i.e. of the field the producer composed. An
		// emphasis whose span is not in the field is a silent no-op, which is the
		// defect this whole channel exists to remove (Rule 15).
		//
		// The carrier is the CACHE sub-cell's read-out since task 01a0f8c9: the rate
		// rides that value ("↑12k↓4k hit 74.5%") rather than leading the context
		// sub-cell, so the field the span must be found in is that sub-cell — and the
		// whole metrics field contains it as one of its parts.
		field := statusCacheField(tc.ops)
		if !strings.Contains(field, span) {
			t.Errorf("%s: the emphasised span %q is not in the row's own metrics field %q",
				tc.name, span, field)
		}
		// ... and the span the COLUMN carries is a substring of that same text and
		// wears that same tier: the full read-out where the sub-cell composed it
		// whole (the measurement, not a fragment of it), the bare percentage where a
		// narrow geometry shed the name. Either way the emphasis is on cells the row
		// really drew — the column-level entry point is what makes that checkable.
		columnSpan, columnStyle := statusHitRateSpan(tc.ops, lineedit.BasicPalette, field)
		if columnStyle != style {
			t.Errorf("%s: the column tier = %q, want %q", tc.name, columnStyle.Open(), style.Open())
		}
		if columnSpan == "" || !strings.Contains(field, columnSpan) {
			t.Errorf("%s: the column span %q is not in the metrics text %q — the emphasis "+
				"would be re-stated nowhere", tc.name, columnSpan, field)
		}
		if !strings.Contains(columnSpan, span) {
			t.Errorf("%s: the column span %q does not carry the reading %q",
				tc.name, columnSpan, span)
		}
	}
}

// TestStatusHitRateEmphasis_UnmeasuredWearsNoTier: with no cache traffic the
// reading is empty (Rule 15b: the absence, never an invented 0%), and the row
// must then wear NO tier and carry NO span — a tier on an unmeasured read-out
// would state a judgement about a number nobody measured.
func TestStatusHitRateEmphasis_UnmeasuredWearsNoTier(t *testing.T) {
	span, style := statusHitRateEmphasis(statusOps{}, lineedit.BasicPalette)
	if span != "" || !style.Empty() {
		t.Fatalf("an unmeasured cache produced span=%q style=%q, want the no-emphasis state",
			span, style.Open())
	}
}

// TestStatusHitRateEmphasis_MonochromeEmitsNoStyle: the monochrome table answers
// every tier with the zero Style, so the whole channel is inert when colour is
// off — and the SPAN is still named, because the span is a fact about the
// measurement, not about the palette. (The row's BYTES are asserted separately,
// in internal/lineedit's status-column emphasis tests.)
func TestStatusHitRateEmphasis_MonochromeEmitsNoStyle(t *testing.T) {
	// The three readings are one per tier, composed at the floors themselves:
	// read/(read+write) sums to a round 1000 tenths, so each case lands exactly on
	// the reading named (and the fraction is part of the form either way).
	for _, tenths := range []int{statusHitRateGoodFloorPct * 10, statusHitRateFairFloorPct * 10, 0} {
		ops := statusOps{cacheRead: tenths * 10, cacheWrite: (1000 - tenths) * 10}
		span, style := statusHitRateEmphasis(ops, lineedit.MonoPalette)
		if !style.Empty() {
			t.Errorf("%d tenths: monochrome produced the style %q, want the zero style", tenths, style.Open())
		}
		if span == "" {
			t.Errorf("%d tenths: monochrome dropped the span — the span is a measured fact, not a colour", tenths)
		}
	}
}

// TestStatusHitRateEmphasis_SpansTheMeasuredText: the span the tier rides is the
// reading the row DRAWS, and the value the tier decision parses is that same
// string — so the tier cannot describe a number the reader does not see.
func TestStatusHitRateEmphasis_SpansTheMeasuredText(t *testing.T) {
	ops := statusOps{cacheRead: 896, cacheWrite: 414}
	span, style := statusHitRateEmphasis(ops, lineedit.BasicPalette)
	if span == "" {
		t.Fatal("a measured cache produced no span")
	}
	if !strings.HasSuffix(span, statusHitRatePctSuffix) {
		t.Fatalf("span %q does not carry the percentage suffix %q", span, statusHitRatePctSuffix)
	}
	// The span is the composer's own reading, character for character: the tier
	// decision reads the SAME string the row draws, so a tier cannot describe a
	// value the reader does not see (Rule 14: one convention, two halves).
	//
	// THE CARRIER IS THE COLUMN'S SPAN, NOT THE BARE PERCENTAGE (this epic): the row
	// now carries TWO readings, so the string the emphasis channel is handed is the
	// one the composer reserved for this reading — the marker together with the
	// percentage (statusHitRateCumulativeSpan), which is what the drawn column holds.
	// The comparison is still against a COMPOSER (Rule 14), never a spelling typed
	// here, so the two halves cannot drift.
	if want := statusHitRateCumulativeSpan(ops); span != want {
		t.Fatalf("span %q is not the composer's own reading %q", span, want)
	}
	// ... and the reading carries the FRACTION the epic asked for: the row's
	// resolution is the printed step, not the whole percent.
	if !strings.Contains(span, statusHitRateDecimalsSepForTest()) {
		t.Fatalf("span %q carries no fraction — the row is still at whole-percent resolution", span)
	}
	// 896/(896+414) = 68.43…% → the FAIR tier, between the two floors.
	if style != lineedit.BasicPalette.StatusFair {
		t.Fatalf("%q wore %q, want the fair tier %q", span, style.Open(), lineedit.BasicPalette.StatusFair.Open())
	}
	// The number the tier was decided from is the number the row PRINTS, to the
	// printed digit: the reader recovers 68.4 from the drawn form.
	back, ok := statusHitRateReading(span)
	if !ok {
		t.Fatalf("the composer's own reading %q does not parse back — the halves disagree", span)
	}
	if math.Abs(back-896.0/1310.0*100) > 0.05 {
		t.Fatalf("the span %q reads back as %v, want 68.4 (896/1310)", span, back)
	}
}

// statusHitRateDecimalsSepForTest is the fraction separator the ROW can print:
// taken from the formatter's own output rather than typed, so a test asserting
// "the form carries a fraction" cannot be satisfied by a spelling the composer
// never writes (Rule 13).
func statusHitRateDecimalsSepForTest() string {
	form := statusHitRatePct(0.5)
	i := strings.IndexFunc(form, func(r rune) bool { return r < '0' || r > '9' })
	if i < 0 {
		return form // no fraction at all — the assertion above will fail loudly
	}
	return string(form[i])
}

// TestStatusHitRateEmphasis_TiersCoverTheWholeRange: every reading the row can
// PRINT lands in exactly one tier, and the tier boundaries are the only places the
// answer changes. Exhaustive over the form's own resolution — 0.0 … 100.0 in
// statusHitRateDecimals steps, i.e. every tenth of a percent the composer can
// state — rather than sampled at the floors: a gap or an overlap in the switch is
// a reading that wears either no tier or the wrong one, and both are invisible on
// a screen one printed step at a time.
func TestStatusHitRateEmphasis_TiersCoverTheWholeRange(t *testing.T) {
	steps := 1
	for i := 0; i < statusHitRateDecimals; i++ {
		steps *= 10
	}
	for tenths := 0; tenths <= statusPctMax*steps; tenths++ {
		ops := statusOps{cacheRead: tenths * 10, cacheWrite: (statusPctMax*steps - tenths) * 10}
		reading := statusCacheHitRate(ops)
		if reading == "" {
			t.Fatalf("%d tenths composed no reading — the case itself is wrong", tenths)
		}
		pct, ok := statusHitRateReading(reading)
		if !ok {
			t.Fatalf("the composer's own reading %q does not parse back", reading)
		}
		_, style := statusHitRateEmphasis(ops, lineedit.BasicPalette)
		want := lineedit.BasicPalette.StatusBad
		if pct >= float64(statusHitRateGoodFloorPct) {
			want = lineedit.BasicPalette.StatusGood
		} else if pct >= float64(statusHitRateFairFloorPct) {
			want = lineedit.BasicPalette.StatusFair
		}
		if style != want {
			t.Fatalf("%q: tier = %q, want %q", reading, style.Open(), want.Open())
		}
		if style.Empty() {
			t.Fatalf("%q: no tier at all — the reading is on the row, so its emphasis must resolve", reading)
		}
	}
}

// TestStatusHitRateEmphasis_ColumnTextDecidesTheSpan: the span is SEARCHED FOR in
// the column's own text, so a column that does not carry the reading wears no
// emphasis — the honest no-op, and the guard against emphasising a form the row
// never printed (the sub-cell yields the bare percentage in a narrow geometry, and
// nothing at all before the cache is exercised).
func TestStatusHitRateEmphasis_ColumnTextDecidesTheSpan(t *testing.T) {
	ops := statusOps{cacheRead: 740, cacheWrite: 260}
	if span, _ := statusHitRateSpan(ops, lineedit.BasicPalette, "cache \u2191740\u2193260"); span != "" {
		t.Errorf("a column without the reading wore %q", span)
	}
	// The carrier is the CACHE sub-cell's own read-out since task 01a0f8c9 (the rate
	// rides the pair it is computed from), so the text the resolver is handed is that
	// sub-cell's — the same shape status() writes onto the metrics column.
	fields := statusCacheField(ops)
	span, style := statusHitRateSpan(ops, lineedit.BasicPalette, fields)
	if span == "" || style.Empty() {
		t.Fatalf("the cache read-out %q produced no span against a colour table", fields)
	}
	if !strings.Contains(fields, span) {
		t.Fatalf("span %q is not in the column text %q", span, fields)
	}
}

// TestStatusHitRateEmphasis_ColumnCarriesTheSpanOnTheRow: the producer's two
// facts reach the COLUMN the row is composed from — the span and the tier — and
// they are the reading that column's text actually contains. This is the seam a
// pure unit test of the resolver cannot see: a resolver that answered perfectly
// while status() never wrote the fields onto the column would leave the row
// unchanged (the silent non-effect, Rule 15).
func TestStatusHitRateEmphasis_ColumnCarriesTheSpanOnTheRow(t *testing.T) {
	src := newChatStatusSource("deepseek-flash", false)
	src.setPalette(lineedit.BasicPalette)
	src.setOpsFn(func() statusOps { return statusOps{cacheRead: 896, cacheWrite: 414} })
	src.update(2, 0, 0, "")

	st := src.status()
	full := statusRowColumns()
	if len(st.Columns) != len(full) {
		t.Fatalf("the composed row carries %d columns, want the geometry's %d",
			len(st.Columns), len(full))
	}
	c := st.Columns[int(statusColMetrics)]
	if c.EmphasisSpan == "" || c.Emphasis.Empty() {
		t.Fatalf("the metrics column carries no emphasis (span=%q style=%q) — the channel is silently inert:\n%q",
			c.EmphasisSpan, c.Emphasis.Open(), c.Text)
	}
	if !strings.Contains(c.Text, c.EmphasisSpan) {
		t.Fatalf("the column's emphasis span %q is not in its own text %q", c.EmphasisSpan, c.Text)
	}
	// And the ROW THE RENDERER DRAWS carries the tier's sequence — the two halves
	// (producer and renderer) are joined, not merely adjacent. Style is compared
	// as a SEQUENCE rather than as "contains some SGR": the assertion is that this
	// column's span wears THIS tier, and a row that coloured something else would
	// satisfy a looser check while the measurement stayed plain.
	plain := st.Line(statusColumnsAtCols)
	styled := st.StyledLine(statusColumnsAtCols, lineedit.BasicPalette)
	if plain == "" {
		t.Fatal("the composed row is empty")
	}
	if styled == plain {
		t.Fatal("the styled row is byte-identical to the plain one — no emphasis reached the row")
	}
	if !strings.Contains(styled, c.Emphasis.Wrap(c.EmphasisSpan)) {
		t.Fatalf("the row does not carry the span %q in its tier's sequence %q:\n%q",
			c.EmphasisSpan, c.Emphasis.Open(), styled)
	}
	if stripSGRForTest(styled) != plain {
		t.Fatalf("the styled row changed the row's TEXT:\n got %q\nwant %q", stripSGRForTest(styled), plain)
	}
}

// stripSGRForTest removes the SGR sequences from a rendered row, so a test can
// compare the TEXT a styled row and a plain row draw. It is a test-local reader
// (the package's own stripper is unexported and this is the consumer's side of the
// boundary): the sequences removed are exactly CSI ...m runs.
func stripSGRForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
