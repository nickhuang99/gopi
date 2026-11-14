package main

// cli_voice_stream_mic_test.go — the routing contract of the bare capture's
// streaming arm (cli_voice_stream_mic.go).
//
// WHAT IS PINNED, and why each property is here rather than in an end-to-end run.
// Three of these are DISPATCH and ARITHMETIC: which shape a given invocation takes,
// how many bytes an utterance is, and what the run's own report may say about the
// time it spent. An end-to-end case cannot isolate them — it can only show the
// whole thing working — and the run that motivated this file is measured in
// seconds of real speech against a real service. The real run is evidence
// (Rule 31, in the report); these are the guards.
//
// THE RED PROOF, stated because it is what makes the first case worth having: on
// the pre-change tree the bare capture + streaming engine RECORDED the whole
// --seconds window before any byte went out, so the run's own report carried a
// capture-window term and the first text waited out that window (measured: 14.7s,
// of which 12.1s was the recording — task 01a120a7's ev4-bare-streaming-mic.err).
//
// IT IS MEASURED, AND IT IS NARROWER THAN "the file is red on the old tree". Three
// proofs, each with the output it produced:
//
//	base tree 2d9ee72c2 (the prerequisite branch is NOT in it, so these files do not
//	compile there at all):
//	  cli_voice_stream_mic.go:133:71: undefined: voiceRunClock
//	  cli_voice_stream_mic.go:157:2: undefined: voiceReportFirstText
//	  cli_voice_stream_mic.go:170:54: undefined: voiceFirstTextBudgetMS
//	  cli_voice_stream_mic.go:282:18: feed.sendFailure undefined (type *voiceLiveFeeder has no field or method sendFailure)
//	  cli_voice_stream_mic_test.go:120:10: undefined: voiceFirstTextReport
//	  FAIL github.com/nickhuang99/gopi/cmd/agent [build failed]
//
//	b0bd44d34 (the prerequisite IS in it, this routing is not — the honest
//	"pre-change tree" for the routing, and still a build failure):
//	  cli_voice_stream_mic_test.go:123:28: undefined: voiceFirstTextWindowTermLabel
//	  cli_voice_stream_mic_test.go:126:29: undefined: voiceFirstTextEngineTermLabel
//	  FAIL github.com/nickhuang99/gopi/cmd/agent [build failed]
//
//	controlled revert — this file present and compiling, the dispatch in
//	voiceCaptureTakesLiveStream answering false the way the old code behaved:
//	  --- FAIL: TestBareCaptureStreamingTakesTheLiveStream (0.00s)
//	      the bare capture with the streaming engine does not take the live
//	      microphone arm — it records the whole --seconds window first
//
// A BUILD FAILURE IS NOT A RED TEST, which is why the third proof is the one that
// counts: the first two say "the file is new", the third says "the tree without the
// routing fails the assertion that describes the routing".
//
// SO ONE CASE IS THE RED GUARD AND THE OTHER THREE ARE GREEN-FIRST, which is
// stated rather than glossed: TestMicStreamSplitCarriesNoCaptureWindowTerm asserts
// the RENDERER's contract (a zero WindowMS prints no window term, a measured one
// still does) and it PASSES under the controlled revert too — the renderer was
// never the broken half. It is kept because it is the contract the routing must
// keep feeding (Rule 30a's discipline: a guard that is green before and after is
// still the guard against the term coming back), not because it was ever red.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// micStreamOptions is the bare capture's options as the parser builds them: no
// positional, no --audio-source, and the engine under test.
func micStreamOptions(engine string) *voiceOptions {
	return &voiceOptions{
		capture: true,
		engine:  engine,
		seconds: voiceLocalDefaultSeconds,
	}
}

// TestBareCaptureStreamingTakesTheLiveStream is the ROUTING contract: only the
// bare capture on the streaming engine may feed the microphone to the channel as
// it is captured, and every other invocation keeps the recording path.
//
// IT IS THE CASE THAT WAS RED BEFORE THE CHANGE (see this file's header): the
// predicate did not exist and the shape it describes did not happen — the run
// recorded first. A test asserting "the streaming shape streams" is the guard
// against the routing being quietly removed again, which is how a fixed latency
// comes back.
func TestBareCaptureStreamingTakesTheLiveStream(t *testing.T) {
	streaming := micStreamOptions(config.AudioEngineStreaming)
	if !voiceCaptureTakesLiveStream(streaming) {
		t.Fatal("the bare capture with the streaming engine does not take the live microphone arm — it records the whole --seconds window first, which is the 14.7s defect this routing exists to remove")
	}

	// THE OTHER ENGINES KEEP THE RECORDING PATH, byte for byte what they were. The
	// batch engine has no live channel to feed and the local engine takes a FILE —
	// routing either of them through the live arm would send their audio somewhere
	// it has never gone, and the acceptance criterion "the non-streaming engines
	// are unaffected" is this half of the case.
	for _, engine := range []string{config.AudioEngineRecording, config.AudioEngineLocal} {
		if voiceCaptureTakesLiveStream(micStreamOptions(engine)) {
			t.Errorf("the bare capture on engine %q was routed to the live microphone arm — only %q has a live channel",
				engine, config.AudioEngineStreaming)
		}
	}

	// --audio-source NAMES A FILE: there is no microphone to feed, which is the
	// whole point of the flag (and it is what makes the shape usable on a host with
	// no sound card). The file keeps the file-fed streaming pass
	// (voiceStreamFirstText), which is a different function on purpose.
	named := micStreamOptions(config.AudioEngineStreaming)
	named.audioSource = "/tmp/utterance.wav"
	if voiceCaptureTakesLiveStream(named) {
		t.Error("a run that named --audio-source was routed to the live MICROPHONE arm — a file is the file-fed pass's business")
	}

	// A nil options is "no invocation", not a crash: the routing is read by a caller
	// that always has options today, and the next one may not.
	if voiceCaptureTakesLiveStream(nil) {
		t.Error("a nil invocation was routed to the live microphone arm")
	}
}

// TestMicStreamSplitCarriesNoCaptureWindowTerm is THE acceptance property, at the
// level the report is produced: a run whose audio went out as it was captured must
// report a split with NO capture-window term, while the recording path must still
// report the measured window.
//
// THE TWO HALVES ARE ONE ASSERTION ABOUT A DIFFERENCE, which is the only way to
// read this line: "capture window" being absent is meaningless unless the same
// renderer still prints it for the path that does record. A renderer that simply
// dropped the term would pass the first half and fail the second.
func TestMicStreamSplitCarriesNoCaptureWindowTerm(t *testing.T) {
	opts := micStreamOptions(config.AudioEngineStreaming)
	// The figures a live run observed on host-b's microphone: the first partial
	// 2.2s after the tool started, of which the channel's own latency was 600ms and
	// the rest was this process's startup and the channel handshake — and NOTHING
	// is a recording, so WindowMS is left at its zero value exactly as
	// voiceMicStreamFirstText leaves it.
	live := voiceFirstTextReport{FirstTextMS: 2200, EngineMS: 600, FinalMS: 12500, Streamed: true}
	line := voiceFirstTextReportLine(opts, live)

	if strings.Contains(line, voiceFirstTextWindowTermLabel) {
		t.Fatalf("the bare streaming report carries a capture-window term — the audio went out as it was captured and no part of this total is a recording:\n%s", line)
	}
	if !strings.Contains(line, voiceFirstTextEngineTermLabel) || !strings.Contains(line, "2.2s") {
		t.Fatalf("the bare streaming report does not state the total and the channel's own latency:\n%s", line)
	}
	// THE TERMS MUST ADD UP: with no window, the total is the channel's figure plus
	// the startup remainder — and this report is the one case where the whole total
	// is shorter than the recording path's WINDOW alone.

	// THE RECORDING PATH STILL CARRIES IT, with the window the run MEASURED (not
	// the --seconds it asked for): this is the pre-change line, and it is the
	// baseline the streaming shape is compared against.
	recorded := voiceFirstTextReport{FirstTextMS: 14700, WindowMS: 12100, EngineMS: 558, FinalMS: 14700}
	recordingLine := voiceFirstTextReportLine(opts, recorded)
	if !strings.Contains(recordingLine, voiceFirstTextWindowTermLabel) {
		t.Fatalf("the recording path stopped reporting its capture window — the comparison this whole report exists for needs both halves:\n%s", recordingLine)
	}
	if !strings.Contains(recordingLine, voiceFirstTextWindowTermLabel+" 12.1s") {
		t.Fatalf("the recording path does not report the window it MEASURED:\n%s", recordingLine)
	}
}

// TestMicStreamUtteranceLengthIsTheSecondsBound pins the ONE number this path
// bounds an utterance by, and it pins it to the SAME conversion the file source
// paces by: --seconds is a length of audio in both shapes, and a second spelling of
// "16 kHz mono PCM16" here would drift from the capture contract the recorder is
// started with.
func TestMicStreamUtteranceLengthIsTheSecondsBound(t *testing.T) {
	const seconds = 12
	sink := &voiceMicStreamSink{max: voiceBytesPerSeconds(seconds)}
	if got, want := sink.max, seconds*voiceCaptureSampleRate*voiceCaptureBytesPerSample; got != want {
		t.Fatalf("--seconds %d bounds an utterance at %d bytes, want %d", seconds, got, want)
	}
	// The wait for the utterance to end is the utterance itself plus the budget the
	// file-fed pass already allows for a late partial, and it is DERIVED: a new
	// literal here would be a second answer to "how long may this run wait".
	if got, want := voiceMicStreamBudgetMS(seconds), int64(seconds)*voiceAudioSourceMSPerSecond+voiceFirstTextBudgetMS; got != want {
		t.Fatalf("the utterance budget is %dms, want %dms", got, want)
	}
}

// TestMicStreamSinkPassesEveryByteAndCutsAtTheBound drives the sink — the ONE
// component that sees every byte of this capture — over a scripted source, and
// asserts what the live arm depends on: every byte reaches the channel, the count
// is the capture's own length, the utterance is CUT at --seconds, and the audio
// `--record` asked for is what was sent (not more, not less).
func TestMicStreamSinkPassesEveryByteAndCutsAtTheBound(t *testing.T) {
	const seconds = 2
	total := voiceBytesPerSeconds(seconds) + voiceAudioSourceChunkBytes
	src := newSeamProcessSource(make([]byte, total))
	sink := &voiceMicStreamSink{bin: src, max: voiceBytesPerSeconds(seconds), keep: true}

	if err := sink.Open(); err != nil {
		t.Fatalf("open the capture: %v", err)
	}
	chunks := sink.Chunks()
	sent := 0
	for chunk := range chunks {
		sent += len(chunk)
	}

	if want := voiceBytesPerSeconds(seconds); sent != want {
		t.Fatalf("the sink handed %d bytes to the channel, want the utterance's own %d — a capture that is not cut at --seconds records a longer utterance than the operator asked for",
			sent, want)
	}
	if got := sink.sent(); got != sent {
		t.Fatalf("the sink counted %d bytes but passed %d on", got, sent)
	}
	// --record KEEPS WHAT WAS SENT: the recording and the utterance are one capture,
	// so a file longer than the audio that went out would be a recording of
	// something the operator never heard.
	if got := len(sink.kept()); got != sent {
		t.Fatalf("--record kept %d bytes for an utterance of %d", got, sent)
	}
	// AND NOTHING IS KEPT WITHOUT THE FLAG: a run that did not ask for the audio
	// does not pay for a second copy of it in memory.
	quiet := &voiceMicStreamSink{bin: newSeamProcessSource(make([]byte, voiceAudioSourceChunkBytes))}
	if err := quiet.Open(); err != nil {
		t.Fatalf("open the capture: %v", err)
	}
	for range quiet.Chunks() {
	}
	if got := len(quiet.kept()); got != 0 {
		t.Fatalf("%d bytes were copied for a run that did not ask for --record", got)
	}
}
