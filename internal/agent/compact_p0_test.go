package agent

// compact_p0_test.go — P0 acceptance tests (task §4.3): trigger/target
// parameterization, keep-recent floor, summarizer input truncation,
// post-compaction assertion log, PromptTokens reset, and default values.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/deepseek"
)

// --- defaults -------------------------------------------------------------

func TestCompactDefaults_ConstantsAndEnv(t *testing.T) {
	if DefaultContextWindow != 200000 {
		t.Fatalf("DefaultContextWindow = %d, want 200000", DefaultContextWindow)
	}
	if DefaultCompactTriggerRatio != 0.75 {
		t.Fatalf("DefaultCompactTriggerRatio = %v, want 0.75", DefaultCompactTriggerRatio)
	}
	if DefaultCompactTargetRatio != 0.35 {
		t.Fatalf("DefaultCompactTargetRatio = %v, want 0.35", DefaultCompactTargetRatio)
	}
	if DefaultCompactKeepRecent != 8 {
		t.Fatalf("DefaultCompactKeepRecent = %d, want 8", DefaultCompactKeepRecent)
	}
	if DefaultCompactCooldown != 600*time.Second {
		t.Fatalf("DefaultCompactCooldown = %v, want 600s", DefaultCompactCooldown)
	}
	if got := defaultMaxSummaryTokens(200000); got != 10000 { // min(5% × 200K, 12K)
		t.Fatalf("defaultMaxSummaryTokens(200000) = %d, want 10000", got)
	}
	if got := defaultMaxSummaryTokens(500000); got != 12000 { // cap
		t.Fatalf("defaultMaxSummaryTokens(500000) = %d, want 12000", got)
	}

	// Explicit env values are honored.
	t.Setenv("GOPI_COMPACT_TRIGGER", "0.5")
	t.Setenv("GOPI_COMPACT_TARGET", "0.4")
	t.Setenv("GOPI_COMPACT_KEEP_RECENT", "3")
	t.Setenv("GOPI_COMPACT_MAX_SUMMARY_TOKENS", "5000")
	t.Setenv("GOPI_COMPACT_COOLDOWN", "120")
	t.Setenv("GOPI_COMPACT_REFERENCE_ONLY", "off")
	cfg := compactConfigFromEnv(200000)
	if cfg.TriggerRatio != 0.5 || cfg.TargetRatio != 0.4 || cfg.KeepRecent != 3 ||
		cfg.MaxSummaryTokens != 5000 || cfg.Cooldown != 120*time.Second || cfg.ReferenceOnly {
		t.Fatalf("unexpected config from env: %+v", cfg)
	}

	// Invalid values warn and fall back to defaults.
	t.Setenv("GOPI_COMPACT_TRIGGER", "abc")
	t.Setenv("GOPI_COMPACT_TARGET", "1.5")
	t.Setenv("GOPI_COMPACT_KEEP_RECENT", "-2")
	t.Setenv("GOPI_COMPACT_MAX_SUMMARY_TOKENS", "0")
	t.Setenv("GOPI_COMPACT_COOLDOWN", "-5")
	t.Setenv("GOPI_COMPACT_REFERENCE_ONLY", "maybe")
	cfg2 := compactConfigFromEnv(200000)
	if cfg2.TriggerRatio != 0.75 || cfg2.TargetRatio != 0.35 || cfg2.KeepRecent != 8 ||
		cfg2.MaxSummaryTokens != 10000 || cfg2.Cooldown != 600*time.Second || !cfg2.ReferenceOnly {
		t.Fatalf("expected default fallback, got %+v", cfg2)
	}
}

// --- trigger parameterization (§4.3.1) ------------------------------------

func TestCompactTriggerRatio_Parameterized(t *testing.T) {
	// est sits in (0.6, 0.75) × 2000: no trigger at the default 0.75...
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	fillHistory(ag.Ctx, 12, 200) // est ≈ 24 × 54 = 1296
	if got := ag.Ctx.EstimatedTokens(); got <= 1200 || got >= 1500 {
		t.Fatalf("precondition: est must sit in (1200, 1500), got %d", got)
	}
	ag.trimContext(context.Background())
	if *calls != 0 {
		t.Fatalf("expected no compaction at default 0.75 with est in (0.6, 0.75)×window, got %d calls", *calls)
	}
	// ...but the same history with an explicit 0.6 trigger does fire.
	ag2, calls2 := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	ag2.CompactTriggerRatio = 0.6
	fillHistory(ag2.Ctx, 12, 200)
	ag2.trimContext(context.Background())
	if *calls2 != 1 {
		t.Fatalf("expected 1 summarizer call at trigger 0.6, got %d", *calls2)
	}
	// Above 0.75 × window triggers with the default.
	ag3, calls3 := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	fillHistory(ag3.Ctx, 16, 200) // est ≈ 32 × 54 = 1728 > 1500
	ag3.trimContext(context.Background())
	if *calls3 != 1 {
		t.Fatalf("expected 1 summarizer call over the 0.75 trigger, got %d", *calls3)
	}
}

// --- target parameterization (§4.3.2) -------------------------------------

func TestCompactTargetRatio_Parameterized(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	ag.CompactKeepRecent = 0 // the tail floor would exceed the tiny test target
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	fillHistory(ag.Ctx, 20, 500) // est ≈ 5160 > 1500
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	target := int(float64(ag.ContextWindow) * ag.CompactTargetRatio)
	if got := ag.Ctx.EstimatedTokens(); got > target+EstimateTokens(ag.Ctx.Messages[0]) {
		t.Fatalf("expected est ≤ target+summary (%d+%d), got %d", target, EstimateTokens(ag.Ctx.Messages[0]), got)
	}
	if strings.Contains(buf.String(), "⚠️") {
		t.Fatalf("unexpected assertion warning: %s", buf.String())
	}
}

// --- keep-recent floor (§4.3.3) -------------------------------------------

func TestTakeOldest_KeepRecentPreservesTail(t *testing.T) {
	c := &Context{}
	fillHistory(c, 12, 500) // 24 messages
	giant := strings.Repeat("G", 50000)
	c.AddToolResult("call_giant", giant)
	taken := c.TakeOldest(1, 8, nil) // tiny budget: wants to delete everything
	_ = taken
	// The last 8 messages (incl. the 50K tool result) are untouched.
	if len(c.Messages) != 9 {
		t.Fatalf("expected 9 messages kept (keepRecent=8 floor), got %d", len(c.Messages))
	}
	last := c.Messages[len(c.Messages)-1]
	if last.Role != "tool" || last.Content != giant {
		t.Fatalf("giant tool result must survive keep-recent, got %q", last.Content)
	}
	// With keepRecent=0 there is no count floor, but the anchors still hold:
	// the last visible assistant and its trailing giant tool result stay
	// (P1, Hermes #29824 — the assistant's tool pair is never split).
	c2 := &Context{}
	fillHistory(c2, 12, 500)
	c2.AddToolResult("call_giant", giant)
	taken2 := c2.TakeOldest(1, 0, nil)
	if len(taken2) != 22 {
		t.Fatalf("expected 22 taken with keepRecent=0 (anchors hold), got %d", len(taken2))
	}
	if len(c2.Messages) != 3 || c2.Messages[2].Content != giant {
		t.Fatalf("expected anchored tail with giant result, got %d messages (last %q)", len(c2.Messages), c2.Messages[len(c2.Messages)-1].Content)
	}
}

// --- summarizer input truncation (§4.3.4) ---------------------------------

func TestSummarize_InputTruncation(t *testing.T) {
	var captured string
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		captured = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("KEY: distilled"))
	}, 2000)
	ag.CompactKeepRecent = 0
	// Old zone: 7000-char messages (truncation kicks in) with a huge-args
	// tool call in the middle, then the final instruction turn (anchors —
	// the last user/assistant must stay, so only the old zone is taken).
	big := strings.Repeat("x", 7000)
	ag.Ctx.AddUserMessage("user-00-" + big)
	ag.Ctx.AddAssistantMessage("assistant-00-"+big, nil)
	ag.Ctx.AddAssistantMessage("", []deepseek.ToolCall{{
		ID:        "call_huge",
		Name:      "big_tool",
		Arguments: map[string]interface{}{"data": strings.Repeat("z", 10000)},
	}})
	ag.Ctx.AddToolResult("call_huge", "ok")
	ag.Ctx.AddUserMessage("user-01-" + big)
	ag.Ctx.AddAssistantMessage("assistant-01-"+big, nil)
	ag.Ctx.AddUserMessage("user-02-FINAL")
	ag.Ctx.AddAssistantMessage("assistant-02-FINAL", nil)
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	// The summarizer receives exactly one user message: the serialized block.
	var wire struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(captured), &wire); err != nil || len(wire.Messages) != 1 {
		t.Fatalf("expected 1 user message in summarizer request, got %d (err %v)", len(wire.Messages), err)
	}
	serialized := wire.Messages[0].Content[0].Text
	if !strings.Contains(serialized, "[truncated]") {
		t.Fatal("expected content truncation marker in summarizer input")
	}
	if !strings.Contains(serialized, "[tool_call big_tool({\"data\":\"") {
		t.Fatal("expected tool call with truncated args head in summarizer input")
	}
	if strings.Contains(serialized, strings.Repeat("z", 2000)) {
		t.Fatal("tool args must be truncated to ≤1500 chars")
	}
	// Key information at the head of each message survives truncation.
	if !strings.Contains(serialized, "user-00-") {
		t.Fatal("message head must survive truncation")
	}
	// No single serialized message exceeds the 6000-char cap + label overhead.
	if strings.Contains(serialized, strings.Repeat("x", 6000)) {
		t.Fatal("content must be truncated to ≤6000 chars")
	}
}

// --- assertion log (§4.3.5) ----------------------------------------------

func TestCompact_AssertionLog(t *testing.T) {
	// TRUE case: window 10000 — the kept tail (9 × ~129) is small vs the
	// 3500-token target, so the post-compaction estimate fits target+summary.
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 10000)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	fillHistory(ag.Ctx, 60, 500) // est ≈ 15480 > 7500
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	if !strings.Contains(buf.String(), "assertion: after ≤ target+summary = true") {
		t.Fatalf("expected true assertion in log, got:\n%s", buf.String())
	}
	// FALSE case: window 2000 with keepRecent=8 — the kept tail alone
	// exceeds the 700-token target (est after ≈ 1161+summary > 700+summary).
	ag2, calls2 := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	var buf2 bytes.Buffer
	ag2.Logger = log.New(&buf2, "", 0)
	fillHistory(ag2.Ctx, 20, 500)
	ag2.trimContext(context.Background())
	if *calls2 != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls2)
	}
	if !strings.Contains(buf2.String(), "⚠️") || !strings.Contains(buf2.String(), "assertion: after ≤ target+summary = false") {
		t.Fatalf("expected warning assertion in log, got:\n%s", buf2.String())
	}
}

// --- GOPI_COMPACT=off still trims (§4.3.6) --------------------------------
// Covered by TestTrimContext_DisabledFallsBackToTrim and
// TestTrimContext_DisabledCompactionTrimsOldestOverThreshold (both assert
// the TrimToFit fallback path with zero summarizer calls).

// --- PromptTokens reset (§4.3.7) ------------------------------------------

func TestCompact_ResetsPromptTokensMeasurement(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	fillHistory(ag.Ctx, 20, 500)
	ag.Ctx.SetPromptTokens(5000) // stale authoritative measurement
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	if ag.Ctx.PromptTokens != 0 || ag.Ctx.PromptTokensIndex != 0 {
		t.Fatalf("expected PromptTokens reset after compaction, got %d/%d", ag.Ctx.PromptTokens, ag.Ctx.PromptTokensIndex)
	}
}

// --- serialization unit checks --------------------------------------------

func TestSerializeForSummary_Format(t *testing.T) {
	out := serializeForSummary([]deepseek.Message{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "", ToolCalls: []deepseek.ToolCall{{
			ID: "call_1", Name: "bash",
			Arguments: map[string]interface{}{"command": "ls"},
		}}},
		{Role: "tool", Content: "exit=0", ToolCallID: "call_1"},
	})
	for _, want := range []string{"[user]: hello world", "[assistant]: ", `[tool_call bash({"command":"ls"})]`, "[tool_call_id=call_1]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("serialized output missing %q:\n%s", want, out)
		}
	}
}

func TestTruncateMiddle(t *testing.T) {
	got := truncateMiddle(strings.Repeat("a", 7000), 6000, 4000, 1500)
	if got != strings.Repeat("a", 4000)+"\n...[truncated]...\n"+strings.Repeat("a", 1500) {
		t.Fatalf("unexpected truncation result (len %d)", len(got))
	}
	if s := truncateMiddle("short", 6000, 4000, 1500); s != "short" {
		t.Fatalf("short strings must pass through, got %q", s)
	}
	if s := truncateMiddle(strings.Repeat("b", 2000), 1500, 1200, 0); s != strings.Repeat("b", 1200)+"..." {
		t.Fatalf("tail-0 truncation must append ..., got len %d", len(s))
	}
}

// TestSerializeForSummary_SelfRecordVerbatim pins the fix for the placeholder
// defect: a self-recording note block must appear in the summarizer's excerpt
// CHARACTER-FOR-CHARACTER, not replaced by the omission marker it used to
// produce.
//
// The two assertions are deliberately opposite halves of one behaviour, so a
// revert lands here rather than in a gate test three files away: the needle is
// PRESENT (the block is passed through) and the marker is ABSENT (nothing is
// standing in its place). The carrier message is included as a second case
// because isSelfRecordMessage recognises BOTH forms — the model's own round
// message carrying the block, and the re-injected carrier — and a fix that
// covered only one of them would leave the other omitted.
func TestSerializeForSummary_SelfRecordVerbatim(t *testing.T) {
	const needle = "ROUND-NOTE-NEEDLE-9f3a"
	cases := map[string]deepseek.Message{
		"model message carrying the block": {
			Role: "assistant",
			Content: "the answer body\n" + selfRecordOpenMarker + "\n" +
				"Finished the parser rewrite " + needle + ". | the vendored SDK pins go1.21\n" +
				selfRecordCloseMarker + "\n",
		},
		"re-injected carrier": {
			Role:    "user",
			Content: selfRecordMarker + "\n- Finished the parser rewrite " + needle + ".\n",
		},
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			out := serializeForSummary([]deepseek.Message{msg})
			if !strings.Contains(out, needle) {
				t.Fatalf("the note text is not in the summarizer input — it was replaced by a placeholder:\n%s", out)
			}
			if strings.Contains(out, "omitted from the summary input") {
				t.Fatalf("the omission placeholder is still emitted:\n%s", out)
			}
		})
	}
}
