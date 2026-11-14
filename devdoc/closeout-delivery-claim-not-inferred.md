<!-- doc-class: historical -->
# A close-out refusal states the delivery the ledger recorded — never one inferred from the directory

**Row:** `01a130c6-d8c8-7a78-bae7-a546c660523f` (epic; row family `01a1307a`)
**Baseline:** `9148500b8` · **Branch:** `task/01a130c6d8c87a78bae7a546c660523f`

## The defect

The close-out chain's refusal reason for a row whose **recorded worktree directory
is gone** read:

> `removal skipped (delivery already attempted): the recorded worktree directory is
> not there — nothing to remove (git status: chdir …: no such file or directory)`

The phrase *delivery already attempted* is a claim about the **delivery**, drawn
from a fact about the **directory** (it is absent). The two are not the same fact,
and the row that carried this reason had **no delivery event anywhere on its
trail** — `[completed align-unverified premerge-unverified
finish-cleanup-removal-refused]`. An operator reading the refusal concludes the
branch is on the peer and stops looking for work that never left the host.

The ledger, meanwhile, was asked the question directly: `git ls-remote` against
the peer held **no ref** for that branch.

## The fix

The reason now states the **delivery half as the report measured it**, in one
composer every arm reads (`finishCleanupRemovalReasonWithDelivery`,
`internal/tools/task_finish_cleanup.go`), so the plan arm and the apply arm cannot
disagree about the shape:

| what this run measured | what the reason says |
|---|---|
| its push step was refused | `delivery owed: … ` + *this close-out's own push step was refused* |
| its push step succeeded | `… ` + *the delivery IS established, by this close-out's own push step* |
| the local ref was already absent | `… ` + *no delivery was owed by this pass (the local branch was already absent)* |
| nothing executed (plan) / owed | `delivery owed: … ` + *the delivery half has not run for this row* |
| no evidence in either direction | `delivery not established: …` |

The marker's **head** also carries the honest clause
(`finishCleanupDeliveryClaimedPrefix = "delivery not established"`), replacing
`delivery already attempted` — which is **retired**, and a test asserts it is
emitted nowhere in production code.

### Two details the change had to respect

1. **The list clip is a budget, not a nuisance.** `gopi task list` clips a row's
   reason at 60 runes (`taskListCloseoutReasonLimit`) and `gopi task list` does not
   render a row's GROUND line for a `completed` row — so the marker's head had to
   *be* the directory fact within that clip. The "removal skipped:" prefix was
   dropped (the row's own verdict token already says `skipped`) and
   `finishCleanupGoneWorktreeDirAbsentHead` is its own named constant, so no
   reader slices the marker by index.
2. **A single-node host owes no delivery.** When `sync_peers` is empty the chain
   appends its own no-peer note; the composer was initially wrong here (it said
   "delivery owed" beside "this branch is NOT handed off to any server"), and the
   real-binary run below is what caught it.

## Evidence

```
$ GOPI_MEMORY_DB=/dev/shm/…/ledger.db GOPI_SYNC_PEERS=probe-peer ./bin/gopi \
      task finish-cleanup --task 01a130c6-d8c8-7a78-bae7-a546c660523f
  01a130c6-… completed  branch=task/01a130c6… worktree=/dev/shm/…/wt — delivery owed:
  the recorded worktree directory is not there — nothing to remove (delivery not
  established; git status: chdir /dev/shm/…/wt: no such file or directory) — the
  local branch IS still here — the delivery half has not run for this row, and the
  absent directory is not evidence that it did; …
finish-cleanup (dry-run): candidates=1 would-run=1 settled=0 refused=0 — pass --apply to run the chain
```

The peer held **no** `refs/heads/task/01a130c6…` in that world, and the row reads
`would-run=1 settled=0` — the settled reading is refused for a row that owes the
act, which is the classifier half this reason feeds.

## Regression guard

`internal/tools/task_finish_cleanup_gone_delivery_claim_test.go` (red proof; every
symbol it uses predates the change, so it fails on **assertions**, not on a build
error) and `internal/tools/task_finish_cleanup_gone_delivery_wording_test.go` (the
vocabulary pin + the composer's five shapes). Both drive the **real** close-out
chain over a real two-node git world (bare peer + client + linked worktree).

The retired wording is also gone from the **fixtures** that quoted it by hand — the
three `internal/tools` files above plus `internal/agent/taskregister_closeout_skip_tail_test.go`
(the AGENT face's own close-out summary test, which had pinned the phrase as a
sample skip reason); a repo-wide sweep now finds the phrase only inside
`task_finish_cleanup.go`'s own comments, which exist to describe what was retired.

Red proof, measured — in a THROWAWAY tree (`git archive HEAD | tar -x -C <sb>`), not by
mutating the run's own worktree: the production file reverted to
`9148500b8`, the wording pin held aside, and the four test files this branch
MODIFIED **inside `internal/tools/`** restored from `9148500b8` as well (they
reference symbols the baseline
does not define, so a run that skips this step fails to COMPILE and proves
nothing). Run
`go test ./internal/tools/ -run TestGoneWorktreeRefusalDoesNotClaimADeliveryTheLedgerNeverRecorded`
→ `--- FAIL: … task_finish_cleanup_gone_delivery_claim_test.go:99: the refusal
asserts a delivery outcome no ledger event records:` followed by the pre-fix
reason, exit 1; restore verified with `git hash-object <path>` against
`git rev-parse HEAD:<path>` (five paths MATCH, `git diff HEAD -- <production
file>` = 0 lines), then the same test → `ok`.

## Not done / honest limits

- The other emitters of a close-out reason (owner-mismatch, branch-absent) were left
  stating their own conditions; the delivery composer is wired to the three
  gone-directory arms and the reclaim arm only.
- The `refused=3` shape the host's ledger holds was not re-run end to end on the
  real ledger (the seeded probe covered the plan + executed-chain shapes).
