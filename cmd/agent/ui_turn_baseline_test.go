package main

// ui_turn_baseline_test.go — THE STATUS BAR'S NUMBER, PINNED TO THE ORDINALS.
//
// THE DEFECT (the operator's own report, 2026-10-27: "重启之后这个数字又归零了"):
// `gopi ui`'s status bar counted the turns the SURFACE ran, from 1, while the
// session's own surfaces reported the whole history.
//
// ITS SECOND OCCURRENCE, AND THE ONE THAT FIXED THE ANCHOR (2026-11-04). The
// first fix anchored the bar on the ledger's `chats.turn_count`, which counts
// turns that FINISHED — a different quantity from the POSITION of a turn-opening
// row. Measured on session e1e22e9f-eee0-4c33-a238-0fca04534ad9 at one instant:
// ledger 619, log 621, screen 620, and 16 of 16 readable banners sat exactly ONE
// below the row carrying the same prompt. So the anchor is now the log's own
// turn count — the producer of the ordinals `session log --turns` numbers and
// `session replay --turn N` selects by (turn_ordinal_baseline.go states why the
// ledger is the wrong anchor and which older ruling this supersedes).
//
// TWO CLAIMS HERE, and a third in internal/uiclient (which owns the bar and can
// therefore assert it without starting a surface):
//
//	(1) the baseline VALUE is the log's turn count — NOT the ledger's
//	    `chats.turn_count` for the same session;
//	(2) a session with no record starts at 0 and does not fail the run;
//	(3) [internal/uiclient] the seeded value is what the FIRST rendered status
//	    line prints.

import (
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// TestUITurnBaselineReadsTheLogsOrdinals: the number the bar is seeded with is
// the count of turn-opening rows in the session's log, and it is NOT the
// ledger's `chats.turn_count` for the same session.
func TestUITurnBaselineReadsTheLogsOrdinals(t *testing.T) {
	sessionChatEnv(t)
	const logTurns = 3
	const ledgerTurns = 11
	path := writeTurnsFixtureSession(t, sessionListTurnsFixtureID, logTurns)

	if _, err := tools.ChatEnsureRow(tools.ChatSession{
		SessionID: sessionListTurnsFixtureID, Mode: tools.ChatModeCLI, Model: "deepseek-flash",
		FirstMsg: "the operator's first ask", Workdir: "/tmp/proj", LogPath: path,
	}); err != nil {
		t.Fatalf("seed the index row: %v", err)
	}
	for i := 0; i < ledgerTurns; i++ {
		if err := tools.ChatRecordTurn(sessionListTurnsFixtureID, 10, 2, 0); err != nil {
			t.Fatalf("record turn %d: %v", i+1, err)
		}
	}
	if logTurns == ledgerTurns {
		t.Fatalf("the fixture's two counts are equal (%d) — it cannot see the defect", logTurns)
	}

	if got := uiTurnBaselineFor(sessionListTurnsFixtureID); got != logTurns {
		t.Fatalf("uiTurnBaselineFor = %d, want the LOG's %d turn-opening rows — with the ledger's %d the bar would print an ordinal one producer behind the rows `session replay --turn N` selects",
			got, logTurns, ledgerTurns)
	}
}

// TestUITurnBaselineIsZeroForASessionWithNoRecord: a session id the sessions
// tree does not name (a run that has written no record yet) starts at 0 and does
// NOT fail — the bar still counts, it simply has nothing to add.
func TestUITurnBaselineIsZeroForASessionWithNoRecord(t *testing.T) {
	sessionChatEnv(t)
	if got := uiTurnBaselineFor("01900000-0000-4000-8000-000000000000"); got != 0 {
		t.Fatalf("uiTurnBaselineFor a session with no record = %d, want 0", got)
	}
	// No session id at all is the same answer, not a crash.
	if got := uiTurnBaselineFor(""); got != 0 {
		t.Fatalf("uiTurnBaselineFor with no id = %d, want 0", got)
	}
}
