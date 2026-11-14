package scratch

// mount_detach_test.go — the regression guard for the DEAD-FUSE-MOUNT residue.
//
// THE BUG (measured on this host, 2026-10-15): three scratch queue children were
// reported by every sweep as
//
//	openfdat …/queue-replay-4007787/runtime/gvfs: transport endpoint is not connected
//
// and reclaimed by none. The path is a `fuse.gvfsd-fuse` mount point whose
// daemon is gone; the kernel keeps the mount-table entry, so the directory
// cannot be removed at all and the tick re-reports it forever.
//
// WHY THE TEST DOES NOT MOUNT ANYTHING: a real dangling FUSE mount is produced
// by mounting a fuse filesystem and then killing its daemon — an act that
// changes what every other process on the host sees, on a SHARED host, from a
// test. It is refused for the same reason a test may not `dd` a device (Rule 27)
// and a test may not escalate privileges (Rule 25). So the two measurements that
// decide a detach — the mount table and the liveness probe — are injected, and
// the arm's DECISION is what gets pinned. The parser half, which needs no
// privilege, is exercised against the REAL table lines this host produced.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// realDeadGvfsMountLine is the mount-table row the defect was reported against,
// copied verbatim from this host's /proc/self/mounts on 2026-10-15. It is the
// fixture that makes the parser test mean something: a parser that cannot read
// THIS line reports "no mounts" for the exact case the arm exists for.
// staleFixtureAge is how far back the fixtures are dated: well past
// DefaultStaleAge, so the entry is unambiguously stale.
const staleFixtureAge = 3 * time.Hour

const realDeadGvfsMountLine = `gvfsd-fuse /home/user/workspace/gopi-test-scratch/queue/queue-replay-4007787/runtime/gvfs fuse.gvfsd-fuse rw,nosuid,nodev,relatime,user_id=1000,group_id=1000 0 0`

// realLiveGvfsMountLine is a gvfs mount whose daemon is alive (measured the same
// day, pid 1080553 on the same host) — the row the arm must NOT detach.
const realLiveGvfsMountLine = `gvfsd-fuse /home/user/workspace/gopi-test-scratch/queue/queue-replay-1080449/runtime/gvfs fuse.gvfsd-fuse rw,nosuid,nodev,relatime,user_id=1000,group_id=1000 0 0`

// foreignFuseMountLine is a mount of a DIFFERENT fuse type inside the same
// entry: not this system's to detach, whatever its state.
const foreignFuseMountLine = `sshfs /home/user/workspace/gopi-test-scratch/queue/queue-other-1/remote fuse.sshfs rw,nosuid,nodev 0 0`

// mountArmFixture installs injected seams for one test and restores the real
// ones on cleanup (LIFO, so a failure mid-test still restores).
type mountArmFixture struct {
	table  []mountPoint
	probed []string
	detach []string
	// probeErr / detachErr are returned for the target whose path CONTAINS the
	// key, so one fixture can hold a healthy mount, a dead one and an
	// unmeasurable one side by side.
	probeErr  map[string]error
	detachErr map[string]error
	// deadTargets names the mount points the injected probe reports as
	// measurably DEAD (daemon gone). When nil the probe falls back to a marker
	// in the path, which keeps the simple cases readable.
	deadTargets map[string]bool
	mu          sync.Mutex
}

func installMountArm(t *testing.T, table ...mountPoint) *mountArmFixture {
	t.Helper()
	f := &mountArmFixture{
		table:     table,
		probeErr:  map[string]error{},
		detachErr: map[string]error{},
	}
	prevTable, prevProbe, prevDetach := scratchMountTable, scratchMountProbe, scratchMountDetach
	scratchMountTable = func() ([]mountPoint, error) { return f.table, nil }
	scratchMountProbe = func(target string) (bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.probed = append(f.probed, target)
		for key, err := range f.probeErr {
			if strings.Contains(target, key) {
				return false, err
			}
		}
		if f.deadTargets != nil {
			return f.deadTargets[target], nil
		}
		return false, nil
	}
	scratchMountDetach = func(target string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.detach = append(f.detach, target)
		for key, err := range f.detachErr {
			if strings.Contains(target, key) {
				return err
			}
		}
		return nil
	}
	t.Cleanup(func() {
		scratchMountTable, scratchMountProbe, scratchMountDetach = prevTable, prevProbe, prevDetach
	})
	return f
}

func mp(target, fstype string) mountPoint {
	return mountPoint{Device: "gvfsd-fuse", Target: target, FSType: fstype}
}

// TestParseSelfMountsReadsTheRealDefectLine pins the parser against the exact
// line the host produced — the half of this arm that needs no privilege, and
// the half that would silently disarm it if it drifted.
func TestParseSelfMountsReadsTheRealDefectLine(t *testing.T) {
	got := parseSelfMounts(strings.Join([]string{
		"# a comment line the kernel never writes but a fixture might",
		realDeadGvfsMountLine,
		realLiveGvfsMountLine,
		"",
	}, "\n"))
	if len(got) != 2 {
		t.Fatalf("parsed %d rows, want 2: %+v", len(got), got)
	}
	const wantTarget = "/home/user/workspace/gopi-test-scratch/queue/queue-replay-4007787/runtime/gvfs"
	if got[0].Target != wantTarget {
		t.Fatalf("target = %q, want %q", got[0].Target, wantTarget)
	}
	if got[0].FSType != GvfsFuseFSType {
		t.Fatalf("fstype = %q, want %q", got[0].FSType, GvfsFuseFSType)
	}
}

// TestParseSelfMountsDecodesEscapedSpaces pins the escape handling: a target
// with a space arrives as `\040`, and a comparison that did not decode it would
// never match the path the caller holds.
func TestParseSelfMountsDecodesEscapedSpaces(t *testing.T) {
	got := parseSelfMounts(`gvfsd-fuse /tmp/a\040b/gvfs fuse.gvfsd-fuse rw 0 0`)
	if len(got) != 1 || got[0].Target != "/tmp/a b/gvfs" {
		t.Fatalf("parsed %+v, want one row targeting /tmp/a b/gvfs", got)
	}
}

// TestMountPointsUnderMatchesOnPathBoundaries pins the boundary: a sibling that
// merely shares a character prefix is not under the root. A prefix test without
// the separator would select a NEIGHBOUR's mount and detach it — the
// blast-radius shape this arm must never have.
func TestMountPointsUnderMatchesOnPathBoundaries(t *testing.T) {
	rows := []mountPoint{
		mp("/root/x/gvfs", GvfsFuseFSType),  // under
		mp("/root/xy/gvfs", GvfsFuseFSType), // NOT under: shares the prefix, not the path
		mp("/root/x", GvfsFuseFSType),       // the root itself
		mp("/elsewhere/gvfs", GvfsFuseFSType),
	}
	got := mountPointsUnder(rows, "/root/x")
	if len(got) != 2 {
		t.Fatalf("selected %+v, want exactly the root and its child", got)
	}
	for _, m := range got {
		if m.Target == "/root/xy/gvfs" {
			t.Fatalf("the sibling /root/xy was selected — the boundary is not enforced")
		}
	}
}

// TestDeadGvfsMountIsDetachedAndTheEntryCollected is the defect's own guard:
// a stale, unheld, in-namespace queue child carrying a dead gvfs mount point
// must be collected by the sweep (the detach is what makes the deletion
// possible), and the arm must have probed before it detached.
func TestDeadGvfsMountIsDetachedAndTheEntryCollected(t *testing.T) {
	home := t.TempDir()
	queue := filepath.Join(home, QueueDirName)
	run := filepath.Join(queue, runtimeEntryPrefix+RuntimeClassShot+"-424242")
	dead := filepath.Join(run, "runtime", "gvfs")
	if err := os.MkdirAll(dead, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", dead, err)
	}
	stale(t, run, queue)

	f := installMountArm(t, mp(dead, GvfsFuseFSType))
	f.deadTargets = map[string]bool{dead: true}
	// The real mount point cannot exist as a plain directory here (that is the
	// whole point), so the fixture's tree stands in for it and the injected
	// seams carry the measurements.
	removed, err := SweepHome(home, time.Hour)
	if err != nil {
		t.Fatalf("SweepHome: %v", err)
	}
	if len(f.probed) != 1 || f.probed[0] != dead {
		t.Fatalf("probed %v, want exactly [%s] — the arm must measure before it detaches", f.probed, dead)
	}
	if len(f.detach) != 1 || f.detach[0] != dead {
		t.Fatalf("detached %v, want exactly [%s]", f.detach, dead)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (the detach must make the entry collectable)", removed)
	}
	if _, err := os.Stat(run); !os.IsNotExist(err) {
		t.Fatalf("entry %s survived the sweep: %v", run, err)
	}
}

// TestLiveGvfsMountIsKept pins the veto: a mount whose daemon still answers is
// NOT residue and is never detached, even though the entry itself ages out.
// Detaching it would yank a filesystem out from under a live process.
func TestLiveGvfsMountIsKept(t *testing.T) {
	home := t.TempDir()
	queue := filepath.Join(home, QueueDirName)
	run := filepath.Join(queue, runtimeEntryPrefix+RuntimeClassShot+"-424243")
	live := filepath.Join(run, "runtime", "gvfs")
	if err := os.MkdirAll(live, DirPerm); err != nil {
		t.Fatal(err)
	}
	stale(t, run, queue)

	f := installMountArm(t, mp(live, GvfsFuseFSType)) // probe answers "reachable"
	if _, err := SweepHome(home, time.Hour); err != nil {
		t.Fatalf("SweepHome: %v", err)
	}
	if len(f.detach) != 0 {
		t.Fatalf("detached %v — a LIVE mount must never be detached", f.detach)
	}
}

// TestForeignFuseMountIsNeverDetached pins the ownership rule: only
// fuse.gvfsd-fuse is this sweep's to detach. Another fuse type inside the entry
// is reported and left alone — and, being live, it must also not be probed.
func TestForeignFuseMountIsNeverDetached(t *testing.T) {
	home := t.TempDir()
	queue := filepath.Join(home, QueueDirName)
	run := filepath.Join(queue, runtimeEntryPrefix+RuntimeClassShot+"-424244")
	foreign := filepath.Join(run, "remote")
	if err := os.MkdirAll(foreign, DirPerm); err != nil {
		t.Fatal(err)
	}
	stale(t, run, queue)

	f := installMountArm(t, mp(foreign, "fuse.sshfs"))
	if _, err := SweepHome(home, time.Hour); err != nil {
		t.Fatalf("SweepHome: %v", err)
	}
	if len(f.detach) != 0 {
		t.Fatalf("detached %v — a foreign fuse type is not this sweep's business", f.detach)
	}
	if len(f.probed) != 0 {
		t.Fatalf("probed %v — a foreign mount is not even measured here", f.probed)
	}
}

// TestUnmeasurableMountIsReportedAndNotDetached is the CONTRACT half: "cannot
// measure = keep". A probe that cannot answer must never authorise a detach, and
// the refusal must come back as an ERROR the caller can print.
//
// THE ASSERTION IS MADE AT THE ARM, not at the sweep, and that is deliberate: a
// fixture whose "mount point" is an ordinary directory CAN still be deleted by
// RemoveAll, so "the entry survived the sweep" would be measuring the fixture's
// limitation rather than the contract. What the contract actually says is what
// is asserted here — nothing was detached, and the reason is returned.
func TestUnmeasurableMountIsReportedAndNotDetached(t *testing.T) {
	home := t.TempDir()
	queue := filepath.Join(home, QueueDirName)
	run := filepath.Join(queue, runtimeEntryPrefix+RuntimeClassShot+"-424245")
	pinned := filepath.Join(run, "runtime", "gvfs")
	if err := os.MkdirAll(pinned, DirPerm); err != nil {
		t.Fatal(err)
	}
	stale(t, run, queue)

	f := installMountArm(t, mp(pinned, GvfsFuseFSType))
	f.probeErr["gvfs"] = fmt.Errorf("permission denied: the mount cannot be measured")

	detached, err := releaseStaleGvfsMounts(run)
	if err == nil {
		t.Fatal("an unmeasurable mount returned no error — the refusal must not be silent (Rule 15)")
	}
	if detached != 0 {
		t.Fatalf("detached = %d on an unmeasurable mount — that is the defect this contract exists to prevent", detached)
	}
	if len(f.detach) != 0 {
		t.Fatalf("detached %v although the mount could not be measured", f.detach)
	}
	if _, statErr := os.Stat(run); statErr != nil {
		t.Fatalf("the arm destroyed the entry it could not measure: %v", statErr)
	}
}

// TestUnreadableMountTableIsReportedNotSilent pins Rule 15 at the top of the
// arm: a table that cannot be read is an error the caller sees, never an empty
// table that reads as "no mounts here".
func TestUnreadableMountTableIsReportedNotSilent(t *testing.T) {
	prev := scratchMountTable
	scratchMountTable = func() ([]mountPoint, error) { return nil, fmt.Errorf("injected: the mount table is unreadable") }
	t.Cleanup(func() { scratchMountTable = prev })

	dir := t.TempDir()
	detached, err := releaseStaleGvfsMounts(dir)
	if err == nil {
		t.Fatal("releaseStaleGvfsMounts returned no error for an unreadable table — the failure must not be silent")
	}
	if !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("error %v does not name the cause", err)
	}
	if detached != 0 {
		t.Fatalf("detached = %d, want 0", detached)
	}
}

// TestRealMountTableSaysNothingIsUnderAnEmptyDir is the un-injected arm: the
// REAL /proc/self/mounts is read, and a fresh temp directory carries no mount
// under it, so nothing is detached and nothing errors. It is the end-to-end
// proof that the production seams are wired (a nil function value would panic
// here rather than in a tick).
func TestRealMountTableSaysNothingIsUnderAnEmptyDir(t *testing.T) {
	dir := t.TempDir()
	detached, err := releaseStaleGvfsMounts(dir)
	if err != nil {
		t.Fatalf("releaseStaleGvfsMounts over the real mount table: %v", err)
	}
	if detached != 0 {
		t.Fatalf("detached = %d under a fresh temp dir, want 0", detached)
	}
}

// TestDetachWithoutAnyToolReportsTheOperatorRoute pins Rule 25 at the seam: with
// no fusermount on PATH the arm refuses BY NAME and hands the operator the
// terminal route — it never reaches for a privilege, because a privilege is not
// a missing ingredient.
func TestDetachWithoutAnyToolReportsTheOperatorRoute(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // an empty PATH: neither helper resolves
	err := detachFuseMount("/nonexistent/gvfs")
	if err == nil {
		t.Fatal("detachFuseMount returned nil with no fusermount tool on PATH")
	}
	for _, want := range []string{fuseUnmountTool3, fuseUnmountTool, "terminal"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not name %q", err, want)
		}
	}
}

// TestDetachRunsTheRealToolWhenPresent is the positive control for the seam: a
// STUB `fusermount3` on PATH is executed with the documented flag and target, so
// the argv shape is pinned without touching a real mount. The stub records its
// arguments and exits 0, exactly as the real helper does after a successful
// detach.
func TestDetachRunsTheRealToolWhenPresent(t *testing.T) {
	stub := t.TempDir()
	argvFile := filepath.Join(stub, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(stub, fuseUnmountTool3), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stub)

	if err := detachFuseMount("/some/gvfs"); err != nil {
		t.Fatalf("detachFuseMount with a stub tool: %v", err)
	}
	got, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("the stub was never executed: %v", err)
	}
	want := fuseUnmountDetachFlag + "\n/some/gvfs\n"
	if string(got) != want {
		t.Fatalf("stub argv = %q, want %q", got, want)
	}
}

// TestDetachFallsBackToTheLazyForm pins the second attempt: when the ordinary
// detach refuses, the lazy spelling is tried before the arm reports failure.
func TestDetachFallsBackToTheLazyForm(t *testing.T) {
	stub := t.TempDir()
	argvFile := filepath.Join(stub, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + argvFile + "\ncase \"$1\" in -u) exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(stub, fuseUnmountTool3), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stub)

	if err := detachFuseMount("/some/gvfs"); err != nil {
		t.Fatalf("detachFuseMount: %v", err)
	}
	got, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	want := fuseUnmountDetachFlag + "\n/some/gvfs\n" + fuseUnmountLazyFlag + "\n/some/gvfs\n"
	if string(got) != want {
		t.Fatalf("stub argv = %q, want %q", got, want)
	}
}

// TestDetachFailureIsReported pins the last half of Rule 15: when both forms
// fail the arm returns the tool's own output, so the sweep's report says WHY
// rather than repeating a bare ENOTCONN.
func TestDetachFailureIsReported(t *testing.T) {
	stub := t.TempDir()
	script := "#!/bin/sh\necho 'fusermount3: failed to unmount: Device or resource busy' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(stub, fuseUnmountTool3), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stub)

	err := detachFuseMount("/some/gvfs")
	if err == nil {
		t.Fatal("detachFuseMount returned nil although the tool failed")
	}
	if !strings.Contains(err.Error(), "Device or resource busy") {
		t.Fatalf("error %v does not carry the tool's own output", err)
	}
	if !strings.Contains(err.Error(), fuseUnmountLazyFlag) {
		t.Fatalf("error %v does not name the lazy fallback it tried", err)
	}
}

// TestProbeMountPointDistinguishesLiveFromUnmeasurable pins the probe's three
// answers against a REAL path: an ordinary directory is reachable (live), and a
// missing path is neither live nor ENOTCONN — it is unmeasurable, and
// "unmeasurable" must come back as an error rather than as "dead".
func TestProbeMountPointDistinguishesLiveFromUnmeasurable(t *testing.T) {
	live := t.TempDir()
	dead, err := probeMountPoint(live)
	if err != nil {
		t.Fatalf("probe of an ordinary directory errored: %v", err)
	}
	if dead {
		t.Fatal("an ordinary directory was reported as a DEAD mount")
	}
	if _, err := probeMountPoint(filepath.Join(live, "no-such-path")); err == nil {
		t.Fatal("a missing path was measured as a live mount — it must be unmeasurable")
	}
}

// TestFuseUnmountToolPathPrefersFusermount3 pins the resolution order, so a host
// carrying both helpers always reaches the modern one.
func TestFuseUnmountToolPathPrefersFusermount3(t *testing.T) {
	stub := t.TempDir()
	for _, name := range fuseUnmountTools {
		if err := os.WriteFile(filepath.Join(stub, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", stub)
	got, err := fuseUnmountToolPath()
	if err != nil {
		t.Fatalf("fuseUnmountToolPath: %v", err)
	}
	if filepath.Base(got) != fuseUnmountTool3 {
		t.Fatalf("resolved %s, want %s", got, fuseUnmountTool3)
	}
	if _, err := exec.LookPath(got); err != nil {
		t.Fatalf("the resolved path is not executable: %v", err)
	}
}

// stale backdates a fixture's tree past DefaultStaleAge so the sweep's age
// predicate selects it, touching the deepest directory last so every level
// carries the same mtime.
func stale(t *testing.T, paths ...string) {
	t.Helper()
	old := time.Now().Add(-staleFixtureAge)
	for i := len(paths) - 1; i >= 0; i-- {
		if err := os.Chtimes(paths[i], old, old); err != nil {
			t.Fatalf("chtimes %s: %v", paths[i], err)
		}
	}
}
