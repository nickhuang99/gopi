package main

// The resume seam's tests, in three arms:
//
//   - a SOURCE-SHAPE arm pinning the ONE property a refactor could silently
//     lose: the run lock is taken BEFORE anything in resumeSession creates or
//     writes a session file (the checkpoint load, the agent construction, the
//     ledger tag and the run itself). A lock placed after any of those would be
//     a refusal that already wrote, which is the defect the epic exists to
//     remove — and it is invisible to a behavioural test whose fixture has no
//     checkpoint to write;
//   - a REAL BEHAVIOUR arm driving the shipped resume core with the lock held
//     by this process: non-zero exit, the lock path named, the holder's pid and
//     start ticks named, and the run directory byte-identical afterwards;
//   - a KILL arm: a genuinely separate process takes the lock, is SIGKILLed,
//     and the directory is immediately (no sleep) lockable again — the kernel
//     is the cleaner, which is the whole reason the lock is a flock.

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/runlock"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// resumeSeamRunID is the run directory this file's fixtures live in. It is a
// task-id-shaped address so the directory is one of the profile's sessions
// directories, exactly like a real run's.
const resumeSeamRunID = "01a1a8be-ddf9-7b63-82b0-8ecf9dbed82b"

// resumeSeamProbeEnv names the directory the holder probe locks; its presence
// is what turns the probe test body on.
const resumeSeamProbeEnv = "RUNLOCKRESUME_PROBE_DIR"

// resumeSeamProbeReady is the line the probe prints once it HOLDS the lock, so
// the parent knows the refusal it will observe is about a real holder rather
// than a race with a process that has not started yet.
const resumeSeamProbeReady = "RESUME-LOCK-HOLDER-READY"

// resumeSeamProbeHoldCap bounds how long the probe would hold the lock if
// nothing kills it. It exists so a broken parent cannot leave a process behind;
// the test kills it well before this.
const resumeSeamProbeHoldCap = 60 * time.Second

// resumeSeamFixture is a run directory of this profile holding one raw log —
// the shape a resume continues.
type resumeSeamFixture struct {
	root   string // the sessions root
	dir    string // <root>/<run-id> — the run directory, and the lock's address
	rawLog string
}

func newResumeSeamFixture(t *testing.T) resumeSeamFixture {
	t.Helper()
	root := t.TempDir()
	// The RESUME SEAM resolves the root through config.SessionsRoot (one
	// source, Rule 14), so the fixture states it the same way instead of
	// assuming where the profile put it.
	t.Setenv(config.SessionsRootEnv, root)
	dir := filepath.Join(root, resumeSeamRunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	raw := filepath.Join(dir, resumeSeamRunID+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("→ REQUEST\n{}\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	// The seam's process-wide hand-off (run_lock_seam.go) must start empty for
	// each case, or a directory held by an earlier case would be handed off
	// instead of locked.
	prev := runLockHeldDir
	runLockHeldDir = ""
	t.Cleanup(func() { runLockHeldDir = prev })
	return resumeSeamFixture{root: root, dir: dir, rawLog: raw}
}

// dirSnapshot renders every entry of dir with its size and mtime, so "nothing
// was written" is measured rather than asserted.
func dirSnapshot(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var sb strings.Builder
	for _, e := range ents {
		info, ierr := e.Info()
		if ierr != nil {
			t.Fatalf("stat %s: %v", e.Name(), ierr)
		}
		sb.WriteString(e.Name())
		sb.WriteString(" ")
		sb.WriteString(info.ModTime().UTC().Format(time.RFC3339Nano))
		sb.WriteString(" ")
		sb.WriteString(itoaPID(int(info.Size())))
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestResumeRefusesWhenTheRunDirectoryIsLocked is the epic's acceptance ①: with
// another holder on <run-dir>/run.lock, the resume exits non-zero, names the
// lock path and the holder's pid/start ticks, and leaves the run directory
// untouched.
//
// The holder here is this process (a second descriptor — flock treats two open
// descriptions as two competing holders even inside one process, which is the
// reason the seam's hand-off exists), and the separate-process case is the
// kill arm below plus the epic's own CLI measurement.
func TestResumeRefusesWhenTheRunDirectoryIsLocked(t *testing.T) {
	fx := newResumeSeamFixture(t)

	holder, err := runlock.Lock(fx.dir)
	if err != nil {
		t.Fatalf("the fixture could not take the lock it is about to hold: %v", err)
	}
	defer func() {
		if rerr := holder.Release(); rerr != nil {
			t.Errorf("release fixture lock: %v", rerr)
		}
	}()
	lockPath := holder.Path()
	if lockPath != runlock.PathIn(fx.dir) {
		t.Fatalf("lock path = %q, want %q", lockPath, runlock.PathIn(fx.dir))
	}

	before := dirSnapshot(t, fx.dir)
	var code int
	stderr := captureStderrStream(t, func() {
		code = runResume([]string{fx.rawLog})
	})
	after := dirSnapshot(t, fx.dir)

	if code == 0 {
		t.Fatalf("the resume SUCCEEDED while %s was held by pid %d — two processes would write one run directory\nstderr:\n%s",
			lockPath, os.Getpid(), stderr)
	}
	if !strings.Contains(stderr, lockPath) {
		t.Errorf("the refusal does not name the lock path %q — an operator cannot stat it:\n%s", lockPath, stderr)
	}
	if !strings.Contains(stderr, "pid "+itoaPID(os.Getpid())) {
		t.Errorf("the refusal does not name the holder's pid %d:\n%s", os.Getpid(), stderr)
	}
	if !strings.Contains(stderr, "startticks ") {
		t.Errorf("the refusal does not carry the holder's start ticks — a pid alone is recyclable:\n%s", stderr)
	}
	if !strings.Contains(stderr, resumeProgName) {
		t.Errorf("the refusal does not name the face the operator typed (%q):\n%s", resumeProgName, stderr)
	}
	if before != after {
		t.Errorf("the refused resume WROTE into the run directory — the lock must be taken before any session file is created\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestResumeTakesAndReleasesTheRunLock is acceptance ③: with nobody holding the
// directory the resume takes the lock (the same file, in the same run
// directory) and gives it back, so the next resume is not blocked by a lock
// nobody holds.
func TestResumeTakesAndReleasesTheRunLock(t *testing.T) {
	fx := newResumeSeamFixture(t)

	rel, arbitrated, err := resumeTakeRunLock(resumeProgName, fx.rawLog)
	if err != nil {
		t.Fatalf("resumeTakeRunLock with no holder = %v, want the lock", err)
	}
	if rel == nil {
		t.Fatal("resumeTakeRunLock returned a nil release — the caller's defer would panic")
	}
	if !arbitrated {
		t.Fatal("resumeTakeRunLock reports arbitrated=false while holding the lock — the holder gate would then keep a refusal it has already disproved")
	}
	path := runlock.PathIn(fx.dir)
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("the lock file %s was not created: %v", path, statErr)
	}
	// While held, this process CANNOT take it again on a second descriptor —
	// that is the evidence the lock really excludes, not merely that a file
	// exists.
	if h, herr := runlock.Lock(fx.dir); herr == nil {
		_ = h.Release() //stylegate:ignore teardown right before an unconditional failure
		rel()
		t.Fatalf("a second holder acquired %s while the resume held it — the lock excludes nobody", path)
	}
	rel()
	// ...and after the release the next resume can take it.
	rel2, _, err := resumeTakeRunLock(resumeProgName, fx.rawLog)
	if err != nil {
		t.Fatalf("resumeTakeRunLock after the release = %v, want the lock", err)
	}
	rel2()
}

// TestResumeLockIsFreeImmediatelyAfterKillNine is the epic's acceptance ②: the
// holder is SIGKILLed and the directory is lockable again with NO sleep and no
// cleanup step — the kernel released it with the process.
func TestResumeLockIsFreeImmediatelyAfterKillNine(t *testing.T) {
	fx := newResumeSeamFixture(t)
	probe := startResumeLockHolder(t, fx.dir)
	// Kill first, then measure: an immediate re-lock attempt is the assertion,
	// so nothing may sit between the two.
	if err := probe.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL the holder: %v", err)
	}
	// Reap it, so the measurement is about a DEAD process rather than a zombie
	// still holding a descriptor.
	if _, werr := probe.Process.Wait(); werr != nil {
		t.Logf("wait for the killed holder: %v (its exit status is not the assertion)", werr)
	}
	rel, arbitrated, err := resumeTakeRunLock(resumeProgName, fx.rawLog)
	if err != nil {
		t.Fatalf("the run directory is STILL locked after a SIGKILLed holder — a resume would need a stale-lock cleanup step: %v", err)
	}
	if !arbitrated {
		t.Error("the seam took the lock but reports arbitrated=false — the holder gate would keep a refusal the kernel has already disproved")
	}
	rel()
}

// startResumeLockHolder runs a separate process that takes the lock and prints
// the ready line, so the caller knows the holder is real before it acts.
func startResumeLockHolder(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestZZNoSuchTestResumeLockHolder$", "-test.v")
	cmd.Env = append(os.Environ(), resumeSeamProbeEnv+"="+dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the holder probe: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill() //stylegate:ignore teardown of a process this test started; a failed kill only leaves a probe that exits on its own cap
		}
	})
	rd := bufio.NewReader(stdout)
	deadline := time.Now().Add(resumeSeamProbeHoldCap)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("the holder probe never reported %q", resumeSeamProbeReady)
		}
		line, rerr := rd.ReadString('\n')
		if strings.Contains(line, resumeSeamProbeReady) {
			// Drain the rest in the background so the probe's later output
			// cannot block on a full pipe while the test is measuring.
			go func() { _, _ = io.Copy(io.Discard, rd) }() //stylegate:ignore the probe's remaining output is not read by anyone
			return cmd
		}
		if rerr != nil {
			t.Fatalf("the holder probe ended before it held the lock: %v (%q)", rerr, line)
		}
	}
}

// TestZZNoSuchTestResumeLockHolder is never run as a test: it is the body the
// arm above launches. With the env set it takes the lock, announces that it
// holds it, and then stays alive until it is killed (or its cap expires).
func TestZZNoSuchTestResumeLockHolder(t *testing.T) {
	dir := os.Getenv(resumeSeamProbeEnv)
	if dir == "" {
		t.Skip("probe body: only meaningful when launched with " + resumeSeamProbeEnv)
	}
	h, err := runlock.Lock(dir)
	if err != nil {
		t.Fatalf("the probe could not take %s: %v", runlock.PathIn(dir), err)
	}
	defer func() {
		if rerr := h.Release(); rerr != nil {
			t.Logf("probe release: %v", rerr)
		}
	}()
	t.Log(resumeSeamProbeReady)
	time.Sleep(resumeSeamProbeHoldCap) //stylegate:allow-sleep deliberate HOLD, not a poll: the sleep IS this probe's body — the child must stay alive holding the lock until the outer arm kills it, so the kill path (not a deadline) owns the outcome, and the cap only bounds a parent that died without killing it
}

// TestResumeTakesTheLockBeforeAnySessionWrite pins the ORDER inside
// resumeSession, which is the epic's actual requirement: the lock precedes
// every call that creates or writes a session file. A behavioural test cannot
// see this (a fixture with no checkpoint writes nothing either way), so the
// order is read off the function itself.
func TestResumeTakesTheLockBeforeAnySessionWrite(t *testing.T) {
	body := seamFuncBody(t, "cli_resume.go", "func resumeSession(")
	lockAt := strings.Index(body, "resumeTakeRunLock(progName, rawLogPath)")
	if lockAt < 0 {
		t.Fatalf("resumeSession does not take the run lock at all — the resume face is unguarded")
	}
	for _, later := range []string{
		"agent.LoadCheckpointWithSource(",
		"newAgentResuming(",
		"tools.TaskTagResumed(",
		"tools.TaskStampRun(",
		"ag.Resume(",
	} {
		at := strings.Index(body, later)
		if at < 0 {
			continue // the arm may move; the ones present must still be after
		}
		if at < lockAt {
			t.Errorf("%s appears BEFORE the run lock in resumeSession — a refused resume would already have written session files", later)
		}
	}
	if !strings.Contains(body, "defer resumeLockRelease()") {
		t.Errorf("resumeSession does not defer the lock release — the run directory would stay locked for the life of the process")
	}
	// The refusal must be FATAL: returning past a lock error would run the
	// resume anyway, which is the opposite of the lock.
	refusal := body[lockAt:]
	if !strings.Contains(refusal, "return 1") {
		t.Errorf("the lock refusal does not exit non-zero — a resume refused for a held lock must not continue")
	}
}

// TestResumeSeamLeavesNonRunDirectoriesUnlocked pins the honest limit: a
// checkpoint that is not inside a run directory of this profile cannot be
// guarded by a run-directory lock, and the seam SAYS so instead of inventing a
// directory to lock (which runlock.Lock would MkdirAll into existence).
func TestResumeSeamLeavesNonRunDirectoriesUnlocked(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.SessionsRootEnv, root)
	outside := filepath.Join(t.TempDir(), "gopi-raw-export.log")
	if err := os.WriteFile(outside, []byte("→ REQUEST\n{}\n"), 0o644); err != nil {
		t.Fatalf("write export raw log: %v", err)
	}
	prev := runLockHeldDir
	runLockHeldDir = ""
	t.Cleanup(func() { runLockHeldDir = prev })

	var rel func()
	var arbitrated bool
	var err error
	stderr := captureStderrStream(t, func() {
		rel, arbitrated, err = resumeTakeRunLock(resumeProgName, outside)
	})
	if err != nil {
		t.Fatalf("a checkpoint outside the sessions root must not be refused: %v", err)
	}
	if rel == nil {
		t.Fatal("the seam returned a nil release for the unlocked shape — the caller's defer would panic")
	}
	if arbitrated {
		t.Error("the seam reports arbitrated=true while it took NO lock — a LIVE holder claim would then be overridden on the strength of a measurement nothing made")
	}
	rel()
	if !strings.Contains(stderr, "NOT taken") {
		t.Errorf("the seam took no lock and said nothing — a silent skip is the failure Rule 15 forbids:\n%s", stderr)
	}
	if _, statErr := os.Stat(runlock.PathIn(filepath.Dir(outside))); statErr == nil {
		t.Error("the seam created a lock file beside an exported raw log — it must not invent a directory to guard")
	}
}

// TestResumeSeamHonoursTheProcessHandOff pins the ONE process-wide fact both
// faces share: a directory THIS process already holds is handed off (a no-op
// release) rather than refused, because flock is per open file description and
// a resume driven by a process already executing that run directory would
// otherwise deadlock against its own descriptor.
func TestResumeSeamHonoursTheProcessHandOff(t *testing.T) {
	fx := newResumeSeamFixture(t)
	if _, _, err := resumeTakeRunLock(resumeProgName, fx.rawLog); err != nil {
		t.Fatalf("first resumeTakeRunLock: %v", err)
	}
	held := runLockHeldDir
	if held != fx.dir {
		t.Fatalf("runLockHeldDir = %q, want the run directory %q", held, fx.dir)
	}
	rel2, _, err := resumeTakeRunLock(resumeProgName, fx.rawLog)
	if err != nil {
		t.Fatalf("second resumeTakeRunLock in the same process = %v — a self-refusal would deadlock a resume driven from a run", err)
	}
	rel2()
	if runLockHeldDir != held {
		t.Errorf("the hand-off release freed the directory — the outer holder lost ownership")
	}
}
