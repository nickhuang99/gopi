package main

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// The measured defect (2026-09-29). A `gopi chat --tools` run was SIGKILLed
// mid tool call; its session was then resumed with the seed argument alone
// (`gopi chat <session-id>`, no --tools). The seed restored the CONVERSATION —
// including the assistant's tool_use and the tool_result that followed it — but
// not the capability: the resumed turn had no tools array on the wire, so the
// model answered by DESCRIBING the call it could not make ("`resolveResumeLogPath`
// 是做什么的（基于 cmd/agent/cli_resume.go 的 read 结果回答）") instead of running it.
//
// Nothing said a word about it. The history said tools, the run had none, and
// the mismatch surfaced only as a worse answer. These tests pin the sentence
// that now closes that gap, and the two cases where it must stay silent (a
// plain chat resumed plainly, and a tool-enabled chat resumed with --tools) —
// a warning that fires on a healthy resume is noise, and noise is how a real
// warning gets ignored.

// chatResumeToolDefs builds the minimal checkpoint a tool-enabled run leaves
// behind: one tool definition is enough to make the point, because the rule
// branches on "the checkpoint carried tools", never on how many.
func chatResumeToolDefs(n int) []deepseek.ClaudeToolDef {
	defs := make([]deepseek.ClaudeToolDef, 0, n)
	for i := 0; i < n; i++ {
		defs = append(defs, deepseek.ClaudeToolDef{
			Name:        "tool-" + string(rune('a'+i)),
			Description: "a tool the crashed run had",
			InputSchema: map[string]any{"type": "object"},
		})
	}
	return defs
}

// TestChatResumeWithoutToolsWarnsAboutLostCapabilities is the repro: a
// checkpoint from a --tools run, resumed with tools off, must SAY SO.
func TestChatResumeWithoutToolsWarnsAboutLostCapabilities(t *testing.T) {
	ckpt := &deepseek.ClaudeRequest{Tools: chatResumeToolDefs(3)}
	got := chatSeededToolsMismatch(ckpt, false)
	if got == "" {
		t.Fatal("resuming a tool-enabled checkpoint with tools OFF produced no warning — the capability is silently dropped")
	}
	if !strings.Contains(got, "3 tool definition(s)") {
		t.Errorf("the warning does not report what the checkpoint carried:\n%s", got)
	}
	if !strings.Contains(got, chatFlagTools) {
		t.Errorf("the warning does not name %s — it states a problem without the remedy:\n%s", chatFlagTools, got)
	}
	// The remedy must be the flag this verb actually parses: a help string
	// naming a flag no parser accepts is the defect it is warning about.
	if !chatOnceIsREPLFlag(chatFlagTools) {
		t.Errorf("%s is named as the remedy but is not in this verb's accepted vocabulary", chatFlagTools)
	}
}

// TestChatResumeToolsWarningStaysSilentWhenNothingIsLost: the two healthy
// shapes. A warning on either of these would be the boy who cried wolf.
func TestChatResumeToolsWarningStaysSilentWhenNothingIsLost(t *testing.T) {
	cases := []struct {
		name         string
		ckpt         *deepseek.ClaudeRequest
		toolsEnabled bool
	}{
		{"tool-enabled checkpoint resumed WITH --tools", &deepseek.ClaudeRequest{Tools: chatResumeToolDefs(2)}, true},
		{"plain checkpoint resumed plainly (no tools to lose)", &deepseek.ClaudeRequest{}, false},
		{"no checkpoint at all", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatSeededToolsMismatch(c.ckpt, c.toolsEnabled); got != "" {
				t.Errorf("warned when nothing was lost:\n%s", got)
			}
		})
	}
}

// TestChatHelpStatesTheCrashRecoveryRecipe: the briefing's question was "how
// does a user resume a crashed chat" — and the answer has to be findable. The
// page is the only place a user learns the recovery path — `--resume` (which
// restores the options the session was started with) and the seed argument
// (which autodetects them) — and what is NOT restored (a session that recorded
// nothing).
func TestChatHelpStatesTheCrashRecoveryRecipe(t *testing.T) {
	for _, want := range []string{
		"Recovering a chat that CRASHED",
		// The recovery command itself, exactly as this verb parses it.
		"gopi chat <session-id>",
		// The recovery command that restores the options the crashed run had.
		chatFlagResume,
		// What the record restores, and the limitation that remains (a session
		// predating the record).
		"RECORDS the options",
		chatFlagTools,
		// Where a killed-before-any-reply chat is (and is not) visible.
		"gopi session log --sessions",
	} {
		if !strings.Contains(chatUsage, want) {
			t.Errorf("`gopi chat -h` never says %q — the recovery path stays undiscoverable", want)
		}
	}
}
