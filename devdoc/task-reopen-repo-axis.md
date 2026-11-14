<!-- doc-class: historical -->
# The reissue row's repo axis — `gopi task reopen` mints a runnable successor

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: 01a15657-86a3-77d9-87b1-4835d07ea92c · Constraint set: Rules 1/13/14, 4/8, 5, 15, 30, 31, 32, 34.

## The defect

`tools.TaskVerifyToReopen` (`internal/tools/tasks.go`) minted `<id>-rN` through
the ONE create path (`insertTaskRow`) with `ParentTaskID`, `Tags`, `Payload`,
`Source` — and **no `Deliverable`**. A successor row therefore carried an empty
deliverable blob, so it had no `deliverable.repo`.

A row with no repo cannot resolve a **ground** (`taskClaimGround` /
`TaskDeliverableRepoFor` need one), so `gopi worker <new-id>` refused it with
"cannot resolve the ground". The refusal's own remedy is
`gopi task reopen <id> --reason …` — which minted another repo-less row. The
reissue inherited the **failure** and not the ability to run: a closed loop with
no exit.

## The fix (three arms, one decision function)

`taskReopenDeliverable(taskID, parentDeliverable, override)` returns
`(blob, lineFragment, error)` and is the single place the outcome is decided:

| arm | condition | deliverable written | line says |
|---|---|---|---|
| override | `--repo PATH` given | `{"repo": <durable main checkout>}` | `deliverable.repo=<path> (from --repo)` |
| inherit | parent blob has a repo | `{"repo": <parent repo>}` | `deliverable.repo=<path> (inherited from <id>)` |
| absent | neither | `""` (no blob at all) | `carries no deliverable.repo — …` + the remedy |

Three decisions worth recording:

1. **Never `worktree`/`branch`.** Those axes are RUN-allocated: the parent's
   ground is a directory that run owned, and the worktree axis is
   single-owner — a successor that claimed it would block its real owner's
   recycle (`reclaimSkipDirtyReason` family) and could only be cleaned up by
   the machinery that already refuses to touch a foreign ground. The successor's
   own run mints its own ground from the inherited repo.
2. **The override is resolved, not copied.** `--repo` goes through
   `DurableRepoForSubmit` — the SAME default the submit engine uses — so a
   `git worktree list` path lands on the repository instead of on another
   ephemeral linked worktree (that is the second half of the same defect class:
   memory record #7478). A path that is not a git work tree is REFUSED before
   the row is written, so a bad flag never leaves an unstartable successor.
3. **Absence is STATED, never silent.** The row is still minted (the rework is
   wanted whether or not a repo is known); the returned line says
   `carries no deliverable.repo`, why that matters, and the one remedy. Writing
   `{}` instead would be a blob that *says* it carries a deliverable while
   carrying nothing — Rule 15's defect in data form.

`TaskVerifyToReopen(ctx, taskID, reason)` is kept as a compat alias that
delegates to `TaskReopenWithOptions`; the CLI's one caller moved to the options
form (`runTaskReopen`).

## CLI

`gopi task reopen <id> --reason R [--repo PATH] [--db PATH]` — the flag is
admitted in `taskFlagMember`'s `Reopen` arm and read in the parse arm from the
SAME constant (`reopenRepoFlagName`, Rule 1b/13: the previous defect class in
this file was one site admitting a spelling another site read differently).
The usage page states the inherit/override/stated-absent contract.

## How it was proven

* **RED, observed** — a build-only overlay that restores the pre-fix decision
  (return the absent arm unconditionally) turns
  `TestTaskReopenInheritsParentRepo` / `…RepoOverrideWins` /
  `…OverrideRefusedWhenNotAGitTree` red:
  `successor deliverable.repo = "", want …`. The overlay was reverted and the
  restore verified in the same round (`grep -c "RED PROOF OVERLAY"` → 0, suite
  green again).
* **GREEN** — `internal/tools/task_reopen_repo_test.go` drives the real engine
  against a real ledger fixture (`testutil.NewTestDBPath`) with real `git init`
  trees under `testutil.TestDeviceScratchDir` (Rule 22/32). It pins: inherit,
  no-worktree/no-branch, override wins, linked-worktree override resolves to
  the main checkout, absent-is-stated (row minted, blob empty, marker present),
  and refusal-before-write.
* **Real surfaces** (Rule 31) — a `gopi tool sandbox` profile
  (`--sandbox-dir /home/user/workspace/_tmp/gopi-sandbox`, so the child-fail
  escape's own sandbox requirement holds): `task submit` a row with a repo,
  drive it `in_progress → failed`, `task reopen`, read `task show` back, then
  `gopi worker <id>-r1` — which claimed the row, resolved the ground and
  started it. The reissued row's post-run state is `status=failed,
  run_method=worker, claimed_by=…` — the deliberate
  `GOPI_TASK_CHILD_FAIL_TEST=1` synthetic failure, i.e. the child ran and was
  judged rather than the row being refused.

## Not Done

* `gopi worker` cannot be driven from inside an agent session without
  `GOPI_ALLOW_AGENT_SPAWN=1` (its own guard states why: a former resident
  worker inherited `GOPI_TASK_CHILD_FAIL_TEST=1` and burned 5 operator rows).
  The real-run evidence therefore carries that bypass flag in its invocation —
  it is stated in the report, not silent, and the run is confined to a
  throwaway sandbox ledger.
* The reissue still does NOT carry the parent's `attachments`/`grounds`. Both
  belong to the parent RUN, not to the rework; a successor that needs an
  attachment re-attaches it at submit. Recorded here so a later reader does not
  read the omission as an oversight.
