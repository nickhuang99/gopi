<!-- doc-class: historical -->
# Client-push rebase: the delivery is ordered BEFORE the removal's guards

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a0c060` epic *gopi worker client mode rebase* (branch
`feat/client-push-rebase`). Date 2026-09-19.

## The three questions, and what the source actually said

The epic asked whether the gopi CLIENT (worker) close-out — `taskFinishCleanupChain`
→ `runFinishCleanupChainTarget`, `internal/tools/task_finish_cleanup.go` — really
does three things. Each was settled by reading the source and then by a test that
measures git's OWN state, never the report:

| # | question | verdict | evidence |
|---|---|---|---|
| ① | does the push depend on the checked-out branch? | **already correct** | argv is `git push <peer> <branch>:<branch>` with the peer from the sync config and `<branch>` from `deliverable.branch`; `TestClientPushArgvIgnoresCheckedOutBranch` (worktree moved to `tmp/experiment`) and `TestClientPushArgvSurvivesDetachedHead` |
| ② | is the branch aligned with the peer's master before the push? | **present on master, hardened here** | commit `2fdc65e4` (`feat/client-premerge-verify`): fetch peer master → trial merge → declared build → push only if both pass. `TestClientPushCarriesPeerMaster` measures ancestry at the peer; the hardening is the unwind bug below |
| ③ | does a dirty / already-gone worktree still deliver? | **DEFECT — fixed here** | three guards returned `rep.Skipped` at the HEAD of the chain, before any push; `TestClientDeliverySurvives*` fail against the pre-fix source with `step push missing from report` |

## The defect (③), and why it was the live failure

`devdoc/hp-push-diagnosis-20260916.md` §1 finding A/A′ is the same shape from the
field: HP's three finished feature branches sat on the client host, each with a
dirty or removed worktree, each having delivered **nothing**. The guard's
*reach* was the bug: a removal-side condition (dirt, a directory that is gone, a
directory this row does not own) aborted a chain whose most important act is the
push, and a close-out is a **single shot per row** — nothing re-runs it.

Fix (all three demoted to REMOVAL-side refusals, recorded):
`finishCleanupRemovalReason(ctx, rep, d, wt, linked)` returns the reason the
directory may not be removed; the chain probes it first (it still never touches a
directory gate ① refuses), then **premerge ①b → push ② → removal ③ → branch ④**.
The refusal sets `rep.Skipped` + its own named reason, so a reader of the verb's
disposition still learns the removal did not happen, and
`finishCleanupNoteRemovalNotReached` appends "the worktree was NOT collected" on
the delivery-failure returns (where `rep.Skipped` is not set — a Rule 15 blind
spot otherwise).

**One delivery-side gate remains**, deliberately narrow —
`finishCleanupDeliveryPremise`: ownership disagreement **AND** no ground left for
the pre-merge gate (checked with `finishCleanupPreMergeGroundOnBranch`, exactly
how the gate resolves its own ground). It exists because the reordering IT
introduces makes the pre-merge gate's ground load-bearing in a way it was not
when the guards ran first. Dirt is explicitly NOT a premise: the pre-merge gate's
cleanliness need is enforced where the trial merge runs.

## The unwind bug the reordering exposed (② hardening)

`finishCleanupPreMergeUnwind` was a `git reset --hard` on the ground, sound only
because the chain used to gate the push on a CLEAN tree. Once a dirty worktree
reaches the push, `--hard` would discard the run's uncommitted work. So the
precondition is now asked at the moment the unwind would run —
`finishCleanupGroundHoldsWork` — and a dirty ground KEEPS the trial merge
(precisely the outcome the refusal path produced anyway, minus the destruction).
On a clean ground the answer is false and every unwind is byte-identical to
before. `TestClientDeliverySurvivesDirtyWorktreeWithTrackedWork` asserts the
reordered world's non-obvious fact: the author's uncommitted edit survives.

## Settled-state narrowing

`FinishCleanupReportSettled` now reads a skip as settled only when the report's
own steps show a SUCCESSFUL push (`finishCleanupStepSucceeded`). This excludes
the dirty-worktree skip that `devdoc/assert-the-bug-audit-20260917.md` §3 flagged
as an over-broad assertion (assigned to `01a0aab8`) — it no longer reads as a
finished row now that the same reason can follow a real delivery. The gone-
directory skip keeps its existing `dir + chdir marker` path, which is
independent of ordering.

**The ENTRY-ARM gate is gone too.** `devdoc/requirements/merge-and-branches.md`
:447-449 recorded the sharper half of the defect: the SAME dirty probe and the
same early returns existed a second time in the OUTER `taskFinishCleanupChain`,
so "a maintainer who reorders inside `runFinishCleanupChainTarget` still ships
the defect, because `taskFinishCleanupChain` returns at `:1148` before the body
is reached. **The entry-arm gate is a door, not a check.**" That duplicate (the
probe, its two returns, and the empty-target return) is removed; the outer
function now resolves the branch + worktree and hands over, and every refusal
live there is a fact the delivery cannot work around at all (no branch name, an
exempt branch).

## The audit's three over-broad assertions (01a0aab8's list) — all three closed

`devdoc/assert-the-bug-audit-20260917.md` §3 assigned three over-broad
assertions to task `01a0aab8`; the fix they depend on is this one, so they are
closed here rather than left split across two branches:

1. `TestFinishCleanupChainDirtyWorktree` — **split, as §3 instructed**: the
   destructive half (directory + branch survive, no successful removal) AND the
   delivery half (`git -C <server> rev-parse --verify <branch>`), never a struct
   check. The dirty guard itself (`finishCleanupWorktreeDirtyReason`) is
   untouched, exactly as §3 required.
2. `TestServerRecycleDirtyWorktreeSkipped` — **reworded to name its arm**: the
   server recycle has no push step at all, so "nothing may run" is true OF THAT
   ARM; the old sentence was the client arm's word for word and taught the false
   half inside a server test.
3. `TestFinishCleanupPlanKeepsEveryGuard` — **same split on the plan path**: the
   refusal plan predicts NO removal and STILL predicts the push. This required a
   production detail: the plan renders the removal reason as its own marked row,
   so the reason is computed against a throwaway report (otherwise the
   untracked-residue note and the marked row printed the same step twice).
4. `TestFinishCleanupClientDirtyStillDelivers` (`internal/tools/is_server_node_test.go`)
   — the MISSING ROLE×STATE cell, un-skipped: §7.3's recorded red run is now the
   green proof, and `TestAssertionTotalityAuditGateClassifiesTheLiveTree`'s
   witness key is re-pointed to the server arm's surviving negation (the file's
   own precedent for a re-pointed key), because the client assertion no longer
   makes a negative-totality claim.

## Not done (honest)

- The server arm (`finishCleanupServerRecycle`) is untouched: it has no push, so
  the defect does not exist there.
- `devdoc/hp-push-diagnosis-20260916.md` A′ names the same defect; whether the
  strand was *also* mitigated by anything outside this chain was not re-audited.
- A failed premerge/push still records only a report step plus the
  removal-not-reached sentence; adding a `task_events` row for it is a separate
  change (named in the report's `## Not Done`).

## Verification recipe (reusable)

Real two-node git world, asserted on git's own state, never the report:

1. bare "server" + client repo + `git worktree add` — the `finishCleanupFixture`
   in `internal/tools/task_finish_cleanup_test.go`;
2. drive `runFinishCleanupChainTarget(ctx, &FinishCleanupReport{}, deliverable, serverDir, false)`;
3. assert `git -C <server> rev-parse refs/heads/<branch>`, `merge-base --is-ancestor`
   for alignment, and the surviving file/dir for the refusal;
4. red-proof: `git stash push` the two production files, keep the test file, and
   confirm the delivery assertions fail with `step push missing from report`.
