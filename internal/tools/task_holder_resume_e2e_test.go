package tools

// END-TO-END acceptance for the dead-holder RESUME arm (epic dead-holder-resume,
// 2026-09-16). Everything here is REAL: a real git repo, a real linked worktree,
// a real `gopi task run` child, a real scripted mock LLM, a real SIGKILL of the
// holder, and then the arm's own entry point resuming the interrupted run as a
// detached `gopi session resume` child.
//
// The three claims the epic must not settle for less than:
//
//	① the identity the arm judges with is RECORDED BY THE REAL RUN (not seeded):
//	   a real `task run` stamps the triple, and after the holder is killed the
//	   SAME verdict functions report it dead;
//	② the resume HAPPENS (a live new holder, stamped in the ledger) — not an
//	   annotation;
//	③ the resumed run CONTINUES the interrupted session (it recovers the
//	   original checkpoint history) instead of starting the work over.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/procname"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// dhE2EGit runs one git command in dir and returns its trimmed stdout.
func dhE2EGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// dhE2ERow reads the fields the arm judges, straight from the ledger.
func dhE2ERow(t *testing.T, dbPath, taskID string) (status, claimedBy, sessionID string, pid int, ticks int64) {
	t.Helper()
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row := db.QueryRow(`SELECT status, COALESCE(claimed_by,''), COALESCE(session_id,''),
		COALESCE(proc_pid,0), COALESCE(proc_start_ticks,0) FROM tasks WHERE task_id = ?`, taskID)
	if err := row.Scan(&status, &claimedBy, &sessionID, &pid, &ticks); err != nil {
		t.Fatalf("scan %s: %v", taskID, err)
	}
	return
}

// dhE2EWaitFor polls until cond holds or the deadline passes.
func dhE2EWaitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond) //stylegate:allow-sleep polling a real subprocess's ledger/JSONL state — the only observable is its side effect
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---- TEARDOWN JOIN (the flake both cleanups below exist to prevent) ---------
//
// THE DEFECT, MEASURED. `t.TempDir()`'s removal runs LAST among a test's
// cleanups and it is allowed to FAIL: it ends in
// `t.Fatalf("TempDir RemoveAll cleanup: %v …")`. So any process this fixture
// spawned that is still RUNNING — or still holding a descriptor inside a temp
// dir — when the test body returns turns a GREEN run red at teardown with
// "directory not empty", while every assertion passed. The fixture spawns a
// THREE-LEVEL tree (each level in its own group, which is what makes a single
// group signal insufficient):
//
//	wrapper `gopi worker`   Setsid  → its own session AND group (== its pid)
//	  worker `gopi`         Setsid  (cli_tasks_worker.go)
//	    agent `gopi task run` Setpgid (cli_tasks.go — the round's child)
//
// and the positive test's cleanup SIGKILLs a DETACHED `gopi session resume`
// child by pid only. **A signal is a REQUEST, not a join** — nothing in either
// cleanup ever asked "is it gone yet?", so TempDir's RemoveAll raced the tree.
//
// THE FIX IS A JOIN, NOT A STRONGER SIGNAL. dhE2EKillTree RECORDS the tree while
// it is still intact, kills it, and does NOT RETURN until every recorded member
// is PROVEN gone. THREE properties are load-bearing, and the guard below pins all
// three — each was measured, and the guard was RED on the first two shapes:
//
//	① the tree is walked by ANCESTRY, not by process group — cli_tasks.go gives
//	  the worker's agent child its OWN group (Setpgid), so a group-only kill
//	  leaves exactly the survivor this flake is made of;
//	② ANCESTRY ALONE IS NOT ENOUGH: a Setsid'd grandchild that its parent
//	  ABANDONED is re-parented to this host's child-subreaper (`systemd --user`,
//	  NOT pid 1 — procname.OrphanReparentTargets carries the measurement), so it
//	  is no longer any root's descendant and every walk returns "nothing left"
//	  while it still stands in the temp dir. The walk therefore ALSO takes the
//	  processes that carry this fixture's own SESSION/group id, dated from
//	  before the root was started (see dhE2EProbe) — the window is what keeps
//	  it from adopting a sibling test's tree that reused a recycled pid;
//	③ it runs while the root is still UNREAPED — a reaped root re-parents its
//	  orphaned children, so `run.Wait()` comes AFTER the join, never before.
const (
	// dhE2EKillRounds bounds the rescan-and-kill rounds: the tree can fork
	// between the scan and the signal (the worker spawns its round child), so a
	// second round is NORMAL; a survivor that keeps forking is REPORTED rather
	// than looped on forever.
	dhE2EKillRounds = 40
	// dhE2EKillStep is the pause between rounds (rounds × step = the window the
	// tree is given to die before the test says it did not).
	dhE2EKillStep = 25 * time.Millisecond
	// dhE2EGuardRounds bounds the guard's wait for its own fixture to fork.
	dhE2EGuardRounds = 200
	// dhE2EGuardStep is the pause between the guard's fixture polls.
	dhE2EGuardStep = 25 * time.Millisecond
)

// dhE2EProbe is the reference a tree is measured against: the root pid plus the
// TWO facts that outlive it. Both are the kernel's, and neither is a guess:
//
//	session  /proc/<root>/stat field 6 (procname.ProcSessionKey) — the SERIES
//	         the root leads. The kernel KEEPS a session id after its leader dies,
//	         which is exactly what ancestry loses when an orphan is re-parented.
//	cutoff   the root's own start time in clock ticks (procname.StartTicks) —
//	         the date line. A process the fixture could have spawned started at
//	         or after it, so a pid RECYCLED onto an unrelated process started
//	         EARLIER is excluded by measurement rather than by luck.
type dhE2EProbe struct {
	Root    int
	Session int
	Cutoff  string
}

// dhE2EProbeFor builds the reference for a root this test started.
func dhE2EProbeFor(root int) dhE2EProbe {
	p := dhE2EProbe{Root: root}
	if root <= 0 {
		return p
	}
	if sid, ok := procname.ProcSessionKey(procname.ProcRootDefault, strconv.Itoa(root)); ok {
		p.Session = sid
	}
	if ticks, ok := procname.StartTicks(procname.ProcRootDefault, strconv.Itoa(root)); ok {
		p.Cutoff = ticks
	}
	return p
}

// dhE2ESpawnedPIDs reports every LIVE process this fixture still owns — the
// ancestry walk of root PLUS the processes carrying its session — read from
// /proc through the repository's ONE stat parser (internal/procname: the comm
// field is parenthesized and may itself contain spaces and parentheses, so
// counting fields by hand is a trap with three historical copies already;
// Rule 14). Zombies are NOT reported: a zombie holds no descriptor and writes
// nothing, which is the only thing teardown cares about, and PidAlive already
// answers "gone" for one.
//
// The SECOND face (the session sweep) is not a refinement — it is the defect
// half the guard found: an abandoned Setsid'd grandchild carries ppid == the
// subreaper, so the ancestry walk alone reports "nothing left" while the process
// still stands in the temp dir (Rule 15: a green the scan did not measure).
//
// An unreadable /proc yields the root alone — never an empty answer, which
// would read as "nothing left" and silently reconnect this file to the race.
func dhE2ESpawnedPIDs(p dhE2EProbe) []int {
	if p.Root <= 0 {
		return nil
	}
	entries, err := os.ReadDir(procname.ProcRootDefault)
	if err != nil {
		return []int{p.Root}
	}
	children := map[int][]int{}
	type fact struct {
		pid     int
		session int
		ticks   string
	}
	var facts []fact
	for _, e := range entries {
		pid, perr := strconv.Atoi(e.Name())
		if perr != nil {
			continue
		}
		statPath := procname.StatStatPath(procname.ProcRootDefault, e.Name())
		raw, ok := procname.StatFieldString(statPath, procname.StatFieldParent)
		if !ok {
			continue
		}
		parent, aerr := strconv.Atoi(raw)
		if aerr != nil {
			continue
		}
		children[parent] = append(children[parent], pid)
		sid, _ := procname.ProcSessionKey(procname.ProcRootDefault, e.Name())
		ticks, _ := procname.StartTicks(procname.ProcRootDefault, e.Name())
		facts = append(facts, fact{pid: pid, session: sid, ticks: ticks})
	}
	owned := map[int]bool{}
	if PidAlive(p.Root) {
		owned[p.Root] = true
	}
	queue := []int{p.Root} // the walk is anchored at root even when root is a zombie
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if PidAlive(c) {
				owned[c] = true
			}
			queue = append(queue, c)
		}
	}
	// THE SESSION FACE. Only when the root's session id was readable AND a date
	// line exists: a session sweep without a cutoff would adopt any process that
	// happens to hold a recycled id, which is the same class of error as the
	// group signal this join replaces.
	if p.Session > 0 && p.Cutoff != "" {
		for _, f := range facts {
			if f.session != p.Session || !PidAlive(f.pid) {
				continue
			}
			if f.ticks != "" && dhE2ETicksBefore(p.Cutoff, f.ticks) {
				continue // started BEFORE this fixture existed: not ours
			}
			owned[f.pid] = true
		}
	}
	out := make([]int, 0, len(owned))
	for pid := range owned {
		out = append(out, pid)
	}
	sort.Ints(out) // deterministic: an error message must not shuffle
	return out
}

// dhE2ETicksBefore reports a < b for two /proc start-time readings. They are
// decimal clock ticks read from the same symbol the kernel uses, so the
// comparison is numeric — never lexicographic ("99" > "100" as text).
func dhE2ETicksBefore(a, b string) bool {
	ai, aerr := strconv.ParseInt(a, 10, 64)
	bi, berr := strconv.ParseInt(b, 10, 64)
	if aerr != nil || berr != nil {
		return false // an unparseable reading never excludes a candidate
	}
	return ai < bi
}

// dhE2EKillTree TERMINATES the process tree this fixture spawned and returns
// ONLY ONCE THAT TREE IS MEASURABLY GONE — the join `t.TempDir`'s removal needs.
//
// WHY A SNAPSHOT AND NOT A LIVE SCAN (this is the half the guard found, twice).
// A process is only reachable by ANCESTRY while its parent is alive, and only by
// SESSION while it still shares the root's — and this fixture's tree defeats both
// the moment it is signalled: `cli_tasks_worker.go` Starts the worker child with
// Setsid (a NEW session), and the shape is recursive (the guard's fixture setsid'd
// a grandchild that was then orphaned when its parent died). So a join that
// re-scans can report "nothing left" while a survivor still stands in the temp
// dir — measured here as the guard's first two reds. Therefore:
//
//	① the membership is RECORDED while the tree is still intact (descendants of
//	   the root, plus everything carrying the root's own session), and every
//	   member is written down as (pid, start ticks);
//	② the await is an IDENTITY check, not a liveness check: a member is still
//	   "ours and alive" iff /proc/<pid>/stat's start time still equals the
//	   recorded one — a pid the kernel RECYCLED onto an unrelated process has a
//	   later date, so it can neither hold this join open nor be signalled;
//	③ each round re-scans first, so a member that forked between rounds is added
//	   before its parent dies.
//
// THE SIGNAL ADDRESSES ARE ALL OWNED (Rule 42). A member's own process group is
// signalled ONLY when /proc says its pgid EQUALS its pid (it is its own group
// leader, and the process is provably one we recorded) — otherwise only the
// process itself is signalled, because `-pid` on a non-leader names somebody
// else's group. Nothing here is addressed by pattern (no pkill/pgrep), and the
// root's group signal is the one this test created with Setsid on its own
// *exec.Cmd.
func dhE2EKillTree(t *testing.T, root int) {
	t.Helper()
	if root <= 0 {
		return
	}
	p := dhE2EProbeFor(root)
	seen := map[int]string{} // pid → start ticks, the identity we must see die
	for round := 0; round < dhE2EKillRounds; round++ {
		// ③ re-scan FIRST: a member that forked since the last round is recorded
		// before we kill its parent and lose the ancestry.
		for _, pid := range dhE2ESpawnedPIDs(p) {
			if _, ok := seen[pid]; !ok {
				seen[pid] = dhE2EStartTicks(pid)
			}
		}
		pids := make([]int, 0, len(seen))
		for pid := range seen {
			pids = append(pids, pid)
		}
		sort.Ints(pids)
		// Descendants FIRST, the root LAST: an orphan outlives the walk but not
		// the snapshot, and killing the root first would only widen the window.
		for _, pid := range pids {
			if pid == root {
				continue
			}
			if !dhE2EMemberAlive(pid, seen[pid]) {
				continue
			}
			if dhE2EIsOwnGroupLeader(pid) {
				_ = syscall.Kill(-pid, syscall.SIGKILL) //stylegate:ignore signal to a group whose leader this scan proved to be a recorded member of this test's own tree; ESRCH is the expected answer once it is dead
			}
			_ = syscall.Kill(pid, syscall.SIGKILL) //stylegate:ignore signalling a process this scan recorded as a member of the tree THIS test spawned; ESRCH is the expected answer once it is dead
		}
		_ = syscall.Kill(-root, syscall.SIGKILL) //stylegate:ignore signal to a process group THIS test created (Setsid on its own *exec.Cmd); no process of another owner can be a member
		_ = syscall.Kill(root, syscall.SIGKILL)  //stylegate:ignore signal to the child THIS test started; the join below, not this call, is the deliverable
		if alive := dhE2EAliveMembers(seen); len(alive) == 0 {
			return // ← THE JOIN: every recorded member is provably gone
		}
		time.Sleep(dhE2EKillStep) //stylegate:allow-sleep polling a killed process tree's /proc state — the only observable is the kernel's own answer
	}
	// A survivor is REPORTED, never swallowed: it is the residue that makes
	// this file's temp dirs unremovable (Rule 15 — no silent failure).
	var survivors []string
	for _, pid := range dhE2EAliveMembers(seen) {
		raw, rerr := os.ReadFile(filepath.Join(procname.ProcRootDefault, strconv.Itoa(pid), "cmdline"))
		if rerr != nil {
			raw = nil
		}
		survivors = append(survivors, fmt.Sprintf("%d %q", pid, strings.ReplaceAll(string(raw), "\x00", " ")))
	}
	t.Errorf("teardown join: the spawned tree survived %d kill rounds (root %d): %s",
		dhE2EKillRounds, root, strings.Join(survivors, "; "))
}

// dhE2EAliveMembers returns the recorded members that are STILL THE SAME PROCESS
// and still running — the await's only predicate (② above). A member whose pid
// now carries a different start time is a RECYCLED number: it is not ours, so it
// neither holds the join open nor gets a signal.
func dhE2EAliveMembers(seen map[int]string) []int {
	var out []int
	for pid, ticks := range seen {
		if dhE2EMemberAlive(pid, ticks) {
			out = append(out, pid)
		}
	}
	sort.Ints(out)
	return out
}

// dhE2EMemberAlive reports whether pid is still alive AND still the process that
// was recorded (same start ticks). An UNREADABLE date is treated as alive: this
// is the await side, and "I could not prove it is gone" must keep waiting, never
// release (Rule 15).
func dhE2EMemberAlive(pid int, ticks string) bool {
	if !PidAlive(pid) {
		return false
	}
	if ticks == "" {
		return true
	}
	now := dhE2EStartTicks(pid)
	return now == "" || now == ticks
}

// dhE2EStartTicks reads a process's start time (clock ticks since boot), the
// kernel's stable identity for a pid.
func dhE2EStartTicks(pid int) string {
	ticks, ok := procname.StartTicks(procname.ProcRootDefault, strconv.Itoa(pid))
	if !ok {
		return ""
	}
	return ticks
}

// dhE2EIsOwnGroupLeader reports whether pid leads its OWN process group (pgid ==
// pid). That is the precondition for addressing the group as `-pid`: on a
// non-leader the negative number names someone else's group (Rule 42).
func dhE2EIsOwnGroupLeader(pid int) bool {
	raw, ok := procname.StatFieldString(
		procname.StatStatPath(procname.ProcRootDefault, strconv.Itoa(pid)), procname.StatFieldPgrp)
	if !ok {
		return false
	}
	pgid, err := strconv.Atoi(raw)
	return err == nil && pgid == pid
}

// TestKillSpawnedTreeJoinsTheWholeTreeBeforeTempDirCleanup is the REGRESSION
// GUARD for the join above — it needs no load and no flake rate (Rule 27), only
// the real shape: a child that forks a grandchild INTO ITS OWN PROCESS GROUP,
// exactly like cli_tasks.go's agent child.
//
// WHAT IT ACTUALLY PINS, measured rather than assumed (Rule 41a). It fails when
// the cleanup's SHAPE decays to the PRE-FIX one — a group-only signal with no
// recorded membership and no await (red 3/3, the survivor named in the output).
// It does NOT catch the narrower mutation of deleting the await line alone: a
// SIGKILL is delivered and /proc answers "gone" inside the same microsecond
// window on this fixture, so both shapes read identically. That is why the
// sub-fixture below asserts the DEFECT SHAPE still exists instead of trusting
// one line to be load-bearing — a guard whose red depends on winning a race
// would be flaky in the direction that matters (a red that never fires).
func TestKillSpawnedTreeJoinsTheWholeTreeBeforeTempDirCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the join reads /proc")
	}
	// The fixture: `sh` whose grandchild is `setsid sh -c 'sleep …'`, i.e. a
	// descendant that LEFT the fixture's group — the shape a group signal
	// cannot reach (measured: `set -m`-style job control does the same thing).
	// The grandchild writes nothing; what matters is that it is ALIVE.
	const marker = "gopi-e2ekilltree-guard"
	const grandchild = marker + "-grandchild"
	//fixture-spawn-ok: this fixture's SUBJECT is the descendant that has deliberately LEFT every group (`exec setsid`), and PR_SET_PDEATHSIG is not inherited across fork(2) — a process can only arm it on itself — so no handle this test holds can reach that grandchild when the binary is SIGKILLed; the group join IS the mechanism under test, and the leak is bounded by the 120 s hold
	root := exec.Command("sh", "-c", "sh -c 'exec setsid sh -c \"sleep 120\" "+grandchild+"' ; sleep 120")
	// Pdeathsig is armed on the ROOT so a SIGKILLed test binary does not leave the
	// root's own `sleep 120` siblings standing (Rule 42: this arm reaches only the
	// child it created).
	//
	// The fixture-spawn gate is still owed a stated reason, and it is a real one:
	// this fixture's SUBJECT is a descendant that has deliberately LEFT every group
	// (`exec setsid sh -c 'sleep 120'`), and PR_SET_PDEATHSIG is NOT inherited
	// across fork(2) — a process can only arm it on ITSELF. So no mechanism this
	// test holds a handle to can reach that grandchild when the test binary is
	// SIGKILLed; the group join (`killSpawnedTree`) is what may, when it gets to
	// run. Typing that limitation down is the honest form, and the leak is bounded
	// by the 120 s hold.
	root.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}
	if err := root.Start(); err != nil {
		t.Fatalf("start the fixture tree: %v", err)
	}
	rootPid := root.Process.Pid
	probe := dhE2EProbeFor(rootPid)
	// Wait for the grandchild to exist AND to have left root's group: until
	// then the fixture proves nothing about the group-only kill.
	var gc int
	deadline := time.Now().Add(time.Duration(dhE2EGuardRounds) * dhE2EGuardStep)
	for time.Now().Before(deadline) {
		for _, pid := range dhE2ESpawnedPIDs(probe) {
			if pid == rootPid {
				continue
			}
			if !dhE2EPidHasArgv(pid, grandchild) {
				continue
			}
			pgid, ok := procname.StatFieldString(
				procname.StatStatPath(procname.ProcRootDefault, strconv.Itoa(pid)), procname.StatFieldPgrp)
			if ok && pgid != strconv.Itoa(rootPid) {
				gc = pid
			}
		}
		if gc != 0 {
			break
		}
		time.Sleep(dhE2EGuardStep) //stylegate:allow-sleep polling a freshly spawned fixture's /proc state — the only observable is the kernel's own answer
	}
	if gc == 0 {
		_ = root.Wait() //stylegate:ignore reaping the guard's own fixture on its failure path; the failure is the assertion, not this status
		t.Fatalf("the fixture's grandchild never appeared in its own process group — this guard cannot prove anything without it")
	}
	// ① THE ORDER THE PRODUCTION CLEANUP USES: the helper is called FIRST, while
	// the tree is intact — it records the membership, kills it, and may only
	// return once every member is gone. The grandchild is in its OWN group and
	// its parent (root's child) dies on the way, so a group-only kill and a
	// live re-scan BOTH lose it: this is the join's subject.
	dhE2EKillTree(t, rootPid)
	_ = root.Wait() //stylegate:ignore reaping the guard's fixture child; the grandchild's survival is the subject
	// ② THE AWAIT, measured three ways. (a) the grandchild itself must be
	// provably gone; (b) a fresh scan of the recorded session face must find
	// nothing; (c) its pid must not have been recycled onto someone else whose
	// process would then be indistinguishable from our survivor.
	if PidAlive(gc) {
		t.Errorf("the join returned before the group-escaping grandchild (pid %d) was gone", gc)
	}
	if left := dhE2ESpawnedPIDs(probe); len(left) != 0 {
		t.Errorf("the join returned while %v was still alive (membership is the recorded snapshot, not a live walk)", left)
	}
	if dhE2EPidHasArgv(gc, grandchild) {
		t.Errorf("the grandchild's pid still carries its argv — it was never killed (pid %d)", gc)
	}
	// ③ REGRESSION DIRECTION: the same fixture, joined by the PRE-FIX cleanup
	// (one group signal, no await), must LEAVE THE GRANDCHILD BEHIND. That is the
	// defect this helper replaces, and asserting it here is what keeps the
	// helper's extra faces from being "simplified" away later.
	t.Run("group-only kill leaves the orphan", func(t *testing.T) {
		//fixture-spawn-ok: the regression arm's whole point is that a setsid grandchild is UNREACHABLE by the group signal (and Pdeathsig is not inherited across fork), so the pinned defect is exactly that this child can outlive the root; bounded by the 120 s hold
		root2 := exec.Command("sh", "-c", "sh -c 'exec setsid sh -c \"sleep 120\" "+grandchild+"-2' ; sleep 120")
		// Setsid as above, PLUS Pdeathsig on the root — and the same stated
		// limitation: the setsid grandchild cannot be reached by any handle this
		// test holds (Pdeathsig is not inherited across fork), which is precisely
		// the defect this arm pins.
		root2.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}
		if err := root2.Start(); err != nil {
			t.Fatalf("start the second fixture tree: %v", err)
		}
		root2Pid := root2.Process.Pid
		probe2 := dhE2EProbeFor(root2Pid)
		var gc2 int
		deadline2 := time.Now().Add(time.Duration(dhE2EGuardRounds) * dhE2EGuardStep)
		for time.Now().Before(deadline2) {
			for _, pid := range dhE2ESpawnedPIDs(probe2) {
				if pid != root2Pid && dhE2EPidHasArgv(pid, grandchild+"-2") && dhE2EIsOwnGroupLeader(pid) {
					gc2 = pid
				}
			}
			if gc2 != 0 {
				break
			}
			time.Sleep(dhE2EGuardStep) //stylegate:allow-sleep polling a freshly spawned fixture's /proc state — the only observable is the kernel's own answer
		}
		if gc2 == 0 {
			dhE2EKillTree(t, root2Pid)
			_ = root2.Wait() //stylegate:ignore reaping the second fixture on its failure path; the failure is the assertion
			t.Fatalf("the second fixture's grandchild never appeared in its own group")
		}
		// THE PRE-FIX CLEANUP, exactly: one group signal + a reap, no await.
		if err := syscall.Kill(-root2Pid, syscall.SIGKILL); err != nil {
			t.Fatalf("kill the second fixture's group: %v", err)
		}
		_ = root2.Wait() //stylegate:ignore reaping the second fixture child; the grandchild's survival is the subject
		survived := PidAlive(gc2)
		// Always join, whatever the measurement said — a leaked fixture child
		// would outlive the test and (Rule 27's spirit) turn this guard itself
		// into the kind of residue it exists to prevent.
		dhE2EKillTree(t, root2Pid)
		if !survived {
			t.Fatalf("a group-only kill already removed the group-escaping grandchild (pid %d) — the fixture no longer models the defect", gc2)
		}
	})
}

// dhE2EPidHasArgv reports whether ANY argument of pid contains substr — the
// identity check that is immune to pid recycling (a recycled number belongs to
// some other program, whose argv will not carry this fixture's marker).
func dhE2EPidHasArgv(pid int, substr string) bool {
	raw, err := os.ReadFile(filepath.Join(procname.ProcRootDefault, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(raw), "\x00", " "), substr)
}

// TestMergerResumesDeadHolderRunE2E is the epic's end-to-end acceptance.
func TestMergerResumesDeadHolderRunE2E(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	if testing.Short() {
		t.Skip("e2e spawns real processes")
	}
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mainRepo := t.TempDir()
	dhE2EGit(t, mainRepo, "init", "-q")
	dhE2EGit(t, mainRepo, "commit", "-q", "--allow-empty", "-m", "init")

	// The interrupted run's worktree: a real linked worktree on its own branch,
	// exactly the shape whose protection the merger reports.
	wtParent := t.TempDir()
	wt := filepath.Join(wtParent, "wt-dead-holder")
	branch := "feat/dead-holder-e2e"
	dhE2EGit(t, mainRepo, "worktree", "add", wt, "-b", branch)

	// Scripted mock LLM: the FIRST request is HELD open, so the run stays alive
	// at a deterministic point until we kill it. Later requests repeat Hold, so
	// the resumed run is alive too — which is what the assertions need.
	// The script is load-bearing: round 1 must COMPLETE (that is what writes the
	// → REQUEST checkpoint a recovery resumes FROM), and round 2 must HOLD, so
	// the run is still alive — and killable mid-flight — with a checkpoint
	// already on disk. A run killed during its very first request has no
	// checkpoint, and `gopi session resume` then refuses it BY DESIGN (verified: the
	// child reports "no → REQUEST checkpoint found" and exits 1).
	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: "noted, starting on the worktree"},
		testutil.Resp{Hold: true})
	env := testutil.MockEnv(t, gopiHome, llm.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	// The tools package reads the ledger from the env; the detached resume child
	// INHERITS the test process's environment, so both must see the fixture.
	// GOPI_BINARY is what makes the spawned recovery the REAL gopi binary (the
	// documented launcher convention — runcmd.Launcher: GOPI_BINARY, then
	// GOPI_SELF, then this executable, which under `go test` is the test binary).
	t.Setenv("GOPI_BINARY", testutil.GopiBin(t))
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_HOME", gopiHome)
	t.Setenv("ANTHROPIC_BASE_URL", llm.URL())
	t.Setenv("DEEPSEEK_BASE_URL", llm.URL())
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("GOPI_ALLOW_AGENT_SPAWN", "1")

	const taskID = "01a11500-0000-7000-8000-000000000007"
	if _, stderr, code := testutil.RunGopi(t, []string{"task", "submit",
		"dead holder e2e", "--payload", "work in the worktree", "--task-id", taskID},
		"", env, mainRepo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// Deliverable: the row must name the worktree, which is how the arm matches
	// a branch to the run that owns it (the worktree->task mapping).
	dl, _ := json.Marshal(map[string]string{
		DeliverableWorktreeKey: wt, deliverableBranchKey: branch, deliverableRepoKey: mainRepo,
	}) //stylegate:ignore marshalling a map built in-process cannot fail (no channels or funcs reach these sites)
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, string(dl), taskID); err != nil {
		t.Fatalf("deliverable: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime fixture handle; the OS reclaims the descriptor

	// ---- ① REAL `gopi task run`: does the run itself record the identity?
	runEnv := append(append([]string{}, env...), "GOPI_ALLOW_AGENT_SPAWN=1")
	run := exec.Command(testutil.GopiBin(t), "worker", taskID)
	run.Dir = wt
	run.Env = runEnv
	run.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // its own group: one kill takes the run and its child
	if err := run.Start(); err != nil {
		t.Fatalf("start the run: %v", err)
	}
	wrapperPid := run.Process.Pid
	dhE2EWaitFor(t, "the run to reach in_progress with a recorded identity", 60*time.Second, func() bool {
		status, _, sid, pid, ticks := dhE2ERow(t, dbPath, taskID)
		return status == taskStatusInProgress && sid != "" && pid > 0 && ticks > 0
	})
	status, claimedBy, sessionID, recPid, recTicks := dhE2ERow(t, dbPath, taskID)
	t.Logf("ledger after the real run: status=%s holder=%s session=%s pid=%d ticks=%d",
		status, claimedBy, sessionID, recPid, recTicks)
	if recPid != wrapperPid {
		t.Errorf("the recorded holder pid = %d, want the runner's own pid %d", recPid, wrapperPid)
	}
	// While it lives, the arm's own verdict must call it LIVE (and therefore
	// leave it completely alone).
	if got := (holderProbe{ident: procIdentity{Pid: recPid, StartTicks: recTicks}}).resolve(); got != holderProbeLive {
		t.Fatalf("a running holder must be judged live, got %v", got)
	}

	// WAIT for the checkpoint BEFORE killing: that is what makes this "a run
	// interrupted with recoverable work in flight" rather than "a run that
	// never got started" (the two are very different, and only the first is
	// resumable — see the mock script comment above).
	rawLog := taskRawLogPathFromSession(sessionID)
	dhE2EWaitFor(t, "the interrupted run's checkpoint to land", 120*time.Second, func() bool {
		body, rerr := os.ReadFile(rawLog)
		return rerr == nil && strings.Contains(string(body), checkpointRequestMarker)
	})

	// ---- kill it, exactly as a crashed child would be — and JOIN the kill
	// (dhE2EKillTree): a group signal alone is a REQUEST, and the tree has two
	// levels in groups of their own, so the joining helper is what makes the
	// holder PROVABLY dead on the next line instead of eventually dead (and it
	// is what stops a survivor outliving the test into t.TempDir's removal).
	dhE2EKillTree(t, wrapperPid)
	_ = run.Wait() //stylegate:ignore reap of the wrapper process being abandoned; the resume verdict is the assertion, not the exit status
	_ = run.Wait() //stylegate:ignore reaping the run this test started after joining it; the assertion reads /proc, not this status
	dhE2EWaitFor(t, "the killed holder to vanish from /proc", 20*time.Second, func() bool {
		_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(wrapperPid)))
		return err != nil
	})
	// The SAME verdict functions, same recorded identity, now report dead.
	deadVerdict := holderProbe{ident: procIdentity{Pid: recPid, StartTicks: recTicks}}.resolve()
	if deadVerdict != holderProbeDead {
		t.Fatalf("after the kill the recorded holder must be dead, got %v", deadVerdict)
	}

	// ---- ② the resume HAPPENS through the arm's own entry point
	// ONE call — the arm is an ACT, so the test calls it once and inspects the
	// disposition it returns (polling it would spawn a resume per iteration).
	var resumed *deadHolderResumeOutcome
	outcomes := ResumeDeadHolderRuns(mainRepo)
	for i := range outcomes {
		t.Logf("arm outcome: %s", outcomes[i].Line())
		if outcomes[i].TaskID == taskID {
			oc := outcomes[i]
			resumed = &oc
		}
	}
	if resumed == nil {
		t.Fatalf("the dead holder's row must produce an arm outcome (candidates were found and the verdict is dead)")
	}
	if !resumed.Resumed {
		body, _ := os.ReadFile(resumed.LogPath) //stylegate:ignore diagnostic read-back of the resumed run's log after the assertion already failed; its error cannot be reported
		t.Fatalf("the arm did not resume: %s\n--- resume child log ---\n%s", resumed.Line(), body)
	}
	if resumed.Verdict != holderProbeDead.String() {
		t.Errorf("the resumed outcome must name the dead verdict, got %q", resumed.Verdict)
	}
	if resumed.Attempts != 1 {
		t.Errorf("the first resume must record attempt 1, got %d", resumed.Attempts)
	}

	// A NEW holder owns the row: not the killed pid, and behaving as a real
	// process (the ledger's own verdict must call it live).
	var newPid int
	dhE2EWaitFor(t, "the resumed run to be stamped as the new holder", 60*time.Second, func() bool {
		_, _, _, pid, ticks := dhE2ERow(t, dbPath, taskID)
		if pid <= 0 || pid == wrapperPid || ticks <= 0 {
			return false
		}
		newPid = pid
		return finishCleanupProcessHolderLive(procIdentity{Pid: pid, StartTicks: ticks})
	})
	t.Logf("the resumed run holds the row as pid %d", newPid)
	t.Cleanup(func() {
		// The detached recovery child is the SECOND spawned root this test must
		// JOIN, not merely signal: it is SpawnTaskResume's Setsid'd child and it
		// runs real agent work with its own round child, so a pid-group signal
		// leaves its descendants behind — and a survivor is exactly what makes
		// t.TempDir's removal race. REGISTRATION ORDER IS LOAD-BEARING: cleanup
		// runs LIFO, so this entry runs BEFORE the writer's, while the wrapper is
		// still unreaped — which is what keeps the resumed child's whole descent
		// reachable from ONE root walk.
		dhE2EKillTree(t, newPid)
	})

	// ---- ③ it CONTINUED the interrupted session (not a fresh start)
	//
	// The recovery is a resume OF THE ROW'S SESSION: the raw-log checkpoint that
	// `task run` was writing when it was killed is loaded and its history is
	// seeded into the new run. The evidence is the recovered history count the
	// resume itself prints, plus the lineage tag it stamps on the row.
	// The resumed run is a REAL gopi session: it registers its own session dir
	// and runs the briefing's first checkpoint (the task-start memory consult).
	// Waiting on that output is waiting on genuine agent work, not on a banner.
	dhE2EWaitFor(t, "the resumed run to announce the recovered context", 90*time.Second, func() bool {
		body, rerr := os.ReadFile(resumed.LogPath)
		return rerr == nil && strings.Contains(string(body), "loaded")
	})
	resumeLogBody, err := os.ReadFile(resumed.LogPath)
	if err != nil {
		t.Fatalf("read the resume log %s: %v", resumed.LogPath, err)
	}
	logText := string(resumeLogBody)
	// The recovered context is the PROOF OF CONTINUATION: the resumed run
	// announces how many history messages it carried over from the interrupted
	// session's checkpoint (not a fresh briefing-only start).
	if i := strings.Index(logText, "resume: loaded"); i >= 0 {
		end := strings.IndexByte(logText[i:], '\n')
		if end < 0 {
			end = len(logText) - i
		}
		t.Logf("continuation evidence: %s", strings.TrimSpace(logText[i:i+end]))
	}
	if !strings.Contains(logText, "resumed") && !strings.Contains(logText, "loaded") {
		t.Errorf("the resume log must show the recovered context:\n%s", logText)
	}
	if !strings.Contains(logText, "--task "+taskID) && !strings.Contains(logText, taskID) {
		t.Errorf("the resume log must be linked to the task:\n%s", logText)
	}
	// The row now carries the resume lineage: the NEW session id, stamped by the
	// resume itself — the ledger's record that this work continued somewhere.
	show, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(show, taskTagResumedPrefix) {
		t.Errorf("the resumed row must carry the %q lineage tag:\n%s", taskTagResumedPrefix, show)
	}
	if !strings.Contains(show, deadHolderResumeTagPrefix) {
		t.Errorf("the resumed row must carry the arm's attempt counter:\n%s", show)
	}

	// ---- the worktree is UNTOUCHED (the recovery depends on it)
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Errorf("the recovered worktree must survive the resume: %v", err)
	}
	// ---- and the arm is now SILENT about it: the holder is alive again, so a
	// second pass must neither resume again nor count another attempt.
	for _, o := range ResumeDeadHolderRuns(mainRepo) {
		if o.TaskID == taskID {
			t.Errorf("a live resumed holder must produce NO outcome (got %s)", o.Line())
		}
	}
	_, _, _, _, attemptsTail := dhE2ERow(t, dbPath, taskID)
	_ = attemptsTail
	rows, err := deadHolderResumeRows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	for _, r := range rows {
		if r.TaskID == taskID && r.Attempts != 1 {
			t.Errorf("the attempt counter must stay at 1 after a live holder is seen, got %d", r.Attempts)
		}
	}
}

// TestResumeArmNeverTouchesALiveHolderWorktreeE2E is the NEGATIVE acceptance:
// with the interrupted run still ALIVE, the arm must not resume, must not
// stamp, and must leave the worktree alone. This is the direction that, if
// broken, double-runs a live run (the P0 of the sibling singleton-guard epic).
func TestResumeArmNeverTouchesALiveHolderWorktreeE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e spawns real processes")
	}
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mainRepo := t.TempDir()
	dhE2EGit(t, mainRepo, "init", "-q")
	dhE2EGit(t, mainRepo, "commit", "-q", "--allow-empty", "-m", "init")
	wt := filepath.Join(t.TempDir(), "wt-live-holder")
	branch := "feat/live-holder-e2e"
	dhE2EGit(t, mainRepo, "worktree", "add", wt, "-b", branch)

	llm := testutil.NewMockLLM(t, testutil.Resp{Hold: true})
	env := testutil.MockEnv(t, gopiHome, llm.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath, config.SkipClaimBroadcastEnv+"=1")

	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv("GOPI_HOME", gopiHome)

	const taskID = "01a11500-0000-7000-8000-000000000008"
	if _, stderr, code := testutil.RunGopi(t, []string{"task", "submit",
		"live holder e2e", "--payload", "work in the worktree", "--task-id", taskID},
		"", env, mainRepo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	dl, _ := json.Marshal(map[string]string{
		DeliverableWorktreeKey: wt, deliverableBranchKey: branch, deliverableRepoKey: mainRepo,
	}) //stylegate:ignore marshalling a map built in-process cannot fail (no channels or funcs reach these sites)
	db, err := sql.Open("sqlite", LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, string(dl), taskID); err != nil {
		t.Fatalf("deliverable: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime fixture handle; the OS reclaims the descriptor

	runEnv := append(append([]string{}, env...), "GOPI_ALLOW_AGENT_SPAWN=1")
	run := exec.Command(testutil.GopiBin(t), "worker", taskID)
	run.Dir = wt
	run.Env = runEnv
	run.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := run.Start(); err != nil {
		t.Fatalf("start the run: %v", err)
	}
	rootPid := run.Process.Pid
	t.Cleanup(func() {
		// JOIN, then reap, then reap AGAIN — in that order, because the join
		// must run while the tree is still REACHABLE (a reaped root hands its
		// orphans to init) and the second Wait is what reaps the processes the
		// first join discovered through /proc (an un-reaped zombie would make
		// Wait's bookkeeping permanently dirty).
		dhE2EKillTree(t, rootPid)
		_ = run.Wait() //stylegate:ignore reap of the wrapper process being abandoned; the resume verdict is the assertion, not the exit status
		dhE2EKillTree(t, rootPid)
		_ = run.Wait() //stylegate:ignore reaping the run this test started after joining it; the assertion reads /proc, not this status
	})
	dhE2EWaitFor(t, "the run to reach in_progress with a recorded identity", 60*time.Second, func() bool {
		status, _, sid, pid, ticks := dhE2ERow(t, dbPath, taskID)
		return status == taskStatusInProgress && sid != "" && pid > 0 && ticks > 0
	})
	_, beforeHolder, _, beforePid, beforeTicks := dhE2ERow(t, dbPath, taskID)

	if got := ResumeDeadHolderRuns(mainRepo); len(got) != 0 {
		for _, o := range got {
			t.Errorf("a LIVE holder must produce no arm outcome, got: %s", o.Line())
		}
	}
	afterStatus, afterHolder, _, afterPid, afterTicks := dhE2ERow(t, dbPath, taskID)
	if afterStatus != taskStatusInProgress || afterPid != beforePid || afterTicks != beforeTicks {
		t.Errorf("the arm must not touch a live holder's row: status %s→%s pid %d→%d ticks %d→%d",
			taskStatusInProgress, afterStatus, beforePid, afterPid, beforeTicks, afterTicks)
	}
	// The holder stamp must be UNCHANGED. It used to be compared to the row's
	// recorded PROCESS pid, and that comparison was only ever true because the
	// pre-spawn status write had no child to point at, so both happened to name
	// the wrapper. Since epic 01a1a87b the row's `process:` identity IS the
	// child (that is the point of the change) while claimed_by names the runner
	// that owns the row — two different, both-true facts. So the assertion
	// compares the stamp to ITSELF before/after, which is what the test is
	// actually about: the resume arm must not rewrite the holder.
	if afterHolder == "" || afterHolder != beforeHolder {
		t.Errorf("the holder stamp must be unchanged, got %q (before %q)", afterHolder, beforeHolder)
	}
	// No attempt was spent and no resume was spawned.
	rows, err := deadHolderResumeRows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	for _, r := range rows {
		if r.TaskID == taskID && r.Attempts != 0 {
			t.Errorf("a live holder must not consume resume attempts, got %d", r.Attempts)
		}
	}
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Errorf("the live holder's worktree must be untouched: %v", err)
	}
}

// The two deliverable keys this fixture writes are NAMED once (Rule 1b): the
// product owner spells the branch/repo keys inside taskDeliverableBranch /
// taskDeliverableWorktree's decode structs, and a fixture that typed the
// literals again could drift from them silently.
const (
	// Aliases of the package's SINGLE source for these keys (Rule 14): the
	// fixture vocabulary predates the exported constants and is kept so the
	// literals appear in exactly one place — the constants file.
	deliverableBranchKey = DeliverableBranchKey
	deliverableRepoKey   = DeliverableRepoKey
)

// checkpointRequestMarker is the raw-log line a resumable checkpoint begins
// with. It is internal/agent's marker (rawMarkerRequest), spelled here rather
// than exported: the test asserts the ON-DISK shape a recovery reads, and that
// shape is the wire contract between the two packages.
const checkpointRequestMarker = "→ REQUEST"
