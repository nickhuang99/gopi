// Package taskverdict is the single source of truth for the reframe
// verdict vocabulary (Rule 14: a value shared across packages lives in ONE
// small exported-constant leaf package).
//
// A reframe (task action=reframe) ends with a verdict — the LLM's structured
// judgment made AFTER investigation, at the only trustworthy moment. The
// harness dispatches on it:
//
//	DOABLE    — do the work here (normal path, no extra payload)
//	TOO_BIG   — roadmap[{goal,acceptance}] REQUIRED; the harness auto-enqueues
//	            the roadmap as child tasks and this run exits honorably
//	            (its value already delivered = due diligence + decomposition)
//	IMPOSSIBLE— reason REQUIRED; the task row is marked failed with it
//
// THREE verdicts, no fourth (the operator 2026-09-10, SPEC §3 — the zero-loophole
// design): NEEDS_RESEARCH was a POC artifact of a filesystem-less prompt.
// Research IS part of reframing (the model has read/code_search/memory_search
// — investigating is its job), so "I need more research" is a failure to
// reframe, not an exit. Whatever genuinely cannot be decided yet becomes a
// roadmap ITEM ("research X: read A/B/C, compare, write the conclusion doc")
// inside a TOO_BIG verdict, so the task still moves forward with new work.
//
// IMPOSSIBLE is deliberately narrow: provably unattainable in principle —
// not merely hard, risky or unknown. If a concrete plan or investigation
// could eventually get there, it is TOO_BIG with the investigation as a
// roadmap item. That strictness is what keeps the honest exit honest.
//
// The values are the WIRE protocol with the LLM (the tool schema's enum
// carries these exact strings) — never type them inline where a verdict is
// validated, dispatched on, or reported (Rule 1/13).
package taskverdict

// The three verdicts, in the order the schema's enum lists them.
const (
	// Doable means the epic is executable in this session — no extra payload.
	Doable = "DOABLE"
	// TooBig means the epic is too big to finish here, but the model has
	// investigated enough to decompose it — roadmap REQUIRED. "Too big" must
	// cost a concrete plan; that price is the anti-bureaucracy mechanism.
	TooBig = "TOO_BIG"
	// Impossible means the epic is provably unattainable in principle —
	// reason REQUIRED (the honest exit).
	Impossible = "IMPOSSIBLE"
)

// All returns the verdict vocabulary in schema/enum order. Callers that build
// an enum (the tool schema) or a distribution report iterate this — never a
// hand-written list that can drift from the constants above.
func All() []string {
	return []string{Doable, TooBig, Impossible}
}

// Valid reports whether s is one of the verdicts. The harness validation
// gate: an empty or unknown verdict is rejected (never silently defaulted —
// Rule 15: a dropped decision is a silent failure).
func Valid(s string) bool {
	for _, v := range All() {
		if s == v {
			return true
		}
	}
	return false
}
