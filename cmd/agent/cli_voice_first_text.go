package main

// cli_voice_first_text.go — WHEN THE FIRST TEXT ARRIVED, measured from the start
// of the TOOL rather than from the start of the engine.
//
// THE GAP THIS CLOSES. `gopi tool voice_input` printed "[voice] transcribed by
// doubao in 2.8s" — and 2.8 s is the ENGINE's own duration, i.e. the time the
// vendor (or the local runtime) says it spent. It says nothing about what the
// operator waited through, which is the whole question when the transcript is
// late. The bare capture adds a fixed recording window in front of that engine
// call, and the three engines add three different amounts of channel latency:
//
//	recording (batch, file upload + poll)   window + upload + poll
//	streaming (v2 realtime, 200ms packets)  window + channel latency
//	local (Fun-ASR on this machine)         window + local inference
//
// So every report carries the TOTAL — the instant the first text existed,
// relative to the tool's own start — and the SPLIT that explains it: how much of
// that total was the capture window, and how much was the engine/channel.
//
// WHAT "FIRST TEXT" MEANS, and it is not the same event in the two cloud
// channels:
//
//   - BATCH (`recording`) returns ONE answer for the whole recording, so the
//     first text IS the answer and its instant is the instant the engine
//     returned.
//   - STREAMING (`doubao-stream`) produces PARTIALS while the audio is still
//     going out, so the first text is the FIRST PARTIAL — the channel's own
//     real-time latency, which is the number this whole comparison is about. The
//     final transcript's instant is reported beside it, because "the first word
//     came at 0.9s and the sentence closed at 12.4s" is two facts.
//
// ZERO NEW DEPENDENCIES (Rule 35): the timing rides on the run the operator
// already asked for, on stderr, next to the lines that were there before. There
// is no flag to remember, no file to look at afterwards, and nothing scheduled.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaostream"
)

const (
	// voiceFirstTextReportLabel is the ONE spelling of the line's prefix. Two
	// shapes print the report (the bare capture and the file run) and a reader
	// who has seen one run must recognise the next, so the prefix is a constant
	// rather than a string typed at each call site (Rule 1b/14).
	voiceFirstTextReportLabel = "[voice] first text"

	// The SPLIT's own term labels. Named rather than typed at each rendering site
	// because a run's line is read against another run's: the recording path and
	// the microphone-streaming path are compared BY THESE WORDS, and the streaming
	// arm's property — that no part of its total is a recording — is asserted by
	// the absence of the first of them (Rule 1b/13: one spelling, and the constant
	// is what the test reads).
	voiceFirstTextWindowTermLabel = "capture window"
	voiceFirstTextEngineTermLabel = "engine/channel"
	// The two labels' RENDERED forms: the term's words plus the figure. They are
	// derived from the labels so a reader (and a test) can ask about the WORD and
	// the run prints the whole term, without two spellings of the same phrase.
	voiceFirstTextWindowTermFormat = voiceFirstTextWindowTermLabel + " %s"
	voiceFirstTextEngineTermFormat = voiceFirstTextEngineTermLabel + " %s"
	// voiceFirstTextNoTextWaitLabel is the same split for a run that produced NO
	// text: the wait happened even though no first text did, and the sentence says
	// which wait it was.
	voiceFirstTextNoTextWaitLabel = " (the run waited %s anyway: " + voiceFirstTextWindowTermFormat + " + engine %s)"

	// voiceFirstTextSubSecondCutoffMS is where the report switches from a plain
	// second figure to milliseconds. MEASURED: the streaming channel's
	// first-partial latency is ~0.3-1.5 s and the local engine's is under a
	// second on a warm run — "in 0.3s" would be a rounding away of the very
	// number this report exists to expose, and "300ms of channel" next to a
	// 12.0s total is exactly the split the operator is asking about.
	voiceFirstTextSubSecondCutoffMS = 2000

	// voiceFirstTextOverheadFloorMS is how much of a remainder is worth naming.
	// Below it the "more than the split" figure is measurement noise (the two
	// halves are read a call apart), and printing "1ms more than the split" would
	// teach a reader to ignore the sentence that exists for the runs where the
	// remainder IS the story.
	voiceFirstTextOverheadFloorMS = 100

	// voiceFirstTextBudgetMS is the budget a FILE fed to the streaming channel
	// gets before the run stops waiting for a first text.
	//
	// WHY THERE IS A BOUND AT ALL. The batch channel's call returns when the job
	// finishes (its own client bounds the poll). The streaming channel has no
	// single-answer semantics to lean on — a file can be sent and the service may
	// simply go quiet (silence in the audio, a dropped channel) — so a run that
	// waited for a first text with no bound would hang the tool with no report at
	// all, which is strictly worse than a report saying that none arrived. It is
	// deliberately the same order as the batch poll budget, so neither channel is
	// given a cheaper excuse than the other.
	voiceFirstTextBudgetMS = 60000
)

// voiceRunClock is ONE run's timeline, started at the earliest moment this
// process knows the tool began. It is the origin EVERY figure in the report is
// measured from, so "the operator waited 18.5s" and "the engine was 2.8s of it"
// are two subtractions of one clock instead of two clocks that happen to be
// close (Rule 2: one source of truth).
type voiceRunClock struct {
	start time.Time
}

// newVoiceRunClock starts the run's clock at the given instant. The caller passes
// voiceRunOrigin() — the stashed process-start stamp — so the clock means "since
// the tool started" and not "since some line of code ran".
func newVoiceRunClock(origin time.Time) *voiceRunClock {
	if origin.IsZero() {
		origin = time.Now()
	}
	return &voiceRunClock{start: origin}
}

// since is how long after the run's start an event happened.
func (c *voiceRunClock) since(t time.Time) time.Duration {
	if c == nil || c.start.IsZero() || t.IsZero() {
		return 0
	}
	return t.Sub(c.start)
}

// elapsed is how long the run has been going.
func (c *voiceRunClock) elapsed() time.Duration {
	if c == nil {
		return 0
	}
	return time.Since(c.start)
}

// The process-start stamp. The process began before main() ran, and the
// difference is not free: the flags are parsed, the settings document is loaded
// and the deployment is resolved all before the capture path is reached, so a
// clock started at the top of the capture measures strictly LESS than what the
// operator waited. The stamp is taken in init() — the earliest point this
// process can take it — and stashed here.
var voiceProcessStart time.Time

func init() { voiceProcessStart = time.Now() }

// voiceRunOrigin is the origin every run clock is built from: the process's own
// start, so "the first text arrived at t+X" is measured from the moment the tool
// was invoked rather than from the moment a function happened to run.
func voiceRunOrigin() time.Time { return voiceProcessStart }

// voiceFirstTextReport is what ONE run observed about its own first text. The
// zero value means "nothing was measured yet", and it is reported as such —
// never printed as 0 s (Rule 15).
type voiceFirstTextReport struct {
	// FirstTextMS is WHEN the first text existed, relative to the run's start:
	// the first partial's arrival on the streaming channel, the engine's return
	// on the batch and local ones.
	FirstTextMS int
	// WindowMS is how much of FirstTextMS was the CAPTURE WINDOW — the fixed
	// recording the bare shape takes before any engine sees a byte. It is 0 for
	// a run that named a file: nothing was captured, so no part of the total
	// belongs to a recording.
	WindowMS int
	// EngineMS is how much of FirstTextMS was the engine/channel: the batch
	// round trip, the streaming channel's latency, or the local inference.
	EngineMS int
	// FinalMS is when the FINAL transcript existed. It differs from FirstTextMS
	// only on the streaming channel, where partials arrive while the audio is
	// still being sent; on the batch and local engines the final text IS the
	// first text and the two are equal.
	FinalMS int
	// Streamed says this run's first text came from a PARTIAL, i.e. the figure is
	// the channel's real-time latency rather than a whole-answer duration. It is
	// a fact about the measurement, so the report says it.
	Streamed bool
	// Missing is WHY the run produced no first text (a channel that went quiet, a
	// recording that carried no speech). It is a sentence and is never empty when
	// no figure was measured: the report states the absence rather than dressing
	// it as a duration (Rule 15).
	Missing string
}

// measured reports whether this run produced a real first-text figure.
func (r voiceFirstTextReport) measured() bool { return r.FirstTextMS > 0 }

// voiceSplitPartsMS renders a millisecond figure in the unit the measurement
// deserves: milliseconds below voiceFirstTextSubSecondCutoffMS, seconds above.
// One spelling, used by every figure in the report line.
func voiceSplitPartsMS(ms int) string {
	if ms < voiceFirstTextSubSecondCutoffMS {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// voiceFirstTextReportLine renders the ONE line the report prints. The engine is
// named, the total is stated first (that is the number the operator asked for),
// and the split follows in parentheses so the total is never read without the
// explanation of what it is made of.
//
// A run that measured nothing gets the honest sentence instead: which engine, and
// why there is no figure. Printing "first text at 0.0s" for a channel that never
// answered would be the exact class of silent failure Rule 15 names.
func voiceFirstTextReportLine(opts *voiceOptions, r voiceFirstTextReport) string {
	engine := voiceEngineLabel(opts)
	if !r.measured() || r.Missing != "" {
		why := r.Missing
		if why == "" {
			why = "no first text was observed before the run ended"
		}
		line := fmt.Sprintf("%s: none from engine %s — %s", voiceFirstTextReportLabel, engine, why)
		// THE WAIT IS STILL REPORTED, because it still happened. A recording that
		// carried no speech costs the operator the capture window plus the engine's
		// round trip all the same, and that figure is exactly what this report is
		// for — but it is NOT labelled a first text, because none existed (Rule 15:
		// the absence is stated, not dressed as a duration).
		if r.measured() {
			line += fmt.Sprintf(voiceFirstTextNoTextWaitLabel,
				voiceSplitPartsMS(r.FirstTextMS), voiceSplitPartsMS(r.WindowMS),
				voiceSplitPartsMS(r.EngineMS))
		}
		return line
	}
	head := fmt.Sprintf("at %s", voiceSplitPartsMS(r.FirstTextMS))
	if r.Streamed {
		head += " (first partial)"
	}
	// THE SPLIT IS COMPLETE OR IT SAYS SO. The total is not "window + engine":
	// MEASURED, a file's first text at 2.6s on a channel whose own latency was
	// 152ms is made of 2.4s of everything else — the process's startup, the
	// settings load, and the vendor channel's HANDSHAKE, paid once per session and
	// invisible in the per-packet latency the channel reports. Leaving that out
	// would be arithmetic pretending to be a measurement, so it is named.
	//
	// THE TERMS ADD UP TO THE TOTAL, and the first two are what the run OBSERVED;
	// the third is the remainder that closes the sum. When nothing is left over the
	// chain stops at the two observed figures rather than printing "0ms of
	// startup".
	terms := []string{}
	if r.WindowMS > 0 {
		terms = append(terms, fmt.Sprintf(voiceFirstTextWindowTermFormat, voiceSplitPartsMS(r.WindowMS)))
	}
	terms = append(terms, fmt.Sprintf(voiceFirstTextEngineTermFormat, voiceSplitPartsMS(r.EngineMS)))
	if rest := r.FirstTextMS - r.WindowMS - r.EngineMS; rest > voiceFirstTextOverheadFloorMS {
		terms = append(terms, fmt.Sprintf("%s of startup + channel handshake",
			voiceSplitPartsMS(rest)))
	}
	line := fmt.Sprintf("%s %s (engine %s) = %s", voiceFirstTextReportLabel, head, engine,
		strings.Join(terms, " + "))
	if r.Streamed && r.FinalMS > r.FirstTextMS+voiceFirstTextOverheadFloorMS {
		line += fmt.Sprintf("; final transcript at %s", voiceSplitPartsMS(r.FinalMS))
	}
	return line
}

// voiceReportFirstText prints the line. It goes to STDERR like every other
// progress line, so stdout stays exactly the transcript — the contract every
// shape of this command keeps.
func voiceReportFirstText(opts *voiceOptions, r voiceFirstTextReport) {
	fmt.Fprintf(os.Stderr, "%s\n", voiceFirstTextReportLine(opts, r))
}

// ── the streaming channel's first text, over a FILE ─────────────────────────

// voiceStreamFirstText runs ONE streaming pass over an audio file, feeding it
// through the SHARED live feeder (chat_voice_live.go) rather than a second copy
// of the same arithmetic, and reports when the first text and the final
// transcript arrived.
//
// IT EXISTS BECAUSE A FILE IS A CAPTURE THAT HAS ALREADY HAPPENED. `doubao-stream`
// on an audio file was reachable only through the batch client's `Transcribe`,
// whose shape is "send everything, then wait for one answer": the real-time
// channel's whole property — text while the audio is still going out — was
// unreachable from the file surface, so it could not be measured there, and the
// operator's question ("how much of the wait is the fixed recording window versus
// the channel?") had no file-shaped answer to compare against. This function asks
// a FILE for the same thing the microphone surface asks it for, so the two
// numbers are comparable instead of being two different experiments.
//
// THE FEEDER AND THE CLOCK ARE THE PRODUCTION ONES (newVoiceLiveFeeder over a
// staged stream of the file's chunks; voiceLiveClock's first-packet →
// first-partial subtraction). Nothing about the packet geometry, the
// end-of-stream discipline or the partial handling is re-derived here: a second
// implementation of any of them would make the comparison a comparison of two
// implementations rather than of two channels.
func voiceStreamFirstText(opts *voiceOptions, audioFile string, runClock *voiceRunClock) (string, voiceFirstTextReport, error) {
	var report voiceFirstTextReport
	missingBudget := fmt.Sprintf("the %s channel produced no partial within %s of the audio being sent",
		config.AudioEngineStreaming, voiceSplitPartsMS(voiceFirstTextBudgetMS))

	cred, credPath, err := voiceStreamCredentials(opts)
	if err != nil {
		return "", report, err
	}
	// THE FILE IS THE SOURCE, through the SAME seam the capture path uses: one
	// implementation of "where the samples come from", so this run's audio is the
	// capture path's audio. It never opens a device — which is what makes this a
	// file measurement rather than a second microphone experiment.
	src := voiceFileSourceFor(audioFile, voiceSourceImmediate)
	// THE STREAM IS BUILT BY THE SEAM'S OWN CONSTRUCTOR, not by a literal: the
	// pre-convergence shape assembled `&voiceStagedStream{source: src, chunks:
	// src.Chunks()}` right here, which is the ONE stream in the package that
	// carried no source origin, no device and no source verdict (a second
	// construction of the same type — exactly the drift Rule 13/14 exist to stop).
	stream, err := newVoiceStagedStream(src)
	if err != nil {
		return "", report, err
	}
	// THE FILE IS RELEASED ON EVERY EXIT PATH, through the stream that owns it.
	defer stream.Close()

	endpoint := voiceStreamEffectiveEndpoint(opts)

	// THE SESSION, THE CLOCK, THE FEEDER, THE WAIT AND THE IN-HAND FLUSH ARE ONE
	// IMPLEMENTATION (voice_stream_runtime.go), shared with the microphone arm and
	// with chat `/voice`'s live dictation. What is specific to THIS shape is the
	// budget that ends a file run and what a partial means here (print it — the
	// first one is the figure the whole report is about).
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
		Budget:    time.Duration(voiceFirstTextBudgetMS) * time.Millisecond,
	})
	// The endpoint (always) and the credential's ORIGIN (under --verbose) are
	// stated once, before the audio moves — the same disclosure every other
	// streaming shape prints, through the same function (Rule 2/14).
	voiceReportStreamStart(cred, credPath, doubaostream.DefaultChunkMillis, endpoint, opts.voiceVerbose())
	finalAt := time.Now()
	res := run.Result
	if run.BudgetExpired {
		report.Missing = missingBudget
	}
	if err := run.error(); err != nil {
		return "", report, voiceStreamError(err, res)
	}
	if res == nil {
		// The runner always hands back the session's own result on this path; a
		// nil here would mean a call that never ran, and saying so is better than
		// reading fields off it (Rule 15).
		return "", report, fmt.Errorf("the %s channel produced no result for this file", config.AudioEngineStreaming)
	}
	// What the channel itself observed — how many partials, when the first and
	// last arrived, how much audio went out — through the SAME summary the other
	// streaming shape prints (Rule 2/14: one spelling of these figures).
	voiceReportStreamSummary(res, opts.voiceVerbose())
	report.Streamed = true
	report.EngineMS = run.Clock.firstPartialLatencyMS()
	// THE TWO FIGURES ARE ONE STAMP READ FROM TWO ORIGINS, not two measurements:
	// FirstTextMS is the instant the first text existed measured from the tool's
	// start (the number the operator waited), and EngineMS is the same instant
	// measured from the first packet sent (the channel's own latency). Taking
	// them from one clock is what makes the split add up.
	if wall := run.Clock.firstPartialWall(); !wall.IsZero() {
		report.FirstTextMS = int(runClock.since(wall).Milliseconds())
	}
	// TWO STAMPS, ONE INSTANT, AND THE SMALLER ONE WINS. FirstTextMS is the run's
	// own view of when the text arrived; EngineMS is the channel's view of the
	// same arrival. They are taken on the same clock but read a call apart, so a
	// sub-millisecond ordering can make the run's view look SHORTER than the
	// channel's — and reporting "first text at 212ms: engine/channel 214ms" is a
	// split that does not add up. The channel's figure is the floor: hard evidence
	// cannot precede the run that contains it.
	if report.FirstTextMS < report.EngineMS {
		report.FirstTextMS = report.EngineMS
	}
	report.FinalMS = int(runClock.since(finalAt).Milliseconds())
	if report.EngineMS <= 0 {
		// The honest absence. MEASURED: a file whose audio the service finds
		// speechless produces no partial at all, and a report reading "0ms" there
		// would claim a latency that never happened.
		report.Missing = fmt.Sprintf("%s (%s of audio were sent)", missingBudget,
			voiceSplitPartsMS(res.AudioMS))
	}
	return strings.TrimSpace(res.Text), report, nil
}
