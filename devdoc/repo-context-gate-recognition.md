<!-- doc-class: historical -->
# Task Report: the repo-context gate's recognition shape was wider than the tree it polices

Branch `fix/spawn-gate-recognition` (worktree `/home/user/workspace/gopi-spawngate`), off
master `a9a96cd7`.

## The defect, as measured

`go test ./cmd/agent/ -run '^TestSpawnersPinTheRepoContext$'` on master `a9a96cd7`:

```
repo-context gate: escaped spawn site (documented): worker_daemon_singleton_e2e_test.go:63: wdForeground()
the scan found only 71 lifecycle spawn site(s), expected at least 80 —
    the verdict would be vacuous (the scan's shape drifted from the tree)
```

Two independent failures, and they are NOT the same failure:

1. **The scan lost 14 sites.** The floor (80) was derived from a measured 85 at
   `a1230099` (the round that raised it). Instrumenting the recognizer at both
   revisions and diffing the recognized-site sets gave an exact account:
   - **13 files were DELETED** on master by the merger-cronjob refactor
     (`fb877170`, "the merger is a cronjob — remove the daemon machinery") —
     verified by diffing the tracked test-file list between `a1230099` and
     `master`: `merger_loop_e2e_test.go`, `merger_loop_test.go`,
     `merger_restart_e2e_test.go`, `merger_restart_takeover_contract_test.go`,
     `merger_retired_flags_test.go`, `merger_singleton_e2e_test.go`,
     `merger_status_cli_test.go`, `merger_stop_holder_child_test.go`,
     `merger_takeover_cgroup_acceptance_test.go`,
     `merger_takeover_gate_e2e_test.go`, `merger_takeover_launcher_test.go`,
     `merger_tick_legs_test.go`, `merger_top_level_e2e_test.go`. Master has 192
     `cmd/agent` test files against 200 at `a1230099`. Among the files that
     SURVIVED, per-file recognized counts did not drop — so the loss is
     deletions, **not** missed sites.
   - **The new `gopi merge` command** (epic merge-command-foreground,
     2026-09-18: `merge_run.go`, `merge_lock_e2e_test.go`, `merge_lock_test.go`)
     arrived as a repo-mutating verb the recognition set had never learned.
   - **The qualified-constant argv spelling** was invisible:
     `merger_work_lock_e2e_test.go` spells its argv `{CliVerbTask,
     taskverbs.Merge, …}`, and `rcsExprVerbValue` returned nothing for a
     `SelectorExpr`.
2. **The escaped site was not accounted for.** `worker_daemon_singleton_e2e_test.go`
   `wdForeground()` carries a `repo-context-ok:` marker whose reason is *the
   caller builds the env vector* — but the gate's marker-count invariant saw a
   marker on a function it did not recognize, and the escape fell through with
   no matching recorded-escape row. One marker, no corresponding escaped site:
   exactly the drift the invariant exists to catch.

So the honest answer to "is 71 the true count?" is **no** — 71 was the
recognizer's blind spot plus a genuinely shrunken tree, and the two must be
separated before anything can be said about the floor.

## What was done

Recognition was widened along the criteria (never by lowering the floor — the
floor is untouched at 80):

- **`rcsInProcessLifecycleDrivers`** — the package-local functions that run a
  close-out pass **in this process**: the four the gate already named
  (`runTaskWorker`/`runTaskMerger`/`runTaskRunOnce`/`runServeLoop`) plus their
  two siblings on the same tick, `serveMergedCleanupSweep` and
  `serveWorktreeReapSweep`. This is the **live hazard** the epic uncovered:
  `serveMergedCleanupSweep()` resolves its repo through
  `tools.GitSyncRepoRoot()` and its callers stood in the package directory, so
  three tests were driving a merged-cleanup close-out against the developer's
  live checkout with no pin and no recognition at all. It is a real defect, not
  a counting artifact.
- **`gopi merge`** (`rcsVerbIdentMerge`) and **the merger work faces**
  (`rcsMergerWorkFaces`: `taskverbs.Merge`/`MergeExecute`/`MergedCleanup`/
  `Adopt`/`AuditMerges`) — the same repo-mutating engine as a merger tick.
- **`rcsExprVerbValue` reads `*ast.SelectorExpr`** as `pkg.Name`, so a table
  that names a constant instead of typing its value is visible. (`*ast.Ellipsis`
  too, for spread argv.)
- **The usage-query arm** now matches the product's own help-flag constant names
  (`mergeHelpFlag`), because `mtlProbe(t, bin, env, CliVerbMerge, mergeHelpFlag)`
  is a zero-side-effect probe, not a spawn.
- **The daemon arm is DERIVED, not listed** (`rcsDaemonHandingHelpers`): a call
  whose daemon-ness lives inside the helper it reaches (`dcStartDetached` →
  `wdRun(… daemonFlagLong)`) is answered by reading the helper's own body. A
  helper that stops detaching stops being excluded in the same commit.
- **A pin conferred by a pinned runner** (`rcsPinnedRunnerIndex`): calling a
  runner that itself pins the child (e.g. `consoleE2EWorkerStreams` sets
  `cmd.Dir = t.TempDir()`) is a pinned spawn. Built by the same predicate, so it
  cannot drift.
- **`rcsFunctionMovesOwnWorkingDir`**: for an in-process driver — which spawns
  no child and therefore has no `cmd.Dir` — moving *this* process out of the
  live checkout (`t.Chdir`/`os.Chdir` with a non-empty argument) is the pin.

Three real pins were added / one escape recorded:

| site | disposition |
|---|---|
| `cli_tasks_merged_cleanup_test.go` `TestServeMergedCleanupSweepThrottle` | **pinned** — `SandboxRepoContext(t)`; it was driving a real close-out against the live checkout |
| `cli_tasks_merged_cleanup_test.go` `…RemovesFrozenPair` | already pinned via `t.Chdir(wd)` + `GOPI_SETTINGS`; now *recognized* |
| `merge_lock_e2e_test.go` `mergeE2EStartRun()` | **reasoned escape** — its call sites pin (`SandboxRepoContext(t)`) before building the env, and the helper cannot re-pin (the env is the caller's argument; a second chdir would move the child off the fixture) |
| `worker_daemon_singleton_e2e_test.go` `wdForeground()` | **reasoned escape** (pre-existing marker); now a *matching* escaped site, so the marker invariant holds |

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0.
- Gate green, floor untouched:
  ```
  repo-context gate: 82 lifecycle spawn site(s) scanned, 62 pinned, 2 escaped,
      18 daemon-start (own-dir) site(s), 2 escape marker(s)
  --- PASS: TestSpawnersPinTheRepoContext
  ```
  (82 ≥ floor 80; 2 markers = 2 escaped sites.) Re-measured verbatim in the
  close-out round with `-v`: the two `escaped spawn site (documented)` log lines
  name `merge_lock_e2e_test.go:198: mergeE2EStartRun()` and
  `worker_daemon_singleton_e2e_test.go:63: wdForeground()` — the marker count and
  the escaped-site count agree.
- Scoped package families: `go test ./cmd/agent/ -count=1 -run 'TestServeMergedCleanup|^TestMergeLock|^TestMergeHelp|^TestMergeUsage|^TestMergerWorkLock_MergeFaces|^TestSpawnersPinTheRepoContext$|^TestRepoContextHazard|^TestWorkerDaemon_Status|^TestWorkerForegroundPrints'`
  → `ok github.com/nick/gopi/cmd/agent 30.177s`.

- Whole-package run (the gate inside a FULL suite, 928s wall):
  `go test ./cmd/agent/ -count=1 -timeout 60m` → the gate itself never
  appears among the failures. The run does report ONE `--- FAIL`:
  `TestWorkerServerDeliveryRecyclesThenMergerMerges` — **pre-existing on master,
  unrelated to this branch**: it drives `gopi task merger --once`, a face the
  resident-merger retirement (`f9ff1a89`, in master) answers with
  "merger … retired … use the one-shot `gopi merge` face". Reproduced
  identically on a pristine detached worktree at `a9a96cd7` with this branch
  absent. Filed as task `01a0b578-105d-73d3-b9ad-13349d6f0c14` (no branch
  re-architecture is parked on the finding).
  Note on the default budget: the same command WITHOUT `-timeout` panics at
  Go's 10m package budget (`FAIL … 601.5s`), which hides the assertion failure
  behind a goroutine dump — raise the budget to see the real verdict.

### Red-then-green mutation proof (every arm proven live)

| mutation | result |
|---|---|
| remove `SandboxRepoContext(t)` from `TestServeMergedCleanupSweepThrottle` | RED twice over: (a) the site is enumerated as an offender — `1 function(s) spawn a real gopi worker/merger WITHOUT the repo-context sandbox: cli_tasks_merged_cleanup_test.go:74: TestServeMergedCleanupSweepThrottle()`; and (b) the scan drops to 79 — the site leaves the *recognized* set together with its pin (the cwd-move arm is what recognizes it), so the same removal also trips `found only 79 … expected at least 80` |
| delete the `repo-context-ok:` marker above `wdForeground` | RED naming `worker_daemon_singleton_e2e_test.go:62: wdForeground()` |
| drop the `CliVerbMerge` recognition arm | RED: `found only 79 … expected at least 80` |
| drop the in-process-driver recognition | RED: `found only 79 … expected at least 80` |
| disable `rcsMergerWorkFaces` and remove the pin at `TestMergerWorkLock_MergeFacesRefuseWhileAHolderRuns` | RED names that site — **and the same removal with the arm disabled counts 0 offenders**, so the arm is measurably what saw it |
| drop the daemon-helper derivation | RED naming `daemon_cgroup_e2e_test.go:236` and `:285` |

## Not Done (honest)

- **A `cmd/agent` whole-package run is NOT green on this branch, and the red is
  not this branch's.** `go test ./cmd/agent/ -count=1 -timeout 60m` → one
  `--- FAIL: TestWorkerServerDeliveryRecyclesThenMergerMerges` (a stale caller
  of the retired `gopi task merger --once` face), measured identically on a
  pristine detached worktree at master `a9a96cd7` with this branch absent →
  PRE-EXISTING master-red, not a regression. Dispatched as task
  `01a0b578-105d-73d3-b9ad-13349d6f0c14` rather than fixed here (Rule 26: an
  incomplete fix is repaired by dispatching again; Rule 28: it touches the
  merger/scheduler layer, which is not this task's scope).
- **The floor itself was NOT re-derived.** It did not have to be: with the
  recognition fixed the scan measures **82** against the incumbent floor of 80,
  so the non-vacuity guard holds on its own terms. Re-deriving it (82 → a new
  floor) is a separate judgement about headroom and is deliberately left to the
  next round; a floor set from the exact current count would fail on the next
  legitimate deletion.
- The `devdoc/repo-context-gate-wdrun.md` "read-floor 15 → 80 against a measured
  85" sentence is now describing a superseded measurement (85 was the pre-deletion
  tree). It is left as the historical record of that round, not edited.
