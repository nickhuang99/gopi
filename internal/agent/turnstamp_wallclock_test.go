package agent

// turnstamp_wallclock_test.go — the ABSOLUTE half of the per-request turn
// stamp: the wallclock the request happened at, with its zone offset.
//
// The relative half (tick + gap) is measured from state INSIDE this process, so
// it is destroyed by a restart — which is exactly what `gopi chat --resume` does
// (a fresh clock, clock.go's lastRequest comment). The tests here pin the three
// properties the absolute half exists for, plus the two things it must NOT
// change:
//
//  1. the spelling: ISO-8601, second precision, a NUMERIC offset, one named
//     layout constant (never a literal at the Format site);
//  2. the zone is the stamp's OWN, not the reader's — two stamps of the same
//     instant in two zones render two different, both-correct strings;
//  3. it does not reset across a restart (a fresh clock's first request carries
//     `[t+0s]` and a wallclock that is LATER, not zero);
//  4. it did not move the two existing arms (First and gap still render their
//     own clauses, byte-identically);
//  5. it did not move the injection point: still exactly ONE message, still LAST.
//
// Plus the record-side agreement: the row the session JSONL holds and the text
// that went on the wire are the same bytes (one LogUser call, no second
// rendering — clock.go turnStampLine).

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
)

// stampWallclockPattern is the absolute half's shape as a REGEXP — the form the
// two FIXTURES that normalize a session log need (hippocampus_test.go's
// per-run-artifact normalizer and sessionbudget_everyround_test.go's
// "stamp and nothing else" pattern). It is a separate constant from
// turnStampWallclockLayout because a Go layout is not a regexp and deriving one
// from the other at run time would be a code generator for five characters of
// pattern; the pair is kept honest by TestStampWallclockPatternMatchesTheLayout,
// which formats a real moment through the PRODUCTION renderer and requires this
// pattern to match it. A hand-written regexp that stopped describing what
// clock.go paints therefore fails a test instead of silently passing logs
// through unnormalized (Rule 1: one named spelling per question, and a test that
// pins the second one to the first).
const stampWallclockPattern = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}`

// stampWallclockRe is the compiled form, used by the normalizers.
var stampWallclockRe = regexp.MustCompile(stampWallclockPattern)

// wallclockOf cuts the absolute half out of a rendered stamp — the token
// between the " @ " marker and the em dash that opens the arm's clause. It
// returns the token verbatim so an assertion compares whole strings rather than
// substrings (a prefix match would pass against "2026-11-07T04:50:12+08:00junk").
func wallclockOf(t *testing.T, rendered string) string {
	t.Helper()
	_, rest, ok := strings.Cut(rendered, " @ ")
	if !ok {
		t.Fatalf("the stamp carries no absolute half: %q", rendered)
	}
	at, _, ok := strings.Cut(rest, " — ")
	if !ok {
		t.Fatalf("the stamp carries no em-dash clause after the absolute half: %q", rendered)
	}
	return at
}

// TestTurnStamp_WallclockSpelling pins the format: second precision, a numeric
// zone offset, and a value that parses back into the very instant it was minted.
// The layout is the named constant turnStampWallclockLayout — this test formats
// through the production path and checks the RESULT, so a literal typed at the
// Format call would still have to agree with the constant to pass.
func TestTurnStamp_WallclockSpelling(t *testing.T) {
	// A fixed instant in a fixed, non-UTC zone: a UTC-only implementation and a
	// "local zone" implementation both fail here, which is the point (the test's
	// own zone is not the machine's).
	zone := time.FixedZone("UTC+05:30", 5*3600+30*60)
	moment := time.Date(2026, 11, 7, 4, 50, 12, 987654321, zone)

	got := wallclockOf(t, TurnStamp{Tick: "[t+~1m]", At: moment}.Render())

	if !strings.HasSuffix(got, "+05:30") {
		t.Fatalf("wallclock = %q, want the stamp's own offset +05:30 (a zone abbreviation or a bare local time is not an offset)", got)
	}
	if strings.ContainsAny(got, "Zz ") && !strings.HasSuffix(got, "+05:30") {
		t.Fatalf("wallclock = %q carries a zone designator instead of a numeric offset", got)
	}
	if strings.Contains(got, ".") {
		t.Fatalf("wallclock = %q carries sub-second digits, want second precision", got)
	}

	parsed, err := time.Parse(turnStampWallclockLayout, got)
	if err != nil {
		t.Fatalf("wallclock %q does not parse as the named layout %q: %v", got, turnStampWallclockLayout, err)
	}
	if parsed.Nanosecond() != 0 {
		t.Fatalf("wallclock %q round-trips with sub-second digits (%d ns)", got, parsed.Nanosecond())
	}
	// Truncated to the second, the value must be the SAME INSTANT that was
	// minted — not merely a string of the right shape.
	if !parsed.Equal(moment.Truncate(time.Second)) {
		t.Fatalf("wallclock %q parses to %v, want the minted instant %v (truncated to the second)",
			got, parsed, moment.Truncate(time.Second))
	}
}

// TestTurnStamp_WallclockUsesTheStampsOwnZone is the anti-"assume local" arm: two
// stamps of ONE instant in two zones render two different strings, and each is
// correct about its own zone. An implementation that normalised to time.Local
// would render one string twice.
func TestTurnStamp_WallclockUsesTheStampsOwnZone(t *testing.T) {
	instant := time.Date(2026, 11, 7, 4, 50, 12, 0, time.UTC)
	east := instant.In(time.FixedZone("UTC+08:00", 8*3600))
	west := instant.In(time.FixedZone("UTC-05:00", -5*3600))

	gotEast := wallclockOf(t, TurnStamp{Tick: "[t+1s]", At: east}.Render())
	gotWest := wallclockOf(t, TurnStamp{Tick: "[t+1s]", At: west}.Render())

	if gotEast == gotWest {
		t.Fatalf("both zones rendered the same string %q — the renderer normalised instead of using the stamp's zone", gotEast)
	}
	if !strings.HasSuffix(gotEast, "+08:00") || !strings.HasSuffix(gotWest, "-05:00") {
		t.Fatalf("offsets = %q / %q, want +08:00 and -05:00", gotEast, gotWest)
	}
	// Same instant: both parse back to the same time.Time (the offset is part of
	// the spelling, not part of the instant).
	pEast, errE := time.Parse(turnStampWallclockLayout, gotEast)
	pWest, errW := time.Parse(turnStampWallclockLayout, gotWest)
	if errE != nil || errW != nil {
		t.Fatalf("parse errors: %v / %v", errE, errW)
	}
	if !pEast.Equal(pWest) {
		t.Fatalf("the two renderings are different instants: %v vs %v", pEast, pWest)
	}
}

// TestTurnStamp_NoMomentIsStatedNotInvented is Rule 15's half of the design: a
// stamp with no moment says so in words. The failure it forbids is concrete —
// formatting a zero time in the local zone prints "0001-01-01T00:00:00+08:00",
// a fabricated instant wearing a real offset.
func TestTurnStamp_NoMomentIsStatedNotInvented(t *testing.T) {
	rendered := TurnStamp{Tick: "[t+5s]", SinceLast: 3 * time.Second}.Render()
	if !strings.Contains(rendered, turnStampWallclockUnknown) {
		t.Fatalf("a stamp with no moment rendered %q, want the explicit %q", rendered, turnStampWallclockUnknown)
	}
	if strings.Contains(rendered, "0001-01-01") {
		t.Fatalf("a zero moment was formatted as a real timestamp: %q", rendered)
	}
}

// TestTurnStamp_AbsoluteHalfDoesNotMoveTheTwoArms is the non-regression arm: the
// First and gap clauses are unchanged byte for byte, and the wallclock is an
// INSERTION INTO THE LEFT HALF — it never lands after the em dash, so a reader
// that keys on the clause (internal/gopilog, internal/replaydrive,
// internal/sessionevent) still finds it exactly where it was.
func TestTurnStamp_AbsoluteHalfDoesNotMoveTheTwoArms(t *testing.T) {
	at := time.Date(2026, 11, 7, 4, 50, 12, 0, time.UTC)

	first := TurnStamp{Tick: "[t+0s]", First: true, At: at}.Render()
	if !strings.HasSuffix(first, " — "+turnStampFirstSuffix) {
		t.Fatalf("first arm = %q, want it to still close with %q", first, turnStampFirstSuffix)
	}
	if !strings.HasPrefix(first, harnessMessagePrefix+"[t+0s] @ ") {
		t.Fatalf("first arm = %q, want the harness prefix, the tick, then the absolute half", first)
	}

	gap, _ := durationTier(9 * time.Minute)
	second := TurnStamp{Tick: "[t+~9m]", SinceLast: 9 * time.Minute, At: at}.Render()
	if !strings.HasSuffix(second, " — +"+gap+" "+turnStampGapSuffix) {
		t.Fatalf("gap arm = %q, want it to still close with %q", second, "+"+gap+" "+turnStampGapSuffix)
	}
	// The absolute half must not leak into the clause: everything right of the
	// em dash is the arm, and the arm contains no zone offset.
	_, clause, _ := strings.Cut(second, " — ")
	if strings.Contains(clause, "+08:00") || strings.Contains(clause, "T04:50") {
		t.Fatalf("the absolute half landed right of the em dash: %q", second)
	}
}

// TestTurnStamp_SurvivesAResume is the deliverable's central claim, measured on
// the smallest sample that can express it: a resumed `gopi chat` builds a FRESH
// clock (clock.go's lastRequest comment), so the resumed run's first request
// reports First and a tick near zero — and its WALLCLOCK is still the real
// moment, later than the pre-resume stamp's, never zero and never the zero time.
//
// The two clocks here stand for two processes of one conversation: the first has
// minted a stamp, the second is what --resume hands the same conversation.
func TestTurnStamp_SurvivesAResume(t *testing.T) {
	beforeResume := NewSessionClock()
	prev := beforeResume.TurnStamp()
	if prev.At.IsZero() {
		t.Fatal("the pre-resume stamp carries no absolute moment")
	}

	// --resume: a NEW clock over the SAME conversation (nothing carried over but
	// the messages themselves).
	time.Sleep(1100 * time.Millisecond) //stylegate:allow-sleep the resume claim is about a real interval between two processes
	afterResume := NewSessionClock()
	next := afterResume.TurnStamp()

	if next.At.IsZero() {
		t.Fatal("the resumed run's stamp carries no absolute moment — it was zeroed by the restart")
	}
	if !next.First {
		t.Fatalf("the resumed run's first request should report First (its gap base is gone): %+v", next)
	}
	if next.Tick != tickLabelOf(afterResume.Elapsed()) {
		t.Fatalf("the resumed run's tick = %q — expected the fresh clock's own tick (the RELATIVE half resets)", next.Tick)
	}
	if !next.At.After(prev.At) {
		t.Fatalf("the resumed run's wallclock %v is not after the pre-resume one %v — the absolute half did not advance",
			next.At, prev.At)
	}
	if !strings.Contains(next.Render(), next.At.Format(turnStampWallclockLayout)) {
		t.Fatalf("the resumed run's render %q does not carry its absolute moment", next.Render())
	}
	// And the pre-resume stamp is unchanged by the resume: it is a value, not a
	// pointer into a live clock.
	if !strings.Contains(prev.Render(), prev.At.Format(turnStampWallclockLayout)) {
		t.Fatalf("the pre-resume stamp changed under the resume: %q", prev.Render())
	}
}

// TestTurnStampInjection_StillLastAndStillOne pins the injection position while
// the stamp grew: exactly ONE message is appended and it is the LAST one, so a
// tool_use/tool_result pair can never be orphaned and the prefix cache sees the
// same message array with a different tail.
func TestTurnStampInjection_StillLastAndStillOne(t *testing.T) {
	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("the operator's question")
	ag.Ctx.AddAssistantMessage("an answer", nil)
	ag.Clock = NewSessionClock()

	before := len(ag.Ctx.WireMessages(false))
	msgs := ag.requestMessages(false)
	if len(msgs) != before+1 {
		t.Fatalf("request carries %d messages, want %d (exactly one injected stamp)", len(msgs), before+1)
	}
	last := msgs[len(msgs)-1]
	if last.Role != turnWireRoleUser {
		t.Fatalf("the last message's role = %q, want %q", last.Role, turnWireRoleUser)
	}
	if !strings.HasPrefix(last.Content, harnessMessagePrefix) {
		t.Fatalf("the last message is not a harness injection: %q", last.Content)
	}
	if !strings.Contains(last.Content, " @ ") || wallclockOf(t, last.Content) == turnStampWallclockUnknown {
		t.Fatalf("the injected stamp carries no absolute moment: %q", last.Content)
	}
	// Nothing was inserted anywhere else: the conversation prefix is unchanged,
	// compared field by field (a Message carries a slice, so it is not a
	// comparable value — Rule 15: compare the parts, never assume == works).
	prefix := ag.Ctx.WireMessages(false)[:before]
	for i := 0; i < before; i++ {
		if msgs[i].Role != prefix[i].Role || msgs[i].Content != prefix[i].Content {
			t.Fatalf("message %d of the conversation was rewritten by the injection: %+v vs %+v", i, msgs[i], prefix[i])
		}
	}
}

// TestTurnStampLine_RecordMatchesWire is the record-side agreement: the session
// JSONL row and the wire text are the SAME BYTES, because turnStampLine logs the
// string it returns (one rendering, no second copy). A reader auditing "what
// time did the model see" therefore never has to reconstruct it.
func TestTurnStampLine_RecordMatchesWire(t *testing.T) {
	_, sessionsDir := tempHome(t)

	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close()

	ag := &Agent{Ctx: &Context{}, Clock: NewSessionClock(), SessionLog: sl}
	wire := ag.turnStampLine()
	if wire == "" {
		t.Fatal("the stamp line is empty")
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("session log sync: %v", err)
	}

	lines := readJSONLLines(t, bareSessionPath(sessionsDir, sl.SessionID()))
	var recorded string
	for _, ev := range lines {
		if ev["type"] != "user" {
			continue
		}
		msg, ok := ev["message"].(map[string]interface{})
		if !ok {
			t.Fatalf("user event with no message body: %+v", ev)
		}
		if c, ok := msg["content"].(string); ok {
			recorded = c
		}
	}
	if recorded != wire {
		t.Fatalf("the session record holds %q but the wire carried %q — one rendering, not two", recorded, wire)
	}
	if !strings.Contains(recorded, " @ ") {
		t.Fatalf("the recorded stamp carries no absolute moment: %q", recorded)
	}
}

// TestStampWallclockPatternMatchesTheLayout pins the fixture regexp to the
// production layout: the pattern must match what clock.go actually paints. It is
// what stops the two normalizers going quietly blind (a stale pattern would leave
// a per-run wallclock inside an equivalence comparison, and the comparison would
// fail for a reason unrelated to the behaviour it exists to check).
func TestStampWallclockPatternMatchesTheLayout(t *testing.T) {
	for _, at := range []time.Time{
		time.Date(2026, 11, 7, 4, 50, 12, 0, time.UTC),
		time.Date(2026, 11, 7, 4, 50, 12, 0, time.FixedZone("UTC+08:00", 8*3600)),
		time.Date(2026, 11, 7, 4, 50, 12, 0, time.FixedZone("UTC-05:00", -5*3600)),
	} {
		rendered := wallclockOf(t, TurnStamp{Tick: "[t+0s]", First: true, At: at}.Render())
		if !stampWallclockRe.MatchString(rendered) {
			t.Fatalf("the fixture pattern %q does not match the painted wallclock %q", stampWallclockPattern, rendered)
		}
		if full := stampWallclockRe.FindString(rendered); full != rendered {
			t.Fatalf("the fixture pattern matched %q inside %q — it must cover the whole token", full, rendered)
		}
	}
}

// TestTurnStamp_WallclockIsBounded re-derives the size bound instead of trusting
// the comment beside it: format the WORST realistic line (both counters at four
// digits) through the production renderer and measure it against the named
// constant the budget's arithmetic uses.
func TestTurnStamp_WallclockIsBounded(t *testing.T) {
	at := time.Date(2026, 11, 7, 4, 50, 12, 0, time.FixedZone("UTC+08:00", 8*3600))
	for _, st := range []TurnStamp{
		{Tick: "[t+~1234d]", SinceLast: 1234 * 24 * time.Hour, At: at},
		{Tick: "[t+~1234d]", First: true, At: at},
	} {
		got := st.Render()
		if n := len(got); n > turnStampMaxChars {
			t.Fatalf("worst-case stamp is %d chars, over the %d bound: %q", n, turnStampMaxChars, got)
		}
		if tok := EstimateTokens(deepseek.Message{Role: turnWireRoleUser, Content: got}); tok > turnStampMaxTokens {
			t.Fatalf("worst-case stamp costs %d estimated tokens, over the %d bound: %q", tok, turnStampMaxTokens, got)
		}
	}
}
