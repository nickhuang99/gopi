package main

// cli_notify_deliver_idle_test.go — the IDLE-WAKE face of
// `gopi notify --session <id> --message <text>`: the delivered message becomes
// the NEXT TURN of a console that is parked at its prompt with NOBODY TYPING.
//
// WHAT ONLY THIS FILE CAN SHOW. The seam's own cases (internal/agent) prove the
// merge and the edge; the sibling case (cli_notify_deliver_test.go) proves a
// delivery rides the next round boundary of a turn that already existed. Neither
// can show what this feature IS: two real processes, one of them a console
// sitting idle with an open stdin that carries NO line at all, and the model
// receiving the delivered text anyway.
//
// THE SUBJECT IS THE WIRE, never the screen. The console's stdout is a
// human-facing surface; the fact under test is "a turn was opened for this
// message, with no input from a person", and the model's request body is the one
// place that fact is unambiguous — a turn can be opened without a request, but
// the delivered text cannot reach a request that no turn produced.
//
// THE REFUSAL HALF lives in the sibling file (each missing flag, and an unknown
// session, asserted as refusals); it is not repeated here.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

const (
	// notifyIdleText is the delivered message: distinctive, so "the model read
	// it" cannot be satisfied by the console's own startup content.
	notifyIdleText = "NOTIFY-IDLE-CLI-5d07: delivered while nobody was typing"
	// notifyIdleQuit is the ONLY line ever written on the console's stdin, and
	// it is written AFTER the delivery has been read — so no line of the
	// operator's own can be the thing that carried the message.
	notifyIdleQuit = "/quit"
	// notifyIdleWait bounds the poll for the delivered turn. The console's idle
	// poll runs every agent.sessionInboxIdleLoadInterval (2s), and this deadline
	// is a small multiple of it — a wait, never a load (Rule 27).
	notifyIdleWait = 30 * time.Second
	// notifyIdleStep is that poll's cadence.
	notifyIdleStep = 50 * time.Millisecond
	// notifyIdleTurnNotice is the operator-facing sentence the console prints
	// when the LEVEL mints a turn for a delivery (chatSessionMessageTurnNotice).
	// Matched by its distinguishing clause so a reworded prefix cannot make this
	// case silently stop checking the announcement.
	notifyIdleTurnNotice = "opening a turn to deliver it"
)

// notifyIdleMockAnswer is the scripted reply. It is LONGER than the chat
// turn's minimum final-answer length (agent.minFinalAnswerChars), so ONE turn
// costs exactly ONE request — which is what makes the "not a spin" reading
// below a count of TURNS rather than of rounds.
const notifyIdleMockAnswer = "Noted: an external process delivered a message into this session, and this console opened a turn to report it."

// newMockLLMForNotifyIdle starts the scripted model the idle case talks to.
func newMockLLMForNotifyIdle(t *testing.T) *testutil.MockLLM {
	t.Helper()
	return testutil.NewMockLLM(t, testutil.Resp{Text: notifyIdleMockAnswer})
}

// TestNotifyDeliverWakesAnIdleChatSessionWithNoInput is the acceptance: a live
// console at an idle prompt, a SEPARATE `gopi notify --session … --message …`
// process, and NO line on the console's stdin — the model still receives the
// message, and the session record shows it.
func TestNotifyDeliverWakesAnIdleChatSessionWithNoInput(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	mock := newMockLLMForNotifyIdle(t)
	defer mock.Close()

	bin := testBinPath(t)
	env := mockEnvForNotifyDeliver(t, home, mock.URL())
	cmd := exec.Command(bin, CliVerbChat, chatFlagTools, chatFlagNoEditor)
	cmd.Env = env
	cmd.Dir = work
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	var stdout, stderr syncBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start chat: %v", err)
	}
	done := armNotifyChildExit(cmd)
	defer func() {
		_ = stdin.Close() //stylegate:ignore closes the notify child's stdin so it sees EOF; the delivery record is the assertion
		if !done.waitFor(notifyTestTimeout) {
			_ = cmd.Process.Kill() //stylegate:ignore kills the idle notify child on the fixture's exit path after its output was captured
		}
	}()

	// THE SESSION, AS THE RUNNING RUN SEES IT: poll until the console has opened
	// its own record — which is also the directory the delivery resolves to.
	//
	// WHAT THE RECORD'S EXISTENCE DOES AND DOES NOT PROVE. It proves the run has
	// a session directory, which is what the delivery needs. It does NOT prove
	// the console has reached its idle prompt, and the case does not need it to:
	// the idle poller is started before the REPL's first read and its FIRST act
	// is a load, so a delivery that lands in the window between the two is merged
	// by that first load rather than lost. Either ordering produces the same
	// fact, which is the property the feature claims (durable + level-driven).
	sessionsRoot := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	record := ""
	deadline := time.Now().Add(notifyIdleWait)
	for time.Now().Before(deadline) {
		record = sessionRecordUnder(t, sessionsRoot)
		if record != "" {
			break
		}
		time.Sleep(notifyIdleStep) //stylegate:allow-sleep bounded poll for the console to open its own session record, deadline notifyIdleWait
	}
	if record == "" {
		t.Fatalf("the live chat opened no session record under %s within %s\nstderr:\n%s", sessionsRoot, notifyIdleWait, stderr.String())
	}
	sessionDir := sessionlayout.RecordContainer(record)
	sessionID := filepath.Base(sessionDir)

	// NO INPUT FROM ANYBODY: nothing is written on stdin, before or after the
	// delivery. Whatever the console does next, it does without a keystroke.
	out, errOut, err := runNotifyDeliverArgv(t, env, notifySessionFlag, sessionID, notifyMessageFlag, notifyIdleText)
	if err != nil {
		t.Fatalf("gopi notify %s %s %s %q = %v\nstdout: %s\nstderr: %s",
			notifySessionFlag, sessionID, notifyMessageFlag, notifyIdleText, err, out, errOut)
	}
	if !strings.Contains(out, "queued:") {
		t.Fatalf("the delivery does not report that it queued anything:\n%s", out)
	}

	// THE TURN, MINTED BY THE CONSOLE ITSELF: the model is handed a request
	// carrying the delivered text with no line on stdin having produced it.
	//
	// WHY MATCHING THE BODY IS THE RIGHT SUBJECT even though the console serves
	// an opening request of its own: that request's body cannot contain this
	// text unless a turn carried it (the text exists nowhere else in the profile
	// — it is not in the system prompt, not in the project context, and the case
	// wrote no line at all). Whether the delivery is merged by the console's
	// first load or by a later poll, the request that carries it is a turn opened
	// WITH NO INPUT, which is the claim under test.
	if !waitForDescribe(t, mock, notifyIdleText) {
		t.Fatalf("no request carried the delivered message within %s — the console did not wake for it\nstdout:\n%s\nstderr:\n%s",
			notifyIdleWait, stdout.String(), stderr.String())
	}
	// THE ANNOUNCEMENT: a turn nobody typed must be accountable to the operator.
	if screen := stdout.String() + stderr.String(); !strings.Contains(screen, notifyIdleTurnNotice) {
		t.Errorf("the console opened a turn for the delivery without telling the operator (%q):\nstdout:\n%s\nstderr:\n%s",
			notifyIdleTurnNotice, stdout.String(), stderr.String())
	}

	// ONE TURN, NOT A SPIN: the notice was drained by the round boundary, so the
	// LEVEL is false and no further turn may be minted for the same message.
	// This is a RUNAWAY check, and it is a check-at-a-moment rather than a
	// guarantee about a span of time — deliberately: a session that repeats an
	// instruction forever is the failure, and re-reading each tick is the only
	// way to catch it, so the observation is taken right after the turn and
	// WOULD catch a session whose self-driven cadence re-minted within one idle
	// poll (a poll interval is 2s, and an ordinary turn takes longer than the
	// two request bodies needed for one extra mint). It is not a load: nothing
	// is fed to make it move, the delivery is already there.
	if len(mock.RequestsContaining(notifyIdleText)) != 1 {
		t.Errorf("the delivered message appears in %d request(s), want exactly 1 — a re-minted notice would hand the model the same instruction twice",
			len(mock.RequestsContaining(notifyIdleText)))
	}

	if _, err := stdin.Write([]byte(notifyIdleQuit + "\n")); err != nil {
		t.Fatalf("write %s: %v", notifyIdleQuit, err)
	}
	if err := stdin.Close(); err != nil {
		t.Errorf("close stdin: %v", err)
	}
	if !done.waitFor(notifyTestTimeout) {
		t.Fatalf("the console did not exit after %s\nstderr:\n%s", notifyIdleQuit, stderr.String())
	}

	// THE RECORD: the delivered sentence is a user turn in the session JSONL —
	// the durable half, which is what a later reader (and a resumed session)
	// sees. The LINE NUMBER is cited because the report quotes it.
	sess := loadSessionForNotifyDeliver(t, home)
	found := false
	// HarnessUserContents, NOT UserContents: the delivered sentence is recorded
	// as a row the HARNESS injected (internal/agent/chat_job_notice_turn.go
	// sessionMessageFact prefixes it), and UserContents returns only the
	// operator's own turns — reading it here would assert against a slice the
	// delivery can never enter, so the check would be vacuously red.
	for _, c := range sess.HarnessUserContents() {
		if strings.Contains(c, notifyIdleText) {
			found = true
		}
	}
	if !found {
		t.Errorf("the session record carries no user turn with the delivered message — the model was told, the record does not show it:\n%v", sess.HarnessUserContents())
	}
	line, ok := notifyIdleRecordLine(record, notifyIdleText)
	if !ok {
		t.Errorf("no line of %s carries the delivered text", record)
	} else {
		t.Logf("delivered turn recorded at %s:%d", record, line)
	}

	// THE INBOX, read back: the delivery was taken (the marker exists) — the
	// console is not sitting on an undrained file.
	raw, err := os.ReadFile(tools.SessionInboxPath(sessionDir))
	if err != nil {
		t.Fatalf("read the inbox back: %v", err)
	}
	if !strings.Contains(string(raw), notifyIdleText) {
		t.Errorf("the inbox at %s does not carry the delivered text:\n%s", tools.SessionInboxPath(sessionDir), raw)
	}
}

// notifyIdleRecordLine returns the 1-based line number of the session record
// carrying want. It is the report's citation, so it reads the FILE the way the
// report's reader will.
func notifyIdleRecordLine(recordPath, want string) (int, bool) {
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		return 0, false
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, want) {
			return i + 1, true
		}
	}
	return 0, false
}
