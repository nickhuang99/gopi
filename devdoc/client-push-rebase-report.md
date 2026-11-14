<!-- doc-class: historical -->

# Task Report: client-push rebase — the delivery before the removal's guards

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: *gopi worker client mode rebase* (session `01a0c060`), branch
`feat/client-push-rebase`, commit **1732dc81**, worktree
`/home/user/workspace/gopi-clientpush`.

## Completed

**The audit the epic asked for** — the three suspicions of the CLIENT close-out
chain (`internal/tools/task_finish_cleanup.go`: `taskFinishCleanupChain` →
`runFinishCleanupChainTarget`), each settled against the source:

1. **Push is checkout-independent — already correct, now test-pinned.** The argv
   is `git push <peer-target> <branch>:<branch>`, `<peer-target>` from the sync
   config (`finishCleanupServerTarget`) and `<branch>` from `deliverable.branch`.
   `TestClientPushArgvIgnoresCheckedOutBranch` moves the worktree to a `tmp/`
   branch and asserts the *deliverable* ref lands at the peer while the tmp
   branch never leaves the host; `TestClientPushArgvSurvivesDetachedHead` does
   the same on a detached HEAD; `TestClientPushUsesThePeerRemoteNotAPath` proves
   the target is the resolver's answer (an unrelated control repo stays empty).
2. **The branch is aligned with the peer's master before pushing — present on
   master (commit `2fdc65e4`), hardened here.** The step fetches the peer master
   into a mirror ref, merges it into the branch, rebuilds through the repo's
   declared contract, and pushes only if both pass.
   `TestClientPushCarriesPeerMaster` measures this on git's own state: with the
   peer advanced, the received ref has the peer's tip as an ancestor
   (`merge-base --is-ancestor`) and carries its content.
   `TestClientPushStopsWhenThePeerMasterConflicts` shows the refusing side — the
   remote is spared a deliverable that cannot be merged.
3. **Dirty / gone worktree still delivers — DEFECT, FIXED.** Three guards
   returned `rep.Skipped` at the HEAD of the chain: the dirty status/reason
   probe and ownership gate ①, so a worktree with residue, or one whose
   directory was already removed, delivered **nothing** — and no removal or
   branch deletion followed. Since a close-out is a single shot per row, this is
   the mechanism behind HP's stranded branches
   (`devdoc/hp-push-diagnosis-20260916.md` A/A′, task `01a0aab8`).

**The fix — reach, not tolerance.** `finishCleanupRemovalReason(ctx, rep, d, wt,
linked)` answers one question ("may this directory be removed?") and the chain
now runs **premerge ①b → push ② → removal ③ → branch ④**. Every removal-side
refusal sets `rep.Skipped` with its OWN named reason (dirt split into the
tracked/untracked names the reclaim guard already uses; a gone directory with
git's own chdir diagnostic; a foreign binding via ownership gate ①), so a reader
of the verb's disposition still learns the removal did not happen. Gate ① keeps
its full force: a directory this row does not own is never entered nor probed.

**The one delivery-side gate left** (`finishCleanupDeliveryPremise`), deliberately
narrow: ownership disagreement **AND** no ground left for the pre-merge gate,
asked exactly as the gate asks it (`finishCleanupPreMergeGroundOnBranch`). It
exists because this reordering makes that gate's ground load-bearing. Dirt is
explicitly not a premise.

**The unwind bug the reordering exposed.** `finishCleanupPreMergeUnwind` was a
`git reset --hard`, sound only while the chain gated the push on a clean tree.
Once a dirty worktree reaches the push, that reset would discard the run's
uncommitted work. The precondition is now asked where the unwind runs
(`finishCleanupGroundHoldsWork`) and a dirty ground KEEPS the trial merge. On a
clean ground every unwind is unchanged. `TestClientDeliverySurvivesDirtyWorktreeWithTrackedWork`
asserts the author's edit survives byte-for-byte.

**Rule 15 blind spots closed.** A failed premerge/push now appends
`finishCleanupRemovalNotReachedNote` — `rep.Skipped` is not set on those returns,
so the removal reason would otherwise vanish. A row whose branch ref is already
gone records a successful no-op instead of `error: src refspec … does not match
any` (measured on the second pass of an idempotent close-out).

**Settled-state narrowing.** `FinishCleanupReportSettled` reads a skip as settled
only when the report's own steps show a successful push
(`finishCleanupStepSucceeded`) — this drops the over-broad assertion
`devdoc/assert-the-bug-audit-20260917.md` §3 flagged (assigned to `01a0aab8`),
which no longer holds now that a skip can follow a real delivery.

## Not Done

- The **server arm** (`finishCleanupServerRecycle`) is untouched: it has no push,
  so the defect does not exist there.
- A failed premerge/push still records only a report step plus the
  removal-not-reached sentence. Adding a `task_events` row for it is a separate
  change (the premerge gate's own abstain/refuse events already exist).
- `devdoc/hp-push-diagnosis-20260916.md` A′ names this defect; whether the strand
  was additionally mitigated outside this chain was not re-audited.
- The 4 unclassified devdoc files failing `TestDocManifestCoversEveryDevdocFile`
  are **pre-existing on master**, not introduced here (named below).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0** (also
  `go build ./...` → exit 0).
- Tests: `go test ./internal/tools/ -count=1` → **ok** (see Evidence).
- Standards: `go test ./internal/standards/ -count=1` → the only failure is
  `TestDocManifestCoversEveryDevdocFile` naming 4 files that are unclassified on
  master as well (proved by re-running the same test with the working tree
  stashed: identical 4 files, 2110 rows / 2114 files). My new devdoc's manifest
  row IS accepted (2111 rows / 2115 files after it).
- Commit: `1732dc81` on `feat/client-push-rebase`.

## Evidence (self-filed)

- **Red proof** (the fix is what makes the new tests pass) — pre-fix source with  the new test file in place (`git stash push` of the two production files, a
  constants shim so the build stays honest):
  `go test ./internal/tools/ -run 'TestClientPush|TestClientDelivery' -count=1`
  ```
  --- FAIL: TestClientDeliverySurvivesGoneWorktree (0.06s)
      ... git [rev-parse refs/heads/feat/…] … exit status 128: unknown revision
  --- FAIL: TestClientDeliverySurvivesDirtyWorktreeWithTrackedWork (0.06s)
      ... step push missing from report []
  --- FAIL: TestClientDeliverySurvivesDirtyWorktreeAgainstAdvancedPeer (0.09s)
      ... step push missing from report [{Step:worktree … untracked-only residue …}]
  --- FAIL: TestClientDeliveryFailureNamesTheUncollectedWorktree (0.09s)
      ... a failed delivery must still say the worktree was not collected:
          "worktree dirty (live bond protected) — carries ONLY UNTRACKED files: ?? residue.tmp"
  FAIL
  ```
  `TestClientPush*` (questions ① and ②) PASSED pre-fix — reported above as
  already-correct-by-design rather than claimed as a fix.
- **Green** — same command on the fixed tree:
  `go test ./internal/tools/ -run 'TestClientPush|TestClientDelivery' -count=1`
  → `ok github.com/nick/gopi/internal/tools 2.318s`.
- **The chain suites** — `go test ./internal/tools/ -run 'TestFinishCleanup|TestPreMerge|TestWorktreeOwnership|TestProbe|TestServerRecycle|TestDeliverableWorktree|TestStale|TestLiveHolder|TestForeign|TestDead|TestCancelled|TestHelper|TestWorktreeReclaim|TestGitSync' -count=1`
  → `ok github.com/nick/gopi/internal/tools 56.030s`.
- **Full touched package** — the project's own budget command, not raw
  `go test`:
  `GOPI_HEAVY_SKIP_LEDGER=$PWD/.heavy-skipped.tsv scripts/test-budget.sh ./internal/tools/`
  → `exit=0 outcome=pass`, `1 finished, 0 with a failure`, 701s wall against a
  25m budget. Raw `go test ./internal/tools/ -count=1` is NOT the gate here: it
  hits Go's own 10m default per-package budget on this host and dies with
  `panic: test timed out after 10m0s` plus a goroutine dump (observed twice).
  The declared budget is 25m (`scripts/test-budget.sh`), and Rule 27's own note
  says the whole-package cost is the host's disk, not the diff.
- **The audit-doc closures** — `go test ./internal/tools/ -run
  'TestFinishCleanup|TestClientPush|TestClientDelivery|TestServerRecycle|TestAssertionTotality|TestPreMerge' -count=1`
  → `ok github.com/nick/gopi/internal/tools 36.532s`.
- **Settled-classifier check** — `TestFinishCleanupIdempotentPush` was observed
  failing pre-fix with `step push ok=false … error: src refspec … does not match
  any` (the second-pass defect) and passes post-fix.
- **Working tree** — `git status --porcelain` in `/home/user/workspace/gopi-clientpush`
  printed nothing after the commit.
