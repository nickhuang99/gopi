// declared_budget_runs_evidence_test.go — the two figures a reader nearly
// misreads on the budget-runs tree, pinned so a future change cannot quietly
// re-spell them as something they are not.
//
// THE MISREADING THIS GUARDS (measured 2026-10-27, nick-sager, tree
// `budget-runs`): the tree printed `collectable=0 kept=429 holding=9172227123
// holding_n=429` while 19790 FLAT per-run evidence files held a further ~7.0 GiB
// at ages up to fifteen days. Two sentences in the shipped docs invited a wrong
// conclusion from that line — `TreeHeldTotal`'s comment said "AND that a live
// process holds" (the body consults no holder at all), and
// declared_budget_runs.go said the age-GC'd directories were "what bounds the
// tree's bytes" (the flat half is unbounded and is the majority of the bytes).
// Both were corrected; these arms are what makes the correction checkable
// instead of prose.
//
// ⚠ THE FIRST VERSION OF THIS FILE WROTE INTO THE OPERATOR'S LIVE SCRATCH TREE
// AND TRUNCATED A REAL FILE. It pinned `GOPI_HOME`, which is NOT the variable
// that resolves the scratch root — the root comes from
// `scratch.HomeDirEnvName` (`GOPI_TEST_SCRATCH_HOME`), so `HomeDir()` fell back
// to `<home>/workspace/gopi-test-scratch` and the fixture's `os.WriteFile` of
// `stamps.log` replaced the live run log (measured 1187568 bytes before, 6 bytes
// after). The pin below is the ONE name that matters, and `pinScratchHome` ALSO
// asserts the resolved root really lies inside the temp home and refuses to run
// otherwise — a test that cannot prove where it is about to write must not write.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveStampsLogBytes is the measured size of the operator's real
// <scratch home>/queue/test-budget/stamps.log immediately BEFORE the first
// version of this file truncated it (2026-10-27: 1187568 bytes; after: 6). It is
// recorded here because the refusal message must state the cost, not just the
// problem — a guard whose failure says "this would be bad" teaches nothing.
const liveStampsLogBytes = 1187568

// pinScratchHome points the scratch root at a fresh temp directory and returns
// it ONLY after proving the tree this file is about to touch resolved inside it.
//
// It takes no shortcut: the same `HomeDir()` the production code calls is the
// one it checks, so a rename of the env var, a second override with precedence
// over it, or a future fallback that ignores it all fail HERE rather than as
// writes into the operator's real tree.
func pinScratchHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(HomeDirEnvName, home)
	if resolved := HomeDir(); resolved != home {
		t.Fatalf("scratch root resolved to %q, not to the pinned temp home %q — refusing to run: a fixture that cannot prove "+
			"where it writes would write into the operator's live tree (this file did exactly that once, and truncated %d "+
			"bytes of stamps.log)", resolved, home, liveStampsLogBytes)
	}
	return home
}

// assertInsideTree refuses to touch a path that is not inside the pinned tree
// root. It is the SECOND half of the same guard: pinScratchHome proves the ROOT
// moved, and this proves the path about to be written really hangs off it — an
// absolute path built from anything else (a fallback, a cached root, a sibling
// resolver) is caught here rather than in the operator's directory.
func assertInsideTree(t *testing.T, home, path string) {
	t.Helper()
	if !strings.HasPrefix(path, home+string(filepath.Separator)) {
		t.Fatalf("refusing to touch %q: it is outside the pinned scratch home %q", path, home)
	}
}

// TestTreeHeldTotal_ConsultsNoHolderAndEqualsTheProbe pins the DEFINITION of
// holding= against a live holder's own record.
//
// The fixture holds two run directories: one registered as held by this process
// (RegisterHolder writes the `.holder` record the sweep reads) and one not.
// A "held" reading would count ONE; the field's real meaning counts BOTH, and
// that number is exactly what ProbeDeclaredTree reports as entries=. If someone
// ever gives TreeHeldTotal a holder filter — a defensible change, but a
// DIFFERENT field — this test fails and says so, instead of leaving the docs to
// disagree with the body in silence.
func TestTreeHeldTotal_ConsultsNoHolderAndEqualsTheProbe(t *testing.T) {
	home := pinScratchHome(t)
	root, err := BudgetRunsRoot()
	if err != nil {
		t.Fatalf("resolving the %s tree: %v", BudgetRunsTreeName, err)
	}
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatal(err)
	}
	assertInsideTree(t, home, root)
	tree, ok := treeByName(BudgetRunsTreeName)
	if !ok {
		t.Fatalf("the %s tree must be declared", BudgetRunsTreeName)
	}

	heldDir := filepath.Join(root, "run-100-1"+BudgetRunTestedBinSuffix)
	freeDir := filepath.Join(root, "run-200-2"+BudgetRunTestedBinSuffix)
	for _, d := range []string{heldDir, freeDir} {
		if err := os.MkdirAll(d, DirPerm); err != nil {
			t.Fatal(err)
		}
		// The run's own binary: what the directory holds and what its size is
		// measured from.
		if err := os.WriteFile(filepath.Join(d, "gopi"), make([]byte, 4096), FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	RegisterHolder(heldDir)
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(heldDir, HolderFileName)) //stylegate:ignore t.Cleanup removal of the registration this test wrote; the assertions it supports already ran
	})

	bytes, count := TreeHeldTotal(tree)
	if count != 2 {
		t.Fatalf("holding_n = %d, want 2 — the figure counts EVERY entry the tree's namespace rule matches, held or not; a count of "+
			"one would mean a holder filter was added and the field's meaning changed", count)
	}
	if bytes <= 0 {
		t.Fatalf("holding = %d bytes, want > 0 for two non-empty run directories", bytes)
	}
	probe := ProbeDeclaredTree(tree)
	if probe.Entries != count {
		t.Fatalf("probe entries = %d but holding_n = %d — the two are the SAME walk by construction, and a divergence means "+
			"one of them grew its own filter", probe.Entries, count)
	}
}

// TestBudgetRuns_FlatEvidenceIsNeverACandidateAndIsNotCountedInHolding pins the
// half of this tree that no window can ever collect — and that holding= cannot
// see either.
//
// THE FIXTURE IS THE MEASURED SHAPE: one aged run DIRECTORY (a candidate once it
// is older than the window) beside the flat evidence a run writes — the stream,
// the start record, the package lists, stamps.log — seeded at an age FAR past
// the window. The aged directory must be collectable at that age (the red-then-
// green direction this tree exists for); every flat entry must be neither a
// candidate nor part of holding=, at ANY age.
func TestBudgetRuns_FlatEvidenceIsNeverACandidateAndIsNotCountedInHolding(t *testing.T) {
	home := pinScratchHome(t)
	root, err := BudgetRunsRoot()
	if err != nil {
		t.Fatalf("resolving the %s tree: %v", BudgetRunsTreeName, err)
	}
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatal(err)
	}
	assertInsideTree(t, home, root)
	tree, ok := treeByName(BudgetRunsTreeName)
	if !ok {
		t.Fatalf("the %s tree must be declared", BudgetRunsTreeName)
	}

	// An age five times the window: nothing here can be argued to be young.
	old := 5 * BudgetRunsStaleAge

	agedRun := filepath.Join(root, "run-300-3"+BudgetRunTestedBinSuffix)
	if err := os.MkdirAll(agedRun, DirPerm); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(agedRun, "gopi")
	if err := os.WriteFile(bin, make([]byte, 8192), FilePerm); err != nil {
		t.Fatal(err)
	}
	// The flat evidence classes, seeded beside it with the same age. The start
	// record's name comes from the tree's OWN declared constant so a rename
	// cannot leave this fixture testing a shape no writer produces any more.
	flat := map[string][]byte{
		"run-300-3.json":                      make([]byte, 4096),
		"run-300-3" + BudgetStartRecordSuffix: []byte("run-300-3 stream\n"),
		"run-300-3-scope-packages.txt":        []byte("./internal/scratch\n"),
		"run-300-3-stream-packages.txt":       []byte("./cmd/agent\n"),
		"stamps.log":                          []byte("stamp\n"),
	}
	for name, body := range flat {
		if err := os.WriteFile(filepath.Join(root, name), body, FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().Add(-old)
	for _, d := range []string{root, agedRun} {
		if err := os.Chtimes(d, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range append([]string{bin}, flatPaths(root, flat)...) {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("planning the aged tree: %v", err)
	}
	planned := map[string]bool{}
	for _, e := range plan {
		planned[e.Path] = true
	}
	if !planned[agedRun] {
		t.Fatalf("the aged run directory was NOT planned at age %s — the age window is not doing its job, and this fixture would "+
			"pass while pinning nothing (planned: %v)", old, plan)
	}
	for name := range flat {
		if planned[filepath.Join(root, name)] {
			t.Fatalf("flat evidence %q was planned for collection at age %s — a flat child of this tree is EVIDENCE for a reader "+
				"chasing a run that never completed, and must never be a candidate at any age", name, old)
		}
	}

	// holding= counts the directory and NOT the flat evidence: the flat bytes are
	// real bytes on the disk that this field does not see, which is why a reader
	// must not take holding= for the tree's size.
	bytes, count := TreeHeldTotal(tree)
	if count != 1 {
		t.Fatalf("holding_n = %d, want 1 (the one run directory) — flat evidence is not counted by this field", count)
	}
	var flatTotal int64
	for _, body := range flat {
		flatTotal += int64(len(body))
	}
	if bytes < 8192 {
		t.Fatalf("holding = %d bytes, want at least the 8192-byte binary inside the run directory", bytes)
	}
	if bytes > 8192+flatTotal {
		t.Fatalf("holding = %d bytes, more than the directory's 8192-byte payload plus the flat evidence (%d) — this field is "+
			"namespace-filtered and must not start counting what the tree's rule refuses", bytes, flatTotal)
	}

	// The explain face must say the same thing: the directory has a verdict, the
	// flat entries have none (they are not verdict lines at all).
	verdicts, summary := DeclaredTreeVerdicts(tree)
	if summary.Collectable != 1 {
		t.Fatalf("collectable = %d, want 1 (the aged run directory)", summary.Collectable)
	}
	if summary.NotOurs != len(flat) {
		t.Fatalf("not_ours = %d, want %d (the flat evidence, counted by SHAPE — not an ownership verdict)", summary.NotOurs, len(flat))
	}
	for _, v := range verdicts {
		for name := range flat {
			if strings.HasSuffix(v.Path, string(filepath.Separator)+name) {
				t.Fatalf("flat evidence %q produced a verdict line (%q); flat entries are examined-and-refused facts, not "+
					"verdict lines", name, v.Reason)
			}
		}
	}
}

// flatPaths is the fixture's own path list — spelled once so the age-stamping
// loop and the assertions cannot disagree about which files were seeded.
func flatPaths(root string, flat map[string][]byte) []string {
	out := make([]string, 0, len(flat))
	for name := range flat {
		out = append(out, filepath.Join(root, name))
	}
	return out
}
