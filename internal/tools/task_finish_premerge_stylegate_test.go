package tools

// Tests for the close-out's STYLEGATE ARM (task_finish_premerge_stylegate.go).
//
// WHAT THESE PIN, and why each arm exists:
//
//	_WiringIsUnconditional   the arm is a check on EVERY delivery, not a member of
//	                         the touched-package selection. This is the defect's
//	                         whole shape: the ratchet's ceiling is a WHOLE-TREE
//	                         reading while the selection is branch-scoped, so a
//	                         `testsuite/` or `cmd/agent/` commit that discards one
//	                         more error never selects `internal/stylegate`.
//	_OutrunsTheTouchedSelection
//	                         the SAME fact measured live on this tree: the
//	                         branch-scoped selection for this diff does NOT carry
//	                         `./internal/stylegate/...`, and the arm builds a check
//	                         anyway. A test that only asserted "a check exists"
//	                         would pass on a tree where the selection happened to
//	                         include the package — i.e. on a tree where nothing is
//	                         being fixed.
//	_AbsentPackageAbstains   a tree with no ratchet package contributes NO check and
//	                         says so — an invented pattern would address nothing and
//	                         read as a pass (Rule 15).
//	_RunsTheDeclaredEntryPoint  the command is the repo's declared scoped entry
//	                         point (`smokegate.ScopedForPackages`), addressed at the
//	                         gate's package — not a re-spelled `go test`.
//	_TheGateItNamesIsReal    the package the arm addresses actually carries the
//	                         ceilings table and the whole-tree arm. A wiring test
//	                         that named a package with no ratchet in it would be
//	                         green and useless.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/smoke"
	"github.com/nick/gopi/internal/smokegate"
)

// TestStylegateArm_WiringIsUnconditional is the RED-proof-shaped arm: the check is
// built from the TREE ALONE (no branch, no base, no diff — the signature takes
// only a directory), so no diff can decide whether it runs.
func TestStylegateArm_WiringIsUnconditional(t *testing.T) {
	root := repoRootFromPackageDir(t)
	if !finishCleanupBranchStyleGatePackagePresent(root) {
		t.Fatalf("the repository root %q does not carry %s — this arm cannot be wired here", root, styleGatePackageDir)
	}
	check, ok := finishCleanupBranchStylegateCheck(root)
	if !ok {
		t.Fatalf("finishCleanupBranchStylegateCheck(%q) returned ok=false on a tree that carries %s",
			root, styleGatePackageDir)
	}
	if check.Name != styleGateCheckName {
		t.Errorf("check.Name = %q, want %q (the name an operator reads on a refusal)", check.Name, styleGateCheckName)
	}
	if check.TimeoutSec <= 0 {
		t.Errorf("check.TimeoutSec = %d — an unbounded check is not a check", check.TimeoutSec)
	}
	if check.Cmd == "" {
		t.Fatalf("check.Cmd is empty — the runner skips an empty command silently, which is the vacuum this arm closes")
	}
}

// TestStylegateArm_OutrunsTheTouchedSelection measures the DEFECT and the FIX in
// the same reading: the branch-scoped selection for this branch's own diff does
// not carry the ratchet package, and the unconditional arm builds a check for it
// regardless.
//
// The selection is the real primitive the lane uses (`smoke.TouchedPackages`
// against the integration ref), not a re-implementation of it. When this branch
// does touch the ratchet package the asymmetry cannot be shown and the arm
// abstains with the reason — it never fabricates the observation.
func TestStylegateArm_OutrunsTheTouchedSelection(t *testing.T) {
	root := repoRootFromPackageDir(t)
	base := finishCleanupIntegrationRef(t.Context(), root)
	if base == "" {
		t.Skip("no integration ref resolves in this clone — the touched-package selection cannot be taken")
	}
	sel, err := smoke.TouchedPackages(root, base, "HEAD")
	if err != nil {
		t.Skipf("touched-package selection unavailable in this clone: %v", err)
	}
	wantPattern := smoke.PackagePattern(styleGatePackageDir)
	for _, p := range sel.Packages {
		if p == wantPattern {
			t.Skipf("this branch's diff touches %s, so the selection DOES carry it — the asymmetry "+
				"this arm demonstrates cannot be shown on this branch", styleGatePackageDir)
		}
	}
	check, ok := finishCleanupBranchStylegateCheck(root)
	if !ok {
		t.Fatalf("no stylegate check built for %q", root)
	}
	if !strings.Contains(check.Cmd, wantPattern) {
		t.Fatalf("check.Cmd = %q does not carry %s", check.Cmd, wantPattern)
	}
	t.Logf("branch-scoped selection (base %s): %v — it does NOT carry %s, yet the arm builds %q",
		shortRev(base), sel.Packages, wantPattern, check.Cmd)
}

// TestStylegateArm_AbsentPackageAbstains pins the other direction: a tree without
// the ratchet package yields NO check and a sentence saying so.
func TestStylegateArm_AbsentPackageAbstains(t *testing.T) {
	dir := t.TempDir() // a tree with no internal/stylegate at all
	if _, ok := finishCleanupBranchStylegateCheck(dir); ok {
		t.Fatalf("finishCleanupBranchStylegateCheck(%q) built a check for a tree that carries no %s — "+
			"the run would address a package that does not exist", dir, styleGatePackageDir)
	}
	if styleGateAbstention() == "" {
		t.Fatalf("styleGateAbstention() is empty — an arm that could not run must say so, never read as clean")
	}
	if _, ok := finishCleanupBranchStylegateCheck(""); ok {
		t.Fatalf("finishCleanupBranchStylegateCheck(\"\") built a check from no tree at all")
	}
}

// TestStylegateArm_RunsTheDeclaredEntryPoint pins the command's SHAPE through the
// constants that define it (Rule 14): the repo's scoped target with the gate's
// package as the ONE quoted PKGS argument — never a hand-written `go test`.
func TestStylegateArm_RunsTheDeclaredEntryPoint(t *testing.T) {
	root := repoRootFromPackageDir(t)
	check, ok := finishCleanupBranchStylegateCheck(root)
	if !ok {
		t.Fatalf("no stylegate check built for %q", root)
	}
	if !strings.HasPrefix(check.Cmd, smokegate.ScopedTarget) {
		t.Errorf("check.Cmd = %q, want it to start with the declared scoped target %q",
			check.Cmd, smokegate.ScopedTarget)
	}
	// The scope is the gate's own package, quoted as ONE argument so a shell cannot
	// re-split it into further make targets.
	wantScoped := "'" + "./" + styleGatePackageDir + "/...'"
	if !strings.Contains(check.Cmd, wantScoped) {
		t.Errorf("check.Cmd = %q does not carry the gate's package as one quoted argument (%s)", check.Cmd, wantScoped)
	}
	if strings.Contains(check.Cmd, "./...") {
		t.Errorf("check.Cmd = %q addresses the WHOLE module — the arm exists to be narrow", check.Cmd)
	}
}

// TestStylegateArm_TheGateItNamesIsReal is the anti-vacuity leg: the package this
// arm addresses must still carry the ceilings table and the whole-tree comparison
// arms. Without them the arm would be running an ordinary package's tests and
// reporting the run as a style measurement.
func TestStylegateArm_TheGateItNamesIsReal(t *testing.T) {
	root := repoRootFromPackageDir(t)
	gateFile := filepath.Join(root, filepath.FromSlash(styleGatePackageDir), "gate_test.go")
	data, err := os.ReadFile(gateFile)
	if err != nil {
		t.Fatalf("read the stylegate ratchet %s: %v — the arm addresses a package whose gate is gone", gateFile, err)
	}
	src := string(data)
	for _, needle := range []string{
		"gateCeilings",
		"TestStyleGate",
		"TestEveryCeilingEqualsItsMeasurement",
	} {
		if !strings.Contains(src, needle) {
			t.Errorf("%s no longer carries %q — the stylegate arm would run a package that no longer holds the ratchet",
				filepath.Base(gateFile), needle)
		}
	}
}

// shortRev renders a revision for a log line (a full object id in a t.Logf buries
// the sentence around it).
func shortRev(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}
