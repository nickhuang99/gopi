package agent

// recorddiscipline.go — the two RECORD DISCIPLINES, each with ONE definition
// here, rendered into the kernel prompt through the EXISTING assembly path: the
// memory lines of toolRoutingRules and the numbering of [ENGINEERING RULES]
// (both in loop.go). No new prompt mechanism, no new tool (Rule 3), no script /
// config key / state file (Rule 35).
//
// WHY THESE TWO, AND WHY THEY ARE NOT ALREADY COVERED BY WHAT THE KERNEL SAYS.
//
//	(1) STORE ON THE SPOT, WITHOUT ASKING PERMISSION. The kernel already told
//	    the model WHEN to store ("mid-task notable find/pitfall →
//	    memory(action=store) NOW", plus the event list in storetriggers.go).
//	    What no surface said is that the model must not OUTSOURCE THE DECISION:
//	    the observed failure shape is a turn that ends by offering the lesson —
//	    "要不要我记下来 / want me to remember this?" — which turns something the
//	    model already holds into a QUESTION. A question waits for a round-trip
//	    that may never come, and the context holding the lesson may be compacted
//	    before the answer does: 未记录=丢失. The discipline is therefore the
//	    AUTHORITY half of the store rule that was already there, not a new
//	    trigger — which is why it renders INSIDE the memory block's if-then list
//	    (its subject is that block's subject) instead of in a section of its own.
//
//	(2) EXISTS ≠ WORKS: EVIDENCE BY EXECUTION, NOT BY SYMBOL. The kernel carried
//	    the instrument half ("Call code_search FIRST with the bare identifier,
//	    not shell grep") — i.e. WHICH tool to search with — and said nothing
//	    about what a hit ENTITLES the model to conclude. The failure it names is
//	    the jump from "I found the symbol" to "the feature is implemented": a
//	    claim about BEHAVIOUR inferred from a claim about TEXT. Both error
//	    directions are measured, not hypothetical — a 假阴性, where the code and
//	    its tests are all present and the effect never reaches the user, and a
//	    假阳性, where the hit that looked like the feature working was a
//	    coincidence the run never separated from success. It renders as a
//	    NUMBERED ENGINEERING RULE because that is what it is: a method, true of
//	    every discipline, derived from a failure (the LAYER 2 division documented
//	    at length in industry_practice.go) — and because [ENGINEERING RULES]
//	    already carries its sibling, rule 5's "every bug fix ships a minimal repro
//	    test", whose whole point is verification by execution.
//
// RULE 1/13/14 — every clause is a named constant, and the two render sites
// COMPOSE from them (`... NOW — ` + storeOnTheSpotClause, and
// engineeringRulesBase + rule 8 + rule 9). The test that proves each discipline
// reached the KERNEL therefore reads the same string the prompt renders: a rule
// that exists only as an unreferenced constant is the failure mode
// selfKnowledgeRules documents for a capability that was built and invisible.
//
// WHY loop.go's LINE COUNT IS NOT MOVED, and why this file exists at all.
// loop.go is the file the devdoc corpus cites by line: 870 `loop.go:NNN`
// references across 166 documents. ONE inserted line re-keys every citation
// below it, and re-pointing them is hand work in files this change has nothing
// to say about. Both render sites above are therefore EDITS ON EXISTING LINES
// (b's trailing sentence; engineeringRules' concatenation), and the constants
// live in this file for the reason storetriggers.go and industry_practice.go
// state: text composed next to the registry keeps the docs' line references
// true.
//
// HISTORY, so nobody satisfies a check that does not exist. Those references
// USED to be enforced — the devdoc line-anchor gate failed when one drifted
// onto a blank line or off its symbol; it was deleted whole with internal/docclass
// in 5a35f61. Nothing
// resolves a `loop.go:NNN` citation today, so the cost above is the cost of
// stale prose, not a red gate. The anchor check that replaced it — the
// config-anchor census over CONFIG.md's rows (`config-anchor-census.go --check`,
// TestMergeConfigAnchorGate* arms) — was deleted with the documentation-citation
// family in 78bdf05a (2026-11-05). Nothing executable checks an anchor now.
//
// DENSITY. TestSystemPromptDensity bounds the kernel at 4500 characters, and it
// stood at 4488 before this change, so these two rules had to be PAID FOR. The
// payment is itemised in tasks/01a0f65c17767624b997662f9af1bbd0-report.md: the
// memory if-then list went from four letters to three clauses (c absorbed d),
// and the compressed clauses were chosen because NO test corpus pins them, while
// every pinned phrase ("mid-task notable find/pitfall", "rating=useful/useless/
// unused", "rating=useless", "memory(action=update)", "under ~250 characters",
// …) was left byte-identical. That is checked, not asserted: the report records
// the measurement, and the suite of prompt assertions is the guard.

// storeOnTheSpotClause is discipline 1's sentence — the authority half of the
// store rule. It is composed into memory line b (see toolRoutingRules): the
// forbidden shape is named explicitly, because a prohibition the model can
// RECOGNIZE in its own draft is stronger than a positive instruction it can
// satisfy in spirit, and the reason given is the compaction boundary, which is
// the mechanical fact that makes the question a loss rather than a courtesy.
const storeOnTheSpotClause = `NEVER ask 要不要记下来: 教训当场落库, 上下文随时可能被 compact, 未记录=丢失.`

// recordDisciplineRule9Number is engineering rule 9's numbered prefix. Kept
// separate from the clause (the shape industryPracticeRule8Number follows) so a
// future renumbering moves every renderer at once rather than re-typing a copy.
const recordDisciplineRule9Number = "9. "

// recordDisciplineRule9Clause is discipline 2's sentence, unnumbered — the form
// a renderer WITHOUT a numbered list can carry.
const recordDisciplineRule9Clause = `命中≠实现: symbol/keyword 命中 is NOT evidence a feature 已实现 — ` +
	`真跑取证再下结论; 假阴性 (grep 0命中≠没实现)/假阳性 都实测过.`

// recordDisciplineRule9 is engineering rule 9 exactly as it renders inside
// [ENGINEERING RULES]: the shared clause with its list number in front.
const recordDisciplineRule9 = recordDisciplineRule9Number + recordDisciplineRule9Clause
