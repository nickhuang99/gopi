<!-- doc-class: normative -->
# The worker's merge tick is retired — merging is `gopi merge`'s job

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **Retired mechanism (merge verb removal, 2026-11-01).** The spelling this page
> names in its own TITLE — the top-level `gopi merge` — was itself removed later
> (epic merge-verb removal): the word now answers exit **1** with a retirement
> sentence. What this page is ABOUT is unaffected: the worker still runs no merge
> pass, and merging is still a foreground act an operator performs. The surviving
> spelling of that act is the family one, `gopi task merge` (same engine, same
> single-instance lock), whose contract page is the FILE `man/gopi-merge.md`.
> Every `gopi merge …` in this document is the record of 2026-09-27.

**Date:** 2026-09-27 · **Task:** `01a0df2d4b03720ebfa78139e25f1f0e` · **Branch:**
`fix/worker-owns-worktree-not-merge`

## What was wrong

A worker round carried a **resident merge tick** (`cmd/agent/merge_tick.go`, cadence
`GOPI_MERGE_TICK_MIN` / settings key `merge_tick`, default 15 min) whose driver was a
single call from the loop:

```go
// cmd/agent/cli_tasks_worker.go, before this change
if !once {
    mergeTickSweep(workerRoleLabel)
}
```

`mergeTickSweep` resolved the cadence, decided it was due, and **spawned `gopi merge
--fast` through `cmd.Run()`** — a blocking child. Two consequences, both structural:

- the worker — the process whose whole job is to keep running tasks — **stopped
  running them** for the duration of the child;
- with two live workers, each spawned its own merge, and the child's single-instance
  flock meant the losers were refused or the winner held the lock for the length of a
  merge run (the run publishes a **45-minute hand-over budget**, `cmd/agent/merge_handover_budget.go:18`).

The operator's ruling: the tick's *existence* was the bug — `merge conflict 不是它的工作`
— a worker's job is the **worktree and the branch**: in before the child, out after it.

## §Evidence 1 — every path from the worker loop into merge machinery

**`mergeTickSweep` was the only one, and there is now none.**

The loop's round carries three lifecycle passes; each was read at its call site:

| pass | can it reach a merge? | why |
|---|---|---|
| `scratchGCSweep` | **no** | reclaims aged, unheld scratch/sandbox directories (`internal/scratch/sandbox_gc.go`); no git, no refs |
| `memorySyncTickSweep` | **no** | spawns `gopi memory sync --peer P --push` per declared peer (`memory_sync_tick.go`); one-way outward, never a merge |
| `mergeTickSweep` | **YES — the door** | spawned `gopi merge --fast` |

Nothing else in the loop reaches merge machinery: `grep -rn "mergeTick\|gopi merge"
cmd/agent/cli_tasks_worker.go` is now clean, and `TestWorkerLoopDoesNotMerge` fails if a
merge invocation returns. The worker does still *write the inputs* a merge consumes —
the branch, and the worktree it releases on close-out — and that is its job, not a
merge.

A **removal that left a second door would have failed**, so the assertion is a test,
not a reading: the test names the deleted entry point, the merge **verb constant** and
its literal (`"merge"`), so an invocation cannot come back through a renamed call.

## §Evidence 2 — the blocking claim, captured

Staged in a sandbox profile (`gopi sandbox create`), two `gopi merge --fast` runs
overlapping, one declared repo. The **second face is refused, and it does not wait**:

```
gopi merge: serve lock held by a live daemon: another merge process is already running
  (lock <sandbox>/.gopi/serve.lock held by pid 1903686) — refusing double-serve: two
  merger passes (or a legacy `task serve`) never double-merge
gopi merge: refused — another merge face holds the single-instance lock; this run
  decided NOTHING: no repo was walked, no ref was written, no branch was deleted, no build ran
```

So the observed behaviour of the second merge is **refuse-and-exit**, and the cost to a
worker is the *first* run's duration: the worker that spawned the holder is blocked
inside `cmd.Run()` until the holder finishes, and the holder's own ceiling is the
**45 minute** hand-over budget it publishes to its child. The contention the operator
saw from the task page ("cannot merge and is waiting for another merge's lock",
`briefings/brief-worker-merge-tick-contention.md:73`) is that holder/waiter pair — one
worker holding, the other's round standing still.

This is the record of the symptom **before** the code that caused it was deleted; the
lock, the `--fast` arm and the refusal text are untouched by this retirement.

## §Evidence 3 — what an operator who still sets the retired knob sees

**Not "nothing".** Both spellings are reported on the worker start path
(`cmd/agent/retired_tick_knobs.go` — re-homed from `merge_tick_retired.go` with the
verb — called from `tmpdirBootstrapFromArgs`, the last
point every worker-shaped verb passes through). Re-measured 2026-11-01 with the
rebuilt binary, `GOPI_MERGE_TICK_MIN=5 ./bin/gopi worker -h`:

```
worker: GOPI_MERGE_TICK_MIN is RETIRED: the worker no longer runs a merge pass at all: the resident merge tick (this cadence knob, its publisher and the worker-round call site that spawned `gopi merge --fast` as a BLOCKING child) was deleted 2026-09-27 — a merge is `gopi task merge`'s job, and a worker that runs one stops running tasks while two live workers contend for the merge flock (see devdoc/worker-merge-tick-retired.md)
  run `gopi task merge` in the foreground — the worker does not merge
```

The second line is `config.MergeTickRetiredReplacement`, the ONE statement of the
replacement. It read `run \`gopi merge\` in the foreground` until 2026-11-01 — i.e.
the retirement message instructed the operator to run the verb that the same epic
had just made refuse (`./bin/gopi merge` exits 1, pointing at `gopi task merge`) —
and the sweep that removed the verb from the docs found it because the sentence is
RENDERED, not just written.

Two facts worth stating, because each is a channel that would otherwise be silent:

- **The settings face is not self-reporting.** `FromSettingsFile` decodes with a plain
  `json.Unmarshal` (no `DisallowUnknownFields`), so an undeclared key is ignored
  **silently** — `MergeTickMin` no longer exists, so nothing in the decode notices
  `"merge_tick"`. The retirement therefore **reads the file**:
  `config.Config.SettingsCarriesKey` (same file, no re-decode into a struct).
- **The file keeps loading.** A `settings.json` written while the tick existed is still
  valid, with every other knob intact (`TestRetiredMergeTickSettingsKeyIsTolerated`
  pins the tolerance). The retirement reports; it does not refuse.

## What now drives unattended merging: **nothing, by design**

There is no timer, no systemd unit, no cron entry, no daemon and no replacement
driver, and none is to be added (Rule 35). A merge is a **foreground command an
operator runs** — `gopi merge` when this was written, `gopi task merge` since that
top-level verb was removed on 2026-11-01; that is the whole mechanism. The deleted
tick's own header had argued
for riding an existing process ("the process already alive on this host, not a cron
entry, not a systemd timer") — the retirement is that **no in-gopi driver is wanted**:
the host already has the thing that decides when to merge, which is the operator.

Nothing about the merge pipeline changed: the one-shot face, `--fast`, the
single-instance lock, the queue, the gates. Only the worker's copy is gone.

## F1 — the ledger printed `task ""` on the line after one that knew the id

Measured, adjacent lines of one session log:

```
SENDER UNSOURCED SIGNAL SIGINT TASK 01a0deb7-41d4-778a-ba4b-6c91ce448956 AT 2026-09-27T02:39:54+08:00
signal provenance — SIGINT (UNSOURCED — no gopi stop path announced this kill …)
the ledger will carry unsourced for task ""
```

**Two producers, one piece of knowledge.** The first two lines render the worker's own
claimed-round latch (`workerStopTargetTask()`); the ledger line was keyed on
`signalrec.CurrentTaskID()`, which reads the id the **launch handoff announced in the
environment** — and a resident `gopi worker` announces none, because it claims a
different row every round. Both were "the task id" to their author.

The fix is one reader, and the order is the fix:

```go
func stopVerdictTaskID() string {
    if id := strings.TrimSpace(workerStopTargetTask()); id != "" {   // OBSERVED
        return id
    }
    return strings.TrimSpace(signalrec.AnnouncedTaskID())            // ANNOUNCED
}
```

- `signalrec.CurrentTaskID` gained the honest alias **`AnnouncedTaskID`** (same value,
  a name that cannot be read as "the row this process is running") — the old name
  invited exactly the misreading.
- The ledger line is rendered by **one** function (`stopVerdictLedgerLine`) from the
  same id the event is written with, so the two lines cannot disagree again.
- When there genuinely is no row, the line no longer prints an empty **value**: it
  states the finding and the reason (`there is no row … the ledger event cannot be
  written at all`), because `RecordSignalVerdict` is a silent no-op on an empty id
  (Rule 15).

Regression tests: `cmd/agent/signal_verdict_test.go` — the two lines must name the same
row; the announced id must still serve a process that claims no round; and the
no-row case must name the gap.

## F2 — an out-of-root registration was permanently un-reclaimable

The walk, printed by every pass (this host, 2026-09-27):

```
own registered worktree kept: <ambient>/gopi-ctl-01a0ddea — outside every declared worktree root — not swept
own registered worktree kept: <ambient>/gopi-prefix-fix — outside every declared worktree root — not swept
```

**The refusal was correct, and that was the defect.** `/tmp` shares the **boot
partition's device** (`scratch.SharesRootFS("/tmp")` = true), which Rule 22 forbids for
any gopi temp workspace — but the same declaration was the **only door**, so the
directories stayed for ever and their branches stayed withheld
(`scripts/safe-merge.sh` gate [c] refuses a branch whose worktree still stands). A
finished deliverable lost silently.

The disposal arm (`reclaimSelfOwnOutOfRootDisposal` in `internal/tools/task_reclaim_self.go`)
gives the residue a path **without weakening the ban**, and the order is what keeps it
honest:

1. **the `/` device is refused first**, with the reason named — *"it shares the BOOT
   PARTITION's filesystem (device measured) … this pass will not delete under it"* — and
   the operator's own next command in the same line (`git worktree remove <path>`, or
   delete + `git worktree prune`; the **branch is not to be deleted**);
2. the checkout leaves by the ordinary `git worktree remove` — **never `--force`** — and
   the **branch is untouched**, which is what makes discarding the directory
   non-destructive (every commit is in the main repo's object store);
3. the age floor is **24 h** (`reclaimSelfOwnQuarantineAge`) — longer than the ordinary
   release floor, because this arm *discards* rather than refuses, and a path a
   concurrent round is creating is minutes old by construction;
4. clean, unheld, and no live row owning it — the same rungs the ordinary ladder asks;
5. **the shared removal-ground checklist** (`reclaimGroundChecklistVerdict`: the branch's
   lineage, a guest's row, unmerged commits) — asked immediately before discarding, as
   the repo's removal-ground gate requires of every arm. It is a gate-found addition and
   the right one: an out-of-root directory is **not automatically this pass's own
   ground**, and the arm that discards is the last that should skip the question;
6. the removal is recorded in the removal audit **before** it happens, and the line says
   `own registered worktree quarantined` so an operator can tell this release from the
   ordinary one.

So the operator-visible policy is **unchanged** — the two measured registrations keep
their permanent refusal, and stay the operator's call, now with the command that
finishes them in the line.

Regression tests: `internal/tools/task_reclaim_self_outofroot_test.go` — the off-`/`
disposal (directory gone, branch still at its tip, git no longer lists it) and the
`/`-device refusal. The `/`-device test **creates nothing at the path**: the arm refuses
at rung ①, before it looks at anything else, and staging the ban by writing a worktree
onto `/` would commit the very act Rule 22 forbids.

The repo's own **removal-ground gate** (`TestEveryWorktreeRemovalArmQuestionsTheGround`)
caught the new arm on its first run — the arm discarded a directory without asking the
shared ground checklist — and the arm now asks it. The gate's counters moved from
**6 guarded / 10 total** to **7 guarded / 11 total** with the number of *flagged* sites
unchanged; an exact-site inventory (`armInventory`) was added to the gate so a removal
site entering or leaving the check is caught by a count rather than by luck.

## RED-then-green

Both new suites were shown RED against the tree **before** the change, on a base
worktree cut from the pre-change commit (a RED proof that only fails on the ancestor is
not a RED proof):

| proof | RED output on the pre-change tree | green now |
|---|---|---|
| worker loop does not merge | `RED: cli_tasks_worker.go still mentions "mergeTick"` · `RED: merge_tick.go still exists` · `RED: merge_tick_test.go still exists` · `RED: tmpdir_inject.go still calls config.PublishMergeTickMin` | `TestWorkerLoopDoesNotMerge`, `TestMergeTickPlumbingIsGone` PASS |
| F1 attribution | the pre-change line renders `for task %q` unconditionally, and the worker hands in `signalrec.CurrentTaskID()` — empty for a resident worker | `TestStopVerdict*` PASS |
| F2 disposal | `out-of-root registration kept for ever — "outside every declared worktree root — not swept"` (directory still on disk) | `TestOutOfRootDisposalFreesAnOffRootRegistration` PASS |

**Positive control** (the retirement must not have cost the worker its real job):
`TestRawPromptRunReleasesItsWorktree` — a real run creates a linked worktree and the
close-out releases it, keeping the branch — PASSES on this branch (it drives the built
binary, and the suite's own freshness gate forced a rebuild first).

## Retired, and still-nameable (Rule 14)

The spellings survive in `internal/config` for exactly one purpose — a refusal must be
able to name the dead knob in the operator's own words:

- `config.MergeTickRetiredEnvName` = `GOPI_MERGE_TICK_MIN` (env-reader-exempt: the
  reader *was* the deleted tick; a retirement must never consume it),
- `config.MergeTickRetiredSettingsKey` = `merge_tick`,
- `config.MergeTickRetiredReplacement` — the ONE statement of what to run instead, so
  the refusal and any help text cannot drift.

`CONFIG.md` carries the same facts (its §2c row count moved 61 → 60 in the same commit,
with the audit-trail note the flag gate requires).

## Not done / honest gaps

- **No live SIGINT walk for F1.** The agreement between the two lines and the no-row
  rendering are unit-tested; the end-to-end walk (drive a worker with a claimed round,
  SIGINT it, read the console) was **not** run — the window between "claimed a round" and
  "the signal arrives" needs a real worker mid-LLM call, and the pieces are covered
  separately. What is *not* covered is the live process shape.
- **F2's disposal was exercised on a fixture, not on the two real registrations.** They
  are on `/`-device temp paths, so the arm refuses them by design — the disposal branch
  was proven on an off-`/` fixture (`/dev/shm`) instead. Their actual disposal is now the
  operator's call, with the command in the line.
- The 24-hour quarantine floor is a judgement, not a measurement: it is one day because
  this arm discards and cannot be un-done, while (also measured) gopi **did** create
  scratch worktrees under the ambient temp root within the last day.
