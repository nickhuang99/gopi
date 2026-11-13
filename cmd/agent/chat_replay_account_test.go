package main

// chat_replay_account_test.go — the completion statement must be a COMPARISON.
//
// THE MEASURED DEFECT (2026-10-27, session 80c07a7d). A replay of a recording
// holding 152 model requests and 134 recorded tool results served 93 and 82, and
// closed with `turns typed: 11/11` and nothing else: every number on the line was
// true, and the line as a whole claimed a completion that had not happened. The
// tests below pin the two halves of the repair — the SUMMARY must name both sides
// and the gap, and the DRIVER must stop reading a quiet counter as a finished
// turn.

import (
	"errors"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/replaydrive"
)

// measuredRecording is the shape of the real session this epic is about: 152
// scripted responses over 11 operator turns, 134 recorded tool results. It is
// spelled as numbers rather than loaded from the operator's profile so the test
// reads the same on any machine.
func measuredRecording() (requests, results int) { return 152, 134 }

// TestChatReplaySummaryNeverClaimsCompletionWhenShort is THE regression pin: a
// run that served less than the recording holds must say so, with both sides of
// every count and the per-turn gaps named.
func TestChatReplaySummaryNeverClaimsCompletionWhenShort(t *testing.T) {
	recorded, recordedResults := measuredRecording()
	a := chatReplayAccounting{
		RecordedRequests: recorded,
		ServedRequests:   93,
		RecordedResults:  recordedResults,
		ServedResults:    82,
		Turns:            11,
		Typed:            11,
		Gaps: []chatReplayTurnGap{
			{Turn: 7, Recorded: 38, Consumed: 4},
			{Turn: 11, Recorded: 10, Consumed: 6},
		},
	}
	if !a.Short() {
		t.Fatalf("an accounting of 93/152 and 82/134 did not report itself short — Short() is the predicate the summary and the exit status read")
	}
	summary := strings.Join(chatReplaySummaryLines(a), "")

	// (a) BOTH SIDES OF EVERY COUNT, on one line a reader can compare.
	for _, want := range []string{"93 of 152", "82 of 134", "turns typed: 11/11"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("the summary does not carry %q — a served count without the recorded total is exactly the claim this test exists to forbid:\n%s", want, summary)
		}
	}
	// (b) THE GAP IS NAMED, not merely implied. 59 requests and 52 results are
	// the numbers pre-fix run never printed.
	for _, want := range []string{"SHORTFALL", "59 of 152", "52 of 134"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("the summary does not name the gap (%q):\n%s", want, summary)
		}
	}
	// (c) IT MUST NOT CLAIM COMPLETION. This is the sentence that was absent and
	// the reason the old line was a false completion claim.
	if strings.Contains(summary, "COMPLETE") {
		t.Fatalf("a short run printed the COMPLETE claim:\n%s", summary)
	}
	// (d) THE TURNS THAT LOST THE SCRIPT ARE NAMED, with their own counts.
	for _, want := range []string{"turn 7 of 11: consumed 4 of 38", "turn 11 of 11: consumed 6 of 10"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("the summary does not name the turn gap (%q):\n%s", want, summary)
		}
	}
}

// TestChatReplaySummaryClaimsCompleteOnlyWhenItIs is the other arm: the word
// COMPLETE must exist, and be reachable ONLY when both sides agree. A summary
// that never says "complete" is as useless as one that always does.
func TestChatReplaySummaryClaimsCompleteOnlyWhenItIs(t *testing.T) {
	recorded, recordedResults := measuredRecording()
	full := chatReplayAccounting{
		RecordedRequests: recorded, ServedRequests: recorded,
		RecordedResults: recordedResults, ServedResults: recordedResults,
		Turns: 11, Typed: 11,
	}
	if full.Short() {
		t.Fatal("a run that served the whole recording reported itself short")
	}
	summary := strings.Join(chatReplaySummaryLines(full), "")
	if !strings.Contains(summary, "COMPLETE") || strings.Contains(summary, "SHORTFALL") {
		t.Fatalf("a complete run did not say so cleanly:\n%s", summary)
	}
	// ONE MISSING RESPONSE is enough to withdraw the claim: the boundary is exact,
	// not a tolerance.
	full.ServedRequests = recorded - 1
	full.ServedResults = recordedResults - 1
	short := strings.Join(chatReplaySummaryLines(full), "")
	if strings.Contains(short, "COMPLETE") || !strings.Contains(short, "SHORTFALL") {
		t.Fatalf("a run one response short still claimed completion:\n%s", short)
	}
	if !strings.Contains(short, "1 of 152") || !strings.Contains(short, "1 of 134") {
		t.Fatalf("the one-response gap is not named:\n%s", short)
	}
}

// TestChatReplaySummaryNamesADriverAbort pins the third input to the claim: a
// driver that stopped for its own reason (a refused key, an unrepresentable
// recording) must appear in the summary, because otherwise the missing responses
// read as the surface's fault when they are the driver's.
func TestChatReplaySummaryNamesADriverAbort(t *testing.T) {
	a := chatReplayAccounting{
		RecordedRequests: 10, ServedRequests: 10,
		RecordedResults: 4, ServedResults: 4,
		Turns: 2, Typed: 1,
		Aborted: errors.New("this run's editor has no key injector"),
	}
	if !a.Short() {
		t.Fatal("an aborted driver with matching counts reported itself complete")
	}
	summary := strings.Join(chatReplaySummaryLines(a), "")
	if !strings.Contains(summary, "SHORTFALL") || !strings.Contains(summary, "no key injector") {
		t.Fatalf("the abort is not named in the summary:\n%s", summary)
	}
}

// TestCloseTurnRecordsTheGapEvenWhenTheCounterLookedStable is the DRIVER half:
// the quiescence arm of waitTurn reports "the surface stopped asking", and the
// pre-fix run read that as a turn that had gone fine. It is the shortfall.
func TestCloseTurnRecordsTheGapEvenWhenTheCounterLookedStable(t *testing.T) {
	r := &chatReplay{turns: 11}
	// The measured shape of turn 7: 38 recorded responses, 4 consumed, the
	// counter quiet (NOT a timeout — `timedOut` false is the quiescence arm).
	r.closeTurn(7, 38, 4, false)
	if len(r.gaps) != 1 || r.gaps[0].Turn != 7 || r.gaps[0].Consumed != 4 || r.gaps[0].Recorded != 38 {
		t.Fatalf("closeTurn did not record the turn's gap: %+v", r.gaps)
	}
	if r.shortfall != 1 {
		t.Fatalf("shortfall = %d, want 1: a turn that ended 34 responses short was counted as fine", r.shortfall)
	}
	// A turn that consumed its own responses adds nothing.
	r.closeTurn(8, 5, 5, false)
	if len(r.gaps) != 1 || r.shortfall != 1 {
		t.Fatalf("a complete turn was counted as a gap: gaps=%+v shortfall=%d", r.gaps, r.shortfall)
	}
	// Over-consumption is not a gap either (the response belongs to a LATER turn;
	// the global-order drift is reported by the totals, not by this row).
	r.closeTurn(9, 2, 3, false)
	if len(r.gaps) != 1 {
		t.Fatalf("a turn that consumed more than its own share was recorded as a gap: %+v", r.gaps)
	}
}

// TestWaitTurnSettledNeverMistakesAShortSilenceForAFinishedTurn pins the second
// mechanism: advancing into a turn that is still working. A counter that keeps
// moving must NOT settle; a counter that holds still for longer than any recorded
// inter-request pause must.
func TestWaitTurnSettledNeverMistakesAShortSilenceForAFinishedTurn(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	if chatReplaySettleQuiescence <= chatReplayQuiescence {
		t.Fatalf("the settle window (%s) is not longer than the turn-internal quiescence (%s) — a quiet counter between two rounds of a running turn would be read as a finished turn, which is the premature advance this constant exists to prevent",
			chatReplaySettleQuiescence, chatReplayQuiescence)
	}
	// A counter that keeps ticking: the wait must refuse to settle.
	var n int
	busy := func() int { n++; return n }
	if _, settled := waitTurnSettled(1, 99, busy); settled {
		t.Fatal("waitTurnSettled settled while the counter was still moving — the next prompt would be typed into a turn that has not finished")
	}
	// A counter that is already still: the wait settles, and says so.
	still := func() int { return 7 }
	if seen, settled := waitTurnSettled(1, 99, still); !settled || seen != 7 {
		t.Fatalf("waitTurnSettled(%v) = seen %d settled %v on an idle counter, want seen 7 settled true", chatReplaySettleQuiescence, seen, settled)
	}
}

// TestAccountingReadsTheStubAndTheRecordingTogether keeps the ONE place the two
// sides are read (Rule 2) honest: the recording's totals come from the recording,
// the run's from the stub, and an unrecorded result is counted as a gap even when
// the totals happen to agree.
func TestAccountingReadsTheStubAndTheRecordingTogether(t *testing.T) {
	// ONE RESPONSE PER REQUEST: the script is `Responses`, indexed by request
	// order, so the recording's request total IS its length (ResponseCount).
	toolResponse := []replaydrive.Step{{Kind: "tool_use", Tool: &replaydrive.ToolCall{ID: "call_1", Name: "bash", Input: []byte(`{}`)}}}
	textResponse := []replaydrive.Step{{Kind: "text", Text: "x"}}
	rec := &replaydrive.Recording{
		Turns:     []replaydrive.Turn{{Text: "a", Responses: [][]replaydrive.Step{toolResponse, textResponse}}},
		Responses: [][]replaydrive.Step{toolResponse, textResponse},
		Results:   map[string]string{"call_1": "recorded"},
	}
	stub := replaydrive.NewStub(rec)
	defer stub.Close()
	// A call the recording has no result for: the seam answers with a refusal
	// sentence and the accounting must count that as a gap even though one
	// result was served.
	stub.Executor()(deepseekToolCall("call_1", "bash"))
	stub.Executor()(deepseekToolCall("call_missing", "bash"))

	r := &chatReplay{rec: rec, stub: stub, turns: 1, typed: 1}
	a := r.accounting()
	if a.RecordedRequests != 2 || a.RecordedResults != 1 || a.ServedResults != 2 || a.UnrecordedResults != 1 {
		t.Fatalf("accounting read the wrong sides: %+v", a)
	}
	if !a.Short() {
		t.Fatal("an unrecorded tool result did not make the run short")
	}
}

// deepseekToolCall builds the seam's argument without dragging the wire type
// through every assertion above.
func deepseekToolCall(id, name string) deepseek.ToolCall {
	return deepseek.ToolCall{ID: id, Name: name}
}
