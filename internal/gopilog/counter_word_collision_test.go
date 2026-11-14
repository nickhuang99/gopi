package gopilog

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// counter_word_collision_test.go — the regression guard for the defect this
// change fixes, stated as a PROPERTY rather than as one golden string.
//
// THE DEFECT (measured 2026-10-16, session
// 5c6cd329-3e34-42c0-96b2-9a406961b1e1): three genuinely different quantities
// were printed under two shared words, inside one CLI.
//
//	quantity      value  producer
//	ROUND          134   JSONL `request` rows
//	LOOP PASS       31   session_end.total_iterations (the chat REPL's own count)
//	TURN            16   operator rows
//
// `gopi session log <id>` printed "Iterations: 31" (the PASS count) while
// `--rounds`, one flag away, headed a 134-row table with "… = one iteration"
// — so the word iteration named 134 on one surface and 31 on another. And
// `gopi session list` printed "turns 31" where `--turns` printed 16.
//
// WHY THE TEST IS SHAPED THIS WAY. A golden snapshot catches the strings that
// exist today; it does not catch a NEW surface that reintroduces the collision,
// and it says nothing about which number a word is allowed to carry. This test
// pins the ALLOWED PAIRING of (word, number) instead: for a session whose three
// counts DIFFER, each surface must print each count under the word that count
// owns, and must NOT print a count under a word another count owns.
//
// The fixture is built so the three differ (a session where the loop ran more
// passes than the JSONL has request rows, as a chat with a notify-driven turn
// does), because on a fixture where they are equal the collision is invisible —
// and a guard that cannot see the defect is not a guard.
func TestCounterWordsNeverCarryTwoNumbers(t *testing.T) {
	// THREE DIFFERENT NUMBERS, by construction: 4 request rows, 6 loop passes,
	// 2 operator rows.
	s := collisionFixtureSession(t)

	rounds := SessionRounds(s)
	passes := s.TotalIterations
	turns := SessionTurns(s)
	if rounds == passes || passes == turns || rounds == turns {
		t.Fatalf("the fixture must make the three counts DIFFER (rounds=%d passes=%d turns=%d) — a fixture where they coincide cannot see the collision",
			rounds, passes, turns)
	}

	// (1) THE COST BLOCK: each count under its own word, and the iteration line
	// carrying the ROUND's number — never the loop's.
	chat := FormatChat(s)
	for _, c := range []struct {
		word string
		why  string
	}{
		{sessionevent.CounterUnitRound, "the `request`-row count is the ROUND, and the word iteration is its own"},
		{sessionevent.CounterUnitHumanTurn, "the operator-row count is the TURN"},
		{sessionevent.CounterUnitLoopPass, "the loop's own count is a PASS — it must not be printed as an iteration or a turn"},
	} {
		if !strings.Contains(chat, c.word) {
			t.Errorf("FormatChat does not name %q at all (%s):\n%s", c.word, c.why, chat)
		}
	}
	// The line that prints the ROUND count must carry the ROUND's number.
	if !strings.Contains(chat, "Iterations: "+strconv.Itoa(rounds)) {
		t.Errorf("FormatChat's iteration line is not the ROUND count %d — the word iteration belongs to the `request`-row quantity:\n%s", rounds, chat)
	}
	// …and NOT the loop's. This is the exact collision: the old output printed
	// "Iterations: 31" where 31 was the loop's own count.
	if strings.Contains(chat, "Iterations: "+strconv.Itoa(passes)) {
		t.Errorf("FormatChat prints the LOOP PASS count %d as the iteration count — that is the collision this test exists for (rounds=%d, passes=%d):\n%s",
			passes, rounds, passes, chat)
	}
	// Same rule for the turn word: the TURN is the operator-row quantity.
	if strings.Contains(chat, "Turns: "+strconv.Itoa(rounds)) || strings.Contains(chat, "Turns: "+strconv.Itoa(passes)) {
		t.Errorf("FormatChat prints a non-turn count (%d or %d) on its Turns line; only the operator-row count %d is a turn:\n%s",
			rounds, passes, turns, chat)
	}

	// (2) THE TWO TABLES: the round table must not print the pass count under a
	// round word, and the turn table must not print the round count under a turn
	// word (the tables' own tests are per-surface; this one is the cross-surface
	// property).
	roundTable := FormatSessionRoundTable(s, SessionRoundRawOrdinals(s, nil), nil)
	turnTable := FormatSessionTurnTable(s)
	// The header reads "%d turn(s), %d rounds" (operator's ruling, 2026-10-22):
	// the turn leads and the round count carries the round's word. The property
	// this test defends is that neither quantity appears under the OTHER one's
	// word — so it asserts exactly that pair of mis-attributions, not a shape.
	if !strings.Contains(roundTable, strconv.Itoa(rounds)+" "+sessionevent.CounterUnitRound) {
		t.Errorf("the --rounds table does not tabulate the reader's %d round(s):\n%s", rounds, roundTable)
	}
	if strings.Contains(roundTable, strconv.Itoa(turns)+" "+sessionevent.CounterUnitRound) {
		t.Errorf("the --rounds table tabulates the TURN count %d as its rounds — the operator row is not a round (rounds=%d):\n%s",
			turns, rounds, roundTable)
	}
	// The subset's table banners "HUMAN TURNS — <id> (<n>)": the qualified word,
	// so the bare word `turns` stays the ledger's alone (2026-10-27).
	if want := "(" + strconv.Itoa(turns) + ")"; !strings.Contains(turnTable, SessionTurnTableWord()+" — ") || !strings.Contains(turnTable, want) {
		t.Errorf("the --turns table does not tabulate the reader's %d human turn(s):\n%s", turns, turnTable)
	}
	if strings.Contains(turnTable, "("+strconv.Itoa(rounds)+")") {
		t.Errorf("the --turns table tabulates the ROUND count %d as its human turns:\n%s", rounds, turnTable)
	}
	// The loop's count appears on both tables under ITS OWN word, never as the
	// table's own total.
	for name, body := range map[string]string{"--rounds": roundTable, "--turns": turnTable} {
		if !strings.Contains(body, SessionPassRowWord()) {
			t.Errorf("`%s` does not print the loop's own count under %q — the third quantity must be named, not dropped:\n%s",
				name, SessionPassRowWord(), body)
		}
	}

	// (3) NO SURFACE PRINTS THE OLD SPELLING. The literal "loop iterations" was
	// the phrase that carried the pass count; the constant no longer holds it,
	// and no rendered surface may reintroduce it by hand (Rule 13/14).
	for name, body := range map[string]string{"log --chat": chat, "log --rounds": roundTable, "log --turns": turnTable} {
		// The retired PHRASE only: a bare "loop iterations" in prose is the
		// spelling that made the loop's count read as the round count, so no
		// rendered surface may carry it (this test's own fixture prose is not a
		// surface and is not scanned here).
		if strings.Contains(body, "loop iterations") {
			t.Errorf("`%s` still prints %q — that spelling is how the pass count was read as an iteration count:\n%s",
				name, "loop iterations", body)
		}
	}
}

// collisionFixtureSession builds a session whose three counts are all DIFFERENT:
// 4 `request` rows (the ROUND), 6 loop passes (the loop's own count), 2 operator
// rows (the TURN). It is built in memory rather than parsed from a file because
// the property under test is about the RENDERERS, and a real log where the loop
// counted more passes than it made requests is not something a fixture file can
// be relied on to keep reproducing (the counts there are an artifact of that
// session's notify traffic).
func collisionFixtureSession(t *testing.T) *Session {
	t.Helper()
	s := &Session{
		ID:    "collision-fixture",
		Model: "fixture-model",
		// 4 request rows = the ROUND count.
		RequestEvents: collisionFixtureRounds,
		// 6 loop passes: the REPL charged two more turns than the log has
		// requests (a notify-driven turn and a failed one), which is exactly
		// the situation in which the old wording printed two numbers as one.
		TotalIterations: collisionFixturePasses,
		ElapsedSec:      1,
		Iterations:      make([]Iteration, 0, collisionFixtureRounds),
	}
	for i := 0; i < collisionFixtureRounds; i++ {
		it := Iteration{Number: i, RequestOrdinal: i + 1, PromptTokens: 10, CompletionTokens: 1}
		// TWO operator rows: the boundaries open turns 1 and 2 (SessionTurns
		// counts TurnBoundary marks), so the TURN count is 2 — a third,
		// different number.
		if i == 0 || i == 2 {
			it.TurnBoundary = true
			it.TurnSeamRecorded = true
			it.UserPrompt = "ask " + strconv.Itoa(i)
		}
		s.Iterations = append(s.Iterations, it)
	}
	return s
}

const (
	collisionFixtureRounds = 4
	collisionFixturePasses = 6
)
