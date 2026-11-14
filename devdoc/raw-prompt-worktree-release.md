<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Raw-prompt runs left their worktree behind — root cause and fix

Epic: "gopi running with raw prompt". Branch `fix/raw-prompt-worktree-release`
(base `44928156`). Date 2026-09-14.

## The report

A gopi run started from a **raw prompt** (no `gopi task submit`, so the harness
auto-registers the run's own ledger row) commits its code into a branch inside
the Rule-6 linked worktree and then exits — **without removing the worktree**.
The same was reported for `gopi worker`; `gopi task run` was said to clean up
correctly. A worktree left registered makes the merger read the branch as
worktree-protected ACTIVE BOND WORK, so the finished deliverable is never
merged — silently.

## Root cause (measured, not inferred)

**An auto-registered row was born with an EMPTY deliverable, and every arm of
the worktree lifecycle is deliverable-keyed.** Three links, in order:

1. **No repo on the row.** `internal/agent/taskregister.go`'s start hook
   (`maybeRegisterSessionTask`) called `tools.TaskNewOID` with a zero-valued
   `Deliverable`. A `task run`/worker run never hits this — its WRAPPER row is
   born at submit with `deliverable.repo`.
2. **So the creation-time backfill refused it.**
   `taskBackfillDeliverableWorktree` returns early on a blank blob ("no
   deliverable blob (auto mirror rows)"), and the seam that calls it
   (`TaskDeliverableWorktreeBash`, wired in `internal/agent/loop.go` after every
   successful bash round) resolved its row from `GOPI_TASK_WRAPPER_ID` alone —
   an env only the `task run` wrapper exports. A bare-prompt run has no wrapper
   row and no wrapper env, so the Rule-6 worktree the executor created with
   `git worktree add` was recorded **nowhere**.
3. **So nothing ever released it.** The reclaim arms select rows by
   `deliverable LIKE '%worktree%'` (`ReclaimOwnFinishedWorktrees`) or by a
   parsed `deliverable.worktree` (the finish-cleanup arms). An empty blob
   matches neither, so the directory stayed registered,
   `repoSweepProtectedBranches` kept reading the branch as protected, and the
   merger never merged it.

`gopi task run` needing no new work is consistent with this: its wrapper row
already carried a repo, and its close-out already runs both the finish-cleanup
chain and the producer-side release. `gopi worker` is a different story that
this branch does not claim to have fixed — see *Not Done*.

### The RED repro (committed)

`cmd/agent/rawprompt_worktree_release_e2e_test.go` drives the REAL binary with a
scripted mock LLM: a raw prompt, a throwaway git repo, and a scripted executor
that does exactly what a real one does — `git worktree add <wt> -b <branch>
master`, then commit in it. Measured on the unfixed tree:

```
the raw-prompt run must RELEASE the worktree it created — the directory is still registered:
  .../003/repo  d3793e3 [master]
  .../003/wt    2d126e3 [feat/rawprompt-release-e2e]
the worktree directory .../003/wt still exists after the run exited
auto row 01a0a20c-… deliverable = (worktree "", branch ""), want (.../003/wt, feat/rawprompt-release-e2e)
```

## The fix

Three surgical changes, each at the point where the information was lost:

1. **`internal/tools/tasks.go` — `TaskAutoDeliverableJSON()`.** The live-blob
   renderer an auto-registered run row is born with: the process's own git work
   tree, resolved by the SAME default the submit engine uses
   (`taskSubmitResolveDefaultRepo`) and rendered by the SAME renderer
   (`taskDeliverableSubmitJSON`). "" when the process stands in no work tree —
   the row then honestly carries no deliverable. One default, one renderer
   (Rules 13/14).
2. **`internal/agent/taskregister.go`** — the start hook now stamps that
   deliverable, so the auto row's blob is non-empty exactly when it has a
   truthful repo to carry.
3. **The backfill seam gained a caller-named row.**
   `TaskDeliverableWorktreeBashForRow(taskID, workdir, command)` (and
   `TaskDeliverableWorktreeForRow` behind it) take the row the CALLER owns;
   `TaskWorktreeBackfillRow` makes the caller's answer win and keeps
   `GOPI_TASK_WRAPPER_ID` as the fallback, so both faces behave as before.
   `internal/agent/loop.go` passes `a.autoTaskRowID` — the row this run
   registered for a bare prompt, or the wrapper row a `task run` child already
   latched onto it (task-single-row-exec), i.e. the same value the liveness
   heartbeat uses.

**No cwd-release code was added, and none was needed.** The executor child's
close-out already releases its cwd (`worktreerelease.ReleaseReporting`, wired by
the predecessor epic `fix/cwd-release-coverage`, merged `0477bd3a`), and the
finish-cleanup chain is what tests whether a live cwd still pins the directory.
Once the row names its worktree, that machinery does its job: measured after the
fix —

```
[task-notify] task 01a0a210-…: deliverable.worktree backfilled to "/…/wt" (branch "feat/probe") at creation
[gopi] task ledger: completed 01a0a210-… (auto)
[gopi] finish-cleanup: 01a0a210-… branch=feat/probe done=[worktree]
```

— and the branch survives as the merger's input, which is the whole acceptance.

## Verification

- RED→GREEN e2e on the real binary:
  `env -u GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID -u GOPI_TASK_RUN_ID go test -count=1 -run TestRawPromptRunReleasesItsWorktree ./cmd/agent/`
  (RED on the unfixed tree: 3 assertion failures; GREEN after the fix).
- Realistic-fixture walk (`~/workspace` worktree, the Rule-6 shape): the run
  log shows the creation-time backfill, the terminal close-out, and
  `done=[worktree]` with `git worktree list` naming only the main checkout.
- Minimal repros with teeth:
  `go test -run 'TestTaskDeliverableWorktreeBashForRow' ./internal/tools/` —
  GREEN on the fix, and proven RED by re-inlining the pre-fix env-only
  resolution (`taskID = os.Getenv(TaskWrapperEnv)`), which fails
  `…NeedsNoWrapperEnv` with `worktree="" branch=""`.
- Full suites: see the commit message / task report for
  `./internal/tools/ ./internal/agent/ ./internal/standards/ ./cmd/agent/` results.

## Not Done (honest)

- **`gopi worker` was not independently re-measured.** The briefing claims the
  worker face leaks too. The worker path funnels through
  `runTaskRunOnce` + `workerRoundOwnWorktrees` and its rows carry a deliverable
  (they are wrapper rows), so the *mechanism* fixed here does not obviously
  apply to it; its own release arm already exists
  (`internal/tools/task_reclaim_self.go`, branch `fix/worker-self-release`). A
  worker-path END-TO-END walk (submit → worker --once → worktree gone → merger
  merges) was NOT run on this branch — the named gap, dispatchable as its own
  task. If the worker really leaks, the cause is elsewhere (most likely the
  claim-pid/terminal-status window those arms key on), not this row-blob defect.
- **The bare-prompt face does not run the producer-side release pass.**
  `ReclaimOwnFinishedWorktreesWithin()` is called by the `task run` wrapper and
  the worker round, keyed on `claimed_by = host:pid`; a bare-prompt run stamps
  its own host:pid via `TaskStampSelfClaim`, but nothing calls the pass on that
  face. It did not matter here because the run only works in the MAIN checkout
  and the worktree it creates is released by the finish-cleanup chain (proven
  above). It matters for the shape "a bare-prompt run is launched FROM a task's
  worktree and must free it" — worth its own task.
- **The `task run` face was not re-verified end-to-end on this branch** (its
  close-out was untouched; only the seam's row argument changed for it, and the
  existing `worktree_release_accept_test.go` / `cli_tasks_recycle_e2e_test.go`
  pair covers it — they are part of the suite run reported above).

## Deliverable

Branch `fix/raw-prompt-worktree-release` (in the main repo's shared object
store: this work happened in the linked worktree
`~/workspace/gopi-rawprompt-release`). Recover with
`git show fix/raw-prompt-worktree-release:devdoc/raw-prompt-worktree-release.md`.
