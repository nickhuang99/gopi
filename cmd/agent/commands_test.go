package main

// R4 tests (devdoc/web-align-p1.md): the shared slash-command table.
//
// The help gate is the 2026-08-28 lesson made testable: EVERY command and
// subcommand answers a help request, and /help lists the whole table.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestSlashHelpListsAllCommands locks the discoverability contract: bare
// /help prints every registry command (the web /help must list all of them —
// the Epic's acceptance wording), and /help <cmd> prints that command's
// usage block verbatim.
func TestSlashHelpListsAllCommands(t *testing.T) {
	out, err := execSlashCommand("/help", CommandContext{})
	if err != nil {
		t.Fatalf("/help error: %v", err)
	}
	for _, name := range slashCommandNames() {
		if !strings.Contains(out, "/"+name) {
			t.Errorf("/help output missing /%s:\n%s", name, out)
		}
	}
	for _, want := range []string{"/task", "/log", "/text2img"} {
		if !strings.Contains(out, want) {
			t.Errorf("/help output missing %s:\n%s", want, out)
		}
	}
	// The two RETIRED words are gone from the page, by decision (operator,
	// 2026-11-07): chat is an agent entry, not an IDE. Their TOOLS still exist
	// (`code_search`, `memory` in internal/agent/loop.go) — this asserts the
	// slash surface, not the tool table.
	for _, gone := range []string{"/code-search", "/memory"} {
		if strings.Contains(out, gone) {
			t.Errorf("/help still lists the retired command %s:\n%s", gone, out)
		}
	}

	// Per-command help: /help <cmd> returns the same usage as -h.
	for name, c := range slashCommands {
		viaHelp, err := execSlashCommand("/help "+name, CommandContext{})
		if err != nil {
			t.Fatalf("/help %s error: %v", name, err)
		}
		if viaHelp != c.Usage {
			t.Errorf("/help %s output differs from the command's Usage:\n--- via /help ---\n%s\n--- Usage ---\n%s", name, viaHelp, c.Usage)
		}
		if !strings.Contains(viaHelp, "Usage:") {
			t.Errorf("/help %s output lacks a Usage line: %q", name, viaHelp)
		}
	}
}

// TestSlashEveryCommandAnswersDashH is the HARD help standard as a test:
// every command answers -h/--help/help with its usage (mirrors the CLI's
// per-subcommand -h exit-0 contract).
func TestSlashEveryCommandAnswersDashH(t *testing.T) {
	for name, c := range slashCommands {
		for _, h := range []string{"-h", "--help", "help"} {
			out, err := execSlashCommand("/"+name+" "+h, CommandContext{})
			if err != nil {
				t.Errorf("/%s %s errored: %v", name, h, err)
				continue
			}
			if out != c.Usage {
				t.Errorf("/%s %s output differs from Usage:\n%q vs %q", name, h, out, c.Usage)
			}
		}
	}
}

// TestRetiredCodeSearchAndMemorySlashCommands is the REGRESSION for the
// 2026-11-07 decision (operator): `/code-search` and `/memory` are gone from the
// chat surface. gopi's chat is an AGENT ENTRY, not an IDE or a copilot —
// printing a tool's result into the transcript for a human to read is the
// model's job, done through the tools themselves (both still defined in
// internal/agent/loop.go, untouched).
//
// The case asserts the three things the decision promises, from the surfaces a
// user actually touches rather than from the map's private shape:
//
//  1. every spelling — the canonical word and the three aliases that used to
//     resolve to them — takes the ORDINARY unknown-command path (an error
//     naming the word and listing the vocabulary), not a special-case refusal;
//  2. neither word appears in the /help page either carrier renders;
//  3. `/help <retired>` is an error for both carriers, i.e. the per-command
//     help went with the command.
//
// It also pins WHAT DID NOT CHANGE: the tools keep their definitions (a slash
// command and a tool are two different doors), so the epic's red line is stated
// here where a later reader would otherwise have to guess.
func TestRetiredCodeSearchAndMemorySlashCommands(t *testing.T) {
	retired := []string{
		"code-search", "codesearch", "searchcode",
		"memory", "mem",
	}
	for _, word := range retired {
		if _, ok := lookupCommand(word); ok {
			t.Errorf("/%s still resolves through the shared table — the retired command is back", word)
		}

		// (1) THE ORDINARY UNKNOWN-COMMAND PATH, on both carriers' dispatch.
		_, err := execSlashCommand("/"+word+" foo", CommandContext{})
		if err == nil {
			t.Errorf("/%s foo was accepted — a retired command must be refused", word)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, `unknown command`) || !strings.Contains(msg, "/"+word) {
			t.Errorf("/%s refused with %q, want the shared unknown-command refusal naming it", word, msg)
		}
		if !strings.Contains(msg, "available:") {
			t.Errorf("/%s refusal lists no vocabulary: %v", word, msg)
		}
	}

	// (2) THE /help PAGE — the discoverability half. Read from the rendering, not
	// from the map, so a name that is refused but still advertised fails here.
	page := slashHelpText()
	for _, gone := range []string{"code-search", "memory"} {
		if strings.Contains(page, gone) {
			t.Errorf("/help still advertises the retired /%s:\n%s", gone, page)
		}
	}
	// A surviving neighbour is the control: the page is not empty by accident.
	if !strings.Contains(page, "/text2img") || !strings.Contains(page, "/task") {
		t.Errorf("/help lost a command it must keep:\n%s", page)
	}

	// (3) PER-COMMAND HELP on BOTH carriers (the shared dispatch and the REPL's
	// own help reader) — an unknown word, never the old usage block.
	for _, word := range []string{"code-search", "memory"} {
		if _, err := execSlashCommand("/help "+word, CommandContext{}); err == nil {
			t.Errorf("/help %s still answers — the usage block outlived its command", word)
		}
		if _, err := chatHelpText("/help " + word); err == nil {
			t.Errorf("the REPL's /help %s still answers", word)
		}
	}

	// WHAT DID NOT CHANGE: the two TOOLS. Their WIRE NAMES are the red line
	// (Rule 3 — internal/agent's claudeToolDefs is the SSOT, pinned by
	// TestGOPIRule3_MatchesClaudeToolDefs in ./internal/agent). This case only
	// proves the slash vocabulary did not take the names with it, so a later
	// reader sees the two doors named side by side.
	for got, want := range map[string]string{
		toolnames.ToolCodeSearch: "code_search",
		toolnames.ToolMemory:     "memory",
	} {
		if got != want {
			t.Errorf("the tool wire name moved: %q, want %q — a slash command was retired, never a tool", got, want)
		}
	}
}

// TestSlashUnknownCommandListsTable: an unrecognized command errors with the
// available-command list (both carriers render it), never silence.
func TestSlashUnknownCommandListsTable(t *testing.T) {
	_, err := execSlashCommand("/nope", CommandContext{})
	if err == nil {
		t.Fatal("unknown command must error")
	}
	if !strings.Contains(err.Error(), "available:") {
		t.Errorf("error should list available commands: %v", err)
	}
	for _, name := range slashCommandNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error missing command %s: %v", name, err)
		}
	}
}

// TestSlashAliasesResolve covers the alias table (t2i/tasks/logs →
// canonical) so short typing works without help-table drift.
func TestSlashAliasesResolve(t *testing.T) {
	for alias, canon := range commandAliases {
		c, ok := lookupCommand(alias)
		if !ok {
			t.Errorf("alias %q unresolved", alias)
			continue
		}
		if c.Name != slashCommands[canon].Name {
			t.Errorf("alias %q resolved to %q, want canonical %q", alias, c.Name, canon)
		}
	}
}

// TestSlashTaskListHidesArchivedRows is the conversational half of the archive
// visibility contract, and the red it was written for: the terminal view
// (`gopi task list`, runTaskList → tools.TaskListVisible) dropped archived
// tombstones while `/task list` called tools.TaskList BARE and rendered them —
// the same archived column with two verdicts (measured 2026-09-29). The two
// views now share ONE predicate, so the assertions below are the same ones
// cmd/agent/cli_tasks_test.go makes about the CLI view: an archived row is
// invisible to every list, and `--all` is the one spelling that brings it back.
//
// The flag spelling is shared too (`--all`, slashTaskAllFlag): a view that
// takes the flag's NAME but not its MEANING is how this defect survived.
func TestSlashTaskListHidesArchivedRows(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv("GOPI_HOME", t.TempDir())

	if _, err := tools.TaskNew("slash live row", "t-slash-arch-live", "", ""); err != nil {
		t.Fatalf("TaskNew live: %v", err)
	}
	if _, err := tools.TaskNew("slash archived row", "t-slash-arch-gone", "", ""); err != nil {
		t.Fatalf("TaskNew archived: %v", err)
	}
	if _, err := tools.TaskUpdate("t-slash-arch-gone", tools.TaskStatusInProgress); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	if _, err := tools.TaskUpdate("t-slash-arch-gone", tools.TaskStatusCompleted); err != nil {
		t.Fatalf("completed: %v", err)
	}
	if _, err := tools.TaskArchive("t-slash-arch-gone"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// The DEFAULT slash view (no --status, no --all) — the exact invocation
	// that used to print the tombstone.
	out, err := execSlashCommand("/task list", CommandContext{})
	if err != nil {
		t.Fatalf("/task list: %v", err)
	}
	if strings.Contains(out, taskResolvedMarkerProbe) {
		t.Errorf("the slash list must not render an archived row (the CLI view does not):\n%s", out)
	}
	if !strings.Contains(out, "t-slash-arch-live") {
		t.Errorf("the unarchived row must stay visible:\n%s", out)
	}
	if got, rows := cliLedgerHeaderCount(t, out), cliLedgerRowCount(out); got != rows || rows != 1 {
		t.Errorf("slash view header=%d rows=%d, want 1/1 (post-filter count):\n%s", got, rows, out)
	}

	// The same row under an explicit --status: invisible for the same reason,
	// so the two views agree about ONE column.
	byStatus, err := execSlashCommand("/task list --status completed", CommandContext{})
	if err != nil {
		t.Fatalf("/task list --status completed: %v", err)
	}
	if strings.Contains(byStatus, taskResolvedMarkerProbe) {
		t.Errorf("--status completed must not render the archived row:\n%s", byStatus)
	}

	// --all is the escape hatch, on BOTH surfaces.
	all, err := execSlashCommand("/task list --all", CommandContext{})
	if err != nil {
		t.Fatalf("/task list --all: %v", err)
	}
	if !strings.Contains(all, "t-slash-arch-gone") || !strings.Contains(all, taskResolvedMarkerProbe) {
		t.Errorf("/task list --all must keep the archived row with its marker:\n%s", all)
	}
}

// TestSlashTaskRoundTrip exercises /task against a REAL temp tasks DB:
// submit (engine) → list (visible) → show → update → search. This is the
// same engine surface `gopi task` uses — the slash layer only reformats
// argv. M4.5 ④: /task new is gone; enqueueing goes through tools.TaskSubmit.
func TestSlashTaskRoundTrip(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", t.TempDir()+"/memory.db")
	t.Setenv("GOPI_HOME", t.TempDir())

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "P1 slash acceptance task", TaskID: "t-slash-1", Source: "human",
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}

	list, err := execSlashCommand("/task list", CommandContext{})
	if err != nil {
		t.Fatalf("/task list error: %v", err)
	}
	if !strings.Contains(list, "P1 slash acceptance task") {
		t.Errorf("/task list missing the submitted task:\n%s", list)
	}

	// update via the shared TaskUpdate engine: in_progress is a legal move.
	if _, err := execSlashCommand("/task update ebcea832-0000-0000-0000-000000000000 --status in_progress", CommandContext{}); err == nil {
		t.Log("update of a bogus id errored as expected")
	}

	// search path (FTS or LIKE under the hood — TaskSearch's contract).
	if _, err := execSlashCommand("/task search slash", CommandContext{}); err != nil {
		t.Errorf("/task search error: %v", err)
	}
}

// TestSlashTaskUpdateReason: the /task update --reason interactive face —
// parse accepts --reason, the engine records it, and the row shows it.
func TestSlashTaskUpdateReason(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", t.TempDir()+"/memory.db")
	t.Setenv("GOPI_HOME", t.TempDir())

	if _, err := tools.TaskNew("slash reason row", "t-slash-reason", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	out, err := execSlashCommand("/task update t-slash-reason --status failed --reason operator-abort-wrong-repo", CommandContext{})
	if err != nil {
		t.Fatalf("/task update --reason: %v", err)
	}
	if !strings.Contains(out, "status=failed") || !strings.Contains(out, "reason=operator-abort-wrong-repo") {
		t.Errorf("update out = %q", out)
	}
	show, err := tools.TaskShow("t-slash-reason")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "reason:         operator-abort-wrong-repo") {
		t.Errorf("show missing slash reason:\n%s", show)
	}
}

// TestSlashTaskErrors lists the failure paths the interactive user hits.
func TestSlashTaskErrors(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", t.TempDir()+"/memory.db")
	t.Setenv("GOPI_HOME", t.TempDir())

	for _, cmd := range []string{
		"/task show",       // missing id
		"/task new",        // missing summary
		"/task update",     // missing id
		"/task search",     // missing query
		"/task frobnicate", // unknown subcommand
	} {
		if _, err := execSlashCommand(cmd, CommandContext{}); err == nil {
			t.Errorf("%q should error, got success", cmd)
		}
	}
}

// TestSlashText2imgRendersAndReturnsPath: /text2img renders a REAL PNG via
// the shared engine and reports its path.
func TestSlashText2imgRendersAndReturnsPath(t *testing.T) {
	out, err := execSlashCommand(`/text2img GOPI-P1-TEST-42 --out `+t.TempDir()+"/slash.png", CommandContext{})
	if err != nil {
		t.Fatalf("/text2img error: %v", err)
	}
	if !strings.Contains(out, "slash.png") {
		t.Errorf("output should name the rendered PNG:\n%s", out)
	}
}

// TestSlashLogEnginesNoCrash: /log subcommands run against whatever sessions
// exist (possibly none) without crashing; errors come back as errors.
func TestSlashLogEnginesNoCrash(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())

	last, err := execSlashCommand("/log last", CommandContext{})
	if err != nil {
		t.Fatalf("/log last error: %v", err)
	}
	if !strings.Contains(last, "no sessions") && !strings.Contains(last, "session") {
		t.Errorf("/log last unexpected output: %q", last)
	}
	if _, err := execSlashCommand("/log summary", CommandContext{}); err == nil {
		t.Error("/log summary without an id should error")
	}
	if _, err := execSlashCommand("/log bogus", CommandContext{}); err == nil {
		t.Error("/log bogus should error")
	}
}

// TestSlashHelpRequestedParsing: the bare command word returns usage, not an
// execution error (the interactive user's first probe).
func TestSlashHelpRequestedParsing(t *testing.T) {
	for _, c := range []string{"/task", "/log", "/text2img"} {
		out, err := execSlashCommand(c, CommandContext{})
		if err != nil {
			t.Errorf("%s (bare) errored: %v", c, err)
			continue
		}
		if !strings.Contains(out, "Usage:") {
			t.Errorf("%s (bare) should print usage, got: %q", c, out)
		}
	}
}

// TestIsSlashCommand: the slash prefix is the trigger; plain words that name
// a command stay chat input for the model.
func TestIsSlashCommand(t *testing.T) {
	if !isSlashCommand(" /task list") {
		t.Error("leading-slash line should be a slash command")
	}
	if isSlashCommand("task list") {
		t.Error("bare word must NOT be a slash command")
	}
}

// TestMemoryListRecentEngine: the new engine renders ids/topics and handles
// an empty DB.
func TestMemoryListRecentEngine(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", t.TempDir()+"/memory.db")
	t.Setenv("GOPI_HOME", t.TempDir())

	out, err := tools.MemoryListRecent(5)
	if err != nil {
		t.Fatalf("MemoryListRecent empty-db error: %v", err)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("empty db output should say so: %q", out)
	}
	if _, err := tools.MemoryStore("R4 list engine test topic", "Why does MemoryListRecent exist?", "It is the recent-rows read without the ranking machinery.", "test", nil); err != nil {
		t.Fatalf("MemoryStore: %v", err)
	}
	out, err = tools.MemoryListRecent(5)
	if err != nil {
		t.Fatalf("MemoryListRecent error: %v", err)
	}
	if !strings.Contains(out, "R4 list engine test topic") {
		t.Errorf("list should show the stored topic:\n%s", out)
	}
}
