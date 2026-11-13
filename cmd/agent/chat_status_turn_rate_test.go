package main

import "testing"

// The turn reading's own regression guard (task 01a1c312).
//
// THE DEFECT. The row printed `Δ100%` on a turn whose measured rate was 98.4%,
// because the two increments the ratio was taken over were read off two counters
// whose credit can land at different instants on this transport: the read side
// arrives with the response's usage block, and the miss side is a SUM
// (cache_creation + input) that is only as fresh as its last term. Any instant at
// which the denominator had not moved produced read/(read+0) == 100% — the reading
// degraded into "has the miss counter moved since the snapshot", which is what the
// operator observed as a binary indicator.
//
// THE FIX, and what these cases pin: the write side is DERIVED from the prompt-side
// total taken at the SAME snapshot instant — prompt Δ − read Δ — so the reading is
// a ratio of two increments of one quantity observed once, instead of the
// difference of two counters observed whenever each happened to be credited.

// chatTurnRateProbe is one turn's state in the row's own terms: the four counters
// as they stood at the turn's SNAPSHOT (cold) and as they stand at the reading
// (live). statusOps carries both, exactly as refreshOpsLocked merges them.
type chatTurnRateProbe struct {
	name string
	cold [3]int // prompt, read, miss at beginTurn
	live [3]int // prompt, read, miss at the reading
	want string // the Δ the row must draw — "" is the honest no-reading
	why  string
}

func (p chatTurnRateProbe) ops() statusOps {
	return statusOps{
		cachePrompt:       p.live[0],
		cacheRead:         p.live[1],
		cacheWrite:        p.live[2],
		cachePromptAtTurn: p.cold[0],
		cacheReadAtTurn:   p.cold[1],
		cacheWriteAtTurn:  p.cold[2],
		cacheTurnKnown:    true,
	}
}

// TestStatusCacheTurnRateIsOneInstantOneQuantity is the minimal reproduction of the
// reported symptom plus the arithmetic the fix must satisfy.
func TestStatusCacheTurnRateIsOneInstantOneQuantity(t *testing.T) {
	// THE REAL NUMBERS are the measured ones from the session that commissioned this
	// row (8adaebe0, its session.jsonl `request` rows, consecutive totals): the turn
	// served 71168 of its 72303 prompt tokens and wrote the other 1135 — 98.43%, the
	// reading the row printed as `Δ100%`.
	const (
		realColdPrompt, realColdRead, realColdMiss = 210917, 146432, 64485
		realPrompt, realRead, realMiss             = 283220, 217600, 65620
	)
	cases := []chatTurnRateProbe{
		{
			name: "the measured turn (98.43% printed as 100%)",
			cold: [3]int{realColdPrompt, realColdRead, realColdMiss},
			live: [3]int{realPrompt, realRead, realMiss},
			want: "98%",
			why:  "71168 served of 72303 prompt tokens is 98.43%, and the whole-percent form says 98",
		},
		{
			// THE OLD BEHAVIOUR'S OWN CONSTRUCTION, stated as a case in its own right:
			// the read side has been credited for this turn and the miss counter has
			// NOT moved since the snapshot (a miss that has its input term but not yet
			// its cache_creation term, or the reverse — the sum arrives last). The
			// pre-fix arithmetic saw read=60800 / write=0 and drew 100%.
			name: "the miss counter has not caught up with the read side",
			cold: [3]int{100000, 20000, 80000},
			live: [3]int{165458, 80800, 80000},
			want: "93%",
			why:  "the prompt side moved by 65458 while the read side moved by 60800, so the write side is 4658 whatever the stale miss counter says: 60800/65458 = 92.9%",
		},
		{
			name: "a turn served entirely from the cache",
			cold: [3]int{100000, 20000, 80000},
			live: [3]int{110000, 30000, 80000},
			want: "100%",
			why:  "prompt Δ == read Δ: nothing was written this turn, so 100% is the measurement",
		},
		{
			name: "a turn written entirely (a cold prefix)",
			cold: [3]int{100000, 20000, 80000},
			live: [3]int{140000, 20000, 120000},
			want: "0%",
			why:  "read Δ is zero while the prompt side moved, so the whole turn was a write",
		},
		{
			name: "the counters did not move at all",
			cold: [3]int{100000, 20000, 80000},
			live: [3]int{100000, 20000, 80000},
			want: "",
			why:  "zero sample is NOT 100%: no increment on either side, so there is no rate (Rule 15b)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := statusCacheTurnHitRate(c.ops())
			if got != c.want {
				t.Fatalf("Δ reading = %q, want %q — %s", got, c.want, c.why)
			}
		})
	}
}

// TestStatusCacheTurnRateIsNotABinaryIndicator pins the property the symptom is
// really about: over a run of turns the reading must VARY, and it must equal the
// served share of the prompt side rather than collapsing onto the two values a
// one-sided delta can produce.
func TestStatusCacheTurnRateIsNotABinaryIndicator(t *testing.T) {
	// (promptΔ, readΔ) pairs taken from the measured session's consecutive totals,
	// with the miss counter deliberately frozen at its snapshot value — the stale
	// construction that made the pre-fix reading a constant.
	turns := []struct {
		promptDelta, readDelta int
		want                   string
	}{
		{70210, 69376, "99%"},
		{71265, 70144, "98%"},
		{72303, 71168, "98%"},
		{72591, 72192, "99%"},
		{144862, 141184, "97%"},
	}
	seen := map[string]bool{}
	for i, turn := range turns {
		ops := statusOps{
			cachePromptAtTurn: 0, cacheReadAtTurn: 0, cacheWriteAtTurn: 0,
			cachePrompt:    turn.promptDelta,
			cacheRead:      turn.readDelta,
			cacheWrite:     0, // the stale denominator, frozen at the snapshot's value
			cacheTurnKnown: true,
		}
		got := statusCacheTurnHitRate(ops)
		if got != turn.want {
			t.Fatalf("turn %d: Δ = %q, want %q (promptΔ=%d readΔ=%d)", i, got, turn.want, turn.promptDelta, turn.readDelta)
		}
		seen[got] = true
	}
	if len(seen) < 3 {
		t.Fatalf("the Δ reading took only %d distinct values over %d turns (%v): it is still a binary "+
			"indicator of whether one counter moved, not a measurement", len(seen), len(turns), seen)
	}
}

// TestStatusCacheTurnRateNeedsABaseline keeps the honesty clause: with no snapshot
// there is no delta, and the row prints the cumulative reading alone.
func TestStatusCacheTurnRateNeedsABaseline(t *testing.T) {
	ops := statusOps{cachePrompt: 900, cacheRead: 800, cacheWrite: 100}
	if got := statusCacheTurnHitRate(ops); got != "" {
		t.Fatalf("Δ = %q with cacheTurnKnown=false, want the empty reading (Rule 15b)", got)
	}
	if _, ok := statusCacheTurnRatio(ops); ok {
		t.Fatalf("statusCacheTurnRatio reported a measurement with no baseline")
	}
}

// TestStatusCacheTurnRateFallsBackWithoutAPromptSide states the ONE named
// degradation: a statusOps that never set the prompt side is not a turn the row's
// own source produced, and there the miss counter's own delta is the only write
// side there is. Behaviour is then the pre-fix value — stated, not silent.
func TestStatusCacheTurnRateFallsBackWithoutAPromptSide(t *testing.T) {
	ops := statusOps{
		cacheRead: 4000, cacheWrite: 1000,
		cacheReadAtTurn: 1000, cacheWriteAtTurn: 400,
		cacheTurnKnown: true,
	}
	ratio, ok := statusCacheTurnRatio(ops)
	if !ok {
		t.Fatalf("no reading from the counters alone, want the miss-delta fallback")
	}
	want := 3000.0 / 3600.0
	if diff := ratio - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("fallback ratio = %v, want %v (read Δ over read Δ + miss Δ)", ratio, want)
	}
}

// TestStatusCacheTurnRateAgreesWithThePromptSide pins the invariant the derivation
// rests on, on values the transport really produces: prompt Δ − read Δ equals the
// miss counter's own delta whenever the two sides were credited together. A reader
// of the fix can then see the same-instant arithmetic is not a new definition of
// the miss — it is the same number, taken from the counter that cannot lag its own
// response.
func TestStatusCacheTurnRateAgreesWithThePromptSide(t *testing.T) {
	rows := [][3]int{
		// prompt, read, miss — consecutive session totals (session 8adaebe0).
		{69442, 6912, 62530},
		{139652, 76288, 63364},
		{283220, 217600, 65620},
		{502386, 434816, 67570},
	}
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		promptDelta := cur[0] - prev[0]
		readDelta := cur[1] - prev[1]
		missDelta := cur[2] - prev[2]
		if got := promptDelta - readDelta; got != missDelta {
			t.Fatalf("row %d: prompt Δ − read Δ = %d, miss Δ = %d — the prompt side and the miss counter "+
				"disagree, so the derivation in statusCacheTurnRatio is not the same quantity", i, got, missDelta)
		}
		ops := statusOps{
			cachePrompt: cur[0], cacheRead: cur[1], cacheWrite: cur[2],
			cachePromptAtTurn: prev[0], cacheReadAtTurn: prev[1], cacheWriteAtTurn: prev[2],
			cacheTurnKnown: true,
		}
		fromPrompt, ok1 := statusCacheTurnRatio(ops)
		// The same turn read off the pair alone (the pre-fix construction), which is
		// the SAME number here because both sides were credited together.
		fromPair, ok2 := statusHitRateRatio(readDelta, missDelta)
		if !ok1 || !ok2 {
			t.Fatalf("row %d: a measured turn reported no ratio", i)
		}
		if diff := fromPrompt - fromPair; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("row %d: prompt-side ratio %v != pair ratio %v — the two definitions must agree when "+
				"the counters land together", i, fromPrompt, fromPair)
		}
	}
}
