package agent

// sessionbudget_everyround_test.go — the SESSION BUDGET on EVERY request, not
// only at the commit beat.
//
// The defect this file pins: the 60-minute budget reached the model through
// exactly one channel, the string task(action=commit) returns (P3,
// sessionbudget_test.go). A run that investigates, reads code, or simply talks
// for twenty rounds without committing saw the budget NEVER — so the ceiling the
// prompt teaches ("a task that cannot finish inside it is TOO BIG") was
// invisible at the moment it mattered, which is BEFORE the round the model
// spends its remaining time on.
//
// The fix is one appended clause on the request-assembly path (clock.go:
// turnStampLine appends sessionBudgetLine verbatim), so what these tests assert
// is a property of the REQUESTS a mock model actually receives — never a copy of
// the sentence in the test:
//
//  1. the task loop's every round carries the budget progress tag;
//  2. the tag's numbers come from the framework's own measurement
//     (sessionBudgetStatus), byte-equal to what the commit return carries;
//  3. past sessionBudgetWarnPercent it carries MORE, in the EXISTING escalation
//     wording — no second threshold, no second phrasing;
//  4. adjacent rounds' tags advance (tick and percent both);
//  5. a nil Clock and an unlimited budget (0) degrade silently, rendering no
//     empty marker;
//  6. the per-request increment is O(1) — it does not grow with the history.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/taskverdict"
)

// The two numbers a reader checks in the tag, parsed out of the rendered text
// (the tag is prose; these are its machine-readable parts). Named once because
// several assertions ask "which tick / which percent", and a bare pattern at
// each site would be several answers to one question (Rule 1).
var (
	budgetTagTickRe    = tickRe
	budgetTagPercentRe = regexp.MustCompile(`\((\d+)% used`)

	// stampOnlyRe matches the per-request stamp and NOTHING ELSE — exactly one
	// of the two arms the sibling feature renders, built from that feature's own
	// suffix constants (so a re-worded suffix moves this pattern with it). It is
	// how a test states "no budget clause was appended": byte-level, with no
	// dependence on two clocks being read in the same second.
	// The stamp's own shape gained its ABSOLUTE half (clock.go turnStampFirstFmt /
	// turnStampGapFmt: "<tick> @ <wallclock> — <clause>"), so this pattern names
	// that half through stampWallclockPattern rather than assuming the pre-change
	// spelling — which would make "the budget appended nothing" unassertable. The
	// pattern still accepts EXACTLY ONE arm and NOTHING ELSE, so the claim it was
	// written for is unchanged: a budget clause on the line still fails it.
	stampOnlyRe = regexp.MustCompile("^" + regexp.QuoteMeta(harnessMessagePrefix) + `\[t\+(~)?\d+[smhd]\] @ ` +
		stampWallclockPattern + " — (" +
		regexp.QuoteMeta(turnStampFirstSuffix) + "|" + regexp.QuoteMeta(turnStampGapSuffix) + ")$")
)

// budgetTagMaxChars bounds the tag a single request may carry. It is the
// escalation template's own length, filled with the widest values it can render
// (a 1h0m0s budget, three-digit elapsed and percent): 365 characters measured,
// so 400 is the bound with room for the widening the templates allow. The point
// of the constant is the O(1) claim — anything that grows with the session
// (a cumulative per-round report, the history itself) blows it immediately.
const budgetTagMaxChars = 400

// budgetTagIn returns the budget clause of the LAST user message of a request
// (where the per-request stamp lands), or "" when the request carries none.
func budgetTagIn(t *testing.T, req *deepseek.ClaudeRequest) string {
	t.Helper()
	text := lastUserTextOf(t, req)
	idx := strings.Index(text, sessionBudgetPrefix)
	if idx < 0 {
		return ""
	}
	return text[idx:]
}

// stampAndTagIn returns the whole per-request stamp — the tick, the gap and the
// budget clause, which is the ORDER the request carries them in (clock.go
// appends the budget line to the time stamp). Tests that ask "did the tick
// advance" need the whole line: the budget clause alone begins AFTER the tick
// has already been rendered.
func stampAndTagIn(t *testing.T, req *deepseek.ClaudeRequest) string {
	t.Helper()
	text := lastUserTextOf(t, req)
	idx := strings.Index(text, harnessMessagePrefix)
	if idx < 0 {
		return ""
	}
	return text[idx:]
}

// budgetTagTick returns the seconds the tag's tick stands for, or -1. It reads
// the label through the package's own tick reader, so the folded units
// ("+~31m") compare correctly against the sub-minute ones and the "advancing
// tick" assertion below stays about the CLOCK rather than about a format.
func budgetTagTick(tag string) int {
	m := budgetTagTickRe.FindString(tag)
	if m == "" {
		return -1
	}
	return tickOf(m)
}

// budgetTagPercent returns the used percentage carried by a tag, or -1.
func budgetTagPercent(tag string) int {
	m := budgetTagPercentRe.FindStringSubmatch(tag)
	if m == nil {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
		return -1
	}
	return n
}

// TestSessionBudgetEveryRound_TaskLoopCarriesTheTag is the epic's core
// acceptance at the real entry point: `Agent.Run` — the worker task main loop —
// sends requests that EACH carry the budget progress tag. Several rounds run, so
// a tag that only rides the first request, or only the round after a commit,
// fails here.
func TestSessionBudgetEveryRound_TaskLoopCarriesTheTag(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(probe, []byte("x\n"), 0644); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	newTestAgentEnv(t)
	t.Setenv(sessionBudgetEnv, "1") // a 1-minute budget: the percent moves

	var tags []string
	var stamps []string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		req := claudeRequestFrom(t, r)
		if tag := budgetTagIn(t, req); tag != "" {
			tags = append(tags, tag)
		}
		if line := stampAndTagIn(t, req); line != "" {
			stamps = append(stamps, line)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch calls {
		case 1, 2:
			// A real interval before answering round 1, so round 2's stamp has
			// something to have advanced by: a monotonicity check over two
			// identical instants would pass vacuously.
			if calls == 1 {
				time.Sleep(1100 * time.Millisecond) //stylegate:allow-sleep an advancing-tick assertion needs a real interval to measure
			}
			claudeToolUseStream(w, "read", map[string]interface{}{"path": probe})
		default:
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		}
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Clock = NewSessionClock()
	if _, err := ag.Run("read the probe file"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(tags) != calls || len(stamps) != calls {
		t.Fatalf("%d/%d requests carried a budget tag and %d/%d the stamp — a round that does not commit saw no budget (%v)",
			len(tags), calls, len(stamps), calls, tags)
	}
	for i, tag := range tags {
		if !strings.Contains(tag, "% used") {
			t.Fatalf("request %d carries %q, want the elapsed/budget/percent report", i+1, tag)
		}
	}
	// Adjacent requests advance, in BOTH numbers the stamp carries: the clock's
	// tick and the framework's percentage.
	first, second := stamps[0], stamps[1]
	if budgetTagTick(second) <= budgetTagTick(first) {
		t.Fatalf("ticks did not advance across requests: %q then %q", first, second)
	}
	if budgetTagPercent(tags[1]) <= budgetTagPercent(tags[0]) {
		t.Fatalf("percent did not advance across requests: %q then %q", tags[0], tags[1])
	}
}

// TestSessionBudgetEveryRound_PlainConversationRoundCarriesTheTag is the second
// acceptance, separated from the tool path on purpose: the round where the model
// calls NOTHING is exactly the round that used to carry no time or budget signal
// at all. The mock records whether any request it saw carried a tool_result, so
// "the tag arrived on a pure conversation round" cannot be satisfied by a tool
// round in disguise.
func TestSessionBudgetEveryRound_PlainConversationRoundCarriesTheTag(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(sessionBudgetEnv, "60")

	var tag string
	sawToolResult := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := claudeRequestFrom(t, r)
		sawToolResult = sawToolResult || claudeHasToolResult(req, "anything")
		if got := budgetTagIn(t, req); got != "" {
			tag = got
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, completedTaskListResponse, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.Clock = NewSessionClock()
	if _, err := ag.Run("answer with one sentence, no tools"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if sawToolResult {
		t.Fatal("the mock saw a tool_result — this test requires a round with no tool call at all")
	}
	if tag == "" {
		t.Fatal("a request with no tool call carried no budget tag — the defect this epic closes")
	}
	if budgetTagPercent(tag) < 0 {
		t.Fatalf("the tag on the plain round is not the framework's report: %q", tag)
	}
}

// TestSessionBudgetTag_IsTheFrameworksOwnMeasurement pins the arithmetic: the
// tag's numbers are sessionBudgetStatus()'s, rendered through the same format
// constant the commit return uses — so the model is never asked to compute
// elapsed/percent itself, and the two surfaces cannot drift apart.
func TestSessionBudgetTag_IsTheFrameworksOwnMeasurement(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(sessionBudgetEnv, "60")

	ag := &Agent{Ctx: &Context{}, Clock: &SessionClock{t0: time.Now().Add(-10 * time.Minute)}}
	st, ok := ag.sessionBudgetStatus()
	if !ok {
		t.Fatal("sessionBudgetStatus reported no claim for a finite budget and a live clock")
	}
	want := fmt.Sprintf(sessionBudgetReportFmt, st.Elapsed, st.Budget, st.Percent)
	line := ag.turnStampLine()
	if !strings.HasSuffix(line, want) {
		t.Fatalf("request stamp = %q, want it to end with the framework's own report %q", line, want)
	}
	if got := budgetTagPercent(want); got != st.Percent {
		t.Fatalf("tag percent = %d, measurement = %d", got, st.Percent)
	}

	// The conversation itself is untouched: the stamp is a property of NOW, so a
	// stored copy would go stale on the next request.
	ag.Ctx.AddUserMessage("hello")
	before := len(ag.Ctx.Messages)
	_ = ag.requestMessages(false)
	if len(ag.Ctx.Messages) != before {
		t.Fatalf("requestMessages mutated the conversation: %d -> %d messages", before, len(ag.Ctx.Messages))
	}
}

// TestSessionBudgetTag_EscalatesPastTheMark is the third acceptance: past
// sessionBudgetWarnPercent the tag carries MORE (elapsed, remaining, the mark and
// the one available action), and that extra wording is the EXISTING escalation
// text — byte-equal to the clause the commit return carries, so the system has
// one phrasing of escalation, not two.
func TestSessionBudgetTag_EscalatesPastTheMark(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(sessionBudgetEnv, "60")

	ag := &Agent{Ctx: &Context{}, Clock: &SessionClock{t0: time.Now().Add(-46 * time.Minute)}}
	st, ok := ag.sessionBudgetStatus()
	if !ok || !st.PastMark {
		t.Fatalf("precondition: 46m of a 60m budget must be past the %d%% mark (percent=%d, ok=%v)",
			sessionBudgetWarnPercent, st.Percent, ok)
	}

	below := &Agent{Ctx: &Context{}, Clock: &SessionClock{t0: time.Now().Add(-10 * time.Minute)}}
	belowTag := below.turnStampLine()
	pastTag := ag.turnStampLine()

	if strings.Contains(belowTag, taskverdict.TooBig) {
		t.Fatalf("below the mark the tag must not escalate: %q", belowTag)
	}
	for _, want := range []string{
		"remaining",
		fmt.Sprintf("%d%% mark", sessionBudgetWarnPercent),
		taskverdict.TooBig,
	} {
		if !strings.Contains(pastTag, want) {
			t.Fatalf("past the mark the tag must carry %q, got %q", want, pastTag)
		}
	}
	// One escalation object, one source: the tag's clause IS the commit
	// return's clause, byte for byte.
	if !strings.HasSuffix(pastTag, ag.sessionBudgetLine()) {
		t.Fatalf("the request tag (%q) does not end with the commit return's line (%q) — two spellings of one escalation",
			pastTag, ag.sessionBudgetLine())
	}
	// Strictly more informative than the below-the-mark tag — which is what
	// "escalation" means here.
	if len(pastTag) <= len(belowTag) {
		t.Fatalf("the escalated tag (%d chars) is not longer than the plain one (%d chars)", len(pastTag), len(belowTag))
	}
}

// TestSessionBudgetTag_UnlimitedAndNilClockDegrade pins the two silent
// degradations (Rule 15): with no ceiling the request carries the time stamp and
// NOTHING about a budget; with no clock it carries neither; in neither case is
// an empty marker rendered.
func TestSessionBudgetTag_UnlimitedAndNilClockDegrade(t *testing.T) {
	newTestAgentEnv(t)

	// (a) budget 0 = unlimited: the time stamp survives INTACT — the request
	// carries exactly one arm of the stamp and no budget clause at all, which is
	// the byte-level statement "nothing was appended" (no second clock read is
	// involved, so the assertion cannot flake on a second boundary).
	t.Setenv(sessionBudgetEnv, "0")
	ag := &Agent{Ctx: &Context{}, Clock: &SessionClock{t0: time.Now().Add(-30 * time.Minute)}}
	unlimited := ag.turnStampLine()
	if strings.Contains(unlimited, sessionBudgetPrefix) {
		t.Fatalf("unlimited budget rendered a budget clause: %q", unlimited)
	}
	if !stampOnlyRe.MatchString(unlimited) {
		t.Fatalf("unlimited budget changed the stamp: %q, want the pre-budget shape (%s)", unlimited, stampOnlyRe)
	}

	// (b) nil clock: no stamp at all, and the wire messages are the plain ones —
	// exactly what an Agent{} literal sent before this epic.
	t.Setenv(sessionBudgetEnv, "60")
	bare := &Agent{Ctx: &Context{}}
	bare.Ctx.AddUserMessage("hello")
	if got := bare.turnStampLine(); got != "" {
		t.Fatalf("nil clock rendered %q, want nothing", got)
	}
	msgs := bare.requestMessages(false)
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("nil clock changed the request: %+v", msgs)
	}
}

// TestSessionBudgetTag_IncrementIsBounded is the O(1) acceptance, measured: the
// clause a request gains is exactly one rendered line, and it does not grow as
// the session ages or as its history grows (a cumulative per-round report would).
func TestSessionBudgetTag_IncrementIsBounded(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(sessionBudgetEnv, "60")

	ag := &Agent{Ctx: &Context{}, Clock: &SessionClock{t0: time.Now().Add(-10 * time.Minute)}}
	tag := ag.sessionBudgetLine()
	line := ag.turnStampLine()

	// The clause is ONE separator plus the line itself.
	if !strings.HasSuffix(line, " "+tag) {
		t.Fatalf("stamp %q does not end with one separator plus the budget line %q", line, tag)
	}
	// The escalation arm's template is the longest rendering; both are bounded
	// by the same constant-sized sentences, which is why one cap holds for it.
	if len(tag) == 0 || len(tag) > budgetTagMaxChars {
		t.Fatalf("budget tag is %d chars, want 1..%d: %q", len(tag), budgetTagMaxChars, tag)
	}

	// O(1) in the other direction too: a long conversation does not enlarge the
	// tag — the tag is a function of the clock and the budget, never of the
	// history it rides on.
	for i := 0; i < 200; i++ {
		ag.Ctx.AddUserMessage(fmt.Sprintf("history line %d", i))
	}
	after := ag.turnStampLine()
	if d := len(after) - len(line); d > turnStampDigitDrift {
		t.Fatalf("the tag grew by %d chars as the history grew by 200 messages (want <= %d): %q", d, turnStampDigitDrift, after)
	}
}

// TestSessionBudgetTag_CommitReturnUnchanged is the regression arm the epic asks
// for explicitly: the commit beat's line is still rendered by sessionBudgetLine
// and the REQUEST path only reads it, so the return the model already relied on
// is unchanged.
func TestSessionBudgetTag_CommitReturnUnchanged(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(sessionBudgetEnv, "60")

	a := newAutoTaskAgent(t)
	a.Clock = &SessionClock{t0: time.Now().Add(-46 * time.Minute)}
	got := a.commitTaskItem(map[string]interface{}{
		"task_id":    "1",
		"status":     "completed",
		"conclusion": "budget every round",
	})
	if !strings.HasSuffix(got, a.sessionBudgetLine()) {
		t.Fatalf("commit return %q no longer ends with the budget line %q", got, a.sessionBudgetLine())
	}
	if !strings.Contains(got, taskverdict.TooBig) {
		t.Fatalf("commit return lost the past-the-mark action: %q", got)
	}
}

// TestSessionBudgetTag_UnlimitedCommitReturnShape is the byte-identity arm: with
// budget 0 the commit return carries no budget clause at all — the same
// invariant sessionbudget_test.go pins, re-asserted here because this epic
// touched the function that renders it.
func TestSessionBudgetTag_UnlimitedCommitReturnShape(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv(sessionBudgetEnv, "0")

	a := newAutoTaskAgent(t)
	a.Clock = NewSessionClock()
	got := a.commitTaskItem(map[string]interface{}{
		"task_id":    "1",
		"status":     "completed",
		"conclusion": "unlimited shape",
	})
	if strings.Contains(got, sessionBudgetPrefix) {
		t.Fatalf("unlimited budget decorated the commit return: %q", got)
	}
}

// TestSessionBudgetTag_WithTickUnchanged is the other regression arm: the
// tool-result tick is a DIFFERENT channel, and the budget clause must not have
// moved it.
func TestSessionBudgetTag_WithTickUnchanged(t *testing.T) {
	ag := &Agent{}
	if got := ag.withTick("body"); got != "body" {
		t.Fatalf("nil clock withTick changed: %q", got)
	}
	ag.Clock = NewSessionClock()
	got := ag.withTick("body")
	if !strings.HasPrefix(got, "[t+") || !strings.HasSuffix(got, " body") {
		t.Fatalf("withTick shape changed: %q", got)
	}
	if strings.Contains(got, sessionBudgetPrefix) {
		t.Fatalf("the budget clause leaked into a tool result: %q", got)
	}
}
