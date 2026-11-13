// turnfold_silent_exit_test.go — the regression for the TWO SILENT EARLY EXITS.
//
// foldCompletedTurns returns without a word on two paths, and from the console
// they are indistinguishable from a broken fold: the log carries the 📦 success
// line and NOTHING else, so an operator reading a session that folded nothing
// sees the same quiet as a session that folded nothing BECAUSE it had nothing
// to fold. Measured 2026-11-14 (session 6f982833): 443 requests, 7 turn-fold
// lines, `grep -c refused` = 0 — the whole session cut into ONE turn range and
// never folded once, silently.
//
//	(A) len(splitTurnRanges(msgs)) < 2   — the conversation is a single range
//	(B) len(complete) <= raw window      — every complete turn is inside the window
//
// Each must name its own criterion, once per session (the boundary these exits
// are reached from runs once per ROUND, so an unlatched line would repeat every
// round and become noise the operator learns to skip).
package agent

import (
	"log"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/toolnames"
)

// turnFoldNothingToFoldToken (the grep token both early exits must carry) is
// declared in production beside turnFoldNoteHead — the test asserts on the
// shipped spelling, never on a copy.

// singleTurnHistory is exit (A)'s shape: ONE turn range in the whole
// conversation. The premise carries the briefing marker (a turn opening), no
// later row opens a turn, so splitTurnRanges returns a single range — and the
// last (only) turn is the in-flight one, which is exactly why the fold cannot
// proceed.
func singleTurnHistory() []deepseek.Message {
	return []deepseek.Message{
		{Role: roleUserMessage, Content: sessionevent.HarnessBriefingOnStdinMarker + "the run's premise"},
		{Role: roleAssistantMessage, Content: "running it", ToolCalls: []deepseek.ToolCall{{
			ID:        "call_single_turn_bash",
			Name:      toolnames.ToolBash,
			Arguments: map[string]interface{}{"command": "go build ./..."},
		}}},
		{Role: roleToolMessage, ToolCallID: "call_single_turn_bash", Content: "exit=0\n"},
		{Role: roleAssistantMessage, Content: "it builds"},
	}
}

// TestTurnFoldSaysWhyItDidNothing pins BOTH early exits: each one logs a line
// carrying the grep token and its own criterion, and a SECOND call at the same
// boundary logs nothing more (once per session, not once per round).
func TestTurnFoldSaysWhyItDidNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		// build returns the agent under test and any text its reason must name.
		build func(logs *strings.Builder) (*Agent, []string)
	}{
		{
			// (A) the whole conversation is ONE range: no completed turn exists.
			name: "single range",
			build: func(logs *strings.Builder) (*Agent, []string) {
				a := turnFoldAgent(singleTurnHistory())
				return a, []string{"range"}
			},
		},
		{
			// (B) two complete turns, and the operator's raw window protects BOTH:
			// nothing is older than the window, so nothing folds.
			name: "all complete turns inside the raw window",
			build: func(logs *strings.Builder) (*Agent, []string) {
				a := turnFoldAgentWindow(turnFoldFixture(), turnFoldRawWindowProtectsAll)
				return a, []string{turnFoldRecentTurnsEnv}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs strings.Builder
			a, mustName := tc.build(&logs)
			a.Logger = log.New(&logs, "", 0)

			stats := a.foldCompletedTurns()
			if stats.Folded != 0 {
				t.Fatalf("the fixture folded %d turn(s) — this arm must reach the early exit, not the success path", stats.Folded)
			}
			first := logs.String()
			if !strings.Contains(first, turnFoldNothingToFoldToken) {
				t.Fatalf("the early exit said nothing greppable — want %q in the log, got:\n%q", turnFoldNothingToFoldToken, first)
			}
			for _, want := range mustName {
				if !strings.Contains(first, want) {
					t.Errorf("the line must name its criterion (missing %q):\n%s", want, first)
				}
			}

			// ONCE PER SESSION: the same boundary is reached again on the next
			// round, and a repeated line is noise an operator learns to skip.
			a.foldCompletedTurns()
			if got := logs.String(); got != first {
				t.Errorf("the early exit logged a SECOND time (once per session is the contract):\n%s", got[len(first):])
			}
		})
	}
}

// turnFoldRawWindowProtectsAll is a raw window wide enough to hold every
// complete turn the fixture has (3), so exit (B) is reached. It is a FIXTURE
// width, not a shipped default: it exists to drive the arm, never as policy.
const turnFoldRawWindowProtectsAll = 3
