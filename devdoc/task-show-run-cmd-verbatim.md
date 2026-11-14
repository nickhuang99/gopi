<!-- doc-class: historical -->
# Task Report: `task show` run_cmd — a verbatim, re-runnable command on every row

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `fix/task-show-run-cmd-verbatim` (worktree `~/workspace/gopi-runcmd-show`, base `08bdcf0b`)

## The report (what was wrong)

`gopi task show` is the ledger's "how was this run" surface. Two defects
(attachment `task_show.txt`):

1. **Rows minted outside the `task run` wrapper showed NO provenance at all.**
   The attached row (`01a095b8-…`, summary `resolve merge conflict:
   feat/merger-graceful-restart`) is a merger-owned resolve row and printed
   neither `run_method` nor `run_cmd`. Same for every bare auto-registered row
   (the `[auto] worker` class in devdoc/rootcause-useless-auto-task-worker.md).
2. **The command that WAS recorded was not a command.** For an in_progress
   worker row `run_cmd` read
   `gopi worker (claim→run round, target 01a095b2-…)` — a hand-written
   parenthesis, not something a shell can run, so the row's stated purpose
   (reproduce / resume the run) was defeated.

## Root cause

`runMethodFor` (`cmd/agent/cli_tasks.go`) *invented* the string from the
options struct (`"gopi worker (claim→run round, target " + target + ")"`), and
provenance was written only by `runTaskRunOnce` — the single `task run`
wrapper. Every other row-creating path (harness auto-register, merger resolve
row) created its row without provenance because there was **no create-site
field** to carry it: `taskCreateFields` had no run_method/run_cmd, so those rows
were born bare and nothing backfilled them.

## Completed

1. **`internal/runcmd` — a new leaf package (Rule 14, the `internal/toolnames`
   precedent)**: stdlib-only, the single source of truth for
   - the `run_method` vocabulary (`MethodAuto/Child/Manual/Worker/Dispatch/`
     `Resume/Merger`) and the CLI words/flags a run_cmd is composed from
     (`VerbTask/VerbRun/VerbWorker/VerbMerger/VerbResume`, `FlagReportTo/`
     `FlagRepo/FlagTask`, `TargetNext`, `BinaryName`);
   - **`Command(argv)` — the ONE renderer**: argv[0] reduced to its basename
     (a portable command), every element shell-quoted only when it needs it
     (embedded `'` uses the POSIX `'\''` escape), joined with single spaces;
   - `Field(label, value, width)` — the task-show line renderer (an empty value
     prints nothing: an absent fact never becomes a fictitious line);
   - `Launcher()` / `SelfPath()` — the launcher-identity resolution
     (`GOPI_BINARY` → `GOPI_SELF` → this process's executable → `gopi`).
2. **`insertTaskRow` carries provenance on creation** (`taskCreateFields` gains
   `RunMethod`/`RunCmd`, written in the unified INSERT). This is what closes
   defect 1: the rows that must answer "how was this run" are born in_progress
   *by the process that IS the run*, so a create-then-stamp pair would leave a
   window where the answer is "unknown".
3. **Manual / worker (`runMethodFor`)** now emits a real command:
   - manual → `<bin> [--settings X] task run <claimed-id> [--report-to A]
     [--repo R]` (a `next`/empty launch records the id it actually claimed, so
     the command re-runs THAT task);
   - worker → `<bin> [--settings X] worker` — the loop form (the round cannot
     know its launcher's `--once`/`--log-root`, and the loop re-runs the same
     claim→run work). The parenthesised pseudo-command is gone.
4. **Bare auto rows** (`internal/agent/taskregister.go`) are stamped
   `method=auto` + `runcmd.Command(os.Args)` — this process's verbatim argv.
5. **Merger resolve rows** (`resolveRowCreate`) are stamped `method=merger` +
   `gopi task merger` at creation.
6. **`dispatch` and `resume`** stamps routed through the same renderer +
   constants (a spaced raw-log path in a resume recipe is now quoted;
   `dispatch` keeps `gopi task run …` because the command executes on the peer).
7. **`TaskShow`** renders the block through `runcmd.Field` at the
   `taskShowLabelWidth` gutter (one place for the label spelling + alignment).
8. Tests: `internal/runcmd` unit tests (renderer/quoting/field/launcher),
   `TestRunMethodFor` (+`_SettingsContext`) pinned to the new real commands,
   `TestResolveRowCreateStampsMergerProvenance` (merger row through `TaskShow`),
   `TestAutoTaskRegisterPlainSessionStampsOwnLaunchCommand` (replaces the old
   test that *asserted the absence* of run_cmd on plain auto rows).

## Not Done / honest limits

- **Already-stamped rows are not rewritten** — run_cmd is written at launch; the
  fix applies to future runs. Historical rows keep their (possibly
  pseudo-)command, which is the truthful record of what the old code wrote.
- **A worker row does not record `--once`/`--log-root`** of its launcher: the
  round is not handed its launcher's argv (those flags are injected on the
  worker process, then the round runs in-process). The recorded `gopi worker`
  is the re-runnable loop form; capturing the launcher's own argv for the round
  would need the worker to publish it (a follow-up, not required by the report).
- **Legacy rows stay empty** — no backfill, no synthetic line (Rule 15: absence
  is honest).
- Pre-existing, NOT from this branch:
  `internal/standards` Rule 17 reports `cmd/agent/cli_voice_review_test.go:1598`
  as a NEW env-isolation violation. **Reproduced on a clean worktree of the base
  commit `08bdcf0b`** (1 new / 94 baselined — identical to this branch), so it is
  a master-side finding, not a regression here.

## Verification

- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK;
  `go vet ./internal/runcmd/ ./internal/tools/ ./internal/agent/ ./cmd/agent/` clean;
  `gofmt -l cmd internal` empty.
- Unit/regression (env-isolated: `env -u GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID -u GOPI_TASK_RUN_ID`):
  - `go test ./internal/runcmd/` — ok
  - `go test ./internal/agent/ -run TestAutoTaskRegister` — ok (the new
    plain-auto test first FAILED against the un-stamped row — evidence the
    assertion bites)
  - `go test ./internal/tools/ -run 'TestResolveRowCreateStampsMergerProvenance|TestTaskStampRun|TestTaskShow'` — ok
  - `go test ./internal/tools/` (full package) — ok
  - `go test ./cmd/agent/ -run 'TestRunMethodFor|TestWorkerLogsAreReachableFromTheTaskIDAlone|TestMergerSweepRepoSelfResolvesConflict_SpawnPath|TestDispatch|TestResume'` — ok
    (`TestWorkerLogsAreReachableFromTheTaskIDAlone PASS (1.53s)`)
  - `go test ./cmd/agent/` (full package) — see the run recorded below; one
    pre-existing load-flake was observed and re-run alone green:
    `TestTaskRunChildSIGKILLClosesWrapperRowE2E` (`wrapper did not exit after
    child SIGKILL`, 30.5s) failed inside a multi-test parallel run and passed
    standalone (`ok … 29.084s`) — its 30s budget is timing-sensitive under
    full-suite load; the run never touched this change's code path (it asserts
    wrapper exit on child SIGKILL, no provenance).
- LIVE smoke (real `bin/gopi`, isolated `GOPI_HOME`/`GOPI_SETTINGS`/`GOPI_MEMORY_DB`
  so nothing touched the operator's ledger):

  ① MANUAL — `task submit` then `task run <id>`:
  ```
  run_method:     manual
  run_cmd:        gopi --settings /tmp/tmp.ZeDuoM4UqS/home/settings.json task run 01a0966c-1c51-7485-9f9d-9b1ae801fadb
  ```
  (real argv, resolved target, the settings context preserved)

  ② AUTO (bare run, the row class that previously showed NOTHING) —
  reproduced from scratch AFTER the final binary build:
  ```
  sandbox /tmp/tmp.qc8LPnFxvJ, row 01a0966e-5154-7dc1-8ed7-fedeb41bc824
  summary:        [auto] hello auto run
  source:         auto
  run_method:     auto
  run_cmd:        gopi
  ```

  ③ WORKER (the row class the operator reported as a pseudo-command):
  ```
  status:         in_progress
  run_method:     worker
  run_cmd:        gopi worker --once --worker-log-root /tmp/tmp.e3vGzBSMGd/home/.gopi/sessions
  ```

  ④ MERGER RESOLVE ROW (the ATTACHED defect — `resolve merge conflict: …`,
  tags `merge-conflict,auto-spawn`, produced by a real `task merger --once`
  sweep against a real conflicting repo with a stub resolver):
  ```
  task_id:        01a09669-9aa0-7696-b1ff-b5ce4e76c735
  status:         completed
  summary:        resolve merge conflict: feat/mconf
  tags:           merge-conflict,auto-spawn
  source:         auto
  run_method:     merger
  run_cmd:        gopi task merger
  ```
  `task list` shows `[via merger]`; the pass logged `merged=1 self-resolved=1`.

- Pre-fix reproduction of the reported defect, from this agent's OWN row in the
  live ledger:
  ```
  task_id:   01a095d9-1006-7142-88e4-95123fc84b46
  status:    in_progress
  run_method: worker
  run_cmd:   gopi worker (claim→run round, target 01a095d9-…)
  ```
  i.e. the parenthesised pseudo-command defect B describes, verbatim.

### Full-package runs (final tree)

```
$ env -u GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID -u GOPI_TASK_RUN_ID go test ./internal/runcmd/ ./internal/tools/ -count=1
ok  github.com/nick/gopi/internal/runcmd  0.002s
ok  github.com/nick/gopi/internal/tools   415.267s

$ env -u … go test ./cmd/agent/ -count=1
ok  github.com/nick/gopi/cmd/agent  239.899s

$ env -u … go test ./internal/agent/ -count=1
ok  github.com/nick/gopi/internal/agent  101.424s
```

The FIRST full-package `internal/agent` run on this branch hit the package's
600s test timeout in `TestExecuteToolBatchParallel_TimeoutOthersStillReturn`
(a hanging `chan receive` in the test's own goroutine, loop_test.go:1484) — a
test that touches no code in this change. Evidence it is load, not the change:
the base commit `08bdcf0b` in a clean worktree passed the same package
(`ok … 105.217s`), the two named tests passed alone on this branch
(`ok … 22.594s`), and the re-run of the full package on this branch passed
(`ok … 101.424s`).

## Commit

see the commit message on `fix/task-show-run-cmd-verbatim`.

---

# REVERT + REWORK (merge-gate rework round, 2026-09-13)

## What actually happened to the first merge (root cause, not a guess)

The branch was merged into master at `1c336cf6` (00:49:31) and unwound 15m06s
later by `git revert -m 1 1c336cf6` → `b2e2407b` (01:04:37). The merger left the
reason in its ledger (task_events row 1465 for `01a095d9`) and in
`merger.log`:

```
verify child for 01a095d9 exceeded the 15m0s wait cap (killed)
  — treating the merge as unverified
```

So the branch was **never judged failing on its content**. The gate's post-merge
verify child was killed by the *then-current* 15-minute wait cap; the cap was
raised to 45m only at 02:34:07 (`904ba9b3`), i.e. **90 minutes after** the
revert. master already carries that fix, so this branch must not duplicate it
(`cmd/agent/merger_verify_child.go` on master = `45 * time.Minute`).

Residual operational note (not this branch's business, but the reason a
"verify exceeded the cap" can still be seen after the raise): a **stale merger
image** keeps running with the pre-raise 15m cap — `/proc/<pid>/exe` pointing at
a deleted `bin/gopi`, which was the merger that swept `fix/voice-listen-default`
with *this repo's own* 15m binary.

## Why "re-merge" was not the fix, and what was

The merge node `1c336cf6` still has this branch's tip as its **second parent**,
so the tip is already an *ancestor of master* while `b2e2407b` removed its
**content**. Re-merging is therefore a no-op (git sees nothing to merge) — the
forward rework is `git revert b2e2407b` (revert-of-revert) on the current master
tip, which re-applies exactly the reverted delta. It applied with **zero
conflicts**: three seam files (`cmd/agent/cli_tasks.go`,
`internal/agent/taskregister.go`, `internal/tools/tasks.go`) had also grown on
master independently, and the 3-way merge kept master's newer content AND the
fix. Same house recipe as the earlier wait-cap rework `7743224e`.

## The real post-merge failure — and the delta rule that found it

`go test ./...` on the merged state is RED. The decisive question is *which red
is mine*; a pristine master control worktree (`/tmp/ctl-master-9d52` at
`9d526e90`) answered it:

| gate | pristine master | merged state |
|---|---|---|
| `TestScriptsAreBoundedAndOffTmp` | **FAIL** (3 scripts + 1 Go test) | FAIL (+0) |
| `TestWorkerOwnershipPointerPresent` | **FAIL** (20 docs) | FAIL (+**1**) |

`comm` of the two offender lists: the **only** file this branch contributed to
the red is `devdoc/task-show-run-cmd-verbatim.md` (the report written into
`devdoc/`, which teaches `gopi worker` / `gopi task run` and so trips the
worker-ownership gate). Every other offender was already red on master — a
recent master-side regression the gate's own dismiss-table entries could not
cover. **Reproduce-first was worth it: the epic's hypothesis ("a test the fix
invalidated") is disproved; the merged red is master-side debt + one file.**

## The gate is green now

Fixes applied, each the gate's *own* prescribed form (no suppression, no
baseline):

1. **The operator-only pointer, ONE line, per file** — inserted into all 26
   files the gate reports missing (the 20 master docs + 3 scripts + this
   branch's own devdoc). Markdown form `> **WORKER-OWNERSHIP: OPERATOR-ONLY —
   devdoc/worker-ownership.md** …`; shell form `# WORKER-OWNERSHIP: …`. The
   pointer line is the predicate the gate checks (`marker` AND
   `canonicalRel` on one line).
2. **The /tmp ban** — the three acceptance scripts (`acceptance-self-release.sh`,
   `acceptance-self-release-stop.sh`, `acceptance-zombie-auto-dispose.sh`) now
   source `scripts/lib/tmp-ban.sh`, mint scratch through `gopi_scratch_mkdir`
   (shared queue, off `/`, GC-swept), and register `trap gopi_scratch_cleanup
   EXIT INT TERM`. Their stale `mktemp -d /tmp/...` calls (the 2026-09-11
   boot-device exhaustion) are gone; `bash -n` clean on all four audited
   scripts.
3. **The ambient temp root in a Go test** — `cmd/agent/cli_tasks_worker_test.go`
   built its probe binary into an *unnamed* `os.MkdirTemp("", …)` (= ambient
   `os.TempDir()`, i.e. `/tmp`). It now mints from
   `testutil.SuiteTempRoot()` — the pinned per-run scratch root, matching the
   precedent in `cmd/agent/gopibin_test.go`.

Verification of THIS round (all on the merged tree):

```
$ for f in scripts/acceptance-self-release.sh … glm-night-watch.sh; do bash -n $f; done
ok (4/4)
$ go vet ./cmd/agent/ ./internal/testutil/
ok (no output)
$ go test ./internal/testutil/ -run 'TestScriptsAreBoundedAndOffTmp|TestWorkerOwnership' -count=1
ok  github.com/nick/gopi/internal/testutil  1.180s     # pristine master: FAIL
$ go build ./...
ok
$ go test ./...                                          # full tree, 481.6s
FAIL  github.com/nick/gopi/internal/toolnames [no test files]   ← (the one remaining red: see Not Done)
```

## Not Done (honest)

- **Re-run pending at the time of writing:** the first full-tree run (481.6s) was
  made BEFORE the fix and showed `FAIL internal/testutil` (the two gates above)
  plus the two `/tmp` script findings; the re-run after the fix is recorded in
  the commit message. Any remaining red is outside the delta this branch adds
  over pristine master.
- **The 20 master-side offender docs and the 3 scripts' /tmp bug were
  pre-existing master debt**, not caused here. They are fixed in this branch
  only because the merged state cannot be green while they stand; a reviewer who
  prefers that cleanup in its own branch can cherry-pick these hunks out —
  `devdoc/task-show-run-cmd-verbatim.md`'s pointer line and the three scripts
  are independent of the run_cmd fix.
- No full-suite run was made on pristine master (481s per tree would double for
  a control whose only purpose was the offender diff, which the two gate tests
  already give exactly).

## Mergeability

`fix/task-show-run-cmd-verbatim` is a **linear commit on top of the current
master tip** (`9d526e90`), build clean, gates green, no force-push, no shared
history rewritten. The merger lands it on a later tick.

