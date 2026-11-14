// scratch_dumps_test.go — the boundary between the SCRATCH dumps class trees
// (this package's declared rows and constants) and the census that reaches them
// (internal/sessionlayout's trees, this repository's CLI face).
//
// WHY IT EXISTS (Rules 13/14). The class directory NAMES are spelled twice on
// purpose — internal/deepseek owns `minimalCoreDumpClass` / `guardDumpClass` and
// cannot be imported by this package (it imports this one through its own
// `scratch.DumpDir` call), so this package re-spells them — and the tree NAMES are
// spelled in two packages for the same reason. Two literals with nothing joining
// them drift. This test IS the join, in the two forms the sibling gate already
// uses: a VALUE comparison where both sides are Go values, and a SOURCE read where
// the far side is an unexported constant in a package that cannot be imported
// back.
//
// THE GATE IT FEEDS. The census prints `decision=bound` for an entry and names a
// declared tree; internal/scratch's sibling gate refuses a bound family whose tree
// nothing sweeps — but it asserts that over
// `sessionlayout.SiblingBoundTreeNames()`, the PROFILE registry. These two trees
// hang off a different root and are enumerated separately
// (sessionlayout.ScratchDumpsTrees), which is exactly why the same assertion has
// to be made here: a `bound:` claim is worth nothing if the sweep holds no such
// row, and nothing else in the tree would notice.
package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// scratchDumpGateRepoRoot finds the repository root for the SOURCE reads below —
// the same walk the declared-tree gate uses, and it fails loudly rather than
// skipping: an anchor that cannot be read must not read as "covered".
func scratchDumpGateRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// TestScratchDumpTreeNamesMatchTheCensus pins the two tree names as VALUES on
// both sides — the census enumerates them and the CLI's `--tree NAME` resolves
// them, so a rename in one package alone would leave the documented command
// naming a tree nothing carries.
func TestScratchDumpTreeNamesMatchTheCensus(t *testing.T) {
	want := []string{ScratchDumpsTreeName, ScratchDumps1214TreeName}
	got := sessionlayout.ScratchDumpsTreeNames()
	if len(got) != len(want) {
		t.Fatalf("the census enumerates %d scratch dumps trees (%v), this package declares %d (%v)",
			len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scratch dumps tree %d: census name %q != declared name %q — `--tree %s` would resolve to nothing",
				i, got[i], want[i], want[i])
		}
	}
	// The two rows must be DISTINCT trees: they share one implementation, so a
	// copy-paste slip would point both at the same directory and the census
	// would report one of the two classes twice.
	if ScratchDumpsTreeName == ScratchDumps1214TreeName || ScratchDumpsClassDirName == ScratchDumps1214ClassDirName {
		t.Errorf("the two scratch dump rows share a name (%q/%q) or a leaf (%q/%q) — they are two classes, not one",
			ScratchDumpsTreeName, ScratchDumps1214TreeName, ScratchDumpsClassDirName, ScratchDumps1214ClassDirName)
	}
}

// TestScratchDumpClassNamesMatchTheirWriters is the SOURCE-read half: the leaf
// names this package re-spells are held against the files that WRITE there. A
// rename in internal/deepseek would otherwise leave the declared row and the
// census describing a directory the writer no longer creates, with every value
// comparison above still green.
func TestScratchDumpClassNamesMatchTheirWriters(t *testing.T) {
	root := scratchDumpGateRepoRoot(t)
	sources := []struct {
		why  string
		rel  string
		want string
	}{
		{
			why:  "the generic error-path dump WRITER's class directory",
			rel:  filepath.Join("internal", "deepseek", "minimal_core.go"),
			want: `minimalCoreDumpClass = "` + ScratchDumpsClassDirName + `"`,
		},
		{
			why:  "the 1214 dump WRITER's class directory",
			rel:  filepath.Join("internal", "deepseek", "error_1214.go"),
			want: `guardDumpClass = "` + ScratchDumps1214ClassDirName + `"`,
		},
		{
			// The writers' own join: the class directory lives under the
			// scratch home's `dumps/`, which is what ScratchDumpsClassRoot
			// re-spells. A rename of DumpsDirName there would otherwise leave
			// this package's root pointing at a directory nobody writes.
			why:  "the scratch dumps ROOT's own directory name",
			rel:  filepath.Join("internal", "scratch", "scratch.go"),
			want: `DumpsDirName = "` + DumpsDirName + `"`,
		},
	}
	for _, s := range sources {
		body, err := os.ReadFile(filepath.Join(root, s.rel))
		if err != nil {
			t.Fatalf("read %s (%s): %v — a gate whose anchor disappeared must not read as covered", s.rel, s.why, err)
		}
		if !strings.Contains(string(body), s.want) {
			t.Errorf("%s no longer carries %q (%s) — this package's constant describes a location nothing writes",
				s.rel, s.want, s.why)
		}
	}
}

// TestScratchDumpWindowsAgree pins the ONE value relationship between the two dump
// homes' windows: the scratch class trees and the profile trees keep the SAME
// 14-day window, because a dump is the same kind of artefact wherever the knob
// pointed it. The two are separate NAMES (each is a decision the declared-set gate
// accounts for by name), so nothing but this assertion holds their values
// together — and a drift here would make the census report one window and the
// sweep apply another for the same file.
func TestScratchDumpWindowsAgree(t *testing.T) {
	if ScratchDumpsStaleAge != SiblingDumpsStaleAge {
		t.Errorf("the scratch dumps window (%v) differs from the profile dumps window (%v): the same artefact would age differently depending on which knob pointed at it",
			ScratchDumpsStaleAge, SiblingDumpsStaleAge)
	}
	// And the relationship is not vacuous: the window must be one the sweep can
	// actually reach (a zero or negative window would make every measured entry
	// immediately collectable, which is a different rule entirely).
	if ScratchDumpsStaleAge <= 0 {
		t.Errorf("ScratchDumpsStaleAge is %v — a non-positive window makes every aged entry collectable at once", ScratchDumpsStaleAge)
	}
}

// TestScratchDumpRowsNameDeclaredTreesAndBoundTheRightRoot is the gate the
// census's own claim rests on: for EACH scratch dumps tree the census walks there
// is a declared row of the same name, that row's family is `bound`, the row claims
// the dump FILE family the profile rows claim, and the row's root is the class
// directory the WRITER MkdirAll's — asserted by joining the leaf onto the scratch
// home's `dumps/` rather than by comparing against a second spelling.
func TestScratchDumpRowsNameDeclaredTreesAndBoundTheRightRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnvName, home)
	sessionlayout.RegisterScratchDumpsRootResolvers(ScratchDumpClassRoots())
	t.Cleanup(func() { sessionlayout.RegisterScratchDumpsRootResolvers(ScratchDumpClassRoots()) })

	declared := map[string]DeclaredTree{}
	for _, tree := range DeclaredTrees() {
		declared[tree.Name] = tree
	}
	trees := sessionlayout.ScratchDumpsTrees()
	if len(trees) == 0 {
		t.Fatal("the census enumerates no scratch dumps trees — this gate's subject is empty and cannot be green")
	}
	leaves := map[string]string{
		ScratchDumpsTreeName:     ScratchDumpsClassDirName,
		ScratchDumps1214TreeName: ScratchDumps1214ClassDirName,
	}
	for _, tree := range trees {
		row, ok := declared[tree.Name]
		if !ok {
			t.Errorf("the census walks tree %q but DeclaredTrees() holds no such row — `decision=bound` names a tree nothing sweeps", tree.Name)
			continue
		}
		leaf, ok := leaves[tree.Name]
		if !ok {
			t.Errorf("tree %q has no class leaf in this test's table — the gate would assert nothing about it", tree.Name)
			continue
		}
		if row.Class != TreeClassErrorDump {
			t.Errorf("declared tree %q carries class %q, not %q — the logfmt line would misattribute the dumps", tree.Name, row.Class, TreeClassErrorDump)
		}
		if row.Kind != EntryKindFileSuffix || row.Suffix != siblingDumpFileSuffix || row.EntryMatch == nil {
			t.Errorf("declared tree %q is not the profile dump rows' own shape (file suffix %q + namespace predicate): a blunt rule over the dump tree would collect files the writers never wrote",
				tree.Name, siblingDumpFileSuffix)
			continue
		}
		if !row.EntryMatch("20260101-000000" + siblingDumpFileSuffix) {
			t.Errorf("declared tree %q does not claim its own dump file name", tree.Name)
		}
		if row.EntryMatch("not-a-dump.txt") {
			t.Errorf("declared tree %q claims a non-dump file — the namespace predicate is too wide", tree.Name)
		}
		// The bound must be BOUND: a family with no decision, or one bound to a
		// different tree, would make the census's `decision=bound` line a lie.
		var bound bool
		for _, f := range tree.Families {
			if f.Decision == sessionlayout.DecisionBound && f.Tree == tree.Name {
				bound = true
			}
		}
		if !bound {
			t.Errorf("tree %q registers no family with decision=bound naming it — the census cannot print the decision the sweep applies", tree.Name)
		}
		// The root, by JOIN rather than by a second literal: the class
		// directory is the scratch home's `dumps/<class>`.
		want := filepath.Join(home, DumpsDirName, leaf)
		got, err := row.Resolve()
		if err != nil {
			t.Errorf("declared tree %q resolver failed: %v", tree.Name, err)
			continue
		}
		if got != want {
			t.Errorf("declared tree %q sweeps %q, not %q — the row bounds a directory the writers never fill", tree.Name, got, want)
		}
		censusRoot, err := tree.Resolve()
		if err != nil {
			t.Fatalf("census tree %q resolver failed: %v (the seam must be wired for the census to read a root at all)", tree.Name, err)
		}
		if censusRoot != want {
			t.Errorf("the census reports root %q for %q while the sweep bounds %q — the census would describe a tree the sweep does not hold",
				censusRoot, tree.Name, want)
		}
		// And the writer's own join agrees, which is the fact both constants
		// exist to preserve.
		if writer := DumpDir(leaf); writer != want {
			t.Errorf("DumpDir(%q) = %q, the declared row resolves %q — the writer and the bound disagree about where this class lives", leaf, writer, want)
		}
	}
	// The two trees must not resolve to the same directory.
	a, _ := sessionlayout.ScratchDumpsTreeRoot(ScratchDumpsTreeName)     //stylegate:ignore resolves the dumps tree root of the name constant under test; an error would be the defect this gate is looking for
	b, _ := sessionlayout.ScratchDumpsTreeRoot(ScratchDumps1214TreeName) //stylegate:ignore resolves the second tree root, compared against the first
	if a == b {
		t.Errorf("both scratch dumps trees resolve to %q — one of the two classes would be censused twice", a)
	}
}

// TestScratchDumpClassTreesBoundOnlyTheirOwnDumps is the sweep-side half of the
// same fact, driven through the REAL plan (one predicate, two faces): an ancient
// dump in the class directory is collectable, a fresh one is not, a file the
// writers never minted is not claimed at all, and the class directory's own ROOT
// is refused as a candidate at any age.
func TestScratchDumpClassTreesBoundOnlyTheirOwnDumps(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnvName, home)
	sessionlayout.RegisterScratchDumpsRootResolvers(ScratchDumpClassRoots())

	root := filepath.Join(home, DumpsDirName)
	class := filepath.Join(root, ScratchDumpsClassDirName)
	if err := os.MkdirAll(class, DirPerm); err != nil {
		t.Fatal(err)
	}
	ancient := filepath.Join(class, "20260101-000000"+siblingDumpFileSuffix)
	fresh := filepath.Join(class, "20260102-000000"+siblingDumpFileSuffix)
	foreign := filepath.Join(class, "keep-me.txt")
	for _, p := range []string{ancient, fresh, foreign} {
		if err := os.WriteFile(p, []byte("{}\n"), FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, p := range []string{ancient, foreign} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	// The class directory is aged LAST: seeding a child moves the parent's mtime,
	// so a parent backdated before its children were written reads as freshly
	// touched.
	if err := os.Chtimes(class, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(root, old, old); err != nil {
		t.Fatal(err)
	}

	var row DeclaredTree
	for _, tree := range DeclaredTrees() {
		if tree.Name == ScratchDumpsTreeName {
			row = tree
		}
	}
	if row.Name == "" {
		t.Fatalf("no declared row named %q", ScratchDumpsTreeName)
	}
	plan, err := DeclaredTreePlan(row)
	if err != nil {
		t.Fatalf("plan %s: %v", row.Name, err)
	}
	collectable := map[string]bool{}
	for _, e := range plan {
		collectable[e.Path] = true
	}
	if !collectable[ancient] {
		t.Errorf("a 30-day-old dump %s is not in %s's own plan (window %v) — the bound collects nothing", ancient, row.Name, row.StaleAge)
	}
	if collectable[fresh] {
		t.Errorf("a fresh dump %s is collectable — the window is not applied", fresh)
	}
	if collectable[foreign] {
		t.Errorf("a file the writers never minted (%s) is collectable — the namespace predicate is too wide", foreign)
	}
	if collectable[class] {
		t.Errorf("the class directory %s itself is in the plan — the sweep would remove a declared ROOT as a unit", class)
	}
	if !IsDeclaredTreeRoot(class) {
		t.Errorf("%s is not recognised as a declared root — the invariant that protects it from its own sweep is not in force", class)
	}
}

// TestScratchDumpsRootRefusesToInventATree pins the missing-root contract: when
// the writers have never run on this host, the resolver answers a path (the census
// reports "no root on this host") and NOTHING is created — a sweep that
// materialised the tree it bounds would make "the writers have never run" and "the
// tree is empty" render the same.
func TestScratchDumpsRootRefusesToInventATree(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnvName, home)
	got, err := ScratchDumpsClassRoot(ScratchDumpsClassDirName)
	if err != nil {
		t.Fatalf("resolve class root: %v", err)
	}
	if want := filepath.Join(home, DumpsDirName, ScratchDumpsClassDirName); got != want {
		t.Errorf("class root resolved to %q, not %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(home, DumpsDirName)); !os.IsNotExist(err) {
		t.Errorf("%s exists after resolution (stat err=%v) — resolving a tree must not create it", filepath.Join(home, DumpsDirName), err)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("%s exists after resolution (stat err=%v) — resolving a tree must not create it", got, err)
	}
}
