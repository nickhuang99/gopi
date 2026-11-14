<!-- doc-class: historical -->
# The chat prefetch runs the recall ladder — the same AND-subset walk memory_search runs

Task `01a113a2-2ef7-71de-9a8c-6195314eb016`. Branch
`task/01a113a22ef771de9a8c6195314eb016`.

## What was wrong

Two consumers asked the same FTS5 index the same question and got different
answers, because only one of them could see the ladder:

- `internal/tools/memory.go` walks a **逐步缩小的 AND ladder** (Route 1.5,
  `memsearchSubsetPick`): full AND → every smaller AND-subset, ranked by how many
  terms it keeps and by how much noise it sheds.
- `cmd/agent/chat_prefetch.go` built its own query with `ftsMatchQuery`: **every
  keyword OR-ed**. It could not use the ladder — the ladder was unexported and
  lived beside `openSearchDB`, a **migrating** opener the prefetch must not touch.

MEASURED on the operator's own ledger (251MB, 2026-10-07), query
`254 321 426 cancelled e2e help merger 条积压`:

```
all-AND  →      0 rows
all-OR   →  2,619 rows
ladder   → "254" AND "321" AND "426" AND "cancelled"  →  1 row
```

The prefetch takes 6 rows by bm25 out of that 2,619-row pool and injects them
into the prompt (default ON, 1200-rune budget). The top-6 of the OR pool
contains *"CLI help synopsis drift"* and *"Promoting a gopi sub-command"* —
echoes of the token `help`, not of the question. The row the question is about
was not in the pool at all.

## Completed

1. **The ladder is now one shared layer.** `internal/tools/memory_ladder.go`
   holds it (`memsearchLadderTerms`/`memsearchLadderCount`/`memsearchDroppedDF`/
   `memsearchTermDF`/`memsearchSubsetPick`/`memsearchSubsets`), moved out of
   `memory.go` **verbatim**, plus one three-line face:

   ```go
   func LadderMatch(ctx context.Context, db *sql.DB, query string) (string, bool)
   ```

   `memory.go` shrank by 280 lines and keeps calling the same functions.

2. **Two stop conditions were added, and they are the only behaviour change**
   (the ladder's *choices* are untouched — the epic's constraint):

   - `ctx.Err()` ends the walk with what it has. The ladder always took a ctx and
     never read it between probes. MEASURED full walks on that ledger:
     **861ms / 1723ms / 3203ms** — i.e. handing the ladder the prefetch's whole
     1500ms budget would burn it and still give up, on exactly the queries this
     task exists to fix. A partial result is the *same* ranking (the walk descends
     from the biggest subset), so stopping early costs nothing but time.
   - `LedgerBusyErr` ends the walk with no pick. A new shared classifier
     (`internal/tools/ledger_dsn.go`, Rule 14) — `cmd/agent`'s retry path and the
     ladder must agree on what "locked" means, so neither re-types the match.

     This one was found by an existing test, not by reasoning:
     `TestPrefetchBusyIsRetriedWithinTheSameBudget` went red with
     `prefetch gave up after 1500ms (query): context deadline exceeded` where it
     demands `SQLITE_BUSY`. Mechanism: the old `memsearchLadderCount` swallowed
     every error as "zero matches", so a locked ledger was asked up to
     `memsearchMaxCombos` (1023) more times at 250ms each, converting a refusal
     the keystroke path already handles *in 250ms* into a deadline expiry that
     carries no reason.

3. **The prefetch's candidate set comes from the ladder** (`prefetchQueryShared`),
   with OR kept as the fallback: ladder → if no pick in budget → OR. `OR` stays
   because it is measurably not useless (it is a wide net that ranks by bm25) —
   it is the right answer exactly when the ladder has none.

4. **An explicit budget partition, not a new budget.**
   `prefetchLadderBudgetMs = 900` is a partition of the existing
   `prefetchGoroutineTimeoutMs = 1500`: the ladder may spend 900ms, the fallback
   gets ~600ms (3x the worst OR measured). **No new algorithm constant was
   invented** — every ladder constant
   (`memsearchMinANDTokens`/`memsearchMaxANDTokens`/`memsearchMaxCombos`/
   `memsearchMaxSubsetProbes`/`memsearchMaxTokensPerSubset`) is reused as-is.

5. **`search shape in ONE place` is preserved.** `prefetchSearchSQL` is still the
   single SQL definition; the new code only chooses which MATCH expression feeds
   it. `prefetchQueryRows` is the one runner.

6. **The read-only contract and the observability are preserved, and extended.**
   The record gains one field, `candidates` (`ladder` / `or`), so the central
   claim is checkable *per lookup* rather than inferred from the code — and the
   OR fallback's rate becomes a number instead of a claim.

7. **Two regression tests** (`cmd/agent/chat_prefetch_ladder_test.go`): one pins
   the ladder's candidate set and the noise that must NOT reach the prompt, one
   pins the OR fallback. Both drive the real seam (`prefetchLookupDetailed`).

## The three measured numbers (改前 vs 改后)

**1. Relevance — 12 real queries against the real ledger** (the recorded
`memory-prefetch.jsonl` queries plus the ladder probes; full table in
`probe-relevance.txt`). OR's pool vs the ladder's pick:

| query | OR pool | OR top-6 | ladder pick | ladder rows | ladder top-1 |
|---|---|---|---|---|---|
| `254 321 426 cancelled e2e help merger 条积压` | 2619 | 2 of 6 about `help` aliasing | `"254" AND "321" AND "426" AND "cancelled"` | 1 | the row the question is about |
| `peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太` | 1616 | HP/peerbox fork rows | `"peerbox" AND "现在" AND "太多"` | 1 | the fork-handling row |
| `read home nick gopi sessions 9b04d80e 16f5 422d` | 7436 | lineedit/光标错位 rows | `"home" AND "nick" AND "gopi" AND "9b04d80e" AND "16f5" AND "422d"` | 3 | the session-row |
| `poc 不走 那么 么集 集成 成之 之后 后在` | 429 | mixed | `"poc" AND "集成" AND "成之"` | 1 | the POC-generalisation row |
| `ctx 81k 69k hit` | 681 | chat-status rows | `"ctx" AND "81k" AND "69k" AND "hit"` | 1 | the ladder record itself |
| `memory prefetch read only ledger budget` | 6206 | prefetch-timeout rows | all 6 tokens | 1 | the prefetch-ledger row |
| …7 more, same shape | 254–7436 | | | 1–13 | |

In **12 of 12** cases the ladder's first row is on topic; the OR pool's top-6 is
dominated by single-token collisions. Pool size drops from **254–7436** to
**1–13**.

**2. gave-up share (the hard constraint).** From the real
`~/.gopi/sessions/*/memory-prefetch.jsonl`, lookups only (excluding the
`adopted`/`superseded` event lines):

| set | lookups | gave-up | ms median | ms p95 | ms max |
|---|---|---|---|---|---|
| pre-existing sessions (before this change) | 134 | **34 = 25.4%** | 317 | 1706 | 2356 |
| this task's runs (after) | 9 | **0 = 0%** | 98 | 678 | 678 |

33 gave-up lines of the 34 pre-existing ones sit at exactly 1503–1624ms — the
budget expiring, the shape `devdoc/memory-prefetch-giveup-diagnosis.md` removed
in a later fix. **The after-run shows no regression**: 0 gave-up, and the
end-to-end maximum is 678ms, well inside 1500ms.

**3. End-to-end, same three queries, same ledger, interactive `gopi chat` on a
pty** (real binary `gopi.before` / `gopi.after`, real 251MB ledger, load 6–17):

| query | BEFORE (OR) | AFTER (ladder) |
|---|---|---|
| `254 321 426 cancelled e2e help merger 条积压` | hits=6, 83ms | `candidates:"ladder"` hits=**1**, 614ms |
| `peerbox 现在 上任务积压太多了 …` | hits=6, 33ms | `candidates:"ladder"` hits=**1**, 678ms |
| `read home nick gopi sessions 9b04d80e 16f5 422d` | hits=6, 98ms | `candidates:"ladder"` hits=**3**, 674ms |
| `memory prefetch ledger budget` | — | `candidates:"ladder"` hits=1, 38ms |

**The trade, stated honestly:** the injected section shrank (hits 6→1/1/3;
`section_bytes` 2169→1234 on the repeated query) and the per-lookup cost rose
from 33–98ms to 589–678ms — the price of the ladder's COUNT probes (up to ~250ms
of walk before the pick). It stays inside the 1500ms budget with 0 gave-up, and
it is still below the pre-existing ledger's own median (317ms, n=134). The
ladder was already measured at 0–465ms on the same queries with a cold handle;
the interactive figure is higher because the first probe after a typing pause
pays the index warm-up.

## Not Done (honest)

- **`peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太` does not finish its
  walk inside 900ms** (measured 1723ms unbounded) yet still reports a ladder
  pick — the pick arrives before the budget ends. A query whose pick arrives
  *after* 900ms falls back to OR. Raising `prefetchLadderBudgetMs` toward 1500
  would convert those to give-up (the shape the table above calls the worst
  failure), so the partition errs toward the fallback.
- **The prefetch still sees fewer routes than `MemorySearchHits`.** memory_search
  unions ladder + OR + jieba-segment route + trigram + LIKE; the prefetch is
  ladder-or-OR only, because the other routes need tables (`distilled_qa_tri`)
  and a migration the read-only contract forbids. So a CJK query that only the
  trigram route can recall still misses in the prefetch. Not attempted here.
- **Sample size.** 9 lookups after the change (3 interactive runs). The
  pre-existing 134 is a real baseline, but the after-set is small; a wider
  sample is the operator's to run by simply using chat.
- **No constant was tuned.** `memsearchMaxCombos` (1023) and
  `memsearchMaxSubsetProbes` (64) are live bounds and cost real time; no measured
  case demanded retuning, so they were left alone (the epic permits retuning only
  with a measurement that demands it).
- **The tiebreak direction was not touched** — `memsearchDroppedDF` maximises
  dropped-DF on measured grounds (documented in its own comment); this change
  moved code without re-deciding it.

## A positive answer to hermes' `prefer_or`

hermes' `_adaptive_bfs` (`agent/fts5_query.py:180`) carries
`prefer_or=True` — *"skip BFS, use OR"*, and its own comment says that is for
**passive injection**. This prefetch *is* passive injection, so the question the
epic asks is fair: why not keep OR?

**Because passive ≠ unfiltered, and the cost of a wrong injection here is not
free.** The argument is three measurements:

1. **The consumer is bounded and the model cannot tell a miss from a match.**
   The section is capped at 1200 runes and is presented as recalled context. An
   irrelevant row does not merely waste a slot — it spends the budget and
   asserts "this was recalled for your question". Measured:
   `section_bytes` 2169 → 1234 on the same query, with `adopted: yes` unchanged:
   the prompt got *smaller and more on-topic*, not emptier.
2. **OR's ranking authority is bm25 over a pool of 254–7436 rows**, which is
   dominated by token frequency, not by the query's subject. Measured top-6 for
   the `254 321 426 …` query is `help`-echoes; the subject row was not in the
   pool. Breadth did not buy the right answer — it bought 2,619 candidates and
   the wrong 6.
3. **hermes' own design says the same thing where it counts.** Its BFS has a
   fast path that returns immediately when the full AND qualifies, and its
   fallback is OR *after* the walk. `prefer_or` is a **switch**, not the
   algorithm: it exists so a caller with no ranking investment can skip the walk.
   Our prefetch has exactly that investment — `prefetchSearchSQL` ranks by
   `bm25` then `confidence` — and it now receives a candidate set worth ranking.

**And OR is not deleted** — it is the fallback, which is where hermes puts it
too. The positive reading of `prefer_or` that this change adopts: *the fallback
must exist and must be reachable*, which the second regression test pins
(`TestPrefetchFallsBackToORWhenTheLadderHasNoPick`). What it rejects is making
the fallback the *primary* path for a consumer whose output is a bounded,
authoritative-looking prompt section.

## Files

| file | change |
|---|---|
| `internal/tools/memory_ladder.go` | NEW — the ladder, moved verbatim + 2 stop conditions + `LadderMatch` |
| `internal/tools/memory.go` | −280 lines (the ladder cut out) |
| `internal/tools/ledger_dsn.go` | +`LedgerBusyErr` — the ONE busy classifier (Rule 14) |
| `cmd/agent/chat_prefetch.go` | ladder-first candidate set, `prefetchLadderBudgetMs`, `candidates` observable, `isBusyErr` delegates |
| `cmd/agent/chat_prefetch_ladder_test.go` | NEW — the two regression tests |

## Verification

Sample: evidence files are under the run's session directory
`~/.gopi/sessions/01a113a2-2ef7-71de-9a8c-6195314eb016/`
(`prefetch-before.jsonl`, `probe-relevance.txt`, `pty-before.out`,
`pty-after.out`).

### Build

```
$ go build ./...
(no output, exit 0)
```

### Tests

```
$ go test ./cmd/agent/ -run Prefetch -count=1
ok  	github.com/nick/gopi/cmd/agent	7.196s

$ go test ./internal/tools/ -run Memsearch -count=1
ok  	github.com/nick/gopi/internal/tools	3.268s
```

### RED → GREEN (the new guard, both directions, verbatim)

RED — production bypassed to the pre-change shape (`ok = false` forced after
`LadderMatch`, i.e. the old pure-OR candidate set):

```
--- FAIL: TestPrefetchCandidatesComeFromTheLadderNotFromOR (0.05s)
    chat_prefetch_ladder_test.go:156: candidates = "or", want "ladder" (outcome=hits err=<nil>) — the candidate set is not coming from the ladder
    chat_prefetch_ladder_test.go:167: hits = 6, want 1 — a candidate set wider than the single row that answers the query is the OR pool this change removes (section="
        memory-prefetch (experimental, optional; 5 hits; 259 runes of 1200 budget)
        [memory:#31|established|hot] the one row whose body carries both real tokens
        [memory:#1|provisional|warm] zzfiller filler body that shares only the common word
        [memory:#2|provisional|warm] zzfiller filler body that shares only the common word
        [memory:#3|provisional|warm] zzfiller filler body that shares only the common word
        [memory:#4|provisional|warm] zzfiller filler body that shares only the common word")
--- FAIL: TestPrefetchCandidatesComeFromTheLadderNotFromOR (0.05s)
FAIL	github.com/nick/gopi/cmd/agent	2.029s
```

Note what the RED output *shows*: the section carrying four `zzfiller` noise rows
— the defect rendered, not asserted about.

GREEN — the same command after restoring the production line:

```
$ go test ./cmd/agent/ -run TestPrefetchCandidatesComeFromTheLadderNotFromOR -count=1 -v
=== RUN   TestPrefetchCandidatesComeFromTheLadderNotFromOR
--- PASS: TestPrefetchCandidatesComeFromTheLadderNotFromOR (0.02s)
PASS
ok  	github.com/nick/gopi/cmd/agent	1.403s
```

The fixture also asserts its own premise before the claim (the full AND matches
**0** rows and the OR pool is wider than the prefetch's limit — the minimal
sample that carries the shape), so the test cannot pass by the ladder's fast path
answering the query.

### Ran (the surfaces this change touches, executed for real)

**Surface 1 — the shared ladder's own cost and picks, through the real function,
against the operator's real 251MB ledger:**

```
$ go test ./internal/tools/ -run TestZZLadderWalkBudget -count=1 -v
    cold   "254 321 426 cancelled e2e help merger 条积压"      ok=true     861ms terms=4 match="254" AND "321" AND "426" AND "cancelled"
    warm   "254 321 426 cancelled e2e help merger 条积压"      ok=true    1208ms terms=4 match="254" AND "321" AND "426" AND "cancelled"
    cold   "peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太"               ok=true    1723ms terms=3 match="peerbox" AND "现在" AND "太多"
    cold   "read home nick gopi sessions 9b04d80e 16f5 422d" ok=true     954ms terms=6 match="home" AND "nick" AND "gopi" AND "9b04d80e" AND "16f5" AND "
    cold   "ctx 81k 69k hit"                                ok=true       1ms terms=4 match="ctx" AND "81k" AND "69k" AND "hit"
    cold   "还没 没修 修复 复的 的问 问题"                              ok=true    3203ms terms=4 match="没修" AND "修复" AND "的问" AND "问题"
```

This is the measurement that produced `prefetchLadderBudgetMs = 900` and the
`ctx.Err()` stop condition. (The probe file was deleted before commit; its output
is preserved above and in the session directory.)

**Surface 2 — the prefetch's callback path, interactive, on a pty, with the real
binary and the real ledger** (three typed lines per run; the pty is required
because the prefetch's line source is the editor's published snapshot, which
exists only on the interactive path):

```
$ python3 pty_hitl_prefetch.py ./gopi.before "254 321 426 cancelled e2e help merger 条积压" \
      "peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太" "read home nick gopi sessions 9b04d80e 16f5 422d"
$ cat ~/.gopi/sessions/aba87a32-4b2f-4a75-a4ef-16450c812b72/memory-prefetch.jsonl
{"query":"254 321 426 cancelled e2e help merger 条积压","hits":6,"ms":83,"outcome":"hits","adopted":"yes","segments":{...},"attempts":1}
{"query":"peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太","hits":6,"ms":33,"outcome":"hits","adopted":"yes","segments":{...},"attempts":1}
{"query":"read home nick gopi sessions 9b04d80e 16f5 422d","hits":6,"ms":98,"outcome":"hits","adopted":"yes","segments":{...},"attempts":1}
```

```
$ python3 pty_hitl_prefetch.py ./gopi.after  <the same three lines>
$ cat ~/.gopi/sessions/379c7d3d-d413-4d75-bad1-4d119305fbfe/memory-prefetch.jsonl
{"query":"254 321 426 cancelled e2e help merger 条积压","hits":1,"ms":614,"outcome":"hits","adopted":"yes","segments":{"seg_stat_ms":0,"seg_open_ms":0,"seg_query_ms":610,"handle":"session","wait_count":0,"wait_ms":0,"handle_warm_ms":2,"journal_mode":"wal","query_only":1,"busy_timeout_ms":250,"attempts":1},"candidates":"ladder"}
{"query":"peerbox 现在 上任务积压太多了 上任 任务 务积 积压 压太","hits":1,"ms":678,"outcome":"hits","adopted":"yes","segments":{"seg_stat_ms":0,"seg_open_ms":0,"seg_query_ms":678,"handle":"session","wait_count":0,"wait_ms":0,"handle_warm_ms":2,"journal_mode":"wal","query_only":1,"busy_timeout_ms":250,"attempts":1},"candidates":"ladder"}
{"query":"read home nick gopi sessions 9b04d80e 16f5 422d","hits":3,"ms":674,"outcome":"hits","adopted":"yes","segments":{"seg_stat_ms":0,"seg_open_ms":0,"seg_query_ms":673,"handle":"session","wait_count":0,"wait_ms":0,"handle_warm_ms":2,"journal_mode":"wal","query_only":1,"busy_timeout_ms":250,"attempts":1},"candidates":"ladder"}
```

The same record shows the read-only contract observed on the live connection
(`journal_mode: wal`, `query_only: 1`, `busy_timeout_ms: 250`, `handle: session`)
— read from the connection, not assumed from the DSN.

**Result:** `candidates:"ladder"` on every lookup, `outcome:"hits"`,
`adopted:"yes"`, hits 6→1/1/3, 0 gave-up, max 678ms.

### Gates

- `go build ./...` — clean.
- `go vet ./cmd/agent/ ./internal/tools/` — clean.
- `go test ./internal/standards/` — 5 failures, **all pre-existing on this
  branch and all naming files outside this diff** (verified by
  `git status --porcelain -- <each file>` returning empty): `Rule 16` at
  `internal/tools/worktree_sweep.go:2431`, `Rule 17` in 38 test files, `Rule 18`
  at `internal/lineedit/editor.go:1623` (and one at `memory.go:3496` that the
  branch's own base already carries — `git show HEAD:internal/tools/memory.go |
  grep -c 'else-ok: the error branch is the guard clause'` → `1`), `Rule 20` at
  `cmd/agent/cli_tool_spelling_table.go:170`, `Rule 13` at
  `internal/shot/x11.go:193`, plus the `DOC-INDEX` banner gate and the
  `COMMANDS.md` generated-block gate. **No gate hit falls in a file this change
  touches.**
