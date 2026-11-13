package standardgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGate7WorktreeRootLiteralRedProof is the red proof the acceptance
// criteria require: the gate must REPORT a deliberately violating snippet, and
// stay silent on the compliant form (a reference to the one definition).
//
// Without this probe the gate could rot into a vacuous no-op and still read
// green — which is exactly how a "no hardcoded root" rule silently stops
// working.
func TestGate7WorktreeRootLiteralRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGate7WorktreeRootLiteralRedProof")

	violating := `package p

import "path/filepath"

func wt(task string) string { return filepath.Join("/dev/shm/gopi-"+task, "wt") }
`
	hits := probeHits(t, g, violating)
	if len(hits) == 0 {
		t.Fatal("RED PROOF FAILED: a hardcoded /dev/shm worktree root was not reported — the gate is vacuous")
	}
	if !strings.Contains(hits[0].desc, "ShmDir") {
		t.Errorf("the refusal does not name the reference to use: %q", hits[0].desc)
	}
	// The gate guards a RETIRED location on purpose: the refusal must say so,
	// loudly enough that a reader of a hit learns the location is retired AND
	// that recognising legacy artifacts there is still required. A gate whose
	// contract silently changed meaning (nothing creates here any more) has to
	// carry the new meaning in the text it prints.
	if !strings.Contains(hits[0].desc, "RETIRED") || !strings.Contains(hits[0].desc, "RECOGNISE") {
		t.Errorf("the refusal does not state that the location is retired for CREATION while legacy "+
			"recognition/cleanup stays required: %q", hits[0].desc)
	}

	// The other standard shapes: the ambient temp root, and both home-rooted
	// spellings.
	for _, src := range []string{
		"package p\n\nconst tmpFixture = \"/tmp/gopi-fixture\"\n",
		"package p\n\nconst root = \"~/workspace/gopi-x\"\n",
		"package p\n\nconst root = \"/home/nick/workspace/gopi-x\"\n",
		"package p\n\nconst root = \"$HOME/workspace/gopi-x\"\n",
		// THE SHAPE THE GATE USED TO MISS: the BARE directory, with no entry
		// below it. The original pattern was anchored to "/dev/shm/<name>", so
		// a production constant whose value IS the root went unreported.
		"package p\n\nconst shmRoot = \"/dev/shm\"\n",
		"package p\n\nconst tmpRoot = \"/tmp\"\n",
	} {
		if got := probeHits(t, g, src); len(got) == 0 {
			t.Errorf("RED PROOF FAILED: %q was not reported", strings.TrimSpace(src))
		}
	}

	// Compliant: a reference to the single definition, and a path that merely
	// CONTAINS the word (a neighbouring lookalike is not a worktree root).
	compliant := `package p

import (
	"path/filepath"

	"github.com/nick/gopi/internal/worktreeroots"
)

func wt(task string) string {
	root, _ := worktreeroots.DefaultRoot()
	return filepath.Join(root, task)
}

const sibling = "/home/nick/workspace-backup/gopi-x"
const cacheDir = "/dev/shm-cache/x"
`
	if got := probeHits(t, g, compliant); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on the compliant form: %+v", got)
	}

	// The escape marker suppresses a REVIEWED exception (and stays visible in
	// the data report, rather than disappearing into a baseline).
	escaped := `package p

// worktree-root-ok: the X11 protocol mandates this socket directory, so a
// headless capture cannot move it (a socket namespace, never a data root).
const x11UnixSocketDir = "/tmp/.X11-unix"
`
	escapedHits := probeHits(t, g, escaped)
	if len(escapedHits) != 1 {
		t.Fatalf("the escape probe must still be REPORTED by the predicate (the escape is applied by classify): %+v", escapedHits)
	}
	fset, file := parseSource(t, "probe.go", escaped)
	if cls := classify(g, escapedHits[0], &parsed{rel: "probe.go", fset: fset, file: file, src: []byte(escaped)}); cls != classEscaped {
		t.Fatalf("the inline escape did not suppress a reviewed site: class=%v hits=%+v", cls, escapedHits)
	}
}

// TestGate7FixtureTopologyRedProof is the red proof for the SECOND topology the
// gate gained: a fixture (_test.go) file, held to a different rule than
// production. It pins all four arms of that rule, because each is a place the
// extension could rot:
//
//  1. a bare host root inlined in a fixture IS reported (the epic's missed
//     shape — before the extension the gate never opened a _test.go file);
//  2. the same literal as the DECLARED VALUE of a fixture constant is NOT
//     (the named-fixture form: the point is the name);
//  3. the fixture escape suppresses a reviewed host-root literal;
//  4. the fixture answer to the production shapes is deliberate: a
//     fingerprint PATH ("/tmp/x/wt") is not the root itself and is not
//     reported — a fixture's spelling below the root is its own data.
func TestGate7FixtureTopologyRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGate7WorktreeRootLiteralRedProof")

	inlined := `package p

import "testing"

func TestX(t *testing.T) { _ = "/dev/shm" }
`
	hits := fixtureHits(t, g, inlined)
	if len(hits) != 1 {
		t.Fatalf("RED PROOF FAILED: a bare host root inlined in a fixture was not reported (%d hits) — the fixture topology is vacuous: %+v", len(hits), hits)
	}
	if !strings.Contains(hits[0].desc, "fixture constant") {
		t.Errorf("the fixture refusal does not name the remedy: %q", hits[0].desc)
	}

	// ARM 2 — the named-fixture form: declared in the same file, not reported.
	declared := `package p

import "testing"

const shmFixtureRoot = "/dev/shm"

func TestX(t *testing.T) { _ = shmFixtureRoot }
`
	if got := fixtureHits(t, g, declared); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on the named-fixture form (a declared fixture constant): %+v", got)
	}

	// ARM 3 — a fixture that must keep the host literal says WHY, inline. The
	// literal here is a plain assignment (not a declared value), so it reaches
	// the escape decision instead of the named-fixture exemption.
	escaped := `package p

import "testing"

func TestX(t *testing.T) {
	// fixture-path-ok: the probe must land on the host's own temp root —
	// deriving it would make the fixture follow the configuration it tests.
	probeDir := "/tmp"
	_ = probeDir
}
`
	escapedHits := fixtureHits(t, g, escaped)
	if len(escapedHits) != 1 {
		t.Fatalf("the escape probe must still be REPORTED by the predicate: %+v", escapedHits)
	}
	fset, file := parseSource(t, "probe_test.go", escaped)
	if cls := classify(g, escapedHits[0], &parsed{rel: "probe_test.go", fset: fset, file: file, src: []byte(escaped)}); cls != classEscaped {
		t.Fatalf("the fixture escape did not suppress the reviewed site: class=%v hits=%+v", cls, escapedHits)
	}

	// ARM 4 — a fingerprint path below a root is the fixture's own data, the
	// reference form is always allowed, and a relative leaf is not a host root.
	quiet := `package p

import (
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/worktreeroots"
)

func TestX(t *testing.T) {
	_ = filepath.Join("/tmp/x", "wt") // a fingerprint, not the root
	_ = worktreeroots.AmbientTempRoot // the reference form
	_ = "gopi-adhoc"                  // a relative leaf
}
`
	if got := fixtureHits(t, g, quiet); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on a fixture fingerprint/leaf/reference: %+v", got)
	}
}

// TestGate7ScansTheFixtureTopology proves the SCAN reaches _test.go files, not
// only the predicate: the red proof above feeds the predicate a snippet, which
// would stay green even if the gate never opened a test file. This is the
// epic's actual hole (the gate scanned production only), so it is asserted on
// the SCAN configuration and on the traversal — a dropped second pass fails
// here instead of silently un-gating the fixture tree.
func TestGate7ScansTheFixtureTopology(t *testing.T) {
	g := gateByProbe(t, "TestGate7WorktreeRootLiteralRedProof")
	if !g.scanTests {
		t.Fatal("Rule 13 gate must scan the fixture topology (_test.go) — a bare /dev/shm in a test would go unreported")
	}
	root := repoRoot(t)
	roots := g.secondRoots
	if len(roots) == 0 {
		roots = prodAndTestRoots
	}
	var tests, prod int
	for _, rel := range goFilesUnder(t, root, roots, true) {
		if strings.HasSuffix(rel, testFileSuffix) {
			tests++
		}
	}
	for _, rel := range goFilesUnder(t, root, roots, false) {
		if !strings.HasSuffix(rel, testFileSuffix) {
			prod++
		}
	}
	if tests == 0 {
		t.Fatalf("the Rule 13 fixture scan sees no %s file under %v — the fixture topology is "+
			"outside the gate's reach", testFileSuffix, roots)
	}
	if prod == 0 {
		t.Fatalf("the Rule 13 production scan sees no production file under %v — the fixture pass "+
			"replaced the original topology instead of adding to it", roots)
	}
	t.Logf("Rule 13 scans %d fixture + %d production files", tests, prod)
}

// fixtureHits runs a gate's predicate over an in-memory _test.go probe, so the
// gate's own dispatch (test suffix → fixture rule) is what decides, never the
// test's choice of predicate.
func fixtureHits(t *testing.T, g gate, src string) []hit {
	t.Helper()
	fset, file := parseSource(t, "probe_test.go", src)
	return g.find(g, &parsed{rel: "probe_test.go", fset: fset, file: file, src: []byte(src)})
}

// TestGate7WorktreeRootLiteralInjectionRedProof injects a violating PRODUCTION
// file into a fixture tree and runs the gate's real traversal (goFilesUnder →
// parseRepoFile → predicate) over it. This is the injection proof: the snippet
// probe above shows the predicate reports, while this one shows the *scan*
// would catch a violation committed anywhere under internal/ or cmd/.
//
// The fixture is a separate root on purpose — injecting into the live worktree
// would make the suite mutate the repository it is testing.
func TestGate7WorktreeRootLiteralInjectionRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGate7WorktreeRootLiteralRedProof")
	root := t.TempDir()

	// A clean file and a violating one, side by side, in the fixture tree.
	cleanDir := filepath.Join(root, "internal", "clean")
	badDir := filepath.Join(root, "internal", "bad")
	for _, dir := range []string{cleanDir, badDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cleanSrc := "package clean\n\nimport \"github.com/nick/gopi/internal/worktreeroots\"\n\nfunc Root() string { r, _ := worktreeroots.DefaultRoot(); return r }\n"
	if err := os.WriteFile(filepath.Join(cleanDir, "clean.go"), []byte(cleanSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	badSrc := "package bad\n\n// the injected violation: a bare root literal where the constant belongs\nconst worktreeRoot = \"/dev/shm/gopi-injected\"\n"
	if err := os.WriteFile(filepath.Join(badDir, "bad.go"), []byte(badSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	// The fixture file is scanned by the SECOND pass (scanTests), not by this
	// production traversal: this proof walks goFilesUnder(..., wantTest=false),
	// so the test file below must not appear in its result. The fixture rule is
	// proven on its own by TestGate7FixtureTopologyRedProof.
	testSrc := "package bad\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = \"/dev/shm/gopi-test-fixture\" }\n"
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

// TestGate7WorktreeRootLiteralRealTreeIsClean is the ratchet's other half: after
// the conversion, the production tree carries NO hardcoded root literal. A
// baseline-less gate is only meaningful if the tree is actually clean today.
func TestGate7WorktreeRootLiteralRealTreeIsClean(t *testing.T) {
	g := gateByProbe(t, "TestGate7WorktreeRootLiteralRedProof")
	hits, files := scan(t, g)
	c := classifyAll(g, hits, files)
	t.Logf("%s: hits=%d baselined=%d escaped=%d NEW=%d", g.id, c.total, c.baselined, c.escaped, c.unannot)
	// Escaped sites are REVIEWED exceptions and pass by contract (they stay
	// visible in the data report); only an unclassified hit is a violation.
	if c.unannot != 0 {
		t.Fatalf("the production tree carries %d hardcoded worktree-root literal(s): %+v", c.unannot, hits)
	}
	if !strings.HasSuffix(worktreeRootDefinitionDir, "/") {
		t.Fatalf("worktreeRootDefinitionDir %q must be a directory prefix", worktreeRootDefinitionDir)
	}
}
