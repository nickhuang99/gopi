package gopilog

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
)

// counterUnitExchangeForTest names the unit constant the header prints, so
// the assertion is against the definition rather than a copy of its wording.
func counterUnitExchangeForTest() string { return sessionevent.CounterUnitRound }

// TestCanonicalRoundIsTheRequestRow is the regression guard for the defect this
// numbering ended: four session surfaces answered "how many rounds?" with four
// different numbers on ONE session because "round" meant "the exchanges that
// carried content" in one place and "the sends that went over the wire" in
// another (measured 2026-10-11: replay 239 / log 251 / stats 343 on one session).
//
// The operator's definition — one HTTP request/response = one round = one
// `request` line — is asserted here on a fixture where the two candidate answers
// DIFFER, so a regression to either old meaning fails this test rather than
// passing it.
func TestCanonicalRoundIsTheRequestRow(t *testing.T) {
	// Three request rows; the middle exchange carries no content at all (the
	// shape the reader dropped: a call that ran and returned nothing, so the
	// old count said 2 where the wire said 3).
	lines := []string{
		`{"type":"session_start","sessionId":"s","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"do it"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one"},{"type":"tool_use","id":"t1","name":"bash","input":{"command":"echo 1"}}]}}`,
		// THE QUANTITY IS DECLARED (requestusage.BaseDelta), as every request row
		// the task path writes now does: this fixture is about NUMBERING, and an
		// untagged synthetic log is ambiguous by construction — its recorded total
		// need not agree with either reading, so a reader would have to guess
		// (epic 01a195b0).
		`{"type":"request","iteration":0,"prompt_tokens":10,"cache_hit":1,"cache_miss":9,"completion_tokens":2,"tagged_usage":true,"usage_base":"delta"}`,
		// NO content here: the exchange recorded usage and nothing else.
		`{"type":"request","iteration":1,"prompt_tokens":20,"cache_hit":2,"cache_miss":18,"completion_tokens":3,"tagged_usage":true,"usage_base":"delta"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"read","input":{"file_path":"/x"}}]}}`,
		`{"type":"request","iteration":2,"prompt_tokens":30,"cache_hit":4,"cache_miss":26,"completion_tokens":5,"tagged_usage":true,"usage_base":"delta"}`,
		`{"type":"session_end","exit_code":0,"total_iterations":1,"total_tokens":60,"elapsed_sec":1}`,
	}
	path := writeFixture(t, lines)
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// (1) THE CANONICAL COUNT IS THE REQUEST ROW COUNT — every surface that
	// numbers rounds reads this one number.
	if got, want := SessionRounds(s), s.RequestEvents; got != want {
		t.Errorf("SessionRounds = %d, want %d (one `request` row = one HTTP exchange)", got, want)
	}
	if SessionRounds(s) == SessionContentRounds(s) {
		t.Errorf("rounds = %d and content rounds = %d are equal on a fixture built to separate them — the content count must be the NAMED neighbour, not the round", SessionRounds(s), SessionContentRounds(s))
	}

	// (2) THE NUMBERING IS DENSE AND PAYS NO ATTENTION TO CONTENT: ordinal k is
	// the kth `request` row, so `--round 3` names row 3 even though only two
	// exchanges carried a body.
	rounds := SessionCanonicalRounds(s)
	if len(rounds) != 3 {
		t.Fatalf("canonical rounds = %d, want 3 (one per request row)", len(rounds))
	}
	if rounds[1].HasContent() {
		t.Errorf("round 2 carries content, want none — this is the row that made the two counts differ")
	}
	// The content of round 3 is the SECOND content round: the mapping is not the
	// content index, and a surface that used it would render round 3 as round 2.
	for _, want := range []struct{ ord, iter int }{{1, 0}, {3, 1}} {
		r, ok := SessionCanonicalRound(s, want.ord)
		if !ok || r.Iter != want.iter {
			t.Errorf("round %d resolves to content index %d (ok=%v), want %d", want.ord, r.Iter, ok, want.iter)
		}
	}

	// (3) THE STATS TABLE NUMBERS THE SAME ROUNDS: a row per request row,
	// with the canonical ordinal in RND (it used to print the log's `iteration`
	// field, which restarts at 0 inside one session).
	st := BuildStatsFromJSONL(s)
	if len(st.RoundStats) != 3 {
		t.Fatalf("stats rows = %d, want 3 (one per request row)", len(st.RoundStats))
	}
	for i := range st.RoundStats {
		if st.RoundStats[i].Number != i+1 {
			t.Errorf("stats row %d is numbered %d, want %d (canonical ordinal)", i, st.RoundStats[i].Number, i+1)
		}
	}
	if st.RoundStats[2].PromptTokens != 30 {
		t.Errorf("stats round 3 prompt = %d, want 30 — the value of the THIRD request row", st.RoundStats[2].PromptTokens)
	}

	// (4) A TOOL CALL IS NOT A ROUND, and the call's round is the canonical one
	// (call 2 belongs to round 3 because round 2 carried no content).
	calls := SessionToolCalls(s)
	if len(calls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(calls))
	}
	if calls[1].Ordinal != 2 || calls[1].RoundOrdinal != 3 {
		t.Errorf("call 2 = ordinal %d in round %d, want ordinal 2 in round 3", calls[1].Ordinal, calls[1].RoundOrdinal)
	}
	if SessionToolCallCount(s) == SessionRounds(s) {
		t.Errorf("tool calls (%d) == rounds (%d) on a fixture where they must differ", SessionToolCallCount(s), SessionRounds(s))
	}

	// (5) THE RENDERED HEADER STATES THE UNIT AND BOTH NEIGHBOURS, and a round
	// with no content SAYS SO rather than printing a neighbour's body.
	hdr := FormatSessionRoundWithRaw(s, rounds[0].Iter, 1, 0)
	for _, want := range []string{sessionRoundCanonicalUnit, "ROUND 1 of 3", counterUnitExchangeForTest()} {
		if !strings.Contains(hdr, want) {
			t.Errorf("round header missing %q:\n%s", want, hdr)
		}
	}
	empty := FormatSessionRoundWithRaw(s, rounds[1].Iter, 2, 0)
	if !strings.Contains(empty, sessionRoundNoContentWord) {
		t.Errorf("round 2's view does not state that its exchange carried no content:\n%s", empty)
	}
	if strings.Contains(empty, "one") {
		t.Errorf("round 2's view printed another round's content:\n%s", empty)
	}
}
