package agent

// envctx.go — the ONE call site that puts the host's environment description on
// the agent's context. It rides the EXISTING system message rather than adding
// one: `withEnvironmentBlock` returns the string the agent's Context already
// carries (Context.ProjectContext), so the task path, the chat path and the
// chat-ops path all get it WITHOUT a new field, a new message, a new prompt
// assembly step or a per-path edit — the three wire sites that pass
// a.Ctx.ProjectContext (loop.go, chat_ops.go, and the agent's own request
// builder) are already the shared ones.
//
// WHY THE ENVIRONMENT BLOCK IS NOT IN THE KERNEL PROMPT. The kernel prompt is a
// CONSTANT measured against a density budget; a machine's node list is neither
// constant nor universal, and putting it in the kernel would spend every
// operator's budget on one host's inventory. The block is bounded by
// construction (a summary, a pointer, one caveat) and is EMPTY — absent from
// the wire entirely — on a host that never generated a description.
//
// WHY THIS WRAPPER EXISTS AT ALL, rather than two calls at the loop. The
// project-context call site sits deep in loop.go, so a comment added above it
// shifts every line reference below — a diff-cost reason that holds whether or
// not anything reads those lines. Folding the second half into one expression
// keeps that file byte-stable, and the explanation lives here where it costs
// nothing (Rule 14 — one definition, at the place that owns the behaviour).
//
// HISTORY, so nobody satisfies a check that does not exist. This used to be
// stated as a gate: the devdoc line-anchor gate asserted that the corpus's
// `loop.go:NNN` references still resolved, and was deleted whole with
// internal/docclass in 5a35f61. NOTHING resolves a devdoc line anchor now (the
// normative docs still spell `loop.go:NNN`; no check reads it). The census that
// replaced it over CONFIG.md's rows — `go run internal/standards/testdata/
// config-anchor-census.go . <table> --check`, driven by its TestMergeConfigAnchorGate*
// arms — was deleted with the documentation-citation family in 78bdf05a
// (2026-11-05), so no executable anchor check runs at all. Keep the file
// byte-stable because a large diff is a real cost, NOT because a gate reddens.
//
// ABSENCE IS FINE: envctx.Load reports it on stderr once, injects nothing, and
// this function returns the project context unchanged. Nothing here can fail a
// run.

import "github.com/nick/gopi/internal/envctx"

// withEnvironmentBlock appends this host's environment description (summary +
// pointer to the full file) to the agent's context string.
func withEnvironmentBlock(projectContext string) string {
	block, ok := envctx.Load("")
	return envctx.Combine(projectContext, block, ok)
}

// projectContextBlock is the whole assembled second system message: the
// project's own context (PROJECT.md, or the client's GOPI.md when only that
// exists) followed by this host's environment description (optional, measured).
// It is what the assembly tests drive; the run path reaches the same string
// through withEnvironmentBlock.
func projectContextBlock() string {
	return withEnvironmentBlock(LoadProjectContext(""))
}
