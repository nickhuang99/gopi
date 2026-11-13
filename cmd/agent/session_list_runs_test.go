package main

// session_list_runs_test.go — THE MERGED LISTING AND THE RUN-AWARE SHOW PAGE.
//
// THE DEFECT THESE PIN (measured on this host 2026-10-28). `gopi session list`
// answered 33 rows because it read ONE table (`chats`), while the sessions tree
// held 2982 directories and ~1800 run logs: every TASK RUN — which writes a
// session log without registering a chat row — was invisible, and there was no
// verb that could tell an operator what a given task run was doing or where.
//
// WHAT IS ASSERTED, each through the REAL run functions the dispatcher calls
// (runSessionList / runSessionShow), never through a helper the test invented:
//
//	(1) the default view lists BOTH species (a chat row AND a run row);
//	(2) --kind narrows to each species, and the view DISCLOSES what it dropped;
//	(3) the run row carries the pid that wrote it and the liveness verdict;
//	(4) `session show <task-id>` prints the runtime facts (pid, workdir, branch,
//	    log path, state) for a session that has NO chats row;
//	(5) `session list --json` carries `kind` and `pid` on every row;
//	(6) a typo'd --kind is a NAMED refusal, never an empty list (Rule 15).
//
// THE RED PROOF for (1)/(2) is a one-line revert of the source this file tests:
// point runSessionList back at tools.ChatList (the pre-fix reader) and every
// assertion about a run row goes red while the chat assertions stay green —
// which is exactly the defect's shape ("the table works; the runs are missing").

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procstat"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

const (
	// sessionRunsTestRunID is the run directory a fixture writes. It is a TASK
	// id on purpose: the acceptance criterion is that a task id reaches the run.
	sessionRunsTestRunID = "run-fixture-01a17fae-bb61-7b41-a204-dd0500000001"
	// sessionRunsTestChatID is the chat row's id — a different id space on
	// purpose, so "both species are listed" cannot pass on one row.
	sessionRunsTestChatID = "chat-fixture-1111-2222-3333-444455556666"
	// sessionRunsTestTaskID is the ledger row the run records as its own.
	sessionRunsTestTaskID = "01a17fae-dead-beef-0000-000000000001"
)

// writeRunFixture writes one run directory the way a real run does — a
// `session_start` first line carrying the pid, the workdir and the task id, and
// a `session_end` last line — and returns the run log's path. It builds the path
// from the SAME profile the pinned GOPI_HOME names (config's own directory
// names, never a re-typed ".gopi/sessions"), so the fixture lands where the
// reader looks.
func writeRunFixture(t *testing.T, runID string, pid int, ended bool) string {
	t.Helper()
	home := os.Getenv("GOPI_HOME")
	if home == "" {
		t.Fatal("GOPI_HOME is not pinned — call sessionChatEnv first")
	}
	dir := filepath.Join(home, config.ProfileDirName, config.SessionsDirName, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	workdir := filepath.Join(home, "workdir-"+runID)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"session_start","timestamp":"2026-10-28T07:26:02Z","model":"deepseek-flash","sessionId":"%s","workdir":"%s","pid":%d,"taskId":"%s"}`+"\n",
		runID, workdir, pid, sessionRunsTestTaskID)
	if ended {
		fmt.Fprintf(&b, `{"type":"session_end","timestamp":"2026-10-28T07:30:00Z","total_iterations":3}`+"\n")
	}
	path := filepath.Join(dir, runID+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write run log: %v", err)
	}
	return path
}

// TestSessionListMergesChatRowsAndRunDirs is assertion (1) and (2): ONE view,
// BOTH sources, and a filter that narrows to each without losing the disclosure.
func TestSessionListMergesChatRowsAndRunDirs(t *testing.T) {
	sessionChatEnv(t)
	sessionSeedRow(t, sessionRunsTestChatID, writeCLISessionJSONL(t, sessionRunsTestChatID))
	// os.Getpid() makes the liveness verdict DETERMINISTIC: this process is
	// alive, so the row must say so (a made-up pid would make the assertion a
	// coin flip).
	writeRunFixture(t, sessionRunsTestRunID, os.Getpid(), false)

	def := captureStdout(t, func() {
		if code := runSessionList(nil); code != 0 {
			t.Fatalf("runSessionList (default) = %d, want 0", code)
		}
	})
	if !strings.Contains(def, sessionRunsTestChatID) {
		t.Errorf("the default view does not list the chat row it is built on:\n%s", def)
	}
	if !strings.Contains(def, sessionRunsTestRunID) {
		t.Errorf("THE DEFECT: a TASK RUN directory is not in the default view — the tree is not being read:\n%s", def)
	}
	if !strings.Contains(def, tools.SessionKindRun) {
		t.Errorf("the run row does not name its kind %q:\n%s", tools.SessionKindRun, def)
	}

	// (2) THE FILTERS, each narrowing to ONE species.
	runs := captureStdout(t, func() {
		if code := runSessionList([]string{sessionKindFlag, tools.SessionKindRun}); code != 0 {
			t.Fatalf("runSessionList --kind run = %d, want 0", code)
		}
	})
	if !strings.Contains(runs, sessionRunsTestRunID) {
		t.Errorf("--kind %s dropped the run row:\n%s", tools.SessionKindRun, runs)
	}
	if strings.Contains(runs, sessionRunsTestChatID) {
		t.Errorf("--kind %s kept a chat row:\n%s", tools.SessionKindRun, runs)
	}
	chats := captureStdout(t, func() {
		if code := runSessionList([]string{sessionKindFlag, tools.SessionKindChat}); code != 0 {
			t.Fatalf("runSessionList --kind chat = %d, want 0", code)
		}
	})
	if !strings.Contains(chats, sessionRunsTestChatID) {
		t.Errorf("--kind %s dropped the chat row:\n%s", tools.SessionKindChat, chats)
	}
	if strings.Contains(chats, sessionRunsTestRunID) {
		t.Errorf("--kind %s kept a run row:\n%s", tools.SessionKindChat, chats)
	}
	// THE DISCLOSURE: a narrowed view says, in numbers, what it hid (Rule 15).
	if !strings.Contains(chats, sessionFilteredPrefix) {
		t.Errorf("a --kind-narrowed view does not disclose what it filtered out (want a line opening %q):\n%s",
			sessionFilteredPrefix, chats)
	}

	// A TYPO IS A NAMED REFUSAL, never an empty list — the spelling is validated
	// against tools.SessionKinds, which is also what the help page teaches.
	if code := runSessionList([]string{sessionKindFlag, "runs"}); code != 1 {
		t.Errorf("runSessionList --kind runs (a typo) = %d, want 1", code)
	}
	// An UNKNOWN flag is still refused by the closed vocabulary.
	if code := runSessionList([]string{sessionKindFlag + "-nope"}); code != 1 {
		t.Errorf("runSessionList with an unknown flag = %d, want 1", code)
	}
}

// TestSessionShowPrintsRunRuntimeFacts is assertion (3) and (4): the page that
// exists to explain a session must explain a TASK RUN — the pid that wrote it,
// whether that process is alive, where it ran, on which branch, and the logs.
func TestSessionShowPrintsRunRuntimeFacts(t *testing.T) {
	sessionChatEnv(t)
	logPath := writeRunFixture(t, sessionRunsTestRunID, os.Getpid(), false)

	out := captureStdout(t, func() {
		if code := runSessionShow([]string{sessionRunsTestRunID}); code != 0 {
			t.Fatalf("runSessionShow(%s) = %d, want 0 — a task run has no chats row, and the page must still resolve it",
				sessionRunsTestRunID, code)
		}
	})
	for _, want := range []struct{ label, value string }{
		{"pid", fmt.Sprint(os.Getpid())},
		{"workdir", "workdir-" + sessionRunsTestRunID},
		{"state", tools.SessionRunStateRunning},
		{"task id", sessionRunsTestTaskID},
		{"log path", logPath},
	} {
		if !strings.Contains(out, want.value) {
			t.Errorf("the run page does not print %s (want %q):\n%s", want.label, want.value, out)
		}
	}
	// The liveness verdict is procstat's own word, so the page cannot invent one:
	// this process wrote the fixture's stamp, so the verdict must be Alive.
	if !strings.Contains(out, string(procstat.VerdictAlive)) {
		t.Errorf("the run page does not print the liveness verdict (%q):\n%s", procstat.VerdictAlive, out)
	}
	// A run's page must NOT pretend to be a chat: the chat accounting word has no
	// place on a run (there is no chats row behind it to count).
	if strings.Contains(out, sessionevent.CounterUnitTurn) {
		t.Errorf("the run page prints a chat-only turn count:\n%s", out)
	}

	// The CHAT page still works, and now carries the runtime block too — the
	// two species read as one page shape, and neither lost its own facts.
	sessionSeedRow(t, sessionRunsTestChatID, writeCLISessionJSONL(t, sessionRunsTestChatID))
	chat := captureStdout(t, func() {
		if code := runSessionShow([]string{sessionRunsTestChatID}); code != 0 {
			t.Fatalf("runSessionShow(%s) = %d, want 0", sessionRunsTestChatID, code)
		}
	})
	if !strings.Contains(chat, "  kind ") {
		t.Errorf("the chat page lost the runtime block:\n%s", chat)
	}
}

// TestSessionListJSONCarriesKindAndPID is assertion (5): a script must be able to
// tell the species apart and reach a running session's process WITHOUT parsing a
// human table.
func TestSessionListJSONCarriesKindAndPID(t *testing.T) {
	sessionChatEnv(t)
	sessionSeedRow(t, sessionRunsTestChatID, writeCLISessionJSONL(t, sessionRunsTestChatID))
	writeRunFixture(t, sessionRunsTestRunID, os.Getpid(), false)

	var rows []sessionSourceJSONRow
	if err := json.Unmarshal([]byte(captureStdout(t, func() {
		if code := runSessionList([]string{sessionJSONFlag}); code != 0 {
			t.Fatalf("runSessionList --json = %d, want 0", code)
		}
	})), &rows); err != nil {
		t.Fatalf("the JSON face is not one decodable document: %v", err)
	}
	byID := map[string]sessionSourceJSONRow{}
	for _, r := range rows {
		byID[r.SessionID] = r
	}
	runRow, ok := byID[sessionRunsTestRunID]
	if !ok {
		t.Fatalf("the run row is missing from the JSON face (rows: %v)", byID)
	}
	if runRow.Kind != tools.SessionKindRun {
		t.Errorf("run row kind = %q, want %q", runRow.Kind, tools.SessionKindRun)
	}
	if runRow.PID != os.Getpid() {
		t.Errorf("run row pid = %d, want the stamp %d", runRow.PID, os.Getpid())
	}
	if runRow.Alive == "" {
		t.Error("run row carries no liveness word — a pid without a verdict is half the fact")
	}
	if runRow.LogPath == "" || runRow.Workdir == "" {
		t.Errorf("run row carries no paths: log_path=%q workdir=%q", runRow.LogPath, runRow.Workdir)
	}
	chatRow, ok := byID[sessionRunsTestChatID]
	if !ok {
		t.Fatalf("the chat row is missing from the JSON face (rows: %v)", byID)
	}
	if chatRow.Kind != tools.SessionKindChat {
		t.Errorf("chat row kind = %q, want %q", chatRow.Kind, tools.SessionKindChat)
	}
}
