package agent

// recall_triggers_test.go — the PROMPT-SIDE gate for the recall-trigger work:
// the guidance has to be PRESENT on both surfaces the model reads, with the
// trigger vocabulary and the anti-abuse floor, or the whole mechanism is a
// comment nobody's request carries.
//
// WHY A PRESENCE GATE AND NOT ONLY A BEHAVIOUR TEST. The behaviour half is
// Rule 11's real-run adoption proof (a chat session whose JSONL shows the model
// issuing memory(action=search) with a query of its own composition) — that
// evidence lives in the task report, not in a unit test, because no test can
// drive a real model. What a test CAN pin is the channel: a later compression
// pass, a section reorder, or a limb that stops being appended would silently
// remove the trigger vocabulary while every behaviour claim stayed in the
// report. So this file asserts the text is on the wire, on BOTH limbs, with the
// clauses that carry the semantics (triggers / composed query / floor / the two
// sources the description may promise).

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

// TestMemorySearchDescriptionCarriesRecallTriggers pins the DESCRIPTION half:
// the phrase list, the "compose the query" instruction, the two sources a
// search really covers, and the anti-abuse clause — all on the action the model
// sees re-sent with every request.
func TestMemorySearchDescriptionCarriesRecallTriggers(t *testing.T) {
	mem := toolDefByName(t, toolnames.ToolMemory)
	if mem == nil {
		t.Fatalf("%s missing from claudeToolDefs", toolnames.ToolMemory)
	}
	desc := mem.Description

	// (a) the bullet is still THERE — the recall text rides the search action
	// rather than replacing it (an action that lost its strategy line would be
	// a different defect this test must not paper over).
	if !strings.Contains(desc, "action="+toolnames.ToolMemoryActionSearch) {
		t.Fatalf("memory description no longer describes action=%s", toolnames.ToolMemoryActionSearch)
	}
	if !strings.Contains(desc, memorySearchBullet) {
		t.Fatalf("memory description does not render memorySearchBullet — the search bullet and this constant have " +
			"drifted apart (loop.go must concatenate the constant, never carry a second copy of the text)")
	}

	// (b) the trigger vocabulary — Chinese AND English, because the operator's
	// recall phrases arrive in both and a model that only reads one list answers
	// in the other language from the window.
	for _, want := range []string{
		"RECALL TRIGGERS",
		"你还记得",    // 你还记得我们上次…
		"你忘了",     // 你忘了…
		"我们之前讨论过", // 我们之前讨论过…
		"上次那个",
		"do you remember",
		"you forgot",
		"we discussed this before",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory search description missing recall trigger %q", want)
		}
	}

	// (c) the TRANSLATION instruction: the query is composed, never echoed.
	for _, want := range []string{"compose the query", "NEVER echo the raw sentence"} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory search description missing %q — the model must translate, not echo", want)
		}
	}

	// (d) the two sources ONE call covers, named as things the engine actually
	// merges today (distilled lessons + procedural workflows). This arm is also
	// the guard against over-promising a source the call does not reach.
	for _, want := range []string{"distilled lessons", "procedural workflows"} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory search description missing source %q", want)
		}
	}

	// (e) the ANTI-ABUSE floor — the half that stops "search every round".
	for _, want := range []string{"NOT a trigger", "SECOND search of the same topic", "one clarifying question"} {
		if !strings.Contains(desc, want) {
			t.Errorf("memory search description missing the anti-abuse clause %q", want)
		}
	}
}

// TestChatPromptCarriesRecallGuidance pins the PROMPT half on BOTH chat carriers
// — the plain path and the tool-enabled one — assembled through the same
// functions the request is built from (never a copy of the text).
func TestChatPromptCarriesRecallGuidance(t *testing.T) {
	a := &Agent{SystemPrompt: chatSystemPrompt}
	for _, c := range []struct {
		name string
		got  string
	}{
		{"plain (Agent.Chat)", chatSystemPromptServed(a)},
		{"tools (ChatOps)", chatOpsSystemPrompt(a)},
	} {
		if !strings.Contains(c.got, chatRecallMarker) {
			t.Errorf("%s: the chat prompt carries no recall guidance (%q missing)", c.name, chatRecallMarker)
			continue
		}
		// The trigger vocabulary and the floor — the two clauses the paragraph
		// owes BOTH limbs.
		for _, want := range []string{"你还记得", "you forgot", "Not every mention of the past"} {
			if !strings.Contains(c.got, want) {
				t.Errorf("%s: chat prompt recall guidance missing %q", c.name, want)
			}
		}
		// The TRANSLATION rule belongs to the tools limb alone: the plain path
		// cannot issue a call, so "compose your query" would be an instruction it
		// has no way to act on — the exact false instruction the limb pair exists
		// to avoid.
		if hasTranslate := strings.Contains(c.got, "Translate the query yourself"); hasTranslate != (c.name != "plain (Agent.Chat)") {
			t.Errorf("%s: translation clause present=%v — it belongs to the tools limb only", c.name, hasTranslate)
		}
		// Each limb states what THIS path can do — a plain path told to call a
		// tool it does not carry is the false instruction the limbs exist for.
		limb := chatRecallNoToolsLimb
		if c.name != "plain (Agent.Chat)" {
			limb = chatRecallToolsLimb
		}
		if !strings.Contains(c.got, limb) {
			t.Errorf("%s: chat prompt recall guidance carries the wrong limb", c.name)
		}
	}

	// The RATIONALE — the sentence that makes it a duty rather than a habit.
	if !strings.Contains(chatSystemPromptServed(a), "asking the operator to repeat themselves is the failure being repaired") {
		t.Errorf("chat prompt recall guidance dropped the rationale")
	}
}

// TestRecallGuidanceLivesOutsideLoopGo is the Rule-1/13 half, and it is also the
// ANCHOR half: loop.go is the file 167 devdoc documents cite by line number, so
// the search bullet's TEXT may not be written there. A second copy of the text
// in loop.go would drift from this constant; a text literal there would re-key
// every anchor below it.
func TestRecallGuidanceLivesOutsideLoopGo(t *testing.T) {
	if !strings.Contains(toolDefByName(t, toolnames.ToolMemory).Description, memorySearchBullet) {
		t.Fatal("the description does not come from memorySearchBullet")
	}
	// The trigger vocabulary must not be duplicated anywhere else in the tree:
	// one definition, so a future edit cannot move the description and leave a
	// stale copy teaching the model a second version.
	for _, s := range promptSections {
		if strings.Contains(renderPromptSection(s), "RECALL TRIGGERS") {
			t.Errorf("prompt section %q carries a second copy of the recall trigger list — it belongs to the "+
				"memory tool description only (a worker re-reads the description every request; a task kernel "+
				"sentence would compete for the 4500-char density budget)", s.Name)
		}
	}
}
