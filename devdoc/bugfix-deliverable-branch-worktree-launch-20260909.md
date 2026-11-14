<!-- doc-class: historical -->
# Task Report: deliverable.branch worktree-launch backfill fix

## Summary

Bug: `deliverable.branch` backfill failed when the task executor **started in the
main repo** (master checked out) and then delivered from a **self-created linked
worktree**. t-1788813641 delivered to `feat/prompt-review` (bb5d1cf) yet its row
stayed `{"repo":...}` with no branch — while last night's tasks (worktree ==
launch dir) backfilled fine.

## Completed

### Root cause (verified code-level + live session evidence)
- `taskWritebackBranch()` (`internal/tools/task_deliverable_branch.go`) resolved
  the branch ONLY from the child session's `session_start.workdir` via
  `git -C <workdir> branch --show-current`.
- t-1788813641's session (2ab6c869, live-verified): executor launched from
  `/home/user/work/GoPi` (**master**) → `session_start.workdir` = main repo →
  writeback read branch = master → `taskBranchExempt(master)` → returned `""` →
  no branch stamped. The real delivery was on `feat/prompt-review` in a worktree
  created mid-session by an **inline** bash command
  (`cd ~/work/GoPi && git worktree add ~/workspace/gopi-prompt-review -b
  feat/prompt-review ...`), followed by ~40 rounds of inline
  `cd ~/workspace/gopi-prompt-review && git ...` — no `workdir` param anywhere.
- The existing mid-run seam `TaskDeliverableWorktreeBash` is gated on
  `TaskWrapperEnv` + per-command workdir candidates that inline `cd` commands
  defeat (only the main-repo cwd is ever a candidate) — it did not fire for this
  shape.

### Fix — two-path resolution in `taskWritebackBranch()`
1. **Launch workdir (primary, unchanged semantics):** `session_start.workdir` →
   `git branch --show-current` → exempt-check. Correct when worktree == launch
   dir (last night's regression shape).
2. **Worktree-add fallback (new):** when path 1 yields nothing or an **exempt**
   branch (master/main/archive — the launch-from-main-repo shape), scan the
   child session JSONL's bash `tool_use` commands for the **LAST**
   `git worktree add ... -b|-B <branch>` and use that branch — after verifying
   it **exists** in the launch repo (`git -C <workdir> rev-parse --verify
   refs/heads/<branch>`) and is non-exempt.

New helpers in `internal/tools/task_deliverable_branch.go`:
- `taskBranchFromWorkdir` — path-1 git call (extracted, exempt-checked).
- `taskWorktreeAddBranchFromSession` — JSONL scan; chronological LAST add wins;
  mirrors gopilog ContentBlock shape (`assistant` → `message.content[]` →
  `{type:tool_use,name:bash,input:{command}}`).
- `taskWorktreeAddBranchFromCommand` — parses one command string; tolerates both
  `add <path> -b <branch>` and `add -b <branch> <path>` orders, `-C repo` form,
  and shell-glued tails (`2>&1 | tail -2`).
- `taskBranchExists` — ref-traversal/flag-injection guarded existence check.

### Verification (acceptance criteria)
1. **Repro** — `TestDeliverableBranchMasterLaunchWorktreeAddBackfill` PASS:
   launch workdir = main repo (master), session records inline
   `git worktree add <wt> -b <branch>` → writeback stamps
   `deliverable.branch = <branch>` (not master, not empty).
2. **Regression** — pre-existing worktree==launch-dir tests all PASS
   (`BackfilledAtWriteback`, `ClosesAdoptionLoop`, …).
3. **Master/main/archive exemption preserved** —
   `TestDeliverableBranchMasterLaunchNoWorktreeNoBackfill` PASS (master work with
   no worktree-add stays branch-less); `taskBranchExempt` still guards both paths.
4. **Live-session probe** — scanning the real t-1788813641 session (2ab6c869)
   yields exactly `feat/prompt-review` (the branch the row was missing).
5. Parser unit test `TestWorktreeAddBranchFromCommand` covers both arg orders,
   `-B`, `-C` form, non-add commands, and shell-glued branch tails.

## Not Done
- The mid-run `TaskDeliverableWorktreeBash` seam remains blind to inline-`cd`
  executors (it requires `TaskWrapperEnv` + workdir candidates). Not needed for
  the writeback fix — the session-scan fallback now covers that shape at the
  authoritative moment (writeback). Not enabling the seam mid-run (it fires on
  pending/in_progress rows only and this row type may not carry `TaskWrapperEnv`).
- No backfill of historical rows (t-1788813641 itself). The fix applies at
  writeback time; a one-off `UPDATE` was out of scope. The branch still exists
  on the repo, so a manual task-row repair is possible if desired.

## Verification
- Build: `go build ./...` OK
- Tests: focused `TestDeliverableBranch* | TestWorktreeAddBranchFromCommand` OK
  (8 PASS); full `go test ./...` **exit 0** — all packages ok (cmd/agent 139s,
  internal/agent 80s, internal/tools 554s); `go vet ./internal/tools/` OK
- Commit: `698c28d` fix(tasks): backfill deliverable.branch from child's
  worktree-add when launch dir is master-exempt
