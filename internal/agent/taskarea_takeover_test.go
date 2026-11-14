package agent

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/tools"
)

// memory-aware T2 (framework hard-takeover of the task list + resume
// carry-forward) — evidence tests E1-E6.
//
// E1: LLM declares "all done" in free text while the framework's pending set
// is non-empty → the run does NOT end; a completion nudge is injected and
// the loop continues.
// E2: every item committed via write_task_area → the run ends normally.
// E3: resume arms CarryForwardSessionID → the FIRST resumed request carries
// the original session's committed-conclusions block (marker
// [WRITE_TASK_AREA — COMMITTED CONCLUSIONS]).
// E4: committed items' analysis rounds disappear from later requests
// (trim works under the guarded loop).
// E6: single-step sessions (no TASK_LIST) exit without a nudge.

// taskListResponse wraps a task-list JSON block around response text.
func taskListResponse(text string, items ...TaskItem) string {
	jsonParts := make([]string, len(items))
	for i, it := range items {
		b, _ := json.Marshal(it) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		jsonParts[i] = string(b)
	}
	return text + "\n[TASK_LIST_BEGIN]\n[" + strings.Join(jsonParts, ",") + "]\n[TASK_LIST_END]"
}

// commitToolUseStream replays a task(action=commit) tool_use round
// (tool-family merge — task family: was write_task_area, now a `task` action).
func commitToolUseStream(w http.ResponseWriter, taskID, conclusion string) {
	claudeToolUseStream(w, "task", map[string]interface{}{
		"action": "commit", "task_id": taskID, "status": "completed", "conclusion": conclusion,
	})
}

// reviewToolUseStream replays a task(action=review) tool_use round returning a
// verdict — the post-commit checkpoint as a structured tool call.
func reviewToolUseStream(w http.ResponseWriter, verdict, taskID, reason string) {
	args := map[string]interface{}{"action": "review", "verdict": verdict}
	if taskID != "" {
		args["task_id"] = taskID
	}
	if reason != "" {
		args["reason"] = reason
	}
	claudeToolUseStream(w, "task", args)
}

// e2eAgent is a stub-backed full-loop harness: an HTTP server replays a
// canned script of Claude SSE responses; the Agent runs against it.
type e2eAgent struct {
	ag             *Agent
	server         *httptest.Server
	callNum        int
	sessionLog     *SessionLogger
	sessionLogPath string
}

func newE2EAgent(t *testing.T, sessionID string, script []func(w http.ResponseWriter)) *e2eAgent {
	t.Helper()
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	s := &e2eAgent{callNum: 0}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.callNum++
		if s.callNum <= len(script) {
			w.Header().Set("Content-Type", "text/event-stream")
			script[s.callNum-1](w)
			return
		}
		http.Error(w, "script exhausted (over-call)", http.StatusInternalServerError)
	}))
	s.ag = &Agent{
		Client: &deepseek.Client{
			BaseURL:    s.server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:               &Context{},
		Model:             "deepseek-v4-pro",
		MaxIterations:     10,
		Logger:            log.New(io.Discard, "", 0),
		autoTaskSessionID: sessionID,
		NoEnforceMemory:   true, // task-start/task-end memory checkpoints OFF — E-tests target task-area guards
		TaskState:         newTodoState(),
	}
	// Session raw-log capture: E1/E3 assert the framework injections appear
	// as real user events in the session JSONL (acceptance evidence, not
	// just in-memory state).
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	s.ag.SessionLog = sl
	s.sessionLog = sl
	s.sessionLogPath = sl.Path()
	t.Cleanup(s.server.Close)
	return s
}

// t2Item builds one TaskItem.
func t2Item(id, summary, status string) TaskItem {
	return TaskItem{TaskID: FlexibleString(id), Summary: summary, Status: status}
}

const allDoneText = "All tasks are complete. Everything is finished."

// E1: free-text all-done while a tracked item lacks a committed conclusion →
// the framework injects the completion nudge and does NOT exit.
func TestT2_E1_AllDoneFreeTextWithoutCommit_NudgedNotExited(t *testing.T) {
	done := func(w http.ResponseWriter) {
		claudeTextStream(w, taskListResponse(allDoneText, t2Item("1", "Fix bug", "completed")), "end_turn")
	}
	script := []func(w http.ResponseWriter){
		done, // round 1: free-text all-done, nothing committed → nudge 1
		done, // round 2: still free-text all-done → nudge 2
		done, // round 3: still → nudge 3 (cap)
		done, // round 4: cap reached → the exit must now proceed normally
	}
	e := newE2EAgent(t, "sess-e1", script)
	// The session has JOINED the working-area contract (has committed at
	// least one write_task_area earlier) and has a tracked list.
	e.ag.taskAreaActive = true
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "completed"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	e.ag.TaskState.update(tasks)

	_, err := e.ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if n := e.ag.taskAreaNudges; n != 3 {
		t.Fatalf("expected 3 nudges (capped), got %d", n)
	}
	if e.callNum < 4 {
		t.Fatalf("run ended too early (callNum=%d) — the guard must continue the loop past the free-text all-done", e.callNum)
	}
	// The nudge message names the pending task_id.
	wantMsg := fmt.Sprintf(taskAreaCompletionNudge, "1")
	if n := countUserMessages(e.ag, wantMsg); n != 3 {
		t.Fatalf("expected 3 injected completion nudges naming task 1, got %d", n)
	}
	// RAW-LOG evidence (acceptance E1): the nudges are real user events in
	// the session JSONL, not just in-memory context. The log stores JSON
	// with escaped newlines, so count the stable first-line fragment of the
	// nudge (matches once per injected user event).
	e.ag.SessionLog.Sync()
	raw := mustReadFile(t, e.sessionLogPath)
	const nudgeFragment = "[system] FRAMEWORK COMPLETION CHECK: your task list still has items without a committed"
	if n := strings.Count(raw, nudgeFragment); n != 3 {
		t.Fatalf("expected 3 completion-nudge user events in the session raw log, got %d\nlog: %s", n, raw)
	}
	// The session did NOT end on the first free-text all-done: the run only
	// ended after the nudge cap (round 4). Assert the loop continued: the
	// nudges are in context and the result came from round 4.
	if got := e.callNum; got != 4 {
		t.Fatalf("expected the run to consume all 4 scripted rounds before exiting, got %d", got)
	}
}

// E2: after every tracked item is committed through write_task_area, a
// free-text all-done round ends the run normally (no nudge).
func TestT2_E2_AllCommittedThenAllDone_EndsNormally(t *testing.T) {
	script := []func(w http.ResponseWriter){
		// Round 1: commit item "1" via write_task_area.
		func(w http.ResponseWriter) { commitToolUseStream(w, "1", "item 1 resolved") },
		// Round 2: all-done free text with all-completed list — the commit
		// happened, so the guard passes and the run exits normally.
		func(w http.ResponseWriter) {
			claudeTextStream(w, taskListResponse(allDoneText, t2Item("1", "Fix bug", "completed")), "end_turn")
		},
	}
	e := newE2EAgent(t, "sess-e2", script)
	e.ag.taskAreaActive = true
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "progressing"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	e.ag.TaskState.update(tasks)

	result, err := e.ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected the all-done answer as result, got: %q", result)
	}
	if e.ag.taskAreaNudges != 0 {
		t.Fatalf("expected zero nudges (all items committed before exit), got %d", e.ag.taskAreaNudges)
	}
}

// E3: resume carry-forward — arming CarryForwardSessionID makes the first
// resumed request carry the original session's committed-conclusions block.
func TestT2_E3_ResumeCarriesForwardCommittedConclusions(t *testing.T) {
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	const origSID = "sess-original"
	// Commit conclusions under the ORIGINAL session (what the crashed run
	// recorded before dying).
	if err := commitViaAgent("", origSID, "1", "item one done fact"); err != nil {
		t.Fatalf("commit original: %v", err)
	}
	if err := commitViaAgent("", origSID, "2", "item two done fact"); err != nil {
		t.Fatalf("commit original: %v", err)
	}

	// The resumed session's server: capture the FIRST request body, then
	// reply end_turn so the run finishes.
	var firstReq bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if firstReq.Len() == 0 {
			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, r.Body); err == nil {
				firstReq = *buf
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStream(w, "resumed answer done", "end_turn")
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:                   &Context{},
		Model:                 "deepseek-v4-pro",
		MaxIterations:         4,
		Logger:                log.New(io.Discard, "", 0),
		NoEnforceMemory:       true,
		TaskState:             newTodoState(),
		CarryForwardSessionID: origSID,
	}
	// Raw-log capture: the resumed run logs its own session.
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ag.SessionLog = sl
	history := []deepseek.Message{{Role: "user", Content: "[EPIC GOAL] finish the work"}}
	if _, err := ag.Resume("", history); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	wire := firstReq.String()
	if !strings.Contains(wire, taskAreaConclusionsMessageMarker) {
		t.Fatalf("first resumed request missing the committed-conclusions marker; wire:\n%s", wire)
	}
	for _, want := range []string{"item one done fact", "item two done fact"} {
		if !strings.Contains(wire, want) {
			t.Errorf("first resumed request missing carried conclusion %q; wire:\n%s", want, wire)
		}
	}
	// RAW-LOG evidence (acceptance E3): the carry-forward block is a real
	// user event in the resumed session's JSONL before the continuation.
	sl.Sync()
	raw := mustReadFile(t, sl.Path())
	if !strings.Contains(raw, taskAreaConclusionsMessageMarker) {
		t.Fatalf("resumed session raw log missing the committed-conclusions marker; log: %s", raw)
	}
	for _, want := range []string{"item one done fact", "item two done fact"} {
		if !strings.Contains(raw, want) {
			t.Errorf("resumed session raw log missing carried conclusion %q; log: %s", want, raw)
		}
	}
}

// E6: a single-step session (no TASK_LIST ever established) with a plain
// end_turn exit — no nudge, normal exit.
func TestT2_E6_SingleStepNoList_ExitsWithoutNudge(t *testing.T) {
	answer := func(w http.ResponseWriter) {
		claudeTextStream(w, "Simple answer, no task list.", "end_turn")
	}
	e := newE2EAgent(t, "sess-e6", []func(w http.ResponseWriter){answer, answer})
	e.ag.taskAreaActive = true // even if it joined, no tracked list → exempt

	result, err := e.ag.Run("Do the simple thing")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "Simple answer") {
		t.Fatalf("expected the simple answer, got: %q", result)
	}
	if e.ag.taskAreaNudges != 0 {
		t.Fatalf("single-step session must never be nudged, got %d", e.ag.taskAreaNudges)
	}
	// bug022: a plain end_turn needs a 2nd consecutive confirmation round
	// even for a single-step session — but that is the normal continuation
	// message, NOT a framework completion nudge (taskAreaNudges==0 above).
	if e.callNum != 2 {
		t.Fatalf("expected 2 API calls (end_turn + bug022 confirmation), got %d", e.callNum)
	}
}

// Guard unit test: an all-completed tracked list whose items all have
// committed conclusions never intercepts (the completed set is authoritative).
func TestT2_Guard_AllConcluded_Passes(t *testing.T) {
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	const sid = "sess-guard-pass"
	if err := commitViaAgent("", sid, "1", "done"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	a := &Agent{
		Ctx:               &Context{},
		taskAreaActive:    true,
		autoTaskSessionID: sid,
		TaskState:         newTodoState(),
	}
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "s", "completed"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	a.TaskState.update(tasks)
	if a.pendingTaskAreaConclusionGuard() {
		t.Fatal("guard must pass when every tracked item is concluded")
	}
	if a.taskAreaNudges != 0 {
		t.Fatalf("no nudge expected, got %d", a.taskAreaNudges)
	}
}

// Guard unit test: a session that never joined (taskAreaActive=false) is
// never intercepted — legacy free-text completion stays intact.
func TestT2_Guard_NotJoined_Exempt(t *testing.T) {
	a := &Agent{
		Ctx:       &Context{},
		TaskState: newTodoState(),
	}
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "s", "completed"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	a.TaskState.update(tasks)
	if a.pendingTaskAreaConclusionGuard() {
		t.Fatal("a session that never called task(action=commit) must keep the legacy (free-text) completion contract")
	}
}

// Guard unit test: the nudge cap bounds the insistence — after
// maxTaskAreaNudges intercepts, further calls pass (anti-deadlock).
func TestT2_Guard_CapAllowsExit(t *testing.T) {
	newTestAgentEnv(t)
	tempAgentMemoryDB(t)
	const sid = "sess-guard-cap"
	a := &Agent{
		Ctx:               &Context{},
		taskAreaActive:    true,
		autoTaskSessionID: sid,
		TaskState:         newTodoState(),
	}
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "s", "progressing"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	a.TaskState.update(tasks)
	for i := 0; i < maxTaskAreaNudges; i++ {
		if !a.pendingTaskAreaConclusionGuard() {
			t.Fatalf("guard must intercept on nudge %d (pending item unconcluded)", i+1)
		}
	}
	// Cap reached: the guard now passes — the run may exit.
	if a.pendingTaskAreaConclusionGuard() {
		t.Fatal("guard must pass once the nudge cap is reached (anti-deadlock)")
	}
}

// corruptSessionConclusionsTable rebuilds the session_conclusions table with
// a schema missing the created_at/analysis columns the read SELECT depends
// on. openTasksDB still succeeds (the gopi tables exist) and tableExists
// still reports the table present, so SessionConclusions reaches its
// QueryContext/rows phase and returns a REAL error — the one path where the
// guard's `if err != nil { return false }` branch (loop.go) is reachable.
// (An unopenable DB path never reaches that branch: openTasksDB failure is
// swallowed to nil,nil inside SessionConclusions — writetaskarea.go.)
func corruptSessionConclusionsTable(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open db for corruption: %v", err)
	}
	defer db.Close()
	// Rename the healthy table out of the way, then recreate it WITHOUT the
	// created_at / analysis columns. tableExists("session_conclusions") stays
	// true, but the SELECT ... ORDER BY created_at now errors — exactly the
	// mid-query failure SessionConclusions surfaces as a non-nil error.
	if _, err := db.Exec(`ALTER TABLE session_conclusions RENAME TO session_conclusions_corrupt`); err != nil {
		t.Fatalf("rename session_conclusions: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE session_conclusions (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		task_id    TEXT NOT NULL,
		conclusion TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("recreate session_conclusions (corrupt schema): %v", err)
	}
}

// Guard fail-open test (D1): a SessionConclusions DB read error must NEVER
// wedge the agent's exit. The guard's err branch returns false (loop.go:3940
// "DB failure — fail open, never wedge the exit"), so a no-tool exit round
// proceeds to the normal completion path — no nudge is injected and the run
// ends. A healthy DB with the SAME script would nudge (the tracked item has
// no committed conclusion), which is what makes this test bite: only the DB
// fault switches the outcome to a clean pass.
func TestT2_Guard_DBFailure_FailsOpen(t *testing.T) {
	newTestAgentEnv(t)
	dbPath := tempAgentMemoryDB(t)
	// Seed a healthy DB + schema with one real committed conclusion (so the
	// corruption below has a table to mangle, and the healthy-control claim
	// in the header is grounded in the same file shape).
	const sid = "sess-guard-dbfail"
	if err := commitViaAgent("", sid, "1", "done"); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	corruptSessionConclusionsTable(t, dbPath)

	// PRECONDITION — make the test fail loudly if the corruption stops
	// producing a real error (silent nil,nil would make the pass vacuous).
	if _, err := tools.SessionConclusions(sid); err == nil {
		t.Fatalf("precondition: corrupted schema must make SessionConclusions error, got nil")
	}

	// Guard-unit level: taskAreaActive session, tracked list with a pending
	// item, but SessionConclusions errors → the guard MUST return false
	// (fail open) and inject nothing.
	a := &Agent{
		Ctx:               &Context{},
		taskAreaActive:    true,
		autoTaskSessionID: sid,
		TaskState:         newTodoState(),
	}
	tasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "completed"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	a.TaskState.update(tasks)
	if a.pendingTaskAreaConclusionGuard() {
		t.Fatal("guard must fail OPEN (return false) when SessionConclusions errors — DB failure must never wedge the exit")
	}
	if a.taskAreaNudges != 0 {
		t.Fatalf("no nudge expected on DB failure, got %d", a.taskAreaNudges)
	}

	// Loop level: a no-tool all-done round under the SAME DB fault ends
	// normally — one round, real result, no nudge, no infinite loop. (With a
	// healthy DB this exact script would be intercepted up to the nudge cap.)
	e := newE2EAgent(t, "sess-guard-dbfail-loop", []func(w http.ResponseWriter){
		func(w http.ResponseWriter) {
			claudeTextStream(w, taskListResponse(allDoneText, t2Item("1", "Fix bug", "completed")), "end_turn")
		},
	})
	// newE2EAgent re-pointed GOPI_MEMORY_DB at a FRESH temp DB — put the
	// corrupted one back so the loop-level guard hits the same DB fault.
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	e.ag.taskAreaActive = true
	loopTasks, _ := ParseTaskList(taskListResponse("", t2Item("1", "Fix bug", "completed"))) //stylegate:ignore parses a task-list response the test itself wrote; the parsed rows are the assertion, so a failure cannot be mistaken for a pass
	e.ag.TaskState.update(loopTasks)
	result, err := e.ag.Run("Fix the bug")
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result, "All tasks are complete") {
		t.Fatalf("expected the all-done answer as result, got: %q", result)
	}
	if e.ag.taskAreaNudges != 0 {
		t.Fatalf("no nudge expected on DB failure, got %d", e.ag.taskAreaNudges)
	}
	if e.callNum != 1 {
		t.Fatalf("DB fault must not wedge the exit — expected 1 API call (clean completion), got %d", e.callNum)
	}
}
