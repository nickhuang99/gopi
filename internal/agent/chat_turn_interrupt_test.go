package agent

// chat_turn_interrupt_test.go — THE INTERRUPTED TURN'S OWN OUTCOME (task
// 01a0e4390, agent side).
//
// WHY THIS FILE EXISTS (the measured defect, found while building the per-turn
// Ctrl+C): the run-scoped cancel already existed (InterruptRun), but a turn it
// abandoned came back indistinguishable from a FAILED turn — Chat returned
// whatever error the cancelled request produced, and the session JSONL got no
// witness at all. So an operator who stopped an answer could not tell "I stopped
// that" from "the provider broke", and a later reader of the transcript could
// not tell an abandoned answer from a short one.
//
// What is asserted here is the agent's share of the fix:
//  1. Chat returns the SENTINEL (IsTurnInterrupted), never a transport error.
//  2. The session JSONL gets turn_interrupted — reason ctrl-c, source operator,
//     and a partial_chars count of what had already been streamed.
//  3. partial_chars is measured from the deltas OnText RECEIVED (the same deltas
//     the operator's screen got), so it cannot be a hopeful restatement of what
//     the model sent.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/sessionevent"
)

// interruptStreamGap is how long the stub waits between two answer fragments.
// It is what gives the test a window in which the turn is genuinely in flight —
// the property under test is "cancelled MID-stream", not "cancelled at a
// convenient boundary".
const interruptStreamGap = 30 * time.Millisecond

// interruptStreamFragments is the answer, split so that "how much arrived" is a
// measurable question rather than a yes/no one.
var interruptStreamFragments = []string{"alpha ", "beta ", "gamma ", "delta ", "epsilon "}

// interruptStub serves a text answer in delayed fragments and then HOLDS the
// request open until the client gives up. Holding is the point: a stub that
// finished would make the test measure "the run completed", and the cancel has
// to land while the stream is still live.
func interruptStub(t *testing.T, streamed *int32, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
		writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		for _, frag := range interruptStreamFragments {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(interruptStreamGap):
			}
			fragJSON, _ := json.Marshal(frag) //stylegate:ignore marshalling a string built in-process cannot fail
			writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, fragJSON))
			if flusher != nil {
				flusher.Flush()
			}
			mu.Lock()
			*streamed++
			mu.Unlock()
		}
		// The answer is complete on the wire, but the response is not: the client
		// is left waiting, exactly as a slow provider leaves it. This is the
		// in-flight state the interrupt must be able to end.
		writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
}

// TestChat_TurnInterruptedReturnsTheSentinelAndRecordsTheTurn is the agent-side
// acceptance test for the per-turn interrupt.
func TestChat_TurnInterruptedReturnsTheSentinelAndRecordsTheTurn(t *testing.T) {
	var mu sync.Mutex
	var streamed int32
	server := interruptStub(t, &streamed, &mu)
	defer server.Close()

	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	defer sl.Close()

	ag := newTestAgent(t, server)
	ag.SessionLog = sl

	// Every delta OnText receives is counted HERE, from the test's own side, so
	// the JSONL's partial_chars can be compared against an independent tally.
	var gotMu sync.Mutex
	var received int
	ag.OnText = func(s string) {
		gotMu.Lock()
		received += len([]rune(s))
		gotMu.Unlock()
	}

	done := make(chan error, 1)
	go func() {
		_, err := ag.Chat("please answer at length", nil)
		done <- err
	}()

	// WAIT FOR THE TURN TO BE IN FLIGHT, then cancel. The wait is on the run
	// scope the RUN published (runScopedRoot, called by Chat itself), NOT on a
	// scope this test installed: pre-installing one is a race the run wins — its
	// own runScopedRoot overwrites the handle, and a cancel aimed at the test's
	// orphan context reaches nothing (and, having no deadline, leaves the run
	// waiting on a stub that never finishes). The observable is therefore the
	// production handle, and the fragment counter below says the STREAM is real
	// rather than merely started.
	if !waitForRunScopePublished(t, ag) {
		t.Fatal("Chat never published its run scope — the turn could not be interrupted")
	}
	// WAIT FOR THE STREAM: on the wire the stub is mid-answer, and on this side
	// the deltas have already reached OnText.
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := streamed
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stub never streamed far enough to interrupt mid-answer")
		}
		time.Sleep(2 * time.Millisecond) //stylegate:allow-sleep polling a streamed-fragment counter, the only observable for "the turn is now in flight"
	}
	if !ag.InterruptRun() {
		t.Fatal("InterruptRun found no run to abandon while the turn was streaming")
	}

	select {
	case err := <-done:
		if !IsTurnInterrupted(err) {
			t.Fatalf("Chat returned %v after a per-turn interrupt; want the turn-interrupted sentinel — a carrier must be able to tell this from a failed turn", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Chat never returned after the run was cancelled — the interrupt did not reach the in-flight stream")
	}

	gotMu.Lock()
	onTextRunes := received
	gotMu.Unlock()

	// THE RECORD: the run wrote the witness before it returned, so it is on disk
	// even if the carrier prints nothing.
	data, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read the session log: %v", err)
	}
	raw := string(data)
	ev := findTurnInterruptEvent(t, raw, sessionevent.EventTypeTurnInterrupted)
	if ev == nil {
		t.Fatalf("the session log has no %s witness — the abandoned turn is recorded as nothing:\n%s",
			sessionevent.EventTypeTurnInterrupted, raw)
	}
	if reason, _ := ev["reason"].(string); reason != InterruptReasonCtrlC {
		t.Errorf("turn_interrupted reason = %v, want %q", ev["reason"], InterruptReasonCtrlC)
	}
	if source, _ := ev["source"].(string); source != InterruptSourceOperator {
		t.Errorf("turn_interrupted source = %v, want %q", ev["source"], InterruptSourceOperator)
	}
	partial, ok := ev["partial_chars"].(float64)
	if !ok {
		t.Fatalf("turn_interrupted carries no numeric partial_chars: %v", ev["partial_chars"])
	}
	if partial <= 0 {
		t.Errorf("partial_chars = %v, want a positive count of the answer the operator had already seen", partial)
	}
	if onTextRunes > 0 && int(partial) > onTextRunes {
		t.Errorf("partial_chars = %v exceeds the %d runes OnText actually received — the count is not a measurement of what reached the carrier",
			partial, onTextRunes)
	}
}

// TestChat_InterruptRunBeforeAnyStreamStillEndsTheTurn: the cancel can land
// before the first delta (the operator reacts to a slow provider, not to text).
// The turn must still come back as an interruption — that is the case the
// original defect hurt most, because there was no partial answer to notice and
// the run looked like a plain failure.
func TestChat_InterruptRunBeforeAnyStreamStillEndsTheTurn(t *testing.T) {
	var mu sync.Mutex
	var streamed int32
	server := interruptStub(t, &streamed, &mu)
	defer server.Close()

	ag := newTestAgent(t, server)

	done := make(chan error, 1)
	go func() {
		_, err := ag.Chat("a question the operator gives up on", nil)
		done <- err
	}()
	if !waitForRunScopePublished(t, ag) {
		t.Fatal("Chat never published its run scope")
	}
	if !ag.InterruptRun() {
		t.Fatal("InterruptRun found no run to abandon")
	}
	select {
	case err := <-done:
		if !IsTurnInterrupted(err) {
			t.Fatalf("Chat returned %v; want the turn-interrupted sentinel for a turn cancelled before any text", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Chat never returned after an early interrupt")
	}
}

// waitForRunScopePublished waits until the agent carries a live run scope — the
// handle InterruptRun needs. It exists so the tests cancel the RUN'S OWN scope
// rather than one they installed first: whatever a test pre-installs, the run's
// own runScopedRoot overwrites on its first line, and a cancel aimed at the
// orphan then reaches nothing.
func waitForRunScopePublished(t *testing.T, a *Agent) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.runMu.Lock()
		live := a.runCtx != nil && a.runCancel != nil && a.runCtx.Err() == nil
		a.runMu.Unlock()
		if live {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond) //stylegate:allow-sleep polling the run's published scope, the handle the interrupt needs
	}
}

// findTurnInterruptEvent parses the JSONL and returns the first event of the given
// type, or nil. It reads the file the RUN wrote (not a re-serialisation), so a
// field the logger never emitted cannot be found here.
func findTurnInterruptEvent(t *testing.T, raw, typ string) map[string]interface{} {
	t.Helper()
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("session log line is not JSON: %v\n%s", err, line)
		}
		if got, _ := ev["type"].(string); got == typ {
			return ev
		}
	}
	return nil
}
