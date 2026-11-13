package agent

// chat_bash_refusal_verbatim_test.go — the REAL dispatch, exercised end to end
// with the operator's measured fence, printing the bytes a chat turn receives
// (task 01a1ae0b; Rule 31: the surface is run, not described).
//
// WHY A DISPATCH TEST AND NOT A UNIT CALL. `ChatBashWriteRefusal` is the renderer;
// what a run actually experiences is `Agent.executeToolDefault` with ChatMode on —
// the arm that decides whether bash runs and returns the refusal text in place of
// the command's output. This test goes through THAT, with the fence and posture
// frozen through the same `tools.SetChatPolicy` the startup resolver calls, so the
// text printed below is the text a console would have shown.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/chatpolicy"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// The FENCE THE OPERATOR ACTUALLY HAD when `echo hello` was refused: three allowed
// read commands, no `echo`. Frozen here rather than read from the file, because
// the file is the operator's own, lives outside this ground, and is hand-edited —
// a test that read it would measure the operator's mood, not this code.
const measuredFence = "```chat-policy\n" +
	"# ---- read-only queries ----\n" +
	"allow ls\n" +
	"allow cat\n" +
	"allow printf\n" +
	"```\n"

// freezeMeasuredFence installs that fence with the `forbidden` posture the
// measured run started with.
func freezeMeasuredFence(t *testing.T) {
	t.Helper()
	pol, err := chatpolicy.Parse("GOPI.md (measured)", measuredFence)
	if err != nil {
		t.Fatalf("the measured fence does not parse: %v", err)
	}
	posture, ok := chatpolicy.PostureNamed("forbidden", "--policy")
	if !ok {
		t.Fatal("the `forbidden` posture is not accepted — the alias table changed")
	}
	tools.SetChatPolicy(pol, posture)
	t.Cleanup(tools.ResetChatPolicyToDefaults)
}

// TestChatBashRefusalAtTheDispatchNamesTheClauseAndCause is the measured GAP:
// `echo hello` is refused, and the text now says a fence line is missing, names the
// line, and reports a COMMAND boundary — never the write boundary, which the
// command does not touch.
func TestChatBashRefusalAtTheDispatchNamesTheClauseAndCause(t *testing.T) {
	freezeMeasuredFence(t)
	a := &Agent{ChatMode: true}
	out := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": "echo hello"},
	})
	t.Logf("dispatch output for `echo hello` under the measured fence:\n%s", out)
	for _, want := range []string{
		"Refused by the CHAT COMMAND BOUNDARY",
		"no rule in the project's ```chat-policy fence mentions",
		"`allow echo`",
		"posture",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the dispatch's refusal does not carry %q — a reader still cannot tell a fence gap "+
				"from `this console may not read`:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Refused by the CHAT WRITE BOUNDARY") {
		t.Fatalf("`echo hello` writes nothing, yet the dispatch refused it as the WRITE boundary:\n%s", out)
	}

	// THE CONTROL, at the same dispatch: a command the fence DOES list runs, so the
	// text above is a statement about the fence's gap and not about a boundary that
	// stopped working.
	allowed := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": "printf CHAT-FENCE-CONTROL"},
	})
	t.Logf("dispatch output for the allowed `printf` control: %q", allowed)
	if strings.Contains(allowed, "Refused by the CHAT") || strings.Contains(allowed, tools.ChatWriteApprovalPrefix) {
		t.Fatalf("a command the fence lists was still stopped — this test would then be measuring a blanket "+
			"boundary rather than a fence gap:\n%s", allowed)
	}
	if !strings.Contains(allowed, "CHAT-FENCE-CONTROL") {
		t.Fatalf("the allowed control did not run, so its silence proves nothing:\n%s", allowed)
	}
}

// TestChatBashCompoundRefusalAtTheDispatchNamesTheDecisiveClause is the SECOND
// measured line: `cd … && ls … ; awk … | head` was refused CORRECTLY (three
// clauses the fence never mentions), and the text must say so clause by clause —
// the operator's `ls` is allowed standing alone, and a refusal that hid the
// distinction taught the opposite.
func TestChatBashCompoundRefusalAtTheDispatchNamesTheDecisiveClause(t *testing.T) {
	freezeMeasuredFence(t)
	a := &Agent{ChatMode: true}
	out := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": "cd /tmp && ls -l ; awk '{print}' /etc/hostname | head -1"},
	})
	t.Logf("dispatch output for the measured compound line:\n%s", out)
	for _, want := range []string{
		"Refused by the CHAT COMMAND BOUNDARY",
		"CHAIN of clauses",
		"`cd /tmp` → `forbidden`",
		"`ls -l` → `allow`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the compound refusal does not carry %q — the reader cannot see that only the unwritten "+
				"clauses refused it and that `ls -l` is allowed:\n%s", want, out)
		}
	}
	// And the clause the fence DOES list, alone, still runs — the measured reason
	// the old sentence ("ls 也被判越界") had to be retracted.
	alone := a.executeToolDefault(deepseek.ToolCall{
		Name:      toolnames.ToolBash,
		Arguments: map[string]interface{}{"command": "ls -l /etc/hostname"},
	})
	if strings.Contains(alone, "Refused by the CHAT") {
		t.Fatalf("`ls -l …` alone was refused by a fence that says `allow ls`:\n%s", alone)
	}
}
