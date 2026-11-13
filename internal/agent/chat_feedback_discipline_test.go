package agent

// chat_feedback_discipline_test.go — the CHAT prompt's half of the memory
// FEEDBACK discipline, asserted against the COMPOSED assemblies (the strings the
// request builders actually send), not against a copy of the text.
//
// WHY THIS FILE EXISTS (the claim it settles). A sibling task asserted "the chat
// system prompt carries no toolRoutingRules, so a chat turn is never told to vote
// on the hits it reads" — a reading of the CODE, not a measurement. The
// measurement says otherwise on both limbs, and the pair is lopsided enough that
// only a test can hold it:
//
//   - the --tools path (ChatOps) composes `a.SystemPrompt` + the chat block
//     (chatOpsSystemPrompt, chat_ops.go), and every production carrier builds
//     that agent through newAgent → assembleSystemPrompt() — so the kernel's
//     memory if-then block, clause c included, is on the chat wire;
//   - the plain path (Agent.Chat) sends chatSystemPromptServed(), which is the
//     persona + the chat block and NOTHING ELSE — correctly, because that path
//     advertises no tools at all and a vote it cannot cast is the false
//     instruction the chat block's own limbs exist to avoid.
//
// The coupling the --tools limb rests on is a CALLER CONTRACT, not a property of
// the assembly: chatOpsSystemPrompt reads the base from a.SystemPrompt, so a
// future edit that dropped that argument (or a carrier that built its agent
// without it) would strip the vote discipline off the chat wire while
// chatOpsToolDefs still advertised the memory tool — the model would hold a tool
// it was never told how to use, and nothing would go red. These assertions are
// that red.
//
// RULE 1/13/14 — the discipline is ONE definition and this test proves it by
// READING it out of the kernel string the task path ships (extractMemoryVoteRule)
// instead of re-typing a copy: a second, differently-worded paragraph in the chat
// block would leave this test green while the two surfaces told different LLMs
// different rules — so the SAME test also asserts the sentence occurs exactly
// once in the composed prompt (a duplicate is the drift, not a miss).
//
// RULE 3/35: it adds no prompt text, no tool, no script and no switch. The
// finding is that the discipline already reaches the chat wire through the SAME
// constant the worker reads; what was missing was the guard.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/toolnames"
)

// memoryVoteRuleHead opens memory clause c of the kernel's if-then block (loop.go
// toolRoutingRules). It is the NEEDLE this file extracts by, and it is kept as a
// prefix rather than the whole sentence on purpose: the sentence's tail (the
// useless→feedback+update consequence) is what a future compression would most
// plausibly reword, and the claim under test is about the SENTENCE as written.
const memoryVoteRuleHead = "  c. IMMEDIATELY after each memory(action=search) vote memory(action=feedback, rating=useful/useless/unused)"

// memoryVoteConsequence is the clause's second half — the CONSEQUENCE half, and
// the one the prefetch epic actually turns on: a hit voted useless is corrected
// and the pair is dropped. It is asserted as its own needle so a compression that
// kept "vote ... id=#N" while dropping the consequence cannot pass.
const memoryVoteConsequence = "memory(action=feedback, rating=useless)+memory(action=update)"

// extractMemoryVoteRule returns memory clause c VERBATIM out of the kernel
// string the task path ships, or "" when the clause is not there. Reading it out
// of the shipped text (rather than declaring a copy here) is the whole point:
// the chat assertions below then say "the chat wire carries THIS sentence",
// which cannot drift from the worker's.
func extractMemoryVoteRule() string {
	for _, line := range strings.Split(toolRoutingRules, "\n") {
		if strings.HasPrefix(line, memoryVoteRuleHead) {
			return line
		}
	}
	return ""
}

// TestMemoryVoteRuleIsOneDefinition is the Rule 1/13/14 floor: the clause the
// chat assertions below re-read must actually exist in the kernel, with both
// halves (the vote and its consequence). Without this, every assertion in this
// file could pass vacuously against an empty extraction.
func TestMemoryVoteRuleIsOneDefinition(t *testing.T) {
	rule := extractMemoryVoteRule()
	if rule == "" {
		t.Fatalf("the memory vote rule is not a line of toolRoutingRules any more:\n%s", toolRoutingRules)
	}
	if !strings.Contains(rule, memoryVoteConsequence) {
		t.Errorf("the vote rule lost its consequence half (%q) — a vote with no consequence is the failure the prefetch epic measured", memoryVoteConsequence)
	}
	if !strings.Contains(rule, "id=#N") {
		t.Errorf("the vote rule does not name the record id (#N) the vote is keyed on:\n%s", rule)
	}
}

// TestChatOpsPromptCarriesTheMemoryVoteDiscipline is the --tools limb: the
// composed prompt a real tool-enabled chat turn sends carries the vote rule
// VERBATIM, and carries it exactly ONCE.
//
// The fixture is the production shape, and deliberately so: `Agent{SystemPrompt:
// assembleSystemPrompt()}` is what newAgent produces (loop.go), and every
// ChatOps carrier (cli_chat.go, cli_web.go, term.go) builds its agent that way.
// Asserting against a hand-written SystemPrompt would prove nothing about the
// shipped path.
func TestChatOpsPromptCarriesTheMemoryVoteDiscipline(t *testing.T) {
	chatBoundaryFixtureRepo(t)
	rule := extractMemoryVoteRule()
	if rule == "" {
		t.Fatal("no vote rule in the kernel — see TestMemoryVoteRuleIsOneDefinition")
	}

	got := chatOpsSystemPrompt(&Agent{SystemPrompt: assembleSystemPrompt()})
	if !strings.Contains(got, rule) {
		t.Fatalf("the --tools chat prompt does not carry the kernel's vote rule verbatim — the chat wire advertises the memory tool without telling the model it may vote on what it reads:\nwant %q", rule)
	}
	// EXACTLY once: the chat block must not grow a second copy of its own. The
	// kernel's line is the ONE definition, and a duplicated paragraph is the
	// drift this assertion exists for (Rule 1/13/14) — two texts for one rule
	// drift the moment either is edited.
	if n := strings.Count(got, memoryVoteRuleHead); n != 1 {
		t.Errorf("the vote rule occurs %d times in the --tools chat prompt, want exactly 1 (the kernel's) — a second copy is a second definition", n)
	}
}

// TestPlainChatPromptCarriesNoMemoryVoteDiscipline is the no-tools limb, and it
// asserts the ABSENCE on purpose. The plain path (Agent.Chat) sends no tool
// definitions at all: it cannot call memory(action=feedback), so a vote
// instruction there would be a false instruction — the failure mode the chat
// block's own limbs (chatNoToolsLimb/chatToolsLimb, chatMemoryTimingNoToolsLimb)
// already name as the worst kind "because it is followed".
//
// It is asserted together with the presence of the chat block itself, so the
// absence cannot be satisfied by an assembly that silently stopped rendering the
// block: "no vote rule" must mean "the chat surface was told what it is, and the
// vote is not among the things it was told".
func TestPlainChatPromptCarriesNoMemoryVoteDiscipline(t *testing.T) {
	chatBoundaryFixtureRepo(t)
	plain := chatSystemPromptServed(nil)
	if !strings.Contains(plain, chatContextHead) {
		t.Fatalf("the plain chat prompt carries no chat block, so this absence proves nothing:\n%s", plain)
	}
	if strings.Contains(plain, memoryVoteRuleHead) {
		t.Errorf("the plain chat prompt instructs a vote it cannot cast — that path advertises no tools:\n%s", head(plain))
	}
	if strings.Contains(plain, toolRoutingRules) {
		t.Errorf("the plain chat prompt carries the kernel's tool-routing block, which routes to tools it does not have:\n%s", head(plain))
	}
}

// TestTaskPromptStillCarriesTheVoteDisciplineOnce is the control: the guard above
// must not have been satisfied by moving the rule out of the kernel. The task
// assembly — the surface the rule was written for — carries it, and carries it
// once.
func TestTaskPromptStillCarriesTheVoteDisciplineOnce(t *testing.T) {
	kernel := systemPromptKernel()
	if !strings.Contains(kernel, extractMemoryVoteRule()) {
		t.Fatalf("the kernel no longer carries the vote rule — the chat --tools limb would be covered by accident:\n%s", kernel)
	}
	// And the chat blocks the plain path DOES carry are absent from it, which is
	// the other direction of the same scope claim (chat_context_test.go states it
	// for the block as a whole; this file only re-states it for the memory
	// paragraphs the prompt paths could most plausibly share by accident).
	if strings.Contains(kernel, chatMemoryTimingMarker) {
		t.Errorf("the TASK prompt carries the chat memory-timing paragraph")
	}
}

// TestChatOpsToolWireAlwaysAdvertisesMemory pins the OTHER half of the coupling
// the assertions above depend on, and records the honest limit: the tool wire is
// built from a.chatOpsToolDefs() unconditionally (it is claudeToolDefs plus the
// chat-scoped def), while the vote DISCIPLINE reaches the same request only
// through a.SystemPrompt. So the two halves come from the caller together — a
// hand-built Agent that carries neither is coherent, and a hand-built Agent that
// carried the tool but not the prompt would be a chat advertising a vote it was
// never told about.
//
// That degraded shape is unreachable from the shipped carriers (all three go
// through newAgent, which sets SystemPrompt from assembleSystemPrompt), which is
// why this test PINS the invariant instead of adding a fallback paragraph: a
// prompt block here would be a second definition of a rule the kernel already
// states, and the point of the epic's chosen shape (A1) is that no second
// definition is needed. The assertion is what makes the coupling visible to the
// next reader rather than implicit.
func TestChatOpsToolWireAlwaysAdvertisesMemory(t *testing.T) {
	ag := &Agent{}
	var onWire bool
	for _, d := range ag.chatOpsToolDefs() {
		if d.Name == toolnames.ToolMemory {
			onWire = true
		}
	}
	if !onWire {
		t.Fatalf("the chat tool wire no longer advertises the memory tool — this file's chat-limb claims are about a tool that is not there")
	}
	if bare := chatOpsSystemPrompt(ag); strings.Contains(bare, memoryVoteRuleHead) {
		t.Errorf("an Agent with no SystemPrompt produced a vote rule from nowhere — the discipline has grown a second source:\n%s", head(bare))
	}
}
