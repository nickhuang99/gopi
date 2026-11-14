// Copyright (c) 2026 The GoPi Authors. All rights reserved.

package testsuite

import (
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The feature's own vocabulary, spelled as LITERALS, for the same reason the
// Ctrl+C and Escape harnesses spell theirs that way: this file must also compile
// against the pre-fix tree (where agent.InterruptReasonEOF does not exist), so
// that its failure there is a BEHAVIOURAL one — the process stays alive and the
// JSONL has no witness — rather than a build error, which would prove nothing.
// The values themselves are pinned where they are defined (internal/agent,
// internal/sessionevent) and asserted against these literals by cmd/agent's
// origin→reason test.
const (
	ctrlDMidTurnQuestion   = "please answer, and take your time about it"
	ctrlDReasonLiteral     = "ctrl-d"
	ctrlDSourceLiteral     = "operator"
	ctrlDEventLiteral      = "turn_interrupted"
	ctrlDEndReasonLiteral  = "eof"
	ctrlDEndKindLiteral    = "key"
	ctrlDTurnHeldPollGap   = 20 * time.Millisecond
	ctrlDSessionSettleTime = 5 * time.Second
)

// TestChatPTY_CtrlDDuringATurnExitsTheSession is the acceptance test for the
// defect measured on a real terminal (task 01a1650f): with a turn IN FLIGHT,
// Ctrl+D — the banner's own "or /quit to exit" key — was read as end-of-input,
// QUEUED, and the reader goroutine then retired, so from that instant no
// goroutine was reading the terminal at all. The turn was still blocked inside
// the model round trip, so the queued marker was never consumed either, and the
// Ctrl+C the operator pressed thirty seconds later reached nobody. The measured
// symptom was therefore not "a slow exit" but a console that had gone DEAF.
//
// The turn is made deterministic by HOLDING the first request open
// (testutil.Resp{Hold: true}) — the same technique chat_queue_pty_test.go and
// chat_pty_escape_interrupt_test.go use, and for the same reason: "the key was
// pressed while a turn was really running" needs a turn that is really running.
//
// This test asserts the BEHAVIOUR the operator needs, end to end, and it fails
// on the pre-fix tree at exactly that point: `chat.close` reports that the child
// did not exit, and the transcript carries a turn_interrupted record with the
// wrong (or no) reason.
func TestChatPTY_CtrlDDuringATurnExitsTheSession(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Hold: true})
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat"}, testutil.MockEnv(t, home, llm.URL()), work)
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	// A turn goes in flight and STAYS there: the mock accepted the request and
	// will not finish it until released (and it never is, on purpose).
	chat.writeGapped(t, ctrlDMidTurnQuestion)
	chat.write(t, "\r")
	ctrlDWaitForHeld(t, llm)

	// THE KEY, pressed mid-turn — on an EMPTY line, which is what makes it the
	// exit key rather than a forward delete.
	chat.write(t, "\x04")

	// 1: THE PROCESS EXITS. This is the assertion the defect fails: before the
	// fix the child was still alive here and `close` gave up after its own bound,
	// which is precisely the hang the operator had to escape with Ctrl+C.
	code := chat.close(t)
	if code != 0 {
		t.Errorf("chat exit = %d, want 0 (Ctrl+D mid-turn is a clean exit)", code)
	}

	// 2: THE TRANSCRIPT SAYS WHICH KEY DID IT, and that a turn was abandoned.
	time.Sleep(ctrlDSessionSettleTime) //stylegate:allow-sleep settles the session file's final write before the transcript assertions
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)

	ev := sess.First(ctrlDEventLiteral)
	if ev == nil {
		t.Fatalf("the session JSONL has no %q event — the abandoned turn is recorded as nothing at all; types: %v",
			ctrlDEventLiteral, sess.Types())
	}
	if got, _ := ev.Raw["reason"].(string); got != ctrlDReasonLiteral {
		t.Errorf("turn_interrupted reason = %q, want %q — recording Ctrl+D as another key's word hides what the operator did",
			got, ctrlDReasonLiteral)
	}
	if got, _ := ev.Raw["source"].(string); got != ctrlDSourceLiteral {
		t.Errorf("turn_interrupted source = %q, want %q", got, ctrlDSourceLiteral)
	}

	// 3: THE SESSION ENDED FOR THE REASON THE KEY MEANS — end of input — and not
	// because something was killed or because an interrupt was mistaken for an
	// exit cause.
	if n := sess.Counts()["session_end"]; n != 1 {
		t.Fatalf("session_end count = %d, want exactly 1 (types: %v)", n, sess.Types())
	}
	end := sess.First("session_end")
	if end == nil {
		t.Fatal("no session_end record to read")
	}
	if got, _ := end.Raw["reason"].(string); got != ctrlDEndReasonLiteral {
		t.Errorf("session_end reason = %q, want %q", got, ctrlDEndReasonLiteral)
	}
	if got, _ := end.Raw["eof_kind"].(string); got != ctrlDEndKindLiteral {
		t.Errorf("session_end eof_kind = %q, want %q", got, ctrlDEndKindLiteral)
	}
	if got := sess.SessionEndExitCode(); got != 0 {
		t.Errorf("session_end exit_code = %d, want 0", got)
	}

	// 4: THE TURN'S QUESTION IS THE SESSION'S, and the model really was asked —
	// the exit cancelled a LIVE turn, it did not skip one.
	if users := strings.Join(sess.UserContents(), "|"); !strings.Contains(users, ctrlDMidTurnQuestion) {
		t.Errorf("the session's user messages = %q, want the mid-turn question present — the exit must not discard the turn's own record", users)
	}
	if n := llm.RequestCount(); n != 1 {
		t.Errorf("the mock served %d request(s), want 1 — the exit interrupted a turn that had really reached the model", n)
	}
}

// ctrlDWaitForHeld blocks until the mock has a request GENUINELY held open, so
// the key below is pressed against a live turn rather than against a request
// that has not arrived yet. It is a readiness wait with a real deadline: the
// moment the condition is observable the wait ends.
func ctrlDWaitForHeld(t *testing.T, llm *testutil.MockLLM) {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	for time.Now().Before(deadline) {
		if llm.RequestCount() >= 1 && llm.HeldCount() >= 1 {
			return
		}
		time.Sleep(ctrlDTurnHeldPollGap) //stylegate:allow-sleep polls the mock's own recorded state; the wait ends the moment it is observable
	}
	t.Fatalf("the mock never held a request open (requests=%d held=%d): this case needs a turn really in flight",
		llm.RequestCount(), llm.HeldCount())
}
