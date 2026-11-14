package main

// session_console_log.go — the FRONT-RUN console log seam (epic
// task-session-cwd, operator ruling 2026-09-12).
//
// The epic's rule for a task-serving process is "stand in the task's own
// directory, so everything it produces lands there". Two modes cannot obey the
// FIRST half of that rule:
//
//	`gopi "prompt"`            — a front run in the operator's shell: its cwd
//	                             is the operator's working context BY
//	                             DEFINITION (GOPI.md, the search roots and the
//	                             relative-path tools all read it), so moving it
//	                             would change what the run MEANS, not just
//	                             where it logs;
//	a detached/daemon process  — same reason (Rule 6's operator context).
//
// For those, the epic requires the SECOND half explicitly: "still we require
// the log must be outputed to session directory even cwd is impossible to
// change when running in front". This file is that half — the run's console
// stream is COPIED into <session-dir>/console.log while still reaching the
// operator's terminal, so a front run is debuggable from its session
// directory exactly like a task run is (the id leads to everything the run
// produced).
//
// WHY A COPY AND NOT A REDIRECT OF THE FILE DESCRIPTOR: the process cannot
// reopen fd 1 onto the log file without taking the operator's terminal away
// (`gopi "prompt"` must still print its answer, and `gopi "prompt" | tee`
// must still pipe). os.Pipe + a streaming copy gives both: os.Stdout/os.Stderr
// point at the pipe's write end, and the copy goroutine fans every chunk out
// to the operator's ORIGINAL stdout and to the log. No dup2, no new
// dependency, and the original streams are restored on Close.
//
// WHEN IT IS DELIBERATELY NOT ATTACHED (each a decided case, not a fallback —
// Rule 15's "no silent failure" is about ERRORS, and none of these is one):
//
//	session logging unavailable → no session dir exists to write into;
//	stdout is a TTY           → the operator's terminal IS the record, live
//	                            and already scrolling; a second copy in the
//	                            profile is redundant;
//	task-RUN CHILD            → the spawn seam already points its stdout at the
//	                            task's output.log (cli_tasks_exec_output.go),
//	                            so attaching here would duplicate every line
//	                            into a second file in the same directory.
//
// A run that DOES qualify and cannot be attached reports it on stderr — the
// epic's whole point is that a missing log must be visible, never guessed at.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// consoleLogFileName is the front-run console record inside a session
	// directory: one task/run, one directory, beside session.jsonl and the raw
	// HTTP log (Rule 1: the name lives here, never as a literal at a call site).
	consoleLogFileName = "console.log"

	// consoleLogMode is the created file's permission bits (operator-readable,
	// never executable — the same posture as the worker's output log).
	consoleLogMode = 0o644
)

// consoleLog is the process-wide attachment state: the original streams, the
// per-stream pipe write ends and the log file. Single attachment per process
// (a one-shot run has exactly one session), so package state is the whole
// state machine — the same reasoning as workerLog.
//
// ONE PIPE PER STREAM, deliberately (measured): a single pipe carrying both
// streams makes os.Stdout and os.Stderr the SAME file object, and the copy can
// only send everything to one destination — so redirecting stdout into a file
// would also swallow the run's diagnostic notices, and a consumer reading only
// stdout would receive the run's errors. The first version did exactly that and
// broke a suite that asserts an interrupt notice arrives on STDERR. Two pipes
// keep the separation exact: stdout fans out to the operator's stdout + the
// log, stderr to the operator's stderr + the same log.
type consoleLog struct {
	original [2]*os.File // 0 = stdout, 1 = stderr
	pipeR    [2]*os.File // read ends, indexed like original
	pipeW    [2]*os.File // write ends, installed as os.Stdout / os.Stderr
	logFile  *os.File
	path     string
	done     [2]chan struct{}
}

// consoleLogCurrent is the active attachment (nil = none).
var consoleLogCurrent *consoleLog

// consoleLogAttachFor points this process's console output at the session
// directory of sessionLogPath (the run's JSONL) while keeping the operator's
// stdout alive, and returns the path it writes ("" = deliberately not
// attached, with the reason reported on stderr when the run qualified).
//
// Attachment is refused (returns "" and changes NO stream — a half-attached
// process would be worse than an unattached one) when the run is a `task run`
// CHILD (its stdout already IS the task's output log) or when stdout is a TTY
// (the operator's terminal is the live record).
func consoleLogAttachFor(sessionLogPath string) string {
	if consoleLogCurrent != nil {
		return consoleLogCurrent.path // already attached — idempotent
	}
	if os.Getenv(tools.TaskWrapperEnv) != "" {
		// A `task run` child: the spawn seam already pointed this
		// process's stdout at <session-dir>/output.log.
		return ""
	}
	if isTerminal(os.Stdout) {
		// An interactive front run: the terminal is the record.
		return ""
	}
	if sessionLogPath == "" {
		fmt.Fprintf(os.Stderr, "[gopi] console log: session logging is off for this run — its console output will not be recorded in a session directory\n")
		return ""
	}
	path := filepath.Join(sessionlayout.RecordContainer(sessionLogPath), consoleLogFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, consoleLogMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] console log: cannot open %s: %v — this run's console output will not be recorded in its session directory\n", path, err)
		return ""
	}
	cur := &consoleLog{
		original: [2]*os.File{os.Stdout, os.Stderr},
		logFile:  f,
		path:     path,
	}
	for i := range cur.pipeW {
		r, w, err := os.Pipe()
		if err != nil {
			// A HALF-attached process would be worse than an unattached one:
			// every pipe already made is closed, the log file is released, no
			// stream is touched, and the refusal is reported (Rule 15).
			for j := 0; j < i; j++ {
				_ = cur.pipeR[j].Close() //stylegate:ignore unconditional release of a pipe this loop just created and is abandoning; the operator is being told about the real failure on the next line
				_ = cur.pipeW[j].Close() //stylegate:ignore same unconditional release as the read end above
			}
			fmt.Fprintf(os.Stderr, "[gopi] console log: pipe: %v — this run's console output will not be recorded in %s\n", err, path)
			_ = f.Close() //stylegate:ignore nothing was ever written to f; a close error here cannot lose a log line and the caller is on its way to reporting the real failure
			return ""
		}
		cur.pipeR[i], cur.pipeW[i] = r, w
		cur.done[i] = make(chan struct{})
	}
	consoleLogCurrent = cur
	os.Stdout, os.Stderr = cur.pipeW[0], cur.pipeW[1]
	// The fan-out, one goroutine per stream: every chunk the process writes
	// reaches the operator's ORIGINAL stream (so a front run prints, a pipe
	// still receives, and stdout/stderr stay separate) and the run's
	// console.log — which is the ONE destination both share, so the file holds
	// the run's full console record even though the streams never merge.
	// Reading ends when that stream's write end closes (Restore below).
	for i := range cur.pipeW {
		go func(idx int) {
			defer close(cur.done[idx])
			_, copyErr := io.Copy(io.MultiWriter(cur.original[idx], f), cur.pipeR[idx])
			// A non-nil error here is a broken operator pipe (the consumer
			// went away — `gopi … | head` semantics), not a lost log: the file
			// was written before the writer returned the error. Reported on the
			// ORIGINAL stderr so it is not lost with the closed pipe.
			if copyErr != nil {
				fmt.Fprintf(cur.original[1], "[gopi] console log: %v (the operator's stream went away; %s keeps what was written)\n", copyErr, path)
			}
		}(i)
	}
	fmt.Fprintf(cur.original[1], "[gopi] console log: %s\n", path)
	return path
}

// consoleLogRestore ends the attachment: the pipe's write end is closed so the
// copy goroutine drains and exits, the original streams are put back BEFORE the
// wait (so the closing line reaches the operator's terminal, not the pipe), and
// the log file is closed last. Idempotent and safe when nothing was attached.
func consoleLogRestore() {
	cur := consoleLogCurrent
	if cur == nil {
		return
	}
	consoleLogCurrent = nil
	os.Stdout = cur.original[0]
	os.Stderr = cur.original[1]
	for i := range cur.pipeW {
		if err := cur.pipeW[i].Close(); err != nil {
			fmt.Fprintf(os.Stderr, "[gopi] console log: closing the console pipe: %v\n", err)
		}
	}
	for i := range cur.done {
		<-cur.done[i]
	}
	if err := cur.logFile.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] console log: closing %s: %v\n", cur.path, err)
	}
}
