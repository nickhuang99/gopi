<!-- doc-class: normative -->
# `gopi worker --resume <task-id>` — the row is the default selector

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## The shape before this change

`gopi worker --resume` (commit `662b16a3`, epic "the worker owns the ground")
exists to be the ONE face that can remove a finished row's worktree: it resolves
an EXISTING ground, runs the ordinary `gopi resume <task-id>` in a CHILD (so its
own cwd never enters the directory), and then runs the standard close-out chain
from outside. `gopi resume` chdir's INTO the row's worktree by design, so its own
chain correctly refuses to remove the checkout it stands in
(`internal/tools/guest_row_guard.go`); the worker is the actor that is not
standing there.

But the mode's SELECTORS were `--branch <name>` and `--worktree <path>`, and its
resolver derived the ROW from the ground. An operator who knows the row — the
common case, and the one every other face of this system speaks — had to go ask
git for a branch name or a path that the ledger *already recorded on the row*.

## The change

`gopi worker --resume <task-id>` is now the default spelling. It reuses the
positional the verb already takes (`gopi worker <task-id>` runs one row, epic
worker-positional-taskid): the mode switch is the only thing that changes what
the argument means, which is exactly the shortcut `--branch`/`--worktree` are —
derivable names for ground the row already knows.

```
gopi worker --resume <task-id>      the row is named; its recorded ground is resolved
gopi worker --resume --branch B     the branch is named; the row is derived from it
gopi worker --resume --worktree P   the path is named; the row is derived from it
```

Three spellings, ONE address: all three end in the same `tools.TaskResumeGround`
(`Dir`, `Branch`, `Repo`, `TaskID`), so the caller has one thing to run and the
guards have one thing to protect. Nothing about the resume itself, the child
spawn, or the close-out chain changed — the row spelling only changes WHICH
question is asked, never what happens next.

### Resolution (`tools.TaskResumeGroundForTask`)

Resolve-only, like its two siblings:

1. the row must EXIST (`TaskStatusOf` — an absent row and a row with an empty
   deliverable must never render the same way, Rule 15);
2. `TaskDeliverableWorktreeFor` / `TaskDeliverableRepoFor` read what the run
   itself recorded: `deliverable.worktree`, `deliverable.branch`,
   `deliverable.repo`;
3. the recorded worktree must still be a LIVE REGISTERED linked worktree of that
   repo (`taskRepoLinkedWorktrees` + `taskResumeGroundLinkedEntry`, matched by
   directory identity, not string equality); a row whose ground is gone is
   refused, never re-created;
4. a row that recorded a BRANCH and no worktree (the ordinary in-flight shape —
   the path is backfilled by the first git-touching round) resolves through the
   same reader the `--branch` spelling uses (`taskGroundResolveList`);
5. a row that recorded a WORKTREE and no branch takes the registration's own
   branch, so the answer never comes back with an empty branch axis.

Refusals, each with its own repair: empty id / unknown row / the row names no
ground / the recorded ground is no longer live.

### Grammar

* `--resume <task-id>` — accepted; the positional feeds the resolver.
* `--resume` alone — the refusal now teaches the TASK ID first (it was the
  spelling that did not exist yet), then the two ground flags.
* `--resume <task-id> --branch B` — refused in ONE sentence with two readers
  (`workerResumeMixedSpellingsRefusal`): the scanner's contradiction arm (before
  anything runs) and the resolution itself (so a resolution never silently picks
  between the row's recorded ground and a hand-named one).
* `--branch`/`--worktree` without `--resume` — unchanged refusal.

## Why the selector kind is exported

`TaskResumeGroundTaskKind` / `BranchKind` / `WorktreeKind` are exported beside
the resolver (Rules 13/14): the CLI's refusals, the e2e assertions and the
resolver's own detail line must agree about which spelling answered, and a
re-typed `"task"` in a third place is exactly the drift Rule 1b/13 forbids. The
answer line is the resolver's existing format —
`resume ground: dir=… branch=… repo=… row=… (task) — resolved by task: …` — which
already carries the selector in parentheses; the constant is what lets the test
assert it without hard-coding the word.

## What was NOT touched

* The resume path (`cmd/agent/cli_resume.go`) and the close-out chain
  (`tools.TaskFinishCleanupSessionID`) — one implementation, unchanged.
* The resolve-only contract: no spelling creates a worktree, and the negative
  tests assert that at git and at the ledger rather than trusting the message.
* The guards: a dirty worktree and an unfinished row are still refused by the
  chain with their reason (`TestWorkerResumeKeepsTheGuardsIntact`, unchanged).

## Tests

* `internal/tools/task_resume_ground_test.go` — four new tests: the three
  spellings answer ONE ground; every refusal names its repair and creates
  nothing (asserted at `git worktree list` and the filesystem); the branch-only
  row resolves; the worktree-only row takes the registration's branch.
* `cmd/agent/cli_worker_resume_grammar_test.go` — the positional is accepted as
  the default spelling (before and after the mode switch), and the row-beside-a-
  flag shape is refused.
* `cmd/agent/cli_worker_resume_e2e_test.go` — two REAL-BINARY tests (Rule 31):
  `--resume <task-id>` removes the worktree, keeps the branch tip, and prints the
  ground; and the four refusal shapes exit non-zero, name the repair, and leave
  `git worktree list`/`git branch`/the ledger byte-identical.
