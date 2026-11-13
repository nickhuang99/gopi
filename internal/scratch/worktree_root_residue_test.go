package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// worktree_root_residue_test.go — the regression guards for the worktree-root
// CENSUS (worktree_root_residue.go).
//
// THE DEFECT EACH ONE PINS (measured 2026-10-07): /home/nick/workspace held 95
// `gopi-*` directories and exactly SEVEN were `git worktree list` registrations.
// Nothing reported the difference, so the root read as "many worktrees" and the
// residue families were in nobody's scope. The two failure modes a fix must not
// introduce are symmetric, and both are red here:
//
//	TOO GREEDY  a live worktree, an evidence directory or a person's own
//	            directory classified removable → testRealWorktreesAndOperatorDirsAreNeverRemovable
//	TOO BLIND   the stub family (the ONE removable class) not seen → testStubFamilyIsTheOnlyRemovableClass

// rootCensusFixture builds one child directory inside root and returns its path.
func rootCensusFixture(t *testing.T, root, name string, write func(dir string)) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if write != nil {
		write(dir)
	}
	return dir
}

// writeGitPointer writes git's linked-worktree pointer file.
func writeGitPointer(t *testing.T, dir, target string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, WorktreeStubGitPointerName),
		[]byte(WorktreeStubGitDirKey+target+"\n"), 0o644); err != nil {
		t.Fatalf("write pointer in %s: %v", dir, err)
	}
}

// planByName indexes a census by child name.
func planByName(plans []RootChildPlan) map[string]RootChildPlan {
	out := make(map[string]RootChildPlan, len(plans))
	for _, p := range plans {
		out[p.Name] = p
	}
	return out
}

// TestRootCensusClassifiesEveryChildWithEvidence is the positive census: one
// directory per class, plus a LIVE registration of the exact shape the stub
// predicate would otherwise collect (the near-miss that must be kept) and the
// flat-file shape that is neither.
func TestRootCensusClassifiesEveryChildWithEvidence(t *testing.T) {
	root := t.TempDir()
	liveGitdir := filepath.Join(root, "live-repo", ".git", "worktrees", "wt")
	if err := os.MkdirAll(liveGitdir, 0o755); err != nil {
		t.Fatalf("mkdir live gitdir: %v", err)
	}

	// ① a live worktree: the same NAME family and the same pointer shape as a
	//    stub, whose target EXISTS. The predicate keeps it; the census must say
	//    WORKTREE, not stub.
	rootCensusFixture(t, root, WorktreeStubNamePrefix+"01a0db4231427aa79740ff9916dae51f", func(dir string) {
		writeGitPointer(t, dir, liveGitdir)
	})
	// ② a dead-pointer stub: the only removable class.
	rootCensusFixture(t, root, WorktreeStubNamePrefix+"01a0db43ec807c36af636a3beda62be7", func(dir string) {
		writeGitPointer(t, dir, filepath.Join(root, "gone", "suite-agent-run-1", "x", ".git", "worktrees", "gopi-x"))
	})
	// ③ ad-hoc residue in the system's own naming families.
	rootCensusFixture(t, root, "gopi-attrib-evidence", nil)
	rootCensusFixture(t, root, "sbxref-T7Zc", nil)
	rootCensusFixture(t, root, "tmp-m2-6Zgx", nil)
	// ④ foreign: a name a person chose, and a flat file.
	rootCensusFixture(t, root, "homm3-m1", nil)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write flat file: %v", err)
	}

	plans, err := PlanWorktreeRootCensus(root)
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	got := planByName(plans)

	wantClass := map[string]RootChildClass{
		WorktreeStubNamePrefix + "01a0db4231427aa79740ff9916dae51f": RootChildLiveWorktree,
		WorktreeStubNamePrefix + "01a0db43ec807c36af636a3beda62be7": RootChildStub,
		"gopi-attrib-evidence": RootChildResidue,
		"sbxref-T7Zc":          RootChildResidue,
		"tmp-m2-6Zgx":          RootChildResidue,
		"homm3-m1":             RootChildForeign,
		"notes.txt":            RootChildForeign,
		"live-repo":            RootChildLiveWorktree, // a .git DIRECTORY: a repository
	}
	for name, want := range wantClass {
		p, ok := got[name]
		if !ok {
			t.Fatalf("census lost %s (it classified %d children)", name, len(plans))
		}
		if p.Class != want {
			t.Errorf("%s: class = %s, want %s (reason: %s)", name, RootChildClassLabel(p.Class), RootChildClassLabel(want), p.Reason)
		}
		if p.Reason == "" {
			t.Errorf("%s: no reason recorded — every class must carry its evidence", name)
		}
	}
	// REMOVABLE IS THE WHOLE SAFETY SURFACE: only the dead-pointer stub may be.
	for name, p := range got {
		wantRemovable := p.Class == RootChildStub
		if p.Removable != wantRemovable {
			t.Errorf("%s: removable = %t, want %t (class %s)", name, p.Removable, wantRemovable, RootChildClassLabel(p.Class))
		}
	}
}

// TestRealWorktreesAndOperatorDirsAreNeverRemovable is the KEEP side, asserted
// through the REAL sweep rather than through the census alone: after a census
// that names one removable stub, the sweep must remove that one and leave every
// other child — including the dead-looking directory a person named — on disk.
func TestRealWorktreesAndOperatorDirsAreNeverRemovable(t *testing.T) {
	root := t.TempDir()
	liveGitdir := filepath.Join(root, "live", ".git", "worktrees", "wt")
	if err := os.MkdirAll(liveGitdir, 0o755); err != nil {
		t.Fatalf("mkdir live gitdir: %v", err)
	}
	keep := []string{
		WorktreeStubNamePrefix + "01a0db4231427aa79740ff9916dae51f", // live registration
		"gopi-attrib-evidence", // evidence a report cites
		"sbxref-T7Zc",          // a Rule-34 sandbox probe
		"tmp-m2-6Zgx",          // a mktemp work dir
		"homm3-m1",             // a person's directory
	}
	rootCensusFixture(t, root, keep[0], func(dir string) { writeGitPointer(t, dir, liveGitdir) })
	for _, name := range keep[1:] {
		rootCensusFixture(t, root, name, nil)
	}
	// A DEAD-POINTER directory OUTSIDE the stub name family (a word, not a
	// generated row stem): the "too greedy" case a delete-by-dangling-pointer
	// predicate would have taken, and it must stay. `gopi-wtrecycle` is
	// deliberately NOT used here — its 9-letter alphanumeric body IS inside the
	// existing stub family (`[0-9a-zA-Z]{8,64}`), so the predicate collects it,
	// and that pre-existing behaviour is pinned by
	// TestDeadPointerInsideTheNameFamilyIsCollectableOutsideItIsNot instead of
	// being wished away in a fixture.
	rootCensusFixture(t, root, "annex-fixture", func(dir string) {
		writeGitPointer(t, dir, filepath.Join(root, "gone", "suite-agent-run-1", "x", ".git", "worktrees", "gopi-x"))
	})
	stub := WorktreeStubNamePrefix + "01a0db43ec807c36af636a3beda62be7"
	rootCensusFixture(t, root, stub, func(dir string) {
		writeGitPointer(t, dir, filepath.Join(root, "gone", "suite-agent-run-2", "x", ".git", "worktrees", "gopi-x"))
	})

	plans, err := PlanWorktreeRootCensus(root)
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	removable := 0
	for _, p := range plans {
		if p.Removable {
			removable++
		}
	}
	if removable != 1 {
		t.Fatalf("census named %d removable children, want exactly 1 (the dead-pointer stub)", removable)
	}

	removed, _, err := SweepWorktreeStubs(root)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != removable {
		t.Fatalf("the census named %d removable but the sweep removed %d — the two disagree", removable, removed)
	}
	if _, err := os.Lstat(filepath.Join(root, stub)); !os.IsNotExist(err) {
		t.Errorf("%s survived the sweep (lstat err = %v)", stub, err)
	}
	for _, name := range keep {
		if _, err := os.Lstat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s was removed by the sweep (lstat err = %v) — an unknown is a KEEP", name, err)
		}
	}
	// The hand-named dead pointer too: same SHAPE, wrong family, so it stays —
	// the predicate's scope is the name family, never "any dangling pointer".
	if _, err := os.Lstat(filepath.Join(root, "annex-fixture")); err != nil {
		t.Errorf("annex-fixture was removed (lstat err = %v) — a name outside the stub family is never removable", err)
	}
}

// TestRootCensusIsReadOnlyOnAMissingRoot: a read-only pass must not be able to
// bring a tree into existence, and an absent root is a reported zero rather than
// a failure (the family's own contract).
func TestRootCensusIsReadOnlyOnAMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-created")
	plans, err := PlanWorktreeRootCensus(root)
	if err != nil {
		t.Fatalf("census of a missing root must not fail: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("census of a missing root returned %d children", len(plans))
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("the census CREATED %s (lstat err = %v)", root, err)
	}
}

// TestRootCensusRefusesDangerousRoots: the arm deletes inside a directory the
// operator owns, so a mistyped root must be refused BY NAME before anything is
// read — the shared refusal, read through this entry point too.
func TestRootCensusRefusesDangerousRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory on this host: %v", err)
	}
	for _, root := range []string{"", ".", string(os.PathSeparator), home} {
		if _, err := PlanWorktreeRootCensus(root); err == nil {
			t.Errorf("census of %q was allowed; want the shared refusal", root)
		}
	}
}

// TestDeadPointerInsideTheNameFamilyIsCollectableOutsideItIsNot pins the ONE
// boundary the two keep-tests above depend on, in both directions, so neither a
// widened nor a narrowed name family can pass unnoticed:
//
//	INSIDE   a dead suite-run pointer whose name is `gopi-` + an 8..64 character
//	         alphanumeric stem is the stub family's own shape (measured: all 174
//	         stubs on this host are `gopi-` + 32 lowercase hex). `gopi-wtrecycle`
//	         is inside it, and the arm's dry run says so.
//	OUTSIDE  the same dead pointer under a name the family does not describe
//	         (`annex-fixture`, `homm3-m1`) is residue/foreign — reported, kept.
func TestDeadPointerInsideTheNameFamilyIsCollectableOutsideItIsNot(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{WorktreeStubNamePrefix + "01a0db43ec807c36af636a3beda62be7", true},
		{WorktreeStubNamePrefix + "wtrecycle", true},
		{WorktreeStubNamePrefix + "scratch", false},
		{WorktreeStubNamePrefix + "1", false},
		{"annex-fixture", false},
		{"homm3-m1", false},
	} {
		if got := WorktreeStubNameMatches(tc.name); got != tc.want {
			t.Errorf("WorktreeStubNameMatches(%q) = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// TestRootCensusNamesEveryChildOfTheMeasuredShape is the SIZE case, on a real
// directory (the actual residue count is what made the defect invisible), and it
// is where the census's own budget is measured: 2000 children must be classified
// in one pass with no unbounded tree walk.
//
// The fixture is a plain t.TempDir(), like every other fixture in this package:
// this package cannot import internal/testutil (testutil imports it, so the test
// binary would not build), and t.TempDir() is already off the `/` partition on
// this host: /tmp being a separate device is NOT assumed here — the device
// question is testutil's own check, tested where testutil can be imported.
func TestRootCensusNamesEveryChildOfTheMeasuredShape(t *testing.T) {
	root := t.TempDir()
	const n = 2000
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("gopi-%s%04d", strings.Repeat("0", 24), i)
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir child %d: %v", i, err)
		}
	}
	plans, err := PlanWorktreeRootCensus(root)
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	if len(plans) != n {
		t.Fatalf("census classified %d of %d children", len(plans), n)
	}
	for _, p := range plans {
		if p.Class != RootChildResidue {
			t.Fatalf("%s: class = %s, want residue", p.Name, RootChildClassLabel(p.Class))
		}
	}
}

// TestRandomSuffixPredicateSeparatesMintedNamesFromChosenOnes pins the ONE
// judgement call in the residue class: os.MkdirTemp's tail carries both a digit
// and a letter, a word or a counter a person typed does not. Both directions are
// asserted, because a predicate that only says YES would classify the operator's
// own directories as residue.
func TestRandomSuffixPredicateSeparatesMintedNamesFromChosenOnes(t *testing.T) {
	minted := []string{"tmp-m2-6Zgx", "vocab.tNMt", "sbxcheck-cfvY", "sbxref-T7Zc", "gopi-scratch-01a0fa9cba4b7a71b31b48d103e16730.P6bH"}
	for _, name := range minted {
		if !rootChildAdHocName(name) {
			t.Errorf("%s: classified FOREIGN; it is a name this system's tooling mints", name)
		}
	}
	chosen := []string{"homm3-m1", "backup", "20210804", "l1-red", "annex-fixture", "fog", "cplusplus", "purec", "vocab"}
	// `gopi-sandbox` is NOT in this list ON PURPOSE, and the reason is the class's
	// own definition: its `gopi-` prefix marks a directory THIS SYSTEM named. Both
	// of this host's own declared roots (`gopi-sandbox`, `gopi-test-scratch`)
	// therefore classify as residue and are REPORTED — which is the honest
	// answer, because the class reports a naming provenance, it does not accuse a
	// directory. What such a report is FOR is in the reason sentence: never
	// removed.
	for _, name := range chosen {
		if rootChildAdHocName(name) {
			t.Errorf("%s: classified RESIDUE; it is a name a person chose, and calling it residue is the first step toward removing it", name)
		}
	}
}
