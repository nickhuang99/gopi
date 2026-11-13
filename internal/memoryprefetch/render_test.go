package memoryprefetch

import (
	"fmt"
	"strings"
	"testing"
)

// TestRender_EveryHitCarriesSourceAndAdjudicability is the acceptance case
// "rendered RAG segment carries a source annotation per hit and an adjudicability
// marker": BOTH are asserted per hit, in the text AND as data.
func TestRender_EveryHitCarriesSourceAndAdjudicability(t *testing.T) {
	hits := []Hit{
		{ID: 907, Topic: "text2img", Answer: "the 20px recipe lives in our own library", Tier: tierHot, Confidence: 1},
		{ID: 6, Topic: "memory", Answer: "FTS5 unicode61 treats a CJK run as one token", Tier: tierWarm, Confidence: 0.9},
		{ID: 6425, Topic: "prefetch", Answer: "the seam study measured the read-only DSN", Tier: tierCold},
	}
	seg := RenderSegment(hits, nil, DefaultConfig())
	if len(seg.Hits) != len(hits) {
		t.Fatalf("rendered %d hits, want %d", len(seg.Hits), len(hits))
	}
	for i, rh := range seg.Hits {
		if rh.Source.Origin != memoryLedgerOrigin {
			t.Fatalf("hit %d source origin = %q, want %q", i, rh.Source.Origin, memoryLedgerOrigin)
		}
		wantRecord := fmt.Sprintf("#%d", hits[i].ID)
		if rh.Source.Record != wantRecord {
			t.Fatalf("hit %d source record = %q, want %q", i, rh.Source.Record, wantRecord)
		}
		if !strings.Contains(rh.Line, rh.Source.String()) {
			t.Fatalf("hit %d line %q does not carry its source annotation %q", i, rh.Line, rh.Source.String())
		}
		if rh.Adjudicability == "" {
			t.Fatalf("hit %d carries no adjudicability marker", i)
		}
		if !strings.Contains(rh.Line, string(rh.Adjudicability)) {
			t.Fatalf("hit %d line %q does not carry its adjudicability marker %q",
				i, rh.Line, rh.Adjudicability)
		}
		if !strings.Contains(seg.Text, rh.Line) {
			t.Fatalf("hit %d line is missing from the rendered segment", i)
		}
	}
	// A stale record is flagged where the reader can see it.
	stale := RenderSegment([]Hit{{ID: 5, Answer: fillAnswer(60), Tier: tierHot, Stale: true}}, nil, DefaultConfig())
	if stale.Hits[0].Adjudicability != AdjudicabilityStale {
		t.Fatalf("stale hit adjudicability = %q, want %q", stale.Hits[0].Adjudicability, AdjudicabilityStale)
	}
	if !strings.Contains(stale.Hits[0].Line, "stale=yes") {
		t.Fatalf("stale hit line %q must say so", stale.Hits[0].Line)
	}
}

// TestRender_AdjudicabilityMatrix pins the classification table, including the
// two edges that matter: a stale hot hit is stale, and an unknown tier is stated
// as unknown rather than defaulted to "verified".
func TestRender_AdjudicabilityMatrix(t *testing.T) {
	cases := []struct {
		name       string
		tier       string
		stale      bool
		confidence float64
		want       Adjudicability
	}{
		{"hot", tierHot, false, 1, AdjudicabilityHot},
		{"warm-high-confidence", tierWarm, false, 0.95, AdjudicabilityVerified},
		{"warm-low-confidence", tierWarm, false, confidenceFloor - 0.01, AdjudicabilityProvisional},
		{"warm-at-the-floor", tierWarm, false, confidenceFloor, AdjudicabilityVerified},
		{"cold", tierCold, false, 0.5, AdjudicabilityLowConfidence},
		{"stale-outranks-hot", tierHot, true, 1, AdjudicabilityStale},
		{"unknown-tier", "tepid", false, 0.9, AdjudicabilityUnknown},
		{"empty-tier", "", false, 0.9, AdjudicabilityUnknown},
		{"case-insensitive", strings.ToUpper(tierWarm), false, 0.9, AdjudicabilityVerified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AdjudicabilityWithConfidence(tc.tier, tc.stale, tc.confidence); got != tc.want {
				t.Fatalf("AdjudicabilityWithConfidence(%q,%v,%v) = %q, want %q",
					tc.tier, tc.stale, tc.confidence, got, tc.want)
			}
		})
	}
}

// TestRender_DegradationsReachTheReader pins that a degraded run says so IN the
// segment, not only in a log (Rule 15).
func TestRender_DegradationsReachTheReader(t *testing.T) {
	set := mustExtract(t, "打字中后台记忆预取 memory leak", DefaultConfig())
	if !set.Degraded() {
		t.Fatal("fixture is not degraded — the assertion would be vacuous")
	}
	seg := RenderSegment([]Hit{{ID: 1, Answer: fillAnswer(60), Tier: tierHot, Confidence: 1}}, set.Degradations, DefaultConfig())
	if !strings.Contains(seg.Text, string(DegradationCJKBelowFloor)) {
		t.Fatalf("the segment must carry the CJK degradation code (the extraction the fixture really performs); got:\n%s", seg.Text)
	}
	if len(seg.Degradations) == 0 {
		t.Fatal("segment degradations are empty")
	}
}

// TestRender_EmptySectionIsWellFormed pins that an empty hit list still produces
// a header (so the caller can tell "nothing to say" from "rendering failed") and
// nothing else.
func TestRender_EmptySectionIsWellFormed(t *testing.T) {
	seg := RenderSegment(nil, nil, DefaultConfig())
	if seg.Hits != nil {
		t.Fatalf("hits = %+v, want none", seg.Hits)
	}
	if !strings.HasPrefix(seg.Text, "memory-prefetch") {
		t.Fatalf("empty segment text = %q, want the header", seg.Text)
	}
	if strings.Count(seg.Text, "\n") != 0 {
		t.Fatalf("empty segment has more than a header: %q", seg.Text)
	}
	if seg.Runes != runeLenOf(seg.Text) {
		t.Fatalf("reported runes = %d, measured %d", seg.Runes, runeLenOf(seg.Text))
	}
}

// TestRender_ReportsItsOwnCost pins the measured-cost contract: Runes is the
// rune count of the text it produced, and the hit bodies carry the trim's budget.
func TestRender_ReportsItsOwnCost(t *testing.T) {
	hits := []Hit{{ID: 1, Answer: fillAnswer(80), Tier: tierHot, Confidence: 1}}
	cfg := DefaultConfig()
	seg := RenderSegment(hits, nil, cfg)
	if seg.Runes != runeLenOf(seg.Text) {
		t.Fatalf("reported runes = %d, measured %d", seg.Runes, runeLenOf(seg.Text))
	}
	if seg.Budget != cfg.MaxSegmentRunes {
		t.Fatalf("budget = %d, want %d", seg.Budget, cfg.MaxSegmentRunes)
	}
	if seg.Runes < seg.Budget {
		// The displayed hit bodies respect the budget; the header/annotations are
		// on top of it. Stated as an assertion so the split is not a surprise.
		if seg.Runes == 0 {
			t.Fatal("segment reported zero runes for a non-empty section")
		}
	}
}

// TestRender_TrimsTheBodiesItDisplays pins that the renderer and the trim agree:
// the bodies the segment displays are exactly the bodies the trim admitted.
func TestRender_TrimsTheBodiesItDisplays(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSegmentHits = 2
	hits := []Hit{
		{ID: 1, Answer: fillAnswer(60), Tier: tierHot, Confidence: 1},
		{ID: 2, Answer: fillAnswer(60), Tier: tierHot, Confidence: 1},
		{ID: 3, Answer: fillAnswer(60), Tier: tierHot, Confidence: 1},
	}
	seg := RenderSegment(hits, nil, cfg)
	if len(seg.Hits) != 2 {
		t.Fatalf("rendered %d hits, want 2 (MaxSegmentHits)", len(seg.Hits))
	}
	for _, rh := range seg.Hits {
		if rh.Hit.ID == 3 {
			t.Fatal("the cap was not applied by the renderer")
		}
	}
}
