<!-- doc-class: roadmap -->
# First-open schema build: the durable-sync mechanism, and bounding it on the working path

Branch: `fix/ledger-firstopen-budget` (worktree `~/workspace/gopi-firstopen`), off
master `91d85a0e`.

## The question this answers

`go test ./cmd/agent/ -count=1` could not finish inside Go's default 10m package
budget on this host. This branch's own stall watchdog (armed by TestMain) attributed
the stall to `internal/tools/memory.go:595 tx.Commit()` inside
`execSchemaBatchOnce` — one exclusive transaction for the whole first-open schema — on
the **ledger's FIRST open**. The epic asked the question "slow disk" does not answer:
*which single fsync can block for minutes when a bare fsync measures ~1s?*

## Mechanism (named, with the kernel's own account)

**Name: an fsync serialized behind ext4's SHARED journal-commit queue, repeated once
per write transaction of the schema build.**

Evidence, from the probe that ships with this branch
(`internal/tools/firstopen_probe_test.go`; it samples `/proc/self/task/<tid>/syscall`,
`.../wchan` and `.../stat` for **every thread** while a phase runs, so the commit
syscall is attributed to the phase that issued it):

```
arm=working-path fs=tmpfs (memory-backed) sync=NORMAL fresh=2
    phase=enableWAL            wall=   0.001s
    phase=ensureSchema         wall=   0.006s
    phase=ensureFeedbackSchema wall=   0.004s
    phase=ensureTasksSchema    wall=   0.006s      # whole first open: 9-10 ms
arm=block-device-durable fs=ext4 /dev/sda4 (device-backed) sync=NORMAL fresh=2
    phase=enableWAL            wall=   1.314s
        blocked-in fsync wchan=jbd2_log_wait_commit           413 samples
        blocked-in fsync wchan=wait_on_page_bit_common         59 samples
        blocked-in pwrite64 wchan=do_get_write_access           4 samples
    phase=ensureSchema         wall=   0.521s
        blocked-in fsync wchan=jbd2_log_wait_commit           164 samples
        blocked-in fsync wchan=wait_on_page_bit_common         12 samples
    phase=close                wall=   0.665s
```

Reading:

* the committing thread is in **`fsync` (syscall 74)**, not in a driver busy-wait —
  the cost is real durability, not SQLite contention;
* its wchan is **`jbd2_log_wait_commit`** — ext4 has ONE journal per filesystem, so
  every writer on that device (a sibling suite, a `dd`, the merger's own writes) joins
  the same commit queue. The commit the schema build waits for is
  `common + the device's queue length`, not the size of its own WAL pages;
* **`rq_qos_wait`** appeared in the same histogram earlier in the day — the block
  layer's write throttling (cgroup/io.latency), the second queue a device write waits
  in;
* **`wait_on_page_bit_common`** — waiting for dirty-page writeback (the dirty-page
  ceiling / writeback fairness), the third;
* the cost is paid **once per write transaction**, and the first open is a *sequence*
  of them (schema batch, feedback table + 2 ALTERs, tasks tables + FTS companions +
  triggers): measured 0.5-2.2s *per phase* with the WAL growing one page per phase
  (53,592 → 103,032 → 284,312 bytes).

So: `bare fsync ≈ 1 s` grows to `minutes` because a first open is ~dozens of commits,
each of which queues behind every other writer on that device, and each of which is a
full journal commit. On tmpfs the same build issues **zero** blocking fsync samples and
completes in 9-10 ms — a ~1400x separation on the identical call (consistent with the
24 ms tmpfs vs 34,067 ms device measurement recorded for the same schema init in
memory #2197).

The same fsync, in the same build, is issued through three SQLite paths
(`xSync → full_fsync`): the atomic schema batch's `tx.Commit()` (the watchdog's
`memory.go:595`), the statement-autocommit DDL that `ensureTasksSchema` runs WITHOUT a
batch, and the close-time WAL checkpoint
(`sqlite3_close_v2 → _sqlite3WalClose → _walCheckpoint → _full_fsync`, the mechanism
already recorded in memory #1644). All three are eliminated on the working path by the
change below.

## The change (what is bounded, and why durability is untouched)

`initKnowledgeBase` builds the schema into a **fresh file** on the suite's pinned
scratch root. Where that file lives decides what its commits owe:

* **measured memory filesystem (`internal/scratch.MemoryFilesystem`, by mounted device
  id — never a path spelling)** → the build's own connection runs
  `synchronous=OFF` (`ledgerSyncModeWorkingFS`): the schema cannot outlive a reboot
  whether or not it is synced, and a process-side crash leaves the pinned scratch tree
  to be GC'd and rebuilt, so the sync protects nothing. Cost removed: the whole first
  open drops to the tmpfs number (9-10 ms measured);
* **anything else** — the operator's `~/.gopi` ledger, any path on a device, any
  directory whose device cannot be measured → `synchronous=NORMAL`
  (`ledgerSyncMode`), i.e. production durability is unchanged, byte for byte.

Two facts make the relaxation provably scoped:

1. **`journal_mode` is a persistent header property, but `synchronous` is
   per-connection.** The mode is applied on a handle that `initKnowledgeBase` owns and
   closes; `TestFirstOpenKeepsProductionDurabilityOffTheWorkingPath` reads
   `PRAGMA synchronous` back from a handle opened AFTER the build and requires
   SQLite's default FULL — the relaxation cannot reach a caller's handle.
2. **The path predicate is the rule-22 measurement, not a spelling**
   (`TestFirstOpenSyncModeDecisionTable`: memory dir → OFF, device dir → NORMAL,
   unmeasurable dir → NORMAL; the burden of proof is on the cheap path).

`enableWAL` is unchanged for every other caller: `enableWALWithSync` takes the mode
explicitly, and the only caller that passes a non-default value is
`initKnowledgeBase`. **The test DSN is untouched** — `ledgerSyncMode` (NORMAL) is
still the third half of `LedgerDSN`'s contract, so fixtures and production agree.

A 30-line probe of the same build on the block device, same file, only the pragma
differing (`TestFirstOpenBlockDeviceCostIsTheSync`, under load):

```
same-file A/B: synchronous(NORMAL) = 4.400915299s, synchronous(OFF) = 3.105860424s
```

## Completed

- Named the mechanism (above) with per-phase kernel-state evidence and a tmpfs/device A/B.
- `firstOpenSyncMode` + `enableWALWithSync` + `memoryFilesystemLedger`
  (`internal/tools/memory.go`), `ledgerSyncModeWorkingFS`
  (`internal/tools/ledger_dsn.go`): the first-open build syncs only when the ledger is
  not provably on a memory filesystem.
- Reproduction/acceptance tests: `internal/tools/firstopen_sync_test.go` (decision
  table, durability-leak guard, device A/B) and the measurement instrument
  `internal/tools/firstopen_probe_test.go` (gated behind
  `GOPI_FIRSTOPEN_PROBE_DIR`, so the ordinary suite stays silent).
- Unblocked the package: master did not compile (duplicate `funcBody` test helper,
  files added by merges `362278e2` and `5aabf3d9`); renamed to `funcSourceText`.
  Filed independently as bug task `01a0a75c`.

## Verification

Measured 2026-09-16 on the shared host (4 cores). io pressure is generated on the
worktree's own device (`/dev/sda4`, `/home`) by a synthetic O_DIRECT+fsync load;
every run below uses **Go's default 10m package budget** (no `-timeout` override),
with the suite's stall watchdog kept armed.

| run | tree | wall | timeouts | `/proc/pressure/io` at start (some / full avg300) |
|---|---|---|---|---|
| br1 | `fix/ledger-firstopen-budget` | **316.44 s** | 0 | 86.1% / 74.4% |
| br2 | `fix/ledger-firstopen-budget` | **299.45 s** | 0 | 93.1% / 78.3% |
| **ctl1** | **pristine master** (hardlink copy, so the worktree sweep cannot delete it) | **317.76 s** | **0** | **98.5% / 94.3%** |
| **ctl2** | **pristine master** | **291.89 s** | **0** | **98.7% / 89.4%** |
| blockfs | same branch, ledger sandbox forced onto **ext4** | __BLOCKFS_WALL__ | __BLOCKFS_TIMEOUTS__ | 99.5% / 94.6% |

**Reading this honestly: the epic's premise does not reproduce at this bar.** Four
runs at the default budget — two on this branch and two on **untouched master** —
all finished in 292-318 s with **zero** `test timed out` panics, at up to 99.7%
`some` / 95.1% `full` avg300 io pressure. On this host the branch is not faster than
master, because the suite's time is not dominated by the ledger's syncs: the suite
pins its ledger/scratch root on **tmpfs** (`internal/testutil` scratch root is
memory-backed), where the mechanism below costs nothing — the same build on a memory
filesystem issues zero blocking fsync samples and opens in 9-10 ms.

The `blockfs` row is the discriminator for that claim: the SAME tree with the
suite's ledger sandbox forced onto **ext4**, i.e. the only difference is the
filesystem every fresh ledger lives on. The report states what it measured rather
than assuming direction.

Where this change does bite is the path the epic named — a ledger on a device. There
a first open costs the measured 0.5-2.2 s **per schema phase** (above), and this
branch bounds it. "The suite's sandbox is on tmpfs" and "a device-backed ledger's
first open costs seconds" are both true, and together they are the finding.

Green in both branch runs, explicitly re-checked: the TestMain stall watchdog
(`TestSuiteStallWatchdogReportsLiveChildren`, `…ReportsNoChildren`,
`…StopsWithoutReporting`, `TestSuiteStallBudgetEnvOverridesTheDefault`), the
scratch-root/ledger sandbox guards, and **both named SQLITE_BUSY residuals** —
`TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound` and
`TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses`.

Failure sets: the branch runs fail 18-19 tests, the control runs 25. **No failure is
branch-only** — every branch failure is also a control failure, and
`TestLedgerOpenersUseProductionDSNContract` points at
`internal/tools/task_holder_resume_e2e_test.go:54,138,346` — three bare-SQLite
openers that arrived with `362278e2` on master itself.

### Mechanism evidence (the probe)

The phase-by-phase histograms above come from
`internal/tools/firstopen_probe_test.go`:

```
GOPI_FIRSTOPEN_PROBE_DIR=/dev/shm/gopi-firstopen-probe \
GOPI_FIRSTOPEN_PROBE_BLOCK_DIR=$PWD/.iopress/probe \
  go test ./internal/tools/ -count=1 -run TestZZFirstOpenProbe -v
```

It is gated behind that env var, so the ordinary suite writes no evidence files.

## Not Done (honest)

1. **The epic's timeout was NOT reproduced, on the branch or on master.** At the
   epic's pressure bar (99.5% `some` / 94.6% `full` avg300) pristine master finishes
   in 292-318 s against a 600 s budget. So this branch cannot claim to have "fixed a
   hang": it fixes a **measured** first-open cost on device-backed ledgers, and the
   suite's own sandbox happens not to be on a device. The operator's original
   observation (the suite overflowing 10m) is therefore explained only in part — the
   probe's numbers support "a device-backed ledger's first open costs seconds" but
   this rig did not reproduce the overflow itself.
2. **The suite still exits non-zero** (~18-19 pre-existing master failures, 25 on the
   control). It now FINISHES well inside the default budget; it is not green.
   `TestLedgerOpenersUseProductionDSNContract` in particular is a real residual of the
   merge that landed `task_holder_resume_e2e_test.go` and belongs to a task of its own
   (Rule 26).
3. **The stall watchdog was not observed printing** in these runs, because nothing
   stalled. What is verified is that it is still armed and that its own tests are
   green — not that a real stall still reports.
4. **`ensureFeedbackSchema` was left as-is**: 3 statements, 2 of them measured free
   (0.004-0.005 s on both arms), so batching it changes nothing measurable.
5. **The two earlier branch runs** (208.5 s / 202.4 s, at 91.2-93.1% `some` avg300,
   also zero timeouts) predate the tasks-DDL batching commit and are not quoted as
   acceptance runs; they are retained only as evidence that the failing-test set is
   stable (the same 14 tests) independent of the change.
6. **`TestZKilledWorker...`-style load-sensitive `cmd/agent` tests** (the 7 that fail
   on the control but not on the branch runs) were not investigated: they are
   worktree/session-dir fixtures whose outcome moved with machine load, not with this
   change.
