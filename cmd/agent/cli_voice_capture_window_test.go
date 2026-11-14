package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The MINIMUM REPRO of task 01a11853's first defect (Rule 5).
//
// THE BUG. voiceCaptureStreaming reads the recorder's stdout until the window is
// full and then stops it. That stop used to WAIT voiceCaptureCloseGraceSec
// BEFORE signalling — and the constant is already a time.Duration, so
// `time.Duration(voiceCaptureCloseGraceSec) * time.Second` expressed 5e8 seconds
// instead of 500 ms. The wait could therefore never elapse, so every round ran to
// the caller's context deadline: seconds+voiceCaptureGraceSec of wall time for
// `seconds` of audio (measured on zpeer, 2026-10-08: `--seconds 6` → 16.03 s,
// `--seconds 2` → 12.03 s, `--seconds 3` → 13.03 s — task 01a11829).
//
// THE REPRO IS THE CLOCK, not the constant's value: a recorder that streams the
// requested window and never exits on its own must be stopped in about the
// window's own length. `seconds` is deliberately tiny and the bound is
// deliberately far from BOTH ends — the theorem is "the grace is not paid", not a
// latency measurement.
func TestVoiceCaptureWindowCostsTheWindowNotTheGrace(t *testing.T) {
	fakeRecorderDir(t)

	dev := voiceDevice{
		ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + voiceFakeRecorderDevice,
		Backend: voiceDeviceBackendPulse,
		Name:    voiceFakeRecorderDevice,
	}

	const seconds = 1
	start := time.Now()
	pcm, err := voiceCaptureStreaming(dev, seconds)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("capture of %ds failed: %v", seconds, err)
	}

	if want := voiceBytesPerSeconds(seconds); len(pcm) != want {
		t.Errorf("captured %d bytes, want exactly %d — the window is the unit the caller asked for",
			len(pcm), want)
	}

	// THE RED THEOREM, in the assertion: the old shape took
	// seconds+voiceCaptureGraceSec, so anything at or past the grace budget is
	// the defect. The bound leaves room for spawn and for the window itself and
	// is still ~5x below what the defect produced.
	budget := time.Duration(seconds)*time.Second +
		time.Duration(voiceCaptureGraceSec/2)*time.Second
	if elapsed >= budget {
		t.Fatalf("a %ds window took %s (budget %s) — the stop waited for a grace that does not "+
			"belong to the window; the recorder must be stopped as soon as the window is full",
			seconds, elapsed.Round(time.Millisecond), budget)
	}
}

// The other half of the same seam: the recorder's exit path must not be able to
// make a COMPLETED window look like a failure. A recorder stopped by the window
// close reports a signal (parecord has no duration flag and is ended by the
// stop), and that is the NORMAL end — the classification, never the exit code,
// is what decides.
func TestVoiceCaptureTreatsAWindowStopAsANormalExit(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		stderr string
		want   bool
	}{
		{"nil exit", nil, "", false},
		{"killed by the stop signal", errVoiceTest("signal: interrupt"), "", true},
		{"broken pipe when the read side closed", errVoiceTest("exit status 1"), voiceCaptureBrokenPipeMarker, true},
		{"connection failure", errVoiceTest("exit status 1"), voiceCaptureConnectionFailureMarker, true},
		{"a real refusal stays a failure", errVoiceTest("exit status 1"), "audio open error: No such file or directory", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := voiceCaptureContextSensitiveError(tc.err, tc.stderr)
			if got != tc.want {
				t.Errorf("voiceCaptureContextSensitiveError(%v, %q) = %v, want %v",
					tc.err, tc.stderr, got, tc.want)
			}
		})
	}
}

// The delivery-latency half of the same fix, and the reason a window's samples
// were late: parecord's default transfer fragment is large (2 s on this host), so
// the first byte of audio arrived 0.08-2.32 s after the process started and the
// delivery came in 2 s BURSTS — a window closed mid-burst had not been handed the
// tail of its own utterance. The argv now ASKS for a small fragment, and this test
// is the regression guard on that flag (a refactor that drops it restores the
// 2 s burst behaviour silently, because the recorder is still "working").
func TestVoiceRecorderAsksForASmallTransferFragment(t *testing.T) {
	dev := voiceDevice{
		ID:      voiceDeviceBackendPulse + voiceDeviceIDSeparator + voiceFakeRecorderDevice,
		Backend: voiceDeviceBackendPulse,
		Name:    voiceFakeRecorderDevice,
	}
	binary, args := voiceRecorderCommand(dev, voiceClientDefaultSeconds)
	if binary != voiceParecordBinary {
		t.Fatalf("backend %s resolved to %s, want %s", dev.Backend, binary, voiceParecordBinary)
	}
	want := fmt.Sprintf(voiceParecordLatencyFormat, voiceParecordLatencyMs)
	found := false
	for _, a := range args {
		if a == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("parecord argv %q does not carry %q — the capture would fall back to the server's "+
			"default fragment and deliver audio in bursts again", args, want)
	}
}

// voiceFakeRecorderDevice is the --device value the fake recorder is addressed
// with. The script ignores its argv; the value exists so a broken rig's failure
// text names a device a reader can search for.
const voiceFakeRecorderDevice = "fake-window-rig"

// fakeRecorderStreamsForever is the script body of a recorder with parecord's
// shape: no duration flag of its own, streaming PCM16 until its stdout goes away.
// Chunks are paced to real time (320 B per 10 ms = 16 kHz mono PCM16), so the byte
// arithmetic is exact and the test needs no sound card.
const fakeRecorderStreamsForever = `#!/usr/bin/env python3
import os, time
chunk = bytes(320)
for _ in range(6000):
    try:
        if os.write(1, chunk) != len(chunk):
            break
    except OSError:
        break
    time.sleep(0.01)
`

// fakeRecorderDir materialises that script under the name voiceRecorderCommand
// resolves (voiceParecordBinary) and puts its directory on PATH for THIS test only
// (t.Setenv — the flip is scoped to the test, Rule 17).
func fakeRecorderDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, voiceParecordBinary)
	if err := os.WriteFile(path, []byte(fakeRecorderStreamsForever), 0o700); err != nil {
		t.Fatalf("write fake recorder: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// errVoiceTest is the package's tiny comparable error type (declared in
// chat_voice_pty_test.go); the classification cases above read the SHAPE
// production sees (os/exec renders a signalled child as "signal: ..."), so they
// are built from it rather than from a sentinel invented here.
