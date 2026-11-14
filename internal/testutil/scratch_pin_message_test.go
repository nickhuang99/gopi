package testutil

// The pin gate's FAIL text is an INSTRUCTION a reader acts on, so it is gated
// like one: TestEveryTempUsingSuiteIsPinned's predicate is right for every
// package, but the REMEDY is shape-dependent, and the shape is decided by the
// filesystem (does this package belong to the gopi module, or to a standalone
// nested one with its own go.mod?).
//
// Measured 2026-10-13 (task 01a13352): devdoc/doubao-asr-eval/minclient was
// reported by BOTH gates while the text prescribed
// `testutil.RunSuiteMain(m, "minclient")` — an import Go refuses twice over for
// that module ("no required module provides package", then "use of internal
// package ... not allowed"), whose acceptance would also pull the whole gopi
// dependency tree into a module declared zero-dependency. The gate was right;
// the instruction was not executable.
//
// This file pins BOTH halves: the diagnosis is unchanged for both shapes (the
// predicate is untouched), and each shape is handed a remedy it can actually
// carry out.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The package shapes and the fixture/real paths this file asserts on, one
// named constant each (Rules 1/13): the values decide WHICH remedy is
// rendered, so a bare literal would be a second source of truth.
const (
	inTreeShape = "in-tree"
	nestedShape = "nested"

	// nestedFixtureModule is the nested module the fixtures below declare. Its
	// shape mirrors what the gate meets under devdoc/: a standalone module in
	// an in-tree directory.
	nestedFixtureModule = "module fixture-nested\n\ngo 1.24.0\n"

	// inTreeFixturePkg is an in-tree package path under the synthetic fixture
	// root, which declares no nested module of its own.
	inTreeFixturePkg     = "internal/somepkg"
	inTreeFixturePkgLeaf = "somepkg"

	// nestedFixtureModuleRoot / nestedFixturePkgLeaf describe the nested shape
	// the fixture builds: a package inside a standalone module.
	nestedFixtureModuleRoot = "devdoc/somemodule"
	nestedFixturePkgLeaf    = "minclient"
)

// realNestedModulePackages are the module roots under devdoc/ that declare
// their own go.mod — the six standalone modules the gate walks on every run.
// Each is verified to still hold its go.mod before anything is asserted about
// it, so a moved module fails LOUDLY instead of passing vacuously.
var realNestedModulePackages = []string{
	"devdoc/doubao-asr-eval/minclient",
	"devdoc/doubao-asr-eval/aucfile",
	"devdoc/doubao-asr-eval/sdk-snapshot",
	"devdoc/voice-zerodep-poc",
	"devdoc/voice-zerodep-poc/memfd",
	"devdoc/voice-zerodep-poc/memfdelf",
}

// inTreeRealPackages are packages of the gopi module itself: none of them may
// be misread as nested merely because nested modules exist elsewhere in the
// tree.
var inTreeRealPackages = []string{"internal/agent", "internal/testutil", "cmd/agent"}

// TestScratchPinFailureNamesTheRemedyForEachShape is the regression guard for
// the FAIL text: a nested standalone module must be told a remedy it can run
// AND must not be handed the in-tree one; an in-tree package must keep both of
// its own forms.
func TestScratchPinFailureNamesTheRemedyForEachShape(t *testing.T) {
	inTree := scratchPinFailure(t.TempDir(), inTreeFixturePkg)
	nestedRoot := t.TempDir()
	nestedPkg := nestedFixtureModuleRoot + "/" + nestedFixturePkgLeaf
	writeFixtureModule(t, filepath.Join(nestedRoot, filepath.FromSlash(nestedPkg)))
	nested := scratchPinFailure(nestedRoot, nestedPkg)

	t.Run("diagnosis is one predicate for both shapes", func(t *testing.T) {
		for _, tc := range []struct{ shape, msg string }{
			{inTreeShape, inTree},
			{nestedShape, nested},
		} {
			if !strings.Contains(tc.msg, scratchPinVerdict) {
				t.Errorf("%s FAIL text does not carry the shared diagnosis — the predicate must not differ by shape.\ngot: %s", tc.shape, tc.msg)
			}
			if !strings.Contains(tc.msg, tc.shape) {
				t.Errorf("%s FAIL text does not name the package's shape (%q), so a reader cannot tell which remedy applies.\ngot: %s",
					tc.shape, tc.shape, tc.msg)
			}
		}
	})

	t.Run("in-tree package keeps the internal/testutil pin", func(t *testing.T) {
		want := fmt.Sprintf(scratchPinFixInTree, inTreeFixturePkgLeaf)
		if !strings.Contains(inTree, want) {
			t.Errorf("in-tree FAIL text no longer prescribes the internal/testutil pin:\nwant substring: %s\ngot: %s", want, inTree)
		}
		if !strings.Contains(inTree, scratchPinFixInTreeOwnRoot) {
			t.Errorf("in-tree FAIL text does not offer the own-TMPDIR form for a package that cannot import internal/testutil.\ngot: %s", inTree)
		}
		if strings.Contains(inTree, scratchPinFixNestedModule) {
			t.Errorf("in-tree FAIL text offers the NESTED remedy — the wrong shape for an in-tree package.\ngot: %s", inTree)
		}
	})

	t.Run("nested module is handed the nested remedy, never the in-tree one", func(t *testing.T) {
		if !strings.Contains(nested, nestedPkg) {
			t.Errorf("nested FAIL text does not name the package.\ngot: %s", nested)
		}
		// The SIBLING gate's message must not drift into the in-tree-only
		// instruction either: both gates report the same class, so a reader must
		// not get one remedy from scripts_gate_test.go and another here.
		sibling := scratchPinCoverageFailure(nestedRoot, pinCoverageRow{rel: nestedPkg, evidence: scratchPinVerdict})
		if !strings.Contains(sibling, scratchPinFixNestedModule) {
			t.Errorf("nested sibling (pin-coverage) text does not list the remedies that exist inside the module.\ngot: %s", sibling)
		}
		if strings.Contains(sibling, fmt.Sprintf(scratchPinFixInTree, nestedFixturePkgLeaf)) {
			t.Errorf("nested sibling (pin-coverage) text prescribes the in-tree pin, which Go refuses for a nested module.\ngot: %s", sibling)
		}
		if !strings.Contains(nested, nestedShape+", standalone nested module") {
			t.Errorf("nested FAIL text does not name the module that decides the shape (its own go.mod), so a reader cannot tell the in-tree remedy is out of reach.\ngot: %s", nested)
		}
		// The unactionable instruction is the defect this text repairs: for a
		// nested module Go offers no github.com/nickhuang99/gopi edge and internal/ is
		// decided by import path, so the import CANNOT be written.
		impossible := fmt.Sprintf(scratchPinFixInTree, nestedFixturePkgLeaf)
		if strings.Contains(nested, impossible) {
			t.Errorf("nested FAIL text prescribes the in-tree pin (%s), which Go refuses for a nested module — the exact defect this text repairs.\ngot: %s",
				impossible, nested)
		}
		if !strings.Contains(nested, scratchPinFixNestedModule) {
			t.Errorf("nested FAIL text does not list the remedies that exist inside the module.\ngot: %s", nested)
		}
		if !strings.Contains(nested, scratchPinWhyNestedNotImportable) {
			t.Errorf("nested FAIL text does not state WHY the in-tree pin is unavailable (the next reader would retry it).\ngot: %s", nested)
		}
	})
}

// TestScratchPinShapeDetectionReadsTheRealTree pins the detector against the
// REAL repository: the fixtures above prove the rule is implemented, this one
// proves it fires for the nested modules the gate actually meets.
func TestScratchPinShapeDetectionReadsTheRealTree(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	for _, rel := range realNestedModulePackages {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel), nestedModuleMarker)); statErr != nil {
			t.Fatalf("%s does not hold a %s (%v) — the list is stale, so this test would pass vacuously",
				rel, nestedModuleMarker, statErr)
		}
		if got := scratchPinPackageKind(root, rel); got != nestedShape {
			t.Errorf("%s classified %q, want %q — a nested module handed the in-tree remedy is the defect this text repairs",
				rel, got, nestedShape)
		}
		// The rendered text for the real package must not carry the impossible
		// instruction either (the fixture case above is the synthetic twin).
		msg := scratchPinFailure(root, rel)
		if strings.Contains(msg, fmt.Sprintf(scratchPinFixInTree, filepath.Base(rel))) {
			t.Errorf("%s FAIL text prescribes the in-tree pin, which Go refuses for this module.\ngot: %s", rel, msg)
		}
	}
	for _, rel := range inTreeRealPackages {
		if got := scratchPinPackageKind(root, rel); got != inTreeShape {
			t.Errorf("%s classified %q, want %q", rel, got, inTreeShape)
		}
	}
}

// TestNestedRemedyIsARecognisedPinShape keeps the FAIL TEXT and the PREDICATE
// from disagreeing — the gap this pair of tasks closes.
//
// Task 01a13352 taught the text to hand a nested standalone module a remedy it
// can actually run: redirect the pin env name in that module's OWN TestMain. The
// predicate could not SEE that remedy, so a module repairing itself exactly as
// instructed stayed reported NO-PIN for ever. The scan in
// pin_redirect_scan_test.go closes it, and this test states the agreement as a
// check: the remedy the nested text prescribes, carried out in a package of the
// nested shape, must be the very thing that flips packagePinned to true.
func TestNestedRemedyIsARecognisedPinShape(t *testing.T) {
	root := t.TempDir()
	nestedPkg := nestedFixtureModuleRoot + "/" + nestedFixturePkgLeaf
	dir := filepath.Join(root, filepath.FromSlash(nestedPkg))
	writeFixtureModule(t, dir)
	// The module repairs itself exactly as remedy (2) instructs, in its own
	// TestMain, with no in-tree import anywhere (that module has none to make).
	writeProbeSource(t, dir, "package "+nestedFixturePkgLeaf+"\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n"+
		"func TestMain(m *testing.M) {\n"+
		"\tos.Setenv("+quoted(scratchTempRootVar)+", probeScratchDir)\n"+
		"\tos.Exit(m.Run())\n}\n")

	// The text must be the NESTED one (the shape this test is about), and it must
	// keep prescribing the remedy that the predicate now recognises.
	msg := scratchPinFailure(root, nestedPkg)
	if !strings.Contains(msg, scratchPinFixNestedModule) {
		t.Fatalf("the nested FAIL text no longer prescribes the module's own remedy:\n%s", msg)
	}
	if !strings.Contains(scratchPinFixNestedModule, scratchTempRootVar) {
		t.Fatalf("the nested remedy no longer names %s, so the text and the predicate cannot be about the same "+
			"assignment:\n%s", scratchTempRootVar, scratchPinFixNestedModule)
	}
	if !packagePinned(t, root, nestedPkg) {
		t.Errorf("a nested package that carried out the text's own remedy (2) is still reported NO-PIN — the text "+
			"and the predicate disagree again, which is the defect this pair closes:\n%s", msg)
	}
	// The negative half: the same package with the remedy REDUCED TO PROSE must
	// be NO-PIN, so the agreement above is not satisfied by a marker that any
	// text can claim.
	writeProbeSource(t, dir, "package "+nestedFixturePkgLeaf+"\n\nimport \"testing\"\n\n"+
		"// Remedy (2) would "+pinEnvSetterReceiverName+"."+pinEnvSetterSelector+"("+
		quoted(scratchTempRootVar)+", dir) here.\n"+
		"func TestMain(m *testing.M) { os.Exit(m.Run()) }\n")
	if packagePinned(t, root, nestedPkg) {
		t.Errorf("a nested package whose only claim to a pin is PROSE is reported PIN — the marker must be the "+
			"assignment, never the text of a comment:\n%s", msg)
	}
}

// writeFixtureModule declares a standalone module at dir: the shape that makes
// a package "nested" for the gate's FAIL text.
func writeFixtureModule(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, nestedModuleMarker), []byte(nestedFixtureModule), 0o644); err != nil {
		t.Fatalf("write %s: %v", nestedModuleMarker, err)
	}
}
