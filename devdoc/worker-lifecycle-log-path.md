<!-- doc-class: historical -->
# Task Report: the host lifecycle record — one declared per-host file for worker/merger start/drain/stop lines

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## The defect (measured, not inferred)

The reader's question on 2026-09-17 was *"what did the workers do last night, and
who stopped them?"*, and it was posed about `~/.gopi/logs/worker.log`. That file
was last written **2026-09-16 11:06** — ~18h BEFORE the night it had to explain:

```
2026-09-16 11:06:58  20022530  /home/user/.gopi/logs/worker.log
2026-09-17 05:55:29   3151736  /home/user/.gopi/logs/merger.log
```

The night's four workers (pids 3066414/3066922/3067418/3074388, started
23:35–23:37) had `fd/1` pointing at `/dev/pts/{1,5,3,4}`: their lifecycle lines
went to a pseudo-terminal whose supervisor then went away, so the night's
drain/termination records existed **nowhere a reader could name**.

Why: `logs/worker.log` was only ever written by ONE face of the command — the
`--daemon` spawn seam (`cmd/agent/cli_daemon_spawn.go`, `child.Stdout = logFile`).
A host that starts workers any other way (the launcher's documented
`setsid nohup ./bin/gopi worker > … 2>&1 &`, an operator's shell) produced no
per-host worker record at all: the lines went wherever the launcher pointed them
(`logs/worker-startup.log`) or nowhere an operator can find.

## Branch context (read this before reading the diff)

The deliverable is the two commits above — `40f41cf5` (the feature) and
`12f6ef9f` (this report). The branch was cut when `master` was at
`a2f519e4`, but **`master` has since moved a long way** (a large deletion
sweep: `devdoc/` pages, `briefings/`, `tasks/` reports, several
`cmd/agent` suites). So `git diff --stat master..HEAD` lists ~90 files and
**~6157 deletions that are NOT this task's doing** — they are master's
newer state being counted as "ours" by a plain two-dot diff. The change
this report describes is exactly:

```
cmd/agent/host_lifecycle_log.go        (new, 227 lines)
cmd/agent/host_lifecycle_log_test.go   (new, 444 lines)
cmd/agent/cli_tasks_worker.go          (call site: worker, line ~318)
cmd/agent/cli_tasks.go                 (call site: merger, line ~3014)
cmd/agent/cli_tasks_worker_stop.go     (stop path reports through the seam)
cmd/agent/merger_graceful.go           (flush before os.Exit)
devdoc/worker-lifecycle-log-path.md    (this report)
```

Read the diff with `git diff a2f519e4..HEAD -- cmd/agent/host_lifecycle_log.go
cmd/agent/cli_tasks_worker.go cmd/agent/cli_tasks.go cmd/agent/merger_graceful.go`
(two-dot to the CUT POINT, not to today's master) to see only this task's
144 new lines in the product files. The ~6157 deletions are master's own
sweep, untouched by this branch; **no merge of this branch into today's
master has been attempted or is claimed.**

## Completed

1. **One seam for the declared record** — `cmd/agent/host_lifecycle_log.go`.
   The path is `daemonLogPathUnder(<base name>)` = `<gopi data root>/logs/<component>.log`,
   i.e. the SAME resolver + the SAME base-name constants the `--daemon` face
   already uses (`workerDaemonLogBaseName`, `mergerDaemonLogBaseName`,
   `daemonLogDirName`; Rules 1/2/13/14 — no new spelling of the directory or of
   either file name). A reader who knows the host can name the file without
   knowing a session id, a task id or a launcher.
2. **The stream is MIRRORED, not moved.** `attachHostLifecycleLog` installs an
   `io.MultiWriter(os.Stdout, file)` for the lifecycle lines; the legacy readers
   keep working byte for byte — the launcher's `logs/worker-startup.log` (it
   counts `procname.WorkerLoopStartLine` banners in it,
   `internal/testutil/glm_night_watch_test.go`), the operator's terminal, and the
   `--once` faces whose stdout is a machine-read contract. It is explicitly NOT
   `os.Stdout = file`: that would put a task's own transcript into a shared
   per-host component file.
3. **The STARTUP LINE names the record** — on the process's own stream, and in
   the file's own head (with the pid), so a reader who opens the file learns what
   they are reading:
   `gopi worker: lifecycle log -> <path> (one declared record per host; start/drain/stop lines append here)`.
4. **The lifecycle call sites reach it** — the worker loop's start banner,
   drain/stop/idle-exit/max-rounds lines
   (`cli_tasks_worker.go` through `hostLifecyclePrint`/`Println`), and the
   merger's arrival note, graceful-stop line and per-repo pass verdicts
   (`cli_tasks.go` through `hostLifecycleErrPrint`/`Print`). The worker's
   per-round account (`workerRoundAnnounce` — the claimed row, the worktree)
   deliberately stays on stdout: that is a task's account, not the component's
   lifecycle record.
5. **Two refusals, each with its reason** (they are not fallbacks):
   - a **daemon child** — its stream IS the declared file (the spawn seam
     redirected it before the fork), so mirroring would double every line and
     change the byte-level content the daemon E2E suites assert;
   - a **`--once` call** — its stdout is the call's result (deploy seams, tests).
   Both still ANNOUNCE the path, because for them it is the file their stream
   already is (daemon child) or the caller's capture (one-shot).
6. **The exit path flushes the record** — `mergerStopped` calls
   `closeHostLifecycleLog()` before `os.Exit` (a `defer` runs on no `os.Exit`
   path): "who stopped it?" is asked precisely about a merger that has just
   exited, so the last lines must not die in the kernel buffer.
7. **Rule 15 (no silent failure)** — an unresolvable/unwritable path is reported
   on stderr naming the consequence (`lifecycle log UNAVAILABLE (…) — start/drain/stop
   lines stay on this stream only`), the component keeps running, and a failed
   CLOSE is reported too.
8. **Tests** — `cmd/agent/host_lifecycle_log_test.go`:
   - path resolution pinned for both components (+ agreement with
     `workerDaemonLogPath`);
   - the mirror + the self-naming head + the startup line on the stream;
   - the unavailable path is reported, not swallowed;
   - the daemon child attaches NOTHING (and creates no file);
   - **the incident's own shape, on the real binary**: `gopi worker` started with
     `SysProcAttr{Setsid, Setctty}` and stdio on a real pty master/slave
     (`/dev/ptmx` + `TIOCGPTN`), asserting ① the child's `fd/1` really IS
     `/dev/pts/N` (measured with `readlink /proc/<pid>/fd/1`, the incident's own
     measurement), ② the startup line naming the record reaches that pty,
     ③ the declared file carries the loop banner, ④ after a SIGTERM the file
     carries BOTH stop verdicts (the signal verdict and the boundary verdict).
     The child is reaped by PID (`cmd.Process.Wait`), never by pattern — the host
     runs the operator's own workers.
9. **Named constants, no new bare literals** — the announce/header/refusal
   formats, the stop-line prefix (`workerStopLinePrefix`), the signal verdict
   phrase (`workerStopInterruptLine`, now built from the constant instead of
   `fmt.Sprintf("%v received", sig)`), and the E2E's budgets/probe names.

## Not Done (honest)

- **The announcement is only on the process's own stream for suppressed cases.**
  For a daemon child the announce line lands in the daemon's log (its stdout IS
  the file) — correct, but it is not a *stderr* notice for an operator watching
  the daemon spawn command's own terminal; that path was already silent about the
  log path before this change (the daemon spawn command's stdout is not the
  child's log), and is unchanged.
- **The worker/merger do not rotate the record.** It is append-only by design;
  `logs/worker.log` is already 20MB after long use and this change makes
  foreground/resident faces write to it too. A rotation policy is NOT implemented
  and is not claimed. (A follow-up task should decide whether the merger/worker
  record needs a size cap; this change deliberately adds no unrequested policy.)
- **No `--daemon` behavioural change was attempted** — its stdout redirect was
  already correct and is left exactly as it was.
- **The full `go test ./...` tree was not run**; the bounded run was the
  `cmd/agent` package (the package the change touches) plus the gate packages.
  See Verification.
- **No real-binary MERGER probe** (the worker probe is the real-binary one). The
  merger's attach is the same seam, pinned in-process by
  `TestHostLifecycleLogPathIsTheDeclaredPerHostFile` and covered by the existing
  merger E2E suites in the package run.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → exit 0.
- **Pins**: `go test ./cmd/agent/ -run 'TestHostLifecycleLog|TestWorkerStartedOnAPty' -count=1` → `ok` (the pty case ~19s).
- **Red proof** (product mutation → red, restore → green): with the worker's
  `attachHostLifecycleLog` call removed, the real-binary pty E2E failed exactly as
  the incident did — *"the worker never named its declared lifecycle record … on
  its own stream"* and the record file was never written; restoring the call →
  `ok`. (The in-process pins and the E2E were both exercised.)
- **Gates**: `go test ./internal/standards/` → `ok`. `go test ./internal/stylegate/`
  → FAIL, **identical to master** (the same pre-existing classes and counts as
  the master checkout: `errorf-verb`, `ignored-error`, `unchecked-close`,
  `test-sleep`, `test-setenv`, `test-skip`); no new site in any file this change
  touches (verified with `-v` and grep over the changed file names).
- **Package suite**: `GOPI_BIN=<tree>/bin/gopi go test ./cmd/agent/ -count=1 -timeout 15m`
  → **6 failures, all reproduced IDENTICALLY on a clean master clone** under the
  same recipe (env-shaped, not this change):
  `TestCLIPositionalSubmitRunsThroughDispatcherE2E`,
  `TestTaskRunChildReceivesSummaryAndPayloadE2E`, `TestTaskRunSingleRowE2E`,
  `TestTaskRunSurvivesClosedStdoutPipe`,
  `TestTaskRunChildNonZeroExitClosesWrapperRowE2E`,
  `TestTaskRunNormalExitSingleRowE2E`; their shared cause is
  `repo "<scratch>/gopi-repo-ctx-N" is not a git work tree (… GIT_DISCOVERY_ACROSS_FILESYSTEM
  not set)`. The worker/merger daemon E2E suites (the regression guard for the
  byte-level `logs/worker.log` / `logs/merger.log` content) pass in that run.
  Harness notes: `GOPI_BIN` must point at a binary built from the SAME tree (else
  ~20 E2E suites fail on a missing `../../bin/gopi`), and the deliverable must be
  COMMITTED before the run — a dirty worktree makes two unrelated suites report
  "uncommitted tracked work" against it.

## Commit

`feat(agent): one declared per-host lifecycle log for worker/merger start/drain/stop lines`
(full hash and files in the session's `git log`; branch `feat/host-lifecycle-log`,
worktree `~/workspace/gopi-lifecycle-log`).
