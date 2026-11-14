package main

// cli_voice_stream_mic.go — the BARE capture fed to the streaming channel AS IT
// IS SPOKEN (the routing half of task 01a12161: the first-text report itself, and
// the file-fed pass it was measured with, are cli_voice_first_text.go's).
//
// THE DEFECT THIS CLOSES, measured on host-b's own microphone (2026-10-09, task
// 01a120a7's evidence, ev4-bare-streaming-mic.err):
//
//	[voice] first text at 14.7s (first partial) (engine doubao-stream) = capture
//	  window 12.1s + engine/channel 558ms + 2.1s of startup + channel handshake
//
// The channel answered in 558ms — the fastest of the three engines — and the
// operator still waited 14.7s, because the bare streaming shape RECORDED the whole
// --seconds window first (voiceRecord → a WAV → transcribeFileReport) and only
// then streamed the finished file. That is not streaming: it is a recording with a
// streaming transcriber attached, and on that surface the recording-vs-streaming
// comparison the parent epic asked for compared nothing — both modes paid the same
// 12s window and the channel's 0.5s was buried in it.
//
// THE FIX IS ROUTING, NOT A SECOND CLIENT. Everything already exists:
//
//	openVoiceStagedStream(opts, dev)     the ONE capture seam (cli_voice_audio_source.go)
//	voiceStagedStream.Chunks()           the microphone's own arrival-ordered hand-off
//	newVoiceLiveFeeder / voiceLiveClock  the production live arm (chat_voice_live.go),
//	                                     proven by chat `/voice` live mode
//	doubaostream.Open/Say/Finish         the ONE streaming client
//
// so the bare shape now opens a MICROPHONE-backed stream and feeds it to the SAME
// feeder the chat's live dictation uses. Rule 3/14: no second capture path, no
// second client, no second packet loop — the feeder and the clock are the
// production ones, and nothing here re-derives the packet geometry or the
// end-of-stream discipline.
//
// WHAT DOES NOT CHANGE, and it is the whole of the recording path:
//
//	--engine recording        → capture the window, transcribe the WAV (unchanged)
//	--audio-source FILE       → the file source, no device opened (unchanged)
//	--record FILE             → written from the SAME capture (the sink below)
//	the file / --client / --server shapes → untouched
//
// THE SPLIT CARRIES NO CAPTURE-WINDOW TERM, and that is a fact about the run
// rather than a word left out of the line: report.WindowMS stays ZERO because the
// audio goes out as it arrives — the time between the tool's start and the first
// text is the channel's own latency plus the run's startup, and there is no
// recording in front of it. The report line already knows how to say that (it
// names the window ONLY when WindowMS > 0 — cli_voice_first_text.go), so the shape
// is expressed by the measurement instead of by a second renderer.

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaostream"
)

const (
	// voiceMicStreamShapeNote is the line that says WHICH shape this run is: the
	// microphone feeds the channel directly, so nothing is recorded ahead of it.
	//
	// IT EXISTS BECAUSE `--seconds` MEANS SOMETHING ELSE HERE. On the recording
	// path the flag is a WINDOW — no engine sees a byte until it elapses — and on
	// this one it is the length of the UTTERANCE, ended when the capture reaches
	// it. A reader who has seen the recording mode has no way to infer the change,
	// and the difference is exactly what the run's own timing report is about.
	voiceMicStreamShapeNote = "[voice] stream: the audio goes out as it is captured — --seconds %d bounds the UTTERANCE (the capture is ended when it is reached), not a recording in front of the channel\n"

	// voiceMicStreamNoFirstText is WHY a run on this path produced no first text:
	// the capture ended with the channel still silent. It is a sentence rather
	// than a zero — the absence is stated, never dressed as an instantaneous
	// answer (Rule 15, the same discipline the file-fed pass applies to a silent
	// recording).
	voiceMicStreamNoFirstText = "the microphone capture ended before the %s channel produced a partial (the utterance held no speech, or the channel never answered)"

	// voiceMicStreamBufferNote is printed when the channel did not take every byte
	// the microphone produced. The buffered bytes are NOT lost (they go up with the
	// end-of-stream flag — chat_voice_live.go's takePending), so this is a
	// statement about the run rather than a failure; but a run whose audio did not
	// all fit in one utterance is a fact the operator needs, because the transcript
	// is of what was SENT.
	voiceMicStreamBufferNote = "[voice] stream: %d of %d bytes (%.1fs of the utterance) were still buffered when the capture ended and were sent with the end-of-stream flag\n"

	// voiceMicStreamCapturedNote is the capture's own account of the run: how much
	// audio the sink saw, how long that is, and which source produced it. It is the
	// FIRST half of the diagnosis when a run reports no text — "the microphone
	// delivered nothing" and "the channel said nothing about the audio it was given"
	// are different failures with the same symptom, and this count separates them.
	voiceMicStreamCapturedNote = "[voice] captured %d bytes (%.1fs of %dHz mono PCM16) from %s\n"

	// voiceMicStreamRecordNote reports what --record kept on this path. The
	// recording is the SAME bytes the channel was fed — one capture, two consumers
	// — and the count is printed for the same reason the one-shot path prints it: a
	// kept file whose length nobody states is a file the operator has to trust.
	voiceMicStreamRecordNote = "[voice] kept %d bytes of WAV (%.1fs) in %s\n"
)

// voiceCaptureTakesLiveStream reports whether the BARE capture shape should feed the
// microphone to the streaming channel as it is spoken, and it is the ONE dispatch
// predicate of this routing (Rule 3): the first-text report cannot describe a
// different shape from the one that ran.
//
// THE TWO CONDITIONS ARE THE SHAPE AND THE ENGINE, and neither is a preference:
//
//	the engine   — config.AudioEngineStreaming is the ONLY engine with a live
//	               channel. `recording` (batch) has nothing to stream to, and the
//	               local engine takes a FILE, so both keep the recording path.
//	the shape    — the BARE capture. A run that named --audio-source reads a file
//	               (there is no microphone to feed, and that is the whole point of
//	               the flag), and a --record run is handled INSIDE this arm (the
//	               capture is kept while it is streamed, so the flag's contract
//	               holds on both paths — see voiceMicStreamKeepRecording).
func voiceCaptureTakesLiveStream(opts *voiceOptions) bool {
	if opts == nil || strings.TrimSpace(opts.audioSource) != "" {
		return false
	}
	return voiceResolveEngine(opts) == config.AudioEngineStreaming
}

// voiceRunMicStream runs the bare capture's live streaming shape end to end:
// open the session over the microphone, wait for the utterance to end, print the
// transcript and the report — and never return, because this arm's output is the
// run's output (the caller's recording path is not resumed: there is no capture
// block to transcribe).
//
// IT REACHES EXIT THE SAME WAY THE RECORDING PATH DOES (os.Exit(1) with the
// error, the transcript on stdout and the progress on stderr), so the two shapes
// have one CLI contract; only the way the text is produced differs.
func voiceRunMicStream(opts *voiceOptions, dev voiceDevice, runClock *voiceRunClock) {
	text, report, err := voiceMicStreamFirstText(opts, dev, runClock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
		os.Exit(1)
	}
	// --self types the transcript into this process's own window instead of only
	// printing it — the same courtesy the recording path extends, with the label
	// that names the streaming engine (voiceLocalEngineLabel names the LOCAL
	// engine, which this run is not).
	if opts.self {
		if err := voiceTypeIntoOwnWindow(opts, text, voiceEngineLabel(opts)); err != nil {
			fmt.Fprintf(os.Stderr, "gopi tool voice_input: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println(text)
	// THE EMPTY TRANSCRIPT IS ANNOUNCED, exactly as it is on the file shapes: an
	// empty stdout with no explanation reads as a broken pipe (Rule 15).
	voiceReportTranscribedBy(opts, text, time.Duration(report.FinalMS)*time.Millisecond)
	// THE REPORT CARRIES NO CAPTURE WINDOW TERM — the audio went out as it was
	// captured — and report.WindowMS is deliberately left at zero by the arm above
	// rather than zeroed here, so the line states the run's own measurement and not
	// a word this function removed.
	voiceReportFirstText(opts, report)
}

// voiceMicStreamBudgetMS is how long this run waits for the utterance to END
// before it finishes the stream with the audio in hand.
//
// IT IS DERIVED, NEVER A NEW NUMBER (Rule 1/14): the utterance's own length in
// milliseconds (--seconds, through the byte conversion's own divisor) plus the
// budget the file-fed pass already allows for "how long after the audio was sent a
// partial may still arrive". The capture normally ends the wait itself — the sink
// below ends the recorder at the --seconds bound — so this is the backstop for a
// source that neither delivers nor stops, and the run always produces a report.
func voiceMicStreamBudgetMS(seconds int) int64 {
	return int64(seconds)*voiceAudioSourceMSPerSecond + voiceFirstTextBudgetMS
}

// voiceMicStreamFirstText runs ONE streaming pass fed by the MICROPHONE, and
// reports when the first text and the final transcript arrived.
//
// IT IS THE FILE-FED PASS WITH THE SOURCE SWAPPED (cli_voice_first_text.go's
// voiceStreamFirstText), deliberately: the session options, the credential origin
// line, the partial printing, the feeder, the end-of-stream tail and the
// first-partial accounting are the same calls in the same order, so the two
// surfaces differ in WHERE THE BYTES COME FROM and in nothing else. A second
// implementation of any of the rest would make the comparison between the file and
// the microphone a comparison of two implementations.
func voiceMicStreamFirstText(opts *voiceOptions, dev voiceDevice, runClock *voiceRunClock) (string, voiceFirstTextReport, error) {
	var report voiceFirstTextReport
	cred, credPath, err := voiceStreamCredentials(opts)
	if err != nil {
		return "", report, err
	}
	// THE CAPTURE IS THE MICROPHONE, through the ONE seam: the same two calls
	// openVoiceStagedStream is made of (cli_voice_audio_source.go — it is exactly
	// `voiceAudioSourceFor` followed by `newVoiceStagedStream`), with the sink
	// between them.
	//
	// THE SINK HAS TO BE THE SOURCE AT CONSTRUCTION, not replaced afterwards: the
	// staged builder takes the source's chunks channel ONCE, right after the
	// source is opened (newVoiceStagedStream — and that ordering is itself a fix,
	// commit 1c6c5f7ab), so a source swapped in after the fact would leave the
	// feeder reading the RECORDER's channel and the sink watching a channel nobody
	// drains. Composing the two seam calls is what keeps this one capture path
	// (Rule 14) while still letting one component see every byte.
	src, err := voiceAudioSourceFor(opts, &dev)
	if err != nil {
		return "", report, err
	}
	sink := &voiceMicStreamSink{bin: src, max: voiceBytesPerSeconds(opts.seconds), keep: opts.record != ""}
	stream, err := newVoiceStagedStream(sink)
	if err != nil {
		return "", report, err
	}
	// THE MICROPHONE IS RELEASED ON EVERY EXIT PATH (the recorder process is
	// signalled and reaped), the same discipline the chat's live arm keeps.
	defer stream.Close()

	endpoint := voiceStreamEffectiveEndpoint(opts)

	// THE SESSION, THE CLOCK, THE FEEDER, THE WAIT AND THE IN-HAND FLUSH ARE ONE
	// IMPLEMENTATION (voice_stream_runtime.go), shared with chat `/voice`'s live
	// dictation and the file-fed first-text pass. What is specific to THIS shape is
	// the sink's byte budget that ends the utterance, the shape note, and what a
	// partial means here (print it).
	run := runVoiceLiveSession(stream, voiceLiveRunConfig{
		// THIS ARM IS THE v2 CHANNEL'S, named as such rather than resolved from
		// the operator's settings: these two tool shapes measure the v2 engine, and
		// a settings file pointing the ENGINE elsewhere must not silently change
		// which channel a documented v2 measurement ran on (Rule 1/2 — the
		// constant is the value, and one place names it).
		Backend: voiceLiveBackendStreaming(),
		Options: doubaostream.Options{
			AppID:    cred.AppID,
			Token:    cred.Token,
			Endpoint: endpoint,
		},
		OnPartial: func(p voiceLivePartial) { voiceReportStreamPartial(voiceLivePartialV2(p), opts.voiceVerbose()) },
		Budget:    time.Duration(voiceMicStreamBudgetMS(opts.seconds)) * time.Millisecond,
	})
	// The endpoint (always) and the credential's ORIGIN (under --verbose) are
	// stated once, before the audio moves — the same disclosure every other
	// streaming shape prints, through the same function (Rule 2/14).
	voiceReportStreamStart(cred, credPath, doubaostream.DefaultChunkMillis, endpoint, opts.voiceVerbose())
	// THE SHAPE NOTE AND THE CAPTURE'S ACCOUNT ARE THE DEBUG SINK OF THIS ARM (see
	// voiceOptions.verbose): the shape note explains what --seconds means on this
	// path — a fact an operator reads ONCE when the behaviour surprises them, and
	// not something to re-print before every transcript — and the captured-bytes
	// count is the diagnosis of a silent run. Both are one switch away, and the
	// run's own timing line (voiceReportFirstText) stays on the default path.
	if opts.voiceVerbose() {
		fmt.Fprintf(os.Stderr, voiceMicStreamShapeNote, opts.seconds)
	}
	finalAt := time.Now()
	res := run.Result
	// THE CAPTURE'S OWN ACCOUNT, printed before the channel's: how much audio the
	// sink actually saw and which source produced it. It is the FIRST half of the
	// diagnosis when a run reports no text — "the microphone delivered nothing" and
	// "the channel said nothing about the audio it was given" are different
	// failures with the same symptom, and the count separates them.
	if opts.voiceVerbose() {
		fmt.Fprintf(os.Stderr, voiceMicStreamCapturedNote, sink.sent(), voiceMicStreamPCMSeconds(sink.sent()), voiceCaptureSampleRate, stream.sourceLabel())
	}
	// The audio still in hand went up BEFORE the end-of-stream flag, so the last
	// packet is not dropped between the two — the same losslessness the file-fed
	// pass guarantees, over the same feeder. It is NOT added to the recording: the
	// sink already counted and kept those bytes as they arrived, and appending the
	// feeder's copy of them would count them twice.
	if len(run.Tail) > 0 && opts.voiceVerbose() {
		if sent := sink.sent(); sent > 0 {
			fmt.Fprintf(os.Stderr, voiceMicStreamBufferNote, len(run.Tail), sent, voicePCMSeconds(run.Tail))
		}
	}
	// THE RECORDING IS WRITTEN FROM THE CAPTURE ITSELF, on every exit path that got
	// one: the operator asked for the audio, and a channel that broke does not
	// un-ask it. Its failure is reported only after the channel's own (below), so
	// the FIRST failure is the one named.
	recordErr := voiceMicStreamKeepRecording(opts, sink)
	if err := run.error(); err != nil {
		return "", report, voiceStreamError(err, res)
	}
	if recordErr != nil {
		return "", report, recordErr
	}
	if res == nil {
		// The runner always hands back the session's own result on a run that
		// reached the channel; a nil here would mean a call that never ran.
		return "", report, fmt.Errorf("the %s channel produced no result for this capture", config.AudioEngineStreaming)
	}
	// What the channel itself observed — how many partials, when the first and last
	// arrived, how much audio went out — through the SAME summary the other
	// streaming shape prints (Rule 2/14: one spelling of these figures).
	voiceReportStreamSummary(res, opts.voiceVerbose())
	report.Streamed = true
	report.EngineMS = run.Clock.firstPartialLatencyMS()
	// THE TWO FIGURES ARE ONE STAMP READ FROM TWO ORIGINS, not two measurements:
	// FirstTextMS is the instant the first text existed measured from the tool's
	// start (the number the operator waited), and EngineMS is the same instant
	// measured from the first packet sent (the channel's own latency). Taking them
	// from one clock is what makes the split add up.
	if wall := run.Clock.firstPartialWall(); !wall.IsZero() {
		report.FirstTextMS = int(runClock.since(wall).Milliseconds())
	}

	// TWO STAMPS, ONE INSTANT, AND THE SMALLER ONE WINS (the same guard the
	// file-fed pass applies): the channel's figure is the floor — hard evidence
	// cannot precede the run that contains it — so the split can never read as a
	// negative remainder.
	if report.FirstTextMS < report.EngineMS {
		report.FirstTextMS = report.EngineMS
	}
	report.FinalMS = int(runClock.since(finalAt).Milliseconds())
	if report.EngineMS <= 0 {
		// The honest absence. MEASURED: a microphone that produced no speech (or a
		// channel that never answered) yields no partial at all, and a report reading
		// "0ms" there would claim a latency that never happened.
		report.Missing = fmt.Sprintf(voiceMicStreamNoFirstText, config.AudioEngineStreaming)
	}
	// WINDOWMS STAYS ZERO, and it is the point of this whole file: no part of this
	// total is a recording, because the audio went out as it was captured.
	return strings.TrimSpace(res.Text), report, nil
}

// voiceMicStreamPCMSeconds is the LENGTH of n bytes of the capture contract,
// derived from the ONE conversion that defines it (voiceBytesPerSeconds) rather
// than from a second spelling of "16 kHz mono PCM16" (Rule 13). It exists because
// this path reports the length of audio it counted WITHOUT keeping a copy of it —
// the operator who did not ask for --record does not pay for the bytes, but is
// still told how many there were.
func voiceMicStreamPCMSeconds(n int) float64 {
	if n <= 0 {
		return 0
	}
	return float64(n) / float64(voiceBytesPerSeconds(1))
}

// voiceMicStreamKeepRecording writes the capture `--record` asked for.
//
// IT IS THE ONE-SHOT PATH'S OWN STORE (voiceStoreCapture), so the flag's contract
// is identical on both shapes: the file is REPLACED, the truncation is explicit,
// and the byte count is announced. The audio is the same audio the channel was fed
// — the sink kept it while it was passing it through — which is the honest reading
// of "keep this capture's audio" on a path that has no WAV of its own until the
// capture ends.
func voiceMicStreamKeepRecording(opts *voiceOptions, sink *voiceMicStreamSink) error {
	if opts.record == "" {
		return nil
	}
	pcm := sink.kept()
	if len(pcm) < voiceMinCaptureBytes {
		return fmt.Errorf("--%s: the capture produced no audio to keep", voiceFlagRecord)
	}
	if err := voiceStoreCapture(opts.record, voicePCM16ToWAV(pcm)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, voiceMicStreamRecordNote, len(pcm), voicePCMSeconds(pcm), opts.record)
	return nil
}

// voiceMicStreamSink is the streaming capture's BYTE WITNESS: it passes every
// chunk through untouched, ends the capture at the operator's own bound, and keeps
// the bytes `--record` asked for.
//
// WHY IT IS A SOURCE AND NOT A WRAPPER AROUND ONE. The feeder reads the stream's
// chunks channel directly (it never consults the stream's Next, and does not know
// a source exists), so the only place every byte of a live capture passes through
// is this call: `Chunks` is taken from the source ONCE, at open time, and the
// feeder reads it from there. A watcher the feeder did not know about would see
// nothing at all.
//
// THE CUT IS `--seconds`, AND IT ENDS THE UTTERANCE. On the recording path the
// same number is the length of the file the engine is handed; here there is no
// file, so the bound is expressed by ENDING THE CAPTURE when the audio reaches it:
// the recorder process is stopped, its stdout closes, and the feeder's own
// end-of-stream path finishes the utterance with the audio in hand. That is the
// one bound the invocation already asked for — no new timer, no new flag, and no
// second clock over the same capture (Rule 35).
type voiceMicStreamSink struct {
	// bin is the source this sink replaced: where the bytes come FROM, and the only
	// thing that owns a process, a pipe and a reap — so this sink's Close delegates
	// rather than re-implementing the teardown (Rule 14).
	bin voiceAudioSource
	// max is the utterance's length in bytes (voiceBytesPerSeconds), or 0 for a
	// capture with no bound — the same meaning `--seconds 0` has on the one-shot
	// path.
	max int
	// keep says whether the operator asked for the audio (`--record`): a run that
	// did not ask does not pay for a second copy of a multi-megabyte capture in
	// memory (the one-shot path holds one copy because it must hand it to the
	// engine).
	keep bool

	// mu guards the fields below; they are written by this sink's pump goroutine
	// and read by the caller once the utterance is over.
	mu        sync.Mutex
	n         int
	keptBytes []byte

	// done is closed by Close and releases the pump if it is parked on the hand-off.
	done chan struct{}
}

// Open adopts the source this sink replaced: from here the bytes come from it.
func (s *voiceMicStreamSink) Open() error { return s.bin.Open() }

// Chunks REPLACES the source's channel with this sink's own and pumps the
// original into it: the pump is where every byte is seen, counted, kept and — on
// the bound — cut.
func (s *voiceMicStreamSink) Chunks() chan []byte {
	s.done = make(chan struct{})
	out := make(chan []byte, cap(s.bin.Chunks()))
	go s.pump(out)
	return out
}

// pump hands every chunk on untouched, in order, until the capture ends — or until
// the operator's own `--seconds` bound is reached, at which point it STOPS THE
// RECORDER (which closes the source's channel, the feeder's end-of-stream signal)
// and returns. A chunk that crosses the bound is cut to it, so the utterance is the
// length that was asked for.
func (s *voiceMicStreamSink) pump(out chan []byte) {
	defer close(out)
	for {
		// THE BOUND IS TESTED BEFORE THE NEXT CHUNK IS WAITED FOR, not after: a
		// capture whose audio is complete must END here rather than park on a
		// channel that is still open (the recorder is stopped below, and waiting
		// for it to close first would leave the utterance hanging on a race between
		// two processes). The count is read through the sink's own accessor so the
		// field has ONE discipline rather than an implicit "only I touch it" the
		// next editor cannot see.
		if s.max > 0 && s.sent() >= s.max {
			break
		}
		chunk, ok := <-s.bin.Chunks()
		if !ok {
			break
		}
		if s.max > 0 {
			if seen := s.sent(); seen+len(chunk) > s.max {
				chunk = chunk[:s.max-seen]
			}
		}
		s.adopt(chunk)
		select {
		case out <- chunk:
		case <-s.done:
			return
		}
	}
	if s.max > 0 && s.sent() >= s.max {
		// THE UTTERANCE IS OVER, and the capture is ended the way the one-shot path
		// ends it: the recorder process is stopped and reaped by the SAME helper
		// (stopVoiceRecorder, through the source's own Close), so "we closed its
		// window" is classified once for the whole command (Rule 14).
		s.bin.Close()
	}
}

// adopt counts a chunk that was passed through, and keeps it when `--record` asked
// for the audio.
func (s *voiceMicStreamSink) adopt(chunk []byte) {
	s.mu.Lock()
	s.n += len(chunk)
	if s.keep {
		s.keptBytes = append(s.keptBytes, chunk...)
	}
	s.mu.Unlock()
}

// sent is how many bytes the capture delivered to the channel.
func (s *voiceMicStreamSink) sent() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// kept hands back the capture's audio, the `--record` copy.
func (s *voiceMicStreamSink) kept() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keptBytes
}

// Health delegates: how the capture ended is the underlying source's account, and
// a second opinion here is how two verdicts come to disagree.
func (s *voiceMicStreamSink) Health() error { return s.bin.Health() }

// Diagnostic delegates for the same reason Health does: the account of why the
// capture ended belongs to the source that owns the process, not to the sink that
// watched it (voiceAudioSource.Diagnostic).
func (s *voiceMicStreamSink) Diagnostic() string { return s.bin.Diagnostic() }

// Origin delegates for the same reason: a refusal must name the device or the file
// that actually failed, not the sink that watched it.
func (s *voiceMicStreamSink) Origin() string { return s.bin.Origin() }

// Close releases the pump's hand-off and then the source, which is the only owner
// of the process. Idempotent, like every Close on this seam.
func (s *voiceMicStreamSink) Close() {
	if s == nil || s.bin == nil {
		return
	}
	if s.done != nil {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	}
	s.bin.Close()
}
