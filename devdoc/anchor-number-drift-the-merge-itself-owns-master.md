<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown — slices 1/5 and 3/5).**
> `gopi tool doccheck` and `tools/docclass/` were DELETED (`e6bfa10`,
> `29882b3`) and this page is no exception: the command quoted below is the
> RECORD of what a past run executed — it cannot be re-run, and no document may
> be classified, verified or frozen by running it.
# The anchor-NUMBER drift a MERGE mints on master

## What this records

`gopi tool doccheck` and `internal/docclass`'s own gate are red on the wire tip
(`bbf8d97604ed5993886f43e28313ec6d0ae1e03d`, read with `git ls-remote` on
hosta at 2026-10-14 04:15). This document records the **mechanism** — the
red was minted by the landing itself, from two revisions that are each green —
and what is (and is not) fixed here. It is a companion to
`devdoc/merge-target-doc-gate.md` and `devdoc/doccheck-anchor-number-master-drift-escape.md`,
which cover an earlier red of the same family.

Re-measure it in three commands:

```bash
gopi tool doccheck                                          # exit 1, one STALE PERMIT
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv --check   # exit 1, same row
go test ./internal/docclass/ -run TestAnchorNumberGateOnTheRealTree -count=1   # FAIL
```

## The measurement that fixes the mechanism

The landed commit is `bbf8d9760`, a merge of two parents:
`476e47ede` (the integration branch) and `77be22766` (the branch
`task/01a135da02fa726b963f76187d31001d`, the TARGET-SCOPED lane).

```
$ git merge-tree --write-tree 476e47ede 77be22766      # the union
439196131baa409e6b1f34e429bd6e216ac0517d
$ git rev-parse bbf8d97^{tree}
439196131baa409e6b1f34e429bd6e216ac0517d               # the union IS the merged tree
```

And the three states, each measured on a **real checkout of that revision** with
the census generator's own `--check` (never a diff reading):

| revision | role | rows in `internal/docclass/testdata/anchor-number-baseline.tsv` | census `--check` |
|---|---|---|---|
| `476e47ede` | integration branch | 205 | **exit 0** — "table matches the tree — the freeze is exact" (figures `frozen=168`) |
| `77be22766` | the landing's branch | 204 | **exit 0** — "table matches the tree — the freeze is exact" (figures `frozen=167`) |
| `bbf8d97` | the merge of the two | 205 | **exit 1** — one STALE PERMIT |

The row in question is the **last** line of the table
(`internal/docclass/testdata/anchor-number-baseline.tsv:106`):

```
devdoc/merge-backlog-terminal-guard.md	number-drifted	internal/tools/tasks.go:2825	}	first-frozen at 0b3c1de29; …
```

* `476e47ede` **carries** it (master's sweep of the cleanup arm froze it);
* `77be22766` **deleted** it — the same commit that deleted it also dropped the
  header figures `frozen=168 → 167`, i.e. the repair was consistent **in
  isolation**;
* `bbf8d97` **carries it again**, with the header reading `167` — so the table's
  own line count and its figures disagree, and `doccheck` reports the permit as
  covering nothing.

## Why this is a routing defect and not only a conflict

`internal/tools/mergeset_docclass.go` asks **this exact question** of the landing,
under the exact revision this defect is about — `mergesetGateMeasure` creates a
detached worktree at `mergeRev`, so `dir` **is** `bbf8d9760`'s own tree. Its run
set is not the problem either: the landing's union holds the manifest
(`devdoc/DOC-INDEX.tsv`) and `devdoc/merge-target-doc-gate.md`, so
`mergesetDocclassHits` names the family and the leg's gate package —
`internal/docclass`, the carrier the leg derives from the census table — joins
the run set. `internal/docclass`'s real-tree gate FAILS on that tree (measured
above).

So a leg that runs, on the right revision, over a tree its own package reds,
reported nothing that a reader of the ledger can see. This document does **not**
claim why: the leg's outcome is only observable through the merger log, which
this client cannot read, and per Rule 41 a claim like "it never ran" would be an
inference — the honest surface is the one below.

**The one surface this client CAN read is the wire tip's own state.** At
`bbf8d97`, with every merge-time arm of this family in force — the landed
TARGET-SCOPED lane (`merge_target_doc_gate.go`, asked at the head of both merge
faces) **and** the merged-result step's report channel — the red is on master,
unrepaired, and the wire's own ledger carries **no** row naming TARGET-DEVDOC
(the target lane's own summary format, `mergeTargetDocGateNotifySummaryFmt`),
which is the shape its bug-row channel would have left behind if it had filed
one. Whatever the internal reason, **the escape is live in the state as
shipped**.

## The class of red is not the one an anchor lane was built to catch

`AnchorNumberDrift` is a **two-armed** report (`internal/docclass/anchor_number_baseline.go:156`):
`Unpermitted` (drift with no permit) and `StalePermits` (a permit covering
nothing). `HasFindings` counts both. A per-branch anchor lane is driven by a
branch's **diff** — `finishCleanupBranchAnchorChangedPaths` — and a branch's diff
naming *nothing wrong* is exactly the abstention such lanes are designed to
return. The stale-permit arm is not reachable that way:

* the branch **deleted** the row (nothing drifts — no finding to attribute);
* the integration branch **carries** the row (nothing new — nothing to attribute
  to the candidate);
* only the **combination** reds, and the combination is not a revision any
  revision-scoped lane ever holds.

That is the mechanism statement, and it is the thing a repair must be aimed at —
not at "the leg did not run", which is a fact about the log rather than about the
code.

## What this change does, and what it deliberately does not

**Does:** repairs the red that is on the tree — the table is DATA, so it is
repaired **through its generator** and never by hand (Rule 36/38):

```bash
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv
# 0 permit(s) added, 1 permit(s) removed — the header figures go 168 → 167
```

**Does not:** touch one line of the merge/close-out machinery. Every arm
(`mergeset_docclass.go`, `merge_*_gate.go`, `rebuild_vet_mergeset.go`,
`task_finish_premerge*.go`) is `internal/tools`, which Rule 28 assigns to the
system layer; this filing host is a client, and it measures and files. The
existing repair rows for this family are on the wire's own ledger:
`01a136cf-f3cf-7c8b-959b-d1440fb22c0f` ("rework: 分支 01a136a01 落地后 docclass
stale permit 回归 — 修复并重新落地", pending) and `01a136a7-dccd-7092-921b-b71c7be980ea`
(in_progress, working `internal/tools/tasks.go:2825`). The row filed with this
document carries the mechanism their briefings do not.

## The residual exposure, stated plainly

This change clears the current red. It does **not** stop the next one: any landing
whose composition deletes (or restores) a frozen row will mint the same shape, and
the arms in force have now twice failed to act on it inside one day. Counting the
same thing this family already counts, the exposure is not hypothetical — the
table holds `167` permits, every one of which is a row whose deletion leaves the
same incoherence.
