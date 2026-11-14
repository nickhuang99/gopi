package main

// voice_convergence_test.go — THE REGRESSION CASE FOR THE CONVERGENCE (task
// 01a121fc, deliverable A, item 3).
//
// WHAT IT PINS, in one sentence: the local engine's two read shapes — the
// console's window arm and the tool's one-block capture — now read ONE capture
// through ONE schedule, so they must hand the engine the SAME audio from the SAME
// window sequence.
//
// WHY IT IS THE CASE THAT MATTERS. Before this change the package answered "where
// does this run's audio end" twice: the console read voiceStagedStream.Next (a
// window plan, a carry, a span ledger and a Drain), and the tool drained the
// source into one buffer with its own byte bound and no ledger at all
// (voiceCapturePCM). The convergence is a claim about code, and this file makes it
// falsifiable: both shapes are driven through their PRODUCTION entry points on one
// fixture, and their audio and boundaries are compared.
//
// THE RED PROOF IS A SHAPE CHANGE, not an import error: hand the file source the
// whole recording in one chunk (or change its chunk geometry) and the window cuts
// move — the span comparison below turns red, because a boundary is decided
// against the arrival of the audio. The measured red/green run is recorded in the
// task report's ## Evidence section.
//
// THE WALL CLOCK IS OUT OF THE COMPARISON, deliberately: a window's bound IS a
// wall-clock budget (a window that reaches it is EXTENDED, which the span
// reports), so two independently paced sources cannot be compared on a schedule.
// Both shapes here run their source UNPACED and are compared on what the seam
// produces; "production really does pace a file source" is pinned by its own case
// (TestVoiceAudioSourceFilePacesDelivery).

import (
	"bytes"
	"os"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// convergenceBoundSeconds is the bound the CONSOLE's shape reads with: the
	// comparison is about two shapes reading one capture the same way, so the one
	// number that must not differ between them is the bound they cut against.
	convergenceBoundSeconds = seamWindowSeconds

	// convergenceWideSeconds is a byte bound WIDER than the fixture, which is how
	// the tool's shape reads a whole recording: `--seconds` is a REQUEST, and a
	// recording shorter than it is read whole rather than refused
	// (TestVoiceCapturePCMReadsOneBoundedBlock pins that too).
	convergenceWideSeconds = seamWindowSeconds * 8
)

// convergenceScratchFile writes the shared fixture into the scratch tree — the
// one place a test may write (testutil.TestDeviceScratchDir; Rule 22/32).
func convergenceScratchFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := testutil.TestDeviceScratchDir(t, "voice-convergence") + "/" + name
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// convergenceRead is ONE shape's reading of one capture: the windows it cut and
// the bytes each window carried to the engine, staged through the PRODUCTION
// staging call (voiceStageCaptureAudio), so the comparison is of the audio the
// engine was handed rather than of a struct this file invented.
type convergenceRead struct {
	spans  []voiceWindowSpan
	audio  [][]byte
	staged [][]byte
	whole  []byte
}

// convergenceReadWindows drives the CONSOLE's shape — a window schedule over an
// open capture, asked for windows until the recording is spent.
func convergenceReadWindows(t *testing.T, src voiceAudioSource, bound, total int) convergenceRead {
	t.Helper()
	stream, err := newVoiceStagedStream(src)
	if err != nil {
		t.Fatalf("open the stream: %v", err)
	}
	defer stream.Close()

	var out convergenceRead
	schedule := newVoiceStagedWindows(stream, bound)
	for i := 0; i < seamMaxWindows; i++ {
		win, err := schedule.Next()
		if err != nil {
			// The window cut AFTER the last sample of a finite recording finds an
			// empty buffer and reports the stream's own end (voiceStagedStream.flush);
			// that is where this loop stops, and only after it has the whole
			// recording.
			if isVoiceStreamEnd(err) && len(out.whole) >= total {
				break
			}
			t.Fatalf("window %d: %v", i+1, err)
		}
		if win.span.End-win.span.Start != len(win.pcm) {
			t.Fatalf("window %d: the span covers %d byte(s) and the window carries %d",
				i+1, win.span.End-win.span.Start, len(win.pcm))
		}
		path, cleanup, serr := voiceStageCaptureAudio(voicePCM16ToWAV(win.pcm))
		if serr != nil {
			t.Fatalf("window %d: stage the audio: %v", i+1, serr)
		}
		raw, rerr := os.ReadFile(path)
		cleanup()
		if rerr != nil {
			t.Fatalf("window %d: read the staged audio: %v", i+1, rerr)
		}
		schedule.Mark(win.span)
		out.spans = append(out.spans, win.span)
		out.audio = append(out.audio, win.pcm)
		out.staged = append(out.staged, raw)
		out.whole = append(out.whole, win.pcm...)
		if len(out.whole) >= total {
			break
		}
	}
	if len(out.whole) != total {
		t.Fatalf("the schedule covered %d of the recording's %d bytes", len(out.whole), total)
	}
	// THE SCHEDULE'S OWN LEDGER MUST TILE THE CAPTURE — the spans ARE the audio,
	// with no hole and no overlap. It is asked of the schedule (not of a copy of the
	// spans) because the schedule is the object both shapes read.
	if err := schedule.Ledger().verify(len(out.whole)); err != nil {
		t.Errorf("the schedule's own ledger does not tile the audio it handed over: %v", err)
	}
	if got := len(schedule.Spans()); got != len(out.spans) {
		t.Errorf("the schedule reports %d span(s) and handed over %d window(s)", got, len(out.spans))
	}
	return out
}

// TestVoiceConvergenceToolAndChatReadTheSameWindows is item 3's case: the tool's
// capture read and the console's window read are the SAME reading of one capture.
func TestVoiceConvergenceToolAndChatReadTheSameWindows(t *testing.T) {
	fixture := seamFixture()
	path := convergenceScratchFile(t, "convergence.pcm", fixture)

	// THE CONSOLE'S SHAPE, through the production schedule.
	chat := convergenceReadWindows(t, voiceFileSourceFor(path, voiceSourceImmediate), convergenceBoundSeconds, len(fixture))
	if len(chat.spans) < 2 {
		t.Fatalf("the fixture produced %d window(s) — the case cannot compare a boundary it never cut", len(chat.spans))
	}

	// THE TOOL'S SHAPE, through ITS OWN production entry point — the function the
	// bare capture and --client both call, over the same audio.
	toolWhole, err := voiceCapturePCM(voiceFileSourceFor(path, voiceSourceImmediate), convergenceWideSeconds)
	if err != nil {
		t.Fatalf("the tool's read of the same recording failed: %v", err)
	}

	// (1) THE AUDIO. The tool's block is the console's windows, concatenated, to
	// the byte: one reading, two shapes.
	if !bytes.Equal(toolWhole, chat.whole) {
		t.Errorf("the tool read %d byte(s) and the console's windows cover %d — the two shapes disagree about the capture",
			len(toolWhole), len(chat.whole))
	}

	// (2) THE BOUNDED READ. With a byte bound the tool stops early, and what it
	// stops with is the HEAD of the very sequence the console's windows tile —
	// which is what makes "the tool's own audio" checkable against the ledger
	// instead of being a second opinion about where audio ends.
	want := voiceBytesPerSeconds(convergenceBoundSeconds)
	toolBounded, err := voiceCapturePCM(voiceFileSourceFor(path, voiceSourceImmediate), convergenceBoundSeconds)
	if err != nil {
		t.Fatalf("the tool's bounded read failed: %v", err)
	}
	if len(toolBounded) != want {
		t.Fatalf("a %ds bound produced %d byte(s), want %d", convergenceBoundSeconds, len(toolBounded), want)
	}
	if !bytes.Equal(toolBounded, chat.whole[:want]) {
		t.Error("the tool's bounded block is not the head of the console's window sequence")
	}

	// (3) THE WINDOWS AND THE ENGINE INPUT. In order, tiling the recording, each
	// window carrying exactly the recording's bytes for its own span, with a staged
	// WAV for each.
	offset := 0
	for i, span := range chat.spans {
		if span.Start != offset {
			t.Errorf("window %d starts at byte %d, want %d — the windows tile the capture in order", i+1, span.Start, offset)
		}
		if want := fixture[span.Start:span.End]; !bytes.Equal(want, chat.audio[i]) {
			t.Errorf("window %d does not carry the recording's own bytes for its span", i+1)
		}
		if len(chat.staged[i]) == 0 {
			t.Errorf("window %d: nothing was staged for the engine", i+1)
		}
		offset = span.End
	}
	if offset != len(fixture) {
		t.Errorf("the windows cover %d of the recording's %d bytes", offset, len(fixture))
	}
}

// TestVoiceConvergenceProcessAndFileSourcesAgreeThroughTheSchedule is the SOURCE
// half of the same claim, through the schedule production now uses: a recording
// fed by a process-shaped source and the same recording fed by the file source cut
// the SAME windows and hand the engine the SAME bytes.
//
// IT IS THE SEAM'S EQUIVALENCE CASE SEEN FROM THE NEW PATH. That case calls
// voiceStagedStream.Next directly; this one goes through voiceStagedWindows, so a
// schedule that quietly re-derived the bound or dropped the carry would be red
// here while the older case stayed green.
func TestVoiceConvergenceProcessAndFileSourcesAgreeThroughTheSchedule(t *testing.T) {
	fixture := seamFixture()
	path := convergenceScratchFile(t, "convergence-sources.pcm", fixture)

	file := convergenceReadWindows(t, voiceFileSourceFor(path, voiceSourceImmediate), convergenceBoundSeconds, len(fixture))
	proc := convergenceReadWindows(t, newSeamProcessSource(fixture), convergenceBoundSeconds, len(fixture))

	if len(file.spans) == 0 || len(file.spans) != len(proc.spans) {
		t.Fatalf("the file source cut %d window(s) and the process source %d — the source changed the boundary",
			len(file.spans), len(proc.spans))
	}
	for i := range file.spans {
		if file.spans[i] != proc.spans[i] {
			t.Errorf("window %d differs by source: file %+v, process %+v", i+1, file.spans[i], proc.spans[i])
		}
		if !bytes.Equal(file.staged[i], proc.staged[i]) {
			t.Errorf("window %d: the engine was handed different audio (%d bytes vs %d)",
				i+1, len(file.staged[i]), len(proc.staged[i]))
		}
	}
}
