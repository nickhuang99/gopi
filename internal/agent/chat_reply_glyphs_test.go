package agent

// chat_reply_glyphs_test.go — the ellipsis rule's battery, driven from the
// composition points the request builders call (never from a copy of the text),
// plus the RED PROOF: the test names the mutation that makes it fail, so the
// paragraph is a removable thing under assertion rather than a tautology about a
// constant.
//
// SCOPE, asserted rather than claimed. The rule is a CHAT rule and it must stay
// one: the task kernel is bounded by systemPromptDensityBudget (4500 bytes) and
// every worker request pays for it, while this artifact (a reply body copied out
// of a chat grid) exists only on the chat surface. The last arm asserts the task
// prompt carries none of it — the same shape chat_context_test.go's
// TestTaskPromptCarriesNoChatContext uses for the block as a whole.

import (
	"strings"
	"testing"
)

// TestChatPromptForbidsEllipsisInReplies is the acceptance test: both chat
// carriers tell the model how an answer body may be written, and the task
// assembly is told nothing.
//
// RED PROOF (measured, see the task report's Evidence section). Delete the
// chatReplyGlyphRules() call from chatContextRulesFor and re-run this test: the
// two presence arms fail with "the plain chat prompt carries no reply-glyph
// paragraph", and the fixture arms below are unchanged — which is what makes the
// failure the paragraph's and not the fixture's.
func TestChatPromptForbidsEllipsisInReplies(t *testing.T) {
	chatBoundaryFixtureRepo(t) // the block renders the repo it stands in

	// NON-VACUITY FIRST: a test whose subject does not exist would pass by
	// accident on every arm that only asks "is it absent from the task prompt".
	rules := chatReplyGlyphRules()
	if strings.TrimSpace(rules) == "" {
		t.Fatal("the reply-glyph paragraph is empty — every presence arm below would be vacuous")
	}
	// The rule names the glyph BOTH ways it really appears: the single character
	// and the ASCII triple dot. A paragraph teaching only one of them leaves the
	// other in daily use (the same reason a banned-verb list is rendered whole).
	for _, glyph := range []string{"…", "..."} {
		if !strings.Contains(rules, glyph) {
			t.Errorf("the paragraph never spells the elided form %q — the reader cannot match a rule against a glyph it cannot see", glyph)
		}
	}
	// The REASON travels with the prohibition: without it the sentence is one
	// more style preference, and the mechanism (the cell is used as typed) is
	// what the reader can check its own sentence against.
	if !strings.Contains(rules, "as typed") {
		t.Errorf("the paragraph states no mechanism — a prohibition with no reason is a preference:\n%s", rules)
	}

	// THE TWO CARRIERS. Both chat paths assemble through the chat-context block,
	// so both must carry the paragraph, verbatim and exactly once.
	promptTests := []struct {
		name, prompt, limb string
	}{
		{"plain (Agent.Chat)", chatSystemPromptServed(nil), chatNoToolsLimb},
		{"tools (Agent.ChatOps)", chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()}), chatToolsLimb},
	}
	for _, tc := range promptTests {
		if !strings.Contains(tc.prompt, chatReplyGlyphMarker) {
			t.Fatalf("%s: the assembled chat prompt carries no reply-glyph paragraph (%q missing):\n%s",
				tc.name, chatReplyGlyphMarker, tc.prompt)
		}
		if !strings.Contains(tc.prompt, rules) {
			t.Errorf("%s: the paragraph is not carried VERBATIM — the prompt renders different bytes than the constant that defines it:\n%s",
				tc.name, tc.prompt)
		}
		if n := strings.Count(tc.prompt, chatReplyGlyphMarker); n != 1 {
			t.Errorf("%s: the paragraph is rendered %d times, want exactly once — a rule the model reads twice reads as two rules:\n%s",
				tc.name, n, tc.prompt)
		}
		// The limb each path really carries, asserted here so a failure above can
		// be told apart from "the block was assembled for the other path".
		if !strings.Contains(tc.prompt, tc.limb) {
			t.Errorf("%s: the prompt does not carry its own limb — the fixture is assembling the other path:\n%s", tc.name, tc.prompt)
		}
	}

	// THE BLOCK ITSELF, un-parameterised: the paragraph is NOT limb-gated.
	// Unlike the write boundary and the PA duty (which instruct actions only the
	// tool-enabled path can take), how the text of an answer is written is true
	// of both limbs, so one rendering is the correct shape and a second would be
	// the same bytes twice.
	for _, toolsOn := range []bool{false, true} {
		if block := chatContextRulesFor(toolsOn); !strings.Contains(block, chatReplyGlyphMarker) {
			t.Errorf("chatContextRulesFor(%v) does not render the reply-glyph paragraph — the rule is limb-gated, but nothing in it needs a tool", toolsOn)
		}
	}

	// SCOPE: the task kernel must stay clean. A chat-only artifact rule folded
	// into it would spend worker budget on a surface no worker runs.
	if task := assembleSystemPrompt(); strings.Contains(task, chatReplyGlyphMarker) {
		t.Fatalf("the TASK prompt carries the chat reply-glyph rule — it belongs to the chat block, and the kernel has its own byte budget:\n%s", task)
	}
}
