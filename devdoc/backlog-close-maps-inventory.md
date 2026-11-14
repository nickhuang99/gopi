<!-- doc-class: historical -->
# Task Report: Close merge-backlog row t-1788441090-1 (feat/maps-inventory)

## Decision
**NO-ACTION / already merged** — the branch was merged and deleted BEFORE this
backlog row was auto-submitted. No verify, merge, or archive was needed (and
none could run: the branch no longer exists).

## Completed
- Located the branch: `feat/maps-inventory` (scan short-name) = actual branch
  `feat/homm3-maps-inventory-feasibility` in ~/work/GoPi (not found in any
  other repo, remote, or workspace; peerbox host).
- Root cause of the row: the serve branch-scan (discoverMergeBacklogBranches,
  internal/tools/task_merge_backlog.go §2.1) enumerates local refs/heads and
  enqueued the row for a branch that the merge round had ALREADY landed.
- Closed row t-1788441090-1 as completed with the NO-ACTION verdict evidence.

## Verification evidence
| Check | Result |
|---|---|
| `git reflog master@{0}` | `merge feat/homm3-maps-inventory-feasibility: Fast-forward` → 38d8509 @ 19:35 CST |
| Branch commits on master | af24509 + 38d8509 "HOMM3 maps inventory + GBK repair" at master tip |
| `git for-each-ref \| grep -i maps` | empty (branch gone) |
| `git tag \| grep -i maps` | empty (never archived — branch -d'd on merge) |
| `git worktree list` | only main repo @ master |
| `git merge-base --is-ancestor 38d8509 master` | ancestor → MERGED-CONFIRMED |
| Row timestamps | merged 19:35, row auto-submitted 21:11 → row postdates the merge |

## Not Done
- No merge executed (branch already in master).
- No archive tag (branch was deleted at merge time, not archived).
- No verify closure needed — the merge round's own evidence stands.

## Follow-up observation (P2.5 guard)
The branch-scan has a small race window: a branch merged between the scan's
`for-each-ref` snapshot and the row's execution surfaces as a phantom backlog
row. The dedup check (mergeBacklogDedupCheck §2.2) keys on summary + branch
tip, which cannot catch a merge that lands after the snapshot. Future backlog
rows should be executed through `gopi task merge-execute` (its clean-tree +
safe-merge + merge-base guards classify an already-merged branch as "already
merged → branch -d → completed" instead of leaving the decision open).

## Verification
- Build: n/a (no code change)
- Tests: n/a
- Row state: `gopi task show t-1788441090-1` → status=completed, result=NO-ACTION
- Report commit: (documented in devdoc/backlog-close-maps-inventory.md)
