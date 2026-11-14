<!-- doc-class: normative -->
# Bug Report: HP in_progress zombie rows — why terminal close-out never ran

> **CORRECTED 2026-11-05.** The measurements below are the record of 2026-09-02 and
> stay; but three mechanisms they name are DELETED: `runTaskSyncTick` (the serve
> tick's db-sync pass), `serveGitSyncSweep` and the `GOPI_SYNC_GIT_INTERVAL` knob it
> read — the whole cross-node sync surface went on 2026-11-05.
> `GOPI_SYNC_GIT_INTERVAL` was removed because it had **no production reader at all**
> (it was advertised by the catalog and `gopi --help-env` while `serveGitSyncInterval`
> did not exist). Read the paragraph below as evidence of what the machine did on that
> date, not as a list of what runs today; `internal/config/sync_knob_removal_gate_test.go`
> is the gate that keeps the knob from coming back.

**Date**: 2026-09-02 · **Node**: HP (client→peerbox) · **Branch**: `feat/hp-zombie-closeout`
**Commit**: `019c464` · **Trigger incident**: 3 in_progress rows, no live process, updated_at frozen 1h+, PA cancelled all three by hand at 08:2x.

## Root cause per row (file:line evidence)

### Row 1 — `78d40ad1…` (auto, summary "tash run t-1788304050", no session file)
- **Row form**: auto mirror of the child runner spawned by peerbox's `task run t-1788304050`. TaskNew created it with `task_id = session uuid` (pre-consolidation code, `internal/agent/taskregister.go` at 941b40c: `TaskNew(summary, sessionID, sessionID, model)`), tags `from-peerbox` — it was **imported by memsync, not created locally**; the child session JSONL never existed on HP (the child ran on peerbox; only the row was mirrored).
- **Why no close-out**: row 2's peerbox source went terminal (failed/cancelled); TaskIngest (the HTTP mirror that would carry terminal status forward) was **retired on master** (`3b3c7ec`, dead-code sweep 2026-09-02) — the ingest path could no longer receive peerbox's terminal push. The memsync delta engine only mirrors **forward** statuses (`internal/tools/memory_sync_task.go:890` `r.Status == ""` live-mirror guard; terminal rows are not downgraded and there is no "expire mirror whose source is terminal" mechanism). With no serve tick and no sync pass, the mirror stayed in_progress forever.
- **Liveness optics**: `claimed_by` was empty (TaskStart CAS never writes claimed_by; only TaskNext/TaskResumeClaimed do), so the zombie evidence chain's pid layer degraded to "suspected" (`internal/tools/taskzombie.go:150`), and the row's session-mtime layer had no file to check.

### Row 2 — `t-1788304050` (mirror of peerbox task, stuck in_progress)
- Created 07:07:30 (source=human, `from-peerbox`); peerbox side reached terminal ~07:23.
- **Close-out mechanisms that all require a process**: FinishSessionTask fires at session exit of the runner (`internal/agent/taskregister.go:270`); stale sweep fires on serve pass first step / `task run` entry / `resume` entry (`cmd/agent/cli_tasks.go:3083`, `cmd/agent/cli_resume.go:172`); sync (`runTaskSyncTick`) rides the serve tick too (`cmd/agent/cli_tasks.go:1280`). HP had **none**: no serve daemon, no worker loop (ps showed zero gopi processes). The peerbox→HP git/db sync cadence (serveGitSyncSweep 15min, GOPI_SYNC_GIT_INTERVAL) also only runs inside a serve pass.
- So the mirror row had no in-process writer left AND no sweep trigger to discover it — the exact "trigger island" shape fixed for serve in `feat/stale-sweep-trigger 941b40c`, now reappearing on the read side.

### Row 3 — `t-1788275530` (LLM-submitted, created 23:12 prev day)
- Its runner (a worker-loop round or a task run child) **died without session_end** — kill -9 / SIGKILL leaves no exit hook, so `finishSessionTask` → writeback → terminal push never ran (the normal close-out chain: `internal/agent/loop.go` session_end → `FinishSessionTask` → `TaskRunWriteback`).
- Subsequent sweep triggers would have caught it — but same island: no serve/worker process on HP afterwards. It sat from 07:16:33 (last updated_at bump = last heartbeat touch) until PA's manual cancel.
- Evidence: task_events for all three rows contain only the manual `cancelled` entries (08:30:28) — nothing ever reported them stale.

## Cross-cutting root cause
The stale sweep is **trigger-side complete but read-side blind**: every trigger lives inside a long-running process (serve pass, task run, resume). HP's usage pattern is one-shot CLI invocations; when the runner dies, *no future gopi process touches the ledger* until a human looks — and "looking" (`task list`) didn't sweep.

## The fix (minimal, no daemon — consistent with "I hate service")
**`task list` reads now trigger the sweep** (query-side, throttled):

> **SUPERSEDED 2026-09-14** (`fix/task-list-sweep-readpath`,
> `devdoc/task-list-sweep-readpath.md`): the sweep no longer rides a plain read
> at all. Measured, the read paid ~45 ms against a ~4 ms render (and paid it in
> WRITES: a ledger `updated_at` bump plus a notify event per look), while the
> throttle below — a PROCESS variable — could never fire, because every CLI
> invocation is a fresh process. The trigger is now explicit:
> `gopi task list --sweep`, throttled by a PERSISTED ledger-side stamp
> (`<gopi-dir>/task-list-sweep.stamp`). The INTENT recorded here — a killed
> one-shot runner's rows must surface on a daemon-less node — is unchanged and
> still covered by a test; only the trigger moved off the incidental glance.
>
> **SUPERSEDED AGAIN 2026-11-05** (commit `e4521fad`): that explicit trigger was
> deleted end to end with the sweep, its persisted stamp and its re-arm UPDATE —
> no process reports another process's health any more. `gopi task list --sweep`
> answers `unknown flag --sweep` and exits 1; the surviving read-out is plain
> `gopi task list`, which resolves each row's holder per row and reports it
> without acting. The intent above ("a killed one-shot runner's rows must
> surface") is what the read-out now serves.

- `cmd/agent/cli_tasks.go`: `maybeTaskListSweep()` — runs `staleSweep()` (the existing agent.DetectStaleTasks with zombie evidence chain) at most once per `taskListSweepThrottleMin` (10min, Rule 1 named const; 0 = disabled), wired before `tools.TaskList` in the list verb.
- Idempotency: the ledger-side `updated_at < staleSince` guard + post-notify bump (the re-arm from 941b40c) prevents re-fire spam across repeated reads; the process clock var keeps the shape testable.
- The human (or any cron/one-shot `gopi task list --status in_progress` line) becomes the trigger — zero new process, zero service.

Not done (deliberately): a `task sweep-stale` verb (redundant — the list read now sweeps); a mirror-expiry mechanism for dead-source mirror rows (needs a source-terminal heartbeat design; the read-side sweep at least surfaces them with zombie evidence so PA sees them on the very next look); resurrecting TaskIngest (retired by audit — memsync is the sync path).

## Regression test
`cmd/agent/cli_tasks_list_sweep_test.go` — `TestTaskListSweepReportsZombieOnRead`: replicates the incident (in_progress row, dead claim pid via spawn-kill-wait, backdated timestamps, hermetic notify env) → asserts ONE `task list` read delivers the stale event, and a throttled second read does not re-fire.

## Verification
- Build: `go build ./...` OK (worktree + bin rebuild)
- Tests: `go test ./cmd/agent/ -count=1` PASS (incl. new test + serve-stale suite); `./internal/agent/` PASS; `./internal/tools/` PASS except 2 pre-existing flaky tests on master (`TestMemorySearchNonBlocking` — reproduces on clean master; `TestJobNoNoticeOnKill` passes in isolation ×3)
- E2E sandbox: seeded zombie row in a scratch DB → `gopi task list --status in_progress` printed `[stale] reported 1 stale in_progress row(s)` and delivered the notify JSON (`"event":"stale"`, zombie evidence attached)
- Commit: `019c464` on `feat/hp-zombie-closeout`
