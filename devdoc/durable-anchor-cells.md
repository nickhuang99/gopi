<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Durable anchor cells — a line number in the manifest was a conflict generator

Task `01a136a0-1d5b-772f-83af-f1d2a14825bd`, base `2f697e5ed`, host `hosta`.

## The symptom, and why the obvious reading was wrong

`devdoc/DOC-INDEX.tsv` is a recurring git-conflict magnet. The first reading is
"somebody keeps hand-editing generated data" — **wrong**: the file is produced by
`tools/docclass/classify.py`, and no writer hand-edits it (Rule 36). The reading
the epic started from was "the generated cell embeds a line number, and the number
drifts". That was **also incomplete**, and the measurement is what showed it.

## What the measurement showed

The anchor cell used to read `anchor <path>:<line> at <text>`. Two halves, and
each behaves differently:

* the **number** is not re-derived from the tree at all — it is read from the
  *document's own prose* (`ANCHOR_RE`, `` `path.go:547` ``), so it is frozen into
  the row and never moves on its own;
* the **text** is read **live** from the tree, at that frozen number
  (`lines[n-1]`, `code_spotcheck`).

So a line inserted **above** the cited site leaves the number alone and changes the
**text**. Measured on a three-line fixture (the same shape as the acceptance test):

```
BASE (classify.py at HEAD)  [RED proof — what master does today]
  run 1 — cell stored when the anchored text is at line 3:
    anchor internal/probe/probe.go:3 at func Target() { os.Exit(1) }
  run 2 — cell after 2 lines were inserted ABOVE the anchored text:
    anchor internal/probe/probe.go:3 at // filler
  BYTE-IDENTICAL (row needs no rewrite)? False
```

Every row that re-derivation rewrites is a row two runs can both rewrite, and two
runs rewriting the same manifest bytes is the conflict. On the real corpus:
**169 of 240** anchor rows render a different cell than the one they carry — while
the row's claim ("this file still carries this line") is intact in **all 169**
(the recorded text is still present in the file; it just is not on the recorded
line any more).

## The fix: the cell is a function of the DOC, and resolution moves to QUERY

1. **The number is no longer written** (`evidence.go`: `anchor <path> at <text>`),
   and the `sym` cell's trailing `at <path>:<line>` is gone with it. The number is
   still *read*, for exactly one purpose: deciding whether the doc's `path.go:NNN`
   mention points at real code. It is not stored. This is the epic's premise, and
   it is what the `Evidence.Check` anchor arm already assumed — it resolves an
   anchor by its recorded **text**, abandoning the number on its first miss,
   because the number is the only part of the pair a merge moves.
2. **The recorded text is carried forward.** Dropping the number alone is **not
   enough**, and this is the finding that changed the design: with a number-free
   cell the text is still read at the doc's frozen line, so a displacement still
   rewrites the row. `stored_anchor_text` reads the row's existing claim back and
   hands it to the emission as `prior`; when that text is still present anywhere in
   the anchored file, the row re-emits **byte-identically** — a 0-line diff, which
   cannot conflict. When it is **not** present, the claim is not carried: the row
   re-derives and `Evidence.Check` reports text-absent, which is the anchor tier's
   red.

```
FIXED (this branch)  [GREEN proof — what this branch does]
  run 1 — cell stored when the anchored text is at line 3:
    anchor internal/probe/probe.go at func Target() { os.Exit(1) }
  run 2 — cell after 2 lines were inserted ABOVE the anchored text:
    anchor internal/probe/probe.go at func Target() { os.Exit(1) }
  BYTE-IDENTICAL (row needs no rewrite)? True
```

**Conflict surface removed: 169 → 0 of 240 anchor rows.**

## One consequence worth naming: the detector's verdict vocabulary

`internal/docclass/anchor_number.go` classifies an anchor cell's number coherence.
A number-free cell has `Number == 0` (no number to parse) while its text is present
(`TextAt != 0`), so before this change it fell through to the number comparison and
was reported **`recorded-line-blank`** — a false drift report for every healthy
number-free row. That matters because the freeze table's direction is **DOWN**:
blessing a false report would have added permits for rows that are fine forever.
So the vocabulary gained `AnchorNumberNumberless` ("number-free"), checked
**before** the number comparison, and `TestAnchorNumberNumberlessCellIsNotReportedAsDrift`
pins it — including the non-vacuity arm (a number-free cell whose text is really
gone is still `text-absent`), so the class cannot be used to mute the tier.

## What was deliberately NOT done

* **No bulk regeneration of `DOC-INDEX.tsv`.** 2493 rows in one write is a one-shot
  conflict storm; the manifest is DATA and whole-corpus writes are an owner's
  decision (Rule 36). Legacy `anchor <path>:<line> at <text>` rows are still
  *read*, so the transition needs no rewrite at all — the change is in what the
  generator *writes*, not in what is already written.
* **`internal/docclass/testdata/anchor-number-baseline.tsv` was left alone.** One
  round of observation first; the freeze table is a decision about drift that
  predates it, per its own header.
* **No new script, hook or scheduled mechanism** (Rule 35) — only the existing
  generator's emission changed.

## A pre-existing defect this work met (and did not cause)

The two `internal/docclass` tests `TestAnchorNumberGateOnTheRealTree` and
`TestAnchorNumberCensusShippedFaceIsExactOnTheRealTree` were **RED on a pure base
tree** (all of this task's files reverted, `git status --porcelain` empty): a
single row, `devdoc/merge-backlog-terminal-guard.md#1212`, recorded
`anchor internal/tools/tasks.go:2825 at }` — a text so non-specific (`}`, one
brace) that the detector's first-occurrence comparison had drifted out of the
167-row freeze table. Re-deriving **that one row** through the generator's own
incremental mode (the gate's own prescribed remedy — regenerate, never hand-edit)
repairs both: the row becomes `anchor internal/tools/tasks.go at }` and both tests
go **green**. The unrelated pre-existing banner defect in the same package is filed
separately (see below).

## Files

| file | what changed |
|---|---|
| `tools/docclass/classify.py` | anchor cell emits `anchor <path> at <text>`; `sym` loses its number; `stored_anchor_text` + `prior` carry a surviving claim forward; grammar tokens (`ANCHOR_TIER`, `ANCHOR_TEXT_SEP`, `DRIFT_TOKEN`) spelled once |
| `internal/docclass/evidence.go` | the ONE parser reads both shapes (the discriminator is the *number*, not the colon); `isLineNumber`; a number-free cell recording no text is refused rather than passed |
| `internal/docclass/anchor_number.go` | new verdict `AnchorNumberNumberless`, checked before the number comparison |
| `internal/docclass/anchor_number_test.go` | the new class + its non-vacuity arm |
| `internal/docindex/anchor_cell_stability_test.go` | the acceptance test: two arms through the real generator |
| `tools/docclass/classify_test.py` | three tests pinned the old contract (a rendered number); re-pointed at the new one, each keeping its protective intent |
| `devdoc/DOC-INDEX.tsv` | ONE row re-derived through the generator |

## How a reader re-checks this

```bash
go test ./internal/docindex/ -run TestAnchorCellSurvivesASourceLineMove -v   # the two arms
go test ./internal/docclass/ -run 'AnchorNumber|AnchorCell'
python3 tools/docclass/classify_test.py                                      # 28 tests
```

The red half is reproducible without this branch: the same fixture, driven by
`git show HEAD:tools/docclass/classify.py`, returns
`anchor internal/probe/probe.go:3 at func Target() { os.Exit(1) }` and then
`anchor internal/probe/probe.go:3 at // filler` — two different cells for one
unchanged claim.
