package tools

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// firstopen_ab_repro_test.go — the instrument's own regression guard (Rule 5)
// and the decision function's table.
//
// WHY THIS FILE EXISTS. The A/B was red on the merge/verify path while its two
// arms differed by nothing but a pragma: the instrument took ONE wall-clock
// sample per arm and asserted their ORDER, which a single contended sample
// inverts (devdoc/firstopen-ab-contention.md). The replacement is an order-
// balanced paired signed-rank test whose burden of proof is the run's own exact
// null distribution. That replacement needs a test that cannot itself be flaky,
// so:
//
//   - the statistic is a PURE function (firstopen_sync_decision.go, no device, no
//     clock, no goroutine) and is driven both by literal hand-computable sets and
//     by sets generated from the host's MEASURED noise model;
//   - the noisy-neighbour case is JITTER INJECTED INTO A SLICE, which is the
//     honest instrument for this regression: Rule 27 forbids generating device
//     load to reproduce a contention failure, and it is not needed — the defect
//     was never "the device was busy", it was "one busy sample decided the
//     verdict", which is fully reproducible as data;
//   - the generator is seeded with a FIXED constant, so the data is identical on
//     every run: a repro whose data varies would be the flake it exists to
//     prevent (Rule 10.4);
//   - the negative probe proves the new instrument did not become a test that
//     always passes: a systematic wrong-way effect still fails through the same
//     code path.

// The synthetic model's constants. Every value is named (Rule 1a) because the
// cases are ABOUT the relation between them, and the magnitudes are the ones this
// host MEASURED (devdoc/firstopen-ab-contention.md, ".evid4/calibration").
const (
	// firstOpenABReproBuildMs is a first open's cost level in the synthetic sets.
	// The host measures 10-30ms and moves by 3x between runs; the comparisons
	// below are all within-run, so the level is a detail.
	firstOpenABReproBuildMs = 20.0
	// firstOpenABReproEffect is the real arm-to-arm effect the A/B exists to
	// detect, as a fraction of the build cost: the host measured 1-14%, median
	// ~10%, so the middle of the real range.
	firstOpenABReproEffect = 0.10
	// firstOpenABReproJitterSigma is the per-build log-noise the host measured:
	// σ ≈ 0.12 log units (median |log ratio| 0.08, p90 0.20).
	firstOpenABReproJitterSigma = 0.12
	// firstOpenABReproOutlierRate and firstOpenABReproOutlierSigma are the tail of
	// that measurement: ~5% of builds are contended and carry ~3x the ordinary
	// wobble (p90 0.20 against σ 0.12 implies a heavy right tail; the observed
	// maximum |log ratio| over 80 pairs was 0.65).
	firstOpenABReproOutlierRate  = 0.05
	firstOpenABReproOutlierSigma = 0.36
	// firstOpenABReproSpikeMs is one contiguous neighbour's contribution to a
	// SINGLE build of a pair — a contended fsync burst. It is an order of
	// magnitude above the whole arm-to-arm effect (2.0ms per pair at a 10%
	// effect), which is the entire defect: the old instrument read that one
	// sample as the mechanism.
	firstOpenABReproSpikeMs = 400.0
	// firstOpenABReproSeed fixes the generated noise so the data is identical on
	// every run.
	firstOpenABReproSeed = 20260917
)

// firstOpenABReproPairs is the synthetic pair count: the calibrated
// firstOpenABPairs, so the table and the probes exercise the same N the
// device test runs.
const firstOpenABReproPairs = firstOpenABPairs

// firstOpenABReproClean returns a delta set of n pairs drawn from the measured
// noise model with the arms' roles as given by effectSign: every pair's true
// difference is effectSign * the real effect (negative = relaxed cheaper), plus
// per-pair noise from the measured distribution.
//
// It returns the DELTAS in milliseconds: a pair's four builds each carry
// independent noise, and the pair's value is the mean of its two relaxed builds
// minus the mean of its two durable builds, which halves the per-build variance
// (firstOpenABPairDelta).
func firstOpenABReproClean(n int, effectSign float64, rng *rand.Rand) []float64 {
	noise := func() float64 {
		sigma := firstOpenABReproJitterSigma
		if rng.Float64() < firstOpenABReproOutlierRate {
			sigma = firstOpenABReproOutlierSigma
		}
		return rng.NormFloat64() * sigma
	}
	trueDelta := effectSign * firstOpenABReproEffect * firstOpenABReproBuildMs
	out := make([]float64, n)
	for i := range out {
		// Two builds per arm, averaged: the pair's noise is the mean of four
		// independent per-build noises, scaled to milliseconds.
		armNoise := 0.0
		for b := 0; b < firstOpenABBuildsPerPair; b++ {
			armNoise += noise()
		}
		out[i] = trueDelta + armNoise/float64(firstOpenABBuildsPerPair/2)*firstOpenABReproBuildMs
	}
	return out
}

// firstOpenABReproSpikedAt adds one contiguous neighbour's burst to pair at: the
// burst lands inside ONE of that pair's builds, so it moves the pair's value by
// the burst divided by the number of builds that arm averaged (two).
func firstOpenABReproSpikedAt(deltas []float64, at int) []float64 {
	out := append([]float64(nil), deltas...)
	out[at] += firstOpenABReproSpikeMs / float64(firstOpenABBuildsPerPair/2)
	return out
}

// TestFirstOpenABDecisionTable pins the pure statistic's contract on literals:
// verdict, the reported evidence, and the exact null's threshold — no device.
func TestFirstOpenABDecisionTable(t *testing.T) {
	repeat := func(v float64, n int) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = v
		}
		return out
	}
	// alternating is a hand-computable set with a decisive relaxed lead and
	// realistic spread: pairs alternate between a 10ms and a 1ms relaxed win.
	alternating := func(n int) []float64 {
		out := make([]float64, n)
		for i := range out {
			if i%2 == 0 {
				out[i] = -10
				continue
			}
			out[i] = -1
		}
		return out
	}
	cases := []struct {
		name         string
		deltas       []float64
		wantVerdict  string
		wantPairs    int
		wantUsable   int
		wantRelaxed  int
		wantCritical bool
		why          string
	}{
		{
			name: "relaxed-cheaper-in-every-pair", deltas: repeat(-5, firstOpenABReproPairs),
			wantVerdict: firstOpenABVerdictRelaxed, wantPairs: firstOpenABReproPairs,
			wantUsable: firstOpenABReproPairs, wantRelaxed: firstOpenABReproPairs, wantCritical: true,
			why: "a uniform lead is the strongest evidence the statistic can carry: every pair votes the same way",
		},
		{
			name: "durable-cheaper-in-every-pair", deltas: repeat(5, firstOpenABReproPairs),
			wantVerdict: firstOpenABVerdictDurable, wantPairs: firstOpenABReproPairs,
			wantUsable: firstOpenABReproPairs, wantCritical: true,
			why: "the roles swapped — a systematic wrong-way effect must be reported, which is what makes the negative probe possible",
		},
		{
			name: "decisive-lead-over-a-noisy-field", deltas: alternating(firstOpenABReproPairs),
			wantVerdict: firstOpenABVerdictRelaxed, wantPairs: firstOpenABReproPairs,
			wantUsable: firstOpenABReproPairs, wantRelaxed: firstOpenABReproPairs, wantCritical: true,
			why: "a lead carried by ranks (not by magnitude) still counts: the noisy pairs keep their ranks, they just lose the argument",
		},
		{
			name: "near-even-split", deltas: func() []float64 {
				d := alternating(firstOpenABReproPairs)
				for i := 0; i < firstOpenABReproPairs/2; i++ {
					d[i] = -d[i] // half the pairs vote the other way
				}
				return d
			}(),
			wantVerdict: firstOpenABVerdictAmbiguous, wantPairs: firstOpenABReproPairs,
			wantUsable: firstOpenABReproPairs, wantCritical: true,
			why: "an even split leaves neither side near the threshold: the run must say nothing and skip",
		},
		{
			name: "one-huge-pair-does-not-decide", deltas: func() []float64 {
				d := repeat(-1, firstOpenABReproPairs)
				d[0] = firstOpenABReproSpikeMs // one contended pair, the wrong way
				return d
			}(),
			wantVerdict: firstOpenABVerdictRelaxed, wantPairs: firstOpenABReproPairs,
			wantUsable: firstOpenABReproPairs, wantRelaxed: firstOpenABReproPairs - 1, wantCritical: true,
			why: "under ranks the burst is ONE observation with ONE rank, however large — the defect the old mean would have read as the mechanism",
		},
		{
			name: "zero-pairs-are-dropped", deltas: func() []float64 {
				d := repeat(-5, firstOpenABReproPairs)
				for i := 0; i < firstOpenABReproPairs/2; i++ {
					d[i] = 0 // ties: no ordering information, no rank
				}
				return d
			}(),
			wantVerdict: firstOpenABVerdictRelaxed, wantPairs: firstOpenABReproPairs,
			wantUsable: firstOpenABReproPairs / 2, wantRelaxed: firstOpenABReproPairs / 2, wantCritical: true,
			why: "a pair whose arms came out identical carries no sign: it is dropped rather than counted either way",
		},
		{
			name: "too-few-pairs", deltas: repeat(-5, 3),
			wantVerdict: firstOpenABVerdictAmbiguous, wantPairs: 3, wantUsable: 3,
			why: "with too few pairs no lead can reach the false-verdict rate at all, so the verdict is ambiguity — exactly what the rate implies",
		},
		{
			name: "empty", deltas: nil,
			wantVerdict: firstOpenABVerdictAmbiguous,
			why:         "no pairs at all is the same statement as too few: nothing measured, nothing asserted",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstOpenABDecide(tc.deltas)
			if got.Verdict != tc.wantVerdict {
				t.Errorf("verdict = %q over %d pairs (usable %d, W- %.1f, W+ %.1f, crit %.1f), want %q — %s",
					got.Verdict, got.Pairs, got.UsablePairs, got.RelaxedRankSum, got.DurableRankSum,
					got.CriticalRankSum, tc.wantVerdict, tc.why)
			}
			if got.Pairs != tc.wantPairs {
				t.Errorf("pairs = %d, want %d — %s", got.Pairs, tc.wantPairs, tc.why)
			}
			if tc.wantUsable != 0 && got.UsablePairs != tc.wantUsable {
				t.Errorf("usable pairs = %d, want %d — %s", got.UsablePairs, tc.wantUsable, tc.why)
			}
			if tc.wantRelaxed != 0 && got.RelaxedCheaperPairs != tc.wantRelaxed {
				t.Errorf("relaxed-cheaper pairs = %d, want %d — %s", got.RelaxedCheaperPairs, tc.wantRelaxed, tc.why)
			}
			if tc.wantCritical && got.CriticalRankSum <= 0 {
				t.Errorf("no critical rank sum was computed for %d usable pairs — the burden of proof is "+
					"the run's own exact null and must exist here (%s)", got.UsablePairs, tc.why)
			}
			if !tc.wantCritical && got.CriticalRankSum > 0 {
				t.Errorf("critical rank sum = %.1f, want none — with %d usable pairs no lead can reach the "+
					"false-verdict rate (%s)", got.CriticalRankSum, got.UsablePairs, tc.why)
			}
			// The reported evidence must describe the input it came from.
			if got.RelaxedCheaperPairs+got.DurableCheaperPairs != got.UsablePairs {
				t.Errorf("the pair counts do not account for the usable pairs: %d + %d != %d",
					got.RelaxedCheaperPairs, got.DurableCheaperPairs, got.UsablePairs)
			}
			if len(tc.deltas) > 0 && got.MeanDeltaMs*float64(len(tc.deltas)) == math.Inf(1) {
				t.Errorf("mean delta is not finite: %v", got.MeanDeltaMs)
			}
		})
	}
}

// TestFirstOpenABExactNullMatchesBruteForce is the independence check on the one
// piece of arithmetic a reader cannot verify by eye: firstOpenABWilcoxonCritical
// counts the exact subset-sum distribution by dynamic programming, and this test
// enumerates the SAME distribution by brute force over every subset for small
// ranks, asserting the two agree on the whole tail. Without it, a silent error in
// the DP would move every threshold in the instrument and no other test would
// notice.
func TestFirstOpenABExactNullMatchesBruteForce(t *testing.T) {
	rankSets := [][]int{
		{1, 2, 3, 4, 5},
		{2, 4, 6, 8, 10, 12},
		{1, 1, 3, 3, 5, 7},    // tie groups, as midranks produce
		{3, 3, 3, 3, 3, 3, 3}, // pathological: every rank identical
		{1, 2, 4, 8, 16},
	}
	for _, ranks := range rankSets {
		t.Run(ranksName(ranks), func(t *testing.T) {
			crit, ok := firstOpenABWilcoxonCritical(ranks, len(ranks))
			// Brute force: every subset of the ranks, one per sign pattern.
			subsets := 1 << len(ranks)
			tailAt := func(w float64) float64 {
				hits := 0
				for mask := 0; mask < subsets; mask++ {
					sum := 0
					for i, r := range ranks {
						if mask&(1<<i) != 0 {
							sum += r
						}
					}
					if float64(sum) >= w {
						hits++
					}
				}
				return float64(hits) / float64(subsets)
			}
			total := 0
			for _, r := range ranks {
				total += r
			}
			if !ok {
				// With this few pairs there may be NO lead whose probability is
				// within the rate — the smallest possible tail is 2^-n, and the
				// function must say so rather than return an unreachable number.
				if smallest := tailAt(float64(total)); smallest <= firstOpenABMaxFalseVerdictRate {
					t.Errorf("no critical value was returned for %v, yet the most extreme possible lead has a "+
						"brute-force tail of %.6f, within the rate %v — a reachable threshold was missed",
						ranks, smallest, firstOpenABMaxFalseVerdictRate)
				}
				return
			}
			// The returned value is the smallest lead whose tail is within the
			// rate; the value below it must NOT be.
			if got := tailAt(crit); got > firstOpenABMaxFalseVerdictRate {
				t.Errorf("the critical lead %.0f has a brute-force tail of %.6f, above the rate %v — the DP "+
					"understates the false-verdict probability", crit, got, firstOpenABMaxFalseVerdictRate)
			}
			if crit > 1 {
				if got := tailAt(crit - 1); got <= firstOpenABMaxFalseVerdictRate {
					t.Errorf("a lead of %.0f already has a brute-force tail of %.6f, at or below the rate %v — "+
						"the DP returns a threshold stricter than it needs to be", crit-1, got,
						firstOpenABMaxFalseVerdictRate)
				}
			}
		})
	}
}

// ranksName renders a rank set for a subtest name.
func ranksName(ranks []int) string {
	out := "ranks"
	for _, r := range ranks {
		out += fmt.Sprintf("-%d", r)
	}
	return out
}

// TestFirstOpenABMidranksAreAveragesInTieGroups pins the tie handling: every
// member of a tie group carries the group's MEAN rank. Collapsing ties to
// distinct ranks would inflate the statistic's resolution, and ignoring the tie
// group's size would make the exact null wrong for the common case of a pair
// whose arms landed within a microsecond.
func TestFirstOpenABMidranksAreAveragesInTieGroups(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		want   []float64
		why    string
	}{
		{name: "no-ties", values: []float64{3, 1, 2}, want: []float64{3, 1, 2}, why: "distinct magnitudes keep their ranks"},
		{name: "sign-is-ignored", values: []float64{-3, 1, -2}, want: []float64{3, 1, 2},
			why: "the rank is of |value|: the sign is the test's business, not the ranking's"},
		{name: "even-tie-group", values: []float64{5, 5, 1}, want: []float64{2.5, 2.5, 1},
			why: "two tied values share ranks 2 and 3, so both carry 2.5"},
		{name: "odd-tie-group", values: []float64{9, 1, 1, 9, 5}, want: []float64{4.5, 1.5, 1.5, 4.5, 3},
			why: "tie groups of two and two, each sharing its ranks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstOpenABMidranks(tc.values)
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("ranks(%v) = %v, want %v — %s", tc.values, got, tc.want, tc.why)
				}
			}
		})
	}
}

// TestFirstOpenNoisyNeighbourDoesNotFlipTheVerdict is the minimal repro of the
// merge-path red (Rule 5), in the only form Rule 27 allows: synthetic samples
// drawn from this host's measured noise model.
//
// It shows BOTH halves of the claim on one sample set —
//
//	(a) the OLD instrument is red on this data: comparing ONE sample per arm, as
//	    the pre-rework assertion did, reports the relaxed arm SLOWER once the
//	    neighbour's burst lands in that sample;
//	(b) the NEW instrument is green on the same data: the burst moves one pair and
//	    the verdict does not move.
//
// (a) is what makes this a repro rather than a restatement: without it, "the new
// statistic is green" could be a test that always passes.
func TestFirstOpenNoisyNeighbourDoesNotFlipTheVerdict(t *testing.T) {
	rng := rand.New(rand.NewSource(firstOpenABReproSeed))
	clean := firstOpenABReproClean(firstOpenABReproPairs, -1, rng)
	spiked := firstOpenABReproSpikedAt(clean, firstOpenABReproSpikedPair)

	// (a) the old instrument: one sample per arm, plain ordering. The clean run's
	// first pair is the comparison the old test would have read; the spiked pair
	// is the same comparison arriving in the neighbour's window.
	if clean[0] >= 0 {
		t.Fatalf("the synthetic set is wrong: the clean first pair (%+.3fms) must show the relaxed arm "+
			"cheaper — otherwise there is no effect for the noise to hide", clean[0])
	}
	if spiked[firstOpenABReproSpikedPair] <= 0 {
		t.Fatalf("the synthetic set is wrong: the spiked pair (%+.3fms) must show the relaxed arm SLOWER — "+
			"otherwise this test does not reproduce the single-sample flip it is about",
			spiked[firstOpenABReproSpikedPair])
	}

	// (b) the new instrument: same data, order-balanced signed ranks.
	decision := firstOpenABDecide(spiked)
	if decision.Verdict != firstOpenABVerdictRelaxed {
		t.Errorf("verdict = %q over %d pairs (usable %d, W- %.1f vs W+ %.1f, crit %.1f; mean %+.3fms, "+
			"median %+.3fms), want %q: one contended pair must not decide the verdict",
			decision.Verdict, decision.Pairs, decision.UsablePairs, decision.RelaxedRankSum,
			decision.DurableRankSum, decision.CriticalRankSum, decision.MeanDeltaMs,
			decision.MedianDeltaMs, firstOpenABVerdictRelaxed)
	}
	if err := firstOpenABMisattribution(decision); err != nil {
		t.Errorf("the assertion fired on noisy-but-correct data: %v", err)
	}
	// The burst must cost the wrong side exactly ONE pair MORE than the same run
	// without it — that is the mechanism of the tolerance: under ranks the
	// hostile sample is one observation, however large, and the pairs the
	// ordinary noise already sent the other way are unaffected.
	cleanDecision := firstOpenABDecide(clean)
	if decision.DurableCheaperPairs != cleanDecision.DurableCheaperPairs+1 {
		t.Errorf("the burst flipped %d pairs (clean run: %d), want exactly one more — a single contended "+
			"build must move one pair and nothing else", decision.DurableCheaperPairs,
			cleanDecision.DurableCheaperPairs)
	}
}

// firstOpenABReproSpikedPair is the pair a neighbour's burst lands in, chosen
// from the middle of the run: the old instrument's failure was not about WHICH
// sample it happened to read — it read one sample per arm and the burst could
// land anywhere.
const firstOpenABReproSpikedPair = firstOpenABReproPairs / 2

// TestFirstOpenABGenuineMisattributionStillFails is the negative probe (Rule
// 10.4): the rework must not have produced a test that cannot fail. With the
// effect's SIGN flipped — a systematic, unambiguous wrong way — the SAME decision
// path must report the misattribution, and it must stay silent for an ambiguous
// run, so the probe cannot be satisfied by an assertion that always fires.
func TestFirstOpenABGenuineMisattributionStillFails(t *testing.T) {
	rng := rand.New(rand.NewSource(firstOpenABReproSeed))
	misattributed := firstOpenABDecide(firstOpenABReproClean(firstOpenABReproPairs, +1, rng))
	if misattributed.Verdict != firstOpenABVerdictDurable {
		t.Fatalf("verdict = %q (W+ %.1f vs crit %.1f) on a systematic wrong-way effect, want %q — this probe "+
			"must reach the misattribution branch or it never exercises it",
			misattributed.Verdict, misattributed.DurableRankSum, misattributed.CriticalRankSum,
			firstOpenABVerdictDurable)
	}
	if err := firstOpenABMisattribution(misattributed); err == nil {
		t.Fatalf("a genuine misattribution (W+ %.1f, %d of %d pairs) produced no error — the assertion "+
			"has been weakened into one that cannot fail",
			misattributed.DurableRankSum, misattributed.DurableCheaperPairs, misattributed.UsablePairs)
	}

	// An ambiguous run is the caller's business (it skips), never a
	// misattribution: if the assertion fired on one, every noisy or quiet host
	// would be red for being noisy or quiet.
	tooFew := firstOpenABDecide(firstOpenABReproClean(3, +1, rand.New(rand.NewSource(firstOpenABReproSeed))))
	if tooFew.Verdict != firstOpenABVerdictAmbiguous {
		t.Fatalf("verdict = %q for 3 pairs, want the un-reachable-threshold ambiguity", tooFew.Verdict)
	}
	if err := firstOpenABMisattribution(tooFew); err != nil {
		t.Errorf("the ambiguity guard reported a misattribution on a too-short run: %v — an unmeasurable "+
			"run must skip, not fail", err)
	}

	// A noisy run that still points the RIGHT way must not be a misattribution
	// either: the two failure directions are asymmetric on purpose.
	clean := firstOpenABReproClean(firstOpenABReproPairs, -1, rand.New(rand.NewSource(firstOpenABReproSeed)))
	noisyButCorrect := firstOpenABDecide(firstOpenABReproSpikedAt(clean, firstOpenABReproSpikedPair))
	if err := firstOpenABMisattribution(noisyButCorrect); err != nil {
		t.Errorf("a noisy run pointing the correct way reported a misattribution: %v", err)
	}
}

// TestFirstOpenABRankStructureDecidesNotTheWrongWayOnly pins the property that
// makes the statistic rank-based rather than magnitude-based, and it is
// deliberately asymmetric.
//
// Upward outliers — a contended pair, the ONLY noise shape this host produces,
// since a slow neighbour can only ADD time — must not be able to manufacture a
// wrong-way verdict: a field in which the winning pairs are the tightly
// clustered ones and the losing pairs are the extreme ones WINS. That is what the
// old instrument could not do (it compared magnitudes, so one huge losing pair
// decided the run) and it is why the rank statistic tolerates the host.
//
// It does NOT hold symmetrically, and the honest statement is worth stating: the
// null distribution of the signed-rank statistic is symmetric (every subset of
// ranks is equally likely), so DOWNWARD outliers — a single pair made unusually
// FAST, which a contended device cannot produce but is not impossible — carry
// proportionally large ranks and can suppress a real signal into a skip. That
// direction fails SAFE: a skip is a logged non-verdict, never a red merge, which
// is the asymmetry the whole rework is built on.
func TestFirstOpenABRankStructureDecidesNotTheWrongWayOnly(t *testing.T) {
	// One EIGHTH of the pairs went the wrong way, and they are the EXTREME ones,
	// while the correct-way pairs are clustered and small. The fraction is a
	// normal neighbour's share of a run, not a majority: a field in which most
	// pairs disagree is un-measurable and SHOULD report ambiguity (the
	// decision-table case above pins that).
	field := make([]float64, firstOpenABReproPairs)
	for i := range field {
		if i%8 == 0 {
			field[i] = firstOpenABReproSpikeMs
			continue
		}
		field[i] = -2
	}
	got := firstOpenABDecide(field)
	if got.Verdict != firstOpenABVerdictRelaxed {
		t.Errorf("verdict = %q (W- %.1f, W+ %.1f, crit %.1f) for a field whose wrong-way pairs are the "+
			"extreme ones, want %q: the outlier must cost its own pair and no more",
			got.Verdict, got.RelaxedRankSum, got.DurableRankSum, got.CriticalRankSum,
			firstOpenABVerdictRelaxed)
	}
	// Its exact mirror — the same field with the arms' roles exchanged — must
	// reach the mirror verdict, which is the negative probe for THIS shape: an
	// instrument that simply ignored large differences would report ambiguity in
	// both directions and be useless.
	mirrored := firstOpenABDecide(negateAll(field))
	if mirrored.Verdict != firstOpenABVerdictDurable {
		t.Errorf("verdict = %q for the mirrored field, want %q — the statistic must depend on the ranks and "+
			"signs alone, so exchanging the arms exchanges the verdict",
			mirrored.Verdict, firstOpenABVerdictDurable)
	}
}

// negateAll flips a delta set's direction (the arms' roles swapped).
func negateAll(xs []float64) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = -x
	}
	return out
}

// TestFirstOpenABMedianIsTheLowerMiddle pins the median's shape, because the
// "was a sync visible at all?" floor reads it and the log line reports it: an
// even-count median that averaged a contended outlier in would let a noisy run
// clear the floor.
func TestFirstOpenABMedianIsTheLowerMiddle(t *testing.T) {
	cases := []struct {
		name    string
		samples []float64
		want    float64
		why     string
	}{
		{name: "odd-count-middle", samples: []float64{3, 1, 2}, want: 2, why: "the middle element"},
		{name: "even-count-lower-middle", samples: []float64{4, 1, 3, 2}, want: 2,
			why: "the lower of the two middles — the floor compares against the CHEAP half"},
		{name: "empty", samples: nil, want: 0, why: "no samples is not a cost; the caller skips on the floor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstOpenABMedian(tc.samples); got != tc.want {
				t.Errorf("median(%v) = %v, want %v — %s", tc.samples, got, tc.want, tc.why)
			}
		})
	}
	// The input must not be reordered by the call (the log line prints the arm
	// samples in the order they were measured, which is the run's chronology).
	in := []float64{9, 1, 5}
	_ = firstOpenABMedian(in)
	if in[0] != 9 {
		t.Errorf("firstOpenABMedian reordered its input: %v — the caller's sample order is the run's "+
			"chronology and the noisy-sample count depends on it staying", in)
	}
}

// TestFirstOpenABPairOrderIsBalancedAcrossTheRun pins the pairing DESIGN, which
// is a load-bearing part of the instrument rather than an implementation detail.
//
// Each pair must be palindromic in arm labels (so a drift across the pair's four
// builds contributes the same amount to each arm and cancels in the
// per-arm-averaged difference), and the two possible palindromes must alternate
// across pairs (so a short burst covering the pair's MIDDLE builds lands on each
// arm equally often instead of systematically on one).
func TestFirstOpenABPairOrderIsBalancedAcrossTheRun(t *testing.T) {
	if firstOpenABPairs%2 != 0 {
		t.Fatalf("firstOpenABPairs = %d is odd: the two pair orders cannot alternate evenly, so a short "+
			"burst would bias one arm", firstOpenABPairs)
	}
	totals := map[string]int{}
	for pair := 0; pair < firstOpenABPairs; pair++ {
		order := firstOpenABPairOrderFor(pair)
		if len(order) != firstOpenABBuildsPerPair {
			t.Fatalf("pair %d has %d builds, want %d", pair, len(order), firstOpenABBuildsPerPair)
		}
		within := map[string]int{}
		for i, arm := range order {
			within[arm]++
			totals[arm]++
			if mirror := order[len(order)-1-i]; mirror != arm {
				t.Errorf("pair %d position %d holds %q while its mirror holds %q — the order must be "+
					"palindromic for a drift to cancel", pair, i, arm, mirror)
			}
		}
		for _, arm := range []string{firstOpenABArmDurable, firstOpenABArmRelaxed} {
			if within[arm] != firstOpenABBuildsPerPair/2 {
				t.Errorf("pair %d has %d %q builds, want %d — an unbalanced pair cannot cancel drift",
					pair, within[arm], arm, firstOpenABBuildsPerPair/2)
			}
		}
	}
	for _, arm := range []string{firstOpenABArmDurable, firstOpenABArmRelaxed} {
		if totals[arm] != firstOpenABPairs*firstOpenABBuildsPerPair/2 {
			t.Errorf("%q is built %d times over the run, want %d — the two orders must alternate evenly, or "+
				"a burst covering the middle builds biases one arm",
				arm, totals[arm], firstOpenABPairs*firstOpenABBuildsPerPair/2)
		}
	}

	// The arithmetic consequence, on literal numbers: a drift that grows by d per
	// build must leave a pair's difference untouched, in BOTH orders.
	const driftPerBuild = 5.0
	for pair := 0; pair < 2; pair++ {
		order := firstOpenABPairOrderFor(pair)
		var durable, relaxed []float64
		for i, arm := range order {
			// The first build's cost is the arm's clean level (durable 20ms,
			// relaxed 18ms), plus this position's drift.
			level := firstOpenABReproBuildMs
			if arm == firstOpenABArmRelaxed {
				level = firstOpenABReproBuildMs * (1 - firstOpenABReproEffect)
			}
			build := level + float64(i)*driftPerBuild
			if arm == firstOpenABArmDurable {
				durable = append(durable, build)
				continue
			}
			relaxed = append(relaxed, build)
		}
		want := -firstOpenABReproEffect * firstOpenABReproBuildMs
		if got := firstOpenABPairDelta(durable, relaxed); got != want {
			t.Errorf("pair %d (%s): delta with a %.1fms/build drift = %+.4f, want the drift-free %+.4f — "+
				"the palindromic order and the per-arm averaging must cancel the drift exactly",
				pair, firstOpenABPairOrderName(pair), driftPerBuild, got, want)
		}
	}
}
