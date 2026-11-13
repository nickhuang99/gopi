package main

// voice_stream_runtime.go — ONE runner for a doubao-stream LIVE utterance
// (task 01a121fc, deliverable A).
//
// THE THREE COPIES THIS REPLACES. Three shapes fed the v2 real-time channel, and
// each one carried its own transcription of the SAME sequence — open the session
// with the resolved credential and endpoint, build the clock BEFORE the session
// (the partials start arriving on the reader goroutine the moment it is open),
// start the production feeder over a staged stream, wait for whichever end
// applies to that shape (the operator's finish key, the capture running out, or
// a budget), close the feeder, take the audio still in hand and send it with the
// end-of-stream flag, prefer the feeder's own send failure over the finish
// failure, and hand back the figures:
//
//	chat_voice_live.go        Live()                        (finish key / capture ended)
//	cli_voice_first_text.go   voiceStreamFirstText()        (the file runs out / a budget)
//	cli_voice_stream_mic.go   voiceMicStreamFirstText()     (the capture ends / a budget)
//
// They agreed because they were copies, and the copies had already drifted: the
// file arm built its staged stream by hand (`&voiceStagedStream{source: src,
// chunks: src.Chunks()}`) instead of going through newVoiceStagedStream, so it
// carried no sourceOrigin, no device and no source verdict — a second
// construction of the stream that the other two do not use.
//
// WHAT IS SHARED HERE, and what deliberately is not:
//
//	shared     the session's open, the clock's construction order, the feeder's
//	           lifetime, the end-of-utterance wait, the in-hand flush, Finish,
//	           the send-failure-before-finish-failure precedence
//	not shared WHAT A PARTIAL MEANS. The chat arm draws the text in the editing
//	           line and drops drafts below voiceLiveDraftMinRunes; the two tool
//	           arms print the partial on stderr and time the first one. That is a
//	           property of the SURFACE, so it is a callback (OnPartial) rather
//	           than something this file decides.
//	not shared THE MESSAGES (voiceReportStreamStart / Summary / Partial) and the
//	           report renderings, which name different sources and different
//	           figures for the same run.
//
// THE BATCH ARM IS NOT MERGED, and the reason is mechanical rather than a
// preference: doubaostream.Transcribe walks the whole audio buffer against a
// CLIENT-OWNED clock (`time.Until(audioStart + n*chunkDur)` — client.go's send
// loop), because a file is audio the service has not "heard" yet and sending it
// early bills a stream longer than the audio; a live session has NO pacing loop
// at all (session.go's header: "a LIVE microphone IS the clock"). The two are
// the two sides of the pacing split, so what they share is the packet geometry
// (PacketAssembler) and the protocol, and what this runner owns — the wait for a
// human to stop speaking, the in-hand flush, the end-of-stream flag — has no
// meaning in Transcribe, which is a bounded call with an audio file in hand.

import (
	"context"
	"errors"
	"strings"
	"time"
)

// voiceLiveSession is the ONE thing this runner needs of a streaming session: the
// audio, the end of the utterance, and the run's own counters.
//
// IT IS AN INTERFACE for the same reason voicePacketSayer is (chat_voice_live.go):
// the utterance's control flow — which end fired, what was flushed, which failure
// is reported first — is the part with an obligation (nothing spoken may be
// dropped, and the first failure must be the one named), and an obligation with a
// network and a credential in front of it is an obligation nobody tests.
//
// IT IS ALSO THE ENGINE SEAM (task 01a13303): the v2 channel (*doubaostream.Session)
// and the v3 SAUC channel (*doubaosauc.Session) are two PROTOCOLS that do the same
// work, and the runner above this interface — the feeder, the clock, the
// end-of-utterance wait, the in-hand flush, the finish key — is the same work
// whichever one is behind it. What each engine answers in ITS OWN vocabulary (a
// result, a partial) is translated by that engine's own adapter
// (voiceLiveBackendFor), so no engine's types leak into this runner and the runner
// is not copied per engine (Rule 3/13/14).
type voiceLiveSession interface {
	// Say sends one packet. The feeder calls it; a refusal after the end was sent
	// is a race, not a failure (see voiceLiveFeeder.absorb). A refusal that MEANS
	// "the utterance was already finished" is named by the engine's own sentinel,
	// which is why voiceLiveRaceRefusal exists.
	//
	// THE SMELL IS GONE: `pcm` says what the bytes ARE — 16 kHz mono PCM16 — where
	// the v2 interface's `pcm` argument said it only at that one call site's
	// comment (Rule 1: a name carries the meaning).
	Say(pcm []byte) error
	// Finish sends the audio still in hand as the LAST packet — the end-of-stream
	// flag — and returns what the service produced.
	Finish(tail []byte) (*voiceLiveResult, error)
	// PartialCount is how many partials arrived (the chat arm reports it).
	PartialCount() int
	// FinishTimedOut reports that the final answer did not arrive within the
	// session's own bound (the chat arm reports it; the tool arms have their own
	// budget).
	FinishTimedOut() bool
	// Close releases the session WITHOUT ending the utterance (the abort path).
	// The runner closes on every exit path, so an engine whose session could not
	// be closed through this seam would leak a socket per utterance.
	Close() error
}

// voiceLiveResult is what ONE session produced, in the runner's own vocabulary:
// the transcript, the vendor's log id, and the two figures the reports quote.
//
// IT EXISTS SO THE RUNNER NEEDS ONE RESULT TYPE, not one per engine, and it is
// SPARSE ON PURPOSE: it carries what the runner and the reports actually read
// rather than re-exporting two engine result structs field by field (a
// re-export would have to be updated whenever either engine grows a field, and
// nothing here would use it).
type voiceLiveResult struct {
	// Text is the transcript (each engine's own final text, verbatim).
	Text string
	// LogID / ReqID identify the request: the vendor's own log id (the ONE
	// identifier its support asks for) and the caller's request id. Both are
	// reported by both channels.
	LogID string
	ReqID string
	// AudioMS is how much audio was sent, in milliseconds. Both engines compute
	// it from the same protocol geometry: bytes of 16 kHz mono PCM16.
	AudioMS int
	// PartialCount is how many partials the channel produced, and FirstPartialAt
	// is when the first one arrived (0 when none did) — the two figures the run
	// summaries are made of.
	PartialCount   int
	FirstPartialAt time.Duration
	// ServiceDurationMS is the duration the SERVICE reported, when a response
	// carried one. MEASURED: the v2 channel answers every packet with duration 0,
	// and the v3 channel's responses carry an audio_info duration — so this is 0
	// whenever the channel did not state one, and the summary prints it only when
	// it is non-zero rather than reporting a 0 that reads as a contradiction.
	ServiceDurationMS int
	// Elapsed is the wall time of the whole exchange.
	Elapsed time.Duration
}

// voiceLivePartial is one partial transcript in the runner's own vocabulary: the
// text known SO FAR.
//
// THE TEXT REPLACES rather than extends on BOTH channels — the v2 service sends
// its latest full text and so does the v3 one (measured 2026-10-13, session.go's
// header) — so this single field is the whole of what a surface needs to draw.
type voiceLivePartial struct {
	// Index counts from 1 in arrival order, on both channels.
	Index int
	// Text is the transcript so far.
	Text string
	// Definite is the engine's own verdict that this segment is closed (false
	// whenever the engine stated none — the honest zero rather than an
	// assumption).
	Definite bool
	// At is how long after the utterance's start this partial arrived, measured
	// from the same origin on both channels.
	At time.Duration
}

// voiceLiveSessionOpener builds an ENGINE's live session: the one call that
// differs between the two channels, in the engine's own options type. It is a
// function type rather than a method on an interface so an engine's adapter can
// close over ITS OWN package's types without this file importing them.
type voiceLiveSessionOpener func(ctx context.Context, opts any) (voiceLiveSession, error)

// voiceLiveRaceRefusal reports whether a Say refusal means "the utterance was
// already finalised" rather than "the transport broke".
//
// IT EXISTS BECAUSE THE TWO CHANNELS SPELL THAT RACE WITH THEIR OWN SENTINELS
// (doubaostream.ErrSessionFinished / doubaosauc.ErrSessionFinished, in different
// packages), and the feeder's decision — drop the packet, report nothing (see
// voiceLiveFeeder.absorb) — is the same decision on both. Each adapter names its
// own sentinels here, so the runner never imports either package's error
// vocabulary (Rule 3).
type voiceLiveRaceRefusal func(error) bool

// voiceLiveBackend is ONE engine's live session: how to OPEN it, and how to read
// the two engine-specific questions the runner asks (is this refusal a race, and
// what did this partial's text become).
//
// THE SEAM IS HERE RATHER THAN INSIDE THE SESSION, because the answer to both
// questions is a fact about the ENGINE's types, and a session that answered them
// itself would be an engine pretending to be the runner. Everything ABOVE this
// struct — the feeder, the clock, the wait, the flush, the failure precedence —
// is written once and shared.
type voiceLiveBackend struct {
	// Open builds this engine's session over the run's own configuration.
	Open voiceLiveSessionOpener
	// IsRaceRefusal classifies a Say refusal (see voiceLiveRaceRefusal).
	IsRaceRefusal voiceLiveRaceRefusal
	// ConfigurePartial returns opts with THIS engine's partial callback
	// installed, translating the engine's own partial into the runner's
	// vocabulary on the way (a nil field means the engine reports no partials,
	// which no engine currently does — and which would lose nothing but the
	// progress anyway).
	ConfigurePartial func(opts any, onPartial func(voiceLivePartial)) any
}

// voiceLiveRunConfig is ONE utterance's input: the session's own configuration,
// what a partial should become, and what ends the utterance.
type voiceLiveRunConfig struct {
	// Backend is the ENGINE this utterance runs on (see voiceLiveBackend). It is
	// required: a run with no backend has no session to open, and the runner says
	// so by name rather than opening nothing and reporting an empty transcript.
	Backend voiceLiveBackend
	// Options is the session's configuration IN THE ENGINE'S OWN TERMS
	// (doubaostream.Options for v2, doubaosauc.Options for v3) — credential,
	// endpoint and the audio geometry. It is opaque here on purpose: the runner
	// does not read one field of it, and a runner that did would be the second
	// place each engine's configuration is understood (Rule 2/3).
	Options any
	// OnPartial is what this SURFACE does with a partial: draw it in the line, or
	// print it. nil is "do nothing but time it", which is what a run that only
	// wants the figures asks for. It is REPLACED (see runVoiceLiveSession) so the
	// measurement cannot be lost by a caller that forgets to stamp the clock.
	OnPartial func(voiceLivePartial)
	// Stop ends the utterance when it is closed (the chat arm's finish key). It is
	// nil for the tool arms, whose utterance ends when the audio does or when the
	// budget expires.
	Stop <-chan struct{}
	// Budget is the longest this runner waits for the utterance to end on its own
	// (a file that the service never answers about, a microphone that keeps
	// delivering). 0 = no bound, which is only correct for a shape whose end is a
	// human's key.
	Budget time.Duration
}

// errVoiceLiveNoBackend is the named refusal for a run that named no engine.
var errVoiceLiveNoBackend = errors.New("the live utterance has no engine backend (voiceLiveRunConfig.Backend was not set)")

// voiceLiveRun is what ONE utterance produced, including WHY it ended: the two
// endings are the same event to the service (the end-of-stream flag goes out
// either way) and are NOT the same event to a caller — a capture that has ended
// cannot record the next sentence.
type voiceLiveRun struct {
	// Text is the final transcript, trimmed.
	Text string
	// Result is the session's own engine-neutral result, nil when the session
	// never opened.
	Result *voiceLiveResult
	// Tail is the audio still in hand that went up with the end-of-stream flag.
	// It is reported rather than only sent, because a caller that keeps or counts
	// the audio must know that these bytes were not in a packet.
	Tail []byte
	// Clock is the utterance's ONE timeline (first packet sent → first partial),
	// built before the session so it cannot miss the earliest drafts.
	Clock *voiceLiveClock
	// SendErr is the feeder's own first send failure, and FinishErr the session's
	// finish failure. They are two fields rather than one error because the
	// PREFERENCE between them is the contract (see error()).
	SendErr   error
	FinishErr error
	// CaptureEnded reports that the audio ran out rather than the operator saying
	// they were done. See voiceLiveReport.CaptureEnded for the measured incident
	// this distinction exists for.
	CaptureEnded bool
	// PartialCount is how many drafts the recogniser produced, and TimedOut is the
	// session's own verdict on its final-answer wait. Both are carried out so the
	// two callers that report them (the chat arm's utterance report and the tool
	// arms) read the SAME counters rather than re-asking the session.
	PartialCount int
	TimedOut     bool
	// BudgetExpired reports that the budget ran out while the capture was still
	// delivering: the utterance was CUT, and the caller's own report says so.
	BudgetExpired bool
}

// error is WHY the utterance did not produce a transcript, in the order the three
// shapes already reported it: a SEND failure is named ahead of a FINISH failure,
// because the transport broke while feeding and "the session could not be
// finished" is then a consequence rather than the cause (Rule 15).
func (r voiceLiveRun) error() error {
	if r.SendErr != nil {
		return r.SendErr
	}
	return r.FinishErr
}

// runVoiceLiveSession runs ONE utterance over a stream that is already open.
//
// THE STREAM IS THE CALLER'S, and that is deliberate: the chat arm opens it
// through the recorder's seam (so a device and a file take the same path), the
// file arm over a file source, and the microphone arm over a sink wrapper that
// counts the bytes — three legitimate sources for one runner, none of which the
// runner has an opinion about.
//
// THE CALLER OWNS THE STREAM'S LIFETIME (its Close), exactly as it did before:
// this function opens and closes the SESSION, never the capture.
func runVoiceLiveSession(stream *voiceStagedStream, cfg voiceLiveRunConfig) voiceLiveRun {
	// THE CLOCK IS BUILT BEFORE THE SESSION, because the partials arrive on the
	// reader goroutine the moment the session is open: a stamp taken by something
	// created afterwards would miss the first drafts — the very ones the latency
	// number is about.
	run := voiceLiveRun{Clock: &voiceLiveClock{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// THE BACKEND IS CHECKED BEFORE THE SESSION IS OPENED: a run with no engine
	// has nothing to open, and saying so here is a named refusal instead of a nil
	// call (Rule 15).
	if cfg.Backend.Open == nil {
		run.FinishErr = errVoiceLiveNoBackend
		return run
	}
	// THE PARTIAL CALLBACK IS INSTALLED BY THE ENGINE'S OWN ADAPTER, and the two
	// halves of it are split deliberately: THE STAMP COMES FIRST, before anything
	// the surface does with the text, because a print (or a repaint) that happens
	// to be slow must not become part of the latency it reports — and it is
	// stamped HERE, in the ONE place every engine's partials pass through, so no
	// caller can forget it and no caller can stamp it twice.
	opts := cfg.Options
	if cfg.Backend.ConfigurePartial != nil {
		opts = cfg.Backend.ConfigurePartial(opts, func(p voiceLivePartial) {
			run.Clock.notePartial()
			if cfg.OnPartial != nil {
				cfg.OnPartial(p)
			}
		})
	}
	sess, err := cfg.Backend.Open(ctx, opts)
	if err != nil {
		run.FinishErr = err
		return run
	}
	// THE SESSION IS CLOSED ON EVERY EXIT PATH, through the package's own
	// function, so the end-of-stream flag is what the service sees rather than a
	// dropped socket.
	defer func() { _ = sess.Close() }() //stylegate:ignore Close is idempotent and already reports through run.FinishErr; a second report here would replace the utterance's own verdict
	return runVoiceLiveUtterance(stream, sess, cfg, run)
}

// runVoiceLiveUtterance is the utterance itself, over a session that is open:
// feed, wait for the end, flush what is in hand, finish. It is separated from the
// open above so the control flow can be exercised without a network, and it is
// the WHOLE of the shape all three arms used to spell out.
func runVoiceLiveUtterance(stream *voiceStagedStream, sess voiceLiveSession, cfg voiceLiveRunConfig, run voiceLiveRun) voiceLiveRun {
	feed := newVoiceLiveFeeder(stream, sess, run.Clock, cfg.Backend.IsRaceRefusal)
	stopFeed := make(chan struct{})
	go feed.run(stopFeed)

	// WHICH END FIRED, and the three cases are exhaustive:
	//
	//	Stop != nil   the operator's key, or the capture running out under it
	//	Budget > 0    the capture running out, or the budget
	//	otherwise     the capture, plainly (a file: there is no key and no budget)
	var tail []byte
	switch {
	case cfg.Stop != nil:
		select {
		case <-cfg.Stop:
		case tail = <-feed.ended:
			run.CaptureEnded = true
		}
	case cfg.Budget > 0:
		select {
		case tail = <-feed.ended:
			run.CaptureEnded = true
		case <-time.After(cfg.Budget):
			run.BudgetExpired = true
		}
	default:
		tail = <-feed.ended
		run.CaptureEnded = true
	}
	close(stopFeed)
	// THE AUDIO IN HAND GOES UP WITH THE END-OF-STREAM FLAG, so nothing the
	// operator spoke is dropped between the last packet and the flag — the same
	// losslessness the window arm's carry guarantees, at packet scale.
	if pending := feed.takePending(); len(pending) > 0 {
		tail = append(tail, pending...)
	}
	run.Tail = tail

	res, err := sess.Finish(tail)
	run.Result = res
	run.SendErr = feed.sendFailure()
	run.FinishErr = err
	if res != nil {
		run.Text = strings.TrimSpace(res.Text)
	}
	run.PartialCount = sess.PartialCount()
	run.TimedOut = sess.FinishTimedOut()
	return run
}
