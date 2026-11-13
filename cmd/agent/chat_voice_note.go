package main

// chat_voice_note.go — the /voice notices' ONE renderer.
//
// WHY THIS FILE EXISTS. Every row `/voice` shows the operator used to be an
// INLINE Fprintf template written at the call site (28 `m.notice(chatVoiceX, …)`
// sites over 20-odd template constants in chat_voice.go). Nothing decided what
// those rows LOOKED like: the shape of the feature's whole visible surface was
// whatever the last person typed, one sentence at a time. The neighbouring
// command had already been through this and settled it — /info is rendered by
// `chatInfoBlock` (chat_info.go), a PURE function of a fields struct with a
// written contract over it and exact-bytes tests under it — and this file is
// `/voice`'s equivalent: ONE pure producer of every notice byte the mode writes.
//
// WHAT IS PURE HERE. Every `voiceNote*` function below takes values and returns
// the row's TEXT. None reads the mode, the session, the terminal, the clock or
// any global; none writes anything; none is a method on anything. That is what
// makes the exact bytes assertable in a unit test with no terminal, and it is
// what makes the REPL arms content-free (they hand over facts and print what
// comes back). The ONE thing the renderer cannot know is the BAND WIDTH — the
// reserved layout wraps rows itself (chatInput.WriteNotice → lineedit.WriteLines)
// — so the contract below is written to hold at ANY width, and the fact layout it
// promises is asserted on a real 60-column pty rather than assumed.
//
// THE SHAPE IS DELIBERATE — and it is NOT /info's shape, on purpose.
//
// THE SHAPE, in two parts, chosen by WHAT THE NOTE IS (not by which row it is):
//
//  1. A NOTE THAT IS A LIST OF INDEPENDENT FACTS IS A BLOCK. Its first row is
//     the head — prefix plus a short claim ("VOICE MODE ON") — and each fact
//     follows on ITS OWN ROW, indented, as `label<gap>value`. The facts are the
//     ones an operator would otherwise have to pick out of a 200-character
//     sentence: which device is recording, which engine is transcribing, how long
//     a window is, and where the text goes. ONE FACT PER ROW is the whole point:
//     a row cannot be half-read, and a fact that shares a line with another fact
//     cannot be found by eye on a screen that is already scrolling.
//  2. A NOTE THAT IS ONE SENTENCE STAYS ONE ROW. A block whose body is a single
//     clause is decoration: it costs a row and separates nothing. The refusals,
//     the failures, the transcript row, the silence rows and the live-arm rows
//     are each one claim, so each is one row.
//
// WHY IT IS NOT `chatInfoBlock`'s SHAPE — the difference is a DECISION, stated
// here because "two surfaces differ" must be a decision and not an accident:
//
//   - /info's block is COPYABLE. Its reason to exist is that a person selects a
//     line and pastes it (a session UUID into `gopi session log`), so it is
//     rendered for the SELECTION: values start at column 0, nothing is padded and
//     nothing is decorative — the empty separator `chatInfoLabelSep` is two
//     spaces precisely because a colon or a rule would be pasted along with the
//     value (chat_info.go says so in as many words).
//   - /voice's notices are GLANCEABLE. Nobody copies "VOICE MODE ON"; they look
//     up from the keyboard for a second, mid-dictation, and ask "is it on, which
//     microphone, is my text going somewhere?". So the rows are OPTIMISED FOR THE
//     GLANCE: the head is a claim, the facts are aligned under it, and an indent
//     plus a label column is what lets the eye land on `device` without reading
//     the sentence around it. Indentation is therefore CORRECT here and would be
//     WRONG in /info — the same two spaces that make a fact findable here are two
//     characters that would be pasted into a command line there.
//
//   Both surfaces keep the one rule they SHARE, and it is the rule that matters:
//   a fact that shares a row with another fact cannot be used on its own. /info
//   gets that from `key: value` rows; /voice gets it from `label<gap>value` rows.
//   Neither ever puts two facts on one row.
//
// WHAT THE BLOCK PROMISES AT ANY WIDTH (the readability property, asserted on a
// real pty at 60 columns in voice_note_test.go's carrier case):
//
//   - every fact BEGINS ITS OWN ROW, so a narrow terminal can only wrap a VALUE;
//     it can never push a fact's label onto a row that already carries another
//     fact's label. The labels therefore remain the thing the eye scrolls for,
//     whatever the width;
//   - the head is its own row, so a wrapped head cannot bury a fact;
//   - a value is NEVER truncated by this renderer. Width is the layout's business
//     (lineedit wraps and scrolls), and deleting text to make a row fit is the
//     one thing the epic forbids: no fact is dropped for looks (Rule 15 — a fact
//     the operator needs is stated, never elided).
//
// THE PREFIX IS A POLICY, NOT A HABIT (the two constants below, and every row in
// this file goes through ONE of them):
//
//   - `[gopi] ` — THE MODE'S OWN VOICE: the session talking about the mode's
//     lifecycle and telling the operator what it just did for them — entering,
//     leaving, the status answer, the refusals, the failure rows, the live arm's
//     key acknowledgements.
//   - `[voice] ` — THE ENGINE'S VOICE: everything that is a fact about AUDIO or
//     about what was HEARD — the transcript row, the silence rows, the window-cut
//     rows, the engine/provenance rows, and the finish row that reports audio the
//     capture lost.
//
// The criterion is WHO IS SPEAKING, and it is stated because the split used to be
// an accident: the same feature printed `[gopi] voice mode is ON …` beside
// `[voice] no text in the last 6s window …` with nothing written down to say why,
// and the refusal rows carried NO prefix at all while sitting between them. The
// refusals are the mode's own voice and now say so (a row that names the command
// and refuses it is the mode talking), which is the ONE byte change this file
// makes to existing text; the report lists it.
//
// SHARED TEXT IS NEVER RE-PREFIXED. Three of the mode's rows are not this file's
// to author: the privacy disclosure and the engine/provenance rows are composed
// by the ONE function each surface prints (`voiceDoubaoPrivacyNotice`,
// `voiceEngineProvenanceLabel`, `chatVoiceStreamEngineLabel`,
// `chatVoiceSaucEngineLabel`, `voiceLiveUsageNote`) and they ALREADY carry their
// own `[voice] ` prefix, because the CLI prints the same sentence. Re-typing the
// prefix here is exactly the defect that produced `[voice] [voice] PRIVACY …`
// (measured; see voice_module_defects_test.go). Those rows reach the console
// through `voiceNoteShared`, which exists to say out loud "this text has an owner
// elsewhere and the owner already prefixed it" (Rule 13/14: the value's spelling
// lives with the value).

import (
	"fmt"
	"strings"
	"time"
)

// The two prefixes — the WHOLE policy in two constants. Every row this feature
// prints is prefixed by one of these, and no other file spells them for `/voice`.
const (
	// voiceNotePrefixMode marks a row the MODE is saying about itself.
	voiceNotePrefixMode = "[gopi] "
	// voiceNotePrefixVoice marks a row about AUDIO — what was heard, where it
	// went, or what the capture lost.
	voiceNotePrefixVoice = "[voice] "
)

// The block's layout. Both are named because the tests assert the shape by them
// and a fact row's first column is a property of the contract, not of a template.
const (
	// voiceNoteFactIndent is what makes a fact findable at a glance: it puts the
	// label column to the right of the head's first character, so the eye lands on
	// `device` instead of reading the row's beginning. It is the character that
	// would be WRONG in /info's copyable block (see the file's contract).
	voiceNoteFactIndent = "  "
	// voiceNoteFactGap separates a fact's label from its value on one row. It is
	// not a colon: a colon reads as YAML and invites the reader to parse, while
	// this block is meant to be skimmed. Values are never padded to this gap —
	// padding would make the gap part of the value on a selection.
	voiceNoteFactGap = "  "
	// voiceNoteNotNamed is what a fact that no surface reported prints as. A block
	// row whose value is blank would read as "this renderer dropped it" (Rule 15:
	// the honest spelling, never a blank the reader fills in).
	voiceNoteNotNamed = "(not reported)"
	// voiceNoteWindowFormat renders the window fact from the constant the loop
	// ENFORCES (voiceChatWindowSeconds), never from a literal at the call site.
	voiceNoteWindowFormat = "%ds"
)

// voiceNoteFact is ONE fact of a block: its label and its value. The value is
// never truncated and the label is never wrapped into the value — the two cells
// the operator reads separately.
type voiceNoteFact struct{ Label, Value string }

// voiceNoteAuthored renders a ONE-ROW note this file prefixes itself. It is the
// single-row arm of the contract: one claim, one row.
//
// It returns the text WITHOUT a trailing newline; the call sites pass it to
// `m.notice("%s\n", …)`, so the row's terminator is decided in one place and a
// body containing a `%` can never be mis-read as a format string.
func voiceNoteAuthored(prefix, sentence string) string {
	return prefix + sentence
}

// voiceNoteAuthoredBlock renders a BLOCK: the head row, then one row per fact.
// It is pure and width-agnostic — it decides SEGMENTATION (which fact is on which
// row), and never decides wrapping (the layout's job).
func voiceNoteAuthoredBlock(prefix, head string, facts ...voiceNoteFact) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(head)
	for _, f := range facts {
		b.WriteString("\n")
		b.WriteString(voiceNoteFactIndent)
		b.WriteString(f.Label)
		b.WriteString(voiceNoteFactGap)
		b.WriteString(f.value())
	}
	return b.String()
}

// value is the fact's printed value, with the absent-fact spelling (Rule 15).
func (f voiceNoteFact) value() string {
	if strings.TrimSpace(f.Value) == "" {
		return voiceNoteNotNamed
	}
	return f.Value
}

// voiceNoteShared renders text that ANOTHER OWNER authored and ALREADY prefixed
// (see the file's contract). The renderer's job here is only to normalise the
// terminator, so every note takes the same road to the console; it must never add
// a prefix, and it does not.
func voiceNoteShared(text string) string {
	return strings.TrimRight(text, "\n")
}

// ---------------------------------------------------------------------------
// The mode's own voice (`[gopi]`): lifecycle, status, refusals, failures.
// ---------------------------------------------------------------------------

// The block heads and the single-row bodies. The bodies of the single-row notes
// keep the EXACT bytes they had as inline templates — a shape decision is not a
// licence to reword rows the operator (and the tests) already read; the rows that
// gained STRUCTURE are the two whose content is a list of facts (entered, status
// on), and the report names that change.
const (
	// voiceNoteHeadEntered is the entered block's claim.
	voiceNoteHeadEntered = "VOICE MODE ON"
	// voiceNoteHeadStatusOn is the status-ON block's claim. It reads as a sentence
	// about the mode because it answers `/voice status`, where the operator is
	// asking a question rather than watching a transition.
	voiceNoteHeadStatusOn = "voice mode is ON"

	// The fact labels. Named (Rule 1b) because the tests assert the block by
	// label, and the labels ARE the glanceable contract.
	voiceNoteLabelDevice = "device"
	voiceNoteLabelEngine = "engine"
	voiceNoteLabelWindow = "window"
	voiceNoteLabelText   = "text"
	voiceNoteLabelLeave  = "leave"

	// voiceNoteTextBody is the fourth fact of the entered block: WHERE the text
	// goes and how to get out. It is the sentence the inline template carried
	// after its second `;`, kept word for word — the fact moved to its own row,
	// the text did not change.
	voiceNoteTextBody = "each transcript is APPENDED at the caret (`%s` or Esc leaves, the line is kept)."
	// voiceNoteLeaveBody is the status-ON block's last fact: how to leave.
	voiceNoteLeaveBody = "`%s` or Esc leaves."

	// voiceNoteLeftBody / voiceNoteErrorBody / voiceNoteRetryBody / … are the
	// single-row bodies. The prefix is concatenated from the ONE constant above,
	// so the policy has exactly one definition per row and the body stays a body.
	voiceNoteLeftBody            = "voice mode OFF (%s) — %s"
	voiceNoteErrorBody           = "voice mode FAILED — %v. The mode is OFF; the text on the line is kept."
	voiceNoteRetryBody           = "voice round FAILED (%d of %d) — %v. Still listening; %d more failed window(s) in a row turns the mode off."
	voiceNoteStatusOffBody       = "voice mode is OFF — `/%s` starts it."
	voiceNoteDepsRefusalBody     = "`/%s` cannot start: %s. " + voiceNoteRefusalTail
	voiceNoteDeviceRefusalBody   = "`/%s` cannot start: %v. " + voiceNoteRefusalTail
	voiceNoteArgRefusalBody      = "`/%s` refused: unexpected argument %q — it takes %q, %q, or nothing (nothing toggles the mode)."
	voiceNoteNoEditorRefusalBody = "`/%s` needs the interactive line editor: this run reads its lines from the caller (piped, one-shot or %s), so there is no input line for a transcript to be appended to. " + voiceNoteNoEditorTail
	voiceNoteLiveFinishingBody   = "the utterance is already settling — the audio in hand is being sent; " +
		"the sentence will appear on the line when the engine answers."
	voiceNoteLiveFinishSentBody = "%s — the audio in hand is being sent to the engine; " +
		"the sentence appears on the line when it answers."
	// voiceNoteRecorderLineBody is the recorder's OWN notes (the optional `notes`
	// Select returns, e.g. a fallback device): the engine's voice, so the prefix is
	// the audio one. It replaces a bare "[voice] %s\n" literal at the call site
	// (Rule 1b).
	voiceNoteRecorderLineBody = "%s"
	// voiceNoteRefusalTail is the sentence every START refusal ends with. It is a
	// constant because it is a promise the three refusals must make in the same
	// words: nothing was recorded, and no half-open mode was left behind.
	voiceNoteRefusalTail = "Nothing was recorded and the mode is NOT on."
	// voiceNoteNoEditorTail is the closing sentence of the no-editor refusal. It is
	// spelled differently on purpose: that row refuses BEFORE a device is ever
	// touched, so "nothing was recorded" would be a sentence about a recording that
	// was never at stake.
	voiceNoteNoEditorTail = "The mode was NOT started."
)

// The composed rows. Named consts so that a test (or a doc) can refer to the row
// it means, and so the prefix policy is visible in one place for every row.
const (
	// chatVoiceLeftNote is the leaving row. ITS SECOND HALF IS THE REASON'S OWN —
	// the two-branch semantics chatVoiceLeftTails carries (a mode-driven exit vs a
	// capture that ended first) are untouched by this file: the pairing reaches the
	// renderer as an argument, exactly as it reached the template.
	chatVoiceLeftNote = voiceNotePrefixMode + voiceNoteLeftBody
	// chatVoiceErrorNote is the failure row that ends the mode.
	chatVoiceErrorNote = voiceNotePrefixMode + voiceNoteErrorBody
	// chatVoiceRoundRetryNote is the failure row that does NOT end the mode.
	chatVoiceRoundRetryNote = voiceNotePrefixMode + voiceNoteRetryBody
	// chatVoiceStatusOff is the OFF answer to `/voice status`.
	chatVoiceStatusOff = voiceNotePrefixMode + voiceNoteStatusOffBody
)

// voiceNoteEntered renders the entered block: the mode's claim, then WHAT is
// recording, WHAT is transcribing, HOW LONG a window is, and WHERE the text goes.
//
// The four facts are the requirement's own list (device / engine / where text
// goes, plus the window the operator will wait), and they are the reason the
// entered note is a block: the inline template packed all four into one
// 200-character sentence, where the device — the first thing an operator checks
// when a transcript is wrong — sat between two `;`.
func voiceNoteEntered(f voiceNoteEnteredFacts) string {
	return voiceNoteAuthoredBlock(voiceNotePrefixMode, voiceNoteHeadEntered,
		voiceNoteFact{voiceNoteLabelDevice, f.Device},
		voiceNoteFact{voiceNoteLabelEngine, f.Engine},
		voiceNoteFact{voiceNoteLabelWindow, fmt.Sprintf(voiceNoteWindowFormat, f.WindowSec)},
		voiceNoteFact{voiceNoteLabelText, fmt.Sprintf(voiceNoteTextBody, f.LeaveWord)},
	)
}

// voiceNoteEnteredFacts is the entered block's input. It exists so the renderer
// is a pure function of VALUES a test can write down, and so the REPL arm has
// nothing to decide — the same shape chatInfoFields gives /info's renderer.
type voiceNoteEnteredFacts struct {
	// Device is the recorder's description (what the operator is about to be
	// transcribed by).
	Device string
	// Engine is the resolved engine name.
	Engine string
	// WindowSec is the recording window bound (voiceChatWindowSeconds).
	WindowSec int
	// LeaveWord is the `/voice` argument that leaves the mode (chatVoiceStopWord,
	// the same word the status-ON row names). It is an ARGUMENT and not a fixed
	// part of this renderer, and that is exactly why the caller must pass the word
	// the dispatcher acts on: the renderer cannot check it, and the entered row
	// taught `status` — a word that PRINTS and stays — until task 01a1424f-4526
	// (see chat_voice_leaveword_test.go, which reads the word back out of this
	// block and dispatches it).
	LeaveWord string
}

// voiceNoteStatusOn renders the status-ON block: the same facts as the entered
// block — an operator who asks `/voice status` mid-dictation is asking exactly the
// questions the entered row answered, and answering with a different set would
// make "what does it say now" depend on when they asked.
func voiceNoteStatusOn(f voiceNoteStatusFacts) string {
	return voiceNoteAuthoredBlock(voiceNotePrefixMode, voiceNoteHeadStatusOn,
		voiceNoteFact{voiceNoteLabelDevice, f.Device},
		voiceNoteFact{voiceNoteLabelEngine, f.Engine},
		voiceNoteFact{voiceNoteLabelWindow, fmt.Sprintf(voiceNoteWindowFormat, f.WindowSec)},
		voiceNoteFact{voiceNoteLabelLeave, fmt.Sprintf(voiceNoteLeaveBody, f.LeaveWord)},
	)
}

// voiceNoteStatusFacts is the status-ON block's input (see voiceNoteEnteredFacts).
type voiceNoteStatusFacts struct {
	Device    string
	Engine    string
	WindowSec int
	// LeaveWord is the `/voice` argument that leaves the mode (chatVoiceStopWord
	// here: the status answer points at the word, not at the toggle's own name).
	LeaveWord string
}

// voiceNoteStatusOff renders the OFF answer. It is ONE FACT — the mode is off and
// here is the word that starts it — so it stays one row (the contract's rule 2).
func voiceNoteStatusOff() string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceStatusOff, chatVoiceCommandName))
}

// voiceNoteLeft renders the leaving row, carrying the reason and the tail PAIRED
// to it by the caller (chatVoiceLeftTail). The renderer neither knows nor decides
// which tail belongs to which reason: that pairing is a semantic rule with its own
// regression case (chat_voice_left_note_test.go), and moving it here would put the
// two-branch rule inside the shape decision where a formatting change could break
// it silently.
func voiceNoteLeft(reason, tail string) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceLeftNote, reason, tail))
}

// voiceNoteFailed renders the failure row that ends the mode.
func voiceNoteFailed(err error) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceErrorNote, err))
}

// voiceNoteRetry renders the failure row for a round the mode RETRIES, carrying
// both counts (this failure N of N, and how many more in a row would end it).
func voiceNoteRetry(run, limit int, err error, remaining int) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceRoundRetryNote, run, limit, err, remaining))
}

// voiceNoteDepsRefusal renders the refusal for a missing engine dependency. Like
// every refusal it is ONE sentence (the contract's rule 2), but it carries the
// mode's prefix: the row that refuses the command is the mode speaking.
func voiceNoteDepsRefusal(missing string) string {
	return voiceNoteAuthored(voiceNotePrefixMode, fmt.Sprintf(voiceNoteDepsRefusalBody, chatVoiceCommandName, missing))
}

// voiceNoteDeviceRefusal renders the refusal for a capture device that could not
// be reached.
func voiceNoteDeviceRefusal(err error) string {
	return voiceNoteAuthored(voiceNotePrefixMode, fmt.Sprintf(voiceNoteDeviceRefusalBody, chatVoiceCommandName, err))
}

// voiceNoteArgRefusal renders the refusal for an argument the word does not take;
// it names the argument and lists what the word DOES take.
func voiceNoteArgRefusal(arg string) string {
	return voiceNoteAuthored(voiceNotePrefixMode, fmt.Sprintf(voiceNoteArgRefusalBody,
		chatVoiceCommandName, arg, chatVoiceStatusWord, chatVoiceStopWord))
}

// voiceNoteNoEditorRefusal renders the refusal for a carrier with no editing line
// to append a transcript to (the cooked path).
func voiceNoteNoEditorRefusal() string {
	return voiceNoteAuthored(voiceNotePrefixMode, fmt.Sprintf(voiceNoteNoEditorRefusalBody,
		chatVoiceCommandName, chatFlagNoEditor))
}

// voiceNoteLiveFinishing renders the live arm's answer to a SECOND finish-key
// press: the utterance is already settling.
func voiceNoteLiveFinishing() string {
	return voiceNoteAuthored(voiceNotePrefixMode, voiceNoteLiveFinishingBody)
}

// voiceNoteLiveFinishSent renders the live arm's answer to the FIRST finish-key
// press, naming the key the operator used (the label comes from the arm, which is
// what teaches the operator which key they pressed).
func voiceNoteLiveFinishSent(key string) string {
	return voiceNoteAuthored(voiceNotePrefixMode, fmt.Sprintf(voiceNoteLiveFinishSentBody, key))
}

// voiceNoteRecorderLine renders one of the recorder's own notes.
func voiceNoteRecorderLine(note string) string {
	return voiceNoteShared(fmt.Sprintf(voiceNotePrefixVoice+voiceNoteRecorderLineBody, note))
}

// ---------------------------------------------------------------------------
// The engine's voice (`[voice]`): what was heard, and what the capture lost.
// ---------------------------------------------------------------------------

// The engine-voice row bodies. The single-row bodies keep their exact bytes; the
// prefix is concatenated from the ONE constant, so the audio rows cannot drift
// away from the policy.
const (
	voiceNoteTranscriptBody  = "%s"
	voiceNoteSilentBody      = "no text in the last %ds window (silence, or the speech fell outside it) — still listening."
	voiceNoteSplitSilentBody = "this window was cut inside speech after %d extension(s) of %ds and produced NO text — a word split across two windows can come back as silence; a short pause between phrases gives the cut a gap to land in. Still listening."
	voiceNoteWindowSplitBody = "window boundary cut inside speech after %d extension(s) of %ds — a word may be split across two windows; a short pause between phrases gives the cut a gap to land in."
	voiceNoteFinishShortBody = "the capture's last window(s) did not finish within %v — part of what you said just before leaving was not transcribed."
)

// The composed rows (see the mode-voice block above for why the prefix lives in
// the composition and the body lives in the body).
const (
	// chatVoiceTranscriptNote is the row that says a transcript arrived; the text
	// itself is on the editing line.
	chatVoiceTranscriptNote = voiceNotePrefixVoice + voiceNoteTranscriptBody
	// chatVoiceSilentNote is the round that produced no text.
	chatVoiceSilentNote = voiceNotePrefixVoice + voiceNoteSilentBody
	// chatVoiceSplitSilentNote is the round CUT INSIDE SPEECH that then produced
	// no text (the note that stops the two facts reading as a contradiction).
	chatVoiceSplitSilentNote = voiceNotePrefixVoice + voiceNoteSplitSilentBody
	// chatVoiceWindowSplitNote is the round whose boundary had to be taken inside
	// speech.
	chatVoiceWindowSplitNote = voiceNotePrefixVoice + voiceNoteWindowSplitBody
	// chatVoiceFinishShortNote is the finish that ran out of budget with audio in
	// hand.
	chatVoiceFinishShortNote = voiceNotePrefixVoice + voiceNoteFinishShortBody
)

// voiceNoteTranscript renders the row for ONE transcript as it lands. The text is
// passed through unchanged: it is the operator's own sentence, not a fact this
// renderer may reformat.
func voiceNoteTranscript(text string) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceTranscriptNote, text))
}

// voiceNoteSilent renders the round that produced no text.
func voiceNoteSilent(windowSec int) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceSilentNote, windowSec))
}

// voiceNoteSplitSilent renders the round that was cut inside speech AND produced
// no text, carrying the cut's own numbers (extensions, then window length).
func voiceNoteSplitSilent(extended, windowSec int) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceSplitSilentNote, extended, windowSec))
}

// voiceNoteWindowSplit renders the round whose boundary fell inside speech and
// which DID produce text, carrying the same two numbers in the same order.
func voiceNoteWindowSplit(extended, windowSec int) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceWindowSplitNote, extended, windowSec))
}

// voiceNoteFinishShort renders the finish that ran out of its budget, naming the
// budget it ran out of (voiceChatFinishWait).
func voiceNoteFinishShort(wait time.Duration) string {
	return voiceNoteShared(fmt.Sprintf(chatVoiceFinishShortNote, wait))
}

// voiceNoteEngineRow renders a row whose text ANOTHER OWNER composed (the engine
// provenance and privacy sentences the CLI prints too). It is the audio voice, and
// the owner already prefixed it — so this is voiceNoteShared with a name that says
// which policy applied, and the tests assert that no row ever carries two
// prefixes.
func voiceNoteEngineRow(text string) string { return voiceNoteShared(text) }

// ---------------------------------------------------------------------------
// The leaving row's two halves.
// ---------------------------------------------------------------------------

// chatVoiceLeftStoppingTail is what the note says when THE MODE is what ended the
// capture — the operator asked (the toggle, the stop word, Esc) or the engine
// refused the round. The recorder is still the mode's and is being stopped.
//
// chatVoiceLeftRanOutTail is what the note says when THE CAPTURE had already ended
// by itself: the sentinel reached a mode that was still ON, so the mode is taking
// itself down over a source that is gone rather than stopping a recording it
// controls. Claiming "the recorder is stopping" here would state the causality
// backwards — there is nothing left to stop. It states the CONSEQUENCE and leaves
// the CAUSE to the reason field beside it.
//
// THE PAIRING between a REASON and one of these halves is a semantic rule, not a
// shape decision, and it lives where it was fixed: chatVoiceLeftTails /
// chatVoiceLeftTail in chat_voice.go, with its own regression case
// (chat_voice_left_note_test.go) that drives all five exits and asserts the row
// this renderer produces carries the RIGHT half and not the other. The renderer
// receives the tail as an argument, so a change to this file's shape cannot
// silently change which of the two belongs to which reason.
const (
	chatVoiceLeftStoppingTail = "the recorder is stopping; the text already on the line is kept. No further transcript will be inserted."
	chatVoiceLeftRanOutTail   = "nothing is left to record; the text already on the line is kept. No further transcript will be inserted."
)
