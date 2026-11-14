// surface.go — WHAT `gopi tool shot` points at. The web SPA is one surface among
// three: gopi's terminal UI (a pty capture interpreted by a real VT grid) and
// the display itself (the X11 root window) are the others, so "shot gopi's own
// UI" no longer means "gopi has a browser open".
//
// The surface is a first-class option, not a hidden mode: the CLI takes
// --surface, the value is validated against Surfaces(), and every surface
// reports its own failure loudly (no browser, no pty, no DISPLAY).
package shot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/text2img"
)

// Surface names (Rule 1b: the accepted values live here, once) — the CLI, the
// usage text and the validation all read these, never a bare literal.
const (
	// SurfaceWeb captures gopi's embedded web chat SPA over Chromium/CDP.
	SurfaceWeb = "web"
	// SurfaceChat captures a TERMINAL UI: the child runs on a pty, its byte
	// stream is interpreted into a cell grid, and the grid is painted to PNG.
	SurfaceChat = "chat"
	// SurfaceScreen captures the live display — the ordinary system screenshot
	// (the X11 root window), no browser and no pty involved.
	SurfaceScreen = "screen"
)

// DefaultSurface is what `gopi tool shot --self` has always meant (backwards
// compatible: an unset --surface keeps capturing the web SPA).
const DefaultSurface = SurfaceWeb

// Surfaces lists the accepted surface values in the order the usage text and
// the parser's error message present them.
func Surfaces() []string { return []string{SurfaceWeb, SurfaceChat, SurfaceScreen} }

// SurfacesHelp is the accepted-value list as it appears in a flag's help and in
// the error for an unknown value — ONE spelling for both (Rule 1b).
func SurfacesHelp() string { return strings.Join(Surfaces(), "|") }

// NormalizeSurface maps an unset surface to the default and validates the rest.
func NormalizeSurface(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return DefaultSurface, nil
	}
	for _, known := range Surfaces() {
		if s == known {
			return s, nil
		}
	}
	return "", fmt.Errorf("shot: unknown surface %q (want %s) — %s", s, SurfacesHelp(), surfaceHelpLine)
}

// surfaceHelpLine explains the three surfaces in one sentence each.
const surfaceHelpLine = "web = gopi's web chat SPA in headless chromium; " +
	"chat = a terminal UI on a pty, interpreted and rasterized; " +
	"screen = the live display (X11 root window)"

// TextSurfaceOptions are the cell-surface knobs the CLI passes through.
type TextSurfaceOptions struct {
	Argv       []string // the TUI command; empty → DefaultTUIArgv(binPath)
	Cols, Rows int      // terminal geometry in CELLS (not pixels)
	Ready      string   // screen marker to wait for ("" = settle only)
	ExitKeys   string   // keys that ask the UI to end itself ("" = none)
	Scrollback bool     // include the rows pushed off the top
}

// captureTextSurface runs a terminal UI on a pty and renders its interpreted
// cell grid to PNG bytes. It is the `chat` surface's whole engine, kept beside
// the surface constants because the two are one contract.
func captureTextSurface(ctx context.Context, opts Options, ts TextSurfaceOptions) ([]byte, error) {
	argv := ts.Argv
	if len(argv) == 0 {
		bin, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("shot: text surface: cannot resolve this binary to run gopi's own chat UI: %w", err)
		}
		argv = DefaultTUIArgv(bin)
	}
	ready := opts.Text.Ready
	if ready == "" {
		ready = TextReadyDefault
	}
	cap, err := CaptureTextSurface(ctx, TextCaptureOptions{
		Argv:       argv,
		Cols:       ts.Cols,
		Rows:       ts.Rows,
		Ready:      ready,
		ExitKeys:   ts.ExitKeys + textCarriageReturn,
		Scrollback: ts.Scrollback,
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cap.ScreenText) == "" {
		// An empty screen is a failed capture, never a blank PNG that looks
		// like a working one (Rule 15).
		return nil, fmt.Errorf("shot: text surface: %s produced an empty screen (%d bytes received) — the UI drew nothing", argv[0], cap.Bytes)
	}
	scroll := cap.Scrollback
	if !ts.Scrollback {
		scroll = nil
	}
	png, res, err := text2img.RenderGridBytes(cap.Lines, scroll, text2img.GridOptions{})
	if err != nil {
		return nil, fmt.Errorf("shot: text surface: render: %w", err)
	}
	if len(png) == 0 || res.Lines == 0 {
		return nil, errors.New("shot: text surface: render produced no image")
	}
	return png, nil
}

// captureScreenSurface grabs the live display's root window. display "" means
// $DISPLAY, and an absent display surfaces the X11 backend's own error naming
// the facility that is missing.
func captureScreenSurface(display string) ([]byte, error) {
	png, geom, err := CaptureX11Provider(CaptureOptions{Display: display, Mode: CaptureModeScreen})
	if err != nil {
		return nil, err
	}
	if geom.Width <= 0 || geom.Height <= 0 {
		return nil, fmt.Errorf("shot: screen surface: display reported %dx%d", geom.Width, geom.Height)
	}
	return png, nil
}
