<!-- doc-class: normative -->
# The close-out's skip line names the steps that DID run

**Date:** 2026-10-12 · **Task:** `01a12dc4-a033-713e-bdfe-d463789df21e`
**Surface:** `gopi task finish-cleanup` (plan and apply), the agent's close-out log line,
and the console's close-out signal.

## The defect, measured

The one line an operator reads at the close-out printed a SKIP reason as if it were
the state of the WHOLE chain. A skip reason is ONE step's state — the removal's — so
a run that had just handed the branch over reported itself as a skip. Verbatim, three
times on hosta (2026-10-12), from the binary rebuilt out of this task's worktree:

```
01a12e98-c1cd-7042-a64a-a6726187ce16 skipped: removal skipped (delivery already attempted):
the recorded worktree directory is not there — nothing to remove
(git status: chdir /home/user/workspace/gopi-01a12e98c1cd7042a64aa6726187ce16: no such file or directory)
```

and, in the same second, the two facts that contradict it:

```
$ git ls-remote peerbox:~/work/GoPi refs/heads/task/01a12e98c1cd7042a64aa6726187ce16
b2f6a4b64826cf4f71ad7d33c5279dd3148bd36f	refs/heads/task/01a12e98c1cd7042a64aa6726187ce16
$ git rev-parse --verify --quiet refs/heads/task/01a12e98c1cd7042a64aa6726187ce16
(nothing — exit 1)
$ cat artifacts/audit-5.log
… "arm":"finish-cleanup-branch" … "delivery_point":"peerbox:~/work/GoPi" …
… "arm":"finish-cleanup-branch-force" … "evidence":"git refused -d and this chain's own push landed the ref (force-delete standing)"
```

So the chain PUSHED the branch, then deleted the local ref, then declined the removal
(the recorded directory was gone) — and the single line it left behind said "skipped".

## Why the line matters more than it looks

`FinishCleanupReportLine` is not a debug print: the same sentence is composed by the
agent's close-out (`finishCleanupRepSummary`), is shown by `gopi task finish-cleanup`
and by `gopi task list`'s close-out signal, and is what an operator (or the console
face) reads to decide whether work still has to be done. Reading "skipped" where the
delivery landed is the difference between closing a row and re-running a chain that
has nothing left to do.

## The fix, and its one boundary

`FinishCleanupSkipStepTail(rep)` renders the steps whose `OK` is true, under a label
that says which arm produced them:

```
… skipped: removal skipped (delivery already attempted): … — steps that DID run in this chain=[align-before-delivery,published-anchors,pre-merge,push,branch]
```

- Both faces call the ONE function (Rule 14): the CLI line and the agent summary can
  no longer disagree about which steps ran.
- Only SUCCESSES are listed: the step that refused is already named by the reason
  itself (its own `Detail`), and printing it twice would say one fact twice.
- The PLAN arm renders NO tail. On a dry run `rep.Steps` holds predictions, and a
  prediction printed under "steps that DID run" would be a claim about a run that has
  not happened (Rule 15); the plan already prints its predictions under its own
  `would run: ` marker.

**The boundary — what this fix deliberately does NOT change.** The literal shape
`done=[push] failed=[worktree: …]` was considered and rejected: `FinishCleanupReportSettled`
and the plan/apply classifier are keyed on `Skipped`, so re-shaping done/failed would
move the settled/would-run verdicts as well. The measurement says the failure is not in
the ACTIONS (the push landed in all four matrix cells) but only in the RENDERING, so the
tail is the change that carries the information without moving a semantic the ledger
and the sweep depend on.

## The regression that would have caught it

`TestFinishCleanupLedgerByGroundMatrix` (`internal/tools/task_finish_cleanup_ledger_matrix_test.go`)
drives the real chain over the four cells of (ledger row) × (ground on disk), asserting
physical truth first (a stat, a rev-parse, and THE PEER'S OWN REF) and the report second.
The cell that was missing from the suite is `completed` + ground GONE — every pre-existing
arm tested the ground PRESENT (the clean-worktree epic cases) or the row ALONE (the
boundary skips), which is why a stranded delivery could print as a skip unnoticed.

Red proof (`artifacts/redproof-matrix.txt`): restoring the pre-fix one-liner fails EXACTLY
that cell, and only that cell —

```
--- FAIL: …/completed_row_+_ground_GONE_→_delivered,_and_the_skip_line_names_the_push
    close-out line "t-matrix skipped: removal skipped (delivery already attempted): …"
    missing " — steps that DID run in this chain=["
```

`TestFinishCleanupSkipLineNamesTheStepsThatRan` pins the four rendering shapes (a landed
push named beside a skip; a plan with no tail; a skip with no successful step carrying no
tail; a non-successful step never claimed as one that ran).
