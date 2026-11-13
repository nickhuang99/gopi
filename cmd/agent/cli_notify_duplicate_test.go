package main

// cli_notify_duplicate_test.go — the DUPLICATE-STATEMENT acceptance of
// `gopi notify --session <id> --message <text>` delivered into an IDLE console.
//
// THE DEFECT. A delivered message was stated TWICE on the chat path: once as the
// opening message of the turn the idle wake mints (`agent.ChatJobNoticeOpenLine`
// → `sessionMessageFact`), and once more by that same turn's round boundary,
// which injected the notice from the queue it had just merged. The operator
// reads one notification as two identical messages, and the session record
// carries two user rows with the same sentence.
//
// WHY ONLY A REAL TWO-PROCESS CASE CAN SEE IT. The duplicate needs the WAKE arm
// (a console parked with nothing on its stdin) AND the round boundary of the
// turn that arm mints — the whole point is that those two steps are one moment
// apart and the second must not restate the first. A seam-level case can drive
// either half alone; neither can produce the join.
//
// THE SUBJECT IS THE SESSION RECORD, and it is the operator's own evidence: the
// user rows a chat writes are literally the messages the operator reads. The
// count is of ROWS CARRYING THE DELIVERED TEXT — one delivery, one row. The wire
// count is asserted too, because a row that was written and never sent would be
// a record of a turn nobody had.
//
// The case is the twin of TestNotifyDeliverWakesAnIdleChatSessionWithNoInput
// (same fixture, same delivery verb); it exists separately because that one
// asserts the feature ARRIVES and this one asserts it arrives ONCE.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// notifyDupText is the delivered message: distinctive, so a count of it
	// cannot be satisfied by the console's own startup content.
	notifyDupText = "NOTIFY-DUP-9f31: one delivery, one message"
	// notifyDupQuit is the ONLY line ever written on the console's stdin, and it
	// is written AFTER the delivered turn has been read.
	notifyDupQuit = "/quit"
	// notifyDupWait bounds the poll for the delivered turn. The idle poller runs
	// every agent.sessionInboxIdleLoadInterval (2s) and this is a small multiple
	// of it — a wait, never a load (Rule 27).
	notifyDupWait = 30 * time.Second
	// notifyDupStep is the poll cadence used while waiting on a file.
	notifyDupStep = 50 * time.Millisecond
	// notifyDupTurnNotice is the operator-facing sentence the console prints when
	// it opens a turn for a delivery. Matched by its distinguishing clause, so a
	// reworded prefix cannot make this case silently stop checking it.
	notifyDupTurnNotice = "opening a turn to deliver it"
)

// notifyDupMockAnswer is longer than the chat turn's minimum final-answer length
// (agent.minFinalAnswerChars), so ONE turn costs exactly ONE request — which is
// what makes the wire count below a count of DELIVERIES rather than of rounds.
const notifyDupMockAnswer = "Noted: an external process delivered a message into this session, and this console opened a turn to report it."

// TestNotifyDeliverStatesTheMessageOnceInTheSession is the duplicate's
// regression guard, driven end to end: a real console, a real
// `gopi notify --session …`, and no line on the console's stdin.
func TestNotifyDeliverStatesTheMessageOnceInTheSession(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: notifyDupMockAnswer})
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
		_ = stdin.Close() //stylegate:ignore closes the duplicate-delivery child's stdin so it proceeds to the second notify
		if !done.waitFor(notifyTestTimeout) {
			_ = cmd.Process.Kill() //stylegate:ignore kills the duplicate-case child after the single-delivery assertion
		}
	}()

	// THE SESSION, AS THE RUNNING RUN SEES IT: poll until the console has opened
	// its own record — the directory the delivery resolves to.
	sessionsRoot := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	record := ""
	deadline := time.Now().Add(notifyDupWait)
	for time.Now().Before(deadline) {
		record = sessionRecordUnder(t, sessionsRoot)
		if record != "" {
			break
		}
		time.Sleep(notifyDupStep) //stylegate:allow-sleep bounded poll for the console to open its own session record, deadline notifyDupWait
	}
	if record == "" {
		t.Fatalf("the live chat opened no session record under %s within %s\nstderr:\n%s", sessionsRoot, notifyDupWait, stderr.String())
	}
	sessionID := filepath.Base(sessionlayout.RecordContainer(record))

	// NO INPUT FROM ANYBODY: nothing is written on stdin, before or after.
	out, errOut, err := runNotifyDeliverArgv(t, env, notifySessionFlag, sessionID, notifyMessageFlag, notifyDupText)
	if err != nil {
		t.Fatalf("gopi notify %s %s %s %q = %v\nstdout: %s\nstderr: %s",
			notifySessionFlag, sessionID, notifyMessageFlag, notifyDupText, err, out, errOut)
	}
	if !strings.Contains(out, "queued:") {
		t.Fatalf("the delivery does not report that it queued anything:\n%s", out)
	}

	// THE TURN, MINTED BY THE CONSOLE ITSELF.
	if !waitForDescribe(t, mock, notifyDupText) {
		t.Fatalf("no request carried the delivered message within %s — the console did not wake for it\nstdout:\n%s\nstderr:\n%s",
			notifyDupWait, stdout.String(), stderr.String())
	}
	if screen := stdout.String() + stderr.String(); !strings.Contains(screen, notifyDupTurnNotice) {
		t.Errorf("the console opened a turn for the delivery without telling the operator (%q):\nstdout:\n%s\nstderr:\n%s",
			notifyDupTurnNotice, stdout.String(), stderr.String())
	}

	// THE WIRE: ONE delivery is ONE request carrying the text. A second request
	// would mean the notice was stated twice to the model — the duplicate.
	if got := len(mock.RequestsContaining(notifyDupText)); got != 1 {
		t.Errorf("the delivered message reaches %d model request(s), want exactly 1 — a second statement is the duplicate (the wake arm already put the message in the turn's opening line, so the round boundary must not inject it again)",
			got)
	}

	if _, err := stdin.Write([]byte(notifyDupQuit + "\n")); err != nil {
		t.Fatalf("write %s: %v", notifyDupQuit, err)
	}
	if err := stdin.Close(); err != nil {
		t.Errorf("close stdin: %v", err)
	}
	if !done.waitFor(notifyTestTimeout) {
		t.Fatalf("the console did not exit after %s\nstderr:\n%s", notifyDupQuit, stderr.String())
	}

	// THE RECORD, and this is the operator's own evidence: the session JSONL
	// holds ONE user row carrying the delivered text, not two.
	sess := loadSessionForNotifyDeliver(t, home)
	var carrying []string
	for _, c := range sess.HarnessUserContents() {
		if strings.Contains(c, notifyDupText) {
			carrying = append(carrying, c)
		}
	}
	if len(carrying) != 1 {
		t.Errorf("the session record carries %d user message(s) with the delivered text, want exactly 1 — the operator would read this delivery %d time(s):\n%q",
			len(carrying), len(carrying), carrying)
	}
}
