<!-- doc-class: normative -->
# A Creation-Seam Ground Stamp Must Prove the Ground Is UNOWNED

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic**: non-worker `gopi <verb>` auto rows (borrowed ground) · **Date**: 2026-10-03
**Standing claim about master**: the CREATION seam stamps a ground only after the
same ONE-OWNER proof the CLAIM seam has always run.
**Code**: `internal/tools/task_deliverable_worktree.go` — `taskCreationGroundBorrowed`,
called from `TaskDeliverableWorktreeForRow` and from the created-grounds pass of
`taskDeliverableWorktreeCommandForRow`.

## 1. The defect, measured

Row `01a10025` (session `03e3a087`, pid `611399`) was a bare `gopi replay` run
standing in task `01a10016`'s worktree. Its deliverable, read back from the
ledger:

```
{"branch":"task/01a1001656777ef69fd6a4bbc7f71ca9","repo":"/home/user/work/GoPi",
 "worktree":"/home/user/workspace/gopi-01a1001656777ef69fd6a4bbc7f71ca9"}
```

while row `01a10016` — the run that actually CREATED that worktree — named the
same two values. Two rows, one ground. The borrower's own log records the
moment (`~/.gopi/sessions/03e3a087-6932-438a-a676-e69712123f3b/console.log:19`):

```
[task-notify] task 01a10025-…: deliverable.worktree backfilled to
"/home/user/workspace/gopi-01a1001656777ef69fd6a4bbc7f71ca9"
(branch "task/01a1001656777ef69fd6a4bbc7f71ca9") at creation
```

That is the 2026-09-12 foreign-owner hijack, one seam over. The deliverable is
READ as an ownership claim — `task_reclaim_self.go`'s
`deliverable LIKE '%worktree%'` scan and the worktree sweep's ownership arm both
keep a named directory alive for the row that names it — so the borrowed stamp
makes ownership lie in BOTH directions: the borrower blocks its owner's recycle
and is itself blocked by it.

## 2. Why the existing guards did not catch it

The rule already existed, in ONE of the two stamping seams:

| seam | entry | ONE-OWNER guard |
|---|---|---|
| **claim** (`taskWorktreeStampClaimer`, called by the wrapper before the child spawns) | `task_deliverable_worktree.go` | YES — `taskWorktreeOwnerScan` on the worktree axis, `taskBranchRowOwner` on the branch axis, both **fail-closed** |
| **creation** (`TaskDeliverableWorktreeForRow` ← `internal/agent/loop.go`'s after-bash-round call) | same file | **NO** |

The creation seam's probe, `taskGroundOwningDir`, answers a different question —
*which ground does this directory belong to* — and never *does another row
already own it*. So the seam that sees every bare-prompt/auto run's ground was
the one place the ownership rule was missing.

## 3. The repair

One helper, two axes, the claim seam's own two scans (Rules 13/14 — the rule has
one home per axis, so the two seams cannot disagree):

```
taskCreationGroundBorrowed(taskID, repo, wt, branch) string   // "" = this row may record it
```

* worktree axis → `taskWorktreeOwnerScan` (package-level seam over
  `taskWorktreeRowOwner`), branch axis → `taskBranchRowOwner`;
* **FAIL-CLOSED** (Rule 15): a scan ERROR is a refusal, never a proceed — "no
  owner could be PROVEN" is not "there is no owner";
* the refusal is REPORTED through the seam's own sink
  (`taskWorktreeBorrowedGroundLineFmt` → `taskNotifyLog`), naming the row and
  the owner — a silent refusal would be the same defect wearing a different hat;
* the refusal withholds only the LEDGER stamp: the run keeps working in the
  directory it stands in, and `TaskDeliverableWorktreeForRow` still returns the
  resolved ground to its caller.

Applied at both of the seam's write paths:

* `TaskDeliverableWorktreeForRow` — the PRIMARY axes (worktree + branch);
* the created-grounds pass of `taskDeliverableWorktreeCommandForRow` — the
  PLURAL axis (`deliverable.grounds`). It needs the same gate for the same
  reason: the pass enumerates the directory a `git worktree add <path>` NAMED,
  whether or not git performed the creation (a failing `git worktree add`
  naming another task's registered worktree still parses).

## 4. What this does NOT do (honest scope)

* It does not stop a non-worker `gopi <verb>` from being registered as an auto
  row, and it does not add a reclaim arm for such rows. Both are separate
  questions with their own evidence; this change closes the OWNERSHIP leak only.
* It does not touch the claim seam, whose guards already held.

## Verification

* `go build ./...` — clean.
* `go test ./internal/tools/ -run TestCreationSeam -count=1` — RED before the
  fix (the borrower's blob gained the owner's worktree+branch), GREEN after.
* `make test-budget PKGS='./internal/tools'` — the package's own suite.

See `internal/tools/task_deliverable_borrowed_ground_test.go` for the three
acceptance arms (borrowed worktree refused, borrowed branch refused, OWN ground
still recorded).
