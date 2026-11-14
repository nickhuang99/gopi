<!-- doc-class: historical -->
# Task Report: the merger's in-flight identity path judges on BOTH axes

Epic (split 3/5): make the dead-holder identity path judge a ledger row on
**both** ownership axes — the worktree it records and the **branch** it names —
instead of only the worktree field.

## The defect (two independent drops of the same row)

`internal/tools/task_holder_resume.go`, the resume arm of the merger's
dead-holder path, could not see a row that names a branch and no worktree
(`{"repo":"…","branch":"…"}`, the blob a run leaves when its tree was never
backfilled). Two places dropped it, and each alone was sufficient:

1. **The candidate scan.** `deadHolderResumeRows` selected rows with
   `WHERE COALESCE(deliverable,'') LIKE '%worktree%'` — a *needle* in the blob,
   not a semantic test. A branch-only row never entered the candidate set.
2. **The occupancy pairing.** `ResumeDeadHolderRuns` compared the row's
   `deliverable.worktree` *field* against git's path; for a branch-only row that
   field is `""`, so `path != wt` was always true and the row was dropped by an
   **empty-string comparison** — never judged at all.

The verdict machinery underneath (`holderProbe.resolve`, the cap, the ownership
handoff) was correct and tested; what was broken is that no branch-named row
ever reached it.

## The change (one file, `internal/tools/task_holder_resume.go`)

- **The scan loads the evidence, the parsers choose the axis.** The filter is now
  `COALESCE(deliverable,'') <> ''`; `taskDeliverableWorktree` (path axis) and
  `taskDeliverableBranchOrEmpty` (branch axis) decide what the row claims. This
  is the same fix `worktreeSweepTaskRows` measured one file over (2026-09-15: on
  a real probe ledger a branch-only row was absent from the evidence set while
  every direct-row test kept passing) — a needle truncates an axis out of the
  evidence **silently**, and no needle can be kept in step with the blob's
  spelling.
- **`deadHolderResumeRowWorktree`** (new) resolves which directory a row holds,
  on whichever axis it records:
  - *path axis* — the row records a directory; git must still register **that
    path** under the row's branch, and the resolved value is the recorded path
    unchanged (every downstream verdict keeps judging the row's own ground);
  - *branch axis* — the row records a branch and no directory; the resolved
    value is the worktree **git reports checking that branch out**. The
    discovery stays confined to the row's branch: a matching name held at a path
    the row never named is still this row's branch, never another row's
    directory (the branch axis selects on the branch; it never licenses a path).
  - *unreadable repo* (`byBranch == nil`) — the path axis falls back to the
    recorded directory (the only ground evidence there is); the branch axis
    cannot answer, so it claims nothing (Rule 15: never a guess).

## Why the occupancy veto is not weakened (the explicit regression risk)

The pairing stays a **two-sided, exact, transitive relation**: a row survives
only when git registers the row's branch **at the row's resolved path**. Because
`mergedCleanupWorktrees` is git's own branch→path map, the set of rows that
survive the branch axis is *exactly* the set the row's own recorded directory
would have selected — a row that records a different directory still fails the
pairing. Matching is exact map lookup on the full branch name (no prefix, no
substring, no trimming). The three-valued verdict (`live|dead|unreadable`) is
untouched: `holderProbe.resolve`, `claimPidLocallyDead` and the ownership
handoff are unchanged.

Two negative tests pin this, and both would fail on a prefix/substring match or
on any path-widening:

- `TestResumeArmBranchPrefixTrapIsNotAMatch` — the row names
  `feat/dead-holder-two` **and** `feat/dead-hold` while git holds
  `feat/dead-holder`: neither is a candidate (both directions of the trap).
- `TestResumeArmDoesNotWeakenTheOccupancyVeto` — the row names the **same
  branch** but records **another directory** (the `fix/<same-name>` collision
  shape): it is not paired with the checkout and never becomes a candidate.

## Verification

- Build: `go build ./...` — OK
- Vet: `go vet ./internal/tools/` — clean
- Standards gates: `go test ./internal/standards/` — **1 pre-existing RED**,
  `TestGateCwdReleaseFindsTheRealTree` (`found only 4 worktree-removing
  producer(s) across [cmd/agent internal/agent] (floor 5)`). Triaged, not
  assumed: this diff touches neither `cmd/agent` nor `internal/agent`
  (`git diff --name-only master...HEAD` = `internal/tools` + `devdoc` only), and
  a clean detached **master** worktree reproduces the identical failure.
  Dispatched as bug task `01a0aa6a-294b-7dcb-9b02-2353b1a19dd3`.
- New tests (real git repo, real linked worktree, real spawned holder processes;
  `gopi` binary stubbed via `GOPI_BINARY` so no real resume spawns):
  - `TestDeadHolderResumeRowsLoadABranchNamedCandidate` — loader red proof
  - `TestResumeArmJudgesABranchNamedRow` — PROTECTED while the holder is live;
    judged dead + acted on once the holder is provably gone, naming git's
    worktree for the branch
  - the two negative guards above — PASS
- **Red proof (pre-change tree, tests kept):** reverting only
  `task_holder_resume.go` to HEAD~1 makes the suite fail on behavior, not
  compilation:
  ```
  --- FAIL: TestDeadHolderResumeRowsLoadABranchNamedCandidate
      a claimed row naming a BRANCH (no worktree field) must be a resume candidate:
      the arm cannot judge what the loader never loaded (candidates = 0)
  --- FAIL: TestResumeArmJudgesABranchNamedRow
      a branch-named row whose holder is provably dead must reach the arm's verdict
      (registered worktree …/wt holds feat/dead-holder)
  --- FAIL: TestResumeArmBranchPrefixTrapIsNotAMatch
      precondition: the branch-named row t-prefix-trap-two must be loaded
  ```
  Both negative guards PASS on the pre-change tree as well — correctly: there a
  branch-named row never reaches the arm at all.
- Full `go test ./internal/tools/ -count=1` — **PASS**: `ok
  github.com/nick/gopi/internal/tools 687.496s`, zero `--- FAIL`, exit 0
  (`-timeout 2400s`; log kept outside the branch at
  `/dev/shm/gopi-test-scratch/evid-tools-full2.log`). A first run under the
  default 10m package budget hit `panic: test timed out after 10m0s / running
  tests: TestReclaimOwnFinishedWorktreesRefusesADirtyWorktree` — that test was
  *starting*, so it is a victim of accumulated wall time, not a hang, and it is
  untouched by this diff. Three sibling agents were running `go test ./...` /
  `./cmd/agent/` / `./internal/tools/` concurrently on this host (`ps -C go`),
  which is why the default budget is wrong here; with it raised the package is
  green.

## Honest scope notes

- The other branch-first path in the same file (the **protection** arm —
  `DeadHolderBranches` / `ReclaimHeldBranches` / `deadHolderProtectedBranches`)
  still judges the terminal-row path (`reclaimDeadHolderCandidate` returns
  `ok=false` when the row records no worktree, since nothing is freeable without
  a directory). Making that arm *report* a branch-named dead holder as protected
  is a different disposition (no reclaim action exists for a row with no
  directory) and is **not** part of this task's deliverable — it is stated here
  so it is not mistaken for done.
- `deadHolderResumeRows` now loads every row carrying a deliverable. The added
  rows are skipped by the axis resolution, not silently acted on; the status
  filter (`taskLiveHolderStatus`) and the attempt cap are unchanged.
- Pre-existing gofmt debt on master in
  `internal/tools/task_deliverable_worktree.go` (unrelated, left alone).

## Files

- `internal/tools/task_holder_resume.go` (production change)
- `internal/tools/task_holder_resume_both_axes_test.go` (new tests)
