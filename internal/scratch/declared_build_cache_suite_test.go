package scratch

// The build-cache SUITE-SANDBOX row: the shells a test run's GOCACHE sandbox
// leaves in the shared build-cache root (declared_build_cache_suite.go).
//
// Two things must hold for the row to be worth having, and each has its own
// case below: the PREDICATE must claim exactly the writer's names and nothing
// else that sits in that root (the three real per-repo keys are the neighbours
// it must not touch), and the SWEEP must reclaim an aged shell while keeping a
// fresh one — a fresh one is the run that is happening right now.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// suiteSandboxTestAgedShellAge is comfortably past the row's window: the
	// sweep is asked for a one-hour window below, so a shell older than that is
	// collectable whatever the tree's own constant is.
	suiteSandboxTestAgedShellAge = 30 * 24 * time.Hour
	// suiteSandboxTestWindow is the age window the sweep is asked for — one
	// hour, the smallest window the fixture can express without racing the
	// clock on a loaded host.
	suiteSandboxTestWindow = time.Hour
)

// TestBuildCacheSuiteSandboxPredicateIsPrefixPlusDigits pins the CONTENT rule.
// The refused half is the point: the prefix alone is not a name any writer
// creates, and the root's other neighbours (a 16-hex per-repo key, this file's
// own rows) must not be claimed by this row.
func TestBuildCacheSuiteSandboxPredicateIsPrefixPlusDigits(t *testing.T) {
	prefix := worktreeroots.SharedBuildCacheSuiteSandboxPrefix
	cases := []struct {
		name string
		want bool
		why  string
	}{
		{fmt.Sprintf("%s4336", prefix), true, "the writer's own shape (a real pid from the 2026-10-12 census)"},
		{fmt.Sprintf("%s4187039", prefix), true, "a seven-digit pid — the census holds these too"},
		{prefix, false, "the bare prefix is not a name the writer creates"},
		{prefix[:len(prefix)-1], false, "a name without the separator is not a run's sandbox"},
		{prefix + "-1", false, "an extra separator is not a pid"},
		{prefix + "1a", false, "a pid is digits — a hex run is the KEY shape, not this one"},
		{"1a6fd2a82928067e", false, "a per-repo key is the key row's entry, never this row's"},
		{"c18dae31bae596a6", false, "the second key shape the census found beside the shells"},
		{"gopi-build-cache", false, "the root's own name is never a candidate inside itself"},
		{"", false, "an empty name is not a sandbox"},
	}
	for _, c := range cases {
		if got := buildCacheSuiteSandboxName(c.name); got != c.want {
			t.Errorf("buildCacheSuiteSandboxName(%q) = %v, want %v — %s", c.name, got, c.want, c.why)
		}
	}
	// The exported face must be the SAME predicate (Rule 14): a caller that
	// asserts the writer's name against a second spelling would drift.
	if !BuildCacheSuiteSandboxNameIsOurs(fmt.Sprintf("%s1", prefix)) {
		t.Errorf("BuildCacheSuiteSandboxNameIsOurs must be the exported face of buildCacheSuiteSandboxName")
	}
}

// TestBuildCacheSuiteSandboxRowReclaimsAnAgedShellAndKeepsAFreshOne drives the
// REAL row (resolved through the tree set, not assembled here) against a fixture
// root: the aged shell goes, the fresh one — this run — stays, and the root
// itself is never a candidate.
func TestBuildCacheSuiteSandboxRowReclaimsAnAgedShellAndKeepsAFreshOne(t *testing.T) {
	home := t.TempDir()
	wtRoot := filepath.Join(home, worktreeroots.WorkspaceDirName)
	root := BuildCacheRootFor(wtRoot)
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	restore := installBuildCacheRootsForTest(func() []worktreeroots.Root {
		return []worktreeroots.Root{{Path: wtRoot, Role: worktreeroots.RoleWorktree, Source: "test"}}
	})
	defer restore()

	tree, ok := suiteSandboxTestTree(t, wtRoot)
	if !ok {
		return
	}

	fresh := seedSuiteSandboxShell(t, root, 4242, 0)
	aged := seedSuiteSandboxShell(t, root, 4336, suiteSandboxTestAgedShellAge)

	removed, freed, err := SweepDeclaredTree(tree)
	if err != nil {
		t.Fatalf("sweeping the %s row: %v", tree.Name, err)
	}
	if removed != 1 {
		t.Errorf("removed=%d, want 1 — exactly the aged shell (age is the whole predicate on this row)", removed)
	}
	if _, err := os.Lstat(aged); !os.IsNotExist(err) {
		t.Errorf("the aged shell %s survived (err=%v) — this is the residue class the row exists for", aged, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("the FRESH shell %s was collected: %v — a shell younger than the window may be the run in "+
			"progress, and the row must keep it", fresh, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the declared ROOT %s did not survive: %v", root, err)
	}
	// The freed figure is what the watchdog log reports; an empty shell's body is
	// zero, and zero is the correct answer (not a missing measurement).
	if freed != 0 {
		t.Errorf("freed=%d bytes for an empty shell, want 0", freed)
	}
}

// TestBuildCacheSuiteSandboxRowLeavesThePerRepoKeysAlone is the disjointness
// case (Rule 14's "two rows, one root" hazard): the key row and this row sit in
// the SAME directory, so the property that matters is that neither predicate
// claims the other's names.
func TestBuildCacheSuiteSandboxRowLeavesThePerRepoKeysAlone(t *testing.T) {
	home := t.TempDir()
	wtRoot := filepath.Join(home, worktreeroots.WorkspaceDirName)
	root := BuildCacheRootFor(wtRoot)
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	restore := installBuildCacheRootsForTest(func() []worktreeroots.Root {
		return []worktreeroots.Root{{Path: wtRoot, Role: worktreeroots.RoleWorktree, Source: "test"}}
	})
	defer restore()

	keyDir := seedBuildCacheKey(t, root, buildCacheFixtureKey, suiteSandboxTestAgedShellAge, "ancient\n")
	agedShell := seedSuiteSandboxShell(t, root, 4336, suiteSandboxTestAgedShellAge)

	keyTree, ok := treeByName(BuildCacheTreeName)
	if !ok {
		t.Fatalf("the %s tree must be declared", BuildCacheTreeName)
	}
	suiteTree, ok := suiteSandboxTestTree(t, wtRoot)
	if !ok {
		return
	}

	if buildCacheKeyName(filepath.Base(agedShell)) {
		t.Errorf("the KEY predicate claims the shell %s — the two rows would fight over one name",
			filepath.Base(agedShell))
	}
	if buildCacheSuiteSandboxName(filepath.Base(keyDir)) {
		t.Errorf("this row's predicate claims the per-repo key %s", filepath.Base(keyDir))
	}

	if _, _, err := SweepDeclaredTree(suiteTree); err != nil {
		t.Fatalf("sweeping %s: %v", suiteTree.Name, err)
	}
	if _, err := os.Stat(keyDir); err != nil {
		t.Errorf("the per-repo key %s was collected by the suite-sandbox row: %v — the key row owns aged "+
			"keys and this row must not reach them", keyDir, err)
	}
	if _, err := os.Lstat(agedShell); !os.IsNotExist(err) {
		t.Errorf("the aged shell survived this row's sweep (err=%v)", err)
	}
	// And the key row, for its part, must not touch the shells — checked by the
	// predicate above for the shared name, and here for the row's own listing.
	candidates, err := DeclaredTreePlan(suiteTree)
	if err != nil {
		t.Fatalf("planning %s: %v", suiteTree.Name, err)
	}
	for _, c := range candidates {
		if buildCacheKeyName(filepath.Base(c.Path)) {
			t.Errorf("the suite-sandbox row listed the key %s as its own candidate", c.Path)
		}
	}
	_ = keyTree // the key row is exercised by its own cases; this one only needs its predicate
}

// suiteSandboxTestTree resolves the suite-sandbox row for a fixture worktree
// root, skipping (with the reason) when the host has no such row.
func suiteSandboxTestTree(t *testing.T, wtRoot string) (DeclaredTree, bool) {
	t.Helper()
	want := filepath.Clean(wtRoot)
	for _, tree := range DeclaredTrees() {
		if tree.Class != TreeClassBuildCache || tree.Name != BuildCacheTreeName+BuildCacheSuiteSandboxTreeNameSuffix {
			continue
		}
		root, err := tree.Resolve()
		if err != nil {
			continue
		}
		if filepath.Clean(root) == BuildCacheRootFor(want) {
			return tree, true
		}
	}
	t.Skipf("no %s row resolves to %s on this host — the row set is rooted at %d worktree root(s), none of "+
		"them the fixture", BuildCacheTreeName+BuildCacheSuiteSandboxTreeNameSuffix, BuildCacheRootFor(want),
		len(buildCacheWorktreeRootsResolver()))
	return DeclaredTree{}, false
}

// seedSuiteSandboxShell creates one shell of the writer's shape — the directory
// the suite's GOCACHE sandbox leaves behind — aged by the given duration.
func seedSuiteSandboxShell(t *testing.T, root string, pid int, age time.Duration) string {
	t.Helper()
	dir := BuildCacheSuiteSandboxDirIn(root, pid)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if age > 0 {
		when := time.Now().Add(-age)
		if err := os.Chtimes(dir, when, when); err != nil {
			t.Fatalf("aging %s: %v", dir, err)
		}
	}
	return dir
}
