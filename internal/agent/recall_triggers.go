package agent

// recall_triggers.go — the TWO prompt surfaces that turn a VAGUE HUMAN RECALL
// PHRASE ("你还记得我们上次…"、"你忘了…"、"我们之前讨论过…"、"do you remember …",
// "we discussed this before") into a memory(action=search) call the model issues
// ITSELF, with a query it COMPOSED rather than the sentence it was handed.
//
// THE MEASURED DEFECT. Recall is opt-in in gopi: nothing auto-injects a
// retrieval into a conversational turn (the typing-time prefetch is a bonus leg,
// not a guarantee, and it is off on the one-shot face). So when the operator
// asked a question that only the LEDGER could answer, the model answered from
// the context window — fluently, and wrong — or asked the operator to restate
// what gopi already had on disk. The ledger held the answer; nothing told the
// model that a human's "你还记得…" is a RETRIEVAL REQUEST.
//
// WHY WORDS AND NOT A PARSER. There is no command to type and no slash command
// to remember: the trigger is the human's own phrasing, and the judgement —
// "is this a recall request, or ordinary chatter?" — is a SEMANTIC one that only
// the model can make. A regex over 你还记得 would fire on a joke, on a
// quotation, and on the operator asking whether the MODEL remembers something it
// was never told. So the mechanism is language, stated on the two surfaces the
// model actually reads before it acts (Rule 3/35: no new tool, no parser, no
// resident process, no config key):
//
//	1. the `memory` tool's search ACTION DESCRIPTION (memorySearchBullet, below)
//	   — the trigger vocabulary, the two sources one call covers, and the
//	   anti-abuse half, on the surface that is re-read on every request;
//	2. the CHAT block's own limb (chatRecallRules) — the same triggers plus the
//	   RATIONALE a console needs and a worker does not: asking the operator to
//	   repeat themselves is a failure when the answer is one call away.
//
// WHY loop.go DOES NOT GROW. loop.go is the file 167 devdoc documents cite by
// line number, and every inserted line re-keys the anchors below it. So the
// search bullet's TEXT lives here and loop.go keeps ONE line that concatenates
// this constant — the description is identical on the wire, the file's line
// count is unchanged, and the text has a home a reader can find without
// scrolling through the tool registry.
//
// RULE 1/13/14: every tool and action name is composed from internal/toolnames
// (a rename fails to compile here instead of leaving the description teaching a
// verb the schema rejects). The Chinese and English trigger phrases ARE the
// literals this file exists to state — they are the model's input vocabulary,
// not identifiers.

import (
	"strings"

	"github.com/nick/gopi/internal/toolnames"
)

// memoryRecallTriggers is the RECALL-TRIGGER clause of the search action's
// description: the phrases a human uses when they want something remembered, the
// instruction that the QUERY must be composed rather than echoed, and the two
// sources one call covers.
//
// IT NAMES EXACTLY THE TWO SOURCES AN ACTION=SEARCH RETURNS TODAY — the
// distilled lesson ledger and the procedural workflow table (both merged inside
// tools.MemorySearch, so one call really does cover both). It deliberately does
// NOT promise the round-level note index or a raw session log: that retrieval is
// a different row's work, and a description that advertised it would be an
// instruction the call cannot honour (Rule 15 — the promise is the contract).
const memoryRecallTriggers = "RECALL TRIGGERS — a vague human recall phrase is a RETRIEVAL REQUEST, and the QUERY is YOURS to " +
	"translate: 「你还记得我们上次…」「你忘了…」「我们之前讨论过…」「上次那个…」「之前遇到过的…」「之前不是说好的…」 / " +
	"\"do you remember …\", \"you forgot …\", \"we discussed this before\", \"last time …\", \"that thing we did …\" ⇒ " +
	"call this action, and compose the query from the TOPIC the sentence is about (the symptom, the entity, the decision, " +
	"the file, the error text) — NEVER echo the raw sentence back as the query: the human's wording is a request to " +
	"REMEMBER, not a search key. ONE call covers TWO sources: distilled lessons (success paths, pitfalls, decisions) and " +
	"procedural workflows (reusable step lists). "

// memoryRecallNotAtrigger is the ANTI-ABUSE half, and it is not optional: a
// trigger list with no floor turns a console into a search-every-round machine,
// which costs a full round trip per turn and returns noise the model then has to
// reason past. Two shapes are refused BY NAME — a recall phrase with no
// searchable handle (a greeting, "还记得我吗", a joke) and a SECOND search of the
// same topic inside one turn — because those are the two the model actually
// reaches for.
const memoryRecallNotATrigger = "NOT a trigger: a recall phrase carrying NO searchable handle (\"还记得我吗\", a greeting, a joke), " +
	"ordinary chatter, and a SECOND search of the same topic in one turn — one clarifying question beats a fishing " +
	"expedition. "

// memorySearchBullet is the whole body of the memory tool's search bullet: the
// pre-existing strategy text (when to use, how to judge a hit, the feedback
// discipline, the empty-result route, the ranking) with the recall triggers
// folded in. loop.go renders it as `- action=<search> — ` + this, so the wire
// text is unchanged in every respect except the two clauses above.
var memorySearchBullet = "READ-ONLY search, parallel-safe. Search gopi's distilled knowledge base for PAST experience: " +
	"success paths, pitfalls, lessons. " + memoryRecallTriggers +
	"USE FIRST at the start of every task, when stuck, or when the task looks done before — a known recipe beats " +
	"re-deriving it (5 分钟 vs 重造轮子). And right after you commit a subtask (task(action=" + toolnames.ToolTaskCommit + ")): " +
	"the next subtask is about to start, and it may be something you already solved — one search there is free; " +
	"re-deriving a known recipe is not. " + memoryRecallNotATrigger +
	"SKIP off-topic chatter / weather-like questions: value-gated retrieval, not dumb RAG. Returns records with tier / " +
	"confidence / lesson_type / stored+age / about signals — judge each hit's worth yourself: the result's first line is " +
	"the now= anchor every age= was measured against, stored= is WHEN THE ROW WAS WRITTEN, and about= is a date the row's " +
	"own TEXT states (absent when it states none) — read them as two different facts, never one. After seeing results, immediately " +
	"assess each hit by RECORD id: used it → memory(action=" + toolnames.ToolMemoryActionFeedback + ", id=#N, " +
	"rating=useful); CONTRADICTS your results → memory(action=" + toolnames.ToolMemoryActionFeedback + ", id=#N, " +
	"rating=useless) — then memory(action=" + toolnames.ToolMemoryActionUpdate + ") to fix it (误导=给未来的自己挖坑). A search " +
	"pair stays in context iff one of its records was voted useful by #N; every other pair is dropped. Empty result = " +
	"nothing distilled yet: after the task, memory(action=" + toolnames.ToolMemoryActionStore + ") what you learned " +
	"(取不到 → store). Hits ranked by query-token coverage + tier + staleness; cold-tier rows excluded. " +
	memorySearchSessionHalf

// memorySearchSessionHalf is the half this description did NOT use to carry: a
// search now answers from the round-note index of PAST RUNS as well as from the
// distilled knowledge base, and the two halves come back as two labelled
// sections. It is a constant of its own (not folded into memorySearchBullet's
// literal) because the merge that added it is a DIFFERENT change from the recall
// triggers beside it: the two arrived on separate branches, both rewrote this
// one description, and keeping them as two values is what let each branch's
// reader find its own sentence.
var memorySearchSessionHalf = "IT ALSO SEARCHES THE HISTORY OF PAST RUNS in the same call: " +
	"the result has TWO sections, and they are ranked SEPARATELY (a distilled row by tier/confidence/feedback, " +
	"a note hit by bm25 over raw note text — never mixed in one ordering). `[distilled]` is the knowledge base above. " +
	"`[session]` is the round-level note index: the one-line [IMPORTANT] summary every round of every past run recorded, " +
	"so it answers \"has a previous RUN been here?\" rather than \"has a lesson been distilled?\". Each `[session]` hit " +
	"names its session_id and message id and shows an evidence SLICE — the session's first few and last few notes plus a " +
	"window around the matching note, with `← hit` marking it and every omitted stretch COUNTED, so a 800-note session " +
	"costs a few lines, not the context. Read the slice first, then widen it with the ordinary session verbs: " +
	"`gopi session notes \"<query>\"` (the full note hits) and `gopi session show <session-id>` (the run itself)."

// chatRecallMarker opens the chat limb. It is the substring the gate asserts, so
// a block that silently fell out of the assembly cannot pass as a delivery.
const chatRecallMarker = "[CHAT RECALL — a vague memory phrase is a SEARCH you owe, not a question you answer from luck]"

// chatRecallAsk is the trigger vocabulary as the CONSOLE sees it. It is the same
// phrases the description carries, restated here because the chat reader is a
// different reader: a task run is handed a briefing and searches it; a console
// is handed a conversation and must recognise the request in the operator's own
// words as they arrive.
const chatRecallAsk = "When the operator's words are vague recall — 「你还记得…」「你忘了…」「我们之前讨论过…」「上次那个…」" +
	"「之前不是说过吗」/ \"do you remember …\", \"you forgot …\", \"we discussed this before\", \"last time …\", " +
	"\"what did we decide about …\" — "

// chatRecallWhy is the RATIONALE, and it is the sentence that makes the rule a
// duty rather than a habit: the failure being repaired is the model asking the
// HUMAN to supply what the LEDGER already holds, which spends the one resource
// this surface cannot get back (the operator's attention) on a lookup that costs
// one call.
const chatRecallWhy = "the answer is in the ledger, not in this conversation's window, and asking the operator to " +
	"repeat themselves is the failure being repaired: it spends their attention on a lookup you can run yourself. "

// chatRecallQueryRule is the translation instruction — the half that makes the
// call ANSWER rather than merely happen. A raw sentence is a poor retrieval key
// (it carries the human's politeness and their pronouns, not the topic), so the
// described behaviour is: compose the key from what the sentence is ABOUT.
const chatRecallQueryRule = "Translate the query yourself: compose it from the TOPIC (the symptom, the entity, the " +
	"decision, the file, the error text) — never pass the raw sentence through. One call covers distilled lessons and " +
	"procedural workflows."

// chatRecallFloor is the anti-abuse floor, stated on this surface too because a
// console is where the temptation is strongest: every turn is a turn you could
// search, so "search" must not become the reflex answer to any mention of the
// past.
const chatRecallFloor = "Not every mention of the past is a trigger: a greeting, a joke, or a phrase with no searchable " +
	"handle gets ONE clarifying question instead of a fishing expedition, and the same topic is never searched twice in " +
	"one turn."

// The two limbs, for the reason every chat paragraph carries a pair: the plain
// chat path has NO tools on the wire, so ordering it to call memory would be an
// instruction it cannot follow and a claim it must not make. The RULE is the same
// on both paths; only the MECHANISM differs.
var (
	chatRecallToolsLimb = "call " + toolnames.ToolMemory + "(action=" + toolnames.ToolMemoryActionSearch + ") in THIS " +
		"turn, before you answer and before you ask anything. " + chatRecallQueryRule

	chatRecallNoToolsLimb = "this path carries NO tools, so you cannot run that search: say plainly that the lookup " +
		"needs a tool-enabled turn (or ask the operator for the one detail you are missing) — and NEVER claim you " +
		"remembered something you did not look up."
)

// chatRecallRules renders the chat block's recall paragraph for ONE limb.
func chatRecallRules(toolsOn bool) string {
	var b strings.Builder
	b.WriteString(chatRecallMarker)
	b.WriteString(" ")
	b.WriteString(chatRecallAsk)
	b.WriteString(chatRecallWhy)
	if toolsOn {
		b.WriteString(chatRecallToolsLimb)
	} else {
		b.WriteString(chatRecallNoToolsLimb)
	}
	b.WriteString(" ")
	b.WriteString(chatRecallFloor)
	return b.String()
}
