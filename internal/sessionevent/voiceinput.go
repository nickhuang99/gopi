package sessionevent

// voiceinput.go — THE ONE LINE THAT TELLS THE MODEL WHERE ITS INPUT CAME FROM
// when the user's message contains characters a `/voice` transcript produced.
//
// WHY THE FACT IS NEEDED AT ALL. A dictated sentence and a typed one arrive at
// the model BYTE-IDENTICAL: `/voice` appends its transcript into the editing line
// (cmd/agent/chat_voice.go), the operator presses Enter, and nothing downstream
// can tell the two apart. The information exists while the transcript is being
// inserted and is gone by the time the request is built. This constant is the
// sentence that carries it back, prepended to the USER message by the harness —
// never to the system prompt, which is a cached prefix: a per-turn fact placed
// there would break the cache on every turn and would sit maximally far from the
// text it qualifies.
//
// THE WORDING DESCRIBES THE SOURCE, NEVER THE QUALITY, and that is the design
// decision this file exists to protect. Writing "this text may contain speech
// recognition errors" would INSTRUCT the model to hunt for mistakes, and a model
// told to hunt will "correct" correct words into wrong ones (the failure the
// operator named: 「预约」 silently becoming 「预压」). What the model needs is the
// provenance — "these are the characters a recogniser produced from speech" —
// and then to decide for itself what that is worth. So the sentence says
// DICTATED, TRANSCRIPT, NOT TYPED, and stops there.
//
// THE PREDICATE IS A COUNT, NOT A FLAG. The carrier measures how many runes on
// the submitted line still carry dictation provenance
// (internal/lineedit/valuevoice.go) and passes that count here. "Voice mode was
// open this turn" is the wrong question: an operator who dictates a sentence,
// deletes it and types their own is submitting pure typing, and telling the
// model otherwise would be a claim about text that is not there. Because the
// buffer's own mutations maintain the bits, the count falls to zero by the
// ordinary act of editing — no rule of this package's own has to predict it.
//
// Rule 14: the value lives in this leaf so its WRITER (cmd/agent, which
// assembles the outbound message) and any READER of a recorded row reference
// the same bytes instead of typing them again.
//
// IT IS DELIBERATELY NOT A HarnessMessageMarkers ENTRY, and that is the one
// classification mistake this constant could cause. That list answers "is this
// row the FRAMEWORK talking?" — and a row carrying this prefix is not: it is the
// OPERATOR's own input, with one line of provenance above it. Listing it there
// would make IsHarnessInjected say yes, IsTurnOpeningRow fall through to its
// framework leg and answer "no turn", `session log --turns` lose the turn, and
// the terminal transcript drop the operator's own message from the page
// (cmd/agent/term.go). The prefix is a CARRIER of provenance, not a carrier of
// chatter.

const (
	// VoiceInputPrefix is prepended to a user message whose text carries
	// dictation provenance. It is ONE line, in the same bracketed form the
	// framework's other user-row carriers use, so a reader scanning the log
	// recognises it as the harness speaking about the text rather than as the
	// operator's own opening words.
	VoiceInputPrefix = "[voice input] The text below was dictated: it is a speech-recognition transcript of what the user said, not characters they typed. Read it as the user's own words."

	// VoiceInputSeparator separates that line from the message itself. Two
	// newlines, because the prefix is its own paragraph: the model must not read
	// it as part of the first sentence the operator said.
	VoiceInputSeparator = "\n\n"
)

// VoiceInputPrefixFor returns the bytes to prepend to a user message that holds
// voiceRunes dictated characters: the prefix plus its separator, or "" when the
// line carries none.
//
// A COUNT OF ZERO TAKES THE EMPTY STRING, and that is the whole delete-and-retype
// case stated as code: the caller does not have to remember to check, and a
// negative or zero count can never produce a marker. The function is TOTAL over
// the counts a buffer can report, so no caller needs a second guard that could
// drift from this one.
func VoiceInputPrefixFor(voiceRunes int) string {
	if voiceRunes <= 0 {
		return ""
	}
	return VoiceInputPrefix + VoiceInputSeparator
}
