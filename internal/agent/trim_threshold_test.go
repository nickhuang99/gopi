package agent

// trim_threshold_test.go — verifies that the context compression / trimming
// logic fires exactly when EstimatedTokens exceeds DefaultCompactTriggerRatio x
// ContextWindow. Re-uses the construction helpers from compact_test.go
// (newCompactTestAgent, fillHistory, compactTextResponse) — this file only
// ADDS tests and touches no existing source file.

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

// testThresholdWindow is the test window used by the shared helper; with the
// engine.s DefaultCompactTriggerRatio (0.75) the trim limit is 0.75 * window tokens.
const testThresholdWindow = 2000

// expectedLimit mirrors the trimContext boundary: DefaultCompactTriggerRatio of the
// ContextWindow. Kept as a helper so the tests assert against the same rule
// the engine uses, not a hand-computed magic number duplicated per test.
func expectedLimit(t *testing.T, window int) int {
	t.Helper()
	return int(float64(window) * DefaultCompactTriggerRatio)
}

// TestTrimThreshold_TriggersWhenOverLimit verifies that a conversation whose
// EstimatedTokens exceeds DefaultCompactTriggerRatio x ContextWindow is brought back
// under the limit by trimContext (here via LLM memory compaction), and that
// the newest message survives. Uses the pure-heuristic estimate path (no
// SetPromptTokens calibration) so compaction reliably drops the estimate.
func TestTrimThreshold_TriggersWhenOverLimit(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, testThresholdWindow)

	// est ≈ 20 * 2 * 129 ≈ 5160+, comfortably above the 0.75 * 2000 = 1500 limit.
	fillHistory(ag.Ctx, 20, 500)

	limit := expectedLimit(t, ag.ContextWindow)
	if got := ag.Ctx.EstimatedTokens(); got <= limit {
		t.Fatalf("precondition: expected est > limit (%d), got %d", limit, got)
	}
	lastContent := ag.Ctx.Messages[len(ag.Ctx.Messages)-1].Content

	ag.trimContext(context.Background())

	if *calls != 1 {
		t.Fatalf("expected exactly 1 summarizer call when over threshold, got %d", *calls)
	}
	if got := ag.Ctx.EstimatedTokens(); got > limit {
		t.Fatalf("expected context under limit (%d) after trim, got %d", limit, got)
	}
	if ag.Ctx.Messages[len(ag.Ctx.Messages)-1].Content != lastContent {
		t.Fatal("most recent message must be preserved after trim")
	}
	// LLM compaction heads the history with a memory block (not a plain drop).
	head := ag.Ctx.Messages[0]
	if head.Role != "user" || !strings.Contains(head.Content, memorySummaryMarker) {
		t.Fatalf("expected memory summary at head after compaction, got %q", head.Content)
	}
}

// TestTrimThreshold_NoTriggerWhenUnderLimit verifies trimContext is a no-op
// when the estimated size is already at or below DefaultCompactTriggerRatio x
// ContextWindow: no HTTP summarizer call, no message removed.
func TestTrimThreshold_NoTriggerWhenUnderLimit(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no summarizer call expected when under threshold")
	}, testThresholdWindow)

	fillHistory(ag.Ctx, 4, 100) // est well under the 1200-token limit
	before := len(ag.Ctx.Messages)

	limit := expectedLimit(t, ag.ContextWindow)
	if got := ag.Ctx.EstimatedTokens(); got > limit {
		t.Fatalf("precondition: expected est <= limit (%d), got %d", limit, got)
	}

	ag.trimContext(context.Background())

	if *calls != 0 {
		t.Fatalf("expected 0 HTTP calls, got %d", *calls)
	}
	if len(ag.Ctx.Messages) != before {
		t.Fatalf("expected no messages removed, want %d kept got %d", before, len(ag.Ctx.Messages))
	}
}

// TestTrimThreshold_BoundaryAtExactLimit verifies the exact boundary: when
// EstimatedTokens EQUALS DefaultCompactTriggerRatio x ContextWindow the trigger does
// NOT fire (the check is strictly `> limit`), so no compaction call and no
// message is removed. Pinning the measured count via SetPromptTokens to the
// exact limit makes the boundary deterministic.
func TestTrimThreshold_BoundaryAtExactLimit(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no summarizer call expected at exactly the limit")
	}, testThresholdWindow)

	fillHistory(ag.Ctx, 4, 100)
	limit := expectedLimit(t, ag.ContextWindow)

	// Pin the measured count EXACTLY to the limit: EstimatedTokens == limit.
	ag.Ctx.SetPromptTokens(limit)
	if got := ag.Ctx.EstimatedTokens(); got != limit {
		t.Fatalf("precondition: expected est == limit (%d), got %d", limit, got)
	}
	before := len(ag.Ctx.Messages)

	ag.trimContext(context.Background())

	if *calls != 0 {
		t.Fatalf("expected 0 HTTP calls when est == limit, got %d", *calls)
	}
	if len(ag.Ctx.Messages) != before {
		t.Fatalf("expected no messages removed when est == limit")
	}
}

// TestTrimThreshold_JustOverBoundaryTriggers verifies that one token over the
// measured limit flips the trigger: the summarizer is called and the oldest
// messages are removed (len shrinks). Uses a large enough history that
// compaction actually trims before the measured count goes stale.
func TestTrimThreshold_JustOverBoundaryTriggers(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, testThresholdWindow)

	fillHistory(ag.Ctx, 20, 500)
	limit := expectedLimit(t, ag.ContextWindow)

	// Just one token over the (measured) limit.
	ag.Ctx.SetPromptTokens(limit + 1)
	if got := ag.Ctx.EstimatedTokens(); got != limit+1 {
		t.Fatalf("precondition: expected est == limit+1 (%d), got %d", limit+1, got)
	}
	before := len(ag.Ctx.Messages)

	ag.trimContext(context.Background())

	if *calls != 1 {
		t.Fatalf("expected exactly 1 summarizer call when est == limit+1, got %d", *calls)
	}
	if len(ag.Ctx.Messages) >= before {
		t.Fatalf("expected oldest messages removed when over limit, want < %d kept got %d", before, len(ag.Ctx.Messages))
	}
}

// TestTrimThreshold_ToolResultFoldingOverLimit verifies that when the context
// is over budget solely because of long OLD tool results,
// summarizeOldToolResults folds them into one-line stubs and brings the
// estimate under the limit with NO HTTP call (the zero-LLM-cost folding path,
// R1). Only tool results older than the newest maxVerbatimToolResults with
// > maxToolResultLines are folded; the newest results stay verbatim.
func TestTrimThreshold_ToolResultFoldingOverLimit(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no summarizer call expected when folding alone is enough")
	}, testThresholdWindow)

	// Small base conversation, then a batch of OLD, long tool results (all
	// foldable — older than the newest 8 and > 20 lines each), then the
	// newest 8 SHORT results that stay verbatim. Folding the old long ones
	// must bring the estimate back under the limit at zero cost.
	fillHistory(ag.Ctx, 3, 40)                              // 6 small messages
	long := strings.Repeat("tool-long-content-line\n", 120) // 120 lines, folds
	for i := 0; i < 20; i++ {
		ag.Ctx.AddToolResult("old-call", long)
	}
	for i := 0; i < 8; i++ {
		ag.Ctx.AddToolResult("new-call", "short verbatim result")
	}

	limit := expectedLimit(t, ag.ContextWindow)
	if got := ag.Ctx.EstimatedTokens(); got <= limit {
		t.Fatalf("precondition: expected est > limit (%d) before folding, got %d", limit, got)
	}

	ag.trimContext(context.Background())

	if *calls != 0 {
		t.Fatalf("expected no HTTP calls (folding-only path), got %d", *calls)
	}
	if got := ag.Ctx.EstimatedTokens(); got > limit {
		t.Fatalf("expected context under limit (%d) after tool-result folding, got %d", limit, got)
	}
	// The most recent tool result must remain verbatim.
	last := ag.Ctx.Messages[len(ag.Ctx.Messages)-1]
	if last.Role != "tool" || last.Content != "short verbatim result" {
		t.Fatalf("most recent tool result must stay verbatim, got %q", last.Content)
	}
}

// TestTrimThreshold_MeasuredTokensTrigger verifies the trigger fires even
// when the heuristic estimate is low, as long as the API-measured prompt
// tokens (fed via SetPromptTokens) put the conversation over
// DefaultCompactTriggerRatio x ContextWindow. The engine must honor the authoritative
// measurement, not its own (optimistic) heuristic.
func TestTrimThreshold_MeasuredTokensTrigger(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, testThresholdWindow)

	fillHistory(ag.Ctx, 20, 40) // heuristic est ≈ 620, well under the 1200 limit
	limit := expectedLimit(t, ag.ContextWindow)
	if got := ag.Ctx.EstimatedTokens(); got > limit {
		t.Fatalf("precondition: heuristic est (%d) must be under limit (%d); adjust history", got, limit)
	}
	// The API reports the REAL (large) size. Calibrate: the engine now
	// believes the conversation is oversized and must compact/trim.
	ag.Ctx.SetPromptTokens(limit + 500)

	ag.trimContext(context.Background())

	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call from measured-token trigger, got %d", *calls)
	}
}

// 以下测试吸收自 dsh 对照实现（覆盖裁剪回退与窗口禁用场景）
func TestTrimContext_DisabledCompactionTrimsOldestOverThreshold(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no summarizer call expected with compaction disabled")
	}, 2000)
	ag.CompactionEnabled = false
	limit := trimLimitFor(2000)

	fillHistory(ag.Ctx, 20, 500) // est ≈ 5220 > 1200

	ag.trimContext(context.Background())

	if *calls != 0 {
		t.Fatalf("expected no HTTP calls, got %d", *calls)
	}
	if ag.Ctx.EstimatedTokens() > limit {
		t.Fatalf("context still over limit after trim: %d", ag.Ctx.EstimatedTokens())
	}
	// EVICTION CONTRACT (2026-09-30, trimToFitStickyHead): the head IS the
	// victim — the oldest message is evicted, in one amortized chunk down to
	// the target waterline rather than a slice per round (that nibbling is the
	// measured cache defect; devdoc/chat-cache-hit-attribution.md). Recency is
	// preserved: the newest turn is never the victim.
	if strings.Contains(ag.Ctx.Messages[0].Content, "user-00") {
		t.Fatalf("expected the oldest message evicted, got head %q", ag.Ctx.Messages[0].Content)
	}
	if last := ag.Ctx.Messages[len(ag.Ctx.Messages)-1]; !strings.Contains(last.Content, "assistant-19") {
		t.Fatalf("expected newest message preserved, got %q", last.Content)
	}
	for _, m := range ag.Ctx.Messages {
		if strings.Contains(m.Content, memorySummaryMarker) {
			t.Fatalf("memory block must not appear when compaction disabled: %q", m.Content)
		}
	}
}

func TestTrimContext_NoOpWhenWindowDisabled(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no call expected with ContextWindow <= 0")
	}, 0)

	fillHistory(ag.Ctx, 20, 500)
	origCount := len(ag.Ctx.Messages)

	ag.trimContext(context.Background())

	if *calls != 0 {
		t.Fatalf("expected no HTTP calls, got %d", *calls)
	}
	if len(ag.Ctx.Messages) != origCount {
		t.Fatalf("expected no-op with window 0, got %d → %d messages", origCount, len(ag.Ctx.Messages))
	}
}

// 以下辅助函数吸收自 dsh 对照实现
func trimLimitFor(window int) int {
	return int(float64(window) * DefaultCompactTriggerRatio)
}

func fitWindowToEstimate(ag *Agent, est int) {
	ag.ContextWindow = int(math.Ceil(float64(est) / DefaultCompactTriggerRatio))
}
