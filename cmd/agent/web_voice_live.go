package main

// web_voice_live.go — the LIVE microphone on the `gopi web` page.
//
// WHAT IT IS. ONE POST that behaves like a conversation, not like a command:
// POST /api/voice/live opens the capture, runs ONE streaming utterance, and
// answers with a text/event-stream body on which every partial the engine
// produces arrives as a `draft` frame WHILE THE OPERATOR IS STILL SPEAKING, and
// the settled sentence arrives as a `final` frame. POST
// /api/voice/live/stop ends the utterance — the same event as the TUI's finish
// key.
//
// WHAT IT REUSES, and the list is the whole point (Rule 13/14 — there is no
// second ASR implementation here, and no second capture policy):
//
//	voiceChatRecorderStaged.Live — the SAME arm `gopi chat`'s /voice runs, reached
//	    through the SAME type assertion chat_voice.go:1199 makes (`.(voiceChatLiveRunner)`),
//	    so the runner, the feeder, the clock, the engine adapters
//	    (voice_live_engines.go) and the end-of-stream discipline are one
//	    implementation for both carriers;
//	voiceLiveEngineFor — the ONE resolution of "which backend, with which session
//	    options" (called inside Live, deliberately not restated here);
//	voiceLiveEngineSelected — the ONE judgement of "does this engine have a live
//	    arm at all", which is what makes the REFUSAL below a named answer instead
//	    of a hang;
//	webVoicePipeline (web_voice.go) — Options/Open: the four-tier device policy,
//	    the liveness probe, the argv and the dependency check are the window
//	    path's, unchanged;
//	webVoiceDraftText — the ONE judgement of what a transcript contributes to the
//	    composer, slash hazard included, so a live draft cannot reach the model as
//	    a local command either;
//	writeSSEHeaders / writeSSEFrame — the ONE framer every server-side event uses.
//
// WHY THE DRAFT IS THE WHOLE TEXT SO FAR, NOT A DELTA. Both live backends publish
// the CURRENT transcript on each partial (v2 doubaostream and v3 doubaosauc
// alike), so a draft REPLACES what came before. The page therefore replaces its
// in-composer draft region instead of appending to it — an append would show
// "你好你好世界" for an utterance of "你好世界".
//
// WHY THIS HANDLER DOES NOT TAKE THE TURN LOCK (s.mu). The window path does
// (web_voice.go, "THE FINISH RUNS UNDER THE TURN LOCK"): its transcription calls
// the shared engine surface a turn streams from. The live arm opens its OWN
// streaming session on its own credentials (voiceLiveEngineFor) and mutates no
// agent state, so charging a whole utterance to the turn lock would queue every
// chat turn behind a microphone for no shared thing — and would make dictating
// and typing mutually exclusive, which is not true of any other carrier.
//
// THE ONE MICROPHONE IS STILL ONE. voiceState.mu (web_voice.go) guards a second
// field, live, so a live utterance and a window recording cannot hold the device
// at the same time; the second caller gets the conflict the window path already
// answers.
//
// THE FALLBACK IS THE POINT OF THE REFUSAL. An engine without a live arm (the
// local/whisper runtimes, a batch file engine) cannot stream, and pretending
// otherwise would be a frozen composer. The refusal is a 501 with the engine's
// name in it, and the page turns it into the EXISTING start/stop path — whose
// tail preservation (chat_voice_recorder_staged's own finish order) is untouched
// by this file.

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/nick/gopi/internal/config"
)

// The route vocabulary (Rule 1: one const block; the page's copies are pinned to
// these by TestVoiceLivePageCarriesTheRoutes).
const (
	webVoiceLivePath     = "/api/voice/live"
	webVoiceLiveStopPath = "/api/voice/live/stop"
	// webVoiceLiveCancelPath is the THIRD ender, and it is the one that DISCARDS:
	// stop ends the utterance and settles the sentence into the composer, cancel
	// ends it and throws the sentence away (the operator's Escape). Two verbs
	// because the two acts are different — a single "end" with a discard flag
	// would put the decision in the page, which is exactly where a lost sentence
	// would come from.
	webVoiceLiveCancelPath = "/api/voice/live/cancel"
)

// The SSE event names this route publishes. Named for the same reason the paths
// are: the page spells them and a page constant is not importable.
const (
	webVoiceLiveEventReady = "ready"
	webVoiceLiveEventDraft = "draft"
	webVoiceLiveEventFinal = "final"
)

// webVoiceLiveSession is ONE live utterance in flight: what the stop route has to
// reach, and what the handler must clear on the way out.
type webVoiceLiveSession struct {
	// finish is closed to end the utterance — the same signal the TUI's finish
	// key closes, so the arm's end-of-stream flag goes out by the same route.
	finish chan struct{}
	// stopOnce makes the three enders (the stop route, the ceiling timer, the
	// client's disconnect) idempotent: closing a channel twice panics, and any two
	// of them may fire in the same millisecond.
	stopOnce sync.Once
	// truncated records that the CEILING ended the utterance rather than the
	// operator (see webVoiceMaxCaptureSeconds). Written under mu.
	mu        sync.Mutex
	truncated bool
	// cancelled records that the utterance was DISCARDED (the page's Escape),
	// so the final frame can say so and the page does not insert what it ended.
	// Written under mu, like truncated.
	cancelled bool
}

// stop ends the utterance. Idempotent.
func (v *webVoiceLiveSession) stop() { v.stopOnce.Do(func() { close(v.finish) }) }

// cancel ends the utterance AND marks its sentence as discarded. Idempotent, and
// it must be callable BEFORE the arm has said anything: the operator's Escape can
// arrive one millisecond after the device opened.
func (v *webVoiceLiveSession) cancel() {
	v.mu.Lock()
	v.cancelled = true
	v.mu.Unlock()
	v.stop()
}

// wasCancelled reports the discard verdict. The handler reads it AFTER the arm
// returned, so the final frame carries the answer rather than the page inferring
// it from which route it called.
func (v *webVoiceLiveSession) wasCancelled() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cancelled
}

// markTruncated records that the ceiling, not the operator, ended this utterance.
func (v *webVoiceLiveSession) markTruncated() {
	v.mu.Lock()
	v.truncated = true
	v.mu.Unlock()
}

// wasTruncated reports the ceiling verdict.
func (v *webVoiceLiveSession) wasTruncated() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.truncated
}

// webVoiceLiveSink is the `gopi web` carrier's voiceLiveSink: the ONE interface
// the live arm publishes into (chat_voice_live.go). A draft is an SSE frame
// flushed before the next one is produced, which is what makes the composer move
// while the operator is still talking; the settle is recorded and written by the
// handler with the run's figures attached, so the final frame carries the report
// the TUI prints.
type webVoiceLiveSink struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	drafts  int
	settled string
	writeOK bool
}

// Draft publishes the text known so far as one `draft` frame.
//
// IT IS CALLED FROM THE SESSION'S READER GOROUTINE (the arm's own OnPartial), so
// the write is guarded: the handler goroutine writes the `final` frame on the
// same ResponseWriter when the arm returns, and two goroutines interleaving
// half-frames would corrupt the stream.
func (s *webVoiceLiveSink) Draft(text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drafts++
	writeSSEFrame(s.w, webVoiceLiveEventDraft, webVoiceLiveJSON(map[string]interface{}{
		"text":     text,
		"draft":    webVoiceDraftText(text),
		"partials": s.drafts,
	}))
	s.flusher.Flush()
}

// Settle takes the finished sentence. It reports whether the text was RECEIVED,
// which is always true here — the carrier is a byte stream with no editing line
// to refuse it, so the arm's own error path ("the transcript could not be
// settled") stays about the TUI, where a settle really can be refused.
func (s *webVoiceLiveSink) Settle(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settled = text
	s.writeOK = true
	return true
}

// webVoiceLiveJSON marshals one frame's body. In-process values only (strings,
// ints, bools), so the marshal cannot fail; the fallback is still an honest
// answer rather than an empty frame (Rule 15).
func webVoiceLiveJSON(v interface{}) []byte {
	payload, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"the frame could not be encoded"}`)
	}
	return payload
}

// webVoiceLiveRefusal is the NAMED refusal for an engine with no live arm. It
// carries the engine's own value, and it names the route the page should take
// instead — a refusal the page can act on, not a bare status code.
func webVoiceLiveRefusal(engine string) string {
	return "engine " + engine + " has no live streaming arm (the live arm is " +
		config.AudioEngineStreaming + " or " + config.AudioEngineSauc +
		") — the page falls back to " + webVoiceStopPath + "'s one-transcript-per-click path"
}

// handleVoiceLive answers POST /api/voice/live: one streaming utterance, draft
// frames as the engine produces them, a final frame at the end.
//
// THE REFUSAL COMES BEFORE ANY BYTE OF THE STREAM, so the page can react to it
// with a status code and a sentence: once the 200 and the headers are written, a
// refusal could only be a frame, and a page that has to parse a frame to learn
// "this engine cannot stream" has to keep two error paths.
func (s *webServer) handleVoiceLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		webVoiceError(w, http.StatusMethodNotAllowed, "method not allowed (POST "+webVoiceLivePath+")")
		return
	}
	if s.shuttingDown {
		webVoiceError(w, http.StatusServiceUnavailable, "server shutting down")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		webVoiceError(w, http.StatusInternalServerError, "this server cannot stream (no http.Flusher on the response writer)")
		return
	}

	pipe := s.voicePipeline()
	opts, err := pipe.Options()
	if err != nil {
		webVoiceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// THE ENGINE GATE IS THE CHAT ARM'S OWN (Rule 2): one judgement of "can this
	// engine stream at all", asked here so the answer is a status code, and asked
	// there so the TUI stays on its window loop — never two predicates to keep in
	// step.
	if !voiceLiveEngineSelected(opts) {
		webVoiceError(w, http.StatusNotImplemented, webVoiceLiveRefusal(voiceResolveEngine(opts)))
		return
	}

	// THE DEVICE IS RESERVED BEFORE IT IS OPENED: the Open below probes for ~1 s,
	// and a second click inside that window must see a conflict rather than a
	// second recorder holding the same microphone.
	live := &webVoiceLiveSession{finish: make(chan struct{})}
	s.voiceState.mu.Lock()
	if s.voiceState.session != nil || s.voiceState.live != nil {
		s.voiceState.mu.Unlock()
		webVoiceError(w, http.StatusConflict, "a recording is already running — stop it before starting another")
		return
	}
	s.voiceState.live = live
	s.voiceState.mu.Unlock()
	// THE RESERVATION IS RELEASED ON EVERY EXIT PATH, below the device open as
	// well: a refusal after the 200 can only be a frame, but a reservation left
	// behind would lock the microphone out for the life of the server.
	defer func() {
		s.voiceState.mu.Lock()
		if s.voiceState.live == live {
			s.voiceState.live = nil
		}
		s.voiceState.mu.Unlock()
	}()

	recorder, device, notes, err := pipe.Open(opts)
	if err != nil {
		webVoiceError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	// THE RECORDER MUST BE A LIVE RUNNER, and the assertion is the SAME one
	// chat_voice.go:1199 makes: the seam is optional by design (a one-window-per-
	// process recorder has no continuous stream to feed), so a pipeline that
	// opened something unable to run live is a NAMED refusal here rather than a
	// silent fall-through to a path the caller did not ask for.
	runner, ok := recorder.(voiceChatLiveRunner)
	if !ok {
		recorder.Close()
		webVoiceError(w, http.StatusNotImplemented, webVoiceLiveRefusal(voiceResolveEngine(opts)))
		return
	}
	defer recorder.Close()

	// THE STREAM IS OPENED ONLY NOW, after every refusal that has a status code:
	// from here on the answer is a body, and its first frame is the one that tells
	// the page the microphone is open and which device it is (the window path
	// answers that in its 200 body; an SSE body has no body to put it in).
	writeSSEHeaders(w)
	flusher.Flush()

	// THE THREE ENDERS. The stop route is the operator's; the ceiling timer is the
	// abandoned tab's (webVoiceMaxCaptureSeconds — a forgotten browser must not
	// hold the microphone for ever); the request context is the closed tab's.
	ceiling := time.AfterFunc(time.Duration(webVoiceMaxCaptureSeconds)*time.Second, func() {
		live.markTruncated()
		live.stop()
	})
	defer ceiling.Stop()
	stopOnDisconnect := make(chan struct{})
	defer close(stopOnDisconnect)
	go func() {
		select {
		case <-r.Context().Done():
			live.stop()
		case <-stopOnDisconnect:
		}
	}()

	sink := &webVoiceLiveSink{w: w, flusher: flusher}
	writeSSEFrame(w, webVoiceLiveEventReady, webVoiceLiveJSON(map[string]interface{}{
		"ok":     true,
		"device": device,
		"notes":  notes,
		"engine": voiceResolveEngine(opts),
	}))
	flusher.Flush()

	started := time.Now()
	report, liveErr := runner.Live(opts, sink, live.finish)
	elapsed := time.Since(started)

	// THE FINAL FRAME IS WRITTEN WHATEVER THE ARM RETURNED, because the two facts
	// are independent: an arm that failed still has a partial sentence the
	// operator can use, and a frame saying nothing at all would lose it. When the
	// arm's own text is empty the sink's settled text is what was heard.
	text := report.Text
	if text == "" {
		text = sink.settled
	}
	// A CANCELLED UTTERANCE DELIVERS NOTHING. The text still goes out (a reader
	// of the stream may want to know what was heard), but `draft` is EMPTY and
	// `cancelled` is set, so the page cannot insert what the operator ended — the
	// discard is the SERVER's answer, not a page-side omission that a reload or a
	// second page would not know about.
	cancelled := live.wasCancelled()
	draft := webVoiceDraftText(text)
	if cancelled {
		draft = ""
	}
	final := map[string]interface{}{
		"ok":         liveErr == nil,
		"transcript": text,
		// The DRAFT is the carrier's own view of a transcript (slash hazard
		// neutralised), so the page never has to re-implement that judgement.
		"draft":            draft,
		"cancelled":        cancelled,
		"device":           device,
		"notes":            notes,
		"engine":           voiceResolveEngine(opts),
		"partials":         report.Partials,
		"first_partial_ms": report.FirstPartialMS,
		"elapsed_ms":       elapsed.Milliseconds(),
		"timed_out":        report.TimedOut,
		"truncated":        live.wasTruncated(),
	}
	if liveErr != nil {
		// THE ERROR IS IN THE FRAME, not only in a status that was already sent:
		// the page prints the server's own sentence (Rule 15).
		final["ok"] = false
		final["error"] = liveErr.Error()
	}
	sink.mu.Lock()
	writeSSEFrame(w, webVoiceLiveEventFinal, webVoiceLiveJSON(final))
	sink.mu.Unlock()
	flusher.Flush()
}

// handleVoiceLiveStop answers POST /api/voice/live/stop: end the utterance in
// flight. The 200 is written at once and does NOT wait for the transcript — the
// sentence arrives on the /api/voice/live stream the page is already reading, and
// making the stop wait would hold the button down for the length of the engine's
// final answer.
func (s *webServer) handleVoiceLiveStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		webVoiceError(w, http.StatusMethodNotAllowed, "method not allowed (POST "+webVoiceLiveStopPath+")")
		return
	}
	s.voiceState.mu.Lock()
	live := s.voiceState.live
	s.voiceState.mu.Unlock()
	if live == nil {
		webVoiceError(w, http.StatusConflict, "no live recording is running")
		return
	}
	live.stop()
	webVoiceJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"stopped": true,
	})
}

// handleVoiceLiveCancel answers POST /api/voice/live/cancel: end the utterance in
// flight AND DISCARD it. This is the operator's Escape, and it is deliberately a
// different verb from the stop route: stop settles the sentence into the composer,
// cancel drops it. Like stop it answers at once and does not wait for the engine —
// the discard is decided HERE, and the final frame the page is already reading
// carries the verdict.
func (s *webServer) handleVoiceLiveCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		webVoiceError(w, http.StatusMethodNotAllowed, "method not allowed (POST "+webVoiceLiveCancelPath+")")
		return
	}
	s.voiceState.mu.Lock()
	live := s.voiceState.live
	s.voiceState.mu.Unlock()
	if live == nil {
		webVoiceError(w, http.StatusConflict, "no live recording is running")
		return
	}
	live.cancel()
	webVoiceJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"cancelled": true,
	})
}

// stopLiveVoiceRecording ends any live utterance. It is the SHUTDOWN half, the
// sibling of stopVoiceRecording: a device opened by a page that never clicked
// stop must not outlive the server (Rule 15), and the handler's own deferred
// Close is what releases it — this only tells it to finish now.
func (s *webServer) stopLiveVoiceRecording() {
	s.voiceState.mu.Lock()
	live := s.voiceState.live
	s.voiceState.mu.Unlock()
	if live != nil {
		live.stop()
	}
}
