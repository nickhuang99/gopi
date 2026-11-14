package main

// cli_voice_recorder_staged.go — the STAGED recorder: ONE capture that outlives
// the transcription of the round before it (task 01a1188c).
//
// THE DEFECT THIS EXISTS FOR, measured, not inferred (task 01a11853, peerbox
// 2026-10-08): `/voice` records a window per round, stops the recorder, and only
// then transcribes. The next round's recorder is opened AFTER the engine answers,
// so a slice of the microphone's audio — the whole inter-round gap, ~1.6 s of it
// — belongs to no recording at all. A 12.455 s continuous utterance lost
// 「测试，我声音完全流逝，」 in exactly that gap: the window cut landed mid-speech
// and the speech on the other side of the cut was never read.
//
// WHAT A ROUND BECOMES. The recorder is opened ONCE, when the mode is entered,
// and a reader goroutine consumes its stdout for as long as the mode is on:
//
//	reader goroutine ──chunks──▶ window builder ──▶ engine ──▶ transcript
//	   (continuous)                (per round)      (per round)
//
// A round now means "the next window OF THIS SAME stream": the reader never stops,
// so the samples captured while the engine is working are not lost — they are
// simply the beginning of the next window. That is the whole fix: the boundary no
// longer has a hole in it.
//
// WHERE A WINDOW ENDS. Two conditions, whichever comes first:
//
//   - the window's BOUND (voiceChatWindowSeconds from its first sample), unchanged
//     from task 01a11853 — a round can never hold the stream forever; and
//   - a RUN of trailing near-silence (voiceCaptureTrailingSilenceMS) after the
//     window has carried speech: the operator stopped, so the round ends there.
//
// THE RUN IS CARRIED, NOT DISCARDED. The samples of the trailing run become the
// next window's head, so the boundary loses nothing at all: consecutive windows
// tile the stream exactly, and the audio the operator spoke is in one window or
// the other, never between them.
//
// ZERO NEW DEPENDENCIES (Rule 35): the recorder is the same arecord/parecord argv
// the one-shot path already spawns, the reader is this file, and nothing is
// written to disk, scheduled, or left resident after the mode is left (Close).

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// voiceStagedDrainWait bounds how long Drain waits for the reader goroutine to
// hand back the last window before it stops waiting. It is a teardown bound, never
// a recording window.
const voiceStagedDrainWait = 8 * time.Second

const (
	// voiceStagedQueueSeconds is how much audio the reader may run AHEAD of the
	// window builder. It has to cover a whole round's engine time with room to
	// spare (the local engine answers a 6 s window in ~2.8 s, the cloud arm in
	// seconds), and it is what makes the reader's independence real: while the
	// engine works, the reader keeps consuming the device and parks the samples
	// here, so the recorder's own pipe never fills and no sample is dropped.
	voiceStagedQueueSeconds = 30

	// voiceStagedQueueChunks is that budget in the reader's unit, derived so the
	// queue and the constant above cannot drift (Rule 13).
	voiceStagedQueueChunks = voiceStagedQueueSeconds * voiceCaptureSampleRate *
		voiceCaptureBytesPerSample / voiceCaptureReadChunk

	// voiceStagedDrainGrace bounds how long Close waits for the reader to hand
	// back the last chunk before it stops waiting. It is a teardown bound, never
	// a recording window: past it the recorder is signalled and the mode's exit
	// is not held up by a device that stopped delivering (Rule 15 — the wait is
	// bounded and the outcome is reported by the process's own exit).
	voiceStagedDrainGrace = 2 * time.Second
)

// errVoiceStreamClosed is what Next reports when the stream is closed under it.
// It is a SENTINEL, not a failure: leaving the mode closes the stream, and the
// loop must exit quietly when that is why its round ended (the exit note has
// already been written by whoever closed it).
var errVoiceStreamClosed = errors.New("the voice capture stream was closed")

// voiceStagedStream is ONE live capture: the AUDIO SOURCE it reads from and the
// window builder that turns the byte stream into rounds.
//
// THE SOURCE IS THE SEAM (cli_voice_audio_source.go): a recorder process in
// production, a PCM/WAV file when a run names one. Everything below the seam —
// the window plan, the carry, the ledger span — is identical for both, which is
// what makes "a file capture produces the same windows as a live one" a property
// a test can assert instead of a claim.
type voiceStagedStream struct {
	// source is where the audio comes from. The stream owns its lifecycle: Open
	// happens in newVoiceStagedStream, Close in Close below.
	source voiceAudioSource

	// dev and binary are the RECORDER's own description, kept for the fields a
	// scripted test source fills in and for the messages that name a device. A
	// file source leaves dev zero, and every refusal prints sourceLabel() so a
	// file run's failure does not send the operator looking for a microphone.
	dev    voiceDevice
	binary string
	// sourceOrigin is the source's own one-phrase identity ("arecord from
	// "pulse:@DEFAULT_SOURCE@"", "the file /tmp/a.pcm"), taken once from the
	// source at construction. It is what makes a refusal name the thing that
	// actually failed — the whole difference between "the mic produced nothing"
	// and "the file you named produced nothing".
	sourceOrigin string

	// chunks is the source's reader→builder hand-off. In production the SOURCE is
	// the only writer (the stream consumes the audio), and the channel is taken
	// from it at construction; a scripted test stream supplies its own, which is
	// why the field stays bidirectional rather than being narrowed to a receive
	// direction. It is CLOSED by whoever feeds it when the stream ends, which is
	// the builder's only end-of-stream signal.
	chunks chan []byte

	// done is closed by Close, and it is what releases a reader parked on a full
	// queue. It is a channel rather than a timer because the reader allocates
	// nothing per chunk: one channel, read once per wait, closed once.
	done chan struct{}

	// mu guards closed.
	mu sync.Mutex
	// closed is set by Close BEFORE the source is released, so Next can tell
	// "the mode left" apart from "the source failed".
	closed bool

	// carry is the head of the next window: the samples a closed window handed
	// back (the trailing run that ended it), or the residue of a window whose
	// bound elapsed mid-chunk. Nothing the source delivered is ever dropped.
	carry []byte
	// received is how many bytes this stream has handed to windows so far: it is
	// what makes every window's span an ABSOLUTE range of the capture, which is
	// what the ledger's tiling check compares.
	received int
	// span is the last window's ledger span — the boundary this stream just took.
	span voiceWindowSpan
}

// voiceLiveRecorderCommand renders the argv for a LIVE stream: the same recorder
// and the same pinned capture contract as voiceRecorderCommand, with every
// DURATION removed — a window is closed by this file's boundary decisions, and a
// recorder that also stopped on its own clock would put a second, unowned end on
// the same stream (the process exiting mid-window is indistinguishable from a
// device that died). arecord's -d and ffmpeg's -t are the flags that carry a
// duration, so their removal is stated here rather than left to the caller.
func voiceLiveRecorderCommand(dev voiceDevice) (string, []string) {
	binary, args := voiceRecorderCommand(dev, voiceClientDefaultSeconds)
	live := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case voiceArecordDurationFlag:
			// The flag AND its value: a duration flag left without its argument
			// would make the recorder read the next flag as a length.
			i++
			continue
		}
		live = append(live, args[i])
	}
	return binary, live
}

// Chunks hands back the source's own hand-off channel: the raw PCM of the live
// capture, in arrival order, closed when the source ends.
//
// IT IS THE SECOND CONSUMER OF ONE CAPTURE, and it exists because the LIVE
// dictation arm (chat_voice_live.go) needs the samples AS THEY ARRIVE rather than
// as a window: a streaming recogniser is fed a packet every 200 ms while the
// operator speaks, so there is no boundary for Next to find and asking it for one
// would put a window's worth of latency in front of every partial.
//
// The disciplines Next obeys are the same here, because it is the same channel:
// the source is the only writer, Close is the only teardown, and a closed channel
// means the source ended (the caller tells the two apart through failure()).
func (s *voiceStagedStream) Chunks() <-chan []byte { return s.chunks }

// failure reports why the source stopped, when it was NOT the mode leaving.
//
// ONE ANSWER, TWO FIELDS. The verdict is the SOURCE's own account of how it
// ended (voiceAudioSource.Health) when the stream has one, and the stream's own
// `closed` flag decides whether that account is a failure at all: a source that
// ended because Close released it ended QUIETLY, whatever it read on the way out
// (a closed pipe reports an I/O error, and reporting that as a device failure is
// precisely the false alarm this flag exists to prevent).
//
// A scripted test stream has no source and carries a first-sample stamp instead;
// its liveness is its own business, and it is never handed an unread device.
func (s *voiceStagedStream) failure() error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errVoiceStreamClosed
	}
	if s.source == nil {
		// A STREAM WITH NO SOURCE HAS NO SECOND OPINION TO CONSULT: its audio is
		// fed by whoever built it (the boundary cases' scripted device), so there
		// is nothing to ask about how the feed ended and no failure to report.
		// Answering the sentinel here would END such a stream at its own
		// end-of-input — the tail window would never be handed over, and the
		// schedule case that these scripted streams exist for measures the tail.
		return nil
	}
	return s.source.Health()
}

// Close stops the stream: the process is signalled, the reader is released and
// the recorder is reaped. IT IS IDEMPOTENT and it is the ONLY teardown path, so a
// mode that is left with a round in flight cannot leave the microphone held by a
// process nobody owns (Rule 15).
func (s *voiceStagedStream) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.done != nil {
		close(s.done)
	}

	// The reader is unblocked FIRST (it may be parked on a full queue), then the
	// SOURCE is released — and the source is the only thing that owns a process,
	// a pipe or a file, so "release the capture" is one call whatever the audio
	// came from (Rule 14: one teardown, not one per shape).
	//
	// A STREAM WITH NOTHING TO STOP IS CLOSED BY THE SAME PATH. A scripted test
	// stream has no source at all: the state above — `closed` and `done` — is the
	// whole of what "closed" means for it, and the release below is what a real
	// source adds on top.
	if s.source != nil {
		s.source.Close()
	}
}

// Next builds ONE window out of the live stream and returns its audio together
// with the ledger span saying which part of the stream it was.
//
// It blocks until the window is finished: the bound elapsed, the operator's own
// trailing silence closed it, the stream ended, or — on audio that never pauses —
// the extension budget was spent and the cut was taken inside speech (the span
// reports Split).
//
// THE TAIL AFTER THE CUT IS CARRIED, NEVER DROPPED: both the trailing run that
// closed a window and any bytes that arrived past the bound belong to the NEXT
// window, so consecutive rounds tile the stream exactly. That is what the
// ledger's verify() checks, and it is the whole point of this file: with one
// continuous reader, "the boundary" is a bookkeeping decision instead of a
// microphone nobody holds.
func (s *voiceStagedStream) Next(seconds int) ([]byte, voiceWindowSpan, error) {
	plan := newVoiceWindowPlan(seconds)
	pcm := make([]byte, 0, plan.boundBytes)
	if len(s.carry) > 0 {
		pcm = append(pcm, s.carry...)
		s.carry = nil
	}
	// THE WINDOW BEGINS WHERE THE LAST ONE STOPPED — s.received, not
	// s.received-minus-the-carry. The carry is audio that has arrived but has not
	// been consumed by any round yet, so it sits AT s.received in the stream; the
	// previous version subtracted it and reported a window starting 8470 bytes
	// before its own audio, which the ledger's tiling check caught as an overlap
	// (measured: "windows 1 and 2 do not meet").
	span := voiceWindowSpan{Start: s.received}

	// THE OPEN DEADLINE: the same budget the one-shot path gives a device to
	// produce its FIRST sample, spent only while the stream has produced none —
	// a stream that has been running is never made to wait for it.
	var openDeadline <-chan time.Time
	if s.source == nil || voiceSourceFirstSample(s.source).IsZero() {
		openDeadline = time.After(time.Duration(voiceCaptureGraceSec) * time.Second)
	}
	// THE BOUND CLOCK is ARMED HERE and re-armed by every extension, so a round
	// can hold the stream for (1+voiceWindowMaxExtensions) bounds and no longer.
	bound := time.After(time.Duration(seconds) * time.Second)

	for {
		select {
		case chunk, ok := <-s.chunks:
			if !ok {
				return s.flush(pcm, span)
			}
			// THE FIRST SAMPLE ARRIVES: the open deadline is DISARMED. A timer
			// that has been armed keeps its appointment whether or not the
			// condition it was armed for still holds, so leaving it armed would
			// fail a stream that is demonstrably producing audio (measured: the
			// rig's first run reported "no audio within 10s" on a stream that had
			// been delivering for 8 of them).
			openDeadline = nil
			pcm = append(pcm, chunk...)
			if cut, done := plan.cut(pcm, false); done {
				audio, done := s.finish(pcm, span, cut)
				return audio, done, nil
			}
		case <-bound:
			if cut, done := plan.cut(pcm, false); done {
				audio, done := s.finish(pcm, span, cut)
				return audio, done, nil
			}
			// NO BOUNDARY IN REACH: the window would have to be cut inside a
			// word. It is extended by one more bound's worth of audio instead —
			// BOUNDED, so audio that never pauses still produces rounds
			// (voiceWindowMaxExtensions), and the eventual cut is REPORTED.
			span.Extended++
			if span.Extended <= voiceWindowMaxExtensions {
				bound = time.After(time.Duration(seconds) * time.Second)
				continue
			}
			cut, _ := plan.cut(pcm, true)
			audio, done := s.finish(pcm, span, cut)
			return audio, done, nil
		case <-openDeadline:
			return nil, span, s.noFirstSample()
		}
	}
}

// finish closes a window at the cut the plan found: this round gets the audio up
// to it, the NEXT window gets the rest — nothing in between is dropped.
//
// IT RETURNS BOTH ITS RESULTS, and that is deliberate: the span is what the caller
// (and the ledger) reports, and a version that stamped only the stream's own copy
// would hand back a span of zeros for a window that carried audio — the exact
// shape of "the report says nothing happened" this task exists to avoid.
func (s *voiceStagedStream) finish(pcm []byte, span voiceWindowSpan, cut voiceWindowCut) ([]byte, voiceWindowSpan) {
	audio := pcm[:cut.End]
	s.carry = append(s.carry[:0], pcm[cut.End:]...)
	s.received += len(audio)
	span.End = span.Start + len(audio)
	span.Run = cut.Run
	span.Split = cut.Split
	s.span = span
	return audio, span
}

// flush ends a round because the STREAM ended: whatever the buffer holds is this
// round's audio, and the verdict on it (a device that never delivered, or a quiet
// exit) is decided from what arrived.
func (s *voiceStagedStream) flush(pcm []byte, span voiceWindowSpan) ([]byte, voiceWindowSpan, error) {
	if err := s.failure(); err != nil {
		return nil, span, err
	}
	if len(pcm) < voiceMinCaptureBytes {
		// A REAL SOURCE THAT ENDED CLEANLY, HAVING ALREADY DELIVERED AUDIO, HAS
		// SIMPLY RUN OUT — and saying "produced no audio" for that is a false
		// statement about a capture that filled every window before it.
		//
		// THE CASE IT EXISTS FOR is the FILE source (cli_voice_audio_source.go): a
		// recording is FINITE, so the window cut after its last sample finds an
		// empty buffer and a source whose Health() is nil. Before this arm that
		// became the loud "reachable but silent" refusal, which the voice mode
		// treats as a FAILED ROUND and leaves on — measured on the first file run
		// of task 01a11f13's second child, where the mode exited "FAILED — the file
		// … produced no audio" one window after a window it had transcribed. The
		// stream's own end is the honest verdict, and the mode's exit path already
		// knows it (errVoiceStreamClosed is the sentinel both of its readers honour
		// — chat_voice.go's loop and voiceChatRecorderStaged.readerFailure).
		//
		// THE REFUSAL STAYS for what it was written for: a source that produced
		// NOTHING (received == 0) is still a loud, named failure, and a scripted
		// test stream (no source at all) keeps its behaviour byte-for-byte — its
		// audio is fed by whoever built it, so it has no second opinion to consult
		// and none is invented here.
		if s.source != nil && s.received > 0 {
			return nil, span, errVoiceStreamClosed
		}
		// A SOURCE THAT ENDED IS THE ONE PLACE A CHILD'S OWN DIAGNOSTIC BELONGS
		// IN THIS REFUSAL (see voiceAudioSource.Diagnostic): a recorder that
		// exited non-zero without ever producing a sample did so for a REASON,
		// and "reachable but silent" is the verdict card I want it printed on.
		return nil, span, fmt.Errorf("%s", s.sourceSilenceNote(fmt.Sprintf(
			"%s produced no audio — the source is reachable but silent: "+
				"check the capture level is not muted", s.sourceLabel())))
	}
	span.End = span.Start + len(pcm)
	s.span = span
	s.received += len(pcm)
	return pcm, span, nil
}

// noFirstSample is the verdict for a stream that produced no sample within its
// budget: a device condition, in the words the one-shot path uses.
func (s *voiceStagedStream) noFirstSample() error {
	if err := s.failure(); err != nil {
		return err
	}
	return fmt.Errorf("%s", s.sourceSilenceNote(fmt.Sprintf(
		"%s produced no audio within %ds — the source is reachable but silent: "+
			"check the capture level is not muted", s.sourceLabel(), voiceCaptureGraceSec)))
}

// sourceSilenceNote attaches the SOURCE's own account of why it ended, when it
// has one worth printing, to a refusal that would otherwise report a silent
// source (voiceAudioSource.Diagnostic).
//
// WHY IT EXISTS. The staged capture's refusals are rendered from what the WINDOW
// BUILDER observed — "no samples arrived" — which is the right verdict but, for a
// recorder that REFUSED TO START, an answer that stops one step short of the
// cause: the child's exit status and its stderr name the device, and both were
// being read and then thrown away (Rule 15). A source with nothing to add
// answers with the empty string, which is every source but the recorder.
func (s *voiceStagedStream) sourceSilenceNote(msg string) string {
	if s.source == nil {
		return msg
	}
	if note := s.source.Diagnostic(); note != "" {
		return msg + " — " + note
	}
	return msg
}

// sourceLabel names the source in one phrase, for the refusals that report a
// silent capture. A scripted test stream carries no source and answers with the
// recorder phrasing its own fields spell, so the production sentence is
// byte-for-byte what it was before the seam existed.
func (s *voiceStagedStream) sourceLabel() string {
	if s.sourceOrigin != "" {
		return s.sourceOrigin
	}
	return fmt.Sprintf("%s from %q", s.binary, s.dev.ID)
}
