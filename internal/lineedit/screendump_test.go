package lineedit

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestScreenDumpGridIsColumnExact pins the property every layout assertion
// built on the dump depends on: a Grid row is CELL-exact — exactly one rune per
// cell, the full width — so the rune index of a row IS the 0-based cell column,
// for ASCII and for a wide (CJK) rune alike. The readable Lines section is
// deliberately NOT column-exact and says so; this test is what keeps the two
// from being confused.
func TestScreenDumpGridIsColumnExact(t *testing.T) {
	s := NewScreen(20, 3)
	// Row 0: ASCII. Row 1: two CJK runes (4 cells) then ASCII.
	// Row 2: empty — the padding must still be materialised.
	s.Feed([]byte("\x1b[1;1Habcdefghij" + "\x1b[2;1H知知ab" + "\x1b[3;1H"))

	d := s.Dump()

	cases := []struct {
		name     string
		row      int
		wantGrid string // full width, untrimmed
		wantLine string // trailing blanks trimmed, continuations skipped
	}{
		{"ascii row pads to the full width", 0, "abcdefghij          ", "abcdefghij"},
		// A wide rune owns TWO cells: the rune and its continuation cell (a
		// space). That is what makes a rune index a cell column — "知知" is 4
		// cells, and "ab" therefore starts at cell column 4.
		{"wide runes: 2 runes occupy 4 cells, then ascii", 1, "知 知 ab" + strings.Repeat(" ", 14), "知知ab"},
		{"untouched row is materialised as blanks", 2, strings.Repeat(" ", 20), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := d.Grid[tc.row]; got != tc.wantGrid {
				t.Errorf("Grid[%d] = %q (%d runes), want %q (%d runes)",
					tc.row, got, len([]rune(got)), tc.wantGrid, len([]rune(tc.wantGrid)))
			}
			if n := len([]rune(d.Grid[tc.row])); n != d.Cols {
				t.Errorf("Grid[%d] carries %d cells, want Cols=%d", tc.row, n, d.Cols)
			}
			if got := d.Lines[tc.row]; got != tc.wantLine {
				t.Errorf("Lines[%d] = %q, want %q", tc.row, got, tc.wantLine)
			}
		})
	}
}

// TestScreenDumpAttrRunsOverFullWidth pins the attribute export: runs are
// emitted over the WHOLE row (not over the interesting part), they are maximal,
// and the run's Col is a cell column a caller can assert on directly.
func TestScreenDumpAttrRunsOverFullWidth(t *testing.T) {
	s := NewScreen(10, 1)
	// Cells 0-2 default, cells 3-5 reverse, cells 6-9 bold.
	s.Feed([]byte("\x1b[1;1Habc\x1b[7mdef\x1b[0;1mghij"))

	d := s.Dump()

	if !d.AttrsRecorded {
		t.Fatal("AttrsRecorded = false; the grid records per-cell SGR, so the dump must say so")
	}
	if len(d.AttrRuns) != 1 {
		t.Fatalf("AttrRuns has %d rows, want 1", len(d.AttrRuns))
	}
	want := []DumpAttrRun{
		{Col: 0, Cells: 3, Style: "default"},
		{Col: 3, Cells: 3, Style: "reverse"},
		{Col: 6, Cells: 4, Style: "bold"},
	}
	got := d.AttrRuns[0]
	if len(got) != len(want) {
		t.Fatalf("row 0 has %d runs (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("run %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The runs must COVER the row: a gap would make "no run here" mean two
	// different things (default style, or not reported at all).
	covered := 0
	for _, r := range got {
		if r.Col != covered {
			t.Errorf("run at col %d leaves column %d unreported", r.Col, covered)
		}
		covered = r.Col + r.Cells
	}
	if covered != d.Cols {
		t.Errorf("runs cover %d cells, want the full grid width %d", covered, d.Cols)
	}
}

// TestScreenDumpGeometryFacts pins the header facts: cols/rows, cursor and
// scroll region are all reported, and the cursor is a CELL position that the
// screen model itself would agree with.
func TestScreenDumpGeometryFacts(t *testing.T) {
	cases := []struct {
		name          string
		cols, rows    int
		feed          string
		wantCursorRow int
		wantCursorCol int
		wantTop       int
		wantBottom    int
	}{
		{
			name: "default region spans the whole screen",
			cols: 40, rows: 5,
			feed:          "\x1b[3;7Hhi",
			wantCursorRow: 2, wantCursorCol: 8,
			wantTop: 0, wantBottom: 4,
		},
		{
			name: "an installed scroll region is reported, not flattened",
			cols: 40, rows: 10,
			feed:          "\x1b[2;8r\x1b[1;1Hx",
			wantCursorRow: 0, wantCursorCol: 1,
			wantTop: 1, wantBottom: 7,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScreen(tc.cols, tc.rows)
			s.Feed([]byte(tc.feed))
			d := s.Dump()

			if d.Cols != tc.cols || d.Rows != tc.rows {
				t.Errorf("geometry = %dx%d, want %dx%d", d.Cols, d.Rows, tc.cols, tc.rows)
			}
			if len(d.Grid) != tc.rows || len(d.Lines) != tc.rows || len(d.AttrRuns) != tc.rows {
				t.Errorf("row counts grid=%d lines=%d attrs=%d, want %d each",
					len(d.Grid), len(d.Lines), len(d.AttrRuns), tc.rows)
			}
			if d.Cursor.Row != tc.wantCursorRow || d.Cursor.Col != tc.wantCursorCol {
				t.Errorf("cursor = (%d,%d), want (%d,%d)",
					d.Cursor.Row, d.Cursor.Col, tc.wantCursorRow, tc.wantCursorCol)
			}
			if d.ScrollRegion.Top != tc.wantTop || d.ScrollRegion.Bottom != tc.wantBottom {
				t.Errorf("scroll region = %+v, want top=%d bottom=%d",
					d.ScrollRegion, tc.wantTop, tc.wantBottom)
			}
		})
	}
}

// TestScreenDumpScrollbackAndBlindSpots pins the two sections a reader uses to
// bound its own conclusions: the saved history, and the explicit list of what
// the representation cannot see.
func TestScreenDumpScrollbackAndBlindSpots(t *testing.T) {
	s := NewScreen(10, 2)
	// Two LFs on the last row push line 0 off the screen's first row, which is
	// the only case a terminal SAVES (interior margins discard).
	s.Feed([]byte("one\r\ntwo\r\nthree"))

	d := s.Dump()

	if got, want := d.Scrollback, []string{"one"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Scrollback = %q, want %q", got, want)
	}
	if d.BlindSpotIDs() == nil || len(d.BlindSpots) == 0 {
		t.Fatal("a dump with no blind-spot list invites an assertion it cannot support")
	}
	required := []string{
		BlindSpotFont, BlindSpotIME, BlindSpotRawBytes, BlindSpotGraphics,
		BlindSpotModes, BlindSpotBlankCells, BlindSpotInteriorScroll, BlindSpotTiming,
	}
	have := map[string]bool{}
	for _, id := range d.BlindSpotIDs() {
		have[id] = true
	}
	for _, id := range required {
		if !have[id] {
			t.Errorf("blind spot %q missing from the dump", id)
		}
	}
	for _, spot := range d.BlindSpots {
		if strings.TrimSpace(spot.Detail) == "" {
			t.Errorf("blind spot %q carries no explanation", spot.ID)
		}
	}
}

// TestScreenDumpIsASnapshot pins the immutability contract: a dump describes the
// screen at the instant Dump() was called, so feeding more bytes afterwards must
// not retroactively change what a report already quoted.
func TestScreenDumpIsASnapshot(t *testing.T) {
	s := NewScreen(10, 2)
	s.Feed([]byte("before"))
	d := s.Dump()

	s.Feed([]byte("\x1b[1;1H\x1b[7mafter\x1b[0m\x1b[2;1Hmore"))

	if d.Lines[0] != "before" {
		t.Errorf("Lines[0] = %q after a later feed, want the snapshot %q", d.Lines[0], "before")
	}
	if d.AttrRuns[0][0].Style != "default" {
		t.Errorf("AttrRuns[0][0] = %+v after a later feed, want the snapshot's default run", d.AttrRuns[0][0])
	}
	if d.Cursor.Row != 0 || d.Cursor.Col != 6 {
		t.Errorf("cursor = (%d,%d) after a later feed, want the snapshot (0,6)", d.Cursor.Row, d.Cursor.Col)
	}
}

// TestScreenDumpTextRendering pins the readable rendering's shape: it is
// line-oriented, sectioned, and every row line is "<3-digit index>|<content>" —
// the prefix a reader greps for and a test asserts on.
func TestScreenDumpTextRendering(t *testing.T) {
	s := NewScreen(8, 2)
	s.Feed([]byte("\x1b[1;1Hhello\x1b[2;1H\x1b[7mworld\x1b[0m\x1b[1;3H"))
	d := s.Dump()
	text := d.Text()

	for _, want := range []string{
		"#gopi-screen-dump v=1 cols=8 rows=2\n",
		"cursor row=0 col=2\n",
		"scroll_region top=0 bottom=1\n",
		"attrs recorded=true\n",
		"lines_begin count=2\n",
		"000|hello\n",
		"001|world\n",
		"lines_end\n",
		"grid_begin count=2\n",
		"000|hello   \n", // the grid line: padded to the full width (8 cells)
		"001|world   \n",
		"grid_end\n",
		"attrs_begin\n",
		"000|0:8=default\n",
		"001|0:5=reverse 5:3=default\n",
		"attrs_end\n",
		"blind_spots_begin count=",
		"blind_spots_end\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text rendering is missing %q\n--- dump ---\n%s", want, text)
		}
	}
	if !strings.HasSuffix(text, dumpBlindEnd+"\n") {
		t.Errorf("text rendering must END with the blind-spot section:\n%s", text[len(text)-80:])
	}
}

// TestScreenDumpJSONRoundTrips pins the machine-checkable form: it parses, and
// the fields a caller asserts on survive the round trip.
func TestScreenDumpJSONRoundTrips(t *testing.T) {
	s := NewScreen(6, 2)
	s.Feed([]byte("\x1b[2;3H\x1b[7mz"))
	d := s.Dump()

	raw, err := d.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var back ScreenDump
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	if back.Version != DumpVersion || back.Cols != 6 || back.Rows != 2 {
		t.Errorf("geometry round trip = v%d %dx%d, want v%d 6x2", back.Version, back.Cols, back.Rows, DumpVersion)
	}
	if back.Cursor != d.Cursor {
		t.Errorf("cursor round trip = %+v, want %+v", back.Cursor, d.Cursor)
	}
	if len(back.AttrRuns) != len(d.AttrRuns) || back.AttrRuns[1][0] != d.AttrRuns[1][0] {
		t.Errorf("attr runs round trip = %+v, want %+v", back.AttrRuns, d.AttrRuns)
	}
	if len(back.BlindSpots) != len(d.BlindSpots) {
		t.Errorf("blind spots round trip = %d, want %d", len(back.BlindSpots), len(d.BlindSpots))
	}
}

// TestScreenDoesNotRenderAnOSCStringAsText is the regression guard for a defect
// the pty evidence run found: the grid rendered an OSC sequence's PAYLOAD as
// cells.
//
// The sequence is not exotic — a bubbletea program asks the terminal for its
// background colour with `ESC ] 11 ; ? ESC \` before it draws anything. The
// parser read the `]` as "ESC + one byte" and then wrote "11;?"'s tail into the
// grid as text, so the FIRST ROW OF THE SCREEN came out shifted: measured in
// the run, the header read " opi ui → …  (tools off)" — a stray "1;?" sitting
// over the first four cells of a frame that was painted correctly on the wire.
//
// An observer that corrupts what it observes is worse than no observer, so this
// stays.
func TestScreenDoesNotRenderAnOSCStringAsText(t *testing.T) {
	const (
		oscAskBackground = "\x1b]11;?\x1b\\"  // the real sequence, ST-terminated
		oscBELTitle      = "\x1b]0;title\x07" // BEL-terminated, same rule
		frame            = "\x1b[1;1Hgopi ui"
	)
	cases := []struct {
		name   string
		feed   string
		row0   string
		cursor [2]int
	}{
		{"an ST-terminated OSC leaves no cells", oscAskBackground + frame, "gopi ui", [2]int{0, 7}},
		{"a BEL-terminated OSC leaves no cells", oscBELTitle + frame, "gopi ui", [2]int{0, 7}},
		{"an OSC payload containing a CSI final byte is still one string", "\x1b]11;rgb:1m2/3/4\x1b\\" + frame, "gopi ui", [2]int{0, 7}},
		{"a bare OSC paints nothing at all", oscAskBackground, "", [2]int{0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScreen(20, 2)
			s.Feed([]byte(tc.feed))
			if got := s.Line(0); got != tc.row0 {
				t.Errorf("row 0 = %q, want %q", got, tc.row0)
			}
			if s.CurRow != tc.cursor[0] || s.CurCol != tc.cursor[1] {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", s.CurRow, s.CurCol, tc.cursor[0], tc.cursor[1])
			}
			// No cell anywhere may hold a byte of the payload.
			for row := 0; row < s.Rows; row++ {
				if line := s.Line(row); strings.Contains(line, "?") || strings.Contains(line, "title") {
					t.Errorf("row %d carries OSC payload text: %q", row, line)
				}
			}
		})
	}
}

// TestScreenOSCSplitAcrossChunks pins what the STATELESS parser promises when a
// string sequence straddles a write boundary: an unterminated payload is
// dropped (never rendered as cells), and once the sequence's bytes are past,
// the next frame paints cleanly.
//
// The grid deliberately carries no partially-received sequence (see Feed's own
// comment on the same rule for CSI): a chunk boundary inside an OSC string means
// the tail is discarded. That is a bound on the PARSER, not a blind spot of the
// representation, and the assertion below says which side of it we are on.
func TestScreenOSCSplitAcrossChunks(t *testing.T) {
	s := NewScreen(20, 2)
	s.Feed([]byte("\x1b]11;?"))
	if got := s.Line(0); got != "" {
		t.Errorf("an unterminated OSC painted %q, want nothing", got)
	}
	if s.CurRow != 0 || s.CurCol != 0 {
		t.Errorf("an unterminated OSC moved the cursor to (%d,%d), want (0,0)", s.CurRow, s.CurCol)
	}
	// The next frame paints from a clean grid: the payload left no cells.
	s.Feed([]byte("\x1b[1;1Hok"))
	if got := s.Line(0); got != "ok" {
		t.Errorf("row 0 = %q after the sequence was abandoned, want %q", got, "ok")
	}
}

// TestScreenEraseDisplayModes is the regression guard for the second defect the
// pty evidence run found: `CSI J` was implemented ONLY for parameter 2, so the
// directional forms a TUI actually uses were no-ops — and the grid kept cells
// the terminal had already wiped.
//
// The measured shape: bubbletea emits `CSI 29A CSI J` on every frame whose
// content grew shorter (it rewinds to the top of its panel and clears what
// follows). With `CSI J` ignored, the single space bubbletea writes for an
// empty view survived on the header row, and the dump read " opi ui → …" while
// the wire carried the correct frame — an observer reporting a screen that
// never existed.
func TestScreenEraseDisplayModes(t *testing.T) {
	// paint builds a 2-row grid whose rows read "AAA" / "BBB" with cursor (0,1).
	paint := func() *Screen {
		s := NewScreen(6, 2)
		s.Feed([]byte("AAA\r\nBBB"))
		s.Feed([]byte("\x1b[1;2H"))
		return s
	}
	cases := []struct {
		name     string
		seq      string
		wantRow0 string
		wantRow1 string
		why      string
	}{
		{
			name: "CSI 0J clears from the cursor to the end of the screen",
			seq:  "\x1b[J",
			// Cursor at (0,1): column 0 survives, the rest of row 0 and row 1 go.
			wantRow0: "A", wantRow1: "",
			why: "bubbletea's panel wipe: everything below the cursor is cleared",
		},
		{
			name: "CSI 1J clears from the start of the screen to the cursor",
			seq:  "\x1b[1J",
			// Row 0 up to AND INCLUDING the cursor column goes (the cursor is
			// at column 1, so column 2's "A" stays); row 1 is below the cursor.
			wantRow0: "  A", wantRow1: "BBB",
			why: "the mirror form: what was SCROLLED OFF is cleared",
		},
		{
			name:     "CSI 2J clears everything",
			seq:      "\x1b[2J",
			wantRow0: "", wantRow1: "",
			why: "the full clear a repaint starts from",
		},
		{
			name:     "CSI 0K clears the rest of the line only",
			seq:      "\x1b[K",
			wantRow0: "A", wantRow1: "BBB",
			why: "the line-scoped sibling keeps its own meaning",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := paint()
			s.Feed([]byte(tc.seq))
			if got := s.Line(0); got != tc.wantRow0 {
				t.Errorf("row 0 = %q, want %q (%s)", got, tc.wantRow0, tc.why)
			}
			if got := s.Line(1); got != tc.wantRow1 {
				t.Errorf("row 1 = %q, want %q (%s)", got, tc.wantRow1, tc.why)
			}
		})
	}
}

// TestScreenEraseDisplayMatchesTheRenderedFrame is the end-to-end shape of the
// same defect, in the order bubbletea really emits it: a frame, then the panel
// rewind + `CSI J` + the next frame written from the top. The HEADER ROW must
// read exactly what the second frame drew — no survivor from the first.
func TestScreenEraseDisplayMatchesTheRenderedFrame(t *testing.T) {
	s := NewScreen(20, 3)
	// Frame 1: a 3-line panel, written the way the renderer writes it — a CR at
	// the end of the last line, which is where the next frame's rewind starts
	// from (the rewind moves the ROW only and leaves the column alone, so a
	// frame that ended mid-line would rewind to mid-line).
	s.Feed([]byte("\x1b[1;1Hgopi ui one\r\ntwo\r\nthree\r"))
	// Frame 2: the panel shrank. Bubbletea rewinds by (linesRendered - 1) and
	// clears what follows, then writes the shorter panel from the top.
	s.Feed([]byte("\x1b[2A\x1b[J\rgrown"))

	if got := s.Line(0); got != "grown" {
		t.Errorf("row 0 = %q, want %q — a cell from the wiped frame survived", got, "grown")
	}
	for row := 1; row < 3; row++ {
		if got := s.Line(row); got != "" {
			t.Errorf("row %d = %q, want it cleared by CSI J", row, got)
		}
	}
}
