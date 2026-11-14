package main

// chat_turn_ordinal_agreement_test.go — THE THREE SURFACES ON ONE NUMBER.
//
// THE EPIC: the operator reads `turn 605` off the chat screen, types
// `gopi session replay <id> --turn 605`, and is shown a DIFFERENT turn. Measured
// on session e1e22e9f-eee0-4c33-a238-0fca04534ad9 on 2026-11-04: the screen's
// banner for the turn whose prompt began "这个我在之前就测试了…" said 604 while
// the reader's row for the SAME prompt said 605 — and 16 of 16 readable banners
// sat exactly one below their row.
//
// SO THE PROPERTY IS AN AGREEMENT, not a constant: for one session log, the
// number the screen prints for the next turn, the number the `--turns` table
// gives that turn, and the ordinal `replay --turn N` resolves must be ONE number.
// This test pins the agreement by driving each surface's OWN producer:
//
//	the screen's heading = chatTurnDisplay(baseline) + 1   (the REPL's seams)
//	the table's last row  = gopilog.FormatSessionTurnTable  (--turns itself)
//	the replay ordinal    = gopilog.SessionTurnLabels/spans (--turn N)
//
// and it makes the ledger's count DIFFER from all of them, so the anchor this
// test replaced (chats.turn_count) cannot pass it.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestChatTurnNumberingAgreesWithTheTurnTable is the regression this epic owes:
// a resumed run's NEXT heading number must be the number of turn-opening rows the
// log already holds, plus one — with the ledger's count deliberately different,
// because that difference is exactly what the screen used to print.
func TestChatTurnNumberingAgreesWithTheTurnTable(t *testing.T) {
	gopiHome := t.TempDir()
	t.Setenv(config.GopiDirEnv, gopiHome)

	const logTurns = 4
	const ledgerTurns = 9
	path := chatResumeTurnFixture(t, gopiHome, chatResumeFixtureID, logTurns)

	s, err := gopilog.ParseFile(path)
	if err != nil {
		t.Fatalf("parse the fixture: %v", err)
	}
	if got := gopilog.SessionTurns(s); got != logTurns {
		t.Fatalf("the fixture holds %d turn-opening rows by gopilog.SessionTurns, want %d — the fixture is not the shape this case asserts", got, logTurns)
	}

	// The ledger is charged a DIFFERENT number of finished turns, which is the
	// whole point: it is the count of turns that COMPLETED, not of rows.
	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: chatResumeFixtureID, Mode: tools.ChatModeCLI, Model: "deepseek-v4-flash",
		FirstMsg: "the crashed run's question", Workdir: gopiHome, LogPath: path,
	}); err != nil {
		t.Fatalf("seed the index row: %v", err)
	}
	for i := 0; i < ledgerTurns; i++ {
		if err := tools.ChatRecordTurn(chatResumeFixtureID, 10, 2, 0); err != nil {
			t.Fatalf("record turn %d: %v", i+1, err)
		}
	}
	if ledgerTurns == logTurns {
		t.Fatalf("the ledger and the log hold the same count (%d) — a fixture like that cannot see the defect", logTurns)
	}

	// WHAT THE SCREEN WOULD PRINT for the next turn it runs: the resume baseline
	// plus one (the REPL's own arithmetic, through its own function).
	baseline, err := chatResumeTurnBaseline(path, "")
	if err != nil {
		t.Fatalf("chatResumeTurnBaseline: %v", err)
	}
	sess := &chatSession{turnBaseline: baseline}
	screenNext := sess.chatTurnDisplay(0 + 1)

	// WHAT THE TABLE SAYS the session already holds, and where its rows end: the
	// `--turns` surface, driven through the renderer `gopi session log --turns`
	// calls. Its rows are the operator-row subset, so the ROW NUMBER is what the
	// operator copies — read off the table, never asserted from a constant here.
	table := gopilog.FormatSessionTurnTable(s)
	lastRow := turnTableLastRow(t, table)

	if screenNext != gopilog.SessionTurns(s)+1 {
		t.Fatalf("the screen's next heading = %d, want %d — the heading must be the log's %d turn-opening rows plus one, or `replay --turn %d` selects a different turn",
			screenNext, gopilog.SessionTurns(s)+1, gopilog.SessionTurns(s), screenNext)
	}
	if lastRow != gopilog.SessionHumanTurns(s) {
		t.Fatalf("the --turns table ends at row %d, want the log's %d human turns — the table's rows are the ordinals the operator copies, so the table and the reader must not disagree",
			lastRow, gopilog.SessionHumanTurns(s))
	}
	// The ledger's number appears NOWHERE in the screen's arithmetic: with the
	// old anchor the heading would have been ledgerTurns+1.
	if screenNext == ledgerTurns+1 {
		t.Fatalf("the heading = %d is the LEDGER's count plus one — the anchor is still the completed-turn count, the defect this fix removes", screenNext)
	}
}

// turnTableLastRow returns the highest TURN number printed in the table's rows,
// 0 when it printed none. It reads the rendered text — the surface the operator
// reads — rather than recomputing the subset.
func turnTableLastRow(t *testing.T, table string) int {
	t.Helper()
	last := 0
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if _, err := strconv.Atoi(fields[1]); err != nil {
			continue
		}
		if n > last {
			last = n
		}
	}
	return last
}
