package scratch

// declared_trees_spelling_test.go — THE REPRO for the holder index's SPELLING
// fail-open (heldSet/heldIn, declared_trees.go).
//
// THE DEFECT IT PINS, stated as the sweep experiences it: a declared tree whose
// ROOT the walk reaches through a symlinked spelling, plus a holder path that the
// kernel reports RESOLVED (which is always the case — /proc/<pid>/cwd is the
// kernel's own path). The two sides were compared lexically on ONE spelling of
// the root, so they never met: heldIn answered FALSE for the directory a live
// process was standing in, the arm's only liveness veto became a no-op, and the
// sweep fell through to age alone — a fail-open in a package whose contract is
// "every unknown is a KEEP".
//
// WHY THE FIXTURE IS SHAPED THIS WAY, one reason per half:
//
//   - The ROOT is reached through a LINK and the tree row is handed the LINKED
//     spelling, because that is the shape the defect needs: the walk's own root
//     and a /proc holder path are then two different strings for the same
//     directory on disk.
//   - The HOLDER is a real child process (`sleep`) standing in a directory BELOW
//     the candidate, started the way the legacy test starts one — the /proc
//     channel, never a hand-written string. A fixture that merely asserted
//     `heldSet` on invented paths would not prove the kernel really reports a
//     resolved path here, and the whole case rests on that fact: the test
//     therefore READS /proc/<pid>/cwd and asserts the two spellings really do
//     differ (see assertHolderSpellingDiffers).
//   - The CANDIDATE is a stale `go-build*` directory of the tree's own namespace
//     and the POSITIVE CONTROL is its sibling, equally stale, with nobody inside
//     it: the plan must name the sibling and refuse the held one. Without the
//     control the test would pass for a plan that simply named nothing.
//
// The assertion is made on the PLAN face — the same predicate the sweep collects
// through (declaredTreeEntries → declaredEntryCollectable → heldIn), so the test
// pins the verdict an operator's `gopi tool scratch-gc trees --dry-run` reports
// and not a private helper's opinion.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// spellingHolderSleepSec is how long the holder child outlives its observation —
// comfortably more than any poll below, so the case never races the child's own
// life. It is a STRING because it is an argv element.
const spellingHolderSleepSec = "120"

// spellingFixture is one planted declared tree whose root is reached through a
// symlink, with its members named by role.
type spellingFixture struct {
	// literalRoot is the spelling the tree row RESOLVES to (through the link).
	literalRoot string
	// resolvedRoot is the same directory as the kernel spells it.
	resolvedRoot string
	// held is the stale `go-build*` directory a live process stands in.
	held string
	// free is the equally stale `go-build*` sibling nobody holds — the control.
	free string
}

// seedSpellingFixture plants the fixture above and backdates EVERYTHING past the
// window, so a KEEP the assertions observe is a KEEP the holder rule decided and
// never the age rule passing for it.
func seedSpellingFixture(t *testing.T) spellingFixture {
	t.Helper()
	base := t.TempDir()
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "linked")
	if err := os.MkdirAll(real, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", real, err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink %s → %s: %v", link, real, err)
	}
	f := spellingFixture{
		literalRoot:  filepath.Join(link, "root"),
		resolvedRoot: filepath.Join(real, "root"),
	}
	f.held = filepath.Join(f.literalRoot, GoBuildDirEntryPrefix+"-held")
	f.free = filepath.Join(f.literalRoot, GoBuildDirEntryPrefix+"-free")
	for _, dir := range []string{f.held, f.free} {
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	aged := time.Now().Add(-(GoTmpStaleAge + 24*time.Hour))
	for _, path := range []string{f.literalRoot, f.held, f.free} {
		if err := os.Chtimes(path, aged, aged); err != nil {
			t.Fatalf("backdate %s: %v", path, err)
		}
	}
	return f
}

// spellingTree is the fixture's declared row: a GOTMPDIR-shaped tree whose
// Resolve hands back the LINKED spelling — exactly what a scratch home reached
// through a link produces.
func spellingTree(f spellingFixture) DeclaredTree {
	return DeclaredTree{
		Name:       TreeNameGoTmp,
		Class:      TreeClassGoTmp,
		Kind:       EntryKindDir,
		StaleAge:   GoTmpStaleAge,
		EntryMatch: hasNamePrefix(GoBuildDirEntryPrefix),
		Resolve:    func() (string, error) { return f.literalRoot, nil },
	}
}

// assertHolderSpellingDiffers reads /proc/<pid>/cwd and fails unless the kernel
// really reports the RESOLVED spelling while the tree is walked under the LITERAL
// one. It is the premise of the whole repro: without it the test could pass for
// the wrong reason (two spellings that happen to be equal).
func assertHolderSpellingDiffers(t *testing.T, pid int, f spellingFixture) string {
	t.Helper()
	raw, err := os.Readlink(filepath.Join("/proc", itoa(pid), ProcCwdName))
	if err != nil {
		t.Fatalf("read /proc/%d/cwd: %v", pid, err)
	}
	got := filepath.Clean(raw)
	if !strings.HasPrefix(got, f.resolvedRoot+string(filepath.Separator)) {
		t.Fatalf("the kernel reports the holder's cwd as %s, which is not inside the RESOLVED root %s — the fixture is not in the spelling-divergent shape this test exists for", got, f.resolvedRoot)
	}
	if strings.HasPrefix(got, f.literalRoot+string(filepath.Separator)) {
		t.Fatalf("the kernel reports the holder's cwd as %s, inside the LITERAL root %s — the two spellings coincide here, so nothing about the spelling fail-open is being tested", got, f.literalRoot)
	}
	return got
}

// itoa is strconv.Itoa under a local name, so this file's imports stay the small
// set it needs.
func itoa(n int) string { return strconv.Itoa(n) }

// TestHeldSetIsSpellingAgnosticAcrossSymlinkedRoot is THE REGRESSION: a stale
// candidate a live process stands in, on a tree reached through a link, is NOT
// planned — while its equally stale, unheld sibling IS. Before the fix the first
// half failed (the index held no key for either spelling of the held directory)
// and the candidate was handed to the sweep.
func TestHeldSetIsSpellingAgnosticAcrossSymlinkedRoot(t *testing.T) {
	f := seedSpellingFixture(t)
	holder := exec.Command("sleep", spellingHolderSleepSec)
	// Pdeathsig: the deferred killHolder covers a normal end only. The child
	// must also die when the binary does, or it holds the tree it was planted in.
	holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	holder.Dir = f.held
	if err := holder.Start(); err != nil {
		t.Fatalf("start the holder: %v", err)
	}
	killHolder := func() {
		_ = holder.Process.Kill()    //stylegate:ignore kills the holder this fixture spawned; a failed kill means it had already exited
		_, _ = holder.Process.Wait() //stylegate:ignore reaps the killed holder so the fixture leaves no zombie
	}
	defer killHolder()
	// The kernel-level truth first: the holder is really standing there, spelled
	// resolved while the tree is walked under the link.
	if !legacyWaitForCwd(holder.Process.Pid, f.held) {
		t.Fatalf("the holder never appeared in %s within %v — a fork/exec under a loaded suite can take seconds, and the case means nothing unless the process is really standing there", f.held, legacyHolderObservationDeadline)
	}
	assertHolderSpellingDiffers(t, holder.Process.Pid, f)

	tree := spellingTree(f)
	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan %s: %v", tree.Name, err)
	}
	planned := make(map[string]bool, len(plan))
	for _, e := range plan {
		planned[filepath.Clean(e.Path)] = true
	}
	if planned[filepath.Clean(f.held)] || planned[filepath.Clean(filepath.Join(f.resolvedRoot, filepath.Base(f.held)))] {
		t.Errorf("the plan named %s although a live process (pid %d) stands in it — the holder index is keyed by one spelling only, so the resolved /proc path never meets the linked root and the arm's only liveness veto is a no-op (Rule 15: an unmeasured fact must not render as a measured negative)",
			f.held, holder.Process.Pid)
	}
	if !planned[filepath.Clean(f.free)] {
		t.Errorf("the plan did not name the unheld sibling %s — without it this test would pass for a plan that names nothing", f.free)
	}

	// The same verdict through the index itself, both spellings: the candidate as
	// the walk spells it (through the link) and as the kernel would spell it. Both
	// must read held, because they are the same directory.
	index := heldSet(ScanHolders(), f.literalRoot)
	for _, spelling := range []string{f.held, filepath.Join(f.resolvedRoot, filepath.Base(f.held))} {
		if !heldIn(spelling, index) {
			t.Errorf("heldIn(%s)=false — a candidate spelled %s is the directory the holder stands in and the index must hold a key for it", spelling, spelling)
		}
	}
	if heldIn(f.free, index) {
		t.Errorf("heldIn(%s)=true — the unheld sibling must not read as held", f.free)
	}
}
