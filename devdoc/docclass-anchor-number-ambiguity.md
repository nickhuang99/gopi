<!-- doc-class: normative -->

> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# The anchor-number freeze counts text matches — a text two lines satisfy is not a determination

**Task**: `01a13280-8c5f-7cc6-bfc7-af6655ea80a6` (re-implementing the intent of
`01a131fd-bc0b-70b9-a007-6ff07636cf95`, whose branch could not be merged).
**Base commit** for every figure below: `43541ad8e5f59549e1b9189e0d64084d5314fe58`.
**Figures' generator**: `go run internal/docclass/testdata/anchor-number-census.go . \
internal/docclass/testdata/anchor-number-baseline.tsv` (Rule 38 — the census
figure names the program that prints it, not a hand count).

## The defect the criterion closes

The anchor-number rung asks two questions about a manifest row
`anchor <path>:<line> at <text>`:

1. does the file still carry `<text>`? — asserted by `Evidence.Check`;
2. does `<line>` still carry it? — asserted by the drift detector.

Both are answered through ONE containment predicate over **normalised** lines
(`Evidence.anchorLineSatisfies`: a line satisfies the text when its normalised
form CONTAINS the normalised text — the two measurements that force normalisation,
and the two graded lines that force containment rather than equality, are in
`anchoredTextLine`'s comment). What was never asked is **how many** lines satisfy
it, and for a short recorded text that is the question that decides whether the
line number means anything.

Measured on this base: of the **167** permits the shipped table held, **28**
recorded a text their own file satisfies on two or more lines — `}` five times,
`//` three times, a bare `return nil` twice. For such a row:

- **the accelerator's answer is a function of unrelated edits.** The text is on
  the recorded number and somewhere else too, so *deleting the very line the cell
  cited* leaves a file the predicate still satisfies (another `}` takes the
  number) and the verdict flips to `number-drifted` **while the permit stays
  green**. A permit whose site was DELETED silently passes — the arm that exists
  to notice a stale line number reports nothing about the one case it is for.
  The two-line fixture in `anchor_number_ambiguity_test.go`
  (`TestAnchorNumberDeletedSiteIsSilentBeforeTheCountIsAsked`) builds exactly
  that state and runs the pre-fix order over it: it answers **AGREES**.
- **and claim (1) is not being tested either**: "the file still carries this
  text" is trivially true of `}` in any Go file, taken from whichever line
  happens to carry it.

## What changed

| surface | change |
|---|---|
| `internal/docclass/evidence.go` | `anchorLineSatisfies` (the ONE predicate, now a function so `anchoredTextLine` and the count cannot disagree) + `anchorTextMatchCount` |
| `internal/docclass/anchor_number.go` | `AnchorNumberAmbiguous = "text-ambiguous"`; `AnchorFinding.MatchCount`; the count is asked **before** the number, `>1` → the undecidable class; `AnchorNumberVerdictCount` (the ONE counter) |
| `internal/docclass/anchor_number_baseline.go` | `AnchorNumberDriftReport.Ambiguous`; ambiguities leave the frozen population, so a permit that stops being incoherent goes **stale and is named** |
| `internal/docclass/anchor_number_baseline_table.go` | a reason tail for the class (a reason that described ambiguity as drift would send a reviewer after a numbering edit that did not happen) |
| `internal/docclass/anchor_number_baseline_header.go` | `ambiguous=` is a declared header figure, read and compared through ONE key list (`AnchorNumberHeaderFigureKeysInOrder`) |
| `internal/docclass/testdata/anchor-number-census.go` | counts + prints the class on every arm, writes it into the header, re-derives it in `--check` |
| `cmd/agent/cli_doccheck.go` | reports the rows and **counts them on every run**; the all-coherent line is scoped to the decided population and is not printed over undecidable rows |
| `internal/docclass/anchor_number_gate_test.go` | `anchorNumberAmbiguityFloor` — a floor on the class, so "found none" and "never looked" stop looking alike |

**The rung itself is unchanged.** Matching is still normalised substring; this is
not a tightening, and the decisions behind that rung (with their measured costs)
are in `devdoc/docclass-anchor-residual-decision-20261012.md`.

## The regeneration, and the numbers it moved

The table is generated, never hand-merged (Rule 36/38):

```
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv
→ 253 anchor row(s) scanned, 140 incoherent (16 first-frozen, 124 pre-existing)
  the write's own delta: 1 permit added, 28 removed
  # figures: frozen=140 pre-existing=124 first-frozen=16 ambiguous=38
```

The 28 removed are precisely the permits whose text is ambiguous; the 1 added is
a row that was ambiguous before and is not now. `gopi tool doccheck` on a real
tree agrees with both faces of the count:

```
doccheck:   text-ambiguous rows (reported above, never frozen):  …
doccheck:   38 of 253 scanned anchor row(s) are text-ambiguous (reported, not
            frozen — a unique-text row is the only one a permit can cover)
doccheck:   140 of 140 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv
```

## The consequence, stated so it is not read as a regression

A row that was frozen while its text was unique and whose text **becomes**
ambiguous has no incoherent finding left, so its permit goes **stale** and the
ratchet names it — instead of quietly continuing to cover a verdict that no
longer means anything. Regression proof: the SAME new binary run against the OLD
shipped table reports **28 `STALE PERMIT`** rows (one per dropped permit) and no
`NOT PERMITTED` row; against the regenerated table it reports **0** stale and
`140 of 140 frozen`.

## Boundaries and honest gaps

- The verb's drift arm exits 1 only on **stored-cell drift** (776 of 2507 rows on
  this base, pre-existing on `master` and untouched by this change). The
  ambiguity class never fails the run: it is a property of the rung, not a defect
  of the tree.
- `tools/docclass/classify.py` is the manifest's writer and was **not** modified;
  `devdoc/DOC-INDEX.tsv` regenerated byte-identical (`git status` clean for it).
- The gate's `anchorNumberAmbiguityFloor` is a floor (20), not a count: repairing
  the corpus's one-word anchors is a legitimate direction and must not red the
  gate, while a detector that stopped asking the question must.
