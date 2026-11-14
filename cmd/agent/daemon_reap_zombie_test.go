package main

// daemon_reap_zombie_test.go — the regression guard for the ZOMBIE the daemon
// spawn seam used to leave behind (task 01a132b0, measured 2026-10-13).
//
// THE DEFECT. daemonSpawn forks the detached daemon and owns one `Wait` — but
// only on the FAILURE path (daemonScopeAbandon). On the SUCCESS path nobody ever
// reaped the child. That is invisible to a launcher that exits immediately (the
// child is re-parented to init, which reaps it), and PERMANENT for a caller that
// outlives the start: a test binary, a REPL, a launcher that starts two daemons.
// The child's exit then leaves `state=Z`, `/proc/<pid>/comm` still reading
// `gopi` while cmdline/exe are empty — a process every comm-based census on the
// host counts (`scripts/gopi-worker-up.sh --count` reported it as an "other gopi
// process"; the cmd/agent suite's own close-out sweep named it
// `leaked child pid=… state=Z`).
//
// ATTRIBUTION (measured with a PASSIVE /proc sampler, never an injected probe):
//
//	pid=738103 comm=[gopi] state=Sl ppid=733871 ppid_comm=[agent.test]
//	cmdline=[…/gopi-suite-bin-…/gopi worker]                       ← alive phase
//	pid=649678 comm=gopi state=Z ppid=645638 ppid_comm=agent.test  ← after exit
//
// WHY THE GUARD DRIVES THE REAL SEAM (Rule 31). A unit test that faked the
// daemon would assert a copy of the spawn logic. This arm calls the SAME
// production entry point the restart path calls (workerDaemonSpawnErr) with the
// SAME published binary, IN-PROCESS — so this test binary is the child's parent,
// which is exactly the relation the defect needs to reproduce.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/flaketest"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// drzStatFileName / drzCommFileName are the two procfs entries this guard
	// reads, named (Rule 1b) so no call site spells them.
	drzStatFileName = "stat"
	drzCommFileName = "comm"
	// drzZombieProbeBound is how long the arm looks for the zombie. It is a wait
	// for an OBSERVATION, never a load (Rule 27), and it is generous on purpose:
	// the fixed seam reaps within milliseconds of the daemon's exit, while a
	// tree without the reap keeps the Z entry for the whole bound.
	drzZombieProbeBound = 15 * time.Second
	// drzZombieProbeStep is that observation's polling period.
	drzZombieProbeStep = 50 * time.Millisecond
	// drzZombieCollectGrace is how long a Z entry may PERSIST before this arm
	// calls it a leak — and it is the whole correction of this guard (task
	// 01a13cdb, 2026-10-15).
	//
	// WHY A TRANSIENT Z IS NOT THE DEFECT (measured). A child that exits becomes
	// Z the moment the kernel releases it, and stays Z until its parent's Wait
	// collects it: that window is what `wait(2)` semantics ARE, so no seam can
	// remove it, however eagerly it reaps. What the missing reap produced was a
	// PERMANENT Z — the entry never left /proc while the parent lived.
	//
	// This arm used to fail on the FIRST Z reading, and its first poll is
	// immediate, so it was a race against a window of ~1 ms: measured on this
	// host, 5 instrumented arms through the real seam showed the Z entry
	// appearing at +1.16..+1.21 ms after SIGKILL and leaving /proc at
	// +2.05..+2.86 ms — i.e. a transient of ~0.9..1.2 ms. Polling at that
	// granularity lands inside the window occasionally: measured 2 FAIL of 120
	// runs in ONE process (-count=120, 0.09s / 0.13s elapsed), each with the
	// FIRST poll already reading Z, against 0 FAIL of 118 archived multi-package
	// streams. That is this guard's own flake, not a reap defect.
	//
	// The grace is therefore set ~700-1700x above the LARGEST measured transient
	// (2.86 ms) so the verdict cannot turn on scheduler luck, while a tree
	// without the reap — where the entry persists for the whole bound — still
	// fails, now naming persistence rather than a first sighting.
	drzZombieCollectGrace = 2 * time.Second
	// drzIdleExits / drzIdleSec keep the daemon this arm starts short-lived by
	// the LOOP'S OWN knobs (never a test-only bypass), so nothing is left
	// running when the case ends. They are values fed to existing knobs, not new
	// ones.
	drzIdleExits = "1"
	drzIdleSec   = "1"
)

// drzPidState reads pid's process state letter from procfs. ok=false means the
// entry could not be read (the process is gone, or /proc raced) — a fact the
// caller must keep SEPARATE from "readable and alive" (Rule 15b).
func drzPidState(pid int) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(procRootDir, strconv.Itoa(pid), drzStatFileName))
	if err != nil {
		return "", false
	}
	s := string(raw)
	i := drzLastParen(s)
	if i < 0 || i+2 >= len(s) {
		return "", false
	}
	return string(s[i+2]), true
}

// drzLastParen returns the index of the last ')' in s — the boundary between
// stat's comm field (which may itself contain spaces and parentheses, e.g.
// `(gopi)`) and the rest of the line. A named helper because the naive
// `strings.LastIndex(s, ") ")` misses the single-space case.
func drzLastParen(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ')' {
			return i
		}
	}
	return -1
}

// drzPidPPID reads pid's parent pid from the SAME procfs stat line the state
// letter comes from (field 4, immediately after the state letter).
//
// It exists because the arm must MEASURE the parent it blames instead of
// assuming it (Rule 24: a liveness/parentage fact is read from /proc, never
// inferred). os.Getpid() is this process's identity, not evidence about the
// child's parent field — and the two can disagree, e.g. when a launcher shape
// inserts a fork between them.
func drzPidPPID(pid int) (int, bool) {
	raw, err := os.ReadFile(filepath.Join(procRootDir, strconv.Itoa(pid), drzStatFileName))
	if err != nil {
		return 0, false
	}
	s := string(raw)
	i := drzLastParen(s)
	if i < 0 || i+2 >= len(s) {
		return 0, false
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

// TestStartedDaemonIsReapedSoItsParentLeavesNoZombie is the regression guard.
//
// It starts a REAL detached worker daemon through the spawn seam, in-process, so
// this test binary is the child's parent — the relation a suite has. It then
// stops the daemon by EXPLICIT pid (never a pattern: the operator has live
// daemons on this host, Rule 24) and measures the parent's side of the relation:
// the child is dead, so what happened to its /proc entry?
//
// RED on a tree without the reap: the daemon exits and its Z entry PERSISTS for
// the whole bound (see the report's before/after samples). GREEN with the fix:
// the seam's reap goroutine collects the exit within milliseconds, so
// /proc/<pid> disappears rather than becoming a zombie.
func TestStartedDaemonIsReapedSoItsParentLeavesNoZombie(t *testing.T) {
	flaketest.Declare("deterministic: this arm used to fail on the FIRST reading of state=Z, and its first poll is immediate, so it raced the child's COLLECT WINDOW — the interval in which a just-exited child is legitimately Z until its parent's Wait reaches it. Measured through the real seam on this host, that window is ~1 ms wide (Z first readable at +1.16..+1.21 ms after SIGKILL, entry released at +2.05..+2.86 ms), while the poll starts at t=0, so the verdict turned on scheduler luck: 2 FAIL of 120 runs in ONE process (-count=120, 0.09s/0.13s elapsed, each with the first poll already reading Z) against 0 FAIL of 118 archived streams. Registered: testdata/flakes.tsv, owner 01a13cdb-2493-798a-9970-da1b13e85c95.")
	// The binary is resolved FIRST: gopiTestBinPath builds with `go build .`,
	// which needs to stand in the package directory, and the profile pin below
	// moves this process's cwd.
	bin := gopiTestBinPath(t)
	irdProfile(t)
	t.Setenv(runcmd.EnvBinary, bin)
	t.Setenv(workerIdleExitsEnv, drzIdleExits)
	t.Setenv(workerIdleIntervalEnv, drzIdleSec)

	if _, err := workerDaemonSpawnErr(nil); err != nil {
		t.Fatalf("in-process daemon start with %s=%s must complete: %v", runcmd.EnvBinary, bin, err)
	}
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve the worker state dir: %v", err)
	}
	pid := wdWaitFreshDaemon(t, dir)
	t.Cleanup(func() { wdStopPID(t, pid) })
	t.Logf("[arm] started daemon pid %d; this test binary (pid %d) is its parent", pid, os.Getpid())

	// The pid came out of the worker state dir (`wdWaitFreshDaemon`), not out of
	// a handle this test holds: read the incarnation and address only that one.
	daemonID, idErr := testutil.ReadProcIdentity(pid)
	if idErr != nil {
		t.Fatalf("read the daemon's identity (pid %d): %v — the arm cannot measure a child it cannot address", pid, idErr)
	}
	if err := testutil.KillIfIdentical(daemonID, syscall.SIGKILL); err != nil {
		t.Fatalf("stop daemon pid %d: %v — the arm cannot measure a child it did not end", pid, err)
	}

	deadline := time.Now().Add(drzZombieProbeBound)
	polls := 0
	// zSince is the first reading of the CURRENT contiguous Z sighting; a
	// reading that is not Z (or an unreadable entry) clears it, so only a
	// PERSISTENT Z — one that outlives drzZombieCollectGrace — is a leak.
	var zSince time.Time
	for time.Now().Before(deadline) {
		polls++
		state, ok := drzPidState(pid)
		if !ok {
			// Gone from /proc: the parent collected the exit and the entry was
			// released. This is the PASSING end state, whether or not the exit
			// was briefly visible as Z on the way (see drzZombieCollectGrace).
			elapsed := ""
			if !zSince.IsZero() {
				elapsed = fmt.Sprintf(" (its Z entry was visible for %v)",
					time.Since(zSince).Round(time.Microsecond))
			}
			t.Logf("[arm] daemon pid %d left /proc after %d poll(s) — reaped, no zombie%s", pid, polls, elapsed)
			return
		}
		if state == suiteProcZombieState {
			if zSince.IsZero() {
				zSince = time.Now()
			}
			if time.Since(zSince) >= drzZombieCollectGrace {
				// The exit has been visible as Z for longer than any collect
				// window can explain, and the parent is still alive: this is
				// the unreaped child the guard exists for. WHO the parent is is
				// MEASURED, never assumed (Rule 24) — a Z entry reparented to
				// init would be a different finding, and the message says which.
				ppid, ppidOK := drzPidPPID(pid)
				attribution := fmt.Sprintf("this test binary (pid %d) is the parent", os.Getpid())
				if !ppidOK {
					attribution = fmt.Sprintf("its parent is unreadable from /proc while this test binary is pid %d", os.Getpid())
				} else if ppid != os.Getpid() {
					attribution = fmt.Sprintf("its parent is pid %d, NOT this test binary (pid %d) — the reap owner is somebody else", ppid, os.Getpid())
				} else {
					attribution = fmt.Sprintf("its parent is pid %d (this test binary, read from stat's ppid field), which is still alive", ppid)
				}
				t.Fatalf("daemon pid %d has been a ZOMBIE (state=%q) for %v — well past any collect window — so the spawn seam never reaped the child it started; %s. "+
					"A zombie keeps comm=gopi and loses cmdline/exe, so every comm-based process census on this host counts it "+
					"(scripts/gopi-worker-up.sh --count reports an extra \"other gopi process\") — see devdoc/cmd-agent-daemon-reap.md",
					pid, state, time.Since(zSince).Round(time.Microsecond), attribution)
			}
		} else {
			zSince = time.Time{}
		}
		time.Sleep(drzZombieProbeStep) //stylegate:allow-sleep bounded poll for the reaped child to leave /proc (the daemon's exit fires no event this test can subscribe to)
	}
	state, ok := drzPidState(pid)
	rendered := state
	if !ok {
		rendered = "(unreadable)"
	}
	t.Fatalf("daemon pid %d was still present in /proc in state %q after %s (%d polls): the arm expected the reap to "+
		"release the entry — a Z state here is the defect, any other state means the daemon did not exit",
		pid, rendered, drzZombieProbeBound, polls)
}

// TestDaemonReapCoversARefusedSpawn is the guard's second arm: the seam must not
// leak a process on the path where NO daemon is started either. A refused start
// is precisely when an operator looks at the process table, so a zombie left
// there would be the worst place for one.
//
// The refusal is forced where the seam produces it deterministically: the re-exec
// target is a Go TEST BINARY, which daemonChildExecutable REFUSES by name, fast,
// before the fork. The arm asserts BOTH the refusal and that it left nothing
// behind — neither direction is assumed.
func TestDaemonReapCoversARefusedSpawn(t *testing.T) {
	irdProfile(t)
	self, err := os.Executable()
	if err != nil {
		t.Skipf("resolve this process's executable: %v", err)
	}
	// The launcher convention, pointed at the TEST BINARY: a start through it
	// cannot work, and the seam's job is to say so and leave no mess.
	t.Setenv(runcmd.EnvBinary, self)
	// The reading is PPID-ATTRIBUTED, never a time window: see
	// drzZombieScanGopi for the measured reason (task 01a18dbe).
	before := drzZombieScanGopi(t, os.Getpid())

	reused, spawnErr := workerDaemonSpawnErr(nil)
	if spawnErr == nil {
		t.Fatalf("a start whose re-exec target is the Go test binary %s must be REFUSED, got reused=%v err=nil", self, reused)
	}
	var exitErr *exec.ExitError
	if errors.As(spawnErr, &exitErr) {
		t.Errorf("the refusal must be the seam's own verdict, not a child's exit status: %v", spawnErr)
	}
	t.Logf("[arm] refused as expected: %v", spawnErr)

	// This test binary is the parent of anything the call spawned, so a Z child
	// whose stat ppid IS this process is chargeable to it — and only such a
	// child is.
	for pid := range drzZombieScanGopi(t, os.Getpid()) {
		if !before[pid] {
			t.Errorf("the refused start left a zombie child: pid %d state=Z comm=%s ppid=%d (this test binary) — a refusal that leaks a process is not a clean refusal",
				pid, procname.NameGopi, os.Getpid())
		}
	}
}

// drzZombieScanGopi returns the set of zombie pids on the host whose comm is
// gopi AND whose stat ppid is owner. It is a PASSIVE read of /proc (Rule 27).
//
// WHY THE SCAN IS PPID-ATTRIBUTED, NOT A HOST-WIDE TIME DELTA (task 01a18dbe,
// measured 2026-11-01). This arm used to take the set of ALL `comm=gopi`
// zombies on the host before and after the refused call and charge every NEW
// pid to the seam. That is a difference of two HOST-WIDE readings attributed by
// TIME, and this host does not have a quiet process table: the operator's own
// resident gopi daemons run here, and ANY live process whose comm is gopi that
// forks produces fresh `comm=gopi` zombies inside the call's window. Measured:
// a perl daemon with `$0="gopi"` forking short-lived children (a live gopi
// parent, unrelated to this test binary) turned the arm RED with five pids from
// the generator's own fork stream (3815406..3815473), while the seam under test
// had not forked anything at all. The same shape arrives for free from this
// host's resident `gopi worker` daemons, which is why the failure showed up in
// two archived runs and never reproduced in isolation (0 FAIL of 100 isolated
// runs, `-count=100`).
//
// The ppid IS the attribution (Rule 24: parentage is read from /proc stat,
// never inferred from a process name or a timestamp). A child this test binary
// spawned has THIS process in its stat's ppid field for as long as it is a
// zombie — the kernel does not reparent an unreaped child — so the guard still
// catches exactly what it was written for: a leak on the seam's own path.
//
// WHAT IT CANNOT SEE, stated so nobody reads more into a green: a child the
// seam spawned and whose parent is NOT this process. That is not a weaker
// guard, it is the correct scope — this arm's premise (documented on the test)
// is that it drives the seam IN-PROCESS, so this binary IS the parent; a leak
// arriving from another shaper of the seam is that shaper's guard to write.
func drzZombieScanGopi(t *testing.T, owner int) map[int]bool {
	t.Helper()
	entries, err := os.ReadDir(procRootDir)
	if err != nil {
		t.Skipf("cannot read %s: %v", procRootDir, err)
	}
	out := make(map[int]bool)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if state, ok := drzPidState(pid); !ok || state != suiteProcZombieState {
			continue
		}
		ppid, ok := drzPidPPID(pid)
		if !ok || ppid != owner {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(procRootDir, e.Name(), drzCommFileName))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == procname.NameGopi {
			out[pid] = true
		}
	}
	return out
}
