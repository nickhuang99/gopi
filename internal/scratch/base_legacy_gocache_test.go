package scratch

// The LEGACY BARE-KEY build-cache family: `<base>/<16-hex>/gocache` — the trees
// a REFUSED build-gate placement used to leave in the memory base, which no
// sweep owned.
//
// THE DEFECT (measured 2026-10-01, this host): 171 such trees in /dev/shm, each
// holding a single empty `gocache`. `internal/tools`' own placement-REFUSAL arm
// pins the merge-time BUILD gate's cache root AT the memory base — it has to, to
// measure the "never a cache on RAM" ban — and the gate created
// `<root>/<repo-key>/gocache` before it measured the placement. The repo key is
// a digest of the repo's ABSOLUTE path, so a suite fixture (a fresh temp path per
// run) minted a fresh key per run.
//
// THE FIX HAS TWO HALVES AND BOTH ARE PINNED HERE:
//
//  1. the WRITER no longer creates what it will refuse (internal/tools: the
//     placement bans moved BEFORE the os.MkdirAll) — pinned red→green in
//     internal/tools/merge_build_gate_gocache_test.go;
//  2. the SWEEP owns the family, so the trees already on disk are collectable
//     through the EXISTING base sweep rather than through a new one (this file).
//
// Every fixture below is a plain directory under t.TempDir() — the test never
// reads or writes the operator's real memory base (Rule 6), and the age/holder
// halves are exercised through the SAME predicates the sweep uses.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	// legacyGoCacheFixtureKey is a repo-key-shaped name: exactly
	// BuildCacheKeyHexLen lowercase hex characters — the shape the gate's own
	// mergeBuildGateGoCacheRepoKey mints (the equality is pinned on the writer's
	// side by TestBuildCacheKeyHexLenMatchesWriter).
	legacyGoCacheFixtureKey = "8a28affa4f85ba27"

	// legacyGoCacheFixtureAge is comfortably past any window the sweep is
	// asked for.
	legacyGoCacheFixtureAge = DefaultStaleAge + time.Hour
)

// legacyGoCacheFixture builds one instance of the family — `<base>/<key>/gocache`
// — empty inside, which is the measured shape (the refusing arm aborts before
// any build writes into it).
func legacyGoCacheFixture(t *testing.T, base, key string) string {
	t.Helper()
	cache := filepath.Join(base, key, baseResidueLegacyGoCacheSuffix)
	if err := os.MkdirAll(cache, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", cache, err)
	}
	return filepath.Join(base, key)
}

// TestLegacyBareKeyGoCache_ShapeTable pins the CONTENT rule, which is what keeps
// the arm from being the blunt "collect anything hex-shaped" it must never be.
// The refused half is the point: a name in our key shape is not a delete
// authorisation for a tree we did not write.
func TestLegacyBareKeyGoCache_ShapeTable(t *testing.T) {
	base := t.TempDir()

	// ── accepted: the measured shape — a key holding only the toolchain's leaf,
	// whose content is nothing at all.
	empty := legacyGoCacheFixture(t, base, legacyGoCacheFixtureKey)
	if !isLegacyBareKeyGoCacheDir(empty) {
		t.Errorf("%s: an empty <key>/%s is the measured shape and must be ours",
			empty, baseResidueLegacyGoCacheSuffix)
	}

	// ── accepted: a cache the toolchain actually wrote into — entry shards plus
	// its own bookkeeping files. Still this system's tree.
	writtenKey := "0f1e2d3c4b5a6978"
	written := legacyGoCacheFixture(t, base, writtenKey)
	shard := filepath.Join(written, baseResidueLegacyGoCacheSuffix, "ab")
	if err := os.MkdirAll(shard, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", shard, err)
	}
	if err := os.WriteFile(filepath.Join(shard, "0123456789abcdef-a"), []byte("entry"), FilePerm); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	for _, name := range baseResidueLegacyGoCacheShapes {
		if err := os.WriteFile(filepath.Join(written, baseResidueLegacyGoCacheSuffix, name), []byte("x"), FilePerm); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if !isLegacyBareKeyGoCacheDir(written) {
		t.Errorf("%s: shards + the toolchain's own bookkeeping are a cache we wrote and must be ours", written)
	}

	// ── refused: the name is our key shape, the tree inside is NOT ours.
	//
	// These are the entries a blunt prefix/shape rule would have deleted — an
	// operator's own directory parked in the shared base under a hex name.
	refused := []struct {
		name  string
		build func(t *testing.T, dir string)
		why   string
	}{
		{
			name: "aaaabbbbccccdddd",
			build: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), FilePerm); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			why: "a hex-named directory holding a person's file: not a tree this system wrote",
		},
		{
			name: "1111222233334444",
			build: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "src"), DirPerm); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
			why: "a hex-named directory that is somebody's checkout",
		},
		{
			name: "5555666677778888",
			build: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, baseResidueLegacyGoCacheSuffix, "not-a-shard"), DirPerm); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
			why: "a gocache leaf holding a directory that is not a toolchain entry shard",
		},
		{
			name: "99990000aaaabbbb",
			build: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, baseResidueLegacyGoCacheSuffix), DirPerm); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, baseResidueLegacyGoCacheSuffix, "my-notes.md"), []byte("x"), FilePerm); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			why: "a gocache leaf holding a file the toolchain never writes there",
		},
		{
			name: "ccdd0011eeff2233",
			build: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "gocache"), DirPerm); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(dir, "gomodcache"), DirPerm); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
			why: "a key holding MORE than the one leaf: a different tree",
		},
	}
	for _, tc := range refused {
		dir := filepath.Join(base, tc.name)
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		tc.build(t, dir)
		if isLegacyBareKeyGoCacheDir(dir) {
			t.Errorf("%s: %s — must be KEPT", tc.name, tc.why)
		}
	}

	// ── the NAME half: a symlink is never a candidate, and a dashed name is the
	// PREGENERATED family's (IsBaseResidueName's), not this arm's.
	if err := os.MkdirAll(filepath.Join(base, "ffeeddccbbaa9988"), DirPerm); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !isLegacyBareKeyGoCacheName("ffeeddccbbaa9988") {
		t.Errorf("a 16-hex name is the key shape this arm matches")
	}
	for _, name := range []string{
		"gocache-01a0a392",  // the PREGENERATED family: it carries a dash, so the generic arm owns it
		"8a28affa4f85ba2",   // 15 hex: too short
		"8a28affa4f85ba270", // 17 hex: too long
		"8A28AFFA4F85BA27",  // uppercase: os.MkdirTemp's alphabet would carry it, the writer's would not
		"gocache",           // the bare leaf: a directory named after the leaf is not a key
	} {
		if isLegacyBareKeyGoCacheName(name) {
			t.Errorf("%q must NOT match this arm's name rule", name)
		}
	}
}

// TestLegacyBareKeyGoCache_PlanSweepAndVerdictsAgree is the family's RED→GREEN
// arm end to end: a stale, unheld instance IS collected, a fresh one and a
// differently-shaped one are UNTOUCHED, and the three faces (plan, verdicts,
// sweep) cannot disagree.
//
// RED BEFORE THE CHANGE: with no arm for this family, `SweepBase` removed 0 and
// `BaseResiduePlan`/`BaseResidueVerdicts` reported nothing about it — the tree
// was in nobody's namespace while sitting in the shared base.
func TestLegacyBareKeyGoCache_PlanSweepAndVerdictsAgree(t *testing.T) {
	base := t.TempDir()

	stale := legacyGoCacheFixture(t, base, legacyGoCacheFixtureKey)
	reAge(t, stale, legacyGoCacheFixtureAge)

	fresh := legacyGoCacheFixture(t, base, "0123456789abcdef")

	// A hex-named directory that is NOT this family, aged past the window: it
	// must survive every face.
	foreign := filepath.Join(base, "deadbeefdeadbeef")
	if err := os.MkdirAll(foreign, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", foreign, err)
	}
	if err := os.WriteFile(filepath.Join(foreign, "mine.txt"), []byte("x"), FilePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	reAge(t, foreign, legacyGoCacheFixtureAge)

	// The PLAN face names exactly the stale instance.
	plan, err := BaseResidueLegacyGoCachePlan(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan) != 1 || plan[0].Path != stale {
		t.Fatalf("plan = %+v, want exactly %s", plan, stale)
	}

	// The --explain face gives a verdict for EVERY bare-key entry (so a reader
	// can ask "why is this one still here?") and agrees with the plan.
	verdicts, err := BaseResidueLegacyGoCacheVerdicts(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("verdicts: %v", err)
	}
	byPath := map[string]EntryVerdict{}
	for _, v := range verdicts {
		byPath[v.Path] = v
	}
	if len(verdicts) != 3 {
		t.Fatalf("verdicts covered %d entries, want 3 (the stale, the fresh and the foreign hex-named one)", len(verdicts))
	}
	if v := byPath[stale]; !v.Collectable {
		t.Errorf("the stale instance must be collectable, got kept: %s", v.Reason)
	}
	if v := byPath[fresh]; v.Collectable {
		t.Errorf("a FRESH instance must be kept, got collectable (%s)", v.Reason)
	}
	if v := byPath[foreign]; v.Collectable {
		t.Errorf("a hex-named directory that is not this family must be kept, got collectable (%s)", v.Reason)
	}

	// The SWEEP collects the stale instance and nothing else. It goes through
	// SweepBase, i.e. through the arm's ONE wired call site, not through the
	// helper directly — a helper-only test would not notice the arm being
	// unwired.
	removed, err := SweepBase(base, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Errorf("SweepBase removed %d, want 1 (the stale instance)", removed)
	}
	if exists(t, stale) {
		t.Errorf("the stale instance survived the sweep")
	}
	if !exists(t, fresh) {
		t.Errorf("the FRESH instance was collected — the age rule did not hold")
	}
	if !exists(t, foreign) {
		t.Errorf("a hex-named directory that is not this family was collected — the shape rule did not hold")
	}
}

// TestLegacyBareKeyGoCache_SymlinkIsNeverACandidate pins the hard kind rule for
// this family: the name can be ours and the shape can look right, but a symlink's
// target is not this sweep's to remove.
func TestLegacyBareKeyGoCache_SymlinkIsNeverACandidate(t *testing.T) {
	base := t.TempDir()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, baseResidueLegacyGoCacheSuffix), DirPerm); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(base, legacyGoCacheFixtureKey)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	reAge(t, link, legacyGoCacheFixtureAge)

	if isLegacyBareKeyGoCacheDir(link) {
		t.Errorf("a symlink must never be this family's candidate, whatever is on the other side")
	}
	if removed, err := SweepBase(base, DefaultStaleAge); err != nil {
		t.Fatalf("sweep: %v", err)
	} else if removed != 0 {
		t.Errorf("SweepBase removed %d symlinked entries, want 0", removed)
	}
	if !exists(t, link) || !exists(t, target) {
		t.Errorf("the symlink or its target was removed — a link is never followed")
	}
}
