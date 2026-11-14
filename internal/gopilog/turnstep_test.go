package gopilog

// turnstep_test.go — THE TURN IS THE UNIT (operator's ruling, 2026-10-22).
//
// These pin the shape the session surfaces print, at the smallest sample that
// can tell it apart from the shape it replaces: one session whose ONE operator
// row was answered with THREE request/response exchanges. Under the old
// spelling that session had three "rounds" and no visible turn; under this one
// it has ONE turn carrying "3 request(s)" as its detail.
//
// The reference shape is `gopi chat --replay`'s notice (chatReplayUses): the
// turn count first, the inner quantities as a parenthetical.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// TestTurnStepLabelSpellsTurnOuterDetailInner is the ONE renderer every
// turn-stepped surface calls.
func TestTurnStepLabelSpellsTurnOuterDetailInner(t *testing.T) {
	cases := []struct {
		name                     string
		index, total, req, calls int
		want                     []string
	}{
		{"turn with both inner quantities", 3, 8, 5, 12,
			[]string{"turn 3/8", "(5 request(s), 12 tool call(s))"}},
		{"a turn that ran no tool", 2, 8, 1, 0,
			[]string{"turn 2/8", "(1 request(s))"}},
		{"a turn with nothing recorded inside", 1, 1, 0, 0,
			[]string{"turn 1/1", sessionevent.CounterUnitTurnNoDetail}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FormatTurnStepLabel(c.index, c.total, c.req, c.calls)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("label %q does not carry %q", got, want)
				}
			}
			// TURN FIRST: the label opens with the unit word and the position,
			// so a reader scanning the column sees the ask before its detail.
			head := sessionevent.CounterUnitTurnOne + " " + strconv.Itoa(c.index)
			if !strings.HasPrefix(got, head) {
				t.Errorf("label %q does not open with %q — the turn must be OUTSIDE its detail", got, head)
			}
			// NO BARE PARENTHESES: an empty detail states its absence instead of
			// rendering "()", which reads like a rendering fault.
			if strings.Contains(got, "()") {
				t.Errorf("label %q renders an empty parenthetical", got)
			}
		})
	}
}

// TestTurnSpansCountTheRequestsInsideOneTurn is the measurement behind the
// label: the turn's span holds the exchanges, and the count is the canonical
// round count inside it — never the number of turns.
func TestTurnSpansCountTheRequestsInsideOneTurn(t *testing.T) {
	// ONE operator row (one TurnBoundary) answered with THREE request rows: the
	// smallest sample on which "how many turns" and "how many requests" give
	// different answers (1 vs 3).
	// RequestEvents is the CANONICAL round count (SessionRounds reads it): the
	// fixture states it so the two quantities — 3 requests, 1 turn — are both
	// real numbers rather than one of them being zero by omission.
	s := &Session{ID: "turn-fixture", Model: "fixture-model", RequestEvents: 3}
	for i := 0; i < 3; i++ {
		it := Iteration{Number: i, RequestOrdinal: i + 1, PromptTokens: 10, CompletionTokens: 1}
		if i == 0 {
			it.TurnBoundary = true
			it.TurnSeamRecorded = true
			it.UserPrompt = "the one ask"
		}
		s.Iterations = append(s.Iterations, it)
	}

	spans := SessionTurnSpans(s)
	if len(spans) != 1 {
		t.Fatalf("SessionTurnSpans = %d turn(s), want 1: one operator row is one turn", len(spans))
	}
	if got, want := spans[0].Requests, SessionRounds(s); got != want {
		t.Fatalf("the turn carries %d request(s), want the reader's %d rounds — the inner quantity must be the canonical round count", got, want)
	}
	if spans[0].Requests == len(spans) {
		t.Fatalf("the inner count %d equals the turn count — the fixture cannot tell the two quantities apart, so it proves nothing", spans[0].Requests)
	}
	if spans[0].Turn != 1 {
		t.Fatalf("the turn is numbered %d, want 1", spans[0].Turn)
	}
	if SessionTurns(s) != len(spans) {
		t.Fatalf("SessionTurns = %d but SessionTurnSpans = %d — two answers to one question", SessionTurns(s), len(spans))
	}
	// The table's row agrees with the span (Rule 14: one derivation).
	rows := SessionTurnRows(s)
	if len(rows) != 1 || rows[0].Rounds != spans[0].Requests {
		t.Fatalf("SessionTurnRows disagrees with the spans: rows=%+v spans=%+v", rows, spans)
	}
	// And the SPAN is where the tool calls are counted, so the label's second
	// inner quantity is measured rather than assumed.
	if spans[0].Start != 0 || spans[0].End != 3 {
		t.Fatalf("the turn's span is [%d,%d), want [0,3) — an open-ended turn runs to the end of the log", spans[0].Start, spans[0].End)
	}
}

// TestStatsOverviewLeadsWithTheTurnAndNamesRequestsBesideIt pins the stats box's
// shape: the headline is the TURN count, the number under it is the REQUEST
// count under the request's own word, and the round word does not appear as the
// headline (Rule 14: one meaning per word).
func TestStatsOverviewLeadsWithTheTurnAndNamesRequestsBesideIt(t *testing.T) {
	// RequestEvents is the CANONICAL round count (SessionRounds reads it): the
	// fixture states it so the two quantities — 3 requests, 1 turn — are both
	// real numbers rather than one of them being zero by omission.
	s := &Session{ID: "turn-fixture", Model: "fixture-model", RequestEvents: 3}
	for i := 0; i < 3; i++ {
		it := Iteration{Number: i, RequestOrdinal: i + 1, PromptTokens: 10, CompletionTokens: 1}
		if i == 0 {
			it.TurnBoundary = true
			it.TurnSeamRecorded = true
			it.UserPrompt = "the one ask"
		}
		s.Iterations = append(s.Iterations, it)
	}
	st := BuildStatsFromJSONL(s)
	if st.HumanTurns != 1 || st.Rounds != 3 {
		t.Fatalf("the fixture's stats are turns=%d rounds=%d, want 1 and 3 — a fixture where they coincide makes this test blind", st.HumanTurns, st.Rounds)
	}
	box := FormatStatsOverview(st)
	if !strings.Contains(box, sessionevent.CounterUnitTurn) {
		t.Errorf("the overview box does not lead with %q:\n%s", sessionevent.CounterUnitTurn, box)
	}
	if !strings.Contains(box, sessionevent.CounterUnitInnerRequest) {
		t.Errorf("the overview box does not print the request count under %q:\n%s", sessionevent.CounterUnitInnerRequest, box)
	}
	// The request line carries the REQUEST's number, and the turn line carries
	// the TURN's — the whole point of the change is that these two are not
	// swapped.
	// THE LINE IS FOUND BY ITS KEY, NOT BY A SUBSTRING: `other turns` contains
	// `turns`, so a Contains scan selects the WRONG line (measured: this arm
	// reported `other turns 0` after the change, i.e. it was reading a different
	// counter than the one it claims to check).
	keyLine := func(k string) string {
		prefix := "\u2502 " + k + " "
		for _, line := range strings.Split(box, "\n") {
			if strings.HasPrefix(line, prefix) {
				return line
			}
		}
		return ""
	}
	turnLine, reqLine := keyLine(sessionevent.CounterUnitTurn), keyLine(sessionevent.CounterUnitInnerRequest)
	// THE TURN LINE CARRIES THE LOG'S OPENED COUNT (epic 2026-11-04), which on
	// this fixture happens to equal its human subset (both 1) — asserted against
	// the field the render actually reads, so a future change cannot pass here by
	// coincidence of the fixture's numbers.
	if !strings.Contains(turnLine, strconv.Itoa(st.TurnsOpened)) {
		t.Errorf("the turn line %q does not carry the log's opened count %d", turnLine, st.TurnsOpened)
	}
	if !strings.Contains(reqLine, strconv.Itoa(st.Rounds)) {
		t.Errorf("the request line %q does not carry the request count %d", reqLine, st.Rounds)
	}
}

// TestStatsOverviewFallsBackToTheRoundWhenNoTurnWasRecorded: a source with no
// operator rows (a raw log, a usage scan) has NO turn boundary to print, so the
// box keeps the round headline it always had — and the aggregate word follows
// the same choice. A branch nothing executes is the shape Rule 15 names, so it
// gets its own repro rather than riding on the jsonl path.
func TestStatsOverviewFallsBackToTheRoundWhenNoTurnWasRecorded(t *testing.T) {
	raw := &SessionStats{
		SessionID: "raw-only", Source: StatsSourceRaw, Sessions: 1,
		Rounds: 2, HumanTurns: 0,
		RoundStats: []RoundStat{
			{Number: 1, PromptTokens: 100, CacheHit: 90, CacheMiss: 10},
			{Number: 2, PromptTokens: 200, CacheHit: 190, CacheMiss: 10},
		},
	}
	box := FormatStatsOverview(raw)
	if strings.Contains(box, sessionevent.CounterUnitTurn) {
		t.Fatalf("a turn-less source printed a %q headline:\n%s", sessionevent.CounterUnitTurn, box)
	}
	if !strings.Contains(box, sessionevent.CounterUnitRound) {
		t.Fatalf("a turn-less source did not fall back to the %q headline:\n%s", sessionevent.CounterUnitRound, box)
	}
	// The aggregate follows the same word: this source folded its numbers over
	// ROUNDS, so calling them per-request would name a quantity it never had.
	if !strings.Contains(box, "per-"+roundWord()) {
		t.Fatalf("the turn-less box's aggregate row does not say per-%s:\n%s", roundWord(), box)
	}
}

// TestRoundViewNamesTheTurnBesideItsAddress: `session log <id> 3` renders ONE
// exchange, and that exchange is still addressed as ROUND 3 — while the header
// also says which of the person's asks it helped answer. The address must not
// move (callers pass it); the subject must be visible (that is the ruling).
func TestRoundViewNamesTheTurnBesideItsAddress(t *testing.T) {
	s := &Session{ID: "turn-fixture", Model: "fixture-model", RequestEvents: 3,
		Iterations: []Iteration{
			{Number: 0, RequestOrdinal: 3, TurnBoundary: true, TurnSeamRecorded: true, UserPrompt: "the one ask", PromptTokens: 10, CompletionTokens: 1},
		}}
	header := FormatSessionRoundWithRaw(s, 0, 3, 0)
	want := "ROUND 3 of 3  (turn 1 of 1)"
	if !strings.Contains(header, want) {
		t.Fatalf("the round view's header does not carry %q — the ADDRESS and the SUBJECT must both be visible:\n%s", want, header)
	}
	// THE ADDRESS STILL RESOLVES: round 3 is what the caller asked for and what
	// the denominator says, whatever the turn numbering is.
	if !strings.Contains(header, "ROUND 3 of 3") {
		t.Fatalf("the round address moved:\n%s", header)
	}
}

// TestRoundViewOmitsTheTurnWhenNoneWasRecorded: a source that records NO
// iteration at all (the raw-log path: exchanges without operator rows) has no
// turn to name, and the header says nothing about one rather than "turn 0 of 0" —
// a fabricated zero would state a count the source never measured (Rule 15).
//
// THE FIXTURE HAS TO BE EMPTY OF ITERATIONS, not merely free of TurnBoundary
// markers: SessionTurnBounds falls back to iteration 0 for a session whose first
// row carries no boundary, so a one-iteration fixture WOULD legitimately report
// one turn — that fallback is the reader's own ("this log's content starts
// here"), and it is the reason this case is pinned at the empty end.
func TestRoundViewOmitsTheTurnWhenNoneWasRecorded(t *testing.T) {
	s := &Session{ID: "raw-only", Model: "fixture-model", RequestEvents: 1}
	if turn, total := turnOfIteration(s, 0); turn != 0 || total != 0 {
		t.Fatalf("turnOfIteration(no iterations) = (%d,%d), want (0,0)", turn, total)
	}
	header := FormatSessionRoundWithRaw(s, 0, 1, 0)
	if strings.Contains(header, sessionevent.CounterUnitTurnOne) {
		t.Fatalf("the header claims a turn the source never recorded:\n%s", header)
	}
	if !strings.Contains(header, "ROUND 1 of 1") {
		t.Fatalf("the round address is missing:\n%s", header)
	}
}

// TestStatsRoundsTableNamesTheTurnOfEachRow: the per-round table is the DETAIL
// of the turn-led view, so each row must say which turn it answered — and the
// column appears only when the source recorded turns.
func TestStatsRoundsTableNamesTheTurnOfEachRow(t *testing.T) {
	withTurns := &SessionStats{
		SessionID: "turn-fixture", Source: StatsSourceJSONL, Sessions: 1,
		Rounds: 2, HumanTurns: 1,
		RoundStats: []RoundStat{
			{Number: 1, Turn: 1, PromptTokens: 100, CacheHit: 90, CacheMiss: 10},
			{Number: 2, Turn: 1, PromptTokens: 200, CacheHit: 190, CacheMiss: 10},
		},
	}
	out := FormatStatsRounds(withTurns)
	if !strings.Contains(out, sessionTurnColumnWord) {
		t.Fatalf("the per-round table has no %q column although every row has a turn:\n%s", sessionTurnColumnWord, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "memory") {
			continue
		}
	}
	// Both rows belong to turn 1 — the numbers differ, the turn does not, which
	// is exactly the nesting the ruling asks a reader to see.
	if strings.Count(out, "    1        1") != 1 || !strings.Contains(out, "    1        2") {
		t.Fatalf("the table does not show two rounds under one turn:\n%s", out)
	}

	rawOnly := &SessionStats{
		SessionID: "raw-only", Source: StatsSourceRaw, Sessions: 1, Rounds: 1,
		RoundStats: []RoundStat{{Number: 1, PromptTokens: 100, CacheHit: 90, CacheMiss: 10}},
	}
	if got := FormatStatsRounds(rawOnly); strings.Contains(got, sessionTurnColumnWord) {
		t.Fatalf("a raw-only table printed a %q column with no turn data:\n%s", sessionTurnColumnWord, got)
	}
}
