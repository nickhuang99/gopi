package gopilog

// humanround_test.go — THE HUMAN ROUND, pinned on the smallest possible fixture.
//
// THE DEFECT THIS GUARDS (operator's report, 2026-10-11; task 01a12b40): the
// playback numbered rounds by HTTP EXCHANGE (the JSONL `request` row), so a
// person's ONE question that the agent answered with several tool calls showed
// as several "rounds" — "你看这个根本不是 iteration/round，依然是 event/toolcall".
//
// The smallest sample that can tell the two apart is ONE operator row and TWO
// tool calls, each with its own `request` row: under the human-round reading
// that is ONE round (both calls answer the same ask), under the exchange
// reading it is two. The fixture writes the rows in the WRITER's own
// order (content first, the `request` row that CLOSES the exchange last) — the
// historical trap this package has already paid for once: a fixture that puts
// the request row first makes the reader attribute content to the NEXT exchange.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// writeHumanRoundFixture writes a log of one human round carrying `calls` tool
// calls, each exchange closed by its own `request` row, and returns the path.
func writeHumanRoundFixture(t *testing.T, calls int) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":"do the thing"}}`+"\n")
	fmt.Fprintf(&b, `{"type":"user","message":{"role":"user","content":"[system] [t+5s] — first request of this session."}}`+"\n")
	// The harness's own tick follows the operator's row — the order the writer
	// produces (measured on 5af45c3e: HUMAN, tick, assistant, request). The tick
	// is a row of the round that is starting; it opens nothing.
	for i := 0; i < calls; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t%d","name":"read","input":{"path":"/tmp/x"}}]}}`+"\n", i+1)
		fmt.Fprintf(&b, `{"type":"request","iteration":%d}`+"\n", i)
		fmt.Fprintf(&b, `{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t%d","content":"ok"}]}}`+"\n", i+1)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}
	return path
}

// TestHumanRoundIsOneAskNotOneExchange is THE regression guard: one operator
// row + N tool calls = ONE human round, while the exchange count still reports N.
func TestHumanRoundIsOneAskNotOneExchange(t *testing.T) {
	for _, calls := range []int{1, 2, 5} {
		s, err := ParseFile(writeHumanRoundFixture(t, calls))
		if err != nil {
			t.Fatalf("ParseFile: %v", err)
		}
		if got := SessionTurns(s); got != 1 {
			t.Fatalf("%d tool calls answering ONE operator row gave %d human rounds, want 1: a round is what the person asked for, not the exchange", calls, got)
		}
		if got := SessionRounds(s); got != calls {
			t.Fatalf("%d tool calls reported %d exchanges, want %d: the two counts are DIFFERENT quantities and neither may absorb the other", calls, got, calls)
		}
		// The ordinal is stable across a round's whole span: every exchange in it
		// belongs to round 1 (this is what makes the label's N agree with the
		// playback's k).
		for i := range s.Iterations {
			if got := TurnOrdinalIn(s, i); got != 1 {
				t.Fatalf("content at exchange %d is in human round %d, want 1 (every exchange of the span shares the round)", i, got)
			}
		}
	}
}

// TestHumanRoundBoundaryIsTheOperatorRowNotAHarnessInjection pins the mark's
// SHAPE: the operator's own row opens a round, and NO harness row does — not a
// clock tick, not an external-process notice, not a checkpoint.
//
// The tick is the case that matters, and it is MEASURED (2026-10-11): session
// 5af45c3e carries 128 `[system] [t+…] … since your previous request.` rows
// against 128 `request` rows — the tick is stamped once per REQUEST — so a
// tick-drawn seam reports exactly the 128 the operator's own 16 rows say it
// must not. Session capture-* settles it: 36 request rows, 18 operator rows,
// ZERO ticks.
func TestHumanRoundBoundaryIsTheOperatorRowNotAHarnessInjection(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"the gap stamp", "[system] [t+222s] — +1s since your previous request.", false},
		{"the first stamp", "[system] [t+638s] — first request of this session.", false},
		{"leading whitespace on a stamp", "\n\n[system] [t+9s] — +3s since your previous request.", false},
		{"an external process", "[system] An external process sent this session a message:\nhi", false},
		{"notices pending", "[system] 3 notices are pending: a, b, c", false},
		{"a checkpoint", sessionevent.HarnessMessagePrefix + "checkpoint: turn 3", false},
		{"plain operator text", "why is this round split?", true},
		{"an operator quoting the stamp's shape", "look at this: [system] [t+222s] — +1s since your previous request.", true},
		{"operator text with leading whitespace", "\n\n先2 再 1:", true},
	}
	for _, c := range cases {
		// The rows in this table carry no `source` field, so the predicate's
		// empty-source default (human) applies — which is what the measured
		// sessions themselves look like.
		if got := sessionevent.IsTurnOpeningRow(c.text, ""); got != c.want {
			t.Errorf("%s: IsTurnOpeningRow = %v, want %v (%q)", c.name, got, c.want, c.text)
		}
	}
}

// TestHumanRoundIsStillCountedWithoutAnyOperatorRow is the FALLBACK's guard: a
// log that recorded NO operator row at all (a resumed file, a worker run nobody
// typed into) still has seams — its `request` rows are the only ones it has, so
// each exchange is one round there (never 0, which would claim the log holds no
// rounds at all).
func TestHumanRoundIsStillCountedWithoutAnyOperatorRow(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&b, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"a%d"}]}}`+"\n", i)
		fmt.Fprintf(&b, `{"type":"request","iteration":%d}`+"\n", i)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got := SessionTurns(s); got != 3 {
		t.Fatalf("a log with no operator row and 3 exchanges reports %d human rounds, want 3 (the request row is the seam when nothing else was recorded)", got)
	}
}

// TestHumanRoundOrdinalRefusesAnIndexItDoesNotHave is the Rule-15 case: an index
// that names no exchange reports 0 (sessionevent.RoundIndexUnknown), which is a
// statement of absence rather than a fabricated round 1.
func TestHumanRoundOrdinalRefusesAnIndexItDoesNotHave(t *testing.T) {
	s, err := ParseFile(writeHumanRoundFixture(t, 2))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, idx := range []int{-1, len(s.Iterations), len(s.Iterations) + 10} {
		if got := TurnOrdinalIn(s, idx); got != sessionevent.RoundIndexUnknown {
			t.Errorf("TurnOrdinalIn(%d) = %d, want %d (unknown)", idx, got, sessionevent.RoundIndexUnknown)
		}
	}
	if got := TurnOrdinalIn(nil, 0); got != sessionevent.RoundIndexUnknown {
		t.Errorf("TurnOrdinalIn(nil, 0) = %d, want %d", got, sessionevent.RoundIndexUnknown)
	}
}

// TestADeliveryOpensATurnAndItsRestatementDoesNot is THE regression guard for the
// 2026-10-27 epic, on the smallest fixture that can see it: ONE notify face row
// (a delivery) and TWO human face rows carrying the SAME carrier sentence (the
// restatements the loop injects at each round boundary). The turn count must be
// 1 + (the operator rows below) — never 1 per carrier row.
//
// THE MEASURED NUMBERS THIS PINS (session 4a0957a3-bb4e-4337-a761-20606c87d1fd,
// 2026-10-27): 11 rows under the `notify` face against 17 restatements under the
// `human` face. Before the fix the predicate was a TEXT test, so it called all 28
// "harness" and the count was the session's own operator lines; a fix that
// counted every carrier row reported 42. The right number is the one both faces
// agree on when the FACE — not the text — decides: 11 deliveries + the operator's
// own lines.
func TestADeliveryOpensATurnAndItsRestatementDoesNot(t *testing.T) {
	const carrier = "[system] An external process sent this session a message: task event: event=claimed"

	// The row shapes are the ones the WRITE side produces (session_start, then
	// user/request pairs); writeFixture is this package's ONE fixture writer.
	lines := []string{
		`{"model":"deepseek-v4-flash","sessionId":"delivery-fixture","timestamp":"2026-10-27T05:00:00.0+08:00","type":"session_start","workdir":"/tmp/w"}`,
		// The delivery: the notify face.
		`{"message":{"content":"` + carrier + `","role":"user"},"source":"notify","timestamp":"2026-10-27T05:00:01.0+08:00","type":"user"}`,
		`{"message":{"content":[{"text":"ack one","type":"text"}],"role":"assistant"},"timestamp":"2026-10-27T05:00:01.5+08:00","type":"assistant"}`,
		`{"iteration":0,"timestamp":"2026-10-27T05:00:02.0+08:00","type":"request"}`,
		// Two restatements of the SAME input, written from the human face — what
		// the loop injects at the next two round boundaries.
		`{"message":{"content":"` + carrier + `","role":"user"},"timestamp":"2026-10-27T05:00:03.0+08:00","type":"user"}`,
		`{"message":{"content":[{"text":"ack two","type":"text"}],"role":"assistant"},"timestamp":"2026-10-27T05:00:03.5+08:00","type":"assistant"}`,
		`{"iteration":1,"timestamp":"2026-10-27T05:00:04.0+08:00","type":"request"}`,
		`{"message":{"content":"` + carrier + `","role":"user"},"timestamp":"2026-10-27T05:00:05.0+08:00","type":"user"}`,
		`{"message":{"content":[{"text":"ack three","type":"text"}],"role":"assistant"},"timestamp":"2026-10-27T05:00:05.5+08:00","type":"assistant"}`,
		`{"iteration":2,"timestamp":"2026-10-27T05:00:06.0+08:00","type":"request"}`,
		// The person's own line, which is a turn too.
		`{"message":{"content":"and now do the thing","role":"user"},"timestamp":"2026-10-27T05:00:07.0+08:00","type":"user"}`,
		`{"message":{"content":[{"text":"ack four","type":"text"}],"role":"assistant"},"timestamp":"2026-10-27T05:00:07.5+08:00","type":"assistant"}`,
		`{"iteration":3,"timestamp":"2026-10-27T05:00:08.0+08:00","type":"request"}`,
	}
	s, err := ParseFile(writeFixture(t, lines))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got, want := SessionTurns(s), 2; got != want {
		t.Fatalf("turns = %d, want %d (ONE delivery + the person's own line) — a count of 3 means a restatement was counted as an input, a count of 1 means the delivery was not", got, want)
	}
	// The delivery is a turn whose FACE is recorded, so a surface can say what
	// kind of input opened it instead of inferring it from the text.
	bounds := SessionTurnBounds(s)
	if len(bounds) == 0 {
		t.Fatalf("no turn boundaries at all — the fixture did not exercise the reader")
	}
	if got := s.Iterations[bounds[0]].TurnSource; got != "notify" {
		t.Fatalf("the delivery's turn records source %q, want %q", got, "notify")
	}
}
