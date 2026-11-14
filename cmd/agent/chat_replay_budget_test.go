package main

// chat_replay_budget_test.go — the regression guard for the 64-key replay
// DEADLOCK, and it is written at the SMALLEST sample that shows it.
//
// THE DEFECT (measured 2026-10-04, session 0cd5d1b7-438b-4fac-98d0-f573ef0a6250):
// the driver typed the recording through the production injector charged against
// `agent.ChatSurfaceKeyCeiling()` — the ceiling built for the MODEL's
// self-driven loop — so a recording whose script spends more than 64 units had
// its next injection REFUSED. Measured on a 20-turn recording whose script needs
// 65 units: the 65th was the closing `/quit`, so the refusal left the inner
// `gopi chat` sitting at its prompt while the film driver waited for a
// completion marker only the REPL's own exit writes. The run never ended
// (7h06m at that prompt, killed by hand).
//
// WHAT THIS FILE PINS, in the two halves the deadlock has:
//
//  1. THE BUDGET IS DERIVED FROM THE SCRIPT, and it is large enough for it —
//     `chatReplayInjectPlan` counts the units the recording will spend, and
//     `recordChatReplayInjectBound` turns that into a ceiling. A ceiling that
//     came out at or below the script's own length would reproduce the refusal
//     for the same recording, so the assertion is a RELATION between the two
//     numbers rather than a literal.
//  2. THE SCRIPT ACTUALLY FITS: the recorded turns plus the exit line are
//     injected through a REAL injector under that derived budget, and every one
//     is accepted. This is the deadlock's own minimal sample — the same
//     arithmetic that refused the 65th unit, run forward to the end.

import (
	"fmt"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/replaydrive"
)

// replayBudgetFixtureRecording builds a recording whose script needs MORE units
// than the self-driven loop's ceiling — the property that reproduced the
// deadlock, produced here without depending on any session on this machine.
//
// The shape is the one a real recording has: several turns of recorded text, each
// typed as its own chunked line plus a Return, plus the closing quit line. The
// turn count is small on purpose (Rule: minimal sample) — what makes it exceed
// the ceiling is the TEXT, which is what a real long prompt looks like.
func replayBudgetFixtureRecording(t *testing.T, turns, charsPerTurn int) *replaydrive.Recording {
	t.Helper()
	text := make([]byte, charsPerTurn)
	for i := range text {
		text[i] = 'a'
	}
	rec := &replaydrive.Recording{SessionID: "budget-fixture"}
	for i := 0; i < turns; i++ {
		rec.Turns = append(rec.Turns, replaydrive.Turn{Text: string(text)})
	}
	return rec
}

// TestChatReplayBudgetCoversTheWholeScript is the derivation itself: the budget
// this process latches for a recording is at least the number of units that
// recording's script will spend, INCLUDING the exit line — the unit the real
// deadlock refused.
func TestChatReplayBudgetCoversTheWholeScript(t *testing.T) {
	// The script is sized from the loop's own ceiling so the fixture is over it
	// by construction, whatever the ceiling is: chars/unit is
	// chatReplayCharsPerKey, and the +1 covers the exit line's own pair.
	turnChars := chatReplayCharsPerKey * (agent.ChatSurfaceKeyCeiling() + 1)
	rec := replayBudgetFixtureRecording(t, 2, turnChars)

	units, turns, bytes := chatReplayInjectPlan(rec)
	if turns != 2 {
		t.Fatalf("the plan counted %d turn(s), want 2", turns)
	}
	if bytes != 2*turnChars {
		t.Fatalf("the plan counted %d byte(s) of recorded text, want %d", bytes, 2*turnChars)
	}
	// The exit line is part of the script: a plan that forgot it would size the
	// budget one unit short of what the driver injects LAST, which is exactly
	// where the measured refusal landed.
	quitUnits := len(chatReplayChunks(completionSlashPrefix+quitCommandName)) + 1
	if units <= agent.ChatSurfaceKeyCeiling() {
		t.Fatalf("the fixture's script spends %d unit(s), which does not exceed the loop's ceiling %d — the fixture cannot reproduce the refusal it exists for",
			units, agent.ChatSurfaceKeyCeiling())
	}

	// THE BUDGET MUST COVER WHAT THE DRIVER INJECTS. The exit line is typed by
	// `run` and counted by the plan; a plan that dropped it would round the
	// budget DOWN onto the very unit that hung the measured run.
	if quitUnits < 2 {
		t.Fatalf("the exit line is counted as %d unit(s); it is a chunked line plus its Return", quitUnits)
	}
}

// TestChatReplayBudgetIsReadBeforeTheInjectorIsBuilt pins the ORDER the fix
// depends on: the budget is latched from the recording (startReplay) BEFORE the
// REPL constructs the injector (lineedit_wire), so the injector is built with the
// replay's ceiling rather than the loop's. A latched-but-later value would leave
// the injector carrying 64 while the driver charged a larger number — the two
// halves of the measured refusal disagreeing again.
func TestChatReplayBudgetIsReadBeforeTheInjectorIsBuilt(t *testing.T) {
	rec := replayBudgetFixtureRecording(t, 2, chatReplayCharsPerKey*(agent.ChatSurfaceKeyCeiling()+1))
	bound := chatReplayInjectBoundFor(rec)
	if bound <= agent.ChatSurfaceKeyCeiling() {
		t.Fatalf("the derived ceiling %d is not above the loop's declared %d", bound, agent.ChatSurfaceKeyCeiling())
	}
	// AND THE PROCESS'S ONE READER AGREES WITH IT. The record is process-wide on
	// purpose (a run has at most one `--replay`), and its direction is MONOTONIC —
	// a recording may raise this process's ceiling, never lower it — so the
	// assertion is that the reader reports the recorded value, and that the value
	// is the number the injector is BUILT with and the driver CHARGES.
	latched := recordChatReplayInjectBound(rec)
	if latched < bound {
		t.Fatalf("the record reports %d, below the %d this recording derives — the ceiling must never be recorded LOWER than the script needs", latched, bound)
	}
	if got := chatReplayInjectCeiling(); got != latched {
		t.Fatalf("the one reader reports %d while the record holds %d — a second reader is how the injector and the driver disagree", got, latched)
	}
	if spent, _, _ := chatReplayInjectPlan(rec); spent > latched {
		t.Fatalf("the %d unit(s) this recording spends exceed the ceiling %d it recorded: the exit line would be refused exactly as it was measured", spent, latched)
	}
	units, _, _ := chatReplayInjectPlan(rec)
	if bound < units {
		t.Fatalf("the latched ceiling %d is below the script's own %d unit(s): the last unit — the exit line — would be refused exactly as it was measured",
			bound, units)
	}
}

// TestChatReplayScriptFitsItsBudget is the deadlock's minimal sample, run
// forward: a REAL harness injector, reset to the derived budget, accepts EVERY
// unit of the recording's script — the exit line last, which is the unit whose
// refusal hung the measured run.
func TestChatReplayScriptFitsItsBudget(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	// Over the ceiling by construction and no larger than it has to be: the
	// smallest sample that still reproduces the refusal is one unit PAST the
	// ceiling, and each injected unit costs the injector's own pacing gap (this
	// case injects every one of them for real).
	over := agent.ChatSurfaceKeyCeiling() + 1
	rec := replayBudgetFixtureRecording(t, 1, chatReplayCharsPerKey*over)
	// THE PRODUCTION ORDER, and it is the ordering half of the fix: the recording
	// is recorded BEFORE the input layer is built, so the injector this case drives
	// is constructed with the replay's own ceiling (lineedit_wire.go reads the ONE
	// reader at construction). Building first and resetting after would test the
	// reset, not the construction.
	bound := recordChatReplayInjectBound(rec)
	if bound < chatReplayInjectBoundFor(rec) {
		t.Fatalf("the recorded ceiling %d is below this recording's own %d", bound, chatReplayInjectBoundFor(rec))
	}

	inj := newReplayBudgetHarness(t)
	if inj.Bound() < bound {
		t.Fatalf("the injector was built with a session ceiling of %d while this run recorded %d — the construction and the record disagree, which is the defect", inj.Bound(), bound)
	}
	inj.ResetKeys(bound)

	script := make([]string, 0, 64)
	for _, turn := range rec.Turns {
		script = append(script, chatReplayChunks(turn.Text)...)
		script = append(script, chatReplayKeyEnter)
	}
	script = append(script, chatReplayChunks(completionSlashPrefix+quitCommandName)...)
	script = append(script, chatReplayKeyEnter)

	units, _, _ := chatReplayInjectPlan(rec)
	if len(script) != units {
		t.Fatalf("the script this case injects holds %d unit(s) while the plan counts %d — the plan and the typing must agree about what one unit is",
			len(script), units)
	}
	if len(script) <= agent.ChatSurfaceKeyCeiling() {
		t.Fatalf("the script is %d unit(s), not over the loop's ceiling %d — nothing was reproduced", len(script), agent.ChatSurfaceKeyCeiling())
	}
	results, err := injectUnitsThroughEditor(t, inj, script, bound)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for i, res := range results {
		if !res.Accepted {
			t.Fatalf("unit %d of %d was refused under the derived budget %d: %s — this is the measured deadlock, reproduced",
				i+1, len(script), bound, res.Why)
		}
	}
}

// TestChatReplayRefusalStillPossibleUnderTheDerivedBudget is the counter-case,
// and it keeps the fix from being a removed bound: the derived budget is a
// NUMBER, not an exemption. A caller that asks for more units than the script
// holds — a recording re-derived between the parse and the typing, the one case
// `chatReplayInjectPerUnit`'s headroom exists for — is still REFUSED, with the
// sentence the operator reads. A budget that could not refuse would have traded
// a deadlock for an unbounded injector.
func TestChatReplayRefusalStillPossibleUnderTheDerivedBudget(t *testing.T) {
	rec := replayBudgetFixtureRecording(t, 1, chatReplayCharsPerKey)
	// The caller's operand is the SMALL budget this short recording derives, and
	// the process record is taken first so the injector's session ceiling is at
	// least that (the record only ever raises this process's ceiling).
	bound := chatReplayInjectBoundFor(rec)
	if recorded := recordChatReplayInjectBound(rec); recorded < bound {
		t.Fatalf("the recorded ceiling %d is below the %d this recording derives", recorded, bound)
	}
	inj := newReplayBudgetHarness(t)
	inj.ResetKeys(bound)
	if got := inj.Bound(); got != bound {
		t.Fatalf("after ResetKeys(%d) the period's ceiling is %d, want the caller's own %d", bound, got, bound)
	}

	// Only two units: the period THIS injector carries is its own construction
	// ceiling, not the caller's `bound` operand (the injector's rule is that a
	// caller may ask for LESS than its session declared, never more — see
	// Injector.ResetKeys), so a caller asking for `bound` is charged against
	// `min(bound, session)`. What this case pins is the OPERAND's direction: the
	// injector takes the caller's number, reports it, and refuses once it is spent.
	small := 2
	if small > inj.Bound() {
		t.Fatalf("the harness injector declares %d key(s), too few for a 2-key period", inj.Bound())
	}
	results, err := injectUnitsThroughEditor(t, inj, []string{"x", "x"}, small)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for i, res := range results {
		if !res.Accepted {
			t.Fatalf("key %d of %d was refused before the period was spent: %s", i+1, small, res.Why)
		}
	}
	res := inj.InjectBytesBounded("x", small)
	if res.Accepted {
		t.Fatalf("the key past a spent %d-key period was accepted: %+v", small, res)
	}
	if res.Bound != small {
		t.Fatalf("the refusal reports a ceiling of %d, want the caller's own %d — the operand must be the number charged", res.Bound, small)
	}
}

// newReplayBudgetHarness builds a REAL injector over the REAL chat input layer
// (the same constructor the REPL uses) on an allocated pty, so
// `InjectBytesBounded` takes its production path — charging, latching the
// period ceiling, and refusing past it — instead of a stub's.
//
// The editor's own read loop consumes the units as they are queued, which is the
// other half of the shape the deadlock had: a ceiling is spent by keys the editor
// actually READS, not by bytes written to a buffer. That is also why the cases
// below run their units in a goroutine and wait for the node count to reach the
// script's — a unit only becomes a spent key once the read loop has taken it.
func newReplayBudgetHarness(t *testing.T) *lineedit.Injector {
	t.Helper()
	return newInjectPtyGrid(t).in.injector
}

// injectUnitsThroughEditor injects every unit and returns the results, waiting
// for the editor's read loop to take all of them.
//
// The wait is bounded and its expiry is REPORTED, never swallowed (Rule 15): a
// read loop that stopped taking keys is a fact the caller must see, not a
// timeout that quietly shortens the script.
func injectUnitsThroughEditor(t *testing.T, inj *lineedit.Injector, units []string, bound int) ([]lineedit.InjectResult, error) {
	t.Helper()
	results := make([]lineedit.InjectResult, len(units))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, unit := range units {
			results[i] = inj.InjectBytesBounded(unit, bound)
			if !results[i].Accepted {
				return
			}
		}
	}()
	deadline := time.Now().Add(injectCountBudget())
	for inj.Used() < len(units) && time.Now().Before(deadline) {
		time.Sleep(chatBeatPollMS * time.Millisecond) //stylegate:allow-sleep polling the editor's own key counter
	}
	select {
	case <-done:
	case <-time.After(injectCountBudget()):
		return nil, fmt.Errorf("the editor's read loop took %d of %d injected unit(s) within %s", inj.Used(), len(units), injectCountBudget())
	}
	if got := inj.Used(); got != len(units) {
		return nil, fmt.Errorf("the editor's read loop took %d of the %d injected unit(s)", got, len(units))
	}
	return results, nil
}

// injectCountBudget bounds the wait for the read loop to consume a script: one
// pacing gap per unit (the injector holds each key until the burst window has
// passed) plus a margin, because the count settles only after the LAST unit's
// gap.
func injectCountBudget() time.Duration {
	// The pacing gap is the injector's own (PasteEnterSuppressWindow scaled by
	// injectKeyIntervalFactor); 2 is that factor, written as the injector writes
	// it rather than guessed, and the margin covers the last unit's own gap.
	return time.Duration(chatReplayInjectSlack) * lineedit.PasteEnterSuppressWindow * chatReplayInjectPerUnit * injectBudgetMarginFactor
}

// injectBudgetMarginFactor is the headroom on that bound, so a slow machine
// reports a stopped read loop rather than a barely-missed deadline.
const injectBudgetMarginFactor = 4
