<!-- doc-class: historical -->
# Epic Merge Re-Review: feat/branch-scan-archive-filter (2026-09-02)

## Outcome: ALREADY MERGED — no action needed

Epic asked: review diff vs master, run safe-merge.sh, then merge (no force).
**The branch is already fully merged into master** by commit `652ea2a`
(2026-09-01, previous Epic run — memory #1217). This session re-verified
everything from scratch rather than trusting the memory record.

## Verification evidence

| Check | Result |
|---|---|
| Branch tip | `b6c96bf077…` — resolves, matches Epic's stated tip; remote-tracking `peerbox/feat/branch-scan-archive-filter` (local branch absent, lives on peerbox host) |
| merge-base(master, branch) | `b6c96bf` — **equals branch tip** ⇒ branch is an ancestor of master |
| `git merge-base --is-ancestor` | YES — already merged |
| ahead/behind (master vs branch) | 21 / 0 — branch has ZERO unmerged commits; master moved 21 commits past the merge |
| Merge commit | `652ea2a` "merge: feat/branch-scan-archive-filter — archive-tag reachability filter stops phantom merge-backlog rows (branch-scan + audit-merges dual path, post-receive trigger)" (Merge: b66324c b6c96bf) |
| Branch content | 5 commits on top of fork base 859db09: 16c391a post-receive hook, 995c062 audit-merges parse arm restore, 3da6409 archive-tag reachability filter, 5fc1a6d peerbox/master merge, b6c96bf docs — net `+557 insertions, 7 files`, zero deletions |
| Branch-owned files vs master | `git diff master <branch>` on the 7 branch files → empty (master side only gained/removed OTHER files after the merge) |

## safe-merge.sh result

```
bash scripts/safe-merge.sh feat/branch-scan-archive-filter
✅ [a] archive tag: 无 archive/feat/branch-scan-archive-filter
✅ [b] 分支名黑名单: 无违规前缀
✅ [c] worktree 占用: 无 worktree 挂着
ℹ️  [d] 尚未 merge 进 master   ← stale heuristic, see below
guard-exit=0
```

Note: guard line [d] says "尚未 merge" but that heuristic does not
distinguish "unmerged" from "merged + branch tip not yet deleted". The
authoritative check is `git merge-base --is-ancestor` (YES) plus
`rev-list --count` (0 ahead). Re-running `git merge` would be a no-op
("Already up to date") — skipped as pointless; no conflict exists to force.

## Post-merge state (Rule 9 compliant)

- Archive tag `archive/branch-scan-archive-filter-20260901` exists and
  contains tip b6c96bf → the branch tip stays reachable for the
  archive-filter feature it introduced.
- Worktree list clean: only master (main repo) + two unrelated fix/* worktrees.
- No conflict surface; nothing was forced; no new merge commit created.

## Not Done
- `gopi task cancel t-1788271386` (accidental empty placeholder submit):
  `gopi task cancel` subcommand doesn't exist. Left pending — harmless,
  claim will be skipped/no-op. Cleanup via `gopi task archive` if desired.
