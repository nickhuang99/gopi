<!-- doc-class: historical -->
# Worktree reclaim: the orphaned cwd-inheritor, and the anchor for a rowless run

**Date:** 2026-09-23 · **Branch:** `fix/worktree-holder-orphan-and-run-anchor`
**Brief:** `briefings/brief-worktree-reclaim-two-gaps.md`
**Siblings:** `01a0c664` (worker reclaims the child's ground), `01a0cdf5` (scratch
lifetime owned by writers), `01a0cd9c` (close-out checks the MAIN repo).

Two defects, one symptom (worktree-held branches with no live worker, count
growing). They share nothing else, so they are fixed separately.

---

## Gap A — a cwd-inheritor is not a holder

### What the guard does today

`finishCleanupWorktreeInUse` scans `/proc/<pid>/cwd` and refuses reclaim while
**any** live process has its cwd inside the worktree. That rule is correct and
load-bearing (it is what saved the ~1000 lines in the `01a0af50` incident), so
**the guard was not touched**: no threshold moved, no rubber stamp added.

### The signal chosen — and why

A **second pass**, `finishCleanupOrphanCwdInheritorReason`
(`internal/tools/task_finish_cleanup_holder_orphan.go`), runs **only after** the
primary guard said "in use", and asks of each cwd-holder whether *all* of these
hold:

1. **reparented to init** — `comm == "init"` (or systemd) **and** `ppid == 0`.
   Measured: `syscall.Kill(1, 0)` returns `EPERM` for a non-root user, so
   `pidAlive(1)` is **false** — init can *never* be identified by a signal
   probe. Identifying it by kernel `comm` + `ppid` is the only correct test.
2. **it carries a gopi exec-time launcher record** — either
   `GOPI_TASK_RUN_WRAPPER_PID` (`TaskRunWrapperPIDEnv`) or the agent-chain
   anchor `config.AgentSessionAncestorEnv`, read from `/proc/<pid>/environ`
   (readable same-uid). The value is the **pid that exported gopi's
   environment**, recorded at exec time — a fact, not an inference.
3. **that recorded launcher is dead**, and
4. **its session holds no live gopi** — via the new
   `procname.ProcSessionKey` (the session id of the process, read from
   `/proc/<pid>/stat`), compared against `procname.RunnerLive`.

Only when all four hold is the directory judged *not held*; **every other
answer returns "" and the veto stands** (unknown is never dead, memory #2633).

### Non-spoofability / why this is not a loophole

The record is `gopi`'s own exec-time export, inherited into children. A process
that has it **and** whose exporter is dead **and** whose session has no live
gopi is, by construction, work whose owner is gone. The unsafe direction
(destroying a live run's directory) requires a *live* gopi in the session, which
condition 4 refuses; the safe direction (keeping a leaked worktree) is the
failure we accept.

### Asymmetry — which way it errs

**It errs toward KEEPING.** A holder that cannot be measured at all (unreadable
`/proc`, unparseable stat, missing record) is **not** released; the veto stands
and the worktree leaks another day. Refusing costs a leaked registration that a
human or a later sweep collects; allowing wrongly costs a live run its working
directory. The measured case we were fixing had been vetoing for days.

### Reuse, not a second implementation (Rule 14)

No new liveness machinery: the primary guard's scan, `procname.RunnerLive`,
`procname.RunnerIdentity` and the pid + start-ticks + cmdline triple are reused.
The only addition to `procname` is `ProcSessionKey` (`EnvironName`'s sibling) —
one more reader of the same `/proc` facts.

### Wired at all four sites

- `task_finish_cleanup.go:1507` — the entry gate,
- `task_finish_cleanup.go:3573` — the removal arm,
- `task_reclaim.go:572` — guard ③,
- `task_merged_cleanup.go:519` — arm 5b, where the release is written into the
  removal's own audit evidence.

---

## Gap B — a run with no ledger row gets an anchor written by its producer

Every reclaim arm keyed on `claimed_by` / the ledger row, so a bare-prompt run
grounded in a worktree (`feat/endhook-release-defer`,
`fix/log-request-rawlog-resolver`) had **no anchor at all** and nothing would
ever collect it.

### Chosen: the producer writes the anchor at grounding time

`TaskGroundAnchorRecord` (`internal/tools/run_ground_anchor.go`) writes **one**
`removalaudit` record (`KindWorktreeGround` / `OpWorktreeGround` /
`ArmTaskGroundAnchor`) at the moment the worktree is grounded, from **both**
seams:

- `stampTaskGround` (the spawn path, `TaskRunGroundFor`) — covers the
  bare-prompt **and** the task-run face, since both go through it;
- the creation-time detector `TaskDeliverableWorktreeForRow`.

`Record` gained `holder_pid` / `holder_start` / `holder_sid` so the anchor
carries the same identity shape the rest of the reclaim family uses. One line
per **ground**, deduped by directory.

**Written by the producer, never inferred** — per
`devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md`: the owner of the
lifecycle writes the state down; no external observer guesses it from `/proc`.
Cost: one ledger line per ground (negligible; the ledger is already the audit
layer).

### Read by a sweeper

- `RunGroundAnchors` — the anchors on disk;
- `RunGroundAnchorLiveness` — the ladder, errs toward LIVE:
  incarnation alive → **LIVE**; session alive and contains a live gopi →
  **LIVE**; session alive but empty of gopi → **COLLECTABLE**; session gone and
  incarnation gone → **COLLECTABLE**; **nothing measurable → LIVE**;
- `RunGroundCollectableGrounds` — applies the sweep's own age floor
  (`MinAge`), and refuses any ground that is *occupied* (the shared cwd guard);
- `RunGroundCollect`.

#### The second defect the Rule 31 run MEASURED: the asker answered itself

The first end-to-end run of `gopi task sweep-worktrees --apply` over a staged
pair — one finished rowless ground, one live ground — produced **`removed=0`**
and kept the finished one with:

```
keep  [run-ground-anchor] …/gopi-seed-finished-rowless  kept: the launch session 3560524 still holds a live gopi process
```

The session named there was the **sweeper's own launch session**: the sweep is
`gopi`, so `finishCleanupSessionHasLiveGopi(sid)` walked the census, found
*itself*, and answered "a live gopi is in that session" about a session whose
only gopi was the process asking the question. Every ground taken from any
gopi-launched shell was therefore permanently uncollectable — the same symptom
(nothing is ever collected) the sweeper exists to remove, reproduced by the
sweeper's own instrument. **A green unit suite had not caught it**: the test
staged a dead taker with a session that had no live gopi in it, so the identity
predicate and the census never disagreed.

**The fix is on the input, not the threshold.** No rung was relaxed and no age
was shortened: `finishCleanupSessionHasLiveGopiExcept(sid, except)` gains ONE
exclusion — the pid of the process **asking** — and `RunGroundAnchorLiveness`
passes its own `os.Getpid()`. The exclusion is deliberately unavailable to the
other caller: the orphan-holder path keeps calling the plain
`finishCleanupSessionHasLiveGopi`, because there a live gopi in the launcher's
session may genuinely be the run still working. A questioner that answers itself
is not evidence; a *different* live gopi in the same session still vetoes.

The false-LIVE could not, in any case, have been the taker's: rung ① already
answered "LIVE" for a live taker incarnation, so rung ② is only ever reached
when the taker is provably gone. The exclusion therefore removes exactly one
candidate — the asker — and keeps every other.

**Red proof** (`TestGroundLivenessDoesNotCountTheAskerAsAHolder`, with its
re-exec'd `TestGroundLivenessAskerProbeChild`): the test re-execs itself through
a symlink named `gopi` (so the asker's kernel `comm` really is
`procname.NameGopi`) in its own `setsid` session (so the only gopi in that
session is the probe), and asks the production ladder about a dead taker. With
the pre-fix call form it **FAILs** with
`live=true … "the launch session … still holds a live gopi process"`; with the
exclusion it passes with
`live=false … "its session … holds no live gopi process"`. One call form
toggled, red → green.

`RunGroundCollect` emits the audit record **before** the `git worktree remove`
(the standards gate requires the record to precede the effect), and removes the
**registration only — never the branch**: the work stays reachable, which is
exactly what the brief demanded ("the anchor's job is to let a *finished* run be
collected, not to guess from absence").

Surfaced as the **run-ground axis** of `gopi task sweep-worktrees` (dry-run by
default, `--apply` to act) — no new script, no cron entry, no daemon, and no
periodic sweeper as the primary mechanism.

### How it composes with the siblings

- `01a0c664` (worker reclaims the child's ground): that path runs a
  deterministic checklist **with a worker and a row present**. Gap B is
  precisely the case it cannot cover — no worker, no row. The two are disjoint:
  row-anchored reclaim keeps doing what it does; the run-ground anchor is the
  second key it was missing.
- `01a0cdf5` (scratch lifetime owned by writers): same principle — the producer
  writes the lifecycle state — applied to a different tree. No re-implementation.
- `01a0cd9c`: that is the MAIN-repo check; this is the worktree side.
