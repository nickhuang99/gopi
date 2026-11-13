package agent

// chat_ops_roundcap_test.go — the bounded-ReAct round cap, pinned on the wire.
//
// The defect these tests close (session bbc4efd8-6bdd-4d6c-aa16-86bae2cecc00,
// 2026-09-24): a tool-enabled chat turn that reaches maxChatOpsToolRounds ended
// the conversation. The session JSONL records 39 tool rounds, ~1.5M prompt
// tokens, and then `session_end {error: {category: "unknown"}, exit_code: 1}`
// plus a crash dump — while the operator's screen still showed the input prompt.
// The cap was invisible until the process died: nothing told the MODEL to wrap
// up, and nothing told the USER that a budget existed or that it was the cap
// that ended the session.
//
// Both directions are pinned here:
//   (a) the model is TOLD (the warning rides the last chatOpsRoundsWarnLead
//       requests), so a turn at the cap can still land a partial answer;
//   (b) when it fails anyway, the error the REPL shows NAMES the cap, its
//       size, and the fact that the session survives it.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// chatOpsRoundCapProbePath is the path the always-looping fake model asks to
// read. It does not exist: the read tool returns a clean "no such file" result,
// which is all the round cap needs (the loop is about the model, not the tool).
const chatOpsRoundCapProbePath = "/nonexistent-round-cap-probe"

// alwaysToolUseServer answers EVERY request with the same tool_use round: the
// model that never stops calling tools — the exact behaviour the cap exists for.
// It counts requests and (optionally) records request bodies so a test can prove
// the cap really bounded the turn and what rode the wire.
func alwaysToolUseServer(t *testing.T, calls *int32, bodies *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if bodies != nil {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read request body: %v", err)
			}
			*bodies = append(*bodies, string(b))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeToolUseStream(w, "read", map[string]interface{}{"path": chatOpsRoundCapProbePath})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestChatOps_RoundCapIsWarnedBeforeItFires proves the model gets notice: the
// cap-approaching rounds carry the harness warning on the wire, and the rounds
// before them do not (a warning sent every round would be noise the model learns
// to ignore, on top of the tokens it costs).
func TestChatOps_RoundCapIsWarnedBeforeItFires(t *testing.T) {
	var calls int32
	var bodies []string
	srv := alwaysToolUseServer(t, &calls, &bodies)

	ag := chatOpsAgent(t, srv)
	if _, err := ag.ChatOps("keep going forever", nil); err == nil {
		t.Fatalf("a turn that never answers must fail at the round cap")
	}

	rounds := int(atomic.LoadInt32(&calls))
	if rounds != maxChatOpsToolRounds {
		t.Fatalf("the model was asked %d times, want exactly the cap (%d) — the turn is not bounded", rounds, maxChatOpsToolRounds)
	}

	// Requests are 1-based here: round n sends request n.
	firstWarned := chatOpsRoundsWarnAt + 1
	for i, body := range bodies {
		round := i + 1
		warned := strings.Contains(body, chatOpsRoundsWarnMarker)
		if round >= firstWarned && !warned {
			t.Errorf("round %d of %d carried NO wrap-up warning — the model would be told only by dying", round, rounds)
		}
		if round < firstWarned && warned {
			t.Errorf("round %d carried the wrap-up warning early — it belongs to rounds %d..%d (the last %d)",
				round, firstWarned, maxChatOpsToolRounds, chatOpsRoundsWarnLead)
		}
	}
}

// TestChatOps_RoundCapErrorNamesTheCap pins the ONE line the user reads. The old
// text ("chat turn exceeded 40 tool rounds without a final answer") did not say
// the session survived, and the REPL then killed it — so the operator had to
// decide from the shell prompt whether the process was still alive.
func TestChatOps_RoundCapErrorNamesTheCap(t *testing.T) {
	var calls int32
	srv := alwaysToolUseServer(t, &calls, nil)

	ag := chatOpsAgent(t, srv)
	_, err := ag.ChatOps("keep going forever", nil)
	if err == nil {
		t.Fatalf("a turn that never answers must fail at the round cap")
	}
	msg := err.Error()
	if !strings.Contains(msg, fmt.Sprintf("%d tool rounds", maxChatOpsToolRounds)) {
		t.Errorf("cap error = %q, want it to name the cap (%d tool rounds)", msg, maxChatOpsToolRounds)
	}
	if !strings.Contains(msg, "the session is still open") {
		t.Errorf("cap error = %q, want it to state that the session survives (the REPL keeps it open) — the fact the operator could not read before", msg)
	}
}

// TestChatOps_InterruptedTurnNeverBlamesTheModel: the same error type is reached
// when the user interrupted the turn (a cancelled run ctx fails every later
// round). Telling that user "the model exceeded 40 rounds" is a lie about their
// own keystroke, so the interrupt path must win the message (Rule 15).
//
// The interrupt root is installed HERE because the hand-built test agent has
// none (newTestAgent) while every production agent is built by NewAgent, which
// mints one — without it Interrupt() is a silent no-op and the loop would run to
// the cap, which is a property of the fixture, not of the product.
func TestChatOps_InterruptedTurnNeverBlamesTheModel(t *testing.T) {
	var calls int32
	srv := alwaysToolUseServer(t, &calls, nil)

	ag := chatOpsAgent(t, srv)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ag.interruptBase = base
	ag.interruptCancel = cancel

	ag.Interrupt()
	_, err := ag.ChatOps("keep going forever", nil)
	if err == nil {
		t.Fatalf("an interrupted turn must fail")
	}
	if !strings.Contains(err.Error(), "interrupt") {
		t.Errorf("interrupted turn error = %q, want the interrupt error, never the round-cap text", err)
	}
	if strings.Contains(err.Error(), fmt.Sprintf("%d tool rounds", maxChatOpsToolRounds)) {
		t.Errorf("interrupted turn error = %q blames the model's round count for the USER's interrupt", err)
	}
}

// TestChatOps_LateAnswerStillWins: the warning must be a WARNING, not a
// deadline — a model that wraps up after being told still returns its answer
// (only a turn that never answers fails).
func TestChatOps_LateAnswerStillWins(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if int(n) <= chatOpsRoundsWarnAt {
			claudeToolUseStream(w, "read", map[string]interface{}{"path": "/nonexistent-probe-file"})
			return
		}
		claudeTextStreamFragmented(w, []string{"Wrapping up with what is verified: the probe path does not exist, so nothing was read."}, "end_turn")
	}))
	defer srv.Close()

	ag := chatOpsAgent(t, srv)
	result, err := ag.ChatOps("loop until told to stop", nil)
	if err != nil {
		t.Fatalf("a turn that answers after the warning must succeed: %v", err)
	}
	if !strings.Contains(result, "Wrapping up") {
		t.Errorf("result = %q, want the answer the model produced after the wrap-up warning", result)
	}
	if n := int(atomic.LoadInt32(&calls)); n > maxChatOpsToolRounds {
		t.Errorf("turn ran %d rounds, want <= the cap %d", n, maxChatOpsToolRounds)
	}
}

// TestChatOps_RequestRoundCount pins the invariant the cap's wording relies on:
// one round = one request, no hidden retry — so "the turn exceeded 40 tool
// rounds" describes the requests that really happened.
func TestChatOps_RequestRoundCount(t *testing.T) {
	var calls int32
	srv := alwaysToolUseServer(t, &calls, nil)
	ag := chatOpsAgent(t, srv)
	if _, err := ag.ChatOps("once", nil); err == nil {
		t.Fatalf("expected the cap error")
	}
	if got := int(atomic.LoadInt32(&calls)); got != maxChatOpsToolRounds {
		t.Fatalf("requests = %d, want %d (one request per round; the cap counts rounds)", got, maxChatOpsToolRounds)
	}
}
