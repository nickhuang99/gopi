<!-- doc-class: historical -->

# The archive gate and the branch's own rework successor

## The measured loop

`cmd/agent/merge_prompt.md` §3.6.1 case ④ orders a hand-over in three steps:

1. archive the branch's diff as an attachment,
2. submit the rework row (`rework <branch>: cannot auto-merge into master`),
3. remove the ref through `gopi task archive <branch>`.

Step 2 made step 3 impossible. The hand-over lane stamps the **spent** branch
into the row's `deliverable` (`resolveRowStampDeliverableBranch`, so the
resolver lane can find its subject), so the ref's only owning row was a LIVE row
that named it — and `ArchiveDecide` refused every live owner. Measured on the
2026-10-18 hand-over run (`merge-rework-01a14b08`, master `4ae648fa5`): eleven
branches handed over, ten refs removed, the eleventh refused.

The refusal is not merely cautious, it is a **closed loop**. The ref stayed a
merge candidate, so the next `gopi merge` pass probed it, conflicted on it, and
filed a **second** rework row for the same branch — the duplicate-row half of the
mechanism that `rework_handoff.go` exists to prevent.

```
$ gopi task archive task/01a1473687cb7823aff395fb56c3ad7c --repo <repo> --reason superseded
archive refused: task 01a14ba9-… owns this branch and is "pending" — the work is IN FLIGHT.
```

## The fix: read the row's SUBJECT, not its deliverable

The gate keeps one new question (`internal/tools/task_rework_successor.go`,
`taskReworkSuccessorOf`): is the live owner row this branch's **rework
successor** rather than its tenant? Three clauses, all read from the row itself:

- **(a) the row is live** — the predicate only ever removes the refusal the gate
  is about to issue for a live row;
- **(b) its tags carry the rework lane's vocabulary** — `rework,merge` (§3.6's own
  recipe), `merge-conflict` / `branch-rework` (the in-code lanes), from ONE list
  (`reworkSelectionTags`) referencing the existing constants;
- **(c) its SUMMARY names this branch as the work it is a rework OF** — the ref
  directly after a lane prefix (`rework `, `resolve merge conflict: rework: `,
  `merge verify rework: `).

Clause (c) is spelled as a **prefix + subject**, not as "the summary mentions the
branch", and that is the whole safety argument. A row summarised `implement
task/X locally` mentions X and is work ON X, so it stays refused; a row
summarised `rework task/X: cannot auto-merge into master` says X is what it
replaces. The first draft of this fix used "the deliverable is a different
branch" as the safety line and it was **wrong in both directions** for the
measured shape: the loop's own row has the deliverable *equal* to the branch
(the stamp), while a plain `implement …` row had a different one. The subject is
the only field that separates the two, and it is the field each lane builds its
identity on.

### Why this is still the safe direction

Archiving writes a **decision tag**; it is not a deletion. The archive-reclaim
arm independently refuses to remove a head the ledger still shows a live row on
(`archiveReclaimRowLiveness`), so a successor that is still in flight keeps its
ref physically present while the tag tells `gopi merge` the branch is spent —
which is exactly what stops the duplicate rework row. The measured end-to-end
run shows both halves in one output:

```
$ gopi task archive task/01a1473687cb7823aff395fb56c3ad7c --repo <repo> --reason superseded
archived task/01a1473687cb7823aff395fb56c3ad7c as archive/task/01a1473687cb7823aff395fb56c3ad7c
  reason: superseded
  owner:  task 01a14ba9-6239-7b9d-a72a-21df703b1adb (pending)
  successor: task 01a14ba9-… is this branch's rework SUCCESSOR — the branch is its SOURCE, not its deliverable, so the ref is spent and the work lives on that row
  skipped          archive/task/01a1473687cb7823aff395fb56c3ad7c  branch … is tracked by task 01a14ba9-… whose row is "pending" — a non-terminal row may still be building on it, so the ground is kept (finish or cancel the row to release it)
### exit=0
```

The tag is written, the ref is kept by the reclaim arm, and the exception is
**named in the output** (`ArchiveDecision.SuccessorTaskID` →
`ArchiveBranchResult.Successor`) rather than left to be inferred.

## The general LIVE refusal now names its escape

The old sentence said "wait for the row to conclude (or cancel it deliberately)"
without the verb. It now names both:

```
  If that row is instead this branch's REWORK SUCCESSOR — a live rework-lane row whose SUMMARY is a
  rework OF task/X ("rework task/X: …") — then the archive is the step that should go ahead: …
  Otherwise the operable escape is the row itself:  gopi task update <id> --status cancelled  (or let it finish: completed)
```

The verb is one constant (`archiveLiveOwnerEscapeVerbFmt`), built from the
`TaskStatus*` names.

## What did NOT change

- a live row that is **not** a rework successor still refuses (arm ③ of the
  tests, and the plan-by-step end-to-end run: `implement <branch> locally` with
  `--override-completed` is still refused);
- a **completed/verified** owner still refuses without `--reason` **and**
  `--override-completed`, and the override path never reports a successor;
- `archiveOwnerRank` / `archiveOwnerDecided` / `archiveOwnerArchivable` are
  byte-identical — the pick's order and the decided/archivable verdicts are
  untouched. The only new material on `archiveOwner` is the row's own text
  (`reworkRowShape`), carried from the scan that already read it (no second
  query, Rule 13/14).

## Tests

`internal/tools/task_rework_successor_test.go`:

| arm | shape | verdict |
|---|---|---|
| ①a | live rework successor, hand-over stamp (`deliverable` == branch, summary `rework <branch>: …`) | **proceed**, successor named |
| ①b | live rework successor, deliverable = fresh branch | proceed |
| ②a | live row, subject `implement <branch> locally` | **refuse** |
| ②b | rework-tagged row, subject `rework the <branch> implementation in place` | **refuse** |
| ②c | live row, summary is a rework of a branch but tags are outside the lane | **refuse** |
| ③ | completed owner | refuse → **proceed** with `--override-completed`, no successor reported |
| ④ | live plain row | refusal names `IN FLIGHT`, the operator's `gopi task update … --status cancelled`, and the lane name |

Red proof: with the exception short-circuited (`if true { return false }` in
`taskReworkSuccessorOf`), arm ①a fails with the pre-fix wording — the same
ledger shape refused before and proceeding after. The production file was
restored and its blob identity re-checked against `HEAD`.

## Files

- `internal/tools/task_rework_successor.go` (new) — the successor predicate, the
  lane tag/prefix vocabulary, `ArchiveReworkLaneName`.
- `internal/tools/archive_reason.go` — the gate's live arm asks the predicate;
  `ArchiveDecision.SuccessorTaskID`, `ArchiveBranchResult.Successor`,
  `archiveLiveOwnerEscapeVerbFmt`, the reworded refusal.
- `internal/tools/task_for_branch.go` — `TaskForBranchOwner.Tags` / `.Summary`
  carried (unexported) from the one scan.
- `internal/tools/task_owner_axes.go` — `taskOwnerAxisRecord.shape`.
- `cmd/agent/cli_archive.go` — renders the successor line.
- `internal/tools/task_rework_successor_test.go` (new) — the table above.
