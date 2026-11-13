package main

// voice_live_engines_test.go — the ENGINE SEAM's own cases: the dispatch truth
// table, the endpoint decision, and the shape of ONE utterance fed through a
// double, so the runner's feed/partial/settle obligations are pinned without a
// network, a credential or a microphone.
//
// WHY THE DISPATCH TABLE IS A CASE AND NOT A COMMENT. "Which engines get the live
// arm" is the whole of task 01a13303's operator-visible change: the v3 engine used
// to fall through to the six-second window loop, and every engine the arm does NOT
// select behaves exactly as it did. A table asserted for EVERY engine value is the
// only form in which "I changed one engine's treatment" is checkable.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaosauc"
	"github.com/nick/gopi/internal/doubaostream"
)

// TestVoiceLiveEngineSelected_TruthTable pins the selection for EVERY engine: the
// two streaming channels run the live arm, and every other engine keeps the
// behaviour it had.
func TestVoiceLiveEngineSelected_TruthTable(t *testing.T) {
	want := map[string]bool{
		config.AudioEngineStreaming: true,
		config.AudioEngineSauc:      true,
		config.AudioEngineLocal:     false,
		config.AudioEngineRecording: false,
	}
	for _, engine := range voiceEngineValues {
		got := voiceLiveEngineSelected(&voiceOptions{engine: engine})
		expected, known := want[engine]
		if !known {
			t.Fatalf("engine %q is not in this case's table — a new engine must state whether it runs the live arm", engine)
		}
		if got != expected {
			t.Errorf("voiceLiveEngineSelected(%q) = %v, want %v", engine, got, expected)
		}
	}
	// THE TABLE IS COMPLETE: an engine added to the vocabulary without a decision
	// here would otherwise be selected by default or silently ignored.
	if len(want) != len(voiceEngineValues) {
		t.Fatalf("the case covers %d engines and the vocabulary has %d: %v", len(want), len(voiceEngineValues), voiceEngineValues)
	}
}

// TestVoiceLiveEngineFor_BothStreamingEnginesResolveABackendAndItsOptions: the
// seam must hand back the ENGINE's OWN options type — the adapters assert it, and
// a run handed the wrong type would open a session with a zero credential and
// report the resulting refusal as the operator's mistake.
func TestVoiceLiveEngineFor_BothStreamingEnginesResolveABackendAndItsOptions(t *testing.T) {
	t.Setenv(config.SettingsPathEnv, filepath.Join(t.TempDir(), "settings.json"))

	streaming, opts, err := voiceLiveEngineFor(&voiceOptions{engine: config.AudioEngineStreaming,
		doubaoKeyFile: writeLiveCredentialFile(t)})
	if err != nil {
		t.Fatalf("the %s engine did not resolve a live backend: %v", config.AudioEngineStreaming, err)
	}
	if streaming.Open == nil || streaming.IsRaceRefusal == nil || streaming.ConfigurePartial == nil {
		t.Fatalf("the %s backend is incomplete: %+v", config.AudioEngineStreaming, streaming)
	}
	if _, ok := opts.(doubaostream.Options); !ok {
		t.Fatalf("the %s engine was handed %T, want doubaostream.Options", config.AudioEngineStreaming, opts)
	}
	if !streaming.IsRaceRefusal(doubaostream.ErrSessionFinished) || streaming.IsRaceRefusal(errors.New("reset")) {
		t.Errorf("the v2 backend's race classifier does not name exactly the v2 sentinels")
	}

	sauc, sopts, err := voiceLiveEngineFor(&voiceOptions{engine: config.AudioEngineSauc,
		doubaoKeyFile: writeLiveCredentialFile(t)})
	if err != nil {
		t.Fatalf("the %s engine did not resolve a live backend: %v", config.AudioEngineSauc, err)
	}
	v3, ok := sopts.(doubaosauc.Options)
	if !ok {
		t.Fatalf("the %s engine was handed %T, want doubaosauc.Options", config.AudioEngineSauc, sopts)
	}
	// THE LIVE ARM OPENS THE BIDIRECTIONAL ENDPOINT unless the settings say
	// otherwise: the file-semantics one answers once, at the end (see
	// voiceSaucLiveEndpoint).
	if v3.Endpoint != doubaosauc.EndpointBidirectional {
		t.Errorf("the %s live arm would open %q, want the bidirectional endpoint %q",
			config.AudioEngineSauc, v3.Endpoint, doubaosauc.EndpointBidirectional)
	}
	if v3.AudioFile != "" {
		t.Errorf("the live arm carries an audio FILE (%q): a live session is fed by Say, not opened on a path", v3.AudioFile)
	}
	if !sauc.IsRaceRefusal(doubaosauc.ErrSessionClosed) || sauc.IsRaceRefusal(errors.New("reset")) {
		t.Errorf("the v3 backend's race classifier does not name exactly the v3 sentinels")
	}

	// AN ENGINE WITH NO LIVE ARM IS REFUSED BY NAME, not by a nil call.
	if _, _, err := voiceLiveEngineFor(&voiceOptions{engine: config.AudioEngineLocal}); err == nil {
		t.Errorf("the %s engine resolved a live backend", config.AudioEngineLocal)
	} else if !strings.Contains(err.Error(), config.AudioEngineLocal) {
		t.Errorf("the refusal does not name the engine: %v", err)
	}
}

// TestVoiceSaucEndpointDefaults_EachArmGetsWhatItsContractNeeds is the ONE
// endpoint decision, asserted from both sides: the file arm keeps the
// file-semantics default and the live arm takes the bidirectional one, while a
// configured endpoint BEATS both. The two defaults are different on purpose, and a
// reading that took them for a contradiction is what this case answers.
func TestVoiceSaucEndpointDefaults_EachArmGetsWhatItsContractNeeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	t.Setenv(config.SettingsPathEnv, path)

	// Neither arm can even be asked without this file; it supplies the v3 pair.
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if got := voiceSaucEffectiveEndpoint(); got != doubaosauc.DefaultEndpoint {
		t.Errorf("the FILE arm's endpoint = %q, want the package default %q", got, doubaosauc.DefaultEndpoint)
	}
	if got := voiceSaucLiveEndpoint(); got != doubaosauc.EndpointBidirectional {
		t.Errorf("the LIVE arm's endpoint = %q, want %q", got, doubaosauc.EndpointBidirectional)
	}
	if doubaosauc.DefaultEndpoint == doubaosauc.EndpointBidirectional {
		t.Fatal("the two arms' defaults are one value: this case would then assert nothing")
	}

	// A CONFIGURED endpoint wins on BOTH arms, verbatim.
	configured := "wss://example.invalid/api/v3/sauc/bigmodel_async"
	body := `{"audio_engine_param":{"sauc":{"endpoint":"` + configured + `"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if got := voiceSaucEffectiveEndpoint(); got != configured {
		t.Errorf("the file arm ignored the configured endpoint: %q", got)
	}
	if got := voiceSaucLiveEndpoint(); got != configured {
		t.Errorf("the live arm ignored the configured endpoint: %q", got)
	}
}

// ── the fake session: ONE utterance through the runner ─────────────────

// fakeLiveSession is a voiceLiveSession double. Its Say records what was handed
// over and can refuse named ways; its Finish answers a scripted result.
type fakeLiveSession struct {
	mu      sync.Mutex
	said    [][]byte
	tailLen int
	sayErr  error
	partial int
	result  *voiceLiveResult
	finErr  error
	timed   bool
	closed  int
	// onPartial is installed by the backend's ConfigurePartial.
	onPartial func(voiceLivePartial)
	// emitOnSay makes the first accepted packet publish one partial, the way a
	// live session's reader does.
	emitOnSay bool
}

func (f *fakeLiveSession) Say(pcm []byte) error {
	f.mu.Lock()
	err := f.sayErr
	if err == nil {
		f.said = append(f.said, append([]byte(nil), pcm...))
	}
	cb := f.onPartial
	emit := f.emitOnSay && len(f.said) == 1
	f.mu.Unlock()
	if err != nil {
		return err
	}
	// THE PARTIAL ARRIVES WHILE THE AUDIO IS STILL GOING: that is what a live
	// session does (its reader publishes the text between packets), so the double
	// publishes it here rather than on a timer no case should depend on.
	if emit && cb != nil {
		cb(voiceLivePartial{Index: 1, Text: "你好", Definite: false})
	}
	return nil
}

func (f *fakeLiveSession) Finish(tail []byte) (*voiceLiveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tailLen = len(tail)
	return f.result, f.finErr
}

func (f *fakeLiveSession) PartialCount() int { return f.partial }

func (f *fakeLiveSession) FinishTimedOut() bool { return f.timed }

func (f *fakeLiveSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

// fakeBackend wires a fakeLiveSession into the runner's backend seam, so the
// utterance can be driven end to end without an engine.
func fakeBackend(sess *fakeLiveSession) voiceLiveBackend {
	return voiceLiveBackend{
		Open: func(context.Context, any) (voiceLiveSession, error) { return sess, nil },
		IsRaceRefusal: func(err error) bool {
			return errors.Is(err, doubaosauc.ErrSessionFinished)
		},
		ConfigurePartial: func(opts any, onPartial func(voiceLivePartial)) any {
			sess.mu.Lock()
			sess.onPartial = onPartial
			sess.mu.Unlock()
			return opts
		},
	}
}

// TestVoiceLiveRunner_FeedsPartialSettlesAndReportsTheShape is the whole
// utterance over a session double: every BYTE the capture produced reaches the
// session (nothing spoken is dropped), a partial is stamped and forwarded, the
// audio in hand goes up with the end-of-stream flag, and the report carries the
// engine's own figures.
func TestVoiceLiveRunner_FeedsPartialSettlesAndReportsTheShape(t *testing.T) {
	stream := liveStream()
	sess := &fakeLiveSession{
		emitOnSay: true,
		result:    &voiceLiveResult{Text: "  你好世界  ", LogID: "log-1", AudioMS: 600, Elapsed: 700 * time.Millisecond},
	}
	var seen []voiceLivePartial

	// THE CAPTURE IS PRE-LOADED AND THEN CLOSED: one packet and a half, so the half
	// must leave through Finish's tail rather than through Say. The channel is a
	// buffered one the feeder owns, so nothing here races the runner's goroutine.
	stream.chunks <- make([]byte, voiceLivePackets.Bytes)
	stream.chunks <- make([]byte, voiceLivePackets.Bytes/2)
	close(stream.chunks)

	run := runVoiceLiveSession(stream, voiceLiveRunConfig{
		Backend:   fakeBackend(sess),
		Options:   "opaque",
		OnPartial: func(p voiceLivePartial) { seen = append(seen, p) },
	})
	if run.error() != nil {
		t.Fatalf("the utterance failed: %v", run.error())
	}
	if run.Text != "你好世界" {
		t.Errorf("the transcript = %q, want it trimmed", run.Text)
	}
	if run.Result == nil || run.Result.LogID != "log-1" {
		t.Errorf("the engine's own result did not reach the report: %+v", run.Result)
	}
	if len(seen) != 1 || seen[0].Text != "你好" {
		t.Errorf("the partial did not reach the surface: %+v", seen)
	}
	// THE STAMP IS THE ASSERTION, not the millisecond difference: this double
	// emits its partial in the same instant the packet goes out, so the latency
	// rounds to 0 ms — and a case that required a non-zero duration would be
	// asserting its own scheduler rather than the runner. What must hold is that
	// the partial passed through the ONE place the clock is stamped.
	if run.Clock.firstPartialWall().IsZero() {
		t.Errorf("the first partial was not timed: the clock is the surface's latency figure")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if len(sess.said) != 1 || len(sess.said[0]) != voiceLivePackets.Bytes {
		t.Errorf("the session was handed %d packets (%v bytes), want exactly one of %d",
			len(sess.said), sizesOf(sess.said), voiceLivePackets.Bytes)
	}
	if sess.tailLen != voiceLivePackets.Bytes/2 {
		t.Errorf("Finish's tail carried %d bytes, want the %d that never completed a packet",
			sess.tailLen, voiceLivePackets.Bytes/2)
	}
	if sess.closed == 0 {
		t.Errorf("the session was never closed: a live session left open is a billed connection nobody owns")
	}
}

// TestVoiceLiveRunner_ANamedRaceRefusalIsNotReported: the engine names its own
// "already finished" sentinel through the backend, and the feeder must drop such a
// packet without recording a failure — while a real transport failure is still
// reported.
func TestVoiceLiveRunner_ANamedRaceRefusalIsNotReported(t *testing.T) {
	// THE CAPTURE DELIVERS ONE WHOLE PACKET, so the feeder really calls Say —
	// otherwise this case would pass by never exercising the refusal at all.
	raced := raceCase(doubaosauc.ErrSessionFinished)
	if raced.SendErr != nil {
		t.Fatalf("a packet refused because the utterance had ended was reported as a failure: %v", raced.SendErr)
	}
	broken := raceCase(errors.New("connection reset by peer"))
	if broken.SendErr == nil {
		t.Fatal("a real transport failure was swallowed — the operator would be told the utterance was transcribed")
	}
}

// raceCase runs ONE utterance whose session refuses the first packet with err.
func raceCase(err error) voiceLiveRun {
	stream := liveStream()
	sess := &fakeLiveSession{sayErr: err, result: &voiceLiveResult{Text: "好"}}
	stream.chunks <- make([]byte, voiceLivePackets.Bytes)
	close(stream.chunks)
	return runVoiceLiveSession(stream, voiceLiveRunConfig{Backend: fakeBackend(sess), Options: "opaque"})
}

// TestVoiceLiveRunner_RefusesARunWithNoBackend: the seam's own contract, stated
// rather than assumed. A run with no engine is a programming error, and it must be
// NAMED instead of opening nothing and reporting an empty transcript.
func TestVoiceLiveRunner_RefusesARunWithNoBackend(t *testing.T) {
	run := runVoiceLiveSession(liveStream(), voiceLiveRunConfig{Stop: closedChan()})
	if !errors.Is(run.error(), errVoiceLiveNoBackend) {
		t.Fatalf("a run with no backend reported %v, want the named refusal", run.error())
	}
}

// TestVoiceLiveOpts_RefuseTheWrongEnginesOptions: the adapters' assertions, so a
// backend handed another engine's options refuses instead of opening a session
// with a zero credential (which would come back as an authentication failure and
// read like the operator's mistake).
func TestVoiceLiveOpts_RefuseTheWrongEnginesOptions(t *testing.T) {
	if _, err := voiceLiveOptsV2(doubaosauc.Options{}); err == nil {
		t.Errorf("the v2 adapter accepted the v3 engine's options")
	}
	if _, err := voiceLiveOptsV3(doubaostream.Options{}); err == nil {
		t.Errorf("the v3 adapter accepted the v2 engine's options")
	}
}

// TestVoiceStreamError_NamesTheEngineThatFailed: one failure, two channels, and the
// sentence must name the one it happened on — "doubao-stream" over a v3 run sends
// the operator to the wrong endpoint.
func TestVoiceStreamError_NamesTheEngineThatFailed(t *testing.T) {
	base := errors.New("server: code 45000030")
	msg := voiceLiveErrorFor(config.AudioEngineSauc, base, nil)
	if !strings.Contains(msg.Error(), config.AudioEngineSauc) {
		t.Errorf("the failure does not name the v3 engine: %v", msg)
	}
	if !errors.Is(msg, base) {
		t.Errorf("the failure stopped carrying the vendor's own error: %v", msg)
	}
	// The v3 no-speech verdict keeps its own type reachable.
	v3NoSpeech := &doubaosauc.NoSpeechError{Message: "no valid speech found", LogID: "l"}
	err := voiceLiveErrorFor(config.AudioEngineSauc, v3NoSpeech, nil)
	if !errors.As(err, new(*doubaosauc.NoSpeechError)) {
		t.Errorf("the v3 no-speech verdict stopped being detectable: %v", err)
	}
	// And the v2 verdict keeps its shape (the behaviour the streaming path had).
	v2NoSpeech := &doubaostream.NoSpeechError{Message: "no speech", LogID: "l"}
	if err := voiceLiveErrorFor(config.AudioEngineStreaming, v2NoSpeech, nil); err == nil ||
		!strings.Contains(err.Error(), "not a failure of the run") {
		t.Errorf("the v2 no-speech sentence changed: %v", err)
	}
}

func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

func sizesOf(packets [][]byte) []int {
	out := make([]int, 0, len(packets))
	for _, p := range packets {
		out = append(out, len(p))
	}
	return out
}

// writeLiveCredentialFile writes a KEY=VALUE credential document for the engine's
// one key-file knob and returns its path. It is a SCRATCH file, never the
// operator's own ~/.doubao document (Rule 32's discipline, applied to a
// credential rather than a database).
func writeLiveCredentialFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "voice.env")
	body := "APPID=fake-app-id\nTOKEN=fake-token\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the scratch credential: %v", err)
	}
	return path
}
