<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The run-record tree is not authority — a ruling on `tasks/` and the doc-class cite tier

**Status: RULING (a) — a run report is NOT authority for a document's doc-class.**
Landed 2026-10-14. The single criterion lives in
`tools/docclass/classify.py`'s `AUTHORITY_EXCLUDE_DIRS`; the regression tests live
in `tools/docclass/classify_test.py` (`TestCitationsAreStatementsNotQuotations`).

## The question, and why it needed a person to answer it

`devdoc/DOC-INDEX.tsv` carries one class per devdoc file. `normative` means "this
doc makes a standing claim about master", and one of the four ways a doc earns it
is the **cite** tier: some file in the *authority corpus* names the doc as the
live statement of the rule it describes. `devdoc/` is excluded from that corpus
on purpose (a sibling reference is corpus-internal); the classification tooling is
excluded too. **`tasks/` was not.**

`tasks/<task-id>-report.md` is a **run's own account of the work it did** — the
file Rule 5 requires every change cycle to write. A run names a devdoc there
because it *wrote, audited or repaired* it. So the mention records an **action**,
not an artifact stating a rule — while the cite tier reads it as the latter.

## The measurements (all reproducible; commands below)

| measurement | value |
|---|---|
| markdown docs under `devdoc/` taking cite evidence from `tasks/` | **220** |
| of those, the normative verdict is the "a landed ruling" arm | 178 |
| docs whose **class** changes when `tasks/` is not authority | **183** |
| of those, the class is **all-the-way** cite-driven (no spot-check either — `sym`/`anchor`/`path` all absent) | **164** |
| rows reading `normative` in the **checked-in** manifest on that basis | **88** |
| reverse direction (2026-10-12 corpus change): `historical→normative` | 205 |
| … of which `normative→historical` when the citing report left the tree | **20** |
| tracked reports under `tasks/` now / already removed | 549 / 4 |

Three findings decide it, and each is a measurement rather than an argument:

1. **It is the largest single driver of class churn in the corpus** — larger than
   `briefings/` (95), `cmd/` (77), or `docs/` (18).
2. **The class follows a file's PRESENCE.** A doc reads `normative` while its
   report is in the tree and `historical` once it is gone. A truth-class that is a
   statement about *ledger retention at the moment someone ran the tool* is not a
   statement about master — and `tasks/` is retention-governed by design (4 reports
   already removed), while no merge path requires a report to stay.
3. **It is the same self-subject defect the file already refuses elsewhere.**
   `scripts/docclass-red-proofs.sh` is excluded because *"a harness that can change
   a document's class by mentioning it is deciding its own subject"*. A run report
   is that harness's output: the run that classifies a doc writes the report that
   then classifies it.

## The alternative that was rejected, and why

*Keep `tasks/` in the corpus and require the mention to be "prose"* (the four
existing arms — fence, checker output, bookkeeping, command transcript — extended
to reports). **Rejected because the arms do not reach this shape**: a run report
says "the doc `devdoc/x.md` states …" in ordinary prose, which is exactly the shape
a real authority cell has, so no line-level predicate can separate them. The
boundary is the **tree**, not the sentence — and a boundary that is a directory is
also what `devdoc/` and `tools/docclass/` already use.

## The criterion (Rule 13/14 — ONE definition)

```python
AUTHORITY_EXCLUDE = {"scripts/docclass-red-proofs.sh"}
AUTHORITY_EXCLUDE_DIRS = ("tasks/",)      # one run's record of the work it did
```

The predicate is `any(r.startswith(d) for d in AUTHORITY_EXCLUDE_DIRS)` inside
`build_citation_index()` — the SAME seam the per-file `AUTHORITY_EXCLUDE` uses, so
the two exclusions cannot drift. **`reports/` stays in** (it is a live authority
tree: `reports/archive/2026-08/report-gopi-r5r6-debug.md:41` is the citation that
holds `devdoc/thinking-budget-fix.md` up), which the regression test pins by
putting the **same bytes at the same filename** in both trees and asserting the
verdict moves.

## What the ruling changed in the corpus

36 rows re-derived by the generator (all `normative → historical` — 35 in the sweep,
plus one row the sweep did not cross because the manifest was mid-write when the path
list was read off it; re-reading the committed file caught it, and it was fixed in its
own commit 99ed85bd8). 35 banner files updated by `banner.py` (the banner is only
carried by the `historical` class). `--check` reports 0 gaps; `banner.py --check`
reports 0 disagreements.

Final stored distribution at 99ed85bd8: **1728 historical / 760 normative / 1 index-meta /
12 roadmap** (from 1692 / 795 / 1 / 12). Every `cite tasks/` evidence cell is gone
(`grep -c 'cite tasks/' devdoc/DOC-INDEX.tsv` → 7 at HEAD, 0 after).

## Reproduce (each line is a run, not a claim)

```bash
# the census
python3 tools/docclass/classify.py . --check                       # gaps + STALE count
grep -c 'cite tasks/' devdoc/DOC-INDEX.tsv                         # before: 7   after: 0

# the ruling's own three arms (fixture-sized, seconds):
python3 tools/docclass/classify_test.py TestCitationsAreStatementsNotQuotations

# the corpus-level measurement (183 docs move) is the scripted form of
# AUTHORITY_EXCLUDE_DIRS' comment — re-measure it by dropping the constant and
# diffing the re-derivation of the same paths, in ONE process:
#   --only <path> is repeatable; --only-file is a SCOPE file and does NOT select
#   the re-derivation path (see "Not Done" below).
```

## Not Done / honest limits

- **The same structural defect exists for other trees, and only the `tasks/`
  question was ruled on.** Measured by the same method (fence one directory off,
  count class moves): `internal/` 264, `briefings/` 95, `cmd/` 77, `docs/` 18,
  `scripts/` 6, `.evid/` 6. Whether a briefing's mention is a *statement* (it is
  an assignment document, arguably a live one) or another run-record face is a
  separate question that was NOT asked here. Filing it as its own task is the
  correct next step; the boundary chosen here is deliberately the one the question
  named.
- **`--only-file` does not select the re-derivation path.** Measured
  2026-10-14 on this tree: `python3 tools/docclass/classify.py . --only-file
  <821 paths>` printed `"updated": 0` and rewrote the whole manifest through the
  carry path, while the same intent spelled as 821 `--only` flags updated every
  row. Nothing in the usage text or the flag's own docstring warns a caller, and
  the effect is silent. Flagged as a defect, not fixed here.
- **`--dry-run` of the whole-corpus mode prints the CARRIED rows**, so it is not a
  preview of a re-derivation (`classify.py`'s main path). Documented because it is
  how this task's first impact measurement produced a false "0 rows change".
- **The 164 "all-the-way cite-driven" count is a level, not an assertion**: measured
  by classifying each path twice in ONE process with the cite removed
  (`CITED.pop`), which is the level measurement; the ruling's own **red proof** is
  the fixture-sized inverse of it — delete the run report and the class must NOT
  move (`test_a_report_that_leaves_the_tree_cannot_move_the_class`).
