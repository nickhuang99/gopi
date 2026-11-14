<!-- doc-class: historical -->
# AND-Subset Degradation Ladder — the missing middle in `memory search`

Branch: `feat/memsearch-subset-degrade` (worktree `~/workspace/gopi-memsearch-degrade`)
Epic: gopi's `memory search` had only two extremes — all-AND and all-OR — so a
natural-language CJK/ASCII query whose full token set matches no single row fell
straight to OR and returned unrelated old rows.

## 1. The defect, measured

`escapeQuery` AND-joins every token into one FTS5 MATCH. One unmatched token
zeroes the whole query:

```
sqlite3 ~/.gopi/memory.db "SELECT COUNT(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH '\"deepseek\" 降低';"
0                     # 337 rows mention deepseek — one conjunct kills them all
```

The old recovery was `escapeQueryOR` (route 1.5): OR every ASCII token. Its pool
is "any row sharing ANY one token", so a high-DF token floods it — 427 rows
match `怎么` alone — and the query's real subject never enters the capped pool.
Measured with the real corpus and a 22-row fixture (`.evid/redproof.sh`):

```
=== PRE (master, no ladder) ===
#12 deepseek clutter 10 ...      <- the subject is NOT in the result set at all
#13 deepseek clutter 11 ...
=== POST (ladder) ===
#1 成本分析 ... tokens=3         <- the conjunction the query actually asked for
#12 deepseek clutter 10 ...
```

## 2. Mechanism

`memsearchSubsetPick` (`internal/tools/memory.go`) is the middle rung, ported
from the reference engine's `_adaptive_bfs` (hermes `agent/fts5_query.py:180`):

1. **Fast path** — `memsearchLadderCount(full AND) >= memsearchMinSubsetCount`
   → return the full AND. A query the exact route can already answer is never
   downgraded.
2. **Descent** — `for size := len(tokens)-1; size >= 1; size--`, every subset of
   that size (lexicographic index combinations, original token order preserved).
   A subset qualifies when its AND matches ≥ `memsearchMinSubsetCount` rows.
3. **Ranking** — `(more terms retained, then LARGER summed DF of the dropped
   terms)`. The walk order already satisfies the first key, so the first size
   that yields qualifiers holds the winner; the second key breaks ties within a
   size. Larger, not smaller — see §3a.
4. **No qualifier** → the ladder reports nothing and the pre-existing OR route
   runs **unchanged**.

Recall is therefore strictly additive: the ladder's subset candidates are added
FIRST, and the OR fallback still runs when the exact route missed. This was a
deliberate correction during implementation — the first version gated OR behind
the ladder (`if len(order) == 0` after the ladder), which NARROWED recall and
went red on `TestMemorySearchASCIIPartialRecall`. The shipped shape adds
precision ahead of breadth; it never trades breadth away.

### Ordering: a sort key, not a score adjustment

Rows the chosen subset matched are promoted to the front by an explicit
comparator branch, and the rendered `tokens=` signal is left EXACTLY as it was
(how many meaningful query tokens the row's own text covers). An intermediate
version added the subset size to `h.Tokens`; that inflated the user-facing
signal (`tokens=6` on a 3-token query) and broke
`TestMemorySearchRankingTokenCoverage`. Keeping the promotions in the comparator
preserves the contract that the recall routes own candidate generation and the
ranking owns order.

## 3a. The tiebreak direction is INVERTED from the reference, on measurement

The reference minimizes the summed DF of the dropped terms
(`penalty = Σ _term_df(dropped)`, sorted ascending); its `_term_df` doc calls
low DF "more likely noise", i.e. it sheds the query's most common words. Ported
verbatim, that rule is measurably WRONG on gopi's corpus:

```
sqlite3 /tmp/memcopy.db "SELECT COUNT(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH '怎么';"   -> 434
sqlite3 /tmp/memcopy.db "SELECT COUNT(*) FROM distilled_qa_fts WHERE distilled_qa_fts MATCH '降低';"   ->  10
MATCH '怎么 AND 降低'                                        -> 136 rows
MATCH '"deepseek" AND 降低'                                  ->   1 row   (4668)
```

Minimizing kept 怎么 — a filler that is literally in gopi's `cjkStopwords` —
and shed `deepseek`, selecting the 136-row conjunct over the 1-row conjunct that
contains the row the user asked for. Maximizing drops 怎么 and keeps
`deepseek AND 降低`, and 4668 becomes **rank 1**. The epic's own wording ("drop
noise keep signal") describes the maximizing direction; "smaller summed DF of
dropped tokens" in the brief was followed only as far as fidelity to the
reference, and the deviation is recorded here with the measurement that forced
it. `TestMemsearchLadderShedsTheFillerNotTheSignal` pins the shipped direction,
so a later "port fidelity" change that flips it back goes red.

## 3. Threshold rationale — why 1, and why it is NOT `min_results`

The reference gates its fast path on `min_results` (an absolute result count).
gopi deliberately does **not**: the brief had to not weaken recall, and lowering
a result-count threshold to make a miss "hit" trades precision for a number.
`memsearchMinSubsetCount = 1` is a **specificity floor** — "this subset is
specific enough to be evidence" — and 1 is the strictest value that still admits
a single well-aimed row. It gates the ladder's activation; it never fabricates
hits. `memsearchMinANDTokens = 3` is the separate activation floor for the walk
itself (below it the OR fallback already handles the query, and the candidate
space is not worth the probes).

## 4. Combination-explosion bound

The candidate space is every subset, so it must be bounded (Rule 1a):

| constant | value | role |
|---|---|---|
| `memsearchMinANDTokens` | 3 | ladder activation floor (and the walk's own bound) |
| `memsearchMaxANDTokens` | 10 | queries above this are refused, not walked |
| `memsearchMaxCombos` | 1023 | hard stop on total subset probes = 2^10 − 1 |
| `memsearchMaxSubsetProbes` | 64 | stop collecting qualifiers once the winner is settled |
| `memsearchMaxTokensPerSubset` | 8 | SQLite variable ceiling per probe statement |

`TestMemsearchLadderTokenCap` pins the arithmetic: at the cap the full subset
space is exactly `memsearchMaxCombos` (1023), and the descent space (sizes 9..1,
1013) is inside it. A pathological query can therefore never become an unbounded
scan.

## 5. The token notion (and injection safety)

`memsearchLadderTerms` splits a query into the terms the descent walks over:
each whitespace field with quotes stripped, plus — for a field containing CJK —
its **2-char CJK grams** and the ASCII sub-run peeled off the glue
(`怎么降低deepseek开支` → `怎么, 么降, 降低, deepseek, 开支`). Grams are the
granularity `jieba_token` indexes, and the reason a natural-language CJK query
can descend at all instead of being one un-matchable 8-char run.

Every term is returned **quoted**, so a token can never reach FTS5 as an
operator — the same neutralization as `escapeQuery`/`escapeQueryOR`.
`escapeQuery` itself is untouched, including its CJK-unquoted semantics
(constraint honoured; `TestMemorySearchOperatorQueryEscaped` and
`TestEscapeQueryOR` stay green).

## 6. Provenance of `jieba_token` is load-bearing (be honest about it)

`jieba_token` was derived from `question + " " + answer` only, so a row whose
phrasing lives in `search_kw` is unreachable — the defect recorded in
`devdoc/jieba-token-absorbs-search-kw.md` (memory #4700/#4701). The ladder's
tests were therefore built on shapes that do NOT depend on that fix: the red
proof's subject row carries its CJK signal in the BODY and in `search_kw` (a
documented consumer writes `search_kw`), and the assertion is about ORDER
(master ranks the hot single-token row first), not about recall — because the
pre-existing 2-char LIKE route recalls such a row anyway and would make a
"was it recalled" assertion green on master.

## 7. The real corpus, and what is NOT delivered here

`gopi memory search "怎么降低deepseek开支"` would ideally recall ids 4663–4668.
**It cannot, and the ladder is not the blocker.** Measured on
`~/.gopi/memory.db`:

- rows 4663–4668 store literal `\uXXXX` escapes (hex-verified, not eyeballed:
  `SELECT hex(topic) FROM distilled_qa WHERE id=4663` → `5C75363231305C…`);
- the same six rows have **zero** entries in `distilled_qa_fts` (external-content
  FTS5: an unindexed row looks healthy in the content table and no MATCH can
  ever reach it);
- even repaired, 4663/4666/4667 share **no** token with that query's token set.

Repairing a COPY of the store (`.evid/repair_copy.py`, `.evid/repair_copy2.py`:
decode the escapes, derive the gram column, rebuild the FTS row with the full
12-column set) and re-measuring gives `MATCH '"deepseek" AND 降低'` = 1 row
(4668), and the ladder puts **4668 at rank 1** (it is not in the top 40 at all with the
reference's minimize direction). 4663/4666/4667 remain unreachable even
repaired: they share no token with the query, so no subset can reach them —
recall cannot invent a shared term. The write-side repair is filed as a
separate bug task (`01a0c668-b7ad-71c2-b31e-f35a8213e21d`) because it is a
different defect (write side, not query side) and Rule 26 forbids closing the
gap by pretending the query side can fix it.

## 8. Verification

- `go build ./...`, `go vet ./internal/tools/` — clean; `gofmt -l` clean on both
  touched files.
- Red proof (behavioural): the five new tests vs MASTER's `memory.go` (with a
  temporary shim reproducing the pre-change semantics, since master cannot see
  the new symbols) — all five FAIL; with the change, all five PASS. See the task
  report for the pasted output.
- The crowd-out red proof on a real 22-row fixture: PRE returns five unrelated
  rows and the subject is absent; POST returns the subject first.
- Regression: the full `internal/tools` memory-search test set passes, including
  `TestMemorySearchASCIIPartialRecall` (the OR route's reachability) and
  `TestMemorySearchRankingTokenCoverage` (the `tokens=` contract).

## 9. OR relationship, in one line

The ladder sits BETWEEN all-AND and all-OR: it is tried only when the exact AND
missed, it contributes its candidates first, and all-OR still runs afterwards
exactly as before — so every row that was reachable before is still reachable,
and the rows that answer the query now lead.
