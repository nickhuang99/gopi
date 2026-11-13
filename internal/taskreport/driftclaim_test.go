package taskreport

import (
	"strings"
	"testing"
)

// driftclaimFixture is the SHAPE of the motivating briefing, reduced to what the
// predicate reads: a count in prose, then the `for p in …` list block the count
// quantifies. It is quoted from row 01a1447c's own payload (the epic's incident),
// with the twelve paths shortened to three — the shape is what is under test, not
// the length of the list.
const driftclaimFixture = "## 要做的\n" +
	"\n" +
	"用 manifest 的**唯一写入者**逐行重生成这 12 行（Rule 36：manifest 是 DATA，绝不手改单元格）。\n" +
	"**一次一条路径**，逐条核验，不要一次全量 `--add-missing`：\n" +
	"\n" +
	"```bash\n" +
	"cd <your worktree>\n" +
	"for p in devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md \\\n" +
	"         devdoc/branch-lifecycle-ruling.md \\\n" +
	"         devdoc/why-worktree-residue.md; do\n" +
	"  python3 tools/docclass/classify.py . --only \"$p\"\n" +
	"done\n" +
	"```\n"

// TestPathSetClaims_ReadsTheListBlockTheCountQuantifies is the GREEN arm: the
// incident's own shape yields one claim, carrying the author's figure, the
// author's unit and the block's paths.
func TestPathSetClaims_ReadsTheListBlockTheCountQuantifies(t *testing.T) {
	claims := PathSetClaimsIn(driftclaimFixture)
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want 1 (the count + the list block it quantifies): %+v", len(claims), claims)
	}
	c := claims[0]
	if c.Count != 12 {
		t.Errorf("count = %d, want 12 (the author's own figure)", c.Count)
	}
	if c.Unit != "行" {
		t.Errorf("unit = %q, want 行 (the author's own unit)", c.Unit)
	}
	want := []string{
		"devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md",
		"devdoc/branch-lifecycle-ruling.md",
		"devdoc/why-worktree-residue.md",
	}
	if strings.Join(c.Paths, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", c.Paths, want)
	}
	if !strings.Contains(c.Line, "12 行") {
		t.Errorf("the quoted line must be the counted one, got %q", c.Line)
	}
}

// TestPathSetClaims_RefusesTheProseLineThatFooledTheLineScopedRule is the arm the
// FIRST implementation failed, kept as a fixture: the incident's text carries a
// parenthesis naming a command and a file, and a line-scoped rule reported it as a
// one-path "set". Under the block rule it is prose — one path, no run of entries —
// and produces nothing.
func TestPathSetClaims_RefusesTheProseLineThatFooledTheLineScopedRule(t *testing.T) {
	body := "（该 12 条与 `./bin/gopi tool doccheck` 第三条腿列出的路径**逐条相同**：evidence/doccheck-before.txt）\n"
	if got := PathSetClaimsIn(body); len(got) != 0 {
		t.Fatalf("claims = %+v, want none: one path on a prose line is not a set", got)
	}
}

// TestPathSetClaims_ArmsThatStaySilentByDesign pins the four shapes the predicate
// must NOT read, each of which is prose that merely mentions a figure or a path.
func TestPathSetClaims_ArmsThatStaySilentByDesign(t *testing.T) {
	cases := map[string]string{
		"a single list entry is prose, not an enumeration": "重生成这 12 行\n" +
			"for p in devdoc/only-one-path.md; do echo ok; done\n",
		"a count that stands beyond the look-back window describes another subject": "重生成这 12 行\n" +
			strings.Repeat("\n", pathSetClaimCountLookbackLines+1) + listBlock(),
		"a block nobody counts is not a claim this clause stamps": "## 要做的\n" + listBlock(),
		"a count with no counted unit is not a figure":            "重生成这些行\n" + listBlock(),
		"a command that names a file is not a list entry":         listBlock() + "python3 tools/docclass/classify.py . --only devdoc/one.md\n",
	}
	for name, body := range cases {
		if got := PathSetClaimsIn(body); len(got) != 0 {
			t.Errorf("%s: claims = %+v, want none", name, got)
		}
	}
}

// TestPathSetClaims_ReadsALatinFigureThePackageAlreadyKnows keeps the two
// vocabularies one: a briefing counting in the figure clause's own English units
// (`rows`) is read by the SAME claim, with the CJK reader never consulted.
func TestPathSetClaims_ReadsALatinFigureThePackageAlreadyKnows(t *testing.T) {
	body := "the drifted set is these 3 rows:\n" + listBlock()
	claims := PathSetClaimsIn(body)
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want 1 (the Latin figure half)", len(claims))
	}
	if claims[0].Count != 3 || claims[0].Unit != "rows" {
		t.Errorf("count/unit = %d/%q, want 3/rows", claims[0].Count, claims[0].Unit)
	}
}

// listBlock is a minimal list block — the smallest enumeration the predicate
// accepts (pathSetClaimMinBlockLines), in the shell shape the incident's text
// carries: a `for … in` header, entries glued by continuations, and a body.
func listBlock() string {
	return "for p in devdoc/a.md \\\n" +
		"         devdoc/b.md; do\n" +
		"  echo \"$p\"\n" +
		"done\n"
}
