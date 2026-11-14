<!-- doc-class: historical -->
# The merge hand-over backstop — a failed resolution child must not leave the MAIN checkout half-merged

**Epic**: `merge-handover-main-repo-backstop` (task `01a0cde3-1830-7694-b065-83eeaf9c7111`), 2026-09-23.
**Files**: `cmd/agent/merge_backstop.go` (new), `cmd/agent/merge_run.go` (`mergeHandOver` failure arms),
`cmd/agent/merge_backstop_test.go` + `cmd/agent/merge_redproof_test.go` (tests).

## The defect

`gopi merge` hands a branch it cannot land by git alone to a child agent
(`mergeHandOver`, briefed by `cmd/agent/merge_prompt.md`). The child's conflict
test is prescribed by that prompt (§3.3) as:

```
git -C <repo> merge --no-commit --no-ff <branch>     # "the ONLY reliable conflict test"
```

That command runs **in the MAIN checkout** and leaves the merge in the index while
the model works — deliberately, because the child has to inspect and resolve the
conflicts. Nothing owned that state afterwards. When the child ended with an error
(the loop detected a stuck pattern, the process was killed, the context was lost),
`mergeHandOver` recorded the failure and returned, and the MAIN repo kept
`MERGE_HEAD` plus unmerged paths.

**The cost is not one blocked branch — it is the whole repo.** The next
`gopi merge` refuses the repo through `mergeSweepRepo`'s tracked-dirty gate
(`mergeDetailTrackedDirty`, which fires on the `UU` entries) *before* anything asks
about `MERGE_HEAD`. Every later merge is parked until a human cleans the tree by
hand. Measured twice on 2026-09-23: a killed merge child (task `01a0cda1`, session
`2d06d79e`) left the checkout unmerged, and a later hand-run merge in the same
session had to abort that state by hand before its own landing could be attempted.

## The fix

A run-level backstop on the failure arm only: `mergeHandOver` claims a **lane
marker** before the child starts, releases it when the child returns, and on every
failing door (child construction failed / `ag.Run` returned an error / the test
seam reported an error) calls `mergeRunHandoverBackstop` — which rolls the main
checkout back out of the merge and *verifies* the rollback by querying the tree.

### The four limits, and why each one exists

1. **The merge must be ours, and "ours" is proven positively.** The child is an
   agent: it may probe a branch the ceiling never handed it (session `2d06d79e`'s
   first act was `git merge --no-commit --no-ff` on a branch the command had
   already filed for rework). So the hand-over *list* cannot be the proof of
   ownership. A marker file (`gopi-merge-handover-lane`) is written into the repo's
   **git dir** (via `rev-parse --absolute-git-dir`, so a linked worktree — whose
   `.git` is a file — resolves correctly; it can never appear in `git status`, so
   Rule 30 holds) and is bounded by age (`mergeBackstopLaneStaleAfter`, 24h): the
   marker of a *killed* run survives by construction, and it must never authorize a
   rollback of a merge a human started in that repo a day later. Two cheap
   questions are asked afterwards as well: the checkout must be ON the integration
   branch, and what is being merged must be a **branch of this repo**
   (`for-each-ref --points-at MERGE_HEAD refs/heads/`) — a cherry-pick, a revert or
   a `git merge <sha>` is not this command's shape.
2. **A foreign edit is never rolled over.** `git status --porcelain` is *never*
   empty while a merge is in progress (conflicted paths are `UU`, auto-merged ones
   `M `), so the cleanliness question is asked narrowly: a path dirty in the
   **second column only** (` M`, ` D`) is an edit somebody made in the main
   checkout, and its presence refuses the rollback and names the path. Reading the
   obvious predicate (`AdoptionGitStatusTracked != ""`) instead would have made
   the backstop dead code — it can never pass while a merge exists, the incident's
   own state included.
3. **The checkout's branch is never moved.** The first attempt at this incident in
   the session log used `git checkout --detach master` to free the ref before
   aborting; the operator is then standing on a detached HEAD — a different broken
   state, and a worse one. The backstop checks nothing out.
4. **Nothing but the merge state is discarded.** `git merge --abort` first; if git
   *refuses* (exit 128, `Entry '<path>' not uptodate. Cannot merge.`), `git reset
   --merge ORIG_HEAD` — literally "abort back to ORIG_HEAD", which **keeps**
   unstaged work that is not part of the merge. `reset --hard`, `stash`, `clean`
   and `checkout -- <path>` are used nowhere in the file.

### The residual, stated rather than hidden

One measured shape survives both arms (git 2.34.1, probed 2026-09-23): a path the
merge staged and the child then edited further without re-staging (`MM`) — the
merge's own path, so limit 2 does not refuse it, and both restore commands refuse
to discard the unstaged edit. The backstop then prints
`STILL half-merged (…refusal…)` with git's own words, so the operator knows the
state is still there. Nothing is loosened to hide it: `mergeDetailTrackedDirty`
and `finishCleanupWorktreeDirtyReason` are untouched, and a repo the backstop
could not repair reaches exactly the refusal it reached before.

`git merge --abort` succeeding or refusing was measured before the design was
written, on the shapes a killed child actually leaves:

| fixture state | `merge --abort` | `reset --merge ORIG_HEAD` |
|---|---|---|
| plain `UU` conflict | exit 0, MERGE_HEAD gone, porcelain clean | exit 0, same |
| conflict file rewritten, not added | exit 0, ` M` edit kept on disk | exit 0, same |
| the above + an unrelated tracked file edited | **exit 128**, state intact | exit 0, **the edit survives** |
| branch-added file staged (`A `) | exit 0, index restored | exit 0, same |

## Tests

| test | claim |
|---|---|
| `TestRedProofFailedHandoverLeavesMainCheckoutUnmerged` | **the red proof**: RED on master tip `d16dc74e`, GREEN on the branch (run both ways — see the task report for the two transcripts) |
| `TestMergeHandoverFailureLeavesMainCheckoutMergeable` | end-to-end: after a failed hand-over the checkout is clean, `MERGE_HEAD` is gone, the branch's side is not in the tree, the integration branch did not move, the branch ref survives, and the repair is reported |
| `TestMergeAfterHandoverFailureAcceptsTheRepo` | the epic's acceptance clause through the real surface: the NEXT run does not refuse the repo as tracked-dirty and hands the branch over again |
| `TestMergeBackstopRollbackOfTheKilledChildShape` | the decision function directly, asserting `tools.AdoptionGitStatusTracked` is empty afterwards — the same predicate the next run's gate reads |
| `TestMergeBackstopLeavesForeignMergeAlone` | no lane marker (a human's half-merge) ⇒ untouched, with the reason printed |
| `TestMergeBackstopStaleMarkerIsNotAClaim` | a marker aged past the bound is not a claim |
| `TestMergeBackstopRefusesAnEditItDidNotMake` | a foreign unstaged edit ⇒ refusal, path named, edit intact, `MERGE_HEAD` intact |
| `TestMergeBackstopDoesNothingWithoutAMerge` | a clean repo ⇒ no action, no output |
| `TestMergeBackstopRemovesItsLaneMarker` | no residue in the MAIN checkout's git dir |
| `TestMergeSuccessPathIsUnchanged` | the success path still lands the fast-forwardable branch and prunes it, hands only the conflicting one over, and leaves no merge state |

## Defect A — the same session's child was killed by the observation bound

The incident's child (session `2d06d79e`) did not die of its own failure; the agent
loop terminated it:

```
agent loop stuck: 40 consecutive rounds that only observed (no mutating tool call)
```

The mechanism: `internal/agent/toolclass.go` classes `bash` as an **observer** tool
("the loop cannot see argv-level intent"), so a round whose only call is
`bash python3 - <<PY … PY` writing files — or `git add` / `git commit` — is billed
as pure observation by `observationState`. The session's reconstruction shows the
bound firing on a genuinely-acting agent: the terminal observer-only streak
contains **20 file-writing or committing bash calls**, including
`git add … && git commit -q -F -` (session rows 429–540). The bound is not
wrong in principle — it exists for the look-without-fixing loop — but `bash` is the
one tool whose class cannot be decided from its name.

**Ruling: not fixed here.** Loosening `maxObservationRounds` is the one move the
epic forbids, and a proper fix needs argv-level classification — a system-layer
change in `internal/agent` (Rule 28: peerbox's layer), which is a separate task, not
a rider on this branch. The evidence and the exact commands are in the task report;
a bug ticket carries them.

## Why this is not a band-aid over a "stuck loop" symptom

The two defects are independent: the child in `2d06d79e` was killed at iteration
199 with 227 assistant rounds of real work behind it, and a *clean* child can fail
too (a crashed process, a lost context, an `ag.Run` error). The half-merged
checkout, not the kill, is what blocks every later merge — which is why the
backstop lives on the failure arm of the command rather than in the loop.
