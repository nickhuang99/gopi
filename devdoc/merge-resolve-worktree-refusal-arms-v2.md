<!-- doc-class: historical -->
# Task Report: resolve merge `feat/worktree-refusal-arms-v2` into `master`

## Completed

- Reproduced the merge in this worktree (`git merge master --no-commit --no-ff`) — 4 conflicted files,
  8 hunks under `internal/tools/`, resolved as a two-parent merge commit
  `659f9dc5` (parents `34d397d1` branch tip + `30450fc6` master tip).
- Resolution per hunk (union — neither side's intent dropped):
  1. **`task_reclaim.go` constants** — `reclaimUntrackedResidueReason` (branch) and
     `reclaimUntrackedResidueMarker` (master) named ONE state; defined once, second name is an
     alias (Rules 13/14). Kept the **branch's wording** because the pre-existing operator word
     `DIRTY` is what live arms + the red proofs match (`zz_redproof_reclaim_promise_test.go`
     asserts it); master's sentence drops it. Same for `reclaimUntrackedProbePrefix` →
     alias of `reclaimPorcelainUntrackedMarker`, and `reclaimStatusNoOutputSentinel` → alias of
     `finishCleanupNoOutput`.
  2. **`reclaimDirtyVerdict` / `reclaimDirtyReason`** — branch's classifier is the definition;
     master's name kept live as a thin delegating sibling so both sides' callers work.
  3. **`dirty_holder_salvage.go` salvage lane** — genuine disagreement. Master excluded the
     untracked-only state from salvage ("nothing to capture"); the branch's `reclaimDirtyHeld`
     covered both shapes. Kept **filing** — an untracked file's only copy IS that directory,
     and the keystone red proof `TestZZRedProof_DirtyDeadHolderWorkIsOrphaned` (present
     UNCHANGED on both sides) is **RED on master's own tree** for the exclusion (measured:
     `git archive 30450fc6` → FAIL "ORPHANED WORK"). Master's intent preserved the other way:
     the state is still named in the refusal and captured as a tarball.
  4. Master's duplicate `taskVerifyWorktreeClause()`/env family in `tasks.go` (unconflicted
     but compile-breaking) de-duplicated onto the branch's row-based clause, master's env arm
     kept as a live fallback.
- Two of the branch's *spec* tests were adapted, not dropped: master's
  `TestReclaimDirtyReasonNamesUntrackedResidueApartFromWork`,
  `TestReclaimDispositionUntrackedResidueStillRefuses` and
  `TestReclaimSelfOwnKeepsDirtyAndLiveGround` compared the reason for exact equality, while the
  kept classifier also quotes the offending entries back (Rule 15 — and master's own
  finish-cleanup arm appends the body the same way). They now pin the named opening sentence
  **plus** the quoted body, so both sides' guarantees are asserted.

## Not Done

- No full-suite run (`go test ./...`) — excluded by the epic (known pre-existing hang).
  Only `./internal/tools/` (the touched package) was run.
- The salvage-arm disagreement resolution is a judgement call recorded above; a reviewer who
  prefers master's "no row for untracked-only" must also delete or invert the keystone red
  proof, which is red on master itself.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK (exit 0)
- Vet: `go vet ./internal/tools/` — clean
- Tests: `go test ./internal/tools/ -count=1 -run 'TestReclaim|TestFinishCleanup|TestTaskVerify|TestZZRedProof|TestSalvage|TestMergerSweep'`
  — `ok github.com/nick/gopi/internal/tools 6.04s`
- Conflict markers: `grep -rn '^<<<<<<<\|^>>>>>>>' --include='*.go' .` → 0
- Commit: `659f9dc5 resolve merge feat/worktree-refusal-arms-v2 into master`
  (branch: `feat/worktree-refusal-arms-v2`; worktree
  `/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-resolve-2938248/gopi-merge-resolve-2769694623/wt`;
  no push, merger re-merges and cleans up)
