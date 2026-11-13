package requestusage

import "testing"

// reading builds one row; hit+miss == prompt so every fixture satisfies the
// protocol's split (the identity the writer produces).
func reading(prompt, comp int) Reading {
	return Reading{Prompt: prompt, CacheHit: prompt, CacheMiss: 0, Completion: comp}
}

// THE DEFECT, AS A TEST (epic 01a195b0): a chat log's three rows are the
// session's RUNNING TOTALS, and the number the surfaces printed was their SUM
// (219,191). The counted value is the last reading — 109,705, which is also what
// the session's raw HTTP log sums to.
func TestCumulativeRowsCountTheLastReadingNotTheirSum(t *testing.T) {
	rows := []Reading{reading(36455, 87), reading(73031, 175), reading(109705, 262)}
	base, reason := Classify(rows, make([]bool, len(rows)), nil)
	if base != BaseCumulative {
		t.Fatalf("Classify = %q (%s), want %q: a monotone column with no recorded "+
			"totals is a running total", base, reason, BaseCumulative)
	}
	tagged := []Tagged{{Reading: rows[0], Base: base}, {Reading: rows[1], Base: base}, {Reading: rows[2], Base: base}}
	got := Counted(tagged)
	if got.Prompt != 109705 || got.Completion != 262 {
		t.Fatalf("Counted = %+v, want prompt 109705 completion 262 (the last reading)", got)
	}
	if got.Segments != 1 {
		t.Fatalf("Segments = %d, want 1", got.Segments)
	}
}

// A RESUMED session restarts its counters, so the counted value is the SUM of
// each segment's LAST reading — neither the session's last reading (which would
// report only the final run) nor the column sum (which would report every run's
// running totals added together). The three numbers are the measured readings of
// chat session 66243525-16ab-4ce7-9133-84d74c539840.
func TestSegmentedCumulativeCountsEachRunsLastReading(t *testing.T) {
	rows := []Reading{
		reading(44155810, 133752), // run 1
		reading(142860, 79),       // run 2 restarts LOW
		reading(432152, 474),
		reading(40741990, 157742), // run 2's last reading
		reading(137568, 929),      // run 3 restarts LOW
		reading(310935011, 1153659),
	}
	base := BaseCumulative
	tagged := make([]Tagged, len(rows))
	for i, r := range rows {
		tagged[i] = Tagged{Reading: r, Base: base, Seg: segOf(i)}
	}
	got := Counted(tagged)
	const wantPrompt = 44155810 + 40741990 + 310935011
	const wantComp = 133752 + 157742 + 1153659
	if got.Prompt != wantPrompt || got.Completion != wantComp {
		t.Fatalf("Counted = prompt %d completion %d, want prompt %d completion %d",
			got.Prompt, got.Completion, wantPrompt, wantComp)
	}
	if got.Segments != 3 {
		t.Fatalf("Segments = %d, want 3 (the log holds three runs)", got.Segments)
	}
	// AND THE FALL ALONE FINDS THE SAME THREE RUNS when no segment id is
	// recorded — every log written before the marker.
	for i := range tagged {
		tagged[i].Seg = 0
	}
	got = Counted(tagged)
	if got.Prompt != wantPrompt || got.Segments != 3 {
		t.Fatalf("without a segment id: prompt %d segments %d, want %d / 3 "+
			"(a cumulative reading that moves backwards is a new run)", got.Prompt, got.Segments, wantPrompt)
	}
}

// segOf numbers the fixture's runs: rows 0 / 1-3 / 4-5 (the shape the real log
// has, where each run opens with a fresh CostTracker).
func segOf(i int) int {
	switch {
	case i == 0:
		return 0
	case i <= 3:
		return 1
	}
	return 2
}

// A TASK LOG IS UNTOUCHED. Its rows are increments, its increments sum to the
// totals its own run recorded, and the counted value is that same sum.
func TestDeltaRowsAreSummedAndProvenByTheRecordedTotals(t *testing.T) {
	rows := []Reading{reading(40000, 100), reading(12000, 40), reading(65000, 300)}
	ends := 40000 + 100 + 12000 + 40 + 65000 + 300
	base, reason := Classify(rows, make([]bool, len(rows)), &ends)
	if base != BaseDelta {
		t.Fatalf("Classify = %q (%s), want %q: the rows sum to the recorded "+
			"session total", base, reason, BaseDelta)
	}
	tagged := make([]Tagged, len(rows))
	for i, r := range rows {
		tagged[i] = Tagged{Reading: r, Base: base}
	}
	got := Counted(tagged)
	if got.Prompt != 117000 || got.Completion != 440 {
		t.Fatalf("Counted = %+v, want the column sum 117000 / 440", got)
	}
	if got.Segments != 0 {
		t.Fatalf("Segments = %d, want 0 (a delta log has no running counter)", got.Segments)
	}
}

// A FALL INSIDE A RUN is evidence of increments — the criterion that identifies
// every one of the profile's multi-row task logs, and which must NOT be fooled
// by a resumed session's restart (that restart sits at a run boundary, and
// boundary marks are skipped).
func TestFallInsideARunClassifiesAsDeltaAndABoundaryFallDoesNot(t *testing.T) {
	rows := []Reading{reading(40000, 100), reading(12000, 40), reading(65000, 300)}
	none := make([]bool, len(rows))
	if base, _ := Classify(rows, none, nil); base != BaseDelta {
		t.Fatalf("fall inside a run: Classify = %q, want %q", base, BaseDelta)
	}
	// The same three readings, but the falls sit where a session_start sat:
	// that is a resume, not evidence of increments.
	boundary := []bool{false, true, true}
	if base, _ := Classify(rows, boundary, nil); base != BaseCumulative {
		t.Fatalf("fall at a run boundary: Classify = %q, want %q", base, BaseCumulative)
	}
}

// A ROW WHOSE QUANTITY IS NEVER ESTABLISHED IS COUNTED AND NAMED, never folded
// in on a guess (Rule 15).
func TestUnknownRowsAreNotFoldedAndAreCounted(t *testing.T) {
	tagged := []Tagged{
		{Reading: reading(1000, 10), Base: BaseUnknown},
		{Reading: reading(500, 5), Base: BaseDelta},
	}
	got := Counted(tagged)
	if got.Prompt != 500 || got.Completion != 5 {
		t.Fatalf("Counted = %+v, want only the classified row (500 / 5)", got)
	}
	if got.Unknown != 1 {
		t.Fatalf("Unknown = %d, want 1", got.Unknown)
	}
}

// THE PER-REQUEST FORM. On a cumulative log the recorded reading is a running
// total; differencing inside a segment recovers the request's own usage, and the
// deltas sum to the counted total. The first row of a segment is its own usage
// (a fresh run starts from zero), and a task log's rows pass through untouched.
func TestDeltasRecoverEachRequestsOwnUsage(t *testing.T) {
	rows := []Reading{
		reading(1000, 10), // run 1 opens
		reading(2500, 30), // +1500 / +20
		reading(300, 5),   // run 2 opens (restart)
	}
	tagged := []Tagged{
		{Reading: rows[0], Base: BaseCumulative, Seg: 0},
		{Reading: rows[1], Base: BaseCumulative, Seg: 0},
		{Reading: rows[2], Base: BaseCumulative, Seg: 1},
	}
	deltas, total := Deltas(tagged)
	want := []Reading{reading(1000, 10), reading(1500, 20), reading(300, 5)}
	for i := range want {
		if deltas[i] != want[i] {
			t.Fatalf("deltas[%d] = %+v, want %+v", i, deltas[i], want[i])
		}
	}
	sumDeltas := 0
	for _, d := range deltas {
		sumDeltas += d.Prompt
	}
	if sumDeltas != total.Prompt {
		t.Fatalf("the deltas sum to %d but the counted total is %d — the two "+
			"surfaces would disagree about the same log", sumDeltas, total.Prompt)
	}

	// A delta log is identity.
	plain := []Tagged{
		{Reading: reading(700, 7), Base: BaseDelta},
		{Reading: reading(900, 9), Base: BaseDelta},
	}
	deltas, total = Deltas(plain)
	if deltas[0] != reading(700, 7) || deltas[1] != reading(900, 9) || total.Prompt != 1600 {
		t.Fatalf("a delta log must pass through untouched: deltas %+v total %d", deltas, total.Prompt)
	}
}

// A log with no `request` row has no quantity to classify, and says so instead
// of claiming one.
func TestEmptyLogIsUnknown(t *testing.T) {
	base, reason := Classify(nil, nil, nil)
	if base != BaseUnknown || reason != ClassifyReasonNoRows {
		t.Fatalf("Classify(nil) = %q / %q, want %q / %q", base, reason, BaseUnknown, ClassifyReasonNoRows)
	}
}
