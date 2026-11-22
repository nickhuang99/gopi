package agent

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/tokenizer"
)

// CalibratedTokens is the token total the STATUS ROW reports: the best
// available number for the prompt this context would send right now.
//
// WHY IT IS STILL A SECOND FUNCTION, NOW THAT BOTH ARE ON THE TOKENIZER. The two
// functions are no longer two BASES — EstimatedTokens() was moved onto the BPE
// tokenizer in this same change, so neither is a chars/4 estimate any more — they
// are two QUESTIONS. EstimatedTokens prices the context the trim/compaction
// trigger judges (and honours a recorded API measurement as its prefix); this one
// prices the prompt the operator is about to SEND and always prices it from
// scratch, including the per-request system prompt the chat wire composes
// (systemPromptForEstimate) and the tool definitions.
//
// The historical note is kept because it is why the file exists: ONE function
// used to answer both, from a basis that under-counted CJK text ~2.4x and did not
// count the tool definitions at all.
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
	// tokenCost carries the cache and the per-message overhead, so this function
	// and EstimateTokens give the SAME answer for the same message by
	// construction. They used to be two arithmetics that had to agree; with the
	// BPE basis the overhead is added AFTER the count (folding it into the text
	// would change how the text tokenizes), and two implementations of that rule
	// drifted by one token the moment either changed.
	return tokenCost(c, messageWireText(m), messageTokenOverhead)
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

// tokenCost is the ONE cache-aware entry point the ESTIMATE side of this package
// uses (EstimateTokens, EstimatedTokens, heuristicTokens, fixedFloorTokens).
//
// It is countTokens plus two things the per-message calls need and the bare text
// calls do not:
//
//   - the CONTENT-keyed cache on c (nil c = no cache), so a trim loop that asks
//     for the same message repeatedly pays one BPE pass, not one per iteration;
//   - the caller's fixed per-unit overhead, added AFTER the count rather than
//     folded into the text. Rolling it into the text would change how the text
//     BPEs, and the overhead does not exist on the wire anyway: it stands in for
//     the template markers the server charges around a message and this package
//     does not render.
//
// WHY THE COUNT IS BPE AND NOT chars/4 (this is the fix; the old basis is kept
// only as countTokens' unavailable-branch fallback). MEASURED on this host, one
// Chinese sentence, `runes / charsPerToken + 1` beside the embedded tokenizer
// (`gopi tool tokens --prompt`, deepseek-v4.1-bpe):
//
//	"我们现在看一看还有多少失败的任务？今天有没有失败的task，你看一下。"
//	    35 runes → chars/4 says  9, BPE says 16   (under by 1.78x)
//	"那你看一看那两条取消的任务到底是什么原因取消的？为什么现在还有没有意义？"
//	    36 runes → chars/4 says 10, BPE says 18   (under by 1.80x)
//	"这是一段较长的中文文本用来测试估算偏差"
//	    19 runes → chars/4 says  5, BPE says 10   (under by 2.00x)
//
// A Chinese character is roughly one token, not a quarter of one, so the old
// basis under-counted every Chinese conversation — and this number is not a
// read-out: it is the INPUT to the compaction trigger, the fold trigger, the
// compaction target's fixed floor and the turn-stamp ceiling. An under-count
// there does not mean "the estimate is a bit low"; it means the brake fires late
// or never, and the request is refused at send time by request_tokens_cap.go's
// exact count instead.
//
// MEASURED PER CONTENT CLASS, against the same encoder (aggregate heur/real on
// 2-6 KB of each class; the pinned bands are in heuristic_ratio_test.go beside
// this file and in internal/tokenizer/heuristic_ratio_test.go):
//
//	Chinese   0.45  → chars/4 reads ~2.2x LOW. The WORST single Chinese sentence
//	                  measured 0.407 at 40 runes and 0.383 at 120 (2.45x and
//	                  2.61x) — the epic's 2.44x is one of those readings. The
//	                  figure this package used to state as "1.7-2x" understated it.
//	English   1.39  → chars/4 reads ~1.4x HIGH.
//	Go source 0.85  → chars/4 reads ~1.2x LOW.
//
// THE TWO DIRECTIONS ARE OPPOSITE, and that is the fact a reader must not lose:
// "the fallback is a conservative lower bound" is false. Chinese (and dense code)
// lets the trigger fire LATE, English lets it fire EARLY — a context that still
// fits gets trimmed. The single-sentence pairs above are the pure-Chinese worst
// case; they are kept because they are the shape of the reported defect, not
// because they are the aggregate.

func tokenCost(c *Context, text string, overhead int) int {
	if text == "" {
		return overhead
	}
	if c != nil {
		key := stringFingerprint(text)
		if n, ok := c.tokenCosts[key]; ok {
			return n + overhead
		}
		n := countTokens(text)
		if c.tokenCosts == nil {
			c.tokenCosts = make(map[uint64]int)
		}
		c.tokenCosts[key] = n
		return n + overhead
	}
	return countTokens(text) + overhead
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
	n, err := countTokensFn(text)
	if err == nil && n >= 0 {
		return n
	}
	warnTokenizerUnavailable(err)
	return heuristicTextTokens(text)
}

// tokenizerFallbackOnce makes the degradation notice ONE line per process. The
// fallback is per-call, but the STATE is per-run: repeating it every estimate
// would bury the one fact the reader needs.
var tokenizerFallbackOnce = new(sync.Once)

// tokenizerFallbackReport is the notice's sink. It is a variable so a test can
// observe the line without capturing the process's stderr (the code path is
// real and must be exercisable — the same seam rule as countTokensFn).
var tokenizerFallbackReport = func(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}

// warnTokenizerUnavailable reports the chars/4 degradation ONCE.
//
// WHY IT IS NOT SILENT (PROJECT.md Rule 15). The fallback keeps the process
// alive, but it silently reinstates the very defect this package just removed,
// and that defect does NOT lean one way. MEASURED against the embedded BPE on
// three content classes (aggregate heur/real per class; the pin is
// internal/tokenizer/heuristic_ratio_test.go plus this package's
// heuristic_ratio_test.go):
//
//	Chinese   0.45  → reads 2.24x LOW on a 2 KB aggregate; single Chinese
//	                  sentences read 2.4-2.6x low (the report's 2.44x is one of them)
//	English   1.39  → reads ~1.4x HIGH
//	Go source 0.85  → reads ~1.2x LOW
//
// So a reader comparing a run with a working tokenizer against a run without one
// would see two different estimates for the same bytes, differing in OPPOSITE
// directions by content, and nothing anywhere saying why: on Chinese the
// compaction trigger fires late (the send-time cap becomes the only brake), on
// English it fires early (a context that still fits gets trimmed).
func warnTokenizerUnavailable(err error) {
	tokenizerFallbackOnce.Do(func() {
		tokenizerFallbackReport(fmt.Sprintf(
			"⚠️ tokenizer unavailable (%v): token estimates fall back to chars/%d for the rest of this run. MEASURED against the BPE (heur/real): Chinese 0.45 (reads ~2.2x LOW, so compaction fires late), English 1.39 (reads ~1.4x HIGH, so compaction fires early), Go source 0.85 (reads ~1.2x LOW) — the fallback errs in BOTH directions, it is not a safe lower bound",
			err, charsPerToken))
	})
}

// countTokensFn is the tokenizer seam. It is a variable so a test can drive the
// UNAVAILABLE branch (parse failure) without corrupting the embedded data — the
// fallback is a real code path a reader must be able to see fail over, not an
// untestable claim.
var countTokensFn = tokenizer.CountDefault

// heuristicTextTokens is the chars/4 rule the whole package used before the
// tokenizer existed. It is now reached ONLY from countTokens' unavailable branch
// — every estimate in this package (EstimateTokens, EstimatedTokens,
// heuristicTokens, fixedFloorTokens) counts with the tokenizer, and the suite
// pins those to the tokenizer's own answer, not to a ratio.
//
// It is not silent when it fires: countTokens reports the degradation once
// (warnTokenizerUnavailable), because a Chinese conversation estimated here is
// low by ~2.2x again — and an English one is HIGH by ~1.4x, so the two content
// classes move the compaction trigger in opposite directions and a reader who
// compared two runs would never know why.
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

// stringFingerprint is the cache key for one message OR one text block: a 64-bit
// hash over exactly the material that is counted. A message whose CONTENT
// changed in place (context_trim rewrites tool-result blocks) therefore misses
// the cache instead of returning the stale cost.
func stringFingerprint(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
