package heavytest

// boundary_test.go — the boundary's own proof, plus the CONSTRUCTED acceptance
// case: a process carrying EnvName=1 AND the task-child/wrapper marker must
// still SKIP, and must say why (never a silent no-op).
//
// The in-process tests drive the REAL predicate (WorkRefusal) against a
// snapshotted environment (absence included — Rule 17's per-call flip). The
// end-to-end half (TestBoundaryTaskChildE2E and its siblings) generates a small
// module in a temp dir, re-runs a real `go test` in it, and reads the CHILD's
// output — which is where the refusal is announced: a refusal nobody can see is
// the silent no-op this boundary exists to remove.
//
// WHY THE FIXTURE IS GENERATED, NOT CHECKED IN (the rule27-gate precedent): a
// committed fixture module under this package would be a *_test.go file inside
// the scanned tree, so it would enter the heavy-test inventory gate and every
// other whole-tree standards scan as if it were part of this repo's suite. The
// fixture is throwaway input, so it is created where it is used and reaped with
// the test.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The generated fixture module's identity. boundarySentinelModule must live
// UNDER the gopi module path, or Go's internal-visibility rule refuses the
// import of this package from it.
const (
	boundarySentinelModule = "github.com/nick/gopi/internal/heavytest/boundarysentinel"
	boundarySentinelHeavy  = "TestSentinelHeavy"
	boundarySentinelMarker = "HEAVY SENTINEL RAN"
)

// boundarySentinelSource is the fixture's test file: one heavy test (the
// subject) and one ordinary test (proves the run itself is healthy, so a
// non-zero exit can never be blamed on the fixture failing).
const boundarySentinelSource = `package boundarysentinel

import (
	"testing"

	"github.com/nick/gopi/internal/heavytest"
)

// boundaryCriterion is a free-form string ON PURPOSE: the fixture is not this
// repo's suite, so it is outside the inventory gate's reach (Rule 1b binds the
// repo's own call sites, which pass a Criterion* constant).
func TestSentinelHeavy(t *testing.T) {
	heavytest.Require(t, "boundary fixture criterion")
	t.Log("` + boundarySentinelMarker + `")
}

func TestSentinelLight(t *testing.T) { t.Log("light sentinel ran") }
`

// boundaryRepoRoot resolves the gopi worktree this test runs in (the fixture
// module's `replace` target).
func boundaryRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod (%v) — the fixture's replace target is wrong", root, err)
	}
	return root
}

// boundaryFixture writes the throwaway module and returns its directory.
func boundaryFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gomod := "module " + boundarySentinelModule + "\n\ngo 1.24.0\n\nrequire github.com/nick/gopi v0.0.0\n\nreplace github.com/nick/gopi => " +
		filepath.ToSlash(boundaryRepoRoot(t)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatalf("write fixture go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sentinel_test.go"), []byte(boundarySentinelSource), 0o644); err != nil {
		t.Fatalf("write fixture test: %v", err)
	}
	return dir
}

// boundaryClearWorkSignature removes every work signature AND the operator
// declaration for the duration of a test: THIS test process is a gopi agent's
// child, so it carries them for real, and a test that forgot that would measure
// the harness instead of the predicate.
//
// t.Setenv(…, "") is the right instrument here and not merely the gate-approved
// one: the boundary reads a name as PRESENT only when its trimmed value is
// non-empty (WorkSignature), so an empty value makes the process read exactly as
// an ordinary shell does — and testing.T restores the real value on cleanup, so
// nothing leaks into a sibling test (Rule 17).
func boundaryClearWorkSignature(t *testing.T) {
	t.Helper()
	for _, n := range append(append([]string{}, WorkEnvVars...), HumanTerminalEnv, EnvName) {
		t.Setenv(n, "")
	}
}

// boundaryRunSentinel runs the fixture module with env overlaid on a MINIMAL
// environment: the runner's own env is dropped, because its agent signature
// would otherwise decide the case under test.
func boundaryRunSentinel(t *testing.T, extra map[string]string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "test", "-count=1", "-run", boundarySentinelHeavy, "-v", "./")
	cmd.Dir = boundaryFixture(t)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOFLAGS=-mod=mod",
	}
	for k, v := range extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestBoundaryRefusesEveryWorkSignature: each work name, alone, must refuse —
// and the reason must NAME it, so an operator reading a log knows which claim
// fired (a bare "refused" is the silent-failure trap Rule 15 names).
func TestBoundaryRefusesEveryWorkSignature(t *testing.T) {
	for _, name := range WorkEnvVars {
		t.Run(name, func(t *testing.T) {
			boundaryClearWorkSignature(t)
			t.Setenv(name, "present")
			reason, sig, declared := WorkRefusal()
			if reason == "" || declared {
				t.Fatalf("%s present: reason=%q declared=%v — a work signature must refuse", name, reason, declared)
			}
			if !strings.Contains(reason, name) {
				t.Errorf("reason %q does not name %s — the refusal must say WHY this process is work", reason, name)
			}
			if len(sig) != 1 || sig[0] != name {
				t.Errorf("signature = %v, want exactly [%s]", sig, name)
			}
		})
	}
}

// TestBoundaryAdmitsTheOperatorTerminal: the operator's own declaration wins
// over an inherited signature — that is the whole point (his shell may sit
// inside an agent session; the declaration is HIS act, not an inherited one).
func TestBoundaryAdmitsTheOperatorTerminal(t *testing.T) {
	boundaryClearWorkSignature(t)
	t.Setenv(TaskWrapperEnv, "01a0b1b5-wrapper")
	t.Setenv(HumanTerminalEnv, "1")
	reason, sig, declared := WorkRefusal()
	if reason != "" || !declared {
		t.Fatalf("operator declaration with a work signature present: reason=%q declared=%v — "+
			"the declaration must step over the inherited signature", reason, declared)
	}
	if len(sig) == 0 {
		t.Errorf("the signature was not recorded — the operator must be told WHAT was stepped over")
	}
}

// TestBoundaryCleanProcessNeedsNoDeclaration: an ordinary shell (the operator's
// terminal, CI, a plain checkout) carries no signature, so nothing is refused
// and nothing has to be declared.
func TestBoundaryCleanProcessNeedsNoDeclaration(t *testing.T) {
	boundaryClearWorkSignature(t)
	reason, sig, declared := WorkRefusal()
	if reason != "" || declared || len(sig) != 0 {
		t.Fatalf("clean env: reason=%q sig=%v declared=%v — want no refusal and no declaration requirement",
			reason, sig, declared)
	}
}

// TestBoundaryFalseDeclarationDoesNotCount: a canonical "off" must stay off
// (the regression config.EnvBool's doc records — a non-empty check treats
// "off" as enabled).
func TestBoundaryFalseDeclarationDoesNotCount(t *testing.T) {
	for _, off := range []string{"", "off", "false", "0", "no", "  "} {
		t.Run("value="+off, func(t *testing.T) {
			boundaryClearWorkSignature(t)
			t.Setenv(TaskWrapperEnv, "wrapper")
			t.Setenv(HumanTerminalEnv, off)
			if reason, _, declared := WorkRefusal(); reason == "" || declared {
				t.Fatalf("%s=%q must NOT be a declaration: reason=%q declared=%v",
					HumanTerminalEnv, off, reason, declared)
			}
		})
	}
}

// TestBoundaryTaskChildE2E is the CONSTRUCTED ACCEPTANCE CASE: a process
// carrying EnvName=1 AND the task-child marker (the wrapper identity this
// harness stamps at spawn) must still SKIP its heavy test — and must print the
// refusal, never silently ignore the switch.
func TestBoundaryTaskChildE2E(t *testing.T) {
	out, err := boundaryRunSentinel(t, map[string]string{
		EnvName:        "1", // the forbidden inheritance
		TaskWrapperEnv: "01a0b1b5-d902-7c3e-b206-c511dcdb7ebf",
		TaskRunIDEnv:   "01a0b1b5-d902-7c3e-b206-c511dcdb7ebf",
	})
	if err != nil {
		t.Fatalf("the fixture run failed (%v) — a refusal must SKIP, not fail the run:\n%s", err, out)
	}
	if strings.Contains(out, boundarySentinelMarker) {
		t.Fatalf("the heavy test RAN while a task-child signature was present — the boundary leaked:\n%s", out)
	}
	if !strings.Contains(out, "--- SKIP: "+boundarySentinelHeavy) {
		t.Fatalf("the heavy test did not SKIP — the run must stay smoke-only:\n%s", out)
	}
	if !strings.Contains(out, RefusalPrefix) {
		t.Fatalf("no %q line in the child's output — a refusal nobody can see is a silent no-op:\n%s",
			RefusalPrefix, out)
	}
	if !strings.Contains(out, TaskWrapperEnv) {
		t.Fatalf("the refusal did not name the signature that caused it (%s):\n%s", TaskWrapperEnv, out)
	}
}

// TestBoundaryOperatorRouteE2E is the other half of the acceptance: the
// documented operator route (EnvName + the terminal declaration, exactly what
// `make test-heavy` sets) must actually RUN the heavy test, first try.
func TestBoundaryOperatorRouteE2E(t *testing.T) {
	out, err := boundaryRunSentinel(t, map[string]string{
		EnvName:          "1",
		HumanTerminalEnv: "1",
	})
	if err != nil {
		t.Fatalf("the operator route failed (%v) — it must work first try:\n%s", err, out)
	}
	if !strings.Contains(out, boundarySentinelMarker) {
		t.Fatalf("the operator route did NOT run the heavy test:\n%s", out)
	}
	if strings.Contains(out, "--- SKIP: "+boundarySentinelHeavy) {
		t.Fatalf("the operator route skipped the heavy test — the opt-in was neutered:\n%s", out)
	}
}

// TestBoundaryDefaultStaysSmokeOnly: with nothing set, the heavy test skips with
// the CLASSIFICATION's own marker (not the boundary's), so the default path is
// untouched by this change.
func TestBoundaryDefaultStaysSmokeOnly(t *testing.T) {
	out, err := boundaryRunSentinel(t, nil)
	if err != nil {
		t.Fatalf("the default run failed (%v):\n%s", err, out)
	}
	if !strings.Contains(out, SkipMarkerPrefix) {
		t.Fatalf("the default run did not carry the classification's skip marker (%q):\n%s",
			SkipMarkerPrefix, out)
	}
	if strings.Contains(out, RefusalPrefix) {
		t.Fatalf("the default run printed a REFUSAL — with nothing set there is nothing to refuse:\n%s", out)
	}
}

// TestBoundaryRefusalWritesTheLedger: the refusal is COUNTABLE — the smoke run's
// ledger distinguishes a classified skip from a refused heavy set, so an
// operator can tell "the default" from "the boundary fired".
func TestBoundaryRefusalWritesTheLedger(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), ".heavy-skipped.tsv")
	out, err := boundaryRunSentinel(t, map[string]string{
		EnvName:        "1",
		TaskWrapperEnv: "wrapper-row",
		SkipLedgerEnv:  ledger,
	})
	if err != nil {
		t.Fatalf("fixture run failed (%v):\n%s", err, out)
	}
	raw, rerr := os.ReadFile(ledger)
	if rerr != nil {
		t.Fatalf("the refused run left no ledger at %s (%v) — an uncountable refusal is a silent one:\n%s",
			ledger, rerr, out)
	}
	if !strings.Contains(string(raw), refusalTag) {
		t.Errorf("the ledger row does not carry %q, so a refusal is indistinguishable from a "+
			"classified skip:\n%s", refusalTag, raw)
	}
	if !strings.Contains(string(raw), boundarySentinelHeavy) {
		t.Errorf("the ledger row does not name the skipped test:\n%s", raw)
	}
}
