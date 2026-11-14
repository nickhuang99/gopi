<!-- doc-class: historical -->
# The completed row whose branch resolves to no ref — and the record nobody read

Task `01a13088-9734-78cc-b58e-f69bf97d10bf` · 2026-10-12 · measured on hosta.

## The shape

Two rows read `completed`. Their `deliverable.branch` —
`task/01a12ff2a6e07f33bc60722ee50ecfb8` and `…a6a87af8a664c1c51dc7676c` —
resolves to **no ref**: no `refs/heads/…`, no `refs/tags/archive/…`, no
`refs/gopi/archive-reason/…`, and no reachable commit names either row. The work
survived only because a briefing happened to re-state its raw tip hash.

## The mechanism (measured, from the removal-audit sink)

`~/.gopi/logs/removal-audit.log`, both branches, in order:

```
12:25:33  worktree-ground    task-ground-anchor         pid 1004143  ground taken
13:19:19  worktree-removal   task-self-own-sweep        pid 1004143  unowned registration of this run's own repo
13:20:04  branch-delete      finish-cleanup-branch      pid 3656808  branch-liveness gate did not refuse
13:20:12  branch-delete      finish-cleanup-branch-force pid 3656808  (5 rows, no sha, no delivery_point)
```

Emitted at `internal/tools/task_finish_cleanup.go:4809` (`-d`) and `:4838`
(`-D`), fed by the ground removal in `run_ground_anchor.go:96` / `:605`.

**Read the order.** The run's own ground was swept first, on the ground of
"unowned registration of this run's own repo" — the sweep's checklist
(`reclaim_ground_checklist.go`) reported *own ground + integration lineage*, and
the live-session veto did not fire. With the worktree gone, the delivery chain's
branch step then asked its local questions — is the worktree there? is the
branch there? — and got "nothing to remove" for both. It deleted the ref anyway
(`-d`, then `-D` when `-d` refused), and its own record says why it felt safe:
*no live row owes this branch*. The row that DID claim it was `completed`
already, and "completed" was the premise, not a condition.

## Why nothing caught it

The removal-audit sink is **write-only**. Three arms remove refs, every removal
is recorded with the arm that decided it and (since a later fix) the tip — and
no surface ever read the record back. So:

* `gopi task branch-refs` reported both kinds of stranded row as one word,
  `unresolved`, whose own text folds the two states together: *"reclaimed
  ground, **or** a branch cleaned up after it landed"*. The question that matters
  — did the work land? — was asked of nothing.
* The rows' worktree/branch block still named a branch that no longer exists, so
  every surface agreed with the row's own claim.

## The fix: split `unresolved` by measurement

`gopi task branch-refs` gained a verdict the pair had no word for:

* **`orphaned`** — no ref here, peer does not advertise it, and no reachable
  commit accounts for the row. The row carries the audit **arm** that deleted the
  ref and the **tip** the record kept.
* **`unresolved`** keeps the ordinary tail and its fail-closed meaning: a row
  whose local refs this run could not read is never adjudicated.

Detection is two unambiguous readings of one `git log --all` index: the merger's
branch-name subject (`Merge branch 'task/<id>' …`) or the FULL task id.

### The two errors this check made before it was right (both measured)

1. **Matching the full id only** → `ORPHANED 1285 of 1310`. The merger's older
   subjects name the BRANCH and no id (`Merge branch 'feat/singlenore-refactor'`),
   so every landed row read as unaccounted. A 60-row sample settled it:
   *named by id 18, named only by branch name 29*.
2. **Matching an 8-hex prefix** → both incident rows read `settled`, while the
   full id and the branch name are both absent. The reason is structural: **a
   task id's leading 8 hex digits are a TIMESTAMP PREFIX**, so
   `01a12ff2-a6e0-…` and `01a12ff2-a6a8-…` share all eight. The `01a12ff2` in
   `(re-lands 01a12ff2)` proves SOME row was re-landed, not that either was.

Final live population (1311 terminal rows, peer `peerbox`): **orphaned 379,
unresolved 919, withheld 11**.

## What is deliberately NOT here

No sweep, no daemon, no reflog keeper, no timer (Rule 35): the check is the
existing `branch-refs` verb plus a test in the existing suite. The audit sink
gained a READ side (`BranchDeleteRecords`, `SinkPath`) and nothing else — it
writes no new file and needs no new process.

## Honest limits

* Orphans are made VISIBLE, not restored. A record written before tips were
  recorded leaves `orphan_tip` empty; recovery for those still needs the raw
  hash a briefing holds.
* A short-tail re-land with no branch-name subject reads as orphaned (the
  prefix is unattributable — see above).
* A row whose branch is gone and whose work is named by nothing but which the
  peer advertises reads `on-peer`, not `orphaned`.
