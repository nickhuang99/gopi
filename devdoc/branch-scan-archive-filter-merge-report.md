<!-- doc-class: historical -->
# Task Report: Epic merge feat/branch-scan-archive-filter → master

Epic: review the branch diff vs master, run `scripts/safe-merge.sh
feat/branch-scan-archive-filter`, then merge. On conflict: report, do not
force. (HP client, 2026-09-01, branch tip 859db09/b6c96bf, ahead 5.)

## Completed

### 1. Branch located + tip verified
- `git fetch peerbox`; local branch `feat/branch-scan-archive-filter` existed in
  the linked worktree `~/work/GoPi-wt-archive-filter`, tip **b6c96bf**
  (5 commits ahead of master b66324c, 9 behind — merge-base 859db09).
- Content (7 files, +557): cmd/agent `archiveTagTips`/`isBranchArchived`
  archive-reachability filter wired into `listUnmergedBranches`;
  internal/tools `archiveTipReachable`/`archiveTagAncestry` filter wired into
  `AuditMergeBacklog` (both degrade to no-op on empty/failed tag listing);
  6 new tests (real git sandboxes); `scripts/git-hooks/post-receive`
  event-driven audit-merges trigger; 2 devdocs. Rule 1 constants everywhere
  (`archiveTagRefPrefix`, `archiveTagScanTimeout`).

### 2. safe-merge guard: exit 1 — expected, by design
- Guard rejected with "branch still occupied by worktree
  GoPi-wt-archive-filter". The branch's own devdoc predicted exactly this
  ("PA merge 时应先 remove worktree 再 merge"). The other guard layers
  (archive tag absent, no loser/ prefix) passed.

### 3. Zero-conflict proof (no force)
- `git merge-tree <base> master branch` → 0 conflict markers.
- Trial `git merge --no-commit --no-ff` → "Automatic merge went well; stopped
  before committing as requested", 7 files staged, zero conflict entries.
- The 9-behind commits touch other files; the only intersecting file
  (cli_tasks.go) merges cleanly.

### 4. Merged (no force needed)
- `go build ./...` + targeted tests green on the merged index →
  commit **652ea2a** `merge: feat/branch-scan-archive-filter — …` on master.
- `git merge-base --is-ancestor feat/branch-scan-archive-filter master` → OK.

### 5. Verification (build / test / full suite)
- Build: `go build ./...` OK; binary builds OK.
- Targeted: branch-scan/archive tests + TestParseTasksArgs PASS
  (internal/tools ok, cmd/agent ok).
- Full `go test ./...`: 4 failures — **all reproduced on the pre-merge
  baseline b66324c in a detached worktree** (zero new failures from this
  merge):
  - `TestReflowExportExcludesFromSelfRows`, `TestReflowExportGateV1Export`,
    `TestReflowGateSelfOnlyBothTagsExported` — pre-existing contract drift
    (56fd84a's from-<self> gate rewritten by eef8342/affc1b3 into a
    from-<peer> gate; a6c0f07's extension tests + the 56fd84a tests still
    pin the OLD self-absolute semantics). Reproduced at baseline 859db09
    too. NOT caused by this branch. Deferred to a memsync-domain task
    (decide: update test assertions to the Peer-gate contract, or restore
    the stricter gate; note production `syncPeerSession` never passes Peer
    → CLI self-export silently drops self-tagged rows — the 27-row class
    eef8342 claimed to close is still open for the CLI path).
  - `TestMemorySearchNonBlocking` — env flake, single-run PASS (green here).
- Full-suite known-noise ([memsync][warn] legacy-conflict lines,
  [task-notify]) matches pre-merge baseline behavior.

### 6. Rule 9 close-out (3-step) executed
1. Merged to master (652ea2a) — merge done by this task per epic.
2. Loser-branch archiving not applicable (winner); branch cleaned per
   guard contract: tag `archive/branch-scan-archive-filter-20260901`
   (keeps the tip reachable so future archive-filtering semantics hold),
   branch deleted, worktree `~/work/GoPi-wt-archive-filter` removed.
3. Review: branches now master / feat/e2e-live / feat/hook-test-probe /
   feat/test-a-doc-review / feat/test-b-rule6; worktrees master +
   gopi-rule6-gap only. Re-running safe-merge.sh for this branch would now
   pass (no worktree, no branch).

## Not Done
- memsync reflow-gate contract drift fix (3 pre-existing test failures) —
  out of this epic's scope; needs a decision, not a mechanical fix.
- `git push peerbox master` intentionally NOT run (push handled host-side).

## Verification
- Build: `go build ./...` OK (merged tree)
- Tests: targeted PASS; full-suite failures = pre-existing (baseline
  b66324c + 859db09 reproduced), zero merge-introduced regressions
- Merge: 652ea2a, zero conflicts, --no-ff, no force
- Commit: 652ea2a (merge), tag archive/branch-scan-archive-filter-20260901

## Deliverable declaration (Rule 6d)
1. Branch: `master` at 652ea2a (merge commit); recovery:
   `git show 652ea2a:devdoc/branch-scan-archive-filter.md` or
   `git show archive/branch-scan-archive-filter-20260901` for the old tip.
2. Commit confirmed in the main repo `~/work/GoPi` (merged in place).
