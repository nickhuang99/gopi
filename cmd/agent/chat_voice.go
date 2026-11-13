package main

// chat_voice.go — `/voice`: the chat REPL's CONTINUOUS local voice-input mode.
//
// WHAT IT IS. A REPL-native slash command that turns the microphone into an
// input device for the editing line. While the mode is ON the run CAPTURES and
// TRANSCRIBES on this machine, over and over, and APPENDS every transcript to the
// editing line at the operator's caret — the line they may keep editing, and the
// line Enter finally submits as an ordinary turn. Escape leaves the mode: the
// recorder stops, no further transcript lands, and the text already on the line
// STAYS (exiting a microphone must never cost the operator their draft).
//
// WHY A SLASH COMMAND AND NOT A TOOL. Rule 3: the agent's tool surface is fixed.
// This is a surface the OPERATOR drives at their own prompt, and its state is the
// REPL's (which process is recording, what is on the line) — which no shared
// command table can see. It therefore sits with /quit, /clear, /tools, /queue and
// /info: answered by the REPL's own switch, documented by its own const block.
//
// WHAT IS REUSED, DELIBERATELY, and there is nothing here that is re-implemented:
// device enumeration and selection are voiceListDevices / voiceResolveSelection
// (the four-tier policy every other voice shape already reads); the recording is
// voiceRecord (device policy + liveness probe + "device is reachable but silent");
// the container and the engine seam are voicePCM16ToWAV / voiceStageCaptureAudio /
// transcribeFile — the SAME single engine entry point the bare `gopi tool
// voice_input` and both server shapes funnel through; the dependency check is
// voiceDepsReady. One capture implementation means a device that cannot be opened
// is reported identically whichever door was reached, and a fix to the policy
// cannot land on one door only.
//
// WHY THE RECORDER IS STOPPED BY A DEADLINE AND RE-ARMED, NOT SIGNALLED. Each
// round records ONE bounded window (voiceChatWindowSeconds) and the next round
// starts immediately: the same microphone, the same engine, a fresh window. A
// signal-based "stop when Escape arrives" would need a handle on a process
// captured behind voiceRecord's own context, i.e. a SECOND capture path — and the
// deadline is what actually bounds an abandoned microphone (a forgotten mode
// cannot leave the device open past one window, and Escape stops the mode at the
// next round boundary). The cost is stated rather than hidden: entering the mode
// costs up to one window's latency before the first transcript, and leaving it may
// let one already-started window finish (its transcript is DISCARDED, and the
// leaving note says so).

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaostream"
	"github.com/nick/gopi/internal/lineedit"
)

// The command's vocabulary, in ONE const block (Rule 1): the word the switch
// answers, the words its arguments accept, and the glyphs it draws. The usage
// block, both refusal sentences and the tests read the SAME spellings, so a
// rename cannot leave the help documenting a word the switch no longer accepts.
const (
	// chatVoiceCommandName is the slash word that owns this file. It is
	// REPL-native (like /quit, /queue, /info): it mutates REPL-session state the
	// shared command table cannot see, so the table must not be asked to
	// dispatch it, and chatReplSlashVocabulary — not the registry — answers
	// `/help voice`.
	chatVoiceCommandName = "voice"
	// chatVoiceStatusWord is the explicit spelling of the bare form: bare
	// `/voice` toggles, `/voice status` only reports. It exists so a scripted
	// line can ask without changing anything.
	chatVoiceStatusWord = "status"
	// chatVoiceStopWord leaves the mode, the same exit the Escape key performs.
	// It is the DOOR for a caller that has no keyboard to press Escape on (a
	// scripted line, a piped session, a test) — the mode is never un-leavable.
	chatVoiceStopWord = "stop"
)

// The bound of ONE recording window, in seconds. It is a BOUND, not a
// preference: the recorder is stopped by the clock, so a mode left on cannot hold
// the microphone open indefinitely, and Escape's effect is bounded by it. Named
// (Rule 1) because the usage text, the refusal that names it and the goroutine
// that enforces it all state the same number.
const voiceChatWindowSeconds = 6

// voiceChatWindowPause is how long the loop waits before arming the next window
// when a round produced NOTHING usable (a silence verdict, a failed transcription).
// Without it a device that refuses instantly would spin: the loop is a long-lived
// goroutine and an unthrottled retry is a busy loop against the recorder. The
// SUCCESS path does not pause — a recognised utterance starts the next window at
// once, which is the "keeps listening" behaviour the mode is for (Rule 27: this is
// a pacing constant, not load generation — nothing here saturates anything).
const voiceChatWindowPauseSeconds = 1

// voiceChatFinishWait bounds how long the teardown waits for the capture loop to
// finish the windows it is already holding before the stream is released. It is a
// TEARDOWN bound, never a recording window: the loop's finish transcribes the
// windows in hand (voiceChatFinisher.Drain) and that takes one engine call per
// window, so the bound is generous enough to cover a normal backlog and finite
// enough that a wedged engine cannot hold the microphone forever.
const voiceChatFinishWait = 30 * time.Second

// voiceChatMaxConsecutiveFailures is how many windows may FAIL IN A ROW before
// `/voice` gives up and leaves the mode (task 01a121fc, deliverable D; Rule 1: a
// named constant, never a literal at the comparison).
//
// WHY A RUN OF FAILURES AND NOT ONE. A single failed round used to end the mode
// outright (m.fail), which is a rule of the "one strike and the microphone is
// closed" shape: the engines this mode drives are a CLOUD CALL (an upload that
// refused one window) and a local process, both of which fail transiently — one
// timeout on one window ended a dictation session and told the operator their
// failure was fatal. The rule is now stated as what it always should have been:
// ONE failure is a fact about that window, reported and retried; a RUN of them is
// a fact about the mode's world (the device is gone, the endpoint is down, the
// model cannot run) and is the state Rule 15 forbids leaving half-open.
//
// WHY THREE. The counter is reset by ANY round that comes back without an error
// (including a silent one — silence is not a failure), so the run is consecutive
// by construction and cannot accumulate across a working session. Three is the
// smallest run that distinguishes "the engine hiccuped" from "the engine is not
// there": a transient refusal is one window, a dead endpoint or a vanished device
// fails every window, so the third failure arrives within ~(2 × window + 2 ×
// pause) seconds of a mode that is otherwise producing nothing — the operator
// waits a few seconds longer instead of losing the session to one bad window.
const voiceChatMaxConsecutiveFailures = 3

// voiceChatFailuresGiveUp is the ONE predicate behind "this run of failures ends
// the mode". Both loops (loop and liveLoop) ask it, so the window arm and the
// live arm cannot disagree about when a failing engine is fatal.
func voiceChatFailuresGiveUp(consecutive int) bool {
	return consecutive >= voiceChatMaxConsecutiveFailures
}

// chatVoiceSilenceMarker is the LOCAL FUN-ASR engine's own silence marker: the
// token it prints when a window carried no speech. It is the ENGINE's spelling,
// not gopi's — funasr-cli writes it as its SRT-mode "drop this segment" verdict,
// and the local arm streams the same token for a speechless window — so it gets a
// name here rather than a literal at the comparison (Rule 1/13: one definition,
// referenced everywhere).
//
// THE ROWS THAT REPORT IT LIVE IN chat_voice_note.go. Every note this mode prints
// is produced by that file's pure renderers — the shape of the feature's visible
// surface is decided in one place with a written contract over it, the same way
// /info's block is decided by chatInfoBlock (chat_info.go). Adding a note here as
// an inline template would be a second answer to "how does this feature look".
const chatVoiceSilenceMarker = "/sil"

// voiceChatUsage is the REPL's own usage block for the word: what `/help voice`
// prints. It is the ONE definition of this text (chatReplSlashVocabulary returns
// it), because the shared command table is not the owner of a REPL-native word —
// a second copy is exactly the drift the vocabulary test exists to prevent.
//
// The window length is interpolated from the constant the loop enforces, so the
// help can never promise a bound the recorder does not keep.
var voiceChatUsage = `Usage: /voice [status | stop]

/voice            turn voice mode ON if it is off, OFF if it is on. With it on,
                  this machine records the microphone and transcribes it in
                  ` + fmt.Sprintf("%d", voiceChatWindowSeconds) + `s windows with the console's engine (` + config.AudioEngineDefault + `
                  by DEFAULT; the first row it prints says where the audio goes);
                  every transcript is APPENDED at the caret, so you keep editing.
/voice status     report the mode without changing it.
/voice stop       leave voice mode (the same exit as Esc).

While the mode is on: type freely, edit the line, and press Enter to send the
whole line as ONE ordinary turn (a transcript is never treated as a command).
Esc leaves the mode and keeps whatever is on the line. The recorder is stopped
by the clock, so at most one window is in flight when you leave; its transcript
is discarded. A missing device, a missing model or a failed engine is reported
and leaves the mode OFF rather than open and producing nothing.

STREAMING ENGINES (` + config.AudioEngineStreaming + `) DO NOT WAIT FOR THE
WINDOW. On that engine the microphone is a continuous stream: packets go out as
they are captured, so the text grows in the input line WHILE you speak, shown as
a DRAFT (the last statement of the recogniser replaces the previous one — nothing
is appended until the sentence is settled). ` + voiceLiveFinishKeyLabel + ` is "I am done speaking":
it sends the audio in hand, settles the sentence onto the line as your own text,
and leaves the mode running so you can dictate the next one. Esc still leaves the
MODE, and still keeps the line. A finish key with no sentence in flight is not
the mode's key and does nothing.
`

// The mode's LIVE-STATUS vocabulary: the ONE definition of the cells the chat
// status row spends on `/voice`'s own state (Rule 1/13/14). It lives beside
// voiceChatUsage because it is the same kind of fact — this file's feature stated
// once — and both the composer that spends it (`voiceChatIndicatorFor`, below) and
// the row that shows it (cmd/agent/chat_stream.go) read THESE constants: the status
// code never spells a voice word.
//
// THE FIELD IS ONE THREE-CELL WORD, and that width is a MEASURED CONSTRAINT rather
// than a preference. The row is pinned at 100 cells with zero slack, and the block
// that hosts this field (the live hint, statusLiveCol) reserves 13 — the room the
// clock and the state word already use, and the room the sentence
// "<field> · <state word>" must fit in. Two measurements decided the spelling:
//
//   - A MARKER PLUS A WORD DOES NOT FIT THE THINKING STATE. A one-cell marker and
//     a gap would make the field 5 cells, and 5 + the block's separator (3) +
//     "think"+gap (6) is 14 — one cell past the block. The field would then have to
//     VANISH while the model is thinking, which is precisely when it is most useful
//     to know the microphone is open. Dropping the marker makes the same sentence
//     12 cells, and the WORD carries the meaning on its own.
//   - A THREE-CELL WORD IS THE WIDEST THAT KEEPS EVERY STATE. At 3 cells the field
//     fits every state word the block can draw, in both the full and the short
//     spelling, with a cell to spare for the animation ("rec · think\u00a0." is 13).
//
// The width test measures both claims against the production composer, so a future
// edit that lengthens a word fails there rather than on the operator's terminal.
const (
	// voiceChatLiveRecordingWord is what the row says while the round is
	// CAPTURING: the microphone is open and a window is being recorded.
	voiceChatLiveRecordingWord = "rec"
	// voiceChatLiveTranscribingWord is what it says while the round is
	// TRANSCRIBING: the window has closed and the engine is turning it into text.
	// The two words are the two things the mode is ever doing, and a reader who can
	// tell them apart knows whether speaking right now would be captured.
	voiceChatLiveTranscribingWord = "stt"
	// voiceChatLiveWordCells is the width of EITHER word, and the equality is the
	// point: both spellings are three cells, so the field's width does not move when
	// the phase does — the property that keeps the cells to its left still (the same
	// reason statusLiveCol is a reservation). The width test asserts the equality
	// rather than trusting these three characters.
	voiceChatLiveWordCells = 3
	// voiceChatLivePrefixCells is the field's whole width. The name says PREFIX
	// because that is what it is on the block: the text that precedes the state word,
	// with the block's own separator between them (see statusLiveField's voiced
	// branch). It is the number the width assertions do their arithmetic with, and it
	// is derived from the word constant above so the two cannot disagree.
	voiceChatLivePrefixCells = voiceChatLiveWordCells
)

// voiceChatLive is the mode's LIVE PHASE: which half of a round is in flight.
// It exists because the two halves are what the status row must tell apart —
// "the microphone is open" and "the engine is working" — and the mode already
// knows the difference at exactly the two points below (the loop's call to the
// recorder and its call to the engine are two calls, not one).
//
// IT IS NOT A SECOND STATE SOURCE (Rule 3). `on` is still the ONE answer to "is
// the mode running": this enum is a refinement of that answer, read through the
// same mutex, and the zero value (idle) is the same condition `on == false`
// carries. A caller can never see a phase without the mode being on (markLive
// refuses to move it for a loop that no longer owns the mode), so the field and
// the state cannot disagree on screen.
type voiceChatLive int

const (
	// voiceChatLiveIdle is the mode being OFF — the zero value, so a mode that
	// was never entered and a mode that has been left are the same state.
	voiceChatLiveIdle voiceChatLive = iota
	// voiceChatLiveRecording is the capture half of a round: the microphone is
	// open for one window.
	voiceChatLiveRecording
	// voiceChatLiveTranscribing is the engine half: the window has closed and the
	// transcript is being produced.
	voiceChatLiveTranscribing
)

// voiceChatIndicatorFor is THE ONE composer of the mode's status-row field: the
// phase in, the cells out. It answers "" for idle and that empty string is the
// field's whole "the mode is off" contract — the row shows nothing, which is what
// "Esc 之后消失" means at the cell level.
//
// WHY IT IS A PURE FUNCTION OF THE PHASE rather than a method reading the state:
// the row must be able to ask the question without holding the mode's mutex (see
// chatStatusSource's voice seam), and a pure function is also what the width
// test drives over the phase grid — one composer, so the drawn field and the
// measured field cannot be two different strings (Rule 14).
func voiceChatIndicatorFor(live voiceChatLive) string {
	switch live {
	case voiceChatLiveRecording:
		return voiceChatLiveRecordingWord
	case voiceChatLiveTranscribing:
		return voiceChatLiveTranscribingWord
	}
	// Idle: NO field. Not a blank word, not a zero state — nothing, so the live
	// block is composed exactly as it was before the mode existed. This empty
	// string IS the "Esc 之后消失" contract at the cell level.
	return ""
}

// chatVoiceState is the mode's whole state: WHO is recording and WHAT owns the
// insert road. It is shared by two goroutines — the REPL's own (the switch, which
// starts and stops the mode) and the capture loop — so it carries a mutex, and
// the mutex is held for a few field reads and never across a callout.
type chatVoiceState struct {
	mu sync.Mutex
	// on reports that the loop is running and transcripts are landing.
	on bool
	// live is the round's current PHASE, for the status row (voiceChatLive). It
	// is guarded by the same mutex as `on` and is the refinement of it described
	// on that type: `on` false implies idle, and markLive never moves this field
	// for a loop that no longer owns the mode.
	live voiceChatLive
	// device is the selection's human description, for the status row and the
	// entered note. It is empty until a device has been resolved.
	device string
	// started is the number of times the mode has been entered, so the loop of an
	// EARLIER session cannot stop the mode a later one started (see voiceStop).
	started int
	// stop closes to tell the running loop to leave. It is per-entry.
	stop chan struct{}
	// inLoop is the GOROUTINE ID of the capture loop of the CURRENT entry, and 0
	// when none is running. It exists so the teardown can tell "somebody outside
	// the loop is leaving" from "the loop itself is leaving" — the difference
	// between a wait that releases the capture and a wait that deadlocks on the
	// caller (see shutdown) — and it is an IDENTITY, not a flag the loop sets
	// about itself: the same rule bandwait.go's band seam decides with, and for
	// the same reason. THE ZERO IS THE POINT: the id is cleared when the loop
	// RETURNS (markLoopGoroutine's deferred restore), so a teardown that runs
	// after the loop is gone joins it instead of skipping the wait (measured:
	// -cpu=1 -count=40 on the voice PTY cases parked two goroutines in lineedit's
	// band wait — a never-cleared flag made every foreign teardown skip the join,
	// the terminal was closed under a loop still inside its notice, and the notice
	// was left waiting for a drainer that had exited).
	inLoop int64
	// loopDone closes when the capture loop of the CURRENT entry has returned, so
	// the teardown can wait for it to finish the capture before the stream is
	// released under it (the wait is skipped when the loop is the caller — see
	// shutdown).
	loopDone chan struct{}
	// gate is the key gate this mode installed, kept so the exit can UNINSTALL
	// exactly what it installed rather than clearing whatever is there.
	gate *voiceChatGate
	// liveUtterance is the utterance in flight, or nil when none is (the live arm
	// only — see voiceChatLiveRunner). It carries the finish signal AND the one
	// object allowed to close it, and nil is what tells the key gate that the
	// finish key is NOT the mode's to take right now: there is no utterance for
	// "I am done speaking" to end, so the key stays the editor's (which ignores
	// it — internal/lineedit's KeyLiveFinish has no handler).
	//
	// IT IS A STRUCT RATHER THAN A BARE CHANNEL because the signal has TWO
	// independent endings — the operator's key and the mode being left — and a
	// channel closed by two owners is a panic. The sync.Once is that single owner
	// (voiceLiveUtterance.finish), so both endings are the same event from the
	// arm's point of view and neither can be the second close.
	//
	// It is per-utterance and cleared by the utterance itself, so a stale signal
	// cannot end the NEXT sentence: the loop sets it before it starts an utterance
	// and clears it after, both under this mutex.
	liveUtterance *voiceLiveUtterance
	// liveReports are the utterances the live arm has completed, in order. They
	// are kept so the mode can report what it heard (and because the numbers are
	// what the acceptance evidence quotes) rather than leaving them inferable only
	// from the line's text.
	liveReports []voiceLiveReport
	// failStreak counts the CONSECUTIVE failed rounds of the current entry (task
	// 01a121fc, deliverable D). It is shared state rather than a loop-local
	// variable because the mode has two loops (the window arm and the live arm)
	// and "how many windows in a row has this engine failed?" is one fact about
	// the session, not one per loop shape — the ONE predicate that reads it
	// (voiceChatFailuresGiveUp) then cannot mean two different things.
	//
	// IT IS CLEARED BY SUCCESS (any round that returns without an error, silence
	// included) and by a new entry, so the run it measures is genuinely
	// consecutive and cannot accumulate across a working session.
	failStreak int
}

// voiceChatSession is the input side of the seam: what the mode needs from the
// REPL to put text on the line and write its own rows.
//
// IT IS AN INTERFACE RATHER THAN *chatInput so the mode is testable without a
// terminal, and so the two dependencies are stated where they are used: the
// INSERT road (which the editor's read loop owns) and the NOTICE road (which the
// same single writer owns). A caller that cannot supply both is refused by the
// seam check, not by a nil panic halfway through a recording.
type voiceChatSession interface {
	// VoiceInsertLine appends text to the editing line at the caret.
	VoiceInsertLine(text string) bool
	// VoiceNotice writes one of the mode's own rows to the console, as plain
	// text. It is kept for the rows a carrier wants verbatim; the mode itself
	// uses VoiceNoticeStyled below.
	VoiceNotice(text string)
	// VoiceNoticeStyled writes one of the mode's own rows in the RUN'S OWN
	// system voice: dim, low-contrast, never a highlight (the operator's ruling
	// of 2026-10-22 — a remark the surface makes about itself must not read as
	// an answer or as an incident).
	//
	// The carrier takes the TEXT and not a style table, on purpose: the emphasis
	// table is resolved ONCE at startup and lives where it was resolved
	// (chatInput.tool, from the run's palette), so a mode that carried a copy
	// would be a second capability decision with its own second answer.
	//
	// A carrier with no emphasis capability (a cooked run, a non-terminal sink)
	// draws the text unchanged — the historic bytes, with no sequence at all.
	VoiceNoticeStyled(text string)
	// VoiceEditor returns the editor the mode installs its key gate on, or nil
	// on a carrier with no editor (the cooked path).
	VoiceEditor() *lineedit.Editor
	// VoicePublishStatus wakes the status row's repaint. It exists because the
	// mode's phase is a fact the row shows, and the row is a VIEW: a state change
	// that nobody publishes is a change the operator sees only on the next
	// unrelated repaint — measured here as the difference between "the row says
	// ♪ the moment the mode starts" and "the row says it whenever the turn
	// happens to move next", which is the staleness this seam removes.
	//
	// It is the SAME publish road every other status change takes
	// (chatInput.PublishStatus: a flag plus a wake, never a terminal write), so
	// the editor's read loop stays the only writer on the fd.
	VoicePublishStatus()
}

// voiceChatMode is the mode ONE REPL session owns. It exists per session (the
// REPL builds it once), never per invocation: the gate it installs and the loop
// it starts must outlive the `/voice` line that created them.
type voiceChatMode struct {
	// sess is what the mode reaches the console and the editing line through.
	sess voiceChatSession
	// state is the shared mode state (see chatVoiceState).
	state chatVoiceState
	// recorder is the seam the loop records and transcribes through. It is an
	// interface so a test can drive the whole loop — the append, the failure
	// path, the exit — without a microphone or a 910MB model, and so the
	// PRODUCTION wiring is one small implementation of two methods (below).
	recorder voiceChatRecorder
	// options are the engine/device settings every round is transcribed with. The
	// mode resolves them ONCE, at entry, so a status query and a running loop
	// cannot disagree about which device or engine they are talking about.
	options *voiceOptions
	// resolveOpts and depsMissing are the TWO production calls enter() makes
	// before it touches the microphone: build the engine settings, and check the
	// engine's dependencies.
	//
	// THEY ARE SEAMS, and the reason is the acceptance criterion they carry —
	// "a missing model or device is reported and leaves the mode OFF". That path
	// is only testable if the settings and the check can be supplied, and the
	// ALTERNATIVE is worse than the seam: a test that reaches the operator's real
	// profile would either find their 910MB of weights (so nothing is verified) or
	// not find them (so the test asserts the machine's state, not the code's).
	// Nil means "the production calls" (chat_voice.go's own functions), so a mode
	// built by newChatVoiceMode needs no wiring — the seam exists for the test,
	// and the production path is the one the REPL builds.
	resolveOpts func() (*voiceOptions, error)
	depsMissing func(*voiceOptions) string
}

// voiceOpts resolves the engine settings, through the seam when one is installed
// and through the production path otherwise. It is the ONE reader of that field,
// so enter() and any future caller cannot disagree about which was used.
func (m *voiceChatMode) voiceOpts() (*voiceOptions, error) {
	if m.resolveOpts != nil {
		return m.resolveOpts()
	}
	return chatVoiceResolveOptions()
}

// voiceDepsMissing is voiceOpts's sibling for the dependency check.
func (m *voiceChatMode) voiceDepsMissing(opts *voiceOptions) string {
	if m.depsMissing != nil {
		return m.depsMissing(opts)
	}
	return voiceDepsReady(opts)
}

// voiceChatRecorder is the two operations one round performs. Both are provided
// by the existing voice_input building blocks (see voiceChatRecorderLocal); the
// interface is what makes the ROUND testable, not a second implementation of
// capture.
type voiceChatRecorder interface {
	// Select resolves a capture device, returning the human description of the
	// selection plus any notes the policy wants printed. An error means no
	// device: the mode must not be entered.
	Select(device string) (string, []string, error)
	// Round records ONE window, transcribes it locally, and returns the text.
	Round(opts *voiceOptions, seconds int) (string, error)
	// Close releases whatever the recorder holds for the mode's whole life. It is
	// part of the interface because ONE production recorder holds a live capture
	// stream (voiceChatRecorderStaged): a mode that stopped without closing it
	// would leave the microphone held by a process nobody owns — the half-open
	// state Rule 15 forbids. A recorder with nothing to release says so with a
	// no-op rather than by being absent from the contract.
	Close()
}

// voiceWindowReporter is the OPTIONAL half of the recorder seam: a recorder that
// cuts windows out of one stream can say HOW the last boundary was reached, and
// the mode reports the one case the operator needs told — a cut taken inside
// speech, where a word may be split across two windows and the engine is being
// asked to transcribe half of it.
//
// It is optional on purpose: a recorder that records one window per process has no
// boundary to describe, and forcing it to implement this would be inventing a fact
// it does not have.
type voiceWindowReporter interface {
	// LastWindow reports the boundary the last Round ended on.
	LastWindow() voiceWindowSpan
}

// voiceRoundGate is the OPTIONAL half of the recorder seam that lets a recorder
// hold a window OPEN: RoundGate returns a channel the loop PARKS on before it
// calls Round, and a nil return means "never wait" — the ordinary recorder, and
// every implementation written before this seam existed.
//
// WHY IT EXISTS, and why it is on the RECORDER rather than on the mode. The
// production recorders pace themselves by the wall clock: voiceChatWindowSeconds
// (6s) elapses between windows, and the operator sees one row per 6 seconds. A
// scripted recorder computes its window in MICROSECONDS, so a case built on one
// produces rounds far faster than any real capture, and the loop cannot outrun
// the case's own bound by waiting: a scripted recorder answers a silent window
// the instant it is called, so the only clock between two silent windows is the
// pacing gap itself (voiceChatWindowPauseSeconds, 1s).
//
// RE-MEASURED 2026-10-12 (row 01a12d65) on the tree this paragraph was written
// from, 0b3c1de29, 4 runs, the recorder's own Round stamp: the loop answered
// windows at t = 0/1001/2003 ms, 0/1002/2003 ms, 0/1000/2001 ms and
// 0/1002/2004 ms — THREE silent windows inside the case's 3s bound (voicePtyWait;
// a red run ends at ~3.58s), one second apart. Not ten windows in 3.2 seconds.
// The first reading of this number was the burst a unit-level round would imply;
// the loop's own clock is the pacing constant.
//
// WHAT DOES HOLD, and why the seam is here. Each silent window writes its own row,
// the rows enter the answer band, and a band taller than the 14-row pty scrolls
// the earliest ones — the privacy row the case was waiting for — off the screen.
// The row's STAY on that grid is a race, not a schedule: sampling the painted grid
// every 200us for the case's whole bound, the phrase was on the grid for ~2-3ms in
// 2 of 3 runs and was never observed in the third (0 of ~2400 samples), while the
// announcement (WriteNotice's tee) fired at t=55-68us every run. So a row can be
// announced inside 0.1ms and leave the grid a few milliseconds later, and a grid
// read cannot tell that from a row that was never written — which is why the case
// read the grid as if the notice had never been announced.
//
// The PACING is the recorder's own business (it is the recorder that has a real
// clock), so the wait lives there: a case parks each round until it has read what
// it needs, exactly as the gated SSE mocks do for the model seam. The mode's
// obligation is only to HONOUR the seam — and it is an obligation, not a
// courtesy: a mode that stopped asking would make every gated case silently
// ungated (Rule 15: a gate nobody consults is not a gate).
type voiceRoundGate interface {
	// RoundGate returns the channel the next Round must wait on, or nil to run
	// immediately.
	RoundGate() <-chan struct{}
}

// voiceRoundGateOf reads the optional gate from a recorder, if it has one.
func voiceRoundGateOf(rec voiceChatRecorder) <-chan struct{} {
	g, ok := rec.(voiceRoundGate)
	if !ok {
		return nil
	}
	return g.RoundGate()
}

// voiceChatFinisher is the OPTIONAL half of the recorder seam that exists to make
// the END of a capture lossless: a recorder that cuts rounds out of ONE live stream
// can be holding a window the engine has not been asked about when the operator
// leaves, and "the audio spoken before the key was pressed" must be transcribed
// rather than discarded (task 01a11b81 — that is exactly where the tail of a long
// recording went).
//
// It returns the transcripts it produced, in stream order, for the mode to insert
// on the line it is about to hand back. It is optional because a recorder that
// records one window per process has nothing in hand: forcing it to implement this
// would be inventing a fact it does not have.
type voiceChatFinisher interface {
	// Drain finishes the capture and returns the transcripts of the windows that
	// were already read but not yet transcribed, in stream order.
	Drain() []string
}

// voiceChatFinishReporter is the OPTIONAL half that says whether the finish
// REACHED every window it had in hand. A bounded finish that ran out of budget has
// left part of the capture unasked, and that must be SAID rather than inferred from
// a shorter transcript (Rule 15: a silent truncation is the defect, not the
// inconvenience).
type voiceChatFinishReporter interface {
	// FinishTimedOut reports that the finish budget ran out with audio still in
	// hand.
	FinishTimedOut() bool
}

// voiceChatRecorderLocal is the PRODUCTION recorder: the same calls the bare
// `gopi tool voice_input` makes, in the same order, with nothing added.
type voiceChatRecorderLocal struct{}

// Select is voiceListDevices + voiceResolveSelection — the four-tier device
// policy, unchanged, with its notes passed through so the operator reads the same
// tier explanation whichever door reached the microphone.
func (voiceChatRecorderLocal) Select(device string) (string, []string, error) {
	devices, notes, err := voiceListDevices(voiceRunCommand)
	if err != nil {
		return "", notes, err
	}
	sel, selNotes, err := voiceResolveSelection(device, devices, voiceRunCommand, voiceCaptureAttempt)
	notes = append(notes, selNotes...)
	if err != nil {
		return "", notes, err
	}
	return voiceDescribeCapture(sel, voiceChatWindowSeconds), notes, nil
}

// Round captures one window and transcribes it through the SINGLE engine seam
// (transcribeFile), staging the recording in the runtime scratch class exactly as
// the bare verb does and removing it as soon as the engine is done with it.
//
// A device that produced no audio is an ERROR (voiceRecord's own verdict), not an
// empty transcript: "the microphone is muted" and "nobody spoke" are different
// facts and the mode must not report the first as the second.
func (voiceChatRecorderLocal) Round(opts *voiceOptions, seconds int) (string, error) {
	devices, _, err := voiceListDevices(voiceRunCommand)
	if err != nil {
		return "", err
	}
	sel, _, err := voiceResolveSelection(opts.device, devices, voiceRunCommand, voiceCaptureAttempt)
	if err != nil {
		return "", err
	}
	pcm, err := voiceRecord(sel.Device, seconds)
	if err != nil {
		return "", err
	}
	path, cleanup, err := voiceStageCaptureAudio(voicePCM16ToWAV(pcm))
	if err != nil {
		return "", err
	}
	text, _, terr := transcribeFile(opts, path)
	cleanup()
	if terr != nil {
		return "", terr
	}
	return strings.TrimSpace(text), nil
}

// Close is the local recorder's no-op: this recorder spawns a process PER ROUND
// and stops it inside Round, so there is nothing held between rounds. The method
// exists to satisfy the seam both production recorders share (see
// voiceChatRecorder.Close) — a recorder that leaks nothing says so here rather
// than by being exempt from the contract.
func (voiceChatRecorderLocal) Close() {}

// chatVoiceTranscriptText is THE ONE judgement of "did this round carry speech?",
// and it is the reason a `/sil` window is not a transcript.
//
// WHY THE MODE AND NOT THE ENGINE SEAM. The seam (transcribeFile and its two
// arms) answers a different question — "what did the engine say?" — and its answer
// is the engine's own text, which the bare `gopi tool voice_input` and both server
// shapes consume VERBATIM (their transcripts are read by a human inspecting a
// recording, and `/sil` there is the engine's honest verdict about that file).
// Teaching the seam to rewrite the marker into "" would change what those
// surfaces print, per engine, at one stroke — the cloud arm never emits the
// marker at all. The mode's question is narrower and its own: "is there anything
// to APPEND to the operator's line?", and the marker is an answer of NO.
//
// WHY IT IS APPLIED BY THE LOOP, not inside voiceChatRecorderLocal.Round: the
// loop's `text == ""` arm is already THE silence signal (it writes
// chatVoiceSilentNote and re-arms the window), and the recorder is an INTERFACE
// (voiceChatRecorder) precisely so a round can come from somewhere else. Judging
// inside the one concrete recorder would leave that silence arm unreachable for
// every other recorder — a fix that holds only for the implementation that
// happened to be wired, which is the shape of the defect being repaired. The
// judgement therefore sits at the mode's single ingest boundary, one line before
// the branch it feeds.
//
// IT ONLY EVER DECIDES SILENT OR NOT — it never rewrites text it passes on. The
// trailing space of a transcript is part of what the engine said, and the append
// contract is that the engine's words land at the caret unchanged; trimming here
// would be a second, silent edit of a transcript (measured: it turned the append
// case's `drhello world` into `drhelloworld`). The one trim that already existed
// stays where it was, in voiceChatRecorderLocal.Round.
//
// It is deliberately conservative in the OTHER direction: a window is silent only
// when it is the marker REPEATED and nothing else (the engine streams one marker
// per chunk, so one window's buffer can hold `/sil/sil`), while speech that merely
// BEGINS with those characters (`/silence`) is not silence and a real word
// followed by the marker is still speech — the marker is kept rather than
// silently deleted from a transcript that also carries words.
func chatVoiceTranscriptText(text string) string {
	rest := strings.TrimSpace(text)
	if rest == "" {
		return ""
	}
	for rest != "" {
		if !strings.HasPrefix(rest, chatVoiceSilenceMarker) {
			return text
		}
		rest = rest[len(chatVoiceSilenceMarker):]
	}
	return ""
}

// newChatVoiceMode builds the session's mode. It takes the TWO roads the mode
// needs (insert + notice) and the editor the gate is installed on; a caller with
// no editor gets a mode that refuses to start (see handleLine), never one that
// records into nothing.
func newChatVoiceMode(sess voiceChatSession, rec voiceChatRecorder) *voiceChatMode {
	return &voiceChatMode{sess: sess, recorder: rec}
}

// handleLine answers ONE `/voice` line: the mutation, its refusal, and the
// visible note row every branch owes.
//
// The ORDER of the checks is the contract, and it is the whole of the "no
// half-open mode" requirement: the seam, then the dependencies, then the device —
// each one BEFORE anything is started, so a run that cannot carry a transcript
// never opens the microphone, and a run with no engine never spends a recording.
func (m *voiceChatMode) handleLine(input voiceChatSession, msg string) {
	if m == nil || input == nil {
		return
	}
	if m.sess == nil {
		m.sess = input
	}
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(msg), completionSlashPrefix+chatVoiceCommandName))
	switch {
	case strings.EqualFold(arg, chatVoiceStatusWord):
		m.writeStatus()
		return
	case strings.EqualFold(arg, chatVoiceStopWord):
		m.leave(chatVoiceLeftByWord)
		return
	case arg != "":
		// An argument that is neither a word this command accepts nor whitespace
		// is refused BY NAME, listing what it does accept — never silently read
		// as the toggle (Rule 15).
		m.notice("%s\n", voiceNoteArgRefusal(arg))
		return
	}
	if m.active() {
		m.leave(chatVoiceLeftByToggle)
		return
	}
	m.enter()
}

// The two exit reasons the leaving note names. They are constants so the note and
// the tests cannot disagree about the spelling of "who left".
const (
	chatVoiceLeftByToggle = "you asked me to"
	chatVoiceLeftByWord   = "stop"
	chatVoiceLeftByEscape = "Esc"
	chatVoiceLeftByFailed = "a failed round"
	// chatVoiceLeftByCaptureEnded is the exit whose capture was closed under a
	// STILL-ON mode (the sentinel): the microphone is gone, and the mode leaves
	// rather than keep a status row claiming it — but nothing FAILED, so the
	// exit is not the failure note and not "a failed round".
	chatVoiceLeftByCaptureEnded = "the capture ended"
)

// chatVoiceLeftTails pairs every exit reason with the note's second half, so the
// sentence and the reason cannot disagree about WHO ended the capture.
//
// THE PAIRING IS THE FIX, not a formatting preference. The reasons split into two
// causally OPPOSITE groups and one template cannot state both:
//
//   - THE MODE ends the capture (the operator asked, or the round failed): the
//     recorder is this mode's and it is being stopped — chatVoiceLeftStoppingTail.
//   - THE CAPTURE ended first (the sentinel: a finite source ran out under a live
//     mode): the mode is taking itself down over something it no longer controls,
//     so "the recorder is stopping" is the direction of the event reversed —
//     chatVoiceLeftRanOutTail.
//
// A reason with no entry falls back to the stopping sentence (its case is the
// unmarked one), so adding a reason is never a silent blank in the note.
var chatVoiceLeftTails = map[string]string{
	chatVoiceLeftByToggle:       chatVoiceLeftStoppingTail,
	chatVoiceLeftByWord:         chatVoiceLeftStoppingTail,
	chatVoiceLeftByEscape:       chatVoiceLeftStoppingTail,
	chatVoiceLeftByFailed:       chatVoiceLeftStoppingTail,
	chatVoiceLeftByCaptureEnded: chatVoiceLeftRanOutTail,
}

// chatVoiceLeftTail is the ONE reader of the pairing above — the leaving note and
// its regression case reach the sentence through this function, so neither can
// hold a private copy of "which reason says what" (Rule 14).
func chatVoiceLeftTail(reason string) string {
	if tail, ok := chatVoiceLeftTails[reason]; ok {
		return tail
	}
	return chatVoiceLeftStoppingTail
}

// enter starts the mode: it checks the seam, the engine dependencies and the
// device IN THAT ORDER, and only then starts the capture loop.
//
// EVERY refusal is a sentence and leaves the mode OFF: entering a mode whose
// transcripts cannot land (no editor) or cannot be produced (no model, no device)
// would be the half-open state Rule 15 forbids — a status row claiming a
// microphone nobody is reading.
func (m *voiceChatMode) enter() {
	if m.sess.VoiceEditor() == nil {
		m.notice("%s\n", voiceNoteNoEditorRefusal())
		return
	}
	opts, err := m.voiceOpts()
	if err != nil {
		m.notice("%s\n", voiceNoteDepsRefusal(err.Error()))
		return
	}
	if missing := m.voiceDepsMissing(opts); missing != "" {
		m.notice("%s\n", voiceNoteDepsRefusal(missing))
		return
	}
	engine := voiceResolveEngine(opts)
	// ONE row per engine, and the row says what that engine actually needs: the
	// weights line is printed only when weights are USED (naming a models path
	// the cloud arm never opens would be a false statement about where the
	// transcript came from), and the cloud arm prints the disclosure instead —
	// the same sentence the CLI prints, from the same one function. The chat path
	// is a REPL command rather than a subcommand, so it never passes through
	// runVoiceSubcommand; a second spelling here is how the two surfaces would
	// come to describe different routes to the same engine.
	switch engine {
	case config.AudioEngineRecording:
		// NO PREFIX HERE: voiceDoubaoPrivacyNotice ALREADY carries the row's
		// `[voice] ` prefix (it is the same one function the CLI prints through,
		// cli_voice_engine.go), so a second spelling at this call site printed
		// every chat privacy row as `[voice] [voice] PRIVACY …` (Rule 13: the
		// value's spelling lives with the value, never re-typed by a caller).
		m.notice("%s\n", voiceNoteEngineRow(voiceDoubaoPrivacyNotice(opts)))
	case config.AudioEngineStreaming:
		// The streaming arm's row names the channel and the credential FILE
		// (never its value) — the same facts the CLI prints, from the same
		// place.
		m.notice("%s\n", voiceNoteEngineRow(fmt.Sprintf(chatVoiceStreamEngineLabel, engine,
			voiceStreamEffectiveEndpoint(opts), doubaostream.DefaultCluster, voiceStreamCredentialLabelForOpts(opts))))
		// THE LIVE ARM'S OWN ROW, and only when the recorder can actually run it:
		// the row advertises a key that ends an utterance, so printing it for a
		// recorder that cannot run one would teach the operator a key that does
		// nothing (Rule 15).
		if _, ok := m.recorder.(voiceChatLiveRunner); ok {
			m.notice("%s\n", voiceNoteEngineRow(voiceLiveUsageNote()))
		}
	case config.AudioEngineSauc:
		// The v3 arm's row: the same facts as the v2 row above (where the
		// audio goes, WHERE the credential came from) with the credential's
		// SHAPE named, because on this channel the shape is the difference
		// between a working run and a 403 that reads like a missing
		// entitlement. The engine reaches the SAME transcription seam as every
		// other one (transcribeFile → transcribeFileSauc).
		m.notice("%s\n", voiceNoteEngineRow(fmt.Sprintf(chatVoiceSaucEngineLabel, engine,
			voiceSaucEffectiveEndpoint(), voiceSaucEndpointOrigin(voiceSaucEffectiveEndpoint()),
			voiceSaucEffectiveResourceID(), voiceSaucCredentialLabel(opts))))
		// THE ORIGIN OF THE URL IS PART OF THAT ROW, and the reason is a
		// measured, silent failure mode rather than thoroughness: the endpoint
		// has TWO producers (this host's settings document, or the package's
		// default) and a configured one wins VERBATIM on BOTH arms — so the LIVE
		// arm's own default is unreachable on a host that configured the value
		// for the FILE arm. A row that named the destination without naming who
		// chose it left the operator unable to tell which of the two they were
		// looking at.
		//
		// MERGE NOTE (union of 79190ad05 and 34883c049): the credential-channel
		// fact and the endpoint-origin fact are ORTHOGONAL — one says WHERE the
		// credential came from, the other WHO CHOSE the URL — and both sides are
		// additive, so the row carries both: 34883c049's %s endpoint field in the
		// label, and 79190ad05's voiceSaucCredentialLabel(opts) as its last
		// argument.
		// THE FALLBACK IS ANNOUNCED HERE TOO (Rule 15): the CLI's own shape
		// prints this sentence before it uploads, and the chat REPL is a second
		// surface on the same engine — an operator who entered /voice must learn
		// the same fact from the same one function, not by reading the path out
		// of the row above and knowing what it means.
		if note := voiceSaucCredentialSource(opts).fallbackNote(); note != "" {
			m.notice("%s\n", voiceNoteEngineRow(note))
		}
		// THE LIVE ARM OWES ONE MORE SENTENCE, and it is the one this whole
		// change exists for: when the endpoint is NOT the bidirectional one, the
		// text cannot appear while the operator speaks (see
		// voiceSaucLiveEndpointNote). Announced, never enforced: a configured
		// endpoint keeps winning.
		//
		// IT RIDES THE SAME GUARD AS THE v2 ROW ABOVE, and for the same reason: a
		// recorder that cannot run a live session falls through to the window
		// loop, so a row that spoke about "the LIVE arm" there would describe an
		// arm this run does not have (Rule 15).
		if _, ok := m.recorder.(voiceChatLiveRunner); ok {
			m.notice("%s\n", voiceSaucLiveEndpointNote(voiceSaucLiveEndpoint()))
		}
	default:
		// voiceDeploymentSource is master's rung provenance (791f63568): the
		// local arm's weights line names the rung that supplied them, and the
		// two rows are ORTHOGONAL — one says WHICH engine, this one says which
		// deployment it read. The union keeps both.
		m.notice("%s\n", voiceNoteEngineRow(fmt.Sprintf(voiceEngineProvenanceLabel, engine, opts.models, opts.runtime, voiceDeploymentSource(opts))))
	}

	desc, notes, err := m.recorder.Select(opts.device)
	for _, n := range notes {
		m.notice("%s\n", voiceNoteRecorderLine(n))
	}
	if err != nil {
		m.notice("%s\n", voiceNoteDeviceRefusal(err))
		return
	}
	m.options = opts

	// THE GATE IS INSTALLED BEFORE THE LOOP STARTS, so there is no window in
	// which the mode is on and Escape is unclaimed — a key pressed in that window
	// would go to the editor and the watcher would read "my Esc did nothing".
	gate := newVoiceChatGate(m)
	gate.chain(m.sess.VoiceEditor().KeyGate())
	m.sess.VoiceEditor().SetKeyGate(gate)

	m.state.mu.Lock()
	m.state.on = true
	m.state.device = desc
	// The phase starts IDLE and is set by the loop at the moment it really opens
	// the microphone (markLive in loop) — not here: `enter` runs while the device
	// is still being selected, and claiming "recording" before the capture starts
	// is the lie the field exists to prevent.
	m.state.live = voiceChatLiveIdle
	m.state.started++
	// A NEW ENTRY BEGINS WITH A CLEAN RUN: the previous session's failures ended
	// that mode (or it was left by the operator), and carrying their count into
	// this one would make the FIRST failure of a fresh session the third of a run
	// nobody had — the mode would give up on the operator's first bad window.
	m.state.failStreak = 0
	entry := m.state.started
	stop := make(chan struct{})
	// THE ENTRY'S OWN JOIN CHANNEL, handed to the loop below rather than left to
	// be read back off the field. shutdown() CLEARS this field while holding the
	// mutex, BEFORE it waits on the channel — so a loop that re-read the field to
	// decide whether to close it was racing a teardown that had already nil'd it
	// (shutdown reads nil, launch reads nil, and the close is skipped by both).
	// The loser of that race closed nothing, and shutdown then waited out the
	// whole voiceChatFinishWait on a goroutine that had already returned: that is
	// the flat-CPU window this parameter removes.
	loopDone := make(chan struct{})
	m.state.stop = stop
	m.state.gate = gate
	m.state.loopDone = loopDone
	m.state.mu.Unlock()

	m.notice("%s\n", voiceNoteEntered(voiceNoteEnteredFacts{
		Device:    desc,
		Engine:    engine,
		WindowSec: voiceChatWindowSeconds,
		// THE WORD THE ROW TEACHES IS THE WORD THAT LEAVES (task
		// 01a1424f-4526): this argument was chatVoiceStatusWord, so the entered
		// block told the operator to type `/voice status` to get out — and
		// `/voice status` PRINTS the status and leaves the mode ON (handleLine
		// above). The advertised word is the one the dispatcher acts on
		// (chatVoiceStopWord), which is also what the status-ON block already
		// says; the regression case that crosses advertisement and behaviour is
		// chat_voice_leaveword_test.go.
		LeaveWord: chatVoiceStopWord,
	}))
	// The row repaints now, so the status line catches up with the mode at the
	// moment the operator turned it on rather than at the next unrelated repaint.
	// The FIELD itself stays off until the loop opens the microphone (markLive).
	m.publishStatus()
	// THE LOOP THE RUN GETS IS CHOSEN HERE, ONCE, from two facts: the engine the
	// resolver answered and the seam the recorder implements. Selecting it in one
	// place is what makes "the non-streaming engines are unaffected" a property of
	// the code rather than a promise — every other engine reaches the window loop
	// by falling through, with nothing about it changed.
	if _, live := m.recorder.(voiceChatLiveRunner); live && voiceLiveEngineSelected(opts) {
		go m.liveLoop(entry, stop, loopDone)
		return
	}
	go m.loop(entry, stop, loopDone)
}

// leave stops the mode: the loop is told, the gate is UNINSTALLED (its chained
// predecessor is restored, so a replay's own controls are not switched off by
// this exit), and the fact is announced with the reason.
//
// IT IS IDEMPOTENT AND ATTRIBUTABLE. A leave from a mode that is already off says
// so rather than silently doing nothing — a second Esc that produced no row would
// be indistinguishable from a key the surface dropped (Rule 15).
func (m *voiceChatMode) leave(reason string) {
	if m == nil {
		return
	}
	if !m.shutdown() {
		m.notice("%s\n", voiceNoteStatusOff())
		return
	}
	m.notice("%s\n", voiceNoteLeft(reason, chatVoiceLeftTail(reason)))
}

// shutdown stops the mode: it tells the capture loop to leave, restores the key
// seam and clears the state. It reports whether it was ON, so the announcing half
// (leave) can say "already off" rather than pretend it did something.
//
// IT IS THE SILENT HALF ON PURPOSE, and the split exists for the session's END.
// The REPL exits through several paths (/quit, /exit, Ctrl+D, a read error, a
// signal), and every one of them must TAKE THE MICROPHONE DOWN — a capture loop
// left parked past the console it types into is the half-open mode Rule 15
// forbids, and the gate it installed must not outlive the session either. None of
// those paths may write a row at that moment (the exit path erases the screen and
// hands it back), so teardown and announcement are two calls: the exit defers
// shutdown, the operator's own exits use leave.
func (m *voiceChatMode) shutdown() bool {
	if m == nil {
		return false
	}
	m.state.mu.Lock()
	if !m.state.on {
		m.state.mu.Unlock()
		return false
	}
	m.state.on = false
	m.state.device = ""
	// The phase goes with `on` — the field disappears the instant the mode is off,
	// which is the mechanism behind "Esc 之后消失": the row's composer reads IDLE and
	// draws no field at all (voiceChatIndicatorFor).
	m.state.live = voiceChatLiveIdle
	stop := m.state.stop
	gate := m.state.gate
	loopDone := m.state.loopDone
	// THE UTTERANCE IN FLIGHT IS RELEASED WITH THE MODE, and leaving it installed
	// is the stale-signal defect this line exists to prevent: the field is read by
	// the gate, so a value surviving the exit is a signal the NEXT entry could
	// claim and close — an utterance nobody is speaking, ended by a key aimed at a
	// sentence that has already been abandoned.
	//
	// IT IS CLEARED RATHER THAN CLOSED, and the difference matters: closing would
	// be this path claiming the operator pressed the finish key, which they did
	// not. The live arm's own utterance ends the way it ends on any teardown — the
	// capture is released below, the reader's hand-off closes, and the arm finishes
	// with the audio it had (chat_voice_live.go's Live, its `feed.ended` arm).
	m.state.liveUtterance = nil
	m.state.stop = nil
	m.state.gate = nil
	m.state.loopDone = nil
	m.state.mu.Unlock()
	// THE LOOP FINISHES THE CAPTURE BEFORE THE STREAM IS RELEASED, and the ORDER is
	// the fix, not a detail: the loop is the only consumer of the reader's
	// hand-off, so the audio the device had already produced when the operator left
	// is transcribed by the loop's own finish (voiceChatFinisher.Drain) — and only
	// then is the capture closed. Releasing the stream first (the pre-fix order)
	// discarded exactly the windows in hand, which is where the tail of a long
	// recording went (task 01a11b81).
	close(stop)
	// THE WAIT IS SKIPPED WHEN THE LOOP IS THE CALLER. A failed round leaves the
	// mode from INSIDE the loop (loop → fail → leave → shutdown), and waiting there
	// for the loop to return would be waiting on itself — a deadlock, not a
	// safeguard. The loop's own exit path finishes the capture through its defer
	// (finishCapture) and closes loopDone on the way out, so nothing is skipped:
	// only the WAIT is conditional on who is asking.
	// THE IDENTITY IS READ UNDER THE SAME LOCK the loop stores it with
	// (markLoopGoroutine). Reading the field bare here raced that write —
	// measured 2026-10-27 (task 01a1756e), `go test ./cmd/agent -race`:
	// Write by voiceChatMode.markLoopGoroutine (chat_voice.go:1156) vs Previous
	// read by voiceChatMode.shutdown (this line). The lock is already released
	// above (it guarded the state snapshot), so a one-line critical section is
	// the whole fix.
	loopIsCaller := false
	{
		m.state.mu.Lock()
		loopIsCaller = m.state.inLoop == lineedit.CurrentGoroutineID()
		m.state.mu.Unlock()
	}
	if loopDone != nil && !loopIsCaller {
		select {
		case <-loopDone:
		case <-time.After(voiceChatFinishWait):
		}
	}
	if m.recorder != nil {
		m.recorder.Close()
	}
	if ed := m.sess.VoiceEditor(); ed != nil && gate != nil {
		// The chain is RESTORED, not cleared: whatever owner was at the seam
		// before this mode entered is put back (nil when there was none, which is
		// what the editor means by "no seat").
		ed.SetKeyGate(gate.previous)
	}
	// The teardown is SILENT about the terminal but not about the ROW: a status row
	// left claiming the microphone is open after the mode ended is the lie this
	// feature exists to remove, so the repaint is published even on the exit path
	// (publishStatus writes no row of its own — it flags and wakes, see
	// chatInput.PublishStatus).
	m.publishStatus()
	return true
}

// livePhase reports the mode's current phase — the read-out the status row asks
// for through chatStatusSource's voice seam. It is guarded by the same mutex as
// `on`, and it answers IDLE whenever the mode is not on, so a caller can never
// observe a phase without the mode (see voiceChatLive's note).
func (m *voiceChatMode) livePhase() voiceChatLive {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if !m.state.on {
		return voiceChatLiveIdle
	}
	return m.state.live
}

// markLive moves the mode's phase and wakes the status row — the ONE place the
// phase changes, so every transition gets the same generation check and the same
// repaint (a transition that forgot to publish would be a row that lies until the
// next unrelated repaint: the operator would see the microphone open while the
// engine is already transcribing).
//
// THE GENERATION CHECK IS THE POINT of routing it through here rather than writing
// the field at each call site: a loop that no longer owns the mode (stale) must
// not be able to write a phase into a mode a LATER entry has already started, and
// `stale` is the existing predicate that answers exactly that (Rule 3: one state
// source, one owner predicate).
func (m *voiceChatMode) markLive(entry int, live voiceChatLive) {
	if m.stale(entry) {
		return
	}
	m.state.mu.Lock()
	if !m.state.on {
		m.state.mu.Unlock()
		return
	}
	m.state.live = live
	m.state.mu.Unlock()
	m.publishStatus()
}

// publishStatus wakes the status row's repaint through the session seam. It is
// separated from markLive so the callers that change the mode's ON state (enter,
// shutdown) can publish without also writing a phase, and it is a no-op on a
// session with no seam (the cooked path).
func (m *voiceChatMode) publishStatus() {
	if m.sess == nil {
		return
	}
	m.sess.VoicePublishStatus()
}

// active reports whether the mode is running.
func (m *voiceChatMode) active() bool {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.on
}

// currentDevice reports the selected device's description ("" while off).
func (m *voiceChatMode) currentDevice() string {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return m.state.device
}

// writeStatus reports the mode without changing it, naming the device when it is
// on so the answer is actionable.
func (m *voiceChatMode) writeStatus() {
	if !m.active() {
		m.notice("%s\n", voiceNoteStatusOff())
		return
	}
	m.notice("%s\n", voiceNoteStatusOn(voiceNoteStatusFacts{
		Device:    m.currentDevice(),
		Engine:    voiceResolveEngine(m.options),
		WindowSec: voiceChatWindowSeconds,
		LeaveWord: chatVoiceStopWord,
	}))
}

// notice writes one row of the mode's own output through the single console
// writer. It exists so every row of this feature takes the same road (chatInput's
// WriteNotice), which is what keeps the reserved layout's rows from tearing.
func (m *voiceChatMode) notice(format string, args ...any) {
	if m.sess == nil {
		return
	}
	// THE SYSTEM VOICE, not the conversation's: a `/voice` row is the surface
	// explaining itself — the engine it picked, the device it is recording, the
	// window it lost — so it is drawn dim (VoiceNoticeStyled). The WORDS are
	// unchanged; only the emphasis is decided here, once, for every row of the
	// mode (chat_voice_note.go is their one renderer, and this is their one
	// road to the console).
	m.sess.VoiceNoticeStyled(fmt.Sprintf(format, args...))
}

// markLoopGoroutine records that the CALLING goroutine is this entry's capture
// loop. It returns the RESTORE, which the caller defers, so the id is cleared
// when the loop returns.
//
// The pair is what makes "the loop is the caller" a per-call fact rather than a
// flag left standing: the id belongs to ONE incarnation of the loop, and a
// goroutine id is reused once that goroutine is gone, so an id left behind would
// eventually describe a stranger. The restore is GUARDED by the id it set, so a
// loop of an EARLIER entry cannot clear the one a later entry installed.
func (m *voiceChatMode) markLoopGoroutine() func() {
	id := lineedit.CurrentGoroutineID()
	m.state.mu.Lock()
	m.state.inLoop = id
	m.state.mu.Unlock()
	return func() {
		m.state.mu.Lock()
		if m.state.inLoop == id {
			m.state.inLoop = 0
		}
		m.state.mu.Unlock()
	}
}

// liveLoop is the long run of the LIVE arm: ONE streaming utterance after
// another until the mode is left, with each utterance ended by the operator's
// finish key.
//
// WHY IT IS A LOOP AND NOT ONE UTTERANCE. Dictation is not one sentence: an
// operator who has turned the mode on says one thing, settles it, and then says
// the next — and a mode that ended after the first sentence would make every
// sentence a fresh `/voice`. So the shape is "utterances back to back", and the
// KEY is what bounds each one.
//
// EACH UTTERANCE IS ITS OWN STREAMING SESSION, and that is a deliberate
// limitation rather than an oversight: the v2 channel is billed per audio second
// and ended by an explicit end-of-stream flag, so a session held open across
// sentences would be a session the service waits on while the operator is silent.
// The cost is the gap in this comment's last line; the alternative (one session
// for the whole mode) would have to invent a rule for when a sentence "ends"
// without the operator saying so, which is exactly the boundary the finish key
// exists to give them.
//
// THE GAP IS REAL AND IS STATED: between the settle of one sentence and the open
// of the next session, the capture is closed, so speech in that window is not
// recorded. It is one credential-free round trip (measured in the report) and the
// entered row tells the operator the mode is settling while it happens.
func (m *voiceChatMode) liveLoop(entry int, stop chan struct{}, loopDone chan struct{}) {
	// The identity is recorded FIRST and cleared LAST (the restore is the first
	// defer registered, so it is the last to run), so every exit path of the loop
	// happens while it is still marked running.
	restore := m.markLoopGoroutine()
	defer restore()
	// The channel comes from the launch site, never off the field — see loop's
	// note: shutdown clears m.state.loopDone before waiting on it.
	defer close(loopDone)
	runner, ok := m.recorder.(voiceChatLiveRunner)
	if !ok {
		// THE DISPATCH SHOULD HAVE PREVENTED THIS: enter only starts this loop for
		// a recorder that answered the seam. A recorder that stopped implementing
		// it here is a programming error, and it is REPORTED as one rather than
		// left as a mode that does nothing (Rule 15).
		m.fail(errVoiceLiveUnsupported)
		return
	}
	sink := voiceLiveSinkFor(m.sess)
	for {
		select {
		case <-stop:
			return
		default:
		}
		if m.stale(entry) {
			return
		}
		round, ok := m.beginUtterance(entry)
		if !ok {
			return
		}
		m.markLive(entry, voiceChatLiveRecording)
		// LEAVING THE MODE ENDS THE UTTERANCE IN FLIGHT, and the watcher below is
		// what makes that true PROMPTLY. The live arm blocks inside Live on a
		// capture and a session, so a mode that waited for the arm to notice `stop`
		// on its own would hold the teardown for the arm's own bound (measured:
		// shutdown's voiceChatFinishWait, 30 s) — the operator presses the exit
		// word and stands there while the microphone is still open.
		//
		// CLOSING THE FINISH SIGNAL IS THE HONEST SPELLING of "this utterance is
		// over": it is the same event as the operator's key, so the arm sends the
		// audio in hand, ends the session cleanly and returns. The transcript it
		// produces is then DISCARDED by the stale check below — the operator asked
		// to stop, not to have a sentence they abandoned written onto their line.
		untie := make(chan struct{})
		go watchUtteranceStop(stop, round, untie)
		report, err := runner.Live(m.options, sink, round.finish)
		close(untie)
		m.endUtterance()
		if m.stale(entry) {
			// The operator left while this utterance was in flight: its transcript
			// is DISCARDED (the leaving note says so) and nothing more is written.
			return
		}
		if err != nil {
			// THE SAME RULE AS THE WINDOW ARM (m.roundFailed): one failed utterance
			// is reported and the mode keeps listening; only
			// voiceChatMaxConsecutiveFailures failures IN A ROW turn it off. The
			// live arm is where the transient case is most visible — one streaming
			// session refused by the service (a timeout, a reconnect) must not
			// close the microphone of an operator mid-dictation — so both arms ask
			// the ONE predicate rather than each spelling its own threshold.
			if m.roundFailed(err) {
				return
			}
			continue
		}
		// THE SESSION ANSWERED: the run of failures is over (a silent utterance
		// included — see roundSucceeded).
		m.roundSucceeded()
		m.markLive(entry, voiceChatLiveTranscribing)
		m.recordLiveReport(report)
		// THE TRANSCRIPT GOES OUT BEFORE THE EXIT IS DECIDED, because the two facts
		// are independent: the sentence that carried audio HAS a text and the
		// operator asked to see it, whether or not a NEXT sentence is possible.
		// (Measured: printing it only after "is there another utterance?" dropped
		// the text row of the last sentence of a finite capture, while the text
		// itself had already been settled onto the line.)
		if report.Text != "" {
			m.notice("%s\n", voiceNoteTranscript(report.Text))
		}
		if report.CaptureEnded {
			// THE SOURCE IS SPENT, so there is nothing for another utterance to
			// record. Opening one anyway is the measured defect this branch exists
			// for: the next session would carry zero bytes of audio, the service
			// would refuse it (code 101), and the mode would report a FAILED round
			// beside a correct transcript that had just been settled (see
			// voiceLiveReport.CaptureEnded).
			//
			// IT IS AN ORDINARY ENDING, NOT A FAILURE: `--voice-audio <file>` is
			// how the live arm is driven end to end, and a file is finite by
			// nature. The exit says so with the CAPTURE-ENDED reason, which the
			// leaving note already renders as "the capture ended" rather than as
			// anything the operator did.
			m.leave(chatVoiceLeftByCaptureEnded)
			return
		}
		if report.Text == "" {
			m.notice("%s\n", voiceNoteSilent(voiceChatWindowSeconds))
		}
	}
}

// watchUtteranceStop closes the utterance's finish signal when the MODE is left,
// so the live arm's blocking call returns at once rather than at the arm's own
// bound (see the call site). It is guarded against closing twice — the key and the
// exit are two independent endings for one utterance, and closing a closed channel
// panics.
//
// IT RETURNS WHEN THE UTTERANCE ENDS FIRST (untie closed by the loop), which is
// what keeps one watcher per utterance rather than one per mode: a goroutine left
// behind per sentence would be the leak the suite's own sweep reports.
func watchUtteranceStop(stop <-chan struct{}, round *voiceLiveUtterance, untie <-chan struct{}) {
	select {
	case <-stop:
		round.end()
	case <-untie:
	}
}

// voiceLiveUtterance is ONE utterance of the live arm, as the mode tracks it: the
// signal the arm watches for "stop", and the single owner allowed to close it.
type voiceLiveUtterance struct {
	// finish closes when the utterance is over, from EITHER ending: the operator's
	// finish key (the gate) or the mode being left (watchUtteranceStop).
	finish chan struct{}
	// once makes those two endings one event. Without it the second ending is a
	// close of a closed channel — a panic in the teardown path, which is the worst
	// place to find one.
	once sync.Once
}

// end closes the utterance's signal, once, whoever asks.
func (u *voiceLiveUtterance) end() { u.once.Do(func() { close(u.finish) }) }

// ended reports whether the utterance has already been ended, so a second press of
// the finish key can be ANSWERED (a row saying it is already settling) instead of
// silently closing a closed channel.
func (u *voiceLiveUtterance) ended() bool {
	select {
	case <-u.finish:
		return true
	default:
		return false
	}
}

// beginUtterance publishes the utterance for ONE round and reports whether the
// loop should start it. It is the ONE writer of that field, so the gate can never
// touch an utterance belonging to a sentence that has already ended.
//
// A false means the mode was left (or a later entry took it over) between the
// check and here: the loop owns nothing and must return rather than open a
// microphone into a mode nobody is watching.
func (m *voiceChatMode) beginUtterance(entry int) (*voiceLiveUtterance, bool) {
	u := &voiceLiveUtterance{finish: make(chan struct{})}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if !m.state.on || m.state.started != entry {
		return nil, false
	}
	m.state.liveUtterance = u
	return u, true
}

// endUtterance clears the utterance in flight: from here the finish key is
// nobody's, and the gate leaves it to the editor.
func (m *voiceChatMode) endUtterance() {
	m.state.mu.Lock()
	m.state.liveUtterance = nil
	m.state.mu.Unlock()
}

// liveUtteranceInFlight reports the utterance in flight, or nil when there is
// none. It is the gate's ONE read of that state — the same predicate that decides
// whether the finish key is the mode's to take.
func (m *voiceChatMode) liveUtteranceInFlight() *voiceLiveUtterance {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if !m.state.on {
		return nil
	}
	return m.state.liveUtterance
}

// recordLiveReport keeps the utterance's numbers, so the mode can report what it
// heard and the acceptance evidence has the timings rather than an impression.
func (m *voiceChatMode) recordLiveReport(report voiceLiveReport) {
	m.state.mu.Lock()
	m.state.liveReports = append(m.state.liveReports, report)
	m.state.mu.Unlock()
}

// liveReportRows reports the live utterances this mode produced — the numbers the
// report and the operator's own `/voice status` can both quote. It is a copy, so a
// caller cannot read the slice while the loop appends to it.
func (m *voiceChatMode) liveReportRows() []voiceLiveReport {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return append([]voiceLiveReport(nil), m.state.liveReports...)
}

// loop is the long run: capture a window, transcribe it, append the text, repeat
// until the mode is left or a round fails.
//
// IT EXITS ON THREE THINGS and never by falling through: the stop channel (the
// operator left), a GENERATION mismatch (a later entry owns the mode — this loop
// belongs to a session that has already ended and must not touch its state), and
// a RUN OF FAILURES long enough to be about the mode's world rather than one
// window (voiceChatFailuresGiveUp, via m.roundFailed). That third one is
// deliberate but no longer one-strike: a device that has gone away or an engine
// that cannot run will not repair itself inside this session, so the run ends the
// mode — while a SINGLE failed window is reported and retried, because an upload
// that refused one window is not a reason to close the operator's microphone
// (task 01a121fc, deliverable D). The failure is REPORTED on every retried
// window AND on the final one, the mode is left only on the run, and the text on
// the line stays.
func (m *voiceChatMode) loop(entry int, stop chan struct{}, loopDone chan struct{}) {
	// The identity is recorded FIRST and cleared LAST (the restore is the first
	// defer registered, so it is the last to run), so every exit path of the loop
	// happens while it is still marked running — the order the join in shutdown
	// relies on.
	restore := m.markLoopGoroutine()
	defer restore()
	// THE CHANNEL COMES FROM THE LAUNCH SITE, never off the field: shutdown clears
	// the field before it waits, so a loop reading it back could find nil and close
	// nothing (see enter's loopDone note — that race cost the teardown the whole
	// voiceChatFinishWait). TestVoiceLoopClosesTheEntryJoinChannelItWasLaunchedWith
	// is the guard.
	defer close(loopDone)
	// THE CAPTURE IS FINISHED ON EVERY EXIT PATH (the operator's key, a
	// generation mismatch, a failed round): leaving must not discard the windows
	// the reader has already cut (m.finishCapture). The defer runs BEFORE the
	// loopDone close above it in this list, so the teardown waiting on loopDone
	// sees a capture that finished rather than one that was merely abandoned.
	defer m.finishCapture()
	for {
		select {
		case <-stop:
			return
		default:
		}
		if m.stale(entry) {
			return
		}
		// THE ROUND'S TEXT IS JUDGED ONCE, HERE, before the branch it feeds: a
		// window the engine answered with its silence marker and nothing else
		// carries no speech, so it must land in the SILENCE arm below (state it,
		// keep listening) rather than be appended to the operator's line as if the
		// marker were a transcript (task 01a116ad: `you> draft-/sil/sil/sil`).
		// The judgement is applied to the RECORDER INTERFACE's answer rather than
		// inside voiceChatRecorderLocal.Round, so it holds for every recorder the
		// mode may be built over — and the `text == ""` arm stays the ONE silence
		// signal it already was.
		// THE MICROPHONE IS OPEN FROM HERE, and the row says so before the recorder
		// is called: the phase is announced at the moment the mode commits to the
		// capture, not after it returns (a status that appears only when the window
		// CLOSED would tell the operator the microphone is open when it is not —
		// the exact confusion the field exists to remove). markLive refuses a
		// stale entry, so a loop that lost the mode cannot paint it.
		m.markLive(entry, voiceChatLiveRecording)
		// THE RECORDER'S OWN GATE, when it publishes one: a recorder that cuts
		// windows faster than the wall clock (a scripted one) can hold each round
		// until its reader is ready, so a case asserts the SCREEN a real cadence
		// produces instead of racing a burst no capture could emit. The exit is
		// watched HERE too, so a mode left on a parked gate still leaves at once
		// rather than waiting for a round that will never be released — and the
		// generation is re-checked after the wait, for the same reason every other
		// await in this loop re-checks it.
		if g := voiceRoundGateOf(m.recorder); g != nil {
			select {
			case <-stop:
				return
			case <-g:
			}
		}
		if m.stale(entry) {
			return
		}
		text, err := m.recorder.Round(m.options, voiceChatWindowSeconds)
		if m.stale(entry) {
			// The operator left while this round was in flight: its transcript is
			// DISCARDED (the leaving note says so) and nothing more is written —
			// no insert, no row, no state change.
			return
		}
		if err != nil {
			// A ROUND THAT ENDED BECAUSE THE STREAM WAS CLOSED UNDER IT IS NOT A
			// FAILURE. errVoiceStreamClosed is the SENTINEL for the mode's OWN
			// exit — the operator's Esc, /quit, a later generation taking the
			// mode — and the leaving note for it has already been written by
			// whoever closed the capture. Treating it as an ordinary error made
			// that exit report itself twice and wrongly: the screen read
			// `voice mode FAILED — the voice capture stream was closed. The mode
			// is OFF` and the exit was attributed to `OFF (a failed round)` on a
			// round nothing had failed (task 01a11da05). The recorder already
			// answers nil for this case (voiceChatRecorderStaged.readerFailure);
			// the loop, which is the other consumer of the same sentinel, now
			// honours it too — one sentinel, both readers (Rule 15: an exit is
			// not a failure, and a failure is never silent).
			if errors.Is(err, errVoiceStreamClosed) {
				// THE MODE MUST NOT STAY ON OVER A CAPTURE THAT ENDED. In the
				// production flow the sentinel means the operator already left
				// (their own exit closed the stream, so `stale` is true and
				// there is nothing to do — the leaving note is already on the
				// screen). When the mode is STILL the owner, the capture went
				// away under a live mode, and a status row still claiming a
				// microphone is the half-open state Rule 15 forbids: the mode
				// takes itself down and SAYS SO, for a reason that is not a
				// failed round.
				if !m.stale(entry) {
					m.leave(chatVoiceLeftByCaptureEnded)
				}
				return
			}
			// A FAILED ROUND IS RETRIED UNTIL THE RUN IS LONG ENOUGH TO BE ABOUT
			// THE MODE'S WORLD RATHER THAN ONE WINDOW (m.roundFailed): only the
			// Nth failure in a row ends the session, and the note for each of the
			// earlier ones says the mode is still listening. The wait is the
			// ordinary pacing gap, so a retry costs the same as the successful
			// round it replaces rather than spinning against a dead engine.
			if m.roundFailed(err) {
				return
			}
			if !m.pause(stop, entry) {
				return
			}
			continue
		}
		// THE ENGINE ANSWERED, so the run of failures is over — cleared before the
		// text is judged, because a SILENT window is a success for this purpose:
		// silence is the engine saying something, and a mode that counted it as a
		// failure would give up on a quiet room.
		m.roundSucceeded()
		// ONE interpretation of what the round carried (see the comment above the
		// call): the engine's marker alone is NO TEXT, so it reaches the silence
		// arm below instead of the line.
		text = chatVoiceTranscriptText(text)
		// THE BOUNDARY THE ROUND ENDED ON IS READ HERE, ONCE, for BOTH arms below:
		// a window cut inside speech is reported when it produced text (a word may
		// be split across the two windows) AND when it produced none (the same cut
		// is then a candidate explanation for the missing words — task 01a11b39).
		// They are two different sentences because they are two different
		// situations, and printing only the first is what made a whole window of
		// lost speech indistinguishable from a quiet room.
		//
		// The window is CLOSED and the engine is working: the phase moves before the
		// failure check below, because a round that fails while transcribing must
		// still have told the truth about which half it failed in — m.fail leaves
		// the mode and the row loses the field whole (publishLive on the way out),
		// so there is no state left claiming a microphone.
		m.markLive(entry, voiceChatLiveTranscribing)
		split, extended := m.lastWindowSplit()
		if text == "" {
			if split {
				m.notice("%s\n", voiceNoteSplitSilent(extended, voiceChatWindowSeconds))
			} else {
				m.notice("%s\n", voiceNoteSilent(voiceChatWindowSeconds))
			}
			if !m.pause(stop, entry) {
				return
			}
			continue
		}
		// THE TEXT IS APPENDED, at the END of the line, through the editor's one
		// insert road (which the read loop applies). A false is REPORTED: the
		// insert road refused it (no editor, or its backlog is full) and a
		// transcript that silently vanished is exactly what this feature must not
		// do.
		if !m.sess.VoiceInsertLine(text) {
			m.fail(fmt.Errorf("the transcript could not be inserted into the input line (the editor's insert road refused it)"))
			return
		}
		m.notice("%s\n", voiceNoteTranscript(text))
		// A WINDOW CUT INSIDE SPEECH IS REPORTED, and only that case: it is the
		// one boundary where a word can be split across two windows, so the
		// operator learns why a word might look broken instead of concluding the
		// recogniser is failing. Every ordinary round stays as quiet as before.
		if split {
			m.notice("%s\n", voiceNoteWindowSplit(extended, voiceChatWindowSeconds))
		}
	}
}

// lastWindowSplit reports the boundary the LAST round ended on, through the
// recorder's optional reporting seam: whether the cut was taken inside speech,
// and how many extensions it took to get there. A recorder that cannot report a
// boundary answers (false, 0) — the mode then behaves exactly as it did before
// the seam existed, which is the honest reading of "this recorder does not know".
func (m *voiceChatMode) lastWindowSplit() (bool, int) {
	rep, ok := m.recorder.(voiceWindowReporter)
	if !ok {
		return false, 0
	}
	span := rep.LastWindow()
	return span.Split, span.Extended
}

// finishCapture is the mode's LAST act before it stops recording: it asks the
// recorder to finish the capture (voiceChatFinisher.Drain) and inserts whatever
// the engine had not been asked about yet, so the audio spoken before the operator
// left lands on the line instead of being discarded with the stream.
//
// IT IS THE FIX FOR THE REPORTED LOSS (task 01a11b81): the reader cuts windows
// ahead of the engine, so at the moment of a leave there can be a window in hand; a
// teardown that closed the stream first threw that window away, and on the 67.4 s
// fixture the windows still in hand were the last thing the speaker said.
//
// A FALSE INSERT IS REPORTED (the editor refused it) and a recorder that cannot
// finish (no voiceChatFinisher) is NOT an error: a recorder that records one window
// per process has nothing in hand, and inventing a refusal for it would refuse the
// recorder that has nothing to lose.
func (m *voiceChatMode) finishCapture() {
	fin, ok := m.recorder.(voiceChatFinisher)
	if !ok {
		return
	}
	for _, text := range fin.Drain() {
		text = chatVoiceTranscriptText(text)
		if text == "" {
			continue
		}
		if !m.sess.VoiceInsertLine(text) {
			m.notice("%s\n", voiceNoteFailed(fmt.Errorf("the last windows of the capture could not be inserted into the input line (the editor's insert road refused it)")))
			return
		}
		m.notice("%s\n", voiceNoteTranscript(text))
	}
	// A FINISH THAT RAN OUT OF BUDGET IS REPORTED, never left to be inferred from
	// a shorter transcript: the operator learns that part of what they said just
	// before leaving was not transcribed.
	if rep, ok := m.recorder.(voiceChatFinishReporter); ok && rep.FinishTimedOut() {
		m.notice("%s\n", voiceNoteFinishShort(voiceChatFinishWait))
	}
}

// pause waits one pacing gap, reporting false when the mode was left (or a later
// entry took it over) while it waited.
func (m *voiceChatMode) pause(stop chan struct{}, entry int) bool {
	timer := newChatVoiceTimer(voiceChatWindowPauseSeconds)
	defer timer.Stop()
	select {
	case <-stop:
		return false
	case <-timer.C:
		return !m.stale(entry)
	}
}

// stale reports whether this loop no longer owns the mode: a later entry
// incremented the generation, or the mode was left. It is the ONE predicate
// behind "am I still the current loop", so the two arms of loop cannot disagree
// about it.
func (m *voiceChatMode) stale(entry int) bool {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return !m.state.on || m.state.started != entry
}

// fail reports a failure the mode does NOT retry and leaves the mode. It is the
// failure half of the loop's contract: the operator learns WHAT went wrong and
// that the mode is off, and the draft on the line is untouched.
//
// IT IS FOR THE FAILURES THAT AGREE WITH THEMSELVES. An error naming a state
// retrying cannot change — a recorder with no live path at all
// (errVoiceLiveUnsupported), an editor whose insert road refused the transcript —
// is not a window that hiccuped, so the run counter is not consulted: the mode
// reports it and goes OFF, because a retry would fail the same way and the
// operator would wait through the whole run to learn nothing new.
func (m *voiceChatMode) fail(err error) {
	m.notice("%s\n", voiceNoteFailed(err))
	m.leave(chatVoiceLeftByFailed)
}

// roundFailed accounts for ONE failed round and answers whether it ENDS the mode
// (task 01a121fc, deliverable D). It is the ONE place the run of consecutive
// failures is counted and the ONE place it is judged, so the window arm and the
// live arm cannot come to different rules about a failing engine.
//
// A FALSE IS NOT A DROPPED FAILURE (Rule 15): the row this method writes says
// which failure of how many it was, what the error was, that the mode is still
// listening, and how many more in a row would turn it off. The operator's screen
// therefore reads the same on a retried window as it did on a fatal one — minus
// the lie that the mode is off.
func (m *voiceChatMode) roundFailed(err error) bool {
	m.state.mu.Lock()
	m.state.failStreak++
	run := m.state.failStreak
	m.state.mu.Unlock()
	if voiceChatFailuresGiveUp(run) {
		m.fail(err)
		return true
	}
	m.notice("%s\n", voiceNoteRetry(run, voiceChatMaxConsecutiveFailures, err, voiceChatMaxConsecutiveFailures-run))
	return false
}

// roundSucceeded clears the run: a round that came back at all — WITH or WITHOUT
// text, because an empty window is silence rather than a failure — says the
// engine is answering, so the run this mode is counting is over.
//
// IT IS NOT AN EVENT ROW. Nothing is printed here: the success has its own rows
// already (the transcript, the silence note), and a "recovered" row would be the
// console narrating machinery the operator did not ask about.
func (m *voiceChatMode) roundSucceeded() {
	m.state.mu.Lock()
	m.state.failStreak = 0
	m.state.mu.Unlock()
}

// chatVoiceResolveOptions builds the engine/device settings ONE round records and
// transcribes with, by the SAME parser the bare `gopi tool voice_input` uses with
// no flags (parseVoiceArgs), followed by the same deployment search every voice
// shape performs (voiceResolveDeployment — rung 1 the gopi profile, rung 2 the
// legacy $HOME/work deployment).
//
// REUSING THE PARSER IS THE POINT, not a shortcut: it is where the model root, the
// runtime path, the capture length and the engine resolver's "nobody chose"
// default are decided, and a REPL-side hand-built voiceOptions would be a SECOND
// answer to all four — drifting the moment any default moves, which is the defect
// the profile-path search was already fixed for once.
//
// The parser cannot fail on an empty argument list (there is no flag to be
// malformed and no positional to contradict one), and if it ever could, the error
// is RETURNED and the mode refuses to start rather than recording against
// half-built settings.
func chatVoiceResolveOptions() (*voiceOptions, error) {
	opts, err := parseVoiceArgs(nil)
	if err != nil {
		return nil, err
	}
	voiceResolveDeployment(opts)
	return opts, nil
}

// newChatVoiceTimer is the ONE clock the capture loop waits on, named so the
// pacing constant (voiceChatWindowPauseSeconds, seconds) and its unit are stated
// once, where the loop and the tests can both see them.
func newChatVoiceTimer(seconds int) *time.Timer {
	return time.NewTimer(time.Duration(seconds) * time.Second)
}

// voiceChatGate is the mode's seat at the editor's key seam: it claims exactly
// ONE key — a lone Escape from the TERMINAL — and forwards everything else to the
// owner behind it.
//
// THE ORIGIN TEST IS THE WHOLE CONTRACT. A `gopi chat --replay` driver types
// recorded keys through the injector, and a recorded Escape is the RECORDING's
// key, not the watcher's: claiming it would let a scripted session switch off the
// operator's microphone, which is the "a script drove my surface" defect the
// origin seam exists to prevent (keysource.go). Only ReadOriginTyped — a
// keystroke the terminal delivered — claims it.
//
// THE CHAIN IS FORWARDED, not replaced: KeyUnclaimed and KeyRelease go to the
// previous owner through lineedit.Forwarding, so the disjointness promise (every
// key is offered exactly one of {consumed, unclaimed, released}) still holds for
// whoever was there first. Without the forwarding, a chained owner would be made
// blind by this mode's presence.
type voiceChatGate struct {
	lineedit.Forwarding
	mode *voiceChatMode
	// previous is the owner that was at the seam before this gate. It is the same
	// value as Forwarding.Next — which routes the unclaimed/released half — and it
	// is named separately because the two are read by different callers:
	// voiceChatMode.leave restores `previous`, while the forwarding calls read
	// Next. Keeping one field would make "who is behind me" a question asked of a
	// different type than the one that owns the answer.
	previous lineedit.KeyGate
}

// newVoiceChatGate builds the mode's gate.
func newVoiceChatGate(m *voiceChatMode) *voiceChatGate {
	return &voiceChatGate{mode: m}
}

// chain remembers the owner this gate was installed IN FRONT OF, so the exit can
// restore exactly it.
func (g *voiceChatGate) chain(prev lineedit.KeyGate) {
	g.Next = prev
	g.previous = prev
}

// GateKey claims two keys, and nothing else: a lone Escape, and — while an
// utterance is in flight — the finish key.
//
// THE TWO SEMANTICS ARE SEPARATE AND ARE KEPT SEPARATE, which is the whole point
// of having a second key at all (task 01a11bd4):
//
//   - Escape leaves the MODE. It is the surface's own interrupt, its meaning is
//     the operator's text, and its contract (the line is preserved) is unchanged.
//   - Ctrl+G ends the UTTERANCE. It leaves the mode running, so the operator can
//     keep dictating, and it is what makes the streaming session's end
//     operator-driven rather than inferred from silence.
//
// A FINISH KEY WITH NO UTTERANCE IN FLIGHT IS NOT THE MODE'S, and it is left to
// the editor — which has no handler for it (internal/lineedit's KeyLiveFinish has
// no arm in handleKey), so the key is inert exactly as it is outside the mode.
// That is deliberately NOT reported as "off": the key was never claimed, so there
// is nothing to say about it, and answering a keypress the operator did not aim at
// the mode with a row would be noise.
func (g *voiceChatGate) GateKey(k lineedit.Key) bool {
	if g == nil || g.mode == nil {
		return false
	}
	if k.Origin != lineedit.ReadOriginTyped {
		return false
	}
	if voiceLiveFinishKeyClaims(k) {
		// THE UTTERANCE, NOT THE MODE. The round is read under the mode's own lock
		// through liveUtteranceInFlight, so the gate can never touch a sentence that
		// has already ended — the next utterance installs its own.
		round := g.mode.liveUtteranceInFlight()
		if round == nil {
			return false
		}
		// THE ENDING IS idempotent (voiceLiveUtterance.end), which is what makes a
		// SECOND press safe: it is the ordinary thing an operator does when the
		// first one is slow, and it is ANSWERED with a row rather than dropped.
		if round.ended() {
			g.mode.notice("%s\n", voiceNoteLiveFinishing())
			return true
		}
		round.end()
		// THE PRESS IS ANSWERED, once, at the moment the key is TAKEN. The round's
		// numbers are printed by the CLI's one-shot arm and not by this mode, so
		// without this row a working press and a dropped keystroke are the same
		// picture on the terminal until the transcript lands.
		g.mode.notice("%s\n", voiceNoteLiveFinishSent(voiceLiveFinishKeyLabel))
		return true
	}
	if k.Code != lineedit.KeyEscape {
		return false
	}
	// The mode's state is read BEFORE the exit so the note can say whether this
	// key did anything: an Escape at a mode that is already off is LEFT to the
	// editor (the completion menu's close key, the interrupt) rather than
	// consumed and answered with "off" — the key was not the mode's to take.
	if !g.mode.active() {
		return false
	}
	g.mode.leave(chatVoiceLeftByEscape)
	return true
}
