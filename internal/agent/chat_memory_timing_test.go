package agent

// chat_memory_timing_test.go — the chat ONLY memory-timing rule, asserted against
// the COMPOSED prompts (the strings the request builders send), not against a
// copy of the paragraph:
//
//  1. BOTH chat paths carry it, each with the limb that is TRUE of that path —
//     the plain path (no tools on the wire) must never be told to call a tool it
//     does not have, and the tool-enabled path must be told the moment is
//     actionable here;
//  2. the TASK prompt carries NEITHER (a worker's timing is "summarize at task
//     end"; the event list is already on its own two surfaces — the task-start
//     checkpoint and the memory tool description — which is what
//     TestEventDrivenStoreTriggers_SurviveInBothSurfaces guards);
//  3. the event list is the ONE definition (eventDrivenStoreEventList), rendered
//     verbatim, so the chat block cannot drift into telling the model a
//     different trigger set than the tool description does — AND the chat block
//     REFUSES eventDrivenStoreTaskEndClause, the worker-only "task end is the
//     floor" framing, which is false on a surface with no task end.

import (
	"strings"
	"testing"
)

// TestChatPromptsCarryTheMemoryTimingRule is the prompt half of the epic's third
// rule: the composed output of BOTH chat assemblies carries the timing paragraph
// with the limb that is true of it, and the task assembly carries neither limb.
func TestChatPromptsCarryTheMemoryTimingRule(t *testing.T) {
	chatBoundaryFixtureRepo(t)

	plain := chatSystemPromptServed(nil)
	if !strings.Contains(plain, chatMemoryTimingMarker) {
		t.Fatalf("the plain chat prompt carries no memory-timing rule:\n%s", plain)
	}
	if !strings.Contains(plain, chatMemoryTimingNoToolsLimb) {
		t.Errorf("the plain chat prompt must carry the NO-TOOLS limb — memory is not on its wire")
	}
	if strings.Contains(plain, chatMemoryTimingToolsLimb) {
		t.Errorf("the plain chat prompt carries the TOOLS limb — a false instruction: it cannot call memory")
	}

	toolsPath := chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()})
	if !strings.Contains(toolsPath, chatMemoryTimingMarker) {
		t.Fatalf("the --tools chat prompt carries no memory-timing rule:\n%s", toolsPath)
	}
	if !strings.Contains(toolsPath, chatMemoryTimingToolsLimb) {
		t.Errorf("the --tools chat prompt must carry the TOOLS limb — memory IS on its wire")
	}
	if strings.Contains(toolsPath, chatMemoryTimingNoToolsLimb) {
		t.Errorf("the --tools chat prompt carries the NO-TOOLS limb — a false instruction")
	}

	// The worker-only deadline. This is a BEHAVIOUR claim, not a style one: the
	// shared event contract used to carry "(task end is the floor, not the
	// ceiling)" inside the string the chat block rendered, so a chat turn was
	// told its floor was a moment it does not have — while the very next
	// sentence of the same paragraph said there is no task end here. The split
	// in storetriggers.go is what allows the block to cite the EVENTS and refuse
	// the DEADLINE; this assertion is the guard that the refusal survives.
	for name, block := range map[string]string{"plain": plain, "tools": toolsPath} {
		if strings.Contains(block, eventDrivenStoreTaskEndClause) {
			t.Errorf("the %s chat prompt carries the worker's task-end clause %q — a chat turn has no task end to defer to",
				name, eventDrivenStoreTaskEndClause)
		}
	}
	if !strings.Contains(toolsPath, eventDrivenStoreEventList) {
		t.Errorf("the --tools chat prompt carries no event list")
	}

	// The task path. It gets the event contract from its OWN two surfaces (the
	// task-start checkpoint and the memory tool description), so a chat paragraph
	// here would be the wrong behaviour mode in the worker's mouth.
	if task := assembleSystemPrompt(); strings.Contains(task, chatMemoryTimingMarker) {
		t.Fatalf("the TASK prompt carries the chat memory-timing paragraph:\n%s", task)
	} else if strings.Contains(task, chatMemoryTimingNoToolsLimb) ||
		strings.Contains(task, chatMemoryTimingToolsLimb) {
		t.Fatalf("the TASK prompt carries a chat timing LIMB — the paragraph's marker was dropped but its text was not")
	}
}

// TestChatMemoryTimingCarriesTheSharedEventContract is the Rule 1/13/14 half: the
// trigger events are RENDERED from the shared definition, not re-typed, and the
// paragraph states the contrast that keeps the two behaviour modes apart (a chat
// has no task end to defer to). A re-typed copy would pass a "the prompt mentions
// 记住" check while drifting from the list the memory tool description and the
// task-start checkpoint send.
func TestChatMemoryTimingCarriesTheSharedEventContract(t *testing.T) {
	chatBoundaryFixtureRepo(t)
	if !strings.Contains(eventDrivenStoreEventList, "记住") {
		t.Fatalf("precondition: eventDrivenStoreEventList no longer names the 记住 event — this test's anchor moved")
	}
	// Precondition on the SPLIT itself: the full worker contract must still be
	// the two halves joined, or the task surfaces silently lost text when the
	// constant was divided.
	if eventDrivenStoreTriggers != eventDrivenStoreEventList+" "+eventDrivenStoreTaskEndClause {
		t.Fatalf("eventDrivenStoreTriggers is no longer events+clause — the task surfaces' contract changed:\n%q", eventDrivenStoreTriggers)
	}

	for name, block := range map[string]string{
		"no tools limb": chatContextRulesFor(false),
		"tools limb":    chatContextRulesFor(true),
	} {
		if !strings.Contains(block, eventDrivenStoreEventList) {
			t.Errorf("%s: the block does not render the shared event list verbatim — the chat contract and the tool description have drifted apart", name)
		}
		if strings.Contains(block, eventDrivenStoreTaskEndClause) {
			t.Errorf("%s: the block renders the worker's task-end clause — false on a surface with no task end", name)
		}
		if !strings.Contains(block, chatMemoryWorkerContrast) {
			t.Errorf("%s: the block loses the worker contrast — nothing keeps a chat turn from reading task-end semantics", name)
		}
		if !strings.Contains(block, chatMemoryStoreAction()) {
			t.Errorf("%s: the block does not name the action %q built from the leaf constants", name, chatMemoryStoreAction())
		}
	}
}
