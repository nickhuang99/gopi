package agent

// message_end_seam_test.go — the assistant-MESSAGE boundary, and the runaway it
// stops in the FORMAT CLASSIFIER (the operator's reported screen).
//
// THE DEFECT (task 01a0ddaa). One turn with tools carries SEVERAL assistant
// messages: a round answers, its tools run, the next round answers. The answer's
// formatter is a cross-line state machine (lineedit.LineComposer): a fenced block
// that a message OPENS is correctly code — but when the message ends without
// closing it, the block stayed open for the REST of the turn, so the next
// message's prose and table rows were classified code and its `**bold**` was
// drawn as its literal markers. The operator saw exactly that (2026-09-26).
//
// The classifier's rule and the cell-level proof live in internal/lineedit
// (fence_runaway_test.go). What THIS file pins is the producer: the agent must
// ANNOUNCE that a message ended, at the round's stream end and BEFORE the round's
// answer text is delivered — otherwise no consumer can tell one message from the
// next, and the boundary is the caller's to know (the renderer's input is one
// stream of deltas).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// messageEndServer answers round 1 with an UNCLOSED fenced block (and a tool call,
// so a second round follows), then round 2 with prose in the same turn. It records
// the ORDER in which the seam saw the two calls.
func messageEndServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if *calls == 1 {
			// A message that STOPS INSIDE the fence it opened, plus a tool_use so
			// the turn continues into a second message.
			writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
			writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			writeClaudeSSE(w, "content_block_delta",
				"{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"关键的一处代码在这里：\\n"+
					FenceMarkerForTest+"go\\nopts = append(opts, tea.WithOutput(w))\"}}")
			writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
			writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"read","input":{}}}`)
			writeClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/dev/null\"}"}}`)
			writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
			writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
			writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
			return
		}
		// The SECOND message of the SAME turn: prose carrying a bold span.
		writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m2","usage":{"input_tokens":10,"output_tokens":0}}}`)
		writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		writeClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"the **bold conclusion** follows"}}`)
		writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":4}}`)
		writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	}))
}

// TestAgent_OnMessageEnd_FiresBetweenMessages pins the seam the fix rides on:
// one call per ASSISTANT MESSAGE of the turn, each landing AFTER the message it
// closes and BEFORE the next message's text.
//
// That ordering is the whole fix. The boundary cannot precede the message's own
// text — a message ends where it ends — but it must precede the NEXT one's, and
// the classifier's rule is exactly that: the block the ending message left open
// belongs to that message, so it is dropped at its end rather than inherited.
//
// A boundary that fired anywhere else would be wrong in a way this test can see:
// after the next message's text closes a block against the text that opened it,
// and before the text lets the previous message's fence classify it.
func TestAgent_OnMessageEnd_FiresBetweenMessages(t *testing.T) {
	calls := new(int)
	server := messageEndServer(t, calls)
	defer server.Close()

	var events []string
	ag := newStuckTestAgent(server.URL, 4)
	ag.OnMessageEnd = func() { events = append(events, seamEventEnd) }
	ag.OnText = func(delta string) {
		switch {
		case strings.Contains(delta, FenceMarkerForTest):
			events = append(events, seamEventOpenText)
		case strings.Contains(delta, secondMessageSpan):
			events = append(events, seamEventNextText)
		}
	}

	if _, err := ag.ChatOps("what draws the frame?", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if *calls < 2 {
		t.Fatalf("the mock was called %d time(s), want at least 2 (the turn must carry two messages)", *calls)
	}
	ends := 0
	for _, e := range events {
		if e == seamEventEnd {
			ends++
		}
	}
	if ends < 2 {
		t.Fatalf("OnMessageEnd fired %d time(s), want one per assistant message (>=2): events=%v", ends, events)
	}
	firstEnd := indexOf(events, seamEventEnd)
	opened := indexOf(events, seamEventOpenText)
	next := indexOf(events, seamEventNextText)
	if opened < 0 || next < 0 {
		t.Fatalf("the mock's two messages were not both delivered: events=%v", events)
	}
	if opened > firstEnd {
		t.Fatalf("the boundary landed BEFORE message 1's own text: events=%v", events)
	}
	if next < firstEnd {
		t.Fatalf("message 2's text arrived BEFORE a boundary — the unclosed block is inherited: events=%v", events)
	}
	t.Logf("seam order observed: %v (boundary count=%d)", events, ends)
}

// seamEvent* are this test's own event names: the seam's order, not its text.
const (
	seamEventEnd      = "message-end"
	seamEventOpenText = "text-with-fence"
	seamEventNextText = "text-next-message"
)

// secondMessageSpan is the emphasised text of the turn's SECOND message: the piece
// that stops being bold when the first message's unclosed fence runs away.
const secondMessageSpan = "bold conclusion"

// FenceMarkerForTest is the fence the mock streams (the format layer's own
// spelling is internal/lineedit's; this file asserts on the DELIVERY, so it names
// what it sends).
const FenceMarkerForTest = "```"

func indexOf(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
