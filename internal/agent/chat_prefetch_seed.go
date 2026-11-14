package agent

// chat_prefetch_seed.go — the SEEDING half of the chat path's memory prefetch:
// a retrieval the harness ran BEFORE the turn began is handed to the model in
// the shape the model's OWN memory(action=search) call produces, so the hit
// lines it reads carry the same record ids (#N) as a self-issued search and the
// EXISTING disciplines apply to them without a second mechanism.
//
// WHY A PAIR AND NOT A PREFIX (the operator's design, 2026-10-09): a prefetched
// block appended to the user's own message is unreachable by every mechanism the
// memory tool already has. It carries no tool_use, so `whitelistContextSlim`
// (loop.go) — which walks assistant tool_use blocks looking for memory searches
// — cannot see it, and an injected hit the model judged useless therefore stayed
// in the request for the rest of the session, paid for on every round. A pair
// shaped like the model's own search fixes that with NO new machinery:
//
//   - the hit lines are the SAME rendering (internal/memoryprefetch owns it;
//     this file never reformats a hit — Rule 13/14), so `#N` means what it means
//     everywhere else and the feedback discipline (toolRoutingRules c) covers it;
//   - votes have consequences: the pair is dropped by the whitelist slim unless
//     one of the records it returned was voted usable, exactly as a self-issued
//     search is.
//
// WHAT IT IS NOT: not a tool (Rule 3 — the memory tool already exists), not a
// second trimmer (Rule 35 — whitelistContextSlim is reused verbatim), and not a
// write to the ledger (the lookup is the executor's read-only one; this file
// only arranges messages).

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// SeededRetrieval is a retrieval the harness ran for the model before the turn
// began, carried so it can be seeded as the model's own memory(action=search).
type SeededRetrieval struct {
	// Query is the retrieval key the lookup used. The model reads it as the
	// tool call's own argument, so a prefetch that answered the wrong question
	// is visible as a wrong QUERY rather than as evidence about the ledger.
	Query string
	// Section is the rendered hit list, byte-identical to what an equivalent
	// memory(action=search) tool_result carries: internal/memoryprefetch owns
	// that rendering and this type never edits it.
	Section string
}

// empty reports whether there is nothing to seed. It is the OFF path's test:
// with the prefetch switch off the caller leaves Agent.PrefetchSeed at its zero
// value and the turn's message array is byte-identical to the pre-change tree.
func (s SeededRetrieval) empty() bool {
	return s.Section == ""
}

// prefetchSeedCallIDPrefix marks a seeded search pair's synthetic tool_use id.
// The wire requires an id (every tool_use needs one and its result must name
// it), and this spelling is deliberate: a reader of a request body can tell a
// seeded pair from a call the model really made, while the prefetch's own record
// file (memory-prefetch.jsonl) stays the authority on the injection.
const prefetchSeedCallIDPrefix = "call_prefetch_"

// takePrefetchSeed consumes the seed the caller published for THIS turn and
// clears it. Consume-once is the contract (mirroring publishChatTurn): a turn
// whose caller published nothing must not inherit the previous turn's retrieval
// and pay for it twice — the failure a stored field would make possible.
func (a *Agent) takePrefetchSeed() SeededRetrieval {
	if a == nil {
		return SeededRetrieval{}
	}
	seed := a.PrefetchSeed
	a.PrefetchSeed = SeededRetrieval{}
	return seed
}

// seedPrefetchedRetrieval appends the seeded pair to the message array:
//
//	assistant: tool_use   memory {"action":"search","query":<the lookup's key>}
//	tool:      tool_result <the rendered section, stamped with its tool_use id>
//
// The result rides the SAME `[tool_use id: …]` stamp the executor's own tool
// results carry (withTick + the id line, chat_ops.go), so the model can name the
// pair when it trims it and one stamp serves one reader.
//
// The arguments carry the action and the query and NOTHING ELSE — no invented
// `limit`: the executor's own bound is memoryprefetch's business, and a
// fabricated argument would be a claim about the retrieval the model could act
// on. A two-field call is exactly what a search with the default limit is.
//
// ORDER: the pair is appended AFTER the turn's user message, which is the order
// a real round produces (a call follows the thing it answers). Its result is
// appended immediately after its owner, so the wire invariant "every tool_use is
// answered before anything else" holds at every read of the array — the same
// invariant chat_ops.go states for the store checkpoint.
//
// It returns the id it minted, which the caller may ignore: the pair is found by
// the context itself (whitelistContextSlim scans messages, not a handle), so no
// bookkeeping of seeded ids exists to drift out of date.
func (a *Agent) seedPrefetchedRetrieval(seed SeededRetrieval) string {
	if a == nil || a.Ctx == nil || seed.empty() {
		return ""
	}
	id := prefetchSeedCallIDPrefix + newUUIDv4()
	a.Ctx.AddAssistantMessageWithThinking("", []deepseek.ToolCall{{
		ID:   id,
		Name: toolnames.ToolMemory,
		Arguments: map[string]interface{}{
			"action": toolnames.ToolMemoryActionSearch,
			"query":  seed.Query,
		},
	}}, "", "")
	a.Ctx.AddToolResult(id, a.withTick(fmt.Sprintf("[tool_use id: %s]\n%s", id, seed.Section)))
	return id
}
