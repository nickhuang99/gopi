package standardgate

// gapdoc_contract_gate_test.go — the check that makes 阶段 3's gap documents a
// MACHINE-CHECKED shape instead of a prose convention.
//
// THE RED ARM IS THE POINT. Reading a clean corpus and saying "clean" is the
// half that can be true by accident, so every arm below PLANTS one defect into
// an otherwise-valid document and requires the gate to name it — the 2026-11-06
// defect first of all: a row whose 我方 level is `none` listed under the bucket
// that reads 「我方有、对方 none」. The planting is done by MUTATION, never by
// hand-writing a "bad document": a hand-written one drifts from the contract the
// day the contract changes, and then the arm passes for the wrong reason.
//
// THE GREEN ARM IS TWO-SIDED. It runs the SAME verdict function over the real
// `devdoc/competitor/gap-*.md` files AND over a synthetic document that must be
// clean, and it refuses a scan that judged nothing (a gate that read no file is
// indistinguishable from a clean corpus — the vacuum shape Rule 36 and Rule 38
// both name).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gapFixtureLevels is the level table the synthetic document is built from. It
// deliberately covers every verdict the function can produce, so the fixture
// exercises all six buckets rather than the two a real competitor happens to
// hit: 敌有我无, 敌强我弱, 平手, 敌强我弱, 不可比 (via unknown), 我强敌弱,
// 我有敌无, plus the both-absent case.
var gapFixtureLevels = []struct {
	our, opp string
}{
	{levelNone, levelPartial},    // 敌有我无
	{levelPartial, levelFull},    // 敌强我弱
	{levelNone, levelUnknown},    // 不可比 (unknown side)
	{levelFull, levelPartial},    // 我强敌弱 — the pair the historic four buckets had no home for
	{levelPartial, levelNone},    // 我有敌无
	{levelFull, levelUnknown},    // 不可比
	{levelPartial, levelPartial}, // 平手
	{levelFull, levelFull},       // 平手
	{levelNone, levelNone},       // 平手 (both absent)
}

// gapBuildDoc renders a contract-conforming document from a level table: one
// main-table row per dimension (D01..D17) and a bucket view listing each
// dimension under the bucket the FUNCTION computes for it — the document is
// derived from the same predicate the gate checks with, so it is correct by
// construction and can only fail when the gate disagrees with its own rule.
func gapBuildDoc(t *testing.T, levels []struct{ our, opp string }) string {
	t.Helper()
	if len(levels) != gapDocDimCount {
		t.Fatalf("fixture level table must carry %d entries, has %d", gapDocDimCount, len(levels))
	}
	buckets := make(map[string][]string, len(gapVerdicts))
	var rows strings.Builder
	rows.WriteString("| 维度 | 我方等级 | 对方等级 | 判定 | 证据 |\n")
	rows.WriteString("|---|---|---|---|---|\n")
	for i, lv := range levels {
		dim := gapDimID(i + 1)
		verdict, err := gapDocVerdictFor(lv.our, lv.opp)
		if err != nil {
			t.Fatalf("fixture level pair %d is not legal: %v", i+1, err)
		}
		buckets[verdict] = append(buckets[verdict], dim)
		fmt.Fprintf(&rows, "| %s | %s | %s | %s | 合成证据 %s |\n", dim, lv.our, lv.opp, verdict, dim)
	}
	var out strings.Builder
	out.WriteString("<!-- doc-class: historical -->\n\n# 合成差距文档（测试夹具）\n\n")
	out.WriteString("## 逐维判定\n\n")
	out.WriteString(rows.String())
	out.WriteString("\n## " + gapBucketViewHeadingCue + "\n\n")
	out.WriteString("| 桶 | 维度清单 |\n|---|---|\n")
	for _, v := range gapVerdicts {
		list := buckets[v]
		if len(list) == 0 {
			fmt.Fprintf(&out, "| %s | %s |\n", v, gapBucketEmptyMark)
			continue
		}
		fmt.Fprintf(&out, "| %s | %s |\n", v, strings.Join(list, gapDimListSep))
	}
	return out.String()
}

// gapSeventeenLevels returns a legal 17-entry level table by cycling the
// fixture pairs — every verdict appears at least once.
func gapSeventeenLevels() []struct{ our, opp string } {
	out := make([]struct{ our, opp string }, 0, gapDocDimCount)
	for i := 0; i < gapDocDimCount; i++ {
		out = append(out, gapFixtureLevels[i%len(gapFixtureLevels)])
	}
	return out
}

// gapRequireNames fails unless at least one problem line carries every needle.
// Naming is the contract of a refusal (Rule 15): a gate that fails without
// naming the dimension leaves the author to diff the document by hand.
func gapRequireNames(t *testing.T, problems []string, needles ...string) {
	t.Helper()
	if len(problems) == 0 {
		t.Fatalf("RED PROOF FAILED: no problem was reported at all — the gate is vacuous")
	}
	joined := strings.Join(problems, "\n")
	for _, n := range needles {
		if !strings.Contains(joined, n) {
			t.Errorf("the refusal does not name %q; it said:\n%s", n, joined)
		}
	}
}

// TestGapDocVerdictFunctionIsTotalAndExclusive pins the FUNCTION, because the
// document contract is only as good as the mapping it holds authors to. The
// historic four buckets were NOT total — `full` vs `partial` (15 of 17 rows in
// both delivered documents) had no bucket — which is the root cause the four
// copies in those documents came from.
func TestGapDocVerdictFunctionIsTotalAndExclusive(t *testing.T) {
	levels := []string{levelFull, levelPartial, levelNone, levelUnknown}
	cases := map[[2]string]string{
		{levelNone, levelPartial}:    verdictEnemyHasOursNot,
		{levelNone, levelFull}:       verdictEnemyHasOursNot,
		{levelNone, levelUnknown}:    verdictIncomparable,
		{levelPartial, levelFull}:    verdictEnemyStronger,
		{levelPartial, levelNone}:    verdictOursHasEnemyNot,
		{levelFull, levelPartial}:    verdictOursStronger,
		{levelFull, levelNone}:       verdictOursHasEnemyNot,
		{levelFull, levelFull}:       verdictEven,
		{levelPartial, levelPartial}: verdictEven,
		{levelNone, levelNone}:       verdictEven,
		{levelUnknown, levelFull}:    verdictIncomparable,
		{levelUnknown, levelUnknown}: verdictIncomparable,
		{levelFull, levelUnknown}:    verdictIncomparable,
		{levelPartial, levelUnknown}: verdictIncomparable,
		{levelUnknown, levelPartial}: verdictIncomparable,
		{levelUnknown, levelNone}:    verdictIncomparable,
	}
	if len(cases) != len(levels)*len(levels) {
		t.Fatalf("the table must cover all %d level pairs, covers %d", len(levels)*len(levels), len(cases))
	}
	for pair, want := range cases {
		got, err := gapDocVerdictFor(pair[0], pair[1])
		if err != nil {
			t.Errorf("gapDocVerdictFor(%s, %s) errored: %v", pair[0], pair[1], err)
			continue
		}
		if got != want {
			t.Errorf("gapDocVerdictFor(%s, %s) = %q, want %q", pair[0], pair[1], got, want)
		}
	}
	// The historical four are a STRICT subset: dropping the two added names
	// must leave a pair with no home, or the added names were decoration.
	historic := []string{verdictEnemyHasOursNot, verdictEnemyStronger, verdictOursHasEnemyNot, verdictEven}
	unhomed := 0
	for pair, want := range cases {
		if gapContainsString(historic, want) {
			continue
		}
		unhomed++
		_ = pair
	}
	if unhomed == 0 {
		t.Errorf("the historic four buckets already covered every pair — the two added names are not load-bearing")
	}
	// An unknown level token is a refusal, not a guess.
	if _, err := gapDocVerdictFor("high", levelPartial); err == nil {
		t.Error("an unknown level token must be refused by name")
	}
}

// TestGapDocContractNamesTheFourCopiesDefect is THE red arm: the exact shape the
// two delivered documents had, planted one defect at a time on a document that
// is otherwise valid.
func TestGapDocContractNamesTheFourCopiesDefect(t *testing.T) {
	clean := gapBuildDoc(t, gapSeventeenLevels())
	if problems := checkGapDoc("synthetic-clean.md", clean); len(problems) != 0 {
		t.Fatalf("the baseline fixture must be clean, got:\n%s", strings.Join(problems, "\n"))
	}

	// (a) A row whose 我方 is `none` listed under 我有敌无 — the defect measured
	// at devdoc/competitor/gap-cline.md:86 and gap-astrbot.md:86. The fixture's
	// D01 is (none, partial), so its home bucket is 敌有我无; moving it is
	// exactly the 2026-11-06 plant.
	planted := gapMoveDim(t, clean, "D01", verdictOursHasEnemyNot)
	gapRequireNames(t, checkGapDoc("planted-none-in-ours.md", planted),
		"D01", verdictOursHasEnemyNot, "机械判定为")

	// (b) A dimension listed TWICE — the four-copies shape in its purest form:
	// every dimension in every bucket. A count would pass (17 dims, 4 lists);
	// the membership rule must not.
	fourCopies := gapBuildDoc(t, gapSeventeenLevels())
	all17 := make([]string, 0, gapDocDimCount)
	for i := 1; i <= gapDocDimCount; i++ {
		all17 = append(all17, gapDimID(i))
	}
	for _, v := range gapVerdicts {
		row := fmt.Sprintf("| %s | %s |", v, strings.Join(all17, gapDimListSep))
		old := gapBucketRowFor(fourCopies, v)
		if old == "" {
			t.Fatalf("fixture is missing bucket %q", v)
		}
		fourCopies = strings.Replace(fourCopies, old, row, 1)
	}
	gapRequireNames(t, checkGapDoc("four-copies.md", fourCopies), "同时列在桶")

	// (c) A hand-written verdict that disagrees with its own two levels.
	wrongVerdict := strings.Replace(clean, "| D01 | "+levelNone+" | "+levelPartial+" | "+verdictEnemyHasOursNot+" |",
		"| D01 | "+levelNone+" | "+levelPartial+" | "+verdictEven+" |", 1)
	if wrongVerdict == clean {
		t.Fatal("plant failed: the D01 row was not found")
	}
	gapRequireNames(t, checkGapDoc("wrong-verdict.md", wrongVerdict), "D01", "机械判定为")

	// (d) A missing bucket — an absent bucket cannot be told from a forgotten one.
	missing := strings.Replace(clean, gapBucketRowFor(clean, verdictIncomparable)+"\n", "", 1)
	gapRequireNames(t, checkGapDoc("missing-bucket.md", missing), "缺少桶")

	// (e) An undeclared level spelling, in place — an author's typo must be
	// named with its dimension, not silently coerced to `none`.
	badLevel := strings.Replace(clean, "| D02 | "+levelPartial+" |", "| D02 | highish |", 1)
	if badLevel == clean {
		t.Fatal("plant failed: the D02 row was not found")
	}
	gapRequireNames(t, checkGapDoc("bad-level.md", badLevel), "D02", "highish")

	// (f) A dimension dropped from the main table, and one listed twice.
	rows := strings.Split(clean, "\n")
	var kept []string
	dropped := false
	for _, r := range rows {
		if !dropped && strings.HasPrefix(r, "| D05 |") {
			dropped = true
			continue
		}
		kept = append(kept, r)
	}
	gapRequireNames(t, checkGapDoc("dropped-row.md", strings.Join(kept, "\n")), "D05")
	duplicated := strings.Replace(clean, "| D06 |", "| D05 |", 1)
	gapRequireNames(t, checkGapDoc("dup-row.md", duplicated), "出现 2 次")

	// (g) A dimension MOVED to a bucket that is not its verdict, while its row
	// stays correct — the membership arm must not be satisfiable by the row's
	// own verdict cell, or the bucket view would be decorative.
	moved := gapMoveDim(t, clean, "D02", verdictEnemyHasOursNot)
	gapRequireNames(t, checkGapDoc("moved-bucket.md", moved),
		"D02", verdictEnemyHasOursNot, "机械判定为")

	// (h) THE TWO DEFECTS AT ONCE, which is how the shape actually arrived:
	// every dimension FOUR times in the main table AND no bucket view at all.
	// The pre-contract documents were literally this — four copies of the same
	// 17 rows, and a bucket vocabulary that did not exist yet — so a gate that
	// named only one half would hand the author a half-fixed document and a
	// second round. Both refusals are required, by name, from ONE check.
	fourRowsZeroBuckets := gapFourCopiesWithoutBucketView(t, clean)
	gapRequireNames(t, checkGapDoc("four-copies-zero-buckets.md", fourRowsZeroBuckets),
		"主表有 68 行", "出现 4 次", "桶视图缺少桶")
}

// gapFourCopiesWithoutBucketView renders the 2026-11-06 shape from a clean
// fixture: each main-table row repeated four times, and the bucket-view section
// cut off entirely. It is derived by MUTATION from the clean document (arm (h)'s
// rule), never hand-written, so it cannot drift from the contract.
func gapFourCopiesWithoutBucketView(t *testing.T, clean string) string {
	t.Helper()
	var rebuilt []string
	rows := 0
	for _, line := range strings.Split(clean, "\n") {
		// The bucket view — and everything after it — is dropped: this is the
		// ZERO-BUCKET half, and it must be a real absence, not an empty section.
		if strings.HasPrefix(line, "#") && strings.Contains(line, gapBucketViewHeadingCue) {
			break
		}
		if !strings.HasPrefix(line, "| "+gapDocDimPrefix) {
			rebuilt = append(rebuilt, line)
			continue
		}
		for i := 0; i < 4; i++ {
			rebuilt = append(rebuilt, line)
		}
		rows += 4
	}
	if rows != gapDocDimCount*4 {
		t.Fatalf("plant failed: the fixture carried %d main-table rows, want %d",
			rows, gapDocDimCount*4)
	}
	return strings.Join(rebuilt, "\n")
}

// gapMoveDim removes dim from whichever bucket lists it and appends it to
// target instead. It returns the document, or fails the test when the fixture
// does not list the dimension at all.
func gapMoveDim(t *testing.T, doc, dim, target string) string {
	t.Helper()
	found := false
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		for _, v := range gapVerdicts {
			if !strings.HasPrefix(line, "| "+v+" |") {
				continue
			}
			cells := strings.Split(line, "|")
			if len(cells) < 4 {
				continue
			}
			list := gapSplitDimList(cells[2])
			kept := make([]string, 0, len(list))
			removed := false
			for _, d := range list {
				if d == dim {
					removed = true
					found = true
					continue
				}
				kept = append(kept, d)
			}
			if !removed {
				continue
			}
			render := gapBucketEmptyMark
			if len(kept) != 0 {
				render = strings.Join(kept, gapDimListSep)
			}
			lines[i] = "| " + v + " | " + render + " |"
		}
	}
	if !found {
		t.Fatalf("fixture does not list %s in any bucket", dim)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "| "+target+" |") {
			continue
		}
		cells := strings.Split(line, "|")
		list := gapSplitDimList(cells[2])
		list = append(list, dim)
		lines[i] = "| " + target + " | " + strings.Join(list, gapDimListSep) + " |"
		break
	}
	return strings.Join(lines, "\n")
}

// gapBucketRowFor returns the whole `| bucket | … |` line, or "".
func gapBucketRowFor(doc, bucket string) string {
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| "+bucket+" |") {
			return line
		}
	}
	return ""
}

// TestGapDocContractOnTheRealCorpus is the GREEN arm over the delivered
// documents, and it refuses a scan that judged nothing.
func TestGapDocContractOnTheRealCorpus(t *testing.T) {
	root := repoRoot(t)
	files, err := gapDocFiles(root)
	if err != nil {
		t.Fatalf("%v", err)
	}
	// A floor, not a magic expectation: the two documents this epic repaired
	// must be among them, or the scan is looking at the wrong directory.
	if len(files) < 2 {
		t.Fatalf("walked %d gap document(s) under %s — expected at least 2 (the scan is vacuous or misplaced)",
			len(files), gapDocDirRel)
	}
	judged := 0
	bad := 0
	for _, rel := range files {
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Errorf("%s: %v", rel, rerr)
			continue
		}
		judged++
		if problems := checkGapDoc(rel, string(body)); len(problems) != 0 {
			bad++
			t.Errorf("%s", strings.Join(problems, "\n"))
		}
	}
	if judged != len(files) {
		t.Fatalf("judged %d of %d file(s) — an unread file must never count as clean", judged, len(files))
	}
	t.Logf("gapdoc contract: walked %d document(s), judged %d, %d with violations", len(files), judged, bad)
}

// TestGapDocContractRefusesAVacuum pins the non-vacuity rule itself: an absent
// corpus directory is an ERROR, never "no documents, nothing to check".
func TestGapDocContractRefusesAVacuum(t *testing.T) {
	dir := t.TempDir()
	if _, err := gapDocFiles(dir); err == nil {
		t.Fatal("a root with no devdoc/competitor directory must be an error, not an empty result")
	}
	if _, err := gapDocFiles(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("an absent root must be an error, not an empty result")
	}
	// A directory holding no gap-* file is an empty result — the caller's
	// floor (len(files) < 2 above) is what turns that into a failure, and this
	// arm pins that the two cases stay distinguishable.
	if mkErr := os.MkdirAll(filepath.Join(dir, gapDocDirRel), 0o755); mkErr != nil {
		t.Fatalf("mkdir: %v", mkErr)
	}
	if err := os.WriteFile(filepath.Join(dir, gapDocDirRel, "astrbot.md"), []byte("not a gap doc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := gapDocFiles(dir)
	if err != nil {
		t.Fatalf("an existing directory must not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a plain stage-2 document must not be treated as a gap document: %v", got)
	}
}

// TestGapDocVerdictOrderIsDeclared keeps the bucket order stable, so a report
// diff means a real change rather than a map iteration.
func TestGapDocVerdictOrderIsDeclared(t *testing.T) {
	want := []string{
		verdictEnemyHasOursNot, verdictEnemyStronger, verdictOursHasEnemyNot,
		verdictOursStronger, verdictEven, verdictIncomparable,
	}
	if strings.Join(gapVerdicts, ",") != strings.Join(want, ",") {
		t.Fatalf("bucket order changed: %v", gapVerdicts)
	}
	sorted := append([]string(nil), gapVerdicts...)
	sort.Strings(sorted)
	if strings.Join(sorted, ",") == strings.Join(gapVerdicts, ",") && len(gapVerdicts) > 1 {
		t.Log("bucket names happen to be alphabetical — cosmetic only")
	}
}
