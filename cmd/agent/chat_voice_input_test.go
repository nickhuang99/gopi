package main

// chat_voice_input_test.go — the voice-input prefix at the CARRIER: WHEN it is
// composed, WHAT it says, and the three states the requirement names.
//
// The buffer-level bookkeeping is pinned in internal/lineedit
// (valuevoice_test.go); what is proved HERE is that the turn's message is what
// carries the line, that the predicate is the submitted line's own count rather
// than the voice mode's state, and that the wording describes PROVENANCE. The
// end-to-end proof that the text reaches a real request's `messages` array is the
// Rule 31 run in the task report (a real `/voice` round plus a typing-only round,
// each read back out of the session's raw.log).

import (
	"io"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sessionevent"
)

// chatVoiceTestInput builds an editor-backed chatInput over an inert source, so a
// case can drive the SAME insert road production takes (/voice → VoiceInsertLine)
// without a terminal.
func chatVoiceTestInput() *chatInput {
	return newChatInputWithSource(&scriptSource{}, &recordingWriter{}, chatPrompt)
}

// voiceTestOutbound is the boundary's zero-value message: the prefix composition
// is a property of the message alone, so the rest of the outbound struct is not
// part of these cases.
func voiceTestOutbound(message string) chatOutbound {
	return chatOutbound{Message: message, Display: message}
}

// TestVoiceInputPrefix_IsTheFirstBytesOfTheMessage is the shape the requirement
// names: ONE line, at the FRONT of the user message — the model must read it
// before the text it qualifies.
func TestVoiceInputPrefix_IsTheFirstBytesOfTheMessage(t *testing.T) {
	out, added := chatVoiceMessage(voiceTestOutbound("deploy the staging box"), 5)
	if !added {
		t.Fatal("chatVoiceMessage added nothing to a message that carries dictated characters")
	}
	if !strings.HasPrefix(out.Message, sessionevent.VoiceInputPrefix) {
		t.Fatalf("the message does not open with the provenance line; got %q", out.Message)
	}
	if !strings.HasSuffix(out.Message, "deploy the staging box") {
		t.Fatalf("the operator's own text is not the tail of the message; got %q", out.Message)
	}
	if strings.Count(out.Message, "\n") < 2 {
		t.Fatalf("the prefix is not its own paragraph; got %q", out.Message)
	}
	// Display is the operator's screen: the harness's sentence about provenance
	// must never appear there.
	if out.Display != "deploy the staging box" {
		t.Fatalf("Display = %q, want the operator's own line unchanged — the prefix is for the model, not the screen", out.Display)
	}
}

// TestVoiceInputPrefix_AbsentWithoutDictatedCharacters is the typing-only state,
// and it is the half that keeps the feature honest: with no dictated characters
// the message is BYTE-IDENTICAL to what the operator typed.
func TestVoiceInputPrefix_AbsentWithoutDictatedCharacters(t *testing.T) {
	const typed = "deploy the staging box"
	out, added := chatVoiceMessage(voiceTestOutbound(typed), 0)
	if added {
		t.Fatal("chatVoiceMessage added a prefix to a line with no dictated characters")
	}
	if out.Message != typed {
		t.Fatalf("the message reads %q, want it byte-identical to the typed line %q", out.Message, typed)
	}
}

// TestVoiceInputPrefix_AbsentAfterDeleteAndRetypeAtTheCarrier walks the WHOLE
// requirement through the carrier: a transcript is inserted by the voice road,
// the operator clears it and types their own, and the count the REPL's feeder
// reads off the line is zero.
//
// IT IS DRIVEN THROUGH chatInput.VoiceInsertLine — the exact call
// cmd/agent/chat_voice.go makes — so the case fails if the voice mode is ever
// wired to the ordinary append road again (in which case the line would carry no
// provenance at all, and the OTHER half of this feature would silently stop
// working while every typing case stayed green).
func TestVoiceInputPrefix_AbsentAfterDeleteAndRetypeAtTheCarrier(t *testing.T) {
	in := chatVoiceTestInput()
	if !in.VoiceInsertLine("转写文本") {
		t.Fatal("the voice insert road refused the transcript")
	}
	if got, want := in.VoiceRunes(), len([]rune("转写文本")); got != want {
		t.Fatalf("VoiceRunes = %d right after the transcript landed, want %d — VoiceInsertLine is not the dictation road", got, want)
	}
	// The operator deletes what they said (Ctrl+U) and types their own message.
	in.editor.Value().KillToStart()
	if in.editor.Value().InsertString("typed instead") > 0 {
		t.Fatal("the retyped text did not fit")
	}
	if got := in.VoiceRunes(); got != 0 {
		t.Fatalf("VoiceRunes = %d on a dictated-then-retyped line, want 0", got)
	}
	out, added := chatVoiceMessage(voiceTestOutbound(in.editor.Value().Text()), in.VoiceRunes())
	if added {
		t.Fatal("the dictated-then-retyped line was marked as dictated — the predicate is not the submitted line's own content")
	}
	if out.Message != "typed instead" {
		t.Fatalf("the message reads %q, want %q", out.Message, "typed instead")
	}
}

// TestVoiceInputPrefix_SurvivesAPartialEdit is the middle state, which is why the
// predicate is a count: some of the transcript is still on the line, so the prefix
// belongs there.
func TestVoiceInputPrefix_SurvivesAPartialEdit(t *testing.T) {
	in := chatVoiceTestInput()
	if !in.VoiceInsertLine("转写文本") {
		t.Fatal("the voice insert road refused the transcript")
	}
	in.editor.Value().Backspace()
	if got := in.VoiceRunes(); got == 0 {
		t.Fatal("VoiceRunes = 0 after deleting ONE dictated rune out of four — a partial transcript still carries dictated characters")
	}
	if _, added := chatVoiceMessage(voiceTestOutbound("转写文"), in.VoiceRunes()); !added {
		t.Fatal("the prefix is missing on a line that still holds dictated characters")
	}
}

// TestVoiceInputPrefix_CookedRunAnswersZero pins the no-editor carrier: there is
// no line to measure, and "no dictated characters" is the honest reading rather
// than a guess.
func TestVoiceInputPrefix_CookedRunAnswersZero(t *testing.T) {
	in, closeCooked := newChatInputCooked(strings.NewReader(""), io.Discard)
	defer closeCooked()
	if got := in.VoiceRunes(); got != 0 {
		t.Fatalf("a cooked run reported %d dictated runes, want 0 — there is no editing line to measure", got)
	}
	var nilInput *chatInput
	if got := nilInput.VoiceRunes(); got != 0 {
		t.Fatalf("a nil input reported %d dictated runes, want 0", got)
	}
}

// TestVoiceInputPrefix_EmptyMessageTakesNothing keeps the prefix off a turn that
// carries no text at all (an empty notice turn): a provenance line above nothing
// would be a claim about text that does not exist.
func TestVoiceInputPrefix_EmptyMessageTakesNothing(t *testing.T) {
	if _, added := chatVoiceMessage(voiceTestOutbound("   \n "), 7); added {
		t.Fatal("a message with no text received the provenance prefix")
	}
}

// TestVoiceInputPrefixDescribesProvenanceNotQuality is the WORDING gate, and it
// is the half of this feature most easily done wrong: telling the model the text
// "may contain speech recognition errors" instructs it to hunt for mistakes, and
// a model told to hunt will correct correct words into wrong ones.
//
// The forbidden vocabulary is spelled out here rather than left to review, so a
// later "helpful" rewording fails a test instead of shipping.
func TestVoiceInputPrefixDescribesProvenanceNotQuality(t *testing.T) {
	lower := strings.ToLower(sessionevent.VoiceInputPrefix)
	mustSay := []string{"dictat", "transcript"}
	for _, want := range mustSay {
		if !strings.Contains(lower, want) {
			t.Errorf("the prefix does not name the SOURCE (%q is missing): %q", want, sessionevent.VoiceInputPrefix)
		}
	}
	mustNotSay := []string{"error", "mistake", "wrong", "incorrect", "typo", "fix", "correct the"}
	for _, banned := range mustNotSay {
		if strings.Contains(lower, banned) {
			t.Errorf("the prefix contains %q, which judges the text's QUALITY — the model must be told where the characters came from, not instructed to look for faults: %q",
				banned, sessionevent.VoiceInputPrefix)
		}
	}
	// ONE line, as the requirement states: the prefix is a sentence, not a
	// paragraph that buries the operator's own words.
	if strings.Contains(sessionevent.VoiceInputPrefix, "\n") {
		t.Errorf("the prefix spans more than one line: %q", sessionevent.VoiceInputPrefix)
	}
}

// TestVoiceInputPrefix_IsNotHarnessChatter pins the classification the prefix
// must NOT fall into. It is the operator's own message with a line of provenance
// above it, so a reader that treats the harness's marker set as "not the
// operator's words" must not swallow it: doing so would drop the turn from
// `session log --turns` and the message from the terminal transcript.
func TestVoiceInputPrefix_IsNotHarnessChatter(t *testing.T) {
	body := chatVoiceInputPrefix(1) + "deploy the staging box"
	if sessionevent.IsHarnessInjected(body) {
		t.Fatalf("the prefixed message is classified as harness chatter; a turn carrying it would stop counting as the operator's: %q", body)
	}
	if !sessionevent.IsTurnOpeningRow(body, "human") {
		t.Fatalf("a prefixed human row does not open a turn: %q", body)
	}
}

// lineeditVoiceRunes is the seam this carrier's count comes from, referenced so a
// rename of the buffer's own reader fails HERE rather than only in production.
var lineeditVoiceRunes = (*lineedit.Value).VoiceRunes
