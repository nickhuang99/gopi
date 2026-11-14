<!-- doc-class: historical -->
# devdoc: the report-seam fixture now owns its ground (hermeticity fix)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a0b9a3` (epic "report seam test is not hermetic"), branch
`fix/report-seam-hermetic`, 2026-09-19.

## Symptom

`cmd/agent/cli_tasks_report_seam_test.go:TestTaskRunSeamWritesReportAndShowRendersIt`
failed in full-package runs while passing when run alone. The failure always wore
the same two faces, in the same order:

```
task show result digest does not record the report line
result:  run t-report-seam left UNCOMMITTED TRACKED WORK in its OWN worktree
         /home/user/workspace/gopi-cmdagent-reds: … entries: M tasks/…
         the completed writeback is REFUSED, so the row stays open
```

## Mechanism (which code resolved the foreign worktree)

1. The fixture submits its row with `--repo <filepath.Abs("../..")>` — the repo
   **under test** (`reportSeamRepoRoot`). `task submit` stamps that path as the
   row's `deliverable.repo` (`internal/tools/tasks.go:taskDeliverableRepoJSON`;
   probe: `{"repo":"/home/user/work/GoPi"}`).
2. `gopi worker t-report-seam` spawns the executor child whose cwd is the TASK's
   ground, not the wrapper's: `cmd/agent/task_session_cwd.go:taskExecutorCwd` →
   `tools.TaskRepoForLaunch` → `tools.TaskRepoFor`
   (`internal/tools/tasks.go:3797`). `TaskRepoFor` returns the row's
   `deliverable.worktree`, else its `deliverable.repo`.
3. The child's bash round then drives the worktree-backfill seam
   (`internal/agent/loop.go:5571` → `tools.TaskDeliverableWorktreeBashForRow`,
   `internal/tools/task_deliverable_worktree.go`): it runs
   `git -C <repo> worktree list --porcelain` (`taskWorktreeOwningDir`) and, when
   the directory the child stands in lies inside one of the repo's **linked
   worktrees**, backfills `deliverable.worktree` + `deliverable.branch` from it.
4. Because the repo under test is a repo **with other sessions' worktrees**
   registered, the row was re-pointed at whichever one encloses the executing
   session's directory. Measured on this host — the row resolved to
   `/home/user/workspace/gopi-cmdagent-reds` (branch `fix/cmd-agent-master-reds`)
   and to `/home/user/workspace/gopi-seam-hermetic` (branch
   `fix/report-seam-hermetic`), depending on who ran the suite.
5. The run's close-out gate then measured **that** tree:
   `ReclaimRunResidueCheck` (`internal/tools/task_reclaim.go:1609`) →
   `reclaimRunCodeWorktree` (the row's worktree, admissible only when git agrees
   it is a linked worktree of the row's repo on the row's branch) →
   `reclaimRunTrackedDirtRefusalNote`. A non-empty `git status --porcelain`
   there **refuses the `completed` writeback** (Rule 30's binding gate,
   default-on), so `result` never carries `taskreport.DigestLinePrefix` and the
   seam test fails for a reason unrelated to the branch under test.

So the defect is the *fixture's* ground: it named a shared, multi-tenant tree,
and two independent seams (`taskExecutorCwd`, the worktree backfill) faithfully
resolved it to the executing session's own worktree.

## Fix

`cmd/agent/cli_tasks_report_seam_test.go`:

- `reportSeamRepoRoot` is **deleted** (with its comment, which had been spliced
  above `modeOf`).
- `reportSeamFixtureRepo(t)` creates the fixture's own ground: a `t.TempDir()`
  (under the suite scratch root) `git init`-ed, one committed file, asserted
  clean at creation. The seam coverage is untouched: the row is still submitted
  into a **real git work tree**, a real `gopi worker` still executes it, and the
  report still travels the harness path end to end.
- Both seam tests (`…WritesReportAndShowRendersIt`, `…FallsBackToSkeleton`) take
  their `--repo` from the fixture.

## Guard

`reportSeamGroundVerdict(show, repo, scratchRoot)` — a pure predicate over
`gopi task show`'s output — plus `assertReportSeamDeliverableIsTheFixturesOwnGround`
(live, inside the seam test) and `TestReportSeamGroundGuardRejectsForeignGround`
(unit, red for the reverted shape, no seam run needed).

Two arms, and the first is the one that matters:

1. **scratch-root arm** — the resolved `deliverable.repo` must lie inside
   `testutil.SuiteTempRoot()`. This is the arm that catches the REVERSION:
   comparing the resolved ground against the `repo` *argument* alone cannot,
   because a reverted fixture passes the tree under test as that argument too.
2. **repo arm + worktree arm** — the ground must be THIS fixture's directory, and
   any `deliverable.worktree` must lie inside it.

For (1) the labels are read from new exported constants in the renderer's own
package (Rule 14): `tools.TaskShowDeliverableRepoLabel` /
`tools.TaskShowDeliverableWorktreeLabel`, referenced by the renderer in
`internal/tools/tasks.go` and by the guard — no re-typed copy that a renderer
change would silently orphan.

## Evidence

| claim | command | output |
|---|---|---|
| pre-fix live red | `cd <worktree> && printf '\n# probe\n' >> GOPI.md && go test ./cmd/agent/ -run TestTaskRunSeamWritesReportAndShowRendersIt` (baseline `90e664e0`) | `FAIL … 7.73s`; digest assertion + `left UNCOMMITTED TRACKED WORK in its OWN worktree /home/user/workspace/gopi-seam-hermetic … REFUSED` |
| post-fix green, foreign tree DIRTY | `git -C /home/user/workspace/gopi-seam-red status --porcelain` → `M GOPI.md`, then the same test with this worktree's `GOPI.md` also dirty | `PASS` (`TestTaskRunSeamWritesReportAndShowRendersIt 6.84s`, `…FallsBackToSkeleton 2.72s`) |
| guard red on the reverted fixture (clean tree) | see `tasks/01a0b9a3-report.md` | guard message, not a digest mismatch |

Full logs live under `/home/user/workspace/_tmp/seamprobe/`.

## Not done

- The executor child still stands in the fixture's temp repo and loads the rules
  file from there; a mock-backed run does not exercise that, and the fixture
  deliberately holds no Go. This is unchanged from before the fix (the old
  fixture's child stood in the tree under test).
- `cmd/agent/report_contract_nested_test.go` shares `reportSeamBinary` (the
  built binary + `GOPI_BIN` stamp) but never submits a task row and so never had
  a repo/worktree; it is outside this epic's scope, and was left as it is.
