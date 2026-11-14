<!-- doc-class: historical -->
# The producer pass completes the delivery chain — 2026-10-17

census: whole-project figures below come from `git ls-files`/`gopi task list` at
this branch's base commit (2c56271fb); the row measurements are narrative evidence
of ONE run and are dated.

## The defect, measured

`ReclaimOwnFinishedWorktrees` (the producer-side release, `internal/tools/task_reclaim_self.go`)
freed the linked worktree of a row *this process* had finished and stopped there. Its own
success line says it: `own worktree released: <branch> (<id>) freed for merge; branch kept`.

That sentence is the **server's** reasoning. A server keeps a branch because the merger's
input is a branch. On a **client** with a declared peer the same sentence is a promise
nobody keeps: the ref is never pushed, and no pass is going to reap it — a single-shot
close-out that skipped is not re-run.

Measured on this host, pre-state captured at `/home/user/.gopi/sessions/01a1473ba979701f9642dc6228a9cfa0/artifacts/pre-state.txt`:

| row | local ref | on peer (peerbox) |
|---|---|---|
| `01a1492f` | fea40f48e | **absent** |
| `01a1494d` | 18b2ace7a | **absent** |
| `01a149e8` | b3094326e | **absent** |

All three rows were `completed`, their worktrees already reclaimed by this very arm, and
the removal-audit sink carried a `task-self-reclaim` worktree-removal record for each —
i.e. the arm ran, decided the *directory* was its business and the *branch* was not.

## What changed

`reclaimSelfDeliveryLine` (`internal/tools/task_reclaim_self_delivery.go`) asks the ONE
close-out chain for the terminal row while its ground is still on disk:

> **RETIRED 2026-10-31 (task 01a19052).** `task_reclaim_self_delivery.go`
> declared nothing but the two marker strings `reclaimSelfDeliveryLine` renders;
> the probe found it DEAD-ALONE with zero production references (its only
> consumers were two test files), so the file, its test, and the helper itself
> were deleted together. Recovery:
> `git show c49dc227:internal/tools/task_reclaim_self_delivery.go` and
> `git show c49dc227:internal/tools/finish_cleanup_moved_helpers_test.go`.

```
① branch clean   — already proven: the disposition refused any dirt, any live holder
                    and any ground the checklist does not own
② push to peer   ┐
③ remove worktree ├ TaskFinishCleanupSessionID — the same implementation the
④ git branch -D  ┘ operator's `gopi task finish-cleanup --apply` runs
```

Then, in `task_reclaim_self.go`:

* chain **collected** the directory → the pass returns ONE line carrying the chain's own
  verdict (`delivered by this close-out: …`), and does not remove anything itself;
* chain **did not** collect it (its own refusal: dirty main checkout, live foreign holder,
  a push that did not land) → the branch is kept, the refusal is named, and the pass falls
  back to its own guarded removal so the directory is not stranded;
* row **not terminal** → the chain is not asked at all (the pass states its own refusal in
  its own vocabulary; a running round must not print a close-out refusal it never asked for).

### Why delegate rather than add a push here

A second push path would be a second set of gates — target resolution
(`peerRemoteTargetFor`), the carried-tip guard, the push-standing axis (`OK:true` from
this chain's own push is what licenses `-D`) and the orphan guard are *decisions of the
chain*. A copy drifts on the first change (Rule 14). The producer pass contributes the one
fact the chain cannot know — "the ground I am about to release holds no work" — and the
chain contributes everything else.

### Deadlock check (this is why the chain, not the CLI face)

`gopi task finish-cleanup --apply` takes the serve lock at the **CLI** layer
(`cmd/agent/cli_tasks.go`); `TaskFinishCleanupSessionID` in `internal/tools` does not. The
pass calls the tools function, so a worker round cannot deadlock against its own serve lock
(the failure mode row `01a149e8` was filed for).

## What did NOT change, on purpose

* **Rule 30** stands: a dirty ground is refused by name and its work kept (test ③). The
  chain's rescue lane still records residue into `refs/gopi/residue/*` before any removal.
* **`-D` keeps its single audited writer**: `removalaudit.BranchDeleteAt` under
  `ArmFinishCleanupBranchForce`, licensed by *this chain's own* push `OK:true` plus the
  orphan guard. The pass never spells `git branch -D` itself.
* **No `-d`/"fully merged" precondition** anywhere on the delete path: the force arm deletes
  a ref that is deliberately *not* merged locally — its standing proof is the delivery
  point, not a local merge.
* **Zero new dependencies** (Rule 35): no state file, no lock, no timer. The carrier is the
  pass that already runs at every close-out; the durable half of the outcome is the chain's
  own audit record.

## Verification

* New repro tests (`internal/tools/task_reclaim_self_delivery_test.go`) drive the REAL pass
  against a real repo, a real **bare peer configured as the declared peer**, a real ledger
  row and a real removal-audit sink:
  * ① success → directory gone, local ref gone, peer holds the tip, one
    `finish-cleanup-branch-force` record naming the row and the tip;
  * ② push fails → ref untouched, peer holds nothing, pass line names the address git could
    not reach;
  * ③ dirty ground → refused by name, file survives, ref untouched, peer untouched;
  * ④ injected chain error → a named sentence, never silence.
* The three real rows, live, through the chain (this is the same implementation the pass now
  calls): `pre-state.txt` (before) vs `post-state.txt` (after) in this run's session
  `artifacts/`.

## Known gap (honest)

No *live worker round* delivered a fresh branch in this session: the pass's own line was
exercised end-to-end by the test above (real git, real peer, real ledger, real sink) and the
chain it delegates to was exercised live on three real rows — but the composition had not
been watched on a brand-new row at the time of writing.
