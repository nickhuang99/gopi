<!-- doc-class: historical -->
# The anchor-NUMBER freeze table's repairs were real; its GENERATOR's face had no real-tree test

**Status:** a measurement record of one run (2026-10-14, task
`01a135ec-58c4-77c1-bdb2-3e8c8e1eb303`, peerbox), classified **historical** by the
corpus's own classifier — the doc's prose reads as a run's account. It records a
verdict on a stale briefing, and the one guard that verdict left behind.

## 1. The briefing's premise, measured

The row claimed `gopi tool doccheck` exits 1 repo-wide on master because
`devdoc/smokegate-scratchroot-pin-unmerged.md` carries an anchor-number drift
(`internal/config/config.go:413`, recorded text now at `:429`) that is **absent
from the baseline table**.

Both halves of that premise are stale, and the run measured each one:

| claim | measurement (this worktree = master) | verdict |
|---|---|---|
| the row is absent from the table | `grep -n smokegate-scratchroot-pin-unmerged internal/docclass/testdata/anchor-number-baseline.tsv` → **line 152**, `number-drifted` | FALSE |
| `gopi tool doccheck` exits 1 | `./bin/gopi tool doccheck` → **exit 0**, "167 of 167 are frozen" | FALSE |
| the census `--check` is red | `go run internal/docclass/testdata/anchor-number-census.go . … --check` → **exit 0**, "table matches the tree — the freeze is exact" | FALSE |

The repair is commit **`da8d4e72a`** ("regenerate the freeze table for the drift
master made by itself"), and it is on master. Its delta is exactly one permit:

```
$ git show da8d4e72a -- internal/docclass/testdata/anchor-number-baseline.tsv
-# figures: frozen=166 pre-existing=152 first-frozen=14
+# figures: frozen=167 pre-existing=152 first-frozen=15
+devdoc/smokegate-scratchroot-pin-unmerged.md	number-drifted	internal/config/config.go:413	// whitelist): a client node with task_sync=false still hands its bran	first-frozen at 0b3c1de29; …
```

`git show da8d4e72a^:<table> | grep -c smokegate-scratchroot-pin-unmerged` → **0**:
the row was genuinely absent one commit earlier, so the briefing was accurate when
it was WRITTEN and stale when it was DISPATCHED. `git diff master HEAD` is empty —
this worktree IS master.

## 2. Why it went stale — the same family, three instances in one evening

The row was submitted **14m53s after** its own repair commit was authored. This is
the stale-brief shape already tracked in this ledger:

- `01a134c6` — "Stale-red briefing (2nd instance of the #12965 shape)" (completed)
- `01a13534` — "Stale brief #3" (completed)
- `01a135ff` — "stale-brief shape, 4th instance: bug row 01a135ec was submitted
  14m53s AFTER its own repair commit da8d4e72a was authored" (pending, tags=bug)

This run's row is the fourth instance, and `01a135ff` already carries the
disposition — it names this task id in its summary, so the bug-flow entry for this
report already exists and this run adds evidence to it rather than filing a fifth.
The mechanism (a brief's decision command is never re-run at submit time) is not
this run's to repair (Rule 28: system layer).

## 3. The gap the run did close: the generator's face was never pinned

The package documents that TWO surfaces ask "is the committed freeze table still
the tree?" and that they **spell the comparison twice, neither calling the other**
(see the note above `computeAnchorNumberDrift`, `internal/docclass/anchor_number_baseline.go`):

- the **VERB's** face — `computeAnchorNumberDrift`, reached by `gopi tool doccheck`
  through `AnchorNumberDrift`;
- the **GENERATOR's** face — `anchorNumberCompareLines`, reached by
  `anchor-number-census.go --check`.

Two implementations of one question is a deliberate structure, and it is only safe
if **both** are pinned against the real tree. Only the verb's face was:
`TestAnchorNumberGateOnTheRealTree` walks the shipped corpus against the shipped
table, and it is a genuine guard — swapping the pre-fix table in turns it red
(measured below). The generator's face had no real-tree test at all; it ran on the
suite path only when a human typed the command.

That gap is not hypothetical, because a **sibling** rung already closed it for
itself: `prose_path_line_one_verdict_test.go` runs the prose-path-line generator
with `--check` over the real corpus (`exec.Command("go", "run", …)`). The
anchor-NUMBER rung was the one that never got the twin.

### 3.1 The guard, and its red proof

`internal/docclass/anchor_number_shipped_face_test.go` adds the twin:

- `TestAnchorNumberCensusShippedFaceIsExactOnTheRealTree` — runs the SHIPPED
  generator with `--check` over the SHIPPED tree and table, and requires exit 0
  **and** the generator's own exact-arm sentence. The exit code alone would pass
  for a program that compares nothing (Rule 15), so the sentence is required too.
- `TestAnchorNumberCensusShippedFaceFiresOnAStaleTable` — the RED half, and what
  makes the above a gate: the same generator, the same tree, a copy of the shipped
  table with ONE permit deleted. It must exit non-zero and say "the table is not
  the tree". A generator whose comparison never fires is caught here.

Both halves were measured, and so was the red proof — the pre-fix table
(`da8d4e72a^`) swapped in, against the shipped tree:

```
--- FAIL: TestAnchorNumberCensusShippedFaceIsExactOnTheRealTree (21.16s)
    the shipped generator's --check is RED on the shipped tree (exit 1) — the committed
    freeze table is not this tree's fixpoint. Regenerate it (never hand-edit, Rule 38):
          NOT PERMITTED (drifts, absent from the table): devdoc/smokegate-scratchroot-pin-unmerged.md …
```

## 4. What is NOT claimed

- **The escape mechanism is not repaired here, and this doc does not imply it is.**
  `devdoc/doccheck-anchor-number-master-drift-escape.md` (§7.3, §7.4) states it: a
  landing can make master red unilaterally, because the docclass leg of
  `RebuildAfterMergeMergeset` is report-only and the refusing merge-time gates
  abstain when the red is not attributable to the incoming branch. Rows
  `01a135da` and `01a135de` carry that, both requiring peerbox (Rule 28).
- **No new merge-path gate is added.** The guard above is a suite test; it catches
  the escape **after** it lands (the suite runs on the landing's own branch and on
  the verify path), not at merge time. Closing the merge-time hole is a different
  change, on a different host.
- **The row's disposition is not this report's to write.** `01a135ff` is the open
  bug row for the stale-brief shape and already covers this instance by name.

## 5. Evidence

Verbatim commands and outputs of every measurement above are in this run's session
directory: `~/.gopi/sessions/01a135ec-58c4-77c1-bdb2-3e8c8e1eb303/artifacts/`,
beside `task-report.md`. The pre-fix table used for both red proofs is preserved
there as `table-at-da8d4e72a^.tsv` (203 lines), so the red is reproducible without
checking out the parent commit.
