package testutil

// Legacy scratch-residue sweep tests.
//
// Every test that exercises the sweep runs against a PRIVATE memory base
// (t.Setenv(MemoryBaseEnvName, …)) under the host's memory filesystem, so the
// host's real /dev/shm is never swept by the tests that verify how it is
// swept. The single exception is TestHostLegacyResidueIsSweptByTheSuitePin,
// which reads (never mutates) the host base to state the post-condition.

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// legacyFixturePrefix names the private base the sweep tests create under
	// the memory filesystem.
	legacyFixturePrefix = "gopi-testutil-legacybase-"
	// ownResidueName / toolsResidueName are the two real legacy spellings this
	// system produced (internal/testutil's and internal/tools' original pin).
	ownResidueName   = "gopi-suite-tmp-1234567"
	toolsResidueName = "gopi-tools-suite-tmp-7654321"
	// foreignResidueName is another PROGRAM's directory: it carries the gopi/
	// suite-tmp vocabulary but not this system's namespace, and must never be
	// touched — the sweep's negative control.
	foreignResidueName = "someone-else-suite-tmp-123"
	// foreignGopiName is a gopi-prefixed directory that is NOT scratch: the
	// account/diagnostic dirs (gopi-acc-*, gopi-diag-*, gopi-gate-probe-*) all
	// use this shape, and the infix check is what keeps them safe.
	foreignGopiName = "gopi-acc-9999999"
	// legacySweepSetupScript is the host script whose variable spelling the
	// sweep must share (drift gate).
	legacySweepSetupScript = "setup-scratch-home.sh"
)

// fixtureMemoryBase creates a private, off-`/` memory base and points the
// sweep at it. It skips (never fails) on a host whose shared-memory filesystem
// is unusable — the sweep cannot be demonstrated there, which is exactly the
// answer TestGCLegacyScratchResidueSkipsABaseOnRootFS covers the other way.
func fixtureMemoryBase(t *testing.T) string {
	t.Helper()
	if fi, err := os.Stat(MemoryBaseDefault); err != nil || !fi.IsDir() {
		t.Skipf("%s is not usable on this host — the legacy-base sweep cannot be exercised", MemoryBaseDefault)
	}
	if shares, err := ScratchRootSharesRootFS(MemoryBaseDefault); err != nil || shares {
		t.Skipf("%s shares the / filesystem here — sweeping it is banned, so there is nothing to demonstrate", MemoryBaseDefault)
	}
	base := filepath.Join(MemoryBaseDefault, legacyFixturePrefix+strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(base, dirPerm); err != nil {
		t.Skipf("private memory base %s is not creatable (%v)", base, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove the private memory base %s: %v", base, err)
		}
	})
	// Hermetic: the base AND the scratch-home override are decided by the
	// test. An override left set would disable the sweep by design (see
	// TestGCLegacyScratchResidueHonoursTheScratchHomeOverride).
	t.Setenv(MemoryBaseEnvName, base)
	t.Setenv(ScratchHomeEnvName, "")
	t.Setenv(SuiteTempRootEnvName, "")
	return base
}

// TestIsLegacyScratchResidueNamespace is the namespace limit itself: the
// sweep's safety argument is this predicate, so its accept/reject table is
// asserted directly rather than inferred from a removal count.
func TestIsLegacyScratchResidueNamespace(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{ownResidueName, true},
		{toolsResidueName, true},
		{"gopi-suite-tmp-0", true},
		// Never collected: other programs, this system's non-scratch dirs, the
		// scratch home, and names that only look similar.
		{foreignResidueName, false},
		{foreignGopiName, false},
		{"gopi-diag-3882788", false},
		{"gopi-gate-probe-2806456", false},
		{"gopi-test-scratch", false},
		{ScratchRootDirName, false},
		{"gopi-merge-batch-123", false},
		{"suite-tmp-123", false},         // no gopi- prefix: not ours
		{"gopi-suite-tmp-", false},       // no random suffix: never created by us
		{"gopisuite-tmp-1", false},       // no gopi- prefix boundary
		{"gopi-x-suite-tmp-1", true},     // nested pattern still ours (MkdirTemp of a suite name)
		{"suite-cmd-agent-run-1", false}, // the CURRENT per-run root shape, home-level only
		{"", false},                      //
		{"x", false},                     //
	}
	for _, c := range cases {
		if got := IsLegacyScratchResidue(c.name); got != c.want {
			t.Errorf("IsLegacyScratchResidue(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestGCLegacyScratchResidueCollectsAgedOrphanAndKeepsForeignEntry is the
// acceptance case: a 7-hour-old self-created legacy root is collected while a
// foreign program's directory — and this system's non-scratch directories —
// survive untouched.
func TestGCLegacyScratchResidueCollectsAgedOrphanAndKeepsForeignEntry(t *testing.T) {
	base := fixtureMemoryBase(t)
	orphan := mkScratchDir(t, base, ownResidueName)
	alsoOurs := mkScratchDir(t, base, toolsResidueName)
	foreign := mkScratchDir(t, base, foreignResidueName)
	foreignGopi := mkScratchDir(t, base, foreignGopiName)
	young := mkScratchDir(t, base, "gopi-suite-tmp-9999999")

	ageDir(t, orphan, ScratchStaleAge+time.Hour)   // 7h old
	ageDir(t, alsoOurs, ScratchStaleAge+time.Hour) // 7h old
	ageDir(t, foreign, ScratchStaleAge+time.Hour)  // 7h old, NOT ours
	ageDir(t, foreignGopi, ScratchStaleAge+time.Hour)
	ageDir(t, young, ScratchStaleAge-time.Hour) // 5h old: inside the threshold

	removed, err := GCLegacyScratchResidue()
	if err != nil {
		t.Fatalf("GCLegacyScratchResidue: %v", err)
	}
	if removed != 2 {
		t.Fatalf("sweep removed %d entries, want exactly 2 (the two 7h-old roots of this system)", removed)
	}
	for _, gone := range []string{orphan, alsoOurs} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("7h-old legacy root %s was NOT collected (stat err = %v)", gone, err)
		}
	}
	for _, kept := range []struct {
		path string
		why  string
	}{
		{foreign, "another program's directory"},
		{foreignGopi, "a gopi-prefixed NON-scratch directory"},
		{young, "a legacy root younger than the threshold"},
	} {
		if _, err := os.Stat(kept.path); err != nil {
			t.Fatalf("%s was deleted — %s must never be touched: %v", kept.path, kept.why, err)
		}
	}
}

// TestGCLegacyScratchResidueNeverDeletesAHeldRoot is the HARD RULE applied to
// the second sweep: an aged legacy root whose owner is alive survives, and is
// collected once the holder exits (proving the survivor was kept by the holder
// check and not by an inert sweep).
func TestGCLegacyScratchResidueNeverDeletesAHeldRoot(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	base := fixtureMemoryBase(t)
	held := mkScratchDir(t, base, ownResidueName)

	cmd := exec.Command(sleepBinary, sleepArg)
	cmd.Dir = held
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder in %s: %v", held, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()    //stylegate:ignore kills the child process the test spawned and is tearing down
		_, _ = cmd.Process.Wait() //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	})
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := waitForHolder(held, pid, procHolderWait); err != nil {
		t.Fatalf("%v", err)
	}
	ageDir(t, held, ScratchStaleAge+time.Hour)
	ageDir(t, base, ScratchStaleAge+time.Hour) // the base itself must never be swept

	if removed, err := GCLegacyScratchResidue(); err != nil {
		t.Fatalf("GCLegacyScratchResidue: %v", err)
	} else if removed != 0 {
		t.Fatalf("sweep removed %d entries while a live process held %s — the holder check failed", removed, held)
	}
	if _, err := os.Stat(held); err != nil {
		t.Fatalf("the held legacy root %s was deleted under a live process: %v", held, err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_, _ = cmd.Process.Wait() //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	if err := waitForNoHolder(held, procHolderWait); err != nil {
		t.Fatalf("%v", err)
	}
	if removed, err := GCLegacyScratchResidue(); err != nil {
		t.Fatalf("sweep after the holder exited: %v", err)
	} else if removed != 1 {
		t.Fatalf("sweep removed %d entries after the holder exited, want 1", removed)
	}
}

// TestGCLegacyScratchResidueSkipsABaseOnRootFS — the sweep must refuse a base
// on the / filesystem: the ban's whole point is that `/` (and /tmp on this
// host) is not scratch space. The aged, correctly-named fixture therefore
// survives.
func TestGCLegacyScratchResidueSkipsABaseOnRootFS(t *testing.T) {
	if fi, err := os.Stat(banProbeDir); err != nil || !fi.IsDir() {
		t.Skipf("%s is missing on this host", banProbeDir)
	}
	shares, err := ScratchRootSharesRootFS(banProbeDir)
	if err != nil || !shares {
		t.Skipf("%s is not on the / filesystem here — the refusal cannot be demonstrated", banProbeDir)
	}
	base, err := os.MkdirTemp(banProbeDir, legacyFixturePrefix)
	if err != nil {
		t.Fatalf("private base under %s: %v", banProbeDir, err)
	}
	//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	t.Setenv(MemoryBaseEnvName, base)
	t.Setenv(ScratchHomeEnvName, "")
	t.Setenv(SuiteTempRootEnvName, "")

	orphan := mkScratchDir(t, base, ownResidueName)
	ageDir(t, orphan, ScratchStaleAge+time.Hour)

	if removed, err := GCLegacyScratchResidue(); err != nil {
		t.Fatalf("GCLegacyScratchResidue: %v", err)
	} else if removed != 0 {
		t.Fatalf("sweep removed %d entries from a base on the / filesystem — the ban must make the sweep a no-op there", removed)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("%s was collected from a /-filesystem base: %v", orphan, err)
	}
}

// TestGCLegacyScratchResidueHonoursTheScratchHomeOverride — an operator pin
// (GOPI_TEST_TMPDIR) points at a scratch HOME the caller owns, so the sweep
// must stand down entirely rather than collect in the host's memory base.
func TestGCLegacyScratchResidueHonoursTheScratchHomeOverride(t *testing.T) {
	base := fixtureMemoryBase(t)
	orphan := mkScratchDir(t, base, ownResidueName)
	ageDir(t, orphan, ScratchStaleAge+time.Hour)

	t.Setenv(SuiteTempRootEnvName, filepath.Join(base, "operator-pinned-home"))
	if bases := LegacyScratchBases(); len(bases) != 0 {
		t.Fatalf("LegacyScratchBases() = %v with %s set, want none (an explicit pin owns its own tree)",
			bases, SuiteTempRootEnvName)
	}
	if removed, err := GCLegacyScratchResidue(); err != nil {
		t.Fatalf("GCLegacyScratchResidue: %v", err)
	} else if removed != 0 {
		t.Fatalf("sweep removed %d entries while an operator had pinned %s", removed, SuiteTempRootEnvName)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("%s was collected despite the operator pin: %v", orphan, err)
	}
}

// TestGCLegacyScratchResidueLeavesFilesAndSymlinksAlone — only a DIRECTORY in
// the namespace is collectable; a same-named regular file or symlink is not a
// root this system created (and following a symlink is how a sweep deletes
// somebody else's data).
func TestGCLegacyScratchResidueLeavesFilesAndSymlinksAlone(t *testing.T) {
	base := fixtureMemoryBase(t)
	file := filepath.Join(base, ownResidueName)
	if err := os.WriteFile(file, []byte("not a scratch root"), filePerm); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	victim := mkScratchDir(t, base, "unrelated-victim")
	link := filepath.Join(base, toolsResidueName)
	if err := os.Symlink(victim, link); err != nil {
		t.Fatalf("symlink %s: %v", link, err)
	}
	ageFile := func(p string) {
		when := time.Now().Add(-(ScratchStaleAge + time.Hour))
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatalf("age %s: %v", p, err)
		}
	}
	ageFile(file)
	ageFile(link)
	// Lstat reads the LINK's own mtime; set it explicitly for portability.
	if err := os.Chtimes(link, time.Now().Add(-(ScratchStaleAge + time.Hour)), time.Now().Add(-(ScratchStaleAge + time.Hour))); err != nil {
		t.Fatalf("age link %s: %v", link, err)
	}

	if removed, err := GCLegacyScratchResidue(); err != nil {
		t.Fatalf("GCLegacyScratchResidue: %v", err)
	} else if removed != 0 {
		t.Fatalf("sweep removed %d entries, want 0 (a file and a symlink are not scratch roots)", removed)
	}
	if _, err := os.Lstat(file); err != nil {
		t.Fatalf("the same-named regular file %s was removed: %v", file, err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("the same-named symlink %s was removed: %v", link, err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the symlink target %s was deleted through the link — a sweep followed a symlink: %v", victim, err)
	}
}

// TestMemoryBaseEnvNameHasOneDefinition is the Rule 13/14 exhaustion gate for
// the memory-base knob's SPELLING: the literal may appear in CODE in exactly
// one file (its definition site, internal/scratch/tmproot.go). Every other
// package references it — internal/testutil's suite-facing name is an alias of
// scratch.MemoryBaseEnvName, not a second copy — so a future re-inline fails
// here instead of silently drifting from the variable scripts/setup-scratch-home.sh
// reads. Comments are stripped before the scan: prose that NAMES the variable
// (to explain it) is documentation, not a definition, and the gate must not
// force documentation to be deleted to stay green.
func TestMemoryBaseEnvNameHasOneDefinition(t *testing.T) {
	const definitionFile = "internal/scratch/tmproot.go"
	literal := `"` + MemoryBaseEnvName + `"`

	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	scanned := 0
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if rel == definitionFile {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			scanned++
			for i, line := range strings.Split(string(raw), "\n") {
				if strings.Contains(stripGoComment(line), literal) {
					t.Errorf("%s:%d carries the %s literal — reference scratch.MemoryBaseEnvName (defined in %s) instead of re-inlining it (Rule 13/14)",
						rel, i+1, literal, definitionFile)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	// Read-floor: a moved tree must not turn this gate into a vacuous pass.
	if scanned < 100 {
		t.Fatalf("scanned only %d .go files — the tree moved or the walk is broken; the gate would pass blindly", scanned)
	}
}

// stripGoComment removes a line comment and a line's own leading comment so the
// mirror gate sees CODE only. It deliberately does not parse strings: a `//`
// inside a string literal containing the knob name does not exist in this tree,
// and a full parser here would be a second implementation of go/parser's job
// for one predicate. Block comments are handled by their own `//`-style shape
// (this tree uses line comments).
func stripGoComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// TestMemoryBaseNameMatchesTheSetupScript is the drift gate: whoever reads the
// memory base must read the SAME variable (Rule 13/14). A second spelling
// would leave one reader cleaning a base the other never wrote.
//
// The setup script is no longer one of those readers (operator ruling,
// 2026-09-18: it is retired and changes nothing), so the gate now pins the
// opposite contract — the retired script must NOT claim to read the base, or a
// future reader would be misled about who points the host where. The live
// reader is the legacy-residue sweep, and the alias check below keeps its
// spelling shared with internal/scratch by construction.
func TestMemoryBaseNameMatchesTheSetupScript(t *testing.T) {
	// The suite-facing name must BE the production one, not a copy of its value
	// (Rule 14): the alias is what makes the sweep's reader and this reader the
	// same knob by construction.
	if MemoryBaseEnvName != scratch.MemoryBaseEnvName {
		t.Fatalf("MemoryBaseEnvName = %q, scratch.MemoryBaseEnvName = %q — two spellings, one variable (Rule 14)",
			MemoryBaseEnvName, scratch.MemoryBaseEnvName)
	}
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	script := filepath.Join(root, scriptsDirName, legacySweepSetupScript)
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read %s: %v", script, err)
	}
	if strings.Contains(string(raw), MemoryBaseEnvName) {
		t.Fatalf("%s mentions %s, but the setup script is RETIRED and reads no base — the legacy-residue "+
			"sweep is the only reader. A script that looks like a reader without being one is how the "+
			"two-readers-one-base drift this gate exists for comes back", script, MemoryBaseEnvName)
	}
	if MemoryBaseDefault != ShmDir {
		t.Fatalf("MemoryBaseDefault = %q, ShmDir = %q — two spellings of the same base (Rule 14)", MemoryBaseDefault, ShmDir)
	}
	if got := MemoryBase(); got == "" || !filepath.IsAbs(got) {
		t.Fatalf("MemoryBase() = %q, want an absolute path", got)
	}
	// The ONE reader honours its variable (Rule 2): an operator's base is used
	// verbatim.
	private := filepath.Join(t.TempDir(), "private-base")
	t.Setenv(MemoryBaseEnvName, private)
	if got := MemoryBase(); got != private {
		t.Fatalf("MemoryBase() = %q with %s set, want %q", got, MemoryBaseEnvName, private)
	}
}

// TestGCLegacyScratchResidueAgeBoundary drives the explicit-threshold form and
// pins the boundary itself: a root older than the given threshold is collected,
// one younger than it is kept. It is also the coverage anchor for
// GCLegacyScratchResidueOlderThan — the watchdogs' knob must not be dead code.
func TestGCLegacyScratchResidueAgeBoundary(t *testing.T) {
	base := fixtureMemoryBase(t)
	justOver := mkScratchDir(t, base, ownResidueName)
	justUnder := mkScratchDir(t, base, toolsResidueName)
	threshold := time.Hour
	ageDir(t, justOver, threshold+time.Minute)
	ageDir(t, justUnder, threshold-time.Minute)

	removed, err := GCLegacyScratchResidueOlderThan(threshold)
	if err != nil {
		t.Fatalf("GCLegacyScratchResidueOlderThan: %v", err)
	}
	if removed != 1 {
		t.Fatalf("sweep removed %d entries at threshold %v, want exactly 1", removed, threshold)
	}
	if _, err := os.Stat(justOver); !os.IsNotExist(err) {
		t.Fatalf("%s (threshold+1m) was not collected: %v", justOver, err)
	}
	if _, err := os.Stat(justUnder); err != nil {
		t.Fatalf("%s (threshold-1m) was collected — the age comparison is off: %v", justUnder, err)
	}
	// And the exported default form must use the ONE documented threshold, not
	// a second literal (Rule 13/14).
	if _, err := GCLegacyScratchResidue(); err != nil {
		t.Fatalf("GCLegacyScratchResidue: %v", err)
	}
}

// TestHostLegacyResidueIsSweptByTheSuitePin states the POST-CONDITION on the
// real host: this suite's TestMain pinned the scratch root, which runs the
// legacy sweep inline, so no legacy root older than the threshold may still be
// in the host's memory base. Residue YOUNGER than the threshold is expected
// (a concurrent suite's dead run) and is only reported.
func TestHostLegacyResidueIsSweptByTheSuitePin(t *testing.T) {
	if _, err := os.Stat(MemoryBaseDefault); err != nil {
		t.Skipf("%s is absent on this host", MemoryBaseDefault)
	}
	if shares, err := ScratchRootSharesRootFS(MemoryBaseDefault); err != nil || shares {
		t.Skipf("%s is not a separate filesystem from / here — the sweep is banned there by design", MemoryBaseDefault)
	}
	entries, err := os.ReadDir(MemoryBaseDefault)
	if err != nil {
		t.Skipf("cannot read %s: %v", MemoryBaseDefault, err)
	}
	aged := 0
	young := 0
	for _, entry := range entries {
		if !IsLegacyScratchResidue(entry.Name()) {
			continue
		}
		path := filepath.Join(MemoryBaseDefault, entry.Name())
		fi, err := os.Lstat(path)
		if err != nil || !fi.IsDir() {
			continue
		}
		if time.Since(fi.ModTime()) > ScratchStaleAge {
			// A live holder may legitimately own an aged root for the duration
			// of its run; report it rather than fail (the holder rule wins).
			//stylegate:ignore holder probe in a fixture; the returned holder is the assertion
			if held, _ := ScratchDirHolder(path); held != "" {
				young++
				continue
			}
			t.Errorf("aged legacy scratch residue %s survived the pin-inline sweep", path)
			aged++
			continue
		}
		young++
	}
	t.Logf("%s: %d aged legacy residue entries, %d younger (threshold %v) — the suite pin sweeps inline",
		MemoryBaseDefault, aged, young, ScratchStaleAge)
}
