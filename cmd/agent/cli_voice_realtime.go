package main

// cli_voice_realtime.go — the OPT-IN realtime display of `gopi tool
// voice_input` (--realtime).
//
// THE DEFECT THIS ANSWERS, stated as the operator met it: the DEFAULT shape
// (bare microphone capture + `--engine recording`, the cloud BATCH engine)
// records the whole --seconds window and prints nothing until the transcript
// appears. The streaming engines DO produce interim partials while the audio
// arrives — but the per-partial stream was wired to the --verbose DEBUG SINK
// (task 01a13500, deliberately: 19 partial lines buried the answer), so a
// normal run could not see them either. The operator had no way at all to know
// what was being heard, on any engine.
//
// WHAT THIS FLAG IS, and what it is NOT:
//
//	it ADDS a stream   one line per interim partial, as it arrives, on stderr.
//	                   The transcript stays on stdout and the ordinary per-run
//	                   lines are untouched — a run without the flag prints
//	                   exactly what it printed before it existed.
//	it is NOT --verbose the debug sink stays what it is (the partial stream PLUS
//	                   the channel's counters, the plumbing note and the live
//	                   shape note). --realtime prints the partials ONLY, because
//	                   the question it answers is "what is being heard", not
//	                   "what is the channel doing".
//	it is NOT a second printer or a second capture path (Rule 3/14)
//	                   the per-partial line is voiceReportStreamPartial — the ONE
//	                   printer — and the gate that decides whether it prints is
//	                   the ONE method voiceOptions.voicePrintPartials, which
//	                   folds this flag with the debug sink. Nothing about the
//	                   capture, the feeder or the channel is re-derived here.
//
// THE HONEST HALF, which is the other deliverable (Rule 15): an engine that
// cannot produce interim text must SAY so and name the route, rather than accept
// the flag and silently display nothing. On the recording path the run also
// prints a capture progress line, because "the tool is recording you" and "the
// tool is doing nothing" are indistinguishable in a silent terminal.

import (
	"fmt"
	"os"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// voiceFlagRealtime is the flag's spelling. Named (Rule 1b) because the
	// parser, the help page, the refusal sentences and the run's own lines all
	// quote it — four sites that must agree.
	voiceFlagRealtime = "realtime"

	// voiceRealtimeProgressInterval is how often the capture path repeats its
	// progress line while the microphone is being recorded. Named (Rule 1) and
	// deliberately coarse: its job is to prove the process is alive, not to
	// animate a spinner, and a line per second would be a second debug sink.
	voiceRealtimeProgressInterval = 2 * time.Second

	// voiceRealtimeCaptureStart announces the capture this flag is about to
	// make visible: how long it runs, how often the progress line repeats, and
	// that the transcript is still the deliverable. A silent literal number or
	// a bare "%ds" would let the promise and the behaviour drift (Rule 1).
	voiceRealtimeCaptureStart = "[voice] --%s: listening for %ds — a progress line every %s, then the transcript\n"

	// voiceRealtimeProgressTick is the progress line itself: the audio captured
	// so far, against the bound. It states a MEASUREMENT, never a percentage —
	// the device's clock is what decides, and a percentage of a requested
	// window would be a claim about a device that may run fast or slow.
	voiceRealtimeProgressTick = "[voice] captured %ds of %ds (%.1f%%)\n"

	// voiceRealtimeNoInterimNote is the HONEST sentence (Rule 15): the run was
	// asked for a realtime display and this engine cannot produce one. It names
	// the engine that cannot, the reason (it transcribes the finished recording
	// and answers once), and the route that CAN — so the flag is never a
	// no-op wearing an instruction.
	voiceRealtimeNoInterimNote = "[voice] --%s: engine %s produces NO interim text — it transcribes the finished recording and answers once, so there is nothing to show while you speak. The per-partial stream comes from the %s channel: run with --%s %s. The transcript of this run is unaffected.\n"

	// voiceRealtimeClientNote is the honest line for the REMOTE shape. A
	// --client run posts its audio to a transcriber elsewhere, so whatever
	// interim text exists is produced on the far side: this process cannot print
	// a partial it never receives, and the engine it would name is not even its
	// own (the server's channels decide that). Saying so is the whole of the
	// honest answer here (Rule 15).
	voiceRealtimeClientNote = "[voice] --%s: this is a %s run — the audio is posted to the transcriber named by --%s, so any interim text is produced on THAT host and never reaches this process. Nothing is lost: the transcript still comes back. For a realtime display, run the local shapes (a bare %s, or an audio file) on the host with the %s engine.\n"
)

// voiceRealtime answers whether this run was asked for the realtime display.
//
// It is a METHOD for the same reason voiceVerbose is one (Rule 2/14): the
// printers ask the question through options, never through a raw field read, so
// a nil options value means "not asked" rather than a crash — the helper entry
// points and the seam tests build options of their own.
func (o *voiceOptions) voiceRealtime() bool { return o != nil && o.realtime }

// voicePrintPartials is the ONE gate of the per-partial line, and it is the
// fold of the two channels that can ask for it (Rule 2/14):
//
//	--verbose   the DEBUG SINK, which carries the partial stream plus the
//	            channel's counters, the plumbing note and the live shape note
//	            (task 01a13500 made that stream opt-in because it buried the
//	            answer; this does not undo that decision, it names a second,
//	            narrower way to see the same lines).
//	--realtime  the operator's own request for exactly those lines and nothing
//	            else.
//
// A call site asks THIS, never `voiceVerbose() || voiceRealtime()`, so the
// precedence has one home: two spellings of a fold is how a printer ends up
// printing for one channel and not the other.
func (o *voiceOptions) voicePrintPartials() bool {
	return o.voiceVerbose() || o.voiceRealtime()
}

// voiceRealtimeHasInterim reports whether THIS run's arm prints interim text.
//
// THE PREDICATE IS THE ENGINE, because the partial stream is wired to the v2
// channel's arms and to nothing else: the microphone arm
// (cli_voice_stream_mic.go) and the file-fed first-text arm
// (cli_voice_first_text.go) both hand voiceReportStreamPartial to
// runVoiceLiveSession. The batch engine has no channel at all (it receives the
// finished recording), the local engine runs a file through a GGUF runtime, and
// the v3 SAUC arm that voice_input reaches is the FILE-semantics one, whose
// answer is its only text (cli_voice_sauc.go: the default endpoint is
// bigmodel_nostream). A predicate that guessed instead — "it is a cloud engine,
// so it streams" — is exactly the silent no-op the honest line exists to
// prevent.
func voiceRealtimeHasInterim(opts *voiceOptions) bool {
	return voiceResolveEngine(opts) == config.AudioEngineStreaming
}

// voiceReportRealtimeNoInterim prints the honest sentence when the run was
// asked for a realtime display it cannot have, and prints nothing at all
// otherwise — so a default run (no flag) is byte-for-byte what it was.
//
// IT IS PRINTED BEFORE THE WORK, not after: the operator learns that nothing
// will appear WHILE they are speaking, which is the only moment the news is
// useful.
func voiceReportRealtimeNoInterim(opts *voiceOptions) {
	if !opts.voiceRealtime() || voiceRealtimeHasInterim(opts) {
		return
	}
	fmt.Fprintf(os.Stderr, voiceRealtimeNoInterimNote, voiceFlagRealtime,
		voiceResolveEngine(opts), config.AudioEngineStreaming,
		voiceFlagEngine, config.AudioEngineStreaming)
}

// voiceReportRealtimeClientNote prints the remote shape's honest answer, and
// nothing when the flag was not given.
func voiceReportRealtimeClientNote(opts *voiceOptions) {
	if !opts.voiceRealtime() {
		return
	}
	fmt.Fprintf(os.Stderr, voiceRealtimeClientNote, voiceFlagRealtime,
		voiceClientShapeName, voiceFlagServerURL, voiceCLIVerb, config.AudioEngineStreaming)
}

// voiceRealtimeCaptureStartNote announces the capture that the progress lines
// belong to, and only when they will actually be printed (the same predicate as
// the honest line: this is the path with no partials to show).
func voiceRealtimeCaptureStartNote(opts *voiceOptions) {
	if !opts.voiceRealtime() || voiceRealtimeHasInterim(opts) {
		return
	}
	fmt.Fprintf(os.Stderr, voiceRealtimeCaptureStart, voiceFlagRealtime,
		opts.seconds, voiceRealtimeProgressInterval)
}

// voiceRealtimeProgress prints ONE progress line for a capture that is
// `elapsed` seconds into a `total`-second bound.
//
// The percentage is derived from the TWO figures printed beside it, so the line
// cannot state a share the counts contradict; the total comes from the bound the
// recorder is actually stopped by, never from a second reading of the clock.
func voiceRealtimeProgress(elapsedSeconds, totalSeconds int) {
	share := 0.0
	if totalSeconds > 0 {
		share = 100 * float64(elapsedSeconds) / float64(totalSeconds)
	}
	fmt.Fprintf(os.Stderr, voiceRealtimeProgressTick, elapsedSeconds, totalSeconds, share)
}

// voiceCaptureWithRealtimeProgress runs a capture and, when the realtime display
// was asked for on a run with no interim text, prints a progress line every
// voiceRealtimeProgressInterval while it runs.
//
// IT IS A WRAPPER AROUND THE EXISTING CAPTURE, never a second capture path
// (Rule 3/14): fn is voiceRecord, unchanged, and this function only observes
// the clock. The ticker is stopped and joined before returning, so no line can
// appear after the transcript, and a run without the flag takes the direct call
// with no goroutine at all — the default path's behaviour is not merely
// unchanged, its code is not entered.
func voiceCaptureWithRealtimeProgress(opts *voiceOptions, fn func() ([]byte, error)) ([]byte, error) {
	if !opts.voiceRealtime() || voiceRealtimeHasInterim(opts) {
		return fn()
	}
	total := opts.seconds
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(voiceRealtimeProgressInterval)
		defer ticker.Stop()
		for elapsed := 1; ; elapsed++ {
			select {
			case <-done:
				return
			case <-ticker.C:
				// A capture longer than its bound is a device running slow: the
				// line states the audio captured, never more than the bound it
				// was stopped by, so the percentage cannot exceed the whole.
				shown := elapsed * int(voiceRealtimeProgressInterval.Seconds())
				if shown > total {
					shown = total
				}
				voiceRealtimeProgress(shown, total)
			}
		}
	}()
	pcm, err := fn()
	close(done)
	<-stopped
	return pcm, err
}
