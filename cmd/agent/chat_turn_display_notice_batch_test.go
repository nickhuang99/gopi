package main

// chat_turn_display_notice_batch_test.go — THE MISSING COVERAGE BEHIND THE
// MEASURED ONE-TURN OFFSET: the BATCH notice row.
//
// WHAT WAS MEASURED (session e68fddf5-32a9-4e6d-b06f-13a101295883, 2026-11-08,
// record = 6183 rows, screen = chat-screen.dump.json on the same session): the
// record's turn axis holds 202 turns while the screen painted 203 bands, and the
// divergence is EXACTLY ONE ROW — the notify batch opener
//
//	[system] 3 notices are pending: external message …, external message …, …
//
// at record ordinal 149, drawn as band 150, with every later band +1 and the
// tail reading 203 against 202. Band number [149] is missing from the screen and
// [203] names no record turn.
//
// WHY THAT ROW AND NOT ANOTHER. `internal/agent` writes turn-opening rows of its
// own — a `gopi notify` delivery, the batch opener above, a boundary carrier —
// and the pre-fix screen numbered a band with `turnBaseline + operatorTurns`,
// where `operatorTurns` is a counter the REPL bumps at ITS OWN seams. Those rows
// are written BELOW the REPL, so one of them advances the record while the
// counter never hears about it, and the offset never closes again.
//
// `chat_turn_display_writer_test.go` already drives the SINGLE-delivery shape
// (`[system] An external process sent this session a message: …`). It does NOT
// drive the batch shape, which is the form the measured session actually hit —
// and the batch shape is a different row under `sessionevent.IsTurnOpeningRow`:
// a single delivery is recognised by its `notify` source alone, while the batch
// text is ALSO matched by `isInboxCarrier` as framework chatter that opens NO
// turn. The two arms therefore disagree about the batch row unless the source is
// stamped, and that is precisely the seam this file pins.
//
// THREE THINGS ARE ASSERTED, and each is a different way the row could go wrong:
//
//	(1) the batch row OPENS EXACTLY ONE TURN — not zero (which would undercount
//	    the record and put the operator's `--turn N` on the wrong turn) and not
//	    two (the line names several notices; a per-notice count would overcount);
//	(2) the WRITER and the READER give the SAME number for it — this is the
//	    equality the whole fix rests on, measured across the package boundary
//	    (only cmd/agent may import both halves; internal/agent cannot import
//	    internal/gopilog — see the import-cycle note in session_test.go);
//	(3) WITHOUT its `notify` source the SAME text opens NO turn — the carrier
//	    classification, which is what stops a restatement of an inbox from
//	    counting as a new input.
//
// The text is built by the REAL producer (`agent.ChatJobNoticeOpenLine`), never
// re-typed here: a hand-written copy would keep passing after the sentence
// changed (Rule 31).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatNoticeBatchLine builds the batch opener the way the REPL does at the turn
// boundary (`cmd/agent/cli_chat.go`: `msg = agent.ChatJobNoticeOpenLine(notices)`)
// from three EXTERNAL notices — the shape the measured session recorded at row
// 4553, which named three deliveries in one line.
func chatNoticeBatchLine(t *testing.T) string {
	t.Helper()
	notices := []tools.JobNotice{
		{External: &tools.ExternalMessage{ID: "ae591fe68fe37236", Message: "first delivery", Source: inputsource.Notify}},
		{External: &tools.ExternalMessage{ID: "c7910dbe07cf82dc", Message: "second delivery", Source: inputsource.Notify}},
		{External: &tools.ExternalMessage{ID: "c65375b5faff6fa6", Message: "third delivery", Source: inputsource.Notify}},
	}
	line := agent.ChatJobNoticeOpenLine(notices)
	// A fixture that is not the batch shape measures a different row: the single
	// form is a different sentence (`jobNoticeFact`) and a different arm here.
	if !strings.Contains(line, sessionevent.HarnessInboxPendingWord) {
		t.Fatalf("fixture is not the batch opener this case asserts: ChatJobNoticeOpenLine returned %q", line)
	}
	return line
}

// TestChatNoticeBatchRowOpensExactlyOneTurnBothHalves is the regression the
// measured 202-record/203-band offset owes.
func TestChatNoticeBatchRowOpensExactlyOneTurnBothHalves(t *testing.T) {
	t.Setenv(config.GopiDirEnv, t.TempDir())
	run := chatTurnRecord(t, "run", chatTurnRunSessionID, 0)
	line := chatNoticeBatchLine(t)

	// The row the production path writes: the REPL's notice turn stamps the
	// `notify` face (chatSealSource reads the peeked notices, never the text).
	if err := run.LogUserFrom("p-notice-batch", line, inputsource.Notify); err != nil {
		t.Fatalf("write the batch opener: %v", err)
	}
	if err := run.LogAssistantText("reported"); err != nil {
		t.Fatalf("write the answer: %v", err)
	}
	if err := run.LogRequest(0, 10, 0, 0, 2); err != nil {
		t.Fatalf("write the request: %v", err)
	}
	if err := run.Sync(); err != nil {
		t.Fatalf("flush the record: %v", err)
	}

	// (1) exactly ONE turn, counted by the writer that wrote it.
	if got := run.TurnsSoFar(); got != 1 {
		t.Fatalf("the batch opener naming 3 notices advanced the writer's turn count by %d, want exactly 1 — "+
			"one row is one turn whatever number of notices it names; got %q", got, line)
	}

	// (2) the reader must give the same number for the same rows: 202 vs 203 was
	// ONE axis disagreeing with the other, so the equality is the assertion.
	parsed, err := gopilog.ParseFile(run.Path())
	if err != nil {
		t.Fatalf("parse the record: %v", err)
	}
	readerTurns := gopilog.SessionTurns(parsed)
	if readerTurns != run.TurnsSoFar() {
		t.Fatalf("the record's reader counts %d turn(s) where its own writer counted %d for the same rows — "+
			"the screen asks the writer, so the two must not disagree; row %q", readerTurns, run.TurnsSoFar(), line)
	}

	// (3) THE CARRIER SHAPE OPENS NO TURN — and the carrier shape is the pending
	// list WITHOUT the opener clause, not the opener under another face.
	//
	// THIS FIXTURE CHANGED ON 2026-11-14 AND THE REASON IS A MEASUREMENT. This
	// arm used to write `line` itself (the clause-carrying OPENER) under the
	// human face and demand no turn; that premise is disproved twice over:
	//   - by the WRITER's own contract — ChatJobNoticeOpenLine's sentence says
	//     "this turn was opened to report them", so the row IS a turn's opening
	//     message whatever face carries it;
	//   - by the LOG — session 6f982833-6291-45cd-b420-ed9df0ce1e36 records 527
	//     batch-opener rows and every one is under the `notify` face (4 of 4 in
	//     its session.jsonl). A restatement OF THE BATCH OPENER does not occur;
	//     the restatements that DO occur are the single-delivery sentence (17 per
	//     11 deliveries), and that one still carries no clause and still opens
	//     nothing.
	// So the protection this arm exists for is asserted on the real restatement
	// shape, and the opener gets the OPPOSITE assertion beside it — a reader can
	// see both halves of the discrimination instead of one.
	restatement := sessionevent.HarnessMessagePrefix + "3" + sessionevent.HarnessInboxPendingWord +
		"external message ae591fe68fe37236, external message c7910dbe07cf82dc"
	if strings.Contains(restatement, sessionevent.HarnessInboxOpenedWord) {
		t.Fatalf("fixture drift: the restatement fixture carries the opener clause, so it does not measure the carrier class")
	}
	if err := run.LogUser("p-carrier", restatement); err != nil {
		t.Fatalf("write the carrier form: %v", err)
	}
	if got := run.TurnsSoFar(); got != 1 {
		t.Fatalf("a bare restatement of the pending list advanced the turn count to %d, want 1 — "+
			"it is an inbox CARRIER and opens no turn; got %q", got, restatement)
	}
	// AND THE OPENER, UNDER A FACE-LESS WRITE, DOES OPEN ONE. The conversation
	// carries no `source` at all (deepseek.Message has no such field), so the
	// fold on the live context judges exactly this row — and while it answered
	// "carrier" the split collapsed into one range and a 247-message chat history
	// folded as nothing (internal/agent's TestTurnFoldSplitsACarrierOnlyChatHistory).
	if err := run.LogUser("p-opener", line); err != nil {
		t.Fatalf("write the opener: %v", err)
	}
	if got := run.TurnsSoFar(); got != 2 {
		t.Fatalf("the turn-opening message a notice batch was MINTED with advanced the count to %d, want 2 — "+
			"its clause is the writer's own statement that it opens a turn; got %q", got, line)
	}
}

// TestChatTurnDisplayNumbersTheBatchNoticeTurnFromTheRecord pins the number the
// operator would ACT on: the band drawn for the turn the batch opener starts must
// be the ordinal `session replay --turn N` selects for that row — i.e. baseline +
// rows the record has opened + 1, with the REPL's own counter deliberately NOT
// matching it (that mismatch is the defect, stated as the fixture).
func TestChatTurnDisplayNumbersTheBatchNoticeTurnFromTheRecord(t *testing.T) {
	const seeded = 148 // the measured record: the batch opener was ordinal 149
	baseline := chatTurnProfile(t, seeded)
	run := chatTurnRecord(t, "run", chatTurnRunSessionID, 0)
	sess := &chatSession{turnBaseline: baseline, turnWriter: run}

	// The band is drawn BEFORE the row lands (chatBeginTurn runs above the
	// ChatOps call that writes it), so the turn now starting is baseline+1 — and
	// the REPL's own counter for this turn is 0, a notice turn having no
	// keystroke. The two are EQUAL here, which is what the fix buys.
	const replLocalTurns = 0
	if got, want := sess.chatTurnDisplay(replLocalTurns)+1, baseline+1; got != want {
		t.Fatalf("the batch notice turn is numbered %d, the record's own axis says %d", got, want)
	}

	line := chatNoticeBatchLine(t)
	if err := run.LogUserFrom("p-notice-batch", line, inputsource.Notify); err != nil {
		t.Fatalf("write the batch opener: %v", err)
	}
	if err := run.LogAssistantText("reported"); err != nil {
		t.Fatalf("write the answer: %v", err)
	}
	if err := run.LogRequest(0, 10, 0, 0, 2); err != nil {
		t.Fatalf("write the request: %v", err)
	}

	// THE DEFECT'S OWN ARITHMETIC, asserted in the failing direction as a value:
	// a screen numbering by the REPL's counter alone would paint baseline+1 AGAIN
	// for the next band (replLocalTurns is still 0 — the notice turn came from no
	// keystroke), while the record has moved on. The screen must follow the record.
	if got, want := sess.chatTurnDisplay(replLocalTurns)+1, baseline+2; got != want {
		t.Fatalf("after the batch opener landed the next band is numbered %d, the record's axis says %d — "+
			"the screen is counting a counter of its own (the REPL says %d local turn(s))", got, want, replLocalTurns)
	}
}
