package lineedit

import (
	"fmt"
	"strings"
	"testing"
)

// screendump_scoped_test.go — the regression guard for the bounded rendering.
//
// THE DEFECT THIS PINS. `chat_surface action=see` used to hand the dump's whole
// text rendering to a model as one tool result. The rendering carries the whole
// scrollback, so a 24-row screen whose turn history had reached 278 turns arrived
// as 2,783,126 characters — the grid was 2,948 of them, the scrollback section
// was 99.9% (measured 2026-11-06). The artifact must keep that history (ui_state
// and the replay self-check read it whole), so the fix is a SECOND entry point
// with a bound, not a change to Text.
//
// The two halves a fix must not confuse: the bound is a property of the
// RENDERING A CALLER ASKS FOR, never of the artifact. So every test here asserts
// both — the bounded rendering is small AND Text still holds everything.

// overflowScreen returns a screen whose scrollback has scrolled past n times, so
// it carries n saved rows plus the grid. It paints one committed line per row and
// pushes each off the top, which is the only way a real terminal records history.
func overflowScreen(cols, rows, n int) *Screen {
	s := NewScreen(cols, rows)
	for i := 0; i < n; i++ {
		// One line at the top, then enough newlines to scroll it off: the first
		// newline moves to the last row, the next scrolls row 0 into history.
		s.Feed([]byte(fmt.Sprintf("\x1b[1;1Hhistory line %04d", i)))
		s.Feed([]byte(strings.Repeat("\r\n", rows)))
	}
	return s
}

// scrollbackRows builds a dump that carries EXACTLY n scrollback rows without
// going through the screen's own scrolling: MarkScrollback is the attested
// boundary the layout tests already use, and it keeps this fixture's shape
// independent of terminal emulation details.
func scrollbackRows(t *testing.T, n int) ScreenDump {
	t.Helper()
	s := NewScreen(20, 3)
	s.Feed([]byte("\x1b[1;1Hthe answer row\r\nstatus: idle"))
	for i := 0; i < n; i++ {
		s.MarkScrollback(fmt.Sprintf("── turn %d · a line of history long enough to cost characters", i+1))
	}
	return s.Dump()
}

// TestScreenDumpTextIsUnboundedAndTextScopedCapsTheScrollback is the core pair:
// the same artifact rendered both ways must differ ONLY in the scrollback
// section's length, and Text must keep the whole history.
func TestScreenDumpTextIsUnboundedAndTextScopedCapsTheScrollback(t *testing.T) {
	const historyRows = 500
	const cap = 7
	d := scrollbackRows(t, historyRows)

	whole := d.Text()
	if got := strings.Count(whole, dumpRowSep); got < historyRows {
		t.Fatalf("the fixture is not measuring what it claims: Text carries %d row-separated lines, want at least the %d scrollback rows", got, historyRows)
	}
	if strings.Contains(whole, dumpScrollbackTruncated) {
		t.Fatalf("Text wrote a truncation line — it is the UNBOUNDED rendering and must never cut history")
	}
	if !strings.Contains(whole, fmt.Sprintf("%s %s=%d", dumpScrollbackBegin, dumpLinesLabel, historyRows)) {
		t.Fatalf("Text dropped the true scrollback count\n--- text ---\n%s", firstLines(whole, 20))
	}

	scoped := d.TextScoped(cap)
	if strings.Contains(scoped, dumpScrollbackTruncated) == false {
		t.Fatalf("TextScoped(%d) did NOT report a truncation on a %d-row history — a silently shortened history is indistinguishable from a short one\n--- scoped ---\n%s",
			cap, historyRows, scoped)
	}
	// The header still names the TRUE total: the count is the artifact's fact,
	// never the rendering's choice.
	if !strings.Contains(scoped, fmt.Sprintf("%s %s=%d", dumpScrollbackBegin, dumpLinesLabel, historyRows)) {
		t.Errorf("TextScoped changed the scrollback COUNT to the truncated length — a reader that greps the header now learns the wrong size of the history\n--- scoped header ---\n%s", firstLines(scoped, 8))
	}
	if len(scoped) >= len(whole) {
		t.Fatalf("TextScoped(%d) returned %d characters, not fewer than Text's %d — the bound did nothing", cap, len(scoped), len(whole))
	}
	// The truncation line states shown / total / omitted, and the numbers agree
	// with the dump.
	wantTrunc := fmt.Sprintf("%s %s=%d %s=%d %s=%d", dumpScrollbackTruncated, dumpShownLabel, cap, dumpTotalLabel, historyRows, dumpOmittedLabel, historyRows-cap)
	if !strings.Contains(scoped, wantTrunc) {
		t.Fatalf("the truncation line does not state shown/total/omitted as %q\n--- scoped ---\n%s", wantTrunc, scoped)
	}
}

// TestTextScopedKeepsTheMostRecentRowsWithTheirIndices pins WHICH rows survive:
// the tail, not the head, and each row keeps its own index. A reader that gets
// the first 7 rows of a 500-row history is looking at the beginning of a session
// it has long left; the screen's own neighborhood is the tail, and a shifted
// index would make every row reference in a compare wrong.
func TestTextScopedKeepsTheMostRecentRowsWithTheirIndices(t *testing.T) {
	const historyRows = 40
	const cap = 5
	d := scrollbackRows(t, historyRows)
	scoped := d.TextScoped(cap)

	// The LAST history row must be present, with its own index.
	lastIdx := fmt.Sprintf("%s%s── turn %d", dumpRowIndex(historyRows-1), dumpRowSep, historyRows)
	if !strings.Contains(scoped, lastIdx) {
		t.Fatalf("the most recent scrollback row (%q) is missing — the bound dropped the WRONG end of the history\n--- scoped ---\n%s", lastIdx, scoped)
	}
	// The FIRST history row must be gone.
	firstIdx := fmt.Sprintf("%s%s── turn 1 ", dumpRowIndex(0), dumpRowSep)
	if strings.Contains(scoped, firstIdx) {
		t.Fatalf("the OLDEST scrollback row (%q) survived a bound of %d — the window is the head, not the tail", firstIdx, cap)
	}
	// Exactly cap rows are present, and the grid/lines/attrs/blind sections are
	// untouched: the bound is scoped to ONE section.
	if got := strings.Count(scoped, "── turn "); got != cap {
		t.Fatalf("TextScoped(%d) carried %d scrollback rows, want exactly %d", cap, got, cap)
	}
	for _, mustKeep := range []string{
		dumpLinesBegin, dumpLinesEnd, dumpGridBegin, dumpGridEnd,
		dumpAttrBegin, dumpAttrEnd, dumpBlindBegin, dumpBlindEnd,
		"the answer row",
	} {
		if !strings.Contains(scoped, mustKeep) {
			t.Errorf("TextScoped dropped %q — only the scrollback section is bounded", mustKeep)
		}
	}
}

// TestTextScopedZeroAndOneAreRealBounds pins the two edge values that a sloppy
// "0 means unbounded" reading gets wrong: 0 is "no scrollback at all" (a real
// request), and 1 is a one-row tail. The unbounded spelling is NEGATIVE and is
// what Text passes, so no caller can accidentally ask for everything by passing
// a zero.
func TestTextScopedZeroAndOneAreRealBounds(t *testing.T) {
	d := scrollbackRows(t, 12)

	none := d.TextScoped(0)
	if strings.Contains(none, "── turn ") {
		t.Fatalf("TextScoped(0) carried scrollback rows — 0 is a bound, not 'unbounded'\n--- none ---\n%s", none)
	}
	if !strings.Contains(none, dumpScrollbackTruncated) {
		t.Errorf("TextScoped(0) held the whole history back without saying so")
	}

	one := d.TextScoped(1)
	if got := strings.Count(one, "── turn "); got != 1 {
		t.Fatalf("TextScoped(1) carried %d rows, want 1", got)
	}
	if !strings.Contains(one, fmt.Sprintf("── turn %d ", 12)) {
		t.Errorf("TextScoped(1) did not keep the most recent row\n--- one ---\n%s", one)
	}

	// A bound at or above the history length is the whole history, and it says
	// nothing about truncation — the bound did not fire.
	exact := d.TextScoped(12)
	if strings.Contains(exact, dumpScrollbackTruncated) {
		t.Errorf("TextScoped(12) reported a truncation on a 12-row history — a bound that fits must be silent")
	}
	if exact != d.Text() {
		t.Errorf("TextScoped(>=len) differs from Text on the same dump — the bounded renderer must be byte-identical when nothing is held back")
	}
}

// TestDumpScrollbackSeeRowsIsBounded is the Rule 1 half: the see budget is a
// named constant, and it must be small enough to serve its purpose (a tool
// result in the tens of kilobytes) while staying a real tail. A future edit that
// raises it into the thousands would restore the defect silently, so the ceiling
// is asserted here rather than trusted.
func TestDumpScrollbackSeeRowsIsBounded(t *testing.T) {
	if DumpScrollbackSeeRows <= 0 {
		t.Fatalf("DumpScrollbackSeeRows = %d: the see budget must be a positive row count", DumpScrollbackSeeRows)
	}
	const ceiling = 1000
	if DumpScrollbackSeeRows > ceiling {
		t.Fatalf("DumpScrollbackSeeRows = %d, above %d: this constant is the `action=see` return budget, and a bound that large re-creates the 2.78-million-character tool result it exists to prevent (measured 2026-11-06)", DumpScrollbackSeeRows, ceiling)
	}
	// The effect, MEASURED on a history the size of the incident: thousands of
	// scrollback rows must render to a small fraction of the whole. A bound of
	// 200 rows against thousands is a ~20x reduction measured here (50,000 rows
	// would be ~200x); asserting an order of magnitude is the conservative
	// reading — the point is that the rendering can never again be dominated by
	// the history.
	s := NewScreen(100, 24)
	for i := 0; i < 5000; i++ {
		s.MarkScrollback(fmt.Sprintf("── turn %d · a history line with enough text to model the incident", i+1))
	}
	d := s.Dump()
	whole, scoped := len(d.Text()), len(d.TextScoped(DumpScrollbackSeeRows))
	if scoped*10 >= whole {
		t.Fatalf("on a %d-row history the bounded rendering is %d characters against the unbounded %d — it is not bounding anything", 5000, scoped, whole)
	}
	// And the absolute ceiling a tool return actually pays: a see rendering must
	// fit in tens of kilobytes, not megabytes.
	const seeCeiling = 64 * 1024
	if scoped > seeCeiling {
		t.Fatalf("the see-bounded rendering of a %d-row history is %d characters, above the %d-character ceiling a tool return may cost", 5000, scoped, seeCeiling)
	}
	t.Logf("MEASURE seed=%d rows: unbounded=%d chars, see-bounded=%d chars (%.2f%% of the whole)",
		5000, whole, scoped, 100*float64(scoped)/float64(whole))
}

// TestOverflowScreenHistorReadsWholeThroughText is the "don't fix it by
// truncating the SCREEN" half: a screen that really scrolled must still hand its
// whole history to the unbounded rendering, so ui_state and the replay self-check
// keep the artifact they depend on.
func TestOverflowScreenHistorReadsWholeThroughText(t *testing.T) {
	const scrolled = 30
	s := overflowScreen(20, 4, scrolled)
	d := s.Dump()
	if len(d.Scrollback) == 0 {
		t.Fatalf("the fixture never scrolled: no scrollback was recorded, so this test would pass on any rendering")
	}
	if got := len(d.Text()); got <= len(d.TextScoped(3)) {
		t.Fatalf("Text (%d chars) is not larger than the bounded rendering on a %d-row scrollback", got, len(d.Scrollback))
	}
	if !strings.Contains(d.Text(), "history line 0000") {
		t.Errorf("Text lost the OLDEST real scrollback row — the artifact must keep its whole history")
	}
}

// firstLines is a bounded excerpt for failure messages: a test that dumps a
// 500-row rendering into a failure report makes the report unreadable.
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
