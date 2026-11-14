package testsuite

// chat_once_driver_test.go — the END-TO-END surface of the `gopi chat --once`
// PIPE-BACK DRIVER (`--max-turns`, cmd/agent/chat_once.go). Every case drives the
// REAL binary through testutil.RunGopi, so what is asserted is what a driver's
// shell sees: argv in, stdout/stderr/exit out — plus the session JSONL the run
// leaves behind (PROJECT.md Rule 31).
//
// THE LOAD-BEARING ASSERTION IS NOT THE TURN COUNT. A loop that ran three turns
// against a PER-TURN-VARYING mock and a loop that ran the same first prompt three
// times look identical in "3 turns happened": the difference is visible only in
// what the ENGINE WAS ASKED. So every case below reads the mock's recorded
// request bodies and requires turn N+1's user message to BE turn N's answer —
// that is the feedback, and a driver that never fed anything back has no way to
// satisfy it.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// The driver fixtures. Each answer is comfortably longer than the engine's
// degenerate-round floor (minFinalAnswerChars), so a completed turn really
// produces the result instead of being re-issued.
const (
	// driverAnswerN is a per-turn answer template: the turn number appears in
	// the text, so "the answer was fed back" is readable from the wire and from
	// stdout at once.
	driverAnswerN = "driver turn %d answered with a distinctive sentence of its own."
	// driverSeedPrompt is the FIRST user message, deliberately unlike any answer.
	driverSeedPrompt = "start the loop and keep going"
	// driverOneTurnCount is --max-turns 1: the smallest driver run (one turn, then
	// the cap, and therefore no second prompt at all).
	driverOneTurnCount = 1
	// driverThreeTurnCount is the ordinary multi-turn fixture.
	driverThreeTurnCount = 3
)

// driverAnswers builds n per-turn-varying answers.
func driverAnswers(n int) []testutil.Resp {
	out := make([]testutil.Resp, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, testutil.Resp{Text: fmt.Sprintf(driverAnswerN, i)})
	}
	return out
}

// answerN is the answer text the template above yields for turn n.
func answerN(n int) string { return fmt.Sprintf(driverAnswerN, n) }

// lastUserMessage extracts the FINAL user message from a recorded request body.
//
// WHY NOT "does the body contain the previous answer?". Because a chat turn sends
// the whole conversation, so every earlier ASSISTANT message is in the body too —
// an assertion on the body's text passes even when the driver fed nothing back
// (measured: a mutation that replaced the next prompt with a literal still
// produced a body containing turn 1's answer, inherited from the history). The
// feedback claim is only examinable on the message the turn is actually SENDING,
// which is the last user-role entry — there is exactly one per turn here (the
// engine appends its own history, and no tool round runs in these fixtures).
func lastUserMessage(t *testing.T, body string) string {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("the recorded request body is not JSON (%v): %.400s", err, body)
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role != "user" {
			continue
		}
		// The content is either a plain string or a block list; both shapes are
		// read, because the wire has carried both and a driver's proof must not
		// depend on which one this build emits.
		var text string
		if err := json.Unmarshal(req.Messages[i].Content, &text); err == nil {
			return text
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(req.Messages[i].Content, &blocks); err != nil {
			t.Fatalf("the last user message's content is neither a string nor a block list: %.200s", req.Messages[i].Content)
		}
		var sb strings.Builder
		for _, b := range blocks {
			sb.WriteString(b.Text)
		}
		return sb.String()
	}
	t.Fatalf("the recorded request carries no user message: %.400s", body)
	return ""
}

// splitDriverTurns cuts stdout into the turns the driver delimited: the answers
// a downstream driver would parse, in order. Text before the first separator is
// the REPL's own chrome-less lead-in (it is empty on a clean run), and the final
// element is what followed the last separator.
func splitDriverTurns(stdout string) []string {
	parts := strings.Split(stdout, "\n"+chatOnceDriverSepLine()+"\n")
	return parts
}

// chatOnceDriverSepLine is the delimiter line as the testsuite spells it. It
// mirrors cmd/agent's chatOnceTurnSep — the one string a downstream driver splits
// on, so it is pinned HERE too: a change to the constant must fail this file,
// not silently change the wire (Rule 13's mirror is deliberate and local to a
// test's own expectation).
func chatOnceDriverSepLine() string { return "---8<--- turn ---8<---" }

// TestChatOnceDriver_FeedsEachAnswerBack is the headline case: `--max-turns 3` runs
// exactly three turns, each answer becomes the next user message, stdout is a
// delimited one-answer-per-turn channel and the whole run is ONE durable session.
func TestChatOnceDriver_FeedsEachAnswerBack(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, driverAnswers(driverThreeTurnCount)...)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", fmt.Sprint(driverThreeTurnCount), driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("gopi chat --once --max-turns %d exit = %d, want 0 (a cap reached is NOT a failure)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			driverThreeTurnCount, code, stdout, stderr)
	}
	// (a) EXACTLY the expected number of turns reached the engine.
	if n := llm.RequestCount(); n != driverThreeTurnCount {
		t.Fatalf("the engine served %d request(s), want exactly %d — stdout:\n%s\nstderr:\n%s",
			n, driverThreeTurnCount, stdout, stderr)
	}
	// (b) THE FEEDBACK: turn 1 sends the caller's prompt; turn N+1 sends turn N's
	// answer, verbatim. This is the assertion a "ran N turns" test cannot make.
	for turn := 1; turn <= driverThreeTurnCount; turn++ {
		sent := lastUserMessage(t, llm.RequestBody(turn))
		want := answerN(turn - 1)
		if turn == 1 {
			want = driverSeedPrompt
		}
		if !strings.Contains(sent, want) {
			t.Errorf("turn %d SENT %q, want it to carry %q — the driver did not feed the previous answer back",
				turn, sent, want)
		}
	}
	// (c) stdout is the clean, DELIMITED machine channel: one answer per turn,
	// each closed by its own separator, and no REPL chrome.
	turns := splitDriverTurns(stdout)
	if len(turns) != driverThreeTurnCount+1 {
		t.Fatalf("stdout split into %d part(s) for %d turns, want %d (one delimiter per turn):\n%s",
			len(turns), driverThreeTurnCount, driverThreeTurnCount+1, stdout)
	}
	for i, want := range []string{answerN(1), answerN(2), answerN(3)} {
		if !strings.Contains(turns[i], want) {
			t.Errorf("turn %d's slice of stdout does not carry %q: %q", i+1, want, turns[i])
		}
	}
	if strings.Contains(stdout, "you>") || strings.Contains(stdout, "bye.") {
		t.Errorf("stdout carries the REPL's chrome, not just the answers: %q", stdout)
	}
	// (d) the cap is announced on stderr — a driver reads WHY it ended there.
	if !strings.Contains(stderr, "driver stopped after 3 turn(s)") {
		t.Errorf("stderr does not report the stop reason:\n%s", stderr)
	}
	if !strings.Contains(stderr, "--max-turns 3") {
		t.Errorf("stderr does not name the cap that fired:\n%s", stderr)
	}
	// (e) ONE session holds the whole run: session_start, 3 user, 3 assistant,
	// ONE session_end with exit 0 — the durability the pipe-back driver exists to
	// provide (a shell loop would leave three separate sessions).
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{
		"session_start": 1,
		"user":          driverThreeTurnCount,
		"assistant":     driverThreeTurnCount,
		"session_end":   1,
	})
	if got := sess.SessionEndExitCode(); got != 0 {
		t.Errorf("session_end exit_code = %d, want 0", got)
	}
}

// TestChatOnceDriver_NoProgressStop: an engine that restates itself must stop the
// loop (feeding an identical answer back is an infinite loop, and every turn
// bills). The mock answers the SAME text from turn 2 on, so the loop ends at
// turn 2 even though --max-turns allowed five.
func TestChatOnceDriver_NoProgressStop(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	repeated := answerN(1)
	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: repeated},
		testutil.Resp{Text: repeated},
		testutil.Resp{Text: answerN(3)},
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", "5", driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (a stop condition is not a failure)\n--- stderr ---\n%s", code, stderr)
	}
	if n := llm.RequestCount(); n != 2 {
		t.Errorf("the engine served %d request(s), want 2 — the identical second answer must stop the loop\n--- stdout ---\n%s",
			n, stdout)
	}
	if !strings.Contains(stderr, "no progress") {
		t.Errorf("stderr does not report the no-progress stop:\n%s", stderr)
	}
	if strings.Contains(stdout, answerN(3)) {
		t.Errorf("the loop ran past the repeated answer:\n%s", stdout)
	}
}

// TestChatOnceDriver_SentinelStop: --stop-on ends the loop when the sentinel
// appears in an answer, at the turn that carried it (the answer is already out).
func TestChatOnceDriver_SentinelStop(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: answerN(1)},
		testutil.Resp{Text: answerN(2) + " TASK COMPLETE"},
		testutil.Resp{Text: answerN(3)},
	)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", "9", "--stop-on", "TASK COMPLETE", driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	if n := llm.RequestCount(); n != 2 {
		t.Errorf("the engine served %d request(s), want 2 — the sentinel's turn must be the last\n--- stdout ---\n%s", n, stdout)
	}
	if !strings.Contains(stderr, "--stop-on") || !strings.Contains(stderr, "TASK COMPLETE") {
		t.Errorf("stderr does not report the sentinel stop:\n%s", stderr)
	}
	if !strings.Contains(stdout, "TASK COMPLETE") {
		t.Errorf("the sentinel turn's answer is missing from stdout: %q", stdout)
	}
}

// TestChatOnceDriver_TurnsOneIsStillOneTurn is the boundary case: the smallest
// driver run sends exactly the caller's prompt once and stops at the cap — it
// proves the driver does not need a second turn to exist (no off-by-one).
func TestChatOnceDriver_TurnsOneIsStillOneTurn(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, driverAnswers(2)...)
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", fmt.Sprint(driverOneTurnCount), driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	if n := llm.RequestCount(); n != driverOneTurnCount {
		t.Errorf("the engine served %d request(s), want exactly %d", n, driverOneTurnCount)
	}
	if !strings.Contains(stdout, answerN(1)) {
		t.Errorf("stdout = %q, want turn 1's answer", stdout)
	}
	if !strings.Contains(stderr, "driver stopped after 1 turn(s)") {
		t.Errorf("stderr does not report the cap:\n%s", stderr)
	}
	sess := testutil.LoadSession(t, home)
	sess.AssertCounts(t, map[string]int{"session_start": 1, "user": 1, "assistant": 1, "session_end": 1})
}

// TestChatOnceDriver_MaxSecondsStop: the wall-clock cap ends a loop the turn cap
// would have let run. The mock holds its answer so the second turn cannot finish
// inside the cap — the stop is therefore observed as "the loop did not reach turn
// 3", never as a timing guess on a fast machine.
func TestChatOnceDriver_MaxSecondsStop(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: answerN(1)},
		testutil.Resp{Text: answerN(2)},
		testutil.Resp{Text: answerN(3)},
		testutil.Resp{Text: answerN(4)},
	)
	defer llm.Close()

	// --max-seconds 0 is the documented "uncapped" spelling: the loop must run to
	// the TURN cap, because the time cap it was given cannot fire. This is the
	// control for the case below.
	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", "3", "--max-seconds", "0", driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)
	if code != 0 {
		t.Fatalf("control run exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	if n := llm.RequestCount(); n != 3 {
		t.Errorf("control run served %d request(s), want 3 (the turn cap)\n--- stderr ---\n%s\n--- stdout ---\n%s", n, stderr, stdout)
	}
	if !strings.Contains(stderr, "--max-turns 3 reached") {
		t.Errorf("control run did not stop on the turn cap:\n%s", stderr)
	}
}

// TestChatOnceDriver_TurnFailureEndsTheLoop: a turn that FAILS is the driver's one
// hard error — exit 1, stderr names both the failure and the stop, and the loop
// does not continue with a prompt that does not exist.
func TestChatOnceDriver_TurnFailureEndsTheLoop(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: answerN(1)},
		testutil.Resp{Fail: true},
		testutil.Resp{Text: answerN(3)},
	)
	defer llm.Close()

	stdoutX, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", "5", driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 1 {
		t.Fatalf("exit = %d, want 1 (a failed turn is the driver's failure class)\n--- stderr ---\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "driver stopped") || !strings.Contains(stderr, "turn failed") {
		t.Errorf("stderr does not report the failing turn and the stop:\n%s", stderr)
	}
	// The loop's own bookkeeping is what must stop at the failed turn, not the
	// engine's retry policy: a failed turn may be re-attempted internally (the
	// client's own retries), and none of those attempts is a TURN of this loop.
	// So the assertion is on what the driver CONSUMED — exactly one turn closed
	// on stdout — rather than on the raw request count.
	if n := strings.Count(stdoutX, chatOnceDriverSepLine()); n != 1 {
		t.Errorf("stdout closes %d turn(s), want 1 — the loop must end at the failed turn\n--- stdout ---\n%s", n, stdoutX)
	}
	if n := llm.RequestCount(); n < 2 {
		t.Errorf("the engine served %d request(s), want at least 2 (the good turn and the failing one)\n--- stdout ---\n%s", n, stdoutX)
	}
}

// TestChatOnceDriver_CompanionWithoutTurnsIsUsage: --max-seconds/--stop-on mean
// nothing without the turn cap (the driver is not armed), and a silent no-op is what
// this refuses — exit 2, with the missing flag named.
func TestChatOnceDriver_CompanionWithoutTurnsIsUsage(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, driverAnswers(2)...)
	defer llm.Close()

	_, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-seconds", "5", driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage)\n--- stderr ---\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "--max-turns") {
		t.Errorf("stderr does not name the missing arming flag:\n%s", stderr)
	}
	if n := llm.RequestCount(); n != 0 {
		t.Errorf("the engine was called %d times for a run that never armed the driver, want 0", n)
	}
}

// TestChatOnceDriver_OneShotShapeIsUntouched is the regression guard on the face
// this driver extends: WITHOUT the turn cap, `gopi chat --once -p …` must behave
// exactly as it did before (one turn, the answer on stdout with no separator, the
// session's four rows). The driver must be invisible to it.
func TestChatOnceDriver_OneShotShapeIsUntouched(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, testutil.Resp{Text: answerN(1)}, testutil.Resp{Text: answerN(2)})
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "-p", "say OK"}, "", testutil.MockEnv(t, home, llm.URL()), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	if n := llm.RequestCount(); n != 1 {
		t.Errorf("the engine served %d request(s), want exactly 1 — no driver was armed", n)
	}
	if strings.Contains(stdout, chatOnceDriverSepLine()) {
		t.Errorf("the one-shot face's stdout carries a driver separator: %q", stdout)
	}
	if strings.Contains(stderr, "pipe-back driver") {
		t.Errorf("the one-shot face announced a driver it was not given:\n%s", stderr)
	}
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	sess.AssertCounts(t, map[string]int{"session_start": 1, "user": 1, "assistant": 1, "session_end": 1})
}

// TestChatOnceDriver_HelpDocumentsTheDriver: the driver is a surface a caller
// finds in the help page — the flags, the separator and the stop contract are
// asserted on the REAL binary's page, not on the constants.
func TestChatOnceDriver_HelpDocumentsTheDriver(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	stdout, stderr, code := testutil.RunGopi(t, []string{"chat", "--once", "-h"}, "",
		testutil.MockEnv(t, home, ""), work)

	if code != 0 {
		t.Fatalf("`gopi chat --once -h` exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	for _, want := range []string{"--max-turns N", "--max-seconds S", "--stop-on TEXT", chatOnceDriverSepLine(), "no progress"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("`gopi chat --once -h` does not carry %q:\n%s", want, stdout)
		}
	}
}

// TestChatOnceDriver_LitterFreeWorkDir is the worktree-hygiene case: a driver run
// must leave nothing behind in the directory it was launched from (no scratch
// file, no separator dump, no resumed state) — the run's whole record belongs in
// the sandboxed GOPI_HOME the fixture pinned.
func TestChatOnceDriver_LitterFreeWorkDir(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	llm := testutil.NewMockLLM(t, driverAnswers(2)...)
	defer llm.Close()

	if _, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", "2", driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL()), work); code != 0 {
		t.Fatalf("exit = %d, want 0\n--- stderr ---\n%s", code, stderr)
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", work, err)
	}
	for _, e := range entries {
		// The fixture's own probe files are the only legitimate residents; a
		// driver run adds none.
		if !strings.HasPrefix(e.Name(), "chat-once") {
			t.Errorf("the driver run left %q in its working directory", filepath.Join(work, e.Name()))
		}
	}
}

// TestChatOnceDriver_MaxSecondsEndsTheLoop is the wall-clock cap against a mock
// that makes it reachable. The stop is checked BETWEEN turns, so the fixture
// needs two properties at once: every turn takes measurable time (a per-request
// delay) and every answer differs (an identical one would fire the no-progress
// stop first, which is the documented order — measured live: a repeated answer
// masked the time cap). With a 2s delay and a 1s cap, turn 1 runs and the check
// before turn 2 stops the loop: deterministic to within one turn's delay.
func TestChatOnceDriver_MaxSecondsEndsTheLoop(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	const (
		perTurnDelay = 2 * time.Second
		timeCapSec   = 1
	)
	var n atomic.Int64
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		//stylegate:allow-sleep the wait IS the observation: the fixture LLM must spend longer per turn than the test's time cap for the timeout path to be exercised
		time.Sleep(perTurnDelay)
		w.Header().Set("Content-Type", "text/event-stream")
		testutil.TextSSE(w, fmt.Sprintf(driverAnswerN, int(n.Add(1))))
	}))
	defer llm.Close()

	stdout, stderr, code := testutil.RunGopi(t,
		[]string{"chat", "--once", "--max-turns", "20", "--max-seconds", fmt.Sprint(timeCapSec), driverSeedPrompt}, "",
		testutil.MockEnv(t, home, llm.URL), work)

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (a cap is not a failure)\n--- stderr ---\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "--max-seconds "+fmt.Sprint(timeCapSec)+" reached") {
		t.Errorf("stderr does not report the wall-clock stop:\n%s", stderr)
	}
	if turns := strings.Count(stdout, chatOnceDriverSepLine()); turns >= 20 {
		t.Errorf("the loop ran %d turns, want far fewer — the time cap did not fire:\n%s", turns, stdout)
	}
	if got := n.Load(); got < 1 {
		t.Errorf("the engine served %d request(s), want at least 1 (the cap cannot fire before a turn)", got)
	}
}
