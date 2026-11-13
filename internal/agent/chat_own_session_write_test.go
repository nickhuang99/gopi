package agent

// chat_own_session_write_test.go — the SESSION-DIRECTORY exemption at the
// DISPATCH, which is where it has to hold.
//
// THE MEASURED DEFECT (2026-10-22, `gopi chat --once` runs): a `write` into the
// run's OWN session directory — the directory the prompt's [CURRENT SESSION] block
// names, and the one Rule 37 says a chat's artifacts belong in — came back
//
//	[NEEDS OPERATOR APPROVAL — command NOT run] `/…/.gopi/sessions/<id>/artifacts/x.md`
//	… The file was NOT written.
//
// The directory is outside every git work tree, so no `write <tree>` rule mentions
// it, so the POSTURE answers, and the shipped posture is `prompt *`. A run told to
// deliver into its session directory was therefore refused by a rule that says "ask
// me first" about the directory its own transcript is written to.
//
// WHY THIS CASE IS AT THE DISPATCH rather than only in internal/tools: the unit
// cases pin the PREDICATE, but the predicate only matters if the dispatch reaches
// it — with the real argument map (the key the model actually sends), the real
// guard entry point (executeToolDefault), and the real session directory
// (signalrec.SessionDir, the same resolver the prompt is built from). A unit test
// that called the predicate directly would pass on a dispatch that never consulted
// it, which is the exact shape of the defect being fixed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/signalrec"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// freezeDefaultChatTableForDispatch restores the SHIPPED default rule table
// (`prompt *`) for one test. It is a precondition, not a convenience: the defect
// is about what the DEFAULT table does to a session path, so a widened table
// would prove nothing.
func freezeDefaultChatTableForDispatch(t *testing.T) {
	t.Helper()
	pol, err := chatpolicy.Parse("dispatch default-table test", "```chat-policy\nprompt *\n```\n")
	if err != nil {
		t.Fatalf("the test's own rule table does not parse: %v", err)
	}
	tools.SetChatPolicy(pol, chatpolicy.NewPosture(chatpolicy.DecisionPrompt, "test"))
	t.Cleanup(tools.ResetChatPolicyToDefaults)
}

// ownSessionDispatchDir points this process at a fresh session directory and
// restores the previous one afterwards. The directory comes from the device
// scratch tree (Rule 22: off the `/` partition, never inside the source tree).
func ownSessionDispatchDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(testutil.TestDeviceScratchDir(t, "owndispatch"), "sessions", "session-under-test")
	if err := os.MkdirAll(filepath.Join(dir, tools.ChatSessionArtifactsDirName), 0o755); err != nil {
		t.Fatalf("create the session directory %s: %v", dir, err)
	}
	signalrec.SetSessionDir(dir, "", "")
	t.Cleanup(func() { signalrec.SetSessionDir("", "", "") })
	return dir
}

// chatWriteCall builds the dispatch's argument map for a `write` call — the SAME
// keys the model sends (toolnames' own spellings, not typed literals), in the same
// call shape the sibling dispatch test (chat_cleanup_authorization_test.go) uses.
func chatWriteCall(path, content string) deepseek.ToolCall {
	return deepseek.ToolCall{
		Name: toolnames.ToolWrite,
		Arguments: map[string]interface{}{
			toolnames.ToolArgPath:    path,
			toolnames.ToolArgContent: content,
		},
	}
}

// TestChatDispatchWritesIntoItsOwnSessionDirectory is defect (a)'s acceptance: the
// real dispatch, under the shipped `prompt *` table, must let a chat write into
// its OWN session directory — and the file must actually be on disk afterwards
// (the guard returning "" proves only that it did not refuse).
func TestChatDispatchWritesIntoItsOwnSessionDirectory(t *testing.T) {
	freezeDefaultChatTableForDispatch(t)
	tools.SetChatOperatorAbsent(false)
	t.Cleanup(func() { tools.SetChatOperatorAbsent(false) })
	dir := ownSessionDispatchDir(t)

	target := filepath.Join(dir, tools.ChatSessionArtifactsDirName, "report.md")
	a := &Agent{ChatMode: true}
	out := a.executeToolDefault(chatWriteCall(target, "delivered by the run\n"))
	if strings.Contains(out, tools.ChatWriteApprovalPrefix) {
		t.Fatalf("the dispatch refused a write into THIS run's own session directory under the shipped "+
			"`prompt *` table:\n%s\nRule 37 puts a chat's artifacts exactly here; a run refused its own "+
			"directory has nowhere to deliver.", out)
	}

	onDisk, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the dispatch reported success but %s is not readable: %v\n--- tool result ---\n%s", target, err, out)
	}
	if !strings.Contains(string(onDisk), "delivered by the run") {
		t.Fatalf("the file exists but does not carry the written content: %q", onDisk)
	}
}

// TestChatDispatchStillRefusesAPathOutsideTheSessionDirectory — the exemption is
// CONTAINMENT, not a blanket open: a target outside the session directory still
// meets the `prompt *` question, and under the shipped table that is the refusal
// (there is no operator turn to approve it here).
func TestChatDispatchStillRefusesAPathOutsideTheSessionDirectory(t *testing.T) {
	freezeDefaultChatTableForDispatch(t)
	tools.SetChatOperatorAbsent(false)
	t.Cleanup(func() { tools.SetChatOperatorAbsent(false) })
	dir := ownSessionDispatchDir(t)

	outside := filepath.Join(dir+"-sibling", "report.md")
	a := &Agent{ChatMode: true}
	out := a.executeToolDefault(chatWriteCall(outside, "not allowed\n"))
	if !strings.Contains(out, tools.ChatWriteApprovalPrefix) {
		t.Fatalf("a path OUTSIDE the session directory was not put to the `prompt *` question (%s vs %s):\n%s",
			outside, dir, out)
	}
}
