package agent

// chat_turn_clock_test.go — the PER-REQUEST time stamp, asserted at the WIRE
// (against the requests a mock model actually receives), not against a copy of
// the sentence:
//
//  1. a PURE CONVERSATION turn — no tool call of any kind — still carries a
//     time signal (this is the defect the stamp closes: before it, a request
//     with no tool result carried no time at all);
//  2. the stamp is the CURRENT tick, and two requests of one session carry
//     INCREASING ticks;
//  3. the gap since the previous request is reported, in seconds, and the first
//     request of a session does NOT invent one;
//  4. a nil Clock is a no-op — Agent{} literals send the pre-existing bytes;
//  5. the existing tool-result tick is untouched (withTick still stamps every
//     tool result on both the chat and the task path);
//  6. the block that teaches the marker is on the CHAT prompts only, never on
//     the task assembly.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
)

// turnWireRoleUser is the wire role of a user message, named once because
// three assertions in this file ask "is this the user's turn" and a bare
// literal at each would be three answers to one question (Rule 1).
const turnWireRoleUser = "user"

// turnStampIn returns the stamp line found in a request's text, or "".
func turnStampIn(body string) string {
	for _, suffix := range []string{turnStampFirstSuffix, turnStampGapSuffix} {
		if strings.Contains(body, suffix) {
			return suffix
		}
	}
	return ""
}

// lastUserTextOf returns the text of the LAST user message in a request body —
// where the stamp must land.
func lastUserTextOf(t *testing.T, req *deepseek.ClaudeRequest) string {
	t.Helper()
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role != turnWireRoleUser {
			continue
		}
		var parts []string
		for _, b := range req.Messages[i].Content {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

// chatTurnClockAgent is the plain (no-tools) chat shape: a real clock, a mock
// model, no tools on the wire.
func chatTurnClockAgent(t *testing.T, server *httptest.Server) *Agent {
	t.Helper()
	newTestAgentEnv(t)
	ag := newTestAgent(t, server)
	ag.SystemPrompt = assembleSystemPrompt()
	ag.Clock = NewSessionClock()
	return ag
}

// TestChatTurnClock_PlainTurnCarriesTick is the acceptance test for the epic's
// central claim: a conversation round that calls NO tool still carries the
// current tick. The mock records every request body; the FIRST request of the
// session must already carry the stamp, because a pure Q&A turn has no other
// time signal anywhere on the wire.
func TestChatTurnClock_PlainTurnCarriesTick(t *testing.T) {
	var bodies []string
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		req := claudeRequestFrom(t, r)
		bodies = append(bodies, lastUserTextOf(t, req))
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, "an answer with no tools at all.", "end_turn")
	}))
	defer server.Close()

	ag := chatTurnClockAgent(t, server)
	if _, err := ag.Chat("what is the answer?", nil); err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if len(bodies) == 0 {
		t.Fatal("the mock model received no request")
	}
	first := bodies[0]
	if !tickRe.MatchString(first) {
		t.Fatalf("a pure conversation request carries no tick: %q", first)
	}
	if turnStampIn(first) == "" {
		t.Fatalf("the tick is present but not as the turn stamp: %q", first)
	}
}

// TestChatTurnClock_TickIncreasesAcrossRequests is the monotonicity guard at the
// REQUEST level (the tool-result half already has one): two consecutive turns of
// one session must carry non-decreasing ticks, so the model can never read the
// clock going backwards.
func TestChatTurnClock_TickIncreasesAcrossRequests(t *testing.T) {
	var ticks []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := claudeRequestFrom(t, r)
		body := lastUserTextOf(t, req)
		if lbl := tickRe.FindString(body); lbl != "" && turnStampIn(body) != "" {
			ticks = append(ticks, tickOf(lbl))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, "ok.", "end_turn")
	}))
	defer server.Close()

	ag := chatTurnClockAgent(t, server)
	for i := 0; i < 2; i++ {
		if _, err := ag.Chat(fmt.Sprintf("turn %d", i), ag.Ctx.Messages); err != nil {
			t.Fatalf("Chat() turn %d: %v", i, err)
		}
	}
	if len(ticks) < 2 {
		t.Fatalf("expected >= 2 stamped requests, got %d (%v)", len(ticks), ticks)
	}
	for i := 1; i < len(ticks); i++ {
		if ticks[i] < ticks[i-1] {
			t.Fatalf("tick went backwards across requests: %v", ticks)
		}
	}
}

// TestTurnStamp_FirstAndGap pins the two render arms and the GAP measurement
// directly on the clock — the smallest sample (Rule: minimal sample), with no
// HTTP in the way. The first stamp says FIRST (no gap is invented); the second
// reports the interval that really elapsed, and the interval is measured
// against the previous stamp, not against t0.
func TestTurnStamp_FirstAndGap(t *testing.T) {
	c := NewSessionClock()
	first := c.TurnStamp()
	if !first.First {
		t.Fatal("the session's first stamp claims a previous request")
	}
	if first.SinceLast != 0 {
		t.Fatalf("first stamp reports a gap of %v — an invented measurement", first.SinceLast)
	}
	if first.Tick == "" {
		t.Fatal("first stamp carries no tick")
	}
	if !tickRe.MatchString(first.Tick) {
		t.Fatalf("stamp tick = %q, want one of the %s forms", first.Tick, tickShapeLabel)
	}
	if !strings.Contains(first.Render(), turnStampFirstSuffix) {
		t.Fatalf("first render = %q, want the first-request form", first.Render())
	}

	time.Sleep(1100 * time.Millisecond) //stylegate:allow-sleep the gap assertion needs a real interval to measure
	second := c.TurnStamp()
	if second.First {
		t.Fatal("the second stamp still claims to be the first")
	}
	if second.SinceLast < time.Second {
		t.Fatalf("gap = %v after a 1.1s pause, want >= 1s", second.SinceLast)
	}
	got := second.Render()
	if !strings.Contains(got, turnStampGapSuffix) {
		t.Fatalf("second render = %q, want the gap form", got)
	}
	// The gap is folded by the SAME function as the tick (clock.go
	// durationTier), so a sub-minute pause still reads as exact seconds.
	wantGap, _ := durationTier(second.SinceLast)
	if !strings.Contains(got, "+"+wantGap) {
		t.Fatalf("second render = %q, want the measured gap +%s", got, wantGap)
	}

	// Nil / zero-valued clock: no stamp at all (the withTick convention).
	var nilClock *SessionClock
	if st := nilClock.TurnStamp(); st.Tick != "" || st.Render() != "" {
		t.Fatalf("nil clock produced a stamp: %+v / %q", st, st.Render())
	}
	if st := (&SessionClock{}).TurnStamp(); st.Render() != "" {
		t.Fatalf("zero-valued clock produced a stamp: %q", st.Render())
	}
}

// TestChatTurnClock_NilClockIsNoOp is the compatibility contract: an Agent with
// no clock (every Agent{} literal in the suite, and every embedded carrier that
// never wired one) sends the messages it always sent — the stamp is skipped, not
// rendered empty.
func TestChatTurnClock_NilClockIsNoOp(t *testing.T) {
	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("hello")
	plain := ag.requestMessages(false)
	if len(plain) != 1 || plain[0].Content != "hello" {
		t.Fatalf("nil clock changed the request: %+v", plain)
	}
	if ag.turnStampLine() != "" {
		t.Fatal("nil clock produced a stamp line")
	}
	// And with a clock, exactly ONE message is added — O(1) per request.
	ag.Clock = NewSessionClock()
	stamped := ag.requestMessages(false)
	if len(stamped) != len(plain)+1 {
		t.Fatalf("stamped request carries %d messages, want %d (exactly one stamp)", len(stamped), len(plain)+1)
	}
	last := stamped[len(stamped)-1]
	if last.Role != turnWireRoleUser || !tickRe.MatchString(last.Content) {
		t.Fatalf("last message = %+v, want a user-role stamp", last)
	}
}

// TestChatTurnClock_IncrementIsBounded measures the per-request token cost
// (chars/4, the same heuristic the context budget uses): the stamp must stay
// O(1) and small — a time signal that costs a paragraph is a time signal that
// gets trimmed away.
func TestChatTurnClock_IncrementIsBounded(t *testing.T) {
	c := NewSessionClock()
	first := c.TurnStamp().Render()
	second := c.TurnStamp().Render()
	if n := len(first); n == 0 || n > turnStampMaxChars {
		t.Fatalf("first stamp is %d chars, want 1..%d: %q", n, turnStampMaxChars, first)
	}
	if n := len(second); n == 0 || n > turnStampMaxChars {
		t.Fatalf("gap stamp is %d chars, want 1..%d: %q", n, turnStampMaxChars, second)
	}
	if tok := EstimateTokens(deepseek.Message{Role: turnWireRoleUser, Content: second}); tok > turnStampMaxTokens {
		t.Fatalf("stamp costs %d estimated tokens, want <= %d", tok, turnStampMaxTokens)
	}
	// O(1): the length does not depend on the session's age or on how many
	// requests came before — only the digits of two counters can move it.
	if d := len(second) - len(first); d > turnStampDigitDrift {
		t.Fatalf("stamp length drifted by %d chars between two requests", d)
	}
}

// TestChatTurnClock_ToolResultTickUnchanged is the regression guard for the
// EXISTING half: the stamp must not have moved the tool-result tick. A tool
// round on the chat path still prefixes its result, byte-identically, and the
// tick is still the clock's own label (one definition).
func TestChatTurnClock_ToolResultTickUnchanged(t *testing.T) {
	probe := chatOpsProbeFile(t)
	var sawToolResult, sawToolTick int32
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		req := claudeRequestFrom(t, r)
		if n >= 2 && claudeHasToolResult(req, chatOpsStoreProbeBody) {
			atomic.StoreInt32(&sawToolResult, 1)
			if claudeHasToolResult(req, "[t+") {
				atomic.StoreInt32(&sawToolTick, 1)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			claudeToolUseStream(w, "read", map[string]interface{}{"path": probe})
			return
		}
		claudeTextStream(w, "read it.", "end_turn")
	}))
	defer server.Close()

	newTestAgentEnv(t)
	ag := newTestAgent(t, server)
	ag.SystemPrompt = assembleSystemPrompt()
	ag.Clock = NewSessionClock()
	if _, err := ag.ChatOps("read the probe", nil); err != nil {
		t.Fatalf("ChatOps() error: %v", err)
	}
	if atomic.LoadInt32(&sawToolResult) == 0 {
		t.Fatal("the tool result never reached the second request")
	}
	if atomic.LoadInt32(&sawToolTick) == 0 {
		t.Fatal("the tool result lost its tick — the existing half regressed")
	}
}

// TestChatTurnClock_PromptBlockIsChatOnly is the third deliverable's guard: the
// sentence that explains the stamp reaches BOTH chat assemblies and reaches the
// TASK assembly never. A task run already re-reads the tick on every tool
// result; teaching it a second, request-level spelling would be a rule it does
// not need, stated differently from the one it has.
func TestChatTurnClock_PromptBlockIsChatOnly(t *testing.T) {
	chatBoundaryFixtureRepo(t)

	plain := chatSystemPromptServed(nil)
	if !strings.Contains(plain, chatTurnClockMarker) {
		t.Fatalf("the plain chat prompt does not explain the turn stamp:\n%s", plain)
	}
	toolsPath := chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()})
	if !strings.Contains(toolsPath, chatTurnClockMarker) {
		t.Fatalf("the --tools chat prompt does not explain the turn stamp:\n%s", toolsPath)
	}
	if task := assembleSystemPrompt(); strings.Contains(task, chatTurnClockMarker) {
		t.Fatal("the TASK assembly carries the chat turn-clock block — it belongs to the chat surface alone")
	}
	// The marker's own meaning must name the tick through its single spelling
	// (Rule 1/13): the prompt cannot drift from the format TickLabel produces.
	if !strings.Contains(chatTurnClockRules(), tickShapeLabel) {
		t.Fatalf("the block does not name the tick as %q", tickShapeLabel)
	}
}
