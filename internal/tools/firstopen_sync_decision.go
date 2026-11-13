package tools

// firstopen_sync_decision.go — the DECISION half of the first-open durability
// A/B (see firstopen_sync_test.go, devdoc/firstopen-ab-contention.md).
//
// The A/B asks one question — "is the durable `synchronous=` mode what makes a
// first open expensive on a block device?" — by timing the SAME schema build
// with the pragma as the only difference, and asserting a DIRECTION (the relaxed
// arm is the cheaper one). The direction is real; reading it off ONE wall-clock
// sample per arm is not a measurement, and that was the defect: on a shared
// block device a single `fsync` costs 0.7-1.3 s at /proc/pressure/io full > 90%
// (measured 2026-09-16) and tens of milliseconds when the device is quiet, so the
// spread BETWEEN two samples of the same arm exceeds the difference BETWEEN the
// arms. Under that noise the single-sample assertion went red on the merge/verify
// path for a reason that had nothing to do with the code under test.
//
// So the statistic lives HERE, as a pure function over a sample set. Its
// properties are the ones the in-test assertion needs, each one earned by a
// measurement recorded in devdoc/firstopen-ab-contention.md:
//
//   - PAIRED: only the within-pair difference is used, so a slow stretch of
//     device that covers a whole pair cancels instead of accumulating in one arm;
//   - ORDER-BALANCED pairs: within a pair the arms are built in a mirror-symmetric
//     order, and the order is flipped on alternate pairs, so a monotone drift
//     during the pair cancels EXACTLY while a short neighbour burst is as likely
//     to land on one arm as the other instead of biasing them;
//   - a signed-RANK statistic, not a mean: a contended pair carries a magnitude
//     many times the whole effect and would dominate any average, whereas under
//     ranks it is one observation with one rank — noise-tolerant by construction
//     rather than by hoping the outlier is small;
//   - the EXACT conditional null distribution, computed from the run's own ranks
//     with its ties (firstOpenABWilcoxonCritical): the burden of proof is the
//     host's measured noise in the same run, never a fixed millisecond gap that
//     is wrong on a busy host and useless on a quiet one;
//   - ASYMMETRIC consequences: an inconclusive run SKIPS and logs its
//     distribution — it is never a failure. Only a decisive wrong-way lead (a
//     tail probability below firstOpenABMaxFalseVerdictRate under that exact
//     null) is reported, because a merge must not be blocked by a host's noise,
//     while a genuine misattribution (the pragma stops controlling the cost) must
//     still be caught.
//
// Nothing here touches a device, so the whole decision table is unit-testable
// (firstopen_ab_repro_test.go) and the assertion itself is regression-tested.

import (
	"fmt"
	"math"
)

// The decision's closed verdict set. Named constants (Rule 1b): the test's
// assertion, the log line and the tests below must agree on exactly these
// spellings.
const (
	// firstOpenABVerdictRelaxed — the evidence says the relaxed arm is the
	// cheaper one: the sync really is the cost. This is the expected verdict.
	firstOpenABVerdictRelaxed = "relaxed-cheaper"
	// firstOpenABVerdictDurable — the evidence says the DURABLE arm is the
	// cheaper one, i.e. the mechanism may be misattributed.
	firstOpenABVerdictDurable = "durable-cheaper"
	// firstOpenABVerdictAmbiguous — the run cannot separate the arms: too few
	// usable pairs to reach the burden of proof, or a lead short of it. The
	// caller SKIPS on this and logs the distribution: a run whose noise is as
	// large as the effect has said nothing, and saying nothing is not a failure.
	firstOpenABVerdictAmbiguous = "ambiguous"
)

// firstOpenABMaxFalseVerdictRate is the A/B's burden of proof: the exact
// probability, under the null (the pragma does not control the cost), of the
// run's lead coming out at least this extreme by chance. A direction is asserted
// only when that probability is below this rate.
//
// CALIBRATION (devdoc/firstopen-ab-contention.md, ".evid4/calibration"): 1000+
// fresh-ledger first opens on this host measured a per-build log-noise σ ≈ 0.12
// (median |log ratio| 0.08, p90 0.20, ~5% of samples 3x that) against a real
// effect of 1-14% (median ~10%). Simulated against that measured noise, 32
// order-balanced pairs at this rate report the WRONG direction in 0.17% of runs,
// pass with ~62% probability at a 10% effect (~90% on a quiet device), skip the
// rest — and still report a genuine wrong-way effect of the same size ~62% of the
// time. The single-sample instrument it replaces inverted on ONE contended
// sample, which the same host produced in ~5% of samples.
//
// It is a PROBABILITY, deliberately: a measurement of a shared device cannot be
// made noise-proof, only noise-tolerant. The point is that the residual risk is
// ~1-in-600 and, when it materialises, the run reports its whole distribution —
// evidence to bring to a human — rather than a bare red on a coin flip.
const firstOpenABMaxFalseVerdictRate = 0.002

// firstOpenABDecision is the A/B's verdict plus the numbers it was reached from.
// The numbers are part of the result, never just a log line: a verdict whose
// evidence is not returned cannot be re-checked by its caller.
type firstOpenABDecision struct {
	// Verdict is one of the firstOpenABVerdict* constants.
	Verdict string
	// Pairs is how many paired differences were submitted.
	Pairs int
	// UsablePairs is how many of them carried an ordering (a pair whose arms came
	// out exactly equal is dropped: the signed-rank test has no rank for a zero).
	UsablePairs int
	// RelaxedCheaperPairs and DurableCheaperPairs count the pairs each way, for
	// the log line: a decisive rank lead resting on a near-even pair count is
	// visible as such.
	RelaxedCheaperPairs int
	DurableCheaperPairs int
	// RelaxedRankSum is the signed-rank statistic W- (weight of the pairs in
	// which the relaxed arm was cheaper); DurableRankSum is W+. One of them must
	// reach CriticalRankSum for a verdict to be asserted.
	RelaxedRankSum float64
	DurableRankSum float64
	// CriticalRankSum is the lead one side needed under this run's exact null — a
	// computed value, not a constant (see firstOpenABWilcoxonCritical). It is 0
	// when the run has too few usable pairs to reach the rate at all.
	CriticalRankSum float64
	// MeanDeltaMs is the mean paired difference (relaxed minus durable) in
	// milliseconds: NEGATIVE means the relaxed arm was cheaper. Reported beside
	// the ranks because it is what a human reads first, and the median beside it
	// so a run carried by one huge pair is visible as such.
	MeanDeltaMs   float64
	MedianDeltaMs float64
}

// firstOpenABDecide is the pure statistic: the paired differences in, a verdict
// out. `pairDeltas[i]` is one pair's relaxed-minus-durable difference in
// milliseconds (see firstOpenABPairDelta for how a pair is built).
//
// Only the paired DIFFERENCE is used — never a comparison of the arms' levels —
// because the absolute cost of a first open moves by 3x between runs on this host
// while the within-pair difference does not (devdoc table). The statistic is the
// signed rank: the pairs are ranked by the SIZE of their difference and each
// side's ranks are summed, so a contended pair contributes one rank like any
// other instead of a magnitude that swamps the effect.
func firstOpenABDecide(pairDeltas []float64) firstOpenABDecision {
	d := firstOpenABDecision{Pairs: len(pairDeltas)}
	if len(pairDeltas) == 0 {
		d.Verdict = firstOpenABVerdictAmbiguous
		return d
	}
	sum := 0.0
	for _, x := range pairDeltas {
		sum += x
	}
	d.MeanDeltaMs = sum / float64(len(pairDeltas))
	d.MedianDeltaMs = firstOpenABMedian(pairDeltas)

	// The signed-rank statistic over the pairs that carried an ordering.
	ranked := make([]float64, 0, len(pairDeltas))
	for _, x := range pairDeltas {
		if x == 0 {
			continue
		}
		ranked = append(ranked, x)
	}
	d.UsablePairs = len(ranked)
	ranks := firstOpenABMidranks(ranked)
	scaled := make([]int, 0, len(ranks))
	for i, x := range ranked {
		if x < 0 {
			d.RelaxedCheaperPairs++
			d.RelaxedRankSum += ranks[i]
			continue
		}
		d.DurableCheaperPairs++
		d.DurableRankSum += ranks[i]
	}
	// The exact null is a subset-sum distribution over these ranks; scaling them
	// by firstOpenABRankScale makes it integer arithmetic (midranks are halves).
	for _, r := range ranks {
		scaled = append(scaled, int(math.Round(r*firstOpenABRankScale)))
	}
	if crit, ok := firstOpenABWilcoxonCritical(scaled, d.UsablePairs); ok {
		d.CriticalRankSum = crit / firstOpenABRankScale
	}
	switch {
	case d.CriticalRankSum <= 0:
		// Too few usable pairs for ANY lead to reach the burden of proof. Nothing
		// can be asserted from that, which is exactly what the rate implies.
		d.Verdict = firstOpenABVerdictAmbiguous
	case d.DurableRankSum >= d.CriticalRankSum:
		d.Verdict = firstOpenABVerdictDurable
	case d.RelaxedRankSum >= d.CriticalRankSum:
		d.Verdict = firstOpenABVerdictRelaxed
	default:
		d.Verdict = firstOpenABVerdictAmbiguous
	}
	return d
}

// firstOpenABRankScale is the integer factor the midranks are carried at: a
// midrank is an integer when no ties exist and a HALF-integer when a tie group
// has an even count, so scaling by 2 makes the exact null an integer subset-sum
// problem with no floating-point comparison anywhere in the distribution.
const firstOpenABRankScale = 2.0

// firstOpenABMidranks returns the average rank of each |value| among the values
// (the standard Wilcoxon handling of ties: every member of a tie group carries
// the group's mean rank). Ties are ordinary here rather than pathological — a
// pair whose arms come out within a microsecond of each other is a real outcome
// on a quiet device, and collapsing them to distinct ranks would quietly inflate
// the statistic's apparent resolution.
func firstOpenABMidranks(values []float64) []float64 {
	n := len(values)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	for i := 1; i < n; i++ {
		for j := i; j > 0 && math.Abs(values[order[j]]) < math.Abs(values[order[j-1]]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	ranks := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && math.Abs(values[order[j+1]]) == math.Abs(values[order[i]]) {
			j++
		}
		meanRank := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			ranks[order[k]] = meanRank
		}
		i = j + 1
	}
	return ranks
}

// firstOpenABWilcoxonCritical returns the smallest rank lead whose probability
// under the EXACT conditional null distribution of the signed-rank statistic is
// at most firstOpenABMaxFalseVerdictRate, and whether such a lead exists.
//
// The null is the run's own ranks: under "the pragma does not control the cost",
// each pair is equally likely to have come out either way, so W+ is the sum of a
// uniformly random SUBSET of the observed ranks. That distribution is computed
// here by exact subset-sum counting — no normal approximation, because with a
// couple of dozen pairs and ties the approximation is exactly where a flake
// threshold would be wrong — and it absorbs the run's own noise: a run whose
// pairs disagree wildly has a broad null and therefore needs a bigger lead, while
// a run whose pairs all agree closely needs a small one.
//
// ranks must be the POSITIVE integer ranks (firstOpenABRankScale applied);
// usablePairs is their count. A false return means no lead can reach the rate
// with this many pairs — the caller reports ambiguity rather than a verdict.
func firstOpenABWilcoxonCritical(ranks []int, usablePairs int) (float64, bool) {
	if usablePairs == 0 || len(ranks) != usablePairs {
		return 0, false
	}
	total := 0
	for _, r := range ranks {
		total += r
	}
	counts := make([]float64, total+1)
	counts[0] = 1
	reachable := 0
	for _, r := range ranks {
		for s := reachable; s >= 0; s-- {
			if counts[s] > 0 {
				counts[s+r] += counts[s]
			}
		}
		reachable += r
	}
	// tail[w] = (number of subsets whose sum is at least w) / 2^usablePairs.
	subsets := math.Exp2(float64(usablePairs))
	acc := 0.0
	tail := make([]float64, total+1)
	for w := total; w >= 0; w-- {
		acc += counts[w]
		tail[w] = acc / subsets
	}
	for w := 0; w <= total; w++ {
		if tail[w] <= firstOpenABMaxFalseVerdictRate {
			return float64(w), true
		}
	}
	return 0, false
}

// firstOpenABMedian is the median of one sample set, reported beside the mean.
// The VERDICT never reads it (see firstOpenABDecide) — it exists to describe the
// distribution, and as the input to the "was a sync visible at all?" floor.
//
// On an even count it returns the LOWER middle: the floor comparison is a
// skip-if-small test, so the median must not be able to push a marginal run over
// it by averaging in a contended outlier.
func firstOpenABMedian(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]float64(nil), samples...)
	// Insertion sort: the arms are a handful of samples (firstOpenABPairs), so
	// this is cheaper than pulling in sort for one median and it keeps the
	// ordering visible.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[(len(sorted)-1)/2]
}

// firstOpenABMisattribution is the A/B's ASSERTION, extracted for the same reason
// the statistic was: an assertion that can only fire by touching a device cannot
// be regression-tested (Rule 5).
//
// It returns nil unless the verdict is the durable-cheaper one — an ambiguous run
// is the caller's business (it skips), and a relaxed verdict is the expected
// answer. An assertion that fired on an ambiguous run would make every quiet or
// busy host red for being quiet or busy, which is the class of bug this rework
// removes.
func firstOpenABMisattribution(d firstOpenABDecision) error {
	if d.Verdict != firstOpenABVerdictDurable {
		return nil
	}
	return fmt.Errorf("the durable arm (%s) was the cheaper one over %d of %d ordered pairs "+
		"(paired difference mean %+.3fms, median %+.3fms; the signed-rank lead reached %.1f against "+
		"the %.1f this run's own noise required, a false-lead probability under %v) — the sync is "+
		"not the cost this change removes, so the mechanism is misattributed",
		ledgerSyncMode, d.DurableCheaperPairs, d.UsablePairs, d.MeanDeltaMs, d.MedianDeltaMs,
		d.DurableRankSum, d.CriticalRankSum, firstOpenABMaxFalseVerdictRate)
}
