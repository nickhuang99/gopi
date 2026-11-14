package main

// bare_prompt_guard_e2e_test.go — the END-TO-END half of the bare-prompt guard
// (bug row 01a1ac14): a bare word from inside an agent session must create
// NOTHING.
//
// WHY THE REAL BINARY, AGAIN. The property under test is "does the shipped CLI
// refuse BEFORE it spends" — a unit assertion on config.GuardBarePrompt (which
// internal/config/agentguard_bareprompt_test.go already pins) would not prove
// the hook is wired, and the hook is exactly where a guard quietly does
// nothing. cmd/agent's own agent_spawn_guard_test.go states the same rule for
// the two never-returning verbs; this file is the third surface.
//
// WHAT "CREATES NOTHING" MEANS, measured three ways, because the defect was
// measured as three artifacts (row 01a1ac14, 2026-11-05):
//
//	① THE LEDGER: `gopi task list` in the sandbox still answers with the empty
//	   contract line — no `[auto] submit-x` row was registered;
//	② THE SESSIONS: the sandbox profile's sessions/ directory is still empty —
//	   no session directory was minted;
//	③ THE WORKSPACE: the run's declared ground root is still empty — no clone
//	   ground was created under the SHARED ~/workspace.
//
// The POSITIVE case beside it is the regression half: the same argv from a
// HUMAN terminal (the documented `gopi "<prompt>"` usage) still runs, answered
// by the mock endpoint, so the guard has not quietly forbidden the face.
//
// COST: zero. The refused arm never reaches a model; the allowed arm reaches
// only testutil.MockLLM (a local httptest server). No real key, no real host.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// barePromptWord is the word this bug was measured with: not a verb, not a
// near-miss, not a spelling of one — so the dispatch chain cannot classify it
// and the run path would spend it as a prompt. It is the literal from the
// reproduction, deliberately: a fixture word that a future alias table might
// adopt would stop reproducing the shape.
const barePromptWord = "submit-x"

// barePromptEmptyLedgerLine is the empty-ledger answer `gopi task list` prints.
// It MIRRORS internal/tools' unexported taskListEmptyResult (that package cannot
// be reached for a constant here without exporting it for a test's sake), and
// the assertion is about the CHILD's output, which is the shipped surface.
const barePromptEmptyLedgerLine = "No tasks found."

// barePromptEnv builds the child's environment: testutil.MockEnv (hermetic
// GOPI_HOME, mock endpoint, pinned ground root), with the agent signature
// STRIPPED and the guard knobs cleared so each case states exactly the shape it
// means to test. MockEnv sets HumanTerminalEnv (it IS the operator actor), so
// the strip is what lets the negative case be a real agent shape rather than a
// human one wearing a signature.
//
// The overrides are appended AFTER the filter, so no duplicate name can decide
// the child's reading — the map is the single writer of every value here.
func barePromptEnv(t *testing.T, mockURL string, overrides map[string]string) []string {
	t.Helper()
	dropped := map[string]bool{
		config.AgentSessionEnv:            true,
		config.HermesSessionIDEnv:         true,
		config.AgentSessionAncestorEnv:    true, // the anchor: re-stated per case
		config.HumanTerminalEnv:           true, // re-stated per case
		config.AgentSpawnGuardOverrideEnv: true,
		config.AgentSpawnGuardOffEnv:      true,
	}
	for name := range overrides {
		dropped[name] = true
	}
	home := t.TempDir()
	out := make([]string, 0, len(overrides)+32)
	for _, kv := range testutil.MockEnv(t, home, mockURL) {
		name, _, _ := strings.Cut(kv, "=")
		if dropped[name] {
			continue
		}
		out = append(out, kv)
	}
	for name, value := range overrides {
		out = append(out, name+"="+value)
	}
	return out
}

// barePromptSessionDirNames lists the session directory NAMES in a sandbox
// home, or nil when the directory does not exist yet — the reading ② makes.
func barePromptSessionDirNames(t *testing.T, home string) []string {
	t.Helper()
	dir := config.ProfilePathIn(home, config.SessionsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the sandbox sessions dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestBarePromptIsRefusedInsideAnAgentSession is the acceptance criterion: the
// real binary, the exact measured argv, an inherited agent signature, and every
// artifact the defect produced measured ABSENT afterwards.
func TestBarePromptIsRefusedInsideAnAgentSession(t *testing.T) {
	bin := testutil.GopiBin(t)
	assertGopiBinIsFresh(t, bin)

	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "the guard did not fire"})
	t.Cleanup(mock.Close)

	// The ground root is pinned to a directory this test owns and COUNTS (③),
	// and the sandbox home is pinned too (① and ②). Both are read from the same
	// values the child gets, so the measurement cannot drift from the run.
	home := t.TempDir()
	groundRoot := t.TempDir()
	ledgerPath := filepath.Join(config.ProfileDirIn(home), config.MemoryDBFileName)
	if err := os.MkdirAll(config.ProfileDirIn(home), 0o700); err != nil {
		t.Fatalf("create the sandbox profile root: %v", err)
	}
	env := barePromptEnv(t, mock.URL(), map[string]string{
		"GOPI_HOME":                       home,
		"GOPI_MEMORY_DB":                  ledgerPath,
		worktreeroots.WorktreeRootEnvName: groundRoot,
		// The measured agent shape: an agent session's id, and an anchor pid
		// that is an ANCESTOR (pid 1 — never this process), which is exactly
		// what a child of an agent inherits.
		config.AgentSessionEnv:         "6424c4c1-0adb-48e8-bf43-d6d0baae5e0e",
		config.AgentSessionAncestorEnv: "1",
	})

	stdout, stderr, code := testutil.RunGopi(t, []string{barePromptWord}, "", env, t.TempDir())
	out := stdout + stderr
	if code != config.AgentGuardRefusalExitCode {
		t.Fatalf("`gopi %s` from an agent session: exit=%d want %d (the named refusal)\noutput:\n%s",
			barePromptWord, code, config.AgentGuardRefusalExitCode, out)
	}
	// Rule 15: the refusal must NAME the surface, the spend and the doors.
	for _, want := range []string{
		config.BarePromptFace,
		barePromptWord,
		config.BarePromptTaskSubmitAddress,
		config.BarePromptChatAddress,
		config.BarePromptVerbHelpAddress,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not carry %q — an agent reading it must learn what it just triggered and where to go instead:\n%s",
				want, out)
		}
	}
	if n := mock.RequestCount(); n != 0 {
		t.Errorf("the refused run reached the model %d time(s) — a refusal must precede the spend:\n%s", n, out)
	}

	// ① the ledger is still empty, read back through the SHIPPED verb.
	listOut, listErr, listCode := testutil.RunGopi(t, []string{CliVerbTask, "list"}, "", env, t.TempDir())
	if listCode != 0 {
		t.Fatalf("`gopi task list` in the sandbox: exit=%d\nstdout:\n%s\nstderr:\n%s", listCode, listOut, listErr)
	}
	if !strings.Contains(listOut, barePromptEmptyLedgerLine) {
		t.Errorf("the refused run registered a ledger row — `gopi task list` must still answer %q:\n%s",
			barePromptEmptyLedgerLine, listOut)
	}
	if strings.Contains(listOut, barePromptWord) {
		t.Errorf("the ledger carries the bare word — that row is the `[auto] %s` the defect wrote:\n%s", barePromptWord, listOut)
	}

	// ② no session directory was minted.
	// why: correct — the set is every entry under the fixture's OWN pinned session root, read after the refused run, and the positive control above it (the same helper returns the directory the accepted run of the sibling test mints) shows the reader can see a non-empty result: a refusal that created one would make len != 0 and the assertion fires. There is no idempotent step in "mint a session directory for a refused prompt" — the whole defect is that it cost something.
	if names := barePromptSessionDirNames(t, home); len(names) != 0 {
		t.Errorf("the refused run minted a session directory (%s) — a refusal must cost nothing", strings.Join(names, ", "))
	}

	// ③ no clone ground was created under the declared root.
	if entries, err := os.ReadDir(groundRoot); err != nil {
		t.Fatalf("read the pinned ground root %s: %v", groundRoot, err)
	} else if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the refused run created a ground (%s) under the declared root — the defect's third artifact", strings.Join(names, ", "))
	}
}

// TestBarePromptStillRunsFromAHumanTerminal is the REGRESSION half: the same
// argv from the operator's own shell — the documented `gopi "<prompt>"` usage —
// still starts a run, answered by the mock. Without this the guard could
// "pass" by forbidding the face outright.
func TestBarePromptStillRunsFromAHumanTerminal(t *testing.T) {
	bin := testutil.GopiBin(t)
	assertGopiBinIsFresh(t, bin)

	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "bare-prompt-human-ok"})
	t.Cleanup(mock.Close)

	home := t.TempDir()
	env := barePromptEnv(t, mock.URL(), map[string]string{
		"GOPI_HOME":                       home,
		"GOPI_MEMORY_DB":                  filepath.Join(config.ProfileDirIn(home), config.MemoryDBFileName),
		worktreeroots.WorktreeRootEnvName: t.TempDir(),
		// The operator's terminal: the negative signature the guard honours
		// before consulting anything else.
		config.HumanTerminalEnv: "1",
	})

	stdout, stderr, code := testutil.RunGopi(t, []string{barePromptWord}, "", env, t.TempDir())
	out := stdout + stderr
	if code == config.AgentGuardRefusalExitCode {
		t.Fatalf("a human terminal was REFUSED the bare-prompt face — that forbids the documented usage of `gopi \"<prompt>\"`:\n%s", out)
	}
	if strings.Contains(out, config.BarePromptFace) {
		t.Errorf("the bare-prompt refusal leaked into a human run:\n%s", out)
	}
	if code != 0 {
		t.Fatalf("a human bare-prompt run must succeed against the mock: exit=%d\nstdout:\n%s\nstderr:\n%s",
			code, stdout, stderr)
	}
	if !strings.Contains(stdout, "bare-prompt-human-ok") {
		t.Errorf("the human run did not reach the model — the answer is missing:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if n := mock.RequestCount(); n == 0 {
		t.Errorf("the mock served no request — the run never reached a model at all:\n%s", out)
	}
}
