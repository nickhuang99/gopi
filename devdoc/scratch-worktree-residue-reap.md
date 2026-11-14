<!-- doc-class: historical -->
# Scratch Worktree & Test-Branch Residue — root cause, fixes, answer (2026-09-14)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns `gopi worker`; an agent must never run it (submit the work instead: `gopi task submit`).

**Branch:** `fix/scratch-worktree-residue-reap` · **Worktree:** `~/workspace/gopi-residue-reap`
(off master `48e623a2`; master has since advanced, the merge is conflict-free).
The worktree was **released at task end** (Rule 6c — the branch is the
deliverable) and re-created only to land this header note; `bash
scripts/safe-merge.sh fix/scratch-worktree-residue-reap` passes all four checks
(`exit 0`) once it is released again, and the devdoc is always readable without
it: `git -C /home/user/work/GoPi show
fix/scratch-worktree-residue-reap:devdoc/scratch-worktree-residue-reap.md`.
**Follows:** task `01a09b33-394c-77e7-98c0-c8451f299b99`
("`gopi worker --status` over-reports workers") — that task's report ends by asking
three questions this one answers: *fix the code-related residue*, *how do we make
gopi remove a test's branch together with its worktree*, and *why are these
scratch gates using `/dev/shm` for worktrees?*

---

## 0. The three questions, answered up front

| Question | Answer |
|---|---|
| Why do scratch gates put worktrees in `/dev/shm`? | Because the **test scratch home is a symlink to it**: `~/workspace/gopi-test-scratch → /dev/shm/gopi-test-scratch`, installed by `scripts/setup-scratch-home.sh` (`MEMORY_BASE="${GOPI_SCRATCH_MEMORY_BASE:-/dev/shm}"`), chosen years ago for suite speed. `internal/scratch.CanonicalPath` resolves the symlink, so every scratch path — and therefore every worktree a fixture/gate creates under it — lands on tmpfs. It is **not a stray literal and not a rule-22 violation**: measured device ids are `/`=2051, `/home`=2052, `/dev/shm`=28, scratch=2052→resolved to tmpfs, all *off* `/`. It is **RAM-only**, and that is the real cost — see §1. |
| How does gopi remove a test branch together with its worktree once testing is done? | **Measured first (git 2.34.1, probes P1–P5): `git worktree remove` does NOT delete the branch.** It drops the *registration* and nothing else — so "remove the branch together with its worktree" is TWO git verbs, and every removal in the system (the merge executor's post-merge cleanup, the adoption chain, the merge-build gate's defer, this sweep's pair lane) leaves the ref behind with no registration. Three mechanisms, all now landed: (a) **the reap goes with the checkout** — when a registration is dead (its directory gone), the sweeper removes the branch *with* the registration, but only through `git branch -d`, so an unmerged branch is refused and *reported*; (b) **the repair happens where the residue is created** — the merge BUILD gate (`internal/tools/merge_build_gate.go`) now clears the stale lock it made instead of `os.RemoveAll`-ing behind git's back, and the *resident* serve pass runs the reap every tick (§4); (c) **`merged-cleanup` collects ORPHAN REFS** — a merged branch with no worktree at all is deleted ref-only (`branch -d`, never `-D`, never `--force`) under the same live-row and merged-recheck guards, so the leftovers every other component makes are finally reachable (§4, E). |
| And the code bugs behind the repeated issue? | Four, all fixed with RED-before/GREEN-after tests: **A** the worktree sweeper could not see a dead registration (its two "keep" arms covered exactly the leaking shape) and could never remove its branch; **B** the merge BUILD gate left a `locked: initializing` registration behind and deleted the directory behind git's back; **C** the scratch GC could not reach `<home>/queue` (5,887 entries, 97 MB) or the pooled batch dirs — and, once it could, the queue was judged all-or-nothing, so one fresh child shielded 5,583 stale siblings (fixed to per-child granularity, live evidence 8 → 5,591 reclaimable); **D** nothing *resident* ever ran the reap (the fix existed but was operator-only). |

---

## 1. Root cause RC1 — the leak's mechanism (probed, not reasoned)

Every step below was measured on this host (git 2.34.1) with probes under
`/dev/shm/gopi-test-scratch/residue-probe/`:

1. `git worktree add` writes `<repo>/.git/worktrees/<id>/locked` containing
   **`initializing`** *while it runs*. On success it deletes the lock (probe A:
   no `locked` file after a completed add).
2. A **SIGKILL** between the registration and the completion leaves that lock
   behind. `merge_build_gate` then removes the directory with
   `os.RemoveAll(workRoot)` in its `defer` — so the registration is left
   **locked + dir-less, forever**.
3. `git worktree remove --force` **REFUSES** a locked tree:
   *"cannot remove a locked working tree, lock reason: initializing — use
   'remove -f -f' to override or unlock first"*.
4. `git worktree unlock <path>` **FAILS** once the directory is gone:
   *"'…' is not a working tree"*.
5. `git worktree prune` **SKIPS** locked entries (probe S5: a locked dead
   registration survived `prune -v` untouched — twice, on this host).
6. The sweeper's own judge classified it `keep` **twice over** — once for
   `Locked` ("an explicit do-not-touch") and once for `Prunable` ("the repo prune
   collects it", which step 5 proves false). So the one leaking shape was the one
   shape nothing could collect.

Live evidence (before the fix): `.git/worktrees/wt-master2` and `wt-master3`,
both `locked: initializing`, both with `gitdir` pointing at
`/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-build-<pid>/…/wt-master` —
directories that no longer existed, whose merger pids were long dead.

**Repair chosen** (and the only one that touches neither a directory nor a ref):
`unlock <path>` first (git's own named repair; succeeds when the directory still
exists), then clear the lock file in the registration's admin dir, then let the
repo's `prune` collect it. `remove -f -f` also works in both states but
**destroys the checkout** — the sweeper deliberately does not use it.

## 2. Root cause RC2 — the scratch GC could not see the queue (new, unbounded)

`SweepHome` walks only **direct children** of the scratch home, and `IsScratchEntry`
matches **markers inside a name** (`suite-`, `gopi-suite-tmp-`, `queue-`). The
queue directory is named by the bare constant `queue` — it matched nothing.
Measured: **5,887 entries, 97 MB of the home's 2.2 GB**, including orphaned
`queue-runtime-merge-build-<deadpid>/` run directories whose `gocache` /
`gomodcache` are tens of MB each. The pooled rsync batch dirs
(`gopi-merge-batch-*`) were the same story from the other side: prefix, parser
and age helper all existed with **tests as their only readers** — a hook with no
consumer (the Rule 21 defect), so a batch directory whose rsync died was
collectable by *nothing*.

### 2a. Reaching it was not enough — the GRANULARITY was the real fix (measured)

The first arm judged the queue **all-or-nothing** (collect the subtree only when
*every* in-namespace child is stale and unheld). That is correct per entry and
useless in practice: on this host **6,064 children, 5,583 stale**, and a single
fresh or held child shielded all of them — `gopi scratch-gc --dry-run` reported
**8** reclaimable entries while 5,583 sat there. A container is not a unit of
ownership: each run is its own, so the sweep now judges each child on its own
age and holder (`queueSweepChildren`), and the queue *directory* survives by
design (it is structure; the next writer recreates it). Both faces — the sweep
and the dry-run plan — run the **same** predicate, so the plan can never promise
a set the sweep refuses.

The fix brought a second measured defect with it: the per-entry liveness check
costs one full `/proc` walk per candidate, and **6,064 walks did not finish in
120s** — the arm meant to bound the tree *hung* the sweep. The sweep now
collects `LiveHolders()` **once per pass** (lazily, only when a queue is
present) and answers every child from that set (`unheldBatch`), keeping the
per-entry form for single-directory callers.

**Live evidence:** reclaimable queue children went **8 → 5,591**. Unit tests
passed both before and after — the live dry-run is what exposed both defects
(which is why the verification below has a live column).

## 3. Root cause RC3 — nothing resident ran the reap

`gopi task sweep-worktrees` (the operator verb) and its machinery existed, but
the verb is operator-only and **dry-run by default**, and the serve pass that
runs every tick (`serveMergedCleanupSweep`) only removes a worktree whose
**directory exists** (its discovery is `git worktree list` × `git branch
--merged`, then `git status` inside it). A registration whose directory is gone
is therefore invisible to every resident component — which is why the same
residue kept reappearing after every repair.

## 4. The fixes

| # | What | Where |
|---|---|---|
| **A** | The sweeper reaps dead registrations. A new arm runs **before** the locked/prunable keeps: `prunable OR dir-gone` → reap (unlock → clear lock file → guaranteed prune). New dispositions `reap` / `reap-branch`, counters `reaped` / `would-reap` / `branches-reaped`. | `internal/tools/worktree_sweep.go` |
| **A2** | **In-flight floor**: a lock younger than `worktreeSweepInitLockFloor` (1 h) is a creator that is *still running* (`worktree add` completes in seconds) — the reap refuses it and says so. A lock we cannot date keeps the registration. | same |
| **A3** | **Branch with worktree**: a dead registration's own branch is removed *after* the repo's prune, always via `git branch -d` — git itself decides whether the branch carries unmerged work. A refusal is a reported keep, never a force. | same |
| **A4** | The prune is run with `-v` and its lines are relayed into the report. Measured why it must be repo-global: `git worktree prune` **accepts no path argument** (usage, exit 129), so scoping is impossible — but it never touches a dir-present registration (it re-resolves the `gitdir` backlink first), which is what makes it safe. | same |
| **B** | The merge BUILD gate repairs the registration it created: on `remove --force` refusal → `worktreeDropStaleLock` → retry → `prune`, each step reported. | `internal/tools/merge_build_gate.go` |
| **C** | The scratch GC reaches `<home>/queue` (swept as a subtree, same namespace+age+holder predicate, one level deep so a run's caches go too) and the pooled `gopi-merge-batch-*` dirs (bounded by their own wider `BatchPoolStaleAge`). An **empty** queue is structure, not residue, and is kept. | `internal/scratch/gc.go` |
| **D** | The reap is **resident**: the serve tick's merged-cleanup pass now also runs `WorktreeSweepRun` in apply mode and logs what it reaped/pruned/errored — the same tick, no new daemon. | `cmd/agent/cli_tasks.go` |
| **E** | **Orphan refs are collected** (`mergedCleanupOrphanBranch`): the branch loop's `!hasWT → continue` — which skipped *exactly* the shape a prior worktree removal leaves — now calls a REF-ONLY lane (no worktree verb, no directory, no force). It keeps the two guards that carry the safety burden (live ledger row; the pair lane's authorisation test "the reflog moved OR a row delivered it") plus the race-free merged recheck, and deletes with `branch -d` only. A refusal is a reported keep. Reported as `worktree=(no worktree — orphan ref)`. | `internal/tools/task_merged_cleanup.go` |

## 5. Verification

- **RED-before / GREEN-after**, on the real mechanism rather than a mock:
  - `TestSweepReapsLockedDeadRegistration` — the fixture is the leak itself
    (`worktree add` → aged `initializing` lock → `os.RemoveAll`). Pre-fix probe
    (temporary `zz_red_probe_test.go` against stock master, logged and then
    deleted): `judge verdict: keep (locked by git)`, `reclaimed=0`,
    `registration wt-dead SURVIVED`, `prune skipped the locked entry`.
  - `TestSweepReapsDeadRegistrationBranchWithTheWorktree` — 1/1 for a
    tip-at-master branch, registration-gone/branch-kept for an unmerged one,
    with the refusal named in the finding.
  - `TestSweepKeepsALockYoungerThanTheInFlightFloor` — the other half: a fresh
    lock is kept ("a `worktree add` may still be running").
  - `TestSweepHomeReachesTheQueueSubtree` — pre-fix: `swept 0 entries, want 1`.
  - `TestSweepHomeKeepsAnEmptyQueueDirectory` — the minimal-sample check that
    caught the first version of the arm (internal/testutil's GC fixtures create
    an empty queue; `removed 2, want exactly 1`).
  - `TestServeMergedCleanupSweepAlsoReapsDeadRegistrations` — the resident wiring,
    end to end on a sandbox repo + sandbox settings.
  - **The orphan-ref lane** (`task_merged_cleanup_orphan_test.go`), on the shape
    every worktree removal leaves (`worktree remove --force`, ref left behind):
    `TestMergedCleanupReapsOrphanMergedBranchWithNoWorktree` (ref collected,
    labelled as the ref-only shape, no directory created),
    `TestMergedCleanupOrphanLaneIsRefOnly` (the worktree list is byte-identical
    before/after — the lane touches the ref and nothing else),
    `TestMergedCleanupOrphanBranchLiveRowKept` (a live row still owing the branch
    protects it), `TestMergedCleanupOrphanLaneRefusesAnUnmergedRef` (both sides
    of the race: after the ref moves the coarse discovery drops it → empty pass;
    inside the race window the lane is handed the branch directly and refuses it
    on the recheck).
  - **RED-before/GREEN-after on the real binary** (`/dev/shm/gopi-test-scratch/orphan-red/repro.sh`,
    built against master `48e623a2` and against this branch):
    pre-fix → `removed=0 skipped=0 failed=0` + `ORPHAN REF SURVIVED (defect
    present)`; post-fix → `removed=1` + `removed branch=feat/merged-leftover
    worktree=(no worktree — orphan ref)`.
- **Live host, real repo (`/home/user/work/GoPi`)**: the pass collected the TWO
  orphan refs measured in subtask 12 and nothing else —
  `removed=2 skipped=3 failed=0`, the two `removed` lines being
  `fix/daemon-context-isolation` and `fix/voice-mic-probe-privileged` (both
  named `worktree=(no worktree — orphan ref)`), while the three skips each carry
  their reason (two live-CWD holders, one dirty live-bond worktree).
  `git for-each-ref refs/heads` before/after differed by **exactly those two
  refs** (diff = 2 deletion lines, nothing else) — no force, no third-party ref
  touched. A SECOND pass over the same repo changed nothing
  (`git for-each-ref` byte-identical) — the lane is idempotent.
- **Live host, real binary**: `task sweep-worktrees` reaped `wt-master2` and
  `wt-master3`; `ls .git/worktrees` went 10 → 8 entries and `git for-each-ref`
  was **byte-identical before/after** (98 refs, `refs-now.txt == refs-before.txt`
  except the unrelated master advance a sibling merger landed concurrently).
- `go build ./...`: OK. `go test ./internal/tools/ ./internal/scratch/ ./cmd/agent/
  …`: see the report for the run.

## 6. Not done (honest)

- **The queue subtree's own protection depends on the scratch home resolving.**
  The scratch home is `~/workspace/gopi-test-scratch → /dev/shm/gopi-test-scratch`,
  so `worktreeSweepRoots` declares the *resolved* scratch home and every queue
  path is in policy — verified: the sweep's two live reaps were both
  `queue-runtime-merge-build-*/merge-build-gate-*/wt-master` registrations, and
  the queue path is reported with a real disposition, never as out-of-policy. On
  a host where the scratch home is *not* symlinked into `TMPDIR`, a gate that
  builds under a raw `os.TempDir()` subtree would still be covered by the
  fixture root; a gate that built somewhere else entirely would not, and would
  be reported (not silently missed) as outside every declared root.
- The **stale-binary** observation from task `01a09b33` (live workers running
  `bin/gopi (deleted)`) remains unaddressed — separate concern.
- `pidfd`/start-time stamping of worker state files — unchanged from that task.
- `internal/stylegate` is **red on master itself** (187 ignored-error hits vs a
  ceiling of 182, plus 1 test-sleep in `cmd/agent/cli_worker_status_test.go`) —
  pre-existing, measured in a clean worktree off master, not introduced here.
