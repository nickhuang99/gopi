package tools

// chat_first_msg.go — WHO MAY WRITE `chats.first_msg`, and WHAT THE ROW SAYS
// WHEN THE PERSON HAS SAID NOTHING YET.
//
// THE DEFECT (measured 2026-10-28, `gopi session list --mode chat` on the
// operator's own profile): the index's head column — the one a reader scans to
// recognise a conversation — read
//
//	66243525-…  [chat]  human  [system] 16 notices are pending: external message …
//	07d98407-…  [chat]  human  [system] 16 notices are pending: external message …
//	4a0957a3-…  [chat]  human  [system] An external process sent this session a message: …
//
// Three conversations, three framework sentences, zero recognisable titles.
// The write point was `ChatEnsureRow`'s ON CONFLICT clause
// (`first_msg=CASE WHEN excluded.first_msg != '' THEN excluded.first_msg ELSE
// chats.first_msg END`) fed by `agent.RegisterChatSession("chat", out.Display)`
// — and on a NOTICE turn `out.Display` is the line the harness composed
// itself (`agent.ChatJobNoticeOpenLine`). So the column was written by
// "whatever non-empty text the turn carried", which is the LAST input rather
// than the operator's first words: on those rows the final `gopi notify`
// delivery of the session overwrote whatever had been there.
//
// WHAT THIS FILE DECIDES, in one place so the three write paths (insert on
// first sight, the per-turn offer, the backfill repair) cannot each answer it
// differently (Rule 13/14):
//
//   - `chatLineIsOperatorOwn` — is this text the PERSON's, under the face it
//     was recorded with? Provenance answers it, never the string alone: a
//     `gopi notify` delivery and a `chat_surface action=type` line are complete
//     inputs too (internal/sessionevent.IsTurnOpeningRow says so), and they are
//     not the operator's words.
//   - `ChatFirstMsgPlaceholder` — what the column says while no operator line
//     exists. AN EXPLICIT PLACEHOLDER, never the injected text: the injection is
//     preserved where it belongs (the session JSONL, the authoritative record)
//     and is never DELETED by this change — see ChatOfferFirstMsg's contract.
//
// WHY THE PREDICATE IS NOT JUST "does it start with [system]": a framework
// marker test alone accepts a model-typed line and would still let one kind of
// non-operator input claim the head. The face is recorded on the row
// (internal/inputsource) precisely so this question is answered by provenance.

import (
	"context"
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// ChatFirstMsgPlaceholder is the head a chat row carries while the operator has
// not typed yet — a row whose first input was a `gopi notify` delivery, a
// background-job notice or a model-typed line.
//
// IT IS A WORD, NOT AN EMPTY STRING (Rule 15): ” reads as "the column was never
// written", which is indistinguishable from "nobody looked", and the two have
// different remedies. The words say whose line is missing rather than what
// happened, because what happened is on the log.
const ChatFirstMsgPlaceholder = "(no operator line yet)"

// chatFirstMsgUnsetSQLPredicate is the ONE test for "this row may still take an
// operator's first line": nothing stored, or only the placeholder. Written once
// so the per-turn offer and the insert-on-conflict clause charge the same
// question (Rule 1/13).
//
// The parameter order is (placeholder) — the column is compared to a bound
// value, never to a literal, so the constant exists in exactly one place.
const chatFirstMsgUnsetSQLPredicate = `(first_msg = '' OR first_msg = ?)`

// chatOfferFirstMsgSQL writes one row's first operator line, and ONLY over a
// column that is empty or still the placeholder: the first operator line wins
// for the life of the row, which is what "first" means.
const chatOfferFirstMsgSQL = `UPDATE chats SET first_msg = ?
	WHERE session_id = ? AND ` + chatFirstMsgUnsetSQLPredicate

// chatLineIsOperatorOwn reports whether text is the PERSON's own input, given
// the face the turn was recorded with (internal/inputsource).
//
// THREE LEGS, and each of them is a measured case rather than a precaution:
//
//  1. An EMPTY source is `human` — every row written before the format recorded
//     faces was the person's, which is the same default the session reader
//     applies (internal/gopilog's userRowSource). Empty TEXT is not an input at
//     all and never qualifies (a re-ensure must not claim the head).
//  2. The face must be the person's: `human` (typed at the prompt, or a paste the
//     editor spilled) or `queue` (the same author, a different producer —
//     internal/gopilog.SessionHumanTurns uses exactly this pair). A `notify`
//     delivery or a `chat_surface action=type` line is a complete input but it
//     is NOT this person's first sentence.
//  3. The text must OPEN A TURN under that face — `sessionevent.IsTurnOpeningRow`,
//     the ONE predicate the session reader uses, so "the operator's line" means
//     the same thing in the index as in `session log --turns`. This is the leg
//     that drops a harness carrier recorded from the human face (the round
//     boundary restates a delivery under `human`), and it keeps the operator's
//     spilled paste ("[gopi message boundary] …") — that IS their message.
func chatLineIsOperatorOwn(text, source string) bool {
	msg := strings.TrimSpace(text)
	if msg == "" {
		return false
	}
	src := strings.TrimSpace(source)
	if src == "" {
		src = inputsource.Human
	}
	if src != inputsource.Human && src != inputsource.Queue {
		return false
	}
	return sessionevent.IsTurnOpeningRow(msg, src)
}

// normalizeSourceOrHuman applies the reader's default to a recorded face: a row
// written before the format recorded faces came from the person, so `human` is a
// FACT about it (internal/gopilog's userRowSource says the same, and both read
// the one constant).
func normalizeSourceOrHuman(source string) string {
	if s := strings.TrimSpace(source); s != "" {
		return s
	}
	return inputsource.Human
}

// chatRepairFirstMsgSQL re-derives one row's head with a COMPARE-AND-SWAP on the
// value the scan read: the write lands only while the column still holds what was
// scanned, so a line the operator typed during the scan is never clobbered.
//
// IT CANNOT REUSE chatFirstMsgUnsetSQLPredicate, and that is a measured lesson
// rather than a preference: the unset predicate accepts only ” and the
// placeholder, while the rows this repair exists for hold an INJECTED sentence —
// so the first version of this code matched zero rows and reported "kept" for
// every damaged row (caught by TestChatRepairFirstMsgsReDerivesFromLog, which
// asserts the row's stored value after the run, not the report's counters).
const chatRepairFirstMsgSQL = `UPDATE chats SET first_msg = ?
	WHERE session_id = ? AND first_msg = ?`

// chatSummaryIsOperatorOwn reports whether a LEGACY tasks row's summary reads as
// the operator's own sentence — the backfill's version of the same question the
// live path asks. A legacy `[auto]` summary was written by the harness hook, so
// it is accepted only when nothing in it is a harness marker: migrating a
// framework sentence into the index head is the defect being repaired, and the
// repair must not re-create it on the migration path.
func chatSummaryIsOperatorOwn(text string) bool {
	return chatLineIsOperatorOwn(text, inputsource.Human)
}

// ChatOfferFirstMsg records text as sessionID's first operator line, if the row
// has not got one yet.
//
// THE CONTRACT, in three parts, because each one is a way this could go wrong:
//
//   - NOT THE OPERATOR'S → NOTHING IS WRITTEN, and the call is NOT an error.
//     A notice turn arrives here every time it is the turn's text; the answer is
//     "no", not a failure, and the caller must keep running the turn (the input
//     is not discarded anywhere: the turn still runs, still charges turn_count,
//     and its row is in the session JSONL — a chats row is an INDEX over that
//     file, never the record itself).
//   - THE ROW ALREADY HAS AN OPERATOR LINE → nothing is written. The FIRST
//     operator line is the head; a second one is just a later question.
//   - NO ROW AT ALL → an error, reported: silently succeeding would claim a
//     write that has nowhere to land (Rule 15).
//
// wrote reports whether the column actually changed, which is what the tests
// assert and what a caller may log; it is never a proxy for "the turn ran".
func ChatOfferFirstMsg(sessionID, text, source string) (wrote bool, err error) {
	if strings.TrimSpace(sessionID) == "" {
		return false, fmt.Errorf("chat: session_id must not be empty")
	}
	if !chatLineIsOperatorOwn(text, source) {
		return false, nil
	}
	msg := normalizeChatText(text)
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		return false, err
	}
	defer db.Close()
	res, err := db.ExecContext(ctx, chatOfferFirstMsgSQL, msg, sessionID, ChatFirstMsgPlaceholder)
	if err != nil {
		return false, fmt.Errorf("chat: offer first msg %s: %w", sessionID, err)
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		// The UPDATE matched nothing: either the row is gone or it already holds
		// a real operator line. Ask which, so a vanished row is not reported as
		// "already had one" (Rule 15: two causes, two sentences).
		var stored string
		if qerr := db.QueryRowContext(ctx, `SELECT first_msg FROM chats WHERE session_id = ?`, sessionID).
			Scan(&stored); qerr != nil {
			return false, fmt.Errorf("chat: offer first msg %s: no row (%w)", sessionID, qerr)
		}
		return false, nil
	}
	return true, nil
}
