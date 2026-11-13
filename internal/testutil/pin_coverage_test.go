package testutil

// Pin-coverage table: the BEFORE/AFTER picture of which test packages are
// pinned by TestMain, plus the audit of the gate's own detector.
//
// The gate (TestEveryTempUsingSuiteIsPinned) is only as good as its marker
// set: it can only demand a pin for a package it RECOGNISES as scratch-using.
// The audit that produced devdoc/scratch-residue-audit.md found the 2026-09-11
// signature — scratch reached through os.TempDir() or an explicit "/tmp"
// literal — was NOT in the marker set, so a package using those channels would
// have gone unpinned AND unnoticed. This test makes that difference visible
// (and re-runnable): it classifies every test package under both marker sets
// and reports the delta.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// legacyTempMarkers is the marker set the gate used BEFORE the audit widened
// it — kept here as the "before" column of the table (and as the regression
// evidence that the widening was necessary).
var legacyTempMarkers = []string{
	"t.TempDir()",
	`os.MkdirTemp("`,
}

// pinCoverageRow is one test package's row in the table.
type pinCoverageRow struct {
	rel      string
	legacy   bool // detected by the pre-audit marker set
	current  bool // detected by the widened marker set
	pinned   bool // TestMain routes the pin through internal/testutil
	pinSrc   string
	evidence string
}

// scratchPinCoverageFailure renders THIS gate's row for one unpinned package.
// It reuses the pin gate's shape-aware remedy text (scratchPinFailure's
// building blocks), so the two gates cannot drift into telling a reader two
// different things — the sibling message carried the same in-tree-only
// instruction, which a nested standalone module cannot carry out.
func scratchPinCoverageFailure(root string, row pinCoverageRow) string {
	head := fmt.Sprintf("%s creates scratch (%s) but no TestMain routes the pin through internal/testutil — "+
		"its scratch lands on the ambient temp root (the boot device)", row.rel, row.evidence)
	if scratchPinPackageKind(root, row.rel) == "nested" {
		return fmt.Sprintf("%s. %s (%s): %s. %s %s",
			head, row.rel, scratchPinShapeTag(root, row.rel), scratchPinVerdict,
			scratchPinFixNestedModule, scratchPinWhyNestedNotImportable)
	}
	return fmt.Sprintf("%s. %s (%s): %s. %s. %s",
		head, row.rel, scratchPinShapeTag(root, row.rel), scratchPinVerdict,
		fmt.Sprintf(scratchPinFixInTree, filepath.Base(row.rel)), scratchPinFixInTreeOwnRoot)
}

// TestPinCoverageTable prints the package → detected → PIN/NO-PIN table and
// fails when a package that creates scratch through ANY known channel has no
// pin. The two detection columns are the before/after numbers the audit
// deliverable quotes.
func TestPinCoverageTable(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	var rows []pinCoverageRow
	for _, rel := range testPackageDirs(t, root) {
		src := readTestSources(t, filepath.Join(root, rel))
		row := pinCoverageRow{
			rel:     rel,
			legacy:  containsAny(src, legacyTempMarkers),
			current: usesScratchTemp(src),
			pinned:  packagePinned(t, root, rel),
		}
		if row.current || row.legacy {
			row.evidence = scratchChannel(src)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].rel < rows[j].rel })

	legacyCount, currentCount, pinnedCount, newOnly := 0, 0, 0, 0
	for _, row := range rows {
		state := "no-temp-use"
		if row.current || row.legacy {
			switch {
			case row.pinned:
				state = "PIN"
				pinnedCount++
			default:
				state = "NO-PIN"
			}
		}
		if row.legacy {
			legacyCount++
		}
		if row.current {
			currentCount++
		}
		if row.current && !row.legacy {
			newOnly++
			t.Errorf("%s creates scratch through %s — a channel the ORIGINAL gate marker set did not "+
				"recognise, and it has no TestMain pin (the widened gate would have missed it before this audit)",
				row.rel, row.evidence)
		}
		if row.current && !row.pinned {
			t.Errorf("%s", scratchPinCoverageFailure(root, row))
		}
		t.Logf("%-42s %-10s %-9s %-7s %s", row.rel,
			pick(row.legacy, "yes", "-"), pick(row.current, "yes", "-"),
			pick(row.pinned, "PIN", "-"), state)
	}
	t.Logf("packages: %d scanned | scratch-using: %d by the ORIGINAL markers, %d by the widened markers "+
		"(+%d newly detected) | pinned: %d", len(rows), legacyCount, currentCount, newOnly, pinnedCount)

	if legacyCount == 0 || currentCount == 0 {
		t.Fatalf("the scan found %d scratch-using packages under the original markers and %d under the widened "+
			"ones — a zero means the scan is looking in the wrong place (vacuous pass guard)", legacyCount, currentCount)
	}
	if currentCount < legacyCount {
		t.Fatalf("the widened marker set detects FEWER packages (%d) than the original (%d) — coverage regressed",
			currentCount, legacyCount)
	}
}

// scratchChannel names the creation channel a package uses, so the failure text
// says which one (a pin fixes all of them, but the diagnosis differs).
func scratchChannel(src string) string {
	var hits []string
	for _, marker := range scratchCreationMarkers {
		if strings.Contains(src, marker) {
			hits = append(hits, marker)
		}
	}
	return strings.Join(hits, ", ")
}

// pick renders a boolean as a table cell.
func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

// TestUsesScratchTempDetectsAmbientChannels is the minimal sample for the
// widened detector (the repo currently has NO unpinned scratch-using package,
// so the widening's live effect is zero and its correctness must be shown on
// synthetic sources instead of inferred from a green gate).
func TestUsesScratchTempDetectsAmbientChannels(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"tempdir", `dir := t.TempDir()`, true},
		{"mkdirtemp-empty", `d, _ := os.MkdirTemp("", "x")`, true},
		{"ambient-tempdir", `p := filepath.Join(os.TempDir(), "x.db")`, true},
		{"subtemptest", `d, _ := os.MkdirTemp("/tmp", "probe-")`, true},
		{"subtestcreatetemp", `f, _ := os.CreateTemp("/tmp", "log-*")`, true},
		// Inert paths are NOT creation channels: a path that is only parsed,
		// compared or passed as an argument must not force a pin.
		{"inert-flag", `run(t, "--db", "/tmp/x.db")`, false},
		{"inert-literal", `want := "/tmp/whatever"`, false},
		{"comment", `// /tmp/os.TempDir? no: neither marker here`, false},
		{"empty", ``, false},
	}
	for _, c := range cases {
		if got := usesScratchTemp(c.src); got != c.want {
			t.Errorf("usesScratchTemp(%s: %q) = %v, want %v", c.name, c.src, got, c.want)
		}
	}
	// The widened set is a superset of the original one: nothing the old gate
	// caught may be dropped by the new one.
	for _, marker := range legacyTempMarkers {
		found := false
		for _, m := range scratchCreationMarkers {
			if m == marker {
				found = true
			}
		}
		if !found {
			t.Errorf("marker %q was dropped by the widened set — coverage regressed", marker)
		}
	}
}

// TestAmbientTmpRootCreationPredicate is the minimal sample for the SECOND
// half of the Go /tmp gate: a test can create scratch at /tmp without ever
// naming it (os.MkdirTemp("", …) means os.TempDir()). The literal predicate
// above returns false for every one of these lines, so without this predicate
// the gate would report the suite clean while the ambient writes happen.
func TestAmbientTmpRootCreationPredicate(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"mkdirtemp-empty", `dir, err := os.MkdirTemp("", agentSuiteScratchPrefix)`, true},
		{"createtemp-empty", `f, err := os.CreateTemp("", "probe-*")`, true},
		{"os-tempdir-mkdirall", `os.MkdirAll(filepath.Join(os.TempDir(), "x"), 0o755)`, true},
		{"tempdir-pinned", `dir, err := os.MkdirTemp(testutil.SuiteTempRoot(), prefix)`, false},
		{"tempdir-scratchqueue", `dir := testutil.ScratchRootQueue()`, false},
		{"t-tempdir", `dir := t.TempDir()`, false},
		// Building a PATH from the ambient root is not itself a write: the
		// predicate answers "does this line create something at /tmp", and the
		// creating call is what carries the action. (A path built on one line
		// and written two lines later is the documented same-line blind spot —
		// the honest limit of a static scan.)
		{"inert-path-join", `p := filepath.Join(os.TempDir(), "x.db")`, false},
		{"inert-argument", `run(t, "--db", filepath.Join(os.TempDir(), "x.db"))`, false},
		{"inert-compare", `if got != os.TempDir() {`, false},
		{"comment-only", `// never os.MkdirTemp("", "x") here`, false},
	}
	for _, c := range cases {
		if got := ambientTmpRootIsCreatedBy(c.line); got != c.want {
			t.Errorf("ambientTmpRootIsCreatedBy(%s: %q) = %v, want %v", c.name, c.line, got, c.want)
		}
	}
	// The two predicates are complementary, not overlapping: the ambient form
	// carries no /tmp literal, so the first one must NOT claim it — otherwise
	// this second rule would be dead weight hiding behind the first.
	for _, line := range []string{
		`dir, err := os.MkdirTemp("", "probe-")`,
		`p := filepath.Join(os.TempDir(), "x.db")`,
	} {
		if tmpPathIsCreatedBy(line) {
			t.Errorf("tmpPathIsCreatedBy(%q) = true — the literal rule is reaching an ambient path; "+
				"the two rules must stay separable (the audit's premise)", line)
		}
	}
}

// TestTmpPathCreationPredicate is the minimal sample for the Go /tmp gate's
// predicate: the same literal is a defect when a creating call consumes it and
// inert when it is only data. Without this, the gate's 324-file clean run
// proves nothing about what it would catch.
func TestTmpPathCreationPredicate(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"build-into-tmp", `cmd := exec.Command("go", "build", "-o", "/tmp/gopi-version-test", ".")`, true},
		{"mkdirtemp", `d, err := os.MkdirTemp("/tmp", "probe-")`, true},
		{"writetest", `t.Errorf("%s", "/tmp/." + x11UnixSocketPrefix)`, false},
		{"removeall", `t.Cleanup(func() { os.RemoveAll("/tmp/scratch-probe") })`, true},
		{"chdir", `if err := os.Chdir("/tmp/x"); err != nil {`, true},
		{"inert-arg", `opts, err := parseVisionArgs([]string{"/tmp/x.png", "--prompt", "p"})`, false},
		{"inert-compare", `if opts.imagePath != "/tmp/x.png" {`, false},
		{"inert-map", `claudeToolUseStream(w, "read", map[string]interface{}{"path": "/tmp/x"})`, false},
		{"comment", `// we used to build into /tmp/gopi-version-test here`, false},
		{"no-tmp", `d, err := os.MkdirTemp("", "probe-")`, false},
	}
	for _, c := range cases {
		if got := tmpPathIsCreatedBy(c.line); got != c.want {
			t.Errorf("tmpPathIsCreatedBy(%s: %q) = %v, want %v", c.name, c.line, got, c.want)
		}
	}
}

// TestPinCoverageTableIsNotVacuous guards the table itself: the scan must see
// the packages the repo is known to contain (the pin implementation's own
// package is the anchor — if the walk cannot find it, the table is meaningless).
func TestPinCoverageTableIsNotVacuous(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	dirs := testPackageDirs(t, root)
	want := filepath.ToSlash(filepath.Join("internal", "testutil"))
	found := false
	for _, d := range dirs {
		if d == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the package walk did not find %s among %d test packages: %v", want, len(dirs), dirs)
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || info.IsDir() {
		t.Fatalf("repo root %s does not hold a go.mod — the walk is not rooted at the module", root)
	}
	fmt.Fprintln(os.Stderr, "")
}
