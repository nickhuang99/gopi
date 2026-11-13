package tools

// screen_capture tool: grab the LIVE X11 desktop — the whole screen, the
// CURRENT WINDOW, or an arbitrary rectangle of it — into a PNG file and return
// machine-readable ground truth (path, pixel size, position, display, byte
// count, content hash).
//
// This is the "eye" of the computer-use loop: it is deliberately NOT the
// headless image on a laptop, the true screen pixels on the X11 desktop.
// It is pure Go, zero-dependency and zero-CGO: the X11 protocol client lives
// in internal/shot/x11.go + internal/shot/x11rect.go (see
// devdoc/screen-capture-and-actuation-tools.md §4 for why a hand-rolled client
// beats a dependency here, and devdoc/screen-capture-window-area.md for the
// rect modes).
//
// The three modes are the common OS screenshot choice, and the reason the
// rect modes exist is a CONTEXT cost, not a feature count: a whole-desktop PNG
// spends most of its pixels on windows the caller did not ask about, and every
// downstream reader (vision_analyze, a human) pays for them. "What is the
// current window doing?" is answered by --window.
//
// Pairing: screen_capture (see) → actuation via `gopi tool act` (touch) →
// screen_capture again + comparison (verify).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/shot"
)

const (
	// screenCaptureDefaultPrefix names the default output file (a unix
	// timestamp is appended, matching the `gopi tool shot` convention).
	screenCaptureDefaultPrefix = "screen-"

	// NOTE: there is deliberately no `screenCapturePNGExt` constant here any
	// more. It spelled the DEFAULT file's extension, and the default is no
	// longer built in this package at all — internal/scratch owns the whole
	// name (`CaptureDumpFileSuffix`), because the reclaim predicate has to
	// recognise it from the name alone (Rule 14: one definition, on the side
	// that both writes and collects). Keep it that way: a local extension
	// literal would be a second, drifting answer.

	// screenCaptureFilePerm is the mode of the written PNG.
	screenCaptureFilePerm = 0o644

	// screenCaptureHashLen is how many hex chars of the content hash are
	// reported (enough to spot a changed screen, short enough to read).
	screenCaptureHashLen = 16
)

// The capture modes, re-exported from the engine so a caller of THIS package
// never needs the string literal (Rule 14: one definition, in internal/shot,
// referenced from here and from the CLI).
const (
	ScreenCaptureModeScreen = shot.CaptureModeScreen
	ScreenCaptureModeWindow = shot.CaptureModeWindow
	ScreenCaptureModeArea   = shot.CaptureModeArea

	// ScreenCaptureModeSelf is the window that HOSTS the gopi process itself
	// (resolved by _NET_WM_PID through the process' own tty chain — see
	// internal/shot/pidwindow.go). It answers a different question from
	// ScreenCaptureModeWindow: not "what is focused" but "where am I running".
	ScreenCaptureModeSelf = shot.CaptureModeSelf
)

// screenCaptureResult is the JSON payload returned to the model. It is the
// ground truth the model MUST quote instead of guessing dimensions.
type screenCaptureResult struct {
	Path    string `json:"path"`
	Display string `json:"display"`
	Mode    string `json:"mode"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Depth   int    `json:"depth"`
	Bands   int    `json:"bands"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256_16"`
	Window  string `json:"window,omitempty"`
	Route   string `json:"window_route,omitempty"`
	Hint    string `json:"hint"`
}

// ScreenCapture grabs the live X11 desktop to a PNG and returns the result as
// JSON. display == "" falls back to $DISPLAY (Rule 2: one env read, in
// internal/shot). out == "" writes screen-<unix>.dump.png into the CAPTURE
// class directory of gopi's scratch tree (scratch.CaptureDumpFile) and reports
// its absolute path — never the process's working directory, which for a task
// run IS its clone ground (Rule 30). It is the whole-screen capture — the mode-preserving entry point
// for every existing caller (the `gopi tool screen_capture` verb, `gopi tool shot
// --self`, and the agent dispatch arm).
func ScreenCapture(display, out string) (string, error) {
	png, geom, err := shot.CaptureX11Provider(shot.CaptureOptions{Display: display, Mode: shot.CaptureModeScreen})
	if err != nil {
		return "", fmt.Errorf("screen_capture: %w", err)
	}
	return screenCaptureWrite(png, geom, out)
}

// ScreenCaptureWith grabs the requested MODE of the live X11 desktop and
// returns the result as JSON. It is the ONE engine behind all three faces
// (the LLM tool, `gopi tool capture`, and `gopi tool screen_capture`), so the human path
// and the model path cannot drift.
//
// opts.Mode: "" or ScreenCaptureModeScreen (whole screen),
// ScreenCaptureModeWindow (the active window, or opts.Window),
// ScreenCaptureModeArea (opts.Area, the "WxH+X+Y" rectangle).
func ScreenCaptureWith(opts ScreenCaptureOptions) (string, error) {
	png, geom, err := shot.CaptureX11(shot.CaptureOptions{
		Mode:        opts.Mode,
		Display:     opts.Display,
		Window:      opts.Window,
		Area:        opts.Area,
		Decorations: opts.Decorations,
	})
	if err != nil {
		return "", fmt.Errorf("screen_capture: %w", err)
	}
	return screenCaptureWrite(png, geom, opts.Out)
}

// ScreenCaptureOptions is the tool/CLI argument set for the mode-aware
// capture. Area is a parsed rectangle (the CLI parses its own spelling; the
// model passes the "WxH[+X+Y]" string through ParseScreenCaptureArea).
type ScreenCaptureOptions struct {
	Mode    string
	Display string
	Out     string
	Window  uint32
	Area    shot.Rect

	// Decorations picks the window-manager FRAME (title bar, borders) over the
	// client area for ScreenCaptureModeSelf. The default keeps the client area
	// — the pixels the application drew — which is what the mode reported
	// before this flag existed.
	Decorations bool
}

// ParseScreenCaptureArea parses the model-facing / CLI area spelling. It is a
// thin, named re-export of the engine's grammar so there is exactly ONE parser
// (Rule 14) — a second one would be the one that drifts.
func ParseScreenCaptureArea(s string) (shot.Rect, error) {
	return shot.ParseArea(s)
}

// ScreenCaptureListWindows enumerates a display's top-level windows and returns
// the listing as JSON — the read-only mode behind `gopi tool capture --list-windows`.
//
// WHY IT EXISTS: every capture mode needs a window IDENTITY, and two of them can
// fail to produce one — `--window` answers "what is ACTIVE" (whatever the user
// last clicked, which from a background session is somebody else's program), and
// `--self` refuses on a display where this process cannot be located in a window
// (an X-forwarded display, a container boundary). The honest answer to that
// refusal is to LOOK: name what is on the display, and let the caller pick one.
//
// It writes no file and takes no --out (there is nothing to name), and it reads
// the display through the same engine as every other mode
// (shot.ListWindows): QueryTree, GetGeometry, TranslateCoordinates,
// GetProperty and InternAtom(only-if-exists). Nothing is created, mapped, moved
// or destroyed on the display — the wire-level read-only property is asserted by
// internal/shot's TestListWindowsIssuesOnlyReadRequests.
func ScreenCaptureListWindows(display string) (string, error) {
	list, err := shot.ListWindows(display)
	if err != nil {
		return "", fmt.Errorf("screen_capture: %w", err)
	}
	res := screenCaptureWindowList{
		Display:     list.Display,
		RootWidth:   list.RootW,
		RootHeight:  list.RootH,
		Windows:     list.Windows,
		Active:      list.ActiveID,
		ActiveRoute: list.ActiveRoute,
		EWMH:        list.EWMH,
		Total:       list.Total,
		Truncated:   list.Truncated,
		Hint:        screenCaptureWindowListHint,
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("screen_capture: marshal window list: %w", err)
	}
	return string(encoded), nil
}

// screenCaptureWindowListHint tells the caller what to DO with a listing: the
// ids are the tool's own region spelling, and an id is only useful if the reader
// knows where to paste it and what makes it stable. It names --self as the first
// answer, because a listing is the FALLBACK for a question that intends "the
// window I am in".
const screenCaptureWindowListHint = "These are the display's top-level windows: id (hex), rectangle in screen coordinates, _NET_WM_PID when the client publishes one, and title. To capture one, pass its rectangle as the area (\"WxH+X+Y\", from x/y/width/height) — that needs no window protocol and works even for a window whose client publishes no pid. If the question is \"the window I am running in\", ask --self first: it resolves the identity instead of making you pick from a list."

// screenCaptureWindowList is the JSON payload of `--list-windows`. Every field
// is present on the wire: `total` next to `windows` because a list cut at the
// engine's cap is otherwise indistinguishable from a complete one, and
// `active_route` next to `active_window` because a fallback answer must be
// visible as one (Rule 15).
type screenCaptureWindowList struct {
	Display     string             `json:"display"`
	RootWidth   int                `json:"root_width"`
	RootHeight  int                `json:"root_height"`
	Windows     []shot.XWindowInfo `json:"windows"`
	Active      string             `json:"active_window,omitempty"`
	ActiveRoute string             `json:"active_route,omitempty"`
	EWMH        bool               `json:"ewmh"`
	Total       int                `json:"total"`
	Truncated   bool               `json:"truncated"`
	Hint        string             `json:"hint"`
}

// ScreenCaptureDefaultPath is the ONE answer to "where does a capture with no
// `--out` land?" for the whole screen_capture family (this package's writer, the
// `gopi tool capture` verb and the `gopi tool screen_capture` verb all reach it
// through screenCaptureWrite).
//
// IT IS NOT THE CALLER'S CWD (measured 2026-11-07, task 01a1b3b1). The default
// used to be a bare relative `screen-<unix>.png`, which `os.WriteFile` resolved
// against the process's working directory — and a task run's cwd IS its clone
// ground, so every capture left an untracked PNG inside a git work tree
// (PROJECT.md Rule 30: the close-out records `completed-unclean`, and no merge
// path picks the file up). The destination is therefore resolved by
// internal/scratch.CaptureDumpFile, which returns an ABSOLUTE path inside the
// capture class directory of gopi's scratch tree — a tree the existing
// declared-tree sweep already walks and age-collects (Rule 35: the reclaim owner
// is named and already ticks; nothing new was installed).
//
// Exported so the regression test can assert the destination without a display:
// the property under test is WHERE a default capture lands, and X11 is a
// different question.
func ScreenCaptureDefaultPath() (string, error) {
	return scratch.CaptureDumpFile(screenCaptureDefaultPrefix)
}

// screenCaptureWrite writes the PNG and assembles the JSON result. Shared by
// both entry points so the reported ground truth is identical in every mode.
func screenCaptureWrite(png []byte, geom shot.Geometry, out string) (string, error) {
	path := out
	if path == "" {
		var derr error
		path, derr = ScreenCaptureDefaultPath()
		if derr != nil {
			return "", fmt.Errorf("screen_capture: %w", derr)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if err := os.WriteFile(path, png, screenCaptureFilePerm); err != nil {
		return "", fmt.Errorf("screen_capture: write %s: %w", path, err)
	}

	sum := sha256.Sum256(png)
	mode := geom.Mode
	if mode == "" {
		mode = ScreenCaptureModeScreen
	}
	res := screenCaptureResult{
		Path:    abs,
		Display: geom.Display,
		Mode:    mode,
		Width:   geom.Width,
		Height:  geom.Height,
		X:       geom.X,
		Y:       geom.Y,
		Depth:   geom.Depth,
		Bands:   geom.Bands,
		Bytes:   len(png),
		SHA256:  hex.EncodeToString(sum[:])[:screenCaptureHashLen],
		Window:  geom.Window,
		Route:   geom.WindowLabel,
		Hint:    screenCaptureHint(mode),
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("screen_capture: marshal result: %w", err)
	}
	return string(encoded), nil
}

// screenCaptureHint tells the caller what to DO with what it just got. It is
// mode-dependent on purpose: the whole-screen hint is about comparing two
// captures, while a rect capture additionally has to be read as "this is only
// that window" so the model does not reason about pixels that are not in the
// image.
func screenCaptureHint(mode string) string {
	switch mode {
	case ScreenCaptureModeWindow, ScreenCaptureModeArea, ScreenCaptureModeSelf:
		return "This PNG contains ONLY the requested rectangle, at x,y in screen coordinates. Pass this path to vision_analyze to read it; compare sha256_16 across captures to prove it changed (or did not)."
	default:
		return "Pass this path to vision_analyze to read the screen; compare sha256_16 across captures to prove the screen changed (or did not)."
	}
}
