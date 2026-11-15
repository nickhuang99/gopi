<!-- doc-class: normative -->
# `gopi worker --once` — what it is for, and whether it is redundant with `gopi task run next`

> **CORRECTED 2026-10-29 (epic `01a186d6`).** The worker round no longer carries
> a peer leg: `workerSyncPeers()` (the per-round db-sync) and
> `workerTakeOverAttempt()` (the peer-row transfer) are DELETED from
> `cmd/agent/cli_tasks_worker.go`. Statements below that describe either of
> them as the round's own behaviour describe the tree BEFORE that epic. What
> the round does today: `devdoc/worker-round-audit.md`.


<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **SUPERSEDED IN PART (row 7 · 2026-09-17, epic `child-stream-task-output`
> plus `worker-foreground-console`)** — see
> `devdoc/child-stream-task-output.md` and
> `devdoc/worker-foreground-console.md`. The *principle* in row 7 below (a
> task's output belongs in its own session directory, reachable from the task
> id) is kept in full and is now unconditional. The *mechanism* is not: the
> parent-side `os.Stdout`/`os.Stderr` re-point at CLAIM time
> (`workerAttachOutputLog`, `cli_tasks_worker_output.go`) was DELETED — the
> destination is set on the executor CHILD at the shared spawn seam
> (`runTaskRunOnce`), for every spawn path. Row 7's "the wrapper's own stream
> only" for `task run next` is also no longer true: a manual `task run` writes
> the SAME `<session-dir>/output.log` as a worker round. And the claim in rows
> 7/9 that the round's lines are only in the diverted log is superseded too —
> a NON-daemon spawner now also TEEs the child's stream to its own console. The
> rest of this document (rows 1–6, 8, 10–13) is unaffected. Kept for the
> reasoning that produced the destination.


**Question answered** (task briefing): *"gopi worker allows `--once`, which seems
to coincide with `gopi task run next`. What is the purpose of running the worker
once? Is it really necessary?"*

**Verdict in one line:** `--once` is **not** a second spelling of `task run
next` — it is the *one-shot face of the worker loop*, i.e. it keeps every
dispatch-side guarantee (loop, lifecycle passes, quarantine/anti-misclaim tag
filter, per-round db-sync, worker provenance, drain-is-success exit contract)
and only drops *waiting*. It is necessary and load-bearing in three places:
the **unit/E2E seam**, the **operator/MCP drain script**, and the **`--daemon
--once` detached bounded drain**. `task run next` is a *different verb with a
different contract* — it claims exactly one row, applies no quarantine filter,
does no lifecycle/db-sync pass, and **exits 1 on a failed row**.

Branch: `feat/worker-once-purpose` (worktree `~/workspace/gopi-worker-once`,
base master `791d81cc`).
All claims below were checked against master's code and **re-verified by running
the freshly built binary** (evidence in §6).

---

## 1. What `--once` actually is (code)

`--once` is one of the two **loop faces** of `runTaskWorkerLoop`:

| site | meaning |
|---|---|
| `cmd/agent/main.go:37` | `workerFlagOnce = "--once"` — the flag's single spelling |
| `cmd/agent/cli_worker_daemon.go:125` | `scanWorkerArgs`: `case a == workerFlagOnce: wa.once = true` |
| `cmd/agent/cli_tasks_worker.go:179` | `runTaskWorker(once bool)` |
| `cmd/agent/cli_tasks_worker.go:350` | `runTaskWorkerLoop(once bool)` — the loop itself |
| `cmd/agent/cli_tasks_worker.go:235` | the `--once` banner (a different start line from the long-running face) |
| `cmd/agent/cli_tasks_worker.go:318-327` | **the drain-exit**: queue empty → print `queue drained — exiting` → *return* (exit 0) |
| `cmd/agent/cli_tasks_worker.go:285` | `if !once { scratchGCSweep(...) }` — the only pass `--once` **skips** |
| `cmd/agent/cli_worker_daemon.go:82` | `workerDaemonArgs = []string{workerFlagOnce, tools.WorkerLogRootFlag}` — `--once` **survives the `--daemon` re-exec** |
| `cmd/agent/cli_tasks_worker_output.go:151` | `workerOnceFace` → the round's claim line is **also** echoed to the launcher's stdout |

The loop body is shared, byte for byte, between the two faces — `--once` differs
**only** at the drained-queue verdict. The long-running face instead sleeps
`GOPI_WORKER_IDLE_SEC` (default 30s) and self-exits after
`GOPI_WORKER_IDLE_EXITS` (default 3) *consecutive* drained rounds
(`cli_tasks_worker.go:336-362`). So `--once` = "same loop, but a drained queue
ends it immediately".

Crucially, `--once` is **not** "run one task":

```
$ gopi worker --once            # 3 pending rows queued
worker: loop start --once (claim → run → must-succeed push → db-sync → next; exit on drained queue)
worker: claimed <id1> / claimed <id2> / claimed <id3>
worker: queue drained — exiting      # exit 0
```

Three rows, three rounds, one process. `--once` is *drain-once*, not
*claim-once*.

## 2. What `gopi task run next` actually is (code)

| site | meaning |
|---|---|
| `cmd/agent/cli_tasks.go:354-361` | usage: `gopi task run [<task_id>\|next] … "next" is the canonical atomic claim-and-run entry` |
| `cmd/agent/cli_tasks.go:1333` | `runTaskRunOnce(opts)` — the single-runner core |
| `cmd/agent/cli_tasks.go:1353-1379` | `next` → `tools.TaskNextFiltered(opts.as, opts.excludeSession, opts.skipTags)` → **one** row → `claimed: <id>` |
| `cmd/agent/cli_tasks.go:1836-1851` | `runTaskRun`: on error → `os.Exit(1)` (exit-on-error wrapper) |
| `cmd/agent/cli_tasks.go:1431` | a `staleSweep()` fires on the run path (report only) |

`task run next` claims **exactly one** row and returns. It is the manual /
one-shot executor spelling.

## 3. Side-by-side — what the worker round adds on top of the same core

Both faces funnel through the **same** `runTaskRunOnce` core (claim → in_progress
→ child `gopi` run → terminal writeback → child close-out chain). The worker
wraps that core; the table lists everything that exists **only** on the worker
side.

| # | capability | `gopi worker --once` | `gopi task run next` | evidence |
|---|---|---|---|---|
| 1 | loop over the queue | **drains** the whole queue, then exits 0 | one row, then done | `cli_tasks_worker.go:287-330` vs `cli_tasks.go:1353-1379` |
| 2 | lifecycle pass per round (stale report + **confirmed-zombie auto-dispose** + **finish-cleanup sweep**) | **yes**, every round (10min throttle) | no (only the report-only `staleSweep` on the run path) | `cli_tasks_worker.go:479` → `runTaskLifecycleTick` → `task_lifecycle_tick.go:183,230-252`; `cli_tasks.go:1431` |
| 3 | **per-round db-sync** to every peer (pull-then-push) | **yes** | no db-sync leg | `cli_tasks_worker.go:646` `workerSyncPeers()` → `cli_tasks_worker.go:665-682` |
| 4 | **quarantine / anti-misclaim tag filter** — never claims `pa-queue` or `quiet` rows | **yes**, `workerExcludedTags` | **no** — it *will* claim a `quiet`-tagged row | `cli_tasks_worker.go:143` `workerExcludedTags = "pa-queue," + tools.QuarantineTagSet`; used at `:498` and `:600`. `task run next` passes `opts.skipTags`, which is **always empty**: `--skip-tag` is not a member of the
`run` verb's flags (`taskFlagMember` lists it only under `case taskverbs.Serve, taskverbs.Merger`,
`cli_tasks.go:1155-1160`), so `gopi task run next --skip-tag quiet` is refused with *unknown flag* (measured).
The manual verb therefore has **no** way to exclude quarantine tags (the flag is simply not a member of the verb's flag set — an argument-set fact, not a runtime one). **Measured**: a `quiet`-tagged pending row is claimed by `task run next` but *not* by `worker --once` (§6.G / §6.F) |
| 5 | **claim stamp = the long-lived worker PID** → the dead-holder reclaim arm can never see it, so the producer releases its own worktrees at round end (`[own-release]`) | **yes** | the claim PID is a per-invocation process; ownership/release is inferred by the sweep | `cli_tasks_worker.go:127-153` `workerRoundOwnWorktrees` / `workerRoundOwnReleaseFmt` |
| 6 | **run provenance**: `run_method=worker` + the worker-executor tag; `run_cmd` recorded **verbatim** (re-runnable) | **yes** | `run_method=manual`, reconstructed cmdline | `cli_tasks.go:1795-1827` `runMethodFor`; `internal/tools/memory_sync_delta.go:366` `TaskRunWritebackTagged`; **measured**: `task list --all` prints `[via manual]` for §6.H |
| 7 | **output attribution** — the round's stdout/stderr is re-pointed into `<session-dir>/output.log`; a claim whose dir cannot be resolved **refuses** the round | **yes** | the wrapper's own stream only | `cli_tasks_worker.go:545` `workerAttachOutputLog`; `cmd/agent/cli_tasks_worker_output.go:70-155` |
| 8 | **exit contract on a failed row** | **exit 0** (drain is success; the row already landed `failed`) | **exit 1** | `cli_tasks_worker.go:600-602` (`run … failed (continuing)`); `cli_tasks.go:1844-1850` (`os.Exit(1)`). **Measured**: identical hermetic failing child → worker `--once` exit **0**, `task run next` exit **1** (§6.C/§6.D) |
| 9 | **`--once` announce to the launcher** (the round's lines are duplicated to the launcher's stdout even though the log was diverted) | **yes** | n/a | `cli_tasks_worker_output.go:145-155` |
| 10 | 1s **settle re-check** before the drained verdict (a submit racing the claim scan is caught) | **yes** | no (immediate `no pending task`) | `cli_tasks_worker.go:85-88, 314-317` |
| 11 | **peak-hour deferral per round** + in-flight **graceful drain** (`--stop`) | **yes** | start-time gate only | `cli_tasks_worker.go:427` (the drained-round idleStreak wait that replaced `workerPeakWait`, deleted with the peak guard by `bb0c4c1a`); `cli_tasks_worker.go:255-303` drain requests |
| 12 | `--daemon --once` (detach + bounded drain) | **yes** | n/a | `cli_worker_daemon.go:82` |
| 13 | scratch GC sweep | **skipped** on purpose (tests must not sweep real state) | not present | `cli_tasks_worker.go:279-286` |

What the two faces **share** (so it is *not* a reason to prefer either): the
claim CAS (`tools.TaskNextFiltered`), the two-phase claim-lock protocol, the
repo guardrail, the child `gopi` run, the terminal writeback with the
must-succeed push (`internal/agent/taskregister.go:526`), and the
finish-cleanup delivery chain on the wrapper row (`cli_tasks.go:1706-1730` (`TaskFinishCleanupSessionID` at `:1710`, own-release loop at `:1728`)).

## 4. So why does `--once` exist? (three real, non-overlapping jobs)

1. **The unit/E2E seam.** The worker loop is otherwise unbounded ("never
   returns"); every hermetic test and acceptance rig needs a *bounded* worker
   that also *does the worker's work*. `--once` is that door: it runs the real
   loop and stops when the queue drains. **17 files** are built on it
   (measured 2026-09-13, `grep -rlE '"worker", *"--once"|workerVerbArgv|
   runTaskWorker\(true\)|workerFlagOnce' --include='*_test.go' cmd internal`
   plus the four shell rigs):

   - **13 Go test files** reference the in-process face
     (`runTaskWorker(true)` / `workerFlagOnce`, 15 sites) or the argv face
     (`[]string{"worker", "--once"}`, **8 sites in 4 files**:
     `cli_tasks_worker_recycle_e2e_test.go` ×3, `task_executor_repo_cwd_e2e_test.go`
     ×3, `task_session_cwd_e2e_test.go` ×1, `cli_tasks_worker_output_e2e_test.go` ×1)
     — e.g. `cli_tasks_worker_recycle_e2e_test.go` (the server-side
     worktree-recycle E2E needs *the worker path*, not `task run`),
     `task_session_cwd_e2e_test.go`, `task_executor_repo_cwd_e2e_test.go`,
     `cli_tasks_worker_output_e2e_test.go` (output attribution is worker-only),
     `agent_spawn_guard_test.go` / `agent_spawn_guard_env_isolation_test.go`
     (the guard's own live-binary probe), `worker_daemon_flag_e2e_test.go` W5
     (`--daemon --once` drains and exits), `singlenode_serve_test.go`,
     `cli_serve_test.go`, `cli_serve_stale_test.go`, `daemon_cgroup_e2e_test.go`,
     `worker_daemon_singleton_e2e_test.go`, `cli_tasks_worker_unknown_arg_test.go`.
   - **4 shell acceptance rigs**: `scripts/acceptance-self-release.sh` (rounds r1/r2,
     status-checked), `-stop.sh`, `acceptance-zombie-auto-dispose.sh`,
     `scripts/peer-worker/hp-night-legacy-audit.sh`.
   - separate but adjacent: `internal/testutil/hp_worker_launchers_test.go` and
     `worker_ownership_gate_test.go` pin the *launcher argv shapes* that carry
     `--once` into the spawned worker (they assert on the logged argv, they do
     not themselves run the flag), and `cli_tasks_worker_idle_exit_test.go`
     documents the contract by contrast ("WITHOUT `--once` must not wait
     forever").

   `task run next` **cannot stand in** for any of these: no lifecycle pass, no
   own-release claim stamp, no output attribution, no quarantine filter, and a
   different exit contract.
2. **The operator drain scripts.** `scripts/peer-worker/hp-night-legacy-audit.sh:46`
   runs `gopi worker --once` in a *serial* loop — "one batch, then the next"
   — and checks the exit status; `devdoc/peer-inline-launchers-20260912.md:120`
   explicitly calls its `--once` round "a deliberate bounded drain".
3. **Detached bounded drain.** `gopi worker --daemon --once` = "put a worker in
   its own session, let it drain whatever is queued *now*, and do not leave a
   process behind" (`cli_worker_daemon.go:82` keeps `--once` across the re-exec;
   `worker_daemon_flag_e2e_test.go:484` pins it).

Plus one design-level reason: `--once` is the **backward-compatible M4.20-D
drain-exit semantics** kept alive after the worker stopped being a "drain and
die" tool by default (`devdoc/serve-worker-split-20260903.md:37`,
`devdoc/m420-worker-loop.md` §M4.20-D, `cmd/agent/cli_tasks.go`). Removing
it would break every script/rig written against that contract.

## 5. Is it *necessary*? — remove/keep analysis

**Keep.** Deleting `--once` would:

- turn 8 test files + 4 acceptance scripts red (or force each to grow its own
  drain logic: `GOPI_WORKER_IDLE_SEC=1 GOPI_WORKER_IDLE_EXITS=1` is the closest
  equivalent and is **not** the same contract — it still sleeps and still can
  pick up a row submitted during the wait);
- strand `--daemon --once` (the only way to get a detached, self-terminating
  drain);
- break `hp-night-legacy-audit.sh`'s status-checked serial rounds.

**Partial redundancy that IS real** (worth recording, not worth "fixing" by
deletion):

- If a caller only wants "run exactly one row, now, and tell me if it failed",
  `gopi task run next` is the *narrower and cheaper* tool (12 ms vs ~1.05 s on
  a drained queue, measured §6.B) — but it is a *different* contract: it exits 1
  on a failed row and has none of items 2/3/4/5/7 of §3.
- The genuinely duplicated part is the *loop*: `while gopi task run next; do :;
  done` resembles `worker --once` in the happy path, and **diverges on every
  other axis** (exit codes per row, no lifecycle pass, no db-sync, a fresh
  claim PID each round so the producer-side release must be inferred by the
  sweep, no `pa-queue`/`quiet` exclusion, no output attribution). So the
  duplication is *surface-level*, not semantic.

**No change is recommended.** `--once` is documented as the drain-exit face of
the loop (`cmd/agent/cli_tasks.go:488-496`, `internal/config/help.go:432`), it is
the seam the whole worker test suite stands on, and every guarantee it carries
is one the manual verb deliberately does not have.

## 6. Verification (real binary, sandboxed `GOPI_HOME`, hermetic child)

Binary: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` in the worktree →
`bin/gopi version` = `dev`. Build of the whole tree: `go build ./...` → OK.
Sandbox = fresh `GOPI_HOME` per case under `/dev/shm` (Rule 22), child runs made
hermetic with `GOPI_TASK_CHILD_FAIL_TEST=1` (no model call); the live-binary
launch guard was passed with `GOPI_ALLOW_AGENT_SPAWN=1` in the sandbox only.

**Re-verification of the flag-membership row (2026-09-13, same binary)**: the
`--skip-tag` probe was re-run end-to-end — `gopi task run next --skip-tag quiet`
→ `unknown flag --skip-tag` (§6.J), and the loop-argv export re-read at its
definition (§6.K). `go build ./...` on the branch: OK.

| # | command (sandbox `GOPI_HOME`) | observed |
|---|---|---|
| A | `gopi task run next` (empty queue) | `no pending task`, exit **0**, ~12 ms |
| B | `gopi worker --once` (empty queue) | banner + `[lifecycle] …` + `worker: queue drained — exiting`, exit **0**, ~1.05 s (the 1s settle re-check) |
| C | 1 pending + `GOPI_TASK_CHILD_FAIL_TEST=1 gopi worker --once` | `worker: run <id> failed (continuing): … FAILED`, then `worker: queue drained — exiting`, exit **0** |
| D | 1 pending + `GOPI_TASK_CHILD_FAIL_TEST=1 gopi task run next` | `gopi task run: task <id> FAILED (child: synthetic child failure …)`, exit **1** |
| E | 3 pending + `gopi worker --once` | `claimed <id1>`/`<id2>`/`<id3>` (3 rounds, one process) + `[own-release]` per round, `queue drained — exiting`, exit 0; `task list --status pending` → *No tasks found* |
| F | 1 pending tagged `quiet` + `gopi worker --once` | `queue drained — exiting` **without claiming**; row still `pending` afterwards |
| G | 1 pending tagged `quiet` + `gopi task run next` | **`claimed: <id>`** — the manual verb has no quarantine filter |
| H | 1 pending + `gopi task run next` → `gopi task list --all` | row shows `[via manual]` (provenance differs by face) |
| I | plain `gopi worker`, drained, `GOPI_WORKER_IDLE_SEC=1 GOPI_WORKER_IDLE_EXITS=1` | `1 idle round(s) with no new task — exiting 0`, ~2 s — the long-running face's *approximation* of `--once`, not the same contract |
| J | `gopi task run next --skip-tag quiet` | **`gopi task run: unknown flag --skip-tag`** — the argument-set fact behind row G: `taskFlagMember` lists `skip-tag` only under `serve`/`merger` (`cli_tasks.go:1082-1160`; `run`'s case at `:1109-1112` = `db, as, exclude-session, report-to, repo, nodes`), so the manual verb can never filter quarantine tags. Re-measured 2026-09-13 against the same binary |
| K | `grep -n 'workerDaemonArgs = ' cli_worker_daemon.go` | `82: var workerDaemonArgs = []string{workerFlagOnce, tools.WorkerLogRootFlag}` — `--once` is re-exported into the detached child's argv, i.e. `--daemon --once` is a *bounded* detached drain (pinned by `worker_daemon_flag_e2e_test.go` W5) |

## 7. One-line answer for the operator

> `--once` is not a synonym for `task run next`: it is the worker loop with the
> waiting removed — same loop, same lifecycle passes, same quarantine filter,
> same db-sync and provenance, same "a drained queue is success" exit — and it is
> the seam the entire worker test/acceptance suite and the operator drain
> scripts are built on. Keep it. Use `task run next` when you want to run *one*
> row manually and be told (exit 1) if it failed.
