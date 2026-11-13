package tools

// THE PIPELINE-PARTNER GUARD (measured defect 2026-10-27, row 01a17d23).
//
// The shape: the operator asks a question FROM INSIDE the ground —
//
//	cd <ground> && gopi task status --worktree . | grep ground_now
//
// — and the shell forks every stage of that pipeline in the ground, so the
// `grep` (or `tee`) at the other end of the pipe stands in the ground while the
// guard scans /proc. finishCleanupSelfChain excludes only the ASKER'S ANCESTORS,
// so the asker's own pipeline partner was read as a competing live run and the
// reading came back "IN USE by live process pid <pid> (grep)" — the asker's own
// pipe sink. The measurement is in
// the row's evidence directory (baseline-repro.txt / pipeline-tree.txt).
//
// THIS FILE PINS BOTH DIRECTIONS, with REAL processes (a forged pid would prove
// nothing about /proc, which is the only evidence channel here — Rule 24):
//
//	1. TestWorktreeInUseSparesTheAskersOwnPipelinePartner
//	   a partner forked by the same SHELL, in the same process group, after the
//	   asker, standing in the ground → the ground reads FREE (the fix).
//	2. TestWorktreeInUseKeepsAnOlderSameGroupProcess
//	   the SAME pipeline shape with the ordering reversed (the candidate
//	   predates the asker: a live run/daemon that merely shares a group) → still
//	   IN USE. This arm exists so the fix cannot pass by excluding everything
//	   that shares a group.
//	3. TestWorktreeInUseKeepsTheAskersOwnChild
//	   the asker's own DESCENDANT standing in the ground → still IN USE (the
//	   shape epic worktree-not-released measured: the run's own executor child
//	   holds the worktree it is working in).
//	4. TestWorktreeInUseKeepsAThirdPartyInTheGround
//	   an unrelated process (its own process group, its own parent) standing in
//	   the ground → still IN USE. The veto a wrong answer must keep.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	// pipelinePartnerAskerModeEnv marks the re-executed test binary as the
	// ASKER half of the pipeline fixture. Unset = an ordinary test process.
	pipelinePartnerAskerModeEnv = "GOPI_TEST_PIPELINE_ASKER"
	// pipelinePartnerGroundEnv names the ground the fixture judges.
	pipelinePartnerGroundEnv = "GOPI_TEST_PIPELINE_GROUND"
	// pipelinePartnerReadyEnv is the file the partner touches ONCE it is
	// standing in the ground — the fixture's "it is there" signal.
	pipelinePartnerReadyEnv = "GOPI_TEST_PIPELINE_READY"
	// pipelinePartnerGoEnv is the file the asker touches to release the
	// partner, so the partner cannot stand in the ground before the asker was
	// forked (which would invert the start-tick ordering under test).
	pipelinePartnerGoEnv = "GOPI_TEST_PIPELINE_GO"
	// pipelinePartnerDoneEnv is the file the asker touches on its way out; the
	// partner's bounded wait exits on it, so no fixture process outlives the
	// test (Rule 30's residue rule).
	pipelinePartnerDoneEnv = "GOPI_TEST_PIPELINE_DONE"
	// pipelinePartnerResultMarker prefixes the asker's one measured line.
	pipelinePartnerResultMarker = "PIPELINE-PARTNER-RESULT"

	// pipelinePartnerWait bounds the asker's wait for the partner to be
	// observable, and the parent's wait for the asker's output.
	pipelinePartnerWait = 20 * time.Second
	// pipelinePartnerTick is the poll interval inside those waits.
	pipelinePartnerTick = 20 * time.Millisecond
	// pipelinePartnerPartnerLife bounds the PARTNER's wait loop (iterations of
	// 20 ms — Rule 27: a bounded loop is bounded by an iteration cap, not only
	// by a timeout), so a killed fixture cannot leave a sleeper in the ground.
	pipelinePartnerPartnerLife = 1500
	// pipelinePartnerTickSkew separates the two fork waves of the
	// partner-first fixture by several /proc clock ticks, so "the candidate
	// predates the asker" is measurable rather than a same-tick coincidence.
	pipelinePartnerTickSkew = "0.2"
	// pipelinePartnerStandInSeconds is the third party's lifetime: far past any
	// assertion in this file, and it is killed explicitly by the fixture.
	pipelinePartnerStandInSeconds = "300"
)

// TestWorktreeInUsePipelinePartnerAsker is the FIXTURE HALF, not a test: the
// re-executed test binary plays the ASKER of the pipeline and prints one
// measured line for the parent to assert on. It skips whenever the mode
// variable is unset, which is every ordinary run of this package.
func TestWorktreeInUsePipelinePartnerAsker(t *testing.T) {
	if os.Getenv(pipelinePartnerAskerModeEnv) != "1" {
		t.Skip("the pipeline-partner fixture helper runs only as the forked ASKER")
	}
	ground := os.Getenv(pipelinePartnerGroundEnv)
	goFile := os.Getenv(pipelinePartnerGoEnv)
	readyFile := os.Getenv(pipelinePartnerReadyEnv)
	doneFile := os.Getenv(pipelinePartnerDoneEnv)
	if ground == "" || goFile == "" || readyFile == "" || doneFile == "" {
		t.Fatalf("the fixture's environment is incomplete: ground=%q go=%q ready=%q done=%q", ground, goFile, readyFile, doneFile)
	}
	defer func() {
		if err := os.WriteFile(doneFile, []byte("done"), 0o644); err != nil {
			t.Logf("could not release the partner (%s): %v", doneFile, err)
		}
	}()
	// Release the partner (it was forked AFTER this process, so the start-tick
	// ordering under test holds by construction) and wait until it is really
	// standing in the ground.
	if err := os.WriteFile(goFile, []byte("go"), 0o644); err != nil {
		t.Fatalf("release the partner: %v", err)
	}
	waitForFile(t, readyFile)
	// THE RAW READING (never the verdict under test): somebody's cwd must be in
	// the ground, or this fixture would prove nothing.
	standing := pipelinePartnerRawCwdHolders(ground)
	if len(standing) == 0 {
		t.Fatalf("no live process has a cwd inside %s — the fixture cannot measure what it claims to", ground)
	}
	holder, why := finishCleanupWorktreeInUse(ground)
	fmt.Printf("%s holder=%q why=%q standing=%v\n", pipelinePartnerResultMarker, holder, why, standing)
}

// TestWorktreeInUseSparesTheAskersOwnPipelinePartner is the FIX: the asker's
// own pipeline partner — same shell, same process group, forked after the
// asker, standing in the ground — is not a third-party holder.
func TestWorktreeInUseSparesTheAskersOwnPipelinePartner(t *testing.T) {
	ground := t.TempDir()
	line := runPipelinePartnerFixture(t, ground, pipelinePartnerOrderAskerFirst)
	if !strings.Contains(line, `why=""`) {
		t.Fatalf("the asker's own pipeline partner was read as a holder: %s\n(want why=\"\" — the caller's diagnostic plumbing is not a competing run)", line)
	}
}

// TestWorktreeInUseKeepsAnOlderSameGroupProcess is the arm that keeps the fix
// from degenerating into "nothing in my group counts": the SAME pipeline shape
// with the ordering reversed — the candidate was forked BEFORE the asker, so it
// cannot be part of the pipeline the asker started (a live run or a daemon that
// merely shares the shell's process group). It keeps the veto.
func TestWorktreeInUseKeepsAnOlderSameGroupProcess(t *testing.T) {
	ground := t.TempDir()
	line := runPipelinePartnerFixture(t, ground, pipelinePartnerOrderPartnerFirst)
	if strings.Contains(line, `why=""`) {
		t.Fatalf("a process that PREDATES the asker and merely shares its process group was spared: %s\n(want a refusal — only the asker's own fork wave may be excluded)", line)
	}
	if !strings.Contains(line, finishCleanupInUseMarker) {
		t.Fatalf("the refusal does not carry the in-use marker %q: %s", finishCleanupInUseMarker, line)
	}
}

// TestWorktreeInUseKeepsTheAskersOwnChild is the descendant arm: a child of the
// asker standing in the ground is a legitimate holder (the run's own executor
// child, epic worktree-not-released 2026-09-24), and it must stay one.
func TestWorktreeInUseKeepsTheAskersOwnChild(t *testing.T) {
	ground := t.TempDir()
	cmd := exec.Command("sleep", pipelinePartnerStandInSeconds)
	cmd.Dir = ground // the child is forked AFTER this process and inherits its group
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn the descendant holder: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore teardown of the child this test spawned; no verdict depends on its exit status
		_, _ = cmd.Process.Wait()
	})
	waitUntilHolderVisible(t, ground)
	if _, why := finishCleanupWorktreeInUse(ground); why == "" {
		t.Fatalf("the asker's OWN descendant standing in %s was spared — a run's executor child is a real holder", ground)
	}
}

// TestWorktreeInUseKeepsAThirdPartyInTheGround is the veto itself: an unrelated
// process — its own process group (Setpgid, the shape this repo's own spawn
// seams use for executor children), its own ancestry — keeps the ground IN USE.
func TestWorktreeInUseKeepsAThirdPartyInTheGround(t *testing.T) {
	ground := t.TempDir()
	cmd := exec.Command("sleep", pipelinePartnerStandInSeconds)
	cmd.Dir = ground
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn the third-party holder: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore teardown of the third party this test spawned; no verdict depends on its exit status
		_, _ = cmd.Process.Wait()
	})
	waitUntilHolderVisible(t, ground)
	if _, why := finishCleanupWorktreeInUse(ground); why == "" {
		t.Fatalf("a third party standing in %s was spared — that is the veto this guard exists for", ground)
	}
}

// pipelinePartnerOrder says which half the shell forks first.
type pipelinePartnerOrder int

const (
	// pipelinePartnerOrderAskerFirst is the measured defect's shape: the asker
	// is forked, then its pipeline partner.
	pipelinePartnerOrderAskerFirst pipelinePartnerOrder = iota
	// pipelinePartnerOrderPartnerFirst reverses it: the candidate predates the
	// asker and merely shares its group.
	pipelinePartnerOrderPartnerFirst
)

// runPipelinePartnerFixture forks the two halves from ONE shell (that is the
// whole point: a pipeline's stages are siblings, which is exactly what the
// ancestry-only exclusion missed) and returns the asker's measured line.
//
// The shell is the fixture's own child and is signalled by PID only (Rule 42);
// the partner's wait loop is bounded by an iteration cap, so nothing the
// fixture starts can outlive it.
func runPipelinePartnerFixture(t *testing.T, ground string, order pipelinePartnerOrder) string {
	t.Helper()
	bin := os.Args[0]
	dir := t.TempDir()
	goFile := filepath.Join(dir, "go")
	readyFile := filepath.Join(dir, "ready")
	doneFile := filepath.Join(dir, "done")
	fifoFile := filepath.Join(dir, "release")
	if err := syscall.Mkfifo(fifoFile, 0o600); err != nil {
		t.Fatalf("create the release fifo %s: %v", fifoFile, err)
	}
	asker := fmt.Sprintf("%s -test.run=^TestWorktreeInUsePipelinePartnerAsker$", bin)
	// The partner blocks on a FIFO this test releases — ONE long-lived process
	// standing in the ground. Measured reason for the FIFO over a polling loop:
	// a `while [ ! -f <release> ]; do sleep 0.02; done` partner forks a `sleep`
	// every 20 ms, and
	// a scan that catches one of those mid-exit reads an UNMEASURABLE start tick,
	// which the criterion refuses — a flake in the FIXTURE, not in the guard
	// (measured: the arm failed 1 run in 3 with `holder="pid <pid> (?)"`).
	partner := fmt.Sprintf(
		`sh -c 'cd %q || exit 1; touch %q; exec cat %q > /dev/null'`,
		ground, readyFile, fifoFile)
	script := asker + " & " + partner // the asker is forked first
	if order == pipelinePartnerOrderPartnerFirst {
		// The candidate is forked FIRST and the asker only after a real delay:
		// /proc starttime is measured in clock ticks, so two forks in the same
		// tick are indistinguishable by ordering (measured — the first version
		// of this arm forked both in one tick and the candidate read as a
		// partner). The delay is several ticks, so "the candidate predates the
		// asker" is a MEASURED fact, not a coin flip.
		script = partner + " & sleep " + pipelinePartnerTickSkew + " ; " + asker
	}
	// `wait` is load-bearing: without it the shell exits the moment both jobs
	// are forked, the parent reads an empty pipe, and the fixture would look
	// green/red for a reason unrelated to the criterion (measured: the first
	// version of this fixture produced "no measured line" in 0.00s).
	script += " & wait"
	sh := exec.Command("sh", "-c", script)
	sh.Dir = dir // never the ground: the shell's own cwd is not the subject
	sh.Env = append(os.Environ(),
		pipelinePartnerAskerModeEnv+"=1",
		pipelinePartnerGroundEnv+"="+ground,
		pipelinePartnerGoEnv+"="+goFile,
		pipelinePartnerReadyEnv+"="+readyFile,
		pipelinePartnerDoneEnv+"="+doneFile,
	)
	sh.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	// The shell's output goes through OUR pipe, read by a goroutine, because the
	// partner is released at TEARDOWN: waiting for the shell to exit before
	// reading the asker's line would deadlock for the whole test timeout.
	slurp, slurpErr := sh.StdoutPipe()
	if slurpErr != nil {
		t.Fatalf("capture the fixture output: %v", slurpErr)
	}
	sh.Stderr = os.Stderr
	if err := sh.Start(); err != nil {
		t.Fatalf("start the pipeline fixture: %v", err)
	}
	var mu sync.Mutex
	output := ""
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := slurp.Read(buf)
			if n > 0 {
				mu.Lock()
				output += string(buf[:n])
				mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = sh.Process.Kill() //stylegate:ignore teardown of the shell this test spawned; no verdict depends on its exit status
		_, _ = sh.Process.Wait()
		// Release the partner: it is blocked in open(2) on the FIFO, so this write
		// is what lets `cat` reach EOF and exit — the fixture leaves nothing in
		// the ground (Rule 30).
		if f, oerr := os.OpenFile(fifoFile, os.O_WRONLY, 0); oerr == nil {
			_, _ = f.WriteString("release") //stylegate:ignore teardown release; the partner's exit status is not a verdict here
			_ = f.Close()                   //stylegate:ignore teardown close; ditto
		}
	})
	deadline := time.Now().Add(pipelinePartnerWait)
	for {
		mu.Lock()
		snapshot := output
		mu.Unlock()
		for _, line := range strings.Split(snapshot, "\n") {
			if strings.HasPrefix(line, pipelinePartnerResultMarker) {
				return line
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the asker produced no measured line within %s; raw output:\n%s", pipelinePartnerWait, snapshot)
		}
		time.Sleep(pipelinePartnerTick) //stylegate:allow-sleep polls the fixture's own output pipe for the asker's single measured line
	}
}

// pipelinePartnerRawCwdHolders is the RAW reading the fixture needs before it
// may assert anything: the pids whose cwd lies in ground, read with the shared
// /proc readers and NEVER with the verdict under test (a fixture that asked the
// verdict whether the fixture is visible could pass for the wrong reason).
func pipelinePartnerRawCwdHolders(ground string) []string {
	pids, err := finishCleanupPIDDirs()
	if err != nil {
		return nil
	}
	var out []string
	for _, pid := range pids {
		cwd, ok := finishCleanupProcCWD(pid)
		if !ok || !finishCleanupPathUnder(cwd, ground) {
			continue
		}
		n, convErr := strconv.Atoi(pid)
		if convErr != nil || !pidAlive(n) {
			continue
		}
		out = append(out, pid)
	}
	return out
}

// waitUntilHolderVisible blocks until the shared cwd scan sees somebody in
// ground, so a fixture never asserts on a process the kernel has not exposed
// yet. It is a hard requirement (never a skip): a skip here would let the test
// pass for the wrong reason.
func waitUntilHolderVisible(t *testing.T, ground string) {
	t.Helper()
	deadline := time.Now().Add(pipelinePartnerWait)
	for {
		if len(pipelinePartnerRawCwdHolders(ground)) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no live process has a cwd inside %s after %s — this fixture cannot measure what it claims to", ground, pipelinePartnerWait)
		}
		time.Sleep(pipelinePartnerTick) //stylegate:allow-sleep polls /proc for the spawned holder's cwd, which the kernel exposes a moment after fork
	}
}

// waitForFile blocks until path exists (a hard requirement, as above).
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(pipelinePartnerWait)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared within %s", path, pipelinePartnerWait)
		}
		time.Sleep(pipelinePartnerTick) //stylegate:allow-sleep polls for the fixture's own marker file, written by a process forked a moment ago
	}
}
