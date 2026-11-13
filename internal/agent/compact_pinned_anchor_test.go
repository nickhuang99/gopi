// compact_pinned_anchor_test.go — option (c) of the anchor-trigger fix: a
// context that compaction can no longer reach must be OPERATOR-VISIBLE.
//
// The anchor policy (compact_followup_anchor_test.go) stops a framework
// follow-up from becoming the instruction floor. What it cannot do is create a
// window where none exists: once the operator's own instruction (or the last
// visible answer) sits at the head, the deletion pass has nothing left to take
// and the context stays above the compaction target for the rest of the run.
// Every round refuses silently, and the only trace is a per-round
// "anchored at …" line that reads like normal operation.
//
// This file pins the visibility contract: ONE loud warning per session, naming
// the pinned anchor, emitted the first time the pin is observed.

package agent

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

// pinnedAnchorTailCap bounds how many tail results the fixture may append while
// lifting the estimate above the compaction target. Each result is capped at
// ingestion (ToolResultTokenCap), so a handful is always enough; the bound
// exists so a fixture that can no longer reach the target FAILS instead of
// looping (Rule 15 — a silent no-op is the defect this whole test is about).
const pinnedAnchorTailCap = 16

// compactTargetFor restates the compaction target the production path computes
// in compactContext, so a fixture can state its own precondition ("above the
// target") instead of hard-coding a number that drifts when the window or the
// ratio moves.
func compactTargetFor(ag *Agent) int {
	ratio := ag.CompactTargetRatio
	if ratio <= 0 {
		ratio = DefaultCompactTargetRatio
	}
	return int(float64(ag.ContextWindow) * ratio)
}

// pinnedAnchorHistory builds the state the warning is about: the operator's own
// instruction sits at the head (index 1, directly behind a small removable
// message), so the instruction floor stops the deletion after ONE message while
// the context is far above the target. Nothing here is a harness message — this
// is the residue of the trigger defect, reported rather than silently absorbed.
//
// THE TAIL IS SIZED FROM THE CONTRACT, NOT FROM A REPEAT COUNT (regression
// fixed here). The fixture used to append ONE fixed 400,000-char tool result
// and relied on that being big enough. It no longer is: AddToolResult caps a
// result at ingestion (ToolResultTokenCap = 32768; commit b941fcad), so those
// 400,000 characters arrive as 59,205 chars / 14,803 estimated tokens — the
// whole history then estimates 14,900 against a 70,000 target, TakeOldest
// never runs its deletion loop, AnchorStop stays empty, and the warning is
// never reached. The test then reported "0 warnings", which reads exactly like
// a dead warning path while the path was in fact intact. Growing the tail until
// the estimate is above the target keeps the fixture true no matter how much of
// any single result survives ingestion; the test's own precondition assertion
// fails loudly if that ever stops holding.
func pinnedAnchorHistory(t *testing.T, ag *Agent) {
	t.Helper()
	c := ag.Ctx
	c.AddAssistantMessage("older answer "+strings.Repeat("o", 300), nil) // 0: the only thing the floor allows
	c.AddUserMessage("REAL OPERATOR INSTRUCTION")                        // 1: THE pinned floor
	c.AddAssistantMessage("working on it", nil)                          // 2: visible answer
	target := compactTargetFor(ag)
	for i := 0; c.EstimatedTokens() <= target; i++ {
		if i >= pinnedAnchorTailCap {
			t.Fatalf("fixture cannot reach the compaction target: est %d still ≤ target %d after %d tail results — the pinned state under test does not exist",
				c.EstimatedTokens(), target, pinnedAnchorTailCap)
		}
		c.AddToolResult("call_tail", strings.Repeat("T", 400000))
	}
}

// TestCompact_PinnedAnchorIsOperatorVisible asserts the once-per-session
// warning: repeated rounds of the same pinned state produce exactly ONE
// warning naming the anchor, and the pinned state costs no summarizer call
// (refusing is correct; refusing silently is not).
func TestCompact_PinnedAnchorIsOperatorVisible(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, fieldCycleWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	// keepRecent would protect the whole (4-message) fixture by itself; the
	// pinned state under test is the ANCHOR floor, so it is removed from the
	// equation exactly as the other compaction tests do.
	ag.CompactKeepRecent = 0
	pinnedAnchorHistory(t, ag)

	// NON-VACUITY (the regression this test shipped without): the warning is
	// only reachable while the context is ABOVE the compaction target — below
	// it TakeOldest is never asked to delete anything, so an empty log proves
	// nothing about the warning path. Assert the precondition instead of
	// trusting the fixture, so a fixture silently shrunk by a production change
	// reports ITSELF rather than a "0 warnings" failure that reads like a dead
	// feature.
	if est := ag.Ctx.EstimatedTokens(); est <= compactTargetFor(ag) {
		t.Fatalf("fixture is VACUOUS: context est %d is not above the compaction target %d, so no pinned state exists and a missing warning would prove nothing",
			est, compactTargetFor(ag))
	}

	for round := 0; round < 3; round++ {
		ag.compactContext(context.Background())
	}
	if *calls != 0 {
		t.Fatalf("a pinned window must not pay summarizer calls, got %d; log:\n%s", *calls, buf.String())
	}
	if n := strings.Count(buf.String(), compactPinnedAnchorWarning); n != 1 {
		t.Fatalf("expected exactly ONE pinned-anchor warning per session, got %d:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), anchorStopLastUser) {
		t.Fatalf("the warning must NAME the pinned anchor (%q):\n%s", anchorStopLastUser, buf.String())
	}
	// The warning is a report, not a mutation: the operator's instruction and
	// the answer behind it must be intact.
	joined := joinContents(ag.Ctx)
	if !strings.Contains(joined, "REAL OPERATOR INSTRUCTION") || !strings.Contains(joined, "working on it") {
		t.Fatalf("the pinned-anchor warning path modified the history:\n%s", joined)
	}
}
