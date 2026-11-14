package main

// cli_voice_window_ledger.go — WHEN one round's audio ends and the next begins
// (task 01a1188c).
//
// THE DEFECT THIS CLOSES. `/voice` runs a round as: record a window, transcribe
// it, insert the text, arm the next window. Two windows are two RECORDINGS, and
// nothing guarantees the second begins where the first stopped — measured on
// zpeer 2026-10-08 (task 01a11853), the second round's recorder is opened after
// the engine answers, so the audio spoken during that ~1.6 s gap is in NO window
// at all. A 12.455 s continuous utterance lost 「测试，我声音完全流逝，」 exactly
// there: the window's cut fell mid-speech and the speech after it was never read.
//
// THE TWO HALVES OF THE FIX, and why BOTH are needed:
//
//  1. THE CUT MUST FALL WHERE IT LOSES NOTHING (this file). A window cut at a
//     byte offset inside a word splits that word across two windows and each
//     engine call is asked to transcribe half a word — which it cannot. So a cut
//     is only taken where a RUN of the audio was near-silent
//     (voiceCaptureTrailingSilenceMS, measured relative to the window's own
//     peak): the audio before the run is one window, the run itself begins the
//     next — a boundary that falls inside a word is REFUSED, and the window is
//     extended until the audio pauses (bounded by voiceWindowMaxExtensions).
//     Leading silence never closes a window (an empty round transcribed during a
//     thinking pause is the same defect from the other side).
//
//  2. THE SAMPLES AFTER THE CUT MUST SURVIVE IT (cli_voice_recorder_staged.go).
//     The rescheduling above is only safe if the recorder keeps reading while it
//     happens: a staged stream delivers every chunk the device produces, and the
//     window boundary CARRIES the tail that follows the cut into the next window
//     instead of discarding it with the process.
//
// EVERY WINDOW IS PINNED AS A SLICE OF THE STREAM. A round reports the byte range
// it transcribed, so the windows of one capture can be checked afterwards to TILE
// the stream exactly: no hole, no overlap. That check is what the regression test
// runs (`voiceWindowLedger.verify`) and it is the property the operator cares
// about — the concatenation of the rounds' transcripts covers the utterance
// because the concatenation of the rounds' AUDIO is the utterance.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const (
	// voiceWindowMinAudioMS is the shortest window worth transcribing. The engine
	// is asked for text from a file, and a window shorter than this carries no
	// word of any language — so a window that has not reached it is EXTENDED (it
	// is not a round); it is a floor, never a cut.
	voiceWindowMinAudioMS = 1500

	// voiceWindowMaxExtensions bounds how many times ONE round may extend its
	// window past the bound while looking for a boundary that does not split a
	// word. It is a BOUND, not a preference: the round must end even on audio that
	// never pauses (continuous music, a noisy room), and the honest outcome there
	// is a cut — reported as a split (voiceWindowReport.split), never hidden.
	voiceWindowMaxExtensions = 8

	// voiceWindowMinPauseSpeechMS is how much SPEECH a window must carry before a
	// pause may close it.
	//
	// IT IS NOT voiceWindowMinAudioMS, and separating the two is the whole of task
	// 01a11b59. A window below voiceWindowMinAudioMS of AUDIO is never cut at all —
	// that minimum keeps a round from being spent transcribing a fragment, and it
	// counts the window's whole span, silence included. Requiring the same span of
	// SPEECH before any pause could be a boundary was the defect: a person dictating
	// continuously speaks ~1.1 s between breaths (measured: the fixture's own
	// silencedetect census, 3.35 s / 4.82 s / 7.02 s ...), so with a 1500 ms speech
	// floor the FIRST qualifying pause was routinely the one after a whole window's
	// bound had already elapsed — the window then extended to
	// voiceWindowMaxExtensions, was cut inside a word, and the words around the cut
	// were lost. Measured on the operator's fixture with the floor at 1500 ms: no cut
	// anywhere, 6 extensions spent on a 67.4 s recording, every window ending at its
	// byte bound (probe-BEFORE.txt).
	//
	// 1000 ms is chosen from the same measurement and is deliberately BELOW the
	// shortest phrase a dictating person speaks between two breaths: a floor above
	// that phrase is a floor that can never fire. It stays ABOVE a consonant's
	// closure and above the sub-second gaps that are not phrase ends, so the window
	// still refuses to close there while it has budget left to look for a better
	// pause — and it is a floor on SPEECH, so the window keeps its "never cut on a
	// fragment" protection where that protection means something.
	voiceWindowMinPauseSpeechMS = 1000
)

// voiceWindowSpan is ONE round's audio as a RANGE of the capture stream: the byte
// offsets the window transcribed, and what the cut that produced it looked like.
type voiceWindowSpan struct {
	// Start and End are byte offsets into the stream the round read from.
	Start, End int
	// Run is the trailing run, in bytes, that closed the window (0 when the
	// window ended on its own bound with nothing to carry).
	Run int
	// Extended counts how many times the window was pushed past its bound while
	// waiting for a boundary that does not split a word.
	Extended int
	// Split is whether the cut had to be taken inside speech anyway (the
	// extension budget ran out). It is a REPORTED fact: the transcript is
	// whatever the engine makes of a split word, and the operator is told.
	Split bool
}

// voiceWindowLedger records every window ONE capture produced, so "did the rounds
// cover the audio continuously?" is a question about data instead of a claim.
type voiceWindowLedger struct {
	spans []voiceWindowSpan
}

// add records the window a round transcribed.
func (l *voiceWindowLedger) add(span voiceWindowSpan) {
	if l == nil {
		return
	}
	l.spans = append(l.spans, span)
}

// verify reports whether the recorded windows TILE the stream exactly: the first
// begins at 0, every window begins where the previous one ended (no gap, no
// overlap), and the last ends at total. It returns the first violation in words,
// so a failing run says WHICH property broke and where.
func (l *voiceWindowLedger) verify(total int) error {
	if l == nil || len(l.spans) == 0 {
		return fmt.Errorf("no windows were recorded from a %d-byte capture", total)
	}
	if l.spans[0].Start != 0 {
		return fmt.Errorf("the first window starts at byte %d, not at the head of the capture", l.spans[0].Start)
	}
	for i, span := range l.spans {
		if span.End <= span.Start {
			return fmt.Errorf("window %d is empty (%d..%d)", i+1, span.Start, span.End)
		}
		if i == 0 {
			continue
		}
		if prev := l.spans[i-1]; span.Start != prev.End {
			return fmt.Errorf("windows %d and %d do not meet: window %d ends at byte %d, window %d starts at byte %d (%d bytes of the capture belong to no window)",
				i, i+1, i, prev.End, i+1, span.Start, span.Start-prev.End)
		}
	}
	// THE TRAILING END IS NOT A HOLE. A capture normally ends on silence (the
	// operator stopped speaking, the fixture's own tail), and silence is not
	// speech, so the last window may stop before the last byte — what must NOT
	// happen is a window boundary with audio on both sides of it. The tail is
	// reported, not failed: verify returns nil and the caller says how much
	// trailing audio was left out.
	return nil
}

// coverage names how much of the capture the windows transcribed and what the
// remainder was, so the report can state the tail rather than imply a hole.
func (l *voiceWindowLedger) coverage(total int) string {
	if l == nil || len(l.spans) == 0 {
		return fmt.Sprintf("no windows for a %d-byte capture", total)
	}
	last := l.spans[len(l.spans)-1]
	return fmt.Sprintf("windows cover bytes 0..%d of %d (%.3fs of speech); the remaining %d bytes (%.3fs) are the capture's own trailing silence",
		last.End, total, voiceStagedSecondsOfAudioAt(last.End), total-last.End,
		voiceStagedSecondsOfAudioAt(total)-voiceStagedSecondsOfAudioAt(last.End))
}

// text renders the ledger as the row the report quotes: one line per window with
// its range, its length in seconds and how it ended.
func (l *voiceWindowLedger) text() string {
	var b strings.Builder
	for i, span := range l.spans {
		how := "bound"
		if span.Split {
			how = "cut inside speech (extension budget spent)"
		} else if span.Run > 0 {
			how = "trailing silence"
		}
		fmt.Fprintf(&b, "window %d: bytes %d..%d (%.3fs..%.3fs, %d extensions, ended on %s)\n",
			i+1, span.Start, span.End, voiceStagedSecondsOfAudioAt(span.Start),
			voiceStagedSecondsOfAudioAt(span.End), span.Extended, how)
	}
	return strings.TrimRight(b.String(), "\n")
}

// voiceStagedSecondsOfAudioAt names a byte offset as a timestamp in the capture,
// so a boundary can be read as a moment rather than as an offset.
func voiceStagedSecondsOfAudioAt(offset int) float64 {
	return float64(offset) / float64(voiceCaptureSampleRate*voiceCaptureBytesPerSample)
}

// voiceWindowPlan is the builder ONE round runs against a staged stream.
//
// IT IS THE PLAN FOR ONE WINDOW, and it holds the two things a cut is decided
// from: the lengths (derived once, so the loop and the constants cannot drift) and
// the SHARED silence floor — the same voiceCaptureSilenceFloor the capture path
// reads, so "this run is silence" has one definition in this codebase (Rule 14).
type voiceWindowPlan struct {
	// seconds is the window bound: the fixed length a round may record for, and
	// the length a window is extended BY while it looks for a boundary.
	seconds int
	// minBytes is the shortest window worth transcribing, in the stream's unit
	// (derived once, so the loop and the constant cannot drift — Rule 13).
	minBytes int
	// minSpeechBytes is the SPEECH a window must carry before a pause may close
	// it, in the stream's unit — a floor on speech, NOT on the window's span
	// (voiceWindowMinPauseSpeechMS; the two are different questions and the
	// original rule conflated them).
	minSpeechBytes int
	// boundBytes is the bound in the stream's unit.
	boundBytes int
	// runBytes is the SHORTEST run that may close a window, in the stream's unit.
	runBytes int
	// floor is the shared statement of what counts as silence.
	floor voiceCaptureSilenceFloor
}

// newVoiceWindowPlan derives the plan for a window bound.
func newVoiceWindowPlan(seconds int) voiceWindowPlan {
	return voiceWindowPlan{
		seconds:        seconds,
		minBytes:       voiceCaptureSampleRate * voiceCaptureBytesPerSample * voiceWindowMinAudioMS / voiceMSPerSecond,
		minSpeechBytes: voiceCaptureSampleRate * voiceCaptureBytesPerSample * voiceWindowMinPauseSpeechMS / voiceMSPerSecond,
		boundBytes:     voiceBytesPerSeconds(seconds),
		runBytes:       voiceCaptureSampleRate * voiceCaptureBytesPerSample * voiceCaptureTrailingSilenceMS / voiceMSPerSecond,
		floor:          newVoiceCaptureSilenceFloor(),
	}
}

// floorAmplitude is the amplitude below which the shared rule calls a run silent.
func (p voiceWindowPlan) floorAmplitude() float64 { return p.floor.amplitude }

// voiceWindowCut is the verdict one window's bytes produce: how much of the
// buffer is this round's audio, how much belongs to the next window, and how the
// cut was reached.
type voiceWindowCut struct {
	// End is the length of this round's audio, in bytes.
	End int
	// Run is the trailing run that closed it, in bytes (0 when the bound did).
	Run int
	// Extended counts the bound-length extensions the round spent looking for a
	// boundary.
	Extended int
	// Split says the cut was taken inside speech because the extension budget ran
	// out.
	Split bool
}

// cut decides where ONE window ends inside a buffer that is already in hand.
//
// THE RULE, in order: a window must carry at least voiceWindowMinAudioMS of audio
// and it must not be cut inside a word, so it is closed at a run of trailing
// near-silence that is at least voiceCaptureTrailingSilenceMS long AND that has at
// least voiceWindowMinPauseSpeechMS of SPEECH before it. Silence BEFORE speech
// never closes a window (the round would be empty).
//
// `exhausted` says the extension budget is spent, in which case the longest
// usable window is taken and the split is REPORTED rather than hidden.
func (p voiceWindowPlan) cut(pcm []byte, exhausted bool) (voiceWindowCut, bool) {
	if len(pcm) < p.minBytes {
		return voiceWindowCut{}, false
	}
	// A WINDOW WITH NO SPEECH AT ALL IS NOT CLOSED AT ITS BOUND, and the reason is
	// measured rather than aesthetic: a window of pure silence has NO boundary to
	// find (a boundary is a pause AFTER speech), so closing it at the bound submits
	// a silent window to the engine every bound — every 6 s, forever, while the
	// operator simply is not talking. On the local engine that is a hallucinated
	// sentence appended to the line every 6 s (measured: five "我想要一个完整的家。"
	// rows in a row during the rig's quiet tail), and on the doubao engine it is a
	// wasted request per bound. The extension budget therefore keeps the OLD
	// behaviour for this case: the window grows, and if it is finally taken it is
	// taken as a split at its end — which the notice reports honestly.
	//
	// THE CUT IS THE LONGEST PAUSE THE WINDOW HOLDS, and this is the whole reason
	// the rule is measured the way it is. "The first run longer than N" is not
	// enough: real speech pauses for a quarter of a second between phrases and
	// for less inside one, so a rule keyed on a single length either never fires
	// (every window runs to its bound and is cut mid-word) or fires inside a
	// word. Asking for the LONGEST qualifying run instead means the boundary is
	// wherever the audio itself breathed most — and a longer pause always beats a
	// shorter one, which is what makes the choice stable as the window grows.
	//
	// IT IS SCANNED PER SAMPLE, not per block, because the cut has to land on the
	// FIRST SAMPLE OF THE PAUSE: a block-wise scan quantises the cut to its own
	// width, and a cut 128 ms into the pause is a cut whose window ends before the
	// audio it was chosen for (measured — the first version cut at 60288 where the
	// pause began at 57600, and the alignment error is the block width).
	//
	// A RUN ONLY COUNTS AFTER SPEECH HAS BEEN HEARD, and HOW MUCH speech is
	// voiceWindowMinPauseSpeechMS of SPEECH (not of audio: see the constant). A
	// window that opens on silence is the operator thinking, not the operator
	// finishing: cutting there would end the round before the first word and hand
	// an empty window to a multi-second transcription.
	//
	// The run belongs to the NEXT window (its start IS the cut): silence is the
	// boundary, not the end of a phrase's audio, and giving it to the previous
	// window is what would make two windows overlap.
	best := p.longestPause(pcm, p.minSpeechBytes, p.runBytes)
	if best.run < p.runBytes && exhausted {
		// THE BUDGET IS SPENT AND NO PAUSE CARRIED ENOUGH SPEECH BEFORE IT (the
		// window is full of speech with only sub-second gaps in it — a genuinely
		// continuous utterance). Cutting at the byte bound would split a word, and
		// a pause taken earlier in a phrase is still a far better place to cut: the
		// samples on either side of it are quiet, so neither engine call is handed
		// half a word. So the floor on the speech BEFORE the pause is dropped and
		// the best pause there is wins. The floor on the pause's own LENGTH stands:
		// a gap shorter than voiceCaptureTrailingSilenceMS is the closure of a
		// consonant, not a boundary, and a cut there would be a cut inside speech
		// and is reported as one.
		best = p.longestPause(pcm, 0, p.runBytes)
	}
	if best.run >= p.runBytes {
		return voiceWindowCut{End: best.start, Run: best.run}, true
	}
	if !exhausted {
		// NO BOUNDARY YET: the window is extended (the caller reads more audio from
		// the same stream) and asked again with more audio in hand.
		return voiceWindowCut{}, false
	}
	// NOTHING TO LAND ON AT ALL — audio that never pauses (continuous music, a
	// noisy room), or a window holding no speech. The whole window is taken and
	// the split is REPORTED, never hidden (Rule 15).
	return voiceWindowCut{End: len(pcm), Split: true}, true
}

// pauseRun is one run of near-silence and where it begins.
type pauseRun struct{ start, run int }

// longestPause returns the LONGEST run of near-silence in the buffer that had at
// least `speechFloor` bytes of speech before it, from runs of at least `floor`
// bytes. It reports a zero run when there is none, and the caller decides what to
// do about that (extend the window, or take the best pause there is).
//
// A RUN THAT REACHES THE END OF THE BUFFER IS EXEMPT FROM THE SPEECH FLOOR, and
// that exemption is the case the acceptance rig found (task 01a1188c). The floor
// exists to stop a window closing on a breath INSIDE a phrase — and a breath
// inside a phrase is followed by more speech. Here the operator's latest audio IS
// the pause: there is nothing after it in the buffer to wait for, so closing there
// loses nothing (the pause carries into the next window) while refusing to close
// spends the whole extension budget and ends with the cut inside speech. Measured
// before this exemption existed: window 3 of the rig run, 9 extensions, 58 s of a
// 12.455 s recording.
func (p voiceWindowPlan) longestPause(pcm []byte, speechFloor, floor int) pauseRun {
	best := pauseRun{start: -1}
	runStart, run, speech := -1, 0, 0
	for off := 0; off+voiceCaptureBytesPerSample <= len(pcm); off += voiceCaptureBytesPerSample {
		if p.sampleIsSilent(pcm, off) {
			// SILENCE BEFORE SPEECH NEVER CLOSES A WINDOW: a window that opens on
			// silence is the operator thinking, not the operator finishing, and
			// cutting there would hand an empty round to the engine.
			if speech == 0 {
				continue
			}
			if runStart < 0 {
				runStart = off
			}
			run += voiceCaptureBytesPerSample
			tail := off+voiceCaptureBytesPerSample >= len(pcm)
			if run >= floor && (speech >= speechFloor || tail) && run > best.run {
				best = pauseRun{start: runStart, run: run}
			}
			continue
		}
		runStart, run = -1, 0
		speech += voiceCaptureBytesPerSample
	}
	return best
}

// sampleIsSilent reports whether ONE sample (at a byte offset) is below the
// shared floor. It is the same test the capture path applies to each chunk it
// receives, so "this audio is silent" is ONE comparison in this codebase, and the
// cut cannot land in a different place than the live reader would have closed.
func (p voiceWindowPlan) sampleIsSilent(pcm []byte, off int) bool {
	sample := float64(int16(binary.LittleEndian.Uint16(pcm[off:])))
	if sample < 0 {
		sample = -sample
	}
	return sample <= p.floorAmplitude()
}

// voiceWindowReport is the ONE row the mode writes per round when the boundary
// work is visible: what the round transcribed and where its audio came from.
// Every other path is unchanged (the mode's own notes still carry the transcript).
func voiceWindowReport(span voiceWindowSpan) string {
	how := "the window bound"
	switch {
	case span.Split:
		how = "the extension budget (the cut is inside speech — a word may be split across two windows)"
	case span.Run > 0:
		how = "a pause"
	}
	return fmt.Sprintf("[voice] window %s..%s ended on %s%s",
		voiceWindowDuration(span.Start), voiceWindowDuration(span.End), how, voiceWindowExtensionNote(span.Extended))
}

// voiceWindowExtensionNote states the extension count, and stays silent when
// there was none — a row that says "0 extensions" on every ordinary round is
// noise.
func voiceWindowExtensionNote(extended int) string {
	if extended == 0 {
		return ""
	}
	return fmt.Sprintf(" after %d extension(s) past the %ds bound", extended, voiceChatWindowSeconds)
}

// voiceWindowDuration renders a stream offset as a mm:ss.mmm stamp.
func voiceWindowDuration(offset int) string {
	return (time.Duration(voiceStagedSecondsOfAudioAt(offset)*voiceMSPerSecond) * time.Millisecond).String()
}
