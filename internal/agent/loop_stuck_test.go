package agent

// Agent-loop deadlock epic (incident t-1788173679: 4276 iterations, every
// 20s one "已完成：HEAD" plain-text round for 12h+). These tests cover:
//
//   - ① stuck detection: N consecutive no-tool same-text rounds → terminate
//     + GOPI_TASK_NOTIFY loop-stuck delivery (mock LLM);
//   - ① non-interference: tool_use-active runs never trip the net;
//   - ② wallclock cap: a cap hit finishes gracefully (nil error, notify
//     event session-wallclock-exceeded, ledger closed);
//   - ② env override: GOPI_SESSION_MAX_WALLCLOCK=0 disables the cap.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
)

// claudeToolUseStream writes one SSE response carrying a single tool_use
// block (read) — the "real work" signal for the stuck detector's reset arm.
func stuckToolUseStream(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_t\",\"name\":\"todo\",\"input\":{}}}")
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// stuckToolUseStreamArgs writes one SSE response carrying a single tool_use
// block whose input arrives as one input_json_delta fragment (the incident
// shape: bash sleep 115 + ssh poll — the toolStreak detector's target). The
// argsJSON must be a JSON-ENCODED STRING of the argument object (the
// fragment accumulator parses the combined string; a bare object reads as
// no input — see testutil.ToolUseSSE).
func stuckToolUseStreamArgs(t *testing.T, w http.ResponseWriter, name, argsJSON string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	nameJSON, _ := json.Marshal(name)        //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	partialJSON, _ := json.Marshal(argsJSON) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)// the arg object wrapped as a JSON string
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf("{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_b\",\"name\":%s,\"input\":{}}}", nameJSON))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", partialJSON))
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":7}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// newStuckTestAgent builds an Agent pointed at the mock server with the
// given MaxIterations. No session log (the loop-stuck notify falls back to
// an empty session id — delivery itself is what these tests verify via the
// GOPI_TASK_NOTIFY_CMD capture file). executeToolFn stubs out real tool
// execution: the mock's tool_use must never actually RUN (a scripted `bash
// sleep 115` would hang the test) — a benign canned result suffices, the
// detectors only consume the round's tool_use shape, not its output.
func newStuckTestAgent(serverURL string, maxIter int) *Agent {
	return &Agent{
		Client: &deepseek.Client{
			BaseURL: serverURL,
			APIKey:  "test-key",
			HTTPClient: &http.Client{
				Transport: &http.Transport{},
			},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: maxIter,
		executeToolFn: func(tc deepseek.ToolCall) string {
			return "mock result for " + tc.Name
		},
	}
}

// captureNotifyCmd points GOPI_TASK_NOTIFY_CMD at a capture file for the
// test's duration and returns a restore func. The notify pipeline runs the
// command with the JSON payload on stdin; appending to the file from `cat`
// is the cheapest capture. Events filter subscribes to the two new events.
func captureNotifyCmd(t *testing.T, captureFile string) func() {
	t.Helper()
	oldCmd, hadCmd := os.LookupEnv("GOPI_TASK_NOTIFY_CMD")
	oldEvents, hadEvents := os.LookupEnv("GOPI_TASK_NOTIFY_EVENTS")
	oldAsync, hadAsync := os.LookupEnv("GOPI_TASK_NOTIFY_ASYNC")
	os.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+captureFile)                                               //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	os.Setenv("GOPI_TASK_NOTIFY_EVENTS", "loop-stuck,loop-stuck-toolful,session-wallclock-exceeded,stale") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	os.Setenv("GOPI_TASK_NOTIFY_ASYNC", "0")                                                               //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	return func() {
		if hadCmd {
			os.Setenv("GOPI_TASK_NOTIFY_CMD", oldCmd) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv("GOPI_TASK_NOTIFY_CMD")
		}
		if hadEvents {
			os.Setenv("GOPI_TASK_NOTIFY_EVENTS", oldEvents) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv("GOPI_TASK_NOTIFY_EVENTS")
		}
		if hadAsync {
			os.Setenv("GOPI_TASK_NOTIFY_ASYNC", oldAsync) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv("GOPI_TASK_NOTIFY_ASYNC")
		}
	}
}

// TestAgent_StuckLoop_TerminatesAndNotifies: mock LLM emits the same
// plain-text end_turn forever (the t-1788173679 shape). The stuck net must
// terminate the run by maxStuckRounds consecutive rounds, deliver a
// loop-stuck notify to the capture file, and return an error.
func TestAgent_StuckLoop_TerminatesAndNotifies(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	restore := captureNotifyCmd(t, captureFile)
	defer restore()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// The incident shape: identical no-tool text every round, delivered
		// as TRUNCATED output (stop_reason max_tokens → finish_reason=length).
		// Truncated rounds reset the bug022 end-turn counter, so the run
		// would loop forever — exactly the hole the stuck net closes.
		claudeTextStream(w, "已完成：HEAD", "max_tokens")
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 20) // far above maxStuckRounds
	_, err := ag.Run("monitor something")
	if err == nil {
		t.Fatalf("expected the stuck net to terminate the run with an error")
	}
	if !strings.Contains(err.Error(), "stuck") {
		t.Fatalf("expected a stuck-related error, got: %v", err)
	}
	// bug022 gives 2 free end_turn rounds; stuck trips at maxStuckRounds.
	if calls > maxStuckRounds+1 {
		t.Fatalf("stuck net fired late: %d rounds (max %d)", calls, maxStuckRounds+1)
	}

	// Notify must have landed on disk (GOPI_TASK_NOTIFY_CMD capture).
	b, readErr := os.ReadFile(captureFile)
	if readErr != nil || len(b) == 0 {
		t.Fatalf("loop-stuck notify not captured: %v (%q)", readErr, string(b))
	}
	if !strings.Contains(string(b), notifyEventLoopStuck) {
		t.Fatalf("capture missing %q event: %s", notifyEventLoopStuck, string(b))
	}
	// Payload sanity: the summary line carries the streak reason.
	if !strings.Contains(string(b), "agent loop stuck") {
		t.Fatalf("loop-stuck payload missing summary text: %s", string(b))
	}
}

// TestAgent_StuckLoop_ToolUseResets: active tool rounds interleaved with
// identical no-tool text never trip the net (真工作不误伤).
func TestAgent_StuckLoop_ToolUseResets(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	restore := captureNotifyCmd(t, captureFile)
	defer restore()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls%2 == 1 {
			stuckToolUseStream(t, w)
			return
		}
		// Identical text between tool rounds — resets keep the streak at 1.
		claudeTextStream(w, "已完成：HEAD", "end_turn")
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 12)
	// The run ends via the max-iterations bail (no stuck trip expected).
	_, err := ag.Run("work in a loop")
	if err == nil {
		t.Fatalf("expected max-iterations bail after %d rounds", 12)
	}
	if strings.Contains(err.Error(), "stuck") {
		t.Fatalf("tool_use-active run must NOT trip the stuck net, got: %v", err)
	}
	if b, readErr := os.ReadFile(captureFile); readErr == nil && strings.Contains(string(b), notifyEventLoopStuck) {
		t.Fatalf("loop-stuck notify fired for an active run: %s", string(b))
	}
}

// TestAgent_StuckLoop_DivergentTextResets: no-tool rounds whose texts
// differ within the first stuckTextPrefixChars runes reset the streak —
// ordinary multi-round final messages are not a stuck loop.
func TestAgent_StuckLoop_DivergentTextResets(t *testing.T) {
	s := &stuckState{}
	s.observe("Here is the summary of the changes I made in this task.", false)
	for i := 0; i < maxStuckRounds+2; i++ {
		s.observe("Round "+string(rune('A'+i))+" of the divergent report stream.", false)
	}
	if s.tripped() {
		t.Fatalf("divergent texts must never trip the net (streak=%d)", s.streak)
	}
	// Two identical rounds in a row count toward the streak…
	s2 := &stuckState{}
	for i := 0; i < maxStuckRounds; i++ {
		s2.observe("已完成：HEAD", false)
	}
	if !s2.tripped() {
		t.Fatalf("identical no-tool rounds must trip at %d", maxStuckRounds)
	}
	// …and a tool round resets it.
	s2.observe("done", true)
	if s2.streak != 0 {
		t.Fatalf("tool round must reset the streak, got %d", s2.streak)
	}
}

// TestSessionWallclockCap: env resolver — unset→default, 0→unlimited,
// positive→override, invalid→default.
func TestSessionWallclockCap(t *testing.T) {
	name := sessionWallclockEnv
	old, had := os.LookupEnv(name)
	defer func() {
		if had {
			os.Setenv(name, old) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv(name)
		}
	}()

	os.Unsetenv(name)
	if got := sessionWallclockCap(); got != defaultSessionWallclock {
		t.Fatalf("unset: want %v, got %v", defaultSessionWallclock, got)
	}
	os.Setenv(name, "0") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	if got := sessionWallclockCap(); got != 0 {
		t.Fatalf("0: want unlimited (0), got %v", got)
	}
	os.Setenv(name, "2") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	if got := sessionWallclockCap(); got != 2*time.Second {
		t.Fatalf("2: want 2s, got %v", got)
	}
	os.Setenv(name, "bogus") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	if got := sessionWallclockCap(); got != defaultSessionWallclock {
		t.Fatalf("bogus: want default %v, got %v", defaultSessionWallclock, got)
	}
}

// TestAgent_WallclockCap_GracefulFinish: a cap of 1s (via env) against a
// mock that keeps looping tool rounds — the run must return gracefully
// (nil error) with the last assistant text, and a session-wallclock-exceeded
// notify must land on the capture file.
func TestAgent_WallclockCap_GracefulFinish(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	restore := captureNotifyCmd(t, captureFile)
	defer restore()

	oldCap, hadCap := os.LookupEnv(sessionWallclockEnv)
	os.Setenv(sessionWallclockEnv, "1") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	defer func() {
		if hadCap {
			os.Setenv(sessionWallclockEnv, oldCap) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv(sessionWallclockEnv)
		}
	}()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Endless real work: the tool_use must VARY per round — an
		// identical tool_use every round is precisely the loop the
		// toolStreak detector (bug055) terminates, which would preempt the
		// wallclock finish this test exercises. A genuine poll varies.
		stuckToolUseStreamArgs(t, w, "task", fmt.Sprintf(`{"action":"view","query":"tick %d"}`, calls))
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 0) // unlimited iterations
	result, err := ag.Run("long-running task")
	if err != nil {
		t.Fatalf("wallclock cap must finish gracefully (nil error), got: %v", err)
	}
	if result == "" {
		t.Fatalf("expected a non-empty graceful result")
	}
	if calls == 0 {
		t.Fatalf("expected at least one round before the cap hit")
	}
	// The notify fires from the end-of-run defer — the capture is sync
	// (GOPI_TASK_NOTIFY_ASYNC=0) so it has landed by the time Run returns.
	b, readErr := os.ReadFile(captureFile)
	if readErr != nil || !strings.Contains(string(b), notifyEventWallclockExceeded) {
		t.Fatalf("session-wallclock-exceeded notify not captured: %v (%q)", readErr, string(b))
	}
}

// TestAgent_WallclockCap_ZeroUnlimited: env=0 keeps a tool-round run going
// until the iteration budget, with NO cap semantics — exercised structurally
// via the resolver (TestSessionWallclockCap) plus a live loop that must
// reach the max-iterations bail rather than a cap finish.
func TestAgent_WallclockCap_ZeroUnlimited(t *testing.T) {
	oldCap, hadCap := os.LookupEnv(sessionWallclockEnv)
	os.Setenv(sessionWallclockEnv, "0") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	defer func() {
		if hadCap {
			os.Setenv(sessionWallclockEnv, oldCap) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv(sessionWallclockEnv)
		}
	}()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		stuckToolUseStream(t, w)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 3)
	_, err := ag.Run("loop until iterations run out")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("env=0 must disable the cap: expected max-iterations bail, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected all 3 iterations to run, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// ② toolStreak detector (bug055 — the toolful blind spot). Incident Bond
// 718319 (session 2b38bfa2, 2026-09-02): after delivery the process never
// exited — it polled `bash sleep 115; ssh hp 'sqlite3 …'` 272× (~120s each,
// 10h15m). Every round HAD a tool_use, so stuckState (no-tool counter)
// never fired. The toolStreak counter catches identical tool_use rounds.
// ---------------------------------------------------------------------------

// TestToolStreak_StateUnit: fingerprint/streak semantics — identical rounds
// accumulate, param change resets, no-tool rounds carry no signal.
func TestToolStreak_StateUnit(t *testing.T) {
	s := newToolStreakState()
	// Same tool, same params, same text → streak accumulates.
	for i := 0; i < 4; i++ {
		s.observe([]deepseek.ToolCall{{Name: "bash", Arguments: map[string]interface{}{"command": "sleep 115; ssh hp 'sqlite3 …'"}}}, "checking task status")
	}
	if s.streak != 4 {
		t.Fatalf("identical tool_use must accumulate, got streak=%d", s.streak)
	}
	// Parameter change (real work — the poll varies) → RESET to 1.
	s.observe([]deepseek.ToolCall{{Name: "bash", Arguments: map[string]interface{}{"command": "ls -la"}}}, "checking task status")
	if s.streak != 1 {
		t.Fatalf("param change must reset the streak, got %d", s.streak)
	}
	// No-tool round → carries no signal (sibling of stuckState semantics).
	s.observe(nil, "plain text round")
	if s.streak != 1 {
		t.Fatalf("no-tool round must not affect the streak, got %d", s.streak)
	}
	// Tool name change → reset.
	s.observe([]deepseek.ToolCall{{Name: "read", Arguments: map[string]interface{}{"path": "a.go"}}}, "checking")
	if s.streak != 1 {
		t.Fatalf("tool-name change must reset the streak, got %d", s.streak)
	}
	// Text prefix divergence within same params → reset (a different text is
	// a different round even with identical params).
	s.observe([]deepseek.ToolCall{{Name: "bash", Arguments: map[string]interface{}{"command": "sleep 115; ssh hp 'sqlite3 …'"}}}, "a completely different thing is happening now")
	if s.streak != 1 {
		t.Fatalf("text-prefix divergence must reset the streak, got %d", s.streak)
	}
}

// TestToolStreak_EnvOverride: env resolver — unset→default, 0→disabled,
// positive→override, invalid→default.
func TestToolStreak_EnvOverride(t *testing.T) {
	name := stuckToolfulRoundsEnv
	old, had := os.LookupEnv(name)
	defer func() {
		if had {
			os.Setenv(name, old) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv(name)
		}
	}()

	os.Unsetenv(name)
	if got := stuckToolfulRounds(); got != maxStuckToolfulRounds {
		t.Fatalf("unset: want %d, got %d", maxStuckToolfulRounds, got)
	}
	if newToolStreakState().enabled != true {
		t.Fatalf("default threshold must enable the detector")
	}
	os.Setenv(name, "0") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	if got := stuckToolfulRounds(); got != 0 {
		t.Fatalf("0: want disabled (0), got %d", got)
	}
	if newToolStreakState().enabled {
		t.Fatalf("threshold 0 must disable the detector")
	}
	os.Setenv(name, "3") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	if got := stuckToolfulRounds(); got != 3 {
		t.Fatalf("3: want 3, got %d", got)
	}
	os.Setenv(name, "bogus") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	if got := stuckToolfulRounds(); got != maxStuckToolfulRounds {
		t.Fatalf("bogus: want default %d, got %d", maxStuckToolfulRounds, got)
	}
}

// TestAgent_StuckToolfulLoop_TerminatesAndNotifies: mock LLM emits the same
// bash tool_use (identical command + identical text) forever — the Bond
// 718319 shape. The toolStreak net must terminate the run after
// maxStuckToolfulRounds rounds, deliver a loop-stuck-toolful notify, and
// return an error. Also asserts the FIRST (no-tool) counter does NOT fire —
// this is the blind spot the second counter closes.
func TestAgent_StuckToolfulLoop_TerminatesAndNotifies(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	restore := captureNotifyCmd(t, captureFile)
	defer restore()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// The incident shape: identical bash poll with sleep + fixed ssh
		// command, every round — parameters byte-identical.
		stuckToolUseStreamArgs(t, w, "bash", `{"command":"sleep 115; ssh hp 'sqlite3 …'"}`)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 30) // far above the threshold
	_, err := ag.Run("monitor the task queue")
	if err == nil {
		t.Fatalf("expected the toolStreak net to terminate the run with an error")
	}
	if !strings.Contains(err.Error(), "stuck") {
		t.Fatalf("expected a stuck-related error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "loop-stuck-toolful") {
		t.Fatalf("expected the error to cite loop-stuck-toolful, got: %v", err)
	}
	// Trips at maxStuckToolfulRounds: the rounds are tool rounds, so the
	// bug022 end-turn path and the no-tool counter never interfere.
	if calls > maxStuckToolfulRounds+1 {
		t.Fatalf("toolStreak net fired late: %d rounds (max %d)", calls, maxStuckToolfulRounds+1)
	}

	// Notify must have landed on disk with the toolful event name.
	b, readErr := os.ReadFile(captureFile)
	if readErr != nil || len(b) == 0 {
		t.Fatalf("loop-stuck-toolful notify not captured: %v (%q)", readErr, string(b))
	}
	if !strings.Contains(string(b), notifyEventLoopStuckToolful) {
		t.Fatalf("capture missing %q event: %s", notifyEventLoopStuckToolful, string(b))
	}
	// The first counter's event must NOT be in the capture (its reset arm
	// zeroes on every tool round — the very blind spot being fixed). Match
	// the exact event field — notifyEventLoopStuck ("loop-stuck") is a
	// substring of the toolful event name, so a bare Contains is fooled.
	if strings.Contains(string(b), `"event":"`+notifyEventLoopStuck+`"`) {
		t.Fatalf("no-tool stuck event fired for an identical-tool_use loop: %s", string(b))
	}
	// Payload sanity: the summary carries the tool + streak.
	if !strings.Contains(string(b), "identical tool_use") {
		t.Fatalf("loop-stuck-toolful payload missing summary: %s", string(b))
	}
}

// TestAgent_StuckToolfulLoop_ParamChangeNoTrip: identical tool_use whose
// PARAMETERS change every round (real work — each round does something
// different) must never trip the net, even with identical text.
func TestAgent_StuckToolfulLoop_ParamChangeNoTrip(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	restore := captureNotifyCmd(t, captureFile)
	defer restore()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Same tool + same text, but the command varies per round — the
		// fingerprint's param hash differs → streak resets every round.
		cmd := fmt.Sprintf("echo round %d", calls)
		stuckToolUseStreamArgs(t, w, "bash", fmt.Sprintf(`{"command":%q}`, cmd))
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 20)
	_, err := ag.Run("do real work in a loop")
	if err == nil {
		t.Fatalf("expected max-iterations bail after %d rounds", 20)
	}
	if strings.Contains(err.Error(), "stuck") {
		t.Fatalf("param-varying tool_use must NOT trip the toolStreak net, got: %v", err)
	}
	if b, readErr := os.ReadFile(captureFile); readErr == nil && strings.Contains(string(b), notifyEventLoopStuckToolful) {
		t.Fatalf("loop-stuck-toolful notify fired for param-varying work: %s", string(b))
	}
}

// TestAgent_StuckToolfulLoop_Disabled: GOPI_STUCK_TOOLFUL_ROUNDS=0 disables
// the detector — an identical tool_use loop runs to the iteration budget.
func TestAgent_StuckToolfulLoop_Disabled(t *testing.T) {
	oldRounds, hadRounds := os.LookupEnv(stuckToolfulRoundsEnv)
	os.Setenv(stuckToolfulRoundsEnv, "0") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
	defer func() {
		if hadRounds {
			os.Setenv(stuckToolfulRoundsEnv, oldRounds) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form
		} else {
			os.Unsetenv(stuckToolfulRoundsEnv)
		}
	}()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		stuckToolUseStreamArgs(t, w, "bash", `{"command":"sleep 115; ssh hp 'sqlite3 …'"}`)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 15)
	_, err := ag.Run("loop until iterations run out")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("env=0 must disable the toolStreak net: expected max-iterations bail, got %v", err)
	}
	if calls != 15 {
		t.Fatalf("expected all 15 iterations to run, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// ③ Polling-tool exemption (bug055 follow-up, t-1788306421/…): a Bond
// waiting on a background job legitimately repeats job(action=output,
// job_id=…) with IDENTICAL arguments every round — same tool + same args
// is BY DESIGN for polling tools, so the pure fingerprint signal falsely
// kills legitimate waits. The stuck signal for a poll is the POLLED
// STATE's identity: a changing result (elapsed ticking, status flip, new
// job id) = waiting on a live change (alive); a byte-identical result N
// times = the target is frozen (genuinely stuck). The 718319 bare bash
// monitor loop keeps the pure fingerprint signal (test ② above still
// terminates).
// ---------------------------------------------------------------------------

// TestToolStreak_PollingStateUnit: polling-round semantics — changing
// results never accumulate, identical results do, intervening real work
// breaks the poll chain, and a poll in a mixed batch follows the same
// rules.
func TestToolStreak_PollingStateUnit(t *testing.T) {
	s := newToolStreakState()
	jobCall := func() []deepseek.ToolCall {
		return []deepseek.ToolCall{{Name: "job", Arguments: map[string]interface{}{"action": "output", "job_id": "J1"}}}
	}
	// Still-running job: result changes every poll (elapsed ticks) → the
	// streak must never leave 1, even across 25 polls (way above the
	// threshold of 10) — the Bond is legitimately waiting.
	for i := 0; i < 25; i++ {
		s.observe(jobCall(), "checking on the job")
		s.observeResults(jobCall(), []string{fmt.Sprintf("job J1 status=running elapsed=%.1fs", float64(i)*0.7)})
		if s.tripped() {
			t.Fatalf("changing poll results must never trip: streak=%d", s.streak)
		}
	}
	if s.streak != 1 {
		t.Fatalf("changing poll result must reset to 1, got streak=%d", s.streak)
	}

	// Completed/unchanging job: byte-identical result every poll → the
	// polled state is frozen → accumulates and trips at the threshold.
	s2 := newToolStreakState()
	frozen := "job J1 status=exited code=0 elapsed=3.0s\nall done"
	for i := 0; i < maxStuckToolfulRounds; i++ {
		s2.observe(jobCall(), "checking on the job")
		s2.observeResults(jobCall(), []string{frozen})
	}
	if !s2.tripped() {
		t.Fatalf("frozen poll results must trip after %d rounds, got streak=%d", maxStuckToolfulRounds, s2.streak)
	}

	// Real work between polls breaks the chain: identical polls separated
	// by a read round must NOT accumulate across the boundary.
	s3 := newToolStreakState()
	for i := 0; i < maxStuckToolfulRounds-2; i++ {
		s3.observe(jobCall(), "polling")
		s3.observeResults(jobCall(), []string{frozen})
	}
	s3.observe([]deepseek.ToolCall{{Name: "read", Arguments: map[string]interface{}{"path": "a.go"}}}, "doing real work")
	s3.observe(jobCall(), "polling again")
	s3.observeResults(jobCall(), []string{frozen})
	if s3.streak != 1 {
		t.Fatalf("intervening real work must break the poll chain, got streak=%d", s3.streak)
	}

	// job action=start: identical args but the result carries a NEW job id
	// every call → changing result → never accumulates.
	s4 := newToolStreakState()
	for i := 0; i < 20; i++ {
		call := []deepseek.ToolCall{{Name: "job", Arguments: map[string]interface{}{"action": "start", "command": "sleep 5"}}}
		s4.observe(call, "starting work")
		s4.observeResults(call, []string{fmt.Sprintf("job started: J%d", i)})
		if s4.tripped() {
			t.Fatalf("job action=start with fresh ids must never trip: streak=%d", s4.streak)
		}
	}

	// 718319 shape: bash is NOT a polling tool → pure fingerprint
	// accumulation still trips (the exemption must stay narrow).
	s5 := newToolStreakState()
	bashCall := []deepseek.ToolCall{{Name: "bash", Arguments: map[string]interface{}{"command": "sleep 115; ssh hp 'sqlite3 …'"}}}
	for i := 0; i < maxStuckToolfulRounds; i++ {
		s5.observe(bashCall, "monitor the task queue")
	}
	if !s5.tripped() {
		t.Fatalf("bare bash monitor loop must still trip, got streak=%d", s5.streak)
	}
}

// TestAgent_PollingJobOutput_RunningJobNotKilled (acceptance a): a Bond
// polling job(action=output) on a still-running job for 20+ rounds is NOT
// terminated — the result changes every round (elapsed ticks), proving the
// Bond waits on a live external change. The run must end only via the
// max-iterations budget, with no loop-stuck-toolful notify.
func TestAgent_PollingJobOutput_RunningJobNotKilled(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	restore := captureNotifyCmd(t, captureFile)
	defer restore()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		stuckToolUseStreamArgs(t, w, "job", `{"action":"output","job_id":"J1"}`)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 25) // 25 rounds, threshold is 10
	ag.executeToolFn = func(tc deepseek.ToolCall) string {
		// Still-running job: the polled state CHANGES every round — the
		// same shape a real JobOutput returns for a live job (elapsed
		// ticks, status stays running).
		return fmt.Sprintf("job J1 status=running elapsed=%.1fs\n(no output yet)", float64(calls)*0.7)
	}
	_, err := ag.Run("wait for the build job to finish")
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("waiting Bond must run to the iteration budget, got: %v", err)
	}
	if strings.Contains(err.Error(), "stuck") {
		t.Fatalf("a waiting Bond polling a RUNNING job must NOT be killed, got: %v", err)
	}
	if b, readErr := os.ReadFile(captureFile); readErr == nil && strings.Contains(string(b), notifyEventLoopStuckToolful) {
		t.Fatalf("loop-stuck-toolful notify fired for a legitimate wait: %s", string(b))
	}
	if calls < 20 {
		t.Fatalf("expected 20+ legitimate poll rounds, got %d", calls)
	}
}

// TestAgent_PollingJobOutput_FrozenJobKilled (acceptance c): a job
// action=output poll on a completed/unchanging job for N rounds IS
// terminated — the polled state is frozen (identical result every round),
// which is exactly the stuck shape. The run must end with the
// loop-stuck-toolful error and notify after maxStuckToolfulRounds rounds.
func TestAgent_PollingJobOutput_FrozenJobKilled(t *testing.T) {
	captureFile := t.TempDir() + "/notify.jsonl"
	restore := captureNotifyCmd(t, captureFile)
	defer restore()

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		stuckToolUseStreamArgs(t, w, "job", `{"action":"output","job_id":"J1"}`)
	}))
	defer server.Close()

	ag := newStuckTestAgent(server.URL, 30) // far above the threshold
	ag.executeToolFn = func(tc deepseek.ToolCall) string {
		// Completed job: status=exited, elapsed frozen, output identical —
		// byte-for-byte the same result every poll. The Bond is NOT waiting
		// on any change; it is stuck.
		return "job J1 status=exited code=0 elapsed=3.0s\nall done"
	}
	_, err := ag.Run("collect the job result")
	if err == nil {
		t.Fatalf("a frozen job action=output poll must terminate the run")
	}
	if !strings.Contains(err.Error(), "loop-stuck-toolful") {
		t.Fatalf("expected the error to cite loop-stuck-toolful, got: %v", err)
	}
	if calls > maxStuckToolfulRounds+1 {
		t.Fatalf("frozen poll net fired late: %d rounds (max %d)", calls, maxStuckToolfulRounds+1)
	}
	b, readErr := os.ReadFile(captureFile)
	if readErr != nil || !strings.Contains(string(b), notifyEventLoopStuckToolful) {
		t.Fatalf("loop-stuck-toolful notify not captured: %v (%q)", readErr, string(b))
	}
}
