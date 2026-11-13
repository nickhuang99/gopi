// Regression gate for the defect this task repaired (Rule 5: every fix ships
// a minimal repro).
//
// THE DEFECT, measured 2026-10-29 on session
// 794cbd02-4722-43c3-924c-f7ba6f36534d — `gopi session log <id> --chat` printed
//
//	Iterations: 85  — one JSONL `request` row = one HTTP request/response
//	turns: not measured — the chat index holds no row for this session …
//	human turns: 85  — one HUMAN TURN = one row the person typed
//	Passes:     85  — session_end total_iterations
//
// THREE OF THE FOUR NUMBERS EQUAL, and only ONE of them was a measurement. That
// log holds 94 `user` rows: 91 are `[system]` clock ticks and 3 are
// `[WRITE_TASK_AREA …]` state rows — NOT ONE is a row the person typed. The 85
// on the `human turns` line came from SessionTurnBounds' fallback (a session
// WITH CONTENT and NO recorded seam gets one turn at index 0) amplified by the
// reader's no-tick path marking every content round a seam, i.e. the derived
// subset collapsed onto the round count and was printed as if measured.
//
// WHY THE FIXTURE IS THE SHAPE IT IS: it must have MANY iterations and NO
// operator row, because that is the only shape in which the borrowed number is
// visible — a fixture with one operator row has a legitimately measured subset
// and a fixture with one iteration cannot show "human turns == Iterations" as a
// COLLISION rather than a coincidence (Rule 10.4: the smallest sample that can
// still distinguish the two answers).
package gopilog

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
)

// humanTurnsReproRounds is how many exchanges (and therefore how many derived
// seams) the repro log holds. More than one so the collision is a COUNT, not a
// coincidence of the single-row fallback.
const humanTurnsReproRounds = 4

// writeNoOperatorRowLog writes a session log in which EVERY `user` row is a
// harness injection — the shape of a resumed session, a worker run, or a task
// run nobody typed into (measured: 794cbd02). Returns its path.
func writeNoOperatorRowLog(t *testing.T) string {
	t.Helper()
	lines := []string{
		`{"sessionId":"no-operator-row","model":"fixture-model","timestamp":"2026-10-29T00:00:00Z","type":"session_start"}`,
	}
	for i := 0; i < humanTurnsReproRounds; i++ {
		// A CLOCK TICK, not an operator row: sessionevent.IsTurnOpeningRow
		// rejects it, so nothing in this log opens a turn.
		lines = append(lines,
			fmt.Sprintf(`{"message":{"content":"%s[t+%ds] — +2s since your previous request.","role":"user"},"type":"user"}`,
				sessionevent.HarnessMessagePrefix, i+1))
		lines = append(lines,
			fmt.Sprintf(`{"message":{"content":[{"text":"reply %d","type":"text"}],"role":"assistant"},"type":"assistant"}`, i+1))
		lines = append(lines,
			fmt.Sprintf(`{"completion_tokens":1,"iteration":%d,"prompt_tokens":10,"type":"request"}`, i))
	}
	lines = append(lines,
		fmt.Sprintf(`{"type":"session_end","total_iterations":%d}`, humanTurnsReproRounds))
	return writeFixture(t, lines)
}

// TestHumanTurnsIsNeverTheIterationCountWhenNoSeamWasRecorded is THE regression
// guard: a session with N iterations and NO recorded turn seam must not print
// `human turns: N`, and must say the count was not measured.
func TestHumanTurnsIsNeverTheIterationCountWhenNoSeamWasRecorded(t *testing.T) {
	s, err := ParseFile(writeNoOperatorRowLog(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// The premise the fixture must establish, or the assertions below prove
	// nothing: the log really has rounds and really recorded no seam.
	if SessionRounds(s) < humanTurnsReproRounds {
		t.Fatalf("the fixture holds %d round(s), want %d — a log with no rounds cannot show the borrowed number", SessionRounds(s), humanTurnsReproRounds)
	}
	note, unmeasured := SessionHumanTurnsUnmeasured(s)
	if !unmeasured {
		t.Fatalf("the fixture is expected to have NO recorded turn seam, but the criterion says measured (%d derived turn(s)) — the fixture stopped reproducing the defect", SessionHumanTurns(s))
	}

	chat := FormatChat(s)
	// (1) THE BORROWED NUMBER IS GONE. This is the exact string the defect
	// printed: the round count under the subset's word.
	borrowed := sessionevent.CounterUnitHumanTurn + ": " + strconv.Itoa(SessionRounds(s))
	if strings.Contains(chat, borrowed) {
		t.Errorf("the COST block still prints %q — that is the ROUND count under the person-only subset's word (rounds=%d):\n%s",
			borrowed, SessionRounds(s), chat)
	}
	// (2) THE GAP IS STATED, with its reason, on the subset's own line.
	want := sessionevent.CounterUnitHumanTurn + ": " + TurnCountUnmeasuredPrefix + note
	if !strings.Contains(chat, want) {
		t.Errorf("the COST block does not print the unmeasured subset as %q:\n%s", want, chat)
	}
	// (3) THE ROUND AND PASS COUNTS ARE UNTOUCHED by this change: the measured
	// values stay byte-identical (Rule 5: no counting semantics changed).
	if !strings.Contains(chat, "Iterations: "+strconv.Itoa(SessionRounds(s))+" ") {
		t.Errorf("the iteration line no longer carries the measured round count %d:\n%s", SessionRounds(s), chat)
	}
	if !strings.Contains(chat, "Passes:     "+strconv.Itoa(s.TotalIterations)+" ") {
		t.Errorf("the pass line no longer carries the measured loop count %d:\n%s", s.TotalIterations, chat)
	}
}

// TestHumanTurnsKeepsItsRealCountWhenASeamWasRecorded is the other half of the
// repro: the fix must not turn a MEASURED subset into "unknown". One operator
// row and N exchanges answer it — ONE turn, N rounds, both real numbers.
func TestHumanTurnsKeepsItsRealCountWhenASeamWasRecorded(t *testing.T) {
	s, err := ParseFile(writeHumanRoundFixture(t, humanTurnsReproRounds))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	note, unmeasured := SessionHumanTurnsUnmeasured(s)
	if unmeasured {
		t.Fatalf("a log with an operator row was called unmeasured (%q) — the fix would hide a real count", note)
	}
	if got, want := SessionHumanTurns(s), 1; got != want {
		t.Fatalf("SessionHumanTurns = %d, want %d (one operator row, four exchanges)", got, want)
	}
	chat := FormatChat(s)
	want := sessionevent.CounterUnitHumanTurn + ": 1  — "
	if !strings.Contains(chat, want) {
		t.Errorf("the COST block does not print the measured subset %q:\n%s", want, chat)
	}
	if strings.Contains(chat, TurnCountUnmeasuredPrefix+HumanTurnsNoRecordedSeamNote) {
		t.Errorf("the COST block calls a measured subset unmeasured:\n%s", chat)
	}
}

// TestEverySurfaceStatesAnUnmeasuredSubset is the cross-surface half: the COST
// block, the `--turns` table and the stats overview all read the ONE criterion,
// so none of them can print the borrowed number the others stopped printing
// (the defect was found on one surface and the other two carried it too —
// measured: the overview printed `human turns 85` beside `requests 85`, and the
// `--turns` table bannered 85 rows that no person opened).
func TestEverySurfaceStatesAnUnmeasuredSubset(t *testing.T) {
	s, err := ParseFile(writeNoOperatorRowLog(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	rows := strconv.Itoa(SessionRounds(s))

	overview := FormatStatsOverview(BuildStatsFromJSONL(s))
	borrowedOverview := sessionevent.CounterUnitHumanTurn + " " + rows
	if strings.Contains(overview, borrowedOverview+"\n") || strings.Contains(overview, borrowedOverview+" ") && !strings.Contains(overview, TurnCountUnmeasuredPrefix) {
		t.Errorf("the stats overview prints the ROUND count %s under the subset's word:\n%s", rows, overview)
	}
	if !strings.Contains(overview, sessionevent.CounterUnitHumanTurn) {
		t.Errorf("the stats overview dropped the subset's word entirely — an unmeasured quantity must still be named:\n%s", overview)
	}

	table := FormatSessionTurnTable(s)
	if strings.Contains(table, "("+rows+")") {
		t.Errorf("the --turns banner carries the ROUND count %s as its human turns:\n%s", rows, table)
	}
	if !strings.Contains(table, "("+HumanTurnsUnknownWord+")") {
		t.Errorf("the --turns banner does not state the subset as %q:\n%s", HumanTurnsUnknownWord, table)
	}
}
