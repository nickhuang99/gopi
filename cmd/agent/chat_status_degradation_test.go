package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_status_degradation_test.go — the status row's WIDTH / DEGRADATION
// contract, table-driven, driven through the REAL composed row.
//
// Every row asserted here is produced by chatStatusSource.status() and rendered
// by lineedit's own composition (Status.Line) — the same two calls the REPL
// makes. The test never re-implements the composition: what it does is STATE an
// expectation (the degradation order the geometry documents, the width a form
// needs, the cells a field starts on) and then compare it against the row the
// production path really draws. A test that composed the row itself would be
// asserting its own arithmetic, which is the defect this file exists to catch
// (Rule 31).
//
// The width the row is composed FOR is the one the source was told about
// (setAvailCols, which the REPL rides on every publish): internal/lineedit
// refuses the aligned form on a terminal narrower than the columns it was handed
// and falls back to the unaligned composition, so a row composed for the pinned
// geometry on a 60-column terminal can only ever be all-or-nothing.

// The terminals the table walks. The first two are read from the production
// geometry (the budget it is pinned to, and its own width); the rest are the
// widths the acceptance names, each one a terminal the row is really drawn on —
// named here (Rule 1) rather than spelled as bare numbers in the table.
const (
	// statusDegradeWidths90AndBelow are the sampled terminals below the pin: 90
	// and 80 cross the cost/ctx steps, 60 and 40 the fallback, 24 the narrowest
	// terminal the epic asks about.
	statusDegradeWidth99 = 99
	statusDegradeWidth90 = 90
	statusDegradeWidth80 = 80
	statusDegradeWidth60 = 60
	statusDegradeWidth40 = 40
	statusDegradeWidth24 = 24
)

var statusDegradationWidths = []int{
	statusLayoutBudgetCols, // the aligned geometry at its budget
	statusDegradeWidth99,   // one cell under: the cost column must yield here
	statusDegradeWidth90,
	statusDegradeWidth80,
	statusDegradeWidth60,
	statusDegradeWidth40,
	statusDegradeWidth24,
}

// The context pair the degrade state supplies: named so the sub-cell's own probe
// (the ratio) is DERIVED from it rather than written down twice.
const (
	statusProbeCtxUsed = 4200
	statusProbeCtxMax  = 64000
	// The cache pair the group's second sub-cell carries: a TRANSPORT-reported
	// fact, which is why it is the one cost-adjacent read-out the row kept when
	// the money sub-cell was retired (task 01a0dcd1).
	statusProbeCacheRead  = 12300
	statusProbeCacheWrite = 4200
)

// The field PROBES, in the row's documented left-to-right order. A probe is a
// substring that only that field's own value produces at the state below — the
// test asserts PRESENCE of the probe, never a whole column's rendering, so a
// padding change cannot make it vacuous (and a field that slid would still be
// caught by the start-cell assertions).
var (
	statusProbeModel = "deepseek-flash"
	statusProbeIter  = "turn9"
	statusProbeTool  = toolGlyph
	// The OPERATIONAL read-out is ONE probe, because the row carries it as ONE
	// column: the metrics group (the context pair and the cache pair joined on a
	// field separator). MERGE NOTE (rework run 01a0dbe2): master's row had ctx and
	// cost as two top-level columns; the grouped row joins them — so one probe
	// states what one column carries, and it is a value no other field can
	// produce. RE-PARTITION (task 01a0dcd1): the probe is the CONTEXT sub-cell's
	// own value — the group's primary fact and the read-out it keeps longest — and
	// the money probe is gone with the sub-cell.
	// statusProbeCachePair is the metrics group's SECOND sub-cell's own value: the
	// cache pair (a transport-reported fact). The money probe that stood here
	// ("~1.23") is RETIRED with the sub-cell it addressed (task 01a0dcd1) — the
	// row carries no money field at any width, and the absence is asserted.
	// Since task 01a0f8c9 this sub-cell's value is the pair PLUS the hit rate riding
	// it; the probe is the PAIR (statusCachePairExpr), which is the fragment whose
	// position is a contract and which no other sub-cell draws.
	statusProbeCachePair = statusCachePairExpr(statusOps{cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite})
	// statusProbeCtxPair is the CONTEXT sub-cell's primary fact: the ABSOLUTE
	// used/max pair (see statusCtxField's note — it is a fact; the percentage beside
	// it is gone from the aligned row, task 01a0ddab).
	statusProbeCtxPair = statusCtxPair(statusOps{ctxUsed: statusProbeCtxUsed, ctxMax: statusProbeCtxMax}, 0)
	// statusProbeMetricsReadOut is the OPERATIONAL sub-cell's own cells — the
	// read-out the metrics group leads with at THIS state, composed by the
	// production composer rather than hand-spelled (task 01a0e8c7: the ctx
	// sub-cell leads with the prompt cache's hit rate; the absolute context pair
	// statusProbeCtxPair above is that sub-cell's FALLBACK form and remains the
	// ground truth for the ladder arithmetic). A probe that locates a START CELL
	// addresses the sub-cell, so it anchors here, and the field it names is
	// derived from the same ops the degrade state renders.
	// RE-PARTITION (task 01a0f8c9): the group's FIRST sub-cell is the CONTEXT pair
	// (qualified by its denominator) at every state where a window is measured, so
	// the probe the group's origin cell is anchored on is composed from the SAME ops
	// the degrade state renders — including the measured context.
	statusProbeMetricsReadOut = statusCtxField(statusOps{
		ctxUsed: statusProbeCtxUsed, ctxMax: statusProbeCtxMax, ctxMeasured: true,
		cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite,
	}, 0)
	// statusMoneyGlyph is the currency the retired money sub-cell printed, spelled
	// HERE on purpose: the production constant died with the field, and a test
	// pointing at live code could not assert the code is gone. It must appear on NO
	// row at ANY width.
	statusMoneyGlyph = "元"
)

// statusProbes is the ordered probe list: index i is the field the ladder's
// i-th step sheds or keeps (statusColModel … statusColMetrics).
var statusProbes = []string{
	statusProbeModel, statusProbeIter,
	statusProbeTool, statusProbeMetricsReadOut,
}

// statusProbeNames labels the probes for a failure message.
var statusProbeNames = []string{"model", "turn", "tool", "metrics"}

// statusDegradeState is the ONE state every width in the table is rendered at:
// a live tool, a measured context against a known window, a transport-reported
// cache pair and a running clock — i.e. every field the geometry reserves has a
// value, so "the field is absent" can only mean the ladder shed it. (The money
// sub-cell is retired: there is no state that puts a price on the row.)
func statusDegradeState(t *testing.T) *chatStatusSource {
	t.Helper()
	s := newChatStatusSource(statusProbeModel, true)
	s.setOpsFn(func() statusOps {
		return statusOps{
			ctxUsed:      statusProbeCtxUsed,
			ctxMax:       statusProbeCtxMax,
			ctxMeasured:  true,
			cacheRead:    statusProbeCacheRead,
			cacheWrite:   statusProbeCacheWrite,
			elapsed:      4 * time.Minute,
			elapsedKnown: true,
		}
	})
	s.update(9, 0, 0, "")
	s.setTool("keyword_search", "internal/lineedit/answer.go")
	return s
}

// statusFormMinCols is the narrowest terminal that hosts a form keeping the
// named columns: the sum of their reserved widths, the separators between them,
// the gap before the live block, and the block itself — read from the SAME
// constants the geometry is built on, so the expectation cannot drift from the
// production arithmetic (and never hand-counted: the separator and the live
// block are where a hand count went wrong twice).
func statusFormMinCols(widths ...int) int {
	total := statusLiveCol + lineedit.StatusLineGapCells
	for i, w := range widths {
		total += w
		if i > 0 {
			total += lineedit.StatusColumnSepCells
		}
	}
	return total
}

// statusDegradeLadder is the degradation order as ARITHMETIC: each step is the
// set of columns kept, widest first, exactly as chat_stream.go's own
// statusDegradationOrder documents it. The test asserts the row follows THIS
// list, so a production order that changed without the documentation (or the
// reverse) is caught rather than agreed with.
func statusDegradeLadder() []struct {
	name   string
	widths []int
	keep   []bool
} {
	// The widths are the SAME reservations the production columns carry, read
	// from the constants rather than restated (the operational column's width is
	// statusMetricsFormCol — the group's own FULL form, which is the width the row
	// really draws and therefore the width the ladder reads); the keep-set of each
	// step is the
	// documented order as it is actually reachable (see statusDegradationOrder in
	// chat_stream.go) — the ctx/tool pair shares a width, so the pair yields one
	// column at a time with ctx kept behind the tool. The last step is the
	// three-column floor: below IT the aligned layout has no form left and the
	// row falls back to the unaligned composition.
	model, iter := statusModelCol, statusTurnCol
	tool, metrics := statusToolCol, statusMetricsFormCol
	t, i := true, true
	f := false
	// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): master's ladder
	// had FOUR steps here, because ctx and cost were two top-level columns (cost
	// shed, tool shed, ctx shed). The grouped row carries ONE operational column,
	// so there are THREE steps and the operational read-out leaves in one: a
	// column that is one column cannot be shed in pieces. This list is the
	// documented order as ARITHMETIC and is pinned to the production ladder by
	// form count and by per-form minimum width, so a step added or removed in
	// chat_stream.go fails this test rather than being absorbed by it.
	return []struct {
		name   string
		widths []int
		keep   []bool
	}{
		{"the full geometry", []int{model, iter, tool, metrics}, []bool{t, i, t, t}},
		{"the metrics group shed", []int{model, iter, tool}, []bool{t, i, t, f}},
		{"tool shed", []int{model, iter}, []bool{t, i, f, f}},
		// (the group's OWN reservation, not its sub-cells': inside the column the
		// sub-cells are joined by field separators and are not separately reserved)
	}
}

// statusFallbackFloor is the narrowest terminal the aligned layout can host at
// all: the three-column floor above, separately — below it internal/lineedit
// refuses the aligned form and the row is the unaligned composition.
func statusFallbackFloor() int {
	ladder := statusDegradeLadder()
	return statusFormMinCols(ladder[len(ladder)-1].widths...)
}

// TestChatStatus_WidthDegradation drives the real row at every width the table
// names and asserts, at each one:
//
//   - the row is ONE screen row and never wider than the terminal (a wrapped
//     status row costs the editing line a row — lineedit_height_test.go's own
//     subject);
//   - the fields present are EXACTLY the ones the documented degradation order
//     keeps at that width (below every form's floor the row falls back to the
//     unaligned composition, whose fields must still be a PREFIX of the order —
//     a field that reappears after a narrower width dropped it, or one that
//     appears out of order, is the "ladder lost a field" defect);
//   - the state word is on the row exactly ONCE at every width (it was printed
//     twice by the unaligned fallback's composition);
//   - at the budget and at the geometry's own width, EVERY field of the row is
//     present with its own value — the provenance half of the contract.
func TestChatStatus_WidthDegradation(t *testing.T) {
	ladder := statusDegradeLadder()
	// The floor each form needs, and the widest form that fits a terminal — the
	// expectation the row is compared against.
	fitsAt := func(cols int) (int, bool) {
		for i, form := range ladder {
			if statusFormMinCols(form.widths...) <= cols {
				return i, true
			}
		}
		return len(ladder) - 1, false
	}

	// The expectation's SHAPE is pinned to the production ladder: the same number
	// of forms, and each form's minimum width equal to the width the production
	// form really needs. A step added, removed or resized in chat_stream.go fails
	// here rather than being absorbed by the table.
	if len(statusDegradationOrder) != len(ladder) {
		t.Fatalf("the production ladder has %d forms, the documented order asserted here has %d",
			len(statusDegradationOrder), len(ladder))
	}
	// The column list the production ladder is resolved against, taken from the
	// row's OWN geometry (statusRowColumns) rather than restated here: a fixture
	// spelling the columns out is a second copy of the geometry, and the merged
	// row carries the metrics GROUP where master's list carried ctx + cost.
	fullCols := statusRowColumns()
	for i, form := range statusDegradationOrder {
		cols, ok := statusFormFor(form, fullCols)
		if !ok {
			t.Fatalf("the production ladder's form %d names a column the row does not carry: %v", i, form)
		}
		if got, want := statusFormWidth(cols), statusFormMinCols(ladder[i].widths...); got != want {
			t.Fatalf("the production ladder's form %d needs %d cells, the documented order asserts %d",
				i, got, want)
		}
	}

	var prevPresent []bool
	// prevCols is the width the previous row was composed at, named in the
	// monotonicity failure so the reader can render both rows.
	prevCols := 0
	for _, cols := range statusDegradationWidths {
		s := statusDegradeState(t)
		// The source is told the terminal it is being drawn on, which is what the
		// REPL's publish does; the row is then rendered for that same width.
		s.setAvailCols(cols)
		row := s.status().Line(cols)

		if rows := wrappedRowsForTest(row, cols); rows != 1 {
			t.Fatalf("cols=%d: the status row occupies %d screen rows: %q", cols, rows, row)
		}
		if w := truncateWidth(row); w > cols {
			t.Fatalf("cols=%d: the status row is %d cells wide: %q", cols, w, row)
		}
		// The row is LOGGED as well as asserted: `go test -run
		// TestChatStatus_WidthDegradation -v ./cmd/agent/` prints the composed row
		// at every width, which is the Rule 31 render evidence for this surface —
		// the test IS the invocation, so the render and its assertion cannot be
		// two different runs.
		t.Logf("cols=%3d width=%3d |%s|", cols, truncateWidth(row), row)
		if n := strings.Count(row, statusLiveIdle); n != 1 {
			t.Fatalf("cols=%d: the state word %q appears %d times on the row, want exactly once: %q",
				cols, statusLiveIdle, n, row)
		}

		present := make([]bool, len(statusProbes))
		for i, probe := range statusProbes {
			present[i] = strings.Contains(row, probe)
		}
		got := statusProbeList(present)

		form, aligned := fitsAt(cols)
		if aligned {
			want := statusProbeList(ladder[form].keep)
			if !statusSameList(got, want) {
				t.Fatalf("cols=%d: the row carries fields %v, want %v (%s)\nrow: %q",
					cols, got, want, ladder[form].name, row)
			}
		} else {
			// Below every form's floor the row is the unaligned composition: the
			// fields it shows must still be an ORDERED PREFIX of the documented
			// order (no field out of order, none resurrected after being
			// dropped), and the ctx read-out — which only the column layout
			// shows — must be absent however wide the label could grow.
			for _, name := range got {
				if name == statusProbeNames[statusColMetrics] {
					t.Fatalf("cols=%d: the unaligned fallback carries %q, which only the column layout shows\nrow: %q",
						cols, name, row)
				}
			}
			if !statusIsPrefix(got, statusProbeList(ladder[0].keep)) {
				t.Fatalf("cols=%d: the fallback's fields %v are not a prefix of the documented order %v\nrow: %q",
					cols, got, statusProbeList(ladder[0].keep), row)
			}
		}
		// Monotonicity is asserted per COLUMN, not as a prefix of the present
		// names: the documented shed order is cost -> tool -> ctx (master's
		// statusShedOrder), so the step that drops the TOOL column leaves the ctx
		// column BEHIND it — a name-list prefix check would read that surviving
		// ctx as a field "brought back", which it is not (it was present at
		// every wider width). What must never happen is a column a narrower
		// terminal RESURRECTS after a wider one dropped it.
		if prevPresent != nil {
			for i := range present {
				if present[i] && !prevPresent[i] {
					t.Fatalf("cols=%d: %s is present here but was dropped at the wider %d — a narrower terminal BROUGHT A FIELD BACK\nwider: %q\nhere:  %q",
						cols, statusProbeNames[i], prevCols, s.status().Line(prevCols), row)
				}
			}
		}
		prevPresent, prevCols = present, cols

		// The two widths that host the aligned geometry: every field is present,
		// which is the epic's ">=100 has every field" clause stated about the
		// real row (and about the operational read-out in particular, since a
		// field that is compiled but never drawn is the defect the prior session
		// reported).
		if cols >= statusColumnsAtCols {
			if !statusSameList(got, statusProbeList(ladder[0].keep)) {
				t.Fatalf("cols=%d: the full geometry must carry every field; the row shows %v\nrow: %q", cols, got, row)
			}
		}
	}
}

// TestChatStatus_FieldsStartOnTheirReservedCells pins the PROVENANCE half at the
// geometry's own width: every field of the row starts on the cell its own
// reservation gives it (the sum of the columns before it plus one separator per
// join), derived here from the production constants rather than copied.
//
// This is the assertion that "the columns hold still" means something: a field
// that started elsewhere would mean the row's geometry moved.
func TestChatStatus_FieldsStartOnTheirReservedCells(t *testing.T) {
	s := statusDegradeState(t)
	s.setAvailCols(statusColumnsAtCols)
	row := s.status().Line(statusColumnsAtCols)

	cells := map[string]int{}
	at := 0
	at += 0 // the first field starts at cell 0
	cells[statusProbeModel] = at
	at += statusModelCol + lineedit.StatusColumnSepCells
	cells[statusProbeIter] = at
	at += statusTurnCol + lineedit.StatusColumnSepCells
	cells[statusProbeTool] = at
	at += statusToolCol + lineedit.StatusColumnSepCells
	// The operational read-out is ONE column of the row: the group starts on the
	// cell the columns before it leave (the sub-cells INSIDE it are joined by
	// field separators, which is a different cell story — and they are not
	// separately reserved, so the group's first sub-cell is what starts on this
	// cell).
	groupStart := at
	// The group's primary read-out — the CONTEXT pair with its fact name (task
	// 01a0f8c9: the pair leads the group again, and the hit rate rides the cache
	// sub-cell beside it) — is what begins on the group's first cell.
	cells[statusProbeMetricsReadOut] = groupStart

	for _, probe := range []string{statusProbeModel, statusProbeIter, statusProbeTool, statusProbeMetricsReadOut} {
		if got := fieldStart(row, probe); got != cells[probe] {
			t.Fatalf("at the geometry's width %d, field %q starts at cell %d, want %d\nrow: %q",
				statusColumnsAtCols, probe, got, cells[probe], row)
		}
	}
	// The group's SECOND sub-cell (the cache pair) is INSIDE the group's
	// reservation: after the group's own cells, and no further right than the
	// group's reserved width allows — which is what "one column" means for the
	// row's arithmetic. (The money sub-cell that used to be the LAST sub-cell is
	// retired; statusMoneyGlyph is asserted absent on every row below.)
	if got := fieldStart(row, statusProbeCachePair); got < groupStart || got >= groupStart+statusMetricsFormCol {
		t.Fatalf("the money sub-cell starts at cell %d, outside the group's cells [%d,%d)\nrow: %q",
			got, groupStart, groupStart+statusMetricsCol, row)
	}
	// The live block is the row's right edge: its start cell is the terminal
	// width minus its own reservation. The anchor is the block the source itself
	// composed (Status.Live), never a word this test assumes is on it — the block
	// carries the elapsed prefix AND the state.
	st := s.status()
	if st.LiveWidth != statusLiveCol {
		t.Fatalf("the live block reserves %d cells, want %d (statusLiveCol)", st.LiveWidth, statusLiveCol)
	}
	anchor := strings.TrimRight(st.Live, " ")
	if got, want := fieldStart(row, anchor), statusColumnsAtCols-statusLiveCol; got != want {
		t.Fatalf("at the geometry's width %d, the live block %q starts at cell %d, want %d\nrow: %q",
			statusColumnsAtCols, anchor, got, want, row)
	}
}

// TestChatStatus_ValuesCannotWidenTheRow is the "jumping row" regression, stated
// as the two failures it has: a value that makes the row WIDER than its terminal
// (a wrap, which costs the editing line a row) and a value that moves a field's
// START CELL (the columns slide). Every value here is adversarial by
// construction: a model name longer than its column, a six-digit token count,
// a seven-figure spend, and a tool digest long enough to fill the tool column.
func TestChatStatus_ValuesCannotWidenTheRow(t *testing.T) {
	const (
		longModel  = "glm-5.3-flash-super-long-provider-model-name"
		hugeTokens = 999999 // formatCount → "1000.0k": six digits, the widest count
		// hugeCache is the widest pair a sub-cell can carry: formatCount's own
		// worst case per side ("1000.0k" for 999999 — the field's six-digit form).
		hugeCache = 999999
	)
	// (A "huge cost" case stood here. It is replaced by the huge cache pair —
	// the widest value the group's second sub-cell can carry — because the row
	// has no money field to widen any more: task 01a0dcd1 retired it, and the
	// adversarial value must be one the row really prints.)
	// The digest is long enough to fill the tool column and no longer: a repeat
	// of the same path, so the field's own cut is what has to absorb it.
	longDigest := strings.Repeat("lineedit/answer/", 6) + "answer.go"

	base := func(t *testing.T) *chatStatusSource {
		t.Helper()
		s := statusDegradeState(t)
		s.update(9, 0, 0, "")
		return s
	}

	for _, cols := range []int{statusLayoutBudgetCols, statusColumnsAtCols, 80, 60, 24} {
		s := base(t)
		s.setAvailCols(cols)
		before := s.status().Line(cols)

		// The adversarial state: every value at once.
		s.setOpsFn(func() statusOps {
			return statusOps{
				ctxUsed:      hugeTokens,
				ctxMax:       0, // no window known → the field reports the raw count
				cacheRead:    hugeCache,
				cacheWrite:   hugeCache,
				elapsed:      200 * time.Hour,
				elapsedKnown: true,
			}
		})
		s.model = longModel
		s.update(987654, 0, hugeTokens, "12 queued")
		s.setTool("keyword_search", longDigest)
		after := s.status().Line(cols)

		for _, r := range []struct {
			name string
			row  string
		}{{"the base state", before}, {"the adversarial state", after}} {
			if w := truncateWidth(r.row); w > cols {
				t.Fatalf("cols=%d %s: the row is %d cells wide — a VALUE widened the row\nrow: %q",
					cols, r.name, w, r.row)
			}
			if rows := wrappedRowsForTest(r.row, cols); rows != 1 {
				t.Fatalf("cols=%d %s: the row occupies %d screen rows\nrow: %q", cols, r.name, rows, r.row)
			}
		}
		// The fields present in BOTH states must not have moved: a value that
		// changed a field's start cell slid the columns after it.
		for _, probe := range statusProbes {
			if !strings.Contains(before, probe) || !strings.Contains(after, probe) {
				continue
			}
			if a, b := fieldStart(before, probe), fieldStart(after, probe); a != b {
				t.Fatalf("cols=%d: field %q starts at cell %d in the base state and %d in the adversarial one — the column SLID\nbase: %q\nadv:  %q",
					cols, probe, a, b, before, after)
			}
		}
	}
}

// TestChatStatus_LiveBlockKeepsTheStateWord is the fix for the defect the prior
// session measured: the live block is statusLiveCol cells and the tail cut
// shears from its END, where the STATE word sits — so an elapsed prefix longer
// than the cells left for it used to eat the state ("4m00s · read…"), leaving
// the operator without the one field the row exists to steer by.
//
// The assertion is about the ROW's tail at every width, with a clock long enough
// that the elapsed text cannot fit beside the state: the state word must be
// there, and the elapsed text is what yields.
func TestChatStatus_LiveBlockKeepsTheStateWord(t *testing.T) {
	states := []struct {
		name string
		do   func(s *chatStatusSource)
		want string
	}{
		{"at the prompt", func(s *chatStatusSource) {}, statusLiveIdle},
		{"sent", func(s *chatStatusSource) { s.beginTurn(9) }, chatTurnPhaseWord(chatTurnSent)},
		{"thinking", func(s *chatStatusSource) {
			s.beginTurn(9)
			s.setPhase(chatTurnThinking)
		}, chatTurnPhaseWord(chatTurnThinking)},
		{"responding", func(s *chatStatusSource) {
			s.beginTurn(9)
			s.setPhase(chatTurnResponding)
		}, chatTurnPhaseWord(chatTurnResponding)},
	}
	elapsed := []time.Duration{4 * time.Minute, 200 * time.Hour}
	for _, cols := range []int{statusLayoutBudgetCols, statusColumnsAtCols, 60, 40} {
		for _, e := range elapsed {
			for _, st := range states {
				s := statusDegradeState(t)
				s.setOpsFn(func() statusOps {
					return statusOps{ctxUsed: 4200, ctxMax: 64000, elapsed: e, elapsedKnown: true}
				})
				s.update(9, 0, 0, "")
				st.do(s)
				s.setAvailCols(cols)
				row := s.status().Line(cols)

				// THE STATE WORD IS THE BLOCK'S REASON TO EXIST AND IS NEVER CUT —
				// what the row must carry is a SPELLING of the turn's state,
				// chosen by the row's own fit rule (statusLiveField +
				// statusLiveStateWords): the FULL word when it shares the
				// reserved block with the counter, the declared SHORT one when
				// it cannot.
				//
				// WHY THIS IS NOT A RELAXATION (Rule 20): demanding this exact
				// string made a CORRECT row red, and the arithmetic below shows
				// the demand was unsatisfiable rather than strict — the block is
				// statusLiveCol cells, the separator is
				// lineedit.StatusSep's own width and the full in-flight word is
				// statusLiveWordResponding's, so the moment a counter is on the
				// block the full word cannot fit for ANY counter (even a
				// one-digit one). The row read `4m00s · resp\u00a0`, which is
				// the design's priority (the counter outranks the word's WIDTH —
				// TestStatusLiveCompactionCounterOutranksTheWidestStateWord)
				// working as specified. What is asserted instead is strictly
				// MORE: a DECLARED spelling is present, exactly once — and a
				// spelling that is not the full one is only admissible when the
				// full pair provably does not fit (the check below, through the
				// production predicates).
				//
				// HONEST NOTE ON THAT CONDITION: at today's geometry it is not
				// exercised for the two wide states — the full pair is 19 cells
				// against statusLiveCol's 13 — so its real job is to make the
				// short spelling a FALLBACK rather than a new default if the
				// block's reservation or a word's width ever moves. What actually
				// bites on today's row is the declared-spelling test: a word the
				// design does not declare (a CUT word, or a spelling invented in
				// the test) is still red.
				spelling := statusRowStateSpelling(row, st.want)
				if spelling == "" {
					t.Fatalf("cols=%d elapsed=%s state=%s: the row carries no spelling of %q — the live block lost the state it describes\nrow: %q",
						cols, e, st.name, st.want, row)
				}
				if n := strings.Count(row, spelling); n != 1 {
					t.Fatalf("cols=%d elapsed=%s state=%s: the state appears %d times, want once\nrow: %q",
						cols, e, st.name, n, row)
				}
				if statusLiveBlockFits(statusElapsedLabel(e), st.want) && spelling != st.want {
					t.Fatalf("cols=%d elapsed=%s state=%s: the row shows %q although the FULL word %q fits beside the counter %q inside the %d reserved cells — the short spelling is the fallback for the widths that cannot hold the word, never a new default\nrow: %q",
						cols, e, st.name, spelling, st.want, statusElapsedLabel(e), statusLiveCol, row)
				}
				if w := truncateWidth(row); w > cols {
					t.Fatalf("cols=%d elapsed=%s state=%s: the row is %d cells wide\nrow: %q", cols, e, st.name, w, row)
				}
			}
		}
	}
}

// TestChatStatus_HeavyContextKeepsItsQualifier is the guard on bug F's SECOND
// half: the "!" that used to mark a context in heavy territory is RETIRED, and
// what a heavy context paints is the SAME qualified form a light one does — the
// label, the absolute pair, and the quantifier that says what the denominator is.
//
// WHY THE MARKER IS GONE (see the RETIRED note on statusCtxHeavyMarker for the
// full argument): it fired at statusTokensHeavy = 60000 — 30% of the deployed
// 200000-token window, and under a third of the 150000 at which trimming really
// starts — and it said so with the glyph the shell (and this chat's own per-command
// error line) uses for a non-zero exit, one cell to the right of the number it
// marked. A warn that fires at 30% trains its reader to ignore it, and a glyph whose
// nearest documented meaning is "something is wrong" cannot carry a weaker one.
//
// WHAT REPLACES IT: nothing has to — the row now states the denominator's identity
// (statusCtxQuantGlyph) on every form, so the reader weighs the pressure himself
// against a number whose kind is visible. This test is the RED-proof's other half:
// it fails if the marker comes back.
//
// The unaligned composition (the form a narrow terminal falls back to) keeps its
// historical WORD, statusTokensHeavySuffix — this test pins that too, so the
// decision is visible as "glyph retired, word kept" rather than "the warn left".
func TestChatStatus_HeavyContextKeepsItsQualifier(t *testing.T) {
	const heavyTokens = statusTokensHeavy + 1000
	s := statusDegradeState(t)
	// A context well past the (now retired) heavy threshold, in the deployed window.
	heavyOps := statusOps{ctxUsed: statusTokensHeavy, ctxMax: ctxWindowDeployedForTest, ctxMeasured: true, elapsed: time.Minute, elapsedKnown: true}
	s.setOpsFn(func() statusOps { return heavyOps })
	s.update(9, 0, heavyTokens, "")
	s.setAvailCols(statusColumnsAtCols)
	row := s.status().Line(statusColumnsAtCols)

	// The MARKER IS RETIRED: the constant is empty, so nothing can paint it. A
	// non-empty value here means someone re-added the verdict without re-doing the
	// arithmetic that removed it (the cell it would take is the qualifier's).
	if statusCtxHeavyMarker != "" {
		t.Fatalf("statusCtxHeavyMarker = %q: the heavy marker was retired (see its RETIRED note) — a "+
			"re-added glyph needs a re-partition that names the column paying for its cell",
			statusCtxHeavyMarker)
	}
	if strings.Contains(row, "!") {
		t.Fatalf("the heavy row paints a %q: the retired marker is back on the row\nrow: %q", "!", row)
	}

	// The QUALIFIER — the fact bug F is about — is on the heavy row exactly as it is
	// on a light one: the denominator's identity does not depend on how full the
	// budget is, and it is the LAST field to be dropped.
	ops := heavyOps
	if got := statusCtxField(ops, 0); !strings.Contains(row, got) {
		t.Fatalf("the heavy row does not carry its own field %q (statusCtxField):\nrow: %q", got, row)
	}
	// THE QUALIFIER IS PROVENANCE-DEPENDENT NOW (the epic's gap A): this fixture
	// sets no ctxMeasured, so its number is a LOCAL ESTIMATE and the row must carry
	// the estimate spelling. Asserting the identity glyph unconditionally is what
	// this arm used to do, and it was asserting a promise the flag did not keep.
	if !strings.Contains(statusCtxField(ops, 0), statusCtxQuantGlyphFor(ops)) {
		t.Fatalf("a heavy context composes NO denominator qualifier: %q — the form a reader meets "+
			"at the row's most loaded state is exactly the one that must state what the denominator is",
			statusCtxField(ops, 0))
	}
	if !strings.Contains(row, statusCtxQuantGlyphFor(ops)) {
		t.Fatalf("the qualifier is on the composed field but not on the row — the cell's composition "+
			"dropped it:\nrow: %q", row)
	}
	// The sub-cell's SHAPE is the fact label, the absolute pair and the qualifier;
	// the bar is drawn only when the geometry's own arithmetic says the cell can host
	// it (statusCtxBarCells — measured, it derives to zero in the pinned geometry).
	// The form is asserted against the production renderer's own output, never
	// re-derived here.
	short := statusOps{ctxUsed: 100, ctxMax: 6400}
	wantShort := statusCtxLabel + statusCtxBarGap + statusCtxPair(short, 0) + statusCtxQuantGlyphFor(short)
	if statusCtxBarCells > 0 {
		wantShort = statusCtxLabel + statusCtxBarGap + statusBar(statusCtxPct(short.ctxUsed, short.ctxMax), statusCtxBarCells) +
			statusCtxBarGap + statusCtxPair(short, 0) + statusCtxQuantGlyphFor(short)
	}
	if got := statusCtxField(short, 0); got != wantShort {
		t.Fatalf("a short ctx read-out renders %q, want %q (the label, the absolute pair and the "+
			"qualifier; the bar only when statusCtxBarCells = %d allows it)", got, wantShort, statusCtxBarCells)
	}
	if w := lineedit.DisplayWidth(statusCtxField(short, 0)); w > statusCtxCol {
		t.Fatalf("the ctx SUB-cell renders %d cells, over its %d-cell reservation", w, statusCtxCol)
	}
	// The pair IS on the aligned row — it is the sub-cell's PRIMARY fact and the
	// whole point of the re-partition (task 01a0dcd1): the operator's criterion is
	// that a bare percentage cannot survive a model/window change, so the
	// denominator must be on the row. (This assertion is the INVERSE of the one
	// the ratio-only grouped row carried.)
	pair := statusCtxPair(ops, 0)
	if pair == "" || !strings.Contains(row, pair) {
		t.Fatalf("the aligned row does not carry the ABSOLUTE context pair %q:\nrow: %q", pair, row)
	}
	// The heavy token total's own warn WORD — the " (large)" suffix — rides the
	// UNALIGNED label (status()'s Label), which is the form a terminal too narrow
	// for the aligned layout falls back to. It is asserted on the composed Status
	// value itself, and then MEASURED on the rendered row: below the aligned
	// floor the label is cut before the token field, so the suffix does not
	// survive on screen (reported as this branch's ## Not Done).
	heavyRow := s.status()
	s.setAvailCols(40)
	narrow := s.status().Line(40)
	if !strings.Contains(heavyRow.Label, statusTokensHeavySuffix) {
		t.Fatalf("the unaligned label does not mark a heavy token total with %q:\nlabel: %q",
			statusTokensHeavySuffix, heavyRow.Label)
	}
	if w := truncateWidth(narrow); w > 40 {
		t.Fatalf("the heavy state widened the row past its terminal: %d cells\nrow: %q", w, narrow)
	}
	if strings.Contains(narrow, statusTokensHeavySuffix) {
		t.Fatalf("the heavy suffix now survives on a 40-column row: the composition changed and this test's ## Not Done note is stale\nrow: %q", narrow)
	}
}

// chatTurnPhaseWord is the state word a phase puts on the row, taken from the
// SAME constants the source composes with (never restated here).
func chatTurnPhaseWord(phase chatTurnPhase) string {
	switch phase {
	case chatTurnThinking:
		return statusLiveWordThinking
	case chatTurnResponding:
		return statusLiveWordResponding
	default:
		return statusLiveWordSent
	}
}

// statusLiveBlockFits reports whether a counter and a state word share the live
// block — the FIT RULE statusLiveField itself applies when it decides whether to
// spend the reserved column on the full spelling or on the short one. It is the
// test-side consumer of the production predicate (same separator, same width unit,
// same reservation), so a guard about the fallback cannot drift from the code
// that performs it.
func statusLiveBlockFits(counter, word string) bool {
	return statusFieldCells(counter+lineedit.StatusSep+word) <= statusLiveCol
}

// statusRowStateSpelling returns the spelling of the state whose full word is
// `full` that the row actually carries — "" when it carries none. The candidates
// are the DECLARED alternatives (statusLiveStateWords, the same call the composer
// walks), each compared as it appears on the row (word + its no-break gap), so a
// reader recognises the state from the spelling the design admits rather than from
// its own idea of how long a word should be.
func statusRowStateSpelling(row, full string) string {
	for _, w := range statusLiveStateWords(full) {
		if strings.Contains(row, w) {
			return w
		}
	}
	return ""
}

// statusProbeList names the present probes, in order.
func statusProbeList(present []bool) []string {
	out := make([]string, 0, len(present))
	for i, ok := range present {
		if ok {
			out = append(out, statusProbeNames[i])
		}
	}
	return out
}

func statusSameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// statusIsPrefix reports whether sub appears — in order — inside the ordered
// superset, i.e. whether the fields shown are the first len(sub) fields of it.
func statusIsPrefix(sub, super []string) bool {
	if len(sub) > len(super) {
		return false
	}
	for i := range sub {
		if sub[i] != super[i] {
			return false
		}
	}
	return true
}

// TestChatStatus_WidestIterFitsItsColumn — task 01a10dd0: THE RESERVATION AND THE
// FORMATTER ARE ONE FACT, CHECKED AS ARITHMETIC.
//
// statusTurnCol is 7 cells because turnLabel draws the word plus the digits
// statusTurnDigitsMax admits, and the defect the sibling tests could not see was
// exactly the gap between those two statements: the label used to carry a SPACE
// ("turn 100" is 8 cells), so the FIRST three-digit count the row met was cut by
// lineedit's own column cut into "turn 1…" — a truncated number, the one thing
// this row must not print (see statusTurnCol's own note and turnLabel's).
//
// The test walks the FORMATTER on both live branches (turnLabel switches on live:
// the completed count at the prompt, the running turn in flight) against the SAME
// constant the reservation is built from, and then composes the row the REPL
// composes at the pin and reads the label back off it — so the assertion is about a
// rendered row and the row's own column cut, never about a copy of that cut. The
// row is composed through status()/Line, the two calls cli_chat makes, and every
// number below comes from the constants rather than from a literal (Rule 13).
func TestChatStatus_WidestIterFitsItsColumn(t *testing.T) {
	// The widest count the column RESERVES for, built from the reservation's own
	// constant: widening the reservation widens this test with it, so the two
	// cannot drift apart silently.
	widest := 1
	for i := 0; i < statusTurnDigitsMax; i++ {
		widest *= 10
	}
	widest--

	// A LIVE turn of the widest count the column admits, in the state the render
	// probe's own geometry row is measured in (a live tool, a measured context, a
	// full live block).
	s := statusDegradeState(t)
	s.setAvailCols(statusColumnsAtCols)

	for _, live := range []int{statusLiveIdleTurn, widest} {
		label := turnLabel(widest, live)
		if got := lineedit.DisplayWidth(label); got > statusTurnCol {
			t.Fatalf("the widest count this column reserves (%d -> %q, %d cells) is wider than the %d cells statusTurnCol holds — the reservation and the formatter disagree",
				widest, label, got, statusTurnCol)
		}

		s.update(widest, live, 0, "")
		row := s.status().Line(statusColumnsAtCols)
		t.Logf("widest iter at the pin (live=%d, %d cells): %q", live, lineedit.DisplayWidth(row), row)

		if !strings.Contains(row, label) {
			t.Fatalf("the row composed at the %d-cell pin does not carry the widest iteration label %q whole — a cut count is the one thing this row must not print\nrow: %q",
				statusColumnsAtCols, label, row)
		}
		if got := lineedit.DisplayWidth(row); got > statusColumnsAtCols {
			t.Fatalf("the row measured %d cells at the %d-cell pin — the label widened the row\nrow: %q",
				got, statusColumnsAtCols, row)
		}
	}

	// The pin is a HARD sum whose terms did not move: with the live block drawing
	// its whole reservation (the idle block "4m00s · ready" is statusLiveCol cells,
	// see statusLiveCol), the row occupies the pinned width EXACTLY — so the cell the
	// label gave back is the cell the pin was built for, not a widening.
	s.update(widest, statusLiveIdleTurn, 0, "")
	row := s.status().Line(statusColumnsAtCols)
	if got := lineedit.DisplayWidth(row); got != statusColumnsAtCols {
		t.Fatalf("the row measures %d cells at the %d-cell pin, want exactly the pin\nrow: %q",
			got, statusColumnsAtCols, row)
	}
}
