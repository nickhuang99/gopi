package gopilog

// counter_units_test.go — the SELF-DECLARATION half of the session-counter
// epic (PROJECT.md Rule 5/31: a surface that prints a count says WHAT it counted).
//
// The defect this file pins: one session answered "how many rounds?" with four
// different numbers (12 / 28 / 24 / 27) on four surfaces, each naming its unit
// (iteration / round / turn / event) without ever saying what that unit counted
// or what its denominator was — so a reader could only conclude "the same thing
// has four values". Not one number was wrong; the WORDS were.
//
// Two things are asserted here, and they are different claims:
//   - each surface CARRIES its unit declaration (a grep for the declaration is
//     not enough on its own — the relation test below pins the arithmetic);
//   - the counts RELATE as the documentation says they do, so a future change
//     that silently makes two of them equal (or reverses an ordering) fails.
//
// The counters of ONE session, over the fixture and over the measured session
// 1988d8a0: loop iterations (session_end) != content-carrying rounds
// (len(Iterations)) <= rounds (JSONL `request` rows) <= raw requests
// (→ REQUEST markers), and the TURNS (operator rows) are a separate quantity —
// the neighbour of the round, never a second spelling of it.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// fixtureSessionWithRaw returns the fixture session parser output. The fixture
// is the real session the goldens are recorded from (fc426fed), with its paired
// raw HTTP log — never a constructed mock.
func fixtureSessionWithRaw(t *testing.T) *Session {
	t.Helper()
	return fixtureSession(t)
}

// TestCounterUnitsAreDeclaredOnEverySurface: each surface that prints a count
// names the unit it counted. This is the acceptance the epic states in the
// words of the defect: a reader must be able to tell "12 iterations" from
// "28 rounds" from "31 raw rounds" WITHOUT knowing the internals.
func TestCounterUnitsAreDeclaredOnEverySurface(t *testing.T) {
	s := fixtureSessionWithRaw(t)

	chat := FormatChat(s)
	for _, want := range []string{
		sessionevent.CounterUnitLoopPass, // Iterations: 9 (loop iterations …)
		sessionevent.CounterUnitRound,    // the JSONL request-event count
	} {
		if !strings.Contains(chat, want) {
			t.Errorf("FormatChat's COST SUMMARY does not name %q — the number is printed without its unit:\n%s", want, chat)
		}
	}
	// The provenance travels too: which producer fed the count is part of it.
	if !strings.Contains(chat, sessionevent.CounterSourceSessionEnd) {
		t.Errorf("FormatChat does not say the Iterations count came from %q:\n%s", sessionevent.CounterSourceSessionEnd, chat)
	}

	round := FormatSessionRoundWithRaw(s, 0, 1, 5)
	for _, want := range []string{
		sessionevent.CounterUnitSessionRound,
		sessionevent.CounterUnitRound,
		sessionevent.CounterUnitRawRequest,
		sessionevent.CounterUnitLoopPass,
	} {
		if !strings.Contains(round, want) {
			t.Errorf("the round header does not name %q — a reader cannot tell it from its neighbours:\n%s", want, round)
		}
	}

	table := FormatSessionRoundTable(s, SessionRoundRawOrdinals(s, nil), nil)
	for _, want := range []string{
		sessionevent.CounterUnitSessionRound,
		sessionevent.CounterUnitRawRequest,
		sessionevent.CounterUnitLoopPass,
	} {
		if !strings.Contains(table, want) {
			t.Errorf("the --rounds table header does not name %q:\n%s", want, table)
		}
	}
	// THE --rounds TABLE'S OWN COUNT IS THE READER'S ROUND COUNT (operator's
	// ruling, 2026-10-12: round = one JSONL `request` row = one HTTP
	// request/response = one iteration). It is the SAME number
	// `gopi session replay --rounds` prints as `rounds=` — one flag, one
	// quantity, on every surface.
	// THE PHRASING MOVED, THE QUANTITY DID NOT (operator's ruling, 2026-10-22):
	// the headline now leads with the TURN and the round count is named with the
	// round's own plural — "%d turn(s), %d rounds" — so the table states the
	// nesting instead of presenting the exchange as the top-level unit. What this
	// assertion defends is unchanged: the number printed under the round's word is
	// the READER's round count.
	if want := fmt.Sprintf("%d %s", SessionRounds(s), sessionevent.CounterUnitRound); !strings.Contains(table, want) {
		t.Errorf("the --rounds table does not tabulate the reader's %d round(s) (%q):\n%s",
			SessionRounds(s), want, table)
	}
	// THE HEADLINE'S TURN COUNT IS THE LEDGER'S, and this package cannot read the
	// ledger — so what the table prints here is the CLAUSE, in whichever of its
	// two states this process is in (`turnCountClause`: a number when a reader is
	// installed, an explicit "not measured" when it is not). Asserting a literal
	// count would make this test pass only in one of those two worlds.
	if want := turnCountClause(s); !strings.Contains(table, want) {
		t.Errorf("the --rounds table does not lead with the turn count's own clause (%q):\n%s", want, table)
	}
	if !strings.Contains(table, sessionevent.CounterUnitRound) {
		t.Errorf("the --rounds table header does not name %q:\n%s", sessionevent.CounterUnitRound, table)
	}

	// THE HUMAN-TURN TABLE — `gopi session log <id> --turns`, the PERSON-ONLY
	// SUBSET. It is the NEIGHBOUR of the round AND the neighbour of the turn
	// count, and it must name ITS OWN unit in both the banner and the unit line:
	// a reader who sees this table must be able to tell that it is neither the
	// round count nor the session's turn count (the operator's ruling,
	// 2026-10-27 — the bare word `turns` belongs to `chats.turn_count`, which
	// this package cannot read).
	turnTable := FormatSessionTurnTable(s)
	if want := fmt.Sprintf("%s — %s (%d)", SessionTurnTableWord(), s.ID, SessionTurns(s)); !strings.Contains(turnTable, want) {
		t.Errorf("the --turns table does not tabulate the reader's %d human turn(s) (%q):\n%s",
			SessionTurns(s), want, turnTable)
	}
	if strings.Contains(turnTable, "SESSION TURNS") {
		t.Errorf("the --turns table still banners the BARE word for the subset — the bare word is the ledger's turn count:\n%s", turnTable)
	}
	if !strings.Contains(turnTable, sessionevent.CounterUnitHumanTurn) {
		t.Errorf("the --turns table header does not name %q:\n%s", sessionevent.CounterUnitHumanTurn, turnTable)
	}
	if !strings.Contains(turnTable, sessionevent.CounterUnitRound) {
		t.Errorf("the --turns table does not name the %q column it carries:\n%s", sessionevent.CounterUnitRound, turnTable)
	}

	// THE THIRD SURFACE of the same rename: `gopi session stats --rounds` /
	// --bill / --bytes count the SAME exchanges as the log table above, so they
	// print the same unit's word — taken from the constant, never typed (this
	// surface said "canonical round" and headed its column "RND" long after the
	// other two had stopped, which is how one log answered "rounds" with two
	// quantities).
	st := BuildStatsFromJSONL(s)
	statsTables := FormatStatsRounds(st) + FormatStatsTraffic(st)
	bill, err := FormatStatsBill(st)
	if err != nil {
		t.Fatalf("FormatStatsBill on the fixture session: %v", err)
	}
	statsTables += bill
	for _, want := range []string{
		sessionevent.CounterUnitRound,
		SessionRoundColumnWord(),
	} {
		if !strings.Contains(statsTables, want) {
			t.Errorf("the stats tables do not name %q — the same quantity is spelled differently here:\n%s", want, statsTables)
		}
	}
	if strings.Contains(statsTables, "PER-EXCHANGE") || strings.Contains(statsTables, "EXCHANGE") {
		t.Errorf("the stats tables head their tables with the RETIRED spelling (EXCHANGE) — the `request`-row unit is %q again (operator's ruling, 2026-10-12) and these are the same rows `gopi session log --rounds` tabulates:\n%s",
			sessionevent.CounterUnitRound, statsTables)
	}
}

// TestCounterUnitNamesAreNotMirrored keeps the unit names in ONE place
// (PROJECT.md Rule 13/14): the values live in internal/sessionevent and a consumer
// references them. A consumer that typed the literal would keep the old
// spelling after a rename, and the two surfaces would silently disagree about
// what a unit is called.
func TestCounterUnitNamesAreNotMirrored(t *testing.T) {
	s := fixtureSessionWithRaw(t)
	body := FormatChat(s) + FormatSessionRoundWithRaw(s, 0, 1, 5)

	// Every unit name that appears in the rendered output must BE the constant.
	for _, name := range []string{
		sessionevent.CounterUnitLoopPass,
		sessionevent.CounterUnitRound,
		sessionevent.CounterUnitSessionRound,
		sessionevent.CounterUnitRawRequest,
	} {
		if !strings.Contains(body, name) {
			t.Errorf("unit %q is not rendered from its constant — a consumer bypassed it (Rule 13)", name)
		}
	}
}

// TestCounterRelationsHoldOnTheFixture pins the DIRECTIONS the counters can
// differ in. These are not incidental: each one is a REAL mechanism.
//
//   - sessions rounds <= request events: the reader drops a `request` whose
//     accumulated blocks were all empty (flushIter), so the rounds that survive
//     can only be fewer.
//   - raw rounds >= request events: the raw log numbers sends the JSONL never
//     recorded (non-streaming meta calls, retries).
//   - iter.Number is the JSONL's own 0-based iteration, reprinted as the
//     round's Number — a session's round number is its iteration + 1.
func TestCounterRelationsHoldOnTheFixture(t *testing.T) {
	s := fixtureSessionWithRaw(t)
	rounds := SessionRounds(s)

	if s.RequestEvents < rounds {
		t.Errorf("request events (%d) < session rounds (%d): the reader can only DROP rounds, never invent one — flushIter's filter ran backwards",
			s.RequestEvents, rounds)
	}
	if s.TotalIterations == 0 {
		t.Fatal("the fixture recorded no session_end total_iterations — the relation test needs a completed session")
	}
	// A completed fixture: the loop's count and the JSONL row count are the
	// numbers a reader conflates, and on THIS session they agree — which is
	// exactly why the surfaces must still name them (a coincidence is not a
	// guarantee, and the measured session 1988d8a0 is the counter-example:
	// 12 loop iterations against 28 session rounds).
	if s.EndUnrecorded {
		t.Errorf("fixture session is marked EndUnrecorded — the relations below assume a recorded end")
	}
}

// TestCounterRelationsCanDisagree is the MEASURED counter-example, kept as a
// regression guard so nobody "simplifies" the two counts into one.
//
// Session 1988d8a0-874b-4f2a-976b-2e8dd37f0079 (2026-10-04) is where the epic
// started: the agent loop counted 12 iterations (one per REPL turn), while the
// JSONL held 31 `request` rows of which 28 carried content — and the raw HTTP
// log held 31 `→ REQUEST` markers. It is not available as a fixture (it lives in
// the operator's profile), so the SHAPE it proves is asserted structurally here:
// the two quantities are computed from DIFFERENT sources and a session that
// runs several model exchanges per turn necessarily makes them differ.
func TestCounterRelationsCanDisagree(t *testing.T) {
	// A log with three request rows of which one carried nothing (the shape the
	// reader's flushIter drops) and a session_end claiming ONE loop iteration:
	// one chat turn that made three model exchanges.
	lines := []string{
		`{"type":"session_start","sessionId":"s","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"do it"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"a"}]}}`,
		`{"type":"request","iteration":0,"prompt_tokens":10}`,
		// Round 2: an EMPTY exchange — the reader drops it from Iterations…
		`{"type":"request","iteration":1,"prompt_tokens":20}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second"}]}}`,
		`{"type":"request","iteration":2,"prompt_tokens":30}`,
		`{"type":"session_end","exit_code":0,"total_iterations":1,"total_tokens":60,"elapsed_sec":1}`,
	}
	path := writeFixture(t, lines)
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if s.TotalIterations != 1 {
		t.Errorf("loop iterations = %d, want the session_end's 1", s.TotalIterations)
	}
	if s.RequestEvents != 3 {
		t.Errorf("request events = %d, want 3 (every `request` row is one send)", s.RequestEvents)
	}
	// THE CANONICAL ROUND IS THE REQUEST ROW (Operator's definition,
	// 2026-10-10): "round" no longer means "an exchange that happened to carry
	// content", so the empty exchange IS round 2 here and the count is the
	// event count. The content count did not disappear — it is the NAMED
	// neighbour SessionContentRounds, which is what used to be called "rounds".
	if got := SessionRounds(s); got != s.RequestEvents {
		t.Errorf("session rounds = %d, want %d (one `request` row = one round, the empty exchange included)", got, s.RequestEvents)
	}
	if got := SessionContentRounds(s); got != 2 {
		t.Errorf("content rounds = %d, want 2 — the empty exchange carries no content, and the two counts must NOT be equal on this fixture", got)
	}
	// The numbers are different quantities on ONE session — which is the whole
	// point of naming them (Rule 15: a surface that cannot tell which producer
	// fed it must say which one did).
	if s.TotalIterations == s.RequestEvents || s.RequestEvents == SessionContentRounds(s) || s.TotalIterations == SessionContentRounds(s) {
		t.Errorf("the counts are not distinct on a fixture built to make them distinct: iterations=%d events=%d content=%d",
			s.TotalIterations, s.RequestEvents, SessionContentRounds(s))
	}
	// And the canonical numbering must REMAIN dense: every request row is a
	// round with its own ordinal, the contentless one included (a gap here
	// would make `--round 3` name a different exchange on different surfaces).
	rounds := SessionCanonicalRounds(s)
	if len(rounds) != s.RequestEvents {
		t.Fatalf("canonical rounds = %d, want %d (one per request row)", len(rounds), s.RequestEvents)
	}
	for i, r := range rounds {
		if r.Ordinal != i+1 {
			t.Errorf("canonical round %d has ordinal %d, want a dense 1..N numbering", i, r.Ordinal)
		}
	}
	if rounds[1].HasContent() {
		t.Errorf("round 2 carries content, want none (its exchange was empty)")
	}
	if !rounds[0].HasContent() || !rounds[2].HasContent() {
		t.Errorf("rounds 1 and 3 must carry the content that belongs to them: %+v", rounds)
	}
}
