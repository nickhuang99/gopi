<!-- doc-class: historical -->
# Task Report: merge-backlog duplicate dispatch — root cause + idempotent fix

Epic: e2e-1-merge-backlog-dedup (briefing `briefings/e2e-1-merge-backlog-dedup.md`),
from the 2026-09-10 legacy-t audit (GLM night review, 1281 rows → 751 deleted).

## Completed

### 1. Root cause (file:line, with hard evidence)

**The bug is a live-only coverage mask.**

`agentBranchScan` (cmd/agent/cli_tasks.go:2044 in master `aafa7ce` /
:2054 in the fixed tree; the same code shipped by `d853c1d` 2026-08-31
09:23 and unchanged until now — git blame on the whole block confirms one
commit) asked the ledger which branches were covered, via
`tools.TaskOpenMergeBacklogBranches`
(internal/tools/tasks.go:2591 in master, `aafa7ce`; the predicate line is
2591-2600 and the query sits at **2600**):

```sql
SELECT summary FROM tasks WHERE status IN ('pending','in_progress') AND tags LIKE '%merge-backlog%'
```

A `pending|claimed|in_progress` row is *queue-live*. Every other state
(completed / failed / cancelled / verified) was treated as "this branch is
UNCOVERED → submit again". The row's summary is a **pure function of the
branch name** — the payload carries no tip, ahead-count or any other input:

```go
summary := fmt.Sprintf("merge-backlog: %s unmerged — merge/archive decision needed", b)
```

So the scan was a memoryless generator: for every unmerged branch whose
decision row had reached a terminal state **without the branch being merged**,
each pass minted a *byte-identical* new row. The loop had no exit — the branch
was still unmerged, so the new row would be closed the same way and re-armed
again. The 10-minute throttle (`serveBranchScanThrottleMin`) did not help: it
paces the loop, it does not terminate it.

Why terminal-without-merge was the *normal* path, not an edge case:

- `YieldClaimedTask` (claim-broadcast M4.18, commit `0371c40`) cancels a row it
  loses the cross-node claim race for — **with no merge performed**.
- The db-sync delta union **replaces** a row's tags wholesale, so a row that
  travelled through that path keeps its `merge-backlog: …` summary but ends up
  tagged `sync-yielded` only (574 of 617 legacy scan rows lost their tag).
- A worker that ran the row but did not merge it also closes it

### Evidence (pre-purge snapshot `~/.gopi/backups/memory.db.bak-pre-legacy-purge-20260910-095008`)

| branch | rows | live | terminal | old predicate re-armed |
|---|---|---|---|---|
| hp/master | 75 | 0 | 75 | 75× |
| hp/fix/memsync-import-atomic | 74 | 0 | 74 | 74× |
| hp/feat/merge-backlog | 73 | 0 | 73 | 73× |
| hp/feat/claim-broadcast | 73 | 0 | 73 | 73× |
| fix/memsync-import-atomic | 73 | 0 | 73 | 73× |
| hp/feat/task-id-collision | 72 | 0 | 72 | 72× |
| hp/feat/serve-tick-git-sync | 72 | 0 | 72 | 72× |
| feat/merge-backlog | 73 | 0 | 73 | 73× |
| feat/pushback-chain-report | 3 | 0 | 3 | 3× |
| feat/pushback-e2e-t1788252501 | 3 | 0 | 3 | 3× |
| **total** | **591** | **0** | **591** | **591 re-arms** |

**591 duplicate rows, 100% terminal, 0 ever live.** Inter-arrival on
`hp/master` is 40–382 s — i.e. *every* serve pass, forever. That is the
"8 branches × 20 repeats / 13 branches × ~10 repeats" the audit reported
(the audit's counts are the post-purge survivors of the same loop).

The trigger for the burst: a peer-side branch set (the `hp/*` push-backs) that
could not be merged locally, so every decision row terminated without a merge
and re-armed 1:1. For the largest case (`hp/master`, 75 rows) that trigger is
independently documented: `devdoc/hp-master-merge-decision.md` (2026-09-01)
found the branch was a **phantom** fed by a stale `peerboxx/*` remote-tracking
ref — nothing to merge, so the decision row was closed every time and the
re-arm minted the next one. That is the cleanest statement of the bug's shape:
**one unchanging input (a branch that neither merges nor disappears) produced
N rows equal to the number of ticks, not one row per decision.**

### 2. Fix (idempotent)

`internal/tools/tasks.go` — `TaskOpenMergeBacklogBranches`:

- **No status filter.** A scan row masks its branch in *every* state — live or
  terminal. A terminal row with no merge is still a **recorded decision**;
  re-submitting it adds zero information.
- **No tags filter.** The tags column is not the fence: the sync union replaces
  tags wholesale (evidence above), so a tag-fenced mask would have re-minted
  all 574 stripped rows. The mask keys on the **summary prefix**, the one
  field a row keeps for life.
- `mergeBacklogScanSummaryVerb` (`"merge-backlog: %s unmerged"`) is now the
  single source (Rule 13/14): the scan *builds* with it
  (`tools.MergeBacklogScanSummaryVerb`), the predicate *parses* with it — no
  mirrored literal to drift.
- New single-branch face `TaskMergeBacklogBranchCovered` for callers probing
  one branch.

`cmd/agent/cli_tasks.go` — `agentBranchScan` now renders its summary through
the shared verb and documents the idempotence contract.

**Deliberate semantics change (and why it is safe):** the old code documented
"a row closed with a decision stops masking the branch, so a still-unmerged
branch re-surfaces on a later serve tick (re-arm semantics)". That intent is
*exactly* the bug: the re-surfaced row is identical to the closed one, and it
re-surfaces unconditionally on the next tick. The legitimate need behind it —
"tell me again when there is something NEW to decide" — is covered by the
audit path (`AuditMergeBacklog`), which dedupes on **branch + tip** and
therefore re-submits precisely when the branch gains commits. The scan has no
tip input, so it has nothing new to say about an unchanged branch.

**Cross-check — the two faces never collide on the key.** The scan (and now its
mask) keys on the summary `merge-backlog: <branch> unmerged …`; the audit keys
on a different verb, `merge <branch> into master`, and on branch+tip. Both use
the SAME tag, which is why the old tag-fenced mask was wrong: measured on the
snapshot, **17 branches carry both a scan row and an audit row** (e.g.
`feat/branch-scan-archive-filter` scan 1 / audit 2) while the old predicate
masked 0 of them, because the tag says nothing about WHICH face a row is. The
new mask parses `merge-backlog: ` + first field and cannot match
`merge <branch> into master`, so the two faces are separated by the summary
verb, not by the tag — and the audit's own idempotence is untouched (its
branch+tip dedup and tests are unchanged; see Verification).

## Residual analysis — can the loop re-fire today?

The purge deleted the historical duplicates, so the fix must be judged against
**live producers**, not only against the snapshot. Both lookups were re-run
against the purged live ledger (`~/.gopi/memory.db`, 872 rows, today 09-10)
and against the snapshot:

| ledger | rows matching the scan summary | OLD predicate masked | NEW predicate masks |
|---|---|---|---|
| live `~/.gopi/memory.db` (872 rows) | 22 | 0 (all 22 terminal: 16 cancelled, 3 completed, 3 verified — **0 live**) | 11 names |
| pre-purge snapshot | 617 | **0** | **36** |
| independent 2nd source: `tasks/legacy-t-rows-export.tsv` (the legacy-t audit's own export) | 618 | **0** (all 618 terminal: 605 cancelled, 10 completed, 3 verified — 0 live) | 36 |

The live table is the strongest single datapoint: **all 22 surviving scan rows
are terminal, and the old predicate masked exactly zero of them.** Eight of the
summaries still carry 2-3 identical rows apiece (the purge kept 2+ per branch).
Any re-enable of the scan against the OLD code would mint a 3rd, 4th, … row on
consecutive ticks, with no exit condition — the loop was still armed in the
shipped binary at `aafa7ce`.

Two honest caveats on reachability:

- **Live writer not pinned.** `sync-yielded` (574 of the 617 snapshot rows) is
  produced by **no code in the current tree** (`grep -rn yielded --include=*.go`
  finds only comments) — that burst 09-01 came from a retired/older sync path
  or the peer node. Terminal-without-merge is nevertheless reachable today via
  the sync blind ruling (`memory_sync_delta.go:501-523`: server writes
  `completed` + `sync-resolved`) and the yield-revert path
  (`memory_sync_claim.go:601+`). The bug therefore did not depend on the dead
  tag writer: **any** terminal close without a merge re-armed the branch.
- **Arm-under-a-different-repo (a residual, NOT this bug).** The mask queries
  the *ledger* by branch name only, while `agentBranchScan` probes the *cwd*
  repo. Two repos with a same-named unmerged branch mask each other. That is a
  distinct (multi-repo ledger) limitation — it suppresses, never amplifies — and
  it is not the duplicate-dispatch mechanism; recorded here rather than silently
  fixed. The `deliverable.repo` stamp already carries the repo for a
  repo-scoped mask if the pipeline is ever re-wired.
- **A branch NAME is masked for the ledger's lifetime.** Because the key is the
  summary (which must survive the tag-stripping sync union), deleting and later
  re-creating a branch with the same name is masked by its old decision row.
  Measured on the live ledger: 11 branch names are masked, and the intersection
  with the repo's current unmerged set is **empty** — every masked name is a
  branch that is already merged or gone, so nothing live is suppressed. Accepted
  as the price of a durable mask; the audit path (branch+tip) re-surfaces a
  re-created name that gains commits.

## Not Done

- **The retired pipeline was not re-wired into a serve tick.** `serveBranchScan`
  / `serveMergeBacklogAudit` / `serveMergeExecSweep` currently have **no live
  caller**: `d11af9a` (2026-09-04, "merger pass is config-driven per-repo
  sweep") retired the row pipeline from the pass and replaced it with
  `MergerRepoSweep`. The fix hardens the code that produced the incident so
  re-enabling it cannot resume the loop; re-wiring a producer is out of this
  epic's scope and would re-create the bureaucracy the audit deleted.
- No pruning of already-existing duplicates: the legacy-t purge (b684656)
  removed them; the fix is preventive.
- The audit's own `Discover` path was left untouched — it already dedupes on
  branch+tip and its tests are green.

## Verification

- **Build**: `go build ./...` → OK
- **Scoped tests**: `go test ./internal/tools/ -run 'TestTaskOpenMergeBacklogBranches|TestTaskMergeBacklogBranchCovered|TestMergeBacklogScanSummaryVerb'` → ok
  `go test ./cmd/agent/ -run 'TestAgentBranchScan|TestServeBranchScanThrottle'` → ok (10.6s)
- **Full suites** (final, post-merge tree, each run ALONE — see the contention
  note below):
  - `go test ./cmd/agent/ -count=1` on `fix/merge-backlog-dedup` → **ok
    198.9s (exit 0)**; the identical command on a detached worktree of
    `master` (`aafa7ce`) → **ok 191.9s (exit 0)**. Branch and baseline agree.
  - `go test ./internal/tools/ -count=1` → **10m test-timeout panic on BOTH
    branch and master, byte-identical signature** (`panic: test timed out
    after 10m0s` … 16 `database/sql.(*DB).connectionOpener` goroutines —
    the known pre-existing full-suite hang, memory #1172 / devdoc/
    bugfix-taskrun-briefing-summary-payload-2026-09-03.md). The named
    victim differs run to run (`TestTaskDeliverableWorktreeBashPlainSessionNoop`
    on the branch run, `TestMergeExecConflict` on the master run) — i.e. it
    is whichever test happens to be executing when the 10m wall is hit, not
    a failing test. **Each victim passes in isolation on BOTH trees**
    (`-run TestTaskDeliverableWorktreeBashPlainSessionNoop`: master 1.65s
    PASS, branch 1.21s PASS). A/B method: same command, two worktrees,
    scoped-then-full.
  - **Test-contention caveat (honesty)**: an earlier full `./cmd/agent/` run
    reported FAIL 356.8s with the exit 0 of the SAME suite at 198.9s minutes
    later. Cause: three worktrees on this box were running full suites
    concurrently (load average 5.8-7.1 on 4 cores), and the suites share
    `~/.gopi` + this git repo. Only the solo runs are evidence; the FAIL was
    contention, not code. Full-suite numbers here are therefore always
    solo-run on a quiet box.
- **Legacy-ledger replay** (real incident data, gated on `GOPI_LEGACY_LEDGER`):

```
GOPI_LEGACY_LEDGER=~/.gopi/backups/memory.db.bak-pre-legacy-purge-20260910-095008 \
  go test ./internal/tools/ -run TestMergeBacklogDedupe_LegacyLedgerReplay -v
--- PASS: TestMergeBacklogDedupe_LegacyLedgerReplay (0.08s)
    legacy ledger fingerprint: 10 branches repeated, 591 rows total, worst=75
    legacy replay: all 10 spammed branches masked by the new predicate (0 re-submits)
```

- **Escalated full-suite (`-timeout 25m`, verbose, solo)** — the run that
  closes the 10m-cap question honestly. Result: the suite reaches
  `TestManyFreshDBsNoHang` and that test **stalls** (it is the suite-order
  reproducer for the very issue it guards: 40 fresh temp DBs, each
  `openTasksDB`). Run **in isolation** it PASSES on both trees:
  branch `972a7ea` 60.67s exit 0, master baseline `aafa7ce` 52.97s exit 0
  (`go test ./internal/tools/ -run TestManyFreshDBsNoHang -count=1 -timeout 180s -v`).
  So the hang is (a) **not introduced by this branch** (master shows the same
  stall at the same test) and (b) **load-sensitive, not a hard hang** — under
  the full suite's accumulated load the 40 init loop exceeds the wall, while
  alone it takes ~1 minute. Conclusion recorded rather than papered over: the
  `internal/tools` package cannot be shown "green end-to-end" on this box in
  one process; the evidence standard used here is branch-vs-master A/B plus
  scoped runs, and no code path touched by this fix is involved in that stall.
- **Consecutive-tick idempotence** (`TestAgentBranchScan_ClosedRowStaysMasked`,
  both `cancelled` and `completed`): 1 row after the first scan, then **4
  consecutive scans → 0 new rows**, ledger row count pinned at 1.
- **Live-tick caveat**: the acceptance "consecutive real ticks no longer
  duplicate" is met at the strongest available level — `agentBranchScan()`
  itself driven 4 consecutive times (the real function, real ledger, real
  fixture repo), NOT a mock. A *live serve tick* cannot be observed because
  `serveBranchScan` has had no live caller since `d11af9a`; the ledger
  fingerprint table above (0 live rows, 0 masked by the old predicate)
  reproduces the real incident's input through the real predicate.
- **Regression matrix**: live rows (pending/claimed/in_progress) still mask;
  terminal rows (cancelled/completed/failed/verified) now mask; a tag-stripped
  scan row (sync-yield shape) still masks; audit-shape rows do not.

## Commit

`7162092 fix(branch-scan): merge-backlog decision rows stop re-arming every
tick — terminal rows mask` (fix + tests), `ba2f2c9` / `494e69a` / `746bc22`
(the devdoc report and its two evidence addenda) on branch
`fix/merge-backlog-dedup` in worktree `~/workspace/gopi-mbl-dedup` — the branch
lives in the main repo (`~/work/GoPi` worktree list), so the commits are the
deliverable; recovery: `git -C ~/work/GoPi show fix/merge-backlog-dedup:devdoc/merge-backlog-dedup-fix.md`.

## Recovery / hand-off

- Branch: `fix/merge-backlog-dedup` (tip `1c9c75c`; fix commit `7162092`, evidence
  addenda `ba2f2c9` / `494e69a` / `746bc22` / `7d30323`, master merge `c835947`)
- Report: `devdoc/merge-backlog-dedup-fix.md` on that branch
- Diff: `git -C ~/work/GoPi diff master...fix/merge-backlog-dedup` (6 files,
  ~690 insertions)
- Replay command (needs the snapshot, which is NOT in the repo):
  `GOPI_LEGACY_LEDGER=~/.gopi/backups/memory.db.bak-pre-legacy-purge-20260910-095008
  go test ./internal/tools/ -run TestMergeBacklogDedupe_LegacyLedgerReplay -v`
