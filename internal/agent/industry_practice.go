package agent

// industry_practice.go — the ABSTRACT-PRINCIPLE layer of the prompt, and the
// authority on what the three layers are.
//
// ---------------------------------------------------------------------------
// THE THREE-LAYER DIVISION (operator's ruling, 2026-09-29)
// ---------------------------------------------------------------------------
// A rule can be true of ONE role, of EVERY engineering discipline, or of ONE
// machine — and each of the three belongs somewhere else. Getting that wrong is
// not a style problem; it is how a prompt rots in three different ways at once.
//
//	LAYER 1 — ROLE-INHERENT (角色固有)
//	  True because of WHAT THE AGENT IS: it has tools, it produces turns, it is
//	  answering a human. The rule needs no argument, no evidence and no
//	  exception, and it holds in any project, in any language, on any host.
//	  Examples already in this prompt: "Use tool_use blocks" (headerRules),
//	  "lead with the result" (efficiencyRules), "no recaps"
//	  (conciseThinkingRules), "never claim to have seen ... something you have
//	  not" (chatSystemPrompt).
//	  HOME: the kernel prompt sections (loop.go's promptSections) — always on,
//	  never configurable. It costs budget on every single turn, so it must be
//	  SHORT: if a sentence needs a justification to be obeyed, it is not this
//	  layer.
//
//	LAYER 2 — ABSTRACT PRINCIPLE (抽象原则)
//	  True of every engineering discipline, but DERIVED — it was learned from a
//	  specific failure, and it would be re-derivable by anyone who hit that
//	  failure. It names a METHOD ("look at prior art first", "find the root
//	  cause before tuning parameters", "prefer zero dependencies"), never a
//	  particular artifact. It survives every repo, every stack and every
//	  operator, which is exactly why it earns kernel space.
//	  Examples: [ENGINEERING RULES] 1-7 (the text2img case-study habits), and
//	  rule 8 added by this file's task.
//	  HOME: the kernel prompt, in the block that owns its subject.
//
//	LAYER 3 — CONCRETE MATERIAL (具体素材)
//	  A PATH, a URL, a version pin, a repo name, a person's name, a local
//	  command, a measured number from ONE incident. It is TRUE AND USEFUL and
//	  it must still NOT be in the kernel prompt — it is meaningless to a future
//	  project, it goes stale without anyone editing it, and it spends kernel
//	  budget that every unrelated task pays.
//	  Examples: the path of a sibling agent project to read for prior art, a
//	  mirror URL to fall back on, "our library is #907", a specific tool's
//	  version.
//	  HOME: memory (memory(action=store)) — retrieved by the task that needs it,
//	  which is precisely the case where it is true; and (Rule 37) a repository
//	  document or an operator's own GOPI.md, for the material that belongs to
//	  one project.
//
//	THE MOVE, WHEN SOMETHING FEELS LIKE IT BELONGS IN THE PROMPT — apply it in
//	this order, because the cheap layers absorb the expensive ones:
//	  1. Does it name an artifact (a path, a repo, a URL, a name, a number from
//	     one incident)? → LAYER 3, memory. Stop here.
//	  2. Is it a method, true of every engineering discipline, that you could
//	     re-derive from the failure it came from? → LAYER 2, kernel prompt.
//	  3. Is it merely what the agent IS, needing no justification? → LAYER 1.
//
//	WHY THE DIVISION IS ENFORCED AND NOT MERELY PREFERRED. Layer-3 material in
//	the kernel is not a cosmetic leak: TestSystemPromptNoGoPiSpecifics (in
//	prompt_test.go) fails the build on project-specific wording in the kernel —
//	"devdoc/", "tasks/", "go build ./", an internal case-study name — because a
//	future project without this one's document layout must still receive a
//	self-sufficient prompt. That gate IS this division, made mechanical. It
//	cannot catch a path in a DIFFERENT project's layout (nothing can, from
//	inside one repo), which is why the move above is a habit the prompt teaches
//	rather than only a test that catches.
//
//	AND WHY LAYER 2 CANNOT SIMPLY BE DEMOTED TO MEMORY EITHER. Memory is
//	RETRIEVED — "search when the task looks familiar" (toolRoutingRules rule a)
//	— so a principle parked there is absent on exactly the tasks that had not
//	yet noticed they needed it. "Look for prior art before building" is worth
//	nothing to a model that has already decided to build; it has to be present
//	BEFORE that decision, on the turn where the decision is made. That
//	timeliness, not the principle's importance, is what buys it kernel space.
// ---------------------------------------------------------------------------
//
// WHAT THIS FILE RENDERS — engineering rule 8, the reuse/prior-art principle,
// in the two forms the two prompt paths can honestly carry.
//
// THE RULE'S SOURCE (recorded because a rule without its origin becomes
// folklore): the operator raised it on 2026-09-29 with a concrete worked
// example — this machine already runs a sibling agent harness in a directory of
// the operator's own, and rebuilding from scratch what that one already does is
// wasted work — plus the tooling that makes "go look at how the industry does
// it" an ACTION rather than advice: the web search and web fetch tools on the
// model's own wire. The EXAMPLE is layer 3 and lives in memory, NOT here (see
// TestIndustryPracticeRulesCarryNoProjectMaterial: this file may not name it).
// What is kernel-legal is the METHOD it demonstrates.
//
// WHY THE RULE IS NOT SIMPLY APPENDED TO [ENGINEERING RULES] IN BOTH PATHS.
// The task path carries the tool routing block, so for it the principle EXTENDS
// a rule it already has ("code_search FIRST with the bare identifier; usages →
// keyword_search" is prior-art search inside the repo) — one numbered line is
// the whole cost, spent where the rule is actionable. A CHAT path carries that
// routing block only when tools are on the wire, so it needs its own carrier,
// and it needs TWO of them: the plain path cannot search ANYTHING, and an
// instruction the reader cannot act on is the worst kind, because it is
// followed (the chat block's own header states it).
//
// RULE 1/13/14 — the tool names come from internal/toolnames, never typed. Rule
// 3/35: no new tool, no script, no resident process, no state file — this is
// text composed at assembly time.

import (
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

// systemPromptKernel is the prompt registry's own join — the KERNEL, without
// the operator's reply-language directive that assembleSystemPrompt prepends.
// It lives in this file rather than beside its caller because loop.go is the
// file the corpus cites by line (870 `loop.go:NNN` references across 166
// documents): moving the kernel's measurement there would re-key them by hand
// and make a pile of unrelated docs stale. The reason survives as churn cost,
// not as a gate — the check that once ASSERTED those citations
// (the devdoc line-anchor gate, whose test resolved them)
// and its remedy for a moved symbol ("re-key by symbol", corpus lesson #3313)
// were deleted with internal/docclass in 5a35f61, so nothing resolves a
// `loop.go:NNN` anchor now. The census that replaced it over CONFIG.md's rows
// (`config-anchor-census.go --check`, TestMergeConfigAnchorGate* arms) went with
// the documentation-citation family in 78bdf05a (2026-11-05), so nothing
// executable checks an anchor on this tree.
func systemPromptKernel() string {
	return joinPromptSections(promptSections)
}

// joinPromptSections assembles a registry into one kernel string: ascending
// Order (stable), empty sections dropped, joined with the registry's separator.
//
// It is a parameterised helper rather than a body inside systemPromptKernel
// because the derivation gate (commands_section_test.go) must drive the REAL
// registry through the REAL join — a test that built its own copy of this loop
// would pass while the assembly drifted (Rule 31: exercise the surface, not a
// stand-in for it).
func joinPromptSections(sections []PromptSection) string {
	sorted := make([]PromptSection, len(sections))
	copy(sorted, sections)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	var parts []string
	for _, s := range sorted {
		text := renderPromptSection(s)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, promptSectionSeparator)
}

// industryPracticeMarker is the chat block's bracketed heading. It is the one
// string that proves which limb reached the ASSEMBLED chat prompt — a test
// asserts the marker AND the limb's own clause, so a block that silently fell
// out of the composition cannot satisfy it (the failure mode
// chatContextRulesFor's doc names for every sub-block it renders).
const industryPracticeMarker = "[REUSE — before you build]"

// engineeringRulesHeading is the [ENGINEERING RULES] block's bracketed heading.
// It is declared beside the marker above and not inside loop.go because the
// test that proves rule 8 landed INSIDE the block (rather than in a section of
// its own) must name the same string the block renders (Rule 1: one definition,
// two readers).
const engineeringRulesHeading = "[ENGINEERING RULES]"

// industryPracticeRule8Number is engineering rule 8's numbered prefix. Kept as
// its own constant because the chat limb renders the clause WITHOUT it (there is
// no numbered list in the chat block), and the strip must be by VALUE, never a
// second hand-matched copy of the prefix (Rule 1/13): a future renumbering then
// moves the task path and the chat path together.
const industryPracticeRule8Number = "8. "

// industryPracticeClause is rule 8's sentence, unnumbered — the form the chat
// block renders. The numbered form is composed from it below, so the two
// surfaces cannot drift into teaching two versions of one principle (the same
// single-definition discipline eventDrivenStoreEventList follows for the
// memory triggers).
const industryPracticeClause = "别重复造轮子: search prior art before building (industry, repo), reuse it — 复用不重做."

// industryPracticeRule8 is engineering rule 8 exactly as it renders inside
// [ENGINEERING RULES]: the shared clause with its list number in front.
const industryPracticeRule8 = industryPracticeRule8Number + industryPracticeClause

// industryPracticeToolsLimb is the chat carrier when tools ARE on the wire (the
// ordinary chatOpsToolDefs surface): there the principle is directly actionable,
// and the four tools that act on it are named from internal/toolnames, so a
// rename fails to compile here instead of leaving the prompt teaching a dead
// word (Rule 13).
const industryPracticeToolsLimb = "On this path that is an ACTION: `" + toolnames.ToolKeywordSearch + "`/`" +
	toolnames.ToolCodeSearch + "` search prior art already in this repo, `" + toolnames.ToolWebSearch + "`/`" +
	toolnames.ToolWebFetch + "` how the industry does it — BEFORE you write a new one. Reuse beats rewrite."

// industryPracticeNoToolsLimb is the chat carrier when NO tools are on the wire.
// It must not teach an invocation this path cannot execute, and it must not let
// the reader claim a search happened: what it CAN do is ask the operator to run
// the search (or answer from what it already knows), and say plainly which of
// the two it did.
const industryPracticeNoToolsLimb = "This path carries NO search tools, so you cannot look anything up this turn: " +
	"say what you would search for and ask the operator to run it — or answer from what you already know — and " +
	"never claim you checked prior art when you did not."

// industryPracticeRules renders the chat carrier for one limb. The two limbs are
// the honesty requirement (Rule 15), matching the precedent set by
// chatNoToolsLimb/chatToolsLimb, chatMemoryTimingRules and
// chatWriteBoundaryRules: a surface that cannot act must be told what is true of
// it, not handed the other surface's instruction.
func industryPracticeRules(toolsOn bool) string {
	var b strings.Builder
	b.WriteString(industryPracticeMarker)
	b.WriteString(" ")
	b.WriteString(industryPracticeClause)
	b.WriteString(" ")
	if toolsOn {
		b.WriteString(industryPracticeToolsLimb)
		return b.String()
	}
	b.WriteString(industryPracticeNoToolsLimb)
	return b.String()
}
