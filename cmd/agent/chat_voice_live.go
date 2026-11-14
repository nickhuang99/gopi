package main

// chat_voice_live.go — the LIVE dictation arm of `/voice`: ONE continuous capture
// fed to the v2 streaming engine packet by packet, with the text growing in the
// editing line WHILE the operator speaks.
//
// WHAT THIS REPLACES, and why the window machinery could not be bent into it.
// The window arm (cli_voice_recorder_staged.go) exists to make a BATCH engine
// usable interactively: it cuts the continuous capture into windows and asks the
// engine about a window after it closes. That is the right answer for an engine
// whose unit of work is a file — and it is structurally the wrong answer for an
// engine whose unit of work is a SAMPLE: a window boundary is a decision made
// AFTER the operator stopped talking, so the transcript can only ever arrive
// after the sentence is over. The streaming channel answers while the sentence is
// still being spoken, and to reach that the audio has to go out as it is
// captured — no window, no boundary, no waiting for the end.
//
// SO THE WINDOW IS NOT TUNED, IT IS ABSENT: this arm never calls Next, never
// consults the window plan, and never waits for a boundary. It takes the SAME
// capture (the reader goroutine of voiceStagedStream is the one reader of the
// device, and it is not restarted, re-spawned or duplicated) and drains its
// chunks into 200 ms packets.
//
// THE TWO SHAPES SHARE EVERYTHING THEY CAN, which is the design constraint and
// not a courtesy:
//
//	                      window arm            live arm
//	capture process      voiceStagedStream      voiceStagedStream  (same)
//	device selection     voiceChatRecorderLocal voiceChatRecorderLocal
//	credential + options voiceStreamCredentials chatVoiceResolveOptions
//	engine channel       batch HTTP             doubaostream.Session
//	unit of work         a closed window        one packet of samples
//	text lands           after the window       WHILE the operator speaks
//
// THE LIVE ARM IS OPT-IN BY ENGINE, never by default: it is selected by
// config.AudioEngineStreaming exactly as the CLI's third arm is, and an
// operator on `local` or `doubao` gets the behaviour they had — the acceptance
// criterion "the non-streaming engines are unaffected" is a property of the
// dispatch below rather than a promise.
//
// ZERO NEW DEPENDENCIES (Rule 35): the capture is the existing recorder argv, the
// transport is the existing doubaostream client, and the draft is rendered by the
// existing editor. Nothing is scheduled, installed or left resident.

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaostream"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

const (
	// voiceLivePacketMillis / voiceLivePacketBytes ARE GONE (as of the
	// convergence): the packet geometry is the streaming library's own
	// (doubaostream.DefaultPacketAssembler, read below), because a second
	// spelling of "200 ms of 16 kHz mono PCM16" in this package was a second
	// place that could drift from the protocol — and it did, in the library's
	// own history: stepping a BYTE buffer by a SAMPLE count halves the audio per
	// packet and doubles the stream. The live arm asks for the geometry instead
	// of computing it (Rule 13/14).

	// voiceLiveDraftMinRunes is how much text a partial must carry before it is
	// drawn in the editing line. It is a HYSTERESIS against a recogniser that
	// revises the first character repeatedly: a one-rune draft that changes five
	// times a second is noise rather than progress, and the operator cannot read
	// it anyway. Two runes is where Chinese and Latin both start carrying
	// something (a single Latin letter is a plausible start of anything).
	voiceLiveDraftMinRunes = 2
)

// voiceLivePackets is the packet geometry the live arm accumulates against. It
// is resolved ONCE (the library's own default cadence), so the feeder's
// arithmetic and the session's own packet construction cannot disagree about
// what a packet is.
var voiceLivePackets = doubaostream.DefaultPacketAssembler()

// voiceLiveFinishKeyLabel is how the usage text and the entered row SPELL the
// finish key. It is one constant because the gate below decides whether a key is
// the finish key from the KEY CODE, and a label written twice is how a surface
// comes to advertise a key it does not claim (Rule 13/14).
const voiceLiveFinishKeyLabel = "Ctrl+G"

// voiceDraftSink is the LIVE arm's door onto the editing line, through the mode's
// session seam. It is an OPTIONAL interface on purpose: the mode's other session
// doubles (the cooked path, the pty harness) have no draft segment, and requiring
// the two calls of every implementer would make the seam's own contract heavier
// for a capability only the live arm uses.
type voiceDraftSink interface {
	VoiceDraft(text string) bool
	VoiceSettle(text string) bool
}

// voiceLiveSinkFor builds the sink the live arm publishes through: the session's
// own draft road when it has one, and a REFUSING sink otherwise.
//
// THE REFUSAL IS RETURNED, never a nil: a live session on a carrier that cannot
// draw a draft (no editor) must report that the text had nowhere to go, rather
// than appear to work while the operator sees nothing (Rule 15). The mode's enter
// already refuses a session with no editor, so this is the second lock on the same
// door — and it is the seam's own answer instead of an assumption about it.
func voiceLiveSinkFor(sess voiceChatSession) voiceLiveSink {
	if d, ok := sess.(voiceDraftSink); ok {
		return draftSink{d}
	}
	return refusingDraftSink{}
}

// draftSink is the session's draft road behind the arm's own narrow interface.
type draftSink struct{ d voiceDraftSink }

func (s draftSink) Draft(text string) { s.d.VoiceDraft(text) }

// Settle hands the text over as final AND reports whether the line took it. A
// refusal is stated by the caller rather than swallowed: the arm returns it from
// Live, so the mode's failure path reports it with the row the operator reads.
func (s draftSink) Settle(text string) bool { return s.d.VoiceSettle(text) }

// refusingDraftSink is the sink for a carrier with no draft road.
type refusingDraftSink struct{}

func (refusingDraftSink) Draft(string)       {}
func (refusingDraftSink) Settle(string) bool { return false }

// voiceLiveFinishKeyClaims reports whether ONE key is the finish key, and it is
// the ONLY place that question is answered: the gate calls it and the loop's own
// documentation names it, so the advertised key and the claimed key cannot be two
// different keys (Rule 13).
//
// THE ORIGIN IS CHECKED, exactly as the Escape gate checks it: a key that did not
// come from the terminal (an injected replay key) must not be able to end a
// human's utterance, and the injected path's own controls are its business.
func voiceLiveFinishKeyClaims(k lineedit.Key) bool {
	return k.Origin == lineedit.ReadOriginTyped && k.Code == lineedit.KeyLiveFinish
}

// voiceLiveReport is what ONE live utterance produced, in the numbers the report
// and the tests quote. The timings are measured from the FIRST PACKET SENT, which
// is the only origin that means anything for "how fast did the text appear" — an
// origin at the mode's entry would measure the device's open latency instead.
type voiceLiveReport struct {
	// Text is the final transcript.
	Text string
	// AudioMS is how much audio was sent.
	AudioMS int
	// Partials is how many draft updates the engine produced.
	Partials int
	// FirstPartialMS is the first-draft latency, or 0 when none arrived.
	FirstPartialMS int
	// Elapsed is the whole session, from the first packet to the final answer.
	Elapsed time.Duration
	// TimedOut reports that the final answer did not arrive within the session's
	// own bound (see doubaostream.Session.FinishTimedOut).
	TimedOut bool
	// CaptureEnded reports that this utterance ended because the CAPTURE ran out
	// rather than because the operator said they were done. The two endings are
	// the same event to the session (the end-of-stream flag goes out either way)
	// and are NOT the same event to the mode: a capture that has ended cannot
	// record the next sentence, so the loop must not open another streaming
	// session over it.
	//
	// MEASURED (2026-10-09, task 01a1204b, `GOPI_CHAT_VOICE_AUDIO=<file>`):
	// without this flag the loop opened a SECOND session over an exhausted
	// capture, sent the end-of-stream flag with zero bytes of audio, and the
	// service answered `server: code 101` — which the mode reported as a FAILED
	// round ("voice mode FAILED — ... server: code 101") beside a
	// `[voice] stream: 0ms of audio sent` line, on a run whose FIRST utterance had
	// transcribed correctly. The zeros were honest and the failure was not: the
	// service was right to refuse an utterance that carried no audio.
	CaptureEnded bool
}

// voiceLiveSink is where a live session publishes what it hears, while it hears
// it. It is the mode's own door onto the editing line, so the streaming arm never
// touches the editor directly: the arm's job is the audio and the text, and
// WHERE the text goes is the mode's business (Rule 3 — a second writer of the
// line is the thing the draft segment exists to prevent).
type voiceLiveSink interface {
	// Draft publishes the text known SO FAR. It replaces whatever the previous
	// call published and is drawn as a draft (internal/lineedit/draft.go).
	Draft(text string)
	// Settle hands the text over as FINAL: from here it is the operator's own,
	// editable text on the line. It reports whether the line took it — a carrier
	// with no editing line cannot, and the arm states that rather than letting a
	// settled sentence vanish (Rule 15).
	Settle(text string) bool
}

// voiceChatLiveRunner is the OPTIONAL half of the recorder seam that runs a LIVE
// utterance: continuous capture in, partials out, until the operator says they
// are done.
//
// IT IS OPTIONAL for the same reason voiceChatFinisher is: a recorder that
// records one window per process has no continuous stream to feed, and forcing it
// to implement this would be inventing a fact it does not have. A recorder that
// cannot run live is not an error — the mode reports the refusal by name and
// stays on the window path.
type voiceChatLiveRunner interface {
	// Live runs ONE utterance. It returns when the utterance is finished (the
	// finish channel closed, or the capture ended), and it reports what it heard.
	Live(opts *voiceOptions, sink voiceLiveSink, finish <-chan struct{}) (voiceLiveReport, error)
}

// errVoiceLiveUnsupported is the named refusal for a recorder with no live path.
var errVoiceLiveUnsupported = errors.New("this recorder cannot run a live streaming session")

// voiceChatRecorderStaged gains the live half here, beside its window half and
// over the SAME capture: Live opens the continuous stream, feeds it to the
// streaming engine packet by packet, and closes it on the way out — the same
// open/close discipline Next obeys, so the microphone is held by exactly one
// owner for the whole of either shape.
//
// THE CAPTURE IS OPENED PER UTTERANCE, not per mode. That is the one place the
// live arm differs from the window arm's lifetime model, and it is deliberate: a
// streaming session is billed per audio second and terminated by an explicit
// end-of-stream flag, so a stream left open between utterances would be a
// session the service is waiting on and the operator is not using. One utterance
// = one session = one capture, and pressing the finish key is what ends it.
func (r *voiceChatRecorderStaged) Live(opts *voiceOptions, sink voiceLiveSink, finish <-chan struct{}) (voiceLiveReport, error) {
	var report voiceLiveReport
	if r.dev == nil {
		return report, errors.New("the live session has no capture device (Select was not called)")
	}
	// THE ENGINE IS RESOLVED BEFORE ANYTHING IS OPENED: which backend this
	// utterance runs on, and the session options it carries, are one decision
	// (voice_live_engines.go) — a run that opened a capture first and then found
	// it had no engine would have held the microphone for nothing.
	backend, sessOpts, err := voiceLiveEngineFor(opts)
	if err != nil {
		return report, err
	}
	// THE CAPTURE COMES FROM THE RUN'S OWN SOURCE (chat_voice_audio.go): a device
	// when the operator named none, the FILE they named otherwise — the same
	// chooser the window arm uses, so the two arms cannot read different audio.
	stream, err := openVoiceStagedStream(r.captureOptions(), r.dev)
	if err != nil {
		return report, err
	}
	// THE CAPTURE IS RELEASED ON EVERY EXIT PATH: an error below must not leave
	// the microphone held by a process nobody owns (Rule 15, and the same
	// discipline Select's one-shot sibling keeps).
	defer stream.Close()

	// THE SESSION, THE CLOCK, THE FEEDER, THE END AND THE IN-HAND FLUSH ARE ONE
	// IMPLEMENTATION (voice_stream_runtime.go), shared with the tool's two
	// streaming shapes and, since task 01a13303, with BOTH engines through the
	// backend seam (voice_live_engines.go). This arm supplies only what is
	// SPECIFIC to it — the finish key, and what a partial MEANS here (a draft in
	// the editing line, gated by the hysteresis below) — so the surfaces that feed
	// this channel cannot drift apart again, which they demonstrably had.
	run := runVoiceLiveSession(stream, voiceLiveRunConfig{
		Backend: backend,
		Options: sessOpts,
		// THE DRAFT IS PUBLISHED FROM THE READER GOROUTINE, and the sink's own
		// contract (lineedit.Editor.SetDraft) is what makes that safe: the text is
		// queued and applied by the read loop that owns the editing model.
		OnPartial: func(p voiceLivePartial) {
			if len([]rune(p.Text)) < voiceLiveDraftMinRunes {
				// See voiceLiveDraftMinRunes: too little to be worth drawing.
				return
			}
			sink.Draft(p.Text)
		},
		Stop: finish,
	})
	// THE FIGURES ARE THE RUNNER'S OWN, read from the one clock and the one
	// session: the first draft's latency is measured from the FIRST PACKET SENT
	// (never from this call's entry, which would fold the handshake and the
	// device's own startup into "how fast did the text appear").
	report.Partials = run.PartialCount
	report.TimedOut = run.TimedOut
	report.FirstPartialMS = run.Clock.firstPartialLatencyMS()
	report.CaptureEnded = run.CaptureEnded
	if run.Result != nil {
		report.AudioMS = run.Result.AudioMS
		report.Elapsed = run.Result.Elapsed
	}
	if err := run.error(); err != nil {
		return report, voiceLiveErrorFor(voiceResolveEngine(opts), err, run.Result)
	}
	report.Text = run.Text
	// THE FINAL TEXT SETTLES THE DRAFT: the text becomes the operator's own line
	// through the ordinary insert road, and the draft segment is cleared in the
	// same pass — so the line never shows the sentence twice (draft.go).
	//
	// A SETTLE THAT DID NOT LAND IS AN ERROR, not a quiet success: the words were
	// recognised and the operator's line did not receive them, which is the
	// failure this whole arm exists to avoid (Rule 15). The report still carries
	// the text, so the caller can say what was lost.
	if !sink.Settle(report.Text) {
		return report, errors.New("the transcript could not be settled onto the input line (the editor's draft road refused it)")
	}
	return report, nil
}

// voiceLiveFeeder is the write side of a live session: it turns the capture
// reader's chunks into packets and says them, and it remembers the audio that has
// arrived but is not yet a full packet so the utterance can end mid-packet
// without losing it.
//
// IT OWNS NO TIMER. The capture's own cadence IS the clock (see
// internal/doubaostream/session.go's header): chunks arrive because the device
// produced them, so sending each packet the moment its samples are in hand is
// what "real time" means here. A pacing loop would be a second clock over the
// same audio, and the file arm already has one because a file has no clock of its
// own.
type voiceLiveFeeder struct {
	stream *voiceStagedStream
	sess   voicePacketSayer
	// isRace tells whether a Say refusal means "the utterance was already
	// finalised" rather than "the transport broke" (see voiceLiveRaceRefusal):
	// it is the ENGINE's own sentinel vocabulary, handed in by the backend, so
	// this feeder never imports one engine's errors (Rule 3).
	isRace voiceLiveRaceRefusal
	// ended is closed when the capture stopped delivering (the device died or the
	// stream was closed): the caller then finishes the utterance with whatever is
	// in hand rather than waiting for a key that may never come.
	ended chan []byte
	// mu guards pending and firstPacket.
	mu sync.Mutex
	// pending is the audio that has arrived and is not yet a full packet.
	pending []byte
	// clock is the utterance's shared timeline (see voiceLiveClock): the feeder
	// starts it and the session's partial callback reads it, so the two halves of
	// one latency number are measured by the same object rather than by two
	// timestamps that could have different origins.
	clock *voiceLiveClock
	// sendErr is why the feeder stopped sending: a broken session, or a failure
	// reported by the transport. It is kept so the caller can report WHY the
	// utterance ended rather than only that it did.
	sendErr error
}

// voicePacketSayer is the ONE thing the feeder needs of a streaming session: send
// this audio. It is an interface rather than *doubaostream.Session so the packet
// assembly — the part with arithmetic and a losslessness obligation — is testable
// without a network, a credential or a microphone (Rule 32's discipline applied to
// a transport rather than a database: a seam at the boundary, never a fixture of
// the real thing).
type voicePacketSayer interface {
	Say(pcm []byte) error
}

// newVoiceLiveFeeder builds the feeder over one capture, one session and the
// engine's own refusal vocabulary.
func newVoiceLiveFeeder(stream *voiceStagedStream, sess voicePacketSayer, clock *voiceLiveClock, isRace voiceLiveRaceRefusal) *voiceLiveFeeder {
	return &voiceLiveFeeder{stream: stream, sess: sess, clock: clock, isRace: isRace, ended: make(chan []byte, 1)}
}

// voiceLiveClock is ONE utterance's timeline: when the first packet went out and
// when the first partial came back. The two are stamped by different goroutines
// (the feeder sends, the session's reader receives), so they live together under
// one mutex and the latency is a subtraction the clock itself performs.
//
// THE ORIGIN IS THE FIRST PACKET SENT, and that choice is the measurement's whole
// meaning: an origin at the session's open would fold the handshake and the
// device's own startup into "how fast did the text appear", which is not the
// number an operator feels when they start speaking.
type voiceLiveClock struct {
	mu           sync.Mutex
	firstPacket  time.Time
	firstPartial time.Time
}

// notePacket stamps the first packet's send time (once).
func (c *voiceLiveClock) notePacket() {
	c.mu.Lock()
	if c.firstPacket.IsZero() {
		c.firstPacket = time.Now()
	}
	c.mu.Unlock()
}

// notePartial stamps the first partial's arrival (once).
func (c *voiceLiveClock) notePartial() {
	c.mu.Lock()
	if c.firstPartial.IsZero() {
		c.firstPartial = time.Now()
	}
	c.mu.Unlock()
}

// firstPartialWall is the WALL instant the first partial arrived, or the zero
// time when none did. The channel latency below is measured between two stamps
// this clock takes; this accessor exists so a caller that has its own origin —
// the run's start, i.e. what the operator waited — can subtract THAT instead of
// re-deriving the same instant from a duration that has a different origin
// (Rule 2: one stamp, however many times it is read).
func (c *voiceLiveClock) firstPartialWall() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firstPartial
}

// firstPartialLatencyMS is how long the first draft took, from the first packet
// SENT: 0 when no partial arrived (the honest value for "the recogniser said
// nothing", never a fabricated duration), and 0 for a stamp that came out before
// the packet (a clock or ordering artefact — a negative latency is not a number
// anyone can act on).
func (c *voiceLiveClock) firstPartialLatencyMS() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.firstPacket.IsZero() || c.firstPartial.IsZero() {
		return 0
	}
	if d := c.firstPartial.Sub(c.firstPacket); d > 0 {
		return int(d.Milliseconds())
	}
	return 0
}

// run consumes the capture until it ends or it is told to stop. It collects the
// audio in hand when it returns, so the caller can send it as the last packet.
func (f *voiceLiveFeeder) run(stop <-chan struct{}) {
	for {
		select {
		case chunk, ok := <-f.stream.Chunks():
			if !ok {
				// THE CAPTURE ENDED (the device failed, or Close released the
				// reader). The audio in hand is handed to the caller so the
				// utterance can be finished rather than abandoned.
				f.mu.Lock()
				inHand := append([]byte(nil), f.pending...)
				f.pending = nil
				f.mu.Unlock()
				f.ended <- inHand
				return
			}
			f.absorb(chunk)
		case <-stop:
			return
		}
	}
}

// absorb appends audio and sends every COMPLETE packet it completes. A partial
// packet is held: sending it early would put a shorter-than-declared packet on a
// channel whose cadence the service relies on, and dropping it would lose audio.
func (f *voiceLiveFeeder) absorb(chunk []byte) {
	f.mu.Lock()
	f.pending = append(f.pending, chunk...)
	packetBytes := voiceLivePackets.Bytes
	packets := make([][]byte, 0, len(f.pending)/packetBytes)
	for len(f.pending) >= packetBytes {
		packet := make([]byte, packetBytes)
		copy(packet, f.pending[:packetBytes])
		f.pending = f.pending[packetBytes:]
		packets = append(packets, packet)
	}
	f.mu.Unlock()
	for _, packet := range packets {
		if err := f.sess.Say(packet); err != nil {
			// A REFUSAL AFTER THE END WAS SENT IS NOT A FAILURE: the finish key
			// and this feeder run on different goroutines, so a packet can be
			// composed in the instant the utterance was finalised. It is dropped
			// ON PURPOSE and the reason is named (Rule 15) — reporting it would
			// turn a race the operator cannot see into a failure they are told
			// about. WHICH refusals those are is the ENGINE's own answer (see
			// voiceLiveRaceRefusal).
			if f.isRace != nil && f.isRace(err) {
				return
			}
			f.noteErr(err)
			return
		}
		f.clock.notePacket()
	}
}

// noteErr remembers the first send failure.
func (f *voiceLiveFeeder) noteErr(err error) {
	f.mu.Lock()
	if f.sendErr == nil {
		f.sendErr = err
	}
	f.mu.Unlock()
}

// sendFailure reports why the feeder stopped sending, and nil when it did not
// fail. It is the caller's "was the send side broken?" — the difference between
// "the service answered nothing" and "this end stopped talking", which produce
// the same empty transcript and are two different incidents (Rule 15).
func (f *voiceLiveFeeder) sendFailure() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sendErr
}

// takePending hands back the audio that is not yet a full packet, clearing it. It
// is the caller's "what is still in hand" at the moment the utterance ends.
func (f *voiceLiveFeeder) takePending() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]byte(nil), f.pending...)
	f.pending = nil
	return out
}

// voiceLiveEngineSelected reports whether this run should use the live arm: the
// resolved engine is the streaming one. It is the ONE dispatch predicate, so the
// mode's loop selection and the usage rows cannot disagree about which shape a
// given engine gets (Rule 3).
func voiceLiveEngineSelected(opts *voiceOptions) bool {
	switch voiceResolveEngine(opts) {
	case config.AudioEngineStreaming, config.AudioEngineSauc:
		// BOTH STREAMING CHANNELS RUN THE LIVE ARM, because both of them CAN:
		// "streaming" is the v2 WebSocket (doubaostream.Session) and "sauc" is the
		// v3 one (doubaosauc.Session), and each has an engine adapter
		// (voice_live_engines.go) behind the same runner. The two protocols
		// differ; what the arm DELIVERS does not.
		//
		// THE v3 ARM'S ENDPOINT IS THE LIVE ONE (bigmodel_async —
		// voiceSaucLiveEndpoint), which is the whole point of the selection: the
		// file-semantics endpoint answers once at the end, so a live session on it
		// would show nothing until the utterance was over — i.e. the window loop's
		// behaviour, with none of the window loop's reasons.
		return true
	}
	return false
}

// voiceLiveUsageNote is what the entered row says about the live arm: the key
// that ends the utterance, the key that leaves, and the fact that the two are
// different. It is built from voiceLiveFinishKeyLabel and chatVoiceStopWord, so
// the usage text and the gate that implements it cannot describe different keys.
//
// THE LEAVE WORD IS chatVoiceStopWord AND NOT chatVoiceStatusWord (task
// 01a1424f-4526): this row is the SECOND surface that advertised `status` as the
// word that leaves, while `/voice status` prints the status and leaves the mode
// ON. A row that names a key the dispatcher does not act on is the same defect as
// a gate that claims a key no row names, which is what the finish-key half above
// exists to prevent — the two halves of one sentence must hold for BOTH keys.
func voiceLiveUsageNote() string {
	return fmt.Sprintf("[voice] speak — the text appears in the input line as it is recognised. "+
		"Press %s when you are DONE SPEAKING (the audio in hand is sent and the sentence becomes yours); "+
		"Esc or `%s` leaves the mode and keeps the line.",
		voiceLiveFinishKeyLabel, chatVoiceStopWord)
}
