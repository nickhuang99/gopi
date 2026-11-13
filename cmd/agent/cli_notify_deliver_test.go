package main

// cli_notify_deliver_test.go — the DELIVER face of `gopi notify`, on the REAL
// binary and across TWO processes (Rule 31): a live `gopi chat` is running, a
// SEPARATE `gopi notify --session … --message …` process hands it a message, and
// the console's own session record shows the message reaching the model.
//
// WHAT ONLY THIS FILE CAN SHOW. The seam's own cases (internal/tools,
// internal/agent) prove the file, the queue and the injection. They cannot show
// that the CLI verb resolves a real session under a real profile, writes into
// the directory a RUNNING session is writing into, and is seen by that run's
// next round boundary — the whole path the operator takes.
//
// THE REFUSALS ARE HALF THE DELIVERABLE, and they are asserted as refusals: no
// --message, no --session, and an unknown session each exit non-zero with the
// flag named. A silent default (an empty text, "the last session") would be a
// message the sender believes was received.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

const (
	// notifyDeliverText is the delivered message. Distinctive so that "the
	// model read it" cannot be satisfied by the turn's own input.
	notifyDeliverText = "NOTIFY-CLI-9f21: an external process sent this"
	// notifyDeliverTurn is the line written on the live console's stdin, so a
	// turn exists at all (the delivery rides the SAME round's boundary).
	notifyDeliverTurn = "please answer briefly"
	// notifyDeliverWait bounds the poll for the live session's record (the run
	// must have opened its log before it can be written into).
	notifyDeliverWait = 30 * time.Second
	// notifyDeliverStep is that poll's cadence.
	notifyDeliverStep = 50 * time.Millisecond
)

// syncBuffer is a mutex-guarded buffer: the chat process writes on its own
// goroutine while the case reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// runNotifyDeliverArgv runs `gopi notify …` with a CALLER-SUPPLIED environment:
// the deliver face resolves its session under the profile the caller names, so
// this case must point it at the live chat's own home (runNotifyArgv's fixed
// temp home is deliberately not used here).
func runNotifyDeliverArgv(t *testing.T, env []string, args ...string) (string, string, error) {
	t.Helper()
	bin := testBinPath(t)
	argv := append([]string{CliVerbNotify}, args...)
	cmd := exec.Command(bin, familyAddressArgs(argv)...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return stdout.String(), stderr.String(), err
	case <-time.After(notifyTestTimeout):
		_ = cmd.Process.Kill() //stylegate:ignore kills the notify child after its printed delivery line was read
		t.Fatalf("gopi notify %v did not return within %s", args, notifyTestTimeout)
		return "", "", nil
	}
}

// testBinPath resolves the binary under test through the shared resolver (the
// same precedence every binary-level case in this package uses).
func testBinPath(t *testing.T) string {
	t.Helper()
	return testutil.GopiBin(t)
}

// newMockLLMForNotifyDeliver starts the scripted model the live console talks
// to. Every round answers with the same short text (the mock repeats its last
// script entry), so the case does not depend on the number of rounds the
// console runs.
func newMockLLMForNotifyDeliver(t *testing.T) *testutil.MockLLM {
	t.Helper()
	return testutil.NewMockLLM(t, testutil.Resp{Text: "noted."})
}

// mockEnvForNotifyDeliver is the sandboxed environment for the live console:
// the shared MockEnv script (isolated profile, no ambient GOPI_*, a pinned
// ground root) — the same one every mocked run in this package uses.
func mockEnvForNotifyDeliver(t *testing.T, gopiHome, mockURL string) []string {
	t.Helper()
	return testutil.MockEnv(t, gopiHome, mockURL)
}

// sessionRecordUnder returns the session record file under a sessions root, or
// "" while the run has not opened one yet. The readiness probe is the shared
// one (testutil.SessionRecordPath), so this case's idea of "the session exists"
// cannot drift from LoadSession's.
func sessionRecordUnder(t *testing.T, sessionsRoot string) string {
	t.Helper()
	return testutil.SessionRecordPath(sessionsRoot)
}

// waitForDescribe polls the mock's OWN record of the requests it served for one
// carrying want. The subject is the request body (what the model was actually
// given), never an output the console printed.
func waitForDescribe(t *testing.T, mock *testutil.MockLLM, want string) bool {
	t.Helper()
	deadline := time.Now().Add(notifyDeliverWait)
	for time.Now().Before(deadline) {
		if len(mock.RequestsContaining(want)) > 0 {
			return true
		}
		time.Sleep(notifyDeliverStep) //stylegate:allow-sleep bounded poll interval for the console's round to reach the mock, with notifyDeliverWait owning the deadline
	}
	return false
}

// loadSessionForNotifyDeliver parses the console's session record through the
// shared session-fixture reader.
func loadSessionForNotifyDeliver(t *testing.T, gopiHome string) *testutil.Session {
	t.Helper()
	return testutil.LoadSession(t, gopiHome)
}

// TestNotifyDeliverRefusesEachMissingHalfAndAnUnknownSession pins the three
// boundaries: each exits non-zero, names the flag it needs, and never reports a
// delivery it did not make.
func TestNotifyDeliverRefusesEachMissingHalfAndAnUnknownSession(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no --message", []string{notifySessionFlag, "some-session"}, notifyMessageFlag},
		{"no --session", []string{notifyMessageFlag, "hello"}, notifySessionFlag},
		{"unknown session", []string{notifySessionFlag, "definitely-not-a-session-id", notifyMessageFlag, "hello"}, "unknown session"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each case runs against its OWN profile: "no sessions directory
			// yet" and "a directory that holds no such session" are two
			// different states of the same refusal, and a shared home would
			// blur them.
			caseHome := t.TempDir()
			caseSettings := filepath.Join(caseHome, "settings.json")
			if err := os.WriteFile(caseSettings, []byte("{}"), 0o600); err != nil {
				t.Fatalf("write settings: %v", err)
			}
			caseEnv := append(notifyScrubbedEnv(),
				"GOPI_HOME="+caseHome,
				"GOPI_SETTINGS="+caseSettings,
			)
			// The "unknown session" case needs a sessions root that EXISTS and
			// holds no such session: without it the answer would be the
			// trivially-true "nothing has run here", which is a different
			// refusal (and is covered by the "neither" case).
			if tc.name == "unknown session" {
				if err := os.MkdirAll(filepath.Join(caseHome, config.ProfileDirName, config.SessionsDirName), 0o755); err != nil {
					t.Fatalf("create sessions root: %v", err)
				}
			}
			stdout, stderr, err := runNotifyDeliverArgv(t, caseEnv, tc.args...)
			if err == nil {
				t.Fatalf("accepted (stdout %q) — a delivery that did not happen must not report success", stdout)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("the refusal does not name %q:\nstderr: %s", tc.want, stderr)
			}
			if strings.Contains(stdout, "queued:") {
				t.Errorf("a refused delivery printed a success line:\n%s", stdout)
			}
			// NOTHING WAS WRITTEN: a refusal must not create an inbox — not
			// under the session dir it could not resolve, and not anywhere else
			// in this profile.
			if found := findInboxUnder(caseHome); found != "" {
				t.Errorf("a refused delivery created an inbox at %s", found)
			}
		})
	}
}

// findInboxUnder returns the first inbox file under root, or "" — the "nothing
// was written" probe. The name comes from the production path (tools), so a
// refusal that started writing a DIFFERENTLY-named file would still be found by
// a reader of the real layout.
func findInboxUnder(root string) string {
	found := ""
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil //stylegate:ignore best-effort walk of a test-created tree; a vanished entry is not this probe's subject
		}
		if info.Name() == inboxFileNameForProbe {
			found = path
		}
		return nil
	})
	return found
}

// inboxFileNameForProbe is the inbox's file name, taken from the production
// path so the probe cannot drift from what the writer actually creates.
var inboxFileNameForProbe = filepath.Base(tools.SessionInboxPath("/probe"))

// TestNotifyDeliverReachesARunningChatSession is the end-to-end acceptance: the
// live console's next round boundary carries the message, and its session
// record proves it happened.
func TestNotifyDeliverReachesARunningChatSession(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	mock := newMockLLMForNotifyDeliver(t)
	defer mock.Close()

	bin := testBinPath(t)
	env := mockEnvForNotifyDeliver(t, home, mock.URL())
	cmd := exec.Command(bin, CliVerbChat, chatFlagTools)
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
		_ = stdin.Close() //stylegate:ignore closes stdin of the second notify case so the child can finish its delivery
		if !done.waitFor(notifyTestTimeout) {
			_ = cmd.Process.Kill() //stylegate:ignore reaps that child once its delivery line has been asserted
		}
	}()

	// THE SESSION, AS THE RUNNING RUN SEES IT: poll until the console has opened
	// its own record, which is also the directory the delivery resolves to.
	sessionsRoot := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	record := ""
	deadline := time.Now().Add(notifyDeliverWait)
	for time.Now().Before(deadline) {
		record = sessionRecordUnder(t, sessionsRoot)
		if record != "" {
			break
		}
		time.Sleep(notifyDeliverStep) //stylegate:allow-sleep bounded poll for the console to open its own session record, deadline notifyDeliverWait
	}
	if record == "" {
		t.Fatalf("the live chat opened no session record under %s within %s\nstderr:\n%s", sessionsRoot, notifyDeliverWait, stderr.String())
	}
	sessionDir := sessionlayout.RecordContainer(record)

	// THE SEPARATE PROCESS: a real `gopi notify --session <id> --message <text>`,
	// addressed by the session's own id (the directory name is the id for a bare
	// session), against the live run's profile.
	sessionID := filepath.Base(sessionDir)
	out, errOut, err := runNotifyDeliverArgv(t, env, notifySessionFlag, sessionID, notifyMessageFlag, notifyDeliverText)
	if err != nil {
		t.Fatalf("gopi notify %s %s %s %q = %v\nstdout: %s\nstderr: %s",
			notifySessionFlag, sessionID, notifyMessageFlag, notifyDeliverText, err, out, errOut)
	}
	if !strings.Contains(out, "queued:") {
		t.Errorf("the delivery does not report that it queued anything:\n%s", out)
	}
	if !strings.Contains(out, tools.SessionInboxPath(sessionDir)) {
		t.Errorf("the delivery does not name the inbox it wrote (%s):\n%s", tools.SessionInboxPath(sessionDir), out)
	}

	// THE TURN: one line on the live console, whose round boundary then loads
	// that inbox before the request is built.
	if _, err := stdin.Write([]byte(notifyDeliverTurn + "\n")); err != nil {
		t.Fatalf("write the console's line: %v", err)
	}
	if !waitForDescribe(t, mock, notifyDeliverText) {
		t.Fatalf("the model's request never carried the delivered message within %s\nchat stderr:\n%s",
			notifyDeliverWait, stderr.String())
	}
	if _, err := stdin.Write([]byte("/quit\n")); err != nil {
		t.Fatalf("write /quit: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Errorf("close stdin: %v", err)
	}
	if !done.waitFor(notifyTestTimeout) {
		t.Fatalf("the console did not exit after /quit\nstderr:\n%s", stderr.String())
	}

	// THE RECORD: the session JSONL holds the delivered sentence as a user turn
	// — the durable half, which is what a later reader (and a resumed session)
	// sees. It is read with the SAME accounting every feature's acceptance uses.
	sess := loadSessionForNotifyDeliver(t, home)
	found := false
	// HarnessUserContents, NOT UserContents: the delivered sentence is recorded
	// as a row the HARNESS injected (internal/agent/chat_job_notice_turn.go
	// sessionMessageFact prefixes it), and UserContents returns only the
	// operator's own turns — reading it here would assert against a slice the
	// delivery can never enter, so the check would be vacuously red.
	for _, c := range sess.HarnessUserContents() {
		if strings.Contains(c, notifyDeliverText) {
			found = true
		}
	}
	if !found {
		t.Errorf("the session record carries no user turn with the delivered message — the model was told, the record does not show it:\n%v", sess.HarnessUserContents())
	}
}
