package scratch

// THE WRITER-OWNED LIFETIME CONTRACT, judged with a REAL sweep against a REAL
// home:
//
//	no process at the recorded pid                  → the recorded owner is GONE
//	                                                        → collect, at ANY age
//	pid alive, kernel start ticks differ            → the number was RECYCLED
//	                                                        → collect, at ANY age
//	pid alive, start ticks agree                    → the OWNER IS LIVE → keep
//	owner alive SOMEWHERE ELSE (not standing in the root) → keep (this is the case
//	   the /proc cwd|fd scan cannot see, and the registration exists for)
//
// The first two directions are exactly what the age clock was standing in for: a
// SIGKILLed run leaves a FRESH root, so an age-only predicate must wait out the
// whole window (6h) before the residue is even a candidate. So both fixtures age
// to ~0 on purpose — if the verdict still needs the clock, these tests are red.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/procname"
)

// ownerIdentity is a recorded owner: the pid, its kernel start ticks and its
// command line — exactly the three factors procname.RunnerIdentity compares.
type ownerIdentity struct {
	pid   int
	start uint64
	cmd   string
}

// deadOwner returns the identity of a process that is PROVABLY gone: a real child
// is started, its identity captured, then it is killed and reaped — so the record
// names a pid the kernel has already released.
func deadOwner(t *testing.T) ownerIdentity {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	// Pdeathsig so the fixture owner dies with this binary even when the run is
	// SIGKILLed (no t.Cleanup then) — a survivor here is a `sleep 60` nobody owns.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a fixture owner (%v) — the gone-owner case is untestable here", err)
	}
	pid := cmd.Process.Pid
	start, err := ProcStartTime(pid)
	if err != nil {
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		_ = cmd.Wait()         //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
		t.Skipf("cannot read /proc/%d/stat (%v)", pid, err)
	}
	argv, _ := procname.Cmdline(ProcRootDir, strconv.Itoa(pid))
	cmdText := ownerRecordCmd(argv)
	if err := cmd.Process.Kill(); err != nil {
		_ = err //stylegate:ignore the error is already reported by the caller's checked call; this site only reaps or logs the leftover// best effort: Wait still reaps it
	}
	if err := cmd.Wait(); err != nil {
		_ = err //stylegate:ignore the error is already reported by the caller's checked call; this site only reaps or logs the leftover// a killed child exits non-zero; the point is that it is GONE
	}
	if _, err := ProcStartTime(pid); err == nil {
		t.Skipf("pid %d is still in /proc after Wait — cannot build a gone-owner fixture here", pid)
	}
	return ownerIdentity{pid: pid, start: start, cmd: cmdText}
}

// writeOwnerIdentity writes a registration body the way a v2 writer does.
func writeOwnerIdentity(t *testing.T, dir string, id ownerIdentity) {
	t.Helper()
	body := fmt.Sprintf("%s=%d\n%s=%d\n%s=%d\n",
		OwnerRecordVersionKey, OwnerRecordVersion, HolderKeyPID, id.pid, HolderKeyStart, id.start)
	if id.cmd != "" {
		body += fmt.Sprintf("%s=%s\n", HolderKeyCmd, id.cmd)
	}
	if err := os.WriteFile(filepath.Join(dir, HolderFileName), []byte(body), FilePerm); err != nil {
		t.Fatalf("write owner record in %s: %v", dir, err)
	}
}

// freshScratchRoot creates an IN-NAMESPACE scratch root whose mtime is NOW — one
// the age rule alone would keep for the whole stale window.
func freshScratchRoot(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

// TestOwnerVerdictGoneOwnerIsCollectedWhileFresh — direction 1 of 3: a recorded
// owner that is GONE makes the root collectable NOW, not in six hours.
func TestOwnerVerdictGoneOwnerIsCollectedWhileFresh(t *testing.T) {
	home := t.TempDir()
	fossil := freshScratchRoot(t, home, SuiteRunMarker+"killed-run-fossil")
	writeOwnerIdentity(t, fossil, deadOwner(t))

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 || exists(t, fossil) {
		t.Fatalf("a FRESH root whose recorded owner is GONE: removed=%d exists=%v, want 1/false "+
			"(the recorded owner is the proof the clock was standing in for)", removed, exists(t, fossil))
	}
}

// TestOwnerVerdictLiveOwnerElsewhereIsKept — direction 2 of 3: the registration
// protects an owner that is alive but NOT standing in the root (the case the
// /proc cwd|fd|exe|maps scan cannot see).
func TestOwnerVerdictLiveOwnerElsewhereIsKept(t *testing.T) {
	home := t.TempDir()
	live := freshScratchRoot(t, home, SuiteRunMarker+"live-run-elsewhere")
	RegisterHolder(live) // this test process: alive, and its cwd is NOT the root

	// The sweep must refuse it while it is FRESH and while it is STALE — the
	// registration is not an age rule.
	for _, age := range []time.Duration{0, staleRootAge} {
		reAge(t, live, age)
		removed, err := SweepHome(home, DefaultStaleAge)
		if err != nil {
			t.Fatalf("sweep: %v", err)
		}
		if removed != 0 || !exists(t, live) {
			t.Fatalf("a root with a LIVE registered owner was collected at age %v: removed=%d exists=%v",
				age, removed, exists(t, live))
		}
	}
}

// TestOwnerVerdictRecycledPIDIsCollected — direction 3 of 3: the pid is alive but
// it is NOT the recorded process (the kernel handed the number on), so the owner
// is gone and the root is garbage.
func TestOwnerVerdictRecycledPIDIsCollected(t *testing.T) {
	home := t.TempDir()
	recycled := freshScratchRoot(t, home, SuiteRunMarker+"recycled-run")
	start, err := ProcStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("read own start ticks: %v", err)
	}
	// This process IS alive at this pid; the recorded incarnation belongs to a
	// previous life of the number.
	writeOwnerIdentity(t, recycled, ownerIdentity{pid: os.Getpid(), start: start + 1})

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 || exists(t, recycled) {
		t.Fatalf("a FRESH root whose pid was RECYCLED: removed=%d exists=%v, want 1/false",
			removed, exists(t, recycled))
	}
}

// TestOwnerVerdictFossilAndLiveInOneSweep — the epic's fixture: one sweep, two
// fresh roots, exactly one collected — the fossil goes, the live-elsewhere one
// stays.
func TestOwnerVerdictFossilAndLiveInOneSweep(t *testing.T) {
	home := t.TempDir()
	fossil := freshScratchRoot(t, home, SuiteRunMarker+"one-sweep-fossil")
	live := freshScratchRoot(t, home, SuiteRunMarker+"one-sweep-live")
	writeOwnerIdentity(t, fossil, deadOwner(t))
	RegisterHolder(live)

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("one sweep, two fresh roots (one fossil, one live-elsewhere): removed=%d, want exactly 1", removed)
	}
	if exists(t, fossil) {
		t.Fatalf("the fossil root %s survived", fossil)
	}
	if !exists(t, live) {
		t.Fatalf("the LIVE-elsewhere root %s was collected — the registration must protect it", live)
	}
}

// TestOwnerVerdictUnreadableRecordKeeps — the conservative direction, and the one
// that must never invert: a record this code cannot parse is NOT evidence of
// death, so the entry is kept at any age (Rule 15a: an unmeasured fact is never
// rendered as a death).
func TestOwnerVerdictUnreadableRecordKeeps(t *testing.T) {
	home := t.TempDir()
	broken := freshScratchRoot(t, home, SuiteRunMarker+"unreadable-record")
	reAge(t, broken, staleRootAge)
	if err := os.WriteFile(filepath.Join(broken, HolderFileName), []byte("pid=not-a-number\n"), FilePerm); err != nil {
		t.Fatalf("write unreadable record: %v", err)
	}

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 0 || !exists(t, broken) {
		t.Fatalf("an entry whose owner record does not parse was collected (removed=%d, exists=%v) — "+
			"an unreadable registration is never a death", removed, exists(t, broken))
	}
	v := OwnerVerdictFor(broken)
	if v.Outcome != procname.OutcomeUnmeasurable || !v.Registered {
		t.Fatalf("verdict for an unreadable record = %s registered=%v, want unmeasurable/true", v.Outcome, v.Registered)
	}
}

// TestOwnerVerdictUnregisteredFallsThroughToTheClock — the OTHER half of the
// division: a root NO writer registered has no owner proof, so the age window
// still decides it (this is why the window could not simply be deleted).
func TestOwnerVerdictUnregisteredFallsThroughToTheClock(t *testing.T) {
	home := t.TempDir()
	fresh := freshScratchRoot(t, home, SuiteRunMarker+"unregistered-fresh")
	aged := freshScratchRoot(t, home, SuiteRunMarker+"unregistered-aged")
	reAge(t, aged, staleRootAge)

	v := OwnerVerdictFor(fresh)
	if v.Registered {
		t.Fatalf("a root with no %s must report Registered=false", HolderFileName)
	}
	if v.Collectable {
		t.Fatal("an unregistered verdict must not authorise a delete on its own (OwnerKeeps reads exactly this)")
	}
	if OwnerKeeps(fresh) {
		t.Fatal("OwnerKeeps(no record) must be false — the age window is what bounds an unregistered root")
	}

	removed, err := SweepHome(home, DefaultStaleAge)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 || !exists(t, fresh) || exists(t, aged) {
		t.Fatalf("unregistered roots: removed=%d fresh-exists=%v aged-exists=%v, want 1/true/false",
			removed, exists(t, fresh), exists(t, aged))
	}
}
