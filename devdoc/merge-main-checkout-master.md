<!-- doc-class: normative -->
# devdoc: the main checkout must be on master before a fast-forward

> **Retired mechanism (merge verb removal, 2026-11-01).** Every `gopi merge …`
> below is the RECORD of the 2026-10 run this document diagnoses; the top-level
> verb was removed afterwards and `./bin/gopi merge` exits **1** today. The two
> defects it describes are in the ENGINE, which survives as `gopi task merge`
> (`man/gopi-merge.md`), so the analysis is still live work even though the
> spelling is not. `--fast` has no counterpart on the surviving face.

Epic: `gopi merge --fast` work in master. Two coupled defects:

1. **The landing ran wherever the checkout was.** Every landing in this system is
   `git merge --ff-only <branch>` executed in the repo's **main checkout**, so it
   fast-forwards *whatever branch that checkout has checked out*. A checkout parked
   on another branch made the landing move the wrong ref.
2. **A worker left the repo parked.** A run that did its work in the main checkout
   finished there instead of restoring the default (master) checkout state.

## The measured failure mode (worse than "ff-merge does not work")

Reproduced by hand on a three-branch fixture (`master`, `feat/land`, `parked`
where `parked` contains `feat/land`):

```
before: HEAD=parked master=1995133 parked=ce9a848 feat/land=e04a7f2
$ git merge --ff-only feat/land
Already up to date.
merge exit=0
after:  HEAD=parked master=1995133 parked=ce9a848
```

git exits **0** and prints "Already up to date." while **master never moved**. In
`gopi merge --fast` that exit-0 is the *success* signal: `mergeDecideBranch`'s
step ② takes it as a landing, writes `landed`, and then **prunes the branch ref**.
So the work leaves the merge queue while the report says it arrived — the branch is
spent and master does not have it. (An unreachable move is the milder arm: the
parked branch is not an ancestor, so git fails with "not possible to
fast-forward" and that branch is merely reported as not-ff.)

**The first-order defect was the ordering.** A branch checked out *anywhere* in a
registered worktree — the main checkout included, since `git worktree list` lists
it first — is classified worktree-held and **excluded from the candidate set**
(`repoSweepClassify` / `mergeCandidateBranches`). So a repo whose main checkout is
parked on the run's branch presents **no candidates at all**: a restore placed
after the discovery would never fire on the very state it exists to repair. The
restore therefore runs **before** the candidate discovery.

## The fix

ONE implementation of the repair, called from both ends:

`internal/tools/task_finish_cleanup.go`
- `FinishCleanupMainCheckoutRestore(ctx, repo, branch)` — resolves the repo's MAIN
  checkout (`finishCleanupMainRepoDir`, the `--git-common-dir` convention), reads
  its HEAD, and puts it back on the integration branch. It refuses (and never
  forces): a **detached HEAD** (a fast-forward there moves no branch), an
  **unreadable** HEAD, and a checkout holding uncommitted **tracked** work (the
  project's own tracked-only policy at this seam — `AdoptionGitStatusTracked`;
  untracked files are left in place by a checkout that need not overwrite them, and
  git itself refuses when one *would* be overwritten).
  - No process scan gates the move, deliberately: refusing while any live process
    stands in the main checkout would freeze the merge queue on a busy host. The
    move touches no commit (the parked branch keeps its tip), so its worst case is
    a tree refresh the landing performs anyway.
- `IntegrationBranchForRepo(repo)` (in `repo_sweep.go`) — `master`, or `main` when
  the repo has no `master` ref. Resolved from refs, never assumed: `checkout master`
  in a main-only repo would fail and report the repo permanently parked.

`internal/tools/repo_sweep.go` — the resident sweep's `①a`: restore the checkout
**before** the candidate discovery, gated by `repoSweepHasCandidates` (a repo with
nothing to decide is not moved at all). The outcome rides `RepoSweepResult.Checkout`.

`cmd/agent/merge_run.go` — `mergeSweepRepo`: same restore, also **before**
`mergeCandidateBranches`, reported through `mergeCheckoutLineFormat`. A refusal
skips the repo (no branch is landed from a checkout that is on another branch) and
the branches stay in `refs/heads` for the next run. *(Re-measured 2026-11-01: that
file, and with it `mergeSweepRepo` / `mergeCandidateBranches` /
`mergeCheckoutLineFormat`, was deleted with the top-level merge verb — the sweep
half above (`internal/tools/repo_sweep.go`, `①a`) and the restore primitive below
are what this tree still has.)*

`internal/tools/main_checkout_restore.go` — `FinishCleanupRestoreMainCheckout`, the
**worker** half: called from `taskFinishCleanup` **before its mode split**, because
every guard above that point returns early (no row, a live status, no
deliverable, no repo) and each of those returns is a run that can still have parked
the repo. It releases the process's own CWD first (`internal/worktreerelease`), and
it is a no-op for a run whose ground is a LINKED worktree — such a run cannot have
moved the main checkout at all (Rule 29). `finishCleanupMainCheckoutPlannedStep` is
its plan-mode face, so a dry run cannot understate the apply.

## Why the restore reports even when nothing lands

The sweep restores for **any** tick with candidates, and reports the parked state
*before* the damage: the parked checkout is the condition that makes the *next*
landing wrong. A repair is always stated (`MainCheckoutRestoredMarker`), a refusal
is always stated (`MainCheckoutParkedMarker` + reason), and the ordinary case —
already on the integration branch — is **silent**, because a line that fires on
every run trains the reader to ignore the one that matters.

## Verification

- `internal/tools/main_checkout_restore_test.go` — the restore's arms, plus
  `TestMainCheckoutRestoreRepairsWhatTheSweepWouldOtherwiseMiss`, the **ordering**
  property: it asserts the sweep's own repair report and that the candidate landed,
  so it cannot pass vacuously. Red-proof: with the two call sites disabled, it
  fails with `the sweep did not report the parked checkout it repaired: ""`.
- `internal/tools/main_checkout_closeout_test.go` — the worker half (repair silent
  no-op, linked-worktree no-op, dirty refusal, plan prediction).
- `cmd/agent/merge_fast_test.go` —
  `TestMergeFastLandsIntoMasterWhenTheCheckoutIsParked` is the command-level
  regression test for the measured defect. Red-proof: with the call sites disabled,
  it fails with `master does not contain the candidate's tip … — the landing went
  to the PARKED branch`. Plus the tracked-dirty skip arm and the not-weakened
  ordinary-state pin.

## Not done (honest)

- **The suite-budget overflow is NOT fixed here, and is pre-existing.** Both
  `internal/tools` and `cmd/agent` exceed Go's 10m package timeout on this host,
  and an A/B against unmodified `master` (`8647bf21`) shows the same failure at
  the same duration with no changes applied — `cmd/agent` `FAIL … 600.726s` on the
  baseline vs `FAIL … 601.321s` on this branch, `panic: test timed out after 10m0s`
  in both. The baseline's timeout stack names its own last test
  (`TestVoiceServeHTTPHandlerContract`), which this diff does not touch. So this
  diff neither causes nor repairs it; the tests this branch ADDS all pass in
  isolation. (The underlying cost is the ledger's first-open schema batch
  committing inside one transaction — PROJECT.md Rule 27's note on the retired
  `iopress` epic. The correct instrument there is timing `tx.Commit()`, never
  device load.)
- The **recurrence guard**: this change restores the checkout at both ends, but
  nothing mechanically forbids a future `git merge --ff-only` call site from being
  written against the main checkout without restoring first. A gate over the
  landing call sites (assert every `--ff-only` runs where the integration branch is
  checked out) is the natural follow-up and is **not** in this branch.
- No live end-to-end run of `gopi merge --fast` against a real parked repo was
  performed; the evidence is the unit + command-level tests above and the by-hand
  git reproduction quoted here.
