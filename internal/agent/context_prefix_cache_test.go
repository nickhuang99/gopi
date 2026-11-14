package agent

import (
	"io"
	"log"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// headID names the current head of the history — the message a trim advances
// past, i.e. the first byte a prompt cache can no longer serve.
func headID(c *Context) string {
	if len(c.Messages) == 0 {
		return "<empty>"
	}
	s := c.Messages[0].Content
	if len(s) > 24 {
		s = s[:24]
	}
	return c.Messages[0].Role + ":" + s
}

// newPrefixTestAgent builds the minimum agent trimContext needs, with
// compaction DISABLED so the tests exercise the zero-LLM-cost mutators that the
// measured field session hit on every request (devdoc/
// chat-cache-hit-attribution.md).
func newPrefixTestAgent(window int) *Agent {
	return &Agent{
		Logger:            log.New(io.Discard, "", 0),
		ContextWindow:     window,
		CompactionEnabled: false,
		// AutoReduceEnabled: the zero-LLM-cost mutators are part of the OPT-IN
		// reduction path (GOPI_CONTEXT_AUTOREDUCE=1 — context_pressure.go), and
		// that is what these tests exercise: compaction disabled, folding and the
		// deterministic trim still reachable.
		AutoReduceEnabled:     true,
		CompactTriggerRatio:   DefaultCompactTriggerRatio,
		CompactTargetRatio:    DefaultCompactTargetRatio,
		CompactKeepRecent:     DefaultCompactKeepRecent,
		lastCompactSavingsPct: 100.0,
		Ctx:                   &Context{},
	}
}

// fillPairs builds a history of (user, assistant) pairs of `chars` characters
// each. SMALL messages are the discriminating shape for the amortization test:
// the overshoot is then worth one message per round, which is exactly the
// nibbling the old sliding-window trim did on the field session (tool results
// of a few thousand tokens against a 31K fixed overhead).
func fillPairs(c *Context, pairs, chars int) {
	big := strings.Repeat("x", chars)
	for i := 0; i < pairs; i++ {
		c.AddUserMessage("u" + strconv.Itoa(i) + big)
		c.AddAssistantMessage("a"+strconv.Itoa(i)+big, nil)
	}
}

// TestCacheEvictionIsAmortizedNotNibbled is the regression guard for the
// measured cache defect (session a09b91ae, 2026-09-30: 91.5% hit, 7.9M miss
// tokens, 96.4% of them on 65 requests).
//
// THE MECHANISM. That chat's context sat just above the trim limit from a fixed
// per-turn overhead (system prompt + tool schema ≈ 31K tokens), so trimContext
// fired on EVERY request. The old `TrimToFit(limit)` deleted only the
// overshoot's worth; the round's own append pushed the context straight back
// over the limit and the next trim deleted from the NEW head. The head crept
// forward a few messages per round, so every request re-prefilled the whole
// conversation. Wire truth, joined 684/685 requests to their payload: the 41
// requests whose message list SHRANK at the head versus the previous request
// carried 60.9% of all miss tokens.
//
// THE CONTRACT, asserted as a direct A/B against the strategy it replaces. The
// scenario is the discriminating one: a full context whose per-round growth is
// worth about one message, so the OLD strategy must evict on every round (it
// removes exactly the overshoot, which the next append re-creates) while the
// amortized one evicts once and then sits below the limit for many rounds.
// Each round whose head changed re-prefills the whole conversation on the wire,
// so that count is the defect, measured.
func TestCacheEvictionIsAmortizedNotNibbled(t *testing.T) {
	const rounds = 8
	window := 4000
	limit := int(float64(window) * DefaultCompactTriggerRatio)
	target := int(float64(window) * DefaultCompactTargetRatio)

	// headChanges drives `rounds` real rounds — append, then trim THROUGH THE
	// SAME GATE the loop uses (only when over the limit) — and counts how many
	// rounds advanced the head. trim is the strategy under test.
	headChanges := func(trim func(*Agent)) int {
		ag := newPrefixTestAgent(window)
		fillPairs(ag.Ctx, 60, 300) // far over the limit; overshoot ≈ one message
		changes := 0
		prev := headID(ag.Ctx)
		for round := 0; round < rounds; round++ {
			ag.Ctx.AddUserMessage("round" + strconv.Itoa(round) + strings.Repeat("n", 200))
			ag.Ctx.AddAssistantMessage("ok"+strconv.Itoa(round), nil)
			if ag.Ctx.EstimatedTokens() > limit { // the gate trimContext applies
				trim(ag)
			}
			if ag.Ctx.EstimatedTokens() > limit {
				t.Fatalf("the trim left the context over the limit: %d > %d", ag.Ctx.EstimatedTokens(), limit)
			}
			head := headID(ag.Ctx)
			if head != prev {
				changes++
			}
			prev = head
		}
		return changes
	}

	old := headChanges(func(ag *Agent) { ag.Ctx.TrimToFit(limit) })
	amortized := headChanges(func(ag *Agent) { ag.Ctx.trimToFitStickyHead(limit, target) })

	if amortized > 1 {
		t.Fatalf("the eviction is not amortized: the head changed on %d of %d rounds — a per-round eviction re-prefills the whole conversation on every request, which is the measured defect", amortized, rounds)
	}
	t.Logf("head-changing rounds over %d rounds of the same scenario: sliding-window TrimToFit=%d, cache-aware trimToFitStickyHead=%d", rounds, old, amortized)
	if amortized >= old {
		t.Fatalf("the cache-aware trim must evict strictly less often than the sliding window: amortized=%d sliding=%d (the sliding window's per-round eviction IS the measured defect)", amortized, old)
	}
}

// TestCacheEvictionReachesTheTargetWaterline pins the other half of
// amortization: one eviction must go down to the target, not merely under the
// trigger line. Stopping at the trigger line is what guarantees a re-eviction
// on the very next round.
func TestCacheEvictionReachesTheTargetWaterline(t *testing.T) {
	ag := newPrefixTestAgent(4000)
	limit := int(float64(ag.ContextWindow) * ag.CompactTriggerRatio)     // 3000
	target := int(float64(ag.ContextWindow) * DefaultCompactTargetRatio) // 1400
	if target >= limit {
		t.Fatalf("test assumes target < limit, got %d >= %d", target, limit)
	}
	fillPairs(ag.Ctx, 8, 4000)
	removed := ag.Ctx.trimToFitStickyHead(limit, target)
	if removed == 0 {
		t.Fatalf("expected an eviction")
	}
	if after := ag.Ctx.EstimatedTokens(); after > target {
		t.Fatalf("the eviction must amortize to the target %d in ONE call, got %d (removed %d)", target, after, removed)
	}
}

// TestTrimKeepsTheContextBounded is the other half of the contract: the fix may
// not buy cache stability with unbounded growth. After every trim the context
// must be under the limit, over a long run.
func TestTrimKeepsTheContextBounded(t *testing.T) {
	ag := newPrefixTestAgent(4000)
	limit := int(float64(ag.ContextWindow) * ag.CompactTriggerRatio)
	fillPairs(ag.Ctx, 6, 3000)
	for round := 0; round < 25; round++ {
		ag.Ctx.AddUserMessage("more" + strconv.Itoa(round) + strings.Repeat("m", 400))
		ag.Ctx.AddAssistantMessage("ok"+strconv.Itoa(round), nil)
		ag.trimContext(nil)
		if got := ag.Ctx.EstimatedTokens(); got > limit {
			t.Fatalf("round %d: trim left the context over the limit: %d > %d", round, got, limit)
		}
		if len(ag.Ctx.Messages) == 0 {
			t.Fatalf("round %d: trim emptied the history", round)
		}
	}
}

// TestTrimKeepsRecencyAndToolPairs pins the two structural contracts the API
// enforces and the round depends on: the newest message is never the victim
// (the model is mid-turn on it), and a tool result never survives without its
// owning assistant tool_use.
func TestTrimKeepsRecencyAndToolPairs(t *testing.T) {
	ag := newPrefixTestAgent(2000)
	limit := int(float64(ag.ContextWindow) * ag.CompactTriggerRatio)
	target := int(float64(ag.ContextWindow) * DefaultCompactTargetRatio)
	for i := 0; i < 20; i++ {
		ag.Ctx.AddUserMessage("u" + strconv.Itoa(i))
		ag.Ctx.AddAssistantMessage("", toolCallLocal("read", "call_"+strconv.Itoa(i)))
		ag.Ctx.AddToolResult("call_"+strconv.Itoa(i), "exit=0\n"+strings.Repeat("y", 300))
	}
	newest := ag.Ctx.Messages[len(ag.Ctx.Messages)-1].Content
	ag.Ctx.trimToFitStickyHead(limit, target)
	if len(ag.Ctx.Messages) == 0 {
		t.Fatalf("trim emptied the history")
	}
	if got := ag.Ctx.Messages[len(ag.Ctx.Messages)-1].Content; got != newest {
		t.Fatalf("the newest message must never be the victim: dropped %q", got)
	}
	if ag.Ctx.Messages[0].Role == "tool" {
		t.Fatalf("trim left an orphaned tool result as the head")
	}
	for i, m := range ag.Ctx.Messages {
		if m.Role != "tool" {
			continue
		}
		owned := false
		for j := i - 1; j >= 0; j-- {
			if ag.Ctx.Messages[j].Role == "assistant" {
				for _, tc := range ag.Ctx.Messages[j].ToolCalls {
					if tc.ID == m.ToolCallID {
						owned = true
					}
				}
				break
			}
		}
		if !owned {
			t.Fatalf("message %d is a tool result whose owning tool_use is gone (tool_call_id=%q)", i, m.ToolCallID)
		}
	}
}

// toolCallLocal builds a one-call assistant turn without spreading the deepseek
// package name across every call site.
func toolCallLocal(name, id string) []deepseek.ToolCall {
	return []deepseek.ToolCall{{Name: name, ID: id}}
}
