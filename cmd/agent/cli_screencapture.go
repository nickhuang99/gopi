// cli_screencapture.go — `gopi tool screen_capture [--display D] [--out PATH]`:
// capture the LIVE X11 desktop to a PNG from the shell.
//
// This is the CLI mirror of the screen_capture LLM tool: same wire word
// (toolnames.ToolScreenCapture == CliVerbScreenCapture), same engine
// (tools.ScreenCapture), so the human path and the model path can never
// drift apart. Shell shape mirrors cli_shot.go / cli_vision.go.

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// screenCaptureUsage is the -h text for the screen_capture subcommand.
const screenCaptureUsage = `gopi tool screen_capture — capture the LIVE X11 desktop to a PNG

Grabs the root window of an X11 display (the real screen: every window
actually on that display) with a hand-rolled, zero-dependency X11 protocol
client (GetImage) — pure Go, no CGO, no external screenshot binary, and no
dependency on ` + "`gopi tool shot`" + `'s headless Chromium.

Use it as the "eye" of a computer-use loop:
  gopi tool screen_capture --out before.png
  …drive the GUI (gopi tool act …)…
  gopi tool screen_capture --out after.png     # compare the sha256_16 field

Usage:
  gopi tool screen_capture [flags]

Flags:
  --display DISPLAY   X11 display to capture (default: $DISPLAY — the display
                      of the desktop the agent runs on).
  --out PATH          Output PNG path. Default: screen-<unix>.dump.png in the
                      capture class directory under gopi's scratch tree
                      (<scratch home>/dumps/gopi-captures/) — NEVER the current
                      directory, because a run's cwd is its own git ground and an
                      untracked PNG there breaks Rule 30. The printed JSON always
                      carries the absolute path in its path field.

X11 only: on a Wayland session this fails loudly (no portal path is
implemented — see devdoc/screen-capture-and-actuation-tools.md §7).
`

// screenCaptureFlagMembers lists every flag the subcommand accepts.
var screenCaptureFlagMembers = map[string]bool{"display": true, "out": true}

// isScreenCaptureSubcommand reports whether argv begins with the
// screen_capture subcommand and runs it.
func isScreenCaptureSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == toolnames.ToolScreenCapture {
		runScreenCaptureSubcommand(args[1:])
		return true
	}
	return false
}

// parseScreenCaptureArgs parses the flags without touching the process
// (unit-testable, per the parseShotArgs pattern).
func parseScreenCaptureArgs(args []string) (display, out string, err error) {
	fs := newMemoryFlagSet()
	positional, err := fs.parse(args)
	if err != nil {
		return "", "", err
	}
	for _, name := range fs.order {
		if !screenCaptureFlagMembers[name] {
			return "", "", fmt.Errorf("unknown flag --%s", name)
		}
	}
	if len(positional) > 0 {
		return "", "", fmt.Errorf("unexpected positional argument %q (flags only)", positional[0])
	}
	return strings.TrimSpace(fs.get("display")), strings.TrimSpace(fs.get("out")), nil
}

// runScreenCaptureSubcommand executes the screen_capture subcommand.
//
// A mode flag (`--window` / `--area`) is FORWARDED to `gopi tool capture`: this verb
// is the whole-screen spelling that carries the screen_capture TOOL's own name,
// and a caller who types `gopi tool screen_capture --window` means the window
// capture — refusing the flag would be pedantry about a spelling while a
// silently refused rectangle would be worse (Rule 15). The whole-screen path
// below is untouched, so every existing invocation behaves exactly as before.
func runScreenCaptureSubcommand(args []string) {
	if captureModeFlagsRequested(args) {
		runCaptureSubcommand(args)
		return
	}
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(screenCaptureUsage)
		return
	}
	display, out, err := parseScreenCaptureArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", toolnames.ToolScreenCapture, err)
		os.Exit(1)
	}
	result, err := tools.ScreenCapture(display, out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", toolnames.ToolScreenCapture, err)
		os.Exit(1)
	}
	fmt.Println(result)
}
