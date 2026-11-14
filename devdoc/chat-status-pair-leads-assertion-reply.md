<!-- doc-class: historical -->
# Reply — [2/4] the cell-addressed guard for the ctx-pair lead (task 01a0faa7)

**For the planner of epic 01a0f8c9.** The deliverable is
`cmd/agent/chat_status_pair_leads_test.go`
(`TestStatusMetricsGroupLeadsWithTheCtxPairAndRidesTheRateOnTheCacheSubCell`).

## The precondition I verified before writing the case

The briefing supposes that a case driving the real composer at the pin **fails today**
("verify by temporarily reverting the lead"). Measured at this branch's base
(`58969b39`, a merge of `task/01a0fa8115587e3d99b9512d13c72c57`): **it does not fail —
the state is already correct.**

- `cmd/agent/chat_stream.go:2245` `statusCtxField` carries the re-partition's own note
  and composes `label + pair + statusCtxQuantGlyph`; it never reaches
  `statusCacheHitRateField`.
- `git log -1 -- cmd/agent/chat_stream.go` → `9316f3dd feat(status-row): the context
  pair leads the metrics group; the hit rate rides the cache sub-cell`.
- The green run prints the pinned row verbatim:
  `deepseek-flash ·  iter 12 ·  ⚙ keyword_search l… ·  ctx 42k/64k≡ ·↑12k↓4k hit 74.5%    ⧉2 · ready`

So this row is a **GUARD over landed work, not a red-first fix**, and the acceptance's
"it fails if either arm regresses" was satisfied by a deliberate local mutation instead
of by the unmodified tree. Two mutations, both reverted, both red (see the report's
`## Evidence`): (a) the cache sub-cell moved ahead of the context sub-cell in
`statusMetricsField`'s `fields` array; (b) the pre-re-partition body restored at the
top of `statusCtxField` (the rate returned whenever the cache counters are measured).
Each failure named the cell it moved to.

## What the case derives rather than spells

Nothing in the new file is a second spelling of the row's composition:
`statusCtxField` / `statusCtxPair` / `statusCacheField` / `statusCachePairExpr` answer
what is drawn, and the geometry (`groupStart`, the cache sub-cell's first cell, the
group's drawn width) is summed from `statusModelCol`, `statusIterCol`, `statusToolCol`,
`statusColumnSepTotalN`, `statusCtxCol`, `statusMetricsSepCells` and
`statusMetricsFormCol`.

## The boundary this case does NOT own

It is addressed at the geometry, so it asserts the pair leads and **does not** assert
that a rate is present whenever the cache pair is — a row that carried the pair alone
passes it. That arm is `TestStatusRowKeepsBothThePairAndTheRate`
(`chat_status_hitrate_test.go:307`), which is explicit about it ("the rate must yield
only where the cells force a choice, and never silently (acceptance (e))"). A reviewer
running the epic's `-run 'StatusRow|StatusMetrics|StatusCtx|StatusCache'` set gets both
halves; this file adds the cell address, not a second presence check.

## Sibling work this run observed

The branch head is `58969b39 Merge branch 'task/01a0fa8115587e3d99b9512d13c72c57'` —
i.e. another row of this epic series landed on this branch's base while this run was
open. Nothing in this file collides with it; the pinned row above is that state's own
output.
