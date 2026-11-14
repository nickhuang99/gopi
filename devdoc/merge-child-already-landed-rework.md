<!-- doc-class: historical -->
# The already-landed flag: reconciling a HALF-LANDED feature (rework of task 01a1315b…)

**Task**: `01a13273e1fc783d804cef77c947debe` — rework of
`task/01a1315ba35a731682a8b4919d3e63f0` (tip `2cbb06ddd`, recovered as the tag
`rework-backup/01a1315ba35a731682a8b4919d3e63f0`).
**Ground**: `/home/user/workspace/gopi-01a13273e1fc783d804cef77c947debe`, cut at `fea0309`.
**Commit**: `1d8867e99` on `task/01a13273e1fc783d804cef77c947debe`.

## The shape of the defect: HALF-LANDED, which is worse than unlanded

The briefing said master already advertised `--already-landed NAME` on `gopi merge -h`, "landed
by a sibling branch". **That is not what the measurement showed, and the difference changes the
repair.**

| asked of | answer |
|---|---|
| `git show master:cmd/agent/merge_run.go \| grep -c -- already-landed` | **0** |
| `git show master:man/gopi-merge.md \| grep -c -- already-landed` | **0** |
| `git grep -c -- '--already-landed' master -- .` | **no file** |
| `git log --all -S'mergeAlreadyLandedFlag' -- cmd/agent/merge_run.go` | **exactly one commit: `2cbb06ddd`** (the origin branch itself) |

So nothing about the arm had reached master — not the parser, not the help row, not the man page.
The premise "the help face already landed" was **not** confirmed by the object store; the arm has
never existed on master under any spelling. The one thing that *did* exist and had to be preserved
is master's **newer `--status` prose** (`3217781bc`, the `BURIED` classification from the archive
read side), which sits in the SAME usage-head region the arm adds a row to.

**Why this matters beyond bookkeeping**: a briefing premise is what the executor plans against.
"Half-landed" would have meant *find and reuse the landed half*; the measurement meant *land the
whole thing while preserving a neighbour that moved under it*. Both end at the same file, but only
one of them tells you to check the neighbour.

## The three conflicts, and the two extra ones the merge produced

```
$ git merge --no-commit --no-ff rework-backup/01a1315ba35a731682a8b4919d3e63f0
CONFLICT (content): Merge conflict in cmd/agent/merge_run.go
CONFLICT (content): Merge conflict in internal/docclass/testdata/anchor-number-baseline.tsv
CONFLICT (content): Merge conflict in man/gopi-merge.md
```

The epic named the last two. The merge produced two more, which the epic did not name and which
are the same two families:

1. **`merge_run.go`'s usage head** — master's `--status` row grew; the arm's row lands beside it.
   Resolution: keep master's prose verbatim AND append the arm's rows (no side dropped).
2. **`devdoc/gopi-tools-maintenance-help-surface-inventory.md`** — both sides numbered their new
   form **85**. This file auto-merged (a Markdown table) and therefore carried a silent duplicate
   row number; it is a GENERATED file, so it was re-emitted, which renumbers the whole table.

## The rule this run re-learned: a generated file is re-emitted, never hand-merged

Each conflict in a generated file was resolved by **taking one side to get a valid tree, then
re-running the file's own declared generator against the merged tree**:

| file | how it was re-emitted |
|---|---|
| `man/gopi-merge.md` | `go run internal/standards/testdata/mancensus.go . bin/gopi` (needs `bin/gopi` built first) → `regenerated` |
| `internal/docclass/testdata/anchor-number-baseline.tsv` | `go run internal/docclass/testdata/anchor-number-census.go . <table>` then `--check` → `the table matches the tree — the freeze is exact` |
| `devdoc/gopi-tools-maintenance-help-surface-inventory.md` | `bash scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi` + `python3 scripts/g6-inventory-doc.py <surface.tsv> cmd/agent <out>.md` → `178 rows, 174 distinct forms, 143 typed, 31 blocked` |

**The docclass generator REFUSES a table still carrying conflict markers** — measured:

```
anchor-number-census: internal/docclass/testdata/anchor-number-baseline.tsv: row 1 does not carry
 the generated shape (<doc><TAB><verdict><TAB><anchor><TAB><text><TAB><reason>)
```

So the order is: `git checkout --ours <table>` (a syntactically valid, if stale, row set) → run
the generator → run `--check`. Skipping the first step makes the generator fail rather than
half-write, which is the property that makes it safe to run at all.

**One row CHANGED VERDICT, and it is not a hand-edit.** The merged tree moved
`cmd/agent/merge_run.go:343`, so `devdoc/merge-command.md` and `devdoc/merge-union-lane.md` moved
from `number-drifted` to `recorded-line-blank` — their recorded text still sits in the file, but
the recorded *line* now points at a blank one. The regeneration recorded that; nothing was typed.

## The help-page LENGTH budget is part of landing a flag

Adding the arm's five lines to `gopi merge -h` reds `cmd/agent/cli_usage_budget_test.go`
(`mergeUsageBudget`), so the constant went **167 → 172** with the reason beside it. Two things
about that gate are worth remembering:

- The margin is `usageBudgetNewFlagMargin = 12`, and the cap may not be **SLACK** either — a page
  measuring more than 12 lines *under* its cap is equally red. So the raise is exactly the page's
  measured length, and the reason must be "a new flag's paragraph", which this is.
- **The usual remedy is unavailable here.** The budget's own error text says "the page routes, the
  man page carries the contract" — but `man/gopi-merge.md`'s SYNOPSIS region is COPIED from `-h`
  by `mancensus.go`. A row that leaves the page leaves the man page with it, so the flag would
  become invisible on both faces. The raise is the only honest option, and it is written into the
  constant.

## One dead constant removed

The origin branch carried `mergeLandedUsageLine` — a raw-string fragment of the very row that
already sits in `mergeUsageHead`. Measured: **it had no reader on the tag either** (`git grep -n
mergeLandedUsageLine <tag>` → 2 hits: the declaration and the tag's own report prose). Left in
place it would have been a second, drifting copy of a usage row, and the help-width sweep would
have measured it. It is deleted; the row lives once.

## Recurrence — what a future reader of this file should do

`git merge --no-commit --no-ff <the-rework-tag>` against master now reports
`Automatic merge went well` with **zero** conflicted files (measured in a clone; the clone lives in
a sandbox, not in the source tree). If it conflicts again, the reason will be a generated file whose
input changed — re-emit it with the generator named in its own header, never by hand.
