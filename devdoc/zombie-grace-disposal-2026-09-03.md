<!-- doc-class: historical -->

> **RETIRED 2026-10-26 — this page describes machinery that NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left exactly where it is. Read this page as history, not as
> instructions: any command below that names `sweep-zombies` exits 1 with a pointer.
> The current behaviour is `devdoc/zombie-sweep-removed.md`.

# Zombie Task Disposal with Grace Periods (zombie-grace-disposal)

Date: 2026-09-03. Branch: `feat/zombie-grace-disposal`.
Epic: parameterized max-run + heartbeat-stall detection; the SERVER may kill
and mark failed. nick's design 2026-09-02.

## Design intent (nick, verbatim)

> We should have a grace period for dead process. Say, 1.5 hour for any task
> running max hours, or like current one: if any process doesn't update
> toolcall for more than 0.5 hour, these are considered zombie and gopi
> server has rights to kill and mark task failed. These numbers should be
> env var AND config file setting AND command line parameter (our
> convention).

The pre-existing machinery (M4.5/M4.8, merged before this epic):
- `DetectStaleTasks` (internal/agent + internal/tools) already detects
  in_progress rows with a stale `last_toolcall_at` (created_at fallback) and
  fires event=stale through the GOPI_TASK_NOTIFY pipeline.
- Stale events only NOTIFY — nothing killed the zombie process or marked the
  row failed. Rows accumulated until a human cancelled them (5+ zombie rows
  cancelled manually in one day, 2026-09-02).
- The notify target was broken (serve restarted without GOPI_TASK_NOTIFY_CMD
  since 8/30 — task-events.jsonl last write 8/30 12:01).

## What this task ships (stage 1 — detection + manual disposal)

1. **Parameterized grace periods** (three-layer convention CLI > env >
   settings, Rule 2 — each knob read in exactly one place):

   | knob            | CLI flag              | env var                    | settings.json key   | default        |
   |-----------------|-----------------------|----------------------------|---------------------|----------------|
   | heartbeat-stall | `--stall-sec` (sweep) | `GOPI_TASK_STALL_SEC`      | `task_stall_sec`    | 1800 (30 min)  |
   | max-run backstop| `--max-run` (sweep)   | `GOPI_TASK_MAX_RUN_SEC`    | `task_max_run_sec`  | 0 (disabled)   |

   - **heartbeat-stall** is the PRIMARY signal: a live Bond touches
     `last_toolcall_at` on every tool batch; a stuck/dead process does not.
   - **max-run** is a BACKSTOP: an in_progress row alive longer than the cap
     is flagged EVEN IF the heartbeat somehow keeps ticking (belt and
     braces). Default 0 = disabled — legitimate long tasks exist
     (reviews/builds); operators opt in.
   - The three layers resolve per knob: the `sweep-zombies` verb's CLI flags
     (`--stall-sec`/`--max-run`) are the CLI layer; env and settings are
     the other two. The serve tick (a long-running daemon, stage 1 =
     notify-only) resolves its detection threshold from env > settings >
     default — serve takes NO CLI grace flags (consistent with how the
     notify config reaches serve via env/settings only).
   - Legacy `GOPI_TASK_STALE_SEC` (the M4.5 env spelling) still drives the
     stall knob when the new name is unset (backward compatibility).
   - Resolver: `config.ResolveTaskGracePeriods(cliStall, cliMaxRun)` in
     internal/config/config.go — ONE reader per knob (Rule 2).
     Precedence: CLI > env > settings > default. Negative = error; explicit
     env/settings 0 = disabled.

2. **`gopi task sweep-zombies` CLI verb** (stage-1 disposal surface):
   ```
   gopi task sweep-zombies [--dry-run] [--kill] [--stall-sec N] [--max-run N] [--db PATH]
   ```
   - Scans in_progress rows against the resolved graces. Each candidate runs
     the three-evidence chain (heartbeat stop + claimed pid signal-0 + session
     JSONL mtime — internal/tools/taskzombie.go) → verdict confirmed/suspected.
   - Default = report-only (stage-1 posture: PA observes kill+resume viability
     before the serve tick auto-disposes in stage 2). Report lists each row:
     task_id, confidence, reason (stalled|max-run), evidence note.
   - `--kill` marks CONFIRMED zombies failed with a zombie reason in the
     task_events trail (failed, NOT cancelled — audit keeps the verdict and
     the queue can re-issue). Suspected-only candidates are NEVER killed by an
     automated sweep. A live claimed pid VETOES disposal (false positives
     impossible for a live task). `--dry-run` forces report-only even with
     --kill (the explicit "observe, don't dispose" spelling).
   - `--stall-sec`/`--max-run` are CLI overrides of the resolved graces.
   - Core: `tools.SweepZombies(tools.SweepZombiesOptions{...})` in
     internal/tools/taskzombie_sweep.go.

3. **Serve tick stays notify-only in stage 1.** The unconditional serve-pass
   sweep (`serveStaleSweep` → `agent.DetectStaleTasks`) now resolves its
   threshold through the config resolver (GOPI_TASK_STALL_SEC /
   settings task_stall_sec). It reports stale events; it does NOT kill.
   Stage 2 (after PA's observation) flips the serve tick to auto-dispose
   (kill + mark failed) using the same graces.

## Stage-1 / Stage-2 split

nick: "I suggest next time, we observe by PA to see if manual kill and
resume can work and then decide."

- **Stage 1 (this task)**: full detection + notify (restored delivery) +
  `gopi task sweep-zombies` verb (report / --dry-run / --kill). PA runs the
  verb manually, observes kill+resume viability over the next days.
- **Stage 2 (follow-up)**: flip the serve tick to auto-dispose (kill+mark
  failed) with the grace params, once manual observation proves kill+resume
  is sound.

Observation plan (stage 1 → stage 2 gate):
1. Let zombies accumulate naturally (or seed one via a kill -9'd `task run`).
2. Run `gopi task sweep-zombies --dry-run` — confirm the report lists the
   row with verdict confirmed + reason stalled/max-run, and the stale event
   lands in the notify target.
3. Run `gopi task sweep-zombies --kill` — confirm the row becomes failed with
   the zombie reason; verify the queue can re-issue (a failed row's work is
   re-submitted as a new pending row — the append-only reissue path).
4. Confirm a LIVE task (recent toolcall, or a live claimed pid) is never
   touched by either invocation.
5. After several successful manual kill+resume cycles → PA approves stage 2:
   serve tick auto-disposal with the same graces.

## Notify chain (restored + launch recipe)

The notify target is the operator's task-events.jsonl. Two configuration
layers, both live today:

- **settings.json "notify" block** (DURABLE — survives serve restarts):
  ```json
  "notify": { "cmd": "cat >> /home/user/.gopi/notify/task-events.jsonl",
              "events": "claimed,in_progress,completed,failed,stale",
              "async": false }
  ```
  Note: "stale" is NOT in the default events set
  (`claimed,in_progress,completed,failed`) — it must be added explicitly
  (or "all") or stale/zombie events are filtered out.
- **env vars** (per-process override):
  ```
  GOPI_TASK_NOTIFY_CMD='cat >> /home/user/.gopi/notify/task-events.jsonl'
  GOPI_TASK_NOTIFY_EVENTS='stale,claimed,in_progress,completed,failed'
  ```

Serve launch recipe (the running daemon's shape — MUST include the notify
env or rely on the settings block):
```bash
cd ~/work/GoPi && mkdir -p ~/.gopi/notify && \
GOPI_TASK_NOTIFY_CMD='cat >> ~/.gopi/notify/task-events.jsonl' \
GOPI_TASK_NOTIFY_EVENTS='stale,claimed,in_progress,completed,failed' \
./bin/gopi task serve --every 5m --skip-tag pa-queue 2>&1
```
PA reads ~/.gopi/notify/task-events.jsonl (a webhook to hermes stays
optional). The durable settings.json "notify" block is the fix for the
8/30-9/2 incident (serve restart without the env dropped delivery) — that
settings-layer resolution lives in the in-flight feat/task-notify-settings
branch and is NOT duplicated here.

## Verification

- `go build ./...` : OK
- `go test ./internal/tools/ -run TestSweepZombies` : 8/8 PASS
  (dry-run reports confirmed; --kill marks failed + zombie reason in
  task_events; live row untouched; live pid vetoes; suspected never killed;
  max-run backstop flags a recent-heartbeat row; no-grace no-op; stale
  event notifies).
- `go test ./internal/config/ -run TestResolveGracePeriods` : 5/5 PASS
  (defaults; CLI>env>settings precedence; legacy GOPI_TASK_STALE_SEC;
  negatives rejected; explicit 0 disables).
- Existing zombie/evidence tests still green (internal/tools stale tests).

## Files

- internal/config/config.go — Config fields TaskStallSec/TaskMaxRunSec +
  ResolveTaskGracePeriods (Rule 2 single reader) + legacy alias.
- internal/config/settings.go — EnvVars catalog entries (docs/help binding).
- internal/config/task_grace_test.go — resolver tests.
- internal/agent/taskheartbeat.go — stale threshold now via config resolver.
- internal/tools/taskzombie_sweep.go — SweepZombies (detect + dispose).
- internal/tools/taskzombie_sweep_test.go — sweep regression tests.
- cmd/agent/cli_tasks.go — sweep-zombies verb (parse/dispatch/report),
  usage text.
- devdoc/zombie-grace-disposal-2026-09-03.md — this document.
