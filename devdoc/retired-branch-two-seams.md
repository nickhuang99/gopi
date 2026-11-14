<!-- doc-class: normative -->
# The retired-branch answer at both seams (task 01a1446e)

**Subject:** one exported predicate — "is this branch a retired, decided
exclusion?" — and the two seams that now ask it before anything is dispatched or
handed over.

## The measured defect (what this closes)

Run `01a141c4` on host `host-b`, 2026-10-16. Branch `task/probe-zombie` was retired
on `peer1` at `06:57:41` as an unrelated-history decided exclusion — archive tag
`refs/tags/archive/task/probe-zombie` plus the companion reason record
`refs/gopi/archive-reason/task/probe-zombie` whose `reason:` line reads
`unrelated-history`. The merge-backlog row `01a141c4-615b-7b0d-9f47-a8c5e946ce07`
("merge task/probe-zombie into master") was filed at `06:52:31`, stayed live, was
handed over to `host-b` by `gopi task sync --handover` (`19:14:21`; the cancel
reason on the source ledger says nothing about the retirement) and was claimed
`9 s` later (`19:14:30`), so a worker session ran a void merge twelve hours after
the branch had left every merge lane.

Two facts that shaped the fix, both read back from the live ledgers:

- The row that survived spelled the retired branch **exactly**
  (`summary = "merge task/probe-zombie into master"`), so a spelling bug alone
  does not explain it. What the row ALSO carries is a **different branch in its
  deliverable** — the run's own ground:

  ```
  summary:     merge task/probe-zombie into master
  deliverable: {"branch":"task/01a141c4615b7b0d9f47a8c5e946ce07",
                "repo":"/home/user/work/GoPi","worktree":"…"}
  ```

  `mergeBacklogGuardBranch` (the family's shared branch resolver) prefers the
  deliverable's branch, so a reader that consulted only it asked about the
  worker's ground — never about the branch in the row's title.
- Nothing at the handover seam asked the question at all: `TaskTransferHandover`
  ran the two-way predicate over the row's fields (status, parenting, skip tags)
  and then wrote the CAS.

## What landed

| file | change |
|---|---|
| `internal/tools/archived_branch_retirement.go` (new) | `ArchivedBranchExclusion(repo, branch)`, `CanonicalBranchRetirementSpec`, `ArchivedBranchExclusionText`, `RowBranchRetirement` |
| `internal/tools/task_transfer.go` | the handover seam asks the predicate (`RowBranchRetirement`) before the signature; a retired row is refused as `*SyncTransferIneligible` and **nothing is written** |
| `internal/tools/task_merge_backlog_dispatch_guard.go` | the claim funnel's sweep asks it first (a distinct, second arm: `MergeBacklogRetiredMarker` / `mergeBacklogRetiredEvent`) and closes the row with the decision named |
| `internal/tools/tasknotify.go` | the new alarm registered, so `GOPI_TASK_NOTIFY_EVENTS=all` cannot drop it |
| `internal/tools/archive_row_reconcile.go` | the reconciler's membership test goes through the same canonicalization instead of string equality |

**ONE predicate, two seams (Rule 14).** `ArchivedBranchExclusion` is the whole
judgement: it reads the decision through the namespace's ONE reader
(`archiveReasonRecords` / `archiveReasonRecordOf`) and the code through the ONE
parser (`ArchiveTagReason`), and treats a tag **without** a vocabulary reason as
*not* a decision (no credit for a typo). The seams add no second rule — they
present a row's branch spellings to it.

**Why the spelling spec exists** (measured against the live namespaces, not
assumed — the archive-reason namespace on `peer1` carries all three shapes):

```
refs/gopi/archive-reason/task/probe-zombie
refs/tags/archive/task/probe-zombie
refs/tags/archive/01a113e467767eb9a4c33cb1df1cdfbb-20261007
```

A row may name a `task/<id>` branch by its ground spelling (`<id>`), and the
retirement may be recorded under either. `CanonicalBranchRetirementSpec` strips
**one** leading `task/` segment and the slashes a ref name contributes — nothing
more, because an unbounded strip folds unrelated names together.

**Where the row's decision is asked from.** `RowBranchRetirement` asks every
branch spelling a merge-backlog-family row carries, the **summary's branch and
the deliverable's branch**. The union is what makes the measured row decidable:
the retired branch is the summary's, and the deliverable's is the worker's ground.

## Honest scope (what this does NOT fix)

- The **measured** row `01a141c4` spelled the retired branch exactly, so the
  spelling arm is robustness for the live namespaces above — **not** the mechanism
  that closed the measured row. The mechanism is the two seams.
- The reconciler still runs only from the deciding pass (spelled `gopi merge`
  when this was written; that top-level verb was removed by the merge-removal
  epic on 2026-11-01 and the deciding pass is reached today as `gopi task
  merge` — same engine, same lock). A
  retirement performed on host A never reaches host B's ledger by itself; what
  changed is that host B's **claim and handover seams answer by name** when the
  branch is retired, instead of dispatching.
- A pending merge-backlog row is closed by the claim funnel's sweep, which runs
  inside `TaskNextFiltered` — i.e. at the first claim attempt after the
  retirement. A row that is never claimed again is never closed by it.
