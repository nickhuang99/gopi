<!-- doc-class: historical -->
# Task Report: the executor child's stream reaches the console again (foreground only)

Epic: **worker-foreground-console** · branch `fix/worker-foreground-console`
· worktree `/home/user/workspace/gopi-worker-console` (based on master `17cec86b`).

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## Completed

- **The console face is back, and it is a TEE — not a re-point.** The spawn
  seam (`cmd/agent/cli_tasks.go`, `runTaskRunOnce`) now assigns
  `cmd.Stdout, cmd.Stderr = taskChildStreamWriters(childOut)`: for a NON-daemon
  spawner the child's stdout **and** stderr are copied to the spawning
  process's own stdout *and* appended to the row's
  `<session-dir>/output.log`; for a detached daemon the child keeps the plain
  file (`cmd.Stdout = cmd.Stderr = childOut`), exactly as before.
- **The routing criterion is the spawner's own declared identity**, read from
  the marker env the ONE daemon spawn seam writes (`daemonChild`):
  `workerDaemonEnv` / `mergerDaemonEnv` ⇒ a detached daemon ⇒ file only.
  Everything else (a foreground `gopi worker`, a manual `gopi task run`, a serve
  tick, an `--once` worker, a test harness) keeps the console copy. New helper
  `taskChildConsoleWriter()` in `cmd/agent/cli_tasks_exec_output.go`.
- **Why a daemon is the one exclusion, stated as a property and not a policy:**
  a `gopi worker --daemon` / `gopi merger --daemon` child already has its stdio
  redirected into `<gopi-dir>/logs/worker.log` / `logs/merger.log` by the shared
  detach mechanism (`cli_daemon_spawn.go` step ②), i.e. into a per-COMPONENT
  file that no task id reaches — the opposite of this epic family's "given the
  id, where is the account of the run?". A foreground worker's stdout is the
  operator's terminal, so the copy lands where the operator is looking.
- **The file face is UNCHANGED and still unconditional.** Every spawner keeps
  `<session-dir>/output.log` (epic child-stream-task-output); the console is
  additive. Both faces are named in the run's own record by the seam's new
  announcement (`taskOutputConsoleFmt` / `taskOutputNoConsoleFmt`, printed
  through `execRunFacts` = the run log + the spawner's stderr), so an operator
  can tell "this run was on a console" from "its console copy was suppressed
  because the spawner is a daemon" without re-deriving it from the environment.
- **The fan-out is a wrapper, not a bare `io.MultiWriter` — and that is
  load-bearing** (`consoleCopyWriter`):
  - `io.MultiWriter` is all-or-nothing, so an EPIPE console (`gopi worker | head`)
    would fail the whole chunk and the RUN LOG would not receive it either — the
    record would be torn by the very gesture that is normal for a CLI;
  - the wrapper writes the **record first**, then the console, so a *blocked*
    console (a reader that stopped, `| head`) cannot stall the log either;
  - a console that does fail is **dropped after one attempt and recorded into
    the run log** (`taskConsoleCopyLostFmt`) — one failure, one note, not a
    per-chunk retry storm (os/exec copies per chunk).
- **Named constants only (Rules 1/1b):** the two routing lines and the loss note
  live in `cli_tasks_exec_output.go`; no literal is typed at a call site; the
  daemon markers and `daemonMarkerValue` are referenced, never re-spelled.

## Not Done (honest)

- **The launcher path (`scripts/gopi-worker-up.sh`) was NOT exercised.** Its
  worker is redirected (`>> "$STARTUP_LOG" 2>&1`, line 480), so a task
  transcript appended to the file it *already* holds is the same byte stream it
  saw before 2026-09-17 — that is why no launcher change was needed — but no
  live launch was driven to demonstrate it. `TestGopiWorkerUpAttributesLogsToSessionDirs`
  is a **heavy** test (`heavytest.CriterionDeviceExec`, skipped by default) and
  was not run.
- **A hand-detached caller (`setsid nohup gopi worker > log 2>&1 &`) takes the
  console branch** and therefore appends the task text to whatever it redirected
  to. That is precisely the pre-2026-09-17 behaviour (the child inherited the
  worker's stream), and it is documented in `taskChildConsoleWriter`'s doc
  comment; `--daemon` is the supported detach door and the one the predicate can
  prove. Making the hand-detached case file-only would require a criterion GoPi
  does not have (there is no reliable "am I detached" signal — setsid is not
  observable from the process itself).
- **`worker_foreground_console_e2e_test.go`'s daemon half relies on two existing
  helpers** (`wdBin`, `wdEnv`, `wdRun`, `wdParseStart`, `wdReap`, `wdProcAlive`
  from `worker_daemon_flag_e2e_test.go`) rather than starting its own sandbox
  daemon plumbing — deliberately, so the daemon shape under test is the one the
  daemon suite already pins.
- **`devdoc/worker-once-vs-task-run-next.md:93`** still describes the retired
  2026-09-12 claim-time re-point ("the round's stdout/stderr is re-pointed into
  `<session-dir>/output.log`"). That sentence was already stale when
  child-stream-task-output landed (it predates it); this change neither
  introduced nor fixed it. Left alone as out of scope, recorded here so the next
  reader does not attribute it to this branch.

## Verification

**Build / vet / fmt**

```
CGO_ENABLED=0 go build ./...                        → OK
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     → OK
go vet ./cmd/agent/                                 → OK
gofmt -l <the four touched/added files>             → (empty)
```

**Gates**

```
go test -count=1 ./internal/standards/   → ok
go test -count=1 ./internal/config/      → ok
```

(No gate exemption, baseline entry or inline escape was added: the new code
carries no `%v`-wrapped error, no `os.Setenv`, no `else`-after-return, no panic,
no prefixed niladic accessor, no unread env constant, and every string is a
named constant.)

**New regression tests**

```
go test -count=1 -run 'TestTaskChildConsoleWriterFollowsTheDaemonMarker|TestTaskChildStreamWritersTeeOnlyWhenAConsoleExists|TestConsoleCopyWriterKeepsTheRunRecordWhenTheConsoleDies' ./cmd/agent/
  → ok  (unit: the routing predicate, the two-destination fan-out through a real
         os.Pipe installed as os.Stdout, the blocked-console control, and the
         dead-console record-keeping)
```

**End-to-end, real binary, hermetic sandbox**

```
GOPI_BIN=$PWD/bin/gopi go test -count=1 -run TestWorkerForegroundPrintsTheTaskOnItsConsole ./cmd/agent/
  → ok   (a real foreground `gopi worker --once` on a sandbox profile: the
          child's model answer reaches the worker's own STDOUT, the seam's
          "also on this console" line is on stderr, and the same text is in
          <home>/.gopi/sessions/<task-id>/output.log)

GOPI_BIN=$PWD/bin/gopi GOPI_TEST_BIN=$PWD/bin/gopi go test -count=1 -run TestDaemonWorkerKeepsTheTaskTextOutOfItsOwnStdio ./cmd/agent/
  → ok   (a real `gopi worker --daemon`: the answer is in the row's output log,
          the run log carries the "no console copy" verdict, and the child's text
          does NOT appear in the daemon's own stdio file logs/worker.log)
```

**MUTATION PROBE (red-before, measured — this is what makes the e2e a guard and
not a coincidence):** replacing the seam's assignment with the pre-fix shape

```go
cmd.Stdout, cmd.Stderr = childOut, childOut // pre-fix (file only)
```

and rebuilding turns `TestWorkerForegroundPrintsTheTaskOnItsConsole` RED with

```
worker_foreground_console_e2e_test.go:184: the executor child's answer must reach the
FOREGROUND worker's console (stdout) — this is the behaviour the epic restores; a reader
of `gopi worker` saw only the round's own lines.
```

The mutation was reverted clean and the rebuilt tree re-tested green (the diff
above shows the production line back in place). The daemon half stays green
under the mutation, which is correct: it asserts the ABSENCE of a console copy,
so it cannot be the thing that proves the console face exists.

**Full package runs** (with the pristine-master worktree
`/home/user/workspace/gopi-before-master` @ `17cec86b` as the baseline, same
command, same host):

```
GOPI_BIN=bin/gopi GOPI_TEST_BIN=bin/gopi go test -count=1 -timeout 1200s ./cmd/agent/
  branch : 17 FAIL / package FAIL     (513.7s)
  master : 17 FAIL / package FAIL     (same set, same names — the diff between the
                                       two failure lists is TIMING SUFFIXES only:
                                       comm on the sorted names returns the same
                                       seventeen tests)
  → ZERO new failures. The 17 are the daemon/cgroup-scope family
    (TestWorkerDaemon_LeavesTheLoginSessionScope, TestMergerDaemon_*,
    Test*_RequireScope*, TestMergerFlock_*, TestMergerSingleton_*), the
    sandbox-repo fixtures that fail because the suite's scratch tree is
    block-backed on this host (TestTaskRun*E2E, "…is not a git work tree"), and
    the two self-referential gate readers (TestVerifyGateLandingCarriesTheDurableCloseLoop:
    "read merger_verify_child.go: no such file"; TestCloseLoopWiringIsComplete).
    My new tests are ABSENT from both lists, i.e. green in the full run.

go test -count=1 -timeout 900s ./internal/tools/
  branch : 5 FAIL / package FAIL
  master : 5 FAIL / package FAIL     (identical five:
                                       TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger,
                                       TestMergeBuildGate_GoCacheLeavesNoScratchCache,
                                       TestSuiteScratchRootIsMemoryBacked,
                                       TestScratchRootFilesystemClassification,
                                       TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy —
                                       all "the suite scratch root must be memory-backed",
                                       a host/environment property this change does not touch;
                                       git diff --name-only HEAD -- internal/ is EMPTY)
```

One extra data point, recorded because it is a real flake and not mine:
`TestTaskFindSessionJSONLStillHonoursEveryLayout` failed in the branch's FIRST
full `internal/tools` run (flat layout resolved `…/<session>.jsonl` where the
fixture expected `…/<session>/session.jsonl`) and passed in isolation on BOTH
the branch and pristine master, then passed in the branch's SECOND full run
(which is why the branch shows 5, matching master). The test calls
`hintFixtureLedger(t)` and the session-tree cache it clears is process-global,
so this is order/state-dependent under a full-package run on this host.
`internal/tools` is not touched by this diff; the observation is handed over
rather than papered over.

**Gates (unchanged by this diff, re-run on the branch)**

```
go test -count=1 ./internal/standards/   → ok
```


## Mechanism summary (for the next reader)

| spawner | child's stdout/stderr |
|---|---|
| `gopi worker` (foreground, incl. `--once`) | row's `output.log` **+** the worker's console (stdout) |
| `gopi worker --daemon` / `gopi merger --daemon` | row's `output.log` only (the daemon's stdio is `logs/<component>.log`) |
| `gopi task run <id>` (manual) | row's `output.log` **+** the operator's terminal |
| serve tick | row's `output.log` **+** the daemon's stdio (unchanged) |
| hand-detached (`setsid nohup … > file`) | row's `output.log` **+** the file the caller chose (the pre-2026-09-17 shape) |

## Commit

On branch `fix/worker-foreground-console` (worktree
`/home/user/workspace/gopi-worker-console`, shared repo — no push needed).

Recovery: `git show fix/worker-foreground-console:devdoc/worker-foreground-console.md`
