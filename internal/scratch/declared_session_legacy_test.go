// declared_session_legacy_test.go — THE REPRO for the sessions tree's
// LEGACY-RESIDUE bound (declared_session_legacy.go).
//
// WHAT IT PINS, and why each half is a real acceptance rather than a restatement
// of the code:
//
//  1. SET EQUALITY between the two registries. The registry that DECIDES
//     (internal/sessionlayout) and the tree that COLLECTS (this package) must
//     agree about which entry is which family — not in prose, but entry by
//     entry: the plan's claim on a fixture entry must equal the registry's
//     `bound:` verdict on that same entry. Set equality in ONE direction is
//     easy and useless (a tree that claimed nothing would satisfy "everything
//     claimed is bound"); the fixture therefore carries the UNCLASSIFIED shapes
//     too, and the tree must refuse them.
//  2. THE SWEEP ITSELF, driven for real (Rule 31): the candidates go, the
//     controls stay, and the ROOT survives.
//  3. THE TWO SAFETY GATES ARE REAL GATES: a live process standing in a
//     container shields everything below it, and a `.holder` registration
//     shields a directory at ANY age — both asserted on a container that IS
//     old enough, so the assertion cannot pass by the age rule.
//  4. THE LIVE TREE IS READ, NEVER TOUCHED: the census half runs the PLAN face
//     only, against the operator's real sessions tree, and asserts what the
//     window actually does there.
package scratch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// legacyFixture is one planted sessions tree, with its members named by role.
type legacyFixture struct {
	root string

	// The container families.
	sessionContainer string
	taskEpochDir     string
	processDir       string
	// The stray-dir family (an exact shape from the registry's table) and the
	// residue-file family.
	strayDir  string
	strayFile string
	// The unclassified shapes: a root-level directory and file NO family
	// claims. They are the under-bound control.
	unknownDir  string
	unknownFile string
	// Inside a session container: a probe's checkout and a private profile.
	checkout string
	profile  string
}

// seedLegacyFixture plants one member per family into a fresh sessions tree and
// backdates EVERYTHING past the legacy window, so every KEEP the assertions
// observe is a KEEP the namespace or the holder rule decided — never the age
// rule passing for it.
func seedLegacyFixture(t *testing.T) legacyFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "sessions")
	f := legacyFixture{
		root:             root,
		sessionContainer: filepath.Join(root, "01a0f522-134a-75ae-9865-54d9756c26d4"),
		taskEpochDir:     filepath.Join(root, "t-1788897004"),
		processDir:       filepath.Join(root, "process-987654"),
		strayDir:         filepath.Join(root, "zz-live-probe"),
		strayFile:        filepath.Join(root, "note.txt"),
		unknownDir:       filepath.Join(root, "brand-new-family"),
		unknownFile:      filepath.Join(root, "unheard-of-artifact.bin"),
	}
	f.checkout = filepath.Join(f.sessionContainer, "smoke-clone")
	f.profile = filepath.Join(f.sessionContainer, "sandbox-home")

	for _, dir := range []string{
		f.sessionContainer, f.taskEpochDir, f.processDir, f.strayDir, f.unknownDir,
		filepath.Join(f.checkout, sessionlayout.GitMarkerName),
		filepath.Join(f.profile, sessionlayout.SandboxProfileMarkerName),
	} {
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// The session record is what keeps the container family's own question
	// honest: it is a KEEP, and the container must not be removed while it is
	// there.
	if err := os.WriteFile(filepath.Join(f.sessionContainer, sessionlayout.RecordName), []byte("{}\n"), FilePerm); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{f.strayFile, f.unknownFile} {
		if err := os.WriteFile(file, []byte("residue\n"), FilePerm); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	aged := time.Now().Add(-(SessionLegacyContainerStaleAge + 24*time.Hour))
	for _, path := range []string{
		f.root, f.sessionContainer, f.taskEpochDir, f.processDir, f.strayDir, f.unknownDir,
		f.checkout, f.profile, f.strayFile, f.unknownFile,
		filepath.Join(f.checkout, sessionlayout.GitMarkerName),
		filepath.Join(f.profile, sessionlayout.SandboxProfileMarkerName),
	} {
		if err := os.Chtimes(path, aged, aged); err != nil {
			t.Fatalf("backdate %s: %v", path, err)
		}
	}
	return f
}

// withSessionsRoot points the declared trees at root for the duration of the
// test, restoring the real resolver afterwards. It is the seam itself
// (RegisterDataRootResolvers assigns the same variable), so the test drives the
// PRODUCTION resolution path — no second way of finding a tree is introduced
// for the test's convenience.
func withSessionsRoot(t *testing.T, root string) {
	t.Helper()
	prev := sessionsDirResolver
	sessionsDirResolver = func() (string, error) { return root, nil }
	t.Cleanup(func() { sessionsDirResolver = prev })
}

const (
	// legacyHolderSleepSec is how long the holder child outlives its observation
	// — comfortably longer than the deadline below, so the case never races the
	// child's own life.
	legacyHolderSleepSec = "120"
	// legacyHolderObservationDeadline bounds how long the test waits for the
	// child to be observable. It is generous ON PURPOSE: a fork/exec under a
	// loaded parallel suite can take seconds, and a tight deadline turns a
	// correct implementation into a flaky test (measured: 2s was enough alone,
	// not enough inside `make test-budget`).
	legacyHolderObservationDeadline = 60 * time.Second
	// legacyHolderPollInterval is the poll period while waiting for it.
	legacyHolderPollInterval = 25 * time.Millisecond
)

// legacyWaitForCwd waits until a live process's working directory IS dir, read
// from the kernel (/proc/<pid>/cwd) — the cheap per-pid truth, polled — so the
// wait never runs the full /proc scan in a loop.
func legacyWaitForCwd(pid int, dir string) bool {
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = dir
	}
	deadline := time.Now().Add(legacyHolderObservationDeadline)
	for time.Now().Before(deadline) {
		if got, lerr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd")); lerr == nil {
			if resolved, rerr := filepath.EvalSymlinks(got); rerr == nil {
				got = resolved
			}
			if got == want {
				return true
			}
		}
		time.Sleep(legacyHolderPollInterval) //stylegate:allow-sleep bounded poll of ANOTHER process's /proc cwd; there is no channel to be notified on its change
	}
	return false
}

// legacyHolderAliveIn reports whether a live process's working directory is
// inside dir — the /proc channel of the holder verdict, read through the
// package's own one pass (LiveHolders + heldSet), never a second spelling.
func legacyHolderAliveIn(dir string) bool {
	return heldIn(filepath.Clean(dir), heldSet(ScanHolders(), dir))
}

// legacyTrees returns the three rows by name, so a test that named a row the set
// no longer holds fails loudly instead of silently testing nothing.
func legacyTrees(t *testing.T) []DeclaredTree {
	t.Helper()
	want := SessionLegacyTreeNames()
	var out []DeclaredTree
	for _, tree := range DeclaredTrees() {
		for _, name := range want {
			if tree.Name == name {
				out = append(out, tree)
			}
		}
	}
	if len(out) != len(want) {
		t.Fatalf("the declared set holds %d of the %d legacy rows (%v) — a bound naming a tree that does not exist bounds nothing",
			len(out), len(want), want)
	}
	return out
}

// planPaths returns the plan's candidate paths for one row, as a set.
func planPaths(t *testing.T, tree DeclaredTree) map[string]bool {
	t.Helper()
	plan, err := DeclaredTreePlan(tree)
	if err != nil {
		t.Fatalf("plan %s: %v", tree.Name, err)
	}
	out := make(map[string]bool, len(plan))
	for _, e := range plan {
		out[filepath.Clean(e.Path)] = true
	}
	return out
}

// TestSessionLegacyTreeClaimsExactlyTheBoundFamilies is the SET-EQUALITY gate
// between the deciding registry and the collecting tree, in BOTH directions.
func TestSessionLegacyTreeClaimsExactlyTheBoundFamilies(t *testing.T) {
	f := seedLegacyFixture(t)
	withSessionsRoot(t, f.root)

	rows := map[string]map[string]bool{}
	for _, tree := range legacyTrees(t) {
		rows[tree.Name] = planPaths(t, tree)
	}
	dirs := rows[TreeNameSessionLegacyDirs]
	files := rows[TreeNameSessionLegacyFiles]
	containers := rows[sessionlayout.TreeSessionLegacyContainers]

	// THE CLAIMED DIRECTION: every family the registry says `bound:` at the root
	// is claimed by the directory row.
	for _, want := range []string{f.taskEpochDir, f.processDir, f.strayDir} {
		if !dirs[filepath.Clean(want)] {
			t.Errorf("the registry binds this directory's family and the tree does not claim it: %s (claimed: %v)", want, dirs)
		}
	}
	if !files[filepath.Clean(f.strayFile)] {
		t.Errorf("the registry binds the root-residue FILE family and the tree does not claim %s (claimed: %v)", f.strayFile, files)
	}
	for _, want := range []string{f.checkout, f.profile} {
		if !containers[filepath.Clean(want)] {
			t.Errorf("the registry binds this container-level family and the tree does not claim it: %s (claimed: %v)", want, containers)
		}
	}

	// THE UNDER-BOUND DIRECTION — the half that makes the gate a gate. An entry
	// no family claims must stay UNCLAIMED, however old it is: a tree that swept
	// "anything at the root" would pass the direction above and take a family
	// nobody has decided about.
	for _, path := range []string{f.unknownDir, f.unknownFile} {
		clean := filepath.Clean(path)
		if dirs[clean] || files[clean] || containers[clean] {
			t.Errorf("%s is UNCLAIMED by every registered family and was claimed by a legacy row — the bound must be the registry's membership test, never a catch-all", path)
		}
	}

	// And the registry really does leave it unclassified (so the two halves of
	// this assertion are the same fact, not two opinions).
	scan, err := sessionlayout.ScanFamilies(f.root)
	if err != nil {
		t.Fatalf("scan the fixture: %v", err)
	}
	unclassified := map[string]bool{}
	for _, u := range scan.Unclassified {
		unclassified[filepath.Clean(u.Path)] = true
	}
	for _, path := range []string{f.unknownDir, f.unknownFile} {
		if !unclassified[filepath.Clean(path)] {
			t.Errorf("%s is not registered in the family registry either — the control must be a shape NO family knows, or it proves nothing", path)
		}
	}
}

// TestSessionLegacySweepCollectsTheBoundResidueAndKeepsTheRest is the real run
// (Rule 31): the three rows' SWEEP faces, on the fixture, with every control
// named.
func TestSessionLegacySweepCollectsTheBoundResidueAndKeepsTheRest(t *testing.T) {
	f := seedLegacyFixture(t)
	withSessionsRoot(t, f.root)

	removed := 0
	for _, tree := range legacyTrees(t) {
		n, _, err := SweepDeclaredTree(tree)
		if err != nil {
			t.Fatalf("sweep %s: %v", tree.Name, err)
		}
		t.Logf("sweep: tree=%s removed=%d", tree.Name, n)
		removed += n
	}
	// 2 task-named containers + 1 stray directory, 1 residue file, and BOTH
	// marker directories (the `.git` checkout and the `.gopi` profile): the
	// per-row split is logged above, so a change in which ROW claims what is
	// visible in this run's own output rather than only in this total.
	if removed != 6 {
		t.Errorf("removed=%d, want 6 (3 root dirs + 1 root file + 2 container-level marker dirs) — the identities below are the contract", removed)
	}

	for _, gone := range []string{f.taskEpochDir, f.processDir, f.strayDir, f.strayFile, f.profile, f.checkout} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s survived the legacy sweep (err=%v)", gone, err)
		}
	}
	// The ROOT is the invariant this package exists to keep: a row rooted at the
	// sessions tree must never remove the sessions tree.
	for _, survives := range []string{f.root, f.sessionContainer, f.unknownDir, f.unknownFile} {
		if _, err := os.Stat(survives); err != nil {
			t.Errorf("%s did not survive the sweep: %v", survives, err)
		}
	}
}

// TestSessionLegacyContainerRowIsGatedByTheContainer pins the one-level-down
// rule with BOTH gates, on a container that is old enough to be collected — so
// neither assertion can pass because of the age rule.
func TestSessionLegacyContainerRowIsGatedByTheContainer(t *testing.T) {
	f := seedLegacyFixture(t)
	withSessionsRoot(t, f.root)
	row := legacyTrees(t)
	if len(row) != 3 {
		t.Fatalf("want the three legacy rows, got %d", len(row))
	}
	var container DeclaredTree
	for _, tree := range row {
		if tree.Name == sessionlayout.TreeSessionLegacyContainers {
			container = tree
		}
	}
	if container.Name == "" {
		t.Fatal("the container row is missing from the declared set")
	}

	// GATE 1 — a LIVE PROCESS inside the container. A child process whose
	// working directory is the container is the exact shape the /proc verdict
	// reads (a separate process, never this one: a directory this test's own
	// process stands in would be shielded by the harness's own cwd).
	holder := exec.Command("sleep", legacyHolderSleepSec)
	// Pdeathsig, not only the teardown below: a SIGKILLed test binary runs no
	// cleanup, and a holder standing in the container would keep the /proc
	// verdict reading IN USE for a run that is long gone.
	holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	holder.Dir = f.sessionContainer
	if err := holder.Start(); err != nil {
		t.Fatalf("start the holder: %v", err)
	}
	killHolder := func() {
		_ = holder.Process.Kill()    //stylegate:ignore kills the legacy-declaration holder this fixture spawned; a failed kill means it had already exited
		_, _ = holder.Process.Wait() //stylegate:ignore reaps the killed holder so the fixture leaves no zombie
	}
	if !legacyWaitForCwd(holder.Process.Pid, f.sessionContainer) {
		killHolder()
		t.Fatalf("the holder never appeared in %s within %v — fork/exec under a loaded suite is slower than this test's patience, and the case means nothing unless the process is really standing there", f.sessionContainer, legacyHolderObservationDeadline)
	}
	// The kernel-level truth is in place; the package's OWN verdict must agree
	// with it, read through the same one pass (LiveHolders + heldSet).
	if !legacyHolderAliveIn(f.sessionContainer) {
		killHolder()
		t.Fatalf("a live process with cwd=%s is visible in /proc and the package's holder verdict does not see it — the two must agree", f.sessionContainer)
	}
	plan, err := DeclaredTreePlan(container)
	if err != nil {
		_ = holder.Process.Kill() //stylegate:ignore kills the second holder of the ancestry case after its reading was taken
		t.Fatalf("plan %s: %v", container.Name, err)
	}
	for _, e := range plan {
		if filepath.Clean(e.Path) == filepath.Clean(f.checkout) {
			t.Errorf("%s was planned while a live process stood in its container — a container in use shields everything below it", f.checkout)
		}
	}
	if _, _, serr := SweepDeclaredTree(container); serr != nil {
		_ = holder.Process.Kill() //stylegate:ignore kills the third holder, whose /proc reading is the assertion
		t.Fatalf("sweep %s: %v", container.Name, serr)
	}
	if _, err := os.Stat(f.checkout); err != nil {
		_ = holder.Process.Kill() //stylegate:ignore kills the last holder on the fixture's exit path
		t.Fatalf("the checkout was removed while its container was held: %v", err)
	}
	killHolder()

	// GATE 2 — a `.holder` registration naming a live process: the WRITER-owned
	// channel of the same verdict, and the one that must hold at ANY age. This
	// process is alive by definition, and the checkout is already backdated past
	// every window, so only the registration can keep it.
	if err := WriteOwnerRecord(f.checkout, os.Getpid()); err != nil {
		t.Fatalf("register the holder: %v", err)
	}
	if !OwnerKeeps(f.checkout) {
		t.Fatalf("the registration of a live process did not keep %s — the two channels must agree about a live owner", f.checkout)
	}
	plan, err = DeclaredTreePlan(container)
	if err != nil {
		t.Fatalf("plan after registration: %v", err)
	}
	for _, e := range plan {
		if filepath.Clean(e.Path) == filepath.Clean(f.checkout) {
			t.Errorf("%s was planned despite a live `.holder` registration — a registered owner keeps it at ANY age", f.checkout)
		}
	}
	if _, _, serr := SweepDeclaredTree(container); serr != nil {
		t.Fatalf("sweep after registration: %v", serr)
	}
	if _, err := os.Stat(f.checkout); err != nil {
		t.Fatalf("the checkout was removed although its `.holder` names a live owner — the registration must keep it at ANY age: %v", err)
	}
}

// TestSessionLegacyLiveTreeCensus is the LIVE half: the operator's real sessions
// tree, READ ONLY. It runs the plan face (no delete, no mkdir, nothing) and it
// reports what the window does there.
//
// IT ASSERTS THE PROPERTY, NOT THE CONTENTS. A live tree is the operator's
// state, so a count pinned here would fail the day they park a file — what is
// asserted is the two facts a reader of this bound must be able to rely on:
// every planned candidate is a member of a family the registry really binds,
// and the plan touched nothing (the tree's entry count is identical before and
// after). The COUNTS are reported through t.Logf for the run's own record.
func TestSessionLegacyLiveTreeCensus(t *testing.T) {
	if os.Getenv(HomeEnvName) != "" {
		// This suite pins a scratch HOME; the live case is defined on the REAL
		// tree, and running it against a sandbox would report a census of the
		// sandbox as if it were the operator's.
		t.Skipf("%s is set (a sandboxed suite): the live census is defined on the real sessions tree", HomeEnvName)
	}
	root, err := SessionRawLogRoot()
	if err != nil {
		t.Skipf("the sessions tree is not configured on this host: %v", err)
	}
	before, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("the sessions tree is not readable here: %v", err)
	}

	scan, err := sessionlayout.ScanFamilies(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	bound := map[string]bool{}
	for _, name := range sessionlayout.BoundTreeNames() {
		bound[name] = true
	}
	for shape, s := range scan.ByShape {
		if s.Family == "" {
			continue
		}
		fam, ok := sessionlayout.FamilyByName(s.Family)
		if ok && bound[fam.Tree] {
			t.Logf("live census: family=%s shape=%q count=%d dir=%v", s.Family, shape, s.Count, s.Dir)
		}
	}
	t.Logf("live census: %d directories, %d files, %d bytes, %d unclassified entries",
		scan.Dirs, scan.Files, scan.Bytes, len(scan.Unclassified))

	for _, tree := range legacyTrees(t) {
		plan, perr := DeclaredTreePlan(tree)
		if perr != nil {
			t.Fatalf("plan %s on the live tree: %v", tree.Name, perr)
		}
		var bytes int64
		for _, e := range plan {
			bytes += e.Size
			rel, rerr := filepath.Rel(root, e.Path)
			if rerr != nil {
				t.Fatalf("candidate %s is not below the tree: %v", e.Path, rerr)
			}
			top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
			fam, ok := sessionlayout.ClassifyFamily(sessionlayout.FamilyEntryFor(filepath.Join(root, top), true, sessionlayout.WhereRoot))
			if !ok || fam.Decision != sessionlayout.DecisionBound {
				t.Errorf("the live plan of %s names %s, whose family the registry does not bind (classified=%v ok=%v) — the tree would collect an undecided family",
					tree.Name, e.Path, fam.Name, ok)
			}
		}
		t.Logf("live census: tree=%s would-remove=%d candidates bytes=%d", tree.Name, len(plan), bytes)
	}

	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("re-read %s: %v", root, err)
	}
	if len(after) != len(before) {
		t.Errorf("the plan face changed the tree: %d entries before, %d after — a report must have no side effects (nothing in this test writes)", len(before), len(after))
	}
}
