package testutil

// Tests for the pid+starttime+cmdline triple.
//
// The triple exists because a bare kill(pid, 0) lies. Each case below is a
// MEASURED lie: a live process whose stored starttime/cmdline does not match, a
// zombie that answers signal 0, and a /proc line whose comm field breaks a naive
// field split.

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// procTestPollInterval is the poll cadence of this file's waits.
	procTestPollInterval = 10 * time.Millisecond
	// procTestPollTries bounds each wait (tries × interval).
	procTestPollTries = 400
)

// TestProcIdentityRefusesASignalledRecycledPID is the pid-reuse guard with a
// REAL live process: its identity is captured, then deliberately aged by one
// clock tick (the shape a recycled pid has), and the stale identity must neither
// report the process alive nor signal it.
func TestProcIdentityRefusesASignalledRecycledPID(t *testing.T) {
	// A REAL long-lived process, spawned through the package's own sanctioned
	// helper (so its own reaper owns it) — the triple is asserted against a
	// genuine /proc entry, never a synthesised one.
	mock := StartMockProcess(t, MockDaemonBinary, MockDaemonHoldSeconds)
	// CAPTURE through AwaitProcIdentity, never a bare read: a just-spawned
	// child's /proc/<pid>/cmdline is EMPTY for the first ~0.4–1.0 ms after Start
	// returns (measured; see ArgvUnpublished), so a bare read stores an empty
	// cmdline and this test's own re-check then reports REUSE for one
	// incarnation — the flaky contradiction this case was diagnosed from.
	live, err := AwaitProcIdentity(mock.Pid())
	if err != nil {
		t.Fatalf("read the identity of pid %d: %v", mock.Pid(), err)
	}
	if live.StartTime == 0 {
		t.Fatalf("starttime of a live process must not be 0: %s", live)
	}
	if live.Cmdline == "" {
		t.Fatalf("the captured identity of the just-spawned child has an EMPTY cmdline (%s) — the capture must never return an unpublished argv", live)
	}

	// (a) starttime mismatch — the pid slot was reused.
	stale := live
	stale.StartTime++
	if alive, reason := stale.Alive(); alive {
		t.Errorf("a stale starttime reported pid %d alive (%s) — pid reuse must be caught", stale.Pid, reason)
	} else if !strings.Contains(reason, "REUSED") {
		t.Errorf("the reason must name PID REUSE, got %q", reason)
	}
	if err := KillIfIdentical(stale, syscall.SIGTERM); err == nil {
		t.Errorf("KillIfIdentical signalled pid %d from a stale identity — a recycled pid must never be signalled", stale.Pid)
	}
	if !SignalZeroAlive(mock.Pid()) {
		t.Fatalf("the mocked process %d is gone — the stale identity must not have killed a live process", mock.Pid())
	}

	// (b) cmdline mismatch — same starttime, different program.
	other := live
	other.Cmdline += " --not-this-one"
	if alive, reason := other.Alive(); alive {
		t.Errorf("a cmdline mismatch reported pid %d alive (%s)", other.Pid, reason)
	} else if !strings.Contains(reason, "REUSED") {
		t.Errorf("the reason must name the identity change, got %q", reason)
	}

	// (c) the TRUE identity is alive, and kills what it names.
	if alive, reason := live.Alive(); !alive {
		t.Fatalf("the captured identity of a live pid %d is not alive: %s", live.Pid, reason)
	}
	if err := KillIfIdentical(live, syscall.SIGTERM); err != nil {
		t.Fatalf("KillIfIdentical on the true identity: %v", err)
	}
	if !awaitGone(live) {
		t.Errorf("pid %d survived the SIGTERM its own identity sent", live.Pid)
	}
}

// TestProcIdentityReportsAZombieAsDead closes the second lie: signal 0 succeeds
// for a zombie whose parent has not waited yet.
func TestProcIdentityReportsAZombieAsDead(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the soon-to-be zombie: %v", err)
	}
	pid := cmd.Process.Pid
	id, err := ReadProcIdentity(pid)
	if err != nil {
		t.Fatalf("read identity of pid %d: %v", pid, err)
	}
	// Let the child exit WITHOUT waiting for it: it becomes a zombie, and the
	// pid entry keeps answering signal 0 — the lie the state letter defeats.
	if !awaitZombie(id) {
		t.Fatalf("pid %d never became a zombie; the case cannot be observed", pid)
	}
	if !SignalZeroAlive(pid) {
		t.Logf("note: this kernel stopped answering signal 0 for the zombie pid %d", pid)
	}
	if alive, reason := id.Alive(); alive {
		t.Errorf("a zombie pid %d reported alive (%s) — signal 0 alone would have called it live", pid, reason)
	} else if !strings.Contains(reason, "zombie") {
		t.Errorf("the reason must name the zombie state, got %q", reason)
	}
	if err := KillIfIdentical(id, syscall.SIGTERM); err == nil {
		t.Logf("note: signalling a zombie pid %d succeeded on this kernel (no-op)", pid)
	}
	_ = cmd.Wait() //stylegate:ignore reap the process under test now that the case is over; the identity verdict was already made
}

// TestParseProcStatHandlesACommWithParensAndSpaces pins the parser's one real
// hazard: field 2 is parenthesized and may itself contain spaces AND ')'.
func TestParseProcStatHandlesACommWithParensAndSpaces(t *testing.T) {
	// A realistic line: 52 fields, the first of them field 3 (state) because the
	// comm field is the parenthesized field 2. Index 19 is therefore field 22.
	const (
		field22Index = 19 // 0-based index of field 22 in the post-comm slice
		statFields   = 50 // fields 3..52
	)
	fields := make([]string, statFields)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "R"    // field 3, state
	fields[1] = "1"    // field 4, ppid
	fields[2] = "900"  // field 5, pgrp
	fields[3] = "4242" // field 6, session (== pid ⇒ session leader)
	fields[field22Index] = "987654"
	raw := "4242 (gopi worker ) (v2)) " + strings.Join(fields, " ")
	id, err := parseProcStat(4242, raw)
	if err != nil {
		t.Fatalf("parse a legal stat line: %v", err)
	}
	if id.StartTime != 987654 {
		t.Errorf("starttime = %d, want 987654 — the parser split on the wrong ')'", id.StartTime)
	}
	if id.State != 'R' || id.PPID != 1 || id.Session != 4242 {
		t.Errorf("state/ppid/session = %c/%d/%d, want R/1/4242", id.State, id.PPID, id.Session)
	}
	if !id.Detached() {
		t.Errorf("session == pid must report Detached, got session %d pid %d", id.Session, id.Pid)
	}
}

// TestProcIdentityOrphanIsJudgedByTheReapingAncestorNotByInit is the
// regression guard for the predicate's ANCHOR, and it exists because the anchor
// was a literal for a long time: `PPID == 1` is not the mechanism ("nobody is
// left to reap me"), it is one host's spelling of it. On a host running a
// systemd user session the kernel's child-subreaper of user.slice is
// `systemd --user`, so a detached, abandoned process is reparented to THAT
// manager and carries ppid 3100 — measured on this host, where a real setsid'd
// `sleep 600` reported `ppid=3100 pgrp=<self> session=<self>`.
//
// The table therefore asserts the MECHANISM in both directions, which the
// literal could not express at all: reparenting to the host's reaper IS a leak
// (whichever reaper that is), and a ppid that merely LOOKS like the other
// host's reaper is NOT one. No process is spawned here — the cases are pure
// inputs to the predicate, and the LIVE proof lives in TestOrphanSweep*.
func TestProcIdentityOrphanIsJudgedByTheReapingAncestorNotByInit(t *testing.T) {
	const (
		// reaperInitValue is a plain host's reaper.
		reaperInitValue = 1
		// reaperManagerValue is this host's reaper — the pid MEASURED for
		// `systemd --user` (PID 3100, /lib/systemd/systemd --user), named here
		// the way the incident named it and never treated as a portable value.
		reaperManagerValue = 3100
		// strangerPid is any pid that is nobody's reaper, used as "a live
		// parent that is still holding the process".
		strangerPid = 4242
		// markedAncestorValue stands for the pid of an ancestor that carries
		// PR_SET_CHILD_SUBREAPER and is neither init nor a systemd user manager
		// — a run wrapper, a multiplexer, any program that called prctl(2). It
		// is deliberately a value that is NOT this host's reaper, because the
		// case it feeds must fail if the predicate can only recognize reapers
		// by name (bug 01a12d18).
		markedAncestorValue = 2711
	)
	cases := []struct {
		name string
		id   ProcIdentity
		want bool
	}{
		{
			name: "reparented to init — a plain host's reaper",
			id:   ProcIdentity{Pid: strangerPid, PPID: reaperInitValue, ProcReaper: reaperInitValue, ProcReaperChain: []int{reaperInitValue}},
			want: true,
		}, {
			name: "reparented to the user session's manager — THIS host's reaper",
			id:   ProcIdentity{Pid: strangerPid, PPID: reaperManagerValue, ProcReaper: reaperManagerValue, ProcReaperChain: []int{reaperManagerValue, reaperInitValue}},
			want: true,
		},
		{
			name: "a live parent that is not a reaper — the spawner is still holding it",
			id:   ProcIdentity{Pid: strangerPid, PPID: strangerPid - 1, ProcReaper: reaperManagerValue, ProcReaperChain: []int{reaperManagerValue, reaperInitValue}},
			want: false,
		},
		{
			name: "the manager is NOT a reaper on a host whose reaper is init (the value is not portable)",
			id:   ProcIdentity{Pid: strangerPid, PPID: reaperManagerValue, ProcReaper: reaperInitValue, ProcReaperChain: []int{reaperInitValue}},
			want: false,
		},
		{
			name: "an unmeasured chain refuses to judge — never a silent fall back to init",
			id:   ProcIdentity{Pid: strangerPid, PPID: reaperInitValue, ProcReaper: 0},
			want: false,
		},
		{
			// THE ANCESTRY ARM (bug 01a12d18): the kernel reparented the process
			// to the NEAREST marked ancestor, which is neither init nor
			// systemd --user and therefore is not in any name set — but it IS on
			// this run's own chain, which is the fact that makes it ours.
			name: "reparented to a marked subreaper ancestor the reaper set cannot name",
			id:   ProcIdentity{Pid: strangerPid, PPID: markedAncestorValue, ProcReaper: strangerPid - 2, ProcReaperChain: []int{strangerPid - 2, markedAncestorValue, reaperInitValue}},
			want: true,
		},
		{
			// The other half of the same arm: a chain was stamped, but this
			// parent is NOT on it — a third party's process, left alone however
			// orphan-shaped it looks.
			name: "a parent off this run's chain is not ours, even when its ppid is meaningless",
			id:   ProcIdentity{Pid: strangerPid, PPID: markedAncestorValue, ProcReaper: strangerPid - 2, ProcReaperChain: []int{strangerPid - 2, reaperInitValue}},
			want: false,
		},
		{
			// The FAIL-CLOSED half: with no chain there is nothing to judge
			// against, and the name arm alone is the enumeration that failed —
			// so a caller that forgot to measure refuses instead of guessing.
			name: "a chain that was never measured refuses, even when the name arm would say yes",
			id:   ProcIdentity{Pid: strangerPid, PPID: reaperInitValue, ProcReaper: reaperInitValue, ProcReaperChain: nil},
			want: false,
		},
	}
	for _, c := range cases {
		if got := c.id.Orphan(); got != c.want {
			t.Errorf("%s: Orphan() = %v, want %v (ppid=%d, reaper=%d)", c.name, got, c.want, c.id.PPID, c.id.ProcReaper)
		}
	}
}

// TestProcReaperOfNamesARealAncestorOfThisRun is the LIVE half of the anchor
// above: the mechanism is not asserted from a table here, it is MEASURED, and
// the two properties that make it a mechanism rather than a coincidence are
// both checked —
//
//  1. the named reaper is a real process, and it IS an ANCESTOR of this test
//     (the kernel reparents an orphaned child to the nearest marked ancestor,
//     so a reaper that is not above us could not be the one that reaps our
//     leaks); and
//  2. it has one of the two known identities: init, or a systemd manager
//     carrying `--user` on its own argv.
//
// A host where neither holds returns (0, reason) from ProcReaperOf and this case
// reports that refusal as its evidence, rather than passing on a value it never
// validated.
func TestProcReaperOfNamesARealAncestorOfThisRun(t *testing.T) {
	reaper, reason := ProcReaperOf()
	if reaper <= 0 {
		t.Fatalf("ProcReaperOf() could not name this host's reaper: %s — the sweep refuses to judge rather than assuming init", reason)
	}
	if reaper == os.Getpid() {
		t.Fatalf("ProcReaperOf() named this very process (%d) as its own reaper — a process cannot be its own reaping ancestor", reaper)
	}
	assertAncestorOfThisProcess(t, reaper)

	id, err := ReadProcIdentity(reaper)
	if err != nil {
		t.Fatalf("read the identity of the named reaper %d: %v", reaper, err)
	}
	comm, err := readProcComm(reaper)
	if err != nil {
		t.Fatalf("read the comm of the named reaper %d: %v", reaper, err)
	}
	argv, err := readProcArgv(reaper)
	if err != nil {
		t.Fatalf("read the argv of the named reaper %d: %v", reaper, err)
	}
	userManager := comm == procname.UserManagerComm && slices.Contains(argv, procname.UserManagerArg)
	if reaper != procname.OrphanReparentPPID && !userManager {
		t.Fatalf("the named reaper %d is neither init (%d) nor a systemd user manager (comm=%q argv=%v) — the reaper set must only ever contain a real reaper",
			reaper, procname.OrphanReparentPPID, comm, argv)
	}
	t.Logf("this host's reaper is pid %d: comm=%q argv=%v (%s)", reaper, comm, argv, id)
}

// assertAncestorOfThisProcess walks up from this process's own parent and fails
// unless pid appears, reporting the chain it walked so a mismatch is diagnosable
// rather than mysterious.
func assertAncestorOfThisProcess(t *testing.T, pid int) {
	t.Helper()
	const maxLevels = 40
	chain := []int{os.Getpid()}
	cur := os.Getpid()
	for i := 0; i < maxLevels; i++ {
		id, err := ReadProcIdentity(cur)
		if err != nil {
			break
		}
		if id.PPID <= 0 {
			break
		}
		chain = append(chain, id.PPID)
		if id.PPID == pid {
			t.Logf("ancestry: %v", chain)
			return
		}
		cur = id.PPID
	}
	t.Fatalf("the named reaper %d is not in this process's ancestry %v — it could not reap a child this process abandons", pid, chain)
}
func awaitZombie(id ProcIdentity) bool {
	for i := 0; i < procTestPollTries; i++ {
		now, err := ReadProcIdentity(id.Pid)
		if err != nil {
			return false
		}
		if now.Zombie() {
			return true
		}
		time.Sleep(procTestPollInterval) //stylegate:allow-sleep bounded poll of a /proc entry
	}
	return false
}

// awaitGone polls the triple until the process is gone.
func awaitGone(id ProcIdentity) bool {
	for i := 0; i < procTestPollTries; i++ {
		if alive, _ := id.Alive(); !alive {
			return true
		}
		time.Sleep(procTestPollInterval) //stylegate:allow-sleep bounded poll of a /proc entry
	}
	return false
}

// TestProcDescendsFromIsTheOwnershipTestAnEnvMatchIsNot is the regression guard
// for the SIGINT defect measured 2026-10-27/28: five live `gopi` processes in
// OTHER sessions were signalled by an `agent.test` sender whose only reason to
// believe the target was "our executor child" was that the target's environment
// carried the row's GOPI_TASK_WRAPPER_ID. That variable is INHERITED, so it
// names a family, not an owner.
//
// The predicate is measured against REAL /proc entries, in both directions:
//   - a child THIS test spawned IS its descendant (the true positive the walk
//     must not lose — a guard that refuses everything turns every walk into a
//     timeout, which is the other half of the same defect);
//   - an ANCESTOR (this process's own parent) is NOT a descendant of this
//     process, and neither is pid 1 — the shape a stranger's process has when
//     it is scanned by pid and happens to carry the same environment.
func TestProcDescendsFromIsTheOwnershipTestAnEnvMatchIsNot(t *testing.T) {
	mock := StartMockProcess(t, MockDaemonBinary, MockDaemonHoldSeconds)
	if mine, why := ProcDescendsFrom(mock.Pid(), os.Getpid()); !mine {
		t.Fatalf("the child this test spawned (pid %d) is not reported as a descendant of pid %d: %s — the walk must keep the true positive", mock.Pid(), os.Getpid(), why)
	}

	parent := os.Getppid()
	if parent > 0 {
		if mine, _ := ProcDescendsFrom(parent, os.Getpid()); mine {
			t.Errorf("pid %d is this process's PARENT and was reported as its descendant — the direction of the walk is wrong, so every stranger above us would read as owned", parent)
		}
	}
	if mine, _ := ProcDescendsFrom(1, os.Getpid()); mine {
		t.Errorf("pid 1 was reported as a descendant of pid %d — no process on this host is", os.Getpid())
	}
	if mine, _ := ProcDescendsFrom(os.Getpid(), os.Getpid()); mine {
		t.Errorf("ProcDescendsFrom(p, p) reported true — a process is not its own descendant, and allowing it would let a scanner's self-match pass as ownership")
	}
	// A pid that is not a process id is a REFUSAL with a reason, never a silent
	// "not mine": the difference is what a caller logs versus what it skips.
	if _, why := ProcDescendsFrom(0, os.Getpid()); why == "" {
		t.Errorf("pid 0 was judged with no reason reported — an unanswerable target must say why (Rule 15b)")
	}
}
