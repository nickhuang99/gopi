package scratch

// The DEFAULT temp root's tests (tmpdir default epic 2026-09-16; re-pointed at
// the workspace by the "make tmpdir an ordinary parameter" epic, same day).
//
// WHAT IS BEING PINNED: gopi publishes a temp root of ITS OWN when no channel
// names one — an ORDINARY directory under the workspace (<home>/workspace/_tmp),
// created on demand. The default used to be the scratch home's `tmp` child,
// which on this host is reached through a symlink into /dev/shm: a tuned,
// test-oriented location for what is really the operator's working space.
//
// THE TESTS SET THE PARAMETER, THEY DO NOT ASSERT A HOST. Every case drives the
// resolution with the knob it is about (a configured value, a fixture workspace,
// an unwritable parent) so the suite behaves the same on a host whose workspace
// lives somewhere else. Nothing here asserts a device id: the /-partition
// question is closed and its mechanical half (AssertPathOffRootFS) is tested by
// its own case below — what these cases pin is RESOLUTION and REFUSAL.
//
// THE NEGATIVE CASES ARE LOAD-BEARING. TestResolvePublishedTmpDirFailsLoudlyOnA
// ReadOnlyParent drives a REAL unwritable parent and asserts that the failure
// names the path, the reason AND the channel; a nil-returning or silently
// falling-back implementation fails it (Rule 13: an assertion never seen failing
// is not an assertion).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/worktreeroots"
)

// defaultTmpRootFixturePrefix names the fixture directories this file creates
// under the ambient temp root. Named (Rule 1b) so the cleanup can recognise
// exactly its own entries.
const defaultTmpRootFixturePrefix = "gopi-tmproot-fixture-"

// readonlyParentPerm makes a directory unwritable-but-traversable: the child
// path can be STAT'ed but not created inside it. A named constant (Rule 1a)
// because the mode IS the case under test.
const readonlyParentPerm = 0o500

// withFixtureWorkspace points the workspace at a fixture directory and returns
// it — the way every case here drives the default WITHOUT touching the
// operator's real workspace (Rule 17: t.Setenv, restored automatically).
func withFixtureWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(WorkspaceHomeEnvName, home)
	return CanonicalPath(filepath.Join(home, WorkspaceDirName))
}

// TestDefaultTmpDirIsAnOrdinaryDirectoryUnderTheWorkspace is the epic's
// acceptance for the default: with nothing configured, the published temp root
// is <home>/workspace/_tmp — the operator's stated working space, NOT the
// scratch home's symlinked child.
func TestDefaultTmpDirIsAnOrdinaryDirectoryUnderTheWorkspace(t *testing.T) {
	workspace := withFixtureWorkspace(t)

	dir, err := DefaultTmpDir()
	if err != nil {
		t.Fatalf("DefaultTmpDir() failed: %v", err)
	}
	want := CanonicalPath(filepath.Join(workspace, DefaultTmpDirLeaf))
	if dir != want {
		t.Fatalf("the default temp root is %q, want the ordinary workspace directory %q", dir, want)
	}
	if filepath.Dir(dir) != workspace {
		t.Fatalf("the default temp root %q does not sit directly under the workspace %q — the default is the workspace's own directory", dir, workspace)
	}
	if filepath.Base(dir) != DefaultTmpDirLeaf {
		t.Fatalf("the default temp root %q has leaf %q, want %q", dir, filepath.Base(dir), DefaultTmpDirLeaf)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the default temp root %q was not created: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("the default temp root %q exists but is not a directory", dir)
	}
	// It is PROVEN WRITABLE, not merely present: the probe in ensureUsableTempRoot	// ran during resolution — this asserts the effect, so a future refactor that
	// drops the probe fails here.
	probe, err := os.CreateTemp(dir, tmpDirProbeFilePattern)
	if err != nil {
		t.Fatalf("the default temp root %q is not writable: %v", dir, err)
	}
	name := probe.Name()
	if cerr := probe.Close(); cerr != nil {
		t.Logf("close %s: %v", name, cerr)
	}
	if rerr := os.Remove(name); rerr != nil {
		t.Logf("remove %s: %v", name, rerr)
	}
	t.Logf("default temp root %s (workspace %s)", dir, workspace)
}

// TestResolvePublishedTmpDirUsesTheConfiguredValue: a configured value is used
// EXACTLY as given (it is the operator's decision), canonicalised — and the
// source label is carried so a refusal can say which channel supplied it.
func TestResolvePublishedTmpDirUsesTheConfiguredValue(t *testing.T) {
	withFixtureWorkspace(t)
	target := filepath.Join(t.TempDir(), "configured-tmp")
	const source = "from the test"

	dir, _, err := ResolvePublishedTmpDir(target, source)
	if err != nil {
		t.Fatalf("ResolvePublishedTmpDir(%q) failed: %v", target, err)
	}
	if dir != CanonicalPath(target) {
		t.Fatalf("the resolved temp root is %q, want the configured %q", dir, CanonicalPath(target))
	}
}

// TestResolvePublishedTmpDirFailsLoudlyOnAReadOnlyParent is the D21 acceptance:
// a temp root that cannot be provided is a LOUD failure naming the path and the
// reason — never a silent fallback to the ambient root. The refusal must also
// carry the CHANNEL that supplied the value, so the operator knows which knob to
// fix.
func TestResolvePublishedTmpDirFailsLoudlyOnAReadOnlyParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a 0500 parent is still writable, so the unwritable case cannot be constructed")
	}
	withFixtureWorkspace(t)
	parent := filepath.Join(t.TempDir(), "readonly-parent")
	if err := os.MkdirAll(parent, readonlyParentPerm); err != nil {
		t.Fatalf("prepare the read-only parent: %v", err)
	}
	t.Cleanup(func() {
		// Restore write permission so the framework can remove the tree.
		if cerr := os.Chmod(parent, DirPerm); cerr != nil {
			t.Logf("could not restore %s: %v", parent, cerr)
		}
	})
	target := filepath.Join(parent, "cannot-be-created")
	const source = "from --tmpdir"

	dir, _, err := ResolvePublishedTmpDir(target, source)
	if err == nil {
		t.Fatalf("ResolvePublishedTmpDir(%q) returned %q — an uncreatable temp root must FAIL, never fall back silently", target, dir)
	}
	if !errors.Is(err, ErrTmpDirUnusable) {
		t.Fatalf("the refusal does not wrap ErrTmpDirUnusable (got %v) — a caller cannot match the cause", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, target) {
		t.Fatalf("the refusal does not name the PATH %q: %s", target, msg)
	}
	if !strings.Contains(msg, "cannot create it") {
		t.Fatalf("the refusal does not name the REASON: %s", msg)
	}
	if !strings.Contains(msg, source) {
		t.Fatalf("the refusal does not name the CHANNEL %q: %s", source, msg)
	}
	t.Logf("refused loudly: %s", msg)
}

// TestResolvePublishedTmpDirRefusesAFile: a path that exists but is not a
// directory is refused with its own reason — the "provided" half of D21.
func TestResolvePublishedTmpDirRefusesAFile(t *testing.T) {
	withFixtureWorkspace(t)
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), FilePerm); err != nil {
		t.Fatalf("prepare the file: %v", err)
	}

	_, _, err := ResolvePublishedTmpDir(file, "from the settings key "+TmpDirSettingsKey)
	if err == nil {
		t.Fatalf("ResolvePublishedTmpDir(%q) accepted a FILE as the temp workspace", file)
	}
	if !errors.Is(err, ErrTmpDirUnusable) {
		t.Fatalf("the refusal does not wrap ErrTmpDirUnusable (got %v)", err)
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("the refusal does not name the reason: %v", err)
	}
	t.Logf("refused loudly: %v", err)
}

// TestDefaultTmpDirFailsLoudlyWhenTheWorkspaceCannotExist: the DEFAULT's refusal
// is just as loud and names the fact that no channel configured it — the run
// continues (the caller decides), but nothing is silent (Rule 15).
func TestDefaultTmpDirFailsLoudlyWhenTheWorkspaceCannotExist(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a 0500 parent is still writable, so the unwritable case cannot be constructed")
	}
	home := t.TempDir()
	t.Setenv(WorkspaceHomeEnvName, home)
	// The workspace directory itself pre-created READ-ONLY: creating the temp
	// leaf inside it must fail.
	workspace := filepath.Join(home, WorkspaceDirName)
	if err := os.MkdirAll(workspace, readonlyParentPerm); err != nil {
		t.Fatalf("prepare the read-only workspace: %v", err)
	}
	t.Cleanup(func() {
		if cerr := os.Chmod(workspace, DirPerm); cerr != nil {
			t.Logf("could not restore %s: %v", workspace, cerr)
		}
	})

	dir, err := DefaultTmpDir()
	if err == nil {
		t.Fatalf("DefaultTmpDir() returned %q for a workspace that cannot be written — it must fail loudly", dir)
	}
	if !errors.Is(err, ErrTmpDirUnusable) {
		t.Fatalf("the default's refusal does not wrap ErrTmpDirUnusable (got %v)", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, TmpDirSourceNothingConfigured) {
		t.Fatalf("the default's refusal does not say that NO channel configured the value: %s", msg)
	}
	t.Logf("refused loudly: %s", msg)
}

// TestDefaultTmpDirIsIndependentOfTheScratchHome pins what the operator's
// decision changed: the default is the WORKSPACE's, so the scratch tree — which
// was reachable through a symlink into /dev/shm — no longer decides where an
// unconfigured gopi keeps its temp files.
func TestDefaultTmpDirIsIndependentOfTheScratchHome(t *testing.T) {
	workspace := withFixtureWorkspace(t)
	// A scratch home somewhere else entirely: it must not move the default.
	t.Setenv(HomeDirEnvName, filepath.Join(t.TempDir(), "elsewhere-scratch"))

	dir, err := DefaultTmpDir()
	if err != nil {
		t.Fatalf("DefaultTmpDir() failed: %v", err)
	}
	want := CanonicalPath(filepath.Join(workspace, DefaultTmpDirLeaf))
	if dir != want {
		t.Fatalf("the default temp root is %q, want %q — the scratch home must not decide the default any more", dir, want)
	}
}

// TestAssertPathOffRootFailsOnRootFS keeps the mechanical /-partition refusal
// covered: a path whose nearest existing ancestor shares `/`'s filesystem is
// REFUSED, with the sentinel wrapped, and no directory is created anywhere. The
// question is closed, but the mechanism is still the one internal/testutil and
// scripts/lib/tmp-ban.sh call — an uncovered mechanism rots.
func TestAssertPathOffRootFailsOnRootFS(t *testing.T) {
	requireAmbientOnRootFS(t)

	fixture, err := os.MkdirTemp(worktreeroots.AmbientTempRoot, defaultTmpRootFixturePrefix)
	if err != nil {
		t.Skipf("cannot create a fixture under %s: %v", worktreeroots.AmbientTempRoot, err)
	}
	t.Cleanup(func() {
		// Reclaim exactly this fixture, by path (Rule 22: never a recursive wipe
		// of the whole root).
		if rerr := os.RemoveAll(fixture); rerr != nil {
			t.Logf("could not reclaim the fixture %s: %v", fixture, rerr)
		}
	})

	// A NOT-YET-EXISTING path under it: the refusal must come from the nearest
	// existing ancestor, never from creating the directory first.
	pending := filepath.Join(fixture, WorkspaceDirName, RootDirName, TmpDirName)
	if _, statErr := os.Stat(pending); statErr == nil {
		t.Fatalf("%s already exists — this case must exercise a path that does not exist yet", pending)
	}
	err = AssertPathOffRootFS(pending)
	if err == nil {
		t.Fatalf("AssertPathOffRootFS(%q) accepted a temp root on the SAME filesystem as / (the fixture is under %s) — the refusal is blind",
			pending, worktreeroots.AmbientTempRoot)
	}
	if !errors.Is(err, ErrTempRootOnRootFS) {
		t.Fatalf("the refusal for %q does not wrap ErrTempRootOnRootFS (got %v) — a caller cannot match the cause", pending, err)
	}
	if _, statErr := os.Stat(pending); statErr == nil {
		t.Fatalf("the refusal CREATED %s — the check must run before anything is written, not after", pending)
	}
	t.Logf("refused (and created nothing): %v", err)
}

// requireAmbientOnRootFS skips a case whose whole point is that the ambient
// temp root SHARES `/`'s filesystem — on a host where /tmp is a separate
// filesystem there is no leak to demonstrate, and failing would be a false
// report (the same guard the sibling value test uses).
func requireAmbientOnRootFS(t *testing.T) {
	t.Helper()
	ambientDev, err := DeviceID(worktreeroots.AmbientTempRoot)
	if err != nil {
		t.Skipf("cannot measure %s: %v", worktreeroots.AmbientTempRoot, err)
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		t.Fatalf("cannot measure /: %v", err)
	}
	if ambientDev != rootDev {
		t.Skipf("%s is NOT on the / filesystem here (device %d vs %d) — there is no ambient leak to demonstrate",
			worktreeroots.AmbientTempRoot, ambientDev, rootDev)
	}
}
