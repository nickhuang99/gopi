<!-- doc-class: historical -->
# Reframe telemetry double-count — root cause, fix, verification

Task: `01a089c9-c5af-7b17-8b1b-b487975e56b4` · 2026-09-10 · branch `fix/reframe-telemetry-doublecount`

## Symptom (as reported)

`task_events WHERE event='reframe'` carried 21–22 rows per task while the session
JSONL showed a single-digit number of real `task(action=reframe)` tool calls
(01a0898e×22, 01a088ef×20, 01a08919-8ef1×18, 01a08975×17, 01a08987×11). Since
`gopi task verdicts` sums those rows, the anti-bureaucracy meter (and the
"count > 1 = bureaucracy loop" detector) read pure noise.

## Investigation — the *reported* hypothesis was wrong

Hypothesis in the briefing: "the telemetry write runs every loop round / per
tool result instead of once per dispatch".

Falsified by measurement, not by reading:

- **In-process cardinality is exactly 1:1.** A scripted 14-round `Run` with ONE
  `task(action=reframe)` call produced exactly **1** row (probe test, 12 extra
  bash rounds).
- Session JSONL for 01a0898e has **2** reframe `tool_use` blocks (at
  12:38:20 and 13:35:02) — the session ran ~2.4h and reframed twice (the second
  reply is the "already reframed this run" branch). 2 real dispatches → 22 rows
  = **11× over-count**, not "one per round".
- `handleTaskReframe` (internal/agent/reframe.go) calls
  `tools.TaskReframeRecord` **once**; that is the ONLY production call site
  (`grep -rn "TaskReframeRecord("` → agent + tests only). `taskEventWrite` is
  called once from it. So the extra rows came from **another process**.

## Root cause (reproduced deterministically)

`GOPI_TASK_WRAPPER_ID` is how `gopi task run` tells its child "you already have
a ledger row": `cli_tasks.go runTaskRunOnce` sets `cmd.Env =
append(os.Environ(), TaskWrapperEnv+"="+target)`. The child latches
`autoTaskRowID` to that wrapper id (taskregister.go) — correct in the child,
**a lie in every descendant**, because:

- `bashChildEnv()` (bash.go) copied `os.Environ()` verbatim → every bash/job
  command inherits the wrapper id;
- `job.go` set no `Env` at all → same inheritance;
- and `GOPI_MEMORY_DB` is usually unset inside a run on master, so
  `memoryDBPath()` resolves the **developer's real `~/.gopi/memory.db`**.

So a nested process that re-launches gopi — most commonly the agent's own
`go test ./internal/agent/`, whose reframe tests drive a real `handleTaskReframe`
— believes it is the wrapper child and writes ITS telemetry under the WRAPPER's
task_id, into the real ledger.

Reproduction (the number, measured):

```
GOPI_TASK_WRAPPER_ID=<task> GOPI_MEMORY_DB=<scratch>/memory.db \
  go test ./internal/agent/ -count=1
→ 2 reframe rows per run under the wrapper's task id   (probe-suite-leak|2)
```

22 rows / 2 rows per run ≈ 11 runs. The task ran 2.4h with nested suite runs
(scalar tasks routinely run `go test ./internal/agent/` while fixing that
package) — the count is exactly the nested-run count, not a per-round write.

## Fix (two halves)

1. **Transport — `internal/tools/childenv.go` (new).** `stripWrapperIdentity`
   removes `GOPI_TASK_WRAPPER_ID` / `GOPI_TASK_CHILD_SESSION` from the
   environment of every spawned shell (`bashChildEnv`, `JobStart`), with one
   log line in `job.go` when a strip happened. A descendant can no longer
   inherit the right to write under the wrapper row; it auto-registers its own
   row instead. The identity is still set with `cmd.Env` on the direct `task
   run` child, so the intended behavior is untouched.
2. **Attribution — `origin=<session>/<pid>` in every reframe row.** The session
   half comes from the Agent's own session (`a.sessionIDForTasks()` — never an
   env lookup, an inherited env is the bug itself), the pid is un-forgeable.
   `TaskReframeRecord(taskID, verdict, items, origin)`; `gopi task verdicts`
   now warns on a task with more than one origin:

   ```
   ⚠ 2 distinct origins reported under this task — telemetry attributed from outside the run
   ```

   So even if some future path restores propagation, an inflated count is
   *visible and attributable* instead of silently summed.

3. **Test-side leak closed — `internal/agent` `TestMain`.** The package had no
   `TestMain`; it now pins `GOPI_MEMORY_DB`/`GOPI_HIPPO_DB` to a per-run scratch
   root and unsets both wrapper identity vars before `m.Run()`. Without it, any
   `go test ./internal/agent/` executed inside a task run still wrote the
   wrapper's row (the guard test `TestSuiteLedgerPin_IsActive` fails if the pin
   is removed).

`gopi task show`/verdicts rendering is otherwise unchanged; old rows keep their
(origin-less) detail and merely lack the new field.

## Verification

- **Repro evidence (before/after)**: `probe-suite-leak|2` rows from one suite
  run with the identity inherited (pre-fix) → **zero** reframe rows post-fix
  (job output in the report companion; scratch ledgers under `/tmp/probe-*`).
- **In-process cardinality**: `TestReframeDispatch_WritesExactlyOneRowWithOrigin`
  — 1 dispatch = 1 row, 2 dispatches = 2 rows, detail prefix
  `verdict=DOABLE items=0 origin=sess-test/<pid>`.
- **Transport**: `TestBashChildEnv_StripsInheritedWrapperIdentity`,
  `TestStripWrapperIdentity_RemovesOnlyWrapperIdentity`,
  `TestBashToolCall_DoesNotPropagateWrapperIdentity` (end-to-end: a real `Run`
  bash probe prints `WRAPPER=[unset] CHILD=[unset]`).
- **Attribution**: `TestReframeRecord_StampsOriginPerWriter` (two writers, two
  origins, the report flags them).
- **Mutation check**: with `wrapperIdentityEnvVars` emptied, both transport
  guards FAIL; restored → PASS.
- Build: `go build ./...` OK; `go vet ./internal/tools/ ./internal/agent/` OK;
  `go test ./internal/agent/ -count=1` and `./internal/tools/` green (see the
  task report for the exact run output).

## Historical data

Rows written before this fix keep their inflated counts (no origin field ⇒ not
attributable retrospectively). Nothing is deleted — the trail is append-only;
`gopi task verdicts` output for those tasks should be read with the multi-origin
warning in mind, and any count taken as "bureaucracy evidence" before
2026-09-10 needs re-derivation from session JSONL.

## Files

- `internal/tools/childenv.go` (new) — identity strip (Rule 14: one definition,
  referenced by bash.go + job.go).
- `internal/tools/reframe.go` — `origin` field, per-writer stamp,
  multi-origin warning in the verdicts render.
- `internal/tools/bash.go`, `internal/tools/job.go` — strip on spawn.
- `internal/agent/reframe.go` — pass the dispatching session id.
- `internal/agent/testmain_isolation_test.go` (new) — suite ledger pin.
- `internal/tools/reframe_telemetry_test.go`,
  `internal/agent/reframe_telemetry_test.go` (new) — the guards.
