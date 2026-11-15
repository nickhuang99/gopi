<!-- doc-class: normative -->
# Task Report: one directory per task — the session workdir + the log re-homing

Branch: `fix/worker-session-cwd` (worktree `/tmp/taskcwd-wt`)
Epic: **task-session-cwd** — every task-serving process must put ALL of a task's
artifacts into `~/.gopi/sessions/<task-id>/`, stand there while it serves the
task, and the legacy flat log areas must be re-homed or deleted.

## Completed

1. **The workdir seam** (`cmd/agent/task_session_cwd.go`, new).
   `taskWorkdirCapture` / `taskWorkdirEnter` / `taskWorkdirRestore` /
   `taskWorkdirChdir` (in `internal/tools/worker_output.go`) /
   `taskExecutorWorkdir` / `taskWorkdirChdirRefuse`.
   - The task-OWNING process (the `task run` wrapper, a worker round) chdirs
     into `sessions/<task-id>/` the moment the id exists, and its `stdout`
     there is the task's `output.log`.
   - The **executor CHILD does not inherit the move**: it is spawned with
     `cmd.Dir = <start workdir>` AND `GOPI_TASK_WORKDIR` exported. The child
     must keep the operator's working context — the agent loop loads the
     project `GOPI.md` from cwd, the search tools index cwd, `task_submit`
     stamps `deliverable.repo` from cwd, and bash's dangerous-command
     whitelist resolves relative targets against cwd.
   - A container that cannot be entered is a **loud refusal** (the worker
     reverts the claim and does not run; the standalone wrapper exits
     non-zero) — never a silent fallback to running with the output outside
     the task's own directory.
   - `resume` re-points the workdir latch when it chdirs into the row's
     worktree, so a descendant is not handed the launch cwd as its context.
2. **`internal/tools` — one root, two faces (Rule 2).** `SessionsRoot` /
   `TaskSessionDirUnder` / `ChdirTaskSessionDir` / `TaskWorkdirEnv` /
   `TaskStartWorkdir` added; `TaskLogDir` removed so the cwd and the output
   log resolve against the SAME root (`--worker-log-root` /
   `GOPI_WORKER_LOG_ROOT` still override it).
3. **The front-run console log** (`cmd/agent/session_console_log.go`, new) —
   the epic's explicit "even cwd is impossible to change" clause.
   `gopi "prompt"` runs in the operator's directory (moving it would change
   what the run MEANS), so its console stream is **copied** into
   `<session-dir>/console.log` via an `os.Pipe` fan-out while still reaching
   the operator's stdout. No `dup2`, no new dependency. Two decided
   refusals — an interactive TTY (the terminal IS the record) and a
   `task run` child (its stdout already IS the task's `output.log`) — and a
   loud stderr line when session logging is off.
4. **Legacy log cleanup** — `scripts/rehome-task-logs.sh` (dry-run default)
   + `internal/testutil/rehome_task_logs_test.go`. Attribution is decided
   FROM FILE CONTENT: 0 claims → delete, 1 claim → whole-file append as
   `output.legacy-<stamp>.log`, N claims → split at claim boundaries.
   `merger.log` / `worker-startup.log` / `watchdogs` are never touched.
   Applied to the real profile: 6 whole + 11 split files (28,686,833 B) into
   69 task dirs, 3 idle worker logs + 71 retired logger files deleted
   (503,679 B). Post-condition checked: **0 mis-attributions**; re-running is
   a no-op.
5. **Tests** (`cmd/agent/task_session_cwd_test.go`,
   `cmd/agent/session_console_log_test.go`,
   `cmd/agent/task_session_cwd_e2e_test.go`) — unit + real-binary e2e.

## Not Done

- **The 818 unreferenced flat raw logs (~4.2 GB, `~/.gopi/raw/gopi-raw-*.log`)**
  are NOT deleted. They are outside this epic's named scope (`~/.gopi/logs`),
  and I could not PROVE they are useless: they are the raw HTTP record, and
  the `stats` cost/byte accounting is derived from raw logs. A sample scan
  showed most open with a system prompt and contain an id, but **57 of 60**
  could not be tied to a single session directory — the deletion criterion the
  epic sets ("if you cannot determine the owner, delete") is met on the
  letter, but at 4.2 GB of ledger-adjacent data I recorded the finding rather
  than acting unilaterally. **Recommend a follow-up task with the operator's
  explicit ruling.** (Evidence: 891 `rawLogPath` refs found across all session
  JSONLs; 608 of the 610 flat refs still exist on disk; the remaining 818 flat
  files are referenced by no session.)
- The worker's per-round **stderr** still goes to the launcher's stderr rather
  than a file inside the session dir (its stdout/stderr ARE re-pointed into
  `output.log` for a managed round; an operator-run foreground worker's
  stderr is the operator's terminal).

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK;
  `gofmt -l cmd internal scripts` → empty; `go vet ./cmd/agent/` → clean.
- **Tests:** unit + e2e in this report's evidence section; the package and the
  whole suite were run serially (see the pitfall below).
- **RED-PROOFS captured (each restores green afterwards):**
  1. Disabling the child's `cmd.Dir` → the child's `pwd` lands in the session
     dir and `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`
     FAILS.
  2. Removing the console-log attach → `TestFrontRunConsoleOutputLandsInItsSessionDir`
     fails with "no console.log in any session dir".
  3. Restoring the old restore-target behaviour → `TestTaskWorkdirRestoreSkipsAGoneRoundOrigin`
     fails with "the process instead ended in .../sessions/t-session-cwd-gone-origin".
  4. Slicing a re-homed log to EOF → mis-attribution failure in
     `internal/testutil`.

## The two bugs this work found (and the pitfall behind them)

Both were found by **running the whole package**, not the new tests alone.

1. **A stale, process-wide restore target stranded the process.** The restore
   originally returned to the once-per-process `taskRunWorkdir` latch — the
   first capture, which in a test binary belongs to an EARLIER test's
   `t.TempDir()`. When that directory was reclaimed, `os.Chdir` still
   SUCCEEDED (the kernel keeps a deleted directory alive for the process) and
   every later operation in that process failed with
   `getwd: no such file or directory` — **24 following tests**, all of which
   read fixtures through a relative path. Fix: the restore target is
   PER-ROUND (`taskWorkdirRoundOrigin`, set at enter, cleared at restore), and
   it is validated with `liveWorkdir()` before the process moves — a missing
   origin is reported and the process stays put.
2. **`resumeChdirToWorktree` leaves the override pointing at the row's
   worktree**, which its owner then releases — the same stale-directory
   hazard, now guarded by the same liveness check.

**Pitfall worth carrying forward:** the failures looked like "missing fixture"
/ "no such file or directory", which invites a search for a deleted file. The
real cause was a PROCESS whose cwd had been removed — a `pwd`-style check
(`os.Getwd()` in a failing test) identifies it in one line where a fixture
hunt takes many rounds. Also: **never run two Go suites concurrently in this
repo** — they share `/dev/shm` scratch and its GC, which deletes a peer's temp
dirs mid-run and fabricates exactly this class of failure.

## A cross-agent incident (recorded, recovered)

While bisecting I ran `git stash pop` in my worktree and it applied **another
agent's** stash (`worker-self-release`): GoPi's linked worktrees share ONE
stash list. I restored my worktree to my branch HEAD, verified the other
worktree still contained its files byte-for-byte (it did — the stash was only
a snapshot), and re-applied my own stash by inspecting
`git stash show --include-untracked --name-only` first. Lesson stored to
memory (#2159): in a shared repo, shelve work with a WIP commit on your own
branch, never `git stash`, and never `pop` without checking what the stash
contains.

## Files

- new: `cmd/agent/task_session_cwd.go`, `cmd/agent/session_console_log.go`,
  `cmd/agent/task_session_cwd_test.go`, `cmd/agent/session_console_log_test.go`,
  `cmd/agent/task_session_cwd_e2e_test.go`,
  `internal/testutil/rehome_task_logs_test.go`, `scripts/rehome-task-logs.sh`,
  `devdoc/task-session-cwd.md`
- modified: `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go`,
  `cmd/agent/cli_resume.go`, `cmd/agent/main.go`,
  `internal/tools/worker_output.go`
