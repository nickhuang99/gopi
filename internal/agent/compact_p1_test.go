package agent

// compact_p1_test.go — P1 acceptance tests (task §5.6): REFERENCE ONLY
// prefix + end marker, keep-recent anchors, memory_store round protection,
// failure cooldown, and ineffective-compression debounce.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// --- REFERENCE ONLY prefix (§5.6.1) ---------------------------------------

func TestCompact_ReferenceOnlyPrefix(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	fillHistory(ag.Ctx, 20, 500)
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	head := ag.Ctx.Messages[0]
	if !strings.HasPrefix(head.Content, memorySummaryMarker) {
		t.Fatalf("expected reference-only marker at head, got %q", head.Content)
	}
	for _, want := range []string{memorySummaryEndMarker, "REFERENCE ONLY", "memory(action=search)", "NOT as active instructions", "past tense"} {
		if !strings.Contains(head.Content, want) {
			t.Fatalf("expected %q in summary message, got %q", want, head.Content)
		}
	}
	// The marker still carries the legacy prefix (non-breaking: the old
	// label "[MEMORY COMPACTION — EARLIER WORK" is retained verbatim).
	if !strings.HasPrefix(head.Content, "[MEMORY COMPACTION — EARLIER WORK") {
		t.Fatal("reference-only marker must keep the legacy label prefix")
	}

	// GOPI_COMPACT_REFERENCE_ONLY=off: legacy single-line label, no end
	// marker, no reference-only semantics.
	ag2, calls2 := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	ag2.CompactReferenceOnly = false
	fillHistory(ag2.Ctx, 20, 500)
	ag2.trimContext(context.Background())
	if *calls2 != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls2)
	}
	head2 := ag2.Ctx.Messages[0]
	if !strings.HasPrefix(head2.Content, memorySummaryLegacyMarker) || strings.HasPrefix(head2.Content, memorySummaryMarker) {
		t.Fatalf("expected legacy marker, got %q", head2.Content)
	}
	if strings.Contains(head2.Content, memorySummaryEndMarker) || strings.Contains(head2.Content, "REFERENCE ONLY") {
		t.Fatalf("legacy mode must not carry reference-only semantics, got %q", head2.Content)
	}
}

// --- anchors (§5.6.2) -----------------------------------------------------

func TestCompact_AnchorsPreserveLastUserAndAssistant(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	ag.CompactKeepRecent = 0 // anchors alone must stop the deletion
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	// Old zone, then a final instruction turn with a giant tool result that
	// keeps the estimate far above target even after everything deletable
	// is gone — the anchors must hold anyway.
	fillHistory(ag.Ctx, 10, 300) // 20 messages, est ≈ 20×79 ≈ 1580 > 1500
	ag.Ctx.AddUserMessage("FINAL TASK INSTRUCTION")
	ag.Ctx.AddAssistantMessage("final visible answer", nil)
	ag.Ctx.AddToolResult("call_giant", strings.Repeat("G", 50000))
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	var joined string
	for _, m := range ag.Ctx.Messages {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "FINAL TASK INSTRUCTION") {
		t.Fatal("last user message must survive compaction")
	}
	if !strings.Contains(joined, "final visible answer") {
		t.Fatal("last visible assistant message must survive compaction")
	}
	if !strings.Contains(joined, strings.Repeat("G", 50000)) {
		t.Fatal("giant tool result after the anchors must survive")
	}
	if !strings.Contains(buf.String(), "anchored at last user") {
		t.Fatalf("expected anchor log, got:\n%s", buf.String())
	}
	// The memory block heads the context.
	if !strings.HasPrefix(ag.Ctx.Messages[0].Content, memorySummaryMarker) {
		t.Fatalf("expected memory block at head, got %q", ag.Ctx.Messages[0].Content)
	}
}

// --- memory_store round protection (§5.6.3) -------------------------------

func TestCompact_StoreRoundsProtected(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	// Old non-store zone (distinct labels — fillHistory would reuse the
	// same "user-00-" strings in the post-store zone), three memory_store
	// distillation rounds, more non-store rounds, then a final instruction
	// turn.
	oldBig := strings.Repeat("x", 700)
	for i := 0; i < 2; i++ {
		ag.Ctx.AddUserMessage(fmt.Sprintf("OLDZONE-u%d-%s", i, oldBig))
		ag.Ctx.AddAssistantMessage(fmt.Sprintf("OLDZONE-a%d-%s", i, oldBig), nil)
	}
	for i := 1; i <= 3; i++ {
		ag.Ctx.AddAssistantMessage("", []deepseek.ToolCall{{
			ID:   fmt.Sprintf("call_s%d", i),
			Name: toolnames.ToolMemory,
			Arguments: map[string]interface{}{
				"action":   toolnames.ToolMemoryActionStore,
				"topic":    fmt.Sprintf("topic-%d", i),
				"question": "question",
				"answer":   strings.Repeat("a", 600),
			},
		}})
		ag.Ctx.AddToolResult(fmt.Sprintf("call_s%d", i), fmt.Sprintf("Memory stored (id=%d).", i))
	}
	fillHistory(ag.Ctx, 2, 700)
	ag.Ctx.AddUserMessage("final task")
	ag.Ctx.AddAssistantMessage("final answer", nil)
	before := len(ag.Ctx.Messages)
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	// All three store rounds (assistant + tool result) survive compaction.
	storeAssistants := 0
	for _, m := range ag.Ctx.Messages {
		for _, tc := range m.ToolCalls {
			if tc.Name == toolnames.ToolMemory {
				storeAssistants++
			}
		}
	}
	if storeAssistants != 3 {
		t.Fatalf("expected 3 store rounds preserved, got %d", storeAssistants)
	}
	for _, id := range []string{"call_s1", "call_s2", "call_s3"} {
		found := false
		for _, m := range ag.Ctx.Messages {
			if m.Role == "tool" && m.ToolCallID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("store tool result %s lost", id)
		}
	}
	// The old non-store zone was compacted away.
	var joined string
	for _, m := range ag.Ctx.Messages {
		joined += m.Content + "\n"
	}
	if strings.Contains(joined, "OLDZONE-") {
		t.Fatal("old non-store zone must be compacted")
	}
	if len(ag.Ctx.Messages) >= before {
		t.Fatalf("expected compaction to remove messages, %d → %d", before, len(ag.Ctx.Messages))
	}
	// Final instruction + answer preserved (anchors).
	if !strings.Contains(joined, "final task") || !strings.Contains(joined, "final answer") {
		t.Fatal("final instruction turn must survive")
	}
}

func TestCompact_AllStoreRoundsFallbackToCompress(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	ag.CompactKeepRecent = 0
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	for i := 1; i <= 3; i++ {
		ag.Ctx.AddAssistantMessage("", []deepseek.ToolCall{{
			ID:   fmt.Sprintf("call_s%d", i),
			Name: toolnames.ToolMemory,
			Arguments: map[string]interface{}{
				"action":   toolnames.ToolMemoryActionStore,
				"topic":    fmt.Sprintf("t%d", i),
				"question": "q",
				"answer":   strings.Repeat("a", 3000),
			},
		}})
		ag.Ctx.AddToolResult(fmt.Sprintf("call_s%d", i), fmt.Sprintf("Memory stored (id=%d).", i))
	}
	ag.Ctx.AddUserMessage("final task")
	ag.Ctx.AddAssistantMessage("final answer", nil)
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call via fallback, got %d", *calls)
	}
	if !strings.Contains(buf.String(), "store rounds protected everything") {
		t.Fatalf("expected fallback log, got:\n%s", buf.String())
	}
	if head := ag.Ctx.Messages[0]; head.Role != "user" || !strings.HasPrefix(head.Content, memorySummaryMarker) {
		t.Fatalf("expected memory block at head after fallback compaction, got %q", head.Content)
	}
}

// --- failure cooldown (§5.6.4) --------------------------------------------

func TestCompact_FailureCooldown(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
	}, 2000)
	ag.Client.RetryConfig.MaxRetries = 0 // keep the 500 test fast
	fillHistory(ag.Ctx, 20, 500)
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer attempt, got %d", *calls)
	}
	if ag.compactFailureCooldownUntil.IsZero() {
		t.Fatal("expected cooldown set after summarizer failure")
	}
	// Second trim while in cooldown: no new summarizer attempt, plain trim
	// keeps the context bounded.
	fillHistory(ag.Ctx, 20, 500)
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected no summarizer attempt during cooldown, got %d", *calls)
	}
	if ag.Ctx.EstimatedTokens() > int(float64(ag.ContextWindow)*DefaultCompactTriggerRatio) {
		t.Fatalf("context must stay bounded during cooldown, est %d", ag.Ctx.EstimatedTokens())
	}
	// Cooldown expiry restores summary attempts.
	ag.compactFailureCooldownUntil = time.Time{}
	fillHistory(ag.Ctx, 20, 500)
	ag.trimContext(context.Background())
	if *calls != 2 {
		t.Fatalf("expected summarizer attempt after cooldown, got %d", *calls)
	}
}

func TestCompact_SuccessClearsCooldown(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	ag.compactFailureCooldownUntil = time.Now().Add(10 * time.Minute)
	fillHistory(ag.Ctx, 20, 500)
	ag.compactContext(context.Background()) // direct call — trimContext's gate would skip
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	if !ag.compactFailureCooldownUntil.IsZero() {
		t.Fatal("successful compaction must clear the cooldown")
	}
}

// --- ineffective-compression debounce (§5.6.5) ----------------------------

func TestCompact_IneffectiveDebounce(t *testing.T) {
	// A huge fake summary makes every compression save <10% (the summary
	// replaces roughly what was removed) — the anti-thrash guard must kick
	// in after two, and a genuinely effective compression resets it.
	summaryBody := strings.Repeat("S", 20000) // ≈ 5000 tokens
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse(summaryBody))
	}, 2000)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)

	// Round 1: tiny removable zone → savings < 10% → ineffective #1.
	fillHistory(ag.Ctx, 6, 500) // 12 messages ≈ 1548 tokens + 5000-token summary
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("round 1: expected 1 summarizer call, got %d", *calls)
	}
	if ag.compactIneffectiveCount != 1 {
		t.Fatalf("round 1: expected ineffective count 1, got %d", ag.compactIneffectiveCount)
	}
	// Round 2: same → ineffective #2.
	fillHistory(ag.Ctx, 1, 500)
	ag.trimContext(context.Background())
	if *calls != 2 {
		t.Fatalf("round 2: expected 1 more summarizer call, got %d", *calls)
	}
	if ag.compactIneffectiveCount != 2 {
		t.Fatalf("round 2: expected ineffective count 2, got %d", ag.compactIneffectiveCount)
	}
	// Round 3: debounced — no summarizer call, plain trim, warning logged.
	before3 := *calls
	fillHistory(ag.Ctx, 1, 500)
	ag.trimContext(context.Background())
	if *calls != before3 {
		t.Fatalf("round 3: expected compaction skipped after 2 ineffective compressions, got %d calls", *calls)
	}
	if !strings.Contains(buf.String(), "saved <10% each") {
		t.Fatalf("expected debounce warning, got:\n%s", buf.String())
	}
	// Recovery: a compression that saves ≥10% resets the counter (direct
	// call — trimContext stays gated until the counter clears).
	summaryBody = "SMALL SUMMARY"
	fillHistory(ag.Ctx, 6, 500)
	ag.compactContext(context.Background())
	if ag.compactIneffectiveCount != 0 {
		t.Fatalf("expected ineffective count reset after effective compression, got %d", ag.compactIneffectiveCount)
	}
	// And the loop compacts again.
	before5 := *calls
	fillHistory(ag.Ctx, 6, 500)
	ag.trimContext(context.Background())
	if *calls != before5+1 {
		t.Fatalf("expected compaction resumed after reset, got %d → %d calls", before5, *calls)
	}
}

// --- no-window is a structural no-op (§5.5.4→P2) --------------------------

func TestCompact_EmptyWindowStructuralNoOp(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	// Context already under the target: TakeOldest takes nothing. This is a
	// structural no-op, NOT an ineffective strike — it arms the structural
	// no-op backoff so the scan defers, and must NOT count against the
	// anti-thrash breaker (a short session would otherwise be permanently
	// disarmed even after it grows compressible material).
	fillHistory(ag.Ctx, 2, 100) // est ≈ 4×27 ≈ 108 — well under target 700
	before := ag.compactIneffectiveCount
	ag.compactContext(context.Background())
	if *calls != 0 {
		t.Fatalf("expected no summarizer call, got %d", *calls)
	}
	if ag.compactIneffectiveCount != before {
		t.Fatalf("structural no-op must not strike the ineffective breaker: %d → %d", before, ag.compactIneffectiveCount)
	}
	if ag.compactStructuralNoOpUntil.IsZero() {
		t.Fatal("structural no-op must arm the structural no-op backoff")
	}
	if !time.Now().Before(ag.compactStructuralNoOpUntil) {
		t.Fatalf("structural no-op backoff must be in the future, got %v", ag.compactStructuralNoOpUntil)
	}
	// The backoff gates compactionPaused until it lapses.
	if !ag.compactionPaused() {
		t.Fatal("compaction must be paused during structural no-op backoff")
	}
	ag.compactStructuralNoOpUntil = time.Time{}
	if ag.compactionPaused() {
		t.Fatal("compaction must resume once the structural no-op backoff lapses")
	}
}
