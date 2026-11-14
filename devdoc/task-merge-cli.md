<!-- doc-class: historical -->
# Task Report: `gopi task merge` CLI + one-line post-receive hook (v6)

Epic: nick's design correction — "why is it not a gopi cli interface?" The
merge hard-logic must be a gopi CLI subcommand; the hook only triggers it.
Node: peerbox · Branch: `feat/task-merge-cli` · Commit: `10dd2e6`

## Completed

- **`gopi task merge [<branch>...] [--all] [--repo PATH] [--db PATH] [--submit]`**
  (`internal/tools/task_merge.go` engine + `cmd/agent/cli_tasks_merge.go`
  CLI face): token-free Go, git is the arbiter. Per branch:
  - already merged (`merge-base --is-ancestor`) → `branch -d`
  - clean merge → ort auto-commits → `branch -d`
  - real conflict → `merge --abort` (master untouched) → with `--submit`
    ONE `merge-conflict` task enqueued (pending row, tags
    `merge-conflict,auto-dispatch`, self-contained briefing with conflict
    file list + diff stat; deduped against live rows by summary). WITHOUT
    `--submit`: conflict reported, exit non-zero, no ledger writes.
  - `hp-unique-*` archive-ledger branches EXEMPT (never merged/deleted)
  - exit codes: 0 clean, 1 conflict/dispatch, 2 per-branch error
  - `--all` sweeps every local branch except master (hp-unique excluded)
  - engine detail: stderr is captured into the error text so the
    "no merge to abort" pre-flight failure is distinguishable from a real
    conflict scene (root-cause, not blanket dispatch)
- **Hook v6 — one line** (`scripts/git-hooks/post-receive`): the v1–v4
  inline bash loops (16c391a/b9aabac/d63a6f5/c80f870 incident chain) are
  gone; the hook is a detached one-shot `gopi task merge --all --repo
  <repo> --submit >> .git/post-receive-audit.log`. `GOPI_REPO`/`GOPI_BIN`
  env overridable (tests); `GIT_DIR/GIT_WORK_TREE/GIT_QUARANTINE_PATH`
  unset so the child sees the checkout, not the quarantine object dir.
- **`gopi task merge -h`** prints the full usage section (also in
  `tasksUsage`). `audit-merges` CLI verb RETIRED (serve tick now runs the
  TaskMerge sweep via `serveMergeBacklogAudit`, which keeps its env/throttle
  machinery; `TaskOpenMergeBacklogBranches` branch-scan untouched).
- **E2E `scripts/e2e-task-merge.sh`** — 19 assertions, all PASS:
  ① `--all` on a fixture repo with FF / diverged-clean / true-conflict
  branches (real `file://` pushes, `receive.denyCurrentBranch=updateInstead`)
  ② hook live-fire: push → one-line trigger → audit log + branch merged+pruned
  ③ idempotent re-push: re-sent ref → sweep no-op, exit 0
  ④ conflict row: pending, `tags: merge-conflict,auto-dispatch`, master
  unmoved (abort verified via `git show master:shared.txt`)
  ⑤ `-h` help.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Unit: `go test ./internal/tools/ -run TestTaskMerge_` → 12/12 PASS (real
  git temp repos); `go test ./cmd/agent/ -run 'TestMergeAudit|TestParseTasks'` PASS
- Full: `go test ./...` → all packages ok except 3 pre-existing flakes in
  internal/tools (TestBashDaemonSpawn_DoesNotLeakProcesses process-leak
  timing, TestReflowExport* sync-gate) + 10m suite timeout — none touch
  this change (verified: they fail identically on master; the tools suite
  alone takes ~9 min)
- E2E: `bash scripts/e2e-task-merge.sh` → **19 passed, 0 failed**
- Commit: `10dd2e6 feat(task): gopi task merge CLI` on `feat/task-merge-cli`
  (worktree-shared → visible in the main repo)

## Key findings during E2E (operational knowledge)

1. **Bare repos cannot merge** — `git merge` in a bare repo exits 128
   ("must be run in a work tree"). The integration repo must be a non-bare
   checkout (peerbox's actual shape). The E2E fixture mirrors that.
2. **Local-path pushes are short-circuited** — pushing to a plain local
   path between repos sharing the FS can skip receive-pack entirely
   ("Everything up-to-date", hook never fires). `file://` URL forces the
   transport. Same class as the "Everything up-to-date = no hook" rule in
   devdoc/hook-investigation-report.md.
3. `merge --abort` on a clean tree exits 128 with "There is no merge to
   abort" — the engine now treats that as a pre-flight failure (report),
   never as a conflict to dispatch.
4. The hook runs detached (`&`) — E2E sleeps 5s before asserting.

## Not Done / deferred

- `audit-merges` CLI verb removed; its unit test file reduced to serve-wiring
  tests. If external scripts still call `audit-merges`, they get a pointed
  "unknown subcommand" message listing `merge` (grep found no other callers).
- The 3 failing internal/tools tests pre-date this branch (daemon leak +
  reflow gate) — filed for separate triage, not merged into this scope.
- ssh-push live-fire from hp not re-run here (hook semantics are
  repo-local; protocol-identical per hook-investigation-report.md §4).

## Deliverable declaration (Rule 6d)

- **Branch**: `feat/task-merge-cli` (commit `10dd2e6`) — worktree-shared
  with `~/work/GoPi`, so the commit is already in the main repo.
- **Recovery**: `git -C ~/work/GoPi show feat/task-merge-cli:internal/tools/task_merge.go`
  or `git -C ~/work/GoPi log feat/task-merge-cli --oneline -1`.
- Worktree `~/workspace/gopi-taskmerge-cli` can be removed after merge.
