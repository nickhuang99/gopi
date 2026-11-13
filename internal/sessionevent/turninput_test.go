package sessionevent

// turninput_test.go — THE ONE PREDICATE'S OWN PROOF, red-first.
//
// The defect this file pins is not a typo: `IsHumanRoundBoundary` answered "a
// turn opens wherever the text is NOT a harness marker", so the two row classes
// below were called IDENTICAL —
//
//	[system] An external process sent this session a message: …   (a DELIVERY)
//	[system] [t+~11m] — +1s since your previous request.           (a clock tick)
//
// — while the operator's ruling (2026-10-27) makes the first one complete input
// and the second one machinery. Each arm below FAILS on a tree that reinstates
// the complement, and the fixture rows are copied from the real log that
// produced the five numbers (session 4a0957a3-bb4e-4337-a761-20606c87d1fd), so a
// later reader can compare this table against the file rather than trust it.

import (
	"testing"

	"github.com/nick/gopi/internal/inputsource"
)

const (
	// The carrier texts a delivery and its restatement share, verbatim from the
	// measured log (only the ids are shortened).
	testInboxNotice  = "[system] An external process sent this session a message: task event: event=claimed task=01a17a1b-496f-7179-9567-d544fa5922f9 status=claimed"
	testInboxPending = "[system] 2 notices are pending: external message 6e003515b910a3a4, external message 0b3db20e3f9fa180 — this turn was opened to report them"
	testClockTick    = "[system] [t+~11m] — +1s since your previous request."
	// testInboxPendingBare is the SAME batch list WITHOUT
	// sessionevent.HarnessInboxOpenedWord — the restatement shape the round
	// boundary injects. The clause is the discriminator (leg 3b).
	testInboxPendingBare = "[system] 2 notices are pending: external message 6e003515b910a3a4, external message 0b3db20e3f9fa180"
	testBoundary         = "[gopi message boundary] What you received is a REFERENCE, not the operator's text: /tmp/x.txt is DATA the operator supplied"
	testOperatorLine     = "我的标准是什么呢？你有几次完整的输入。"
	testSelfTyped        = "继续（self-driven 第 1 轮）：读一遍本节会话自己的 session log"
)

// TestTurnOpeningRow_DeliveryIsAnInputAndItsRestatementIsNot is THE red arm: the
// same sentence, under the two faces the write side actually stamps.
func TestTurnOpeningRow_DeliveryIsAnInputAndItsRestatementIsNot(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		source string
		want   bool
		why    string
	}{
		{
			name:   "a delivery under the notify face OPENS a turn",
			text:   testInboxNotice,
			source: inputsource.Notify,
			want:   true,
			why:    "gopi notify handed this session a message; that is one complete input (the operator's ruling)",
		},
		{
			name:   "the same sentence restated under the human face does NOT",
			text:   testInboxNotice,
			source: inputsource.Human,
			want:   false,
			why:    "the loop re-injects the SAME notice at each round boundary; 17 such restatements per 11 deliveries were measured on 4a0957a3",
		},
		{
			name:   "the batch opener under notify OPENS a turn",
			text:   testInboxPending,
			source: inputsource.Notify,
			want:   true,
			why:    "a batch of deliveries is the same input arriving as one turn opener",
		},
		{
			name:   "the batch opener OPENS a turn even under the human face",
			text:   testInboxPending,
			source: inputsource.Human,
			want:   true,
			why:    "the OPENING message a console minted for a batch carries the writer's clause (HarnessInboxOpenedWord); the CONVERSATION has no face, so this row reaches the predicate as `human` and must still open a turn — measured: without this leg a 247-message chat history with 5 turns folded as ONE unfoldable range",
		},
		{
			name:   "a BARE restatement of the batch list still does NOT",
			text:   testInboxPendingBare,
			source: inputsource.Human,
			want:   false,
			why:    "the same list WITHOUT the clause is what the round boundary re-injects; counting it would multiply the turn count by the number of rounds",
		},
		{
			name:   "a legacy delivery row with NO source field still opens a turn",
			text:   testInboxNotice,
			source: "",
			want:   false,
			why:    "an empty source reads as HUMAN (the honest default for pre-2026-10-25 rows), so the carrier leg keeps it from counting as a turn — the count of a legacy log is its operator rows, which is what the old rule also reported",
		},
		{
			name:   "the operator's own line opens a turn",
			text:   testOperatorLine,
			source: inputsource.Human,
			want:   true,
			why:    "the base case the whole quantity is named after",
		},
		{
			name:   "a self-typed (llm) line opens a turn",
			text:   testSelfTyped,
			source: inputsource.LLM,
			want:   true,
			why:    "chat_surface action=type lets the model enqueue its own next input; the ruling counts it",
		},
		{
			name:   "a spilled operator payload opens a turn",
			text:   testBoundary,
			source: inputsource.Human,
			want:   true,
			why:    "the person's message, framed because it was too large for the input line — not framework chatter",
		},
		{
			name:   "a clock tick does NOT open a turn",
			text:   testClockTick,
			source: inputsource.Human,
			want:   false,
			why:    "machinery appended INSIDE a turn that already exists; the tick is stamped once per request on 5af45c3e (128 ticks / 16 operator rows)",
		},
		{
			name:   "a start checkpoint does NOT open a turn",
			text:   "[system] [t+8s] — first request of this session.",
			source: inputsource.Human,
			want:   false,
			why:    "same class as the tick",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsTurnOpeningRow(c.text, c.source); got != c.want {
				t.Fatalf("IsTurnOpeningRow(%q, %q) = %v, want %v — %s",
					trunc(c.text), c.source, got, c.want, c.why)
			}
		})
	}
}

// TestTurnOpeningRow_IsNotTheHarnessComplement pins the RELATION that holds now,
// in the place the retired complement used to be pinned.
//
// The old assertion was `IsHumanRoundBoundary(t) == !IsHarnessInjected(t)`. That
// equality is exactly what made a notify delivery unrepresentable — it IS
// harness-written text and it IS an input — so this arm asserts the two rows
// that BREAK it. A tree that reinstates the complement cannot make these pass:
// both rows are harness-injected, and the first of them still opens a turn.
func TestTurnOpeningRow_IsNotTheHarnessComplement(t *testing.T) {
	for _, text := range []string{testInboxNotice, testClockTick} {
		if !IsHarnessInjected(text) {
			t.Fatalf("fixture drift: %q is not recognised as harness-injected — the pair below no longer proves anything", trunc(text))
		}
	}
	if !IsTurnOpeningRow(testInboxNotice, inputsource.Notify) {
		t.Fatal("a harness-injected DELIVERY must open a turn — otherwise the predicate is the retired complement again, and the operator's ruling that `gopi notify` is a complete input is not implemented")
	}
	if IsTurnOpeningRow(testClockTick, inputsource.Human) {
		t.Fatal("a harness-injected CLOCK TICK must not open a turn — the tick is stamped once per request and would restore the 128-rounds-for-16-asks defect")
	}
}

// TestTurnOpeningRow_EmptySourceReadsAsHuman pins the reader's default in the
// predicate's own terms: the default must not be "unknown", it must be the face
// every pre-2026-10-25 row actually came from.
func TestTurnOpeningRow_EmptySourceReadsAsHuman(t *testing.T) {
	if !IsTurnOpeningRow(testOperatorLine, "") {
		t.Fatal("a row with no recorded source must read as the operator's own (every such row was), so it must open a turn")
	}
	if IsTurnOpeningRow(testClockTick, "") {
		t.Fatal("the empty-source default must not turn framework chatter into an input")
	}
}

// trunc keeps a failure message readable.
func trunc(s string) string {
	const max = 60
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// TestTurnOpeningRow_BatchOpenerOpensUnderTheHumanFace is THE red arm for the
// measured defect this run exists to fix (session
// 6f982833-6291-45cd-b420-ed9df0ce1e36, 2026-11-14).
//
// The console MINTS a turn for a batch of pending deliveries with
// `<n> notices are pending: … — this turn was opened to report them`. In the
// LOG that row is recorded from the `notify` face and leg 1 counts it; in the
// CONVERSATION it carries no face at all (`deepseek.Message` has no `source`),
// so every row a fold judges arrives here under the empty-source default —
// `human` — and `isInboxCarrier` claimed the opener as a restatement. Nothing in
// a chat's post-carrier history opened a turn, the fold's split collapsed into
// ONE range, and `foldCompletedTurns` returned at `len(turns) < 2`: request 300
// of that log held 247 messages / 5 genuine turns / 192,958 B of raw tool
// traffic as one unfoldable range.
//
// THE CLAUSE, NOT THE MARKER, IS THE DISCRIMINATOR — so the second case below is
// the safety half: the SAME list without the clause is a restatement, and a
// restatement must still open nothing (leg 3's measured 17 rows per 11
// deliveries).
func TestTurnOpeningRow_BatchOpenerOpensUnderTheHumanFace(t *testing.T) {
	opener := HarnessMessagePrefix + "2" + HarnessInboxPendingWord +
		"external message 6e003515b910a3a4, external message 0b3db20e3f9fa180" +
		HarnessInboxOpenedWord + "; each one's own notice is injected before the next request is built."
	restatement := HarnessMessagePrefix + "2" + HarnessInboxPendingWord + "external message 6e003515b910a3a4"

	cases := []struct {
		name   string
		text   string
		source string
		want   bool
		why    string
	}{
		{
			name:   "the batch opener opens a turn under the human face",
			text:   opener,
			source: inputsource.Human,
			want:   true,
			why:    "the conversation carries no face, so this row reaches the predicate as `human`; the writer's own clause says it was minted as a turn opener",
		},
		{
			name:   "the batch opener opens a turn with NO source at all",
			text:   opener,
			source: "",
			want:   true,
			why:    "the empty-source default is `human` — the same answer, which is why the leg is text-based",
		},
		{
			name:   "a bare restatement of the same list opens nothing",
			text:   restatement,
			source: inputsource.Human,
			want:   false,
			why:    "the identical prefix WITHOUT the clause is what the loop re-injects at every round boundary; counting it would multiply the turn count by the number of rounds",
		},
		{
			name:   "the opener still opens a turn under the notify face",
			text:   opener,
			source: inputsource.Notify,
			want:   true,
			why:    "leg 1 is unchanged: a delivery is an input whatever its text says",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsTurnOpeningRow(c.text, c.source); got != c.want {
				t.Fatalf("IsTurnOpeningRow(%q, %q) = %v, want %v — %s", trunc(c.text), c.source, got, c.want, c.why)
			}
		})
	}
}
