// sessions_scratch_dir_live_container_test.go — the KEEP that the candidate-only
// holder veto could not see: a directory inside a LIVE run's container.
//
// THE DEFECT THIS PINS (measured 2026-10-16, this host, task
// 01a142630844745eb09a754876bcec9a). The session-raw directory arm asked
// `heldIn(path, index)` — "is THE CANDIDATE held" — and a live run does not stand
// in the directory it is about to lose: it stands in the CONTAINER (`cd $S`, the
// shape every evidence-writing bash call uses) or it holds a FILE in the
// container (the run's own `output.log`). Measured against the real sweep on a
// sandbox profile with the holder alive, `<container>/artifacts` was listed as
// `would-remove` while the process whose cwd was the container ran; the same
// fixture with the holder dead listed it too, which is exactly why the control
// below is two-sided — "the arm listed it" is not "the arm ignored the holder"
// unless the identical fixture behaves the SAME with no holder, and the identical
// fixture WITH the holder does not.
//
// WHY THE HOLDER IS INJECTED RATHER THAN SPAWNED. `heldTreeScan` is the caller's
// ONE indexed /proc pass (the walk makes it: `heldSet(ScanHolders(), root)`), and
// the index's shape is the whole subject here — "the holder's path and every
// ancestor of it, up to the tree root". Injecting the path a live process would
// report (`/proc/<pid>/cwd` readlink) exercises the SAME structure the real pass
// produces, with no second process to schedule, no sleep to tune and no flake:
// the arm under test is the one that reads the index, and it is the real one
// (`containerScratchDirEntries`, driven through the sweep).
//
// THE THREE CASES, and each is a KEEP or a TAKE for a stated reason:
//
//  1. holder cwd == the CONTAINER           → KEEP the directory inside it
//  2. holder fd  == a FILE in the CONTAINER → KEEP the directory inside it
//  3. no holder at all (the CONTROL)        → TAKE it (the arm still works)
//
// Case 3 is what makes 1 and 2 mean anything: without it, "nothing was collected"
// is satisfied by an arm that never collects anything.
package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// scratchDirLiveFixtureFile is the name of the file a run keeps in its own
// container and holds open while it works — the second holder shape (the run
// writing `output.log` beside the directory it is filling).
const scratchDirLiveFixtureFile = "output.log"

// TestScratchDirKeepsADirectoryInsideALiveContainer is the regression guard for
// the container-level veto: a directory inside a container that a live process
// stands in (or holds a file in) must NOT be a candidate, at any age — and the
// same fixture with no holder must still be one.
func TestScratchDirKeepsADirectoryInsideALiveContainer(t *testing.T) {
	root, tree := scratchDirFixtureRoot(t)
	now := time.Now()

	// Three containers the ledger has FORGOTTEN (no metadata.json, no *.jsonl), so
	// the age rule alone would collect every directory in them: the ONLY thing
	// that can save case 1 and case 2 is the holder, and the only thing that makes
	// case 3 collectable is that nothing holds it.
	heldContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan))
	heldDir, _ := seedScratchDir(t, filepath.Join(heldContainer, scratchDirFixtureDeadName))

	fdContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan+"-fd"))
	fdDir, _ := seedScratchDir(t, filepath.Join(fdContainer, scratchDirFixtureDeadName))
	writeContainerFile(t, fdContainer, scratchDirLiveFixtureFile)

	controlContainer := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan+"-control"))
	controlDir, _ := seedScratchDir(t, filepath.Join(controlContainer, scratchDirFixtureDeadName))

	// AGE LAST: creating a child moves its parent's mtime, so a container aged
	// before it was seeded reads as freshly touched (the trap the fixture's own
	// comment records).
	for _, p := range []string{heldContainer, heldDir, fdContainer, fdDir, controlContainer, controlDir} {
		agePath(t, p, scratchDirFixtureAge)
	}

	// The two holder shapes, as the ONE /proc pass would index them: a process
	// whose cwd IS the container, and a process holding a FILE inside it.
	cwdHeld := heldTreeScan{index: heldSet(HolderScan{Paths: []string{heldContainer}}, root)}
	fdHeld := heldTreeScan{index: heldSet(HolderScan{Paths: []string{filepath.Join(fdContainer, scratchDirLiveFixtureFile)}}, root)}
	noneHeld := heldTreeScan{index: heldSet(HolderScan{}, root)}

	if cand := containerScratchDirEntries(tree, heldDir, heldContainer, now, scratchDirFixtureWindow, cwdHeld); len(cand) != 0 {
		t.Errorf("a live process's cwd is the CONTAINER %s and its directory %s was still planned for collection (%+v): the veto asked about the candidate alone, so the ordinary `cd $S; …` shape loses the run's own evidence underneath it", heldContainer, heldDir, cand)
	}
	if cand := containerScratchDirEntries(tree, fdDir, fdContainer, now, scratchDirFixtureWindow, fdHeld); len(cand) != 0 {
		t.Errorf("a live process holds the FILE %s inside the container and its directory %s was still planned for collection (%+v): a run writing its own %s beside the directory it fills is the same live run, and its directory must survive too", filepath.Join(fdContainer, scratchDirLiveFixtureFile), fdDir, cand, scratchDirLiveFixtureFile)
	}
	if cand := containerScratchDirEntries(tree, controlDir, controlContainer, now, scratchDirFixtureWindow, noneHeld); len(cand) != 1 {
		t.Fatalf("the CONTROL — the same fixture with NO holder — was refused (%+v): without this case the two assertions above are satisfied by an arm that collects nothing at all, which is the whole reason the control exists", cand)
	}

	// The plan and the sweep are the SAME predicate — and the walk derives its OWN
	// holder pass (heldSet(ScanHolders(), root) inside declaredTreePlan), so the
	// synthetic index above cannot be carried into it. What CAN be driven with a
	// controlled holder pass is the walk's own container function, which is the
	// code path the plan and the sweep both go through: containerEntries.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	byName := map[string]os.DirEntry{}
	for _, e := range entries {
		byName[e.Name()] = e
	}
	walkCases := []struct {
		container, dir string
		held           heldTreeScan
		wantDir        bool
		why            string
	}{
		{heldContainer, heldDir, cwdHeld, false, "a process whose cwd is the container"},
		{fdContainer, fdDir, fdHeld, false, "a process holding a file in the container"},
		{controlContainer, controlDir, noneHeld, true, "the no-holder control"},
	}
	for _, c := range walkCases {
		entry, ok := byName[filepath.Base(c.container)]
		if !ok {
			t.Fatalf("container %s is not a directory entry of %s: the walk would never judge it", c.container, root)
		}
		planned := map[string]bool{}
		for _, e := range containerEntries(tree, c.container, entry, now, scratchDirFixtureWindow, c.held) {
			planned[e.Path] = true
		}
		if planned[c.dir] != c.wantDir {
			t.Errorf("the walk planned %s=%v for %s (%s): want %v — the walk's answer and the hook's must be one answer", c.dir, planned[c.dir], c.container, c.why, c.wantDir)
		}
	}
}

// TestScratchDirLiveContainerFixtureControlIsReal asserts the fixture itself:
// the control container's directory reaches the window the arm judges by, so
// the two held cases are refused by the HOLDER and not by being young.
func TestScratchDirLiveContainerFixtureControlIsReal(t *testing.T) {
	root, tree := scratchDirFixtureRoot(t)
	control := seedContainer(t, filepath.Join(root, scratchDirFixtureOrphan+"-age"))
	dir, _ := seedScratchDir(t, filepath.Join(control, scratchDirFixtureDeadName))
	agePath(t, control, scratchDirFixtureAge)
	agePath(t, dir, scratchDirFixtureAge)
	window := scratchDirWindow(scratchDirFixtureWindow)
	stale, err := tree.entryStale(dir, time.Now(), window)
	if err != nil || !stale {
		t.Fatalf("the fixture's directory is not past the arm's own window (%s) — the cases above would pass on age, not on the holder (stale=%v err=%v)", window, stale, err)
	}
	if _, err := os.Lstat(dir); err != nil {
		t.Fatalf("fixture directory %s: %v", dir, err)
	}
}
