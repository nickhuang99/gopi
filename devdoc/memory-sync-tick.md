<!-- doc-class: historical -->
# The resident memory-sync tick (phase 3 of the memory-asset sync)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **RETIRED 2026-10-25.** This tick no longer exists. Each fired run blocked a
> worker round for 315–534s (measured in `/home/user/.gopi/logs/worker.log`), so the worker
> round no longer syncs anything; `gopi memory sync --peer P --push` is a command an
> operator runs by hand. Read this page for the reasoning that produced the tick,
> not for how gopi behaves: the retirement, what survived it and the hand-run
> recipe are in [`memory-sync-tick-retired.md`](memory-sync-tick-retired.md).

**Task**: `01a0daf3-2e1b-77a6-9069-65a5c4967afd` · **Branch**: `task/01a0daf32e1b77a6906965a5c4967afd`

## The defect this closes

Phase 1 gave the knowledge base a live ssh sync (`gopi memory sync --peer X`),
phase 2 gave it a foreground loop (`--push --every 5m`). Both are things a
**human** starts. Phase 2's own file header states the boundary it stopped at —
*"it is wired into NO worker/merger tick"* — and that boundary was the defect in
practice: a distilled row written on `peerbox` reached `hp` only if the operator
typed the verb, and two hosts converged only for as long as someone remembered to
keep a terminal loop alive. Convergence was a function of operator attention, not
of the hosts being alive.

Phase 3 makes it a function of the hosts being alive: the resident worker's round
— the process that already carries the lifecycle sweep, the scratch sweep and the
merge tick — now carries one more leg.

## Design decisions, and why each went the way it did

### Ride the existing tick (choice 3 of 3)

| option | why not |
|---|---|
| the `--every` foreground loop (phase 2) | holds a terminal; one process per host that must be restarted by hand after every reboot; nothing in the worker knows whether it is alive. **It stays exactly as it is** — it is the right shape for an operator watching a convergence, which is a different job |
| a host scheduler (cron / systemd `--user` timer) | a second residency model (unit + installed copy + enable step + log) that no in-tree code owns and no test can exercise — the same rejection `merge_tick.go` records for the merge pass |
| **the resident `gopi worker` tick** | chosen: the process already alive on this host, already carrying three throttled legs, adding zero resident processes and zero new mechanisms, and ticking whether the task queue is busy or drained (so it is independent of LLM-task traffic) |

### A CHILD process, not an in-process call

`tools.MemoryAssetsPeerSessionMode` could have been called directly, and that
would have been wrong for two independent reasons:

1. **The lock's lifetime.** The sync's single-instance guard is a flock on the
   open file description — process exit is what releases it (crash, SIGKILL
   included). In-process, the lock's lifetime becomes the *worker's* (days).
2. **The round's blast radius.** An ssh leg and a peer's ledger write would run
   inside the worker's round, where a hang or a slow peer is the worker's problem.
   As a child, the round only waits for an exit status.

The child's **stdout/stderr are inherited** (`cmd.Stdout = os.Stdout`), so its
own report — `sync hp: mode=push pushed=… pull{imported=…}`, or *nothing at all*
on a converged round — lands in the stream `attachHostLifecycleLog` already
mirrors into `<gopi dir>/logs/worker.log`. The tick's own lines are the pointer
to it, and they name the file.

### The zero clock is NOT DUE (the property that makes this safe)

`memorySyncTickLastRun`'s zero value means "this process just started", and the
tick **stamps** instead of firing. This is sharper here than for the merge tick:
a first-tick firing driver would open an ssh session to every declared peer
seconds after every worker start — and the unit/E2E suite spawns workers, so the
suite would contact real peers and **write to their ledgers**. The first
opportunity is one full interval after the worker came up; a process that exits
before then syncs nothing.

### One `--push` round PER declared peer — never a pull, and never `--every`

`--push` is in the argv, not left to the child's default: an unattended leg must
never PULL rows into this store (a write nobody asked for; `--pull` is the
direction an *operator* chooses when they want it).

**The peer is spelled out, one child per declared peer** — and this is the one
place where measurement changed the design (Rule 31 finding #1, below). The
plan was a single child with no `--peer`, on the reading that "omitting `--peer`
is exactly how the verb means every declared peer". Running it disproved that:
`gopi memory sync --push` with no `--peer` exits 1 with *"--peer <alias> is
required"*. The "all declared peers" semantics live in the periodic leg's target
resolver (`memorySyncLoopPeers`), reachable only through `--every` — and
`--every` is a foreground ENDLESS loop, which a resident tick must never spawn
(one never-returning child per interval). So the tick resolves the peer set
through the same single source the verb uses (`config.ResolveMemoryPeers`; this
file keeps no peer list of its own) and spawns one short-lived child per peer, in
the declared order.

Sequential, not parallel, deliberately: the round's cost is a **peer's** write
lock, so fanning out multiplies the load a peer sees for no gain, and one peer at
a time bounds the tick's own duration by exactly one peer's round.

A node with **no** declared peer is refused *at the tick*, with the settings key
named, rather than spawning a child that would only exit 1: a per-interval error
line for a topology the operator chose is noise, not information.

### One knob, one reader (Rule 2), and why not reuse phase 2's

`memory_sync_tick` (settings) / `GOPI_MEMORY_SYNC_TICK_MIN` (env), resolved by
one reader (`memorySyncTickInterval`), published by one function
(`config.PublishMemorySyncTickMin`) from the same pre-chain moment as the merge
tick's cadence (a group verb returns from `main` at the subcommand chain and
never reaches the agent path's config block). Default 30 minutes, in minutes;
`0`/negative is the off switch; a malformed value is **reported and falls back to
the default** (never a silent off).

**Deliberately NOT `GOPI_MEMORY_SYNC_EVERY_SEC`** (phase 2's knob), even though
both are "a memory-sync cadence". Phase 2's value paces a *foreground loop an
operator started and is watching*; this one paces a *resident tick nobody
started*. One value governing both would mean an operator slowing down the
resident tick also silently retimed a loop they had sized for another purpose.
Two lifecycles, two knobs.

**Why 30 and not the merge tick's 15**: the asset is a distilled lesson, which
nobody is blocked on — it is invisible until searched for, unlike a finished
branch sitting unmerged. And the round's cost is paid by a **peer**: one ssh leg
carrying this node's whole local corpus (measured 2026-09-26: 5209 rows / 15 MB,
~47.5s to apply) with the peer's ledger write-locked per batch. A faster cadence
spends a peer's write lock more often for no gain.

### Fail-soft, and NO retry (the constraint that stays a constraint)

Nothing the tick does can fail the host task: an unreachable peer, a broken ssh
config or a refused import costs one tick and one line (Rule 15 — reported, never
swallowed).

The tick **must not** retry a failed child, and the reason is a filed defect:
peer-side imports have failed with `SQLITE_BUSY` on the batched apply
(`internal/tools/memory_sync_assets.go` bounds the peer's write-lock window to
one batch, ~0.3 s), and that is tracked as task
`01a0daf2-d440-7578-9044-3ca502b4200b`. A retry loop here would convert a
reproducible busy failure into a slow success and hide the defect this tick
exists to surface. One due tick → **at most one child**, whatever its status; the
next opportunity is the next interval. `TestMemorySyncTickDoesNotRetryAFailedRun`
encodes that as a test rather than a comment.

## The wiring

One line in the worker's round, in the same `if !once {` guard the scratch sweep
and the merge tick use:

```go
if !once {
        memorySyncTickSweep(workerRoleLabel)
}
```

`--once` is the unit/E2E seam; a test worker must not contact real peers.
`TestMemorySyncTickIsWiredIntoTheWorkerRound` reads the round's source, so
*deleting the call site* fails the build's tests rather than silently returning
convergence to "someone remembered to type the verb".

## Verification (what was run, not what was written)

* **Unit** — `go test ./cmd/agent/ -run 'MemorySyncTick|PublishMemorySyncTickMin'`
  the cadence's four readings; the zero-clock property (not due at *any* instant,
  including +24 h); due exactly one interval after the stamp; throttled inside the
  interval; the argv is exactly `[memory sync --push]` with `argv[0]` in
  `topLevelCommandNames` and `--pull/--every/--serve-assets/--peer` absent; the
  off switch; no declared peer → no spawn + the settings key named; status
  reported verbatim; no retry within a tick; the publisher's precedence and its
  negative-value report; the wiring gate.
* **The real surface (Rule 31)** — a real `bin/gopi worker` started with
  `GOPI_MEMORY_SYNC_TICK_MIN=1`, a pinned private home/settings, and a fake `ssh`
  shim mapping peer `hp` to a second database. See `## Verification` in the task
  report for the exact invocation, the worker's own log lines, and the peer's
  row-count change.

## Traps hit while building this (for the next agent)

1. **`append(verbPath, flag)` aliasing** — the first cut built the argv as
   `append(memorySyncTickVerbPath(), memorySyncDirectionFlag)`, which appends into
   the returned slice's spare capacity. The test caught it immediately
   (`["memory" "sync" "--push" "--push"]`). The verb path now **contains** the
   direction flag; nothing appends to it.
2. **`argv[0]` must be a dispatched verb.** `merge_tick.go`'s header records the
   measured version of this: passing the operator-facing LABEL ("gopi merge") to
   `exec` asked the binary for a subcommand named that, and the child printed the
   top-level usage while the tick "looked like" it had driven a merge. The test
   asserts `argv[0] ∈ topLevelCommandNames` for the same reason, and asserts the
   label (`memorySyncProg`) is **absent** from the argv.
3. **A worker started from inside an agent session refuses to start**
   (`GOPI_AGENT_SESSION` inherited) — `exit 3`, with the override named in the
   refusal. For the live check: `GOPI_ALLOW_AGENT_SPAWN=1`.
4. **Rule 1b on the new words**: `memorySyncVerb` / `memorySyncDirectionFlag` /
   `memorySyncPushFlagKey` are constants in `cli_memory_sync.go` (the file that
   owns the surface), referenced by the dispatcher, the flag tables and the tick —
   not retyped.
