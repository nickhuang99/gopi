<!-- doc-class: historical -->
# The clean-merge docclass half-state, and the lane that now asks for it

**Status:** the merge-time lane is CLOSED for this family, by measurement.
Task `01a13717` (this document is its Tier-B record). Bug row for the original
observation: `01a13717`, filed under Rule 41.

## The shape, reproduced at its smallest

`devdoc/DOC-INDEX.tsv` and the document it classifies are **two paths**, and the
classification invariant is written on both of them: the ROW carries the class,
the markdown file's own first line carries the BANNER. `docclass.BannerDefects`
is the ONE criterion that compares the two halves, and
`TestDocClassBannerMatchesManifest` (`internal/standards/docclass_gate_test.go`)
is the ONE test that runs it over the corpus.

Because the halves are on different paths, a **CLEAN** merge can mint a
self-contradictory result out of two internally-consistent inputs:

| side | its own diff | its own tree |
|---|---|---|
| A — the banner pass | `devdoc/<doc>.md` only | consistent (row already says `historical`) |
| B — the reclassification | `devdoc/DOC-INDEX.tsv` only | consistent (file carries no banner) |
| merge | **no conflict anywhere** | row `normative` + file banner `historical` |

No conflict-keyed lane can see it, because there is no conflict: git merges per
path and takes both halves untouched.

## The measurement (real merge `476e47ede`)

The real instance, measured on the real revisions:

```
$ git merge-base 476e47ede^1 476e47ede^2   → 2f697e5ede0ebac1eb4e360e049e9e3881bae012
       base 2f697e5ed   row=historical  line1=# Pure-read ledger faces…   (no banner)
  side A 476e47ede^1    row=historical  line1=<!-- doc-class: historical -->
  side B 476e47ede^2    row=normative   line1=# Pure-read ledger faces…   (no banner)
  merge  476e47ede      row=normative   line1=<!-- doc-class: historical -->   ← the contradiction
$ git ls-files -u 476e47ede | wc -l        → 0            (no unmerged entry, stage-0 only)
$ git diff --name-only 476e47ede^1 476e47ede   → devdoc/DOC-INDEX.tsv   (the legs mirror)
$ git diff --name-only 476e47ede^2 476e47ede   → devdoc/ledger-readonly-faces.md + 24 more
```

The union the measurement reads is therefore the MANIFEST ALONE (measured through
`mergesetGendocUnion`, verbatim: `union=[devdoc/DOC-INDEX.tsv]
documentTouched=false legs=[first-parent c06e04e22… second-parent cbfe03583…]`).

### What the leg did BEFORE this change

`mergesetDocclassRuns` **did fire** on that merge — the manifest is one of the
family's gated documents by construction, so any landing that moves a row is a
hit (`1 path(s) in the union are anchored by a manifest row (1 gated
document(s))`). It then returned:

```
{Runs: [{Gate:devdoc-anchors, Argv: [go test -count 1 ./internal/docclass], Packages: [./internal/docclass]}]}
```

One package. And `go test ./internal/docclass` is **green** on that tree — the
criterion's *code* lives there, the corpus-facing *test* does not. So the
half-state passed the merge-time lane green.

### What the gate whose name the leg borrows does say

Run over the merged tree, the gate that enforces the row↔banner criterion:

```
$ go test ./internal/standards -run TestDocClassBannerMatchesManifest   (at 476e47ede)
    docclass_gate_test.go:212: devdoc/ledger-readonly-faces.md: carries the "historical" banner
        but devdoc/DOC-INDEX.tsv classes it "normative" — only [historical] classes are bannered
        (a banner on an unclaimed class claims something the doc does not)
    docclass_gate_test.go:222: devdoc/DOC-INDEX.tsv: 1437 markdown docs of a bannered class carry
        their banner (1 mislabelled)
FAIL
```

RED — in a package the leg never asked for. That is the whole defect: **the lane
fired and asked the wrong package.**

### The other legs, for completeness (measured, all abstained)

```
touched-gate-packages: []   (the landing's own diff names no *.go under the three fixed gate dirs)
gendoc/union leg: "the landing's union touches no published anchor source (1 path(s) measured;
    parent leg(s) read: first-parent c06e04e22…, second-parent cbfe03583…) — nothing to re-run"
```

## The fix: the leg asks for TWO packages, in the existing leg

No new tool, no new script, no new resident dependency (Rule 3/35) — the seam is
the measurement that already runs on every landing.

- `internal/docclass/docclass.go` gains **`ManifestGatePackageRel`**, the
  repository-relative package that carries the manifest's OWN gates. Named in the
  leaf (Rule 14) because two consumers need one spelling: the leg, and the test
  below.
- `internal/tools/mergeset_docclass.go` appends `./<ManifestGatePackageRel>` to
  the run set beside the derived carrier. **The trigger is unchanged** and stays
  union-scoped (Rule 27): the second package is owed only on landings whose union
  already named the family — which, by the same argument that made the manifest a
  gated document, is every landing that can mint this half-state.

### The one shape that was rejected, and why it is recorded here

The first spelling of the second package **gated it on `os.Stat`**: present in the
landing's tree → join the run set, absent → abstain naming the missing directory.
It was removed, because the stat can only ever answer "present": the union that
reached this point already contains the manifest, and any tree that has the
manifest has `internal/standards`. So the arm was dead code in the only trees this
leg runs on, and — worse — a tree that had the manifest and *not* the gate package
would have been told "nothing was obliged" instead of being asked the question. A
gate the tree does not carry must fail at the toolchain's own price, not be
silenced by a stat.

### What it costs

One more package, on the landings that already oblige the family. `internal/tools`
and `internal/docclass` are in the ordinary branch-scoped smoke path; the
`./internal/standards` suite is the expensive one, and it is now paid ONCE more
per such landing — against a family whose repair loop has been walked per-file at
least seven times. Measured wall time of that suite on this host: ~146 s cold,
~7.7 s for the single banner gate.

## The regression test

`internal/tools/mergeset_docclass_test.go`:
`TestMergesetDocclassAsksTheManifestGatePackageForTheCleanMergeHalfState`.

It builds the scenario with the fixture's own git — a base where the row and the
file agree, side A moving only the banner, side B moving only the row, merged from
the same base — then asserts, in order:

1. the landing is **conflict-free** (`git ls-files -u` empty);
2. **both halves landed**, each from its own branch;
3. the result **contradicts docclass's own criterion** — judged by
   `docclass.BannerDefects`, the tree's reader, never a second opinion;
4. the run set names the derived carrier **and** the manifest's gate package, and
   the argv ends with the same list.

**Red→green, measured.** With the leg reverted to `HEAD` (constant kept),
step ④ fails:

```
    mergeset_docclass_test.go:458: the clean merge minted a self-contradiction (the gate's own
        sentence): devdoc/md-half-state.md: carries the "historical" banner but devdoc/DOC-INDEX.tsv
        classes it "normative" — only [historical] classes are bannered
    mergeset_docclass_test.go:476: the run set must name the carrier AND the manifest's gate package
        [./internal/docclass ./internal/standards], got [./internal/docclass]
```

With the fix in place, all five `MergesetDocclass*` tests pass and the leg on the
real merge `476e47ede` returns
`Argv: [go test -count 1 ./internal/docclass ./internal/standards]`.

## What this does NOT close (honest)

- **Other invariants with the same two-path shape.** The lane is closed for the
  row↔banner pair, because that pair is the family this leg already reads. A
  different pair (say a baseline table and the generator that produces it) would
  need its own membership argument — the leg's trigger is a membership test, not
  a general conflict-keyed detector.
- **Landings that never reach this measurement.** A ref that arrives by another
  route (`git fetch`, a manual push, a branch landing without the merger's pass)
  does not run the merged-result step at all. This lane's coverage is exactly the
  coverage of `RebuildAfterMergeMergeset`.
- **The 1 mislabelled doc at `476e47ede`** was repaired by a later landing, not by
  this change (this change makes the repair *owed*, it performs nothing). The
  current tree is green for `TestDocClassBannerMatchesManifest`.
