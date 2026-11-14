package replayplay

// turnroundspan_test.go — A TURN SAYS WHICH ROUNDS IT COVERED.
//
// Material.Turns carries the ROUND interval each turn spans (FirstRound /
// LastRound, in the ROUND ADDRESS SPACE `--round N` resolves), derived from
// gopilog's ONE turn-boundary decision. It exists because the two units are
// adjacent numbers that never met on one line: an operator read a turn's ordinal
// and asked for that turn as `--round 152` — one round of the ten it covered.
//
// The fixture is the smallest that can tell the shapes apart: ONE operator row
// answered with THREE exchanges, so the turn count (1) and the round count (3)
// differ and an interval that was really the turn's own number could not pass.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/gopilog"
)

// TestTurnCarriesItsRoundInterval pins the PRESENT half: a turn that covered
// rounds 1..3 says exactly that, and the interval's ends are the numbers
// `--round N` resolves (the round ADDRESS SPACE, not an iteration index).
func TestTurnCarriesItsRoundInterval(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 3)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(mat.Turns) != 1 {
		t.Fatalf("turns = %d, want 1 — one operator row is one turn", len(mat.Turns))
	}
	turn := mat.Turns[0]
	if turn.FirstRound != 1 || turn.LastRound != mat.RoundCount {
		t.Fatalf("turn 1 covers rounds %d-%d, want 1-%d — every round of the turn must be inside its interval",
			turn.FirstRound, turn.LastRound, mat.RoundCount)
	}
	// The interval ENDS are ROUND numbers a caller can address: resolving
	// LastRound must name a real round of the material.
	if _, rerr := RoundIndexFor(mat, itoaTest(turn.LastRound)); rerr != nil {
		t.Fatalf("the interval's last round %d does not resolve in the round address space: %v", turn.LastRound, rerr)
	}
	// And each ROUND row knows nothing of a turn span: its interval fields stay
	// zero so a reader cannot mistake a round for a turn.
	for _, r := range mat.Rounds {
		if r.FirstRound != 0 || r.LastRound != 0 {
			t.Fatalf("round %d carries a turn interval (%d-%d) — only a TURN has one", r.Index, r.FirstRound, r.LastRound)
		}
	}
}

// TestRoundTurnOfAnswersAndStaysSilent pins BOTH halves of the join: a round a
// turn covers answers with that turn and its interval; a round no span covers
// answers ok=false rather than a made-up turn.
func TestRoundTurnOfAnswersAndStaysSilent(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 3)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	for n := 1; n <= mat.RoundCount; n++ {
		turn, first, last, total, ok := RoundTurnOf(mat, n)
		if !ok {
			t.Fatalf("RoundTurnOf(round %d) named no turn, but the fixture's turn covers every round", n)
		}
		if turn != 1 || first != 1 || last != mat.RoundCount || total != len(mat.Turns) {
			t.Fatalf("RoundTurnOf(round %d) = (turn %d, %d-%d of %d), want (1, 1-%d of %d)",
				n, turn, first, last, total, mat.RoundCount, len(mat.Turns))
		}
	}
	// ABSENT: a round the material does not have, and a material whose turns
	// carry no interval at all, both answer ok=false — never a neighbouring
	// turn's number.
	if _, _, _, _, ok := RoundTurnOf(mat, mat.RoundCount+1); ok {
		t.Fatalf("RoundTurnOf named a turn for round %d, which the material does not have", mat.RoundCount+1)
	}
	bare := Material{RoundCount: 1, Turns: []Round{{Index: 1, Kind: RoundTurn}}}
	if _, _, _, _, ok := RoundTurnOf(bare, 1); ok {
		t.Fatal("RoundTurnOf invented a turn for a span with no round interval")
	}
}

// TestRoundRangeSentenceNamesBothEnds pins the shared spelling the surfaces
// print, including its honest empty form (a bare "rounds " would read as a
// rendering fault, Rule 15).
func TestRoundRangeSentenceNamesBothEnds(t *testing.T) {
	got := gopilog.FormatTurnRoundRange(1, 3)
	if !strings.Contains(got, "1") || !strings.Contains(got, "3") {
		t.Fatalf("the interval sentence %q does not name both ends", got)
	}
	if empty := gopilog.FormatTurnRoundRange(0, 0); empty == got || !strings.Contains(empty, "no rounds") {
		t.Fatalf("an empty interval rendered as %q — it must say there is none", empty)
	}
}
