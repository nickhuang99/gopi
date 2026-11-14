<!-- doc-class: historical -->
# Task Report: merger `--status` branch accounting (bug 01a0aa74)

## The report

`gopi merger --status` counts "ready-to-merge branches", and the operator
reading it on peerbox saw numbers that could not be reconciled with reality:

```
$ git branch | wc -l        → 20 (+ master = 21)
  protected: 10 branch(es) held by a live worktree
  pending:   1 branch(es) the next sweep would merge
```

11 of 21 heads were accounted for. The operator's words: *"this seems very
strange that merger ignores so many branches without + sign before it. you can
even verify by using git worktree list."*

## Root cause (two independent defects, both PRESENTATION)

The **counts were arithmetically correct**. Reading the attachment against real
git state on peerbox:

| bucket | count | evidence |
|---|---|---|
| `+`-marked heads (registered worktree) | 10 | `git for-each-ref --format='%(worktreepath)'` |
| archive-tagged decided losers | 9 | `git tag -l archive/<branch>` |
| real pending branch | 1 | `fix/cwd-release-gate-inventory` |
| total | 21 | — |

`protected=10` = exactly the `+` set; `pending=1` = exactly the one unmarked,
unarchived, unmerged branch. So this was **not** a miscount. Two real defects:

1. **The claim was unverified.** The line said *"held by a **live** worktree"*,
   but the protected set comes from git worktree **REGISTRATION**
   (`mergedCleanupWorktrees` → `repoSweepProtectedBranches`) — a fact about the
   checkout. Holder liveness is **never measured** on this path. Naming a
   property you do not measure is an unverified operator-facing claim
   (Rule 15). The operator reasonably tried to verify it with
   `git worktree list` — and the `+` marks did match, but nothing in the output
   said that was the evidence.
2. **Three kinds of head were dropped silently.** Integration (master), the
   `hp-unique-*` ledger namespace, and all 9 archive-tagged branches were
   `continue`d past with no trace. `protected` + `pending` therefore could
   never sum to `git branch`'s count, so the merger *looked* like it was
   ignoring branches it had in fact already decided on. The exemption arms were
   individually correct (an archive tag **is** the decision — `repoSweepArchiveExempt`,
   `taskMergeExempt`, shared with `TaskMerge`) — the defect was that their
   effect was invisible.

A latent third: the discovery loop `break`-ed at the per-tick cap, so every
head after the cap vanished from the output entirely — the same invisible-loss
shape, waiting for a repo with >20 candidate branches.

## Completed

- **`BranchInventory`** (`internal/tools/repo_sweep.go`) — the reconciling
  view. Every local head is counted **before** any bucket test and lands in
  exactly one bucket:
  `Total == Integration + Protected + Archived + Exempt + Deferred + Candidate + Capped`.
  `Spent` (tip already in master → the sweep prunes the ref) is a sub-count of
  `Candidate` and deliberately does not participate in the sum.
- **One classification loop** — `repoSweepClassify` replaces the body of
  `repoSweepUnprotectedBranches` (kept as a thin wrapper for its two callers),
  so the sweep and the status preview cannot drift (Rule 13).
- **Capped heads stay accounted for.** The loop no longer `break`s; a head
  beyond `scanMax` is counted as `Capped`.
- **Honest label.** The line no longer claims a measured liveness: it names
  its evidence — *"git worktree list (holder liveness is not measured here)"*.
- **Reconciliation line + per-bucket name lists** — the operator can now add
  `git branch` up and see *which* branch went where, with truncation stated
  (`branchInventoryNameMax`, `…` suffix).
- **Dead-holder verdict moved onto the worktree-held bucket line**, so the
  protected arm is stated once instead of twice and the red-proof test
  (`zz_redproof_reclaim_promise_test.go`) still passes.

## Verification

Live render on this host (`8` local heads, `git branch` agrees):

```
  branches:  8 local head(s) = 1 integration + 2 worktree-held + 3 archive-tagged + 0 exempt-namespace + 0 deferred + 2 pending
    worktree-held (2): feat/chat-m2-streaming, fix/merger-status-branch-account — git worktree list (holder liveness is not measured here)
    archive-tagged (3): feat/web-m1-b4b7, feat/web-m2-b5b8b9b12, fix/standards-stylegate-master-red — decided losers (archive tag): never merged, never deleted
    already-in-master (1): gopi-chat-m1 — the sweep prunes the ref with branch -d
  pending:   2 branch(es) the next sweep would act on (1 already in master → the ref is pruned)
```

The 3 archive-tagged branches — invisible before this fix — are now named, and
every one of the 8 heads appears in exactly one bucket.

Tests (`internal/tools/merger_status_inventory_test.go`):
`TestBranchInventory_PartitionsEveryLocalHead` (the sum invariant, one branch of
every kind), `TestBranchInventory_DeferredBucketsAreCounted`,
`TestBranchInventory_CappedHeadsStayAccounted` (the `break` regression),
`TestMergerStatus_RenderReconcilesBranchCounts` (buckets named, `live worktree`
claim gone), `TestBranchInventory_SummaryLineAddsUp` (rendered arithmetic adds
up and matches `git branch`), plus the silent-drop guards:
`TestBranchInventory_BucketLinesNeverDropANote` (a zero count with a note still
renders — the dead-holder names survive),
`TestBranchInventory_CappedHeadsStayAccounted`,
`TestMergerStatus_RenderReconcilesBranchCounts`.

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `go test ./internal/tools/ -run 'Protected|Redproof|Reclaim|Inventory|Bucket|Merger|Sweep'`
  → ok 29.0s; the full `internal/tools` package exceeds a 2-minute bash call on
  this host (pre-existing, unrelated to this change)
- `go vet ./internal/tools/` → clean
- Commit: `57a27710` (accounting) + `c874b08c` (silent-drop guards), branch
  `fix/merger-status-branch-account`

## The bug's own shape, found twice more in the fix (commit `c874b08c`)

Running the whole `internal/tools` suite — not the new tests' happy path —
surfaced three sites where a **named branch is silenced by a count**. That is
bug 01a0aa74 in miniature: the operator saw a number where a name belonged.

1. **`MergerRepoStatus.Protected` duplicated `Inventory.Protected`.** A fixture
   that set only the flat field rendered an **empty bucket list**, dropping the
   dead-holder names — the bug reintroduced by the fix's own plumbing. The
   duplicate field was **deleted** (not reconciled) so every use site is a
   compile error until it reads `Inventory.Protected`, and the silent
   divergence is unrepresentable.
2. **`BucketLines` gated every bucket on `total == 0`.** The worktree bucket
   additionally carries the caller's `protectedNote` (the dead-holder names),
   so it must render when **either** the count **or** the note is non-empty.
   The naive `total == 0 && note == ""` gate was wrong for the other four
   buckets — their notes are non-empty constants, so an empty bucket would
   print a bare label. Only the worktree bucket takes the dual gate.
3. **`DescribeProtected`'s `p.Protected == 0 → ""` guard** had the same shape:
   a preview carrying dead-holder/held NAMES but a zero count returned the
   empty string. The guard now keys on the count **or** the name lists.

## Not Done (honest)

- **The merger's own per-repo LOG line is only partly reconciled.**
  `DescribeProtected` (the daemon tick's line) now renders for a
  names-but-zero-count preview, but `MergerRepoSweep` still consumes only the
  candidate list and the protected count — a tick's log does **not** print the
  `BranchInventory` buckets. The operator's report was about `--status`;
  extending the tick's result/log to the inventory is a separate, narrower
  change and is **not** claimed here.
- **A sibling branch overlaps this area**: `feat/branch-attribution`
  (peerbox, `93939e1a`) edits the same two files (`merger_status.go`,
  `repo_sweep.go`) to add a branch-axis `Attribution`. This fix was cut fresh
  from master (Rule 23) and does **not** build on that branch; whoever merges
  second resolves the overlap in those two files.
- `Spent` is measured only for the first `branchInventorySpentScanMax` (50)
  candidates — a read-only command's cost ceiling. Beyond that the count is
  under-reported rather than guessed; the sweep itself asks the same question
  per branch at merge time with no cap.
