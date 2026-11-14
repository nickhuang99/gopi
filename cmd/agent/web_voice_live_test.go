package main

// web_voice_live_test.go — the LIVE microphone route (web_voice_live.go).
//
// THE FOUR THINGS THIS FILE IS ABOUT, each a way the carrier could be wrong
// while looking right:
//
//  1. THE ROUTES ARE WIRED (both of them, on the server's own mux), and the
//     stream's first frame is a `ready` carrying the device the SERVER opened —
//     the window path answers that in its 200 body, and an SSE body has no body
//     to put it in.
//  2. THE SINK → FRAME MAPPING. Every partial the arm publishes becomes one
//     `draft` frame, in order, each flushed BEFORE the next is produced (that
//     ordering is the whole feature: a page that receives all the drafts at once
//     at the end has the window path's latency with more machinery), and the
//     settled sentence arrives as ONE `final` frame carrying the run's figures.
//  3. THE REFUSAL. An engine with no live arm answers 501 with the engine's name
//     in the sentence — the page's fallback trigger, and the reason a browser on
//     a whisper-local host is not a frozen composer.
//  4. THE PAGE. The routes, the event names and the fallback status are spelled
//     twice (Go and the embedded page), so a test pins every pair.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// ------------------------------------------------------------- the live fake

// fakeLiveRecorder is a capture recorder that ALSO answers the live seam
// (voiceChatLiveRunner) — the same two-hat shape the production
// *voiceChatRecorderStaged wears. It publishes scripted partials on its own
// goroutine, waits for the finish signal, then settles one sentence.
type fakeLiveRecorder struct {
	partials []string
	final    string
	// delay is how long each partial waits before it is published, so a test can
	// observe a draft ARRIVING BEFORE the stop request (the feature's whole
	// claim) instead of only its final text.
	delay   time.Duration
	liveErr error
	// autoFinish, when set, models THE CAPTURE RUNNING OUT: the arm ends by itself
	// after this long instead of waiting for the stop route. It is a real ending
	// (voiceLiveReport.CaptureEnded's own case), and without it a test that never
	// clicks stop would wait out the carrier's 120 s ceiling.
	autoFinish time.Duration
	closed     chan struct{}
	liveDone   chan struct{}

	mu       sync.Mutex
	drafts   []string
	settled  string
	liveRuns int
}

func newFakeLiveRecorder(partials []string, final string, delay time.Duration) *fakeLiveRecorder {
	return &fakeLiveRecorder{
		partials: partials,
		final:    final,
		delay:    delay,
		closed:   make(chan struct{}),
		liveDone: make(chan struct{}),
	}
}

// Round/Drain/Close are the WINDOW half: this recorder is never driven through
// it here, and answering the interface is what makes "the same recorder serves
// both arms" true rather than a test-only fiction.
func (f *fakeLiveRecorder) Round(*voiceOptions, int) (string, error) { return "", errVoiceStreamClosed }
func (f *fakeLiveRecorder) Drain() []string                          { return nil }
func (f *fakeLiveRecorder) Close()                                   { close(f.closed) }

// Live publishes the scripted partials, waits for finish, and settles.
func (f *fakeLiveRecorder) Live(_ *voiceOptions, sink voiceLiveSink, finish <-chan struct{}) (voiceLiveReport, error) {
	f.mu.Lock()
	f.liveRuns++
	f.mu.Unlock()
	defer close(f.liveDone)
	for _, p := range f.partials {
		if f.delay > 0 {
			select {
			case <-time.After(f.delay):
			case <-finish:
				return f.settle(sink)
			}
		}
		f.mu.Lock()
		f.drafts = append(f.drafts, p)
		f.mu.Unlock()
		sink.Draft(p)
	}
	if f.autoFinish > 0 {
		select {
		case <-finish:
		case <-time.After(f.autoFinish):
		}
		return f.settle(sink)
	}
	<-finish
	return f.settle(sink)
}

// settle hands over the final sentence the way the real arm does.
func (f *fakeLiveRecorder) settle(sink voiceLiveSink) (voiceLiveReport, error) {
	sink.Settle(f.final)
	f.mu.Lock()
	n := len(f.drafts)
	f.mu.Unlock()
	return voiceLiveReport{Text: f.final, Partials: n, FirstPartialMS: 2500}, f.liveErr
}

// livePipeline is the pipeline the handler sees: engine options plus this
// recorder.
type livePipeline struct {
	engine string
	rec    *fakeLiveRecorder
}

func (p livePipeline) Options() (*voiceOptions, error) {
	return &voiceOptions{engine: p.engine}, nil
}

func (p livePipeline) Open(*voiceOptions) (webVoiceRecorder, string, []string, error) {
	return p.rec, "pulse:scripted-live-mic", []string{"[scripted]"}, nil
}

// ------------------------------------------------------------------ the routes

// TestVoiceLiveRoutesAreWired drives BOTH routes on the server's OWN mux (not the
// handler in isolation), because the failure this catches is a handler nobody
// registered — the page would post to a 404 and the feature would be invisible.
func TestVoiceLiveRoutesAreWired(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder([]string{"你好"}, "你好世界", 0)
	rec.autoFinish = 20 * time.Millisecond
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineStreaming, rec: rec})
	mux := srv.webChatHandler()

	if got := voicePOST(t, mux.ServeHTTP, webVoiceLivePath).Code; got != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", webVoiceLivePath, got)
	}
	<-rec.liveDone
	// THE RESERVATION IS RELEASED BY THE HANDLER'S OWN DEFER, after the final
	// frame — so the test waits for the EFFECT (the field going back to nil)
	// rather than for a duration that "should be enough".
	deadline := time.After(5 * time.Second)
	for {
		srv.voiceState.mu.Lock()
		held := srv.voiceState.live != nil
		srv.voiceState.mu.Unlock()
		if !held {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the live reservation was not released after the utterance ended")
		case <-time.After(time.Millisecond):
		}
	}
	// The utterance is over, so the stop route has nothing to end and says so
	// with the same conflict the window path uses.
	if got := voicePOST(t, mux.ServeHTTP, webVoiceLiveStopPath).Code; got != http.StatusConflict {
		t.Fatalf("POST %s with no utterance running = %d, want 409", webVoiceLiveStopPath, got)
	}
}

// ----------------------------------------------------- the sink → frame mapping

// TestVoiceLiveSinkToFrames is the mapping test: partials become flushed draft
// frames in order, the settle becomes ONE final frame carrying the arm's own
// figures, and the ready frame names the device.
func TestVoiceLiveSinkToFrames(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder([]string{"你好", "你好世界"}, "你好世界。", 0)
	rec.autoFinish = 20 * time.Millisecond
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineSauc, rec: rec})
	out := voicePOST(t, srv.handleVoiceLive, webVoiceLivePath)

	if ct := out.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	events := sseEvents(out.Body.String())
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e[0])
	}
	want := []string{webVoiceLiveEventReady, webVoiceLiveEventDraft, webVoiceLiveEventDraft, webVoiceLiveEventFinal}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v, want %v", kinds, want)
	}

	var ready map[string]interface{}
	if err := json.Unmarshal([]byte(events[0][1]), &ready); err != nil {
		t.Fatalf("the ready frame is not JSON: %v", err)
	}
	if ready["device"] != "pulse:scripted-live-mic" {
		t.Fatalf("the ready frame names device %v, want the one the SERVER opened", ready["device"])
	}
	if ready["engine"] != config.AudioEngineSauc {
		t.Fatalf("the ready frame names engine %v, want %q", ready["engine"], config.AudioEngineSauc)
	}

	// THE DRAFTS ARE THE ARM'S TEXTS, IN ORDER, and each carries the carrier's
	// own escaped view beside the raw one (webVoiceDraftText is the ONE judge of
	// the slash hazard; the page must not re-implement it).
	var drafts []string
	for _, e := range events[1:3] {
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(e[1]), &d); err != nil {
			t.Fatalf("a draft frame is not JSON: %v", err)
		}
		drafts = append(drafts, d["text"].(string))
	}
	if strings.Join(drafts, "|") != "你好|你好世界" {
		t.Fatalf("draft texts = %v, want the arm's partials in order", drafts)
	}

	var final map[string]interface{}
	if err := json.Unmarshal([]byte(events[3][1]), &final); err != nil {
		t.Fatalf("the final frame is not JSON: %v", err)
	}
	if final["transcript"] != "你好世界。" || final["draft"] != "你好世界。" {
		t.Fatalf("final frame = %v, want the settled transcript", final)
	}
	if final["ok"] != true {
		t.Fatalf("final frame says ok=%v for a clean utterance", final["ok"])
	}
	if got, _ := final["partials"].(float64); int(got) != 2 {
		t.Fatalf("final frame reports %v partials, want 2", final["partials"])
	}
}

// TestVoiceLiveDraftArrivesBeforeStop is the feature's own claim, measured: the
// draft frames are on the wire WHILE the utterance is still open, i.e. before the
// stop request. It reads the recorder's growing body mid-run, which is what the
// page's streaming reader does.
func TestVoiceLiveDraftArrivesBeforeStop(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder([]string{"第一段", "第二段"}, "第一段第二段", 30*time.Millisecond)
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineStreaming, rec: rec})

	// A ResponseRecorder that reports each Flush, so "the bytes reached the wire"
	// is observed rather than inferred (a buffered body proves nothing about
	// timing).
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan string, 32)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleVoiceLive(w, httptest.NewRequest(http.MethodPost, webVoiceLivePath, nil))
	}()

	var sawDraftBeforeStop bool
	deadline := time.After(5 * time.Second)
	for !sawDraftBeforeStop {
		select {
		case <-w.flushed:
			if strings.Contains(w.bodyString(), "event: "+webVoiceLiveEventDraft) {
				sawDraftBeforeStop = true
			}
		case <-deadline:
			t.Fatal("no draft frame was flushed in 5s while the utterance was still open")
		}
	}
	// THE STOP ENDS IT: the final frame follows, and the handler returns.
	voicePOST(t, srv.handleVoiceLiveStop, webVoiceLiveStopPath)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end after the stop route was called")
	}
	if !strings.Contains(w.bodyString(), "event: "+webVoiceLiveEventFinal) {
		t.Fatalf("the ended stream carries no final frame:\n%s", w.bodyString())
	}
}

// flushRecorder is an httptest.ResponseRecorder that signals every Flush.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed chan string
	mu      sync.Mutex
}

// THE BODY IS READ WHILE THE HANDLER WRITES IT. The streamed frames are produced
// by the handler on ITS goroutine (handleVoiceLive, an SSE loop that never
// returns until the utterance ends) while this test reads ResponseRecorder.Body
// to look for a frame — a read and a write of the same bytes.Buffer with no
// happens-before between them. Measured 2026-10-27 (task 01a1756e),
// `go test ./cmd/agent -race`:
//
//	Write at … by bytes.(*Buffer).grow ← ResponseRecorder.WriteString ←
//	    writeSSEFrame (cli_web.go:1801) ← handleVoiceLive
//	Previous read at … by bytes.(*Buffer).String ← this test
//
// So Write takes the lock the readers take: bodyString() is the ONLY way this
// test looks at the body, and the lock is what turns "probably already written"
// into "written, and here it is".
func (f *flushRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ResponseRecorder.Write(p)
}

// bodyString returns what the stream has written so far.
func (f *flushRecorder) bodyString() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ResponseRecorder.Body.String()
}

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	f.ResponseRecorder.Flush()
	f.mu.Unlock()
	select {
	case f.flushed <- "":
	default:
	}
}

// ---------------------------------------------------------------- the refusal

// TestVoiceLiveRefusesWithoutALiveArm pins the fallback trigger: an engine with
// no live arm answers 501 BEFORE any byte of stream, naming the engine, so the
// page can take the existing start/stop path on a status code rather than by
// parsing a frame.
func TestVoiceLiveRefusesWithoutALiveArm(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder(nil, "", 0)
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineLocal, rec: rec})
	out := voicePOST(t, srv.handleVoiceLive, webVoiceLivePath)

	if out.Code != http.StatusNotImplemented {
		t.Fatalf("POST %s with the %s engine = %d, want 501", webVoiceLivePath, config.AudioEngineLocal, out.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, config.AudioEngineLocal) {
		t.Fatalf("the refusal does not name the engine: %q", msg)
	}
	if !strings.Contains(msg, webVoiceStopPath) {
		t.Fatalf("the refusal does not name the fallback route %q: %q", webVoiceStopPath, msg)
	}
	select {
	case <-rec.liveDone:
		t.Fatal("a refused engine ran the live arm anyway")
	case <-time.After(20 * time.Millisecond):
	}
}

// TestVoiceLiveRefusesOnMethodAndDisconnect pins the two other refusals the page
// can hit, because both are silent-failure shapes if they answer 200: a GET (the
// route exists, the verb does not) and a server that is shutting down.
func TestVoiceLiveRefusesOnMethodAndDisconnect(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder(nil, "", 0)
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineStreaming, rec: rec})

	get := httptest.NewRecorder()
	srv.handleVoiceLive(get, httptest.NewRequest(http.MethodGet, webVoiceLivePath, nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET %s = %d, want 405", webVoiceLivePath, get.Code)
	}

	srv.shuttingDown = true
	down := voicePOST(t, srv.handleVoiceLive, webVoiceLivePath)
	if down.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST %s while shutting down = %d, want 503", webVoiceLivePath, down.Code)
	}
}

// TestVoiceLiveOneMicrophone pins the shared device reservation: a live
// utterance and a window recording cannot hold the microphone at once, and the
// loser gets the conflict the window path already answers.
func TestVoiceLiveOneMicrophone(t *testing.T) {
	t.Parallel()
	rec := newFakeLiveRecorder([]string{"a"}, "a", 40*time.Millisecond)
	srv := newVoiceTestServer(livePipeline{engine: config.AudioEngineStreaming, rec: rec})

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleVoiceLive(&flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan string, 8)},
			httptest.NewRequest(http.MethodPost, webVoiceLivePath, nil))
	}()
	// Wait for the reservation to be taken, then compete for it.
	deadline := time.After(5 * time.Second)
	for {
		srv.voiceState.mu.Lock()
		held := srv.voiceState.live != nil
		srv.voiceState.mu.Unlock()
		if held {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the live route never reserved the microphone")
		case <-time.After(time.Millisecond):
		}
	}
	if got := voicePOST(t, srv.handleVoiceLive, webVoiceLivePath).Code; got != http.StatusConflict {
		t.Fatalf("a second live utterance = %d, want 409", got)
	}
	// The window route sees the same reservation.
	srv2 := srv
	if got := voicePOST(t, srv2.handleVoiceStart, webVoiceStartPath).Code; got != http.StatusConflict {
		t.Fatalf("POST %s while a live utterance holds the device = %d, want 409", webVoiceStartPath, got)
	}
	voicePOST(t, srv.handleVoiceLiveStop, webVoiceLiveStopPath)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end after stop")
	}
	srv.voiceState.mu.Lock()
	defer srv.voiceState.mu.Unlock()
	if srv.voiceState.live != nil {
		t.Fatal("the reservation was not released when the stream ended")
	}
}

// -------------------------------------------------------------------- the page

// TestVoiceLivePageCarriesTheRoutes pins the page's copy of the live wire: two
// routes, three event names and the fallback status. A page constant is not
// importable, so a rename on either side must fail HERE rather than as a button
// that posts to a 404 or a stream whose frames nobody recognises.
func TestVoiceLivePageCarriesTheRoutes(t *testing.T) {
	t.Parallel()
	page := webChatPage
	for _, want := range []string{
		"const API_VOICE_LIVE='" + webVoiceLivePath + "';",
		"const API_VOICE_LIVE_STOP='" + webVoiceLiveStopPath + "';",
		"const EV_LIVE_READY='" + webVoiceLiveEventReady + "'",
		"const EV_LIVE_DRAFT='" + webVoiceLiveEventDraft + "'",
		"const EV_LIVE_FINAL='" + webVoiceLiveEventFinal + "'",
		"const HTTP_NOT_IMPLEMENTED=" + itoaInt(http.StatusNotImplemented) + ";",
		`id="voicelive"`,
		"micDraftSet(payload.draft",
		"micInsert(body.draft)",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the embedded page does not carry %q — the page and the server disagree about the live voice wire", want)
		}
	}
	// THE STOP ROUTE MUST BE PREFIX-MATCHED AFTER THE STREAM'S, and the page's
	// fetch handler in the harness carries that ordering; here the shared VALUE
	// is pinned so a rename cannot make them equal by accident.
	if strings.TrimSuffix(webVoiceLiveStopPath, "/stop") != webVoiceLivePath {
		t.Fatalf("%q is not %q + \"/stop\" — the stop route must be the stream's child path",
			webVoiceLiveStopPath, webVoiceLivePath)
	}
}

// itoaInt is a tiny decimal spelling so the assertion above carries no magic
// string of its own (Rule 1).
func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
