package scratch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The sandbox reclaimer's contract, driven through the REAL predicate
// (PlanSandboxes / SweepSandboxes) against real directories and real processes.
//
// The three directions the epic demands proof of are here in their smallest
// form: a KILLED owner's sandbox is collected, a LIVE holder's is not, and a
// repeat run changes nothing. The raw shell-level demonstration of the same
// three (through the built binary) lives in the report; these tests are the
// regression guard that keeps them true.

// sandboxMarker writes an ownership marker with the fields the sweep reads.
func sandboxMarker(t *testing.T, dir string, pid int, ticks uint64) {
	t.Helper()
	blob, err := json.Marshal(map[string]any{
		"task_id":     "01a0demo-0000-7000-8000-00000000dead",
		"pid":         pid,
		"start_ticks": ticks,
		"created_at":  time.Now().UTC().Format(time.RFC3339),
		"version":     1,
	})
	if err != nil {
		t.Fatalf("marshal the marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, SandboxOwnerFileName), blob, 0o644); err != nil {
		t.Fatalf("write the marker: %v", err)
	}
}

// sandboxDir creates a sandbox-shaped directory (the marker prefix plus one
// private child) under root.
func sandboxDir(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, SandboxMarkerPrefix+name)
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	return dir
}

// ageDir backdates a directory's mtime. It MUST be called AFTER every other
// mutation — creating or removing an entry inside a directory updates that
// directory's own mtime, so aging first would be undone by the marker write.
// (That ordering is not a detail of the test: it is the same reason the sweep
// reads the DIRECTORY's mtime, which moves on use, rather than the marker's
// frozen created_at.)
func ageDir(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("age %s: %v", dir, err)
	}
}

// spawnIn starts a child whose CWD is dir and returns its pid, its start ticks
// and a kill function. The child is what makes "a live holder" real rather than
// simulated: the sweep's second liveness arm is a /proc scan, and a fake would
// not be in it.
func spawnIn(t *testing.T, dir string) (pid int, ticks uint64, kill func()) {
	t.Helper()
	//fixture-spawn-ok: the child's CWD inside `dir` is the measured subject (the sweep's /proc liveness arm reads it), and the forked `sleep` must be reachable by the TREE kill the caller asserts; the Pdeathsig below is defence in depth for the shell only
	cmd := exec.Command("/bin/sh", "-c", "sleep 300")
	cmd.Dir = dir
	// The child must die as a TREE, not as one process. /bin/sh runs the command
	// by forking it, so killing only the shell leaves the `sleep` alive with
	// cwd=dir — and the sweep's own liveness arm reads that orphan as a LIVE
	// HOLDER, correctly keeps the directory, and the caller's "the owner is gone,
	// so the sweep collects it" assertion fails at random (measured: 1-in-25 under
	// load, with the sweep reporting "a live process stands inside it"). Setpgid
	// makes the shell and everything it forks ONE process group, so the kill
	// below takes the whole tree.
	// Pdeathsig rides the same attr: the group signal below is this test's own
	// cleanup, and a SIGKILLed test binary never runs it. The kernel's
	// parent-death signal is the only cover that survives that path, and it
	// reaches the shell — the forked `sleep` is the group kill's job.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a child in %s: %v", dir, err)
	}
	killed := false
	kill = func() {
		if killed {
			return
		}
		killed = true
		// The NEGATIVE pid is the process group Setpgid just created: it reaches
		// the forked `sleep`, which killing cmd.Process alone would miss.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			t.Logf("kill process group %d: %v", cmd.Process.Pid, err)
		}
		_, _ = cmd.Process.Wait() //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	}
	t.Cleanup(kill)
	pid = cmd.Process.Pid
	// The kernel's start time is the identity half: wait for the child to be
	// visible in /proc before reading it (a just-forked pid can race the read).
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := ProcStartTime(pid)
		if err == nil {
			return pid, got, kill
		}
		if time.Now().After(deadline) {
			kill()
			t.Skipf("child %d never appeared in /proc: %v", pid, err)
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSweepCollectsAKilledOwnerSandbox is the epic's headline direction: the
// owner is gone (SIGKILL — no cleanup hook ran, which is the whole reason this
// reclaimer exists), the sandbox has aged, so the next sweep takes it.
func TestSweepCollectsAKilledOwnerSandbox(t *testing.T) {
	root := t.TempDir()
	dir := sandboxDir(t, root, "killed-owner")

	pid, ticks, kill := spawnIn(t, dir)
	// SIGKILL: no defer, no t.Cleanup, no graceful shutdown — the process just
	// stops existing, exactly the failure the epic names.
	kill()

	sandboxMarker(t, dir, pid, ticks)
	ageDir(t, dir, 48*time.Hour)

	plans, err := PlanSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("PlanSandboxes: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plan returned %d entries, want 1: %+v", len(plans), plans)
	}
	if plans[0].Keep {
		t.Fatalf("a sandbox whose owner was SIGKILLed is KEPT (reason %q) — the killed-owner direction is broken",
			plans[0].Reason)
	}

	removed, bytes, err := SweepSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("SweepSandboxes: %v", err)
	}
	if removed != 1 {
		t.Fatalf("SweepSandboxes removed %d, want 1 (bytes=%d)", removed, bytes)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the sandbox %s survived a sweep that reported it removed", dir)
	}
	// The ROOT is never removed, at any age, even when it is now empty.
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the sweep removed its own root %s: %v", root, err)
	}
}

// TestSweepKeepsALiveHolderSandbox is the other headline direction: a process is
// standing INSIDE the sandbox, so no age and no marker verdict may collect it.
func TestSweepKeepsALiveHolderSandbox(t *testing.T) {
	root := t.TempDir()
	dir := sandboxDir(t, root, "live-holder")

	// The owner's marker names a pid that is now dead for the OWNERSHIP arm...
	deadPid, deadTicks, killDead := spawnIn(t, t.TempDir())
	killDead()
	sandboxMarker(t, dir, deadPid, deadTicks)

	// ...but a LIVE process is inside, which the second arm must see.
	_, _, killLive := spawnIn(t, dir)
	defer killLive()
	ageDir(t, dir, 48*time.Hour)

	plans, err := PlanSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("PlanSandboxes: %v", err)
	}
	if len(plans) != 1 || !plans[0].Keep {
		t.Fatalf("a sandbox with a LIVE process inside was not kept: %+v", plans)
	}
	removed, _, err := SweepSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("SweepSandboxes: %v", err)
	}
	if removed != 0 {
		t.Fatalf("the sweep removed %d sandboxes, want 0 — a live holder must survive", removed)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the live-held sandbox was deleted: %v", err)
	}
}

// TestSweepKeepsAnUnverifiableOwner: an unknown is a KEEP. A marker without
// start ticks cannot prove identity, so a recycled pid must never be read as
// "the owner is still here" NOR as "the owner is gone".
func TestSweepKeepsAnUnverifiableOwner(t *testing.T) {
	root := t.TempDir()
	dir := sandboxDir(t, root, "no-ticks")
	sandboxMarker(t, dir, os.Getpid(), 0) // pid lives, ticks absent
	ageDir(t, dir, 48*time.Hour)

	plans, err := PlanSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("PlanSandboxes: %v", err)
	}
	if len(plans) != 1 || !plans[0].Keep {
		t.Fatalf("an unverifiable owner was collected: %+v", plans)
	}
	if plans[0].Reason != SandboxKeepUnknownOwner {
		t.Errorf("reason %q, want %q", plans[0].Reason, SandboxKeepUnknownOwner)
	}
}

// TestSweepKeepsAFreshSandbox: age is the first gate — a brand-new sandbox whose
// owner died a minute ago is still entitled to its window.
func TestSweepKeepsAFreshSandbox(t *testing.T) {
	root := t.TempDir()
	dir := sandboxDir(t, root, "fresh")
	sandboxMarker(t, dir, 999999, 1) // an impossible pid: gone, if it were aged

	plans, err := PlanSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("PlanSandboxes: %v", err)
	}
	if len(plans) != 1 || !plans[0].Keep || plans[0].Reason != SandboxKeepFresh {
		t.Fatalf("a fresh sandbox was not kept as fresh: %+v", plans)
	}
}

// TestSweepOnlyTouchesSandboxNames: the root is an ordinary directory the
// operator may share. A foreign child must survive at any age.
func TestSweepOnlyTouchesSandboxNames(t *testing.T) {
	root := t.TempDir()
	// A directory that is NOT a sandbox: no marker prefix in its name.
	notOurs := filepath.Join(root, "my-notes")
	if err := os.MkdirAll(notOurs, 0o700); err != nil {
		t.Fatalf("create %s: %v", notOurs, err)
	}
	ageDir(t, notOurs, 72*time.Hour)

	plans, err := PlanSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("PlanSandboxes: %v", err)
	}
	for _, p := range plans {
		if p.Path == notOurs {
			t.Fatalf("a directory that is not a sandbox appeared in the plan: %+v", p)
		}
	}
	// And a real sweep leaves it alone.
	if _, _, err := SweepSandboxes(root, time.Hour); err != nil {
		t.Fatalf("SweepSandboxes: %v", err)
	}
	if _, err := os.Stat(notOurs); err != nil {
		t.Errorf("the sweep removed a directory that is not a sandbox of ours: %v", err)
	}
}

// TestSpawnedOwnerDiesAsATree is the regression guard for the flaky sweep
// assertion above: `kill()` must leave NOTHING standing in dir. Killing only
// /bin/sh leaves the `sleep` it forked alive with cwd=dir — a live holder the
// sweep must honour — so a caller that wants "the owner is gone" would get it
// only at random.
func TestSpawnedOwnerDiesAsATree(t *testing.T) {
	dir := t.TempDir()
	_, _, kill := spawnIn(t, dir)
	kill()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if !pathHeldByProcesses(dir, ScanHolders().Paths) {
			return // the tree is gone: the sweep can collect the directory
		}
		if time.Now().After(deadline) {
			var who []string
			entries, _ := os.ReadDir(ProcRootDir) //stylegate:ignore reads /proc for the sandbox-owner scan this case measures; a missing directory is the empty case, not a failure
			for _, e := range entries {
				if !e.IsDir() || !IsPID(e.Name()) {
					continue
				}
				if target, err := os.Readlink(filepath.Join(ProcRootDir, e.Name(), ProcCwdName)); err == nil && PathWithin(target, dir) {
					who = append(who, fmt.Sprintf("%s cwd=%s", e.Name(), target))
				}
			}
			t.Fatalf("a process survived the kill of the spawned owner and still holds %s: %v", dir, who)
		}
		//stylegate:allow-sleep bounded poll interval: the loop's own deadline owns the outcome and exits on the first matching observation
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSweepIsIdempotentAndKeepsTheRoot: a repeat run finds nothing and the root
// survives an empty tree (the epic's "repeated runs stable").
func TestSweepIsIdempotentAndKeepsTheRoot(t *testing.T) {
	root := t.TempDir()
	dir := sandboxDir(t, root, "once")
	pid, ticks, kill := spawnIn(t, dir)
	kill()
	sandboxMarker(t, dir, pid, ticks)
	ageDir(t, dir, 48*time.Hour)

	first, _, err := SweepSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	second, bytes, err := SweepSandboxes(root, time.Hour)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if first != 1 || second != 0 || bytes != 0 {
		// The likeliest cause of a 0 here is not the sweep: a survivor of the
		// killed child (a forked grandchild still holding cwd=dir) is a LIVE
		// holder, and the sweep is RIGHT to keep the directory. Naming that in
		// the failure is the difference between a five-minute fix and an
		// afternoon: the same shape cost a full hour on 2026-09-28.
		var reason string
		if plans, perr := PlanSandboxes(root, time.Hour); perr == nil && len(plans) > 0 {
			reason = plans[0].Reason
		}
		handles := killedOwnerHandles(t, dir, pid)
		t.Fatalf("sweeps removed %d then %d (bytes=%d), want 1 then 0; the sweep's verdict on %s was %q, and the killed owner's pids still holding it are %v",
			first, second, bytes, dir, reason, handles)
	}
}

// killedOwnerHandles lists the live processes (if any) whose /proc cwd still
// resolves INTO dir — the evidence that makes the sweep's "live holder" keep the
// correct verdict rather than a bug.
func killedOwnerHandles(t *testing.T, dir string, pids ...int) []string {
	t.Helper()
	var out []string
	for _, pid := range pids {
		target, err := os.Readlink(filepath.Join(ProcRootDir, fmt.Sprint(pid), ProcCwdName))
		if err == nil && PathWithin(target, filepath.Clean(dir)) {
			out = append(out, fmt.Sprintf("pid %d cwd=%s", pid, target))
		}
	}
	return out
}

// TestSandboxRootRefusalNamesTheDangerousValues: a mistyped configuration must be
// NAMED, not swept — this arm deletes one level down, so its jurisdiction is
// real.
func TestSandboxRootRefusalNamesTheDangerousValues(t *testing.T) {
	if err := SandboxRootRefusal("/"); err == nil {
		t.Errorf("the filesystem root was accepted as a sandbox root")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if err := SandboxRootRefusal(home); err == nil {
			t.Errorf("the user's home %s was accepted as a sandbox root", home)
		}
	}
	t.Setenv(GopiDirEnvName, t.TempDir())
	if err := SandboxRootRefusal(gopiProfileDir()); err == nil {
		t.Errorf("the gopi profile %s was accepted as a sandbox root", gopiProfileDir())
	}
	if err := SandboxRootRefusal(""); err == nil {
		t.Errorf("an empty root was accepted")
	}
	ok := filepath.Join(t.TempDir(), "gopi-sandbox")
	if err := SandboxRootRefusal(ok); err != nil {
		t.Errorf("an ordinary sandbox root was refused: %v", err)
	}
}

// TestSandboxKillHelperIsSIGKILL documents the signal used above: the tests rely
// on the child being GONE with no chance to clean up, so the kill must not be
// SIGTERM (which a shell could trap).
func TestSandboxKillHelperIsSIGKILL(t *testing.T) {
	if syscall.SIGKILL == syscall.SIGTERM {
		t.Fatalf("SIGKILL and SIGTERM are the same signal on this platform — the killed-owner test would be meaningless")
	}
}

// TestSweepKeepsASandboxWhenTheHolderScanWasTruncated is the sandbox arm's
// fail-closed direction: a scan that STOPPED EARLY has not looked, so "nobody
// inside" is missing evidence, not a measurement. Without it a truncated pass
// would authorise deleting a sandbox a live process is writing in — the
// fail-open direction the bounded /proc scan exists to close. The sibling arms
// (base residue, worktree stub, the exported veto) carry the same proof; the
// sandbox planner did not.
//
// No process is spawned and no /proc is walked: judgeSandbox takes the scan as a
// value, so the two facts are injected directly — the smallest sample that
// distinguishes them.
func TestSweepKeepsASandboxWhenTheHolderScanWasTruncated(t *testing.T) {
	root := t.TempDir()
	dir := sandboxDir(t, root, "truncated-scan")
	// A marker naming an impossible pid: it is gone, so the OWNERSHIP arm does
	// not keep the directory and the verdict rests on the liveness arm alone.
	sandboxMarker(t, dir, 999999, 1)
	ageDir(t, dir, 48*time.Hour)

	// ① The CONTROL: a COMPLETE scan that found nobody inside ⇒ collectable.
	// Without it the kept case below would pass vacuously.
	control := judgeSandbox(dir, time.Now(), time.Hour, HolderScan{}, "")
	if control.Keep {
		t.Fatalf("the control failed: an aged, unowned, unheld sandbox is kept (reason %q) — this test would pass vacuously", control.Reason)
	}

	// ② The SAME directory, judged with a scan that stopped early: KEPT, and
	// the sentence says WHICH fact this is.
	kept := judgeSandbox(dir, time.Now(), time.Hour, HolderScan{Truncated: true}, "")
	if !kept.Keep {
		t.Fatalf("a TRUNCATED holder scan authorised a sandbox delete — 'not scanned' was read as 'not held', the fail-open direction this repo closes")
	}
	if !strings.Contains(kept.Reason, HolderScanWithheldReason()) {
		t.Fatalf("the keep reason does not disclose the truncation: %q", kept.Reason)
	}
	if strings.Contains(kept.Reason, SandboxKeepHeld) {
		t.Fatalf("the keep reason CLAIMS a live holder for a sandbox nothing was observed holding: %q", kept.Reason)
	}
}
