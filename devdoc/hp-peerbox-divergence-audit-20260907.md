<!-- doc-class: historical -->
# Task Report: HP↔peerbox git divergence audit — 4 local commits dropped as duplicates

## Completed
- Audited `master...peerbox/master` [ahead 4, behind 25]. Range-diff + content diff proved ALL 4 HP-only
  commits are patch-identical to commits already on peerbox/master (the "4 vs 25" divergence was
  cosmetic — same patches, different SHAs, peerbox-side history was rewritten/merged differently):
  - 90f31d0/043c2cc ≙ 0721592/4b82a1b (system-prompt review docs, peerbox has pre-redaction variant)
  - 506b65d/fdeba33 ≙ 453c1fd/18121de (websearch merge_results.py recovery + devdoc, identical patches)
  - `git cherry peerbox/master master` → 0 unique (+)
- Rebased master onto peerbox/master with `git rebase --onto peerbox/master 043c2cc master`:
  git auto-dropped 2 truly-identical websearch commits; the 2 review-doc commits also folded —
  net result **no new HP content pushed**, peerbox's redacted variant is the surviving content.
- Verified post-rebase: `git status -sb` = `## master...peerbox/master` (0 ahead, 0 behind), working
  tree clean. Untracked r7-k.png/r7-o.png (render checks) + sandbox/ left untracked intentionally.
- Anchored 7 dangling commits (rescue branches, see Not Done) instead of letting gc eat them.
- Docs audit result: review2-log-hp.md / ab2-control / ab2-control-runcmd-trace /
  investigation-fake-in-progress do NOT exist on peerbox master → rescued, NOT pushed (operator review
  gate; rescue branches are the audit trail).

## Not Done (deliberate)
- NO rescue-branch content pushed to peerbox. 4 doc-only dangling commits (review2-log 208L,
  ab2 trace 82+39L, fake-in-progress investigation 80L) need an operator yes/no on HP-local
  reviews being worth peerbox history; memory_sync_claim_test+arxiv WIP (8aeaa76) is superseded by
  4f28a76/99822f9 on peerbox master; 7fa34c4 (post-commit-review hash fill) vs e8dc675 is a 1-line
  coin-flip — all parked on rescue/* branches 2026-09-07.
- Untracked sandbox/ + PNGs not committed (render scratch).

## Verification
- git log: master == peerbox/master tip be87985, `git cherry` empty pre-rebase
- git status -sb: `[ahead 0, behind 0]` equivalent (no divergence marker) post-rebase
- dangling commits: 7 rescue branches created (hp-review2-log, hp-ab-reports, hp-ab-control,
  hp-fake-inprogress, hp-postcommit-8cc1fd1, hp-wip-arxiv, + fdeba33-equivalent no longer needed
  — dropped as duplicate)
- Commit: n/a (rebase only, nothing new to commit; no push performed — peerbox already had 100% of
  HP's unique patches)
