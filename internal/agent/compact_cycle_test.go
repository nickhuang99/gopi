package agent

// compact_cycle_test.go — regression guard for the compaction LIVELOCK
// (2026-09-17 bug report: "memory compaction bug … when reaching memory limit
// which is running in a cycle").
//
// Observed field evidence (task attachment, session c2eff914 raw log):
//
//	🛡️ compaction anchored at last user — keeping est above target rather
//	   than losing the task anchor
//	🧠 memory compaction: summarized 1 oldest message(s) into a memory block
//	⚠️ est 157995 tokens (measured 0) > 150000 … compacted to est 118285 …
//	   assertion: after ≤ target+summary = false
//
// repeating every round with the SAME before/after numbers. The mechanism:
// the self-hosted session had the previous compaction's memory block sitting
// at index 1, so TakeOldest's anchor scan stopped the deletion at offset 1 —
// exactly ONE message was ever taken. compactContext then paid a summarizer
// call, prepended ANOTHER memory block and cleared the failure cooldown, so
// the next round repeated the whole cycle: an LLM call per round that can
// never converge, plus one more summary block each time.
//
// These tests pin the convergence property: once a compaction has run, the
// following rounds of the same session must NOT keep paying summarizer calls,
// and the context must not accumulate an unbounded run of memory blocks.

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
)

// compactCycleHistory builds the pinned-window shape from the field report:
// index 0 is a user message (the epic input — deliberately never an anchor),
// index 1 is the memory block left behind by an EARLIER compaction, and the
// rest is a large body of removable conversation followed by recent turns.
// The two anchors (last user, last visible assistant) both sit up front for
// the first user message, exactly as in a long self-hosted session where
// follow-up instructions arrive early.
func compactCycleHistory(ag *Agent, summarySize int) {
	c := ag.Ctx
	c.AddUserMessage("EPIC TASK INPUT — " + strings.Repeat("e", 4000))
	// Previous compaction's memory block, planted where compactContext
	// prepends it: index 1, an assistant message with visible content.
	c.AddAssistantMessage(memorySummaryMarker+"\n"+strings.Repeat("s", summarySize), nil)
	// The last user instruction of the session (anchor) followed by the last
	// visible assistant reply (anchor) — both near the head, mirroring the
	// field session where the user's follow-ups came early.
	c.AddUserMessage("FINAL USER INSTRUCTION")
	c.AddAssistantMessage("final visible answer", nil)
	// A large removable middle: the estimate must exceed the trigger.
	fillHistory(c, 20, 2000)
}

// TestCompact_NoLivelockAcrossRounds is the direct regression for the reported
// cycle: after one compaction the next rounds must not re-summarize forever.
//
// On master the middle is removable, so this test guards the *convergence*
// half (a second round must not keep costing a summarizer call once the
// context is under the trigger). The pinned-window half — where the anchors
// leave only one message in the window — is
// TestCompact_PinnedWindowBacksOffInsteadOfLooping.
func TestCompact_NoLivelockAcrossRounds(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 20000)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	compactCycleHistory(ag, 200)

	ag.trimContext(context.Background())
	if *calls != 1 {
		t.Fatalf("round 1: expected 1 summarizer call, got %d", *calls)
	}
	// The summary block heads the history and the oldest messages are gone.
	if head := ag.Ctx.Messages[0]; !strings.HasPrefix(head.Content, memorySummaryMarker) {
		t.Fatalf("round 1: expected memory block at head, got %q", head.Content)
	}
	if blocks := countMemoryBlocks(ag); blocks != 1 {
		t.Fatalf("round 1: expected exactly 1 live memory block after compaction, got %d", blocks)
	}
	// Rounds 2..4: the context is now under the trigger, so compaction must
	// be a no-op — no further summarizer calls (the cycle paid one per round).
	for round := 2; round <= 4; round++ {
		ag.trimContext(context.Background())
		if *calls != 1 {
			t.Fatalf("round %d: compaction re-fired after converging — %d summarizer calls total (livelock); log:\n%s",
				round, *calls, buf.String())
		}
	}
	if blocks := countMemoryBlocks(ag); blocks > 2 {
		t.Fatalf("memory blocks accumulated across rounds: %d (expected ≤2)", blocks)
	}
}

// TestCompact_PinnedWindowBacksOffInsteadOfLooping reproduces the exact field
// shape: everything the deletion loop may remove is pinned by the anchor
// floors, so TakeOldest can only ever yield ONE message. Paying an LLM call
// per round for that is the reported cycle; the fix treats such a window as a
// structural no-op (arm the existing backoff) and falls back to the bounded
// trim, so the round costs nothing and the context still cannot grow without
// bound.
func TestCompact_PinnedWindowBacksOffInsteadOfLooping(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 20000)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	// No keep-recent pinning: the deterministic trim must be free to bite on
	// the giant result, otherwise the window cannot be bounded at all and the
	// test would be measuring the tail floor, not the livelock guard.
	ag.CompactKeepRecent = 0

	// Pinned window: index 0 + the planted memory block at index 1 are the
	// only compressible material; the anchors (last user, last visible
	// assistant) sit at index 2/3 and hold the loop.
	c := ag.Ctx
	c.AddUserMessage("EPIC TASK INPUT")
	c.AddAssistantMessage(memorySummaryMarker+"\n"+strings.Repeat("s", 300), nil)
	c.AddUserMessage("FINAL USER INSTRUCTION")
	c.AddAssistantMessage("final visible answer", nil)
	c.AddToolResult("call_giant", strings.Repeat("G", 80000)) // keeps est > trigger

	ag.trimContext(context.Background())
	firstRoundCalls := *calls

	// The next rounds must NOT repeat the cycle, whatever the first round did.
	for round := 2; round <= 4; round++ {
		ag.trimContext(context.Background())
		if *calls > firstRoundCalls {
			t.Fatalf("round %d: summarizer re-fired for a window that cannot converge — %d calls total (livelock); log:\n%s",
				round, *calls, buf.String())
		}
	}
	// The window must not be left EMPTY, and the newest message (the round the
	// turn is actually working from) must survive. Bounding has a hard floor:
	// when the newest message ALONE exceeds the trigger, a valid over-budget
	// request is strictly better than an emptied — i.e. malformed — one. The
	// trim therefore stops at the last message instead of deleting it, and
	// this assertion states that floor rather than re-deriving a bound the
	// fixture cannot reach.
	//
	// This is the corrected expectation (2026-09-30): the earlier form asserted
	// "est ≤ trigger" and passed only because TrimToFit's two-step loop deleted
	// the whole array — the defect that produced `"messages":null` and the
	// endpoint's HTTP 422. A test that green-lights a bug is not a guard.
	if len(ag.Ctx.Messages) == 0 {
		t.Fatalf("trim emptied the history — the request would serialize as messages:null (HTTP 422)")
	}
	if ag.Ctx.Messages[0].Role == "tool" {
		t.Fatalf("trim left an orphaned tool result at the head; kept %v", rolesOf(ag.Ctx))
	}
	var last string
	for _, m := range ag.Ctx.Messages {
		last = m.Content
	}
	if !strings.Contains(last, "G") {
		t.Fatalf("the newest message (the round the turn is working from) was trimmed away; kept %v", rolesOf(ag.Ctx))
	}
	// The deterministic trim is what bounds the window when compaction is
	// refused (TrimToFit has no anchor floors — that is pre-existing
	// behaviour, and it is why the guard must NOT let the cycle roll on: the
	// trim is cheap, the summarizer round-trip is not).
	var tail string
	for _, m := range ag.Ctx.Messages {
		tail += m.Content + "\n"
	}
	if strings.Count(tail, memorySummaryMarker) > 1 {
		t.Fatalf("memory blocks accumulated while compaction was refused: %s", tail)
	}
}

// TestCompact_SavingsVerdictUsesSameBasis pins the anti-thrash verdict's
// measurement basis. compactContext resets the API measurement while mutating
// the context, so a verdict that compares the post-reset chars/4 heuristic
// against the pre-compaction measured count reports phantom savings for a
// compression that changed almost nothing — and the breaker can never latch
// (the field log: 119145 measured → 118285 heuristic "saved 0.7%").
//
// The invariant asserted here: the recorded savings must equal the
// compression's real change measured on ONE basis — the heuristics-only
// estimate recomputed over the same message sets.
func TestCompact_SavingsVerdictUsesSameBasis(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	ag.CompactKeepRecent = 0
	fillHistory(ag.Ctx, 6, 500)
	ag.Ctx.AddUserMessage("final task")
	ag.Ctx.AddAssistantMessage("final answer", nil)
	// Real sessions carry an API-measured prompt count LARGER than the
	// chars/4 heuristic (the field log only reads "measured 0" right after a
	// reset). Comparing across that reset is what made the phantom savings.
	ag.Ctx.SetPromptTokens(heuristicOf(ag.Ctx) * 3)

	hBefore := heuristicOf(ag.Ctx)
	ag.compactContext(context.Background())
	if *calls != 1 {
		t.Fatalf("expected 1 summarizer call, got %d", *calls)
	}
	hAfter := heuristicOf(ag.Ctx)
	if hAfter >= hBefore {
		t.Fatalf("test setup: expected the compression to shrink the heuristic estimate (%d → %d)", hBefore, hAfter)
	}
	want := (float64(hBefore) - float64(hAfter)) / float64(hBefore) * 100
	if diff := ag.lastCompactSavingsPct - want; diff > savingsBasisTolerancePct || diff < -savingsBasisTolerancePct {
		t.Fatalf("savings verdict computed on a mixed basis: recorded %.2f%% vs the real single-basis change %.2f%% (phantom savings — the breaker can never latch); before(measured+heuristic)=%d after(heuristic)=%d",
			ag.lastCompactSavingsPct, want, hBefore, hAfter)
	}
	if ag.compactIneffectiveCount != 0 {
		t.Fatalf("an effective compression must not strike the breaker, got count %d (log:\n%s)", ag.compactIneffectiveCount, buf.String())
	}
}

// TestCompact_LargeSliverWindowStillConverges covers the ONE shape the
// caller-side livelock guard does NOT catch, and which therefore has to be
// handled inside TakeOldest: an anchor floor pins the window to a single
// message that is ITSELF LARGE (removing it frees well over
// compactEffectiveSavingsPct — deleting the giant tool result that sits
// directly behind the epic input).
//
// That is the shape of the field log's own numbers (one message summarized,
// 157995 → 118285 — a 25% reduction, i.e. an "effective" compaction by the
// anti-thrash definition), so the guard's "< savings" test reads FALSE and the
// summarizer runs legitimately. What must not happen is the NEXT round
// deleting the memory block the first round just wrote: summarizing a summary
// frees nothing, costs an LLM call, and is the same fixed point one level up.
//
// Before the windowIsMemoryBlocks bound was corrected to the FIRST stop floor,
// this test's round 2 summarized the just-written memory block and round 3 did
// it again — the defect the probe surfaced.
func TestCompact_LargeSliverWindowStillConverges(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, fieldCycleWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	ag.CompactKeepRecent = 0

	c := ag.Ctx
	c.AddUserMessage("EPIC TASK INPUT")
	c.AddToolResult("call_giant", strings.Repeat("G", 400000)) // the large sliver
	c.AddUserMessage("FINAL USER INSTRUCTION")
	c.AddAssistantMessage("final visible answer", nil)
	c.AddToolResult("call_tail", strings.Repeat("T", 20000))

	// Round 1 is a legitimate, effective compaction of the giant result.
	c.SetPromptTokens(fieldCycleMeasured)
	ag.compactStructuralNoOpUntil = time.Time{}
	ag.trimContext(context.Background())
	round1Calls := *calls
	if round1Calls != 1 {
		t.Fatalf("round 1: the giant result should be summarized once, got %d calls; log:\n%s", round1Calls, buf.String())
	}
	// The next rounds must not keep paying for a window that holds only the
	// block round 1 wrote. Bounded: the total call count may rise by at most
	// a small constant across many rounds (the trim fallback can re-expose a
	// message), but it must NOT grow once per round.
	prev := round1Calls
	for round := 2; round <= 20; round++ {
		c.SetPromptTokens(fieldCycleMeasured)
		ag.compactStructuralNoOpUntil = time.Time{}
		ag.trimContext(context.Background())
		if *calls-prev > 1 {
			t.Fatalf("round %d: summarizer calls grew by %d in ONE round (total %d) — a memory block is being re-summarized; log:\n%s",
				round, *calls-prev, *calls, buf.String())
		}
		if *calls > round1Calls+5 {
			t.Fatalf("round %d: %d summarizer calls over 20 rounds (round 1 took %d) — the cycle is still paying per round; log:\n%s",
				round, *calls, round1Calls, buf.String())
		}
		prev = *calls
	}
}

// savingsBasisTolerancePct is the rounding slack allowed when comparing the
// recorded savings percentage against the recomputed single-basis change.
const savingsBasisTolerancePct = 0.01

// countMemoryBlocks counts the live memory-summary blocks in the history.
func countMemoryBlocks(ag *Agent) int {
	n := 0
	for _, m := range ag.Ctx.Messages {
		if isMemorySummaryMessage(m) {
			n++
		}
	}
	return n
}

// heuristicOf computes the chars/4 estimate of a context locally, so this
// test file builds against the PRE-FIX sources too (the red proof runs it on
// master, where Context.heuristicTokens does not exist yet).
func heuristicOf(c *Context) int {
	total := 0
	if c.SystemPrompt != "" {
		total += len(c.SystemPrompt)/4 + 1
	}
	if c.ProjectContext != "" {
		total += len(c.ProjectContext)/4 + 1
	}
	for _, m := range c.Messages {
		total += EstimateTokens(m)
	}
	return total
}

// fieldCycleWindow is the self-hosted session's context window (window=200000,
// trigger 75% = 150000, target 35% = 70000 — the ratios in the field log).
const fieldCycleWindow = 200000

// The field session's own numbers (window=200000, limit 75% = 150000).
const (
	fieldCycleLimit        = 150000
	fieldCycleMeasured     = 157995 // the measured prompt count in the log
	fieldCycleBulkMessages = 250    // removable tool-result bulk
	fieldCycleBulkChars    = 2000   // per bulk result, measured below
)

// TestCompact_FieldNumbersStopLooping replays the reported session's numbers
// VERBATIM — measured on the attachment, not invented. The log's three
// before/after pairs (157995 → 118285, 158347 → 118616, 158844 → 119145,
// "assertion: after ≤ target+summary = false", "summarized 1 oldest message
// (s)") alternate ~2000× each, i.e. the same fixed point forever.
//
// The shape is the one the field transcript actually has (read off session
// c2eff914: 493 assistant messages, 284 tool_results, and only 7 user
// messages across 219 API requests — this is a LONG autonomous run, not a
// chat): ONE epic input, the previous compaction's memory block at index 1,
// then the session's last user turn and its visible answer (the two anchor
// floors the log names), and the bulk as TOOL RESULTS — which is what makes
// it removable: a tool message is neither anchor, so the deletion loop that
// stopped at the anchors does have compressible material behind it.
//
// The measured basis is restored with SetPromptTokens (157995 — the log's
// own number) so the trigger fires on measured size exactly as in the field.
//
// MASTER (red): the loop takes the bulk in one 250-message window, the
// summarizer runs once, and the NEXT round — the bulk is gone, the block sits
// behind the anchors, and the estimate is still 150355 > 150000 — is the
// reported fixed point: "anchored at last user" / "summarized 1 oldest
// message(s)" / "after ≤ target+summary = false", forever, one LLM call per
// round. The fix recognizes that shape (≤ maxCompactSliverMessages taken,
// stopped by an anchor floor, still above the trigger) as a structural no-op,
// puts the sliver back and lets the deterministic trim bound the context.
func TestCompact_FieldNumbersStopLooping(t *testing.T) {
	ag, calls := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, fieldCycleWindow)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	ag.CompactKeepRecent = 0

	c := ag.Ctx
	c.AddUserMessage("EPIC TASK INPUT")
	c.AddAssistantMessage(memorySummaryMarker+"\n"+strings.Repeat("s", 1000), nil)
	c.AddUserMessage("FINAL USER INSTRUCTION")
	c.AddAssistantMessage("final visible answer", nil)
	for i := 0; i < fieldCycleBulkMessages; i++ {
		c.AddToolResult(fmt.Sprintf("call_bulk_%03d", i), strings.Repeat("b", fieldCycleBulkChars))
	}
	// A real round is re-measured at the API before trimContext runs
	// (loop.go: SetPromptTokens on message_delta). Without that, the
	// post-compaction `measured 0` readback makes the NEXT round judge the
	// heuristic — and the cycle VANISHES, because the heuristic now sits
	// under the trigger. The field log shows the opposite: every round read
	// "est 157995 (measured 0)". So each simulated round re-measures, which
	// is what makes the field's fixed point reproducible.
	c.SetPromptTokens(fieldCycleMeasured)
	ag.compactStructuralNoOpUntil = time.Time{}
	ag.trimContext(context.Background())
	if *calls > 1 {
		t.Fatalf("round 1: expected the summarizer to run once for the field window, got %d", *calls)
	}
	h1 := heuristicOf(ag.Ctx)
	// Rounds 2..5 are the reported cycle: on master each one re-logs
	// "anchored at last user" + "summarized 1 oldest message(s)" + a failed
	// assertion (149 / 2268 / 7031 occurrences in the attachment).
	for round := 2; round <= 5; round++ {
		c.SetPromptTokens(fieldCycleMeasured)
		ag.compactStructuralNoOpUntil = time.Time{} // the backoff is a delay, not the guard
		ag.trimContext(context.Background())
		if *calls > 1 {
			t.Fatalf("round %d: the summarizer re-fired for a window that cannot converge (%d calls, round 1 had 1) — the reported cycle; log:\n%s",
				round, *calls, buf.String())
		}
		if got := heuristicOf(ag.Ctx); got > h1 {
			// The field's before-values ROSE (157995 → 158347 → 158844)
			// while each round claimed to compact — the accumulation is the
			// second half of the defect. Post-fix the rounds must not grow
			// the transcript (the heuristic basis is the one that reacts to
			// message changes; the measured count is re-injected above, so
			// it cannot be used as a growth probe here).
			t.Fatalf("round %d: context GREW across the cycle rounds (heuristic %d > %d) — compaction is accumulating, not bounding", round, got, h1)
		}
	}
	if blocks := countMemoryBlocks(ag); blocks > 2 {
		t.Fatalf("memory blocks accumulated during the cycle-following rounds: %d", blocks)
	}
}
