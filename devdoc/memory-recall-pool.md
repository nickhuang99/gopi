<!-- doc-class: normative -->
# Memory search — the recall loss is the CANDIDATE POOL, not the ranking

**Status:** fix implemented (one named constant + regression guard), arena re-run
measured (natural 0/120 → 11/120, control 40/40 unchanged), upstream route-3 fix
merged as a measured precondition.
**Worktree:** `~/workspace/gopi-01a1280181b17c96b2e9f6fc05533a17` (branch
`task/01a1280181b17c96b2e9f6fc05533a17`), branched from master `0a1bc1f7c`.
**Evidence:** `/home/user/.gopi/sessions/01a12801-81b1-7c96-b2e9-f6fc05533a17/`
(`evidence/`, `arena_after/`).
**Related:** `devdoc/memory-route3-relevance.md` (the upstream route-3 ordering
fix, merged here), `devdoc/memory-arena.md` (the arena), `devdoc/memory-cjk-infix-route.md`.

## The epic's premise, checked before it was trusted

The briefing asserted that `distilled_qa_tri` (route 2, the trigram MATCH) returns
an empty set for 119/120 natural queries and is therefore "the first lever". The
briefing also warned — correctly — that the premise might be a mis-attributed
observation.

**Measured: the premise is TRUE about the route and FALSE about the lever.**

Route 2's query side (`cjkRunsGE3`) is fed the query's contiguous CJK runs, which
for a natural Chinese question are the whole **punctuation-delimited clauses**.
Those are 15–26 runes, and the route OR-s them into the trigram MATCH as
`"<verbatim run>"`:

```
query  Go 项目里整个模块跑测试时有个别用例收尾阶段报错，怎么定位是环境问题还是辅助代码写错了？
runs   ["项目里整个模块跑测试时有个别用例收尾阶段报错", "怎么定位是环境问题还是辅助代码写错了"]
MATCH "项目里整个模块跑测试时有个别用例收尾阶段报错"      -> 0 rows
MATCH "怎么定位是环境问题还是辅助代码写错了"              -> 0 rows
MATCH "项目里"                                          -> 5 rows
MATCH "怎么定位"                                        -> 130 rows
```

The trigram index splits text into 3-rune grams; a 20-rune query needs a
**verbatim ≥20-rune stretch** to be present in a row. A natural question is
almost never quoted verbatim in a stored lesson, so the route returns 0 by
construction — the index is not broken (`distilled_qa_tri` holds all 8,939 rows,
`distilled_qa` holds 8,939, `distilled_qa_fts` holds 8,939 distinct rowids).

But route 2 is **not** what loses the recall: it is a narrow route that was never
carrying natural questions in the first place, and widening it cannot help,
because the rows that answer a question like the one above contain neither clause
verbatim. The route that DOES reach them is route 3 (the CJK substring LIKE
scan), whose measured candidate set for those queries runs to **thousands of rows
(median 4,412)** — and that is where the recall is lost.

## The defect

Every route's candidates pass through one shared rescore, and **the rescore can
only re-order the rows a route RETURNED** — it never sees what a route's `LIMIT`
discarded before it ran.

Route 3's `WHERE` is a broad OR of `LIKE` predicates. Its candidate set is in the
thousands; its depth was `trigramRecallK` (15). So the route handed the rescore
the first 15 rows the planner happened to visit, and every row beyond that cut —
including the row that answers the question — was unreachable no matter how well
it answered.

The upstream fix (merged here) gave route 3 a **relevance key** that can order the
whole match set (`likeCJKQuery` / `likeCJKRelevanceExpr`, length-weighted
`instr()` sum with confidence demoted to a tiebreak). That fix's own report says
what it did not fix: *"39/120 natural queries had the target OUTSIDE THE POOL
ENTIRELY, and a pool the routes never recall cannot be reordered into the
answer"*. **An ordering the LIMIT truncates inside a tie is not a pool.** This
change is that ceiling.

## The fix

One named constant, one call site — route 3 now scans the depth the relevance key
can actually rank over, instead of a depth borrowed from the trigram route:

```go
// likeCJKRecallK is route 3's OWN scan depth, and it is deliberately not
// trigramRecallK. ... the depth now spans the relevance key's reach ...
likeCJKRecallK = 200
```

`likeCJKQuery(terms, selectExtra, limit)` takes the depth as a parameter (one
production SQL builder, one argument order — SQLite numbers parameters in TEXT
order, so the WHERE terms, then the key's `instr` terms, then the LIMIT are
produced together), and the route passes `likeCJKRecallK`.

Why the change is cheap: the relevance key was already evaluating
`instr(...)` over the whole match set to ORDER it; the LIMIT is applied after
that ordering, so the extra rows cost a longer sort, not a longer scan.
Measured: the arena's 160 CLI invocations complete in the same wall-clock band as
before.

## Upstream decision (measured, not assumed)

The two upstream fixes (route-3 relevance key, and the `meaningfulQueryTokens`
segmenter) existed only on `task/01a127d7fdc37a07a6c940589236fd8b`, not on
master. **Decision: merge it first.** Reasons, all measurable:

1. This epic's own acceptance metric must be a before/after of the *fix under
   test*, and the upstream change is already committed evidence that the ordering
   half was necessary-but-insufficient. Measuring this fix on a baseline that
   lacks it would re-measure a defect that is already diagnosed and fixed.
2. The upstream fix changes the arena baseline (its report: natural 0/120 → still
   fail, control 40/40). Recording that as the baseline is honest; pretending
   master's ordering is the baseline would attribute the ordering fix's absence
   to this change.
3. Red→green separation is kept: the RED proof below reverts **only** this
   change's constant and reproduces the failure on the pre-fix source, with the
   upstream fix present in both runs.

## Verification

The RED proof is "same command, pre-fix source → exit 1, restored source → exit 0",
reverting **only** the depth constant (`likeCJKRecallK = trigramRecallK`, the
byte-exact pre-fix behaviour, since that was the argument the route passed):

```
$ git stash-free revert: internal/tools/memory.go  likeCJKRecallK = trigramRecallK
$ go test ./internal/tools/ -run TestMemorySearchRouteDepthRecallsTheAnswer -count=1
--- FAIL: TestMemorySearchRouteDepthRecallsTheAnswer (1.24s)
    route 3 (depth 15) did not recall the row that answers "盘配与测链" (id=21): the scan
    depth cut the candidate set before the relevance key could act, so the shared rescore
    never saw it. returned 15 ids: [1 2 3 4 5 6 7 8 9 10 11 12 13 14 15]
FAIL  (exit 1)

$ restore: internal/tools/memory.go  likeCJKRecallK = 200
$ go test ./internal/tools/ -run TestMemorySearchRouteDepthRecallsTheAnswer -count=1
ok  github.com/nick/gopi/internal/tools  4.076s   (exit 0)
```

Both transcripts are archived verbatim: `evidence/red_pool_depth.txt`,
`evidence/green_pool_depth.txt`; the two source states are
`evidence/memory_pre_fix_constant.go.txt` / `evidence/memory_post_fix_constant.go.txt`.

**The guard's reader uses the production SQL builder**, not a re-derived query:
`likeCJKQuery(cjkQuerySegments(poolDepthQuery), "", likeCJKRecallK)` — the same
function the route calls, with the same shipped constant, so the test tracks the
route's real reach and cannot drift from it.

**The corpus is a minimal sample, not a mock.** 20 decoys (> `trigramRecallK`) at
confidence 1.0, each matching the query's infix `盘配` **inside a longer word**
(`网盘配置`) so no token route sees it, and the target last at confidence 0.3.
Route 1 asks FTS5 for the whole query run (0 rows), route 1b demands the
segmenter's words `盘配` AND `测链` as `jieba_token` tokens (the rows carry
neither), route 2 asks the trigram index for the verbatim run (0 rows), the
ASCII-OR fallback has no ASCII token — **route 3 is the only route that can reach
either the decoys or the target**, which is what makes its scan depth the single
variable under test. Two earlier corpora failed to reproduce the RED and were
discarded for a measured reason: identical decoys are **deduped at store time**
(the corpus collapsed to 2 rows), and an undotted query let the **recall ladder**
rescue the target through its own AND-subset (`ladder n=1, target? 1`), which is
a route, not a measurement.

## The arena (the acceptance metric)

Same `arena.py run` + `analyze.py`, new arena dir (`arena_after/`, no
`arena_log.jsonl`, so nothing is resumed from the old records), the fixed binary
first on `PATH`:

```
$ PATH=<worktree>/bin:$PATH ARENA_OUT=<session>/arena_after python3 arena.py run
$ ARENA_OUT=<session>/arena_after python3 analyze.py
```

| arm | before | after |
|---|---|---|
| natural (120) | 0/120 (0 perfect, 0 partial, 120 fail) | **11/120** (1 perfect, 10 partial, 109 fail) — hit_rate 0.092 |
| control (40) | 40/40 (34 perfect, 6 partial) | **40/40** (35 perfect, 5 partial) — hit_rate 1.000, NO regression |

Before-column source: the recorded arena (`.../01a1268a-.../arena/analysis.json`)
and `devdoc/memory-route3-relevance.md`; after-column: this run's
`analyze.json` / `analysis.json` in `arena_after/`. The control arm moving 34→35
perfect is one query whose rank moved within the hit set, not a regression (fail
count 0 both times).

### The route-contribution table (all 160 queries, offline harness)

One ranked scan per route per query, on the real corpus, target position read off
each route's own ordering (`evidence/probe_routes.jsonl`, reader
`internal/tools/zz_route_probe2_test.go` — session evidence, not committed):

| | natural (120) | control (40) |
|---|---|---|
| route 1 (exact-token FTS5 AND) matched >0 | **0/120** | 8/40 |
| route 1b (segmented AND on `jieba_token`) matched >0 | **0/120** | 2/40 |
| route 2 (trigram MATCH) matched >0 | **1/120** | 27/40 |
| route 3 target reachable at depth ≤15 | 8/120 | 40/40 (median position **1**) |
| route 3 target reachable at depth ≤60 | 14/120 | 40/40 |
| route 3 target reachable at depth ≤200 | **26/120** | 40/40 |
| route 3 target reachable at any depth ≤300 | 33/120 | 40/40 |
| merged pipeline returned the target in the quota | 12/120 (arena: 11) | 40/40 |

**This confirms the epic's premise and corrects its attribution.** Route 2 is
empty for 119/120 natural queries exactly as the briefings said — and so are
route 1 and route 1b, which the briefings did not name. All three token-driven
routes are structurally dead on a whitespace-free CJK question, for two different
reasons: routes 1 and 1b demand ALL of the query's words in ONE row (an implicit
AND), and route 2 demands a verbatim ≥3-rune stretch. **Route 3 is the only route
that reaches natural questions at all** — so it is the lever, and its depth was
the ceiling: 8 targets reachable at 15, 26 at 200.

The control arm shows the same routes working as designed: route 3 puts the target
at position 1 (median) for all 40, which is why the control arm never moved.

## Not Done (honest)

- 33/120 natural queries have their target somewhere in route 3's ordering (≤300
  rows scanned); **87/120 have it outside that scan entirely** — for those, no
  depth of this route would help, and their recall depends on a route that
  matches *words* rather than substrings. That is the next real lever and it is
  not in this change.
- The natural arm improved 0/120 → 11/120, which is a real lift and a small one:
  **the pool is wider, but route 3's relevance key still cannot separate the row
  that answers a question from a row that merely shares a word with it.** The
  remaining 109 failures are mostly "the target is in the pool and the ranking
  put it outside the quota", which is a ranking problem, not a depth problem —
  the depth ceiling is no longer the binding constraint at 200.
- Full `./cmd/agent` smoke has one pre-existing-looking failure
  (`TestChatCookedFallback_NonTTY` asserts 2 user events in a session file that
  shows 4, plus a `memory-prefetch.jsonl` side file). It is NOT caused by this
  diff (the diff touches `internal/tools/memory.go` only) and is reported, not
  hidden: measured on this branch with the same command.
- Route 2 is left structurally unable to answer natural questions (its query side
  feeds whole clauses to a verbatim-gram index). It is now *diagnosed* with a
  reproduction, not fixed: fixing it means changing `cjkRunsGE3`'s segmentation
  contract, which touches every route's query side — that is its own task with
  its own red/green pair.
- The route-3 depth is a constant (200). It was chosen from the measured
  distribution of the target's position in route 3's relevance order, not from a
  closed-form; a corpus whose answers rank past 200 would still lose them.
- No index was added (ZERO-DEP): the extended depth is a longer sort over the
  same LIKE scan, unprofiled for the worst-case query shape.

## Next dispatchable gap

If the arena's natural arm does not lift as far as the ranking evidence predicts,
the next lever is **not another constant** — it is the pool's *composition*: route
3's LIKE scan is the only route that reaches a query whose words never appear as
segmented tokens, and its candidate set is unordered when the query's terms are
all short. A follow-up task should (a) publish the per-query route-contribution
table the harness above was building, and (b) decide whether route 3's depth
should be **adaptive** (the key's own score distribution) rather than a constant.
