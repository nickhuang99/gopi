package scratch

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/heavytest"
)

// These are the three LIVE fixtures the sweep's contract is judged on, with
// before/after counts: a stale unheld root goes, a LIVE-held root stays, and a
// foreign directory is never touched. The point of running them against a real
// home (rather than asserting the predicate) is that the holder rule is a
// property of /proc and the filesystem, not of a function's return value.

// staleRootAge is comfortably past DefaultStaleAge (the sweep's own threshold).
const staleRootAge = DefaultStaleAge + time.Hour

// fixtureHome builds a scratch home with one directory of the given name and
// age, returning the home and the directory path.
func fixtureHome(t *testing.T, name string, age time.Duration) (string, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	old := time.Now().Add(-age)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", dir, err)
	}
	return home, dir
}

// reAge sets a path's mtime to now-age (fixtures age themselves; a write inside
// a directory refreshes that directory's mtime, which is a real property of the
// tree, not an artefact).
func reAge(t *testing.T, path string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("lstat %s: %v", path, err)
	return false
}

// TestSweepFixture_StaleUnheldRootIsCollected — fixture 1 of 3.
func TestSweepFixture_StaleUnheldRootIsCollected(t *testing.T) {
	home, dir := fixtureHome(t, "suite-ghost-run-1", staleRootAge)
	before := 1

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	after := before - removed
	if removed != 1 || after != 0 {
		t.Fatalf("stale unheld root: before=%d removed=%d after=%d, want 1/1/0 (dir=%s)",
			before, removed, after, dir)
	}
	if exists(t, dir) {
		t.Fatalf("%s survived the sweep — a stale unheld root leaks forever", dir)
	}
}

// TestSweepFixture_FreshRootIsKept — the age rule's other side: a root that is
// NOT stale must survive, or the sweep would delete a running suite's tree.
func TestSweepFixture_FreshRootIsKept(t *testing.T) {
	home, dir := fixtureHome(t, "suite-live-run-1", time.Minute)
	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 || !exists(t, dir) {
		t.Fatalf("fresh root: removed=%d exists=%v, want 0/true (dir=%s)", removed, exists(t, dir), dir)
	}
}

// TestSweepFixture_LiveHeldRootIsKept — fixture 2 of 3, the hard rule. The
// holder is a REAL process whose working directory is inside the root (the same
// channel a running suite occupies), and the test then proves the rule is not
// vacuous by collecting the very same root once the holder is gone.
func TestSweepFixture_LiveHeldRootIsKept(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionWideParallel)
	home, dir := fixtureHome(t, "suite-held-run-1", staleRootAge)

	// A live process sitting inside the root (sleep is enough: cwd is the
	// channel, not the workload).
	holder := exec.Command("sleep", "30")
	// The child must not outlive THIS BINARY: `go test -timeout` ends in an
	// os.Exit and a SIGKILLed binary runs no t.Cleanup, so the defer below is
	// not enough — Pdeathsig is the only arm the kernel enforces on both paths.
	holder.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	holder.Dir = dir
	if err := holder.Start(); err != nil {
		t.Skipf("cannot start a holder process: %v", err)
	}
	defer func() {
		_ = holder.Process.Kill()    //stylegate:ignore kill of a helper the caller immediately waits on; the wait is the checked step
		_, _ = holder.Process.Wait() //stylegate:ignore wait of a helper we are abandoning; the exit status is not asserted
	}()
	// The kernel must publish the cwd before it can be observed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if pid, err := DirHolder(dir); err == nil && pid != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the holder process's cwd never showed up under %s", dir)
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep poll interval while the kernel publishes the holder process's cwd (the /proc entry is the only probe)
	}

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 || !exists(t, dir) {
		t.Fatalf("LIVE-held root was collected: removed=%d exists=%v (dir=%s) — a live directory was deleted",
			removed, exists(t, dir), dir)
	}

	// Now release the holder and re-sweep: the same root must go, proving the
	// previous pass was protected BY THE HOLDER and not by an accidental age
	// or name mismatch.
	if err := holder.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_, _ = holder.Process.Wait() //stylegate:ignore wait of a process we just killed; the exit status is not asserted
	reaped := time.Now().Add(5 * time.Second)
	for time.Now().Before(reaped) {
		if pid, err := DirHolder(dir); err == nil && pid == "" {
			break
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep poll interval while the reaped holder's /proc entry disappears (the entry is the only probe)
	}
	removed, err = SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if removed != 1 || exists(t, dir) {
		t.Fatalf("after the holder exited: removed=%d exists=%v, want 1/false (dir=%s)",
			removed, exists(t, dir), dir)
	}
}

// TestSweepFixture_ForeignNamespaceIsUntouched — fixture 3 of 3. The sweep is
// namespace-limited: a directory this system did not create (a neighbouring
// program's tree, a user's file) is never collected, however old it is.
func TestSweepFixture_ForeignNamespaceIsUntouched(t *testing.T) {
	home := t.TempDir()
	// NOTE: "queue" is deliberately NOT in this list. It used to be, and that
	// was the leak's fingerprint: the sweep treated the queue subtree as
	// foreign because its name carries no marker, which is exactly why 5,887
	// queue entries accumulated unbounded. The queue directory is OURS
	// (scratch.QueueDirName) and is now swept as a subtree — see
	// gc_queue_subtree_test.go for its own contract.
	foreign := []string{"someone-elses-data", "gopi-notes", "tmp-build", "suite"}
	for _, name := range foreign {
		dir := filepath.Join(home, name)
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		old := time.Now().Add(-staleRootAge * 10)
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", dir, err)
		}
	}
	// One of ours, to prove the sweep actually ran (a vacuous "0 removed" would
	// otherwise pass this test even if the sweep were disabled entirely).
	_, ours := fixtureHome(t, "suite-ours-run-9", staleRootAge)
	if err := os.Rename(ours, filepath.Join(home, "suite-ours-run-9")); err != nil {
		t.Fatalf("move ours: %v", err)
	}

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d, want 1 (only the entry in this system's namespace)", removed)
	}
	left, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range left {
		if e.Name() == "suite-ours-run-9" {
			t.Fatalf("the sweep did not collect its own stale root — the fixture proves nothing")
		}
	}
	// Count the survivors explicitly (the acceptance criterion is a count).
	if len(left) != len(foreign) {
		t.Fatalf("foreign entries: before=%d after=%d, want %d/%d",
			len(foreign), len(left), len(foreign), len(foreign))
	}
}

// TestSweepHome_FailOpenOnBrokenHome is the failure half of the contract: a
// home that cannot be read must NOT be an authorising error for anything, and
// it must not panic — a resident tick calls this.
func TestSweepHome_FailOpenOnBrokenHome(t *testing.T) {
	// A home that is a FILE: ReadDir fails with ENOTDIR.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), FilePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	removed, err := SweepHome(file, DefaultStaleAge)
	if err == nil || removed != 0 {
		t.Fatalf("a non-directory home must report an error and remove nothing, got removed=%d err=%v", removed, err)
	}

	// A missing home is the EXPECTED non-event (a host that never ran a suite).
	removed, err = SweepHome(filepath.Join(t.TempDir(), "absent"), DefaultStaleAge)
	if err != nil || removed != 0 {
		t.Fatalf("a missing home must be a no-op, got removed=%d err=%v", removed, err)
	}
}

// TestGCLock_SecondSweepDefersAndStaleLockBreaks pins the concurrency rule: a
// held lock makes the second GC defer (never a double sweep), and an abandoned
// lock is broken so a crashed GC cannot disable the sweep forever.
func TestGCLock_SecondSweepDefersAndStaleLockBreaks(t *testing.T) {
	home, dir := fixtureHome(t, "suite-ghost-run-2", staleRootAge)
	release, locked, err := AcquireGCLock(home)
	if err != nil || !locked {
		t.Fatalf("first lock: locked=%v err=%v", locked, err)
	}
	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("deferred sweep: %v", err)
	}
	if removed != 0 || !exists(t, dir) {
		t.Fatalf("the second GC must defer to the lock, got removed=%d exists=%v", removed, exists(t, dir))
	}
	release()

	// An abandoned lock (older than GCLockStaleAge) must be broken. The lock
	// file is created and THEN aged (writing it would refresh its mtime).
	stale := time.Now().Add(-GCLockStaleAge - time.Minute)
	if _, err := os.Create(filepath.Join(home, GCLockFileName)); err != nil {
		t.Fatalf("recreate lock: %v", err)
	}
	if err := os.Chtimes(filepath.Join(home, GCLockFileName), stale, stale); err != nil {
		t.Fatalf("age the lock: %v", err)
	}
	removed, err = SweepHome(home, DefaultStaleAge)
	if err != nil || removed != 1 {
		t.Fatalf("a stale lock must be broken: removed=%d err=%v", removed, err)
	}
}

// TestHolderRegistration protects a root through the permission-free channel.
func TestHolderRegistration(t *testing.T) {
	home, dir := fixtureHome(t, "suite-registered-run-1", staleRootAge)
	RegisterHolder(dir)
	reAge(t, dir, staleRootAge) // the registration write lives INSIDE the root

	alive, why := HolderAlive(dir)
	if !alive {
		t.Fatalf("this process registered %s but HolderAlive says dead (%s)", dir, why)
	}
	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 || !exists(t, dir) {
		t.Fatalf("a REGISTERED live root was collected: removed=%d exists=%v", removed, exists(t, dir))
	}

	// A registration whose pid is alive but whose START TIME differs is a
	// reused pid, not the owner — the root must be collectable again.
	if err := os.WriteFile(filepath.Join(dir, HolderFileName),
		[]byte("pid=1\nstart=1\n"), FilePerm); err != nil {
		t.Fatalf("rewrite holder: %v", err)
	}
	// Writing a file INSIDE the root refreshes the root's own mtime (a real
	// property of the tree: a suite that writes into its root keeps it young),
	// so re-age it to keep the fixture about the HOLDER rule.
	reAge(t, dir, staleRootAge)
	removed, err = SweepHome(home, DefaultStaleAge)
	if err != nil || removed != 1 {
		t.Fatalf("a stale/reassigned registration must not protect the root: removed=%d err=%v", removed, err)
	}
}

// TestParseProcStartTime pins the /proc/<pid>/stat parsing trap: the comm field
// is parenthesised and may contain spaces and parens.
func TestParseProcStartTime(t *testing.T) {
	// fields[0] is the state (kernel field 3), so kernel field 22 (starttime)
	// is fields[19] — the fixture places 1234567 exactly there.
	raw := "42 (a name (with) parens) R 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 1234567 21 22\n"
	got, err := ParseProcStartTime(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != 1234567 {
		t.Fatalf("starttime = %d, want 1234567 (field 22, counted after the last ')')", got)
	}
	if _, err := ParseProcStartTime("no-parens"); err == nil {
		t.Fatal("a stat line without a comm field must be an error, not a silent zero")
	}
}
