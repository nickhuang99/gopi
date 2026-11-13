package tools

import (
	"syscall"
	"testing"
)

const (
	// killGuardIncarnationBefore is the incarnation a decision-time read answered
	// for the child.
	killGuardIncarnationBefore = "887766"
	// killGuardIncarnationRecycled is what the SAME number reads after the
	// kernel reaped the child and handed the pid to an unrelated process. Two
	// distinct values are what an incarnation check exists to compare.
	killGuardIncarnationRecycled = "991122"
)

// bash_kill_group_recycle_test.go — THE GROUP ID MAY CHANGE OWNER BETWEEN THE
// MEASUREMENT AND THE DELIVERY.
//
// THE DEFECT SHAPE (measured 2026-10-24). killChildTree reads
// syscall.Getpgid(pid) at T1 and delivers syscall.Kill(-pgid, SIGKILL) at T4. A
// process-group id IS its leader's pid, so the kernel may reap the child between
// the two and recycle that number into an unrelated group — at which point the
// signal reaches strangers the caller never named.
//
// MEASURED INCIDENT behind these tests: 8 firefox processes exited in the same
// second ("Exiting due to channel error", snap.firefox…scope consumed) with no
// OOM record and no SIGKILL record — the shape of a GROUP SIGKILL, which cannot
// be caught. A `make test-budget` run over internal/tools and internal/agent was
// live at that second, and the pid ranges overlap (firefox 1047211..1219592 vs
// the suite's children 1084347..1223658).
//
// WHAT IS PINNED HERE. The delivery must be conditional on a reading taken
// AFTER the decision (killChildTreeGroupStillTheChilds): the child still alive,
// still its own group leader, the group id still equal to the pid. When any of
// that has moved, NO group signal may be sent and the caller must degrade to the
// per-pid path.

// TestKillChildTreeRefusesAGroupSignalWhenTheGroupIdMoved recreates the recycle
// window through the seam: the group was measured as the child's own, then the
// child is gone and the number belongs to someone else. No group signal may be
// delivered.
func TestKillChildTreeRefusesAGroupSignalWhenTheGroupIdMoved(t *testing.T) {
	groupSignals := recordGroupSignals(t)

	orig := killChildTreeGroupStillTheChilds
	killChildTreeGroupStillTheChilds = func(pid, pgid int, incarnation string) bool { return false }
	t.Cleanup(func() { killChildTreeGroupStillTheChilds = orig })

	// A live fixture whose group really is its own: without the re-verification
	// this pid takes the group branch (that is the positive control below).
	fixture := startKillGuardFixture(t)
	if own := killChildTreeGroupOf(); fixture.pid == own {
		t.Skipf("the fixture's group equals this process's group (%d): a foreign group is required", own)
	}

	killChildTree(fixture.pid)

	// why: the audited set is the calls killChildTreeGroupSignal saw; the per-pid
	// kill is expected and asserted by the fixture's teardown, so the action verb
	// names the one call whose ABSENCE is the subject.
	if got := groupSignals(); len(got) != 0 {
		t.Fatalf("killChildTree sent a group signal to %v — the re-verification refused the group, so the delivery must not happen: a recycled group id reaches strangers and SIGKILL cannot be caught", got)
	}
}

// TestKillChildTreeStillSignalsAGroupItReverified is the POSITIVE CONTROL: a
// re-verification that passes must NOT swallow the group branch, because that
// branch is what takes `nohup … &`-style descendants down (losing it reinstates
// bug011's deadlock).
func TestKillChildTreeStillSignalsAGroupItReverified(t *testing.T) {
	fixture := startKillGuardFixture(t)
	groupSignals := recordGroupSignals(t)

	if own := killChildTreeGroupOf(); fixture.pid == own {
		t.Skipf("the fixture's group equals this process's group (%d): the positive control needs a foreign group", own)
	}
	incarnation := liveIncarnation(t, fixture.pid)
	if !killChildTreeGroupStillTheChilds(fixture.pid, fixture.pid, incarnation) {
		t.Fatal("the re-verification refused a live child that really leads its own group — the group branch would be dead code")
	}

	killChildTree(fixture.pid)

	if got := groupSignals(); len(got) != 1 || got[0] != fixture.pid {
		t.Fatalf("group signals = %v, want exactly [%d] — a re-verified child-owned group must still be addressed as a group", got, fixture.pid)
	}
}

// TestKillChildTreeGroupStillTheChildsReadsTheLiveTable pins the predicate to
// the MEASUREMENT rather than to a value it could assume.
func TestKillChildTreeGroupStillTheChildsReadsTheLiveTable(t *testing.T) {
	fixture := startKillGuardFixture(t)

	incarnation := liveIncarnation(t, fixture.pid)
	if !killChildTreeGroupStillTheChilds(fixture.pid, fixture.pid, incarnation) {
		t.Fatalf("pid %d leads its own group and is alive — the predicate answered false", fixture.pid)
	}

	// A pid that cannot name a group: the guard must refuse rather than trust it.
	for _, bad := range [][2]int{{0, 0}, {-1, -1}, {fixture.pid, 0}} {
		if killChildTreeGroupStillTheChilds(bad[0], bad[1], incarnation) {
			t.Fatalf("predicate accepted pid=%d pgid=%d — a non-positive id is not a group to address", bad[0], bad[1])
		}
	}

	// An UNREADABLE incarnation is a refusal, never a pass (Rule 42).
	if killChildTreeGroupStillTheChilds(fixture.pid, fixture.pid, "") {
		t.Fatalf("predicate accepted an empty incarnation for child %d — a process whose identity could not be measured is not one this caller has shown it owns", fixture.pid)
	}

	// The group id no longer matching the child's own: refused.
	if killChildTreeGroupStillTheChilds(fixture.pid, fixture.pid+1, incarnation) {
		t.Fatalf("predicate accepted pgid=%d for child %d — a group id that is not the child's own must be refused", fixture.pid+1, fixture.pid)
	}
}

// TestKillChildTreeGroupStillTheChildsRefusesAGoneChild: once the child is gone
// the number names nothing (or a stranger), so the group signal must be refused.
func TestKillChildTreeGroupStillTheChildsRefusesAGoneChild(t *testing.T) {
	fixture := startKillGuardFixture(t)
	pid := fixture.pid
	incarnation := liveIncarnation(t, pid)
	_ = fixture.cmd.Process.Kill()    //stylegate:ignore the fixture is teardown: the subject is the predicate's reading of a gone pid
	_, _ = fixture.cmd.Process.Wait() //stylegate:ignore the exit status is not the subject; the pid being gone is

	if killChildTreeGroupStillTheChilds(pid, pid, incarnation) {
		t.Fatalf("predicate accepted pid=%d after it was reaped — a gone child's group id may already have been recycled", pid)
	}
	_ = syscall.Getpid() // the import is used by the positive control above via killChildTreeGroupOf
}

// liveIncarnation reads a fixture's start ticks through the PRODUCTION seam, so
// a test that compares "the value taken before" with "the value read at
// delivery" is comparing the same measurement production compares.
func liveIncarnation(t *testing.T, pid int) string {
	t.Helper()
	ticks, ok := killChildTreeGroupStartTicks(pid)
	if !ok || ticks == "" {
		t.Fatalf("the fixture pid %d has no readable incarnation — the positive control would be vacuous", pid)
	}
	return ticks
}

// pinIncarnationSequence swaps the incarnation seam for one that answers a fixed
// SEQUENCE (the value read by the decision, then the value read at delivery) and
// restores it on cleanup.
func pinIncarnationSequence(t *testing.T, seq ...string) {
	t.Helper()
	orig := killChildTreeGroupStartTicks
	i := 0
	killChildTreeGroupStartTicks = func(int) (string, bool) {
		if i >= len(seq) {
			return "", false
		}
		v := seq[i]
		i++
		if v == "" {
			return "", false
		}
		return v, true
	}
	t.Cleanup(func() { killChildTreeGroupStartTicks = orig })
}

// TestKillChildTreeRefusesAGroupSignalWhenTheIncarnationChanged is THE RECYCLE
// CASE the group-id probes cannot see: the number is still a live group leader,
// but it is a DIFFERENT process than the child the caller spawned.
//
// WHY THIS IS THE DEFECT AND NOT A HYPOTHETICAL. A pgid IS its leader's pid, so
// between the measurement (Getpgid(pid) == pid) and the delivery (kill(-pgid))
// the kernel may reap the child and hand that number to an unrelated group. The
// two probes in killChildTreeGroupStillTheChilds are BOTH satisfied by the new
// holder — it leads a group of its own, and kill(pid, 0) succeeds because it is
// alive — so before this arm the predicate answered TRUE and a SIGKILL went to
// strangers. The incarnation is the only reading that distinguishes the two
// processes, and an unmeasurable one must degrade rather than address.
func TestKillChildTreeRefusesAGroupSignalWhenTheIncarnationChanged(t *testing.T) {
	groupSignals := recordGroupSignals(t)
	fixture := startKillGuardFixture(t)
	if own := killChildTreeGroupOf(); fixture.pid == own {
		t.Skipf("the fixture's group equals this process's group (%d): a foreign group is required", own)
	}
	// What the child's incarnation was when the decision was taken, and what the
	// same NUMBER reads at delivery: the process now living there is not it.
	pinIncarnationSequence(t, killGuardIncarnationBefore, killGuardIncarnationRecycled)

	killChildTree(fixture.pid)

	// why: the audited set is the calls killChildTreeGroupSignal saw — the
	// per-pid kill is expected and is asserted by the fixture's own teardown, so
	// the action verb names the one call whose ABSENCE is the subject.
	if got := groupSignals(); len(got) != 0 {
		t.Fatalf("killChildTree sent a group signal to %v — the number is a live group leader but a DIFFERENT process, so the signal reaches a stranger the caller never named (SIGKILL cannot be caught)", got)
	}
}

// TestKillChildTreeRefusesAGroupSignalWhenIncarnationIsUnreadable: a process
// whose identity cannot be measured is not one the caller has shown it owns, so
// the group branch must not be taken (Rule 42 — signal only what you hold).
func TestKillChildTreeRefusesAGroupSignalWhenIncarnationIsUnreadable(t *testing.T) {
	groupSignals := recordGroupSignals(t)
	fixture := startKillGuardFixture(t)
	if own := killChildTreeGroupOf(); fixture.pid == own {
		t.Skipf("the fixture's group equals this process's group (%d): a foreign group is required", own)
	}
	pinIncarnationSequence(t, "", "")

	killChildTree(fixture.pid)

	// why: same audited set as the case above — the absence of a GROUP signal is
	// the subject, and the child's own death on the per-pid path is expected.
	if got := groupSignals(); len(got) != 0 {
		t.Fatalf("killChildTree sent a group signal to %v with no readable incarnation — an unverifiable group is not the caller's to address", got)
	}
}
