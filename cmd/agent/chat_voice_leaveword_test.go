package main

// chat_voice_leaveword_test.go — THE WORD THE PROMPT TEACHES MUST BE THE WORD
// THAT LEAVES THE MODE.
//
// WHAT WAS WRONG (measured on a real pty, task 01a1424f-4526-76df-b3f8-ae3172ce643e;
// the grids are in that task's report). Two surfaces ADVERTISED `status` as the
// argument that leaves the mode:
//
//	entered block   `  text  each transcript is APPENDED at the caret (`status` or
//	                Esc leaves, the line is kept).`
//	live usage row  `Press Ctrl+G when you are DONE SPEAKING … Esc or `status`
//	                leaves the mode and keeps the line.`
//
// while the dispatcher answers `status` by PRINTING the status and leaving the
// mode ON — the word that leaves is chatVoiceStopWord ("stop"). So the prompt
// taught a key that does not do what the prompt says, and an operator who
// followed it typed `/voice status`, read a status block, and stayed on the air.
//
// WHY THIS CASE IS DISPATCH-DRIVEN, and not a string comparison. A comparison
// between the rendered block and chatVoiceStopWord would have passed the moment
// the CONSTANT was re-spelled, and it would not have been the defect's judge
// either: the dispatcher was the half that was RIGHT. The defect lived between
// the advertisement and the behaviour, so the case has to cross that gap: the
// word is TAKEN OUT OF WHAT THE OPERATOR READS (parsed out of the rendered block,
// not named by the case), sent through the mode's own command line — the same
// entry point the REPL's switch calls — and then the mode is asked whether it is
// still on. Pre-fix it read `status`, the mode stayed ON, and this case failed;
// post-fix the same code reads `stop` and the mode goes off. That is the red→green
// the report carries.
//
// THE PARTNER CASES are the mirror image, so a future change cannot make BOTH
// words leave the mode and call it a fix: the status word reports and stays
// (the property the entered block's old wording denied), and the entered row's
// advertised word is the constant, so the two surfaces cannot drift apart again.

import (
	"strings"
	"testing"
)

// voiceAdvertisedLeaveWord is the word a rendered block TEACHES, read out of the
// block itself: the first backticked token, which is the shape every row of this
// feature uses to quote an argument (the refusal quotes `/voice status`, and the
// two leave rows quote the leave word). It is a PARSER and not a lookup on
// purpose — the case must be able to fail when the block advertises the wrong
// word, and a helper that asked the dispatcher what the word was would be
// asking one half to confirm the other.
func voiceAdvertisedLeaveWord(t *testing.T, block string) string {
	t.Helper()
	open := strings.Index(block, "`")
	if open < 0 {
		t.Fatalf("the block the operator reads advertises no argument at all — the leave instruction is not a backticked word:\n%s", block)
	}
	rest := block[open+1:]
	end := strings.Index(rest, "`")
	if end < 0 {
		t.Fatalf("the block opens a backtick and never closes it:\n%s", block)
	}
	word := strings.TrimSpace(rest[:end])
	if word == "" {
		t.Fatalf("the block advertises an EMPTY argument:\n%s", block)
	}
	return word
}

// voiceAnnouncedBlockFrom is the announced record starting at the marker, to the
// end of the record — the operator's OWN view of a block as the console wrote it
// (the painted band scrolls, so the announcement seam is the record of what was
// said; see voicePtyGrid.waitAnnounced).
func voiceAnnouncedBlockFrom(log, marker string) string {
	at := strings.Index(log, marker)
	if at < 0 {
		return ""
	}
	return log[at:]
}

// voiceAnnouncedRowWith is the ONE announced line carrying the marker. It exists
// for the live arm's usage row, whose word must be read from the row itself and
// whose block is a single (long) line: reading a whole block would let a word
// from ANOTHER row answer for it.
func voiceAnnouncedRowWith(log, marker string) string {
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
}

// TestChatVoice_AdvertisedLeaveWordLeavesTheMode is THE DEFECT CASE for the
// window arm: the entered block's own words, dispatched through the mode's own
// line, must turn the mode OFF.
func TestChatVoice_AdvertisedLeaveWordLeavesTheMode(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic"}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	if !h.mode.active() {
		t.Fatalf("the mode did not enter: %s", h.rows())
	}
	h.waitAnnounced(t, "the entered block", voiceNoteHeadEntered)
	block := voiceAnnouncedBlockFrom(h.log.String(), voiceNoteHeadEntered)
	word := voiceAdvertisedLeaveWord(t, block)

	// THE BEHAVIOUR — the assertion the pre-fix code fails, and the only one that
	// can fail for the right reason. It dispatches through handleLine, exactly the
	// call the REPL's switch makes for a `/voice …` line.
	h.mode.handleLine(h.in, completionSlashPrefix+chatVoiceCommandName+" "+word)
	if h.mode.active() {
		t.Fatalf("the entered block teaches `%s`, and `/voice %s` left the mode ON — the prompt teaches a key that does not do what it says.\n--- the block the operator read ---\n%s",
			word, word, block)
	}
	// AND THE ADVERTISEMENT IS THE CONSTANT (Rule 13): with the behaviour pinned
	// above, this arm is what keeps the two surfaces from drifting apart again.
	if word != chatVoiceStopWord {
		t.Errorf("the entered block teaches `%s`; the word this mode leaves on is %q — one of the two is wrong and the operator cannot tell which",
			word, chatVoiceStopWord)
	}
	// THE LEAVING ROW NAMES THE REASON, so the exit is attributable on the screen
	// the operator is looking at (the word arm, not the toggle).
	h.waitAnnounced(t, "the leaving note for the advertised word", chatVoiceLeftByWord)
}

// TestChatVoice_AdvertisedStatusWordReportsAndStays is the MIRROR: the status
// word is a REPORT. Without this half, a change that made both words leave the
// mode would satisfy the case above while destroying `/voice status` — and the
// old wording is exactly the claim that this test denies.
func TestChatVoice_AdvertisedStatusWordReportsAndStays(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic"}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	if !h.mode.active() {
		t.Fatalf("the mode did not enter: %s", h.rows())
	}
	h.mode.handleLine(h.in, completionSlashPrefix+chatVoiceCommandName+" "+chatVoiceStatusWord)
	if !h.mode.active() {
		t.Fatalf("`/voice %s` left the mode — the status word reports the mode, it does not end it", chatVoiceStatusWord)
	}
	h.waitAnnounced(t, "the status-ON block", voiceNoteHeadStatusOn)
}

// TestChatVoiceLive_AdvertisedLeaveWordLeavesTheMode is the same defect case on
// the LIVE arm, whose usage row carries its own copy of the leave instruction
// ("Esc or `<word>` leaves the mode and keeps the line."). The word is read off
// that row, and the mode must go off when it is dispatched.
func TestChatVoiceLive_AdvertisedLeaveWordLeavesTheMode(t *testing.T) {
	h := newLivePtyGrid(t)
	h.mode.handleLine(h.in, completionSlashPrefix+chatVoiceCommandName)
	if !h.mode.active() {
		t.Fatalf("the mode did not enter: %s", h.rows())
	}
	h.waitAnnounced(t, "the live arm's usage row", voiceLiveFinishKeyLabel)
	row := voiceAnnouncedRowWith(h.log.String(), voiceLiveFinishKeyLabel)
	if row == "" {
		t.Fatalf("the live arm announced no usage row naming %q:\n%s", voiceLiveFinishKeyLabel, h.log.String())
	}
	word := voiceAdvertisedLeaveWord(t, row)

	h.mode.handleLine(h.in, completionSlashPrefix+chatVoiceCommandName+" "+word)
	if h.mode.active() {
		t.Fatalf("the live usage row teaches `%s`, and `/voice %s` left the mode ON — the row advertises a key that does not leave.\n--- the row the operator read ---\n%s",
			word, word, row)
	}
	if word != chatVoiceStopWord {
		t.Errorf("the live usage row teaches `%s`; the word this mode leaves on is %q", word, chatVoiceStopWord)
	}
}
