package main

// voice_window_schedule.go — ONE window schedule over ONE capture, and the two
// READ SHAPES the two local-engine surfaces take from it (task 01a121fc,
// deliverable A).
//
// THE TWO DEVICES THIS REPLACES. Under the local engine the console's `/voice`
// and the tool's `voice_input` read the same microphone-and-file seam, and until
// this file each one carried its own reader:
//
//	chat /voice   voiceStagedStream.Next over a WALL-CLOCK bound: a window plan, a
//	              carry that makes consecutive windows tile the stream, a ledger of
//	              spans, and a Drain that transcribes what is still in hand when
//	              the operator leaves.
//	tool          voiceCapturePCM: drain the source until `--seconds` of audio is
//	              in hand, trim, return one buffer. No plan, no carry, no ledger,
//	              and nothing of the kind — a second reading of the same stream,
//	              with its own idea of where audio ends.
//
// The audio was never wrong (both read the same bytes), but the two answers to
// "where does this run's audio end" were two implementations, and the tool's had
// no ledger to check. Now there is ONE schedule (this type) and two shapes read it:
//
//	Next()          one window — the console's shape, as often as the loop asks
//	ReadBounded(n)  n BOUNDS' worth of audio read as consecutive windows and
//	                stitched, trimmed to the caller's byte count — the tool's shape
//
// ReadBounded is not "call the engine on a block": it is the SAME Next, repeated,
// with the same plan, the same carry and the same ledger; what differs is only
// WHEN the caller stops asking. So a file fed to the tool and the same file fed to
// the console's window arm produce the SAME span sequence and the SAME per-window
// bytes when they are given the same bound — the property the regression case
// asserts rather than assumes.
//
// PACING IS NOT DECIDED HERE. Whether a file source delivers at the rate the audio
// was spoken is a property of the SHAPE, and it is decided once, in
// voiceFileSourceFor (cli_voice_audio_source.go) with the reason written beside it:
// a shape whose bound is a WALL CLOCK (the console's window cut, which reacts to
// the timing of the audio's arrival) needs the capture's own rhythm, and a shape
// whose bound is a BYTE COUNT (a one-block read) has no schedule to protect.

import "errors"

// voiceSourcePacing says HOW a file source delivers its audio, and it is the ONE
// place that question is answered (Rule 13/14): the flag used to be a bare bool
// passed at four call sites, which is a decision nobody can find and a reader
// cannot audit.
//
// THE TWO SHAPES NEED DIFFERENT PACING, and the reason is mechanical rather than a
// preference:
//
//   - a shape whose bound is a WALL CLOCK (the console's window cut: Next arms a
//     timer and reacts to whether the audio arrived with a pause in it) needs the
//     capture's own RHYTHM. Handed the whole file in one burst, every window
//     boundary is decided by a clock that has already elapsed by the time the
//     bytes are in hand — the run keeps the audio and loses the SHAPE, which is
//     the thing the window cut is a function of.
//   - a shape whose bound is a BYTE COUNT (the tool's one-block read: read
//     `--seconds` of audio, hand it up) has no schedule to protect, and pacing it
//     would make a 30-second file cost 30 seconds for no property at all.
type voiceSourcePacing bool

const (
	// voiceSourcePaced is the rate the audio was spoken: one chunk per chunk of
	// wall clock (voiceAudioSourceChunkMS). See the type's own note for which
	// shapes must use it.
	voiceSourcePaced voiceSourcePacing = true
	// voiceSourceImmediate is "as fast as the reader takes it". See the type's own
	// note for which shapes may use it.
	voiceSourceImmediate voiceSourcePacing = false
)

// voiceFileSourceFor builds a file source with an explicitly named pacing, so a
// call site states which of the two shapes it is instead of passing a bool whose
// meaning is only in the reader's head.
func voiceFileSourceFor(path string, pacing voiceSourcePacing) *voiceFileSource {
	return newVoiceFileSource(path, bool(pacing))
}

// voiceStagedWindows is one capture's window schedule: the bound every window is
// cut against, the spans the windows covered, and the ledger that proves they tile
// the stream.
//
// IT WRAPS the stream rather than replacing it: the stream owns the source, the
// carry and the end-of-stream verdict (voiceStagedStream, cli_voice_recorder_staged.go),
// and this type owns the SCHEDULE over it — the bound, the span bookkeeping and the
// two read shapes. That split is what lets a scripted test stream keep being built
// exactly as before while both production readers go through one schedule.
type voiceStagedWindows struct {
	// stream is the capture, already open.
	stream *voiceStagedStream
	// bound is the window bound in SECONDS: the same number voiceStagedStream.Next
	// has always taken, and the one the console resolves from voiceChatWindowSeconds
	// and the tool from --seconds.
	bound int
	// ledger records every span this schedule handed over, in stream order, so
	// "the windows tiled the capture" is a property the run can check rather than a
	// claim about the boundary code (voiceWindowLedger.verify).
	ledger voiceWindowLedger
	// spans is the same sequence, kept because a caller that wants to compare two
	// runs' boundaries wants the values rather than a set to query (the regression
	// case, and the report).
	spans []voiceWindowSpan
}

// newVoiceStagedWindows builds the schedule for one open stream and one bound.
func newVoiceStagedWindows(stream *voiceStagedStream, bound int) *voiceStagedWindows {
	return &voiceStagedWindows{stream: stream, bound: bound}
}

// Next hands over ONE window: the stream's own cut, filed in this schedule's
// SEQUENCE. It is the console's whole read shape, and the tool's is below.
//
// A CUT IS NOT A CONSUMPTION, and the two are kept apart on purpose: a reader may
// cut ahead of the engine (the console's look-ahead, which is what stops a long
// window's engine call from making the capture drift behind real time), so the
// sequence of cuts is longer than the sequence of windows an engine was asked
// about. The LEDGER holds the second — Mark is the caller saying "the engine has
// this audio now" — because the ledger's question is "was every byte of the
// capture handed to the engine", and a window nobody transcribed has not answered
// it (the finish's Drain is what answers it for the tail).
func (w *voiceStagedWindows) Next() (voiceStagedWindow, error) {
	pcm, span, err := w.stream.Next(w.bound)
	if err != nil {
		return voiceStagedWindow{}, err
	}
	w.spans = append(w.spans, span)
	return voiceStagedWindow{pcm: pcm, span: span}, nil
}

// Mark records that one window's audio has been CONSUMED — handed to the engine,
// or (for a shape whose output is one block) kept for the caller. It is the only
// writer of the ledger, so "what the rounds covered" is one list in one place
// however many shapes read the schedule.
func (w *voiceStagedWindows) Mark(span voiceWindowSpan) { w.ledger.add(span) }

// Ledger is the tiling check's input: every span this schedule handed over.
func (w *voiceStagedWindows) Ledger() *voiceWindowLedger { return &w.ledger }

// Spans is the window sequence itself, in stream order.
func (w *voiceStagedWindows) Spans() []voiceWindowSpan { return w.spans }

// ReadBounded reads a ONE-BLOCK capture out of the same schedule: consecutive
// windows until the caller's byte bound is reached, then the head of exactly that
// many bytes.
//
// IT IS THE TOOL'S SHAPE (voiceCapturePCM), and it is deliberately built on Next:
// the audio it returns is the head of the same window sequence the console would
// have read from the same source and bound, so "the tool and the console agree
// about where audio ends" is a property of the code rather than a coincidence.
//
// A NON-POSITIVE BOUND MEANS THE WHOLE SOURCE (the flag's own contract): every
// window until the stream ends, which is how the tool gets the console's END
// behaviour too — the tail is handed over by the stream's flush rather than being
// cut off by a byte count, and the ledger covers the capture completely.
//
// THE TRIM IS STATED, NOT HIDDEN. A window boundary rarely lands exactly on the
// byte count (a cut is where the audio paused, not where the operator's flag ran
// out), so the LAST window's head is the remainder and the rest of it is audio the
// caller never asked for — it is dropped HERE, where the caller's bound is known,
// rather than being handed up as extra audio the flag did not request.
func (w *voiceStagedWindows) ReadBounded(want int) ([]byte, error) {
	pcm := make([]byte, 0, want)
	for want <= 0 || len(pcm) < want {
		win, err := w.Next()
		if err != nil {
			// THE END OF THE SOURCE IS NOT A FAILURE HERE, and that is a decision
			// rather than a convenience: a recording is FINITE, and the tool's
			// `--seconds 30` against a 6 s file is a run that captured 6 s. A
			// stream that produced AUDIO and then ended has answered the question;
			// one that produced NOTHING is a named refusal (voiceCapturePCM), which
			// is where the distinction belongs.
			if isVoiceStreamEnd(err) && len(pcm) > 0 {
				return pcm, nil
			}
			return pcm, err
		}
		pcm = append(pcm, win.pcm...)
		// EVERY WINDOW THIS SHAPE CUTS IS CONSUMED BY IT — it is kept for the
		// caller — so the tool's audio gets the SAME tiling check the console's
		// rounds have, which is a property this shape never had before.
		w.Mark(win.span)
	}
	return trimVoiceBytes(pcm, want), nil
}

// isVoiceStreamEnd reports whether err is the stream's own end rather than a
// failure: the sentinel the mode's exit path uses, or the "source ended cleanly"
// verdict the flush produces for a finite source (see voiceStagedStream.flush).
func isVoiceStreamEnd(err error) bool {
	return errors.Is(err, errVoiceStreamClosed)
}

// trimVoiceBytes returns the head of pcm up to want bytes, and pcm itself for a
// non-positive want.
func trimVoiceBytes(pcm []byte, want int) []byte {
	if want <= 0 || len(pcm) <= want {
		return pcm
	}
	return pcm[:want]
}
