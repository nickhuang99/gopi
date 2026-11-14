<!-- doc-class: historical -->
# Clients force-delete their branch; the two `-d` readers next to it must not follow

**Date:** 2026-10-18 · **Task:** 01a14b8e · **Branch:** `task/01a14b8ecca97dfcaeef41ebbf669fed`
**Commits:** `61af5aadb`, `622545840`

## The claim this task started from, and what was actually true

The briefing said: `finishCleanupBranchForceDeleteFlag` is *defined but nobody uses
it*, so the client's close-out still runs `git branch -d`, which can never succeed
there. **Half of that was wrong.**

Measured on master with `git log -S`:

```
$ git log -S'finishCleanupBranchForceDeleteFlag, branch' --oneline -- internal/tools/task_finish_cleanup.go
287a831c4 fix(cleanup): never delete a branch the sweep did not deliver or a live row works through
```

Step ④ (`internal/tools/task_finish_cleanup.go:5586`) has been
**force-only since 287a831c4 / task 01a131bb** — the `-d`-then-`-D` fallback was
removed there, behind the same two guards (axis 2: *this chain's own push landed
the ref*; orphan guard: *the tip survives in another ref, the delivery point
included*). Changing it again would be a regression, not a fix.

The real population of `-d` readers is elsewhere, and both are deliberate:

| site | who | why `-d` is right there |
|---|---|---|
| `worktree_sweep.go:2665` (`worktreeSweepDeleteBranch`) | host-agnostic sweeper rung | guards with `worktreeSweepBranchIsSpent` (`merge-base --is-ancestor <b> <master>`); a client's `task/<id>` never satisfies it, so this rung cannot touch it |
| `task_merged_cleanup.go:728` (orphan lane) | server-side merge housekeeping | guards with `mergedCleanupStillMergedReason` (race-free "still master's ancestor" recheck) |
| `task_merged_cleanup.go:630` (pair lane) | server-side merge housekeeping | same, plus `branchDeleteNonDurableTipRefusal` |
| `task_merge_executor.go:320/368`, `task_merge.go:779/802`, `cmd/agent/merge_run.go` | the merge itself | the merge just landed, so `-d`'s question is genuinely YES |

**So the deliverable was evidence, not another flag change:** pin *why* ④'s force
half is correct on a client, so a future reader does not "restore safety" by
putting `-d` back.

## What the new tests pin (`task_finish_cleanup_force_delete_test.go`)

`TestClientCloseoutForceDeletesABranchLocalMasterNeverMerged` measures, in ONE
fixture at ONE tip:

1. `merge-base --is-ancestor <tip> master` fails — the client never merges;
2. a plain `git branch -d` on a ref at that tip is **refused by git**, with its
   own `not fully merged` sentence (measured *after* the chain, on a throwaway
   ref at the same tip — see the ordering trap below);
3. the chain's ④ executes `branch -D <branch>` **exactly once**, and never the
   safe half (argv from a PATH shim, not from reading the code);
4. the local ref is gone **and the delivery point holds the same tip** — the
   delete is a handover.

`TestClientCloseoutKeepsTheBranchWhenTheDeliveryDidNotLand`: when the push is
refused (`receive.denyCurrentBranch`), the chain spawns **no `git branch`
deletion at all** and the ref survives. Asserting an *empty argv log* is stronger
than asserting a refusal: it says the delete was never authorised, not that it
failed.

## Three traps worth keeping

**(1) Ordering: the `-d` probe must run AFTER the chain.** A probe ref created at
the same tip before the run is a second container of that tip — it would satisfy
the ORPHAN guard on the chain's behalf and make the test prove something the
chain did not earn. It also lands in the shim's argv log, and the argv assertions
are about what the *chain* ran. Both problems vanish by taking the reading after.

**(2) Two gates pointing in opposite directions.** The new Rule 13 mirror gate
(no bare `"-d"` / `"-D"` outside the definition site) forced
`task_merged_cleanup.go:630` from `"branch", "-d"` onto the shared constants.
That single edit turned an *unrelated* census red:
`TestEveryBranchDeleteSiteAsksTheNonDurableTipLock` counted only the inline
spelling, so the population went 8 → 7 and it failed `"at least 8"` — while the
site it lost was still there, still deleting, still asking the lock.

  **A census that cannot see a spelling reports a count about a different set
  than the one it claims.** And because Rule 13 pushes writers *toward* the
  constants, a literal-only census shrinks every time the code gets better. Fixed
  in `622545840`: both spellings count, `worktreeSweepObserve` is recognised as a
  record rather than a site, the two real guards the blind spot had hidden
  (`worktreeSweepBranchIsSpent`, `mergedCleanupStillMergedReason`) join the lock
  list, and a site that relies on a *different named* guard says so with
  `branch-delete-ok:<reason>` on its own line — nothing else could be dropped
  quietly. Now: `census: 10 ref-removing branch -d sites, all of them preceded by
  a lock ask`.

**(3) `make test-budget` on this tree has pre-existing reds.** 5 of them, none in
a file this branch touches (`git diff --name-only master...HEAD` lists 4 files);
each is reproduced on master by object-store read: stylegate's `test-sleep`
ceiling (4 sites), the CONFIG-ANCHOR gate (8 ROT anchors in `cmd/agent/*`),
`TestFinishCleanupWorktreeVerbLiteralGate` (`archive_reason.go:440`, unchanged
since `38b73e48b`), and the red-proof-restore gates on the same file. Filed as
bug rows `01a14bcb…` and `01a14bda…` (Rule 41).

## Live evidence (Rule 31)

```
./bin/gopi task finish-cleanup --apply --task 01a13652-1e68-7641-866a-32132a1d15f9
```

- BEFORE: `task/01a13652…` = `6fc79ed7…`; `merge-base --is-ancestor <tip> master`
  → **NO**; `git ls-remote peerbox refs/heads/<branch>` → **empty**
- chain: `steps that DID run=[branch-checks,push-target,push,stale-registration,branch]`, `settled=1`
- AFTER: ref gone locally; `git ls-remote peerbox` → `6fc79ed7… refs/heads/task/01a13652…`
- `removal-audit.log`: `arm=finish-cleanup-branch` and `arm=finish-cleanup-branch-force`,
  both `sha=6fc79ed7…`, `delivery_point="peerbox:~/work/GoPi"`

Raw readings: `~/.gopi/sessions/live-evidence-01a14b8e/{before,apply,after}.txt`.
