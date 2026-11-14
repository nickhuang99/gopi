package replayplay

// render_test.go — WHAT THE PIXELS ACTUALLY ARE, asserted on the BYTES.
//
// Four properties are worth a test here, and each has been wrong in this package
// at least once:
//
//  1. EVERY ROW IS EXACTLY cols COLUMNS WIDE. A renderer that is one column off
//     wraps the row in a real terminal and pushes the status line off the
//     bottom, so "the row is the canvas" is the property, not a detail.
//  2. THE RECORDED COLOUR IS COPIED VERBATIM. A dump frame claims to be the
//     screen, so its SGR must carry the recording's own truecolour values.
//  3. THE CHROME IS THE PALETTE'S, AND IT IS OURS — BY ROLE. Material with no
//     recorded colour is drawn in this package's palette, and a ROUND's rows are
//     split by what they ARE (the request, the reasoning, the prose, a tool call,
//     a tool result, a harness row) rather than sharing one tint.
//  4. SCROLLING PAINTS A WINDOW ONTO THE ROUND, and the status line says where.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
)

// csiPattern matches one CSI sequence (the escape, its parameter bytes, and a
// final byte in 0x40..0x7E).
var csiPattern = regexp.MustCompile("\x1b\\[[0-9;:?]*[ -/]*[@-~]")

// cupPattern matches the CUP the renderer heads every row with. Splitting on it
// is how a rendered frame is read back as ROWS.
var cupPattern = regexp.MustCompile("\x1b\\[[0-9]+;[0-9]+H")

// stripCells returns one rendered row's printable text and its display width —
// the RENDERER's own accounting done independently of the renderer, and what a
// real terminal does with the bytes.
func stripCells(row string) (string, int) {
	text := csiPattern.ReplaceAllString(row, "")
	return text, lineedit.DisplayWidth(text)
}

// renderedRows splits a rendered frame into its rows on the CUP that heads each
// one. The row's own text can contain any byte (including an `H` or an escape
// that is part of the material), so the split is on the exact CUP shape rather
// than on a character.
func renderedRows(out string) []string {
	locs := cupPattern.FindAllStringIndex(out, -1)
	var rows []string
	for i, loc := range locs {
		end := len(out)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		rows = append(rows, out[loc[1]:end])
	}
	return rows
}

// dumpFrameOptions is a RenderOptions showing one dump frame of one row.
func dumpFrameOptions(kind FrameKind, cells string, cols, rows int, pal Palette) RenderOptions {
	return RenderOptions{
		DumpFrame: Frame{Kind: kind, Label: string(kind), Rows: []FrameRow{{Cells: cells}}},
		Cols:      cols, Rows: rows, RoundTotal: 1, RoundIndex: 1,
		Palette: pal, Truecolor: true,
	}
}

// TestRender_EveryRowIsExactlyTheCanvasWidth asserts property 1 on a frame that
// carries a wide rune and its continuation slot — the shape that made this
// package render a 117-column row 121 columns wide.
func TestRender_EveryRowIsExactlyTheCanvasWidth(t *testing.T) {
	const cols, rows = 20, 4
	row := "ab常 驻 cdefghij          " // 1 wide rune + 1 wide rune, continuation slots included
	opts := dumpFrameOptions(FrameScreen, row, cols, rows, DefaultPalette())
	opts.DumpFrame.Rows = append(opts.DumpFrame.Rows, FrameRow{Cells: "short"})
	out := Render(opts)
	got := renderedRows(out)
	if len(got) != rows {
		t.Fatalf("rendered %d rows, want %d (one per canvas row)", len(got), rows)
	}
	for i, r := range got {
		_, cells := stripCells(r)
		if i == StatusRow(rows) {
			if cells > cols {
				t.Fatalf("status row is %d columns wide, canvas is %d: %q", cells, cols, r)
			}
			continue
		}
		if cells > cols {
			t.Fatalf("row %d renders %d columns, wider than the canvas's %d: %q", i, cells, cols, r)
		}
		// Every body row is padded to exactly the canvas width, INCLUDING the
		// rows past the material's own: a scrolled or short picture must not
		// leave the previous repaint's pixels beside it.
		if cells != cols {
			t.Fatalf("row %d renders %d columns, want exactly %d: %q", i, cells, cols, r)
		}
	}
}

// TestRender_CopiesTheRecordedTruecolourVerbatim asserts property 2: the dump's
// own per-cell colour reaches the terminal untranslated.
func TestRender_CopiesTheRecordedTruecolourVerbatim(t *testing.T) {
	const cols, rows = 8, 3
	runs := []lineedit.DumpAttrRun{
		{Col: 0, Cells: 3, Style: "fg=rgb:fff8dc bold"},
		{Col: 3, Cells: 5, Style: "fg=rgb:daa520"},
	}
	opts := dumpFrameOptions(FrameScreen, "abcdefgh", cols, rows, DefaultPalette())
	opts.DumpFrame.Rows = []FrameRow{{Cells: "abcdefgh", Runs: runs}}
	out := Render(opts)
	// sgr's OWN parameter builders render the values, so the assertion is about
	// the value being copied, not about a spelling typed in this test.
	for _, want := range []string{
		sgr.Seq(sgr.TruecolorFgParams(0xff, 0xf8, 0xdc) + string(sgr.ParamSeparator) + sgr.BoldParams()),
		sgr.Seq(sgr.TruecolorFgParams(0xda, 0xa5, 0x20)),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the frame does not carry the recorded style %q:\n%q", want, out)
		}
	}
}

// TestRender_DegradesColourWithoutLosingLayout asserts the no-truecolour arm:
// the recorded COLOUR may degrade, the COLUMNS may not move.
func TestRender_DegradesColourWithoutLosingLayout(t *testing.T) {
	const cols, rows = 8, 3
	opts := dumpFrameOptions(FrameScreen, "abcdefgh", cols, rows, DefaultPalette())
	opts.DumpFrame.Rows = []FrameRow{{Cells: "abcdefgh", Runs: []lineedit.DumpAttrRun{{Col: 0, Cells: 8, Style: "fg=rgb:fff8dc"}}}}
	opts.Truecolor = false
	out := Render(opts)
	if strings.Contains(out, sgr.TruecolorFgParams(0xff, 0xf8, 0xdc)) {
		t.Fatalf("a sink without truecolour was sent a 24-bit sequence:\n%q", out)
	}
	rowsOut := renderedRows(out)
	_, cells := stripCells(rowsOut[0])
	if cells != cols {
		t.Fatalf("the degraded row is %d columns, want %d", cells, cols)
	}
	opts.Truecolor = true
	if trueOut := Render(opts); trueOut == out {
		t.Fatal("degradation changed nothing — the two arms must differ")
	}
}

// TestRender_DumpKindsAreColouredApart asserts property 3 for the dump's own
// material: the scrollback tint and the recorded-screen tint are DIFFERENT,
// because they are different claims about where the picture came from.
func TestRender_DumpKindsAreColouredApart(t *testing.T) {
	pal := DefaultPalette()
	scroll := Render(dumpFrameOptions(FrameScrollback, "history", 10, 3, pal))
	screen := Render(dumpFrameOptions(FrameScreen, "history", 10, 3, pal))
	if scroll == screen {
		t.Fatal("the scrollback and the screen render identically — the kind is not in the colour")
	}
	if !strings.Contains(scroll, sgr.TruecolorFgParams(0x8a, 0x9b, 0xb0)) {
		t.Fatalf("the scrollback tint (%s) is not in the output:\n%q", pal.Scrollback, scroll)
	}
}

// TestRender_RoundRowsAreColouredByRole is THE visual contract of this change:
// inside one round, the request, the reasoning, the prose, a tool call, a tool
// result and a harness row are six different colours — so a viewer can tell
// them apart at a glance instead of reading one grey block.
func TestRender_RoundRowsAreColouredByRole(t *testing.T) {
	pal := DefaultPalette()
	round := Round{
		Index: 1, Kind: RoundModel, Label: "round 1/1",
		Rows: []FrameRow{
			{Cells: "PROMPT: do the thing", Role: RolePrompt},
			{Cells: "HARNESS: [t+1s] tick", Role: RoleHarness},
			{Cells: "THINKING:", Role: RoleThinking},
			{Cells: "TEXT:", Role: RoleText},
			{Cells: "TOOL: bash(x=1)", Role: RoleToolCall},
			{Cells: "RESULT:", Role: RoleToolResult},
			{Cells: "RESULT:(the tool recorded no result)", Role: RoleError},
		},
	}
	opts := RenderOptions{Round: round, RoundIndex: 1, RoundTotal: 1, Cols: 40, Rows: 10, Palette: pal, Truecolor: true}
	out := Render(opts)
	for name, hex := range map[string]string{
		"prompt":      pal.Prompt,
		"thinking":    pal.Thinking,
		"text":        pal.Text,
		"tool call":   pal.ToolCall,
		"tool result": pal.ToolResult,
		"harness":     pal.Harness,
		"error":       pal.ErrorRole,
	} {
		params, ok := hexParams(hex)
		if !ok {
			t.Fatalf("the palette's %s colour %q is not a hex colour", name, hex)
		}
		if !strings.Contains(out, params) {
			t.Errorf("the round's %s row is not drawn in %q — the roles collapsed into one colour:\n%q", name, hex, out)
		}
	}
	// And the harness row is the DIMMEST: it must not read as the operator's own
	// words even for a glance.
	if pal.Harness == pal.Prompt || pal.Harness == pal.Text {
		t.Fatalf("the harness tint (%s) equals the prompt (%s) or the prose (%s) colour", pal.Harness, pal.Prompt, pal.Text)
	}
}

// hexParams renders a palette hex colour into the SGR parameters the renderer
// must emit for it, so the assertion reads the VALUE rather than a spelling.
// It goes through the renderer's OWN hex parser (parseHexColour), so a palette
// colour the renderer cannot parse fails here too rather than in a terminal.
func hexParams(hex string) (string, bool) {
	r, g, b, ok := parseHexColour(hex)
	if !ok {
		return "", false
	}
	return sgr.TruecolorFgParams(r, g, b), true
}

// TestRender_ScrollingShowsAWindowOntoTheRound asserts property 4: with a scroll
// offset the renderer paints the rows the offset names, keeps every row at the
// canvas width, and reports the offset on the status line.
func TestRender_ScrollingShowsAWindowOntoTheRound(t *testing.T) {
	const cols, rows = 60, 5 // 4 body rows
	var fr []FrameRow
	for i := 1; i <= 10; i++ {
		fr = append(fr, FrameRow{Cells: "row" + strconv.Itoa(i) + " content here", Role: RoleText})
	}
	opts := RenderOptions{
		Round:      Round{Index: 1, Kind: RoundModel, Label: "round 1/1", Rows: fr},
		RoundIndex: 1, RoundTotal: 1, Cols: cols, Rows: rows,
		Palette: DefaultPalette(), Truecolor: true,
	}
	top := Render(opts)
	if !strings.Contains(top, "row1 content") {
		t.Fatalf("the unscrolled view does not start at the round's first row:\n%q", top)
	}
	opts.Scroll = 4
	scrolled := Render(opts)
	if strings.Contains(scrolled, "row1 content") {
		t.Fatalf("the scrolled view still shows the first row:\n%q", scrolled)
	}
	if !strings.Contains(scrolled, "row5 content") {
		t.Fatalf("scrolling by 4 rows did not bring row 5 to the top:\n%q", scrolled)
	}
	if !strings.Contains(scrolled, scrollIndicator(4)) {
		t.Fatalf("the status line does not report the scroll offset %d:\n%q", 4, scrolled)
	}
	// Past the end the offset CLAMPS (the renderer is the first place the
	// round's height is known) and the view shows the round's own last rows.
	opts.Scroll = 999
	clamped := Render(opts)
	if strings.Contains(clamped, "row1 content") || !strings.Contains(clamped, "row10 content") {
		t.Fatalf("a scroll past the end did not clamp to the round's last rows:\n%q", clamped)
	}
	for i, r := range renderedRows(clamped) {
		if i == StatusRow(rows) {
			continue
		}
		if _, cells := stripCells(r); cells != cols {
			t.Fatalf("scrolled row %d renders %d columns, want %d", i, cells, cols)
		}
	}
}

// TestRender_StatusLineNamesTheRoundAndTheTransport pins what the operator reads
// while playing: which round of how many, the material's kind, and whether it is
// running. The round numbering is 1-based and matches `--round N`.
func TestRender_StatusLineNamesTheRoundAndTheTransport(t *testing.T) {
	out := Render(RenderOptions{
		Round:      Round{Index: 2, Kind: RoundModel, Label: "round 2/25"},
		RoundIndex: 2, RoundTotal: 25, Cols: 100, Rows: 4,
		Paused: true, Speed: 1.5, SessionID: "56507166-ef62-4d2d-bafe-af504fca9311",
		Palette: DefaultPalette(), Truecolor: true,
	})
	for _, want := range []string{PlaybackCommand, "56507166", string(FrameRound), "2/25", statusPaused, "1.5x"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the status line does not carry %q:\n%q", want, out)
		}
	}
	running := Render(RenderOptions{
		Round: frameRoundOptions(), RoundIndex: 1, RoundTotal: 1,
		Cols: 100, Rows: 4, Palette: DefaultPalette(), Truecolor: true,
	})
	if !strings.Contains(running, statusPlaying) {
		t.Fatalf("a running playback does not say %q:\n%q", statusPlaying, running)
	}
	if !strings.Contains(Render(dumpFrameOptions(FrameScreen, "x", 40, 4, DefaultPalette())), string(FrameScreen)) {
		t.Fatal("a dump frame's status line does not name its kind")
	}
}

// frameRoundOptions is a one-row model round, for a status-line case.
func frameRoundOptions() Round {
	return Round{Index: 1, Kind: RoundModel, Rows: []FrameRow{{Cells: "x", Role: RoleText}}}
}

// TestRender_HelpOverlayReplacesTheRound pins the `?` overlay: it is painted
// INSTEAD of the material, and it carries the key map the player actually binds.
func TestRender_HelpOverlayReplacesTheRound(t *testing.T) {
	out := Render(RenderOptions{
		Round:      Round{Index: 1, Kind: RoundModel, Rows: []FrameRow{{Cells: "SECRET-ROUND-CONTENT"}}},
		RoundIndex: 1, RoundTotal: 1, Cols: 80, Rows: 20, Palette: DefaultPalette(), Truecolor: true,
		Help: HelpLines(),
	})
	if strings.Contains(out, "SECRET-ROUND-CONTENT") {
		t.Fatal("the help overlay drew the round underneath it")
	}
	for _, row := range KeyMapRows {
		if !strings.Contains(out, row[1]) {
			t.Fatalf("the overlay is missing the action %q — the overlay and the bindings are one list", row[1])
		}
	}
}

// TestRender_NarrowStatusKeepsTheNotice asserts the priority order of the
// status line, at the width where it was measured to fail: on a 60-column
// terminal the notice ("(top of this round)") was cut to "(top o", so the message
// that a key DID something became unreadable. The state survives; the static
// prefix (the session id, the rate) is what gives way.
func TestRender_NarrowStatusKeepsTheNotice(t *testing.T) {
	const cols = 60
	out := Render(RenderOptions{
		Round:      Round{Index: 1, Kind: RoundModel, Rows: []FrameRow{{Cells: "x"}}},
		RoundIndex: 1, RoundTotal: 71, Cols: cols, Rows: 8,
		Paused: true, Speed: DefaultSpeed,
		SessionID: "930d996e-e739-46d7-89c1-2168dbf1600d",
		Notice:    atRoundStartNotice,
		Palette:   DefaultPalette(), Truecolor: true,
	})
	if !strings.Contains(out, atRoundStartNotice) {
		t.Fatalf("the notice %q was not readable whole at %d columns:\n%q", atRoundStartNotice, cols, out)
	}
	for _, want := range []string{"1/71", statusPaused} {
		if !strings.Contains(out, want) {
			t.Errorf("the narrow status line dropped %q — the state must outlive the static fields:\n%q", want, out)
		}
	}
	// The status row still fits: it never wraps into a second line.
	rows := renderedRows(out)
	last := rows[len(rows)-1]
	if _, cells := stripCells(last); cells > cols {
		t.Fatalf("the status row is %d columns wide at a %d-column canvas: %q", cells, cols, last)
	}
}

// TestRender_EmptyCanvasIsEmpty keeps the degenerate case quiet: a caller that
// has no canvas to draw into gets nothing, never a panic.
func TestRender_EmptyCanvasIsEmpty(t *testing.T) {
	if got := Render(RenderOptions{Cols: 0, Rows: 10}); got != "" {
		t.Fatalf("a zero-width canvas rendered %q", got)
	}
	if got := Render(RenderOptions{Cols: 10, Rows: 0}); got != "" {
		t.Fatalf("a zero-height canvas rendered %q", got)
	}
}
