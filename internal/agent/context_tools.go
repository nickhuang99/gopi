package agent

import (
	"encoding/json"
	"hash/fnv"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// Tool definitions are part of the request body and part of the prompt the
// server bills for: `ClaudeRequest.Tools` is a TOP-LEVEL field of the wire
// (internal/deepseek/claude_client.go), and the API's prompt_tokens covers it
// exactly as it covers the system blocks and the messages.
//
// The context estimate did NOT know that until this file existed: the pure
// chars/4 branch of EstimatedTokens counted the system prompt, the project
// context and the messages, and nothing else. Measured on a real chat request
// body (18 defs, 534,636 B, extracted from the session raw log): the tools
// alone are 11,070 BPE tokens out of 144,967 the server reported, i.e. 7.6% of
// the prompt — and on a SHORT history (a resumed session before its first
// round) that is a far larger share, which is the reported defect: the status
// row read 36k where the server then charged 131k.
//
// So the definitions are recorded on the Context and counted. SetTools is the
// ONE writer; it is called where the request is BUILT (the only place that
// knows which list this wire carries) and, for the chat REPL, once at start-up
// so a resumed session's FIRST reading already includes them. The count is
// cached: the definitions do not change between rounds unless the surface
// changes, so the (cheap) marshal + (expensive) BPE count happen once per
// distinct list rather than once per repaint.

// toolsTokensCache invalidates on a CHANGE of the recorded list. Compaction
// builds its own request with nil tools, and it must NOT clear what the main
// wire carries, so a nil SetTools is a no-op rather than a reset.
type toolsEstimate struct {
	defs    []deepseek.ClaudeToolDef
	fp      uint64 // fingerprint of defs, so an unchanged list is not re-counted
	tokens  int    // 0 with counted=false means "not computed yet"
	counted bool
}

// SetTools records the tool definitions this context's wire carries. Passing an
// empty list is a no-op: a caller that builds a SEPARATE tool-less request
// (compaction, the plain chat path) must not zero the estimate for the main
// wire. A nil Context is a no-op so call sites stay nil-safe.
//
// AN UNCHANGED LIST IS NOT RE-COUNTED. The task loop records the same
// claudeToolDefs every round, and re-tokenizing 18 schemas per round would put
// the tokenizer on the round path for no new information — the fingerprint is
// what makes "same list" cheap to decide (context_tokens_test measures the
// second reading at zero new tokenizations).
func (c *Context) SetTools(defs []deepseek.ClaudeToolDef) {
	if c == nil || len(defs) == 0 {
		return
	}
	fp := toolsFingerprint(defs)
	if c.tools.counted && c.tools.fp == fp {
		return
	}
	c.tools.defs = defs
	c.tools.fp = fp
	c.tools.counted = false
}

// toolsFingerprint hashes the material a definition contributes to the wire —
// name, description, marshalable schema — so a list that changed anywhere is
// re-counted and one that did not is not.
func toolsFingerprint(defs []deepseek.ClaudeToolDef) uint64 {
	h := fnv.New64a()
	for _, def := range defs {
		h.Write([]byte(def.Name))
		h.Write([]byte{0})
		h.Write([]byte(def.Description))
		h.Write([]byte{0})
		if payload, err := json.Marshal(def.InputSchema); err == nil {
			h.Write(payload)
		}
		h.Write([]byte{0xff})
	}
	return h.Sum64()
}

// ToolsTokens is the token count of the recorded tool definitions — what the
// server pays for the `tools` array. 0 when nothing was recorded. It is the
// single place the array's size is measured, so the cheap estimate and the
// exact one cannot disagree about how much the tools cost (Rule 14).
func (c *Context) ToolsTokens() int {
	if c == nil || len(c.tools.defs) == 0 {
		return 0
	}
	if c.tools.counted {
		return c.tools.tokens
	}
	// The wire carries the definitions as JSON (name, description, schema,
	// cache marker); marshalling is what makes the count independent of how
	// the defs were constructed. A marshal failure is not silently ignored:
	// the caller gets the cheap character estimate for the same bytes, which
	// is the same fallback every other site uses.
	total := 0
	for _, def := range c.tools.defs {
		payload, err := json.Marshal(def)
		if err != nil {
			// The marshal failed, so the exact bytes are unavailable; fall back to
			// the chars/4 rule for the two fields we do have rather than dropping
			// the definition from the total.
			total += heuristicTextTokens(def.Name + def.Description)
			continue
		}
		total += countTokens(string(payload))
	}
	c.tools.tokens = total
	c.tools.counted = true
	return total
}
