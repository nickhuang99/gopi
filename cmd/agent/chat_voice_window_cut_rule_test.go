package main

// chat_voice_window_cut_rule_test.go — THE CUT RULE against a REAL pause, as a
// unit property (task 01a11b59).
//
// THE DEFECT THESE CASES PIN, in one sentence: the pause that may close a window
// was required to BEGIN after voiceWindowMinAudioMS of the window (1500 ms), so
// on continuous dictation — where a person speaks short phrases and breathes for
// a fifth of a second between them — NO pause qualified until the window was
// already longer than 1.5 s, the window grew to the extension budget, and the cut
// was taken INSIDE speech: a word split across two windows, and a systematic loss
// of the words around the split.
//
// The rule the fix states: a window may be closed by the LONGEST run of
// near-silence it holds, provided the window carried at least
// voiceWindowMinPauseSpeechMS of SPEECH before it. How much AUDIO the window holds
// is a separate question (voiceWindowMinAudioMS — a window below it is never cut
// at all); requiring that same span of SPEECH before every pause is what made a
// 200 ms breath invisible.
//
// THE FIXTURE IS THE MEASURED ONE, not an imagined one: it is sized exactly like
// the phrases the acceptance rig's recording is made of — 1.1 s of speech, the
// 200 ms pause the constant's own calibration found between phrases, then more
// speech. All audio is synthetic and constant-amplitude (boundarySpeech /
// boundarySilence / boundaryBlock, the siblings in
// chat_voice_window_boundary_test.go), so the expected cut is arithmetic.
//
// THE RED-THEN-GREEN RECORD IS IN the task's report: `probe-BEFORE-*.txt` is the
// same rule run over the real fixture bytes before the fix (NO cut anywhere, six
// extensions spent, every window ended at its byte bound), and the cases below are
// the smallest sample that shows it.

import (
	"fmt"
	"strings"
	"testing"
)

const (
	// cutRuleTestWindowSeconds is the window bound for these cases. It is the
	// PRODUCTION bound (not a local one): the defect is about how a 6 s window
	// behaves, and a case that ran a different bound would be measuring a
	// different rule.
	cutRuleTestWindowSeconds = voiceChatWindowSeconds

	// cutRuleTestPhraseMS is the speech this fixture is built from: a phrase 200 ms
	// longer than the floor the rule names, so the case fails if that floor ever
	// grows past what a person actually says between breaths.
	cutRuleTestPhraseMS = 1200
)

// cutRuleTestBytesAt is a moment in the fixture, in the stream's unit (Rule 13:
// derived from the capture's own rate, never a number typed at the comparison).
func cutRuleTestBytesAt(ms int) int {
	return voiceCaptureSampleRate * voiceCaptureBytesPerSample * ms / voiceMSPerSecond
}

// cutRuleTestWindow is the fixture every case starts from: one phrase, the
// fixture's own 200 ms pause, then more speech than the window bound.
func cutRuleTestWindow() []byte {
	pause := boundarySilence(voiceCaptureTrailingSilenceMS)
	held := append(boundarySpeech(cutRuleTestPhraseMS), pause...)
	return append(held, boundarySpeech(cutRuleTestWindowSeconds*1000)...)
}

// TestVoiceWindowCutLandsOnAPhrasePause is the defect itself: a phrase, a 200 ms
// breath and more speech produced NO cut before this change (the pause began
// before the window had 1500 ms of audio), so the window ran to its bound and was
// finally cut inside a word.
func TestVoiceWindowCutLandsOnAPhrasePause(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	held := cutRuleTestWindow()

	cut, ok := plan.cut(held, false)
	if !ok {
		t.Fatalf("a window holding %dms of speech, a %dms pause and %dms more produced no cut: the pause after the first phrase is not a boundary",
			cutRuleTestPhraseMS, voiceCaptureTrailingSilenceMS, cutRuleTestWindowSeconds*1000)
	}
	if want := cutRuleTestBytesAt(cutRuleTestPhraseMS); cut.End != want {
		t.Errorf("cut at byte %d, want %d — the first sample of the pause that follows the first phrase", cut.End, want)
	}
	if cut.Split {
		t.Errorf("the cut was reported as a split although a %dms pause was available", voiceCaptureTrailingSilenceMS)
	}
	if want := cutRuleTestBytesAt(voiceCaptureTrailingSilenceMS); cut.Run < want {
		t.Errorf("the cut claims a %d-byte run, shorter than the %d bytes of the pause it landed on", cut.Run, want)
	}
}

// TestVoiceWindowCutEndsOnThePhraseTheBufferEndsOn is the measured BEFORE/AFTER in
// its smallest form, and it is the case an unbounded round hits first — the shape a
// long /voice session spends most of its rounds in, where the newest audio IS the
// last phrase and the pause after it has arrived but no further speech has.
//
// THE BEFORE, measured on the operator's own recording (artifacts/probe-BEFORE-
// short18.txt, the production cut() over real bytes, no engine involved): this
// buffer had NO cut at all, the round extended to the budget and was finally cut
// on whatever run it held — "window 3: 12.228s..18.000s ext=0 run=0ms", i.e. the
// round ended at the byte bound with no pause under it.
func TestVoiceWindowCutEndsOnThePhraseTheBufferEndsOn(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	// The phrase is the window MINIMUM here, so the buffer is a window the ledger
	// would take (a shorter one is refused by cut's own first guard) and the test is
	// about the boundary rather than about that guard.
	const phraseMS = voiceWindowMinAudioMS
	held := append(boundarySpeech(phraseMS), boundarySilence(voiceCaptureTrailingSilenceMS)...)

	cut, ok := plan.cut(held, false)
	if !ok {
		t.Fatalf("a window that ends on the pause after a %dms phrase produced no cut", phraseMS)
	}
	if want := cutRuleTestBytesAt(phraseMS); cut.End != want {
		t.Errorf("cut at byte %d, want %d — the pause that ends the buffer is a boundary", cut.End, want)
	}
	if cut.Split {
		t.Errorf("the cut was reported as a split although it landed on a %dms pause", voiceCaptureTrailingSilenceMS)
	}
}

// TestVoiceWindowCutTakesTheLongestPauseNotTheFirst pins the half of the rule that
// must not regress while the position floor is removed: a window holding two
// pauses is cut at the LONGER one, so the boundary is wherever the audio breathed
// most rather than wherever it first paused.
func TestVoiceWindowCutTakesTheLongestPauseNotTheFirst(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	shortPause := boundarySilence(voiceCaptureTrailingSilenceMS)
	longPause := boundarySilence(voiceCaptureTrailingSilenceMS * 2)
	held := append(boundarySpeech(cutRuleTestPhraseMS), shortPause...)
	held = append(held, boundarySpeech(cutRuleTestPhraseMS)...)
	held = append(held, longPause...)
	held = append(held, boundarySpeech(cutRuleTestPhraseMS)...)

	cut, ok := plan.cut(held, false)
	if !ok {
		t.Fatalf("a window holding a %dms and a %dms pause produced no cut", voiceCaptureTrailingSilenceMS, voiceCaptureTrailingSilenceMS*2)
	}
	if want := cutRuleTestBytesAt(cutRuleTestPhraseMS*2 + voiceCaptureTrailingSilenceMS); cut.End != want {
		t.Errorf("cut at byte %d, want %d — the first sample of the LONGER pause", cut.End, want)
	}
}

// TestVoiceWindowCutRefusesAPauseBeforeItsPhrase is the other side of the same
// rule, and it is why the speech floor stays: a breath taken a few hundred
// milliseconds into the window is not the end of a phrase. What must NOT happen is
// a window closed on it while there is still time to look for a better boundary.
func TestVoiceWindowCutRefusesAPauseBeforeItsPhrase(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	const shortPhraseMS = voiceWindowMinPauseSpeechMS - 200
	held := append(boundarySpeech(shortPhraseMS), boundarySilence(voiceCaptureTrailingSilenceMS)...)
	held = append(held, boundarySpeech(cutRuleTestWindowSeconds*1000)...)

	if cut, ok := plan.cut(held, false); ok {
		t.Errorf("a %dms pause was taken as a boundary although the window had carried only %dms of speech (the floor is %dms): cut at byte %d",
			voiceCaptureTrailingSilenceMS, shortPhraseMS, voiceWindowMinPauseSpeechMS, cut.End)
	}
	// WITH THE BUDGET SPENT the best available pause beats a cut inside speech, and
	// the cut is taken there rather than at the byte bound.
	cut, ok := plan.cut(held, true)
	if !ok {
		t.Fatalf("with the extension budget spent, a window holding a %dms pause produced no cut", voiceCaptureTrailingSilenceMS)
	}
	if want := cutRuleTestBytesAt(shortPhraseMS); cut.End != want {
		t.Errorf("the spent-budget cut landed at byte %d, want %d — the pause was available and is better than the bound", cut.End, want)
	}
	if cut.Split {
		t.Errorf("the spent-budget cut was reported as a split although it landed on a %dms pause", voiceCaptureTrailingSilenceMS)
	}
}

// TestVoiceWindowCutLeavesAPauseOnlyWindowAlone pins the branch the acceptance rig
// found (task 01a1188c): a window that opens with the tail of a phrase and then
// carries nothing but silence has less than voiceWindowMinAudioMS of SPEECH in it,
// so the window minimum — which exists to keep a round from being spent on a
// fragment — has nothing to protect. Without this the window extended nine times
// and was finally cut inside speech.
func TestVoiceWindowCutLeavesAPauseOnlyWindowAlone(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	held := append(boundarySpeech(voiceCaptureTrailingSilenceMS), boundarySilence(cutRuleTestWindowSeconds*1000)...)

	cut, ok := plan.cut(held, false)
	if !ok {
		t.Fatalf("a window holding %dms of speech and then %dms of silence produced no cut",
			voiceCaptureTrailingSilenceMS, cutRuleTestWindowSeconds*1000)
	}
	if want := cutRuleTestBytesAt(voiceCaptureTrailingSilenceMS); cut.End != want {
		t.Errorf("cut at byte %d, want %d — the first sample of the trailing silence", cut.End, want)
	}
	if cut.Split {
		t.Errorf("a pause-only window was reported as a split: %+v", cut)
	}
}

// TestVoiceWindowCutReportsASplitWhenThereIsNothingToLandOn is the non-regression
// of the hint task 01a11b39 added: when the budget is spent on audio that never
// pauses, the cut IS inside speech and it must be REPORTED as one (the notice
// `chatVoiceWindowSplitNote` carries the count), never hidden. The change to the
// cut rule must not turn this into a silent cut.
func TestVoiceWindowCutReportsASplitWhenThereIsNothingToLandOn(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	held := boundarySpeech(cutRuleTestWindowSeconds * 1000) // speech, with no pause anywhere

	cut, ok := plan.cut(held, true)
	if !ok {
		t.Fatalf("with the budget spent, a window of unbroken speech produced no cut at all")
	}
	if !cut.Split {
		t.Errorf("a cut inside unbroken speech was not reported as a split: %+v", cut)
	}
	if cut.End != len(held) {
		t.Errorf("cut at byte %d, want the whole window %d", cut.End, len(held))
	}
	if note := fmt.Sprintf(chatVoiceWindowSplitNote, voiceWindowMaxExtensions, cutRuleTestWindowSeconds); !strings.Contains(note, "[voice]") {
		t.Errorf("the split notice does not render: %q", note)
	}
}

// TestVoiceWindowCutKeepsASpeechlessWindowOutOfTheEngine is the measured
// non-regression: a room with nobody in it must not have a silent window submitted
// every bound. Closing a speechless window at the bound looks tidy and is wrong —
// on the local engine it appends a hallucinated sentence to the line every 6 s
// (measured on the rig: five "我想要一个完整的家。" rows in a row during the quiet
// tail), and on doubao it is a wasted request per bound. A boundary is a pause
// AFTER speech, so a window holding no speech has NO boundary to find and the
// window keeps growing, as it always did.
func TestVoiceWindowCutKeepsASpeechlessWindowOutOfTheEngine(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	silent := boundarySilence(cutRuleTestWindowSeconds * 1000)

	if cut, ok := plan.cut(silent, false); ok {
		t.Errorf("a window of pure silence was cut at byte %d and would be submitted to the engine", cut.End)
	}
	// WITH THE BUDGET SPENT the window is finally taken — and honestly reported as
	// a split, because it was taken inside whatever is there.
	cut, ok := plan.cut(silent, true)
	if !ok {
		t.Fatalf("with the budget spent a speechless window produced no cut: the stream could never advance")
	}
	if !cut.Split {
		t.Errorf("a speechless window taken at its end was not reported as a split: %+v", cut)
	}
	if cut.End != len(silent) {
		t.Errorf("cut at byte %d, want the whole window %d", cut.End, len(silent))
	}
	// Below the bound nothing is decided either: the stream may still deliver speech.
	if _, ok := plan.cut(silent[:plan.boundBytes-voiceCaptureReadChunk], false); ok {
		t.Errorf("a window below its bound was cut although the stream may still deliver speech")
	}
}

// TestVoiceWindowCutTilesAcrossAPhrasePause is the property the whole ledger rests
// on, at the shortest pause that may close a window: the audio after the cut is
// CARRIED, so two consecutive rounds cover the capture with no gap and no overlap.
func TestVoiceWindowCutTilesAcrossAPhrasePause(t *testing.T) {
	plan := newVoiceWindowPlan(cutRuleTestWindowSeconds)
	held := cutRuleTestWindow()
	cut, ok := plan.cut(held, false)
	if !ok {
		t.Fatalf("no cut on the fixture, so its tiling cannot be checked")
	}
	head, tail := held[:cut.End], held[cut.End:]
	if len(head)+len(tail) != len(held) {
		t.Errorf("the window and its carry do not cover the buffer: %d + %d != %d", len(head), len(tail), len(held))
	}
	if len(tail) < 2 || tail[0] != 0 || tail[1] != 0 {
		t.Errorf("the carry does not begin with the silence the cut landed on: %v", tail[:4])
	}

	var led voiceWindowLedger
	led.add(voiceWindowSpan{Start: 0, End: cut.End, Run: cut.Run})
	// The second round is what the stream would take next: the carry (which OPENS
	// with the pause — and a window that opens on silence is never cut there, which
	// is the other half of the rule), then a phrase and a pause of its own.
	next := append(append([]byte{}, tail...), boundarySpeech(cutRuleTestPhraseMS)...)
	next = append(next, boundarySilence(voiceCaptureTrailingSilenceMS)...)
	second, ok := plan.cut(append(next, boundarySpeech(cutRuleTestWindowSeconds*1000)...), false)
	if !ok {
		t.Fatalf("the carried audio produced no second cut — the boundary cannot tile")
	}
	if want := len(tail) + cutRuleTestBytesAt(cutRuleTestPhraseMS); second.End != want {
		t.Errorf("the second cut landed at byte %d, want %d — the pause after the phrase the carry's head was followed by", second.End, want)
	}
	led.add(voiceWindowSpan{Start: cut.End, End: cut.End + second.End, Run: second.Run})
	if err := led.verify(cut.End + second.End); err != nil {
		t.Errorf("two rounds cut at a phrase pause do not tile the stream: %v", err)
	}
}

// TestVoiceWindowPauseSpeechFloorFitsInsideTheWindow is the invariant behind the
// constant: the speech a window must carry before a pause may close it has to fit
// INSIDE the window minimum — otherwise a window sized for a fragment would demand
// more speech than the floor it was supposed to protect, which is the shape the
// original rule had (1500 ms of speech required to close a window that may be
// refused at 1500 ms of audio).
func TestVoiceWindowPauseSpeechFloorFitsInsideTheWindow(t *testing.T) {
	if voiceWindowMinPauseSpeechMS > voiceWindowMinAudioMS {
		t.Errorf("a window must carry %dms of speech before a pause may close it, but a window of %dms is never cut at all: the floor cannot be reached before the window minimum is",
			voiceWindowMinPauseSpeechMS, voiceWindowMinAudioMS)
	}
	if cutRuleTestPhraseMS < voiceWindowMinPauseSpeechMS {
		// THE CASE'S OWN FIXTURE MUST BE ABLE TO TRIGGER THE RULE, or the green
		// above would be about a fixture no pause can close: the phrase before the
		// fixture's pause has to carry at least the speech the floor demands.
		t.Errorf("the rule requires %dms of speech before a pause, more than the %dms phrase the cases above are built from",
			voiceWindowMinPauseSpeechMS, cutRuleTestPhraseMS)
	}
}
