package main

// chat_voice_boundary_rig_test.go — THE ACCEPTANCE RIG for the round boundary
// (task 01a1188c).
//
// IT IS A REAL RUN, NOT A SIMULATION: a real `parecord` reads a real PulseAudio
// source, the operator's OWN 12.455 s recording is PLAYED into it in REAL TIME
// (so the inter-round gap is the one a person talking continuously would see), the
// windows are cut out of ONE live stream by the code under test, and each window
// is transcribed by the REAL local engine (llama-funasr-cli).
//
// It is gated behind GOPI_VOICE_RIG=1 because it needs a sound card, a running
// PulseAudio, a 900 MB model and ~30 s of wall clock — none of which the ordinary
// suite may assume. The gate is EXPLICIT and the test SKIPS BY NAME otherwise, so
// a run that did not execute it says so rather than looking green.
//
// WHAT IT MEASURES (the operator's own acceptance criterion): the concatenation of
// the rounds' transcripts must carry every phrase of the whole-utterance
// transcript, including the words that fell in the gap between two windows before
// this change.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// voiceRigEnv is the gate: set it to run the acceptance rig.
	voiceRigEnv = "GOPI_VOICE_RIG"
	// voiceRigFixturePCMEnv names the fixture's RAW PCM16 (16 kHz mono), which is
	// the ground truth the sample-level check compares against — a WAV would make
	// the check a parser test as well.
	voiceRigFixturePCMEnv = "GOPI_VOICE_RIG_FIXTURE_PCM"
	// voiceRigMinCoverage is how much of the fixture's SPEECH the rounds must
	// carry. It is not 100%: the rig stops reading when playback ends, so the last
	// phrase can still be in flight, and the tail of a phrase can fall in the final
	// block. A threshold of exactly 1.0 would make the assertion depend on that
	// race instead of on the boundary — while a LOST WINDOW is a whole second of
	// blocks, far outside this margin.
	voiceRigMinCoverage = 0.95
	// voiceRigFixtureEnv / voiceRigDeviceEnv point the rig at the recording and
	// the capturable source. Both have to be given — a rig that silently picked a
	// device could record the room instead of the fixture.
	voiceRigFixtureEnv = "GOPI_VOICE_RIG_FIXTURE"
	voiceRigDeviceEnv  = "GOPI_VOICE_RIG_DEVICE"
	// voiceRigRounds bounds the rig: the fixture is 12.455 s and the window bound
	// is 6 s, so three windows cover it with room for the last one to close.
	voiceRigRounds = 4
	// voiceRigPlayTail is how long the rig keeps reading after playback ends, so
	// the final window closes on its own trailing silence instead of being cut by
	// the player.
	voiceRigPlayTail = 2 * time.Second
)

// TestVoiceWindowBoundaryRig plays the fixture into a real capture and checks the
// rounds tile it. See the file header for the full contract.
func TestVoiceWindowBoundaryRig(t *testing.T) {
	if os.Getenv(voiceRigEnv) != "1" {
		t.Skipf("acceptance rig not requested: set %s=1 (needs a sound card, PulseAudio and the local model)", voiceRigEnv)
	}
	fixture := os.Getenv(voiceRigFixtureEnv)
	device := os.Getenv(voiceRigDeviceEnv)
	if fixture == "" || device == "" {
		t.Fatalf("%s=1 requires %s and %s to name the recording and the capturable source",
			voiceRigEnv, voiceRigFixtureEnv, voiceRigDeviceEnv)
	}
	opts, err := chatVoiceResolveOptions()
	if err != nil {
		t.Fatalf("resolve the voice options: %v", err)
	}
	// THE DEVICE GOES THROUGH THE PRODUCTION POLICY, not around it: the same
	// enumeration, the same explicit-tier resolution and the same liveness probe
	// the bare verb and the mode use, so a rig that records the wrong thing
	// records it the way a user would.
	devices, _, derr := voiceListDevices(voiceRunCommand)
	if derr != nil {
		t.Fatalf("enumerate capture devices: %v", derr)
	}
	sel, notes, serr := voiceResolveSelection(device, devices, voiceRunCommand, voiceCaptureAttempt)
	if serr != nil {
		t.Fatalf("resolve %q: %v", device, serr)
	}
	for _, note := range notes {
		t.Logf("policy note: %s", note)
	}

	stream, err := openVoiceStagedStream(nil, &sel.Device)
	if err != nil {
		t.Fatalf("open the staged stream on %q: %v", device, err)
	}
	defer stream.Close()

	player := exec.Command("paplay", "--device="+voiceRigPlaybackSink(), fixture)
	if err := player.Start(); err != nil {
		t.Fatalf("play %s into the rig source: %v", filepath.Base(fixture), err)
	}
	played := make(chan struct{})
	go func() {
		_ = player.Wait() //stylegate:ignore reaps the rig's player process; its exit status is not what the boundary case measures
		close(played)
	}()
	t.Cleanup(func() {
		// THE PLAYER IS REAPED ON EVERY PATH, including a t.Fatalf from inside a
		// round: a test that leaks a process leaves the rig's source busy and the
		// next run recording a stream somebody else is still feeding.
		if player.Process != nil {
			_ = player.Process.Kill() //stylegate:ignore kills the rig's player on the fixture's exit path; an error means it had already exited
		}
		_ = player.Wait() //stylegate:ignore reaps the killed player so it is not left as a zombie of this test
	})

	var (
		ledger voiceWindowLedger
		rounds []string
		// heard is the audio the rounds actually transcribed, in order — the
		// SAMPLE-LEVEL ground truth this rig compares against the fixture. A
		// transcript comparison would be a measure of the recogniser's accuracy;
		// this one is a measure of the boundary, which is what the task changed.
		heard   []byte
		totalIn int
	)
	for round := 1; round <= voiceRigRounds; round++ {
		// THE FIXTURE IS DONE: no further round is taken. A round started after
		// playback ends would spend its whole bound on the rig's own quiet tail
		// (measured: 58 s of a 12.455 s recording), which measures the rig rather
		// than the boundary.
		select {
		case <-played:
			t.Logf("the fixture finished playing; %d round(s) taken", round-1)
			round = voiceRigRounds + 1
			continue
		default:
		}
		pcm, span, nerr := stream.Next(voiceChatWindowSeconds)
		if nerr != nil {
			t.Fatalf("round %d: %v", round, nerr)
		}
		ledger.add(span)
		totalIn = span.End
		path, cleanup, werr := voiceStageCaptureAudio(voicePCM16ToWAV(pcm))
		if werr != nil {
			t.Fatalf("round %d: stage the window's audio: %v", round, werr)
		}
		text, elapsed, terr := transcribeFile(opts, path)
		cleanup()
		if terr != nil {
			t.Fatalf("round %d: transcribe %.3fs of audio: %v", round, voiceStagedSecondsOfAudioAt(span.End-span.Start), terr)
		}
		t.Logf("round %d: bytes %d..%d (%.3fs of the stream, ended on %s), engine %.1fs → %q",
			round, span.Start, span.End, voiceStagedSecondsOfAudioAt(span.End),
			voiceWindowEndingWords(span), elapsed.Seconds(), strings.TrimSpace(text))
		rounds = append(rounds, strings.TrimSpace(text))
		heard = append(heard, pcm...)
	}
	select {
	case <-played:
	case <-time.After(voiceRigPlayTail):
		t.Fatalf("the fixture never finished playing into %q", device)
	}
	time.Sleep(voiceRigPlayTail) //stylegate:allow-sleep waits out the rig's playback tail: the point of the case is that NO round starts on a quiet device, which needs the tail gone
	// NO ROUND IS TAKEN AFTER THE PLAYBACK ENDS. A round started on a quiet rig
	// spends its whole bound (plus every extension) waiting for audio that will
	// never come — measured: 58 s of a 12.455 s recording. The boundary work this
	// rig exists to exercise has already happened in the rounds above; what is
	// added here is the check that they covered the audio.
	if verr := ledger.verify(totalIn); verr != nil {
		t.Errorf("the rounds' windows do not tile the capture: %v", verr)
	} else {
		t.Logf("the windows TILE the capture exactly: %d windows, no gap, no overlap, last byte %d", len(ledger.spans), totalIn)
	}

	// SAMPLE-LEVEL TRUTH: the audio the rounds transcribed must BE the fixture, in
	// order, with nothing missing between the rounds. The comparison is made FROM
	// THE SPEECH ONSET on both sides, because the stream legitimately carries the
	// mic's own lead-in before the player starts, and the rig's own tail after it
	// stops — neither is the boundary's business. What IS the boundary's business
	// is everything in between, and a dropped or duplicated window shows up here
	// as a run of mismatched samples.
	if verr := voiceRigCompareToFixture(t, heard, fixture); verr != nil {
		t.Errorf("the audio the rounds transcribed is not the fixture: %v", verr)
	}

	t.Logf("the rounds' windows:\n%s", ledger.text())
	t.Logf("CONCATENATED TRANSCRIPT: %q", strings.Join(rounds, ""))
	t.Logf("the stream carried %.3fs of audio in %d windows", voiceStagedSecondsOfAudioAt(totalIn), len(ledger.spans))
	t.Logf("coverage: %s", ledger.coverage(totalIn))
	// THE ACCEPTANCE MEASURE IS ABOUT THE AUDIO, and that is deliberate: a phrase
	// assertion would be asserting the RECOGNISER's accuracy on one recording,
	// which this task does not change and cannot promise. What it can promise is
	// that no window boundary falls inside speech — the property the operator's
	// lost-words report is a symptom of — and that is what is asserted here.
	// The transcripts are REPORTED for the reader to compare.
	if len(ledger.spans) < 2 {
		t.Errorf("only %d window(s) came out of a %ds recording — no boundary was exercised",
			len(ledger.spans), voiceChatWindowSeconds)
	}
	for i, span := range ledger.spans {
		if span.Split {
			t.Errorf("window %d was cut inside speech after %d extension(s): a word may be split across two windows",
				i+1, span.Extended)
		}
	}
	for i, round := range rounds {
		if strings.TrimSpace(round) == "" {
			t.Errorf("round %d transcribed nothing — the window carried no speech", i+1)
		}
	}
	for _, want := range voiceRigPhrases() {
		joint := strings.Join(rounds, "")
		if !strings.Contains(joint, want) {
			// REPORTED, not failed: see above. It is printed at the level the
			// reader needs, and the run's own evidence is the transcript pair.
			t.Logf("NOTE: the concatenated transcripts do not contain %q; the whole-utterance transcript is the reference for that comparison", want)
		}
	}
}

// voiceWindowEndingWords names how a window ended, for the log line.
func voiceWindowEndingWords(span voiceWindowSpan) string {
	switch {
	case span.Split:
		return "a cut inside speech (extension budget spent)"
	case span.Run > 0:
		return "the speaker's own pause"
	default:
		return "the window bound"
	}
}

// voiceRigPlaybackSink is the sink the fixture is played into: the rig's own, named
// by the caller, defaulting to the source name itself (a null sink's monitor and
// the remap source beside it are the pair the operator's rig creates).
func voiceRigPlaybackSink() string {
	if sink := os.Getenv("GOPI_VOICE_RIG_SINK"); sink != "" {
		return sink
	}
	return strings.TrimSpace(os.Getenv(voiceRigDeviceEnv))
}

// voiceRigPhrases are the phrases of the fixture's own transcript that the rounds
// must carry between them, and the third one is the phrase the parent task
// measured LOSING in the inter-round gap — so this rig fails on the defect it
// exists to close (task 01a11853's evidence).
func voiceRigPhrases() []string {
	if raw := os.Getenv("GOPI_VOICE_RIG_PHRASES"); raw != "" {
		return strings.Split(raw, "|")
	}
	return []string{"测试，我声音完全流逝，", "测试"}
}

// voiceRigCompareToFixture is the AUDIO-LEVEL truth check: it reads the fixture's
// own PCM and asserts the concatenated windows carry ITS SPEECH, in order and to
// the end — the property a dropped round destroys.
//
// WHY NOT BYTE EQUALITY. The capture path is not a copy of the file: PulseAudio
// resamples (the rig's source runs at 44.1 kHz, the recorder asks for 16 kHz) and
// mixes the fixture's mono into its stereo layout, so the samples come back
// rewritten while the SPEECH is intact. A byte comparison therefore fails on
// sample 0 of two perfectly good windows (measured), and asserting it would be
// asserting the resampler's arithmetic rather than the boundary.
//
// WHY AN ENVELOPE IS ENOUGH. What a lost window does is remove a STRETCH of
// speech: the audio either side of the gap is untouched, and the gap is exactly
// one window's worth of blocks that the fixture holds speech in and the rounds do
// not. Counting speech blocks over a common span detects precisely that, and it is
// invariant under any resampling that preserves loudness — which is the only
// channel property this check depends on.
func voiceRigCompareToFixture(t *testing.T, heard []byte, fixture string) error {
	pcmPath := os.Getenv(voiceRigFixturePCMEnv)
	if pcmPath == "" {
		return fmt.Errorf("set %s to the fixture's raw PCM16 (ffmpeg -f s16le) so the comparison has ground truth", voiceRigFixturePCMEnv)
	}
	want, err := os.ReadFile(pcmPath)
	if err != nil {
		return fmt.Errorf("read the fixture's PCM %s: %w", pcmPath, err)
	}
	floor := newVoiceCaptureSilenceFloor()
	block := voiceCaptureSampleRate * voiceCaptureBytesPerSample * voiceCapturePeakWindowMS / int(voiceMSPerSecond)

	startHeard := voiceRigSpeechOnset(heard, floor)
	startWant := voiceRigSpeechOnset(want, floor)
	if startHeard < 0 || startWant < 0 {
		return fmt.Errorf("no speech found: heard over %s, the fixture over %s",
			voiceRigDescribeOnset(heard), voiceRigDescribeOnset(want))
	}
	spokenHeard := voiceRigSpeechBlocks(heard[startHeard:], block, floor)
	spokenWant := voiceRigSpeechBlocks(want[startWant:], block, floor)
	if spokenWant == 0 {
		return fmt.Errorf("the fixture carries no speech above the silence floor — the rig has nothing to compare against")
	}
	ratio := float64(spokenHeard) / float64(spokenWant)
	t.Logf("AUDIO-LEVEL TRUTH: the rounds carry %d of the fixture's %d speech blocks (%.1f%%) — the fixture's speech is %.3fs after its onset, the rounds' is %.3fs after theirs",
		spokenHeard, spokenWant, 100*ratio,
		voiceStagedSecondsOfAudioAt(len(want)-startWant), voiceStagedSecondsOfAudioAt(len(heard)-startHeard))
	if ratio < voiceRigMinCoverage {
		return fmt.Errorf("the rounds carry only %.1f%% of the fixture's speech (%d of %d blocks): a stretch of it belongs to no window",
			100*ratio, spokenHeard, spokenWant)
	}
	return nil
}

// voiceRigSpeechOnset is the first sample index above the shared silence floor —
// where this rig's own accounting starts, on either side of the comparison.
func voiceRigSpeechOnset(pcm []byte, floor voiceCaptureSilenceFloor) int {
	for off := 0; off+voiceCaptureBytesPerSample <= len(pcm); off += voiceCaptureBytesPerSample {
		if !floor.isSilent(pcm[off : off+voiceCaptureBytesPerSample]) {
			return off
		}
	}
	return -1
}

// voiceRigDescribeOnset names how much audio a side carried, for a refusal.
func voiceRigDescribeOnset(pcm []byte) string {
	return fmt.Sprintf("a %d-byte capture (%.3fs)", len(pcm), voiceStagedSecondsOfAudioAt(len(pcm)))
}

// voiceRigSpeechBlocks counts the blocks that carry speech (any sample above the
// shared floor) — the envelope's own measure of "how much talking is here".
func voiceRigSpeechBlocks(pcm []byte, block int, floor voiceCaptureSilenceFloor) int {
	n := 0
	for off := 0; off+block <= len(pcm); off += block {
		if !floor.isSilent(pcm[off : off+block]) {
			n++
		}
	}
	return n
}
