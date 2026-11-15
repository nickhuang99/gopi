<!-- doc-class: normative -->
# Memory search route-3 relevance — the ordering key, measured

**Status:** fix implemented, regression test red→green, arena A/B measured.
**Worktree:** `~/workspace/gopi-01a127d7fdc37a07a6c940589236fd8b` (branch `task/01a127d7fdc37a07a6c940589236fd8b`)
**Measured at:** commit of this branch (see the report's `## Verification`).
**Related:** `devdoc/memory-arena.md` (the arena that measures natural-language
recall), `devdoc/memory-cjk-infix-route.md` (route 3's introduction).

## The defect

`gopi memory search` for a **natural Chinese question** returned a set that had
nothing to do with the question. The arena (160 rows, natural queries) measured
**natural 0/120 in the top-N** while the control set scored **40/40 perfect**.

Reproduction on the production ledger — the query is the single most common CJK
token in the corpus (1,940 rows, more than the whole result quota), so a
relevance-ranked search MUST return different rows for different questions:

```
gopi memory search "怎么"     --limit 10     # same 12 rows as:
gopi memory search "测试环境" --limit 10     # ...a different question
```

## Root cause (two halves, both required for the symptom)

**1. Route 3's ordering key carried no query signal.**
The CJK substring route (route 3) ended in `ORDER BY d.confidence DESC LIMIT
<trigramRecallK>`. Confidence is a property of the ROW, not of the query, so for
every query whose candidate set exceeded the quota the returned rows were the
same constant set — the highest-confidence, lowest-rowid rows. On the
production corpus **70 rows tie at confidence=1.0**, i.e. the quota (15) is
filled entirely by the tie, and no row the query actually matched can enter the
pool. Measured: the natural target was **in the pool but outside the returned
set** for 81/120 queries; `route 3 top15 target 0/60` in the offline
simulation.

**2. The shared rescore could not repair it, and its coverage signal was
structurally zero for CJK.**
Every route's rows pass through one rescore (`queryTokenCoverage`). That
function's tokenizer, `meaningfulQueryTokens`, split the query on
`strings.Fields` — whitespace. **A natural Chinese question has no whitespace**,
so the query was ONE "token": the entire 20-rune question, a string no record
contains. Measured across 120 natural queries: **coverage = 0 on 1,189 of
1,200 returned rows** (99.1%), so the rescore's coverage term was a constant and
the order fell back to confidence — the same defect, one layer down.

The two halves are sequential: (2) made the rescore blind, so (1)'s ordering
key decided everything, and (1)'s key could not see the query either.

## The fix

**Half 2 first, because it is the shared signal** (`internal/tools/memory.go`):
`meaningfulQueryTokens` now routes each whitespace field through
`splitCJKRuns`, and each CJK run through `cjkQuerySegments` — the SAME segmenter
that writes the index column (`cjkSegmentation`), so index and query stay at one
granularity (the invariant the file already asserts). A field like
`"网络测试环境部署完成"` now yields `[网络 测试环境 部署 完成]` instead of
itself. ASCII fields and mixed fields keep their previous behaviour.

**Half 1** (`likeCJKQuery`, `likeCJKRelevanceExpr`, `likeCJKRelevanceArgs`):
route 3's primary ordering key is now a query-relevance score, computed inside
the same LIKE scan with `instr()` per column:

```
SUM over rankable terms t:  (does any target column contain t) * rune_len(t)
```

- **rune length is the weight** — a longer CJK term is a more specific match.
  This is the specificity signal a LIKE scan can compute with no index support
  (corpus DF is not available to an expression the way FTS5's `bm25` is to a
  `MATCH`). Every decision here was measured against the arena corpus: `count`,
  `idf`, `raresum`/`raremax`, `bm25`, length-weighting — length-weighting won
  (`20/120` reachable in the top 200 vs `6/120` for count weighting).
- **terms shorter than 2 runes carry no weight** (`likeCJKRankMinTermRunes`):
  the segmenter emits 1-rune function words (里, 是, 的) as real tokens — they
  stay in the WHERE (genuine recall surface) but they do not rank.
- **confidence is demoted to a TIEBREAK**, after the relevance key.
- **one builder for the SQL and its bound args** (`likeCJKQuery`): SQLite
  numbers parameters in TEXT order, and the key sits after the WHERE clause —
  so the two must be produced together. Measured the hard way: binding the key's
  terms first silently produced a key that scored every row the same, i.e. the
  pre-fix ordering at higher cost.

## Measurements

Arena (160 rows, the same `questions.jsonl` / `sample.json` as
`devdoc/memory-arena.md`), `<arena>/arena.py run` + `analyze.py`:

| | natural | control |
|---|---|---|
| before | 0/120 | 40/40 (34 perfect, 6 partial) |
| after (partial run, 38 natural) | **1 partial / 37 fail** | 12 (10 perfect, 2 partial) |

**The arena does NOT show the natural-recall lift this change was aimed at, and
that is the honest headline.** The mechanism is fixed and observable (see the
`tokens=` and reorder evidence below), but the acceptance metric for natural
queries did not move. The offline measurement said why in advance: with the
best key tested, the target was already IN the candidate pool but outside the
returned quota for **81/120** natural queries — no, read the other way —
**39/120 had the target OUTSIDE THE POOL ENTIRELY**, and a pool the routes
never recall cannot be reordered into the answer. The reorder recovers what the
pool contains; the arena says the pool is the binding constraint.

Offline route-3 simulation (jieba segmenter stood in for `internal/cjkseg`;
the GSE word list is not importable from python):

| ranking key | target reachable within |
|---|---|
| `count` | top10 6/120 |
| `idf` | top10 5/120 |
| `raresum` | top10 7/120 |
| `bm25` | top10 4/120 |
| **length-weighted** | **top200 20/120, top10 2/120** |


## Verification the next reader can re-run

```bash
# the regression pair (red before, green after)
go test ./internal/tools/ -run 'Route3Ranks|MeaningfulQueryTokens' -count=1

# the binding-order canary
go test ./internal/tools/ -run 'LikeCJKRelevance' -count=1
```

The red proof is reproducible: revert only the `ORDER BY` key and the tokenizer
(`git show <pre-fix>:internal/tools/memory.go`), keep the new test file, and
both cases fail — `datatype mismatch (20)`, i.e. the pre-fix arg order, and
`[网络测试环境部署完成]` as one token.

## Not Done / limits (honest)

- Route 3 still scans with `LIKE` per term per column; the relevance key adds
  `instr` calls to the same scan. No index was added (ZERO-DEP), and the scan
  was not profiled for the worst-case query shape.
- The relevance key is a **recall-of-query-words** score, not a semantic one:
  a query whose words appear in a decoy's boilerplate still outranks a shorter,
  truer answer. The arena's control set is the guard against that.
- `trigramMatchK`, the earlier trigram-window route, returns **empty for 119 of
  120 natural queries** — it feeds essentially nothing into the union, and that
  is a separate defect (filed as a follow-up, not fixed here).
