package scratch

// declared_tmpdir_residue_test.go — the regression guard for the DECLARED TEMP
// ROOT's own residue, the tree nothing bounded until 2026-11-02.
//
// THE DEFECT (measured on host-a): `_tmp` holds 20 886 MB across 528
// top-level entries. The only declared row that reached it was `go-tmp`
// (106 `go-build*` / 7 466 MB); every other family — 224 run-chosen names /
// 11 815 MB, plus the residue namespace — was named by no row, so the resident
// tick planned nothing there while the tree grew without a bound.
//
// WHAT THIS ROW MAY TAKE, and what it must refuse, are the two halves below:
//
// TestTmpDirResidueTreeIsDeclaredAndReachesOnlyItsOwnNamespace — the GREEN half:
// an aged `gopi-*` / `probe-*` directory is planned and collected, while a FRESH
// one, a HELD one, a directory a run named itself (`sigpipe-repro-…`) and the
// tree's own ROOT all survive.
// TestTmpDirResidueTreeWasOutsideTheDeclaredScope — the RED half, EXECUTED: the
// same declared set with this row removed plans nothing, which is exactly the
// pre-fix state and the reason the 2026-11-02 census found 18.6 GB unreachable.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// tmpDirResidueFixture builds a temp root in the MEASURED shape of `_tmp`: the
// residue families, a run-chosen name, a `go-build*` directory (another row's
// namespace in the same root) and a live-held entry.
func tmpDirResidueFixture(t *testing.T) (root, aged, held, fresh, foreign, goBuild string) {
	t.Helper()
	root = t.TempDir()
	// The row resolves the process temp root when TMPDIR is explicit — pinning it
	// makes the fixture the root the row reads, and nothing else.
	t.Setenv(TmpDirEnvName, root)
	t.Setenv(HomeEnvName, t.TempDir())

	mk := func(name string, age time.Duration) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "payload.bin"), make([]byte, 4096), FilePerm); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	aged = mk(BaseResiduePrefixGopi+"01a19555e1f7", TmpDirResidueStaleAge+2*time.Hour)
	held = mk(BaseResiduePrefixProbe+"home-held00", TmpDirResidueStaleAge+2*time.Hour)
	fresh = mk(BaseResiduePrefixGopi+"fresh000", 0)
	// A name a RUN chose. It carries no residue token, so no row may take it —
	// this is the 224-entry / 11 815 MB majority of the real root.
	foreign = mk("sigpipe-repro-1793506307", TmpDirResidueStaleAge+2*time.Hour)
	// Another row's namespace in the same root: `go-build*` belongs to `go-tmp`.
	goBuild = mk(GoBuildDirEntryPrefix+"2498748496", GoTmpStaleAge+2*time.Hour)
	return root, aged, held, fresh, foreign, goBuild
}

// TestTmpDirResidueTreeIsDeclaredAndReachesOnlyItsOwnNamespace is the GREEN half.
func TestTmpDirResidueTreeIsDeclaredAndReachesOnlyItsOwnNamespace(t *testing.T) {
	root, aged, held, fresh, foreign, goBuild := tmpDirResidueFixture(t)
	RegisterHolder(held)
	t.Cleanup(func() { _ = os.Remove(filepath.Join(held, HolderFileName)) }) //stylegate:ignore t.Cleanup removal of the registration this test wrote; the assertion it supports already ran

	row, ok := treeByName(TmpDirResidueTreeName)
	if !ok {
		t.Fatalf("no declared row named %q — the declared temp root's residue is outside the sweep's scope again", TmpDirResidueTreeName)
	}
	if got, err := row.Resolve(); err != nil || got != CanonicalPath(root) {
		t.Fatalf("the row resolves %q (err=%v), want the fixture root %q", got, err, root)
	}

	plan, err := DeclaredTreePlan(row)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != aged {
		t.Fatalf("plan = %+v, want exactly the aged residue directory %s (fresh, held, run-chosen and go-build* must all be refused)", plan, aged)
	}

	removed, bytes, err := SweepDeclaredTree(row)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 || bytes <= 0 {
		t.Errorf("removed=%d bytes=%d, want 1 directory and its measured size", removed, bytes)
	}
	if _, err := os.Lstat(aged); !os.IsNotExist(err) {
		t.Errorf("the aged residue directory %s survived the sweep (err=%v)", aged, err)
	}
	// THE CONTROLS — each one is a way the fix could have bought its green by
	// widening the sweep, so each is asserted separately.
	for _, kept := range []string{fresh, held, foreign, goBuild, root} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was collected — the age rule, the holder veto, the namespace predicate and the root invariant must all hold: %v", kept, err)
		}
	}
}

// TestTmpDirResidueTreeKeepsEveryRunChosenName pins the NAMESPACE predicate
// directly, so a future widening of the row fails here with the reason instead of
// silently deleting a report's cited artifact.
func TestTmpDirResidueTreeKeepsEveryRunChosenName(t *testing.T) {
	_, _, _, _, _, _ = tmpDirResidueFixture(t)
	row, ok := treeByName(TmpDirResidueTreeName)
	if !ok {
		t.Fatalf("the %q row must be declared", TmpDirResidueTreeName)
	}
	if row.EntryMatch == nil {
		t.Fatalf("the row carries no namespace predicate — every aged directory in the declared temp root would be collectable")
	}
	cases := []struct {
		name string
		want bool
		why  string
	}{
		{BaseResiduePrefixGopi + "01a19555e1f7", true, "gopi-<token> is the residue namespace this repository generates"},
		{BaseResiduePrefixProbe + "home-17p17396", true, "probe-<token> is a probe fixture the repository creates"},
		{BaseResiduePrefixGocache + "9f3a2c1b7e4d", true, "gocache-<token> is the legacy build-cache family; the token must be MACHINE-GENERATED (a base36-ish key), which is the predicate's own rule"},
		{"sigpipe-repro-1793506307", false, "a run CHOSE this name — no token makes it ours, and the real root holds 224 such entries"},
		{"walrep", false, "a run-chosen name with no token at all"},
		{GoBuildDirEntryPrefix + "2498748496", false, "go-build* belongs to the go-tmp row; two rows taking one entry is a second owner"},
		{"gopi-", false, "the prefix alone with no token is a name a human typed"},
	}
	for _, tc := range cases {
		if got := row.EntryMatch(tc.name); got != tc.want {
			t.Errorf("EntryMatch(%q)=%v, want %v — %s", tc.name, got, tc.want, tc.why)
		}
	}
	// The ROOT is refused at any age — the invariant that saved GOTMPDIR.
	if IsDeclaredTreeRoot(CanonicalPath(os.Getenv(TmpDirEnvName))) != true {
		t.Fatalf("the fixture root must be recognised as a declared tree root")
	}
}

// TestTmpDirResidueTreeWasOutsideTheDeclaredScope is the RED PROOF, executed:
// with this row removed, the fixture's aged residue is planned by nothing — the
// pre-fix state, when 18.6 GB of the declared temp root answered to no row.
func TestTmpDirResidueTreeWasOutsideTheDeclaredScope(t *testing.T) {
	_, aged, _, _, _, _ := tmpDirResidueFixture(t)

	var without []DeclaredTree
	for _, row := range DeclaredTrees() {
		if row.Name != TmpDirResidueTreeName {
			without = append(without, row)
		}
	}
	if len(without) == len(DeclaredTrees()) {
		t.Fatalf("the declared set does not hold %q — the red proof would be vacuous", TmpDirResidueTreeName)
	}
	for _, row := range without {
		root, err := row.Resolve()
		if err != nil {
			continue
		}
		if !PathWithin(aged, root) {
			continue
		}
		plan, perr := DeclaredTreePlan(row)
		if perr != nil {
			continue
		}
		for _, entry := range plan {
			if entry.Path == aged {
				t.Fatalf("row %q plans %s — another tree reaches into the declared temp root's residue, which is a SECOND owner for one directory", row.Name, aged)
			}
		}
	}
	if _, err := os.Stat(aged); err != nil {
		t.Fatalf("the fixture's aged residue directory is gone before any sweep: %v", err)
	}
}
