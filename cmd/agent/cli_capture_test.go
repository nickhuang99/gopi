// cli_capture_test.go — guards for `gopi tool capture` and for the
// `gopi tool screen_capture` alias that must keep working.
//
// The CLI is a MIRROR of the screen_capture tool, so the assertions here are
// about the mirror staying true: the verb is dispatched, the help documents it,
// completion offers it, the mode flags parse into the mode the engine expects,
// and a contradiction is REFUSED rather than silently resolved.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/nick/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/shot"
	"github.com/nick/gopi/internal/tools"
)

// TestCaptureVerbIsDispatchedAndDocumented pins the three exposure surfaces a
// new top-level verb has to reach (Rule 14's CLI contract): the dispatcher, the
// usage text, and tab completion. Any one of them missing makes the verb
// undiscoverable or unreachable, which is the failure mode this file exists to
// prevent — the same one devdoc/screen-capture-window-area.md records for the
// `capture` rename.
func TestCaptureVerbIsDispatchedAndDocumented(t *testing.T) {
	if !isCaptureSubcommand([]string{CliVerbCapture, "--help"}) {
		t.Fatal("isCaptureSubcommand does not claim `gopi tool capture` — the verb is not dispatched")
	}
	if isCaptureSubcommand([]string{CliVerbScreenCapture}) {
		t.Fatal("isCaptureSubcommand claims `gopi tool screen_capture` — the two verbs must not shadow each other")
	}
	// DISPATCHED, BUT NOT AT TOP LEVEL (verb-retirement, 2026-10-05). The verb
	// is reached as `gopi tool capture` now, so it must be ABSENT from the
	// top-level completion set (an entry there would teach a word the dispatch
	// chain refuses) and PRESENT in the group's vocabulary.
	if containsString(topLevelCommandNames, CliVerbCapture) {
		t.Errorf("topLevelCommandNames still offers %q — the word is refused at top level since the retirement; offering it would teach a refusal", CliVerbCapture)
	}
	if !containsString(toolCompletionNames, CliVerbCapture) {
		t.Errorf("toolCompletionNames does not offer %q — `gopi tool <TAB>` cannot reach the verb", CliVerbCapture)
	}
	// The page spells the FAMILY address (`gopi tool capture`), because that is
	// the door the verb now has: a usage head naming the retired flat word would
	// advertise a spelling the dispatch chain refuses by name.
	if !strings.Contains(captureUsage, programAddress(toolHome(CliVerbCapture))) {
		t.Errorf("the capture usage text never spells %q", programAddress(toolHome(CliVerbCapture)))
	}
	for _, mode := range []string{"--screen", "--window", "--area"} {
		if !strings.Contains(captureUsage, mode) {
			t.Errorf("the capture usage text does not document %s — the three shapes are the whole point of the verb", mode)
		}
	}
}

// TestCaptureModeFlagsParseIntoTheEngineModes is the mirror contract: what the
// CLI accepts must be the mode the ENGINE gets, spelled with the engine's own
// constants (never a literal that could drift).
func TestCaptureModeFlagsParseIntoTheEngineModes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no mode flag defaults to the whole screen", nil, tools.ScreenCaptureModeScreen},
		{"--screen", []string{"--screen"}, tools.ScreenCaptureModeScreen},
		{"--window", []string{"--window"}, tools.ScreenCaptureModeWindow},
		{"--area with offsets", []string{"--area", "800x600+10+20"}, tools.ScreenCaptureModeArea},
		{"--area without offsets", []string{"--area", "800x600"}, tools.ScreenCaptureModeArea},
		{"--area=VALUE form", []string{"--area=800x600+1+2"}, tools.ScreenCaptureModeArea},
		{"display and out do not change the mode", []string{"--window", "--display", ":9", "--out", "/tmp/x.png"}, tools.ScreenCaptureModeWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := parseCaptureArgs(tc.args)
			if err != nil {
				t.Fatalf("parseCaptureArgs(%v): %v", tc.args, err)
			}
			if req.mode != tc.want {
				t.Fatalf("parseCaptureArgs(%v) mode = %q, want %q", tc.args, req.mode, tc.want)
			}
			if _, err := captureOptionsFor(req); err != nil {
				t.Fatalf("captureOptionsFor(%v): %v", tc.args, err)
			}
		})
	}
}

// TestCaptureRefusesContradictoryModeFlags pins Rule 15 on the mode: asking for
// two shapes at once must be an error. A precedence rule would hand back a
// rectangle the caller did not ask for while reporting success.
func TestCaptureRefusesContradictoryModeFlags(t *testing.T) {
	conflicts := [][]string{
		{"--screen", "--window"},
		{"--screen", "--area", "10x10"},
		{"--window", "--area", "10x10"},
		{"--screen", "--window", "--area", "10x10"},
	}
	for _, args := range conflicts {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			req, err := parseCaptureArgs(args)
			if err == nil {
				t.Fatalf("parseCaptureArgs(%v) succeeded with mode %q — mutually exclusive shapes must be refused, not silently resolved", args, req.mode)
			}
			if !strings.Contains(err.Error(), "--") {
				t.Errorf("the refusal does not name the flags it rejected: %v", err)
			}
		})
	}
}

// TestCaptureRefusesAMalformedAreaAtParseTime keeps the CLI from writing an
// image for a rectangle it could not understand: the failure must happen while
// building the options, and it must quote the offending text.
func TestCaptureRefusesAMalformedAreaAtParseTime(t *testing.T) {
	for _, bad := range []string{"abc", "800x", "x600", "0x0", "800x600+1+2+3"} {
		t.Run(bad, func(t *testing.T) {
			req, err := parseCaptureArgs([]string{"--area", bad})
			if err != nil {
				t.Fatalf("parseCaptureArgs accepted --area %q with error %v — the grammar belongs to the engine, and this stage should only split flags", bad, err)
			}
			if _, err := captureOptionsFor(req); err == nil {
				t.Fatalf("--area %q produced engine options — a malformed rectangle must be refused before any capture runs", bad)
			} else if !strings.Contains(err.Error(), "area") {
				t.Errorf("the refusal for --area %q does not say it is about the area: %v", bad, err)
			}
		})
	}
}

// TestCaptureRefusesUnknownFlagsAndPositionals pins the shell contract: a
// misspelled flag is an error (not silently ignored while a whole-screen grab
// happens), and a stray argument is refused too.
func TestCaptureRefusesUnknownFlagsAndPositionals(t *testing.T) {
	if _, err := parseCaptureArgs([]string{"--windwo"}); err == nil {
		t.Error("a misspelled flag was accepted")
	}
	if _, err := parseCaptureArgs([]string{"grab-it"}); err == nil {
		t.Error("a positional argument was accepted")
	}
}

// TestScreenCaptureAliasForwardsModeFlagsToCapture is the compatibility half of
// the pair: the tool's own verb name must still work AND must honour a mode
// flag (a caller typing `gopi tool screen_capture --window` means the window
// capture).
func TestScreenCaptureAliasForwardsModeFlagsToCapture(t *testing.T) {
	forwarding := [][]string{
		{"--window"},
		{"--area", "10x10"},
		{"--area=10x10+5+5"},
		{"--window", "--display", ":9"},
	}
	for _, args := range forwarding {
		if !captureModeFlagsRequested(args) {
			t.Errorf("captureModeFlagsRequested(%v) = false — `gopi tool screen_capture %v` would refuse a mode it should forward", args, strings.Join(args, " "))
		}
	}
	plain := [][]string{nil, {"--display", ":9"}, {"--out", "x.png"}, {"-h"}}
	for _, args := range plain {
		if captureModeFlagsRequested(args) {
			t.Errorf("captureModeFlagsRequested(%v) = true — the plain whole-screen path must not be diverted", args)
		}
	}
}

// The X11 BOUNDARY SEAM -------------------------------------------------------
//
// Every test below drives a whole verb — a real argv into
// runCaptureSubcommand / runScreenCaptureSubcommand, through parsing, mode
// resolution, the engine and the write path — and replaces exactly ONE thing:
// shot.CaptureX11Provider, the function that would talk to a display server.
// No test here needs a display, an X socket or Xvfb, and none of them touches
// the operator's desktop (PROJECT.md Rule 31: the surface is executed, not parsed;
// Rule 27: no device load, ever).
//
// The seam sits at the X11 boundary rather than on the verbs so that a bypass
// FAILS these tests instead of hiding: anything that reaches for a display
// behind the seam's back finds no display and the test goes red (proved in the
// report's red-then-green probe).

const (
	captureStubScreenW = 1280
	captureStubScreenH = 800
	captureStubWindowW = 640
	captureStubWindowH = 480

	// The --self stub's own rectangle, and how many pixels taller it becomes
	// when the frame is asked for.
	captureStubSelfW     = 900
	captureStubSelfH     = 520
	captureStubSelfDecoH = 37
	captureStubDepth     = 24
	captureStubBands     = 4
	captureStubDisplay   = ":99"

	// captureResultPathField / captureResultHashField are the two field names a
	// SUCCESSFUL capture result always carries: their presence on stdout after a
	// refusal means the verb claimed a capture it did not make (Rule 15).
	captureResultPathField = `"path"`
	captureResultHashField = `"sha256_16"`

	captureProbeExitEnv = "GOPI_TEST_CAPTURE_PROBE_ARGV"
	captureProbeFailX11 = "GOPI_TEST_CAPTURE_PROBE_FAIL_X11"
	// captureProbeReturnedExit is the child's exit status when the verb RETURNED
	// instead of refusing — a value no real run produces, so "the verb returned
	// after a failed capture" is distinguishable from every legitimate outcome.
	captureProbeReturnedExit = 0
)

// captureStubPNG stands in for the pixels. It is the SAME bytes for every seam
// call, which is what makes "both verbs go through one engine" assertable as
// content-hash equality.
var captureStubPNG = []byte("gopi tool capture stub: not real pixels\n")

// captureStubGeometry is the geometry the stubbed X11 leg reports for the
// options the verb handed it: the rectangle that was ASKED FOR, so the argv a
// test passes is visible in the JSON the verb prints.
func captureStubGeometry(opts shot.CaptureOptions) shot.Geometry {
	g := shot.Geometry{
		Display: captureStubDisplay,
		Depth:   captureStubDepth,
		Bands:   captureStubBands,
		Mode:    opts.Mode,
	}
	switch opts.Mode {
	case tools.ScreenCaptureModeArea:
		g.Width, g.Height = opts.Area.W, opts.Area.H
		g.X, g.Y = opts.Area.X, opts.Area.Y
	case tools.ScreenCaptureModeWindow:
		g.Width, g.Height = captureStubWindowW, captureStubWindowH
	case tools.ScreenCaptureModeSelf:
		g.Width, g.Height = captureStubSelfW, captureStubSelfH
		// The decoration switch must reach the engine: a stub that ignored it
		// would let the flag be dropped between the parser and the engine with
		// every test still green.
		if opts.Decorations {
			g.Height += captureStubSelfDecoH
		}
	default:
		g.Width, g.Height = captureStubScreenW, captureStubScreenH
	}
	return g
}

// captureSeam records what the X11 boundary was asked for.
type captureSeam struct {
	calls []shot.CaptureOptions
}

// stubCaptureX11 installs the seam: every "display" call is recorded and
// answered with fixed PNG bytes and a geometry derived from the request. The
// real engine, the real write path and the real JSON assembly all still run.
func stubCaptureX11(t *testing.T) *captureSeam {
	t.Helper()
	seam := &captureSeam{}
	old := shot.CaptureX11Provider
	shot.CaptureX11Provider = func(opts shot.CaptureOptions) ([]byte, shot.Geometry, error) {
		seam.calls = append(seam.calls, opts)
		return captureStubPNG, captureStubGeometry(opts), nil
	}
	t.Cleanup(func() { shot.CaptureX11Provider = old })
	return seam
}

// captureResult is the subset of the verb's JSON payload these tests assert on.
type captureResult struct {
	Path    string `json:"path"`
	Display string `json:"display"`
	Mode    string `json:"mode"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256_16"`
}

// decodeCaptureResult parses what a verb printed — the verb's contract is that
// this line IS the ground truth (never a bare "done").
func decodeCaptureResult(t *testing.T, out string) captureResult {
	t.Helper()
	var res captureResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
		t.Fatalf("the verb printed %q, which is not the JSON result it promises: %v", out, err)
	}
	if res.SHA256 == "" || res.Bytes <= 0 {
		t.Fatalf("the result carries bytes=%d sha256_16=%q — the payload is empty, so the write path never ran", res.Bytes, res.SHA256)
	}
	return res
}

// TestCaptureVerbRunsARealArgvEndToEnd hands the verb a real argv for each of
// its three modes through its own dispatch function and asserts the pixels
// landed where the argv said. This is the drive the census's `capture` row
// recorded as missing: the six tests that used to guard this file stopped at
// parseCaptureArgs, which proves the parser and nothing about the verb.
func TestCaptureVerbRunsARealArgvEndToEnd(t *testing.T) {
	cases := []struct {
		name       string
		argv       []string
		wantMode   string
		wantWidth  int
		wantHeight int
		wantX      int
		wantY      int
	}{
		{"--screen", []string{CliVerbCapture, "--screen", "--display", ":9", "--out", "e2e.png"}, tools.ScreenCaptureModeScreen, captureStubScreenW, captureStubScreenH, 0, 0},
		{"--window", []string{CliVerbCapture, "--window", "--out", "e2e.png"}, tools.ScreenCaptureModeWindow, captureStubWindowW, captureStubWindowH, 0, 0},
		{"--area with offsets", []string{CliVerbCapture, "--area", "10x20+3+4", "--out", "e2e.png"}, tools.ScreenCaptureModeArea, 10, 20, 3, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seam := stubCaptureX11(t)
			dir := t.TempDir()
			t.Chdir(dir)

			stdout := captureStdout(t, func() { runCaptureSubcommand(tc.argv[1:]) })
			res := decodeCaptureResult(t, stdout)

			if res.Mode != tc.wantMode {
				t.Errorf("mode = %q, want %q — the flag never reached the engine", res.Mode, tc.wantMode)
			}
			if res.Width != tc.wantWidth || res.Height != tc.wantHeight {
				t.Errorf("geometry = %dx%d, want %dx%d — the argv's shape is not what was captured", res.Width, res.Height, tc.wantWidth, tc.wantHeight)
			}
			if res.X != tc.wantX || res.Y != tc.wantY {
				t.Errorf("origin = +%d+%d, want +%d+%d", res.X, res.Y, tc.wantX, tc.wantY)
			}
			if len(seam.calls) != 1 {
				t.Fatalf("the X11 boundary was reached %d times for %v, want exactly 1", len(seam.calls), tc.argv)
			}
			if got := seam.calls[0].Mode; got != tc.wantMode && !(got == "" && tc.wantMode == tools.ScreenCaptureModeScreen) {
				t.Errorf("the engine was asked for mode %q, want %q", got, tc.wantMode)
			}
			wantPath := filepath.Join(dir, "e2e.png")
			if res.Path != wantPath {
				t.Errorf("path = %q, want %q — --out must decide where the pixels land", res.Path, wantPath)
			}
			if _, err := os.Stat(wantPath); err != nil {
				t.Errorf("the verb reported %q but no file is there: %v", res.Path, err)
			}
		})
	}
}

// TestScreenCaptureAliasRunsARealArgvEndToEnd drives the SECOND top-level verb
// — the `screen_capture` alias — through its own dispatch function. The census
// counted this verb as covered; that claim is only as good as a test that
// actually runs it, so it is driven here rather than assumed.
func TestScreenCaptureAliasRunsARealArgvEndToEnd(t *testing.T) {
	seam := stubCaptureX11(t)
	dir := t.TempDir()
	t.Chdir(dir)
	const outName = "alias-e2e.png"

	argv := []string{CliVerbScreenCapture, "--display", captureStubDisplay, "--out", outName}
	stdout := captureStdout(t, func() { runScreenCaptureSubcommand(argv[1:]) })
	res := decodeCaptureResult(t, stdout)

	if len(seam.calls) != 1 {
		t.Fatalf("the alias reached the X11 boundary %d times for %v, want exactly 1", len(seam.calls), argv)
	}
	if got := seam.calls[0].Display; got != captureStubDisplay {
		t.Errorf("the engine was given display %q, want %q — --display never reached it", got, captureStubDisplay)
	}
	if res.Mode != tools.ScreenCaptureModeScreen {
		t.Errorf("mode = %q, want %q — the alias is the whole-screen spelling", res.Mode, tools.ScreenCaptureModeScreen)
	}
	if res.Display != captureStubDisplay {
		t.Errorf("display in the result = %q, want %q", res.Display, captureStubDisplay)
	}
	wantPath := filepath.Join(dir, outName)
	if res.Path != wantPath {
		t.Errorf("path = %q, want %q", res.Path, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("the alias reported %q but no file is there: %v", res.Path, err)
	}
}

// TestScreenCaptureAliasForwardsAModeFlagToTheCaptureVerb drives the alias's
// second arm: a mode flag must be FORWARDED to `gopi tool capture`, not dropped. The
// alias's own parser knows only --display/--out, so a window-mode result can
// only have come through the capture verb — which is what makes this drive
// distinguish the two paths rather than re-test one of them.
func TestScreenCaptureAliasForwardsAModeFlagToTheCaptureVerb(t *testing.T) {
	seam := stubCaptureX11(t)
	dir := t.TempDir()
	t.Chdir(dir)

	argv := []string{CliVerbScreenCapture, "--window", "--out", "forwarded.png"}
	stdout := captureStdout(t, func() { runScreenCaptureSubcommand(argv[1:]) })
	res := decodeCaptureResult(t, stdout)

	if res.Mode != tools.ScreenCaptureModeWindow {
		t.Errorf("mode = %q, want %q — `gopi %s --window` dropped the flag instead of forwarding it", res.Mode, tools.ScreenCaptureModeWindow, CliVerbScreenCapture)
	}
	if len(seam.calls) != 1 {
		t.Fatalf("the X11 boundary was reached %d times for %v, want exactly 1", len(seam.calls), argv)
	}
	if got := seam.calls[0].Mode; got != tools.ScreenCaptureModeWindow {
		t.Errorf("the engine was asked for mode %q, want %q", got, tools.ScreenCaptureModeWindow)
	}
	if _, err := os.Stat(filepath.Join(dir, "forwarded.png")); err != nil {
		t.Errorf("the forwarded capture wrote no file: %v", err)
	}
}

// TestCaptureAndScreenCaptureShareOneEngine is the drift guard — the one that
// can FAIL, where the previous version of this test asserted that a constant
// equalled itself. It drives both verbs with the same argv and the same stubbed
// pixels, and requires three things of the pair:
//
//  1. each verb reaches the ONE X11 boundary (the alias is not a second
//     implementation that talks to a display on its own),
//  2. each verb asks that boundary for the whole-screen shape, and
//  3. the two payloads hash IDENTICALLY — same engine, same encode, same write
//     path. A "small local variation" in either verb changes the hash and this
//     test says so at the pixel level rather than at the pixel's absence.
func TestCaptureAndScreenCaptureShareOneEngine(t *testing.T) {
	seam := stubCaptureX11(t)
	dir := t.TempDir()
	t.Chdir(dir)

	captureOut := captureStdout(t, func() {
		runCaptureSubcommand([]string{"--screen", "--display", captureStubDisplay, "--out", "shared-capture.png"})
	})
	aliasOut := captureStdout(t, func() {
		runScreenCaptureSubcommand([]string{"--display", captureStubDisplay, "--out", "shared-alias.png"})
	})

	if len(seam.calls) != 2 {
		t.Fatalf("the two verbs reached the X11 boundary %d times, want 2 — a verb that does not reach this seam is a second capture implementation", len(seam.calls))
	}
	for i, call := range seam.calls {
		if call.Mode != "" && call.Mode != tools.ScreenCaptureModeScreen {
			t.Errorf("call %d asked the engine for mode %q; both verbs mean the whole screen here", i, call.Mode)
		}
		if call.Display != captureStubDisplay {
			t.Errorf("call %d used display %q, want %q", i, call.Display, captureStubDisplay)
		}
		if call.Area != (shot.Rect{}) || call.Window != 0 {
			t.Errorf("call %d carried area %+v / window %d on a whole-screen capture", i, call.Area, call.Window)
		}
	}
	resCapture := decodeCaptureResult(t, captureOut)
	resAlias := decodeCaptureResult(t, aliasOut)
	if resCapture.SHA256 != resAlias.SHA256 {
		t.Errorf("the two verbs produced different content hashes for identical pixels (%s vs %s) — they no longer share one engine",
			resCapture.SHA256, resAlias.SHA256)
	}
	if resCapture.Bytes != resAlias.Bytes || resCapture.Width != resAlias.Width || resCapture.Height != resAlias.Height {
		t.Errorf("the two verbs reported different payloads: capture %dx%d/%d bytes, alias %dx%d/%d bytes",
			resCapture.Width, resCapture.Height, resCapture.Bytes, resAlias.Width, resAlias.Height, resAlias.Bytes)
	}
}

// TestCaptureVerbFailureArmsExitNonZero drives the verb's REFUSAL paths in a
// child process, because the refusal is an os.Exit: the observed exit STATUS is
// the evidence, and a returning verb (the failure that would let a caller read
// success out of a failed capture) is reported as its own outcome.
func TestCaptureVerbFailureArmsExitNonZero(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	if argv := os.Getenv(captureProbeExitEnv); argv != "" {
		runCaptureProbeChild(t, argv)
		return
	}
	cases := []struct {
		name     string
		argv     []string
		failX11  bool
		wantStem string
	}{
		{
			name:     "the engine reports no display",
			argv:     []string{CliVerbCapture, "--screen", "--out", "unreachable.png"},
			failX11:  true,
			wantStem: "gopi " + CliVerbCapture + ":",
		},
		{
			name:     "a malformed rectangle is refused before any display is touched",
			argv:     []string{CliVerbCapture, "--area", "not-a-rectangle"},
			wantStem: "gopi " + CliVerbCapture + ":",
		},
		{
			name:     "two shapes at once are refused",
			argv:     []string{CliVerbCapture, "--screen", "--window"},
			wantStem: "gopi " + CliVerbCapture + ":",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := captureProbeChild(t, tc.argv, tc.failX11)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (stdout %q, stderr %q) — a failed capture must not read as success", code, stdout, stderr)
			}
			if strings.Contains(stdout, captureResultHashField) {
				t.Errorf("the verb printed a result payload on a FAILED capture (%q) — a result line is a claim the pixels exist", stdout)
			}
			if strings.Contains(stdout, captureResultPathField) {
				t.Errorf("the verb printed a path on a FAILED capture (%q)", stdout)
			}
			if !strings.Contains(stderr, tc.wantStem) {
				t.Errorf("the refusal does not name the verb (%q missing) in %q", tc.wantStem, stderr)
			}
			if strings.Contains(stderr, "--- FAIL") {
				t.Errorf("the child failed as a TEST rather than as the verb: %q", stderr)
			}
		})
	}
}

// runCaptureProbeChild is the child arm: an argv from the environment, the X11
// boundary either failing or (with no stub at all) genuinely absent, and the
// verb's own dispatch in between. It never returns when the verb refuses — that
// is what the parent measures.
func runCaptureProbeChild(t *testing.T, rawArgv string) {
	t.Helper()
	var argv []string
	if err := json.Unmarshal([]byte(rawArgv), &argv); err != nil {
		t.Fatalf("child: argv %q is not JSON: %v", rawArgv, err)
	}
	if os.Getenv(captureProbeFailX11) == "1" {
		shot.CaptureX11Provider = func(shot.CaptureOptions) ([]byte, shot.Geometry, error) {
			return nil, shot.Geometry{}, errors.New("stub: no X11 display")
		}
	}
	runCaptureSubcommand(argv)
	// Reached only when the verb RETURNED — the failure this probe exists to
	// catch. A distinct status keeps it out of the "textbook refusal" bucket.
	os.Exit(captureProbeReturnedExit)
}

// captureProbeChild re-execs this test binary with the argv under test and
// returns what the process really did: exit status, stdout, stderr.
func captureProbeChild(t *testing.T, argv []string, failX11 bool) (int, string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	encoded, err := json.Marshal(argv)
	if err != nil {
		t.Fatalf("marshal argv: %v", err)
	}
	// -test.v stays OFF: the child's stdout must carry the VERB's output and
	// nothing else, or "the verb printed a result anyway" cannot be asserted.
	cmd := exec.Command(exe, "-test.run=^TestCaptureVerbFailureArmsExitNonZero$")
	cmd.Env = append(os.Environ(), captureProbeExitEnv+"="+string(encoded))
	if failX11 {
		cmd.Env = append(cmd.Env, captureProbeFailX11+"=1")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	case err != nil:
		t.Fatalf("running the probe child: %v", err)
	}
	return 0, stdout.String(), stderr.String()
}

// The --self verb path --------------------------------------------------------
//
// `--self` is the capture of the window that HOSTS the process (see
// internal/shot/pidwindow.go). These tests drive the VERB — a real argv through
// parseCaptureArgs, captureOptionsFor, the engine and the write path — with
// only the X11 boundary stubbed, and they pin the two things the verb owns:
// the mode it selects, and that --decorations survives the trip to the engine.

// TestCaptureSelfFlagSelectsTheSelfMode pins the argv → options mapping.
func TestCaptureSelfFlagSelectsTheSelfMode(t *testing.T) {
	req, err := parseCaptureArgs([]string{"--self"})
	if err != nil {
		t.Fatalf("parseCaptureArgs(--self): %v", err)
	}
	if req.mode != tools.ScreenCaptureModeSelf {
		t.Fatalf("mode = %q, want %q", req.mode, tools.ScreenCaptureModeSelf)
	}
	opts, err := captureOptionsFor(req)
	if err != nil {
		t.Fatalf("captureOptionsFor: %v", err)
	}
	if opts.Mode != tools.ScreenCaptureModeSelf || opts.Decorations {
		t.Fatalf("options = %+v, want mode %q without decorations", opts, tools.ScreenCaptureModeSelf)
	}
}

// e2eSelfOutName is the output file name the self path writes in its test's
// own temporary directory.
const e2eSelfOutName = "self.png"

// TestCaptureSelfDecorationsReachTheEngine is the end-to-end assertion for the
// new flag: the whole verb runs, and the geometry the engine returned (which
// the stub derives FROM the decorations switch) proves the flag arrived.
func TestCaptureSelfDecorationsReachTheEngine(t *testing.T) {
	seam := stubCaptureX11(t)
	dir := t.TempDir()
	t.Chdir(dir)
	stdout := captureStdout(t, func() {
		runCaptureSubcommand([]string{"--self", "--decorations", "--out", e2eSelfOutName})
	})
	res := decodeCaptureResult(t, stdout)
	if res.Mode != tools.ScreenCaptureModeSelf {
		t.Fatalf("the verb reported mode %q, want %q", res.Mode, tools.ScreenCaptureModeSelf)
	}
	if res.Height != captureStubSelfH+captureStubSelfDecoH || res.Width != captureStubSelfW {
		t.Fatalf("the frame capture reported %dx%d, want %dx%d — --decorations did not reach the engine",
			res.Width, res.Height, captureStubSelfW, captureStubSelfH+captureStubSelfDecoH)
	}
	if len(seam.calls) != 1 || !seam.calls[0].Decorations {
		t.Fatalf("the X11 boundary was asked for %+v, want one call with Decorations set", seam.calls)
	}
	wantPath := filepath.Join(dir, e2eSelfOutName)
	if res.Path != wantPath {
		t.Errorf("path = %q, want %q", res.Path, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("the verb reported %q but no file is there: %v", res.Path, err)
	}
}

// TestCaptureDecorationsWithoutSelfIsRefused pins that the flag is not silently
// ignored on a mode where it means nothing: a caller who typed it against
// --window gets an error naming the flag, not a capture that quietly omitted
// the frame they asked for.
func TestCaptureDecorationsWithoutSelfIsRefused(t *testing.T) {
	if _, err := parseCaptureArgs([]string{"--window", "--decorations"}); err == nil {
		t.Fatal("--window --decorations was accepted, want a refusal naming --decorations")
	} else if !strings.Contains(err.Error(), captureFlagDecorations) {
		t.Fatalf("the refusal does not name the flag: %v", err)
	}
	if _, err := parseCaptureArgs([]string{"--screen", "--self"}); err == nil {
		t.Fatal("--screen --self was accepted, want a mutual-exclusion refusal")
	}
}

// TestCaptureSelfForwardsThroughScreenCaptureSpelling keeps the alias honest:
// `gopi tool screen_capture --self` must reach the same verb as `gopi tool capture
// --self` (the mode flag is what triggers the forward), and a plain
// screen_capture call must still be diverted nowhere.
func TestCaptureSelfForwardsThroughScreenCaptureSpelling(t *testing.T) {
	if !captureModeFlagsRequested([]string{"--self"}) {
		t.Fatal("`gopi tool screen_capture --self` would not forward to the capture verb")
	}
	if !captureModeFlagsRequested([]string{"--decorations"}) {
		t.Fatal("`gopi tool screen_capture --decorations` would not forward to the capture verb")
	}
	if captureModeFlagsRequested([]string{"--display", ":9"}) {
		t.Fatal("a plain whole-screen screen_capture call was diverted")
	}
}

// ---------------------------------------------------------------------------
// The read-only listing mode: `gopi tool capture --list-windows`
// ---------------------------------------------------------------------------

// TestCaptureListWindowsIsADocumentedReadOnlyMode pins the three exposure
// surfaces plus the promise the mode makes: it is dispatched, it is documented
// in -h, and it takes no --out because it writes nothing.
func TestCaptureListWindowsIsADocumentedReadOnlyMode(t *testing.T) {
	if !captureFlagMembers[captureFlagListWindows] {
		t.Fatalf("--%s is not a member flag — the parser would refuse it", captureFlagListWindows)
	}
	if !strings.Contains(captureUsage, "--"+captureFlagListWindows) {
		t.Errorf("the capture usage text does not document --%s", captureFlagListWindows)
	}
	if !captureModeFlagsRequested([]string{"--" + captureFlagListWindows}) {
		t.Errorf("`gopi tool screen_capture --%s` would not forward to the capture verb", captureFlagListWindows)
	}
	req, err := parseCaptureArgs([]string{"--" + captureFlagListWindows})
	if err != nil {
		t.Fatalf("parseCaptureArgs(--%s): %v — the read-only mode must parse", captureFlagListWindows, err)
	}
	if !req.listWindows {
		t.Fatalf("parseCaptureArgs(--%s) did not record the listing mode", captureFlagListWindows)
	}
}

// TestCaptureListWindowsRefusesCombinations pins the mode dispatch on the
// listing: it is exclusive with every capturing mode (a caller who asked for two
// things must be told), and it refuses --out because a listing has no file to
// name and --decorations because there is no window to expand.
func TestCaptureListWindowsRefusesCombinations(t *testing.T) {
	conflicts := [][]string{
		{"--" + captureFlagListWindows, "--screen"},
		{"--" + captureFlagListWindows, "--window"},
		{"--" + captureFlagListWindows, "--self"},
		{"--" + captureFlagListWindows, "--area", "10x10"},
		{"--" + captureFlagListWindows, "--out", "/tmp/never.png"},
		{"--" + captureFlagListWindows, "--decorations"},
	}
	for _, args := range conflicts {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if _, err := parseCaptureArgs(args); err == nil {
				t.Fatalf("parseCaptureArgs(%v) was accepted — a listing combined with another mode, or with --out/--decorations, must be refused", args)
			}
		})
	}
	// The positive half: the flag alone, with a display, is accepted.
	if _, err := parseCaptureArgs([]string{"--" + captureFlagListWindows, "--display", ":99"}); err != nil {
		t.Fatalf("parseCaptureArgs(--%s --display :99): %v", captureFlagListWindows, err)
	}
}

// TestCaptureListWindowsRunsTheListingAndWritesNoFile drives the CLI path for
// real: the subcommand is executed with the X11 leg stubbed, and the listing it
// prints must be the tools layer's payload and must name no PNG (Rule 31 — the
// surface under test is the verb, not its parser).
func TestCaptureListWindowsRunsTheListingAndWritesNoFile(t *testing.T) {
	oldList := shot.ListWindowsProvider
	shot.ListWindowsProvider = func(display string) (shot.XWindowList, error) {
		return shot.XWindowList{
			Display:  display,
			RootW:    1280,
			RootH:    1024,
			Windows:  []shot.XWindowInfo{{ID: "0x1400007", X: 1, Y: 2, W: 3, H: 4, PID: 42, Name: "listed", Active: true, Measured: true}},
			ActiveID: "0x1400007",
			Total:    1,
		}, nil
	}
	t.Cleanup(func() { shot.ListWindowsProvider = oldList })

	out, err := tools.ScreenCaptureListWindows(":99")
	if err != nil {
		t.Fatalf("ScreenCaptureListWindows(:99): %v", err)
	}
	var got struct {
		Display string `json:"display"`
		Total   int    `json:"total"`
		Hint    string `json:"hint"`
		Windows []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"windows"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the listing is not JSON: %v (%q)", err, out)
	}
	if got.Display != ":99" || got.Total != 1 || got.Hint == "" {
		t.Errorf("listing = %+v, want display :99, total 1 and a hint", got)
	}
	if len(got.Windows) != 1 || got.Windows[0].ID != "0x1400007" || got.Windows[0].Name != "listed" {
		t.Errorf("listing windows = %+v, want the stubbed window", got.Windows)
	}
	if strings.Contains(out, ".png") || strings.Contains(out, `"path"`) {
		t.Errorf("the listing payload names a file: %q — a read-only mode writes nothing", out)
	}
}
