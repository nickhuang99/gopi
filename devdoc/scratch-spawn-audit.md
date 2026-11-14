<!-- doc-class: historical -->
# Test scratch / spawn hygiene — part B (creator-owns-cleanup)

Companion to `devdoc/tmp-ban-scratch-root.md` (part A: the pinned scratch root
+ age/holder GC). Part A bounded the DATA a test suite writes. This part bounds
the PROCESSES a test suite spawns: a long-lived mock that outlives its test
holds its scratch files open, keeps polling, and survives the whole run — the
process half of the 2026-09-11 peer1 exhaustion
(`devdoc/peer1-resource-exhaustion-20260911.md`).

Branch: `feat/test-spawn-pgid-reaper`
Commits: `9a44974` (reaper) · `4e71bad` (dead-pid/holder/daemon migration) ·
`6828a56` (X servers + test-binary build paths) · `5fbcef0` (creator-owns-cleanup:
no orphan root per re-exec'd holder + the missing helper contract tests) ·
`<report commit>` (this doc)

## 1. The helper: `internal/testutil/mockproc.go`

`StartMockProcess(t, path, args...)` / `StartMockProcessCmd(t, cmd, opts...)`
start the child with `Setpgid` (its own process group, pgid == pid) and register
a `t.Cleanup` that reaps **that group**: `SIGTERM` → `SIGKILL` → `Wait`.

Safety properties, each one a test:

| Property | How it holds | Test |
|---|---|---|
| own group at spawn | `Setpgid`; pgid == pid asserted | `TestStartMockProcessSpawnsOwnGroup` |
| never a global scan / never `pkill -f` | the reaper addresses ONE pgid; no `/proc` walk, no name match | by construction (no such code) |
| never signals a foreign process | a handle only ever signals `-m.pgid`; a process started outside the handle is untouched | `TestReapNeverTouchesAForeignProcess` |
| refuses its own group | `pgid < reaperMinPGID (=2)` or `pgid == getpgrp()` → refuses and reports why | `TestReapRefusesItsOwnProcessGroup`, `TestReapRefusesDegenerateGroupID` |
| idempotent | `atomic.Bool` CAS — a fixture's explicit `Reap` + the cleanup is the normal pattern | `TestStartMockProcessSpawnsOwnGroup` (reaps twice) |
| exited-safe | a gone group is never signalled (a recycled pgid would hit a stranger) | `TestReapIsIdempotentOnAlreadyExitedProcess` |
| panic-safe | the kill lives in `t.Cleanup`, which `testing` runs on every exit path including a panic | `TestMockProcessReapedOnPanicPath` (re-exec'd child that panics on purpose) |
| shared, non-racing wait | one `waitOnce`-guarded goroutine; `MockProcess.Wait()` is the caller-visible face | `Reap` + `Wait` used together in the migrated daemon fixtures |
| kills the bloodline | a grandchild in the same group dies with the leader | `TestReaperKillsTheWholeSpawnedGroup` |

Two measurements worth recording, both found while building this and both now
regression-guarded:

1. **A zombie leader keeps the group alive.** `kill(-pgid, 0)` succeeds while an
   unreaped direct child is a zombie — it is still a group member. So the reaper
   reaps the direct child *while* polling the group (`reapAndGroupGone`), not
   after; a reaper that polls first burns its whole grace window on a process
   that is already dead. Corollary for assertions: probe liveness with
   `kill(-pgid, 0)`, never `kill(pid, 0)`.
2. **`Setsid` + `Setpgid` = `EPERM`.** `setpgid(2)` on a session leader fails,
   and `cmd.Start()` fails with it (`fork/exec /usr/bin/sleep: operation not
   permitted`). The helper therefore sets `Setpgid` only when the caller did not
   already claim `Setsid` (which gives the same own-group contract). This is
   `TestStartMockProcessCmdKeepsASetsidCallersOwnGroup` — the X-server fixtures
   would have been broken outright by the naive version.

Opt-in deviation from the default (a missing fixture is a **hard failure**,
Rule 15a): `AllowStartFailure(...)` turns it into a visible `t.Skipf`, and
`OnSkip(fn)` runs a cleanup hook first (the xev log file).

## 2. Spawn inventory

Inventory method: `grep -rn --include='*_test.go' -E '\.Start\(\)' internal cmd`
(all 47 test files that call `exec.Command` were read for the *long-lived* ones —
a `CombinedOutput()`/`Output()`/`Run()` call is a one-shot child that blocks its
own parent and needs no group).

**Long-lived (daemon-shaped) — all migrated.** These are the sites that could
outlive a failing test:

| Site | What it spawns | Migrated to |
|---|---|---|
| `cmd/agent/cli_tasks_waitpid_test.go` | `gopi task run` wrapper + its real child | `StartMockProcessCmd` + handle `Wait()` |
| `cmd/agent/cli_tasks_test.go` (`TestTaskRunSurvivesClosedStdoutPipe`) | `gopi task run` on a real pipe | `StartMockProcessCmd` + handle `Wait()` |
| `cmd/agent/merger_singleton_e2e_test.go` | `gopi task merger --every 5m` daemon | `StartMockProcessCmd` (mid-test reaps via `hu.Reap()`) |
| `cmd/agent/cli_web_align_test.go` | `gopi web --addr=…` server | `StartMockProcess` + handle `Wait()` |
| `internal/tools/task_finish_cleanup_test.go` | re-exec'd CWD holder | `StartMockProcessCmd` |
| `cmd/agent/cli_tasks_recycle_e2e_test.go` | re-exec'd CWD holder | `StartMockProcessCmd` |
| `internal/shot/cov_shot_x11_test.go` | `Xvfb` (Setsid) | `StartMockProcessCmd(…, AllowStartFailure())` |
| `internal/shot/cov_shot_x11_test.go` | `xev` recorder (Setsid) | `StartMockProcessCmd(…, AllowStartFailure(OnSkip(close log)))` |
| `internal/testutil/run.go` `StartGopi` | background `gopi` process (shared library helper) | `StartMockProcessCmd` inside `StartGopi`; `Stop()` reaps the group, cleanup is the net |

**One-shot children — migrated where a helper already existed, harmless
otherwise.** A `CombinedOutput()` child cannot outlive its parent call:

| Site | Migrated to |
|---|---|
| `internal/tools/taskzombie_test.go` (`spawnAndKill`) | `StartMockProcess` (handle exposes `Reap`) |
| `internal/tools/taskzombie_sweep_test.go` (`deadPid`) | `StartReapedProcess` |
| `internal/tools/repo_resolve_row_test.go` (`spawnAndKillPID`) | `StartReapedProcess` |
| `cmd/agent/cli_serve_stale_test.go` | `StartReapedProcess` |
| `cmd/agent/cli_resume_stale_test.go` | `StartReapedProcess` |
| `cmd/agent/cli_tasks_list_sweep_test.go` | `StartReapedProcess` |
| `cmd/agent/cli_tasks_sweep_zombies_test.go` (×3) | `StartReapedProcess` |
| `internal/testutil/scratchroot_test.go` (×2 holder fixtures) | `StartMockProcessDir` |
| everything else (`git …`, `go build …`, `xdotool`, `xwininfo`, `go test -run`) | left as-is — blocking one-shot children |

After the migration there is **no `.Start()` anywhere outside
`internal/testutil/`**:

```
$ grep -rn --include='*_test.go' -E '\.Start\(\)' internal cmd | grep -v internal/testutil/
(no output)
```

### Un-migratable / deliberately untouched

- `internal/shot/x11.go: x11UnixSocketDir = "/tmp/.X11-unix"` — a **protocol
  endpoint**: X11 clients must find servers at that exact path. It is a
  connect/read path, not scratch we create. It does mean X-server tests leave a
  socket + lock file under `/tmp`; both are tiny (0 bytes / 11 bytes), are
  removed by the fixtures' own cleanup, and cannot be relocated without breaking
  X11. Documented here so the leftover is explicit rather than accidental.
- `plugins/websearch/…` — a deployed remote service, excluded by part A's gate
  scope (see `tmp-ban-scratch-root.md`), unchanged.
- `/tmp/gopi-job-*.{out,err}` and other agent-shell artifacts — produced by the
  agent's own tooling, not by a suite.

## 3. Un-asked-for finding: four test binaries were built into `/tmp`

### 3a. The re-exec'd child's own scratch root — the biggest leftover

A second, bigger leak turned up only by *counting* the scratch home after a run:
**94 of the 104 entries under `~/workspace/gopi-test-scratch` were empty
per-run roots from already-finished suites** (`suite-tools-run-*` 50,
`suite-internal-testutil-run-*` 29, `suite-cmd-agent-run-*` 10, …). Every one
held nothing but its own `.holder` registration, and every one is younger than
`ScratchStaleAge` — so the age GC would collect them a run-later at best, and
a busy host accumulates them in the meantime.

Root cause (5-why, not a parameter):

1. `go test` runs each suite in its own process whose TestMain pins a run root
   and removes it after `m.Run()` via `FinishSuiteForTestMain`.
2. Several tests re-exec **the same suite binary as a child** to get a live CWD
   holder (`internal/tools/task_finish_cleanup_test.go`, `cmd/agent/cli_tasks_recycle_e2e_test.go`,
   `internal/tools/memory_sync_txn_test.go`, …).
3. The child's TestMain therefore pins a run root **of its own** — in the
   shared scratch home, since it inherits the ambient environment.
4. The parent then kills that child (the pgid reaper, or `CombinedOutput()`
   finishing), so the child's `m.Run()` never returns and its
   `FinishSuiteForTestMain` never runs.
5. → the child's root is orphaned by construction: nothing will ever remove it
   except the 6h age sweep.

Fix — creator-owns-cleanup at the point where the creator is known: the
re-exec sites pass the child its own scratch root **inside the test's fixture**:

```go
cmd.Env = append(os.Environ(), holderEnvMarker+"=1",
    testutil.SuiteTempRootEnvName+"="+t.TempDir(),
    testutil.TempDirEnvName+"="+t.TempDir())
```

The child's pin then happens inside the *parent test's* `t.TempDir()`, which
`testing` removes at test end — the leak becomes a directory the spawning test
already owns and always cleans. Two sites fixed
(`internal/tools/task_finish_cleanup_test.go:liveCwdHolder`,
`cmd/agent/cli_tasks_recycle_e2e_test.go:startRecycleHolder`); measured after
the fix: **0 new roots** for the tests that spawn holders (before: one per
holder).

`internal/testutil`'s own `TestMain` was a third instance of the same class: it
pinned a root with the two-call form (`PinSuiteTempRoot` +
`FatalAssertSuiteScratchRootSafe` — this package's suite deliberately asserts
its own root) but never called `FinishSuiteForTestMain`, so **every run of this
package** left an empty `suite-internal-testutil-run-*` behind. Fixed in
`internal/testutil/scratchroot_test.go`.

### 3b. Four test binaries were built into `/tmp`

The `/tmp` audit for this part turned up test-infrastructure writes that part A
had not caught, all in the same class ("the suite's own toolchain output"):

| Site | Was | Now |
|---|---|---|
| `cmd/agent/cli_version_test.go` | `go build -o /tmp/gopi-version-test .` | `os.MkdirTemp(testutil.SuiteTempRoot(), versionBuildDirPrefix)` |
| `cmd/agent/home_settings_test.go` | `go build -o /tmp/gopi-home-settings-test .` | same, via `homeSettingsBuildDirPrefix` |
| `cmd/agent/cli_contracts_test.go` | `os.MkdirTemp("", "gopi-contract-bin-")` | `os.MkdirTemp(testutil.SuiteTempRoot(), …)` |
| `cmd/agent/settings_nilpanic_test.go` | `os.MkdirTemp("", "gopi-settings-nilpanic-bin-")` | `os.MkdirTemp(testutil.SuiteTempRoot(), …)` |

They now land in the suite's pinned scratch root (GC'd by part A's sweep)
instead of the `/` partition. Note the second-order effect: the *build tree* for
a suite (`/tmp/go-build*`, measured at 129M in part A's audit) stays wherever
`GOTMPDIR`/`TMPDIR` points, and `TMPDIR` is pinned by `RunSuiteMain` — so for
suites that call it, the go toolchain's own scratch already moves with it.

## 4. Part A acceptance re-confirmation

Part A's semantics are pinned in `internal/testutil/scratchroot_test.go`; this
branch re-ran and extended them. `go test ./internal/testutil/ -run TestGC -v`:

- `TestGCStaleScratchRemovesOldOrphanAndKeepsYoung` — a 7h-old orphan is
  collected, a 5h-old one is kept (age boundary).
- `TestGCStaleSweepBoundedAgeBoundary` — ±1min around `ScratchStaleAge`,
  collected/kept respectively.
- **new** `TestGCNeverTouchesAnotherSuitesLiveDirectory` — the two-sided
  namespace/liveness boundary: another suite's **aged root whose owner is alive
  survives** (cwd-held holder process), a **non-namespace directory is never a
  candidate**, and the aged unheld orphan in *this* suite's namespace is the
  single entry removed (the control that proves the sweep ran).
- `TestGCKeepsScratchHeldByALiveProcess`, `TestGCKeepsScratchHeldByOpenFileDescriptor`
  — the in-use veto (cwd, and open fd).
- `TestGCCollectsAbandonedRunRoot`, `TestGCConcurrentSweepsAreSafe`,
  `TestGCMissingHomeIsANoOp`, `TestGCUnreadableScratchEntryDoesNotPanic`.

Honest scope note: the sweep is **shared across suites** by design — a suite
killed with `-9` leaves a root nobody else would collect, so GC is not scoped to
"my own suite's entries" (the epic's phrase). What is scoped is *deletion*
authority: an entry is removed only if it is attributable (carries a scratch
marker), age-stale, and **unheld** — the two independent holder channels
(`/proc/<pid>/cwd|fd` and the `.holder` pid+start-time registration) are what
protect a live peer. That is what the new test pins.

## 5. Verification

```
$ go build ./...                                  # OK
$ go vet ./internal/testutil/ ./internal/tools/ ./cmd/agent/ ./internal/shot/   # OK
$ gofmt -l internal cmd                           # (no output)
$ go test ./internal/testutil/ ./internal/tools/ ./cmd/agent/ ./internal/shot/ -count=1   # see below
```

### 5a. Acceptance run (4 suites)

Isolated-namespace acceptance run (`GOPI_TEST_SCRATCH_HOME` = a fresh private
home, so every entry counted is this run's), job log `gopi-isolated.log`,
2026-09-11 08:2x:

```
########## internal/testutil   ok   2.448s   roots left: 0
########## internal/tools      ok 345.023s   roots left: 0
########## cmd/agent           ok  40.450s   roots left: 0
########## internal/shot       ok  40.953s   roots left: 0
=== TOTAL entries left in my private home: 0 ===
```

| Measurement | Result |
|---|---|
| `go test ./internal/testutil/ ./internal/tools/ ./cmd/agent/ ./internal/shot/ -count=1` | **all 4 `ok`, exit 0** |
| scratch roots left after the whole run | **0** |
| straggler mock/daemon pids after the run | **none** (the only `pgrep` hit is the harness's own shell) |
| `/tmp` growth attributable to the suites | **none** (§5b/§5c) |

Re-run on the FROZEN commit `707e0ed`, host loaded by other sessions' suites:

```
$ go test ./internal/testutil/ ./internal/tools/ ./cmd/agent/ -count=1
ok  github.com/nick/gopi/internal/testutil   0.809s
ok  github.com/nick/gopi/internal/tools    162.825s
ok  github.com/nick/gopi/cmd/agent          33.264s
$ GOPI_TEST_SCRATCH_HOME=$(mktemp -d /dev/shm/gopi-iso-XXXX) go test ./cmd/agent/ ./internal/testutil/ -count=1
ok  github.com/nick/gopi/cmd/agent  42.877s    # isolated home: 0 roots left
ok  github.com/nick/gopi/internal/testutil  0.856s
```

`scripts/safe-merge.sh feat/test-spawn-pgid-reaper` at the same commit ✅ a/b/d,
and its ⚠️ c ("still held by a worktree") is the EXPECTED answer while the
branch is being handed over: the guard exists so a branch cannot be merged
WHILE a worktree still holds it. Removing the worktree is the merger's step
after acceptance, not this branch's.

### 5b. `/tmp` accounting — what the numbers actually mean

The honest reading of a before/after `df` here: **the test suites are no longer
a `/tmp` writer, and the residue is attributable to the `go` TOOLCHAIN, not to
the suites.** Evidence, from running the compiled suite BINARIES directly with
`TMPDIR` moved off `/tmp` (job log `gopi-suiteonly.log`):

| Participant | Writes to `/tmp`? |
|---|---|
| suite binaries (`internal/testutil`, `internal/tools`, `cmd/agent`, `internal/shot` run directly) | **no new entries** (only the toolchain's own `go-build*`, created by `go test -c`) |
| `go test` (the toolchain) | `/tmp/go-build*` working tree — `GOTMPDIR`/`TMPDIR`-driven, and `RunSuiteMain` pins `TMPDIR` for the suite *after* the toolchain has already built it |
| other agents' sessions on this host | yes — `/tmp/xev97.log`, `/tmp/sg-b*.json`, `/tmp/xvfb8*.log`, `/tmp/f1.txt`, `/tmp/gate_probe.sh` all appeared during our window and **none** is produced by this repo's suites (`grep` over `internal/`, `cmd/`, `scripts/` finds no producer) |

So a bare before/after `df` on a host running other agents is not a clean
measurement, and the deliverable reports it as such rather than as a green
number. The clean measurement is the per-package one in §5c.

Direct `df`/entry-count before→after, taken again at the end of this branch
(2026-09-11 08:3x, host busy — `go test ./...` and a stylegate run from two
other sessions were live at the time):

```
before  go test ./internal/shot/   : 2888 top-level /tmp entries | /dev/sda3 122G used 18G avail
after   go test ./internal/shot/   : 2887 top-level /tmp entries | /dev/sda3 122G used 18G avail
```

`/tmp` did **not** grow across a full suite run — the count went down by one
(a concurrent session's artifact), `df` is byte-identical at this granularity,
and the same run left **0 entries** in an isolated `GOPI_TEST_SCRATCH_HOME`.
The 2887 residual entries are pre-existing (12 `go-build*` trees from the
toolchain plus other sessions' files); none is attributable to this repo's
suites.

### 5c. Per-package scratch-home delta, measured in an ISOLATED namespace

A shared scratch home cannot attribute a leftover: other agents' sessions on
this host run the same suites concurrently, so a before/after count in the
shared home mixes their roots with ours (observed directly: a run of
`internal/shot` appeared to add `suite-cmd-agent-run-*` roots, which that
package cannot create). The clean measurement is a **private scratch home** —
with `GOPI_TEST_SCRATCH_HOME` pointed at a fresh directory, every entry in it
was created by the run under measurement (job log `gopi-isolated.log`):

```
$ GOPI_TEST_SCRATCH_HOME=/dev/shm/gopi-probe-XXXX go test ./<pkg>/ -count=1
```

| Package | roots left behind |
|---|---|
| `internal/testutil` | **0** |
| `internal/tools` | 0 |
| `cmd/agent` | 0 |
| `internal/shot` | 0 |

Every suite's own TestMain removes its run root after `m.Run()`; the re-exec'd
holders' roots now live inside their creator's `t.TempDir()` (§3a). What remains
in a shared home between runs is therefore a *peer's in-flight* root, which the
holder registration protects from the GC — that is the designed state, not a
leak.

`/tmp`: no suite binary writes there (measured by running the compiled binaries
with the toolchain out of the picture — §5b). The `/tmp` residue during a `go
test ./...` window is the `go` TOOLCHAIN's own `go-build*` tree plus other
sessions' artifacts.

### 5d. Acceptance criterion → test

| Epic acceptance criterion | Test (all PASS) |
|---|---|
| a 7h-old orphan under a suite's namespace is removed on next start | `TestGCStaleScratchRemovesOldOrphanAndKeepsYoung` (7h removed, 5h kept), `TestGCStaleSweepBoundedAgeBoundary` (±1min around `ScratchStaleAge`) |
| an in-use dir is kept | `TestGCKeepsScratchHeldByALiveProcess` (cwd), `TestGCKeepsScratchHeldByOpenFileDescriptor` (fd) |
| another suite's dir is untouched | `TestGCNeverTouchesAnotherSuitesLiveDirectory` — an aged PEER root held by a live holder survives, a non-namespace directory is never a candidate, and the aged unheld orphan in this suite's namespace is the ONE entry removed (the control proving the sweep ran) |
| after a test that spawns a mock, the pid is unreachable | `TestStartMockProcessSpawnsOwnGroup`, `TestStartMockProcessIsTheCmdFormWithABareCommand` |
| …including a deliberate PANIC | `TestMockProcessReapedOnPanicPath` (re-exec'd child spawns a mock and panics; parent asserts `kill(pid,0) == ESRCH`) |
| the reaper never touches a pid it did not spawn | `TestReapNeverTouchesAForeignProcess` (a process started outside the handle survives another handle's `Reap`) |
| idempotent on an already-exited process | `TestReapIsIdempotentOnAlreadyExitedProcess` (exits on its own, waited for, then `Reap` ×3) |
| spawn sites inventoried + migrated | §2 (inventory table); `grep -rn '\.Start\(\)' internal cmd \| grep -v internal/testutil/` → **no output** |
| no `pkill` / name matching / global scan | by construction (`mockproc.go` signals only `kill(-pgid)` for its own handle); the only `/proc` reader in the new code is the cwd ASSERTION in `TestStartMockProcessDirRunsTheChildInTheGivenDirectory` |
| helper usable from every site | `TestReaperKillsTheWholeSpawnedGroup` (a `nohup`-style grandchild dies with the group), `TestStartReapedProcessReturnsADeadPid` (shared dead-pid fixture), `TestStartMockProcessDirRunsTheChildInTheGivenDirectory`, `TestAllowStartFailureSkipsVisiblyAndRunsTheHook` |
| self-pgid / degenerate pgid refused | `TestReapRefusesItsOwnProcessGroup`, `TestReapRefusesDegenerateGroupID`, `TestStartMockProcessCmdRefusesAPresetPgid` |
| `Setsid` callers still get an own group | `TestStartMockProcessCmdKeepsASetsidCallersOwnGroup` (measured: `Setsid` + `Setpgid` = `EPERM`) |

### 5e. Coverage

`scripts/func_coverage.py` (metric unchanged, no script touched; `test_func_coverage.py`
still passes) — `internal/testutil` at `master`: 63 funcs / 29 covered / 46.0%;
on this branch: 74 funcs / 41 covered / 55.4%. Every function this part ADDS is
covered (`StartMockProcess`, `StartMockProcessCmd`, `StartMockProcessDir`,
`StartReapedProcess`, `Reap`, `Wait`, `Pid`, `Pgid`, `Cmd`, `Alive`,
`AllowStartFailure`, `OnSkip`); the remaining gap is pre-existing
(`StartGopi`, `MockEnv`, the SSE/session helpers) and unchanged here.

## 6. Rule-notes

- No product-code change: everything is under `internal/testutil/`, `*_test.go`,
  and this devdoc.
- No `pkill -f`, no name matching, no global process scan anywhere in the new
  code or the migrated sites; the only signals are `kill(-pgid)` for a group the
  handle spawned, plus the pre-existing product-side logic untouched.
- Rule 1a/1b: `reaperSIGTERMGrace`, `reaperSIGKILLGrace`, `reaperPollInterval`,
  `reaperMinPGID`, `MockDaemonBinary`, `MockDaemonHoldSeconds`,
  `covXLockDir`/`covXLockPrefix`/`covXLockSuffix`, `versionBuildDirPrefix`,
  `homeSettingsBuildDirPrefix` — no inline timeouts or bare `"sleep"` repeats.
- Rule 5/7: build + test + commit + this doc.
