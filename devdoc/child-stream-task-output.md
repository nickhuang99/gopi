<!-- doc-class: normative -->
# Task Report: the executor child's stdout/stderr goes to its own row's session directory

Epic: **child-stream-task-output** · branch `feat/child-stream-task-output`
· worktree `/home/user/workspace/gopi-child-output` (based on master `0dac2ae7`).

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## Superseded in part (2026-09-17, epic `worker-foreground-console`)

The ONE thing this change got wrong (operator report, same day): the child's
stream destination moved to the file and **nothing kept the console face**, so a
`gopi worker` running in the front printed nothing of the task it was executing —
before this change the child INHERITED the worker's stdout, which is the behaviour
the operator expects as the software-development convention ("only a daemon
outputs its stdout/stderr to session output files").

`devdoc/worker-foreground-console.md` restores it as a TEE at the same seam:
every spawner that is **not** a detached daemon (`workerDaemonChild()` /
`mergerDaemonChild()`, both read from the marker env this repo's daemon spawn
writes) gets the child's stdout+stderr copied to its own stdout **in addition
to** the row's `output.log`; a `--daemon` spawner keeps the file alone, because
its own stdio is already `<gopi-dir>/logs/<component>.log` — a per-component
file no task id reaches. The file face described below is unchanged and still
unconditional. Everything else in this document stands.


## Completed


- **The destination moved from the SPAWNER to the CHILD, at the one seam every
  path shares.** `runTaskRunOnce` (`cmd/agent/cli_tasks.go`) now opens
  `<gopi-dir>/sessions/<task-id>/output.log` and hands that file to the child as
  **both** stdout and stderr — new file `cmd/agent/cli_tasks_exec_output.go`
  (`taskExecutorOutputLog`, `execRunFacts`, `taskExecutorOutputLogPath`). A
  manual `gopi task run`, a worker round and a serve tick now produce the SAME
  file for the same row; before, each inherited its spawner's stream.
- **The path is the resolver `task show` already globs** (`tools.TaskLogPath` /
  `TaskLogGlob`) — one spelling, keyed on the **task id** (the child's session
  id does not exist at spawn time). No new root, no new env knob, no second
  path spelling (Rules 2/13/14).
- **Re-run safety**: `TaskLogPath` gives a row's first run the fixed
  `output.log` and every later run `output.<pid>.log`, opened `O_APPEND` — a
  re-claimed row can never truncate its dead predecessor's evidence, and one
  glob still lists both (test: `TestExecutorOutputLogAppendsOnReRun`).
- **The destination is announced** on the spawner's stream
  (`gopi task: <id> output log → <path>`), and the seam's own verdicts about the
  run (executor cwd + its source, env-strip) go to the run log **and** the
  spawner's stderr via `io.MultiWriter`.
- **Rule 15 on an unusable destination**: the error is returned and reported, the
  child keeps the spawning stream, the writeback stamp is omitted, and the run is
  NOT refused (refusing would strand a claimed row on a full disk).
- **The parent-side machinery is DELETED, not left dead**: the whole
  `workerAttachOutputLog`/`workerDetachOutputLog`/`workerCloseOutputLog`/
  `workerRoundOut`/`workerOutputLogPathOf` state machine (file
  `cmd/agent/cli_tasks_worker_output.go` removed, with its two test files), the
  claim-time `os.Stdout`/`os.Stderr` re-point, and the
  `RevertClaimUnattributed` rollback that existed only for "output not
  attributed".
- **Both faces of the retired knob are gone**: `tools.WorkerLogRootFlag` /
  `WorkerLogRootEnv`, `procname.EnvWorkerLogRoot`, the `cmd/agent/main.go` injection, the
  arg scanner's value-flag arm, the daemon re-exec's flag list, and the
  launcher's `--output-root` + `GOPI_WORKER_LOG_ROOT` export + its hard `mkdir`
  precondition. The retired spelling survives as exactly ONE constant per
  language (`workerLogRootRetiredFlag`, `RETIRED_OUTPUT_OPTION`) so the refusal
  can NAME it with its replacement — no code path reads it as an option.
- **The worker reap's IDENTITY was re-keyed** (`internal/testutil`): it used to
  find "this profile's workers" by the `--worker-log-root <sandbox>` argv pair —
  the flag being retired — so it now reads each candidate's own
  `/proc/<pid>/environ` through the new `config.GopiDirFromEnviron` and compares
  the resolved gopi dir. The environment is the sturdier identity: it says what
  the process IS, not what its launcher typed.
- **Signature simplifications the change forced** (each removing a spelling that
  could drift): `tools.SessionsRoot()` lost its root argument (the retired knob
  was its only source), `TaskLogPath(id, pid)`, `TaskLogGlob(id)` and
  `taskWorkerOutputLogLine(id)` lost their `session_id` parameters, and
  `TaskSessionDirUnder` now delegates its join to `taskSessionDir`
  (task_session.go) — the one resolution the report, attachments and legacy
  migration already share.
- **Retired-option refusal at BOTH faces** (`gopi worker --worker-log-root …`
  and `scripts/gopi-worker-up.sh … --worker-log-root …`): exit 2, naming the
  retired spelling, the replacement, and (on the CLI) the stale-binary cause.
- **Scripts swept**: `scripts/acceptance-self-release{,-stop}.sh` no longer pass
  the flag; `scripts/hp/hp-checkout-staleness.sh`'s probe comment records why the
  string is still a valid staleness probe.
- **Devdoc**: `devdoc/child-stream-task-output.md`; the superseded mechanism in
  `devdoc/worker-log-session-attribution-20260912.md` carries a
  "SUPERSEDED IN PART" banner naming what was kept (the principle) and what was
  deleted (the mechanism).

## Not Done (honest)

- **A serve/dispatch spawn path was NOT exercised end-to-end.** The seam is
  shared (`runTaskRunOnce`) and the two reachable paths were verified on a real
  binary, but no live serve tick was driven. This is the one leg of the
  briefing's evidence list left to the reviewer.
- **Pre-existing NEW violations reported by `internal/standards` are untouched**:
  `internal/tools/task_merge.go:336,366` and
  `internal/agent/journalreport_test.go:101,104` fail the Gate 1/2/3 red-proofs
  **on master too** (verified by running the same gate in `/home/user/work/GoPi`).
  One baseline entry was re-pointed in this branch (`cmd/agent/main.go:972 →
  973`) because this change adds one net line above that `%v`-wrap site.
- **Pre-existing environment-dependent test failures were left alone.** The
  three families, each re-run in `/home/user/work/GoPi` at `0dac2ae7` with the
  SAME command and failing IDENTICALLY there:
  1. *daemon / session-scope* — `TestWorkerDaemon_DetachesAndKeepsRunning`
     (`setsid did not take`), `TestWorkerDaemon_LeavesTheLoginSessionScope`,
     `TestMergerDaemon_LeavesTheLoginSessionScope`,
     `TestWorkerDaemon_RequireScopeIsHardWhenUnavailable`,
     `TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis`,
     `TestMergerFlock_DaemonLockStopsWithTheDaemon`: no systemd user scope is
     available in this environment (the documented setsid-only degradation).
  2. *shared-host / tmp residue* — `TestTmpDirValueProbeLeavesNothingBehind`
     (11-hour-old probe directories in the shared temp root),
     `TestPinCoverageTable`, `TestScriptsAreBoundedAndOffTmp`,
     `TestEveryTempUsingSuiteIsPinned`,
     `TestTestBudgetHasASingleSourceAndNamesATimeout`,
     `TestWorkerOwnershipPointerPresent` — all red on master.
  3. *`[gopi] tmpdir:` prelude counted as output* —
     `TestCLIContractTaskServeAliasPrintsOneLinePointer` and
     `TestTasksCLIListHeaderCountMatchesRows` (each counts printed lines and the
     environment's tmpdir note is one of them), and `TestStandardGateDataReport`
     (red on master too).
  4. *parallel-collision flakes, green when run alone* —
     `TestTaskFindSessionJSONLStillHonoursEveryLayout` and
     `TestWorkerReleaseOwnWorktree_Acceptance` failed in the full-package run
     while 6+ sibling worktrees were executing their own suites on this host,
     and both pass when re-run alone. Do not treat either as a regression.

## Verification

**Build / vet**

```
CGO_ENABLED=0 go build ./...      → OK
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → OK
go vet ./...                      → OK
```

**Unit tests (this branch)**

```
go test -count=1 ./cmd/agent/ ./internal/tools/ ./internal/testutil/ ./internal/config/ ./internal/standards/
```

- `./internal/tools/` **ok** (259.7s, full package).
- `./internal/config/` **ok**.
- `./cmd/agent/`, `./internal/testutil/`, `./internal/standards/`: the full-run
  failures are exactly the pre-existing set enumerated under **Not Done**, each
  reproduced on master at `0dac2ae7` with the same command.
- `./internal/standards/` red-proofs: the SAME three NEW violations as master
  (`internal/tools/task_merge.go:387`, `:336`,
  `internal/agent/journalreport_test.go:101,104`) — this branch introduces none
  and re-points its own baseline entry only.

**New regression tests (this branch)**

- `TestExecutorChildOutputLogLivesWhereTaskShowLooks` — the file the seam opens
  is the file `task show`'s own glob finds (two different code paths, so
  agreement is real, not a mirrored literal), it is named `output.log`, and it
  names its own task.
- `TestExecutorOutputLogAppendsOnReRun` — a second run of one row gets its own
  file; the first run's bytes survive; one glob reaches both.
- `TestExecutorOutputLogReportsAnUnwritableDestination` — an occluded sessions
  root yields an error and a nil file (no silent fallback).
- Updated: `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` (the
  round's move line on the launcher's stream, the run's own record in
  `output.log`), `TestTaskWorkdirAndOutputLogShareTheOneSessionsRoot` (replaces
  the retired explicit-root case),
  `TestWorkerUnknownArgumentIsRefused` (four retired-flag cases: both spellings,
  both positions, both faces), `TestWorkerUpCarriesNoRetiredLauncherForms`,
  `TestWorkerReapIdentifiesItsOwnProfile`, and the scan-table case in
  `worker_daemon_flag_e2e_test.go`.

**End-to-end, real binary, hermetic sandbox** (`GOPI_HOME=<sandbox>`,
`--repo <scratch git repo>`, `GOPI_ALLOW_AGENT_SPAWN=1 GOPI_HUMAN_TERMINAL=1`):

| path | command | result |
|---|---|---|
| worker round | `gopi worker --once` | `sessions/<id>/output.log` written (77 lines), row `completed`, launcher stream carries `gopi task: <id> output log → …` |
| manual | `gopi task run <id>` | the SAME file name written for the other row, announce line on the operator's stream, child's `executor cwd → <repo> (… deliverable repo)` inside it |
| `task show <id>` | — | `task_output:  …/sessions/<id>/output.log` — the exact file the seam opened |
| re-run guard | `task run` on a completed row | refused by the claim guard (not a seam failure) — the append/pid-suffix contract is covered by the unit test above |

**Launcher / CLI refusal (real binary)**

```
$ ./bin/gopi worker --worker-log-root /tmp/zz
gopi worker: unexpected argument "--worker-log-root" — refusing to start the worker loop
  accepted: -h/--help | --once | --daemon
  (the retired `--worker-log-root <dir>` is no longer a flag: a task's output goes to its own
   session directory, <gopi-dir>/sessions/<task-id>/output.log)
  if you passed an option a newer gopi supports, the deployed binary is STALE
$ echo $?    → 2

$ bash scripts/gopi-worker-up.sh 1 --worker-log-root /tmp/x
gopi-worker-up.sh: unexpected argument '--worker-log-root' — --worker-log-root is retired; a task's
output goes to <gopi-dir>/sessions/<task-id>/output.log (its own session directory, decided by the
spawn seam, not by the launcher)
$ echo $?    → 2
```

`gopi worker -h` no longer advertises the option (it explains where a task's
output goes instead: `no --worker-log-root any more`), and
`grep -rn 'GOPI_WORKER_LOG_ROOT' --include=*.go` finds it only inside refusal/
explanatory text — no reader.

## Commit

`feat(worker): the executor child writes its own stream into its row's session dir`
on branch `feat/child-stream-task-output` (worktree
`/home/user/workspace/gopi-child-output`). The branch is in the SHARED repo — the
commit is already on it, no push needed.

Recovery: `git show feat/child-stream-task-output:devdoc/child-stream-task-output.md`
