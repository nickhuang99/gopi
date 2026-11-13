package standardgate

// The gate and its proofs for exported symbols declared in `_test.go` files
// (PROJECT.md Rule 43). The mechanism, the incident it came from and what it
// deliberately does NOT judge are documented in exported_test_symbol_gate.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportedTestSymbolBaselinePath is the frozen table of violations that predate
// the gate. It ratchets DOWN: rows are deleted as the symbols are cleaned, and a
// row that stops firing is an error.
const exportedTestSymbolBaselinePath = "internal/standards/testdata/exported-test-symbol-baseline.tsv"

// TestExportedTestSymbolRedProof plants, into a synthetic tree, the exact shape
// this gate exists for — and its neighbours that must stay clean, so the proof
// cannot pass by refusing everything.
func TestExportedTestSymbolRedProof(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	// ── THE VIOLATION, and it is the measured 0e9fdbca shape: an exported
	// declaration in a test file whose ONLY mention outside it is a COMMENT
	// ("exported so the CLI sweep shares the renderer shape").
	write("pkg/report_test.go", `package pkg

// ReportLine renders a report (exported so the CLI sweep shares the shape).
func ReportLine() string { return "x" }

// SettledPredicate has no reader anywhere at all.
func SettledPredicate() bool { return true }
`)
	write("pkg/sweep_test.go", `package pkg

// The CLI sweep used to call ReportLine and SettledPredicate — a memory of a
// caller is not a caller (Rule 43's homophone trap).
func TestSomething(t *testing.T) {}
`)

	// ── CLEAN CONTROLS. Each of these must NOT be reported, or the predicate is
	// refusing exports rather than vacuous ones.
	write("pkg/ok_test.go", `package pkg

// Shared lives here and is used by its neighbour: a real reader.
func Shared() int { return 1 }

// Trusted is exported from a test file and carries the inline escape.
// exported-test-symbol-ok: the entry point go test's contract cannot reach
func Trusted() int { return 2 }

// TestEntryPointsAreNotExports covers the naming contract.
func TestEntryPointsAreNotExports(t *testing.T) {}
`)
	write("pkg/ok_reader_test.go", `package pkg

import "testing"

func TestUsesShared(t *testing.T) {
	if Shared() != 1 {
		t.Fatal("no")
	}
	_ = Trusted()
}
`)

	// ── THE PACKAGE-CLAUSE CONTROL: the same directory, a DIFFERENT package
	// clause, so `external` can never call `OnlyInternal`, and the name in the
	// external file is not a reader for the internal one.
	write("pkg/ext_test.go", `package pkg_test

func TestExternal(t *testing.T) { OnlyInternal() }
`)
	write("pkg/only_internal_test.go", `package pkg

// OnlyInternal is read by the OTHER package clause in this directory — not by
// this one, so the export is vacuous where it lives.
func OnlyInternal() {}
`)

	walked, judged, findings, err := exportedTestSymbolScan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if walked == 0 || judged == 0 {
		t.Fatalf("the probe tree judged nothing (walked=%d judged=%d) — a scan that looked at no file proves nothing", walked, judged)
	}
	got := map[string]exportedTestSymbolFinding{}
	for _, f := range findings {
		got[f.Symbol] = f
	}
	for _, want := range []string{"ReportLine", "SettledPredicate", "OnlyInternal"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s must be reported: it is exported from a `_test.go` file with no CODE reader in its own package", want)
		}
	}
	for _, clean := range []string{"Shared", "Trusted", "TestEntryPointsAreNotExports", "TestUsesShared"} {
		if _, ok := got[clean]; ok {
			t.Errorf("%s must NOT be reported — the predicate is refusing exports instead of vacuous ones", clean)
		}
	}
	if f, ok := got["ReportLine"]; ok {
		if len(f.ProseOnly) != 1 || !strings.HasSuffix(f.ProseOnly[0], "sweep_test.go") {
			t.Errorf("ReportLine's finding must name the prose-only mention (sweep_test.go), got %v — the two shapes "+
				"(\"nothing names it\" / \"only a comment names it\") are different evidence", f.ProseOnly)
		}
	}
	if f, ok := got["SettledPredicate"]; ok && len(f.ProseOnly) != 1 {
		t.Errorf("SettledPredicate is named in a comment of sweep_test.go but in no code, so its ProseOnly must list "+
			"that file, got %v", f.ProseOnly)
	}
}

// TestExportedTestSymbolJudgeRefusesAStalePermit pins the ratchet: a baseline row
// whose violation is gone is an error, not a harmless leftover.
func TestExportedTestSymbolJudgeRefusesAStalePermit(t *testing.T) {
	findings := []exportedTestSymbolFinding{{File: "pkg/a_test.go", Symbol: "Kept"}}
	rows := []exportedTestSymbolBaselineRow{
		{File: "pkg/a_test.go", Symbol: "Kept", Reason: "frozen"},
		{File: "pkg/a_test.go", Symbol: "Cleaned", Reason: "frozen"},
	}
	unexplained, stale := exportedTestSymbolJudge(findings, rows)
	if len(unexplained) != 0 {
		t.Errorf("a permitted finding must not be reported as unexplained: %v", unexplained)
	}
	if len(stale) != 1 || stale[0].Symbol != "Cleaned" {
		t.Errorf("the permit for a CLEANED symbol must be reported as stale, got %v", stale)
	}
	newcomer := []exportedTestSymbolFinding{{File: "pkg/b_test.go", Symbol: "Fresh"}}
	unexplained, _ = exportedTestSymbolJudge(newcomer, rows)
	if len(unexplained) != 1 || unexplained[0].Symbol != "Fresh" {
		t.Errorf("a finding with no permit must be reported, got %v", unexplained)
	}
}

// TestExportedTestSymbolBaselineRefusesMalformedRows — a dropped permit must not
// read like a permit nobody needed (Rule 15).
func TestExportedTestSymbolBaselineRefusesMalformedRows(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"two-columns": "pkg/a_test.go\tKept\n",
		"no-reason":   "pkg/a_test.go\tKept\t\n",
		"no-symbol":   "pkg/a_test.go\t\tfrozen\n",
	}
	for name, body := range cases {
		p := filepath.Join(dir, name+".tsv")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := exportedTestSymbolBaseline(p); err == nil {
			t.Errorf("%s: a malformed row must be an ERROR, not a skipped line", name)
		}
	}
	p := filepath.Join(dir, "ok.tsv")
	if err := os.WriteFile(p, []byte("# comment\n\npkg/a_test.go\tKept\tfrozen\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, err := exportedTestSymbolBaseline(p)
	if err != nil || len(rows) != 1 {
		t.Fatalf("a well-formed table must parse to 1 row, got %d/%v", len(rows), err)
	}
}

// TestExportedTestSymbolGateJudgesTheRealTree is the gate over this repository:
// every exported `_test.go` declaration must have a code reader in its own
// package, carry the inline escape, or be frozen in the baseline table. It prints
// its own counts and enforces a floor, so a scan that looked at nothing cannot
// pass as a clean tree.
func TestExportedTestSymbolGateJudgesTheRealTree(t *testing.T) {
	root := repoRoot(t)
	walked, judged, findings, err := exportedTestSymbolScan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if walked < exportedTestSymbolScanFloor {
		t.Fatalf("walked only %d `_test.go` file(s) (floor %d) — the scan is broken, so a green here proves nothing",
			walked, exportedTestSymbolScanFloor)
	}
	if judged == 0 {
		t.Fatalf("walked %d test file(s) and judged 0 exported declaration(s) — the parse is broken", walked)
	}
	rows, err := exportedTestSymbolBaseline(filepath.Join(root, filepath.FromSlash(exportedTestSymbolBaselinePath)))
	if err != nil {
		t.Fatalf("baseline %s: %v — the table is the gate's allow-list and a table that cannot be read "+
			"would silently permit nothing (or everything)", exportedTestSymbolBaselinePath, err)
	}
	unexplained, stale := exportedTestSymbolJudge(findings, rows)
	for _, f := range unexplained {
		detail := "nothing outside it names it"
		if len(f.ProseOnly) > 0 {
			detail = "only a COMMENT names it (" + strings.Join(f.ProseOnly, ", ") + ")"
		}
		t.Errorf("%s:%d: %s %s is EXPORTED from a `_test.go` file and has no reader in its own package — %s.\n"+
			"    An export is a claim that something outside the package consumes it; from a test file nothing\n"+
			"    checks that claim, which is how a retired production function kept its exported spelling here\n"+
			"    (measured 2026-10-31, task 01a18f76). Unexport it (Rule 43), or — if it must stay exported —\n"+
			"    write `// %s <reason>` on the line above it.",
			f.File, f.Line, f.Kind, f.Symbol, detail, exportedTestSymbolEscape)
	}
	for _, r := range stale {
		t.Errorf("%s: the baseline permits %s (%q) but nothing violates it any more — delete the row; the "+
			"allow-list ratchets DOWN only, and a stale permit is a silent permission", exportedTestSymbolBaselinePath, r.Symbol, r.Reason)
	}
	t.Logf("exported-test-symbol gate: walked %d test file(s), judged %d exported declaration(s), "+
		"%d finding(s), %d baselined, %d unexplained, %d stale permit(s)", walked, judged, len(findings), len(rows), len(unexplained), len(stale))
}
