<!-- doc-class: historical -->
# A gone worktree directory wedges close-out step ④ — measured, and fixed

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Measured 2026-10-13** on hosta, from the row
`01a1307a-b066-78d9-a849-b80fb80e111b` (bug: `gopi worker --resume` cannot
recover a row whose close-out was interrupted).

## The symptom, as the operator sees it

A row reads `completed` and carries no marker, while its branch is still on the
host and on no peer ref. Later it wears `[closeout-refused]` with:

```
removal skipped (delivery already attempted): the recorded worktree directory is
not there — nothing to remove (git status: chdir <gone path>: no such file or directory)
```

## What is NOT the defect (measured, so nobody re-fixes it)

Both halves of the epic's presupposition turned out to be working:

1. **Detection works.** `gopi task branch-refs --peer … --only withheld`
   reports both the registered-worktree and the gone-directory state as
   `withheld` / `actionable: true` and exits **2**. On the live ledger it prints
   `0 withheld / 374 unresolved`, and that is correct rather than a miss: the
   only local `task/*` branches belong to `in_progress` rows, and every terminal
   row's work is either an ancestor of peer master or genuinely gone.
2. **Delivery works.** The push performed in every state measured — fixture and
   real binary — and the peer received the row's own tip. The `[closeout-refused]`
   marker is also already a **state, not a scar**: a row refused once and
   delivered on a later pass ends with the refusal as a non-newest event and the
   marker clears.

So the three incident rows in the original briefing (`01a1300b`, `01a13019`,
`01a1301b`) had already been delivered by a later pass — their tips are
`git merge-base --is-ancestor` **true** against peer master.

## The defect that DID reproduce

A worktree whose **directory is gone** leaves a **registration** behind. Git
keeps naming the vanished path in `git worktree list --porcelain`, and

```
error: cannot delete branch 'task/<id>' used by worktree at '<gone path>'
```

**for ever.** The removal arm correctly declines to `worktree remove` a directory
that is not there (git refuses such an entry anyway — exit 128, "is not a working
tree"), so nothing in the close-out ever drops the registration, and **step ④ can
never succeed**. The ref outlives its own delivery, permanently, while the report
reads like a benign no-op.

This is the same defect the reclaim arm already repairs for its own path
(`internal/tools/task_reclaim.go` → `reclaimRepairRegistration`, whose comment
records the identical consequence: *"a gone-but-registered worktree keeps
appearing there until it is pruned — so a vanished directory used to withhold its
own branch from merging forever"*). The close-out chain simply never ran it.

## The fix

`internal/tools/task_finish_cleanup.go`, branch-delete arm: run
`git worktree prune -v` **before** attempting `git branch -d`, through one new
helper `finishCleanupPruneStaleWorktrees`, and record the reading as its own
report step (`finishCleanupStaleRegistrationStep = "stale-registration"`).

**Why it cannot be a guard bypass.** `git worktree prune` takes no path and drops
only registrations whose directory is MISSING. No live worktree, no ground
holding uncommitted work and no foreign row's binding can be affected — the three
removal guards (liveness, foreign owner, ledger liveness) are not weakened,
because there is no directory here for them to protect. It runs on the APPLY path
only, after the plan arm returns, so a dry run's forecast is unchanged.

**Why the reading is printed either way (Rule 15).** `prune -v` prints one line
per entry dropped and *nothing* when it held none — so an empty output is the
evidence that a later delete refusal has some other cause than a
vanished-directory registry entry, instead of leaving a reader to guess.

## Verified

- Repro test: `TestFinishCleanupGoneWorktreeDeliversAndDeletesTheRef` (deletes the
  worktree directory, runs the chain, judges by the **bare peer repo** and the
  **local ref** — never by the report's prose).
- Red proof: with only the prune *call block* removed (constants and helper kept,
  so the build stays clean) the test fails on exactly the two post-fix
  assertions — *"the local branch … still exists after a close-out whose delivery
  SUCCEEDED"* and the missing `stale-registration` step. Restored and verified by
  content hash, not by an index-relative diff.
- Rule 31: the **real binary** was run —
  `bin/gopi task finish-cleanup --task t-e2e-gone --apply` against a two-node
  world with the worktree directory deleted. Pre-fix: peer holds the tip, local
  ref **still present**. Post-fix: peer holds the tip, local ref **empty**.

## Still open (filed, not fixed here)

The refusal reason's parenthetical *"delivery already attempted"* is an inference
about the DELIVERY drawn from a fact about the DIRECTORY, and it ships even when
the row's trail carries no `delivery-verified` event. The existing
`finishCleanupGoneWorktreeDeliveryOwedReason` prefixes the owed wording only in
the direction where the delivery is *known to be owed*; the "not known to have
happened" direction still reads optimistic. Filed as its own bug row.
