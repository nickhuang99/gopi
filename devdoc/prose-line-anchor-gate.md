<!-- doc-class: normative -->
# Prose line-number citations in `tasks/*.md` — the third carrier, and the gate built for it

**Task**: `01a12dc1-2f0f-744e-bfe5-c53db7550d3c`
**Full run record (evidence, red/green transcripts, filed rows):** `tasks/01a12dc1-report.md`
**Supersedes nothing; sits beside** `anchor_number.go` (DOC-INDEX anchors) and
`internal/standards/census_evidence_gate.go` (Rule 38) — three carriers, three gates.

This stub exists so that the NEXT agent who opens `internal/docclass/prose_anchor.go`
finds, in the tracked corpus, what the gate does, what it deliberately does NOT cover,
and which tests pin the boundary. The run's own narrative lives in the task report;
only the standing facts are here.

## What the carrier is

A report under `tasks/` can cite a line of ANOTHER report in prose:

```
(that file, line 342: *"A whole-package run of `./internal/testutil` on this branch is red, …"*)
```

The number is an **accelerator**; the **quoted text is the authority**. When a later
commit moves those lines the number silently rots, and nothing fails — the third such
carrier, after DOC-INDEX anchors and census figures.

## The decision that shapes the code (do not "fix" this)

**No existing mechanism could cover it.** Measured, not argued:

- `AnchorNumberFindings` iterates **`devdoc/DOC-INDEX.tsv` rows**; `cut -f1 devdoc/DOC-INDEX.tsv | grep -c '^tasks/'` → `0`,
  and `docclass.Root = "devdoc"`. The input does not exist — the corpus is not iterated at all.
- `census_evidence_gate.go` reads the same `devdoc` corpus and answers a different question.

So the carrier needs its own reader. `TestProseAnchorExpressionIsSeparateFromTheManifestRung`
asserts `ProseAnchorScanRoot != docclass.Root` and that the manifest carries no `tasks/` row:
**folding this carrier into `AnchorNumberFindings` must break that test first.**

Reused on purpose (not rewritten): `anchorTextKey` is the ONE whitespace fold
(`TestProseAnchorFoldAgreesWithAnchorTextKey` pins the agreement), and Rule 39's freeze-table
shape is column-isomorphic with `anchor_number_baseline.tsv`.

## Vocabulary — closed set of three

| verdict | meaning | red? |
|---|---|---|
| `agrees` | the citing report carries the text at the cited line | no |
| `line-drifted` | exactly ONE other report carries it, at a DIFFERENT line | **yes** |
| `referent-unresolved` | 0 or >1 carriers — the machine cannot adjudicate | **never** |

`reference-unresolved` never turns red: measured, **9 of 10** citations in the corpus land
there (cites an external authority, quotes tool output such as `line 194: die: command not
found`, or is carried only by the citing report itself). A gate that reds them is noise.
`TestProseAnchorUnresolvedIsNeverRed` pins it in both directions (0 carriers / 2 carriers).

## Two implementation traps, both measured

1. **Evidence regions must be blanked, or the gate corrupts itself.** A report that transcribes
   a citation *verbatim into a fenced block* to show the gate's output becomes a **second
   carrier** of that sentence ⇒ the referent turns ambiguous ⇒ the verdict flips silently from
   `line-drifted` to `referent-unresolved`, retiring the very finding the gate exists for.
   `proseAnchorCanonical` therefore parses referents against the **statement view only**
   (fenced blocks, indented code, `>` quotes and table rows blanked). Blank, not deleted:
   the line map must index back to **real file lines** (deleting shifted a sample referent to `:258`).
2. **Line mapping is by BYTE, not rune.** `strings.Index` returns a byte offset; counting runes
   drifted ~11 lines on CJK reports.

Regression test: `TestProseAnchorEvidenceRegionsAreNotReferents` (three sub-cases).

## Measured coverage boundary — the gate reads the SMALLER of two citation forms

Both forms were counted on the same tree (`tasks/*.md`, 460 files at measurement time):

```
grep -ohE  '\blines? [0-9]+'                                  tasks/*.md | wc -l   → 201   ← what the gate reads
grep -rhoE '[A-Za-z0-9._/-]+\.(md|go|tsv|json|sh):[0-9]+'     tasks/*.md | wc -l   → 2158  ← NOT read
```

The gate's only input is a bare `line N` **paired with an immediately following ASCII-quoted
run ≥ 48 folded chars** — 10 citations today, of which 1 is resolvable. Consequences a reader
must know:

- **`<path>:<line>` is invisible to this gate**, although it dominates the corpus and needs no
  quote to resolve (the path IS the referent). 949 of 2158 point at existing files; **24 are past
  EOF** — a provable, near-zero-false-positive arm. Those 24 mix *deliberate placeholders*
  (`:999999`) with *real drift* (`internal/agent/chat_ops.go:1854` on an 878-line file). Filed as
  `01a12de6-c052-7346-9efc-bbd885387489`.
- **CJK corner brackets (`「…」`) are not a quote delimiter** — deliberately. Reading them would
  turn "quoting someone" into "citing", a false-positive source. So a report that *quotes an old
  citation as evidence* is not adjudicated (measured: `tasks/01a12d67-report.md:88` → no hit).
- The epic's "27 pre-existing citations" is **not reproducible on this tree**; the gate freezes
  only what it can adjudicate. Bare `(line 258)`-style mentions carry neither quote nor referent
  and are not red.

## How to run it

```bash
go test ./internal/docclass/ -run 'ProseAnchor'                      # the gate + red/green/ratchet proofs
go run internal/docclass/testdata/prose-anchor-census.go . \
    internal/docclass/testdata/prose-anchor-baseline.tsv [--check]    # Rule 38: the generator, re-runnable
```

`testdata/prose-anchor-baseline.tsv` is DATA, never hand-edited (Rule 36/39); a permit that no
longer covers a drift is an **error**, so the table only ratchets down.
