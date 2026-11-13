package main

// web_voice_test.go — the microphone endpoints and the page↔server contract.
//
// THE FOUR THINGS THIS FILE IS ABOUT, and each is a way the carrier could be
// wrong while looking right:
//
//  1. THE SLASH HAZARD. A transcript is SPEECH, but a message whose first word is
//     "/something" is a LOCALLY DISPATCHED COMMAND on this carrier
//     (classifyChatLine → serveSlashCommand), so an engine returning "/new" for a
//     misheard utterance would silently reset the conversation instead of
//     drafting a message. webVoiceDraftText neutralises it into the carrier's own
//     literal-send form, and the tests below pin BOTH halves: the draft carries
//     the escape, and the escape classifies as text (not as a command).
//  2. THE TURN LOCK. A transcription runs on the same agent a turn streams from,
//     so it must not overlap one. The test holds s.mu for a while and proves the
//     transcript waits — and that the DEVICE work does not (a 1 s device probe
//     must never be charged to a live turn).
//  3. THE ONE PIPELINE. The web carrier must reach the device policy and the
//     engine seam, not restate them. The production pipeline is asserted to call
//     the shared functions (voiceListDevices / voiceResolveSelection /
//     transcribeFile) rather than assemble a second command line.
//  4. THE PAGE. The routes and the neutralisation constant are spelled twice —
//     once in Go, once in the embedded page — so a test pins the pair.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------- test pipeline

// fakeVoiceRecorder is a SCRIPTED capture: Round hands back one scripted
// transcript per call and then BLOCKS (a microphone that is still open delivers
// nothing until it is closed), Drain hands back the tail, Close releases. No device
// is opened and no host needs a sound card (Rule 10.4's minimal sample), while the
// carrier's own behaviour — every round kept, in order, the tail drained on stop —
// is what the tests observe.
// fakeVoiceRoundBound is how long a scripted round with nothing to say waits
// before reporting the end of the capture: the test stand-in for the window
// builder's own bound.
const fakeVoiceRoundBound = 120 * time.Millisecond

type fakeVoiceRecorder struct {
	mu       sync.Mutex
	rounds   []string
	roundErr error
	tail     []string
	next     int
	calls    int
	closed   bool
	released chan struct{}
	// started is replaced-and-closed on every call count change: a waiter reads
	// it under mu and then selects on it, so it resumes the instant a round
	// arrives instead of polling on a fixed interval.
	started  chan struct{}
	closedAt time.Time
}

func newFakeVoiceRecorder(rounds ...string) *fakeVoiceRecorder {
	return &fakeVoiceRecorder{rounds: rounds, released: make(chan struct{}), started: make(chan struct{})}
}

func (f *fakeVoiceRecorder) Round(_ *voiceOptions, seconds int) (string, error) {
	f.mu.Lock()
	f.calls++
	close(f.started) // wake every waiter: the count moved
	f.started = make(chan struct{})
	if f.roundErr != nil {
		err := f.roundErr
		f.mu.Unlock()
		return "", err
	}
	if f.next < len(f.rounds) {
		i := f.next
		f.next++
		f.mu.Unlock()
		return f.rounds[i], nil
	}
	f.mu.Unlock()
	// THE OPEN MICROPHONE: no more scripted audio, so this round ends the way a
	// real one does when the speaker pauses — at the bound, with nothing to say.
	// (Released early by Close, so a stop never waits out the bound in a test.)
	select {
	case <-f.released:
	case <-time.After(fakeVoiceRoundBound):
	}
	return "", errVoiceStreamClosed
}

func (f *fakeVoiceRecorder) Drain() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tail...)
}

func (f *fakeVoiceRecorder) Close() {
	f.mu.Lock()
	already := f.closed
	f.closed = true
	f.closedAt = time.Now()
	f.mu.Unlock()
	if !already {
		close(f.released)
	}
}

// fakeVoicePipeline stands in for the microphone. It records what it was asked,
// so a test can prove the order the handler did things in.
type fakeVoicePipeline struct {
	mu sync.Mutex

	openErr   error
	openNotes []string
	device    string
	recorder  *fakeVoiceRecorder

	opened int
	openAt time.Time
}

func (f *fakeVoicePipeline) Options() (*voiceOptions, error) { return &voiceOptions{}, nil }

func (f *fakeVoicePipeline) Open(*voiceOptions) (webVoiceRecorder, string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened++
	f.openAt = time.Now()
	if f.openErr != nil {
		return nil, "", f.openNotes, f.openErr
	}
	device := f.device
	if device == "" {
		device = "pulse:scripted-mic"
	}
	if f.recorder == nil {
		f.recorder = newFakeVoiceRecorder()
	}
	return f.recorder, device, f.openNotes, nil
}

// newVoiceTestServer is a webServer carrying one scripted pipeline.
func newVoiceTestServer(pipe webVoicePipeline) *webServer {
	return &webServer{voice: pipe}
}

// waitForRounds blocks until the recorder has been asked for at least n rounds —
// the test's stand-in for "the operator spoke before they clicked stop".
//
// IT WAITS ON THE RECORDER'S OWN WAKE-UP CHANNEL, never on a fixed interval: the
// loop resumes the instant a round arrives, so the test is neither slower than
// the work nor a poll that a different machine's timing can outrun (stylegate's
// test-sleep rule asks for exactly this — "poll/wait instead").
func waitForRounds(t *testing.T, r *fakeVoiceRecorder, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		calls := r.calls
		started := r.started
		r.mu.Unlock()
		if calls >= n {
			return
		}
		select {
		case <-started:
		case <-deadline:
			t.Fatalf("the recorder was asked for %d rounds in 5s, want at least %d", calls, n)
		}
	}
}

// voicePOST drives one endpoint the way the page does.
func voicePOST(t *testing.T, h http.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, path, nil))
	return rec
}

// voiceResult decodes a stop answer.
func voiceResult(t *testing.T, rec *httptest.ResponseRecorder) webVoiceStopResult {
	t.Helper()
	var out webVoiceStopResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the stop answer is not valid JSON (%v): %s", err, rec.Body.String())
	}
	return out
}

// ------------------------------------------------------------------ the hazard

// TestVoiceDraftNeutralisesTheSlashHazard is THE red-proofable case: a transcript
// beginning with "/" must reach the composer as TEXT, not as a command.
//
// IT IS RED WITHOUT THE FIX: dropping the escape makes the first arm report
// "/new" (a command-shaped line) instead of "//new", and the classification arm
// below then turns red too — the two arms are the two halves of one contract.
func TestVoiceDraftNeutralisesTheSlashHazard(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		transcript string
		want       string
		why        string
	}{
		{"a command word", "/new", "//new", "a spoken pattern that looks like /new must be text"},
		{"a known command with arguments", "/task list", "//task list", "the whole line is text, arguments included"},
		{"an unknown command", "/nonexistent thing", "//nonexistent thing", "the classifier refuses unknown commands too — so this is a hazard as well"},
		{"already escaped", "//new", "//new", "the engine's own literal form is left alone (it un-escapes to /new, which is what was said)"},
		{"plain speech", "hello there", "hello there", "ordinary speech is untouched"},
		{"a quote-like slash mid-line", "say /new please", "say /new please", "only the FIRST word decides the route"},
		{"silence", "/sil", "", "the engine's silence marker is not a draft"},
		{"repeated silence marker", "/sil/sil", "", "one marker per chunk is still silence"},
		{"empty", "   ", "", "whitespace is not a draft"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := webVoiceDraftText(c.transcript)
			if got != c.want {
				t.Fatalf("webVoiceDraftText(%q) = %q, want %q (%s)", c.transcript, got, c.want, c.why)
			}
		})
	}
}

// TestVoiceDraftIsCarriedAsTextNotAsACommand closes the loop through the SHARED
// classifier: the draft the page inserts must classify as the model's text. A
// neutralisation the carrier does not honour would be a decoration.
func TestVoiceDraftIsCarriedAsTextNotAsACommand(t *testing.T) {
	t.Parallel()
	for _, spoken := range []string{"/new", "/task list", "/help"} {
		draft := webVoiceDraftText(spoken)
		line := classifyChatLine(draft, sharedSlashWordKnown)
		if line.kind != chatLineText && line.kind != chatLineEscape {
			t.Fatalf("draft %q (from %q) classifies as %v — it would be executed as a command, not sent to the model", draft, spoken, line.kind)
		}
		if line.text != spoken {
			t.Fatalf("draft %q un-escapes to %q, want the spoken %q", draft, line.text, spoken)
		}
		// And the OTHER half: the raw transcript WOULD have been a command.
		if raw := classifyChatLine(spoken, sharedSlashWordKnown); raw.kind == chatLineText {
			t.Fatalf("%q does not classify as a command — the hazard this test exists for has moved", spoken)
		}
	}
}

// readFileString reads a file in the package directory for the source-shape gate.
func readFileString(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// lookPathNode finds node (or nodejs) for the page-harness gate.
func lookPathNode() (string, error) {
	node, err := exec.LookPath("node")
	if err != nil {
		node, err = exec.LookPath("nodejs")
	}
	return node, err
}

// runNode runs the page harness with the given arguments.
func runNode(t *testing.T, node string, args ...string) ([]byte, error) {
	t.Helper()
	return exec.Command(node, args...).CombinedOutput()
}

// ------------------------------------------------------------------ the routes

// TestVoiceStartStopRoundTrip drives the two endpoints over the REAL mux, with a
// scripted pipeline: start opens a capture, stop glues every window into one
// recording, and the answer is a draft.
func TestVoiceStartStopRoundTrip(t *testing.T) {
	t.Parallel()
	pipe := &fakeVoicePipeline{
		recorder: newFakeVoiceRecorder("what is on the", "build queue"),
	}
	s := newVoiceTestServer(pipe)
	mux := s.webChatHandler()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, webVoiceStartPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("start answered %d: %s", rec.Code, rec.Body.String())
	}
	var started map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("start answer is not JSON: %v", err)
	}
	if started["device"] != "pulse:scripted-mic" {
		t.Fatalf("start answer names the device %v, want the pipeline's", started["device"])
	}
	if pipe.opened != 1 {
		t.Fatalf("start opened %d captures, want 1", pipe.opened)
	}

	// THE OPERATOR SPEAKS FIRST: wait until both scripted rounds have been handed
	// over, so the stop below is a stop AFTER speech. (Stopping before the first
	// round returns is the separate, honest case "nothing was said".)
	waitForRounds(t, pipe.recorder, 2)

	stop := voicePOST(t, mux.ServeHTTP, webVoiceStopPath)
	if stop.Code != http.StatusOK {
		t.Fatalf("stop answered %d: %s", stop.Code, stop.Body.String())
	}
	out := voiceResult(t, stop)
	if !out.OK || out.Draft != "what is on the build queue" {
		t.Fatalf("stop answered %+v, want the rounds' transcripts joined as the draft", out)
	}
	if out.Truncated || out.TailDropped {
		t.Fatalf("an ordinary recording was reported truncated (%+v)", out)
	}
	// EVERY ROUND'S TRANSCRIPT REACHED THE DRAFT, in stream order, and the capture
	// was released exactly once — a device that outlived the click is the defect.
	pipe.recorder.mu.Lock()
	closed := pipe.recorder.closed
	pipe.recorder.mu.Unlock()
	if !closed {
		t.Fatal("the capture was never released — the microphone outlived the click")
	}
}

// TestVoiceStartRefusesASecondRecording: one microphone, one start/stop pair.
// A page that double-clicked, or two tabs, must be told — never silently given a
// second capture that the first stop would leave running forever.
func TestVoiceStartRefusesASecondRecording(t *testing.T) {
	t.Parallel()
	pipe := &fakeVoicePipeline{recorder: newFakeVoiceRecorder("audio")}
	s := newVoiceTestServer(pipe)
	mux := s.webChatHandler()

	first := voicePOST(t, mux.ServeHTTP, webVoiceStartPath)
	if first.Code != http.StatusOK {
		t.Fatalf("the first start answered %d", first.Code)
	}
	second := voicePOST(t, mux.ServeHTTP, webVoiceStartPath)
	if second.Code != http.StatusConflict {
		t.Fatalf("a second start answered %d, want 409: %s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), "already running") {
		t.Fatalf("the refusal does not say what is wrong: %s", second.Body.String())
	}
	if pipe.opened != 1 {
		t.Fatalf("a refused second start opened %d captures, want 1", pipe.opened)
	}
	// And a refused start must not have replaced the live recording: stopping
	// still answers the FIRST one's draft.
	//
	// WHERE THE ROUND IS WHEN THE CLICK LANDS IS PART OF THE RECORDING, NOT A
	// DETAIL THE TEST MAY ASSUME AWAY. fakeVoiceRecorder.Round banks its scripted
	// round ("audio") the instant it is called, so the same recording reaches
	// stop in two shapes:
	//
	//	round already returned -> Drain() = [tail],      texts = ["audio"]
	//	round still in flight  -> Drain() = [tail]+["audio"], texts = []
	//
	// and webVoiceSession builds ONE transcript from BOTH halves — record appends
	// each round it gets, finish joins what the goroutine collected (transcript)
	// with what Drain still held. Asserting "the tail and nothing else" therefore
	// pinned a SCHEDULING ORDER rather than the carrier's behaviour: it held in
	// isolation, where start's handler returns before the goroutine is scheduled,
	// and failed inside a full-package run, where the goroutine has already banked
	// "audio" and the honest answer reads `audio the first recording` (measured
	// red at exactly this line, 3/3, by waiting for that round deliberately).
	//
	// So the test now drives the loaded ordering itself and asserts the STRONGER
	// fact the refusal is about: the live recording SURVIVED the refused start
	// with everything it had banked, and the whole of it is what stop answers —
	// where it sat relative to the click no longer matters.
	waitForRounds(t, pipe.recorder, 2)
	pipe.recorder.mu.Lock()
	pipe.recorder.tail = []string{"the first recording"}
	pipe.recorder.mu.Unlock()
	out := voiceResult(t, voicePOST(t, mux.ServeHTTP, webVoiceStopPath))
	const want = "audio the first recording"
	if out.Transcript != want {
		t.Fatalf("stop answered %q, want the first recording's whole transcript %q", out.Transcript, want)
	}
	if out.Draft != want {
		t.Fatalf("stop's draft is %q, want the first recording's transcript %q", out.Draft, want)
	}
}

// TestVoiceStopWithoutARecording: the second click of an already-finished pair is
// an error the page can SHOW, not a 200 with an empty draft.
func TestVoiceStopWithoutARecording(t *testing.T) {
	t.Parallel()
	s := newVoiceTestServer(&fakeVoicePipeline{})
	rec := voicePOST(t, s.webChatHandler().ServeHTTP, webVoiceStopPath)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stop without a recording answered %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// TestVoiceMethodsArePostOnly pins the verb of both routes: a GET is a browser
// prefetch or an <img> probe, never a recording.
func TestVoiceMethodsArePostOnly(t *testing.T) {
	t.Parallel()
	s := newVoiceTestServer(&fakeVoicePipeline{})
	mux := s.webChatHandler()
	for _, path := range []string{webVoiceStartPath, webVoiceStopPath} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s answered %d, want 405", path, rec.Code)
		}
	}
}

// TestVoiceStartReportsTheDeviceRefusal: a device/engine refusal reaches the page
// as the SERVER's own sentence (Rule 15 — the operator must be able to act on it),
// and it does not leave a session behind.
func TestVoiceStartReportsTheDeviceRefusal(t *testing.T) {
	t.Parallel()
	pipe := &fakeVoicePipeline{
		openErr:   errors.New("no capture device: arecord found no card"),
		openNotes: []string{"the system default capture is unreachable"},
	}
	s := newVoiceTestServer(pipe)
	rec := voicePOST(t, s.webChatHandler().ServeHTTP, webVoiceStartPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a refused device answered %d, want 503", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}
	if got := body["error"]; got != "no capture device: arecord found no card" {
		t.Fatalf("the refusal sentence is %v, want the pipeline's own", got)
	}
	// Nothing is left armed: the second start must be allowed.
	if again := voicePOST(t, s.webChatHandler().ServeHTTP, webVoiceStartPath); again.Code == http.StatusConflict {
		t.Fatalf("a refused start left a session armed: %s", again.Body.String())
	}
}

// TestVoiceStartRefusedWhileShuttingDown: the interlock handleChat uses, so a
// start queued behind the finalizer cannot open a device for a dying server.
func TestVoiceStartRefusedWhileShuttingDown(t *testing.T) {
	t.Parallel()
	s := newVoiceTestServer(&fakeVoicePipeline{})
	s.shuttingDown = true
	rec := voicePOST(t, s.webChatHandler().ServeHTTP, webVoiceStartPath)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("start during shutdown answered %d, want 503", rec.Code)
	}
}

// ------------------------------------------------------------------ the turn lock

// TestVoiceTranscriptionWaitsForTheTurnLock is the serialisation requirement,
// measured: with s.mu held (a turn streaming), the stop endpoint must not reach
// the engine until the lock is released.
func TestVoiceTranscriptionWaitsForTheTurnLock(t *testing.T) {
	t.Parallel()
	pipe := &fakeVoicePipeline{recorder: newFakeVoiceRecorder("draft")}
	s := newVoiceTestServer(pipe)
	mux := s.webChatHandler()

	if rec := voicePOST(t, mux.ServeHTTP, webVoiceStartPath); rec.Code != http.StatusOK {
		t.Fatalf("start answered %d: %s", rec.Code, rec.Body.String())
	}
	waitForRounds(t, pipe.recorder, 1)

	// A turn is in flight.
	s.mu.Lock()
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() { answered <- voicePOST(t, mux.ServeHTTP, webVoiceStopPath) }()

	// While the lock is held the capture must NOT have been released (the release
	// is inside finish, i.e. inside the lock).
	select {
	case <-pipe.recorder.released:
		s.mu.Unlock()
		t.Fatal("the capture was released while a turn held s.mu — the finish raced an in-flight turn")
	case <-time.After(300 * time.Millisecond):
	}
	s.mu.Unlock()
	select {
	case rec := <-answered:
		if rec.Code != http.StatusOK {
			t.Fatalf("stop answered %d after the lock was released: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stop never answered after the turn lock was released")
	}
	pipe.recorder.mu.Lock()
	defer pipe.recorder.mu.Unlock()
	if !pipe.recorder.closed {
		t.Fatal("the capture was never released after the lock was released")
	}
}

// TestVoiceStartDoesNotTakeTheTurnLock is the OTHER half, and it is the reason
// the lock is taken at stop: opening a device is a ~1 s probe that touches no
// agent state, so a live turn must not be queued behind it.
func TestVoiceStartDoesNotTakeTheTurnLock(t *testing.T) {
	t.Parallel()
	pipe := &fakeVoicePipeline{}
	s := newVoiceTestServer(pipe)
	mux := s.webChatHandler()

	s.mu.Lock()
	done := make(chan int, 1)
	go func() { done <- voicePOST(t, mux.ServeHTTP, webVoiceStartPath).Code }()
	select {
	case code := <-done:
		if code != http.StatusOK {
			s.mu.Unlock()
			t.Fatalf("start answered %d while a turn held s.mu, want 200", code)
		}
	case <-time.After(5 * time.Second):
		s.mu.Unlock()
		t.Fatal("start blocked on the turn lock — a device probe was charged to a live turn")
	}
	s.mu.Unlock()
	// The recording this arm opened is closed the way the server closes one: the
	// device must not outlive the test (the suite's goroutine sweep counts it).
	s.stopVoiceRecording()
}

// ------------------------------------------------------------ the one pipeline

// TestVoiceProductionPipelineUsesTheSharedSeams is the Rule 13/14 gate: the web
// carrier must not assemble a second capture or a second engine call. The
// production pipeline's methods are asserted to be thin wrappers over the shared
// functions — their source is checked for the calls they must make and the argv
// spelling they must NOT contain.
//
// A source check rather than an execution check is the honest instrument here:
// the alternative (running a real recorder) needs a microphone, and what this
// test is about is that no SECOND device policy was written, which is a fact
// about the code, not about a device.
func TestVoiceProductionPipelineUsesTheSharedSeams(t *testing.T) {
	t.Parallel()
	src := stripGoComments(readFileString(t, "web_voice.go"))
	for _, want := range []string{
		"chatVoiceRecorder(",
		"rec.Select(",
		"chatVoiceResolveOptions(",
		"voiceDepsReady(",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("web_voice.go does not call %s — the web carrier must reach the shared seam, not restate it", want)
		}
	}
	for _, banned := range []string{
		"exec.Command",   // the recorder's argv belongs to voiceRecorderCommand
		"parecord",       // a device command assembled in the web layer
		"arecord",        //
		"--device",       // the four-tier policy is voiceResolveSelection's
		"--audio-source", //
	} {
		if strings.Contains(src, banned) {
			t.Fatalf("web_voice.go mentions %q — the web layer must not assemble a device command or a second device policy", banned)
		}
	}
}

// TestVoicePageCarriesTheRoutes pins the TWO carriers' shared vocabulary: the page
// spells the routes and the escape itself (a page constant is not importable), so
// the Go side must agree — otherwise the button posts to a 404 or the neutralised
// draft is a form the server does not un-escape.
func TestVoicePageCarriesTheRoutes(t *testing.T) {
	t.Parallel()
	page := webChatPage
	for _, want := range []string{
		"const API_VOICE_START='" + webVoiceStartPath + "';",
		"const API_VOICE_STOP='" + webVoiceStopPath + "';",
		`id="mic"`,
		"micInsert(body.draft)",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the embedded page does not carry %q — the page and the server disagree about the voice wire", want)
		}
	}
	if chatSlashEscape != "//" {
		t.Fatalf("chatSlashEscape is %q; the page's comment and this test assume %q", chatSlashEscape, "//")
	}
}

// voiceScenarioName is the selector this gate asks the harness to run alone.
//
// It is a CONSTANT (Rule 1/13/14), and its VALUE matters: it used to be the
// inline literal "voice:", which matched exactly ONE scenario — the one-shot
// route. The streaming route's scenario is named "voice live: …" (no colon after
// "voice"), so indexOf("voice:") missed it and that path was driven by nothing.
// The colon is dropped on purpose: "voice" matches BOTH scenarios (measured: 2
// hits, 24 checks), which is what this gate's own name already claims.
const voiceScenarioName = "voice"

// TestVoicePageHarnessRunsTheVoiceScenario drives the SHIPPED page's mic button
// under node (the harness the M2 increments use), scoped to the voice scenarios
// so this gate reports the increment it is about.
func TestVoicePageHarnessRunsTheVoiceScenario(t *testing.T) {
	node, err := lookPathNode()
	if err != nil {
		t.Skip("skipping the page-harness voice gate: no `node` on PATH (the Go-side contract tests above still run)")
	}
	out, runErr := runNode(t, node, pageHarnessPath, "--only", voiceScenarioName)
	text := string(out)
	// "FAIL" without the colon is the harness's per-check marker AND its
	// empty-match refusal, so the bare prefix catches both a red check and a
	// selector that matched no scenario (which would otherwise look green).
	if !strings.Contains(text, menuHarnessPass) || strings.Contains(text, menuHarnessFail) || runErr != nil {
		t.Fatalf("the voice page scenario did not pass (err=%v):\n%s", runErr, text)
	}
	t.Logf("voice page scenario:\n%s", text)
}
