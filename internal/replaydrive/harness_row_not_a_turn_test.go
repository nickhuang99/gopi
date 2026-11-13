package replaydrive

import (
	"strings"
	"testing"
)

// TestHarnessInjectionIsNotATurnTheReplayTypes pins the turn unit of a replay
// against the ONE rule every session view already uses, so a recording cannot
// report two turn counts depending on which surface reads it.
//
// THE MEASURED DEFECT (2026-10-20, session 8616b46c-db23-41cb-a657-c3a519abd998,
// 702 JSONL rows): `gopi chat --replay <id>` reported `turns typed: 201/201`
// and its status row read `turn201`, while `gopi session replay <id>` said
// `turns=15` and `gopi session log <id> --turns` said 15 — for the SAME file.
// The recording held 202 string user rows, of which 187 were `[system] …`
// harness injections (task events, `[t+…]` clock ticks). Every surface except
// the replay counts a turn by the operator-row rule
// (sessionevent.IsTurnOpeningRow → the reader's TurnBoundary); the replay
// counted every string user row, so it typed 187 lines the person never typed
// and called each one a turn.
//
// WHAT IS ASSERTED, and each arm names a different mistake:
//   - only the operator rows become turns (the count is the reader's),
//   - the injected text is NOT typed (typing it puts words on the filmed screen
//     the original screen never carried),
//   - the injected row still SEAMS an exchange (flush runs), so bounding is not
//     lost along with the turn,
//   - the response script is untouched — this is a turn-count fix and must not
//     re-partition the model answers.
func TestHarnessInjectionIsNotATurnTheReplayTypes(t *testing.T) {
	const (
		operatorAsk   = "请看一下这个任务"
		harnessNotice = "[system] An external process sent this session a message: task event: event=failed"
		clockTick     = "[system] [t+11236s] — +141s since your previous request."
	)
	p := writeJSONL(t, "session.jsonl", []string{
		userRow(operatorAsk),
		assistantRow(jsonTextBlock("first answer")),
		requestRow(),
		// A harness injection arrives mid-conversation and produces its own
		// response — it is an exchange, but nobody typed it.
		userRow(harnessNotice),
		assistantRow(jsonTextBlock("answer to the notice")),
		requestRow(),
		userRow(clockTick),
		userRow(operatorAsk + "（第二问）"),
		assistantRow(jsonTextBlock("second answer")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}

	if got, want := len(rec.Turns), 2; got != want {
		typed := make([]string, 0, len(rec.Turns))
		for _, turn := range rec.Turns {
			typed = append(typed, turn.Text)
		}
		t.Fatalf("turns = %d, want %d — the two operator rows are the turns and the two harness rows are not.\n"+
			"turns the replay would type: %q", got, want, typed)
	}
	for i, turn := range rec.Turns {
		if strings.Contains(turn.Text, "task event: event=failed") || strings.Contains(turn.Text, "since your previous request") {
			t.Fatalf("turn %d is a harness injection the replay would TYPE onto the filmed screen: %q", i, turn.Text)
		}
	}
	if got, want := rec.Turns[0].Text, operatorAsk; got != want {
		t.Fatalf("turn 0 text = %q, want the operator's own row %q", got, want)
	}

	// THE SCRIPT IS NOT RE-PARTITIONED BY THIS CHANGE: three `request` rows are
	// three responses, whatever the turns are.
	if got, want := rec.ResponseCount(), 3; got != want {
		t.Fatalf("responses = %d, want %d — dropping a harness TURN must not drop a model ANSWER", got, want)
	}
	if got, want := len(rec.Turns[1].Responses), 1; got != want {
		t.Fatalf("the operator's second turn holds %d response(s), want %d — the exchanges the harness rows bounded still belong to a turn", got, want)
	}
}

// TestHarnessOnlyRecordingTypesNoLineAndOpensNoOperatorTurn is the Rule 15 arm
// of the same rule, stated at the size the behaviour actually has.
//
// A log whose ONLY user rows are harness injections has no turn anybody typed.
// LoadRecording still returns a recording — it does NOT refuse — because the
// seeded-session shape is legitimate and pre-existing: a response with no
// operator row before it gets a turn of its own with empty text, which the
// replay types nothing for and which keeps the script addressable (see the
// `flush` closure's comment). Refusing here would break that shape.
//
// SO THE ASSERTION IS THE HONEST ONE: the recording carries NO turn whose text
// the replay would type. That is what makes "nothing was typed" an observable
// property of the parsed recording rather than a hope about the driver — and it
// is exactly the arm that failed before the fix, when the injection itself
// became a typed turn.
func TestHarnessOnlyRecordingTypesNoLineAndOpensNoOperatorTurn(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		userRow("[system] An external process sent this session a message: task event: event=failed"),
		assistantRow(jsonTextBlock("answer")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording refused the seeded-session shape: %v", err)
	}
	for i, turn := range rec.Turns {
		if strings.TrimSpace(turn.Text) != "" {
			t.Fatalf("turn %d carries text the replay would TYPE from a harness injection: %q", i, turn.Text)
		}
	}
	if got, want := rec.ResponseCount(), 1; got != want {
		t.Fatalf("responses = %d, want %d — the answer must still be addressable by the script", got, want)
	}
}
