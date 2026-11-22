package main

// host_lifecycle_log.go — the ONE declared, per-host, append-only record of a
// resident component's LIFECYCLE lines (started / draining / stopped /
// exiting).
//
// WHY THIS FILE EXISTS (the measured defect, 2026-09-16/17): the reader's
// question — "what did the workers do last night, and who stopped them?" — was
// posed about `~/.gopi/logs/worker.log`, and that file was last written ~18h
// BEFORE the night's run. Four workers had been started in the shape the
// launcher documents
//
//	setsid nohup ./bin/gopi worker > <somebody's file> 2>&1 &
//
// i.e. every lifecycle line a worker prints went to the stream SOMEONE ELSE
// chose — the launcher's flat `logs/worker-startup.log`, an operator's
// terminal redirect, or, in the shape that was actually measured, a
// pseudo-terminal (`readlink /proc/<pid>/fd/1` → `/dev/pts/1`): when that pty's
// supervisor went away, the night's drain/termination records existed nowhere
// a reader could name. `logs/worker.log` was only ever written by the
// `--daemon` face (cli_daemon_spawn.go step ②, child.Stdout = logFile), so a
// host that starts its workers any other way has NO per-host worker record at
// all.
//
// THE CONTRACT THIS FILE ESTABLISHES: a component's lifecycle lines go to ONE
// declared file per host — `<gopi data root>/logs/<component>.log` — resolved
// through the SAME seam the daemon face already uses (daemonLogPathUnder +
// workerDaemonLogBaseName / mergerLogBaseName; Rules 1/2/13/14: one
// definition of the directory and of each file name), and the path is NAMED on
// the stream at startup so a reader never has to know a session id, a task id
// or a launcher to find it.
//
// THE STREAM IS MIRRORED, NOT MOVED, and that is deliberate. The legacy readers
// of these very lines keep working, byte for byte:
//
//   - the launcher's startup log (`scripts/gopi-worker-up.sh` appends the
//     worker's stdout to `<gopi-dir>/logs/worker-startup.log` and counts
//     WorkerLoopStartLine banners in it — internal/testutil/glm_night_watch_test.go);
//   - the operator running `gopi worker` in the front, who must keep seeing the
//     loop's lines on their terminal;
//   - the `--once` face, whose stdout IS a machine-read contract (deploy seams,
//     E2E suites), which is also why `--once` is excluded from the attach.
//
// The alternative — re-pointing os.Stdout at the file, the way the daemon spawn
// seam redirects its child — is explicitly NOT taken: everything this process
// prints would then land in the per-host file, and the epic's boundary is that
// a component's lifecycle record is NOT a task's account (a task's text belongs
// in <gopi-dir>/sessions/<task-id>/output.log, where a task id reaches it).
// The mirror carries only the lines the lifecycle call sites print through
// hostLifecyclePrint / hostLifecyclePrintln / hostLifecycleErrPrint.
//
// IT IS ALWAYS SAFE TO APPEND (the constraint that decided this shape, not a
// nicety): `gopi worker --daemon` and `gopi merger --daemon` already point the
// daemon's whole stdio at this very file, so for a detached child the mirror
// would write every line TWICE. The attach is therefore REFUSED for a daemon
// child (workerDaemonChild() — the merger has no daemon face any more): its
// stream IS the declared
// file already, and the byte-for-byte content of `logs/worker.log` /
// `logs/merger.log` that the existing worker/merger E2E suites assert (`no
// console copy`; the child's text must be ABSENT) is left untouched.
//
// RULE 15 (no silent failure): an unresolvable or unwritable lifecycle-log path
// is REPORTED on stderr naming the reason, and the lifecycle lines keep going to
// the stream — never a silent fallback, never a swallowed error, and never a
// refusal that would stop a worker from doing its job.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// hostLifecycleLogAnnounceFmt is the STARTUP LINE that NAMES the declared
	// record. It is written to the process's own stream (and, once the file is
	// open, into the file's head as well), because the reader who does not yet
	// know the path cannot be told about it through the file it describes.
	hostLifecycleLogAnnounceFmt = "%s: lifecycle log -> %s (one declared record per host; start/drain/stop lines append here)\n"
	// hostLifecycleLogHeaderFmt is the file's OWN head — the same fact, written
	// where a reader of the FILE finds it without any stream. It carries the
	// path and the pid so an appended file stays readable line by line when
	// several launches have shared it.
	hostLifecycleLogHeaderFmt = "%s: lifecycle log %s (pid %d, appended)\n"
	// hostLifecycleLogUnavailableFmt reports an attach that could NOT be made.
	// It names the failure and states the consequence (Rule 15: the loss of the
	// record is announced, never silent) — the component keeps running on its
	// own stream.
	hostLifecycleLogUnavailableFmt = "%s: lifecycle log UNAVAILABLE (%v) — start/drain/stop lines stay on this stream only\n"
	// hostLifecycleLogCloseFailFmt reports a close that failed after the record
	// was written (a full disk at the last flush is the case that matters).
	hostLifecycleLogCloseFailFmt = "%s: closing the lifecycle log %s: %v\n"
)

// The process-wide attachment state — ONE per process, like every other gopi
// stream attachment (see session_console_log.go's consoleLogCurrent for the
// precedent): a component attaches once, at its loop's start.
var (
	// hostLifecycleLogFile is the open declared record (nil = not attached).
	hostLifecycleLogFile *os.File
	// hostLifecycleLogPath is the path it was opened at (for the close report).
	hostLifecycleLogPath string
	// hostLifecycleLogProg is the component prefix the close report carries.
	hostLifecycleLogProg string
)

// hostLifecycleOut / hostLifecycleErrOut are what the lifecycle call sites print
// through. They are FUNCTIONS, not cached writers, for a measured reason: the
// process's own os.Stdout is re-pointed while gopi runs (session_console_log.go
// fans a front run's console into its session dir), and a writer captured at
// attach time would keep writing to the stream as it was BEFORE the re-point —
// a lifecycle line about to be lost to a stream nobody reads again. Reading
// os.Stdout at print time follows the process's actual stream, which is the
// whole point of mirroring rather than moving it.
func hostLifecycleOut() io.Writer {
	if f := hostLifecycleLogFile; f != nil {
		return io.MultiWriter(os.Stdout, f)
	}
	return os.Stdout
}

// hostLifecycleErrOut is hostLifecycleOut's stderr twin.
func hostLifecycleErrOut() io.Writer {
	if f := hostLifecycleLogFile; f != nil {
		return io.MultiWriter(os.Stderr, f)
	}
	return os.Stderr
}

// hostLifecycleLogAttached reports whether this process ALREADY has its declared
// lifecycle record open.
//
// IT EXISTS FOR THE RESIDENT LOOP (epic merger-self-driving, 2026-09-18). The
// attach belongs to the LONGEST-LIVED face a process has: for `gopi merger
// loop that is the LOOP, not one round — a per-round attach would close the
// file when the round returned, so the lines the loop prints between rounds
// (the boundary and the sleep edge, which the retired merger `--status` read
// a live loop's schedule from) would never reach the declared
// record. The cron form has no loop, so there the tick IS the longest-lived
// face and attaches itself. This predicate is how one tick knows which case it
// is in without a second parameter threaded through every call site.
func hostLifecycleLogAttached() bool { return hostLifecycleLogFile != nil }

// attachHostLifecycleLog installs this process's lifecycle record: lifecycle
// lines printed through hostLifecyclePrint / hostLifecyclePrintln (stdout) and
// hostLifecycleErrPrint (stderr) then reach the declared file as well, and the
// STARTUP LINE names it. The caller MUST arrange closeHostLifecycleLog on every
// exit path (a defer, plus an explicit call before an os.Exit path).
//
// baseName is the component's file name (workerDaemonLogBaseName /
// mergerLogBaseName); prog is the prefix every line this component prints
// already carries, so the announce/refusal lines are recognisable as this
// component's; oneShot marks the faces whose stdout is a machine-read contract
// (`gopi worker --once`, `gopi merger --once`).
//
// The suppressed cases leave the writers at the plain streams — and they still
// ANNOUNCE, because the file the reader is being sent to is the one this
// process's stream ALREADY is (a daemon child) or the caller's own capture (a
// one-shot call). Saying so is the difference between a declared record and a
// guess.
func attachHostLifecycleLog(prog, baseName string, oneShot bool) {
	if workerDaemonChild() {
		fmt.Fprintf(os.Stdout, hostLifecycleLogAnnounceFmt, prog, hostLifecycleLogPathOrUnresolved(baseName))
		return
	}
	if oneShot {
		fmt.Fprintf(os.Stdout, hostLifecycleLogAnnounceFmt, prog, hostLifecycleLogPathOrUnresolved(baseName))
		return
	}
	path, err := daemonLogPathUnder(baseName)
	if err != nil {
		fmt.Fprintf(os.Stderr, hostLifecycleLogUnavailableFmt, prog, err)
		return
	}
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(filepath.Dir(path), daemonLogDirMode); err != nil {
		fmt.Fprintf(os.Stderr, hostLifecycleLogUnavailableFmt, prog, err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, daemonLogFileMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, hostLifecycleLogUnavailableFmt, prog, err)
		return
	}
	// The file's own head first (so the file explains itself), then the pointer
	// on the stream — the reader watching NOW learns the path BEFORE the loop
	// starts printing its rounds.
	fmt.Fprintf(f, hostLifecycleLogHeaderFmt, prog, path, os.Getpid())
	fmt.Fprintf(os.Stdout, hostLifecycleLogAnnounceFmt, prog, path)
	hostLifecycleLogFile = f
	hostLifecycleLogPath = path
	hostLifecycleLogProg = prog
}

// closeHostLifecycleLog releases the declared record. Idempotent (a deferred
// call after an explicit one is a no-op) so an os.Exit path can close it
// explicitly without changing what the defer does. A close FAILURE is reported
// (Rule 15b: a disk that filled up at the last flush is exactly what the reader
// must hear about).
func closeHostLifecycleLog() {
	f := hostLifecycleLogFile
	if f == nil {
		return
	}
	hostLifecycleLogFile = nil
	path, prog := hostLifecycleLogPath, hostLifecycleLogProg
	hostLifecycleLogPath, hostLifecycleLogProg = "", ""
	if err := f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, hostLifecycleLogCloseFailFmt, prog, path, err)
	}
}

// hostLifecyclePrint renders one lifecycle line through the attached record —
// the ONE way a start/drain/stop line reaches both the file and the stream.
func hostLifecyclePrint(format string, args ...interface{}) {
	fmt.Fprintf(hostLifecycleOut(), format, args...)
}

// hostLifecyclePrintln is the line-shaped sibling of hostLifecyclePrint.
func hostLifecyclePrintln(args ...interface{}) {
	fmt.Fprintln(hostLifecycleOut(), args...)
}

// hostLifecycleErrPrint renders a lifecycle line whose STREAM is stderr (the
// merger's arrival note and its graceful-stop line keep their stream) while the
// declared file gets the same line.
func hostLifecycleErrPrint(format string, args ...interface{}) {
	fmt.Fprintf(hostLifecycleErrOut(), format, args...)
}

// hostLifecycleLogPathOrUnresolved resolves the declared path for the ANNOUNCE
// line, degrading to a NAMED answer — never to an empty string, which would
// render the reader's pointer as a path that looks like "" (Rule 15).
func hostLifecycleLogPathOrUnresolved(baseName string) string {
	path, err := daemonLogPathUnder(baseName)
	if err != nil {
		return fmt.Sprintf("(unresolved: %v)", err)
	}
	return path
}
