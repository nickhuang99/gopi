package main

// cli_scratch_gc_trees_test.go — the DECLARED-TREES arm's contract
// (`gopi tool scratch-gc trees`), the acceptance the epic states: a backdated entry
// planted in EACH of the declared trees — including the shared build cache whose
// per-checkout keys used to accumulate unbounded — is removed by a REAL run of
// the verb, every declared ROOT survives, a fresh entry and a foreign file
// survive, and the line reports the tree and the bytes.
//
// The E2E cases exec the built binary through testutil.RunGopi against a
// SANDBOX: GOTMPDIR points into a t.TempDir, GOPI_HOME puts the gopi data root
// (raw + sessions) inside the same sandbox. Nothing here can touch the
// developer's real /home/user/workspace/gopi-go-tmp, ~/.gopi/raw or sessions.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// declaredTreesEnv is the child environment for the declared-trees cases: a
// sandbox gopi data root (raw + sessions live under it), a sandbox GOTMPDIR, and
// the scratch home sandbox so the DEFAULT arm (which the same binary would sweep
// if it were invoked) cannot see the developer's tree either.
func declaredTreesEnv(home, goTmp string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GOPI_HOME=" + home,
		"GOPI_SETTINGS=" + filepath.Join(home, "settings.json"),
		scratch.GoTmpDirEnvName + "=" + goTmp,
		scratch.HomeEnvName + "=" + filepath.Join(home, "workspace", "gopi-test-scratch"),
	}
}

// backdate sets a path's mtime to ageBefore.
func backdate(t *testing.T, path string, age time.Duration) {
	t.Helper()
	stamp := time.Now().Add(-age)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}

// seedDeclaredTreesFixture plants one entry in each of the three trees, aged
// past its OWN constant, plus the controls each tree must keep: a fresh entry
// and (for the raw trees) a foreign file. It returns the paths by role.
type declaredTreesFixture struct {
	home    string
	goTmp   string
	rawDir  string
	sessDir string
	runDir  string
	cache   string

	agedGoBuild   string
	freshGoBuild  string
	agedRawLog    string
	freshRawLog   string
	foreignRawLog string
	agedSessRaw   string
	sessRecord    string
	agedCacheKey  string
	freshCacheKey string
	foreignCache  string
}

func seedDeclaredTreesFixture(t *testing.T) declaredTreesFixture {
	t.Helper()
	home := t.TempDir()
	f := declaredTreesFixture{
		home:  home,
		goTmp: filepath.Join(home, "workspace", "gopi-go-tmp"),
		// The shared build cache hangs off the WORKTREE ROOT (worktreeroots'
		// default here, because the sandbox's settings file does not declare
		// one) — the same join the merge gate writes through.
		cache:   filepath.Join(home, "workspace", worktreeroots.SharedBuildCacheDirName),
		rawDir:  filepath.Join(home, ".gopi", "raw"),
		sessDir: filepath.Join(home, ".gopi", "sessions"),
	}
	for _, dir := range []string{f.goTmp, f.rawDir, f.sessDir} {
		if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
			t.Fatal(err)
		}
	}

	// Tree 1 — the GOTMPDIR tree: an abandoned go-build* dir, plus a fresh one
	// (a build that is starting right now).
	f.agedGoBuild = filepath.Join(f.goTmp, "go-build1234567")
	f.freshGoBuild = filepath.Join(f.goTmp, "go-build7654321")
	if err := os.MkdirAll(f.agedGoBuild, scratch.DirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.agedGoBuild, "obj.bin"), make([]byte, 2048), scratch.FilePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.freshGoBuild, scratch.DirPerm); err != nil {
		t.Fatal(err)
	}
	backdate(t, f.agedGoBuild, scratch.GoTmpStaleAge+2*time.Hour)

	// Tree 2 — the flat raw-log directory: an aged OUR log, a fresh one, and a
	// human's own dump carrying the same .log suffix (never a candidate: the
	// tree takes OUR name shape, which is what the gate's evidence pins).
	f.agedRawLog = filepath.Join(f.rawDir, "gopi-raw-20260101-000000.log")
	f.freshRawLog = filepath.Join(f.rawDir, "gopi-raw-20260920-000000.log")
	f.foreignRawLog = filepath.Join(f.rawDir, "fmt-detail.log")
	for _, p := range []string{f.agedRawLog, f.freshRawLog, f.foreignRawLog} {
		if err := os.WriteFile(p, []byte("raw http traffic\n"), scratch.FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	backdate(t, f.agedRawLog, scratch.RawLogStaleAge+2*time.Hour)
	backdate(t, f.foreignRawLog, scratch.RawLogStaleAge+2*time.Hour)

	// Tree 3 — the sessions tree: a run directory holding an aged per-run raw
	// log AND its session record (the record is why the DIRECTORY must survive
	// this run — the container is only removed once it is empty of them).
	f.runDir = filepath.Join(f.sessDir, "0f1e2d3c-4b5a-6978-8765-43210fedcba9")
	if err := os.MkdirAll(f.runDir, scratch.DirPerm); err != nil {
		t.Fatal(err)
	}
	f.agedSessRaw = filepath.Join(f.runDir, "0f1e2d3c-4b5a-6978-8765-43210fedcba9"+scratch.SessionRawLogFileSuffix)
	f.sessRecord = filepath.Join(f.runDir, "session.jsonl")
	if err := os.WriteFile(f.agedSessRaw, []byte("raw http traffic, per run\n"), scratch.FilePerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.sessRecord, []byte("{}\n"), scratch.FilePerm); err != nil {
		t.Fatal(err)
	}
	backdate(t, f.agedSessRaw, scratch.SessionRawLogStaleAge+time.Hour)

	// Tree 4 — the shared build cache: one abandoned per-checkout key (the shape
	// the merge gate mints, holding a cache body one level down), one key a build
	// is filling RIGHT NOW, and an operator's own directory beside them. The
	// freshness is planted in the CACHE BODY, not on the key directory, because
	// that is where the writer's writes land (the key's own mtime never moves
	// after the gate creates it — measured on the real tree).
	f.agedCacheKey = filepath.Join(f.cache, "0f0f0f0f0f0f0f0f", "gocache", "00")
	f.freshCacheKey = filepath.Join(f.cache, "1111111111111111", "gocache", "00")
	f.foreignCache = filepath.Join(f.cache, "operator-notes")
	for _, dir := range []string{f.agedCacheKey, f.freshCacheKey, f.foreignCache} {
		if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
			t.Fatal(err)
		}
	}
	agedCacheFile := filepath.Join(f.agedCacheKey, "abcdef01-a")
	freshCacheFile := filepath.Join(f.freshCacheKey, "abcdef01-d")
	foreignCacheFile := filepath.Join(f.foreignCache, "notes.txt")
	for _, p := range []string{agedCacheFile, freshCacheFile, foreignCacheFile} {
		if err := os.WriteFile(p, make([]byte, 4096), scratch.FilePerm); err != nil {
			t.Fatal(err)
		}
	}
	backdate(t, agedCacheFile, scratch.BuildCacheStaleAge+2*time.Hour)
	backdate(t, foreignCacheFile, scratch.BuildCacheStaleAge+2*time.Hour)

	return f
}

// TestScratchGCCLITrees_RemovesAgedEntryInEachDeclaredTree is the epic's
// acceptance: a planted backdated entry in EACH of the three trees is removed by
// ONE real run, each tree's ROOT survives, and the line reports bytes per tree.
func TestScratchGCCLITrees_RemovesAgedEntryInEachDeclaredTree(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	f := seedDeclaredTreesFixture(t)

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag}, "", declaredTreesEnv(f.home, f.goTmp), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}

	// Each planted entry is gone …
	for _, gone := range []string{f.agedGoBuild, f.agedRawLog, f.agedSessRaw, filepath.Dir(filepath.Dir(f.agedCacheKey))} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("aged entry %s survived the declared-trees sweep (err=%v)", gone, err)
		}
	}
	// … every declared ROOT survives (GOTMPDIR above all: deleting it broke the
	// next build), and so does the run directory that still holds a session.
	for _, survives := range []string{f.goTmp, f.rawDir, f.sessDir, f.runDir, f.sessRecord, f.cache} {
		if _, err := os.Stat(survives); err != nil {
			t.Errorf("ROOT/record %s did not survive the sweep: %v", survives, err)
		}
	}
	// … the controls are untouched.
	for _, kept := range []string{f.freshGoBuild, f.freshRawLog, f.foreignRawLog, f.freshCacheKey, f.foreignCache} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("control %s was collected — the age/namespace rules must protect it: %v", kept, err)
		}
	}

	// The log line: the arm's role, one line per tree, and the reclaimed BYTES.
	for _, want := range []string{
		"role=" + scratchGCTreesRole,
		"tree=go-tmp", "bytes=",
		"tree=raw",
		"tree=session-raw",
		"tree=" + scratch.BuildCacheTreeName,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log must carry %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tree=go-tmp bytes=0") {
		t.Errorf("the reclaimed go-build entry must be reported with a non-zero byte count, got:\n%s", out)
	}
	// The build-cache line reports BYTES too: a key is removed whole, so the
	// number is the subtree it freed.
	if strings.Contains(out, "tree="+scratch.BuildCacheTreeName+" bytes=0") {
		t.Errorf("the reclaimed cache key must be reported with a non-zero byte count, got:\n%s", out)
	}
}

// TestScratchGCCLITrees_DryRunDeletesNothing pins the vetting face of the new
// arm: the plan names every candidate, deletes nothing, and reports the bytes it
// would reclaim.
func TestScratchGCCLITrees_DryRunDeletesNothing(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	f := seedDeclaredTreesFixture(t)

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag, scratchGCDryRunFlag}, "", declaredTreesEnv(f.home, f.goTmp), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	for _, cand := range []string{f.agedGoBuild, f.agedRawLog, f.agedSessRaw, filepath.Dir(filepath.Dir(f.agedCacheKey))} {
		if !strings.Contains(out, "would-remove "+cand) {
			t.Errorf("dry-run must name the candidate %s, got:\n%s", cand, out)
		}
		if _, err := os.Lstat(cand); err != nil {
			t.Errorf("dry-run DELETED %s — a report must have no side effects: %v", cand, err)
		}
	}
	if !strings.Contains(out, "dry-run=true") {
		t.Errorf("the plan line must be marked dry-run=true, got:\n%s", out)
	}
}

// TestScratchGCCLITrees_UnconfiguredTreeIsReportedNotAlarmed: a tree that does
// not exist on this host (no GOTMPDIR, no raw dir) is REPORTED with its own line
// and exits 0 — a watchdog must not alarm every 30 minutes for a tree that was
// never created — while a tree that exists and cannot be read IS an alarm
// (covered by the plan/sweep error path). This case pins the first half.
func TestScratchGCCLITrees_UnconfiguredTreeIsReportedNotAlarmed(t *testing.T) {
	home := t.TempDir()
	// GOPI_HOME points at the sandbox but NOTHING under it exists: no sessions,
	// no raw dir. GOTMPDIR points at a directory that does not exist either.
	missing := filepath.Join(home, "workspace", "gopi-go-tmp")

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag}, "", declaredTreesEnv(home, missing), "")
	if code != 0 {
		t.Fatalf("an unconfigured tree must not alarm: exit=%d stderr=%s", code, stderr)
	}
	// HOME (the sandbox) is set but GOPI_HOME is NOT, so GopiDir falls back to
	// the real user home — that would make this case touch the developer's own
	// raw dir. The env above DOES set it (declaredTreesEnv), so the raw/session
	// trees must be reported as unconfigured, not swept.
	if !strings.Contains(out, "tree=go-tmp") || !strings.Contains(out, "tree=raw") ||
		!strings.Contains(out, "tree=session-raw") || !strings.Contains(out, "tree="+scratch.BuildCacheTreeName) {
		t.Errorf("every declared tree must get a line, configured or not, got:\n%s", out)
	}
}

// --- the PER-TREE SELECTOR (`trees --tree NAME`) -----------------------------
//
// THE EPIC'S ACCEPTANCE, DRIVEN THROUGH THE BINARY. `gopi tool scratch-gc trees`
// swept EVERY declared tree, so an operator who wanted the GOTMPDIR tree bounded
// also reclaimed the shared build cache (~50 GB on the developer's host) and the
// raw logs beside it. `--tree NAME` bounds the pass to one tree; these cases run
// the REAL binary over a SANDBOX (no path here can reach the developer's own
// trees) and assert the confinement by byte-level digests taken before and after.

// selectorDigest walks a directory into ONE digest over names + sizes + kinds
// (metadata only — nothing here reads a cache body or creates load, Rule 27).
func selectorDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		lines = append(lines, fmt.Sprintf("%s|%d|%t", rel, info.Size(), d.IsDir()))
		return nil
	})
	if err != nil {
		t.Fatalf("digest %s: %v", root, err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// TestScratchGCCLITrees_SelectorSweepsExactlyOneDeclaredTree is deliverable 4's
// machine-checked half: the named tree's aged entry moves, every OTHER declared
// tree is byte-for-byte identical, the run report carries ONE tree= line, and
// every declared ROOT — the GOTMPDIR root above all — is still present.
func TestScratchGCCLITrees_SelectorSweepsExactlyOneDeclaredTree(t *testing.T) {
	f := seedDeclaredTreesFixture(t)

	before := map[string]string{}
	for _, root := range []string{f.rawDir, f.sessDir, f.cache} {
		before[root] = selectorDigest(t, root)
	}
	cacheKey := filepath.Dir(filepath.Dir(f.agedCacheKey))

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag,
		scratch.DeclaredTreeFlagSpelling, scratch.TreeNameGoTmp}, "", declaredTreesEnv(f.home, f.goTmp), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}

	// (a) the NAMED tree's aged entry is gone; its fresh control survives.
	if _, err := os.Lstat(f.agedGoBuild); !os.IsNotExist(err) {
		t.Errorf("the named tree's aged entry %s survived a --tree %s pass (err=%v)", f.agedGoBuild, scratch.TreeNameGoTmp, err)
	}
	if _, err := os.Stat(f.freshGoBuild); err != nil {
		t.Errorf("the named tree's FRESH control was collected: %v", err)
	}
	// (b) NO OTHER DECLARED TREE was touched — byte-for-byte. This is the
	// failure the epic exists to prevent (a build cache is expensive to
	// rebuild) and it fails here rather than on the operator's disk.
	for _, root := range []string{f.rawDir, f.sessDir, f.cache} {
		if got := selectorDigest(t, root); got != before[root] {
			t.Errorf("declared tree %s WAS TOUCHED by a --tree %s pass:\n  before %s\n  after  %s", root, scratch.TreeNameGoTmp, before[root], got)
		}
	}
	for _, kept := range []string{f.agedRawLog, f.agedSessRaw, cacheKey, f.foreignCache} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("another declared tree's entry %s was collected by a --tree %s pass: %v", kept, scratch.TreeNameGoTmp, err)
		}
	}
	// (c) the report: exactly ONE tree= line, and it is the named one.
	if n := strings.Count(out, "role="+scratchGCTreesRole); n != 1 {
		t.Errorf("a --tree pass must print ONE line, got %d:\n%s", n, out)
	}
	for _, want := range []string{"tree=" + scratch.TreeNameGoTmp, "present=true", "entries=", "bytes=", "stale="} {
		if !strings.Contains(out, want) {
			t.Errorf("the selector's report line must carry %q, got:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{"tree=" + scratch.TreeNameRaw, "tree=" + scratch.TreeNameSessionRaw, "tree=" + scratch.BuildCacheTreeName} {
		if strings.Contains(out, forbidden) {
			t.Errorf("a --tree %s pass reported %q — the selector did not bound the pass:\n%s", scratch.TreeNameGoTmp, forbidden, out)
		}
	}
	// (b′) every declared tree ROOT still exists.
	for _, survives := range []string{f.goTmp, f.rawDir, f.sessDir, f.cache} {
		if _, err := os.Stat(survives); err != nil {
			t.Errorf("declared ROOT %s did not survive the selected pass: %v", survives, err)
		}
	}
}

// TestScratchGCCLITrees_SelectorRefusesAnUnknownName pins the FAIL-CLOSED half,
// in dry-run and live form alike: an unknown name exits 1, names the valid ones,
// deletes nothing, and never renders as a clean empty pass (Rule 15).
func TestScratchGCCLITrees_SelectorRefusesAnUnknownName(t *testing.T) {
	f := seedDeclaredTreesFixture(t)

	for _, extra := range [][]string{nil, {scratchGCDryRunFlag}} {
		args := append([]string{CliVerbScratchGC, scratchGCTreesFlag, scratch.DeclaredTreeFlagSpelling, "no-such-tree"}, extra...)
		out, stderr, code := runGopiMapped(t, args, "", declaredTreesEnv(f.home, f.goTmp), "")
		if code == 0 {
			t.Errorf("argv %v: an unknown tree name must be REFUSED, got exit 0, stdout:\n%s", args, out)
		}
		if !strings.Contains(stderr, "no-such-tree") || !strings.Contains(stderr, scratch.TreeNameGoTmp) {
			t.Errorf("argv %v: the refusal must name the offending value AND the valid names, got stderr:\n%s", args, stderr)
		}
	}
	// Nothing was swept by either refusal.
	for _, kept := range []string{f.agedGoBuild, f.agedRawLog, f.agedSessRaw} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("a refused selector swept %s — a refusal must touch nothing: %v", kept, err)
		}
	}
	// An EXPLICITLY EMPTY value is refused too: widening a bounded destructive
	// pass because of a truncated argument is what fail-closed prevents.
	_, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag, scratch.DeclaredTreeFlagSpelling + "="},
		"", declaredTreesEnv(f.home, f.goTmp), "")
	if code == 0 {
		t.Errorf("`--tree=` must be refused, got exit 0 (stderr=%s)", stderr)
	}
}

// TestScratchGCCLITrees_SelectorAllEqualsNoSelector: the `all` value and an
// absent selector are the SAME pass — the arm's behaviour before the selector
// existed, preserved (so a script can be explicit about the full set).
func TestScratchGCCLITrees_SelectorAllEqualsNoSelector(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	absent := seedDeclaredTreesFixture(t)
	explicit := seedDeclaredTreesFixture(t)

	absentOut, absentErr, absentCode := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag},
		"", declaredTreesEnv(absent.home, absent.goTmp), "")
	allOut, allErr, allCode := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag,
		scratch.DeclaredTreeFlagSpelling + "=" + scratch.DeclaredTreeSelectorAll},
		"", declaredTreesEnv(explicit.home, explicit.goTmp), "")
	if absentCode != 0 || allCode != 0 {
		t.Fatalf("absent: exit=%d err=%s; all: exit=%d err=%s", absentCode, absentErr, allCode, allErr)
	}
	if n, m := strings.Count(absentOut, "role="+scratchGCTreesRole), strings.Count(allOut, "role="+scratchGCTreesRole); n != m {
		t.Errorf("`--tree all` printed %d lines, an absent selector printed %d — the two must be the same pass:\n%s\n---\n%s", m, n, absentOut, allOut)
	}
	for _, gone := range []string{absent.agedGoBuild, absent.agedRawLog, absent.agedSessRaw, explicit.agedGoBuild, explicit.agedRawLog, explicit.agedSessRaw} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("the full-set pass left %s behind (err=%v) — `all` must sweep every declared tree", gone, err)
		}
	}
}

// --- `removed=` IS A MEASUREMENT, NEVER A PLAN -------------------------------
//
// THE DEFECT THESE CASES CLOSE (measured 2026-10-27, task 01a17bb7): the dry-run
// line passed len(plan) into the shared `removed=` slot, so a report-only pass
// printed `removed=1` while deleting NOTHING. The field means "what this sweep
// deleted" on every other face of this verb, so the plan count was published as
// a deletion claim — a reader could not tell the report from the run.
//
// The fix is the field NAME, not the number: the vetting face still reports what
// it WOULD remove, as `plan=`. The two cases below pin both halves against the
// REAL binary, and they are written so the old behaviour fails:
//
//   - the dry-run case asserts NO `removed=` field at all (the pre-fix line
//     carries one, so this case is red on the old code) and that the same
//     quantity appears as `plan=`;
//   - the live case asserts `removed=` EQUALS the number of entries that actually
//     left the disk, so the field cannot go back to carrying a plan figure.

// treesLogFields parses the `role=<role>` logfmt lines of a verb's stdout into
// name→value maps, one per line, and requires at least one line (a scan that
// found nothing must not pass as agreement — Rule 15).
func treesLogFields(t *testing.T, out, role string) []map[string]string {
	t.Helper()
	var rows []map[string]string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "scratch-gc role="+role+" ") {
			continue
		}
		row := map[string]string{}
		for _, field := range strings.Fields(line) {
			if k, v, ok := strings.Cut(field, "="); ok {
				row[k] = v
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatalf("no `role=%s` line in the output — nothing was measured:\n%s", role, out)
	}
	return rows
}

// TestScratchGCCLITrees_PlanLineNeverSpellsItsPlanAsRemoved is the RED-then-GREEN
// half for the report-only face: `--dry-run` must not publish a `removed=` field,
// and the plan count must land in `plan=`.
func TestScratchGCTrees_DryRunPlanIsNotARemovedField(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	// The derivation itself, first: the plan format is scratchGCTreesLogFmt with
	// the ONE field renamed. If the live format ever loses `removed=%d`, the
	// substitution becomes a no-op and BOTH faces would print the same line — so
	// the derivation is pinned here rather than trusted (a silent no-op is exactly
	// the shape that makes a rename test pass on a broken format).
	if strings.Contains(scratchGCTreesPlanLogFmt, scratchGCTreesRemovedField) {
		t.Fatalf("the plan format still carries %q: %q", scratchGCTreesRemovedField, scratchGCTreesPlanLogFmt)
	}
	if n := strings.Count(scratchGCTreesPlanLogFmt, scratchGCTreesPlanField); n != 1 {
		t.Fatalf("the plan format must carry %q exactly once, got %d: %q", scratchGCTreesPlanField, n, scratchGCTreesPlanLogFmt)
	}
	if strings.Contains(scratchGCTreesLogFmt, "plan=") {
		t.Fatalf("the LIVE format must never carry plan=: %q", scratchGCTreesLogFmt)
	}

	f := seedDeclaredTreesFixture(t)
	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag, scratchGCDryRunFlag}, "", declaredTreesEnv(f.home, f.goTmp), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}

	rows := treesLogFields(t, out, scratchGCTreesRole)

	// A tree that RESOLVES on this host gets a PLAN line (`present=true`); a tree
	// that does not gets the FAILURE line (`present=false`, its reason on
	// stderr). The two are different facts and are asserted apart: a failure
	// row's `removed=0` is a measurement ("nothing was deleted"), not the plan
	// count this case is about.
	planned, planRows, failureRows := 0, 0, 0
	for _, row := range rows {
		name := row["tree"]
		// NOTHING was deleted by a report-only pass, so a NON-ZERO removed= on
		// this output is by definition a plan count wearing a deletion's name —
		// which is the defect in one sentence (the pre-fix line printed
		// removed=1 here).
		if got, ok := row["removed"]; ok && got != "0" {
			t.Errorf("tree=%s: the dry-run reported removed=%s — this pass deleted NOTHING, so that figure is a plan count published as a deletion:\n%s", name, got, out)
		}
		if row["present"] != "true" {
			failureRows++
			if got, ok := row["plan"]; ok {
				t.Errorf("tree=%s does not resolve on this host, yet its line carries plan=%s — an unmeasurable tree must not report a plan:\n%s", name, got, out)
			}
			continue
		}
		planRows++
		got, ok := row["plan"]
		if !ok {
			t.Errorf("tree=%s: a plan line must carry plan=, and must NOT carry removed= (got removed=%q):\n%s", name, row["removed"], out)
			continue
		}
		if m := row["dry-run"]; m != "true" {
			t.Errorf("tree=%s: every plan line must be marked dry-run=true, got %q:\n%s", name, m, out)
		}
		var n int
		if _, err := fmt.Sscanf(got, "%d", &n); err != nil {
			t.Errorf("tree=%s: plan=%s is not a count:\n%s", name, got, out)
		}
		planned += n
	}
	if planRows == 0 {
		t.Fatalf("no tree resolved on this fixture, so nothing measured the plan line:\n%s", out)
	}
	if failureRows == 0 {
		t.Fatalf("this fixture leaves most declared trees unconfigured, so the failure line must be present too — a scan that saw only one shape is not the agreement:\n%s", out)
	}

	// The plan is non-empty on this fixture (one aged entry per declared tree),
	// so "no removed=" cannot be satisfied by printing nothing at all.
	if planned < 1 {
		t.Fatalf("the fixture plants aged entries, so the plan must be non-empty, got plan=%d:\n%s", planned, out)
	}
	candidates := strings.Count(out, "would-remove ")
	if candidates != planned {
		t.Errorf("plan=%d disagrees with %d would-remove lines — the count and the list must describe the same plan:\n%s", planned, candidates, out)
	}

	// And the promise the name makes: nothing was deleted.
	for _, cand := range []string{f.agedGoBuild, f.agedRawLog, f.agedSessRaw, filepath.Dir(filepath.Dir(f.agedCacheKey))} {
		if _, err := os.Lstat(cand); err != nil {
			t.Errorf("the dry-run DELETED %s — it reported a plan, and a plan does not delete: %v", cand, err)
		}
	}
}

// TestScratchGCCLITrees_RemovedCountsWhatLeftTheDisk is the GREEN half for the
// live face: `removed=` must equal the number of fixture entries that actually
// disappeared — the count is checked against the DISK, not against the verb's own
// account of itself.
func TestScratchGCTrees_LiveRemovedIsTheDeletionCount(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	f := seedDeclaredTreesFixture(t)

	// The fixture's candidates, as the fixture itself names them (the build-cache
	// candidate is the whole key, which is two levels above the planted file).
	planted := []string{f.agedGoBuild, f.agedRawLog, f.agedSessRaw, filepath.Dir(filepath.Dir(f.agedCacheKey))}

	out, stderr, code := runGopiMapped(t, []string{CliVerbScratchGC, scratchGCTreesFlag}, "", declaredTreesEnv(f.home, f.goTmp), "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}

	gone := 0
	for _, cand := range planted {
		if _, err := os.Lstat(cand); os.IsNotExist(err) {
			gone++
		}
	}
	if gone == 0 {
		t.Fatalf("the live sweep removed nothing on a fixture that plants one aged entry per tree — nothing to measure:\n%s", out)
	}

	rows := treesLogFields(t, out, scratchGCTreesRole)
	reported := 0
	for _, row := range rows {
		got, ok := row["removed"]
		if !ok {
			t.Errorf("a LIVE line must carry removed= (it is the measurement of what this pass deleted):\n%s", out)
			continue
		}
		if _, ok := row["plan"]; ok {
			t.Errorf("a live line carried plan= — the plan field belongs to the report-only face:\n%s", out)
		}
		if _, ok := row["dry-run"]; ok {
			t.Errorf("a live line was marked dry-run:\n%s", out)
		}
		var n int
		if _, err := fmt.Sscanf(got, "%d", &n); err != nil {
			t.Errorf("removed=%s is not a count:\n%s", got, out)
		}
		reported += n
	}
	if reported != gone {
		t.Errorf("the verb reported removed=%d but %d planted entries left the disk — removed= must BE the deletion count, not a plan:\n%s", reported, gone, out)
	}

	// The controls are untouched, so `gone` counts the pass's work and not the
	// fixture's own losses.
	for _, kept := range []string{f.freshGoBuild, f.freshRawLog, f.foreignRawLog, f.sessRecord, f.freshCacheKey, f.foreignCache} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("control %s was collected: %v", kept, err)
		}
	}
}
