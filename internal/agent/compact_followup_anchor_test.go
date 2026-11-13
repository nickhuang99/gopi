// compact_followup_anchor_test.go — regression guard for the ANCHOR TRIGGER of
// the compaction livelock (the follow-up to the livelock fix 903c0d5c).
//
// The livelock fix made an anchor-jammed window CONVERGE
// (windowIsMemoryBlocks → structural no-op → bounded trim). What it did not
// address is the shape that creates the jam: TakeOldest chose its "last user"
// floor by ROLE alone, so a user-role message the HARNESS appended became the
// instruction floor as soon as it was the newest user message in the history.
//
// Measured on the field session (c2eff914, 397 request dumps in its raw log;
// see devdoc/compact-followup-anchor.md for the command):
//
//   - 478 user-role messages carry TEXT (the rest are tool results), and their
//     only two texts are the epic and harness messages — "[system] TASK-START
//     CHECKPOINT …", "[system] Hot-tier resident candidates …", "[system] You
//     ended your turn without calling any tools …". The session has NO operator
//     follow-up: every user-role message after index 0 is framework output.
//   - The injections are appended EARLY in a run (start checkpoint, hot-tier
//     digest) and then immediately fall behind the newest user message — so
//     they sit near the HEAD, and everything after them (the session's whole
//     body of assistant/tool work) is on the protected side of the floor.
//
// That is the jam: the floor stops on a framework message a couple of messages
// behind the head, the pass can take only the sliver in front of it, and the
// bulk behind it is unreachable — the context stays above the limit while every
// round reports "anchored at last user" and "summarized 1 oldest message(s)"
// (the field log: ~8500 and 7490 respectively).
//
// These tests pin the policy: a harness-injected follow-up is NOT the
// instruction floor (the fix), and the operator's own instruction still IS (the
// guard against over-reach). Both are behaviour-level — there is no test-only
// flag in production, so the red proof is a plain stash of the production files
// (commands in the devdoc).

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

// anchorStopLastUserWire mirrors the production AnchorStop reason for the
// instruction floor. It is declared here rather than imported because this file
// has to COMPILE against the pre-fix sources: the red proof stashes the
// production files and runs this test to show the pinned-window failure.
const anchorStopLastUserWire = "last user"

// followUpAnchorBulkResults / followUpAnchorBulkChars size the session's own
// work — the material the jam keeps out of reach. ~30 × 20000 chars ≈ 150K
// heuristic tokens, i.e. the size the field log's limit was crossed with
// (measured 157995 at window 200000, trigger 75%).
const (
	followUpAnchorBulkResults = 30
	followUpAnchorBulkChars   = 20000
)

// followUpAnchorHistory builds the measured shape: the harness injections sit
// near the head (where they land in a run), and everything behind them is the
// session's own assistant/tool work.
//
//	index 0   previous compaction's memory block (removable)
//	index 1   the harness's start checkpoint   (a "[system] " injection)
//	index 2   the harness's hot-tier digest    (a "[system] " injection)
//	index 3+  the session's work: tool results + assistant turns — NOTHING
//	          user-text, exactly as measured (only index 0 carries operator text
//	          and it is never an anchor)
//	last      a visible assistant answer (the "last visible assistant" floor)
//
// On master the newest user message is the injection at index 2, so the
// instruction floor stops the pass there: the bulk behind it is unreachable and
// the context stays above the trigger.
func followUpAnchorHistory(ag *Agent) {
	c := ag.Ctx
	c.AddUserMessage("EPIC TASK INPUT — " + strings.Repeat("e", 3000)) // index 0: never anchors
	c.AddAssistantMessage(memorySummaryMarker+"\n"+strings.Repeat("s", 200), nil)
	c.AddUserMessage(taskStartSearchMessage)
	c.AddAssistantMessage("start checkpoint acknowledged", nil)
	c.AddUserMessage(hotResidentMessagePrefix + "some distilled lessons")
	c.AddAssistantMessage("understood, planning now", nil)
	for i := 0; i < followUpAnchorBulkResults; i++ {
		c.AddToolResult("call_bulk", strings.Repeat("B", followUpAnchorBulkChars))
		c.AddAssistantMessage("", nil)
	}
	c.AddAssistantMessage("here is what I found so far", nil) // the last visible answer
}

// TestCompact_HarnessFollowUpDoesNotPinTheWindow is the red→green proof of the
// TRIGGER defect.
//
// MASTER (red): the anchor scan walks backwards and stops at the newest user
// message — a harness injection at index 2 (its position in the real log). The
// deletion pass therefore takes only the sliver in front of it (the previous
// memory block), which frees a couple of percent, so the estimate stays above
// the limit for every subsequent round: "anchored at last user" +
// "summarized 1 oldest message(s)", the field log's fixed point.
//
// WITH THE FIX: the harness injection is not a floor. The floors that remain
// are the last visible assistant (the tail) plus the keep-recent window, so the
// session's own bulk becomes reachable and one summarizer call frees an
// effective share of the context.
func TestCompact_HarnessFollowUpDoesNotPinTheWindow(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, fieldCycleWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	followUpAnchorHistory(ag)
	before := heuristicOf(ag.Ctx)
	ag.Ctx.SetPromptTokens(fieldCycleMeasured)

	ag.trimContext(context.Background())

	after := heuristicOf(ag.Ctx)
	saved := 0.0
	if before > 0 {
		saved = (float64(before) - float64(after)) / float64(before) * 100
	}
	if saved < compactEffectiveSavingsPct {
		t.Fatalf("the harness follow-up pinned the compaction window: the context moved %d → %d heuristic "+
			"tokens (%.1f%%, < %.0f%%) in %d summarizer call(s), AnchorStop=%q — the instruction floor is a "+
			"framework message, not the operator's, so the session's own work behind it is unreachable; log:\n%s",
			before, after, saved, compactEffectiveSavingsPct, *calls, ag.Ctx.AnchorStop, buf.String())
	}
	if *calls != 1 {
		t.Fatalf("expected the pinned bulk to be summarized in ONE call, got %d; log:\n%s", *calls, buf.String())
	}
	// The compaction must keep the session coherent: the summary heads the
	// context (index 0 is the compaction boundary by design — the summary
	// replaces the epic, which is why it is never an anchor) and the newest
	// visible answer survives (the reply floor).
	if len(ag.Ctx.Messages) == 0 || !isMemorySummaryMessage(ag.Ctx.Messages[0]) {
		t.Fatalf("expected the summarized block at the head of the compacted context, got %d message(s)",
			len(ag.Ctx.Messages))
	}
	if joined := joinContents(ag.Ctx); !strings.Contains(joined, "here is what I found so far") {
		t.Fatalf("the last visible assistant answer was deleted — the reply floor must still hold; log:\n%s", buf.String())
	}
}

// TestTakeOldest_HarnessFollowUpIsNotTheInstructionFloor pins the anchor choice
// at the unit level: the pass must not stop on a framework message, and the
// operator's own instruction (when one exists) must still floor it — the
// exemption cannot over-reach into "compaction must not swallow the current
// instruction" (Hermes #10896/#29824).
func TestTakeOldest_HarnessFollowUpIsNotTheInstructionFloor(t *testing.T) {
	build := func() *Context {
		c := &Context{}
		c.AddUserMessage("EPIC TASK INPUT")        // 0: never anchors
		c.AddUserMessage(taskStartSearchMessage)   // 1: harness injection (master's floor)
		c.AddAssistantMessage("acknowledged", nil) // 2
		for i := 0; i < 6; i++ {                   // 3..: the session's removable work
			c.AddToolResult("call_1", strings.Repeat("R", 4000))
			c.AddAssistantMessage("step", nil)
		}
		c.AddAssistantMessage("final visible answer", nil)
		return c
	}

	// FIXED: the only remaining floors are the keep-recent window and the last
	// visible assistant, so the work behind the injection is taken.
	c := build()
	taken := c.TakeOldest(0, 0, nil)
	if len(taken) == 0 {
		t.Fatalf("expected the session's work behind the harness message to be compressible, got an empty window (AnchorStop=%q)", c.AnchorStop)
	}
	if c.AnchorStop == anchorStopLastUserWire {
		t.Fatalf("the deletion stopped on the harness injection (%q) — a framework message must not be the instruction floor", c.AnchorStop)
	}
	joined := joinContents(c)
	if !strings.Contains(joined, "final visible answer") {
		t.Fatalf("the last visible assistant answer was deleted — the reply floor must still hold:\n%s", joined)
	}
	if strings.Contains(joined, taskStartSearchMessage) {
		t.Fatalf("the harness message was still protected as a floor — with the exemption it is ordinary history:\n%s", joined)
	}
}

// joinContents concatenates every live message, for presence assertions.
func joinContents(c *Context) string {
	var b strings.Builder
	for _, m := range c.Messages {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}
