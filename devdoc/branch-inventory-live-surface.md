<!-- doc-class: historical -->
# Branch inventory on a live surface — `gopi merge --status`

Class: historical record of one run (epic `branch-inventory-live-surface`, task
`01a1487274f879608ec424be30829ef6`, 2026-10-15).

## The gap: a correct reconciliation with no renderer

Bug 01a0aa74's fix landed `BranchInventory` in `internal/tools/repo_sweep.go`
(commit `69fe2e8c2`): every local head is classified into exactly one named
bucket, and two methods render it — `BranchInventory.SummaryLine`
(`internal/tools/repo_sweep.go`) and `BranchInventory.BucketLines`
(`internal/tools/repo_sweep.go`). The invariant is stated in the type's own doc
comment and pinned by `TestBranchInventory_PartitionsEveryLocalHead`.

Their only caller was `MergerStatus.Render`, and that renderer was deleted by
`930096880` (stage 2 of epic `merger-delete-dead-impl`) when the resident merger
daemon was retired. The consequence is not a wrong number — it is an unreachable
one:

```
$ git grep -n 'SummaryLine()\|\.BucketLines(' -- internal/ cmd/ | grep -v _test.go
internal/tools/repo_sweep.go:294:func (inv BranchInventory) SummaryLine() string {
```

One hit, and it is the DEFINITION. `branchInventorySummaryFmt` was formatted by
nobody, so an operator had no surface on which to add `git branch`'s head count
up against the merger's own view of the repo — the exact complaint bug 01a0aa74
was filed for, restored by the retirement of the surface that answered it.

This is the shape Worth naming: **a fix can be landed, correct, tested and
still dead.** The unit tests kept the classifier honest and could not see the
missing renderer, because they call the renderer themselves.

## The surface choice: `gopi merge --status` (option a)

Two read-only faces survived the retirement, and only one of them reaches an
operator:

| face | state |
|---|---|
| `gopi merger --status` | **RETIRED.** `retireMergerFace` (`cmd/agent/merger_retired.go`) exits 1 for every spelling of the verb, `-h` and unknown flags included. It cannot be the surface. |
| `gopi merge --status` | **LIVE.** `runMergeStatus` (`cmd/agent/merge_run.go`), the read-only status arm after the daemon. |

The second was chosen. It already answered half of the operator's question
("what is in the graveyard?" — the archived set), and the head reconciliation is
the other half of the same question: *what happened to all my branches*. Printing
them beside each other means one command answers "where is this branch" for every
branch, decided or not.

`runMergeStatus` is contracted READ-ONLY — no lock, no sweep, no child, no ref —
and the renderer respects that contract by construction: it consumes
`tools.MergerSweepPreview` (`internal/tools/repo_sweep.go`), which is exactly the
preview the doc comment describes as running "NO merge, NO delete, NO rebuild and
never [taking] the singleton lock".

## The implementation

- `internal/tools/merger_status_inventory_block.go` —
  `RepoSweepPreview.InventoryBlock()` composes the header, `SummaryLine()` and
  `BucketLines(protectedNote)`. The note passed to the bucket renderer is
  `describeDeadHolderNote` + `describeHeldNote` — the same composition the
  merger log already uses, so the worktree bucket carries the dead-holder
  verdict in ONE spelling and no caller can pass a second one.
- `cmd/agent/merge_run.go` — `runMergeStatus` prints one `InventoryBlock()` per
  configured repo, immediately before that repo's archived set. It renders
  BEFORE the archive read, and independently of it: a repo whose archived
  listing fails still gets its head reconciliation, and the failure still turns
  the exit status non-zero.
- The `--status` help page was updated to say what the arm now answers. It is a
  `//`-comment block, so the phrase "a `git branch` count" could not be used
  there (a backtick inside a comment line starts a Go string — the build caught
  it); it reads "a git branch count".

Rule 1/13/14 compliance: **no new numbers and no re-spelled labels.** The header
and the failure line are the only two spellings this file owns
(`inventoryBlockHeaderFmt`, `inventoryBlockErrFmt`); the summary line, the bucket
line and every bucket label are the existing constants in `repo_sweep.go`.

## The measurement (Rule 31: the surface was invoked)

Built with the repo's declared build and run against a hermetic profile and a
seeded repo carrying one head of every kind (integration, worktree-held,
archive-tagged, exempt-namespace, pending):

```
$ GOPI_HOME=…/home GOPI_SETTINGS=…/home/settings.json ./bin/gopi-inv merge --status
branch-inventory: /home/user/workspace/gopi-inv1/repo
  branches:  5 local head(s) = 1 integration + 1 worktree-held + 1 archive-tagged + 1 exempt-namespace + 0 deferred + 1 pending
    worktree-held (1): feat/held — git worktree list (a registration, not a liveness measurement)
    archive-tagged (1): feat/decided-loser — decided losers (archive tag): never merged, never deleted
    exempt-namespace (1): hp-unique-ledger-row — archive ledger: never merged or deleted
archived: /home/user/workspace/gopi-inv1/repo
  1 branch(es) tagged out of the merge queue (buried: 0, no reason recorded: 1)
  REASON-MISSING feat/decided-loser — (none) (unknown) — no reason recorded
EXIT=0
```

The buckets sum to the head count (1+1+1+1+0+1 = 5 = `git branch | wc -l`), and
every seeded branch is named in its own bucket. After this change the
acceptance grep finds a PRODUCTION call site rather than only a definition:

```
$ git grep -n 'SummaryLine()\|\.BucketLines(' -- internal/ cmd/ | grep -v _test.go
internal/tools/merger_status_inventory_block.go:71:	return out + p.Inventory.SummaryLine() +
internal/tools/merger_status_inventory_block.go:72:		p.Inventory.BucketLines(p.inventoryProtectedNote())
internal/tools/repo_sweep.go:294:func (inv BranchInventory) SummaryLine() string {
```

## Tests

- `internal/tools/merger_status_inventory_block_test.go` — the rendered block's
  numbers add up to the head count, every seeded head is named in its bucket, a
  FAILED classification renders `UNAVAILABLE` and never `0 local head(s)`, and
  the worktree bucket's annotation is the shared dead-holder composition without
  a repeated protected count.
- `cmd/agent/merge_status_inventory_e2e_test.go` — drives `runMergeStatus`
  itself (the real arm, reached through the resolver's own `repos` table) against
  a seeded repo, then asserts the printed reconciliation line, the sum, the named
  buckets, and that the archived set is still rendered beside it.

**Parsing the arithmetic in a test**: take every numeric token AFTER the `=`
sign. Do not split on `" + "` and take the first token of each piece — the first
piece carries no number, so that form silently yields 0 for the head count and
turns a correct render into "buckets do not add up" (memory #1812 records the
same pitfall costing a round in the merger-tick sibling task).

## What was deliberately NOT done

- **No `gopi merger --status` revival.** The verb refuses by design; the
  retirement is a decision, not an oversight, and re-opening it would restore a
  daemon surface the operator asked to be removed.
- **No new flag, script, state file or tool** (Rule 35). The block rides an
  existing arm of an existing command.
- **The candidate list and the per-branch attribution are NOT printed here.**
  They remain on the surfaces that own them. This block is the arithmetic plus
  the bucket names — the minimum that makes `git branch` reconcilable — because a
  second full per-branch report on the status face is the drift the original fix
  removed.
