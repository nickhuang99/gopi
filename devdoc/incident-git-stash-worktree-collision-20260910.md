<!-- doc-class: normative -->
# Incident: git stash cross-worktree collision (2026-09-10)

## What happened

While verifying the job-family merge in linked worktree
`/home/user/workspace/gopi-jobfamily-t3` (branch `feat/job-family-t3`), I ran
`git stash` + `git stash pop` to measure the system-prompt density baseline
against the pristine tree. `refs/stash` is **REPO-GLOBAL** — shared by every
linked worktree of the same repository. The parallel task-family agent was
simultaneously working in `/home/user/workspace/gopi-task-tool` (branch
`feat/task-tool`) and had its own stash in flight. My `pop` consumed **their**
stash and vice versa: my complete job-family diff landed uncommitted in the
task-tool worktree, and their task-family diff landed in mine.

Symptom: `git status` in my tree showed files I never touched
(`internal/agent/reframe.go`, `prompt_test.go`, `taskarea_*.go`) modified and
my edited files (`cmd/agent/cli_log.go`, `internal/gopilog/formatter.go`,
`internal/tools/job*.go`) reverted — a change-set swap, not a loss.

## Recovery (nothing lost — a stash is a dangling commit, and both diffs were
## intact in the wrong trees)

1. In the tree holding MY work (theirs by accident): `git diff > /tmp/mine.patch`.
2. In the tree holding THEIR work (mine by accident): `git diff > /tmp/theirs.patch`.
3. Verify each patch's identity by unique symbols (job: `ToolJob`/`action=start`;
   task: `ToolTask`/`action=review`) — the `mine.patch` must have 0 of their
   symbols and vice versa.
4. In each tree: `git checkout -- .` (baseline) then `git apply` its rightful patch.
5. **`git commit` IMMEDIATELY** — committed work lives on a branch and can
   never be swapped by a later stash operation.
6. Untracked files (e.g. a devdoc report) are never part of a stash and stay
   put throughout.

## Guard

- **NEVER run `git stash`/`git stash pop` in a repo that has >1 linked
  worktree with live agents.** Use `git diff > patch` + `git apply` (per-tree
  files, no global ref) or commit to the branch instead.
- Verified state after recovery: `go build ./...` OK; density gate
  (`TestSystemPromptDensity`) OK; job-family targeted tests OK. Full suite run
  fresh post-recovery.
- Memory record #1570 holds the same lesson for cross-task retrieval.
