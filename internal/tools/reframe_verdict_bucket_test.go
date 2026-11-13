package tools

import (
	"context"
	"github.com/nick/gopi/internal/heavytest"

	"path/filepath"

	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverdict"
)

// ---------------------------------------------------------------------------
// Regression guard: an out-of-vocabulary verdict token must land in the UNKNOWN
// render bucket, and NO counted row may be invisible in the distribution table.
//
// Defect (found 2026-09-11 by the coverage-pilot review of
// internal/tools/reframe.go): parseReframeVerdict only fell back to UNKNOWN when
// the "verdict=" FIELD was absent — a token that WAS present but outside the
// vocabulary became its own dist key. Such a key is counted by the header total
// ("N reframe record(s)") because every row increments total, yet the render
// loop iterated only append(taskverdict.All(), UNKNOWN) — so the row was
// silently absent from the table: total != sum(table counts) (Rule 15, a silent
// drop of a recorded fact).
//
// Two real producers, both reproduced here:
//   - NEEDS_RESEARCH: a RETIRED verdict. Rows written before the retirement
//     (taskverdict's package doc: "NEEDS_RESEARCH was a POC artifact") are
//     append-only and stay in task_events forever.
//   - BANANA: a corrupt/foreign token (a hand-edited row, a writer from another
//     branch/binary) — the trail is plain text and never re-validated on write.
//
// Both are seeded through reframeEventDetail, the production renderer, so the
// detail strings under test are exactly the ones a real writer produces.
// ---------------------------------------------------------------------------

// reframeRetiredVerdict is a verdict that ONCE was in the wire vocabulary and is
// not any more. It appears here as a literal because that is what the historical
// rows on disk contain — the point of the test is that the reader recognises a
// token it no longer owns.
const reframeRetiredVerdict = "NEEDS_RESEARCH"

// reframeCorruptVerdict is a token that never was a verdict.
const reframeCorruptVerdict = "BANANA"

// TestReframeVerdicts_OutOfVocabularyLandsInUnknownEndToEnd is the minimal
// repro: seed a retired verdict row, a corrupt verdict row and two valid rows,
// then assert (a) the UNKNOWN row exists with the right count and payload items,
// (b) sum(table counts) == header total, and (c) no out-of-vocabulary token
// became a table row of its own.
func TestReframeVerdicts_OutOfVocabularyLandsInUnknownEndToEnd(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Valid rows go through the production writer (the full wire path).
	if err := TaskReframeRecord("t-doable", taskverdict.Doable, 0, ""); err != nil {
		t.Fatalf("seed DOABLE: %v", err)
	}
	if err := TaskReframeRecord("t-toobig", taskverdict.TooBig, 3, ""); err != nil {
		t.Fatalf("seed TOO_BIG: %v", err)
	}
	// The out-of-vocabulary rows CANNOT go through TaskReframeRecord (it
	// correctly rejects them at the wire gate) — they are written the way the
	// historical/corrupt writer did: straight into the append-only trail.
	taskEventWrite(ctx, db, "t-retired", ReframeEventName, reframeEventDetail(reframeRetiredVerdict, 0, ""))
	taskEventWrite(ctx, db, "t-corrupt", ReframeEventName, reframeEventDetail(reframeCorruptVerdict, 5, ""))

	out, err := TaskReframeStats()
	if err != nil {
		t.Fatalf("TaskReframeStats: %v", err)
	}

	total, dist := reframeParseReport(t, out)
	// One record per seeded row: 2 valid + 2 out-of-vocabulary.
	const seededTotal = 4
	if total != seededTotal {
		t.Fatalf("header total = %d, want %d (every row is counted):\n%s", total, seededTotal, out)
	}

	// (c) an out-of-vocabulary token must never be its own distribution row —
	// that is the defect's fingerprint (the token appears as a table key).
	for _, bogus := range []string{reframeRetiredVerdict, reframeCorruptVerdict} {
		if _, ok := dist[bogus]; ok {
			t.Errorf("%q became its own table row — it must be bucketed as %s:\n%s",
				bogus, reframeVerdictUnknown, out)
		}
	}

	// (a) both out-of-vocabulary rows land in the UNKNOWN bucket, payload items
	// included (0 from the retired row + 5 from the corrupt one).
	unk, ok := dist[reframeVerdictUnknown]
	if !ok {
		t.Fatalf("no %s row in the table — the out-of-vocabulary rows were dropped:\n%s",
			reframeVerdictUnknown, out)
	}
	const wantUnknownCount, wantUnknownItems = 2, 5
	if unk.count != wantUnknownCount || unk.items != wantUnknownItems {
		t.Errorf("%s row = count %d items %d, want count %d items %d:\n%s",
			reframeVerdictUnknown, unk.count, unk.items, wantUnknownCount, wantUnknownItems, out)
	}
	// The valid rows still render under their own verdicts.
	if got := dist[taskverdict.Doable]; got.count != 1 || got.items != 0 {
		t.Errorf("%s row = %+v, want count 1 items 0:\n%s", taskverdict.Doable, got, out)
	}
	if got := dist[taskverdict.TooBig]; got.count != 1 || got.items != 3 {
		t.Errorf("%s row = %+v, want count 1 items 3:\n%s", taskverdict.TooBig, got, out)
	}

	// (b) THE INVARIANT: the table accounts for every counted record.
	if sum := reframeDistSum(dist); sum != total {
		t.Errorf("sum(table counts) = %d != header total %d — a counted row is table-invisible:\n%s",
			sum, total, out)
	}

	// The bucket must stay RENDER-ONLY: the tempting wrong fix is to add
	// UNKNOWN to the wire vocabulary. That would widen the harness gate
	// (taskverdict.Valid is the write gate at internal/tools/reframe.go's
	// TaskReframeRecord and internal/agent/reframe.go) and let a fabricated
	// verdict through the LLM-facing schema.
	if taskverdict.Valid(reframeVerdictUnknown) {
		t.Errorf("taskverdict.Valid(%q) = true — the UNKNOWN bucket is render-only, never a wire verdict", reframeVerdictUnknown)
	}
	for _, v := range taskverdict.All() {
		if v == reframeVerdictUnknown {
			t.Errorf("taskverdict.All() now carries %q — the vocabulary was widened to hide a reader bug", reframeVerdictUnknown)
		}
	}
}

// TestReframeVerdictTableOrder_NoCountedKeyIsInvisible pins the render-loop
// hardening on its own. parseReframeVerdict makes a residual key unreachable
// through the DB path today, but the invariant "no observed dist key is left out
// of the table" must not depend on that: the order helper is given a dist map
// containing a key outside the vocabulary and must still return it (sorted,
// after the vocabulary order) so any future reader cannot silently hide a row.
func TestReframeVerdictTableOrder_NoCountedKeyIsInvisible(t *testing.T) {
	dist := map[string]int{
		taskverdict.Doable:     1,
		reframeCorruptVerdict:  2, // a key outside the vocabulary ∪ UNKNOWN
		reframeVerdictUnknown:  3,
		taskverdict.Impossible: 0, // never observed → never rendered
	}
	got := reframeVerdictTableOrder(dist)

	// Vocabulary order first (taskverdict.All() … then the UNKNOWN bucket).
	wantPrefix := []string{taskverdict.Doable, reframeVerdictUnknown}
	if len(got) < len(wantPrefix) {
		t.Fatalf("table order %v is missing vocabulary rows", got)
	}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Errorf("table order[%d] = %q, want %q (vocabulary order first): %v", i, got[i], want, got)
		}
	}
	// Deterministic residual tail.
	if want := []string{taskverdict.Doable, reframeVerdictUnknown, reframeCorruptVerdict}; !equalStrings(got, want) {
		t.Errorf("table order = %v, want %v (observed keys always render)", got, want)
	}

	// Stable across map iteration order (Go randomises it): run it repeatedly.
	for i := 0; i < 20; i++ {
		if !equalStrings(reframeVerdictTableOrder(dist), got) {
			t.Fatalf("table order is not deterministic: %v vs %v", reframeVerdictTableOrder(dist), got)
		}
	}

	// A zero/empty dist renders nothing.
	if got := reframeVerdictTableOrder(map[string]int{}); len(got) != 0 {
		t.Errorf("empty dist rendered %v, want nothing", got)
	}
}

// --- helpers ---------------------------------------------------------------

type reframeRow struct{ count, items int }

// reframeParseReport reads a `task verdicts` render back: the header total and
// the VERDICT/COUNT/ITEMS table rows, keyed by verdict.
func reframeParseReport(t *testing.T, out string) (int, map[string]reframeRow) {
	t.Helper()
	dist := map[string]reframeRow{}
	total := -1
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, " ")
		f := strings.Fields(line)
		if len(f) == 3 && f[0] != "VERDICT" {
			n, err := strconv.Atoi(f[1])
			if err != nil {
				continue
			}
			it, err := strconv.Atoi(f[2])
			if err != nil {
				t.Fatalf("items column of %q is not a number:\n%s", line, out)
			}
			dist[f[0]] = reframeRow{count: n, items: it}
			continue
		}
		if len(f) > 0 && f[0] != "" && total < 0 {
			if n, err := strconv.Atoi(f[0]); err == nil {
				total = n
			}
		}
	}
	if total < 0 {
		t.Fatalf("no header total in the render:\n%s", out)
	}
	return total, dist
}

// reframeDistSum is the table's own accounting of the records it shows.
func reframeDistSum(dist map[string]reframeRow) int {
	sum := 0
	for _, r := range dist {
		sum += r.count
	}
	return sum
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestReframeVerdicts_GoldenRenderIsStable keeps the rest of the existing
// report contract pinned while the render loop changes: verdict order follows
// the vocabulary, and the per-task section still lists every seeded task.
func TestReframeVerdicts_GoldenRenderIsStable(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	if err := TaskReframeRecord("t-1", taskverdict.Impossible, 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := TaskReframeRecord("t-1", taskverdict.Doable, 0, ""); err != nil {
		t.Fatal(err)
	}
	out, err := TaskReframeStats()
	if err != nil {
		t.Fatalf("TaskReframeStats: %v", err)
	}
	total, dist := reframeParseReport(t, out)
	if total != 2 || reframeDistSum(dist) != total {
		t.Fatalf("total=%d dist=%v out of balance:\n%s", total, dist, out)
	}
	want := []string{taskverdict.Doable, taskverdict.Impossible}
	var got []string
	for _, v := range []string{taskverdict.Doable, taskverdict.TooBig, taskverdict.Impossible} {
		if _, ok := dist[v]; ok {
			got = append(got, v)
		}
	}
	if !equalStrings(got, want) {
		t.Errorf("rendered verdicts = %v, want %v (vocabulary order):\n%s", got, want, out)
	}
	if !strings.Contains(out, "t-1 ×2") {
		t.Errorf("per-task section lost the repeated reframe:\n%s", out)
	}
}
