package main

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// chat_voice_input.go — WHERE THE VOICE-INPUT PREFIX IS COMPOSED, and why it is
// composed HERE.
//
// THE FACT THIS CARRIES. A dictated line and a typed line reach the model
// byte-identical: `/voice` appends its transcript into the editing line, the
// operator presses Enter, and nothing downstream can tell where the characters
// came from. So the TURN's message — the one the boundary has already sealed and
// the prefetch has already been adopted into, i.e. the exact bytes that will be
// asked and recorded — gains ONE line of provenance when, and only when, that
// message still contains dictated characters.
//
// WHY THE USER MESSAGE AND NOT THE SYSTEM PROMPT. The system prompt is a cached
// prefix: a per-turn fact placed there would break the cache on every turn and
// would sit maximally far from the text it qualifies. The user message is where
// the text IS, it costs one line, and it is per-turn by construction.
//
// THE PREDICATE IS A MEASUREMENT OF THE SUBMITTED LINE, never of the mode. The
// count comes from the editing buffer's own provenance bits
// (internal/lineedit/valuevoice.go), which the ordinary mutations maintain, so an
// operator who dictates, deletes and retypes submits a line with zero dictated
// characters and therefore carries no prefix — without this file having a rule of
// its own about what "deleted" means.
//
// THE SPILL INTERACTION, stated because it is the one path where the prefix and
// an existing framing meet. A line too large for the chat boundary is billed to a
// file and the message becomes a reference ("[gopi message boundary] …") with the
// payload spilled. The prefix still goes FIRST and the reference follows it, so
// the model reads "this turn was dictated" and then "the payload is that file"
// — the two facts are independent and both are true. What the prefix can no
// longer do on that path is quote the size of the dictated text, which is why it
// does not quote a size at all.

// chatVoiceInputPrefix returns the provenance line a turn's message must open
// with, or "" when the line carries no dictated characters.
//
// IT TAKES THE COUNT rather than the carrier, so the decision is a pure function
// a test can drive without building an editor — and so no caller can reach for
// the voice mode's state instead of the buffer's. The prefix text itself is
// sessionevent.VoiceInputPrefix (Rule 14: one definition, referenced, never
// mirrored).
func chatVoiceInputPrefix(voiceRunes int) string {
	return sessionevent.VoiceInputPrefixFor(voiceRunes)
}

// chatVoiceMessage adds that prefix to a turn's outbound message, and reports
// whether it added anything.
//
// IT IS APPLIED TO Message AND NEVER TO Display: Display is the operator's own
// line, drawn on their screen, and the harness's sentence about provenance is for
// the model — putting it in Display would show the operator words they did not
// type, which is the same class of lie as the marker being absent in the first
// place.
//
// AN EMPTY MESSAGE TAKES NOTHING: a turn with no text (a notice turn whose notice
// list was empty) is not a dictated user message, and a prefix floating above
// nothing would be a claim about text that does not exist.
func chatVoiceMessage(out chatOutbound, voiceRunes int) (chatOutbound, bool) {
	prefix := chatVoiceInputPrefix(voiceRunes)
	if prefix == "" || strings.TrimSpace(out.Message) == "" {
		return out, false
	}
	out.Message = prefix + out.Message
	return out, true
}
