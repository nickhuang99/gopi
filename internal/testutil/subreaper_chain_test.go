package testutil

// subreaper_chain_test.go — the ancestry-chain arm of the eligibility
// predicate, pinned with a REAL marked subreaper ABOVE the sweeping process, and
// a third-party marked subreaper that is NOT above it.
//
// THE DEFECT THIS PINS (measured 2026-10-11, bug row
// 01a12d18-b200-7662-8bb2-e264674ed4de). The sweep used to decide eligibility
// with `PPID == ProcReaperOf()`, and ProcReaperOf only ever named one of two
// process NAMES ({init} ∪ live `systemd --user`). That is an enumeration of
// reaper names, not the kernel's rule: when a process dies its orphans are
// reparented to the NEAREST ancestor marked PR_SET_CHILD_SUBREAPER, and to init
// only when no ancestor is marked. Below Linux 6.10 /proc does not expose the
// mark (this host is 5.15: `grep -ci subreaper /proc/self/status` → 0), so a
// marked ancestor that is neither init nor systemd --user cannot be enumerated
// at all — and under such an ancestor EVERY leaked orphan carries ITS pid,
// matches nothing in the name set, and the sweep reports "no orphan" for a scene
// full of them. Both cases of this package then burn their whole discovery
// budget: measured 30.06 s + 30.04 s = 60.9 s of red.
//
// WHY THE FIXTURE HAS THREE LAYERS. A marked subreaper must be an ANCESTOR of
// the process that runs the sweep — that is the whole mechanism — and a test
// binary cannot be its own ancestor. So this case plays the shape for real:
//
//	driver (the ordinary `go test` process)
//	  └─ parent — same test binary, prctl(PR_SET_CHILD_SUBREAPER, 1) marks IT,
//	              so it is the kernel's reparent target for everything below it
//	     ├─ leaf     — runs the sweep's OWN case body; the leak it spawns
//	     │             (setsid --fork sleep 600) is reparented to `parent`,
//	     │             which IS on the leaf's chain and is NOT the reaper the
//	     │             leaf can name
//	     └─ control  — a SECOND marked layer, in another branch of the tree: it
//	                   abandons a process of its own (same argv, detached, with
//	                   an owned path INSIDE the sweep's scope). That process is
//	                   not a descendant of the leaf, so its parent is not on the
//	                   leaf's chain — and the sweep must leave it ALONE.
//
// The leaf's assertions are the load-bearing ones, and the first thing it proves
// is that the name arm CANNOT fire there (the leak's ppid is neither init nor
// systemd --user). Without the chain arm this case fails exactly the way the
// epic measured; the control is what keeps the repair honest — eligibility must
// widen to "a reparent target provably above THIS run", never to "any marked
// subreaper".
//
// WHAT IT IS NOT: it spawns two `sleep`s and holds them for the life of one
// case — no load, no saturation, no daemon, no script, no scheduling (Rule 27,
// Rule 35). The mechanism is Go, in this package. Every process it starts is
// terminated by the case's own cleanup, by identity, even when the case fails.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// prSetChildSubreaper is PR_SET_CHILD_SUBREAPER (linux/prctl.h): mark this
	// process as the reparent target for its orphaned descendants. It is the
	// same mark the kernel installs on `systemd --user` for user.slice, named
	// here because the fixture must carry EXACTLY the mechanism it is about.
	prSetChildSubreaper = 36
	// subreaperRoleEnv selects which layer of the fixture a process is. The
	// driver (the ordinary test run) sets no role.
	subreaperRoleEnv = "GOPI_TESTUTIL_SUBREAPER_ROLE"
	// subreaperSceneEnv carries the scene directory from the driver down to the
	// layers, so all of them agree on the tree the processes work in.
	subreaperSceneEnv = "GOPI_TESTUTIL_SUBREAPER_SCENE"
	// subreaperThirdPartyPidFileEnv names the file the control's layer writes
	// the abandoned pid into. The driver builds the path (it owns the scene) and
	// hands it down, so the layers cannot disagree about where it is.
	subreaperThirdPartyPidFileEnv = "GOPI_TESTUTIL_SUBREAPER_TP_PIDFILE"
	// subreaperRoleParent is the marked intermediate layer: the kernel's
	// reparent target for everything below it.
	subreaperRoleParent = "parent"
	// subreaperRoleLeaf runs the sweep's own case body, in a process whose
	// ancestry contains the marked layer.
	subreaperRoleLeaf = "leaf"
	// subreaperRoleThirdParty is the control's marked layer, in a branch of the
	// tree that does not contain the sweeping process.
	subreaperRoleThirdParty = "third-party"
	// subreaperChainSceneName names the scene directory this fixture plays in.
	subreaperChainSceneName = "subreaper-chain-scene"
	// subreaperThirdPartyDirName is the control's working directory INSIDE the
	// sweep's scope: the control's owned path must lie in the scope, or the case
	// would pass because of the path check rather than the ancestry test.
	subreaperThirdPartyDirName = "third-party"
	// subreaperThirdPartyPidFile is where the control's layer publishes the pid
	// of the process it abandoned, so the sweeping layer can assert on it.
	subreaperThirdPartyPidFile = "third-party.pid"
	// subreaperThirdPartyHold is how long the control's layer stays alive. It
	// must outlive the sweeping layer's assertions: that layer is the reparent
	// target of the control's process, and one that exited early would hand the
	// process to an ancestor the sweep IS entitled to reap.
	subreaperThirdPartyHold = 60 * time.Second
	// subreaperThirdPartyWait bounds the sweeping layer's wait for the control
	// to exist — the positive existence of the negative control.
	subreaperThirdPartyWait = 20 * time.Second
	// subreaperPollInterval is the poll cadence of this file's bounded waits.
	subreaperPollInterval = 50 * time.Millisecond
	// subreaperChainWait bounds the driver's wait for the whole layered run. It
	// is generous because each layer pays the package's TestMain (scratch pin +
	// suite-start sweep); what it must never do is pass vacuously, so a run that
	// hangs is killed and reported.
	subreaperChainWait = 5 * time.Minute
	// subreaperChainTestTimeout is the `-test.timeout` handed to each child
	// layer, so a deadlock inside a layer is reported by that layer rather than
	// by the driver's clock.
	subreaperChainTestTimeout = "4m"
)

// TestOrphanSweepUnderAMarkedSubreaperAncestor is ONE case with four roles,
// decided by subreaperRoleEnv: the driver (no role) builds the tree, the marked
// `parent` starts the `leaf` and the control's `third-party` layer, and the
// `leaf` is where every assertion lives.
func TestOrphanSweepUnderAMarkedSubreaperAncestor(t *testing.T) {
	switch os.Getenv(subreaperRoleEnv) {
	case subreaperRoleParent:
		runSubreaperChainParent(t)
	case subreaperRoleLeaf:
		runSubreaperChainLeaf(t)
	case subreaperRoleThirdParty:
		runSubreaperThirdPartyLayer(t)
	default:
		runSubreaperChainDriver(t)
	}
}

// runSubreaperChainDriver starts the marked layer and reports its outcome. It
// asserts nothing about the sweep itself: the verdict belongs to the layer where
// the shape is real, and the driver's job is to make the tree exist and to fail
// loudly if that run failed, timed out, or could not be built.
func runSubreaperChainDriver(t *testing.T) {
	t.Helper()
	requireSetsid(t)
	scene := sceneDir(t, subreaperChainSceneName)

	// The marked layer is started with its OWN process group, so a layer that
	// wedges can be torn down WHOLE — a leftover test binary would hold the
	// scene's cwd and veto the scratch GC (the measured pitfall behind this
	// repo's Setpgid discipline).
	parent := newSubreaperLayer(t, subreaperRoleParent, scene)
	if err := parent.Start(); err != nil {
		t.Fatalf("start the marked-subreaper layer: %v", err)
	}
	pgid := parent.Process.Pid
	t.Cleanup(func() {
		if syscall.Kill(-pgid, 0) != nil {
			return // the whole group is already gone — never signal a stale id
		}
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			t.Logf("the marked-subreaper fixture's process group %d could not be torn down: %v", pgid, err) // Rule 15b
		}
	})

	done := make(chan error, 1)
	go func() { done <- parent.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the marked-subreaper run FAILED (%v) — the failing layer's own output above is the verdict; the sweep's case body must pass under an ancestor the reaper set cannot name", err)
		}
	case <-time.After(subreaperChainWait):
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			t.Logf("teardown of the fixture's process group %d failed: %v", pgid, err) //stylegate:ignore best-effort teardown of this case's own fixture group on the timeout path; the timeout itself is the reported failure
		}
		t.Fatalf("the marked-subreaper run did not finish within %s", subreaperChainWait)
	}
	t.Logf("a marked subreaper ancestor above this run reaps its orphans and the sweep still found them; a marked subreaper that is NOT above this run keeps its own")
}

// newSubreaperLayer builds one child layer of the fixture: the same test binary,
// ONE case, verbose, bounded by its own timeout, with the role and scene in its
// environment, and its own process group.
func newSubreaperLayer(t *testing.T, role, scene string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], subreaperChildFlags(t.Name())...)
	cmd.Env = append(os.Environ(),
		subreaperRoleEnv+"="+role,
		subreaperSceneEnv+"="+scene)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// subreaperChildFlags is the argv each child layer is started with: ONE test
// (this one), verbose so the layers' log lines reach the driver's output, and a
// timeout so a wedged layer reports itself instead of hanging the whole run.
func subreaperChildFlags(name string) []string {
	return []string{
		"-test.run=^" + name + "$",
		"-test.v",
		"-test.timeout=" + subreaperChainTestTimeout,
	}
}

// runSubreaperChainParent is the MARKED layer: it carries the kernel's
// PR_SET_CHILD_SUBREAPER mark for everything below it, starts the control's
// layer and the leaf, and stays alive for the whole leaf run — the mark is what
// parks an abandoned process on THIS pid, so exiting early would hand every leak
// to some other ancestor and dissolve the shape.
func runSubreaperChainParent(t *testing.T) {
	t.Helper()
	scene := os.Getenv(subreaperSceneEnv)
	if scene == "" {
		t.Fatalf("%s is not set — this layer must be started by the driver, which owns the scene", subreaperSceneEnv)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		// NO SKIP: a host that cannot carry the mark cannot build this shape,
		// and silently passing here would leave the regression untested on
		// exactly the hosts that need it (Rule 15b).
		t.Fatalf("prctl(PR_SET_CHILD_SUBREAPER, 1) failed: %v — this layer must really be the kernel's reparent target", errno)
	}
	ppid, err := parentOf(os.Getpid())
	if err != nil {
		t.Fatalf("read this layer's own parent: %v", err)
	}
	t.Logf("marked subreaper layer: pid %d (parent %d) now reaps the abandoned processes of everything it starts", os.Getpid(), ppid)

	// The control runs in a SIBLING branch (never as this layer's own child of
	// the leaf, and never as the driver's child): its marked layer must be off
	// the sweeping process's ancestry chain, or the control would prove nothing.
	sidecar := startThirdPartySidecar(t, scene)
	// Teardown order (LIFO): the scene's leftovers first, then the control's
	// whole process group. Both are needed — a process abandoned by the control
	// reparents to the control's layer, whose own exit would strand it.
	defer killFixtureProcessGroup(t, sidecar.Process.Pid)
	defer killSceneLeftovers(t, scene)

	leaf := newSubreaperLayer(t, subreaperRoleLeaf, scene)
	if err := leaf.Run(); err != nil {
		t.Fatalf("the swept run under a marked subreaper ancestor FAILED: %v", err)
	}
}

// startThirdPartySidecar launches the control's marked layer as a DIRECT child of
// the marked layer, in its own process group, and publishes the pidfile path in
// the child's environment (the parent's own os.Environ() carries it through, so
// one place decides where it lives).
func startThirdPartySidecar(t *testing.T, scene string) *exec.Cmd {
	t.Helper()
	t.Setenv(subreaperThirdPartyPidFileEnv, filepath.Join(scene, subreaperThirdPartyPidFile))
	sidecar := newSubreaperLayer(t, subreaperRoleThirdParty, scene)
	if err := sidecar.Start(); err != nil {
		t.Fatalf("start the third-party control layer: %v", err)
	}
	return sidecar
}

// runSubreaperChainLeaf runs the sweep's OWN case body in a process whose
// ancestry contains the marked layer.
//
// It is deliberately the same sequence of steps as
// TestOrphanSweepFindsAndReapsTheLeakInItsScene, because what regressed was the
// predicate those steps rest on: build the scene's leak, discover it, reap it,
// prove the sweep is idempotent afterwards — and, with the control present the
// whole time, prove the sweep did not widen its reach to it.
func runSubreaperChainLeaf(t *testing.T) {
	t.Helper()
	scene := os.Getenv(subreaperSceneEnv)
	if scene == "" {
		t.Fatalf("%s is not set — this layer must be started by the marked layer, which owns the scene", subreaperSceneEnv)
	}
	marker, err := parentOf(os.Getpid())
	if err != nil {
		t.Fatalf("read this layer's own parent: %v", err)
	}
	t.Logf("sweeping layer: pid %d, its parent (the marked subreaper) is %d", os.Getpid(), marker)

	// The control must EXIST before the sweep runs, or "the sweep left it alone"
	// would be an assertion about a process that was not there yet.
	third := awaitThirdPartyPid(t, scene)
	idThird, err := ReadProcIdentity(third)
	if err != nil {
		t.Fatalf("read the third-party control %d: %v", third, err)
	}
	t.Logf("third-party control %d: ppid %d, cmdline %s, its own marked layer is NOT on this run's chain", third, idThird.PPID, idThird.Cmdline)

	leak := startOrphanInScene(t, scene)
	id, err := ReadProcIdentity(leak)
	if err != nil {
		t.Fatalf("read the scene's leaked daemon %d: %v", leak, err)
	}
	reaper, reason := ProcReaperOf()
	ancestry, closed := ProcReaperChainOf()
	// THE SHAPE, ASSERTED AS MEASURED FACTS: the leak's parent is the marked
	// layer, NOT the reaper this host can name. This is what makes the case a
	// regression PIN rather than a coincidence — if the leak had landed on the
	// named reaper, the OLD predicate would have found it and this case would
	// prove nothing.
	if id.PPID != marker {
		t.Fatalf("the leak %d has ppid %d, want the marked subreaper %d — the fixture did not build the shape (chain %v, closed=%v)",
			leak, id.PPID, marker, ancestry, closed)
	}
	if reaper > 0 && id.PPID == reaper {
		t.Fatalf("the leak %d landed on the reaper this host can name (%d) — this case must exercise the ancestry-chain arm, not the name arm", leak, reaper)
	}
	id.ProcReaper = reaper
	id.ProcReaperChain = ancestry
	if !id.Orphan() {
		t.Fatalf("the leak %d (ppid %d, reaper %d, chain %v) was NOT judged an orphan — the ancestry-chain arm did not fire (this host's reaper answer is %q)",
			leak, id.PPID, reaper, ancestry, reason)
	}
	if !id.Detached() {
		t.Fatalf("the leak %d is not a session leader (session=%d) — the fixture did not build the detached shape", leak, id.Session)
	}
	t.Logf("the leak %d is an orphan by ANCESTRY (ppid %d is on this run's chain %v), not by name (this host's reaper=%d)", leak, id.PPID, ancestry, reaper)

	// (a) the sweep FINDS the leak, with the owned path as its proof.
	found, err := FindOrphansUnderRoot(scene)
	if err != nil {
		t.Fatalf("find orphans under %s: %v", scene, err)
	}
	if !containsPid(found, leak) {
		t.Fatalf("the orphan %d under a marked subreaper ancestor was NOT found (found %v) — the sweep cannot see its own leak", leak, found)
	}
	if containsPid(found, third) {
		t.Fatalf("the sweep judged the third-party control %d an orphan — its marked parent is NOT on this run's chain, so eligibility has widened to 'any marked subreaper'", third)
	}

	// (b) the sweep REAPS the leak... and only the leak.
	swept, err := SweepOrphansUnderRoot(scene)
	if err != nil {
		t.Fatalf("sweep orphans under %s: %v", scene, err)
	}
	if !containsPid(swept, leak) {
		t.Fatalf("the sweep reported no row for pid %d (rows: %v)", leak, swept)
	}
	if containsPid(swept, third) {
		t.Fatalf("the sweep ACTED on the third-party control %d (rows: %v) — it is not a descendant of this run", third, swept)
	}
	if !waitPIDGone(t, leak, orphanTestSweepWait) {
		t.Fatalf("the swept orphan %d is still alive — the sweep did not reap it", leak)
	}

	// (c) THE CONTROL, judged from the only place that can judge it: it was in
	// the scope the whole time, it is orphan-shaped and detached, and it is
	// still running.
	if !SignalZeroAlive(third) {
		t.Fatalf("the third-party control %d was killed by a sweep it is not eligible for", third)
	}
	if idThird.PPID == marker {
		t.Fatalf("the control %d has ppid %d — the very value this run's chain contains, so the control's own shape is wrong and the case proves nothing", third, marker)
	}

	// (d) idempotent: a clean scene reports no rows.
	again, err := SweepOrphansUnderRoot(scene)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a second sweep of the same scene reported %d row(s) — the scene was already clean: %v", len(again), again)
	}
	// Release the control: the assertion is made, and a fixture that outlives
	// the case is a leak this very package exists to prevent.
	if err := KillIfIdentical(idThird, syscall.SIGKILL); err != nil {
		t.Errorf("could not release the third-party control %d after asserting on it: %v", third, err)
	}
}

// runSubreaperThirdPartyLayer is the CONTROL's marked layer: a subreaper in
// another branch of the tree, whose abandoned process must stay invisible.
func runSubreaperThirdPartyLayer(t *testing.T) {
	t.Helper()
	scene := os.Getenv(subreaperSceneEnv)
	if scene == "" {
		t.Fatalf("%s is not set — this layer must be started by the marked layer, which owns the scene", subreaperSceneEnv)
	}
	pidFile := os.Getenv(subreaperThirdPartyPidFileEnv)
	if pidFile == "" {
		t.Fatalf("%s is not set — this layer must be told where to publish the pid it abandons", subreaperThirdPartyPidFileEnv)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); errno != 0 {
		t.Fatalf("prctl(PR_SET_CHILD_SUBREAPER, 1) failed in the control layer: %v", errno)
	}
	own := filepath.Join(scene, subreaperThirdPartyDirName)
	if err := os.MkdirAll(own, dirPerm); err != nil {
		t.Fatalf("create the control's own directory %s: %v", own, err)
	}
	pid := spawnDetachedSleeperIn(t, own)
	if err := os.WriteFile(pidFile, []byte(itoa(pid)), filePerm); err != nil {
		t.Fatalf("publish the abandoned pid to %s: %v", pidFile, err)
	}
	t.Cleanup(func() {
		// Own your own fixture: this layer is normally killed by its parent once
		// the control has been judged, and a piece of it would then be nobody's
		// to clean up. By identity, never by name.
		if id, err := ReadProcIdentity(pid); err == nil {
			if kerr := KillIfIdentical(id, syscall.SIGKILL); kerr != nil {
				t.Logf("control layer: could not kill its own abandoned process %d: %v", pid, kerr) // Rule 15b
			}
		}
	})
	t.Logf("third-party marked layer: pid %d abandoned pid %d in %s (the sweeping run's chain does not contain this layer)", os.Getpid(), pid, own)
	// Stay alive: this layer is the reparent target of the process the control
	// is about, and exiting early would hand that process to an ancestor the
	// sweep IS entitled to reap.
	time.Sleep(subreaperThirdPartyHold) //stylegate:allow-sleep this sleep IS the fixture: the layer must stay alive long enough for the control's abandoned process to be reparented onto it and for the sweeping run to judge the chain; there is no observable to poll for "still required to exist"
}

// spawnDetachedSleeperIn produces the leak shape — a detached, abandoned
// stand-in daemon working in dir — exactly as the sweep's own cases do
// (`sh -c 'cd <dir> && exec setsid --fork sleep 600'`; see
// startOrphanInScene's comment for why `--fork` is load-bearing), and returns
// its pid, discovered from the raw facts rather than through the sweep's
// predicate.
//
// It is a separate helper because the CONTROL's layer cannot use
// awaitScenePID: that discovery IS the predicate, and this layer's own abandoned
// process is — correctly — invisible to it.
func spawnDetachedSleeperIn(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "cd "+shellQuote(dir)+" && exec setsid --fork "+MockDaemonBinary+" "+orphanTestHoldSeconds)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the control's abandoned process: %v", err)
	}
	if !awaitLauncherExit(t, cmd) {
		_ = cmd.Process.Kill() //stylegate:ignore best-effort kill of a launcher that outlived its budget; the failure is reported here
		_, _ = cmd.Process.Wait()
		t.Fatalf("the control's launcher %q never returned — the process was not detached", cmd.Args)
	}
	return awaitSleeperPIDByCwd(t, dir)
}

// awaitSleeperPIDByCwd finds the abandoned stand-in daemon by its WORKING
// DIRECTORY and its finished argv, with no eligibility predicate involved.
//
// The control's layer cannot use awaitScenePID: that helper's discovery IS the
// sweep's predicate, and this layer's own abandoned process is — correctly —
// invisible to it (this layer is not on its own ancestor chain). That
// invisibility is the control's whole point; finding the process needs the raw
// facts (cwd, argv, identity) instead.
func awaitSleeperPIDByCwd(t *testing.T, dir string) int {
	t.Helper()
	deadline := time.Now().Add(orphanTestSweepWait)
	for time.Now().Before(deadline) {
		pids, err := ListPIDs()
		if err != nil {
			t.Fatalf("enumerate processes: %v", err)
		}
		for _, pid := range pids {
			id, rerr := ReadProcIdentity(pid)
			if rerr != nil || strings.TrimSpace(id.Cmdline) != orphanTestSleeperCmdline {
				continue
			}
			cwd, cerr := Cwd(pid)
			if cerr != nil {
				continue
			}
			if filepath.Clean(strings.TrimSuffix(cwd, deletedSuffix)) == filepath.Clean(dir) {
				return pid
			}
		}
		time.Sleep(dlPollInterval) //stylegate:allow-sleep bounded wait for this layer's own fixture process to appear
	}
	t.Fatalf("no process with argv %q appeared with cwd %s within the budget", orphanTestSleeperCmdline, dir)
	return 0
}

// awaitThirdPartyPid waits (bounded) for the control's layer to publish the pid
// of the process it abandoned. A control that never appeared is a FAILED control,
// never a silent pass — the negative assertions rest on it.
func awaitThirdPartyPid(t *testing.T, scene string) int {
	t.Helper()
	path := filepath.Join(scene, subreaperThirdPartyPidFile)
	deadline := time.Now().Add(subreaperThirdPartyWait)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, cerr := strconv.Atoi(strings.TrimSpace(string(b))); cerr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(subreaperPollInterval) //stylegate:allow-sleep bounded wait for the peer layer's fixture to publish itself
	}
	t.Fatalf("the control's layer did not publish its abandoned pid to %s within %s — the negative control did not run", path, subreaperThirdPartyWait)
	return 0
}

// parentOf reads a process's parent out of /proc. It exists so the fixture can
// assert its own shape from the kernel's table rather than from a bookkeeping
// variable it wrote itself.
func parentOf(pid int) (int, error) {
	id, err := readProcStat(pid)
	if err != nil {
		return 0, err
	}
	return id.PPID, nil
}

// killFixtureProcessGroup tears down one fixture layer's whole process group
// (SIGKILL, then nothing): every layer is started with Setpgid, so this is the
// layer and its children, never a bare pid that could be recycled.
func killFixtureProcessGroup(t *testing.T, pgid int) {
	t.Helper()
	if syscall.Kill(-pgid, 0) != nil {
		return // the whole group is already gone — never signal a stale id
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Logf("fixture layer %d could not be torn down: %v", pgid, err) // Rule 15b
	}
}

// killSceneLeftovers terminates, by identity, the processes DESCENDED FROM THIS
// PROCESS that are still working inside scene. It runs on the marked layer's way
// out, where the only processes it can reach are its own descendants (the
// layer's children and grandchildren, found by walking each candidate's ancestry
// back to this pid) — never a scan-and-kill by name, and never a process that
// merely happens to be shaped like a leak.
func killSceneLeftovers(t *testing.T, scene string) {
	t.Helper()
	clean := filepath.Clean(strings.TrimSuffix(scene, string(os.PathSeparator)))
	me := os.Getpid()
	pids, err := ListPIDs()
	if err != nil {
		t.Logf("subreaper fixture cleanup: cannot enumerate processes: %v", err) // Rule 15b
		return
	}
	for _, pid := range pids {
		if pid == me || !descendsFrom(pid, me) {
			continue
		}
		cwd, err := Cwd(pid)
		if err != nil {
			continue
		}
		if filepath.Clean(strings.TrimSuffix(cwd, deletedSuffix)) != clean {
			continue
		}
		id, err := ReadProcIdentity(pid)
		if err != nil {
			continue
		}
		if err := KillIfIdentical(id, syscall.SIGKILL); err != nil {
			t.Logf("subreaper fixture cleanup: could not kill pid %d in %s: %v", pid, scene, err)
			continue
		}
		t.Logf("subreaper fixture cleanup: killed the leftover %d in %s", pid, scene)
	}
}

// descendsFrom reports whether pid's ancestry contains ancestor, using the same
// bounded walk the eligibility predicate itself uses (one implementation, so the
// fixture's cleanup cannot disagree with the mechanism it is pinning).
func descendsFrom(pid, ancestor int) bool {
	db := newProcReaperDB()
	cur := pid
	for i := 0; i < procReaperChainMaxLevels; i++ {
		parent, ok := db.parent(cur)
		if !ok || parent <= 0 || parent == cur {
			return false
		}
		if parent == ancestor {
			return true
		}
		cur = parent
	}
	return false
}
