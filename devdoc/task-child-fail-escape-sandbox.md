<!-- doc-class: normative -->
# Devdoc: the sandbox precondition on the hermetic test escapes

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Motivating incident: **2026-09-16, 5 real operator rows burned in 6 seconds.**
Parent ledger id `01a0a92d-77a0-7eb0-90a6-8c5f63051177`; payloads under
`/home/user/.gopi/sessions/01a0a92d-*/metadata.json`.

## What broke (root cause, not the symptom)

`cmd/agent/cli_tasks.go` read the hermetic test escape straight from the
environment:

```go
if strings.EqualFold(strings.TrimSpace(os.Getenv(taskChildFailTestEnv)), "1") {
    // …write the row back as `failed`, elapsed=0s, synthetic error
}
```

That variable was **the entire authority**. Any process holding it — and every
process it spawns, because environment is inherited — could write back every
claimed ledger row as `failed` with no attempt and no judgement. A resident
`gopi worker` inherited `GOPI_TASK_CHILD_FAIL_TEST=1` from its launcher shell,
claimed five real operator rows and terminally burned them:

| row | claimed_at | status |
|---|---|---|
| `01a0a92d-21a5-…` | 15:57:26 | failed |
| `01a0a92d-2361-…` | 15:57:37 | failed |
| `01a0a92d-560e-…` | 15:57:47 | failed |
| `01a0a92d-575d-…` | 15:57:56 | failed |
| `01a0a92d-77a0-…` | 15:58:06 | failed |

All five carry `claimed_by = peer1:558874` and the escape's fingerprint detail.
The ledger they were burned in was the operator's **real** `~/.gopi/memory.db`.

**Measured correction to the count**: the incident actually burned **six** rows
in that window, across two parents. The sixth is
`01a0a4a4-48cf-7515-bcdb-11cfcb609fd7` ("merge verify retry:
fix/peak-orphan-audit") at 15:58:20, with the same synthetic detail. It is not
in the table above because it belongs to a different parent — and it is the one
nobody counted, because a merge-verify row is re-derived by the merger on its
next tick. The count matters for the guard's justification, not for the fix: a
gate above the first claim is indifferent to how many rows the next incident
would have taken.

**Re-verified on the closed tip (`e60a2d38`)**, real binary, both directions:
armed against the real profile → refusal + real ledger still at exactly those 6
synthetic rows, zero new; armed inside a throwaway profile under the measured
temp root → no refusal, the escape fires as the hermetic suite requires.

## The fix

The escape is now bound to its sandbox. `cmd/agent/task_child_fail_guard.go`:

1. **`taskChildFailSandboxRoot()`** — a resolved root is a sandbox only when it
   is BOTH inside a measured temp root AND off the `/` partition, and never
   inside the operator's live profile. Two paths decide where a run lands, and
   either one alone relocates it, so both are measured: the ledger
   (`config.LedgerPath()`) and the gopi data root (`config.GopiDir()`).
2. **The gate sits before any claim.** `runTaskWorkerLoop` (worker face →
   `os.Exit(96)`) and `runTaskRunOnce` (funnel face → returned error) both call
   it at their entry, above the first claim. A refusal therefore changes **no**
   row's status — which is the whole point. A gate placed after the first claim
   would still burn one row.
3. **The refusal is loud** (Rule 15): stderr carries the resolved path that was
   measured and found on `/`, the measurement that condemned it, the incident's
   own numbers, and the fix. It is never a silent pass and never a silently
   ignored variable.

### Why "inside a temp root", not merely "off `/`" — a measured false negative

The obvious predicate ("is this path off the `/` partition?") **does not work on
this host**, and the measurement is the reason this design is what it is:

```
$ stat -c '%d %n' / /home /home/user/.gopi
2051 /
2052 /home
2052 /home/user/.gopi
```

`/` is device 2051, `/home` is **2052**. The operator's real profile lives on
`/home`, so a device-only test calls the very ledger the five rows were burned
into a *sandbox*. The containment test is what separates them: a throwaway
sandbox is a **child of a temp root**, an operator profile never is.

`scratch.IsTempRootOffRootFS` (new, `internal/scratch/tmproot.go`) keeps the
measurement honest: when the path IS the effective temp root it defers to
`AssertTempRootOffRootFS` (the one existing definition of the temp-root ban);
otherwise it measures the path's device against `/`'s, judging a not-yet-created
path by its nearest existing ancestor. A path that cannot be measured returns an
error — **"cannot tell" is never "safe"** — so the caller refuses.

### Reduced: `internal/config.LedgerPath()`

The precedence (GOPI_MEMORY_DB > GOPI_HIPPO_DB > `<gopi dir>/memory.db`) existed
unexported in `internal/tools`. The guard needs the same answer one process
earlier and may not import that private resolver, so the resolver was extracted
to the package both sides already import (`internal/config`) — no new dependency
edge, no second copy (Rules 13/14).

### Rule 22 compliance of this change itself

The fixture that reproduces the incident must live on `/` (that is the recorded
shape) and is therefore a **measured, deliberate** target, not a hardcoded
temp path: `unsandboxedProfileRoot` tries `/tmp` and `os.TempDir()`, checks each
against the measured device of `/` and the measured temp roots, and **skips with
its reason** when nothing qualifies. Nothing in the change creates temp work on
`/`; the production default temp-root policy is untouched.

## The proof

* **RED before the fix** (Rule 10.5). With the guard stashed and only the repro
  re-added:
  `TestTaskChildFailEscapeRefusesOutsideSandboxE2E` fails with
  `an armed escape against a NON-sandbox ledger must be refused with exit 96, got exit=0`
  — the pre-fix binary accepted the escape and the fixture row came back
  `failed` with the synthetic detail, i.e. the incident reproduced end to end.
  The repro is deliberately written to compile against the pre-fix tree (its
  mirror helpers and the `96` literal are local, pinned to the shipped constant
  by `TestTaskChildFailRefusalExitCodeMatchesTheRepro` /
  `TestRootPartitionReproMatchesTheConstant` / `TestReproPredicateMirrorsTheProductionGuard`).
* **GREEN after.** `go test ./cmd/agent/ -run TestTaskChildFail` — 8 tests pass,
  including the pairing control (the escape still fires inside a sandbox, which
  is what keeps `TestRunTaskWorker_SurvivesFailedRun` green) and the in-process
  proof against the operator's REAL profile path.
* **Live E2E, both directions** (real binary, throwaway ledger on `/`):
  `gopi worker --once` with the escape armed → exit **96** with the refusal on
  stderr, and the pending row was **still `pending` with zero `task_events`**
  afterwards; the same binary in a throwaway sandbox under the temp root still
  failed its row synthetically, as the hermetic tests require.

## Sibling escapes audited (same family, `grep`-verified)

| escape | reaches production? | verdict |
|---|---|---|
| `GOPI_TASK_CHILD_FAIL_TEST` | yes — failed rows with no precondition | **fixed here** |
| `GOPI_SKIP_CLAIM_BROADCAST` | yes — `cli_tasks.go` claim path | **made a constant** (`config.SkipClaimBroadcastEnv`, Rule 14: two readers — the claim path and 20 test fixtures — must name one variable; `internal/config/help.go` and the settings catalog reference it now). Not sandbox-gated: it only *skips* a remote CAS leg, it cannot fail, complete or otherwise decide a row, and 20 fixtures plus the single-node production default depend on it. Documented non-change. |
| `GOPI_WORKER_MAX_ROUNDS` / `GOPI_WORKER_IDLE_EXITS` / `GOPI_WORKER_IDLE_SEC` | yes — worker loop shaping | **documented non-change**: each has a named constant and a bounded effect. A malformed value falls back to the default with a warning (it can neither unbound the loop nor fail a row), and the maximum blast radius is this worker's own lifetime — it never writes a row's terminal state. |
| `GOPI_SKIP_TERMINAL_PUSH` | yes — `internal/agent/taskregister.go` | **documented non-change**: skipping the terminal push *loses* a sync, it never fabricates a terminal state; the event is logged rather than silent, and the same reasoning as the broadcast switch applies. |
| `GOPI_RAW_LOG=OFF` | yes — `internal/agent/rawlog.go` | **documented non-change**: it discards log text, it decides nothing about a row. |

The distinction the table applies: an escape that can **fabricate a row's
terminal state** is a sandbox-gated hazard; an escape that only *skips an
optional leg* is a knob, and const-naming it is the right-sized treatment
(Rules 1b/13/14).

## Files

* `cmd/agent/task_child_fail_guard.go` (new) — the guard, the constants, the refusal.
* `cmd/agent/task_child_fail_guard_test.go` (new) — unit + real-profile proof.
* `cmd/agent/task_child_fail_sandbox_guard_e2e_test.go` (new) — the RED-before-fix repro + the pairing control (pre-fix-compilable by design).
* `cmd/agent/cli_tasks_worker.go`, `cmd/agent/cli_tasks.go` — the two gates (+ the funnel now shares `taskChildFailEscapeArmed`).
* `cmd/agent/cli_tasks.go` — the claim-broadcast read moved onto its constant.
* `internal/scratch/tmproot.go` — `IsTempRootOffRootFS`, `RootDeviceProbePath`.
* `internal/config/memory_env.go` — `LedgerPath()` (single definition of the precedence).
* `internal/config/settings.go` — `SkipClaimBroadcastEnv` (+ catalog/help reference it).
* Hermetic fixtures — every arming site now names its sandbox in the SAME
  fixture (`GOPI_HOME`/`GopiDirEnv` + `GOPI_MEMORY_DB` together): `cli_serve_test.go`,
  `serve_worker_split_test.go`, `singlenode_serve_test.go`,
  `cli_tasks_worker_idle_exit_test.go` (via `workerIdleSandbox`),
  `worker_drain_unit_test.go`, `task_run_release_coverage_test.go`,
  `cli_tasks_worker_unknown_arg_test.go`, `agent_spawn_guard_test.go`,
  `cli_tasks_singleton_guard_test.go` (+ the 20-file broadcast-constant backfill).
