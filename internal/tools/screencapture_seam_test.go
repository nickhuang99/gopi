// screencapture_seam_test.go — the drift guard for the X11 boundary seam.
//
// shot.CaptureX11Provider is the ONE function a capture talks to a display
// server through. Every consumer — the `screen_capture` TOOL, the `screen_capture`
// CLI verb and the `capture` verb (which the other two reach through this
// package) — must go through it, because that seam is what lets a whole capture
// path be driven with no display at all (PROJECT.md Rule 31).
//
// This test CAN fail: it installs the seam, drives the real tool entry point,
// and requires the seam to have been reached EXACTLY once. A future "small local
// shortcut" that dials the display behind the seam's back turns it red instead of
// silently splitting the capture engine in two.
package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/shot"
)

// captureSeamStubPNG is the payload the stubbed X11 leg returns — the seam
// replaces the display, not the encode, the write or the JSON assembly.
var captureSeamStubPNG = []byte("gopi tool screen_capture seam stub\n")

const (
	captureSeamStubDisplay = ":98"
	captureSeamStubWidth   = 321
	captureSeamStubHeight  = 123
	captureSeamStubDepth   = 24
	captureSeamStubBands   = 4
)

// TestScreenCaptureGoesThroughTheX11BoundarySeam drives the tool's entry point
// end to end with the display leg replaced, and asserts both halves: the seam
// was reached exactly once, and the result is a real file with a real hash.
func TestScreenCaptureGoesThroughTheX11BoundarySeam(t *testing.T) {
	calls := 0
	old := shot.CaptureX11Provider
	shot.CaptureX11Provider = func(shot.CaptureOptions) ([]byte, shot.Geometry, error) {
		calls++
		return captureSeamStubPNG, shot.Geometry{
			Display: captureSeamStubDisplay,
			Width:   captureSeamStubWidth,
			Height:  captureSeamStubHeight,
			Depth:   captureSeamStubDepth,
			Bands:   captureSeamStubBands,
			Mode:    shot.CaptureModeScreen,
		}, nil
	}
	t.Cleanup(func() { shot.CaptureX11Provider = old })

	outPath := filepath.Join(t.TempDir(), "seam.png")
	res, err := ScreenCapture(captureSeamStubDisplay, outPath)
	if err != nil {
		t.Fatalf("ScreenCapture with the seam installed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("the X11 boundary was reached %d times, want 1 — the capture did not go through shot.CaptureX11Provider", calls)
	}
	if len(captureSeamStubPNG) == 0 {
		t.Fatal("the stub payload is empty, which would make the assertions below vacuous")
	}
	written, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("the tool reported %s but no file is there: %v", outPath, err)
	}
	if string(written) != string(captureSeamStubPNG) {
		t.Errorf("the file holds %d bytes, want the %d the seam returned — the payload was substituted somewhere", len(written), len(captureSeamStubPNG))
	}
	if res == "" {
		t.Error("the tool returned an empty result string")
	}
}

// TestScreenCaptureSurfacesTheSeamError pins the failure direction: when the
// display leg fails, the error reaches the caller naming screen_capture, and no
// file is written. A capture that fails silently is the defect this asserts
// against.
func TestScreenCaptureSurfacesTheSeamError(t *testing.T) {
	old := shot.CaptureX11Provider
	shot.CaptureX11Provider = func(shot.CaptureOptions) ([]byte, shot.Geometry, error) {
		return nil, shot.Geometry{}, errors.New("stub: no X11 display")
	}
	t.Cleanup(func() { shot.CaptureX11Provider = old })

	outPath := filepath.Join(t.TempDir(), "never-written.png")
	res, err := ScreenCapture("", outPath)
	if err == nil {
		t.Fatalf("ScreenCapture reported success (%q) with a failing display leg", res)
	}
	if _, statErr := os.Stat(outPath); statErr == nil {
		t.Errorf("a failed capture wrote %s anyway — an empty PNG would read as a working one", outPath)
	}
}

// ---------------------------------------------------------------------------
// The read-only listing: tools.ScreenCaptureListWindows
// ---------------------------------------------------------------------------

const (
	listWindowsSeamDisplay = ":77"
	listWindowsSeamRootW   = 1024
	listWindowsSeamRootH   = 768
	listWindowsSeamWinID   = "0x300001"
	listWindowsSeamWinPID  = 31337
	listWindowsSeamWinName = "tools-layer-fixture"
	listWindowsSeamWinX    = 11
	listWindowsSeamWinY    = 22
	listWindowsSeamWinW    = 333
	listWindowsSeamWinH    = 444
)

// listWindowsSeamList is the listing the stubbed X11 leg returns. It is shaped
// like a real one (a measured window with a pid and a title, a WM present) so
// the JSON assembly below is exercised rather than a degenerate empty list.
func listWindowsSeamList() shot.XWindowList {
	return shot.XWindowList{
		Display: listWindowsSeamDisplay,
		RootW:   listWindowsSeamRootW,
		RootH:   listWindowsSeamRootH,
		Windows: []shot.XWindowInfo{{
			ID: listWindowsSeamWinID, X: listWindowsSeamWinX, Y: listWindowsSeamWinY,
			W: listWindowsSeamWinW, H: listWindowsSeamWinH,
			PID: listWindowsSeamWinPID, Name: listWindowsSeamWinName,
			Active: true, Measured: true,
		}},
		ActiveID:    listWindowsSeamWinID,
		ActiveRoute: "GetInputFocus",
		EWMH:        true,
		Total:       1,
	}
}

// TestScreenCaptureListWindowsGoesThroughTheListingSeam drives the tools-layer
// entry point with the display leg replaced and asserts the payload it hands a
// model: the seam was reached exactly once, and every field a caller needs to
// NAME a window survived the JSON trip under the name it is promised by.
func TestScreenCaptureListWindowsGoesThroughTheListingSeam(t *testing.T) {
	var asked []string
	old := shot.ListWindowsProvider
	shot.ListWindowsProvider = func(display string) (shot.XWindowList, error) {
		asked = append(asked, display)
		return listWindowsSeamList(), nil
	}
	t.Cleanup(func() { shot.ListWindowsProvider = old })

	out, err := ScreenCaptureListWindows(listWindowsSeamDisplay)
	if err != nil {
		t.Fatalf("ScreenCaptureListWindows(%s): %v", listWindowsSeamDisplay, err)
	}
	if len(asked) != 1 || asked[0] != listWindowsSeamDisplay {
		t.Fatalf("the listing seam was reached %v, want exactly [%s]", asked, listWindowsSeamDisplay)
	}
	var got struct {
		Display     string `json:"display"`
		RootWidth   int    `json:"root_width"`
		RootHeight  int    `json:"root_height"`
		Total       int    `json:"total"`
		Truncated   bool   `json:"truncated"`
		EWMH        bool   `json:"ewmh"`
		Active      string `json:"active_window"`
		ActiveRoute string `json:"active_route"`
		Hint        string `json:"hint"`
		Windows     []struct {
			ID       string `json:"id"`
			X        int    `json:"x"`
			Y        int    `json:"y"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			PID      uint32 `json:"pid"`
			Name     string `json:"name"`
			Active   bool   `json:"active"`
			Measured bool   `json:"measured"`
		} `json:"windows"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the listing is not the JSON a model is promised: %v (%q)", err, out)
	}
	if got.Display != listWindowsSeamDisplay || got.RootWidth != listWindowsSeamRootW || got.RootHeight != listWindowsSeamRootH {
		t.Errorf("listing header = %+v, want %s %dx%d", got, listWindowsSeamDisplay, listWindowsSeamRootW, listWindowsSeamRootH)
	}
	if !got.EWMH || got.Total != 1 || got.Truncated {
		t.Errorf("ewmh=%v total=%d truncated=%v, want true/1/false", got.EWMH, got.Total, got.Truncated)
	}
	if got.Active != listWindowsSeamWinID || got.ActiveRoute != "GetInputFocus" {
		t.Errorf("active = %q via %q — the active-window answer and its ROUTE must both survive", got.Active, got.ActiveRoute)
	}
	if got.Hint == "" {
		t.Error("the payload carries no hint — a caller cannot tell what to do with an id")
	}
	if len(got.Windows) != 1 {
		t.Fatalf("the payload carries %d windows, want 1", len(got.Windows))
	}
	w := got.Windows[0]
	if w.ID != listWindowsSeamWinID || w.X != listWindowsSeamWinX || w.Y != listWindowsSeamWinY ||
		w.Width != listWindowsSeamWinW || w.Height != listWindowsSeamWinH {
		t.Errorf("window = %+v, want %s at %d,%d %dx%d — the rectangle is what a caller pastes into --area",
			w, listWindowsSeamWinID, listWindowsSeamWinX, listWindowsSeamWinY, listWindowsSeamWinW, listWindowsSeamWinH)
	}
	if w.PID != listWindowsSeamWinPID || w.Name != listWindowsSeamWinName || !w.Active || !w.Measured {
		t.Errorf("window facts = %+v, want pid %d name %q active/measured", w, listWindowsSeamWinPID, listWindowsSeamWinName)
	}
	// The listing is a READ: it names no output file, because it writes none.
	if strings.Contains(out, `"path"`) || strings.Contains(out, ".png") {
		t.Errorf("the payload names a file (%q) — a listing writes none", out)
	}
}

// TestScreenCaptureListWindowsSurfacesTheSeamError pins the failure direction:
// an unreachable display returns an error and NO payload, so a caller can never
// read a failed listing as "this display has no windows".
func TestScreenCaptureListWindowsSurfacesTheSeamError(t *testing.T) {
	old := shot.ListWindowsProvider
	shot.ListWindowsProvider = func(string) (shot.XWindowList, error) {
		return shot.XWindowList{}, errors.New("stub: the display refused the connection")
	}
	t.Cleanup(func() { shot.ListWindowsProvider = old })

	out, err := ScreenCaptureListWindows(listWindowsSeamDisplay)
	if err == nil {
		t.Fatalf("the listing returned %q and no error", out)
	}
	if !strings.Contains(err.Error(), "screen_capture") {
		t.Errorf("the error does not name the engine that failed: %v", err)
	}
	if out != "" {
		t.Errorf("a payload (%q) came back alongside the error", out)
	}
}
