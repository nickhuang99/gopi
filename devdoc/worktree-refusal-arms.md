<!-- doc-class: historical -->
# Task Report: the three worktree-removal refusal arms (epic 01a0a6ff, 2026-09-16)
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## What the epic asked

A gopi run is supposed to remove its Rule 6 worktree when its development is
done. Three reported refusal classes keep it registered — and a registered
worktree is what `scripts/safe-merge.sh` gate [c] and the merger's protected
arm use to withhold a branch. The operator asked for each case to be
investigated from source and FIXED, not explained away:

1. a worktree whose only "dirt" is untracked files is not removed, and no run
   is ever told that leaving those files is a violation of its own hygiene;
2. "a second worktree is invisible to every ledger-keyed arm" — who creates a
   second worktree, and why does nothing collect it;
3. the verification path — who spawns the verifying gopi, does it create or
   reuse a worktree, does it commit, and what says "branch ready to merge".

## Completed

All three cases were traced to source symbols and measured on the live host,
and each got a fix with a regression test.

### Case 1 — untracked-only dirt is now a NAMED hygiene debt

**Measured root cause.** `git status --porcelain` is non-empty in two states
that the refusal arm rendered identically. `reclaimDisposition`'s guard ②
(`internal/tools/task_reclaim.go`) compared the porcelain body against the
empty string and returned ONE constant:

```go
if strings.TrimSpace(out) != reclaimStatusClean {
    return reclaimSkip, reclaimSkipDirtyReason   // "worktree is DIRTY (uncommitted work) — left for a human"
}
```

`reclaimSkipDirtyReason` is the only dirty-wording in the file, so every
refusal — including a tree whose tracked content is IDENTICAL to its commit
and whose only entries are `??` (a build output, a scratch file, a stray
probe) — read as "uncommitted work, left for a human". The measured live tally
in the attachment (242 refusals, ~58 of them this state) is therefore
unreadable: the actionable half — a run that must clean up after itself before
it exits, which is the operator's own requirement — is invisible inside the
word "dirty". Salvage made the same conflation: `dirty_holder_salvage.go`
selects on `reclaimSkipDirtyReason`, so an untracked-only branch was queued for
a "salvage" whose capture (`git diff HEAD --binary` + untracked archive)
contains no tracked change at all.

**Fix (refusal unchanged, the reason split).** `reclaimDirtyReason(porcelain)`
is the ONE classifier (new, `task_reclaim.go`): a body whose every entry
carries the `??` prefix returns the new `reclaimUntrackedResidueMarker`; any
other status (M/A/D/R/C/U/…) keeps `reclaimSkipDirtyReason`. The constant
`reclaimUntrackedProbePrefix = "??"` names the porcelain plumbing value instead
of inlining it. The finish-cleanup chain's dirty gate — which occurs THREE
times (`finishCleanupServerRecycle`, the client chain, and the worktree
step) — now goes through `finishCleanupWorktreeDirtyReason(rep, porcelain)`,
which makes the same split, records a durable step note for the untracked case
(`finishCleanupUntrackedResidueNote`) and reuses
`finishCleanupNoOutputSentinel` for the clean case (this also removed three
inlined `"(no output)"` literals in `task_merged_cleanup.go`,
`worktree_sweep.go`). The salvage lane now REPORTS an untracked-only branch
instead of queueing a no-op salvage for it.

**Not done deliberately:** the directory is still NOT removed. `git worktree
remove` (no `--force`) refuses it anyway, and the epic's own framing — "each
gopi should clean up development by not modifying any untracked files in the
worktree" — makes the run's hygiene the fix, not a delete. The fix is that the
violation is now nameable and countable.

### Case 2 — the second worktree: who makes it, and why nothing collected it

**Who creates it (answered from source).** `git worktree add` has **no Go-side
call site**: the executor creates it by RUNNING git (documented at
`internal/agent/loop.go`, the bash tool's post-round hook). Detection is the
seam `TaskDeliverableWorktreeBashForRow` → `TaskDeliverableWorktreeForRow`
(`internal/tools/task_deliverable_worktree.go`), which stamps the row's
`deliverable.worktree` when a round creates/moves a worktree or runs a
MUTATING git verb in one. Two facts make a SECOND registration inevitable:

* the seam **stops at the first hit** (`if _, _, ok := …ok { return }`) and
  never REBINDS an existing stamp (deliberately — the same file documents a
  measured "hijack" where a read-only probe re-pointed a row at somebody
  else's worktree);
* a row's deliverable records **exactly ONE** worktree path.

So when one run creates two linked worktrees in the same repo — its Rule 6
development tree AND a scratch checkout (`git worktree add --detach <dir>
<tip>`, the baseline / verification shape) — only the first is ever owned. The
second is named by no ledger row, and every reclaim arm is ledger-keyed:
`reclaimDeadHolderRowsFrom` (`WHERE deliverable LIKE '%worktree%' AND status IN
(…terminal…)`), `reclaimSelfRows` (`claimed_by = <this process> AND deliverable
LIKE '%worktree%'`), and the finish-cleanup chain (walks `deliverable.repo` /
`deliverable.worktree`). **Measured live, this epic's own attachment:** the
verification worktree `~/workspace/gopi-verify-01a0a4ae/wt-tip` sat registered
(detached HEAD, clean) while the only ledger row naming anything under that path
named the SIBLING directory; `gopi task sweep-worktrees --dry-run` reported it
`keep … younger than the threshold`.

**Second, independent reason it never converged (the honest half).** The
ownership-independent instrument already existed —
`WorktreeSweepRun`/`worktreeSweepJudge`, whose rungs are "no live holder, no
task row owns it, clean, older than 24 h" — but its only production caller is
`serveWorktreeReapSweep`, and that is called from `serveMergedCleanupSweep`
(`cmd/agent/cli_tasks.go:3100`), whose ONLY production caller was deleted
(2026-09-04). The heap is not unreachable for a reason (exhaustive `git worktree
list` + the live-holder pass is heavy for a 5-minute tick and a 24 h floor makes
the cost pointless) — but the effect is that only the operator verb
(`gopi task sweep-worktrees --apply`) ever runs it. Both halves are addressed:
the producer pass below frees the fresh case, and the resident wiring is the
separate, already-filed task `01a0a6eb-b6fa-7e9d-a3bd-cd92d6b07c6b` (I did not
duplicate that fix; it is pending, and this report states the overlap).

**Fix — a producer-side, ownership-independent pass.**
`ReclaimSelfOwnRegisteredWorktrees` (`internal/tools/task_reclaim_self.go`) +
its bounded wrapper `ReclaimSelfOwnRegisteredWorktreesWithin`, wired into BOTH
producer close-outs (the `gopi worker` round defer and the `gopi task run`
wrapper tail). It is scoped by PROCESS, not by ledger:

* repos = the claim rows THIS process holds (`reclaimSelfRows`, already the
  producer's evidence) ∪ the repo of its own cwd (`reclaimSelfOwnRepoOf`);
* per registration: main/bare/prunable/locked skipped; **age floor**
  `reclaimSelfOwnMinAge = 5m` (creation record, never mtime) so a concurrent
  round of the same process cannot lose the checkout it is making; **live
  holder** veto (`scratch.HeldByProcesses`, the shared scan); **cleanliness**
  (`reclaimDirtyReason`); **live-owner ledger veto**
  (`worktreeSweepTaskRows`, the shared ownership reader — a live row naming
  this path owns it, which is what protects an IDLE sibling session whose
  build runs in the background); **declared-root** policy
  (`reclaimSelfOwnInRoot` → `worktreeroots.PermitsAny`, the sweeper's own
  rungs); removal is the ordinary `reclaimGitRemove` (never `--force`; the
  branch is never touched); the run is capped
  (`reclaimSelfOwnMax = 8`) and every keep is NAMED (Rule 15).

The binary-tested RED→GREEN test is
`TestReclaimSelfOwnReclaimsUnownedAgedRegistration`; the safety invariant is
`TestReclaimSelfOwnKeepsLiveSiblingsGround` (an idle live sibling's clean, aged
worktree must survive — without that veto this pass would be worse than the
leak it closes).

### Case 3 — the verification path

**Answered from source.** The merger spawns it: `runMergeVerifyChild` →
`gopi task verify <id>` → `TaskVerify` (`internal/tools/tasks.go`), which runs a
fresh `gopi --timeout 0` child fed a briefing. The child **commits nothing** —
its contract is "Record the verdict EXACTLY ONCE, then stop — do NOT fix the bug
yourself". And **it used to create its own worktree**: the live session log for
`01a0a4ae` shows the verifier's first rounds as

```
V=~/workspace/gopi-verify-01a0a4ae; W=$V/wt-tip; rm -rf $W; cd ~/work/GoPi; git worktree add --detach $W 664b3784
```

i.e. `rm -rf` + `git worktree add --detach` — a SECOND linked worktree on the
merger's own hot path, not an operator's one-off. That is case 2's leak with a
named producer. (The ledger was never the signal the operator was looking for:
the merger reads the verdict from the ROW's status — `mergeVerifyRecorded` →
`verified`/`failed` — and a `failed` verdict is not the same as a merge
conflict. A detached tip worktree cannot become "the branch merges but the
worktree lingers", because a detached HEAD holds no branch and the branch is
never checked out in it.)

**Fix.** The gate now hands the child the delivering row's OWN recorded
worktree (`MergeVerifyRecord` gained a `Worktree` field, populated in
`MergeVerifyRecordForBranch` from `deliverable.worktree`; `runMergeVerifyChild`
exports it as `tools.MergeVerifyWorktreeEnv`), and the briefing gained
`taskVerifyWorktreeClause`: "the delivered work already occupies the linking
worktree <path> — run your build/tests THERE; create NO new worktree; a
read-only detached head inside that same directory is acceptable; never delete
the delivering run's files." The verifier thus reuses the ground the work is
already in instead of minting a registration nobody can own, and the producer
pass above frees the one it used to leak.

## Files changed

| file | change |
|---|---|
| `internal/tools/task_reclaim.go` | `reclaimUntrackedResidueMarker` / `reclaimUntrackedProbePrefix` constants; `reclaimDirtyReason` classifier; guard ② uses it |
| `internal/tools/task_finish_cleanup.go` | `finishCleanupWorktreeDirtyReason` + the two refusal prefixes + the residue step note + `finishCleanupNoOutputSentinel`; the three dirty gates use it |
| `internal/tools/task_merged_cleanup.go`, `internal/tools/worktree_sweep.go` | inlined `"(no output)"` → `finishCleanupNoOutputSentinel` (Rule 1b) |
| `internal/tools/dirty_holder_salvage.go` | untracked-only branch is REPORTED, not queued for a no-op salvage |
| `internal/tools/task_reclaim_self.go` | `ReclaimSelfOwnRegisteredWorktrees{,Within}`, the guard ladder, repo resolution, report markers |
| `internal/tools/verify_gate.go` | `MergeVerifyRecord.Worktree` read from the deliverable |
| `internal/tools/tasks.go` | `MergeVerifyWorktreeEnv`, `taskVerifyWorktreeClause`, briefing wired |
| `cmd/agent/merger_verify_child.go` | the gate exports the delivered worktree to the verify child |
| `cmd/agent/cli_tasks_worker.go`, `cmd/agent/cli_tasks.go` | both producer close-outs run the new pass before the cwd release |
| `internal/tools/worktree_refusal_arms_test.go` | new: the regression tests for all three cases |
| `devdoc/worktree-refusal-arms.md`, `tasks/01a0a6ff-report.md` | this report + the distilled writeup |

## Not Done

* **Case 1 removal is deliberately NOT enabled.** An untracked-only tree is
  still kept (`git worktree remove` refuses it, and the epic's own requirement
  is that the RUN cleans up). The fix makes the violation visible; a future
  task could add an opt-in removal for untracked-only trees, but that is a
  policy decision for the operator, not a silent sweeper behaviour.
* **The unowned-registration collector is production-reachable only through the
  producer close-outs.** The resident tick path is `01a0a6eb-b6fa-7e9d-a3bd-cd92d6b07c6b`
  (already filed, still pending): `serveWorktreeReapSweep` hangs off
  `serveMergedCleanupSweep`, which has no production caller. Verified in this
  worktree: `code_search serveWorktreeReapSweep` → `cmd/agent/cli_tasks.go:3100`
  (inside `serveMergedCleanupSweep`) and `:3114` (the definition); that function
  is called only from tests.
* **`cmd/agent`'s test package does not compile on master** —
  `funcBody redeclared` (`cli_tasks_rebuild_pass_test.go` vs
  `merger_resume_arm_wiring_test.go`), introduced by merge `664b3784` and filed
  as `01a0a6eb-10c6-7ee0-a22e-18394734c4ec`. Consequence for THIS task: the
  `cmd/agent` package's own tests (including the verify-child tests) could not
  be executed here, so my `cmd/agent` change is verified by `go build` +
  `go vet` on the non-test tree only. This must be re-run once that bug is
  fixed.

## Verification

* Build: `CGO_ENABLED=0 go build ./...` → exit 0.
* Vet: `go vet ./internal/tools/` → clean. `go vet ./cmd/agent/` → the
  pre-existing `funcBody redeclared` test-compile failure (see Not Done).
* New regression tests (Rule 10.5), all in `internal/tools/worktree_refusal_arms_test.go`:
  `TestReclaimDirtyReasonNamesUntrackedResidueApartFromWork` (7 subcases),
  `TestFinishCleanupWorktreeDirtyReasonMatchesReclaim`,
  `TestReclaimDispositionUntrackedResidueStillRefuses` (refusal unchanged, file
  survives), `TestReclaimSelfOwnWhyYoungerThanFloorIsKept`,
  `TestReclaimSelfOwnReclaimsUnownedAgedRegistration` (RED→GREEN: the directory
  is gone, the branch survives), `TestReclaimSelfOwnKeepsDirtyAndLiveGround`,
  `TestReclaimSelfOwnKeepsLiveSiblingsGround`,
  `TestTaskVerifyWorktreeClauseNamesTheDeliveredTree`.
* `go test -run 'TestReclaimDirtyReason|TestFinishCleanupWorktreeDirtyReason|TestReclaimDispositionUntrackedResidue|TestReclaimSelfOwn|TestTaskVerifyWorktreeClause' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools`.
* Full package suites: see the run recorded in the final message.
* `go test ./internal/standards/` → ok (the six machine-checkable gates pass
  with the new code in tree).

## Live diagnosis (the reproduction, on this host)

```
$ gopi task sweep-worktrees --dry-run
sweep-worktrees (dry-run): repos=2 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0
  keep        age=0h /home/user/workspace/gopi-master-baseline (dirty …: M cmd/agent/cli_tasks_rebuild_pass_test.go)
  keep        age=0h /home/user/workspace/gopi-verify-01a0a4ae/wt-tip (held by a live process (cwd/fd/exe/maps))
  keep        age=0h /home/user/workspace/gopi-verify-base-4ab3aaca (younger than the threshold (17m0s < 24h0m0s))
```

`wt-tip` is exactly case 2+3: a verifier-minted detached registration, held only
because that verification was still running; `gopi-verify-base-*` is the same
shape from the same task. The ledger join reproduces the operator's "invisible"
observation: of the rows naming a worktree, 16 paths are named by more than one
row (a loop `rm -rf` + re-`worktree add` on the SAME path, so the previous
run's registration silently changes owner), and **every one of the 49
`verified` rows' recorded worktrees no longer exists** — `verified` is the
merger's acceptance state and it does not enter the close-out chain
(`taskCleanupEligibleStatus` = completed/failed/cancelled), so those rows keep a
stale record forever while the directory is freed by another arm.
