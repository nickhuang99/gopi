<!-- doc-class: historical -->
# The close-out refuses a ground a live run still stands on (SL3-2)

## What happened (measured, 2026-09-26)

A probe-started agent run inherited a live session's environment and reached its
own close-out. It freed **that** session's worktree, then its branch ref:

```
11:54:30  {"kind":"worktree-removal","arm":"finish-cleanup-worktree-force",
           "target":"/home/user/workspace/gopi-01a0dcfa-d9a6…",
           "evidence":"delivery chain's live-owner gate did not refuse
                       (no live owner of this worktree); checklist own-ground +
                       integration-lineage (reclaim_ground_checklist.go)"}
```

The audit sink records **both** halves, and the pair is the whole diagnosis:

```
11:24:03  {"kind":"worktree-ground","arm":"task-ground-anchor",
           "pid":2938071,"holder_pid":"2938071","holder_start":"239893298",
           "holder_sid":"1247531"}
```

## Why every existing rung was blind

The closing row was **terminal**: `status=completed`, and the ledger clears
`proc_pid`/`proc_start_ticks`/`proc_cmd` on every non-live transition — the
measured row reads `proc_pid = 0`. So:

* `finishCleanupLiveOwnerGate` / `finishCleanupLiveOwnerBlockReason` — ask about
  **live** rows only; a terminal row is not one.
* `finishCleanupWorkCompletionReason` — status-gated.
* `finishCleanupOwnerRowGate` / `finishCleanupForeignOwnerReason` —
  `claimed_by`-gated, and a claim stamp's pid carries **no start-ticks
  incarnation**, so it cannot tell "the claimer" from a recycled number.

None of them can answer *"is a RUN still standing on this ground?"* once the row
is terminal — which is exactly when a close-out runs.

## The record that does answer it — and had one reader, in the wrong place

The run-ground anchor is written **by the taker** at acquisition
(`TaskGroundAnchorRecord`, `internal/tools/run_ground_anchor.go`), and the
terminal transition cannot erase it. Its one reader, `RunGroundAnchorLiveness`,
was wired into the **sweep** only — never into the chain whose act destroyed the
directory.

## The guard

`internal/tools/task_finish_cleanup_run_identity.go` —
`finishCleanupRunIdentityRefusal(worktree, branch)`:

1. read the anchor for the candidate directory (first match = the taker), and
2. ask `RunGroundAnchorLiveness` — the ONE existing reader, never a second pid
   ladder (Rule 14); every uncertain rung of that ladder KEEPS the ground, and
   here "keep" means refuse, never a guess.

A live taker that is **not the asker** ⇒ a named refusal, nothing removed and no
ref moved. Wired into **both** worktree-removal arms (the two
`reclaimGroundChecklistVerdictForCloseout` call sites) and into the branch step
(⑭, resolving the registered worktree via `finishCleanupBranchWorktree` first —
the incident's second effect: the ref moved after the directory was gone).

### Two subtleties, both measured on a real host

* **The asker's own upward chain is excluded.** The anchor's taker is the run's
  **wrapper**, i.e. an *ancestor* of the process executing the close-out
  (measured here: `…→3490477 gopi→3007821 gopi`, and `3007821` **is**
  `GOPI_TASK_RUN_WRAPPER_PID`). Without the exclusion every legitimate close-out
  refuses its own ground and no worktree in the fleet is ever freed. Only the
  UPWARD chain (`finishCleanupSelfChain`) is excluded — the downward closure
  (`finishCleanupSelfTree`) forgives a live runner that is the asker's
  *descendant*, which this tree measured as harmful.
* **An anchor with no ticks+session reads LIVE** (the ladder's fail-safe rung).
  A test fixture must therefore record a real session that then dies; a fictional
  no-identity anchor measures the ladder, not the guard.

### Why this is NOT the rejected widening

* **Not** "refuse every open row" — no status axis changed, so the dead-local-holder
  orphan (`TestDeadLocalHolderReleasesTheDirectory`) still releases: its taker is
  provably dead.
* **Not** `git merge-base --is-ancestor` — no lineage is asked, so the six recycle
  arms keep working: the directories their rows name carry **no anchor**, and an
  unmeasured ground is not a refusal (it is the pre-existing behaviour).

## Rule 31 — the real surface

Private fixture (real repo + linked worktree + real ledger + a real live holder),
`role: server` so no `sync_peers` exists and the worktree step is reached. ONE
variable, the taker's liveness:

| taker | `gopi task finish-cleanup --apply --task …` |
|---|---|
| **LIVE** (standing in its own session dir, not on the ground) | `skipped: ground still stood on by a live run: … belongs to a live run — not freeing it` + `refused=1`; `git worktree list` unchanged, ref unchanged, directory present |
| **DEAD** (same command, taker killed) | `removed=1`, `worktree-freed=…`; ref preserved |

The live-holder arm had to be built carefully: a holder whose **cwd is the
ground** is refused *earlier* by `finishCleanupWorktreeInUse` (an unclassifiable
orphan), so it never reaches the new guard. The incident's shape — and therefore
the only fixture that measures this guard — is a live taker standing **elsewhere**
while the ground is still its own.

The **pre-fix red** was measured on the SAME fixture, with a binary built from
`git archive master` (`grep -c finishCleanupRunIdentityRefusal` in master's
`task_finish_cleanup.go` → `0`): the close-out reported
`done=[align-before-delivery,pre-merge,worktree] worktree-freed=…/fixture-wt`,
`removed=1 settled=0 refused=0` and wrote a `worktree-removal` record **while
`ps -p <taker>` still showed the taker alive**. That is the incident, reproduced
by the binary the branch replaces — and the reason this guard is not a
hypothesis.

## Scope: the pushback path (closed 2026-09-26)

The note above left `task_sync_git_pushback.go` as **scope, not as a claim**. That
question is now answered by measurement. Every ref the pushback path (plus the
delivery half it serves) can move, and on which directory:

| ref | moved at | directory |
|---|---|---|
| `refs/remotes/gitPushcheck/<target>` — the lane's OWN mirror namespace, read by nobody else | `gitPushbackFetchMaster` (`task_sync_git_pushback.go:233`), pruned by `gitPushbackPruneMirrors` (`:547`); the root const is `gitPushbackCheckRootRef` (`:61`) | the repo dir handed in |
| `refs/heads/<delivered>` on the PEER | `git push <target> <branch>:<branch>` — the refspec is built from the ROW's branch, never from a directory's contents | the peer |
| `refs/heads/<delivered>` LOCALLY | **nothing in the close-out touches it** — the trial merge (`finishCleanupAlignDelivery` → `finishCleanupLocalSync`, in the now-removed `task_finish_deliverysync.go`) used to move it, and the branch's own check that replaced it reads it without moving it | the SHARED main repo |

**No other ref, and no other name.** Nothing in the package takes a ref name from
the ground's contents, so a *foreign-named* ref cannot be moved or deleted by this
path — the delete (`④`) is the row's own branch name, and it sits behind `③`.

**But the foreign GROUND was reachable all the same, and that is the defect.**
The delivered branch's ref lives in the **shared main repo**, while the foreign
live run's linked worktree is a *view* of it (`git-common-dir` → the main repo).
So a `--repo <main>` close-out of a row whose ground is a live foreign run's
worktree could align — and thus move — that shared ref, and the live run's
checkout changed under it while its directory and its HEAD ref name stayed put.
Measured on the real CLI with the binary built from `git archive master`:

```
--- refs BEFORE   refs/heads/feat/rule31-scope  cf49acc…     taker alive: yes
    gopi task finish-cleanup --apply --task 01a0df17-… --db …/memory.db
01a0df17-… branch=feat/rule31-scope done=[align-before-delivery,pre-merge,push]
finish-cleanup (APPLY): candidates=1 removed=0 settled=1 refused=0     ← settled, holder ALIVE
--- refs AFTER    refs/heads/feat/rule31-scope  a0fe8fd…     taker alive: yes
```

and the refusal that *did* fire came only at `③`, one step too late, saying
`nothing was removed and no ref was moved` — while `①a` had just moved one
(that sentence is now reworded; Rule 15's silent failure, inverted).

### The fix: the same guard, moved to the ref-moving decision point

`①0` in `internal/tools/task_finish_cleanup.go:3744` asks the SAME
`finishCleanupRunIdentityRefusal` — no second pid ladder (Rule 14) — before the
first act that can merge into a ground or move a ref, resolving the ground
exactly as the REMOVAL phase does (`finishCleanupBranchWorktree`, so a row that
records no worktree is still caught by the branch registry). Nothing about a
row's delivery is owed to a directory another live run owns.

| taker (same world, same command, one variable) | `gopi task finish-cleanup --apply --task …` |
|---|---|
| **LIVE** foreign run, branch binary | `refused=1`; `refs/heads/feat/rule31-scope` **byte-identical**; ground present; holder alive |
| **DEAD**, branch binary | `settled=1`; the ref moved by the alignment (`git merge-base --is-ancestor` holds) — the legitimate case is untouched |
| **LIVE** foreign run, `git archive master` binary | `settled=1`; ref MOVED under the live holder — the defect, reproduced |

Go pins, in-package: `internal/tools/task_finish_cleanup_run_identity_pushback_test.go`
— `TestPushbackPathMovesNoForeignRefThroughARealGround` (①the ref universe over a
real linked worktree) and `TestPushbackPathMovesAndDeletesNothingForALiveForeignRun`
(②the live-foreign arm, which asserts the guard fires and **no ref-moving step
ran**). RED on master's `task_finish_cleanup.go` (`a REF-MOVING step ran for a
ground a live foreign run owns`), GREEN here; the four union pins unchanged.

## Not done (still honest, now narrower)

* `task_sync_git_pushback.go` was **not** changed, and does not need to be: the
  table above is exhaustive for it, its only writable ref of its own is its
  scratch mirror namespace, and the delivered-branch ref it pushes is generated
  from the row rather than from a directory — see "## Scope: the pushback path"
  (closed 2026-09-26). The reachable half was the delivery half, and it is now
  guarded at `①0`.
* The guard covers **this** close-out chain's steps; the reclaim/sweep arms have
  their own (pre-existing) liveness ladder and were left alone.
* `①0` is wired on the `--apply` delivery path; the PLAN face renders the same
  chain's predictions and asks no peer (unchanged by this work).
