package agent

// chat_memory_ownscope_test.go — the 2026-10-14 ruling that memory work is gopi
// chat's OWN SCOPE, asserted against the COMPOSED prompts (the strings the
// request builders send), never against a copy of the paragraph:
//
//  1. BOTH chat paths carry it — the plain path (no memory tool on the wire) and
//     the --tools path — each with the limb that is TRUE of it;
//  2. what each path is told is the AUTHORITY (unconditional, no permission, at
//     any moment) and the FOUR actions `memory` offers, spelled from
//     internal/toolnames, so the paragraph cannot teach a verb the tool rejects;
//  3. the TASK prompt carries NO such clause: a worker's memory contract is its
//     own (the kernel's storeOnTheSpotClause plus eventDrivenStoreTriggers), and
//     "memory is this surface's own scope" is a statement about a CONVERSATION
//     with an operator in it;
//  4. the authority clause precedes the timing clause — a reader who has not been
//     told the work is theirs will not act on WHEN it fires.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

// TestChatPromptStatesMemoryNeedsNoPermission is the acceptance test for the
// 2026-10-14 ruling. It reads the two composition points the request builders
// actually call (chatSystemPromptServed / chatOpsSystemPrompt) and asserts the
// ruling is stated IN WORDS there — not merely implied by a timing rule and not
// delegated to the kernel's tool-routing block, which speaks in the worker's
// voice about a task run.
func TestChatPromptStatesMemoryNeedsNoPermission(t *testing.T) {
	chatBoundaryFixtureRepo(t) // the block renders the repo it stands in

	prompts := map[string]string{
		"plain (Agent.Chat)":    chatSystemPromptServed(nil),
		"tools (Agent.ChatOps)": chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()}),
	}

	for name, prompt := range prompts {
		// (a) The clause is PRESENT, and it is present as a separable thing: its
		// own marker, so "was this run told?" is one substring, not a reading of
		// the whole prose.
		if !strings.Contains(prompt, chatMemoryOwnScopeMarker) {
			t.Fatalf("%s: the composed prompt does not state that memory work is its own "+
				"scope with no permission asked — the reader is left to infer it:\n%s", name, prompt)
		}

		// (b) What it says, in the ruling's own words: the operator's sentence is
		// the authority, and a paraphrase would be a different rule.
		if !strings.Contains(prompt, chatMemoryOwnScopeRuling) {
			t.Errorf("%s: the clause does not carry the operator's ruling verbatim (%q)",
				name, chatMemoryOwnScopeRuling)
		}

		// (c) The four actions. `search` and `store` were named by the 2026-10-09
		// clause; `feedback` and `update` were the ones a reader had to guess at,
		// and they are the ones that make the scope a CATEGORY rather than a
		// pair of chores.
		for _, action := range []string{
			toolnames.ToolMemoryActionStore,
			toolnames.ToolMemoryActionSearch,
			toolnames.ToolMemoryActionFeedback,
			toolnames.ToolMemoryActionUpdate,
		} {
			if !strings.Contains(prompt, action) {
				t.Errorf("%s: the memory clause does not name action %q — the scope is stated "+
					"narrower than the tool it authorises", name, action)
			}
		}
		if !strings.Contains(prompt, toolnames.ToolMemory+"(action=") {
			t.Errorf("%s: the clause does not spell the call the reader is authorised to make", name)
		}

		// (d) The authority words themselves — the claim the test is named for.
		for _, want := range []string{"unconditional", "no permission", "at any moment"} {
			if !strings.Contains(strings.ToLower(prompt), want) {
				t.Errorf("%s: the composed prompt never says %q about memory work — the "+
					"ruling's own force is missing even where its marker is present", name, want)
			}
		}

		// (e) The honest LIMB: the plain path cannot call the tool, so it must not
		// be told to, and the tools path must be.
		if name == "plain (Agent.Chat)" {
			if !strings.Contains(prompt, chatMemoryOwnScopeNoTools) {
				t.Errorf("%s: the plain path must carry the NO-TOOLS limb — memory is not on its wire", name)
			}
			if strings.Contains(prompt, chatMemoryOwnScopeTools) {
				t.Errorf("%s: the plain path carries the TOOLS limb — a false instruction: "+
					"it cannot call memory", name)
			}
			continue
		}
		if !strings.Contains(prompt, chatMemoryOwnScopeTools) {
			t.Errorf("%s: the --tools path must carry the TOOLS limb — memory IS on its wire", name)
		}
		if strings.Contains(prompt, chatMemoryOwnScopeNoTools) {
			t.Errorf("%s: the --tools path carries the NO-TOOLS limb — a false instruction", name)
		}
	}
}

// TestChatMemoryOwnScopePrecedesTheTimingClause pins the ORDER: the AUTHORITY
// comes before the TRIGGER. A paragraph that opened with "the operator's
// instruction is your trigger" while the authority arrived later would read as a
// rule about obeying an instruction rather than a scope the surface owns.
func TestChatMemoryOwnScopePrecedesTheTimingClause(t *testing.T) {
	for _, prompt := range []string{
		chatSystemPromptServed(nil),
		chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()}),
	} {
		authorityAt := strings.Index(prompt, chatMemoryOwnScopeMarker)
		timingAt := strings.Index(prompt, chatMemoryTimingMarker)
		if authorityAt < 0 || timingAt < 0 {
			t.Fatalf("precondition: both clauses must be present (authority=%d, timing=%d)", authorityAt, timingAt)
		}
		if authorityAt > timingAt {
			t.Errorf("the memory AUTHORITY clause renders AFTER the timing clause — read in that order, the "+
				"paragraph teaches obedience to an instruction instead of a scope this surface owns "+
				"(authority@%d, timing@%d)", authorityAt, timingAt)
		}
	}
}

// TestChatMemoryOwnScopeIsNotTheWorkersRule keeps the two surfaces apart. The
// kernel (toolRoutingRules) already forbids one narrow question — "never ask
// 要不要记下来" about a lesson that lands — and that sentence is TRUE of a worker.
// This clause is a different claim about a different surface, and a task run told
// "memory is your own scope, unconditional" would be reading a console's contract
// inside a worktree run whose deliverable is the committed change.
func TestChatMemoryOwnScopeIsNotTheWorkersRule(t *testing.T) {
	task := assembleSystemPrompt()
	if strings.Contains(task, chatMemoryOwnScopeMarker) {
		t.Fatalf("the TASK prompt carries the chat memory-scope clause — the two surfaces' contracts "+
			"are now one:\n%s", task)
	}
	// The kernel's own narrower sentence stays where it belongs: this clause does
	// not replace or duplicate it.
	if !strings.Contains(task, storeOnTheSpotClause) {
		t.Errorf("the kernel no longer carries storeOnTheSpotClause — the chat clause was moved instead of added")
	}
}
