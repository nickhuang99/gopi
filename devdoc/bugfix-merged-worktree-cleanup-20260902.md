<!-- doc-class: historical -->
# Bugfix Report: serve merges branches but never removes the worktree

Date: 2026-09-02
Branch: `feat/fix-merged-worktree-cleanup`
Epic: BUG FIX — serve (server role) merges branches but never cleans up the
worktree; branch list frozen forever after merge (nick's finding, 2026-09-02).

## Symptom (nick, verified)

All 12 branches were IN-master (`merge-base --is-ancestor` confirmed), but
`git branch` still showed all 12 — every one had a worktree checkout
(worktree=1), and nothing removed the worktree after merge. Branch count
never decreased; cleanup never finished. nick: "git branch merged, if you
don't remove worktree, the cleanup work is not finished. this is a bug of
gopi server".

## Root cause (verified in code)

1. The serve tick's merge paths (serveMergeBacklogAudit/serveMergeExecSweep
   executor, adoption ③a) merge a branch into master, then attempt
   `git branch -d <branch>` — but git REFUSES `-d` for a branch checked out
   in a worktree ("Cannot delete branch ... checked out at ..."). The code
   tolerated this as a "reportable wart" (task_adoption.go:236,
   task_merge_executor.go:310) — so the branch AND its worktree stayed
   forever.
2. The mechanism that SHOULD remove worktrees — serveFinishCleanupSweep
   (cli_tasks.go, the 4-step push → worktree remove → branch -d chain) — is
   a CLIENT-only pass: `TaskFinishCleanupSessionID` carries an internal
   `IsServerNode()` gate that skips server/integration nodes (the server
   integrates, it does not push its own repo to itself). The integration
   node merges local branches via the merge-backlog executor, which is
   role-INDEPENDENT — so the merge happened, the client-gated cleanup
   never did.
3. Result: merged branches + their worktrees accumulated; merge works,
   cleanup doesn't.

## Fix

A new SERVER/INTEGRATION-side merged-branch cleanup pass, wired into the
serve tick alongside the other sweeps:

- **Engine** (`internal/tools/task_merged_cleanup.go`): `MergedCleanupSweep`
  discovers branches that are IN master (`git branch --merged master`) and
  still checked out in a linked worktree (`git worktree list --porcelain`).
  For each candidate it applies the safety guard chain (every guard
  load-bearing):

  ① merged — the branch must be an ancestor of master.
  ② linked worktree — a branch without a worktree is not this pass's job.
  ③ clean — the worktree's `git status --porcelain` must be EMPTY
     (untracked included). Dirty = a live Bond still writing there.
  ④ race-free recheck — `git merge-base --is-ancestor <branch> master`
     re-checked at removal time: a Bond that recommitted after the merge
     (branch ref moved off master, worktree HEAD moved in lockstep) is
     caught HERE, not just in the coarse discovery snapshot.
  ④b on-tip — the worktree HEAD must equal the branch tip.
  ⑤ no live ledger task (pending/claimed/in_progress) referencing the
     branch — via deliverable.branch OR the audit's
     "merge <branch> into master" summary shape.

  Only when ①–⑤ all pass: `git worktree remove <path>` then
  `git branch -d <branch>` (in the main repo dir). Fail-soft per branch
  (log + continue). Exemptions shared with the finish-cleanup chain:
  master/main/archive/* + GOPI_FINISH_CLEANUP_EXEMPT.

- **Serve wiring** (`cmd/agent/cli_tasks.go`): `serveMergedCleanupSweep()`
  rides the serve tick like the other sweeps — throttled by
  `GOPI_CLEANUP_INTERVAL` (default 15min, ≤0 disables), scan capped at 10
  branches/pass, log lines rendered per branch (removed/skipped/failed).

Why a worktree-removal + branch -d pass and not a change to the merge
executor: the executor's `branch -d` refusal is correct fail-soft behavior
at merge time — the worktree may hold a LIVE bond (the adoption merge gate
epic, running in parallel, depends on the worktree surviving while its task
is live). The cleanup belongs in a separate, guarded sweep that only acts
once the branch is provably dead (merged + clean + no live task). The
"reportable wart" comments in task_merge_executor.go / task_adoption.go now
point at this pass.

## Verification

- `go build ./...`: OK
- `go vet ./internal/tools/ ./cmd/agent/`: OK
- Engine tests (real-git fixture, `internal/tools/task_merged_cleanup_test.go`):
  - `TestMergedCleanupRemovesFrozenPair` (a) merged + clean + terminal
    task → worktree AND branch removed — PASS
  - `TestMergedCleanupNoTaskRowStillRemoves` — no ledger row (pure
    serve-merge shape) still safe to clean — PASS
  - `TestMergedCleanupDirtyWorktreeSkipped` (b) dirty → skipped untouched — PASS
  - `TestMergedCleanupLiveTaskSkipped` (c) in_progress task → skipped — PASS
  - `TestMergedCleanupRecommitAfterMergeSkipped` (d) recommit → untouched — PASS
  - `TestMergedCleanupStillMergedRecheck` — race-free ancestor recheck — PASS
  - `TestMergedCleanupLiveMergeSummaryMatch` — "merge X into master" live
    row protects — PASS
  - `TestMergedCleanupIdempotentSecondRun`, `TestMergedCleanupExemptBranches` — PASS
- Serve wiring tests (`cmd/agent/cli_tasks_merged_cleanup_test.go`):
  - `TestMergedCleanupIntervalFromEnv` — env parsing (Rule 2) — PASS
  - `TestServeMergedCleanupSweepThrottle` — pass cadence — PASS
  - `TestServeMergedCleanupSweepRemovesFrozenPair` — full serve pass
    removes the frozen pair — PASS
- Full suites: `go test ./internal/tools/ ./cmd/agent/` (run detached;
  internal/tools ≈ 13min, cmd/agent ≈ 3min).
- **End-to-end (real binary)**: built `bin/gopi`, constructed a real frozen
  pair (merged branch `feat/e2e-frozen` + linked worktree), ran
  `gopi task serve --once` from inside that repo → the serve tick logged
  `[serve] [merged-cleanup] removed branch=feat/e2e-frozen
  worktree=.../.e2e/wt` and both the worktree AND the branch were gone
  afterward. The fix works in the production serve path, not just fixtures.

## Live-note (2026-09-02 14:38)

While this fix was being built, the parallel adoption-merge-gate agent's
work and the live serve daemon shifted the frozen population: the last
merged-with-worktree branch (`feat/fix-task-run-double-exec`) was cleaned
up externally (worktree removed, branch gone — its worktree was clean and
its task terminal). The remaining `review/cli-vision` branch is NOT merged
(still carries work), so it is correctly untouched. The 12 frozen branches
the epic names had already been merged into master by the serve daemon
before this session; only `feat/fix-task-run-double-exec` still had a
worktree when this fix landed, and the fixture tests prove the pass removes
exactly that shape. The serve daemon (pid 980759, running the pre-fix
binary) will pick up the new pass after a rebuild + restart.

## Piggyback note (2026-09-02 15:00)

A parallel agent merged `review/cli-vision` (docs-only: devdoc/review-cli-
vision.md, a vision_analyze review) into this branch — piggybacking its
review doc onto a branch headed for master. Docs-only, no code overlap with
the fix; the merge (5feac96) is clean.

## Not Done

- The live serve daemon was not restarted with the fixed binary (its
  stdout is not captured; restarting a production daemon mid-epic risks the
  parallel agents' queue). The pass fires on the next daemon start after a
  `bin/gopi` rebuild.
- Acceptance #1 ("serve tick removes at least one currently-frozen merged
  branch") cannot be demonstrated against the live repo today: the frozen
  population shifted mid-task and the last merged-with-worktree branch was
  already cleaned by the parallel agent. The fixture tests demonstrate the
  removal path deterministically.
