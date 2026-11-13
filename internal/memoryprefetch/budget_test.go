package memoryprefetch

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// fillAnswer builds an answer of exactly n runes.
func fillAnswer(n int) string {
	return strings.Repeat("x", n)
}

// TestTrimNeverExceedsBudget is the acceptance case "trimming never exceeds the
// token-free budget": it holds across a sweep of budgets, including budgets far
// too small for the hits offered.
func TestTrimNeverExceedsBudget(t *testing.T) {
	hits := []Hit{
		{ID: 1, Answer: fillAnswer(200), Tier: tierWarm, Confidence: 0.9},
		{ID: 2, Answer: fillAnswer(200), Tier: tierWarm, Confidence: 0.9},
		{ID: 3, Answer: fillAnswer(200), Tier: tierHot, Confidence: 1},
		{ID: 4, Answer: fillAnswer(200), Tier: tierCold},
		{ID: 5, Answer: fillAnswer(200), Tier: tierWarm, Confidence: 0.5},
		{ID: 6, Answer: fillAnswer(200), Tier: tierWarm, Confidence: 0.4},
	}
	for budget := 1; budget <= 1500; budget++ {
		cfg := DefaultConfig()
		cfg.MaxSegmentRunes = budget
		res := TrimHits(hits, cfg)
		if res.Runes > budget {
			t.Fatalf("budget %d: trimmed runes = %d, exceeds the budget", budget, res.Runes)
		}
		if res.Runes != measureKept(res.Kept, cfg) {
			t.Fatalf("budget %d: reported %d runes, measured %d — the accounting drifted",
				budget, res.Runes, measureKept(res.Kept, cfg))
		}
		if len(res.Kept) > cfg.MaxSegmentHits {
			t.Fatalf("budget %d: kept %d hits, over MaxSegmentHits=%d",
				budget, len(res.Kept), cfg.MaxSegmentHits)
		}
	}
}

// measureKept re-measures the kept bodies the way the contract defines the cost.
func measureKept(kept []Hit, cfg Config) int {
	total := 0
	for _, h := range kept {
		total += bodyRunes(h, cfg)
	}
	return total
}

// TestTrimCapsTheHitCount pins MaxSegmentHits: the cap drops the tail by name,
// even when the rune budget would have room for it.
func TestTrimCapsTheHitCount(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSegmentRunes = 2000
	cfg.MaxSegmentHits = 2
	hits := []Hit{
		{ID: 1, Answer: fillAnswer(200)},
		{ID: 2, Answer: fillAnswer(200)},
		{ID: 3, Answer: fillAnswer(200)},
	}
	res := TrimHits(hits, cfg)
	if len(res.Kept) != 2 {
		t.Fatalf("kept %d hits, want 2 (MaxSegmentHits)", len(res.Kept))
	}
	if len(res.Dropped) != 1 {
		t.Fatalf("dropped %d hits, want 1", len(res.Dropped))
	}
	if res.Dropped[0].Reason != DropReasonHitCountCap {
		t.Fatalf("drop reason = %q, want %q", res.Dropped[0].Reason, DropReasonHitCountCap)
	}
}

// TestTrimBudgetExhaustionIsNamed pins the budget-exhausted path, including the
// whole-section-lost case.
func TestTrimBudgetExhaustionIsNamed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSegmentRunes = 50
	hits := []Hit{{ID: 1, Answer: fillAnswer(200)}, {ID: 2, Answer: fillAnswer(200)}}
	res := TrimHits(hits, cfg)
	if len(res.Kept) != 0 {
		t.Fatalf("kept %d hits inside a %d-rune budget, want 0", len(res.Kept), cfg.MaxSegmentRunes)
	}
	if len(res.Dropped) != 2 || res.Dropped[0].Reason != DropReasonBudgetExhausted {
		t.Fatalf("drops = %+v, want 2 hits dropped as %q", res.Dropped, DropReasonBudgetExhausted)
	}
	d, ok := findDegradationIn(res.Degradations, DegradationAllHitsDropped)
	if !ok {
		t.Fatalf("an all-dropped trim must record %s; got %v", DegradationAllHitsDropped, res.Degradations)
	}
	if !strings.Contains(d.Detail, string(DropReasonBudgetExhausted)) {
		t.Fatalf("the detail must name the drop reason, got %q", d.Detail)
	}
}

// TestTrimTooSmallBudgetForAnyHit pins the configuration edge where the budget
// cannot hold even the shortest renderable hit: an empty result and a NAMED
// degradation, never an over-budget section.
func TestTrimTooSmallBudgetForAnyHit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSegmentRunes = cfg.MinHitRunes - 1
	res := TrimHits([]Hit{{ID: 1, Answer: fillAnswer(200)}}, cfg)
	if len(res.Kept) != 0 || res.Runes != 0 {
		t.Fatalf("kept %+v / %d runes, want nothing inside a sub-MinHitRunes budget", res.Kept, res.Runes)
	}
	if _, ok := findDegradationIn(res.Degradations, DegradationBudgetTooSmall); !ok {
		t.Fatalf("must record %s; got %v", DegradationBudgetTooSmall, res.Degradations)
	}
}

// TestTrimDropsSliversNotFullHits pins MinHitRunes: a hit with almost no content
// is dropped by name instead of being rendered as a meaningless fragment.
func TestTrimDropsSliversNotFullHits(t *testing.T) {
	cfg := DefaultConfig()
	res := TrimHits([]Hit{{ID: 1, Answer: "tiny"}}, cfg)
	if len(res.Kept) != 0 {
		t.Fatalf("kept a %d-rune body; want it dropped below MinHitRunes=%d",
			len([]rune("tiny")), cfg.MinHitRunes)
	}
	if res.Dropped[0].Reason != DropReasonHitTooShort {
		t.Fatalf("drop reason = %q, want %q", res.Dropped[0].Reason, DropReasonHitTooShort)
	}
}

// TestTrimClipsLongBodiesOnRuneBoundaries pins MaxHitRunes with multi-byte text:
// the clip is counted in runes and never splits a CJK character. It also pins
// that Kept carries the caller's ORIGINAL hit — the clip happens at render time,
// so the trim reports a measured cost, not a rewritten record.
func TestTrimClipsLongBodiesOnRuneBoundaries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxHitRunes = 64
	cfg.MinHitRunes = 8
	hits := []Hit{{ID: 1, Answer: strings.Repeat("中", 100)}}
	res := TrimHits(hits, cfg)
	if len(res.Kept) != 1 {
		t.Fatalf("kept %d hits, want 1 (cost %d)", len(res.Kept), res.Runes)
	}
	if res.Runes != cfg.MaxHitRunes {
		t.Fatalf("reported %d runes, want exactly the clip of MaxHitRunes=%d", res.Runes, cfg.MaxHitRunes)
	}
	clipped := clipBody(hitBody(res.Kept[0]), cfg.MaxHitRunes)
	if !utf8.ValidString(clipped) {
		t.Fatalf("the clipped body is not valid UTF-8 — a rune was split: %q", clipped)
	}
	if got := len([]rune(clipped)); got != cfg.MaxHitRunes {
		t.Fatalf("clipped body has %d runes, want %d (the clip mark is INSIDE the budget)",
			got, cfg.MaxHitRunes)
	}
	if !strings.HasSuffix(clipped, clipMark) {
		t.Fatalf("a clipped body must say it was cut short, got %q", clipped)
	}
	if strings.ContainsRune(clipped, utf8.RuneError) {
		t.Fatalf("the clipped body carries a replacement character: %q", clipped)
	}
}

// TestTrimUnclippedBodyCarriesNoMark pins the other side: a body that fits is
// rendered verbatim, so the mark MEANS "cut short" rather than "long".
func TestTrimUnclippedBodyCarriesNoMark(t *testing.T) {
	cfg := DefaultConfig()
	const body = "short and complete but still under the clip"
	res := TrimHits([]Hit{{ID: 1, Answer: body}}, cfg)
	if len(res.Kept) != 1 {
		t.Fatalf("kept %d hits, want 1", len(res.Kept))
	}
	if strings.Contains(clipBody(hitBody(res.Kept[0]), cfg.MaxHitRunes), clipMark) {
		t.Fatal("a body that fits must not carry the clip mark")
	}
}

// TestTrimFlattensMultiLineBodies pins that a hit cannot break the section's line
// structure: newlines and runs of whitespace collapse to a single space.
func TestTrimFlattensMultiLineBodies(t *testing.T) {
	hits := []Hit{{ID: 1, Answer: "line one\nline two\t\tline three"}}
	res := TrimHits(hits, DefaultConfig())
	if len(res.Kept) != 1 {
		t.Fatalf("kept %d hits, want 1", len(res.Kept))
	}
	if flat := cellText(res.Kept[0].Answer); strings.ContainsAny(flat, "\n\t") {
		t.Fatalf("body was not flattened: %q", flat)
	}
}

// TestTrimFallsBackToQuestionThenTopic pins the body resolution order, so a hit
// with no answer is still usable rather than silently empty.
func TestTrimFallsBackToQuestionThenTopic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinHitRunes = 1
	byQuestion := TrimHits([]Hit{{ID: 1, Question: fillAnswer(50)}}, cfg)
	if len(byQuestion.Kept) != 1 || byQuestion.Runes == 0 {
		t.Fatalf("a hit with only a Question must be usable: %+v", byQuestion)
	}
	byTopic := TrimHits([]Hit{{ID: 2, Topic: fillAnswer(50)}}, cfg)
	if len(byTopic.Kept) != 1 || byTopic.Runes == 0 {
		t.Fatalf("a hit with only a Topic must be usable: %+v", byTopic)
	}
	empty := TrimHits([]Hit{{ID: 3}}, cfg)
	if len(empty.Kept) != 0 || empty.Dropped[0].Reason != DropReasonHitTooShort {
		t.Fatalf("a hit with no body at all must be dropped by name: %+v", empty)
	}
}

// TestTrimIsPureAndOrderPreserving pins the two properties a caller relies on.
func TestTrimIsPureAndOrderPreserving(t *testing.T) {
	hits := []Hit{
		{ID: 11, Answer: fillAnswer(40)},
		{ID: 12, Answer: fillAnswer(40)},
		{ID: 13, Answer: fillAnswer(40)},
	}
	cfg := DefaultConfig()
	first := TrimHits(hits, cfg)
	second := TrimHits(hits, cfg)
	if len(first.Kept) != len(second.Kept) || first.Runes != second.Runes {
		t.Fatalf("trim is not deterministic: %+v vs %+v", first, second)
	}
	for i, h := range first.Kept {
		if h.ID != hits[i].ID {
			t.Fatalf("kept order changed at %d: %d, want %d", i, h.ID, hits[i].ID)
		}
	}
	if hits[0].ID != 11 {
		t.Fatal("trim mutated the caller's slice")
	}
}

func findDegradationIn(list []Degradation, code DegradationCode) (Degradation, bool) {
	for _, d := range list {
		if d.Code == code {
			return d, true
		}
	}
	return Degradation{}, false
}
