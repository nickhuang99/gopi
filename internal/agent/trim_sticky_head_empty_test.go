package agent

// trim_sticky_head_empty_test.go — the NEVER-EMPTY guarantee of the
// cache-aware trim (Context.trimToFitStickyHead), which is the trim
// trimContext actually runs.
//
// WHY THIS FILE EXISTS (measured 2026-10-01). The guarantee was written for
// TrimToFit by 9b9264b9 ("a trim trades old context for budget; it must never
// trade away the request itself"), together with the outbound refusal of an
// empty array. Neither half covers the trimmer on the hot path today:
// trimToFitStickyHead was added later (6dbe5a43) with an UNCAPPED floor
// (`len(c.Messages) > 0` on both the head drop and the orphan-eviction loop),
// and a delivery-point merge that landed 9b9264b9's test corrections together
// with the sticky trimmer — but without 9b9264b9's context.go/loop.go changes —
// switched the call site to it.
//
// The visible consequence is the red test at master tip:
// TestCompact_PinnedWindowBacksOffInsteadOfLooping fails with "trim emptied the
// history — the request would serialize as messages:null (HTTP 422)", i.e. the
// exact wire defect 9b9264b9 closed is reachable again through the other
// trimmer.
//
// THE CONTRACT PINNED HERE: a trim may leave the context OVER budget — the
// anchor floors of TakeOldest do the same deliberately, and the newest message
// is the round being served — but it must never leave the history EMPTY, and
// never with an orphaned tool result as its sole head, because no valid
// request exists with `"messages": null`.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// Named limits (Rule 1): the fixture's window and payload sizes.
const (
	// stickyHeadReproWindow is a context window small enough that the
	// fixture's single giant tool result alone exceeds the trim target —
	// the shape where the trim has to choose between budget and a valid
	// request.
	stickyHeadReproWindow = 20000
	// stickyHeadGiantChars keeps that one tool result above the trigger
	// (est ≈ chars/4 = 20000 > 15000 = 75% of the window).
	stickyHeadGiantChars = 80000
	// stickyHeadDirectWindow is the smallest-sample window for the direct
	// call: the giant result alone is over the target.
	stickyHeadDirectWindow = 2000
)

// pinnedRoundFixture builds the pinned-window shape trimContext meets in a
// short task session: the epic at index 0 (never an anchor), the previous
// compaction's memory block at index 1, the last user instruction and its
// visible answer (the two anchor floors), and the CURRENT round's own tool
// result — the newest message, and the only message the trim can actually
// reach.
func pinnedRoundFixture(ag *Agent) {
	c := ag.Ctx
	c.AddUserMessage("EPIC TASK INPUT")
	c.AddAssistantMessage(memorySummaryMarker+"\n"+strings.Repeat("s", 300), nil)
	c.AddUserMessage("FINAL USER INSTRUCTION")
	c.AddAssistantMessage("final visible answer", nil)
	c.AddToolResult("call_giant", strings.Repeat("G", stickyHeadGiantChars))
}

// TestTrimToFitStickyHeadNeverEmptiesMessages is the minimal reproduction of
// the red test's failure, measured through the production entry point
// (Agent.trimContext) rather than by calling the trimmer directly: it fails at
// master tip with an emptied history, and passes once the sticky trim carries
// the same floor TrimToFit does.
func TestTrimToFitStickyHeadNeverEmptiesMessages(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, stickyHeadReproWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	// No keep-recent pinning: the deterministic trim must be free to bite on
	// the giant result, so a failure states the floor and not the tail pin.
	ag.CompactKeepRecent = 0
	pinnedRoundFixture(ag)
	if len(ag.Ctx.Messages) != 5 {
		t.Fatalf("fixture: want 5 messages, got %d", len(ag.Ctx.Messages))
	}

	ag.trimContext(context.Background())

	if len(ag.Ctx.Messages) == 0 {
		t.Fatalf("REGRESSION: trim emptied the history — the request serializes as messages:null and the endpoint answers HTTP 422; log:\n%s", buf.String())
	}
	if ag.Ctx.Messages[0].Role == "tool" {
		t.Fatalf("REGRESSION: trim left an orphaned tool result as the head; kept %v", rolesOf(ag.Ctx))
	}
	var last string
	for _, m := range ag.Ctx.Messages {
		last = m.Content
	}
	if !strings.Contains(last, "G") {
		t.Fatalf("the newest message (the round being served) must never be the victim; kept %v", rolesOf(ag.Ctx))
	}
	// The wire consequence, asserted on the real request body: this is the
	// failure the operator saw, so the guard is anchored to it and not to an
	// internal invariant.
	req := deepseek.BuildClaudeRequest("deepseek-flash", ag.Ctx.WireMessages(false), ag.Ctx.SystemPrompt, ag.Ctx.ProjectContext, nil, 16384, nil)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), `"messages":null`) {
		t.Fatalf("REGRESSION: the next request serializes as %s", body)
	}
	// A window that is pinned by anchor floors cannot be summarized into
	// convergence, so the round must not pay for a summarizer call either.
	if *calls != 0 {
		t.Fatalf("expected no summarizer call for a pinned window, got %d; log:\n%s", *calls, buf.String())
	}
}

// TestTrimToFitStickyHeadDirectNeverEmptiesMessages is the SMALLEST sample of
// the same contract (Rule 10.4): three messages, nothing to anchor, and one
// call to the trimmer on the hot path. The trim may stop above its target —
// the newest message is the round's own input — but it must stop with a
// history, not without one.
func TestTrimToFitStickyHeadDirectNeverEmptiesMessages(t *testing.T) {
	c := &Context{}
	c.AddUserMessage("u1")
	c.AddAssistantMessage("final visible answer", nil)
	c.AddToolResult("call_giant", strings.Repeat("G", stickyHeadDirectWindow*4))
	// The trim target is deliberately below what the newest message alone
	// costs: the context cannot be brought under it without deleting the
	// message the turn is working from.
	target := stickyHeadDirectWindow / 4

	removed := c.trimToFitStickyHead(stickyHeadDirectWindow, target)

	if len(c.Messages) == 0 {
		t.Fatalf("REGRESSION: trimToFitStickyHead emptied Messages (removed=%d) — an empty array has no valid request body", removed)
	}
	if c.Messages[0].Role == "tool" {
		t.Fatalf("REGRESSION: the trim left an orphaned tool result as the only head; kept %v", rolesOf(c))
	}
	if got := c.Messages[len(c.Messages)-1].Content; !strings.Contains(got, "G") {
		t.Fatalf("the newest message must never be the victim; kept %v", rolesOf(c))
	}
	t.Logf("kept %d message(s) %v (removed=%d, est %d > target %d) — over budget is the accepted outcome, empty is not",
		len(c.Messages), rolesOf(c), removed, c.EstimatedTokens(), target)
}
