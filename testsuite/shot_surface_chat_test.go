package testsuite

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/shot"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/text2img"
)

// The surface capture's E2E geometry (Rule 1). The CELL width is the renderer's
// own default (font size / 2 — the monospace advance at DefaultSize), derived
// from the exported constants rather than copied as a policy of its own.
var (
	shotE2ECellW  = text2img.DefaultSize / shotE2ECellDivisor
	shotE2EMargin = text2img.DefaultMargin
)

// shotE2ECellDivisor is the renderer's own cell-width rule: a monospace face at
// N px advances ≈ N/2 px. Mirrored here as arithmetic on an exported constant so
// the E2E check re-derives the same number instead of hardcoding 10.
const shotE2ECellDivisor = 2

// The geometry the E2E runs ask for: a narrow screen and a wide one, and the
// slack the capture's own trim is allowed to carry.
const (
	shotE2ECols       = 100
	shotE2ERows       = 24
	shotE2ERowsNarrow = 16
	shotE2ERowsWide   = 36
	// shotE2EGridRowSlack is how many screen rows the capture may DROP and still
	// be reported as the geometry it was asked for: internal/shot's gridLines
	// folds a trailing run of blank rows (the grid is the capture's truth, and a
	// page of nothing is not part of what the UI drew), so the last row of a
	// screen the UI did not paint to the bottom is legitimately absent.
	shotE2EGridRowSlack = 1
)

// shotE2ELh is the grid path's line height: the renderer derives its default from
// DefaultSize through its own ratio, and DefaultLh is exactly what that ratio
// produces at the default size — so the E2E re-derives the row pitch from
// exported constants instead of hardcoding 32.
const shotE2ELh = text2img.DefaultLh

// gridRows maps an image back to the number of screen rows it shows: the margins
// are the page's, every remaining pixel band is one row of the grid.
func gridRows(img image.Image) int {
	return (img.Bounds().Dy() - 2*shotE2EMargin) / shotE2ELh
}

// captureChatSurface runs the REAL binary's `gopi shot --surface chat` in a fresh
// gopi home and returns the decoded image.
func captureChatSurface(t *testing.T, cols, rows int) image.Image {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "unused"})
	defer llm.Close()

	out := filepath.Join(t.TempDir(), "chat-surface.png")
	// The door is the FAMILY address, spelled from the shared constants: `shot`
	// alone is a RETIRED flat word and the binary REFUSES it, so a test that
	// drives it as argv[0] observes the refusal instead of the capture (Rule 13:
	// the word comes from its owner, never a bare literal). Caught by
	// cmd/agent's retired_verb_test_driver_gate_test.go.
	cmd := exec.Command(testutil.GopiBin(t), cliverbs.Tool, cliverbs.Shot, "--surface", "chat",
		"--cols", strconv.Itoa(cols), "--rows", strconv.Itoa(rows), "--out", out)
	cmd.Env = testutil.MockEnv(t, home, llm.URL())
	cmd.Dir = work
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gopi shot --surface chat: %v\nstderr:\n%s", err, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != out {
		t.Fatalf("shot printed %q, want the output path %q", got, out)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the capture: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the capture is not a PNG: %v", err)
	}
	return img
}

// darkPixels counts the ink in an image — the only cheap way to tell a rendered
// screen from a blank page of the right size.
func darkPixels(img image.Image) int {
	bounds := img.Bounds()
	dark := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r < 0x8000 && g < 0x8000 && b < 0x8000 {
				dark++
			}
		}
	}
	return dark
}

// TestShotSurfaceChatCapturesTheTerminalUI is the end-to-end deliverable check
// for `gopi shot --surface chat`: the REAL binary runs gopi's own chat UI on a
// pty, the terminal screen is interpreted, and the PNG that comes out is a
// MONOSPACE GRID render — its width is a whole number of cells plus the margins.
//
// That signature is what separates this capture from every other surface: the
// web SPA renders a 1440px browser viewport and a prose render lays glyphs out
// at variable advances, while only the cell-grid path produces
// margin + N×cellW exactly.
func TestShotSurfaceChatCapturesTheTerminalUI(t *testing.T) {
	img := captureChatSurface(t, shotE2ECols, shotE2ERows)
	bounds := img.Bounds()
	gridW := bounds.Dx() - 2*shotE2EMargin
	if gridW <= 0 || gridW%shotE2ECellW != 0 {
		t.Fatalf("capture width = %d: %d - %d margins is not a whole number of %dpx cells — this is not a terminal grid render",
			bounds.Dx(), bounds.Dx(), 2*shotE2EMargin, shotE2ECellW)
	}
	// A terminal capture of a chat UI is far narrower than the browser viewport
	// the web surface uses: shot.DefaultViewportWidth.
	if bounds.Dx() >= shot.DefaultViewportWidth {
		t.Fatalf("capture width = %d, at or above the browser viewport %d — the web SPA was captured, not the terminal UI",
			bounds.Dx(), shot.DefaultViewportWidth)
	}
	if dark := darkPixels(img); dark == 0 {
		t.Fatal("the capture is blank — the terminal UI's screen was not rendered")
	}
}

// TestShotSurfaceChatGeometryIsHonoured — the geometry flags reach the engine.
//
// WHAT IS ASSERTED, and why it is the rows COUNT rather than the pixel height:
// the capture's height is NOT the terminal's height. internal/shot's gridLines
// folds a trailing run of blank screen rows before the grid is rendered, so a
// screen the UI painted to its last row captures taller than one it left blank at
// the bottom — the chat REPL's own layout draws a prompt row and a status row and
// leaves everything above them blank, and how far down the grid those two land is
// the terminal's business, not the capture's. Comparing heights across two
// terminals therefore measures where the trim landed, which is exactly the flake
// this test used to report.
//
// A taller terminal is honoured in the ROW PITCH: the image is
// margin + N×lineHeight, so the row count is recovered from the pixels
// (gridRows) and must match the rows asked for, up to the trim's own slack
// (shotE2EGridRowSlack). The old pair (16 vs 36) is kept because it is the one
// that flaked: 36 rows and 16 rows can never be confused for one another however
// the trim lands.
func TestShotSurfaceChatGeometryIsHonoured(t *testing.T) {
	short := captureChatSurface(t, shotE2ECols, shotE2ERowsNarrow)
	tall := captureChatSurface(t, shotE2ECols, shotE2ERowsWide)

	shortRows, tallRows := gridRows(short), gridRows(tall)
	if shortRows > shotE2ERowsNarrow || shortRows < shotE2ERowsNarrow-shotE2EGridRowSlack {
		t.Fatalf("--rows %d → %d screen rows (%dpx with %dpx margins at %dpx per row), want %d (a trailing blank row may be folded)",
			shotE2ERowsNarrow, shortRows, short.Bounds().Dy(), shotE2EMargin, shotE2ELh, shotE2ERowsNarrow)
	}
	if tallRows > shotE2ERowsWide || tallRows < shotE2ERowsWide-shotE2EGridRowSlack {
		t.Fatalf("--rows %d → %d screen rows (%dpx with %dpx margins at %dpx per row), want %d (a trailing blank row may be folded)",
			shotE2ERowsWide, tallRows, tall.Bounds().Dy(), shotE2EMargin, shotE2ELh, shotE2ERowsWide)
	}
	// The two terminals must not collapse onto the same screen: that is the
	// failure the pixel-height assertion was reaching for, stated in the
	// observable the capture actually controls.
	if tallRows <= shortRows {
		t.Fatalf("--rows %d → %d rows and --rows %d → %d rows — the rows flag did not change the capture",
			shotE2ERowsNarrow, shortRows, shotE2ERowsWide, tallRows)
	}
	// The flags reach the rasterizer too, not just the grid interpreter: the row
	// pitch is the renderer's own line height, and neither capture may collapse
	// to a page with no grid in it.
	for name, img := range map[string]image.Image{"--rows " + strconv.Itoa(shotE2ERowsNarrow): short, "--rows " + strconv.Itoa(shotE2ERowsWide): tall} {
		if h := img.Bounds().Dy(); h <= 2*shotE2EMargin {
			t.Fatalf("%s captured %dpx, at or below the page margins — no rows were rendered", name, h)
		}
	}
}
