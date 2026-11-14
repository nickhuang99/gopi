<!-- doc-class: historical -->
# Task Report: post-receive hook v5 — direct `git merge` per nick's final design

Epic: hook should first call gopi with token-free-hard-logic to execute simple git
merge and cleanup — "most merge is no brainer. just git merge branch-name and done".
Node: peer1 · Date: 2026-09-01 23:05–23:20

## Design (nick's verdict, zero ceremony)

On every push that updates a **non-master** branch, the hook merges it into master
RIGHT NOW. Git is the sole merge decision maker (ort built in):

- **clean** → auto-commit + `branch -d` (zero LLM, zero enqueue)
- **conflict** → exit non-zero → `git merge --abort` → `gopi task audit-merges
  --submit` enqueues ONE LLM merge task (the ONLY LLM touchpoint, deduped by tip)

Explicit non-goals per Epic: no merge-tree dry-run (git merge subsumes it), no
FF-only special-case (git merge fast-forwards naturally), no worker spawn
(serve daemon / manual `gopi task run` drains the queue).

Guards (all exit-0 — a hook must NEVER block a push):
1. clean working tree required (`git status --porcelain`) — dirty ⇒ skip merge,
   log only
2. master pushes never merged (master is the TARGET)
3. branch-deletion pushes (newrev = zeros) ignored
4. `^hp-unique*` archive-exempt branches untouched

## Deliverables

- `scripts/git-hooks/post-receive` — v5 final (was v4 enqueue-only stub @ c80f870)
- `scripts/install-hooks.sh` — cp+chmod sync of scripts/git-hooks/* → .git/hooks/*
  (kills the version-skew class flagged in hook-investigation-report.md)
- Installed: `.git/hooks/post-receive` (diff-verified identical to repo copy)
- Commits: `4bbb84d` (hook v5 + install script) … master tip `8916110`

## E2E verification — five live-fire cases, all PASS

| # | Case | Expected | Observed (audit log / reflog) |
|---|------|----------|-------------------------------|
| 1 | FF branch push (`probe/ff2`, parent = master tip) | merge = fast-forward, then `branch -d` | `merged: probe/ff2 (ort) — branch -d`; `master@{t}: merge probe/ff2: Fast-forward` (reflog `fa19911`) |
| 2 | Diverged clean branch (`probe/ort`: client adds file, master adds file) | ort 3-way merge, auto-commit, `branch -d` | `Merge made by the 'ort' strategy. … merged: probe/ort (ort) — branch -d` → master `8916110` |
| 3 | True conflict (`probe/sideA`: base X, master edits shared.txt→Y, sideA edits same lines) | exit≠0 ⇒ `merge --abort` ⇒ enqueue | `CONFLICT (content): Merge conflict in shared.txt` → `conflict: probe/sideA — abort + enqueue for LLM` → pending row `t-1788275828 merge probe/sideA into master`; master UNMOVED at `9b5889a` |
| 4 | Idempotent re-push (same tip replayed ×4; conflict + already-merged) | no duplicate merges, no ledger spam | re-conflict: still exactly 1 pending row (audit-merges tip-dedup); already-merged replay: `cleanup: probe/conflict already in master — branch -d`, no new commit |
| 5 | Non-blocking | hook ≤ ~100 ms, push returns instantly | hook wall 10–77 ms (enqueue is a detached subshell); full `git push` incl. hook = 28 ms. Hook exit always 0 |

Negative controls: dirty-tree replay (untracked file present) →
`skip: dirty working tree — merge deferred (enqueue-only)`, no merge attempted;
deletion-push replay (newrev=zeros) → zero merge traces; `hp-unique-test` replay →
`exempt: hp-unique-test — skipped`, branch survived.

## Operational hazards found during testing (all handled, none hook bugs)

1. **Shared worktree concurrency**: peer1 runs multiple agents; the merge-all Bond
   (t-1788275458) was mid-merge in the SAME working tree during my probes — my hook
   replays saw `UU cmd/agent/cli_tasks.go` left by ITS merge, not mine. Rule: before
   hook work, check `gopi task list --status in_progress` and wait for tree-clean
   windows (`git status --porcelain --untracked-files=no` = 0). Untracked files
   alone still count as dirty for the guard (by design — porcelain is the guard).
2. `merge --abort` on a clean tree exits 128 (nothing to abort) — harmless.
3. Probe rows/branches fully cleaned: probe/* branches deleted, ledger rows
   t-1788275828 / t-1788275554 → cancelled; audit-log evidence preserved in
   `.git/post-receive-audit.log` (backup: /tmp/hook-audit-backup.log pre-test).
4. Client clone for tests parked at `~/workspace/hook-e2e/client` (sandbox:
   /tmp is rejected by bash policy; clone under ~/workspace with explicit paths).

## Not Done / deferred

- The 2 pending LLM merge rows created by conflict-path tests were cancelled, not
  executed — executing the real backlog is t-1788275343 / t-1788275389 (dispatched).
- ssh-push (hp remote) path not live-fired from this session (no key); file-path
  push is protocol-identical (receive-pack), per hook-investigation-report.md §4.
- `gopi task merge` CLI subcommand (nick's alternate direction "why is it not a
  gopi cli interface?") — separate task #3128f17f already in flight; the hook
  works standalone without it.

## Verification summary

- Hook syntax: `bash -n` OK; installed copy diff-verified == repo copy.
- 5/5 live-fire cases pass (table above) + 3 negative controls.
- Master received only intended commits (probe artifacts reviewed + left in
  history — they document the test trail; `8916110` tip builds, tree clean).
- No Go code changed ⇒ no build/test delta beyond master's own state.
