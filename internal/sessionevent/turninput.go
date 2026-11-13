package sessionevent

// turninput.go — THE ONE PREDICATE THAT DECIDES WHERE A TURN BEGINS.
//
// WHAT WAS WRONG (measured, 2026-10-27, session 4a0957a3-bb4e-4337-a761-20606c87d1fd).
// The predicate was `IsHumanRoundBoundary(text) = !IsHarnessInjected(text)`,
// i.e. "a turn starts wherever the text does NOT open with a framework marker".
// That is a TEXT test standing in for a PROVENANCE question, and the two are not
// the same question once a delivery path that is a COMPLETE INPUT also carries a
// framework marker. On that session the two kinds of row the model read as
//   "[system] An external process sent this session a message: …"   (×20)
//   "[system] [t+~11m] — +1s since your previous request."           (×50)
// were treated IDENTICALLY by the old rule (both "harness"), while the FIRST is
// `gopi notify` handing the session a message — one complete input the harness
// opens a turn to report (cmd/agent/chat_job_notice_turn.go's own words: "this
// turn was opened to report them") — and the SECOND is the loop's clock tick,
// which is machinery inside a turn that already exists. The operator's ruling
// (2026-10-27) is that a turn begins at EVERY complete input — a person's typed
// line, a `gopi notify` delivery, and a `chat_surface action=type` self-typed
// line — and the old rule got all three faces' bookkeeping wrong in different
// ways on the same log.
//
// THE SHAPE OF THE FIX, and why it is not "count more prefixes". A reader asks
// two DIFFERENT questions, and only one of them is about text:
//
//	(1) WHERE DOES A TURN BEGIN?   → provenance + input shape  (this file)
//	(2) IS THIS ROW THE FRAMEWORK TALKING? → text markers only (IsHarnessInjected)
//
// Question (1) cannot be answered from text alone, because a notify delivery's
// TEXT is the framework's own sentence (the caller's words are quoted inside it,
// after the marker), so text can never separate it from a clock tick. The
// separation the format itself records is the row's `source` field
// (internal/inputsource: human / queue / notify / llm — written by the producer
// that enqueued the row). So the predicate takes the SOURCE, and only falls back
// to text for the two things text genuinely can say: "this row is a framework
// CARRIER" (a marker), and "this row is the operator's own input, framed by the
// harness because it was too large for the chat line" (the message boundary).
//
// Rule 14: this is the ONE definition. internal/gopilog's reader (its
// TurnBoundary flag, read by SessionTurns / SessionTurnBounds), internal/
// replaydrive's recording loader and every surface built on them call it and
// nothing re-derives it.

import (
	"strings"

	"github.com/nick/gopi/internal/inputsource"
)

// The three framework CARRIERS that are not chatter — the markers a COMPLETE
// INPUT arrives under. They live here for the same reason HarnessMessagePrefix
// and HarnessMessageMarkers do (Rule 14): the WRITER is internal/agent (which
// imports this package) and the READER cannot import internal/agent, so the
// value has exactly one home and both sides reference it. internal/agent's own
// writers spell their sentences from these constants, so a change to the marker
// cannot leave a reader unable to recognise the row it writes.
const (
	// HarnessInboxNoticeWord follows HarnessMessagePrefix on the notice that
	// carries ONE message an external process delivered with
	// `gopi notify --session <id> --message <text>` (internal/agent's
	// sessionMessageFact). The caller's own text is quoted after it, verbatim.
	HarnessInboxNoticeWord = "An external process sent this session a message: "
	// HarnessInboxPendingWord follows the count on the opening line that names
	// SEVERAL pending deliveries ("3 notices are pending: …"), which is the same
	// turn opener for a batch (internal/agent's ChatJobNoticeOpenLine).
	HarnessInboxPendingWord = " notices are pending: "
	// HarnessInboxOpenedWord closes the BATCH opener and is what separates it
	// from a bare restatement of the same list. internal/agent's
	// ChatJobNoticeOpenLine writes it (it spells its whole sentence from this
	// constant), and the leg below reads it — one spelling, two readers (Rule 14).
	//
	// WHY A CLAUSE AND NOT THE MARKER ALONE. The batch form and the single-
	// delivery form are both carriers, and in a live log BOTH open turns when the
	// recorded face is `notify` (leg 1). The CONVERSATION, however, has no face:
	// deepseek.Message carries no `source`, so every row a fold sees is judged
	// under the empty-source default (`human`), where the carrier legs decide —
	// and "the list of pending ids" is exactly the sentence a restatement would
	// also carry. The clause is the writer's own statement that this row was
	// MINTED as a turn's opening message, so it distinguishes the opener from a
	// restatement with the writer's words rather than with a guess.
	HarnessInboxOpenedWord = " — this turn was opened to report them"
	// HarnessMessageBoundaryPrefix opens the framing the chat boundary puts on
	// the OPERATOR'S OWN input when it was too large for the input line and was
	// billed to a file (cmd/agent/chat_boundary.go). It is not framework
	// chatter: the turn exists because the person sent something, and the
	// framing is how the model is told the payload is data.
	HarnessMessageBoundaryPrefix = "[gopi message boundary] "
)

// IsTurnOpeningRow reports whether a user row OPENS A TURN — whether what
// arrived is ONE COMPLETE INPUT the harness has to answer.
//
// source is the row's recorded provenance (internal/inputsource: human, queue,
// notify, llm). An empty source is a row written before the format recorded
// faces, and every such row was the person's own, so `human` is the honest
// reading rather than a guess (the same default internal/gopilog's reader
// applies beside this call, `userRowSource`).
//
// FOUR LEGS, IN THIS ORDER — and the ORDER is the rule, because the same
// sentence arrives under two different faces.
//
//  1. A DELIVERY (`source == notify`) IS AN INPUT. `gopi notify` handed this
//     session a message from another process; that is one complete input by the
//     operator's ruling (2026-10-27), and it opens a turn. Its TEXT cannot be
//     the judge — the text is the framework's own sentence, with the caller's
//     words quoted after it.
//  2. THE OPERATOR'S OWN INPUT, FRAMED because it was too large for the chat
//     line ("[gopi message boundary] …"). Still the person's message; the
//     framing only says "the payload is DATA". An input.
//  3. A DELIVERY'S RESTATEMENT IS NOT AN INPUT. The SAME notice is re-injected
//     at every round boundary that follows, and those rows are recorded from the
//     HUMAN face (the loop injects them) with the identical carrier text.
//     Measured on session 4a0957a3-bb4e-4337-a761-20606c87d1fd, 2026-10-27: 11
//     delivery rows under `notify` against 17 restatements under `human` — leg 1
//     counts the 11, and this leg keeps the 17 from each becoming a turn. This
//     is the whole reason the predicate takes a SOURCE: from text alone those 28
//     rows are one indistinguishable class, and every attempt to merge them by
//     "the previous row was also a carrier" or by text identity is wrong in a
//     direction that is measurable (8 turns reported as 24, or two genuinely
//     different deliveries merged into one).
//     3b. A BATCH OPENER IS AN INPUT, EVEN UNDER THE HUMAN FACE. The row
//     `<n> notices are pending: … — this turn was opened to report them`
//     (internal/agent's ChatJobNoticeOpenLine) is how a console MINTS a turn for
//     a batch of deliveries, so it is an input by construction — and it must be
//     recognised from TEXT, because the CONVERSATION carries no face at all:
//     `deepseek.Message` has no `source` field, so every row a fold on the live
//     context judges arrives here under the empty-source default (`human`). While
//     this leg was absent, leg 3 claimed the opener as a restatement and NOTHING
//     in a chat's post-carrier history opened a turn: the fold's split collapsed
//     into ONE range and `foldCompletedTurns` returned at `len(turns) < 2` —
//     measured on session 6f982833-6291-45cd-b420-ed9df0ce1e36, where request
//     300 carried 247 messages / 5 genuine turns as ONE unfoldable range while
//     the same log's 527 batch-opener rows all carried the clause.
//     THE CLAUSE IS THE DISCRIMINATOR, not the marker: the single-delivery
//     restatement (leg 3's measured 17 rows) carries `HarnessInboxNoticeWord`
//     and NO clause, so the two classes stay separated by the writer's own words.
//  4. A BRIEFING IS AN INPUT. `[gopi briefing] ` (and the `[EPIC GOAL]` frame a
//     dispatched run puts around it) is the ONE complete input a run session ever
//     receives — it has no operator. While this leg was absent, nothing in a
//     136-row run log opened a turn, the reader's request-row fallback fired, and
//     `--turns` printed one turn per exchange (measured 2026-11-01 on
//     01a1944e-900f-70b7-81ac-24eda447b38b: 127 rounds reported as 127 turns).
//     It sits BEFORE the marker leg for the same reason leg 1 does: harness-written
//     AND an input is one row, not a contradiction.
//  5. EVERYTHING ELSE is answered by "is this the framework talking?" — a clock
//     tick, a start/review checkpoint, a truncation warning: machinery appended
//     INSIDE a turn that already exists, so it opens nothing. A self-typed
//     (`llm`) line and a parked prompt (`queue`) reach this leg as ordinary
//     input and open their own turns, as the ruling requires.
//
// IT IS NOT THE COMPLEMENT OF IsHarnessInjected ANY MORE, and that is the fix,
// not a loss: a `gopi notify` row IS harness-injected (the harness wrote the
// sentence) AND it IS a turn opening. The old complement assertion pinned the
// very conflation this predicate removes; turninput_test.go now pins the relation
// that holds instead — "a delivery opens a turn" and "a delivery's restatement,
// recorded from the human face, does not" — both of which fail red on a tree that
// reinstates the complement.
func IsTurnOpeningRow(text, source string) bool {
	trimmed := trimLeadingSpace(text)
	if source == inputsource.Notify {
		return true
	}
	if hasPrefix(trimmed, HarnessMessageBoundaryPrefix) {
		return true
	}
	if isBriefingInput(trimmed) {
		return true
	}
	if isInboxBatchOpener(trimmed) {
		return true
	}
	if isInboxCarrier(trimmed) {
		return false
	}
	return !hasHarnessPrefix(trimmed)
}

// TurnRowSource resolves the FACE a recorded `user` row arrived under, applying
// the ONE default: a row written before the format recorded faces came from the
// person, so `human` is a FACT about those rows rather than a guess (measured —
// every pre-2026-10-25 row in this profile's tree has no `source` field).
//
// IT LIVES HERE, NOT IN A CONSUMER (Rule 14). Three readers ask this question
// and each of them will act on the answer:
//
//	internal/gopilog/reader.go   — the turn-boundary predicate and TurnSource
//	internal/replaydrive         — the face a replayed line is TYPED back under
//
// and if the default were spelled in two of them, a log with no faces would be
// counted as one thing and replayed as another. That divergence is exactly the
// defect this epic removes, one layer down: the replay re-entered recorded lines
// as keystrokes, the keystroke face is `human` by construction, and the surface's
// predicate then answered "no turn" for a carrier row that the READER counts as a
// turn — 7 bands numbered 1,2,2,2,2,2,2 on the filmed screen (session
// 0b51ae4a-fa82-41ba-8147-1ba3ebc92476).
//
// An empty source is the ONLY case needing the default; every recorded value is
// returned verbatim, so an unknown spelling is passed through rather than
// silently mapped to `human`. That is deliberate: `IsTurnOpeningRow` then judges
// it by its text, and a face this binary cannot name is not one it should
// fabricate (the same refusal chatCountsAsOperatorTurn makes upstream).
func TurnRowSource(source string) string {
	if source != "" {
		return source
	}
	return inputsource.Human
}

// isBriefingInput reports whether a trimmed row body carries a briefing the
// PARENT handed this session — the `[gopi briefing] ` marker, or the
// `[EPIC GOAL]` frame put around it for a DISPATCHED run.
//
// WHY IT OPENS A TURN AND SITS BEFORE THE MARKER TEST (measured 2026-11-01,
// session 01a1944e-900f-70b7-81ac-24eda447b38b — a dispatched RUN): a run
// session has no operator, so the only complete input it ever receives is the
// briefing it was dispatched with. `HarnessBriefingOnStdinMarker` is listed in
// HarnessMessageMarkers, so the framework leg below classified that row as
// machinery, NOTHING in the log opened a turn, and the reader's request-row
// fallback (internal/gopilog/reader.go, readRequestRoundSeams) then gave every
// exchange a turn of its own: 127 rounds printed as 127 turns of one round each.
//
// So the ORDER is the rule, exactly as it is for a `gopi notify` delivery (a
// delivery IS harness-injected AND IS an input): a briefing is framework-written
// AND it is the input. The marker is the SAME marker the marker list matches —
// this leg does not re-word it, it decides a different question about it
// (Rule 14: one definition, two classifications).
//
// It does NOT make every later row of the run an input: the repeated harness
// injections inside the run (`[system]` ticks, checkpoints, state rows) carry
// none of these marks and still open nothing.
func isBriefingInput(trimmed string) bool {
	if hasPrefix(trimmed, HarnessBriefingOnStdinMarker) {
		return true
	}
	return hasFramedBriefing(trimmed)
}

// isInboxBatchOpener reports whether a row is the OPENER a console minted for a
// batch of pending notices — `<n> notices are pending: …, … — this turn was
// opened to report them`.
//
// IT IS NOT isInboxCarrier's NEGATION, and the two are asked in that order
// (IsTurnOpeningRow's legs 3 and 3b): the carrier legs answer "is this the
// framework restating a delivery?", and a batch opener IS framework-written —
// but it is also the FIRST thing in a turn the framework opened, which is a
// different question with a different answer. The clause is the writer's own
// (HarnessInboxOpenedWord), so this function reads the sentence the producer
// wrote rather than guessing from the count.
func isInboxBatchOpener(trimmed string) bool {
	if !isPendingNoticeCarrier(stripHarnessMessagePrefix(trimmed)) {
		return false
	}
	return strings.Contains(trimmed, HarnessInboxOpenedWord)
}

// stripHarnessMessagePrefix removes the framework prefix when present, so a
// caller can ask about the BODY of a row without caring which face wrote it.
func stripHarnessMessagePrefix(trimmed string) string {
	if hasPrefix(trimmed, HarnessMessagePrefix) {
		return trimmed[len(HarnessMessagePrefix):]
	}
	return trimmed
}

// isInboxCarrier reports whether a trimmed row body is the framework's notice
// sentence for a delivered message — the text a delivery AND its restatements
// share.
//
// IT MATCHES THE MARKER, NOT THE MESSAGE: the caller's words are quoted after it
// and are never consulted (a delivery whose payload opens with a bracket is still
// a delivery).
func isInboxCarrier(trimmed string) bool {
	if hasPrefix(trimmed, HarnessMessagePrefix) {
		body := trimmed[len(HarnessMessagePrefix):]
		if hasPrefix(body, HarnessInboxNoticeWord) {
			return true
		}
		return isPendingNoticeCarrier(body)
	}
	// The batch form has been seen WITHOUT the prefix on a row whose producer
	// wrote the sentence straight into the message. Recognising it costs nothing
	// and losing it would silently count a restatement as an input (Rule 15).
	return isPendingNoticeCarrier(trimmed)
}

// isPendingNoticeCarrier reports whether body opens with "<count> notices are
// pending: " — the batch form of the inbox carrier.
func isPendingNoticeCarrier(body string) bool {
	i := 0
	for i < len(body) && body[i] >= '0' && body[i] <= '9' {
		i++
	}
	return i > 0 && len(body) > i && hasPrefix(body[i:], HarnessInboxPendingWord)
}

// hasPrefix is strings.HasPrefix for the calls above, kept local so this file
// adds no dependency beyond the standard library and the one constant package it
// must reference.
func hasPrefix(s, prefix string) bool {
	return strings.HasPrefix(s, prefix)
}

// IsFrameworkChatter reports whether a user row is THE LOOP TALKING TO ITSELF —
// a clock tick, a start/review checkpoint, the committed-conclusions block, the
// self-record carrier, an inbox notice's restatement.
//
// IT IS THE TURN PREDICATE'S SHAPE TEST, NOT THE WHOLE PREDICATE — and the
// distinction became load-bearing on 2026-11-01, when a BRIEFING row
// (`[gopi briefing] `/the `[EPIC GOAL]` frame) started opening a turn: a
// dispatched run has no operator, so the briefing it was dispatched with is the
// only complete input it ever receives, and while this function answered "not an
// input" for it NOTHING in a 136-row run log opened a turn — the reader's
// request-row fallback then printed one turn per exchange (session
// 01a1944e-900f-70b7-81ac-24eda447b38b: 127 rounds, 127 turns).
//
// WHY IT STILL SAYS `true` FOR A BRIEFING: the question this function answers is
// "does this row carry TEXT the framework wrote?", and a briefing does — it is
// the harness's sentence, and every surface that prints a stored row (the chat
// transcript's typing, the replay's labels) must render it as machinery rather
// than as something the person typed. The question "is it an INPUT the harness
// must answer?" is IsTurnOpeningRow's, and it answers YES for a briefing. The two
// are different questions and a briefing is the row where they come apart — the
// same pair a `gopi notify` delivery has carried since the 2026-10-27 ruling
// (harness-written AND an input).
//
// SO IT IS NOT `!IsTurnOpeningRow(..., human)` ANY MORE. That equality was true
// only while "framework-written" and "opens no turn" were the same fact; the
// briefing leg broke it, and defining it as the complement would silently
// reclassify a briefing as the operator's own line on every surface that renders
// one. What replaces it is the shape test alone (harness prefix / marker), which
// is what those surfaces actually need.
//
// The delivery carrier is DELIBERATELY in this set: the sentence is the
// framework's, and the restatements of a delivery are exactly the rows that must
// not each become a turn. The DELIVERY ITSELF is not this row — it is the same
// text under the notify face, and IsTurnOpeningRow's first leg is what makes it
// an input.
func IsFrameworkChatter(text string) bool {
	return hasHarnessPrefix(trimLeadingSpace(text))
}

// OpensNoTurn is IsFrameworkChatter: a plain-language spelling for the reader
// who is asking about turns rather than about the framework. Both are kept
// because both questions are asked (Rule 14: one DEFINITION, two names is an
// alias — the definition above is the only one).
func OpensNoTurn(text string) bool {
	return IsFrameworkChatter(text)
}
