package agent

// session_message_notice_once_test.go — the DUPLICATE-STATEMENT case for
// `gopi notify --session <id> --message <text>`.
//
// THE DEFECT THIS PINS. A delivered message is injected TWICE on the chat
// path:
//
//  1. the idle console's wake arm mints the next turn directly from the queue,
//     and that turn's opening message IS the notice statement
//     (`ChatJobNoticeOpenLine` → `sessionMessageFact`, logged by ChatOps as the
//     turn's user message). The message is now part of the session's
//     conversation;
//  2. that turn's OWN round boundary (internal/tools/session_message.go's
//     `SessionInboxLoad`) injected the very same text from the conversation's
//     own history — and, because the queue was never drained before the take,
//     injected it ONCE MORE as a second statement.
//
// The session record shows both statements, and the second is not the first one
// replayed: it is `sessionMessageFact` applied to the history copy, word for
// word, which is exactly why the operator reads one notification as two.
//
// WHY A REGRESSION GUARD IS NOT ENOUGH AND THIS MUST FAIL WITHOUT THE FIX. The
// existing cases assert the delivery is not re-injected on a turn that had
// nothing to do with it (session_inbox_test.go's "once per turn" case drives a
// PLAIN turn). This case drives the shape the defect needs — a turn whose
// opening message IS the notice — which is the shape the idle wake produces on
// a real console.
//
// THE OBSERVABLE IS THE STATEMENT, never the raw text: the delivered text
// legitimately appears once in the replayed conversation, so a count of the
// text alone cannot tell a duplicate from a replay. The statement
// ("[system] An external process sent this session a message:") is what a
// second injection adds.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

// sessionMessageNoticeOnceText is the delivered text: distinctive, so that the
// statement count cannot be satisfied by any other harness message.
const sessionMessageNoticeOnceText = "NOTIFY-OPENLINE-7c19: delivered while the console was idle"

// TestSessionMessageIsInjectedOnceWhenTheTurnOpensWithIt is the duplicate
// statement case: a turn whose opening message IS the notice must leave the
// statement in the conversation exactly ONCE.
func TestSessionMessageIsInjectedOnceWhenTheTurnOpensWithIt(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a request body the mock itself served
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"acknowledged"}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	// A PERSISTENT chat: one session log across turns — the shape the console
	// runs, and the one a duplicate statement is about.
	ag.PersistChatSession = true
	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() {
		if cerr := sl.Close(); cerr != nil {
			t.Errorf("close session log: %v", cerr)
		}
	}()
	ag.SessionLog = sl

	dir := sessionlayout.RecordContainer(sl.Path())
	if _, err := tools.SessionInboxDeliver(dir, sl.SessionID(), sessionMessageNoticeOnceText, ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}

	// THE WAKE PATH: merge the inbox into the queue the way the idle poller does
	// and PEEK the way the REPL's wake arm does — but the turn opens with a line,
	// not with the notice statement (the statement-composed opener is the probe
	// below, and the duplicate is what it exposes). A `t+` clock row is injected
	// on the first round of every agent run, so a clean fixture drains while no
	// run is in flight.
	ag.SessionInboxIdleLoad()
	if !tools.JobNoticePending() {
		t.Fatal("the inbox is empty after the delivery was merged — nothing would open a turn")
	}
	opening := ChatJobNoticeOpenLine(tools.PeekJobNotices())
	if !strings.Contains(opening, sessionMessageNoticeOnceText) {
		t.Fatalf("the turn's opening line does not state the delivery at all:\n%s", opening)
	}
	if _, err := ag.ChatOps(opening, nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}
	if len(bodies) == 0 {
		t.Fatal("the turn issued no request")
	}

	last := bodies[len(bodies)-1]
	if !strings.Contains(last, sessionMessageNoticeOnceText) {
		t.Fatalf("the turn's request does not carry the delivered text at all:\n%s", last)
	}
	if got := strings.Count(last, sessionInboxInjectionMarker); got != 1 {
		t.Errorf("the request states the delivery %d time(s), want exactly 1 — a turn opened WITH the notice must not have the same statement injected a second time from the conversation:\n%s",
			got, last)
	}
}
