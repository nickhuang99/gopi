package agent

// context_pressure_test.go — the DEFAULT path: a full window WARNS, removes
// nothing.
//
// These tests exist because the defect they close was silent. The old
// trimContext reduced the conversation by default, and nothing told the operator
// — so a session could lose its middle and the human's only instrument was
// noticing that the model "forgot". The three facts pinned here are therefore
// each independently load-bearing:
//
//  1. the message SEQUENCE is untouched across a call that crosses the trigger
//     (not "fewer were removed" — byte-for-byte the same);
//  2. the operator is TOLD, through the carrier callback, with the numbers;
//  3. the announcement repeats once per level, not once per round (a warning on
//     every round is noise the operator learns to skip).

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

// newPressureTestAgent builds the minimum trimContext needs with automatic
// reduction LEFT OFF — the shipped default (context_pressure.go). No HTTP client
// is wired on purpose: the warn-only path must not reach the summarizer at all,
// so a call would be a nil-pointer panic rather than a silent pass.
func newPressureTestAgent(window int) *Agent {
	return &Agent{
		Logger:              log.New(io.Discard, "", 0),
		ContextWindow:       window,
		AutoReduceEnabled:   false,
		CompactionEnabled:   true,
		CompactTriggerRatio: DefaultCompactTriggerRatio,
		CompactTargetRatio:  DefaultCompactTargetRatio,
		CompactKeepRecent:   DefaultCompactKeepRecent,
		Ctx:                 &Context{},
	}
}

// contextPressureFixture is one history plus an over-threshold token count, so
// every test below starts from "the window is nearly full" without depending on
// how many characters a message happens to cost.
func contextPressureFixture(t *testing.T, ag *Agent) []string {
	t.Helper()
	fillHistory(ag.Ctx, 10, 40)
	limit := int(float64(ag.ContextWindow) * ag.CompactTriggerRatio)
	ag.Ctx.SetPromptTokens(limit + 1) // strictly over the trigger
	if got := ag.Ctx.EstimatedTokens(); got <= limit {
		t.Fatalf("precondition: expected est > limit (%d), got %d", limit, got)
	}
	before := make([]string, 0, len(ag.Ctx.Messages))
	for _, m := range ag.Ctx.Messages {
		before = append(before, m.Role+"\x00"+m.Content)
	}
	return before
}

// TestContextPressure_DefaultRemovesNothingAndWarns is the whole policy in one
// test: over the trigger, with automatic reduction OFF (the default), the
// conversation is IDENTICAL afterwards and the operator was told.
func TestContextPressure_DefaultRemovesNothingAndWarns(t *testing.T) {
	ag := newPressureTestAgent(testThresholdWindow)
	before := contextPressureFixture(t, ag)

	var notices []string
	ag.OnUserNotice = func(text string) { notices = append(notices, text) }

	ag.trimContext(context.Background())

	if len(ag.Ctx.Messages) != len(before) {
		t.Fatalf("the default path must remove NOTHING: had %d message(s), now %d", len(before), len(ag.Ctx.Messages))
	}
	for i, want := range before {
		got := ag.Ctx.Messages[i].Role + "\x00" + ag.Ctx.Messages[i].Content
		if got != want {
			t.Fatalf("message %d changed on the warn-only path: want %q got %q", i, want, got)
		}
	}
	if len(notices) != 1 {
		t.Fatalf("expected exactly 1 notice, got %d: %q", len(notices), notices)
	}
	text := notices[0]
	// The notice must carry the three things the operator acts on, and the
	// numbers must be the ones the trigger judged — a warning whose own figures
	// cannot be checked against the run is a warning a reader stops trusting.
	used := ag.Ctx.EstimatedTokens()
	limit := int(float64(ag.ContextWindow) * ag.CompactTriggerRatio)
	for _, want := range []string{
		fmt.Sprintf("%d of %d tokens", used, limit),
		fmt.Sprintf("of the %d-token window", ag.ContextWindow),
		AutoReduceEnv,
		"START A NEW SESSION",
		"session_search",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("notice is missing %q; got:\n%s", want, text)
		}
	}
}

// TestContextPressure_AnnouncesOncePerLevel pins the granularity: a second
// round at the same level says nothing, a round that crosses the next step
// speaks again. Without this the notice would fire on every round of a full
// session, which is the failure mode that teaches an operator to ignore it.
func TestContextPressure_AnnouncesOncePerLevel(t *testing.T) {
	ag := newPressureTestAgent(testThresholdWindow)
	contextPressureFixture(t, ag)

	var notices int
	ag.OnUserNotice = func(string) { notices++ }

	ag.trimContext(context.Background())
	if notices != 1 {
		t.Fatalf("first crossing: expected 1 notice, got %d", notices)
	}
	ag.trimContext(context.Background())
	ag.trimContext(context.Background())
	if notices != 1 {
		t.Fatalf("same level repeated: expected still 1 notice, got %d", notices)
	}

	// Now cross the NEXT bucket. The bucket is a percentage of the window, so the
	// next level is (current bucket + contextPressureNoticeStep)% of it — computed
	// from the constants rather than hardcoded, so a change to the step or the
	// window cannot leave this test asserting a number that no longer exists.
	window := ag.ContextWindow
	pct := float64(ag.Ctx.EstimatedTokens()) / float64(window) * 100
	nextPct := float64(contextPressureBucket(pct) + contextPressureNoticeStep)
	ag.Ctx.SetPromptTokens(int(float64(window)*nextPct/100) + 1)
	ag.trimContext(context.Background())
	if notices != 2 {
		t.Fatalf("crossing the next level (%.0f%%): expected a 2nd notice, got %d", nextPct, notices)
	}
}

// TestContextPressure_NoCarrierStillRecordsTheWarning is Rule 15's half: a run
// with nobody to tell (`gopi chat --once`, a scripted caller) must not DROP the
// statement — it goes to the Logger, and that line says which channel it took.
func TestContextPressure_NoCarrierStillRecordsTheWarning(t *testing.T) {
	var logged strings.Builder
	ag := newPressureTestAgent(testThresholdWindow)
	ag.Logger = log.New(&logged, "", 0)
	contextPressureFixture(t, ag)

	ag.trimContext(context.Background())

	out := logged.String()
	if !strings.Contains(out, "context pressure") {
		t.Fatalf("a run with no carrier must still record the warning; log was:\n%s", out)
	}
	if !strings.Contains(out, "no user-facing carrier installed") {
		t.Errorf("the log line must say WHICH channel it took (Rule 15), got:\n%s", out)
	}
}

// TestContextPressure_OptInRestoresTheReductionPath is the other side of the
// switch: GOPI_CONTEXT_AUTOREDUCE=1 is what a long unattended run says when it
// WANTS the old behaviour, and there the reduction still happens — no notice,
// because the run asked for it.
func TestContextPressure_OptInRestoresTheReductionPath(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, testThresholdWindow)
	// newCompactTestAgent already opts in (AutoReduceEnabled: true); assert the
	// fixture rather than assuming it.
	if !ag.AutoReduceEnabled {
		t.Fatal("fixture: newCompactTestAgent must build an OPTED-IN agent")
	}
	fillHistory(ag.Ctx, 20, 500)

	var notices int
	ag.OnUserNotice = func(string) { notices++ }

	ag.trimContext(context.Background())

	if *calls != 1 {
		t.Fatalf("opted-in run: expected 1 summarizer call, got %d", *calls)
	}
	if notices != 0 {
		t.Fatalf("opted-in run asked for the reduction, so it must not be warned: got %d notice(s)", notices)
	}
}

// TestWarnContextPressureCoversTheNoToolsChatPath pins the second entry point.
//
// Agent.Chat (a plain `gopi chat`, no --tools) never calls trimContext, so the
// warning had to be reachable from a function THAT path calls. Measured on this
// tree 2026-11-08: `gopi chat --tools` printed the warning at
// GOPI_CONTEXT_WINDOW=2000 while `gopi chat` printed nothing for the same
// conversation. This test is the cheap half of that proof — the function warns
// over the trigger, stays silent under it, and REDUCES NOTHING even with
// automatic reduction opted in (this path has never reduced, and must not start).
func TestWarnContextPressureCoversTheNoToolsChatPath(t *testing.T) {
	ag := newPressureTestAgent(testThresholdWindow)
	ag.AutoReduceEnabled = true // even opted in, this path only WARNS
	contextPressureFixture(t, ag)
	before := len(ag.Ctx.Messages)

	var notices int
	ag.OnUserNotice = func(string) { notices++ }

	ag.warnContextPressure()
	if notices != 1 {
		t.Fatalf("over the trigger: expected 1 notice, got %d", notices)
	}
	if len(ag.Ctx.Messages) != before {
		t.Fatalf("the no-tools chat path must not reduce: had %d message(s), now %d", before, len(ag.Ctx.Messages))
	}

	// Under the trigger: silence.
	under := newPressureTestAgent(testThresholdWindow)
	under.OnUserNotice = func(string) { notices++ }
	fillHistory(under.Ctx, 2, 20)
	under.warnContextPressure()
	if notices != 1 {
		t.Fatalf("under the trigger the warning must stay quiet, got %d notice(s)", notices)
	}
}
