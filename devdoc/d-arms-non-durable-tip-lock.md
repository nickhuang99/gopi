<!-- doc-class: historical -->
# The `-d` arms and the scratch mirror: measured, wired, and pinned

Task: `01a12f68-a72c-71ae-a5b0-52e5c18b79c6` · host `hosta` · branch
`task/01a12f68a72c71aea5b052e5c18b79c6`

This closes the gap the previous run left open on purpose
(`devdoc/archive-ref-holds-tip.md` §5b-2, task `01a12f48`): only two arms asked
`branchDeleteOrphanRefusal` (`task_finish_cleanup.go:4750`, `task_adoption.go:414`),
while every other ref-removing site trusted `git branch -d` — and that trust was
**READ, not measured**. Here it is measured, and the arms are wired.

## 1. The mechanism is REAL — and it is about the UPSTREAM, not about master

The brief's hypothesis was: `-d` refuses unless the branch is merged into its
upstream, and if that upstream resolves to the pushback lane's per-pass mirror,
"merged" can be satisfied by a ref about to vanish. **Confirmed**, by driving real
git in a fixture repo (`/home/user/workspace/gopi-01a12f68-fixture/{orphan-probe.sh,upstream-probe.sh}`;
raw output `…/sessions/01a12f68-…/artifacts/git-d-upstream-probe.txt`):

```
$ git branch -d topic
warning: deleting branch 'topic' that has been merged to
         'refs/remotes/gitPushcheck/topic', but not yet merged to HEAD.
Deleted branch topic (was 32a2803).            RC=0
$ git merge-base --is-ancestor 32a2803 master → rc=1   (NOT an ancestor)
$ git for-each-ref --contains 32a2803 → then the pass-end prune → (empty)
$ git cat-file -t 32a2803                     → commit
$ git fsck --unreachable --no-reflogs | grep -c 32a2803 → 1
```

Two findings this run added to the brief, both MEASURED, both narrowing the risk:

- **A plain fetch into the mirror namespace does NOT create an upstream.** The
  pushback lane's fetch (`gitPushbackFetchPeerRef`, `+refs/heads/*:refs/remotes/gitPushcheck/*`)
  leaves no `branch.<name>.remote` and no `%(upstream)`; `git branch -d` then
  REFUSED (`error: The branch 'topic' is not fully merged`, RC=1). The mirror can
  be an upstream only through an explicit `git branch --set-upstream-to`/`push -u`
  — which **this repo never issues**: measured on the real repo, the only branch
  with an upstream is `master → refs/remotes/peerbox/master`, a DURABLE ref, exactly
  the shape git's rule was written for.
- **The integration branch has nothing to do with git's lock.** `-d` consulted
  the upstream at a tip that was an ancestor of nothing, and accepted it.

So the hazard is real but unobserved in the field; the arms' actual exposure is
narrower and is stated in §3.

## 2. It is NOT reachable through `gopi merge`'s three `-d` sites — measured

Each of those sites reads that the tip is in the integration branch **in the same
invocation**, and two of the three MOVE the integration branch onto the tip first:

| site | the read that precedes the delete | verdict |
|---|---|---|
| `cmd/agent/merge_run.go:2039` (spent-ref) | `merge-base --is-ancestor <branch> master` | master contains the tip |
| `cmd/agent/merge_run.go:2052` (ff landed) | `git merge --ff-only <branch>` succeeded | master MOVED onto the tip |
| `cmd/agent/merge_run.go:2145` (union landed) | the union lane landed in master | master contains the tip |
| `internal/tools/task_merge.go:691/710` | same two reads as above | same |
| `internal/tools/task_merge_executor.go:306/348` | `mergeExecIsAncestor` + a committed merge | same |
| `internal/tools/task_merged_cleanup.go:613` | discovery ① + `merge-base --is-ancestor` recheck ④ | same |

Driven for real, with the lane's mirror planted as the branch's upstream
(`cmd/agent/merge_nondurable_upstream_test.go`, and the CLI run below): the run
fast-forwards the branch into master — i.e. **the tip's durable container is
created by the same invocation that deletes the ref** — so no ref-less tip is
possible on that path. The negative is therefore pinned, not assumed: the tests
assert `git for-each-ref --contains <tip>` non-empty **and naming the integration
branch** after a real `gopi merge` run over that fixture.

## 3. What was nonetheless wired, and why it is not a widened policy

The `-d` arms rest on a READ taken earlier, and in the worst case by **another
process on a different tip** (the merger holds no lock against the branch's own
run still committing). `git branch -d` is not a lock that catches that — measured
above. So `branchDeleteNonDurableTipRefusal` (a sibling of the existing
`branchDeleteOrphanRefusalAt`, in the same file, reusing its parts) now runs at all
eight `-d` sites and answers ONE question at the ref as it stands:

> is the tip in the integration branch NOW, or does a **durable** ref (branch,
> tag, another remote's tracking ref — the same classification the guard already
> owns) contain it?

If neither: the ref is KEPT, with a named refusal carrying the tip, the absence of
a binding upstream, and the recovery command. The refusal has **both arms in the
tests** — it fires on a scratch-only holder, and stays silent for a durable
holder and for a tip already in the integration branch — so the ordinary tidy is
provably untouched (the positive control: a spent ref is still pruned and reported
as `ref pruned`, both in-process and through the CLI).

The precedent it matches is the close-out chain's own entrance contract
(`finishCleanupDeleteDeliveredBranch` → the guard, escalating to `-D` only when
the guard allows): one behaviour, now one spelling, at the delete site.

## 4. Files

- `internal/tools/branch_delete_guard.go` — `branchDeleteNonDurableTipRefusal` +
  its evidence renderer, the exported wrapper for `cmd/agent`, and the two lock
  verb constants.
- `internal/tools/task_merge.go` (×2), `task_merge_executor.go` (×2),
  `task_merged_cleanup.go` (×1) — the lock at each delete site, fail-soft
  (report + keep, never force, never error).
- `cmd/agent/merge_run.go` (×3) — same, through `mergeSpentRefusal`.
- `internal/tools/branch_delete_nondurable_tip_test.go` — NEW: the measurement,
  the refusal's two poles plus two controls, **and the census** that fails the
  build if any `branch -d` site stops asking the lock (8/8 today).
- `cmd/agent/merge_nondurable_upstream_test.go` — NEW: the same fixture driven
  through the real `gopi merge`, asserting on git's refs and `--contains`.

## 5. Red → green (observed, not claimed)

Reverting the five arm edits while keeping the guard and the new tests
(`git diff -- <5 files> > arm-locks.patch; git checkout -- <5 files>; go test …; git apply arm-locks.patch`):

```
--- FAIL: TestEveryBranchDeleteSiteAsksTheNonDurableTipLock
    task_merge.go: a `branch -d` removes a ref without asking the non-durable-tip lock …
    task_merge_executor.go: …  (×2)      task_merged_cleanup.go: …
    ../../cmd/agent/merge_run.go: …      (×3)
    branch_delete_nondurable_tip_test.go:216: 0 of 8 delete sites ask the lock
```

raw: `…/sessions/01a12f68-…/artifacts/census-red-proof.txt`; the reverted diff is
kept as `…/artifacts/arm-locks.patch`. With the arm edits restored the same run is
green, with the census printing `8 ref-removing branch -d sites, all of them
preceded by a lock ask`.

## 6. Honest limits

- **The whole hazard rests on an upstream this repo never writes** (§1). The
  wiring is justified as a TOCTOU closer for the arms' earlier READs, not as a fix
  for an observed field orphaning. If a future lane ever passes `-u` on a branch
  push, this lock is what stands between it and a scratch-only holder.
- **One fetch shape was not exercised**: `git clone` (rather than `remote add`)
  also configures `branch.master.remote/merge` for the default branch. It writes
  `refs/remotes/origin/*`, never the pushback namespace, so it cannot produce this
  shape; stated as reasoning, not as a measurement.
- **`task_adoption.go`'s `-D` arm (line 422)** was left as the previous run left
  it: it asks `branchDeleteOrphanRefusal` at 414 and audits before the force. Not
  re-examined here.
- **The mechanism is not fixed, only guarded**: git's `-d` still accepts a
  scratch upstream — the protection is that this codebase never lets an
  upstream-bearing branch reach a `-d` site unasked.
