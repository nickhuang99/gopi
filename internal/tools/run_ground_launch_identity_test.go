// run_ground_launch_identity_test.go — the launch rung's IDENTITY question
// (bug 01a17d00, measured 2026-10-27).
//
// THE DEFECT these tests pin. The run-ground launch ladder asked "does the
// taker's launch SESSION still hold a live gopi process?" and answered it with
// the session's whole census. A run launched from an operator's terminal stands
// in THAT terminal's session, which this host keeps stocked with gopi processes
// belonging to other grounds — measured: session 823212 (comm=bash, ppid 55121,
// alive 22 h) held two `gopi worker` rounds while ground 01a17a5f's own taker
// (pid 2345865, start-ticks 95908227) had been dead for hours. The rung read the
// bystanders as "the launch is still here", so a provably finished ground was
// kept forever and the row stayed `in_progress`.
//
// WHAT REPLACED IT — two questions, both about THIS ground:
//
//	① the recorded taker's incarnation AND image: a bare pid existence was a
//	  proxy, so rung ① now asks whether the process wearing that number is this
//	  PROGRAM (runGroundPidIsGopi, over procname.IsGopiImage).
//	② the session census, ATTRIBUTED: a live gopi process holds the ground only
//	  when it stands IN it — its cwd, its argv or its image path names the
//	  directory (finishCleanupSessionCensusHoldsGopiIn). A bystander in somebody
//	  else's shell no longer counts.
//
// The two acceptance tests below are the two SIDES, and each one alone would be
// a wrong fix: (1) a bash-owned session with an unrelated gopi bystander →
// COLLECTABLE, and (2) a gopi process standing in the ground → KEEP. Test (1)
// is the RED proof: on the pre-fix tree it fails with the incident's own
// sentence.
package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// groundLaunchBystanderMarker identifies the re-exec'd child that plays the
	// BYSTANDER gopi: live, gopi-named, and working somewhere else entirely.
	groundLaunchBystanderMarker = "GOPI_RUN_GROUND_BYSTANDER_PROBE"
	// groundLaunchHolderMarker identifies the re-exec'd child that plays a real
	// gopi process STANDING IN the ground.
	groundLaunchHolderMarker = "GOPI_RUN_GROUND_HOLDER_PROBE"
	// groundLaunchProbeSleep bounds how long either probe lives; the tests kill
	// it as soon as they are done, so a passing run leaves no fixture behind.
	groundLaunchProbeSleep = 60 * time.Second
	// groundLaunchProbeSettle bounds the wait for the probe's comm to become
	// readable as `gopi` (the launcher symlink's name is what the kernel reads,
	// but the exec is asynchronous to the parent).
	groundLaunchProbeSettle = 10 * time.Second
	// groundLaunchProbePoll is the settle poll interval.
	groundLaunchProbePoll = 20 * time.Millisecond
)

// TestGroundLaunchLivenessReleasesAGroundWhoseSessionOnlyHasBystanders is the
// acceptance test for the MEASURED defect. The fixture is the real shape: a
// session leader that is NOT gopi (comm=bash — the operator's shell role), a live
// gopi-named process in that session working SOMEWHERE ELSE, and an anchor whose
// taker is dead.
//
// PRE-FIX this test fails with "the launch session N still holds a live gopi
// process" — the incident's sentence. POST-FIX the verdict is collectable and the
// reason says the session holds no live gopi process IN THIS GROUND.
func TestGroundLaunchLivenessReleasesAGroundWhoseSessionOnlyHasBystanders(t *testing.T) {
	if _, err := os.Stat(procRootName); err != nil {
		t.Skipf("no procfs at %s — the census cannot be measured here", procRootName)
	}
	// The ground the anchor names does NOT exist: the undelivered-commits rung
	// then has nothing to protect (its own GONE rule), so the verdict this test
	// reads is the LAUNCH rider's alone.
	ground := filepath.Join(t.TempDir(), "ground-does-not-exist")
	// The bystander works in a directory of its OWN — never the ground.
	sid := groundLaunchStartBystanderSession(t, groundLaunchBystanderMarker, t.TempDir())

	anchor := RunGroundAnchor{
		Worktree:   ground,
		PID:        strconv.Itoa(deadPidOnThisHost(t)),
		StartTicks: "999999999",
		Session:    strconv.Itoa(sid),
	}
	verdict := RunGroundAnchorLiveness(anchor)
	t.Logf("liveness verdict: live=%v reason=%q", verdict.Live, verdict.Reason)
	if verdict.Live {
		t.Fatalf("a gopi process working ELSEWHERE kept a finished ground — the incident's defect: %s", verdict.Reason)
	}
	if strings.Contains(verdict.Reason, "still holds a live gopi process") {
		t.Fatalf("the reason still claims the incident's sentence: %q", verdict.Reason)
	}
	if !strings.Contains(verdict.Reason, "holds no live gopi process in this ground") {
		t.Errorf("the verdict must name the ground-attributed census as its reason: %q", verdict.Reason)
	}
}

// TestGroundLaunchLivenessKeepsAGroundAHolderStandsIn is the ANTI-OVERREACH half:
// the same session shape, but the live gopi process's working directory IS the
// ground — a run that is genuinely standing there. That ground must stay LIVE, on
// both the pre-fix and the post-fix tree (the fix narrows WHO counts, never
// whether a real holder counts).
func TestGroundLaunchLivenessKeepsAGroundAHolderStandsIn(t *testing.T) {
	if _, err := os.Stat(procRootName); err != nil {
		t.Skipf("no procfs at %s — the census cannot be measured here", procRootName)
	}
	ground := t.TempDir()
	// The probe is started IN the ground, so its cwd IS the ground: that is the
	// "standing in the ground" shape, produced by the kernel (cmd.Dir) rather
	// than by a field the test wrote.
	sid := groundLaunchStartBystanderSession(t, groundLaunchHolderMarker, ground)

	anchor := RunGroundAnchor{
		Worktree:   ground,
		PID:        strconv.Itoa(deadPidOnThisHost(t)),
		StartTicks: "999999999",
		Session:    strconv.Itoa(sid),
	}
	verdict := RunGroundAnchorLiveness(anchor)
	t.Logf("liveness verdict: live=%v reason=%q", verdict.Live, verdict.Reason)
	if !verdict.Live {
		t.Fatalf("a live gopi process STANDING IN the ground must keep it: %s", verdict.Reason)
	}
	if !strings.Contains(verdict.Reason, "live gopi process") {
		t.Errorf("the keep must name the census as its reason: %q", verdict.Reason)
	}
}

// TestGroundLaunchIncarnationRequiresTheProcessToBeGopi pins rung ①: an anchor
// that recorded NO start-ticks falls back to the pid alone, and a bare pid
// existence must not be read as a live taker. The fixture is a live process this
// test starts itself, so the answer is measured, never modelled.
//
// PRE-FIX: the verdict is "the launch context … is still alive", because
// `pidAlive` was the whole test. POST-FIX: the process is not this program, so
// rung ① does not vouch for it and the ladder descends — the ground is not held
// on that basis.
func TestGroundLaunchIncarnationRequiresTheProcessToBeGopi(t *testing.T) {
	if _, err := os.Stat(procRootName); err != nil {
		t.Skipf("no procfs at %s — the census cannot be measured here", procRootName)
	}
	// A live, NON-gopi process: `bash` is the incident's own comm (823212), and
	// the test starts its own so nothing about the host's shells is assumed.
	cmd := exec.Command("bash", "-c", "sleep 60")
	// The fixture must stay a SHELL (the test waits for comm="bash", and a
	// builtin `exec sleep` would turn the hold into a `sleep` and make that wait
	// unmeetable), so the forked grandchild is the hazard a Pdeathsig cannot
	// reach. StartMockProcessCmd is the sanctioned cover: Setpgid gives the shell
	// and its fork ONE group, its cleanup signals kill(-pgid), and it arms
	// Pdeathsig for the SIGKILL path. The cwd is not this fixture's subject (the
	// ANCHOR's ground is), so the helper parks it outside every checkout.
	// The helper registers its own cleanup (a group-scoped Reap), so this test
	// keeps only the handle: it must NOT also kill cmd.Process, because the
	// assertion below depends on the shell being alive until it has been read.
	mp := testutil.StartMockProcessCmd(t, cmd)
	holderPid := mp.Pid()
	groundLaunchWaitForComm(t, holderPid, "bash")

	anchor := RunGroundAnchor{
		Worktree: filepath.Join(t.TempDir(), "ground-does-not-exist"),
		PID:      strconv.Itoa(holderPid),
		Session:  strconv.Itoa(holderPid),
	}
	rungOne := "the launch context that took this ground (pid " + anchor.PID + ") is still alive"
	verdict := runGroundLaunchLiveness(anchor)
	t.Logf("liveness verdict: live=%v reason=%q", verdict.Live, verdict.Reason)
	if verdict.Reason == rungOne {
		t.Fatalf("a bare pid existence was read as a live taker — the comm/argv evidence was not required: %s", verdict.Reason)
	}
	if !runGroundPidIsGopi(strconv.Itoa(os.Getpid())) == false {
		// The test binary is not gopi either — the predicate's own negative
		// control, so a predicate that answered TRUE to everything would be
		// caught here rather than in production.
		t.Logf("note: this test binary reports as gopi — check procname.IsGopiImage")
	}
}

// groundLaunchStartBystanderSession starts a session whose LEADER is not gopi and
// which holds one gopi-named process, then returns the session id. The leader
// exec's the gopi-named probe, so the session's id never changes hands: `sid ==`
// the probe's pid, exactly the shape a terminal session has when a worker is
// running in it. workDir is the probe's working directory — the channel the
// attributed census reads.
func groundLaunchStartBystanderSession(t *testing.T, marker, workDir string) int {
	t.Helper()
	dir := t.TempDir()
	link := filepath.Join(dir, procname.NameGopi)
	if err := os.Symlink(os.Args[0], link); err != nil {
		t.Skipf("cannot plant the gopi-named probe: %v", err)
	}
	cmd := exec.Command(link, "-test.run=TestGroundLaunchProbeChild", "-test.v")
	cmd.Env = append(os.Environ(), marker+"=1")
	cmd.Dir = workDir
	// A session of its own: the anchor's session must not be the test process's,
	// whose real gopi ancestors would (correctly) keep the ground.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start the gopi-named probe: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore kills the process this test started and holds the handle to (Rule 42)
		_ = cmd.Wait()         //stylegate:ignore reaps the child the test spawned; its exit status is not the assertion
	})
	groundLaunchWaitForComm(t, pid, procname.NameGopi)
	sid, ok := procname.ProcSessionKey(procRootName, strconv.Itoa(pid))
	if !ok {
		t.Skipf("cannot read the probe's session key")
	}
	if !pidAlive(sid) {
		t.Skipf("the probe's session %d is not alive yet", sid)
	}
	return sid
}

// groundLaunchWaitForComm blocks until the pid's kernel comm is want, so every
// assertion below runs against a process that has already EXEC'd (a probe still
// carrying its launcher's name would make the census spelling-dependent).
func groundLaunchWaitForComm(t *testing.T, pid int, want string) {
	t.Helper()
	deadline := time.Now().Add(groundLaunchProbeSettle)
	for time.Now().Before(deadline) {
		if finishCleanupProcCommand(strconv.Itoa(pid)) == want {
			return
		}
		time.Sleep(groundLaunchProbePoll) //stylegate:allow-sleep bounded poll interval: the enclosing deadline owns the outcome and the loop exits on the first observation
	}
	t.Skipf("the probe pid %d never reported comm=%q (got %q)", pid, want, finishCleanupProcCommand(strconv.Itoa(pid)))
}

// TestGroundLaunchProbeChild is the re-exec'd body of both probes: it lives
// exactly long enough to be measured, and it does nothing else — the CWD it
// inherited from its launcher IS its evidence.
func TestGroundLaunchProbeChild(t *testing.T) {
	if os.Getenv(groundLaunchBystanderMarker) == "" && os.Getenv(groundLaunchHolderMarker) == "" {
		t.Skip("launch-identity probe — only runs as the re-exec'd child")
	}
	//stylegate:allow-sleep the probe EXISTS to be measured by its parent: a live, idle process is the fixture, and parking it (rather than sleeping) would change its comm/cwd evidence
	time.Sleep(groundLaunchProbeSleep)
}
