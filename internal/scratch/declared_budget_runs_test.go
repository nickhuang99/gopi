package scratch

// declared_budget_runs_test.go — the regression guard for the tree the
// 2026-10-12 `/`-exhaustion found outside every arm of this sweep.
//
// THE DEFECT (measured on this host): <scratch home>/queue/test-budget held 320
// `run-<epoch>-<pid>-tested-bin/` directories / 10.27 GB — one ~63 MB built
// `gopi` per `make test-budget` run — while the `/` partition hit 0 bytes free.
// TWO faces of the sweep were blind to it, and each blindness has its own test
// below:
//
//	TestBudgetRunsTreeWasOutsideTheDeclaredScope  — the SCOPE half. Before the
//	    fix the tree was in no declared row, so `gopi tool scratch-gc trees`
//	    printed no line for it and the resident tick never walked it. The test
//	    drives the declared set WITHOUT this row and asserts the fixture's aged
//	    run directory is planned by nothing — the pre-fix scope, executed.
//	TestQueueArmCannotReachBelowItsOwnChildren — the ARM half. The queue arm
//	    judges the queue's direct children individually, and `test-budget` is one
//	    of them: the CONTAINER was judged as ONE unit, so a single live
//	    `make test-budget` held all 320 runs. The test shows the queue's verdict
//	    is about the container alone (its children are never named by it).
//
// And the GREEN half: with the row declared, the aged, unheld run directories
// are candidates and are collected, the fresh one, every FLAT evidence file
// (`run-*.json`, `stamps.log`, the `last.json` symlink) and a foreign directory
// all survive, and the tree's ROOT survives.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// budgetRunsFixture builds a scratch home holding the tree in its REAL measured
// shape, returning the paths by role:
//
//	queue/test-budget/run-<aged>-tested-bin/  (a run's built binary, aged)
//	queue/test-budget/run-<held>-tested-bin/  (aged, but its .holder is live)
//	queue/test-budget/run-<fresh>-tested-bin/ (a run starting right now)
//	queue/test-budget/run-fresh.json          (FLAT evidence — never a candidate)
//	queue/test-budget/stamps.log              (FLAT evidence)
//	queue/test-budget/last.json -> …          (the SYMLINK the script's readers use)
//	queue/test-budget/not-ours/               (a foreign directory)
func budgetRunsFixture(t *testing.T) (home, tree, aged, held, fresh string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv(HomeEnvName, home)
	queue := filepath.Join(home, QueueDirName)
	tree = filepath.Join(queue, BudgetRunsDirName)
	if err := os.MkdirAll(queue, DirPerm); err != nil {
		t.Fatal(err)
	}
	// The queue CONTAINER is backdated far past every window: age alone must
	// never make a container's verdict decide for the runs inside it.
	ancient := time.Now().Add(-72 * time.Hour)
	if err := os.MkdirAll(tree, DirPerm); err != nil {
		t.Fatal(err)
	}

	mkRun := func(name string, age time.Duration) string {
		dir := filepath.Join(tree, name)
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "gopi"), make([]byte, 4096), 0o755); err != nil {
			t.Fatal(err)
		}
		if age > 0 {
			stamp := time.Now().Add(-age)
			if err := os.Chtimes(dir, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	aged = mkRun(BudgetRunPrefix+"100-1"+BudgetRunTestedBinSuffix, BudgetRunsStaleAge+2*time.Hour)
	held = mkRun(BudgetRunPrefix+"200-2"+BudgetRunTestedBinSuffix, BudgetRunsStaleAge+2*time.Hour)
	fresh = mkRun(BudgetRunPrefix+"300-3"+BudgetRunTestedBinSuffix, 0)

	// The FLAT evidence the tree exists to keep: a run's JSON stream, the run's
	// START RECORD (written before `go test`, so a killed run is queryable), the
	// appended stamps log, and the symlink that resolves the newest run.
	for _, name := range []string{
		BudgetRunPrefix + "100-1.json",
		BudgetRunPrefix + "100-1" + BudgetStartRecordSuffix,
		"stamps.log",
	} {
		if err := os.WriteFile(filepath.Join(tree, name), []byte("evidence\n"), FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(BudgetRunPrefix+"100-1.json", filepath.Join(tree, "last.json")); err != nil {
		t.Fatal(err)
	}
	// A FOREIGN directory: another writer's, with this tree's suffix and without
	// its tag. It must never be a candidate.
	if err := os.MkdirAll(filepath.Join(tree, "not-ours"+BudgetRunTestedBinSuffix), DirPerm); err != nil {
		t.Fatal(err)
	}
	// The queue container's own mtime, backdated so no test depends on how long
	// the fixture took to build.
	if err := os.Chtimes(tree, ancient, ancient); err != nil {
		t.Fatal(err)
	}
	return home, tree, aged, held, fresh
}

// TestBudgetRunsTreeIsDeclaredAndReachesItsAgedRuns is the GREEN half: the row
// exists, the plan names the aged run directory (and the bytes), the sweep takes
// it, and everything that is not a run survives.
func TestBudgetRunsTreeIsDeclaredAndReachesItsAgedRuns(t *testing.T) {
	_, tree, aged, held, fresh := budgetRunsFixture(t)
	RegisterHolder(held)
	t.Cleanup(func() { _ = os.Remove(filepath.Join(held, HolderFileName)) }) //stylegate:ignore t.Cleanup removal of the registration this test wrote; the assertion it supports already ran

	row, ok := treeByName(BudgetRunsTreeName)
	if !ok {
		t.Fatalf("no declared row named %q — the test-budget artifact tree is outside the sweep's scope again", BudgetRunsTreeName)
	}
	if got, err := row.Resolve(); err != nil || got != tree {
		t.Fatalf("the row resolves %q (err=%v), want the tree %q", got, err, tree)
	}

	plan, err := DeclaredTreePlan(row)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != aged {
		t.Fatalf("plan = %+v, want exactly the aged run directory %s", plan, aged)
	}
	if plan[0].Size <= 0 {
		t.Errorf("the planned entry reports size=%d — the epic's log line carries the reclaimed bytes", plan[0].Size)
	}

	removed, bytes, err := SweepDeclaredTree(row)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed=%d, want 1 (only the aged unheld run directory)", removed)
	}
	if bytes <= 0 {
		t.Errorf("bytes=%d, want the measured size of what was reclaimed", bytes)
	}
	if _, err := os.Lstat(aged); !os.IsNotExist(err) {
		t.Errorf("the aged run directory %s survived the sweep (err=%v)", aged, err)
	}
	// The HARD RULE and the CONTROLS, each of which the fix must not have bought
	// by widening the sweep.
	for _, kept := range []string{fresh, held, tree} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was collected — the age rule, the holder veto and the root invariant must hold: %v", kept, err)
		}
	}
	for _, evidence := range []string{
		filepath.Join(tree, "stamps.log"),
		filepath.Join(tree, BudgetRunPrefix+"100-1.json"),
		filepath.Join(tree, BudgetRunPrefix+"100-1"+BudgetStartRecordSuffix),
		filepath.Join(tree, "last.json"),
		filepath.Join(tree, "not-ours"+BudgetRunTestedBinSuffix),
	} {
		if _, err := os.Lstat(evidence); err != nil {
			t.Errorf("EVIDENCE/foreign entry %s did not survive: %v", evidence, err)
		}
	}
}

// TestBudgetRunsTreeKeepsEveryFlatEvidenceFileAndForeignDirectory pins the
// namespace predicate directly, so a future widening of the row fails here with
// the reason instead of silently deleting a report's cited artifact.
func TestBudgetRunsTreeKeepsEveryFlatEvidenceFileAndForeignDirectory(t *testing.T) {
	_, tree, _, _, _ := budgetRunsFixture(t)
	row, ok := treeByName(BudgetRunsTreeName)
	if !ok {
		t.Fatalf("the %q row must be declared", BudgetRunsTreeName)
	}
	entry := func(path string) (name string, isDir bool) {
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("lstat %s: %v", path, err)
		}
		return fi.Name(), fi.IsDir()
	}
	cases := []struct {
		path string
		want bool
		why  string
	}{
		{filepath.Join(tree, BudgetRunPrefix+"100-1"+BudgetRunTestedBinSuffix), true, "a run's built-binary directory is the one shape this tree takes"},
		{filepath.Join(tree, BudgetRunPrefix+"100-1.json"), false, "the per-run JSON stream is EVIDENCE a report cites — a flat entry is never a run"},
		{filepath.Join(tree, BudgetRunPrefix+"100-1"+BudgetStartRecordSuffix), false, "a run's START RECORD is evidence: it is written BEFORE `go test` starts and is what makes a KILLED run's stream findable, so collecting it would delete the index of the very runs whose streams are the tree's record"},
		{filepath.Join(tree, "stamps.log"), false, "the appended stamps log is evidence"},
		{filepath.Join(tree, "last.json"), false, "last.json is a SYMLINK to the newest run and the script's own reader resolves it"},
		{filepath.Join(tree, "not-ours"+BudgetRunTestedBinSuffix), false, "a name without this tree's run tag is another writer's"},
	}
	for _, tc := range cases {
		name, isDir := entry(tc.path)
		if got := row.entryName(name, isDir); got != tc.want {
			t.Errorf("entryName(%q)=%v, want %v — %s", name, got, tc.want, tc.why)
		}
	}
	// The ROOT is refused at any age, through the shared predicate — the
	// invariant every declared tree carries (the GOTMPDIR incident).
	if IsDeclaredTreeRoot(tree) != true {
		t.Fatalf("%s must be recognised as a declared tree root", tree)
	}
	if declaredEntryCollectable(row, tree, time.Now(), 0, heldTreeScan{}) {
		t.Fatalf("the ROOT of %s is a candidate — the invariant that saved GOTMPDIR is violated", BudgetRunsTreeName)
	}
	if err := removeCollectableOfTree(row, tree); err == nil {
		t.Fatalf("removeCollectableOfTree removed the ROOT of %s", BudgetRunsTreeName)
	}
	if _, err := os.Stat(tree); err != nil {
		t.Fatalf("the tree ROOT did not survive: %v", err)
	}
}

// TestBudgetRunsTreeWasOutsideTheDeclaredScope is the RED PROOF of the scope
// half, and it is executed rather than asserted in prose: it drives the SAME
// declared set the `trees` face and the resident tick walk, with this row
// REMOVED, and requires that the fixture's aged run directory is planned by
// nothing.
//
// That is exactly the pre-fix state — the tree was in no row at all, which is
// why `gopi tool scratch-gc trees --dry-run` printed 13 tree= lines and not one
// of them was this tree. If a future change makes some OTHER row reach into this
// tree, this test fails: that reach would be a second owner for one directory.
func TestBudgetRunsTreeWasOutsideTheDeclaredScope(t *testing.T) {
	_, _, aged, _, _ := budgetRunsFixture(t)

	var without []DeclaredTree
	for _, row := range DeclaredTrees() {
		if row.Name != BudgetRunsTreeName {
			without = append(without, row)
		}
	}
	if len(without) == len(DeclaredTrees()) {
		t.Fatalf("the declared set does not hold %q — the red proof would be vacuous", BudgetRunsTreeName)
	}
	for _, row := range without {
		plan, err := DeclaredTreePlan(row)
		if err != nil {
			continue // unconfigured on this host: it plans nothing here either
		}
		for _, e := range plan {
			if e.Path == aged || pathIsInside(e.Path, aged) {
				t.Fatalf("declared tree %q plans %s — the pre-fix scope was blind to this tree, and a second owner for its runs is a defect, not a fix",
					row.Name, e.Path)
			}
		}
	}
	// And with the row restored the very same walk DOES reach it: the proof
	// above is about the row, not about a fixture that is unreachable in
	// principle.
	row, ok := treeByName(BudgetRunsTreeName)
	if !ok {
		t.Fatalf("the %q row must be declared", BudgetRunsTreeName)
	}
	plan, err := DeclaredTreePlan(row)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var reached bool
	for _, e := range plan {
		if e.Path == aged {
			reached = true
		}
	}
	if !reached {
		t.Fatalf("the %q row does not reach its own aged run directory %s (plan=%+v)", BudgetRunsTreeName, aged, plan)
	}
}

// pathIsInside reports whether path lies at or under dir — the containment test
// the red proof asks about the reported candidate.
func pathIsInside(path, dir string) bool {
	if path == dir {
		return true
	}
	return len(path) > len(dir) && path[:len(dir)] == dir && path[len(dir)] == filepath.Separator
}

// TestQueueArmCannotReachBelowItsOwnChildren is the RED PROOF of the ARM half,
// measured rather than described: the queue arm judges the queue's DIRECT
// CHILDREN, and `test-budget` is one of them — so the verdict it returns is
// about the CONTAINER, and no run directory inside it is ever named.
//
// This is the shape the 2026-10-12 incident hit: one live `make test-budget`
// held the container, the container's verdict was `keep`, and all 320 run
// directories (10.27 GB) were shielded by that single entry. The declared-tree
// row above is what makes each run directory its own candidate — this test pins
// WHY that row had to be a new one instead of a queue-arm fix.
func TestQueueArmCannotReachBelowItsOwnChildren(t *testing.T) {
	home, tree, aged, _, _ := budgetRunsFixture(t)
	_ = home

	now := time.Now()
	// The queue's own predicate, on the container, with NO holder scan: the
	// verdict is one entry about one path, and it never descends.
	verdict := queueChildVerdict(tree, true, now, time.Hour, HolderScan{})
	if len(verdict.Path) == 0 || verdict.Path != tree {
		t.Fatalf("the queue arm's verdict is about %q, want the container %q — the unit of ownership is the direct child",
			verdict.Path, tree)
	}
	// The runs inside are below that unit: the queue arm can only ever return
	// the container, so an aged run directory is invisible to it by construction.
	plan, err := SweepHomePlan(tree, time.Hour)
	if err != nil {
		t.Fatalf("plan of the tree's own children: %v", err)
	}
	for _, p := range plan {
		if p.Path == aged {
			t.Fatalf("the queue arm planned %s — this test's premise (the arm cannot descend into a container) is wrong", aged)
		}
	}
}
