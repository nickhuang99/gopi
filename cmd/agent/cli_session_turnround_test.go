// cli_session_turnround_test.go — the CLI seam of the reverse lookup: the flag
// parse (one criterion, numeric ordinals) and the two rendered answers.
//
// WHAT THIS FILE DOES NOT DO, and why. It does not drive `runSessionShow` end to
// end: that verb resolves its operand through tools.SessionSourceShow, which
// answers from the CHAT INDEX or the runs tree — so an end-to-end test would
// have to seed a chats row or a run directory, and what it would then be
// measuring is the resolver, not this file's subject. The resolver is already
// covered by its own tests; here the SUBJECT is the join: the parse, the two
// sentences and the table. The real end-to-end run (a live session, three
// surfaces, the report's Ran:/Result: block) is recorded in this task's report.
package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// The fixture's two quantities, named (Rule 1): THREE turns, TWO rounds each.
const (
	joinTestTurns         = 3
	joinTestRoundsPerTurn = 2
)

// joinTestIndex builds the only fixture that can tell the two axes apart: three
// turns of two rounds each. A one-turn or one-round-per-turn fixture is BLIND —
// both numbers agree, so an index reading the wrong axis would still pass.
func joinTestIndex(t *testing.T) gopilog.TurnRoundIndex {
	t.Helper()
	s := &gopilog.Session{ID: "cli-join-fixture", Model: "fixture-model",
		RequestEvents: joinTestTurns * joinTestRoundsPerTurn}
	round := 0
	for turn := 0; turn < joinTestTurns; turn++ {
		for i := 0; i < joinTestRoundsPerTurn; i++ {
			round++
			it := gopilog.Iteration{Number: round - 1, RequestOrdinal: round}
			if i == 0 {
				it.TurnBoundary = true
				it.TurnSeamRecorded = true
				it.UserPrompt = "ask"
			}
			s.Iterations = append(s.Iterations, it)
		}
	}
	ix := gopilog.BuildTurnRoundIndex(s)
	if ix.Total() != joinTestTurns {
		t.Fatalf("fixture index holds %d turns, want %d", ix.Total(), joinTestTurns)
	}
	return ix
}

// TestSessionShowParsesTheLookupFlags pins the parse: both spellings of each
// flag, the session id surviving untouched, and ONE criterion per call.
func TestSessionShowParsesTheLookupFlags(t *testing.T) {
	t.Run("both spellings, id untouched", func(t *testing.T) {
		cases := []struct {
			argv []string
			want sessionTurnRoundLookup
		}{
			{[]string{"sid", "--round", "5"}, sessionTurnRoundLookup{round: 5}},
			{[]string{"sid", "--round=5"}, sessionTurnRoundLookup{round: 5}},
			{[]string{"--turn", "2", "sid"}, sessionTurnRoundLookup{turn: 2}},
			{[]string{"sid", "--turn=2", "--json"}, sessionTurnRoundLookup{turn: 2}},
			{[]string{"sid", "--mapping"}, sessionTurnRoundLookup{mapping: true}},
			{[]string{"sid"}, sessionTurnRoundLookup{}},
		}
		for _, c := range cases {
			lk, rest, err := parseSessionTurnRoundLookup(c.argv)
			if err != nil {
				t.Errorf("%v: %v", c.argv, err)
				continue
			}
			if lk != c.want {
				t.Errorf("%v parsed to %+v, want %+v", c.argv, lk, c.want)
			}
			joined := strings.Join(rest, " ")
			if !strings.Contains(joined, "sid") {
				t.Errorf("%v left the session id out of the remainder %q — a lookup must not eat the operand", c.argv, joined)
			}
			for _, flag := range []string{sessionShowTurnFlag, sessionShowRoundFlag, sessionShowMapFlag} {
				if strings.Contains(joined, flag) {
					t.Errorf("%v left %q in the remainder %q — the show parser would refuse it as an unknown flag", c.argv, flag, joined)
				}
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		cases := []struct {
			argv []string
			want string
		}{
			// TWO CRITERIA: one call asks one question.
			{[]string{"sid", "--turn", "2", "--round", "3"}, "pass one"},
			{[]string{"sid", "--mapping", "--round", "3"}, "pass one"},
			// A NON-NUMERIC or zero ordinal, refused BY NAME with the value.
			{[]string{"sid", "--round", "abc"}, "needs a number"},
			{[]string{"sid", "--round", "0"}, "needs a number"},
			{[]string{"sid", "--turn", "-1"}, "needs a number"},
		}
		for _, c := range cases {
			_, _, err := parseSessionTurnRoundLookup(c.argv)
			if err == nil {
				t.Errorf("%v was accepted, want a refusal", c.argv)
				continue
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("%v refused with %q, want it to say %q", c.argv, err, c.want)
			}
		}
	})
}

// TestSessionShowRendersBothLookups is the deliverable's user-visible half: each
// question gets ONE line, and that line carries BOTH unit words so neither
// number can be read as the other.
func TestSessionShowRendersBothLookups(t *testing.T) {
	ix := joinTestIndex(t)

	out, err := renderSessionTurnRound(ix, sessionTurnRoundLookup{round: 3})
	if err != nil {
		t.Fatalf("round lookup: %v", err)
	}
	for _, want := range []string{
		sessionevent.CounterUnitRound[:len(sessionevent.CounterUnitRound)-1], // "round"
		sessionevent.CounterUnitTurnOne,
		"rounds 3–4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the round lookup %q does not carry %q", out, want)
		}
	}
	if n := strings.Count(out, "\n"); n != 1 {
		t.Errorf("the round lookup printed %d line(s), want 1: %q", n, out)
	}

	out, err = renderSessionTurnRound(ix, sessionTurnRoundLookup{turn: 2})
	if err != nil {
		t.Fatalf("turn lookup: %v", err)
	}
	for _, want := range []string{sessionevent.CounterUnitTurnOne + " 2", "rounds 3–4", "3 " + sessionevent.CounterUnitTurn} {
		if !strings.Contains(out, want) {
			t.Errorf("the turn lookup %q does not carry %q", out, want)
		}
	}
}

// TestSessionShowMappingListsEveryTurn pins --mapping: one row per turn, a
// header naming both units, and a trailing count so a truncated listing
// discloses its own size (Rule 15).
func TestSessionShowMappingListsEveryTurn(t *testing.T) {
	ix := joinTestIndex(t)
	out, err := renderSessionTurnRound(ix, sessionTurnRoundLookup{mapping: true})
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// header + one row per turn + the count line
	if len(lines) != joinTestTurns+2 {
		t.Fatalf("mapping printed %d line(s), want %d:\n%s", len(lines), joinTestTurns+2, out)
	}
	if !strings.Contains(lines[0], sessionevent.CounterUnitTurnOne) || !strings.Contains(lines[0], sessionevent.CounterUnitRound) {
		t.Errorf("the mapping header %q does not name both units", lines[0])
	}
	for turn := 1; turn <= joinTestTurns; turn++ {
		if !strings.Contains(lines[turn], "rounds ") {
			t.Errorf("row %d (%q) carries no round interval", turn, lines[turn])
		}
	}
	if got, want := strings.TrimSpace(lines[len(lines)-1]), strings.TrimSpace(
		strings.Replace(sessionevent.CounterJoinTotalFmt, "%d", string(rune('0'+joinTestTurns)), 1)); !strings.Contains(got, "3") || !strings.Contains(got, sessionevent.CounterUnitTurn) {
		t.Errorf("the mapping's count line %q does not state %d turns (want shape %q)", got, joinTestTurns, want)
	}
}

// TestSessionShowLookupRefusalsNameTheFact pins the two refusals APART: an
// ordinal this session does not have, and (on a session with no round axis at
// all) the absence of the axis itself. Conflating them would tell a reader their
// typo was a property of the session (Rule 15).
func TestSessionShowLookupRefusalsNameTheFact(t *testing.T) {
	ix := joinTestIndex(t)
	if _, err := renderSessionTurnRound(ix, sessionTurnRoundLookup{round: 99}); err == nil {
		t.Errorf("round 99 was answered on a %d-round session", joinTestTurns*joinTestRoundsPerTurn)
	} else if !strings.Contains(err.Error(), "no such round") {
		t.Errorf("the out-of-range refusal said %q, want it to name the round", err)
	}
	if _, err := renderSessionTurnRound(ix, sessionTurnRoundLookup{turn: 99}); err == nil {
		t.Errorf("turn 99 was answered on a %d-turn session", joinTestTurns)
	} else if !strings.Contains(err.Error(), "no such turn") {
		t.Errorf("the out-of-range refusal said %q, want it to name the turn", err)
	}

	// A SESSION WITH NO CONTENT AT ALL has no axis to join against, and the
	// refusal says so rather than rendering an empty mapping.
	empty := gopilog.BuildTurnRoundIndex(&gopilog.Session{ID: "empty", Model: "m"})
	if _, err := renderSessionTurnRound(empty, sessionTurnRoundLookup{mapping: true}); err == nil {
		t.Errorf("an empty session produced a mapping, want a refusal naming the missing axis")
	}
}
