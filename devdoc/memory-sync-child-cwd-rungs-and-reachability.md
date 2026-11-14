<!-- doc-class: normative -->
# The memory-sync child cwd: which rung failed, and why the escape was unreachable

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

Task `01a17889-6356-7f02-8169-82f0bc74afc0`, superseding the two-rung analysis in
`memory-sync-child-cwd-two-seams.md` (that document's rungs are still correct —
they were just never reached, see §2).

## 1. The measurement that reopened the defect

`ca2a14b` (three rungs: ① `TaskGroundInUse`, ② `GOPI_RUN_GROUND`, ③ linked
worktree) was already an ancestor of the running binary on this host. Measured on
2026-10-26 at 22:11:04, `readlink /proc/2204429/cwd` for a child whose argv carried
`--db` (the store seam's fingerprint) still answered a **task ground**:

```
pid=2204429 ppid=2038486 cwd=/home/user/workspace/01a17887-34f1-74aa-abf1-006952daec85
   argv=/home/user/work/gopi/bin/gopi memory sync --peer peer1 --push --db /home/user/.gopi/memory.db
   started 2026-10-26 22:11:04
```

and its launcher's environment:

```
pid 2038486  cwd=/home/user/workspace/01a17887-34f1-74aa-abf1-006952daec85
  GOPI_RUN_GROUND=/home/user/workspace/01a17850-bb6a-7438-a07e-4960f6a34ca7
```

## 2. Which rung failed: all three, at once

| rung | what it reads | measured input | answer |
|---|---|---|---|
| ① `TaskGroundInUse` | a live process **outside the caller's own chain** | the ground's only occupant is the launcher itself (`finishCleanupSelfChain` excludes it by design) | `""` (no holder) |
| ② `GOPI_RUN_GROUND` | the launcher's **declared** ground | declared `…/01a17850-…`, inherited `…/01a17887-…` → `within()` false | no ground |
| ③ `linkedWorktreeTop` | `.git` is a **FILE** | `ls -ld` → `.git` is a **DIRECTORY** (clone ground) | `""` |
| (resolver) | a provably off-`/` escape root | see §3 | `WhyNoRoot` |

Rung ② failing is not a coincidence: today's worker ground is a **clone**, so a
run's `GOPI_RUN_GROUND` can name a different row's ground while the run itself
stands in its own. Rung ① failing is by design. Rung ③ only ever covered the
*linking-worktree* model, and the worker no longer uses it.

## 3. The second, decisive defect: the escape root was unreachable

`memorySyncChildProcessRoot` refuses a candidate that lies inside a **declared
worktree root**, unconditionally. Measured on this host:

```
worktree_root = /home/user/workspace           (config show; source: default <home>/workspace)
scratch home  = /home/user/workspace/gopi-test-scratch
$ worktreeroots.PermitsAny([worktree_root], scratch_home) → true   (matched /home/user/workspace)
```

There is exactly ONE candidate root, and it is always inside the declared root, so
the decision could **only ever** answer `MemorySyncChildCwdWhyNoRoot` = "keep the
inherited directory". **Adding rungs ①②③ could not have changed any behaviour on
this host** — which is exactly what the 22:11 measurement showed.

### A repair that was tried and rejected (recorded so it is not retried)

The first repair permitted the candidate when it sat on **another filesystem**
("a path-walk cannot leave its filesystem"). Measured:

```
stat -c %d /home/user/workspace  /home/user/workspace/gopi-test-scratch
66305 /home/user/workspace
66305 /home/user/workspace/gopi-test-scratch      ← SAME device
```

The relaxation permits nothing in production, while the suite (whose pinned
worktree root lives on `/dev/shm`) reads green. **Green under the pin and dark in
production is the failure mode this whole task is about**, so the device test was
discarded rather than shipped.

## 4. The repair

**Criterion, stated as a mechanism instead of a path list** (the sentence
`MemorySyncChildCwdWhyRepoCrossed` has carried since the file was written and
which had no reader until now): a sync child must not start somewhere that makes a
`git` leg answer about **another repository**. Measured by looking for a `.git`
entry at or above the candidate (git discovers upward). Measured on this host:

```
$ git -C /home/user/workspace/gopi-test-scratch rev-parse --show-toplevel
fatal: not a git repository (or any parent up to mount point /home/user)
```

— so the scratch home belongs to no repository and is a legitimate home for the
child, while a candidate inside a checkout is refused by name.

**Rung ④ — the LEDGER**: the one source that knows a directory is a ground without
the launcher's cooperation, and the same record guard ③ is asked about
(`deliverable.worktree`). It reuses `taskGroundClaimRows` (the existing
deliverable scan: LIKE pre-filter + JSON decode + holder verdict) rather than
querying the table again. A directory **no row names** is not a ground and stays
neutral — the main checkout, `~`, a session directory keep their inherited cwd
(the theatre guard is intact).

**The guard is untouched.** `finishCleanupWorktreeInUse` / `TaskGroundInUse` /
`reclaimSkipDirtyReason`: no character changed, no process-name exemption added.

## 5. Verification (this run)

Measured with the REAL spawn seam (`memoryStoreSyncRunChild`), the REAL ground this
run stands in, and `/proc` as the reading:

```
LAUNCHER cwd = /home/user/workspace/01a17889-…/internal/tools
LAUNCHER GOPI_RUN_GROUND = /home/user/workspace/01a17889-…
MEASURED child cwd = /home/user/workspace/gopi-test-scratch
DECISION LINE = [memory-sync-on-store] [store-triggered round] child cwd
  /home/user/workspace/gopi-test-scratch (left the inherited directory … behind …)
```

Guard ③ read **while that child was alive**:

```
CHILD pid=2507727 cwd="/home/user/workspace/gopi-test-scratch"  (ground=…/01a17889-…)
CHILD cwd inside the ground? false
GUARD3 holder=""   GUARD3 reason=""
GUARD3 names the sync child (pid 2507727)? false
```

Red proofs (each: disable → FAIL → restore → `sha256sum` identical):
1. rung ④'s application removed → `TestDecidedChildCwdEscapesALedgerNamedCloneGround` FAILS;
2. the pre-fix unconditional root refusal → both the ledger test and
   `TestGitLandscapeRefusalIsDecidedByTheRepository` FAIL
   (`the scratch home … was refused as an escape root`).

## 6. Parallel `--push`: the status and what it costs (behaviour NOT changed)

Measured 2026-10-26, three simultaneous pushes, each a separate
`gopi memory sync --peer peer1 --push --db /home/user/.gopi/memory.db` with its own
`ssh … --serve-assets` leg:

| pid | launcher's ground | started | cwd |
|---|---|---|---|
| 2033047 | `01a17873-…` | 22:04:31 | that ground |
| 2037230 | `01a17850-…` | 22:04:44 | that ground |
| 2204429 | `01a17887-…` | 22:11:04 | that ground |

**Cost, measured:** the corpus is `539,975,680` bytes (≈540 MB) and each leg opens
an **independent** SQLite read on the same file; three legs multiply peer write-lock
contention and egress by ~3 while each one carries the WHOLE corpus (the push is a
superset, so the two later legs re-send what the first already delivered). Duration
per push measured earlier in this epic: **315–534 s**; the outer bound is
`memoryStoreSyncChildWait = 30 min`, i.e. a stuck leg pins its ground for up to half
an hour.

**Why it is allowed to happen** (not a bug to lock away): the throttle/in-flight
guards are **per process** (`memoryStoreSyncInFlight`, `memoryStoreSyncLastRun`),
and each task runs in its OWN `gopi`/worker process — so three runs store a memory
within the same minute and three legs start. Nothing coordinates them, by design:
`memoryStoreSyncMaxRoundsPerProcess = 8`, `memoryStoreSyncDueDefault = 60 s`.

**Not changed here** (the task forbids it): no lock, no queue, no cross-process
coordination was added. Recording the shape is the deliverable.

## 7. What is still unfixed (named, not hidden)

`daemoncwd.WorktreeRoots()` builds its list from `worktreeroots.DefaultRoot()`, so
it **ignores** the declared `worktree_root` / `GOPI_WORKTREE_ROOT` — measured in the
same run: `daemoncwd` answers `/dev/shm/…/gopi-worktree-root` (source `default`)
while `worktreeroots.Resolve("")` answers `/home/user/workspace` (source
`settings worktree_root / GOPI_WORKTREE_ROOT`). The decision no longer depends on
that list (the git-landscape criterion replaced it), so it does not affect this fix;
it remains a real resolver defect and is filed as its own row.
