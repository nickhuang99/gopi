package standardgate

import (
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// red proof + real-tree coverage
// ---------------------------------------------------------------------------

// TestGate9GitEnvRedProof proves the predicate reports the incident's shape and
// stays silent on both repaired shapes. Without it the gate could rot to a
// no-op and still read green.
func TestGate9GitEnvRedProof(t *testing.T) {
	g := gitEnvGate()

	// RED: the incident's own shape — a fixture that builds a repository in a
	// temp root and hands the child the inherited environment.
	violating := `package p

import (
	"os"
	"os/exec"
)

func fixture(root string) {
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=fixture")
	_ = cmd.Run()
}
`
	hits := probeHits(t, g, violating)
	if len(hits) != 1 {
		t.Fatalf("red proof: got %d hits, want exactly 1 (the inherited environment a git "+
			"child would receive): %+v", len(hits), hits)
	}
	if hits[0].line != 11 {
		t.Fatalf("red proof: the hit must be reported ON the os.Environ() line (11), got %d — "+
			"a reader has to land on the expression to fix", hits[0].line)
	}

	// GREEN: the repaired shape — the helper's strip.
	repaired := `package p

import (
	"os/exec"

	"github.com/nickhuang99/gopi/internal/testutil"
)

func fixture(root string) {
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	cmd.Env = testutil.GitEnv("GIT_AUTHOR_NAME=fixture")
	_ = cmd.Run()
}
`
	if got := probeHits(t, g, repaired); len(got) != 0 {
		t.Fatalf("green proof: the helper's environment must not be reported, got %+v", got)
	}

	// GREEN: the field's documented escape — a site that must keep the
	// inherited environment says why, on the line above.
	escapedSrc := "package p\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n)\n\nfunc fixture(root string) {\n" +
		"\tcmd := exec.Command(\"git\", \"init\", \"-q\")\n" +
		"\tcmd.Dir = root\n" +
		"\t// " + strings.TrimPrefix(gitEnvEscape, "// ") + " the child must see the caller's whole environment\n" +
		"\tcmd.Env = append(os.Environ())\n" +
		"\t_ = cmd.Run()\n}\n"
	p := &parsed{rel: "probe_test.go", src: []byte(escapedSrc)}
	fset, f := parseSource(t, "probe_test.go", escapedSrc)
	p.fset, p.file = fset, f
	raw := g.find(g, p)
	if len(raw) != 1 {
		t.Fatalf("escape fixture must produce one raw hit to classify, got %+v", raw)
	}
	if class := classify(g, raw[0], p); class != classEscaped {
		t.Fatalf("escape proof: the %q marker above the hit must classify as escaped, got %v",
			gitEnvEscape, class)
	}

	// GREEN: a NON-git child that inherits the environment is someone else's
	// business — the gate is about repository routing, not about env hygiene.
	other := `package p

import (
	"os"
	"os/exec"
)

func run() {
	cmd := exec.Command("go", "build", "./...")
	cmd.Env = append(os.Environ())
	_ = cmd.Run()
}
`
	if got := probeHits(t, g, other); len(got) != 0 {
		t.Fatalf("green proof: a non-git child must not be reported, got %+v", got)
	}
}

// TestGate9GitEnvScansTheRealTree is the gate's THIRD top-level arm: the
// real-tree ENFORCEMENT of gate 9, runnable by its OWN name.
//
// WHY A THIRD ARM (measured, task 01a19730). This gate shipped two top-level
// arms and neither one turns red on a fresh breach when a reader runs it by
// name:
//
//	TestGate9GitEnvRedProof                          PREDICATE only: a planted
//	                                                 snippet, 0.00s, reads no file
//	TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly  FLOOR + stale entries
//	                                                 only: a NEW unbaselined hit
//	                                                 prints `ok` here
//
// so `-run 'TestGate9GitEnvRedProof'` exited 0 while three unannotated gate-9
// hits sat in the tree (the 2026-10-28 incident recorded in probe_paths_test.go),
// and the only arm that could fail — the SUBTEST TestStandardGates/TestGate9GitEnvRedProof
// — is unreachable from a single-segment `-run` on the probe name. The fix there
// was to publish the SUBTEST path, and probe_paths_test.go now machine-checks
// that publication. That leaves a second door open: a reader who greps the probe
// name out of a page and pastes it gets a green that no tree can turn red.
//
// The sibling gates that DO have a top-level real-tree arm
// (TestBranchForceGateScansTheRealTree, TestSandboxSettingsDocGateScansTheRealTree,
// TestRemovalAuditGateSeesTheRealTree) can be run by their own name and fail on
// a real violation. This arm gives gate 9 the same property WITHOUT touching the
// red proof: probe_paths_test.go arm (b) asserts a registered probe never reads
// the tree, and git_env_gate.go's predicate deliberately keeps "the predicate
// rotted" and "the tree grew a violation" distinguishable. A red proof that
// scanned the tree would destroy both, so the arm is separate — exactly the
// shape the siblings use.
//
// It asserts what the subtest asserts, through the same ONE accounting
// (scan → classifyAll → enforce): a hit that is neither baselined nor escaped
// FAILS here, naming the site.
func TestGate9GitEnvScansTheRealTree(t *testing.T) {
	g := gitEnvGate()
	root := repoRoot(t)
	// The floor is this gate's OWN non-vacuity constant (gitEnvScannedFilesFloor),
	// reused rather than re-invented: a second number would let the two arms
	// disagree about how much tree is enough (Rule 1/13).
	files := goFilesUnder(t, root, g.scanRoots, g.wantTest)
	if len(files) < gitEnvScannedFilesFloor {
		t.Fatalf("the gate read %d test file(s), below the floor of %d — a scan that walked "+
			"nothing reports a clean green, so this arm refuses to pass before it judges",
			len(files), gitEnvScannedFilesFloor)
	}
	hits, byRel := scan(t, g)
	c := classifyAll(g, hits, byRel)
	t.Logf("real tree: %d test file(s), %d hit(s), %d baselined, %d escaped, %d NEW",
		len(files), c.total, c.baselined, c.escaped, c.unannot)
	// enforce is the ONE accounting (gate.go): a NEW hit fails, naming the file,
	// the line, the function and the command that reproduces it; a stale baseline
	// entry fails too, so this arm keeps the ratchet downward exactly like the
	// subtest does.
	enforce(t, g, hits, byRel)
}

// TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly is the COVERAGE arm — it
// is NOT this gate's red proof, and it must never be run as one. It asserts
// exactly two things, and nothing else:
//
//	(a) the scan READ the test tree — a floor on the FILE COUNT, so a scan that
//	    silently walked nothing cannot read as a green gate; and
//	(b) no baseline ENTRY is stale — every baselined key still matches a live
//	    hit, so a repaired site whose entry was not deleted fails the ratchet
//	    (and an empty baseline is refused, since it would read green on a tree
//	    the scan never visited).
//
// A NEW unbaselined hit does NOT fail it: this arm reads the class
// distribution of a stale list, never `enforce()`, so a tree carrying a fresh
// Gate9 breach prints `ok` here exactly like a clean one. The FATAL arm — the
// one that DOES fail on a fresh breach, naming the site — is the gate record's
// probe, TestStandardGates/TestGate9GitEnvRedProof, which reports
//
//	NEW violation (checklist 1b/git-env) at <file>:<line> [<func>]
//
// (or TestStandardGates itself, which runs every gate's probe). Measured
// 2026-10-16 (task 01a1417e-3b3b-7bd6-abee-1943754ebefe): this arm was reached
// for by name as a red proof, printed `ok … 11.717s` over a reverted repair, and
// therefore proved nothing. See devdoc/standards-gate.md §8.
func TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly(t *testing.T) {
	g := gitEnvGate()
	root := repoRoot(t)
	files := goFilesUnder(t, root, g.scanRoots, g.wantTest)
	if len(files) < gitEnvScannedFilesFloor {
		t.Fatalf("the gate read %d test files, below the floor of %d — a scan that "+
			"walked nothing is not a green gate", len(files), gitEnvScannedFilesFloor)
	}
	var keys []string
	hits, _ := scan(t, g)
	for _, h := range hits {
		keys = append(keys, h.key()+"  ["+h.desc+"]")
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("hit %s", k)
	}
	if len(g.baseline) == 0 {
		t.Fatalf("the baseline is empty while the scan reports %d hit(s) — measure the "+
			"pre-existing population, do not disable it", len(hits))
	}
	// Every baselined entry must still be a REAL hit, and vice versa: the two
	// halves agreeing is what makes the list a measurement.
	live := map[string]bool{}
	for _, h := range hits {
		live[h.key()] = true
	}
	var stale []string
	for _, b := range g.baseline {
		if !live[b] {
			stale = append(stale, b)
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		t.Errorf("stale baseline entry %s matches no hit — delete it (ratchet down)", s)
	}
	t.Logf("git-env gate: %d test files scanned, %d hit(s), %d baseline entries",
		len(files), len(hits), len(g.baseline))
}
