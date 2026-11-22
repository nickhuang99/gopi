package tools

// worker_output.go — the WORKER OUTPUT-LOG PATH CONTRACT (Rule 1/13/14: every
// directory and file name below is a named constant, and the session-dir root
// is the SAME single source the session logger uses).
//
// THE PRINCIPLE (operator, 2026-09-12): everything a worker produces is
// ATTRIBUTED TO THE SESSION it serves. A worker's stdout/stderr therefore
// lives inside that task's session directory — the SAME directory the ledger
// resolves for the row — never in a flat public area named by timestamp.
//
// WHY (this is the whole point, not tidiness): the workflow depends on
// "find a task's output BY ITS TASK ID". A failed task is never re-run; a new
// task is submitted with the old id as a reference, and the next agent walks
// from that id to every byte the old run produced. `~/.gopi/logs/worker-
// <ts>-<i>.log` broke that chain: the name carries no task id, and the
// timestamps overlap between concurrent workers (the 04:12 incident killed 6
// workers inside one ~70 s window — indistinguishable by timestamp).
//
// ACCEPTANCE (machine-checkable, the epic's core test): hand a task id to a
// later agent with NO other record — it must reach every output. The surfaces
// are keyed by the id:
//
//	`gopi task show <id>`
//	  session_log:  <gopi-dir>/sessions/<session-id>/<run-uuid>.jsonl
//	  raw_log:      <gopi-dir>/sessions/<session-id>/<run-uuid>.log
//	  task_output:  <gopi-dir>/sessions/<task-id>/output.log   (this file)
//
// The third line is TaskLogPath below. All three are keyed on the ROW (as
// `task show` renders them), so the surfaces can never disagree about where a
// task's artifacts live.
//
// THE ROUTE (design answer #1 — "does the worker know its session id at
// launch?"): NO, and it no longer has to. The launcher spawns a worker for the
// QUEUE, not for a row — the id exists only after the worker's atomic claim
// (workerRoundClaiming ② in cmd/agent/cli_tasks_worker.go). The destination is
// therefore owned by the party that DOES know the id: the spawn seam of
// runTaskRunOnce points the EXECUTOR CHILD's stdout/stderr at this path
// (cmd/agent/cli_tasks_exec_output.go), and the path is derived from the row's
// TASK ID alone. There is no root to configure and no claim-time re-point: the
// same seam serves `task run`, a worker round and a serve tick identically.
// The worker launcher keeps only its own pre-claim record
// (<gopi-dir>/logs/worker-startup.log).
//
// CONCURRENCY: two runs may serve ONE task id in sequence (the first died
// mid-run; the row was reclaimed). A single fixed name would let run 2
// truncate run 1's evidence, so run 1 owns the fixed name and every later run
// appends its pid — "output.<pid>.log" — making the pair distinguishable while
// "list this task's outputs" stays one glob.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// TaskWorkdirEnv names the directory a task-serving process STARTED in —
	// captured by main before anything moves the process and exported for its
	// descendants. The `task run` wrapper is the reader that matters: its
	// EXECUTOR child must run in the operator's working context even though
	// the wrapper has itself moved into the task's session directory (see
	// cmd/agent/task_session_cwd.go — the child resolves GOPI.md by walking up
	// from cwd, the
	// search tools index cwd, task_submit stamps deliverable.repo from cwd,
	// and bash's dangerous-command whitelist resolves relative targets
	// against cwd). Rule 2: one writer (main / the resume path), one reader
	// (TaskStartWorkdir, via taskExecutorWorkdir). Empty = the caller never
	// moved, so cwd already IS the workdir.
	TaskWorkdirEnv = "GOPI_TASK_WORKDIR"

	// WorkerLogFileName is the output-log name of a task's FIRST run in its
	// session dir. Fixed (never timestamped) on purpose: "given the task id,
	// where is its output?" must have ONE answer, not a directory scan. A
	// run after the first appends its pid (workerLogRunFilePrefix) so a
	// reclaimed row can never overwrite the dead run's evidence.
	WorkerLogFileName = "output.log"

	// workerLogRunFilePrefix / workerLogFileSuffix build the later-run name
	// "output.<pid>.log" — and the glob "output*.log" that matches BOTH
	// shapes, i.e. "every output this task produced" in one pattern.
	workerLogRunFilePrefix = "output."
	workerLogFileSuffix    = ".log"

	// workerLogGlobPattern matches EVERY shape at once ("output.log" AND
	// "output.<pid>.log"). It is DERIVED from the two constants above so the
	// pattern can never drift away from the names actually written.
	workerLogGlobPattern = workerLogFilePrefixStem + "*" + workerLogFileSuffix

	// workerLogFilePrefixStem is the name stem the glob hangs off — the run
	// prefix without its dot ("output"), so "output.log" matches too.
	workerLogFilePrefixStem = "output"

	// workerLogDirMode is the permission bits of a created session dir
	// (mirrors the session logger's own MkdirAll mode).
	workerLogDirMode = 0o755
)

// WorkerOutputLogSummaryLine is the ledger writeback tag stamped on a run
// that opened an executor output log — it makes "which task ids have the new
// attribution" a grep over `task show`, no filesystem scan.
const WorkerOutputLogSummaryLine = "worker-output-log: "

// TaskSessionDirUnder returns the path of the task's session container and
// CREATES it (MkdirAll), so a caller can both resolve and ENTER it; a creation
// failure is returned, never swallowed (Rule 15: a task whose container cannot
// be created is a task whose output nobody can find).
//
// The path itself is taskSessionDir's (task_session.go) — the ONE resolution of
// <gopi-dir>/sessions/<task-id> that the task's report, attachments and legacy
// migration already share (Rule 2/14: a second join here would be a second
// spelling of the tree, free to drift). This function adds only the creation
// the resolve-and-enter caller needs; taskSessionDir deliberately does not
// create.
func TaskSessionDirUnder(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task session dir: task id must not be empty")
	}
	dir, err := taskSessionDir(taskID)
	if err != nil {
		return "", err
	}
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, workerLogDirMode); err != nil {
		return "", fmt.Errorf("task session dir: cannot create %s: %w", dir, err)
	}
	return dir, nil
}

// ChdirTaskSessionDir moves the CALLING PROCESS into taskID's session
// container and returns the directory it now stands in — the chdir half of the
// epic's "the task's process stands in the task's directory" contract.
// Idempotent: a process already inside the container is not moved again.
//
// ONLY a task-OWNING process (the `task run` wrapper, a worker round) may
// call this, and both of those die with the run they own. A process that
// OUTLIVES its task — the executor child, a daemon, the merger — must not:
// it is handed the start workdir instead (TaskWorkdirEnv / TaskStartWorkdir).
func ChdirTaskSessionDir(taskID string) (string, error) {
	dir, err := TaskSessionDirUnder(taskID)
	if err != nil {
		return "", err
	}
	if wd, wdErr := os.Getwd(); wdErr == nil && wd == dir {
		return dir, nil // already there — idempotent, nothing to move
	}
	if err := os.Chdir(dir); err != nil {
		return "", fmt.Errorf("chdir %s: %w", dir, err)
	}
	return dir, nil
}

// TaskStartWorkdir returns the workdir the calling task-serving process
// STARTED in (TaskWorkdirEnv), or "" when the caller never moved out of it —
// cwd is then the answer (see taskExecutorWorkdir in cmd/agent).
func TaskStartWorkdir() string {
	return strings.TrimSpace(os.Getenv(TaskWorkdirEnv))
}

// TaskLogPath returns the path of a task's worker output log, creating the
// session dir first. A failure (no gopi root, unwritable profile) is
// RETURNED, never swallowed (Rule 15: no silent failure — the caller must
// surface it rather than let the run continue with its output nowhere).
//
// THE KEY IS THE TASK ID, never the session id, for two measured reasons:
//
//  1. STABILITY. The row's session_id is EMPTY when the worker claims it and
//     is backfilled a moment later, when the child session appears
//     (TaskBackfillSession). A directory keyed on it would therefore MOVE
//     mid-run: the worker would open output.log under one directory and the
//     terminal `task show` would look under another (measured on the e2e
//     mock-LLM path, where the backfill does land).
//  2. DERIVABILITY BY THE CALLER. The whole requirement is "hand over the
//     task id and reach the output" — so the id must be the key. The
//     session_id needs a second record (the ledger) to learn, which is
//     exactly what the epic forbids.
//
// The session_id is deliberately NOT a parameter and NOT part of the path —
// the same choice `task show` makes for session_log/raw_log, which resolve
// THROUGH the id.
//
// pid selects the name: the fixed WorkerLogFileName when the directory holds
// no output log yet (this task's first run), else "output.<pid>.log" so a
// re-claimed row cannot truncate the dead run's evidence.
func TaskLogPath(taskID string, pid int) (string, error) {
	dir, err := TaskSessionDirUnder(taskID)
	if err != nil {
		return "", err
	}
	first := filepath.Join(dir, WorkerLogFileName)
	if _, err := os.Stat(first); os.IsNotExist(err) {
		return first, nil
	}
	return filepath.Join(dir, workerLogRunFilePrefix+strconv.Itoa(pid)+workerLogFileSuffix), nil
}

// TaskLogGlob returns the glob matching EVERY output log of one task dir —
// the "list this task's outputs" one-liner, stable for the first run and for
// any re-run (workerLogFilePrefix covers both shapes). It creates nothing:
// the glob is a resolution, and a caller asking "what is there" must not
// bring a directory into existence.
func TaskLogGlob(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task log glob: task id must not be empty")
	}
	dir, err := taskSessionDir(taskID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, workerLogGlobPattern), nil
}

// TaskShowOutputLogLabel is the render label of the worker output-log line in
// `task show`, INCLUDING its column padding so the label column aligns with
// session_log/raw_log above it (Rule 14: exported so a test can assert the
// SAME string the renderer prints instead of mirroring the literal — and so
// the alignment lives in one place rather than in the format string).
const TaskShowOutputLogLabel = "task_output:    "

// taskWorkerOutputLogLine renders the `task_output:` line for `task show`,
// or "" when the task has no run output log (a row that was never executed —
// nothing to point at, and an empty marker would be noise). A recorded-but-
// missing file renders the path with an explicit "(not found)" marker, exactly
// like session_log/raw_log above it: the absence must be visible, never
// silent.
//
// It takes the TASK ID alone, like the resolver it wraps: the log is found
// through the id (epic child-stream-task-output), so a caller that has the row
// never has to know a session uuid to reach the run's output.
func taskWorkerOutputLogLine(taskID string) string {
	glob, err := TaskLogGlob(taskID)
	if err != nil {
		return ""
	}
	paths, err := filepath.Glob(glob)
	if err != nil || len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		if _, statErr := os.Stat(p); statErr != nil {
			fmt.Fprintf(&b, "%s%s (not found — output log pruned)\n", TaskShowOutputLogLabel, p)
			continue
		}
		fmt.Fprintf(&b, "%s%s\n", TaskShowOutputLogLabel, p)
	}
	return b.String()
}
