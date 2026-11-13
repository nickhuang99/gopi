package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// THE RED PROOF FOR THE TWO-ARM WINDOW DEFECT (task 01a17b8b-581a).
//
// THE DEFECT, as measured on 2026-10-27: one declared tree was judged by two
// different clocks depending on which arm REACHED it. The `trees` arm and the
// resident tick passed the row's own StaleAge; the HOME arm reached the same tree
// through its queue walk (queueSweepChildren → declaredTreeForRoot →
// sweepDeclaredTreeFromQueue) and passed its own GLOBAL window, which
// DeclaredTree.staleAge let WIN whenever it was non-zero. With
// `--scratch-gc-stale-min 1` the `trees` arm reported `stale=6h0m0s collectable=0`
// while the home arm planned all 401 entries of the same tree — a 24h/336h tree
// collected on a one-minute window, i.e. a data-loss-shaped disagreement rather
// than a display difference.
//
// WHAT THIS TEST PINS: the queue arm's interposition must judge the container by
// the CONTAINER'S OWN row, so the fixture is the exact disagreement — a tree whose
// constant is 6h, holding an entry that is 1h old, swept by a home arm whose
// window is ONE MINUTE. The entry is INSIDE its tree's window and OUTSIDE the home
// arm's, so the two answers differ and only one of them may be implemented.
//
// It is deliberately driven through SweepHome — the real HOME arm, not a helper —
// because the defect lived in the wiring between the arm and the row, and a test
// that called SweepDeclaredTree directly would be asserting about the one face
// that was already correct.

// armWindowFixtureTreeConstant is the window the FIXTURE TREE declares: 6 hours.
// It is the measured constant of the row this defect was found on
// (BudgetRunsStaleAge == DefaultStaleAge) and it is stated here as a fixture
// value so the test keeps meaning if that row's constant is ever retuned.
const armWindowFixtureTreeConstant = 6 * time.Hour

// armWindowFixtureEntryAge is how old the fixture's entries are: ONE HOUR, i.e.
// inside the tree's window and five hours inside it — and wildly outside the home
// arm's window below.
const armWindowFixtureEntryAge = time.Hour

// armWindowFixtureHomeWindow is the window the HOME arm is driven with: one
// minute. It is the measured invocation (`--scratch-gc-stale-min 1`) that made the
// two arms disagree, and it is the only thing that may NOT decide this tree.
const armWindowFixtureHomeWindow = time.Minute

// TestDeclaredTreeQueueArmUsesTheRowsOwnWindow is the red proof. On the pre-fix
// code the home arm collected every fixture entry (`removed=3`, entries gone);
// with the repair it collects NONE, because the row's own window is the one
// judgement of that row.
func TestDeclaredTreeQueueArmUsesTheRowsOwnWindow(t *testing.T) {
	home := pinScratchHome(t)
	queue := QueueDir()
	assertInsideTree(t, home, queue)
	if queue != filepath.Join(home, QueueDirName) {
		t.Fatalf("QueueDir() = %q, want it under the pinned scratch home %q — the fixture cannot fence what it does not own", queue, home)
	}
	if err := os.MkdirAll(queue, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", queue, err)
	}

	// The row whose ROOT the queue walk will find. It is resolved through the
	// REAL declared set (not assembled here): the interposition matches on the
	// resolved root, so a hand-built row would not be found and this test would
	// assert about the queue's own child verdict instead of the arm's.
	tree, ok := treeByName(BudgetRunsTreeName)
	if !ok {
		t.Fatalf("the %s row must be declared — it is the tree this defect was measured on", BudgetRunsTreeName)
	}
	if tree.StaleAge != armWindowFixtureTreeConstant {
		t.Fatalf("%s declares StaleAge=%s, want the %s this fixture is built around — retune the fixture constants with the row",
			BudgetRunsTreeName, tree.StaleAge, armWindowFixtureTreeConstant)
	}
	root, err := tree.Resolve()
	if err != nil {
		t.Fatalf("resolve %s: %v — the row must resolve under the pinned scratch home", BudgetRunsTreeName, err)
	}
	assertInsideTree(t, home, root)
	if filepath.Dir(root) != queue {
		t.Fatalf("%s resolves to %q, which is not a child of the queue %q — the queue walk would never reach it and this test would prove nothing",
			BudgetRunsTreeName, root, queue)
	}
	if err := os.MkdirAll(root, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}

	// The candidates: the writer's own shape (run-<tag>-tested-bin), aged INSIDE
	// the row's window and far outside the home arm's.
	const entries = 3
	aged := make([]string, 0, entries)
	for _, tag := range []string{"1793000001-100001", "1793000002-100002", "1793000003-100003"} {
		dir := filepath.Join(root, BudgetRunPrefix+tag+BudgetRunTestedBinSuffix)
		assertInsideTree(t, home, dir)
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "payload.bin"), make([]byte, 4096), FilePerm); err != nil {
			t.Fatalf("write payload in %s: %v", dir, err)
		}
		stamp := time.Now().Add(-armWindowFixtureEntryAge)
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatalf("age %s: %v", dir, err)
		}
		aged = append(aged, dir)
	}

	// CONTROL, on the same fixture: the tree's OWN faces must call these entries
	// collectable had their age passed the row's window — i.e. the fixture is
	// INSIDE the tree's window by the row's own predicate, not by a number this
	// test invented.
	if plan, perr := DeclaredTreePlan(tree); perr != nil {
		t.Fatalf("plan %s: %v", tree.Name, perr)
	} else if len(plan) != 0 {
		t.Fatalf("control: the row's own plan lists %d entries %+v, but every fixture entry is %s old inside a %s window — the fixture is wrong about the tree's clock",
			len(plan), plan, armWindowFixtureEntryAge, tree.StaleAge)
	}

	// THE SUBJECT: the HOME arm, with its own (much shorter) window.
	removed, err := SweepHome(home, armWindowFixtureHomeWindow)
	if err != nil {
		t.Fatalf("sweep home: %v", err)
	}
	if removed != 0 {
		t.Errorf("the home arm removed %d entrie(s) from a %s tree on its own %s window — a declared tree must be judged by ITS OWN window on EVERY arm (this is the 2026-10-27 data-loss disagreement)",
			removed, tree.StaleAge, armWindowFixtureHomeWindow)
	}
	for _, dir := range aged {
		if _, serr := os.Stat(dir); serr != nil {
			t.Errorf("entry %s was collected although it is %s old inside its row's %s window: %v — the tree's constant must win on the queue arm too",
				filepath.Base(dir), armWindowFixtureEntryAge, tree.StaleAge, serr)
		}
	}

	// AND the root survives: the interposition never removes the container, on
	// any window (the GOTMPDIR invariant).
	if _, serr := os.Stat(root); serr != nil {
		t.Errorf("the declared ROOT %s did not survive the home arm: %v", root, serr)
	}

	// THE OTHER DIRECTION, so this cannot pass by the arm being unable to collect
	// at all: age one entry PAST the row's window and the SAME home arm (same
	// short window) must take exactly that one — the row's own clock still decides,
	// it just decides differently now.
	past := aged[0]
	stamp := time.Now().Add(-(tree.StaleAge + time.Hour))
	if err := os.Chtimes(past, stamp, stamp); err != nil {
		t.Fatalf("age %s past the row window: %v", past, err)
	}
	removed, err = SweepHome(home, armWindowFixtureHomeWindow)
	if err != nil {
		t.Fatalf("second sweep home: %v", err)
	}
	if removed != 1 {
		t.Errorf("the second pass removed %d, want exactly 1 (the entry aged past the row's own %s window) — an arm that cannot collect on the row's clock is the opposite failure",
			removed, tree.StaleAge)
	}
	if _, serr := os.Stat(past); !os.IsNotExist(serr) {
		t.Errorf("the entry %s aged past the row's window survived (err=%v)", filepath.Base(past), serr)
	}
	for _, dir := range aged[1:] {
		if !strings.Contains(dir, BudgetRunTestedBinSuffix) {
			t.Fatalf("fixture drift: %q is not the writer's shape", dir)
		}
		if _, serr := os.Stat(dir); serr != nil {
			t.Errorf("the entry %s is still inside the row's window and must have survived: %v", filepath.Base(dir), serr)
		}
	}
}
