// cli_capture.go — `gopi tool capture [--screen | --window | --area WxH[+X+Y]]`:
// capture the LIVE X11 desktop — the whole screen, the current window, or an
// arbitrary rectangle of it — to a PNG, from the shell.
//
// THE VERB, AND WHY IT IS NOT `screen_capture`: the capability stopped being
// "a screen" the moment it grew a window and a rectangle mode, and the common
// OS screenshot tool's own vocabulary is exactly this triple — whole screen /
// window / area. One verb with a mode is also how the three shapes stay
// discoverable: `gopi tool capture -h` lists all three, where a second top-level
// verb (`gopi capture_window`) would leave the third one to be guessed.
//
// `gopi tool screen_capture` is NOT removed: it is the CLI mirror of the
// screen_capture LLM TOOL, and its word IS toolnames.ToolScreenCapture, so a
// session log's tool_use stays greppable as a command. It keeps its own
// whole-screen behaviour and forwards here when a mode flag is used (see
// cli_screencapture.go). Dispatch that never breaks a caller's spelling is the
// rule this pair follows.
//
// This is the CLI mirror of the screen_capture tool: same engine
// (tools.ScreenCapture / tools.ScreenCaptureWith), same JSON payload, so the
// human path and the model path can never drift apart. Shell shape mirrors
// cli_screencapture.go / cli_shot.go / cli_vision.go.

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// captureUsage is the -h text for the capture subcommand.
const captureUsage = `gopi tool capture — capture the LIVE X11 desktop to a PNG

Grabs pixels from a real X11 display with a hand-rolled, zero-dependency X11
protocol client (GetImage) — pure Go, no CGO, no external screenshot binary,
and no dependency on ` + "`gopi tool shot`" + `'s headless Chromium.

Three shapes, the common OS screenshot choice:

  gopi tool capture --screen                      # the whole display (default)
  gopi tool capture --window                      # the CURRENT window only
  gopi tool capture --self                        # the window HOSTING this process
  gopi tool capture --area 800x600+100+50         # an arbitrary rectangle

and one read-only mode that captures nothing:

  gopi tool capture --list-windows                # what IS on this display (JSON)

--self vs --window: --window asks the display "which window is ACTIVE?", which
is answered by whatever the user last clicked — a capture taken from a terminal
therefore shows ANOTHER application. --self asks "where am I RUNNING": it reads
this process' controlling tty (/proc/self/fd/0), finds the terminal emulator
that owns that pty's master side, and matches that pid against the EWMH
_NET_WM_PID property of every window. The answer is an identity, not a guess,
and the result's window_route carries the whole chain (pid, tty, window id).
--decorations includes the window-manager frame (title bar, borders) via
_NET_FRAME_EXTENTS; without it the client area is captured.

Prefer --window when the question is about the current window: a whole-screen
grab spends most of its pixels on windows you did not ask about, and every
downstream reader (vision_analyze, a human) pays for them. --area takes
WxH[+X+Y], so "800x600" is the top-left 800x600; the rectangle must FIT the
display — one that runs off the screen is refused, never silently clipped.

When --self cannot find you — an X-forwarded display, a container boundary, a
process with no tty — it REFUSES and names what it tried. --list-windows is that
refusal's next step: it enumerates the display's top-level windows (id,
rectangle, _NET_WM_PID, title, which one is active) so you can pick one and ask
for it by its rectangle (--area) rather than guessing. It reads the display and
writes nothing, so it takes no --out.

Use it as the "eye" of a computer-use loop:
  gopi tool capture --window --out before.png
  …drive the GUI (gopi tool act …)…
  gopi tool capture --window --out after.png     # compare the sha256_16 field

Usage:
  gopi tool capture [--screen | --window | --self | --area WxH[+X+Y]] [flags]
  gopi tool capture --list-windows [--display DISPLAY]

Flags:
  --screen            Capture the whole display (the default).
  --window            Capture the ACTIVE window (the current one).
  --self              Capture the window that HOSTS this gopi process — the
                      terminal you are looking at, resolved by pid.
  --decorations       With --self: include the window-manager frame (title
                      bar and borders) instead of the client area.
  --area WxH[+X+Y]    Capture that rectangle of the display, in pixels.
  --list-windows      Print what is ON the display as JSON — the top-level
                      windows with their ids, rectangles, pids and titles, and
                      which one is active — instead of capturing. Read-only:
                      it writes no PNG, so it takes no --out.
  --display DISPLAY   X11 display to capture (default: $DISPLAY — the display
                      of the desktop the agent runs on).
  --out PATH          Output PNG path. Default: screen-<unix>.dump.png in the
                      capture class directory under gopi's scratch tree
                      (<scratch home>/dumps/gopi-captures/) — NEVER the current
                      directory, because a run's cwd is its own git ground and an
                      untracked PNG there breaks Rule 30. The printed JSON always
                      carries the absolute path in its path field.

The result is JSON: path, display, mode, width/height, x/y (the rectangle's
position on the screen), depth, bytes and a sha256_16 content hash. For a
--window capture it also names the window id and the route that resolved it
(_NET_ACTIVE_WINDOW, GetInputFocus, or QueryPointer) — a fallback is visible,
never passed off as the authoritative answer.

` + "`gopi tool screen_capture`" + ` is the whole-screen alias of this verb (it is the
screen_capture TOOL's own name, kept greppable); any mode flag given to it is
forwarded here.

X11 only: on a Wayland session this fails loudly (no portal path is
implemented — see devdoc/screen-capture-window-area.md §5).
`

// captureFlagListWindows names the ONE read-only mode flag. It is declared
// beside the other flag spellings so the parser, the member check, the usage
// text and the alias's forwarding test cannot disagree about it (Rule 1b).
const captureFlagListWindows = "list-windows"

// captureFlagMembers lists every flag the subcommand accepts.
var captureFlagMembers = map[string]bool{
	captureFlagScreen:      true,
	captureFlagWindow:      true,
	captureFlagSelf:        true,
	captureFlagDecorations: true,
	captureFlagArea:        true,
	captureFlagDisplay:     true,
	captureFlagOut:         true,
	captureFlagListWindows: true,
}

// isCaptureSubcommand reports whether argv begins with the capture
// subcommand and runs it.
func isCaptureSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbCapture {
		runCaptureSubcommand(args[1:])
		return true
	}
	return false
}

// captureRequest is the parsed invocation: which of the three modes was asked
// for, and the capture arguments that go with it.
type captureRequest struct {
	mode        string
	area        string
	window      bool
	screen      bool
	self        bool
	decorations bool

	// listWindows is not a CAPTURE mode: it selects the read-only listing
	// (tools.ScreenCaptureListWindows), which writes no file. It is exclusive
	// with every capturing mode for the same reason they are exclusive with
	// each other — a caller who asked for two things must be told, not handed
	// one of them.
	listWindows bool

	display string
	out     string
}

// parseCaptureArgs parses the flags without touching the process
// (unit-testable, per the parseScreenCaptureArgs pattern).
func parseCaptureArgs(args []string) (captureRequest, error) {
	var req captureRequest
	fs := newMemoryFlagSet()
	for name := range captureFlagMembers {
		switch name {
		case captureFlagScreen, captureFlagWindow, captureFlagSelf, captureFlagDecorations, captureFlagListWindows:
			fs.boolean(name)
		}
	}
	positional, err := fs.parse(args)
	if err != nil {
		return req, err
	}
	for _, name := range fs.order {
		if !captureFlagMembers[name] {
			return req, fmt.Errorf("unknown flag --%s", name)
		}
	}
	if len(positional) > 0 {
		return req, fmt.Errorf("unexpected positional argument %q (flags only)", positional[0])
	}
	req.screen = fs.has(captureFlagScreen)
	req.window = fs.has(captureFlagWindow)
	req.self = fs.has(captureFlagSelf)
	req.decorations = fs.has(captureFlagDecorations)
	req.listWindows = fs.has(captureFlagListWindows)
	req.area = strings.TrimSpace(fs.get(captureFlagArea))
	req.display = strings.TrimSpace(fs.get(captureFlagDisplay))
	req.out = strings.TrimSpace(fs.get(captureFlagOut))
	return req, req.resolveMode()
}

// resolveMode turns the flags into exactly one of the three modes, refusing
// combinations that name two. A conflict is an error rather than a precedence
// rule: silently preferring one shape would hand back a rectangle the caller
// did not ask for (Rule 15 — a substituted geometry is visible).
func (r *captureRequest) resolveMode() error {
	asked := 0
	if r.screen {
		asked++
	}
	if r.window {
		asked++
	}
	if r.self {
		asked++
	}
	if r.area != "" {
		asked++
	}
	if r.listWindows {
		asked++
	}
	if asked > 1 {
		return fmt.Errorf("--screen, --window, --self, --area and --%s are mutually exclusive; give at most one", captureFlagListWindows)
	}
	if r.listWindows {
		if r.decorations {
			return fmt.Errorf("--decorations applies to --self only: it expands a window's rectangle by %s, which is only meaningful for the window this process hosts", "_NET_FRAME_EXTENTS")
		}
		if r.out != "" {
			return fmt.Errorf("--%s prints a listing and writes no file, so --out has nothing to name", captureFlagListWindows)
		}
		return nil
	}
	switch {
	case r.area != "":
		r.mode = tools.ScreenCaptureModeArea
	case r.self:
		r.mode = tools.ScreenCaptureModeSelf
	case r.window:
		r.mode = tools.ScreenCaptureModeWindow
	default:
		r.mode = tools.ScreenCaptureModeScreen
	}
	if r.decorations && r.mode != tools.ScreenCaptureModeSelf {
		return fmt.Errorf("--decorations applies to --self only: it expands a window's rectangle by %s, which is only meaningful for the window this process hosts", "_NET_FRAME_EXTENTS")
	}
	return nil
}

// captureFlagScreen / captureFlagWindow / captureFlagArea / captureFlagDisplay
// / captureFlagOut name the flags, so the parser, the member check and the
// usage text cannot disagree about a spelling (Rule 1b).
const (
	captureFlagScreen      = "screen"
	captureFlagWindow      = "window"
	captureFlagSelf        = "self"
	captureFlagDecorations = "decorations"
	captureFlagArea        = "area"
	captureFlagDisplay     = "display"
	captureFlagOut         = "out"
)

// captureOptionsFor builds the engine options for a parsed request. The area
// string is parsed HERE (through the engine's own grammar) so a malformed
// rectangle never reaches the capture.
func captureOptionsFor(req captureRequest) (tools.ScreenCaptureOptions, error) {
	opts := tools.ScreenCaptureOptions{
		Mode:        req.mode,
		Display:     req.display,
		Out:         req.out,
		Decorations: req.decorations,
	}
	if req.mode == tools.ScreenCaptureModeArea {
		rect, err := tools.ParseScreenCaptureArea(req.area)
		if err != nil {
			return opts, err
		}
		opts.Area = rect
	}
	return opts, nil
}

// runCaptureSubcommand executes the capture subcommand.
func runCaptureSubcommand(args []string) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(captureUsage)
		return
	}
	req, err := parseCaptureArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbCapture, err)
		os.Exit(1)
	}
	// The listing is not a capture: it is dispatched BEFORE the engine so that
	// the read-only mode cannot inherit a write path (it has no output file to
	// report, and the conflict/--out refusals above already ran).
	if req.listWindows {
		out, err := tools.ScreenCaptureListWindows(req.display)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbCapture, err)
			os.Exit(1)
		}
		fmt.Println(out)
		return
	}
	opts, err := captureOptionsFor(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbCapture, err)
		os.Exit(1)
	}
	result, err := tools.ScreenCaptureWith(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbCapture, err)
		os.Exit(1)
	}
	fmt.Println(result)
}

// captureModeFlagsRequested reports whether an argument list asks for a
// non-screen mode. It is what lets `gopi tool screen_capture` forward to
// `gopi tool capture` instead of silently ignoring a rectangle it does not know how
// to honour (its own parser would reject the flag as unknown; forwarding is
// the behaviour a caller actually wants).
func captureModeFlagsRequested(args []string) bool {
	for _, a := range args {
		name := strings.TrimPrefix(a, "--")
		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
		}
		switch name {
		case captureFlagWindow, captureFlagSelf, captureFlagDecorations, captureFlagArea, captureFlagListWindows:
			return true
		}
	}
	return false
}
