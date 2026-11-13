package runlock

// THE LOCK'S TWO FACTS, PINNED APART (bug epic 01a1ac0f).
//
// The red these tests exist to keep red: a run directory whose stamp names pid
// X says NOTHING about whether X holds the directory. So the arms below drive
// the two facts against the SAME file and require them to disagree:
//
//   - a file with a stamp and NO flock must read State=free (the stamp's pid is
//     a lie the moment its writer exited — measured on this host 2026-11-05);
//   - a file with a flock must read State=held and NAME the holder;
//   - a file that cannot be looked up must read unmeasured, never free.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestLocksHoldingInParsesOnlyThisFile pins the /proc/locks parser: FLOCK lines
// are matched by the (major:minor:inode) key, POSIX lines are ignored (a
// different mechanism that never satisfies a run-directory guard), and a
// malformed row is skipped rather than turning the whole reading into an error.
func TestLocksHoldingInParsesOnlyThisFile(t *testing.T) {
	const (
		myKey    = "103:04:5653565"
		otherKey = "103:04:1365189"
		myPid    = 2680836
	)
	doc := strings.Join([]string{
		"1: POSIX  ADVISORY  WRITE 511028 103:04:" + strings.TrimPrefix(otherKey, "103:04:") + " 0 EOF",
		"2: FLOCK  ADVISORY  WRITE 999 " + otherKey + " 0 EOF",
		"3: FLOCK  ADVISORY  WRITE " + strconv.Itoa(myPid) + " " + myKey + " 0 EOF",
		"",
		"garbage row with too few fields",
	}, "\n")
	pid, found, err := locksHoldingIn(doc, myKey)
	if err != nil {
		t.Fatalf("locksHoldingIn returned an error over a well-formed document: %v", err)
	}
	if !found {
		t.Fatalf("locksHoldingIn did not find the FLOCK on %s in:\n%s", myKey, doc)
	}
	if pid != myPid {
		t.Fatalf("locksHoldingIn named pid %d, want %d — the holder must be the FLOCK's own pid", pid, myPid)
	}

	// The NEGATIVE arm is the one that matters: a key nobody holds must come
	// back (0, false, nil). Returning "found" here would refuse every run.
	if pid, found, err := locksHoldingIn(doc, "103:04:9999999"); err != nil || found || pid != 0 {
		t.Fatalf("locksHoldingIn reported pid=%d found=%v err=%v for a key no line names — want 0/false/nil", pid, found, err)
	}

	// A FLOCK-shaped row for the right file but held by pid 0 is not a holder:
	// the kernel never reports one, and treating it as held would name no
	// process at all.
	if _, found, err := locksHoldingIn("1: FLOCK  ADVISORY  WRITE 0 "+myKey+" 0 EOF\n", myKey); err != nil {
		t.Fatalf("locksHoldingIn on a pid-0 row: %v", err)
	} else if found {
		t.Fatal("locksHoldingIn accepted pid 0 as a holder — a held lock always names a positive pid")
	}
}

// TestLiveHolderOfTellsTheStampFromTheFlock is the epic's own shape, driven on
// a real file and a real kernel lock: the same path answers free-with-a-stamp
// and then held-by-me, and the two readings are never the same value.
func TestLiveHolderOfTellsTheStampFromTheFlock(t *testing.T) {
	dir := t.TempDir()
	path := PathIn(dir)
	if got := LiveHolderOf(path); got.State != LockStateFree {
		t.Fatalf("a lock file that does not exist read as %q (%s) — want %q", got.State, got.Why, LockStateFree)
	}
	if err := os.WriteFile(path, []byte("pid=2662735\nticks=123\n"), 0o600); err != nil {
		t.Fatalf("seed the stamp: %v", err)
	}
	if got := LiveHolderOf(path); got.State != LockStateFree {
		t.Fatalf("a stamped but UNLOCKED file read as %q (%s) — want %q: the stamp is the previous holder's diagnosis, not a holder", got.State, got.Why, LockStateFree)
	}
	if stamp := StampOf(path); !strings.Contains(stamp, "2662735") {
		t.Fatalf("StampOf = %q, want the stamped pid 2662735 in it", stamp)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the lock file: %v", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("take the flock: %v", err)
	}
	// Releasing at teardown cannot fail in a way a test could act on: the
	// descriptor is about to close, which drops the lock regardless, and the
	// assertion about the lock has already been made above this line.
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }() //stylegate:ignore teardown release — closing the fd drops the lock anyway, and the assertion already ran

	got := LiveHolderOf(path)
	if got.State != LockStateHeld {
		t.Fatalf("a file this process holds a flock on read as %q (%s) — want %q", got.State, got.Why, LockStateHeld)
	}
	if got.Pid != os.Getpid() {
		t.Fatalf("LiveHolderOf named pid %d, want this process's %d — the kernel's ledger names the lock's holder", got.Pid, os.Getpid())
	}
	if !got.Held() {
		t.Fatal("LiveHolder.Held() is false for State=held")
	}
}

// TestLiveHolderOfRefusesToCallAnUnreadableLockFree pins Rule 15b on this axis:
// a path whose parent is not a directory cannot be measured, and "unmeasured"
// must not be spelled "free" (an operator acting on a fabricated free would
// start a second run on a live one).
func TestLiveHolderOfRefusesToCallAnUnreadableLockFree(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed the regular file: %v", err)
	}
	got := LiveHolderOf(PathIn(notADir))
	if got.State == LockStateFree {
		t.Fatalf("an unreadable lock path read as %q — the answer must be %q with the failure named (%s)", got.State, LockStateUnmeasured, got.Why)
	}
	if got.State != LockStateUnmeasured {
		t.Fatalf("got state %q, want %q", got.State, LockStateUnmeasured)
	}
	if strings.TrimSpace(got.Why) == "" {
		t.Fatal("an unmeasured reading carries no reason — absence must be stated (Rule 15)")
	}
}
