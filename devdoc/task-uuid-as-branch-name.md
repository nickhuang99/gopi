<!-- doc-class: normative -->
# The task uuid IS the branch name (and the worktree name)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a11554-b3b1-731e-8425-2ce4986786f9` — "why cannot we use task UUID as branch name?"

## The operator's rule, verbatim

> "in most cases except resume, the name of branch can use task uuid, so does the
> worktree, unless there exists any conflict, or special case of resume, gopi should
> take first choice to use this principle, however, if conflict, i.e. name exists,
> then fallback to create new uuid."

## Before: three surfaces blessed the opposite

The naming rule was spread over a code path and three pieces of prose, and they
disagreed with the operator's rule:

| surface | what it said |
|---|---|
| `internal/tools/task_run_ground.go` `taskGroundBranchFor` | a recorded `deliverable.branch` was **inherited verbatim** when minting a ground — so a descriptive name, once recorded, was reused forever |
| `taskGroundAnnouncementFmt` (the briefing block) | "If you want a descriptive branch name, RENAME this one — `git branch -m <new-name>`" |
| `internal/tools/task_ground_advisory.go` | the same blessing, worded for the no-ground case |
| `GOPI.md` Rule 6a | "The branch name is yours to change — rename it with `git branch -m <descriptive-name>`" |

The cost is exactly what the operator reported: **cross-comparing which task runs
under which branch/worktree took a ledger read per candidate**, because the ref no
longer carried the row's address. Measured specimen already in this repo's history:
`task/crelease` — a branch no ledger row resolves (`gopi task for-branch` answers
"no owner").

## After: one rule, three shapes

`taskGroundBranchFor(taskID, recorded, repo)` (internal/tools/task_run_ground.go) now:

1. **A minted ground always carries the row's own name** —
   `TaskGroundBranchPrefix + taskGroundIDStem(taskID)` = `task/<uuid-without-dashes>`.
   A recorded descriptive/foreign branch is **not inherited**; the row's own derived
   name and an exempt integration branch are.
2. **A taken name mints a fresh uuid** — `taskGroundBranchUnowned` is the ONE
   conflict resolver: the ref is free → take the row's name; the ref exists **and**
   the ledger says this row owns it → take it (a re-claim of its own deliverable);
   otherwise (owned by another row, owned by nobody in git terms — an orphan ref —
   or unprovable) → mint `task/<fresh-uuid-v7>` and **log the reason**. Logging is
   not decoration: an unattributable fresh name is the defect this rule exists to
   prevent, so it is never silent (Rule 15).
3. **A taken name is never ATTACHED to** — `git worktree add <dir> <existing-branch>`
   would put the run on somebody else's base, and finish-cleanup's push-back would
   then fast-forward that ref with this run's commits.

**Resume is untouched, and structurally so** — `TaskRunGroundFor` step 1 returns the
recorded ground (branch verbatim) *before* the mint path, so the "special case of
resume" costs no conditional in the new rule.

## Cross-comparison is now one read

`gopi task list` renders a live row's ground (new `internal/tools/tasklist_ground.go`):

```
#01a11554-b3b1-731e-8425-2ce4986786f9  [in_progress]  …  [via worker] [ground task/01a11554b3b1731e84252ce4986786f9 @ /home/user/workspace/gopi-…]
```

The tail is read off the row's **own** recorded deliverable through the existing
readers (`TaskDeliverableBranch`, `DeliverableWorktreePath`) — no re-derivation and no
extra query (`deliverable` already rides `taskSelectColumns`), so `task list` stays a
pure read of one row per line. The two axes are independent (a row with a branch but
no recorded worktree renders one half), and only `in_progress` rows carry a tail at
all, so a pending row holding a stale stamp says nothing.

The other direction already existed and now always answers: `gopi task for-branch
<branch>` reads a row back off the ref, and with the name being the uuid the two
lookups are inverses rather than guesses.

## What was NOT changed

- `TaskGroundBranchPrefix` (`task/`) is kept — the namespace is how the branch form
  is recognised at all (`internal/branchform`), and `master` stays exempt.
- The worktree **directory** already followed the uuid (`taskGroundDirFor`); only the
  *branch* had the escape hatch.
- Nothing was added to the ledger schema: the fix makes the recorded value agree with
  the rule instead of introducing a second field to reconcile.

## Measurements

Two e2e runs against a **sandbox profile** (`gopi tool sandbox create`) and a
throwaway real git repo, driving the production spawn seam
(`gopi worker <task-id>` → `runTaskRunOnce` → `tools.TaskRunGroundFor`):

1. an unclaimed row → its ground branch is `task/<row-uuid>` (no rename involved);
2. an **orphan ref** pre-created at exactly that name → the mint falls back to a
   **fresh** `task/<uuid>` and the orphan ref is left exactly where it was.

Both are recorded in the task report; the ref positions were read back with
`git rev-parse` before and after.

Re-measured a second time (this run) with the binary built **inside the run's own
evidence directory** rather than `/tmp` (Rule 30), a fresh throwaway repo and the same
sandbox profile — same two outcomes, and the exact numbers are in the task report:

```
# A: no collision
CREATED the ground …/gopi-01a1157b3a7371a3a7b021caf1ff05ad on branch task/01a1157b3a7371a3a7b021caf1ff05ad
# B: the ref task/01a1157bb88f7083b71fb00207cdd402 pre-planted at the row's own name
CREATED the ground …/gopi-01a1157bb88f7083b71fb00207cdd402 on branch task/01a1157bc8e47036b34e9b84669a72dd
$ git rev-parse refs/heads/task/01a1157bb88f7083b71fb00207cdd402   # before and after: identical
47e259416d32868b80e9de635151cc58e6871113
```

and the reverse lookup that makes the ground cross-comparable answers on the
terminal row too (`gopi task for-branch task/01a1157b3a7371a3a7b021caf1ff05ad` → the
one owner, its status, its session and its worktree).

## Why no new dependency (Rule 35)

No new file, daemon, config key or hook. The rule lives in the function that already
minted the ground; the announcement is the same string the runner already prints; the
`task list` tail is the same row data already read. The invariant is enforced by the
existing test files, re-pinned red→green.

## The suite run, attributed (2026-10-07, this run)

`make test-budget PKGS='./internal/tools ./cmd/agent'` came back `exit=1` (1222s; the
budget did not trip) with 15 FAILs. Two independent causes, measured:

1. **The harness's tested binary was stale because THIS run was editing the tree
   while the suite ran** — built `16:15:48`, `internal/tools/tasks.go` mtime
   `16:18:28`. Several tests detect precisely that and said so:
   `task_ground_announcement_e2e_test.go:69: … is OLDER than
   internal/tools/tasks.go … it would verify code that is not in that binary`.
   Re-run against a settled tree: `TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree`
   → PASS (`ok ./cmd/agent 10.138s`); `TestTaskSyncImportConcurrentSamePatch` → PASS;
   `go test ./internal/tools/ -run 'Ground|TaskListGround|Briefing|GroundAnnounce|UuidBranch|TaskList'`
   → `ok … 31.859s`.
2. **Pre-existing on master**, reproduced in a detached worktree at `1da7ab8ba`
   (`git worktree add --detach`, never the main checkout — Rule 29): the same test
   set fails there with the SAME names, and every file those tests read is
   byte-identical between master and this branch (`git show <rev>:<file> | sha256sum`).
   `git diff master..HEAD --name-only` (16 files) names none of them.

**Lesson worth carrying**: do not start the branch-scoped suite while still editing the
tree it builds from — the tested-binary freshness guard turns your own typing into
15 red tests, and reading that as a branch failure costs a triage cycle.
