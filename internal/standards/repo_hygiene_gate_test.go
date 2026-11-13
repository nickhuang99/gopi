package standardgate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// baselineFloorRatio is the fraction of the tree's LIVE violations the shipped
// permit table must cover before the real-tree test will score it (Rule 1: it is
// one knob, named). It is a ratio and not an absolute row count because the
// ratchet it guards runs in one direction only — see baselineFloorVerdict.
const baselineFloorRatio = 0.5

// baselineFloorVerdict is the non-vacuity floor of the REAL-TREE run, and the
// reason it is a ratio rather than the absolute row count it used to be (1007
// rows at landing, later `500`).
//
// The floor and the ratchet measure the SAME axis but run in OPPOSITE
// directions: Rule 39's table may only ratchet DOWN, so every legitimate
// clean-up walks the shipped table INTO an absolute floor. Measured 2026-10-03:
// three clean commits (1c868ff6 −325 permits, 49a56355 −181, 981054bc −72)
// took the table 1007 → 429 and turned this test RED on master, with a failure
// text ("429 < 500") that says nothing about whether the scan ran — the exact
// question the floor exists to ask. So the guard is written against the scan
// that just ran.
//
// It is NOT a weakening. The permits are a SHIPPED, reviewed artifact read one
// at a time (Rule 39) while the hits are what the tree carries now, so a
// truncated or hand-trimmed table is caught here at ANY scale — which the
// absolute `500` could not do at all for a tree whose live violations fall
// below 1000 (every row could be missing and 500 would still pass).
//
// Returns the failure message, or "" when the floor is satisfied.
func baselineFloorVerdict(permits, hits, paths int, baselinePath string) string {
	if hits == 0 {
		return fmt.Sprintf("the scan of %d tracked paths found no violation at all — the classifier is broken or the tree is not the repository, not 'clean' (Rule 15)", paths)
	}
	if permits < int(float64(hits)*baselineFloorRatio) {
		return fmt.Sprintf("the baseline carries only %d rows while the scan of %d tracked paths reports %d violations — a permit table covering less than %.0f%% of the tree's live violations is a fragment, not the measured table (%s)",
			permits, paths, hits, baselineFloorRatio*100, baselinePath)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Rule 39's gate — the proofs
//
// Four shapes, and the gate is only trustworthy with all four:
//   1. the RED proof — a fixture index carrying deliberately over-limit,
//      magic-headed and font blobs turns the gate red (a predicate that stopped
//      matching would otherwise read green forever);
//   2. the COMPLEMENT — the same fixture's clean files are NOT reported (a
//      predicate loosened to "report everything" is the other failure);
//   3. the RATCHET proofs — a stale permit, a class-traded permit and a new
//      violation each produce their own verdict;
//   4. the REAL-TREE run — the shipped baseline leaves the real index green.
// ---------------------------------------------------------------------------

// hygieneFixture builds a throwaway git repository whose INDEX carries one blob
// per violation class plus clean files, and returns its root. A real repository
// (not a synthetic path list) is the point: the gate reads `git ls-files`, and a
// fixture that bypasses the index would not prove the index is read.
func hygieneFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(argv ...string) {
		t.Helper()
		cmd := exec.Command(gitBinary, argv...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(argv, " "), dir, err, out)
		}
	}
	run("init", "-q")

	write := func(rel string, data []byte) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// CLEAN: ordinary source and text, PLUS a large SOURCE file. Rule 39's size
	// arm refuses a file "above 256 KiB that is not source or text of record",
	// so a .go file over the limit is legitimate (the tree really carries seven
	// of them) and must NOT be reported — this fixture's clean half carries one
	// on purpose, at exactly the size of the OVERSIZE blob below so the two are
	// a minimal pair differing only in extension.
	write("src/main.go", []byte("package main\n\nfunc main() {}\n"))
	write("src/large.go", bytes.Repeat([]byte("// a source line\n"), oversizeLimitBytes/len("// a source line\n")+1))
	write("docs/notes.txt", []byte("a tracked note — text, small, legitimate\n"))

	// OVERSIZE ONLY: text (no NUL, no magic), over the ordinary limit.
	write("assets/big.txt", bytes.Repeat([]byte("a"), oversizeLimitBytes+1))
	// MAGIC ONLY: a real PNG signature, tiny.
	write("docs/shot.png", append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("small body")...))
	// MAGIC ONLY: the ELF signature.
	write("bin/tool", append([]byte{0x7F, 'E', 'L', 'F'}, bytes.Repeat([]byte{0}, 64)...))
	// FONT + OVERSIZE: a font over the font limit, filled with 'a' so the size
	// arms fire without the NUL arm (a second blob can carry several classes at
	// once, which is exactly why a permit is keyed on path AND class).
	write("fonts/face.ttf", bytes.Repeat([]byte("a"), fontLimitBytes+1))
	// NUL ONLY: no magic, under the ordinary limit, NUL in the first 8 KiB
	// (a small compressed capture).
	write("data/capture.gz", append([]byte{0x1F, 0x8B, 0x08}, bytes.Repeat([]byte{0}, 512)...))

	run("add", "-A")
	return dir
}

// hygienePairs renders "path class" lines for an assertion, sorted.
func hygienePairs(hits []blobViolation) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.path+" "+h.class)
	}
	sort.Strings(out)
	return out
}

// TestRepoHygieneGateRedProof is half 1: the fixture's poisoned index must be
// reported, class by class.
func TestRepoHygieneGateRedProof(t *testing.T) {
	root := hygieneFixture(t)
	hits, err := scanTrackedBlobs(root)
	if err != nil {
		t.Fatalf("scan of the fixture index failed: %v", err)
	}
	want := []string{
		"assets/big.txt " + classOversize,
		"bin/tool " + classMagic,
		"data/capture.gz " + classNul,
		"docs/shot.png " + classMagic,
		"fonts/face.ttf " + classFont,
		"fonts/face.ttf " + classOversize,
	}
	got := hygienePairs(hits)
	t.Logf("RED: the fixture index (one blob per class) reported %d violations:\n  %s", len(got), strings.Join(got, "\n  "))
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("red proof: the fixture index reported\n  %s\nwant exactly\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	// The gate PROPER: with no baseline at all, every one of those is a NEW
	// violation and the report must say so, naming each path.
	report := hygieneReport(nil, nil, nil, hits, "testdata/repo-hygiene-baseline.tsv")
	if !strings.Contains(report, "NEW tracked blobs (6)") {
		t.Errorf("report does not count the new violations:\n%s", report)
	}
	for _, p := range []string{"assets/big.txt", "bin/tool", "data/capture.gz", "docs/shot.png", "fonts/face.ttf"} {
		if !strings.Contains(report, p) {
			t.Errorf("report does not name %s:\n%s", p, report)
		}
	}
	// And the clean half must be absent — a gate that reports everything is as
	// useless as one that reports nothing.
	for _, p := range []string{"src/main.go", "src/large.go", "docs/notes.txt"} {
		if strings.Contains(report, p) {
			t.Errorf("report names the clean path %s:\n%s", p, report)
		}
	}
}

// TestRepoHygieneGateAcceptsTheBaselinedFixture is half 2: freeze exactly the
// fixture's violations and the SAME scan is green — no fresh hit, no stale
// permit, no traded class.
func TestRepoHygieneGateAcceptsTheBaselinedFixture(t *testing.T) {
	root := hygieneFixture(t)
	hits, err := scanTrackedBlobs(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	baseline := map[baselineKey]baselineRow{}
	for _, h := range hits {
		baseline[baselineKey{path: h.path, class: h.class}] = baselineRow{reason: "fixture permit: " + h.path}
	}
	baselined, traded, stale, fresh := classifyViolations(hits, baseline)
	if len(fresh) != 0 || len(stale) != 0 || len(traded) != 0 {
		t.Fatalf("a fully-baselined tree must be clean: fresh=%v stale=%v traded=%v",
			hygienePairs(fresh), stale, hygienePairs(traded))
	}
	if len(baselined) != len(hits) {
		t.Errorf("baselined %d of %d hits", len(baselined), len(hits))
	}
	if report := hygieneReport(baselined, traded, stale, fresh, "fixture"); strings.Contains(report, "NEW") || strings.Contains(report, "STALE") {
		t.Errorf("a clean tree still reported a defect:\n%s", report)
	}
}

// TestRepoHygieneGateStalePermitIsAnError is the ratchet's first tooth: a permit
// whose file is gone (or which no longer violates) is RED, naming the path.
func TestRepoHygieneGateStalePermitIsAnError(t *testing.T) {
	root := hygieneFixture(t)
	hits, err := scanTrackedBlobs(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	baseline := map[baselineKey]baselineRow{}
	for _, h := range hits {
		baseline[baselineKey{path: h.path, class: h.class}] = baselineRow{reason: "fixture permit"}
	}
	// A permit for a path that is not in the index at all — the shape left
	// behind when the epic deletes a blob but forgets its row.
	baseline[baselineKey{path: "docs/removed-blob.png", class: classMagic}] = baselineRow{reason: "fixture: the file this permitted is gone"}

	_, _, stale, _ := classifyViolations(hits, baseline)
	if len(stale) != 1 || stale[0] != "docs/removed-blob.png ["+classMagic+"]" {
		t.Fatalf("stale permits = %v, want exactly [docs/removed-blob.png]", stale)
	}
	report := hygieneReport(nil, nil, stale, nil, "testdata/repo-hygiene-baseline.tsv")
	if !strings.Contains(report, "STALE PERMITS (1)") || !strings.Contains(report, "docs/removed-blob.png") {
		t.Errorf("the stale permit is not reported:\n%s", report)
	}
	if !strings.Contains(report, "DELETE the row") {
		t.Errorf("the report does not say what to do about a stale permit:\n%s", report)
	}
}

// TestRepoHygieneGateClassTradeIsNotClean is the ratchet's second tooth: a path
// frozen as one class that now violates as another has not been made clean.
func TestRepoHygieneGateClassTradeIsNotClean(t *testing.T) {
	root := hygieneFixture(t)
	hits, err := scanTrackedBlobs(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	baseline := map[baselineKey]baselineRow{}
	for _, h := range hits {
		baseline[baselineKey{path: h.path, class: h.class}] = baselineRow{reason: "fixture permit"}
	}
	// docs/shot.png is a MAGIC violation; pretend its permit was frozen as NUL,
	// and drop the magic permit so the trade is unambiguous.
	delete(baseline, baselineKey{path: "docs/shot.png", class: classMagic})
	baseline[baselineKey{path: "docs/shot.png", class: classNul}] = baselineRow{reason: "fixture: frozen under the wrong class"}

	_, traded, stale, fresh := classifyViolations(hits, baseline)
	if len(traded) != 1 || traded[0].path != "docs/shot.png" {
		t.Fatalf("traded = %v, want exactly one for docs/shot.png", hygienePairs(traded))
	}
	if len(stale) != 1 || stale[0] != "docs/shot.png ["+classNul+"]" {
		t.Errorf("the dropped magic permit shows as stale = %v, want [docs/shot.png [nul]]", stale)
	}
	if len(fresh) != 0 {
		t.Errorf("a class trade must be its OWN verdict, not a new violation: fresh=%v", hygienePairs(fresh))
	}
	report := hygieneReport(nil, traded, stale, fresh, "testdata/repo-hygiene-baseline.tsv")
	if !strings.Contains(report, "DIFFERENT CLASS") || !strings.Contains(report, `frozen class set {nul}`) {
		t.Errorf("the class trade is not reported in full:\n%s", report)
	}
}

// TestRepoHygieneGateRefusesToPassBlind is Rule 15's half of the gate: a scan
// that found nothing must be distinguishable from a scan that never ran.
func TestRepoHygieneGateRefusesToPassBlind(t *testing.T) {
	// (a) a git repository with an EMPTY index is an error, not "clean".
	empty := t.TempDir()
	cmd := exec.Command(gitBinary, "init", "-q")
	cmd.Dir = empty
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if _, err := trackedPathsIn(empty); err == nil {
		t.Error("an empty index returned no error — the gate would read green on a tree it never scanned")
	}
	// (b) a directory that is not a repository at all is an error too.
	if _, err := trackedPathsIn(t.TempDir()); err == nil {
		t.Error("a non-repository returned no error")
	}
	// (c) a MISSING baseline table is an error, never an empty allow-list.
	if _, err := loadBaseline(filepath.Join(t.TempDir(), "absent.tsv")); err == nil {
		t.Error("a missing baseline table loaded as empty — every frozen permit would read as a new violation")
	}
}

// TestRepoHygieneBaselineRefusesMalformedRows pins the table's contract: a
// typo'd class, an empty reason, a short row and a duplicate path are each
// rejected with the line number, so a permit cannot be added by accident.
func TestRepoHygieneBaselineRefusesMalformedRows(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"short row", "a.png\tmagic\n", "want 3 tab-separated fields"},
		{"unknown class", "a.png\tgigantic\tbecause\n", "unknown class"},
		{"empty reason", "a.png\tmagic\t\n", "empty reason"},
		{"duplicate", "a.png\tmagic\tbecause\na.png\tmagic\tagain\n", "duplicate row"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "b.tsv")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, err := loadBaseline(path)
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
	// And the positive control, so "every row is rejected" cannot pass the
	// cases above: a well-formed table loads.
	path := filepath.Join(t.TempDir(), "ok.tsv")
	if err := os.WriteFile(path, []byte("# a comment\ndocs/a.png\tmagic\ta reviewed reason\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, err := loadBaseline(path)
	if err != nil {
		t.Fatalf("a well-formed table was rejected: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("well-formed table parsed as %v", rows)
	}
	if _, ok := rows[baselineKey{path: "docs/a.png", class: classMagic}]; !ok {
		t.Fatalf("well-formed table keyed wrong: %v", rows)
	}
}

// TestRepoHygieneSizeArmExemptsSourceAndRecord is the minimal repro for the RED
// this fix was written against (Rule 10.5 / Rule 5: every fix carries its
// regression test), and the shape is ONE minimal pair differing only in
// extension: two identical byte bodies, one named `.go`, one named `.txt`.
//
// THE MEASURED RED, on the tree as it landed: 264,149-byte
// internal/lineedit/answer.go — plus six other `.go` files and one 2 MB `.md`
// — was reported as an `oversize` violation and had to be frozen in
// testdata/repo-hygiene-baseline.tsv, which directly contradicts Rule 39's own
// sentence: a tracked file above 256 KiB that is **not source or text of
// record**. The predicate and the rule disagreed, and the predicate was wrong.
//
// THE THREE THINGS THIS PINS, in both directions:
//  1. a >256 KiB SOURCE (.go) file is NOT oversize (the regression);
//  2. the exemption did not become "nothing is oversize": the same body in
//     `assets/big.txt` IS;
//  3. the exemption does not travel to the CONTENT arms — a PNG/ELF-headed file
//     named `.go` is still a magic violation, and a NUL-carrying `.go` is still
//     a NUL violation. A `.go` extension is a claim about the content, and the
//     magic/NUL arms test the content itself.
//
// And the census generator's mirrored predicate is asserted on the SAME paths,
// because the table is written by the generator while the gate is enforced by
// the other file: a fix landing in only one of them would make the tree red on
// the gate, or make `--check` disagree with the tree, depending on which half
// was missed.
func TestRepoHygieneSizeArmExemptsSourceAndRecord(t *testing.T) {
	root := t.TempDir()
	run := func(argv ...string) {
		t.Helper()
		cmd := exec.Command(gitBinary, argv...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(argv, " "), err, out)
		}
	}
	run("init", "-q")
	write := func(rel string, data []byte) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// The minimal pair: one byte body, two extensions. `a` was chosen because it
	// carries no magic and no NUL, so the SIZE arm is the only arm either file
	// can fire — which is exactly the axis under test.
	body := bytes.Repeat([]byte("a"), oversizeLimitBytes+1)
	write("internal/lineedit/answer.go", body) // SOURCE, over the limit
	write("assets/big.txt", body)              // a blob, same bytes, same size
	write("devdoc/evidence-record.md", body)   // text of record, over the limit

	// The content arms must still fire through a source extension.
	pngHead := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("body")...)
	write("internal/fake.go", pngHead)
	write("internal/nul.go", append([]byte("package x\n"), bytes.Repeat([]byte{0}, 64)...))

	run("add", "-A")

	hits, err := scanTrackedBlobs(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := strings.Join(hygienePairs(hits), "\n")
	want := strings.Join([]string{
		"assets/big.txt " + classOversize,
		"internal/fake.go " + classMagic,
		"internal/nul.go " + classNul,
	}, "\n")
	t.Logf("scan of the minimal-pair fixture reported:\n  %s", strings.Join(hygienePairs(hits), "\n  "))
	if got != want {
		t.Fatalf("size arm / content arms are not as Rule 39 states:\n got:\n  %s\nwant exactly:\n  %s",
			strings.Join(hygienePairs(hits), "\n  "), strings.Join(strings.Split(want, "\n"), "\n  "))
	}
	for _, p := range []string{"internal/lineedit/answer.go", "devdoc/evidence-record.md"} {
		if strings.Contains(got, p) {
			t.Errorf("%s is source/text of record and must not be a violation:\n%s", p, got)
		}
	}

	// The census generator is a `main` package under testdata/, so it cannot be
	// imported: its mirrored lists are kept in step by `--check` (this file's
	// TestRepoHygieneCensusReDerivesTheTable runs it and requires exit 0), which
	// is why the drift never survives a test run. What IS asserted here is that
	// the gate's own predicate covers the real tree's large source/record files
	// and NOT the blobs, so the exemption cannot be "everything".
	for _, rel := range []string{
		"internal/lineedit/answer.go",
		"cmd/agent/cli_tasks.go",
		"internal/tokenizer/tokenizer.json",
	} {
		got := isSourceOrTextOfRecord(rel)
		want := rel != "internal/tokenizer/tokenizer.json"
		if got != want {
			t.Errorf("isSourceOrTextOfRecord(%q) = %v, want %v", rel, got, want)
		}
	}
	for _, rel := range []string{"assets/big.txt", "tests/fixtures/x.log", "scripts/links/amd64/links"} {
		if isSourceOrTextOfRecord(rel) {
			t.Errorf("%s is a regenerable artefact — the size arm must judge it", rel)
		}
	}
}

// TestRepoHygieneMagicTableIsNotVacuous checks the signature list itself: every
// entry must be found by the matcher, no entry may be longer than the probe,
// and no entry may be a PREFIX of another (which would make the named format
// depend on list order).
func TestRepoHygieneMagicTableIsNotVacuous(t *testing.T) {
	if len(binaryMagics) == 0 {
		t.Fatal("the signature list is empty — the magic arm can never fire")
	}
	for _, m := range binaryMagics {
		if len(m.head) > maxMagicLenBytes {
			t.Errorf("%s: signature is %d bytes, over maxMagicLenBytes=%d", m.name, len(m.head), maxMagicLenBytes)
		}
		got, ok := matchBinaryMagic(append(append([]byte{}, m.head...), []byte("body")...))
		if !ok || got != m.name {
			t.Errorf("%s: matcher returned (%q, %v) on its own signature", m.name, got, ok)
		}
	}
	for i, a := range binaryMagics {
		for j, b := range binaryMagics {
			if i == j || len(a.head) >= len(b.head) {
				continue
			}
			if bytes.Equal(b.head[:len(a.head)], a.head) {
				t.Errorf("%s (%v) is a prefix of %s (%v) — the reported format would depend on list order", a.name, a.head, b.name, b.head)
			}
		}
	}
	// A text head must match nothing at all.
	if name, ok := matchBinaryMagic([]byte("package main\n")); ok {
		t.Errorf("plain Go source matched the %s signature", name)
	}
}

// TestRepoHygieneRealTreeIsBaselined is the green half on the REAL index: the
// shipped baseline must leave the tree clean, the scan must actually have run
// (a non-trivial path count), and the baseline must actually have been
// exercised (a non-trivial permit count) — a gate that read a missing table as
// "nothing to check" is the failure mode this asserts against.
func TestRepoHygieneRealTreeIsBaselined(t *testing.T) {
	root := repoRoot(t)
	baselinePath := filepath.Join(root, filepath.FromSlash(repoHygieneBaselineRel))
	baseline, err := loadBaseline(baselinePath)
	if err != nil {
		t.Fatalf("load %s: %v", baselinePath, err)
	}
	paths, err := trackedPathsIn(root)
	if err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	if len(paths) < 1000 {
		t.Fatalf("the index listed only %d paths in %s — refusing to score a tree that is not the repository", len(paths), root)
	}
	hits, err := scanTrackedBlobs(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if msg := baselineFloorVerdict(len(baseline), len(hits), len(paths), baselinePath); msg != "" {
		t.Fatal(msg)
	}
	baselined, traded, stale, fresh := classifyViolations(hits, baseline)
	if len(fresh) > 0 || len(traded) > 0 || len(stale) > 0 {
		t.Fatalf("%s\n(scanned %d tracked paths, %d violations, %d baselined)",
			hygieneReport(baselined, traded, stale, fresh, baselinePath), len(paths), len(hits), len(baselined))
	}
	// The gate exercised its own table: the real tree still carries the frozen
	// violations, so this is a live scan with live permits, not an empty pass.
	if len(baselined) != len(hits) || len(hits) == 0 {
		t.Fatalf("baselined %d of %d hits — the scan and the table disagree", len(baselined), len(hits))
	}
	t.Logf("Rule 39 scan: %d tracked paths, %d violations, all baselined by %d permits",
		len(paths), len(hits), len(baseline))
}

// TestRepoHygienePermitFloorIsNotScaleBound is the minimal repro for the RED
// this guard was fixed against (Rule 5: every fix carries its regression
// test), and it pins BOTH directions — a floor that reddened on a shrinking
// tree would come straight back, and a floor quietly turned into a no-op is the
// other way to make the real-tree run pass.
//
// The measured red, on master 2026-10-03: the table stood at 429 rows (a live
// scan's own count) and the absolute floor said "429 < 500".
func TestRepoHygienePermitFloorIsNotScaleBound(t *testing.T) {
	const (
		livePermits = 429 // the shipped table, and the number of hits the scan reports
		tracked     = 6250
	)
	// (a) THE REGRESSION: the shipped, fully-exercised table is accepted, at
	// whatever scale the ratchet has walked it down to — including a table far
	// below the absolute floor that produced the red.
	if msg := baselineFloorVerdict(livePermits, livePermits, tracked, "testdata/repo-hygiene-baseline.tsv"); msg != "" {
		t.Fatalf("a fully-exercised table was refused: %s", msg)
	}
	// …and a clean-up that shrinks BOTH sides together (the blobs land and their
	// permits are deleted in the same commit — how 1c868ff6/49a56355/981054bc
	// actually went) must stay accepted all the way down.
	for _, n := range []int{1007, 682, 501, 429, 120, 3} {
		if msg := baselineFloorVerdict(n, n, tracked, "b.tsv"); msg != "" {
			t.Fatalf("a table of %d rows covering all %d live violations was refused: %s", n, n, msg)
		}
	}
	// The boundary is the ratio itself: exactly half is the floor.
	if msg := baselineFloorVerdict(214, 429, tracked, "b.tsv"); msg != "" {
		t.Fatalf("a table at exactly the floor ratio was refused: %s", msg)
	}
	// (b) THE TEETH: a fragment — rows gone while the violations they permitted
	// are still in the tree — is refused at every one of those scales. The
	// absolute `500` this guard replaced could not see it below 1000 hits.
	for _, tc := range []struct{ permits, hits int }{{100, 429}, {213, 429}, {0, 100}, {499, 1000}} {
		msg := baselineFloorVerdict(tc.permits, tc.hits, tracked, "b.tsv")
		if msg == "" {
			t.Fatalf("a fragment (%d permits for %d live violations) was accepted", tc.permits, tc.hits)
		}
		if !strings.Contains(msg, "fragment") || !strings.Contains(msg, "b.tsv") {
			t.Errorf("the refusal does not name the defect and the table: %s", msg)
		}
	}
	// (c) An empty scan is never a pass (Rule 15) — the other half of the floor.
	if msg := baselineFloorVerdict(429, 0, tracked, "b.tsv"); msg == "" || !strings.Contains(msg, "found no violation") {
		t.Fatalf("an empty scan was not refused: %q", msg)
	}
}

// TestRepoHygieneCensusReDerivesTheTable is Rule 38's half: the table must be
// RE-DERIVABLE from the tree, not merely believed. It runs the shipped census
// generator with --check (which exits 1 on any delta) and requires exit 0.
//
// It exec's a REAL BINARY built once for this test process (generatorBinary),
// not through `go run`: `go run` recompiles on every call — the cost that took
// this package past its per-package budget — and it also collapses the program's
// exit status to 1. The arm is skipped in short mode: the gate's own verdict does
// not depend on it (the gate is the live scan above), so a machine without a
// toolchain still gets the check.
func TestRepoHygieneCensusReDerivesTheTable(t *testing.T) {
	if testing.Short() {
		t.Skip("census re-derivation shelling out to the generator is skipped in -short mode")
	}
	root := repoRoot(t)
	census := filepath.Join(root, filepath.FromSlash(repoHygieneCensusGeneratorRel))
	if _, err := os.Stat(census); err != nil {
		t.Fatalf("census generator %s: %v", census, err)
	}
	cmd := exec.Command(generatorBinary(t, repoHygieneCensusGeneratorRel), root, repoHygieneBaselineRel, "--check")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("census --check failed (%v) — the baseline no longer matches the tracked tree:\n%s", err, out)
	}
	if !strings.Contains(string(out), "matches the tracked tree") {
		t.Errorf("census --check produced an unexpected verdict:\n%s", out)
	}
}
