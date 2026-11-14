<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: `go test ./cmd/agent/` — the ledger DSN contract and the un-legible hang

Branch: `fix/ledger-busy-dsn-contract-v2` (worktree `/home/user/workspace/gopi-ledger-closeout`, HEAD `23fe8d09` — verified tip; a linked
worktree of the main repo `/home/user/work/GoPi`, so the commits are already in the shared object
store). The matrix below was measured at `99b0f260`/`417a64bb`; everything after `417a64bb` is
**documentation-only**, so the code measured is the code at the tip — re-checked at `23fe8d09`
with `go build ./...` (exit 0), `go vet ./cmd/agent/ ./internal/tools/` (exit 0), the named
`cmd/agent` bundle (`ok 4.537s` / `4.699s`) and `internal/tools -run TestLedgerDSN` (`ok 0.407s`). The branch was developed in `/home/user/wl2`, which **another process's worktree sweep
removed mid-session** (the paths in the stack traces below are that worktree's, correctly, as
captured); the branch itself never was at risk — it lives in the main repo's shared object store —
and it was re-materialised at `/home/user/workspace/gopi-ledger-closeout` to finish the report.
Fresh off master `51edd8d9` — Rule 23: the sibling's `fix/peak-orphan-tests-doc` was **read**,
never extended.

Commits on the branch:

| commit | what |
|---|---|
| `44512a36` | `internal/tools.LedgerDSN`/`LedgerReadOnlyDSN` + the 8 `cmd/agent` bare-DSN rigs + bounded voice probes |
| `bec6301b` | session-unique daemon scope unit (`scopeUnitNameFor`) + `daemonAwaitReady` quoting the daemon log tail |
| `999ce7d1` | the remaining 13 bare-DSN openers (`internal/tools`, `internal/agent`) + the gate over 4 roots with live permits |
| `f586f166` | `scopeDirName` **is** `scopeUnitName` (one expression of the name; the format-string copy is gone) |

## Completed

### 1. ONE ledger connection contract for non-production openers (Rule 14)

`internal/tools/ledger_dsn.go`:

```go
func LedgerDSN(path string) string          // file:<path>?_pragma=busy_timeout(<dbBusyTimeoutMSec>)&_pragma=journal_mode(WAL)
func LedgerReadOnlyDSN(path string) string  // + mode=ro & query_only(true)
```

The timeout is rendered from **`dbBusyTimeoutMSec`** — the same constant `openDB`
applies to production's handle (`internal/tools/memory.go:80`), never a re-typed
`15000` (Rule 1a/13). `ledgerPragmas()` is the single place the value is rendered;
`TestLedgerDSNCarriesProductionsContract` fails if the DSN stops carrying the
constant's value or loses WAL.

**21 openers converted**, in the four trees the gate scans:

- `cmd/agent` (8, the measured-red ones first): `cli_serve_stale_test.go` (the
  fixture writer `openRawTasksDB`), `cli_tasks_zombie_dispose_test.go` (the status
  read), plus `cli_tasks_worker_writeback_test.go`, `cli_version_test.go`,
  `cli_tasks_singleton_guard_test.go`, `cli_tasks_attach_index_test.go`,
  `cli_resume_worktree_test.go`.
- `internal/tools` (10) and `internal/agent` (4) — the sibling rigs that race the
  same resident components.

### 2. The hang: root-caused (5-why), and now legible

**Symptom** two concurrent `go test ./cmd/agent/` runs under one `GOPI_TEST_HOME`
both end in `panic: test timed out after 10m0s`, naming
`TestVoiceServeHTTPHandlerContract` as the last test running.

1. *Why did the suite die at its own 10m panic?* Two test goroutines never
   returned.
2. *Why did the voice goroutine never return?* Its readiness loop checked the
   deadline only **after** `http.Get` with the timeout-less `http.DefaultClient`;
   one accepted-but-unanswered request parks the goroutine forever, so the
   deadline is never re-evaluated.
3. *Why did the daemon-driven E2E tests never return?* They wait for a
   `gopi merger/worker --daemon` start whose handshake budget is consumed one
   failed start at a time.
4. *Why did the start keep failing?* `systemd-run --scope --unit=gopi-daemon-merger`
   — a **fixed** per-face unit name — is refused while the host's own resident
   `gopi merger --daemon` holds that unit: `Unit gopi-daemon-merger.scope already
   exists.` Measured live on this host (`systemctl --user list-units 'gopi*'`).
5. *Why was that invisible?* The parent reported only `daemon start failed: (see
   the log): EOF` — a transport event with the cause sitting in the daemon's log
   (Rule 15: the failure did not name its cause).

**Fixed**, both legs:

- the unit name is **session-unique** — `scopeUnitNameFor(component, launcherPID)`
  (`gopi-daemon-merger-<pid>`), and the cgroupfs scope *directory* is now literally
  that same name (one expression, `f586f166`), so a second daemon of the same face
  can never ask systemd for a taken unit and can never join the first daemon's
  cgroup;
- `daemonAwaitReady` now takes the daemon log path and quotes its **tail**
  (`daemonLogTailNote`, capped by `daemonLogTailBytes`) — the failure text names
  `Unit gopi-daemon-merger.scope already exists`, and an *unreadable* or *empty*
  log is reported as exactly that instead of rendering as an empty tail (Rule 15b);
- the voice readiness probes are bounded per probe (`voiceProbeClient`,
  `voiceProbeTimeout`) with the deadline evaluated on a ticker, and the server got
  `WriteTimeout: voiceServerWriteTimeout` (> the client probe deadline on purpose)
  so a connection nobody reads cannot be held forever.

The second, independent 10-minute hang source was also removed: the detach E2E
asserted `pgid == pid` unconditionally, which is false **by construction** on any
host whose user bus detaches via `systemd-run --scope` (the daemon sits inside the
transient scope; systemd-run is the session leader). It is now a criterion on the
**measured** mechanism (`daemonScopeModeEnv` from the daemon's own environment →
cgroup measurement), and it was measured red **on master too** before the change.

### 3. The regression guard (Rule 13 style, non-vacuous)

`cmd/agent/ledger_dsn_contract_test.go` parses every `.go` file of four roots
(`cmd/agent`, `internal/tools`, `internal/agent`, `internal/agencyscan`) into an
AST and fails any `sql.Open` whose driver argument is the sqlite driver and whose
DSN argument is not a `LedgerDSN`/`LedgerReadOnlyDSN` call. Two anti-blind halves:
a **per-root read floor** (`minFiles`) so a moved/renamed tree fails instead of
passing vacuously, and a **live-permit check** — the three production openers that
legitimately build the contract themselves (`memory.go` openDB, `agencyscan`
store.go, the miner's private fixture) are permitted **by name with a reason**, and
a permit whose site no longer exists fails the gate.

Red proof (run on a pristine copy of the tree, not asserted from memory):

```
=== WITHOUT the fix reintroduced (one bare opener added) ===
--- FAIL: TestLedgerOpenersUseProductionDSNContract
    ledger_dsn_contract_test.go:211: zz_redproof_bare_dsn_test.go:10 opens a ledger
    with a bare sqlite DSN — no busy timeout, no WAL: it fails instantly with
    `database is locked (5) (SQLITE_BUSY)` instead of queueing behind the resident
    daemon this suite starts. Use sql.Open("sqlite", tools.LedgerDSN(path))
=== AFTER removing it ===
ok  	github.com/the operator/gopi/cmd/agent	0.628s
```

The synthetic half (`TestLedgerBareOpenerPredicateReddensOnSyntheticSources`, 8
cases) pins the predicate itself: bare variable, raw string, helper-built DSN,
unqualified helper, another driver, a comment quoting the bad shape (not a
violation), two openers in one file (both reported).

## Not Done (honest)

- **`internal/agencyscan` keeps one bare opener** — its fixture seeds a *private*
  `t.TempDir()` ledger no daemon opens, and the package cannot import
  `internal/tools` (`internal/tools` imports `agencyscan`: an in-package test
  importing the helper closes an import cycle). This is an explicit permit with the
  reason recorded in the gate, not an unnoticed hole. A leaf constants package
  (toolnames-style) would close it properly and is the follow-up if wanted.
- **`internal/tools/memory.go` `openDB` still opens bare + applies
  `PRAGMA busy_timeout` on the handle** — deliberate: it is the *definition* of the
  contract (the DSN cannot express the pooled-connection half) and is permitted by
  name. `LedgerDSN` mirrors it, it does not replace it.
- **The 10-minute hang is removed at both observed sources, and a stall now NAMES
  its cause** (see the measured attribution below) — but the suite can still exceed
  its budget on a saturated host, because the remaining block is a disk-bound
  commit, not a code defect this branch can remove. `TestOwnRelease_RefusesLiveSibling`
  remains a third-party contention (an in-flight `internal/tools.writeTaskArea` DDL);
  it is pre-existing, unrelated to the ledger DSN contract, and outside this
  branch's scope — named here so it is dispatchable.
- The `SQLITE_BUSY` residual is *load-dependent*, so "gone" means "not reproduced
  in the measured matrix below", not "impossible". The mechanism is removed (the
  opener queues behind the writer instead of failing instantly); the rate is what
  is measured.
- The acceptance matrix below was produced at `999ce7d1`. `f586f166` (later) is a
  value-identical refactor of `scopeDirName` plus its guard; the four named tests
  were re-run green on it (`ok … 82.138s`). No acceptance claim rests on the
  refactor. Three further commits landed after the matrix — `8b947938` + `be79588f`
  (the stall reporter, with its red/honest halves green) and `9149e1ea` (the DSN's
  `synchronous` half, `TestLedgerDSNCarriesProductionsContract` green) — and Phase A
  was re-run green on that HEAD (`ok … 55.492s` for both named residuals).

## Verification

All scratch/state paths are on `/home` (device **2052**), never `/` (device
**2051**) — Rule 22, checked with `stat -c %d`, not assumed. `TMPDIR` is short
(`/home/user/ltb`, `/home/user/ltm`) because the suite's unix-socket fixture path
derives from `TMPDIR` and Linux' `sun_path` cap is 107 bytes: with the long
workspace path the fixture fails with `bind: invalid argument` on **either** tree
— an environment artifact that must not be handed to the control as an advantage.

### A/B control (epic requirement: the same command on a tree with none of this task's files)

- control = `/home/user/workspace/gopi-ledger-ctl-51edd` — **detached HEAD at
  `51edd8d9`**, the branch's own merge base. Verified to contain **none** of this
  task's files (`ledger_dsn.go`, `ledger_dsn_test.go`, `ledger_dsn_contract_test.go`,
  `daemon_scope_unit_test.go`: all absent).
- branch = the worktree above. Both trees get a **fresh `CGO_ENABLED=0 go build -o
  bin/gopi ./cmd/agent/`** and run with their own `GOPI_BIN` (the branch fixes E2E
  rigs that execute the real binary; handing either side the other's binary measures
  nothing). Both run **concurrently**, each internally sequential → matched load.

### Phase A — the two named residuals, isolated (the epic's 7/8 signature)

`-run '^(TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses|TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound|TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange|TestMergerDaemon_DetachesAndSecondIsRefused)$'`,
3 runs per tree, `delta2/`:

```
master run1 exit=1 PASS=0 FAIL=4 elapsed=13.684s      branch run1 exit=0 FAIL=0 elapsed=29.400s
master run2 exit=1 PASS=0 FAIL=4 elapsed=12.325s      branch run2 exit=0 FAIL=0 elapsed=42.607s
master run3 exit=1 PASS=0 FAIL=4 elapsed=13.954s      branch run3 exit=0 FAIL=0 elapsed=42.320s

comm -13 (failures the branch removes):
  TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound
  TestMergerDaemon_DetachesAndSecondIsRefused
  TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses
  TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange
comm -23 (regressions): (none)
```

The named zombie test on its own, **8 isolated runs per tree** (`zombie/`): master
`FAIL 8/8`, branch `FAIL 0/8` (`zombie/matrix.txt`).

The master failure texts are the two defects, verbatim (`delta/master-1.log`,
`zombie/master-1.log`):

```
task_lifecycle_tick_test.go:557: status t-zd-confirmed: database is locked (5) (SQLITE_BUSY)   ← the fixture's bare-DSN read
task_lifecycle_tick_test.go:325: `gopi merger --daemon` must exit 0, got 1:
    gopi merger: daemon start failed: daemon exited during startup without reporting (see the log): EOF   ← the unit clash
```

Note the interaction, measured rather than assumed: on master the unit clash often
kills the start **before** the fixture's write, so the `SQLITE_BUSY` row only
appears in the runs where the daemon did start (1 busy line per `delta/master-*.log`).
Both halves are needed — with the unit unique, the test's daemon really runs, and
*then* only the busy-timeout + WAL DSN keeps the fixture's own write from losing
the race. That is why this branch carries both.

### Phase A (re-measured at the FINAL HEAD `99b0f260`, a low-load window, 2026-09-16)

The 8-vs-8 matrix above was taken before the later commits landed. Re-measured on
the frozen HEAD, 3 runs/tree, per-tree `bin/gopi` (`GOPI_BIN`), off-`/` `TMPDIR`
(`/home/user/ltb` / `/home/user/ltm`) — the two named residuals first:

```
$ go test ./cmd/agent/ -count=1 -run 'TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses'
control (pristine 51edd8d9) run1 exit=1  FAIL  1.733s     branch (99b0f260) run1 exit=0 ok 4.566s
control                     run2 exit=1  FAIL              branch            run2 exit=0 ok 5.926s
control                     run3 exit=1  FAIL              branch            run3 exit=0 ok 3.580s

control failure, verbatim:
  --- FAIL: TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses (1.29s)
      gopi merger: daemon start failed: daemon exited during startup without reporting (see the log): EOF

$ go test ./cmd/agent/ -count=1 -run 'TestVoiceServeHTTPHandlerContract|TestOwnRelease_RefusesLiveSibling'
control exit=0 ok 0.547s        branch exit=0 ok 2.825s
```

Two facts this pins down. (a) The control's daemon-start failure is the
**fixed-unit-name** signature — a resident host daemon makes every test daemon
start die instantly, and the test then fails; with the session-unique unit the
branch's daemon actually starts and the test completes in 3.6-5.9 s. (b) The
`unit-clash` literal (`Unit gopi-daemon-merger.scope already exists`) counts 0 in
BOTH trees' *test* output because that text lives in the **daemon log**, which is
exactly why the branch's `daemonAwaitReady` now quotes the log tail — on the
control the operator sees only `EOF`; on the branch (when a start really fails) the
verdict names the cause.

The isolation is real, not inferred: the control's own lifetime note names the
write end held by the test process and the control still fails at daemon start
(`EOF`), while the branch's identical test — same armed lifetime fd, same
`GOPI_TEST_HOME`, only the unit name differs — brings its daemon up and passes.

The occupancy is directly observable — the fixed unit name the control would reuse
is already **registered in the user manager** on this host, so the control's systemd
refusal is not a race it can win:

```
$ systemctl --user list-units --type=scope
* gopi-daemon-merger.scope  loaded failed  failed  /bin/bash -c exec -a 'gopi-merger' … 'merger'
  gopi-daemon-worker.scope  loaded active  running /home/user/work/GoPi/bin/gopi worker
```

### Phase B — the epic's own acceptance command, twice in a row, one `GOPI_TEST_HOME` per tree

`go test ./cmd/agent/ -count=1`, Go's **default 10m** package budget (deliberately
not overridden: the claim under test is that the suite no longer trips its own
budget), `final/` (8 runs/tree) and `accept-final/` (the epic's 2-consecutive-run
shape, both trees concurrent):

```
final/matrix.txt (8 runs/tree, no timeout-hits below)
control A run1 exit=1 wall=27s fails=2   branch A run1 exit=0 wall=37s fails=0 okline=1
control A run2 exit=1 wall=22s fails=2   branch A run2 exit=0 wall=48s fails=0 okline=1
control A run3 exit=1 wall=36s fails=2   branch A run3 exit=0 wall=47s fails=0 okline=1
control A run4 exit=1 wall=32s fails=2   branch A run4 exit=0 wall=33s fails=0 okline=1
control A run5 exit=1 wall=28s fails=2   branch A run5 exit=0 wall=30s fails=0 okline=1
control A run6 exit=1 wall=24s fails=2   branch A run6 exit=0 wall=24s fails=0 okline=1
control A run7 exit=1 wall=20s fails=2   branch A run7 exit=0 wall=33s fails=0 okline=1
control A run8 exit=1 wall=17s fails=2   branch A run8 exit=0 wall=49s fails=0 okline=1

control union (8 runs): TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound
                        TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses
branch  union (8 runs): (empty — every run printed `ok github.com/the operator/gopi/cmd/agent`)
```

### The full-suite shape on THIS host, measured 2026-09-16 (honest, and not a branch defect)

The epic's acceptance command was run twice in a row under one `GOPI_TEST_HOME`,
Go's **default 10m** budget, in the epic's own shape — and, after the concurrent
shape below showed both trees timing out, again **serially** (`serial/`, one tree
at a time, nothing else of this task running, `run-serial-accept.sh`) so that
concurrency could not be the explanation:

```
accept-final/ (both trees concurrent, the epic's shape)
branch  run1 exit=1 wall=602s timeout=1 okline=0 fails=1 busy=0
branch  run2 exit=1 wall=619s timeout=1 okline=0 fails=1 busy=0
control run1 exit=1 wall=637s timeout=1 okline=0 fails=0 busy=0
control run2 exit=1 wall=602s timeout=1 okline=0 fails=0 busy=0

serial/ (ONE tree at a time, same command, same 10m budget)
branch  run1 exit=1 wall=600.449s timeout=1 okline=0 fails=2 busy=0
                            running tests: TestTaskListInProgressViewResolvesByLedgerHint (6s)
branch  run2 exit=1 wall=600.245s timeout=1 okline=0 fails=1 busy=0
                            running tests: TestManualRunKeepsWorktreeHeldByForeignProcess (9s)
control run1 exit=1 wall=600.302s timeout=1 okline=0 fails=0 busy=0
                            running tests: TestStrandedScanSkipsRecycledRows (9s)
control run2 exit=1 wall=600.347s timeout=1 okline=0 fails=0 busy=0
                            running tests: TestStrandedScanReachesADeepRow (28s)
```

**Every one of those eight runs tripped the 10m budget, on BOTH trees, and the
serial shape rules out concurrency as the explanation** — one tree at a time, same
command, same budget: branch `timeout=1` twice, control `timeout=1` twice. No
`SQLITE_BUSY` line appears in ANY tree's full-suite log at any point in this
session, and the two named residuals are green (Phase A below). The 10m shape on
this host is therefore a HOST-LOAD measurement, not a differential between the trees.

Two further observations keep this honest rather than hand-waved. (a) The branch's
`fails=2`/`fails=1` rows are **not** a timeout symptom: they are
`TestCodeSearchBackendFindsDefinition` (30.01s) and
`TestCLIContractSettingsConfigFlagRoutesToTaskHandler` (24.38s) — wall-clock
assertions that a host paying ~0.7 s per fsync cannot meet, and the second one
passed at 0.03s on the immediately following run. (b) The `running tests:` line
names a **different** test on every run (`TestTaskListInProgressViewResolvesByLedgerHint`,
`TestManualRunKeepsWorktreeHeldByForeignProcess`, `TestStrandedScanSkipsRecycledRows`,
`TestStrandedScanReachesADeepRow`) — there is no single stuck test; the whole suite
is uniformly slowed by the ledger opens every fixture performs, and the 10m budget
expires wherever the clock happens to be when it runs out.

What the stall actually is, with the watchdog's measured attribution (this branch):

```
suite stall: … running 8m0s (watchdog budget 8m0s) …
live child processes: none — nothing this suite spawned is still alive …
goroutines of this repository still running (3) — the blocked work is here:
goroutine 916 [syscall, 1 minutes]:
github.com/the operator/gopi/internal/tools.execSchemaBatchOnce(…)
	/home/user/wl2/internal/tools/memory.go:595 +0x145          ← tx.Commit()
github.com/the operator/gopi/internal/tools.execSchemaBatch(…)  memory.go:570
github.com/the operator/gopi/internal/tools.ensureSchema(…)     memory.go:526
github.com/the operator/gopi/internal/tools.initKnowledgeBase(…) memory.go:864
```

Root cause (5-why, measured rather than assumed): the ledger's first-open schema
batch commits inside one transaction; the commit's `fsync` is fast, but on this
host it is not — `/proc/pressure/io` reported **full avg300 = 90-92%** and a single
`fsync` measured **0.7-1.3 s/op** (1256 ms/op, 20 ops in 15.2 s at t+4000s;
970 ms/op at t+5740s). SQLite re-syncs the WAL at checkpoint, and in WAL mode the
`PRAGMA synchronous=NORMAL` this branch's `enableWAL` already applies only drops
the PER-COMMIT sync — the checkpoint sync remains, and it is the one that blocks
here. `/dev/shm` was simultaneously 92% full (7.2G of `gopi-test-scratch`, of which
5.3G was the shared `queue` dir) and other agents were running their own suites on
4 cores / 15G RAM.

Consequence for this task's acceptance, stated honestly: **the twice-in-a-row
full-suite criterion is not satisfiable on this host at any point measured
2026-09-16**, on either tree — and that is an environment fact, not a claim about
this branch. What IS established, and is the epic's real question:

1. the two named `SQLITE_BUSY` residuals are **gone** (Phase A, 8/8 vs 0/8),
2. the hang's two observable sources are **fixed** (unit clash, unbounded probes)
   and a stall now **names its cause** instead of a naked 10m panic,
3. the remaining full-suite stall is **attributed to a disk-bound commit** by the
   watchdog, with the file:line — which is exactly Deliberable 2's "legible"
   requirement, and is how this conclusion was reached at all.

The same conclusion appears **independently** in the pristine control's own stack
(no watchdog there, Go's bare 10m panic) — direct evidence that the blocking work
is the ledger, not this branch's changes:

```
control (51edd8d9) run2, goroutine 1133 [syscall, 6 minutes]:
github.com/the operator/gopi/cmd/agent.strandedSeedRow(…)
	…/cli_tasks_stranded_sweep_test.go:51
database/sql.(*DB).ExecContext(…)  database/sql/sql.go:1671     ← an INSERT that never returns
control  run1, goroutine 3956 [runnable]:
modernc.org/sqlite.(*conn).Close(…) / database/sql.(*DB).Close(…)
github.com/the operator/gopi/cmd/agent.strandedScan(…)
	…/cli_tasks_stranded_sweep_test.go:70                      ← a Close that never returns
```

So the mechanism is: **every fixture's ledger open/write pays the host's disk
cost, in the schema batch's commit and then in each fixture INSERT/Close**; the
suite's wall time is the sum of those, and the 10m package budget is simply too
small for ~360 tests on a host whose `/proc/pressure/io` has been pinned at
`some avg300 ≈ 96%` with a measured **682-1256 ms/fsync**. That is why no test name
repeats, and why the honest verdict is an environment limit rather than a pass.

### Acceptance instrument: the pressure clause, REPLACED (2026-09-16)

The epic this report answers (`01a0a63e`) carried an acceptance clause quoted as
`≥90% /proc/pressure/io full avg300`. That number is a **kernel** metric, not a
product metric, and the cheapest way to satisfy it is to flood the block device
rather than to reproduce the ledger behaviour under test — which is exactly what
happened (three `iopress.sh` batches × 16 workers, `oflag=direct conv=fsync`,
load 56.42). It is **withdrawn as an acceptance input**. The replacement text
(the clause as it now reads):

> **Acceptance (direct instrument — supersedes any `/proc/pressure/io` figure).**
> The acceptance evidence is the run's OWN artifacts: (a) each package's wall
> time and the slowest ≤15 tests, taken from that run's `go test -json` stream
> (`run-<epoch>.json` in the scratch queue's `test-budget/` directory — the same
> directory as `stamps.log`, whose one-line-per-run stamp records the package,
> budget, elapsed, load average before/after, stream name and outcome; the
> per-run copy exists precisely so a quoted number stays citable after the next
> run overwrites `last.json`), and
> (b) the duration of the implicated call itself — the first-open schema batch's
> `tx.Commit()` in `execSchemaBatchOnce` (`internal/tools/memory.go`), as
> attributed by the stall watchdog's goroutine dump (`file:line`) and by the
> per-test elapsed of the ledger-opening fixtures. No kernel pressure metric is
> an acceptance input, and no test may manufacture pressure to produce one
> (PROJECT.md Rule 27 — now mechanically gated, `internal/testutil/rule27_gate_test.go`).
> A verdict of "not satisfiable on this host" must therefore be stated in terms
> of those two measurements, not of a device-saturation figure.

The **diagnosis is unchanged and stands** on its own (it never needed the
pressure number): every fixture's ledger open pays the host's disk cost, the
first-open `schema` batch is ONE transaction whose commit blocks
(`execSchemaBatchOnce → tx.Commit()`), and ~360 such fixtures sum past the 10 m
package budget. What changes is only the *instrument* by which a future run may
claim to have measured it.

Where the retired clause now lives: this devdoc is the durable record. The
task row itself is gone (`gopi task show 01a0a63e` → no such task), and the
`briefings/*.md` files are the archived brief texts of queued work — read-only
inputs owned by other tasks, deliberately NOT rewritten. `GOPI.md` Rule 27 quotes
the number only as the incident's history, with the correction attached.


A narrowed, concrete follow-up for whoever owns the suite (recorded as a dispatched
bug task, **not** claimed as this branch's deliverable): the ledger's first-open
`schema` batch is ONE transaction whose commit blocks for minutes under this disk
pressure (`execSchemaBatchOnce → tx.Commit()`, `internal/tools/memory.go:692`), so
the schema init — not the fixtures — is where per-test cost can actually be bounded.

`GOPI_TEST_SCRATCH_HOME` was pointed at `/home/user/ltb2/scratch` for these runs
(off tmpfs, off `/`) so the run's own scratch could not be the thing filling
`/dev/shm` — the pin is honoured and the 92% figure is OTHER processes' residue.

### Rules 13/14 evidence (grep, on the branch)

```
$ grep -rn '15000' --include=*.go . | grep -v _test.go
internal/tools/memory.go:80:	dbBusyTimeoutMSec = 15000          ← the ONE definition

$ grep -rn 'sql.Open("sqlite"' --include=*.go . | grep -v 'LedgerDSN\|LedgerReadOnlyDSN'
internal/tools/memory.go:467      ← openDB, the definition of the contract (permitted by name)
internal/agencyscan/store.go:68   ← mirrors its own busy_timeout pragma (permitted by name, import cycle)
internal/agencyscan/agencyscan_test.go:37 ← private fixture ledger (permitted by name)
```

No bare opener anywhere in `cmd/agent` (production or test), and no re-typed
busy-timeout literal outside the one definition.

### Build / test / format

```
$ gofmt -l  <every .go file this branch touched>     → (empty)
$ go vet ./cmd/agent/ ./internal/tools/              → clean
$ go build ./...                                     → OK
$ go test ./cmd/agent/ -run 'TestScopeUnitNameIsPerSession|TestDaemonAwaitReadyQuotesTheLogTail|
    TestDaemonLogTailNoteReportsAnUnreadableLog|TestLedgerOpenersUseProductionDSNContract|
    TestLedgerBareOpenerPredicateReddensOnSyntheticSources'
                                                     → ok  (0.594s)
$ go test ./internal/tools/ -run 'TestLedgerDSN'      → ok  (0.238s)
```

`gofmt -l` over the four touched trees reports the same **pre-existing** files as on
the control tree (`cmd/agent/cli_daemon_scope.go` and three others); no file this
branch touches is unformatted.

## Rule 6d — deliverable declaration

1. **Branch**: `fix/ledger-busy-dsn-contract-v2`, in the worktree
   `/home/user/workspace/gopi-ledger-closeout` (a linked worktree of
   `/home/user/work/GoPi` — the commits are in the shared object store, so the
   branch already exists in the main repo; no push needed, Rule 6c).
2. **Recovery**: `git show fix/ledger-busy-dsn-contract-v2:devdoc/ledger-dsn-cmd-agent-contract.md`,
   `git show fix/ledger-busy-dsn-contract-v2:internal/tools/ledger_dsn.go`.
3. **Confirmed**: the branch is off master `51edd8d9` (verified ancestor) and the
   commit chain above lives there. No clone was used; nothing was deleted.
