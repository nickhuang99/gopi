package tools

// chat_approval_authorization.go — the CHANNEL through which a `prompt` decision
// in the chat write boundary becomes a RUN, and the reason it cannot become an
// always-open switch.
//
// THE DEADLOCK THIS CLOSES (measured, task 01a16885-6754). The boundary's three
// decisions were allow / prompt / forbidden, and only two of them had a
// destination. `allow` ran the call, `forbidden` refused it, and `prompt`
// returned text saying "the command did NOT run — ask the operator and retry only
// on a yes". But the retry IS the same rule, the same decision and the same text:
// nothing the operator could say changed the answer, so `prompt` was a permanent
// deadlock rather than a question. With the shipped default table (`prompt *`) and
// the shipped default posture (on-request) that made EVERY bash and write call
// dead — measurably: `gopi chat --once --tools` came back
//
//	[NEEDS OPERATOR APPROVAL — command NOT run] … matched rule `*`
//	(built-in chat-policy defaults:2)
//
// for a `write`, and the file never landed; the testsuite's two chat cases stayed
// red for the same reason.
//
// WHERE THE AUTHORIZATION COMES FROM — the SAME shape as the cleanup gate
// (chat_cleanup_authorization.go), deliberately reused rather than reinvented:
// the CURRENT TURN'S OPERATOR MESSAGE, tested by a pure function. Three
// properties fall out, and each is why the other designs were rejected:
//
//   - It cannot become an always-on switch. Nothing is stored: the guard
//     re-derives the answer from the turn's own text on every call
//     (internal/agent publishes the turn with publishChatTurn and restores it when
//     the turn ends), so there is no state to leave enabled, no key to forget, and
//     a turn that says nothing approves nothing.
//   - A previous turn's approval cannot leak into a later one: the message is the
//     input, and each turn carries its own.
//   - A settings key or an env var WOULD be a persistent dependency (Rule 35)
//     invented to make one sentence unnecessary — the operator writes the
//     sentence instead.
//
// WHAT IT CANNOT DO, stated rather than hidden (Rule 15):
//
//   - It never opens a `forbidden` decision. The caller asks this predicate ONLY
//     on the prompt branch; a forbidden rule is not a question and this file has
//     no spelling that could reach it.
//   - It cannot distinguish WHICH question was approved. The boundary holds no
//     memory of what it asked, so an affirmative message approves the prompt-
//     decision it meets in that turn. That is bounded by what a prompt decision
//     already is — a command the operator's own rules marked "ask me first" — and
//     the approval text tells the model to retry the SAME command, never a
//     re-spelt one. A correlation would need state, which is the debt this whole
//     channel exists to avoid.
//   - The vocabulary is crude on purpose (a seatbelt, not a parser): a paraphrase
//     outside it is a false negative that leaves the operator able to authorize on
//     the next turn by typing one of its words, and the approval text prints what
//     those words are.

import "strings"

// chatToolApprovalPhrases are the operator's own words for "yes, do it" — the
// affirmative vocabulary that opens a prompt decision for THIS turn. Both
// languages the console is used in, because the message is the operator's and the
// operator writes either.
//
// The list is deliberately SHORT and its entries are the forms an operator types
// as an ANSWER. It is not a sentiment classifier: a sentence that merely contains
// no discouragement is not an approval, and the ASCII entries are matched on a
// WORD BOUNDARY (see chatHasApprovalPhrase) precisely so `token`, `look` and
// `broke` cannot answer a question by accident.
var chatToolApprovalPhrases = []string{
	"yes", "yep", "yeah", "sure", "ok", "okay", "approved", "approve", "proceed",
	"go ahead", "go for it", "do it", "run it", "go on",
	"可以", "同意", "批准", "没问题", "跑吧", "执行吧", "干吧", "就这样",
	// The operator's own words for "you have my authorisation, stop asking"
	// (measured 2026-10-28: two consecutive turns said 「不要请示，直接做」 and
	// 「你自己做，不要请示」 about a `git push`, and EVERY one of those forms was
	// outside this table — so a `prompt` decision stayed refused while the
	// operator believed they had already authorised it, and the console replied
	// by explaining the boundary back to them. An approval vocabulary that does
	// not carry the clearest thing an operator can say is not crude, it is
	// wrong: it turns "do it" into a third round of asking.
	"直接做", "自己做", "不要请示", "不用请示", "别请示", "直接干", "赶紧做",
	// The bare directive forms: 「快去」/「去做」/「执行」answer a question with an ORDER
	// rather than an affirmative word, and an operator in a hurry writes the order
	// (measured 2026-10-28: 「快去！！！！」 was the third turn spent on the same
	// `git push`). 「去」 alone is deliberately NOT in the table — it is a substring
	// of 「去掉」(remove) and of 「过去」, so a two-character floor is what keeps this
	// from authorizing a deletion.
	"快去", "去做", "执行", "动手",
}

// chatTurnNegationMarkers are the markers that flip an AFFIRMATIVE into a
// refusal. They are the cleanup gate's own list, referenced rather than re-typed
// (Rule 13/14: one definition, two readers) — "do not run it" must not authorize
// a command merely by containing "run it".
var chatTurnNegationMarkers = chatCleanupNegations

// ChatTurnApprovesTheToolQuestion reports whether THIS turn's operator message
// approved a `prompt` decision the boundary raised. The message is the operator's
// own words and it is passed in rather than read from anywhere: a function of its
// input cannot hold a switch open.
//
// A negation anywhere in the message wins over an affirmative: "don't run it" and
// "不要执行" both contain an approval word, and reading either as consent is the
// one failure this gate exists to be safe against.
func ChatTurnApprovesTheToolQuestion(userInput string) bool {
	msg := strings.ToLower(strings.TrimSpace(userInput))
	if msg == "" {
		return false
	}
	// A NEGATED INSTRUCTION IS STILL AN INSTRUCTION. 「不要请示」 and 「别问了，直接做」
	// contain a negation MARKER (不要 / 别) and an approval PHRASE at the same time,
	// so the plain "a marker anywhere refuses" rule rejected them — measured
	// 2026-10-28: two consecutive operator turns saying exactly that about a
	// `git push` were read as refusals, and the console asked a third time.
	// The markers exist to stop "don't run it" authorizing `run it`; a marker
	// that is part of the APPROVAL WORD ITSELF ("不要请示" = stop asking) is not
	// that shape, so the approval phrase is matched FIRST and a marker only
	// vetoes when it stands OUTSIDE the phrase that matched.
	for _, phrase := range chatToolApprovalPhrases {
		if !chatHasApprovalPhrase(msg, phrase) {
			continue
		}
		if !chatNegationVetoesApproval(msg, phrase) {
			return true
		}
	}
	return false
}

// chatNegationVetoesApproval reports whether a negation marker appears in the
// message OUTSIDE the approval phrase that matched. "don't run it" → the marker
// is outside `run it`, so it vetoes; 「不要请示」 → the only `不要` IS the phrase,
// so nothing vetoes.
func chatNegationVetoesApproval(msg, phrase string) bool {
	i := strings.Index(msg, phrase)
	if i < 0 {
		return true
	}
	outside := msg[:i] + " " + msg[i+len(phrase):]
	return chatHasAny(outside, chatTurnNegationMarkers)
}

// chatHasApprovalPhrase reports whether the (lowercased) message carries one
// approval word.
//
// ASCII WORDS MATCH ON A BOUNDARY; CJK WORDS MATCH AS SUBSTRINGS. The asymmetry is
// the languages', not a preference: an English approval is a WORD ("ok" is an
// answer, "token" is not), while a Chinese one is a run of characters inside a
// sentence with no delimiter to anchor on ("可以，跑吧" has no spaces at all). So a
// phrase that begins and ends with an ASCII letter is checked for non-word runes
// on both sides, and every other phrase is a plain substring.
func chatHasApprovalPhrase(msg, phrase string) bool {
	if phrase == "" {
		return false
	}
	if !chatPhraseIsASCIIWord(phrase) {
		return strings.Contains(msg, phrase)
	}
	for from := 0; ; {
		i := strings.Index(msg[from:], phrase)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(phrase)
		if chatBoundaryAt(msg, start-1) && chatBoundaryAt(msg, end) {
			return true
		}
		from = start + 1
	}
}

// chatPhraseIsASCIIWord reports whether the phrase is bounded by ASCII letters —
// the test for "this phrase is an English word and needs a word boundary".
func chatPhraseIsASCIIWord(phrase string) bool {
	return chatIsASCIILetter(phrase[0]) && chatIsASCIILetter(phrase[len(phrase)-1])
}

// chatBoundaryAt reports whether the byte at i is a word boundary: off the
// message (the ends are boundaries), or a byte that cannot continue an English
// word.
func chatBoundaryAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	return !chatIsASCIILetter(c) && !(c >= '0' && c <= '9') && c != '_'
}

// chatIsASCIILetter is the one spelling of "is this byte part of an English word"
// this file needs (Rule 1: the classification is a predicate, not an inline
// comparison at two sites).
func chatIsASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
