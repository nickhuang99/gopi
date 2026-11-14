// Regression gate for the defect this task repaired (Rule 5: every fix
// ships a minimal repro).
//
// THE DEFECT, measured 2026-11-01 on the dispatched RUN session
// 01a1944e-900f-70b7-81ac-24eda447b38b: `gopi session replay <id> --turns`
// printed
//
//	turns=127
//	  1   turn 1/127   (1 request(s), 2 tool call(s))   rounds 1–1
//	  2   turn 2/127   (1 request(s), 5 tool call(s))   rounds 2–2
//	  …
//
// — one turn PER ROUND, each label spanning exactly one round. That log holds
// 127 `request` rows and 136 `user` rows, and NOT ONE of the 136 opened a turn:
// 133 are `[system]` injections (clock ticks, checkpoints, the SELF-RECORD
// carrier) and the rest are state rows. A run session has no operator, so its
// only complete input is the briefing it was dispatched with.
//
// TWO LAYERS WERE WRONG, and both are pinned below:
//
//  1. `sessionevent.IsTurnOpeningRow` called the briefing a harness injection
//     (`HarnessBriefingOnStdinMarker` is in HarnessMessageMarkers), so the run's
//     real input opened nothing;
//  2. with no seam in the log, `readRequestRoundSeams` (internal/gopilog/
//     reader.go) returned true and the reader stamped EVERY content round a
//     turn boundary — the exchange count wearing the turn's word, which is the
//     exact substitution that function's own comment says it exists to prevent.
//
// WHY THE FIXTURE IS THE SHAPE IT IS (Rule 10.4: the smallest sample that can
// still tell the two answers apart): it must have MANY rounds and FEWER turns —
// a fixture with equal counts cannot distinguish "a turn per input" from "a turn
// per exchange", which is the whole question. It is RUN-shaped: one briefing
// row, then N exchanges whose only user rows are harness ticks.
package gopilog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// runShapeRounds is how many exchanges the repro log holds. More than one so the
// collapsed count is a COUNT rather than the single-row fallback, and unequal to
// runShapeTurns so "rounds > turns" is measurable.
const runShapeRounds = 5

// runShapeTurns is how many turns the repro log really has: ONE — the briefing
// the run was dispatched with. Every exchange after it belongs to that turn.
const runShapeTurns = 1

// writeRunShapedLog writes a session log of the shape a dispatched run records:
// a session_start, the BRIEFING the parent marked onto stdin, then N exchanges
// in which the only `user` rows are harness ticks (the same shape
// human_turns_unmeasured_test.go builds, with the briefing added — which is the
// one row that distinguishes a run from a log nobody typed into).
func writeRunShapedLog(t *testing.T) string {
	t.Helper()
	lines := []string{
		`{"sessionId":"run-shaped","model":"fixture-model","timestamp":"2026-11-01T00:00:00Z","type":"session_start"}`,
		// THE RUN'S INPUT: a briefing that went through the stdin seam, which is
		// what a dispatched child records. It is the ONLY row in this log that
		// is one complete input.
		fmt.Sprintf(`{"message":{"content":%q,"role":"user"},"type":"user"}`,
			sessionevent.BriefingOnStdin("[Task summary] clear the idle commits left in the workspace clones")),
	}
	for i := 0; i < runShapeRounds; i++ {
		if i > 0 {
			// A CLOCK TICK between exchanges: machinery inside the turn that
			// already exists, so it opens nothing
			// (sessionevent.IsTurnOpeningRow). It is written BETWEEN rounds, not
			// before the first one, because a tick that arrives before any round
			// is open is HELD and attached to the round about to start — so a
			// tick before round 1 would put the tick, not the briefing, in the
			// first round's prompt.
			lines = append(lines,
				fmt.Sprintf(`{"message":{"content":%q,"role":"user"},"type":"user"}`,
					sessionevent.HarnessMessagePrefix+fmt.Sprintf("[t+%ds] — first request of this turn.", i+1)))
		}
		lines = append(lines,
			fmt.Sprintf(`{"message":{"content":[{"text":"reply %d","type":"text"}],"role":"assistant"},"type":"assistant"}`, i+1))
		lines = append(lines,
			fmt.Sprintf(`{"completion_tokens":1,"iteration":%d,"prompt_tokens":10,"type":"request"}`, i))
	}
	lines = append(lines,
		fmt.Sprintf(`{"type":"session_end","total_iterations":%d}`, runShapeRounds))
	return writeFixture(t, lines)
}

// runShapeEvents reads the fixture back as the reader's own event type, so the
// arm below can drive readRequestRoundSeams — the function that decides whether
// every exchange becomes a turn — with BOTH predicates (the live one and the
// retired one). The parse is the reader's: same struct, same decoder.
func runShapeEvents(t *testing.T, path string) []RawEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var out []RawEvent
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("decode fixture line %q: %v", truncateRunes(line, 60), err)
		}
		out = append(out, ev)
	}
	return out
}

// TestRunSessionWithUnequalRoundsAndTurns is THE regression guard: a RUN-shaped
// log with MORE ROUNDS THAN TURNS must report the turn count, and the turn must
// SPAN the rounds — never one turn per exchange.
//
// It also drives the RETIRED predicate to prove the new one is what fixes it
// (the honest way to demonstrate a red: hand readRequestRoundSeams the old
// answer rather than keep a copy of the old rule in the tree).
func TestRunSessionWithUnequalRoundsAndTurns(t *testing.T) {
	path := writeRunShapedLog(t)

	// PREMISE 1 — the fixture really holds more rounds than turns. A fixture
	// where the two coincide proves nothing (Rule 10.4).
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got := SessionRounds(s); got < runShapeRounds {
		t.Fatalf("the fixture holds %d round(s), want >= %d — a log with one exchange cannot show a collapsed count", got, runShapeRounds)
	}

	// PREMISE 2 — THE OLD PREDICATE, DRIVEN: with the briefing classified as
	// machinery (which is what `IsTurnOpeningRow` did to it before this change)
	// no row in the log opens a turn, so the reader's request-row fallback fires
	// and every exchange becomes a turn. This is the failure the fix removes,
	// reproduced through the production function.
	oldPredicate := func(text, source string) bool {
		// The retired answer, in the one place a test may state it: a briefing
		// row is machinery, so it opened nothing. The carrier leg is kept so the
		// retired predicate is not MORE permissive than the live one elsewhere.
		if sessionevent.IsFrameworkChatter(text) {
			return false
		}
		return sessionevent.IsTurnOpeningRow(text, source)
	}
	if !readRequestRoundSeams(runShapeEvents(t, path), oldPredicate) {
		t.Fatalf("the retired predicate did NOT select the request-row fallback on this log — the fixture stopped reproducing the defect, so the arm below cannot prove the fix")
	}
	// And the live one does not: the briefing is an input, so the log has a
	// recorded seam and its exchanges are not promoted to turns.
	if readRequestRoundSeams(runShapeEvents(t, path), sessionevent.IsTurnOpeningRow) {
		t.Fatalf("the LIVE predicate selected the request-row fallback on a log that holds a briefing — the run's only complete input was not recognised, so `--turns` will print one turn per round again")
	}

	// THE COUNT — turns strictly fewer than rounds, at the real number.
	turns := SessionTurns(s)
	if turns >= SessionRounds(s) {
		t.Fatalf("turns = %d, rounds = %d — a RUN session's turn count has collapsed onto its round count (the defect: every exchange printed as a turn of its own)", turns, SessionRounds(s))
	}
	if turns != runShapeTurns {
		t.Fatalf("turns = %d, want %d (the ONE briefing the run was dispatched with; the ticks after it open nothing)", turns, runShapeTurns)
	}

	// THE SPANS — one turn covering every round, which is what the playback
	// label renders as "rounds 1–N" instead of "rounds N–N".
	spans := SessionTurnSpans(s)
	if len(spans) != runShapeTurns {
		t.Fatalf("SessionTurnSpans returned %d span(s), want %d", len(spans), runShapeTurns)
	}
	if spans[0].FirstRound != 1 || spans[0].LastRound < runShapeRounds {
		t.Fatalf("the turn spans rounds %d–%d, want 1–>=%d — a span of one round is the collapsed count wearing a label",
			spans[0].FirstRound, spans[0].LastRound, runShapeRounds)
	}

	// THE FACE — the turn says WHAT opened it, so a surface can print the kind of
	// input instead of inferring it from the prompt text.
	bounds := SessionTurnBounds(s)
	if len(bounds) != 1 {
		t.Fatalf("SessionTurnBounds = %v, want ONE boundary", bounds)
	}
	if !strings.HasPrefix(s.Iterations[bounds[0]].UserPrompt, sessionevent.HarnessBriefingOnStdinMarker) {
		t.Fatalf("turn 1's prompt is %q — it must be the briefing the run was dispatched with",
			truncateRunes(s.Iterations[bounds[0]].UserPrompt, 60))
	}
}
