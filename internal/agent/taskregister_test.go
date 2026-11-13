package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/runend"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// Task-ledger auto-registration (internal/agent/taskregister.go). Every test
// uses a fresh temp memory.db (GOPI_MEMORY_DB) + a real SessionLogger pointed
// at a temp sessions dir via GOPI_HOME (the gopi-data root override: .gopi
// moves under it), so the real ~/.gopi/sessions is NEVER touched. The comment
// used to claim a "Home override" without actually setting one — this leaked
// one throwaway session log into the real profile per test run.
// ---------------------------------------------------------------------------

func newAutoTaskAgent(t *testing.T) *Agent {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir()) // isolate the real ~/.gopi profile
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	// The test process may itself be a task-run child (GOPI_TASK_WRAPPER_ID
	// set by an outer wrapper) — every test here runs an agent that must be
	// treated as a PLAIN session unless a test explicitly sets the env.
	t.Setenv(tools.TaskWrapperEnv, "")
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	//stylegate:ignore test cleanup: SessionLogger.Close flushes and fsyncs the JSONL at teardown; the test asserts on registration state, not on log durability
	t.Cleanup(func() { sl.Close() })
	a := &Agent{SessionLog: sl}
	return a
}

func TestAutoTaskRegisterOnAndEndHook(t *testing.T) {
	a := newAutoTaskAgent(t)

	// START hook: registers in_progress row with an auto t-<unix> task_id,
	// decoupled from the session UUID (consolidation).
	a.maybeRegisterSessionTask("Fix the login timeout bug in the scheduler")
	if !a.autoTaskRegistered {
		t.Fatal("start hook did not register")
	}
	taskID := a.autoTaskRowID
	if len(taskID) != 36 || !strings.Contains(taskID, "-") {
		t.Fatalf("auto task_id = %q, want uuid v7 shape (consolidation: task_id != session_id)", taskID)
	}
	sessID := a.SessionLog.SessionID()
	if taskID == sessID {
		t.Fatalf("task_id %q must not equal session_id (consolidation)", taskID)
	}
	row, err := tools.TaskShow(taskID)
	if err != nil || !strings.Contains(row, "in_progress") || !strings.Contains(row, "[auto] Fix the login timeout") {
		t.Fatalf("start row = %q, err %v; want in_progress + [auto] summary", row, err)
	}
	if !strings.Contains(row, sessID) {
		t.Errorf("session_id column not set: %q", row)
	}

	// END hook: completed on exit 0, stats recorded as the task_events
	// result digest — the summary stays the [auto] briefing (bug056).
	a.finishSessionTask(0, 7, 4321, 12.5)
	a2, err := tools.TaskShow(taskID)
	if err != nil || !strings.Contains(a2, "completed") || !strings.Contains(a2, "result:         — elapsed=12.5s iter=7 tokens=4321") {
		t.Fatalf("end row = %q, err %v; want completed + stats in result line", a2, err)
	}
	if !strings.Contains(a2, "summary:        [auto] Fix the login timeout") {
		t.Errorf("auto summary must stay the [auto] briefing (bug056):\n%s", a2)
	}
	if strings.Contains(a2, "summary:        [auto] Fix the login timeout — elapsed") {
		t.Errorf("stats must NOT append to the immutable summary (bug056):\n%s", a2)
	}
	if !strings.Contains(a2, "ended_at:") || strings.Contains(a2, "(not ended)") {
		t.Errorf("ended_at not stamped: %q", a2)
	}
	// Latch cleared: without a second START the end hook is a no-op (one row
	// per session — the single-threaded run path cannot re-arm the latch).
	if a.autoTaskRegistered {
		t.Error("end hook did not clear the registration latch")
	}
	a.finishSessionTask(0, 9, 9999, 99.9)
	a3, _ := tools.TaskShow(taskID) //stylegate:ignore task read after a ledger mutation the test performed; the compared row is the contract
	if strings.Contains(a3, "iter=9") {
		t.Errorf("second end hook mutated the row: %q", a3)
	}
}

func TestAutoTaskRegisterFailedStatus(t *testing.T) {
	a := newAutoTaskAgent(t)

	a.maybeRegisterSessionTask("Run the failing deploy task")
	a.finishSessionTask(1, 3, 100, 5.0)
	row, err := tools.TaskShow(a.autoTaskRowID)
	// Width-agnostic (bug027): the label padding is the formatter's
	// business — assert on label + value with flexible whitespace.
	if err != nil || !regexp.MustCompile(`status:\s+failed`).MatchString(row) {
		t.Fatalf("row = %q, err %v; want failed status on exit 1", row, err)
	}
}

// TestFailedLedgerHint_AccountLevelNotResumable — the second half of the
// GLM-1113 misreport: the failed-row digest used to offer
// "recoverable: `gopi session resume <id>`" for EVERY failure, so an operator reading
// the ledger was told to replay a task into an account that is out of balance.
// The hint now follows the taxonomy; the account remedy lands right after it.
func TestFailedLedgerHint_AccountLevelNotResumable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","code":"1113","message":"[1113][余额不足或无可用资源包,请充值。]"}}`))
	}))
	defer srv.Close()
	c := &deepseek.Client{
		BaseURL:     srv.URL,
		APIKey:      "k",
		HTTPClient:  srv.Client(),
		RetryConfig: deepseek.RetryConfig{MaxRetries: 0},
	}
	_, err := c.ClaudeChat(context.Background(), &deepseek.ClaudeRequest{
		Model: "m", MaxTokens: 10,
		Messages: []deepseek.ClaudeMessage{{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "hi"}}}},
	})
	if err == nil {
		t.Fatal("expected the account-level send to fail")
	}

	a := newAutoTaskAgent(t)
	a.Client = c
	a.maybeRegisterSessionTask("Run the account-blocked deploy task")
	a.finishSessionTask(1, 3, 100, 5.0)

	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(row, "FAILED — recoverable:") {
		t.Errorf("account-level failure offered the resume pointer — a resumer would replay into the same dead account:\n%s", row)
	}
	if !strings.Contains(row, "NOT recoverable by resume") {
		t.Errorf("ledger row missing the not-resumable verdict:\n%s", row)
	}
	if !strings.Contains(row, "category=account") || !strings.Contains(row, "remedy=") {
		t.Errorf("ledger row missing the account evidence + remedy:\n%s", row)
	}
}

// TestFailedLedgerHint_GenuineFailureKeepsResumePointer is the regression
// guard: a NON-account failure keeps the original resume wording verbatim
// (the ledger contract of every other failure is untouched).
func TestFailedLedgerHint_GenuineFailureKeepsResumePointer(t *testing.T) {
	a := newAutoTaskAgent(t)
	a.maybeRegisterSessionTask("Run the plainly failing task")
	a.finishSessionTask(1, 3, 100, 5.0)

	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(row, "FAILED — recoverable: `gopi session resume") {
		t.Errorf("non-account failure lost its resume pointer:\n%s", row)
	}
	if strings.Contains(row, "NOT recoverable by resume") {
		t.Errorf("non-account failure wrongly marked not-resumable:\n%s", row)
	}
}

func TestAutoTaskRegisterKillSwitch(t *testing.T) {
	a := newAutoTaskAgent(t)
	t.Setenv(autoTaskRegisterEnv, autoTaskRegisterOffValue)

	a.maybeRegisterSessionTask("should not be registered")
	if a.autoTaskRegistered {
		t.Fatal("kill switch ignored — row registered anyway")
	}
	// End hook is a no-op when nothing was registered.
	a.finishSessionTask(0, 1, 1, 1.0) // must not panic or write
	out, err := tools.TaskList("")
	if err != nil || !strings.Contains(out, "No tasks found.") {
		t.Errorf("kill-switch run left rows: %q, err %v", out, err)
	}
}

func TestAutoTaskRegisterEmptyBriefingSkipped(t *testing.T) {
	a := newAutoTaskAgent(t)

	a.maybeRegisterSessionTask("   \n\t  ")
	if a.autoTaskRegistered {
		t.Fatal("empty briefing registered")
	}
	out, err := tools.TaskList("")
	if err != nil || !strings.Contains(out, "No tasks found.") {
		t.Errorf("empty briefing left rows: %q", out)
	}
}

func TestAutoTaskSummaryNormalized(t *testing.T) {
	got := normalizeTaskSummary("  Hello \n\t world   \n  ")
	if got != "Hello world" {
		t.Errorf("normalizeTaskSummary = %q, want \"Hello world\"", got)
	}
	long := strings.Repeat("字", autoTaskSummaryMaxChars+50)
	got = normalizeTaskSummary(long)
	if len([]rune(strings.TrimSuffix(got, "…"))) != autoTaskSummaryMaxChars {
		t.Errorf("capped summary rune count = %d, want %d (+ ellipsis)", len([]rune(strings.TrimSuffix(got, "…"))), autoTaskSummaryMaxChars)
	}
}

func TestAutoTaskNoDeletePathRegression(t *testing.T) {
	// The append-only iron rule: the auto-registration path must not add any
	// way to remove a ledger row. Guard the whole engine surface: TaskSearch
	// and the hooks may only create/update — a grep of the tasks engine +
	// registration files must show no DELETE statement anywhere.
	for _, f := range []string{"tasks.go", "tasks_fts_test.go"} {
		src, err := os.ReadFile(filepath.Join("..", "tools", f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "DELETE FROM tasks") {
				t.Errorf("%s contains a task delete path: %q", f, line)
			}
		}
	}
	// The end hook flips status and records the run stats as a task_events
	// result digest (bug056: the summary column is never edited — TaskUpdate's
	// terminal-status stamping is the sole ending mechanism; existing
	// tasks_test.go covers "no delete CLI verb").
}

// TestChatTaskRegistration_Wrappers verifies the exported RegisterSessionTask
// / FinishSessionTask wrappers — the entry points the gopi chat CLI uses to
// create and close its task-ledger row with the correct status. The chat path
// can't call the unexported hooks from package main, so these wrappers must
// faithfully delegate: register → in_progress row keyed by the session id,
// finish → completed (exit 0) / failed (exit != 0).
func TestChatTaskRegistration_Wrappers(t *testing.T) {
	a := newAutoTaskAgent(t)

	// Register a chat as a task row (task_id = auto t-<unix>, session_id
	// column = the session id — consolidation decouples the two).
	a.RegisterSessionTask("what is the capital of France?")
	if !a.autoTaskRegistered {
		t.Fatal("RegisterSessionTask did not register a row")
	}
	taskID := a.autoTaskRowID
	row, err := tools.TaskShow(taskID)
	if err != nil || !strings.Contains(row, "in_progress") {
		t.Fatalf("registered row = %q, err %v; want in_progress", row, err)
	}
	if !strings.Contains(row, "[auto] what is the capital of France?") {
		t.Errorf("row summary missing chat input: %q", row)
	}

	// Finish with exit 0 → completed.
	a.FinishSessionTask(0, 2, 150, 8.0)
	row2, err := tools.TaskShow(taskID)
	if err != nil || !strings.Contains(row2, "completed") {
		t.Fatalf("finished row = %q, err %v; want completed", row2, err)
	}
	if !strings.Contains(row2, "result:         — elapsed=8.0s iter=2 tokens=150") {
		t.Errorf("finished row missing stats in the result line: %q", row2)
	}
	if !strings.Contains(row2, "summary:        [auto] what is the capital of France?") {
		t.Errorf("chat summary must stay the [auto] input (bug056): %q", row2)
	}
}

func TestChatTaskRegistration_WrapperFailedStatus(t *testing.T) {
	a := newAutoTaskAgent(t)

	a.RegisterSessionTask("failing chat turn")
	a.FinishSessionTask(1, 1, 20, 2.0)
	row, err := tools.TaskShow(a.autoTaskRowID)
	// Width-agnostic (bug027) — see TestAutoTaskRegisterFailedStatus.
	if err != nil || !regexp.MustCompile(`status:\s+failed`).MatchString(row) {
		t.Fatalf("row = %q, err %v; want failed on exit 1", row, err)
	}
}

func TestChatTaskRegistration_NoRowWhenUnregistered(t *testing.T) {
	// FinishSessionTask with no prior RegisterSessionTask must be a no-op
	// (empty chat / /quit before any turn) — no orphan in_progress row lingers.
	a := newAutoTaskAgent(t)
	a.FinishSessionTask(0, 0, 0, 1.0)
	out, err := tools.TaskList("")
	if err != nil || !strings.Contains(out, "No tasks found.") {
		t.Errorf("unregistered chat left rows: %q, err %v", out, err)
	}
}

// countTerminalRowWriteWitness counts the session JSONL's task_row_closed
// events (the end hook's terminal-write witness, sessionevent). Used for the
// NEGATIVE arm: a run that writes no terminal row must carry no witness.
func countTerminalRowWriteWitness(t *testing.T, a *Agent) int {
	t.Helper()
	if a.SessionLog == nil {
		return 0
	}
	data, err := os.ReadFile(a.SessionLog.Path())
	if err != nil {
		t.Fatalf("read session JSONL %s: %v", a.SessionLog.Path(), err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev["type"] == sessionevent.EventTypeTaskRowClosed {
			n++
		}
	}
	return n
}

// TestAutoTaskRegisterStartHookReentryKeepsTheEndHookArmed pins the
// truncated-run defect (epic 2026-09-25): the START hook can be reached TWICE
// for one run, and the row the first call registered must still be closed by
// the END hook afterwards.
//
// Measured shape of the defect (a probe on the failing run, since removed):
// the hook cleared a.autoTaskRegistered on ENTRY, then returned through the
// adopted-row gate ("continues an existing row; no auto mirror"). The end
// hook's own early return — `!a.autoTaskRegistered || a.SessionLog == nil` —
// then fired and the row was NEVER closed, so a run the wallclock cap cut
// short sat at `in_progress` forever (the two reds:
// TestAgent_{WallclockCap,StuckLoop}_TruncatedRunIsNotCompleted). The status
// rule was already correct; the latch is what was lost.
func TestAutoTaskRegisterStartHookReentryKeepsTheEndHookArmed(t *testing.T) {
	// SHAPE A — the loop's real order: the first call arrives BEFORE the
	// session logger exists (nothing to register, no row), the second call is
	// the one that mints the row. The end hook must be armed by THAT call.
	a := newAutoTaskAgent(t)
	sl := a.SessionLog
	a.SessionLog = nil
	a.maybeRegisterSessionTask("pre-logger call — nothing registers yet")
	if a.autoTaskRegistered || a.autoTaskRowID != "" {
		t.Fatalf("with no session log there is nothing to register: registered=%v rowID=%q", a.autoTaskRegistered, a.autoTaskRowID)
	}
	a.SessionLog = sl
	a.maybeRegisterSessionTask("post-logger call — the row is minted here")
	if !a.autoTaskRegistered {
		t.Fatal("the call that REGISTERED the row must leave the end hook armed")
	}
	rowID := a.autoTaskRowID

	// SHAPE B — the measured defect: the hook re-enters with the row already
	// registered. It must keep the latch (and mint no mirror).
	a.maybeRegisterSessionTask("post-logger call — the row is minted here")
	if !a.autoTaskRegistered {
		t.Fatal("a second start-hook call DISARMED the end hook — the row stays in_progress forever (the truncated-run defect)")
	}
	if a.autoTaskRowID != rowID {
		t.Errorf("autoTaskRowID = %q after the re-entry, want the registered row %q", a.autoTaskRowID, rowID)
	}
	list, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if got := strings.Count(list, "[auto]"); got != 1 {
		t.Errorf("the re-entrant call minted a mirror row: %d [auto] rows, want exactly 1:\n%s", got, list)
	}

	// The point of the latch: the END hook actually closes the row.
	a.finishSessionTaskRecorded(0, 3, 90, 2.5, nil, runend.WallclockCap)
	row, err := tools.TaskShow(rowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !regexp.MustCompile(`status:\s+failed`).MatchString(row) {
		t.Fatalf("the end hook must still close the row it stayed armed for:\n%s", row)
	}
	if !strings.Contains(row, runend.WallclockCap.Token()) {
		t.Errorf("the closed row must record the run's end reason %q:\n%s", runend.WallclockCap.Token(), row)
	}
}

// TestAutoTaskRegisterWrapperChildNoAutoRow (task-single-row-exec): when the
// session is a `task run` CHILD, the parent exports GOPI_TASK_WRAPPER_ID (the
// human wrapper row's task_id) BEFORE spawn. The start hook must NOT mint an
// auto mirror row — the human wrapper row IS the run (single execution row).
// Instead it latches autoTaskRowID to the wrapper id (heartbeat target) and
// autoTaskRegistered stays false (the END hook is a no-op — the wrapper
// side's writeback owns the terminal state). A plain session (env absent)
// still registers its own auto row.
func TestAutoTaskRegisterWrapperChildNoAutoRow(t *testing.T) {
	a := newAutoTaskAgent(t)

	// Seed the wrapper row the child would be dispatched from.
	if _, err := tools.TaskNew("wrapper work", "t-wrap-env", "", ""); err != nil {
		t.Fatalf("TaskNew wrapper: %v", err)
	}

	// CHILD run: env set → NO auto row, autoTaskRowID = wrapper id.
	t.Setenv(tools.TaskWrapperEnv, "t-wrap-env")
	a.maybeRegisterSessionTask("run the wrapper work")
	if a.autoTaskRegistered {
		t.Fatal("wrapper child must NOT register an auto row")
	}
	if a.autoTaskRowID != "t-wrap-env" {
		t.Errorf("autoTaskRowID = %q, want the wrapper id t-wrap-env (heartbeat targets the human row)", a.autoTaskRowID)
	}
	// No auto mirror row was created: the wrapper row is the ONLY row.
	list, err := tools.TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if !strings.Contains(list, "t-wrap-env") {
		t.Errorf("wrapper row missing from list:\n%s", list)
	}
	if strings.Contains(list, "[auto]") {
		t.Errorf("no auto row may exist for a wrapper child (single-row exec):\n%s", list)
	}
	// The END hook is a no-op for a wrapper child (autoTaskRegistered=false)
	// — it must not try to close a row it never registered, and it must leave
	// NO terminal-write witness: that absence is the negative half of the
	// truncated-run evidence (a witness is proof the ledger write happened, so
	// a run that performs no write must not carry one).
	a.finishSessionTask(0, 2, 50, 1.5)
	if n := countTerminalRowWriteWitness(t, a); n != 0 {
		t.Errorf("a wrapper child writes no terminal row, so it must log no %s event, got %d", sessionevent.EventTypeTaskRowClosed, n)
	}

	// Plain session (env absent) still registers its own auto row.
	a2 := newAutoTaskAgent(t)
	t.Setenv(tools.TaskWrapperEnv, "")
	a2.maybeRegisterSessionTask("plain auto work")
	if !a2.autoTaskRegistered {
		t.Fatal("plain session start hook must still register an auto row")
	}
}

// TestAutoTaskRegisterPlainSessionStampsOwnLaunchCommand: a plain auto row
// (no wrapper env) IS the run, so it must carry its OWN launch command — the
// row's whole "how was this run" answer (`task show` on such a row used to
// print NEITHER run_method nor run_cmd, the defect in the attached report).
// The command is this process's argv (runcmd renders it: basename + shell
// quoting), and it must be a REAL command — a hand-written description would
// defeat the paste-and-re-run purpose.
func TestAutoTaskRegisterPlainSessionStampsOwnLaunchCommand(t *testing.T) {
	a := newAutoTaskAgent(t)
	// Clear any wrapper-published identity so the stamped command is THIS
	// process's own argv.
	t.Setenv(runcmd.EnvBinary, "")
	t.Setenv(runcmd.EnvSelf, "")

	a.maybeRegisterSessionTask("plain auto work")
	if !a.autoTaskRegistered {
		t.Fatal("plain start hook did not register")
	}
	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow plain: %v", err)
	}
	if !strings.Contains(row, "run_method:     "+runcmd.MethodAuto) {
		t.Errorf("plain auto row must carry method=%s:\n%s", runcmd.MethodAuto, row)
	}
	// The stamped command is THIS process's verbatim argv (rendered by the
	// shared renderer): the row's own launch line, not a description of it.
	want := "run_cmd:        " + runcmd.Command(os.Args)
	if !strings.Contains(row, want) {
		t.Errorf("plain auto row must carry the verbatim launcher command %q:\n%s", want, row)
	}
	// A pseudo-command (the defect: a human-readable parenthesis where a
	// command belongs) is what this epic removed — pin its absence.
	cmd := runcmd.Command(os.Args)
	if strings.ContainsAny(cmd, "()") {
		t.Errorf("run_cmd must be a pasteable command, not a description: %q", cmd)
	}
	if first, _, _ := strings.Cut(cmd, " "); strings.Contains(first, "/") {
		t.Errorf("run_cmd must name the binary by basename: %q", cmd)
	}
}

// TestAutoTaskRegisterSelfClaim (stale-zombie epic regression, 2026-09-05):
// the START hook must stamp the auto row with the runner's own hostname:pid
// claim (TaskStampSelfClaim) — BEFORE the fix auto rows carried NO claim
// stamp, so a killed runner's row stayed forever at "suspected" in the
// zombie evidence chain (--kill refuses suspected rows) = the 8h+ stale
// in_progress rows. The row IS the run, so the runner owning it means a
// dead runner → confirmed → disposable, a LIVE runner → pid veto.
func TestAutoTaskRegisterSelfClaim(t *testing.T) {
	a := newAutoTaskAgent(t)

	a.maybeRegisterSessionTask("Fix the stale smoke rows")
	if !a.autoTaskRegistered {
		t.Fatal("start hook did not register")
	}
	row, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	// The claim stamp must be present and shaped hostname:pid — the exact
	// stamp zombieVerdict's pid-liveness layer needs to CONFIRM a dead runner.
	if !strings.Contains(row, "claimed_by:") {
		t.Fatalf("auto row must carry a claim stamp (zombie-confirm layer):\n%s", row)
	}
	if !strings.Contains(row, "(at ") {
		t.Errorf("auto row must stamp claimed_at (rendered as (at <time>)):\n%s", row)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	want := host + ":"
	if !strings.Contains(row, want) {
		t.Errorf("claim stamp must be %s<pid> (hostname:pid), got:\n%s", want, row)
	}
	// End hook still closes the row normally (claim stamp must not break the
	// completed transition).
	a.finishSessionTask(0, 3, 100, 2.0)
	row2, err := tools.TaskShow(a.autoTaskRowID)
	if err != nil || !regexp.MustCompile(`status:\s+completed`).MatchString(row2) {
		t.Fatalf("stamped auto row must still complete on exit 0: %q err=%v", row2, err)
	}
}

// TestAutoTaskRegisterOff_IsOff: the exported OFF pair must be the WIRE
// contract a spawner hands a child — pinned by literal, deliberately (the
// literal IS the contract; the constants must equal it), and validated
// against the ONLY reader (autoTaskRegisterEnabledWith). Bug 01a089b7
// (2026-09-10): the merger's resolve child was spawned with the OFF env NAME
// carrying the ON value ("=1"), so it minted a ledger row per resolve — one
// merger lineage looking like several merger processes. This gate makes that
// class of typo fail here instead of in the ledger.
func TestAutoTaskRegisterOff_IsOff(t *testing.T) {
	// (1) the wire spelling: NAME=0 — the canonical OFF value documented on
	// autoTaskRegisterEnv. The literal is intentional: a future "improvement"
	// that changes the OFF spelling without changing the spawners is exactly
	// the silent drift this test exists to catch.
	if AutoTaskRegisterOff != "GOPI_AUTO_TASK_REGISTER=0" {
		t.Fatalf("AutoTaskRegisterOff = %q, want the OFF wire pair %q", AutoTaskRegisterOff, "GOPI_AUTO_TASK_REGISTER=0")
	}
	// (2) the constants stay one pair.
	if AutoTaskRegisterOff != autoTaskRegisterEnv+"="+autoTaskRegisterOffValue {
		t.Fatalf("AutoTaskRegisterOff = %q, want %s=%s", AutoTaskRegisterOff, autoTaskRegisterEnv, autoTaskRegisterOffValue)
	}
	kv := strings.SplitN(AutoTaskRegisterOff, "=", 2)
	if len(kv) != 2 {
		t.Fatalf("AutoTaskRegisterOff must be a NAME=VALUE pair, got %q", AutoTaskRegisterOff)
	}
	// (3) the ONLY reader must actually read it as OFF.
	lookup := func(k string) string {
		if k == kv[0] {
			return kv[1]
		}
		return ""
	}
	if autoTaskRegisterEnabledWith(lookup) {
		t.Errorf("AutoTaskRegisterOff (%q) does NOT disable auto-registration — an OFF name with an ON value re-mints rows", AutoTaskRegisterOff)
	}
	// (4) and the ON value is not OFF: the regression shape ("=1").
	onLookup := func(k string) string {
		if k == kv[0] {
			return "1"
		}
		return ""
	}
	if !autoTaskRegisterEnabledWith(onLookup) {
		t.Errorf("%s=1 must mean auto-registration ON (the defect shape)", kv[0])
	}
	// (5) absent env → default ON (only an explicit OFF value disables).
	if !autoTaskRegisterEnabledWith(func(string) string { return "" }) {
		t.Error("auto-registration must default ON (only an explicit OFF value disables it)")
	}
}
