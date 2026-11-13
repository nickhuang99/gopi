package tools

import (
	"syscall"
	"testing"
)

// bash_kill_group_addressing_forms_test.go — THE GUARD'S CONTRACT IS ABOUT THE
// VALUE THE KERNEL IS HANDED, not about the number we happen to call a group.
//
// THE DEFECT SHAPE (measured 2026-10-23, this task). The one definition
// killChildTreeGroupIsTheCallers answers "would a group signal addressed at pgid
// reach the process that is doing the killing". Its production delivery spells
// the signal `syscall.Kill(-pgid, sig)` (bash.go's killChildTreeGroupSignal), so
// the kernel is handed `-pgid` — and by kill(2) three other values in that
// address space reach the caller just as surely as `-Getpgrp()` does:
//
//	pgid =  0          → kill(0)         every process in MY OWN group
//	pgid =  1          → kill(-1)        every process I may signal (init excepted)
//	pgid = -Getpid()   → kill(+Getpid()) ME, by pid
//
// The shipped predicate compared against Getpgrp() and Getpid() only, so it
// answered false for all three — its stated contract was wrong for three of the
// five forms that reach the caller. Wrong in the safe direction is still wrong:
// the next caller that trusts it spells `kill(-pgid, …)` where pgid came from
// somewhere else, and SIGKILL cannot be caught.
//
// MEASURED (sig 0 delivers nothing — a pure existence/permission probe):
//
//	my pid=2040004 my pgid=2039778
//	kill(0, 0)        -> err=<nil>
//	kill(-1, 0)       -> err=<nil>
//	kill(-2039778, 0) -> err=<nil>
//	kill(2039778, 0)  -> err=<nil>
//	kill(6234082, 0)  -> err=no such process
//
// kill(2): "If pid equals 0, then sig is sent to every process in the process
// group of the calling process." … "If pid equals -1, then sig is sent to every
// process for which the calling process has permission to send signals, except
// for process 1 (init)."
//
// HONEST SCOPE (Rule 41). Both production call sites pass a POSITIVE group id
// today: killChildTree passes a Getpgid() result gated on `pgid == pid` with
// pid > 0, and runCloseoutSelection passes RunCloseoutGroup.Leader, which
// RunCloseoutGroupFor mints only for pid > 0. So this closes the CONTRACT, not a
// reachable call — the same "defence in depth" standing the guard itself was
// added with. The refusal is never a silent drop: both call sites degrade to
// per-pid signals, so refusing these forms cannot suppress a real kill.

const (
	// kbafOwnGroupID is the pgid whose ADDRESSED value is 0: `kill(-0)` is
	// kill(0), the calling process's own group.
	kbafOwnGroupID = 0
	// kbafBroadcastID is the pgid whose ADDRESSED value is -1: `kill(-1)`
	// delivers to every process this caller may signal.
	kbafBroadcastID = 1
	// kbafForeignOffset names a group that provably is not this process's: group
	// ids are pid numbers, so a live value plus a large offset addresses no
	// process at all. It is the positive control's probe.
	kbafForeignOffset = 1 << 22
)

// TestKillChildTreeOwnGroupGuardRefusesEveryAddressingFormThatReachesTheCaller
// is THE REFUSAL, stated over the address space instead of over the number:
// every pgid whose `kill(-pgid, …)` spelling reaches this process must be
// refused — and a pgid that reaches nothing must NOT be, which is the control arm
// without which a guard that refused everything would pass while breaking every
// group kill in the tool (bug011's deadlock returns: a `nohup … &` descendant
// holding a pipe write-end is only reachable as a group).
func TestKillChildTreeOwnGroupGuardRefusesEveryAddressingFormThatReachesTheCaller(t *testing.T) {
	own := killChildTreeGroupOf()
	me := syscall.Getpid()

	refused := []struct {
		name string
		pgid int
		why  string
	}{
		{"own group", own, "kill(-Getpgrp()) is this process's own group"},
		{"own pid", me, "kill(-Getpid()) is the group this process leads"},
		{"own-group address", kbafOwnGroupID, "kill(0) is every process in this process's own group"},
		{"broadcast address", kbafBroadcastID, "kill(-1) is every process this caller may signal"},
		{"negated own pid", -me, "kill(-(-Getpid())) is kill(Getpid()): this process, by pid"},
	}
	for _, c := range refused {
		if !killChildTreeGroupIsTheCallers(c.pgid) {
			t.Errorf("pgid %d (%s) is NOT refused, but the delivery spelling kill(-pgid, sig) hands the kernel"+
				" %d — %s; SIGKILL cannot be caught, so no handler can soften a self-kill there",
				c.pgid, c.name, -c.pgid, c.why)
		}
	}

	foreign := own + me + kbafForeignOffset
	for _, guarded := range []int{own, me, kbafOwnGroupID, kbafBroadcastID, -me} {
		if foreign == guarded {
			t.Fatalf("the foreign probe %d collides with the guarded value %d", foreign, guarded)
		}
	}
	if killChildTreeGroupIsTheCallers(foreign) {
		t.Errorf("pgid %d is refused although neither it nor its negation names this process (%d), its group"+
			" (%d), or the two addressing forms 0 and 1", foreign, me, own)
	}
}
