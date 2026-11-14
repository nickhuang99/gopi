// cli_scratch_gc_scratch_dumps_test.go — the CLI contract that closes CONFIG.md
// rows 655/656: the census must be able to REACH the dumps the error paths
// actually write on a DEFAULT configuration.
//
// THE DEFECT IT PINS (measured 2026-10-11). Both dump knobs document their
// default as the SCRATCH home's class directory
// (`internal/scratch.DumpDir(<class>)` — `<scratch home>/dumps/gopi-dumps`), but
// the census was advertised as `--tree dumps`, which is the PROFILE's `dumps/`
// tree and exists only when an operator exports the knob. On a default
// configuration that command exits 1 ("no such file or directory") while the
// dumps sit where no name could reach them. The test seeds the scratch class
// directory EXACTLY where the writers put it and requires the census to find it.
//
// The E2E execs the REAL built binary through testutil.RunGopi against a SANDBOX
// scratch home (both homes inside a t.TempDir), so it cannot touch the
// developer's own dumps tree.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// seedScratchDumpClass seeds one dump class directory under the SCRATCH home with
// one aged dump and one fresh one, and returns the class directory plus both
// paths. Ageing each ENTRY is what the row's window measures; the class directory
// itself is deliberately left fresh, because the class directory is not the tree
// the window applies to (its dumps are).
func seedScratchDumpClass(t *testing.T, scratchHome, class string) (dir, aged, fresh string) {
	t.Helper()
	dir = filepath.Join(scratchHome, scratch.DumpsDirName, class)
	if err := os.MkdirAll(dir, scratch.DirPerm); err != nil {
		t.Fatalf("seed dump class %s: %v", dir, err)
	}
	aged = filepath.Join(dir, "20260101-000000.dump.json")
	fresh = filepath.Join(dir, "20260102-000000.dump.json")
	for _, p := range []string{aged, fresh} {
		if err := os.WriteFile(p, []byte("{}\n"), scratch.FilePerm); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(aged, old, old); err != nil {
		t.Fatalf("age %s: %v", aged, err)
	}
	return dir, aged, fresh
}

// TestScratchGCFamiliesReachesTheScratchDumpClassTree is the regression test for
// the defect: on a default configuration the census name for the scratch class
// tree must exit 0 and report the class directory it walked, the family that
// claims the dumps and the decision taken about them.
func TestScratchGCFamiliesReachesTheScratchDumpClassTree(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	classDir, _, _ := seedScratchDumpClass(t, scratchHome, scratch.ScratchDumpsClassDirName)

	out, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCFamiliesFlag, scratchGCFamiliesTreeFlag, sessionlayout.ScratchDumpsTree},
		"", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("`scratch-gc %s %s %s` exited %d on a default configuration — this is the defect the tree exists to close.\nstdout:\n%s\nstderr:\n%s",
			scratchGCFamiliesFlag, scratchGCFamiliesTreeFlag, sessionlayout.ScratchDumpsTree, code, out, stderr)
	}
	for _, want := range []string{
		"tree=" + sessionlayout.ScratchDumpsTree,
		"root=" + classDir,
		"family=error-request-dump",
		"decision=" + string(sessionlayout.DecisionBound),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the census line does not carry %q — the tree, its root and the decision are the arm's whole claim.\nstdout:\n%s", want, out)
		}
	}
	// The class directory is NOT a declared root: the dumps inside it are the
	// candidates. A root that was itself declared collectable would remove the
	// whole class at once.
	if scratch.IsDeclaredTreeRoot(classDir) {
		t.Errorf("%s is reported as a declared ROOT", classDir)
	}
}

// TestScratchGCFamiliesListsBothDumpHomes is the enumeration half: `--list` is
// what makes the selector self-describing, and both dump homes must be on it with
// the root each one resolves — the profile's pair (where an EXPORTED knob points)
// and the scratch pair (the DOCUMENTED default). Without the second pair a reader
// reading CONFIG.md has no name to pass.
func TestScratchGCFamiliesListsBothDumpHomes(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	seedScratchDumpClass(t, scratchHome, scratch.ScratchDumpsClassDirName)

	out, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCFamiliesFlag, scratchGCFamiliesListFlag},
		"", scratchGCEnv(home, scratchHome), "")
	if code != 0 {
		t.Fatalf("`scratch-gc %s %s` exited %d\nstdout:\n%s\nstderr:\n%s", scratchGCFamiliesFlag, scratchGCFamiliesListFlag, code, out, stderr)
	}
	for _, tree := range sessionlayout.ScratchDumpsTreeNames() {
		if !strings.Contains(out, tree+" root=") {
			t.Errorf("--list does not name the tree %q — the name CONFIG.md prints would be guessable only", tree)
		}
	}
	// The scratch pair's roots are the WRITER's join (scratch home + dumps/ +
	// class), asserted by path rather than by name.
	for _, tc := range []struct{ tree, class string }{
		{sessionlayout.ScratchDumpsTree, scratch.ScratchDumpsClassDirName},
		{sessionlayout.ScratchDumpsTree1214, scratch.ScratchDumps1214ClassDirName},
	} {
		want := filepath.Join(scratchHome, scratch.DumpsDirName, tc.class)
		if !strings.Contains(out, tc.tree+" root="+want) {
			t.Errorf("--list does not resolve %q to %q — the census would walk a directory the writers never fill.\nstdout:\n%s", tc.tree, want, out)
		}
	}
}

// TestScratchGCFamiliesReportsTheAbsentProfileDumpTree is the OTHER half of the
// same contract, and it is deliberately NOT a failure: the profile dump tree's
// absence is the honest report that no knob pointed there. It is asserted so a
// later "fix" cannot make the profile pair resolve to the scratch tree (which
// would make the census answer a question the caller did not ask).
func TestScratchGCFamiliesReportsTheAbsentProfileDumpTree(t *testing.T) {
	home := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	seedScratchDumpClass(t, scratchHome, scratch.ScratchDumpsClassDirName)

	out, stderr, code := runGopiMapped(t,
		[]string{CliVerbScratchGC, scratchGCFamiliesFlag, scratchGCFamiliesTreeFlag, sessionlayout.SiblingTreeDumps},
		"", scratchGCEnv(home, scratchHome), "")
	if code == 0 {
		t.Fatalf("the profile dump tree reported success although no knob pointed there — an absent tree must never read as a collected one.\nstdout:\n%s", out)
	}
	profileRoot := filepath.Join(home, config.ProfileDirName, sessionlayout.SiblingTreeDumps)
	if !strings.Contains(stderr, profileRoot) {
		t.Errorf("the refusal does not name the root it could not walk (%s) — an operator cannot tell which tree was absent.\nstderr:\n%s", profileRoot, stderr)
	}
}
