package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file drives the `gopi tool gif` VERB with a real (non-help) argv end to end:
// real PNG frames on disk in, a real GIF on disk out, read back through the
// decoder. It is the cmd/agent-layer evidence for the verb, beside the engine
// suite in internal/gifgen — the census counts an argv drive only when it is
// neither `-h` nor a bare constant in a list, so the drive below hands the
// predicate a full argument vector and then runs the verb with it.

// writeGifTestFrame writes a w×h PNG filled with fill and a lighter square, so
// the frame has real content (a flat image would not prove much about a codec).
func writeGifTestFrame(t *testing.T, path string, w, h int, fill color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, fill)
		}
	}
	for y := h / 4; y < h/2; y++ {
		for x := w / 4; x < w/2; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// TestGifVerbRunsARealArgvEndToEnd is the verb's own contract: the dispatch
// predicate claims a real argument vector, the run function accepts it, and the
// file it claims to write is on disk afterwards — decoded back, with the frame
// count, the logical size and the per-frame delay the argv asked for. A verb that
// prints a summary while writing nothing would pass every unit test in the engine
// package and fail here.
func TestGifVerbRunsARealArgvEndToEnd(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.png")
	f2 := filepath.Join(dir, "b.png")
	out := filepath.Join(dir, "film.gif")
	writeGifTestFrame(t, f1, 64, 48, color.RGBA{R: 20, G: 40, B: 80, A: 255})
	writeGifTestFrame(t, f2, 64, 48, color.RGBA{R: 80, G: 20, B: 40, A: 255})

	// A REAL argv: two frames, an output path, a delay and one caption — no -h.
	argv := []string{CliVerbGif,
		"--" + gifFlagFrames, f1 + "," + f2,
		"--" + gifFlagOut, out,
		"--" + gifFlagDelay, "40",
		"--" + gifFlagCaption, f1 + "=first frame",
		"--" + gifFlagJSON,
	}

	if !isGifSubcommand(argv) {
		t.Fatalf("the dispatch predicate does not claim %v — the verb is unreachable", argv)
	}

	stdout := captureStdout(t, func() { runGifSubcommand(argv[1:]) })

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the verb reported success but wrote no file at %s: %v", out, err)
	}
	var doc gifJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &doc); err != nil {
		t.Fatalf("the verb's --json output is not JSON: %v\n%s", err, stdout)
	}
	if doc.Frames != 2 {
		t.Errorf("result says frames=%d, the argv named 2", doc.Frames)
	}
	if doc.DelayCS != 40 {
		t.Errorf("result says delay_cs=%d, the argv named 40", doc.DelayCS)
	}
	if doc.Captioned != 1 {
		t.Errorf("result says captioned=%d, the argv named 1 caption", doc.Captioned)
	}

	// Read the file back through the package's own decoder: the claim is about
	// what LANDED on disk, not about what the run function believed it wrote.
	back, err := readGifForTest(out)
	if err != nil {
		t.Fatalf("read back %s: %v", out, err)
	}
	if back.frames != 2 {
		t.Errorf("the GIF on disk has %d frames, the argv named 2", back.frames)
	}
	if back.width != 64 || back.frameHeight < 48 {
		t.Errorf("the GIF on disk is %dx%d — the frames are 64x48 and the caption band may only ADD height",
			back.width, back.frameHeight)
	}
	if back.delayCS != 40 {
		t.Errorf("the GIF on disk carries delay %d, the argv named 40", back.delayCS)
	}
}

// readGifForTest decodes a GIF far enough to state what the FILE carries.
func readGifForTest(path string) (gifFacts, error) {
	var out gifFacts
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return out, err
	}
	out.frames = len(g.Image)
	out.width = g.Config.Width
	out.frameHeight = g.Config.Height
	if len(g.Delay) > 0 {
		out.delayCS = g.Delay[0]
	}
	return out, nil
}

// gifFacts is what the decoder reports about one file on disk.
type gifFacts struct {
	frames      int
	width       int
	frameHeight int
	delayCS     int
}

// TestGifWarnsOverThePlayableWidthAndStillWritesTheFile is the guard on the
// AFTER-THE-FACT path: `gopi tool gif` composes from frames the caller already
// has, so it cannot shoot narrower — an over-wide product is REPORTED and
// written (Rule 15's "said, never silently smoothed"; the refusal belongs to the
// film arm, which owns its frames).
//
// The three legs are the whole contract, and each is a different failure:
//
//  1. OVER the ceiling  → the warning names the width, the ceiling and the file.
//  2. AT the ceiling    → silence: a warning on the boundary would make the
//     ceiling meaningless (off-by-one in the safe direction).
//  3. EITHER WAY        → the GIF is ON DISK and decodes to the width asked for,
//     because a warning must never become a refusal.
//
// The threshold comes from playableGIFMaxWidth (referenced, never re-typed) —
// so this test keeps passing if the number is ever re-measured, and it fails if
// a second 500 appears beside the constant.
func TestGifWarnsOverThePlayableWidthAndStillWritesTheFile(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name     string
		width    int
		wantWarn bool
	}{
		{"over the playable ceiling", playableGIFMaxWidth + 1, true},
		{"exactly at the playable ceiling", playableGIFMaxWidth, false},
		{"well under the playable ceiling", 494, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".png")
			out := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".gif")
			writeGifTestFrame(t, frame, tc.width, 120, color.RGBA{R: 20, G: 40, B: 80, A: 255})

			argv := []string{CliVerbGif,
				"--" + gifFlagFrames, frame,
				"--" + gifFlagOut, out,
				"--" + gifFlagJSON,
			}
			stdout := captureStdout(t, func() { runGifSubcommand(argv[1:]) })

			// Leg 3 first: the run SUCCEEDED. A missing file means the warning
			// was implemented as a refusal, which is the exact defect this
			// guard exists to catch.
			facts, err := readGifForTest(out)
			if err != nil {
				t.Fatalf("an over-wide-or-not run wrote no decodable GIF at %s: %v", out, err)
			}
			if facts.width != tc.width {
				t.Fatalf("the GIF on disk is %d px wide, the frame was %d — the verb resized the picture", facts.width, tc.width)
			}

			var doc gifJSON
			if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &doc); err != nil {
				t.Fatalf("the verb's --json output is not JSON: %v\n%s", err, stdout)
			}
			joined := strings.Join(doc.Warnings, " | ")

			if !tc.wantWarn {
				if len(doc.Warnings) != 0 {
					t.Fatalf("a %d px wide GIF (ceiling %d) warned: %s", tc.width, playableGIFMaxWidth, joined)
				}
				return
			}
			if len(doc.Warnings) == 0 {
				t.Fatalf("a %d px wide GIF (ceiling %d) in --json mode carried NO warning — the JSON reader is not told the animation may not play",
					tc.width, playableGIFMaxWidth)
			}
			// The warning must be CHECKABLE by its reader: the width, the ceiling
			// and the path are the three numbers that let them act without
			// re-running the verb.
			for _, want := range []string{strconv.Itoa(tc.width), strconv.Itoa(playableGIFMaxWidth), out} {
				if !strings.Contains(joined, want) {
					t.Errorf("the warning does not name %q, so the reader cannot check it: %s", want, joined)
				}
			}
		})
	}
}

// TestGifOverWideWarningReachesStderrInTextMode drives the OTHER half of the
// delivery: the default (non-JSON) invocation is the one a human runs, and a
// warning it never sees is a warning that does not exist. It also re-states that
// the text mode still prints its summary and exits normally — the warning rides
// beside the result, it does not replace it.
func TestGifOverWideWarningReachesStderrInTextMode(t *testing.T) {
	dir := t.TempDir()
	frame := filepath.Join(dir, "wide.png")
	out := filepath.Join(dir, "wide.gif")
	writeGifTestFrame(t, frame, playableGIFMaxWidth+1, 120, color.RGBA{R: 90, G: 20, B: 40, A: 255})

	argv := []string{CliVerbGif, "--" + gifFlagFrames, frame, "--" + gifFlagOut, out}

	var stdout string
	stderr := captureStderr(t, func() { stdout = captureStdout(t, func() { runGifSubcommand(argv[1:]) }) })

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the text-mode run warned but wrote no file at %s: %v", out, err)
	}
	if !strings.Contains(stderr, strconv.Itoa(playableGIFMaxWidth)) {
		t.Fatalf("text mode did not print the ceiling on stderr, so a human running this command is never told: %q", stderr)
	}
	if !strings.Contains(stdout, out) {
		t.Fatalf("text mode printed no summary line naming the file: %q", stdout)
	}
}
