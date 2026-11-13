package main

// edit_tool_arg_refusal_e2e_test.go — the REAL-BINARY demonstration of the
// silent-content-loss fix (Rule 31: the surface that changed is the `edit`
// tool's argument path, so it is driven end to end through the shipped binary,
// not only through unit tests).
//
// THE MEASURED DEFECT. Task 01a103c2 (2026-10-04) recorded, in one session's
// raw log, 17 `edit` tool_use blocks of which 6 carried their replacement under
// the key `newText` instead of `newStr`. The dispatcher read the absent `newStr`
// as "" and called tools.Edit(path, oldStr, "", …) — which is a DELETION — and
// answered "Successfully edited <path>". The matched line vanished, the
// replacement never landed, and no channel said so; only a standards gate that
// happened to cover the file noticed.
//
// WHY AN END-TO-END CASE AND NOT ONLY THE UNIT TESTS. The unit tests prove the
// dispatcher and the tool refuse the shape; this case proves the refusal reaches
// a REAL run's model — the scripted `edit` call goes out on the wire, the real
// binary dispatches it, and the refusal must come back in the tool result the
// NEXT model request carries. A refusal that is computed but never fed back
// would keep the defect's shape (the model walks away believing it worked)
// while every unit test stayed green.
//
// The shape is the established cmd/agent harness: the real binary, a scripted
// mock LLM (zero API calls), a throwaway git fixture repo as the run's cwd, and
// a sandboxed GOPI_HOME/GOPI_MEMORY_DB. The edited file lives INSIDE that
// fixture repo, so nothing can leak into a real checkout.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

// editE2EFixtureFile is the file both cases edit: the minimal sample of the
// defect's own report — a unique anchor line between two neighbours, so "the
// replacement landed", "the anchor survived" and "the file grew a line" are
// three independently observable facts.
const (
	editE2EFixtureFile = "notes.md"
	editE2EAnchorLine  = "| anchor-row | keep me |"
	editE2EFixture     = "line-A\n" + editE2EAnchorLine + "\nline-C\n"
	// editE2ERefusalMarker is a sentence from the refusal itself; the run's
	// SECOND model request must carry it back, which is how this case observes
	// that the refusal was DELIVERED rather than merely produced.
	//
	// It is a WHOLE SENTENCE of the refusal, and must be asserted ABSENT from the
	// first request, because the first measurement of this constant was the bare
	// word "refused" — which the chat write boundary's own prompt carries eight
	// times, so the delivery assertion passed on a run where nothing was ever
	// delivered (a vacuous green, measured 2026-10-21 in this task: request 1
	// contained "refused" 8× before any tool call had run). A marker is only
	// evidence of delivery if the turn that could not have delivered it lacks it.
	editE2ERefusalMarker = "an empty replacement is not an edit"
	// editE2EPrompt drives one turn with the toolset enabled (the tools are
	// opt-in on the one-shot face).
	editE2EPrompt = "rename the anchor row"
	// chatPolicyFlagForFixture opens the CHAT WRITE BOUNDARY for this fixture
	// run: `--policy allow`, the posture channel `gopi chat` already serves.
	//
	// WHY THE CASE NEEDS IT, stated because the absence is what this test was
	// actually measuring. A fixture repo carries no project context file, so the
	// rule table has no ```chat-policy fence and the boundary falls back to the
	// built-in `prompt *` table — under which EVERY write target is a question
	// for a human, and a `--once` run has no human. Measured red (2026-10-21,
	// this task): the tool result the model received was
	// "[NEEDS OPERATOR APPROVAL — command NOT run] …notes.md … matched rule `*`
	// (built-in chat-policy defaults:2)" and the file was untouched — i.e. the
	// case was measuring the BOUNDARY, not the edit tool's match/replace
	// semantics it names. The posture is the channel that answers "what does the
	// console do when no rule speaks" (internal/chatpolicy/posture.go), which is
	// exactly the question a fixture with no rules raises; the boundary's own
	// behaviour is covered by internal/tools' chat_write_guard tests and by this
	// package's chat-policy cases, which declare their fences.
	chatPolicyFlagForFixture = "--policy=allow"
)

// editE2EEnv is the sandboxed environment every case hands the binary: the mock
// provider, a private profile and ledger, and the switches that keep a fixture
// run from touching the host (mirrors the sibling writer E2E).
func editE2EEnv(t *testing.T, home, mockURL string) []string {
	t.Helper()
	env := testutil.MockEnv(t, home, mockURL)
	return append(env,
		"GOPI_MEMORY_DB="+filepath.Join(t.TempDir(), "memory.db"),
		"GOPI_SKIP_CLAIM_BROADCAST=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
	)
}

// editE2ERun drives one real `gopi chat --once --tools` turn against a fixture
// repo holding the sample file, with the given `edit` arguments, and returns the
// mock so the case can read what the model was told.
func editE2ERun(t *testing.T, editArgs map[string]interface{}) (string, *testutil.MockLLM) {
	t.Helper()
	bin := testutil.GopiBin(t)
	t.Setenv("GOPI_BIN", bin)
	SandboxRepoContext(t)
	repo := RepoContextFixtureRepo(t)

	target := filepath.Join(repo, editE2EFixtureFile)
	if err := os.WriteFile(target, []byte(editE2EFixture), 0o644); err != nil {
		t.Fatalf("seed the sample file: %v", err)
	}
	// The path the model cites is the file in the FIXTURE repo, so the assertion
	// below can only pass if the real binary edited THAT file.
	editArgs[toolnames.ToolArgPath] = target

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_edit_e2e",
			Name: toolnames.ToolEdit,
			Args: editArgs,
		}},
		testutil.Resp{Text: "Done."},
	)
	t.Cleanup(mock.Close)

	_, stderr, code := runGopiMapped(t,
		[]string{"chat", "--once", "--tools", chatPolicyFlagForFixture, "-p", editE2EPrompt}, "",
		editE2EEnv(t, t.TempDir(), mock.URL()), repo)
	if code != 0 {
		t.Fatalf("the run must complete: code=%d\n--- stderr ---\n%s", code, stderr)
	}
	return target, mock
}

// TestEditToolRefusesAMisnamedReplacementEndToEnd is the fix's acceptance on the
// shipped binary: the measured misnomer (`newText`) must produce a refusal that
// names the key the caller sent AND the schema's key, and the file must be left
// exactly as it was.
func TestEditToolRefusesAMisnamedReplacementEndToEnd(t *testing.T) {
	target, mock := editE2ERun(t, map[string]interface{}{
		toolnames.ToolArgOldStr:             editE2EAnchorLine,
		toolnames.ToolArgNewTextMisspelling: "| NEW-ROW | added |",
	})

	// The refusal must have reached the MODEL: the run's later request carries
	// the tool result, which is the one channel that proves delivery.
	bodies := mock.RequestsContaining(editE2ERefusalMarker)
	if len(bodies) == 0 {
		t.Fatalf("no model request carried the refusal (%q) — the run would have walked away believing the edit worked; requests=%d",
			editE2ERefusalMarker, mock.RequestCount())
	}
	last := bodies[len(bodies)-1]
	for _, want := range []string{toolnames.ToolArgNewTextMisspelling, toolnames.ToolArgNewStr} {
		if !strings.Contains(last, want) {
			t.Errorf("the refusal delivered to the model does not name %q", want)
		}
	}

	onDisk, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read the sample file: %v", err)
	}
	if string(onDisk) != editE2EFixture {
		t.Errorf("a refused edit changed the file — the silent-content-loss shape is still live:\n%q\nwant\n%q", onDisk, editE2EFixture)
	}
}

// TestEditToolLandsAMultilineReplacementEndToEnd is the positive half, on the
// same binary: a single-line oldStr with a MULTI-LINE newStr must land the new
// row AND keep the matched line, so the file grows by exactly one line.
func TestEditToolLandsAMultilineReplacementEndToEnd(t *testing.T) {
	target, mock := editE2ERun(t, map[string]interface{}{
		toolnames.ToolArgOldStr: editE2EAnchorLine,
		toolnames.ToolArgNewStr: "| NEW-ROW | added |\n" + editE2EAnchorLine,
	})

	want := "line-A\n| NEW-ROW | added |\n" + editE2EAnchorLine + "\nline-C\n"
	onDisk, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read the sample file: %v", err)
	}
	if string(onDisk) != want {
		t.Fatalf("the corrected shape did not land through the real binary:\n%q\nwant\n%q\nrequests=%d",
			onDisk, want, mock.RequestCount())
	}
	if mock.RequestCount() < 2 {
		t.Errorf("the run took %d model request(s) — a landed tool call should be reported back and answered",
			mock.RequestCount())
	}
}
