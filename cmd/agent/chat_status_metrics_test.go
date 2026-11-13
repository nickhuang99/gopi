package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// This file is the status row's GEOMETRY test: the row is a fixed-width layout
// whose budget is arithmetic on display CELLS, so the properties that matter are
// measured against the real composition (lineedit.Status.Line) and never against
// a re-implementation of it in the test.
//
// Three families:
//   - the group's own arithmetic (sub-cells, separator, derived widths, budget);
//   - the row rendered at the pin and at every narrower width (Rule 31: the
//     surface is EXECUTED, not described) — each field's cells present where the
//     ladder says they are, and the row never exceeding its band;
//   - the jump regressions: a value that suddenly grows (a long model name, a
//     six-digit token count, a many-digit amount) must not widen the row or
//     slide the columns that follow it.

// statusProbe is a source with every read-out measurable — the WIDEST row the
// row's own numbers can produce, which is the one the budget has to host.
func statusProbeRow(t *testing.T, cols int) string {
	t.Helper()
	s := probeStatusSource()
	// The band the row is composed for is the LIVE band width, exactly as the
	// REPL feeds it (cli_chat → input.bandCols → setAvailCols): the ladder is
	// driven by what the terminal has, so a probe that skipped this would measure
	// one fixed form at every width and prove nothing about the order.
	s.setAvailCols(cols)
	return s.status().Line(cols)
}

// probeStatusSource is the WIDEST row the row's own counters can produce: every
// read-out measured, a tool in flight, a non-trivial iteration. It is a fresh
// source per call (chatStatusSource carries a mutex, so it is neither copied nor
// shared), and each test perturbs it through the source's OWN setters.
func probeStatusSource() *chatStatusSource {
	s := newChatStatusSource("deepseek-flash", true)
	s.update(12, 0, 42100, "")
	s.setTool("keyword_search", "lineedit region cell widths")
	s.setOpsFn(probeStatusSourceOps)
	return s
}

// probeStatusSourceOps is the ONE definition of the probe fixture's counters
// (Rule 14): the widest row the row's own numbers can produce, so a test that
// needs the read-out this state composes asks for it here instead of restating
// the numbers (a second copy would drift, and the drift would be invisible).
func probeStatusSourceOps() statusOps {
	return statusOps{
		ctxUsed: 42100, ctxMax: 64000, ctxMeasured: true,
		cacheRead: 12300, cacheWrite: 4200,
		elapsed: 42 * time.Second, elapsedKnown: true,
		compressions: 2, compressionsKnown: true,
	}
}

// ctxWindowDeployedForTest is the context window a session on THIS host really
// runs with — internal/agent/context.go's DefaultContextWindow — spelled here so
// the reservation is checked against the number that put a six-cell side in a
// pair the old arithmetic (10 cells) claimed to cover. It is a test-local literal
// on purpose: a production constant for "the deployed window" would be a second
// definition of configuration the agent package already owns.
const ctxWindowDeployedForTest = 200000

// TestStatusMetricsGroupArithmetic pins the group's arithmetic in the ONE place
// the geometry can be checked as arithmetic rather than by rendering: the group's
// reserved width is its sub-cells plus the separators that join them, its
// separator is the light one (a sub-cell boundary, never a column boundary), and
// the whole row is inside the pinned budget.
func TestStatusMetricsGroupArithmetic(t *testing.T) {
	if got, want := lineedit.DisplayWidth(statusMetricsSep), statusMetricsSepCells; got != want {
		t.Errorf("statusMetricsSep %q measures %d cells, statusMetricsSepCells says %d", statusMetricsSep, got, want)
	}
	if statusMetricsSepCells >= lineedit.StatusColumnSepCells {
		t.Errorf("the group separator costs %d cells, the column separator %d: the group must use the LIGHT one, or the row pays a column boundary for a field boundary",
			statusMetricsSepCells, lineedit.StatusColumnSepCells)
	}
	if got, want := statusMetricsSepCells, lineedit.StatusSepGroupCells; got != want {
		t.Errorf("statusMetricsSepCells = %d, want lineedit.StatusSepGroupCells = %d", got, want)
	}
	if got, want := statusMetricsSepCells, lineedit.StatusSepCells-1; got != want {
		t.Errorf("statusMetricsSepCells = %d, want StatusSepCells-1 = %d", got, want)
	}
	// The group's arithmetic is DERIVED from its sub-cells (task 01a0dcd1): the
	// count, the reserved width, and the runtime list the group draws from must
	// agree, or a read-out was added or removed in one place and not the others.
	if got, want := statusMetricsCol, statusCtxCol+statusCacheCol+(statusMetricsSubCells-1)*statusMetricsSepCells; got != want {
		t.Errorf("statusMetricsCol = %d, want %d (sub-cells + separators)", got, want)
	}
	if got, want := len(statusMetricsSubCellWidths()), statusMetricsSubCells; got != want {
		t.Errorf("statusMetricsSubCellWidths lists %d sub-cells, statusMetricsSubCells says %d", got, want)
	}
	if got, want := statusMetricsGroupWidth(statusMetricsSubCellWidths()), statusMetricsCol; got != want {
		t.Errorf("the derived group width is %d cells, statusMetricsCol says %d — the group and its "+
			"reservation disagree about what the row draws", got, want)
	}
	for i, w := range statusMetricsSubCellWidths() {
		if w <= 0 {
			t.Errorf("sub-cell %d reserves %d cells: a reservation the row cannot draw in is the "+
				"retired-column defect (see statusCostCol)", i, w)
		}
	}
	// The context sub-cell's composition is derived and must be self-consistent:
	// the bar takes the remainder, and the pair that remainder is measured from is
	// The LABEL and its gap are spent, and the BAR takes whatever is left — which
	// in the pinned geometry (task 01a0ddab) is nothing, because the label and the
	// pair's honest worst case already fill the sub-cell.
	//
	// The percentage is NOT in this arithmetic any more: it left the aligned row
	// (the fold), so no cells are reserved for one. statusCtxPctCells stays a
	// measured width the fold's reasoning is RESTATED against — a fact about
	// statusCtxPct's output, not a term in this reservation.
	// The bar is what the RESERVATION leaves over, and the qualifier IS a term now
	// (task 01a0f8c9): the sub-cell reserves the four fields the pinned row draws
	// (label, gap, pair, qualifier), so the bar is the remainder — and at this pin the
	// remainder is ZERO, which is the honest statement ("no cells for a bar") rather
	// than a reservation nothing draws in.
	if got := statusCtxCol - statusCtxLabelCells - statusCtxBarGapCells - statusCtxPairCells -
		statusCtxQuantCells; got != statusCtxBarCells {
		t.Errorf("statusCtxBarCells = %d, want %d (ctx %d - label %d - gap %d - pair %d - qualifier %d)",
			statusCtxBarCells, got, statusCtxCol, statusCtxLabelCells, statusCtxBarGapCells,
			statusCtxPairCells, statusCtxQuantCells)
	}
	if statusCtxBarCells != 0 {
		t.Errorf("statusCtxBarCells = %d, want 0 at the pin: the sub-cell is the label, the pair and the "+
			"qualifier, and a bar would need cells this budget does not have", statusCtxBarCells)
	}
	if got, want := lineedit.DisplayWidth(statusCtxQuantGlyph), statusCtxQuantCells; got != want {
		t.Errorf("statusCtxQuantGlyph %q measures %d cells, statusCtxQuantCells says %d — the sub-cell's "+
			"reservation and the pinned budget are both derived from that number", statusCtxQuantGlyph, got, want)
	}
	if statusCtxQuantGlyph == "" {
		t.Errorf("statusCtxQuantGlyph is empty: the denominator would print with no statement of WHAT it " +
			"is — the defect this field exists to close (bug F)")
	}
	if got, want := lineedit.DisplayWidth(statusCtxLabel), statusCtxLabelCells; got != want {
		t.Errorf("statusCtxLabel %q measures %d cells, statusCtxLabelCells says %d — the sub-cell's "+
			"reservation is derived from that number, so the two cannot drift", statusCtxLabel, got, want)
	}
	if got, want := lineedit.DisplayWidth(statusCtxPairSep), statusCtxPairSepCells; got != want {
		t.Errorf("statusCtxPairSep %q measures %d cells, statusCtxPairSepCells says %d", statusCtxPairSep, got, want)
	}
	// statusCtxPairCells must COVER what formatCount really prints for the tokens
	// it really receives — the assertion the old reservation was missing. Its 10
	// was not a measurement of a pair at all (5+1+4, where the 4 was a percentage's
	// cells), and the DEPLOYED window (200000 → "200.0k", six cells) put a 13-cell
	// pair in a sub-cell that 10 claimed to cover.
	if got, want := statusCtxPairCells, 2*statusCtxPairSideCellsWidest+statusCtxPairSepCells; got != want {
		t.Errorf("statusCtxPairCells = %d, want %d (two sides of the widest class + the separator)", got, want)
	}
	for _, n := range []int{statusProbeCtxMax, ctxWindowDeployedForTest} {
		if w := lineedit.DisplayWidth(formatCountBounded(n)); w > statusCtxPairSideCellsWidest {
			t.Errorf("formatCountBounded(%d) = %q is %d cells, over statusCtxPairSideCellsWidest = %d: the pair's "+
				"reservation does not cover a window this row really receives", n, formatCountBounded(n), w,
				statusCtxPairSideCellsWidest)
		}
	}
	// The row is composed for the budget and never past it. It is deliberately
	// not asserted EQUAL: the pin is a ceiling, and one cell of slack is the air
	// that keeps the live block's left edge where it is on the narrowest terminal
	// the aligned form is offered on (the render test below is what holds the row
	// inside the band at every width).
	if statusColumnsAtCols > statusLayoutBudgetCols {
		t.Errorf("statusColumnsAtCols = %d cells, over the pinned budget %d", statusColumnsAtCols, statusLayoutBudgetCols)
	}
	t.Logf("group: ctx %d + %d seams x%d + cache %d = %d reserved, %d at the full form (%d sub-cells); "+
		"row pin %d, budget %d, slack %d; "+
		"ctx sub-cell = label %d + gap %d + pair %d + qualifier %d = %d; bar %d",
		statusCtxCol, statusMetricsSubCells-1, statusMetricsSepCells, statusCacheCol,
		statusMetricsCol, statusMetricsFormCol, statusMetricsSubCells,
		statusColumnsAtCols, statusLayoutBudgetCols, statusPinSlackCells,
		statusCtxLabelCells, statusCtxBarGapCells, statusCtxPairCells, statusCtxQuantCells,
		statusCtxCol, statusCtxBarCells)
}

// TestStatusRowRendersAtThePin is Rule 31 for this change: the row is rendered,
// at the pinned width, and every field the epic names is found in the cells the
// geometry reserves for it. The sub-cell anchors are derived from the PRODUCTION
// constants (statusCtxField/statusCtxPair/statusCacheField), and the one the
// re-partition RETIRED is asserted ABSENT (statusMoneyGlyph below), so the
// assertion states which fields the column must carry, not how they are spelled.
func TestStatusRowRendersAtThePin(t *testing.T) {
	row := statusProbeRow(t, statusColumnsAtCols)
	t.Logf("pin row (%d cells): %q", lineedit.DisplayWidth(row), row)

	if got := lineedit.DisplayWidth(row); got > statusColumnsAtCols {
		t.Fatalf("row measures %d cells, over the %d-cell pin", got, statusColumnsAtCols)
	}
	// The identity field is reserved so the real model names fit UNCUT: a
	// truncated model name is a worse row than a shorter one, and "deepseek-flash"
	// is the measured worst case on this host.
	if !strings.Contains(row, "deepseek-flash") || strings.Contains(row, "deepseek-fla…") {
		t.Errorf("the model field is cut on the pinned row: %q", row)
	}
	// The metrics field is anchored through the row's OWN state (probeStatusSource):
	// the sub-cell the group leads with is the production composer's output for
	// exactly the ops that source renders (task 01a0e8c7), so this asserts the
	// field's presence without restating a form that a counter change would move.
	metricsField := statusCtxField(probeStatusSourceOps(), 42100)
	if metricsField == "" {
		t.Fatalf("the probe's own ops compose no metrics read-out — the fixture is wrong")
	}
	for _, want := range []string{
		"deepseek-flash", "turn12", "keyword_search",
		metricsField,
		statusCacheField(statusOps{cacheRead: 12300, cacheWrite: 4200}),
		statusLiveIdle,
	} {
		if want == "" {
			t.Fatalf("a field the row must carry renders empty — the case itself is wrong")
		}
		if !strings.Contains(row, want) {
			t.Errorf("pinned row is missing %q\n  row: %q", want, row)
		}
	}
	// The group is joined by the LIGHT separator: one seam between its TWO
	// sub-cells (the operational read-out and the cache pair) — and the money sub-cell
	// that used to be the third is RETIRED, so no currency glyph is on the row
	// (task 01a0dcd1). The seam is counted INSIDE the group's own cells rather than
	// across the whole row: statusMetricsSep is the light separator, which is the
	// same glyph a TOP-LEVEL column boundary carries as its middle cell, so a
	// whole-row count would be counting the columns, not the group.
	group := fieldStartInTest(row, metricsField)
	if group < 0 {
		t.Fatalf("the row does not carry its own metrics read-out %q: %q", metricsField, row)
	}
	groupCells := row
	if group < len(row) {
		groupCells = row[group:]
	}
	// The group's own cells are statusMetricsCol wide; the pair's cell is where the
	// group starts, so the seam the group renders is the FIRST light separator
	// inside them.
	if got := strings.Index(groupCells, statusMetricsSep); got < 0 {
		t.Errorf("the metrics group renders no seam between its two sub-cells: %q", row)
	} else if got > statusMetricsCol {
		t.Errorf("the group's seam starts %d cells into the group, past its %d-cell reservation: %q",
			got, statusMetricsCol, row)
	}
	if strings.Contains(groupCells, statusMetricsSep+statusMetricsSep) {
		t.Errorf("the group's separators are adjacent — an empty sub-cell indented the ones after it: %q", row)
	}
	if strings.Contains(row, statusMoneyGlyph) {
		t.Errorf("the pinned row carries a money field: %q — the row shows only measured read-outs", row)
	}
}

// TestStatusRowNeverExceedsItsBand renders the row at the pin and at EVERY
// narrower width, and asserts the row still composes inside the band it is given.
// This is the property a fixed-geometry row exists to have: the fields move
// between forms, but the row never paints past the terminal.
func TestStatusRowNeverExceedsItsBand(t *testing.T) {
	for cols := statusColumnsAtCols; cols >= 1; cols-- {
		row := statusProbeRow(t, cols)
		got := lineedit.DisplayWidth(row)
		if got > cols {
			t.Fatalf("cols=%d: row measures %d cells\n  row: %q", cols, got, row)
		}
	}
	// The widths the epic calls out explicitly, quoted in the report.
	for _, cols := range []int{statusColumnsAtCols, 96, 88, 80, 72, 60, 40, 26, 12} {
		row := statusProbeRow(t, cols)
		t.Logf("cols=%-3d cells=%-3d %q", cols, lineedit.DisplayWidth(row), row)
	}
}

// TestStatusRowDegradesInTheDocumentedOrder drives the ladder through its steps
// and names what each step keeps and sheds. The order is the documented one: the
// metrics GROUP goes first as a whole (every read-out inside it, the context pair
// and the cache pair together), then tool, and model · iter · mode are the last
// three standing.
func TestStatusRowDegradesInTheDocumentedOrder(t *testing.T) {
	type step struct {
		name  string
		keeps []string
		sheds []string
	}
	steps := []step{
		{name: "full", keeps: []string{"deepseek-flash", "keyword_search", "turn12", statusCacheReadGlyph}},
		{name: "metrics group shed first", keeps: []string{"deepseek-flash", "keyword_search", "turn12"}, sheds: []string{statusCacheReadGlyph}},
		{name: "tool next", keeps: []string{"deepseek-flash", "turn12"}, sheds: []string{"keyword_search"}},
	}

	// Walk the widths down from the pin and record the FIRST width at which each
	// step's row appears: the ladder's own steps, measured — never a hand-kept
	// table of widths that could drift from the geometry.
	seen := map[string]int{"full": statusColumnsAtCols}
	for cols := statusColumnsAtCols; cols >= 1; cols-- {
		row := statusProbeRow(t, cols)
		if _, ok := seen["metrics group shed first"]; !ok && !strings.Contains(row, statusCacheReadGlyph) && strings.Contains(row, "keyword_search") {
			seen["metrics group shed first"] = cols
		}
		if _, ok := seen["tool next"]; !ok && !strings.Contains(row, "keyword_search") && strings.Contains(row, "turn") {
			seen["tool next"] = cols
		}
	}

	for _, st := range steps {
		cols, ok := seen[st.name]
		if !ok {
			t.Errorf("step %q never appeared while narrowing from the pin: that step is not in the ladder", st.name)
			continue
		}
		row := statusProbeRow(t, cols)
		for _, k := range st.keeps {
			if !strings.Contains(row, k) {
				t.Errorf("step %q at cols=%d must keep %q\n  row: %q", st.name, cols, k, row)
			}
		}
		for _, sh := range st.sheds {
			if strings.Contains(row, sh) {
				t.Errorf("step %q at cols=%d must have shed %q\n  row: %q", st.name, cols, sh, row)
			}
		}
		t.Logf("step %-26s cols=%-3d %q", st.name, cols, row)
	}

	// The documented ORDER: each step is reached at a strictly narrower band than
	// the one before it.
	if !(seen["full"] > seen["metrics group shed first"] && seen["metrics group shed first"] > seen["tool next"]) {
		t.Errorf("ladder order is wrong: full=%d, group-shed-first=%d, tool-next=%d (each must be strictly narrower)",
			seen["full"], seen["metrics group shed first"], seen["tool next"])
	}
}

// TestStatusLongValueCannotWidenTheRow is the "jumping row" regression: the row's
// layout belongs to the GEOMETRY, so a value that suddenly grows must be cut,
// never allowed to push the row wider or slide the cells around it. Each case
// perturbs a fresh, fully-measured source through its own setters and then
// measures the RENDERED row: its width stays inside the pin, and every fixed
// field still starts at the cell its column reserves.
func TestStatusLongValueCannotWidenTheRow(t *testing.T) {
	// The fixed cells, measured from the row's left edge. These are the cells the
	// geometry reserves — model at 0, then each column one reserved width plus one
	// column separator further right. They must not move for ANY value.
	fixed := []struct {
		anchor string
		cell   int
	}{
		{"turn", statusModelCol + lineedit.StatusColumnSepCells},
		{statusToolGlyphTest, statusModelCol + statusTurnCol + 2*lineedit.StatusColumnSepCells},
	}

	cases := []struct {
		name  string
		apply func(s *chatStatusSource)
	}{
		{"long model name", func(s *chatStatusSource) { s.model = "glm-5.3-flash-long-model-name" }},
		{"six-digit token count", func(s *chatStatusSource) { s.update(12, 0, 421000, "") }},
		{"huge cache pair", func(s *chatStatusSource) {
			s.setOpsFn(func() statusOps {
				return statusOps{ctxUsed: 42100, ctxMax: 64000, ctxMeasured: true,
					cacheRead: 1234567, cacheWrite: 987654,
					elapsed: 42 * time.Second, elapsedKnown: true}
			})
		}},
		{"huge context pair", func(s *chatStatusSource) {
			s.setOpsFn(func() statusOps {
				return statusOps{ctxUsed: 4210000, ctxMax: 8000000, ctxMeasured: true}
			})
		}},
		{"unmeasured everything", func(s *chatStatusSource) {
			s.setOpsFn(func() statusOps { return statusOps{} })
		}},
		{"very long tool summary", func(s *chatStatusSource) {
			s.setTool("keyword_search", strings.Repeat("a very long summary ", 20))
		}},
	}
	for _, tc := range cases {
		s := probeStatusSource()
		tc.apply(s)
		row := s.status().Line(statusColumnsAtCols)
		if got := lineedit.DisplayWidth(row); got > statusColumnsAtCols {
			t.Errorf("%s: row measures %d cells, over the %d-cell pin — the layout left the band\n  row: %q",
				tc.name, got, statusColumnsAtCols, row)
		}
		for _, f := range fixed {
			if got := fieldStartInTest(row, f.anchor); got != f.cell {
				t.Errorf("%s: %q starts at cell %d, want %d — a value moved a fixed column\n  row: %q",
					tc.name, f.anchor, got, f.cell, row)
			}
		}
		t.Logf("%-22s cells=%-3d %q", tc.name, lineedit.DisplayWidth(row), row)
	}
}

// statusToolGlyphTest is the marker the row's tool column begins with, read from
// lineedit's own definition (Rule 1b) rather than retyped here.
const statusToolGlyphTest = lineedit.ToolGlyph

// TestStatusMetricsFieldsAreHonest pins the group's unmeasured branches: a
// counter nobody measured prints NO NUMBER (never a zero), and the group's seams
// hold so a value appearing later cannot slide the sub-cell before it.
//
// THE ROTATED CLAUSE (task 01a0e562, the group's honesty clause). This test used
// to open by asserting the all-unmeasured group renders the EMPTY STRING — an
// exact-equality pin on the value the arm returned, written when "" was taken to
// be the honest answer. It is not: the group is a RESERVED column
// (statusMetricsCol), so "" left its cells blank between two drawn separators,
// and a reader could not tell "nothing has been measured yet" from "this field is
// broken" — the defect the operator filed, and the same sentence the tool
// column's idle indent used to say one column to the left (statusNoToolLabel).
//
// So the clause is DELIBERATELY REPLACED rather than relaxed: the group's
// unmeasured state must be STATED in words (statusMetricsUnmeasured), and Rule
// 15b's contract is preserved exactly where it belongs — on the COUNTERS, an
// unmeasured counter never printing a number. The rotation is not a weaker
// assertion: the new clause binds the group to a WORD and to a digit-free
// rendering, which the old one did not check at all. The dedicated guard for the
// new behaviour is chat_status_metrics_unmeasured_test.go.
func TestStatusMetricsFieldsAreHonest(t *testing.T) {
	if got := statusMetricsField(statusOps{}, 0); got != statusMetricsUnmeasured {
		t.Errorf("an all-unmeasured group renders %q, want the stated state %q — a reserved "+
			"column is never a hole, and it never renders a counter nobody measured as a number",
			got, statusMetricsUnmeasured)
	}
	if got := statusMetricsField(statusOps{}, 0); strings.ContainsAny(got, "0123456789") {
		t.Errorf("an all-unmeasured group renders a DIGIT (%q): an unmeasured counter must print "+
			"no number at all, least of all the zero that reads as a measurement (Rule 15b)", got)
	}
	// ctx unmeasured, cache measured: no leading indent, one separator.
	got := statusMetricsField(statusOps{cacheRead: 12300, cacheWrite: 4200}, 0)
	if strings.HasPrefix(got, statusMetricsSep) {
		t.Errorf("a leading empty sub-cell indented the group: %q", got)
	}
	// The state this fixture describes is the cache counters WITHOUT a context
	// measurement. The group's leading sub-cell is the context pair, which has
	// nothing to say here, so the cache sub-cell's own read-out stands at the
	// group's FIRST cell — no leading indent — and the seam rule under test is
	// unchanged: an unmeasured sub-cell costs no seam.
	if want := statusCacheField(statusOps{cacheRead: 12300, cacheWrite: 4200}); got != want {
		t.Errorf("the cache counters alone render %q, want just the cache read-out %q "+
			"(the leading sub-cell has no value, so no seam is spent)", got, want)
	}
	// An unmeasured sub-cell costs no seam: the group joins the read-outs it can
	// show, and a hole where a sub-cell has no value is the left anchor's rule one
	// position over. Regression guard for the doubled separator a joined-over hole
	// produced ("6% ░░░░ · ·~1.23元") — the cache sub-cell is the one a session
	// reaches LAST, so "ctx measured, cache not" is the ordinary shape.
	ctxOnly := statusOps{ctxUsed: 12600, ctxMax: 200000, ctxMeasured: true}
	if got := statusMetricsField(ctxOnly, 0); got != statusCtxField(ctxOnly, 0) {
		t.Errorf("ctx measured + cache unmeasured renders %q, want just %q (no seam for the hole)",
			got, statusCtxField(ctxOnly, 0))
	}
	// And the same state drives the CACHE sub-cell's own composer: the counters
	// alone (no context measurement) are what the ordinary row of a session that has
	// re-sent a prompt but whose context has not been counted yet carries.
	cacheOnly := statusOps{cacheRead: 12300, cacheWrite: 4200}
	if got, want := statusMetricsField(cacheOnly, 0),
		statusCacheField(cacheOnly); got != want {
		t.Errorf("cache measured + ctx unmeasured renders %q, want %q (the cache read-out alone)",
			got, want)
	}
	// A seam BETWEEN two rendered sub-cells IS held, so a value appearing later
	// cannot slide the one before it.
	both := statusOps{ctxUsed: 12600, ctxMax: 200000, ctxMeasured: true,
		cacheRead: 12000, cacheWrite: 4000}
	got = statusMetricsField(both, 0)
	want := statusCtxField(both, 0) + statusMetricsSep + statusCacheField(both)
	if got != want {
		t.Errorf("ctx + cache render %q, want %q (one seam between them)", got, want)
	}
}

// fieldStartInTest is the display cell a field starts at inside a row, or -1 when
// the row does not carry it. It is the cmd/agent twin of lineedit's
// fieldStartIn — that one is test-private to its package — and it measures in
// CELLS (lineedit.DisplayWidth), never in bytes: the row is full of multi-byte
// glyphs, so a byte offset would be a different (and wrong) number.
func fieldStartInTest(row, field string) int {
	i := strings.Index(row, field)
	if i < 0 {
		return -1
	}
	return lineedit.DisplayWidth(row[:i])
}

// statusCtxFormsForQualifierTest enumerates the context forms a session really
// reaches, and the pair each one composes — derived by calling the PRODUCTION pair
// composer, never spelled here. The list is chosen so that the whole width axis of
// a PAIR side is covered: five cells (64.0k), six (200.0k, the deployed window) and
// seven (10000.0k, above formatCount's "%.1fk" class), which is the boundary the
// sub-cell's reservation is stated against.
func statusCtxFormsForQualifierTest() []struct {
	name string
	ops  statusOps
} {
	return []struct {
		name string
		ops  statusOps
	}{
		{"deployed window (200.0k sides)", statusOps{ctxUsed: 92100, ctxMax: 200000, ctxMeasured: true}},
		{"the test fixture's 64.0k sides", statusOps{ctxUsed: 42100, ctxMax: 64000, ctxMeasured: true}},
		{"heavy territory (the retired marker's threshold)", statusOps{ctxUsed: statusTokensHeavy, ctxMax: 200000, ctxMeasured: true}},
		{"a 1M window (one side over the reservation's class)", statusOps{ctxUsed: 999900, ctxMax: 1000000, ctxMeasured: true}},
		{"both sides over the reservation's class", statusOps{ctxUsed: 9999900, ctxMax: 9999900, ctxMeasured: true}},
		{"the window's percentage saturating", statusOps{ctxUsed: 199999, ctxMax: 200000, ctxMeasured: true}},
	}
}

// TestStatusContextQualifierSurvivesEveryPairForm is bug F's primary guard, and it
// is CELL-level rather than string-level: on EVERY form the context sub-cell really
// takes — including the widest pair the reservation is measured against — the row
// states WHAT the denominator is, and it states it ADJACENT TO the denominator
// (immediately after it, in the next cell), so the glyph cannot be read as
// belonging to the cache pair further right or to any other field.
//
// The adjacency assertion is what makes this a guard on the DEFECT rather than on a
// substring: "the glyph is somewhere on the row" would pass even if the qualifier
// drifted to another field's cells. The cells are measured with
// lineedit.DisplayWidth (a byte offset would be wrong for the row's multi-byte
// glyphs), and both anchors come from the production composition
// (statusCtxPair, statusCtxQuantGlyph) rather than from a hand-derived rendering.
func TestStatusContextQualifierSurvivesEveryPairForm(t *testing.T) {
	groupStart := statusModelCol + statusTurnCol + statusToolCol +
		statusColumnSepTotalN*lineedit.StatusColumnSepCells
	for _, form := range statusCtxFormsForQualifierTest() {
		s := newChatStatusSource("deepseek-flash", true)
		s.update(12, 0, form.ops.ctxUsed, "")
		ops := form.ops
		s.setOpsFn(func() statusOps { return ops })
		s.setAvailCols(statusColumnsAtCols)
		row := s.status().Line(statusColumnsAtCols)

		pair := statusCtxPair(form.ops, form.ops.ctxUsed)
		field := statusCtxField(form.ops, form.ops.ctxUsed)
		if pair == "" {
			t.Fatalf("%s: the case composes no pair (%q) — the case itself is wrong", form.name, pair)
		}
		// The sub-cell leads with the cache ratio once the cache has been measured
		// (task 01a0e8c7), so the pair only reaches the screen in this form's own
		// state: NO cache counters, i.e. before the first re-sent prompt. The row
		// is composed with the cache unmeasured for exactly that reason, and the
		// sub-cell must be the pair's own qualified form — the fallback this test
		// is about.
		if field == "" || field == statusCacheHitRateField(form.ops, statusCtxCol) {
			t.Fatalf("%s: the case does not exercise the pair's FALLBACK form (field %q) — the case itself is wrong",
				form.name, field)
		}
		// The sub-cell never overruns its reservation on ANY of these forms: the
		// quantifier is the field that stays when the label cannot (see
		// statusCtxField's own note), and this is the arithmetic that proves the
		// choice is affordable at both ends of the class.
		if w := lineedit.DisplayWidth(field); w > statusCtxCol {
			t.Errorf("%s: the composed sub-cell is %d cells, over its %d-cell reservation: %q",
				form.name, w, statusCtxCol, field)
		}
		// The DENOMINATOR: the pair must be on the row WHOLE — a cut number is the
		// one thing this row must never print.
		pairCell := fieldStartInTest(row, pair)
		if pairCell < 0 {
			t.Fatalf("%s: the row does not carry the pair %q\nrow: %q", form.name, pair, row)
		}
		if pairCell < groupStart || pairCell+lineedit.DisplayWidth(pair) > groupStart+statusCtxCol {
			t.Errorf("%s: the pair %q sits at cells %d..%d, outside the ctx sub-cell's %d..%d\nrow: %q",
				form.name, pair, pairCell, pairCell+lineedit.DisplayWidth(pair),
				groupStart, groupStart+statusCtxCol-1, row)
		}
		// The QUALIFIER: present, and in the cell immediately after the denominator.
		quantCell := fieldStartInTest(row, statusCtxQuantGlyph)
		if quantCell < 0 {
			t.Fatalf("%s: the row carries a denominator with NO statement of what it is — no %q anywhere\n"+
				"row: %q", form.name, statusCtxQuantGlyph, row)
		}
		if want := pairCell + lineedit.DisplayWidth(pair); quantCell != want {
			t.Errorf("%s: the qualifier %q sits at cell %d, want %d — it must RIDE the denominator "+
				"(immediately after it), not float somewhere on the row\nrow: %q",
				form.name, statusCtxQuantGlyph, quantCell, want, row)
		}
		if quantCell+lineedit.DisplayWidth(statusCtxQuantGlyph) > groupStart+statusCtxCol {
			t.Errorf("%s: the qualifier falls outside the ctx sub-cell (cell %d, sub-cell ends at %d)\nrow: %q",
				form.name, quantCell, groupStart+statusCtxCol-1, row)
		}
		t.Logf("%-48s pair=%-15q field=%-18q quantCell=%-3d", form.name, pair, field, quantCell)
	}
}

// TestStatusContextDenominatorIsNeverUnqualified is the NO-BARE-DENOMINATOR
// regression the epic asks for, and it is a SWEEP rather than a table of cases: it
// walks the tokens axis (including every token count around the retired marker's
// threshold, where the label used to be the field that got dropped) and asserts
// that no composition the production renderer can produce ever shows a denominator
// — a "/" with a count after it — without the qualifier riding it.
//
// The check is expressed on the COMPOSED STRING through the production helper
// (statusCtxField), so it covers every branch of that composer (the full form, the
// yielded-label form of a 1M window, and the bar form a wider geometry would use)
// instead of the one branch a table would have picked. A bare pair reappearing —
// the defect the operator reported on the live row — fails here.
func TestStatusContextDenominatorIsNeverUnqualified(t *testing.T) {
	window := ctxWindowDeployedForTest
	checked := 0
	for _, used := range []int{
		0, 1, 999, statusTokensHeavy - 1, statusTokensHeavy, statusTokensHeavy + 1,
		92100, window / 2, window - 1, window, window + 1, 999999, 9999999,
	} {
		ops := statusOps{ctxUsed: used, ctxMax: window, ctxMeasured: true}
		field := statusCtxField(ops, used)
		pair := statusCtxPair(ops, used)
		if field == "" {
			// The only legitimate empty: nothing measured, which happens exactly
			// when neither the context nor the token total is known.
			if pair != "" {
				t.Errorf("used=%d: the pair %q composes but the sub-cell is empty", used, pair)
			}
			continue
		}
		checked++
		if !strings.Contains(pair, statusCtxPairSep) {
			continue // no denominator on this form: nothing to qualify
		}
		if !strings.Contains(field, statusCtxQuantGlyph) {
			t.Errorf("used=%d: the sub-cell shows a denominator with no qualifier: %q (the pair is %q) — "+
				"the denominator's identity must be ON the row", used, field, pair)
		}
		if !strings.HasSuffix(field, statusCtxQuantGlyph) {
			t.Errorf("used=%d: the qualifier is not the sub-cell's LAST field, so a padded cell would "+
				"elide it: %q", used, field)
		}
		if w := lineedit.DisplayWidth(field); w > statusCtxCol {
			t.Errorf("used=%d: the sub-cell is %d cells, over its %d-cell reservation: %q", used, w, statusCtxCol, field)
		}
	}
	if checked == 0 {
		t.Fatal("no form with a measured context was checked — the sweep is not exercising the composer")
	}
	t.Logf("checked %d measured forms across the token axis, every one qualified", checked)
}

// TestStatusContextQualifierDoesNotMoveTheColumns keeps the property the retired
// warn-form test held: a context in any territory occupies the SAME cells, so the
// columns to its right do not slide. The row is a fixed geometry, and the qualifier
// is inside the ctx sub-cell's reservation rather than beside it.
func TestStatusContextQualifierDoesNotMoveTheColumns(t *testing.T) {
	rows := map[string]string{}
	for _, used := range []int{1000, statusTokensHeavy, statusTokensHeavy * 3} {
		s := probeStatusSource()
		s.setOpsFn(func() statusOps {
			return statusOps{ctxUsed: used, ctxMax: ctxWindowDeployedForTest, ctxMeasured: true}
		})
		rows[fmt.Sprintf("used=%d", used)] = s.status().Line(statusColumnsAtCols)
	}
	ref := ""
	for name, row := range rows {
		if got := lineedit.DisplayWidth(row); got > statusColumnsAtCols {
			t.Errorf("%s: the row is %d cells, over the %d-cell pin: %q", name, got, statusColumnsAtCols, row)
		}
		tool := fieldStartInTest(row, toolGlyph)
		if ref == "" {
			ref = fmt.Sprintf("%d", tool)
		}
		if fmt.Sprintf("%d", tool) != ref {
			t.Errorf("%s: the tool column starts at cell %d, want %s — a context value moved the row's "+
				"fixed columns\nrow: %q", name, tool, ref, row)
		}
		t.Logf("%-18s cells=%-3d toolCell=%d %q", name, lineedit.DisplayWidth(row), tool, row)
	}
}

// TestStatusRowAtThePinIsExactlyTheBudget is the epic's CONSTRAINT CHECK (task
// 01a0e394, bug F): at the width the row is designed for, the composed row
// occupies EXACTLY statusLayoutBudgetCols cells — not fewer — with the context
// denominator's qualifier (statusCtxQuantGlyph) in the ctx field. It is the
// assertion that makes "no column reserves air" measurable rather than argued:
// a reservation wider than what the pinned row draws would leave no cell for the
// live block's own gap, and the row would come out SHORT of the budget by exactly
// the air that reservation hoards.
//
// The live block is probed at its reserved width on purpose. A session whose
// block is narrower than statusLiveCol (that is what probeStatusSource's 42s
// yields) composes a correct but SHORTER row — the block is a real column whose
// content legitimately underfills it — so measuring the budget there would
// measure the probe, not the geometry. The row that spends the budget to its last
// cell is the one whose every column is at its reserved width, which is what the
// constant below is.
func TestStatusRowAtThePinIsExactlyTheBudget(t *testing.T) {
	const (
		// probeCtxUsed / probeCtxMax are the context read-out the pinned row is
		// measured with — the same 42.1k of a 64.0k window this file's other
		// fixtures render, so the row measured here and the rows the neighbouring
		// assertions render are one geometry (Rule 1: a number with a name).
		probeCtxUsed = 42100
		probeCtxMax  = 64000
	)
	// The widest live block the reservation hosts: the state word plus the counter
	// form that fills statusLiveCol exactly. Composed by the PRODUCTION composer
	// (statusLiveField), never hand-derived here.
	liveOps := statusOps{
		ctxUsed: probeCtxUsed, ctxMax: probeCtxMax, ctxMeasured: true,
		elapsed: time.Hour + 2*time.Minute, elapsedKnown: true,
	}
	live := statusLiveField(statusLiveIdle, "", liveOps)
	if got := statusFieldCells(live); got != statusLiveCol {
		t.Fatalf("the fixture's live block is %d cells, want the reserved %d: %q — the probe is not at the "+
			"column's own width, so this test would measure the probe", got, statusLiveCol, live)
	}

	s := newChatStatusSource("deepseek-flash", true)
	s.update(12, 0, probeCtxUsed, "")
	s.setTool("keyword_search", "lineedit region cell widths")
	s.setOpsFn(func() statusOps { return liveOps })
	s.setAvailCols(statusColumnsAtCols)
	row := s.status().Line(statusColumnsAtCols)

	t.Logf("pin row: %d cells (pin %d, budget %d, slack %d)", lineedit.DisplayWidth(row),
		statusColumnsAtCols, statusLayoutBudgetCols, statusPinSlackCells)
	t.Logf("row: %q", row)

	// The arithmetic the epic names, read from the constants it names. The ctx
	// sub-cell reserves the four fields the pinned row draws — label, gap, pair and
	// the denominator's qualifier (task 01a0f8c9; before the re-partition the
	// qualifier lived INSIDE the label's cells) — and the bar is what is left over.
	if got, want := statusCtxCol, statusCtxLabelCells+statusCtxBarGapCells+statusCtxPairCells+
		statusCtxQuantCells; got != want {
		t.Errorf("statusCtxCol = %d, want %d (label %d + gap %d + pair %d + qualifier %d)", got, want,
			statusCtxLabelCells, statusCtxBarGapCells, statusCtxPairCells, statusCtxQuantCells)
	}
	if statusPinCols != statusLayoutBudgetCols {
		t.Errorf("statusPinCols = %d, want the budget %d", statusPinCols, statusLayoutBudgetCols)
	}
	if statusPinSlackCells != 0 {
		t.Errorf("statusPinSlackCells = %d, want 0 — the qualifier's cell came from inside the ctx sub-cell, "+
			"so no column may hold cells nothing draws", statusPinSlackCells)
	}
	// The composed row: exactly the budget, and carrying the qualifier.
	if got := lineedit.DisplayWidth(row); got != statusLayoutBudgetCols {
		t.Errorf("the pinned row is %d cells, want EXACTLY the %d-cell budget: %q", got, statusLayoutBudgetCols, row)
	}
	if !strings.Contains(row, statusCtxPair(statusOps{ctxUsed: probeCtxUsed, ctxMax: probeCtxMax, ctxMeasured: true}, probeCtxUsed)+statusCtxQuantGlyph) {
		t.Errorf("the pinned row's denominator is not qualified (want the pair immediately followed by %q): %q",
			statusCtxQuantGlyph, row)
	}
}
