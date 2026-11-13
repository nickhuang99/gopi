// chat_status_voice_test.go — the `/voice` field on the chat status row
// (task 01a116b0).
//
// WHAT IS UNDER TEST is the ROW, not a helper: every assertion below composes the
// production row through the production paths (chatStatusSource.Status at the
// pinned width, i.e. the string the operator reads) and asks a question with an
// ANSWER OBSERVABLE ON THAT STRING. The unit-level facts (the vocabulary's cell
// widths, the funding arithmetic) are asserted beside it because a row that is
// right for the wrong reason is the defect this task's tests exist to refuse —
// but the row is the subject, so a change that makes the helper "correct" while
// the row loses the field still fails here.
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// voiceStatusRowWith composes the PRODUCTION row at the pinned width with the
// given voice phase and no turn in flight. It drives chatStatusSource the way the
// chat does — the seam is set with setVoiceFn, exactly as cli_chat.go sets it —
// so what is asserted is the row the operator reads rather than a string built by
// the test from the same helper under test.
func voiceStatusRowWith(t *testing.T, live voiceChatLive, ops statusOps) string {
	t.Helper()
	src := newChatStatusSource("deepseek-flash", false)
	src.setOpsFn(func() statusOps { return ops })
	src.setVoiceFn(func() voiceChatLive { return live })
	return src.status().Line(statusColumnsAtCols)
}

// TestStatusRowShowsTheVoiceFieldWhileTheModeIsOn is the feature's own claim,
// measured on the composed row: the field is there while `/voice` is capturing or
// transcribing, and the row carries NO trace of it when the mode is off.
func TestStatusRowShowsTheVoiceFieldWhileTheModeIsOn(t *testing.T) {
	ops := statusOps{elapsedKnown: true, elapsed: 3720 * time.Second}

	// The field's spellings come from the mode's own composer (Rule 14: one
	// definition), so this test cannot pass against a row that invented its own.
	recording := voiceChatIndicatorFor(voiceChatLiveRecording)
	transcribing := voiceChatIndicatorFor(voiceChatLiveTranscribing)
	if recording == "" || transcribing == "" || recording == transcribing {
		t.Fatalf("the mode's composer does not state two distinct phases: recording=%q transcribing=%q",
			recording, transcribing)
	}

	// THE LIVE PHASES: the field is on the row.
	//
	// TWO INVARIANTS ARE ASSERTED HERE, and they are the operator-facing ones: the
	// row never grows past the pinned width (a field that widened the row would wrap
	// on the terminal the layout is designed for), and the cells BEFORE the live
	// block do not move — the block is the row's right-aligned tail, so a field that
	// changed the columns to its left would be the "columns move while the agent
	// works" defect (the reason every other column is a reservation).
	//
	// The row's own width is content-driven with the block's trailing pad given
	// back (an idle "ready" draws a shorter row than a "1h02m · ready" does), so the
	// assertion is an upper bound plus an equality of the PREFIX, not an equality of
	// the whole line.
	idleRow := voiceStatusRowWith(t, voiceChatLiveIdle, ops)
	idleBlock := statusLiveFieldVoice(statusLiveIdle, "", ops, voiceChatLiveIdle)
	idlePrefix := strings.TrimSuffix(idleRow, idleBlock)

	for _, tc := range []struct {
		name string
		live voiceChatLive
		want string
	}{
		{"capturing", voiceChatLiveRecording, recording},
		{"transcribing", voiceChatLiveTranscribing, transcribing},
	} {
		row := voiceStatusRowWith(t, tc.live, ops)
		if !strings.Contains(row, tc.want) {
			t.Errorf("the row does not show the %s field %q while the mode is on: %q", tc.name, tc.want, row)
		}
		if got := lineedit.DisplayWidth(row); got > statusColumnsAtCols {
			t.Errorf("the %s row is %d cells, past the %d the layout is composed for: %q",
				tc.name, got, statusColumnsAtCols, row)
		}
		// The block's own content is the row's tail (it is the last field, with no
		// trailing pad), so trimming it leaves the columns this field must not move.
		block := statusLiveFieldVoice(statusLiveIdle, "", ops, tc.live)
		if prefix := strings.TrimSuffix(row, block); prefix != idlePrefix {
			t.Errorf("the cells left of the live block changed when the mode turned on:\n idle  %q\n %s %q\n"+
				"the voice field is a change INSIDE the block, so every column before it must be untouched",
				idlePrefix, tc.name, prefix)
		}
	}

	// IDLE — THE ESC CASE. The same composer, the same width, the same session:
	// the field is gone and the row is byte-for-byte the row it was before this
	// feature existed, which is the property that makes "Esc 之后消失" checkable.
	idle := idleRow
	for _, gone := range []string{recording, transcribing} {
		if strings.Contains(idle, gone) {
			t.Errorf("the idle row still carries %q: %q — the field must vanish WHOLE when the mode ends", gone, idle)
		}
	}
	// The row the idle state composes is the row a source with NO voice seam
	// composes: the field's arrival is additive, so the two must agree.
	src := newChatStatusSource("deepseek-flash", false)
	src.setOpsFn(func() statusOps { return ops })
	if noSeam := src.status().Line(statusColumnsAtCols); noSeam != idle {
		t.Errorf("the idle row %q differs from the row a source with no voice seam draws %q — "+
			"the field is not additive, so a surface without voice would render differently", idle, noSeam)
	}
}

// TestStatusVoiceFieldWidthIsTheReservation is the funding assertion: the field
// costs the row exactly the cells the geometry reserved for it, and the words it
// can show are the same width, so the field cannot make the row wider OR narrower
// than the account says.
//
// A width that is right by accident (a glyph that measures differently in one
// terminal regime, a word with a different length) is what this refuses: the
// numbers are measured off the CONSTANTS the composer uses, not off a sample
// string, so the assertion moves with the definition (Rule 14).
func TestStatusVoiceFieldWidthIsTheReservation(t *testing.T) {
	// THE FIELD COSTS THE ROW NO GEOMETRY: it lives inside the live block's own
	// reservation (statusLiveCol), which did not change for the feature. The
	// assertion is the arithmetic that forced the word down to three cells — the
	// sentence "<field> · <widest state word the block may use>" has to fit the
	// block, or the field would vanish at exactly the state it matters in.
	widestSentence := voiceChatLivePrefixCells + lineedit.StatusSepCells +
		statusFieldCells(statusLiveWordThinkingShort)
	if widestSentence > statusLiveCol {
		t.Errorf("the widest sentence the voiced block composes, %q, is %d cells — over the %d the block "+
			"reserves; the field would be dropped while the model is thinking",
			voiceChatLiveRecordingWord+lineedit.StatusSep+statusLiveWordThinkingShort, widestSentence, statusLiveCol)
	}
	// The glyph-plus-word spelling this feature started as is REFUSED by the same
	// arithmetic, and the refusal is asserted so nobody re-introduces it without
	// meeting the measurement: a 5-cell field cannot sit beside the thinking word.
	if widened := voiceChatLivePrefixCells + 2 + lineedit.StatusSepCells +
		statusFieldCells(statusLiveWordThinkingShort); widened <= statusLiveCol {
		t.Errorf("a glyph-prefixed field would fit the block (%d <= %d), so the vocabulary's own note that "+
			"the glyph was dropped for width is stale — either restore the glyph or fix the note", widened, statusLiveCol)
	}

	// THE FIELD ITSELF: both words are the same width, so the phase cannot move the
	// cells to the field's left — the invariant that lets a fixed reservation hold
	// a changing word.
	fields := map[string]string{
		"recording":    voiceChatIndicatorFor(voiceChatLiveRecording),
		"transcribing": voiceChatIndicatorFor(voiceChatLiveTranscribing),
	}
	for name, f := range fields {
		if got := lineedit.DisplayWidth(f); got != voiceChatLivePrefixCells {
			t.Errorf("the %s field %q measures %d cells, want %d (voiceChatLivePrefixCells)",
				name, f, got, voiceChatLivePrefixCells)
		}
	}
	if lineedit.DisplayWidth(fields["recording"]) != lineedit.DisplayWidth(fields["transcribing"]) {
		t.Errorf("the two phases measure differently (%d vs %d): the cells left of the field would move when the phase changes",
			lineedit.DisplayWidth(fields["recording"]), lineedit.DisplayWidth(fields["transcribing"]))
	}
	// The words are the SAME width by construction, and that is the property the
	// reservation leans on — asserted rather than trusted (the constants are three
	// characters each, but a future edit could spell one of them differently).
	if got := lineedit.DisplayWidth(voiceChatLiveRecordingWord); got != voiceChatLiveWordCells {
		t.Errorf("the recording word %q is %d cells, want %d", voiceChatLiveRecordingWord, got, voiceChatLiveWordCells)
	}
	if got := lineedit.DisplayWidth(voiceChatLiveTranscribingWord); got != voiceChatLiveWordCells {
		t.Errorf("the transcribing word %q is %d cells, want %d", voiceChatLiveTranscribingWord, got, voiceChatLiveWordCells)
	}

	// THE IDLE PHASE COSTS NOTHING. This is the "Esc 之后消失" contract at the cell
	// level: not a blank glyph, not a zero — the empty string.
	if got := voiceChatIndicatorFor(voiceChatLiveIdle); got != "" {
		t.Errorf("the idle phase composes %q, want the empty string: an idle field must cost the row no cells at all", got)
	}
}

// TestStatusVoiceFieldFitsBesideEveryStateWord is the block's fit proof: with the
// mode on, EVERY state the turn walks — at EVERY animation frame the keepalive
// advances — composes a block that carries the field AND a state word, inside the
// reserved width.
//
// The frames matter for the same reason they matter to the compaction rule (a
// frame is a real repaint): a form that only fits at frame zero would blink the
// field away once per keepalive, and a blinking status row is a status row the
// operator stops reading.
func TestStatusVoiceFieldFitsBesideEveryStateWord(t *testing.T) {
	ops := statusOps{elapsedKnown: true, elapsed: 4 * time.Minute}
	states := []struct{ name, word string }{
		{"idle", statusLiveIdle},
		{"sent", statusLiveWordSent},
		{"thinking", statusLiveWordThinking},
		{"responding", statusLiveWordResponding},
	}
	for _, live := range []voiceChatLive{voiceChatLiveRecording, voiceChatLiveTranscribing} {
		field := voiceChatIndicatorFor(live)
		for _, st := range states {
			for frame := 0; frame < liveHintFrameCount; frame++ {
				hint := st.word + strings.Repeat(statusLiveAnimationChar, frame)
				got := statusLiveFieldVoice(hint, "", ops, live)
				if !strings.Contains(got, field) {
					t.Errorf("the %s state at animation frame %d drops the voice field %q: %q — the mode "+
						"is on, so the row must say which half of a round it is in", st.name, frame, field, got)
				}
				word, _ := statusLiveVoiceStateWord(got, field)
				if word == "" {
					t.Errorf("the %s block %q carries the field but no state word", st.name, got)
				}
				if w := statusFieldCells(got); w > statusLiveCol {
					t.Errorf("the %s block at frame %d is %d cells, over the %d the row reserves: %q",
						st.name, frame, w, statusLiveCol, got)
				}
			}
		}
	}

	// THE WIDEST REACHABLE FORM, measured rather than assumed: with the field on,
	// the block composes the field, the block's separator and the longest state word
	// that still fits — and it never exceeds the reservation (over budget would wrap
	// the row on the terminal the layout is designed for).
	widest, widestAt := 0, ""
	for _, voice := range []voiceChatLive{voiceChatLiveRecording, voiceChatLiveTranscribing} {
		for _, w := range []string{statusLiveIdle, statusLiveWordSent, statusLiveWordThinking, statusLiveWordResponding} {
			for frame := 0; frame < liveHintFrameCount; frame++ {
				v := statusLiveFieldVoice(w+strings.Repeat(statusLiveAnimationChar, frame), "", ops, voice)
				if c := statusFieldCells(v); c > widest {
					widest, widestAt = c, v
				}
			}
		}
	}
	if widest > statusLiveCol {
		t.Errorf("the widest block with the voice field on is %d cells (%q), past the reserved %d",
			widest, widestAt, statusLiveCol)
	}
	// AND THE RESERVATION IS STILL EARNED WITHOUT THE FIELD, which is what says the
	// feature did not convert a drawn column into one reserving air: the block's
	// widest form with the mode OFF is the reservation exactly.
	offWidest := statusLiveFieldVoice(statusLiveIdle, "", statusOps{elapsedKnown: true, elapsed: time.Hour + 2*time.Minute}, voiceChatLiveIdle)
	if got := statusFieldCells(offWidest); got != statusLiveCol {
		t.Errorf("the block's widest form with voice off is %d cells (%q), want the reserved %d — "+
			"the block would be reserving %d cell(s) nothing draws", got, offWidest, statusLiveCol, statusLiveCol-got)
	}
}

// statusLiveVoiceStateWord splits a composed block into its state word: the text
// after the field and its separator. It is the test's own reader (the row's
// fields are a display string, and asserting on the whole string would let a word
// hide inside the digest of another), and it returns "" when the shape is not the
// block's.
func statusLiveVoiceStateWord(block, field string) (word, rest string) {
	i := strings.Index(block, field)
	if i < 0 {
		return "", block
	}
	tail := block[i+len(field):]
	tail = strings.TrimPrefix(tail, lineedit.StatusSep)
	return tail, block[:i]
}

// TestStatusVoiceFieldOutranksTheCounterAndSaysSo pins the BLOCK'S RANK, which is
// the decision this feature makes and the one a later reader is most likely to
// mistake for an oversight: while the mode is on, the field is on the block and
// the elapsed counter is off it.
//
// The arithmetic is the reason and it is asserted, not narrated: the shortest form
// that carries the field, a counter and a state word is wider than the block's
// reservation, so one of the three must yield — and the mode's own state is the
// one the operator is watching (the counter reports something that already
// happened; the field reports that the microphone is open NOW).
func TestStatusVoiceFieldOutranksTheCounterAndSaysSo(t *testing.T) {
	ops := statusOps{elapsedKnown: true, elapsed: 3720 * time.Second}
	field := voiceChatIndicatorFor(voiceChatLiveRecording)

	// THE ARITHMETIC THAT FORCES THE CHOICE, measured from the constants the
	// composer uses: the shortest combination carrying all three does not fit.
	shortestAll := statusFieldCells(field + lineedit.StatusSep +
		statusElapsedLabel(ops.elapsed) + lineedit.StatusSep + statusLiveWordRespondingShort)
	if shortestAll <= statusLiveCol {
		t.Fatalf("the fixture no longer describes the pressure: field+counter+short word is %d cells, "+
			"inside the reserved %d — the rank this test pins is not forced, so it proves nothing",
			shortestAll, statusLiveCol)
	}

	got := statusLiveFieldVoice(statusLiveWordThinking, "", ops, voiceChatLiveRecording)
	if !strings.Contains(got, field) {
		t.Errorf("the block dropped the voice field while the mode is on: %q", got)
	}
	if strings.Contains(got, statusElapsedLabel(ops.elapsed)) {
		t.Errorf("the block carries the elapsed counter %q beside the voice field: %q — the two cannot "+
			"share the reservation, so the rank must be stated and this one is the field",
			statusElapsedLabel(ops.elapsed), got)
	}

	// AND THE COUNTER COMES BACK the moment the mode ends: the field takes the
	// counter's cells only WHILE IT IS ON, so a session that never uses /voice is
	// untouched, and one that leaves the mode gets its clock (and its compaction
	// count) back. The state word here is one the counter fits beside ("4m00s" +
	// sep + "sent" is 12 cells) — the counter's own ladder, not this feature's.
	off := statusLiveFieldVoice(statusLiveWordSent, "", ops, voiceChatLiveIdle)
	if !strings.Contains(off, statusElapsedLabel(ops.elapsed)) {
		t.Errorf("the counter is missing from the idle block %q — the field must give the cells back when the mode ends", off)
	}
	compacted := statusOps{compressionsKnown: true, compressions: 30}
	offCompaction := statusLiveFieldVoice(statusLiveWordThinkingShort, "", compacted, voiceChatLiveIdle)
	if !strings.Contains(offCompaction, statusCompactionLabel(compacted.compressions)) {
		t.Errorf("the compaction counter is missing from the idle block %q", offCompaction)
	}
}
