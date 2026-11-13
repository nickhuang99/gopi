// Package thinkmode is the single source of truth for the thinking.type wire
// values of the Anthropic-compatible Messages API ("enabled" / "disabled").
//
// Why a leaf package (GoPi Rule 14): the value is shared across package
// boundaries — the agent loop writes it on every request, the deepseek client
// builder compares it, organ clients (memory brew, hippocampus) and compaction
// pass it, and the OpenAI-path proxy adapter maps it. Go constants do not
// cross packages unless exported, so the alternatives were a mirror copy in
// each package (drift: one copy renamed, the others keep the old spelling and
// the wire value mismatches) or a bare literal at every site (Rule 1b/13).
// A leaf package has neither problem: it imports nothing, so no consumer can
// create a cycle.
//
// The literal may live ONLY here — TestThinkingTypeLiteralGate fails any
// production file outside this package that re-inlines the value at a
// thinking-type site (Rule 13's ratchet).
package thinkmode

const (
	// Enabled asks the endpoint for thinking (reasoning) blocks. The request
	// also carries budget_tokens; note the API ACCEPTS but does not enforce
	// that budget, so the agent harness enforces it (see internal/agent
	// chatOnce).
	Enabled = "enabled"
	// Disabled turns thinking off. Honored by DeepSeek models; GLM models
	// always think and reject the field (the client omits it there).
	Disabled = "disabled"
)
