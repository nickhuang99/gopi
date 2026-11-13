package gopilog

// turn_axes_named_test.go — REGRESSION GATE for the two-turn-numbers defect
// (Rule 5: every fix ships a minimal repro; Rule 10.4: the fixture must be one
// where the two numbers DIFFER, or the assertions prove nothing).
//
// THE DEFECT, measured 2026-11-04 on session
// e1e22e9f-eee0-4c33-a238-0fca04534ad9 (a real chat session on this host,
// `state interrupt`):
//
//	$ gopi session log e1e22e9f-… --turns
//	SESSION HUMAN TURNS — e1e22e9f-… (214)
//	  turns = 619   ← the LEDGER's chats.turn_count
//	  …
//	   TURN   ROUNDS  PROMPT
//	    621       27  那么你看现在要不要我们把使用这个统计工具啊？…
//
// The header printed 619, the column under it ran to 621, and BOTH numbers
// appeared under the one word "turn" with a single explanatory sentence that
// described neither of them precisely enough to say why they differ. A reader
// who looked up "turn 621" landed on turn 619's rows.
//
// WHY THE TWO NUMBERS ARE BOTH TRUE AND ARE NOT THE SAME QUANTITY:
//
//	chats.turn_count        advances when a turn COMPLETES
//	                        (tools.ChatRecordTurnOutcome, one write per
//	                        finished turn)
//	SessionTurns / TurnBoundary
//	                        counts the seams the LOG recorded, one per turn
//	                        that OPENED (internal/gopilog.SessionTurnBounds)
//
// A session that was interrupted — or is still running — holds boundaries the
// ledger never charged. On the session above the gap is exactly 2: it ended in
// `interrupt`, and its last two turns opened and never closed. Neither number
// is wrong, so the fix is NOT to force them equal (that would make one of them
// a lie); it is to print each under its OWN word, with the difference stated.
//
// WHAT THIS FILE PINS:
//
//	(1) the two numbers each appear under their own distinct word;
//	(2) the sentence accounting for the gap is printed when they differ, and
//	    names BOTH numbers and the difference;
//	(3) NOTHING is printed to explain a gap that does not exist (a session
//	    whose turns all closed must not carry the paragraph — noise teaches a
//	    reader to skip the line that matters);
//	(4) the two words are DIFFERENT (the collision is that they were one);
//	(5) the boundary count is the number the TURN COLUMN actually reaches —
//	    i.e. the axis the table numbers along is the axis the header names.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
)

// turnAxesFixture builds a session whose ledger turn count and logged turn
// boundary count DIFFER by construction, by the same mechanism the real session
// showed: more boundaries than completed turns.
//
// The ledger number is INJECTED (this package cannot read the ledger — see
// ledger_turns.go), so the fixture publishes a reader answering from a constant
// and restores the process state afterwards.
func turnAxesFixture(t *testing.T, boundaries, completed int) *Session {
	t.Helper()
	if boundaries <= completed {
		t.Fatalf("fixture must make boundaries > completed (got %d and %d) — a fixture where they coincide cannot see the defect", boundaries, completed)
	}
	s := &Session{ID: turnAxesFixtureSessionID}
	for i := 0; i < boundaries; i++ {
		s.Iterations = append(s.Iterations, Iteration{
			Number:           i,
			RequestOrdinal:   i + 1,
			TurnBoundary:     true,
			TurnSeamRecorded: true,
			UserPrompt:       "ask " + strconv.Itoa(i),
		})
	}
	prev := ledgerReaderForTest()
	InstallLedgerTurnCount(func(id string) (int, bool, string) {
		if id != turnAxesFixtureSessionID {
			return 0, false, "fixture: unknown session " + id
		}
		return completed, true, ""
	})
	t.Cleanup(func() { InstallLedgerTurnCount(prev) })
	return s
}

// turnAxesFixtureSessionID names the fixture, so the injected reader can refuse
// an id it does not know instead of answering for every session in the process.
const turnAxesFixtureSessionID = "turn-axes-fixture"

// ledgerReaderForTest returns the currently published reader so a test can put
// it back — the installed state is PROCESS-WIDE and a test that left its own
// reader behind would make every later render in this package report its
// fixture's number.
func ledgerReaderForTest() LedgerTurnCount { return ledgerTurnCount }

// TestTurnCountAndTurnBoundariesAreNamedSeparately is the gate: the log's OPENED
// count and the ledger's COMPLETED count must each print under their own word,
// and the difference must be stated in numbers.
//
// THE CONTRACT CHANGED ON 2026-11-04 AND THIS IS WHERE THAT IS RECORDED. The
// earlier version of this gate asserted the bare word `turns` carried the
// LEDGER's number. That is exactly what shipped the defect: on session
// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f `gopi session list` printed `turns 29`
// while `gopi session replay --turns` printed 30 rows and the replay screen ended
// on `──── turn 30`. The bare word is now the log's OPENED count everywhere —
// the axis `--turn N`, a replay band and the TURN column number along — and the
// ledger's number prints under its own word, only when it differs.
func TestTurnCountAndTurnBoundariesAreNamedSeparately(t *testing.T) {
	const boundaries, completed = 12, 9
	s := turnAxesFixture(t, boundaries, completed)
	table := FormatSessionTurnTable(s)

	// (4) THE TWO WORDS ARE DISTINCT — the whole defect is that they were one.
	if sessionevent.CounterUnitTurn == sessionevent.CounterUnitTurnCompleted {
		t.Fatalf("the two turn quantities print under ONE word %q — that is the collision, restored",
			sessionevent.CounterUnitTurn)
	}

	// (1) the OPENED count under the bare word, the LEDGER's under its own.
	if want := sessionevent.CounterUnitTurn + " = " + strconv.Itoa(boundaries); !strings.Contains(table, want) {
		t.Errorf("the table does not print the LOG's opened count as %q:\n%s", want, table)
	}
	if want := sessionevent.CounterUnitTurnCompleted + " = " + strconv.Itoa(completed); !strings.Contains(table, want) {
		t.Errorf("the table does not print the LEDGER's completed count as %q:\n%s", want, table)
	}
	// …and NOT each other's: the ledger's number under the bare word is the exact
	// mis-attribution that made `session list` contradict `session replay`.
	if bad := sessionevent.CounterUnitTurn + " = " + strconv.Itoa(completed); strings.Contains(table, bad) {
		t.Errorf("the table prints the LEDGER's completed count %d under the bare turn word (%q) — that is the defect:\n%s",
			completed, bad, table)
	}

	// (3) the ledger line says which quantity it is — a reader must not have to
	// infer it from the numbers.
	if !strings.Contains(table, sessionevent.CounterUnitTurnCompletedIsNot) {
		t.Errorf("the completed line does not carry its own %q sentence:\n%s",
			sessionevent.CounterUnitTurnCompleted, table)
	}

	// (5) THE TURN COLUMN REACHES THE BARE WORD'S NUMBER — the two cannot disagree
	// about "turn N" while both sit on one screen.
	if got := SessionTurns(s); got != boundaries {
		t.Fatalf("SessionTurns = %d, want the %d boundaries the fixture recorded", got, boundaries)
	}
}

// TestTheCompletedLineIsSilentWhenTheNumbersAgree pins the other direction
// (Rule 15): a session that closed every turn it opened prints ONE turn line,
// because a second identical number under a second word is noise — and noise is
// how the line that mattered gets skipped.
func TestTheCompletedLineIsSilentWhenTheNumbersAgree(t *testing.T) {
	const both = 7
	s := &Session{ID: turnAxesFixtureSessionID}
	for i := 0; i < both; i++ {
		s.Iterations = append(s.Iterations, Iteration{
			Number: i, RequestOrdinal: i + 1,
			TurnBoundary: true, TurnSeamRecorded: true,
			UserPrompt: "ask",
		})
	}
	prev := ledgerReaderForTest()
	InstallLedgerTurnCount(func(string) (int, bool, string) { return both, true, "" })
	t.Cleanup(func() { InstallLedgerTurnCount(prev) })

	table := FormatSessionTurnTable(s)
	if strings.Contains(table, sessionevent.CounterUnitTurnCompleted) {
		t.Errorf("two EQUAL turn counts still printed the completed line — it must be silent:\n%s", table)
	}
	if want := sessionevent.CounterUnitTurn + " = " + strconv.Itoa(both); !strings.Contains(table, want) {
		t.Errorf("the table does not print the bare turn word as %q:\n%s", want, table)
	}
}

// TestTheCompletedLineIsSilentWhenTheLedgerWasNotRead: an unmeasured ledger
// prints NO completed line — the block says it did not measure that count
// instead of printing a number or a bare reason where a number belongs, and the
// reason is carried by the faces that have a row for it (`stats`, the
// `--turns` banner). Rule 15 read carefully: silence about a number the run
// never took is the honest form; the defect would be silence about the fact that
// it was not taken, and that is reported where it belongs.
func TestTheCompletedLineIsSilentWhenTheLedgerWasNotRead(t *testing.T) {
	s := turnAxesFixture(t, 5, 4)
	InstallLedgerTurnCount(nil) // no reader at all — the unmeasured branch
	t.Cleanup(func() { InstallLedgerTurnCount(ledgerReaderForTest()) })

	table := FormatSessionTurnTable(s)
	if strings.Contains(table, sessionevent.CounterUnitTurnCompleted) {
		t.Errorf("an UNMEASURED ledger count still printed a completed line — a row with no number under it:\n%s", table)
	}
	if strings.Contains(table, sessionevent.CounterUnitTurnCompleted+" = 0") {
		t.Errorf("an unmeasured ledker count printed as 0 — unknown is stated, never filled:\n%s", table)
	}
	// The bare word does not depend on the ledger at all: it is the log's own
	// count, and it is still measured.
	if want := sessionevent.CounterUnitTurn + " = 5"; !strings.Contains(table, want) {
		t.Errorf("the log's opened count must print regardless of the ledger, want %q:\n%s", want, table)
	}
}

// TestTurnColumnNumbersAlongTheBoundaryAxis pins (5): the number the TURN column
// actually reaches must be the boundary count the header names. If a future
// change renumbers the column along a different axis, the header's sentence
// becomes a claim the table no longer backs.
func TestTurnColumnNumbersAlongTheBoundaryAxis(t *testing.T) {
	const boundaries, completed = 10, 6
	s := turnAxesFixture(t, boundaries, completed)
	if got := SessionTurns(s); got != boundaries {
		t.Fatalf("SessionTurns = %d, want the %d boundaries the fixture recorded", got, boundaries)
	}
	table := FormatSessionTurnTable(s)
	// The last row the table prints is the highest TURN number it reaches.
	maxRow := 0
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if n > maxRow {
			maxRow = n
		}
	}
	if maxRow != boundaries {
		t.Errorf("the TURN column reaches %d, but the header names %d %s — the column and its axis disagree:\n%s",
			maxRow, boundaries, sessionevent.CounterUnitTurnBoundary, table)
	}
	// And the ledger's smaller number is NOT where the column ends: a reader who
	// looks up the ledger's number must land before the last row, not on it.
	if maxRow == completed {
		t.Errorf("the fixture no longer separates the two axes (%d == %d) — this test is blind", maxRow, completed)
	}
}
