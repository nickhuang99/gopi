package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Golden tests for the `gopi session log` CONTENT views driven through the CLI's own
// dispatch (cli_log.go) inside a fixture sandbox. The session resolution path
// (ResolveSessionDir → FindSession) runs hermetically against a copy of the
// real fixture session (fc426fed) in a fresh GOPI_HOME, and the rendered
// output is compared against the SAME committed goldens the gopilog package
// owns (assertGolden — a missing golden is a failure here: recording lives in
// internal/gopilog via GOPI_GOLDEN_UPDATE=1).
//
// Two layers of golden coverage exist on purpose:
//   - internal/gopilog/log_golden_test.go calls the formatters directly
//     (renderer-level canary);
//   - these tests go through runLogSubcommand-equivalent dispatch so a future
//     change in cli_log.go flag wiring / dispatch order breaks the test here,
//     not silently in production.
//
// Views the fixture cannot exercise (search/web/job/vision/text2img/git tool
// families, real context_trim activity) are recorded as fixture gaps in
// tests/fixtures/log-stats-raw/README.md — covered here only via their empty
// states, never with fabricated content.
func TestGoldenCLILogChat(t *testing.T) {
	clearLedgerTurnCounterForGolden(t)
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--chat"}) })
	assertGolden(t, "log-chat.txt", out)
}

func TestGoldenCLILogResponse(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--response"}) })
	assertGolden(t, "log-response.txt", out)
}

func TestGoldenCLILogThinking(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--thinking"}) })
	assertGolden(t, "log-thinking.txt", out)
}

func TestGoldenCLILogSession(t *testing.T) {
	clearLedgerTurnCounterForGolden(t)
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id}) })
	assertGolden(t, "log-session.txt", out)
}

func TestGoldenCLILogTodo(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--todo"}) })
	assertGolden(t, "log-todo.txt", out)
}

// TestGoldenCLILogMemory: --memory is the family shortcut for the six memory
// tools (memory_search/store/feedback/update/delete/list). The fixture made 3
// real memory calls.
func TestGoldenCLILogMemory(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--memory"}) })
	assertGolden(t, "log-memory-filter.txt", out)
}

// TestGoldenCLILogFilterWriteTaskArea: --filter with a plain tool name.
func TestGoldenCLILogFilterWriteTaskArea(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--filter", "write_task_area"}) })
	assertGolden(t, "log-write-task-area-filter.txt", out)
}

func TestGoldenCLILogGrepContent(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--grep", "seine"}) })
	assertGolden(t, "log-grep-content.txt", out)
}

// TestGoldenCLILogGrepScopedMemory: --grep scoped with --memory searches only
// memory tool calls (the dispatch runs grep before the family filter).
func TestGoldenCLILogGrepScopedMemory(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--grep", "seine", "--memory"}) })
	assertGolden(t, "log-grep-scoped-memory.txt", out)
}

func TestGoldenCLILogGrepNoMatch(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--grep", "zzz-no-such-content-zzz"}) })
	assertGolden(t, "log-grep-nomatch.txt", out)
}

func TestGoldenCLILogContextTrim(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--context-trim"}) })
	assertGolden(t, "log-context-trim.txt", out)
}

// TestGoldenCLILogSearchEmptyState: --search over the fixture (no search tool
// calls) renders the filter view's empty state.
func TestGoldenCLILogSearchEmptyState(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--search"}) })
	assertGolden(t, "log-search-filter.txt", out)
}

// TestGoldenCLILogJSONIsByteExact: --json passthrough through the CLI equals
// the fixture file's own bytes.
func TestGoldenCLILogJSONIsByteExact(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--json"}) })
	want, err := os.ReadFile(filepath.Join(statsFixtureRootRel, "sessions", id+".jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if out != string(want) {
		t.Errorf("--json passthrough is not the fixture's bytes (%d vs %d bytes)", len(out), len(want))
	}
}

// TestGoldenCLILogRequestSystem: `gopi session log <id> --request` pairs the session
// with its raw HTTP log inside the sandbox and prints the verbatim system
// prompt.
func TestGoldenCLILogRequestSystem(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--request"}) })
	assertGolden(t, "log-request-system.txt", out)
}

// TestGoldenCLILogRequestJSON: `gopi session log <id> --request --json` prints the
// full first request body as indented JSON.
func TestGoldenCLILogRequestJSON(t *testing.T) {
	id := statsFixtureSandbox(t)
	out := captureStdout(t, func() { runLogSubcommand([]string{id, "--request", "--json"}) })
	assertGolden(t, "log-request-json.json", out)
}
