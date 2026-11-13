package main

// chat_notice_sink_test.go — the destination of this run's driver notices, and
// the property that makes it a fix rather than a preference: a FILMED run's own
// diagnostics must not be paintable into the frames the film is photographing.
//
// The route's own semantics are the mechanism, so they are asserted directly
// (bytes go to the routed writer and to nothing else, and the previous
// destination is restored). The frame-level half of the claim — a delivered film
// carries no such sentence and no white ground — is STRUCTURAL PIXEL EVIDENCE on
// a real artifact (Rules 10.2 / 31), and it lives in
// TestFilmNoticesAreNotPaintedIntoTheDeliveredFrames below.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/replaymode"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionlayout"
)

const noticeSinkTestModel = "fixture-model"

// TestChatNoticeRouteMovesTheNoticesAndRestoresThePreviousOne pins the ONE thing
// the fix is made of: after routeChatNotices(w), every notice goes to w and
// nowhere else, and after the restore it goes back exactly where it was — not to
// a hardcoded os.Stderr (a caller that had already routed keeps its route).
func TestChatNoticeRouteMovesTheNoticesAndRestoresThePreviousOne(t *testing.T) {
	// The previous destination is deliberately NOT os.Stderr: a restore that
	// assumed stderr would pass this test while silently discarding a caller's
	// earlier route.
	outer := &bytes.Buffer{}
	inner := &bytes.Buffer{}
	defer routeChatNotices(outer)() // leave the process's sink as it was found

	chatNoticef("[gopi replay] before the route: %d\n", 1)
	restore := routeChatNotices(inner)
	chatNoticef("[gopi replay] inside the route: %d\n", 2)
	restore()
	chatNoticef("[gopi replay] after the restore: %d\n", 3)

	if got := inner.String(); !strings.Contains(got, "inside the route: 2") {
		t.Fatalf("the routed writer saw %q — a notice written inside the route did not reach it, so the route is not the destination", got)
	}
	if strings.Contains(inner.String(), "before the route") || strings.Contains(inner.String(), "after the restore") {
		t.Fatalf("the routed writer saw a notice written OUTSIDE the route: %q", inner.String())
	}
	if got := outer.String(); !strings.Contains(got, "before the route: 1") || !strings.Contains(got, "after the restore: 3") {
		t.Fatalf("the previous destination saw %q — the restore did not put the notices back where they were", got)
	}
	if strings.Contains(outer.String(), "inside the route") {
		t.Fatalf("the previous destination ALSO saw a notice written inside the route: %q — the route leaves a second copy on the screen, which is the defect it exists to remove", outer.String())
	}
}

// TestChatNoticeRouteRefusesNilWithoutDeletingTheDiagnostics pins the Rule 15
// half: a nil route must not become a silent hole in the record. It is a no-op
// (the previous destination stays) rather than io.Discard.
func TestChatNoticeRouteRefusesNilWithoutDeletingTheDiagnostics(t *testing.T) {
	keep := &bytes.Buffer{}
	defer routeChatNotices(keep)()

	restore := routeChatNotices(nil)
	chatNoticef("[gopi replay] still recorded: %d\n", 7)
	restore()

	if !strings.Contains(keep.String(), "still recorded: 7") {
		t.Fatalf("a nil route swallowed the notice (destination saw %q) — a route to nowhere DELETES the diagnostics instead of moving them", keep.String())
	}
}

// TestChatNoticesToConsoleLogWritesBesideTheSession pins that the filmed run's
// destination is the session's OWN record — the same console.log the agent's
// notices already use — and that the file is created there (an open handle to a
// session log that nothing closes is the leak the close-out would have to guess
// at).
func TestChatNoticesToConsoleLogWritesBesideTheSession(t *testing.T) {
	dir := t.TempDir()
	ag := noticeSinkTestAgent(t, dir)
	sessDir := sessionlayout.RecordContainer(ag.SessionLog.Path())

	path, restore, err := routeChatNoticesToConsoleLog(ag)
	if err != nil {
		t.Fatalf("route the notices: %v", err)
	}
	want := filepath.Join(sessDir, consoleLogFileName)
	if path != want {
		t.Fatalf("the notices were routed to %q, want %q beside the session log", path, want)
	}
	chatNoticef("[gopi replay] a driver notice that must not be photographed\n")
	restore()

	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read %s: %v", path, rerr)
	}
	if !strings.Contains(string(raw), "must not be photographed") {
		t.Fatalf("%s holds %q — the notice was not recorded", path, string(raw))
	}
}

// TestChatNoticesToConsoleLogSaysSoWithNoSessionLog pins the honest-degradation
// arm: a run with no session log reports "" and a no-op restore rather than
// inventing a path or panicking.
func TestChatNoticesToConsoleLogSaysSoWithNoSessionLog(t *testing.T) {
	path, restore, err := routeChatNoticesToConsoleLog(&agent.Agent{})
	if err != nil {
		t.Fatalf("a run with no session log must not be an error: %v", err)
	}
	if path != "" {
		t.Fatalf("a run with no session log reported the destination %q — a path that does not exist is worse than none", path)
	}
	restore()
}

// TestFilmNoticesAreNotPaintedIntoTheDeliveredFrames is the FRAME-LEVEL proof,
// and it is structural pixel evidence rather than a comment (Rules 10.2, 31).
//
// WHY A PIXEL TEST AND NOT A LOG ASSERTION. The defect was never that the
// notices were printed — they must be printed. It was that they were DRAWN: the
// layout owns rows at absolute positions, a bare stderr write lands wherever its
// cursor is, and the film photographs that screen. A test of the log file cannot
// see a pixel; the measurement below can, and it is the one the acceptance names
// (dark ground share, one canvas, no white picture, the declared geometry).
//
// IT IS GATED ON A REAL FILM (Rule 31's bridge between a fixture and a run):
//
//	GOPI_FILM_PIXEL_PROBE=<dir holding frame-NNNN.png> \
//	    go test ./cmd/agent/ -run TestFilmNoticesAreNotPaintedIntoTheDeliveredFrames -v
//
// With no directory named it SKIPS BY NAME — a probe that fell back to a
// committed fixture would report "the film is fine" about a run nobody made.
func TestFilmNoticesAreNotPaintedIntoTheDeliveredFrames(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv(filmPixelProbeEnv))
	if dir == "" {
		t.Skipf("no %s: set it to a film directory (its %s files are measured)", filmPixelProbeEnv, filmFrameGlob)
	}
	frames, err := filepath.Glob(filepath.Join(dir, filmFrameGlob))
	if err != nil {
		t.Fatalf("glob the film %s: %v", dir, err)
	}
	if len(frames) == 0 {
		t.Fatalf("%s holds no %s — a probe that measured nothing cannot witness anything", dir, filmFrameGlob)
	}
	gr, gg, gb, err := filmHexRGB(t, lineedit.GroundBackgroundHex)
	if err != nil {
		t.Fatalf("the surface's declared ground is unreadable: %v", err)
	}
	ground := color.RGBA{R: gr, G: gg, B: gb, A: 0xff}
	white := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	width, height := 0, 0
	sampled, onGround, pureWhite := 0, 0, 0
	for i, p := range frames {
		img, derr := filmProbeDecodePNG(p)
		if derr != nil {
			t.Fatalf("decode %s: %v", p, derr)
		}
		b := img.Bounds()
		// EVERY FRAME IS THE SAME CANVAS. An appended caption band is exactly a
		// frame that is NOT: the picture's geometry would differ from the
		// window's, which is the shape the operator's report measured (a 706x392
		// GIF over 706x342 frames).
		if i == 0 {
			width, height = b.Dx(), b.Dy()
		} else if b.Dx() != width || b.Dy() != height {
			t.Fatalf("%s is %dx%d but frame 0 is %dx%d — the frames are not one canvas, which is the caption band's signature",
				filepath.Base(p), b.Dx(), b.Dy(), width, height)
		}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
				sampled++
				if c == ground {
					onGround++
				}
				if c == white {
					pureWhite++
				}
			}
		}
	}
	share := 1.0
	if sampled > 0 {
		share = float64(onGround) / float64(sampled)
	}
	t.Logf("measured %d frame(s) of %dx%d: %d/%d px on the declared ground %s (%.1f%%), %d pure-white px",
		len(frames), width, height, onGround, sampled, lineedit.GroundBackgroundHex, share*100, pureWhite)
	// THE PROPERTY: a frame of this surface is overwhelmingly its own dark ground.
	// The rejected artifact (an unconfigured white xterm) measured ~0 % on it.
	if share < filmGroundShareFloorNum/float64(filmGroundShareFloorDen) {
		t.Fatalf("only %.1f%% of the film's pixels are the declared ground — this is a film of a window that never got the surface's ground (the operator's rejected artifact measured ~0%%)", share*100)
	}
	if pureWhite*filmWhiteShareFloorDen > sampled {
		t.Fatalf("%d of %d px are pure white — the frames are a white picture, which is what the operator rejected", pureWhite, sampled)
	}
	// The declared geometry: the film forces the producer to the chat surface's
	// own ideal cell count, and a canvas drawn at another width is a different
	// picture (chat_geometry.go).
	if width <= 0 || height <= 0 {
		t.Fatalf("the canvas measured %dx%d — no frame carried a picture", width, height)
	}
}

const (
	// filmPixelProbeEnv names the delivered film directory this probe measures.
	filmPixelProbeEnv = "GOPI_FILM_PIXEL_PROBE"
	// filmFrameGlob is the frame name the driver writes (`frame-0001.png`), so
	// the probe and the driver cannot disagree about what a film directory holds.
	filmFrameGlob = "frame-*.png"
	// filmGroundShareFloorNum / Den are the floor, as a fraction so the comparison
	// is exact: half the canvas. The two measured populations are ~88 % (the real
	// film, `/tmp/filmfix-run/replay.gif`) and ~0 % (the rejected artifact) —
	// internal/film/ground.go owns the criterion and its reasoning; this is the
	// probe's own copy of the SAME number, and it is a copy on purpose (a test may
	// not import the criterion's unexported floor, and a probe that read the
	// shipped constant could not witness a regression in it).
	filmGroundShareFloorNum = 1
	filmGroundShareFloorDen = 2
	// filmWhiteShareFloorDen bounds the pure-white share at 1/Den of the canvas.
	filmWhiteShareFloorDen = 8
)

// filmHexRGB parses `#rrggbb` into three channels, failing the test on anything
// else (a malformed ground must not silently become black, which would make every
// frame look "white" by comparison).
func filmHexRGB(t *testing.T, hex string) (uint8, uint8, uint8, error) {
	t.Helper()
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) != 6 {
		return 0, 0, 0, fmt.Errorf("%q is not a 6-digit hex colour", hex)
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%q does not parse: %w", hex, err)
	}
	return uint8(n >> 16), uint8(n >> 8), uint8(n), nil
}

// noticeSinkTestAgent builds the smallest agent this file's route needs: a real
// session logger in dir (its Path() is what the route derives console.log from).
func noticeSinkTestAgent(t *testing.T, dir string) *agent.Agent {
	t.Helper()
	t.Setenv("GOPI_HOME", dir)
	sl, err := agent.NewSessionLogger("notice-sink-probe", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened
	return &agent.Agent{SessionLog: sl, Logger: log.New(&bytes.Buffer{}, "", 0)}
}

// filmProbeDecodePNG decodes one frame, failing the probe rather than skipping it.
func filmProbeDecodePNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// TestReplayNoticesAreRoutedByTheReplayCriterionNotByTheFilm pins the 2026-11-04
// correction, and it is the minimal repro of the defect it closes.
//
// THE DEFECT. The sink's only route was installed inside the FILM branch of the
// REPL, so the criterion in force was "this process is a film's producer". A
// plain `gopi chat --replay <id>` therefore kept os.Stderr and printed the whole
// `[gopi replay] …` family — the session it re-runs, the stub the model is bound
// to, the injection budget, the refusal catalogue — onto the operator's screen,
// which is a picture of SOMEBODY ELSE'S session (measured: 15 lines).
//
// THE FIX, and what this case asserts: the criterion is "this process is a
// REPLAY" (internal/replaymode.Active — the package that already answers exactly
// that question, Rule 13), so the route can be installed for a replay that films
// nothing, and BOTH writers reach it — cmd/agent's chatNoticef and
// internal/agent's replaymode.Noticef, which is where the session-mint and
// raw-log refusals are spoken.
//
// It also asserts the Rule 15 half: the destination is a FILE whose bytes are
// greppable, not io.Discard and not a silent fallback — the diagnostics are
// MOVED, never deleted.
func TestReplayNoticesAreRoutedByTheReplayCriterionNotByTheFilm(t *testing.T) {
	replaymode.Arm("/a/recording/this/case/does/not/need/on/disk.jsonl")
	t.Cleanup(replaymode.Disarm)

	path, restore, err := routeChatNoticesForReplay()
	if err != nil {
		t.Fatalf("route a replay's driver notices: %v", err)
	}
	if path == "" {
		t.Fatal("a replay's route answered NO destination — the notices would stay on the screen that draws the recording, which is the defect")
	}
	// The destination must be inside the scratch tree and OUTSIDE the recording:
	// a replay writes nothing into the session it re-runs (Rule 22 + the epic's
	// read-only contract), and the scratch tree is what `gopi tool scratch-gc`
	// already sweeps (Rule 35: no new artifact class).
	if !scratch.IsInScratchTree(path) {
		t.Fatalf("the replay's notice log is %s, which is outside the scratch tree — it would never be collected", path)
	}
	if strings.Contains(path, "/sessions/") {
		t.Fatalf("the replay's notice log is %s — inside a sessions tree, so a replay would write into a recording's own directory", path)
	}

	// Both spellings the production code uses, because they are two packages'
	// entry points to the ONE sink.
	chatNoticef("[gopi replay] spoken by cmd/agent\n")
	replaymode.Noticef("[gopi replay] spoken by internal/agent\n")
	restore()

	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read %s: %v", path, rerr)
	}
	for _, want := range []string{"spoken by cmd/agent", "spoken by internal/agent"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s holds %q — the notice %q was not recorded, so it was dropped rather than moved (Rule 15)", path, string(raw), want)
		}
	}
}

// TestReplayNoticeRouteLeavesANonReplayOnStderr pins that the correction did not
// move a LIVE chat's diagnostics: with no replay armed, the sink is os.Stderr —
// the historic destination an operator watching a live session reads.
func TestReplayNoticeRouteLeavesANonReplayOnStderr(t *testing.T) {
	if replaymode.Active() {
		t.Fatal("the replay flag is armed outside this case's control — its premise fails")
	}
	if got := chatNoticeWriter(); got != io.Writer(os.Stderr) {
		t.Fatalf("a non-replay run's notices go to %T, not os.Stderr — the fix widened into a live chat's screen", got)
	}
}
