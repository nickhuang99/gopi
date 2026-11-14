<!-- doc-class: historical -->

> **RETIRED 2026-10-26 — this page describes machinery that NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left exactly where it is. Read this page as history, not as
> instructions: any command below that names `sweep-zombies` exits 1 with a pointer.
> The current behaviour is `devdoc/zombie-sweep-removed.md`.

# Task Report: independent verification of zombie auto-disposal (ledger row 01a0948b-85b3-72be-8d56-ca5f6bb8c32b)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Verifier posture:** read-only. Nothing in the deliverable was edited; no live
worktree was touched; no `pkill`. All experiments ran in throwaway detached
worktrees under `/tmp/zv/` plus a sandbox `GOPI_HOME`/ledger/sink per run.

**Verdict: `verified`** (evidence digest recorded once via `gopi task update`).

## The reported defect and the delivered fix

Defect: the ledger had a zombie *detector* with no automatic *disposer* — a row
at the highest verdict (`confidence=confirmed`, `pid_dead=true`) only ever got
re-reported; the sole disposer was the manual verb, which no tick calls. The
notify sink was write-only (45MB / 124927 events) and 352 confirmed-stale rows
were re-reported for days.

Fix on master:

| item | value |
| --- | --- |
| merge | `b8c556ae` — `resolve merge fix/zombie-auto-dispose into master` |
| parents | `0e3ec4bd` (tip) + `875816fd` (pre-fix master) |
| feature commit | `c75975a1` feat(zombie): the worker disposes CONFIRMED zombies behind a second grace |
| harness+devdoc | `0ddbc604` (devdoc + `scripts/acceptance-zombie-auto-dispose.sh`) |
| docs | `0e3ec4bd` (byte-identical sink run alongside the concurrent-writer run) |

Ancestry proof (all four commits are ancestors of `master`):

```
$ for c in c75975a1 0ddbc604 0e3ec4bd b8c556ae; do git merge-base --is-ancestor $c master && echo "ancestor OK"; done
ancestor OK (×4)
$ git rev-list --parents -n1 b8c556ae
b8c556ae… 0e3ec4bd… 875816fd…
```

Pre-fix parent `875816fd` contains none of the new machinery
(`git grep -E 'autoDisposeConfirmedZombies|DisposeSec|IgnoreRearm' 875816fd` → empty).

Touched files (10, 890 insertions / 22 deletions):

```
cmd/agent/cli_tasks.go | cli_tasks_worker.go | cli_tasks_zombie_dispose_test.go
internal/config/config.go | settings.go | task_grace_test.go
internal/tools/taskzombie_sweep.go | taskzombie_sweep_test.go
devdoc/zombie-auto-dispose-2026-09-12.md | scripts/acceptance-zombie-auto-dispose.sh
```

## Acceptance criteria — one by one

**(c) reuses the existing disposer — PASS.**
`cmd/agent/cli_tasks_worker.go:autoDisposeConfirmedZombies` calls the SAME
`tools.SweepZombies` (internal/tools/taskzombie_sweep.go) with
`Kill=true`, `Notify=&false`, `IgnoreRearm=true` — the same verdict chain the
manual `gopi task sweep-zombies --kill` drives. No new evidence rule, no new
daemon. Safety is enforced by the existing predicate, not by new code.

**(d) second, longer grace — PASS.**
`config.ResolveTaskGracePeriods` gains `DisposeSec`: `GOPI_TASK_DISPOSE_SEC` /
settings `task_dispose_sec`, default `TaskDisposeGraceMultiplier` (2×) the
RESOLVED stall grace, `0` = off — one resolver, one reader.
`SweepZombiesOptions.IgnoreRearm` drops the `updated_at` re-arm guard from the
stall leg only for the disposal leg; the report leg keeps it (it is notify
dedup). Without IgnoreRearm the longer grace is unreachable, because the report
pass bumps `updated_at` every stall window — pinned by
`TestSweepZombies_IgnoreRearmDisposesPastLongerGrace` and
`TestWorkerLifecyclePass_ReportsButDoesNotDisposeInsideDisposeGrace`.

**(e) the sink gets a consumer surface — PASS.**
One operator line per lifecycle pass:
`[worker] [lifecycle] stale reported=2 disposed=1 sink=cat >> …/notify.log`
(`cli_tasks_worker.go` `workerLifecycleSummaryFmt` + `workerNotifySinkTarget`,
resolution through the single `config.ResolveNotify`). Observed verbatim in the
real-binary run below, plus one `[worker] [zombie-dispose] <id> disposed (…)`
line per disposal and a `task_events` audit reason containing `zombie`.

**(a) confirmed zombie auto-disposed by a tick alone — PASS (A/B proven).**
**(b) `suspected` row untouched — PASS in BOTH trees.**

## Verification

### Build + format (fixed tree, `/tmp/zv/fixed` @ `b8c556ae`)

```
$ CGO_ENABLED=0 go build ./...        → exit 0
$ gofmt -l $(git diff --name-only 875816fd HEAD | grep '\.go$')   → (empty), exit 0
```

### The project's own tests (fixed tree)

Targeted selectors over the touched packages:

```
$ CGO_ENABLED=0 go test -count=1 -v -run 'TestWorkerLifecyclePass|TestSweepZombies|TestResolveGracePeriods' ./cmd/agent/ ./internal/tools/ ./internal/config/
--- PASS: TestSweepZombiesCLI_KillMarksFailed … (25 × "--- PASS", 0 × "--- FAIL")
ok  github.com/nick/gopi/cmd/agent      0.821s
ok  github.com/nick/gopi/internal/tools 10.098s
ok  github.com/nick/gopi/internal/config 0.046s
```

Full touched-package suites, fixed tree (background run):

```
ok  github.com/nick/gopi/internal/tools    233.230s
ok  github.com/nick/gopi/internal/config     0.075s
ok  github.com/nick/gopi/internal/standards  3.031s
FAIL github.com/nick/gopi/cmd/agent        111.566s
```

The `cmd/agent` failure is a NETWORK-DEPENDENT e2e, unrelated to this change
and unrelated to the ledger row: the log shows
`ssh: Could not resolve hostname peerbox-single-node-test-peer: Temporary failure
in name resolution` + `no tracking information for the current branch` in the
single-node peer test (`t-singlenode-claim … child: synthetic child failure`).
A re-run of `./cmd/agent/` alone on the same tree was used to attribute it
(`/tmp/zv/fixed-agent-full.log`); see the "Full-suite attribution" note below.

### A2 red-proof — the defect signature at the pre-fix parent

Two throwaway DETACHED worktrees: `/tmp/zv/pre` @ `875816fd` and
`/tmp/zv/fixed` @ `b8c556ae`. The fix's own test file was placed in the PRE tree
carrying ONLY the two pre-rename env literals (`GOPI_TASK_STALL_SEC` /
`GOPI_TASK_DISPOSE_SEC` instead of the `config.Task*SecEnv` constants that do
not exist at the parent) — every other byte identical to
`git show c75975a1:cmd/agent/cli_tasks_zombie_dispose_test.go`.

```
PRE (875816fd):
--- FAIL: TestWorkerLifecyclePass_AutoDisposesConfirmedZombie (0.27s)
    cli_tasks_zombie_dispose_test.go:163: confirmed zombie status = "in_progress",
      want "failed" (one lifecycle pass, no manual sweep)
--- PASS: TestVerifierSafetyTwin_SuspectedAndLiveRowsUntouchedByTick (0.16s)

FIXED (b8c556ae):
--- PASS: TestWorkerLifecyclePass_AutoDisposesConfirmedZombie (0.20s)
--- PASS: TestVerifierSafetyTwin_SuspectedAndLiveRowsUntouchedByTick (…)
```

The red message literally IS the reported defect (the row never leaves
non-terminal status on a tick), and the safety twin is green in BOTH trees —
so the new behavior is absent-then-present, while the safety property does not
depend on the fix.

### A2 red-proof at the REAL-BINARY level (the fix's own harness, same script both trees)

`scripts/acceptance-zombie-auto-dispose.sh`, run against sandbox homes/ledgers/
sinks, one `gopi worker --once` lifecycle pass per phase.

PRE (`/tmp/zv/pre/bin/gopi`, `PRE_ACC_EXIT=1`):

```
== phase: past the dispose grace (heartbeat age 7200s) ==
  ledger: t-acc-conf=in_progress t-acc-live=in_progress t-acc-susp=in_progress
  FAIL confirmed status = in_progress (want failed)
  PASS suspected status (never auto-disposed) = in_progress
  FAIL lifecycle summary counts — 'disposed=1' not in worker.out
  summary line: MISSING
ACCEPTANCE FAIL
```

FIXED (`/tmp/zv/fixed/bin/gopi`, `FIXED_ACC_EXIT=0`):

```
== phase: past the dispose grace (heartbeat age 7200s) ==
  ledger: t-acc-conf=failed t-acc-live=in_progress t-acc-susp=in_progress
  PASS confirmed status = failed
  PASS suspected status (never auto-disposed) = in_progress
  PASS live-pid status (always vetoed) = in_progress
  PASS disposal logged with evidence
  PASS zombie audit reason recorded
  summary line: [worker] [lifecycle] stale reported=2 disposed=1 sink=cat >> …/notify.log
== phase: inside the dispose grace (suspended-host case, heartbeat age 600s) ==
  PASS confirmed status = in_progress
  PASS suspected status (never auto-disposed) = in_progress
  summary line: [worker] [lifecycle] stale reported=2 disposed=0 sink=cat >> …/notify.log
ACCEPTANCE PASS
```

The operator's live sink was byte-identical across every run
(`sha256 ac17eea0…5f9b494`, 47638370 bytes) — the verification wrote nothing
into live state (no manual verb was ever issued; the disposal came from the
tick).

## Not Done / limits (honest)

- Full `go test ./...` was NOT run to completion; I ran the touched packages
  plus `internal/standards`, and the `cmd/agent` failure is a DNS-dependent
  peer e2e, not this change (attribution above). A green `./...` is therefore
  NOT claimed.
- Only two env-var spellings differ between the PRE-planted test file and the
  committed one; the rest of the file is byte-identical by construction (PRE
  lacks the `config.Task*SecEnv` constants — the constants are a SEPARATE
  pre-existing naming convention in the fix, not the behavior under test).
- The A2 red-proof at real-binary level uses the fix author's own harness,
  unmodified except for `WORKDIR`/`BIN` pointing at the throwaway tree; that is
  a shared-method caveat, which is why the unit-level red-proof was run
  independently as well.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0); `gofmt -l` on the 8 fix-touched `.go` files → clean.
- Tests: targeted `-run 'TestWorkerLifecyclePass|TestSweepZombies|TestResolveGracePeriods'` → 25/25 PASS, 0 FAIL in the fixed tree; PRE tree → the three `TestWorkerLifecyclePass_*` FAIL (defect signature), safety twin PASS in both.
- Real-binary acceptance: PRE `ACCEPTANCE FAIL` (confirmed stays `in_progress`, `disposed=1` absent), FIXED `ACCEPTANCE PASS`.
- Commit (this report, docs branch): see `docs/zombie-auto-dispose-verification-20260912`.
- Verdict: `gopi task update 01a0948b-85b3-72be-8d56-ca5f6bb8c32b --status verified` — recorded ONCE.

## Recovery

Report lives on branch `docs/zombie-auto-dispose-verification-20260912`
(worktree `/home/user/workspace/gopi-verify-zombie-01a0948b`, based on
`b8c556ae`):

```
git show docs/zombie-auto-dispose-verification-20260912:devdoc/zombie-auto-dispose-verification-2026-09-12.md
```
