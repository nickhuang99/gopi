package main

// session_console_log_test.go — the FRONT-RUN console log contract (epic
// task-session-cwd): a front run (`gopi "prompt"`) cannot chdir into its
// session directory, so its console output is COPIED there instead — while
// still reaching the process's original stdout.
//
// The unit half (the fan-out) lives in the first test; the second test drives
// the REAL binary and reads the run's own artifact, which is the epic's actual
// acceptance (the session directory holds what the run printed).

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestConsoleLogFansOutToTheOperatorAndTheSessionDir is the seam's core
// promise: after attachment, everything written to os.Stdout/os.Stderr reaches
// BOTH the run's console.log and the original stream, and restoration puts the
// original streams back.
//
// It exercises the real os.Pipe fan-out rather than a stub: the point of the
// seam is the file-descriptor plumbing, and a stubbed copy would prove nothing
// about it. The original stdout is replaced by a temp file (NOT a buffer — the
// production path holds *os.File) so the goroutine's writes are observable.
func TestConsoleLogFansOutToTheOperatorAndTheSessionDir(t *testing.T) {
	taskWorkdirResetTest() // the workdir latches are process state; keep this test independent of ordering
	t.Cleanup(consoleLogRestore)
	t.Cleanup(taskWorkdirResetTest)

	sessionDir := t.TempDir()
	sessionLog := filepath.Join(sessionDir, "session.jsonl")
	if err := os.WriteFile(sessionLog, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
	operator := filepath.Join(t.TempDir(), "operator-stdout")
	opFile, err := os.Create(operator)
	if err != nil {
		t.Fatalf("create operator sink: %v", err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = opFile, opFile
	t.Cleanup(func() { os.Stdout, os.Stderr = savedOut, savedErr })

	// The two REFUSAL cases (a `task run` child, session logging off) have
	// their own tests below — this one covers the attachment itself, and its
	// precondition is that neither applies. The TTY refusal is driven by the
	// real-binary test in task_session_cwd_e2e_test.go, since a test binary's
	// stdout is never a terminal.
	gotPath := consoleLogAttachFor(sessionLog)
	wantPath := filepath.Join(sessionDir, consoleLogFileName)
	if gotPath != wantPath {
		t.Fatalf("the console log must live in the session directory: got %q want %q", gotPath, wantPath)
	}
	if os.Stdout != consoleLogCurrent.pipeW[0] || os.Stderr != consoleLogCurrent.pipeW[1] {
		t.Fatalf("after attach, stdout and stderr must be their own pipe write ends (a single shared pipe would merge the streams)")
	}
	// The separation is the point: the log file is the ONE shared destination,
	// so two DISTINCT pipes must be open.
	if consoleLogCurrent.pipeW[0] == consoleLogCurrent.pipeW[1] {
		t.Fatalf("stdout and stderr must not share one pipe")
	}

	const marker = "front-run console line\n"
	if _, err := os.Stdout.WriteString(marker); err != nil {
		t.Fatalf("write via the attached stdout: %v", err)
	}
	consoleLogRestore()
	if err := opFile.Close(); err != nil {
		t.Fatalf("close operator sink: %v", err)
	}
	if os.Stdout != opFile || os.Stderr != opFile {
		// (the t.Cleanup restores the real streams; this asserts that
		// consoleLogRestore itself put back what it replaced)
		t.Fatalf("restore must put the replaced streams back")
	}

	for _, p := range []string{operator, wantPath} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if !bytes.Contains(data, []byte(marker)) {
			t.Errorf("%s must hold the run's console output; got:\n%s", p, data)
		}
	}
}

// TestConsoleLogRefusesWhenSessionLoggingIsOff: no session JSONL means no
// session directory to write into, so the seam reports the gap on stderr and
// attaches nothing (Rule 15: the absence is LOUD — a run whose output is not
// recorded must say so rather than look recorded).
func TestConsoleLogRefusesWhenSessionLoggingIsOff(t *testing.T) {
	t.Cleanup(consoleLogRestore)
	savedOut, savedErr := os.Stdout, os.Stderr
	sink, err := os.Create(filepath.Join(t.TempDir(), "sink"))
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}
	defer func() { os.Stdout, os.Stderr = savedOut, savedErr }()
	os.Stdout, os.Stderr = sink, sink

	if got := consoleLogAttachFor(""); got != "" {
		t.Fatalf("no session log ⇒ no attachment; got %q", got)
	}
	if consoleLogCurrent != nil {
		t.Fatalf("a refused attach must not leave attachment state behind")
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close sink: %v", err)
	}
	data, err := os.ReadFile(sink.Name())
	if err != nil {
		t.Fatalf("read sink: %v", err)
	}
	if !bytes.Contains(data, []byte("session logging is off")) {
		t.Errorf("the refusal must be reported, not silent; sink held:\n%s", data)
	}
}

// TestConsoleLogChildRunDoesNotAttach pins the CHILD case against the env var
// the wrapper exports (tools.TaskWrapperEnv) rather than the ambient profile:
// a `task run` child already has <session-dir>/output.log as its stdout, and a
// second copy in the same directory would double every line.
func TestConsoleLogChildRunDoesNotAttach(t *testing.T) {
	t.Cleanup(consoleLogRestore)
	t.Setenv(tools.TaskWrapperEnv, "t-console-child")
	sessionDir := t.TempDir()
	if got := consoleLogAttachFor(filepath.Join(sessionDir, config.SessionsDirName+".jsonl")); got != "" {
		t.Fatalf("a task-run child must not attach; got %q", got)
	}
}
