package agent

import (
	"encoding/json"
	"hash/fnv"
	"strconv"
	"unicode/utf8"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/tokenizer"
)

// CalibratedTokens is the token total the STATUS ROW reports: the best
// available number for the prompt this context would send right now.
//
// WHY IT IS A SECOND FUNCTION AND NOT EstimatedTokens ITSELF. EstimatedTokens
// is the COMPACTION/TRIM trigger's input, and that trigger is calibrated against
// a chars/4 heuristic on purpose (every trim threshold in the suite is pinned in
// those units, and a trigger that changed its unit would re-tune itself
// silently). What the row needs is different: the number a reader compares
// against the API's prompt_tokens — which is BPE-exact and includes the tool
// definitions. The two questions are genuinely different, and the defect this
// file closes is that ONE function answered both, from a basis that under-counts
// CJK text ~2.4x and does not count the tool definitions at all.
//
// MEASURED, on a real chat request body taken from this host's session raw log
// (18 tool definitions, 534,636 B): the server charged input 1,479 + cache-read
// 143,488 = 144,967 prompt tokens. This function's basis gives 141,548 (97.6% of
// it). The shipped chars/4 basis gave 100,254 for the same body — and did not
// count the 11,070 tokens of tool definitions at all, which is the 3.6x gap the
// status row showed on a resumed session (36k before the first round, 131k
// once a measured count existed).
//
// THE MEASURED BRANCH IS UNCHANGED: when a real prompt count covers the current
// message range, that count IS the answer, plus the tail added since — and the
// tool definitions are NOT added on top, because the server's count already
// includes them (adding them would double-count).
// PredictionAnchored reports WHICH KIND of prediction CalibratedTokens() is
// currently answering with — and therefore what a reconciliation against the
// server's own count actually measures.
//
//	true  — the context holds an API measurement for a prefix (PromptTokens), so
//	        the prediction is that MEASURED base plus a BPE estimate of the tail
//	        added since. A reconciliation then measures THE TAIL ESTIMATE ONLY,
//	        which is tiny (measured: 1–9 tokens on a 4-turn chat) and is NOT the
//	        figure the 2.4% question is about.
//	false — no measurement covers this context yet (a fresh session, or one
//	        resumed from a log), so the whole prompt is this engine's own BPE
//	        guess. A reconciliation then measures THE WHOLE-PROMPT ESTIMATE, and
//	        that is the comparison a reader means by "how far off is the row".
//
// THE TWO POPULATIONS MUST NOT BE AVERAGED TOGETHER (the defect this predicate
// exists to prevent, measured on its first real run): a 4-turn chat's rows came
// back as 0, 1, 0 and 94 bp, and the mean of 24 bp would have read as "the
// estimate is accurate to a quarter of a percent" — when in fact ONE of those
// rows measured the whole-prompt error (0.94%) and the other three measured the
// tail alone. Same field, two quantities: the reconciliation row records which
// one it was (reconFieldPredictionAnchored), and a reader that folds them groups
// by that flag rather than averaging across it.
func (c *Context) PredictionAnchored() bool {
	if c == nil {
		return false
	}
	return c.PromptTokens > 0 && c.PromptTokensIndex <= len(c.Messages)
}

func (c *Context) CalibratedTokens() int {
	if c == nil {
		return 0
	}
	if c.PromptTokens > 0 && c.PromptTokensIndex <= len(c.Messages) {
		total := c.PromptTokens
		for _, m := range c.Messages[c.PromptTokensIndex:] {
			total += c.messageTokens(m)
		}
		return total
	}
	total := c.textTokens(c.systemPromptForEstimate()) + c.textTokens(c.ProjectContext)
	for _, m := range c.Messages {
		total += c.messageTokens(m)
	}
	return total + c.ToolsTokens()
}

// systemPromptForEstimate is the system prompt this reading prices: the composed
// one when a caller recorded it (the chat wire builds its prompt per request),
// otherwise the plain base the wire renders from.
func (c *Context) systemPromptForEstimate() string {
	if c.estimateSystemPrompt != "" {
		return c.estimateSystemPrompt
	}
	return c.SystemPrompt
}

// messageTokens is the BPE cost of one message as it appears on the wire. It is
// cached by CONTENT FINGERPRINT, not by index: trimming drops from the head and
// compaction rewrites the head, so an index-keyed cache would be invalidated
// wholesale by the very operations that call this function in a loop.
//
// THE COST THAT MATTERS: the trim loops call the estimate once per removed
// message, so a per-call full BPE pass over the history would be O(n²) — at the
// measured rate (313,101 chars in 456 ms) a 200-message trim would take minutes.
// With the fingerprint cache each message is tokenized exactly once per distinct
// content, and every later call is a hash plus a map hit.
func (c *Context) messageTokens(m deepseek.Message) int {
	key := messageFingerprint(m)
	if n, ok := c.tokenCosts[key]; ok {
		return n
	}
	n := countTokens(messageWireText(m))
	if c.tokenCosts == nil {
		c.tokenCosts = make(map[uint64]int)
	}
	c.tokenCosts[key] = n
	return n
}

// textTokens is messageTokens for a bare string (the system prompt, the project
// context). Those two are large, stable blocks — the system prompt is often the
// single biggest item in a chat request (measured: 51,547 of 144,967 tokens) —
// so caching them by content keeps every repaint cheap.
func (c *Context) textTokens(s string) int {
	if s == "" {
		return 0
	}
	key := stringFingerprint(s)
	if n, ok := c.tokenCosts[key]; ok {
		return n
	}
	n := countTokens(s)
	if c.tokenCosts == nil {
		c.tokenCosts = make(map[uint64]int)
	}
	c.tokenCosts[key] = n
	return n
}

// countTokens is the ONE place this package turns text into a token count: the
// embedded BPE tokenizer when it is available (a true count, no round trip) and
// the chars/4 heuristic when it is not — the tokenizer parses a 7.8 MB file on
// its first call and reports an error if that parse fails, and a status row that
// panicked or printed nothing because a data file was unreadable would be a
// worse read-out than an estimate.
func countTokens(text string) int {
	if text == "" {
		return 0
	}
	if n, err := countTokensFn(text); err == nil && n >= 0 {
		return n
	}
	return heuristicTextTokens(text)
}

// countTokensFn is the tokenizer seam. It is a variable so a test can drive the
// UNAVAILABLE branch (parse failure) without corrupting the embedded data — the
// fallback is a real code path a reader must be able to see fail over, not an
// untestable claim.
var countTokensFn = tokenizer.CountDefault

// heuristicTextTokens is the chars/4 rule the whole package used before the
// tokenizer existed. It stays as the fallback; EstimateTokens' own arithmetic is
// unchanged (the trigger's units are pinned by the suite).
func heuristicTextTokens(text string) int {
	return utf8.RuneCountInString(text)/charsPerToken + 1
}

// messageWireText renders the parts of a message the server counts, in a stable
// order: role, content, tool-call names/ids/arguments, the tool-result id and
// the image envelope. Tool arguments are the payload of a `tool_use` block and
// are counted as the JSON the wire carries, which is what the server sees.
func messageWireText(m deepseek.Message) string {
	var b []byte
	b = append(b, m.Role...)
	b = append(b, m.Content...)
	b = append(b, m.ToolCallID...)
	for _, tc := range m.ToolCalls {
		b = append(b, tc.Name...)
		b = append(b, tc.ID...)
		if args, err := json.Marshal(tc.Arguments); err == nil {
			b = append(b, args...)
		} else {
			b = append(b, tc.RawArgs...)
		}
	}
	for i := range m.Images {
		b = append(b, m.Images[i].MediaType...)
		b = append(b, strconv.Itoa(len(m.Images[i].Data))...)
	}
	return string(b)
}

// messageFingerprint is the cache key for one message: a 64-bit hash over the
// same material messageWireText counts, so a message whose CONTENT changed in
// place (context_trim rewrites tool-result blocks) misses the cache instead of
// returning the stale cost.
func messageFingerprint(m deepseek.Message) uint64 {
	h := fnv.New64a()
	h.Write([]byte(messageWireText(m)))
	return h.Sum64()
}

// stringFingerprint is messageFingerprint for a bare string.
func stringFingerprint(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
