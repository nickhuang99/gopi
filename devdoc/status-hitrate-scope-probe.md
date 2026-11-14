<!-- doc-class: historical -->
# The chat status row's hit rate — measured scope, and where a second reading's cells come from

Task row: `01a10f17b9d07831b19f5c9fe374ed76`. This document is the MEASUREMENT half
of that epic; the implementation half is dispatched separately (see `## Not Done`,
which names the row). Everything below is observed output, not a reading of the
source.

## What the epic asked, and what this run established

The row prints `↑2M↓69k hit 96.6%` with nothing on screen saying what the
percentage is a percentage OF. The operator has doubted the algorithm twice for
that reason. The epic's first requirement was to MEASURE the numerator/denominator
before touching anything, using a throwaway probe.

## 1. The probe, and what it measured

`cmd/agent/zz_probe_test.go` (temporary; **deleted before the commit that landed
this document** — `git status --porcelain` prints nothing, Rule 30). It drove the
PRODUCTION composer rather than guessing from source: `newChatStatusSource` +
`setOpsFn` + `s.status().Line(statusColumnsAtCols)`, the recipe of memory #7321.

```
    zz_probe_test.go:27: tracker: TotalCacheHitTokens=2100000 TotalCacheMissTokens=69000 (write = cache_creation + fresh input)
    zz_probe_test.go:29: hit rate (statusCacheHitRate) = "96.8%"
    zz_probe_test.go:30: pair       (statusCachePairExpr) = "↑2M↓69k"
    zz_probe_test.go:31: field      (statusCacheField)    = "↑2M↓69k hit 96.8%"
    zz_probe_test.go:33: hand-computed read/(read+write)  = "96.8%"  (equal to the composer: true)
```

**Finding 1 — the two counters.** `statusCacheHitRate` is
`cacheRead / (cacheRead + cacheWrite)`, and those two ARE the row's own ops
fields: `cli_chat.go` fills `ops.cacheRead = ag.Cost.TotalCacheHitTokens` and
`ops.cacheWrite = ag.Cost.TotalCacheMissTokens` (from `deepseek.CostTracker`; on
the Claude transport `AddUsage` adds `CacheReadInputTokens` and
`cache_creation_input_tokens + input_tokens`).

**Finding 2 — one source, not two.** `statusCachePairExpr` draws those SAME two
fields. So `↑2M↓69k` and `hit 96.6%` are not two measurements that happen to sit
together: they are one counter pair and its quotient. The denominator IS `↑+↓`,
and it is a SESSION-CUMULATIVE sum (`CostTracker` accumulates across every
request of the session).

**⇒ The algorithm is not wrong; its SCOPE is unstated.** That is exactly the
operator's `#5912` criterion — a percentage without its denominator is
meaningless — and it is why the fix is a display fix, as the epic's boundary says.

## 2. The pinned row, reproduced

```
ROW (100 cells) = "deepseek-flash ·  iter9   ·  no tool             ·  ctx 13k/1M≡ ·↑2M↓69k hit 96.7%     4m00s · ready"
```

Column audit of that same row (reserved vs drawn):

| col | reserved | drawn (trimmed) |
|---|---|---|
| model | 14 | 14 |
| iter | 7 | 5 |
| tool | 19 | 7 |
| metrics | 33 | **30** |
| live | 13 | — |

plus 4 separators × 4 and the 2-cell gap. `statusPinCols == statusColumnsAtCols ==
statusLayoutBudgetCols == 100`, `statusPinSlackCells == 0`.

## 3. Where a second reading's cells can come from — the funding answer

A second, labelled hit-rate reading costs **10 cells** (`hit` 3 + gap 1 + a 6-cell
percentage), or less in a shorter spelling:

```
candidate "96.7%" = 5 cells
candidate "96.7% Δ96.7%" = 12 cells
candidate "96.7% now 96.7%" = 15 cells
candidate "hit 96.7% Δ96.7%" = 16 cells
candidate "hit 96.7% now 96.7%" = 19 cells
```

**No top-level column is a donor.** The air audit
(`TestStatusReservationsAreFullyDrawn`) reports every reservation EXACTLY drawn —
ctx 12/12, cache 19/19, group 33/33, tool 19/19, live 13/13 — and the joint
widest cache value is 19, i.e. its reservation, so the cache sub-cell holds no air
either:

```
ctx sub-cell (statusCtxCol) reserve 12 -> 12
cache value  (statusCacheCol + statusCacheHitRateCells) reserve 19 -> 19
metrics group (statusMetricsFormCol) reserve 33 -> 33
tool column  (statusToolCol) reserve 19 -> 19
live block   (statusLiveCol) reserve 13 -> 13
```

**The one donor is the group's SEAM.** `statusMetricsSep` is
`lineedit.StatusSepGroup` (2 cells) and it is composed BETWEEN two present
sub-cells — it belongs to neither sub-cell's own text, so a re-composition that
joins them on a single cell of air spends exactly those two cells while every fact
on the row survives. That is a 2-cell donation against a ~10-cell need, and it is
the reason the second reading is a two-part job rather than a one-line one (see
`## Not Done`).

## Verification

- Build: `go build ./...` → OK (exit 0, no output).
- Tests: `go test ./cmd/agent/ -run 'TestZZProbe' -v` → `ok github.com/nick/gopi/cmd/agent 1.064s`
  (5 probe tests PASS; the probe file is deleted before this document's commit).
- Ran: `go test ./cmd/agent/ -run 'TestStatusReservationsAreFullyDrawn' -v` →
  the air audit's own table, quoted in §3.
- Result: the probe's output above; the row of §2 reproduced at 100 cells.

## Not Done (honest — Rule 26)

**Neither of the epic's two required changes is implemented in this branch**, and
this section says so rather than closing the row:

1. **The scope is not yet marked on the row.** No cumulative-scope marker, no
   legend sentence saying `hit%`'s denominator is `↑+↓` and that it is the
   session's total.
2. **The per-turn (differential) reading does not exist.** It needs the turn
   boundary (`internal/agent/loop.go`'s `Chat`/`ChatOps` already log a per-request
   delta for the session JSONL — `a.Cost.Total* - prev*`), a new pair of fields on
   `statusOps`, a composer, and the reader half in
   `chat_status_hitrate_emphasis.go` for the SECOND reading (Rule 14: one suffix
   constant for writer and reader, or the tier colouring fails silently).
3. **The ~10 premise-bound guard tests were not touched**, because the change they
   guard was not made. Whoever implements item 1–2 must update their premises
   (never silence them).
4. **No real-pty evidence** — nothing to demonstrate yet, and a `## Ran:` line for
   an unimplemented surface would be a claim with nothing behind it.

The follow-up row carries the measurements above so the implementer does not
re-derive them.

`statusGroupCellsShed` (the group's seam as a named term) was drafted during this
run and then **reverted** rather than committed: with no second reading to fund,
it was a constant nothing referenced, which is the defect Rule 13 names. It
belongs in the commit that spends it.
