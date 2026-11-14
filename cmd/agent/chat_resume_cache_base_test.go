package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

// chat_resume_cache_base_test.go — the regression guard for the Σ reading's
// SCOPE (task 01a198e4): the chat status row's cumulative cache hit rate was
// computed from ag.Cost.TotalCacheHitTokens / TotalCacheMissTokens, a RUNTIME
// counter that starts at zero in every process. A `gopi chat --resume <id>`
// therefore reported Σ over THIS RUN while the marker says "sum over the
// session" — measured by the operator on one session as 81.5% on the row against
// 86.03% in `gopi session stats`.
//
// THREE FACTS ARE PINNED, and the third is the RED proof that makes the other two
// mean something:
//
//	(a) the base a resumed run folds IS the session's counted total — the same
//	    two numbers gopilog's COST blocks print (SessionPromptCacheHit/Miss);
//	(b) the row's read-out ADDS it, so Σ covers the whole session, while the
//	    Δ reading (a difference of two readings) is exactly as it was;
//	(c) WITHOUT the base the read-out falls back to this run alone — the defect
//	    — so a change that drops the base turns (b) red rather than passing
//	    vacuously.
//
// The fixture is a REAL resumed log's shape (two `session_start` segments whose
// CostTracker running totals restart at the second one), because the fold's
// segment handling is exactly what a single-segment fixture cannot exercise.

// chatCacheBaseFixtureHit/Miss are the two segments' LAST readings — the numbers
// a resumed chat log holds, and the ones the fold must sum. Segment 1 climbs from
// a low opening reading (a fresh CostTracker) to its last; segment 2 restarts
// BELOW it, which is the evidence internal/requestusage uses to split segments.
const (
	chatCacheBaseSeg1FirstHit   = 20000
	chatCacheBaseSeg1FirstMiss  = 10000
	chatCacheBaseSeg1LastHit    = 70000
	chatCacheBaseSeg1LastMiss   = 30000
	chatCacheBaseSeg2FirstHit   = 300
	chatCacheBaseSeg2FirstMiss  = 200
	chatCacheBaseSeg2LastHit    = 40000
	chatCacheBaseSeg2LastMiss   = 20000
	chatCacheBaseWantHit        = chatCacheBaseSeg1LastHit + chatCacheBaseSeg2LastHit
	chatCacheBaseWantMiss       = chatCacheBaseSeg1LastMiss + chatCacheBaseSeg2LastMiss
	chatCacheBaseThisRunHit     = 1000
	chatCacheBaseThisRunMiss    = 500
	chatCacheBaseFixtureSessID  = "ca5e0000-1111-4222-8333-444455556666"
	chatCacheBaseRowTemplate    = `{"type":"request","iteration":0,"prompt_tokens":%d,"cache_hit":%d,"cache_miss":%d,"completion_tokens":7,"tagged_usage":true,"usage_base":"cumulative"}`
	chatCacheBaseEndTemplate    = `{"type":"session_end","exit_code":0,"total_iterations":1,"total_tokens":0,"elapsed_sec":1}`
	chatCacheBaseStartTemplate  = `{"type":"session_start","sessionId":"` + chatCacheBaseFixtureSessID + `","timestamp":"2026-11-01T00:00:0%dZ"}`
	chatCacheBaseAssistantLine  = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}`
	chatCacheBaseUserLine       = `{"type":"user","message":{"role":"user","content":"hi"}}`
	chatCacheBaseFileForFixture = "session.jsonl"
)

// chatCacheBaseFixture writes a two-segment chat session record and returns its
// path. The rows are the chat path's own quantity (`usage_base: cumulative` and
// the protocol's prompt == hit + miss split), so the fixture is parsed by the SAME
// fold production uses rather than by a shape this test invented.
func chatCacheBaseFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	row := func(hit, miss int) string {
		return fmt.Sprintf(chatCacheBaseRowTemplate, hit+miss, hit, miss)
	}
	lines := []string{
		fmt.Sprintf(chatCacheBaseStartTemplate, 1),
		chatCacheBaseUserLine,
		chatCacheBaseAssistantLine,
		row(chatCacheBaseSeg1FirstHit, chatCacheBaseSeg1FirstMiss),
		chatCacheBaseAssistantLine,
		row(chatCacheBaseSeg1LastHit, chatCacheBaseSeg1LastMiss),
		chatCacheBaseEndTemplate,
		// THE RESUME: a second run segment, whose counters restart BELOW the
		// previous segment's last reading — the detail a fold that summed the
		// column (or kept only the last reading) gets wrong.
		fmt.Sprintf(chatCacheBaseStartTemplate, 2),
		chatCacheBaseUserLine,
		chatCacheBaseAssistantLine,
		row(chatCacheBaseSeg2FirstHit, chatCacheBaseSeg2FirstMiss),
		chatCacheBaseAssistantLine,
		row(chatCacheBaseSeg2LastHit, chatCacheBaseSeg2LastMiss),
	}
	path := filepath.Join(dir, chatCacheBaseFileForFixture)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write the session fixture: %v", err)
	}
	return path
}

// TestChatResumeCacheBaseIsTheSessionsCountedTotal: the base is the SESSION's
// cache total, not the last run's, and it is READ from gopilog's own fold (the
// numbers `gopi session stats` prints) rather than added up here.
func TestChatResumeCacheBaseIsTheSessionsCountedTotal(t *testing.T) {
	path := chatCacheBaseFixture(t)

	base, err := chatResumeCacheBase(path, "")
	if err != nil {
		t.Fatalf("chatResumeCacheBase(%q): %v", path, err)
	}
	if !base.known {
		t.Fatal("base.known = false for a session record that exists: a measured zero and an unmeasured absence are different facts (Rule 15a)")
	}
	if base.cacheRead != chatCacheBaseWantHit || base.cacheWrite != chatCacheBaseWantMiss {
		t.Fatalf("base = %d hit / %d miss, want the session's counted %d / %d (each run segment's LAST reading, summed) — the row's Σ would print a different session total than `gopi session stats`",
			base.cacheRead, base.cacheWrite, chatCacheBaseWantHit, chatCacheBaseWantMiss)
	}
}

// TestStatusRowCumulativeReadingCoversTheWholeSession is the acceptance criterion
// itself: the Σ reading a RESUMED run prints must agree with the session's counted
// cache totals (`gopi session stats`'s two numbers), and the Δ reading must be
// unchanged by the base.
func TestStatusRowCumulativeReadingCoversTheWholeSession(t *testing.T) {
	path := chatCacheBaseFixture(t)
	base, err := chatResumeCacheBase(path, "")
	if err != nil {
		t.Fatalf("chatResumeCacheBase: %v", err)
	}

	// This run's own counters, as a live CostTracker would hold them.
	ag := &agent.Agent{Cost: &deepseek.CostTracker{
		TotalCacheHitTokens:  chatCacheBaseThisRunHit,
		TotalCacheMissTokens: chatCacheBaseThisRunMiss,
	}}
	start := time.Now()

	withBase := statusOpsFor(ag, start, base)
	if want := chatCacheBaseWantHit + chatCacheBaseThisRunHit; withBase.cacheRead != want {
		t.Fatalf("row cacheRead = %d, want %d (the session's %d it carried + %d this run): the Σ reading is reporting THIS RUN while its marker says the session",
			withBase.cacheRead, want, chatCacheBaseWantHit, chatCacheBaseThisRunHit)
	}
	if want := chatCacheBaseWantMiss + chatCacheBaseThisRunMiss; withBase.cacheWrite != want {
		t.Fatalf("row cacheWrite = %d, want %d (the session's %d + %d this run)",
			withBase.cacheWrite, want, chatCacheBaseWantMiss, chatCacheBaseThisRunMiss)
	}

	// THE ACCEPTANCE IN THE ROW'S OWN TERMS: the drawn Σ percentage must equal the
	// rate over the WHOLE SESSION'S counters — the session's counted total PLUS the
	// tokens this run has since sent, which is exactly what `gopi session stats`
	// reports once the run's rows are in the log (the acceptance criterion allows
	// that difference; what it does not allow is this run ALONE, asserted below).
	wantPct := statusHitRatePct(float64(chatCacheBaseWantHit+chatCacheBaseThisRunHit) /
		float64(chatCacheBaseWantHit+chatCacheBaseThisRunHit+chatCacheBaseWantMiss+chatCacheBaseThisRunMiss))
	if got := statusCacheHitRate(withBase); got != wantPct {
		t.Fatalf("Σ reading = %q, want %q (the whole session's hit/(hit+miss)) — this is the 4.5-point gap the fix removes",
			got, wantPct)
	}

	// RED PROOF (c): without the base the reading collapses to this run — the
	// defect — so the assertions above cannot pass by accident if the base stops
	// being applied.
	withoutBase := statusOpsFor(ag, start, chatCostBase{})
	if withoutBase.cacheRead == withBase.cacheRead {
		t.Fatalf("the read-out is identical with and without the base (%d): the base is not reaching the row, "+
			"so this test proves nothing", withBase.cacheRead)
	}
	if got, want := withoutBase.cacheRead, chatCacheBaseThisRunHit; got != want {
		t.Fatalf("without the base the read-out = %d, want this run's %d — the red side of the proof must be the DEFECT's own reading",
			got, want)
	}

	// Δ IS NOT TOUCHED: it is a difference of two readings from this same function,
	// so a constant base cancels out of it exactly. A base that leaked into the
	// turn reading would inflate the delta on the first turn of every resume.
	turnOps := withBase
	turnOps.cacheReadAtTurn, turnOps.cacheWriteAtTurn = base.cacheRead, base.cacheWrite
	turnOps.cacheTurnKnown = true
	wantTurn := statusHitRatePctWhole(float64(chatCacheBaseThisRunHit) / float64(chatCacheBaseThisRunHit+chatCacheBaseThisRunMiss))
	if got := statusCacheTurnHitRate(turnOps); got != wantTurn {
		t.Fatalf("Δ reading = %q, want %q (this run's own tokens): the session base must not enter the turn reading",
			got, wantTurn)
	}
}

// TestChatResumeCacheBaseZeroForAnUnresolvableRecord: no record is not a failure,
// and it is not a measurement either — known stays false so the read-out is
// unchanged rather than claiming a measured zero.
func TestChatResumeCacheBaseZeroForAnUnresolvableRecord(t *testing.T) {
	base, err := chatResumeCacheBase(filepath.Join(t.TempDir(), "absent.jsonl"), "")
	if err != nil {
		t.Fatalf("chatResumeCacheBase on a missing record must not fail (the conversation still resumes): %v", err)
	}
	if base.known || base.cacheRead != 0 || base.cacheWrite != 0 {
		t.Fatalf("base = %+v, want the unknown zero (no record to fold)", base)
	}
}
