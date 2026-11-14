// Regression gate for the defect this task repaired (Rule 5: every fix
// ships a minimal repro).
//
// THE DEFECT, measured 2026-10-27 on session
// a60787b0-9081-451f-bd5a-9e396cfc048b: once the turn predicate became
// source-based (sessionevent.IsTurnOpeningRow), every recorded face opened a
// turn — a `gopi notify` delivery and a `chat_surface action=type` self-typed
// line included. The surfaces that print `human turns` kept reading
// SessionTurns, so `session show`, `session stats` and the `SESSION HUMAN
// TURNS` table of `session log --turns` all printed 16 for a session whose
// honest person-only subset was 8. The subset had collapsed into the total:
// the word `human` said something the number no longer meant (Rule 14), and a
// subset that equals its superset is a claim the measurement stopped
// supporting (Rule 15).
//
// THE FIXTURE IS DELIBERATELY MIXED — two person turns, one notify turn and
// one self-typed turn — because a fixture where every face is human makes this
// test BLIND: both counters agree and the assertion cannot fail. Rule 10.4:
// verify on the smallest sample that can still distinguish the two answers.
package gopilog

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/inputsource"
)

// mixedFaceSession is the smallest session that separates the two counters:
// 4 turns, of which 2 are the person's own.
func mixedFaceSession() *Session {
	return &Session{
		ID:    "01a17a1b-fixture",
		Model: "fixture",
		Iterations: []Iteration{
			{TurnBoundary: true, TurnSeamRecorded: true, TurnSource: inputsource.Human, UserPrompt: "the person's first ask"},
			{TurnBoundary: true, TurnSeamRecorded: true, TurnSource: inputsource.Notify, UserPrompt: "[system] a delivery arrived"},
			{TurnBoundary: true, TurnSeamRecorded: true, TurnSource: inputsource.Queue, UserPrompt: "the person's parked ask"},
			{TurnBoundary: true, TurnSeamRecorded: true, TurnSource: inputsource.LLM, UserPrompt: "the model typed this itself"},
		},
	}
}

// TestHumanTurnsIsThePersonSubsetNotTheTotal pins the two numbers apart. If a
// future change makes SessionHumanTurns return SessionTurns again, THIS is the
// assertion that fails, and it fails with both values named.
func TestHumanTurnsIsThePersonSubsetNotTheTotal(t *testing.T) {
	s := mixedFaceSession()
	if got, want := SessionTurns(s), 4; got != want {
		t.Fatalf("SessionTurns = %d, want %d (every recorded face opens a turn)", got, want)
	}
	if got, want := SessionHumanTurns(s), 2; got != want {
		t.Fatalf("SessionHumanTurns = %d, want %d (Human + Queue only — the person's own input)", got, want)
	}
}

// TestHumanTurnsDefaultsToPersonOnAnUnrecordedFace pins the pre-`source`-field
// default: a row whose face was never recorded is the person's, the same
// default internal/gopilog/reader.go writes. It must not be silently dropped
// from BOTH counters.
func TestHumanTurnsDefaultsToPersonOnAnUnrecordedFace(t *testing.T) {
	s := &Session{Iterations: []Iteration{{TurnBoundary: true, TurnSeamRecorded: true}}}
	if got, want := SessionHumanTurns(s), 1; got != want {
		t.Fatalf("SessionHumanTurns = %d, want %d (an unrecorded face defaults to the person)", got, want)
	}
}

// TestTurnTableBannersTheSubsetItActuallyTabulates is the surface-level half:
// the table's banner number, its row count and its rows must all be the
// SUBSET, in ONE reading. Printing the total in the banner over subset rows
// would be two numbers for one table — the very collision this epic removes.
func TestTurnTableBannersTheSubsetItActuallyTabulates(t *testing.T) {
	s := mixedFaceSession()
	table := FormatSessionTurnTable(s)

	// The banner carries the SUBSET, not the total.
	if want := "(" + itoa(SessionHumanTurns(s)) + ")"; !strings.Contains(table, want) {
		t.Errorf("the --turns banner does not carry the subset %d:\n%s", SessionHumanTurns(s), table)
	}
	if blind := "(" + itoa(SessionTurns(s)) + ")"; strings.Contains(table, blind) {
		t.Errorf("the --turns banner carries the TOTAL %d — the subset collapsed into it:\n%s", SessionTurns(s), table)
	}

	// The rows are the SUBSET, counted rather than assumed.
	rows := 0
	for _, line := range strings.Split(table, "\n") {
		if isTurnRow(line) {
			rows++
		}
	}
	if rows != SessionHumanTurns(s) {
		t.Errorf("the --turns table printed %d row(s), want the %d human turn(s):\n%s", rows, SessionHumanTurns(s), table)
	}
	// And the fixture must be one where the two numbers DIVERGE, or the
	// assertions above prove nothing (Rule 10.4).
	if SessionTurns(s) == SessionHumanTurns(s) {
		t.Fatalf("the fixture no longer separates the two counts (%d == %d) — this test is blind", SessionTurns(s), SessionHumanTurns(s))
	}
}

// isTurnRow reports whether a line of the turn table is one TURN's row: a
// leading ordinal followed by the round count, which the header (TURN/PROMPT)
// and the unit lines do not match.
func isTurnRow(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	for _, f := range fields[:2] {
		if f == "" {
			return false
		}
		for _, r := range f {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
