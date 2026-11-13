package smoke

// Tests for the must-run gate roster (gatepackages.go) — the epic's central
// assertion, measured three ways:
//
//	_IsNotEmpty            the VACUUM FLOOR. An empty roster unions nothing, so
//	                       every caller would keep reading green while running no
//	                       gate at all. A check that cannot fail for lack of
//	                       content is not a check (Rule 15).
//	_NamesTheGatePackages  the roster is the set testdata/heavy-tests.tsv excludes
//	                       from the heavy exemption: internal/standards and
//	                       internal/stylegate. If either disappears the whole-tree
//	                       readings stop being taken.
//	_UnionsIntoAnySelection the pure function's contract: the caller's list comes
//	                       out WHOLE, the roster is ADDED, the result is sorted,
//	                       deduped, and never the whole module.
//	_AnswersAnEmptySelection the docs-only shape: a diff that touched nothing
//	                       still produces a runnable command, and that command is
//	                       exactly the roster.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGatePackagesIsNotEmpty is the non-vacuity floor, and it is deliberately
// the FIRST test in this file: every other arm below is trivially satisfiable by
// an empty roster (an empty list is "never the whole module", "already sorted",
// "equal to WithGatePackages of itself"), so the floor is what makes them mean
// anything.
func TestGatePackagesIsNotEmpty(t *testing.T) {
	if len(GatePackageDirs) == 0 {
		t.Fatal("GatePackageDirs is EMPTY — the union adds nothing, so every delivery reads green " +
			"while running no gate at all; this is the vacuum the roster exists to close")
	}
	if got := len(GatePackagePatterns()); got != len(GatePackageDirs) {
		t.Fatalf("GatePackagePatterns() rendered %d patterns for %d directories — one composer "+
			"failure would leave a gate unaddressed", got, len(GatePackageDirs))
	}
	for _, p := range GatePackagePatterns() {
		if strings.TrimSpace(p) == "" || p == rootPackagePattern {
			t.Errorf("GatePackagePatterns() produced %q — an empty or root-only pattern addresses "+
				"nothing useful on the delivery path", p)
		}
	}
}

// TestGatePackagesNamesTheGatePackages pins the MEMBERSHIP against the reason the
// roster exists (testdata/heavy-tests.tsv's own sentence: the gate packages
// "must stay on the smoke path so a merge cannot land with its gates unrun"). It
// asserts the two names by their constants, so a rename cannot quietly drop one
// while the roster stays non-empty.
func TestGatePackagesNamesTheGatePackages(t *testing.T) {
	have := map[string]bool{}
	for _, dir := range GatePackageDirs {
		have[dir] = true
	}
	for _, want := range []string{GatePackageStandards, GatePackageStylegate} {
		if !have[want] {
			t.Errorf("GatePackageDirs %v does not carry %q — that gate package would never be run "+
				"by a branch whose diff does not touch it, which is the defect this roster closes",
				GatePackageDirs, want)
		}
		if got := PackagePattern(want); got != "./"+want+"/..." {
			t.Errorf("PackagePattern(%q) = %q — the pattern composer and the roster disagree", want, got)
		}
	}
}

// TestWithGatePackagesUnionsIntoAnySelection is the pure function's contract: the
// caller's own packages survive, the roster joins them, the result is sorted and
// deduplicated, and it never widens to the whole module (Rule 27 — this must not
// become the whole-tree run it replaces).
func TestWithGatePackagesUnionsIntoAnySelection(t *testing.T) {
	// The caller's selection deliberately ALREADY contains one roster member, so
	// the dedupe arm is measured rather than assumed.
	in := []string{"./internal/tools/...", PackagePattern(GatePackageStandards)}
	got := WithGatePackages(in)

	if len(got) != len(in)+len(GatePackageDirs)-1 {
		t.Fatalf("WithGatePackages(%v) = %v — expected the caller's packages plus the roster, "+
			"deduplicated (got %d entries, want %d)", in, got, len(got), len(in)+len(GatePackageDirs)-1)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"./internal/tools/...", PackagePattern(GatePackageStandards), PackagePattern(GatePackageStylegate)} {
		if !strings.Contains(joined, want) {
			t.Errorf("WithGatePackages(%v) = %v is missing %q", in, got, want)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("WithGatePackages(%v) = %v is not strictly sorted+deduped at index %d", in, got, i)
		}
	}
	for _, p := range got {
		if p == wholeModulePattern {
			t.Fatalf("WithGatePackages(%v) returned %v containing %q — the roster must add the gates, "+
				"never the whole module (Rule 27)", in, got, wholeModulePattern)
		}
	}
	// The input is not mutated: a caller that reuses its selection must not find
	// the roster appended to it (a shared slice is how one call's answer becomes
	// another's input).
	if len(in) != 2 || in[0] != "./internal/tools/..." {
		t.Fatalf("WithGatePackages mutated its argument: %v", in)
	}
}

// TestWithGatePackagesAnswersAnEmptySelection is the docs-only shape: a diff that
// touched no package now yields the ROSTER, not an empty selection. That is the
// measured case this epic rests on (a document-only landing moved a gate's man
// fixture and turned cmd/agent red — mergeset_cmdagent.go's header), and the
// answer to "nothing was touched" is the invariant, not silence.
func TestWithGatePackagesAnswersAnEmptySelection(t *testing.T) {
	got := WithGatePackages(nil)
	want := GatePackagePatterns()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("WithGatePackages(nil) = %v, want exactly the roster %v — an empty selection must "+
			"still run the gates", got, want)
	}
	if len(got) == 0 {
		t.Fatal("WithGatePackages(nil) is empty — a docs-only landing would run nothing")
	}
}

// TestWithGatePackagesInNarrowsToWhatTheTreeCarries pins the delivery-path form
// on a real directory: a roster member the tree does not carry is NOT added, the
// members it does carry ARE, and the caller's own selection survives untouched.
// Without this arm the roster would be unioned into every tree on the host and
// the command would name packages that do not exist there.
func TestWithGatePackagesInNarrowsToWhatTheTreeCarries(t *testing.T) {
	dir := t.TempDir()
	present := GatePackageDirs[0]
	absent := GatePackageDirs[len(GatePackageDirs)-1]
	if present == absent {
		t.Skipf("the roster holds one package (%q) — a narrowing test needs two to be a test",
			present)
	}
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(present)), 0o755); err != nil {
		t.Fatalf("materialise %s: %v", present, err)
	}
	got := WithGatePackagesIn(dir, []string{"./internal/tools/..."})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, PackagePattern(present)) {
		t.Errorf("WithGatePackagesIn(%s) = %v misses %q, which the tree carries", dir, got, PackagePattern(present))
	}
	if strings.Contains(joined, PackagePattern(absent)) {
		t.Errorf("WithGatePackagesIn(%s) = %v names %q, which the tree does NOT carry", dir, got, PackagePattern(absent))
	}
	if !strings.Contains(joined, "./internal/tools/...") {
		t.Errorf("WithGatePackagesIn(%s) = %v dropped the caller's own package", dir, got)
	}
	if missing := GatePackagesMissing(dir); len(missing) != len(GatePackageDirs)-1 {
		t.Errorf("GatePackagesMissing(%s) = %v, want every member but %q", dir, missing, present)
	}
	if missing := GatePackagesMissing(dir); len(missing) == 0 {
		t.Error("GatePackagesMissing reports nothing missing on a tree carrying ONE member — " +
			"an answer that cannot be wrong is not a measurement")
	}
}
