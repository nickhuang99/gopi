<!-- doc-class: historical -->
# The ledger half of an unrelated-history retirement

`gopi merge`'s unrelated-history verdict retires a branch **durably** — an archive
tag in `refs/tags/archive/<branch>` plus a reason record in
`refs/gopi/archive-reason/<branch>`, with the branch ref **preserved** (a
retirement is a decision, never a deletion). Until this change it wrote **no
ledger state at all**, and that is a gap with a measurable cost.

## The measured defect

`2026-10-13T10:49:59`, `feat/unrun-demo` was retired as a decided exclusion.
Two open rows named it:

| row | status | source |
|---|---|---|
| `01a13322-12a3-723c-b496-6c2038a2c290` | in_progress | merger |
| `01a135d2-36db-75a6-9f19-a9583b964cb5` | pending | human |

A reviewer kept being dispatched onto a ref the retirement had already taken out
of every merge lane, and the row's disposition was derivable **only by re-running
git by hand**. Nothing on the row said it.

## What was already covered, and what was not

Two earlier mechanisms exist, and neither covers this:

1. **The FILER half** — `archiveTagAncestry` / `archiveTipReachable`
   (`internal/tools/task_merge_backlog.go`) stop a *new* merge-backlog row from
   being filed for an already-retired branch.
2. **The PENDING sweep** — commit `2e6e4868c` adds `mergeBacklogGhostSweepPending`,
   which closes **pending** family rows whose branch resolves nowhere, lazily,
   from inside `TaskNextFiltered`.

The sweep is real, and running this work's own first test proved it: the fixture's
pending row was closed by the sweep from inside the test's `TaskNextFiltered`
probe. That is also why `TaskNextFiltered` must never be used as a read-only probe
in a test — **it mutates the ledger**. What the sweep does *not* cover is the row
already `claimed` / `in_progress`: the reviewer standing on the ref.

## The change

`tools.ReconcileUnrelatedHistoryRetirement(repo, branch)` — the writer, then the
ledger half. `cmd/agent/merge_run.go`'s `mergeUnrelatedHistoryOutcome` calls it in
place of `RetireUnrelatedHistoryBranch`, and appends what it did to the verdict's
action line.

### Order is load-bearing

The reconciliation runs **after** the writer and **reads the decision back** from
the record the writer leaves:

```go
RetireUnrelatedHistoryBranch(repo, branch)              // the decision
code := ArchiveTagReason(cat-file blob refs/gopi/archive-reason/<branch>)
```

A reconciler that ran first, or that trusted its caller's word, would cancel rows
on a decision **nobody made** — the owner gate refusing a finished deliverable's
branch is the case the gate exists for, so a refusal returns an error and nothing
is closed. `TestArchiveRetirementReconcilesNothingWhenTheWriterRefuses` pins it.

### Row selection: the row's own classification, never `deliverable.branch`

Rows are found by replaying the **one** merge-backlog classifier
(`mergeBacklogFamilyBranch(summary, tags)`) over the live rows — the same
membership rule the dispatch guard and the terminal guard read.

This is not a style choice. The owner gate reads `deliverable.branch`, and a row's
deliverable names **its own ground** (`task/<id>`), not the branch it asked to
merge: `gopi task for-branch feat/unrun-demo` answered *"no ledger row names
branch"* over 2164 branch-carrying rows. That is precisely why the owner gate never
fired for these two rows — and why a row set selected on `deliverable.branch` would
have found zero rows, while one selected on a text resemblance would close
unrelated work.

### The predicate that cost the most: `finishCleanupOwnerRowLive` is the WRONG question

The first implementation reused it. Its own test then reconciled **0 of 2** rows.
The reason is a sign error, not a bug in that function: it answers the **worktree
veto** — *"any reason to keep this ground?"* — where an **absent** claim stamp is a
HOLD (default-deny, because removal is destructive). This reconciler asks the
opposite question — *"may I close this row?"* — and **31 of the 36 live rows on
this host are pending with `claimed_by = ''`**. Borrowing the veto makes the
reconciler unreachable for exactly its target rows.

The correct ladder (`rowHasLiveHolder`) asks, weakest last:

1. the row's **own** runner identity (`proc_pid` + start ticks + cmdline) through
   `finishCleanupProcessHolderLive` → `procname.RunnerIdentity` — the only rung
   that catches a **recycled** pid;
2. the claim stamp, when `claimHolder` resolves a **local** pid → `pidAlive`;
3. a **foreign or unparseable** stamp → **hold** (the write is terminal; Rule 15's
   default-deny direction forbids resting it on an unmeasured factor);
4. an **absent** stamp → nobody here holds it (measured: 31/36 live rows).

### The close

`pending → claimed` (`TaskClaimExplicit`, holder `archive-retirement-reconciler`)
then `taskUpdate(id, cancelled, "", reason)` — the shared mutation core, so the
state machine and the claim trail are not bypassed. `cancelled`, never `failed`: the
row's premise was never true; a `failed` row would read as *we tried and could not*
and invite a retry of the same ghost. The judgement also rides the row's append-only
trail (`archive-retirement-reconciled`), so a reader of `gopi task show` sees it.

### What it deliberately does not do

- **No tag, no row.** It reads the decision; it does not make one, and it files
  nothing.
- **It never fights a live holder** — those rows are left open and **reported**
  (the verdict line prints `LEFT OPEN <id> (<status>): <holder clause>`).
- **Idempotent** — the writer is idempotent by reason (same blob ⇒ no new tag), and
  a closed row is terminal, so a second pass closes nothing and writes no second row.

## Verification

```bash
go test ./internal/tools/ -run TestArchiveRetirement -count=1
```

Three tests, all driving the real surface (`ReconcileUnrelatedHistoryRetirement`
against a real temp repo **and** a real ledger):

- `TestArchiveRetirementReconcilesTheOpenRow` — both member rows terminal, the
  recorded reason carries branch + tag + reason code + gloss, a trail entry exists,
  the funnel-closed state agrees, the bystander is untouched, the tag survives.
- `TestArchiveRetirementHoldsALiveRowAndIsIdempotent` — a live local holder leaves
  the row open and reported; a second pass closes nothing, writes no second tag and
  files no row.
- `TestArchiveRetirementReconcilesNothingWhenTheWriterRefuses` — the owner gate's
  refusal closes nothing and writes no tag.

## Files

- `internal/tools/archive_row_reconcile.go` (new) — the reconciler.
- `internal/tools/archive_row_reconcile_test.go` (new) — the regression tests.
- `cmd/agent/merge_run.go` — the caller (`mergeUnrelatedHistoryOutcome`), plus the
  clause that renders the ledger half into the verdict line.
