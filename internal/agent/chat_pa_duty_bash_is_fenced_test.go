package agent

// chat_pa_duty_bash_is_fenced_test.go — the prompt's over-promise, pinned
// (task 01a1ae0b, 2026-11-06).
//
// WHAT WAS MEASURED. Clause (1b) told every console that a "LIGHT READ-ONLY
// investigation — read, code_search, keyword_search, and the read-only `bash`
// queries" was "yours to do at any moment, without asking anybody", and the write
// boundary's rule (1) said "Reading is always fine (`git status`, `log`, `show`,
// `diff`, `git ls-files`)". Both sentences are about the BASH FACE, and that face
// is not governed by the prompt: it is governed by the operator's ```chat-policy
// fence plus the run's startup posture (internal/tools/chat_policy_gate.go). A run
// whose fence listed `allow ls`, `allow cat`, `allow printf` and no `allow echo`
// received a refusal for `echo hello` while its prompt had just told it that
// reading needed nobody's permission — so the two surfaces disagreed about the
// same call, and the disagreement was in the PROMPT, not in the gate.
//
// THE PROPERTY, and it is what both halves below assert:
//
//	the READ TOOLS and the MEMORY TOOL are ungated — no fence, no posture, no ask;
//	a BASH COMMAND is a different question, decided by the fence.
//
// The clause must therefore still name the ungated faces (a prompt that stopped
// naming them would teach the model to ask about reads, which is the defect the
// 2026-10-09 ruling closed) AND must say which face the fence governs, so a
// refusal on the shell arm is read as "this fence line is missing" rather than as
// "this console may not read".

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// chatPADutyBashFenceClause is the sentence that must exist somewhere in the duty
// paragraph: it names the fence as the thing that decides a shell command. It is a
// literal (not a production constant) so the assertion still measures the RENDERED
// text, which is what the model reads.
const chatPADutyBashFenceClause = "chat-policy"

func TestChatPADutyDoesNotPromiseBashReadsAreUngated(t *testing.T) {
	for name, prompt := range chatPADutyChatPrompts(t) {
		// The over-promise itself, in the two spellings that were shipped.
		for _, banned := range []string{
			"read-only `bash` queries",
			"Reading is always fine",
		} {
			if strings.Contains(prompt, banned) {
				t.Errorf("%s: the prompt still promises %q — the bash face is decided by the operator's fence "+
					"and the startup posture, not by the prompt, so a run that is refused on the shell arm has "+
					"been told the opposite of what happened", name, banned)
			}
		}
	}

	// NON-VACUOUS, and the OTHER direction: the ungated faces must still be named,
	// or this test would pass on a prompt that told the model to ask about reads.
	duty := chatPADutyRules(true)
	for _, want := range []string{
		chatPADutyNoPermissionMarker,
		"without asking anybody",
		"`" + toolnames.ToolRead + "`",
		"`" + toolnames.ToolCodeSearch + "`",
		"`" + toolnames.ToolKeywordSearch + "`",
		"`" + toolnames.ToolMemory + "(action=" + toolnames.ToolMemoryActionSearch + ")`",
	} {
		if !strings.Contains(duty, want) {
			t.Errorf("clause (1b) no longer names %q — the ungated faces are exactly what must stay ungated, "+
				"and a prompt silent about them teaches the model to ASK about a read:\n%s", want, duty)
		}
	}

	// And the fence must be named as the decider for the OTHER face, with the two
	// faces told apart rather than blurred into one promise.
	if !strings.Contains(duty, chatPADutyBashFenceClause) {
		t.Errorf("clause (1b) never names the %q fence, so the reader has no way to know that a shell "+
			"command is judged by the operator's rules rather than by this paragraph:\n%s",
			chatPADutyBashFenceClause, duty)
	}
	if !strings.Contains(duty, "`"+toolnames.ToolBash+"`") {
		t.Errorf("clause (1b) does not name the `%s` face it is drawing the line around:\n%s",
			toolnames.ToolBash, duty)
	}
}
