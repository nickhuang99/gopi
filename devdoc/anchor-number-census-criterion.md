<!-- doc-class: historical -->
# The anchor-number census: which detector works for this defect class

Row `01a131d9-83e8-733f-a1a6-58914a9fe9e1`, branch `task/01a131d983e8733fa1a658914a9fe9e1`,
base `0ab59bf`. Written for whoever writes the NEXT acceptance criterion over the
anchor-number census — because the last one was built on a detector that cannot see the
defect, and that cost a whole row.

## The defect class, in one line

Every file under `devdoc/` carries a row in `devdoc/DOC-INDEX.tsv`, and each such row is
read by TWO programs: the **writer** (`internal/docclass/testdata/anchor-number-census.go`)
that generates the freeze table, and the **reader**
(`internal/docclass/anchor_number_baseline_table.go`) that judges it. The defect class is
**write-reader predicate split**: the writer decides by a different (weaker) rule than the
reader judges by, so the writer's own output is refused by the reader — and regeneration,
the documented remedy, reproduces the refusal.

The instance (repaired at `b8ecae820`, an ancestor of `0ab59bf`): the writer kept a stored
reason whenever its PROVENANCE HALF still matched the measurement and then carried the WHOLE
row forward — the CLASS TAIL included — while the reader compared the whole reason. Three
shipped rows were between them, each a permit whose class column had moved while its reason
still described the class it no longer had.

## The detectors, ranked by what they can actually see

### WORKS — 1. Pristine-tree `--check` is green, and it was red before the fix

```bash
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv --check
```

`rc=1` before the fix with `0 unpermitted, 0 stale permit(s), 3 unreproducible reason(s)`;
`rc=0` (`table matches the tree — the freeze is exact`) after. Two properties make it
load-bearing: it is red on the tree the reviewers actually hold (no fixture to trust), and
the pre-fix red was **unrepairable by its own advice** — "regenerate the table" reproduced
the same three rows, because the same half-matching rule kept the same stale tail.

### WORKS — 2. A red row must print `stored != Expected`, both halves from the judgement point

The pre-fix report printed the writer's carried-forward value on BOTH sides
(`produced = reasons[key]`), so three rows read `stored "X", produced "X"` under the verdict
`REASON NOT REPRODUCIBLE`. The verdict was true and the report was unfalsifiable: a reader
sees two equal strings. The fix computes `Expected[key]` where the judgement is made and
prints that. **An acceptance criterion may assert this**: for every red row the two printed
halves differ, and the expected half equals the reason the producer emits for the finding
under the claim the row itself makes.

### WORKS — 3. The artifact's internal coherence: the reason TAIL must name the class its own class column states

`<PROVENANCE HALF>; <CLASS TAIL>`, and the tail is a function of the row's class column.
A row labelled `recorded-line-blank` whose reason says "the recorded text is present in its
file, but not on the recorded line" is self-contradictory **on its own**. No second run, no
reference tree, no regeneration — the check is decidable from the table plus the class→tail
function. The pre-fix table failed it on exactly the three rows the fix moved.

### DOES NOT WORK — "seed a COPIED out path, run the generator twice, diff the runs"

This was asserted for this defect and does not reproduce. MEASURED at the pre-fix base
`e2271f8` (generator + transcript: the row's session artifacts, `faceA-arms.sh` /
`faceA-verdict.md`):

- the pristine table seeded at two distinct copied out paths, 3 runs each: `run1 == run2 ==
  run3` byte for byte **in the data rows**; the only bytes the two DIFFERENT paths disagree
  on are the header line that names the out path;
- two CONSTRUCTED seeds whose provenance half alone was broken (claiming `pre-existing` for
  keys the freeze population does not carry; claiming `first-frozen` for keys it does): the
  writer repaired 12 resp. 306 rows on run 1 and STILL `run1 == run2`;
- the pre-fix writer seeded with the fixed table: `run1 == seed` in the DATA ROWS (byte for
  byte; the whole file differs only in the header lines naming the out path and the scanned-row
  count) — a seed from the rule's own FUTURE is already a fixpoint of it;
- the documented REMEDY, run in place on the pristine pre-fix table: every data row comes back
  byte-identical and `--check` still exits 1.

**Why it is structurally blind** (a property of the pre-fix rule, not of the seed): the
decision consults `(key, provenance half)` and never the tail; what run 1 emits satisfies
that predicate by construction, so **run 1 is a fixpoint of the writer's own keep rule**, for
any seed. The quantity such a criterion measures is therefore identically zero, and the
quantity it observes instead (`seed != run1`) fires precisely when the writer is working
correctly and repairing a bad provenance claim.

The generalization worth keeping: **a criterion phrased as "run the generator twice and diff"
tests the generator against itself, so it can only fail for non-determinism.** Any
predicate-split between a writer and a reader is invisible to it. To test a split, compare
the two predicates' verdicts on the SAME material.

## Verification (Rule 38: the figure names its generator)

```bash
bash <session>/artifacts/faceA-arms.sh <pre-fix-tree> <artifacts-dir>
```
`<pre-fix-tree>` is a DETACHED worktree at `e2271f84c` (= `b8ecae820^`) — not this row's ground,
removed after the arms; every table the script writes lives under the artifacts directory, never
in a tree. Transcripts: `artifacts/faceA-arms.out` (run 1) and `artifacts/repro2-01a131d9.out`
(run 2, a freshly created tree at the same revision), plus `artifacts/arm5-inplace.txt` for the
documented remedy — arm 5 is measured by hand, not by the script, because it deliberately
mutates the tree it runs in (and restores it).

**On the hashes**: a whole-file sha256 here is NOT portable between runs — line 30 is the header
usage line naming the out path. The portable figure is the DATA rows only:
`grep -v '^#' F | sha256sum`. For the pristine pre-fix table and every arm-2 output that value is
`9c2c65ab755f36ce`, in both runs.

After the correction: `go build ./internal/docclass/` OK, `gofmt` clean,
`make test-budget PKGS='./internal/docclass/'` → `outcome=pass exit=0`.
