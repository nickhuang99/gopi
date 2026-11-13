package scratch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// worktree_stub_test.go — the predicate's tests. They build the shapes by hand,
// because the input is what matters here: every shape below was either MEASURED
// on the operator's real tree (2026-10-01) or is a near miss that must survive.

const (
	// stubHex is the measured name body: a row id with its dashes removed.
	stubHex = "01a0e85bd6977df790e4a048b251610c"
	// suiteRunDir is the measured provenance: a per-run suite root under a memory
	// base, exactly the shape the suite entry point mints (suite-agent-run-<tail>).
	suiteRunDir = "suite-agent-run-2046292404"
)

// stubRoot builds a root containing ONE stub, and returns (root, stub path, the
// dead gitdir path the stub points at).
func stubRoot(t *testing.T) (root, stub, deadGitdir string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	return root, filepath.Join(root, WorktreeStubNamePrefix+stubHex),
		filepath.Join(base, "memory-base", suiteRunDir, "TestReframeChildRows6264886", "001", "reframe-main", ".git", "worktrees", WorktreeStubNamePrefix+stubHex)
}

// writeStub materialises the measured degenerate shape: a directory whose ONLY
// entry is a `.git` FILE naming a gitdir that does not exist.
func writeStub(t *testing.T, stub, gitdir string) {
	t.Helper()
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatalf("mkdir stub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stub, WorktreeStubGitPointerName),
		[]byte(WorktreeStubGitDirKey+gitdir+"\n"), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
}

// TestPlanWorktreeStubsCollectsTheMeasuredShape is the regression test for the
// RESIDUE: the exact shape found 174 times in the operator's ~/workspace must be
// judged collectable, with the missing gitdir named in the reason.
func TestPlanWorktreeStubsCollectsTheMeasuredShape(t *testing.T) {
	root, stub, gitdir := stubRoot(t)
	writeStub(t, stub, gitdir)

	plans, err := PlanWorktreeStubs(root)
	if err != nil {
		t.Fatalf("PlanWorktreeStubs: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(plans), plans)
	}
	if plans[0].Keep {
		t.Fatalf("the measured stub shape was KEPT: %s — the residue would never be reclaimed", plans[0].Reason)
	}
	if !strings.Contains(plans[0].Reason, gitdir) {
		t.Errorf("the reason must name the dead gitdir, got %q", plans[0].Reason)
	}
}

// TestPlanWorktreeStubsKeepsEveryNearMiss is the SAFETY half, and it is the half
// that matters: the root is a directory the operator shares with real worktrees,
// so each near miss below must survive. Every case asserts BOTH the verdict and
// that the reason names the condition that saved it (Rule 15: a keep explains
// itself).
func TestPlanWorktreeStubsKeepsEveryNearMiss(t *testing.T) {
	dead := func(t *testing.T, base string) string {
		return filepath.Join(base, "memory-base", suiteRunDir, "x", ".git", "worktrees", WorktreeStubNamePrefix+stubHex)
	}

	t.Run("live target", func(t *testing.T) {
		root, stub, _ := stubRoot(t)
		base := filepath.Dir(root)
		live := dead(t, base)
		if err := os.MkdirAll(live, 0o755); err != nil {
			t.Fatalf("mkdir live gitdir: %v", err)
		}
		writeStub(t, stub, live)
		assertKept(t, root, WorktreeStubKeepTargetLive)
	})

	t.Run("has a working tree", func(t *testing.T) {
		root, stub, gitdir := stubRoot(t)
		writeStub(t, stub, gitdir)
		// A SUBDIRECTORY is the shape a real checkout has and a fixture leftover
		// does not (the measured residue is one 8-byte README.md).
		if err := os.MkdirAll(filepath.Join(stub, "internal", "tools"), 0o755); err != nil {
			t.Fatalf("mkdir working tree: %v", err)
		}
		assertKept(t, root, WorktreeStubKeepHasContent)
	})

	t.Run("symlink beside the pointer", func(t *testing.T) {
		root, stub, gitdir := stubRoot(t)
		writeStub(t, stub, gitdir)
		if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(stub, "link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		assertKept(t, root, WorktreeStubKeepHasContent)
	})

	t.Run("too large to be a fixture leftover", func(t *testing.T) {
		root, stub, gitdir := stubRoot(t)
		writeStub(t, stub, gitdir)
		if err := os.WriteFile(filepath.Join(stub, "big.bin"),
			make([]byte, StubLeftoverMaxBytes+1), 0o644); err != nil {
			t.Fatalf("write big file: %v", err)
		}
		plans, err := PlanWorktreeStubs(root)
		if err != nil {
			t.Fatalf("PlanWorktreeStubs: %v", err)
		}
		if len(plans) != 1 || !plans[0].Keep || !strings.HasPrefix(plans[0].Reason, WorktreeStubKeepTooBig) {
			t.Fatalf("an oversized candidate was not kept for size: %+v", plans)
		}
	})

	t.Run("fixture README beside the pointer is collectable", func(t *testing.T) {
		root, stub, gitdir := stubRoot(t)
		writeStub(t, stub, gitdir)
		// The MEASURED shape: the pointer plus the fixture's one checked-out file.
		if err := os.WriteFile(filepath.Join(stub, "README.md"), []byte("fixture\n"), 0o644); err != nil {
			t.Fatalf("write README: %v", err)
		}
		plans, err := PlanWorktreeStubs(root)
		if err != nil {
			t.Fatalf("PlanWorktreeStubs: %v", err)
		}
		if len(plans) != 1 || plans[0].Keep {
			t.Fatalf("the measured leftover (pointer + README.md) was kept: %+v", plans)
		}
	})

	t.Run("is a repository", func(t *testing.T) {
		root, stub, _ := stubRoot(t)
		if err := os.MkdirAll(filepath.Join(stub, WorktreeStubGitPointerName), 0o755); err != nil {
			t.Fatalf("mkdir .git dir: %v", err)
		}
		assertKept(t, root, WorktreeStubKeepPointersDir)
	})

	t.Run("name is not the generated family", func(t *testing.T) {
		root, _, gitdir := stubRoot(t)
		// A hand-named ground-shaped directory: same shape, no generated stem.
		for _, name := range []string{"gopi-scratch", "gopi-1", "gopi-archive-ref-reclaim", "hermes-agent"} {
			writeStub(t, filepath.Join(root, name), gitdir)
		}
		plans, err := PlanWorktreeStubs(root)
		if err != nil {
			t.Fatalf("PlanWorktreeStubs: %v", err)
		}
		if len(plans) != 0 {
			t.Fatalf("a name outside the generated family was considered at all: %+v", plans)
		}
	})

	t.Run("gitdir outside a suite run", func(t *testing.T) {
		root, stub, _ := stubRoot(t)
		// The operator's own layout: a real path that happens to be gone, under no
		// suite run directory. Not this sweep's business.
		//
		// The target must sit OUTSIDE this run's scratch, because every t.TempDir()
		// path lives under `suite-<name>-run-<tail>` (the very provenance the
		// predicate looks for) — so the case is built from a home-level path that
		// is never created.
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no home directory to build the case from: %v", err)
		}
		writeStub(t, stub, filepath.Join(home, "workspace-elsewhere-never-created", ".git", "worktrees", "x"))
		assertKept(t, root, WorktreeStubKeepNotFromSuite)
	})

	t.Run("pointer does not name an absolute path", func(t *testing.T) {
		root, stub, _ := stubRoot(t)
		if err := os.MkdirAll(stub, 0o755); err != nil {
			t.Fatalf("mkdir stub: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stub, WorktreeStubGitPointerName),
			[]byte(WorktreeStubGitDirKey+"relative/path\n"), 0o644); err != nil {
			t.Fatalf("write pointer: %v", err)
		}
		assertKept(t, root, WorktreeStubKeepPointerShape)
	})

	t.Run("symlink", func(t *testing.T) {
		root, stub, gitdir := stubRoot(t)
		real := filepath.Join(filepath.Dir(root), "real-target")
		writeStub(t, real, gitdir)
		if err := os.Symlink(real, stub); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		assertKept(t, root, WorktreeStubKeepNotDir)
	})

	t.Run("held by a live process", func(t *testing.T) {
		root, stub, gitdir := stubRoot(t)
		writeStub(t, stub, gitdir)
		// String-level: the batch probe resolves a holder to the directories it
		// stands in, and an exact-path holder IS one — so the veto path is
		// exercised without depending on this test process's own cwd.
		if unheldBatch(stub, HolderScan{Paths: []string{stub}}) {
			t.Fatalf("the batch probe did not see an exact-path holder")
		}
		// And the real shape, end to end: the test process's OWN cwd, returned to
		// before the assertion so the suite is left where it was found. The plan is
		// taken WHILE the process stands in the stub — that is the measurement the
		// veto exists for.
		prev, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(stub); err != nil {
			t.Fatalf("chdir stub: %v", err)
		}
		held, who := worktreeStubHeld(stub, ScanHolders())
		heldPlans, planErr := PlanWorktreeStubs(root)
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("chdir back: %v", err)
		}
		if !held {
			t.Fatalf("a process standing in the stub was not detected as a holder")
		}
		if who == "" {
			t.Fatalf("the holder was detected but not named")
		}
		if planErr != nil {
			t.Fatalf("PlanWorktreeStubs while held: %v", planErr)
		}
		if len(heldPlans) != 1 || !heldPlans[0].Keep {
			t.Fatalf("a stub held by a live process was collectable: %+v", heldPlans)
		}
		// The reason names the veto AND its evidence (which probe saw the holder),
		// so the assertion pins the leading sentence rather than the whole line.
		if !strings.HasPrefix(heldPlans[0].Reason, WorktreeStubKeepHeld) {
			t.Fatalf("held stub kept for %q, want it to begin with %q", heldPlans[0].Reason, WorktreeStubKeepHeld)
		}
	})
}

// assertKept runs the plan over root and asserts the single candidate is kept for
// the stated reason.
func assertKept(t *testing.T, root, wantReason string) {
	t.Helper()
	plans, err := PlanWorktreeStubs(root)
	if err != nil {
		t.Fatalf("PlanWorktreeStubs: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(plans), plans)
	}
	if !plans[0].Keep {
		t.Fatalf("candidate was collectable, want KEEP (%s): %+v", wantReason, plans[0])
	}
	if plans[0].Reason != wantReason {
		t.Fatalf("reason = %q, want %q", plans[0].Reason, wantReason)
	}
}

// TestSweepWorktreeStubsRemovesExactlyThePlannedSet is the acceptance invariant,
// asserted in the smallest possible sample: the planned set IS the deleted set,
// and every near miss in the same directory survives the sweep.
func TestSweepWorktreeStubsRemovesExactlyThePlannedSet(t *testing.T) {
	root, stub, gitdir := stubRoot(t)
	writeStub(t, stub, gitdir)
	// Neighbours that must survive, in the SAME directory the sweep runs over.
	liveStub := filepath.Join(root, WorktreeStubNamePrefix+"deadbeefdeadbeefdeadbeefdeadbeef")
	writeStub(t, liveStub, filepath.Join(filepath.Dir(root), "still-here", ".git", "worktrees", "x"))
	if err := os.MkdirAll(filepath.Join(filepath.Dir(root), "still-here", ".git", "worktrees", "x"), 0o755); err != nil {
		t.Fatalf("mkdir live gitdir: %v", err)
	}
	notOurs := filepath.Join(root, "gopi-test-scratch")
	if err := os.MkdirAll(notOurs, 0o755); err != nil {
		t.Fatalf("mkdir neighbour: %v", err)
	}

	plans, err := PlanWorktreeStubs(root)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	wantRemove := 0
	for _, p := range plans {
		if !p.Keep {
			wantRemove++
		}
	}
	if wantRemove != 1 {
		t.Fatalf("planned %d removals, want 1: %+v", wantRemove, plans)
	}

	removed, _, err := SweepWorktreeStubs(root)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != wantRemove {
		t.Fatalf("removed %d, planned %d — the plan and the sweep disagree", removed, wantRemove)
	}
	if _, err := os.Lstat(stub); !os.IsNotExist(err) {
		t.Fatalf("the collectable stub survived the sweep (%v)", err)
	}
	for _, survivor := range []string{liveStub, notOurs} {
		if _, err := os.Lstat(survivor); err != nil {
			t.Fatalf("a kept neighbour was removed: %s (%v)", survivor, err)
		}
	}

	// A SECOND run finds nothing: the sweep is idempotent, which is what makes it
	// safe in a tick.
	removed, _, err = SweepWorktreeStubs(root)
	if err != nil || removed != 0 {
		t.Fatalf("second sweep: removed=%d err=%v, want 0/nil", removed, err)
	}
}

// TestPlanWorktreeStubsDoesNotCreateAMissingRoot pins the read-only contract: a
// plan must not be able to bring a tree into existence (the same rule the scratch
// dry run follows).
func TestPlanWorktreeStubsDoesNotCreateAMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-there")
	plans, err := PlanWorktreeStubs(missing)
	if err != nil {
		t.Fatalf("plan over a missing root: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("a missing root produced candidates: %+v", plans)
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatalf("the plan CREATED %s", missing)
	}
}

// TestWorktreeStubRootRefusalNamesEveryDangerousRoot is the guard's own test. The
// guard is the only thing between a mistyped `--root` and the operator's home, so
// every refusal is asserted BY NAME and each case also proves the guard does not
// over-refuse an ordinary root.
func TestWorktreeStubRootRefusalNamesEveryDangerousRoot(t *testing.T) {
	refused := []string{"/", "", "."}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		refused = append(refused, home, home+"/")
	}
	if profile := gopiProfileDir(); profile != "" {
		refused = append(refused, profile)
	}
	for _, root := range refused {
		if err := WorktreeStubRootRefusal(root); err == nil {
			t.Errorf("WorktreeStubRootRefusal(%q) allowed the sweep", root)
		}
	}

	// A git work tree is refused: its stale registrations belong to `git worktree
	// prune`, not to a directory sweep.
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, WorktreeStubGitPointerName),
		[]byte(WorktreeStubGitDirKey+filepath.Join(t.TempDir(), "x")+"\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	if err := WorktreeStubRootRefusal(worktree); err == nil {
		t.Errorf("WorktreeStubRootRefusal(%q) allowed a git work tree", worktree)
	}

	// And an ordinary worktree root passes — the guard refuses danger, not work.
	ordinary := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(ordinary, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := WorktreeStubRootRefusal(ordinary); err != nil {
		t.Errorf("WorktreeStubRootRefusal(%q) refused an ordinary root: %v", ordinary, err)
	}
}

// TestWorktreeStubNamePrefixAgreesWithTheGroundFamily pins the ONE value this
// leaf cannot import (internal/tools imports this package, so the direction is
// one-way). The tie is asserted here by the SHAPE the tools layer produces:
// `gopi-` + a row id with its dashes removed — the name every one of the 174
// measured stubs carried.
func TestWorktreeStubNamePrefixAgreesWithTheGroundFamily(t *testing.T) {
	rowID := "01a0e85b-d697-7df7-90e4-a048b251610c"
	ground := WorktreeStubNamePrefix + strings.ReplaceAll(rowID, "-", "")
	if !worktreeStubNameRe.MatchString(ground) {
		t.Fatalf("a ground named from a row id (%s) is not in the sweep's family — the predicate and the producer disagree", ground)
	}
	if WorktreeStubNamePrefix != "gopi-" {
		t.Fatalf("WorktreeStubNamePrefix = %q — the ground family's leading value changed", WorktreeStubNamePrefix)
	}
}
