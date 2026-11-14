<!-- doc-class: historical -->
# A reissue carries its evidence — `gopi task reopen` and the attachment manifest

Status: shipped (branch `master` worktree for task `01a15b11-dd6a-7e34-972e-573f1a424fc7`).
Baseline: `bf119eb`.

## The reported defect

`gopi task reopen <failed-id> --reason "..."` mints a successor `<id>-rN` carrying
the judged row's payload **verbatim**. When that payload says *"see attachment"*,
the successor still says it — but its deliverable carried no `attachments`
manifest and its session dir held no replica files. `gopi task show` on the
successor printed, verbatim (measured on peerbox, row `01a15b03…` → `-r2`):

```
[Attachments] NONE — but the payload references an attachment ("see attachment").
No manifest, no replica files: the referenced evidence is NOT archived with this task.
```

So the rework was dispatched to do work whose **input it never received**. The
only route back to the evidence was to open the *parent* row and read its
session dir — which is exactly what a reissue is supposed to spare you.

## Why `taskReopenDeliverable` dropped it (the reasoning that was wrong)

The repo axis (epic `01a15657`) deliberately carried `deliverable.repo` and
deliberately did **not** carry `worktree`/`branch`/`grounds`/`attachments`,
with this stated reason: *"the last two belong to the parent's run, not to the
rework."*

That is right for a **run directory** and wrong for **evidence**. A run's
worktree is a place, owned by one run, that a successor must not stand in. An
attachment manifest is a *record of the briefing's inputs* — the same class as
the payload, which the reissue already copies verbatim. Carrying the prose that
says "see attachment" while dropping the attachment is what makes the row
partial: the payload and the deliverable disagreed, and only the payload was
carried.

## The fix

`taskReopenCarryAttachments(newID, parentID, deliverable, parentDeliverable)`
(`internal/tools/tasks.go`) runs right after the repo axis:

1. reads the parent's `attachments` array from its deliverable (parsed through
   the ONE parser, `taskDeliverableParse`);
2. for each entry, copies the parent's replica (`<parentSessionDir>/<replica_rel_path>`)
   into the **successor's own session dir** through `taskAttachmentCopyTo`, at
   the same area-relative path — so the original path structure, and therefore
   the `[attachmentN:]` numbering a briefing cites, is preserved;
3. **re-measures** sha256 + size from the copy that actually landed (a manifest
   entry copied but not verified is the same false claim one layer down);
4. rebuilds the successor's deliverable through `taskDeliverableRebuild` (which
   preserves repo/branch/worktree/grounds — it takes the whole parse, never the
   fields one writer happens to own), and
5. returns the line fragment stating which of three things happened.

One helper was added because the inverse mapping was missing:
`taskAttachmentsAreaRel` (`internal/tools/task_session.go`) strips the
`attachments/` prefix from a `replica_rel_path`. Without it, `taskAttachmentCopyTo`
— which re-applies that prefix — landed the replica one `attachments/` too deep,
a path `task show` prints and the reader then cannot open. That was caught by the
test arm that `stat`s the printed path, not by a review.

### The three-way outcome, all stated on the result line (Rule 15)

```
… deliverable.repo=/home/user/work/gopi (inherited from 01a…f950b) attachments=2 (carried from 01a…f950b)
… carries no attachments (the judged row archived none)
… could NOT carry attachment /dev/shm/x.log (<reason> — recover it from parent 01a…f950b)
```

An entry whose replica path is empty, or whose file is gone (a finished run's
scratch is swept long before somebody judges the row), is **left out of the
manifest** rather than written as a path `task show` would print and nobody could
open. The line names the original path and the parent row that still has it, so
the reader is pointed at the one place the evidence still is.

## What is deliberately NOT changed

- **The parent edge stays.** `parent_task_id` is what
  `FailedNeedsReissue`'s successor test and bug-flow adoption *read*. Removing
  it to "make the successor self-contained" would trade a missing file for a
  lost lineage, and the single-verdict-per-row machinery would lose the pointer
  from a rework back to the verdict that asked for it.
- **Rule 33's ordinary-child park is untouched.** A rework row is released by the
  parent's *terminal verdict*; a plain child of the same failed parent is still
  parked. `TestReworkParentGateReopenChildIsClaimable` asserts both arms.
- **No schema change, no new dependency, no new tool.** One deliverable axis and
  one line fragment.

## Why `reopen` instead of submitting a fresh UUID

A reissue is a **judgement about a row**, not a new piece of work:

| | `task reopen <id>` | a fresh `task_submit` |
|---|---|---|
| lineage | `parent_task_id` → the judged row; `rework` tag | none |
| readers that find it | `FailedNeedsReissue`, bug-flow adoption, `task show` trail | nothing |
| payload | the judged briefing + the reason the rework exists | re-typed from memory |
| repo | inherited from the judged row (or `--repo`) | re-resolved from the submitter's cwd |
| evidence | **now** carried (this change) | must be re-attached by hand |
| operator view | `-rN` says "this is the Nth attempt at that row" | an unrelated id |

The `-rN` naming and the parent edge are what let a reader answer *"how many
times has this been reissued, and why?"* from the ledger alone. That is the whole
reason the verb exists rather than "submit it again".

## Verification

- `go build ./...` — OK.
- `go test ./internal/tools/ -run 'TestReopen…'` — the three new arms **fail on the
  pre-fix code** (measured: `the reissue must CARRY the parent's 2 attachments,
  got 0`) and pass after.
- Real surface, real binaries, sandbox ledger: the pre-fix binary (`bf119eb`,
  built from `git archive`) prints `[Attachments] NONE …` on the successor; the
  fixed binary prints both `attachmentN:` lines, and both printed paths `stat`
  to the same sha256 the manifest records.
