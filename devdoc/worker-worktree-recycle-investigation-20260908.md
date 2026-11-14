<!-- doc-class: historical -->
# Investigation: worker leaves worktree of completed branch (epic t-1788815283)

Date: 2026-09-08 · Investigator verdict report (no code change needed from this
lane — diagnostic + regression confirmation)

## Symptom (the operator's finding)

Two worker-run tasks delivered on peer1 and left their worktrees behind:

```
+ feat/cli-param-review      /home/user/workspace/gopi-cli-param-review
+ feat/prompt-review         /home/user/workspace/gopi-prompt-review
```

Both rows terminal `completed`, yet the branches stayed worktree-PROTECTED —
under the merger's trigger ("no worktree protection = merge candidate") a
parked worktree makes the branch look still under development.

## Root cause (verified end to end)

The machinery for server-side worker recycle is present and correct; the two
rows never reached it because their **deliverable JSON carried no branch**:

```
sqlite3 ~/.gopi/memory.db  (tasks table)
t-1788812762|completed|{"repo":"/home/user/work/GoPi"}
t-1788813641|completed|{"repo":"/home/user/work/GoPi"}
```

The server-mode recycle arm (`finishCleanupServerRecycle`,
internal/tools/task_finish_cleanup.go) resolves the branch from the
deliverable's `branch` field, falling back to the repo's checked-out HEAD.
With no backfilled branch the fallback reads the **main checkout = master →
exempt → skip**. No branch → no worktree resolution → nothing recycled.

Why the backfill was empty: the child ran its `git worktree add` inside bash;
the branch/worktree backfill seam keys off the child session's recorded
launch workdir, which for these runs was the main repo (master) — so the
writeback exempted master and left the row branchless. **The sibling worker
task t-1788815951 (live, 2026-09-08 05:19) is fixing exactly this backfill**
— it owns the code fix + regression. This report does not duplicate it; it
confirms the surrounding machinery and the worker-path sharing question the operator
raised.

## Code-path sharing: worker and manual `task run` already funnel together

Nick asked: "this is behavior when task is run by `task run <taskid>`, worker
should follow exact style — the code path should share instead of separate."

Confirmed **already shared** — one funnel, no fork:

- `workerRound` (cmd/agent/cli_tasks_worker.go:214) → `runTaskRunOnce`
  (cli_tasks_worker.go:250) → … → delivery-time finish-cleanup hook
  (cmd/agent/cli_tasks.go:1237) → `tools.TaskFinishCleanupSessionID`.
- Manual `gopi task run <id>` → same `runTaskRunOnce` → same hook.
- The child's own close-out chain (internal/agent/taskregister.go:273,
  `finishSessionTaskRecorded`) runs on the child's AUTO row, which carries no
  deliverable — so the WRAPPER row's delivery-time hook is the one that
  releases the worktree, for both entry styles.

Mode split inside the shared chain: client (declared sync peer) pushes →
worktree remove → branch -d; server/single-node removes the clean linked
worktree on the branch and PRESERVES the branch (the merger's input). peer1
shape = no role field + no sync_peers → server arm fires.

## Regression coverage: worker recycle E2E already exists and is green

`cmd/agent/cli_tasks_worker_recycle_e2e_test.go` (commit 9edb8e5, epic
t-1788389175) drives the REAL binary through the FULL worker chain — submit →

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

`gopi worker --once` from a linked worktree → real child + mock LLM →
terminal writeback → delivery-time recycle — then asserts the worktree is
gone, the branch survives, and a subsequent `gopi task merger --once` merges
the now-unprotected branch (no starvation). `TestWorkerServerDeliveryRecycles
ThenMergerMerges` + `TestWorkerServerDeliveryDirtyWorktreeKeepsBranch`.
Manual-path twins: `TestManualRunRecyclesItsWorktree`,
`TestManualRunMainCheckoutSkipsRecycle`.

Fresh verification on current master:

```
go test ./cmd/agent/ -run 'TestWorkerServerDelivery|TestManualRunRecycles|TestManualRunMain' -count=1
ok  github.com/the operator/gopi/cmd/agent  7.269s
```

## Verdict

- **No worker-path code gap**: worker and manual runs already share
  `runTaskRunOnce` → delivery-time `TaskFinishCleanupSessionID`; server-arm
  recycle + branch preservation are implemented and E2E-tested for the worker
  path.
- The observed leftovers are the **backfill data gap** (branchless
  deliverable → master-exempt skip), owned by sibling t-1788815951.
- Cleanup of the two parked worktrees (feat/cli-param-review,
  feat/prompt-review) is deliberately deferred: they are the sibling's live
  repro + the operator's evidence. After the sibling's fix lands, the PA/merger flow
  (or a manual recycle) removes them.

## Verification
- Build: not run (docs-only report; no code touched this lane).
- Tests: `go test ./cmd/agent/ -run 'TestWorkerServerDelivery|TestManualRunRecycles|TestManualRunMain'` OK (7.269s).
- Commit: see git log on master (docs genre).
