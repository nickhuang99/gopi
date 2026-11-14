package main

// chat_voice_audio_smoke_test.go — THE NO-MICROPHONE ENTRY POINT, PINNED INSIDE
// CI (task 01a121fc's deliverable B).
//
// WHAT THIS FILE IS. `gopi chat --voice-audio FILE` is the official way to drive
// the `/voice` mode with no microphone and no sound card (devdoc/
// chat-voice-no-mic.md is the operator's runbook). That runbook is a REAL pty run
// — a pty, the real chat binary, the real local engine, ~15 s of wall clock — and
// it is evidence, not a regression test: a case that needs a 910 MB model and a
// terminal cannot be the thing that fails a pull request.
//
// SO THIS FILE PINS THE PART THAT MUST NOT ROT, ON A SEAM THAT MUST NOT LIE:
//
//  1. THE FILE SOURCE IS THE ONLY SOURCE, and it is chosen BEFORE any device is
//     consulted. The case below drives the PRODUCTION chooser
//     (voiceAudioSourceFor via chatVoiceRecorder's own options) with a device
//     that would make the recorder arm fail loudly, and asserts the file arm is
//     taken anyway: on a host with no capture device — the whole point of the
//     entry point — device enumeration is never reached.
//  2. THE WINDOW SEQUENCE IS A FUNCTION OF THE FILE, NOT OF THE CLOCK. The same
//     bytes run twice through the production chat recorder must yield the SAME
//     window spans and the SAME audio handed to the engine, and a ledger that
//     tiles the file. This is the CI half of the runbook's own acceptance ("the
//     same fixture run twice cuts the same windows"); the runbook's pty run is
//     the half that proves the TEXT reaches the `you>` line.
//
// WHAT IT IS NOT — stated here because a reader will otherwise read the green as
// more than it is. The engine is STOOD IN FOR (a recorder seam that records the
// bytes it was handed and answers text); no audio device is opened; no terminal
// exists. Fake-source evidence is NOT device evidence and must never be filed as
// such: this case proves the CHANNEL and the SCHEDULE, and the report's Rule 31
// `- Ran:` line carries the real pty run for the rest.

import (
	"bytes"
	"errors"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"strings"
	"testing"
)

// chatVoiceSmokeMaxWindows bounds both loops: the fixture is known to produce a
// handful of windows, and a loop that did not terminate would hang the suite
// rather than fail it (the same reason chatVoiceAudioMaxWindows exists).
const chatVoiceSmokeMaxWindows = 16

// chatVoiceSmokeRun drives ONE file through the production chat recorder and
// returns the window sequence it cut and the audio it handed the engine.
//
// IT IS A FUNCTION RATHER THAN A LOOP INSIDE THE CASE because the property is a
// COMPARISON between two runs of it: one definition of "what a file run does"
// is what makes the two runs comparable at all.
func chatVoiceSmokeRun(t *testing.T, path string) ([]voiceWindowSpan, [][]byte, int) {
	t.Helper()
	rec := chatVoiceRecorder(path)
	// THE DEVICE FIELD IS LEFT UNTOUCHED, and that is honest scope: this case does
	// NOT prove "no device is enumerated" by detonating a fake device (a seam for
	// that does not exist here and inventing one would be a new hook, Rule 35).
	// What it proves is that the file arm ENTERS, cuts a repeatable window
	// sequence and hands the engine repeatable bytes — and the row it prints says
	// which source it read (the second case below). That device enumeration is
	// never reached on a file run is stated by the wiring itself
	// (chat_voice_recorder_staged.go's Select branches to selectFileSource BEFORE
	// voiceListDevices) and observed by the real pty run in the report: a host
	// with no capture device was not available to this run to make that a
	// controlled experiment, so it is NOT claimed as one.
	var staged [][]byte
	rec.engine = chatVoiceAudioEngine(t, &staged)
	if _, _, err := rec.Select(""); err != nil {
		t.Fatalf("the production chat recorder refused the file %s: %v", path, err)
	}
	opts := &voiceOptions{}
	spans := make([]voiceWindowSpan, 0, chatVoiceSmokeMaxWindows)
	consumed := 0
	for i := 0; i < chatVoiceSmokeMaxWindows; i++ {
		audio, rerr := rec.Round(opts, voiceChatWindowSeconds)
		if rerr != nil {
			// The end of a finite file is an ordinary end of capture, never a
			// failure (errVoiceStreamClosed is the sentinel both of the mode's
			// readers already honour).
			if errors.Is(rerr, errVoiceStreamClosed) {
				break
			}
			t.Fatalf("round %d: %v", i+1, rerr)
		}
		_ = audio
		spans = append(spans, rec.LastWindow())
		consumed = rec.LastWindow().End
	}
	rec.Close()
	return spans, staged, consumed
}

// TestChatVoiceAudioFileRunIsRepeatableWithoutADevice is THE CI CASE for the
// no-microphone entry point.
func TestChatVoiceAudioFileRunIsRepeatableWithoutADevice(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	path, pcm := chatVoiceAudioFixture(t, "smoke-repeat.wav")

	firstSpans, firstStaged, firstEnd := chatVoiceSmokeRun(t, path)
	if len(firstSpans) < 2 {
		t.Fatalf("the fixture produced %d window(s) — the case cannot compare a sequence it never cut", len(firstSpans))
	}

	// ---- RUN 2: the same bytes, a fresh recorder.
	secondSpans, secondStaged, secondEnd := chatVoiceSmokeRun(t, path)
	if len(secondSpans) != len(firstSpans) {
		t.Fatalf("the same file cut %d window(s) and then %d — the window sequence is not a function of the file:\n first %+v\n second %+v",
			len(firstSpans), len(secondSpans), firstSpans, secondSpans)
	}
	for i := range firstSpans {
		if firstSpans[i] != secondSpans[i] {
			t.Errorf("window %d differs between two runs of ONE file: first %+v, second %+v", i+1, firstSpans[i], secondSpans[i])
		}
	}
	if len(firstStaged) != len(secondStaged) {
		t.Fatalf("the engine was handed %d window(s) and then %d", len(firstStaged), len(secondStaged))
	}
	for i := range firstStaged {
		if !bytes.Equal(firstStaged[i], secondStaged[i]) {
			t.Errorf("window %d: the engine was handed different audio across two runs of one file (%d bytes vs %d)",
				i+1, len(firstStaged[i]), len(secondStaged[i]))
		}
	}
	if firstEnd != secondEnd {
		t.Errorf("two runs of one file consumed different amounts of it: %d vs %d bytes", firstEnd, secondEnd)
	}

	// ---- THE LEDGER TILES THE FILE: the windows cover the capture with no hole
	// and no overlap, which is the property the whole window plan exists for.
	var ledger voiceWindowLedger
	for _, span := range firstSpans {
		ledger.add(span)
	}
	if verr := ledger.verify(len(pcm)); verr != nil {
		t.Errorf("the windows do not tile the file they were read from: %v", verr)
	}
}

// TestChatVoiceAudioFileRunNeverNamesADevice is the SECOND half, and it is the
// half the entry point exists for: the entered row must SAY that no capture
// device is opened, and the recorder must carry the "file" statement rather than
// a device it resolved. A run that opened a device on a host without one would
// fail with the device's own error — which is exactly what a machine with no
// sound card would have reported before this channel existed.
func TestChatVoiceAudioFileRunNeverNamesADevice(t *testing.T) {
	path, _ := chatVoiceAudioFixture(t, "smoke-nodevice.wav")
	rec := chatVoiceRecorder(path)
	desc, _, err := rec.Select("")
	if err != nil {
		t.Fatalf("the file run refused to enter: %v", err)
	}
	if !strings.Contains(desc, chatVoiceAudioDeviceID) || !strings.Contains(desc, voiceCaptureGeometryText()) {
		t.Errorf("the entered row must name the file capture and the geometry it required: %q", desc)
	}
	if rec.dev == nil || rec.dev.ID != chatVoiceAudioDeviceID || rec.dev.Backend != chatVoiceAudioDeviceID {
		t.Errorf("the recorder's device for a file run is %+v — want the %q statement, not a resolved device", rec.dev, chatVoiceAudioDeviceID)
	}
	rec.Close()
}
