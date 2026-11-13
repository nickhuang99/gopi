package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

// time72h is the fixture's registration age: comfortably past every age floor
// the sweep applies, named so the value is not a bare literal in three tests.
func time72h() time.Duration { return 72 * time.Hour }

// deviceIDForTest measures a fixture path's device id (the same measurement the
// sweep's derivation uses — Rule 22: measure, never spell).
func deviceIDForTest(path string) (uint64, error) { return scratch.DeviceID(path) }

// contextBackground is the fixture's context, named for readability at the
// call site.
func contextBackground() context.Context { return context.Background() }

// The memory base is a WORLD-WRITABLE tmpfs, so these tests never write into the
// real one: they MEASURE whether the host actually runs its scratch home on a
// memory filesystem, and skip (never fake) when it does not. The epic's
// acceptance fixtures are the three dispositions a memory-base registration can
// get, and every one of them is asserted here twice — once on the DERIVED root
// (pure, no host dependency) and once, where the host permits, through the tool
// itself.

// memoryBaseFixtureRoot builds a tmpfs-backed directory to stand in for the
// memory base, or skips when this host has no writable memory filesystem. It is
// the smallest sample that can carry the fixture: a directory whose PARENT is
// the mount point (so the ancestor walk has a base above the child).
func memoryBaseFixtureRoot(t *testing.T) string {
	t.Helper()
	shm := scratchMemoryFixtureBase(t)
	root, err := os.MkdirTemp(shm, "gopi-membase-fixture-")
	if err != nil {
		t.Skipf("no writable memory fixture available: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

// scratchMemoryFixtureBase returns a writable memory-filesystem mount point on
// this host, or skips. It MEASURES (Rule 22): a host whose scratch home is
// disk-backed has no memory base to test against, and a test that invented one
// would be asserting against a spelling rather than a filesystem.
//
// The standard memory filesystem is named through the one definition
// (worktreeroots.ShmDir) rather than re-typed: the fixture's point is whether
// that filesystem is USABLE here, which the constant's value answers exactly —
// nothing about the test changes if the standard path is ever redefined, and
// that is what makes the reference correct rather than merely compliant.
func scratchMemoryFixtureBase(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{worktreeroots.ShmDir, os.TempDir()} {
		if _, ok := memoryFilesystemKind(candidate); !ok {
			continue
		}
		if err := os.MkdirAll(candidate, 0o777); err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(candidate, ".gopi-membase-probe"), nil, 0o600); err != nil {
			continue
		}
		os.Remove(filepath.Join(candidate, ".gopi-membase-probe"))
		return candidate
	}
	t.Skip("no writable memory filesystem on this host")
	return ""
}

// memoryBaseFixtureRoots returns a root list whose memory base is the fixture
// directory — the DERIVED root a scratch home inside that base would produce.
func memoryBaseFixtureRoots(base string) []worktreeroots.Root {
	return []worktreeroots.Root{{
		Path:   base,
		Role:   worktreeroots.RoleMemoryBase,
		Source: worktreeroots.SourceMemoryBaseMeasured,
	}}
}

// TestMemoryBaseAncestorFindsTheMountPoint: the walk climbs from a deep
// directory to the mount point and stops there — it never returns `/`.
func TestMemoryBaseAncestorFindsTheMountPoint(t *testing.T) {
	base := scratchMemoryFixtureBase(t)
	device, err := deviceIDForTest(base)
	if err != nil {
		t.Skipf("device id unmeasurable: %v", err)
	}
	deep := filepath.Join(base, "gopi-verify-fixture", "nested", "deeper")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Skipf("cannot create the fixture tree: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(base, "gopi-verify-fixture")) })

	got, ok := memoryBaseAncestor(deep, device)
	if !ok {
		t.Fatalf("the ancestor walk found no base above %s", deep)
	}
	if got != base {
		t.Fatalf("base = %q, want the mount point %q", got, base)
	}
	if got == memoryBaseDeviceProbePath {
		t.Fatalf("the walk returned %q — a root of / claims everything", got)
	}
}

// TestMemoryBaseDerivedRootIsMeasuredNotSpelled: the root the sweep derives is
// the filesystem the scratch home resolves onto; a scratch home on `/` yields NO
// root at all (the shape where this root must not exist).
func TestMemoryBaseDerivedRootIsMeasuredNotSpelled(t *testing.T) {
	base := scratchMemoryFixtureBase(t)
	t.Setenv(scratch.HomeEnvName, base)
	root, ok := memoryBaseRoot()
	if !ok {
		t.Skipf("scratch home %s yielded no measured memory base on this host", base)
	}
	if root.Role != worktreeroots.RoleMemoryBase {
		t.Fatalf("role = %q, want %q", root.Role, worktreeroots.RoleMemoryBase)
	}
	// The root is the tmpfs mount point the home resolves onto, and it is NEVER
	// the filesystem root.
	if root.Path == memoryBaseDeviceProbePath || root.Path == "" {
		t.Fatalf("derived root = %q, want a memory mount point", root.Path)
	}
	if _, isMemory := memoryFilesystemKind(root.Path); !isMemory {
		t.Fatalf("derived root %q is not a memory filesystem — the root would claim a directory nobody chose", root.Path)
	}
}

// TestMemoryBaseRootSkippedOnDiskBackedScratchHome: a scratch home on `/` (the
// default host shape) yields no implicit root, and the skip is EXPLAINED.
func TestMemoryBaseRootSkippedOnDiskBackedScratchHome(t *testing.T) {
	base := scratchMemoryFixtureBase(t)
	if base != os.TempDir() {
		// Only meaningful where the temp root itself is the memory filesystem:
		// otherwise os.TempDir() is on `/` and the check below asserts the
		// disk-backed branch directly.
		t.Skip("temp root is not the memory filesystem on this host")
	}
	// A directory on `/` (a plain temp dir on the root partition) must yield no
	// memory base: the sweep would otherwise claim a directory nobody chose.
	disk := t.TempDir()
	t.Setenv(scratch.HomeEnvName, disk)
	if root, ok := memoryBaseRoot(); ok {
		t.Fatalf("disk-backed scratch home %s yielded root %+v, want none", disk, root)
	}
	if reason := memoryBaseRootSkipped(disk); reason == "" {
		t.Fatal("no reason reported for a skipped memory base — the operator learns nothing")
	}
}

// TestMemoryBaseRegistrationIsGoverned: the epic's core disposition — a
// verification worktree directly in the memory base is INSIDE policy, so the
// root-scope rung no longer reports it "outside every declared root".
func TestMemoryBaseRegistrationIsGoverned(t *testing.T) {
	base := memoryBaseFixtureRoot(t)
	roots := memoryBaseFixtureRoots(base)
	reg := worktreeRegistration{
		Repo:   "/fixture/repo",
		Path:   filepath.Join(base, "gopi-verify-abc123-wt"),
		Branch: "fix/verify-abc123",
	}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, ho)
	if f.Rule == string(worktreeRuleRootScope) {
		t.Fatalf("a registration under the memory base is still out of policy: %s", f.Reason)
	}
	if f.Disposition != string(worktreeReclaim) {
		t.Fatalf("disposition = %s (%s), want reclaim", f.Disposition, f.Reason)
	}
	if f.Root == "" {
		t.Fatal("the finding names no root — the plan is not actionable")
	}
}

// TestMemoryBaseHeldRegistrationIsKept: the holder guard survives the new root —
// a registration inside the memory base that a live process holds is KEPT, with
// the holder arm named.
func TestMemoryBaseHeldRegistrationIsKept(t *testing.T) {
	base := memoryBaseFixtureRoot(t)
	roots := memoryBaseFixtureRoots(base)
	reg := worktreeRegistration{
		Repo:   "/fixture/repo",
		Path:   filepath.Join(base, "gopi-verify-held-wt"),
		Branch: "fix/verify-held",
	}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-time72h())}

	holders := scratch.HolderScan{Paths: []string{filepath.Join(reg.Path, "sub")}}
	f := worktreeSweepJudge(reg, nil, holders, roots, opts, ho)
	if f.Disposition != string(worktreeKeep) {
		t.Fatalf("disposition = %s (%s), want keep", f.Disposition, f.Reason)
	}
	if f.Rule != string(worktreeRuleLiveHolder) {
		t.Fatalf("rule = %s, want %s (the holder arm must decide, not the root arm)", f.Rule, worktreeRuleLiveHolder)
	}
}

// TestMemoryBaseForeignDirectoryIsNeverTouched: the blast-radius constraint. A
// foreign directory in the world-writable base is inside the ROOT, and the sweep
// judges git REGISTRATIONS only — so it can never be a candidate, and the
// fixture proves the directory survives a full apply pass.
func TestMemoryBaseForeignDirectoryIsNeverTouched(t *testing.T) {
	base := memoryBaseFixtureRoot(t)
	foreign := filepath.Join(base, "someones-unrelated-directory")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(foreign) })
	if err := os.WriteFile(filepath.Join(foreign, "keep.txt"), []byte("not ours"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The foreign directory is not a git registration, so it never reaches the
	// judge: the sweep's candidate list is `git worktree list` per repo. Prove
	// BOTH halves — it is inside the new root (so the root is real), and an
	// apply pass over a repo that knows nothing about it leaves it untouched.
	if !worktreeroots.Within(foreign, base) {
		t.Fatalf("fixture is wrong: %s is not inside the memory base %s", foreign, base)
	}
	repo := sweepInitRepo(t)
	res := worktreeSweepRunWith(contextBackground(), sweepOptionsNow(false), worktreeSweepDeps{
		Repos:      []string{repo},
		LedgerRead: true,
	}, nil)
	// Read the report, not only the filesystem: a candidate list that ever grew
	// beyond `git worktree list` would show the foreign path HERE first.
	for _, f := range res.Findings {
		if f.Path == foreign {
			t.Fatalf("the foreign directory entered the candidate list as a registration: %+v", f)
		}
	}
	if _, err := os.Stat(filepath.Join(foreign, "keep.txt")); err != nil {
		t.Fatalf("the foreign directory was touched by the sweep: %v (%+v)", err, res.Findings)
	}
}

// TestMemoryBaseKeepArmsOverrideTheNewRoot is the guard-ladder table for the
// memory base: being inside the derived root is necessary but never sufficient.
// Each row pairs a fixture with the arm that must decide it, so the new root can
// never become a byway around the holder, dirtiness or age guards.
func TestMemoryBaseKeepArmsOverrideTheNewRoot(t *testing.T) {
	base := memoryBaseFixtureRoot(t)
	roots := memoryBaseFixtureRoots(base)
	path := filepath.Join(base, "gopi-verify-guarded")
	for _, tc := range []struct {
		name string
		rows []tasksRowForSweep
		hold scratch.HolderScan
		ho   func(now time.Time) worktreeSweepHostObservations
		want string
		rule worktreeSweepRule
	}{
		{
			name: "unheld clean stale ⇒ reclaimed through the new root",
			ho: func(now time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{CreatedAt: now.Add(-time72h())}
			},
			want: string(worktreeReclaim),
			rule: worktreeRuleUnclaimed,
		},
		{
			name: "held by a live process ⇒ kept",
			hold: scratch.HolderScan{Paths: []string{filepath.Join(path, "pkg")}},
			ho: func(now time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{CreatedAt: now.Add(-time72h())}
			},
			want: string(worktreeKeep),
			rule: worktreeRuleLiveHolder,
		},
		{
			name: "dirty, unowned and past the age floor ⇒ escalated to a human (uncommitted work is never destroyed)",
			ho: func(now time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{CreatedAt: now.Add(-time72h()), DirtyReason: "?? pkg/new.go"}
			},
			want: string(worktreeEscalateOrphan),
			rule: worktreeRuleOrphan,
		},
		{
			name: "younger than the threshold ⇒ kept",
			ho: func(now time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{CreatedAt: now.Add(-time.Hour)}
			},
			want: string(worktreeKeep),
			rule: worktreeRuleTooYoung,
		},
		{
			name: "unmeasurable age ⇒ kept (cannot tell is never authorise)",
			ho:   func(now time.Time) worktreeSweepHostObservations { return worktreeSweepHostObservations{} },
			want: string(worktreeKeep),
			rule: worktreeRuleUnmeasurable,
		},
		{
			name: "live ledger row holding ground ⇒ kept",
			rows: []tasksRowForSweep{{TaskID: "T-live", Status: taskStatusInProgress, Worktree: path, Branch: "fix/verify-guarded"}},
			ho: func(now time.Time) worktreeSweepHostObservations {
				return worktreeSweepHostObservations{CreatedAt: now.Add(-time72h())}
			},
			want: string(worktreeKeep),
			rule: worktreeRuleLedgerOwnership,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := sweepOptionsNow(true)
			reg := worktreeRegistration{Repo: "/fixture/repo", Path: path, Branch: "fix/verify-guarded"}
			f := worktreeSweepJudge(reg, tc.rows, tc.hold, roots, opts, tc.ho(opts.Now))
			if f.Disposition != tc.want {
				t.Fatalf("disposition = %s (%s), want %s", f.Disposition, f.Reason, tc.want)
			}
			if tc.rule != "" && f.Rule != string(tc.rule) {
				t.Fatalf("rule = %s, want %s (%s)", f.Rule, tc.rule, f.Reason)
			}
			if f.Root == "" {
				t.Fatal("the finding names no root — the plan is not actionable")
			}
			if !strings.Contains(f.Root, string(worktreeroots.RoleMemoryBase)) {
				t.Fatalf("root = %q, want the memory-base role named", f.Root)
			}
		})
	}
}

// TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy: the DRY RUN's dry half —
// a registration genuinely outside every root must still be REPORTED with the
// rule that refused it, and that reason must name the memory-base remedy (so an
// operator reading the plan learns how to bring it into policy).
func TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy(t *testing.T) {
	// The consulted root this arm must NAME is the memory base the SCRATCH HOME
	// resolves onto (worktreeSweepConsultedRoot) — so the assertion below is
	// only satisfiable while that home resolves onto a memory filesystem. A
	// scratch home on a disk-backed filesystem resolves onto NO root at all,
	// and the producer then legitimately reports the empty/fallback label and
	// shortens the reason to its "no memory-base root is available here" arm
	// (Rule 15: it refuses to fabricate a root for a filesystem that is not a
	// memory base). That host state is therefore part of the fixture, never an
	// ambient accident: pin the scratch home onto a MEASURED memory
	// filesystem (Rule 22) so this test asserts the knowable arm on every
	// host instead of only on the developer's own /dev/shm.
	base := memoryBaseFixtureRoot(t)
	t.Setenv(scratch.HomeEnvName, base)
	// The registration is genuinely OUTSIDE every declared root — it sits in
	// the memory base itself, while the only declared root is elsewhere — which
	// is exactly the case whose remedy is "make the scratch home resolve onto
	// the memory-base root".
	outsider := filepath.Join(base, "gopi-verify-outside")
	reg := worktreeRegistration{Repo: "/fixture/repo", Path: outsider, Branch: "fix/verify-outside"}
	roots := []worktreeroots.Root{{Path: filepath.Join(t.TempDir(), "declared"), Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-time72h())}

	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReport) {
		t.Fatalf("disposition = %s (%s), want report", f.Disposition, f.Reason)
	}
	if f.Rule != string(worktreeRuleRootScope) {
		t.Fatalf("rule = %s, want %s", f.Rule, worktreeRuleRootScope)
	}
	if !strings.Contains(f.Reason, "memory-base") {
		t.Fatalf("the refusal does not name the memory-base remedy: %s", f.Reason)
	}
	// The field must not merely be NON-EMPTY: the no-root spelling is a valid
	// answer for the arms decided BEFORE any root is resolved, and it is
	// precisely the wrong answer here — this arm reached the root test, so it
	// consulted a real root and the plan must let the operator READ it (the
	// honest fallback label satisfies a bare `!= ""` check while telling the
	// operator nothing: the WIP defect's silent half, reintroduced through the
	// fallback).
	if f.Root == worktreeSweepNoRootLabel {
		t.Fatalf("the refusal reports the no-root label %q — this arm DID resolve roots; the plan must name what was consulted", f.Root)
	}
	if !strings.Contains(f.Root, " [") {
		t.Fatalf("root = %q, want a path and its bracketed provenance (a role alone is not actionable)", f.Root)
	}
}
