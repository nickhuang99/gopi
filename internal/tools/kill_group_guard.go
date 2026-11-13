package tools

import "syscall"

// killGroupGuard.go — ONE re-verification before ANY address-a-number group
// signal, shared by every call site that used to assume pid == pgid forever.
//
// THE DEFECT (measured 2026-10-24, incident devdoc/operator-apps-killed-during-suite-runs.md).
// A process-group id IS its leader's pid, so `syscall.Kill(-cmd.Process.Pid, …)`
// is correct at the instant `cmd.Start()` returns and only then. Between the
// decision to kill and the delivery, the kernel may reap the child and hand that
// number to an unrelated group — after which the signal reaches strangers, and
// because SIGKILL cannot be caught no victim writes a death record. The measured
// case: the operator's 8 firefox processes all exited in the same second during a
// suite run, with no OOM and no SIGKILL record anywhere, while the suite's child
// pid range overlapped theirs.
//
// THE REMEDY, and why it is a second READING rather than a lock: the window
// cannot be closed by holding anything (the kernel recycles the number without
// asking us). What CAN be done is to make delivery conditional on a reading
// taken AFTER the decision. The group is signalled only when the pid still names
// a live process AND that process still leads exactly the group being addressed;
// otherwise the caller falls back to the per-pid path, which reads /proc live and
// therefore cannot reach a stranger.
//
// bash.go's killChildTreeGroupStillTheChilds is the same predicate, kept as a var
// seam there because its refused path is what that file's tests observe. This is
// the shared production form the other thirteen call sites use.

// killGroupStillTheChilds re-reads the live process table immediately before a
// group signal is delivered. It answers "is this group id STILL this child's",
// which is the only question that makes `kill(-pgid, sig)` safe to send.
//
// A false answer means the caller must NOT send the group signal; it should
// signal the pid it holds (or iterate the descendants by a live /proc read).
func killGroupStillTheChilds(pid, pgid int) bool {
	if pid <= 0 || pgid <= 0 {
		return false
	}
	if now, err := syscall.Getpgid(pid); err != nil || now != pgid {
		return false
	}
	// The pid behind the number must still be our child, not a recycled stranger
	// that happens to lead a group of its own.
	return syscall.Kill(pid, 0) == nil
}

// signalProcessGroup sends sig to the group `pgid` ONLY when that group is
// provably still the child's; otherwise it degrades to signalling the pid this
// caller holds, and reports which arm ran.
//
// The degradation is deliberately the PID path: a child that died mid-kill has no
// reachable grandchildren left, so the group arm would have addressed a stranger
// to reach processes that no longer exist.
func signalProcessGroup(pid, pgid int, sig syscall.Signal) (group bool, err error) {
	if killGroupStillTheChilds(pid, pgid) {
		return true, syscall.Kill(-pgid, sig)
	}
	//stylegate:ignore best-effort direct kill of the child we started; ESRCH (already gone) is expected
	_ = syscall.Kill(pid, sig)
	return false, nil
}
