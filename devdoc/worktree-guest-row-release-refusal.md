<!-- doc-class: historical -->
# A guest process may not release a task row's worktree (path B of the worktree-eviction defect)

Epic `01a0c2b5-cf3d-7e27-90d0-fbe179f9eec5` (tag `bug`), 2026-09-21. Branch
`fix/guest-row-reconcile-refusal`. Complement of the SWEEP half
(`fix/own-release-ledger-ownership`, `832794fe`, arm `worktreeRuleSelfCheckout`,
devdoc `worktree-guest-eviction-refusal.md`).

## Root cause

Two passes free worktrees, and the first fix closed only one of them.

| pass | decider | what it frees | guarded by |
|---|---|---|---|
| SWEEP (path A) | `worktreeSweepJudge` | registrations of the repos it enumerates | `worktreeRuleSelfCheckout` (fixed) |
| ROW RELEASE (path B) | `reclaimDisposition` (called by `ReclaimDeadHolderWorktrees`, `ReclaimOwnFinishedWorktrees`, `prunes.repair`) | the worktree a **row's deliverable** names | **nothing about the actor — this fix** |

Every rung on path B asked about the REGISTRATION (dirty? held by a live
process? owned by a live row? too young?) or about the ACTOR being the row's
**claimant** — never about **where the actor stands**. A process standing in a
LINKED worktree of repo R (a *guest*) therefore reconciled R's rows with the
authority of R's owner, and a per-task ledger (`GOPI_MEMORY_DB`, the normal
shape of every `go test` run and of every worker round) cannot observe another
task's claims — so a peer's row read as unowned/abandoned and the guest freed a
checkout that was not its to free.

Measured in the wild (2026-09-21) on the sweep fix's own closing gate run: the
worktree it ran from — `/home/user/workspace/gopi-ownrel` — was gone afterwards
(`getwd: no such file or directory`) while a planted decoy survived. Exactly the
shape this doc's own reproduction shows: the OWN row's worktree went, the
unowned decoy stayed.

## The fix

One criterion, in one file (`internal/tools/guest_row_guard.go`), asked where
the repo is in hand — inside `reclaimDisposition` (per row, so a pass spanning
several repos stays honest) and inside `reclaimSelfRelease` /
`reclaimSelfOwnRepos` (the producer arm, whose candidate set is "rows THIS
process claims").

A process is a **GUEST of repo R** iff (both halves required):

1. **identity** — R's common git dir (`git rev-parse --git-common-dir`)
   equals the common git dir of the repository the process stands in. The common
   dir is the identity because a linked worktree and its main checkout SHARE it;
   comparing top levels would answer "not my repository" exactly when the
   process stands in a linked worktree of it.
2. **residency** — the process's own top level is NOT the directory holding that
   common git dir (i.e. not R's main checkout).

Half 1 alone would refuse every row of every repo; half 2 alone would strip the
owner of its own repo. What each direction keeps:

| actor | its own repo's rows | a foreign repo's rows |
|---|---|---|
| guest in a linked worktree | **refused** (arm named) | reconciled as before |
| resident in the main checkout | reconciled (its own row included) | reconciled as before |
| no repository at all (cwd handed back to `/`) | reconciled | reconciled |

Two writers keep the `ReclaimOwnFinishedWorktrees` producer leg alive rather
than retired by this fix: the worker round's own-release pass
(`workerRoundOwnWorktrees`) — its cwd has been handed back to `/`, so it is a
guest of nothing — and a run standing in the repo's main checkout.

### Refusal arm names (Rule 15 — no silent refusal)

    guestRowReleaseRefusal      — a PEER row's worktree, refused to a guest
    guestRowOwnReleaseRefusal   — its OWN row's worktree, refused to a guest
    guestRowUnresolvedGuestRefusal — a guest whose repository identity is unreadable

Each sentence quotes both halves of the evidence (the actor's checkout and the
repository's main checkout), so a withheld directory is never a silent one:

    [task-notify] dead-holder reclaim feat/x (t-...) skipped: guest-row-reconcile
    refusal (arm guestRowReleaseRefusal): this process stands in the linked
    worktree <actor>, not in the main checkout <main> of that repository — a guest
    does not release or reconcile another row's worktree of <repo> (path B of the
    worktree-eviction defect)

### Why NOT at the call sites (the reverted attempt)

The previous attempt guarded `runTaskLifecycleTick` / `cli_tasks_worker.go` and
took `cmd/agent` from green to 8 failures, because at a call site the
repo-under-judgement is not in hand — the helpers hold it, their callers do not.
Every face (the resident tick, the worker round, the wrapper's close-out, `gopi
task finish-cleanup`) now reaches ONE implementation by calling the same
helpers.

### Narrowness: what is deliberately NOT refused

* **An actor outside any repository** (a worker that has handed its cwd back to
  `/`, a one-shot run from a temp dir) keeps the full mandate — it is a guest of
  nothing. Refusing there would have retired the producer pass in production
  while leaving every unit test (cwd = a checkout) green.
* **A repository whose identity cannot be read** is NOT a refusal on path A:
  the sweep's `worktreeSweepSelfIsGuest` had to be repaired mid-change for the
  same reason (`SelfOwned` → `SelfRepoGuestCheckout`, `832794fe`), because
  `repoRootOf` walks up — the first `git rev-parse` of ANY suite run in a
  subdirectory of a checkout used to answer "guest" about a repo it could not
  name, and the sweep stopped sweeping. Here the "identity unreadable" case is
  refused ONLY when the actor's own `.git` is a gitdir backlink naming
  `…/worktrees/<name>` — i.e. it is demonstrably a guest of SOME repository
  (arm `guestRowUnresolvedGuestRefusal`); otherwise the arms keep their mandate.

## Reproduction, before and after

Recipe (memory #4542): plant the probes in a ledger the run reads, run the
deleting test FROM a linked worktree with a watcher on the paths, then compare.
Rows are inserted with `sqlite3` directly; the "dead holder" is a pid this host
can prove dead (`peerbox:903924`, a long-gone pid).

```bash
S=/home/user/workspace/_scratch/evict
# two decoys registered + aged past DefaultWorktreeSweepMinAge (24h)
git -C /home/user/work/GoPi worktree add --detach /home/user/workspace/gopi-decoy3 master
git -C /home/user/work/GoPi worktree add --detach /home/user/workspace/gopi-decoy4 master
for w in gopi-decoy3 gopi-decoy4; do g=$(sed 's/^gitdir: //' /home/user/workspace/$w/.git)
  touch -d '-3 days' "$g/gitdir" "$g/HEAD"; done
# row-owned probe (A) + unowned probe (B) — one per decoy, plus a row naming the
# run's own cwd worktree, in a COPY of the live ledger the run is pointed at
nohup $S/watch.sh $S/watch1.log /home/user/workspace/gopi-decoy3 \
      /home/user/workspace/gopi-decoy4 /home/user/workspace/gopi-repro &
cd <the run's own linked worktree>
env GOPI_MEMORY_DB=$S/prep.db go test ./cmd/agent/ \
    -run 'TestTaskLifecycleTick_ReclaimsDeadHolderWorktree' -count=1 -v
```

### BEFORE (`master` @ a32c4ea0, run from the guest worktree `gopi-repro`)

    --- PASS: TestTaskLifecycleTick_ReclaimsDeadHolderWorktree (4.66s)
    removal-audit: worktree-removal record for /home/user/workspace/gopi-decoy3 …
    removal-audit: worktree-removal record for /home/user/workspace/gopi-decoy4 …
    GONE_AT 15:44:26.511582118 /home/user/workspace/gopi-decoy3
    GONE_AT 15:44:26.729883925 /home/user/workspace/gopi-decoy4
    $ ls -d /home/user/workspace/gopi-decoy3 /home/user/workspace/gopi-decoy4
    ls: cannot access '/home/user/workspace/gopi-decoy3': No such file or directory
    ls: cannot access '/home/user/workspace/gopi-decoy4': No such file or directory

Both peer checkouts were freed by the one tick, with NO refusal line anywhere —
and `gopi-decoy4` was owned by NO row at all (the ownership-independent half of
the same pass). The run's own cwd (`gopi-repro`, named by a planted row) was the
one path that survived in this test, because the fixture's own `GOPI_MEMORY_DB`
pin was overridden by the child's env: the unit test
`TestGuestRowGuardRedProof` drives that arm directly (below).

### AFTER (branch `fix/guest-row-reconcile-refusal`, same command, same probes)

    --- PASS: TestTaskLifecycleTick_ReclaimsDeadHolderWorktree (3.89s)
    PASS
    ok  github.com/nick/gopi/cmd/agent  4.468s
    EXIT=0
    /home/user/workspace/gopi-decoy3
    /home/user/workspace/gopi-decoy4
    /home/user/workspace/gopi-repro2
    --- watcher log: empty (no GONE_AT line at all)

## Red proof (mutation, quoted)

`guestRowGuardOffEnv` (`GOPI_TEST_GUEST_ROW_GUARD=off`) turns BOTH wired
predicates into the pre-fix behaviour (`""`) from the fixed source, so the same
subtests can be driven against a tree with no guard:

    $ go test ./internal/tools/ -run TestGuestRowGuardRedProof -count=1
        guest_row_guard_test.go:394: guest frees its OWN row of its own repo
        row t-guest-row-a of …/006/repo: worktree …/006/wt gone=true, want gone=true
        ROW dead-holder worktree reclaimed …: feat/guest-row-guard (t-guest-row-a) freed for merge; branch kept

(i.e. with the guard disabled the peer/own checkouts ARE freed — the pre-fix
outcome), while the production run keeps them and prints the arm:

    $ go test ./internal/tools/ -run TestGuestRowRefusesToReconcileFromAGuestWorktree -count=1
        [task-notify] dead-holder reclaim feat/guest-row-guard (t-guest-row-a) skipped:
        guest-row-reconcile refusal (arm guestRowReleaseRefusal): this process stands
        in the linked worktree …/wt, not in the main checkout …/repo of that repository …
        OUTCOME …/wt exists=true

Note on the red proof's shape: the guard's own SOURCE does not exist on the
pre-fix tree, so "delete the guard" cannot be the mutation there. The switch is
the equivalent mutation (the predicates' body), `TestGuestRowGuardRedProof`
drives it in-suite, and `TestGuestRowGuardIsWiredAndNarrow` asserts the wired
predicates are reachable and refuse nothing on ground they cannot identify (so
the file cannot pass vacuously against an unwired build).

## Not done / honest limits

* The reproduction's decoys are planted in a COPY of the live ledger plus the
  real repo registrations; the full-suite run below is the stronger end-to-end
  statement for the run's OWN worktree.
* `internal/tools`' full-package run exceeds Go's 10m package budget on this
  host's disk-backed scratch ledger (the reason `scripts/test-budget.sh` exists);
  the branch-scoped selection (`TestReclaim|TestGuestRow|TestWorktreeSweep*|
  TestSweepKeeps|TestFinishCleanup`) is what was run and is green.
* No load generator was used (Rule 27); a suite run is not load.

## Evidence (self-filed)

* Pre-fix, from a linked worktree, one lifecycle test:
  `ls -d /home/user/workspace/gopi-decoy3 /home/user/workspace/gopi-decoy4` →
  `No such file or directory` (both), watcher `GONE_AT 15:44:26` ×2, and the
  removal-audit lines naming both paths (quoted above) — the deletion is
  attributed to the tick, not inferred.
* Post-fix, same command: `EXIT=0`, watcher log EMPTY, and `ls -d` printing
  `/home/user/workspace/gopi-decoy3`, `/home/user/workspace/gopi-decoy4`,
  `/home/user/workspace/gopi-repro2`.
* Red proof: `go test ./internal/tools/ -run TestGuestRowGuardRedProof -count=1`
  with `GOPI_TEST_GUEST_ROW_GUARD=off` frees the checkouts (pre-fix outcome);
  without it they survive and the refusal names its arm.
* Branch-scoped suite: `go test ./internal/tools/ -run
  'TestReclaim|TestGuestRow|TestWorktreeSweepRefuses|TestWorktreeSweepSelf|TestSweepKeeps|TestFinishCleanup' -count=1`
  → `ok github.com/nick/gopi/internal/tools 64.878s`.
