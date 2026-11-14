package tools

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestChatTurnClauseStatesTheOpenedCountAndNamesTheGap pins the contract the
// INDEX ROW implements (chatTurnClause, chat_turns.go).
//
// THE CONTRACT CHANGED, AND THIS FILE IS WHERE THE REASON LIVES (epic 2026-11-04,
// measured on session 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f): `session list`
// printed `turns 29` while `session replay --turns` printed 30 rows and the replay
// screen's last band read `──── turn 30`. One session, one word, two numbers.
//
// The row was printing chats.turn_count, which advances only when a turn
// COMPLETES (cmd/agent/cli_chat.go's `runErr == nil && !interrupted` gate), while
// the log records a seam when one OPENS — and that session's thirtieth turn opened
// and then quit. The completed count can also run AHEAD (e364e357-a9ea-4210:
// ledger 209 against 34 boundaries), so no ordinal can be anchored on it.
//
// So the bare word now carries the OPENED count — the axis `--turn N`, a replay
// band and the `--turns` table number along — and the ledger's completed count
// prints in parentheses under its own word whenever it differs, so the gap is
// accounted in numbers instead of a reader meeting two unqualified totals.
func TestChatTurnClauseStatesTheOpenedCountAndNamesTheGap(t *testing.T) {
	// THE DISAGREEING CASE — the shipped defect's shape: 30 opened, 29 completed.
	measured := chatTurnClause(ChatSessionDetail{TurnsKnown: true, TurnsOpened: 30, Turns: 17, TurnCount: 29})
	want := sessionevent.CounterUnitTurn + " 30"
	if measured != want {
		t.Fatalf("measured clause = %q, want exactly %q — the bare turn word carries the OPENED count (%d, the axis every "+
			"replay numbers along)", measured, want, 30)
	}
	// THE LEDGER'S NUMBER DOES NOT RIDE THE INDEX ROW. It prints on the
	// per-session faces (`session show`, `stats`, `log`, `replay`); on a one-line
	// index it would ride nearly every row (measured: 15 of 15 differ on this
	// host) and turn a scannable table into a two-number column — the 2026-10-27
	// ruling's own point, kept while the number it protects changed.
	if strings.Contains(measured, sessionevent.CounterUnitTurnCompleted) {
		t.Fatalf("the index clause %q carries the ledger's completed count — that belongs to the per-session faces", measured)
	}

	// THE AGREEING CASE prints ONE number: a session that closed every turn it
	// opened must not wear a redundant twin, which is how a reader learns to skip
	// the parenthesis that matters.
	agreeing := chatTurnClause(ChatSessionDetail{TurnsKnown: true, TurnsOpened: 12, TurnCount: 12})
	if wantAgreeing := sessionevent.CounterUnitTurn + " 12"; agreeing != wantAgreeing {
		t.Fatalf("agreeing clause = %q, want exactly %q — the index row carries ONE number", agreeing, wantAgreeing)
	}

	// THE BARE WORD IS NEVER THE QUALIFIED ONE (Rule 14): `human turns` names a
	// different number on another surface, so the index row must not print it.
	if strings.Contains(measured, sessionevent.CounterUnitHumanTurn) {
		t.Fatalf("clause %q prints the person-only subset under its qualified word — that quantity belongs to "+
			"`session show` and `session stats`, not to the one-line index row", measured)
	}

	// AN UNMEASURED ROW SAYS WHY instead of printing a number nobody took
	// (Rule 15) — and it must not print the completed count either, because the
	// clause that carries it never ran.
	unmeasured := chatTurnClause(ChatSessionDetail{TurnsKnown: false, TurnsNote: "session log not on this machine", TurnCount: 18})
	if strings.Contains(unmeasured, "18") {
		t.Fatalf("unmeasured clause = %q — it printed the ledger's count beside an unmeasured total, which reads as a "+
			"measurement of the same thing", unmeasured)
	}
	if !strings.Contains(unmeasured, "session log not on this machine") {
		t.Fatalf("unmeasured clause = %q, want it to carry the reason verbatim", unmeasured)
	}
}
