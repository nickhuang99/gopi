package standardgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// TestGate8ScratchEnvHomeRedProof is the red proof the acceptance criteria
// require: the gate must REPORT a re-inlined scratch env-name literal, stay
// silent on the two legal shapes (a reference, and a true alias), and not
// punish the prose that explains the contract.
//
// Without this probe the gate could rot into a vacuous no-op and still read
// green — which is exactly how a "one home per value" rule silently stops
// working.
func TestGate8ScratchEnvHomeRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGate8ScratchEnvHomeRedProof")
	memoryBase, resolveBase := scratch.MemoryBaseEnvName, scratch.ResolveBaseEnvName

	// THE DEFECT: a production file that re-inlines the value its neighbour
	// already defines. This is precisely internal/testutil/legacy_residue.go's
	// pre-fix shape, in a third package.
	violating := "package p\n\nconst MemoryBaseEnvName = " + quote(memoryBase) + "\n"
	hits := probeHits(t, g, violating)
	if len(hits) == 0 {
		t.Fatal("RED PROOF FAILED: a re-inlined scratch env-name literal was not reported — the gate is vacuous")
	}
	if !strings.Contains(hits[0].desc, "scratch.MemoryBaseEnvName") {
		t.Errorf("the refusal does not name the reference to use: %q", hits[0].desc)
	}

	// The fixture knob's value is policed the same way.
	resolveViolating := "package p\n\nconst ResolveBaseEnvName = " + quote(resolveBase) + "\n"
	if got := probeHits(t, g, resolveViolating); len(got) == 0 {
		t.Errorf("RED PROOF FAILED: a re-inlined %s was not reported", resolveBase)
	}

	// The declared home IS allowed to spell its own value...
	fset, file := parseSource(t, scratchMemoryBaseHolderFile, "package scratch\n\nconst MemoryBaseEnvName = "+quote(memoryBase)+"\n")
	dupeHits := scratchEnvFind(g, &parsed{rel: scratchMemoryBaseHolderFile, fset: fset, file: file})
	if len(dupeHits) != 0 {
		t.Errorf("the declared home must be allowed to spell its own value: %+v", dupeHits)
	}
	// ...but a SECOND declaration inside the owner package is reported: that is
	// the exact shape that broke the build (two attempts, both landing a
	// definition in internal/scratch).
	fset2, file2 := parseSource(t, "internal/scratch/other.go", "package scratch\n\nconst MemoryBaseEnvName2 = "+quote(memoryBase)+"\n")
	if got := scratchEnvFind(g, &parsed{rel: "internal/scratch/other.go", fset: fset2, file: file2}); len(got) == 0 {
		t.Error("RED PROOF FAILED: a SECOND declaration inside the owner package was not reported — that is the redeclaration that broke the build")
	}

	// COMPLIANT 1 — a reference to the constant (what every consumer must do).
	referencing := `package p

import "github.com/nickhuang99/gopi/internal/scratch"

func f() string { return scratch.MemoryBaseEnvName }
`
	if got := probeHits(t, g, referencing); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on a reference to the ONE definition: %+v", got)
	}

	// COMPLIANT 2 — the TRUE ALIAS Rule 14 prescribes: `const X = scratch.Y`
	// carries no literal at all, so an alias and a mirrored copy are
	// distinguishable by this gate alone. This is the post-fix shape of
	// internal/testutil.
	aliasing := `package p

import "github.com/nickhuang99/gopi/internal/scratch"

const MemoryBaseEnvName = scratch.MemoryBaseEnvName
`
	if got := probeHits(t, g, aliasing); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on the prescribed alias form: %+v", got)
	}

	// COMPLIANT 3 — prose. A doc comment that MENTIONS the variable to explain
	// the contract is not a definition site; the gate walks string literals, so
	// the explanation is deliberately not a hit.
	prose := "package p\n\n// MemoryBase() is the ONE place " + memoryBase + " is read (Rule 2).\nfunc MemoryBase() string { return \"\" }\n"
	if got := probeHits(t, g, prose); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on a doc comment that explains the contract: %+v", got)
	}

	// The escape marker suppresses a REVIEWED exception (and stays visible in
	// the data report, rather than disappearing into a baseline).
	escaped := "package p\n\n// scratch-env-literal-ok: this constant IS the wire interface a child\n// process receives, so the spelling is the value's definition, not a mirror.\nconst childEnvName = " + quote(memoryBase) + "\n"
	escapedHits := probeHits(t, g, escaped)
	if len(escapedHits) != 1 {
		t.Fatalf("the escape probe must still be REPORTED by the predicate (the escape is applied by classify): %+v", escapedHits)
	}
	fset3, file3 := parseSource(t, "probe.go", escaped)
	if cls := classify(g, escapedHits[0], &parsed{rel: "probe.go", fset: fset3, file: file3, src: []byte(escaped)}); cls != classEscaped {
		t.Fatalf("the inline escape did not suppress a reviewed site: class=%v hits=%+v", cls, escapedHits)
	}
}

// quote renders a Go double-quoted string literal for the synthetic probes.
func quote(s string) string { return `"` + s + `"` }

// TestGate8ScratchEnvHomeInjectionRedProof injects a violating PRODUCTION file
// into a fixture tree and runs the gate's real traversal (goFilesUnder →
// parseRepoFile → predicate) over it: the snippet probe above shows the
// PREDICATE reports, while this shows the SCAN would catch a violation
// committed anywhere under internal/ or cmd/.
//
// The fixture is a separate root on purpose — injecting into the live worktree
// would make the suite mutate the repository it is testing.
func TestGate8ScratchEnvHomeInjectionRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGate8ScratchEnvHomeRedProof")
	root := t.TempDir()

	cleanDir := filepath.Join(root, "internal", "clean")
	badDir := filepath.Join(root, "internal", "bad")
	for _, dir := range []string{cleanDir, badDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cleanSrc := "package clean\n\nimport \"github.com/nickhuang99/gopi/internal/scratch\"\n\nconst n = scratch.MemoryBaseEnvName\n"
	if err := os.WriteFile(filepath.Join(cleanDir, "clean.go"), []byte(cleanSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	badSrc := "package bad\n\n// the injected violation: a mirrored copy where the alias belongs\nconst MemoryBaseEnvName = " + quote(scratch.MemoryBaseEnvName) + "\n"
	if err := os.WriteFile(filepath.Join(badDir, "bad.go"), []byte(badSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	// A TEST file must be ignored by this gate (wantTest=false): a test that
	// runs the setup script must be able to write the wire spelling into a
	// child's environment, and pinning it there is the drift gate's job.
	testSrc := "package bad\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = " + quote(scratch.MemoryBaseEnvName) + " }\n"
	if err := os.WriteFile(filepath.Join(badDir, "bad_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	var found []string
	for _, rel := range goFilesUnder(t, root, []string{"internal"}, false) {
		p := parseRepoFile(t, root, rel)
		for _, h := range g.find(g, p) {
			found = append(found, h.rel+": "+h.desc)
		}
	}
	if len(found) != 1 {
		t.Fatalf("injection proof: the scan reported %d hits (%v), want exactly the injected production file", len(found), found)
	}
	if !strings.HasPrefix(found[0], "internal/bad/bad.go") {
		t.Fatalf("injection proof: reported %q, want internal/bad/bad.go", found[0])
	}
}

// TestGate8ScratchEnvHomeRealTreeIsClean is the ratchet's other half: after the
// mirrored literal became a true alias, the production tree carries NO second
// definition site. A baseline-less gate is only meaningful if the tree is
// actually clean today.
//
// It also asserts the scan SAW a non-empty production tree, so a broken glob
// cannot turn the gate into a vacuous pass over zero files.
func TestGate8ScratchEnvHomeRealTreeIsClean(t *testing.T) {
	g := gateByProbe(t, "TestGate8ScratchEnvHomeRedProof")
	hits, files := scan(t, g)
	if len(files) < scratchEnvScanFloor {
		t.Fatalf("the gate scanned only %d production file(s) — a scan this thin cannot witness the rule (floor is %d)", len(files), scratchEnvScanFloor)
	}
	c := classifyAll(g, hits, files)
	t.Logf("%s: files=%d hits=%d baselined=%d escaped=%d NEW=%d", g.id, len(files), c.total, c.baselined, c.escaped, c.unannot)
	if c.unannot != 0 {
		t.Fatalf("the production tree carries %d re-inlined scratch env-name literal(s): %+v", c.unannot, hits)
	}
	// The declared homes must be REAL files the gate actually scanned: if a
	// holder is renamed, the permitted value would silently move with it.
	for _, holder := range []string{scratchMemoryBaseHolderFile, scratchResolveBaseHolderFile} {
		if _, ok := files[holder]; !ok {
			t.Fatalf("the declared holder %s was not scanned — the one-home claim is unverified", holder)
		}
	}
}

// scratchEnvScanFloor is the minimum production file count the real-tree check
// accepts: a scan that suddenly returns a handful of files is a broken glob,
// not a clean tree (the repo carries ~300 production .go files).
const scratchEnvScanFloor = 100
