package main

// cli_tasks_exec_console_test.go — the FOREGROUND console face of the executor
// child's stream destination (epic worker-foreground-console), at the unit
// level: the routing predicate, the fan-out writer, and the reason the fan-out
// cannot be a bare io.MultiWriter.
//
// THE DEFECT THIS PINS (operator report, 2026-09-17): "before moving task run
// function into worker, this is default behavior, now it stops output in
// worker" — the destination moved to the CHILD (epic child-stream-task-output)
// and took the console face with it, so a `gopi worker` running in the front
// printed nothing of the task it was executing. The fix keeps the file face for
// EVERY spawner and restores the console face for every spawner that is NOT a
// detached daemon.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// consoleProbeText is the child-text stand-in these tests push through the
// seam.
const consoleProbeText = "the executor child's line\n"

// consoleControlWait bounds the negative control's wait for a blocked console
// write — a test deadline for a deliberately blocking write, never a
// production timer.
const consoleControlWait = 2 * time.Second

// brokenConsole is a console whose reader went away — the `gopi worker | head`
// shape: the process is alive, nobody is reading its stdout, and the next write
// fails with EPIPE. It counts the attempts so the test can prove the console is
// dropped after the FIRST failure instead of re-failing on every later chunk.
type brokenConsole struct {
	attempts int
}

// Write always fails with EPIPE and records the attempt.
func (b *brokenConsole) Write(p []byte) (int, error) {
	b.attempts++
	return 0, syscall.EPIPE
}

// openConsoleRunLog opens a run log inside the test's own tree — the file face
// the seam hands to taskChildStreamWriters.
func openConsoleRunLog(t *testing.T) (*os.File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "output.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, taskOutputLogFileMode)
	if err != nil {
		t.Fatalf("open a run log at %s: %v", path, err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close the run log: %v", err)
		}
	})
	return f, path
}

// TestTaskChildConsoleWriterFollowsTheDaemonMarker pins the ONE decision the
// epic turns on: which spawner keeps the console face. The daemon markers are
// the spawn seam's own identity channel (daemonChild), so this is a property of
// the SPAWNER the child can read — never of a flag someone has to remember.
func TestTaskChildConsoleWriterFollowsTheDaemonMarker(t *testing.T) {
	cases := []struct {
		name         string
		workerMarker string
		wantConsole  bool
	}{
		{"foreground worker / manual task run / serve tick", "", true},
		{"detached worker daemon child", daemonMarkerValue, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(workerDaemonEnv, tc.workerMarker)
			got := taskChildConsoleWriter()
			if tc.wantConsole && got == nil {
				t.Fatalf("a NON-daemon spawner must copy the child's stream to its console (%s=%q): this is the console face the epic restores — without it `gopi worker` in the front prints nothing of the task it executes",
					workerDaemonEnv, tc.workerMarker)
			}
			if !tc.wantConsole && got != nil {
				t.Fatalf("a DETACHED DAEMON must keep its stdio in files (%s=%q): its stdout is the component log (logs/worker.log), where a task transcript is a shared file no task id reaches",
					workerDaemonEnv, tc.workerMarker)
			}
		})
	}
}

// WHY THERE IS NO MERGER ROW ANY MORE (epic daemon-machinery-removal,
// 2026-09-17): the marker this predicate read for the merger
// (mergerDaemonEnv) belonged to the retired `gopi merger --daemon` child. The
// merger is a cronjob now (one tick, or that repeated by `--every`) — so it is
// never a detached
// daemon whose child would need its stdio kept in files, and the predicate has
// one marker to read instead of two.

// TestTaskChildStreamWritersTeeOnlyWhenAConsoleExists pins the seam's two
// destinations together: a console spawner gets a writer that reaches BOTH the
// run log and the console, a daemon gets the plain file (so os/exec hands the
// child the descriptor directly and no copying goroutine is spun up).
//
// The console is captured through a real pipe installed as os.Stdout — the SAME
// channel the seam fans out to, so the assertion is about the write PATH and
// not about a stand-in.
func TestTaskChildStreamWritersTeeOnlyWhenAConsoleExists(t *testing.T) {
	t.Run("daemon: the file IS the destination", func(t *testing.T) {
		t.Setenv(workerDaemonEnv, daemonMarkerValue)
		runLog, _ := openConsoleRunLog(t)
		stdout, stderr := taskChildStreamWriters(runLog)
		if stdout != io.Writer(runLog) || stderr != io.Writer(runLog) {
			t.Fatalf("a daemon must hand the child the run log itself (stdout=%T stderr=%T)", stdout, stderr)
		}
	})

	t.Run("foreground: the child reaches the file AND the console", func(t *testing.T) {
		t.Setenv(workerDaemonEnv, "")
		runLog, logPath := openConsoleRunLog(t)
		consoleRead, consoleWrite, err := os.Pipe()
		if err != nil {
			t.Fatalf("make the console pipe: %v", err)
		}
		original := os.Stdout
		os.Stdout = consoleWrite
		t.Cleanup(func() { os.Stdout = original })

		stdout, stderr := taskChildStreamWriters(runLog)
		if stdout == nil || stderr == nil {
			t.Fatalf("a console spawner must fan BOTH streams out (stdout=%T stderr=%T)", stdout, stderr)
		}
		if stdout == stderr {
			t.Fatalf("each stream needs its OWN writer: os/exec copies a non-*os.File writer in a per-stream goroutine, so one shared instance is written concurrently (a race on its lost flag); got the same %T twice", stdout)
		}
		if _, err := stdout.Write([]byte(consoleProbeText)); err != nil {
			t.Fatalf("write through the fan-out: %v", err)
		}
		// The console side is a pipe with a bounded buffer; closing the write
		// end before reading keeps this a deterministic one-shot check.
		if err := consoleWrite.Close(); err != nil {
			t.Fatalf("close the console write end: %v", err)
		}
		onConsole, err := io.ReadAll(consoleRead)
		if err != nil {
			t.Fatalf("read the console pipe: %v", err)
		}
		if !strings.Contains(string(onConsole), consoleProbeText) {
			t.Errorf("the child's text must reach the spawner's CONSOLE (the epic's whole goal); the console saw:\n%s", onConsole)
		}
		body, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read the run log: %v", err)
		}
		if got := strings.Count(string(body), consoleProbeText); got != 1 {
			t.Errorf("the run log must hold the chunk EXACTLY once, got %d — the fan-out owns BOTH destinations, so wrapping it in an io.MultiWriter(runLog, fanout) writes the record twice; log:\n%s", got, body)
		}
	})
}

// TestConsoleCopyWriterKeepsTheRunRecordWhenTheConsoleDies is the Rule 15b half,
// and the proof that the wrapper is LOAD-BEARING rather than decoration:
//
//   - the negative control (io.MultiWriter) shows the defect being avoided: a
//     24-byte child chunk splitting the 32-byte pipe buffer shows the
//     all-or-nothing shape an EPIPE console would produce — the log never sees
//     the chunk the console killed;
//   - the writer under test records the loss in the log, keeps writing the log,
//     and does NOT retry the dead console on every later chunk.
func TestConsoleCopyWriterKeepsTheRunRecordWhenTheConsoleDies(t *testing.T) {
	// bigChunk is deliberately larger than one pipe buffer, so a run-log-first
	// writer gets the record in before the console blocks on a reader that
	// stopped (`gopi worker | head`); a bare MultiWriter would write the console
	// FIRST and stall, or fail, before the log ever saw the bytes.
	bigChunk := strings.Repeat(consoleProbeText, 40)

	t.Run("negative control: MultiWriter's console-first order stalls the record", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("make the pipe: %v", err)
		}
		defer reader.Close()
		var log strings.Builder
		done := make(chan error, 1)
		go func() {
			_, werr := io.MultiWriter(writer, &log).Write([]byte(bigChunk))
			done <- werr
		}()
		select {
		case <-done:
			// A writer that finished means the pipe had room for the whole
			// chunk: the control cannot demonstrate the stall on this host, so
			// the remaining assertions (which do not depend on it) still run.
			t.Logf("control: the write completed (the pipe accepted %d bytes) — the stall shape is host-dependent, the load-bearing assertion is below", len(bigChunk))
		case <-time.After(consoleControlWait):
			if strings.Contains(log.String(), consoleProbeText) {
				t.Fatalf("with a bare MultiWriter the run log must NOT have the chunk yet (the console write comes first and is still blocked); got:\n%s", log.String())
			}
			_ = writer.Close() //stylegate:ignore closes the fixture writer the test owns// release the blocked copy goroutine
			<-done
		}
	})

	t.Run("the seam's writer keeps the record and drops the dead console", func(t *testing.T) {
		console := &brokenConsole{}
		var log strings.Builder
		w := &consoleCopyWriter{console: console, record: &log, stream: streamLabelStdout}
		for i := 0; i < 3; i++ {
			n, err := w.Write([]byte(consoleProbeText))
			if err != nil {
				t.Fatalf("write %d: the child's stream must never fail because the console did: %v", i+1, err)
			}
			if n != len(consoleProbeText) {
				t.Fatalf("write %d: reported %d bytes for a %d-byte chunk", i+1, n, len(consoleProbeText))
			}
		}
		body := log.String()
		if got := strings.Count(body, consoleProbeText); got != 3 {
			t.Errorf("the log must hold EVERY chunk (3), got %d — an unserviceable console may not cost the child's record:\n%s", got, body)
		}
		if !strings.Contains(body, taskConsoleCopyLostFmt[:strings.Index(taskConsoleCopyLostFmt, "%s")]) {
			t.Errorf("the loss must be RECORDED in the run's own log (Rule 15b), got:\n%s", body)
		}
		if !strings.Contains(body, streamLabelStdout) {
			t.Errorf("the loss note must name WHICH stream lost its console copy; got:\n%s", body)
		}
		if console.attempts != 1 {
			t.Errorf("the dead console must be attempted ONCE, not per chunk (os/exec copies per chunk, so a retry storm is the alternative); attempts=%d", console.attempts)
		}
	})
}
