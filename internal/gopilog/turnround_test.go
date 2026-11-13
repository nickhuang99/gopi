// turnround_test.go — the reverse-lookup gate (Rule 5: every fix ships a
// minimal repro).
//
// THE FIXTURE SEPARATES THE TWO AXES THE SMALLEST WAY THAT CAN STILL DISAGREE
// (Rule 10.4): THREE operator rows answered by SIX request rows — two rounds per
// turn. A fixture with one turn, or with as many rounds as turns, is BLIND:
// every candidate answer prints the same number, so an index that re-derived
// boundaries wrongly (or read the round ordinal as a turn) would still pass.
//
// The boundaries here are REAL, not simulated: each turn opens at a row whose
// direction is a plain user input, which is exactly what
// sessionevent.IsTurnOpeningRow answers "a turn opens here" for. That is what
// makes the assertions below evidence — this file proves the lookup reads the
// SAME predicate the `turns` row and the export's TURN address read.
package gopilog

import (
	"strings"
	"testing"
)

// fixtureTurnRounds is how many rounds each fixture turn covers, and
// fixtureTurns how many turns there are: together they are the numbers the
// assertions read, named once (Rule 1).
const (
	fixtureJoinTurns         = 3
	fixtureJoinRoundsPerTurn = 2
)

// joinFixture builds the session described above: fixtureJoinTurns operator rows,
// each answered by fixtureJoinRoundsPerTurn request rows.
func joinFixture() *Session {
	s := &Session{ID: "join-fixture", Model: "fixture-model",
		RequestEvents: fixtureJoinTurns * fixtureJoinRoundsPerTurn}
	round := 0
	for turn := 0; turn < fixtureJoinTurns; turn++ {
		for i := 0; i < fixtureJoinRoundsPerTurn; i++ {
			round++
			it := Iteration{Number: round - 1, RequestOrdinal: round}
			if i == 0 {
				// The operator row: the FIRST exchange of each turn.
				it.TurnBoundary = true
				it.TurnSeamRecorded = true
				it.UserPrompt = "ask"
			}
			s.Iterations = append(s.Iterations, it)
		}
	}
	return s
}

// TestTurnRoundIndexAnswersBothDirections is the deliverable's core: the same
// index answers "which rounds does turn N cover" and "which turn owns round R",
// and the two answers must be each other's inverse — on EVERY turn and EVERY
// round, not on a spot-check (a spot-check passes for an index built from a
// second, drifting walk).
func TestTurnRoundIndexAnswersBothDirections(t *testing.T) {
	ix := BuildTurnRoundIndex(joinFixture())
	if got := ix.Total(); got != fixtureJoinTurns {
		t.Fatalf("index total = %d turn(s), want %d — the assertions below would be measuring the wrong fixture", got, fixtureJoinTurns)
	}
	for turn := 1; turn <= fixtureJoinTurns; turn++ {
		first, last, covered, ok := ix.RoundsOfTurn(turn)
		if !ok || !covered {
			t.Fatalf("turn %d: ok=%v covered=%v — every fixture turn covers %d rounds", turn, ok, covered, fixtureJoinRoundsPerTurn)
		}
		wantFirst := (turn-1)*fixtureJoinRoundsPerTurn + 1
		if first != wantFirst || last != wantFirst+fixtureJoinRoundsPerTurn-1 {
			t.Errorf("turn %d covers rounds %d–%d, want %d–%d", turn, first, last, wantFirst, wantFirst+fixtureJoinRoundsPerTurn-1)
		}
		for r := first; r <= last; r++ {
			owner, ok := ix.TurnOfRound(r)
			if !ok {
				t.Errorf("round %d has no owner, though turn %d covers it", r, turn)
				continue
			}
			if owner != turn {
				t.Errorf("round %d answered turn %d, but turn %d covers it — the two directions disagree", r, owner, turn)
			}
		}
	}
}

// TestTurnRoundIndexRefusesWhatItDoesNotHold pins the two REFUSALS apart: an
// ordinal past the end of the session is one fact, a round no turn owns is
// another. Conflating them would tell a reader their typo was a session
// property (Rule 15).
func TestTurnRoundIndexRefusesWhatItDoesNotHold(t *testing.T) {
	ix := BuildTurnRoundIndex(joinFixture())

	if _, _, _, ok := ix.RoundsOfTurn(fixtureJoinTurns + 1); ok {
		t.Errorf("RoundsOfTurn(%d) answered for a session holding %d turns", fixtureJoinTurns+1, fixtureJoinTurns)
	}
	if _, ok := ix.TurnOfRound(fixtureJoinTurns*fixtureJoinRoundsPerTurn + 1); ok {
		t.Errorf("a round past the session's numbering answered an owner")
	}

	// A LOG WITH NO OPERATOR ROW still has ONE turn, and that is
	// SessionTurnBounds' own documented fallback (a session with content and no
	// recorded seam is one turn) — NOT something this index invents. The
	// assertion is therefore the FALLBACK's, read through the index: the single
	// turn covers every round, and turn 2 does not exist. Writing the opposite
	// here would have been a test asserting a behaviour the ONE boundary
	// algorithm does not have, and it would have gone red against correct code.
	res := &Session{ID: "no-operator-row", Model: "fixture-model", RequestEvents: 2}
	for i := 0; i < 2; i++ {
		res.Iterations = append(res.Iterations, Iteration{Number: i, RequestOrdinal: i + 1})
	}
	fallback := BuildTurnRoundIndex(res)
	if got, want := fallback.Total(), SessionTurns(res); got != want {
		t.Fatalf("index total = %d but SessionTurns = %d — the index re-derived boundaries", got, want)
	}
	if owner, ok := fallback.TurnOfRound(1); !ok || owner != 1 {
		t.Errorf("round 1 answered turn %d,%v — the fallback makes the whole log turn 1", owner, ok)
	}
	if first, last, covered, ok := fallback.RoundsOfTurn(1); !ok || !covered || first != 1 || last != 2 {
		t.Errorf("turn 1 covers %d–%d (ok=%v covered=%v), want 1–2", first, last, ok, covered)
	}
	if _, _, _, ok := fallback.RoundsOfTurn(2); ok {
		t.Errorf("turn 2 answered on a one-turn fallback session")
	}
}

// TestTurnRoundJoinNamesBothUnits is the WORDING half, and it is the epic's own
// subject: a line that answers "which turn is this round?" must not let the two
// numbers be confused. Each shape therefore carries BOTH unit words, and the
// round-owner shape carries the round's own interval as well.
func TestTurnRoundJoinNamesBothUnits(t *testing.T) {
	ix := BuildTurnRoundIndex(joinFixture())

	got, err := FormatTurnRoundJoin(ix, 0, 3)
	if err != nil {
		t.Fatalf("FormatTurnRoundJoin(round 3) = %v", err)
	}
	for _, want := range []string{"round 3", "turn 2", "rounds 3–4"} {
		if !strings.Contains(got, want) {
			t.Errorf("round lookup %q does not carry %q — the reader cannot tell the two numbers apart without both units", got, want)
		}
	}

	got, err = FormatTurnRoundJoin(ix, 2, 0)
	if err != nil {
		t.Fatalf("FormatTurnRoundJoin(turn 2) = %v", err)
	}
	for _, want := range []string{"turn 2", "rounds 3–4", "3 turns"} {
		if !strings.Contains(got, want) {
			t.Errorf("turn lookup %q does not carry %q", got, want)
		}
	}

	// THE TWO SHAPES NAME THE SAME PAIR, from either side: this is the property
	// a reader relies on when they cross-check one against the other.
	if _, err := FormatTurnRoundJoin(ix, 0, 0); err == nil {
		t.Errorf("a lookup with neither a turn nor a round answered, want a refusal")
	}
}

// TestTurnRoundIndexReadsTheOneBoundaryAlgorithm is the epic's red line, stated
// as a test rather than as prose: the index's turn total must EQUAL
// SessionTurns, which is what `session show`'s `turns` row prints. Two counters
// over one log would be the second derivation this epic forbids, and the way it
// would first show up is exactly here — a total that drifts from the row above
// it (Rule 14).
func TestTurnRoundIndexReadsTheOneBoundaryAlgorithm(t *testing.T) {
	s := joinFixture()
	ix := BuildTurnRoundIndex(s)
	if got, want := ix.Total(), SessionTurns(s); got != want {
		t.Errorf("index total = %d but SessionTurns = %d — two answers to one question", got, want)
	}
	if got, want := ix.TotalRounds(), SessionRounds(s); got != want {
		t.Errorf("index round total = %d but SessionRounds = %d", got, want)
	}
}
