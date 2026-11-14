package lineedit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// palette_capture_test.go — the BEFORE/AFTER capture the acceptance asks for.
//
// It writes the REAL renderers' byte stream to files: the same AnswerRenderer,
// Renderer and status composition the chat surfaces use, driven through their
// public entry points. A capture produced any other way (a hand-written sample,
// a mock) would show what the palette is supposed to look like rather than what
// it emits, which is the one thing a "before/after" image must not do.
//
// The emitter runs only when GOPI_PALETTE_CAPTURE_DIR names a directory, so it is
// inert during an ordinary test run (the file-writing is not a test result: the
// assertions live in palette_test.go, and this file produces evidence).

// PaletteCaptureDirEnv names the directory the capture emitter writes into. It is
// read by paletteCaptureDir below — a hook with no reader would be a wiring
// defect (Rule 21), so the reader is right here.
const PaletteCaptureDirEnv = "GOPI_PALETTE_CAPTURE_DIR"

// captureSurfaceNames are the two captures written: the colour-capable run and
// the same run with the capability off. They are named (Rule 1b) because the
// devdoc and the capture script both refer to them.
const (
	captureColourName = "capture-colour.ansi"
	captureMonoName   = "capture-mono.txt"
	// captureChromeName is the same palette without the answer band, so the
	// banner, the hint, the warning, the error and the editing line are all on
	// screen at once: the answer band legitimately claims the lower screen (one
	// writer per row), and a capture of the band alone would hide the chrome the
	// epic also names.
	captureChromeName = "capture-chrome.ansi"
)

// paletteCaptureDir returns the capture directory, or "" when the emitter is not
// requested.
func paletteCaptureDir() string { return os.Getenv(PaletteCaptureDirEnv) }

// TestPaletteCaptureGolden emits the before/after capture pair. It asserts the
// three properties the images exist to show (colour on ⇒ SGR present, colour off
// ⇒ zero SGR, the same characters either way) and writes the files.
func TestPaletteCaptureGolden(t *testing.T) {
	dir := paletteCaptureDir()
	if dir == "" {
		t.Skipf("%s not set: the capture emitter is inert during an ordinary run", PaletteCaptureDirEnv)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create capture dir: %v", err)
	}

	on := captureSurfaces(t, BasicPalette)
	off := captureSurfaces(t, MonoPalette)

	if !hasSGR(on) {
		t.Fatal("the colour capture carries no SGR — there would be nothing to show")
	}
	if hasSGR(off) {
		t.Fatalf("the monochrome capture carries SGR bytes:\n%q", off)
	}
	if stripSGR(on) != stripSGR(off) {
		t.Fatalf("the two captures differ in characters:\n on  %q\n off %q", stripSGR(on), stripSGR(off))
	}

	if err := os.WriteFile(filepath.Join(dir, captureColourName), []byte(on), 0o644); err != nil {
		t.Fatalf("write colour capture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, captureMonoName), []byte(off), 0o644); err != nil {
		t.Fatalf("write mono capture: %v", err)
	}
	chrome := captureChrome(BasicPalette)
	if !hasSGR(chrome) {
		t.Fatal("the chrome capture carries no SGR")
	}
	if err := os.WriteFile(filepath.Join(dir, captureChromeName), []byte(chrome), 0o644); err != nil {
		t.Fatalf("write chrome capture: %v", err)
	}
	t.Logf("captures written to %s (%d / %d / %d bytes)", dir, len(on), len(off), len(chrome))
}

// captureSurfaces renders the ANSWER BAND through a real region and the shipped
// AnswerRenderer: an answer with prose, a fenced code block and a diff inside it,
// under a live status row.
func captureSurfaces(t *testing.T, pal Palette) string {
	t.Helper()
	var b strings.Builder
	cap := newPaletteCapture()
	reg := newRegion(cap.out)
	if !reg.Enable(captureRows, captureCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want true", captureRows, captureCols)
	}
	r := NewAnswerRenderer(reg, func() Status {
		return Status{Label: captureStatusLabel, Live: captureStatusLive, Thinking: true}
	})
	r.SetCols(reg.Cols())
	r.SetPalette(pal)
	r.Begin()
	for _, line := range []string{
		"Here is the patch that fixes the row shear:\n",
		"```diff\n",
		"-\tbody := count(s)\n",
		"+\tbody := count(stripSGR(s))\n",
		"```\n",
		"The remainder of the answer streams underneath.\n",
	} {
		r.Delta(line)
	}
	r.Tick()
	r.Finish()
	b.WriteString(cap.bytes())
	b.WriteString(pal.Error.Wrap(captureError))
	b.WriteString("\n")
	return b.String()
}

// captureChrome renders every chrome surface the epic names, EXCEPT the answer
// band (which the pair above captures through the real region): the banner, the
// tools hint, the warning, the error, the status row and the editing line.
//
// It lays them out SEQUENTIALLY rather than through a screen region. That is
// deliberate: the band's region addresses absolute rows (one writer per row), and
// a capture that relies on absolute addressing shows whatever the previous run
// left on those rows — the row geometry belongs to the layout test, not to an
// image whose subject is the EMPHASIS. The status row and the editing line are
// produced by the same functions the region path calls (styledStatusLine,
// layoutText + prompt emphasis), so what the image shows is still the shipped
// composition.
func captureChrome(pal Palette) string {
	var b strings.Builder
	b.WriteString(pal.Banner.Wrap(captureBanner))
	b.WriteString(pal.Hint.Wrap(captureToolsHint))
	b.WriteString("\n")
	b.WriteString(pal.Warning.Wrap(captureWarning))
	b.WriteString("\n")
	b.WriteString(pal.Error.Wrap(captureError))
	b.WriteString("\n")
	b.WriteString(styledStatusLine(Status{
		Label:    captureStatusLabel,
		Live:     captureStatusLive,
		Thinking: true,
	}, captureCols, pal))
	b.WriteString("\n")
	b.WriteString(captureEditingLine(pal))
	b.WriteString("\n")
	return b.String()
}

// captureEditingLine is the editing line as the renderer composes it: the prompt
// (emphasised) followed by the typed buffer (never emphasised).
func captureEditingLine(pal Palette) string {
	rows, _, _ := layoutText(capturePrompt, captureTyped, len([]rune(captureTyped)), captureCols)
	if len(rows) == 0 {
		return ""
	}
	r := &Renderer{prompt: capturePrompt}
	r.SetPalette(pal)
	r.promptCols = displayWidth(capturePrompt)
	r.promptRunes = []rune(capturePrompt)
	return wrapSegs(rows[0], r.promptEmphasis(0, captureCols))
}

// capture geometry and prompt: the shape the devdoc's image is rendered at (a
// 100×30 terminal, as the chat layout's own documentation uses).
const (
	captureRows   = 30
	captureCols   = 100
	capturePrompt = "you> "
	captureTyped  = "why did the row shear?"
	// The chrome text is the REPL's own wording, so the image shows the surfaces
	// the user actually sees rather than invented samples.
	captureBanner    = "gopi chat — Ctrl+C interrupts the running turn, Ctrl+D or /quit to exit."
	captureToolsHint = "  (tools ON)"
	captureWarning   = "[gopi] the status row refreshes on the next keystroke."
	captureError     = "[gopi] chat: diagnostics cannot be recorded — see console.log"
	// The status row is a single named sample: the label carries the tool glyph
	// and the counters (so the tool-activity emphasis is visible in the capture),
	// and the live hint is the in-flight state.
	captureStatusLabel = "glm-5.3-flash" + StatusSep + "turn 2" + StatusSep + ToolGlyph + " bash git diff" + StatusSep + "1.2k tokens"
	captureStatusLive  = "thinking…"
)

// captureStatusModel / captureStatusLiveWord are the substrings the grid
// assertions locate the row's two halves by: the label's first field (the model)
// and the live hint — both taken from the ONE sample this file composes, so a
// change to the sample cannot leave a test searching for a word nobody draws.
var (
	captureStatusModel    = strings.Split(captureStatusLabel, StatusSep)[0]
	captureStatusLiveWord = captureStatusLive
)

// ---------------------------------------------------------------------------
// The status row's gold, on the CELL GRID
// ---------------------------------------------------------------------------

// statusCaptureRow is the grid row the status row is replayed at: the count of
// blank rows fed ahead of it. It is a named constant (Rule 1) because the capture
// and its assertions must name the SAME row — a hard-coded row in one place is how
// a capture starts showing a neighbouring band.
const statusCaptureRow = 3

// statusCaptureCases are the four tiers the epic names, each with the colour its
// row must paint: a 24-bit label gold for the gold tables, the xterm cube's
// #FFD700 for the 256 tier, and the named gold for the floor (which has no
// numbered spelling — see statusGold).
//
// liveRGB/liveIdx are the THINKING state's colour, because the captured sample is
// the in-flight row (thinking…): the label is the table's `label` role and the
// live hint is the table's thinking role — on the gold tables that is `muted`,
// which is the second shade those tables can afford and the 256 tier cannot.
type statusCaptureCase struct {
	name     string
	pal      Palette
	labelRGB int // expected FgRGB on a label cell, or sgr.NoRGB for a palette index
	labelIdx int // expected Fg on a label cell, or sgr.DefaultColor for a 24-bit one
	liveRGB  int
	liveIdx  int
}

func statusCaptureCases() []statusCaptureCase {
	return []statusCaptureCase{
		{"basic", BasicPalette, sgr.NoRGB, sgr.Yellow, sgr.NoRGB, sgr.Yellow},
		{"palette256", Palette256, sgr.NoRGB, cubeGoldAccent, sgr.NoRGB, cubeGoldAccent},
		{"hermes-dark", HermesDarkPalette, hermesDarkLabel, sgr.DefaultColor, hermesDarkMuted, sgr.DefaultColor},
		{"hermes-light", HermesLightPalette, hermesLightLabel, sgr.DefaultColor, hermesLightLabel, sgr.DefaultColor},
	}
}

// TestPaletteCaptureStatusRowGoldIsOnTheGrid is the rule-10.2 half of the
// acceptance: the status row is composed by the SHIPPED composition
// (styledStatusLine), replayed through the real cell grid, and each cell's
// recorded STYLE — never its glyph — is asserted.
//
// Why the grid and not the string: a sequence in a byte stream can be satisfied by
// bytes that reach no cell (or reach the wrong one), and an assertion on the text
// cannot distinguish "the label is gold" from "the label is bold and some other
// cell is gold". The grid is where a terminal's colour decision actually lands, so
// it is where "the row is gold" is checked — and, for the same reason, where a
// regression that leaves the row on the default foreground is caught.
func TestPaletteCaptureStatusRowGoldIsOnTheGrid(t *testing.T) {
	for _, tc := range statusCaptureCases() {
		t.Run(tc.name, func(t *testing.T) {
			sc := NewScreen(captureCols, captureRows)
			sc.Feed([]byte(strings.Repeat("\r\n", statusCaptureRow)))
			sc.Feed([]byte(styledStatusLine(statusRowState(), captureCols, tc.pal)))

			row := sc.Line(statusCaptureRow)
			labelCol := strings.Index(row, captureStatusModel)
			if labelCol < 0 {
				t.Fatalf("the status row did not reach grid row %d: %q", statusCaptureRow, row)
			}
			liveCol := strings.Index(row, captureStatusLiveWord)
			if liveCol < 0 {
				t.Fatalf("the status row's live hint did not reach grid row %d: %q", statusCaptureRow, row)
			}

			label := sc.Attr(statusCaptureRow, labelCol)
			if !label.Bold {
				t.Errorf("%s: the label cell carries no bold — the attribute emphasis is lost:\n%q", tc.name, row)
			}
			labelAttr := "the label cell"
			assertCapturedColour(t, tc.name, labelAttr, label, tc.labelRGB, tc.labelIdx)

			live := sc.Attr(statusCaptureRow, liveCol)
			liveAttr := "the live-hint cell"
			assertCapturedColour(t, tc.name, liveAttr, live, tc.liveRGB, tc.liveIdx)

			// The row BELOW is untouched, so the capture shows the row and not a
			// filled screen (a "gold everywhere" grid would pass the assertions
			// above while proving nothing).
			if below := sc.Attr(statusCaptureRow+1, 0); below.FgRGB != sgr.NoRGB || below.Fg != sgr.DefaultColor {
				t.Errorf("%s: the cell below the row carries %v — the capture painted more than the status row", tc.name, below)
			}

			// The evidence artifact: the raw bytes as emitted, and the grid dump
			// the assertions read (the dump records the per-cell style, which is
			// what a pixel-level verification consumes downstream).
			if dir := paletteCaptureDir(); dir != "" {
				raw := []byte(strings.Repeat("\r\n", statusCaptureRow) + styledStatusLine(statusRowState(), captureCols, tc.pal))
				if err := os.WriteFile(filepath.Join(dir, "status-row-"+tc.name+".ansi"), raw, 0o644); err != nil {
					t.Fatalf("write raw capture: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "status-row-grid-"+tc.name+".txt"), []byte(sc.Dump().Text()), 0o644); err != nil {
					t.Fatalf("write grid dump: %v", err)
				}
			}
		})
	}
}

// assertCapturedColour checks one cell against the colour its tier must paint: a
// 24-bit value when the tier has one, a palette index otherwise. The two spellings
// are mutually exclusive on a cell (sgr.Style says so), so the check asserts the
// ABSENCE of the other one too — a cell that claimed both would be a renderer bug
// the equality alone would not catch.
func assertCapturedColour(t *testing.T, tier, what string, got sgr.Style, wantRGB, wantIdx int) {
	t.Helper()
	if got.FgRGB != wantRGB {
		t.Errorf("%s: %s carries FgRGB=%s, want %s (Fg=%d) — the row is not the gold this table names",
			tier, what, rgbHex(got.FgRGB), rgbHex(wantRGB), got.Fg)
	}
	if got.Fg != wantIdx {
		t.Errorf("%s: %s carries Fg=%d, want %d (FgRGB=%s)", tier, what, got.Fg, wantIdx, rgbHex(got.FgRGB))
	}
	if wantRGB != sgr.NoRGB && got.Fg != sgr.DefaultColor {
		t.Errorf("%s: %s claims both a 24-bit colour and a palette index %d", tier, what, got.Fg)
	}
}

// rgbHex renders a cell colour for a failure message ("none" for the unset
// sentinel, so a message never prints a hex value that is not a colour).
func rgbHex(rgb int) string {
	if rgb == sgr.NoRGB {
		return "none"
	}
	return fmt.Sprintf("#%06x", rgb)
}
