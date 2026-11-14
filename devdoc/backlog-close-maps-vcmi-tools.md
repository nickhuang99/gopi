<!-- doc-class: historical -->
# Task Report: Close merge-backlog row t-1788441090-2 (feat/maps-vcmi-tools)

## Decision
**NO-ACTION / already merged** — `feat/maps-vcmi-tools` was merged into the
**maps repo** (`/home/user/Downloads/games/Maps`) master at `55715bb` on
2026-09-03 21:11:31 +0800, ~1 second BEFORE this backlog row was
auto-submitted (21:11:30). The branch was deleted at merge time; no verify,
merge, or archive was needed (and none could run).

## Completed
- **Located the branch's true home**: the serve branch-scan short-name
  `feat/maps-vcmi-tools` = the maps repo's branch (NOT ~/work/GoPi — grep of
  every ref/reflog/remote/tag/worktree there came up empty). The branch was
  created in `/home/user/Downloads/games/Maps` with worktree
  `/home/user/work/workspace/maps-vcmi` (commit b6dbeef "feasibility: vcmi
  map-editor toolchain…"), part of the 4-branch HOMM3 maps project
  (feat/maps-h3m-title, feat/maps-inventory, feat/maps-vcmi-tools,
  feat/maps-visual-verify).
- **Merge confirmed**: maps master `55715bb` = `Merge branch
  'feat/maps-vcmi-tools'`; branch commits b6dbeef + 5ca6651 (visual-verify
  explorer) sit in master ancestry (`git merge-base --is-ancestor` →
  ancestor = merged-confirmed).
- **Root cause of the row**: same phantom-race as the sibling maps-inventory
  row (see devdoc/backlog-close-maps-inventory.md): the serve branch-scan
  (`discoverMergeBacklogBranches`, internal/tools/task_merge_backlog.go §2.1)
  enumerates local refs/heads and enqueued the row in the same second the
  merge round landed the branch (row epoch 1788441090 vs merge epoch
  1788441091).
- Closed row t-1788441090-2 as **completed** with the NO-ACTION verdict.

## Verification evidence
| Check | Result |
|---|---|
| Branch refs in ~/work/GoPi (refs/heads, remotes, tags, worktrees, reflog) | empty — branch never lived there |
| Branch refs in /home/user/Downloads/games/Maps | empty (deleted at merge time) |
| Maps master reflog `master@{1}` | `merge feat/maps-vcmi-tools: Merge made by the 'ort' strategy.` → 55715bb @ 21:11:31 |
| Branch commits on maps master | b6dbeef + 5ca6651, ancestry of 55715bb |
| `git merge-base --is-ancestor` | ancestor → MERGED-CONFIRMED |
| `git tag \| grep -i vcmi/maps` (maps repo) | empty (never archived — branch -d'd at merge) |
| Row timestamps | merge 21:11:31 vs row submitted 21:11:30 → row predates merge by ~1s |
| `git worktree list` (maps repo) | only main repo @ master 83f76ed |

## Not Done
- No merge executed (branch already in master).
- No archive tag (branch deleted at merge time, not archived — correct).
- No verify closure needed — the merge round's own evidence stands.

## Follow-up (P2.5 guard, same as maps-inventory)
The branch-scan race window persists: a branch merged between the scan's
`for-each-ref` snapshot and the row's execution surfaces as a phantom backlog
row. Future backlog rows should run through `gopi task merge-execute` (its
clean-tree + safe-merge + merge-base guards classify an already-merged branch
as "already merged → completed" instead of leaving the decision open).

## Verification
- Build: n/a (no code change)
- Tests: n/a
- Row state: `gopi task show t-1788441090-2` → status=completed,
  result=NO-ACTION (verify_evidence digest)
