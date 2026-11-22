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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

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

// consoleLogAttachGuard serialises ATTACH, RESTORE and the fan-out's own
// detach. Without it the two interleave: Restore puts the operator's stream back
// and the fan-out's goroutine then overwrites it with the (now dead) pipe write
// end — or the reverse, closing a read end the copy is still using.
var consoleLogAttachGuard sync.Mutex

// detachOnFanOutExit is the fan-out goroutine's own cleanup, run on EVERY exit —
// both the normal one (Restore closed the write end, the copy returned EOF) and
// the fatal one (the operator's consumer went away and the copy returned
// EPIPE). See the call site for why the fatal arm is a deadlock and not merely
// an early return.
//
// WHAT IT DOES is deliberately the minimum that cannot lose anything: the
// stream is put back ONLY while this attachment is still the current one (a
// Restore that already ran has put the operator's own stream back and must not
// be undone), and the read end is closed so nothing can block on it. It never
// touches the log file — that is Restore's, and the fan-out owes no write after
// its copy returned.
func (c *consoleLog) detachOnFanOutExit(idx int) {
	consoleLogAttachGuard.Lock()
	defer consoleLogAttachGuard.Unlock()
	if consoleLogCurrent != c {
		// Restore already took the attachment apart (it sets consoleLogCurrent
		// nil before touching any stream). It owns the streams now.
		return
	}
	// The stream goes back to the operator's original as soon as no fan-out is
	// left to carry it: a remaining write to os.Stdout then reaches the real
	// consumer directly (and, if that consumer is gone, gets EPIPE — the
	// ordinary non-fatal answer `| head` already relies on).
	if c.pipeW[idx] == os.Stdout && idx == 0 {
		os.Stdout = c.original[0]
	}
	if c.pipeW[idx] == os.Stderr && idx == 1 {
		os.Stderr = c.original[1]
	}
	// Closing the read end is what turns "block forever" into EPIPE: while this
	// process holds a read end nothing consumes, the pipe can never drain.
	c.readEndClose(idx)
}

// readEndClose releases one stream's read end exactly once. It is called from
// the fan-out's exit and, on the failure path of Attach, from a half-built
// attachment; a second close returns EBADF, which is not news.
func (c *consoleLog) readEndClose(idx int) {
	if c.pipeR[idx] == nil {
		return
	}
	if err := c.pipeR[idx].Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		fmt.Fprintf(c.original[1], "[gopi] console log: closing the console pipe: %v\n", err)
	}
	c.pipeR[idx] = nil
}

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
			// THE FAN-OUT'S DEATH MUST NOT OUTLIVE IT AS A DEADLOCK. This is
			// defect row 01a1e59c, measured on a live process (pid 2680213, a
			// one-shot bare-prompt run, frozen 11 days 4 hours and killable only
			// by SIGKILL): when the operator's consumer goes away (`gopi … |
			// head -15`), io.Copy's first write fails, this goroutine EXITS — and
			// the run continues writing to os.Stdout, which is still pipeW here,
			// whose read end `cur.pipeR[idx]` this process is still holding open.
			// A pipe with a live reader and NO reader-side consumption FILLS and
			// the writer BLOCKS FOREVER, in a plain syscall, so no timeout,
			// context or handler can reach it. Reproduced with a 30-line
			// standalone sample: `fan-out exited after 0 bytes: broken pipe` then
			// `WRITE BLOCKED FOREVER`.
			//
			// The cure is one line each: put the ORIGINAL stream back, and close
			// the read end so anything already holding pipeW gets EPIPE (the
			// ordinary, non-fatal `| head` answer) instead of blocking. Both are
			// idempotent under the same guard as Restore.
			cur.detachOnFanOutExit(idx)
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
	consoleLogAttachGuard.Lock()
	cur := consoleLogCurrent
	if cur == nil {
		consoleLogAttachGuard.Unlock()
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
	consoleLogAttachGuard.Unlock()
	for i := range cur.done {
		<-cur.done[i]
		// The fan-out has returned; its read end goes with it. detachOnFanOutExit
		// skipped this because Restore already cleared consoleLogCurrent (that
		// guard exists to stop it overwriting the streams Restore just put
		// back), so the release is owed HERE.
		consoleLogAttachGuard.Lock()
		cur.readEndClose(i)
		consoleLogAttachGuard.Unlock()
	}
	if err := cur.logFile.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] console log: closing %s: %v\n", cur.path, err)
	}
}
