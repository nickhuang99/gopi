<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The class banner is a BODY property: banner.py read line 1, and the corpus paid for it

**Script**: `tools/docclass/banner.py` (and its fixture, `tools/docclass/classify_test.py`).
**Boundary**: `devdoc/DOC-INDEX.tsv` is the row side, the file's first line is the
banner side, and this tool is the only thing that makes the two agree.

## The symptom the task started from

`devdoc/scripts-retired-verb-sweep.md` carried, on `master`:

```
<!-- doc-class: normative -->
# Retired verb spellings in `scripts/` — …
```

One banner, disagreeing with its row — and both Go gates agreed about the row and
the file: `TestDocManifestRowsAreWellFormed` (class `normative`, evidence cell
EMPTY) and `TestDocClassBannerMatchesManifest` (class `normative`, and only
`historical` is bannered). `python3 tools/docclass/classify.py . --check` named it
too: `normative-no-check`. So the row was inconsistent, not the banner, and the
repair is a reclass.

## What was actually wrong — measured, not assumed

The defect is NOT "the banner disagreed with the manifest". It is that
`banner.py` read **line 1 and nothing else**, in BOTH arms, and the two arms
disagreed with each other about the same file. Re-measured against master's tool
on a doc whose row says `historical` and whose body opens `normative` then
`historical`:

```
$ python3 tools/docclass/banner.py . --check
BANNER MISMATCH devdoc/rec.md
1 markdown docs whose banner disagrees with the manifest      # rc=1
$ python3 tools/docclass/banner.py .
banner applied to 1 docs, removed from 0
$ head -4 devdoc/rec.md
<!-- doc-class: historical -->
<!-- doc-class: normative -->
<!-- doc-class: historical -->
$ python3 tools/docclass/banner.py . --check
0 markdown docs whose banner disagrees with the manifest      # rc=0  ← blind
```

One run both reported clean AND left three banners. The next run then removed
line 1 (the correct one), promoted `normative` to line 1, and reported clean
again — so the tool could oscillate, each pass "fixing" the file and undoing the
previous pass. The corpus state on master, measured: **25 docs carried a second
banner-shaped line outside a fence**, and **7 docs had a banner that was not the
one line 1 held**.

The mirror arm is what made the damage visible in `devdoc/`: a doc whose line 1
held a banner the manifest does not ban was read as a *stale banner* and had that
line DELETED — the tool's "removal" branch did not distinguish "the manifest no
longer bannes this class" from "the manifest's class is not what you wrote on line
1".

## The rule the fix states

**Scan the whole body; leave exactly one banner; make it the manifest's.**

- A banner-SHAPED line is read in two steps — **shape first, payload second**,
  with the class vocabulary consulted only where it decides something (which
  banner a doc must CARRY). A payload that is a SINGLE TOKEN is a **declaration**
  and goes, whatever word it chose; a payload carrying WHITESPACE is a **sentence
  about a banner** and stays. That cut was added on 2026-10-14 — see “The second
  pass” below, because the vocabulary-first reading left 16 docs unreachable.
- A banner-shaped line INSIDE a fenced code block is a **quotation** of a banner
  (3 in the corpus, e.g. `devdoc/merge-ownership-requirement.md:522`, a shell
  transcript). It is preserved; removing it destroys the evidence that makes the
  doc worth keeping.
- Every banner outside a fence is removed, and the manifest's is prepended. The
  ordinary case stays a plain prepend — `+1 -0` in `git diff --numstat` — because
  `split("\n")`/`join("\n")` are exact inverses and the original bytes remain a
  suffix. The anti-pattern this guards is on the record: **an earlier generator
  stripped the FIRST LINE of every un-bannered doc — 412 markdown titles.**
- `--check` runs the SAME scan, so the report can never be weaker than the repair
  it fronts, and it names the banner that will not survive by value.

## The second pass — the vocabulary was the wrong judge (2026-10-14)

The rule above was still read through the closed class vocabulary, so a
banner-SHAPED line whose payload was a single word the vocabulary does not know
was called "prose about a banner" and left alone. Sixteen docs carried one
(measured this run, whole corpus): a second line `<!-- doc-class: reference -->`,
`design`, `report`, `incident`, `fix-record`, `evidence`, `standing`, `bugfix`,
`feature`, `test-report` — under a line 1 that was already the manifest's banner.
Every reader was green on all sixteen, and the tool was a **fixpoint** on each:

```
$ sed -n '1,2p' devdoc/worktree-sweep-ladder-order-and-live-session-arm.md
<!-- doc-class: historical -->
<!-- doc-class: reference -->
$ python3 tools/docclass/banner.py . --check ; echo rc=$?
0 markdown docs whose banner disagrees with the manifest      # rc=0
$ python3 -c "... split_banners(body.split(chr(10))) ..."
found: ['historical']   # the vocabulary dropped the second line
first 2 lines after split: ['<!-- doc-class: reference -->', '# The sweep's ladder order, …']
would rewrite? False    # → continue → `applied 0` for ever
```

A defect the tool can never reach is the one shape it must not have. The cut is
therefore on the PAYLOAD's shape, not on the vocabulary:

```
  1. SHAPE   — `BANNER_PREFIX … BANNER_SUFFIX`, outside a fence.
  2. PAYLOAD — a single token is a DECLARATION (a claim; REMOVED + REPORTED),
               whitespace makes it a SENTENCE about a banner (prose; KEPT).
```

`internal/docclass` now mirrors it — `BannerDeclarations` (line + payload, fence
aware) and `BannerDefects` (the verdict both faces run), with a red proof that
measures the old reading's blindness and the new scan's report on ONE body
(`internal/docclass/banner_body_test.go`), and the gate
(`TestDocClassBannerMatchesManifest`) reads the file's whole body instead of
`FirstLine`.

What the two surviving shapes cost if this cut is ever loosened: the sentence
payload is `<!-- doc-class: historical — classified (devdoc/DOC-INDEX.tsv); this
banner stays on line 1 -->` (two handoff docs carry it, and it is a NOTE), and a
fenced quotation is the record that makes the doc worth keeping.

## The boundary — banner.py renders a decision, it never makes one

The CLASS is the manifest's; `classify.py` is the only thing that decides it. So a
wrong ROW is out of scope by construction, and the tool will faithfully remove a
banner the doc's content deserves. That is not hypothetical: it is what happened
to `devdoc/scripts-retired-verb-sweep.md`, whose row read `normative` with a
`historical` template reason and an empty evidence cell. Reclassing the row and
re-running the banner tool is the whole repair, in that order:

```bash
python3 tools/docclass/classify.py . --only devdoc/scripts-retired-verb-sweep.md
# → {"added": 0, "updated": 1, "rows": 2055}   row is now historical (0 standing / 2 record)
python3 tools/docclass/banner.py . --only devdoc/scripts-retired-verb-sweep.md
# → banner applied to 1 docs, removed from 0   line 1 is <!-- doc-class: historical -->
```

Two faces, one corpus: `classify.py --check` is the row-side check, `banner.py
--check` is the banner-side one. `gopi doccheck` reads ROWS too — it is not the
detector for a stray banner.

## What the corpus run changed

**2026-10-06 (the body-wide pass)**: 20 docs, 19 lines. All 19 removed lines were
banner comments (10 `historical`, 9 `normative`); no title, no body line, and no
fenced quotation was touched. 19 docs had a duplicate/stale banner removed;
`devdoc/verb-retirement-phase1.md` gained the banner its row required.

**2026-10-14 (the payload-shape pass, this change)**: 16 docs, 16 lines,
`banner applied to 11 docs, removed from 16` — every one of them a single-token
declaration the vocabulary had waved through (`reference`, `design`, `report`,
`incident` ×2, `fix-record` ×2, `evidence` ×2, `bugfix`, `standing`, `feature`,
`test-report`). `git diff --numstat` is `0 1` per file: one line removed, nothing
added, no title or body line touched. The two SENTENCE payloads
(`devdoc/handoff-20260924-preflight.md`, `devdoc/handoff-20261002-restart.md`) and
the four fenced quotations are byte-identical across the run.

## Verification

- `python3 tools/docclass/banner.py . --check` → `0 markdown docs whose banner
  disagrees with the manifest`, rc=0.
- `python3 tools/docclass/banner.py .` → `banner applied to 11 docs, removed from
  16` on the pre-repair corpus, and `banner applied to 0 docs, removed from 0` on
  a second run — the repair is a FIXPOINT.
- `python3 tools/docclass/classify.py . --check` → rc=0 (769 rows reported STALE,
  a pre-existing, non-fatal, separate matter).
- `python3 tools/docclass/classify_test.py` → `Ran 29 tests … OK`.
- `go test ./internal/standards/ -run TestDocClassBannerMatchesManifest` → RED
  before the corpus repair (12 line-2 defects named by line and payload), `ok`
  after it.
- `go test ./internal/docclass/ -run 'Banner'` → ok.
