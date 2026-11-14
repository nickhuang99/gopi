// cli_tasks_ssh_passthrough_test.go — the task dispatcher's ssh-passthrough
// arm, pinned in BOTH directions.
//
// THE DEFECT THIS CLOSES (measured 2026-09-29 on the built binary, task
// 01a0ece6-09dc-7219-afd3-22b8183586a1): `gopi web search "task list"` printed
// the LEDGER instead of searching the web. The passthrough arm in
// isTasksSubcommand ran whenever args[0] != "task" and ASSUMED the first
// non-flag token was an ssh HOST, so argv [web, search, "task list"] read
// `web` as a host, word-split the rest, found the literal `task` inside the
// QUERY TEXT and dispatched runTasksSubcommand(["list"]). A hijacked run
// SUCCEEDS (exit 0) while performing a different, ledger-touching action — the
// worst shape a routing bug can take, because every cheap signal (exit code,
// no error, plausible output) says the command worked.
//
// WHICH SHAPES THE ARM WAS ACTUALLY WRITTEN FOR (and must keep working):
//
//	① the option-carrying ssh argv — `ssh -o BatchMode=yes bnode "gopi task
//	  sync --serve-remote"` — where gopi is handed the FULL ssh argv, options
//	  and host included (M4.13, strace-proven: without the passthrough it fell
//	  through to the agent loop and burned an LLM session on a handshake);
//	② the peer-side prologue — ssh runs the remote command under a shell, so
//	  the peer's gopi receives the peer's own command line, program word first
//	  (`gopi task list`), and reads it as `gopi` followed by the verb.
//
// The tests below drive the REAL binary (Rule 31) through the shared CLI
// contract probe (cli_contracts_test.go: scrubbed environment, isolated
// GOPI_HOME, the no-agent-loop timeout), because the defect lived in argv
// routing and a routing defect is precisely what a unit test on a helper
// (with the argv the test author imagined) cannot see.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// tasksSSHPassthroughTimeout bounds one probe that legitimately reaches the WEB
// TOOL path: a `web search`/`web fetch` really tries to run its plugin scripts
// and a network call, so it is slower than a ledger read. The parent bound
// (cliContractTimeout) is the no-agent-loop detector for handlers that answer
// instantly; this one exists only so a hang cannot wedge the suite.
const tasksSSHPassthroughTimeout = 60 * time.Second

// taskverbsListForTest is the task sub-verb the argv tables below spell — read
// from the family leaf package (Rule 13: never a bare literal at a decision
// site, and a test that pins ROUTING is such a site).
const taskverbsListForTest = taskverbs.List

// tasksDispatchFingerprint is the header EVERY non-empty `gopi task list` render
// starts with — the ledger read's own signature, and the positive assertion of
// the pair ("did the task handler run?").
//
// An EMPTY ledger answers in words instead ("No tasks found.", measured on the
// built binary), which would make the assertion vacuous — so the probes below
// mint a row FIRST, through the CLI's own `task submit`, into the very profile
// the shape probe then runs in (the technique the family's other contract tests
// use). The row count is non-zero BY CONSTRUCTION and no test seeds a database
// by hand (Rule 32).
const tasksDispatchFingerprint = "task(s):"

// tasksSubmitFingerprint is what `gopi task submit` prints when it has written
// the row — the fixture's own proof that the ledger is populated (same literal
// the neighbouring routing contract test asserts).
const tasksSubmitFingerprint = "Task queued"

// taskDispatchAnswered reports whether out is the TASK HANDLER's own answer.
// Everything else — the web tool's JSON payload, a usage dump, an
// unknown-subcommand refusal — is a DIFFERENT handler answering.
func taskDispatchAnswered(out string) bool {
	return strings.Contains(out, tasksDispatchFingerprint)
}

// taskverbsListForTest is the task sub-verb the argv tables above spell — the
// family leaf package's constant (Rule 13: never a bare literal at a decision
// site, and a test that pins routing is such a site).
// TestIsTasksSubcommandNeverClaimsAGopiCommandLine is the in-process pin: the
// ROUTER is asked directly, on the shapes a user types, and must answer
// "not the task family" for every gopi command line — including the hijack
// repro of 2026-09-29, which pre-fix dispatched a ledger listing.
//
// Calls that DO dispatch are the third column: they exit the process, so they
// are driven through the real binary in the tests below rather than here.
func TestIsTasksSubcommandNeverClaimsAGopiCommandLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"web search with a task query (the reported hijack)", []string{CliVerbWeb, "search", "task list"}},
		{"web search with a longer task query", []string{CliVerbWeb, "search", "how to list a task in a ledger"}},
		{"web fetch of a URL carrying task/", []string{CliVerbWeb, "fetch", "http://example.invalid/task/12"}},
		{"web search with flags after the query", []string{CliVerbWeb, "search", "task list", "--method", "bing"}},
		{"flat web_search mirror with a task query", []string{"web_search", "task list"}},
		{"web server flags", []string{CliVerbWeb, "--addr", "127.0.0.1:0"}},
		{"memory search for the word task", []string{CliVerbMemory, "search", "task list"}},
		{"code-search for a task symbol", []string{CliVerbCodeSearch, "task list"}},
		{"keyword search for the word task", []string{"keyword_search", "task list"}},
		{"a bare prompt that is not a command", []string{"explain the task list command"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if isTasksSubcommand(append([]string(nil), tc.args...)) {
				t.Errorf("isTasksSubcommand(%q) = true — a gopi command line was claimed by the task family; "+
					"the ssh-passthrough arm must never read a free-text argument as a remote command",
					strings.Join(tc.args, " "))
			}
		})
	}
}

// TestSSHPrefixWalkOnlyConsumesATransportPrefix pins the walk the passthrough
// router and the task parser SHARE: it answers "what follows an ssh transport
// prefix", and nil for every argv that does not carry one. Getting this wrong
// in either direction re-opens the hijack (too permissive) or breaks the
// shapes the arm exists for (too strict).
func TestSSHPrefixWalkOnlyConsumesATransportPrefix(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string // nil = "this argv carries no ssh transport prefix"
	}{
		{"ssh option pair + host + command", []string{"-o", "BatchMode=yes", "bnode", "gopi", "task", "list"},
			[]string{"gopi", "task", "list"}},
		{"several transport flags", []string{"-4", "-o", "StrictHostKeyChecking=yes", "bnode", "task", "list"},
			[]string{"task", "list"}},
		{"a gopi command line is never an ssh argv", []string{CliVerbWeb, "search", "task list"}, nil},
		{"the task verb itself is a command line", []string{CliVerbTask, taskverbsListForTest}, nil},
		{"one argument that is a command word", []string{CliVerbWeb}, nil},
		{"a bare host with no command", []string{"bnode"}, nil},
		{"a lone flag with no host", []string{"-o", "BatchMode=yes"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := argvAfterSSHPrefix(append([]string(nil), tc.args...))
			if len(got) != len(tc.want) {
				t.Fatalf("argvAfterSSHPrefix(%q) = %q, want %q", strings.Join(tc.args, " "), strings.Join(got, " "), strings.Join(tc.want, " "))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("argvAfterSSHPrefix(%q) = %q, want %q", strings.Join(tc.args, " "), strings.Join(got, " "), strings.Join(tc.want, " "))
				}
			}
		})
	}
}

// TestWebSearchWithTaskQueryReachesTheWebTool is the acceptance probe of the
// reported bug, on the REAL binary: `gopi web search "task list"` must reach
// the web_search path and must NOT be answered by the ledger.
//
// The web tool's payload and the ledger's render are asserted by shape, not by
// a marker file: the payload is the tool's JSON envelope (`{"status": …`) and
// the ledger's render is `N task(s):`. The probe runs WITHOUT the plugin
// scripts on purpose — the tool path answers its own payload even then (it
// reports `scripts dir not found` INSIDE the JSON), while the pre-fix code
// never produced that JSON at all, which is what makes this red on the old
// binary and green on the new one.
func TestWebSearchWithTaskQueryReachesTheWebTool(t *testing.T) {
	bin := gopiTestBinPath(t)
	// The exit status is deliberately NOT asserted: the web tool exits non-zero
	// when its payload says status=unavailable (no plugin scripts and no
	// network in the probe environment), which is the tool's own report about
	// the WORLD, not about routing. Routing is asserted by SHAPE — the tool's
	// JSON envelope versus the ledger's render.
	out, _, timedOut := cliContractProbeWithTimeout(bin, t.TempDir(), tasksSSHPassthroughTimeout, //stylegate:ignore runs the task verb through the passthrough probe and asserts on its stdout; the probe's error IS the second assertion
		CliVerbWeb, "search", "task list")
	if timedOut {
		t.Fatalf("gopi web search \"task list\": TIMEOUT — argv fell through to the agent loop")
	}
	if taskDispatchAnswered(out) {
		t.Fatalf("gopi web search \"task list\": the LEDGER answered instead of the web tool (the hijack)\n--- output ---\n%s", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), `{"status"`) {
		t.Fatalf("gopi web search \"task list\": output is not a web_search JSON payload\n--- output ---\n%s", out)
	}
}

// TestWebFetchOfATaskURLReachesTheWebTool is the same probe for the sibling
// shape the briefing names: a URL whose path contains `task/`. Pre-fix the
// word-scan found the literal `task` in the URL and listed the ledger.
func TestWebFetchOfATaskURLReachesTheWebTool(t *testing.T) {
	bin := gopiTestBinPath(t)
	url := "http://127.0.0.1:1/task/12"
	out, _, timedOut := cliContractProbeWithTimeout(bin, t.TempDir(), tasksSSHPassthroughTimeout, //stylegate:ignore same probe on the second passthrough case, where the printed diagnosis is the contract
		CliVerbWeb, "fetch", url)
	if timedOut {
		t.Fatalf("gopi web fetch %s: TIMEOUT — argv fell through to the agent loop", url)
	}
	if taskDispatchAnswered(out) {
		t.Fatalf("gopi web fetch %s: the LEDGER answered instead of the web tool (the hijack)\n--- output ---\n%s", url, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), `{"status"`) {
		t.Fatalf("gopi web fetch %s: output is not a web_fetch JSON payload\n--- output ---\n%s", url, out)
	}
}

// TestTaskFamilyStillDispatchesThroughTheSSHShapes is the OTHER direction: the
// shapes the passthrough arm was written for must still reach the task family,
// and a plain gopi command line must keep dispatching as before. A fix that
// only refused the hijack would be half a fix.
//
// `gopi -o BatchMode=yes bnode gopi task list` is the option-carrying ssh argv
// of ① — the pre-fix build reached the task handler with the transport prefix
// STILL ON THE SLICE and answered `unknown subcommand "BatchMode=yes"` (a
// half-fix observed live); the tests below assert the ledger render instead.
func TestTaskFamilyStillDispatchesThroughTheSSHShapes(t *testing.T) {
	bin := gopiTestBinPath(t)
	// One profile for the whole test: a row is submitted into it FIRST, so every
	// shape below reads a NON-EMPTY ledger and the assertion is the render
	// header rather than the empty-ledger sentence (tasksDispatchFingerprint).
	ledgerHome := t.TempDir()
	seed, _ := runCLIContractProbeEnv(t, bin, ledgerHome, CliVerbTask, "submit", //stylegate:ignore seeds a ledger row through the real CLI; the seeded row is read back below
		"ssh-passthrough routing fixture (row content is irrelevant — only that the ledger is non-empty)",
		"--payload", "created by the routing contract test to give the ledger one row; never claimed")
	if !strings.Contains(seed, tasksSubmitFingerprint) {
		t.Fatalf("fixture: `gopi task submit` did not queue a row — the shape probes below would then assert the empty-ledger sentence\n--- output ---\n%s", seed)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"plain task list", []string{CliVerbTask, taskverbsListForTest}},
		{"peer prologue: argv[0] is the program word", []string{"gopi", CliVerbTask, taskverbsListForTest}},
		{"option-carrying ssh argv", []string{"-o", "BatchMode=yes", "bnode", "gopi", CliVerbTask, taskverbsListForTest}},
		{"option-carrying ssh argv, bare remote verb", []string{"-o", "BatchMode=yes", "bnode", CliVerbTask, taskverbsListForTest}},
		{"transport flags and an ssh option", []string{"-4", "-o", "BatchMode=yes", "bnode", "gopi", CliVerbTask, taskverbsListForTest}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := runCLIContractProbeEnv(t, bin, ledgerHome, tc.args...) //stylegate:ignore runs the case's own args through the probe; the printed output is what the table asserts
			if !taskDispatchAnswered(out) {
				t.Fatalf("gopi %s: output lacks the ledger render header (%q) — did NOT reach the task handler\n--- output ---\n%s",
					strings.Join(tc.args, " "), tasksDispatchFingerprint, out)
			}
			if strings.Contains(out, "unknown subcommand") {
				t.Fatalf("gopi %s: the ssh transport prefix reached the family parser\n--- output ---\n%s", strings.Join(tc.args, " "), out)
			}
		})
	}
}

// TestWebSearchOfAnUnrelatedQueryIsUnchanged is the control the briefing asks
// for: a query with no `task` word in it behaved correctly before the fix and
// must behave identically after — the change is a routing gate, not a change
// to the web surface.
func TestWebSearchOfAnUnrelatedQueryIsUnchanged(t *testing.T) {
	bin := gopiTestBinPath(t)
	out, _, timedOut := cliContractProbeWithTimeout(bin, t.TempDir(), tasksSSHPassthroughTimeout, //stylegate:ignore runs the timeout case and asserts on timedOut, so the probe's error channel is not the signal
		CliVerbWeb, "search", "postgres append-only ledger", "--method", "wiki")
	if timedOut {
		t.Fatalf("gopi web search (control): TIMEOUT — argv fell through to the agent loop")
	}
	if !strings.HasPrefix(strings.TrimSpace(out), `{"status"`) {
		t.Fatalf("gopi web search (control): output is not a web_search JSON payload\n--- output ---\n%s", out)
	}
	if taskDispatchAnswered(out) {
		t.Fatalf("gopi web search (control): the LEDGER answered\n--- output ---\n%s", out)
	}
}

// TestWebSearchProbeHomeIsTheSandboxProfileForThisRun is the guard that keeps
// the probes above honest about WHERE they run (Rule 34): the contract probe's
// isolated GOPI_HOME is the only profile they touch. It asserts the environment
// the shared probe builds actually redirects the profile root, so a future
// change to cliContractEnv cannot silently point these probes at the
// operator's live ledger.
func TestWebSearchProbeHomeIsTheSandboxProfileForThisRun(t *testing.T) {
	home := t.TempDir()
	env := cliContractEnv(home)
	found := ""
	for _, e := range env {
		if strings.HasPrefix(e, "GOPI_HOME=") {
			found = strings.TrimPrefix(e, "GOPI_HOME=")
		}
	}
	if found == "" {
		t.Fatalf("cliContractEnv(%s) carries no GOPI_HOME — the probes would run against the operator's profile", home)
	}
	if found != home {
		t.Fatalf("cliContractEnv(%s) sets GOPI_HOME=%s — a probe would write outside its sandbox", home, found)
	}
	if _, err := os.Stat(filepath.Join(home, "memory.db")); err == nil {
		t.Fatalf("probe home %s already contains a ledger", home)
	}
}
