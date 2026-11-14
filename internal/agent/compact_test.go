package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// --- helpers -------------------------------------------------------------

// newCompactTestAgent builds an Agent whose Claude client points at a fake
// server. The window is small so the trim threshold trips with a handful of
// messages. Returns the agent and a counter of HTTP requests made.
func newCompactTestAgent(t *testing.T, handler http.HandlerFunc, window int) (*Agent, *int) {
	t.Helper()
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:     server.URL,
			APIKey:      "test-key",
			HTTPClient:  server.Client(),
			RetryConfig: deepseek.DefaultRetryConfig(),
		},
		Model:             "test-model",
		Logger:            log.New(io.Discard, "", 0),
		ContextWindow:     window,
		CompactionEnabled: true,
		// AutoReduceEnabled: this helper builds a run that has OPTED IN to
		// automatic context reduction (GOPI_CONTEXT_AUTOREDUCE=1), which is the
		// only configuration in which trimContext mutates the conversation at all
		// (context_pressure.go). Every test below is about the reduction path, so
		// the opt-in is part of the fixture; the DEFAULT (warn-only) path has its
		// own test, context_pressure_test.go.
		AutoReduceEnabled:       true,
		CompactTriggerRatio:     DefaultCompactTriggerRatio,
		CompactTargetRatio:      DefaultCompactTargetRatio,
		CompactKeepRecent:       DefaultCompactKeepRecent,
		CompactMaxSummaryTokens: defaultMaxSummaryTokens(window),
		CompactCooldown:         DefaultCompactCooldown,
		CompactReferenceOnly:    true,
		lastCompactSavingsPct:   100.0,
		Ctx:                     &Context{},
	}
	return ag, &callCount
}

// fillHistory loads the context with alternating user/assistant messages of
// the given approximate size; contents embed a unique index per message.
func fillHistory(c *Context, n, msgSize int) {
	big := strings.Repeat("x", msgSize)
	for i := 0; i < n; i++ {
		c.AddUserMessage(fmt.Sprintf("user-%02d-%s", i, big))
		c.AddAssistantMessage(fmt.Sprintf("assistant-%02d-%s", i, big), nil)
	}
}

// compactTextResponse is a minimal non-streaming Claude response.
func compactTextResponse(text string) string {
	t, _ := json.Marshal(text) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	return `{"id":"m1","content":[{"type":"text","text":` + string(t) + `}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`
}

// --- tests ---------------------------------------------------------------

func TestTrimContext_CompactsWithSummary(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("MEMORY: goal preserved, two files written"))
	}, 2000)

	fillHistory(ag.Ctx, 20, 500) // est ≈ 20 * 126 ≈ 2500 > 1800 limit
	lastContent := ag.Ctx.Messages[len(ag.Ctx.Messages)-1].Content

	ag.trimContext(context.Background())

	if *calls != 1 {
		t.Fatalf("expected exactly 1 summarizer call, got %d", *calls)
	}
	// The memory block heads the history.
	head := ag.Ctx.Messages[0]
	if head.Role != "user" || !strings.Contains(head.Content, memorySummaryMarker) {
		t.Fatalf("expected memory summary at head, got %q", head.Content)
	}
	if !strings.Contains(head.Content, "MEMORY: goal preserved") {
		t.Fatalf("summary text not present: %q", head.Content)
	}
	// The oldest messages are gone; the most recent one survives verbatim.
	var keptUsers int
	for _, m := range ag.Ctx.Messages {
		if strings.HasPrefix(m.Content, "user-") {
			keptUsers++
		}
	}
	if keptUsers == 0 || keptUsers >= 20 {
		t.Fatalf("expected some oldest user messages removed, some recent kept: %d kept", keptUsers)
	}
	if ag.Ctx.Messages[len(ag.Ctx.Messages)-1].Content != lastContent {
		t.Fatalf("most recent message not preserved")
	}
	// Context now fits under the trim limit.
	if ag.Ctx.EstimatedTokens() > int(float64(ag.ContextWindow)*DefaultCompactTriggerRatio) {
		t.Fatalf("context still over limit after compaction: %d", ag.Ctx.EstimatedTokens())
	}
}

func TestTrimContext_CompactionFallsBackToTrim(t *testing.T) {
	// The summarizer fails with a non-retryable 400 → the oldest messages
	// stay removed (plain trim) and no memory block is inserted.
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusBadRequest)
	}, 2000)

	fillHistory(ag.Ctx, 20, 500)

	ag.trimContext(context.Background())

	if *calls == 0 {
		t.Fatal("expected the summarizer to be attempted")
	}
	for _, m := range ag.Ctx.Messages {
		if strings.Contains(m.Content, memorySummaryMarker) {
			t.Fatalf("memory block inserted despite failure: %q", m.Content)
		}
	}
	if ag.Ctx.EstimatedTokens() > int(float64(ag.ContextWindow)*DefaultCompactTriggerRatio) {
		t.Fatalf("context still over limit after fallback trim: %d", ag.Ctx.EstimatedTokens())
	}
}

func TestTrimContext_NoCallUnderThreshold(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no summarizer call expected")
	}, 2000)

	fillHistory(ag.Ctx, 4, 100) // est well under the 1800 limit

	ag.trimContext(context.Background())

	if *calls != 0 {
		t.Fatalf("expected no HTTP calls, got %d", *calls)
	}
}

func TestTrimContext_DisabledFallsBackToTrim(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no summarizer call expected when compaction disabled")
	}, 2000)
	ag.CompactionEnabled = false

	fillHistory(ag.Ctx, 20, 500)

	ag.trimContext(context.Background())

	if *calls != 0 {
		t.Fatalf("expected no HTTP calls, got %d", *calls)
	}
	if ag.Ctx.EstimatedTokens() > int(float64(ag.ContextWindow)*DefaultCompactTriggerRatio) {
		t.Fatalf("context still over limit after plain trim: %d", ag.Ctx.EstimatedTokens())
	}
	for _, m := range ag.Ctx.Messages {
		if strings.Contains(m.Content, memorySummaryMarker) {
			t.Fatalf("memory block inserted while disabled")
		}
	}
}

// --- measured token feedback --------------------------------------------

func TestSetPromptTokens_CalibratesEstimate(t *testing.T) {
	c := &Context{}
	fillHistory(c, 4, 500)

	// No measurement yet: pure heuristic estimate.
	heuristic := c.EstimatedTokens()
	if heuristic <= 0 {
		t.Fatalf("expected positive heuristic estimate, got %d", heuristic)
	}

	// After a response, the API reports the authoritative count.
	c.SetPromptTokens(1000)
	if got := c.EstimatedTokens(); got != 1000 {
		t.Fatalf("expected measured 1000, got %d", got)
	}

	// New messages since the measurement are heuristic on top of the base.
	c.AddUserMessage("fresh-" + strings.Repeat("y", 400))
	got := c.EstimatedTokens()
	if got <= 1000 {
		t.Fatalf("expected estimate > measured base after new messages, got %d", got)
	}
	if got >= 1000+EstimateTokens(c.Messages[len(c.Messages)-1])+1 {
		t.Fatalf("expected estimate = base + tail heuristic, got %d", got)
	}
}

func TestSetPromptTokens_StaleAfterTrimFallsBackToHeuristic(t *testing.T) {
	c := &Context{}
	fillHistory(c, 10, 500)
	c.SetPromptTokens(5000)

	c.TakeOldest(1000, 0, nil) // trims below the measured index — measurement is stale

	if got := c.EstimatedTokens(); got <= 0 || got >= 5000 {
		t.Fatalf("expected heuristic fallback after trim, got %d", got)
	}
}

func TestTakeOldest_ReturnsOldestInOrder(t *testing.T) {
	c := &Context{}
	c.AddUserMessage("epic")
	c.AddAssistantMessage("first answer", nil)
	c.AddToolResult("call_1", "first result")
	c.AddAssistantMessage("second answer", nil)
	c.AddToolResult("call_2", "second result")

	taken := c.TakeOldest(1000, 0, nil) // generous budget: nothing removed

	if len(taken) != 0 || len(c.Messages) != 5 {
		t.Fatalf("expected no-op with big budget, got taken=%d kept=%d", len(taken), len(c.Messages))
	}

	// Tiny budget: everything before the anchors goes. The last visible
	// assistant message ("second answer") is an anchor (P1, Hermes #29824) —
	// it stays even when the budget wants everything, together with its
	// trailing tool result (orphan protection keeps the pair intact).
	taken = c.TakeOldest(1, 0, nil)

	if len(taken) != 3 {
		t.Fatalf("expected 3 taken, got %d", len(taken))
	}
	if taken[0].Content != "epic" || taken[2].Content != "first result" {
		t.Fatalf("taken order not preserved: %v", taken)
	}
	if len(c.Messages) != 2 || c.Messages[0].Content != "second answer" || c.Messages[1].ToolCallID != "call_2" {
		t.Fatalf("expected anchor + trailing tool result kept, got %v", c.Messages)
	}
}
