<!-- doc-class: historical -->
# memsearch query safety — no user query can produce an FTS5 error

## Symptom

`gopi memory search` answered with an engine refusal instead of hits or an
empty result:

```
gopi memory search 网络-测试            → SQL logic error: no such column: 测试 (1)
gopi memory search VPN-网络测试环境      → SQL logic error: no such column: 网络测试环境 (1)
gopi memory search deepseek-开支        → SQL logic error: no such column: 开支 (1)
```

The raw query text had reached FTS5's MATCH grammar, where `-` is not a literal
but a **column qualifier** (`term:col` / `term-col`):

```
sqlite> create virtual table f using fts5(x, tokenize='unicode61');
sqlite> select count(*) from f where f match 'token-free';
Error: stepping, no such column: free (1)
```

## Root cause — narrower than the mechanics suggest, and measured, not assumed

The reaching-claim was "the query reaches FTS5 unescaped". The RED run
**refuted** that as stated: pre-fix, `gopi memory search token-free` returned 13
hits with no error. `escapeQuery` (`internal/tools/memory.go`) had always quoted
pure-ASCII fields.

The defect was the **per-field CJK exemption** added by commit `3aff9c4b`
("escapeQuery CJK balance", 2026-08-24):

```go
for _, f := range strings.Fields(q) {
    f = strings.ReplaceAll(f, `"`, "")
    if f == "" { continue }
    if hasCJK(f) {
        quoted = append(quoted, f)      // ← UNQUOTED, hyphen and all
    } else {
        quoted = append(quoted, `"`+f+`"`)
    }
}
```

The exemption exists for a real reason — the unicode61 tokenizer runs a
contiguous CJK run into ONE stored token, so quoting `("网络" "测试")` demands
two standalone tokens that are never stored and the query returns nothing. But
the test was **per field**, and a hyphenated CJK-bearing field is exactly the
shape the exemption must not swallow. Three further holes of the same family:

| input | pre-fix behaviour | why |
|---|---|---|
| `网络-测试` | `no such column: 测试` | field contains CJK → left raw |
| `"` (a lone quote) | `fts5: syntax error near ""` | quote stripped → empty field → empty MATCH |
| `""`, `"   "` | `fts5: syntax error` / refused later | no searchable term at all |

## Fix

`escapeQuery` now returns `(string, bool)`; `ok=false` means "no searchable
term" and the caller **skips route 1** (an empty MATCH expression is a syntax
error, so skipping is what turns "no terms" into a clean 0-hit answer —
Rule 15: a hostile-input query gets a structured result, never an engine error).

The expression builder is one place, `memsearchMatchTerms`:

- every whitespace field is split into its **maximal CJK runs** and the
  ASCII/separator runs between them (`splitCJKRuns`);
- a CJK run goes in **bare** — the pre-existing idiom, unchanged, so CJK recall
  is not traded away;
- an ASCII run is split **where the unicode61 index splits** (`splitFTSWords` /
  `isFTSWordRune` = letter|digit) and every piece is quoted. Splitting at a
  separator (not merely wrapping `"token-free"` as a whole) is the spelling that
  keeps `-` out of the grammar in every position, including the degenerate
  `no-` → `"no"`;
- every `"` is stripped before quoting, so no term can break out of its phrase;
- terms are deduped and capped (below).

Call sites: route 1 of `MemorySearchHits` (`internal/tools/memory.go`) and route
1 of `ProceduralSearch` (`internal/tools/memory_procedural.go`).

### Declared-safe degradation, with measured caps

| constant | value | why |
|---|---|---|
| `memsearchMaxMatchTerms` | 256 | measured: a 120 KB query built ~40 000 terms / a 160 KB statement |
| `memsearchMaxCJKRouteTerms` | 32 | measured: a 300-char CJK query cost 4.75 s across the trigram + 2-gram routes |

A capped query answers with the hits its first terms match (the head wins,
deterministic), never an error.

`cjkQueryGrams` additionally makes the LIKE route refuse a **pure-ASCII** query:
`cjkGrams2` over ASCII yields one 2-char gram per character *pair*, so a
2000-word query built 7 999 terms ≈ 40 000 bound parameters over five columns.

## What recall costs (measured on the production store, 8 rows A/B)

Quoting does not cost recall: per row, the quoted spelling matched the same
count as the raw one. `"memory*"` is a literal inside a phrase (1 245 rows, same
as raw); `token-free` 13 rows. Pure-ASCII hyphen recall is therefore unchanged
from pre-fix — the boundary test pins it so a future fix cannot "simplify" this
back into losing it.

## The filed CJK gap — reproduced, not fixed

A query fragment **shorter than a stored contiguous CJK run** is not reachable
by the bare run: `网络` does not match a stored `网络测试环境` through FTS5
unicode61 alone. On a store whose `jieba_token` column is populated this does
**not** manifest — `jieba_token` carries the 2-grams and `distilled_qa_tri` the
3-grams, so every fragment (`网络`, `测试环境`, `网络测`, the whole run) was
recalled even when the text appeared **only in the answer**
(`TestMemorySearchCJKFragmentsStillRecall`).

The residue is real and filed: a **legacy row whose `jieba_token` is empty**
(imported by direct SQL past the store helper) is reachable only at trigram
granularity. Fixing that is a store/backfill concern, not a query-safety one.

## Tests

`internal/tools/memory_query_safety_test.go` — public entry points only
(`MemorySearch` / `MemorySearchHits` / `ProceduralSearch`), so the pre-fix source
still compiles against it and the RED observation is the real symptom rather
than a compile error.

- `TestMemorySearchQuerySafety` — the boundary sample set: `token-free` /
  `chat-context` / `read-only` / `no-op` / `no-` / `-free` / `x--y` /
  `VPN-网络测试环境` / `网络-测试`; `"token-free"` / `memory"search` / `a'b` /
  `memory*` / `(x)` / `a OR b` / `NOT x` / `NEAR(a b)` / `topic:x` / `a^b` /
  lone `"` / `a\b`; pure CJK; empty / whitespace / quotes-only; oversized ASCII
  and CJK. No sample may put the text `SQL logic`, `fts5:`, `syntax error`,
  `no such column` or `unterminated string` into its output.
- `TestMemorySearchHyphenPhraseRecall` — minimal sample: the one reported
  string must both stop erroring and keep recalling its own row.
- `TestMemorySearchHostileInputNoEngineError` — the same contract through the
  structured exit and the procedural merge (the agent loop's path).
- `TestMemorySearchOverlappingCJKRunsFollowedByASCII` — the grammar hole a
  single-field spell would leave open (`deepseek-开支`).
- `TestMemorySearchCJKFragmentsStillRecall` — the CJK half must not regress.
- `TestMemorySearchOversizedQueryIsCapped` — behaviour, not the number.

### The RED proof needed its own split (a lesson worth keeping)

The first version of the file called the new helper `memsearchMatchTerms`
directly. Restoring the pre-fix sources then produced `undefined:
memsearchMatchTerms` — a **compile error**, and a compile error is not a RED
proof: the regression owes an assertion failure against a *compiling* pre-fix
source, or the RED run measures the test file, not the defect.

Split in `4b1edbf0`: `memory_split_invariant_test.go` carries
`TestFTSBoundarySplitMatchesUnicode61` (it calls the fix's helper and opens a
real fts5 table — it needs a real `db`, which is exactly why the earlier
`splitFTSWords` bug in this very fix was caught there). The RED leg drops that
file **by name in the transcript** and keeps `memory_query_safety_test.go`, which
references no fix-side identifier at all. Then RED is `no such column: 网络测试环境
(1)` / `no such column: 测试 (1)` / `no such column: 开支 (1)` / `no such column: 词
(1)` / `fts5: syntax error near "` — the user-visible symptom, verbatim.

Generalised: **a regression test may only touch the public surface if it is to be
the RED proof.** Anything reaching a new private helper belongs in the same file
only if the helper exists in both legs (or in a second file that the RED leg
removes by name).
