package main

// chat_unattended_write_e2e_test.go — the REAL-BINARY demonstration that an
// UNATTENDED chat run is not told to wait for a human who is not there.
//
// THE MEASURED DEFECT (2026-10-22, `gopi chat --once` runs; the mechanism is
// internal/tools/chat_operator_presence.go): a `prompt` decision's text ended
// "DO THIS NOW: ask the operator … and wait for their answer", and the run it was
// handed to had declared NO OPERATOR — its own prompt forbids asking ("do NOT ask
// the operator — nobody will", chat_unattended.go), so the surface contradicted
// itself in two places a model reads in the same turn. Measured on a real
// one-shot run: the model re-issued the refused call until its turn budget ran
// out.
//
// WHY END TO END AND NOT ONLY UNIT TESTS (Rule 31). The unit cases in
// internal/tools prove the DECISION; they cannot prove the decision reaches a
// REAL RUN's model — that cmd/agent publishes the run's own contract into the
// boundary, that the real dispatcher consults the guard for the real `write`
// tool, and that the tool result the model receives on the NEXT request is the
// new text. Every link in that chain is process-shaped, so it is driven through
// the shipped binary with a scripted mock LLM (zero API calls) and a sandboxed
// profile.
//
// WHAT THIS FILE DOES NOT COVER, stated rather than implied: the OTHER half of the
// same fix — a run writing into its OWN session directory — cannot be driven by a
// scripted mock, because that directory's name contains a session id the binary
// mints INSIDE the process, and a scripted tool call carries fixed arguments. Its
// evidence is the dispatch-level case
// (internal/agent/chat_own_session_write_test.go), which drives the real
// entry point with the real argument map.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// chatUnattendedE2EPrompt is the one turn the run gets.
const chatUnattendedE2EPrompt = "write your report into the file named in the task"

// chatUnattendedE2ETarget is a path NO rule can allow: it is outside the fixture
// repository (so no `write <tree>` rule names it), outside the run's own session
// directory (so the session exemption cannot apply), and outside every absolute
// path this machine has. The shipped `prompt *` table therefore answers it with a
// QUESTION — which is the decision whose wording this case measures, and the one
// an unattended run must not be told to put to a human.
const chatUnattendedE2ETarget = "/nonexistent/outside-every-rule/probe.md"

// chatUnattendedE2EEnv is the sandboxed environment: the mock provider, a private
// profile and ledger, and the switches that keep a fixture run off the host
// (mirrors the sibling edit-E2E helper).
func chatUnattendedE2EEnv(t *testing.T, home, mockURL string) []string {
	t.Helper()
	return append(testutil.MockEnv(t, home, mockURL),
		"GOPI_MEMORY_DB="+filepath.Join(t.TempDir(), "memory.db"),
		"GOPI_SKIP_CLAIM_BROADCAST=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
	)
}

// TestUnattendedRunIsNotToldToAskAnOperator drives one real
// `gopi chat --once --tools` turn whose scripted `write` meets a `prompt`
// decision, and reads the tool result the model got back.
func TestUnattendedRunIsNotToldToAskAnOperator(t *testing.T) {
	bin := testutil.GopiBin(t)
	t.Setenv("GOPI_BIN", bin)
	SandboxRepoContext(t)
	repo := RepoContextFixtureRepo(t)

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_write_outside_every_rule",
			Name: toolnames.ToolWrite,
			Args: map[string]interface{}{
				toolnames.ToolArgPath:    chatUnattendedE2ETarget,
				toolnames.ToolArgContent: "content the rules do not allow here\n",
			},
		}},
		testutil.Resp{Text: "Could not deliver there."},
	)
	t.Cleanup(mock.Close)

	_, stderr, code := runGopiMapped(t,
		[]string{"chat", "--once", "--tools", "-p", chatUnattendedE2EPrompt}, "",
		chatUnattendedE2EEnv(t, t.TempDir(), mock.URL()), repo)
	if code != 0 {
		t.Fatalf("the run must complete: code=%d\n--- stderr ---\n%s", code, stderr)
	}
	if mock.RequestCount() < 2 {
		t.Fatalf("the run took %d model request(s) — the tool result never went back to the model, so nothing "+
			"about its wording was measured\n--- stderr ---\n%s", mock.RequestCount(), stderr)
	}

	bodies := mock.RequestsContaining("NEEDS OPERATOR APPROVAL")
	if len(bodies) == 0 {
		t.Fatalf("no model request carried the boundary's answer at all — the run was not measured\n--- stderr ---\n%s",
			stderr)
	}
	last := bodies[len(bodies)-1]
	// SLICE AT THE TOOL RESULT, not at the whole request: the chat system
	// prompt itself carries the interactive wording ("you STOP and ask the
	// operator in the conversation"), so a substring test over the whole body
	// would fail on the PROMPT no matter what the tool result said — a vacuous
	// red, the mirror of the vacuous green the sibling edit-E2E case documents.
	// The prefix is the tool result's own first line, so everything from there
	// on is the boundary's answer.
	at := strings.Index(last, "NEEDS OPERATOR APPROVAL")
	if at < 0 {
		t.Fatalf("the request selected by the prefix does not contain it — the assertion would be vacuous:\n%s", last)
	}
	answer := last[at:]
	for _, forbidden := range []string{"ask the operator in the conversation", "wait for their answer"} {
		if strings.Contains(answer, forbidden) {
			t.Fatalf("the UNATTENDED run's model was told %q — nobody on this run can answer, and the same run's "+
				"prompt forbids asking:\n%s", forbidden, answer)
		}
	}
	if !strings.Contains(answer, "NO OPERATOR IS READING") {
		t.Fatalf("the delivered text does not say why the wait cannot end:\n%s", answer)
	}
}
