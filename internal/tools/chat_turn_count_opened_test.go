package tools

// chat_turn_count_opened_test.go — THE LEDGER COLUMN IS THE READER'S COUNTER.
//
// THE DEFECT (task 01a17a37, measured 2026-10-27): `chats.turn_count` was
// incremented by EVERY completed chat turn, while the session log opens a turn
// only at a row sessionevent.IsTurnOpeningRow accepts. Harness chatter written
// from the human face (a background-job notice) therefore charged the ledger and
// opened no turn in the log, and the two numbers — both printed under the word
// `turn` — diverged for the rest of the session's life. Session
// 80c07a7d-bb6b-48b4-be66-ea1867f403f2: ledger 18, reader 11.
//
// THE FIX: the caller answers the question (ChatRecordTurnOutcome's `opened`), and
// a row that opens no turn folds its TOKENS without charging a TURN. Both halves
// are asserted below, because a fix that stopped folding the tokens would make a
// non-turn's cost vanish, which is a second wrong answer.

import (
	"context"
	"testing"
)

// TestChatRecordTurnOutcomeChargesOnlyTurnsTheLogOpens: one real turn and one
// chatter row on the SAME session — the turn count advances by exactly one, and
// both rows' tokens are folded.
func TestChatRecordTurnOutcomeChargesOnlyTurnsTheLogOpens(t *testing.T) {
	chatTestEnv(t)
	const sid = "01a17a37-0000-4000-8000-00000000turn"
	seedOpenedTurnRow(t, sid)

	if err := ChatRecordTurnOutcome(sid, true, 100, 20, 90); err != nil {
		t.Fatalf("record the real turn: %v", err)
	}
	if err := ChatRecordTurnOutcome(sid, false, 300, 50, 280); err != nil {
		t.Fatalf("record the chatter row: %v", err)
	}

	got := chatRowForTest(t, sid)
	if got.turns != 1 {
		t.Fatalf("turn_count = %d after one opening turn and one chatter row, want 1 — the chatter row charged a turn the session log does not open", got.turns)
	}
	if got.input != 300 || got.output != 50 || got.cache != 280 {
		t.Fatalf("tokens in=%d out=%d cache=%d, want 300/50/280 — a row that opens no turn still SPENDS tokens", got.input, got.output, got.cache)
	}
}

// TestChatRecordTurnStillChargesEveryTurnItsOldCallersMake: the wrapper keeps the
// documented behaviour of the old surface (one call = one turn), so the carriers
// whose turn always opens one cannot silently stop counting.
func TestChatRecordTurnStillChargesEveryTurnItsOldCallersMake(t *testing.T) {
	chatTestEnv(t)
	const sid = "01a17a37-0000-4000-8000-00000000old1"
	seedOpenedTurnRow(t, sid)

	for i := 0; i < 3; i++ {
		if err := ChatRecordTurn(sid, 10, 2, 0); err != nil {
			t.Fatalf("ChatRecordTurn #%d: %v", i+1, err)
		}
	}
	if got := chatRowForTest(t, sid); got.turns != 3 {
		t.Fatalf("turn_count = %d after three ChatRecordTurn calls, want 3", got.turns)
	}
}

// seedOpenedTurnRow indexes a session the way the chat START hook does, with a
// zeroed turn count, so the assertions below read only what THIS file wrote.
func seedOpenedTurnRow(t *testing.T, sid string) {
	t.Helper()
	if _, err := ChatEnsureRow(ChatSession{
		SessionID: sid, Mode: ChatModeCLI, Model: "deepseek-flash",
		FirstMsg: "the operator's ask", Workdir: "/tmp/proj",
	}); err != nil {
		t.Fatalf("seed the chat row: %v", err)
	}
	if got := chatRowForTest(t, sid); got.turns != 0 {
		t.Fatalf("a freshly indexed row started at turn_count=%d, want 0 — the fixture would hide a double charge", got.turns)
	}
}

// chatRow is the three ledger facts this file judges.
type chatRow struct {
	turns  int
	input  int
	output int
	cache  int
}

// chatRowForTest reads the row back through the SAME database the writer used —
// never through a second path (Rule 32's discipline, in the tools layer).
func chatRowForTest(t *testing.T, sid string) chatRow {
	t.Helper()
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("open the ledger: %v", err)
	}
	defer db.Close()
	var r chatRow
	if err := db.QueryRowContext(ctx,
		`SELECT turn_count, input_tokens, output_tokens, cache_tokens FROM chats WHERE session_id = ?`, sid,
	).Scan(&r.turns, &r.input, &r.output, &r.cache); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	return r
}
