<!-- doc-class: normative -->
# Memory retrieval low-quality evaluation — "deepseek v4 flash price adjustment"

Date: 2026-08-25
Repro: `gopi memory_search "deepseek v4 flash price adjustment"` against `~/.gopi/memory.db`
(667 rows). The same query via the memory_search tool returns identical results
(CLI and tool share `tools.MemorySearch`).

## The symptom

Query intent is "deepseek v4 flash **price adjustment**". Top result is #524
"配置有双重设置——model.default: deepseek-v4-pro 是实际生效的默认值" — a Hermes
model-config note whose answer never mentions price or adjustment, and whose
keywords are garbage (`keywords: about actual and architecture are assess`).
A more on-topic row (#185, v4-flash thinking-mode failure) ranks second, purely
by luck of BM25.

## Root causes (all reproduced with SQL against the live DB)

### 1. Exact AND match returns 0 → silent AND→OR downgrade drops the user's intent

`escapeQuery` builds `"deepseek" "v4" "flash" "price" "adjustment"` (FTS5
implicit AND). Token presence in the DB:

| token | rows containing it |
|---|---|
| deepseek | 175 |
| v4 | 33 |
| flash | 44 |
| price | **1** |
| adjustment | **0** |

`price` + `adjustment` make the AND match return 0 rows, so
`escapeQueryOR` (route 1.5, the ASCII partial-recall fallback) fires with
`"deepseek" OR "v4" OR "flash" OR "price" OR "adjustment"` and orders the 15
candidates by **BM25 `fts.rank` only**. Rows that repeat rare tokens
(`deepseek`,`v4`,`flash`) outrank everything regardless of whether they answer
the query. The two tokens that carry the actual intent (`price`, `adjustment`)
are effectively dead weight that *triggered* the downgrade and then contribute
nothing to ordering.

### 2. Ranking ignores every value signal

`searchSelect` does `ORDER BY fts.rank` — nothing else. `tier`,
`confidence`, `possibly_stale` and `intent_hits` are computed and *rendered*
for the LLM to judge, but never influence order or filtering:

- **`tier` is inert.** All 667 rows are `warm`; nothing filters or weights it.
  Consequence: the epic's "make the item cold so it is no longer 1st tier"
  cannot work — cold would have no effect.
- `possibly_stale` (aged + never re-verified) never demotes.
- `intent_hits` is only computed from the optional `intent` arg (the CLI
  example passed none), and is never used in ordering even when set.
- `confidence` never weights.

### 3. No maintenance affordance

CLI exposes only `memory_search` / `memory_store`. Verified: zero occurrences
of `memory_update` / `memory_delete` / `memory_tier` / `DELETE FROM
distilled_qa` anywhere in cmd/ or internal/. There is no way to demote, edit,
or remove a low-quality row, and no `memory_get` to inspect one row's full
record before acting.

### 4. Garbage keywords from imports pollute the FTS answer index

Rows imported from distilled.db carry stopword-soup keyword tails
(#524 `about actual and architecture are assess`, #525 `above agent and any
bash cannot change c`, #689 `already api call cheap deepseek difficul`).
They are written into `search_kw` AND the answer tail, so FTS5 indexes them as
tokens. They do not cause *this* match (which is on deepseek/v4/flash), but a
query like "about actual" would match garbage rows, and there is no keyword
quality gate at store time.

## What the fix must include

1. **Tier-aware ranking**: composite score = BM25 rank + tier weight
   (hot/warm/cold) + confidence + possibly_stale penalty + intent_hits boost.
   Cold rows excluded from tier-1 results (or demoted below warm/hot).
2. **Smarter fallback**: when the exact AND misses, drop the zero-hit tokens
   and re-AND the rest before falling to OR ("deepseek" "v4" "flash" → AND).
   Only if that still misses, OR.
3. **Maintenance CLI**: `memory_get` (inspect one row), `memory_update`
   (`--id` + any field incl. `--tier cold`), `memory_delete` (`--id`).
4. **Relevance transparency**: render which tokens matched per hit (or a
   match score) so the user/LLM can judge what to demote.
5. Optional hardening: keyword quality gate at store time (reject pure
   stopword keyword lists).

## Resolution (implemented, same session)

1. **Value-signal ranking** (`MemorySearch`): every recalled candidate is
   rescored — primary key = query-token coverage (`tokens=N`, how many
   distinct meaningful query terms appear in topic+question+answer), then
   tier (hot +2 / cold −6), possibly_stale penalty (−1.5), confidence;
   BM25 `fts.rank` is the tiebreak. The `tokens=` signal is rendered in the
   result meta so the user can spot low-quality rows.
2. **Cold tier is now live**: `cold` rows are excluded from tier-1 results
   entirely and only surface when NO warm/hot row matches (last-resort
   bucket). Before this change `tier` was read-only display — demoting did
   nothing.
3. **Maintenance CLI**: `gopi memory_get <id>` (inspect), `gopi
   memory_update <id> --tier hot|warm|cold [--topic/--question/--answer/
   --source/--confidence/--keywords]`, `gopi memory_delete <id>`. All share
   the engine functions (`tools.MemoryGet/Update/Delete`), are advertised in
   `gopi -h`, and handle FTS sync for foreign DBs without triggers.
4. Verified on a copy of the live DB: `memory_update 524 --tier cold` moves
   the low-quality #524 out of tier-1; the query now surfaces #185
   (v4-flash thinking-mode fix) and #689 (v4-flash cost/cheap note) first.
5. Keyword replacement (`--keywords`) rewrites BOTH the `search_kw` column
   and the `keywords:` answer tail (tail is stripped, never duplicated) — a
   path to clean the imported stopword-soup keywords (#524/#525).
6. Not changed: the AND→OR downgrade still drops zero-hit intent tokens
   (`price`/`adjustment`) — token-coverage ranking makes the fallback
   order sensible, but a literal-token engine cannot recover semantics a
   synonym would; that is inherent to FTS5-keyword retrieval, not a bug.
   Tests: coverage ordering, cold demotion/exclusion/last-resort fill,
   get/update/delete + validation, keyword tail replacement, index cleanup.
