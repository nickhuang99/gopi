package main

// chat_turn_display_writer_test.go — THE SCREEN MUST NUMBER A TURN BY THE
// RECORD'S OWN TURN AXIS, NOT BY A COUNTER OF ITS OWN MAKING.
//
// THE DEFECT (measured 2026-11-08, session e68fddf5-32a9-4e6d-b06f-13a101295883:
// the screen's `chat-screen.dump.json` against `gopi session replay --turns` on
// the same session). The screen numbered a turn 169 for the prompt the reader
// gave 168, and every band after it stayed exactly one ahead; before it, band
// 76 == row 76. So one event moved the screen's counter and not the record's,
// and the gap never closed again.
//
// WHY THE TWO CAN PART. chatTurnDisplay used `turnBaseline + operatorTurns`,
// where operatorTurns is a counter THIS REPL bumps at its own seams. The
// record's ordinal is a POSITION in its turn-opening rows — the axis
// `gopi session log --turns` prints and `session replay --turn N` selects. The
// REPL is not the only writer of that record: internal/agent writes
// turn-opening rows of its own (a `gopi notify` delivery, a job notice opened
// to report one, a boundary carrier), and each one advances the record while
// operatorTurns never hears about it. A permanent one-turn offset is not
// cosmetic: the number the operator copies into `--turn N` selects a DIFFERENT
// turn.
//
// The fix is the one `gopi web` already uses (webTurnOrdinal): ask the WRITER.
// `agent.SessionLogger.TurnsSoFar` counts the turn-opening rows THIS run has
// written under the SAME predicate (`sessionevent.IsTurnOpeningRow`) the reader
// uses, so a foreign row is inside the number by construction.
//
// THE TWO HALVES COUNT DISJOINT ROWS, and the fixture has to model that or it
// measures its own double count: `turnBaseline` is what the CONTINUED record
// held before this run started, `TurnsSoFar` is what THIS run's record has
// written since. A resumed chat opens a NEW record (cli_chat's
// `NewSessionLoggerForResumedRun`) and reads the baseline from the old one, so
// exactly one of them owns any given row.

import (
	"strconv"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/inputsource"
)

// The two session ids model the resume shape: the CONTINUED record is one
// session, and the resumed chat opens ANOTHER record of its own — so
// turnBaseline and TurnsSoFar count disjoint rows, which is what the two halves
// of chatTurnDisplay assume.
const (
	chatTurnPriorSessionID = "c0ffee00-1111-4222-8333-444455550001"
	chatTurnRunSessionID   = "c0ffee00-1111-4222-8333-444455550002"
)

// chatTurnRecord writes `turns` complete exchanges through a REAL session
// logger and returns the logger. A turn in the record's own axis is the opening
// `user` row plus the content that follows it, so each turn here is written as
// the pair a real run produces — a `user` row with no content after it is not
// yet a turn the reader can see.
func chatTurnRecord(t *testing.T, label, sessionID string, turns int) *agent.SessionLogger {
	t.Helper()
	log, err := agent.NewSessionLoggerForResumedRun(
		"deepseek-v4-flash", "", sessionID, nil, "")
	if err != nil {
		t.Fatalf("%s: open the session record: %v", label, err)
	}
	t.Cleanup(func() { log.Close() }) //stylegate:ignore close in a cleanup; the assertions already ran
	for i := 0; i < turns; i++ {
		n := strconv.Itoa(i)
		if err := log.LogUserFrom("p-"+label+"-"+n, label+" question "+n, inputsource.Human); err != nil {
			t.Fatalf("%s: write turn %d: %v", label, i, err)
		}
		if err := log.LogAssistantText(label + " answer " + n); err != nil {
			t.Fatalf("%s: write answer %d: %v", label, i, err)
		}
		if err := log.LogRequest(0, 10, 0, 0, 2); err != nil {
			t.Fatalf("%s: write request %d: %v", label, i, err)
		}
	}
	if err := log.Sync(); err != nil {
		t.Fatalf("%s: flush: %v", label, err)
	}
	return log
}

// chatTurnProfile pins a throwaway profile and returns the turn baseline the
// CONTINUED record holds — the production shape of `sess.turnBaseline`.
func chatTurnProfile(t *testing.T, priorTurns int) int {
	t.Helper()
	t.Setenv(config.GopiDirEnv, t.TempDir())
	if priorTurns == 0 {
		return 0
	}
	prior := chatTurnRecord(t, "prior", chatTurnPriorSessionID, priorTurns)
	parsed, err := gopilog.ParseFile(prior.Path())
	if err != nil {
		t.Fatalf("parse the continued record: %v", err)
	}
	if got := gopilog.SessionTurns(parsed); got != priorTurns {
		t.Fatalf("fixture is not the shape this case asserts: the continued record holds %d turns, want %d",
			got, priorTurns)
	}
	return priorTurns
}

// TestChatTurnDisplayFollowsTheWriterNotItsOwnCounter is the regression this
// epic owes: a turn-opening row written by internal/agent — never seen by the
// REPL's own counter — must not shift the screen's numbering away from the
// record's.
func TestChatTurnDisplayFollowsTheWriterNotItsOwnCounter(t *testing.T) {
	const seeded = 4
	baseline := chatTurnProfile(t, seeded)
	run := chatTurnRecord(t, "run", chatTurnRunSessionID, 0)

	sess := &chatSession{turnBaseline: baseline, turnWriter: run}

	// (1) the REPL's own turn — its row and its counter both advance.
	if err := run.LogUserFrom("p-a", "the operator's own line", inputsource.Human); err != nil {
		t.Fatalf("write the REPL row: %v", err)
	}
	if err := run.LogAssistantText("the answer"); err != nil {
		t.Fatalf("write the answer: %v", err)
	}
	if err := run.LogRequest(0, 10, 0, 0, 2); err != nil {
		t.Fatalf("write the REPL row's request: %v", err)
	}
	const replLocalTurns = 1 // what the REPL's own counter would say

	// (2) a FOREIGN turn-opening row: internal/agent delivers a `gopi notify`
	// message and opens a turn to report it. The REPL never bumps for this one —
	// it is written inside the agent, below the REPL's seams. THIS is the row
	// that produced the measured one-turn offset.
	if err := run.LogUserFrom("p-b",
		"[system] An external process sent this session a message: task event: event=completed task=01a1b802",
		inputsource.Notify); err != nil {
		t.Fatalf("write the delivery row: %v", err)
	}
	if err := run.LogAssistantText("reported"); err != nil {
		t.Fatalf("write the delivery answer: %v", err)
	}
	if err := run.LogRequest(0, 10, 0, 0, 2); err != nil {
		t.Fatalf("write the delivery row's request: %v", err)
	}

	if err := run.Sync(); err != nil {
		t.Fatalf("flush the run's record: %v", err)
	}
	parsed, err := gopilog.ParseFile(run.Path())
	if err != nil {
		t.Fatalf("parse the run's record: %v", err)
	}
	// The record's own answer for the turn now starting: everything the
	// CONTINUED record held, plus the two this run wrote, plus the one starting.
	wantOrdinal := baseline + gopilog.SessionTurns(parsed) + 1

	got := sess.chatTurnDisplay(replLocalTurns) + 1
	if got != wantOrdinal {
		t.Fatalf("the screen numbers the next turn %d, the record's own axis says %d — "+
			"the screen counted the REPL's %d local turn(s) since baseline %d and could not see the row "+
			"internal/agent wrote; the operator copying %d into `session replay --turn %d` is shown a different turn",
			got, wantOrdinal, replLocalTurns, baseline, got, got)
	}
}

// TestChatTurnDisplayCountsTheTurnNowStarting pins the SEAM the band is drawn
// at: the heading is printed BEFORE the operator's row reaches the writer
// (`chatBeginTurn` runs above `ChatOps`, which is what calls LogUserFrom), so
// the number for the turn now starting is "rows the record has opened" + 1 —
// and the caller applies that +1 itself, exactly as it did before this fix.
// What this pins is that the two sides stay EQUAL across the row landing: the
// writer's count moves by one, and the number the screen shows moves by one
// with it, never twice.
func TestChatTurnDisplayCountsTheTurnNowStarting(t *testing.T) {
	const seeded = 3
	baseline := chatTurnProfile(t, seeded)
	run := chatTurnRecord(t, "run", chatTurnRunSessionID, 0)
	sess := &chatSession{turnBaseline: baseline, turnWriter: run}

	// The band is drawn: no row of this run has reached the writer yet, so the
	// turn now starting is the record's next one.
	const replLocalTurns = 0
	if got, want := sess.chatTurnDisplay(replLocalTurns)+1, baseline+1; got != want {
		t.Fatalf("the turn now starting is numbered %d before any row is written, want %d", got, want)
	}
	// The row lands, and the NEXT band is the next turn — exactly one higher.
	if err := run.LogUserFrom("p-a", "the operator's own line", inputsource.Human); err != nil {
		t.Fatalf("write the row: %v", err)
	}
	if err := run.LogAssistantText("the answer"); err != nil {
		t.Fatalf("write the answer: %v", err)
	}
	if err := run.LogRequest(0, 10, 0, 0, 2); err != nil {
		t.Fatalf("write the request: %v", err)
	}
	if got, want := sess.chatTurnDisplay(1)+1, baseline+2; got != want {
		t.Fatalf("the next turn is numbered %d after one turn landed, want %d", got, want)
	}
}

// TestChatTurnDisplayWithoutARecordUsesTheReplsOwnCount states the nil-writer
// branch rather than leaving it to be inferred: a run whose record was refused
// (replaymode, or a nil logger) has no writer to ask, and it keeps the
// pre-existing behaviour instead of silently numbering everything the baseline.
func TestChatTurnDisplayWithoutARecordUsesTheReplsOwnCount(t *testing.T) {
	sess := &chatSession{turnBaseline: 7}
	if got, want := sess.chatTurnDisplay(2), 7+2; got != want {
		t.Fatalf("with no writer the numbering must fall back to the REPL's own count: got %d, want %d", got, want)
	}
}
