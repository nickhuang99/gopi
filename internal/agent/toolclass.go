package agent

import "github.com/nick/gopi/internal/toolnames"

// This file owns the AGENT-LOOP's tool classes — the predicates the loop
// detectors need to say "this round only looked" or "this round changed
// something". It is the single definition site for both classes (Rules
// 1b/13/14): a class spelled at a call site would drift from the detectors
// that read it, and the drift is silent.
//
// Why a class and not a hand-picked name list: Rule 3 fixes the tool surface
// at 17 names, and the two classes below partition exactly the tools that can
// appear in a round — every name is either an OBSERVER, a MUTATOR, or the
// polling tool (`job`), which is neither and owns its own semantics in
// toolStreakState. A new tool added to internal/toolnames without a home here
// is caught by TestToolClassesCoverTheToolSurface (no silent third class).

// observerTools are the tools whose effect is to LOOK: they return a view of
// live state (a file, a command's output, a search result, a screen) and
// change nothing outside their own read. `bash` is deliberately in this class
// even though it CAN mutate: the loop cannot see argv-level intent, and the
// look-without-fixing loop being bounded here is dominated by `bash
// <observe>` (tail/grep/ps/ssh …) calls — a `bash` that mutates is followed by
// a verified state change, which is an ACT on the next round, so the bound has
// rounds to spare (threshold ≈ 3× the deepest productive observation run in
// the real corpus; see devdoc/look-without-fixing-bound.md).
var observerTools = map[string]bool{
	toolnames.ToolRead:          true,
	toolnames.ToolBash:          true,
	toolnames.ToolCodeSearch:    true,
	toolnames.ToolKeywordSearch: true,
	toolnames.ToolWebSearch:     true,
	toolnames.ToolWebFetch:      true,
	toolnames.ToolGitBlame:      true,
	toolnames.ToolVisionAnalyze: true,
	toolnames.ToolScreenCapture: true,
}

// mutatorTools are the tools that CHANGE something the loop must then verify:
// the artefact store, the task ledger/working area, the issue queue, the
// knowledge base, a rendered image, or a detached background job. A round
// carrying any of them is an ACT, so it resets the observation streak — the
// bound must never bill real work as observation.
var mutatorTools = map[string]bool{
	toolnames.ToolWrite:      true,
	toolnames.ToolEdit:       true,
	toolnames.ToolTask:       true,
	toolnames.ToolTaskSubmit: true,
	toolnames.ToolMemory:     true,
	toolnames.ToolText2img:   true,
}

// observationToolClass decides a round's class from the tool names it carries.
//
//   - act:   >=1 MUTATOR, or a polling tool (`job`, whose semantics belong to
//     toolStreakState — detector 2 — and must not be double-billed).
//   - observe: >=1 tool_use and EVERY name is an OBSERVER — the round is a
//     CANDIDATE for the repeat judgment.
//   - none:  no tool_use (the round is detector 1's signal, not this one's).
//
// IMPORTANT — this class is NOT the judged quantity any more (fix for task
// 01a0faac): a round being all-observer only makes it a candidate, and
// observationState.observeResults decides whether it counts by asking
// whether the round REPEATED (same fingerprint AND same result). Billing the
// class itself is the defect that truncated legitimate evidence-gathering
// runs at 40 rounds (field task 01a0f8c9): reading a different file each
// round is investigation, not a loop.
//
// An UNKNOWN name is not observation: a tool the class table does not know is
// a class this bound has not reasoned about, and treating "unknown" as "look"
// would silently widen the net over the next tool the tree grows.
func observationToolClass(names []string) (observe bool, act bool) {
	if len(names) == 0 {
		return false, false
	}
	for _, n := range names {
		if mutatorTools[n] {
			return false, true
		}
		if n == toolnames.ToolJob {
			return false, true // polling: detector 2's class
		}
		if !observerTools[n] {
			return false, true // unknown name: never billed as observation
		}
	}
	return true, false
}
