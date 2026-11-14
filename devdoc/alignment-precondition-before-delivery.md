<!-- doc-class: historical -->
# Alignment as a Precondition of Delivery

`internal/tools/task_finish_deliverysync.go`,
`internal/tools/task_finish_cleanup.go` — 2026-09-21,
branch `feat/align-precondition-before-delivery`.

## The requirement, in the operator's words

> the worker, at the moment it checks in (commit), must be consistent with the
> remote/main branch — that is the normal human flow: pull/rebase before you
> check in, and check in on top of the master you can see.

Measured against reality: delivered branches sat **46, 1359, and 5074 commits**
behind master while three of them were still non-ff-able.

## Why the existing sync did not deliver this

`finishCleanupLocalSync` (delivery-point sync, `3c396080`) already merges the
integration branch into the delivered branch, builds the merged state, and
refuses the release when either fails. It DID run — and it still left branches
behind, because **it is an end-of-task event, not a condition of the commit**.

## The root cause: the sync was placed BELOW guards that RETURN

This is the part that is easy to get wrong, so it is written down exactly.

`finishCleanupServerRecycle` (the server/local arm) read:

```go
wt := declared; if wt == "" { wt = linked }
rep.Worktree = wt
dirty, err := finishCleanupGit(ctx, wt, "status", "--porcelain")   // ← HERE
if err != nil { rep.Skipped = true; …; return rep, nil }
if reason := finishCleanupWorktreeDirtyReason(rep, dirty); reason != "" {
        rep.Skipped = true; rep.SkipReason = reason; return rep, nil   // ← AND HERE
}
…
sync := finishCleanupLocalSync(ctx, mainRepoDir, wt, branch)       // ← NEVER REACHED
```

Every guard between the head of the arm and the sync `return`s early with
`rep.Skipped = true`. **One untracked file in the worktree is enough** — the
ordinary litter of a finished run (logs, scratch files, `.evid/`) — and the
arm returns `Skipped` before the sync is ever reached. The report then reads as
routine hygiene ("untracked-only residue … the run must clean up after itself"),
with nothing on it saying that the branch was never aligned.

That is the **silent abstention** the acceptance criteria name. It is not a bug
in the sync; it is a bug in the sync's *placement* relative to guards that
answer a completely different question (may this directory be removed?).

## The fix: ask the alignment at the delivery point, above the removal's guards

`finishCleanupAlignDelivery` (`task_finish_deliverysync.go`) is the
precondition, and it is called by **both** arms at their delivery point:

| arm | where the gate is asked | why there |
|---|---|---|
| server / local (`finishCleanupServerRecycle`) | after ownership + liveness + ledger gates, **before** the removal-state question | the earliest point where the ground is proven to be THIS row's own and no other live row claims it (Rule 29), and the first point that is not pre-empted by a removal question |
| client (`…push` region) | before the pre-merge verification / push | the push is the delivery; the alignment must hold before the ref leaves the run |

The gate itself is **the existing lane, reused verbatim** — `finishCleanupLocalSync`:

1. resolve this repo's OWN integration ref (tip-most of `master`/`main`, no
   network);
2. `merge-base --is-ancestor <ref> HEAD` → already contained → **zero cost**
   (no merge, no commit, no build, no vet, no ledger write);
3. otherwise a real `git merge --no-edit <ref>` in the run's OWN worktree;
4. run the repo's **declared** build + vet on the merged state (Rule 12: no
   declaration → abstain, never invent a command);
5. conflict or failing build → **unwind** and refuse.

So the gate returns the lane's own verdict, and the caller decides:

- **VERIFIED** (`aligned` / `were-behind`) → step recorded, delivery proceeds;
- **REFUSED** (conflict, or the merged state does not build) → the step is
  recorded, the row's trail gets the refusal with the *same phrase* the release
  lane uses, and **the branch keeps its commits, its ground and its name**;
- **SKIPPED** (no ground on this branch · no integration ref · the gate switched
  off) → **recorded on the row** as an abstention, and the arm proceeds. An
  unmeasurable delivery is reported; it is never silently blessed and never
  silently blocked (Rule 15).

### Merge, not rebase — the mechanism, argued

A rebase rewrites the branch's own commits, which Rules 8/23 forbid, and would
force-push an already-published branch. A merge adds **one** commit on top and
leaves every existing commit's identity intact, so the branch is on top of the
integration branch and remains a fast-forward at the merger. The alignment
delivered is therefore a *fact about the ref* (`master` is an ancestor of it),
which is exactly the property the measurement (`git rev-list --count
branch..master`) reports.

## The reorder, and what it did NOT weaken

`finishCleanupServerRecycle`'s guards were reordered, never relaxed. The
removal-side question now has ONE helper (`finishCleanupRemovalStateReason`,
shared by the plan path and the applied path so the two cannot drift):

```go
removeStateWhy := finishCleanupRemovalStateReason(ctx, rep, wt)   // gone-dir + dirty
if removeStateWhy != "" { rep.Skipped = true; rep.SkipReason = removeStateWhy; return rep, nil }
```

- **Nothing is removed in any dirty state** — the untracked-only split keeps its
  own name (the shared reader `finishCleanupWorktreeDirtyReason`); the tracked
  case still skips.
- **Uncommitted work is never discarded.** The tracked-work case is now caught
  one step earlier, by the gate's own ground protection
  (`finishCleanupPreMergeUnwind` → `finishCleanupGroundHoldsWork`, the
  `dirty-ground-preserved` marker from `1732dc81`), which refuses to unwind a
  ground that HOLDS WORK. That path's unwind is also non-destructive by
  construction (`git reset --mixed`, never `--hard`). Verified by
  `TestAlignmentPrecedesTheDirtyGuardTrackedWork`: the author's uncommitted edit
  survives, and the removal is still refused.
- **The gone-directory diagnostic survives** in the same words, including the
  "not handed off to any server" note.

### One pre-existing test was updated, and why it is not a weakening

`TestServerRecycleDirtyWorktreeSkipped` asserted `len(rep.Steps) == 0` — an
*empty report* was the evidence that nothing ran. That can no longer hold: the
alignment step is now reported (it RAN, and that is the point of the reorder).
The assertion was retargeted at the act it was always about — the **removal** —
and still demands the refusal's own note be the last row:

```go
if len(rep.Steps) == 0 || !strings.Contains(rep.Steps[len(rep.Steps)-1].Detail, finishCleanupUntrackedResidueNote) {
```

which is strictly stronger than `len(rep.Steps) == 0` — a report that claims a
removal on a dirty tree now fails, where an empty one never could have said.

## Acceptance evidence

Rule 31: every claim below is an invocation and its observed output. Raw logs:
`/tmp/align-e2e.log` (real verbs), `/tmp/align-t4.log` (the test scope).

### (a) BEFORE — an unaligned branch driven through the real path

Real verb, real ledger row, real declared build, branch created 5 commits behind
master. Scenario world is a **clone** of the repo (Rule 29) under a scratch root
measured off `/` (Rule 22).

`git rev-list --count <branch>..master` = 6 before; the binary is the pre-change
one (`/tmp/gopi-align-old`, built from `2c14d74c`):

```
SCENARIO clean   binary=/tmp/gopi-align-old
git rev-list --count feat/deliverable-clean..master = 6
--- RUN: gopi task finish-cleanup --task e2e-clean --apply
finish-cleanup (APPLY): candidates=1 removed=1 settled=0 refused=0
--- AFTER
git rev-list --count feat/deliverable-clean..master = 6      ← still behind
worktree        = …/wt-clean (released)
```

**The defect, reproduced**: the branch was released and reported `removed=1`
while still 6 commits behind — nothing on the report said the alignment was
never measured.

### (b) AFTER — the same branch, aligned

Same world, same verb, binary `/tmp/gopi-align-new` (this branch):

```
SCENARIO clean   binary=/tmp/gopi-align-new
git rev-list --count feat/deliverable-clean..master = 6
--- RUN: gopi task finish-cleanup --task e2e-clean --apply
e2e-clean branch=feat/deliverable-clean done=[align-before-delivery,pre-merge,worktree]
finish-cleanup (APPLY): candidates=1 removed=1 settled=0 refused=0
--- AFTER
branch          = 5fade7c43823f370b20e3fb37703421402065292
git rev-list --count feat/deliverable-clean..master = 0
master is an ancestor of the branch: YES (aligned)
--- the row's own event trail
align-before-delivery|merged the integration branch locally and rebuilt — the branch is aligned with master and compiles
```

Count **6 → 0**, a new commit on the branch (`2271ceaa… → 5fade7c4…`), and the
trail names the mechanism. **The measured breaker itself** — the untracked file
that used to turn the whole close-out into a `Skipped` before the sync was
reached:

```
SCENARIO dirty   binary=/tmp/gopi-align-new
git rev-list --count feat/deliverable-dirty..master = 6
--- RUN: gopi task finish-cleanup --task e2e-dirty --apply
e2e-dirty skipped: worktree dirty (live bond protected) — carries ONLY UNTRACKED files (build output / scratch residue), a run's own hygiene, not uncommitted work: ?? scratch-residue.log
finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
--- AFTER
git rev-list --count feat/deliverable-dirty..master = 0        ← ALIGNED anyway
master is an ancestor of the branch: YES (aligned)
worktree        = …/wt-dirty (kept)
--- the row's own event trail
align-before-delivery|merged the integration branch locally and rebuilt — the branch is aligned with master and compiles
```

The removal is still refused (its own note, `refused=1`) — **the reorder
separated the two questions**: the branch is aligned *and* the dirty worktree is
still not recycled.

### (c) Conflict — the loud refusal

```
SCENARIO conflict   binary=/tmp/gopi-align-new
git rev-list --count feat/deliverable-conflict..master = 7
--- RUN: gopi task finish-cleanup --task e2e-conflict --apply
e2e-conflict skipped: branch does NOT merge with this repo's integration branch (conflict classes: content, modify/delete, rename, binary): Auto-merging conflict.txt merge unwound — the worktree is back at the pre-merge tip delivery-point sync refused — the worktree was NOT released; the branch keeps its check-out until it merges cleanly
finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
--- AFTER
branch          = 6ec545d86d59ee44f77cc1220f4ea4e6df84122e    ← unchanged
git rev-list --count feat/deliverable-conflict..master = 7
worktree        = …/wt-conflict (kept)
--- the row's own event trail
premerge-refused|branch does NOT merge with this repo's integration branch (conflict classes: content, modify/delete, rename, binary): Auto-merging conflict.txt merge unwound — the worktree is back at the pre-merge tip delivery-point sync refused — the worktree was NOT released; the branch keeps its check-out until it merges cleanly
```

Refused, named, recorded; the branch, its commits and its ground are all kept,
and the merge was unwound (no half-merged tree left behind).

### (d) No ground — the recorded abstention

```
SCENARIO noground   binary=/tmp/gopi-align-new
git rev-list --count feat/deliverable-noground..master = 6
worktree        = …/wt-noground (GONE)
--- RUN: gopi task finish-cleanup --task e2e-noground --apply
e2e-noground skipped: worktree unreadable: git rev-parse --verify HEAD: chdir …/wt-noground: no such file or directory
finish-cleanup (APPLY): candidates=1 removed=0 settled=1 refused=0
```

Honest gap, stated rather than papered over: the existing worktree-unreadable
guard in this arm returns before the gate is asked, so the abstention reaches
the operator as *that* guard's sentence rather than as
`finishCleanupAlignNoGroundMarker`. The distinct abstention text is proven where
it is owned — `TestAlignmentGateAbstainsWhenThereIsNoGround` calls the gate
directly and pins `Status == skipped` + the named detail (a merge ground must be
a directory ON the branch; borrowing the main checkout is forbidden, Rule 29).
Nothing is silently blessed in either shape: no alignment happened here, and the
report says the worktree could not be read.

## Files

- `internal/tools/task_finish_deliverysync.go` — `finishCleanupAlignDelivery`
  (the precondition), `finishCleanupAlignReason` (the trail record) and their
  constants.
- `internal/tools/task_finish_cleanup.go` — `finishCleanupRemovalStateReason`;
  the precondition wired into both arms; the removal-state question moved below
  it; the plan path predicts the same sequence.
- `internal/tools/task_finish_deliverysync_test.go`,
  `internal/tools/task_finish_cleanup_test.go` — see below.
