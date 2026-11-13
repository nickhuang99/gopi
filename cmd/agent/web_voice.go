package main

// web_voice.go — the MICROPHONE on the `gopi web` page.
//
// WHAT IT IS. Two endpoints and a recorder goroutine: POST /api/voice/start opens
// the microphone ON THE MACHINE RUNNING `gopi web`, captures until POST
// /api/voice/stop, transcribes the whole utterance through the EXISTING engine
// seam, and answers with a DRAFT the page places in the composer. The draft is
// text, not a turn: the operator edits it and presses Enter, exactly like the
// TUI's `/voice` (chat_voice.go), which appends a transcript to the editing line
// and lets the operator submit it.
//
// WHY SERVER-SIDE RECORDING (route B) AND NOT THE BROWSER'S MediaRecorder (route
// A). The device policy — four tiers, by-name identity, a liveness probe before
// the real recording, the "reachable but silent" verdict — lives in
// cli_voice_device_policy.go and is read by every voice shape gopi has. Route A
// would put a SECOND capture implementation in JavaScript, where none of that
// policy exists: the browser would pick a device by its own rules (a permission
// prompt, a default-input preference, an unnamed `getUserMedia` track) and gopi
// would receive audio with no identity behind it — nothing to print in a refusal,
// nothing an operator can pin with --device, and a fix to the policy unable to
// land on that door. Route B keeps ONE microphone policy for both carriers.
//
// THE COST IS STATED, NOT HIDDEN: the microphone is the one attached to the
// machine running `gopi web`. A browser on a laptop talking to a server on
// another host records the SERVER's room, not the speaker's. That is the whole
// price of the shared policy and it is why `gopi web` binds loopback by default.
//
// WHAT IS REUSED, deliberately, and there is nothing here that re-implements it
// (Rule 13/14): voiceListDevices + voiceResolveSelection (the four-tier device
// policy), openVoiceStagedStream (the ONE capture seam — the same call
// chat_voice_recorder_staged.go's Select makes, so the argv, the recorder choice
// and the pacing cannot diverge between the two carriers), voicePCM16ToWAV /
// voiceStageCaptureAudio / transcribeFile (the ONE engine seam every voice shape
// funnels through), chatVoiceTranscriptText (the ONE judgement of "this window
// carried no speech") and parseVoiceArgs (the ONE builder of the engine options).
//
// WHY ONE TRANSCRIPT PER CLICK AND NOT A STREAM OF WINDOWS. The TUI's `/voice`
// transcribes window after window because it is a CONTINUOUS dictation mode whose
// output appends to a live editing line. A click is one utterance: the recorder
// goroutine collects the windows (the stream cuts them so no audio is lost) and
// the engine sees ONE file, so a sentence split across a window boundary is not
// assembled from two of the engine's guesses.
//
// LOCK DISCIPLINE. Two locks, in this order and never the other way:
//
//	voiceMu — serialises the two ENDPOINTS (one microphone, one start/stop pair).
//	          Taken first, released last, and never taken by a turn.
//	s.mu    — the turn lock (cli_web.go). Taken ONLY around the transcription, so
//	          a transcript cannot race an in-flight turn on the shared agent
//	          (Agent.Chat mutates Ctx.Messages unsynchronised), and released
//	          before the HTTP answer is written.
//
// START deliberately does NOT take s.mu: opening a device mutates no agent state,
// and charging a 1 s device probe to the turn lock would queue a live turn behind
// a microphone for no shared thing. STOP is where the agent is touched.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The route vocabulary (Rule 1: one const block; the page's copies are pinned to
// these by TestWebVoicePageCarriesTheRoutes).
const (
	webVoiceStartPath = "/api/voice/start"
	webVoiceStopPath  = "/api/voice/stop"
	// webVoiceCancelPath is the WINDOW path's discard route, the sibling of
	// webVoiceLiveCancelPath: end the open capture WITHOUT transcribing it. Stop
	// and cancel are two verbs here for the same reason they are two on the
	// stream — a stop settles a draft into the composer and a cancel must not,
	// and a single verb with a "discard" flag would put the discard decision
	// somewhere no route names.
	webVoiceCancelPath = "/api/voice/cancel"
)

const (
	// webVoiceWindowSeconds is the bound of ONE capture window. The builder closes
	// a window at a PAUSE in the speech or at this bound, whichever comes first,
	// so it is also the worst-case delay between the click and the round ending.
	// It is NOT the recording length — the recording is every window the click
	// spans, and their transcripts are concatenated in stream order.
	webVoiceWindowSeconds = 6

	// webVoiceStopDeadline bounds how long STOP waits for the capture to finish:
	// the round in flight ends at its own window boundary and its transcript comes
	// back, and the recorder's own Drain then covers the windows already read. It
	// is generous because the ENGINE is inside that wait (a round transcribes
	// before it returns). Past it the capture is closed under the loop and the
	// outcome is REPORTED (TailDropped) rather than presented as a whole utterance
	// — a click that hangs forever would be worse than a dropped tail.
	webVoiceStopDeadline = 90 * time.Second

	// webVoiceMaxCaptureSeconds is the HARD ceiling on one recording. A click that
	// is never followed by another opens the microphone, and a forgotten browser
	// tab must not hold the device forever: past this the recorder stops
	// accumulating, the capture is closed, and the answer says it was truncated.
	webVoiceMaxCaptureSeconds = 120

	// webVoiceMaxCaptureBytes is that ceiling in the capture contract's own unit,
	// derived so the two cannot drift (Rule 13).
	webVoiceMaxCaptureBytes = webVoiceMaxCaptureSeconds * voiceCaptureSampleRate * voiceCaptureBytesPerSample
)

// webVoiceDraftText is THE ONE judgement of what a transcript contributes to the
// composer, and it is where the SLASH HAZARD is neutralised.
//
// THE HAZARD, stated as the defect it is: a message whose first word is
// "/something" does not reach the model on this carrier — classifyChatLine routes
// it to the shared command table and serveSlashCommand answers it locally
// (cli_web.go). A transcript is SPEECH: an operator who says "slash new" is not
// asking to clear the conversation, and a cloud engine happily returning "/new"
// for a misheard utterance would silently reset the session instead of drafting a
// message. The neutralisation is the carrier's OWN literal-send channel —
// chatSlashEscape ("//"), whose classification is chatLineEscape: the page sends
// "//new", the server strips ONE slash, and the model receives the operator's
// literal "/new" as text. Inventing a second escape here would be a second answer
// to "how do I send a literal slash" on the same wire.
//
// A transcript that is ALREADY in the escape form ("//word") is left alone: it
// will be un-escaped to "/word" by exactly that rule, which is what the engine
// said. Silence is not a draft — chatVoiceTranscriptText is the ONE judgement of
// that (a `/sil` marker window is the engine saying nothing), and an empty draft
// is reported as such rather than inserted as a blank line.
func webVoiceDraftText(transcript string) string {
	text := strings.TrimSpace(chatVoiceTranscriptText(transcript))
	if text == "" {
		return ""
	}
	if body, ok := strings.CutPrefix(text, chatSlashTrigger); ok &&
		!strings.HasPrefix(text, chatSlashEscape) {
		// ONE EXTRA SLASH is the whole spelling: chatSlashEscape is built from
		// the trigger, and chatLineEscape strips exactly one, so prepending the
		// TRIGGER rebuilds the escape for this line ("/new" → "//new") instead of
		// gluing a second copy of the two-slash constant onto it.
		return chatSlashEscape + body
	}
	return text
}

// webVoiceRecorder is the RECORDING SIDE of the carrier, and it is deliberately
// the TUI's own recorder type: *voiceChatRecorderStaged (chat_voice_recorder_staged.go),
// the one `/voice` drives. Round records ONE window and returns its transcript;
// Drain hands back the windows that were already read but not yet transcribed;
// Close releases the capture.
//
// WHY THE TUI'S RECORDER AND NOT A LOOP OVER voiceStagedStream.Next. The two
// shapes are the same question ("capture, then stop without losing what was
// said") and it already has ONE right answer here: stopping CLOSES the stream,
// and a close discards the window the builder holds (voiceStagedStream.flush
// returns errVoiceStreamClosed once the stream is closed). chat_voice_recorder_staged
// solves that by running the builder in its own goroutine and stopping the READER
// first, then the stream — so the tail is handed over instead of dropped. A web
// carrier looping over Next() directly would re-introduce exactly the defect the
// TUI fixed (task 01a11b81: "the tail of a long recording went there"), and it
// would be a second capture loop to keep in step. It is an interface so a test can
// drive the endpoints with scripted transcripts instead of a microphone.
type webVoiceRecorder interface {
	Round(opts *voiceOptions, seconds int) (string, error)
	Drain() []string
	Close()
}

// webVoicePipeline is the seam a test replaces WHOLE: engine options, the device
// decision + the opened recorder. Two methods, each mirroring one production
// call, so nothing about the routing above it is faked when the microphone is.
type webVoicePipeline interface {
	// Options is the engine/capture settings ONE click records and transcribes
	// with — chatVoiceResolveOptions, the same builder the TUI's `/voice` uses.
	Options() (*voiceOptions, error)
	// Open resolves the device through the four-tier policy and opens the capture.
	Open(opts *voiceOptions) (webVoiceRecorder, string, []string, error)
}

// webVoiceProduction is the PRODUCTION pipeline: the same calls, in the same
// order, as chat_voice_recorder_staged.go, with nothing added.
type webVoiceProduction struct{}

func (webVoiceProduction) Options() (*voiceOptions, error) { return chatVoiceResolveOptions() }

// Open is the TUI's recorder constructor plus ITS OWN Select
// (chatVoiceRecorder → voiceChatRecorderStaged.Select), so the device policy, the
// liveness probe, the argv and the engine seam are ONE implementation for both
// carriers — chatVoiceRecorder("") is the same call cli_chat.go makes, with no
// audio source, i.e. the microphone.
func (webVoiceProduction) Open(opts *voiceOptions) (webVoiceRecorder, string, []string, error) {
	// THE DEPENDENCY CHECK COMES FIRST and it is the one the TUI mode uses: a
	// missing engine is an installation instruction, not a microphone problem, and
	// reporting it after a successful recording would charge the operator's time
	// for nothing.
	if missing := voiceDepsReady(opts); missing != "" {
		return nil, "", nil, errors.New(missing)
	}
	rec := chatVoiceRecorder("")
	device, notes, err := rec.Select("")
	if err != nil {
		rec.Close()
		return nil, "", notes, err
	}
	return rec, device, notes, nil
}

// webVoiceSession is ONE recording: the open capture, the transcripts collected so
// far and the goroutine driving it.
type webVoiceSession struct {
	opts     *voiceOptions
	recorder webVoiceRecorder
	device   string
	notes    []string

	// done is closed when the recorder goroutine is over, for any reason.
	done chan struct{}

	startedAt time.Time
	// quit is closed by stop. The round IN FLIGHT is allowed to end (that is why
	// the window is short) and the loop returns before the next one — stopping
	// must never yank the capture out from under a round, which is exactly the
	// path that loses the audio of the window the operator clicked during.
	quit chan struct{}
	// truncated is set by the recorder goroutine when the CEILING ended the
	// capture rather than the operator's click (see webVoiceMaxCaptureSeconds). It
	// is written by that goroutine and read after it has returned.
	truncated bool

	mu    sync.Mutex
	texts []string
	err   error
}

// record reads the capture until stop, the device fails, or the ceiling is hit.
// EVERY window is kept, in arrival order, and the WINDOWS ARE THE BUILDER'S: this
// loop does not decide where one ends (voiceStagedStream.Next does, from the
// audio), so a click cannot cut a word that the builder would have kept whole.
func (v *webVoiceSession) record() {
	defer close(v.done)
	for {
		select {
		case <-v.quit:
			return
		default:
		}
		text, err := v.recorder.Round(v.opts, webVoiceWindowSeconds)
		if err != nil {
			if errors.Is(err, errVoiceStreamClosed) {
				// THE CAPTURE ENDED UNDER THE ROUND (stop closed it): the windows
				// still in hand are Drain's, and the caller runs that.
				return
			}
			// A ROUND THAT FAILED IS REPORTED, not swallowed: "the device went
			// away" and "nobody spoke" are different facts (Rule 15).
			v.mu.Lock()
			v.err = err
			v.mu.Unlock()
			return
		}
		if text = strings.TrimSpace(text); text != "" {
			v.mu.Lock()
			v.texts = append(v.texts, text)
			v.mu.Unlock()
		}
		if v.elapsedExceeded() {
			v.truncated = true
			// THE CEILING ENDS THE CAPTURE, not just the accumulation: an
			// abandoned tab must not leave the microphone open (Rule 15).
			v.recorder.Close()
			return
		}
	}
}

// elapsedExceeded reports whether this recording has outlived its ceiling. The
// ceiling exists because a click that is never followed by another leaves the
// microphone open: an abandoned browser tab must not hold the device for ever, and
// the answer then says the capture was cut short.
func (v *webVoiceSession) elapsedExceeded() bool {
	return time.Since(v.startedAt) >= time.Duration(webVoiceMaxCaptureSeconds)*time.Second
}

// transcript is what was said so far, in stream order.
func (v *webVoiceSession) transcript() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return strings.Join(v.texts, " ")
}

// addTexts appends a finish's transcripts in stream order.
func (v *webVoiceSession) addTexts(texts []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, t := range texts {
		if t = strings.TrimSpace(t); t != "" {
			v.texts = append(v.texts, t)
		}
	}
}

// failure is why the recorder stopped, when it was not the operator.
func (v *webVoiceSession) failure() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.err
}

// webVoiceState is the mutable half of the carrier's voice state, on webServer as
// a single pointer so the zero value needs no initialiser (and a test can drop a
// recording in without a constructor).
type webVoiceState struct {
	mu      sync.Mutex
	session *webVoiceSession
	// live is the utterance in flight on the STREAMING route
	// (web_voice_live.go). It shares this lock — and therefore this "one
	// microphone" answer — with the window route above: a device can be held by
	// either shape, never by both, and the loser gets the same 409.
	live *webVoiceLiveSession
}

// voicePipeline resolves the pipeline, defaulting to production: ONE reader of
// "where does the microphone come from" for both endpoints (Rule 2). An injected
// pipeline is a test's; the field is nil in every real server.
func (s *webServer) voicePipeline() webVoicePipeline {
	if s.voice != nil {
		return s.voice
	}
	return webVoiceProduction{}
}

// handleVoiceStart answers POST /api/voice/start: resolve the device, open the
// capture, and hand the recording to a goroutine.
//
// The device is resolved BEFORE the 200 so a refusal (no engine, no device, a
// device that will not open) reaches the page as a 4xx/5xx with the sentence that
// names it — a capture that fails a second later, after the button has turned
// red, is the silent failure Rule 15 is about.
func (s *webServer) handleVoiceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		webVoiceError(w, http.StatusMethodNotAllowed, "method not allowed (POST "+webVoiceStartPath+")")
		return
	}
	if s.shuttingDown {
		webVoiceError(w, http.StatusServiceUnavailable, "server shutting down")
		return
	}
	s.voiceState.mu.Lock()
	defer s.voiceState.mu.Unlock()
	// ONE MICROPHONE: a LIVE utterance (web_voice_live.go) holds the device through
	// the same field, so a click on the one-shot route while a stream is open is
	// the same conflict — two recorders on one microphone is not a queue.
	if s.voiceState.session != nil || s.voiceState.live != nil {
		webVoiceError(w, http.StatusConflict, "a recording is already running — stop it before starting another")
		return
	}

	pipe := s.voicePipeline()
	opts, err := pipe.Options()
	if err != nil {
		webVoiceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recorder, device, notes, err := pipe.Open(opts)
	if err != nil {
		webVoiceError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	sess := &webVoiceSession{
		opts:      opts,
		recorder:  recorder,
		startedAt: time.Now(),
		device:    device,
		notes:     notes,
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	s.voiceState.session = sess
	go sess.record()
	webVoiceJSON(w, http.StatusOK, map[string]interface{}{
		"ok":     true,
		"device": device,
		"notes":  notes,
	})
}

// webVoiceStopResult is the stop endpoint's answer: the DRAFT the page puts in the
// composer (empty when nothing was said), what the engine actually returned, and
// the two honesty flags a caller may act on.
type webVoiceStopResult struct {
	OK         bool     `json:"ok"`
	Draft      string   `json:"draft"`
	Transcript string   `json:"transcript"`
	Device     string   `json:"device"`
	Notes      []string `json:"notes"`
	// Truncated says the ceiling ended the recording rather than the operator.
	Truncated bool `json:"truncated"`
	// TailDropped says the stop deadline was spent and the audio still in the
	// window builder was given up (see webVoiceStopDeadline).
	TailDropped bool `json:"tail_dropped"`
}

// handleVoiceStop answers POST /api/voice/stop: end the recording, glue the
// windows, transcribe under the TURN LOCK, and answer with the draft.
func (s *webServer) handleVoiceStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		webVoiceError(w, http.StatusMethodNotAllowed, "method not allowed (POST "+webVoiceStopPath+")")
		return
	}
	s.voiceState.mu.Lock()
	sess := s.voiceState.session
	if sess == nil {
		s.voiceState.mu.Unlock()
		webVoiceError(w, http.StatusConflict, "no recording is running")
		return
	}
	// THE SESSION IS TAKEN OUT OF THE FIELD BEFORE THE WORK, so a second stop (or
	// a start racing it) sees "nothing running" instead of joining a transcription
	// halfway and transcribing the same audio twice.
	s.voiceState.session = nil
	s.voiceState.mu.Unlock()

	// THE FINISH RUNS UNDER THE TURN LOCK (s.mu): the windows still in hand are
	// transcribed HERE, so the engine is called on the same shared agent a turn
	// streams from — and a transcript cannot land while a turn is mutating
	// Agent.Ctx.Messages. Opening the device deliberately does NOT take this lock
	// (a ~1 s probe must never be charged to a live turn), so this is the ONE
	// section of the carrier that is serialised against a turn.
	s.mu.Lock()
	tailDropped := sess.finish()
	s.mu.Unlock()

	if err := sess.failure(); err != nil {
		webVoiceError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	text := sess.transcript()
	draft := webVoiceDraftText(text)
	webVoiceJSON(w, http.StatusOK, webVoiceStopResult{
		OK:          true,
		Draft:       draft,
		Transcript:  text,
		Device:      sess.device,
		Notes:       sess.notes,
		Truncated:   sess.truncated,
		TailDropped: tailDropped,
	})
}

// handleVoiceCancel answers POST /api/voice/cancel: end the open recording and
// THROW THE AUDIO AWAY. It is the window path's Escape, and its whole reason to
// exist is that stopping would transcribe: an utterance the operator decided not
// to say must not become a draft, and a discard implemented on the page (ignore
// the answer) would still pay the engine call — and would still put the sentence
// in the server's logs.
//
// THE SESSION IS TAKEN OUT FIRST, exactly like stop: a second cancel (or a start
// racing it) must see "nothing running" rather than join a teardown.
func (s *webServer) handleVoiceCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		webVoiceError(w, http.StatusMethodNotAllowed, "method not allowed (POST "+webVoiceCancelPath+")")
		return
	}
	s.voiceState.mu.Lock()
	sess := s.voiceState.session
	s.voiceState.session = nil
	s.voiceState.mu.Unlock()
	if sess == nil {
		webVoiceError(w, http.StatusConflict, "no recording is running")
		return
	}
	// THE DEVICE IS RELEASED WITHOUT A TRANSCRIPTION. The reader loop is asked to
	// end and the capture is closed — `finish` is NOT used here, because its whole
	// job is to drain the windows into `texts` under the turn lock, and every one
	// of those windows is audio the operator discarded. Closing first is correct
	// on this path (it is the one place the "do not lose the tail" rule inverts):
	// the tail is what we were told to lose.
	close(sess.quit)
	sess.recorder.Close()
	<-sess.done
	webVoiceJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"cancelled": true,
		"device":    sess.device,
	})
}

// stopVoiceRecording closes any live recording. It is the SHUTDOWN half: a device
// opened by a page that never clicked stop must not outlive the server that
// opened it (Rule 15 — the process is released by whoever owns it, and this server
// is the only owner a browser tab gives it).
func (s *webServer) stopVoiceRecording() {
	s.voiceState.mu.Lock()
	sess := s.voiceState.session
	s.voiceState.session = nil
	s.voiceState.mu.Unlock()
	if sess != nil {
		// The device is released by the recorder's own Close, and the goroutine is
		// waited for so nothing outlives the server that opened it.
		sess.recorder.Close()
		<-sess.done
	}
}

// finish stops the recording: ask the reader to end at the window boundary, wait
// for it, and CLOSE the capture whichever way the wait went (the device must not
// outlive the click — Rule 15).
//
// It returns whether the tail was DROPPED, i.e. the deadline expired with audio
// still in the builder.
func (v *webVoiceSession) finish() bool {
	// THE STOP IS THE CHAT RECORDER'S OWN ORDER (voiceChatMode.finishCapture):
	// let the round in flight come back, DRAIN the windows already read, and only
	// then Close the capture. Closing first would signal the recorder process and
	// discard exactly the audio the operator had just spoken.
	close(v.quit)
	timer := time.NewTimer(webVoiceStopDeadline)
	defer timer.Stop()
	dropped := false
	select {
	case <-v.done:
	case <-timer.C:
		// THE ROUND DID NOT COME BACK, so releasing the device is the only way out
		// and whatever that round held is gone — REPORTED, never presented as the
		// whole utterance.
		dropped = true
		v.recorder.Close()
		<-v.done
	}
	v.addTexts(v.recorder.Drain())
	v.recorder.Close()
	return dropped
}

// webVoiceError writes one refusal as JSON, so the page can show the SERVER's own
// sentence instead of "request failed" (Rule 15: a refusal must name its reason).
func webVoiceError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload, _ := json.Marshal(map[string]interface{}{"ok": false, "error": msg}) //stylegate:ignore marshalling a value built in-process cannot fail
	w.Write(payload)                                                              //stylegate:ignore a failed write is the client's disconnect, not a server fault
}

// webVoiceJSON writes one success body.
func webVoiceJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload, err := json.Marshal(v)
	if err != nil {
		// UNREACHABLE for the shapes above (no channel, no func reaches them), and
		// still checked: a silent empty 200 would be the worst possible answer.
		fmt.Fprintf(w, "{\"ok\":false,\"error\":%q}", err.Error())
		return
	}
	w.Write(payload) //stylegate:ignore a failed write is the client's disconnect, not a server fault
}
