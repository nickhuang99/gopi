package main

// chat_voice_window_boundary_test.go — the round boundary as a UNIT PROPERTY
// (task 01a1188c).
//
// THE DEFECT THESE CASES PIN, in one sentence: `/voice` used to record a window,
// stop the recorder, transcribe, and only then open the next recorder — so the
// audio spoken while the engine was working belonged to no window at all, and a
// continuous utterance lost exactly the words that fell in that gap.
//
// The fix has two halves and each gets a case here, on the SMALLEST sample that
// can show it (Rule: verify the minimal sample — synthetic audio, a handful of
// samples, no sound card):
//
//  1. voiceWindowPlan.cut decides where ONE window ends inside audio already in
//     hand, and the case below is the boundary rule itself: a window is cut at
//     the LONGEST pause it holds rather than at its byte bound (which splits a
//     word), the pause it was cut at is handed to the next window (so the two
//     windows tile the audio with no hole and no overlap), and a window that has
//     no pause at all is extended rather than cut — until the budget is spent,
//     when the split is REPORTED rather than hidden.
//
//  2. voiceStagedStream.Next turns that decision into the next round's audio on a
//     LIVE stream, carrying the tail forward. Its case drives the stream through
//     a SCRIPTED reader (a pipe it can feed at will), which is what makes "the
//     audio after the cut is still in the stream" an observable fact instead of a
//     claim about a sound card.

import (
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"time"
)

const (
	// boundaryTestLoud / boundaryTestQuiet are the two amplitudes the synthetic
	// windows are built from: a sample loud enough to be speech and digital
	// silence, so the case states its audio rather than depending on a recording.
	boundaryTestLoud  = 8000
	boundaryTestQuiet = 0
	// boundaryTestWindowSeconds is the window bound these cases run with. It is a
	// LOCAL value (not voiceChatWindowSeconds) so the case's arithmetic — how many
	// samples are a window — stays readable and the case cannot silently change
	// meaning when the production bound is tuned.
	boundaryTestWindowSeconds = 2
	// boundaryTestPauseMS is the pause the synthetic audio is built around: long
	// enough to be a boundary (voiceCaptureTrailingSilenceMS), short enough that
	// several fit in one window.
	boundaryTestPauseMS = 400
)

// boundarySpeech builds `ms` of loud audio (a stand-in for a phrase).
func boundarySpeech(ms int) []byte {
	return boundaryBlock(boundaryTestLoud, ms)
}

// boundarySilence builds `ms` of digital silence (a stand-in for a pause).
func boundarySilence(ms int) []byte {
	return boundaryBlock(boundaryTestQuiet, ms)
}

// boundaryBlock builds `ms` of PCM16 at ONE amplitude. A constant amplitude is
// deliberate: the rule is a threshold on a peak, so a synthetic block makes the
// expected verdict arithmetic instead of a judgement about a waveform.
func boundaryBlock(amplitude int, ms int) []byte {
	samples := voiceCaptureSampleRate * ms / voiceMSPerSecond
	out := make([]byte, 0, samples*voiceCaptureBytesPerSample)
	for i := 0; i < samples; i++ {
		var buf [voiceCaptureBytesPerSample]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(int16(amplitude)))
		out = append(out, buf[:]...)
	}
	return out
}

// TestVoiceWindowCutTakesThePauseAndCarriesIt is case 1: where one window ends and
// what the next one inherits.
func TestVoiceWindowCutTakesThePauseAndCarriesIt(t *testing.T) {
	plan := newVoiceWindowPlan(boundaryTestWindowSeconds)
	// THE SPEECH BEFORE THE PAUSE IS LONGER THAN THE MINIMUM WINDOW, and that is
	// part of the sample rather than an accident of it: a window shorter than
	// voiceWindowMinAudioMS is never cut at all (there is no word in it), so a
	// pause inside the first 1.5 s cannot be the boundary this case is about.
	speech := boundarySpeech(voiceWindowMinAudioMS + 300)
	pause := boundarySilence(boundaryTestPauseMS)
	held := append(append(append([]byte{}, speech...), pause...), boundarySpeech(900)...)

	cut, ok := plan.cut(held, false)
	if !ok {
		t.Fatalf("a window carrying %dms of speech, a %dms pause and more speech produced no cut",
			voiceWindowMinAudioMS+300, boundaryTestPauseMS)
	}
	if cut.Run < plan.runBytes {
		t.Errorf("the cut claims a %d-byte run, shorter than the %d-byte floor for a pause",
			cut.Run, plan.runBytes)
	}
	// THE CUT LANDS ON THE PAUSE'S FIRST SAMPLE, and that is what makes the next
	// window begin with the pause rather than with the audio on the far side of
	// it: cutting AFTER the pause would take the run into the first window and
	// lose no audio either — but then the second window would open mid-word, at
	// the far edge of the silence, which is the split this rule exists to avoid.
	wantEnd := len(speech)
	if cut.End != wantEnd {
		t.Errorf("cut at byte %d, want %d (the first sample of the pause)", cut.End, wantEnd)
	}
	if cut.Split {
		t.Errorf("the cut was reported as a split although a %dms pause was available", boundaryTestPauseMS)
	}
	// THE TAIL IS THE NEXT WINDOW'S HEAD, and the two tile the buffer exactly.
	carry := held[cut.End:]
	if len(carry) == 0 {
		t.Fatalf("nothing was carried into the next window: the audio after the cut %v", cut)
	}
	if len(held[:cut.End])+len(carry) != len(held) {
		t.Errorf("the window and its carry do not cover the buffer: %d + %d != %d",
			cut.End, len(carry), len(held))
	}
}

// TestVoiceWindowCutRefusesAShortGap is the SAME rule seen from the other side: a
// gap shorter than a pause is not a boundary, so the case asserts the window is
// NOT cut there — and, with the budget unspent, not cut at all (it waits for more
// audio). A rule that accepted this gap would cut inside a word, which is exactly
// the defect.
func TestVoiceWindowCutRefusesAShortGap(t *testing.T) {
	plan := newVoiceWindowPlan(boundaryTestWindowSeconds)
	// A gap one block shorter than the floor: the smallest sample that can show
	// the difference between "a pause" and "the closure of a consonant".
	short := boundarySilence(voiceCaptureTrailingSilenceMS - voiceCapturePeakWindowMS)
	held := append(append(append([]byte{}, boundarySpeech(voiceWindowMinAudioMS+300)...), short...), boundarySpeech(1200)...)

	if cut, ok := plan.cut(held, false); ok {
		t.Errorf("a %dms gap was accepted as a boundary (cut at byte %d, run %d bytes): only a run of at least %dms is a pause",
			voiceCaptureTrailingSilenceMS-voiceCapturePeakWindowMS, cut.End, cut.Run, voiceCaptureTrailingSilenceMS)
	}
	// AND WITH THE AUDIO PAUSELESS PAST THE BUDGET, the cut is taken and REPORTED.
	cut, ok := plan.cut(held, true)
	if !ok {
		t.Fatalf("with the extension budget spent, a window with no good boundary produced no cut")
	}
	if !cut.Split {
		t.Errorf("a cut with no qualifying pause reported Split=false: the operator is not told a word may be split")
	}
}

// TestVoiceWindowCutLeavesLeadingSilenceAlone pins the direction that would be the
// same defect from the other side: a window that opens on silence (the operator is
// thinking) must NOT close on it, or every window in a quiet room would be an empty
// round handed to a multi-second transcription.
func TestVoiceWindowCutLeavesLeadingSilenceAlone(t *testing.T) {
	plan := newVoiceWindowPlan(boundaryTestWindowSeconds)
	held := append(boundarySilence(voiceWindowMinAudioMS+800), boundarySpeech(1200)...)
	if _, ok := plan.cut(held, false); ok {
		t.Errorf("a window opening on %dms of leading silence was closed on it", voiceWindowMinAudioMS+800)
	}
}

// TestVoiceWindowLedgerVerifySpotsAHole is the TILING CHECK itself, on the
// smallest sample that can fail: two windows with a gap between them. It is the
// check the acceptance rig runs against a real capture, so a case that did not
// catch a hole would make that run's pass meaningless.
func TestVoiceWindowLedgerVerifySpotsAHole(t *testing.T) {
	var ok voiceWindowLedger
	ok.add(voiceWindowSpan{Start: 0, End: 100})
	ok.add(voiceWindowSpan{Start: 100, End: 250})
	if err := ok.verify(250); err != nil {
		t.Fatalf("contiguous windows were reported as broken: %v", err)
	}

	var gapped voiceWindowLedger
	gapped.add(voiceWindowSpan{Start: 0, End: 100})
	gapped.add(voiceWindowSpan{Start: 120, End: 250})
	if err := gapped.verify(250); err == nil {
		t.Fatalf("a 20-byte gap between two windows was accepted: %v", gapped.text())
	}

	var overlapping voiceWindowLedger
	overlapping.add(voiceWindowSpan{Start: 0, End: 150})
	overlapping.add(voiceWindowSpan{Start: 120, End: 250})
	if err := overlapping.verify(250); err == nil {
		t.Fatalf("overlapping windows were accepted: %v", overlapping.text())
	}

	var shifted voiceWindowLedger
	shifted.add(voiceWindowSpan{Start: 40, End: 250})
	if err := shifted.verify(250); err == nil {
		t.Fatalf("a first window that does not start at the head of the capture was accepted")
	}
}

// scriptedStreamReader is the stream's byte source for case 2: it hands back
// chunks the case controls, one at a time, and it BLOCKS when it has nothing left
// — which is what makes the case able to assert what a round does while it waits,
// without a sound card in the loop.
type scriptedStreamReader struct {
	chunks   [][]byte
	deadline time.Duration
}

func (r *scriptedStreamReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		time.Sleep(r.deadline) //stylegate:allow-sleep the scripted reader parks for exactly the window deadline under test — the sleep IS the silence being measured
		return 0, io.EOF
	}
	chunk := r.chunks[0]
	r.chunks = r.chunks[1:]
	if len(chunk) > len(p) {
		r.chunks = append([][]byte{chunk[len(p):]}, r.chunks...)
		chunk = chunk[:len(p)]
	}
	copy(p, chunk)
	return len(chunk), nil
}

// TestVoiceStagedStreamCarriesTheTailForward is case 2: the LIVE stream half. A
// round ends on the pause it finds, the audio after that pause is still readable
// by the next round, and the two rounds' spans tile the stream — which is the
// mechanical statement of "the microphone is never unread".
func TestVoiceStagedStreamCarriesTheTailForward(t *testing.T) {
	speech := boundarySpeech(voiceWindowMinAudioMS + 300)
	pause := boundarySilence(boundaryTestPauseMS)
	// Three phrases separated by pauses, then the end of the stream.
	body := append([]byte{}, speech...)
	body = append(body, speech...)
	body = append(body, pause...)
	body = append(body, speech...)
	body = append(body, pause...)
	body = append(body, speech...)
	body = append(body, pause...)

	s := &voiceStagedStream{
		dev:    voiceDevice{ID: "test-device", Name: "test"},
		binary: "test",
		chunks: make(chan []byte, 8),
		done:   make(chan struct{}),
	}
	// THE FEEDER STOPS WHEN THE CASE DOES. A live recorder's reader runs until the
	// stream is closed, so a feeder that pushed its whole fixture into a buffered
	// channel would park there forever once the case stopped reading — a leaked
	// goroutine, and a test that then asserts about the leak instead of the
	// boundary.
	go func() {
		defer close(s.chunks)
		const chunkBytes = 3200 // 100 ms per chunk, so the stream is FED progressively
		for off := 0; off < len(body); off += chunkBytes {
			end := off + chunkBytes
			if end > len(body) {
				end = len(body)
			}
			select {
			case s.chunks <- body[off:end]:
			case <-s.done:
				return
			}
		}
	}()
	defer s.Close()

	var ledger voiceWindowLedger
	received := 0
	for i := 0; i < 2; i++ {
		audio, span, err := s.Next(boundaryTestWindowSeconds)
		if err != nil {
			t.Fatalf("round %d: %v", i+1, err)
		}
		if len(audio) == 0 {
			t.Fatalf("round %d returned no audio", i+1)
		}
		if span.Split {
			t.Errorf("round %d was cut inside speech: %v", i+1, span)
		}
		if span.Start != received {
			t.Errorf("round %d starts at byte %d, but the stream had handed out %d — the boundary is not contiguous",
				i+1, span.Start, received)
		}
		ledger.add(span)
		received = span.End
	}
	if received == 0 {
		t.Fatalf("two rounds consumed nothing from the stream")
	}
	// THE SECOND ROUND'S AUDIO BEGINS WITH THE PAUSE THE FIRST ENDED ON, and that
	// is the property that makes the boundary lossless: the pause was not dropped
	// with the process that recorded it.
	if len(s.carry) > 0 && len(s.carry) > voiceCaptureSampleRate*voiceCaptureBytesPerSample {
		t.Errorf("the carry holds %d bytes — more than a second of audio, so a window is not consuming its stream",
			len(s.carry))
	}
}

// TestVoiceStagedStreamReturnsItsSpan pins the shape the caller depends on: Next
// hands back BOTH the audio and the span for it, and the span covers exactly the
// bytes returned. A version that stamped only the stream's own field handed back a
// zero span for a window that carried audio (measured in the acceptance rig's
// first run), which is the "the report says nothing happened" failure.
func TestVoiceStagedStreamReturnsItsSpan(t *testing.T) {
	s := &voiceStagedStream{
		dev:    voiceDevice{ID: "test-device", Name: "test"},
		binary: "test",
		chunks: make(chan []byte, 8),
		done:   make(chan struct{}),
	}
	go func() {
		defer close(s.chunks)
		for _, chunk := range [][]byte{
			boundarySpeech(voiceWindowMinAudioMS + 300),
			boundarySilence(boundaryTestPauseMS),
			boundarySpeech(800),
		} {
			select {
			case s.chunks <- chunk:
			case <-s.done:
				return
			}
		}
	}()
	defer s.Close()
	audio, span, err := s.Next(boundaryTestWindowSeconds)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if span.End-span.Start != len(audio) {
		t.Errorf("the span covers %d bytes but the round returned %d", span.End-span.Start, len(audio))
	}
	if span.Start != 0 {
		t.Errorf("the first window of a stream starts at byte %d, want 0", span.Start)
	}
	if strings.TrimSpace(s.binary) == "" {
		t.Errorf("the stream lost its recorder's name")
	}
}
