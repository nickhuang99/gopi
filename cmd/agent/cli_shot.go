// cli_shot.go — `gopi tool shot` subcommand: screenshot gopi's own web chat UI or
// any URL to a PNG for the vision-debug loop (shot → vision_analyze → fix).
//
// Engine: internal/shot — a zero-dependency raw CDP client driving a local
// headless Chromium (devdoc/shot-design.md, route c). CLI shape mirrors
// cli_text2img.go (isXSubcommand dispatch, hand-rolled flag parse, no
// os.Exit inside the parser so it stays unit-testable).
//
// The --self page HTML is wired here via shot.SelfPageProvider (the SPA const
// lives in cli_web.go; internal/shot cannot import package main).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/shot"
)

// shotUsage is the -h text for the shot subcommand.
const shotUsage = `gopi tool shot — screenshot a URL or gopi's own web chat UI to PNG (for vision debugging)

Drives a local headless Chromium over the raw Chrome DevTools Protocol
(zero Go dependencies; finds chrome via GOPI_SHOT_CHROME, the playwright
browser cache, or PATH). The PNG output feeds the vision loop:
  gopi tool shot --self && gopi tool vision_analyze <out.png>

Usage:
  gopi tool shot --url URL [--out PATH] [flags]
      Screenshot any external page.
  gopi tool shot --self [--addr HOST:PORT] [--out PATH] [flags]
      Screenshot gopi's own web chat UI. With --addr, shoot an already-running
      ` + "`gopi web`" + ` instance; otherwise a temporary static server hosting the
      same embedded page is started for the capture.

Exit status and the control channel:
  A URL that does NOT load is a FAILURE, however Chrome renders it: Chromium
  answers an unreachable address with its own error page, and a PNG of that
  error page is indistinguishable from the requested page on stdout alone
  (that was the defect). So a failed navigation exits NON-ZERO, prints one
  stderr line naming the URL and the error-page fact, and — because the error
  page is still real evidence about the request — writes the PNG and names that
  path on stderr too. A successful capture exits 0 and prints the PNG path on
  stdout, exactly as before.

Flags:
  --out PATH            Output PNG path. Default: shot-<unix>.dump.png in the
                        capture class directory under gopi's scratch tree
                        (<scratch home>/dumps/gopi-captures/) — NEVER the current
                        directory, because a run's cwd is its own git ground and an
                        untracked PNG there breaks Rule 30. The absolute path is
                        printed on stdout.
  --viewport WxH        Capture size. Default %dx%d (tall enough for the chat
                        UI, short enough to dodge vision token truncation).
                        (JS-rendered pages: give the page enough height, e.g.
                        --viewport 1440x2800, or content below the fold is not
                        in the capture at all.)
  --wait SECONDS        Extra seconds after navigation for SPA render.
                        Default %d. 0 skips the extra wait.
  --wait-selector CSS   Poll until document.querySelector(CSS) matches
                        (timeout %s). Combinable with --wait.
  --addr HOST:PORT      With --self: reuse this running gopi web instance.
  --surface NAME        WHAT --self captures: %s.
                        Default %s (unchanged behaviour).
  --cols N / --rows N   With --surface chat: terminal geometry in CELLS.
                        Default %dx%d.
  --display NAME        With --surface screen: X11 display (default $DISPLAY).
  --scrollback          With --surface chat: include the rows scrolled off the
                        top, above a rule line marking where the screen starts.

Chrome launch (the address-space escape):
  Chrome reserves ~51 GiB of VIRTUAL address space at startup while using
  ~200 MiB of RSS. gopi's bash children carry scratch's child clamp
  (ulimit -v 12 GiB, internal/scratch), and the hard limit is inherited, so
  an attached chrome dies instantly with a V8 CHECK failure. When this
  process is clamped, gopi tool shot therefore starts chrome as a transient
  systemd USER unit (systemd-run --user --unit=... --collect), which the user
  manager starts outside the session's limits, and gives that unit a
  MemoryMax=<N>M MemorySwapMax=0 ceiling instead — the honest bound, since
  resident memory is what threatens the host, not address space. Without a
  user manager the launch degrades to an attached one and SAYS SO on stderr.
  Tune the ceiling with %s (MiB, default %d).

Examples:
  gopi tool shot --self                          shoot the embedded chat page
  gopi tool shot --url https://example.com       shoot any page
  gopi tool shot --url http://x --wait-selector "#chat-log"
  gopi tool shot --self --viewport 1440x900 --out web.png
`

// shotFlagMembers lists every flag the shot subcommand accepts.
var shotFlagMembers = map[string]bool{
	"url": true, "self": true, "out": true, "viewport": true,
	"wait": true, "wait-selector": true, "addr": true,
	"surface": true, "cols": true, "rows": true, "display": true,
	"scrollback": true,
}

// shotBooleanFlags are the flags that take no value.
var shotBooleanFlags = map[string]bool{"scrollback": true}

// stripShotBooleanFlags removes the value-less flags from args before the
// shared parser sees them: memoryFlagSet treats every `--flag` as
// value-taking, so a bare `--scrollback` would swallow the next token (or
// report "wants a value"). The removed names are returned as an explicit seen
// set, which keeps the boolean semantics in ONE place.
func stripShotBooleanFlags(args []string) (stripped []string, seen map[string]bool) {
	seen = map[string]bool{}
	for _, a := range args {
		if name, ok := strings.CutPrefix(a, "--"); ok && shotBooleanFlags[name] {
			seen[name] = true
			continue
		}
		stripped = append(stripped, a)
	}
	return stripped, seen
}

// isShotSubcommand reports whether argv begins with the shot subcommand.
func isShotSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbShot {
		runShotSubcommand(args[1:])
		return true
	}
	return false
}

// The shot surface flags' own defaults (Rule 1: named here, referenced below).
// shotDefaultPrefix names a default-named shot's file family inside the capture
// class directory (`internal/scratch.CaptureDumpFile` appends the unix stamp and
// the `.dump.png` suffix). It is the CLONE of the capture faces' own prefix
// constant only because each face owns its family name; the DESTINATION and the
// suffix are the one shared resolver's, never a literal here (Rules 13/14).
const shotDefaultPrefix = "shot-"

const (
	// shotTextColsDefault / shotTextRowsDefault are the terminal geometry a
	// `--surface chat` capture uses when --cols/--rows are unset: wide enough
	// for the chat UI's editor band, tall enough for its status row.
	shotTextColsDefault = shot.TextDefaultCols
	shotTextRowsDefault = shot.TextDefaultRows

	// shotChatExitVerb is what ends the chat TUI after the capture: the slash
	// verb that closes an interactive session (cmd/agent's quit/exit commands).
	// The capture delivers it as TYPED keys — never as one paste burst, whose
	// newline the editor suppresses — see internal/shot's writeTypedKeys.
	shotChatExitVerb = "/" + quitCommandName
)

// chatPromptMark is chatPrompt WITHOUT its trailing blank — the form a capture
// can actually find on the INTERPRETED screen. The renderer trims a row's
// trailing cells and positions the cursor with a CSI instead, so the cell grid
// holds "you>" and never "you> "; waiting for the padded spelling would time out
// on a perfectly healthy UI (the testsuite harness documents the same fact). The
// marker is DERIVED from the prompt constant, so the two cannot drift.
var chatPromptMark = strings.TrimRight(chatPrompt, " ")

// parseShotArgs parses the shot flags into shot.Options without touching the
// process (unit-testable, per the parseText2imgArgs pattern).
func parseShotArgs(args []string) (shot.Options, error) {
	stripped, booleans := stripShotBooleanFlags(args)
	fs := newMemoryFlagSet()
	positional, err := fs.parse(stripped)
	if err != nil {
		return shot.Options{}, err
	}
	for name := range booleans {
		fs.seen[name] = true
	}
	for _, name := range fs.order {
		if !shotFlagMembers[name] {
			return shot.Options{}, fmt.Errorf("unknown flag --%s", name)
		}
	}
	if len(positional) > 0 {
		return shot.Options{}, fmt.Errorf("unexpected positional argument %q (use --url URL or --self)", positional[0])
	}

	opts := shot.Options{
		URL:          strings.TrimSpace(fs.get("url")),
		Self:         fs.seen["self"],
		Out:          strings.TrimSpace(fs.get("out")),
		WaitSelector: strings.TrimSpace(fs.get("wait-selector")),
		Addr:         strings.TrimSpace(fs.get("addr")),
		WaitSeconds:  -1, // engine default (shot.WaitDefault)
	}
	if err := parseShotSurfaceFlags(&opts, fs); err != nil {
		return shot.Options{}, err
	}

	// memoryFlagSet lets a bare flag swallow the next non-flag token as its
	// value (--self extra → self="extra"). --self is boolean: any value is a
	// mis-placed positional.
	if fs.get("self") != "" {
		return opts, fmt.Errorf("unexpected positional argument %q after --self (use --url URL or --self)", fs.get("self"))
	}

	// Exactly one of --url / --self — except on the two surfaces that need
	// neither: a terminal capture runs gopi's own UI and a screen capture grabs
	// the display, so `gopi tool shot --surface chat` stands alone.
	switch {
	case opts.Surface == shot.SurfaceWeb && opts.Self && opts.URL != "":
		return opts, fmt.Errorf("--url and --self are mutually exclusive")
	case opts.Surface == shot.SurfaceWeb && !opts.Self && opts.URL == "":
		return opts, fmt.Errorf("nothing to shoot — pass --url URL or --self")
	}

	if v := fs.get("viewport"); v != "" {
		w, h, err := parseViewport(v)
		if err != nil {
			return opts, err
		}
		opts.ViewportW, opts.ViewportH = w, h
	}
	if v := fs.get("wait"); fs.seen["wait"] {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return opts, fmt.Errorf("--wait needs a non-negative integer, got %q", v)
		}
		opts.WaitSeconds = n
	}
	return opts, nil
}

// parseShotSurfaceFlags validates and attaches the surface selection
// (--surface/--cols/--rows/--display/--scrollback) to opts.
func parseShotSurfaceFlags(opts *shot.Options, fs *memoryFlagSet) error {
	surface, err := shot.NormalizeSurface(fs.get("surface"))
	if err != nil {
		return err
	}
	opts.Surface = surface
	opts.Display = strings.TrimSpace(fs.get("display"))

	// The cell-surface knobs are parsed unconditionally (a value is validated
	// even when another surface is selected, so a typo is reported rather than
	// silently ignored), but they only ACT on --surface chat.
	cols, err := parsePositiveIntFlag(fs, "cols", shotTextColsDefault)
	if err != nil {
		return err
	}
	rows, err := parsePositiveIntFlag(fs, "rows", shotTextRowsDefault)
	if err != nil {
		return err
	}
	opts.Text.Cols, opts.Text.Rows = cols, rows
	opts.Text.Scrollback = fs.seen["scrollback"]
	opts.Text.ExitKeys = shotChatExitVerb
	// The prompt is the readiness marker the capture waits for on the
	// INTERPRETED screen — cmd/agent owns the prompt's text, so the CLI passes
	// it rather than letting internal/shot guess.
	opts.Text.Ready = chatPromptMark
	return validateSurfaceFlagUse(opts, fs)
}

// validateSurfaceFlagUse refuses flags that the selected surface would IGNORE.
// A silently dropped flag is worse than a rejected one: the operator believes
// the capture was configured a certain way (Rule 15). Flags shared by every
// surface (--out, --wait) are always allowed.
func validateSurfaceFlagUse(opts *shot.Options, fs *memoryFlagSet) error {
	if opts.Surface == shot.SurfaceWeb {
		if opts.Display != "" {
			return fmt.Errorf("--display applies to --surface %s, not %s", shot.SurfaceScreen, opts.Surface)
		}
		return nil
	}
	if opts.URL != "" {
		return fmt.Errorf("--url applies to --surface %s, not %s", shot.SurfaceWeb, opts.Surface)
	}
	if opts.Addr != "" {
		return fmt.Errorf("--addr applies to --surface %s, not %s", shot.SurfaceWeb, opts.Surface)
	}
	if opts.Surface == shot.SurfaceScreen {
		for _, name := range []string{"cols", "rows", "scrollback"} {
			if fs.seen[name] {
				return fmt.Errorf("--%s applies to --surface %s, not %s", name, shot.SurfaceChat, opts.Surface)
			}
		}
		return nil
	}
	// --surface chat is the remaining case: --addr/--display already rejected.
	if opts.Display != "" {
		return fmt.Errorf("--display applies to --surface %s, not %s", shot.SurfaceScreen, opts.Surface)
	}
	return nil
}

// parsePositiveIntFlag reads one optional positive-integer flag, falling back to
// def when it was not supplied.
func parsePositiveIntFlag(fs *memoryFlagSet, name string, def int) (int, error) {
	if !fs.seen[name] {
		return def, nil
	}
	v := strings.TrimSpace(fs.get(name))
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("--%s wants a positive integer, got %q", name, v)
	}
	return n, nil
}

// parseViewport splits "WxH" (case-insensitive x) into two positive ints.
func parseViewport(v string) (int, int, error) {
	parts := strings.SplitN(strings.ToLower(v), "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("--viewport wants WxH (e.g. 1440x2800), got %q", v)
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("--viewport wants positive WxH, got %q", v)
	}
	return w, h, nil
}

// shotStartupRenewal reclaims the previous captures' orphaned units at the
// START of a capture. It is the epic's first new trigger (Rule 35: `gopi tool shot`
// is the only thing that creates these units, so every capture is an event that
// necessarily happens on the host where the leak accumulates — no daemon, timer,
// cron line, script or config key is added, and nothing self-schedules).
//
// WHY BEFORE THE CAPTURE AND NOT AFTER: a start-up sweep reclaims what is
// already leaked and cannot be skipped (the defect is a SIGKILLed previous
// capture), while an after-the-fact sweep would depend on this capture
// finishing. It is safe with respect to THIS capture either way — the sweep only
// touches units whose CREATING PROCESS IS MEASURED GONE (internal/scratch
// decides which; a live or unmeasurable creator keeps its unit), and this
// process is alive for the whole run.
//
// Only the WEB surface pays the cost. The text and screen surfaces launch no
// chrome unit at all (the text surface is a CDP session against a browser the
// caller already has; the screen surface reads the X11 framebuffer), so sweeping
// for them would be pure cost with zero possible benefit. The choice is driven
// by the SAME NormalizeSurface the capture itself uses, so a new surface cannot
// silently default to "no sweep".
func shotStartupRenewal(opts shot.Options) {
	surface, err := shot.NormalizeSurface(opts.Surface)
	if err != nil || surface != shot.SurfaceWeb {
		return // nothing on this path starts a unit (or the capture is about to refuse)
	}
	shot.RenewNotice(shot.RenewRoleShotStartup)
}

// runShotSubcommand executes the shot subcommand (the only place that exits
// the process; parse errors print usage).
func runShotSubcommand(args []string) {
	help := len(args) > 0 && (args[0] == "-h" || args[0] == "--help")
	if help {
		fmt.Print(shotUsageText())
		return
	}
	opts, err := parseShotArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool shot: %v\n%s", err, shotUsageHeader())
		os.Exit(1)
	}
	shotStartupRenewal(opts)
	out, err := runShot(opts)
	if err != nil {
		reportShotFailure(err, opts.Out)
		os.Exit(1)
	}
	fmt.Println(out)
}

// shotUsageArgs fills shotUsage's substitution slots positionally. It is the ONE
// place the usage template's argument order lives, so adding a slot changes this
// function and the template together (and the compile-time arity check in
// TestShotUsageArgsMatchTemplate keeps the count honest).
func shotUsageArgs() []any {
	return []any{
		shot.DefaultViewportWidth,     // 1 viewport default width
		shot.DefaultViewportHeight,    // 2 viewport default height
		shot.WaitDefault,              // 3 --wait default
		shot.WaitSelectorTimeout,      // 4 --wait-selector timeout
		shot.SurfacesHelp(),           // 5 --surface accepted values
		shot.DefaultSurface,           // 6 --surface default
		shotTextColsDefault,           // 7 --cols default
		shotTextRowsDefault,           // 8 --rows default
		shot.ChromeMemMaxEnvName,      // 9 chrome ceiling env name
		shot.DefaultChromeMemoryMaxMB, // 10 chrome ceiling default (MiB)
	}
}

// shotUsageText renders the usage template with its defaults.
func shotUsageText() string { return fmt.Sprintf(shotUsage, shotUsageArgs()...) }

// shotUsageHeader renders the flag defaults into the usage text (the short form
// printed after a parse error).
func shotUsageHeader() string { return shotUsageText() }

// reportShotFailure writes a failed capture's report to stderr: the one-line
// reason, and — when the failure IS a navigation that produced Chromium's error
// page — the PNG itself, still saved and named on stderr. The error page is real
// evidence about the request (it names the error code), so a failed navigation
// costs the operator an exit status, never the pixel. Everything else (no chrome,
// a broken CDP session, an unwritable path) has no PNG to name.
//
// Kept separate from runShotSubcommand so the report is testable without
// os.Exit.
func reportShotFailure(err error, outFlag string) {
	var navErr *shot.NavigationFailure
	if errors.As(err, &navErr) && len(navErr.PNG) > 0 {
		if path, werr := writeShotPNG(outFlag, navErr.PNG); werr == nil {
			fmt.Fprintf(os.Stderr, "gopi tool shot: error-page PNG: %s\n", path)
		} else {
			fmt.Fprintf(os.Stderr, "gopi tool shot: %v\n", werr)
		}
	}
	fmt.Fprintf(os.Stderr, "gopi tool shot: %v\n", err)
}

// runShot performs one capture and writes the PNG to disk. It returns the
// absolute output path (printed for the next vision_analyze step).
func runShot(opts shot.Options) (string, error) {
	// Wire the --self page (lives in cli_web.go as an unexported const;
	// internal/shot stays main-free). One-time assignment is fine for a
	// single-shot CLI process.
	if shot.SelfPageProvider == nil {
		shot.SelfPageProvider = func() string { return webChatPage }
	}

	png, err := shot.CaptureSurface(context.Background(), opts)
	if err != nil {
		return "", err
	}
	return writeShotPNG(opts.Out, png)
}

// writeShotPNG writes the captured bytes to opts.Out (the default name when
// unset) and returns the absolute path. ONE writer for both exit paths: a
// successful capture prints the path on stdout, a navigation FAILURE has the
// same path printed on stderr next to the reason, and neither can drift from
// the other's naming.
func writeShotPNG(outFlag string, png []byte) (string, error) {
	out := outFlag
	if out == "" {
		// THE DEFAULT IS NOT THE CALLER'S CWD (measured 2026-11-07, task
		// 01a1b3b1). A bare `shot-<unix>.png` resolved against the process's cwd,
		// and a task run's cwd is its own clone ground — so every shot left an
		// untracked PNG in a git work tree (PROJECT.md Rule 30). The ONE resolver
		// in internal/scratch answers an ABSOLUTE path inside a class directory an
		// existing sweep already walks, so the file cannot land in a checkout and
		// needs no new collector (Rule 35).
		var derr error
		out, derr = scratch.CaptureDumpFile(shotDefaultPrefix)
		if derr != nil {
			return "", fmt.Errorf("shot: %w", derr)
		}
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
	}
	if err := os.WriteFile(out, png, 0o644); err != nil {
		return "", fmt.Errorf("shot: write %s: %w", out, err)
	}
	return abs, nil
}
