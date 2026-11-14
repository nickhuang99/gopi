<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The anchor excerpt is a fixed point of its own render

**Class**: historical — the generator's own verdict (this doc records one run's
measurement, and the corpus index is DATA the generator writes, never hand-edited).
The standing claim it demonstrates lives in the code: `classify.py:anchor_excerpt`
and the guards named at the end of this file.

## The defect

`tools/docclass/classify.py` records, for the `anchor` evidence tier, a PREFIX of
one source line: `lines[n - 1].strip()[:70]`. When column 70 lands inside a run
of whitespace the cell keeps that whitespace — and **both** readers of the cell
drop it when they read it back:

* `docclass.anchorRecordedText` (`internal/docclass/evidence.go:263`) → `TrimSpace`
* `stored_anchor_text` (`tools/docclass/classify.py:17`) → `.strip()`

So the row the generator wrote is **not a fixed point of its own render**:

| pass | written cell (tail) |
|---|---|
| 1st `--only` (no prior text to carry) | `…started (zero ` |
| 2nd `--only` (prior text, stripped) | `…started (zero` |
| render / `--check` | `…started (zero` |

The writer oscillates once and then settles on the stripped form, while the
**render** always strips — so `--check` (a byte comparison of one render against
the stored cell) counted such a row drifted **forever**, and `gopi tool doccheck`
(which compares Cell-normalised, `strings.Join(strings.Fields(s), " ")`) reported
that same corpus as clean. Two faces, one corpus, opposite verdicts by exactly
those rows.

Measured 2026-10-14, before the fix, on the two rows the epic names:

```
$ python3 tools/docclass/classify.py . --only devdoc/verb-double-spelling-slice.md   # pass 1
  written = 'anchor cmd/agent/cli_websearch.go at The flat spelling gopi web_search is RETIRED, and this binary refuses '
  rendered= '…this binary refuses'
```

The source lines both cut on a space: `cmd/agent/cli_tasks.go:3531` (column 70 is
the space before `the`) and `cmd/agent/cli_websearch.go:90` (column 70 is the
space before `it`).

## The repair

**The WRITER is the side that stops emitting the noise** — one named constant and
one function, no change to either reader:

```python
ANCHOR_TEXT_MAX = 70          # the recorded excerpt's ceiling

def anchor_excerpt(line):
    return line.strip()[:ANCHOR_TEXT_MAX].rstrip()
```

Both readers already treat the stored cell as normalised. Dropping the space on
the READING side instead would leave the manifest carrying bytes no reader would
ever keep, i.e. a stored claim that is not the claim. Stripping at the cut puts
the values a reader KEEPS and the values the writer STORES in one set.

`line.strip()[:N]` (not `line[:N].strip()`) is deliberate and unchanged from the
old code: the leading indentation of a source line was never part of the excerpt.

## What this does NOT do

The corpus still carries **699** byte-drifted rows (`classify.py . --check`), the
same 699 before and after this change, field-differing as
`{reason: 588, evidence: 532, class: 194}`. That population is **semantic drift**
(the quoted source line or the cited artifact moved on), not the cut's
whitespace — it is the repair queue `--only` exists for, and it is orthogonal to
this fix. The fix's whole measured effect on the corpus is: no row's stored cell
moves, and the two named rows become byte-identical fixed points.

## The regression guard, and where it lives

Two guards, because the Python one is reachable from no suite entry point:

* `tools/docclass/classify_test.py` —
  `TestTheAnchorExcerptIsAFixedPointOfItsOwnRender`, built on a fixture whose
  cut lands on whitespace **by construction**. Not referenced by any Makefile or
  `.sh` in this repo; it is run explicitly (`python3 tools/docclass/classify_test.py`).
* `internal/docindex/docclass_fixpoint_test.go` — the same acceptance driven
  against the repo's own script on a throwaway repository, so it rides the Go
  suite. This is the guard that runs on the merge path.

Both go RED against the pre-fix emitter and GREEN against the fixed one:

```
# pre-fix emitter (lines[n-1].strip()[:70])
FAIL: TestTheAnchorExcerptIsAFixedPointOfTheGeneratorsOwnRender
  the stored anchor cell ends in whitespace: "…aaaaaaaa       "

# fixed emitter
ok  github.com/nick/gopi/internal/docindex
```

The Go fixture does **not** name the cut length: it puts a 20-space run between
the filler and the tail, so the property holds for any cut column inside the run
and a change to `ANCHOR_TEXT_MAX` cannot quietly turn the guard into a test of
nothing. The non-vacuity arm (`TestTheFixtureAnchorsOnAWhitespaceCut`) fails
loudly if that run is edited away.

## Measured acceptance

```
$ python3 tools/docclass/classify.py . --only devdoc/task-report-harness-facts-agreement.md \
      --only devdoc/verb-double-spelling-slice.md
   → FIXED POINT  (stored cell == render, byte for byte, both rows)
$ python3 tools/docclass/classify_test.py
   → Ran 34 tests … OK
```
