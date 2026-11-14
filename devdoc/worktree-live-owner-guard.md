<!-- doc-class: historical -->
# Task Report: a finish-cleanup sweep must never collect a worktree a LIVE ledger row works in

**Branch**: `feat/worktree-live-owner-guard` · **Worktree**: `~/workspace/gopi-liveowner`
**Date**: 2026-09-12 · **Epic**: a sweep can remove a worktree (and delete its branch ref)
belonging to a STILL-RUNNING session whose task row is live, destroying the owner's working
tree mid-run.

## Completed

### 0. Scope of THIS change (the epic's second half)

The worktree gate below landed first (`e1c696f9` + `c2e51a9b`). This round adds
the epic's remaining deliverable — **the branch-deletion guard** — so step ④
never `-D`-deletes a ref this sweep did not deliver, and never deletes a ref a
LIVE ledger row still works through. Both halves read ONE loader
(`finishCleanupLiveClaims`), so they cannot disagree on what "live" means.

### 1. The hole, found by being the victim (the 06:05:4x incident)

This session created its Rule-6 worktree (`git worktree add ~/workspace/gopi-liveowner -b
feat/worktree-live-owner-guard`, row `01a09258-5d29-71a5-a361-b77aabe5fe41`, status
`in_progress`, claimed `peerbox:463445`) and, ~4 minutes later, **both the directory and the
branch ref were gone**:

| probe at t+261s | result |
|---|---|
| `ls -d ~/workspace/gopi-liveowner` | `No such file or directory` |
| `git rev-parse --verify feat/worktree-live-owner-guard` | unknown revision — **ref deleted, not merely unchecked-out** |
| `git reflog show feat/worktree-live-owner-guard` | unknown revision — **reflog pruned too** |
| `git worktree list \| grep liveowner` | gone from the list |
| `ls .git/worktrees/` (main repo) | `gopi-liveowner` **still present** — i.e. the admin entry and the directory did not go together |

The row's own `deliverable.worktree` had been correctly stamped at creation by the executor
backfill seam (`task_deliverable_worktree.go` — "task 01a09258…: deliverable.worktree
backfilled to /home/user/workspace/gopi-liveowner"). It was stamped and still removed.

Recovery note (honest): the work survived only because three edits had landed in the MAIN
repo — the paths in my `edit` calls were still `~/work/GoPi/...` after creating the worktree.
That was the Rule-6b trap on my side. They were backed up, restored out of the main repo, and
re-applied in a fresh worktree at master tip `51323ed5`; the main repo was verified clean of
them. Nothing was lost, but the margin was luck, not design.

### 2. Which guard could have fired (and why the candidate survived anyway)

The reachable, branch-shaped actor is `fix/wip-worktree-delete-guard` (sibling session,
worktree `~/workspace/gopi-provguard`), whose devdoc documents **exactly this victim shape**:
a pair whose member is "a brand-new worktree the session is still setting up" — deleted
because **a branch created at the integration tip is trivially an ancestor of it** (measured
there: `merge-base --is-ancestor` exit 0, `git branch --merged master` lists it, `worktree
remove` exit 0, then `branch -d` succeeds once the checkout is gone). Master's guard ① does
not exclude the integration branch's own tip; guard ⑤ (live-row) only *skips* the pass.

This epic asked for the complementary, **ledger-side** statement. Reviewing the two removal
paths on master `51323ed5` against a live row:

| removal path | pre-existing live-row protection | gap |
|---|---|---|
| `MergedCleanupSweep` (`task_merged_cleanup.go`) | ⑤ live-branch **and** live-worktree sets (`mergedCleanupLiveBranches`), plus ⑤b live-CWD, ⑤c foreign owner | **none found** — its live-worktree set already carries `deliverable.worktree` for live rows. (The older sibling devdoc's "no live-row gate" note predates the bugfix epic 2026-09-03 closure that added it.) |
| finish-cleanup arms (`task_finish_cleanup.go`) | row must be TERMINAL; ownership gates compare **recorded worktree PATHS**; process-CWD liveness guard | **the hole**: both ownership gates are path-based, so a LIVE row that has not recorded `deliverable.worktree` yet is invisible to them — see §3 |

### 3. The gap, stated exactly

A freshly claimed row carries `{"repo":…,"branch":…}` and only gets `deliverable.worktree` on
its first **git-touching** round (`TaskDeliverableWorktreeBash`'s `needle` pre-filter skips
pure-edit rounds). Until then:

- `finishCleanupForeignOwnerReason` — compares recorded **paths** → sees nothing;
- `finishCleanupWorktreeInUse` — needs an observable live process CWD (not in the shapes this
  epic describes: a same-session holder, or a `/proc` that cannot be scanned degrades to "no
  holder observed");
- the merged sweep's ⑤ answers on the **branch** axis (which does catch it), but the
  finish-cleanup arms never asked that axis at all.

So a terminal row's close-out for the *same path* collects the directory while a live row is
working in it — and the chain's branch step runs `branch -d`, falling back to `branch -D`
*after a successful push* (`task_finish_cleanup.go` step ④), which deletes the live owner's
branch ref. Both halves of the epic's damage (tree + ref) follow from this one gap.

### 4. The fix — a ledger-liveness gate on the finish-cleanup removal paths

- `taskLiveClaims` / `taskLoadLiveClaims` (`task_finish_cleanup.go`) — **one** loader of the
  live side of the ledger for BOTH cleanup paths, classified through `isLiveTaskStatus`, so
  the two can never drift on what "live" means (Rule 14).
- `finishCleanupLiveOwnerGate(ctx, worktree, selfTaskID)` — the gate. Two axes:
  1. the **worktree path** a live row recorded (`deliverable.worktree`), and
  2. the **branch checked out in the candidate directory** (`finishCleanupWorktreeBranch`,
     detached HEAD = no branch axis) matched against a live row's `deliverable.branch`.
     Axis 2 is the one that catches the just-claimed row above.
  Attached to both arms: step ③ of the client chain and the server/single-node recycle.
- **Ordering**: placed AFTER the push (client arm) exactly like the pre-existing ownership
  gate — a refused removal must still leave the branch delivered.
- **Fail-soft direction**: an unreadable ledger **refuses** ("ledger unreadable"), because
  this gate exists to protect a live session — report AND keep, never report AND destroy.
  The skip is a reported reason, never a crash (the epic's fail-soft constraint).
- **Self-exclusion** (`excludeTaskID`): a caller's own live claim never blocks its own
  cleanup; the server arm passes the row being cleaned, so existing behaviour is preserved.
- **Observability**: the serve sweep used to drop every skip silently (`if rep.Skipped {
  continue }`), which would have made this guard invisible in production. `serveFinishCleanupSweep`
  now surfaces the liveness-refusal lines (`TaskFinishCleanupSkipIsLiveOwner`), so the exact
  situation the epic is about — a cleanup refused because a live session's ground was at
  stake — is visible in the serve log instead of silent.
- `isLiveTaskStatus` is now the single definition of "live", and the merged sweep's
  `mergedCleanupLiveBranches` reads every row and classifies through it (Rule 14/13), instead
  of an inline SQL status list that could drift from the Go constants.

### 5. The branch-deletion guard (deliverable 2 — this round)

Step ④'s old shape: try `branch -d`, and on failure fall straight through to
`branch -D`. `-D` **overrides git's own "not fully merged" verdict** — and the
only thing justifying it was a comment claiming the push had landed. Nothing
checked. A sweep that delivered nothing (a push step that failed and was
reported as `OK:false`) still force-deleted the ref; and nothing at all asked
whether the ref belonged to a live session.

Two isolated predicates now decide, each with its own marker constant:

| guard | question | refusal |
|---|---|---|
| `finishCleanupBranchLiveOwnerReason` (axis 1, whole step) | does a LIVE row (pending/claimed/in_progress) record this branch? | `finishCleanupBranchLiveOwnerMarker` + owner row/status/holder |
| `finishCleanupBranchForceDeleteReason(delivered)` (axis 2, `-D` half only) | did THIS sweep's push land? | `finishCleanupBranchNotDeliveredMarker` — the ref survives |
| `finishCleanupStepSucceeded(rep, worktree)` (pair integrity) | did the sweep collect the pair's directory? | the branch step reports and returns — the pair stays together |

- The live-owner axis is asked for **the step**, not just the `-D` half: a
  `-d` that would have succeeded on a merged ref a live row is working through
  is still a live session's ground being deleted.
- The delivery axis is asked only for the **`-D` half**, because `-d` is the
  safe half — refusing it would break the pre-existing preserve behaviour for
  a caller's own run.
- The pair-integrity check makes the chain sound rather than merely lucky: the
  two removal arms happen to `return` as soon as the worktree step is not OK,
  so reaching step ④ usually implies the worktree went. That implication is
  now ENFORCED, so a future third call site cannot append a refused worktree
  step and fall through to a ref deletion.
- `TaskFinishCleanupSkipIsLiveOwner` recognises **both** markers (worktree and
  branch), so the serve sweep surfaces either refusal instead of swallowing
  it. New constants: `finishCleanupBranchLiveOwnerMarker`,
  `finishCleanupBranchNotDeliveredMarker`, `finishCleanupPushVerb`,
  `finishCleanupBranchVerb`, `finishCleanupBranchDeleteFlag` (`-d`),
  `finishCleanupBranchForceDeleteFlag` (`-D`) — the bare `"push"`/`"branch"`/
  `"-d"`/`"-D"` literals that lived at the call sites are now referenced
  (Rules 1b/13).

### Deliberately NOT done (honest)

A **distinct-merger-sweep** guard (protect a worktree whose checked-out branch has no
ledger claim) was designed in the sibling branch's lane and **dropped** on the evidence in §2: the
live-claims evidence that a branch-at-tip with a live row is a WIP worktree is already
available on the merged path (⑤'s branch axis), the sibling's guard ①b covers the
reflog/claim discriminator more precisely, and no actor could be tied to the 06:05 removal on
that path. Adding an unproven second guard there would enlarge the recycle lane's refusal
surface without evidence.

### Also honest about the sibling branch

`fix/wip-worktree-delete-guard` / `~/workspace/gopi-provguard` is a **live, unmerged** sibling
branch (its worktree is present) covering the adjacent guard-①b discriminator. This work does
not delete, merge, or absorb it, and the two fixes are complementary (ledger axis vs reflog
axis).

## Verification

- Build: `go build ./...` — OK; `gofmt -l internal cmd` reports nothing.
- New regression (the epic's acceptance test):
  `TestFinishCleanupChainSkipsLiveOwnerWorktree` — a terminal row's chain runs
  with a LIVE row (`in_progress`, host pid stamp) recording the **branch** but no worktree path
  → push lands, the worktree step is refused with `finishCleanupLiveOwnerMarker` + the owner
  row id, and **the tree and the branch ref both still exist**.
- Branch half, end to end: `TestFinishCleanupChainSparesLiveOwnerBranch` — a LIVE
  (`claimed`, live pid) row on the swept branch → push lands, the removal step is
  refused, and **tree AND ref survive**. (Honest: gate ① binds the swept branch to
  the candidate directory's checked-out branch, so the step-③ lock fires first in the
  client chain; the step-④ lock is therefore pinned separately — a test that asserted
  "the branch step refused" here would be asserting a lock that never gets to run.)
- Branch gate, in isolation: `TestFinishCleanupBranchLiveOwnerReasonRefuses` — a live
  row's branch refuses, naming row + status + live holder pid; a row never blocks its
  own cleanup; an unclaimed branch stays deletable; an unreadable ledger refuses.
- `-D` standing: `TestFinishCleanupBranchForceDeleteNeedsDelivery` — undelivered
  refuses (marker), delivered is inert, and only an `OK:true` push step in the report
  grants the standing (nil report / non-OK step grant nothing).
- Gate edges: `TestFinishCleanupLiveOwnerGateEdges` — the loader keeps only live rows (a
  terminal row is not a live claim), self-exclusion, empty inputs are never a vacuous match,
  and an unreadable ledger refuses with a reported reason.
- **Red-proof, worktree half** (revert-the-fix must FAIL): with the two
  `finishCleanupLiveOwnerGate` call sites neutered,
  `TestFinishCleanupChainSkipsLiveOwnerWorktree` and
  `TestFinishCleanupChainSparesLiveOwnerBranch` both FAIL with
  `a live row's worktree must not be removed: {Step:worktree … OK:true}` — the pre-fix
  behaviour really does remove the live owner's tree.
- **Red-proof, branch half**: with `finishCleanupBranchLiveOwnerReason` and
  `finishCleanupBranchForceDeleteReason` short-circuited to `""`,
  `TestFinishCleanupBranchLiveOwnerReasonRefuses` FAILS (`a live row's branch must
  refuse the deletion, got ""`) and `TestFinishCleanupBranchForceDeleteNeedsDelivery`
  FAILS (`an undelivered branch must refuse a force-delete …, got ""`). Both were
  restored and re-verified green afterwards (`git diff` = the intended change only).
- Package suites: `go test -count=1 ./internal/tools/ ./internal/standards/` →
  `ok github.com/nick/gopi/internal/tools 211.033s` and
  `ok github.com/nick/gopi/internal/standards 4.426s` (the standards package is
  the Rules 16–21 machine gate; `./internal/stylegate/` also run for this
  change: `ok`).

### Independent red-proof re-run (no self-report taken on faith)

The red-proofs above were re-executed from a clean tree (`git status --porcelain` empty)
using a neuter that is safer than deleting the gate body — a synthetic `return ""` on the
first line of the gate, so no variable is left unused and the compiler cannot fail for an
unrelated reason:

```
# per predicate/path: back up, insert `if true { return "" }` after the opening brace
cp internal/tools/task_finish_cleanup.go /tmp/fc-verify.bak
go test ./internal/tools/ -run <the four named tests>
cp /tmp/fc-verify.bak internal/tools/task_finish_cleanup.go
grep -c 'if true {' internal/tools/task_finish_cleanup.go   # 0 = restored
git status --porcelain                                      # empty = restored
```

Observed failures (each test really is bound to its gate, and to nothing else):

| neutered | test | observed |
|---|---|---|
| `finishCleanupLiveOwnerGate` | `TestFinishCleanupChainSkipsLiveOwnerWorktree` | FAIL `a live row's worktree must not be removed: {Step:worktree … OK:true}` |
| `finishCleanupLiveOwnerGate` | `TestFinishCleanupChainSparesLiveOwnerBranch` | FAIL, same message |
| `finishCleanupBranchLiveOwnerReason` | `TestFinishCleanupBranchLiveOwnerReasonRefuses` | FAIL `a live row's branch must refuse the deletion, got ""` |
| `finishCleanupBranchForceDeleteReason` | `TestFinishCleanupBranchForceDeleteNeedsDelivery` | FAIL `an undelivered branch must refuse a force-delete with a reported reason, got ""` |

`go build ./...` and `gofmt -l internal cmd` were green on the restored tree.

### Final clean-suite run on the frozen tree (head `a9d47260`)

`go test -count=1 -timeout=1200s ./internal/tools/ ./internal/standards/` with the
working tree clean and **no other test run of mine in flight**:

```
ok  	github.com/nick/gopi/internal/tools	1062.022s
ok  	github.com/nick/gopi/internal/standards	48.880s
exit=0
```

**Pitfall worth recording (this bit me):** an earlier run of the same command FAILED as
`FAIL github.com/nick/gopi/internal/tools 600.059s` with a `panic: test timed out` dump whose
stack was only parked `database/sql.connectionOpener` goroutines. That was **not** a hang in
this change — the machine was at load average ~26–31 with four other gopi sessions running
`go test ./...` over the same package concurrently, and this package's wall time went from
the 211s recorded earlier to 1062s. The default 10m per-package timeout is a *wall-clock*
budget, so on a loaded box it fires on a package that is merely slow, and the truncation
makes it look like a deadlock. Diagnosis order for a `tools.test` timeout: `uptime` +
`ps -eo pid,pcpu,args | grep tools.test` first (count the competing suites), then re-run
clean with an explicit `-timeout`. Do not "fix" a lock that is not there.

### Honest note on the in_progress-vs-terminal gate (epic deliverable 1, wording)

The gate refuses on **live** rows (`pending`/`claimed`/`in_progress`) — `isLiveTaskStatus`
is the single definition, so the epic's "in_progress/claimed" is the strict subset that
matters in practice: only `claimed`/`in_progress` rows hold a live pid, and the gate never
narrows below a live row. Conversely the finish-cleanup arms themselves still require a
**terminal** row (`finishCleanupTerminalStatuses`) — the two ends of the chain are stated as
different predicates on purpose: "who may be swept" (terminal) vs "whose ground is protected"
(live), and neither is derived from the other.

## Commit

All on branch `feat/worktree-live-owner-guard`, worktree `~/workspace/gopi-liveowner`
(Rule 6a — the main repo was never developed in):

1. `e1c696f9 fix(cleanup): never collect a worktree a LIVE ledger row works in` — worktree half.
2. `c2e51a9b refactor(cleanup): one live-status list for both the predicate and the SQL filter`.
3. `287a831c fix(cleanup): never delete a branch the sweep did not deliver or a live row works through` — branch half (deliverable 2).
4. `f879a2c1 docs(cleanup): record the branch-guard half, red-proofs and green suite results` — this report.

Recovery: `git -C ~/work/GoPi show feat/worktree-live-owner-guard:devdoc/worktree-live-owner-guard.md`
(the branch lives in the shared object store of the main repo — a linked worktree needs no push).
