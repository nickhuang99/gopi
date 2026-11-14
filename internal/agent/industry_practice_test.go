package agent

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

// TestSystemPromptEngineeringRule8 sits on the TASK kernel: engineering rule 8
// (the reuse/prior-art principle) must reach the assembled prompt, and must
// reach it INSIDE [ENGINEERING RULES] — appended to the block that owns the
// derived habits, not floating as a section of its own. A section of its own
// would cost the density gate a second heading and would read as a tenth
// discipline rather than as the eighth habit.
func TestSystemPromptEngineeringRule8(t *testing.T) {
	p := systemPromptKernel()
	if !strings.Contains(p, industryPracticeRule8) {
		t.Fatalf("kernel prompt does not carry engineering rule 8 verbatim (%q)", industryPracticeRule8)
	}
	// The rule is rendered INSIDE the block: after its heading, and with no
	// other section heading in between (rule 8 is appended after rule 7, so the
	// check is "nothing closed the block between the heading and the rule" —
	// comparing indices against rule 7's position would be wrong, because rule 8
	// is deliberately LAST).
	blockStart := strings.Index(p, engineeringRulesHeading)
	if blockStart < 0 {
		t.Fatalf("kernel prompt lost the %s heading", engineeringRulesHeading)
	}
	ruleAt := strings.Index(p, industryPracticeRule8)
	if ruleAt < blockStart {
		t.Fatalf("engineering rule 8 renders BEFORE the %s heading — it belongs inside the derived-habits block",
			engineeringRulesHeading)
	}
	between := p[blockStart+len(engineeringRulesHeading) : ruleAt]
	if i := strings.Index(between, "\n["); i >= 0 {
		t.Errorf("a section heading (%q) sits between %s and engineering rule 8 — the rule landed outside the block",
			strings.TrimSpace(between[i:]), engineeringRulesHeading)
	}
	// Rule 1/13: the numbering is composed from ONE constant, so the numbered
	// and unnumbered forms cannot drift. This is the assertion that fails if
	// someone re-types the clause into the chat block.
	if !strings.HasPrefix(industryPracticeRule8, industryPracticeRule8Number) {
		t.Errorf("industryPracticeRule8 does not begin with %q — the [ENGINEERING RULES] list numbering drifted",
			industryPracticeRule8Number)
	}
	if !strings.Contains(industryPracticeRule8, industryPracticeClause) {
		t.Errorf("the numbered form does not contain the shared clause — the two forms have forked")
	}
}

// TestIndustryPracticeRulesCarryNoProjectMaterial is the LAYER-3 GUARD, and the
// concrete half of the three-layer division this file's authority comment
// states: the abstract principle is kernel-legal, the material that motivated it
// (a path, a project name, an operator's own directory) is NOT, because it is
// meaningless to a future project, goes stale unwatched, and spends budget every
// unrelated task pays. The material lives in memory; this test is what stops it
// being folded back in by the next person who learns where it is.
//
// The list is the class, not one instance: any absolute path, home reference,
// URL scheme, or a project name in this file's rendered text is a leak.
func TestIndustryPracticeRulesCarryNoProjectMaterial(t *testing.T) {
	rendered := industryPracticeRules(true) + "\n" + industryPracticeRules(false) + "\n" + industryPracticeRule8
	for _, banned := range []string{
		"/home/", "~/", "$HOME", // absolute/home paths — someone's machine
		"http://", "https://", // a URL — a resource that moves
		"devdoc/", "tasks/", // this repo's document layout (NoGoPiSpecifics' class)
	} {
		if strings.Contains(rendered, banned) {
			t.Errorf("the rendered rule carries project material %q — concrete material (layer 3) belongs in "+
				"memory(action=store), never the kernel: it is meaningless to a future project and costs every "+
				"task budget", banned)
		}
	}
}

// TestIndustryPracticeRulesBothLimbs is the honest-limb guard (Rule 15), the
// same contract chatNoToolsLimb/chatToolsLimb, chatMemoryTimingRules and
// chatWriteBoundaryRules carry. The two paths must differ, and each must say
// what is TRUE OF IT:
//
//   - the tool-enabled limb names the tools that make the principle an ACTION,
//     through internal/toolnames (Rule 13 — a rename must fail to compile here
//     rather than leave the prompt teaching a dead word), and must NOT tell the
//     reader it cannot search;
//   - the plain limb must NOT name any tool as invocable (it can reach none) and
//     must forbid the false claim, because "never claim you checked prior art
//     when you did not" is the failure this half exists to prevent.
func TestIndustryPracticeRulesBothLimbs(t *testing.T) {
	tools, plain := industryPracticeRules(true), industryPracticeRules(false)

	// Both limbs carry the heading and the shared principle: the two paths must
	// not disagree about the principle itself, only about what to do with it.
	for name, limb := range map[string]string{"tools": tools, "no-tools": plain} {
		if !strings.Contains(limb, industryPracticeMarker) {
			t.Errorf("%s limb is missing the block marker %q", name, industryPracticeMarker)
		}
		if !strings.Contains(limb, industryPracticeClause) {
			t.Errorf("%s limb is missing the shared principle clause", name)
		}
	}

	// The tool-enabled limb is affirmative and names all four tools.
	for _, tool := range []string{
		toolnames.ToolKeywordSearch, toolnames.ToolCodeSearch,
		toolnames.ToolWebSearch, toolnames.ToolWebFetch,
	} {
		if !strings.Contains(tools, tool) {
			t.Errorf("tool-enabled limb does not name %q — the principle is not actionable without it", tool)
		}
	}
	if !strings.Contains(tools, "BEFORE you write a new one") {
		t.Error("tool-enabled limb does not place the search BEFORE the build — that ordering is the whole rule")
	}
	if strings.Contains(tools, industryPracticeNoToolsLimb) {
		t.Error("tool-enabled limb carries the no-tools sentence — a FALSE instruction is the worst kind, because it is followed")
	}

	// The plain limb says what is true of it, and promises nothing.
	if !strings.Contains(plain, "carries NO search tools") {
		t.Error("no-tools limb does not state that this path cannot search")
	}
	if !strings.Contains(plain, "never claim you checked prior art") {
		t.Error("no-tools limb does not forbid the false prior-art claim")
	}
	for _, tool := range []string{toolnames.ToolWebSearch, toolnames.ToolWebFetch} {
		if strings.Contains(plain, tool) {
			t.Errorf("no-tools limb names %q — it can reach no tool, so naming one teaches an invocation "+
				"this path cannot run", tool)
		}
	}
	if tools == plain {
		t.Fatal("the two limbs render identical text — the parameterisation is dead")
	}
}

// TestChatPromptsCarryIndustryPractice asserts the principle on the chat path
// through the SAME composition functions the request is built from (Rule 31:
// the surface under test is the surface that ships): the plain chat prompt
// (assembleChatSystemPrompt — no tools on the wire) and the operations chat
// prompt (chatOpsSystemPrompt — the ordinary tool surface). Each must carry its
// own limb, and neither may carry the other's.
func TestChatPromptsCarryIndustryPractice(t *testing.T) {
	plain := assembleChatSystemPrompt(&Agent{})
	if !strings.Contains(plain, industryPracticeMarker) {
		t.Fatalf("plain chat prompt is missing the %q block", industryPracticeMarker)
	}
	if !strings.Contains(plain, industryPracticeNoToolsLimb) {
		t.Error("plain chat prompt does not carry the NO-TOOLS limb")
	}
	if strings.Contains(plain, "BEFORE you write a new one") {
		t.Error("plain chat prompt carries the tool-enabled limb — it has no tools on the wire")
	}

	ops := chatOpsSystemPrompt(&Agent{SystemPrompt: "base"})
	if !strings.Contains(ops, industryPracticeMarker) {
		t.Fatalf("ops chat prompt is missing the %q block", industryPracticeMarker)
	}
	if !strings.Contains(ops, industryPracticeToolsLimb) {
		t.Error("ops chat prompt does not carry the TOOL-ENABLED limb")
	}
	if strings.Contains(ops, industryPracticeNoToolsLimb) {
		t.Error("ops chat prompt carries the no-tools limb — bash/web_search are on that wire")
	}
}

// TestEngineeringRule8IsNotReTyped asserts the single-definition discipline
// (Rule 1/13/14): the shared clause exists in exactly one place, and the chat
// block renders it WITHOUT the [ENGINEERING RULES] numbering (there is no
// numbered list in the chat block). A second hand-typed copy would satisfy every
// other test in this file while letting the two prompts drift apart.
func TestEngineeringRule8IsNotReTyped(t *testing.T) {
	kernel := systemPromptKernel()
	if n := strings.Count(kernel, industryPracticeClause); n != 1 {
		t.Errorf("the shared clause renders %d times in the kernel, want exactly 1 — a re-typed copy has appeared", n)
	}
	chat := industryPracticeRules(true)
	if strings.Contains(chat, industryPracticeRule8Number+industryPracticeClause) {
		t.Errorf("the chat block renders the NUMBERED form — the chat block has no numbered list, so the "+
			"number is stripped by value (%q), never re-typed", industryPracticeRule8Number)
	}
	if !strings.Contains(chat, industryPracticeClause) {
		t.Error("the chat block lost the shared clause while stripping the number")
	}
}
