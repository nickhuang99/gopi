package main

// cli_tasks_exec_output.go — the EXECUTOR CHILD's STREAM DESTINATION,
// established at the ONE spawn seam (runTaskRunOnce).
//
// WHAT CHANGED AND WHY (epic child-stream-task-output, 2026-09-17). The child
// used to INHERIT the spawner's stdout/stderr (`cmd.Stdout = os.Stdout`), so
// where a task's text landed depended on whoever spawned it: the operator's
// terminal for a manual `gopi task run`, the launcher's
// logs/worker-startup.log for a worker round's pre-claim window, the task's
// session dir only after the WORKER had re-pointed its own os.Stdout at
// CLAIM time (workerAttachOutputLog), or the service log. Three spawn paths,
// three destinations, one of them reachable only through a parent-side
// re-point that existed solely to compensate for the inheritance.
//
// The destination now belongs to the CHILD and is derived from the ROW: the
// spawn seam opens <session-dir>/output.log and hands it to the child as its
// stdout AND stderr. The seam is shared by every path (`task run`, the worker
// round, the serve tick all funnel through runTaskRunOnce), so the answer to
// "where did this task's output go?" is ONE path for all of them — and it is
// the row's own session directory, beside the session JSONL, the raw log, the
// report and the attachments.
//
// The path is resolved by tools.TaskLogPath — the SAME resolver `task show`
// globs (Rule 2/14: one spelling of the path, no second one to drift), keyed
// on the TASK ID because that is the only key that exists here: the child's
// session_id does not exist yet (the child creates its session; the row's
// session_id is backfilled from it moments later, TaskBackfillSession).
//
// The tree itself is <gopi-dir>/sessions/<task-id> — the location
// internal/agent's sessionDirForID already resolves for a TASK run (its
// taskID branch), which is what makes the executor's text land beside its own
// session JSONL and raw log rather than in a second tree. Both are joins on
// config.SessionsDirName (Rule 14: one source of truth for the directory
// name), and the container is created on use by either side.
//
// WHAT THIS FILE REPLACED (all deleted in the same change, no dead flag):
//   - `gopi worker --worker-log-root DIR` and GOPI_WORKER_LOG_ROOT — the knob
//     that told the worker where to re-point itself. Nothing to configure
//     remains: the tree is the profile's own <gopi-dir>/sessions.
//   - the claim-time attach / detach / close machinery and the
//     os.Stdout / os.Stderr re-point (cmd/agent/cli_tasks_worker_output.go).
//   - the launcher's flag/env passing (scripts/gopi-worker-up.sh).
//
// WHAT IS DELIBERATELY UNCHANGED:
//   - the session JSONL and the raw HTTP log keep their own paths and
//     writers (this file touches neither);
//   - the worker launcher's fixed pre-claim sink,
//     <gopi-dir>/logs/worker-startup.log, still holds everything the launcher
//     itself prints — including the round's grep-able facts (claim /
//     attribution / workdir), which the launcher's stream receives because
//     no parent-side re-point diverts them any more;
//   - `--once`'s printed lines are the launcher's stdout, untouched.
//
// THE CONSOLE FACE (epic worker-foreground-console, 2026-09-17, ADDENDUM). Moving
// the destination to the child also took away the stream the child used to
// INHERIT — so a `gopi worker` running in the front stopped printing the task it
// was executing, which is the operator's report ("before moving task run
// function into worker, this is default behavior, now it stops output in
// worker"). The file face was the gain and it stays; the console face is
// restored as a TEE for every spawner that is NOT a detached daemon
// (taskChildStreamWriters + taskChildConsoleWriter below). A daemon keeps the
// file alone BECAUSE its own stdio is already <gopi-dir>/logs/<component>.log —
// a per-component file that no task id reaches.

import (
	"fmt"
	"io"
	"os"

	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// taskOutputLogFileMode is the created output log's permission bits
	// (operator-readable, never executable).
	taskOutputLogFileMode = 0o644

	// taskOutputLogHeaderFmt is the ONE header line written into a FRESHLY
	// created output log, so the file identifies itself when opened directly
	// rather than through `task show` (Rule 1: the message shape lives here).
	// The session id is deliberately absent: at spawn time the child's session
	// does not exist yet, and a header naming a made-up session would be a
	// lie in the evidence.
	taskOutputLogHeaderFmt = "# gopi task output — task %s pid %d (find it again: gopi task show %s)\n"

	// taskOutputLogAnnounceFmt is the spawner-side line naming the destination
	// the child's stream was pointed at (see runTaskRunOnce). It is printed on
	// the SPAWNING stream, so an operator (and the launcher's pre-claim record)
	// learns where the run's text went without searching for it.
	taskOutputLogAnnounceFmt = "gopi task: %s output log → %s\n"

	// taskOutputConsoleFmt / taskOutputNoConsoleFmt render the SECOND half of
	// that destination decision — whether the child's stream is ALSO copied to
	// the spawning process's console (epic worker-foreground-console). One line
	// per branch, printed through execRunFacts (the run's own log AND the
	// spawner's stream), so the choice is never silent: an operator reading the
	// record can tell "this run was on a console" from "this run's console copy
	// was suppressed because its spawner is a daemon" without re-deriving it
	// from the environment (Rule 15).
	taskOutputConsoleFmt   = "gopi task: %s: its output is also on this console (the spawning process is not a daemon)\n"
	taskOutputNoConsoleFmt = "gopi task: %s: no console copy — the spawning process is a detached daemon, so its output log above is the record\n"

	// taskConsoleCopyLostFmt records the console face stopping (a closed pipe —
	// `gopi worker | head` — an occluded terminal, an EIO). It is written into
	// the RUN'S OWN LOG, because that is the surviving record (Rule 15: the
	// degradation is recorded where the reader of the record will see it, and
	// the message cannot go to the stream that just failed).
	taskConsoleCopyLostFmt = "gopi task: the console copy of this run's %s stopped: %v (this log keeps the run's text)\n"
)

// execRunFacts is where the spawn seam records what it decided ABOUT the run
// (the executor cwd and its source, the environment strip verdict): the run's
// own output log PLUS the spawning process's stderr, whenever that log exists.
//
// WHY BOTH FACES: these lines describe THIS run, so a copy belongs in THIS
// run's record — a daemon's stderr is a shared stream no task id can reach,
// and the epic's whole point is that "given the id, where is the account of
// the run?" has one answer. But the operator who typed `task run` is watching
// the terminal, and an unannounced silent relocation of the one line that says
// which project the executor chose would be a Rule 15 regression — so the
// stderr face is kept, not replaced.
//
// Announcing the destination itself is the ONE line that stays on the
// spawner's stream alone (cli_tasks.go): the reader who does not know the path
// yet cannot be told through the file it names.
//
// Single-threaded per process: one run per `task run` / worker round, the same
// latch reasoning as workerWriteback.
var execRunFacts io.Writer = os.Stderr

// taskExecutorOutputLogPath is the path the CURRENT run's child streams into
// ("" when the seam has not run). It is the value the terminal writeback
// stamps, so `task show` can name the exact file even for a row whose session
// dir is later pruned. Single-threaded per process — the same package-level
// latch reasoning as workerWriteback.
var taskExecutorOutputLogPath string

// taskChildConsoleWriter returns the writer the executor child's stdout AND
// stderr must ALSO be copied to — the spawning process's console — or nil when
// the spawning process is a DETACHED DAEMON, the one case whose output belongs
// in files alone (epic worker-foreground-console, operator ruling 2026-09-17:
// "when worker is running in frontend, its stdout should be also output to
// console … only daemon output its stdout/stderr to session output files").
//
// WHY A RETURNED WRITER AND NOT A RE-POINT: the file face is not optional — a
// task's text must land in its own <session-dir>/output.log for EVERY path
// (epic child-stream-task-output, the property `task show` and every evidence
// reader depends on). The console face is ADDITIVE, so the two are combined by
// taskChildStreamWriters rather than by choosing one.
//
// WHAT "DAEMON" MEANS HERE — the process's own declared identity, read from the
// marker env the ONE spawn seam writes (daemonChild): a `gopi worker --daemon`
// or `gopi merger --daemon` child is detached, its stdio is already redirected
// into <gopi-dir>/logs/<component>.log by that seam, and a task transcript
// appended there is a shared, per-COMPONENT file that no task id reaches — the
// opposite of the epic's "given the id, where is the account of the run?".
// Every other spawner keeps the console copy, which is what makes `gopi worker`
// in the front, a manual `gopi task run` and a serve tick print the run's text
// as they did before the destination moved to the child.
//
// THE BOUNDARY, honestly: a caller that detaches BY HAND (`setsid nohup gopi
// worker > log 2>&1 &`) carries no marker, so it takes the console branch — and
// its "console" IS the file the caller redirected to. That is exactly the
// pre-child-stream-task-output behaviour (`the child inherited the worker's
// stream`), so nothing is lost or duplicated that was not already; `--daemon`
// is the supported detach door, and it is the one this predicate can prove.
func taskChildConsoleWriter() io.Writer {
	if workerDaemonChild() {
		return nil
	}
	return os.Stdout
}

// taskChildStreamWriters is the ONE place the child's two stream destinations
// are decided (Rule 2), so the seam assigns and a test can read the SAME
// decision without running a claim/run cycle: with a console the child gets a
// writer that fans every chunk out to its own run log AND to the spawner's
// console; without one (a daemon) it keeps the plain file, so os/exec hands it
// the descriptor directly and no copying goroutine is spun up.
//
// The fan-out is consoleCopyWriter ALONE — deliberately NOT an
// io.MultiWriter(runLog, wrapper) wrapping it. The wrapper already owns both
// destinations, so stacking a MultiWriter in front wrote every chunk into the
// run log twice (measured while writing this: the log showed the child's answer
// duplicated) while a console error would have made the MultiWriter return an
// error for a chunk the log had already accepted. One writer, one pass.
//
// EACH STREAM GETS ITS OWN WRITER. os/exec hands a NON-*os.File writer to a
// per-stream copying goroutine, so a single shared instance would be written by
// two goroutines at once — a race on its `lost` flag, and an ordering promise
// between the two streams that pipes never made anyway. Two instances share no
// mutable state: the run log is an *os.File (safe for concurrent use, and
// O_APPEND writes interleave at syscall granularity exactly as the child's own
// two descriptors did before) and the console is os.Stdout (same). The cost is
// that a dead console is noted once PER STREAM, which is the more useful
// record: the note names which stream lost its copy.
//
// The console target is os.Stdout for BOTH streams on purpose: os.Stdout here
// means "the console this gopi process was launched with", which is the
// operator's terminal for a foreground worker, and it is the stream a reader of
// `gopi worker` is looking at. (The child's own stdout/stderr separation is
// preserved inside its run log, where each stream's bytes are what they always
// were — the file is the record, the console is the read-along.)
func taskChildStreamWriters(runLog *os.File) (stdout, stderr io.Writer) {
	console := taskChildConsoleWriter()
	if console == nil {
		return runLog, runLog
	}
	return &consoleCopyWriter{console: console, record: runLog, stream: streamLabelStdout},
		&consoleCopyWriter{console: console, record: runLog, stream: streamLabelStderr}
}

// consoleCopyWriter fans a chunk out to the spawner's console WITHOUT letting a
// console failure reach the child (Rule 15's other half).
//
// io.MultiWriter is all-or-nothing: it stops at the FIRST writer that errors,
// so `gopi worker | head` — the console pipe closing mid-run, the ordinary
// `| head` gesture this codebase already reasons about (see the SIGPIPE notes in
// runTaskRunOnce) — would give the child a torn write and a spurious failed
// run. The console is the ADDITIVE destination and the run log is the load-
// bearing one, so an unserviceable console is REPORTED INTO THE LOG (the
// surviving record) and then dropped, permanently, for the rest of the run: one
// goroutine per stream in os/exec means one failed chunk would otherwise repeat
// the same failure for every later chunk.
type consoleCopyWriter struct {
	console io.Writer
	record  io.Writer
	// stream names WHICH of the child's streams this writer carries, so the
	// loss note says so (`stdout` / `stderr`) — one writer per stream, so the
	// note is unambiguous.
	stream string
	// lost records that the console has been dropped: after it is set the copy
	// stops being attempted, so the log's evidence is already complete. It is
	// touched by that stream's own copying goroutine only (each stream has its
	// own writer — see taskChildStreamWriters).
	lost bool
}

// The child's stream names, used in the loss note. They are the child's own
// descriptors, not gopi's stream nouns, so they are spelled here rather than
// borrowed from a logging constant whose meaning is "a gopi process's stream".
const (
	streamLabelStdout = "stdout"
	streamLabelStderr = "stderr"
)

// Write satisfies io.Writer. It never returns an error: the run log's own
// error is deliberately not propagated to the child either (the file face is
// the seam's business — an unwritable log was already reported when the seam
// opened it — and aborting the child's output mid-run would trade a missing
// copy for a broken run).
func (w *consoleCopyWriter) Write(p []byte) (int, error) {
	// The RECORD goes first, deliberately: the file face is the load-bearing
	// one, so it must not be gated behind a console write that can block on a
	// stopped reader (the same `| head` case, from the other side).
	_, _ = w.record.Write(p) //stylegate:ignore the exec record is a diagnostic side channel; a refused write must not block the console path that is the actual output
	if !w.lost {
		if _, err := w.console.Write(p); err != nil {
			w.lost = true
			fmt.Fprintf(w.record, taskConsoleCopyLostFmt, w.stream, err)
		}
	}
	return len(p), nil
}

// taskExecutorOutputLog opens the row's output log and returns the *os.File
// the child's stdout and stderr must be pointed at.
//
// Rule 15 (no silent failure): an unwritable session directory is RETURNED,
// never swallowed and never replaced by a silent fallback. The caller REPORTS
// it and leaves the child on the spawning stream (the latch stays "" so the
// writeback stamp is omitted) rather than refusing the run: refusing would
// strand a claimed row on a full disk, which trades one lost artifact for a
// stuck queue. What must not happen silently is the LOSS — and it is announced.
//
// APPEND, never truncate: a re-run of a re-claimed row must not erase the
// dead run's evidence. tools.TaskLogPath picks "output.log" for the row's
// first log and "output.<pid>.log" when one already exists, so two runs of
// one row are two files and neither can overwrite the other.
func taskExecutorOutputLog(taskID string) (*os.File, error) {
	path, err := tools.TaskLogPath(taskID, os.Getpid())
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, taskOutputLogFileMode)
	if err != nil {
		return nil, fmt.Errorf("cannot open the task output log %s: %w", path, err)
	}
	// The header marks a FRESH log only; an appended run leaves the previous
	// run's bytes (and its header) untouched.
	if fi, statErr := f.Stat(); statErr == nil && fi.Size() == 0 {
		fmt.Fprintf(f, taskOutputLogHeaderFmt, taskID, os.Getpid(), taskID)
	}
	taskExecutorOutputLogPath = path
	return f, nil
}
