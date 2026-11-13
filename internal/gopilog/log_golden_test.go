package gopilog

import (
	"os"
	"testing"
)

// Golden tests for the `gopi session log` CONTENT views (--chat / --response /
// --thinking / --request / --filter / --grep / --todo / --context-trim /
// --json and the default session view). The stats/raw goldens
// (stats_golden_test.go) cover `gopi session stats` / `gopi session raw`; this file extends
// the same regression canary to the log reader's rendered content.
//
// Input is the SAME real fixture as the stats goldens — a real session
// (fc426fed, 9 rounds / 43 events: memory_search/memory_feedback/
// write_task_area tool calls, thinking blocks, text responses, task lists)
// plus its paired raw HTTP log. No constructed mocks: every view below renders
// real logged content byte-exactly.
//
// Fixture coverage notes (views the fixture CANNOT exercise are recorded, not
// faked — see README in tests/fixtures/log-stats-raw):
//   - search/web/job/context/vision/text2img/git families: the fixture session
//     contains no such tool calls → the filter view's empty state is what is
//     golden-tested (log-search-filter.txt), NOT a fabricated match.
//   - context_trim: no context_trim calls in the fixture → empty state
//     (log-context-trim.txt).
//   - A fixture exercising those families would need a real session that used
//     them (a --search/--web/--job/--context-trim-heavy run) — out of scope of
//     the current fixture, listed as a fixture gap.
//
// Re-record after an INTENTIONAL output change with
//	GOPI_GOLDEN_UPDATE=1 go test ./internal/gopilog/ -run TestGoldenLog
// and review the diff of tests/fixtures/log-stats-raw/golden/log-*.txt like
// code (same mechanism as the stats goldens — no second framework).

// fixtureSession parses the fixture session JSONL once per golden test.
func fixtureSession(t *testing.T) *Session {
	t.Helper()
	s, err := ParseFile(fixturePath(t, "sessions/"+fixtureSessionID+".jsonl"))
	if err != nil {
		t.Fatalf("ParseFile(fixture session): %v", err)
	}
	return s
}

// memoryFilterSet mirrors toolFamilies["memory"] in cmd/agent/cli_log.go —
// the exact --memory filter the CLI resolves. It carries the merged single
// `memory` tool name plus the pre-merge archival memory_* wire names (old
// sessions logged under the retired names must still filter).
func memoryFilterSet() map[string]bool {
	return map[string]bool{
		"memory":          true,
		"memory_search":   true,
		"memory_store":    true,
		"memory_feedback": true,
		"memory_update":   true,
		"memory_delete":   true,
		"memory_list":     true,
	}
}

// searchFilterSet mirrors toolFamilies["search"] — a family with NO matches in
// the fixture, golden-testing the empty state.
func searchFilterSet() map[string]bool {
	return map[string]bool{"code_search": true, "keyword_search": true}
}

func TestGoldenLogSession(t *testing.T) {
	compareGolden(t, "log-session.txt", FormatSession(fixtureSession(t)))
}

func TestGoldenLogChat(t *testing.T) {
	compareGolden(t, "log-chat.txt", FormatChat(fixtureSession(t)))
}

func TestGoldenLogResponse(t *testing.T) {
	compareGolden(t, "log-response.txt", FormatResponse(fixtureSession(t), false))
}

func TestGoldenLogResponseText(t *testing.T) {
	compareGolden(t, "log-response-text.txt", FormatResponse(fixtureSession(t), true))
}

func TestGoldenLogThinking(t *testing.T) {
	compareGolden(t, "log-thinking.txt", FormatThinking(fixtureSession(t), false))
}

func TestGoldenLogThinkingText(t *testing.T) {
	compareGolden(t, "log-thinking-text.txt", FormatThinking(fixtureSession(t), true))
}

// TestGoldenLogTodo covers `gopi session log <id> --todo` — the fixture session ran a
// real 5-item task list ([TASK_LIST_BEGIN]…[TASK_LIST_END]) through
// write_task_area, so the todo view has real items to render.
func TestGoldenLogTodo(t *testing.T) {
	compareGolden(t, "log-todo.txt", FormatTodo(fixtureSession(t)))
}

// TestGoldenLogMemoryFilter covers `gopi session log <id> --memory` (family = the six
// memory tools). The fixture made 3 real memory calls (memory_search ×1,
// memory_feedback ×2), each with a full input + matched tool_result.
func TestGoldenLogMemoryFilter(t *testing.T) {
	compareGolden(t, "log-memory-filter.txt", FormatToolFilter(fixtureSession(t), memoryFilterSet()))
}

// TestGoldenLogWriteTaskAreaFilter covers `gopi session log <id> --filter write_task_area`
// — the fixture committed 5 task items with write_task_area, one per iteration.
func TestGoldenLogWriteTaskAreaFilter(t *testing.T) {
	names := map[string]bool{"write_task_area": true}
	compareGolden(t, "log-write-task-area-filter.txt", FormatToolFilter(fixtureSession(t), names))
}

// TestGoldenLogSearchFilterEmptyState: the fixture session made no
// code_search/keyword_search calls, so the --search view renders its empty
// state. Golden-testing the empty state is deliberate — it pins the
// "no matching tool calls" contract for families the fixture cannot cover
// (see the fixture-coverage note at the top of this file).
func TestGoldenLogSearchFilterEmptyState(t *testing.T) {
	compareGolden(t, "log-search-filter.txt", FormatToolFilter(fixtureSession(t), searchFilterSet()))
}

// TestGoldenLogContextTrimEmptyState: the fixture session has no context_trim
// activity → the --context-trim view renders its empty state.
func TestGoldenLogContextTrimEmptyState(t *testing.T) {
	compareGolden(t, "log-context-trim.txt", FormatContextTrim(fixtureSession(t)))
}

// TestGoldenLogGrepContent: `gopi session log <id> --grep seine` searches ALL content
// (tool calls, text, thinking). "Seine" appears in the session's memory
// results and final-answer text — real cross-block matches.
func TestGoldenLogGrepContent(t *testing.T) {
	compareGolden(t, "log-grep-content.txt", FormatGrep(fixtureSession(t), "seine", nil, false))
}

// TestGoldenLogGrepScopedTool: --grep scoped to the memory family searches
// only memory tool calls (results + args), the log-side code_search analogue.
func TestGoldenLogGrepScopedTool(t *testing.T) {
	compareGolden(t, "log-grep-scoped-memory.txt", FormatGrep(fixtureSession(t), "seine", memoryFilterSet(), false))
}

// TestGoldenLogGrepNoMatch pins the no-match contract on a real session: a
// query that matches nothing in this fixture renders the "no entries" line.
func TestGoldenLogGrepNoMatch(t *testing.T) {
	compareGolden(t, "log-grep-nomatch.txt", FormatGrep(fixtureSession(t), "zzz-no-such-content-zzz", nil, false))
}

// TestGoldenLogRequestSystem covers `gopi session log <id> --request` — the verbatim
// system prompt of the session's first API request, read from the paired raw
// HTTP log.
func TestGoldenLogRequestSystem(t *testing.T) {
	p, err := ParseFirstRequest(fixturePath(t, "raw/"+fixtureRawLogName))
	if err != nil {
		t.Fatalf("ParseFirstRequest(fixture raw log): %v", err)
	}
	compareGolden(t, "log-request-system.txt", FormatRequestSystem(p, fixtureRawLogName, false))
}

// TestGoldenLogRequestJSON covers `gopi session log <id> --request --json` — the full
// first request body as indented JSON.
func TestGoldenLogRequestJSON(t *testing.T) {
	p, err := ParseFirstRequest(fixturePath(t, "raw/"+fixtureRawLogName))
	if err != nil {
		t.Fatalf("ParseFirstRequest(fixture raw log): %v", err)
	}
	compareGolden(t, "log-request-json.json", FormatRequestJSON(p))
}

// TestGoldenLogJSONIsByteExact: `gopi session log <id> --json` is a raw passthrough —
// its output must be the fixture file's own bytes (compared against the file
// itself, not a snapshot — same pattern as TestGoldenRawRequestIsByteExact).
func TestGoldenLogJSONIsByteExact(t *testing.T) {
	path := fixturePath(t, "sessions/"+fixtureSessionID+".jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	out, err := FormatJSONL(path)
	if err != nil {
		t.Fatalf("FormatJSONL: %v", err)
	}
	if out != string(raw) {
		t.Errorf("--json passthrough is not the fixture's bytes (%d vs %d bytes)", len(out), len(raw))
	}
}
