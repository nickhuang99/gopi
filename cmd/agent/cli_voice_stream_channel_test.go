package main

// cli_voice_stream_channel_test.go — THE BUILDER GETS ITS CHANNEL FROM THE OPEN
// SOURCE (task 01a1215c).
//
// WHAT IT PINS, in one sentence: newVoiceStagedStream must select on the channel
// the source's READER writes to, for a source whose channel only exists after
// Open — which is the production one.
//
// THE DEFECT THIS CASE EXISTS FOR, measured on a real sound card (host-b,
// 2026-10-10, task 01a1215c): newVoiceStagedStream read `source.Chunks()` BEFORE
// calling `source.Open()`. voiceStagedRecorderSource assigns its `chunks` field
// INSIDE Open (`s.chunks = make(chan []byte, voiceStagedQueueChunks)`, because the
// channel is the reader goroutine's hand-off and there is no reader before the
// recorder process exists), so the pre-Open read captured the field's ZERO VALUE
// — nil. The builder then spent the whole `/voice` mode in a select on a nil
// channel, which never fires: the probe measured `builder.chunks = 0x0` while the
// reader was writing to `0xc000218380`, and `Next(2)` returned
//
//	"parecord from \"pulse:@DEFAULT_SOURCE@\" produced no audio within 10s"
//
// after exactly the 10 s grace — on a device that had stamped its own first
// sample (s.firstSample) 10 s earlier and proved `voice_input` works.
//
// WHY THE EXISTING SEAM CASE DID NOT CATCH IT. voiceFileSource builds its channel
// in newVoiceFileSource, so for a FILE run the pre-Open read happened to be
// correct. The equivalence case (TestVoiceAudioSourceFileAndProcessAgreeOnWindows)
// feeds BOTH arms through a scripted source that also builds its channel
// up-front — so both arms were green while the production device path was dead.
// The invariant belongs to the SOURCE, not to one implementation: a source's
// channel exists once it is OPEN.
//
// THE RED PROOF is the shape below: a source that materialises its channel in
// Open, exactly as the production source does. Against the pre-fix code this case
// hangs until its own deadline and reports "no audio within 10s"; against the
// fixed code it returns the fixture's audio.

import (
	"testing"
	"time"
)

// lateChannelSource is the SMALLEST source that shows the ordering rule: its
// channel does not exist until Open, which is what the production recorder source
// does. Its delivery is synchronous and unpaced — the case is about WHICH channel
// the builder listens on, not about pacing.
type lateChannelSource struct {
	pcm    []byte
	chunks chan []byte
	opened bool
}

func newLateChannelSource(pcm []byte) *lateChannelSource {
	return &lateChannelSource{pcm: pcm}
}

// Chunks answers the source's hand-off. BEFORE Open it is the zero value, exactly
// as voiceStagedRecorderSource.Chunks() is before its Open runs.
func (s *lateChannelSource) Chunks() chan []byte { return s.chunks }

func (s *lateChannelSource) Open() error {
	s.opened = true
	s.chunks = make(chan []byte, voiceStagedQueueChunks)
	go func() {
		defer close(s.chunks)
		for off := 0; off < len(s.pcm); off += voiceAudioSourceChunkBytes {
			end := off + voiceAudioSourceChunkBytes
			if end > len(s.pcm) {
				end = len(s.pcm)
			}
			s.chunks <- s.pcm[off:end]
		}
	}()
	return nil
}

func (s *lateChannelSource) Health() error  { return nil }
func (s *lateChannelSource) Origin() string { return "a scripted late-channel recorder" }
func (s *lateChannelSource) Close()         {}

// Diagnostic is empty: a scripted source has no recorder behind it, so there is
// no child diagnostic to report (voiceAudioSource.Diagnostic).
func (s *lateChannelSource) Diagnostic() string { return "" }

// TestVoiceStagedStreamTakesTheChannelOfTheOpenSource is THE case: the builder
// must receive the audio of a source whose channel appears in Open.
func TestVoiceStagedStreamTakesTheChannelOfTheOpenSource(t *testing.T) {
	pcm := boundarySpeech(seamSpeechMS)
	src := newLateChannelSource(pcm)

	if src.Chunks() != nil {
		t.Fatalf("the probe source built its channel before Open — this case would pass without testing anything")
	}

	stream, err := newVoiceStagedStream(src)
	if err != nil {
		t.Fatalf("open the stream: %v", err)
	}
	defer stream.Close()

	if !src.opened {
		t.Fatalf("the stream did not open its source")
	}
	if stream.chunks == nil {
		t.Fatalf("the builder holds a nil channel: it read source.Chunks() before the source had one — "+
			"Next will block past every deadline and the mode reports \"produced no audio within %ds\" about a live device",
			voiceCaptureGraceSec)
	}
	if stream.chunks != src.chunks {
		t.Fatalf("the builder's channel is not the source's: it selects on a channel nobody writes to")
	}

	// THE BOUND IS THE DEADLINE THE DEFECT WAS REPORTED AGAINST: a builder on a
	// nil channel waits out the grace and then refuses, so a case that returns
	// audio here cannot be won by waiting.
	type result struct {
		pcm  []byte
		span voiceWindowSpan
		err  error
	}
	done := make(chan result, 1)
	go func() {
		audio, span, nerr := stream.Next(seamWindowSeconds)
		done <- result{audio, span, nerr}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Next failed on a source that was delivering: %v", got.err)
		}
		if len(got.pcm) == 0 {
			t.Fatalf("Next returned no audio from a source that delivered %d byte(s)", len(pcm))
		}
		if got.span.End-got.span.Start != len(got.pcm) {
			t.Fatalf("the span covers %d bytes but %d were returned",
				got.span.End-got.span.Start, len(got.pcm))
		}
	case <-time.After(time.Duration(voiceCaptureGraceSec) * time.Second):
		t.Fatalf("Next produced nothing within the %ds grace the operator's refusal names — "+
			"the builder is selecting on a channel the source's reader does not write to", voiceCaptureGraceSec)
	}
}
