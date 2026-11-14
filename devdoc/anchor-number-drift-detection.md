<!-- doc-class: historical -->
# Devdoc: an anchor row makes TWO claims — detecting the one nobody checked

**Task:** `01a12d1e-045d-70e8-9186-c548bf7acaee` (branch
`task/01a12d1e045d70e89186c548bf7acaee`)
**Tree measured:** `03f2d1dfe` (this run's own commit), worktree clean at close.
**Census base:** `0b3c1de29` — the commit the frozen population was measured at
(Rule 38: a census figure names its generator and its commit).

## The defect, in one sentence

An `anchor <path>:<line> at <text>` manifest row asserts two things at once —
(1) the file still **carries** that text, (2) the number beside it still
**points at** it — and only (1) was ever checked, so 153 rows whose number
points elsewhere were GREEN under every gate the repo has.

## Why the generator cannot repair it (measured, this run)

`tools/docclass/classify.py:537` emits the cell as

```python
"anchor %s:%d at %s" % (path, n, source_lines[n-1])
```

where **`n` is copied out of the citing doc's OWN prose** (its inline
`path.go:NNN` reference) and the text is then read from the tree **AT** `n`.
The pair is therefore coherent *by construction at write time*: the generator
cannot produce an incoherent row. It goes incoherent by **aging** — one edit
above the referenced line moves the code and leaves the number, and the number's
source (the doc's prose) still says the old line, so a regeneration re-reads the
same number and re-records the same wrong pair.

That is exactly what the briefing's arithmetic showed and this run reproduced:
a regeneration moves 265 anchor rows' **text** and 0 rows' **numbers**.
Nothing was broken; a census was simply never taken.

## What was built

| artifact | what it answers |
|---|---|
| `internal/docclass/anchor_number.go` | "does `path:LINE` still carry the recorded text?" — five verdicts (`agrees` / `number-drifted` / `recorded-line-blank` / `text-absent` / `legacy-line-only`) |
| `internal/docclass/testdata/anchor-number-baseline.tsv` | the frozen population (**153** rows, one per row, with a per-row reason) — DATA, regenerated only |
| `internal/docclass/testdata/anchor-number-census.go` | the generator, with `--check` (Rule 38) |
| `internal/docclass/anchor_number_gate_test.go` | read floor, permit ratio floor, stale permit = **ERROR** (ratchet-down only) |
| `cmd/agent/cli_doccheck.go` | the human-visible surface: reports the drift, fails only on drift NOT in the baseline |

**One grammar, not two.** The detector calls `ParseEvidence` + `Evidence.Text`'s
own `anchoredTextLine` + `anchorTextKey` — it re-spells neither ` at ` nor the
normalisation (Rules 1/13/14). A second spelling of the format would be a second
question wearing the first one's clothes.

**The verdicts are a closed vocabulary** and the values are stored in the
baseline table, so the spellings are a wire format (Rule 1), not display text.

## The decisive design decision: the two questions stay separate

`Evidence.Check` gates the **text** and deliberately tolerates a wrong number
(`internal/docclass/evidence.go`: a master merge moves lines under a healthy
row). That verdict is untouched — a branch-scoped, length-only `doccommit` must
not start failing on a pre-existing population it did not create.

So the new detector answers question (2) and **nothing else**:
`TestAnchorNumberDriftIsReportedWhileEvidenceCheckStillPasses` pins exactly
that — one row, one number that drifted, `Evidence.Check` PASSES it and
`AnchorNumberFindings` reports it. The two answers are allowed to disagree on
the same row, because they are answers to different questions.

**Why `text-absent` is NOT freezeable.** A row whose text is gone is *already
red* under `Evidence.Check`. Folding it into the frozen census would let a red
row hide inside a permit — so `Incoherent()` admits only `number-drifted` and
`recorded-line-blank`, the two classes that are true claims with a stale
accelerator.

**Why the permit key has no manifest line number.** The key is
`doc ⇥ verdict ⇥ path:number ⇥ text`. Keying on the row's line in
`DOC-INDEX.tsv` would invalidate permits whenever an unrelated doc was inserted
above — the table would ratchet by accident rather than by repair.

## The ratchet

`anchor-number-census.go --check` exits **0 iff the table IS the tree**: no
unpermitted row, and **no stale permit** (a frozen row that stopped drifting is
an ERROR, not a harmless leftover — it is a silent permission). Both red arms
were run for real (see the task report's `## Evidence`).

Absent baseline = **abstention**, named as such in `doccheck` — never a pass.
`Scanned == 0` is likewise named rather than reported as coherent (Rule 15).

## Not taken, deliberately

The 660-row whole-corpus regeneration (re-point every drifted number). The
briefing flags it as the owner's policy call, and it is: re-pointing 153
numbers edits 153 docs' *prose* (the number's source is the doc's own inline
reference), destroys the record of how far each claim had drifted, and is a
different task with a different risk profile. This task makes the population
**visible and ratchetable**; repairing it is a decision, not a chore.
