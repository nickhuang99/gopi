package main

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// chat_status_ladder_test.go — the status row's DEGRADATION LADDER, pinned per
// terminal width at the seam the row is really composed through.
//
// The bug this file exists for (task 01a0dbc7): the ladder was unreachable on a
// live REPL. The width the row was composed for (availCols) reached the source
// only through publishChatStatus -> setAvailCols, and when the live loop's
// publish path moved to chatTurnStatus that helper stopped being called at all —
// so the composer read 0 on every terminal, fell back to the pinned geometry, and
// below the pin NO form survived internal/lineedit's aligned-fit check. Measured
// on a real pty at 100/95/80 columns: the aligned row appeared at 100, and at
// 95/80 the row was the unaligned composition ("deepseek-flash · turn 0 · tools
// 0s · ready", 43 cells), never a shed aligned form.
//
// What is pinned here, per width, in ONE table:
//
//  1. the FORM the ladder selects — which columns survive, and that they yield in
//     documented order (cost first, then tool, and ctx LAST). A CACHE column
//     used to head that order and is RETIRED (task 01a0dbe2): the row carried
//     no such column at any width, so the step was a silent no-op — see the
//     RETIRED note on statusCacheCol, and
//     TestStatusLadder_EveryNameIsAColumnOfTheRow, which now fails on a
//     shed-order name with no column behind it;
//  2. that the row really is the ALIGNED composition, by asserting on the
//     separator only the aligned row is joined with (lineedit.StatusColumnSep);
//     the unaligned fallback joins with the narrower StatusSep, so this is the
//     assertion whose absence let the whole table pass while the row degraded;
//  3. that the state word is never cut — the live block is a reserved column, so
//     the elapsed label yields to it rather than truncating it.

// ladderElapsed is the session age the fixture composes at: a realistic grown
// label ("4m00s", 5 cells) rather than "0s", so the live block is exercised near
// its worst case on every row in the table. That is the reading at which the
// pre-fix row cut the state word.
const ladderElapsed = 4 * time.Minute

// composeLadderRow builds the status row exactly as the REPL does — through the
// source's own status() accessor, with the terminal width arriving through the
// viewport HOOK (never through a value pushed at publish time) — and renders it
// at cols the way the editor's read loop does.
//
// The ops are pinned to values where every column has something to say, so "this
// column was shed" can never be confused with "this column had nothing to show":
// a valueless column is padded, not dropped, and the assertions would then pass
// for the wrong reason. Every column the row carries is given a value here;
// there is no exception left to state, and that is the retirement's own point:
// the fixture used to have to explain away a column that never appeared.
func composeLadderRow(t *testing.T, cols int) lineedit.Status {
	t.Helper()
	src := newChatStatusSource("deepseek-flash", false)
	src.setOpsFn(func() statusOps {
		return statusOps{
			ctxUsed: ladderCtxUsedForTest, ctxMax: ladderCtxWindowMax, ctxMeasured: true,
			cacheRead: ladderCacheReadForTest, cacheWrite: ladderCacheWriteForTest,
			elapsed: ladderElapsed, elapsedKnown: true,
		}
	})
	src.setViewportFunc(func() int { return cols })
	src.setTool("keyword_search", "internal/lineedit/region.go")
	src.update(1, statusLiveIdleTurn, ladderCtxUsedForTest, "")
	return src.status()
}

// ladderCtxWindowMax is the window the fixture's context is measured against, and
// ladderCtxWindow is the marker its sub-cell then carries on the row: the ABSOLUTE
// pair, which is the sub-cell's primary fact (task 01a0dcd1). It is DERIVED from
// the same pair the fixture supplies, so the marker and the field cannot drift.
const ladderCtxWindowMax = 200000

// ladderCacheReadForTest / ladderCacheWriteForTest give the metrics group's SECOND
// sub-cell a value, so a row that does not carry the cache pair at a given width
// means the group was toggled — never that the sub-cell had nothing to say (a
// valueless sub-cell is padded, not dropped, and the assertion would then pass for
// the wrong reason).
const (
	ladderCacheReadForTest  = 12300
	ladderCacheWriteForTest = 4200
)

// ladderCtxWindow is the ABSOLUTE context pair the fixture's ops produce — the
// sub-cell's FALLBACK form (task 01a0e8c7: the pair is drawn while the cache
// counters are unmeasured, which is what ladderShedMarker's context arm asserts
// is absent once the group is shed).
var ladderCtxWindow = statusCtxPair(statusOps{ctxUsed: ladderCtxUsedForTest, ctxMax: ladderCtxWindowMax}, 0)

// ladderMetricsMarker is the metrics group's own marker on the row: the read-out
// its FIRST sub-cell draws at this fixture's counters, composed by the production
// composer (task 01a0e8c7 — the sub-cell leads with the prompt cache's hit rate).
// It is what proves the group is on the row, and it is derived from the same ops
// the fixture renders, so the marker and the field cannot drift (Rule 13).
var ladderMetricsMarker = statusCtxField(statusOps{
	cacheRead: ladderCacheReadForTest, cacheWrite: ladderCacheWriteForTest,
	ctxUsed: ladderCtxUsedForTest, ctxMax: ladderCtxWindowMax, ctxMeasured: true,
}, ladderCtxUsedForTest)

// ladderCtxUsedForTest is the token count the fixture's ctx column reads: it is
// the same number statusOps.ctxUsed carries, so the unmeasured-token fallback in
// statusCtxBase cannot make the column's content a function of the fixture.
const ladderCtxUsedForTest = 12600

// TestStatusLadder_FormPerWidth is the table the epic asks for: one row per
// width, stating which columns survive, in which order they yield, and that the
// result is the ALIGNED composition.
func TestStatusLadder_FormPerWidth(t *testing.T) {
	cases := []struct {
		name string
		cols int
		// tool / ctx / cost record whether that read-out survives at this width.
		//
		// MERGE NOTE (rework run 01a0dbe2, the metrics group x master): the ladder's
		// order here is the GROUP -> tool -> the identity columns, so the group's
		// read-outs go false TOGETHER, at the step that sheds the group. They used to
		// be top-level columns and therefore separate steps; a column that is one
		// column cannot be shed in pieces, and pretending otherwise is the
		// phantom-step defect the guard below exists for. Inside the group the
		// documented priority is still the group's own left-to-right policy: a tail
		// cut takes cache first and keeps the context read-out longest.
		//
		// The widths are the ladder's own form widths (98 / 69 / 49 — the pin, then
		// the tool form, then the identity set) rather than a spread chosen for
		// looks: 70 is the interesting one, because 69 <= 70 means the tool read-out
		// SURVIVES there and the identity-only fallback starts below 69.
		//
		// RE-PARTITION (task 01a0dcd1): `cost` is no longer "is the money sub-cell on
		// the row" but the ABSENCE PROOF — the row carries no money field at ANY
		// width, so every case below states false and the table fails the day a
		// money rendering comes back.
		tool bool
		ctx  bool
		cost bool
	}{
		// cost is false at EVERY width: the money sub-cell is RETIRED, not merely
		// shed — the row carries no price even where every column fits.
		{name: "at the pin: the full geometry", cols: statusColumnsAtCols, tool: true, ctx: true, cost: false},
		{name: "under the pin: the metrics group yields first (it is ONE column, so the context and cache read-outs go with it)", cols: 95, tool: true, ctx: false, cost: false},
		{name: "narrower: the SAME step already fits, so the tool read-out stays (69 cells survive)", cols: 80, tool: true, ctx: false, cost: false},
		{name: "the tool-keeping form still fits there: tool is the LAST counter, so it stays", cols: 70, tool: true, ctx: false, cost: false},
		{name: "well under it: tool goes too (49 cells survive)", cols: 60, tool: false, ctx: false, cost: false},
		{name: "narrowest: the identity columns alone", cols: 50, tool: false, ctx: false, cost: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := composeLadderRow(t, tc.cols)
			row := st.Line(tc.cols)

			// 1. The composer pinned the width the terminal has, capped by the pin
			//    — the ladder's input is the terminal, never the pinned geometry
			//    when the terminal is narrower than it.
			wantLayout := tc.cols
			if wantLayout > statusColumnsAtCols {
				wantLayout = statusColumnsAtCols
			}
			if st.LayoutCols != wantLayout {
				t.Errorf("LayoutCols = %d at cols=%d, want %d", st.LayoutCols, tc.cols, wantLayout)
			}

			// 2. The ALIGNED composition is the only one that joins its columns
			//    with StatusColumnSep. This is the assertion the missing table let
			//    slip: every column assertion below can hold while the row is the
			//    unaligned fallback, which is exactly what the bug produced.
			if !strings.Contains(row, lineedit.StatusColumnSep) {
				t.Fatalf("cols=%d: the row is not the aligned composition (no %q separator)\nrow = %q",
					tc.cols, lineedit.StatusColumnSep, row)
			}

			// 3. The shed order, read off the row by each column's own marker: the
			//    tool column's glyph, the context sub-cell's ABSOLUTE pair (the
			//    group's primary fact), and — for the retired money sub-cell — the
			//    currency glyph, which must be absent at EVERY width. A marker is
			//    chosen so that no OTHER column can carry it.
			for _, marker := range []struct {
				column string
				text   string
				want   bool
			}{
				{ladderToolName, toolGlyph, tc.tool},
				{ladderMetrics, ladderMetricsMarker, tc.ctx},
				{ladderCost, statusMoneyGlyph, tc.cost},
			} {
				if got := strings.Contains(row, marker.text); got != marker.want {
					t.Errorf("cols=%d: column %s on the row = %v, want %v (marker %q)\nrow = %q",
						tc.cols, marker.column, got, marker.want, marker.text, row)
				}
			}

			// 4. The CACHE COLUMN is RETIRED (task 01a0dbe2) — but the cache PAIR is
			//    not: the metrics group's second sub-cell carries it, and the arrows
			//    are that sub-cell's own vocabulary. What must not come back is the
			//    top-level column, so the assertion is a CELL one rather than a glyph
			//    one: the pair may appear only inside the metrics group's cells, which
			//    is what "a sub-cell, not a column" means for the row's arithmetic. A
			//    column of its own would sit at its own reserved start cell, outside
			//    the group — and would also need the cells its separator costs, which
			//    the budget does not have (see statusCacheCol).
			if pair := statusCacheField(statusOps{cacheRead: statusProbeCacheRead, cacheWrite: statusProbeCacheWrite}); strings.Contains(row, pair) {
				groupStart := fieldStart(row, statusProbeModel)
				if tc.ctx {
					groupStart = fieldStart(row, ladderMetricsMarker)
				}
				got := fieldStart(row, pair)
				if groupStart < 0 || got < groupStart || got >= groupStart+statusMetricsCol {
					t.Errorf("cols=%d: the cache pair starts at cell %d, outside the metrics group's cells "+
						"[%d,%d) — the retired cache COLUMN has come back\nrow = %q",
						tc.cols, got, groupStart, groupStart+statusMetricsCol, row)
				}
			}

			// 5. The state word survives at its worst realistic label: this is the
			//    half of the defect where a grown elapsed label used to cut the
			//    state word with the row's ellipsis.
			if !strings.Contains(row, statusLiveIdle) {
				t.Errorf("cols=%d: the state word %q is missing (the live block was cut)\nrow = %q",
					tc.cols, statusLiveIdle, row)
			}

			// 6. An aligned row is exactly one screen row: a row wider than the
			//    terminal wraps, and a wrapped status row costs the editing line.
			if w := lineedit.DisplayWidth(row); w > tc.cols {
				t.Errorf("cols=%d: the row is %d cells wide (over the terminal)\nrow = %q", tc.cols, w, row)
			}
		})
	}
}

// TestStatusLadder_LiveBlockIsReserved pins the second half of the defect: the
// live block is a RESERVED column, so the elapsed label and the queued note yield
// to the state word instead of widening the block and truncating it.
//
// Before the fix the elapsed label was appended unconditionally, so the block's
// real width grew with the session's age ("4m00s" is three cells wider than "0s")
// while statusLiveCol stayed a fixed reservation — at the pin that overflowed the
// row's own budget, and the state word was cut with the row's ellipsis.
func TestStatusLadder_LiveBlockIsReserved(t *testing.T) {
	cases := []struct {
		name    string
		elapsed time.Duration
		known   bool
		queued  string
		// wantElapsed / wantQueued record whether each suffix survives: they ride
		// the block while it has room and are left OFF WHOLE when it does not
		// (never shown cut, which would make the block's width jump and re-open
		// the defect this reservation exists to close).
		wantElapsed bool
		wantQueued  bool
	}{
		{name: "no clock: the block is the state word alone", elapsed: 0, known: false, wantElapsed: false},
		{name: "an hours-deep label fits beside the state word (the worst case)", elapsed: time.Hour + 2*time.Minute, known: true, wantElapsed: true},
		{name: "with a grown label there is no room left for a note", elapsed: time.Minute, known: true, queued: ladderQueuedNote, wantElapsed: true, wantQueued: false},
		{name: "with the clock unknown, a short note rides the block", elapsed: 0, known: false, queued: ladderShortQueuedNote, wantElapsed: false, wantQueued: true},
		{name: "a note too long for the block is left off whole, never cut", elapsed: 0, known: false, queued: ladderQueuedNote, wantElapsed: false, wantQueued: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := statusLiveField(statusLiveIdle, tc.queued, statusOps{elapsed: tc.elapsed, elapsedKnown: tc.known})
			// The reservation is the contract: whatever the content, the block
			// never exceeds the cells the row's arithmetic budgeted for it.
			if w := statusFieldCells(got); w > statusLiveCol {
				t.Fatalf("the live block is %d cells, over the reserved column's %d: %q", w, statusLiveCol, got)
			}
			if tc.known && tc.elapsed > 0 {
				want := statusElapsedLabel(tc.elapsed)
				if gotElapsed := strings.Contains(got, want); gotElapsed != tc.wantElapsed {
					t.Errorf("elapsed %q on the block = %v, want %v (block = %q)", want, gotElapsed, tc.wantElapsed, got)
				}
			}
			if tc.queued != "" {
				if gotQueued := strings.Contains(got, tc.queued); gotQueued != tc.wantQueued {
					t.Errorf("queued %q on the block = %v, want %v (block = %q)", tc.queued, gotQueued, tc.wantQueued, got)
				}
			}
			// A suffix that did NOT fit is absent WHOLE — a partly shown note
			// would be the truncation this test exists to refuse.
			if !tc.wantQueued && tc.queued != "" && strings.Contains(got, statusEllipsisForTest) {
				t.Errorf("the block shows a CUT suffix (%q): %q", statusEllipsisForTest, got)
			}
			// The state word is never sacrificed to a suffix — the defect the
			// unconditional append produced.
			if !strings.Contains(got, statusLiveIdle) {
				t.Errorf("the state word %q is missing from the block %q", statusLiveIdle, got)
			}
		})
	}
}

// TestStatusLadder_ReachableOnScreenBelowThePin is the WIRING half of the defect,
// asserted where the operator actually sees it: the rendered STATUS ROW on a
// narrow terminal.
//
// The table above pins what the COMPOSER does with a width. This one pins that the
// live wiring HANDS it the terminal's width at all — the bug was never in the
// composer, it was that nothing called it with a smaller number than the pin
// (availCols reached the source only through publishChatStatus, which the live
// loop stopped calling when chatTurnStatus took over the publish path).
//
// It is deliberately written the way the REPL wires it — SetStatus plus
// setViewportFunc(input.bandCols) — because a test that handed the width over by
// hand would pass on the broken tree. Measured against the pre-fix wiring
// (setViewportFunc removed): this test fails with the unaligned row on screen,
// and passes with the source composed for a width under the pin.
func TestStatusLadder_ReachableOnScreenBelowThePin(t *testing.T) {
	// ladderNarrowCols is the width this test reads the row at: comfortably under
	// the pin, so the composer MUST shed a column to fit, and wide enough that the
	// shed form is not the identity-only fallback (which is not aligned either).
	const ladderNarrowCols = 80
	const rows = 12
	h, status := heightHarness(t, rows, ladderNarrowCols)
	// The wiring under test: exactly the two lines the REPL installs.
	status.setViewportFunc(h.in.bandCols)
	h.in.SetStatus(status.status)
	status.setTool("keyword_search", "internal/lineedit/region.go")
	status.update(1, statusLiveIdleTurn, ladderCtxUsedForTest, "")
	// The publish is a flag plus a wake (the read loop is the only writer): the
	// width itself is NOT pushed here — it is read at compose time through the
	// hook installed above, which is the behaviour under test.
	h.in.PublishStatus()

	h.waitFor(t, "the status row to be drawn at the narrow width", func() bool {
		h.replay()
		return strings.TrimSpace(h.screen.Line(h.layout().StatusRow)) != ""
	})
	h.replay()
	row := h.screen.Line(h.layout().StatusRow)

	// The row the terminal holds is the ALIGNED composition: only it joins its
	// columns with StatusColumnSep. Below the pin there is room for a SHED form —
	// the whole point of the ladder — so a fallback to the unaligned row here is
	// the defect, not a legitimate degradation.
	if !strings.Contains(row, lineedit.StatusColumnSep) {
		t.Fatalf("the status row on an %d-column terminal is NOT aligned: %q\n"+
			"the ladder must shed a column to fit, never fall back to the unaligned composition",
			ladderNarrowCols, row)
	}
	// The ladder yields in the documented order — the metrics GROUP, then tool —
	// so at this width the group (the context pair and the cache pair together) is
	// gone while the tool read-out is still there. The group is ONE column whose
	// width the geometry RESERVES, so it cannot be shed in pieces: the context
	// read-out does NOT outlive the tool column here, and that is the union's
	// stated trade — the grouped row states the first step as one step, rather than
	// keeping a priority it cannot draw.
	//
	// (Master's ladder had cost -> tool -> ctx and this assertion read "ctx
	// outlives tool". With one operational column the atomicity is what decides,
	// and TestStatusLadder_EveryNameIsAColumnOfTheRow is the guard that no name in
	// the order lacks a column behind it.)
	if strings.Contains(row, statusMoneyGlyph) {
		t.Errorf("a money field survives at %d columns — the row carries NONE at ANY width (task 01a0dcd1): %q", ladderNarrowCols, row)
	}
	if strings.Contains(row, ladderMetricsMarker) {
		t.Errorf("the metrics read-out survives at %d columns — it rides the group, which is the ladder's FIRST shed: %q", ladderNarrowCols, row)
	}
	if !strings.Contains(row, toolGlyph) {
		t.Errorf("the tool column is missing at %d columns — it is shed AFTER the group: %q", ladderNarrowCols, row)
	}
	// One screen row, and it ends on the state word rather than on a cut field.
	if w := lineedit.DisplayWidth(row); w > ladderNarrowCols {
		t.Errorf("the status row is %d cells wide on an %d-column terminal", w, ladderNarrowCols)
	}
	// The LIVE BLOCK is what must not be cut, and it is the row's tail: the
	// aligned composition trims the block's padding, so a whole live block means
	// the row ENDS on its state word. (A long MODEL name may legitimately end in
	// the ellipsis inside its own column — that is the field cut working, pinned
	// by TestChatStatus_LongLeadingFieldIsCutWithTheEllipsis — so the assertion is
	// scoped to the block rather than to the row.)
	if tail := strings.TrimRight(row, " "); !strings.HasSuffix(tail, statusLiveIdle) {
		t.Errorf("the status row's live block is cut (row does not end on %q): %q", statusLiveIdle, row)
	}
}

// The names and values this file's table speaks in. They are constants here
// rather than literals in the assertions so the column a table row refers to and
// the marker it is read by cannot drift apart silently.
const (
	// ladderCacheGlyphRead / ladderCacheGlyphWrite are the RETIRED top-level cache column's
	// direction markers, spelled here on purpose: the production constants are
	// deleted with the column, so these two are literal facts about a rendering
	// that must NOT come back — not references to live code that could be renamed
	// and quietly stop meaning anything.
	ladderCacheGlyphRead  = "↑"
	ladderCacheGlyphWrite = "↓"

	ladderTool     = "tool"
	ladderToolName = "tool"
	// ladderMetrics is the GROUP's name in the documented order (it is ONE column:
	// see statusColumnNameMetrics).
	ladderMetrics = "metrics"
	// ladderCost is the money sub-cell's name. The sub-cell itself is RETIRED
	// (task 01a0dcd1) — the row carries no money field at any width — so the name
	// now labels the ABSENCE PROOF: the table asks whether a money rendering
	// appears on the row, and the answer must be false everywhere.
	ladderCost = "cost"
	// ladderCtxPct is the percentage the fixture's own pair produces, DERIVED from
	// the production formatter so the marker and the field cannot drift apart. The
	// grouped sub-cell prints the ABSOLUTE pair as its primary fact and this
	// percentage beside it (task 01a0dcd1), so the percentage alone is still a
	// marker no other column can produce — and ladderCtxWindow (ladderCtxPair's
	// twin) is the pair itself.
	ladderCtxPct = "6%"
	// The currency the RETIRED money sub-cell printed is statusMoneyGlyph, defined
	// beside the degradation table's probes (statusProbeCachePair's block in
	// chat_status_degradation_test.go) so BOTH absence proofs share ONE subject —
	// and the production constant died with the field, so neither of them could
	// reference live code to state that the field is gone.
	ladderQueuedNote = "queued 1"
	// ladderShortQueuedNote is a note short enough to ride the reserved block
	// beside the state word: it is what proves the queued text is not simply
	// discarded by the fix.
	ladderShortQueuedNote = "q1"
	// statusEllipsisForTest is the row's own cut marker, read from the package
	// that renders it (Rule 1b): a locally spelled "…" could drift from it.
	statusEllipsisForTest = lineedit.StatusEllipsis
)

// ── the guard: the ladder may only name columns the row CARRIES ──────────────
//
// THE DEFECT THIS GUARD EXISTS FOR (task 01a0dbe2). The row's column list carried
// six columns (model, iter, mode, tool, ctx, cost) while statusColumnsForWidth's
// own doc comment named a CACHE column as the FIRST thing shed, with
// statusCacheCol reserving 10 cells for it. The column was drawn on no row at any
// width, so the claim "cache sheds first" was unobservable — and UNFALSIFIABLE
// too, which is the part worth a guard: a form is resolved by statusFormFor, which
// returns ok=false for a name the row does not carry, and statusColumnsForWidth
// SKIPS such a form with a bare `continue`. A name with no column behind it
// therefore cost nothing and said nothing: no test saw it, no compiler could.
//
// The fix retired the column, its renderer and its glyphs (the arithmetic is in
// the RETIRED note on statusCacheCol: the six columns + five separators + gap +
// live block come to EXACTLY the 100-cell budget, and the composed width is only
// ever capped DOWN to it — so no terminal, however wide, could draw a seventh
// column). THIS section is the half that makes the equivalence CHECKABLE: the
// geometry is a value (statusRowColumns), the documented order is a value
// (statusShedOrder), and a name with no column behind it now FAILS a test.

// ladderShedMarkers maps a shed column's NAME to a string it — and no other
// column — puts on the row. A switch rather than a map so a name added to the
// order without a marker is a compile-time thought here, not a lookup that
// silently matches nothing (Rule 15).
func ladderShedMarker(name string) string {
	switch name {
	case ladderMetrics:
		// The group's own marker is the context sub-cell's ratio: the group is
		// ATOMIC, so the read-out that proves the group is on the row is the one it
		// keeps longest.
		return ladderCtxWindow
	case ladderCost:
		// The money sub-cell is RETIRED (task 01a0dcd1): its marker is the
		// currency glyph SPELLED HERE, never a reference to production code — the
		// constant died with the field, and a test pointing at live code could not
		// assert that the row does not carry it (see statusMoneyGlyph).
		return statusMoneyGlyph
	case ladderToolName:
		return toolGlyph
	}
	return ""
}

// ladderNameProblems lists every way a shed ORDER fails to resolve against a
// row's column list and its name table. It takes its inputs rather than reading
// the package's globals so a PHANTOM column can be handed to it in a test: the
// red path of a guard is part of the guard.
//
// Three distinct failures, three distinct repairs:
//
//  1. the order names something the name table does not know — a typo, or a
//     column deleted from the vocabulary while a claim about it stayed;
//  2. the order names a column whose KEY the row's list does not reach — the name
//     exists but the geometry does not. This is the retired cache column exactly:
//     a claim with no cells behind it;
//  3. the name table and the geometry differ in SIZE — a column with no name (the
//     ladder cannot shed it deliberately) or a name with no column.
func ladderNameProblems(full []lineedit.StatusColumn, names map[statusColumn]string, shedOrder []string) []string {
	var problems []string
	for _, name := range shedOrder {
		key, ok := ladderKeyForName(names, name)
		if !ok {
			problems = append(problems, "the shed order names "+name+", which is no column of the row")
			continue
		}
		if int(key) >= len(full) {
			problems = append(problems, "the shed order names "+name+
				": a claim with no cells behind it (the row's list reaches only "+
				itoaForTest(len(full))+" columns)")
		}
		if ladderShedMarker(name) == "" {
			problems = append(problems, "the shed order names "+name+", which has no marker to locate it on the row")
		}
	}
	if len(names) != len(full) {
		problems = append(problems, "the name table covers "+itoaForTest(len(names))+
			" columns and the row carries "+itoaForTest(len(full))+
			": a column with no name, or a name with no column")
	}
	return problems
}

// ladderKeyForName is the reverse lookup (name → column key) over a name table.
// ok is false when no key carries that name.
func ladderKeyForName(names map[statusColumn]string, name string) (statusColumn, bool) {
	for key, n := range names {
		if n == name {
			return key, true
		}
	}
	return 0, false
}

// itoaForTest formats a small non-negative int without importing fmt into the
// predicate, so the checker stays a pure function of its arguments and cannot
// fail for a formatting reason.
func itoaForTest(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// TestStatusLadder_EveryNameIsAColumnOfTheRow is the guard the epic asks for: the
// documented shed order is held against the row's REAL geometry
// (statusRowColumns — the same list status() composes its columns from), so a
// name the row does not carry fails HERE instead of being skipped by
// statusFormFor's ok=false path.
//
// It also cross-checks the order the ladder ACTUALLY implements, derived from the
// forms themselves (ladderShedOrderFromForms), against the order the doc states.
// The two are written independently on purpose — the forms as column keys, the
// order as names — so each can catch the other; restating one in terms of the
// other would make this vacuous.
func TestStatusLadder_EveryNameIsAColumnOfTheRow(t *testing.T) {
	full := statusRowColumns()

	if problems := ladderNameProblems(full, statusColumnNames, statusShedOrder); len(problems) > 0 {
		t.Fatalf("the documented shed order does not match the row's geometry:\n  - %s",
			strings.Join(problems, "\n  - "))
	}
	got := ladderShedOrderFromForms(full, statusColumnNames, statusDegradationOrder)
	if strings.Join(got, " → ") != strings.Join(statusShedOrder, " → ") {
		t.Fatalf("the ladder sheds %v but the documented order is %v (the row carries %d columns)",
			got, statusShedOrder, len(full))
	}
	for key, name := range statusColumnNames {
		if strings.Contains(name, ladderCacheWord) && int(key) < len(full) {
			t.Fatalf("the name table declares a %q column and the row's list reaches it — the cache "+
				"column cannot fit the budget (zero slack at %d cells; see statusCacheCol's RETIRED note)",
				ladderCacheWord, statusLayoutBudgetCols)
		}
	}
}

// ladderCacheWord is the retired column's name, spelled once so the guard and its
// red proof agree on what "the cache column came back" means.
const ladderCacheWord = "cache"

// TestStatusLadder_GuardFailsOnAPhantomColumn is the guard's own red path, kept
// in-repo: it hands ladderNameProblems a column the row does NOT carry and
// requires the failure to be reported — by name, in the sentence a reader can act
// on. A checker that has never returned a problem has not been shown capable of
// one, and this codebase has paid for that lesson before (a table-driven test of a
// composer stayed green while the production wiring was removed).
//
// The phantom is built exactly the way the real defect arose — a name with no
// column behind it — so what is proven to fail is the DEFECT, not a typo.
func TestStatusLadder_GuardFailsOnAPhantomColumn(t *testing.T) {
	full := statusRowColumns()

	withPhantom := map[statusColumn]string{}
	for key, name := range statusColumnNames {
		withPhantom[key] = name
	}
	withPhantom[statusColumn(len(full))] = ladderCacheWord

	problems := ladderNameProblems(full, withPhantom,
		append(append([]string{}, statusShedOrder...), ladderCacheWord))
	if len(problems) == 0 {
		t.Fatal("a shed-order name with no column behind it was accepted — the guard cannot fail, so it guards nothing")
	}
	if joined := strings.Join(problems, "\n"); !strings.Contains(joined, ladderCacheWord) {
		t.Fatalf("the failure does not name the phantom column %q:\n%s", ladderCacheWord, joined)
	}
	// The control: the SAME geometry with the production name table is clean, so
	// the red above came from the phantom and not from a guard that always fails.
	if clean := ladderNameProblems(full, statusColumnNames, statusShedOrder); len(clean) > 0 {
		t.Fatalf("the guard rejects the production geometry too — it is not measuring the phantom:\n%s",
			strings.Join(clean, "\n"))
	}
	// A name table whose every key is off the end of the geometry: a renamed
	// column with the ladder left untouched is the same defect one step over.
	drifted := map[statusColumn]string{}
	for key, name := range statusColumnNames {
		drifted[key+statusColumn(len(full))] = name
	}
	if got := ladderNameProblems(full, drifted, statusShedOrder); len(got) == 0 {
		t.Fatal("a ladder whose every name resolves to no column was accepted")
	}
}

// ladderShedOrderFromForms derives the shed order from the FORMS: the order in
// which each column's last surviving appearance ends. A column kept by every form
// is never shed and is not in the result.
//
// Deriving it (rather than restating it) is the point: the documented order and
// the drawn behaviour are then two independent statements about one fact.
func ladderShedOrderFromForms(full []lineedit.StatusColumn, names map[statusColumn]string, forms [][]statusColumn) []string {
	type ranked struct {
		name     string
		lastKept int
	}
	var shed []ranked
	for key, name := range names {
		if int(key) >= len(full) {
			// A name with no column: reported by the guard above; ranked first
			// here so it cannot hide a real column's position.
			shed = append(shed, ranked{name: name, lastKept: -1})
			continue
		}
		last, keptEverywhere := -1, true
		for i, form := range forms {
			kept := false
			for _, k := range form {
				if k == key {
					kept = true
					break
				}
			}
			if kept {
				last = i
			} else {
				keptEverywhere = false
			}
		}
		if keptEverywhere {
			continue
		}
		shed = append(shed, ranked{name: name, lastKept: last})
	}
	// Ascending by last-kept form index IS the order the columns leave: a form
	// that drops a column is by construction wider than the form after it, so the
	// LAST form that keeps a column decides when it goes.
	sort.SliceStable(shed, func(i, j int) bool { return shed[i].lastKept < shed[j].lastKept })
	out := make([]string, 0, len(shed))
	for _, r := range shed {
		out = append(out, r.name)
	}
	return out
}

// TestStatusLadder_FormsNarrowStrictly is the ladder's arithmetic guard. Two
// things must hold, and both were violated before this task:
//
//   - every step must be STRICTLY narrower than the one before it. Two forms of
//     equal width make the second one dead, because statusColumnsForWidth takes
//     the FIRST form that fits — and a dead form is invisible on screen and
//     untestable through the composer. Measured here: the form keeping tool and
//     the form keeping ctx BOTH come to 69 cells, so the ctx-keeping step could
//     never be selected and ctx actually shed BEFORE tool, the opposite of the
//     doc comment's own argument that ctx is the read-out the row exists for.
//
//   - the row's arithmetic must come to EXACTLY statusLayoutBudgetCols: the
//     compile-time `const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`
//     catches a sum OVER the budget, and this catches the other direction (a sum
//     UNDER it would mean the pinned geometry is not the row being composed, and
//     every ladder width would be wrong by the same slack).
func TestStatusLadder_FormsNarrowStrictly(t *testing.T) {
	widths := make([]int, 0, len(statusDegradationOrder))
	for _, form := range statusDegradationOrder {
		cols, ok := statusFormFor(form, statusRowColumns())
		if !ok {
			t.Fatalf("a form names a column the row does not carry: %v", form)
		}
		widths = append(widths, statusFormWidth(cols))
	}
	for i := 1; i < len(widths); i++ {
		if widths[i] >= widths[i-1] {
			t.Fatalf("ladder step %d is %d cells and step %d is %d: a step must be STRICTLY narrower, "+
				"or the later one is never selected (statusColumnsForWidth takes the first form that fits)"+
				"\nwidths = %v", i-1, widths[i-1], i, widths[i], widths)
		}
	}
	if widths[0] != statusColumnsAtCols {
		t.Fatalf("the widest form is %d cells but the row's geometry is %d: the ladder and the pinned "+
			"layout disagree about the row they compose", widths[0], statusColumnsAtCols)
	}
	columnSum := 0
	for _, c := range statusRowColumns() {
		columnSum += c.Width
	}
	got := columnSum + statusColumnSepTotalN*lineedit.StatusColumnSepCells +
		lineedit.StatusLineGapCells + statusLiveCol
	// The row is composed at the PIN (statusColumnsAtCols) and the budget is the
	// CEILING above it (task 01a0dcd1). Since bug F the pin equals the budget
	// exactly — the last cell was spent on the context denominator's qualifier —
	// so the NAMED headroom (statusPinSlackCells) is 0 rather than an
	// unaccounted gap. Both halves are asserted — the pin EQUALS the geometry's
	// own sum, and the sum plus the slack EQUALS the budget — so a change that
	// only moved one number fails here instead of silently spending headroom the
	// row does not have.
	if got != statusColumnsAtCols {
		t.Fatalf("the row's arithmetic is %d cells (columns %d + %d seps × %d + gap %d + live %d), want "+
			"exactly the pin %d: the pinned geometry is not the one composed", got, columnSum,
			statusColumnSepTotalN, lineedit.StatusColumnSepCells, lineedit.StatusLineGapCells,
			statusLiveCol, statusColumnsAtCols)
	}
	if got+statusPinSlackCells != statusLayoutBudgetCols {
		t.Fatalf("the row's arithmetic is %d cells and statusPinSlackCells says the headroom is %d: the "+
			"two do not add up to the %d-cell budget — the slack is unaccounted for",
			got, statusPinSlackCells, statusLayoutBudgetCols)
	}
	if statusPinSlackCells < 0 {
		t.Fatalf("statusPinSlackCells = %d: the geometry is OVER its budget", statusPinSlackCells)
	}
	if w := statusFormWidth(statusRowColumns()); w != got {
		t.Fatalf("statusFormWidth says %d cells and the named arithmetic says %d — the two ways of "+
			"measuring one row have drifted apart", w, got)
	}
	last, _ := statusFormFor(statusKeepAlways, statusRowColumns())
	if widths[len(widths)-1] != statusFormWidth(last) {
		t.Fatalf("the narrowest form is %d cells but the identity set is %d: the ladder ends somewhere "+
			"other than the columns no step may shed", widths[len(widths)-1], statusFormWidth(last))
	}
}
