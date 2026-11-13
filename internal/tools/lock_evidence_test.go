package tools

// lock_evidence_test.go — the STOP-TARGET authority (2026-09-13, row
// merger-stop-pid).
//
// THE DEFECT THESE TESTS PIN: `gopi merger --stop` resolved its target from a
// merger STATE FILE (a best-effort record) and, when the pid it watched died,
// printed "pid N exited …" and exited 0 — while the real daemon, whose flock is
// the only proof of "a merger is running", kept merging. Two measured shapes:
//
//	ARM A  no state file at all      → "no merger is running", rc=0, daemon alive
//	ARM B  a state file naming a     → "--stop" signalled the foreign process,
//	       FOREIGN live pid (pid         reported ITS exit, rc=0, daemon alive
//	       reuse / substitution)
//
// The fix moves the authority to the flock and asks it for the WHOLE holder
// set, because a flock lives on the open file description: a fork/exec child
// that inherits the descriptor is a holder too, and /proc/locks plus the fdinfo
// scan can each name a different member of the set. A stop that signals only
// "the" holder — as LockProbe's single-pid answer would — can leave a second
// live process holding the lock while reporting success.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// leHolderScript is the child's "inherit the lock and keep it" recipe: fd 3 is
// the lock file the PARENT flock()ed, handed over by ExtraFiles. The child
// takes no lock of its own — it does not have to: flock(2) lives on the open
// file DESCRIPTION, so the inherited descriptor IS the lock, and the child's
// own /proc/<pid>/fdinfo therefore carries a FLOCK row for the same inode.
//
// That is precisely the daemon's shape (the parent takes the flock, the
// detached child inherits the descriptor), and it is why "who holds the lock?"
// has more than one right answer: /proc/locks names the process that CREATED
// the lock while every inheriting pid's fdinfo names the lock too.
const leHolderScript = "exec sleep %d\n"

// leStartHolderInheriting starts a child that holds the lock through a
// descriptor INHERITED from this test process (the daemon's own shape), and
// returns its pid. f MUST be the descriptor that actually holds the flock —
// flock lives on the open file description, so a second descriptor on the same
// file is unlocked and its child would inherit nothing (measured: a reopened
// fd appeared in the child's table with the inode but no lock row).
//
// The child is reaped by the testutil mock-process harness — never by a
// pattern kill.
func leStartHolderInheriting(t *testing.T, f *os.File, seconds int) int {
	t.Helper()
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skipf("sleep(1) not available: %v", err)
	}
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf(leHolderScript, seconds))
	// ExtraFiles[0] becomes fd 3 in the child. os/exec clears close-on-exec on
	// the duplicated descriptors, so the child inherits the very description
	// this test holds the flock on.
	cmd.ExtraFiles = []*os.File{f}
	proc := testutil.StartMockProcessCmd(t, cmd)
	t.Cleanup(proc.Reap)
	return proc.Pid()
}

// leWaitHolderRegistered waits until pid's descriptor table shows the flock on
// lockPath's inode — the proof that the child really holds the inherited lock
// (reading the answer instead of sleeping a guessed interval).
func leWaitHolderRegistered(t *testing.T, pid int, ino uint64) {
	t.Helper()
	deadline := time.Now().Add(leWait)
	for time.Now().Before(deadline) {
		if fdInfoHoldsFlockOn(pid, ino) {
			return
		}
		time.Sleep(lePoll) //stylegate:allow-sleep wait for the child to start holding the inherited flock
	}
	t.Fatalf("child %d never registered as a holder of inode %d (the inherited descriptor was not held)", pid, ino)
}

const (
	// leWait bounds every probe in this file.
	leWait = 20 * time.Second
	// lePoll is the probe cadence.
	lePoll = 50 * time.Millisecond
	// leHoldSeconds is how long a holder child keeps the lock — far longer than
	// any assertion below needs.
	leHoldSeconds = 30
)

// TestLockEvidences_ListsEveryLiveHolder is THE regression test for the fix: a
// second process holding the SAME flock through an inherited descriptor must
// appear in the evidence set. A single-pid answer (LockProbe) is what let the
// stop path signal one holder and report success while another kept running.
func TestLockEvidences_ListsEveryLiveHolder(t *testing.T) {
	path, f, ino := leHeldLock(t)
	child := leStartHolderInheriting(t, f, leHoldSeconds)
	leWaitHolderRegistered(t, child, ino)

	evidences, probed := LockEvidences(path)
	if !probed {
		t.Fatalf("the lock could not be probed at all on this host — the stop path would degrade to a state file")
	}
	got := map[int]bool{}
	for _, ev := range evidences {
		if ev.Stray {
			t.Errorf("a live holder of the NAMED lock path must not read as a stray (unlinked) holder: pid %d", ev.PID)
		}
		got[ev.PID] = true
	}
	if !got[os.Getpid()] {
		t.Errorf("the evidence set must name this (flock-holding) process %d: %v", os.Getpid(), evidences)
	}
	if !got[child] {
		t.Errorf("the evidence set must name the child holding the INHERITED descriptor %d — a stop that misses it reports a success it never observed: %v",
			child, evidences)
	}
	if len(evidences) != 2 {
		t.Errorf("want exactly the two real holders (%d, %d), got %v", os.Getpid(), child, evidences)
	}
}

// TestLockEvidences_FreeLockIsNoEvidence is the false-positive guard: with
// nobody holding the lock the set is EMPTY (and probed=true, i.e. "we looked
// and the lock is free" — not "we could not look", which is a different answer
// the stop path treats differently).
func TestLockEvidences_FreeLockIsNoEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.lock")
	if err := os.WriteFile(path, []byte("1234"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidences, probed := LockEvidences(path)
	if !probed {
		t.Fatalf("a readable /proc must allow a probe")
	}
	if len(evidences) != 0 {
		t.Fatalf("a free lock must yield NO evidence (the stamped pid is advisory, never a holder): %v", evidences)
	}
	// A lock path that does not exist at all: still "probed, nothing holds it"
	// — the stop path must not read it as a surviving daemon.
	missing, probedMissing := LockEvidences(filepath.Join(t.TempDir(), "absent.lock"))
	if !probedMissing || len(missing) != 0 {
		t.Fatalf("an absent lock path must read as free (probed=%v, evidence=%v)", probedMissing, missing)
	}
}

// TestLockEvidences_DeadTableRowIsNotEvidence — /proc/locks attributes a flock
// to the process that CREATED it, so the row routinely names a pid that is
// already gone (GoPi's daemon forks its own child and the creator exits). A
// DEAD pid in the evidence set would hand the stop path a recycled pid to
// signal — the accident class this whole change exists to remove.
func TestLockEvidences_DeadTableRowIsNotEvidence(t *testing.T) {
	path, ino := lockFixture(t, fmt.Sprint(os.Getpid()))
	corpse := deadPID(t)
	fakeLockTable(t, corpse, ino)

	evidences, probed := LockEvidences(path)
	if !probed {
		t.Fatal("probe must be possible here")
	}
	for _, ev := range evidences {
		if ev.PID == corpse {
			t.Fatalf("the dead table row %d must not be evidence to SIGNAL — the live holder is found by the fdinfo scan instead: %v",
				corpse, evidences)
		}
	}
	if len(evidences) != 1 || evidences[0].PID != os.Getpid() {
		t.Fatalf("want exactly the live holder %d, got %v", os.Getpid(), evidences)
	}
}

// TestLockEvidences_StrayHolderIsEvidenceAndNamed — a process holding an
// UNLINKED copy of the lock path is still a merger (that is what holding
// this lock means), but its guard is detached. The evidence must include it AND
// label it, so the stop path can end it while telling the operator which fact
// named it (the 2026-09-12 unlink incident).
func TestLockEvidences_StrayHolderIsEvidenceAndNamed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Skipf("cannot take flock in this environment: %v", err)
	}
	t.Cleanup(func() { f.Close() }) //stylegate:ignore test cleanup: the descriptor must stay open for the whole test body
	// UNLINK the path underneath the holder: the lock now lives on a deleted
	// inode, so the name-based probes see a free path and only the stray probe
	// can find the live holder.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "serve.lock")
	if err := os.WriteFile(fresh, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidences, probed := LockEvidences(fresh)
	if !probed {
		t.Fatal("the stray probe must count as a probe")
	}
	named := false
	for _, ev := range evidences {
		if ev.PID == os.Getpid() && ev.Stray {
			named = true
		}
	}
	if !named {
		t.Fatalf("the stray holder (this process, pid %d) must be evidence and labelled stray: %v", os.Getpid(), evidences)
	}
}

// leHeldLock is the self-contained fixture: it opens and flock()s a lock file
// and returns (path, the HOLDING descriptor, inode). Passing THAT descriptor to
// a child is what creates the second holder (see leStartHolderInheriting).
func leHeldLock(t *testing.T) (string, *os.File, uint64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "serve.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close() //stylegate:ignore close of the descriptor we could not lock before failing
		t.Skipf("cannot take flock in this environment: %v", err)
	}
	t.Cleanup(func() { f.Close() }) //stylegate:ignore test cleanup: the descriptor must stay open for the whole test body
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, f, inodeOf(st)
}
