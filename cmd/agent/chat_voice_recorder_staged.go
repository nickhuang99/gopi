package main

// chat_voice_recorder_staged.go — the `/voice` mode's recorder when it runs on ONE
// continuous capture (task 01a1188c).
//
// THE MODE'S CONTRACT IS UNCHANGED. voiceChatRecorder is still two calls — Select
// once, Round per window — and the loop still owns the judgement of what a round
// carried, the silence arm, the note rows and the line. What changes is under
// `Round`: instead of spawning a recorder for the window it is about to record
// and stopping it before transcribing, it takes the NEXT WINDOW OF A STREAM THAT
// WAS ALREADY RUNNING while the previous round's transcript was produced. The
// microphone is therefore never unread between rounds, which is the defect the
// parent task measured and left open.
//
// WHAT IT COSTS. One process, opened at entry and closed on the way out — the
// same arecord/parecord argv the one-shot path spawns, held for the length of the
// mode instead of one window. A mode that is left always closes it (voiceChatMode
// calls Close through the same seam that stops the loop), so the microphone is
// released on every exit path, including the ones that write no row.
//
// WHY IT IS A SECOND IMPLEMENTATION OF THE INTERFACE RATHER THAN A FLAG ON THE
// LOCAL ONE. The local recorder's whole shape is "one window, one process, cut at
// the bound" — its Round is that sentence. Keeping the staged shape beside it
// means the `/sil`-style single-window semantics (and every test that pins them)
// still have an implementation that does exactly what they assert, and the staged
// one is what the interactive mode is built over.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// voiceStagedLookaheadWindows is how many windows the reader may cut out of the
// stream AHEAD of the engine that is transcribing the previous one.
//
// IT IS THE CONSTANT THIS FILE'S SECOND HALF EXISTS FOR (task 01a11b81). A round
// used to be strictly serial — read a window, transcribe it, read the next — so
// the capture kept running while the engine worked and NOTHING consumed it: the
// audio piled up behind the engine at (window + engine) seconds of wall per
// window's worth of audio. Measured on the 67.4 s fixture with the local engine
// (2.4–4.2 s per window): the mode ran at ~0.6x real time, so by the time the
// speaker stopped a quarter of the recording was still queued, and leaving the
// mode — the operator's own Esc, the event that ends every real session — closed
// the stream and DISCARDED it. That is the "tail disappears" report.
//
// ONE is enough to make the loop keep up: the engine (~2.9 s) is faster than the
// audio it is asked about (a window closes on a pause and averages ~4 s), so a
// single window of look-ahead absorbs the engine time inside the recording time
// and the queue behind the engine stays EMPTY (measured: see the report's rig
// table). A deeper queue would only buffer more audio for the exit to throw away.
const voiceStagedLookaheadWindows = 1

// voiceStagedWindow is ONE window the reader has cut out of the stream and handed
// to the rounds: the audio to transcribe and the ledger span saying where in the
// capture it came from. The span travels WITH the audio so the ledger is written
// in the order the windows were read, whatever order the engine answers in.
type voiceStagedWindow struct {
	pcm  []byte
	span voiceWindowSpan
}

// voiceChatRecorderStaged is the PRODUCTION recorder for window mode: ONE capture
// stream, windows cut out of it, the tail of every window carried into the next —
// and, since task 01a11b81, a reader goroutine that keeps CUTTING windows while
// the engine transcribes the previous one.
type voiceChatRecorderStaged struct {
	// stream is the live capture. It is nil until Select opens it.
	stream *voiceStagedStream
	// device is the description Select produced, kept so every round records from
	// the device that was resolved and the operator was told about.
	device string
	// dev is the resolved capture device itself, kept beside the description
	// because the LIVE arm (chat_voice_live.go) opens its own capture per
	// utterance and must open it on the SAME device the operator was told about —
	// re-running the four-tier selection would be a second answer to a question
	// already answered, and on a machine where the tiers move it could be a
	// different device entirely.
	dev *voiceDevice
	// notes are Select's notes, held so the caller's row can print them once.
	notes []string
	// schedule is the ONE window reader over this capture (voice_window_schedule.go):
	// it owns the window bound, the carry, the plan and the SPAN LEDGER, and both
	// read shapes go through it — this recorder's rounds and the tool's one-block
	// read. The ledger used to live here, beside a second reader of the same stream
	// in the same package (Rule 14: one schedule, however many shapes read it).
	schedule *voiceStagedWindows
	// openedOptions is the settings the stream was opened under — recorded so a
	// later caller can ask which engine/device this capture belongs to without
	// re-resolving (and possibly re-answering) the question.
	openedOptions *voiceOptions
	// opts is the settings each round transcribes with, kept so the finish (Drain)
	// — which runs on the exit path, after the loop has stopped — asks the engine
	// the same way the rounds did.
	opts *voiceOptions

	// windows is the reader's hand-off to the rounds: the windows it has cut out
	// of the stream that no engine has been asked about yet. It is buffered to
	// voiceStagedLookaheadWindows and CLOSED by the reader when the stream ends.
	windows chan voiceStagedWindow
	// done is closed by Close, and it is what releases a reader parked on a full
	// hand-off (the operator left; nobody will take another window).
	done chan struct{}
	// readerOnce starts the reader exactly once PER ENTRY: the FIRST Round asks
	// for the window bound, and the reader is launched with that same bound, so
	// the two cannot disagree about how long a window is. It is RESET by
	// beginEntry — see the block comment on the guards below.
	readerOnce sync.Once
	// stopOnce, drainOnce and closeOnce keep the three teardown calls idempotent:
	// the mode reaches them from more than one exit path (the stop word, Escape,
	// the session's own teardown), and a second call must be a no-op rather than a
	// second close of the same channel.
	//
	// THEY ARE PER-ENTRY, NOT PER-OBJECT, and that distinction is the whole fix of
	// the `/voice` re-entry leak (measured 2026-10-26). This recorder object is
	// built ONCE per chat session (cli_chat.go: newChatVoiceMode(input,
	// chatVoiceRecorder(...))) while `/voice` is a TOGGLE that can be entered any
	// number of times. An object-scoped guard is therefore spent by the FIRST
	// leave: entry 2's Select opens a NEW stream and assigns it to r.stream, and
	// entry 2's Close finds closeOnce already fired — so that stream is never
	// closed, its recorder process is never signalled, and nobody ever reads the
	// error its cmd.Wait() would have produced. The child exits on its own and the
	// process table keeps it. Measured on a live chat (pid 331978, 18h, binary
	// revision b9be76f whose four recorder files are byte-identical to this tree):
	//
	//	$ pgrep -x parecord | wc -l
	//	14
	//	$ ps -o pid,ppid,stat,cmd -p $(pgrep -x parecord | paste -sd,)
	//	431808 331978 Z [parecord] <defunct>     … one per `/voice` recording …
	//	2212490 331978 Z [parecord] <defunct>
	//
	// The 2026-10-22 fix (task 01a163df) made ONE teardown correct — the forced
	// Kill+Wait inside stopVoiceRecorder. It did not make the teardown REACHABLE a
	// second time, which is this block's job: every guard that protects an
	// ENTRY-scoped resource is reset at the moment the entry opens, so the entry
	// and its teardown always agree about how many times they may fire.
	stopOnce  sync.Once
	drainOnce sync.Once
	closeOnce sync.Once
	// entryOnce serializes beginEntry, so two concurrent Selects (a re-entry
	// racing a teardown) cannot interleave a reset with an open. It is the
	// per-object lock the reset needs and nothing else uses the object's state
	// under it (see beginEntry for why the reset is done before the spawn rather
	// than after).
	entryOnce sync.Mutex
	// entry counts the entries this object has opened, so a report can name the
	// entry a leftover belonged to (Rule 15: a process we could not retire is
	// named, never silently dropped).
	entry int
	// readerDone closes when the window-cutting goroutine has returned, so the
	// teardown that stops the reader can WAIT for it to have handed back the window
	// it was holding — the difference between "this capture was closed under a
	// reader mid-window" and a reader racing the process it reads from.
	readerDone chan struct{}
	// draining reports that the rounds are taking what is still in hand to finish
	// the capture (Drain), which is what turns "the operator left" from "discard
	// whatever the engine has not been asked about" into "ask it about all of it".
	draining atomic.Bool
	// drainTimedOut reports that the finish budget ran out with audio still in
	// hand: a REPORTED fact (the mode says so), never a silent truncation.
	drainTimedOut atomic.Bool
	// last is the span of the window the last Round transcribed — held here
	// because the stream's own field belongs to the window the READER cut last,
	// which with look-ahead is not the one just transcribed.
	last voiceWindowSpan
	// bound is the window bound the reader was started with.
	bound int
	// errMu guards readErr.
	errMu sync.Mutex
	// readErr is why the reader stopped, when it did not stop because the mode
	// left. It is reported by the Round that finds the hand-off closed.
	readErr error
	// engine is the transcription seam: nil means the production call
	// (transcribeFile). It exists so the regression case can pin the SCHEDULE —
	// that the engine for one window runs while the next window is being read —
	// without a 910 MB model, which is the behaviour this file is about.
	engine func(*voiceOptions, string) (string, time.Duration, error)

	// audioSource is the FILE this mode reads its audio from, empty for the
	// production capture. It is the chat surface's channel onto the audio-source
	// seam (chat_voice_audio.go) and nothing more: it holds a PATH, never a
	// decoded buffer, because the reading, the geometry check and the pacing are
	// voiceFileSource's and would be a second implementation here (Rule 14/35).
	//
	// IT MUST REACH THE ENGINE TOO, and that is why it is not simply handed to
	// openVoiceStagedStream and forgotten: a file capture carries no device, and
	// the engine dispatch answers a FILE arm and a DEVICE arm differently
	// (cli_voice_local.go's transcribeFile). A `/voice` run over a file whose
	// options still named no source would take the device arm — the one path that
	// reaches for a capture device this run deliberately never opened — so
	// transcribeOptions below puts the path on every round's options.
	audioSource string
}

// transcribeOptions is the ONE place the recorder's audio source is put onto the
// options a round transcribes with, so the window arm and the live arm cannot
// answer "where did the audio come from" differently.
//
// A RUN WITH NO FILE RETURNS THE CALLER'S OWN POINTER, unchanged: the production
// path must not start carrying a copy of its settings for a feature it is not
// using, and a copy would be a second value that could be edited by one round and
// read by the next.
func (r *voiceChatRecorderStaged) transcribeOptions(opts *voiceOptions) *voiceOptions {
	if r == nil || strings.TrimSpace(r.audioSource) == "" || opts == nil {
		return opts
	}
	derived := *opts
	derived.audioSource = r.audioSource
	return &derived
}

// errVoiceStagedBoundChanged is the refusal for a Round that asks for a different
// window bound than the reader was started with: the reader cuts windows out of
// the live stream, so a bound it does not know about is not a preference it can
// honour — it is a caller that changed its mind mid-capture, and the honest
// answer is to say so (Rule 15) rather than to silently keep the old bound.
var errVoiceStagedBoundChanged = fmt.Errorf("the window bound cannot change while a capture is open")

// beginEntry opens ONE `/voice` entry on this recorder: it releases anything the
// previous entry left behind and resets every ENTRY-SCOPED field, so the entry's
// teardown fires for THIS entry rather than for the object's first one.
//
// WHY A RESET AND NOT A NEW OBJECT (the measured reason). The recorder is built
// once per chat session (cli_chat.go), and the mode is a TOGGLE. Making the
// session build a fresh recorder per entry would be the other way to keep the
// guards honest, and it was rejected because the recorder is not the only thing
// that would have to be rebuilt: the mode, the status row's voice seam
// (status.setVoiceFn(voiceMode.livePhase)) and the key gate all bind to that one
// object at session start, so a per-entry object is a per-entry WIRING problem
// spread across three surfaces. Resetting the entry-scoped half keeps ONE owner
// and puts the invariant where it can be read: every resource this object opens
// belongs to an entry, so every guard protecting one is armed by that entry.
//
// THE GUARDS ARE RESET, THE OBJECT-LEVEL STATE IS NOT: `audioSource` and `engine`
// are configuration the session set once (they are how the run was started), and
// they survive an entry by design.
//
// IT IS CALLED BEFORE THE SPAWN, never after: a Select that fails to open a
// capture must still leave the object in a state whose NEXT entry works, and a
// reset placed after the spawn would leave the failed entry's spent guards in
// place for the next one — the same defect one level down.
func (r *voiceChatRecorderStaged) beginEntry() {
	r.entryOnce.Lock()
	// THE PREVIOUS ENTRY'S CAPTURE IS RELEASED FIRST, and through the same steps
	// its own teardown used, so an entry interrupted before its leave (a failed
	// round, a session exit racing an enter) cannot leave a recorder process owned
	// by nobody. A nil stream here is the first entry, and the helpers' own guards
	// make this a no-op for it.
	readerDone, stale := r.retireCaptureLocked()
	r.entryOnce.Unlock()
	// THE OLD CAPTURE IS GONE BEFORE ANY FIELD IS ZEROED: the reader goroutine
	// still READS this object's schedule/window state while it runs, so resetting
	// first and joining after would be a data race against the very goroutine this
	// step is releasing. Closing the stream signals the recorder; the close of
	// `done` (inside retireCaptureLocked) releases a parked reader; only then is
	// the state this object shares with it safe to overwrite.
	voiceStagedJoinReader(readerDone)
	if stale != nil {
		stale.Close()
	}

	r.entryOnce.Lock()
	r.entry++
	// THE ENTRY-SCOPED HALF: the capture, its reader and the three teardown
	// guards, all back to their zero values. A sync.Once has no Reset method, and
	// assigning a zero value is exactly what "this guard has not fired YET, for
	// THIS entry" means.
	r.dev = nil
	r.device = ""
	r.notes = nil
	r.schedule = nil
	r.windows = nil
	r.done = nil
	r.readerDone = nil
	r.bound = 0
	r.readerOnce = sync.Once{}
	r.stopOnce = sync.Once{}
	r.drainOnce = sync.Once{}
	r.closeOnce = sync.Once{}
	r.errMu.Lock()
	r.readErr = nil
	r.errMu.Unlock()
	r.draining.Store(false)
	r.drainTimedOut.Store(false)
	r.last = voiceWindowSpan{}
	// THE ROUND SETTINGS ARE PER-ENTRY TOO: they are recorded by the entry's own
	// rounds and read by its own finish (Drain), so carrying them across an entry
	// would let entry 2's finish transcribe with entry 1's engine configuration —
	// a stale value the operator never chose for the capture being finished.
	r.openedOptions = nil
	r.opts = nil
	r.entryOnce.Unlock()
}

// Entry reports which entry this object has opened, so a report (or a refusal)
// can name the capture a leftover process belonged to.
func (r *voiceChatRecorderStaged) Entry() int {
	r.entryOnce.Lock()
	defer r.entryOnce.Unlock()
	return r.entry
}

// Select resolves the device and OPENS THE STREAM: from here until Close, the
// microphone is being read continuously, so the round boundary has nothing left
// to lose.
func (r *voiceChatRecorderStaged) Select(device string) (string, []string, error) {
	// A RE-ENTRY IS AN ORDINARY EVENT, NOT A MISTAKE: `/voice` is a toggle, so
	// this is the second, third, … time the operator turned it on. The entry's
	// guards are armed HERE, before anything is spawned, or this entry's teardown
	// would find them spent (see beginEntry and the guard block above).
	r.beginEntry()
	// THE SOURCE IS ONE DECISION, TAKEN HERE. A run that was given an audio FILE
	// reads it and never asks the host what it has: device enumeration and the
	// liveness probe are how a MICROPHONE is chosen, and a file run chooses none
	// (this is what makes the mode usable on a machine with no sound card at all).
	// The decision is not made a second time anywhere: both capture points below
	// go through voiceAudioSourceFor, which is the seam's own chooser.
	if path := strings.TrimSpace(r.audioSource); path != "" {
		return r.selectFileSource(path)
	}
	devices, notes, err := voiceListDevices(voiceRunCommand)
	if err != nil {
		return "", notes, err
	}
	sel, selNotes, err := voiceResolveSelection(device, devices, voiceRunCommand, voiceCaptureAttempt)
	notes = append(notes, selNotes...)
	if err != nil {
		return "", notes, err
	}
	// THE SAME SEAM, ONE LEVEL UP. The options this stream is opened under carry
	// this run's audio source (empty here — the device branch was taken because
	// there is none), so "which source" is answered by ONE function for both the
	// window arm and the live arm instead of by a nil that means "the production
	// capture" at each call site. The pacing is that function's decision, not one
	// taken here (voiceSourcePacing).
	stream, err := openVoiceStagedStream(r.captureOptions(), &sel.Device)
	if err != nil {
		return "", notes, err
	}
	r.stream = stream
	r.dev = &sel.Device
	r.device = voiceDescribeCapture(sel, voiceChatWindowSeconds)
	r.notes = notes
	return r.device, notes, nil
}

// selectFileSource enters the mode over a FILE: no device is enumerated, nothing
// is probed for liveness, and the stream is built from the same seam the tool
// surface's `--audio-source` uses.
//
// THE REFUSALS ARE THE SEAM'S OWN, deliberately. A path that is missing, a
// directory, an empty file and a WAV whose geometry is not the capture contract
// are all decided inside voiceFileSource.Open (via voiceValidateAudioSource at
// parse time and voiceSourceWAVPCM at read time), so the chat surface has no
// second opinion to offer and none is written here: the operator gets the one
// sentence the tool surface gets, from the one place that knows the contract.
func (r *voiceChatRecorderStaged) selectFileSource(path string) (string, []string, error) {
	dev, description := chatVoiceFileDevice(path)
	// THE FILE GOES THROUGH THE SEAM'S OWN CHOOSER (voiceAudioSourceFor), so the
	// console's file run and the tool's file run cannot take different source
	// kinds — and so the pacing is the chooser's decision rather than a second one
	// taken at this call site.
	src, err := voiceAudioSourceFor(r.captureOptions(), &dev)
	if err != nil {
		return "", nil, err
	}
	stream, err := newVoiceStagedStream(src)
	if err != nil {
		return "", nil, err
	}
	r.stream = stream
	r.dev = &dev
	r.device = description
	r.notes = nil
	return r.device, nil, nil
}

// captureOptions is the ONE construction of the settings a capture is opened
// under from this recorder's own state. It exists so neither branch can pass a
// nil that means something different: the empty path here IS the production
// capture, and voiceAudioSourceFor is the function that reads it.
func (r *voiceChatRecorderStaged) captureOptions() *voiceOptions {
	return &voiceOptions{audioSource: r.audioSource}
}

// Round takes ONE window out of the running stream and transcribes it.
//
// THE READER IS ALREADY CUTTING THE NEXT WINDOW while this call's engine runs,
// which is what keeps a long recording from outrunning the console: the audio the
// engine is asked about is cut BEFORE the engine is asked, so the engine's own
// time is spent on the NEXT window's recording instead of on nobody reading the
// microphone (voiceStagedLookaheadWindows).
//
// The audio up to the cut goes to the engine; the audio after it stays in the
// stream for the next round. Nothing is dropped, which is the property the
// ledger's tiling check proves for the run as a whole.
func (r *voiceChatRecorderStaged) Round(opts *voiceOptions, seconds int) (string, error) {
	if r.stream == nil {
		return "", errVoiceStagedNotOpen
	}
	// RECORDED BEFORE THE ENGINE, not deferred: the finish (Drain) runs on the exit
	// path, after the loop has stopped, and it transcribes through the SAME settings
	// the rounds used — a nil here is a panic in the engine's own dispatch
	// (voiceResolveEngine), which is exactly the failure a defer would have hidden
	// until the operator's last window.
	r.recordOptions(opts)
	if err := r.startReader(seconds); err != nil {
		return "", err
	}
	win, ok := <-r.windows
	if !ok {
		// THE READER STOPPED: either the mode left (Close closed the stream under
		// it) or the device failed. Which of the two is a FACT, not a guess —
		// readerFailure answers it, and the mode's own exit path is reported as the
		// sentinel the loop exits quietly on.
		if err := r.readerFailure(); err != nil {
			return "", err
		}
		return "", errVoiceStreamClosed
	}
	return r.transcribe(win)
}

// transcribe runs ONE window through the engine and marks it covered. It
// is the ONE place a window becomes text, so a round and the finish (Drain) cannot
// disagree about what "transcribing a window" means.
func (r *voiceChatRecorderStaged) transcribe(win voiceStagedWindow) (string, error) {
	// THE LEDGER IS WRITTEN WHEN THE ENGINE HAS THE AUDIO, not when the reader cut
	// it: the reader runs ahead of the rounds on purpose, and a window cut but never
	// transcribed has not been covered (voiceStagedWindows.Mark).
	r.schedule.Mark(win.span)
	r.last = win.span
	path, cleanup, err := voiceStageCaptureAudio(voicePCM16ToWAV(win.pcm))
	if err != nil {
		return "", err
	}
	text, _, terr := r.engineFor()(r.transcribeOptions(r.opts), path)
	cleanup()
	if terr != nil {
		return "", terr
	}
	return strings.TrimSpace(text), nil
}

// startReader launches the window-cutting goroutine ONCE, with the bound of the
// first Round that asks for one.
//
// IT IS STARTED AT THE FIRST ROUND RATHER THAN AT Select, and the reason is the
// same one that makes the bound a parameter at all: a window's length is the
// caller's decision (the mode passes voiceChatWindowSeconds, a case passes its
// own), and a reader that guessed it would cut windows the caller never asked
// for. The first Round is the first moment the bound is known.
func (r *voiceChatRecorderStaged) startReader(seconds int) error {
	r.readerOnce.Do(func() {
		r.bound = seconds
		// THE SCHEDULE IS BUILT HERE, with the bound the first round asked for:
		// it is the ONE reader of the stream from this moment on, and every window
		// the console transcribes comes out of it (Rule 14 — the bound, the plan,
		// the carry and the span ledger in one place, shared with the tool's own
		// read shape).
		r.schedule = newVoiceStagedWindows(r.stream, seconds)
		r.windows = make(chan voiceStagedWindow, voiceStagedLookaheadWindows)
		r.done = make(chan struct{})
		r.readerDone = make(chan struct{})
		go r.readWindows()
	})
	if seconds != r.bound {
		return errVoiceStagedBoundChanged
	}
	return nil
}

// readWindows is the window-cutting goroutine: it takes windows out of the live
// stream for as long as the stream produces them, through the ONE schedule
// (voice_window_schedule.go), so the window ledger is written in stream order
// wherever the windows were asked for — the console's rounds and the tool's
// one-block read alike.
//
// IT BLOCKS WHEN THE ROUNDS FALL BEHIND — deliberately. The hand-off holds
// voiceStagedLookaheadWindows windows and no more: a round that has stopped asking
// (a wedged engine) must stop the reader rather than let it consume the whole
// capture into memory, and a live preview shows the backlog as "windows exist but
// no transcript has arrived", which is the true state.
//
// IT CUTS NOTHING NEW ONCE THE CAPTURE IS BEING FINISHED (draining): the window it
// is in the middle of closes at its own bound and is handed over, and then the
// hand-off is CLOSED — which is how the rounds learn that there is no more audio
// and stop asking (Round's EOF). Cutting a further window then would be asking the
// operator to wait for audio they did not speak.
func (r *voiceChatRecorderStaged) readWindows() {
	defer close(r.readerDone)
	defer close(r.windows)
	for {
		// THE WINDOW SCHEDULE IS THE ONE READER OF THE STREAM (voice_window_schedule.go):
		// the bound, the span ledger and the carry live there, and the tool's
		// one-block shape reads the SAME schedule, so the console and the tool
		// cut the same windows out of the same audio.
		win, err := r.schedule.Next()
		if err != nil {
			r.recordReaderErr(err)
			return
		}
		select {
		case r.windows <- win:
		case <-r.done:
			return
		}
		// THE CAPTURE IS BEING FINISHED: this window was the last one worth
		// cutting, and the hand-off is closed so the rounds stop asking. Cutting
		// another would ask the operator to wait for audio they did not speak.
		if r.draining.Load() {
			return
		}
	}
}

// recordReaderErr remembers why the reader stopped, unless it stopped because the
// mode left (Close closed the stream under it).
func (r *voiceChatRecorderStaged) recordReaderErr(err error) {
	if err == nil {
		return
	}
	r.errMu.Lock()
	if r.readErr == nil {
		r.readErr = err
	}
	r.errMu.Unlock()
}

// readerFailure reports the reader's error, or nil when the reader stopped because
// the stream was closed (the mode's own exit, which the loop exits on quietly).
func (r *voiceChatRecorderStaged) readerFailure() error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	if r.readErr == nil {
		return nil
	}
	if errors.Is(r.readErr, errVoiceStreamClosed) {
		return nil
	}
	return r.readErr
}

// engineFor resolves the transcription call: the seam when a case installed one,
// the production file transcription otherwise. One reader of the field, so the
// regression case and the production path cannot take different routes.
func (r *voiceChatRecorderStaged) engineFor() func(*voiceOptions, string) (string, time.Duration, error) {
	if r.engine != nil {
		return r.engine
	}
	return transcribeFile
}

// recordOptions remembers the settings the first round was transcribed with, for
// the same reason the local recorder resolves them per round: the answer to "which
// device, which engine" belongs to the run, not to a caller's guess.
func (r *voiceChatRecorderStaged) recordOptions(opts *voiceOptions) {
	if r.openedOptions == nil {
		r.openedOptions = opts
	}
	r.opts = opts
}

// Drain finishes the capture: it stops the reader from cutting NEW windows, then
// takes the windows that were already read and hands them to the engine, in stream
// order, returning their transcripts.
//
// WHY IT EXISTS (measured, task 01a11b81). The mode keeps the microphone open for
// its whole life and the reader cuts windows ahead of the engine, so at any moment
// there can be a window in hand that no engine has been asked about. Leaving the
// mode used to close the stream and DISCARD it — and the operator's own Esc, the
// event that ends every real session, is exactly when that happens. On the 67.4 s
// fixture the two windows still in hand at the Esc were the last thing the speaker
// said.
//
// IS IS CALLED BY THE LOOP, NOT BY THE MODE'S TEARDOWN, and that placement is what
// makes it race-free: the loop is the only consumer of the hand-off, so the finish
// cannot compete with a round for the same window (a drain sharing the hand-off
// with a live round would transcribe the same audio twice).
//
// THE FINISH IS BOUNDED (voiceStagedDrainWait) and its exhaustion is REPORTED
// (drainTimedOut), never hidden: the stream is live, so a reader inside a window
// whose audio has not paused yet takes until that window's bound to hand it over,
// and the honest statement of a budget that ran out is the ledger's coverage line
// in the report rather than a silent truncation.
func (r *voiceChatRecorderStaged) Drain() []string {
	if r == nil || r.windows == nil || r.opts == nil {
		// NO ROUND EVER COMPLETED: there are no settings to transcribe with and
		// nothing was ever asked of the engine, so there is nothing to finish. A nil
		// here is NOT a silent skip — a Round that left these unset would have
		// already failed (errVoiceStagedNotOpen), and the loop reaching the finish
		// with none is the case "the operator turned it on and left immediately".
		return nil
	}
	r.drainOnce.Do(func() { r.draining.Store(true) })
	var out []string
	deadline := time.After(voiceStagedDrainWait)
	for {
		select {
		case win, ok := <-r.windows:
			if !ok {
				return out
			}
			if text, err := r.transcribe(win); err == nil && text != "" {
				out = append(out, text)
			}
		case <-deadline:
			r.drainTimedOut.Store(true)
			return out
		}
	}
}

// FinishTimedOut reports that the finish budget ran out with windows still in
// hand — the recorder's own account of a partial finish, read by the mode's report.
func (r *voiceChatRecorderStaged) FinishTimedOut() bool { return r.drainTimedOut.Load() }

// retireCapture is the ONE teardown of the entry's capture, and it is a LOCKED
// region (entryOnce) because TWO different goroutines reach it: the entry's own
// teardown (Close) and the NEXT entry's opening (beginEntry), which happens
// whenever a `/voice` toggle lands while the previous entry is still winding
// down. Without the lock those two interleave — the opening resets
// `stopOnce`/`done`, the teardown's late `stopOnce.Do` then closes a `done`
// belonging to the NEW entry (or a guard the new entry had already armed is
// wiped), and the new entry's teardown is spent before it starts. That is the
// same class of defect as the object-scoped guards this change exists to fix,
// one level down.
//
// THE WAIT IS NOT HELD UNDER THE LOCK: the reader only needs `done` closed,
// which happens before the release; a reader parked on a full hand-off is
// released by that close and the join below runs outside the critical section.
func (r *voiceChatRecorderStaged) retireCapture() {
	r.entryOnce.Lock()
	readerDone, stream := r.retireCaptureLocked()
	r.entryOnce.Unlock()
	voiceStagedJoinReader(readerDone)
	if stream != nil {
		stream.Close()
	}
}

// retireCaptureLocked is retireCapture's body for a caller that ALREADY holds
// entryOnce (beginEntry), and it returns the two things that must be released
// outside the critical section: the reader's join channel and the capture.
func (r *voiceChatRecorderStaged) retireCaptureLocked() (chan struct{}, *voiceStagedStream) {
	readerDone := r.stopReaderLocked()
	stream := r.stream
	r.stream = nil
	return readerDone, stream
}

// voiceStagedJoinReader waits for the window-cutting goroutine to return, within
// the drain budget. A nil channel is "no reader was started", which is the first
// entry and the failed-Select case.
func voiceStagedJoinReader(readerDone chan struct{}) {
	if readerDone == nil {
		return
	}
	select {
	case <-readerDone:
	case <-time.After(voiceStagedDrainWait):
	}
}

// stopReaderLocked closes the reader's release channel and hands back the join
// channel. The caller MUST hold entryOnce (see retireCapture for why).
func (r *voiceChatRecorderStaged) stopReaderLocked() chan struct{} {
	if r.done == nil {
		return nil
	}
	r.stopOnce.Do(func() { close(r.done) })
	return r.readerDone
}

// stopReader releases the window-cutting goroutine and waits for it to return: the
// rounds take no window that has not been handed over, so a reader parked on a full
// hand-off comes back, and the caller knows the reader is done rather than racing
// the process it reads from.
func (r *voiceChatRecorderStaged) stopReader() {
	r.entryOnce.Lock()
	readerDone := r.stopReaderLocked()
	r.entryOnce.Unlock()
	voiceStagedJoinReader(readerDone)
}

// Close releases the stream. It is IDEMPOTENT and safe on a recorder whose Select
// failed, so the mode's teardown can call it unconditionally.
//
// IT RELEASES THE READER FIRST (the hand-off is closed, so a reader parked on a
// full queue returns) and then the capture, the same order voiceStagedStream.Close
// uses for its own reader: the microphone is released on every exit path, and a
// reader that outlived the process it reads would hold the device open.
//
// THE READER IS STOPPED WITHOUT CLOSING THE STREAM, and the order is load-bearing:
// the stream's own Close signals the recorder process, and a signal is what makes
// the reader's pipe end — so stopping the reader first lets it hand back the audio
// the device had already produced, while closing the stream first would discard
// exactly the audio the operator just spoke. Drain runs before this on the mode's
// exit path (voiceChatMode.releaseCapture).
func (r *voiceChatRecorderStaged) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		r.retireCapture()
	})
}

// Ledger exposes the run's windows for a caller that wants to check the boundary
// (the acceptance measurement and the report). It is a read of this run's own
// records; nothing else reads it.
// Ledger is the schedule's span ledger: every window this capture handed over,
// whichever shape asked for it.
func (r *voiceChatRecorderStaged) Ledger() *voiceWindowLedger {
	if r.schedule == nil {
		return &voiceWindowLedger{}
	}
	return r.schedule.Ledger()
}

// LastWindow reports the boundary the last Round ended on, for the mode's
// reporting of a cut taken inside speech (voiceWindowReporter).
func (r *voiceChatRecorderStaged) LastWindow() voiceWindowSpan { return r.last }

// errVoiceStagedNotOpen is the refusal for a Round on a recorder whose Select did
// not run (or already closed): stated, never silently an empty transcript.
var errVoiceStagedNotOpen = errVoiceStreamClosed
