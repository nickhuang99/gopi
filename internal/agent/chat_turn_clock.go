package agent

// chat_turn_clock.go — the CHAT prompt's sentence about the PER-REQUEST time
// stamp, and the block it rides in.
//
// WHY IT IS A CHAT-ONLY BLOCK (Rule: do not pollute the task prompt). The task
// loop already re-reads the tick every round because every tool result carries
// it (withTick), and it carries its own planning ceiling in clockRule(). A chat
// turn is the ONE surface where a request can arrive with no tool call at all —
// so the stamp's meaning has to be taught where the stamp is actually the only
// time signal, and NOWHERE ELSE: a task run told "each request carries your tick
// and the gap since your last one" would be reading a second, differently
// spelled version of a rule it already has.
//
// It rides on BOTH chat paths (chatOpsSystemPrompt and
// assembleChatSystemPrompt), because both carry the chat-context block and both
// receive the stamp. It is not parameterised by tools: the stamp arrives whether
// tools are on the wire or not, so the sentence is true of both limbs verbatim.

const (
	// chatTurnClockMarker opens the block and is the string a test keys on. The
	// marker is separated from the body so "is the block present at all" is a
	// one-substring question.
	chatTurnClockMarker = "[CHAT CLOCK — every request carries the current tick]"

	// chatTurnClockRulesText is the block's body. It names the tick shape through
	// tickShapeLabel (Rule 1/13 — the tick's spelling has ONE definition, in
	// clock.go, and the prompt here is one of its readers) and states the ONE
	// behaviour the stamp exists to change: a time assumption from an earlier
	// request is not carried forward without being re-checked against the gap.
	chatTurnClockRulesText = chatTurnClockMarker +
		" Every request you receive ends with the current session tick " + tickShapeLabel +
		" and, after the first, how long passed since your previous request (+Ns)." +
		" Time passes between operator messages whether or not you were asked anything," +
		" so read that stamp before repeating an earlier time assumption" +
		" (\"I just ran the tests\") — the gap tells you whether it still holds." +
		" The tick is ALSO followed by the ABSOLUTE wall-clock time of that request" +
		" (ISO-8601 to the second, with its zone offset), which is the reading to use" +
		" when you need a date or an hour of the day rather than an interval:" +
		" the tick alone cannot tell you what day it is, and the tick restarts at" +
		" zero in a resumed session while the wall-clock time does not."
)

// chatTurnClockRules renders the block. It takes no argument and builds no
// runtime value (unlike the chat-context block, which reads the process's host
// and repo): the sentence is the same bytes on every chat request of every run,
// which is exactly what keeps it out of the way of the prompt cache.
func chatTurnClockRules() string { return chatTurnClockRulesText }
