<!-- doc-class: historical -->
# The memory-sync child's working directory — TWO seams, ONE decision

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **Retired mechanism — reclassified `historical` 2026-10-27.** Both seams this page
> is about are DELETED, and so is the decision they were made to share. Seam ① (the
> resident memory-sync tick) was retired 2026-10-25
> (`devdoc/memory-sync-tick-retired.md`); seam ② (the store-triggered round) was
> deleted 2026-10-27 in `5e3ce2e` together with
> `internal/tools/memory_store_trigger.go` and `internal/tools/memory_sync_child_cwd.go`
> — verify with `grep -rn --include=*.go -w DecideMemorySyncChildCwd internal/ cmd/`
> (zero definitions at HEAD). The page was `normative` and opened with
> "**Status:** fixed … and what **now** prevents it structurally"; both halves of that
> sentence now name code the tree has not got, so it no longer teaches an action a
> reader can take. Nothing below is rewritten (Rule 36: a record quotes what was
> typed that day) — the numbers, the `file.go:NNN` references and the red-proof
> transcript are the state of THAT run. What survives and is still worth reading is
> the **"Traps worth knowing"** section: `TaskGroundInUse`'s self-chain exclusion and
> `task status`'s exit-code verdict are live machinery, unretired. Current state of
> the store→sync leg: `devdoc/memory-store-sync-decouple.md`.

**Status:** fixed 2026-10-25 (task `01a17050`), after an earlier fix
(`5b06b59`, 2026-10-22) had already fixed *one* of the two seams.
**Subject:** why `gopi memory sync --push` kept inheriting a task ground, and what
now prevents it structurally.

## The symptom, as reported

A worker's memory-sync tick logged

```
[memory-sync-tick] child keeps the inherited directory /: no live process stands in a task ground there…
```

while the child it had just started reported `readlink /proc/<pid>/cwd` =
`/home/user/workspace/01a16fe2-…` — a *task ground*. The consequence was measured,
not inferred: for the 315–534 s a push took, `gopi task status <that row>` printed
`ground_now: IN USE by live process …` and exited `2`
(`taskStatusExitGroundInUse`), so the row's own machinery vetoed its own
reclamation, and the worktree could not be recycled.

## The root cause: the log line and the child belonged to different seams

The report quoted **one** line as if the tick's own child contradicted it. It did
not. Two *production* seams start the same verb:

| seam | where | argv | escaped its inherited cwd? |
|---|---|---|---|
| **tick** | `cmd/agent/memory_sync_tick.go` (`memorySyncTickRun`) | `memory sync --peer P --push` | **yes** — since `5b06b59` |
| **store-triggered round** | `internal/tools/memory_store_trigger.go` (`memoryStoreSyncRunChild`) | `memory sync --peer P --push --db D` | **no** — `exec.CommandContext` with no `Dir` |

`5b06b59` built the decision in `cmd/agent`, where the second seam **cannot reach
it**: `internal/tools` imports nothing from `cmd/agent` (doing so would invert the
dependency). So the store-triggered round kept `exec.CommandContext` with no
`cmd.Dir` and the child simply inherited whatever directory the caller stood in —
and the caller is a `gopi worker` (or a `gopi chat`) whose cwd can be the run's own
ground.

**How the two were told apart, from the evidence alone**: the child's argv carried
`--db`. `memoryStoreSyncArgv` is the only argv builder that adds it; the tick's
`memorySyncTickArgv` does not. The line above it was the tick speaking about *its
own* (correctly escaped) child.

```
CHILD  pid=694319  gopi memory sync --peer peerbox --push --db /home/user/.gopi/memory.db
       ppid=545796
       /proc/694319/cwd = /home/user/workspace/01a16fe2-…   ← the ground
```

`ppid` and its own `/proc/<ppid>/cwd` were the ground too, so the inheritance was
from a launcher that was standing in it — not from the child's own chdir.

## What was done

The decision moved to `internal/tools/memory_sync_child_cwd.go`
(`DecideMemorySyncChildCwd`), where **both** seams can reach it, and both now call
it. One copy is the point: two copies is exactly how this defect survived its own
fix.

The decision's shape, and why each part is there:

1. **Ask the guard's own question — and then one rung further, because the
   guard's answer is wrong exactly when the defect fires.** The first draft asked
   only `TaskGroundInUse(inherited)` (the predicate `finishCleanupWorktreeInUse`
   runs) and was measured to be **insufficient**: that predicate deliberately
   excludes the caller's own process chain (`finishCleanupSelfChain` — the
   close-out runs *inside* the run that occupied the worktree, so a ground whose
   only occupant is the caller is recyclable). At the moment a sync child is
   spawned the caller is the **only** occupant — the child does not exist yet — so
   "no holder" is what the predicate says precisely in the incident's shape. The
   ground is therefore read on **three rungs, any one sufficient**: ① a live
   process outside the caller's own chain stands in the inherited directory
   (`TaskGroundInUse`); ② the inherited directory **is (inside) the ground this
   process was started on**, per the launcher's own published record
   `GOPI_RUN_GROUND` (`TaskGroundDirEnv`); ③ the inherited directory lies in a
   **linked git worktree** (`.git` is a *file*) — which is what every gopi task
   ground *is*, `git worktree add … -b task/<id>`. Rung ② is what makes the
   decision fire in the real shape; rung ③ keeps it honest when a caller stands in
   a worktree-shaped ground without the announcement (measured: `cmd/agent`'s own
   `TestMemorySyncTickSpawnsChildOutsideGround` does exactly that, and the
   two-rung version answered "neutral" there — the suite caught it); rung ①
   catches a caller in some *other* run's ground, which no declaration names. The
   **main checkout is never a ground** (its `.git` is a directory): it is where the
   operator works, and moving children out of it protects nothing.
2. **Escape only when it buys something.** A worker standing in `~` or a session
   directory vetoes nothing, so nothing is moved for it (no churn, no path a
   `--db` fallback might care about).
3. **Never weaken the guard, never exempt the process.** No arm of
   `finishCleanupWorktreeInUse` / `TaskGroundInUse` / `reclaimSkipDirtyReason`
   changed, and no process is excused by name. The cure is the child's directory,
   not the guard's tolerance.
4. **Rule 22 by measurement.** The escape root is `scratch.HomeDir()`, and it is
   used only after `scratch.DeviceID` proves it is on a different device from `/`
   (never by spelling), and only when it lies inside no declared worktree root and
   not inside the ground it is escaping.
5. **A refusal is named, never silent** (Rule 15). If no root can be proven, the
   child keeps the inherited directory and the line says *why* — an unmeasurable
   device, a same-device root, an uncreatable directory, or no scratch home at all.

The tick keeps its own seam but calls the shared decision, so its line and its
child still agree:

```
[memory-sync-tick]        [resident tick]           child cwd … (left the inherited directory …)
[memory-sync-on-store]    [store-triggered round]   child cwd … (left the inherited directory …)
```

The seam label is a constant on each side (`memorySyncTickCwdLabel`,
`memoryStoreSyncCwdLabel`), so an operator reading a worker log can tell which seam
moved a child — the exact distinction whose absence caused this report.

## Why the earlier fix's own test did not catch it

`cmd/agent/memory_sync_tick_cwd_test.go` drives **`memorySyncTickSpawn`** — the
tick's seam. It stubs the spawn and asserts on the directory the tick *decided*.
That test was, and is, correct: the tick really does hand its child a non-ground
directory. The defect lived in a seam that test never touches, in a package that
test cannot even see. **A test that pins one seam's decision proves nothing about
the other seam's behaviour** — the regression guard therefore drives the *other*
seam (`internal/tools/memory_store_trigger_cwd_test.go`), starts a **real**
process, and reads that process's own `/proc/self/cwd`.

## The regression guard, and its red proof

`TestStoreTriggeredSyncChildDoesNotInheritGround`:

1. starts a live `sleep` inside the ground (a plain `t.TempDir()`, so no real
   repository is touched — Rule 29), because `finishCleanupWorktreeInUse`
   deliberately excludes the caller's own chain and a ground occupied **only** by
   the caller is correctly recyclable;
2. asserts `TaskGroundInUse` sees that occupant (a fixture nobody checks is a
   vacuous test);
3. points the seam's child at a one-line shell script that prints
   `readlink /proc/self/cwd` and exits — a real process, no ledger, no network;
4. asserts the measured cwd is **not** the ground, that it is provably off `/` by
   device id, and that the production line names both directories and the seam.

**Red proof (measured, not described):** removing the two lines that apply the
decision in `memoryStoreSyncRunChild` makes the test fail with

```
MEASURED child /proc/self/cwd = /dev/shm/suite-tools-run-…/001 (launcher cwd = …/001)
the child INHERITED the ground "…/001" — pre-fix behaviour…
```

Restoring the file and re-running is green; the restore was verified by comparing
`git diff HEAD -- internal/tools/memory_store_trigger.go` against the pre-mutation
baseline (`diff` empty, 48 lines both sides) — an index-relative diff would not
have been evidence of a restore.

## The end-to-end acceptance run (real binary, real ledger)

The decision was then exercised on the **real** production seed with the **real**
`gopi` binary, with the launcher genuinely standing inside this task's ground and a
separate occupant process holding it (the production condition):

```
MEASURED child /proc/2874115/cwd = /home/user/workspace/gopi-test-scratch
REPORTED [memory-sync-on-store] [store-triggered round] child cwd
         /home/user/workspace/gopi-test-scratch (left the inherited directory
         /home/user/workspace/01a17050-… behind: …)
ACCEPTANCE-1 OK: the line names the measured directory
ACCEPTANCE-2 OK: child cwd is outside the ground
ACCEPTANCE-3 ground_now:  no live process stands in it
ACCEPTANCE-3 OK: exit=1 (not the ground-in-use verdict 2) and the ground reads free
```

`exit=1` in that last line is `taskStatusExitRunning` — correct, and not what this
bug produced: the row **is** the in-flight run, so a resume must still be refused
for that reason. The bug's own verdict was `2`, and `2` is gone.

## Traps worth knowing (each one cost time here)

- **`TaskGroundInUse` excludes the caller's own process chain**
  (`finishCleanupSelfChain`). A test — or a decision — that asks "does a live
  process stand in my own cwd" from *inside* that process gets `no holder`. An
  occupied ground in a fixture needs a **separate** process.
- **A process orphaned into a ground keeps vetoing it forever.** Two diagnostic
  runs left their probe children alive after the harness reaped the parent; each
  one made the live row read `IN USE` minutes later, with the argv naming the
  orphan. Reap what you spawn (Rule 42), and when a row reads `IN USE`, read the
  argv in the report before believing it is the production code.
- **`task status`'s exit code is a verdict, not a liveness probe**: `1` = live
  holder, `2` = ground occupied, `3` = unknown, `5` = status forbids. Piping it
  makes `$?` the pipe's, so redirect to a file (Rule 4.10).
- **Do not search the whole report for "IN USE".** This very task's summary
  quotes the bug's wording, so a substring search over the document matches its own
  title; assert on the `ground_now:` line.
- **The suite pins `GOPI_MEMORY_DB`** at a throwaway test database, so a `gopi`
  subprocess started from a test cannot see the operator's rows at all — it answers
  `no task with task_id=…`. A test that wants the real ledger must drop that
  variable from the child's environment.
