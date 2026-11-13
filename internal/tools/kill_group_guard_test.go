package tools

import (
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

// kill_group_guard_test.go — THE RE-VERIFICATION IS THE GUARD, AND IT MUST
// REFUSE A NUMBER THAT MOVED.
//
// THE DEFECT (measured 2026-10-24): thirteen call sites did
// `syscall.Kill(-cmd.Process.Pid, SIGKILL)` on the assumption that a group id
// stays its leader's pid forever. It does not — the kernel recycles the number
// once the leader is reaped, and SIGKILL cannot be caught, so a stranger group
// dies with no death record. The operator's 8 firefox processes went that way in
// one second during a suite run.

// TestKillGroupRefusesANumberThatIsNotTheChilds is the RED-able arm: a live
// process that is NOT the group leader of the id handed in must be refused, so
// the caller degrades to the pid path instead of addressing a stranger's group.
func TestKillGroupRefusesANumberThatIsNotTheChilds(t *testing.T) {
	// THIS process leads (or shares) its own group; its pid is NOT our test
	// child's pid, so "pgid = child pid, pid = our pid" is the mismatched shape.
	child := exec.Command("sleep", "30")
	// Setpgid makes the child lead a group whose id IS its pid — the shape every
	// production call site creates and then addresses. Without it the child stays
	// in OUR group and the positive control below would be asserting the wrong
	// thing (measured: without Setpgid the guard correctly refuses pid-as-pgid).
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := child.Start(); err != nil {
		t.Skipf("cannot start a child to stand in: %v", err)
	}
	t.Cleanup(func() {
		//stylegate:ignore best-effort cleanup of a child this test started
		_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
		//stylegate:ignore the reaper's error is not this test's subject
		_ = child.Wait()
	})

	// The honest shape: the child's own pid as the group, because Setpgid made it
	// the leader of exactly that number.
	if !killGroupStillTheChilds(child.Process.Pid, child.Process.Pid) {
		t.Fatalf("a live child that leads group %d was refused — the guard would degrade every "+
			"legitimate group signal", child.Process.Pid)
	}

	// The stale shape: a number that is NOT this child's.
	strangerPGID := child.Process.Pid + 100000
	if killGroupStillTheChilds(child.Process.Pid, strangerPGID) {
		t.Errorf("pgid %d (which is not the child's group) was accepted for pid %d — the guard "+
			"would deliver a group signal to whatever that number names now",
			strangerPGID, child.Process.Pid)
	}
}

// TestKillGroupRefusesAReapedChild is the measured incident, made deterministic:
// once the child is gone its number is recycle-eligible, so no group signal may
// be addressed by it.
func TestKillGroupRefusesAReapedChild(t *testing.T) {
	child := exec.Command("true")
	if err := child.Start(); err != nil {
		t.Skipf("cannot start a child: %v", err)
	}
	pid := child.Process.Pid
	if err := child.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	// Wait for the reap to be visible. Rule: poll, do not sleep — the criterion is the
	// state, not the clock, and cmd.Wait() has already returned so the poll terminates
	// as soon as the kernel publishes the reap.
	reaped := false
	for i := 0; i < 200; i++ {
		if syscall.Kill(pid, 0) != nil {
			reaped = true
			break
		}
		runtime.Gosched()
	}
	if !reaped {
		t.Skipf("pid %d never became reapable within the poll budget — the guard's refusal "+
			"below would prove nothing", pid)
	}
	if killGroupStillTheChilds(pid, pid) {
		t.Errorf("pid %d was REAPED and still passed the guard — this is the recycle window that "+
			"killed the operator's browser and game", pid)
	}
}

// TestSignalProcessGroupDegradesToThePid pins the degradation contract: a refused
// group must NOT fall through to silence — the caller still signals the pid it
// holds, which is the handle it actually owns.
func TestSignalProcessGroupDegradesToThePid(t *testing.T) {
	child := exec.Command("sleep", "30")
	// Pdeathsig only: this child deliberately does NOT lead its own group (the
	// degradation arm needs a pgid that is not its pid), and it must not
	// outlive a SIGKILLed test binary either.
	child.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := child.Start(); err != nil {
		t.Skipf("cannot start a child: %v", err)
	}
	pid := child.Process.Pid
	inGroup, err := signalProcessGroup(pid, pid+100000, syscall.SIGTERM)
	if err != nil {
		t.Logf("pid-path delivery reported %v (already gone is acceptable)", err)
	}
	if inGroup {
		t.Errorf("a mismatched group id %d was accepted as the child's — the degradation arm did "+
			"not run", pid+100000)
	}
	_ = child.Wait() //stylegate:ignore reaping the fixture child the test spawned; its exit status is not the assertion
}
