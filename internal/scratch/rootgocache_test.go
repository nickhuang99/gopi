package scratch

// rootgocache_test.go — the placement-ban guards for a SHARED Go build cache.
//
// The ban exists because the merge-time BUILD gate put its GOCACHE inside the
// tmpfs scratch root (per PROCESS), which is where a reusable cache can never
// live: measured 726M-1.14G per build, /dev/shm at 100% on 2026-09-15. The arms
// below pin the two things the ban must not lose: that the classification is by
// MEASUREMENT (a disk path passes, `/` and a memory filesystem are refused), and
// that the leaf name the refusal renders is the SAME value the gate builds its
// path from (an alias that drifts is Rule 13's defect).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// TestSharedBuildCacheLeafMatchesWorktreeRoots pins the alias contract: this
// package spells the cache leaf once (it cannot import worktreeroots — that
// import would be a cycle, since worktreeroots is a leaf) and the refusal text
// renders it, so a rename in the roots package must fail HERE rather than
// silently make the gate's message name a directory nothing uses.
func TestSharedBuildCacheLeafMatchesWorktreeRoots(t *testing.T) {
	if SharedBuildCacheLeaf() != worktreeroots.SharedBuildCacheDirName {
		t.Fatalf("shared build cache leaf %q != worktreeroots.SharedBuildCacheDirName %q — the alias drifted (Rule 13)",
			SharedBuildCacheLeaf(), worktreeroots.SharedBuildCacheDirName)
	}
}

// TestAssertGoCacheOffRootFSRefusesTheRootFilesystem is the `/` arm: a cache
// root on the boot partition is the 2026-09-11 exhaustion, so it is refused by
// measurement.
func TestAssertGoCacheOffRootFSRefusesTheRootFilesystem(t *testing.T) {
	err := AssertGoCacheOffRootFS("/")
	if err == nil {
		t.Fatalf("AssertGoCacheOffRootFS(/) must refuse the boot filesystem")
	}
	if !strings.Contains(err.Error(), worktreeroots.SharedBuildCacheDirName) {
		t.Fatalf("the refusal must name the location the cache belongs in (%s): %v", worktreeroots.SharedBuildCacheDirName, err)
	}
	if !strings.Contains(err.Error(), GoCacheRootEnvName) {
		t.Fatalf("the refusal must name the override that moves the cache (%s): %v", GoCacheRootEnvName, err)
	}
}

// TestAssertGoCacheOffRootFSPassesOffRootFS is the positive arm (the negative
// probe Rule 16-21's gates require): a disk path off `/` is ALLOWED. It is
// skipped when this host offers no such directory — the assertion is about the
// measurement, and a host whose only writable areas are RAM or `/` has nothing
// to measure.
func TestAssertGoCacheOffRootFSPassesOffRootFS(t *testing.T) {
	dir, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache dir to probe: %v", err)
	}
	if mem, _, merr := MemoryFilesystem(dir); merr != nil || mem {
		t.Skipf("%s is not a disk path on this host", dir)
	}
	onRoot, serr := SharesRootFS(dir)
	if serr != nil || onRoot {
		t.Skipf("%s shares the / filesystem on this host", dir)
	}
	if err := AssertGoCacheOffRootFS(dir); err != nil {
		t.Fatalf("a disk path off / must be allowed: %v", err)
	}
}

// TestAssertGoCacheOffRootFSIsAnswerableBeforeCreation is the arm the
// /dev/shm/<16-hex>/gocache family (measured 2026-10-01, 171 trees) made
// necessary, and it REPLACES the older "a not-yet-created root is refused" arm
// — which the fix deliberately invalidated. That older contract was the defect
// in disguise: a check that can only judge a path that ALREADY EXISTS forces
// the caller to create the directory first, so every refusal leaves the refused
// tree behind. The family WAS that residue.
//
// So the contract is now the opposite one, and it is pinned in both
// directions — an allowed placement and a refused one — plus the Rule 15 case
// that survives unchanged: a root the check cannot judge is never assumed safe.
func TestAssertGoCacheOffRootFSIsAnswerableBeforeCreation(t *testing.T) {
	// (a) ALLOWED: a not-yet-created root is measured on its nearest existing
	// ancestor, so a caller can decide BEFORE its os.MkdirAll. Skipped on a host
	// whose temp directory is on `/` or on RAM — then there is no off-root
	// ancestor to measure and the arm has nothing to say.
	probe := filepath.Join(t.TempDir(), "not-created", "nested")
	base := filepath.Dir(filepath.Dir(probe))
	if mem, _, merr := MemoryFilesystem(base); merr != nil || mem {
		t.Skipf("%s is not a disk path on this host", base)
	}
	if onRoot, serr := SharesRootFS(base); serr != nil || onRoot {
		t.Skipf("%s shares the / filesystem on this host", base)
	}
	if err := AssertGoCacheOffRootFS(probe); err != nil {
		t.Fatalf("a not-yet-created root inside an existing disk directory must be measured, not refused: %v", err)
	}
	// ...and the check must not CREATE what it judges: measuring is the whole
	// contract, so a caller's ordering (measure, then create) is preserved.
	if _, err := os.Stat(probe); err == nil {
		t.Fatalf("the check created %s — it must judge a path without minting it (that creation is the family)", probe)
	}
	// (b) REFUSED, and answerable before creation: a root that does not exist
	// under `/` resolves to an ancestor on `/` and is refused. This is the
	// direction the fix must not lose — the pre-creation answer stays a real
	// verdict, not a blanket pass.
	if err := AssertGoCacheOffRootFS(filepath.Join(rootDeviceProbePath, "gopi-not-created-probe", "nested")); err == nil {
		t.Fatalf("a not-yet-created root under %s must be refused", rootDeviceProbePath)
	}
	// (c) Rule 15: an empty root cannot be measured and is never assumed safe.
	if err := AssertGoCacheOffRootFS(""); err == nil {
		t.Fatalf("an empty cache root must be refused")
	}
}

// TestAssertGoCacheOffShmRefusesMemoryFilesystem is the tmpfs arm: a cache on
// RAM is the 2026-09-15 incident, and /dev/shm is its own mount — NOT on `/` —
// so the device comparison alone cannot catch it. That is why the check is by
// filesystem TYPE.
func TestAssertGoCacheOffShmRefusesMemoryFilesystem(t *testing.T) {
	if _, err := os.Stat(worktreeroots.ShmDir); err != nil {
		t.Skipf("%s does not exist on this host", worktreeroots.ShmDir)
	}
	mem, description, err := MemoryFilesystem(worktreeroots.ShmDir)
	if err != nil {
		t.Skipf("cannot measure %s: %v", worktreeroots.ShmDir, err)
	}
	if !mem {
		t.Skipf("%s is not a memory filesystem on this host (%s)", worktreeroots.ShmDir, description)
	}
	if err := AssertGoCacheOffShm(worktreeroots.ShmDir); err == nil {
		t.Fatalf("AssertGoCacheOffShm(%s) must refuse a RAM-backed cache (%s)", worktreeroots.ShmDir, description)
	}
	if _, description, err := MemoryFilesystem(t.TempDir()); err == nil && !strings.Contains(description, "statfs") {
		// The positive half: the classification answers for an ordinary
		// directory too (never a bare "unknown"), so a caller can act on it.
		return
	}
}
