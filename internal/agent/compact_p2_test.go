package agent

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

	"github.com/nick/gopi/internal/deepseek"
)

// compact_p2_test.go — P2 acceptance tests: mechanical Anchor Index
// (needle-facts survive the LLM summary), debounce subdivision + recovery
// probe, and the total summary-input 160K cap.

// --- ① Anchor Index (§3.1) ------------------------------------------------

func TestBuildAnchorIndex_HarvestsNeedleFacts(t *testing.T) {
	// A transcript carrying the exact facts a 10:1 LLM summary would lose:
	// a 40-hex commit SHA, a short SHA, a Go file path, an error string, a
	// version number and a URL.
	msgs := []deepseek.Message{
		{Role: "user", Content: "fix the flaky test in internal/agent/compact.go related to commit 8b0a704"},
		{Role: "assistant", Content: "fixed, saw a nil pointer dereference: panic: index out of range"},
		{Role: "user", Content: "bump to v2.7.1 and reference https://github.com/nick/gopi/pull/42"},
	}

	anchors := buildAnchorIndex(msgs)
	if anchors == "" {
		t.Fatal("expected non-empty anchor index")
	}
	if !strings.HasPrefix(anchors, "\n\n"+anchorHeading) {
		t.Fatalf("expected %s heading, got %q", anchorHeading, anchors)
	}

	// File path.
	if !strings.Contains(anchors, "internal/agent/compact.go") {
		t.Fatalf("expected file path anchor, got %q", anchors)
	}
	// Commit SHA (short SHA 8b0a704).
	if !strings.Contains(anchors, "8b0a704") {
		t.Fatalf("expected commit SHA anchor, got %q", anchors)
	}
	// Error string.
	if !strings.Contains(anchors, "panic: index out of range") && !strings.Contains(anchors, "index out of range") {
		t.Fatalf("expected error anchor, got %q", anchors)
	}
	// Version number.
	if !strings.Contains(anchors, "2.7.1") {
		t.Fatalf("expected version anchor, got %q", anchors)
	}
	// URL.
	if !strings.Contains(anchors, "https://github.com/nick/gopi/pull/42") {
		t.Fatalf("expected URL anchor, got %q", anchors)
	}
}

func TestBuildAnchorIndex_Empty(t *testing.T) {
	// No content → no anchor block.
	if got := buildAnchorIndex([]deepseek.Message{{Role: "user", Content: ""}}); got != "" {
		t.Fatalf("expected empty anchor index for empty content, got %q", got)
	}
	if got := buildAnchorIndex(nil); got != "" {
		t.Fatalf("expected empty anchor index for nil messages, got %q", got)
	}
}

func TestCompact_SummaryGetsAnchorsAppended(t *testing.T) {
	// The summarizer returns a plain summary; the Anchor Index must be
	// mechanically appended so needle-facts from the compressed region
	// survive in the memory block.
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("MEMORY: fixed the flaky test"))
	}, 2000)
	fillHistorySized(ag.Ctx, []deepseek.Message{
		{Role: "user", Content: "commit 8b0a704 broke internal/agent/compact.go with panic: index out of range"},
		{Role: "assistant", Content: "reverted to v2.7.1 at https://github.com/nick/gopi/pull/42"},
	})
	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	head := ag.Ctx.Messages[0]
	if !strings.HasPrefix(head.Content, memorySummaryMarker) {
		t.Fatalf("expected memory block at head, got %q", head.Content)
	}
	if !strings.Contains(head.Content, "MEMORY: fixed the flaky test") {
		t.Fatalf("summary text not present: %q", head.Content)
	}
	// The mechanically-extracted anchors ride along.
	if !strings.Contains(head.Content, anchorHeading) {
		t.Fatalf("expected anchor heading in memory block, got %q", head.Content)
	}
	for _, want := range []string{"8b0a704", "internal/agent/compact.go", "index out of range", "2.7.1", "https://github.com/nick/gopi/pull/42"} {
		if !strings.Contains(head.Content, want) {
			t.Fatalf("expected anchor %q in memory block, got %q", want, head.Content)
		}
	}
}

// fillHistorySized loads the context with the given explicit messages so the
// test controls exactly which facts appear (fillHistory's generic gg strings
// are anchor-free).
func fillHistorySized(c *Context, msgs []deepseek.Message) {
	for _, m := range msgs {
		switch m.Role {
		case "user":
			c.AddUserMessage(m.Content)
		case "assistant":
			c.AddAssistantMessage(m.Content, nil)
		default:
			c.AddToolResult(m.ToolCallID, m.Content)
		}
	}
	// Push the estimate far over the trigger by appending a final lively
	// turn so compaction fires on the full context.
	for i := 0; i < 12; i++ {
		c.AddUserMessage(fmt.Sprintf("extra-user-%02d-%s", i, strings.Repeat("x", 500)))
		c.AddAssistantMessage(fmt.Sprintf("extra-assistant-%02d-%s", i, strings.Repeat("x", 500)), nil)
	}
}

// --- ② debounce subdivision + recovery probe (§3.2) -----------------------

func TestCompact_RecoveryProbeAllowsRetryAfterDebounce(t *testing.T) {
	// A huge fake summary makes every compression save <10% → the breaker
	// trips after two. The recovery probe must then let one attempt through
	// once the recovery window elapses, and an effective probe clears the
	// breaker.
	summaryBody := strings.Repeat("S", 20000) // ≈ 5000 tokens → each save <10%
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse(summaryBody))
	}, 2000)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)

	// Rounds 1 & 2: tiny removable zone → savings <10% → ineffective ×2.
	fillHistory(ag.Ctx, 6, 500)
	ag.trimContext(context.Background())
	if ag.compactIneffectiveCount != 1 {
		t.Fatalf("round 1: expected ineffective count 1, got %d", ag.compactIneffectiveCount)
	}
	fillHistory(ag.Ctx, 1, 500)
	ag.trimContext(context.Background())
	if ag.compactIneffectiveCount != 2 {
		t.Fatalf("round 2: expected ineffective count 2, got %d", ag.compactIneffectiveCount)
	}
	// The recovery probe must now be armed.
	if ag.compactRecoveryProbeAt.IsZero() {
		t.Fatal("expected the recovery probe to be armed once the breaker tripped")
	}

	// Round 3: still before the recovery window → debounced (no call).
	fillHistory(ag.Ctx, 1, 500)
	before3 := *calls
	ag.trimContext(context.Background())
	if *calls != before3 {
		t.Fatalf("round 3: expected debounce skip before recovery window, got %d calls", *calls)
	}

	// Round 4: recovery window elapses → probe allows one attempt (counter
	// drops to 1) which re-trips because the summary is still huge.
	ag.compactRecoveryProbeAt = time.Now().Add(-time.Minute) // already elapsed
	fillHistory(ag.Ctx, 6, 500)
	before4 := *calls
	ag.trimContext(context.Background())
	if *calls != before4+1 {
		t.Fatalf("round 4: expected the recovery probe to attempt a compaction, got %d → %d calls", before4, *calls)
	}
	if ag.compactIneffectiveCount != 2 {
		t.Fatalf("round 4: expected ineffective probe to re-trip the breaker, got count %d", ag.compactIneffectiveCount)
	}
	if ag.compactRecoveryProbeAt.IsZero() {
		t.Fatal("round 4: expected the recovery probe to re-arm after an ineffective probe")
	}
	if !strings.Contains(buf.String(), "recovery") {
		t.Fatalf("expected recovery log, got:\n%s", buf.String())
	}

	// Round 5: a genuinely effective compression (small summary) after the
	// probe clears the breaker entirely.
	ag2, calls2 := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SMALL SUMMARY"))
	}, 2000)
	ag2.compactIneffectiveCount = 2
	ag2.compactRecoveryProbeAt = time.Now().Add(-time.Minute)
	fillHistory(ag2.Ctx, 6, 500)
	before5 := *calls2
	ag2.trimContext(context.Background())
	if *calls2 != before5+1 {
		t.Fatalf("round 5: expected the probe to attempt a compaction, got %d → %d calls", before5, *calls2)
	}
	if ag2.compactIneffectiveCount != 0 {
		t.Fatalf("round 5: expected effective probe to clear the breaker, got count %d", ag2.compactIneffectiveCount)
	}
	if !ag2.compactRecoveryProbeAt.IsZero() {
		t.Fatal("round 5: expected effective probe to clear the recovery probe")
	}
}

// --- ③ summary input total cap (§3.3) -------------------------------------

func TestBoundSummaryInput_UnderCapUntouched(t *testing.T) {
	content := "short input under the cap"
	if got := boundSummaryInput(content); got != content {
		t.Fatalf("expected under-cap input returned verbatim, got %q", got)
	}
}

func TestBoundSummaryInput_CapsAndKeepsHeadTail(t *testing.T) {
	// Build an input far over the (small) cap with distinctive head and tail.
	const cap = 1000
	head := "HEAD SETUP " + strings.Repeat("a", 900)
	tail := strings.Repeat("z", 900) + " TAIL STATE"
	content := head + strings.Repeat("m", 5000) + tail

	out := boundSummaryInputMax(content, cap)
	if len(out) > cap {
		t.Fatalf("output %d chars exceeds cap %d", len(out), cap)
	}
	// Head preserved (task setup) and tail preserved (current state).
	if !strings.HasPrefix(out, "HEAD SETUP") {
		t.Fatalf("expected head preserved, got %q", out)
	}
	if !strings.HasSuffix(out, "TAIL STATE") {
		t.Fatalf("expected tail preserved, got %q", out)
	}
	// Omitted middle is marked.
	if !strings.Contains(out, "omitted") && !strings.Contains(out, "capped") {
		t.Fatalf("expected omission marker, got %q", out)
	}
}
