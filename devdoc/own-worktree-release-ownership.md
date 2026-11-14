<!-- doc-class: normative -->
# own-worktree-release: whose registration is it?

The close-out release pass had every guard except the question of OWNERSHIP, and
that missing question put another task's dirty files on an unrelated row's
account.

## The measured defect

Row `01a17dcd-c59c-7344-a99a-83133d4416c7` finished its work, committed it and
released its own ground. Its close-out then printed:

```
task 01a17dcd-c59c-7344-a99a-83133d4416c7: own-worktree-release: own registered worktree kept: /home/user/workspace/cptest-verify — worktree is DIRTY (uncommitted work) — left for a human
```

`/home/user/workspace/cptest-verify` was a chat session's hand-made verification
checkout. It had never been created by, nor registered for, `01a17dcd`; the seven
uncommitted files in it belonged to a DIFFERENT task whose work had already
landed. The fact ("a directory is dirty, a human should look") was true. The
SUBJECT (`task <id>:`, i.e. this row's state) was false, and the operator read the
line as a contradiction: the row completed, yet its worktree was said to be dirty.

## Why the guard ladder could not catch it

`ReclaimSelfOwnRegisteredWorktrees` enumerates every repo the ledger names and
then EVERY registration of each, and each registration is judged only by "may
this be deleted?" — `Main`/`Bare`/`Prunable`/`Locked`, the age floor, the
live-holder scan, the dirty rung, the ground checklist. All of those are questions
about a DIRECTORY. None of them is a question about a ROW. So any registration
nobody owns — a hand-made checkout, another tool's scratch tree, a leftover from
a previous session — belongs to whichever row happens to be closing out when the
walk reaches it.

The function was named `...SelfOwn...`, and `own` had no referent.

## The fix: one resolver, two report subjects

`internal/tools/own_worktree_ownership.go` owns the question. `GroundOwnership`
answers it from two tests, in this order:

1. **the recorded directory** — the row's own `deliverable.worktree` names this
   path. Model-independent, so it holds for a clone-channel row too.
2. **the row's ground branch, on a LINKED worktree only** — the branch the row
   records, or the branch DERIVED from the row's id (`TaskGroundBranchName`).
   This is the launch-directory shape `reclaim_own_ground.go` documents: the
   worker creates its own ground and the row records that one, while the run was
   launched in a linked worktree of the same row's branch.

The shape half is `devworkspace.GroundModelOf` — the same single discriminator
PROJECT.md Rule 6c names for "what kind of ground is this". That is not
decoration: the two models own their commits in OPPOSITE ways (a linked
worktree's commit is already on its branch in the shared repository; a clone's is
owed by the clone channel's delivery loop), so the branch axis must not claim a
CLONE. Every clone of that channel stands on `task/<id>` by construction; if the
branch axis claimed it, the pass would announce ownership of a directory whose
delivery is somebody else's job.

The verdict decides the REPORT, never the removal: every guard rung keeps
deciding what may be deleted, so this change cannot free a directory the previous
tree kept. A registration that is not a row's ground is printed with the neutral
lead `unowned (not this close-out's ground): ` and — at the seam that renders a
row id (`cmd/agent/cli_tasks.go`, via `taskRunPrintOwnReleaseLine`) — without the
`task <id>:` prefix. The worker seam
(`cmd/agent/cli_tasks_worker.go:153`, `workerRoundOwnReleaseFmt`) never printed a
row id at all, so it was never the defect site; it keeps its own prefix and the
neutral lead travels inside the line it renders. The observation survives in full (Rule 15); the claim on a
row's account does not.

## What a future fix must not undo

- **The neutral lead is the pass's, not the seam's.** `tools.ReclaimSelfOwnIsOwnLine`
  is the one classification; a seam that re-derives ownership is the drift this
  change removes.
- **A CLONE is claimed by its recorded directory only.** Widening the branch axis
  to clones is what would make the pass report on a channel's grounds.
- **The dirty rung still reports.** Silencing an unowned dirty directory instead
  of re-subjecting it would trade one Rule 15 violation for another.
