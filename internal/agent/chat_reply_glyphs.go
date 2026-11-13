package agent

// chat_reply_glyphs.go — the chat block's rule about the CHARACTERS the answer
// body may be built from, in one case only: the ellipsis.
//
// WHY IT EXISTS (the measured defect). A chat answer is not read on this screen
// alone — it is COPIED OUT of it. The operator selects a line and pastes it into
// a shell, an editor or a task briefing, and the screen cell is the deliverable
// (the same premise chat_context.go states: "the ANSWER BODY is the artifact").
// The ellipsis was measured to be the MODEL'S OWN character in the reply body,
// not a mark the renderer draws: `internal/lineedit`'s note-wrap arm paints no
// marker at all, and its `statusEllipsis` belongs to the status row and to
// field truncation, never to the answer. So nothing downstream can repair it —
// a body that reads
//
//	go test ./internal/agent/... -run TestChatPromptForbids…
//
// hands the operator an argument no shell will accept, and the surrounding
// prose ("…and the rest of the files") reads as a complete instruction while
// naming half of one. The rule has to be stated where the model reads it: the
// chat system prompt.
//
// WHY THE CHAT BLOCK AND NOT THE TASK KERNEL. The two surfaces answer to
// different budgets. The task kernel is bounded by systemPromptDensityBudget
// (4500 BYTES, CJK at 3 bytes per rune — memory #6373/#6375) and paid for by
// every worker request; the chat block is assembled per turn by
// chatContextRulesFor and rides on BOTH chat paths (assembleChatSystemPrompt,
// chatOpsSystemPrompt) without entering that budget at all. A chat-only rule
// about a chat-only artifact belongs in the chat-only block, which is the same
// ruling chat_memory_timing.go and chat_write_boundary.go already follow (Rule
// 3's spirit: do not grow the shared worker-facing surface for one surface's
// concern).
//
// WHY ONE LIMB AND NOT TWO. The other chat paragraphs carry a tools / no-tools
// pair because what they instruct ("run it", "call the memory tool") is only
// possible on the tool-enabled path. This paragraph instructs NOTHING that needs
// a tool — how to write the text of an answer is a property of the answer on
// every path — so a second limb would be the same bytes twice.
//
// RULE 3/35: no new tool, no new dependency, no state file, no new config key.
// The paragraph is composed at assembly time from the constants below and is
// appended to a block that is already rendered.
//
// THE ONE TYPED GLYPH. The forbidden character appears once, literally, inside
// chatReplyGlyphRulesText — it is the character the model has to RECOGNISE, so
// a spelling that only existed as an escape would be a rule the reader cannot
// match against what it just wrote. It is prose, never a compared value: nothing
// in this package branches on it (`digestEllipsis` in chat_ops.go marks a
// digest a cap cut — a different job, and the renderer's own business).

const (
	// chatReplyGlyphMarker opens the paragraph and is the string a test keys on
	// when the question is "was this run told how to write its answer body?".
	// Separated from the body so PRESENCE is a one-substring question.
	chatReplyGlyphMarker = "[CHAT REPLY GLYPHS — screen cells are the deliverable]"

	// chatReplyGlyphRulesText is the paragraph. Two sentences, and the second is
	// the REASON rather than a second prohibition: a rule stated with its
	// mechanism survives being paraphrased by a reader, and the mechanism here
	// (the cell is copied out and executed as typed) is what makes the first
	// sentence checkable by the model itself before it finishes a sentence.
	chatReplyGlyphRulesText = chatReplyGlyphMarker +
		" Every character of an answer body is copied straight out of this grid and used as typed, " +
		"so never elide anything with an ellipsis (`…`, `...`): write the path, command, URL or quote in FULL, " +
		"or name what you left out and why — half a path stopped by `…` is invalid input the moment it is pasted."
)

// chatReplyGlyphRules returns the paragraph. It takes no argument and reads no
// runtime state: the sentence is the same bytes on every chat request of every
// run, which is what keeps it out of the way of the prompt cache's prefix (the
// same reason chatTurnClockRules builds nothing).
func chatReplyGlyphRules() string { return chatReplyGlyphRulesText }
