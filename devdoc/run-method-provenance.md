<!-- doc-class: normative -->
# Task Report: run-method-provenance — run_method + run_cmd on the task ledger

Branch: `feat/run-method-provenance` (worktree `~/workspace/gopi-run-method-provenance`, base 991dbe4)

## Goal
Record HOW a task was executed (which launcher path) and the exact re-runnable
command line — like `/proc/<pid>/cmdline` — directly in the `tasks` table, so
ops can `task list` / `task show` answer "how was this run" and can resume a
crashed / SIGKILLed task with the verbatim recipe.

## Completed

### ① Schema — internal/tools/tasks.go
- `tasksSchemaTable()` CREATE literal gains `run_method TEXT DEFAULT ''` and
  `run_cmd TEXT DEFAULT ''` (fresh tables).
- `tasksP2Columns` gains the two idempotent ALTER statements (the ensureSchema
  migration pattern — duplicate-column errors swallowed at ~L725):
  `ALTER TABLE tasks ADD COLUMN run_method TEXT DEFAULT ''`,
  `ALTER TABLE tasks ADD COLUMN run_cmd TEXT DEFAULT ''`.
- `tasksRebuildCheckConstraint` copy SELECT gains `colExpr("run_method","''")`
  and `colExpr("run_cmd","''")` + the two names in the INSERT column list
  (legacy four-status DB rebuild defaults the missing columns).
- `Task` struct gains `RunMethod string` + `RunCmd string`.
- `taskSelectColumns` + `scanTask` + `scanTaskRow` gain the two columns —
  POSITIONAL, adjacent (the scanTaskRow single-row variant was the P2.5
  pitfall: it has its OWN Scan, not a scanTask wrapper — both were widened).
- FTS (`tasksFTSColumns`) deliberately NOT indexed: operational metadata,
  like claimed_by/claimed_at — zero FTS churn.
- Two literal test-schema fixtures (task_dispatch_test.go:202,
  repro_review_test.go:134) were audited: both are LEGACY fixtures (they
  deliberately lack the new columns to exercise the migration/rebuild path —
  no change needed; assertions extended).

### ② Stamp helper — internal/tools/tasks.go
- `TaskStampRun(taskID, method, cmdline string) error`: UPDATE
  `tasks SET run_method=?, run_cmd=?, updated_at=? WHERE task_id=?`.
  - Empty method = silent no-op (never wipes an earlier stamp).
  - Empty cmdline keeps the earlier command (never clears a filled value).
  - Non-empty cmdline overwrites (last-non-empty-wins — a re-run records ITS
    launch command; a resume of a crashed row records the resume recipe).
  - Best-effort: returns error for log-and-continue, never fails the run.
  - `updated_at` moves so the sync version anchor reflects the stamp.

### ③ Launch-site stamps
- **manual / worker** — cmd/agent/cli_tasks.go `runTaskRunOnce`: stamp right
  after `TaskStart` + the claim-lock protocol (near the "running gopi with
  task %s as briefing…" print). `runMethodFor(target, opts)` resolves:
  - worker latch (`workerWriteback == target`) → `worker`,

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

    cmdline = the worker process's OWN argv rendered by `runcmd.Command`
    (`gopi worker --once --worker-log-root …`) — the round runs in-process, so
    this IS the verbatim launcher command. (Superseded 2026-09-13: it used to
    read `gopi worker (claim→run round, target <id>)`, a human-readable
    pseudo-command that could not be pasted into a shell — see
    devdoc/task-show-run-cmd-verbatim.md.)
  - otherwise → `manual`, cmdline `gopi task run <id>` + the caller's
    `--report-to`/`--repo` when present (a dispatched NODE's own row carries
    its full return path — the same command dispatchRemoteCommand built).
  - NOTE (serve/worker-split, already in master): the serve/merger daemon no
    longer executes payload tasks (06a5154) — payload execution funnels ONLY
    through runTaskRunOnce (manual + worker), so no separate serve stamp site
    exists. The `serve` method value is reserved for legacy deployments /
    future re-introduction; the enum is documented in the schema comment.
- **dispatch** — cmd/agent/dispatch.go `dispatchToNode` Leg ③ (center mirror
  update → in_progress): stamp `method=dispatch`, run_cmd = the exact remote
  command (`gopi task run <id> --report-to <alias> [--repo X]`) just handed
  over via ssh. The NODE later stamps its own row `method=manual` with the
  same flags — both truthful on their own DB; sync keeps the last-non-empty.
- **resume** — cmd/agent/cli_resume.go `runResume` --task linkage block (next
  to TaskTagResumed): stamp `method=resume`, run_cmd =
  `gopi resume <resolved-raw-log-path> --task <id>`. Same non-fatal posture.
  On a crashed row the run_cmd doubles as the "how to continue" hint.

### ④ Sync propagation — internal/tools/memory_sync_task.go + memory_sync_delta.go
The sneaky part: a column that does not travel shows empty on peers. Every
wire path now carries run_method + run_cmd:
- `SyncTaskRow` struct: `RunMethod`/`RunCmd` (`json:"run_method,omitempty"`).
- v1 export SELECT + scan (memory_sync_task.go ~L283): COALESCE'd columns.
- v2 delta export (`syncDeltaSelectColumns`) + scan: COALESCE'd columns.
- `syncDeltaApplyNewer` rank-guarded UPDATE: `run_method = COALESCE(NULLIF(?,
  ''), run_method), run_cmd = COALESCE(NULLIF(?, ''), run_cmd)` — the remote
  provenance rides a forward move, last-non-empty-wins (a re-run's newer
  stamp updates the peer), never clears a filled local value with empty.
- `syncDeltaInsertRow` (fresh row): columns in the named INSERT.
- Harvest face: `harvestRemoteSelect` gains the two COALESCE'd columns,
  `harvestRemoteRow` gains fields, `harvestInsert` INSERTs them,
  `harvestUpdate` adopts them with COALESCE NULLIF. Terminal locals are
  never rewritten on the harvest face (终态不回退 — pre-existing guard), so
  provenance backfills arrive on the fresh insert.
- `importTaskMirror` INSERT carries them.
- memory_sync_delta.go ~L456 INSERT audited: named columns → safe.
- Claim-lock channel rides the v2 exporter → covered automatically.

### ⑤ Display
- `TaskShow`: renders `run_method:` + `run_cmd:` after submitted_by (only
  when non-empty — legacy/never-run rows stay clean).
- `TaskList`: compact ` [via manual]` / ` [via worker]` / ` [via dispatch]` /
  ` [via resume]` suffix following the `[running …]`/`[resumed×N]` pattern.
  Full command line stays in `task show` (list rows stay sane-width).
- `/task list` (commands.go:253) calls TaskList → automatic.

### ⑥ Tests
- `TestTaskStampRun` (internal/tools/tasks_test.go): no-op empty stamp,
  first stamp lands, empty-cmdline keeps command, re-stamp overwrites,
  terminal writeback never clears provenance.
- `TestRunMethodFor` (cmd/agent/cli_tasks_test.go): manual bare, manual with
  --report-to/--repo (the dispatched node's own stamp), worker latch, latch
  mismatch fallback.
- `TestDispatchToNode` extended: center row carries run_method=dispatch +
  the re-runnable remote command.
- `TestSyncDeltaRunProvenanceTravels`: provenance rides the v2 delta —
  fresh insert carries it; a re-run's newer worker stamp overwrites on the
  peer (last-non-empty-wins).
- `TestHarvestInsertNewRows` extended: provenance rides the harvest wire
  (fresh terminal row lands method + cmdline).
- `TestEnsureTasksSchemaMigratesLegacyFourStatusDB` extended: legacy rows
  default run provenance to ''.

## Not Done
- `serve` method value: the serve/merger daemon (post serve-worker-split)
  executes no payload tasks, so no live serve stamp site exists. Value is
  reserved + documented; stamping would be a no-op today.
- Merge-backlog executor (`TaskMergeBacklogExecute`, claimed_by=merge-executor)
  is git-merge automation, not a payload execution — NOT stamped (out of the
  epic's enum scope; claimed_by already identifies it).
- FTS index of the new columns: deliberately skipped (operational metadata —
  task_events precedent).

## Verification
- Build: `go build ./...` OK.
- Tests: full suite via background job (see commit message evidence);
  targeted runs green:
  - internal/tools schema/migration tests (`TestEnsureTasksSchema…Legacy…`,
    `TestReproLegacyMigrationLosesFTSRows`) PASS.
  - sync tests (`TestSyncDeltaRunProvenanceTravels`, `TestHarvestInsertNewRows`,
    `TestSyncDeltaTwoNodeHandoff`) PASS.
  - cmd/agent dispatch tests (`TestDispatchToNode`) PASS.
  - `TestRunMethodFor`, `TestTaskStampRun` PASS.
  - Pre-existing suite flake noted in master: `TestRunTaskWorker_OnceSweepsZombieFirst`
    fails only under full-suite parallel load (notify.log race) — reproduced
    on a clean checkout of master (stash test), unrelated to this change.
- Live smoke (isolated GOPI_HOME/GOPI_MEMORY_DB):
  - `task submit` → `task run <id>` → `task show`: run_method=manual,
    run_cmd=`gopi task run t-smoke-manual`; completed row keeps the stamp.
  - `worker --once` round: run_method=worker, run_cmd names the target.
  - `gopi resume <raw.log> --task <id>`: run_method=resume,
    run_cmd=`gopi resume <raw.log> --task <id>` (alongside the resumed: tag).
  - `task list` shows `[via manual]` / `[via worker]` suffixes.
- Legacy-DB regression: pre-column ledger migrates (idempotent ALTER no-op +
  rebuild colExpr path) and lists without error — pinned by
  TestEnsureTasksSchemaMigratesLegacyFourStatusDB (run_method/run_cmd default '').

## Commit
- bbb66b9 feat(tasks): run_method/run_cmd run provenance columns + TaskStampRun
- 98a549e feat(run-provenance): stamp manual/worker/dispatch/resume at launch sites
- d767586 feat(run-provenance): carry run_method/run_cmd over the sync wires
- 74e722a test(run-provenance): pin stamp semantics + wire travel
- a5140b8 docs(run-provenance): epic report — schema, stamp sites, sync wires, verification

(Branch `feat/run-method-provenance` — 5 commits, working tree clean, NOT merged.)

## Superseded (2026-09-13) — run_cmd is now a VERBATIM command, not a description

`devdoc/task-show-run-cmd-verbatim.md` revisits two gaps this epic left:

1. Rows minted OUTSIDE the `task run` wrapper (the harness auto-register row,
   the merger's `resolve merge conflict: …` row) carried NO provenance at all —
   provenance was only ever STAMPED by runTaskRunOnce, and creating a row had
   no field to carry it. Now `taskCreateFields`/`insertTaskRow` accept
   RunMethod/RunCmd, so those rows are born with it.
2. The worker cmdline above and the manual cmdline were hand-BUILT strings. They
   are now rendered by `internal/runcmd` (a leaf constant+renderer package) from
   the launcher's real argv: `run_cmd` is always a pasteable command, argv[0] a
   basename, arguments shell-quoted only when needed. The method vocabulary and
   the CLI words live in that package as exported constants.
