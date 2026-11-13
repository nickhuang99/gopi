package main

// web_voice_cancel_test.go — SHIFT+ESCAPE'S ACT: end a recording and DISCARD it.
//
// THE DEFECT THIS FILE GUARDS. The page had two Escape branches (close the
// trigger menu; abort a running TURN) and a recording is neither — so while the
// microphone was open, Escape did nothing at all, and the only way out was the ⏹
// button, which STOPS AND TRANSCRIBES. "I started recording by mistake" therefore
// had no answer that did not put a sentence into the composer.
//
// THE SEMANTICS THIS PINS, and why they are two routes rather than one:
//
//	⏹           (micToggle)  STOP AND TRANSCRIBE — the sentence becomes an editable draft.
//	Esc         (micStopKeep) STOP AND KEEP       — the sentence stays, the caret goes last.
//	Shift+Esc   (micCancel)   CANCEL AND DISCARD   — the sentence is thrown away.
//
// WHY THE DISCARD MOVED BEHIND A MODIFIER (2026-11-07, the operator's own defect
// report). Escape USED to be the discard, and the key erases the composer — so an
// operator who pressed Escape to start PROOF-READING a half-right transcript
// (an ASR result always needs a human pass) lost that transcript, irreversibly:
// the composer has no undo stack. "Stop recording" and "throw the sentence away"
// are two intents of different sizes, and the coarse one is the destructive one.
// The discard survives — it is the right answer to "I never meant to say this" —
// but it now sits behind Shift, and it is deliberately unannounced.
//
// The discard is the SERVER's verdict, not a page-side omission: a page that
// merely ignored the answer would still pay the engine call and would still have
// transcribed audio the operator decided not to say. So each path gets its own
// route (`/api/voice/live/cancel`, `/api/voice/cancel`), each marks the session,
// and each answers `cancelled:true` — and for the live path the FINAL frame
// carries `draft:""` so the page's settle step has nothing to insert.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// TestVoiceLiveCancelDiscardsTheSentence drives the whole stream: start it, let
// one draft arrive, cancel, and read the final frame. The frame must carry the
// CANCEL verdict with an EMPTY draft — that pair is the discard, and it is what
// stops the page from inserting the sentence.
func TestVoiceLiveCancelDiscardsTheSentence(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder([]string{"不要", "不要这句"}, "不要这句。", 0)
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineSauc, rec: rec})
	mux := srv.webChatHandler()
	out := streamWithCancel(t, srv, mux)

	var final map[string]interface{}
	for _, e := range sseEvents(out.Body.String()) {
		if e[0] == webVoiceLiveEventFinal {
			if err := json.Unmarshal([]byte(e[1]), &final); err != nil {
				t.Fatalf("the final frame is not valid JSON (%v): %s", err, e[1])
			}
		}
	}
	if final == nil {
		t.Fatalf("no %q frame in the stream: %s", webVoiceLiveEventFinal, out.Body.String())
	}
	// The arm settled a sentence, and the CANCEL is what must empty it.
	if final["cancelled"] != true {
		t.Fatalf("cancelled = %v, want true — the cancel route did not reach the live session", final["cancelled"])
	}
	if got, _ := final["draft"].(string); got != "" {
		t.Fatalf("draft = %q, want empty — a cancelled utterance must not offer the operator a draft", got)
	}
	if got, _ := final["transcript"].(string); got != "不要这句。" {
		t.Fatalf("transcript = %q — the text that was heard is still REPORTED, only the draft is withheld", got)
	}
}

// streamWithCancel opens ONE live utterance on the server's mux, waits until the
// device is reserved, CANCELS it, and returns the finished stream. It exists
// because the stream route HOLDS the connection until the utterance ends: a
// synchronous voicePOST would deadlock against its own cancel.
func streamWithCancel(t *testing.T, srv *webServer, mux http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	got := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		got <- voicePOST(t, mux.ServeHTTP, webVoiceLivePath)
	}()
	deadline := time.After(5 * time.Second)
	for {
		srv.voiceState.mu.Lock()
		held := srv.voiceState.live != nil
		srv.voiceState.mu.Unlock()
		if held {
			break
		}
		select {
		case <-got:
			t.Fatal("the stream ended before the device was reserved")
		case <-deadline:
			t.Fatal("the live reservation never appeared — the stream did not start")
		case <-time.After(time.Millisecond):
		}
	}
	if code := voicePOST(t, mux.ServeHTTP, webVoiceLiveCancelPath).Code; code != http.StatusOK {
		t.Fatalf("POST %s with a recording running = %d, want 200", webVoiceLiveCancelPath, code)
	}
	select {
	case out := <-got:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end after the cancel")
	}
	return nil
}

// TestVoiceLiveStopStillSettles is the CONTRAST half, and without it the test
// above passes on a page that discards everything: the same stream stopped (not
// cancelled) must still hand the sentence over as a draft.
func TestVoiceLiveStopStillSettles(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder([]string{"你好"}, "你好世界。", 0)
	rec.autoFinish = 20 * time.Millisecond
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineSauc, rec: rec})
	out := voicePOST(t, srv.webChatHandler().ServeHTTP, webVoiceLivePath)

	var final map[string]interface{}
	for _, e := range sseEvents(out.Body.String()) {
		if e[0] == webVoiceLiveEventFinal {
			if err := json.Unmarshal([]byte(e[1]), &final); err != nil {
				t.Fatalf("the final frame is not valid JSON (%v): %s", err, e[1])
			}
		}
	}
	if final == nil {
		t.Fatalf("no %q frame in the stream: %s", webVoiceLiveEventFinal, out.Body.String())
	}
	if final["cancelled"] == true {
		t.Fatalf("cancelled = true on a recording nobody cancelled — the ⏹ path must still settle")
	}
	if got, _ := final["draft"].(string); got == "" {
		t.Fatalf("draft is empty with no cancel — the sentence was lost by the stop path")
	}
}

// TestVoiceLiveCancelRouteIsWired drives the cancel route on the server's OWN mux
// (a handler nobody registered answers 404 and the key would be invisible), and
// pins the two refusals: nothing running is a 409 (the state the caller wanted),
// and a wrong method is a 405.
func TestVoiceLiveCancelRouteIsWired(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder([]string{"x"}, "x", 0)
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineStreaming, rec: rec})
	mux := srv.webChatHandler()

	if got := voicePOST(t, mux.ServeHTTP, webVoiceLiveCancelPath).Code; got != http.StatusConflict {
		t.Fatalf("POST %s with nothing running = %d, want 409", webVoiceLiveCancelPath, got)
	}
	// A recording in flight: the cancel ends it (streamWithCancel waits for the
	// reservation, cancels, and returns the finished stream — the reservation's
	// release is the handler's own defer, so the SECOND cancel below answering
	// 409 is the effect read back).
	streamWithCancel(t, srv, mux)
	if got := voicePOST(t, mux.ServeHTTP, webVoiceLiveCancelPath).Code; got != http.StatusConflict {
		t.Fatalf("a second POST %s = %d, want 409 (the recording was already cancelled)",
			webVoiceLiveCancelPath, got)
	}
	if got := voicePOST(t, mux.ServeHTTP, webVoiceCancelPath).Code; got != http.StatusConflict {
		t.Fatalf("POST %s on the window path with nothing running = %d, want 409", webVoiceCancelPath, got)
	}
}

// TestVoiceWindowCancelDiscardsTheAudio is the ONE-SHOT path's discard: the
// capture is released and NO transcription is asked for. The fake recorder counts
// rounds, so "no round was asked for after the cancel" is the measurable form of
// "the engine was never called on audio the operator discarded".
func TestVoiceWindowCancelDiscardsTheAudio(t *testing.T) {
	t.Parallel()
	pipe := &fakeVoicePipeline{recorder: newFakeVoiceRecorder("这句不该被转写")}
	srv := newVoiceTestServer(pipe)
	mux := srv.webChatHandler()

	if got := voicePOST(t, mux.ServeHTTP, webVoiceStartPath).Code; got != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", webVoiceStartPath, got)
	}
	waitForRounds(t, pipe.recorder, 1)

	out := voicePOST(t, mux.ServeHTTP, webVoiceCancelPath)
	if out.Code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200 (%s)", webVoiceCancelPath, out.Code, out.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatalf("the cancel answer is not valid JSON (%v): %s", err, out.Body.String())
	}
	if body["cancelled"] != true {
		t.Fatalf("cancelled = %v, want true", body["cancelled"])
	}
	// THE DEVICE IS RELEASED, and it was released by the CANCEL (not by a later
	// stop): the recorder's own Close is the only owner of that fact.
	pipe.recorder.mu.Lock()
	closed := pipe.recorder.closed
	pipe.recorder.mu.Unlock()
	if !closed {
		t.Fatal("the capture was not released by the cancel — the microphone would stay open")
	}
	// A stop after a cancel is the same conflict the stop path gives any second
	// stop: nothing is running, and there is no audio left to transcribe.
	if got := voicePOST(t, mux.ServeHTTP, webVoiceStopPath).Code; got != http.StatusConflict {
		t.Fatalf("POST %s after a cancel = %d, want 409", webVoiceStopPath, got)
	}
}

// TestVoicePageEscCancelsTheRecording is the PAGE half: the keydown branch, the
// two cancel routes, and the geometry fix all live in the embedded page, so a
// rename on one side must fail HERE rather than as a key that silently does
// nothing.
func TestVoicePageEscCancelsTheRecording(t *testing.T) {
	t.Parallel()
	page := webChatPage
	for _, want := range []string{
		"const API_VOICE_LIVE_CANCEL='" + webVoiceLiveCancelPath + "';",
		"const API_VOICE_CANCEL='" + webVoiceCancelPath + "';",
		"const HTTP_CONFLICT=" + itoaInt(http.StatusConflict) + ";",
		"function micCancel(){",
		"function micStopKeep(){",
		"function micRecordingOpen(){",
		// ESCAPE IS ONE ARBITER, and both key listeners read it. The rungs are the
		// menu, the recording, a VISIBLE VOICE LAYER, and the turn.
		"function micEscAct(){",
		"if(menu.open)return ESC_MENU;",
		"if(micRecordingOpen()||micStopping)return ESC_RECORDING;",
		"if(micVoiceLayerUp())return ESC_VOICE_TEXT;",
		"if(busy)return ESC_TURN;",
		// THE VISIBILITY PREDICATE ITSELF, and it is a COMPARISON — not a flag.
		// Same measurement micDiscardSettled already uses: a sentence voice put in
		// the composer is a layer on screen exactly while the composer still holds
		// the value voice left there.
		"function micVoiceLayerUp(){",
		"return (input.value||'')===run.value;",
		"function micEscRun(act,discard){",
		"if(discard){micCancel();}else{micStopKeep();}",
		// The bubbling listener is what makes the key work when the composer is
		// not focused — a recording outlives a click on the page — and it must ask
		// the SAME arbiter rather than re-deriving the answer.
		"document.addEventListener('keydown',e=>{",
		"const act=micEscAct();",
		"micEscRun(act,e.shiftKey);",
		// The settle-window discard, its refusal, and the notice that makes a
		// discard distinguishable from a broken key.
		"function micDiscardSettled(){",
		"function micDiscardReport(){",
		"micVoiceRun={head:head,sep:sep,text:draft,tail:tail,value:next};",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the embedded page does not carry %q — Escape would not arbitrate its four meanings", want)
		}
	}
	// THE ACTS MUST STAY DISTINCT: the cancel route and the stop route are
	// different verbs, and the page must not send one to the other.
	if strings.Contains(page, "micCancel(){micToggle(") {
		t.Fatal("the page routes a cancel through micToggle — a cancel must not transcribe")
	}
	// AND THE DEFAULT KEY MUST NOT BE THE DISCARD. This is the defect the
	// operator reported: plain Escape ran micCancel, which cleared the composer
	// on the page AND asked the server for cancelled:true, draft:"". A page that
	// binds Escape to the cancel again has to fail HERE.
	if strings.Contains(page, "e.preventDefault();micCancel();") {
		t.Fatal("plain Escape is bound to micCancel again — Escape must STOP AND KEEP (micStopKeep), Shift+Escape is the discard")
	}
	// THE DISCARD IS NEVER SILENT. A key that deletes a sentence and says nothing
	// is indistinguishable from a key that did nothing, and the two need opposite
	// follow-ups. Both the discard and its refusal name themselves.
	for _, want := range []string{
		"micNotice('voice',micDiscardReport());",
		"'the composer no longer holds what voice inserted, so nothing was discarded — removing the recognised sentence now would take your own text with it'",
		"'there is no recognised sentence to discard — the composer holds only what you typed'",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page does not carry %q — a discard must never be silent", want)
		}
	}
	// THE OPERATOR'S TYPING IS NEVER DELETED: the settled-sentence discard must
	// refuse unless the composer is EXACTLY the value voice left there.
	if !strings.Contains(page, "if((input.value||'')!==run.value){") {
		t.Fatal("the discard does not compare the composer to the value voice left there — it could delete the operator's own typing")
	}
	// AND ESCAPE NEVER CLEARS THE COMPOSER. Only Ctrl+U may do that, and the
	// no-Escape branch is allowed to write input.value only from the REBUILD of a
	// record it has just proved still matches.
	if strings.Contains(page, "micEscRun") && strings.Contains(page, "if(act===ESC_VOICE_TEXT){input.value='';}") {
		t.Fatal("an Escape rung clears the composer — the operator's typing must survive every Escape")
	}
	// THE ORDER OF THE RUNGS IS PART OF THE CONTRACT, and the two orderings that
	// matter are asserted by POSITION, not by presence: a page that carried both
	// guards in the wrong order would still contain every string above and would
	// have silently changed what Escape means.
	//
	//   * the RECORDING precedes the VISIBLE VOICE LAYER, or a sentence left over
	//     from a previous utterance would steal the key from a live recording;
	//   * the VISIBLE VOICE LAYER precedes the TURN — the operator's report of
	//     2026-11-09: with the layer up and a response streaming, Escape aborted
	//     the response and left the layer exactly where it was, i.e. the key
	//     addressed everything on screen EXCEPT the visible layer it was aimed at.
	//     An on-screen input interaction owns Escape ahead of the content it is
	//     layered over — the same principle the recording rung already followed.
	//
	// BOTH directions are pinned by this one ordering, because the layer rung is
	// NOT unconditional: it asks micVoiceLayerUp(), which is false the moment the
	// composer stops holding what voice inserted. So a residual sentence still
	// loses to the turn (the measured reason the old order existed, and it is
	// still honoured), while a VISIBLE layer wins. The unconditional form is the
	// shape that would break the interrupt, and TestVoiceLayerOutranksOnlyWhileVisible
	// is what measures that half.
	// THE MEASUREMENT IS SCOPED TO THE ARBITER'S OWN BODY. Reading the whole page
	// would match a rung NAME that a comment quotes (this file's own prose explains
	// the ordering by quoting the guards, and the first such quote wins a whole-page
	// Index), which would report an order that the code does not have.
	body := page[strings.Index(page, "function micEscAct(){"):]
	body = body[:strings.Index(body, "\n}")]
	rec := strings.Index(body, "if(micRecordingOpen()||micStopping)return ESC_RECORDING;")
	layer := strings.Index(body, "if(micVoiceLayerUp())return ESC_VOICE_TEXT;")
	turn := strings.Index(body, "if(busy)return ESC_TURN;")
	residual := strings.Index(body, "if(micVoiceRun)return ESC_VOICE_TEXT;")
	if rec < 0 || layer < 0 || turn < 0 || residual < 0 || !(rec < layer && layer < turn && turn < residual) {
		t.Fatalf("the ESCAPE rungs are out of order (recording=%d, visible-layer=%d, turn=%d, residual-sentence=%d) — the VISIBLE layer sits between the recording and the turn, and the RESIDUAL sentence stays below the turn", rec, layer, turn, residual)
	}
	// AND THE UPPER RUNG MUST ASK VISIBILITY, not the bare record — that is the
	// whole difference between this fix and the ordering that was measured to break
	// the interrupt (a settled sentence plus a running turn made the arbiter answer
	// voice-text and the turn kept streaming). The rung ABOVE the turn reads
	// micVoiceLayerUp; the bare record appears ONLY below it, as the residual rung
	// whose job there is to REFUSE the discard out loud instead of doing nothing.
	// AND THE LAYER RUNG MUST STAY A COMPARISON, not a flag. Hoisting
	// ESC_VOICE_TEXT above the turn UNCONDITIONALLY is the shape that was measured
	// to break the interrupt (a settled sentence plus a running turn made the
	// arbiter answer voice-text and the turn kept streaming); a FLAG would be the
	// same defect one layer down, because it would have to be cleared from every
	// call site that touches the composer, and each missed one is a key that dies
	// mid-stream. The predicate therefore re-derives visibility from the same
	// measurement the discard uses: does the composer still hold what voice left?
	if !strings.Contains(page, "function micVoiceLayerUp(){") ||
		!strings.Contains(page, "return (input.value||'')===run.value;") {
		t.Fatal("micVoiceLayerUp no longer compares the composer to the value voice left there — visibility of the voice layer must be MEASURED on every call, never cached in a flag")
	}
	// AND THE RESIDUAL SENTENCE STILL ANSWERS WHEN NOTHING STREAMS — that is the
	// only rung whose job is to REFUSE a discard out loud (Rule 15: a Shift+Escape
	// on a stale record that does nothing at all is indistinguishable from a broken
	// key). Its position BELOW the turn is what keeps it from stealing the
	// interrupt; its existence is what keeps the refusal reachable.
	if residual < turn {
		t.Fatal("the residual-sentence rung was moved above the turn — a stale record would steal the interrupt from a streaming response")
	}
	// A STOP ALREADY ON THE WIRE IS NOT SENT TWICE: the guard is what keeps a
	// second Escape from buying a 409 whose notice reads as the operator's error.
	if !strings.Contains(page, "if(micStopping)return true;") {
		t.Fatal("micStopKeep has no in-flight guard — a second Escape would re-send a stop that is already travelling")
	}
	// THE WHOLE-COMPOSER CLEAR IS Ctrl+U, and it is UNANNOUNCED: the chord must
	// be in the code and must NOT be in the visible markup (a placeholder, a
	// title, a hint). The operator's own design point: a key nobody is told
	// about cannot be tripped into.
	if !strings.Contains(page, "e.key===KEY_CLEAR_INPUT||e.key===KEY_CLEAR_INPUT_UPPER") {
		t.Fatal("the page carries no Ctrl+U whole-composer clear")
	}
	if markup := page[:strings.Index(page, "<script>")]; strings.Contains(strings.ToLower(markup), "ctrl+u") {
		t.Fatal("the visible markup advertises Ctrl+U — the clear must stay unannounced (no placeholder, no title, no hint)")
	}
	if webVoiceLiveCancelPath == webVoiceLiveStopPath || webVoiceCancelPath == webVoiceStopPath {
		t.Fatal("a cancel route equals its stop route — the discard would settle the sentence after all")
	}
	// THE LAYOUT FIX: #voicelive must be OUT OF FLOW (a flex child is charged its
	// intrinsic width out of the composer's share — measured 184.72 px), so the
	// rule must carry position:absolute and must NOT be an in-flow nowrap span.
	rule := pageCSSRule(t, page, "#voicelive{")
	for _, want := range []string{"position:absolute", "pointer-events:none"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("#voicelive's rule does not carry %q — it is an in-flow flex child again, "+
				"which is the 184.72 px the composer loses while recording: %s", want, rule)
		}
	}
}

// pageCSSRule returns the text of one CSS rule from the page, from the selector
// through its closing brace. A rule split across lines is returned whole.
func pageCSSRule(t *testing.T, page, selector string) string {
	t.Helper()
	i := strings.Index(page, selector)
	if i < 0 {
		t.Fatalf("the page carries no %q rule", selector)
	}
	j := strings.Index(page[i:], "}")
	if j < 0 {
		t.Fatalf("the %q rule is never closed", selector)
	}
	return page[i : i+j+1]
}
