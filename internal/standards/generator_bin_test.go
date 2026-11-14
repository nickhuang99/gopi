package standardgate

// generator_bin_test.go — ONE build slot for every testdata generator this
// package EXECUTES.
//
// THE DEFECT THIS CLOSES. Arms used to invoke their generator with
//
//	exec.Command("go", "run", internal/standards/testdata/<gen>.go, …)
//
// and `go run` COMPILES the program before it runs it. Five such sites in this
// package, each paying a fresh compile (39s measured for one arm on this host),
// took the package past Go's default 10m per-package budget: the run died with
// `panic: test timed out after 10m0s` and `FAIL … 600.393s`. The cost is a BUILD
// cost, measured directly — not load, and not the arms' own work (Rule 27:
// nothing here generates load; the measurement is the wall clock of a build).
//
// WHY A REAL BINARY AND NOT `go run`. `go run` COLLAPSES a program's exit status
// to 1, so a `go run` arm can never tell a generator's own exitDrift (1) from
// its exitError (2) — and those two are the generator's actual vocabulary. So
// the arms exec the built binary and read the real status; this file changes
// WHERE the binary comes from, never what an arm asserts.
//
// THE PRECEDENT is citationCensusBinary in citation_census_args_test.go: build
// once, reuse the path. This file generalises it to the generators the remaining
// arms exec, so one cache serves all of them.
//
// WHY THE BINARIES LIVE IN THE SUITE SCRATCH ROOT, NOT t.TempDir(). A slot
// keyed on a test's own temp dir is reaped at that test's end, so "built once"
// would mean "built once per test" — and the arms in different files never share
// it. testutil.SuiteTempRoot() is the one tree the suite's TestMain
// (testmain_scratchroot_test.go → testutil.RunSuiteMain) pins and removes when
// the binary exits, so it outlives every test and still leaves nothing behind.
// It is also OFF the `/` partition by construction (Rule 22).

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// goBinaryName / goBuildVerb are the toolchain invocation that replaces
	// `go run`. They are spelled once here (Rule 1/13): the arms name the
	// VERB through this slot, not as a literal.
	goBinaryName = "go"
	goBuildVerb  = "build"

	// generatorBinDirName is the directory inside the suite scratch root that
	// holds this run's built generators.
	generatorBinDirName = "standards-generator-bin"

	// repoHygieneCensusGeneratorRel is the Rule 39 census generator, read
	// through the constant that lives beside the gate it serves.
	repoHygieneCensusGeneratorRel = repoHygieneCensusRel
)

var (
	// generatorBinaries caches ONE built binary per generator for the whole
	// TEST PROCESS: the key is the generator's repo-relative path, the value is
	// the binary's path. A generator is compiled exactly once however many arms
	// exec it.
	generatorBinariesMu sync.Mutex
	generatorBinaries   = map[string]string{}
)

// generatorBinary builds generatorRel ONCE for this test process and returns the
// path of the real binary. Every arm that used to shell out with `go run` calls
// this instead, so it reads the program's OWN exit status.
func generatorBinary(t *testing.T, generatorRel string) string {
	t.Helper()
	generatorBinariesMu.Lock()
	defer generatorBinariesMu.Unlock()
	if path, ok := generatorBinaries[generatorRel]; ok {
		return path
	}
	dir := filepath.Join(testutil.SuiteTempRoot(), generatorBinDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the generator build slot %s: %v", dir, err)
	}
	root := repoRoot(t)
	out := filepath.Join(dir, filepath.Base(generatorRel))
	cmd := exec.Command(goBinaryName, goBuildVerb, "-o", out, generatorRel)
	cmd.Dir = root
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the generator %s failed: %v\n%s", generatorRel, err, buildOut)
	}
	generatorBinaries[generatorRel] = out
	return out
}
