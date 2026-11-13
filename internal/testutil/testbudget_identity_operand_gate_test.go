package testutil

// testbudget_identity_operand_gate_test.go — the SHELL half of the
// single-definition discipline that internal/buildidentity/literal_gate_test.go
// enforces on the Go half.
//
// THE GAP THIS CLOSES, measured on master 01c1674 (2026-10-16). The constants in
// internal/buildidentity are the ONE definition of the build-identity spelling for
// every GO consumer, and internal/version/module_path_agreement_test.go holds
// buildidentity.RevisionLdflagPath against its own sources of truth — go.mod's
// module line, the package's directory, the declared variable. scripts/test-budget.sh
// stamps the tested binary with a SECOND, independent spelling:
//
//	tested_bin_identity_var="Revision"
//	tested_bin_identity_pkg="./internal/version"
//	tested_bin_pkg="$(cd "$root" && go list -f '{{.ImportPath}}' "$tested_bin_identity_pkg" …)"
//	tested_ldflag_args=(-ldflags "-X $tested_bin_pkg.$tested_bin_identity_var=$tested_bin_rev")
//
// The script's own comment calls `go list` "the module's own source of truth", and
// for the MODULE half that is true — but the package leaf (`internal/version`) and
// the variable name (`Revision`) are typed literals that NO machine holds against
// buildidentity (measured: `git grep -n 'tested_bin_identity_var\|tested_bin_identity_pkg'
// -- '*.go'` outside this file returns nothing). And the linker checks no operand:
// rename the variable or move the package and this recipe degrades to stamping
// NOTHING at exit 0 with empty stderr — the same silent-drop path
// module_path_agreement_test.go documents for the Go constant. Every freshness
// verdict resting on that stamp (cmd/agent/gopibin_identity_guard_test.go) then
// falls back to mtimes, which is the failure the stamp exists to prevent.
//
// WHY THE GATE LIVES IN THIS PACKAGE and not beside the Go gate: this is a gate
// over scripts/test-budget.sh, whose other gates already live here
// (testbudget_gate_test.go, testbudget_start_record_gate_test.go,
// testbudget_run_condition_gate_test.go, …) — one home per subject, and the
// script's path comes from the constant this package already declares rather than
// being spelled once more (Rules 1/13). internal/buildidentity stays a LEAF: this
// package's non-test code already imports it (ldflags.go) and it imports nothing.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/buildidentity"
)

const (
	// budgetIdentityVarAssignment / ...PkgAssignment are the two script lines
	// carrying the operand's halves. Their VALUES are what must agree with
	// buildidentity; the assignments themselves must exist, or the predicate has
	// nothing to hold and says so (Rule 15: not-found is a FAIL, never a silent
	// pass).
	budgetIdentityVarAssignment = `tested_bin_identity_var="`
	budgetIdentityPkgAssignment = `tested_bin_identity_pkg="`
	// budgetIdentityStampNeedle is the composition site: the two halves must
	// actually be joined into the `go build` … `-X <pkg>.<var>=<rev>` argument.
	// Without it a script could keep two correct DECLARATIONS while stamping with
	// something else entirely, and every check above would pass while the artifact
	// carried no identity. (Written INTO the script's line, not into a comment:
	// measured 2026-10-16, `grep -n -F` finds this string on exactly one line of
	// scripts/test-budget.sh, the stamping one.)
	budgetIdentityStampNeedle = `"-X $tested_bin_pkg.$tested_bin_identity_var=`
	// budgetIdentityQualifierSeparator is the character between the operand's
	// package path and its variable name (`importpath.name`) — the linker's own
	// shape, so it lives with the check rather than at each use.
	budgetIdentityQualifierSeparator = "."
	// budgetIdentityGoListFormat is the template asking the toolchain for the ONE
	// fact the script's own `go list` also asks for: the import path a package spec
	// resolves to. Spelled once (Rule 1).
	budgetIdentityGoListFormat = "{{.ImportPath}}"
	// budgetIdentityGoBinaryName is the toolchain binary the resolution runs,
	// named here so a host without it fails BY NAME (Rule 1a). This package's other
	// gates each name their own copy — the local convention for a test-file
	// constant, and a leaf "go" package is not something this change should
	// invent (Rule 35).
	budgetIdentityGoBinaryName = "go"
	// budgetIdentityProbeVarName is the variable rename the red proof plants: a
	// plausible typo of the real one, so the probe cannot pass by naming something
	// obviously absent while the real spelling is also absent.
	budgetIdentityProbeVarName = "Revisions"
)

// budgetIdentityOperandFaults is the gate's PURE predicate: given the script's
// text and the import path its package spec resolves to, return one finding per
// way the stamped operand disagrees with buildidentity.RevisionLdflagPath. Pure
// (no filesystem, no environment) so the red proof drives the SAME decider the
// real gate runs, instead of a copy of its logic.
func budgetIdentityOperandFaults(script, resolvedImportPath string) []string {
	var findings []string
	varSpec, okVar := budgetIdentityAssignmentValue(script, budgetIdentityVarAssignment)
	pkgSpec, okPkg := budgetIdentityAssignmentValue(script, budgetIdentityPkgAssignment)
	if !okVar {
		findings = append(findings, "the script no longer assigns "+budgetIdentityVarAssignment+
			`"…" — the operand's variable half has no spelling to check`)
	}
	if !okPkg {
		findings = append(findings, "the script no longer assigns "+budgetIdentityPkgAssignment+
			`"…" — the operand's package half has no spelling to check`)
	}
	if !strings.Contains(script, budgetIdentityStampNeedle) {
		findings = append(findings, "the script no longer composes "+budgetIdentityStampNeedle+
			`…" — the two halves are declared but nothing stamps with them, so the artifact carries no identity`)
	}
	if resolvedImportPath == "" {
		findings = append(findings, "the package spec "+pkgSpec+" resolved to an EMPTY import path — an `-X` operand "+
			"aimed at a path that names nothing is dropped in silence (exit 0, empty stderr)")
	}
	wantPkg, wantVar := budgetIdentityOperandHalves()
	if okVar && okPkg && resolvedImportPath != "" {
		composed := resolvedImportPath + budgetIdentityQualifierSeparator + varSpec
		if composed != buildidentity.RevisionLdflagPath {
			findings = append(findings, "the composed operand "+composed+
				" is not buildidentity.RevisionLdflagPath ("+buildidentity.RevisionLdflagPath+
				"; variable half buildidentity.RevisionVarName="+wantVar+", package half resolving to "+wantPkg+
				") — the shell recipe and the Go readers would disagree about which variable carries the revision")
		}
	}
	return findings
}

// budgetIdentityAssignmentValue returns the value on the first executable line
// assigning `name="…"`, and whether that line exists. A missing line is reported
// to the caller (never folded into an empty value), because "" would make every
// comparison below trivially satisfiable.
func budgetIdentityAssignmentValue(script, name string) (string, bool) {
	for _, line := range strings.Split(script, "\n") {
		code := strings.TrimSpace(stripComment(line))
		if !strings.HasPrefix(code, name) {
			continue
		}
		rest := strings.TrimPrefix(code, name)
		if cut := strings.IndexByte(rest, '"'); cut >= 0 {
			rest = rest[:cut]
		}
		return rest, true
	}
	return "", false
}

// budgetIdentityOperandHalves splits buildidentity.RevisionLdflagPath into the
// package half and the variable half AT ITS OWN LAST SEPARATOR — the same split
// the linker performs, so the expectation cannot drift from the constant. A
// constant that stops having the `importpath.name` shape returns an empty
// variable half, which the callers below turn into a finding rather than a
// silently satisfied comparison.
func budgetIdentityOperandHalves() (pkg, variable string) {
	at := strings.LastIndex(buildidentity.RevisionLdflagPath, budgetIdentityQualifierSeparator)
	if at <= 0 || at == len(buildidentity.RevisionLdflagPath)-1 {
		return buildidentity.RevisionLdflagPath, ""
	}
	return buildidentity.RevisionLdflagPath[:at], buildidentity.RevisionLdflagPath[at+1:]
}

// budgetIdentityResolveImportPath asks the toolchain what import path the
// script's package spec resolves to — the ONE fact checked against
// buildidentity, taken from `go list` rather than from a re-typed constant. It
// FAILS rather than skips when `go` is absent or the spec resolves to nothing: a
// check that cannot reach its source of truth must be RED, not green by default
// (Rule 15).
func budgetIdentityResolveImportPath(t *testing.T, root, pkgSpec string) string {
	t.Helper()
	cmd := exec.Command(budgetIdentityGoBinaryName, "list", "-f", budgetIdentityGoListFormat, pkgSpec)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve the stamped package spec %q with `%s list`: %v\n%s",
			pkgSpec, budgetIdentityGoBinaryName, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestBudgetRecipeOperandAgreesWithBuildIdentity is THE GATE: the operand the
// budget entry point stamps the tested binary with must be the SAME spelling the
// Go readers use, checked against the toolchain rather than against a copy of the
// constant.
func TestBudgetRecipeOperandAgreesWithBuildIdentity(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v — the stamped operand cannot be checked without the script", budgetScriptPath, err)
	}
	script := string(raw)
	pkgSpec, ok := budgetIdentityAssignmentValue(script, budgetIdentityPkgAssignment)
	if !ok {
		t.Fatalf("%s carries no %s line — the operand's package half has no spelling in the script",
			budgetScriptPath, budgetIdentityPkgAssignment)
	}
	resolved := budgetIdentityResolveImportPath(t, root, pkgSpec)
	if resolved == "" {
		t.Fatalf("%s list %s answered an empty import path — the gate has no fact to check against", budgetIdentityGoBinaryName, pkgSpec)
	}
	for _, f := range budgetIdentityOperandFaults(script, resolved) {
		t.Errorf("%s: %s — reference buildidentity (internal/buildidentity/buildidentity.go, re-exported by "+
			"internal/version) instead of a second spelling; the linker checks no operand, so a stale spelling "+
			"stamps NOTHING at exit 0 with empty stderr", budgetScriptPath, f)
	}
}

// TestBudgetIdentityOperandGateIsFalsifiable is the RED PROOF, executed rather
// than asserted: the SAME predicate the real gate runs is fed a script with each
// half of the operand mutated, and is required to report it. Without this, "the
// gate passes" and "the gate cannot fail" are indistinguishable.
func TestBudgetIdentityOperandGateIsFalsifiable(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	script := string(raw)
	pkgSpec, _ := budgetIdentityAssignmentValue(script, budgetIdentityPkgAssignment)
	resolved := budgetIdentityResolveImportPath(t, root, pkgSpec)

	// The control: the real script against the real resolution must be CLEAN, or
	// every mutation below proves nothing.
	if findings := budgetIdentityOperandFaults(script, resolved); len(findings) != 0 {
		t.Fatalf("the REAL script already fails the operand predicate (%v) — every red proof below would be vacuous", findings)
	}

	// The split the gate's expectation is built from must agree with the two
	// constants that name each half — a change to either half's shape is announced
	// here instead of silently making the comparison unsatisfiable.
	wantPkg, wantVar := budgetIdentityOperandHalves()
	if wantVar != buildidentity.RevisionVarName {
		t.Errorf("budgetIdentityOperandHalves() split %q into variable %q, want buildidentity.RevisionVarName (%q)",
			buildidentity.RevisionLdflagPath, wantVar, buildidentity.RevisionVarName)
	}
	if wantPkg+budgetIdentityQualifierSeparator+wantVar != buildidentity.RevisionLdflagPath {
		t.Errorf("splitting %q and rejoining it gave %q — the halves are not a decomposition of the operand",
			buildidentity.RevisionLdflagPath, wantPkg+budgetIdentityQualifierSeparator+wantVar)
	}
	if !strings.HasSuffix(wantPkg, "/internal/version") {
		t.Fatalf("the operand's package half is %q, which the package-move mutation below assumes ends in "+
			"/internal/version — the red proof's anchor rotted", wantPkg)
	}

	// Mutation 1: the VARIABLE half is renamed — the plausible typo, and the
	// mutation the linker silently ignores.
	renamedVar := budgetIdentityReplaceAssignment(t, script, budgetIdentityVarAssignment,
		budgetIdentityVarAssignment+budgetIdentityProbeVarName)
	if findings := budgetIdentityOperandFaults(renamedVar, resolved); len(findings) == 0 {
		t.Errorf("a script stamping the variable %q passes the predicate — the linker would write nothing and "+
			"the freshness guard would fall back to mtimes without a word", budgetIdentityProbeVarName)
	}

	// Mutation 2: the PACKAGE half points somewhere else. The resolution is the
	// input the toolchain supplies, so the mutation lands on that side — exactly
	// the shape of a package MOVE.
	movedPkg := strings.TrimSuffix(resolved, "/internal/version") + "/internal/versionx"
	if findings := budgetIdentityOperandFaults(script, movedPkg); len(findings) == 0 {
		t.Errorf("a package spec resolving to %q passes the predicate — a package move would stamp nothing in silence", movedPkg)
	}

	// Mutation 3: the composition line is removed, so the two (correct) halves are
	// declared and never used.
	uncomposed := budgetIdentityDropLineContaining(t, script, budgetIdentityStampNeedle)
	if findings := budgetIdentityOperandFaults(uncomposed, resolved); len(findings) == 0 {
		t.Errorf("a script that declares the operand and never stamps with it passes the predicate — the artifact " +
			"carries no identity while every declaration reads correct")
	}

	// Mutation 4: an EMPTY resolution — what a `go list` that matched nothing
	// answers. It must be a finding, never a silent skip.
	if findings := budgetIdentityOperandFaults(script, ""); len(findings) == 0 {
		t.Errorf("an empty import path passes the predicate — a spec that names no package is the silent-drop case " +
			"the gate exists for")
	}
}

// budgetIdentityStampLinePrefix names the shape of the composition line for a
// reader; the mutation below locates it by the NEEDLE (the operand itself), so a
// rename of the shell variable cannot leave the arm removing a line that no
// longer carries the operand.
const budgetIdentityStampLinePrefix = "tested_ldflag_args=("

// budgetIdentityDropLineContaining returns script with the first executable line
// carrying needle deleted, and FAILS when no such line exists: a mutation that
// silently did nothing would make the red proof vacuous — the failure mode this
// whole file exists to avoid.
func budgetIdentityDropLineContaining(t *testing.T, script, needle string) string {
	t.Helper()
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if !strings.Contains(line, needle) {
			continue
		}
		if strings.TrimSpace(stripComment(line)) == "" {
			continue
		}
		return strings.Join(append(append([]string{}, lines[:i]...), lines[i+1:]...), "\n")
	}
	t.Fatalf("no executable line carries %q — the red proof's anchor rotted", needle)
	return script
}

// budgetIdentityReplaceAssignment returns script with the first executable line
// assigning name replaced by `with`. It fails the test when the anchor is absent,
// because a mutation that silently did nothing would make the red proof vacuous —
// the failure mode this whole file exists to avoid.
func budgetIdentityReplaceAssignment(t *testing.T, script, name, with string) string {
	t.Helper()
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), name) {
			lines[i] = strings.Replace(line, name, with, 1)
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("no executable line assigns %q — the red proof's anchor rotted", name)
	return script
}
