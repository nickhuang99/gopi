package agent

// chat_signal_interrupt_record_test.go — THE SIGNAL HALF of the interrupted-turn
// witness (epic: "interrupt received / ended (interrupt)" on screen with ZERO
// turn_interrupted rows in session.jsonl).
//
// THE MEASURED DEFECT (session 07d98407): 1356 JSONL lines carrying only
// human/notify rows, a raw.log parked on one unanswered REQUEST — the operator
// saw the marker, the record saw nothing. The cause is a branch, not a missing
// feature: chatOps' signal arm returned interruptErr WITHOUT calling
// recordTurnInterruption, while the operator arm (TurnInterrupted) did call it.
// So the half that DOES write is the operator's, and the half that wrote nothing
// is the one an external SIGINT produces.
//
// WHY THE FIX IS NOT "ADD ONE CALL". recordTurnInterruption hard-codes
// InterruptSourceOperator, and TurnInterruptReason() answers the honest default
// InterruptReasonCtrlC for a run that was never abandoned through InterruptRun.
// Reusing it unchanged would make every SIGINT read `source: operator,
// reason: ctrl-c` — a record that LIES about who ended the turn, which is worse
// than the silence it replaces: a reader (and the scripts that group by these
// fields) would attribute a supervisor's SIGTERM to the person at the keyboard.
//
// THE SHAPE ASSERTED HERE, all read from the JSONL the child process really
// wrote (Rule 31 — the surface is executed, not the function unit-tested):
//  1. turn_interrupted EXISTS after a real SIGINT;
//  2. its source is `signal`, NOT operator/human;
//  3. its reason NAMES THE SIGNAL (SIGINT), NOT ctrl-c.
//
// WHY A CHILD PROCESS AND A REAL SIGNAL (Rule 42, and the acceptance criterion).
// A delivered SIGINT is the subject; a mock of it would test the mock. The only
// legal target is a process this test STARTS ITSELF, so the harness re-execs
// this test binary as a child (-test.run selecting the helper below), hands the
// child a stub provider that holds the answer mid-stream, waits until the child
// reports the stream is genuinely in flight, and then signals THAT pid. Nothing
// is ever signalled by pattern, by group or by number-we-computed.
//
// WHY A PIPE AND NOT A PTY. The defect is the signal arm of the agent, which is
// reached the same way with or without a terminal: no raw mode, no key decoder,
// no lineedit.CaptureSignals in the picture. A pipe therefore keeps the test
// convergent in a CI run with no TTY, and it keeps this harness from competing
// with any other subscriber of the signal (the lesson behind memory #14160: a
// second consumer of the same signal — and `signal.Reset` in particular — is how
// an in-process handler gets dismantled). The child re-registers SIGINT through
// signal.Notify exactly as the carriers do, so the signal is caught by the code
// path under test rather than killing the child by default disposition.
//
// THE VOCABULARY IS SPELLED AS LITERALS HERE, DELIBERATELY (same rule as
// testsuite/chat_pty_interrupt_test.go). This file must compile against the
// PRE-FIX tree to be a red proof: importing agent.InterruptSourceSignal would
// fail the BUILD there, and a build failure proves nothing about behaviour.
// Spelled as literals, the same file compiles on both trees and FAILS on the old
// one for the right reason — there is no turn_interrupted row at all.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/signalrec"
)

const (
	// signalChildEnv marks the RE-EXEC'd copy of this test binary as the child
	// under test: without it, the helper test skips (so a plain package run does
	// not spawn anything).
	signalChildEnv = "GOPI_AGENT_SIGNAL_INTERRUPT_CHILD"
	// signalChildReadyEnv names the file the child uses to publish "the turn is
	// in flight, signal me now" plus the path of the session JSONL it writes.
	// A file rather than a pipe because BOTH facts must be readable by the
	// parent before it signals, and one small file keeps the readiness handshake
	// and the artifact location in one place.
	signalChildReadyEnv = "GOPI_AGENT_SIGNAL_INTERRUPT_READYFILE"
	// signalChildHomeEnv is the child's GOPI_HOME, OUTSIDE any t.TempDir(): the
	// parent owns this directory and reads the child's session JSONL AFTER the
	// child exits — and a t.TempDir() is removed when the test that made it
	// finishes, which in the child happens before the parent reads a byte (the
	// measured first failure of this harness).
	signalChildHomeEnv = "GOPI_AGENT_SIGNAL_INTERRUPT_HOME"
	// signalChildTestName is the helper test the parent re-execs.
	signalChildTestName = "TestSignalTurnInterruptChildRunsATurn"
	// The feature's OWN vocabulary, as literals (see the file header).
	signalSourceLiteral = "signal"
	signalReasonLiteral = "SIGINT"
	signalEventLiteral  = "turn_interrupted"
	// signalStreamFragments is how many fragments the child's stub streams before
	// the parent is told to signal. Two is enough to prove the answer was really
	// being streamed (a cancel at the boundary would otherwise be
	// indistinguishable from a cancel that reached nothing).
	signalStreamFragments = 2
	// The child's stub timing: a slow answer that is HELD open after its last
	// fragment, so the turn is live whenever the signal lands.
	signalChildStreamGap = 20 * time.Millisecond
	// signalParentReadyWait bounds how long the parent waits for the child to
	// reach "in flight". Generous: a cold child pays for the process start plus
	// the first round trip.
	signalParentReadyWait = 60 * time.Second
	// signalParentExitWait bounds how long the parent waits for the child to
	// finish its close-out after the signal.
	signalParentExitWait = 60 * time.Second
	// signalParentPollStep is the readiness poll interval.
	signalParentPollStep = 10 * time.Millisecond
)

// signalChildTurn is the answer the child's stub streams: several fragments, so
// the turn is still being answered when the signal arrives.
var signalChildTurn = []string{"alpha ", "beta ", "gamma ", "delta "}

// TestSignalTurnInterruptIsRecordedWithTheSignalAsItsSource is the arm.
func TestSignalTurnInterruptIsRecordedWithTheSignalAsItsSource(t *testing.T) {
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready.txt")
	sessionPath := filepath.Join(dir, "session-path.txt")

	// The child is OURS: started here, signalled here, waited for here (Rule 42 —
	// nothing is addressed by pattern or by a group id derived from anything).
	child := exec.Command(os.Args[0], "-test.run", "^"+signalChildTestName+"$", "-test.v")
	child.Env = append(os.Environ(),
		signalChildEnv+"=1",
		signalChildReadyEnv+"="+readyPath,
		"GOPI_AGENT_SIGNAL_INTERRUPT_SESSIONFILE="+sessionPath,
		signalChildHomeEnv+"="+dir,
	)
	if err := child.Start(); err != nil {
		t.Fatalf("start the signal-turn child: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill() //stylegate:ignore teardown of the process THIS test started; the verdict is read from the session JSONL, not from its exit status
		_, _ = child.Process.Wait()
	})

	// WAIT FOR "IN FLIGHT". The child writes the marker only after the stub has
	// streamed enough fragments that the cancellation is landing mid-answer.
	if !waitForFile(t, readyPath, signalParentReadyWait) {
		t.Fatalf("the child never reported its turn in flight (waited %s for %s) — a signal sent to a turn that had not started would prove nothing", signalParentReadyWait, readyPath)
	}

	// THE REAL SIGNAL, delivered to the pid of the process we started.
	if err := child.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("SIGINT the child pid %d: %v", child.Process.Pid, err)
	}

	// The child unwinds the turn, writes its close-out and exits. A child that
	// never returns is killed by the cleanup above and reported here.
	waitDone := make(chan error, 1)
	go func() { waitDone <- child.Wait() }()
	select {
	case <-waitDone:
	case <-time.After(signalParentExitWait):
		t.Fatalf("the child did not exit within %s of the SIGINT — the interrupt path never unwound (pid %d)", signalParentExitWait, child.Process.Pid)
	}

	// THE ARTIFACT: the JSONL the child's run really wrote.
	rawPath, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("the child never published its session log path: %v", err)
	}
	sessionLog := strings.TrimSpace(string(rawPath))
	raw, err := os.ReadFile(sessionLog)
	if err != nil {
		t.Fatalf("read the child's session log %q: %v", sessionLog, err)
	}
	ev := findTurnInterruptEvent(t, string(raw), signalEventLiteral)
	if ev == nil {
		t.Fatalf("a real SIGINT ended the child's turn and the session log carries NO %s witness — this is exactly the defect: the screen says interrupted while the record says nothing happened\n--- session log %s ---\n%s",
			signalEventLiteral, sessionLog, string(raw))
	}
	if source, _ := ev["source"].(string); source != signalSourceLiteral {
		t.Errorf("turn_interrupted source = %q, want %q — a signal that ended the turn is not an operator (a reader, and every script grouping by this field, would attribute a supervisor's signal to the person at the keyboard)",
			source, signalSourceLiteral)
	}
	// The record itself, on the test's own output: a green that only says "found
	// an event" leaves a reader unable to see WHAT was written, and this file is
	// the evidence the epic asks for.
	if encoded, mErr := json.Marshal(ev); mErr == nil {
		t.Logf("child's own turn_interrupted row: %s", encoded)
	}
	reason, _ := ev["reason"].(string)
	if reason == InterruptReasonCtrlC {
		t.Errorf("turn_interrupted reason = %q — the record claims the operator pressed Ctrl-C while no key was pressed; the signal's own name is the honest answer", reason)
	}
	if reason != signalReasonLiteral {
		t.Errorf("turn_interrupted reason = %q, want %q — the reason must NAME the signal that ended the turn (its spelling is signalrec's, the one source the carriers already publish as CloseSignal)", reason, signalReasonLiteral)
	}
}

// waitForFile reports whether path appeared (non-empty) within budget.
func waitForFile(t *testing.T, path string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(signalParentPollStep) //stylegate:allow-sleep bounded readiness poll: the deadline above owns the outcome
	}
}

// TestSignalTurnInterruptChildRunsATurn is the CHILD half: it is not a unit test
// of anything — it is the process the parent signals, running the REAL carrier
// shape (a signal watcher that publishes the signal's name then calls
// Interrupt(), exactly as cmd/agent's chat carrier does) against the REAL
// ChatOps signal branch. It skips unless the parent asked for it.
func TestSignalTurnInterruptChildRunsATurn(t *testing.T) {
	if os.Getenv(signalChildEnv) != "1" {
		t.Skip("helper for TestSignalTurnInterruptIsRecordedWithTheSignalAsItsSource; nothing to do when run directly")
	}
	readyPath := os.Getenv(signalChildReadyEnv)
	sessionPathEnv := os.Getenv("GOPI_AGENT_SIGNAL_INTERRUPT_SESSIONFILE")
	if readyPath == "" || sessionPathEnv == "" {
		t.Fatalf("%s is set but the ready/session file names are empty (%q, %q) — the parent could not observe this child",
			signalChildEnv, readyPath, sessionPathEnv)
	}

	// The child's own profile, in the directory the PARENT made (see
	// signalChildHomeEnv): its session JSONL has to survive this process's exit
	// for the parent to read it. GOPI_HOME is what sessionDirForID resolves
	// through.
	home := os.Getenv(signalChildHomeEnv)
	if home == "" {
		t.Fatalf("%s is empty — the child would write into its own (self-removing) temp dir and the parent could not read the record", signalChildHomeEnv)
	}
	t.Setenv("GOPI_HOME", home)

	var mu sync.Mutex
	var streamed int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
		writeClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		for _, frag := range signalChildTurn {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(signalChildStreamGap):
			}
			fragJSON, _ := json.Marshal(frag) //stylegate:ignore marshalling a string built in-process cannot fail
			writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, fragJSON))
			if flusher != nil {
				flusher.Flush()
			}
			mu.Lock()
			streamed++
			mu.Unlock()
		}
		// HELD OPEN: the answer is complete on the wire but the response is not —
		// the live state a signal must be able to end.
		<-r.Context().Done()
	}))
	defer server.Close()

	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	defer sl.Close()
	if err := os.WriteFile(sessionPathEnv, []byte(sl.Path()), 0o644); err != nil {
		t.Fatalf("publish the session log path: %v", err)
	}

	ag := chatOpsAgent(t, server)
	ag.SessionLog = sl
	// A REAL interrupt root, the shape NewAgent builds in production: without it
	// Interrupt() has nothing to cancel and the signal would reach nothing (the
	// hand-built &Agent{} literal falls back to context.Background()).
	base, cancelRoot := context.WithCancel(context.Background())
	ag.interruptBase, ag.interruptCancel = base, cancelRoot
	defer cancelRoot()

	// THE CARRIER'S OWN WATCHER, reproduced: subscribe, and on the decoded signal
	// publish its name (signalrec's spelling, the one definition) BEFORE calling
	// Interrupt(), which is the order cmd/agent/cli_chat.go uses and the reason
	// the close-out can state the cause instead of guessing it.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)
	go func() {
		if sig, ok := <-sigCh; ok {
			ag.CloseSignal = signalrec.SignalName(sig)
			ag.Interrupt()
		}
	}()

	done := make(chan error, 1)
	go func() {
		_, err := ag.ChatOps("a question the operator gives up on", nil)
		done <- err
	}()

	// READY only once the stream is genuinely live (see the parent's wait): the
	// signal has to land MID-ANSWER for the branch under test to be the one that
	// answers it.
	deadline := time.Now().Add(signalParentReadyWait)
	for {
		mu.Lock()
		n := streamed
		mu.Unlock()
		if n >= signalStreamFragments {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stub never streamed %d fragments — the turn was not in flight to interrupt", signalStreamFragments)
		}
		time.Sleep(signalChildStreamGap) //stylegate:allow-sleep polling the stub's own fragment counter, the only observable for "the answer is in flight"
	}
	if err := os.WriteFile(readyPath, []byte("in flight\n"), 0o644); err != nil {
		t.Fatalf("publish the readiness marker: %v", err)
	}

	select {
	case err := <-done:
		// The child's job is to END the turn through the signal path; whether the
		// carrier worded it as an interrupt is asserted by the parent from the
		// record, so this only keeps a silent hang from looking like a pass.
		t.Logf("child: ChatOps returned after the signal: %v", err)
	case <-time.After(signalParentExitWait):
		t.Fatalf("ChatOps never returned after the signal — the interrupt did not reach the in-flight turn")
	}
	// Flush the witness before the process goes away (the run already Syncs; this
	// is the child's own belt-and-braces so the parent reads a complete file).
	if err := sl.Sync(); err != nil {
		t.Fatalf("flush the session log: %v", err)
	}
}
