package main

// scratch_gc_tick_declared_trees_test.go — THE REGRESSION GUARD for the defect
// this run measured: the resident scratch sweep never called the DECLARED-TREES
// sweep, so the trees `gopi tool scratch-gc trees` exists to bound were bounded
// by nothing on a host that keeps a worker or a merger alive and never runs the
// verb — and that is this host, where `scripts/scratch-gc-watchdog.sh` is not
// installed at all (measured 2026-10-17: no systemd user timer for it, no
// crontab line, no copy under ~/.local/bin or ~/bin).
//
// THE FAILURE THIS PINS, MEASURED (host-b, 2026-10-17 06:4x), one device
// (66309 = /home), `du -xsh`:
//
//	~/.cache/go-build              78 G   (agent-side toolchain cache — out of scope by an existing decision)
//	~/workspace/gopi-build-cache   81 G   (DECLARED tree, 45.7 GB of it age-and-holder vetted collectable)
//	~/.gopi/sessions               49 G   (DECLARED, 5.4 GB of aged *.raw.log collectable)
//	~/workspace/_tmp               11 G   (DECLARED go-tmp, 1.27 GB abandoned go-build*)
//	~/workspace/gopi-test-scratch  5.8 G  (the HOME arm — the one arm that did run)
//
// `gopi tool scratch-gc trees --dry-run` reported every one of those as
// `collectable`, in the same hour, and a `cmd/agent` suite died with
// `设备上没有空间` (ENOSPC) on a `git worktree add` regardless — because the only
// face that collects a declared tree is a command nobody runs.
//
// THE ASSERTION IS AIMED AT THE WIRING, NOT AT THE RECLAMATION, and the
// distinction was MEASURED, not reasoned (2026-10-17). The first version of this
// test drove a bare `scratchGCTick` and asserted the fixture entry was gone; it
// came back GREEN on the mutated tree, because ANOTHER arm of the same tick (the
// home arm — every declared-tree root resolves inside the suite's scratch root by
// design, so the home sweep walks the same sandbox) removed the fixture for its
// own reasons. A test whose green survives the removal of the code it names is
// not a test of that code.
//
// So this file reads the arm's own answer through the package's existing seam
// shape: `declaredTreesSweepBody` (a package-level var, exactly like
// `scratchGCSweepBody`) is spied on, and the test asserts BOTH that the tick
// reached it and what it returned. On the pre-fix tick the spy is never called —
// the red proof recorded in the report.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// pinDeclaredTreeFixture points every declared-tree resolver this test can reach
// into a sandbox, so nothing it does can touch a tree the operator's host really
// holds. The pins are the same ones TestScratchGCTick_ThrottleAndFailOpen uses
// for its own arms, for the same reason: an unrelated residue on the operator's
// host must not be counted into this assertion.
//
// It returns the sandbox HOME and the go-tmp root, and installs the spy.
func pinDeclaredTreeFixture(t *testing.T) (home string, goTmp string, calls *int) {
	t.Helper()
	home = t.TempDir()
	goTmp = t.TempDir()
	// go-tmp's rank 1 is the environment; pinning it puts every rank (env, the
	// go env file, the process temp root) inside the fixture.
	t.Setenv(scratch.GoTmpDirEnvName, goTmp)
	// The scratch HOME — what the home arm, the queue/dumps rows and the
	// test-budget row all hang off.
	t.Setenv(scratch.HomeEnvName, home)
	t.Setenv(scratch.GCIntervalEnvName, "10")
	t.Setenv(scratch.ResolveBaseEnvName, t.TempDir())
	t.Setenv(config.SandboxEnvName, t.TempDir())
	scratchGCLastTick = time.Time{} // isolate the process-wide cadence clock

	prev := declaredTreesSweepBody
	t.Cleanup(func() { declaredTreesSweepBody = prev })
	n := 0
	calls = &n
	declaredTreesSweepBody = func() []scratch.DeclaredTreeSweepResult {
		n++
		return prev()
	}
	return home, goTmp, calls
}

// TestResidentTickReachesTheDeclaredTrees is the red-then-green proof of the
// WIRING: one real tick must call the declared-trees arm.
func TestResidentTickReachesTheDeclaredTrees(t *testing.T) {
	_, goTmp, calls := pinDeclaredTreeFixture(t)

	// ONE aged, unheld entry in the declared go-tmp tree — the exact shape the
	// toolchain abandons when a build is killed (`go-build*`), aged past its own
	// window (GoTmpStaleAge = 24h) by backdating, which is a filesystem write and
	// not load (Rule 27).
	abandoned := filepath.Join(goTmp, "go-build9999999")
	if err := os.MkdirAll(abandoned, scratch.DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", abandoned, err)
	}
	if err := os.WriteFile(filepath.Join(abandoned, "obj.bin"), make([]byte, 4096), scratch.FilePerm); err != nil {
		t.Fatalf("write fixture entry: %v", err)
	}
	old := time.Now().Add(-(scratch.GoTmpStaleAge + 2*time.Hour))
	if err := os.Chtimes(abandoned, old, old); err != nil {
		t.Fatalf("backdate %s: %v", abandoned, err)
	}

	// The tree must be VISIBLE to the declared set before the tick claims
	// anything: if the resolver does not point where the fixture wrote, a green
	// below would prove nothing and the red proof would be vacuous.
	root, err := scratch.ResolveGoTmpDir()
	if err != nil {
		t.Fatalf("the fixture's go-tmp tree does not resolve: %v", err)
	}
	if root != goTmp {
		t.Fatalf("the fixture's go-tmp root resolved to %q, want %q — the fixture is not pointing at the tree it planted the entry in", root, goTmp)
	}

	scratchGCTick(workerRoleLabel, time.Now())

	if *calls == 0 {
		t.Fatalf("the resident tick never called the declared-trees arm — this is the defect: the declared set (%s, %s, the shared build cache, the test-budget tree) is bounded by NOTHING on a host that runs no `gopi tool scratch-gc trees`, so an aged `go-build*` survives every tick while /home fills to 100%% and builds die with ENOSPC",
			scratch.TreeNameGoTmp, scratch.TreeNameSessionRaw)
	}
	if _, statErr := os.Stat(abandoned); !os.IsNotExist(statErr) {
		t.Fatalf("the aged, unheld entry %s survived a tick whose declared arm DID run (stat err = %v)", abandoned, statErr)
	}
	// THE ROOT SURVIVES, unconditionally — the invariant that protects
	// GOTMPDIR: a sweep that collected the tree's root would have deleted the
	// tree, not bounded it.
	if fi, statErr := os.Stat(goTmp); statErr != nil || !fi.IsDir() {
		t.Fatalf("the declared tree's ROOT %s did not survive the tick (stat err = %v) — sweeping must bound a tree, never remove it", goTmp, statErr)
	}
}

// TestResidentDeclaredTreeSweepKeepsAFreshEntry pins the OTHER half of the same
// predicate: the resident arm must not be a "delete everything in a declared
// tree" pass. An entry INSIDE its own window survives — which is what makes the
// automatic caller safe to run unattended.
func TestResidentDeclaredTreeSweepKeepsAFreshEntry(t *testing.T) {
	_, goTmp, calls := pinDeclaredTreeFixture(t)

	// A build that is running RIGHT NOW — the shape of a live toolchain.
	fresh := filepath.Join(goTmp, "go-build1111111")
	if err := os.MkdirAll(fresh, scratch.DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", fresh, err)
	}

	scratchGCTick(workerRoleLabel, time.Now())

	if *calls == 0 {
		t.Fatalf("the resident tick never called the declared-trees arm")
	}
	if fi, statErr := os.Stat(fresh); statErr != nil || !fi.IsDir() {
		t.Fatalf("a fresh entry in a declared tree was collected (stat err = %v) — the automatic arm must respect the tree's own age window", statErr)
	}
}
