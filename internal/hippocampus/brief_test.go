package hippocampus

// brief_test.go — the ONE derivation of the numbers a host publishes about a
// recall (ClampedBriefConfidence / ClampedBriefMemoryWeight).
//
// These tests live next to the arithmetic on purpose: the host that consumes
// them is a pure adapter, so the numbers' behaviour (the damping, the tier
// normalisation, the display cap, the empty reading) is pinned HERE, once — not
// re-derived and re-tested on the host side (devdoc/hippocampus-host-resolution.md).

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// hit builds one lead with an explicit tier/coverage; EffConfidence is left
// zero unless a test sets it, mirroring a hit the memory engine did not re-score.
func hit(tier string, conf float64, tokens int) tools.MemoryHit {
	return tools.MemoryHit{Tier: tier, Confidence: conf, Tokens: tokens}
}

// briefFloatEps is how close two readings must be to count as equal: the
// arithmetic is float64, so an exact compare would fail on representation
// noise (0.29250000000000004) rather than on behaviour.
const briefFloatEps = 1e-9

// closeEnough compares two derived readings within briefFloatEps.
func closeEnough(got, want float64) bool {
	d := got - want
	return d < briefFloatEps && d > -briefFloatEps
}

// TestClampedBriefConfidence_TakesTheMaxNotTheFirst: the reading must not
// depend on retrieval order — the live path deliberately does NOT re-sort hits,
// so a consumer that assumed Hits[0] was the strongest would publish a weaker
// number than the recall earned.
func TestClampedBriefConfidence_TakesTheMaxNotTheFirst(t *testing.T) {
	got := ClampedBriefConfidence(&Result{Hits: []tools.MemoryHit{
		hit(tools.TierWarm, 0.2, 1),
		hit(tools.TierWarm, 0.9, 1),
	}})
	if got != 0.9 {
		t.Fatalf("confidence = %v, want 0.9 (the STRONGEST hit, not Hits[0])", got)
	}
}

// TestClampedBriefConfidence_IsDampedByLeadFit pins the protection the host's
// delivery ladder depends on: one matching lead among four loose ones must not
// be published as if every lead had matched.
func TestClampedBriefConfidence_IsDampedByLeadFit(t *testing.T) {
	all := ClampedBriefConfidence(&Result{Hits: []tools.MemoryHit{
		hit(tools.TierWarm, 0.9, 1), hit(tools.TierWarm, 0.9, 2),
		hit(tools.TierWarm, 0.9, 3), hit(tools.TierWarm, 0.9, 1),
	}})
	if all != 0.9 {
		t.Fatalf("all leads matched: confidence = %v, want 0.9 (no damping)", all)
	}
	one := ClampedBriefConfidence(&Result{Hits: []tools.MemoryHit{
		hit(tools.TierWarm, 0.9, 1), hit(tools.TierWarm, 0.9, 0),
		hit(tools.TierWarm, 0.9, 0), hit(tools.TierWarm, 0.9, 0),
	}})
	// fit = 1/4 ⇒ 0.9 × (0.1 + 0.9×0.25) = 0.2925.
	if want := 0.2925; !closeEnough(one, want) {
		t.Fatalf("one of four leads matched: confidence = %v, want %v", one, want)
	}
	if one <= briefConfidenceNoHits {
		t.Fatalf("a matching lead must read ABOVE the empty-recall floor (%v), got %v", briefConfidenceNoHits, one)
	}
}

// TestClampedBriefConfidence_EmptyRecallStaysUnderTheAppendFloor: the host
// archives anything below its append floor (0.2), so an empty retrieval must
// read UNDER it — never dressed up as a lead.
func TestClampedBriefConfidence_EmptyRecallStaysUnderTheAppendFloor(t *testing.T) {
	if got := ClampedBriefConfidence(nil); got != briefConfidenceNoHits {
		t.Fatalf("nil result: confidence = %v, want %v", got, briefConfidenceNoHits)
	}
	if got := ClampedBriefConfidence(&Result{}); got != briefConfidenceNoHits {
		t.Fatalf("no hits: confidence = %v, want %v", got, briefConfidenceNoHits)
	}
}

// TestClampedBriefConfidence_PrefersTheEffectiveReading: the memory engine's
// decay/re-verify adjusted confidence is the number the model is SHOWN about a
// memory, so a brief's number must be built on the same reading.
func TestClampedBriefConfidence_PrefersTheEffectiveReading(t *testing.T) {
	h := hit(tools.TierWarm, 0.2, 1)
	if got := EffectiveConfidence(h); got != 0.2 {
		t.Fatalf("no effective confidence: got %v, want the stored 0.2", got)
	}
	h.EffConfidence = 0.95
	if got := EffectiveConfidence(h); got != 0.95 {
		t.Fatalf("effective confidence present: got %v, want 0.95", got)
	}
	if got := ClampedBriefConfidence(&Result{Hits: []tools.MemoryHit{h}}); got != 0.95 {
		t.Fatalf("confidence = %v, want 0.95 (the effective reading)", got)
	}
}

// TestClampedBriefConfidence_IsClamped: a value outside [0,1] would defeat
// every floor comparison the host makes.
func TestClampedBriefConfidence_IsClamped(t *testing.T) {
	if got := ClampedBriefConfidence(&Result{Hits: []tools.MemoryHit{hit(tools.TierHot, 1.5, 1)}}); got != 1 {
		t.Fatalf("over-confident lead: got %v, want the 1.0 ceiling", got)
	}
}

// TestClampedBriefMemoryWeight_IsNormalisedByTheTierFactor: the weight is a
// READING in [0,1], so a lead's tier must not shrink its published weight — a
// warm lead at 0.9 weighs 0.9, not 0.9×0.7.
func TestClampedBriefMemoryWeight_IsNormalisedByTheTierFactor(t *testing.T) {
	for _, tier := range []string{tools.TierHot, tools.TierWarm, tools.TierCold} {
		if got := ClampedBriefMemoryWeight(&Result{Hits: []tools.MemoryHit{hit(tier, 0.9, 1)}}); got != 0.9 {
			t.Fatalf("tier %q: weight = %v, want 0.9 (tier-normalised)", tier, got)
		}
	}
	// A cold lead reads at its own confidence, NOT at the memory engine's
	// ranking weight (-6.0 would put it far below zero).
	if got := ClampedBriefMemoryWeight(&Result{Hits: []tools.MemoryHit{hit(tools.TierCold, 0.1, 1)}}); got != 0.1 {
		t.Fatalf("cold lead: weight = %v, want 0.1", got)
	}
}

// TestClampedBriefMemoryWeight_AveragesTheTierFactors: mixed tiers must blend
// by the tier factors (hot 2.0 / warm 1.0 / cold 0.25), so a hot lead pulls the
// mean up and a cold one down.
func TestClampedBriefMemoryWeight_AveragesTheTierFactors(t *testing.T) {
	got := ClampedBriefMemoryWeight(&Result{Hits: []tools.MemoryHit{
		hit(tools.TierHot, 0.9, 1), hit(tools.TierWarm, 0.4, 1),
	}})
	// (0.9×2.0 + 0.4×1.0) / (2.0+1.0) = 2.2/3.
	if want := 2.2 / 3.0; !closeEnough(got, want) {
		t.Fatalf("mixed tiers: weight = %v, want %v", got, want)
	}
	// A cold partner contributes LESS than a warm one, so the hot lead
	// dominates the mean: (0.9×2.0 + 0.4×0.25) / (2.0+0.25).
	cold := ClampedBriefMemoryWeight(&Result{Hits: []tools.MemoryHit{
		hit(tools.TierHot, 0.9, 1), hit(tools.TierCold, 0.4, 1),
	}})
	if want := 1.9 / 2.25; !closeEnough(cold, want) {
		t.Fatalf("hot+cold blend: weight = %v, want %v", cold, want)
	}
	if cold == got {
		t.Fatalf("the tier factor had no effect on the blend: both readings are %v", cold)
	}
}

// TestTierWeight_UnknownTierWeighsLikeWarm: a future tier value must never be
// ranked as cold (which would silently suppress a legitimate lead), and the
// lookup must tolerate the spacing/case a stored value may carry.
func TestTierWeight_UnknownTierWeighsLikeWarm(t *testing.T) {
	warm := TierWeight(tools.TierWarm)
	for _, tier := range []string{"", "future-tier", "  Warm  ", "WARM"} {
		if got := TierWeight(tier); got != warm {
			t.Fatalf("tier %q: weight = %v, want the warm factor %v", tier, got, warm)
		}
	}
	if TierWeight(tools.TierHot) <= warm || TierWeight(tools.TierCold) >= warm {
		t.Fatalf("tier order broken: hot=%v warm=%v cold=%v",
			TierWeight(tools.TierHot), warm, TierWeight(tools.TierCold))
	}
}

// TestClampedBriefMemoryWeight_AveragesOnlyTheDisplayedLeads: the host shows
// BriefTopHits leads, so the number must describe THOSE — a change in a lead
// the model never sees must not move the published weight.
func TestClampedBriefMemoryWeight_AveragesOnlyTheDisplayedLeads(t *testing.T) {
	leads := []tools.MemoryHit{
		hit(tools.TierWarm, 0.9, 1), hit(tools.TierWarm, 0.9, 1), hit(tools.TierWarm, 0.9, 1),
	}
	shown := ClampedBriefMemoryWeight(&Result{Hits: leads})
	if shown != 0.9 {
		t.Fatalf("weight = %v, want 0.9", shown)
	}
	// A zero-weight FOURTH lead (beyond the display) must not drag the number.
	tail := append(append([]tools.MemoryHit{}, leads...), hit(tools.TierWarm, 0, 0))
	if got := ClampedBriefMemoryWeight(&Result{Hits: tail}); got != shown {
		t.Fatalf("a lead beyond the display cap moved the weight: %v != %v", got, shown)
	}
	if BriefTopHits != 3 {
		t.Fatalf("display cap = %d, want 3 (the fixture above assumes it)", BriefTopHits)
	}
}

// TestClampedBriefMemoryWeight_EmptyAndClamped covers the degenerate readings.
func TestClampedBriefMemoryWeight_EmptyAndClamped(t *testing.T) {
	if got := ClampedBriefMemoryWeight(nil); got != 0 {
		t.Fatalf("nil result: weight = %v, want 0", got)
	}
	if got := ClampedBriefMemoryWeight(&Result{}); got != 0 {
		t.Fatalf("no hits: weight = %v, want 0", got)
	}
	if got := ClampedBriefMemoryWeight(&Result{Hits: []tools.MemoryHit{hit(tools.TierHot, 2, 1)}}); got != 1 {
		t.Fatalf("over-weight lead: got %v, want the 1.0 ceiling", got)
	}
}
