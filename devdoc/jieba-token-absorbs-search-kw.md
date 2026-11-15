<!-- doc-class: normative -->
# The jieba_token retrieval-intent contract (why search_kw must enter the token)

`internal/tools/memory.go` derives `jieba_token` from the row's WHOLE retrieval
surface — topic + question + answer + `search_kw` + the sibling retrieval-intent
columns (`exam_questions`, `kw_what..who`, `when_at`, `lesson_type`) — and
re-derives it on every write that can change that surface. This doc states the
contract, the measured symptom that produced it (2026-09-22) and the reusable
verification recipe.

Scope line: task "memory/db ingest — jieba_token must absorb search_kw", branch
`fix/jieba-token-absorbs-search-kw`; sibling (out of scope) `01a0c571` — the
query-side AND-subset degradation ladder.

## Symptom (measured, not inferred)

A row whose `question` is a **declarative title** and whose real phrasing lives
**only in `search_kw`** is not retrievable by that phrasing:

```
$ gopi memory search "显示模型" --db <pre-change copy>
#1361 HOMM3 map visual-verification pipeline …      ← unrelated stale rows
#1118 gopi task show briefing + task run …

$ gopi memory search "统计通道" --db <pre-change copy>
#1150 GoPi memsync 双向 task sync 真机 E2E 配方 …   ← unrelated
$ python3 -c "…where id=1072 select search_kw…"
'gopi task show model 缺失 model name, tasks ledger TaskShow … 显示模型 追溯 …'
```

Row **1072**'s `search_kw` literally contains 显示模型; querying exactly that
returns other rows. Same for 1127 (统计通道), 1284 (环境变量), 1072 (显示模型):
the query words exist in the row and the row is still not recalled.

## Root cause: two tokenizers, one index

| side | what happens | consequence |
|---|---|---|
| **index** | `distilled_qa_fts` is FTS5 **unicode61** over `topic, question, answer, sources, search_kw, jieba_token, exam_questions, kw_what..kw_who` | splits on whitespace/punctuation **only** — a contiguous CJK run of ANY length becomes ONE token |
| **query** | `escapeQuery` leaves a CJK field bare (a single unicode61 term); `cjkRunsGE3` demands a ≥3-char run; `cjkGrams2` produces 2-char grams for the LIKE route | the query speaks **substrings** |

Proven on the real tokenizer:

```
TOKENS: [('挪到夜间便宜时段',), ('账单分析',), ('避峰',)]
'挪到夜间便宜时段' -> 1     '夜间便宜' -> 0
'避峰'            -> 1     '挪到'     -> 0     '便宜' -> 0
```

So `search_kw = "避峰 挪到夜间 错峰"` gives the index exactly three tokens
(`避峰`, `挪到夜间`, `错峰`). Every natural narrower query — `夜间`, `挪到`,
`便宜` — has **no** matching token, and no query route can reconstruct an
arbitrary long token. `search_kw` is, by design, where the LLM writes the
phrasings a human will actually search with (`避峰/错峰/挪到夜间` are the tool's
own documented breadcrumbs) — and the index was storing them at a granularity
nobody searches at.

`jieba_token` is the ONE indexed column that carries the query side's
granularity: `cjkSegmentation` emits exactly the 2-char grams `cjkGrams2`
produces, and a 2-char gram always occurs inside a ≥3-char run `cjkRunsGE3`
demands. It was generated from `question + answer` **only** (`memory.go:2576`).

## The contract (what a future agent must not break)

> **Every write that can change a row's retrieval surface must re-derive
> `jieba_token` from the WHOLE surface: topic + question + answer + `search_kw`
> + the other retrieval-intent columns (`exam_questions`, `kw_what..who`).**

Rationale: `search_kw` / `exam_questions` / the 5W1H labels exist precisely
because the *body* does not contain the phrasing a user will search with.
Indexing them only under unicode61 stores them at a granularity the query side
cannot speak — the breadcrumb column becomes write-only.

Two mechanisms make the contract hold:

1. **store** — `deriveJiebaToken(topic, question, answer, searchKwValue(kws), nil)`.
   It also now absorbs the `keywords:` answer tail automatically: `MemoryStore`
   appends it to `answer` **before** composition (measured: 1373/4639 rows get a
   changed token, 1373 of them with non-empty `search_kw`), which is why the
   pre-existing store test was already green while the real defect lived on.
2. **update** — `memory_update` with `--keywords` used to rewrite `search_kw`
   and **leave `jieba_token` stale** (the index kept the OLD wording inscribed
   and never learned the new one). It now reads the stored row and re-derives
   from the stored + pending values, so a partial update keeps every column it
   does not touch. **This is the RED the new test pinned.**

Named constants (Rules 1/13): `m1JiebaTextSep` (the composition separator —
a **space**, deliberately not the comma `cjkSegmentation` joins grams with, so
the composed text stays separately tokenizable), `m1JiebaTextCols` (the ordered
source-column set), `searchKwValue` (ONE renderer for the `search_kw` value, so
the column and the absorbed text can never disagree), `memoryJiebaTextCols`
(schema-probed rendering — a pre-M1 DB without `exam_questions` still works).

## Deliberately NOT changed

- **The FTS table definition.** `search_kw`'s unicode61 indexing is untouched
  (its exact-token hit is the feature `TestMemoryStoreSearchKwColumn` pins — the
  phrasing with no separator between its CJK runs). Widening it was out of scope.
- **`escapeQuery`'s injection safety.** Untouched: CJK fields stay bare, ASCII
  fields stay quoted, stray `"` still stripped.
- **No ranking gate was loosened** — `min_results`, `trigramRecallK`,
  `tokenCoverageWeight` and the tier logic are all as they were. The recall
  improvement is purely index content.
- **The 835 MB hermes DB's FTS table is not rebuilt with a Go-side partial
  INSERT.** The sibling FTS layout differs from gopi's (9 cols, names
  `whats_ds/whys_ds`); a partial write (5 cols) blanks `jieba_token` and makes
  the row permanently unfindable. Rebuild is the FTS5 `'rebuild'` command only.

## Verification recipe (reusable)

1. **Real tokenizer ground truth before designing** — SQLite `fts5vocab`
   shows tokens; `MATCH '夜间'` on a row whose only CJK is `挪到夜间` returns 0.
   Design against this, not against intuition about CJK segmentation.
2. **Reproduce on a COPY of the production DB** (`cp ~/.gopi/memory.db`), never
   in place: query a known `search_kw`-only row and show it missing.
3. **Predict the green before writing the Go** — simulate the post-fix
   `jieba_token` with a 10-line Python re-implementation of `cjkSegmentation`,
   `UPDATE` it on the copy, re-query. Measured green *before* a line of Go was
   written (`夜间` → hit).
4. **Count candidates for the backfill**: rows where a ≥3-char CJK run in
   `search_kw` appears in NO body column (gopi: **25**; but the absorption
   rewrites **1373** rows — a sub-gram like `夜间` inside `挪到夜间` is invisible
   to that crude test).
5. **E2E through the real CLI**, `--db <copy>`, `--limit 40` (the default limit
   hides recalled rows behind higher-scoring ones): row 1072 appears at rank 86
   for `显示模型`, 1127 at rank 52 for `统计通道`.
