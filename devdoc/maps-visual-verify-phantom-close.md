<!-- doc-class: historical -->
# Task Report: feat/maps-visual-verify merge-backlog phantom close

## Summary
Close the serve branch-scan merge-backlog row for `feat/maps-visual-verify`
with the correct decision: **NO-ACTION — already merged** (phantom row).
The branch is not unmerged on any master; it never lived in ~/work/GoPi.

## Completed
- Located the branch's home repo: `/home/user/Downloads/games/Maps` (maps
  project repo — branch-scan snapshot rows for maps branches run in GoPi's
  ledger but the branches live in the maps repo; pattern per memory #1363).
- Evidence set (maps repo):
  - `git reflog master@{0}` = `83f76ed merge feat/maps-visual-verify:
    Merge made by the 'ort' strategy.` @ 2026-09-03 21:11:31 +0800
  - `git log --oneline master --grep=visual` shows the branch's commit
    `5ca6651 exploration: visual-verify pipeline — headless vcmi
    render→screenshot→vision OCR verified on real map UI (task 4/4)` on master.
  - `git merge-base --is-ancestor <tip> master` = ancestor (merged-confirmed).
  - No `archive/*` tag; branch ref absent (`git for-each-ref | grep -i visual`
    empty) and no worktree — FF/branch -d at land time is the completed state,
    not a loss.
- Race quantified: merge landed 21:11:31; the backlog row
  (t-1788441091) was auto-submitted 21:11:31 and its [auto] dup mirror
  (t-1788441516) 21:11:36 — the serve scan's for-each-ref snapshot raced the
  merge round in the same minute.
- Closed BOTH rows as NO-ACTION (row + its dup mirror — one execution, two rows):
  - `t-1788441091` → completed
  - `t-1788441516` → completed

## Not Done
- No re-merge, no verify closure run, no archive tag, no branch deletion —
  the branch is already on maps master (83f76ed) as of 21:11:31.

## Verification
- Build: n/a (no code change)
- Tests: n/a (no code change)
- Ledger: `gopi task list` shows t-1788441091 and t-1788441516 completed;
  task-notify cmd ok on both updates
- Commit: n/a (maps repo holds the merge; no commit needed in GoPi — doc
  deliverable only, stored uncommitted in devdoc/ like prior NO-ACTION closes)
