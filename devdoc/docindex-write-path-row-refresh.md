<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# The write path re-renders a row it owns — the source of self-inflicted `stored-cell drift`

**Class: historical** — that is the corpus classifier's reading of this document's
own text (its title/name declares a past run's record, and no current artifact cites
it), and it is the class its manifest row records; a banner that claimed otherwise
would put the file and the manifest in disagreement. What follows is a standing
claim about master regardless: the rule the write path holds today, the measured
defect that produced it, and the seam that keeps the two faces agreeing.

## The defect, measured

A devdoc row is DERIVED FROM THE LIVE TREE. Its reason cell carries counts taken
over the document's own text (`standing N / record M`) and its evidence cell names
a code artifact the document cites. The write hook classified a document **once**,
at the moment its bytes landed — and every later write through the same door
(`edit`) only materialised the row's class in the file. The ROW was never
re-rendered.

So a document that is EXTENDED after its row was written keeps a row the corpus's
own classifier renders differently today. The author is then red **on their own
document, at their own commit**:

```
doccheck: stored-cell drift: 1 of 2551 rendered row(s) whose stored text is
          not what the classifier renders today        (verdict: drift, exit 1)
```

The red arm was measured by REVERTING the cell, not argued (task `01a141a7`,
reported by session `01a1362e`, 2026-10-16): restoring the hook's own stored cell
moved the drift count **8 → 9**, re-deriving it moved the count back **9 → 8** —
ONE cell, both directions. The remedy the reporter could find was to re-derive the
row by hand with the generator, which is a workaround: the row would drift again on
the next edit.

## The rule

`internal/docindex.ClassifyWrittenFile` — the ONE hook the `write` and `edit` tools
both call, after the bytes are on disk — now takes a path that ALREADY carries a
well-formed manifest row through `refreshOwnedRow`, which does two things and
nothing else:

1. **The file half** (unchanged, `materialiseRowClass`): the class the ROW declares
   is written into the file as its banner, or removed from it when that class
   carries none. The class is taken FROM THE ROW, never derived from the text.
2. **The row half** (new): the row's LIVE-DERIVED cells are re-rendered for the
   document's FINAL bytes.

`Fixup.RowRefreshed` reports the second half to the caller, so "the stored cell was
brought up to date" is distinguishable from "the stored cell is what the earlier
write left behind".

### What is re-rendered, and what is never touched

**Re-rendered**: the recorded citation, the `standing N / record M` counts, the
anchor text — the half a row DERIVES from the tree, which is stale by construction
once the document changes, and the half `doccheck` compares.

**Never touched**: the row's CLASS. The guard this respects is older and measured:
task `01a0dc29` / commit `051a1ab3`, where an ordinary write silently flipped a
deliberately classified row's class and emptied its evidence cell. The refresh leg
does not weaken that guard — it asks the renderer for the row, COMPARES the class
cell with the row's, and on a disagreement it **keeps the row and the file exactly
as they were**, records `Fixup.RowRefreshSkipped`, and names the one act that can
resolve it (`python3 tools/docclass/classify.py <repo> --only <path>`, run by a
human). A write may not re-decide a classification.

That disagreement is deliberately **not** an error: it is a pre-existing condition
of the corpus (a deliberately classified document whose text reads as a record), not
a failure of the write the user asked for. Returning it as an error would make an
ordinary edit of such a document impossible; swallowing it would hide that the
corpus — not the write — is what needs deciding (Rule 15).

## The channel: why the write path asks doccheck's own question

The re-render goes through `classify.py <repo> --dry-run --only <path>` — the SAME
write-free rendering mode `gopi tool doccheck` uses as its comparison basis
(`internal/docindex/docclass_drift.go`). Asking the same generator the same question
is what makes the write path and the check agree **by construction** rather than by
two spellings happening to match: a row this leg adopts is, byte for byte, the row
that check will render for the same tree.

The other candidate is `--only PATH`, which the CREATE leg uses
(`ClassifyWithGenerator`) — and it **writes**. Reaching it for an already-classified
path is precisely the re-adjudication the ownership guard exists to refuse.

**One trap, measured and worth recording.** `classify.py`'s write-free mode prints
the ROW on stdout and its own SUMMARY on stderr (`would write N row(s): [...]`). A
caller that folds the two streams together — the pre-existing `generatorRunner` used
`CombinedOutput` — tries to parse that sentence as a manifest row and refuses a
perfectly good render. `generatorRunnerSplit` keeps the two apart, and `--dry-run`'s
row line is taken alone.

**A second trap, measured by the first run of the regression test.** A manifest row
IS a line: the reader (`ReadManifest`) holds the manifest as `m.lines` and the writer
(`Manifest.Write`) joins them back with `"\n"`. A renderer's line handed over WITH
its trailing newline therefore splices a blank line into the corpus on every
re-render — the manifest grew one empty line per write, and the reader then reported
`line 12: blank line (no field at all)`.

## What is excluded, and why

- **`devdoc/DOC-INDEX.tsv`** is the one path with TWO renderers by design: its row is
  a fact about the index (`internal/docindex.selfReason`), while the generator
  renders its own older wording for it. Both `doccheck` faces already step aside for
  that row; a refresh that asked the generator would put the writing side in
  permanent disagreement with the check. The manifest is still materialised (its
  class is `index-meta`, which carries no banner), never re-rendered.
- **A corpus with no generator**: there is no renderer to ask and nothing to compare
  against — that row is this package's own output and cannot disagree with a generator
  that does not exist, so the leg is a no-op.
- **A non-markdown classified path**: not a document the classifier renders.

## Cost

The re-render is ONE generator invocation for the path whose bytes were just
written, never a corpus walk of its own — the generator walks the content index
internally, measured at 23.4s per invocation on this repo (warm page cache, idle
host; the write path already pays the same cost once per NEW document, and the
hook's own bound, `internal/tools`' `ClassifyHookTimeout` = 60s, was sized above it
as a hang stop rather than a budget). It runs for every write/edit of an
already-classified document, so an edit of a large corpus document costs one
generator invocation; a path written once does not pay it again, because
`EnsureRow` returning false writes nothing (`TestRefreshIsIdempotent`).

## Evidence

The regression test is `internal/docindex/generator_row_refresh_test.go`, driving the
SHIPPED generator against a real on-disk corpus (Rule 31):

- `TestWrittenRowIsRefreshedOnTheNextWrite` — write a document, then edit it; the
  stored row must equal what the classifier renders for the FINAL bytes, and the
  class must not have moved. RED before the fix (measured: the mutation
  `refreshOwnedRow` → `materialiseRowClass` fails it with "the stored row is
  STALE"), green after.
- `TestRefreshIsIdempotent` — a write that changes nothing leaves the manifest
  byte-identical (the non-vacuity control: a refresh that rewrote on every write
  would pass the first test while making every write a manifest churn).
- `TestRefreshRefusesADisagreeingClass` — the renderer reports a class disagreement
  instead of inventing agreement.

`internal/tools/write_docindex_refresh_seam_test.go` pins the RECORDING of a declined
refresh: the operator-facing stderr line names the path and the remedy.

The LIVE proof (a real `gopi` run driving the real `write` and `edit` tools) is what
closed the loop: the row read `historical (0 standing / 0 record)` after the first
write and `historical (0 standing / 2 record)` after ONE edit of the same file —
the counts are the half that moved, and the class did not. The proof document itself
is `devdoc/refresh-write-path-proof.md`.

## Scope boundary — what this fix does NOT do

It repairs the row of a path **when that path is written or edited through the tools**.
It is not a corpus migration: a row left stale on master by a document nobody touches
again stays stale, and `gopi tool doccheck` still counts it. Measured at the fix, on
the fix's own worktree, after the live proof: **6 rows drifted, and none of them was
a document this run wrote** (the three echoed were
`devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md` — a `cite …:188` versus
`cite …:222` — `devdoc/gopi-task-ledger-architecture.md` and
`devdoc/main-checkout-deposit-arm.md`; `git diff --name-only` shows this run touched
none of them). Re-deriving those is the documented repair already available to a
human (`python3 tools/docclass/classify.py <repo> --only <path>`, one path at a time),
and doing it as part of THIS change would mix a corpus migration into a write-path
fix. The defect the task named — an author red on their OWN document at their own
commit — is closed; the pre-existing residue is a separate, already-reported state
(`classify.py --check` lists the same rows as STALE, "reported, NOT fatal").
