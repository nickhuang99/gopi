package main

// chat_voice_live_test.go — the LIVE dictation arm's own contract, at the seams
// this package owns.
//
// WHAT IS PINNED HERE, and why each is here rather than in the pty case. Three of
// these properties are arithmetic or dispatch — the packet assembly, the finish
// key's two semantics, and which loop a given engine gets — and a pty case cannot
// isolate them: it can only show the whole thing working or not working. The pty
// case then proves the PAINTED result (chat_voice_pty_test.go), which is the half
// these cannot see.
//
// The transport is stubbed at voicePacketSayer and the capture at
// voiceStagedStream, so nothing here needs a microphone, a credential or a
// network — and nothing here is a fixture of the real thing: the stub carries the
// same contract the session does (a packet is 200 ms of 16 kHz mono PCM16).

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaostream"
	"github.com/nick/gopi/internal/lineedit"
)

// voiceLiveLoopBoundForTest is how long a case waits for the live loop to reach
// its own exit before calling it a loop that does not stop. It is a wall-clock
// bound on a goroutine with no I/O, so the margin is enormous on purpose: the
// passing path returns in microseconds and only the defect can reach it.
const voiceLiveLoopBoundForTest = 5 * time.Second

// sayRecorder is a voicePacketSayer that records what it was asked to send.
type sayRecorder struct {
	mu      sync.Mutex
	packets [][]byte
	err     error
}

func (r *sayRecorder) Say(pcm []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.packets = append(r.packets, append([]byte(nil), pcm...))
	return nil
}

func (r *sayRecorder) sizes() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int, 0, len(r.packets))
	for _, p := range r.packets {
		out = append(out, len(p))
	}
	return out
}

// liveStream builds a capture whose chunks are written by the caller, so a case
// controls exactly when audio arrives.
func liveStream() *voiceStagedStream {
	return &voiceStagedStream{
		dev:    voiceDevice{ID: "test-device", Name: "test"},
		binary: "test",
		chunks: make(chan []byte, 64),
		done:   make(chan struct{}),
	}
}

// TestVoiceLiveFeeder_SendsOnlyWholePacketsAndKeepsTheRemainder is the PACING and
// LOSSLESSNESS contract, and it is the one case every dictation session depends
// on: a packet must be exactly the geometry the protocol declares, and the audio
// that did not complete one must be HELD (sending it early would put a
// short-packet on a channel whose rhythm the service relies on; dropping it would
// lose the operator's words).
//
// The feeder is driven DIRECTLY, without its goroutine, because the property under
// test is the arithmetic of accumulation — a reader goroutine would put a
// scheduler between the case and the answer.
func TestVoiceLiveFeeder_SendsOnlyWholePacketsAndKeepsTheRemainder(t *testing.T) {
	stream := liveStream()
	sayer := &sayRecorder{}
	f := newVoiceLiveFeeder(stream, sayer, &voiceLiveClock{}, nil)

	// One packet is voiceLivePackets.Bytes; feed two and a half of them.
	half := voiceLivePackets.Bytes / 2
	f.absorb(make([]byte, voiceLivePackets.Bytes))
	f.absorb(make([]byte, half))
	f.absorb(make([]byte, half))

	if got, want := sayer.sizes(), []int{voiceLivePackets.Bytes, voiceLivePackets.Bytes}; len(got) != len(want) || (len(got) == 2 && (got[0] != want[0] || got[1] != want[1])) {
		t.Fatalf("the feeder sent packets of %v bytes, want exactly two of %d — a packet that is not the declared geometry is re-paced by the service", got, voiceLivePackets.Bytes)
	}
	// THE REMAINDER IS IN HAND, not dropped: it is what Finish sends as the last
	// packet, so losing it would truncate the last word of the sentence.
	if got := len(f.takePending()); got != 0 {
		t.Fatalf("%d bytes were left in hand after two whole packets, want 0 (the input was an exact multiple)", got)
	}
	f.absorb(make([]byte, voiceLivePackets.Bytes/4))
	if got := len(f.takePending()); got != voiceLivePackets.Bytes/4 {
		t.Fatalf("the feeder holds %d bytes in hand, want the %d that did not complete a packet", got, voiceLivePackets.Bytes/4)
	}
}

// TestVoiceLiveFeeder_AFailureAfterTheEndIsNotReported pins the RACE the two
// goroutines create: the finish key and the feeder run concurrently, so a packet
// can be composed in the instant the utterance was finalised. The session refuses
// it by name (ErrSessionFinished) and the feeder must drop it WITHOUT recording a
// failure — reporting it would turn a race the operator cannot see into a failure
// they are told about.
//
// A REAL transport failure must still be recorded, and the second half asserts
// exactly that: the exception is the named refusal, never "any error here".
func TestVoiceLiveFeeder_AFailureAfterTheEndIsNotReported(t *testing.T) {
	stream := liveStream()
	sayer := &sayRecorder{err: errSessionFinishedForTest()}
	f := newVoiceLiveFeeder(stream, sayer, &voiceLiveClock{}, voiceLiveBackendStreaming().IsRaceRefusal)
	f.absorb(make([]byte, voiceLivePackets.Bytes))
	f.mu.Lock()
	raced := f.sendErr
	f.mu.Unlock()
	if raced != nil {
		t.Fatalf("a packet refused because the utterance had ended was recorded as a failure: %v", raced)
	}

	broken := &sayRecorder{err: errors.New("connection reset by peer")}
	g := newVoiceLiveFeeder(liveStream(), broken, &voiceLiveClock{}, voiceLiveBackendStreaming().IsRaceRefusal)
	g.absorb(make([]byte, voiceLivePackets.Bytes))
	g.mu.Lock()
	real := g.sendErr
	g.mu.Unlock()
	if real == nil {
		t.Fatal("a real transport failure was swallowed — the operator would be told the utterance was transcribed")
	}
}

// TestVoiceLiveFeeder_EndedCarriesTheAudioInHand is the CAPTURE-DIED path: when the
// device stops delivering (rather than the operator pressing the key), the audio
// that never completed a packet must still reach the caller, so the utterance is
// FINISHED with it rather than abandoned.
func TestVoiceLiveFeeder_EndedCarriesTheAudioInHand(t *testing.T) {
	stream := liveStream()
	f := newVoiceLiveFeeder(stream, &sayRecorder{}, &voiceLiveClock{}, nil)
	stop := make(chan struct{})
	go f.run(stop)
	stream.chunks <- make([]byte, voiceLivePackets.Bytes/2)
	// Wait for the chunk to be absorbed rather than guessing a sleep: the
	// assertion is about WHAT was in hand when the channel closed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		held := len(f.pending)
		f.mu.Unlock()
		if held > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond) //stylegate:allow-sleep waiting on the feeder goroutine, no production path
	}
	close(stream.chunks)
	select {
	case inHand := <-f.ended:
		if len(inHand) != voiceLivePackets.Bytes/2 {
			t.Fatalf("the ended hand-off carried %d bytes, want the %d that were in hand", len(inHand), voiceLivePackets.Bytes/2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the feeder never reported that the capture ended — the utterance would wait for a key that can never come")
	}
	close(stop)
}

// TestVoiceFinishKey_EndsTheUtteranceAndNotTheMode is the SEMANTIC SEPARATION, and
// it is the reason this feature has a second key at all.
//
// THREE FACTS, in the order the operator meets them:
//  1. with an utterance in flight the finish key is CLAIMED, and it closes that
//     utterance's signal;
//  2. a second press while the first is still settling is answered (not dropped,
//     and never a panic on a closed channel);
//  3. with NO utterance in flight the key is NOT claimed — it stays the editor's,
//     which has no handler for it, so it is inert.
//
// Escape is asserted beside it because the separation is the point: the two keys
// must not be able to converge into "both leave the mode".
func TestVoiceFinishKey_EndsTheUtteranceAndNotTheMode(t *testing.T) {
	m := liveModeForTest()
	gate := &voiceChatGate{mode: m}

	// (3) No utterance in flight: the key is nobody's.
	if gate.GateKey(lineedit.Key{Code: lineedit.KeyLiveFinish, Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("the finish key was claimed with no utterance in flight — the key the editor ignores became the mode's")
	}

	// (1) An utterance in flight: claimed, and it ends THAT utterance.
	round := &voiceLiveUtterance{finish: make(chan struct{})}
	m.state.liveUtterance = round
	if !gate.GateKey(lineedit.Key{Code: lineedit.KeyLiveFinish, Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("the finish key was not claimed while an utterance was in flight")
	}
	select {
	case <-round.finish:
	default:
		t.Fatal("the finish key was claimed but did not end the utterance — the audio in hand would never be sent")
	}

	// (2) A second press: still claimed, answered, and — this is the property the
	//     once-guarded ending exists for — it does NOT close the signal twice.
	if !gate.GateKey(lineedit.Key{Code: lineedit.KeyLiveFinish, Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("a second finish press was left to the editor although an utterance was in flight")
	}

	// THE MODE IS STILL ON. This is the assertion that would fail if the finish key
	// had been wired to the exit path: leaving the mode on "I am done speaking"
	// makes every sentence turn the microphone off.
	if !m.state.on {
		t.Fatal("the finish key turned the mode OFF — it must end the UTTERANCE and leave the mode running")
	}
	// AN INJECTED FINISH KEY IS NOT THE MODE'S: a replay must not be able to end a
	// human's sentence, the same rule the Escape gate keeps.
	m.state.liveUtterance = &voiceLiveUtterance{finish: make(chan struct{})}
	if gate.GateKey(lineedit.Key{Code: lineedit.KeyLiveFinish, Origin: lineedit.ReadOriginInjected}) {
		t.Fatal("an injected finish key was claimed — a scripted keystroke ended a live utterance")
	}
}

// TestVoiceFinishKey_ThePressIsAnswered is the visibility contract of the finish
// key, asserted as the SENTENCE the operator would read: a press that was TAKEN
// must leave a row saying so, at the moment it is taken.
//
// WHY IT IS A CONTRACT AND NOT A NICETY. On the chat surface the round's own
// numbers ("Nms of audio sent", the logid) are printed by the CLI's one-shot arm,
// NOT by the mode — so a terminal shows the same picture for a key that worked and
// a key the surface dropped, until the transcript appears. The second-press note
// already exists for exactly this reason (chatVoiceLiveFinishingNote); the FIRST
// press had nothing.
//
// RED WITHOUT THE FIX: the gate claims the key and returns true, and no row that
// names the key is written.
func TestVoiceFinishKey_ThePressIsAnswered(t *testing.T) {
	sess := &voiceLiveSessionTest{}
	m := liveModeForTest()
	m.sess = sess
	m.state.liveUtterance = &voiceLiveUtterance{finish: make(chan struct{})}
	gate := &voiceChatGate{mode: m}

	if !gate.GateKey(lineedit.Key{Code: lineedit.KeyLiveFinish, Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("the finish key was not claimed while an utterance was in flight")
	}
	if !sess.contains(voiceLiveFinishKeyLabel) {
		t.Errorf("the press was consumed and nothing named the key; the operator cannot tell a working press "+
			"from a dropped keystroke. rows were:\n%s", sess.rows())
	}
	// THE KEY IS NAMED BY THE LABEL THE USAGE ROW TEACHES, not by a second spelling
	// typed into the note: a row that says one key and a usage row that says another
	// is the drift Rule 14 forbids.
	if !strings.Contains(voiceLiveUsageNote(), voiceLiveFinishKeyLabel) {
		t.Fatalf("the usage row does not name %q — the two would disagree", voiceLiveFinishKeyLabel)
	}
}

// TestVoiceFinishKey_EscapeStillLeavesTheMode is the OTHER half of the separation,
// pinned on the same gate: Escape must reach the leave path unchanged, whatever
// the finish key does.
func TestVoiceFinishKey_EscapeStillLeavesTheMode(t *testing.T) {
	m := liveModeForTest()
	m.state.liveUtterance = &voiceLiveUtterance{finish: make(chan struct{})}
	gate := &voiceChatGate{mode: m}
	if !gate.GateKey(lineedit.Key{Code: lineedit.KeyEscape, Origin: lineedit.ReadOriginTyped}) {
		t.Fatal("Escape was not claimed by the voice gate")
	}
	// THE MODE IS OFF, which is what Escape means and what the finish key must
	// never do: leave() moves the mode's own state, and it is the observable the
	// separation is judged on.
	if m.state.on {
		t.Fatal("Escape did not leave the mode — the mode's own interrupt changed meaning")
	}
	if m.state.liveUtterance != nil {
		t.Fatal("leaving the mode left an utterance installed — the next entry could end a sentence nobody is speaking")
	}
}

// TestVoiceLiveDispatch_IsOptInByEngine is the "the non-streaming engines are
// unaffected" criterion, asserted as a FUNCTION rather than described: the live arm
// is selected for exactly one engine, and every other value falls through to the
// window loop.
func TestVoiceLiveDispatch_IsOptInByEngine(t *testing.T) {
	live := map[string]bool{}
	for _, engine := range voiceEngineValues {
		live[engine] = voiceLiveEngineSelected(&voiceOptions{engine: engine})
	}
	// THE LIVE ARM'S ENGINES ARE THE TWO STREAMING CHANNELS (task 01a13303 added
	// the v3 one, which until then fell through to the six-second window loop).
	// Every OTHER engine keeps the behaviour it had, which is what this half
	// asserts.
	liveEngines := map[string]bool{config.AudioEngineStreaming: true, config.AudioEngineSauc: true}
	for engine, selected := range live {
		if liveEngines[engine] != selected {
			t.Errorf("voiceLiveEngineSelected(%q) = %v, want %v", engine, selected, liveEngines[engine])
		}
	}
}

// TestVoiceLiveUsageNote_NamesTheKeyTheGateClaims is the advertised-vs-claimed
// contract: the row the operator reads and the predicate the gate runs are built
// from one label, so a surface cannot advertise a key it does not claim (Rule 13).
func TestVoiceLiveUsageNote_NamesTheKeyTheGateClaims(t *testing.T) {
	note := voiceLiveUsageNote()
	if !strings.Contains(note, voiceLiveFinishKeyLabel) {
		t.Fatalf("the usage row %q does not name the finish key %q", note, voiceLiveFinishKeyLabel)
	}
	// THE WORD IS THE ONE THE DISPATCHER LEAVES ON (task 01a1424f-4526). This arm
	// asserted chatVoiceStatusWord, which pinned the DEFECT: the row advertised
	// `status` while `/voice status` prints and stays (the behaviour arm lives in
	// chat_voice_leaveword_test.go).
	if !strings.Contains(note, chatVoiceStopWord) {
		t.Fatalf("the usage row %q does not name the exit word %q, so the two keys are not told apart", note, chatVoiceStopWord)
	}
}

// TestVoiceLiveSink_RefusesWhenTheCarrierHasNoDraftRoad pins the FAILURE PATH at
// the sink: a live session on a carrier that cannot draw a draft must hear that
// the text had nowhere to go, rather than appear to work while the operator sees
// nothing (Rule 15).
func TestVoiceLiveSink_RefusesWhenTheCarrierHasNoDraftRoad(t *testing.T) {
	sink := voiceLiveSinkFor(&voiceChatRecorderStagedTest{}) // no draft road
	sink.Draft("温度")                                         // must not panic
	if sink.Settle("温度") {
		t.Fatal("a carrier with no draft road reported the sentence as settled — the transcript was lost silently")
	}
}

// liveModeForTest builds the minimum mode a gate case can drive: the mode is ON,
// with a session double (so the exit path's own seam calls have somewhere to go)
// and a stop channel (the teardown closes it). Every field it sets is one the
// production enter() sets, so a case cannot pass on a state the mode never
// reaches.
func liveModeForTest() *voiceChatMode {
	m := &voiceChatMode{sess: voiceChatRecorderStagedTest{}}
	m.state.on = true
	m.state.stop = make(chan struct{})
	return m
}

// voiceChatRecorderStagedTest is a session double with the mandatory seam and
// NOTHING else, so the sink's capability check is exercised on the shape it
// exists for.
type voiceChatRecorderStagedTest struct{}

func (voiceChatRecorderStagedTest) VoiceInsertLine(string) bool { return false }

func (voiceChatRecorderStagedTest) VoiceNotice(string) {}

// VoiceNoticeStyled is the styled twin of the double's VoiceNotice: the mode
// writes its rows through it, and this double has no console — it is here so the
// double satisfies the seam, not to observe anything.
func (voiceChatRecorderStagedTest) VoiceNoticeStyled(string) {}

func (voiceChatRecorderStagedTest) VoiceEditor() *lineedit.Editor { return nil }

func (voiceChatRecorderStagedTest) VoicePublishStatus() {}

// errSessionFinishedForTest is the library's named refusal, reached through the
// exported sentinel so the case cannot drift from what the session returns.
func errSessionFinishedForTest() error { return doubaostream.ErrSessionFinished }

// voiceChatRecorderScriptedTest is a LIVE recorder double: it answers one
// utterance per Live call from a SCRIPT the case wrote, so a case can drive the
// loop's own decisions (how many utterances, and how each one ended) without a
// microphone, a network or a clock.
type voiceChatRecorderScriptedTest struct {
	mu      sync.Mutex
	reports []voiceLiveReport
	// errs is the live arm's PER-CALL failure script (task 01a121fc, deliverable
	// D), consumed one entry per Live call: a non-nil entry is that session's
	// error and a nil entry means the call answered from reports. It exists for
	// the same reason the window recorder's roundErrs does — the rule under test
	// is about a RUN of failures and its reset, which one sticky error cannot
	// express.
	errs  []error
	calls int
}

func (r *voiceChatRecorderScriptedTest) Select(string) (string, []string, error) {
	return "scripted", nil, nil
}

func (r *voiceChatRecorderScriptedTest) Round(*voiceOptions, int) (string, error) { return "", nil }

func (r *voiceChatRecorderScriptedTest) Close() {}

func (r *voiceChatRecorderScriptedTest) Live(*voiceOptions, voiceLiveSink, <-chan struct{}) (voiceLiveReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		r.calls++
		if err != nil {
			return voiceLiveReport{}, err
		}
		// A NIL ENTRY IS A SESSION THAT ANSWERED: it settles no text and reports
		// the capture ended, which is the terminal success the failure-run case
		// needs (a failure run interrupted by a real answer must not count that
		// answer as another failure).
		return voiceLiveReport{CaptureEnded: true}, nil
	}
	if r.calls >= len(r.reports) {
		// THE SCRIPT IS SPENT: a run that keeps calling after the script ends is
		// exactly the defect the case is looking for, so it is ANSWERED rather
		// than blocked (a blocked call would look like a hang).
		return voiceLiveReport{CaptureEnded: true}, nil
	}
	rep := r.reports[r.calls]
	r.calls++
	return rep, nil
}

func (r *voiceChatRecorderScriptedTest) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestVoiceLiveLoop_ACaptureThatEndedIsNotFollowedByAnotherUtterance is the
// minimal repro for the measured live-arm failure, and it is written as the
// OBSERVABLE the operator saw rather than as a unit of the fix:
//
// MEASURED (2026-10-09, task 01a1204b, `GOPI_CHAT_VOICE_AUDIO=<file>`, real
// doubao-stream service). The first utterance transcribed correctly. The capture
// was a FILE and therefore finite, so it ended on its own; the mode then opened a
// SECOND streaming session over an exhausted capture, sent the end-of-stream flag
// with zero bytes of audio, and the service refused it:
//
//	[voice] stream: 0ms of audio sent, logid …, reqid …
//	[gopi] voice mode FAILED — doubao-stream engine: streaming asr: server: code 101
//	[gopi] voice mode OFF (a failed round) — the recorder is stopping…
//
// The service was right and the round was a phantom: the SECOND utterance had no
// source, so the honest answer is to leave the mode, not to report a failure over
// audio nobody could have spoken.
//
// RED WITHOUT THE FIX: the loop takes the capture-ended report and loops, so the
// recorder is asked for a SECOND utterance (callCount 2) and the leaving note
// names a FAILED round.
func TestVoiceLiveLoop_ACaptureThatEndedIsNotFollowedByAnotherUtterance(t *testing.T) {
	rec := &voiceChatRecorderScriptedTest{reports: []voiceLiveReport{
		{Text: "你好你好！", AudioMS: 7227, Partials: 26, CaptureEnded: true},
	}}
	m := liveModeForTest()
	m.recorder = rec
	m.sess = &voiceLiveSessionTest{}
	sess := m.sess.(*voiceLiveSessionTest)
	// THE GENERATION IS PART OF THE SETUP: the loop belongs to entry 1, and a mode
	// whose `started` counter does not name that entry is a loop that must NOT run
	// (stale) — the case would pass for the wrong reason.
	m.state.started = 1
	// THE LOOP'S OWN DONE CHANNEL IS PART OF THE SETUP: liveLoop closes it on the
	// way out, and a case waiting on a NIL channel would hang instead of failing —
	// a hang is not evidence about the behaviour under test.
	m.state.loopDone = make(chan struct{})
	runLoopBounded(t, m)

	if got := rec.callCount(); got != 1 {
		t.Fatalf("the loop asked for %d utterances after the capture ended, want 1 — "+
			"a second session over a spent capture carries no audio and is refused by the service (code 101)", got)
	}
	if !sess.said(chatVoiceLeftByCaptureEnded) {
		t.Errorf("the exit did not name the capture as what ended the mode; rows were:\n%s", sess.rows())
	}
	if sess.contains(chatVoiceLeftByFailed) {
		t.Errorf("a spent capture was reported as a FAILED round — nothing failed, the source ran out; rows were:\n%s", sess.rows())
	}
	// THE TRANSCRIPT IS KEPT: the utterance that DID carry audio was transcribed
	// and its text belongs on the line.
	if !sess.contains("你好你好！") {
		t.Errorf("the transcript of the utterance that carried audio was dropped; rows were:\n%s", sess.rows())
	}
}

// TestVoiceLiveLoop_AFinishKeyEndedUtteranceKeepsGoing is the OTHER side of the
// same predicate, and it is what stops the fix from being "stop after one
// sentence": an utterance ended by the operator's finish key leaves the mode ON,
// because dictation is sentences back to back (see liveLoop's own note).
//
// RED WITHOUT THE FIX: nothing — the pre-fix loop also continued here. It is
// included because a fix for the case above could be written as "one utterance per
// entry", and this is the assertion that refuses that reading.
func TestVoiceLiveLoop_AFinishKeyEndedUtteranceKeepsGoing(t *testing.T) {
	rec := &voiceChatRecorderScriptedTest{reports: []voiceLiveReport{
		{Text: "第一句", AudioMS: 1000, Partials: 3},
		{Text: "第二句", AudioMS: 1000, Partials: 3, CaptureEnded: true},
	}}
	m := liveModeForTest()
	m.recorder = rec
	m.sess = &voiceLiveSessionTest{}
	sess := m.sess.(*voiceLiveSessionTest)
	// THE GENERATION IS PART OF THE SETUP: the loop belongs to entry 1, and a mode
	// whose `started` counter does not name that entry is a loop that must NOT run
	// (stale) — the case would pass for the wrong reason.
	m.state.started = 1
	// THE LOOP'S OWN DONE CHANNEL IS PART OF THE SETUP: liveLoop closes it on the
	// way out, and a case waiting on a NIL channel would hang instead of failing —
	// a hang is not evidence about the behaviour under test.
	m.state.loopDone = make(chan struct{})
	runLoopBounded(t, m)

	if got := rec.callCount(); got != 2 {
		t.Fatalf("the loop ran %d utterances, want 2 — a finish-key utterance must not end the mode", got)
	}
	if !sess.contains("第一句") || !sess.contains("第二句") {
		t.Errorf("both transcripts belong on the line; rows were:\n%s", sess.rows())
	}
	if sess.contains(chatVoiceLeftByFailed) {
		t.Errorf("an ordinary ending was reported as a FAILED round; rows were:\n%s", sess.rows())
	}
}

// voiceLiveSessionTest is the console double the loop writes its rows through. It
// records them so a case can assert on the SENTENCE the operator would read,
// rather than on an internal flag that no reader sees.
type voiceLiveSessionTest struct {
	mu   sync.Mutex
	text []string
}

func (s *voiceLiveSessionTest) VoiceInsertLine(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text = append(s.text, text)
	return true
}

func (s *voiceLiveSessionTest) VoiceNotice(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text = append(s.text, text)
}

// VoiceNoticeStyled records the row's TEXT, which is what this double asserts on
// (rows()). The emphasis is the carrier's business and the double has no palette;
// the mode's WORDS are the same whichever road they take, so recording the text
// keeps every existing assertion meaningful.
func (s *voiceLiveSessionTest) VoiceNoticeStyled(text string) { s.VoiceNotice(text) }

func (s *voiceLiveSessionTest) VoiceEditor() *lineedit.Editor { return nil }

func (s *voiceLiveSessionTest) VoicePublishStatus() {}

func (s *voiceLiveSessionTest) rows() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.text, "\n")
}

func (s *voiceLiveSessionTest) contains(fragment string) bool {
	return strings.Contains(s.rows(), fragment)
}

func (s *voiceLiveSessionTest) said(reason string) bool { return s.contains(reason) }

// runLoopBounded drives the live loop to its own exit and FAILS THE CASE if it
// never gets there.
//
// THE BOUND IS THE POINT, not a precaution. The defect these cases are written
// against is a loop that does not stop when its source is spent, so the pre-fix
// behaviour is an UNBOUNDED loop — and an unbounded loop that a case waits on
// produces a stack dump rather than the sentence being asserted (measured: the red
// proof of this very case died in runtime/panic.go instead of printing its own
// failure). A case that has to be read through a panic is a case that is not
// reading.
func runLoopBounded(t *testing.T, m *voiceChatMode) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.liveLoop(1, m.state.stop, make(chan struct{}))
	}()
	select {
	case <-done:
	case <-time.After(voiceLiveLoopBoundForTest):
		t.Fatal("the live loop did not return within the case's bound — it kept asking the recorder for " +
			"another utterance after the capture had ended, which is the defect under test")
	}
}
