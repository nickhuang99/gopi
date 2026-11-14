package main

// host_lifecycle_log_test.go — the pins for the ONE declared, per-host lifecycle
// record (host_lifecycle_log.go). Two layers, deliberately:
//
//  1. IN-PROCESS pins of the path resolution, the mirror and the refusals — the
//     rules that must hold whatever launches a component (Rules 1/2/13/14: the
//     path comes from daemonLogPathUnder + the component's base name, never from
//     a literal at a call site).
//  2. A REAL-BINARY E2E in the shape the 2026-09-17 incident was measured in: a
//     worker whose stdio is a PSEUDO-TERMINAL (`setsid nohup …`, fd 1 →
//     /dev/pts/N) and which is then STOPPED. Pre-change, every lifecycle line of
//     such a worker went to that pty and NOTHING named it for a reader — the
//     nightly run's drain/termination records were unobtainable. Post-change the
//     lines are in <gopi-dir>/logs/worker.log and the startup line names it.
//
// The suite's own profile isolation (testmain_test.go) is not relied upon: every
// in-process case pins GOPI_HOME at its own t.TempDir(), because the path under
// test is precisely the one resolved from the gopi data root.

import (
	"context"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
)

// The E2E's budgets and probes (Rule 1: named constants, one place to change).
const (
	// hlPtyBannerWait bounds the wait for the worker's loop banner — the proof
	// the loop really started before the assertions read anything.
	hlPtyBannerWait = 30 * time.Second
	// hlPtyStopWait bounds the wait for the stop line after the signal.
	hlPtyStopWait = 30 * time.Second
	// hlPtyPoll is that wait's cadence.
	hlPtyPoll = 100 * time.Millisecond
	// hlPtyExitWait bounds the wait for the worker process to leave after the
	// drain line — the record must be complete BEFORE the assertions run, and a
	// process that has exited has flushed everything it will ever write.
	hlPtyExitWait = 20 * time.Second
	// hlIdleSecEnv / hlIdleExitsEnv are the loop's own knobs (referenced through
	// their named constants): a 1s drained-queue tick and 0 = never self-exit,
	// so the worker waits for OUR stop instead of self-terminating mid-assert.
	hlIdleSecEnv   = "GOPI_WORKER_IDLE_SEC"
	hlIdleExitsEnv = "GOPI_WORKER_IDLE_EXITS"
	// hlIdleSecValue / hlIdleExitsValue are those knobs' values.
	hlIdleSecValue   = "1"
	hlIdleExitsValue = "0"
)

// hlReadFile reads a file, returning "" when it does not exist yet (a poll
// helper: absence is the normal state while a process warms up).
func hlReadFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// hlWaitFor polls a body-producing probe until pred is satisfied or the budget
// runs out, returning the last body it saw for the failure message. The probe is
// passed the test's context so every wait in this file is bounded by the
// framework's timeout too (a wedged child cannot hang the suite holding a lock),
// and the label is what the context carries (test2json's completion event).
func hlWaitFor(ctx context.Context, budget time.Duration, label string, probe func() string, pred func(string) bool) string {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	body := ""
	for {
		body = probe()
		if pred(body) {
			return body
		}
		select {
		case <-ctx.Done():
			return body
		case <-time.After(hlPtyPoll):
		}
	}
}

// TestHostLifecycleLogPathIsTheDeclaredPerHostFile pins the RESOLUTION: the
// declared record of each component is `<gopi data root>/logs/<component>.log`,
// i.e. the same seam the daemon face already uses — one definition of the
// directory (daemonLogDirName) and of each file name (Rule 2/14). A reader who
// knows only the host can name the file.
func TestHostLifecycleLogPathIsTheDeclaredPerHostFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)

	wantDir := filepath.Join(home, config.ProfileDirName, daemonLogDirName)
	for _, tc := range []struct {
		base string
		want string
	}{
		{workerDaemonLogBaseName, filepath.Join(wantDir, workerDaemonLogBaseName)},
		{mergerLogBaseName, filepath.Join(wantDir, mergerLogBaseName)},
	} {
		got, err := daemonLogPathUnder(tc.base)
		if err != nil {
			t.Fatalf("daemonLogPathUnder(%q): %v", tc.base, err)
		}
		if got != tc.want {
			t.Errorf("the declared lifecycle record of %q resolves to %q, want %q — a reader must be able to name it from the host alone",
				tc.base, got, tc.want)
		}
	}
	// The worker's own resolver (the one the daemon face uses) must agree with
	// the seam — a second spelling would send a reader to a file nobody writes.
	got, err := workerDaemonLogPath()
	if err != nil {
		t.Fatalf("workerDaemonLogPath: %v", err)
	}
	if want := filepath.Join(wantDir, workerDaemonLogBaseName); got != want {
		t.Errorf("workerDaemonLogPath() = %q, want %q", got, want)
	}
}

// TestHostLifecycleLogMirrorsToTheDeclaredFileAndNamesIt pins the three facts an
// operator needs at startup: the file is created, its head names it, the
// STARTUP LINE on the stream NAMES it, and a lifecycle line printed afterwards
// is in BOTH.
func TestHostLifecycleLogMirrorsToTheDeclaredFileAndNamesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Cleanup(closeHostLifecycleLog)

	out := captureStdout(t, func() {
		attachHostLifecycleLog(workerProgTopLevel, workerDaemonLogBaseName, false)
		hostLifecyclePrintln("worker: probe-line-9f21")
	})

	path, err := daemonLogPathUnder(workerDaemonLogBaseName)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	body := hlReadFile(path)
	if body == "" {
		t.Fatalf("the declared lifecycle record %s was not written — the whole point of the seam", path)
	}
	if !strings.Contains(body, "worker: probe-line-9f21") {
		t.Errorf("a lifecycle line must land IN the declared record %s; got:\n%s", path, body)
	}
	if !strings.Contains(body, path) {
		t.Errorf("the record must name ITSELF in its head (a reader who opens the file must learn the path); got:\n%s", body)
	}
	// The stream keeps the line (the launcher's startup log and the operator's
	// terminal are the legacy readers — the mirror is additive, never a move).
	if !strings.Contains(out, "worker: probe-line-9f21") {
		t.Errorf("the lifecycle line must ALSO stay on the process's stream (the launcher and the operator read it there); got:\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("the STARTUP LINE must name the declared record on the process's own stream; got:\n%s", out)
	}
	if !strings.Contains(out, workerProgTopLevel) {
		t.Errorf("the startup line must say which component is speaking; got:\n%s", out)
	}
}

// TestHostLifecycleLogUnavailableIsReportedNotSwallowed is the Rule 15 pin: when
// the declared record cannot be created (here: `logs` exists as a FILE), the
// failure is REPORTED on stderr — naming the consequence — and the lifecycle
// lines keep flowing on the stream. A silent fallback here is exactly how the
// nightly record went missing.
func TestHostLifecycleLogUnavailableIsReportedNotSwallowed(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Cleanup(closeHostLifecycleLog)

	// Make the declared path unusable: the logs DIRECTORY is a regular file.
	gopiDir := filepath.Join(home, config.ProfileDirName)
	if err := os.MkdirAll(gopiDir, 0o755); err != nil {
		t.Fatalf("mkdir gopi dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gopiDir, daemonLogDirName), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("occupy the logs path: %v", err)
	}

	var errOut string
	out := captureStdout(t, func() {
		errOut = captureStderr(t, func() {
			attachHostLifecycleLog(workerProgTopLevel, workerDaemonLogBaseName, false)
			hostLifecyclePrintln("worker: probe-line-1b04")
		})
	})

	if !strings.Contains(errOut, "lifecycle log UNAVAILABLE") {
		t.Errorf("an unusable declared record must be REPORTED (Rule 15); stderr was:\n%s", errOut)
	}
	if !strings.Contains(errOut, filepath.Join(gopiDir, daemonLogDirName)) {
		t.Errorf("the refusal must name the declared record it could not open; stderr was:\n%s", errOut)
	}
	if !strings.Contains(out, "worker: probe-line-1b04") {
		t.Errorf("the component must keep running on its own stream when the record is unavailable; stdout was:\n%s", out)
	}
}

// TestHostLifecycleLogAttachIsRefusedForADaemonChild pins the double-write
// guard: a daemon child's stream IS the declared file (the spawn seam redirected
// it before the fork), so mirroring must be REFUSED there — otherwise every line
// lands in `logs/worker.log` twice and the daemon E2E suites' byte-level view of
// that file changes.
func TestHostLifecycleLogAttachIsRefusedForADaemonChild(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	t.Setenv(workerDaemonEnv, daemonMarkerValue)
	t.Cleanup(closeHostLifecycleLog)

	out := captureStdout(t, func() {
		attachHostLifecycleLog(workerProgTopLevel, workerDaemonLogBaseName, false)
	})
	if hostLifecycleLogFile != nil {
		t.Fatalf("a daemon child must NOT attach the mirror — its stream already IS %s", hostLifecycleLogPath)
	}
	path, err := daemonLogPathUnder(workerDaemonLogBaseName)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Errorf("the mirror CREATED %s for a daemon child — the spawn seam owns that file, this process must not open it", path)
	}
	// It must still SAY where the record is: for a daemon child the path is the
	// file its own stdout/stderr resolve to.
	if !strings.Contains(out, path) {
		t.Errorf("a daemon child's startup line must still name its declared record %s; got:\n%s", path, out)
	}
}

// TestWorkerStartedOnAPtyWritesItsLifecycleToTheDeclaredFile is the incident's
// own shape, on the REAL binary, with the incident's own measurement:
//
//	`setsid nohup gopi worker` — stdio inherited to a PSEUDO-TERMINAL, no
//	--daemon, nothing naming a log file.
//
// Pre-change (measured while writing this test): such a worker's lifecycle
// lines reached ONLY that pty (`readlink /proc/<pid>/fd/1` → /dev/pts/N, the
// 2026-09-17 incident) and `logs/worker.log` was not written at all — so the
// night's stop/termination lines were unobtainable from the file the question
// was posed about. Post-change: the lines are in the declared per-host record
// and the startup line names it.
//
// The assertions are ordered so the test cannot pass for the wrong reason:
//
//	① the process's fd 1 really IS a pty slave (the incident's shape, measured,
//	   not assumed);
//	② the STARTUP LINE naming the declared record is on that pty;
//	③ the declared record carries the loop banner — i.e. the lines the reader
//	   needs are in a file whose name does not depend on the pty;
//	④ after a SIGTERM, the record carries the STOP line — the "who stopped them"
//	   half, which is exactly what the night lost.
func TestWorkerStartedOnAPtyWritesItsLifecycleToTheDeclaredFile(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := wdBin(t)
	home := t.TempDir()
	// The IN-PROCESS resolution must read the same profile the child is handed,
	// or the pins below would compare the record against the wrong file (the
	// suite pins its own sandbox home — this case has its own).
	t.Setenv(config.GopiDirEnv, home)
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings,
		hlIdleSecEnv+"="+hlIdleSecValue,
		hlIdleExitsEnv+"="+hlIdleExitsValue,
		"GOPI_AGENT_SPAWN_GUARD_OFF=1",
	)

	master, slavePath := hlOpenPty(t)
	defer func() { _ = master.Close() }() //stylegate:ignore closing the pty master on the test's way out; the child is already reaped
	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open the pty slave %s: %v", slavePath, err)
	}
	cmd := exec.Command(bin, CliVerbWorker)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s %s on a pty: %v", bin, CliVerbWorker, err)
	}
	_ = slave.Close() //stylegate:ignore the child holds its own descriptor for the slave

	pty := &hlPtyReader{}
	pty.drain(master)
	// The child must never outlive this test — reaped explicitly, by PID, never
	// by pattern (the host runs the operator's own workers).
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGKILL) //stylegate:ignore best-effort kill of a child this test started; ESRCH means it already left
		_, _ = cmd.Process.Wait()               //stylegate:ignore reaping a child this test started; a wait error on an already-reaped child is not actionable
	})

	// ① THE INCIDENT'S SHAPE, MEASURED: the worker's stdout is a pty.
	fd1, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/1", cmd.Process.Pid))
	if err != nil {
		t.Fatalf("readlink /proc/%d/fd/1: %v", cmd.Process.Pid, err)
	}
	if !strings.HasPrefix(fd1, "/dev/pts/") {
		t.Fatalf("the probe must reproduce the incident's shape (stdio on a pty), but fd 1 is %q", fd1)
	}

	recordPath, err := daemonLogPathUnder(workerDaemonLogBaseName)
	if err != nil {
		t.Fatalf("resolve the declared record: %v", err)
	}
	// Config sanity: the child resolves the record inside its own sandbox
	// (GOPI_HOME), which is the profile this test reads.
	wantPath := filepath.Join(home, config.ProfileDirName, daemonLogDirName, workerDaemonLogBaseName)
	if recordPath != wantPath {
		t.Fatalf("the declaring process resolves %q, the sandbox profile is %q — the pins below would read the wrong file", recordPath, wantPath)
	}

	// ② THE STARTUP LINE on the pty names the record.
	deadline := time.Now().Add(hlPtyBannerWait)
	for !strings.Contains(pty.body(), wantPath) {
		if time.Now().After(deadline) {
			t.Fatalf("the worker never named its declared lifecycle record (%s) on its own stream within %s; pty saw:\n%s\nrecord file:\n%s",
				wantPath, hlPtyBannerWait, pty.body(), hlReadFile(wantPath))
		}
		time.Sleep(hlPtyPoll) //stylegate:allow-sleep bounded poll for the child's startup line
	}
	// The loop banner is the SAME startup burst as the record line ② waited for
	// and can land on either side of it under load, so it is waited for with the
	// file's ONE bounded wait instead of sampled once: a single sample here made
	// this case red in the full package while green standalone (2026-10-04: the
	// record line was on the stream at 5.03s and the banner was not, load_end
	// 12.01 on 4 cores — the missing deadline, not the worker).
	ptyHead := hlWaitFor(t.Context(), hlPtyBannerWait, "the worker's loop banner reaches its own stream",
		pty.body,
		func(b string) bool { return strings.Contains(b, procname.WorkerLoopStartLine) })
	if !strings.Contains(ptyHead, procname.WorkerLoopStartLine) {
		t.Fatalf("the worker's loop banner never reached its stream within %s; pty saw:\n%s", hlPtyBannerWait, ptyHead)
	}

	// ③ THE DECLARED RECORD carries the lifecycle lines, though the process's
	//    own stdio is a pty that belongs to nobody.
	body := hlWaitFor(t.Context(), hlPtyBannerWait, "the declared lifecycle record gains the loop banner",
		func() string { return hlReadFile(wantPath) },
		func(b string) bool { return strings.Contains(b, procname.WorkerLoopStartLine) })
	if !strings.Contains(body, procname.WorkerLoopStartLine) {
		t.Fatalf("the declared per-host record %s does not carry the worker's loop banner — the lifecycle lines are still pty-only (the 2026-09-17 incident); record:\n%s\npty:\n%s",
			wantPath, body, pty.body())
	}
	// The record's own head names it too (a reader who opens the file, not the
	// pty, must learn what they are reading) — asserted on the SAME body.
	if !strings.Contains(body, wantPath) {
		t.Errorf("the declared record %s must name itself in its head; got:\n%s", wantPath, body)
	}

	// ④ THE STOP LINE (the "who stopped them" half) lands in the record too.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal the worker: %v", err)
	}
	// The stop lines are the worker's own two verdicts (cli_tasks_worker_stop.go):
	// the INTERRUPT verdict the handler prints the moment the signal is taken
	// (stopReasonSignalSuffix) and the boundary verdict the loop prints as it
	// leaves (workerStopLine/workerDrainLine). Asserting on the producers' own
	// prefixes — not on a spelling copied here — keeps this pin tied to the lines
	// the operator's `--stop` is waiting on.
	body = hlWaitFor(t.Context(), hlPtyStopWait, "the declared lifecycle record gains both stop verdicts",
		func() string { return hlReadFile(wantPath) },
		func(b string) bool {
			return strings.Contains(b, stopReasonSignalSuffix) && strings.Contains(b, workerStopLinePrefix)
		})
	if !strings.Contains(body, stopReasonSignalSuffix) {
		t.Errorf("the signal verdict (%q) never reached the declared record %s; record:\n%s\npty:\n%s",
			stopReasonSignalSuffix, wantPath, body, pty.body())
	}
	if !strings.Contains(body, workerStopLinePrefix) {
		t.Fatalf("after the stop signal the declared record %s must carry the boundary verdict (%q) — the night's termination records were exactly this and existed nowhere; record:\n%s\npty:\n%s",
			wantPath, workerStopLinePrefix, body, pty.body())
	}
	if err := hlWaitForExit(cmd); err != nil {
		t.Errorf("the worker did not leave after the drain: %v", err)
	}
}

// hlWaitForExit waits for the worker to leave (its record is then complete).
func hlWaitForExit(cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(hlPtyExitWait):
		return fmt.Errorf("still running %s after the stop line", hlPtyExitWait)
	}
}

// hlOpenPty opens a pty pair and resolves the slave's device path — the plain
// openpty recipe (testsuite/chat_pty_test.go's, using the exported ioctl helpers
// from internal/lineedit). A host without /dev/ptmx skips: this case's whole
// point is the pty shape, so a substitute stream would assert nothing.
func hlOpenPty(t *testing.T) (master *os.File, slavePath string) {
	t.Helper()
	// Allocated through the package's ONE verified fixture (Rule 13): it
	// confirms the slave fd it opened is a terminal, retrying a bounded number
	// of times, before this helper resolves the path for the child to open.
	pair, err := chatAllocatePtyPair(func() (chatPtyPair, chatPtyCondition) {
		return chatAllocPtyPair(chatPtyWanted{})
	})
	if err != nil {
		if strings.Contains(err.Error(), chatPtyStepOpenMaster) {
			t.Skipf("%v — no %s on this host, so the pty shape the incident was measured in cannot be reproduced", err, chatPtyDevicePath)
		}
		t.Skipf("%v — a host that cannot allocate a pty pair cannot reproduce the pty shape", err)
	}
	t.Cleanup(func() { closeQuietly(pair.Master) })
	// The caller opens the slave PATH itself (the child's descriptor is the
	// case's subject), so the fixture's verified slave is closed here while the
	// master — the thing that keeps that path resolvable to this pty — stays open.
	closeQuietly(pair.Slave)
	return pair.Master, fmt.Sprintf(chatPtySlavePathFormat, pair.Number)
}

// hlPtyReader collects everything a pty master receives (the pty is a byte
// stream with no framing, so the reader is a goroutine and the body is read
// under a lock).
type hlPtyReader struct {
	mu  sync.Mutex
	buf strings.Builder
}

// drain starts the copy loop for a master; it returns when the master is closed
// or the writer end is gone (the child exited).
func (p *hlPtyReader) drain(master *os.File) {
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.buf.Write(buf[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
}

// body returns what the pty has delivered so far.
func (p *hlPtyReader) body() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}
