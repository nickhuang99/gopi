package testutil

// testbudget_run_condition_gate_test.go — the RUN CONDITION block's acceptance,
// driven through the REAL script (scripts/test-budget.sh) against a stub `go`.
//
// WHY A STUB AND NOT A REAL MID-RUN EDIT. The condition the block reports needs a
// tree that moves while a ~25-minute suite runs; an arm nobody can exercise is an
// arm nobody checks. The stub reproduces the two MEASURABLE inputs the block reads
// — the tested binary's own build identity (`go version -m` on the artifact the
// run builds) and a stream carrying the guard's refusal signature — and leaves
// every other input real: the real script, the real git repository, the real
// revision range. So both directions are checked on the same footing:
//
//	testbudgetRunConditionArmDrift  — an artifact stamped with an ANCESTOR revision
//	                                  whose production sources have since moved:
//	                                  the block must state the drift and name the
//	                                  refusing test;
//	testbudgetRunConditionArmSettled — an artifact stamped with HEAD: the block must
//	                                  state that the comparison came out settled and
//	                                  print no differ row.
//
// The settled arm is the control the epic's acceptance asks for, at the level where
// it can be asserted cheaply and deterministically. Rule 27 is untouched: nothing
// here generates load, and the fixture builds are a hello-world module.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// testbudgetRunConditionAncestorSkips is how far back the drift arm's stamped
	// revision sits. It must be far enough that production sources have changed
	// (Rule 15: the arm states what it measured, not "some revision"), and near
	// enough that the revision is a real ancestor of HEAD rather than an ancient
	// tree.
	testbudgetRunConditionAncestorSkips = 50
	// testbudgetRunConditionFixtureMod, ...Main, ...GoMod are the throwaway module
	// the stub builds: the block reads the OPERAND out of the artifact's build
	// settings, and the linker records `-ldflags` whatever the operand names.
	testbudgetRunConditionFixtureMod   = "module runconditionfixture\n\ngo 1.21\n"
	testbudgetRunConditionFixtureMain  = "package main\n\nfunc main() {}\n"
	testbudgetRunConditionFixtureFile  = "main.go"
	testbudgetRunConditionFixtureGoMod = "go.mod"
	// testbudgetRunConditionRefusingTest is the test the synthetic stream reports as
	// refusing — the name the block must print.
	testbudgetRunConditionRefusingTest = "TestRunConditionRefusesAStaleBinary"
	// testbudgetRunConditionHeading and ...DifferMarker / ...SettledMarker are the
	// block's own text: the heading a reader greps for, the row state of a source
	// that moved, and the sentence the settled arm must carry.
	testbudgetRunConditionHeading      = "--- RUN CONDITION"
	testbudgetRunConditionDifferMarker = "production source(s) in this run's scope differ from the revision"
	testbudgetRunConditionSettledMark  = "byte-for-byte what the tested binary was built from"
	// testbudgetRunConditionEnvRev is how the test hands the stub the revision to
	// stamp (the stub is a shell script; a file is the channel that needs no
	// quoting rules).
	testbudgetRunConditionEnvRev = "RUN_CONDITION_REV_FILE"
	// testbudgetRunConditionGoBin is the toolchain binary the stub stands in front
	// of (one spelling, Rule 1b).
	testbudgetRunConditionGoBin = "go"
)

// testbudgetRunConditionStream is the synthetic stream: ONE test refusing with the
// guard's CONTENT-arm refusal — the arm a mid-run content change fires, and the arm
// master's runner could not see.
func testbudgetRunConditionStream(module string) string {
	pkg := module + "/cmd/agent"
	bin := "/tmp/x-tested-bin/gopi"
	refusal := "    task_session_cwd_e2e_test.go:127: " + bin +
		" does NOT contain the tree's production sources: 1 file(s) differ from the revision it was built from (0123456789ab)" +
		" — this test drives the BUILT binary, so it would verify code that is not in that binary."
	criterion := "        deciding criterion: content (git compared the tracked production .go content NOW against the content at 0123456789ab — a timestamp was never consulted)."
	row := func(action, test, output string) string {
		if output == "" {
			return `{"Action":"` + action + `","Package":"` + pkg + `","Test":"` + test + `"}`
		}
		return `{"Action":"` + action + `","Package":"` + pkg + `","Test":"` + test + `","Output":"` + output + `\n"}`
	}
	return strings.Join([]string{
		`{"Action":"start","Package":"` + pkg + `"}`,
		row("run", testbudgetRunConditionRefusingTest, ""),
		row("output", testbudgetRunConditionRefusingTest, refusal),
		row("output", testbudgetRunConditionRefusingTest, criterion),
		row("output", testbudgetRunConditionRefusingTest, "--- FAIL: "+testbudgetRunConditionRefusingTest+" (0.01s)"),
		`{"Action":"fail","Package":"` + pkg + `","Test":"` + testbudgetRunConditionRefusingTest + `","Elapsed":0.01}`,
		`{"Action":"fail","Package":"` + pkg + `","Elapsed":0.05}`,
	}, "\n") + "\n"
}

// testbudgetRunConditionRun runs the REAL script with a stub `go` in front of the
// PATH. `stampRef` decides what the artifact the run builds claims to be built
// from: the empty string means "whatever the script itself asked for" (HEAD, i.e.
// a settled tree); anything else is the revision the stub stamps instead.
func testbudgetRunConditionRun(t *testing.T, stampRef string) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	realGo, err := exec.LookPath(testbudgetRunConditionGoBin)
	if err != nil {
		t.Skipf("%s not available: %v", testbudgetRunConditionGoBin, err)
	}
	module := repoModulePath(t, root)
	scratch := t.TempDir()
	stubDir := t.TempDir()
	revFile := filepath.Join(t.TempDir(), "rev")
	if err := os.WriteFile(revFile, []byte(stampRef), 0o644); err != nil {
		t.Fatalf("write the stamp file: %v", err)
	}
	fixtureDir := t.TempDir()
	for name, body := range map[string]string{
		testbudgetRunConditionFixtureGoMod: testbudgetRunConditionFixtureMod,
		testbudgetRunConditionFixtureFile:  testbudgetRunConditionFixtureMain,
	} {
		if err := os.WriteFile(filepath.Join(fixtureDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	stream := strings.ReplaceAll(testbudgetRunConditionStream(module), budgetFailuresFixtureModule, module)
	if err := os.WriteFile(filepath.Join(stubDir, "fixture-stream.json"), []byte(stream), 0o644); err != nil {
		t.Fatalf("write the fixture stream: %v", err)
	}
	// The stub stands in for `go` on the two verbs the run's verdict rests on and
	// FORWARDS everything else (go list, go env) to the real toolchain: the block's
	// scope resolution is then a real answer, not a stubbed one.
	stub := "#!/bin/bash\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = build ]; then\n" +
		"    out=\"\"; want=\"\"\n" +
		"    while [ $# -gt 0 ]; do case \"$1\" in -o) out=\"$2\";; -ldflags) want=\"$2\";; esac; shift; done\n" +
		"    ref=\"$(cat \"$" + testbudgetRunConditionEnvRev + "\" 2>/dev/null)\"\n" +
		"    [ -n \"$ref\" ] || ref=\"$(printf '%s' \"$want\" | sed 's/.*=//')\"\n" +
		"    ( cd " + fixtureDir + " && " + realGo + " build -ldflags \"-X " + module + "/internal/version.Revision=$ref\" -o \"$out\" . )\n" +
		"    exit $?\n" +
		"  fi\n" +
		"  if [ \"$a\" = test ]; then\n" +
		"    cat " + stubDir + "/fixture-stream.json\n" +
		"    exit 1\n" +
		"  fi\n" +
		"done\n" +
		"exec " + realGo + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(stubDir, testbudgetRunConditionGoBin), []byte(stub), 0o755); err != nil {
		t.Fatalf("write the stub go: %v", err)
	}

	script := filepath.Join(root, filepath.FromSlash(budgetScriptPath))
	cmd := exec.Command(budgetShellBin, script, "./cmd/agent/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratch,
		testbudgetRunConditionEnvRev+"="+revFile,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, _ := cmd.CombinedOutput() //stylegate:ignore the child's exit code is asserted elsewhere; the report text is the signal under test
	return string(out)
}

// testbudgetRunConditionAncestor is the revision the drift arm stamps: a real
// ancestor of HEAD whose tracked production sources have since changed.
func testbudgetRunConditionAncestor(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command(gitExecutable, "-C", root, "rev-list", "--max-count=1",
		"--skip="+itoa(testbudgetRunConditionAncestorSkips), "HEAD").Output()
	if err != nil {
		t.Skipf("cannot walk back %d commits: %v", testbudgetRunConditionAncestorSkips, err)
	}
	return strings.TrimSpace(string(out))
}

// testbudgetRunConditionHead is the revision the control arm stamps: HEAD itself.
func testbudgetRunConditionHead(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command(gitExecutable, "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestBudgetRunConditionNamesTheDriftAndTheRefusingTest is the epic's acceptance
// arm (1)+(2) at the level a test can hold: the block appears, states the drift,
// names the refusing test and the artifact — and the whole thing is printed by the
// REAL script.
func TestBudgetRunConditionNamesTheDriftAndTheRefusingTest(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	report := testbudgetRunConditionRun(t, testbudgetRunConditionAncestor(t, root))
	if !strings.Contains(report, testbudgetRunConditionHeading) {
		t.Fatalf("the run's summary carries no %s block, so this run's refusals stay unattributed:\n%s",
			testbudgetRunConditionHeading, report)
	}
	if !strings.Contains(report, testbudgetRunConditionDifferMarker) {
		t.Errorf("the block does not state the drift it measured (no %q):\n%s", testbudgetRunConditionDifferMarker, report)
	}
	if !strings.Contains(report, testbudgetRunConditionRefusingTest) {
		t.Errorf("the block does not name the refusing test %s:\n%s", testbudgetRunConditionRefusingTest, report)
	}
}

// TestBudgetRunConditionStaysQuietOnASettledTree is the control: the same run with
// an artifact stamped at HEAD must state that the comparison came out settled, and
// must print NO differ row — a block that reported drift here would be crying wolf
// on every healthy run.
func TestBudgetRunConditionStaysQuietOnASettledTree(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	report := testbudgetRunConditionRun(t, testbudgetRunConditionHead(t, root))
	if !strings.Contains(report, testbudgetRunConditionHeading) {
		t.Fatalf("the settled run printed no %s block at all — the reading must be stated, not omitted (a silent "+
			"empty answer is exactly what lets the same failures read as a code verdict):\n%s",
			testbudgetRunConditionHeading, report)
	}
	if strings.Contains(report, testbudgetRunConditionDifferMarker) {
		t.Errorf("the settled run reported drift — the comparison is against HEAD and nothing moved:\n%s", report)
	}
	if !strings.Contains(report, testbudgetRunConditionSettledMark) {
		t.Errorf("the settled run does not say the comparison CAME OUT settled (%q):\n%s",
			testbudgetRunConditionSettledMark, report)
	}
}
