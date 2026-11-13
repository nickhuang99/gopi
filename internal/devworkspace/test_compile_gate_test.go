package devworkspace

// The clone channel's TEST-COMPILE leg, proved end to end through the REAL
// delivery surface (`AttemptDelivery`), on a repository shaped like this one.
//
// ── THE SHAPE THIS PINS (measured 2026-11-03, row 01a19c0a) ────────────────
//
// Commit 928740fe landed a `type result` in a `cmd/agent` PRODUCTION file that
// collided with a `type result` already declared in a `cmd/agent` _test.go. The
// package's production code compiled perfectly; its TEST package did not
// compile at all. `go build ./...` exited 0 — it never reads a `_test.go` — so
// the delivery loop published the commit, the test package was uncompilable for
// 44 minutes, and every existing guard was green.
//
// So the fixture below is deliberately NOT a build failure: the production half
// is valid Go on both sides of the collision, and the ONLY thing wrong is that
// one identifier is declared twice in one package. A leg that runs the declared
// build cannot see it. The test asserts the delivery is REFUSED, and — the half
// that matters — that ORIGIN DID NOT MOVE, because a gate that refuses after the
// push has refused nothing.
//
// ── WHY THE FIXTURE'S BUILD COMMAND IS A NO-OP ────────────────────────────
//
// The incident's premise is that the DECLARED BUILD SUCCEEDS. If the fixture's
// build were the go toolchain, this test would pass for the wrong reason (a
// build failure produces a BlockerKindBuild, not this one). The build is a stamp
// instead, exactly as devworkspace_test.go's fixtures do for the pull and race
// arms, so the ONLY step that can refuse is the one under test.
//
// ── THE GREEN CONTROL IS NOT OPTIONAL ─────────────────────────────────────
//
// A leg that refuses EVERY delivery would satisfy the red arm. So the same
// fixture, with the collision removed, must deliver and push: that is the arm
// that proves this leg does not false-refuse a healthy branch — the failure mode
// that gets a gate switched off.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testcompile"
)

const (
	// tcFixtureModule is the fixture's module path. It is a literal rather than
	// a named constant of the package under test because it is the TEST's own
	// data, not a value two packages must agree on (Rule 1's scope).
	tcFixtureModule = "example.com/tcgate"

	// tcFixtureGoDirective is the fixture's `go` line: the oldest supported
	// dialect, so the fixture cannot be refused for a language-version reason.
	tcFixtureGoDirective = "go 1.21"

	// tcFixturePackageDir is the directory the collision lives in: a package
	// with BOTH a production file and a test file, which is the shape 928740fe
	// had (`cmd/agent`).
	tcFixturePackageDir = "app"

	// tcFixtureTypeName is the identifier declared on both sides.
	tcFixtureTypeName = "result"

	// tcFixtureProdFile / tcFixtureTestFile are the two halves. The production
	// one is committed to ORIGIN, so it is present on the tip the channel clones;
	// the test file is the ROW'S OWN WORK, which is what makes the tree
	// unbuildable for tests only after the row does something.
	tcFixtureProdFile = tcFixturePackageDir + "/app.go"
	tcFixtureTestFile = tcFixturePackageDir + "/app_test.go"

	// tcFixtureGoModFile is the fixture's module declaration.
	tcFixtureGoModFile = "go.mod"
)

// tcFixtureOrigin builds an "origin" that is a GO MODULE with one package whose
// PRODUCTION file already declares tcFixtureTypeName. Nothing is wrong with it
// yet: this is the branch point the row inherits.
func tcFixtureOrigin(t *testing.T) string {
	t.Helper()
	origin := newOrigin(t)
	write := func(rel, content string) {
		path := filepath.Join(origin, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(tcFixtureGoModFile, "module "+tcFixtureModule+"\n\n"+tcFixtureGoDirective+"\n")
	write(tcFixtureProdFile, "package "+tcFixturePackageDir+"\n\n"+
		"// "+tcFixtureTypeName+" is the PRODUCTION declaration the incident's test\n"+
		"// file collided with.\n"+
		"type "+tcFixtureTypeName+" struct{ N int }\n")
	gitIn(t, origin, "add", tcFixtureGoModFile, tcFixtureProdFile)
	gitIn(t, origin, "commit", "-q", "-m", "the module and the production declaration")
	return origin
}

// tcFixtureCfg is the channel pointed at the fixture. The declared build is a
// STAMP, never the toolchain: the incident's premise is that the declared build
// SUCCEEDS (see this file's header).
func tcFixtureCfg(t *testing.T, origin string) Config {
	t.Helper()
	cfg := cloneCfg(t, origin)
	cfg.Build = testBuildCommand
	return cfg
}

// tcCommitRowWork opens the workspace against origin and commits ONE file on the
// row's own branch, so there is something to deliver.
func tcCommitRowWork(t *testing.T, origin string, rel, content string) (Config, string) {
	t.Helper()
	cfg := tcFixtureCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", rel)
	gitIn(t, dir, "commit", "-q", "-m", "the row's own change")
	return cfg, dir
}

// tcCollisionTestFile is the ROW'S WORK in the red arm: a `_test.go` that
// declares the SAME identifier the production file already declares. This is
// 928740fe's exact defect, reduced to the minimum that reproduces it.
func tcCollisionTestFile() string {
	return "package " + tcFixturePackageDir + "\n\n" +
		"// " + tcFixtureTypeName + " collides with the production declaration in\n" +
		"// " + tcFixtureProdFile + " — the 928740fe shape.\n" +
		"type " + tcFixtureTypeName + " struct{ M int }\n"
}

// tcHealthyTestFile is the row's work in the GREEN arm: a test file that COMPILES
// and, because the leg runs nothing, never executes.
func tcHealthyTestFile() string {
	return "package " + tcFixturePackageDir + "\n\n" +
		"import \"testing\"\n\n" +
		"// TestFixtureIsHealthy would FAIL if it ever ran: the leg must not run it.\n" +
		"func TestFixtureIsHealthy(t *testing.T) {\n" +
		"\tt.Fatal(\"the test-compile leg RAN a test; the filter must run none\")\n" +
		"}\n"
}

// tcProducesCollision is the premise check, run on the fixture ITSELF and
// through the ONE spelling the leg uses: the collision must make `go build`
// green and the test-compile command red. Without this the red arm below could
// be passing because of something else about the fixture.
func TestTestCompileGateFixtureHasTheGoBuildGreenTestTreeRedShape(t *testing.T) {
	origin := tcFixtureOrigin(t)
	cfg, dir := tcCommitRowWork(t, origin, tcFixtureTestFile, tcCollisionTestFile())

	// `go build ./...` — the declared build's own command, and the one the
	// incident's guard ran.
	if out, err := run(context.Background(), dir, "go", "build", tcFixtureCommandEllipsis); err != nil {
		t.Fatalf("the fixture must COMPILE for production (that is the incident's whole point): %v\n%s", err, out)
	}
	// The leg's own command must report it.
	out, err := run(context.Background(), dir, shellBinary, "-c", testcompile.Command)
	if err == nil {
		t.Fatalf("%s exited 0 on a package whose test file redeclares %q; the fixture does not have the shape this test needs\n%s",
			testcompile.Command, tcFixtureTypeName, out)
	}
	if !strings.Contains(out, "redeclared") {
		t.Fatalf("the compiler must say `redeclared`; got:\n%s", out)
	}
	_ = cfg
}

// tcFixtureCommandEllipsis is the "./..." operand — a literal of the Go
// toolchain's own vocabulary, used with an explicit argument list rather than
// inside a shell string.
const tcFixtureCommandEllipsis = "./..."

// TestDeliveryRefusesATreeWhoseTestsDoNotCompile is the RED arm: the exact
// 928740fe shape, delivered through the REAL AttemptDelivery.
func TestDeliveryRefusesATreeWhoseTestsDoNotCompile(t *testing.T) {
	origin := tcFixtureOrigin(t)
	cfg, dir := tcCommitRowWork(t, origin, tcFixtureTestFile, tcCollisionTestFile())
	tip := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a tree whose TESTS do not compile is a BLOCKER, not an error: %v", err)
	}
	if blk == nil {
		t.Fatalf("want a test-compile blocker, got none (%+v)", res)
	}
	if blk.Kind != BlockerKindTestCompile {
		t.Fatalf("blocker kind = %q, want %q (words: %s)", blk.Kind, BlockerKindTestCompile, blk.Words)
	}
	report := blk.Report()
	// THE REFUSAL VERBATIM, so a reader of the test log (and of this run's
	// evidence) sees the sentence an operator would see — not a summary of it.
	t.Logf("the delivery refused with: %s", report)
	// THE KIND MUST BE LEGIBLE: the refusal says which step refused, and it says
	// that no TEST ran — otherwise a reader hunts for a failing test that this
	// leg never executed.
	if !strings.Contains(report, testCompileGateStep) {
		t.Fatalf("the refusal must name the step that refused; got %q", report)
	}
	if !strings.Contains(report, "no test was RUN") {
		t.Fatalf("the refusal must say no test was executed; got %q", report)
	}
	// THE COMPILER'S OWN WORDS, not this channel's summary of them.
	if !strings.Contains(blk.Words, "redeclared") {
		t.Fatalf("Blocker.Words must quote the compiler; got %q", blk.Words)
	}
	// AND THE ONE FACT THAT MAKES IT A GATE: origin did not move.
	if got := strings.TrimSpace(gitIn(t, origin, "rev-parse", DefaultBranch)); got == tip {
		t.Fatalf("origin moved to the refused tip %s — the refusal came AFTER the push", tip)
	}
	if out := gitIn(t, origin, "log", "--oneline", DefaultBranch); strings.Contains(out, "the row's own change") {
		t.Fatalf("a tree whose tests do not compile must never be pushed; origin log:\n%s", out)
	}
	if res.Pushed {
		t.Fatalf("Result.Pushed must be false on a refusal (%+v)", res)
	}
}

// TestDeliveryAcceptsATreeWhoseTestsCompile is the GREEN CONTROL, and it is the
// arm that proves the leg does not false-refuse: the same fixture, the same
// steps, one healthy test file instead. It also pins Rule 27 — the fixture's
// test FAILS if it is ever run, so a green delivery proves the leg built the
// binary and executed nothing.
func TestDeliveryAcceptsATreeWhoseTestsCompile(t *testing.T) {
	origin := tcFixtureOrigin(t)
	cfg, dir := tcCommitRowWork(t, origin, tcFixtureTestFile, tcHealthyTestFile())

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("a healthy test tree was refused by %q: %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("a healthy delivery must push (%+v)", res)
	}
	if out := gitIn(t, origin, "log", "--oneline", DefaultBranch); !strings.Contains(out, "the row's own change") {
		t.Fatalf("the healthy commit must have been pushed; origin log:\n%s", out)
	}
}

// TestTestCompileGateAbstainsOnANonGoTree pins Rule 15's fourth shape: a
// repository with no `go.mod` has no KNOWN way to compile tests, so the leg
// reports that it did not measure rather than pretending the tree is clean. A
// fixture repository is exactly this case — and it is the case every
// non-Go repo in the fleet is in.
func TestTestCompileGateAbstainsOnANonGoTree(t *testing.T) {
	origin := newOrigin(t) // README only, no go.mod
	cfg, dir := tcCommitRowWork(t, origin, "work.txt", "deliverable\n")

	var log strings.Builder
	cfg.Log = &log
	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("AttemptDelivery: %v", err)
	}
	if blk != nil {
		t.Fatalf("a non-Go tree must not be refused by this leg (%q): %s", blk.Kind, blk.Report())
	}
	if !res.Pushed {
		t.Fatalf("an unmeasurable tree still delivers (%+v)", res)
	}
	// THE ABSTENTION IS SPOKEN: "not measured" must not look like "measured
	// clean".
	if !strings.Contains(log.String(), testCompileGateNotePrefix) {
		t.Fatalf("the abstention was silent; channel log:\n%s", log.String())
	}
}

// TestTestCompileGateUsesTheOneCommandSpelling pins Rules 13/14 at the seam a
// grep cannot reach: the leg executes `testcompile.Command`, the SAME value the
// merge gates read, and it builds its budget from the leaf package rather than
// from `cfg.BuildTimeout` (a second budget would be a second answer to "how long
// may this take").
func TestTestCompileGateUsesTheOneCommandSpelling(t *testing.T) {
	// The leaf value is the documented command, assembled from its own parts.
	if want := testcompile.GoBinary + " test -run '" + testcompile.RunNothingFilter + "' -count=1 ./..."; testcompile.Command != want {
		t.Fatalf("testcompile.Command = %q, want %q", testcompile.Command, want)
	}
	// The filter matches no test — the property Rule 27 rests on.
	if testcompile.RunNothingFilter != "^$" {
		t.Fatalf("the run filter must match no test; got %q", testcompile.RunNothingFilter)
	}
	// The budget is the leaf package's, derived from its own seconds, never a
	// second literal.
	if testCompileGateTimeout() != testcompile.Timeout {
		t.Fatalf("the leg's budget %v is not the leaf package's %v", testCompileGateTimeout(), testcompile.Timeout)
	}
	// And the leg fails the delivery with the leaf command quoted, so a reader
	// reproduces exactly what ran.
	blk := &Blocker{Kind: BlockerKindTestCompile, Step: testcompile.Command, Words: "the compiler's own words: x"}
	if !strings.Contains(blk.Report(), testcompile.Command) {
		t.Fatalf("the refusal must quote the command it ran; got %q", blk.Report())
	}
	// It must ALSO be distinguishable from a build refusal by construction.
	if (Blocker{Kind: BlockerKindTestCompile}).Kind == BlockerKindBuild {
		t.Fatal("the test-compile kind must not collapse into the build kind")
	}
}

// TestTestCompileGateLegIsOnTheDeliveryPath is the anti-representation guard:
// the leg must be CALLED by AttemptDelivery, not merely defined. Measured
// 2026-11-03: `finishCleanupBranchCheckVerify` had zero production callers
// (`grep -v _test.go | wc -l` = 1, the definition line), which is exactly how a
// guard becomes a decoration. This arm fails if the call is ever removed.
func TestTestCompileGateLegIsOnTheDeliveryPath(t *testing.T) {
	src, err := os.ReadFile("devworkspace.go")
	if err != nil {
		t.Fatalf("read the delivery loop: %v", err)
	}
	body := string(src)
	call := "testCompileGate(ctx, cfg, dir)"
	if !strings.Contains(body, call) {
		t.Fatalf("AttemptDelivery does not call %s — the leg is not on the delivery path", call)
	}
	// ORDER: after the man gate (which reads the binary the build produced) and
	// before the push. Index comparison is enough for a single file.
	man := strings.Index(body, "manCensusGate(ctx, cfg, dir)")
	leg := strings.Index(body, call)
	push := strings.Index(body, "publishGround(ctx, dir, base)")
	if man < 0 || leg < 0 || push < 0 {
		t.Fatalf("could not locate the three steps (man=%d leg=%d push=%d)", man, leg, push)
	}
	if !(man < leg && leg < push) {
		t.Fatalf("the leg must sit AFTER the man gate (%d) and BEFORE the push (%d); it is at %d", man, push, leg)
	}
}

// TestTestCompileGateBlocksOnlyOnACompileError pins the four shapes' boundary at
// the unit level, without a delivery: only an *exec.ExitError from the leg is a
// blocker. A timeout, an absent toolchain and an unreachable workspace each
// abstain — refusing on those would make the gate's own slowness look like a
// branch defect.
func TestTestCompileGateBlocksOnlyOnACompileError(t *testing.T) {
	// An empty dir has no go.mod: abstention.
	if why := testCompileGateAbsence(t.TempDir()); why == "" {
		t.Fatal("a tree without go.mod must abstain with a reason")
	}
	// A cancelled context is NOT a branch defect.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, testCompileGateGoModFile), []byte("module x\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := Config{Log: &strings.Builder{}}
	if blk := testCompileGate(ctx, cfg, dir); blk != nil {
		t.Fatalf("a cancelled run must ABSTAIN, not refuse: %s", blk.Report())
	}
	// And the exit-error path does refuse, proved on the real toolchain: a
	// module whose test file redeclares its own production identifier.
	collide := t.TempDir()
	for rel, content := range map[string]string{
		tcFixtureGoModFile: "module " + tcFixtureModule + "\n\n" + tcFixtureGoDirective + "\n",
		tcFixtureProdFile:  "package " + tcFixturePackageDir + "\n\ntype " + tcFixtureTypeName + " struct{ N int }\n",
		tcFixtureTestFile:  tcCollisionTestFile(),
	} {
		path := filepath.Join(collide, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	blk := testCompileGate(context.Background(), Config{Log: &strings.Builder{}}, collide)
	if blk == nil || blk.Kind != BlockerKindTestCompile {
		t.Fatalf("a real compile error must block; got %+v", blk)
	}
	// The block carries the compiler's own words, not this channel's summary.
	if !strings.Contains(blk.Words, "redeclared") {
		t.Fatalf("the blocker must quote the compiler; got %q", blk.Words)
	}
}
