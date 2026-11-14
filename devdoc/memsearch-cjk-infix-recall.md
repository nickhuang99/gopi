<!-- doc-class: normative -->
# Infix-CJK recall in `memory_search` — a measurement (epic 01a0e4b4)

**Verdict: DOCUMENTED LIMITATION, not a recall defect. No fix ships.**

Infix-CJK recall is **recovered** — and, more precisely, route 1 never loses it in
the first place: `distilled_qa_fts` is **not** a plain unicode61 index. It also
indexes `jieba_token`, a column holding 2-char CJK grams, so an infix like `测试`
is a *stored token* of route 1. The epic's premise ("a query for 测试 should not
match a row containing 网络测试环境 by route 1") is **true of a unicode61-only
index and false of the shipped one** — both halves are measured below, on real
fts5 tables.

The one genuinely lost shape is a **single CJK character** (below every route's
lower bound). That is a bounded, documented limitation, not a defect: no route
selection change can fix it without either an unbounded LIKE scan or a new index
(a new dependency in all but name).

---

## 1. The measurement rig

| item | value |
|---|---|
| Corpus | **12 rows**, seeded through the real CLI `gopi memory store` |
| Ledger | `/home/user/workspace/_tmp/epic-probe-01a0e4b4/probe.db` (off the `/` partition: `/` is dev `2051`, scratch is `2052`) |
| Engine | the production binary `bin/gopi` (`CGO_ENABLED=0 go build ./cmd/agent/`) |
| Index DDL | the production path — `MemoryStore` → `ensureSchema` → `m1FTSVirtualTable` / `m1TriVirtualTable` (no hand-rolled schema) |
| Route 1 | `distilled_qa_fts MATCH escapeQuery(query)` |
| Route 2a | `distilled_qa_tri MATCH` `<cjkRunsGE3 OR-joined>` (trigram) |
| Route 2b | `distilled_qa LIKE` `<cjkGrams2 clauses>` over question/topic/exam_questions/kw_what/kw_why |
| Public authority | `MemorySearchHits(query, 20, "")` — the union a caller actually sees |
| Raw log | `~/.gopi/sessions/01a0e4b4-71ea-797e-aa80-7c705255a826/probe-route-raw.txt` |

## 2. The tokenizer decision (measured, not assumed)

`fts5vocab` over the **real production** `distilled_qa_fts` (511 tokens), asking
whether a given string is a stored token:

| candidate token | stored? | why |
|---|---|---|
| `网络测试环境部署完成` | **yes** | the whole contiguous CJK run — unicode61 makes it ONE token (the premise holds for the `topic` column) |
| `测试` | **yes** | the `jieba_token` 2-char-gram column: `cjkSegmentation` writes 网络 络测 测试 试环 环境 … |
| `试环` | **yes** | a 2-char gram exists for *every* adjacent pair, not just word boundaries |
| `golang` | **yes** | unicode61 splits the fused run `golang内存泄漏排查` at the script boundary |
| `golang内存泄漏排查` | **yes** | …into the ASCII token + the CJK run |
| `vpn` | **yes** (`VPN` no) | FTS5 folds case |
| `络测试` | **no** | 3-char odd gram — not in `jieba_token` |
| `内存泄漏排查` | **no** | not a 2-char gram |
| `测` | **no** | single char — below every bound |

**Decision:** `distilled_qa_fts` = `(unicode61 columns) + (jieba_token 2-char-gram column) + (trigram-or-LIKE substrings, routes 2a/2b)`. Route 1 therefore stores
the 2-char substrings that the naive reading of the premise says it cannot.

## 3. Causal control — a real fts5 table with unicode61 ONLY

The same 12 rows inserted into `CREATE VIRTUAL TABLE probe_ctl USING fts5(topic,
question, answer, tokenize='unicode61')` (no `jieba_token` column):

| query | unicode61 `MATCH` | naive LIKE `%q%` |
|---|---|---|
| `网络测试环境部署完成` | 1 | 1 |
| `测试` | **0** | 4 |
| `络测试` | **0** | 2 |
| `试环` | **0** | 2 |
| `测` | **0** | 4 |
| `golang内存泄漏排查` | 1 | 1 |
| `golang` | 1 | 1 |
| `VPN 网络测试环境` | 1 | 1 |

This is the epic's premise, reproduced exactly — **on an index that lacks the gram
column.** The shipped index matches `测试` in 4 rows (§4). The delta is the
`jieba_token` column, and it is the whole answer.

## 4. Per-shape, per-route hit counts (real production schema)

Hit sets are ledger ids; row topics are in §1's raw log.

| # | query shape | query | route 1 `escapeQuery` → hits | route 2a trigram → hits | route 2b LIKE/cjkGrams2 → hits | union | public `MemorySearchHits` |
|---|---|---|---|---|---|---|---|
| S1 | full stored CJK run | `网络测试环境部署完成` | 1 {id1} | 1 {id1} | 5 | **5** | **5** |
| S2 | infix of a stored token | `测试` | **4** {id1,id2,id6,id12} | 0 (runs <3) | 4 | 4 | **4** |
| S3 | mixed CJK+ASCII | `VPN 网络测试环境` | 1 {id2} | 2 {id1,id2} | 5 | **5** | **5** |
| S4 | single CJK char | `测` | **0** | 0 | **0** | **0** | **0** ← LOST |
| S5 | CJK split at an ASCII/space boundary | `网络 延迟` | 1 {id8} | 0 (runs <3) | 3 | **3** | **3** |
| S6 | fused ASCII+CJK run (stored glued) | `golang内存泄漏排查` | 1 {id9} | 1 {id9} | 2 | 2 | 2 |
| S7 | 3-char infix, odd offset | `络测试` | **0** | 2 {id1,id2} | 4 | **4** | **4** |
| S8 | 3-char infix spanning a gram boundary | `试环境` | **0** | 2 {id1,id2} | 2 | 2 | 2 |
| S9 | lone ASCII token of a fused row | `golang` | 1 {id9} | 0 | 0 | 1 | 1 |
| S10 | 2-char non-word infix | `试环` | **2** {id1,id2} | 0 (runs <3) | 2 | 2 | 2 |
| S11 | answer-only 2-char infix | `飞锁` | **1** {id7} | 0 | 1 | 1 | 1 |
| S12 | answer-only 3-char infix | `只做一个` | **1** {id7} | 1 {id7} | 1 | 1 | 1 |
| S13 | answer-only 4-char infix | `空值占位` | **1** {id7} | 1 {id7} | 1 | 1 | 1 |
| S14 | answer-only 2-char infix, 2 rows | `雪崩` | **2** {id5,id7} | 0 | 2 | 2 | 2 |

The first column is the epic's five shapes; S6–S14 are the adversarial ones.

## 5. What the table says, route by route

- **Route 1 is not the narrow route the premise assumed.** It answers S2, S10, S11
  and S14 — all *infixes* — because `jieba_token` stores every 2-char gram. It
  misses only the 3-char **odd** infixes (S7, S8), which no 2-char gram can cover.
- **Route 2a (trigram) is exactly the complement of route 1's 2-char granularity**:
  it covers ≥3-char runs (S1, S3, S6, S7, S8, S12, S13) and is empty for anything
  shorter.

- **The routes are complementary, not redundant**, and the union is what the public
  entry point returns: in every row of §4 the public hit count equals the union.
- **S4 is the only loss, and it is not reachable by route selection.** Both routes
  are structurally empty for a single char: `cjkRunsGE3` returns nothing below 3
  runes and `cjkGrams2` nothing below 2 (by design — `likeCJKTargets` holds the five
  short-text columns, and a 1-char LIKE would scan them all and match nearly
  everything). Recovering it would need a new index or an unbounded scan: a new
  dependency for a query shape nobody issues.

## 6. Verdict

**Documented limitation.** The summary line:



## 7. Scope / honest limits


- S4 is a limitation of the *corpus shape* as much as of the index: 1-char CJK is a
  prefix of nearly every CJK query, so it is also the shape where a wide recall would
  be least useful.
- The 12-row corpus is a probe, not a benchmark: it establishes *mechanism*
  (which route answers which shape), not recall rates at scale.
